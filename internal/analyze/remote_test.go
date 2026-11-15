package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSubdir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	write(t, root, "bad.go", "invalid Go outside the selection")
	write(t, root, "unselected/go.mod", "invalid module outside the selection")
	write(t, root, "selected/pkg/f.go", "package pkg\nfunc Entry() {}\n")
	write(t, root, "selected/nested/go.mod", "module example.com/nested\n")
	write(t, root, "selected/nested/f.go", "package nested\nfunc Other() {}\n")
	p, err := LoadSubdir(root, "selected")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 2 || p.Functions["example.com/root/selected/pkg.Entry"].File != "pkg/f.go" || p.Functions["example.com/nested.Other"].File != "nested/f.go" {
		t.Fatalf("functions = %v", p.Functions)
	}
	selected, err := filepath.EvalSymlinks(filepath.Join(root, "selected"))
	if err != nil {
		t.Fatal(err)
	}
	if p.RepositorySubdir != "selected" || p.Root != selected {
		t.Fatalf("metadata = %s, %s", p.Root, p.RepositorySubdir)
	}
	// An inherited module alone counts even if no go.mod is in the selection.
	p, err = LoadSubdir(root, "selected/pkg")
	if err != nil || len(p.Functions) != 1 {
		t.Fatalf("inherited module: %v, %v", p, err)
	}
	// The nearest ancestor module, not the repository module, owns this package.
	write(t, root, "selected/nested/deep/f.go", "package deep\nfunc Deep() {}\n")
	p, err = LoadSubdir(root, "selected/nested/deep")
	if err != nil || p.Functions["example.com/nested/deep.Deep"].File != "f.go" {
		t.Fatalf("nearest ancestor: %v, %v", p, err)
	}
}

func TestLoadSubdirMultiModuleWithoutRootModule(t *testing.T) {
	root := t.TempDir()
	write(t, root, "selected/a/go.mod", "module example.com/a\n")
	write(t, root, "selected/a/f.go", "package a\nfunc A() {}\n")
	write(t, root, "selected/b/go.mod", "module example.com/b\n")
	write(t, root, "selected/b/f.go", "package b\nfunc B() {}\n")
	p, err := LoadSubdir(root, "selected")
	if err != nil || len(p.Functions) != 2 {
		t.Fatalf("nested modules: %v, %v", p, err)
	}
}

func TestLoadSubdirRootAndInheritedModuleCount(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	write(t, root, "selected/f.go", "package selected\nfunc Entry() {}\n")
	for _, subdir := range []string{"", ".", "./"} {
		p, err := LoadSubdir(root, subdir)
		if err != nil || p.RepositorySubdir != "" || len(p.Functions) != 1 {
			t.Fatalf("LoadSubdir(%q) = %v, %v", subdir, p, err)
		}
	}
	write(t, root, "selected/nested/go.mod", "module example.com/root\n")
	if _, err := LoadSubdir(root, "selected"); err == nil || !strings.Contains(err.Error(), "duplicate module path") {
		t.Fatalf("inherited module not counted: %v", err)
	}
	// An explicit module at the selection root shadows ancestors entirely.
	write(t, root, "selected/go.mod", "module example.com/selected\n")
	if _, err := LoadSubdir(root, "selected/nested"); err != nil {
		t.Fatalf("selected module must not collide with unindexed ancestors: %v", err)
	}
}

func TestLoadSubdirContainment(t *testing.T) {
	parent := t.TempDir()
	write(t, parent, "go.mod", "module example.com/outside\n")
	root := filepath.Join(parent, "repo")
	write(t, root, "selected/f.go", "package selected\nfunc Entry() {}\n")
	if _, err := LoadSubdir(root, "selected"); err == nil || !strings.Contains(err.Error(), "no go.mod") {
		t.Fatalf("inherited module outside repository: %v", err)
	}
	outside := t.TempDir()
	write(t, outside, "child/go.mod", "module example.com/escape\n")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "selected"), filepath.Join(root, "inside-link")); err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{"..", "../repo/selected", "selected/../selected", outside, "link", "link/child", "inside-link", "missing", "selected/f.go"} {
		t.Run(subdir, func(t *testing.T) {
			if _, err := LoadSubdir(root, subdir); err == nil {
				t.Fatal("unsafe or nonexistent directory accepted")
			}
		})
	}
}

func TestSourceURL(t *testing.T) {
	p := &Project{
		RepositoryURL: "https://github.com/owner/repo/", CommitSHA: "abcdef123", RepositorySubdir: "some dir/日本語",
		Functions: map[string]Function{
			"local":      {File: "a # %?.go", Line: 3, End: 9},
			"sdk":        {File: "GOROOT/src/fmt/print.go", Line: 1, End: 2},
			"dependency": {File: "dependency.go", Line: 1, End: 2},
		},
		local: map[string]bool{"local": true},
	}
	want := "https://github.com/owner/repo/blob/abcdef123/some%20dir/%E6%97%A5%E6%9C%AC%E8%AA%9E/a%20%23%20%25%3F.go#L3-L9"
	if got := p.SourceURL("local"); got != want {
		t.Fatalf("SourceURL = %s; want %s", got, want)
	}
	for _, id := range []string{"sdk", "dependency", "missing"} {
		if got := p.SourceURL(id); got != "" {
			t.Errorf("SourceURL(%s) = %s", id, got)
		}
	}
	p.RepositorySubdir = ""
	if got := p.SourceURL("local"); !strings.HasSuffix(got, "/a%20%23%20%25%3F.go#L3-L9") {
		t.Fatal(got)
	}
	p.CommitSHA = ""
	if p.SourceURL("local") != "" {
		t.Fatal("missing commit accepted")
	}
	p.CommitSHA, p.RepositoryURL = "abcdef123", ""
	if p.SourceURL("local") != "" {
		t.Fatal("missing repository accepted")
	}
}
