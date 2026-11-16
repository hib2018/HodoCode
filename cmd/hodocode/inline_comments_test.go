package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hib2018/HodoCode/internal/explain"
)

func lineAnswer(summary string, comments ...explain.LineComment) string {
	if comments == nil {
		comments = []explain.LineComment{}
	}
	data, _ := json.Marshal(explain.LineExplanation{Summary: summary, Comments: comments})
	return string(data)
}

func TestInlineGenerationNavigationValidationAndFailure(t *testing.T) {
	calls := 0
	answer := lineAnswer("概要だけ", explain.LineComment{File: "main.go", Function: "example.com/screen.First", Line: 3, EndLine: 4, Text: "行単位の詳細"}, explain.LineComment{File: "other.go", Function: "example.com/screen.First", Line: 3, Text: "不明な位置"})
	fail := false
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		if fail {
			return "", errors.New("offline")
		}
		calls++
		if calls%2 == 1 {
			return `["example.com/screen.First"]`, nil
		}
		if !strings.Contains(prompt, "comments") {
			t.Error("missing structured prompt")
		}
		return answer, nil
	})
	id := s.current
	agentTestKey(t, s, "E")
	press(t, s, "\r")
	agentTestResult(t, s)
	if s.current == id || len(s.inlineComments()) != 0 {
		t.Fatal("inserted result into navigated function")
	}
	press(t, s, "b")
	want := map[int][]string{2: {"行単位の詳細"}}
	if !reflect.DeepEqual(s.inlineComments(), want) {
		t.Fatalf("comments=%v", s.inlineComments())
	}
	pane := strings.Join(s.paneLines(100, 10), "\n")
	if strings.Contains(pane, "行単位の詳細") || !strings.Contains(pane, "不明な位置") || !strings.Contains(pane, "概要だけ") {
		t.Fatalf("wrong split: %s", pane)
	}
	agentTestKey(t, s, "c")
	if len(s.inlineComments()) != 0 {
		t.Fatal("not hidden")
	}
	agentTestKey(t, s, "s")
	agentTestKey(t, s, "c")
	if !reflect.DeepEqual(s.inlineComments(), want) || calls != 2 {
		t.Fatal("toggle regenerated or depended on right pane")
	}
	fail = true
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	if !reflect.DeepEqual(s.inlineComments(), want) {
		t.Fatal("failure erased comments")
	}
	fail = false
	answer = "malformed JSON"
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	if !reflect.DeepEqual(s.inlineComments(), want) || !strings.Contains(s.agent.text, "失敗") {
		t.Fatal("malformed result replaced comments")
	}
	answer = lineAnswer("新しい概要", explain.LineComment{File: "main.go", Function: id, Line: 3, Text: "置換済み"})
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	if got := s.inlineComments(); !reflect.DeepEqual(got, map[int][]string{1: {"置換済み"}}) {
		t.Fatalf("not replaced: %v", got)
	}
}

func TestInlineNormalQuestionAndExplicitUpdate(t *testing.T) {
	calls := 0
	s := agentTestScreen(t, func(_ context.Context, prompt string) (string, error) {
		calls++
		if calls%2 == 1 {
			return `["example.com/screen.First"]`, nil
		}
		if calls == 4 {
			return "通常の返答", nil
		}
		text := "初回のコメント"
		if calls == 6 {
			text = "修正済みのコメント"
			if !strings.Contains(prompt, "初回のコメント") {
				t.Error("prior inline comments missing from conversation")
			}
		}
		return lineAnswer("概要", explain.LineComment{File: "main.go", Function: "example.com/screen.First", Line: 3, Text: text}), nil
	})
	agentTestKey(t, s, "E")
	agentTestResult(t, s)
	old := s.agent.comments[s.current]
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "なぜ？\r")
	agentTestResult(t, s)
	if !reflect.DeepEqual(old, s.agent.comments[s.current]) {
		t.Fatal("ordinary followup changed comments")
	}
	agentTestKey(t, s, "i")
	typeAgentInput(t, s, "/comments より詳しく修正して\r")
	agentTestResult(t, s)
	if calls != 6 || !reflect.DeepEqual(s.inlineComments(), map[int][]string{1: {"修正済みのコメント"}}) || strings.Contains(s.agent.text, "失敗") {
		t.Fatal("explicit update failed")
	}
}

