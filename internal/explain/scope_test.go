package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func TestDeclarationOffsetsAndKinds(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.org/scope\n\ngo 1.23\n")
	write(t, root, "scope.go", "package scope\nvar Outside = 123; func A() { println(1) }; func B() { println(2) }; var After = 456\ntype Box[T any] struct{}\nfunc (b *Box[T]) Method() { println(3) }\nfunc init() { println(4) }\n")
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Prepare(p, "example.org/scope.A", "explain A", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.snippets["example.org/scope.A"] != "func A() { println(1) }" {
		t.Fatal(r.snippets["example.org/scope.A"])
	}
	if r.snippets["example.org/scope.Box.Method"] != "func (b *Box[T]) Method() { println(3) }" {
		t.Fatal("method missing")
	}
	if r.snippets["example.org/scope.init@scope.go:5"] != "func init() { println(4) }" {
		t.Fatal("init missing")
	}
	calls := 0
	_, err = r.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		if calls == 1 {
			return `["example.org/scope.A"]`, nil
		}
		for _, outside := range []string{"Outside", "After", "func B", "Method", "func init"} {
			if strings.Contains(prompt, outside) {
				t.Errorf("out of scope: %s", outside)
			}
		}
		return "done", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("%v calls=%d", err, calls)
	}
	// Stale line metadata cannot expand a declaration into adjacent code.
	fn := p.Functions["example.org/scope.A"]
	fn.End++
	p.Functions[fn.ID] = fn
	if _, err := Prepare(p, "example.org/scope.A", "purpose", nil, nil); err == nil {
		t.Fatal("accepted stale line range")
	}
}

func TestLineDirectivesUsePhysicalLines(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.org/directive\n\ngo 1.23\n")
	write(t, root, "scope.go", "package directive\n//line invented.go:100\nfunc F() {}\n")
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Prepare(p, "example.org/directive.F", "purpose", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Preview(), "scope.go:3-3") || strings.Contains(r.Preview(), "invented.go") {
		t.Fatalf("not physical source positions: %s", r.Preview())
	}
}

func TestTwelveSelectionsWithoutDroppingContent(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.org/twelve\n\ngo 1.23\n")
	var code strings.Builder
	code.WriteString("package twelve\n")
	ids := []string{}
	for i := 0; i < maxSelections; i++ {
		fmt.Fprintf(&code, "func F%d() { println(%d) }\n", i, i)
		ids = append(ids, fmt.Sprintf("example.org/twelve.F%d", i))
	}
	write(t, root, "scope.go", code.String())
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Prepare(p, ids[0], "purpose", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := json.Marshal(ids)
	calls := 0
	_, err = r.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		if calls == 1 {
			return string(selection), nil
		}
		for i := 0; i < maxSelections; i++ {
			if !strings.Contains(prompt, fmt.Sprintf("func F%d()", i)) {
				t.Errorf("dropped F%d", i)
			}
		}
		return "done", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("%v calls=%d", err, calls)
	}
}
