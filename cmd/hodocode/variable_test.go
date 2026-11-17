package main

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func variableScreen(t *testing.T, source string) *screen {
	t.Helper()
	fakeScreenSDK(t, nil)
	p := screenProject(t, map[string]string{"main.go": source})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVariableHighlightIdentity(t *testing.T) {
	s := variableScreen(t, `package main
func main() {
	名前 := 1 // outer declaration
	other := 2
	_ = 名前 // outer use
	{
		名前 := 3 // shadow declaration
		_ = 名前 // shadow use
	}
	closure := func(param int) {
		_ = 名前 // captured use
		local := other
		_ = local
		_ = param
	}
	_ = closure
	values := []int{1}
	_ = values
	lookup := map[string]int{"a": 1}
	_ = lookup
	events := make(chan int)
	_ = events
	pointer := &other
	_ = pointer
	_ = 名前 // final use
}
`)
	for _, tc := range []struct {
		name    string
		markers []string
	}{
		{"名前", []string{"outer declaration", "outer use", "captured use", "final use"}},
		{"名前", []string{"shadow declaration", "shadow use"}},
		{"other", []string{"other :=", "local := other", "pointer := &other"}},
		{"local", []string{"local :=", "_ = local"}},
		{"closure", []string{"closure :=", "_ = closure"}},
		{"param", []string{"closure :=", "_ = param"}},
		{"values", []string{"values :=", "_ = values"}},
		{"lookup", []string{"lookup :=", "_ = lookup"}},
		{"events", []string{"events :=", "_ = events"}},
		{"pointer", []string{"pointer :=", "_ = pointer"}},
	} {
		t.Run(tc.markers[0], func(t *testing.T) {
			want := map[int][]syntaxHighlight{}
			for _, marker := range tc.markers {
				pointDefinition(t, s, marker, tc.name)
				start := s.cursorCell()
				want[s.cursorRow] = []syntaxHighlight{{start: start, end: start + displayWidth(tc.name), style: "\x1b[1;30;46m"}}
			}
			// Every occurrence, including a captured use, must activate the same identity.
			for _, marker := range tc.markers {
				pointDefinition(t, s, marker, tc.name)
				if s.variableAtCursor() < 0 {
					t.Fatalf("no variable at %s", marker)
				}
				for row := range s.code {
					if got := s.variableSpans(row); !reflect.DeepEqual(got, want[row]) {
						t.Errorf("from %s row %d: got %+v, want %+v", marker, row, got, want[row])
					}
				}
			}
		})
	}
}

func TestVariableJapaneseTabsHorizontal(t *testing.T) {
	s := variableScreen(t, "package main\nfunc main() {\n\t名前 := 1\n\t\t_ = 名前\n}\n")
	pointDefinition(t, s, "_ = 名前", "名前")
	start := s.cursorCell()
	spans := s.variableSpans(s.cursorRow)
	if len(spans) != 1 || spans[0].start != start || spans[0].end != start+4 {
		t.Fatalf("Japanese/tab cell bounds: %+v, cursor %d", spans, start)
	}
	for _, horizontal := range []int{0, start - 2, start + 2} {
		s.horizontal = horizontal
		got := s.codeLine(s.cursorRow, 40, true)
		want := "名前"
		if horizontal == start+2 {
			want = "前"
		}
		if !strings.Contains(got, "\x1b[1;30;46m"+want+"\x1b[0m") {
			t.Errorf("horizontal %d lost variable highlight: %q", horizontal, got)
		}
		if strings.Contains(s.codeLine(s.cursorRow, 40, false), "\x1b[") {
			t.Fatal("plain code emitted ANSI styling")
		}
	}
	// A wide identifier's second rune is still inside the same variable.
	offset := s.variableAtCursor()
	s.cursorColumn++
	if s.variableAtCursor() != offset {
		t.Fatal("second Japanese rune lost variable identity")
	}
}

func TestVariablePopupDeclarationAndFallback(t *testing.T) {
	s := variableScreen(t, `package main
type Item struct { Field int }
func main() {
	items := []Item{{Field: 1}}
	_ = items
	_ = Item{}
	_ = missing
}
`)
	pointDefinition(t, s, "_ = items", "items")
	popup := s.renderVariable(120, 20, false)
	if !strings.Contains(popup, "items := []Item{{Field: 1}}") || strings.Contains(popup, "type Item struct") {
		t.Fatalf("popup must show declaration, not element type: %q", popup)
	}
	pointDefinition(t, s, "_ = Item{}", "Item")
	if got := s.renderVariable(120, 20, false); got != "" {
		t.Fatalf("type identifier opened variable popup: %q", got)
	}
	pointDefinition(t, s, "_ = missing", "missing")
	offset := s.variableAtCursor()
	if offset < 0 || !s.definitions[offset].Unresolved {
		t.Fatal("unresolved identifier was not indexed")
	}
	popup = s.renderVariable(120, 20, false)
	if !strings.Contains(popup, "定義を取得できません") {
		t.Fatalf("missing unresolved fallback: %q", popup)
	}
	for row := range s.code {
		if len(s.variableSpans(row)) != 0 {
			t.Fatalf("unresolved identifier highlighted row %d", row)
		}
	}
	press(t, s, "0")
	if got := s.renderVariable(120, 20, false); got != "" || s.variableOffset != -1 || s.variableTop != 0 {
		t.Fatalf("leaving variable retained popup: %q offset=%d top=%d", got, s.variableOffset, s.variableTop)
	}
}

func TestVariablePopupBounds(t *testing.T) {
	s := variableScreen(t, "package main\nfunc main() {\n\t名前 := 1\n"+strings.Repeat("\t// padding\n", 14)+"\t_ = 名前\n}\n")
	pointDefinition(t, s, "_ = 名前", "名前")
	moves := regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
	styles := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, width := range []int{41, 80, 120, 200} {
		for _, layout := range []struct{ height, cursor int }{{14, 0}, {14, 5}, {14, 10}, {8, 2}} {
			height, cursor := layout.height, layout.cursor
			for _, color := range []bool{false, true} {
				t.Run(fmt.Sprintf("width%d/height%d/row%d/color%v", width, height, cursor, color), func(t *testing.T) {
					s.top = s.cursorRow - cursor
					s.horizontal = s.cursorCell() - 3
					popup := s.renderVariable(width, height, color)
					indices := moves.FindAllStringSubmatchIndex(popup, -1)
					if len(indices) == 0 {
						t.Fatal("missing popup")
					}
					var rows []int
					for i, m := range indices {
						y, _ := strconv.Atoi(popup[m[2]:m[3]])
						x, _ := strconv.Atoi(popup[m[4]:m[5]])
						end := len(popup)
						if i+1 < len(indices) {
							end = indices[i+1][0]
						}
						text := styles.ReplaceAllString(popup[m[1]:end], "")
						if y < 2 || y > height-2 || x < 1 || x+displayWidth(text)-1 > screenCodeWidth(width) {
							t.Fatalf("popup outside code area: (%d,%d) %q", x, y, text)
						}
						if y == cursor+2 {
							t.Fatalf("popup overlaps cursor row %d", y)
						}
						rows = append(rows, y)
					}
					sort.Ints(rows)
					if cursor == 0 && rows[0] <= cursor+2 || cursor == 10 && rows[len(rows)-1] >= cursor+2 {
						t.Fatalf("popup did not switch sides: %v", rows)
					}
					if !color && strings.Contains(popup, "\x1b[0m") {
						t.Fatal("plain popup emitted styling")
					}
				})
			}
		}
	}
}

func TestVariablePopupScrollIsolation(t *testing.T) {
	s := variableScreen(t, "package main\nfunc main() {\n\tvalues := []int{\n"+strings.Repeat("\t\t1,\n", 30)+"\t}\n\t_ = values\n}\n")
	pointDefinition(t, s, "_ = values", "values")
	s.top = s.cursorRow - 2
	for _, conversation := range []bool{false, true} {
		t.Run(fmt.Sprint(conversation), func(t *testing.T) {
			s.agent = newAgentPane(agentOptions{})
			s.agent.visible = conversation
			s.agent.followTail = false
			s.agent.text = "saved conversation"
			s.explanationTop = 2
			s.variableTop = 0
			s.renderVariable(120, 14, false)
			position := s.screenPosition
			pane := append([]string(nil), s.paneLines(100, 20)...)
			for range 100 {
				press(t, s, "}")
				s.renderVariable(120, 14, false)
			}
			last := s.variableTop
			if last <= 0 {
				t.Fatal("popup did not scroll")
			}
			press(t, s, "}")
			s.renderVariable(120, 14, false)
			if s.variableTop != last {
				t.Fatal("popup scroll did not clamp")
			}
			press(t, s, "{")
			if s.variableTop != last-1 {
				t.Fatal("popup did not scroll back")
			}
			if s.screenPosition != position || !reflect.DeepEqual(s.paneLines(100, 20), pane) || s.agent.text != "saved conversation" {
				t.Fatal("popup scrolling altered source, right pane, or conversation")
			}
			for range 100 {
				press(t, s, "{")
			}
			if s.variableTop != 0 {
				t.Fatal("popup scrolled below zero")
			}
		})
	}
}

func TestVariablePopupPreservesScalarIdentifierHelp(t *testing.T) {
	s := variableScreen(t, "package main\nfunc main() {\n\tvalue := 1\n\tother := 2\n\t_ = value\n\t_ = other\n}\n")
	before, err := s.p.Definitions(s.current)
	if err != nil {
		t.Fatal(err)
	}
	pointDefinition(t, s, "value :=", "value")
	first := s.variableAtCursor()
	if first < 0 || !s.definitions[first].PopupOnly || len(s.definitions[first].Snippets) == 0 {
		t.Fatal("scalar declaration must retain popup-only source")
	}
	for _, tc := range []struct{ marker, name, declaration string }{
		{"value :=", "value", "value := 1"},
		{"other :=", "other", "other := 2"},
	} {
		pointDefinition(t, s, tc.marker, tc.name)
		pane := strings.Join(s.paneLines(200, 20), "\n")
		for _, want := range []string{`"` + tc.name + `"`, "\n意味\n", "\n書き方\n", "\n注意点\n", "\n公式リファレンス\n"} {
			if !strings.Contains(pane, want) {
				t.Errorf("scalar right pane missing %q: %s", want, pane)
			}
		}
		if strings.Contains(pane, "main.go") || strings.Contains(pane, " │ "+tc.declaration) {
			t.Fatalf("popup source replaced generic identifier help: %s", pane)
		}
		popup := s.renderVariable(120, 20, false)
		if !strings.Contains(popup, tc.declaration) {
			t.Fatalf("missing scalar declaration popup: %q", popup)
		}
		if tc.name == "other" && (s.variableOffset == first || s.variableTop != 0 || strings.Contains(popup, "value := 1")) {
			t.Fatalf("switching variables retained previous popup/scroll: offset=%d top=%d %q", s.variableOffset, s.variableTop, popup)
		}
		if !reflect.DeepEqual(s.definitions, before) {
			t.Fatal("right pane or popup mutated source definition snippets")
		}
		// Simulate an already scrolled popup before moving to the next identifier.
		s.variableTop = 5
	}
}
