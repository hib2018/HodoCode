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

func (a *agentPane) purpose(id string) string {
	parts := []string{a.options.purpose}
	parts = append(parts, a.instructions[id]...)
	return strings.Join(parts, "\n\n")
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
		if len(a.purpose(in.id))+2+len(instruction) > maxAgentPurpose {
			in.status = "目的と追加指示の合計は8KiB以内にしてください。"
			return true
		}
		if a.instructions == nil {
			a.instructions = make(map[string][]string)
		}
		a.instructions[in.id] = append(a.instructions[in.id], instruction)
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
func (s *screen) renderAgentInput(width, height int) string {
	if s.agent == nil || s.agent.input == nil || width < 4 || height < 3 {
		return ""
	}
	in := s.agent.input
	boxWidth := min(76, width)
	inner := boxWidth - 4
	lines := []string{"追加指示（Enter:確認 / Esc:取消）", in.id}
	text := wrapPaneText(in.text+"_", inner)
	available := max(1, min(8, height-6))
	if len(text) > available {
		text = text[len(text)-available:]
	}
	lines = append(lines, text...)
	lines = append(lines, "Ctrl+U:消去 / Ctrl+C,D:終了", in.status)
	if len(lines) > height-2 {
		lines = lines[:height-2]
	}
	x, y := (width-boxWidth)/2+1, (height-len(lines)-2)/2+1
	var out strings.Builder
	row := func(offset int, text string) {
		fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[0m%s", y+offset, x, text)
	}
	row(0, "+"+strings.Repeat("-", boxWidth-2)+"+")
	for i, line := range lines {
		row(i+1, "| "+displaySlice(line, 0, inner)+" |")
	}
	row(len(lines)+1, "+"+strings.Repeat("-", boxWidth-2)+"+")
	return out.String()
}
