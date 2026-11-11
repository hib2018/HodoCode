package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/explain"
)

func TestAgentConversationPreservesInitialAnswerAndFailures(t *testing.T) {
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
	id := s.current
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "y")
	agentTestResult(t, s)
	// Merely returning to the chat must not regenerate the explanation.
	agentTestKey(t, s, "e")
	if s.agent.pending != nil || calls != 2 {
		t.Fatal("existing chat was regenerated")
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "この説明の理由は？\r")
	agentTestKey(t, s, "n")
	if len(s.agent.history[id]) != 1 || calls != 2 || !strings.Contains(strings.Join(s.paneLines(100, 10), "\n"), "INITIAL_ANSWER") {
		t.Fatal("cancel lost the initial answer or sent the question")
	}
	agentTestKey(t, s, "e")
	fail = true
	agentTestKey(t, s, "y")
	agentTestResult(t, s)
	pane := strings.Join(s.paneLines(100, 10), "\n")
	if len(s.agent.history[id]) != 2 || !strings.Contains(pane, "offline") || !strings.Contains(pane, "INITIAL_ANSWER") || !strings.Contains(pane, "この説明の理由は？") {
		t.Fatal("failure destroyed conversation or hid its error")
	}
	// Retry without duplicating the failed question in either the prompt or chat.
	fail = false
	calls = 2
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "y")
	agentTestResult(t, s)
	if len(s.agent.history[id]) != 3 {
		t.Fatalf("retry duplicated question: %+v", s.agent.history[id])
	}
	for _, prompt := range prompts[len(prompts)-2:] {
		if strings.Count(prompt, "この説明の理由は？") != 1 || !strings.Contains(prompt, "INITIAL_ANSWER") {
			t.Fatalf("retry context: %s", prompt)
		}
	}
	notes := s.paneLines(30, 8)
	if notes[s.explanationTop] != "エージェント" || notes[s.explanationTop+1] != "FOLLOWUP_ANSWER" {
		t.Fatal("new reply did not start at the top")
	}
	top := s.explanationTop
	press(t, s, "[")
	if s.explanationTop != top-1 {
		t.Fatal("cannot scroll back through conversation")
	}
	// An explicit rewrite is still a new answer, not a destructive replacement.
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "説明全体を改訂して\r")
	agentTestKey(t, s, "y")
	agentTestResult(t, s)
	if len(s.agent.history[id]) != 5 || s.agent.history[id][0].Content != "INITIAL_ANSWER" {
		t.Fatal("rewrite replaced conversation")
	}
}

func TestAgentConversationOverflowDoesNotTransmitOrTruncate(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(context.Context, string) (string, error) { calls++; return "", nil })
	id := s.current
	original := strings.Repeat("過去の回答", 20000)
	s.agent.history[id] = []explain.Message{{Role: "assistant", Content: original}}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "続きは？\r")
	if s.agent.pending != nil || s.agentKey(context.Background(), "y") || calls != 0 {
		t.Fatal("oversized history was sent")
	}
	if s.agent.history[id][0].Content != original || !strings.Contains(s.agent.texts[id], "prompt exceeds") || !strings.Contains(s.agent.texts[id], "送信していません") {
		t.Fatal("history was truncated or size failure was not shown")
	}
}
