package main

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxAgentPurpose = 8 * 1024

type agentInput struct {
	draft                             *lineDraft
	lineSnapshot                      commentSnapshot
	line                              int
	cursor                            int
	cursorSet                         bool
	id, text, partial, escape, status string
}

// agentInputByte must run before the navigation decoder, including on small terminals.
func (s *screen) agentInputByte(ctx context.Context, b byte) bool {
	if s.agent == nil || s.agent.input == nil {
		return false
	}
	a, in := s.agent, s.agent.input
	in.initCursor()
	if b == 3 || b == 4 {
		a.input = nil
		return false // Let the global quit handler receive Ctrl+C/D.
	}
	if in.escape != "" {
		if in.escape == "\x1b" {
			switch b {
			case '[', 'O':
				in.escape += string(b)
			case '\r', '\n':
				in.escape = ""
				in.insert("\n")
			default:
				a.input = nil
			}
		} else {
			in.escape += string(b)
			if b >= 0x40 && b <= 0x7e {
				in.editKey(in.escape)
				in.escape = ""
			}
		}
		return true
	}
	switch b {
	case 0x1b:
		in.escape, in.partial = "\x1b", ""
	case '\n':
		in.partial = ""
		in.insert("\n")
	case '\r':
		instruction := strings.TrimSpace(in.text)
		if instruction == "" {
			in.status = "指示を入力してください。"
			return true
		}
		a.input = nil
		if in.line > 0 {
			s.saveLineQuestion(in)
			return true
		}
		if instruction == "/comments" {
			in.status = "/comments の後にコメントの修正指示を入力してください。"
			a.input = in
			return true
		}
		if strings.HasPrefix(instruction, "/comments ") {
			s.startAgentRequest(ctx, in.id, strings.TrimSpace(strings.TrimPrefix(instruction, "/comments ")), true)
		} else {
			s.startAgent(ctx, in.id, instruction)
		}
	case 0x7f, 0x08:
		in.partial = ""
		if in.cursor > 0 {
			_, size := utf8.DecodeLastRuneInString(in.text[:in.cursor])
			in.text = in.text[:in.cursor-size] + in.text[in.cursor:]
			in.cursor -= size
		}
		in.status = ""
	case 0x01:
		in.editKey("\x1b[H")
	case 0x05:
		in.editKey("\x1b[F")
	case 0x15:
		start, _ := in.lineBounds()
		in.text = in.text[:start] + in.text[in.cursor:]
		in.cursor, in.partial, in.status = start, "", ""
	case 0x0b:
		_, end := in.lineBounds()
		in.text = in.text[:in.cursor] + in.text[end:]
		in.partial, in.status = "", ""
	default:
		in.partial += string([]byte{b})
		for utf8.FullRuneInString(in.partial) {
			r, size := utf8.DecodeRuneInString(in.partial)
			in.partial = in.partial[size:]
			if r == utf8.RuneError && size == 1 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				continue
			}
			in.insert(string(r))
		}
	}
	return true
}

// Existing line-question drafts start with their cursor at the end.
func (in *agentInput) initCursor() {
	if !in.cursorSet {
		in.cursor, in.cursorSet = len(in.text), true
	}
}

func (in *agentInput) lineBounds() (int, int) {
	start := strings.LastIndex(in.text[:in.cursor], "\n") + 1
	end := len(in.text)
	if n := strings.IndexByte(in.text[in.cursor:], '\n'); n >= 0 {
		end = in.cursor + n
	}
	return start, end
}

func (in *agentInput) insert(text string) {
	if len(in.text)+len(text) > maxAgentPurpose {
		in.status = "追加指示は8KiB以内にしてください。"
		return
	}
	in.text = in.text[:in.cursor] + text + in.text[in.cursor:]
	in.cursor += len(text)
	in.status = ""
}

func (in *agentInput) editKey(key string) {
	in.partial = ""
	switch key {
	case "\x1b[D", "\x1bOD":
		if in.cursor > 0 {
			_, size := utf8.DecodeLastRuneInString(in.text[:in.cursor])
			in.cursor -= size
		}
	case "\x1b[C", "\x1bOC":
		if in.cursor < len(in.text) {
			_, size := utf8.DecodeRuneInString(in.text[in.cursor:])
			in.cursor += size
		}
	case "\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~":
		in.cursor, _ = in.lineBounds()
	case "\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~":
		_, in.cursor = in.lineBounds()
	case "\x1b[3~":
		if in.cursor < len(in.text) {
			_, size := utf8.DecodeRuneInString(in.text[in.cursor:])
			in.text = in.text[:in.cursor] + in.text[in.cursor+size:]
		}
		in.status = ""
	}
}

