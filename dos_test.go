package validate_test

import (
	"strings"
	"testing"
	"time"

	validate "github.com/tobibamidele/validate"
)

// Pathological inputs must fail (or pass) quickly and never panic.
func TestPathologicalInputsAreFast(t *testing.T) {
	cases := map[string]string{
		"deep nesting":      strings.Repeat("[", 1_000_000),
		"deep objects":      strings.Repeat(`{"a":`, 200_000),
		"huge digit run":    `{"age":` + strings.Repeat("9", 1_000_000) + `}`,
		"long exponent":     `{"age":1e` + strings.Repeat("9", 100_000) + `}`,
		"long email":        `{"email":"` + strings.Repeat("a.", 200_000) + `@b.com"}`,
		"quote-heavy email": `{"email":"` + strings.Repeat(`\"`, 200_000) + `@b.com"}`,
		"comment email":     `{"email":"` + strings.Repeat("(", 100_000) + `a@b.com"}`,
		"many keys":         "{" + strings.Repeat(`"k":1,`, 200_000) + `"z":1}`,
		"long string":       `{"password":"` + strings.Repeat("é", 5_000_000) + `"}`,
	}
	for name, body := range cases {
		start := time.Now()
		_, err := validate.FromJSON([]byte(body), CreateUserSchema)
		if err == nil {
			t.Errorf("%s: unexpectedly valid", name)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s: took %v", name, d)
		} else {
			t.Logf("%-18s %v", name, d.Round(time.Millisecond))
		}
	}
}
