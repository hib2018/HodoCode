package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolyglotOpaqueTextAndDynamicCalls(t *testing.T) {
	root := t.TempDir()
	source := "function helper() {}\nfunction main() {\n const text = 'fake()'; // hidden()\n helper();\n callbacks[index]();\n factory()();\n}\n"
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "js")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := p.polyCallSites("a.js.main")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 4 {
		t.Fatalf("sites=%#v", sites)
	}
	if sites[0].Unknown {
		t.Fatalf("direct function unresolved: %#v", sites[0])
	}
	for _, site := range sites[1:] {
		if !site.Unknown {
			t.Fatalf("dynamic call guessed: %#v", site)
		}
	}
}

func TestPolyglotZigMethods(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.zig"), []byte("const Thing = struct { pub fn run(self: *Thing) void { dynamic(); } };\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "zig")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Functions["a.zig.Thing.run"]; !ok {
		t.Fatalf("functions=%#v", p.Functions)
	}
}

func TestPolyglotModuleBoundary(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	for filename, source := range map[string]string{filepath.Join(root, "go.mod"): "module example.test/m\n\ngo 1.23\n", filepath.Join(sub, "a.go"): "package sub\nfunc Hello() {}\n", filepath.Join(sub, "a.py"): "def hello():\n    pass\n"} {
		if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := loadPolyglot(sub, root, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Functions["example.test/m/sub.Hello"]; !ok {
		t.Fatalf("Go source not loaded: %#v", p.Functions)
	}
}
