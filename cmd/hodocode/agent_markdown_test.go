package main

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/hib2018/HodoCode/internal/explain"
)

// Styles are renderer-owned SGR sequences, never text or terminal commands.
var markdownTestStyle = regexp.MustCompile(`^(\x1b\[[0-9;]+m)+$`)

func checkMarkdownOutput(t *testing.T, lines []string, spans [][]syntaxHighlight, width int) {
	t.Helper()
	if len(lines) != len(spans) {
		t.Fatalf("line/span count = %d/%d", len(lines), len(spans))
	}
	for i, line := range lines {
		if !utf8.ValidString(line) || displayWidth(line) > width {
			t.Errorf("line %d invalid or wider than %d: %q", i, width, line)
		}
		for _, r := range line {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				t.Errorf("line %d contains terminal control %U: %q", i, r, line)
			}
		}
		end := 0
		for _, span := range spans[i] {
			if span.start < end || span.end <= span.start || span.end > displayWidth(line) || !markdownTestStyle.MatchString(span.style) {
				t.Errorf("line %d invalid or untrusted span: %+v in %q", i, span, line)
			}
			end = span.end
		}
	}
}

func TestMarkdownInlineStyles(t *testing.T) {
	for _, tc := range []struct {
		name, input, want, styled, style string
	}{
		{"heading", "## Heading", "## Heading", "Heading", "\x1b[1m"},
		{"bold", "before **bold** after", "before bold after", "bold", "\x1b[1m"},
		{"italic", "before *italic* after", "before italic after", "italic", "\x1b[3m"},
		{"code", "before `a*b` after", "before `a*b` after", "`a*b`", "\x1b[7m"},
		{"nested", "**bold *both* bold** plain", "bold both bold plain", "both", "\x1b[3m"},
		{"nested closing", "**bold *both*** plain", "bold both plain", "both", "\x1b[3m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, spans := markdownLines(tc.input, 80)
			checkMarkdownOutput(t, lines, spans, 80)
			if !reflect.DeepEqual(lines, []string{tc.want}) {
				t.Fatalf("lines = %q, want %q", lines, tc.want)
			}
			found := false
			for _, span := range spans[0] {
				if strings.Contains(displaySlice(lines[0], span.start, span.end-span.start), tc.styled) && strings.Contains(span.style, tc.style) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q style on %q: %+v", tc.style, tc.styled, spans)
			}
		})
	}
}

func TestMarkdownNestedAndEscapedDelimiters(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		spans             []syntaxHighlight
	}{
		{"bold inside italic", "*outer **inner** outer* plain", "outer inner outer plain", []syntaxHighlight{{0, 6, "\x1b[3m"}, {6, 11, "\x1b[3m\x1b[1m"}, {11, 17, "\x1b[3m"}}},
		{"delimiter inside code", "**`a**b`**", "`a**b`", []syntaxHighlight{{0, 6, "\x1b[1m\x1b[7m"}}},
		{"escaped closing marker", "*escaped \\* still* plain", "escaped * still plain", []syntaxHighlight{{0, 15, "\x1b[3m"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, spans := markdownLines(tc.input, 80)
			checkMarkdownOutput(t, lines, spans, 80)
			if !reflect.DeepEqual(lines, []string{tc.want}) {
				t.Fatalf("lines = %q, want %q", lines, tc.want)
			}
			if !reflect.DeepEqual(spans[0], tc.spans) {
				t.Fatalf("spans = %+v, want %+v", spans[0], tc.spans)
			}
		})
	}
}

func TestMarkdownOrdinaryTextRemainsLiteral(t *testing.T) {
	for _, input := range []string{"ANSWER_FROM_AGENT", "PREVIOUS_MODEL_ANSWER", "snake_case_name", "a * b * c"} {
		lines, spans := markdownLines(input, 80)
		checkMarkdownOutput(t, lines, spans, 80)
		if !reflect.DeepEqual(lines, []string{input}) || len(spans[0]) != 0 {
			t.Errorf("ordinary text interpreted as emphasis: %q -> %q, %+v", input, lines, spans)
		}
	}
}

