package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func setDisplayComments(s *screen, comments map[int][]string) {
	s.agent = newAgentPane(agentOptions{})
	s.agent.comments[s.current] = functionComments{lines: comments}
}

func TestInlineDisplayWrapAndMapping(t *testing.T) {
	code := []string{"1 │ first", "2 │ second", "3 │ third"}
	rows := makeInlineDisplay(code, map[int][]string{0: {"日本語の説明\tvalue\x1b[31m"}, 1: {"one", "two"}, -1: {"invalid"}, 8: {"invalid"}}, 12)
	if rows[0].comment || rows[0].codeRow != 0 || !rows[1].comment || !strings.HasPrefix(strings.TrimSpace(rows[1].text), "╭─ AI解説 ") {
		t.Fatalf("code must precede its marked comments: %+v", rows)
	}
	comments := 0
	for _, row := range rows {
		if row.comment {
			comments++
			if displayWidth(row.text) > 12 || strings.ContainsAny(row.text, "\x1b\t") || strings.HasPrefix(strings.TrimSpace(row.text), "1 │ ") {
				t.Fatalf("unsafe, numbered or over-wide comment: %+v", row)
			}
		}
	}
	if comments < 5 || codeVisualRow(rows, 1) <= 2 || codeVisualRow(rows, 2) != len(rows)-1 {
		t.Fatalf("wrong visual/source mapping: %+v", rows)
	}
	plain := makeInlineDisplay(code, nil, 12)
	for i, row := range plain {
		if row.comment || row.codeRow != i {
			t.Fatalf("no-comment mapping changed: %+v", plain)
		}
	}
}

func TestInlineDisplayCursorSearchAndCalls(t *testing.T) {
	s := testScreen(t, "Recur")
	original := append([]string(nil), s.code...)
	setDisplayComments(s, map[int][]string{0: {strings.Repeat("comment-only ", 20)}})
	s.cursorRow, s.cursorColumn = 1, 0
	s.followCursor(3, 24)
	visual := s.cursorVisualRow(24)
	if visual < 4 || visual < s.top || visual >= s.top+3 || s.cursorRow != 1 {
		t.Fatalf("cursor not followed in visual rows: visual=%d position=%+v", visual, s.screenPosition)
	}
	s.findCode("comment-only", 3, 24)
	if len(s.search.matches) != 0 {
		t.Fatal("virtual comments entered source search")
	}
	s.findCode("Recur", 3, 24)
	if len(s.search.matches) == 0 || s.search.matches[s.search.selected].row != s.cursorRow {
		t.Fatalf("source search moved to wrong row: %+v", s.search)
	}
	s.search = codeSearch{}
	s.selected = len(s.sites) - 1
	s.jumpCall()
	s.follow(3, 24)
	visual = s.cursorVisualRow(24)
	if s.callAtCursor() != s.selected || visual < s.top || visual >= s.top+3 {
		t.Fatalf("call navigation lost source coordinates: %+v", s.screenPosition)
	}
	if !reflect.DeepEqual(s.code, original) {
		t.Fatal("display changed source")
	}
}

func TestInlineDisplayMouseAndPaging(t *testing.T) {
	s := testScreen(t, "Recur")
	setDisplayComments(s, map[int][]string{0: {strings.Repeat("long explanation ", 30)}})
	const width, height = 90, 8
	codeWidth := screenCodeWidth(width)
	rows := s.displayRows(codeWidth)
	s.cursorRow, s.cursorColumn, s.horizontal = 1, 2, 12
	if !s.handleMouse(mouseEvent{x: 20, y: 3}, width, height) || s.cursorRow != 0 || s.cursorColumn != 0 {
		t.Fatalf("comment click must select anchor at column zero: %+v", s.screenPosition)
	}
	for range len(rows) {
		s.handleMouse(mouseEvent{x: 1, y: 2, delta: 1}, width, height)
	}
	if s.top != len(rows)-(height-3) {
		t.Fatalf("wheel used source row limit: top=%d visual=%d", s.top, len(rows))
	}
	s.top, s.cursorRow = 0, 0
	if _, err := s.key("pagedown", height-3, codeWidth); err != nil {
		t.Fatal(err)
	}
	if s.top != height-3 {
		t.Fatalf("page down did not scroll across long comment: top=%d", s.top)
	}
	if _, err := s.key("pageup", height-3, codeWidth); err != nil || s.top != 0 {
		t.Fatalf("page up: top=%d err=%v", s.top, err)
	}
}

