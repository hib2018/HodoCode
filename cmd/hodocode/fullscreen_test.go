package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func testScreen(t *testing.T, name string) *screen {
	t.Helper()
	p, err := analyze.Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	s, err := newScreen(p, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func press(t *testing.T, s *screen, keys ...string) {
	t.Helper()
	for _, key := range keys {
		done, err := s.key(key, 6, 30)
		if err != nil || done {
			t.Fatalf("key %q: done=%v err=%v", key, done, err)
		}
	}
}

func screenProject(t *testing.T, files map[string]string) *analyze.Project {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.com/screen\n\ngo 1.23\n"
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFullScreenNavigation(t *testing.T) {
	s := testScreen(t, "Recur")
	root := s.current
	if len(s.sites) != 2 || s.sites[0].Name != "Recur" || s.sites[1].Name != "Leaf" || len(s.flow.expanded) != 1 {
		t.Fatalf("source-order sites/lazy expansion: %+v", s.sites)
	}
	cached := s.flow.sites[root]
	press(t, s, "\r", "\r")
	if s.current != root || len(s.history) != 2 || len(s.flow.expanded) != 1 || &s.sites[0] != &cached[0] {
		t.Fatal("recursive navigation did not reuse cached sites")
	}
	press(t, s, "b", "b", "n")
	s.top, s.horizontal = 1, 4
	position := s.screenPosition
	press(t, s, "\n")
	if !strings.HasSuffix(s.current, ".Leaf") || len(s.flow.expanded) != 2 || len(s.sites) != 0 {
		t.Fatal("Enter did not expand the selected leaf")
	}
	leaf := s.screenPosition
	press(t, s, "\r", "\n", "down", "up")
	if s.screenPosition != leaf || len(s.history) != 1 {
		t.Fatal("Enter/navigation without calls changed state")
	}
	press(t, s, "b")
	if s.screenPosition != position || len(s.history) != 0 || &s.sites[0] != &cached[0] {
		t.Fatalf("back did not restore selection/vertical/horizontal position: %+v", s.screenPosition)
	}
	press(t, s, "b")
	if s.screenPosition != position {
		t.Fatal("back without history changed state")
	}
	press(t, s, "N", "N", "n", "n", "n")
	if s.selected != 1 || s.current != root || len(s.history) != 0 {
		t.Fatal("selection must clamp without following calls")
	}
	for _, key := range []string{"q", "\x03", "\x04"} {
		if done, err := s.key(key, 6, 30); !done || err != nil {
			t.Fatalf("quit %q: %v, %v", key, done, err)
		}
	}
}

func TestFullScreenCursorMovement(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n    Leaf()\n    value := 1\n    _ = value\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.cursorRow != 1 || s.cursorColumn != 4 || s.callAtCursor() != 0 {
		t.Fatalf("initial cursor must jump to the first call: %+v", s.screenPosition)
	}
	for _, tc := range []struct {
		key      string
		row, col int
	}{
		{"h", 1, 3}, {"l", 1, 4}, {"right", 1, 5}, {"left", 1, 4},
		{"j", 2, 4}, {"down", 3, 4}, {"up", 2, 4}, {"k", 1, 4},
		{"0", 1, 0}, {"h", 1, 0}, {"$", 1, 9}, {"l", 1, 9},
		{"g", 0, 0}, {"k", 0, 0}, {"w", 0, 5}, {"B", 0, 0},
		{"G", 4, 0}, {"j", 4, 0}, {"g", 0, 0},
	} {
		press(t, s, tc.key)
		if s.cursorRow != tc.row || s.cursorColumn != tc.col || len(s.history) != 0 || len(s.flow.expanded) != 1 {
			t.Fatalf("key %q: cursor=(%d,%d), want (%d,%d), history=%d expanded=%d", tc.key, s.cursorRow, s.cursorColumn, tc.row, tc.col, len(s.history), len(s.flow.expanded))
		}
	}
}

func TestFullScreenEnterOffCall(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n    Leaf() // Leaf()\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		row, col int
	}{
		{"declaration", 0, 5}, {"whitespace", 1, 0},
		{"opening parenthesis", 1, 8}, {"closing parenthesis", 1, 9},
		{"comment call name", 1, 14}, {"closing brace", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.cursorRow, s.cursorColumn = tc.row, tc.col
			s.followCursor(6, 30)
			before := s.screenPosition
			press(t, s, "\r", "\n")
			if s.callAtCursor() != -1 || s.screenPosition != before || len(s.history) != 0 || len(s.flow.expanded) != 1 {
				t.Fatalf("Enter off-call followed the stale selection: %+v", s.screenPosition)
			}
			if line := s.codeLine(1, 80, true); strings.Contains(line, "\x1b[97;44m") {
				t.Fatalf("off-call cursor left a selected call highlight: %q", line)
			}
			if frame := s.render(160, 12, false); !strings.Contains(frame, "カーソル "+s.cursorStatus()) {
				t.Fatalf("off-call status must show cursor position: %q", frame)
			}
		})
	}
	press(t, s, "n", "l", "\r")
	if !strings.HasSuffix(s.current, ".Leaf") || len(s.history) != 1 {
		t.Fatal("Enter inside, not just at the start of, a call name must follow it")
	}
}

func TestFullScreenUnicodeTabCursor(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc 日本語() {}\nfunc Leaf() {}\nfunc main() {\n\t日本語();\tLeaf()\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key       string
		col, cell int
		call      int
	}{
		{"h", 0, 4, -1}, {"l", 1, 8, 0}, {"l", 2, 10, 0},
		{"right", 3, 12, 0}, {"l", 4, 14, -1},
		{"n", 8, 24, 1}, {"N", 1, 8, 0}, {"N", 1, 8, 0},
	} {
		press(t, s, tc.key)
		if s.cursorRow != 1 || s.cursorColumn != tc.col || s.cursorCell() != tc.cell || s.callAtCursor() != tc.call {
			t.Fatalf("key %q: position=%+v cell=%d call=%d, want col=%d cell=%d call=%d", tc.key, s.screenPosition, s.cursorCell(), s.callAtCursor(), tc.col, tc.cell, tc.call)
		}
	}
	press(t, s, "l")
	s.followCursor(1, 1)
	if s.top != 1 || s.horizontal != 10 {
		t.Fatalf("resize must follow the cursor cell, not the whole selected call: %+v", s.screenPosition)
	}
	position := s.screenPosition
	press(t, s, "\r", "b")
	if s.screenPosition != position {
		t.Fatalf("back must restore Unicode cursor and viewport: %+v, want %+v", s.screenPosition, position)
	}
}

func TestFullScreenCursorRightPaneCoupling(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n    Leaf()\n    value := 1\n    _ = value\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, label string }{
		{"n", `"Leaf"`}, {"j", `"value"`}, {"w", ":="}, {"B", `"value"`},
	} {
		s.explanationTop = 3
		press(t, s, tc.key)
		notes := strings.Join(s.paneLines(200, 6), "\n")
		position := fmt.Sprintf("%d行 %d列: %s:", s.p.Functions[s.current].Line+s.cursorRow, s.cursorColumn+1, tc.label)
		if s.explanationTop != 0 || !strings.Contains(notes, position) {
			t.Fatalf("key %q: pane must follow cursor and reset scroll: %q", tc.key, notes)
		}
		if s.selected != 0 || len(s.history) != 0 {
			t.Fatal("syntax navigation must not follow or discard the selected call")
		}
		if frame := s.render(240, 12, false); !strings.Contains(frame, position) {
			t.Fatalf("rendered right pane missing cursor explanation %q: %q", position, frame)
		}
	}
}

func TestFullScreenDefaultMainAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, source, other, want string
	}{
		{"unique", "package main\nfunc main() {}\n", "", ""},
		{"absent", "package sample\nfunc main() {}\n", "", "main関数がありません"},
		{"multiple", "package main\nfunc main() {}\n", "package main\nfunc main() {}\n", "main関数が複数"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"main.go": tc.source}
			if tc.other != "" {
				files["other/main.go"] = tc.other
			}
			p := screenProject(t, files)
			s, err := newScreen(p, "", nil)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "--func") {
					t.Fatalf("default start: %v", err)
				}
				return
			}
			if err != nil || s.current != "example.com/screen.main" || s.flow == nil || len(s.flow.expanded) != 1 {
				t.Fatalf("automatic main: %+v, %v", s, err)
			}
		})
	}
	s := testScreen(t, "Recur")
	for _, tc := range []struct {
		name   string
		expand []string
	}{
		{"missingFunction", nil}, {"Recur", []string{"missingFunction"}}, {"Recur", []string{"Hidden"}},
	} {
		if _, err := newScreen(s.p, tc.name, tc.expand); err == nil {
			t.Fatalf("accepted invalid start/expansion: %+v", tc)
		}
	}
	// Force expansion and refresh failures without modifying production source.
	s.selected = 1
	s.jumpCall()
	s.flow.nodes[s.sites[1].To] = false
	if _, err := s.key("\r", 6, 30); err == nil || len(s.history) != 0 {
		t.Fatal("expansion failure was swallowed or pushed history")
	}
	s.current = "missingFunction"
	if err := s.refresh(); err == nil {
		t.Fatal("refresh failure was swallowed")
	}
}

