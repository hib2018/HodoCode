package analyze

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/types"
	"reflect"
	"strings"
	"testing"
)

func TestTypeFilesOmitClosureBodies(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/test\n")
	write(t, root, "main.go", `package test
 type Value struct{}
 func (Value) Run() {}
 var Handler = func(v Value) { packageBodyMustNotBeChecked(); v.Run() }
 var Handlers = []func(Value){func(v Value) { compositeBodyMustNotBeChecked() }}
 func Other() { unselectedBodyMustNotBeChecked() }
 func Entry(v Value) {
   v.Run()
   Handler(v)
   Handlers[0](v)
   local := func(x Value) {
     nestedBodyMustNotBeChecked()
     deeper := func() { deeperBodyMustNotBeChecked() }
     deeper()
   }
   local(v)
   (func() Value { immediateBodyMustNotBeChecked(); return v }()).Run()
 }
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	const pkg = "example.com/test"
	id := pkg + ".Entry"
	selected := p.declarations[id]
	snapshot := func() string {
		var b bytes.Buffer
		for _, file := range p.packages[pkg] {
			if err := printer.Fprint(&b, p.fset, file); err != nil {
				t.Fatal(err)
			}
		}
		return b.String()
	}
	before := snapshot()
	for _, selection := range []*ast.FuncDecl{selected, nil} {
		files, body := p.typeFiles(pkg, selection)
		if selection != nil {
			if body == nil {
				t.Fatal("selected body omitted")
			}
			// Unaffected expressions must keep their original identities.
			if body.List[0] != selected.Body.List[0] {
				t.Fatal("copied an unaffected statement")
			}
		} else if body != nil {
			t.Fatal("unexpected selected body")
		}
		literals := 0
		for _, file := range files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.FuncDecl:
					if selection == nil || n.Pos() != selection.Pos() {
						if n.Body != nil {
							t.Errorf("unselected declaration %s retains body", n.Name)
						}
					}
				case *ast.FuncLit:
					literals++
					if n.Body == nil || len(n.Body.List) != 0 {
						t.Error("closure body not replaced with empty block")
					}
				case *ast.Ident:
					if strings.Contains(n.Name, "MustNotBeChecked") {
						t.Errorf("unselected body in checker input: %s", n.Name)
					}
				}
				return true
			})
		}
		wantLiterals := 2
		if selection != nil {
			wantLiterals = 4
		}
		if literals != wantLiterals {
			t.Fatalf("literals = %d, want %d", literals, wantLiterals)
		}
		config := types.Config{Error: func(err error) {
			// Empty result-bearing closures may report missing return.
			if !strings.Contains(err.Error(), "missing return") {
				t.Errorf("type check: %v", err)
			}
		}}
		_, _ = config.Check(pkg, p.fset, files, nil)
	}
	want := []Edge{
		{From: id, To: pkg + ".Value.Run"},
		{From: id, To: "unknown: Handler", Unknown: true},
		{From: id, To: "unknown: Handlers[0]", Unknown: true},
		{From: id, To: "unknown: func literal", Unknown: true},
		{From: id, To: "unknown: local", Unknown: true},
	}
	for i := 0; i < 2; i++ {
		got, err := p.Calls(id)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Calls = %+v, %v; want %+v", got, err, want)
		}
	}
	if after := snapshot(); after != before {
		t.Fatal("typeFiles or Calls mutated the index AST")
	}
}
