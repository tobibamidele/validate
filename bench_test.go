package validate_test

import (
	"testing"
)

func BenchmarkObjectParse(b *testing.B) {
	in := map[string]any{
		"email": " A@B.com ", "password": "longenough", "age": 20,
		"address": map[string]any{"street": "s", "city": "c", "state": "x"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CreateUserSchema.Parse(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkObjectParseInvalid(b *testing.B) {
	in := map[string]any{"email": "no", "password": "x", "age": 1, "address": map[string]any{}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = CreateUserSchema.Parse(in)
	}
}

func BenchmarkJSONEndToEnd(b *testing.B) {
	body := `{"email":"a@b.com","password":"longenough","age":20,"address":{"street":"s","city":"c","state":"x"}}`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := validate_FromJSON(body); err != nil {
			b.Fatal(err)
		}
	}
}
