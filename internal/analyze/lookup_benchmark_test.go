package analyze

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

// Compare warm navigation with the former repeated signature checking, using
// identical cached ASTs and a deterministic eight-package SDK import graph.
func BenchmarkCallSitesSDK(b *testing.B) {
	previous := build.Default
	b.Cleanup(func() { build.Default = previous })
	sdk, root := b.TempDir(), b.TempDir()
	build.Default.GOROOT = sdk
	b.Setenv("PATH", b.TempDir())
	put := func(root, name, data string) {
		b.Helper()
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
			b.Fatal(err)
		}
	}
	put(sdk, "src/sdk0/sdk.go", "package sdk0\ntype Item struct{}\nfunc (*Item) Run() {}\n")
	for n := 1; n < 8; n++ {
		put(sdk, fmt.Sprintf("src/sdk%d/sdk.go", n), fmt.Sprintf("package sdk%d\nimport \"sdk%d\"\ntype Item = sdk%d.Item\nfunc New() *Item { return nil }\n", n, n-1, n-1))
	}
	put(root, "go.mod", "module example.com/bench\n")
	put(root, "app.go", "package bench\nimport \"sdk7\"\nfunc Entry() { sdk7.New().Run() }\n")
	for _, cached := range []bool{false, true} {
		name := "recheck-imports"
		if cached {
			name = "cached-imports"
		}
		b.Run(name, func(b *testing.B) {
			p, err := Load(root)
			if err != nil {
				b.Fatal(err)
			}
			const id = "example.com/bench.Entry"
			sites, err := p.CallSites(id)
			if err != nil || len(sites) != 2 || sites[1].To != "sdk0.Item.Run" || sites[1].Unknown {
				b.Fatalf("calls %+v %v", sites, err)
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if !cached {
					p.importers = nil
				}
				if _, err := p.CallSites(id); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
