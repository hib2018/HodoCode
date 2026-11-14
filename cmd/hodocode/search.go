package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type searchMatch struct {
	callHighlight
	column int
}

type codeSearch struct {
	query    string
	matches  []searchMatch
	selected int
	input    *agentInput
}

func (s *screen) findCode(query string, height, width int) {
	s.search = codeSearch{query: query, selected: -1}
	if query == "" {
		return
	}
	for row, line := range s.code {
		prefix, raw, _ := strings.Cut(line, " │ ")
		for offset := 0; offset < len(raw); {
			n := strings.Index(raw[offset:], query)
			if n < 0 {
				break
			}
			start := offset + n
			s.search.matches = append(s.search.matches, searchMatch{
				callHighlight: callHighlight{row: row, start: displayWidth(prefix + " │ " + raw[:start]), end: displayWidth(prefix + " │ " + raw[:start+len(query)])},
				column:        utf8.RuneCountInString(raw[:start]),
			})
			_, size := utf8.DecodeRuneInString(raw[start:])
			offset = start + size
		}
	}
	for i, m := range s.search.matches {
		if m.row > s.cursorRow || m.row == s.cursorRow && m.column >= s.cursorColumn {
			s.search.selected = i - 1
			break
		}
	}
	s.moveSearch(1, height, width)
}

func (s *screen) moveSearch(delta, height, width int) {
	if len(s.search.matches) == 0 {
		return
	}
	s.search.selected = (s.search.selected + delta + len(s.search.matches)) % len(s.search.matches)
	m := s.search.matches[s.search.selected]
	s.cursorRow, s.cursorColumn = m.row, m.column
	s.explanationTop = 0
	s.followCursor(height, width)
	if m.end > s.horizontal+width {
		s.horizontal = max(0, m.end-width)
	}
	if m.start < s.horizontal || m.end-m.start > width {
		s.horizontal = m.start
	}
}

// Consume UTF-8 input before navigation and agent shortcuts, just like agent input.
func (s *screen) searchInputByte(b byte, height, width int) bool {
	in := s.search.input
	if in == nil {
		return false
	}
	if in.escape != "" {
		if in.escape == "\x1b" {
			if b == '[' || b == 'O' {
				in.escape += string(b)
			} else {
				s.search = codeSearch{}
			}
		} else if b >= 0x40 && b <= 0x7e {
			in.escape = ""
		}
		return true
	}
	switch b {
	case 0x1b:
		in.escape, in.partial = "\x1b", ""
	case '\r', '\n':
		s.findCode(in.text, height, width)
	case 0x7f, 0x08:
		in.partial = ""
		if len(in.text) > 0 {
			_, size := utf8.DecodeLastRuneInString(in.text)
			in.text = in.text[:len(in.text)-size]
		}
	case 0x15:
		in.text, in.partial = "", ""
	default:
		in.partial += string([]byte{b})
		for utf8.FullRuneInString(in.partial) {
			r, size := utf8.DecodeRuneInString(in.partial)
			in.partial = in.partial[size:]
			if r == utf8.RuneError && size == 1 || unicode.IsControl(r) && r != '\t' || unicode.Is(unicode.Cf, r) {
				continue
			}
			in.text += string(r)
		}
	}
	return true
}

func (s *screen) searchInputIdle() bool {
	in := s.search.input
	if in == nil || in.escape == "" {
		return false
	}
	if in.escape == "\x1b" {
		s.search = codeSearch{}
	} else {
		in.escape = ""
	}
	return true
}

func (s *screen) searchPrompt(width int) string {
	text := "/" + s.search.input.text
	return displaySlice(text, max(0, displayWidth(text)-width), width)
}

func (s *screen) searchStatus() string {
	if s.search.input != nil {
		return "/" + s.search.input.text + "▏  Enter:検索 Esc:取消"
	}
	if len(s.search.matches) == 0 {
		return fmt.Sprintf("/%s 該当なし (0/0)", s.search.query)
	}
	return fmt.Sprintf("/%s %d/%d", s.search.query, s.search.selected+1, len(s.search.matches))
}
