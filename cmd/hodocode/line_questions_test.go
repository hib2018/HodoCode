package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLineQuestionDraftEditDeleteAndDisplay(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.cursorRow = 1
	agentTestKey(t, s, "a")
	overlay := s.renderAgentInput(100, 24)
	for _, want := range []string{"行質問", "main.go:3", "println", "Enter:保存"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay missing %q: %s", want, overlay)
		}
	}
	typeAgentInput(t, s, "なぜ？\r")
	if len(s.agent.questions) != 1 || s.agent.cancel != nil {
		t.Fatal("saving draft sent or lost question")
	}
	d := s.agent.questions[0]
	if d.line != 3 || d.text != "なぜ？" || !d.selected {
		t.Fatalf("draft=%+v", d)
	}
	s.cursorRow = 2
	agentTestKey(t, s, "?")
	if len(s.agent.questions) != 2 || s.agent.questions[1].text != "これは何？" {
		t.Fatal("preset missing")
	}
	ai := map[int][]string{1: {"existing AI"}}
	s.agent.comments[s.current] = functionComments{lines: ai}
	rows := s.displayRows(90)
	var display string
	for _, row := range rows {
		display += row.text + "\n"
	}
	for _, want := range []string{"行質問", "利用者 [q1]", "なぜ？", "AI解説", "existing AI"} {
		if !strings.Contains(display, want) {
			t.Fatalf("display missing %q: %s", want, display)
		}
	}
	agentTestKey(t, s, "C")
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "\x15変更\r")
	if d.text != "変更" || len(s.agent.questions) != 2 {
		t.Fatal("editing duplicated draft")
	}
	agentTestKey(t, s, " ")
	if d.selected {
		t.Fatal("selection not toggled")
	}
	agentTestKey(t, s, "d")
	if len(s.agent.questions) != 1 || s.agent.questions[0].id != "q2" {
		t.Fatal("delete removed wrong draft")
	}
	if !reflect.DeepEqual(s.agent.comments[s.current].lines, ai) {
		t.Fatal("questions changed AI comments")
	}
}

func TestLineQuestionBatchAnswersAndRetry(t *testing.T) {
	var prompts []string
	fail := true
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		if fail {
			return "", errors.New("offline")
		}
		if len(prompts)%2 == 0 {
			return `["example.com/screen.First"]`, nil
		}
		return `{"answers":[{"id":"q1","text":"first answer"},{"id":"q2","text":"second answer"}]}`, nil
	})
	s.cursorRow = 1
	agentTestKey(t, s, "?")
	s.cursorRow = 2
	agentTestKey(t, s, "?")
	original := map[int][]string{1: {"keep AI"}}
	s.agent.comments[s.current] = functionComments{lines: original}
	agentTestKey(t, s, "C")
	agentTestKey(t, s, "\r")
	if !s.agent.questions[0].sending || s.agent.cancel == nil {
		t.Fatal("batch not started")
	}
	if result := agentTestResult(t, s); result.err == nil {
		t.Fatal("expected failure")
	}
	for _, d := range s.agent.questions {
		if d.sending || !d.selected || d.text != "これは何？" || !strings.Contains(d.status, "送信失敗") {
			t.Fatalf("lost retry draft: %+v", d)
		}
	}
	fail = false
	agentTestKey(t, s, "\r")
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
	for i, d := range s.agent.questions {
		if d.sending || d.selected || d.answer == "" {
			t.Fatalf("answer %d not attached: %+v", i, d)
		}
	}
	if len(prompts) != 3 {
		t.Fatalf("runner calls=%d", len(prompts))
	}
	for _, prompt := range prompts {
		if strings.Contains(prompt, "LITERAL_SECRET") || strings.Contains(prompt, "COMMENT_SECRET") {
			t.Fatal("raw source sent")
		}
	}
	if !reflect.DeepEqual(s.agent.comments[s.current].lines, original) {
		t.Fatal("AI comments overwritten")
	}
	pane := strings.Join(s.paneLines(100, 15), "\n")
	if !strings.Contains(pane, "first answer") || !strings.Contains(pane, "second answer") {
		t.Fatal("answers not visible in review")
	}
}

