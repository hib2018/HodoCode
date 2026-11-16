package main

import (
	"fmt"

	"github.com/hib2018/HodoCode/internal/analyze"
)

const staleCommentsAdvice = "ソースが変わったため古い行解説を非表示にしました。E で再生成してください。"

// The unredacted snapshot stays local; prompts continue to use explain's redaction.
type commentSnapshot struct {
	file      string
	line, end int
	source    string
}

type functionComments struct {
	snapshot commentSnapshot
	lines    map[int][]string
	stale    bool
}

func snapshotComments(p *analyze.Project, id string) (commentSnapshot, error) {
	source, err := p.Source(id)
	if err != nil {
		return commentSnapshot{}, err
	}
	fn := p.Functions[id]
	return commentSnapshot{fn.File, fn.Line, fn.End, source}, nil
}

func (s *screen) inlineComments() map[int][]string {
	if s.agent == nil || s.agent.commentsHidden {
		return nil
	}
	comments := s.agent.comments[s.current]
	if comments.stale {
		return nil
	}
	return comments.lines
}

func (s *screen) storeComments(result *agentResult) {
	lines := make(map[int][]string)
	for _, comment := range result.comments {
		end := comment.EndLine
		if end == 0 {
			end = comment.Line
		}
		if comment.File != result.snapshot.file || comment.Function != result.id || comment.Line < result.snapshot.line || end < comment.Line || end > result.snapshot.end {
			result.text += fmt.Sprintf("\n\n配置を確認できない行解説（%s / %s:%d-%d）:\n%s", comment.Function, comment.File, comment.Line, end, comment.Text)
			continue
		}
		row := end - result.snapshot.line
		lines[row] = append(lines[row], comment.Text)
	}
	current, err := snapshotComments(s.p, result.id)
	stale := err != nil || current != result.snapshot
	s.agent.comments[result.id] = functionComments{snapshot: result.snapshot, lines: lines, stale: stale}
}

func (a *agentPane) revalidateComments(p *analyze.Project) {
	for id, comments := range a.comments {
		snapshot, err := snapshotComments(p, id)
		comments.stale = comments.stale || err != nil || snapshot != comments.snapshot
		a.comments[id] = comments
	}
}
