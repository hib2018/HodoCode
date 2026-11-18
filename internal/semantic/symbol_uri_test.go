package semantic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlatSymbolRejectsOtherFile(t *testing.T) {
	for _, mode := range []string{"flat-wrong-uri", "flat-invalid-uri"} {
		t.Run(mode, func(t *testing.T) {
			c, ctx := startServer(t, mode)
			symbols, err := c.Symbols(ctx, filepath.Join(t.TempDir(), "requested.go"))
			if err == nil || len(symbols) != 0 {
				t.Fatalf("got %+v, %v; want no symbols and an error", symbols, err)
			}
			select {
			case <-c.done:
			default:
				t.Fatal("invalid symbol URI did not terminate client")
			}
		})
	}
}

func TestSymbolFilePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.go")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if symbolFilePath(alias) != symbolFilePath(target) {
		t.Fatal("symlink alias mismatch")
	}
	path := filepath.Join(dir, "nonexistent", "..", "missing.go")
	if symbolFilePath(path) != filepath.Clean(path) {
		t.Fatal("nonexistent path was not cleaned")
	}
}
