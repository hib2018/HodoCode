package main

import (
	"bytes"
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func typeAgentInput(t *testing.T, s *screen, text string) {
	t.Helper()
	for _, b := range []byte(text) {
		if !s.agentInputByte(context.Background(), b) {
			t.Fatalf("input byte %x was not consumed", b)
		}
	}
}

func TestAgentInputHistoryAndEnterSend(t *testing.T) {
	var calls atomic.Int32
	var prompts []string
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		if calls.Add(1)%2 == 1 {
			return `["example.com/screen.First","example.com/screen.Second"]`, nil
		}
		return "PREVIOUS_MODEL_ANSWER", nil
	})
	agentTestKey(t, s, "e")
	id := s.current
	for _, instruction := range []string{"日本語で説明してq", "呼び出し元も説明して"} {
		agentTestKey(t, s, "s") // Follow-up from syntax view must still send the prior conversation.
		agentTestKey(t, s, "e")
		agentTestKey(t, s, "i")
		if s.agent.input == nil || s.agent.input.id != id || s.agent.cancel != nil {
			t.Fatal("i did not open input without starting generation")
		}
		typeAgentInput(t, s, instruction)
		typeAgentInput(t, s, "\r")
		if s.agent.input != nil || s.agent.cancel == nil || len(s.agent.history) == 0 {
			t.Fatal("Enter did not close input and start generation")
		}
		message := s.agent.history[len(s.agent.history)-1]
		if message.id != id || message.Role != "user" || message.Content != instruction {
			t.Fatalf("submitted message=%+v", message)
		}
		if result := agentTestResult(t, s); result.err != nil {
			t.Fatal(result.err)
		}
	}
	if len(s.agent.history) != 4 || len(prompts) != 4 {
		t.Fatalf("history=%+v prompts=%d", s.agent.history, len(prompts))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, `"purpose":"Explain the flow"`) || !strings.Contains(prompt, "日本語で説明してq") || (i >= 2 && (!strings.Contains(prompt, "呼び出し元も説明して") || !strings.Contains(prompt, "PREVIOUS_MODEL_ANSWER"))) {
			t.Fatalf("unexpected prompt %d: %s", i, prompt)
		}
		if i%2 == 1 && (!strings.Contains(prompt, "現在の question にだけ") || !strings.Contains(prompt, "明示的に求められない限り")) {
			t.Fatal("follow-up requested full regeneration")
		}
	}
	pane := strings.Join(s.paneLines(100, 10), "\n")
	if strings.Count(pane, "PREVIOUS_MODEL_ANSWER") != 2 || !strings.Contains(pane, "利用者") || !strings.Contains(pane, "エージェント") {
		t.Fatal("conversation did not append both replies with speakers")
	}
	press(t, s, "\r")
	other := s.current
	if other == id || len(s.agent.history) != 4 || strings.Count(strings.Join(s.paneLines(100, 10), "\n"), "PREVIOUS_MODEL_ANSWER") != 2 {
		t.Fatal("navigation did not preserve shared conversation")
	}
	agentTestKey(t, s, "i")
	if s.agent.input.id != other {
		t.Fatal("popup did not target the newly selected function")
	}
	typeAgentInput(t, s, "別の関数\r")
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
	if len(s.agent.history) != 6 || len(prompts) != 6 {
		t.Fatalf("shared history=%+v prompts=%d", s.agent.history, len(prompts))
	}
	for i, message := range s.agent.history {
		wantID, wantRole := id, "user"
		if i >= 4 {
			wantID = other
		}
		if i%2 == 1 {
			wantRole = "assistant"
		}
		if message.id != wantID || message.Role != wantRole {
			t.Fatalf("history[%d]=%+v, want target %s and role %s", i, message, wantID, wantRole)
		}
	}
	for i, prompt := range prompts {
		target := id
		if i >= 4 {
			target = other
			for _, want := range []string{"日本語で説明してq", "呼び出し元も説明して", "PREVIOUS_MODEL_ANSWER", "別の関数"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("cross-target prompt %d missing %q: %s", i, want, prompt)
				}
			}
		}
		if !strings.Contains(prompt, `"target":"`+target+`"`) || (i >= 2 && !strings.Contains(prompt, "対象の関数ID: "+id)) {
			t.Fatalf("prompt %d missing current or historical target ID: %s", i, prompt)
		}
	}
	press(t, s, "b")
	if s.current != id || strings.Count(strings.Join(s.paneLines(100, 10), "\n"), "PREVIOUS_MODEL_ANSWER") != 3 {
		t.Fatal("returning to function lost shared conversation")
	}
}

