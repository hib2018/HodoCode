package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func searchText(t *testing.T, s *screen, text string) {
	t.Helper()
	press(t, s, "/")
	for _, b := range []byte(text + "\r") {
		if !s.searchInputByte(b, 3, 16) {
			t.Fatalf("input byte %q was not consumed", b)
		}
	}
}

func TestCodeSearchMatchesAndNavigation(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n\tLeaf() // Leaf leaf\n\t_ = \"Leaf\"\n\n\n\n\t// abcdefghijklmnopqrstuv日本語\tLeaf\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	searchText(t, s, "Leaf")
	if len(s.search.matches) != 4 || s.search.selected != 0 || s.cursorRow != 1 {
		t.Fatalf("matches: %+v", s.search)
	}
	for i := 1; i < 4; i++ {
		press(t, s, "\x0e")
		if s.search.selected != i {
			t.Fatal("next match")
		}
	}
	if s.top == 0 || s.horizontal == 0 || s.cursorRow != 6 {
		t.Fatalf("offscreen match not followed: %+v", s.screenPosition)
	}
	m := s.search.matches[3]
	if s.cursorCell() != m.start || m.end-m.start != 4 {
		t.Fatalf("Japanese/tab cells: %+v, cursor %d", m, s.cursorCell())
	}
	press(t, s, "\x0e")
	if s.search.selected != 0 {
		t.Fatal("next did not wrap")
	}
	press(t, s, "\x10")
	if s.search.selected != 3 {
		t.Fatal("previous did not wrap")
	}
	s.cursorRow, s.cursorColumn = 8, 0
	searchText(t, s, "Leaf")
	if s.search.selected != 0 {
		t.Fatal("initial search did not wrap")
	}
	s.cursorRow, s.cursorColumn = 1, 5
	searchText(t, s, "Leaf")
	if s.search.selected != 1 {
		t.Fatal("search did not start at cursor")
	}
	searchText(t, s, "leaf")
	if len(s.search.matches) != 1 {
		t.Fatal("search must be case sensitive")
	}
	searchText(t, s, "日本語")
	if len(s.search.matches) != 1 || s.search.matches[0].end-s.search.matches[0].start != 6 {
		t.Fatal("Unicode match width")
	}
	searchText(t, s, " │ ")
	if len(s.search.matches) != 0 {
		t.Fatal("gutter searched")
	}
	if !strings.Contains(s.searchStatus(), "該当なし") {
		t.Fatal("missing no-match status")
	}
	searchText(t, s, "")
	if s.search.query != "" || len(s.search.matches) != 0 {
		t.Fatal("empty search did not clear")
	}
}

func TestCodeSearchInputAndExistingKeys(t *testing.T) {
	s := testScreen(t, "Recur")
	searchText(t, s, "Recur")
	before := s.screenPosition
	press(t, s, "/")
	for _, b := range []byte("qienst日本語\x7f\x08") {
		s.searchInputByte(b, 6, 30)
	}
	if s.search.input.text != "qienst日" || s.screenPosition != before {
		t.Fatal("input triggered shortcuts or broke UTF-8 backspace")
	}
	s.searchInputByte(0x1b, 6, 30)
	if !s.searchInputIdle() || s.search.input != nil || s.search.query != "" || len(s.search.matches) != 0 || s.screenPosition != before {
		t.Fatal("Escape did not exit search")
	}
	press(t, s, "n")
	if s.selected != 1 {
		t.Fatal("n call navigation changed")
	}
	press(t, s, "N")
	if s.selected != 0 {
		t.Fatal("N call navigation changed")
	}
	press(t, s, "n", "\r")
	if s.search.query != "" || len(s.search.matches) != 0 {
		t.Fatal("function navigation retained search")
	}
	press(t, s, "b")
	if s.search.query != "" {
		t.Fatal("back restored search")
	}
}

