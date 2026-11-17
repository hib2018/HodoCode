package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func editingScreen(text string) *screen {
	return &screen{agent: &agentPane{input: &agentInput{text: text}}}
}

func TestAgentInputCursorEditing(t *testing.T) {
	s := editingScreen("日a本")
	typeAgentInput(t, s, "\x1b[D!")
	in := s.agent.input
	if in.text != "日a!本" || in.cursor != len("日a!") {
		t.Fatalf("insert: text=%q cursor=%d", in.text, in.cursor)
	}
	typeAgentInput(t, s, "\x7f\x1b[3~")
	if in.text != "日a" || in.cursor != len("日a") {
		t.Fatalf("backspace/delete: text=%q cursor=%d", in.text, in.cursor)
	}
	typeAgentInput(t, s, "\x1b[H\x1b[D\x1b[3~\x1b[F\x1b[C!")
	if in.text != "a!" || in.cursor != 2 || !utf8.ValidString(in.text) {
		t.Fatalf("boundary movement: text=%q cursor=%d", in.text, in.cursor)
	}
}

func TestAgentInputLogicalLineEditing(t *testing.T) {
	s := editingScreen("前\n日本abc\n後")
	typeAgentInput(t, s, "\x1b[D\x1b[D\x01\x1b[C\x15")
	in := s.agent.input
	if in.text != "前\n本abc\n後" || in.cursor != len("前\n") {
		t.Fatalf("Ctrl+U: text=%q cursor=%d", in.text, in.cursor)
	}
	typeAgentInput(t, s, "\x1b[C\x0b")
	if in.text != "前\n本\n後" || in.cursor != len("前\n本") {
		t.Fatalf("Ctrl+K: text=%q cursor=%d", in.text, in.cursor)
	}
	typeAgentInput(t, s, "\x01\x05\x08")
	if in.text != "前\n\n後" || in.cursor != len("前\n") {
		t.Fatalf("Ctrl+A/E: text=%q cursor=%d", in.text, in.cursor)
	}
	typeAgentInput(t, s, "\x08")
	if in.text != "前\n後" || in.cursor != len("前") {
		t.Fatalf("join lines: text=%q cursor=%d", in.text, in.cursor)
	}
}

func TestAgentInputMultilineKeysAndLimit(t *testing.T) {
	s := editingScreen("a")
	typeAgentInput(t, s, "\n日\x1b\r本\x1b\nb")
	in := s.agent.input
	if in == nil || in.text != "a\n日\n本\nb" || in.escape != "" {
		t.Fatalf("newline input: %+v", in)
	}
	s = editingScreen(strings.Repeat("x", maxAgentPurpose))
	typeAgentInput(t, s, "\n\x1b\r日")
	in = s.agent.input
	if len(in.text) != maxAgentPurpose || in.status == "" || !utf8.ValidString(in.text) {
		t.Fatal("newline/UTF-8 insertion exceeded the byte limit")
	}
	typeAgentInput(t, s, "\x08\n")
	if len(in.text) != maxAgentPurpose || !strings.HasSuffix(in.text, "\n") || in.status != "" {
		t.Fatal("newline at the exact limit was rejected")
	}
}

func TestAgentInputMultilineCRSends(t *testing.T) {
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		return `["example.com/screen.First"]`, nil
	})
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "first\nsecond\r")
	if s.agent.input != nil || len(s.agent.history) != 1 || s.agent.history[0].Content != "first\nsecond" {
		t.Fatal("CR did not send the multiline draft")
	}
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
}

func TestAgentInputPromptLines(t *testing.T) {
	for _, tt := range []struct {
		name            string
		text            string
		keys            string
		width, maxLines int
		lines           []string
		row, column     int
	}{
		{"empty", "", "", 10, 3, []string{"› "}, 0, 2},
		{"wide", "日本", "\x1b[D", 10, 3, []string{"› 日本"}, 0, 4},
		{"newline", "a\nb", "", 10, 3, []string{"› a", "b"}, 1, 1},
		{"wrap", "abcdef", "", 6, 3, []string{"› abc", "def"}, 1, 3},
		{"end viewport", "a\nb\nc", "", 10, 2, []string{"b", "c"}, 1, 1},
		{"start viewport", "a\nb\nc", "\x01\x1b[D\x01\x1b[D\x01", 10, 2, []string{"› a", "b"}, 0, 2},
		{"spaces", "a  ", "", 10, 3, []string{"› a  "}, 0, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := editingScreen(tt.text)
			typeAgentInput(t, s, tt.keys)
			lines, row, column := s.agent.input.promptLines(tt.width, tt.maxLines)
			if !reflect.DeepEqual(lines, tt.lines) || row != tt.row || column != tt.column {
				t.Fatalf("got %q (%d,%d), want %q (%d,%d)", lines, row, column, tt.lines, tt.row, tt.column)
			}
		})
	}
}

func TestAgentInputRegularChatIsNotModal(t *testing.T) {
	s := editingScreen("draft")
	if got := s.renderAgentInput(80, 24); got != "" {
		t.Fatalf("regular chat still renders a modal: %q", got)
	}
	s.agent.input.line = 1
	if got := s.renderAgentInput(80, 24); !strings.Contains(got, "行質問") || !strings.Contains(got, "› draft") {
		t.Fatalf("line question lost its modal: %q", got)
	}
}

func TestAgentInputPromptLinesBounds(t *testing.T) {
	for _, text := range []string{"", "日本語\nabc\n", "a\n\n", "e\u0301 末尾 ", "x\x1b[2J\r\t\u202e"} {
		for cursor := 0; cursor <= len(text); cursor++ {
			if cursor < len(text) && !utf8.RuneStart(text[cursor]) {
				continue
			}
			for width := 1; width <= 16; width++ {
				for maxLines := 1; maxLines <= 4; maxLines++ {
					in := &agentInput{text: text, cursor: cursor, cursorSet: true}
					lines, row, column := in.promptLines(width, maxLines)
					if len(lines) > maxLines || row < 0 || row >= len(lines) || column < 0 || column >= width {
						t.Fatalf("text=%q cursor=%d width=%d maxLines=%d: lines=%q cursor=(%d,%d)", text, cursor, width, maxLines, lines, row, column)
					}
					for _, line := range lines {
						if displayWidth(line) > width || !utf8.ValidString(line) || strings.ContainsAny(line, "\x1b\r\n\t\u202e") {
							t.Fatalf("unsafe prompt line: %q", line)
						}
					}
				}
			}
		}
	}
}
