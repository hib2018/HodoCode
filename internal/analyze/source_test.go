package analyze

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSourcePosition(t *testing.T) {
	sdk := fakeSDK(t, "fakepath", "package fakepath\n//line forged.go:900:70\n\t  func Entry() {}\n")
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/source\n")
	write(t, root, "entry.go", "package sample\n//line forged.go:900:70\n\tfunc 日本語() {}; func Entry() {}\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing", "Entry", "fakepath.Entry"} {
		if got, err := p.SourcePosition(id); got != (token.Position{}) || err == nil {
			t.Fatalf("SourcePosition(%q) = %+v, %v; want not found", id, got, err)
		}
	}
	if err := p.EnsureFunction("fakepath.Entry"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, filename string
		column       int
	}{
		{"example.com/source.Entry", filepath.Join(root, "entry.go"), len("\tfunc 日本語() {}; ") + 1},
		{"fakepath.Entry", filepath.Join(sdk, "src/fakepath/sdk.go"), 4},
	} {
		want, err := filepath.EvalSymlinks(tc.filename)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.SourcePosition(tc.id)
		if err != nil || got.Filename != want || !filepath.IsAbs(got.Filename) || got.Line != 3 || got.Column != tc.column {
			t.Errorf("SourcePosition(%q) = %+v, %v; want %q:3:%d", tc.id, got, err, want, tc.column)
		}
		if got.Filename == p.Functions[tc.id].File {
			t.Errorf("SourcePosition(%q) returned display label %q", tc.id, got.Filename)
		}
	}
}

func TestSourcePreservesIndexedDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/source\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code := "func Entry() {\n\t// 日本語のコメント\n\tprintln(`original text`)\n}"
	filename := filepath.Join(root, "entry.go")
	if err := os.WriteFile(filename, []byte("package sample\n//line fake.go:900\n"+code+"; func Other() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// Source must match the AST snapshot even if the file changes afterwards.
	if err = os.WriteFile(filename, []byte("package sample\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := p.Source("example.com/source.Entry")
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Fatalf("source changed: %q", got)
	}
	if _, err = p.Source("missing"); err == nil {
		t.Fatal("unknown function accepted")
	}
	other, err := p.Source("example.com/source.Other")
	if err != nil || other != "func Other() {}" {
		t.Fatalf("other=%q err=%v", other, err)
	}
}
