package analyze

import (
	"reflect"
	"strings"
	"testing"
)

func TestCallSites(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/sites\n")
	source := `package sample
import (
 "strings"
 "example.invalid/missing"
)
type Value struct{}
func (Value) Run() {}
func Leaf() {}
func Generic[A, B any]() {}
func 日本語() {}
func Factory() Value { return Value{} }
func Entry(v Value, dynamic func(), i interface{ Run() }) {
//line forged.go:900:70
	Leaf(); Leaf()
	v.Run()
	(Value.Run)(v)
	(*Value).Run(&v)
	Generic[int, string]()
	(Generic[int, string])()
	strings.TrimSpace("x")
	missing.Work()
	dynamic()
	i.Run()
	日本語(); Leaf()
	Factory().Run()
	_ = int(1); _ = Value(v); _ = len("x"); println("x")
	_ = func() { Leaf() }
	func() { Leaf() }()
	go Leaf()
	defer Leaf()
	Absent()
}
func Empty() {}
func NoBody()
`
	write(t, root, "main.go", source)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/sites.Entry"
	got, err := p.CallSites(id)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		line    int
		name    string
		target  string
		unknown bool
		last    bool
	}{
		{14, "Leaf", "example.com/sites.Leaf", false, false},
		{14, "Leaf", "example.com/sites.Leaf", false, true},
		{15, "Run", "example.com/sites.Value.Run", false, false},
		{16, "Run", "example.com/sites.Value.Run", false, false},
		{17, "Run", "example.com/sites.Value.Run", false, false},
		{18, "Generic", "example.com/sites.Generic", false, false},
		{19, "Generic", "example.com/sites.Generic", false, false},
		{20, "TrimSpace", "strings.TrimSpace", false, false},
		{21, "Work", "unknown: missing.Work", true, false},
		{22, "dynamic", "unknown: dynamic", true, false},
		{23, "Run", "unknown: i.Run", true, false},
		{24, "日本語", "example.com/sites.日本語", false, false},
		{24, "Leaf", "example.com/sites.Leaf", false, false},
		{25, "Factory", "example.com/sites.Factory", false, false},
		{25, "Run", "example.com/sites.Value.Run", false, false},
		{28, "", "unknown: func literal", true, false},
		{29, "Leaf", "example.com/sites.Leaf", false, false},
		{30, "Leaf", "example.com/sites.Leaf", false, false},
		{31, "Absent", "unknown: Absent", true, false},
	}
	lines := strings.Split(source, "\n")
	var want []CallSite
	for _, check := range checks {
		name := check.name
		search := name
		if search == "" {
			search = "func"
		}
		column := strings.Index(lines[check.line-1], search) + 1
		if check.last {
			column = strings.LastIndex(lines[check.line-1], search) + 1
		}
		offset := len(strings.Join(lines[:check.line-1], "\n")) + 1 + column - 1 - strings.Index(source, "func Entry")
		want = append(want, CallSite{
			Edge: Edge{From: id, To: check.target, Unknown: check.unknown},
			Line: check.line, Column: column, EndColumn: column + len(name), Offset: offset, Name: name,
		})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CallSites = %+v\nwant %+v", got, want)
	}
	// Calls still deduplicates every repeated target for flow consumers.
	edges, err := p.Calls(id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[Edge]bool{}
	var wantEdges []Edge
	for _, site := range want {
		if !seen[site.Edge] {
			seen[site.Edge] = true
			wantEdges = append(wantEdges, site.Edge)
		}
	}
	sortEdges(wantEdges)
	if !reflect.DeepEqual(edges, wantEdges) {
		t.Fatalf("Calls = %+v, want %+v", edges, wantEdges)
	}
	for _, name := range []string{"Empty", "NoBody"} {
		sites, err := p.CallSites("example.com/sites." + name)
		if err != nil || len(sites) != 0 {
			t.Fatalf("CallSites(%s) = %+v, %v", name, sites, err)
		}
	}
	if _, err := p.CallSites("Entry"); err == nil {
		t.Fatal("CallSites accepted a non-exact ID")
	}
	// Repeated analysis must not mutate the indexed AST or lose occurrences.
	again, err := p.CallSites(id)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("repeated CallSites = %+v, %v", again, err)
	}
}

func TestCallSiteOffsetsFromSource(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/offsets\n")
	declaration := "func Entry(v Value) { 日本語(); v.Run(); (func() { 日本語() })()\n\t日本語()\n}"
	write(t, root, "main.go", "package sample\ntype Value struct{}\nfunc (Value) Run() {}\nfunc 日本語() {}\n//line forged.go:900:70\n\t  "+declaration+"\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/offsets.Entry"
	source, err := p.Source(id)
	if err != nil || source != declaration {
		t.Fatalf("Source = %q, %v", source, err)
	}
	sites, err := p.CallSites(id)
	if err != nil {
		t.Fatal(err)
	}
	wantOffsets := []int{
		strings.Index(source, "日本語"),
		strings.Index(source, "Run"),
		strings.Index(source, "func()"),
		strings.LastIndex(source, "日本語"),
	}
	wantNames := []string{"日本語", "Run", "", "日本語"}
	if len(sites) != len(wantOffsets) {
		t.Fatalf("CallSites = %+v", sites)
	}
	for i, site := range sites {
		if site.Offset != wantOffsets[i] || site.Name != wantNames[i] {
			t.Fatalf("site %d = %+v, want offset %d, name %q", i, site, wantOffsets[i], wantNames[i])
		}
		if got := source[site.Offset : site.Offset+len(site.Name)]; got != site.Name {
			t.Fatalf("source range = %q, want %q", got, site.Name)
		}
	}
	if sites[0].Column != sites[0].Offset+4 || sites[0].Line != 6 {
		t.Fatalf("indented first-line call: %+v", sites[0])
	}
}

func TestMainFunctions(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/mains\n")
	write(t, root, "main.go", "package library\nfunc main() {}\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.MainFunctions(); len(got) != 0 {
		t.Fatalf("library main included: %v", got)
	}
	write(t, root, "cmd/z/main.go", "package main\nfunc main() {}\nfunc Main() {}\nfunc init() {}\ntype Value struct{}\nfunc (Value) main() {}\n")
	write(t, root, "cmd/a/main.go", "package main\nfunc main() {}\n")
	write(t, root, "cmd/method/main.go", "package main\ntype Value struct{}\nfunc (*Value) main() {}\n")
	write(t, root, "nested/go.mod", "module example.com/nested\n")
	write(t, root, "nested/main.go", "package main\nfunc main() {}\n")
	p, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/mains/cmd/a.main", "example.com/mains/cmd/z.main", "example.com/nested.main"}
	for range 3 {
		if got := p.MainFunctions(); !reflect.DeepEqual(got, want) {
			t.Fatalf("MainFunctions = %v, want %v", got, want)
		}
	}
}