func TestLineQuestionStaleReloadRebindAndExclusion(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.cursorRow = 1
	agentTestKey(t, s, "?")
	d := s.agent.questions[0]
	path := filepath.Join(s.p.Root, "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "println(", "print(", 1))
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.reload(20, 80); err != nil {
		t.Fatal(err)
	}
	if !d.stale || len(s.questionComments()) != 0 {
		t.Fatal("changed source did not invalidate draft")
	}
	s.sendLineQuestions(context.Background())
	if s.agent.cancel != nil || !strings.Contains(s.notice, "古い位置") {
		t.Fatal("stale draft was sent")
	}
	agentTestKey(t, s, "C")
	agentTestKey(t, s, "p")
	if d.stale || d.snapshot.source == "" {
		t.Fatal("explicit rebind failed")
	}
	s.agent.options.excludes = []string{"main.go"}
	agentTestKey(t, s, "\r")
	if s.agent.cancel != nil || !strings.Contains(s.notice, "送信していません") || len(s.agent.questions) != 1 {
		t.Fatal("excluded source not blocked or draft lost")
	}
}

func TestLineQuestionTimeoutAndCancelInput(t *testing.T) {
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	s.agent.options.timeout = time.Millisecond
	agentTestKey(t, s, "a")
	typeAgentInput(t, s, "cancel\x1b")
	s.agentInputIdle()
	if len(s.agent.questions) != 0 {
		t.Fatal("cancel saved input")
	}
	agentTestKey(t, s, "?")
	s.sendLineQuestions(context.Background())
	if result := agentTestResult(t, s); !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", result.err)
	}
	if !s.agent.questions[0].selected || s.agent.questions[0].sending {
		t.Fatal("timeout lost retry state")
	}
}

func TestLineQuestionScreenLoopKeys(t *testing.T) {
	s := agentTestScreen(t, nil)
	// The standalone Escape is followed by a read timeout, as on a real terminal.
	keys := []byte("a日本語q\r?Ci\x15編集\r \x1b")
	idle := false
	read := func() (byte, bool, error) {
		if len(keys) > 0 {
			b := keys[0]
			keys = keys[1:]
			return b, true, nil
		}
		if !idle {
			idle = true
			return 0, false, nil
		}
		return 'q', true, nil
	}
	var out bytes.Buffer
	if err := screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 120, 24, nil }, false); err != nil {
		t.Fatal(err)
	}
	if s.agent.lineReview || s.agent.input != nil || len(s.agent.questions) != 2 || s.agent.cancel != nil {
		t.Fatal("screen loop left incorrect modal state")
	}
	if s.agent.questions[0].text != "編集" || s.agent.questions[0].selected || s.agent.questions[1].text != "これは何？" {
		t.Fatalf("keys affected navigation instead of drafts: %+v", s.agent.questions)
	}
	if !strings.Contains(out.String(), "QUESTIONS") {
		t.Fatal("missing list mode indicator")
	}
}

func TestLineQuestionSubsetAndCrossFunctionNavigation(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		calls++
		if strings.Contains(prompt, `"id":"q2"`) {
			t.Error("unselected question sent")
		}
		if calls == 1 {
			return `["example.com/screen.First"]`, nil
		}
		return `{"answers":[{"id":"q1","text":"answer for First"}]}`, nil
	})
	agentTestKey(t, s, "?")
	first := s.current
	press(t, s, "\r")
	if s.current == first {
		t.Fatal("failed to navigate to second function")
	}
	agentTestKey(t, s, "?")
	s.agent.questions[1].selected = false
	s.sendLineQuestions(context.Background())
	if result := agentTestResult(t, s); result.err != nil {
		t.Fatal(result.err)
	}
	if s.agent.questions[0].answer != "answer for First" || s.agent.questions[1].answer != "" {
		t.Fatal("answer attached to wrong function")
	}
	if strings.Contains(strings.Join(s.questionComments()[0], "\n"), "answer for First") {
		t.Fatal("answer inserted into navigation destination")
	}
	press(t, s, "b")
	if s.current != first || !strings.Contains(strings.Join(s.questionComments()[s.agent.questions[0].line-s.agent.questions[0].snapshot.line], "\n"), "answer for First") {
		t.Fatal("answer lost at original line")
	}
}

func TestLineQuestionReloadWhileSending(t *testing.T) {
	started := make(chan struct{})
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	agentTestKey(t, s, "?")
	s.sendLineQuestions(context.Background())
	<-started
	old := s.agent
	if err := s.reload(20, 80); err != nil {
		t.Fatal(err)
	}
	d := s.agent.questions[0]
	if d.sending || !d.selected || d.stale || s.agent.cancel != nil || !strings.Contains(d.status, "中断") {
		t.Fatalf("reload lost retry draft: %+v", d)
	}
	select {
	case <-old.results:
	case <-time.After(time.Second):
		t.Fatal("reload failed to cancel worker")
	}
	if s.receiveAgentResult() {
		t.Fatal("old result applied to reloaded screen")
	}
}

func TestLineQuestionEditedAnswerAndBoundedScroll(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "?")
	d := s.agent.questions[0]
	d.answer, d.selected, d.status = "old answer", false, "回答済み"
	agentTestKey(t, s, "C")
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "\x15追加の質問\r")
	if !d.selected || d.answer != "" || d.text != "追加の質問" {
		t.Fatal("editing answer did not create a sendable revised draft")
	}
	count := len(s.questionLines(40, 12))
	s.explanationTop = count + 100
	if len(s.questionLines(40, 12)) != count {
		t.Fatal("scrolling grew list without bound")
	}
}

func TestLineQuestionNonlocalTarget(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.current = "fmt.Println"
	agentTestKey(t, s, "a")
	if s.agent.input != nil || !strings.Contains(s.notice, "対象外") {
		t.Fatal("nonlocal input allowed or missing reason")
	}
	agentTestKey(t, s, "?")
	if len(s.agent.questions) != 0 {
		t.Fatal("nonlocal question saved")
	}
}
