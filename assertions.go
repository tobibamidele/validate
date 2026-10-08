package validate

import (
	"time"

	"github.com/google/uuid"
)

// Compile-time guarantees that schemas preserve their output types.
var (
	_ Schema[string]                = StringSchema{}
	_ Schema[bool]                  = BoolSchema{}
	_ Schema[any]                   = AnySchema{}
	_ Schema[int]                   = IntSchema{}
	_ Schema[uint8]                 = Uint8Schema{}
	_ Schema[float32]               = Float32Schema{}
	_ Schema[[]byte]                = BytesSchema{}
	_ Schema[time.Time]             = TimeSchema{}
	_ Schema[uuid.UUID]             = UUIDSchema{}
	_ Schema[[]string]              = ArraySchema[string]{}
	_ Schema[map[string]int]        = MapSchema[int]{}
	_ Schema[OptionalValue[string]] = OptionalSchema[string]{}
	_ Schema[NullableValue[string]] = NullableSchema[string]{}
	_ Schema[any]                   = UnionSchema{}
	_ Node                          = StringSchema{}
	_ Node                          = IntSchema{}
)
