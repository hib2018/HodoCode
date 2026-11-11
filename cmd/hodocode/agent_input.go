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
	id, text, partial, escape, status string
}

// agentInputByte must run before the navigation decoder, including on small terminals.
func (s *screen) agentInputByte(ctx context.Context, b byte) bool {
	if s.agent == nil || s.agent.input == nil {
		return false
	}
	a, in := s.agent, s.agent.input
	if b == 3 || b == 4 {
		a.input = nil
		return false // Let the global quit handler receive Ctrl+C/D.
	}
	if in.escape != "" {
		if in.escape == "\x1b" {
			if b == '[' || b == 'O' {
				in.escape += string(b)
			} else {
				a.input = nil
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
		instruction := strings.TrimSpace(in.text)
		if instruction == "" {
			in.status = "指示を入力してください。"
			return true
		}
		a.questions[in.id] = instruction
		a.input = nil
		s.agentKey(ctx, "e")
	case 0x7f, 0x08:
		in.partial = ""
		if len(in.text) > 0 {
			_, size := utf8.DecodeLastRuneInString(in.text)
			in.text = in.text[:len(in.text)-size]
		}
		in.status = ""
	case 0x15:
		in.text, in.partial, in.status = "", "", ""
	default:
		in.partial += string([]byte{b})
		for utf8.FullRuneInString(in.partial) {
			r, size := utf8.DecodeRuneInString(in.partial)
			in.partial = in.partial[size:]
			if r == utf8.RuneError && size == 1 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				continue
			}
			if len(in.text)+size > maxAgentPurpose {
				in.status = "追加指示は8KiB以内にしてください。"
				continue
			}
			in.text += string(r)
			in.status = ""
		}
	}
	return true
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
	if s.agent == nil || s.agent.input == nil || width < 6 || height < 4 {
		return ""
	}
	color := len(colors) > 0 && colors[0]
	in := s.agent.input
	boxWidth := min(76, width)
	inner := boxWidth - 4
	target := in.id
	if slash := strings.LastIndex(target, "/"); slash >= 0 {
		target = target[slash+1:]
	}
	target = "対象: " + target
	if displayWidth(target) > inner {
		target = displaySlice(target, 0, inner-1) + "…"
	}
	text := wrapPaneText("› "+in.text+"▏", inner)
	if in.text == "" {
		text = []string{"› ▏指示を入力してください…"}
	}
	available := max(1, min(8, height-8))
	if len(text) > available {
		text = text[len(text)-available:]
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
	const safety = "Enterで送信前確認へ（まだ送信しません）"
	const footer = "Enter:確認へ  Esc:取消  Ctrl+U:消去"
	lines := []string{"追加指示", target, ""}
	lines = append(lines, text...)
	lines = append(lines, status, safety, footer)
	if height < 11 {
		// Keep the entry and controls useful when the full panel cannot fit.
		lines = []string{"追加指示", text[len(text)-1]}
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
	return out.String()
}