func TestMarkdownListsAndQuoteContinuations(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		width       int
		want        []string
	}{
		{"nested list", "- first\n  - nested\n    continuation\n2. second", 40, []string{"• first", "  • nested", "    continuation", "2. second"}},
		{"list wrapping", "- abcdefghij", 8, []string{"• abcdef", "  ghij"}},
		{"quote wrapping", "> abcdefghij", 8, []string{"│ abcdef", "│ ghij"}},
		{"nested quote", "> > abcdefgh", 8, []string{"│ │ abcd", "│ │ efgh"}},
		{"quoted list", "> - abcdefgh", 8, []string{"│ • abcd", "│   efgh"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, spans := markdownLines(tc.input, tc.width)
			checkMarkdownOutput(t, lines, spans, tc.width)
			if !reflect.DeepEqual(lines, tc.want) {
				t.Fatalf("lines = %q, want %q", lines, tc.want)
			}
		})
	}
}

func TestMarkdownFencesPreserveCode(t *testing.T) {
	for _, fence := range []string{"```", "~~~"} {
		for _, closed := range []bool{false, true} {
			name := fence + "/unclosed"
			input := fence + "go\n  **x**\n\n  abcdefghijk"
			want := []string{"┌─ go", "│   **x**", "│ ", "│   abcdef", "│ ghijk"}
			if closed {
				name = fence + "/closed"
				input += "\n" + fence + "\nplain"
				want = append(want, "└─", "plain")
			}
			t.Run(name, func(t *testing.T) {
				lines, spans := markdownLines(input, 10)
				checkMarkdownOutput(t, lines, spans, 10)
				if !reflect.DeepEqual(lines, want) {
					t.Fatalf("lines = %q, want %q", lines, want)
				}
				for _, i := range []int{1, 3, 4} {
					if len(spans[i]) == 0 {
						t.Errorf("code line %d lost its style", i)
					}
				}
				if closed && len(spans[len(spans)-1]) != 0 {
					t.Fatal("code style leaked beyond closing fence")
				}
			})
		}
	}
}

func TestMarkdownFencesInsideQuotesAndLists(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"quote", "> ```go\n> **literal**\n> ```\nplain", []string{"│ ┌─ go", "│ │ **literal**", "│ └─", "plain"}},
		{"list", "- ```go\n  **literal**\n  ```\nplain", []string{"• ┌─ go", "  │ **literal**", "  └─", "plain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, spans := markdownLines(tc.input, 40)
			checkMarkdownOutput(t, lines, spans, 40)
			if !reflect.DeepEqual(lines, tc.want) {
				t.Fatalf("lines = %q, want %q", lines, tc.want)
			}
			if !reflect.DeepEqual(spans[1], []syntaxHighlight{{4, 15, "\x1b[36m"}}) {
				t.Errorf("literal code style = %+v", spans[1])
			}
			if len(spans[3]) != 0 {
				t.Errorf("code style leaked after closing fence: %+v", spans[3])
			}
		})
	}
}

func TestMarkdownLinksRemainVisibleAndInert(t *testing.T) {
	input := "[docs](https://example.invalid/path) [local](file:///tmp/no-open) [nested](https://example.invalid/a(b))"
	lines, spans := markdownLines(input, 100)
	checkMarkdownOutput(t, lines, spans, 100)
	if got := strings.Join(lines, ""); got != "docs (https://example.invalid/path) local (file:///tmp/no-open) nested (https://example.invalid/a(b))" {
		t.Fatalf("link destination lost: %q", got)
	}
	for _, row := range spans {
		for _, span := range row {
			if strings.ContainsAny(span.style, "]\a") {
				t.Fatalf("link generated an OSC hyperlink: %q", span.style)
			}
		}
	}
}