func TestCodeSearchHighlightAndStatus(t *testing.T) {
	s := testScreen(t, "Recur")
	searchText(t, s, "Recur")
	s.horizontal = 0
	m := s.search.matches[s.search.selected]
	line := s.codeLine(m.row, 100, true)
	if !strings.Contains(line, "\x1b[1;30;46mRecur") {
		t.Fatalf("active match must override call style: %q", line)
	}
	for i, other := range s.search.matches {
		if i == s.search.selected {
			continue
		}
		if !strings.Contains(s.codeLine(other.row, 100, true), "\x1b[30;43mRecur") {
			t.Fatal("other match not highlighted")
		}
	}
	if strings.Contains(s.codeLine(m.row, 100, false), "\x1b[") {
		t.Fatal("NO_COLOR emitted styling")
	}
	if !strings.Contains(s.render(120, 24, false), s.searchStatus()) {
		t.Fatal("missing search status")
	}
	searchText(t, s, "missing")
	before := s.screenPosition
	press(t, s, "\x0e", "\x10")
	if s.screenPosition != before {
		t.Fatal("no-match navigation moved cursor")
	}
	for _, width := range []int{41, 80, 140, 200} {
		if !strings.Contains(screenHelp(width), "/検索") || !strings.Contains(screenHelp(width), "^N/P") {
			t.Fatal("missing search help")
		}
	}
}

func TestCodeSearchModalKeys(t *testing.T) {
	s := testScreen(t, "Recur")
	searchText(t, s, "Recur")
	before := s.screenPosition
	for _, key := range []string{"q", "\x03", "\x04", "n", "N", "j", "k", "h", "l", "up", "down", "left", "right", "pageup", "pagedown", "w", "B", "0", "$", "g", "G", "b", "t", "i", "e", "s", "[", "]", "\r", "\n"} {
		press(t, s, key)
		if s.screenPosition != before || s.search.query != "Recur" || len(s.history) != 0 || s.tree != nil {
			t.Fatalf("normal key %q changed search mode", key)
		}
	}
	if !strings.Contains(s.render(120, 24, false), " SEARCH ") {
		t.Fatal("confirmed search must remain in SEARCH mode")
	}
	press(t, s, "\x1b")
	if s.search.query != "" || s.search.input != nil || len(s.search.matches) != 0 {
		t.Fatal("Escape did not clear search")
	}
	press(t, s, "n")
	if s.selected != 1 {
		t.Fatal("normal operations not restored after Escape")
	}
}

func TestCodeSearchLoopModalEscape(t *testing.T) {
	for _, prefix := range []string{"/", "/Recur\r", "/not-found\r"} {
		for _, small := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/small=%v", prefix, small), func(t *testing.T) {
				s := testScreen(t, "Recur")
				s.agent = newAgentPane(agentOptions{})
				blocked := "q\x03\x04nNjkhliestsb[]\r\n\x1b[A\x1b[B\x1b[C\x1b[D\x1b[5~\x1b[6~"
				input := []byte(prefix + blocked + "\x1b")
				i, idled := 0, false
				var before screenPosition
				read := func() (byte, bool, error) {
					if i == len(prefix) {
						before = s.screenPosition
					}
					if i == len(prefix)+len(blocked) {
						if s.screenPosition != before || s.agent.input != nil || s.agent.visible || s.tree != nil || (s.search.query == "" && s.search.input == nil) {
							t.Fatal("normal operations leaked through search mode")
						}
					}
					if i < len(input) {
						b := input[i]
						i++
						return b, true, nil
					}
					if !idled {
						idled = true
						return 0, false, nil
					}
					if s.search.query != "" || s.search.input != nil || len(s.search.matches) != 0 {
						t.Fatal("standalone Escape did not exit search")
					}
					return 0, false, io.EOF
				}
				size := func() (int, int, error) {
					if small && i >= len(prefix) {
						return 30, 6, nil
					}
					return 120, 24, nil
				}
				if err := screenLoop(context.Background(), s, io.Discard, read, size, false); err != io.EOF {
					t.Fatalf("loop ended before Escape: %v", err)
				}
				if done, err := s.key("q", 6, 30); !done || err != nil {
					t.Fatal("quit not restored after Escape")
				}
			})
		}
	}
}

func TestCodeSearchLoopInput(t *testing.T) {
	s := agentTestScreen(t, nil)
	input := []byte("/qienst日本語\x7f\x1b")
	i := 0
	idle := false
	read := func() (byte, bool, error) {
		if i < len(input) {
			b := input[i]
			i++
			return b, true, nil
		}
		if !idle {
			idle = true
			return 0, false, nil
		}
		return 0, false, io.EOF
	}
	var out bytes.Buffer
	err := screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 120, 24, nil }, true)
	if err != io.EOF {
		t.Fatalf("loop: %v", err)
	}
	if s.search.input != nil || s.agent.input != nil || s.agent.visible || s.tree != nil && s.tree.visible {
		t.Fatal("search input leaked to commands")
	}
	if !strings.Contains(out.String(), "qienst日本") {
		t.Fatal("UTF-8 input not rendered")
	}
}
