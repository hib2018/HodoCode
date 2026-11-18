package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantic"
)

func TestSemanticCallableVariables(t *testing.T) {
	for _, tc := range []struct {
		name, source, selected string
		kind, want             int
	}{
		{"arrow", "const f = () => 1;\n", "f =", 13, 1},
		{"getter", "class Thing { get f() { return 1; } }\n", "f()", 7, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "a.js")
			if err := os.WriteFile(file, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := loadPolyglot(root, "", "js")
			if err != nil {
				t.Fatal(err)
			}
			file = filepath.Join(p.Root, "a.js")
			selection := strings.Index(tc.source, tc.selected)
			start, end := 0, len(tc.source)-1
			if tc.name == "getter" {
				start = strings.Index(tc.source, "get ")
				end = strings.Index(tc.source, "}") + 1
			}
			position := func(offset int) semantic.Position {
				pos, err := semanticPosition([]byte(tc.source), offset)
				if err != nil {
					t.Fatal(err)
				}
				return pos
			}
			declarations, err := p.indexSemanticSymbols(file, "javascript", []semantic.Symbol{{Name: "f", Kind: tc.kind, Range: semantic.Range{Start: position(start), End: position(end)}, SelectionRange: semantic.Range{Start: position(selection), End: position(selection + 1)}}})
			if err != nil || len(declarations) != tc.want {
				t.Fatalf("declarations: %+v, %v", declarations, err)
			}
		})
	}
}
