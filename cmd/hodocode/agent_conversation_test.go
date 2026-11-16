package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/explain"
)

func TestAgentConversationPreservesAnswersAndFailures(t *testing.T) {
	calls := 0
	fail := false
	var prompts []string
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		calls++
		prompts = append(prompts, prompt)
		if fail {
			return "", errors.New("offline")
		}
		if calls%2 == 1 {
			return `["example.com/screen.First"]`, nil
		}
		if calls == 2 {
			return "INITIAL_ANSWER", nil
		}
		return "FOLLOWUP_ANSWER", nil
	})
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	// Opening input must not send or discard the previous answer.
	agentTestKey(t, s, "i")
	if s.agent.cancel != nil || calls != 2 || s.agent.input == nil {
		t.Fatal("i sent a request")
	}
	typeAgentInput(t, s, "この説明の理由は？\x1b")
	if !s.agentInputIdle() || len(s.agent.history) != 2 || calls != 2 || !strings.Contains(strings.Join(s.paneLines(100, 10), "\n"), "INITIAL_ANSWER") {
		t.Fatal("cancel lost the initial answer or sent the question")
	}
	fail = true
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "この説明の理由は？\r")
	agentTestResult(t, s)
	pane := strings.Join(s.paneLines(100, 10), "\n")
	if len(s.agent.history) != 3 || !strings.Contains(pane, "offline") || !strings.Contains(pane, "INITIAL_ANSWER") || !strings.Contains(pane, "この説明の理由は？") {
		t.Fatal("failure destroyed conversation or hid its error")
	}
	// E always appends a new explanation request, including after a failure.
	fail = false
	calls = 2
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	if len(s.agent.history) != 5 {
		t.Fatalf("history: %+v", s.agent.history)
	}
	for _, prompt := range prompts[len(prompts)-2:] {
		if strings.Count(prompt, "この説明の理由は？") != 1 || !strings.Contains(prompt, "INITIAL_ANSWER") {
			t.Fatalf("missing failure context: %s", prompt)
		}
	}
	notes := s.paneLines(100, 8)
	if notes[s.explanationTop] != "エージェント" || notes[s.explanationTop+2] != "FOLLOWUP_ANSWER" {
		t.Fatal("new reply did not start at the top with its target")
	}
	top := s.explanationTop
	press(t, s, "[")
	if s.explanationTop != top-1 {
		t.Fatal("cannot scroll back through conversation")
	}
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	if len(s.agent.history) != 7 || s.agent.history[1].Content != "INITIAL_ANSWER" {
		t.Fatal("regeneration replaced conversation")
	}
}

func TestAgentConversationOverflowDoesNotTransmitOrTruncate(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(context.Context, string) (string, error) { calls++; return "", nil })
	original := strings.Repeat("過去の回答", 20000)
	s.agent.history = []agentMessage{{id: s.current, Message: explain.Message{Role: "assistant", Content: original}}}
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "続きは？\r")
	if s.agent.cancel != nil || calls != 0 {
		t.Fatal("oversized history was sent")
	}
	if s.agent.history[0].Content != original || !strings.Contains(s.agent.text, "prompt exceeds") || !strings.Contains(s.agent.text, "送信していません") {
		t.Fatal("history was truncated or size failure was not shown")
	}
}

func TestAgentConversationSyntaxChatRestoresIndependentScroll(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		calls++
		if calls == 1 {
			return `["example.com/screen.First"]`, nil
		}
		return strings.Repeat("CHAT_HISTORY\n", 40), nil
	})
	s.explanationTop = 2
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	s.paneLines(40, 10)
	s.explanationTop = 12
	history := append([]agentMessage(nil), s.agent.history...)
	for i := 0; i < 2; i++ {
		agentTestKey(t, s, "s")
		if s.agent.visible || s.explanationTop != 2 || s.agent.scroll != 12 {
			t.Fatal("s lost independent syntax/chat scroll")
		}
		agentTestKey(t, s, "s")
		agentTestKey(t, s, "e")
		s.paneLines(40, 10)
		if !s.agent.visible || s.explanationTop != 12 || calls != 2 || s.agent.cancel != nil || !reflect.DeepEqual(s.agent.history, history) {
			t.Fatal("s -> e regenerated chat or lost its history/scroll")
		}
	}
	press(t, s, "\r")
	if s.explanationTop != 12 {
		t.Fatal("navigation reset shared conversation scroll")
	}
}

func TestAgentInputKeyOnlyInAgentView(t *testing.T) {
	s := agentTestScreen(t, nil)
	if s.agentKey(context.Background(), "i") || s.agent.input != nil || s.agent.visible {
		t.Fatal("i opened input in syntax view")
	}
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "i")
	if s.agent.input == nil || s.agent.input.id != s.current || s.agent.cancel != nil {
		t.Fatal("i did not open input for the displayed function")
	}
}
