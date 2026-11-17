package main

import (
	"fmt"
	"strings"

	"github.com/hib2018/HodoCode/internal/analyze"
)

type variableHighlight struct {
	callHighlight
	offset int
}

func (s *screen) variableAtCursor() int {
	if s.cursorRow < 0 || s.cursorRow >= len(s.code) {
		return -1
	}
	cell := s.cursorCell()
	for _, h := range s.variables {
		if h.row == s.cursorRow && cell >= h.start && cell < h.end {
			return h.offset
		}
	}
	return -1
}

func (s *screen) syncVariable() {
	offset := s.variableAtCursor()
	if offset != s.variableOffset {
		s.variableOffset, s.variableTop = offset, 0
	}
}

func (s *screen) variableSpans(row int) []syntaxHighlight {
	offset := s.variableAtCursor()
	if offset < 0 {
		return nil
	}
	id := s.definitions[offset].VariableID
	if id == 0 {
		return nil
	}
	var spans []syntaxHighlight
	for _, h := range s.variables {
		if h.row == row && s.definitions[h.offset].VariableID == id {
			spans = append(spans, syntaxHighlight{start: h.start, end: h.end, style: "\x1b[1;30;46m"})
		}
	}
	return spans
}

func variableLines(definition analyze.Definition, width int) []string {
	var text []string
	// The first snippet is the variable declaration, not its element's type.
	if len(definition.Snippets) > 0 {
		snippet := definition.Snippets[0]
		text = append(text, fmt.Sprintf("%s:%d", snippet.File, snippet.Line))
		for row, raw := range strings.Split(snippet.Source, "\n") {
			text = append(text, fmt.Sprintf("%d │ %s", snippet.Line+row, strings.TrimSuffix(raw, "\r")))
		}
	} else {
		text = append(text, "定義を取得できません", definition.Reason)
	}
	return wrapPaneText(strings.Join(text, "\n"), width)
}

func (s *screen) renderVariable(width, height int, color bool) string {
	s.syncVariable()
	if s.variableOffset < 0 || s.search.input != nil || s.search.query != "" ||
		s.tree != nil && s.tree.visible || s.agent != nil && s.agent.input != nil {
		return ""
	}
	body, codeWidth := height-3, s.codeWidth(width)
	cursor := s.cursorVisualRow(codeWidth) - s.top
	if cursor < 0 || cursor >= body {
		return ""
	}
	above, below := cursor, body-cursor-1
	available := max(above, below)
	if available < 2 {
		return ""
	}
	popupWidth := min(codeWidth, 76)
	inner := popupWidth - 2
	lines := variableLines(s.definitions[s.variableOffset], inner)
	bordered, overhead := available >= 4, 1
	if bordered {
		overhead = 3
	}
	rows := min(len(lines), available-overhead)
	s.variableTop = max(0, min(s.variableTop, len(lines)-rows))
	popupHeight := rows + overhead
	y := cursor + 3
	if above > below {
		y = cursor + 2 - popupHeight
	}
	x := max(1, min(s.cursorCell()-s.horizontal+1, codeWidth-popupWidth+1))
	var b strings.Builder
	write := func(row int, text string) {
		fmt.Fprintf(&b, "\x1b[%d;%dH", y+row, x)
		b.WriteString(uiPaint(text, "38;5;252;48;5;237", color))
	}
	border := "+" + strings.Repeat("-", inner) + "+"
	start := 0
	if bordered {
		write(0, border)
		start = 1
	}
	title := fmt.Sprintf("定義 LOCAL { / } %d/%d", s.variableTop+1, max(1, len(lines)-rows+1))
	write(start, "│"+displaySlice(title, 0, inner)+"│")
	for i := 0; i < rows; i++ {
		write(start+i+1, "│"+displaySlice(lines[s.variableTop+i], 0, inner)+"│")
	}
	if bordered {
		write(rows+2, border)
	}
	return b.String()
}
