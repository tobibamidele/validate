package validate

// AnySchema accepts any present value, including null, unchanged. For JSON
// input, numbers arrive as json.Number (the decoder uses UseNumber) so large
// integers are not rounded through float64.
type AnySchema struct{}

// Any returns the dynamic escape-hatch schema.
func Any() AnySchema { return AnySchema{} }

func (AnySchema) run(c *pctx, in input) (any, bool) {
	if !in.present {
		c.required(in)
		return nil, false
	}
	return in.v, true
}
