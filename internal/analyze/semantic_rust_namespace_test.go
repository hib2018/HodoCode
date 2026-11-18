package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantic"
)

func TestSemanticTraitMethodOwnership(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "trait.rs")
	source := "trait Runner { fn run() {} }\n"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := loadPolyglot(root, "", "rust")
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(p.Root, "trait.rs")
	rangeAt := func(start, end int) semantic.Range {
		return semantic.Range{Start: semantic.Position{Character: start}, End: semantic.Position{Character: end}}
	}
	run := strings.Index(source, "run")
	symbols := []semantic.Symbol{{Name: "Runner", Kind: 11, Range: rangeAt(0, len(source)-1), SelectionRange: rangeAt(6, 12), Children: []semantic.Symbol{{Name: "run", Kind: 12, Range: rangeAt(strings.Index(source, "fn run"), strings.Index(source, "}")+1), SelectionRange: rangeAt(run, run+3)}}}}
	_, err = p.indexSemanticSymbols(file, "rust", symbols)
	if err != nil {
		t.Fatal(err)
	}
	if fn, ok := p.polyglot["trait.rs.Runner.run"]; !ok || fn.owner != "Runner" {
		t.Fatalf("trait method mistaken for free function: %+v", p.polyglot)
	}
}
