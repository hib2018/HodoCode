package main

import "strings"

func (s *screen) panePercent() int {
	if s.notePercent == 0 {
		return 33
	}
	return s.notePercent
}

func (s *screen) codeWidth(width int) int {
	if s.notePercent == 0 {
		return screenCodeWidth(width)
	}
	return width - max(12, min(width-23, width*s.notePercent/100)) - 3
}

// The prompt shares the right pane; only line-question drafts use a modal.
func (s *screen) chatPrompt(width, body int) ([]string, int, int) {
	a := s.agent
	if a == nil || !a.visible || a.lineReview {
		return nil, 0, 0
	}
	target := s.current
	if a.input != nil && a.input.line == 0 {
		target = a.input.id
	}
	if slash := strings.LastIndex(target, "/"); slash >= 0 {
		target = target[slash+1:]
	}
	status := "対象: " + target
	if a.unread {
		status += " · 新着 f:末尾"
	} else if !a.followTail {
		status += " · f:末尾"
	}
	lines := []string{status}
	if a.input == nil || a.input.line > 0 {
		prompt := "› i:質問"
		if a.cancel != nil {
			prompt = "生成中… x:中断"
		}
		return append(lines, prompt), 1, 2
	}
	input, row, column := a.input.promptLines(width, max(1, min(6, body/2-1)))
	lines = append(lines, input...)
	if a.input.status != "" && len(lines) < body-1 {
		lines = append(lines, "! "+a.input.status)
	}
	return lines, row + 1, column
}
