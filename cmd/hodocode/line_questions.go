package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/hib2018/HodoCode/internal/explain"
)

type lineDraft struct {
	id, function, text, answer, status string
	snapshot                           commentSnapshot
	line                               int
	selected, stale, sending           bool
}

func (s *screen) lineQuestionKey(ctx context.Context, key string) bool {
	a := s.agent
	if a.lineReview {
		n := len(a.questions)
		a.questionIndex = max(0, min(a.questionIndex, n-1))
		var d *lineDraft
		if n > 0 {
			d = a.questions[a.questionIndex]
		}
		switch key {
		case "C", "\x1b":
			a.lineReview = false
		case "j", "down":
			a.questionFocus = true
			a.questionIndex = min(n-1, a.questionIndex+1)
		case "k", "up":
			a.questionFocus = true
			a.questionIndex = max(0, a.questionIndex-1)
		case " ":
			if d != nil && !d.sending {
				d.selected = !d.selected
			}
		case "i":
			if d != nil && !d.sending {
				a.input = &agentInput{id: d.function, text: d.text, draft: d, lineSnapshot: d.snapshot, line: d.line}
			}
		case "d":
			if d != nil && !d.sending {
				a.questions = append(a.questions[:a.questionIndex], a.questions[a.questionIndex+1:]...)
			}
		case "p":
			if d != nil && !d.sending {
				snapshot, err := s.questionTarget()
				if err != nil {
					s.notice = err.Error()
				} else {
					d.function, d.snapshot, d.line, d.stale = s.current, snapshot, snapshot.line+s.cursorRow, false
					d.answer, d.status = "", "位置を再指定しました"
				}
			}
		case "\r", "\n":
			s.sendLineQuestions(ctx)
		case "pageup":
			s.explanationTop = max(0, s.explanationTop-10)
		case "pagedown":
			s.explanationTop += 10
		case "[", "]", "q", "\x03", "\x04":
			return false
		default:
			return true
		}
		return true
	}
	switch key {
	case "C":
		if !a.visible {
			s.syntaxTop = s.explanationTop
			a.visible = true
		}
		a.lineReview, a.questionFocus = true, false
		s.explanationTop = 0
	case "a", "?":
		snapshot, err := s.questionTarget()
		if err != nil {
			s.notice = err.Error()
			return true
		}
		in := &agentInput{id: s.current, lineSnapshot: snapshot, line: snapshot.line + s.cursorRow}
		if key == "?" {
			in.text = "これは何？"
			s.saveLineQuestion(in)
		} else {
			a.input = in
		}
	default:
		return false
	}
	return true
}

func (s *screen) questionTarget() (commentSnapshot, error) {
	if !s.p.LocalFunction(s.current) {
		return commentSnapshot{}, fmt.Errorf("行質問の対象外です。プロジェクト内関数だけ送信できます。")
	}
	snapshot, err := snapshotComments(s.p, s.current)
	if err != nil {
		return snapshot, err
	}
	if s.cursorRow < 0 || snapshot.line+s.cursorRow > snapshot.end {
		return snapshot, fmt.Errorf("対象コード行を選んでください。")
	}
	return snapshot, nil
}

func (s *screen) saveLineQuestion(in *agentInput) {
	a, d := s.agent, in.draft
	if d == nil {
		a.questionSerial++
		d = &lineDraft{id: fmt.Sprintf("q%d", a.questionSerial), function: in.id, snapshot: in.lineSnapshot, line: in.line, selected: true}
		a.questions = append(a.questions, d)
	}
	d.text, d.answer, d.status, d.selected = strings.TrimSpace(in.text), "", "下書き", true
	s.notice = "行質問を保存しました。C:一覧・編集・送信"
}

