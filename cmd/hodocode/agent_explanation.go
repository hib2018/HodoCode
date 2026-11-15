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

type agentMessage struct {
	id string
	explain.Message
}

type agentPane struct {
	options    agentOptions
	visible    bool
	text       string
	results    chan agentResult
	cancel     context.CancelFunc
	history    []agentMessage
	focusReply bool
	scroll     int
	input      *agentInput
}

func newAgentPane(options agentOptions) *agentPane {
	return &agentPane{options: options, results: make(chan agentResult, 1)}
}

// All pane state stays on the screen loop; the worker only owns its snapshot.
func (s *screen) agentKey(ctx context.Context, key string) bool {
	a := s.agent
	if a == nil {
		return false
	}
	switch key {
	case "s":
		if a.visible {
			a.scroll = s.explanationTop
			a.visible = false
			s.explanationTop = s.syntaxTop
		}
	case "e", "E":
		if !a.visible {
			s.syntaxTop = s.explanationTop
			a.visible = true
			s.explanationTop = a.scroll
		}
		if a.cancel != nil {
			a.focusReply = false
			a.text = "生成中は追加指示を入力できません。完了してから e・E を押してください。"
			s.explanationTop = 0
			return true
		}
		if key == "e" {
			if a.input == nil {
				a.input = &agentInput{id: s.current}
			}
		} else {
			s.startAgent(ctx, s.current, "表示中の関数の解説を生成してください。")
		}
	default:
		return false
	}
	return true
}

func (s *screen) startAgent(ctx context.Context, id, question string) {
	a := s.agent
	if a.cancel != nil {
		return
	}
	a.focusReply = false
	var edges []analyze.Edge
	for _, edge := range s.flow.edges {
		if edge.From == id {
			edges = append(edges, edge)
		}
	}
	history := make([]explain.Message, 0, len(a.history))
	for _, message := range a.history {
		history = append(history, explain.Message{Role: message.Role, Content: fmt.Sprintf("対象の関数ID: %s\n%s", message.id, message.Content)})
	}
	req, err := explain.Prepare(s.p, id, a.options.purpose, edges, a.options.excludes)
	if err == nil {
		req, err = req.WithConversation(history, question)
	}
	if err != nil {
		a.text = "解説の準備に失敗しました（送信していません）。e:再入力 / E:解説を再生成\n" + err.Error()
		s.explanationTop = 0
		return
	}
	a.history = append(a.history, agentMessage{id: id, Message: explain.Message{Role: "user", Content: question}})
	jobCtx, cancel := context.WithTimeout(ctx, a.options.timeout)
	a.cancel = cancel
	a.text = "エージェントが解説を生成中です。\n対象: " + id + "\nコード移動可 / q:終了して中断"
	s.explanationTop = 0
	runner, results := a.options.runner, a.results
	go func() {
		defer cancel()
		text, err := req.Explain(jobCtx, runner)
		results <- agentResult{id: id, text: text, err: err}
	}()
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
			a.text = "解説に失敗しました。e:再入力 / E:解説を再生成\n対象: " + result.id + "\n" + result.err.Error()
		} else {
			a.history = append(a.history, agentMessage{id: result.id, Message: explain.Message{Role: "assistant", Content: result.text}})
			a.focusReply = true
			a.text = "エージェントの解説（回答の根拠を確認してください）\ne:追加質問 / E:解説を生成 / s:構文解説"
		}
		a.scroll = 0
		if a.visible {
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
	lines, _ := s.paneContent(width, height)
	return lines
}

func (s *screen) paneContent(width, height int) ([]string, [][]syntaxHighlight) {
	if s.agent == nil || !s.agent.visible {
		return s.explanationLines(width, height), nil
	}
	text := s.agent.text
	if text == "" {
		text = fmt.Sprintf("エージェント解説\n%s\ne:追加指示 / E:解説を生成 / s:構文解説", s.current)
	}
	lines := wrapPaneText(text, width)
	spans := make([][]syntaxHighlight, len(lines))
	lastReply := 0
	for _, message := range s.agent.history {
		lines = append(lines, "")
		lines = append(lines, wrapPaneText("──────── 会話 ────────", width)...)
		label := "利用者"
		if message.Role == "assistant" {
			label = "エージェント"
			lastReply = len(lines)
		}
		lines = append(lines, wrapPaneText(label+"\n対象: "+message.id, width)...)
		for len(spans) < len(lines) {
			spans = append(spans, nil)
		}
		if message.Role == "assistant" {
			body, styles := markdownLines(message.Content, width)
			lines = append(lines, body...)
			spans = append(spans, styles...)
		} else {
			lines = append(lines, wrapPaneText(message.Content, width)...)
			for len(spans) < len(lines) {
				spans = append(spans, nil)
			}
		}
	}
	// Leave room below a short reply so its first line can stay at the top.
	if lastReply > 0 {
		for len(lines) < lastReply+height {
			lines = append(lines, "")
			spans = append(spans, nil)
		}
		if s.agent.focusReply {
			s.explanationTop = lastReply
			s.agent.focusReply = false
		}
	}
	return lines, spans
}
