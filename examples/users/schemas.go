package main

import (
	"time"

	"github.com/google/uuid"
	validate "github.com/tobibamidele/validate"
)

// ---- requests ----

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

type ListUsers struct {
	Page   int
	Limit  int
	Search string
}

// Query strings are text, so numbers need an explicit Coerce().
var ListUsersSchema = validate.Object[ListUsers](
	validate.Field("page", validate.Int().Coerce().Min(1).Default(1)),
	validate.Field("limit", validate.Int().Coerce().Min(1).Max(100).Default(20)),
	validate.Field("search", validate.Optional(validate.String().Trim())),
)

// PatchUser distinguishes absent / null / set, which is what PATCH needs.
type PatchUser struct {
	Name validate.OptionalValue[string]                         // absent | set
	Bio  validate.OptionalValue[validate.NullableValue[string]] // absent | null | set
}

var PatchUserSchema = validate.Object[PatchUser](
	validate.Field("name", validate.Optional(validate.String().Trim().Min(1).Max(100))),
	validate.Field("bio", validate.Optional(validate.Nullable(validate.String().Max(500)))),
)

type UserID struct{ ID uuid.UUID }

var UserIDSchema = validate.Object[UserID](
	validate.Field("id", validate.UUID()),
)

// ---- responses ----
//
// The same schema machinery checks what we send. Strict() makes the schema an
// allow-list: if someone adds a Password field to UserView, responses fail
// (a generic 500, never sent) instead of leaking it.

type UserView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Bio       *string   `json:"bio"`
	Age       int       `json:"age"`
	CreatedAt time.Time `json:"created_at"`
}

var UserViewSchema = validate.Object[UserView](
	validate.Field("id", validate.String().UUID()),
	validate.Field("email", validate.String().Email()),
	validate.Field("name", validate.String()),
	validate.Field("bio", validate.Nullable(validate.String())),
	validate.Field("age", validate.Int()),
	validate.Field("created_at", validate.Time().Layout(time.RFC3339Nano)), // time.Time encodes to a string
).Strict()

type ListView struct {
	Users []UserView `json:"users"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}

var ListViewSchema = validate.Object[ListView](
	validate.Field("users", validate.Array(UserViewSchema)),
	validate.Field("page", validate.Int()),
	validate.Field("limit", validate.Int()),
).Strict()
