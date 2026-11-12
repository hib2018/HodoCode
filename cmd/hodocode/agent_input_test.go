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
			return `["example.com/screen.First"]`, nil
		}
		return "PREVIOUS_MODEL_ANSWER", nil
	})
	id := s.current
	for _, instruction := range []string{"日本語で説明してq", "呼び出し元も説明して"} {
		agentTestKey(t, s, "s") // Follow-up from syntax view must still send the prior conversation.
		agentTestKey(t, s, "i")
		typeAgentInput(t, s, instruction)
		typeAgentInput(t, s, "\r")
		if s.agent.input != nil || s.agent.cancel == nil || s.agent.questions[id] != instruction {
			t.Fatal("Enter did not close input and start generation")
		}
		if result := agentTestResult(t, s); result.err != nil {
			t.Fatal(result.err)
		}
	}
	if len(s.agent.history[id]) != 4 || len(prompts) != 4 {
		t.Fatalf("history=%+v prompts=%d", s.agent.history[id], len(prompts))
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
	if len(s.agent.history[s.current]) != 0 {
		t.Fatal("history leaked to another function")
	}
	s.agent.options.excludes = []string{"main.go"}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "別の関数\r")
	if s.agent.questions[s.current] != "別の関数" || len(s.agent.history[id]) != 4 {
		t.Fatal("per-function history was not retained")
	}
	press(t, s, "b")
	if strings.Count(strings.Join(s.paneLines(100, 10), "\n"), "PREVIOUS_MODEL_ANSWER") != 2 {
		t.Fatal("returning to function lost conversation")
	}
}

func TestAgentInputEditingAndCancel(t *testing.T) {
	s := agentTestScreen(t, nil)
	if s.agentInputByte(context.Background(), 'q') || s.agentInputIdle() || s.renderAgentInput(80, 24) != "" {
		t.Fatal("closed input handled a byte or rendered")
	}
	s.agent.texts[s.current] = "EXISTING_CHAT"
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
	if !s.agentInputIdle() || s.agent.input != nil || s.agent.texts[s.current] != "EXISTING_CHAT" || len(s.agent.history) != 0 {
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
	s := agentTestScreen(t, nil)
	s.agent.options.excludes = []string{"main.go"}
	id := s.current
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, strings.Repeat("x", maxAgentPurpose+1))
	if len(s.agent.input.text) != maxAgentPurpose || s.agent.input.status == "" || len(s.agent.history) != 0 {
		t.Fatal("draft size bound failed")
	}
	typeAgentInput(t, s, "\r")
	if s.agent.input != nil || len(s.agent.questions[id]) != maxAgentPurpose || s.agent.cancel != nil {
		t.Fatal("exactly 8KiB question was not accepted independently of purpose")
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "日\r")
	if s.agent.input != nil || s.agent.questions[id] != "日" || len(s.agent.history) != 0 {
		t.Fatal("new question was accumulated instead of replacing the blocked question")
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "\x15"+strings.Repeat("日", maxAgentPurpose/3+1))
	if len(s.agent.input.text) > maxAgentPurpose || !utf8.ValidString(s.agent.input.text) || s.agent.input.status == "" {
		t.Fatal("draft byte limit corrupted UTF-8 or was exceeded")
	}
}

func TestAgentInputGenerationBlocked(t *testing.T) {
	s := agentTestScreen(t, nil)
	_, cancel := context.WithCancel(context.Background())
	s.agent.cancel = cancel
	agentTestKey(t, s, "i")
	if s.agent.input != nil || !s.agent.visible || !strings.Contains(s.agent.texts[s.current], "生成中は追加指示を入力できません") {
		t.Fatal("generation did not block input with visible status")
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
		input := []byte("i日本語q")
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
		if len(input) != 0 || s.agent.input != nil || s.agent.cancel != nil || s.agent.pending != nil {
			t.Fatal("modal did not consume q as text, close, and quit without sending")
		}
		for _, want := range []string{"追加指示", "› 日本語q▏", "Enterで送信前確認へ（まだ送信しません）"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("screen loop missing %q", want)
			}
		}
		if !cancel && !strings.Contains(out.String(), "まだ送信していません") {
			t.Fatal("Enter skipped preview")
		}
		if cancel && len(s.agent.history) != 0 {
			t.Fatal("cancel retained draft")
		}
	}
}

func TestRenderAgentInput(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "i")
	empty := s.renderAgentInput(100, 24)
	if !strings.Contains(empty, "› ▏指示を入力してください…") {
		t.Fatal("missing entry placeholder")
	}
	typeAgentInput(t, s, "日本語q")
	output := s.renderAgentInput(100, 24)
	for _, want := range []string{"\x1b[7;13H╭", "追加指示", "対象: screen.First", "› 日本語q▏", "Enterで送信前確認へ（まだ送信しません）", "Enter:確認へ  Esc:取消  Ctrl+U:消去", "╰"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %q", want, output)
		}
	}
	if strings.Contains(output, s.agent.input.id) || strings.Contains(output, "_") {
		t.Fatal("full target ID or fake underscore caret rendered")
	}
	positions := regexp.MustCompile(`\x1b\[\d+;\d+H`)
	if strings.Join(positions.FindAllString(empty, -1), "") != strings.Join(positions.FindAllString(output, -1), "") {
		t.Fatal("short text changed modal position or height")
	}
	s.agent.input.status = "指示を入力してください。"
	withError := s.renderAgentInput(100, 24)
	if !strings.Contains(withError, "! 指示を入力してください。") || strings.Join(positions.FindAllString(withError, -1), "") != strings.Join(positions.FindAllString(output, -1), "") {
		t.Fatal("inline error missing or shifted the modal")
	}
	rows := positions.Split(output, -1)[1:]
	blank := "│ " + strings.Repeat(" ", 72) + " │"
	if len(rows) != 11 || !strings.Contains(rows[4], "› 日本語q▏") || rows[5] != blank || rows[6] != blank {
		t.Fatal("entry area must reserve at least three padded rows")
	}
}

func TestRenderAgentInputLayoutBounds(t *testing.T) {
	s := agentTestScreen(t, nil)
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
						cells := displayWidth(line)
						if row < 1 || row > height || col < 1 || col+cells-1 > width || cells != min(76, width) || !utf8.ValidString(line) || strings.ContainsAny(line, "\x1b\r\n\t\u202e") {
							t.Fatalf("layout %dx%d row=%d col=%d cells=%d: %q", width, height, row, col, cells, line)
						}
					}
					if color && output != "" && strings.Count(output, ";48;5;236m") != len(matches) {
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
	agentTestKey(t, s, "i")
	for _, output := range []string{s.renderAgentInput(100, 24), s.renderAgentInput(100, 24, false)} {
		if regexp.MustCompile(`\x1b\[[0-9;]*m`).MatchString(output) {
			t.Fatal("default/NO_COLOR rendering contains color escapes")
		}
		if !strings.Contains(output, "╭") || !strings.Contains(output, "▏") {
			t.Fatal("color-disabled modal lost its border or caret")
		}
	}
}
