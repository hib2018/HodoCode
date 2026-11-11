package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hib2018/HodoCode/internal/analyze"
	"github.com/hib2018/HodoCode/internal/explain"
)

type agentOptions struct {
	runner   explain.Runner
	purpose  string
	excludes []string
	timeout  time.Duration
}

type agentResult struct {
	id, text string
	err      error
}

type agentPane struct {
	options         agentOptions
	visible         bool
	texts           map[string]string
	pending         *explain.Request
	pendingID       string
	results         chan agentResult
	cancel          context.CancelFunc
	history         map[string][]explain.Message
	questions       map[string]string
	focusReply      map[string]bool
	pendingQuestion string
	input           *agentInput
}

func newAgentPane(options agentOptions) *agentPane {
	return &agentPane{options: options, texts: map[string]string{}, results: make(chan agentResult, 1), history: map[string][]explain.Message{}, questions: map[string]string{}, focusReply: map[string]bool{}}
}

// All pane state stays on the screen loop; the worker only owns its snapshot.
func (s *screen) agentKey(ctx context.Context, key string) bool {
	a := s.agent
	if a == nil {
		return false
	}
	switch key {
	case "s":
		a.visible = false
		s.explanationTop = 0
	case "i":
		if a.cancel != nil {
			a.visible = true
			a.texts[s.current] = "生成中は追加指示を入力できません。完了してから i を押してください。"
			s.explanationTop = 0
			return true
		}
		if a.input == nil {
			a.input = &agentInput{id: s.current}
		}
	case "e":
		delete(a.focusReply, s.current)
		a.visible = true
		s.explanationTop = 0
		if a.cancel != nil {
			return true
		}
		a.pending, a.pendingID = nil, ""
		var edges []analyze.Edge
		for _, edge := range s.flow.edges {
			if edge.From == s.current {
				edges = append(edges, edge)
			}
		}
		purpose, question := a.options.purpose, a.questions[s.current]
		history := a.history[s.current]
		if question == "" && len(history) > 0 {
			a.texts[s.current] = "会話履歴\ni:追加質問 / s:構文解説"
			return true
		}
		// A failed question is already visible, but is sent as the current question on retry.
		if n := len(history); question != "" && n > 0 && history[n-1].Role == "user" && history[n-1].Content == question {
			history = history[:n-1]
		}
		req, err := explain.Prepare(s.p, s.current, purpose, edges, a.options.excludes)
		if err == nil && question != "" {
			req, err = req.WithConversation(history, question)
		}
		if err != nil {
			a.texts[s.current] = "解説の準備に失敗しました（送信していません）。\n" + err.Error()
			return true
		}
		a.pending, a.pendingID, a.pendingQuestion = req, s.current, question
		a.texts[s.current] = "送信前の確認（まだ送信していません）\ny: fxへ送信 / n:中止 / []:スクロール\n候補一覧を最後まで確認してください。\n文字列・コメントは空白化しますが、識別子などの機密情報は残り得ます。会話履歴と今回の質問は加工せず送信します。fxはOSサンドボックスではありません。送信先・料金はfx設定に従い、通信は2回です。\n目的: " + purpose + "\n今回の質問: " + question + "\n\n" + req.Preview()
	case "n":
		if a.pendingID != s.current || a.pending == nil {
			return false
		}
		a.pending, a.pendingID = nil, ""
		a.texts[s.current] = "送信を中止しました。e:確認し直す / s:構文解説"
		s.explanationTop = 0
	case "y":
		if !a.visible || a.pending == nil || a.pendingID != s.current || a.cancel != nil {
			return false
		}
		req, id, question := a.pending, a.pendingID, a.pendingQuestion
		a.pending, a.pendingID, a.pendingQuestion = nil, "", ""
		history := a.history[id]
		if n := len(history); question != "" && (n == 0 || history[n-1].Role != "user" || history[n-1].Content != question) {
			a.history[id] = append(history, explain.Message{Role: "user", Content: question})
		}
		jobCtx, cancel := context.WithTimeout(ctx, a.options.timeout)
		a.cancel = cancel
		a.texts[id] = "エージェントが解説を生成中です。\nコード移動可 / q:終了して中断"
		s.explanationTop = 0
		runner, results := a.options.runner, a.results
		go func() {
			defer cancel()
			text, err := req.Explain(jobCtx, runner)
			results <- agentResult{id: id, text: text, err: err}
		}()
	default:
		return false
	}
	return true
}

func (s *screen) receiveAgentResult() bool {
	if s.agent == nil {
		return false
	}
	select {
	case result := <-s.agent.results:
		a := s.agent
		a.cancel = nil
		if result.err != nil {
			a.texts[result.id] = "解説に失敗しました。e:再試行\n" + result.err.Error()
		} else {
			a.history[result.id] = append(a.history[result.id], explain.Message{Role: "assistant", Content: result.text})
			delete(a.questions, result.id)
			a.focusReply[result.id] = true
			a.texts[result.id] = "エージェントの解説（回答の根拠を確認してください）\ni:追加質問 / s:構文解説"
		}
		if s.current == result.id {
			s.explanationTop = 0
		}
		return true
	default:
		return false
	}
}

func wrapPaneText(text string, width int) []string {
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		// Sanitize terminal controls before measuring and wrapping untrusted output.
		raw = strings.TrimRight(displaySlice(raw, 0, displayWidth(raw)), " ")
		line, cells := "", 0
		for _, r := range raw {
			n := runeWidth(r)
			if cells+n > width {
				lines = append(lines, line)
				line, cells = "", 0
			}
			line += string(r)
			cells += n
		}
		lines = append(lines, line)
	}
	return lines
}

func (s *screen) paneLines(width, height int) []string {
	if s.agent == nil || !s.agent.visible {
		return s.explanationLines(width, height)
	}
	text := s.agent.texts[s.current]
	if text == "" {
		text = fmt.Sprintf("エージェント解説\n%s\ni:追加指示 / e:送信前の確認 / s:構文解説", s.current)
	}
	lines := wrapPaneText(text, width)
	lastReply := 0
	for _, message := range s.agent.history[s.current] {
		lines = append(lines, "")
		lines = append(lines, wrapPaneText("──────── 会話 ────────", width)...)
		label := "利用者"
		if message.Role == "assistant" {
			label = "エージェント"
			lastReply = len(lines)
		}
		lines = append(lines, wrapPaneText(label+"\n"+message.Content, width)...)
	}
	// Leave room below a short reply so its first line can stay at the top.
	if lastReply > 0 {
		for len(lines) < lastReply+height {
			lines = append(lines, "")
		}
		if s.agent.focusReply[s.current] {
			s.explanationTop = lastReply
			delete(s.agent.focusReply, s.current)
		}
	}
	return lines
}
