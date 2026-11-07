package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixtureModule = "example.com/fixture"

func fixture(t *testing.T) *Project {
	t.Helper()
	p, err := Load(filepath.Join("..", "..", "testdata", "analyze"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAndResolve(t *testing.T) {
	p := fixture(t)
	id := fixtureModule + ".Entry"
	fn := p.Functions[id]
	if fn.ID != id || fn.File != "main.go" || fn.Line != 12 || fn.End != 32 {
		t.Fatalf("positions: %+v", fn)
	}
	for _, name := range []string{id, "Entry", "fixture.Entry"} {
		got, err := p.Resolve(name)
		if err != nil || got != id {
			t.Fatalf("Resolve(%q) = %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"", "Leaf", "Absent"} {
		if _, err := p.Resolve(name); err == nil {
			t.Fatalf("Resolve(%q) should fail", name)
		}
	}
	for _, name := range []string{"Thing.Run", "Thing.Pointer", "Generic"} {
		if _, err := p.Resolve(name); err != nil {
			t.Fatal(err)
		}
	}
	// Invalid symbols inside an unselected body do not prevent indexing/calling.
	if _, err := p.Calls(fixtureModule + ".Leaf"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Calls("missing"); err == nil {
		t.Fatal("missing function accepted")
	}
}

func TestCallsOneLevelAndUnknown(t *testing.T) {
	p := fixture(t)
	id := fixtureModule + ".Entry"
	got, err := p.Calls(id)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	unknown := map[string]bool{}
	for _, edge := range got {
		if edge.From != id {
			t.Fatalf("wrong source: %+v", edge)
		}
		if edge.Unknown {
			unknown[edge.To] = true
		} else {
			known[edge.To] = true
		}
	}
	wantKnown := map[string]bool{fixtureModule + ".Recur": true, fixtureModule + "/helper.Work": true, fixtureModule + ".Generic": true, fixtureModule + ".Thing.Run": true, fixtureModule + ".Thing.Pointer": true}
	if !reflect.DeepEqual(known, wantKnown) {
		t.Fatalf("known = %v, want %v", known, wantKnown)
	}
	for _, label := range []string{"unknown: dynamic", "unknown: func literal"} {
		if !unknown[label] {
			t.Errorf("missing %q in %v", label, unknown)
		}
	}
	if len(unknown) != 2 {
		t.Fatalf("unknown = %v", unknown)
	}
	for _, edge := range got {
		if strings.Contains(edge.To, "Hidden") {
			t.Fatalf("traversed nested literal: %+v", edge)
		}
	}
	recur, err := p.Calls(fixtureModule + ".Recur")
	if err != nil {
		t.Fatal(err)
	}
	want := []Edge{{From: fixtureModule + ".Recur", To: fixtureModule + ".Leaf"}, {From: fixtureModule + ".Recur", To: fixtureModule + ".Recur"}}
	if !reflect.DeepEqual(recur, want) {
		t.Fatalf("recursive edges: %v", recur)
	}
	shadow, err := p.Calls(fixtureModule + ".Shadow")
	if err != nil || len(shadow) != 1 || !shadow[0].Unknown {
		t.Fatalf("shadow: %v, %v", shadow, err)
	}
	imported, err := p.Calls(fixtureModule + "/helper.Work")
	if err != nil || !reflect.DeepEqual(imported, []Edge{{From: fixtureModule + "/helper.Work", To: "strings.TrimSpace"}}) {
		t.Fatalf("imported: %v, %v", imported, err)
	}
}

func TestDependencies(t *testing.T) {
	p := fixture(t)
	tests := []struct {
		dir     string
		depth   int
		sources []string
	}{
		{".", 0, []string{"main.go"}},
		{".", 1, []string{"helper/helper.go", "main.go"}},
		{".", -1, []string{"helper/deep/deep.go", "helper/helper.go", "main.go"}},
		{"helper", 0, []string{"helper/helper.go"}},
		{"helper", 1, []string{"helper/deep/deep.go", "helper/helper.go"}},
		{filepath.Join(p.Root, "helper"), -1, []string{"helper/deep/deep.go", "helper/helper.go"}},
	}
	for _, tt := range tests {
		edges, err := p.Dependencies(tt.dir, tt.depth)
		if err != nil {
			t.Fatal(err)
		}
		var sources []string
		for _, edge := range edges {
			sources = append(sources, edge.From)
		}
		if !reflect.DeepEqual(sources, tt.sources) {
			t.Errorf("Dependencies(%q,%d) = %v", tt.dir, tt.depth, edges)
		}
	}
	if !reflect.DeepEqual(p.Imports, []Edge{{From: "helper/deep/deep.go", To: "fmt"}, {From: "helper/helper.go", To: "strings"}, {From: "main.go", To: fixtureModule + "/helper"}}) {
		t.Fatalf("imports = %v", p.Imports)
	}
	for _, tt := range []struct {
		dir   string
		depth int
	}{{"..", -1}, {".", -2}, {"missing", 0}, {"main.go", 0}} {
		if _, err := p.Dependencies(tt.dir, tt.depth); err == nil {
			t.Errorf("accepted invalid directory/depth: %+v", tt)
		}
	}
}

func TestDependenciesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/test\n")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Dependencies("escape", -1); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestLoadErrorsAndExclusions(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(root); err == nil {
		t.Fatal("missing module accepted")
	}
	write(t, root, "go.mod", "go 1.22\n")
	if _, err := Load(root); err == nil {
		t.Fatal("missing module directive accepted")
	}
	write(t, root, "go.mod", "module example.com/test\n")
	write(t, root, "bad.go", "package test\nfunc (")
	if _, err := Load(root); err == nil {
		t.Fatal("parse error accepted")
	}
	write(t, root, "bad.go", "package test\nfunc Good() {}\n")
	for _, name := range []string{"bad_test.go", "vendor/bad.go", ".hidden/bad.go"} {
		write(t, root, name, "not go")
	}
	write(t, root, "nested/go.mod", "module nested\n")
	write(t, root, "nested/nested.go", "package nested\nfunc Nested() {}\n")
	p, err := Load(root)
	if err != nil || len(p.Functions) != 2 || p.Functions["nested.Nested"].File != "nested/nested.go" {
		t.Fatalf("exclusions: %v, %v", p, err)
	}
	write(t, root, "duplicate.go", "package test\nfunc Good() {}\n")
	if _, err := Load(root); err == nil {
		t.Fatal("duplicate declaration accepted")
	}
}

func TestCyclesAndMermaid(t *testing.T) {
	p := fixture(t)
	a, err := p.Calls(fixtureModule + ".CycleA")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Calls(fixtureModule + ".CycleB")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 || a[0].To != b[0].From || b[0].To != a[0].From {
		t.Fatalf("cycle: %v %v", a, b)
	}
	edges := []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}, {From: "b", To: "x\"\n<>&`[]#", Unknown: true}, {From: "a", To: "b"}}
	before := append([]Edge(nil), edges...)
	got := Mermaid(edges)
	want := "flowchart TD\n  n0[\"a\"]\n  n1[\"b\"]\n  n2[\"x#34;#10;#60;#62;#38;#96;#91;#93;#35;\"]\n  n0 --> n1\n  n1 --> n0\n  n1 -.-> n2\n"
	if got != want {
		t.Fatalf("Mermaid:\n%s\nwant:\n%s", got, want)
	}
	if !reflect.DeepEqual(edges, before) {
		t.Fatal("renderer mutated input")
	}
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}
	if Mermaid(edges) != got {
		t.Fatal("unstable rendering")
	}
	if Mermaid(nil) != "flowchart TD\n" {
		t.Fatal("empty graph")
	}
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