func TestAgentInputEditingAndCancel(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	if s.agentInputByte(context.Background(), 'q') || s.agentInputIdle() || s.renderAgentInput(80, 24) != "" {
		t.Fatal("closed input handled a byte or rendered")
	}
	s.agent.text = "EXISTING_CHAT"
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "日q本\x7f")
	if s.agent.input.text != "日q" || !utf8.ValidString(s.agent.input.text) {
		t.Fatalf("UTF-8 backspace: %q", s.agent.input.text)
	}
	typeAgentInput(t, s, "\x08")
	if s.agent.input.text != "日" {
		t.Fatal("ASCII backspace failed")
	}
	typeAgentInput(t, s, "\x15\r")
	if s.agent.input.text != "" || s.agent.input.status == "" || len(s.agent.history) != 0 {
		t.Fatal("clear/empty commit mutated history or closed input")
	}
	typeAgentInput(t, s, "取消\x1b")
	if !s.agentInputIdle() || s.agent.input != nil || s.agent.text != "EXISTING_CHAT" || len(s.agent.history) != 0 {
		t.Fatal("Escape did not cancel without mutation")
	}
	for _, b := range []byte{3, 4} {
		agentTestKey(t, s, "i")
		if s.agentInputByte(context.Background(), b) || s.agent.input != nil {
			t.Fatal("Ctrl+C/D did not close and pass through")
		}
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "\x1b[A\x1b[5~q")
	if s.agent.input.text != "q" || s.agentInputIdle() {
		t.Fatal("terminal escape sequences entered the draft")
	}
}

func TestAgentInputSizeBounds(t *testing.T) {
	var calls atomic.Int32
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		if calls.Add(1)%2 == 1 {
			return `["example.com/screen.First"]`, nil
		}
		return "ANSWER", nil
	})
	agentTestKey(t, s, "e")
	id := s.current
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, strings.Repeat("x", maxAgentPurpose+1))
	if len(s.agent.input.text) != maxAgentPurpose || s.agent.input.status == "" || len(s.agent.history) != 0 {
		t.Fatal("draft size bound failed")
	}
	typeAgentInput(t, s, "\r")
	if s.agent.input != nil || len(s.agent.history) != 1 || s.agent.history[0].id != id || len(s.agent.history[0].Content) != maxAgentPurpose || s.agent.cancel == nil {
		t.Fatal("exactly 8KiB question was not accepted independently of purpose")
	}
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "日\r")
	if s.agent.input != nil || len(s.agent.history) != 3 || s.agent.history[2].Content != "日" {
		t.Fatal("new question was accumulated instead of sent independently")
	}
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "\x15"+strings.Repeat("日", maxAgentPurpose/3+1))
	if len(s.agent.input.text) > maxAgentPurpose || !utf8.ValidString(s.agent.input.text) || s.agent.input.status == "" {
		t.Fatal("draft byte limit corrupted UTF-8 or was exceeded")
	}
}

func TestAgentInputGenerationBlocked(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	_, cancel := context.WithCancel(context.Background())
	s.agent.cancel = cancel
	agentTestKey(t, s, "i")
	if s.agent.input != nil || !s.agent.visible || !strings.Contains(s.agent.text, "生成中は追加指示を入力できません。完了してから i を押してください。") {
		t.Fatal("generation did not block input with visible status")
	}
}

