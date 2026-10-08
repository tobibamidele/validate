package gentest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	validate "github.com/tobibamidele/validate"
	"github.com/tobibamidele/validate/gen"
	"github.com/tobibamidele/validate/gentest"
)

func TestGeneratedFileIsUpToDate(t *testing.T) {
	want, err := gen.Generate(gentest.Targets())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("validators_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("validators_gen.go is stale: run `go run ./gentest/cmd/gen` from the module root")
	}
}

func TestGeneratedIsActive(t *testing.T) {
	if !gentest.ProfileFast.Active() || !gentest.StrictProfileFast.Active() || !gentest.GeoFast.Active() {
		t.Fatal("a generated validator fell back to runtime: fingerprint mismatch")
	}
}

func TestStaleGeneratedFallsBackToRuntime(t *testing.T) {
	stale := validate.Generated[gentest.Geo](gentest.GeoSchema, "not-the-fingerprint",
		func() validate.GenFunc[gentest.Geo] { panic("build must not run for a stale fingerprint") })
	if stale.Active() {
		t.Fatal("stale generated code must not be active")
	}
	in := map[string]any{"lat": 10.0, "lng": 20.0}
	a, errA := stale.Parse(in)
	b, errB := gentest.GeoSchema.Parse(in)
	if errA != nil || errB != nil || a != b {
		t.Fatal(a, b, errA, errB)
	}
	if _, err := stale.Parse(map[string]any{"lat": 0.0, "lng": 0.0}); !validate.IsValidationError(err) {
		t.Fatal("fallback must still validate")
	}
}

func TestGeneratorRejectsUnsupported(t *testing.T) {
	if _, err := gen.Generate(gen.Config{Package: "x", Targets: []gen.Target{{Name: "A", Expr: "S", Schema: validate.String()}}}); err == nil {
		t.Fatal("non-Object schema must be rejected")
	}
	type viaPtr struct{ *gentest.Base }
	s := validate.Object[viaPtr](validate.Field("id", validate.String()))
	_, err := gen.Generate(gen.Config{Package: "gentest_test", Targets: []gen.Target{{Name: "A", Expr: "S", Schema: s}}})
	if err == nil || !strings.Contains(err.Error(), "embedded pointer") {
		t.Fatalf("embedded pointer promotion must be rejected clearly, got %v", err)
	}
	if _, err := gen.Generate(gen.Config{Package: "x"}); err == nil {
		t.Fatal("no targets")
	}
}

// ---- differential testing ----

type pair struct {
	name string
	rt   validate.Schema[gentest.Profile]
	fast validate.Schema[gentest.Profile]
}

var pairs = []pair{
	{"profile", gentest.ProfileSchema, gentest.ProfileFast},
	{"strict", gentest.StrictProfileSchema, gentest.StrictProfileFast},
}

var optionSets = map[string][]validate.Option{
	"default": nil,
	"abort":   {validate.WithAbortEarly()},
	"values":  {validate.WithValues()},
	"both":    {validate.WithAbortEarly(), validate.WithValues()},
}

func compare(t *testing.T, label string, rt, fast validate.Schema[gentest.Profile], in any, opts []validate.Option) {
	t.Helper()
	a, errA := validate.ParseContext(context.Background(), rt, in, opts...)
	b, errB := validate.ParseContext(context.Background(), fast, in, opts...)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("%s: VALUE differs\n input: %#v\n runtime:   %#v\n generated: %#v", label, in, a, b)
	}
	if !reflect.DeepEqual(errA, errB) {
		t.Fatalf("%s: ERROR differs\n input: %#v\n runtime:   %v\n generated: %v", label, in, errA, errB)
	}
}

func validProfile() map[string]any {
	return map[string]any{
		"id": "123e4567-e89b-12d3-a456-426614174000", "email": " A@B.io ", "age": 30,
		"nick": "nicky", "bio": "hello", "tags": []any{"a", "b"}, "role": "admin",
		"address": map[string]any{"street": " s ", "city": "c", "geo": map[string]any{"lat": 1.5, "lng": 2.5}},
		"home":    map[string]any{"street": "s", "city": "c", "geo": map[string]any{"lat": 3.0, "lng": 4.0}},
		"score":   9.5, "prev": nil, "level": "HIGH",
	}
}

