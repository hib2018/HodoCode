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

func TestAgentPreviewDoesNotTransmit(t *testing.T) {
	var calls atomic.Int32
	s := agentTestScreen(t, func(context.Context, string) (string, error) {
		calls.Add(1)
		return "", errors.New("unexpected transmission")
	})
	if s.agentKey(context.Background(), "y") {
		t.Fatal("confirmation without a preview was accepted")
	}
	s.explanationTop = 9
	agentTestKey(t, s, "e")
	if !s.agent.visible || s.agent.pending == nil || s.agent.pendingID != s.current || s.explanationTop != 0 {
		t.Fatal("preview did not set visible pending state and reset scrolling")
	}
	preview := strings.Join(s.paneLines(200, 20), "\n")
	for _, want := range []string{"まだ送信していません", "y: fxへ送信", "通信は2回", "Explain the flow", "Target: " + s.current, "main.go:2-5", "example.com/screen.Second"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q: %s", want, preview)
		}
	}
	for _, secret := range []string{"LITERAL_SECRET", "COMMENT_SECRET", "println"} {
		if strings.Contains(preview, secret) {
			t.Errorf("preview contains source %q", secret)
		}
	}
	agentTestKey(t, s, "n")
	if s.agent.pending != nil || s.agent.pendingID != "" || !strings.Contains(s.agent.texts[s.current], "送信を中止") {
		t.Fatal("declining did not discard the pending request")
	}
	if s.agentKey(context.Background(), "y") || calls.Load() != 0 {
		t.Fatal("preview/decline transmitted a request")
	}
	agentTestKey(t, s, "s")
	if s.agent.visible || !reflect.DeepEqual(s.paneLines(40, 10), s.explanationLines(40, 10)) {
		t.Fatal("s did not restore syntax explanations")
	}
}

func TestAgentConfirmationTwoCallsAndNavigationCache(t *testing.T) {
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
	agentTestKey(t, s, "e")
	// A pending confirmation belongs to the function that was previewed.
	press(t, s, "\r")
	if s.agentKey(context.Background(), "y") {
		t.Fatal("confirmation from a different function was accepted")
	}
	press(t, s, "b")
	s.explanationTop = 8
	agentTestKey(t, s, "y")
	if s.agent.pending != nil || s.agent.pendingID != "" || s.agent.cancel == nil || s.explanationTop != 0 {
		t.Fatal("confirmation did not consume the preview and start a job")
	}
	select {
	case <-answerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("answer runner did not start")
	}
	if s.agentKey(context.Background(), "y") {
		t.Fatal("duplicate confirmation was accepted")
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
	if s.agent.pending != nil || s.agent.pendingID != "" || !strings.Contains(s.agent.texts[s.current], "target is excluded or blocked") {
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
			agentTestKey(t, s, "y")
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
			return 'y', true, nil
		case 3:
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
	if !strings.Contains(output.String(), "ANSWER_FROM_AGENT") || !strings.Contains(output.String(), "まだ送信していません") {
		t.Fatal("screen loop did not render preview and answer")
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
	agentTestKey(t, s, "y")
	s.explanationTop = 7
	result := agentTestResult(t, s)
	if !errors.Is(result.err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("timeout result=%+v calls=%d", result, calls.Load())
	}
	if s.explanationTop != 0 || !strings.Contains(s.agent.texts[s.current], "解説に失敗") || !strings.Contains(s.agent.texts[s.current], "context deadline exceeded") {
		t.Fatalf("timeout was not displayed: %q", s.agent.texts[s.current])
	}
}
