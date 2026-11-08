package analyze

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSelectsBuildFiles(t *testing.T) {
	root := t.TempDir()
	otherOS := "windows"
	if build.Default.GOOS == otherOS {
		otherOS = "linux"
	}
	otherArch := "arm64"
	if build.Default.GOARCH == otherArch {
		otherArch = "amd64"
	}
	for name, text := range map[string]string{
		"go.mod":                                 "module example.com/buildfiles\n\ngo 1.23\n",
		"entry.go":                               "package sample\nfunc Entry() { Platform() }\n",
		"platform_" + build.Default.GOOS + ".go": "package sample\nfunc Platform() {}\n",
		"platform_" + otherOS + ".go":            "package sample\nimport \"excluded.example/os\"\nfunc Platform() {}\n",
		"platform_" + otherArch + ".go":          "package sample\nimport \"excluded.example/arch\"\nfunc Platform() {}\n",
		"excluded.go":                            fmt.Sprintf("//go:build !%s\n\npackage sample\nimport \"excluded.example/tag\"\nfunc Platform() {}\n", build.Default.GOOS),
		"selected.go":                            fmt.Sprintf("//go:build %s && %s\n\npackage sample\nfunc Selected() {}\n", build.Default.GOOS, build.Default.GOARCH),
		"ignored.go":                             "//go:build ignore\n\nthis is deliberately invalid Go\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Functions) != 3 || len(p.Imports) != 0 {
		t.Fatalf("excluded build files indexed: functions=%v imports=%v", p.Functions, p.Imports)
	}
	id := "example.com/buildfiles.Platform"
	if got := p.Functions[id].File; got != "platform_"+build.Default.GOOS+".go" {
		t.Fatalf("platform file = %q", got)
	}
	calls, err := p.Calls("example.com/buildfiles.Entry")
	if err != nil || len(calls) != 1 || calls[0].To != id || calls[0].Unknown {
		t.Fatalf("selected platform call = %v, %v", calls, err)
	}
}
