package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestSourceHighlights(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("newline=%q", newline), func(t *testing.T) {
			source := strings.ReplaceAll("func 日本語() {\n\tvalue := `first\n日本語 last`; /* first\nlast */ other := \"hello\"; r := '界'; n := 42 + 1.5 + 2i // tail\n}\n", "\n", newline)
			got := sourceHighlights(source, 99)
			want := [][]string{{"func"}, {"`first"}, {"日本語 last`", "/* first"}, {"last */", `"hello"`, "'界'", "42", "1.5", "2i", "// tail"}, nil, nil}
			styles := [][]string{{"35"}, {"32"}, {"32", "90"}, {"90", "32", "32", "36", "36", "36", "90"}, nil, nil}
			lines := strings.Split(source, "\n")
			if len(got) != len(want) {
				t.Fatalf("rows: %d", len(got))
			}
			for row, spans := range got {
				if len(spans) != len(want[row]) {
					t.Fatalf("row %d: %+v", row, spans)
				}
				line := fmt.Sprintf("%d │ %s", 99+row, strings.TrimSuffix(lines[row], "\r"))
				for i, span := range spans {
					if text := displaySlice(line, span.start, span.end-span.start); text != want[row][i] || span.style != "\x1b["+styles[row][i]+"m" {
						t.Errorf("row %d span %d: %q, style %q", row, i, text, span.style)
					}
				}
			}
		})
	}
}

func TestFullScreenSyntaxAndMarkers(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n\tLeaf(); missing(); Leaf() // Leaf()\n\ttext := `Leaf()\n日本語`; _ = text\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.sites) != 3 {
		t.Fatalf("calls in literals/comments: %+v", s.sites)
	}
	line := s.codeLine(1, 120, true)
	for _, want := range []string{
		"\x1b[97;44m\x1b[1mLeaf\x1b[0m()",
		"\x1b[30;43m\x1b[4mmissing\x1b[0m()",
		"\x1b[30;46mLeaf\x1b[0m()",
		"\x1b[90m// Leaf()\x1b[0m",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %q", want, line)
		}
	}
	for selected, site := range s.sites {
		s.selected = selected
		line := s.codeLine(1, 120, true)
		style := "\x1b[97;44m\x1b[1m"
		if site.Unknown {
			style += "\x1b[4m"
		}
		if strings.Count(line, "\x1b[97;44m") != 1 || !strings.Contains(line, style+site.Name+"\x1b[0m") {
			t.Fatalf("selection %d must have exactly one distinct marker: %q", selected, line)
		}
		if strings.Contains(s.codeLine(1, 120, false), "\x1b") {
			t.Fatal("NO_COLOR emitted selection styling")
		}
	}
	if got := s.codeLine(0, 120, true); !strings.Contains(got, "\x1b[35mfunc\x1b[0m main()") {
		t.Fatalf("declaration name should remain plain: %q", got)
	}
	for row, text := range s.code {
		for start := 0; start < displayWidth(text)+2; start++ {
			s.horizontal = start
			for _, width := range []int{1, 4, 17, 120} {
				styled := s.codeLine(row, width, true)
				plain := displaySlice(text, start, width)
				if got := sgrPattern.ReplaceAllString(styled, ""); got != plain {
					t.Fatalf("row %d start %d width %d: %q != %q", row, start, width, got, plain)
				}
				if got := s.codeLine(row, width, false); got != plain {
					t.Fatalf("NO_COLOR row %d: %q", row, got)
				}
			}
		}
	}
}

func TestStyledSlicePriorityAndControlSafety(t *testing.T) {
	line := "\t日本語 e\u0301 \x1b[31m\u202e"
	spans := []syntaxHighlight{
		{0, displayWidth(line), "\x1b[32m"},
		{8, 14, "\x1b[97;44m\x1b[1m"},
	}
	if got := styledSlice(line, 0, 30, spans); !strings.Contains(got, "\x1b[97;44m\x1b[1m日本語\x1b[0m") {
		t.Fatalf("marker must override syntax: %q", got)
	}
	for start := 0; start < 30; start++ {
		for width := 1; width < 31; width++ {
			got := styledSlice(line, start, width, spans)
			plain := sgrPattern.ReplaceAllString(got, "")
			if plain != displaySlice(line, start, width) || strings.ContainsAny(plain, "\x1b\u202e") {
				t.Fatalf("unsafe or misaligned slice at %d/%d: %q", start, width, got)
			}
		}
	}
}
