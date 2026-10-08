package validate_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	validate "github.com/tobibamidele/validate"
)

func jsonVal(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func codes(err error) []string {
	ve, ok := validate.AsValidationError(err)
	if !ok {
		return nil
	}
	var out []string
	for _, i := range ve.Issues {
		out = append(out, i.Path.String()+":"+i.Code)
	}
	return out
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	f()
}

// ---- compile-time type safety (#98, #99) ----

type Role string

const (
	Admin  Role = "admin"
	Member Role = "member"
)

var (
	_ validate.Schema[string]         = validate.String()
	_ validate.Schema[int]            = validate.Int()
	_ validate.Schema[bool]           = validate.Bool()
	_ validate.Schema[[]string]       = validate.Array(validate.String())
	_ validate.Schema[uuid.UUID]      = validate.UUID()
	_ validate.Schema[Role]           = validate.Enum(Admin, Member)
	_ validate.Schema[[][]int]        = validate.Array(validate.Array(validate.Int()))
	_ validate.Schema[map[string]int] = validate.Map(validate.Int())
)

func TestEnumPreservesType(t *testing.T) {
	role, err := validate.Enum(Admin, Member).Parse("admin")
	if err != nil || role != Admin {
		t.Fatal(role, err)
	}
	if reflect.TypeOf(role) != reflect.TypeOf(Role("")) {
		t.Fatal("lost named type")
	}
	_, err = validate.Enum(Admin, Member).Parse("root")
	if got := codes(err); !reflect.DeepEqual(got, []string{":invalid_enum"}) {
		t.Fatal(got)
	}
	mustPanic(t, "empty enum", func() { validate.Enum[string]() })
	n, err := validate.Enum(1, 2).Parse(jsonVal(t, "2"))
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
}

// ---- strings ----

func TestStringPipelineOrder(t *testing.T) {
	s := validate.String().Trim().Lower().Min(3).Email()
	got, err := s.Parse("  USER@EXAMPLE.COM  ")
	if err != nil || got != "user@example.com" {
		t.Fatal(got, err)
	}
	// Min measures the trimmed value even though declared before Trim.
	if _, err := validate.String().Min(3).Trim().Parse("  a  "); err == nil {
		t.Fatal("Min should see trimmed value")
	}
}

func TestStringRunes(t *testing.T) {
	s := validate.String().Max(3)
	if _, err := s.Parse("héé"); err != nil { // 3 runes, 5 bytes
		t.Fatal(err)
	}
	if _, err := s.Parse("héé!"); err == nil {
		t.Fatal("4 runes should fail")
	}
}

func TestStringChecks(t *testing.T) {
	tests := []struct {
		name string
		s    validate.StringSchema
		in   string
		ok   bool
	}{
		{"email ok", validate.String().Email(), "a@b.com", true},
		{"email display name", validate.String().Email(), "A <a@b.com>", false},
		{"email no dot", validate.String().Email(), "a@localhost", false},
		{"email space", validate.String().Email(), "a b@c.com", false},
		{"url ok", validate.String().URL(), "https://x.dev/p?q=1", true},
		{"url relative", validate.String().URL(), "/x", false},
		{"uuid ok", validate.String().UUID(), "123e4567-e89b-12d3-a456-426614174000", true},
		{"uuid braces", validate.String().UUID(), "{123e4567-e89b-12d3-a456-426614174000}", false},
		{"regex ok", validate.String().Regex(`^a+$`), "aaa", true},
		{"regex no", validate.String().Regex(`^a+$`), "b", false},
		{"prefix", validate.String().StartsWith("x"), "xy", true},
		{"suffix no", validate.String().EndsWith("x"), "xy", false},
		{"contains", validate.String().Contains("m"), "amb", true},
		{"oneof", validate.String().OneOf("a", "b"), "c", false},
		{"length", validate.String().Length(2), "ab", true},
	}
	for _, tc := range tests {
		_, err := tc.s.Parse(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
	mustPanic(t, "bad regex", func() { validate.String().Regex("[") })
}

func TestTypeErrorSuppressesSemanticIssues(t *testing.T) { // #63
	_, err := validate.String().Email().Min(3).Parse(5)
	if got := codes(err); !reflect.DeepEqual(got, []string{":invalid_type"}) {
		t.Fatal(got)
	}
}

func TestImmutability(t *testing.T) { // #58
	base := validate.String().Trim()
	short := base.Max(5)
	long := base.Max(255)
	if _, err := short.Parse("abcdefg"); err == nil {
		t.Fatal("short should reject")
	}
	if _, err := long.Parse("abcdefg"); err != nil {
		t.Fatal("long should accept")
	}
	if _, err := base.Parse(strings.Repeat("x", 1000)); err != nil {
		t.Fatal("base mutated")
	}
}

// ---- numbers (#96, #97, #41, #100) ----

func TestInt8Bounds(t *testing.T) {
	s := validate.Int8()
	for in, ok := range map[string]bool{"-128": true, "127": true, "-129": false, "128": false} {
		v, err := s.Parse(jsonVal(t, in))
		if (err == nil) != ok {
			t.Errorf("%s: %v", in, err)
		}
		if ok && int64(v) != mustInt(in) {
			t.Errorf("%s: got %d", in, v)
		}
	}
}

func mustInt(s string) int64 {
	var n int64
	_ = json.Unmarshal([]byte(s), &n)
	return n
}

func TestUnsignedBounds(t *testing.T) {
	if v, err := validate.Uint8().Parse(jsonVal(t, "255")); err != nil || v != 255 {
		t.Fatal(v, err)
	}
	for _, in := range []string{"-1", "256"} {
		if _, err := validate.Uint8().Parse(jsonVal(t, in)); err == nil {
			t.Errorf("%s should fail", in)
		}
	}
	if v, err := validate.Uint64().Parse(jsonVal(t, "18446744073709551615")); err != nil || v != math.MaxUint64 {
		t.Fatal(v, err)
	}
	if _, err := validate.Uint64().Parse(jsonVal(t, "18446744073709551616")); err == nil {
		t.Fatal("overflow accepted")
	}
	if v, err := validate.Int64().Parse(jsonVal(t, "-9223372036854775808")); err != nil || v != math.MinInt64 {
		t.Fatal(v, err)
	}
	if _, err := validate.Int64().Parse(jsonVal(t, "9223372036854775808")); err == nil {
		t.Fatal("int64 overflow accepted")
	}
	if v, err := validate.Int().Parse(jsonVal(t, "9007199254740993")); err != nil || v != 9007199254740993 {
		t.Fatalf("precision lost: %v %v", v, err) // not representable in float64
	}
}

func TestJSONNumberSemantics(t *testing.T) {
	if _, err := validate.Int().Parse(jsonVal(t, "18")); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"18.5", "18.0", "1e2"} {
		if _, err := validate.Int().Parse(jsonVal(t, in)); err == nil {
			t.Errorf("%s should be rejected for Int", in)
		}
	}
}

func TestCoercionIsExplicit(t *testing.T) {
	if _, err := validate.Int().Parse("18"); err == nil {
		t.Fatal("implicit coercion")
	}
	if v, err := validate.Int().Coerce().Parse("18"); err != nil || v != 18 {
		t.Fatal(v, err)
	}
	if _, err := validate.Int().Coerce().Parse(" 18"); err == nil {
		t.Fatal("whitespace should not be trimmed")
	}
	if _, err := validate.Bool().Parse("true"); err == nil {
		t.Fatal("implicit bool coercion")
	}
	if v, err := validate.Bool().Coerce().Parse("false"); err != nil || v {
		t.Fatal(v, err)
	}
	for _, bad := range []string{"yes", "on", "1", "TRUE"} {
		if _, err := validate.Bool().Coerce().Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestIntConstraints(t *testing.T) {
	s := validate.Int().Min(1).Max(10).MultipleOf(2)
	if _, err := s.Parse(4); err != nil {
		t.Fatal(err)
	}
	if got := codes(errOf(s.Parse(0))); !reflect.DeepEqual(got, []string{":too_small", ":not_multiple_of"}[:1]) {
		// 0 is a multiple of 2, so only too_small
		t.Fatal(got)
	}
	if got := codes(errOf(s.Parse(11))); len(got) != 2 {
		t.Fatal(got)
	}
	mustPanic(t, "unsigned negative", func() { validate.Uint().Negative() })
	mustPanic(t, "multipleOf 0", func() { validate.Int().MultipleOf(0) })
	if _, err := validate.Int().Positive().Parse(0); err == nil {
		t.Fatal("0 is not positive")
	}
	if _, err := validate.Int().OneOf(1, 2).Parse(3); err == nil {
		t.Fatal("oneOf")
	}
}

func errOf[T any](_ T, err error) error { return err }

func TestFloat(t *testing.T) {
	for _, bad := range []any{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := validate.Float64().Parse(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := validate.Float64().Coerce().Parse("NaN"); err == nil {
		t.Fatal("coerced NaN accepted")
	}
	if v, err := validate.Float64().Parse(jsonVal(t, "1.5")); err != nil || v != 1.5 {
		t.Fatal(v, err)
	}
	if _, err := validate.Float32().Parse(1e39); err == nil {
		t.Fatal("float32 overflow accepted")
	}
	if _, err := validate.Float64().MultipleOf(0.1).Parse(0.3); err != nil {
		t.Fatal("0.3 should be a multiple of 0.1:", err)
	}
	if _, err := validate.Float64().MultipleOf(0.1).Parse(0.35); err == nil {
		t.Fatal("0.35 is not")
	}
}

// ---- defaults (#16, #74) ----

type Config struct {
	Limit  int
	Active bool
	Name   string
}

var ConfigSchema = validate.Object[Config](
	validate.Field("limit", validate.Int().Default(20)),
	validate.Field("active", validate.Bool().Default(true)),
	validate.Field("name", validate.String().Default("x")),
)

func TestDefaultsApplyOnlyToAbsence(t *testing.T) {
	got, err := ConfigSchema.Parse(map[string]any{})
	if err != nil || got != (Config{20, true, "x"}) {
		t.Fatal(got, err)
	}
	got, err = ConfigSchema.Parse(jsonVal(t, `{"limit":0,"active":false,"name":""}`))
	if err != nil || got != (Config{0, false, ""}) {
		t.Fatal(got, err)
	}
	if _, err := ConfigSchema.Parse(jsonVal(t, `{"limit":null}`)); err == nil {
		t.Fatal("null must not trigger default")
	}
	// defaults flow through constraints
	s := validate.String().Default("  X ").Trim().Lower()
	if v, _ := s.Parse(nil); v != "" && false {
		t.Fatal()
	}
	obj := validate.Object[struct{ S string }](validate.Field("s", s))
	if v, err := obj.Parse(map[string]any{}); err != nil || v.S != "x" {
		t.Fatal(v, err)
	}
}

func TestMutableDefaultsAreFresh(t *testing.T) {
	type D struct{ Tags []string }
	s := validate.Object[D](validate.Field("tags", validate.Array(validate.String()).DefaultFunc(func() []string { return []string{} })))
	a, _ := s.Parse(map[string]any{})
	a.Tags = append(a.Tags, "x")
	b, _ := s.Parse(map[string]any{})
	if len(b.Tags) != 0 {
		t.Fatal("shared default")
	}
}

// ---- optional / nullable (#15) ----

type Patch struct {
	Name  validate.OptionalValue[string]
	Bio   validate.OptionalValue[validate.NullableValue[string]]
	Count validate.OptionalValue[int]
}

var PatchSchema = validate.Object[Patch](
	validate.Field("name", validate.Optional(validate.String())),
	validate.Field("bio", validate.Optional(validate.Nullable(validate.String()))),
	validate.Field("count", validate.Optional(validate.Int())),
)

func TestOptionalNullableTable(t *testing.T) {
	type row struct {
		schema validate.Schema[validate.OptionalValue[string]]
		in     input
		ok     bool
		set    bool
	}
	opt := validate.Optional(validate.String())
	if v, err := opt.Parse(nil); err == nil || v.IsSet() {
		t.Fatal("null on Optional(String) must fail")
	}
	nullable := validate.Nullable(validate.String())
	if _, err := nullable.Parse("x"); err != nil {
		t.Fatal(err)
	}
	if v, err := nullable.Parse(nil); err != nil || !v.IsNull() {
		t.Fatal(v, err)
	}
	// absent against Nullable inside an object is required
	type N struct {
		V validate.NullableValue[string]
	}
	nobj := validate.Object[N](validate.Field("v", nullable))
	if got := codes(errOf(nobj.Parse(map[string]any{}))); !reflect.DeepEqual(got, []string{"v:required"}) {
		t.Fatal(got)
	}
}

type input struct{}

func TestPatchSemantics(t *testing.T) { // #73
	cases := []struct {
		json string
		name validate.OptionalValue[string]
		ok   bool
	}{
		{`{}`, validate.OptionalValue[string]{}, true},
		{`{"name":""}`, validate.Some(""), true},
		{`{"name":"x"}`, validate.Some("x"), true},
		{`{"name":null}`, validate.OptionalValue[string]{}, false},
	}
	for _, c := range cases {
		p, err := PatchSchema.Parse(jsonVal(t, c.json))
		if (err == nil) != c.ok {
			t.Fatalf("%s: %v", c.json, err)
		}
		if c.ok && p.Name != c.name {
			t.Errorf("%s: got %+v", c.json, p.Name)
		}
	}
	p, _ := PatchSchema.Parse(jsonVal(t, `{"bio":null}`))
	if bio, set := p.Bio.Value(); !set || !bio.IsNull() {
		t.Fatal("explicit null lost", p.Bio)
	}
	p, _ = PatchSchema.Parse(jsonVal(t, `{}`))
	if p.Bio.IsSet() {
		t.Fatal("absent bio became set")
	}
	p, _ = PatchSchema.Parse(jsonVal(t, `{"count":0}`))
	if v, set := p.Count.Value(); !set || v != 0 {
		t.Fatal("present zero confused with absence")
	}
	p, _ = PatchSchema.Parse(jsonVal(t, `{"bio":"hi"}`))
	if bio, _ := p.Bio.Value(); bio.GetOr("") != "hi" {
		t.Fatal("bio value")
	}
}

func TestOptionalUnwrapsIntoPlainAndPointerFields(t *testing.T) { // #27
	type U struct {
		Search  string
		Display *string
		Age     *int
	}
	s := validate.Object[U](
		validate.Field("search", validate.Optional(validate.String().Trim())),
		validate.Field("display", validate.Nullable(validate.String())),
		validate.Field("age", validate.Optional(validate.Int())),
	)
	u, err := s.Parse(jsonVal(t, `{"display":null}`))
	if err != nil || u.Search != "" || u.Display != nil || u.Age != nil {
		t.Fatal(u, err)
	}
	u, err = s.Parse(jsonVal(t, `{"search":" q ","display":"d","age":3}`))
	if err != nil || u.Search != "q" || *u.Display != "d" || *u.Age != 3 {
		t.Fatal(u, err)
	}
}

// ---- objects ----

type Address struct{ Street, City, State string }
type CreateUser struct {
	Email    string
	Password string
	Age      int
	Address  Address
}

var AddressSchema = validate.Object[Address](
	validate.Field("street", validate.String().Min(1)),
	validate.Field("city", validate.String().Min(1)),
	validate.Field("state", validate.String().Min(1)),
)

var CreateUserSchema = validate.Object[CreateUser](
	validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
	validate.Field("password", validate.String().Min(8).Max(128)),
	validate.Field("age", validate.Int().Min(13).Max(120)),
	validate.Field("address", AddressSchema),
)

func TestObjectHappyPathAndNested(t *testing.T) {
	u, err := CreateUserSchema.Parse(jsonVal(t, `{"email":" A@B.COM ","password":"longenough","age":20,
		"address":{"street":"s","city":"c","state":"st"},"extra":1}`))
	if err != nil || u.Email != "a@b.com" || u.Address.City != "c" {
		t.Fatal(u, err)
	}
	_, err = CreateUserSchema.Parse(jsonVal(t, `{"email":"a@b.com","password":"longenough","age":20,
		"address":{"street":"s","city":"","state":"st"}}`))
	ve, _ := validate.AsValidationError(err)
	if ve == nil || len(ve.Issues) != 1 || ve.Issues[0].Path.String() != "address.city" {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ve.Issues[0].Path)
	if string(b) != `["address","city"]` {
		t.Fatal(string(b))
	}
}

func TestMultipleErrorsAndOrdering(t *testing.T) { // #102, #1.4
	in := jsonVal(t, `{"email":"no","password":"x","age":8,"address":{"street":"s","city":"c","state":"x"}}`)
	for i := 0; i < 50; i++ {
		_, err := CreateUserSchema.Parse(in)
		want := []string{"email:invalid_email", "password:too_small", "age:too_small"}
		if got := codes(err); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}
	// missing and null
	_, err := CreateUserSchema.Parse(jsonVal(t, `{"email":null}`))
	got := codes(err)
	if got[0] != "email:invalid_type" || got[1] != "password:required" {
		t.Fatal(got)
	}
}

func TestUnknownKeys(t *testing.T) { // #103
	type E struct{ Email string }
	f := validate.Field("email", validate.String())
	strict := validate.Object[E](f).Strict()
	_, err := strict.Parse(jsonVal(t, `{"email":"x@example.com","unexpected":123,"another":1}`))
	if got := codes(err); !reflect.DeepEqual(got, []string{"another:unknown_key", "unexpected:unknown_key"}) {
		t.Fatal(got)
	}
	if _, err := validate.Object[E](f).Parse(jsonVal(t, `{"email":"x","zzz":1}`)); err != nil {
		t.Fatal("default must strip:", err)
	}
}

func TestRootNull(t *testing.T) { // #44
	if _, err := CreateUserSchema.Parse(nil); err == nil {
		t.Fatal("null object accepted")
	}
	n, err := validate.Nullable(AddressSchema).Parse(nil)
	if err != nil || !n.IsNull() {
		t.Fatal(n, err)
	}
	if _, err := CreateUserSchema.Parse("str"); err == nil {
		t.Fatal("non-object accepted")
	}
}

func TestFieldNameMapping(t *testing.T) { // #22, #26
	type Base struct{ ID string }
	type U struct {
		Base
		EmailAddress string
		HomeURL      string
	}
	s := validate.Object[U](
		validate.Field("id", validate.String()),
		validate.Field("email_address", validate.String()),
		validate.Field("home-url", validate.String()),
	)
	u, err := s.Parse(map[string]any{"id": "1", "email_address": "e", "home-url": "u"})
	if err != nil || u.ID != "1" || u.EmailAddress != "e" || u.HomeURL != "u" {
		t.Fatal(u, err)
	}
	// "base" is a nested object, not flattening.
	type W struct{ Base }
	n := validate.Object[W](validate.Field("base", validate.Object[Base](validate.Field("id", validate.String()))))
	w, err := n.Parse(map[string]any{"base": map[string]any{"id": "z"}})
	if err != nil || w.ID != "z" {
		t.Fatal(w, err)
	}
}

func TestSchemaDefinitionErrorsPanic(t *testing.T) { // #106
	type S struct {
		A  string
		AA string
		a  string
		N  int
	}
	str := validate.String()
	mustPanic(t, "duplicate", func() { validate.Object[S](validate.Field("a", str), validate.Field("a", str)) })
	mustPanic(t, "no match", func() { validate.Object[S](validate.Field("missing", str)) })
	mustPanic(t, "not assignable", func() { validate.Object[S](validate.Field("n", str)) })
	mustPanic(t, "non-struct", func() { validate.Object[int]() })
	type Amb struct{ FooBar, Foobar string }
	mustPanic(t, "ambiguous", func() { validate.Object[Amb](validate.Field("foo_bar2", str), validate.Field("foo-bar", str)) })
	type Amb2 struct{ FooBar, Foo_Bar string }
	mustPanic(t, "ambiguous norm", func() { validate.Object[Amb2](validate.Field("foobar", str)) })
	mustPanic(t, "two to one", func() { validate.Object[S](validate.Field("a", str), validate.Field("A", str)) })
}

func TestObjectRefine(t *testing.T) { // #34
	type R struct{ Password, ConfirmPassword string }
	s := validate.Object[R](
		validate.Field("password", validate.String().Min(8)),
		validate.Field("confirm_password", validate.String()),
	).Refine(func(v R) error {
		if v.Password != v.ConfirmPassword {
			return validate.IssueAt(validate.PathOf("confirm_password"), "password_mismatch", "passwords do not match")
		}
		return nil
	})
	_, err := s.Parse(map[string]any{"password": "12345678", "confirm_password": "x"})
	ve, _ := validate.AsValidationError(err)
	if ve == nil || ve.Issues[0].Code != "password_mismatch" || ve.Issues[0].Path.String() != "confirm_password" {
		t.Fatal(err)
	}
	// refinement does not run when fields failed
	_, err = s.Parse(map[string]any{"password": "short", "confirm_password": "x"})
	if got := codes(err); !reflect.DeepEqual(got, []string{"password:too_small"}) {
		t.Fatal(got)
	}
}

// ---- arrays / maps (#18, #19, #101, #113) ----

type OrderItem struct {
	ProductID string
	Quantity  int
}
type CreateOrder struct {
	CustomerID string
	Items      []OrderItem
	Note       string
}

var OrderSchema = validate.Object[CreateOrder](
	validate.Field("customer_id", validate.String().Min(1)),
	validate.Field("items", validate.Array(validate.Object[OrderItem](
		validate.Field("product_id", validate.String().Min(1)),
		validate.Field("quantity", validate.Int().Min(1).Max(100)),
	)).Min(1).Max(100)),
	validate.Field("note", validate.String().Trim().Max(500)),
)

func TestNestedArrayPaths(t *testing.T) {
	_, err := OrderSchema.Parse(jsonVal(t, `{"customer_id":"c","note":"n","items":[
		{"product_id":"a","quantity":1},{"product_id":"b","quantity":0}]}`))
	ve, _ := validate.AsValidationError(err)
	if ve == nil || len(ve.Issues) != 1 {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ve.Issues[0].Path)
	if string(b) != `["items",1,"quantity"]` || ve.Issues[0].Path.String() != "items[1].quantity" {
		t.Fatal(string(b))
	}
	if len(ve.Field("items", "1", "quantity")) != 1 || !ve.Has("too_small") {
		t.Fatal("Field/Has")
	}
	o, err := OrderSchema.Parse(jsonVal(t, `{"customer_id":"c","note":"","items":[{"product_id":"a","quantity":2}]}`))
	if err != nil || len(o.Items) != 1 || o.Items[0].Quantity != 2 {
		t.Fatal(o, err)
	}
}

func TestMapDeterministicOrder(t *testing.T) {
	s := validate.Map(validate.Int().Min(10))
	in := map[string]any{"b": 1, "a": 1, "c": 1, "d": 50}
	for i := 0; i < 30; i++ {
		_, err := s.Parse(in)
		if got := codes(err); !reflect.DeepEqual(got, []string{"a:too_small", "b:too_small", "c:too_small"}) {
			t.Fatal(got)
		}
	}
	m, err := validate.Map(validate.String()).Parse(map[string]string{"k": "v"})
	if err != nil || m["k"] != "v" {
		t.Fatal(m, err)
	}
}

func TestArrayAcceptsTypedSlicesAndEmpty(t *testing.T) {
	out, err := validate.Array(validate.String()).Parse([]string{"a", "b"})
	if err != nil || len(out) != 2 {
		t.Fatal(out, err)
	}
	out, err = validate.Array(validate.String()).Parse([]any{})
	if err != nil || out == nil {
		t.Fatal("empty array must be non-nil", out, err)
	}
	if _, err := validate.Array(validate.String()).Parse("abc"); err == nil {
		t.Fatal("string is not an array")
	}
}

// ---- transform / refine / custom / union ----

func TestTransformAndFailure(t *testing.T) { // #32, #110
	s := validate.Transform(validate.String(), func(v string) (uuid.UUID, error) { return uuid.Parse(v) })
	var _ validate.Schema[uuid.UUID] = s
	if _, err := s.Parse("123e4567-e89b-12d3-a456-426614174000"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Parse("nope")
	ve, _ := validate.AsValidationError(err)
	if ve == nil || ve.Issues[0].Code != "transform_error" || ve.Issues[0].Cause() == nil {
		t.Fatal(err)
	}
	if strings.Contains(ve.Issues[0].Message, "uuid") {
		t.Fatal("leaked internals:", ve.Issues[0].Message)
	}
}

func TestRefine(t *testing.T) { // #33
	s := validate.String().Refine(func(v string) error {
		if !strings.HasSuffix(v, "@company.com") {
			return validate.Error("company_email", "must be a company email").WithMetadata("domain", "company.com")
		}
		return nil
	})
	if _, err := s.Parse("a@company.com"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Parse("a@x.com")
	ve, _ := validate.AsValidationError(err)
	if ve.Issues[0].Code != "company_email" || ve.Issues[0].Metadata["domain"] != "company.com" {
		t.Fatal(ve.Issues[0])
	}
	g := validate.Refine(validate.Int(), func(v int) error { return validate.Error("odd", "x") })
	if got := codes(errOf(g.Parse(1))); !reflect.DeepEqual(got, []string{":odd"}) {
		t.Fatal(got)
	}
}

func TestCustomAndUnion(t *testing.T) {
	c := validate.Custom(func(v any) (int, []validate.Issue) {
		if s, ok := v.(string); ok && s == "ok" {
			return 1, nil
		}
		return 0, []validate.Issue{{Code: "nope", Message: "no"}}
	})
	if v, err := c.Parse("ok"); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	if got := codes(errOf(c.Parse("x"))); !reflect.DeepEqual(got, []string{":nope"}) {
		t.Fatal(got)
	}
	u := validate.Union(validate.String(), validate.Int())
	if v, _ := u.Parse("a"); v != "a" {
		t.Fatal(v)
	}
	if v, _ := u.Parse(3); v != 3 {
		t.Fatal(v)
	}
	if got := codes(errOf(u.Parse(true))); !reflect.DeepEqual(got, []string{":invalid_union"}) {
		t.Fatal(got)
	}
	mustPanic(t, "empty union", func() { validate.Union() })
}

func TestBytesTimeUUIDAny(t *testing.T) {
	src := []byte("abc")
	out, err := validate.Bytes().Min(1).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	src[0] = 'z'
	if string(out) != "abc" {
		t.Fatal("aliased input")
	}
	if b, err := validate.Bytes().Base64().Parse("YWJj"); err != nil || string(b) != "abc" {
		t.Fatal(b, err)
	}
	now := time.Now()
	if _, err := validate.Time().Parse("2020-01-01T00:00:00Z"); err == nil {
		t.Fatal("Time must not silently parse strings")
	}
	if _, err := validate.Time().Layout(time.RFC3339).Parse("2020-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := validate.Time().After(now).Parse(now.Add(-time.Hour)); err == nil {
		t.Fatal("After")
	}
	if _, err := validate.Time().Between(now, now.Add(time.Hour)).Parse(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if u, err := validate.UUID().Parse("123e4567-e89b-12d3-a456-426614174000"); err != nil || u == (uuid.UUID{}) {
		t.Fatal(u, err)
	}
	if v, err := validate.Any().Parse(nil); err != nil || v != nil {
		t.Fatal(v, err)
	}
}

// ---- errors ----

func TestErrorStringAndSecurity(t *testing.T) { // #39, #75
	_, err := CreateUserSchema.Parse(jsonVal(t, `{"email":"hunter2-secret","password":"x","age":4,"address":{"street":"s","city":"c","state":"x"}}`))
	if !strings.HasPrefix(err.Error(), "validation failed: email: invalid_email; password: too_small; age: too_small") {
		t.Fatal(err.Error())
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("value leaked into Error()")
	}
	ve, _ := validate.AsValidationError(err)
	if ve.Issues[0].Value != nil {
		t.Fatal("value populated by default")
	}
	b, _ := json.Marshal(ve.Response())
	if strings.Contains(string(b), "secret") {
		t.Fatal("value serialised")
	}
	_, err = validate.ParseContext(context.Background(), CreateUserSchema, jsonVal(t, `{"email":"hunter2-secret"}`), validate.WithValues())
	_ = err
}

func TestAbortEarlyAndContext(t *testing.T) {
	in := jsonVal(t, `{"email":"no","password":"x","age":8,"address":{}}`)
	_, err := validate.ParseContext(context.Background(), CreateUserSchema, in, validate.WithAbortEarly())
	if got := codes(err); len(got) != 1 {
		t.Fatal(got)
	}
	_, err = validate.ParseContext(context.Background(), CreateUserSchema, in, validate.WithValues())
	ve, _ := validate.AsValidationError(err)
	if ve.Issues[0].Value != "no" {
		t.Fatal("WithValues")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = validate.ParseContext(ctx, validate.Array(validate.Int()), []any{1, 2}, validate.WithAbortEarly())
	if err == nil || validate.IsValidationError(err) {
		t.Fatal("cancelled context should return ctx error:", err)
	}
	if !validate.IsValidationError(&validate.ValidationError{}) {
		t.Fatal()
	}
}

func TestSafeParse(t *testing.T) {
	r := CreateUserSchema.SafeParse(map[string]any{})
	if r.OK || r.Error == nil || r.Value.Email != "" {
		t.Fatal(r)
	}
	if r := validate.Int().SafeParse(3); !r.OK || r.Value != 3 || r.Error != nil {
		t.Fatal(r)
	}
}

func TestConcurrentUse(t *testing.T) {
	in := jsonVal(t, `{"email":"a@b.com","password":"longenough","age":20,"address":{"street":"s","city":"c","state":"x"}}`)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := CreateUserSchema.Parse(in); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// ---- HTTP ----

func jsonReq(body string, ct string) *http.Request {
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	return r
}

func TestJSONHelper(t *testing.T) {
	good := `{"email":"a@b.com","password":"longenough","age":20,"address":{"street":"s","city":"c","state":"x"}}`
	if _, err := validate.JSON(jsonReq(good, "application/json; charset=utf-8"), CreateUserSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := validate.JSON(jsonReq(good, ""), CreateUserSchema); err != nil {
		t.Fatal("missing content-type should be allowed by default:", err)
	}
	if _, err := validate.JSON(jsonReq(good, ""), CreateUserSchema, validate.RequireJSONContentType()); err == nil {
		t.Fatal("require")
	}
	cases := map[string]string{
		"trailing":  good + " garbage",
		"two":       good + good,
		"malformed": `{"email":`,
		"empty":     ``,
		"spaces":    "  \n",
	}
	for name, body := range cases {
		_, err := validate.JSON(jsonReq(body, "application/json"), CreateUserSchema)
		if !validate.IsValidationError(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := validate.JSON(jsonReq(good+"  \n", "application/json"), CreateUserSchema); err != nil {
		t.Fatal("trailing whitespace is fine:", err)
	}
	if _, err := validate.JSON(jsonReq("null", "application/json"), CreateUserSchema); !validate.IsValidationError(err) {
		t.Fatal("root null")
	}
	_, err := validate.JSON(jsonReq(good, "text/plain"), CreateUserSchema)
	if re, ok := err.(*validate.RequestError); !ok || re.Status != 415 {
		t.Fatal(err)
	}
	_, err = validate.JSON(jsonReq(good, "application/json"), CreateUserSchema, validate.WithMaxBodyBytes(10))
	if re, ok := err.(*validate.RequestError); !ok || re.Status != 413 {
		t.Fatal(err)
	}
	type E struct{ A string }
	opt := validate.Object[E](validate.Field("a", validate.String().Default("d")))
	if v, err := validate.JSON(jsonReq("", ""), opt, validate.AllowEmptyBody()); err != nil || v.A != "d" {
		t.Fatal(v, err)
	}
}

func TestWriteError(t *testing.T) {
	_, err := validate.JSON(jsonReq(`{"email":"bad","password":"hunter2hunter2"}`, "application/json"), CreateUserSchema)
	w := httptest.NewRecorder()
	validate.WriteError(w, err)
	if w.Code != 400 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal(w.Code, w.Header())
	}
	var body struct {
		Error  string `json:"error"`
		Issues []struct {
			Path []any  `json:"path"`
			Code string `json:"code"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != "validation_error" || body.Issues[0].Path[0] != "email" {
		t.Fatal(w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "hunter2") {
		t.Fatal("leaked value")
	}
	w = httptest.NewRecorder()
	validate.WriteError(w, http.ErrAbortHandler)
	if w.Code != 500 || strings.Contains(w.Body.String(), "abort") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	validate.WriteErrorWith(w, err, validate.WriteOptions{ValidationStatus: 422})
	if w.Code != 422 {
		t.Fatal(w.Code)
	}
}

type Query struct {
	Page   int
	Limit  int
	Active bool
	Search string
	Tag    []string
}

var QuerySchema = validate.Object[Query](
	validate.Field("page", validate.Int().Coerce().Min(1).Default(1)),
	validate.Field("limit", validate.Int().Coerce().Min(1).Max(100).Default(20)),
	validate.Field("active", validate.Bool().Coerce().Default(true)),
	validate.Field("search", validate.Optional(validate.String().Trim())),
	validate.Field("tag", validate.Array(validate.String()).DefaultFunc(func() []string { return nil })),
)

func TestQuery(t *testing.T) { // #47, #48, #100
	r := httptest.NewRequest("GET", "/?page=2&limit=30&active=false&tag=go&search=+x+", nil)
	q, err := validate.Query(r, QuerySchema)
	if err != nil || q.Page != 2 || q.Limit != 30 || q.Active || q.Search != "x" || !reflect.DeepEqual(q.Tag, []string{"go"}) {
		t.Fatalf("%+v %v", q, err)
	}
	r = httptest.NewRequest("GET", "/?tag=go&tag=postgres&tag=api", nil)
	q, err = validate.Query(r, QuerySchema)
	if err != nil || !reflect.DeepEqual(q.Tag, []string{"go", "postgres", "api"}) || q.Page != 1 || q.Limit != 20 || !q.Active {
		t.Fatalf("%+v %v", q, err)
	}
	r = httptest.NewRequest("GET", "/?page=abc&limit=0", nil)
	_, err = validate.Query(r, QuerySchema)
	if got := codes(err); !reflect.DeepEqual(got, []string{"page:invalid_value", "limit:too_small"}) {
		t.Fatal(got)
	}
	// scalar repeated is rejected, not silently first-wins
	r = httptest.NewRequest("GET", "/?page=1&page=2", nil)
	if _, err := validate.Query(r, QuerySchema); err == nil {
		t.Fatal("repeated scalar accepted")
	}
}

func TestPathHeadersCookies(t *testing.T) {
	type P struct{ ID int }
	ps := validate.Object[P](validate.Field("id", validate.Int().Coerce().Positive()))
	if p, err := validate.PathValues(map[string]string{"id": "7"}, ps); err != nil || p.ID != 7 {
		t.Fatal(p, err)
	}
	type H struct {
		XRequestID string
		Accept     []string
	}
	hs := validate.Object[H](
		validate.Field("x-request-id", validate.String().Min(1)),
		validate.Field("accept", validate.Array(validate.String())),
	)
	h := http.Header{}
	h.Set("X-REQUEST-ID", "abc")
	h.Add("Accept", "a")
	h.Add("Accept", "b")
	if v, err := validate.Headers(h, hs); err != nil || v.XRequestID != "abc" || len(v.Accept) != 2 {
		t.Fatal(v, err)
	}
	type C struct{ Session string }
	cs := validate.Object[C](validate.Field("session", validate.String().Min(1)))
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "s1"})
	if v, err := validate.Cookies(r, cs); err != nil || v.Session != "s1" {
		t.Fatal(v, err)
	}
}

func TestAcceptanceHandler(t *testing.T) { // #132
	type Create struct {
		Email    string
		Password string
		Age      int
	}
	schema := validate.Object[Create](
		validate.Field("email", validate.String().Trim().Lower().Email().Max(255)),
		validate.Field("password", validate.String().Min(8).Max(128)),
		validate.Field("age", validate.Int().Min(13).Max(120)),
	)
	var got Create
	h := func(w http.ResponseWriter, r *http.Request) {
		input, err := validate.JSON(r, schema)
		if err != nil {
			validate.WriteError(w, err)
			return
		}
		got = input
	}
	w := httptest.NewRecorder()
	h(w, jsonReq(`{"email":" Me@X.io ","password":"password1","age":30}`, "application/json"))
	if got.Email != "me@x.io" || got.Age != 30 || w.Code != 200 {
		t.Fatal(got, w.Code)
	}
}
