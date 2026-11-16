package main

import (
	"strconv"
	"strings"
)

type mouseEvent struct {
	x, y, delta int // One-based coordinates; delta 0 is a left-button press.
}

// Separate SGR mouse reports before either text input or keyboard decoding.
// Keep incomplete reports across read timeouts (trackpads can arrive in bursts).
type mouseDecoder struct {
	pending  string
	invalid  bool
	timeouts int
}

func (d *mouseDecoder) feed(b byte) (string, *mouseEvent) {
	d.timeouts = 0
	if d.pending == "" {
		if b == 0x1b {
			d.pending = "\x1b"
			return "", nil
		}
		return string([]byte{b}), nil
	}
	if d.pending == "\x1b" || d.pending == "\x1b[" {
		d.pending += string(b)
		if d.pending == "\x1b[" || d.pending == "\x1b[<" || d.pending == "\x1b[M" {
			return "", nil
		}
		text := d.pending
		d.pending = ""
		return text, nil
	}
	// Terminals without SGR support still send three-byte X10 reports.
	if strings.HasPrefix(d.pending, "\x1b[M") {
		if b < 32 {
			*d = mouseDecoder{}
			return d.feed(b)
		}
		d.pending += string([]byte{b})
		if len(d.pending) < 6 {
			return "", nil
		}
		report := d.pending
		*d = mouseDecoder{}
		return "", mouseButtonEvent(int(report[3])-32, int(report[4])-32, int(report[5])-32)
	}
	if b == 3 || b == 4 || b == 0x1b || b == '\r' || b == '\n' ||
		b >= 'a' && b <= 'z' && b != 'm' || b >= 'A' && b <= 'Z' && b != 'M' {
		*d = mouseDecoder{}
		return d.feed(b)
	}
	if b == 'M' || b == 'm' {
		report, invalid := d.pending[3:], d.invalid
		*d = mouseDecoder{}
		fields := strings.Split(report, ";")
		if invalid || b != 'M' || len(fields) != 3 {
			return "", nil
		}
		button, e1 := strconv.Atoi(fields[0])
		x, e2 := strconv.Atoi(fields[1])
		y, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil || x < 1 || y < 1 {
			return "", nil
		}
		return "", mouseButtonEvent(button, x, y)
	}
	if (b < '0' || b > '9') && b != ';' || len(d.pending) >= 64 {
		d.invalid = true
	}
	if !d.invalid {
		d.pending += string(b)
	}
	return "", nil
}

func mouseButtonEvent(button, x, y int) *mouseEvent {
	if x < 1 || y < 1 {
		return nil
	}
	button &^= 4 | 8 | 16 // Shift, Alt and Ctrl do not change wheel direction.
	if button == 0 {
		return &mouseEvent{x: x, y: y}
	}
	if button != 64 && button != 65 {
		return nil
	}
	delta := -1
	if button == 65 {
		delta = 1
	}
	return &mouseEvent{x: x, y: y, delta: delta}
}

func (s *screen) handleMouse(event mouseEvent, width, height int) bool {
	if width < 41 || height < 8 || event.x < 1 || event.x > width || event.y < 2 || event.y > height-2 || (s.tree != nil && s.tree.visible) {
		return false
	}
	body, codeWidth := height-3, screenCodeWidth(width)
	if event.delta == 0 {
		if event.x > codeWidth || s.search.input != nil || s.agent != nil && s.agent.input != nil {
			return false
		}
		return s.clickCode(s.top+event.y-2, s.horizontal+event.x-1)
	}
	if event.x <= codeWidth {
		s.top = max(0, min(max(0, len(s.code)-body), s.top+event.delta))
		return true
	}
	if event.x <= codeWidth+3 {
		return false
	}
	var notes []string
	if s.agent == nil || !s.agent.visible {
		notes, _ = s.localExplanation(width/3-2, body)
	} else {
		notes, _ = s.paneContent(width/3-2, body)
	}
	s.explanationTop = max(0, min(max(0, len(notes)-body), s.explanationTop+event.delta))
	return true
}

func (s *screen) clickCode(row, cell int) bool {
	if row < 0 || row >= len(s.code) {
		return false
	}
	prefix, raw, _ := strings.Cut(s.code[row], " │ ")
	column, position := 0, displayWidth(prefix+" │ ")
	for i, r := range []rune(raw) {
		column = i
		n := runeWidth(r)
		if r == '\t' {
			n = 8 - position%8
		}
		if cell < position+n {
			break
		}
		position += n
	}
	s.cursorRow, s.cursorColumn = row, column
	if index := s.callAtCursor(); index >= 0 {
		s.selected = index
	}
	if s.agent == nil || !s.agent.visible {
		s.explanationTop = 0
	}
	s.notice = ""
	s.syncVariable()
	return true
}
