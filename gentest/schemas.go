// Package gentest holds deliberately awkward schemas used to prove that
// generated validators behave identically to the runtime. It is test support,
// not part of the public API.
package gentest

import (
	"strings"

	validate "github.com/tobibamidele/validate"
	"github.com/tobibamidele/validate/gen"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Base struct{ ID string }

type Geo struct {
	Lat, Lng float64
}

type Address struct {
	Street string
	City   string
	Geo    Geo
}

type Profile struct {
	Base                                                           // promoted: id -> Base.ID
	Email   string                                                 //
	Age     int                                                    // leaf with default
	Nick    string                                                 // Optional(String) unwrapped into a plain field
	Bio     *string                                                // Nullable(String) into a pointer
	Tags    []string                                               // array leaf, absent -> empty default
	Role    Role                                                   // Enum
	Address Address                                                // nested object, unrolled
	Home    *Address                                               // Optional(Object) into a pointer (leaf, then unwrap+pointer)
	Score   validate.OptionalValue[float64]                        // wrapper kept as-is
	Prev    validate.OptionalValue[validate.NullableValue[string]] // double wrapper kept as-is
	Level   int                                                    // transform leaf
}

var GeoSchema = validate.Object[Geo](
	validate.Field("lat", validate.Float64().Min(-90).Max(90)),
	validate.Field("lng", validate.Float64().Min(-180).Max(180)),
).Strict().Refine(func(g Geo) error {
	if g.Lat == 0 && g.Lng == 0 {
		return validate.Error("null_island", "coordinates must not be 0,0")
	}
	return nil
})

var AddressSchema = validate.Object[Address](
	validate.Field("street", validate.String().Trim().Min(1)),
	validate.Field("city", validate.String().Trim().Min(1)),
	validate.Field("geo", GeoSchema),
)

var levelSchema = validate.Transform(validate.String().Lower().OneOf("low", "high"), func(s string) (int, error) {
	if s == "high" {
		return 2, nil
	}
	return 1, nil
})

var ProfileSchema = validate.Object[Profile](
	validate.Field("id", validate.String().UUID()),
	validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
	validate.Field("age", validate.Int().Coerce().Min(13).Max(120).Default(18)),
	validate.Field("nick", validate.Optional(validate.String().Trim().Min(2))),
	validate.Field("bio", validate.Nullable(validate.String().Max(50))),
	validate.Field("tags", validate.Array(validate.String().Min(1)).Max(5).DefaultFunc(func() []string { return []string{} })),
	validate.Field("role", validate.Enum(RoleAdmin, RoleMember)),
	validate.Field("address", AddressSchema),
	validate.Field("home", validate.Optional(AddressSchema)),
	validate.Field("score", validate.Optional(validate.Float64().Min(0))),
	validate.Field("prev", validate.Optional(validate.Nullable(validate.String()))),
	validate.Field("level", levelSchema),
).Refine(func(p Profile) error {
	if strings.HasSuffix(p.Email, "@blocked.example") {
		return validate.IssueAt(validate.PathOf("email"), "blocked_domain", "domain is not allowed")
	}
	return nil
})

// StrictProfileSchema is the same shape, strict, with two refinements.
var StrictProfileSchema = validate.Object[Profile](
	validate.Field("id", validate.String().UUID()),
	validate.Field("email", validate.String().Trim().Lower().Email()),
	validate.Field("age", validate.Int().Min(13)),
	validate.Field("nick", validate.Optional(validate.String())),
	validate.Field("bio", validate.Nullable(validate.String())),
	validate.Field("tags", validate.Array(validate.String())),
	validate.Field("role", validate.Enum(RoleAdmin, RoleMember)),
	validate.Field("address", AddressSchema),
	validate.Field("home", validate.Optional(AddressSchema)),
	validate.Field("score", validate.Optional(validate.Float64())),
	validate.Field("prev", validate.Optional(validate.Nullable(validate.String()))),
	validate.Field("level", levelSchema),
).Strict().
	Refine(func(p Profile) error {
		if p.Age > 100 && p.Role == RoleAdmin {
			return validate.Error("too_old_admin", "admins must be 100 or younger")
		}
		return nil
	}).
	Refine(func(p Profile) error { return validate.Error("always", "second refinement") })

// Targets lists every schema that gets a generated validator. Both the
// generator command and the up-to-date test read it.
func Targets() gen.Config {
	return gen.Config{Package: "gentest", Targets: []gen.Target{
		{Name: "ProfileFast", Expr: "ProfileSchema", Schema: ProfileSchema},
		{Name: "StrictProfileFast", Expr: "StrictProfileSchema", Schema: StrictProfileSchema},
		{Name: "GeoFast", Expr: "GeoSchema", Schema: GeoSchema},
	}}
}