func TestInlineDisplayHideAndResize(t *testing.T) {
	s := testScreen(t, "Recur")
	setDisplayComments(s, map[int][]string{0: {strings.Repeat("explanation ", 20)}})
	s.displayRows(50)
	s.top = codeVisualRow(s.display, 1)
	s.displayRows(24)
	if s.top != codeVisualRow(s.display, 1) {
		t.Fatal("resize lost source anchor")
	}
	s.agent.commentsHidden = true
	s.displayRows(24)
	if s.top != 1 || len(s.display) != len(s.code) {
		t.Fatal("hidden comments left visual offsets")
	}
	s.agent.commentsHidden = false
	s.displayRows(24)
	if s.top != codeVisualRow(s.display, 1) {
		t.Fatal("showing comments lost source anchor")
	}
}

func TestInlineDisplayRenderAndTerminalCursor(t *testing.T) {
	s := testScreen(t, "Recur")
	setDisplayComments(s, map[int][]string{0: {"short explanation"}})
	s.cursorRow, s.cursorColumn = 1, 0
	const width, height = 120, 20
	frame := s.render(width, height, true)
	if !strings.Contains(frame, "\x1b[38;5;153;48;5;235m") || !strings.Contains(frame, "╭─ AI解説 ") {
		t.Fatal("inline comment block not rendered with its muted background")
	}
	plain := s.render(width, height, false)
	if sgrPattern.MatchString(plain) || sgrPattern.ReplaceAllString(frame, "") != plain {
		t.Fatal("colorless rendering changed the comment layout or emitted styles")
	}
	var out bytes.Buffer
	err := screenLoop(context.Background(), s, &out, func() (byte, bool, error) { return 0, false, io.EOF }, func() (int, int, error) { return width, height, nil }, false)
	if err != io.EOF {
		t.Fatal(err)
	}
	want := fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", s.cursorVisualRow(screenCodeWidth(width))-s.top+2, s.cursorCell()-s.horizontal+1)
	if !strings.Contains(out.String(), want) || s.cursorVisualRow(screenCodeWidth(width)) <= s.cursorRow {
		t.Fatalf("terminal cursor did not use visual row: want %q", want)
	}
}

func TestInlineDisplayVariablePopupAnchor(t *testing.T) {
	s := variableScreen(t, `package main
func main() {
	value := 1
	_ = value
}
`)
	pointDefinition(t, s, "_ = value", "value")
	setDisplayComments(s, map[int][]string{0: {"explanation"}})
	const width, height = 120, 30
	popup := s.renderVariable(width, height, false)
	visual := s.cursorVisualRow(screenCodeWidth(width))
	if visual <= s.cursorRow || !strings.HasPrefix(popup, fmt.Sprintf("\x1b[%d;", visual+3)) {
		t.Fatalf("popup is not below the visual cursor: visual=%d popup=%q", visual, popup)
	}
	s.top = visual + 1
	if popup := s.renderVariable(width, height, false); popup != "" {
		t.Fatal("popup visible for offscreen source anchor")
	}
}

