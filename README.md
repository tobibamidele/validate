# validate

Typed, schema-first validation and decoding for Go. No struct tags, no
reflection on the request path beyond assignment, and `Schema[T]` keeps your
output type.

```go
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

func create(w http.ResponseWriter, r *http.Request) {
    input, err := validate.JSON(r, CreateUserSchema) // input is a CreateUser
    if err != nil {
        validate.WriteError(w, err)
        return
    }
    _ = input
}
```

A complete runnable API (create, list with query strings, PATCH with
absent/null/set semantics, path parameters, response validation) lives in
[`examples/users`](examples/users); `go run ./examples/users` starts it, and its
end-to-end test runs in CI.

## Semantics

| Topic | Behaviour |
|---|---|
| Coercion | Never implicit. `Int().Parse("18")` fails; `Int().Coerce()` accepts it. Same for `Bool().Coerce()` (`"true"`/`"false"` only) and floats. |
| Absent / null / zero | Distinct. `Optional` accepts absence, `Nullable` accepts null, `Optional(Nullable(x))` both. Null into `Optional(String())` is an error. |
| Defaults | Apply to absence only. `0`, `false`, `""` and `null` are kept/rejected, never replaced. Defaults still flow through normalisers and constraints. |
| Pipeline | type check → normalisers (`Trim`, `Lower`, `Upper`) → constraints (declaration order) → refinements. A wrong type yields only `invalid_type`. |
| String length | Unicode code points (runes), not bytes. |
| JSON numbers | Decoded with `UseNumber`, so `9007199254740993` is exact. `18.5`, `18.0`, `1e2` are rejected for integers; overflow is rejected, never wrapped. |
| Floats | NaN and ±Inf are always rejected. |
| Unknown keys | Stripped by default; `.Strict()` reports `unknown_key`. There is no "allow" mode: a fixed struct can't keep unknown keys. |
| Errors | `Issue{Path, Code, Message, Expected, Received, Metadata}`. Paths are structured (`["items", 1, "quantity"]`). Order is deterministic. `Issue.Value` is empty unless `WithValues()` and is never serialised. |
| Schema bugs | Invalid regex, empty enum, duplicate/ambiguous/unassignable fields **panic when the schema is built** (like `regexp.MustCompile`), not on requests. |
| Concurrency | Schemas are immutable (copy-on-write builders) and safe to share. |

### Optional, Nullable and PATCH

```go
type UpdateUser struct {
    Name validate.OptionalValue[string]                              // absent | set
    Bio  validate.OptionalValue[validate.NullableValue[string]]      // absent | null | set
}
var UpdateSchema = validate.Object[UpdateUser](
    validate.Field("name", validate.Optional(validate.String())),
    validate.Field("bio", validate.Optional(validate.Nullable(validate.String()))),
)
```

An `Optional`/`Nullable` output can also land in a plain field (`string`) or a
pointer (`*string`) when you don't need the distinction; absent/null leave the
zero value / nil. Pointer fields are never assumed nullable.

### Field naming

`Field("email_address", ...)` maps to `EmailAddress` by exact Go name, then by
normalised name (case, `_`, `-` ignored). Ambiguity panics. Promoted fields of
embedded structs are reachable by name; `Field("base", BaseSchema)` is a nested
object, never flattening.

## Transports

```go
validate.JSON(r, schema, opts...)           // body: 1 MiB default cap, trailing data/empty body rejected
validate.FromJSON(bytes, schema)            // same strictness, no http.Request
validate.Query(r, schema)                   // use .Coerce() for numbers/bools
validate.PathValues(map[string]string, s)   // router-extracted params
validate.Headers(r.Header, schema)          // case-insensitive; "user_agent" matches User-Agent
validate.Cookies(r, schema)
```

Query/path/header/cookie take an `Object` schema. Array fields receive every
occurrence (a single `?tag=go` still gives `[]string{"go"}`); a *scalar* field
that is repeated is rejected rather than silently first-wins.

JSON options: `WithMaxBodyBytes`, `RequireJSONContentType`, `IgnoreContentType`,
`AllowEmptyBody`, `WithParseOptions`. Default Content-Type policy: if sent, it
must be JSON; if missing, accepted.

`WriteError` returns `400` with `{error, message, issues}` for validation
errors, the right status for `*RequestError` (413/415), and a generic `500`
otherwise. `WriteErrorWith` changes status/shape.

## Discriminated unions

Polymorphic payloads get a typed result. Pick an interface as the output type;
each variant converts its struct into it:

```go
var EventSchema = validate.Discriminated[Event]("type",
    validate.Variant("created", CreatedSchema, func(v Created) Event { return v }),
    validate.Variant("deleted", DeletedSchema, func(v Deleted) Event { return v }),
)

ev, err := validate.JSON(r, EventSchema) // ev is an Event holding a Created or Deleted
```

Only the selected variant is validated, so errors are about its real fields
(`reason: too_small`) rather than "matches no alternative". A missing tag is
`required` at the tag's path, a non-string tag is `invalid_type`, and an
unknown tag is `invalid_union` with the allowed tags in metadata. The tag key
stays in the object handed to the variant, so a `Strict()` variant must declare
it. Use `Union` only for genuinely untyped alternatives (it returns `any`).

## Recursive schemas

