package analyze

import (
	"reflect"
	"testing"
)

func TestConcreteAndInterfaceMethods(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/test\n")
	write(t, root, "other/other.go", `package other
 type Value struct{}
 func (Value) Run() {}
 func (*Value) Pointer() {}
 type Generic[T any] struct{}
 func (Generic[T]) Run() {}
`)
	write(t, root, "main.go", `package test
import "example.com/test/other"
type Runner interface { Run() }
type Embedded struct { other.Value }
type Dynamic struct { Runner }
type NamedInterface Runner
func Generic[A, B any]() {}
func Entry(v other.Value, p *other.Value, i Runner, n NamedInterface, e Embedded, d Dynamic, g other.Generic[int]) {
 v.Run()
 p.Run()
 v.Pointer()
 p.Pointer()
 other.Value.Run(v)
 (*other.Value).Run(p)
 (*other.Value).Pointer(p)
 e.Run()
 g.Run()
 other.Generic[int].Run(g)
 i.Run()
 Runner.Run(i)
 n.Run()
 d.Run()
 method := v.Run
 method()
 expression := other.Value.Run
 expression(v)
 pointer := p.Pointer
 pointer()
 Generic[int, string]()
}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/test.Entry"
	got, err := p.Calls(id)
	if err != nil {
		t.Fatal(err)
	}
	want := []Edge{
		{From: id, To: "example.com/test.Generic"},
		{From: id, To: "example.com/test/other.Generic.Run"},
		{From: id, To: "example.com/test/other.Value.Pointer"},
		{From: id, To: "example.com/test/other.Value.Run"},
		{From: id, To: "unknown: Runner.Run", Unknown: true},
		{From: id, To: "unknown: d.Run", Unknown: true},
		{From: id, To: "unknown: expression", Unknown: true},
		{From: id, To: "unknown: i.Run", Unknown: true},
		{From: id, To: "unknown: method", Unknown: true},
		{From: id, To: "unknown: n.Run", Unknown: true},
		{From: id, To: "unknown: pointer", Unknown: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Calls = %+v, want %+v", got, want)
	}
}

func TestPhysicalPositions(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/test\n")
	write(t, root, "main.go", `package test
//line forged.go:900
func Leaf() {
}
//line forged.go:900
func init() {}
//line forged.go:900
func init() {}
`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Function{
		"example.com/test.Leaf":           {ID: "example.com/test.Leaf", File: "main.go", Line: 3, End: 4},
		"example.com/test.init@main.go:6": {ID: "example.com/test.init@main.go:6", File: "main.go", Line: 6, End: 6},
		"example.com/test.init@main.go:8": {ID: "example.com/test.init@main.go:8", File: "main.go", Line: 8, End: 8},
	}
	if !reflect.DeepEqual(p.Functions, want) {
		t.Fatalf("Functions = %+v, want %+v", p.Functions, want)
	}
}

func TestMermaidNodes(t *testing.T) {
	p := fixture(t)
	id := fixtureModule + ".Leaf"
	edges, err := p.Calls(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := MermaidNodes(edges, []string{id}), "flowchart TD\n  n0[\"example#46;com#47;fixture#46;Leaf\"]\n"; got != want {
		t.Fatalf("leaf graph = %q, want %q", got, want)
	}
	edges = []Edge{{From: "b", To: "a"}, {From: "b", To: "a"}}
	nodes := []string{"z\"", "a", "z\""}
	before := append([]string(nil), nodes...)
	want := "flowchart TD\n  n0[\"a\"]\n  n1[\"b\"]\n  n2[\"z#34;\"]\n  n1 --> n0\n"
	if got := MermaidNodes(edges, nodes); got != want {
		t.Fatalf("graph = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(nodes, before) {
		t.Fatal("renderer mutated nodes")
	}
	if MermaidNodes(edges, nil) != Mermaid(edges) {
		t.Fatal("Mermaid compatibility")
	}
}
