package validate_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	validate "github.com/tobibamidele/validate"
)

type exampleUser struct {
	Email string
	Age   int
}

var exampleSchema = validate.Object[exampleUser](
	validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
	validate.Field("age", validate.Int().Min(13).Max(120)),
)

func Example() {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"email":"  Me@Example.COM ","age":7}`))
	r.Header.Set("Content-Type", "application/json")

	_, err := validate.JSON(r, exampleSchema)
	w := httptest.NewRecorder()
	validate.WriteError(w, err)
	fmt.Println(w.Code)
	fmt.Println(err)
	// Output:
	// 400
	// validation failed: age: too_small
}

func ExampleOptional() {
	type Patch struct {
		Name validate.OptionalValue[string]
	}
	s := validate.Object[Patch](validate.Field("name", validate.Optional(validate.String())))

	absent, _ := s.Parse(map[string]any{})
	empty, _ := s.Parse(map[string]any{"name": ""})
	fmt.Println(absent.Name.IsSet(), empty.Name.IsSet())
	// Output: false true
}

func ExampleQuery() {
	type Page struct{ Page, Limit int }
	s := validate.Object[Page](
		validate.Field("page", validate.Int().Coerce().Min(1).Default(1)),
		validate.Field("limit", validate.Int().Coerce().Min(1).Max(100).Default(20)),
	)
	r, _ := http.NewRequest("GET", "/?limit=50", nil)
	p, err := validate.Query(r, s)
	fmt.Println(p, err)
	// Output: {1 50} <nil>
}
