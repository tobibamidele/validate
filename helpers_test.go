package validate_test

import (
	"context"
	"encoding/json"
	"strings"
)

func bg() context.Context { return context.Background() }

func jsonValB(s string) any {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		panic(err)
	}
	return v
}
