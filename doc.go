// Package validate is a typed, schema-first validation and decoding library.
//
// A schema is ordinary Go code that describes the shape of external data and
// produces a statically typed value:
//
//	var CreateUserSchema = validate.Object[CreateUser](
//	    validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
//	    validate.Field("age", validate.Int().Min(13)),
//	)
//
//	user, err := CreateUserSchema.Parse(input) // user is a CreateUser
//
// # Semantics worth knowing
//
//   - Schemas are immutable once built; every fluent method returns a copy, so
//     schemas are safe to share and to use from many goroutines.
//   - There is no implicit coercion. Int().Parse("18") fails; Int().Coerce()
//     accepts it. Coerce is explicit on purpose, and is the way query strings,
//     headers and cookies are turned into numbers and booleans.
//   - Absent, null and zero values are three different things. Optional accepts
//     absence, Nullable accepts null, and defaults apply only to absence.
//   - Length constraints on strings count Unicode code points (runes), not bytes.
//   - Validation collects every independent issue, in a deterministic order
//     (object fields in declaration order, array indexes ascending, map keys
//     sorted). WithAbortEarly stops at the first one.
//   - Schema definition mistakes (an invalid regex, an empty enum, a field that
//     cannot be assigned to the target struct) panic when the schema is built,
//     like regexp.MustCompile, never on the request path.
//   - Issue.Value (the offending raw input) is not populated unless WithValues
//     is passed, and is never serialised to JSON.
//
// The core never imports Lathe. HTTP helpers live in http*.go and are thin
// wrappers around the same engine; the Lathe bridge is the separate module in
// the lathe/ directory.
package validate