Trees, comment threads and ASTs refer to themselves. A package-level variable
that mentions itself is a Go *initialization cycle* (even through a closure),
so `Recursive` hands the builder a reference to the schema being defined:

```go
type Comment struct {
    Body    string
    Replies []Comment
}

var CommentSchema = validate.Recursive(func(self validate.Schema[Comment]) validate.Schema[Comment] {
    return validate.Object[Comment](
        validate.Field("body", validate.String().Trim().Min(1)),
        validate.Field("replies", validate.Array(self).DefaultFunc(func() []Comment { return nil })),
    )
})
```

Error paths run straight through the recursion: `replies[1].replies[0].body`.
`Optional(self)` into a `*T` field gives linked structures. For schemas that
reference each other, build them inside a function and use
`validate.Lazy(func() validate.Schema[T] { return other })` (resolved once,
safe for concurrent use).

**Depth limit.** Every nested `Recursive`/`Lazy` level counts toward a limit of
128 (`validate.DefaultMaxDepth`); exceeding it reports `too_deep` instead of
overflowing the stack. This is what stops a 5,000-level hostile JSON body and
Go values that contain cycles. Width is free: 500 siblings use depth 1. Raise
or lower it per call with `validate.WithMaxDepth(n)` (via `ParseContext`, or
`WithParseOptions` on the HTTP helpers).

## Response validation

The same schema can check what you are about to send:

```go
if err := validate.RespondJSON(w, http.StatusOK, UserViewSchema, user); err != nil {
    log.Printf("response contract violated: %v", err) // *validate.ResponseError
}
```

`RespondJSON` encodes the value once, validates those exact bytes, and writes
them, so what was checked is what is sent. A violation writes nothing of the
value: the client gets a generic `500`, and you get a `*ResponseError` to log.
It is deliberately **not** a `*ValidationError`, because a bad response is a
server bug, not bad client input, and `WriteError` never exposes its issues.
`validate.JSONResponse(schema, value)` is the check without the writing.

- A `Strict()` schema doubles as an allow-list, which catches the classic leak
  of a field that must never be sent (`password`, tokens, internal ids).
- The value is encoded with `encoding/json`, so your `json` tags must produce
  the schema's field names. A mismatch is reported, not ignored.
- Normalisers (`Trim`, `Lower`) and `Coerce` still apply: this proves the value
  *would be accepted*, not that it is already normalised.
- `time.Time` encodes to a string, so use `Time().Layout(time.RFC3339Nano)`;
  `[]byte` encodes to base64, so use `Bytes().Base64()`.

## Generated validators

`validate/gen` generates the *structure* of Object validators (the field loop,
assignment into the struct, unknown keys, refinements) without reflection.
Every leaf still runs on the schema's own runtime engine, so there is no second
implementation of any rule to drift: the two paths differ only in how objects
are walked.

```go
//go:build ignore

package main

func main() {
    src, _ := gen.Generate(gen.Config{Package: "api", Targets: []gen.Target{
        {Name: "CreateUserFast", Expr: "CreateUserSchema", Schema: api.CreateUserSchema},
    }})
    os.WriteFile("validators_gen.go", src, 0o644)
}
```

Then use `CreateUserFast` anywhere a `Schema` is accepted. It embeds a
fingerprint of the schema's structure; if the schema changes and the file is
not regenerated, the fingerprint no longer matches and `validate.Generated`
**falls back to the runtime schema** (same results, just slower) instead of
running stale code. Make CI fail on that with a one-line test:

```go
if !api.CreateUserFast.Active() { t.Fatal("run go generate") }
```

Only the object layer is generated; nested objects are unrolled inline.
Embedded pointers (`struct{ *Base }`) are rejected with a clear error.
Generated validators are checked against the runtime by differential tests
(20,000 random valid and invalid inputs x 2 schemas x 4 option sets, plus a
fuzzer) and by mutation: breaking the generated code in several ways is caught.
On the test fixture they allocate ~38% less (47 vs 76) and run ~25% faster;
expect a modest win, since leaves are shared by design.

## Other building blocks

`Array`, `Map`, `Enum` (type preserving), `Discriminated`, `Recursive`, `Lazy`, `Union` (returns `any`),
`Transform[A,B]`, `Refine`, `Custom`, `AsNode`, `ParseContext(ctx, schema, v,
WithAbortEarly(), WithValues())`.

## Lathe

`github.com/tobibamidele/validate/lathe` is a **separate Go module** so the root
module never depends on Lathe. Lathe's generated `Column[T]` carries only table
and column names, so the adapter reads definitions from the `schema.Snapshot`
your Lathe schema exports:

```go
cat := validatelathe.NewCatalog(exp.Snapshot)
email := validatelathe.String(cat, db.Users.Email).Trim().Lower().Email() // VarChar(255) -> Max(255)
status := validatelathe.Enum(cat, db.Users.Status)                        // Enum(...) -> Enum(...)
```

Only safe metadata maps (VarChar length, enum values, Go type). Unique, FKs,
indexes, primary keys and DB defaults are never validation, and nothing
queries a database.

## Development

```
go test -race ./...                  # root module
(cd lathe && go test -race ./...)    # adapter (uses `replace ../` locally)
go test -run xxx -fuzz FuzzFromJSON -fuzztime 30s .
go generate ./...                    # regenerates boilerplate.go (python3)
go run ./gentest/cmd/gen             # regenerates gentest/validators_gen.go
```