func TestAgentViewOnlyScreenLoop(t *testing.T) {
	for _, generating := range []bool{false, true} {
		name := "idle"
		if generating {
			name = "generating"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			s := agentTestScreen(t, func(context.Context, string) (string, error) {
				calls.Add(1)
				return "", nil
			})
			s.agent.results <- agentResult{id: s.current, text: "EXISTING_ANSWER"}
			s.receiveAgentResult()
			s.agent.focusReply = false
			status := strings.Repeat("EXISTING_STATUS\n", 30)
			if generating {
				_, cancel := context.WithCancel(context.Background())
				s.agent.cancel = cancel
				status = "エージェントが解説を生成中です。\n" + status
			}
			s.agent.text = status
			s.agent.scroll = 4
			input := []byte("ieesssieeeq")
			var out bytes.Buffer
			read := func() (byte, bool, error) {
				if s.agent.input != nil || s.agent.text != status || len(s.agent.history) != 1 || s.agent.history[0].Content != "EXISTING_ANSWER" || calls.Load() != 0 {
					t.Fatal("view keys opened input, transmitted, or changed the conversation/status")
				}
				if s.agent.visible && s.explanationTop != 4 {
					t.Fatal("view keys lost agent scrolling")
				}
				if len(input) == 0 {
					t.Fatal("view keys consumed quit as input")
				}
				b := input[0]
				input = input[1:]
				return b, true, nil
			}
			if err := screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 120, 24, nil }, false); err != nil {
				t.Fatal(err)
			}
			if len(input) != 0 || !s.agent.visible || calls.Load() != 0 || strings.Contains(out.String(), "Enterでfxへ送信します") {
				t.Fatal("view-only screen loop opened a popup or sent a request")
			}
		})
	}
}

func TestAgentInputScreenLoop(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		var calls atomic.Int32
		s := agentTestScreen(t, func(context.Context, string) (string, error) {
			calls.Add(1)
			return "", nil
		})
		s.agent.options.excludes = []string{"main.go"}
		input := []byte("ei日本語q")
		if cancel {
			input = append(input, '\x1b', 0, 'q')
		} else {
			input = append(input, '\r', 'q')
		}
		var out bytes.Buffer
		read := func() (byte, bool, error) {
			if len(input) == 0 {
				t.Fatal("input exhausted before quit")
			}
			b := input[0]
			input = input[1:]
			return b, b != 0, nil
		}
		if err := screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 120, 24, nil }, false); err != nil {
			t.Fatal(err)
		}
		if len(input) != 0 || s.agent.input != nil || s.agent.cancel != nil || calls.Load() != 0 {
			t.Fatal("modal did not consume q as text, close, and quit without sending")
		}
		for _, want := range []string{"追加指示", "› 日本語q", "Enterでfxへ送信します"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("screen loop missing %q", want)
			}
		}
		if !cancel && (len(s.agent.history) != 0 || !strings.Contains(out.String(), "target is excluded or blocked")) {
			t.Fatal("Enter did not reject the blocked target without retaining a conversation")
		}
		if strings.Contains(out.String(), "▏") {
			t.Fatal("input rendered a fake cursor")
		}
		if cancel && len(s.agent.history) != 0 {
			t.Fatal("cancel retained draft")
		}
	}
}

func TestRenderAgentInput(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	empty := s.renderAgentInput(100, 24)
	if !strings.Contains(empty, "› 指示を入力してください…") {
		t.Fatal("missing entry placeholder")
	}
	typeAgentInput(t, s, "日本語q")
	output := s.renderAgentInput(100, 24)
	for _, want := range []string{"\x1b[7;13H╭", "追加指示", "対象: screen.First", "› 日本語q", "Enterでfxへ送信します", "Enter:送信  Esc:取消  Ctrl+U:消去", "╰"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %q", want, output)
		}
	}
	if strings.Contains(output, s.agent.input.id) || strings.Contains(output, "_") || strings.Contains(output, "▏") {
		t.Fatal("full target ID or fake caret rendered")
	}
	positions := regexp.MustCompile(`\x1b\[\d+;\d+H`)
	boxPositions := func(text string) string {
		matches := positions.FindAllString(text, -1)
		return strings.Join(matches[:len(matches)-1], "")
	}
	if boxPositions(empty) != boxPositions(output) {
		t.Fatal("short text changed modal position or height")
	}
	s.agent.input.status = "指示を入力してください。"
	withError := s.renderAgentInput(100, 24)
	if !strings.Contains(withError, "! 指示を入力してください。") || boxPositions(withError) != boxPositions(output) {
		t.Fatal("inline error missing or shifted the modal")
	}
	rows := positions.Split(output, -1)[1:]
	rows = rows[:len(rows)-1] // Final movement positions the real cursor, not a box row.
	blank := "│ " + strings.Repeat(" ", 72) + " │"
	if len(rows) != 11 || !strings.Contains(rows[4], "› 日本語q") || rows[5] != blank || rows[6] != blank {
		t.Fatal("entry area must reserve at least three padded rows")
	}
}

