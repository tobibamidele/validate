package validate_test

import (
	"reflect"
	"testing"

	validate "github.com/tobibamidele/validate"
)

type Event interface{ isEvent() }

type Created struct {
	Type string
	ID   string
}
type Deleted struct {
	Type   string
	Reason string
}

func (Created) isEvent() {}
func (Deleted) isEvent() {}

var EventSchema = validate.Discriminated[Event]("type",
	validate.Variant("created", validate.Object[Created](
		validate.Field("type", validate.String()),
		validate.Field("id", validate.String().Min(1)),
	).Strict(), func(v Created) Event { return v }),
	validate.Variant("deleted", validate.Object[Deleted](
		validate.Field("type", validate.String()),
		validate.Field("reason", validate.String().Min(3)),
	), func(v Deleted) Event { return v }),
)

var _ validate.Schema[Event] = EventSchema

func TestDiscriminatedSelectsVariant(t *testing.T) {
	ev, err := EventSchema.Parse(map[string]any{"type": "created", "id": "x1"})
	if err != nil || ev != (Created{Type: "created", ID: "x1"}) {
		t.Fatal(ev, err)
	}
	ev, err = EventSchema.Parse(map[string]any{"type": "deleted", "reason": "spam"})
	if _, ok := ev.(Deleted); !ok || err != nil {
		t.Fatal(ev, err)
	}
}

func TestDiscriminatedErrors(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"missing tag", map[string]any{}, []string{"type:required"}},
		{"non-string tag", map[string]any{"type": 3}, []string{"type:invalid_type"}},
		{"unknown tag", map[string]any{"type": "moved"}, []string{"type:invalid_union"}},
		{"not an object", "x", []string{":invalid_type"}},
		{"null", nil, []string{":invalid_type"}},
		// only the selected variant is validated: errors are about its fields
		{"variant error", map[string]any{"type": "deleted", "reason": "x"}, []string{"reason:too_small"}},
		{"strict variant", map[string]any{"type": "created", "id": "a", "extra": 1}, []string{"extra:unknown_key"}},
	}
	for _, tc := range cases {
		_, err := EventSchema.Parse(tc.in)
		if got := codes(err); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	_, err := EventSchema.Parse(map[string]any{"type": "moved"})
	ve, _ := validate.AsValidationError(err)
	if allowed := ve.Issues[0].Metadata["allowed"]; !reflect.DeepEqual(allowed, []string{"created", "deleted"}) {
		t.Fatal(allowed)
	}
}

func TestDiscriminatedInsideArrayAndDefinitionErrors(t *testing.T) {
	_, err := validate.Array(EventSchema).Parse([]any{
		map[string]any{"type": "created", "id": "a"},
		map[string]any{"type": "deleted", "reason": "no"},
	})
	if got := codes(err); !reflect.DeepEqual(got, []string{"[1].reason:too_small"}) {
		t.Fatal(got)
	}
	v := validate.Variant("a", validate.String(), func(s string) Event { return nil })
	mustPanic(t, "dup tag", func() { validate.Discriminated("t", v, v) })
	mustPanic(t, "no variants", func() { validate.Discriminated[Event]("t") })
	mustPanic(t, "empty key", func() { validate.Discriminated("", v) })
}