func (s *screen) questionLines(width, height int) []string {
	a := s.agent
	lines := wrapPaneText("行質問 · C:閉じる j/k:選択 Space:送信対象 i:編集 d:削除 p:現在のコード行へ再指定 Enter:選択分を送信\n再指定先は一覧を閉じてコード上で選択してください。", width)
	if s.notice != "" {
		lines = append(lines, wrapPaneText(s.notice, width)...)
	}
	if len(a.questions) == 0 {
		return append(lines, "下書きなし · Cで閉じて a:行質問入力 / ?:これは何？")
	}
	selectedTop := 0
	for i, d := range a.questions {
		if i == a.questionIndex {
			selectedTop = len(lines)
			if a.questionFocus {
				s.explanationTop = selectedTop
			}
		}
		mark, cursor := " ", " "
		if d.selected {
			mark = "x"
		}
		if i == a.questionIndex {
			cursor = "›"
		}
		status := d.status
		if d.stale {
			status = "位置が古いため送信不可 · pで再指定"
		}
		if d.sending {
			status = "送信中"
		}
		body := fmt.Sprintf("%s [%s] %s %s:%d · %s\n質問: %s", cursor, mark, d.id, d.snapshot.file, d.line, status, d.text)
		if d.answer != "" {
			body += "\n回答: " + d.answer
		}
		lines = append(lines, wrapPaneText(body, width)...)
		lines = append(lines, "")
	}
	a.questionFocus = false
	// Allow the selected item to reach the first visible row.
	for len(lines) < selectedTop+height {
		lines = append(lines, "")
	}
	return lines
}

func (s *screen) questionComments() map[int][]string {
	lines := map[int][]string{}
	if s.agent == nil {
		return lines
	}
	for _, d := range s.agent.questions {
		if d.function != s.current || d.stale {
			continue
		}
		text := "利用者 [" + d.id + "]: " + d.text
		if d.sending {
			text += " （送信中）"
		} else if d.answer == "" {
			text += " （" + d.status + "）"
		}
		if d.answer != "" {
			text += "\nエージェント [" + d.id + "]: " + d.answer
		}
		lines[d.line-d.snapshot.line] = append(lines[d.line-d.snapshot.line], text)
	}
	return lines
}

func (s *screen) sendLineQuestions(ctx context.Context) {
	a := s.agent
	if a.cancel != nil {
		s.notice = "生成中です。完了後に送信してください。"
		return
	}
	var questions []explain.LineQuestion
	var drafts []*lineDraft
	for _, d := range a.questions {
		if !d.selected {
			continue
		}
		snapshot, err := snapshotComments(s.p, d.function)
		d.stale = d.stale || err != nil || snapshot != d.snapshot
		if d.stale {
			s.notice = "古い位置の質問があります。pで再指定するか送信対象から外してください。"
			return
		}
		questions = append(questions, explain.LineQuestion{ID: d.id, Function: d.function, File: d.snapshot.file, Line: d.line, Text: d.text})
		drafts = append(drafts, d)
	}
	if len(questions) == 0 {
		s.notice = "送信する質問を選択してください。"
		return
	}
	req, err := explain.Prepare(s.p, questions[0].Function, a.options.purpose, nil, a.options.excludes)
	if err == nil {
		req, err = req.WithLineQuestions(questions)
	}
	if err != nil {
		s.notice = "送信準備に失敗しました（送信していません）: " + err.Error()
		return
	}
	jobCtx, cancel := context.WithTimeout(ctx, a.options.timeout)
	a.cancel = cancel
	for _, d := range drafts {
		d.sending = true
		d.status = "送信中"
	}
	s.notice = "選択した行質問を送信中です。"
	runner, results := a.options.runner, a.results
	go func() {
		defer cancel()
		text, err := req.Explain(jobCtx, runner)
		var answers map[string]string
		if err == nil {
			answers, err = explain.ParseLineAnswers(text, questions)
		}
		results <- agentResult{questions: questions, answers: answers, err: err}
	}()
}

func (s *screen) receiveLineAnswers(result agentResult) {
	for _, q := range result.questions {
		for _, d := range s.agent.questions {
			if d.id != q.ID {
				continue
			}
			d.sending = false
			snapshot, err := snapshotComments(s.p, d.function)
			d.stale = d.stale || err != nil || snapshot != d.snapshot
			if result.err != nil {
				d.status = "送信失敗・Enterで再送可"
			} else {
				d.answer, d.status, d.selected = result.answers[d.id], "回答済み", false
			}
		}
	}
	if result.err != nil {
		s.notice = "行質問に失敗しました。下書きは保持しています: " + result.err.Error()
	} else {
		s.notice = "行質問への回答を保存しました。"
	}
}
