package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLanguageSubdirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.py"), []byte("def main():\n    pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.js"), []byte("function outside() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadSubdirLanguage(root, "src", "python")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 1 || p.RepositorySubdir != "src" {
		t.Fatalf("project: %+v", p)
	}
	id, err := p.Resolve("main")
	if err != nil {
		t.Fatal(err)
	}
	if p.FunctionLanguage(id) != "python" {
		t.Fatal(p.FunctionLanguage(id))
	}
	p.RepositoryURL = "https://github.com/owner/repo"
	p.CommitSHA = strings.Repeat("a", 40)
	if got := p.SourceURL(id); !strings.Contains(got, "/src/main.py#L1-L2") {
		t.Fatal(got)
	}
	defs, err := p.Definitions(id)
	if err != nil || len(defs) != 0 {
		t.Fatalf("nonGo definitions: %v %v", defs, err)
	}
}