func TestRenderAgentInputLayoutBounds(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	s.agent.input.id = "example.com/" + strings.Repeat("非常に長いパッケージ/", 20) + "日本語.対象関数\x1b[2J\n\t\u202e"
	s.agent.input.status = "エラー\x1b[2J\r\u202e" + strings.Repeat("長い説明", 40)
	cursor := regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, text := range []string{"", "日本語", strings.Repeat("日本語の長い指示", 120) + "\x1b[2J\r\t\u202e"} {
		s.agent.input.text = text
		for _, color := range []bool{false, true} {
			for width := 0; width <= 100; width++ {
				for height := 0; height <= 26; height++ {
					output := s.renderAgentInput(width, height, color)
					plain := sgr.ReplaceAllString(output, "")
					matches := cursor.FindAllStringSubmatchIndex(plain, -1)
					if output != "" && len(matches) == 0 {
						t.Fatalf("missing cursor positions: %q", output)
					}
					for i, match := range matches {
						row, _ := strconv.Atoi(plain[match[2]:match[3]])
						col, _ := strconv.Atoi(plain[match[4]:match[5]])
						end := len(plain)
						if i+1 < len(matches) {
							end = matches[i+1][0]
						}
						line := plain[match[1]:end]
						if i == len(matches)-1 {
							if line != "\x1b[?25h" || row < 1 || row > height || col < 1 || col > width {
								t.Fatalf("invalid final cursor at %dx%d: row=%d col=%d suffix=%q", width, height, row, col, line)
							}
							continue
						}
						cells := displayWidth(line)
						if row < 1 || row > height || col < 1 || col+cells-1 > width || cells != min(76, width) || !utf8.ValidString(line) || strings.ContainsAny(line, "\x1b\r\n\t\u202e") {
							t.Fatalf("layout %dx%d row=%d col=%d cells=%d: %q", width, height, row, col, cells, line)
						}
					}
					if color && output != "" && strings.Count(output, ";48;5;236m") != len(matches)-1 {
						t.Fatal("panel background does not cover every padded row")
					}
					if !color && sgr.MatchString(output) {
						t.Fatal("color-disabled render contains SGR")
					}
				}
			}
		}
	}
}

func TestRenderAgentInputNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	for _, output := range []string{s.renderAgentInput(100, 24), s.renderAgentInput(100, 24, false)} {
		if regexp.MustCompile(`\x1b\[[0-9;]*m`).MatchString(output) {
			t.Fatal("default/NO_COLOR rendering contains color escapes")
		}
		if !strings.Contains(output, "╭") || !strings.HasSuffix(output, "\x1b[?25h") || strings.Contains(output, "▏") {
			t.Fatal("color-disabled modal lost its border or real cursor")
		}
	}
}

func TestRenderAgentInputCursorPosition(t *testing.T) {
	cursor := regexp.MustCompile(`\x1b\[(\d+);(\d+)H\x1b\[\?25h$`)
	for _, tc := range []struct {
		name, text              string
		width, height, row, col int
	}{
		{"placeholder", "", 100, 24, 11, 17},
		{"Japanese", "日本語q", 100, 24, 11, 24},
		{"combining", "e\u0301日", 100, 24, 11, 20},
		{"wrapped Japanese", strings.Repeat("日", 40), 100, 24, 12, 25},
		{"short terminal", "日本語q", 100, 8, 4, 24},
		{"minimal height", "日本語q", 100, 4, 3, 24},
		{"narrow wrapped", "日本語q", 12, 24, 12, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := agentTestScreen(t, nil)
			agentTestKey(t, s, "e")
			agentTestKey(t, s, "i")
			typeAgentInput(t, s, tc.text)
			for _, color := range []bool{false, true} {
				output := s.renderAgentInput(tc.width, tc.height, color)
				match := cursor.FindStringSubmatch(output)
				if match == nil {
					t.Fatalf("missing final cursor position/show: %q", output)
				}
				row, _ := strconv.Atoi(match[1])
				col, _ := strconv.Atoi(match[2])
				if row != tc.row || col != tc.col || strings.Contains(output, "▏") {
					t.Fatalf("cursor = %d,%d, want %d,%d (no fake caret)", row, col, tc.row, tc.col)
				}
			}
		})
	}
}
