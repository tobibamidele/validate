package validate_test

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	validate "github.com/tobibamidele/validate"
)

type UserView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	Password  string    `json:"password,omitempty"`
}

var UserViewSchema = validate.Object[UserView](
	validate.Field("id", validate.String().UUID()),
	validate.Field("email", validate.String().Email()),
	validate.Field("role", validate.Enum(Admin, Member)),
	validate.Field("created_at", validate.Time().Layout(time.RFC3339Nano)),
).Strict()

func goodView() UserView {
	return UserView{ID: "123e4567-e89b-12d3-a456-426614174000", Email: "a@b.io", Role: Admin, CreatedAt: time.Now()}
}

func TestJSONResponseAcceptsConformingValue(t *testing.T) {
	if err := validate.JSONResponse(UserViewSchema, goodView()); err != nil {
		t.Fatal(err)
	}
	if err := validate.JSONResponse(UserViewSchema, &UserView{ID: goodView().ID, Email: "a@b.io", Role: Member, CreatedAt: time.Now()}); err != nil {
		t.Fatal("pointer values should work:", err)
	}
}

func TestJSONResponseCatchesContractBugs(t *testing.T) {
	bad := goodView()
	bad.Role = "superuser"
	bad.Email = "not-an-email"
	err := validate.JSONResponse(UserViewSchema, bad)
	re, ok := validate.AsResponseError(err)
	if !ok {
		t.Fatalf("want *ResponseError, got %T %v", err, err)
	}
	var got []string
	for _, i := range re.Issues {
		got = append(got, i.Path.String()+":"+i.Code)
	}
	if !reflect.DeepEqual(got, []string{"email:invalid_email", "role:invalid_enum"}) {
		t.Fatal(got)
	}
	if validate.IsValidationError(err) {
		t.Fatal("a response failure must not look like a client validation error")
	}
	if !strings.HasPrefix(err.Error(), "response validation failed: email: invalid_email") {
		t.Fatal(err.Error())
	}
}

func TestJSONResponseLeakedFieldWithStrict(t *testing.T) {
	// The classic bug: a field that must never be sent. Strict makes the
	// schema reject it, and the schema doubles as an allow-list.
	leaky := goodView()
	leaky.Password = "hunter2"
	err := validate.JSONResponse(UserViewSchema, leaky)
	re, _ := validate.AsResponseError(err)
	if re == nil || len(re.Issues) != 1 || re.Issues[0].Code != "unknown_key" || re.Issues[0].Path.String() != "password" {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatal("value leaked into the error")
	}
}

func TestJSONResponseTagMismatchIsReported(t *testing.T) {
	type Untagged struct{ Email string } // marshals as "Email"; schema says "email"
	s := validate.Object[Untagged](validate.Field("email", validate.String()))
	if err := validate.JSONResponse(s, Untagged{Email: "x"}); err == nil {
		t.Fatal("encoded field name differs from the contract; must be reported")
	}
}

func TestJSONResponseUnencodable(t *testing.T) {
	err := validate.JSONResponse(validate.Any(), make(chan int))
	re, ok := validate.AsResponseError(err)
	if !ok || len(re.Issues) != 0 || re.Unwrap() == nil {
		t.Fatal(err)
	}
}

func TestRespondJSONWritesValidatedBytes(t *testing.T) {
	w := httptest.NewRecorder()
	if err := validate.RespondJSON(w, 201, UserViewSchema, goodView()); err != nil {
		t.Fatal(err)
	}
	var back UserView
	if w.Code != 201 || w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &back) != nil || back.Email != "a@b.io" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestRespondJSONNeverSendsInvalidBody(t *testing.T) {
	leaky := goodView()
	leaky.Password = "hunter2"
	w := httptest.NewRecorder()
	err := validate.RespondJSON(w, 200, UserViewSchema, leaky)
	if _, ok := validate.AsResponseError(err); !ok {
		t.Fatal(err)
	}
	if w.Code != 500 || strings.Contains(w.Body.String(), "hunter2") || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("client must see only a generic 500, got %d %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "internal_error") {
		t.Fatal(w.Body.String())
	}
}

func TestResponseErrorThroughWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	validate.WriteError(w, &validate.ResponseError{Issues: []validate.Issue{{Path: validate.PathOf("x"), Code: "c"}}})
	if w.Code != 500 || strings.Contains(w.Body.String(), "\"x\"") {
		t.Fatal(w.Code, w.Body.String())
	}
}
