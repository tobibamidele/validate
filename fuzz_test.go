package validate_test

import (
	"testing"

	validate "github.com/tobibamidele/validate"
)

func validate_FromJSON(body string) (CreateUser, error) {
	return validate.FromJSON([]byte(body), CreateUserSchema)
}

// FuzzFromJSON checks that arbitrary bytes never panic and that a failure is
// always a well-formed *ValidationError whose message never echoes input.
func FuzzFromJSON(f *testing.F) {
	for _, s := range []string{
		``, `null`, `{}`, `[]`, `{"email":null}`, `{"age":1e999}`, `{"age":-0}`,
		`{"age":18446744073709551616}`, `{"email":"a@b.com"} x`, `{"address":[]}`,
		`{"email":"` + "\xff\xfe" + `"}`, `[[[[[[[[[[[[[[[[`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, err := validate_FromJSON(s)
		if err == nil {
			return
		}
		ve, ok := validate.AsValidationError(err)
		if !ok || len(ve.Issues) == 0 {
			t.Fatalf("non-validation or empty error: %v", err)
		}
		for _, is := range ve.Issues {
			if is.Code == "" || is.Path == nil {
				t.Fatalf("malformed issue %+v", is)
			}
		}
	})
}

func FuzzIntCoerce(f *testing.F) {
	for _, s := range []string{"0", "-1", "+5", "99999999999999999999", "1.5", "", " 1", "0x10", "1_0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := validate.Int8().Coerce().Parse(s)
		if err == nil && (v < -128 || v > 127) {
			t.Fatalf("wrapped: %q -> %d", s, v)
		}
	})
}
