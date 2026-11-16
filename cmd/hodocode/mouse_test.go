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

func TestMouseDecoder(t *testing.T) {
	for _, tc := range []struct {
		input  string
		text   string
		events []mouseEvent
	}{
		{"\x1b[Maq\"", "", []mouseEvent{{81, 2, 1}}},
		{"\x1b[M q\"", "", []mouseEvent{{81, 2, 0}}},
		{"\x1b[M\x03q", "\x03q", nil},
		{"\x1b[M\x04q", "\x04q", nil},
		{"\x1b[M\x1b[<64;3;4M", "", []mouseEvent{{3, 4, -1}}},
		{"\x1b[<65;2q", "q", nil},
		{"\x1b[<65;2\r", "\r", nil},
		{"abc\x1b[A\x1bOBe", "abc\x1b[A\x1bOBe", nil},
		{"\x1b[<64;1;2M\x1b[<65;1234;56M", "", []mouseEvent{{1, 2, -1}, {1234, 56, 1}}},
		{"\x1b[<92;10;2M", "", []mouseEvent{{10, 2, -1}}},
		{"\x1b[<0;10;2M\x1b[<0;10;2m\x1b[<66;10;2M\x1b[<96;10;2M", "", []mouseEvent{{10, 2, 0}}},
		{"\x1b[<65;0;2M\x1b[<65;-1;2M\x1b[<65;1M\x1b[<65;1;2;3M", "", nil},
		{"\x1b[<" + strings.Repeat("9", 100) + ";2;2Mx", "x", nil},
		{"\x1b[<65;2\x03q", "\x03q", nil},
		{"\x1b[<65;2\x1b[<64;3;4M", "", []mouseEvent{{3, 4, -1}}},
	} {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			var d mouseDecoder
			var text string
			var events []mouseEvent
			for _, b := range []byte(tc.input) {
				out, event := d.feed(b)
				text += out
				if event != nil {
					events = append(events, *event)
				}
				if len(d.pending) > 64 {
					t.Fatal("unbounded mouse report")
				}
			}
			if text != tc.text || !reflect.DeepEqual(events, tc.events) {
				t.Fatalf("text=%q events=%v, want %q %v", text, events, tc.text, tc.events)
			}
		})
	}
}

func TestMouseIndependentScroll(t *testing.T) {
	s := testScreen(t, "Recur")
	// Keep real call/explanation metadata, but provide enough content to scroll.
	for len(s.code) < 30 {
		s.code = append(s.code, "99 │ // extra")
	}
	const width, height = 90, 8
	s.render(width, height, false)
	s.explanationTop = 2
	before := s.screenPosition
	notes := strings.Join(s.explanationLines(width/3-2, height-3), "\n")
	for range 8 {
		s.handleMouse(mouseEvent{1, 2, 1}, width, height)
	}
	s.render(width, height, false)
	if s.top != before.top+8 {
		t.Fatalf("left scroll=%d", s.top)
	}
	got := s.screenPosition
	got.top = before.top
	if got != before {
		t.Fatalf("left scroll changed other state: %+v => %+v", before, got)
	}
	if current := strings.Join(s.explanationLines(width/3-2, height-3), "\n"); current != notes {
		t.Fatal("left scroll changed explanation after cursor left viewport")
	}
	before = s.screenPosition
	s.handleMouse(mouseEvent{width, 2, 1}, width, height)
	if s.explanationTop != before.explanationTop+1 {
		t.Fatal("right did not scroll")
	}
	got = s.screenPosition
	got.explanationTop = before.explanationTop
	if got != before {
		t.Fatal("right scroll changed code/cursor/selection")
	}
	for _, x := range []int{1, width} {
		for range 200 {
			s.handleMouse(mouseEvent{x, 2, 1}, width, height)
		}
		if x == 1 && s.top != len(s.code)-(height-3) {
			t.Fatal("code bottom not clamped")
		}
		if x == width && s.explanationTop != len(s.explanationLines(width/3-2, height-3))-(height-3) {
			t.Fatal("explanation bottom not clamped")
		}
		for range 200 {
			s.handleMouse(mouseEvent{x, 2, -1}, width, height)
		}
	}
	if s.top != 0 || s.explanationTop != 0 {
		t.Fatal("top not clamped")
	}
	for _, event := range []mouseEvent{{0, 2, 1}, {width + 1, 2, 1}, {1, 1, 1}, {1, height - 1, 1}, {1, height, 1}, {screenCodeWidth(width) + 1, 2, 1}, {screenCodeWidth(width) + 2, 2, 1}, {screenCodeWidth(width) + 3, 2, 1}} {
		before = s.screenPosition
		if s.handleMouse(event, width, height) || before != s.screenPosition {
			t.Fatalf("invalid region scrolled: %v", event)
		}
	}
	// x=50 is code at 90 columns, but the right pane at 60 columns.
	before = s.screenPosition
	s.handleMouse(mouseEvent{50, 2, 1}, 60, height)
	if s.top != before.top || s.explanationTop != 1 {
		t.Fatal("resize used stale pane boundaries")
	}
}

