package main

import "slices"

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
		for _, comment := range comments[row] {
			for _, line := range wrapPaneText("AI解説 "+comment, max(1, width)) {
				rows = append(rows, inlineDisplayRow{codeRow: row, comment: true, text: line})
			}
		}
	}
	return rows
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
