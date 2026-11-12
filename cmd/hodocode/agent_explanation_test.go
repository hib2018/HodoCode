package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hib2018/HodoCode/internal/explain"
)

func agentTestScreen(t *testing.T, runner explain.Runner) *screen {
	t.Helper()
	p := screenProject(t, map[string]string{"main.go": `package sample
func First() {
	println("LITERAL_SECRET") // COMMENT_SECRET
	Second()
}
func Second() {}
`})
	s, err := newScreen(p, "First", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.agent = newAgentPane(agentOptions{runner: runner, purpose: "Explain the flow", timeout: 5 * time.Second})
	t.Cleanup(func() {
		if s.agent.cancel != nil {
			s.agent.cancel()
		}
	})
	return s
}

func agentTestKey(t *testing.T, s *screen, key string) {
	t.Helper()
	if !s.agentKey(context.Background(), key) {
		t.Fatalf("agent key %q was not handled", key)
	}
}

func agentTestResult(t *testing.T, s *screen) agentResult {
	t.Helper()
	select {
	case result := <-s.agent.results:
		// Exercise the screen-loop receiver after synchronizing with the worker.
		s.agent.results <- result
		if !s.receiveAgentResult() {
			t.Fatal("queued result was not received")
		}
		if s.agent.cancel != nil || s.receiveAgentResult() {
			t.Fatal("result did not clear the job or was received twice")
		}
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent result")
		return agentResult{}
	}
}

func TestAgentStartsImmediatelyAndIgnoresConfirmationKeys(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	for _, key := range []string{"y", "n"} {
		if s.agentKey(context.Background(), key) {
			t.Fatalf("agent handled obsolete confirmation key %q", key)
		}
	}
	s.explanationTop = 9
	agentTestKey(t, s, "e")
	if !s.agent.visible || s.agent.cancel == nil || s.explanationTop != 0 || s.syntaxTop != 9 {
		t.Fatal("e did not immediately start generation and preserve syntax scrolling")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("e did not call the runner")
	}
	for _, key := range []string{"y", "n"} {
		if s.agentKey(context.Background(), key) {
			t.Fatalf("agent handled obsolete confirmation key %q during generation", key)
		}
	}
	agentTestKey(t, s, "e")
	if calls.Load() != 1 {
		t.Fatal("e started a duplicate job")
	}
	agentTestKey(t, s, "s")
	if s.agent.visible || s.explanationTop != 9 || !reflect.DeepEqual(s.paneLines(40, 10), s.explanationLines(40, 10)) {
		t.Fatal("s did not restore syntax explanations and scrolling")
	}
	s.agent.cancel()
	agentTestResult(t, s)
}

func TestAgentImmediateTwoCallsAndNavigationCache(t *testing.T) {
	var prompts []string
	answerStarted := make(chan struct{})
	release := make(chan struct{})
	const target = "example.com/screen.First"
	s := agentTestScreen(t, func(ctx context.Context, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		if len(prompts) == 1 {
			selection, _ := json.Marshal([]string{target})
			return string(selection), nil
		}
		if len(prompts) == 2 {
			close(answerStarted)
		}
		select {
		case <-release:
			return "First calls Second (main.go:4).", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	s.explanationTop = 8
	agentTestKey(t, s, "e")
	if s.agent.cancel == nil || s.explanationTop != 0 || s.syntaxTop != 8 {
		t.Fatal("e did not start a job and save syntax scroll")
	}
	select {
	case <-answerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("answer runner did not start")
	}
	if s.agentKey(context.Background(), "y") {
		t.Fatal("obsolete confirmation was accepted")
	}
	press(t, s, "\r")
	other := s.current
	s.explanationTop = 3
	close(release)
	result := agentTestResult(t, s)
	if result.err != nil || result.id != target || len(prompts) != 2 {
		t.Fatalf("result=%+v, runner calls=%d", result, len(prompts))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, `"target":"`+target+`"`) {
			t.Errorf("call %d missing target JSON: %s", i+1, prompt)
		}
		for _, secret := range []string{"LITERAL_SECRET", "COMMENT_SECRET"} {
			if strings.Contains(prompt, secret) {
				t.Errorf("call %d leaked %q", i+1, secret)
			}
		}
	}
	if strings.Contains(prompts[0], "println") || !strings.Contains(prompts[0], `"candidates":`) {
		t.Fatal("selection request must contain metadata, not source")
	}
	if !strings.Contains(prompts[1], "func First()") || !strings.Contains(prompts[1], "println") || strings.Contains(prompts[1], "func Second()") {
		t.Fatal("answer request did not contain only selected sanitized source")
	}
	if s.current != other || s.explanationTop != 3 || s.agent.texts[other] != "" {
		t.Fatal("background result changed the navigated function's pane")
	}
	if strings.Contains(strings.Join(s.paneLines(100, 10), "\n"), result.text) {
		t.Fatal("answer appeared on the wrong function")
	}
	press(t, s, "b")
	if !strings.Contains(strings.Join(s.paneLines(100, 10), "\n"), result.text) || !strings.Contains(s.agent.texts[target], "エージェントの解説") {
		t.Fatal("returning to the original function did not show its cached answer")
	}
}

func TestAgentExcludedTargetDoesNotTransmit(t *testing.T) {
	var calls atomic.Int32
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		calls.Add(1)
		return "", nil
	})
	s.agent.options.excludes = []string{"main.go"}
	agentTestKey(t, s, "e")
	if s.agent.cancel != nil || !strings.Contains(s.agent.texts[s.current], "target is excluded or blocked") {
		t.Fatalf("excluded target was not rejected: %+v", s.agent)
	}
	if s.agentKey(context.Background(), "y") || calls.Load() != 0 {
		t.Fatal("excluded target was transmitted")
	}
}

func TestAgentPaneSanitizesAndWraps(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		width      int
		want       []string
	}{
		{"wide and combining", "日本abc\ne\u0301xy\n", 4, []string{"日本", "abc", "e\u0301xy", ""}},
		{"terminal controls", "a\x1b[31m\r\x00\u202eb", 40, []string{"a�[31m��b"}},
		{"tabs and trailing spaces", "a\tb  ", 4, []string{"a   ", "    ", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &screen{agent: newAgentPane(agentOptions{}), screenPosition: screenPosition{current: "target"}}
			s.agent.visible = true
			s.agent.texts[s.current] = tc.text
			got := s.paneLines(tc.width, 10)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("paneLines = %q, want %q", got, tc.want)
			}
			for _, line := range got {
				if displayWidth(line) > tc.width || strings.ContainsAny(line, "\x1b\r\x00\u202e\t") {
					t.Fatalf("unsafe or overflowing pane line: %q", line)
				}
			}
		})
	}
}

