package main

import (
	"context"
	"encoding/json"
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
	questions []explain.LineQuestion
	answers   map[string]string
	id, text  string
	err       error
	inline    bool
	comments  []explain.LineComment
	snapshot  commentSnapshot
}

type agentMessage struct {
	id       string
	comments []explain.LineComment
	explain.Message
}

type agentPane struct {
	questions                     []*lineDraft
	questionSerial, questionIndex int
	lineReview, questionFocus     bool
	options                       agentOptions
	visible                       bool
	text                          string
	results                       chan agentResult
	cancel                        context.CancelFunc
	history                       []agentMessage
	followTail, unread, stopping  bool
	scroll                        int
	input                         *agentInput
	comments                      map[string]functionComments
	commentsHidden                bool
}

func newAgentPane(options agentOptions) *agentPane {
	return &agentPane{options: options, results: make(chan agentResult, 1), followTail: true, comments: make(map[string]functionComments)}
}

// All pane state stays on the screen loop; the worker only owns its snapshot.
func (s *screen) agentKey(ctx context.Context, key string) bool {
	a := s.agent
	if a == nil {
		return false
	}
	if s.lineQuestionKey(ctx, key) {
		return true
	}
	switch key {
	case "<", ">":
		delta := -5
		if key == ">" {
			delta = 5
		}
		s.notePercent = max(25, min(70, s.panePercent()+delta))
	case "x":
		if a.cancel == nil {
			return false
		}
		a.stopping = true
		a.text = "中断中…"
		a.cancel()
	case "f":
		if !a.visible || a.lineReview {
			return false
		}
		a.followTail, a.unread = true, false
	case "c":
		a.commentsHidden = !a.commentsHidden
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
		if key == "e" {
			return true
		}
		if a.cancel != nil {
			a.text = "生成中…"
			return true
		}
		s.startAgentRequest(ctx, s.current, "表示中の関数の解説を生成してください。", true)
	case "i":
		if !a.visible {
			return false
		}
		if a.cancel != nil {
			a.text = "生成中…"
			return true
		}
		if a.input == nil {
			a.input = &agentInput{id: s.current}
		}
	default:
		return false
	}
	return true
}

func (s *screen) startAgent(ctx context.Context, id, question string) {
	s.startAgentRequest(ctx, id, question, false)
}

func (s *screen) startAgentRequest(ctx context.Context, id, question string, inline bool) {
	a := s.agent
	if a.cancel != nil {
		return
	}
	var edges []analyze.Edge
	for _, edge := range s.flow.edges {
		if edge.From == id {
			edges = append(edges, edge)
		}
	}
	history := make([]explain.Message, 0, len(a.history))
	for _, message := range a.history {
		content := fmt.Sprintf("対象の関数ID: %s\n%s", message.id, message.Content)
		if len(message.comments) > 0 {
			comments, _ := json.Marshal(message.comments)
			content += "\n行別解説: " + string(comments)
		}
		history = append(history, explain.Message{Role: message.Role, Content: content})
	}
	req, err := explain.Prepare(s.p, id, a.options.purpose, edges, a.options.excludes)
	if err == nil {
		req, err = req.WithConversation(history, question)
	}
	if err == nil && inline {
		req, err = req.WithLineComments()
	}
	var snapshot commentSnapshot
	if err == nil && inline {
		snapshot, err = snapshotComments(s.p, id)
	}
	if err != nil {
		a.text = "解説の準備に失敗しました（送信していません）。i:再入力 / E:解説を再生成\n" + err.Error()
		s.explanationTop = 0
		return
	}
	a.history = append(a.history, agentMessage{id: id, Message: explain.Message{Role: "user", Content: question}})
	jobCtx, cancel := context.WithTimeout(ctx, a.options.timeout)
	a.cancel = cancel
	a.text = "生成中…"
	runner, results := a.options.runner, a.results
	go func() {
		defer cancel()
		text, err := req.Explain(jobCtx, runner)
		result := agentResult{id: id, text: text, err: err, inline: inline, snapshot: snapshot}
		if err == nil && inline {
			answer, parseErr := explain.ParseLineExplanation(text)
			result.err = parseErr
			result.text, result.comments = answer.Summary, answer.Comments
		}
		results <- result
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
		if a.stopping {
			a.stopping = false
			a.text = "生成を中断しました。"
			if len(result.questions) > 0 {
				s.receiveLineAnswers(agentResult{questions: result.questions, err: context.Canceled})
			}
			return true
		}
		if len(result.questions) > 0 {
			s.receiveLineAnswers(result)
			return true
		}
		if result.err != nil {
			a.text = "解説に失敗しました。i:再入力 / E:解説を再生成\n対象: " + result.id + "\n" + result.err.Error()
		} else {
			if result.inline {
				s.storeComments(&result)
			}
			a.history = append(a.history, agentMessage{id: result.id, comments: result.comments, Message: explain.Message{Role: "assistant", Content: result.text}})
			a.text = ""
		}
		if !a.followTail {
			a.unread = true
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
	if s.agent.lineReview {
		return s.questionLines(width, height), nil
	}
	var lines []string
	var spans [][]syntaxHighlight
	appendText := func(text, style string) {
		for _, line := range wrapPaneText(text, width) {
			lines = append(lines, line)
			var highlights []syntaxHighlight
			if cells := displayWidth(line); cells > 0 && style != "" {
				highlights = []syntaxHighlight{{start: 0, end: cells, style: style}}
			}
			spans = append(spans, highlights)
		}
	}
	lastTarget := ""
	for _, message := range s.agent.history {
		if len(lines) > 0 {
			appendText("", "")
		}
		if message.id != lastTarget {
			appendText("対象: "+message.id, "\x1b[38;5;245m")
			lastTarget = message.id
		}
		label, accent := "› 利用者", "\x1b[1;38;5;179m"
		if message.Role == "assistant" {
			label, accent = "• エージェント", "\x1b[1;38;5;110m"
		}
		appendText(label, accent)
		if message.Role == "assistant" {
			body, styles := markdownLines(message.Content, width)
			lines = append(lines, body...)
			spans = append(spans, styles...)
		} else {
			appendText(message.Content, "")
		}
	}
	if s.agent.text != "" {
		appendText(s.agent.text, "")
	}
	if s.agent.comments[s.current].stale {
		appendText(staleCommentsAdvice, "")
	}
	if len(lines) == 0 {
		appendText("E:解説を生成 · i:質問", "")
	}
	if s.agent.followTail {
		top := max(0, len(lines)-height)
		if s.agent.visible {
			s.explanationTop = top
		} else {
			s.agent.scroll = top
		}
	}
	return lines, spans
}
