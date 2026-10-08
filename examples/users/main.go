// Command users is a small runnable API showing validate end to end:
// request bodies, query strings, path parameters, PATCH semantics and
// response validation.
//
//	go run ./examples/users
//
//	curl -i -X POST localhost:8080/users -d '{"email":"Me@Example.com","password":"correct horse","age":30}' -H 'Content-Type: application/json'
//	curl -i -X POST localhost:8080/users -d '{"email":"nope","password":"x","age":3}'  -H 'Content-Type: application/json'
//	curl -i 'localhost:8080/users?limit=5&search=me'
//	curl -i -X PATCH localhost:8080/users/<id> -d '{"bio":null}' -H 'Content-Type: application/json'
package main

import (
	"errors"
	"log"
	"net/http"

	validate "github.com/tobibamidele/validate"
)

type server struct{ db *store }

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", s.create)
	mux.HandleFunc("GET /users", s.list)
	mux.HandleFunc("GET /users/{id}", s.get)
	mux.HandleFunc("PATCH /users/{id}", s.patch)
	return mux
}

func main() {
	s := &server{db: newStore()}
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", s.routes()))
}

func view(u user) UserView {
	return UserView{ID: u.ID.String(), Email: u.Email, Name: u.Name, Bio: u.Bio, Age: u.Age, CreatedAt: u.CreatedAt}
}

// respond validates the outgoing value against its schema. A violation is a
// server bug: the client gets a generic 500 and we log the details.
func respond[T any](w http.ResponseWriter, status int, schema validate.Schema[T], v any) {
	if err := validate.RespondJSON(w, status, schema, v); err != nil {
		log.Printf("response error: %v", err)
	}
}

func writeProblem(w http.ResponseWriter, status int, code, msg string) {
	validate.WriteErrorWith(w, &validate.RequestError{Status: status, Code: code, Message: msg}, validate.WriteOptions{})
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	in, err := validate.JSON(r, CreateUserSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	u, err := s.db.create(in)
	if errors.Is(err, errEmailTaken) {
		writeProblem(w, http.StatusConflict, "email_taken", "email already registered")
		return
	}
	respond(w, http.StatusCreated, UserViewSchema, view(u))
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	q, err := validate.Query(r, ListUsersSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	out := ListView{Users: []UserView{}, Page: q.Page, Limit: q.Limit}
	for _, u := range s.db.list(q.Search, q.Page, q.Limit) {
		out.Users = append(out.Users, view(u))
	}
	respond(w, http.StatusOK, ListViewSchema, out)
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	p, err := validate.PathValues(map[string]string{"id": r.PathValue("id")}, UserIDSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	u, ok := s.db.get(p.ID)
	if !ok {
		writeProblem(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	respond(w, http.StatusOK, UserViewSchema, view(u))
}

func (s *server) patch(w http.ResponseWriter, r *http.Request) {
	p, err := validate.PathValues(map[string]string{"id": r.PathValue("id")}, UserIDSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	in, err := validate.JSON(r, PatchUserSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	u, ok := s.db.patch(p.ID, in)
	if !ok {
		writeProblem(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	respond(w, http.StatusOK, UserViewSchema, view(u))
}
