package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (s *screen) rawLine(row int) string {
	_, raw, _ := strings.Cut(s.code[row], " │ ")
	return raw
}

func (s *screen) cursorCell() int {
	prefix, _, _ := strings.Cut(s.code[s.cursorRow], " │ ")
	raw := []rune(s.rawLine(s.cursorRow))
	return displayWidth(prefix + " │ " + string(raw[:min(s.cursorColumn, len(raw))]))
}

func (s *screen) callAtCursor() int {
	cell := s.cursorCell()
	for i, h := range s.highlights {
		if h.row == s.cursorRow && cell >= h.start && cell < h.end {
			return i
		}
	}
	return -1
}

func (s *screen) followCursor(height, width int) {
	s.cursorRow = max(0, min(s.cursorRow, len(s.code)-1))
	s.cursorColumn = max(0, min(s.cursorColumn, max(0, utf8.RuneCountInString(s.rawLine(s.cursorRow))-1)))
	s.top = max(0, min(s.top, s.cursorRow))
	if s.cursorRow >= s.top+height {
		s.top = s.cursorRow - height + 1
	}
	cell := s.cursorCell()
	if cell < s.horizontal {
		s.horizontal = cell
	}
	if cell >= s.horizontal+width {
		s.horizontal = cell - width + 1
	}
	if i := s.callAtCursor(); i >= 0 {
		s.selected = i
	}
}

func (s *screen) jumpCall() {
	if len(s.sites) == 0 {
		return
	}
	site := s.sites[s.selected]
	s.cursorRow = site.Line - s.p.Functions[s.current].Line
	raw := s.rawLine(s.cursorRow)
	s.cursorColumn = utf8.RuneCountInString(raw[:min(len(raw), max(0, site.Column-1))])
}

func (s *screen) moveWord(back bool) {
	row, col := s.cursorRow, s.cursorColumn
	step := func() bool {
		if back {
			if col > 0 {
				col--
				return true
			}
			if row == 0 {
				return false
			}
			row--
			col = max(0, len([]rune(s.rawLine(row)))-1)
			return true
		}
		if col+1 < len([]rune(s.rawLine(row))) {
			col++
			return true
		}
		if row+1 == len(s.code) {
			return false
		}
		row++
		col = 0
		return true
	}
	class := func() int {
		text := []rune(s.rawLine(row))
		if col >= len(text) || unicode.IsSpace(text[col]) {
			return 0
		}
		if unicode.IsLetter(text[col]) || unicode.IsDigit(text[col]) || text[col] == '_' {
			return 1
		}
		return 2
	}
	if back {
		if step() {
			for class() == 0 && step() {
			}
			for {
				oldRow, oldCol, kind := row, col, class()
				if !step() {
					break
				}
				if class() != kind {
					row, col = oldRow, oldCol
					break
				}
			}
		}
	} else {
		kind := class()
		for step() {
			if class() != kind || col == 0 {
				break
			}
		}
		for class() == 0 && step() {
		}
	}
	s.cursorRow, s.cursorColumn = row, col
}

func (s *screen) cursorStatus() string {
	return fmt.Sprintf("%s:%d:%d", s.p.Functions[s.current].File, s.p.Functions[s.current].Line+s.cursorRow, s.cursorColumn+1)
}
