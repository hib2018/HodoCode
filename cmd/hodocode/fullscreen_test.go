package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestFullScreenNavigation(t *testing.T) {
	for _, keys := range [][]string{{"down", "\r", "b"}, {"j", "l", "h"}, {"down", "right", "left"}} {
		s := testScreen(t, "Recur")
		root := s.current
		if len(s.flow.expanded) != 1 {
			t.Fatal("eager expansion")
		}
		press(t, s, keys[0], keys[1])
		if s.current != root || len(s.history) != 1 || len(s.flow.expanded) != 1 {
			t.Fatal("recursive navigation or caching failed")
		}
		if strings.Count(s.render(100, 15, false), "Recur") < 3 {
			t.Fatal("missing parent/current/callee")
		}
		press(t, s, keys[2])
		if len(s.history) != 0 || s.selected != 1 {
			t.Fatal("back did not restore selection")
		}
		press(t, s, "up", "\r")
		if !strings.HasSuffix(s.current, ".Leaf") || len(s.flow.expanded) != 2 {
			t.Fatal("failed to expand selected callee")
		}
		if len(s.choices) != 0 {
			t.Fatal("leaf has choices")
		}
		press(t, s, "\r", "b")
		if s.current != root {
			t.Fatal("back from leaf")
		}
	}
}

func TestFullScreenPicking(t *testing.T) {
	s := testScreen(t, "")
	if s.flow != nil || !s.picking {
		t.Fatal("initial list should not expand calls")
	}
	first := s.current
	press(t, s, "j")
	if s.current == first || s.rows[s.selectedRow()].id != s.current {
		t.Fatal("preview did not follow candidate")
	}
	source, err := s.p.Source(s.current)
	if err != nil || !strings.Contains(s.code[0], strings.Split(source, "\n")[0]) {
		t.Fatal("wrong preview")
	}
	press(t, s, "l")
	if !s.picking {
		t.Fatal("only Enter should confirm starting point")
	}
	press(t, s, "\r")
	if s.picking || s.flow == nil || len(s.flow.expanded) != 1 {
		t.Fatal("starting point not confirmed")
	}
}

func TestFullScreenDetails(t *testing.T) {
	for _, tc := range []struct{ name, summary, detail string }{
		{"helper.Work", "外部ソース: 1件", "strings.TrimSpace（外部ソース・選択不可）"},
		{"Unchecked", "呼び出し先不明: 1件", "missingSymbol（呼び出し先不明・選択不可）"},
	} {
		s := testScreen(t, tc.name)
		before := s.render(160, 16, false)
		if !strings.Contains(before, tc.summary) || len(s.choices) != 0 {
			t.Fatal(before)
		}
		press(t, s, "d")
		after := s.render(160, 16, false)
		if !strings.Contains(after, tc.detail) || len(s.choices) != 0 {
			t.Fatal(after)
		}
		press(t, s, "d")
		if s.render(160, 16, false) != before {
			t.Fatal("details toggle did not restore view")
		}
	}
}

func TestFullScreenScrollAndResize(t *testing.T) {
	s := testScreen(t, "Entry")
	s.follow(6)
	selected := s.selected
	press(t, s, "\t", "j", "l")
	if s.top != 1 || s.horizontal != 4 || s.selected != selected || s.left != 0 {
		t.Fatalf("independent source scroll: %+v", s.screenPosition)
	}
	rightTop, rightX := s.top, s.horizontal
	press(t, s, "\t", "j", "j", "j", "j", "j")
	s.follow(3)
	if s.top != rightTop || s.horizontal != rightX || s.left == 0 {
		t.Fatalf("independent hierarchy scroll: %+v", s.screenPosition)
	}
	current, choice := s.current, s.rows[s.selectedRow()].id
	s.render(41, 8, false)
	s.follow(18)
	frame := s.render(120, 20, false)
	if s.current != current || s.rows[s.selectedRow()].id != choice {
		t.Fatal("resize changed selected function")
	}
	if !strings.Contains(frame, "main.go:12-32") || !strings.Contains(frame, "13 │") {
		t.Fatal(frame)
	}
	for _, size := range [][2]int{{41, 8}, {42, 8}, {80, 15}, {101, 12}} {
		frame = s.render(size[0], size[1], false)
		for i, line := range strings.Split(frame, "\x1b[") {
			if i == 0 {
				continue
			}
			_, line, ok := strings.Cut(line, "H")
			if !ok || line == "" {
				continue
			}
			if got := displayWidth(line); got != size[0] {
				t.Fatalf("row width=%d, want %d: %q", got, size[0], line)
			}
		}
	}
	narrow := s.render(40, 7, false)
	if !strings.Contains(narrow, "端末を広げてください") {
		t.Fatal(narrow)
	}
	if strings.Contains(s.render(100, 12, false), "\x1b[1m") {
		t.Fatal("NO_COLOR ignored")
	}
	if !strings.Contains(s.render(100, 12, true), "\x1b[1m") {
		t.Fatal("current position not emphasized")
	}
}

func TestFullScreenLeftPageScroll(t *testing.T) {
	s := testScreen(t, "")
	press(t, s, "pagedown")
	if s.left == 0 || s.selectedRow() < s.left || s.selectedRow() >= s.left+6 {
		t.Fatal("left pane did not scroll with a visible selection")
	}
	if s.current != s.rows[s.selectedRow()].id {
		t.Fatal("preview did not follow paged selection")
	}
	press(t, s, "pageup")
	if s.left != 0 {
		t.Fatal("left page-up failed")
	}
	press(t, s, "\t", "pagedown")
	if s.left != 0 {
		t.Fatal("right scroll affected left pane")
	}
}

func TestFullScreenCRLFSource(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"go.mod":  "module example.com/crlf\n\ngo 1.23\n",
		"main.go": "package example\r\nfunc Work() {\r\n\tprintln(`日本語`)\r\n}\r\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newScreen(p, "Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := s.render(120, 12, false)
	if strings.Contains(frame, "�") || !strings.Contains(frame, "3 │") || !strings.Contains(frame, "println(`日本語`)") {
		t.Fatal(frame)
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
	input := []byte{'j', '\r', '\t', 'j', '\t', 'b', 'q'}
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