// junk is what the mutator substitutes for a field: wrong types, nulls,
// boundary values and out-of-range values.
var junk = []any{
	nil, "", " ", "x", "not-an-email", "a@blocked.example", "123e4567-e89b-12d3-a456-426614174000",
	0, 1, -1, 12, 13, 121, 1e9, 18.5, json.Number("7"), json.Number("200"), json.Number("1.5"), "30",
	true, false, []any{}, []any{"a", "b", "c", "d", "e", "f"}, []any{1, 2}, []any{""},
	map[string]any{}, map[string]any{"street": "s"}, map[string]any{"lat": 0.0, "lng": 0.0},
	map[string]any{"lat": 91.0, "lng": 0.0}, "admin", "member", "root", "low", "high", "HIGH",
	map[string]any{"street": "s", "city": "c", "geo": map[string]any{"lat": 0.0, "lng": 0.0}},
}

var keys = []string{"id", "email", "age", "nick", "bio", "tags", "role", "address", "home", "score", "prev", "level", "extra"}

func mutate(r *rand.Rand, m map[string]any) {
	for n := r.Intn(4); n >= 0; n-- {
		target, nested := m, false
		// sometimes descend into a nested object
		if r.Intn(3) == 0 {
			for _, k := range []string{"address", "home"} {
				if nm, ok := m[k].(map[string]any); ok && r.Intn(2) == 0 {
					target, nested = nm, true
					if g, ok := nm["geo"].(map[string]any); ok && r.Intn(2) == 0 {
						target = g
					}
				}
			}
		}
		ks := keys
		if nested {
			ks = []string{"street", "city", "geo", "lat", "lng", "extra"}
		}
		k := ks[r.Intn(len(ks))]
		switch r.Intn(3) {
		case 0:
			delete(target, k)
		default:
			target[k] = junk[r.Intn(len(junk))]
		}
	}
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		o := make(map[string]any, len(x))
		for k, e := range x {
			o[k] = deepCopy(e)
		}
		return o
	case []any:
		o := make([]any, len(x))
		for i, e := range x {
			o[i] = deepCopy(e)
		}
		return o
	}
	return v
}

func TestDifferentialRandom(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	var okCount, failCount int
	for i := 0; i < 20000; i++ {
		in := deepCopy(validProfile()).(map[string]any)
		mutate(r, in)
		for _, p := range pairs {
			for name, opts := range optionSets {
				compare(t, fmt.Sprintf("%s/%s/#%d", p.name, name, i), p.rt, p.fast, in, opts)
			}
		}
		if _, err := gentest.ProfileSchema.Parse(in); err == nil {
			okCount++
		} else {
			failCount++
		}
	}
	// the corpus must exercise both outcomes or the comparison proves little
	if okCount < 500 || failCount < 500 {
		t.Fatalf("weak corpus: %d valid, %d invalid", okCount, failCount)
	}
	t.Logf("compared 20000 inputs x 2 schemas x 4 option sets (%d valid, %d invalid under ProfileSchema)", okCount, failCount)
}

func TestDifferentialStructuralShapes(t *testing.T) {
	shapes := []any{nil, "x", 1, []any{}, map[string]any{}, map[string]string{"id": "x"}, validProfile()}
	for _, in := range shapes {
		for _, p := range pairs {
			for name, opts := range optionSets {
				compare(t, p.name+"/"+name, p.rt, p.fast, in, opts)
			}
		}
	}
	// whole valid payload round trips to the same value
	a, _ := gentest.ProfileSchema.Parse(validProfile())
	b, err := gentest.ProfileFast.Parse(validProfile())
	if err != nil || !reflect.DeepEqual(a, b) || b.Base.ID == "" || b.Address.Geo.Lat != 1.5 || b.Home == nil || b.Level != 2 || b.Nick != "nicky" || *b.Bio != "hello" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestDifferentialCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errA := validate.ParseContext(ctx, gentest.ProfileSchema, validProfile())
	_, errB := validate.ParseContext(ctx, gentest.ProfileFast, validProfile())
	if !reflect.DeepEqual(errA, errB) || errA == nil {
		t.Fatal(errA, errB)
	}
}

// FuzzGeneratedDifferential drives both implementations with arbitrary JSON.
func FuzzGeneratedDifferential(f *testing.F) {
	seed, _ := json.Marshal(validProfile())
	for _, s := range []string{string(seed), `{}`, `null`, `[]`, `{"id":1}`, `{"address":{"geo":{"lat":0,"lng":0}}}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var in any
		if dec.Decode(&in) != nil {
			return
		}
		for _, p := range pairs {
			for name, opts := range optionSets {
				compare(t, p.name+"/"+name, p.rt, p.fast, in, opts)
			}
		}
	})
}

func BenchmarkRuntimeProfile(b *testing.B) {
	in := validProfile()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := gentest.ProfileSchema.Parse(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeneratedProfile(b *testing.B) {
	in := validProfile()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := gentest.ProfileFast.Parse(in); err != nil {
			b.Fatal(err)
		}
	}
}