func TestAgentScreenLoopExitCancelsRunner(t *testing.T) {
	for _, exit := range []string{"quit", "read error", "size error", "parent canceled"} {
		t.Run(exit, func(t *testing.T) {
			started := make(chan struct{})
			s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			})
			agentTestKey(t, s, "e")
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("runner did not start")
			}
			failure := errors.New("terminal failure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if exit == "parent canceled" {
				cancel()
			}
			err := screenLoop(ctx, s, io.Discard, func() (byte, bool, error) {
				if exit == "read error" {
					return 0, false, failure
				}
				return 'q', true, nil
			}, func() (int, int, error) {
				if exit == "size error" {
					return 0, 0, failure
				}
				return 80, 12, nil
			}, false)
			wantErr := exit == "read error" || exit == "size error"
			if (wantErr && !errors.Is(err, failure)) || (!wantErr && err != nil) {
				t.Fatalf("screenLoop error = %v", err)
			}
			if result := agentTestResult(t, s); !errors.Is(result.err, context.Canceled) {
				t.Fatalf("runner was not canceled on exit: %+v", result)
			}
		})
	}
}

func TestAgentScreenLoopDisplaysAnswer(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		calls++
		if calls == 1 {
			return `["example.com/screen.First"]`, nil
		}
		return "ANSWER_FROM_AGENT", nil
	})
	var output bytes.Buffer
	step := 0
	read := func() (byte, bool, error) {
		step++
		switch step {
		case 1:
			return 'e', true, nil
		case 2:
			if s.agent.cancel == nil {
				return 'q', true, nil
			}
			select {
			case result := <-s.agent.results:
				s.agent.results <- result
				return 0, false, nil
			case <-time.After(5 * time.Second):
				return 0, false, errors.New("agent did not complete")
			}
		default:
			return 'q', true, nil
		}
	}
	if err := screenLoop(context.Background(), s, &output, read, func() (int, int, error) {
		return 120, 20, nil
	}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ANSWER_FROM_AGENT") {
		t.Fatal("screen loop did not render the answer")
	}
}

func TestAgentTUIFlags(t *testing.T) {
	t.Setenv("TERM", "dumb")
	for _, timeout := range []string{"1s", "0s"} {
		var output, stderr bytes.Buffer
		calls := 0
		err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "Recur", "--purpose", "explain", "--exclude", "unused", "--timeout", timeout}, strings.NewReader("q\n"), &output, &stderr, func(context.Context, string) (string, error) {
			calls++
			return "", errors.New("unexpected send")
		})
		if timeout == "0s" {
			if err == nil || !strings.Contains(err.Error(), "timeout must be positive") {
				t.Fatalf("timeout validation: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatal("line-mode TUI transmitted source")
		}
	}
}

func TestAgentTimeout(t *testing.T) {
	var calls atomic.Int32
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
		calls.Add(1)
		<-ctx.Done()
		return "", ctx.Err()
	})
	s.agent.options.timeout = time.Second
	agentTestKey(t, s, "e")
	s.explanationTop = 7
	result := agentTestResult(t, s)
	if !errors.Is(result.err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("timeout result=%+v calls=%d", result, calls.Load())
	}
	if s.explanationTop != 0 || !strings.Contains(s.agent.texts[s.current], "解説に失敗") || !strings.Contains(s.agent.texts[s.current], "context deadline exceeded") {
		t.Fatalf("timeout was not displayed: %q", s.agent.texts[s.current])
	}
}

func TestAgentResultWhileSyntaxVisiblePreservesScroll(t *testing.T) {
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		return "", errors.New("offline")
	})
	s.explanationTop = 4
	agentTestKey(t, s, "e")
	agentTestKey(t, s, "s")
	s.explanationTop = 7
	agentTestResult(t, s)
	if s.agent.visible || s.explanationTop != 7 {
		t.Fatal("incoming agent failure reset the active syntax scroll")
	}
	// A successful reply must likewise stay hidden until chat is reopened.
	s.agent.results <- agentResult{id: s.current, text: "ANSWER"}
	if !s.receiveAgentResult() {
		t.Fatal("successful result was not received")
	}
	s.paneLines(40, 10)
	if s.agent.visible || s.explanationTop != 7 {
		t.Fatal("incoming agent answer reset the active syntax scroll")
	}
}