func TestMouseAgentScrollAndSwitch(t *testing.T) {
	s := testScreen(t, "Recur")
	s.agent = newAgentPane(agentOptions{})
	s.explanationTop = 2
	s.agent.scroll = 3
	s.agentKey(context.Background(), "e")
	s.agent.input = nil
	s.agent.text = strings.Repeat("conversation\n", 40)
	s.render(90, 8, false)
	before := s.screenPosition
	s.handleMouse(mouseEvent{90, 2, 1}, 90, 8)
	if s.explanationTop != 4 || s.top != before.top || s.cursorRow != before.cursorRow || s.cursorColumn != before.cursorColumn {
		t.Fatal("agent scroll moved left pane")
	}
	s.agentKey(context.Background(), "s")
	if s.explanationTop != 2 {
		t.Fatal("syntax position lost")
	}
	s.agentKey(context.Background(), "e")
	if s.explanationTop != 4 {
		t.Fatal("agent position lost")
	}
}

func TestMouseScreenLoop(t *testing.T) {
	for _, mode := range []string{"normal", "search", "agent"} {
		t.Run(mode, func(t *testing.T) {
			s := testScreen(t, "Recur")
			for len(s.code) < 30 {
				s.code = append(s.code, "99 │ // extra")
			}
			s.agent = newAgentPane(agentOptions{})
			if mode == "search" {
				s.search.input = &agentInput{text: "検索"}
			}
			if mode == "agent" {
				s.agent.input = &agentInput{text: "質問"}
			}
			// Every byte after CSI is separated by a read timeout.
			input := "\x1b[<65;1;2M\x1b[<65;1;2M\x1b[<2;1;2M\x1b[<2;1;2m\x1b[M q\""
			index, idle := 0, false
			var frames []string
			var out bytes.Buffer
			read := func() (byte, bool, error) {
				frames = append(frames, out.String())
				out.Reset()
				if index == len(input) {
					return 0, false, io.EOF
				}
				if idle {
					idle = false
					return 0, false, nil
				}
				b := input[index]
				index++
				idle = b != 0x1b
				return b, true, nil
			}
			err := screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 90, 8, nil }, false)
			if err != io.EOF {
				t.Fatal(err)
			}
			if s.top != 2 {
				t.Fatalf("scroll=%d, want 2", s.top)
			}
			if mode == "search" && (s.search.input == nil || s.search.input.text != "検索") {
				t.Fatal("mouse leaked into search")
			}
			if mode == "agent" && (s.agent.input == nil || s.agent.input.text != "質問") {
				t.Fatal("mouse leaked into agent input")
			}
			if mode == "normal" {
				last := ""
				for _, f := range frames {
					if f != "" {
						last = f
					}
				}
				if strings.Contains(last, "\x1b[?25h") {
					t.Fatal("offscreen code cursor was shown")
				}
			}
		})
	}
}

func TestMouseResizeKeepsOffscreenCursor(t *testing.T) {
	s := testScreen(t, "Recur")
	for len(s.code) < 30 {
		s.code = append(s.code, "99 │ // extra")
	}
	row, column, selected := s.cursorRow, s.cursorColumn, s.selected
	input := "\x1b[<65;1;2M\x1b[<65;1;2M\x1b[<65;1;2M"
	index := 0
	var out bytes.Buffer
	read := func() (byte, bool, error) {
		if index == len(input) {
			return 0, false, io.EOF
		}
		b := input[index]
		index++
		return b, true, nil
	}
	size := func() (int, int, error) {
		if index == len(input) {
			out.Reset()
			return 60, 9, nil
		}
		return 90, 8, nil
	}
	if err := screenLoop(context.Background(), s, &out, read, size, false); err != io.EOF {
		t.Fatal(err)
	}
	if s.top != 3 || s.cursorRow != row || s.cursorColumn != column || s.selected != selected {
		t.Fatalf("resize pulled viewport to cursor or changed selection: %+v", s.screenPosition)
	}
	if strings.Contains(out.String(), "\x1b[?25h") {
		t.Fatal("resize showed offscreen cursor")
	}
}

func TestMouseTruncatedReportTimeout(t *testing.T) {
	s := testScreen(t, "Recur")
	input := "\x1b[<65;1"
	index, timeouts := 0, 0
	read := func() (byte, bool, error) {
		if index < len(input) {
			b := input[index]
			index++
			return b, true, nil
		}
		if timeouts < 10 {
			timeouts++
			return 0, false, nil
		}
		if index == len(input) {
			index++
			return 'q', true, nil
		}
		return 0, false, io.EOF
	}
	if err := screenLoop(context.Background(), s, io.Discard, read, func() (int, int, error) { return 90, 8, nil }, false); err != nil {
		t.Fatalf("keyboard did not recover after truncated report: %v", err)
	}
}

