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
	focusReply                    bool
	scroll                        int
	input                         *agentInput
	comments                      map[string]functionComments
	commentsHidden                bool
}

func newAgentPane(options agentOptions) *agentPane {
	return &agentPane{options: options, results: make(chan agentResult, 1), comments: make(map[string]functionComments)}
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
			a.focusReply = false
			a.text = "生成中は解説を生成できません。完了してから E を押してください。"
			s.explanationTop = 0
			return true
		}
		s.startAgentRequest(ctx, s.current, "表示中の関数の解説を生成してください。", true)
	case "i":
		if !a.visible {
			return false
		}
		if a.cancel != nil {
			a.focusReply = false
			a.text = "生成中は追加指示を入力できません。完了してから i を押してください。"
			s.explanationTop = 0
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
	a.focusReply = false
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
	a.text = "エージェントが解説を生成中です。\n対象: " + id + "\nコード移動可 / q:終了して中断"
	s.explanationTop = 0
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
			a.focusReply = true
			a.text = "エージェントの解説 · コードと照合してください\ni:質問  E:再生成  c:行解説  s:構文\nコメント修正: i → /comments 指示"
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
	if s.agent.lineReview {
		return s.questionLines(width, height), nil
	}
	text := s.agent.text
	if text == "" {
		text = fmt.Sprintf("エージェント解説\n%s\ni:追加指示 / E:解説を生成 / s:構文解説\na:行質問 / ?:これは何？ / C:質問一覧", s.current)
	}
	if s.agent.comments[s.current].stale {
		text += "\n" + staleCommentsAdvice
	}
	lines := wrapPaneText(text, width)
	spans := make([][]syntaxHighlight, len(lines))
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
	lastReply := 0
	for _, message := range s.agent.history {
		appendText("", "")
		heading, label, accent := "質問", "利用者", "\x1b[1;38;5;179m"
		if message.Role == "assistant" {
			heading, label, accent = "回答", "エージェント", "\x1b[1;38;5;110m"
		}
		rule := "── " + heading + " "
		rule += strings.Repeat("─", max(0, width-displayWidth(rule)))
		appendText(rule, "\x1b[38;5;240m")
		if message.Role == "assistant" {
			lastReply = len(lines)
		}
		appendText(label, accent)
		appendText("対象: "+message.id, "\x1b[38;5;245m")
		if message.Role == "assistant" {
			body, styles := markdownLines(message.Content, width)
			lines = append(lines, body...)
			spans = append(spans, styles...)
		} else {
			appendText(message.Content, "")
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
