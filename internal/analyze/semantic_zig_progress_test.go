package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestSemanticZigNamespaceCallAfterBuildProgress(t *testing.T) {
	semantictest.Use(t)
	t.Setenv("HODOCODE_TEST_LSP_BUILD_PROGRESS", "1")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"src/main.zig": "const ztodo = @import(\"ztodo\");\npub fn main() void { ztodo.cli.run(); }\n",
		"src/root.zig": "pub const cli = @import(\"cli.zig\");\n",
		"src/cli.zig":  "pub fn run() void {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadLanguage(root, "zig")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := p.CallSites("src/main.zig.main")
	if err != nil || len(sites) != 1 || sites[0].Unknown || sites[0].To != "src/cli.zig.run" {
		t.Fatalf("namespace call after build progress: %+v, %v", sites, err)
	}
	if err := p.EnsureFunction(sites[0].To); err != nil {
		t.Fatalf("target unavailable for navigation: %v", err)
	}
	source, err := p.Source(sites[0].To)
	if err != nil || source != "fn run() void {}" {
		t.Fatalf("target source: %q, %v", source, err)
	}
}
