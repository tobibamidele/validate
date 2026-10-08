// Command acceptance is the spec's acceptance application (section 132). It
// exists so CI proves the documented usage compiles.
package main

import (
	"fmt"
	"net/http"

	validate "github.com/tobibamidele/validate"
)

type CreateUser struct {
	Email    string
	Password string
	Age      int
}

var CreateUserSchema = validate.Object[CreateUser](
	validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
	validate.Field("password", validate.String().Min(8).Max(128)),
	validate.Field("age", validate.Int().Min(13).Max(120)),
)

func createUser(w http.ResponseWriter, r *http.Request) {
	input, err := validate.JSON(r, CreateUserSchema)
	if err != nil {
		validate.WriteError(w, err)
		return
	}
	fmt.Println(input.Email, input.Age)
}

func main() {
	http.HandleFunc("/users", createUser)
	_ = http.ListenAndServe(":8080", nil)
}