func TestMarkdownTablesKeepEveryCell(t *testing.T) {
	input := "| Name | Detail |\n| :--- | ---: |\n| alpha | longvalue |\n| 日本 | **太字** |\n| last | value | extra |"
	for _, width := range []int{80, 12} {
		lines, spans := markdownLines(input, width)
		checkMarkdownOutput(t, lines, spans, width)
		// Ignore layout whitespace so vertically wrapped records can be compared.
		compact := strings.Join(strings.Fields(strings.Join(lines, "")), "")
		for _, cell := range []string{"Name", "Detail", "alpha", "longvalue", "日本", "太字", "last", "value", "extra"} {
			if !strings.Contains(compact, cell) {
				t.Errorf("width %d dropped table cell %q: %q", width, cell, lines)
			}
		}
	}
}

func TestMarkdownJapaneseWidthsAndStyleWrapping(t *testing.T) {
	for _, width := range []int{1, 2, 4, 7} {
		lines, spans := markdownLines("**日本語abcdef** plain\ne\u0301xy", width)
		checkMarkdownOutput(t, lines, spans, width)
		if width > 1 && !strings.Contains(strings.Join(lines, ""), "日本語abcdef plain") {
			t.Errorf("width %d lost or split text: %q", width, lines)
		}
	}
	lines, spans := markdownLines("**abcdefgh** plain", 4)
	if !reflect.DeepEqual(lines, []string{"abcd", "efgh", " pla", "in"}) {
		t.Fatalf("wrapped lines: %q", lines)
	}
	for i := 0; i < 2; i++ {
		if !reflect.DeepEqual(spans[i], []syntaxHighlight{{0, 4, "\x1b[1m"}}) {
			t.Errorf("wrapped bold span %d: %+v", i, spans[i])
		}
		if got := styledSlice(lines[i], 0, 4, spans[i]); got != "\x1b[1m"+lines[i]+"\x1b[0m" {
			t.Errorf("style not reset at wrap: %q", got)
		}
	}
	for i := 2; i < len(lines); i++ {
		if len(spans[i]) != 0 || strings.Contains(styledSlice(lines[i], 0, displayWidth(lines[i]), spans[i]), "\x1b") {
			t.Errorf("style leaked to plain line %d", i)
		}
	}
}

func TestMarkdownRejectsTerminalControls(t *testing.T) {
	attack := "safe\x1b[2J\x1b]52;c;SECRET\a\r\x00\b\u009b\u202e\u2066end"
	for _, input := range []string{attack, "**" + attack + "**", "```\n" + attack + "\n```", "[label](" + attack + ")"} {
		lines, spans := markdownLines(input, 20)
		checkMarkdownOutput(t, lines, spans, 20)
		if !strings.Contains(strings.Join(lines, ""), "safe") || !strings.Contains(strings.Join(lines, ""), "end") {
			t.Errorf("sanitization lost surrounding text: %q", lines)
		}
	}
}

func TestMarkdownPaneOnlyFormatsAssistantAndPreservesHistory(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.visible = true
	s.agent.followTail = false
	s.agent.text = "status **literal**"
	s.agent.history = []agentMessage{
		{id: s.current, Message: explain.Message{Role: "user", Content: "user **literal** `code`"}},
		{id: s.current, Message: explain.Message{Role: "assistant", Content: "**ANSWER** plain\n```\n  x\n\n```"}},
	}
	history := append([]agentMessage(nil), s.agent.history...)
	position, status, input := s.screenPosition, s.agent.text, s.agent.input
	lines, spans := s.paneContent(80, 10)
	checkMarkdownOutput(t, lines, spans, 80)
	if !reflect.DeepEqual(lines, s.paneLines(80, 10)) {
		t.Fatal("plain pane wrapper differs from styled pane content")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "status **literal**") || !strings.Contains(joined, "user **literal** `code`") || !strings.Contains(joined, "ANSWER plain") || strings.Contains(joined, "**ANSWER**") {
		t.Fatalf("wrong roles formatted: %q", lines)
	}
	for i, line := range lines {
		if strings.Contains(line, "literal") && len(spans[i]) != 0 {
			t.Errorf("non-assistant line styled: %q", line)
		}
		if line == "ANSWER plain" && (len(spans[i]) == 0 || spans[i][0].style != "\x1b[1m") {
			t.Errorf("assistant style missing: %+v", spans[i])
		}
	}
	if !reflect.DeepEqual(history, s.agent.history) || position != s.screenPosition || status != s.agent.text || input != s.agent.input || s.agent.cancel != nil {
		t.Fatal("rendering mutated history, navigation, input, or request state")
	}
}

