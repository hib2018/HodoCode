package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

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