func TestInlineDisplayHistorySourceAnchor(t *testing.T) {
	for _, navigation := range []string{"call", "tree", "recursive"} {
		for _, change := range []string{"toggle", "comments and width"} {
			t.Run(navigation+"/"+change, func(t *testing.T) {
				s := testScreen(t, "Recur")
				root := s.current
				setDisplayComments(s, map[int][]string{0: {strings.Repeat("old comment ", 8)}})
				s.selected = 1
				if navigation == "recursive" {
					s.selected = 0
				}
				s.jumpCall()
				s.top = codeVisualRow(s.displayRows(30), 1)
				cursorRow, cursorColumn := s.cursorRow, s.cursorColumn
				if navigation == "tree" {
					if err := s.openTree(); err != nil {
						t.Fatal(err)
					}
					for _, row := range s.tree.rows() {
						if row.node.site.To == s.sites[s.selected].To {
							s.tree.selected = row.node
							break
						}
					}
				}
				if _, err := s.key("\n", 6, 30); err != nil {
					t.Fatal(err)
				}
				if len(s.history) != 1 || s.history[0].top != 1 {
					t.Fatalf("history did not save source anchor: %+v", s.history)
				}
				width := 30
				if change == "toggle" {
					s.agentKey(context.Background(), "c")
				} else {
					s.agent.comments[root] = functionComments{lines: map[int][]string{0: {strings.Repeat("new background explanation ", 20)}}}
					width = 24
				}
				if _, err := s.key("b", 6, width); err != nil {
					t.Fatal(err)
				}
				wantTop := codeVisualRow(s.displayRows(width), 1)
				if s.current != root || s.top != wantTop || s.cursorRow != cursorRow || s.cursorColumn != cursorColumn {
					t.Fatalf("history lost source anchor: want top=%d cursor=%d:%d, got %+v", wantTop, cursorRow, cursorColumn, s.screenPosition)
				}
			})
		}
	}
}

func TestInlineDisplayBorderedParagraphs(t *testing.T) {
	const width = 24
	code := []string{"123 │ value()", "124 │ next()"}
	rows := makeInlineDisplay(code, map[int][]string{0: {"abcdefghijklmno", "second"}}, width)
	indent := strings.Repeat(" ", displayWidth("123 │ "))
	wantBody := []string{"abcdefghijklm", "no", "", "second"}
	if len(rows) != len(wantBody)+4 || rows[0].comment || rows[len(rows)-1].comment {
		t.Fatalf("expected one block after code: %+v", rows)
	}
	labelCount := 0
	for i, row := range rows[1 : len(rows)-1] {
		if !row.comment || row.codeRow != 0 || !strings.HasPrefix(row.text, indent) || displayWidth(row.text) >= width {
			t.Fatalf("decoration lost mapping, gutter or right margin: %+v", row)
		}
		labelCount += strings.Count(row.text, "AI解説")
		if i > 0 && i <= len(wantBody) {
			want := indent + "│ " + displaySlice(wantBody[i-1], 0, 13) + " │"
			if row.text != want {
				t.Fatalf("body row %d: got %q want %q", i, row.text, want)
			}
		}
	}
	if labelCount != 1 || !strings.HasPrefix(rows[len(rows)-2].text, indent+"╰") {
		t.Fatal("comments did not share one labeled, closed frame")
	}
	short := makeInlineDisplay(code, map[int][]string{0: {"short"}}, 120)
	if displayWidth(short[1].text) >= 30 {
		t.Fatal("short comment expanded into a full-width frame")
	}
	spaced := makeInlineDisplay(code, map[int][]string{0: {" a  b "}}, 120)
	if !strings.HasPrefix(spaced[2].text, indent+"│  a  b ") {
		t.Fatal("comment whitespace was rewritten")
	}
}

func TestInlineDisplayBorderNarrowAndSanitized(t *testing.T) {
	for width := 0; width <= 40; width++ {
		rows := makeInlineDisplay([]string{"123456 │ code"}, map[int][]string{0: {"日本語\ttext\x1b[31m\nnext", "second"}}, width)
		for _, row := range rows[1:] {
			if !row.comment || row.codeRow != 0 || displayWidth(row.text) > width || strings.ContainsAny(row.text, "\x1b\t\n") {
				t.Fatalf("width=%d: unsafe, over-wide or unmapped decoration: %+v", width, row)
			}
		}
	}
}
