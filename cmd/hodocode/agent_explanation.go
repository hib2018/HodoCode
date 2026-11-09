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
	options      agentOptions
	visible      bool
	texts        map[string]string
	pending      *explain.Request
	pendingID    string
	results      chan agentResult
	cancel       context.CancelFunc
	instructions map[string][]string
	input        *agentInput
}

func newAgentPane(options agentOptions) *agentPane {
	return &agentPane{options: options, texts: map[string]string{}, results: make(chan agentResult, 1)}
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
		purpose := a.purpose(s.current)
		req, err := explain.Prepare(s.p, s.current, purpose, edges, a.options.excludes)
		if err != nil {
			a.texts[s.current] = "解説の準備に失敗しました。\n" + err.Error()
			return true
		}
		a.pending, a.pendingID = req, s.current
		a.texts[s.current] = "送信前の確認（まだ送信していません）\ny: fxへ送信 / n:中止 / []:スクロール\n候補一覧を最後まで確認してください。\n文字列・コメントは空白化しますが、識別子などの機密情報は残り得ます。fxはOSサンドボックスではありません。送信先・料金はfx設定に従い、通信は2回です。\n目的: " + purpose + "\n\n" + req.Preview()
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
		req, id := a.pending, a.pendingID
		a.pending, a.pendingID = nil, ""
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
			a.texts[result.id] = "エージェントの解説（回答の根拠を確認してください）\ni:追加指示 / e:再生成 / s:構文解説\n\n" + result.text
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
	return wrapPaneText(text, width)
}
