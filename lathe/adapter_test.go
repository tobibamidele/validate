package lathe_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tobibamidele/lathe"
	"github.com/tobibamidele/lathe/schema"
	validate "github.com/tobibamidele/validate"
	validatelathe "github.com/tobibamidele/validate/lathe"
)

type Status string

func catalog(t *testing.T) *validatelathe.Catalog {
	t.Helper()
	s := schema.New(schema.Config{Dialect: schema.Postgres},
		schema.Table("users",
			schema.UUID("id").PrimaryKey(),
			schema.VarChar("email", 255).Unique(),
			schema.Text("bio"),
			schema.Enum("status", "active", "banned"),
			schema.Int("age"),
		),
	)
	exp, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	return validatelathe.NewCatalog(exp.Snapshot)
}

// Columns are built exactly the way Lathe's generated code builds them.
var (
	emailCol  = lathe.NewColumn[string]("users", "email")
	bioCol    = lathe.NewColumn[string]("users", "bio")
	statusCol = lathe.NewColumn[Status]("users", "status")
	idCol     = lathe.NewColumn[uuid.UUID]("users", "id")
)

func TestVarCharMapsToMaxRunes(t *testing.T) { // spec #104
	s := validatelathe.String(catalog(t), emailCol).Trim().Lower()
	for n, ok := range map[int]bool{254: true, 255: true, 256: false} {
		_, err := s.Parse(strings.Repeat("a", n))
		if (err == nil) != ok {
			t.Errorf("%d chars: err=%v", n, err)
		}
	}
	// 255 multi-byte runes are 510 bytes but within the column limit.
	if _, err := s.Parse(strings.Repeat("é", 255)); err != nil {
		t.Fatal(err)
	}
}

func TestUniqueIsNotValidation(t *testing.T) { // spec #105
	s := validatelathe.String(catalog(t), emailCol)
	// Parsing twice must succeed: nothing remembers or queries uniqueness.
	for i := 0; i < 2; i++ {
		if _, err := s.Parse("same@example.com"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTextAddsNoConstraint(t *testing.T) {
	if _, err := validatelathe.String(catalog(t), bioCol).Parse(strings.Repeat("x", 100000)); err != nil {
		t.Fatal(err)
	}
}

func TestEnumColumn(t *testing.T) {
	s := validatelathe.Enum(catalog(t), statusCol)
	v, err := s.Parse("active")
	if err != nil || v != Status("active") {
		t.Fatal(v, err)
	}
	if _, err := s.Parse("deleted"); err == nil {
		t.Fatal("unknown enum value accepted")
	}
}

func TestFromColumnAndObject(t *testing.T) {
	cat := catalog(t)
	var _ validate.Schema[uuid.UUID] = validatelathe.FromColumn(cat, idCol)
	type CreateUser struct{ Email string }
	schema := validate.Object[CreateUser](validate.Field("email", validatelathe.String(cat, emailCol).Trim().Lower().Email()))
	u, err := schema.Parse(map[string]any{"email": " A@B.io "})
	if err != nil || u.Email != "a@b.io" {
		t.Fatal(u, err)
	}
}

func TestUnknownColumnPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	validatelathe.String(catalog(t), lathe.NewColumn[string]("users", "nope"))
}
