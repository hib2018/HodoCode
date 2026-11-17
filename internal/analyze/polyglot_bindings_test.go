package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolyglotConservativeBindings(t *testing.T) {
	cases := []struct{ file, source string }{
		{"a.c", "void helper(void) {}\nvoid main(void) { void (*helper)(void); helper(); }"},
		{"a.c", "void helper(void) {}\nvoid main(void) { Callable helper; helper(); }"},
		{"a.py", "def helper():\n    pass\nhelper = other\ndef main():\n    helper()\n"},
		{"a.js", "function helper() {}\nfunction main() { const {helper} = object; helper(); }"},
		{"a.js", "function helper() {}\nfunction main() { try {} catch (helper) { helper(); } }"},
	}
	for _, tc := range cases {
		t.Run(tc.file+tc.source, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := loadPolyglot(root, "", "auto")
			if err != nil {
				t.Fatal(err)
			}
			sites, err := p.polyCallSites(tc.file + ".main")
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != 1 || !sites[0].Unknown {
				t.Fatalf("guessed binding: %#v", sites)
			}
		})
	}
}

func TestPolyglotPythonAsyncAndImports(t *testing.T) {
	root := t.TempDir()
	source := "from pkg.sub import thing\nimport one, two as alias\ndef helper():\n    pass\nasync def main():\n    helper(\n0,\n    )\n    helper()\ndef after():\n    pass\n"
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 3 {
		t.Fatalf("functions=%#v", p.Functions)
	}
	src, err := p.polySource("a.py.main")
	if err != nil {
		t.Fatal(err)
	}
	if len(src) < 5 || src[:5] != "async" {
		t.Fatalf("async prefix omitted: %q", src)
	}
	sites, err := p.polyCallSites("a.py.main")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("multiline body truncated: %#v", sites)
	}
	want := map[string]bool{"pkg.sub": true, "one": true, "two": true}
	if len(p.Imports) != len(want) {
		t.Fatalf("imports=%#v", p.Imports)
	}
	for _, edge := range p.Imports {
		if !want[edge.To] {
			t.Fatalf("unexpected import: %#v", edge)
		}
	}
}

func TestPolyglotArrowArguments(t *testing.T) {
	root := t.TempDir()
	source := "function helper() {}\nfunction main() { list.map(x => hidden(), helper()); helper(); }"
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
	if len(sites) != 3 || sites[1].Unknown || sites[2].Unknown {
		t.Fatalf("arrow skip crossed argument boundary: %#v", sites)
	}
}