// promptLines returns a cursor-visible viewport, including the prompt prefix.
// Cursor row and column are zero-based terminal cells, not byte offsets.
func (in *agentInput) promptLines(width, maxLines int) ([]string, int, int) {
	in.initCursor()
	width, maxLines = max(1, width), max(1, maxLines)
	// Leave one cell for the cursor at logical line endings.
	wrapWidth := max(1, width-1)
	prefix := "› "
	if width < 3 {
		prefix = ""
	}
	text := prefix + in.text
	cursor := len(prefix) + in.cursor
	lines := []string{""}
	row, column, cells := 0, 0, 0
	for offset, r := range text {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			if offset == cursor {
				row, column = len(lines)-1, cells
			}
			continue
		}
		n := runeWidth(r)
		if r != '\n' && n > wrapWidth {
			r, n = '?', 1
		}
		if r != '\n' && cells+n > wrapWidth {
			lines = append(lines, "")
			cells = 0
		}
		if offset == cursor {
			row, column = len(lines)-1, cells
		}
		if r == '\n' {
			lines = append(lines, "")
			cells = 0
		} else {
			lines[len(lines)-1] += string(r)
			cells += n
		}
	}
	if cursor == len(text) {
		if cells == width {
			lines = append(lines, "")
			cells = 0
		}
		row, column = len(lines)-1, cells
	}
	if column >= width {
		row, column = row+1, 0
		if row == len(lines) {
			lines = append(lines, "")
		}
	}
	start := min(max(0, len(lines)-maxLines), row)
	return lines[start:min(len(lines), start+maxLines)], row - start, column
}

// A read timeout distinguishes standalone Escape from an escape sequence.
func (s *screen) agentInputIdle() bool {
	if s.agent == nil || s.agent.input == nil || s.agent.input.escape == "" {
		return false
	}
	if s.agent.input.escape == "\x1b" {
		s.agent.input = nil
	} else {
		s.agent.input.escape = ""
	}
	return true
}

// Append after the main render so the centered box covers the underlying panes.
func (s *screen) renderAgentInput(width, height int, colors ...bool) string {
	if s.agent == nil || s.agent.input == nil || s.agent.input.line == 0 || width < 6 || height < 4 {
		return ""
	}
	color := len(colors) > 0 && colors[0]
	in := s.agent.input
	boxWidth := min(76, width)
	inner := boxWidth - 4
	target := fmt.Sprintf("対象: %s:%d", in.lineSnapshot.file, in.line)
	if displayWidth(target) > inner {
		target = displaySlice(target, 0, inner-1) + "…"
	}
	available := max(1, min(8, height-8))
	text, cursorLine, cursorColumn := in.promptLines(inner, available)
	if in.text == "" {
		text[0] += "指示を入力してください…"
	}
	if height >= 11 {
		for len(text) < 3 {
			text = append(text, "")
		}
	}
	status := ""
	if in.status != "" {
		status = "! " + in.status
	}
	safety := "保存だけでは送信しません · Cで一覧・送信"
	footer := "Enter:保存  Esc:取消  Ctrl+U:消去  Alt+Enter:改行"
	title := "行質問 · 下書き"
	code := ""
	source := strings.Split(in.lineSnapshot.source, "\n")
	lineIndex := in.line - in.lineSnapshot.line
	if lineIndex >= 0 && lineIndex < len(source) {
		code = source[lineIndex]
	}
	cursorRow := 4 + cursorLine
	lines := []string{title, target, code}
	lines = append(lines, text...)
	lines = append(lines, status, safety, footer)
	if height < 11 {
		// Keep the entry and controls useful when the full panel cannot fit.
		lines = []string{title, text[cursorLine]}
		cursorRow = 2
		if status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, safety, footer)
		lines = lines[:min(len(lines), height-2)]
	}
	x, y := (width-boxWidth)/2+1, (height-len(lines)-2)/2+1
	var out strings.Builder
	row := func(offset int, text string) {
		style := "38;5;252;48;5;236"
		if offset == 0 || offset == len(lines)+1 {
			style = "38;5;67;48;5;236"
		}
		if offset == 1 {
			style = "1;38;5;110;48;5;236"
		}
		if offset == len(lines) {
			style = "38;5;245;48;5;236"
		}
		fmt.Fprintf(&out, "\x1b[%d;%dH%s", y+offset, x, uiPaint(text, style, color))
	}
	row(0, "╭"+strings.Repeat("─", boxWidth-2)+"╮")
	for i, line := range lines {
		row(i+1, "│ "+displaySlice(line, 0, inner)+" │")
	}
	row(len(lines)+1, "╰"+strings.Repeat("─", boxWidth-2)+"╯")
	fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[?25h", y+cursorRow, x+2+cursorColumn)
	return out.String()
}
