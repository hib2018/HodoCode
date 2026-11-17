package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolyglotLanguages(t *testing.T) {
	cases := []struct{ language, file, source string }{
		{"zig", "main.zig", "const std = @import(\"std\");\nfn helper() void {}\npub fn main() void { helper(); dynamic(); }\n"},
		{"ts", "main.ts", "import {x} from 'pkg';\nfunction helper(): void {}\nfunction main(): void { helper(); obj.run(); }\nconst arrow = (x: number) => helper();\nclass Thing { run() { helper(); } }\n"},
		{"rust", "main.rs", "use std::io;\nfn helper() {}\nfn main() { helper(); value.run(); }\nstruct Thing;\nimpl Thing { fn run(&self) { helper(); } }\n"},
		{"c", "main.c", "#include <stdio.h>\nvoid helper(void) {}\nint main(void) { helper(); dynamic(); return 0; }\n"},
		{"js", "main.js", "import 'pkg';\nfunction helper() {}\nfunction main() { helper(); obj.run(); }\nconst arrow = () => { helper(); };\nclass Thing { run() { helper(); } }\n"},
		{"py", "main.py", "from pkg import thing\ndef helper():\n    pass\ndef main():\n    helper()\n    obj.run()\nclass Thing:\n    def run(self):\n        helper()\n"},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			root := t.TempDir()
			filename := filepath.Join(root, tc.file)
			if err := os.WriteFile(filename, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := loadPolyglot(root, "", tc.language)
			if err != nil {
				t.Fatal(err)
			}
			main := tc.file + ".main"
			helper := tc.file + ".helper"
			if _, ok := p.Functions[main]; !ok {
				t.Fatalf("main missing: %#v", p.Functions)
			}
			if len(p.Imports) == 0 {
				t.Fatal("missing imports")
			}
			sites, err := p.polyCallSites(main)
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != 2 {
				t.Fatalf("sites=%#v", sites)
			}
			if sites[0].Unknown || sites[0].To != helper || !sites[1].Unknown {
				t.Fatalf("sites=%#v", sites)
			}
			source, err := p.polySource(main)
			if err != nil {
				t.Fatal(err)
			}
			pos, err := p.polyPosition(main)
			if err != nil {
				t.Fatal(err)
			}
			realFilename, err := filepath.EvalSymlinks(filename)
			if err != nil {
				t.Fatal(err)
			}
			if pos.Filename != realFilename || source != tc.source[pos.Offset:pos.Offset+len(source)] {
				t.Fatalf("position=%#v source=%q", pos, source)
			}
			for _, site := range sites {
				if source[site.Offset:site.Offset+len(site.Name)] != site.Name {
					t.Fatalf("bad relative offset: %#v", site)
				}
				line := strings.Split(tc.source, "\n")[site.Line-1]
				if line[site.Column-1:site.EndColumn-1] != site.Name {
					t.Fatalf("bad position: %#v", site)
				}
			}
			mains := p.polyMainFunctions()
			if len(mains) != 1 || mains[0] != main {
				t.Fatalf("mains=%v", mains)
			}
			if tc.language == "js" || tc.language == "ts" {
				if _, ok := p.Functions[tc.file+".arrow"]; !ok {
					t.Fatal("arrow missing")
				}
			}
			if tc.language == "js" || tc.language == "ts" || tc.language == "rust" || tc.language == "py" {
				if _, ok := p.Functions[tc.file+".Thing.run"]; !ok {
					t.Fatalf("method missing: %#v", p.Functions)
				}
			}
		})
	}
}

func TestPolyglotUnknownAndNested(t *testing.T) {
	root := t.TempDir()
	source := "function helper() {}\nfunction main(helper) {\n helper();\n function nested() { hidden(); }\n const closure = () => { hidden(); };\n object.helper();\n}\n"
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := p.polyCallSites("a.js.main")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("nested leaked: %#v", sites)
	}
	for _, site := range sites {
		if !site.Unknown {
			t.Fatalf("guessed dynamic call: %#v", site)
		}
	}
}

func TestPolyglotAutoAndExclusions(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{"go.mod": "module example.test/m\n\ngo 1.23\n", "main.go": "package main\nfunc main() {}\n", "a.py": "def hello():\n    pass\n", "node_modules/dep.js": "function skipped() {}", "target/dep.rs": "fn skipped() {}"} {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := loadPolyglot(root, "", "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 2 {
		t.Fatalf("functions=%#v", p.Functions)
	}
	p, err = loadPolyglot(root, "", "python")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 1 {
		t.Fatalf("explicit language included unrelated files: %#v", p.Functions)
	}
	if _, err := loadPolyglot(root, "", "ruby"); err == nil {
		t.Fatal("unsupported language accepted")
	}
}
