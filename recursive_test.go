package validate_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	validate "github.com/tobibamidele/validate"
)

type Comment struct {
	Body    string
	Replies []Comment
}

var CommentSchema = validate.Recursive(func(self validate.Schema[Comment]) validate.Schema[Comment] {
	return validate.Object[Comment](
		validate.Field("body", validate.String().Trim().Min(1)),
		validate.Field("replies", validate.Array(self).DefaultFunc(func() []Comment { return nil })),
	)
})

var _ validate.Schema[Comment] = CommentSchema

func TestRecursiveParsesTree(t *testing.T) {
	c, err := CommentSchema.Parse(jsonVal(t, `{"body":" top ","replies":[
		{"body":"a","replies":[{"body":"a1"}]},
		{"body":"b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "top" || len(c.Replies) != 2 || c.Replies[0].Replies[0].Body != "a1" || c.Replies[1].Replies != nil {
		t.Fatalf("%+v", c)
	}
}

func TestRecursiveErrorPathsAreExact(t *testing.T) {
	_, err := CommentSchema.Parse(jsonVal(t, `{"body":"top","replies":[
		{"body":"ok"},
		{"body":"x","replies":[{"body":"fine"},{"body":"  "}]}]}`))
	want := []string{"replies[1].replies[1].body:too_small"}
	if got := codes(err); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	ve, _ := validate.AsValidationError(err)
	if len(ve.Field("replies", "1", "replies", "1", "body")) != 1 {
		t.Fatal("Field lookup through recursion")
	}
}

func TestDepthLimitStopsCyclicGoValues(t *testing.T) {
	cyc := map[string]any{"body": "x"}
	cyc["replies"] = []any{cyc} // a real cycle: parsing would never end without a limit
	start := time.Now()
	_, err := CommentSchema.Parse(cyc)
	if got := codes(err); len(got) != 1 || !strings.HasSuffix(got[0], ":too_deep") {
		t.Fatal(got)
	}
	if time.Since(start) > time.Second {
		t.Fatal("too slow")
	}
}

func TestDepthLimitStopsHostileJSON(t *testing.T) {
	body := strings.Repeat(`{"body":"x","replies":[`, 5000) + strings.Repeat(`]}`, 5000)
	start := time.Now()
	_, err := validate.FromJSON([]byte(body), CommentSchema)
	if !validate.IsValidationError(err) || !strings.Contains(err.Error(), "too_deep") {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("too slow")
	}
}

func TestMaxDepthOption(t *testing.T) {
	nest := func(n int) any {
		v := map[string]any{"body": "leaf"}
		for i := 0; i < n; i++ {
			v = map[string]any{"body": "x", "replies": []any{v}}
		}
		return v
	}
	if _, err := validate.ParseContext(bg(), CommentSchema, nest(10)); err != nil {
		t.Fatal("depth 10 under default should pass:", err)
	}
	if _, err := validate.ParseContext(bg(), CommentSchema, nest(10), validate.WithMaxDepth(5)); err == nil {
		t.Fatal("WithMaxDepth(5) should reject depth 10")
	}
	if _, err := validate.ParseContext(bg(), CommentSchema, nest(200)); err == nil {
		t.Fatal("default limit should reject depth 200")
	}
	if _, err := validate.ParseContext(bg(), CommentSchema, nest(200), validate.WithMaxDepth(1000)); err != nil {
		t.Fatal("raised limit should accept depth 200:", err)
	}
	// siblings do not accumulate depth: wide trees are fine
	wide := map[string]any{"body": "r", "replies": func() []any {
		out := make([]any, 500)
		for i := range out {
			out[i] = map[string]any{"body": "c"}
		}
		return out
	}()}
	if _, err := validate.ParseContext(bg(), CommentSchema, wide, validate.WithMaxDepth(3)); err != nil {
		t.Fatal("width must not count as depth:", err)
	}
}

type LinkedNode struct {
	Value int
	Next  *LinkedNode
}

func TestRecursiveOptionalIntoPointer(t *testing.T) {
	s := validate.Recursive(func(self validate.Schema[LinkedNode]) validate.Schema[LinkedNode] {
		return validate.Object[LinkedNode](
			validate.Field("value", validate.Int()),
			validate.Field("next", validate.Optional(self)),
		)
	})
	n, err := s.Parse(jsonVal(t, `{"value":1,"next":{"value":2,"next":{"value":3}}}`))
	if err != nil || n.Next == nil || n.Next.Next == nil || n.Next.Next.Value != 3 || n.Next.Next.Next != nil {
		t.Fatalf("%+v %v", n, err)
	}
	if _, err := s.Parse(jsonVal(t, `{"value":1,"next":null}`)); err == nil {
		t.Fatal("null next must be rejected for Optional (not Nullable)")
	}
}

type File struct {
	Name string
	Size int
}
type Folder struct {
	Name    string
	Files   []File
	Folders []Folder
}

// Mutual recursion needs Lazy; schemas are built in a function, not at package
// level (which would be a Go initialization cycle).
func newFolderSchema() validate.Schema[Folder] {
	fileSchema := validate.Object[File](
		validate.Field("name", validate.String().Min(1)),
		validate.Field("size", validate.Int().Min(0)),
	)
	var folder validate.Schema[Folder]
	folder = validate.Object[Folder](
		validate.Field("name", validate.String().Min(1)),
		validate.Field("files", validate.Array(fileSchema)),
		validate.Field("folders", validate.Array(validate.Lazy(func() validate.Schema[Folder] { return folder }))),
	)
	return folder
}

func TestLazyMutualStyleRecursion(t *testing.T) {
	s := newFolderSchema()
	f, err := s.Parse(jsonVal(t, `{"name":"root","files":[{"name":"a","size":1}],
		"folders":[{"name":"sub","files":[],"folders":[{"name":"deep","files":[{"name":"z","size":9}],"folders":[]}]}]}`))
	if err != nil || f.Folders[0].Folders[0].Files[0].Size != 9 {
		t.Fatalf("%+v %v", f, err)
	}
	_, err = s.Parse(jsonVal(t, `{"name":"root","files":[],"folders":[{"name":"s","files":[{"name":"","size":-1}],"folders":[]}]}`))
	want := []string{"folders[0].files[0].name:too_small", "folders[0].files[0].size:too_small"}
	if got := codes(err); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLazyResolvesOnceAndIsConcurrencySafe(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	var s validate.Schema[Comment]
	lazy := validate.Lazy(func() validate.Schema[Comment] {
		mu.Lock()
		calls++
		mu.Unlock()
		return s
	})
	s = validate.Object[Comment](
		validate.Field("body", validate.String()),
		validate.Field("replies", validate.Array[Comment](lazy).DefaultFunc(func() []Comment { return nil })),
	)
	in := jsonVal(t, `{"body":"a","replies":[{"body":"b","replies":[{"body":"c"}]}]}`)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := s.Parse(in); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("lazy fn ran %d times", calls)
	}
}

func TestRecursiveDefinitionMisuse(t *testing.T) {
	mustPanic(t, "nil lazy fn", func() { validate.Lazy[int](nil) })
	var leaked validate.Schema[int]
	validate.Recursive(func(self validate.Schema[int]) validate.Schema[int] {
		leaked = self
		return validate.Int()
	})
	if v, err := leaked.Parse(5); err != nil || v != 5 {
		t.Fatal("self ref should work after builder returns:", v, err)
	}
	mustPanic(t, "use before build finished", func() {
		validate.Recursive(func(self validate.Schema[int]) validate.Schema[int] {
			_, _ = self.Parse(1) // inner not set yet
			return validate.Int()
		})
	})
}

type Expr interface{ isExpr() }
type Num struct {
	Type  string
	Value float64
}
type Add struct {
	Type        string
	Left, Right Expr
}

func (Num) isExpr() {}
func (Add) isExpr() {}

func TestRecursiveWithDiscriminatedUnion(t *testing.T) {
	expr := validate.Recursive(func(self validate.Schema[Expr]) validate.Schema[Expr] {
		return validate.Discriminated[Expr]("type",
			validate.Variant("num", validate.Object[Num](
				validate.Field("type", validate.String()),
				validate.Field("value", validate.Float64()),
			), func(v Num) Expr { return v }),
			validate.Variant("add", validate.Object[Add](
				validate.Field("type", validate.String()),
				validate.Field("left", self),
				validate.Field("right", self),
			), func(v Add) Expr { return v }),
		)
	})
	e, err := expr.Parse(jsonVal(t, `{"type":"add","left":{"type":"num","value":1},
		"right":{"type":"add","left":{"type":"num","value":2},"right":{"type":"num","value":3}}}`))
	if err != nil {
		t.Fatal(err)
	}
	add := e.(Add)
	if add.Right.(Add).Right.(Num).Value != 3 {
		t.Fatalf("%+v", e)
	}
	_, err = expr.Parse(jsonVal(t, `{"type":"add","left":{"type":"num","value":1},"right":{"type":"mul"}}`))
	if got := codes(err); !reflect.DeepEqual(got, []string{"right.type:invalid_union"}) {
		t.Fatal(got)
	}
}

func BenchmarkRecursiveTree(b *testing.B) {
	in := jsonValB(`{"body":"top","replies":[{"body":"a","replies":[{"body":"a1"},{"body":"a2"}]},{"body":"b"}]}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CommentSchema.Parse(in); err != nil {
			b.Fatal(err)
		}
	}
}