func TestInlineReloadKeepsUnchangedAndInvalidatesChanged(t *testing.T) {
	s := agentTestScreen(t, nil)
	id := s.current
	snapshot, err := snapshotComments(s.p, id)
	if err != nil {
		t.Fatal(err)
	}
	s.agent.comments[id] = functionComments{snapshot: snapshot, lines: map[int][]string{1: {"保存する解説"}}}
	s.agent.visible = true
	s.agent.history = []agentMessage{{id: id, Message: explain.Message{Role: "assistant", Content: "前の概要"}}}
	if err := s.reload(12, 70); err != nil {
		t.Fatal(err)
	}
	if len(s.inlineComments()) != 1 || len(s.agent.history) != 1 {
		t.Fatal("unchanged reload discarded explanation")
	}
	path := filepath.Join(s.p.Root, "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), "LITERAL_SECRET", "CHANGED_LITERAL", 1)
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.reload(12, 70); err != nil {
		t.Fatal(err)
	}
	if len(s.inlineComments()) != 0 || !s.agent.comments[id].stale {
		t.Fatal("changed source kept stale comments")
	}
	if pane := strings.Join(s.paneLines(100, 12), "\n"); !strings.Contains(pane, "E で再生成") {
		t.Fatal("no regeneration advice")
	}
	agentTestKey(t, s, "s")
	if pane := strings.Join(s.paneLines(100, 12), "\n"); !strings.Contains(pane, "E で再生成") {
		t.Fatal("syntax pane hid regeneration advice")
	}
}

func TestInlineRejectsWrongFunctionAndOutOfRange(t *testing.T) {
	s := agentTestScreen(t, nil)
	snapshot, err := snapshotComments(s.p, s.current)
	if err != nil {
		t.Fatal(err)
	}
	r := agentResult{id: s.current, text: "概要", snapshot: snapshot, inline: true, comments: []explain.LineComment{
		{File: "main.go", Function: "wrong", Line: 3, Text: "別関数"},
		{File: "main.go", Function: s.current, Line: 1, Text: "関数の外"},
		{File: "main.go", Function: s.current, Line: 4, EndLine: 3, Text: "逆順"},
		{File: "main.go", Function: s.current, Line: 3, EndLine: 99, Text: "末尾の外"},
	}}
	s.storeComments(&r)
	if len(s.inlineComments()) != 0 || strings.Count(r.text, "配置を確認できない") != 4 {
		t.Fatalf("unverified positions accepted: %+v", r)
	}
}

func TestInlineNeovimUsesSourceCoordinates(t *testing.T) {
	s := agentTestScreen(t, nil)
	setDisplayComments(s, map[int][]string{0: {strings.Repeat("long comment ", 30)}})
	s.cursorRow, s.cursorColumn = 2, 1
	s.followCursor(5, 30)
	if s.cursorVisualRow(30) <= s.cursorRow {
		t.Fatal("test needs inserted display rows")
	}
	cmd, err := s.neovimCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "call cursor(4,2)") {
		t.Fatalf("wrong Neovim position: %v", cmd.Args)
	}
}

func TestInlineReloadCancelsPendingGeneration(t *testing.T) {
	started := make(chan struct{})
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	agentTestKey(t, s, "E")
	<-started
	oldResults := s.agent.results
	if err := s.reload(12, 70); err != nil {
		t.Fatal(err)
	}
	if s.agent.cancel != nil || !strings.Contains(s.agent.text, "中断") || strings.Contains(s.agent.text, "生成中") {
		t.Fatal("canceled generation still appears pending")
	}
	select {
	case result := <-oldResults:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("not canceled: %v", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old worker did not stop")
	}
	if s.receiveAgentResult() {
		t.Fatal("old result reached reloaded pane")
	}
}
