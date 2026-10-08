package validate

// BoolSchema validates booleans.
type BoolSchema struct {
	coerce bool
	def    *bool
}

// Bool returns a schema for booleans.
func Bool() BoolSchema { return BoolSchema{} }

// Coerce additionally accepts the strings "true" and "false" (exactly, and
// nothing else: no "yes", "on", "1").
func (s BoolSchema) Coerce() BoolSchema { s.coerce = true; return s }

// Default supplies a value when the input is absent. An explicit false is kept.
func (s BoolSchema) Default(v bool) BoolSchema { s.def = &v; return s }

func (s BoolSchema) run(c *pctx, in input) (bool, bool) {
	if !in.present {
		if s.def == nil {
			c.required(in)
			return false, false
		}
		return *s.def, true
	}
	switch x := in.v.(type) {
	case bool:
		return x, true
	case string:
		if s.coerce {
			switch x {
			case "true":
				return true, true
			case "false":
				return false, true
			}
			c.fail(in, CodeInvalidValue, `must be "true" or "false"`)
			return false, false
		}
	}
	c.typeErr(in, "boolean")
	return false, false
}
