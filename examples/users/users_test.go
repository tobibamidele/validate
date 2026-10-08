package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type call struct {
	srv *httptest.Server
	t   *testing.T
}

func (c call) do(method, path, body string) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func issueCodes(body map[string]any) []string {
	var out []string
	for _, i := range body["issues"].([]any) {
		m := i.(map[string]any)
		path := m["path"].([]any)
		p := ""
		if len(path) > 0 {
			p = path[0].(string)
		}
		out = append(out, p+":"+m["code"].(string))
	}
	return out
}

func TestUsersAPIEndToEnd(t *testing.T) {
	srv := httptest.NewServer((&server{db: newStore()}).routes())
	defer srv.Close()
	c := call{srv, t}

	// POST: trims and lowercases, never echoes the password
	status, u := c.do("POST", "/users", `{"email":"  Me@Example.COM ","password":"correct horse","age":30}`)
	if status != 201 || u["email"] != "me@example.com" {
		t.Fatal(status, u)
	}
	if _, leaked := u["password"]; leaked {
		t.Fatal("password in response")
	}
	id := u["id"].(string)

	// POST: every problem reported at once, with paths
	status, bad := c.do("POST", "/users", `{"email":"nope","password":"x","age":3}`)
	if status != 400 || strings.Join(issueCodes(bad), ",") != "email:invalid_email,password:too_small,age:too_small" {
		t.Fatal(status, bad)
	}

	// POST: missing and null are different codes
	_, bad = c.do("POST", "/users", `{"email":null}`)
	if got := strings.Join(issueCodes(bad), ","); got != "email:invalid_type,password:required,age:required" {
		t.Fatal(got)
	}

	// POST: malformed JSON, wrong content type, duplicate
	if status, _ = c.do("POST", "/users", `{"email":`); status != 400 {
		t.Fatal(status)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/users", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 415 {
		t.Fatal(res.StatusCode)
	}
	if status, bad = c.do("POST", "/users", `{"email":"me@example.com","password":"correct horse","age":30}`); status != 409 || bad["error"] != "email_taken" {
		t.Fatal(status, bad)
	}

	// GET list: query defaults and coercion
	status, list := c.do("GET", "/users", "")
	if status != 200 || list["page"] != float64(1) || list["limit"] != float64(20) || len(list["users"].([]any)) != 1 {
		t.Fatal(status, list)
	}
	if _, list = c.do("GET", "/users?limit=5&search=ME", ""); len(list["users"].([]any)) != 1 || list["limit"] != float64(5) {
		t.Fatal(list)
	}
	status, bad = c.do("GET", "/users?page=abc&limit=500", "")
	if status != 400 || strings.Join(issueCodes(bad), ",") != "page:invalid_value,limit:too_large" {
		t.Fatal(status, bad)
	}

	// GET one / path validation / not found
	if status, u = c.do("GET", "/users/"+id, ""); status != 200 || u["id"] != id {
		t.Fatal(status, u)
	}
	if status, bad = c.do("GET", "/users/not-a-uuid", ""); status != 400 || issueCodes(bad)[0] != "id:invalid_uuid" {
		t.Fatal(status, bad)
	}
	if status, _ = c.do("GET", "/users/123e4567-e89b-12d3-a456-426614174000", ""); status != 404 {
		t.Fatal(status)
	}

	// PATCH: absent vs set vs null
	if status, u = c.do("PATCH", "/users/"+id, `{}`); status != 200 || u["name"] != "" || u["bio"] != nil {
		t.Fatal("empty patch must change nothing:", status, u)
	}
	if _, u = c.do("PATCH", "/users/"+id, `{"name":"  Tobi ","bio":"hello"}`); u["name"] != "Tobi" || u["bio"] != "hello" {
		t.Fatal(u)
	}
	if _, u = c.do("PATCH", "/users/"+id, `{"name":"Renamed"}`); u["bio"] != "hello" {
		t.Fatal("bio must be untouched when absent:", u)
	}
	if _, u = c.do("PATCH", "/users/"+id, `{"bio":null}`); u["bio"] != nil || u["name"] != "Renamed" {
		t.Fatal("explicit null must clear bio only:", u)
	}
	if status, bad = c.do("PATCH", "/users/"+id, `{"name":null}`); status != 400 || issueCodes(bad)[0] != "name:invalid_type" {
		t.Fatal("null name is not allowed:", status, bad)
	}
	if status, _ = c.do("PATCH", "/users/"+id, `{"name":""}`); status != 400 {
		t.Fatal("empty name must be rejected")
	}
}

// If a handler ever sends a value that breaks the response schema, the client
// must get a generic 500 and none of the data.
func TestResponseContractViolationNeverReachesClient(t *testing.T) {
	type leaky struct {
		UserView
		Password string `json:"password"`
	}
	w := httptest.NewRecorder()
	respond(w, 200, UserViewSchema, leaky{UserView: UserView{ID: "123e4567-e89b-12d3-a456-426614174000", Email: "a@b.io"}, Password: "hunter2"})
	if w.Code != 500 || strings.Contains(w.Body.String(), "hunter2") {
		t.Fatal(w.Code, w.Body.String())
	}
}
