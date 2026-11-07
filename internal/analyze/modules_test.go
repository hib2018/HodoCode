package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModulePath(t *testing.T) {
	for _, input := range []string{
		"module example.com/lib\n",
		"// comment\nmodule \"example.com/lib\" // comment\ngo 1.22\n",
		"module `example.com/lib`\n",
		"/* comment */ module example.com/lib /* comment */\n",
		"module example.com/lib\nreplace example.com/x => \"https://not-a-comment\"\n",
	} {
		got, err := modulePath(input)
		if err != nil || got != "example.com/lib" {
			t.Errorf("modulePath(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{
		"", "go 1.22\n", "module\n", "module a b\n", "module \"\"\n",
		"module a\nmodule b\n", "module \"unterminated\n", "module a /* unterminated",
		"module \"example.com//lib\"\n", "module ../outside\n", "module /absolute\n",
		"module example.com/../lib\n", "module \"a b\"\n", "module \"a\\nb\"\n",
		"module example.com/lib/\n", "module example.com/lib@v1\n",
	} {
		if got, err := modulePath(input); err == nil {
			t.Errorf("modulePath(%q) accepted %q", input, got)
		}
	}
}

func TestLoadRepositoryModules(t *testing.T) {
	for _, rootModule := range []bool{true, false} {
		t.Run(map[bool]string{true: "root-and-nested", false: "sibling-modules"}[rootModule], func(t *testing.T) {
			root := t.TempDir()
			entryDir := "app/"
			if rootModule {
				entryDir = ""
			} else {
				// Invalid Go outside every module must not even be parsed.
				write(t, root, "unowned.go", "not Go")
			}
			write(t, root, entryDir+"go.mod", `module "example.com/app"
require example.com/lib v0.0.0
replace example.com/lib => ./unused
`)
			entry := `package app
import (
 alias "example.com/lib"
 . "example.com/tools/util"
 external "invalid.example/not-indexed"
)
func Entry(v alias.Value) {
 alias.Same()
 v.Run()
 alias.Value.Run(v)
 v.Pointer()
 Work()
 external.Same()
 Same()
}
func Same() { missingInUnselectedBody() }
`
			write(t, root, entryDir+"entry.go", entry)
			write(t, root, "modules/lib/go.mod", "module example.com/lib\n")
			leaf := "func Same() {\n missingInLeafBody()\n}\n"
			write(t, root, "modules/lib/lib.go", "package lib\n"+leaf+`type Value struct{}
func (Value) Run() { missingInMethodBody() }
func (*Value) Pointer() {}
`)
			write(t, root, "modules/tools/go.mod", "module example.com/tools\n")
			write(t, root, "modules/tools/util/util.go", "package util\nfunc Work() { missingInDotImportedBody() }\nfunc Other() {}\n")
			p, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Functions) != 7 {
				t.Fatalf("functions = %v", p.Functions)
			}
			id := "example.com/app.Entry"
			got, err := p.Calls(id)
			want := []Edge{
				{From: id, To: "example.com/app.Same"},
				{From: id, To: "example.com/lib.Same"},
				{From: id, To: "example.com/lib.Value.Pointer"},
				{From: id, To: "example.com/lib.Value.Run"},
				{From: id, To: "example.com/tools/util.Work"},
				{From: id, To: "unknown: external.Same", Unknown: true},
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("Calls = %+v, %v; want %+v", got, err, want)
			}
			if _, err := p.Resolve("Same"); err == nil {
				t.Fatal("ambiguous short name accepted")
			}
			fn := p.Functions["example.com/lib.Same"]
			if fn.File != "modules/lib/lib.go" || fn.Line != 2 || fn.End != 4 {
				t.Fatalf("location = %+v", fn)
			}
			if source, err := p.Source(fn.ID); err != nil || source != strings.TrimSuffix(leaf, "\n") {
				t.Fatalf("Source = %q, %v", source, err)
			}
			wantImports := []Edge{
				{From: entryDir + "entry.go", To: "example.com/lib"},
				{From: entryDir + "entry.go", To: "example.com/tools/util"},
				{From: entryDir + "entry.go", To: "invalid.example/not-indexed"},
			}
			if !reflect.DeepEqual(p.Imports, wantImports) {
				t.Fatalf("imports = %v, want %v", p.Imports, wantImports)
			}
			// Calls must not mutate the indexed bodies or analyze leaf bodies eagerly.
			if source, _ := p.Source(fn.ID); source != strings.TrimSuffix(leaf, "\n") {
				t.Fatalf("leaf body changed: %q", source)
			}
		})
	}
}

func TestLoadModuleErrors(t *testing.T) {
	for _, tt := range []struct {
		name, rootMod, nestedMod, want string
	}{
		{"root", "module\n", "", "go.mod"},
		{"nested", "module example.com/root\n", "go 1.22\n", "nested/go.mod"},
		{"nested-without-root", "", "module a b\n", "nested/go.mod"},
		{"duplicate", "module example.com/root\n", "module example.com/root\n", "duplicate module path"},
		{"duplicate-siblings", "", "module example.com/other\n", "duplicate module path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.rootMod != "" {
				write(t, root, "go.mod", tt.rootMod)
			}
			if tt.nestedMod != "" {
				write(t, root, "nested/go.mod", tt.nestedMod)
			}
			if tt.name == "duplicate-siblings" {
				write(t, root, "other/go.mod", tt.nestedMod)
			}
			if _, err := Load(root); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want %q", err, tt.want)
			}
		})
	}
	// Different module names can still produce the same package import path.
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	write(t, root, "sub/sub.go", "package sub\nfunc One() {}\n")
	write(t, root, "nested/go.mod", "module example.com/root/sub\n")
	write(t, root, "nested/nested.go", "package sub\nfunc Two() {}\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "duplicate package path") {
		t.Fatalf("package collision error = %v", err)
	}
}