func TestMarkdownFullscreenUsesAssistantSpans(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.visible = true
	s.agent.history = []agentMessage{{id: s.current, Message: explain.Message{Role: "assistant", Content: "**MARKDOWN_REPLY** plain"}}}
	s.agent.followTail = true
	if got := s.render(120, 20, true); !strings.Contains(got, "\x1b[1mMARKDOWN_REPLY\x1b[0m plain") {
		t.Fatalf("fullscreen did not apply Markdown spans: %q", got)
	}
	if got := s.render(120, 20, false); !strings.Contains(got, "MARKDOWN_REPLY plain") || strings.Contains(got, "**MARKDOWN_REPLY**") || strings.Contains(got, "\x1b[1mMARKDOWN_REPLY") {
		t.Fatalf("plain fullscreen did not retain rendered text without emphasis: %q", got)
	}
}

func TestMarkdownReplyFocusAndIndependentSyntaxScroll(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.explanationTop = 2
	agentTestKey(t, s, "e")
	s.agent.input = nil
	s.agent.history = []agentMessage{{id: s.current, Message: explain.Message{Role: "assistant", Content: "# Reply\n**answer**"}}}
	s.agent.followTail = true
	lines, spans := s.paneContent(40, 10)
	checkMarkdownOutput(t, lines, spans, 40)
	top := s.explanationTop
	if top != max(0, len(lines)-10) || len(lines) > 10 && top == 0 {
		t.Fatalf("reply was not focused/padded: top=%d lines=%q", top, lines)
	}
	s.agent.followTail = false
	s.explanationTop = top + 1
	s.paneContent(40, 10)
	if s.explanationTop != top+1 {
		t.Fatal("render reset manually scrolled reply")
	}
	agentTestKey(t, s, "s")
	plain, syntaxSpans := s.paneContent(40, 10)
	if s.explanationTop != 2 || !reflect.DeepEqual(plain, s.explanationLines(40, 10)) || len(syntaxSpans) != 0 {
		t.Fatal("Markdown changed syntax explanation or its scroll")
	}
	agentTestKey(t, s, "e")
	if s.explanationTop != top+1 {
		t.Fatal("chat scroll not restored")
	}
}

func TestAgentChatVisualHierarchy(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.visible = true
	s.agent.text = "操作案内"
	s.agent.history = []agentMessage{
		{id: s.current, Message: explain.Message{Role: "user", Content: "質問の本文"}},
		{id: s.current, Message: explain.Message{Role: "assistant", Content: "回答の本文"}},
	}
	for _, width := range []int{12, 24, 40, 80} {
		lines, spans := s.paneContent(width, 10)
		checkMarkdownOutput(t, lines, spans, width)
		var roles, metadata, rules int
		for i, line := range lines {
			if strings.HasPrefix(line, "── ") {
				rules++
				if displayWidth(line) != width || len(spans[i]) != 1 {
					t.Fatalf("rule not fitted/styled: %q", line)
				}
			}
			if strings.HasPrefix(line, "› ") || strings.HasPrefix(line, "• ") {
				roles++
				if len(spans[i]) != 1 || !strings.Contains(spans[i][0].style, "[1;") {
					t.Fatalf("role not emphasized: %q", line)
				}
			}
			if strings.HasPrefix(line, "対象: ") {
				metadata++
				if len(spans[i]) != 1 || spans[i][0].style != "\x1b[38;5;245m" {
					t.Fatalf("metadata not subdued: %q", line)
				}
			}
			if line == "質問の本文" && len(spans[i]) != 0 {
				t.Fatal("user body got role styling")
			}
		}
		if roles != 2 || metadata != 1 || rules != 0 {
			t.Fatalf("missing hierarchy: %q", lines)
		}
	}
}
