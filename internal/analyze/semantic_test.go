package analyze

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantic"
	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestSemanticLanguages(t *testing.T) {
	for _, tc := range []struct{ language, file, source string }{
		{"zig", "main.zig", "fn helper() void {}\nfn main() void { helper(); missing(); }\n"},
		{"ts", "main.ts", "function helper(): void {}\nfunction main(): void { helper(); missing(); }\n"},
		{"rust", "main.rs", "fn helper() {}\nfn main() { helper(); missing(); }\n"},
		{"c", "main.c", "int helper(void) { return 1; }\nint main(void) { helper(); missing(); }\n"},
		{"js", "main.js", "function helper() {}\nfunction main() { helper(); missing(); }\n"},
		{"python", "main.py", "def helper():\n    pass\ndef main():\n    helper()\n    missing()\n"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			semantictest.Use(t)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := LoadLanguage(root, tc.language)
			if err != nil {
				t.Fatal(err)
			}
			sites, err := p.CallSites(tc.file + ".main")
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != 2 || sites[0].Unknown || sites[0].To != tc.file+".helper" || !sites[1].Unknown || sites[1].Reason == "" {
				t.Fatalf("sites: %+v", sites)
			}
			// Public results must not mutate the snapshot cache.
			sites[0].To = "changed"
			again, err := p.CallSites(tc.file + ".main")
			if err != nil || again[0].To != tc.file+".helper" {
				t.Fatalf("cache mutated: %+v, %v", again, err)
			}
		})
	}
}

func TestSemanticCrossFileAndCandidates(t *testing.T) {
	semantictest.Use(t)
	root := t.TempDir()
	for name, source := range map[string]string{
		"main.ts":   "import {helper} from './helper';\nfunction main() { helper(); }\n",
		"helper.js": "function helper() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadLanguage(root, "auto")
	if err != nil {
		t.Fatal(err)
	}
	sites, err := p.CallSites("main.ts.main")
	if err != nil || len(sites) != 1 || sites[0].Unknown || sites[0].To != "helper.js.helper" {
		t.Fatalf("cross-file: %+v, %v", sites, err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.ts"), []byte("function helper() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = LoadLanguage(root, "auto")
	if err != nil {
		t.Fatal(err)
	}
	sites, err = p.CallSites("main.ts.main")
	if err != nil || len(sites) != 1 || !sites[0].Unknown || len(sites[0].Candidates) != 2 {
		t.Fatalf("ambiguous: %+v, %v", sites, err)
	}
	sites[0].Candidates[0] = "changed"
	again, _ := p.CallSites("main.ts.main")
	if again[0].Candidates[0] == "changed" {
		t.Fatal("candidate cache mutated")
	}
}

func TestSemanticMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte("def main():\n    pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLanguage(root, "python"); err == nil || !strings.Contains(err.Error(), "requires pyright-langserver on PATH") {
		t.Fatalf("missing tool: %v", err)
	}
}

func TestSemanticCanceledLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadLanguageContext(ctx, t.TempDir(), "python"); !errors.Is(err, context.Canceled) {
		t.Fatalf("local cancellation: %v", err)
	}
	if _, err := LoadSubdirLanguageContext(ctx, t.TempDir(), ".", "python"); !errors.Is(err, context.Canceled) {
		t.Fatalf("subdir cancellation: %v", err)
	}
}

func TestSemanticPositions(t *testing.T) {
	source := []byte("a😀é\r\n日本語()\n")
	for _, offset := range []int{0, 1, 5, 7, 8, 9, 12, len(source)} {
		pos, err := semanticPosition(source, offset)
		if err != nil {
			t.Fatal(err)
		}
		got, err := semanticOffset(source, pos)
		if err != nil || got != offset {
			t.Fatalf("offset %d: %+v => %d, %v", offset, pos, got, err)
		}
	}
	for _, pos := range []semantic.Position{{Line: -1}, {Character: -1}, {Line: 10}, {Character: 2}, {Character: 99}} {
		if _, err := semanticOffset(source, pos); err == nil {
			t.Fatalf("accepted %+v", pos)
		}
	}
	if _, err := semanticPosition(source, 2); err == nil {
		t.Fatal("accepted split UTF-8")
	}
}

func TestSemanticSymbolsReplaceLexicalDeclarations(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.js")
	source := []byte("function stale() {}\nfunction actual() {}\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "js")
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(p.Root, "a.js")
	symbols := []semantic.Symbol{{Name: "actual", Kind: 12, Range: semantic.Range{Start: semantic.Position{Line: 1}, End: semantic.Position{Line: 1, Character: 20}}, SelectionRange: semantic.Range{Start: semantic.Position{Line: 1, Character: 9}, End: semantic.Position{Line: 1, Character: 15}}}}
	decls, err := p.indexSemanticSymbols(file, "javascript", symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 1 || len(p.Functions) != 1 {
		t.Fatalf("declarations: %+v, functions: %+v", decls, p.Functions)
	}
	if _, ok := p.Functions["a.js.stale"]; ok {
		t.Fatal("lexical declaration survived")
	}
}