func TestFullScreenCallHighlights(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": `package main
import "strings"
type Thing struct{}
func (Thing) Run() {}
func Z() {}
func A() {}
func 日本語() {}
func main() {
	Z(); Z(); A(); Z() // Z() is only a comment
	thing := Thing{}; thing.Run(); Thing.Run(thing)
	日本語(); Z()
	strings.TrimSpace("Z()"); missing()
}
`})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Z", "Z", "A", "Z", "Run", "Run", "日本語", "Z", "TrimSpace", "missing"}
	if len(s.sites) != len(want) {
		t.Fatalf("sites: %+v", s.sites)
	}
	for i, name := range want {
		if s.sites[i].Name != name || s.selected != i {
			t.Fatalf("site %d: %+v, selected=%d", i, s.sites[i], s.selected)
		}
		h := s.highlights[i]
		if got := displaySlice(s.code[h.row], h.start, h.end-h.start); got != name {
			t.Fatalf("name-only highlight %d = %q, want %q", i, got, name)
		}
		style := "\x1b[97;44m\x1b[1m"
		if i >= 8 {
			style = "\x1b[97;44m\x1b[1m\x1b[4m"
		}
		line := s.codeLine(h.row, 160, true)
		if !strings.Contains(line, style+name+"\x1b[0m") || strings.Count(line, "\x1b[1m") != 1 {
			t.Fatalf("selected name styling %d: %q", i, line)
		}
		if i == 0 && (!strings.Contains(line, "\x1b[30;46mZ\x1b[0m();") || !strings.Contains(line, "// Z() is only a comment")) {
			t.Fatalf("repeated call/comment styling: %q", line)
		}
		if i == 4 && !strings.Contains(line, "thing.\x1b[97;44m\x1b[1mRun\x1b[0m()") {
			t.Fatalf("receiver or punctuation included in method highlight: %q", line)
		}
		frame := s.render(160, 18, false)
		for _, ansi := range []string{"\x1b[35m", "\x1b[32m", "\x1b[36m", "\x1b[90m", "\x1b[30;46m", "\x1b[30;43m", "\x1b[97;44m", "\x1b[1m", "\x1b[4m", "\x1b[0m"} {
			if strings.Contains(frame, ansi) {
				t.Fatalf("emphasis=false emitted %q", ansi)
			}
		}
		position := fmt.Sprintf("main.go:%d:%d", s.sites[i].Line, s.sites[i].Column)
		if !strings.Contains(frame, position) || !strings.Contains(frame, fmt.Sprintf("%d/%d", i+1, len(want))) {
			t.Errorf("plain status missing selected position %s for %s", position, name)
		}
		press(t, s, "n")
	}
	// Clipping inside a wide name must not split a UTF-8 rune or overflow cells.
	s.selected = 6
	s.jumpCall()
	h := s.highlights[6]
	for _, start := range []int{h.start - 1, h.start + 1, h.end - 1} {
		s.horizontal = start
		got := s.codeLine(h.row, 4, true)
		plain := strings.NewReplacer("\x1b[97;44m", "", "\x1b[30;46m", "", "\x1b[1m", "", "\x1b[0m", "").Replace(got)
		if plain != displaySlice(s.code[h.row], start, 4) || displayWidth(plain) != 4 {
			t.Fatalf("Unicode/tab clip at %d: %q", start, got)
		}
	}
	press(t, s, "\r")
	if s.current != "example.com/screen.日本語" {
		t.Fatal("Unicode function navigation failed")
	}
	press(t, s, "b")
	s.selected = 4
	s.jumpCall()
	press(t, s, "\r")
	if s.current != "example.com/screen.Thing.Run" {
		t.Fatal("method navigation failed")
	}
}

func TestFullScreenUnavailableCalls(t *testing.T) {
	for _, tc := range []struct{ name, call, reason string }{
		{"helper.Work", "TrimSpace", "対象範囲内にソースがない"},
		{"Unchecked", "missingSymbol", "呼び出し先を静的に特定できない"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testScreen(t, tc.name)
			if len(s.sites) != 1 || s.sites[0].Name != tc.call {
				t.Fatalf("unavailable call must be selectable: %+v", s.sites)
			}
			before := s.screenPosition
			press(t, s, "\r", "\n", "\t", "d")
			if s.screenPosition != before || len(s.history) != 0 || len(s.flow.expanded) != 1 {
				t.Fatal("unavailable Enter/Tab/d changed state")
			}
			frame := s.render(160, 16, true)
			if !strings.Contains(frame, tc.reason) || !strings.Contains(frame, "\x1b[97;44m\x1b[1m\x1b[4m"+tc.call+"\x1b[0m") {
				t.Fatal(frame)
			}
		})
	}
}

func TestFullScreenScrollAndResize(t *testing.T) {
	s := testScreen(t, "Entry")
	root := s.current
	before := s.screenPosition
	press(t, s, "\t", "d", "b")
	if s.screenPosition != before {
		t.Fatal("Tab/d/back without history must be no-ops")
	}
	for _, key := range []string{"right", "l", "left", "h"} {
		previous := s.screenPosition
		press(t, s, key)
		want := previous.cursorColumn + 1
		if key == "left" || key == "h" {
			want = previous.cursorColumn - 1
		}
		if s.cursorColumn != want || s.horizontal != previous.horizontal || s.cursorRow != previous.cursorRow || s.top != previous.top || s.current != root || len(s.history) != 0 {
			t.Fatalf("character movement %q: %+v", key, s.screenPosition)
		}
	}
	press(t, s, "pagedown")
	if s.top != 6 || s.selected != before.selected {
		t.Fatalf("page height must be code body height: %+v", s.screenPosition)
	}
	press(t, s, "pageup", "pageup")
	if s.top != 0 {
		t.Fatal("page-up did not clamp")
	}
	for range 10 {
		press(t, s, "pagedown", "right")
	}
	if s.top != len(s.code)-6 {
		t.Fatal("page-down did not clamp")
	}
	s.selected = len(s.sites) - 1
	s.jumpCall()
	s.follow(3, 12)
	h := s.highlights[s.selected]
	if h.row < s.top || h.row >= s.top+3 || h.start < s.horizontal || h.end > s.horizontal+12 {
		t.Fatalf("selection not visible: %+v highlight=%+v", s.screenPosition, h)
	}
	s.selected = 0
	s.jumpCall()
	s.follow(3, 2)
	h = s.highlights[0]
	if s.horizontal != h.start || h.row < s.top || h.row >= s.top+3 {
		t.Fatal("long name should follow from its beginning")
	}
	for _, size := range [][2]int{{41, 8}, {42, 8}, {80, 15}, {101, 12}} {
		s.followCursor(size[1]-3, screenCodeWidth(size[0]))
		position := s.screenPosition
		frame := s.render(size[0], size[1], false)
		rows := 0
		for i, line := range strings.Split(frame, "\x1b[") {
			if i == 0 {
				continue
			}
			_, line, ok := strings.Cut(line, "H")
			if !ok || line == "" {
				continue
			}
			rows++
			if got := displayWidth(line); got != size[0] {
				t.Fatalf("row width=%d, want %d: %q", got, size[0], line)
			}
		}
		if rows != size[1] || s.current != root || s.selected != position.selected || !strings.Contains(frame, "main.go:12-32") {
			t.Fatalf("header + height-3 body + status/help: rows=%d frame=%q", rows, frame)
		}
	}
	if narrow := s.render(40, 7, false); !strings.Contains(narrow, "端末を広げてください") {
		t.Fatal(narrow)
	}
}

func TestFullScreenLoopBodyHeight(t *testing.T) {
	s := testScreen(t, "Entry")
	input := []byte("\x1b[6~q")
	read := func() (byte, bool, error) {
		b := input[0]
		input = input[1:]
		return b, true, nil
	}
	if err := screenLoop(context.Background(), s, io.Discard, read, func() (int, int, error) {
		return 80, 12, nil
	}, false); err != nil {
		t.Fatal(err)
	}
	if s.top != 9 || s.selected != 0 {
		t.Fatalf("loop must pass height-3 to page keys: %+v", s.screenPosition)
	}
}

func TestFullScreenRefreshUsesCachedSites(t *testing.T) {
	s := testScreen(t, "Recur")
	cached := append([]analyze.CallSite(nil), s.sites...)
	// A deliberately narrowed cache distinguishes refresh from a fresh analysis.
	s.flow.sites[s.current] = s.flow.sites[s.current][1:]
	s.selected = 99
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	if s.selected != 0 || !reflect.DeepEqual(s.sites, cached[1:]) || &s.sites[0] != &s.flow.sites[s.current][0] {
		t.Fatal("refresh did not use the flow cache and clamp selection")
	}
}

func TestFullScreenCRLFSource(t *testing.T) {
	p := screenProject(t, map[string]string{
		"main.go": "package example\r\nfunc Work() {\r\n\t日本語(); 日本語()\r\n}\r\nfunc 日本語() {}\r\n",
	})
	s, err := newScreen(p, "Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := s.render(120, 12, false)
	if strings.Contains(frame, "�") || !strings.Contains(frame, "3 │") || !strings.Contains(frame, "日本語(); 日本語()") {
		t.Fatal(frame)
	}
	if len(s.highlights) != 2 {
		t.Fatalf("CRLF call sites: %+v", s.sites)
	}
	for _, h := range s.highlights {
		if got := displaySlice(s.code[h.row], h.start, h.end-h.start); got != "日本語" {
			t.Fatalf("CRLF highlight offset: %q", got)
		}
	}
}

func TestDisplaySlice(t *testing.T) {
	for _, tc := range []struct {
		text         string
		start, width int
		want         string
	}{
		{"a\tb", 0, 10, "a       b "},
		{"a\tb", 4, 5, "    b"},
		{"日本abc", 1, 5, " 本ab"},
		{"日本abc", 0, 3, "日 "},
		{"e\u0301x", 0, 1, "e\u0301"},
		{"\x1b[31m", 0, 5, "�[31m"},
		{"a\u202eb", 0, 3, "ab "},
		{"ab", 20, 2, "  "},
	} {
		if got := displaySlice(tc.text, tc.start, tc.width); got != tc.want {
			t.Errorf("slice(%q,%d,%d)=%q want %q", tc.text, tc.start, tc.width, got, tc.want)
		}
	}
}

func TestFullScreenKeyDecoder(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\x1b[A", "up"}, {"\x1bOB", "down"}, {"\x1b[C", "right"}, {"\x1b[D", "left"},
		{"\x1b[5~", "pageup"}, {"\x1b[6~", "pagedown"}, {"j", "j"}, {"\t", "\t"}, {"\r", "\r"},
		{"\x1b\x03", "\x03"}, {"\x1b[q", "q"},
	} {
		var decoder keyDecoder
		var got string
		for _, b := range []byte(tc.input) {
			got = decoder.feed(b)
		}
		if got != tc.want {
			t.Errorf("decode(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}

func TestFullScreenLoop(t *testing.T) {
	s := testScreen(t, "Recur")
	var out bytes.Buffer
	input := []byte{'n', '\r', '\t', 'j', '\t', 'b', 'q'}
	calls := 0
	read := func() (byte, bool, error) { b := input[0]; input = input[1:]; calls++; return b, true, nil }
	size := func() (int, int, error) {
		if calls < 3 {
			return 80, 12, nil
		}
		return 101, 18, nil
	}
	if err := screenLoop(context.Background(), s, &out, read, size, false); err != nil {
		t.Fatal(err)
	}
	if s.selected != 1 || len(s.history) != 0 || !strings.Contains(out.String(), "main.go:34-37") {
		t.Fatal("loop lost state after navigation/resize")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := screenLoop(cancelled, s, io.Discard, read, size, false); err != nil {
		t.Fatal(err)
	}
	want := errors.New("terminal disconnected")
	if err := screenLoop(context.Background(), s, io.Discard, func() (byte, bool, error) { return 0, false, want }, size, false); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := screenLoop(context.Background(), s, io.Discard, read, func() (int, int, error) { return 0, 0, want }, false); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := screenLoop(context.Background(), s, errorWriter{want}, read, size, false); !errors.Is(err, want) {
		t.Fatal(err)
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write(p []byte) (int, error) { return 0, w.err }
