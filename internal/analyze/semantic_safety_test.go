package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantic"
	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestSemanticDynamicCandidates(t *testing.T) {
	for _, tc := range []struct{ file, source string }{
		{"a.py", "@decorate\ndef helper():\n    pass\ndef main():\n    helper()\n"},
		{"a.js", "function helper() {}\nfunction main() { object.helper(); }\n"},
		{"a.rs", "fn helper() {}\nfn main() { T::helper(); }\n"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			semantictest.Use(t)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := LoadLanguage(root, "auto")
			if err != nil {
				t.Fatal(err)
			}
			sites, err := p.CallSites(tc.file + ".main")
			if err != nil || len(sites) != 1 || !sites[0].Unknown || len(sites[0].Candidates) != 1 {
				t.Fatalf("unproven dispatch: %+v, %v", sites, err)
			}
		})
	}
}

func TestSemanticThirdFileMutation(t *testing.T) {
	semantictest.Use(t)
	root := t.TempDir()
	for name, source := range map[string]string{
		"main.py":   "import patch\nfrom target import helper\ndef main():\n    helper()\n",
		"target.py": "def helper():\n    pass\n",
		"patch.py":  "import target\ntarget.helper = other\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadLanguage(root, "python")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := p.CallSites("main.py.main")
	if err != nil || len(sites) != 1 || !sites[0].Unknown || len(sites[0].Candidates) != 1 {
		t.Fatalf("third-file mutation: %+v, %v", sites, err)
	}
}

func TestSemanticFlatSymbolExtent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.js")
	source := "function main() { helper(); }\nfunction helper() {}\n"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "js")
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(p.Root, "a.js")
	// SymbolInformation is allowed to provide a name-only location.
	_, err = p.indexSemanticSymbols(file, "javascript", []semantic.Symbol{{Name: "main", Kind: 12, Range: semantic.Range{Start: semantic.Position{Character: 9}, End: semantic.Position{Character: 13}}}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.Source("a.js.main")
	if err != nil || !strings.Contains(text, "helper()") {
		t.Fatalf("source extent: %q, %v", text, err)
	}
	sites, err := p.polyCallSites("a.js.main")
	if err != nil || len(sites) != 1 || sites[0].Offset+len(sites[0].Name) > len(text) {
		t.Fatalf("call extent: %+v, %v", sites, err)
	}
}

func TestSemanticSameNameVariableIsNotFunction(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.js")
	source := "function f() { let f = 1; }\n"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "js")
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(p.Root, "a.js")
	decls, err := p.indexSemanticSymbols(file, "javascript", []semantic.Symbol{{Name: "f", Kind: 13, Range: semantic.Range{Start: semantic.Position{Character: 19}, End: semantic.Position{Character: 24}}, SelectionRange: semantic.Range{Start: semantic.Position{Character: 19}, End: semantic.Position{Character: 20}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 0 {
		t.Fatalf("variable became function: %+v", decls)
	}
}