func TestMouseClickCodePosition(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		line                  string
		x, horizontal, column int
	}{
		{"ASCII", "123 │ value()", 9, 0, 2},
		{"gutter", "123 │ value()", 2, 0, 0},
		{"past line end", "123 │ value()", 40, 0, 6},
		{"empty", "123 │ ", 10, 0, 0},
		{"wide first cell", "1 │ あ\t名前éx", 5, 0, 0},
		{"wide second cell", "1 │ あ\t名前éx", 6, 0, 0},
		{"tab first cell", "1 │ あ\t名前éx", 7, 0, 1},
		{"tab second cell", "1 │ あ\t名前éx", 8, 0, 1},
		{"Japanese", "1 │ あ\t名前éx", 10, 0, 2},
		{"combining mark", "1 │ あ\t名前éx", 14, 0, 6},
		{"horizontal", "123 │ value()", 3, 7, 3},
		{"clipped tab", "1 │ あ\t名前éx", 1, 7, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &screen{code: []string{"1 │ before", tc.line, "3 │ after"}}
			s.top, s.horizontal, s.explanationTop = 1, tc.horizontal, 5
			if !s.handleMouse(mouseEvent{tc.x, 2, 0}, 90, 8) {
				t.Fatal("click ignored")
			}
			if s.cursorRow != 1 || s.cursorColumn != tc.column {
				t.Fatalf("cursor=(%d,%d), want (1,%d)", s.cursorRow, s.cursorColumn, tc.column)
			}
			if s.top != 1 || s.horizontal != tc.horizontal {
				t.Fatal("click scrolled viewport")
			}
			if s.explanationTop != 0 {
				t.Fatal("local explanation not reset for new cursor")
			}
		})
	}
}

func TestMouseClickOutsideCode(t *testing.T) {
	for _, event := range []mouseEvent{{1, 1, 0}, {1, 7, 0}, {1, 8, 0}, {58, 2, 0}, {59, 2, 0}, {60, 2, 0}, {61, 2, 0}, {90, 2, 0}, {1, 4, 0}} {
		s := &screen{code: []string{"1 │ first", "2 │ second"}}
		before := s.screenPosition
		if s.handleMouse(event, 90, 8) || s.screenPosition != before {
			t.Fatalf("non-code click changed cursor: %v", event)
		}
	}
	for _, mode := range []string{"search", "agent", "tree"} {
		s := &screen{code: []string{"1 │ first", "2 │ second"}}
		switch mode {
		case "search":
			s.search.input = &agentInput{text: "検索"}
		case "agent":
			s.agent = &agentPane{input: &agentInput{text: "質問"}}
		case "tree":
			s.tree = &callTree{visible: true}
		}
		before := s.screenPosition
		if s.handleMouse(mouseEvent{7, 3, 0}, 90, 8) || s.screenPosition != before {
			t.Fatalf("click moved code cursor during %s input/overlay", mode)
		}
	}
}

func TestMouseClickScreenLoop(t *testing.T) {
	for _, input := range []string{"\x1b[<0;10;3M\x1b[<0;10;3m", "\x1b[M *#\x1b[M#*#"} {
		p := screenProject(t, map[string]string{"main.go": "package main\nfunc Leaf() {}\nfunc main() {\n    Leaf()\n}\n"})
		s, err := newScreen(p, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		s.cursorRow, s.cursorColumn = 0, 0
		current := s.current
		index := 0
		read := func() (byte, bool, error) {
			if index == len(input) {
				return 0, false, io.EOF
			}
			b := input[index]
			index++
			return b, true, nil
		}
		if err := screenLoop(context.Background(), s, io.Discard, read, func() (int, int, error) { return 90, 8, nil }, false); err != io.EOF {
			t.Fatal(err)
		}
		if s.cursorRow != 1 || s.cursorColumn != 5 || s.callAtCursor() != 0 || s.selected != 0 {
			t.Fatalf("click did not select call: %+v", s.screenPosition)
		}
		if s.current != current || len(s.history) != 0 {
			t.Fatal("click followed call instead of moving cursor")
		}
		if _, err := s.key("\r", 5, 57); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(s.current, ".Leaf") {
			t.Fatal("Enter did not follow clicked call")
		}
	}
}

func TestMouseClickKeepsAgentScroll(t *testing.T) {
	s := &screen{code: []string{"1 │ first", "2 │ second"}, agent: &agentPane{visible: true}}
	s.explanationTop = 5
	s.handleMouse(mouseEvent{7, 3, 0}, 90, 8)
	if s.cursorRow != 1 || s.explanationTop != 5 {
		t.Fatal("click did not move code cursor or changed agent scroll")
	}
}
