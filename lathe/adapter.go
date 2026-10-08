// Package lathe bridges Lathe column definitions to validate schemas.
//
// It carries only type and safe size/enum metadata. It never turns database
// constraints (Unique, ForeignKey, Index, PrimaryKey, defaults, NOT NULL) into
// request validation, and it never touches a database.
//
// Lathe's generated Column[T] values identify a column (table and name) but do
// not carry its definition, so the adapter reads definitions from the
// schema.Snapshot your Lathe schema package exports:
//
//	snap := mySchema.Snapshot // from schema.Export()
//	cat := validatelathe.NewCatalog(snap)
//	email := validatelathe.String(cat, db.Users.Email).Trim().Lower().Email()
package lathe

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tobibamidele/lathe"
	"github.com/tobibamidele/lathe/schema"
	validate "github.com/tobibamidele/validate"
)

// Catalog resolves columns against a Lathe schema snapshot.
type Catalog struct{ snap *schema.Snapshot }

// NewCatalog wraps a snapshot. It panics on nil.
func NewCatalog(snap *schema.Snapshot) *Catalog {
	if snap == nil {
		panic("validate/lathe: nil snapshot")
	}
	return &Catalog{snap: snap}
}

func (c *Catalog) def(col lathe.ColumnRef) *schema.ColumnDef {
	t := c.snap.Table(col.TableName())
	if t == nil {
		panic(fmt.Sprintf("validate/lathe: table %q is not in the snapshot", col.TableName()))
	}
	for i := range t.Columns {
		if t.Columns[i].Name == col.ColumnName() {
			return t.Columns[i]
		}
	}
	panic(fmt.Sprintf("validate/lathe: column %s.%s is not in the snapshot", col.TableName(), col.ColumnName()))
}

// String builds a StringSchema for a string column. Safe mappings only:
// VarChar(n) -> Max(n) (runes, per validate's documented length semantics),
// Enum(...) -> OneOf(...), UUID -> UUID(). Text and everything else add no
// constraint.
func String(c *Catalog, col lathe.Column[string]) validate.StringSchema {
	d := c.def(col)
	s := validate.String()
	switch d.Type.Kind {
	case schema.KindVarChar:
		if d.Type.Length > 0 {
			s = s.Max(d.Type.Length)
		}
	case schema.KindEnum:
		if len(d.Type.Values) > 0 {
			s = s.OneOf(d.Type.Values...)
		}
	case schema.KindUUID:
		s = s.UUID()
	}
	return s
}

// Int16 builds a schema for an int16 column.
func Int16(_ *Catalog, _ lathe.Column[int16]) validate.Int16Schema { return validate.Int16() }

// Int32 builds a schema for an int32 column.
func Int32(_ *Catalog, _ lathe.Column[int32]) validate.Int32Schema { return validate.Int32() }

// Int64 builds a schema for an int64 column.
func Int64(_ *Catalog, _ lathe.Column[int64]) validate.Int64Schema { return validate.Int64() }

// Float32 builds a schema for a float32 column.
func Float32(_ *Catalog, _ lathe.Column[float32]) validate.Float32Schema { return validate.Float32() }

// Float64 builds a schema for a float64 column.
func Float64(_ *Catalog, _ lathe.Column[float64]) validate.Float64Schema { return validate.Float64() }

// Bool builds a schema for a bool column.
func Bool(_ *Catalog, _ lathe.Column[bool]) validate.BoolSchema { return validate.Bool() }

// UUID builds a schema for a uuid.UUID column.
func UUID(_ *Catalog, _ lathe.Column[uuid.UUID]) validate.UUIDSchema { return validate.UUID() }

// Time builds a schema for a time.Time column.
func Time(_ *Catalog, _ lathe.Column[time.Time]) validate.TimeSchema { return validate.Time() }

// Bytes builds a schema for a []byte column.
func Bytes(_ *Catalog, _ lathe.Column[[]byte]) validate.BytesSchema { return validate.Bytes() }

// Enum builds a type-preserving enum schema for a string-backed named type
// when the column is a Lathe Enum. convert maps each database value to your Go
// constant type. It panics if the column is not an enum, or if convert
// produces duplicates.
func Enum[T ~string](c *Catalog, col lathe.Column[T]) validate.EnumSchema[T] {
	d := c.def(col)
	if d.Type.Kind != schema.KindEnum || len(d.Type.Values) == 0 {
		panic(fmt.Sprintf("validate/lathe: column %s.%s is not an enum", col.TableName(), col.ColumnName()))
	}
	vals := make([]T, len(d.Type.Values))
	for i, v := range d.Type.Values {
		vals[i] = T(v)
	}
	return validate.Enum(vals...)
}

// FromColumn returns the schema for any column whose Go type has a mapping,
// as a plain validate.Schema[T] (use String, Enum, ... when you want the
// fluent builder). It panics for unmapped types so mismatches are found at
// startup, not at request time.
func FromColumn[T any](c *Catalog, col lathe.Column[T]) validate.Schema[T] {
	var zero T
	var s any
	switch any(zero).(type) {
	case string:
		s = String(c, any(col).(lathe.Column[string]))
	case bool:
		s = validate.Bool()
	case int16:
		s = validate.Int16()
	case int32:
		s = validate.Int32()
	case int64:
		s = validate.Int64()
	case float32:
		s = validate.Float32()
	case float64:
		s = validate.Float64()
	case uuid.UUID:
		s = validate.UUID()
	case time.Time:
		s = validate.Time()
	case []byte:
		s = validate.Bytes()
	default:
		panic(fmt.Sprintf("validate/lathe: no schema mapping for column %s.%s of Go type %T",
			col.TableName(), col.ColumnName(), zero))
	}
	c.def(col) // fail fast if the column is unknown
	return s.(validate.Schema[T])
}
