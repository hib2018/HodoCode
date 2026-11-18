package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestSemanticZigNamespaceCall(t *testing.T) {
	semantictest.Use(t)
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
		t.Fatalf("namespace call: %+v, %v", sites, err)
	}
	if err := p.EnsureFunction(sites[0].To); err != nil {
		t.Fatalf("target unavailable for navigation: %v", err)
	}
	source, err := p.Source(sites[0].To)
	if err != nil || source != "fn run() void {}" {
		t.Fatalf("target source: %q, %v", source, err)
	}
}

func TestSemanticZigMutableCallsRemainUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		candidates   int
	}{
		{"reassigned", "fn run() void {}\nfn other() void {}\nfn main() void { ns.run(); }\nvar callback = run;\nfn change() void { ns.run = other; }\n", 1},
		{"function pointer", "fn main(handler: anytype) void { handler.callback(); }\n", 0},
		{"anytype receiver", "fn run() void {}\nfn main(handler: anytype) void { handler.run(); }\n", 1},
		{"parameter shadows import", "fn run() void {}\nfn main(ns: anytype) void { ns.run(); }\n", 1},
		{"local shadows import", "fn run() void {}\nfn main() void { const ns = handler; ns.run(); }\n", 1},
		{"capture shadows import", "fn run() void {}\nfn main() void { for (values) |ns| { ns.run(); } }\n", 1},
		{"middle capture shadows import", "fn run() void {}\nfn main() void { for (a, b, c) |first, ns, third| { ns.run(); } }\n", 1},
		{"ambiguous", "const first = struct { fn run() void {} };\nconst second = struct { fn run() void {} };\nfn main() void { ns.run(); }\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			semantictest.Use(t)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "module.zig"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "main.zig"), []byte("const ns = @import(\"module.zig\");\n"+tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := LoadLanguage(root, "zig")
			if err != nil {
				t.Fatal(err)
			}
			sites, err := p.CallSites("main.zig.main")
			if err != nil || len(sites) != 1 || !sites[0].Unknown || len(sites[0].Candidates) != tc.candidates {
				t.Fatalf("unsafe call: %+v, %v", sites, err)
			}
		})
	}
}
