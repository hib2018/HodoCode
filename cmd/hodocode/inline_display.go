package main

import (
	"slices"
	"strings"
)

// Inline rows are presentation only; every code-facing coordinate stays in s.code.
type inlineDisplayRow struct {
	codeRow int
	comment bool
	text    string
}

func makeInlineDisplay(code []string, comments map[int][]string, width int) []inlineDisplayRow {
	var rows []inlineDisplayRow
	for row := range code {
		rows = append(rows, inlineDisplayRow{codeRow: row})
		if len(comments[row]) > 0 {
			for _, line := range inlineCommentBlock(code[row], comments[row], width) {
				rows = append(rows, inlineDisplayRow{codeRow: row, comment: true, text: line})
			}
		}
	}
	return rows
}

func inlineCommentBlock(code string, comments []string, width int) []string {
	prefix, _, numbered := strings.Cut(code, " │ ")
	gutter := 0
	if numbered {
		gutter = displayWidth(prefix + " │ ")
	}
	indent := strings.Repeat(" ", max(0, min(gutter, width-12)))
	paragraphs := strings.Split(strings.Join(comments, "\n\n"), "\n")
	longest := 0
	for i, raw := range paragraphs {
		paragraphs[i] = displaySlice(raw, 0, displayWidth(raw))
		longest = max(longest, displayWidth(paragraphs[i]))
	}
	frameWidth := max(1, min(width-len(indent)-1, max(11, longest+4)))
	inner := max(1, frameWidth-4)
	var lines []string
	appendLine := func(text string) {
		lines = append(lines, indent+displaySlice(text, 0, min(max(0, width-len(indent)), frameWidth)))
	}
	appendLine("╭─ AI解説 " + strings.Repeat("─", max(0, frameWidth-11)) + "╮")
	for _, raw := range paragraphs {
		line, cells := "", 0
		for _, r := range raw {
			n := runeWidth(r)
			if cells > 0 && cells+n > inner {
				appendLine("│ " + displaySlice(line, 0, inner) + " │")
				line, cells = "", 0
			}
			line += string(r)
			cells += n
		}
		appendLine("│ " + displaySlice(line, 0, inner) + " │")
	}
	appendLine("╰" + strings.Repeat("─", max(0, frameWidth-2)) + "╯")
	return lines
}

func (s *screen) displayRows(width int) []inlineDisplayRow {
	rows := makeInlineDisplay(s.code, s.inlineComments(), width)
	// Preserve the viewport's source anchor when comments toggle or rewrap.
	if s.displayFunction == s.current && !slices.Equal(s.display, rows) && len(s.display) > 0 {
		old := max(0, min(s.top, len(s.display)-1))
		anchor := s.display[old]
		start := old
		for start > 0 && s.display[start-1].codeRow == anchor.codeRow {
			start--
		}
		for i, row := range rows {
			if row.codeRow != anchor.codeRow {
				continue
			}
			if !anchor.comment {
				if row.comment {
					continue
				}
				s.top = i
			} else {
				s.top = i
				for offset := 0; offset < old-start && s.top+1 < len(rows) && rows[s.top+1].codeRow == anchor.codeRow; offset++ {
					s.top++
				}
			}
			break
		}
	}
	s.display, s.displayFunction = rows, s.current
	return rows
}

func codeVisualRow(rows []inlineDisplayRow, codeRow int) int {
	for i, row := range rows {
		if row.codeRow == codeRow && !row.comment {
			return i
		}
	}
	return 0
}

func (s *screen) cursorVisualRow(width int) int {
	return codeVisualRow(s.displayRows(width), s.cursorRow)
}

// History stores a source anchor, not a layout-dependent visual row.
func (s *screen) savedPosition(width int) screenPosition {
	rows := s.displayRows(width)
	position := s.screenPosition
	if len(rows) > 0 {
		position.top = rows[max(0, min(s.top, len(rows)-1))].codeRow
	}
	return position
}

func (s *screen) restoreHistoryTop(width int) {
	top := s.top
	s.display = nil
	s.top = codeVisualRow(s.displayRows(width), top)
}
