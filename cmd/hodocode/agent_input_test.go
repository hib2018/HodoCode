package main

import (
	"bytes"
	"context"
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

func TestAgentInputHistoryAndExplicitSend(t *testing.T) {
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
		agentTestKey(t, s, "i")
		typeAgentInput(t, s, instruction)
		before := calls.Load()
		typeAgentInput(t, s, "\r")
		if s.agent.input != nil || s.agent.pending == nil || calls.Load() != before {
			t.Fatal("commit did not preview without sending")
		}
		if !strings.Contains(s.agent.texts[id], instruction) {
			t.Fatal("preview missing instruction")
		}
		agentTestKey(t, s, "y")
		if result := agentTestResult(t, s); result.err != nil {
			t.Fatal(result.err)
		}
	}
	want := "Explain the flow\n\n日本語で説明してq\n\n呼び出し元も説明して"
	if s.agent.purpose(id) != want || len(prompts) != 4 {
		t.Fatalf("purpose=%q prompts=%d", s.agent.purpose(id), len(prompts))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, "日本語で説明してq") || (i >= 2 && !strings.Contains(prompt, "呼び出し元も説明して")) || strings.Contains(prompt, "PREVIOUS_MODEL_ANSWER") {
			t.Fatalf("unexpected prompt %d: %s", i, prompt)
		}
	}
	press(t, s, "\r")
	if s.agent.purpose(s.current) != "Explain the flow" {
		t.Fatal("instructions leaked to another function")
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "別の関数\r")
	if !strings.Contains(s.agent.purpose(s.current), "別の関数") || s.agent.purpose(id) != want {
		t.Fatal("per-function history was not retained")
	}
}

func TestAgentInputEditingAndCancel(t *testing.T) {
	s := agentTestScreen(t, nil)
	if s.agentInputByte(context.Background(), 'q') || s.agentInputIdle() || s.renderAgentInput(80, 24) != "" {
		t.Fatal("closed input handled a byte or rendered")
	}
	agentTestKey(t, s, "e")
	pending := s.agent.pending
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
	if s.agent.input.text != "" || s.agent.input.status == "" || len(s.agent.instructions) != 0 {
		t.Fatal("clear/empty commit mutated history or closed input")
	}
	typeAgentInput(t, s, "取消\x1b")
	if !s.agentInputIdle() || s.agent.input != nil || s.agent.pending != pending || len(s.agent.instructions) != 0 {
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
	id := s.current
	agentTestKey(t, s, "e")
	pending := s.agent.pending
	agentTestKey(t, s, "i")
	room := maxAgentPurpose - len(s.agent.options.purpose) - 2
	typeAgentInput(t, s, strings.Repeat("x", room+1)+"\r")
	if s.agent.input == nil || s.agent.input.status == "" || len(s.agent.instructions) != 0 || s.agent.pending != pending {
		t.Fatal("oversize purpose mutated history or preview")
	}
	typeAgentInput(t, s, "\x7f\r")
	if s.agent.input != nil || len(s.agent.purpose(id)) != maxAgentPurpose || s.agent.pending == nil {
		t.Fatal("exactly 8KiB purpose was not accepted")
	}
	pending = s.agent.pending
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "日\r")
	if s.agent.input == nil || len(s.agent.instructions[id]) != 1 || s.agent.pending != pending {
		t.Fatal("cumulative size validation mutated existing state")
	}
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
		s := agentTestScreen(t, nil)
		input := []byte("i日本語q")
		if cancel {
			input = append(input, '\x1b', 0, 'q')
		} else {
			input = append(input, '\r', 'n', 'q')
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
		for _, want := range []string{"追加指示（Enter:確認 / Esc:取消）", "日本語q_"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("screen loop missing %q", want)
			}
		}
		if !cancel && !strings.Contains(out.String(), "まだ送信していません") {
			t.Fatal("Enter skipped preview")
		}
		if cancel && len(s.agent.instructions) != 0 {
			t.Fatal("cancel retained draft")
		}
	}
}

func TestRenderAgentInput(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "日本語q")
	output := s.renderAgentInput(100, 24)
	if !strings.HasPrefix(output, "\x1b[9;13H") || !strings.Contains(output, "日本語q_") || !strings.Contains(output, "Esc:取消") {
		t.Fatalf("missing centered input: %q", output)
	}
	// Rendering at small sizes must remain safe and nonnegative.
	for width := 0; width < 80; width++ {
		for height := 0; height < 12; height++ {
			if strings.Contains(s.renderAgentInput(width, height), "\x1b[-") {
				t.Fatal("negative terminal position")
			}
		}
	}
}