func TestLoadModuleSymlinksAndExclusions(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, outside, "go.mod", "module example.com/outside\n")
	write(t, outside, "outside.go", "not Go")
	for _, dir := range []string{"", "nested"} {
		t.Run("symlink-go.mod-"+dir, func(t *testing.T) {
			r := t.TempDir()
			if dir != "" {
				write(t, r, "go.mod", "module example.com/root\n")
				if err := os.Mkdir(filepath.Join(r, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			filename := filepath.Join(r, dir, "go.mod")
			if err := os.Symlink(filepath.Join(outside, "go.mod"), filename); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(r); err == nil || !strings.Contains(err.Error(), filename) {
				t.Fatalf("symlink go.mod error = %v", err)
			}
		})
	}
	write(t, root, "go.mod", "module example.com/root\n")
	for _, dir := range []string{".hidden", "vendor", "nested/.hidden", "nested/vendor"} {
		write(t, root, dir+"/go.mod", "invalid module")
		write(t, root, dir+"/bad.go", "not Go")
	}
	write(t, root, "nested/go.mod", "module example.com/nested\n")
	for _, file := range []string{".hidden.go", "bad_test.go", "nested/.hidden.go", "nested/bad_test.go"} {
		write(t, root, file, "not Go")
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil || len(p.Functions) != 0 || len(p.packages) != 0 {
		t.Fatalf("exclusions = %v, %v", p, err)
	}
}

func TestLoadDoesNotFollowReplace(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, outside, "go.mod", "module invalid.example/outside\n")
	write(t, outside, "bad.go", "not Go")
	write(t, root, "go.mod", "module example.com/root\nrequire invalid.example/outside v0.0.0\nreplace invalid.example/outside => "+filepath.ToSlash(outside)+"\n")
	write(t, root, "main.go", "package root\nimport external \"invalid.example/outside\"\nfunc Entry() { external.Same() }\nfunc Same() {}\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "example.com/root.Entry"
	got, err := p.Calls(id)
	want := []Edge{{From: id, To: "unknown: external.Same", Unknown: true}}
	if err != nil || !reflect.DeepEqual(got, want) || len(p.packages) != 1 {
		t.Fatalf("external calls = %v, %v; packages = %v", got, err, p.packages)
	}
}
