package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVendorDirectoryBoundary(t *testing.T) {
	offlineTestEnv(t)
	t.Setenv("GOFLAGS", "-overlay=/must-not-read.json")
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.23\nrequire example.com/dep v1.0.0\n")
	write(t, root, "main.go", "package app\nimport \"example.com/dep\"\nfunc Entry() { _ = dep.Item{} }\n")
	write(t, root, "vendor/modules.txt", "# example.com/dep v1.0.0\n## explicit; go 1.23\nexample.com/dep\n")
	outside := t.TempDir()
	write(t, outside, "dep.go", "package dep\ntype Item struct { Secret int }\n")
	if err := os.MkdirAll(filepath.Join(root, "vendor/example.com"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "vendor/example.com/dep")); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	def := externalDefinition(t, p, "example.com/app.Entry", "Item{}")
	if def.Reason == "" || len(def.Snippets) != 0 {
		t.Fatalf("escaping directory exposed source: %+v", def)
	}
}
