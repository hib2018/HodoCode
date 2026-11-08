package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/hib2018/HodoCode/internal/analyze"
)

type treeRow struct {
	text, id string
	current  bool
}

type screenPosition struct {
	current                         string
	selected, left, top, horizontal int
}

type screen struct {
	p          *analyze.Project
	flow       *flow
	expansions []string
	screenPosition
	history                 []screenPosition
	picking, right, details bool
	rows                    []treeRow
	choices                 []int
	code                    []string
	location                string
}

func newScreen(p *analyze.Project, name string, expansions []string) (*screen, error) {
	s := &screen{p: p, expansions: expansions, picking: name == ""}
	if s.picking {
		for _, id := range functionIDs(p) {
			s.choices = append(s.choices, len(s.rows))
			s.rows = append(s.rows, treeRow{text: id, id: id})
		}
		if len(s.rows) == 0 {
			return nil, fmt.Errorf("no functions found")
		}
		s.current = s.rows[0].id
	} else {
		id, err := p.Resolve(name)
		if err != nil {
			return nil, err
		}
		s.current = id
		f, err := newFlow(p, id, expansions)
		if err != nil {
			return nil, err
		}
		s.flow = f
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *screen) refresh() error {
	if !s.picking {
		s.rows = nil
		s.choices = nil
		visible := []string{s.current}
		if len(s.history) > 0 {
			visible = append(visible, s.history[len(s.history)-1].current)
		}
		var children []analyze.Edge
		external, unknown := 0, 0
		for _, e := range s.flow.edges {
			if e.From != s.current {
				continue
			}
			_, local := s.p.Functions[e.To]
			if e.Unknown {
				unknown++
			} else {
				visible = append(visible, e.To)
				if !local {
					external++
				}
			}
			if s.details || (!e.Unknown && local) {
				children = append(children, e)
			}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].To < children[j].To })
		labels := treeLabels(s.current, visible)
		indent, prefix := "", ""
		if len(s.history) > 0 {
			s.rows = append(s.rows, treeRow{text: labels[s.history[len(s.history)-1].current]})
			indent, prefix = "   ", "└─ "
		}
		s.rows = append(s.rows, treeRow{text: prefix + currentLabel(labels[s.current], false), current: true})
		for i, e := range children {
			branch := "├─ "
			if i == len(children)-1 {
				branch = "└─ "
			}
			row := treeRow{text: indent + branch}
			_, local := s.p.Functions[e.To]
			switch {
			case e.Unknown:
				row.text += e.To + "（呼び出し先不明・選択不可）"
			case !local:
				row.text += labels[e.To] + "（外部ソース・選択不可）"
			default:
				row.text += labels[e.To]
				row.id = e.To
				s.choices = append(s.choices, len(s.rows))
			}
			s.rows = append(s.rows, row)
		}
		if external+unknown > 0 && !s.details {
			s.rows = append(s.rows, treeRow{text: fmt.Sprintf("%s   外部ソース: %d件、呼び出し先不明: %d件（dで詳細表示）", indent, external, unknown)})
		}
	}
	s.selected = max(0, min(s.selected, len(s.choices)-1))
	source, err := s.p.Source(s.current)
	if err != nil {
		return err
	}
	fn := s.p.Functions[s.current]
	s.location = fmt.Sprintf("%s:%d-%d", fn.File, fn.Line, fn.End)
	s.code = nil
	for i, line := range strings.Split(source, "\n") {
		s.code = append(s.code, fmt.Sprintf("%d │ %s", fn.Line+i, strings.TrimSuffix(line, "\r")))
	}
	return nil
}

func (s *screen) selectedRow() int {
	if len(s.choices) == 0 {
		return -1
	}
	return s.choices[s.selected]
}

func (s *screen) follow(height int) {
	if row := s.selectedRow(); row >= 0 {
		if row < s.left {
			s.left = row
		}
		if row >= s.left+height {
			s.left = row - height + 1
		}
	}
	s.left = max(0, min(s.left, max(0, len(s.rows)-height)))
}

func (s *screen) key(key string, height, codeWidth int) (bool, error) {
	switch key {
	case "q", "\x03", "\x04":
		return true, nil
	case "\t":
		s.right = !s.right
		return false, nil
	case "d":
		if !s.picking {
			s.details = !s.details
			if err := s.refresh(); err != nil {
				return false, err
			}
			s.follow(height)
		}
		return false, nil
	}
	if s.right {
		switch key {
		case "up", "k":
			s.top = max(0, s.top-1)
		case "down", "j":
			s.top = min(max(0, len(s.code)-height), s.top+1)
		case "pageup":
			s.top = max(0, s.top-height)
		case "pagedown":
			s.top = min(max(0, len(s.code)-height), s.top+height)
		case "left", "h":
			s.horizontal = max(0, s.horizontal-4)
		case "right", "l":
			longest := 0
			for _, line := range s.code {
				longest = max(longest, displayWidth(line))
			}
			s.horizontal = min(max(0, longest-codeWidth), s.horizontal+4)
		}
		return false, nil
	}
	switch key {
	case "up", "k", "down", "j":
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		s.selected = max(0, min(len(s.choices)-1, s.selected+delta))
		if s.picking {
			s.current = s.rows[s.selectedRow()].id
			s.top = 0
			s.horizontal = 0
			if err := s.refresh(); err != nil {
				return false, err
			}
		}
		s.follow(height)
	case "pageup", "pagedown":
		if key == "pageup" {
			s.left = max(0, s.left-height)
		} else {
			s.left = min(max(0, len(s.rows)-height), s.left+height)
		}
		if row := s.selectedRow(); row < s.left || row >= s.left+height {
			for i, row := range s.choices {
				if row >= s.left && row < s.left+height {
					s.selected = i
					if key == "pagedown" {
						break
					}
				}
			}
		}
		if s.picking {
			s.current = s.rows[s.selectedRow()].id
			s.top, s.horizontal = 0, 0
			return false, s.refresh()
		}
	case "b", "h", "left":
		if len(s.history) > 0 {
			s.screenPosition = s.history[len(s.history)-1]
			s.history = s.history[:len(s.history)-1]
			return false, s.refresh()
		}
	case "\r", "\n", "l", "right":
		row := s.selectedRow()
		// Paging through non-selectable details must not activate an off-screen item.
		if row < s.left || row >= s.left+height {
			return false, nil
		}
		if s.picking {
			// Only Enter confirms the starting point.
			if key != "\r" && key != "\n" {
				return false, nil
			}
			f, err := newFlow(s.p, s.current, s.expansions)
			if err != nil {
				return false, err
			}
			s.flow = f
			s.picking = false
			s.selected = 0
			s.left = 0
			s.top = 0
			s.horizontal = 0
		} else {
			id := s.rows[row].id
			if err := s.flow.expand(id); err != nil {
				return false, err
			}
			s.history = append(s.history, s.screenPosition)
			s.screenPosition = screenPosition{current: id}
		}
		if err := s.refresh(); err != nil {
			return false, err
		}
		s.follow(height)
	}
	return false, nil
}

func (s *screen) render(width, height int, bold bool) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	if width < 41 || height < 8 {
		b.WriteString(displaySlice("端末を広げてください（41列×8行以上）。qで終了", 0, max(0, width)))
		b.WriteString("\x1b[J")
		return b.String()
	}
	leftWidth := (width - 1) / 2
	rightWidth := width - 1 - leftWidth
	body := height - 2
	s.left = max(0, min(s.left, max(0, len(s.rows)-body)))
	s.top = max(0, min(s.top, max(0, len(s.code)-body)))
	// Keep the source viewport valid when the terminal grows.
	longest := 0
	for _, line := range s.code {
		longest = max(longest, displayWidth(line))
	}
	s.horizontal = max(0, min(s.horizontal, max(0, longest-rightWidth)))
	title := "呼び出し階層"
	if s.picking {
		title = "起点を選択（Enterで確定）"
	}
	if !s.right {
		title = "[操作中] " + title
	}
	codeTitle := s.location
	if s.right {
		codeTitle = "[操作中] " + codeTitle
	}
	writeRow := func(n int, left, right string, emphasis bool) {
		fmt.Fprintf(&b, "\x1b[%d;1H", n)
		l := displaySlice(left, 0, leftWidth)
		if bold && emphasis {
			b.WriteString("\x1b[1m")
		}
		b.WriteString(l)
		if bold && emphasis {
			b.WriteString("\x1b[0m")
		}
		b.WriteString("│")
		b.WriteString(displaySlice(right, 0, rightWidth))
	}
	writeRow(1, title, codeTitle, false)
	for i := 0; i < body; i++ {
		left, right := "", ""
		emphasis := false
		if row := s.left + i; row < len(s.rows) {
			marker := "  "
			if row == s.selectedRow() {
				marker = "> "
			}
			left = marker + s.rows[row].text
			emphasis = s.rows[row].current
		}
		if row := s.top + i; row < len(s.code) {
			right = displaySlice(s.code[row], s.horizontal, rightWidth)
		}
		writeRow(i+2, left, right, emphasis)
	}
	fmt.Fprintf(&b, "\x1b[%d;1H", height)
	b.WriteString(displaySlice("Tab:切替 ↑↓/kj:選択・縦移動 Enter/l:進む b/h:戻る 右:h/l横移動 PgUp/Dn:スクロール d:詳細 q:終了", 0, width))
	return b.String()
}

// shortcut: cell widths cover CJK and combining marks, not every emoji grapheme;
// use a grapheme-aware width library if complex emoji alignment becomes necessary.
func runeWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || r >= 0x2e80 && r <= 0xa4cf && r != 0x303f || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe10 && r <= 0xfe19 || r >= 0xfe30 && r <= 0xfe6f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6 || r >= 0x1f300 && r <= 0x1faff || r >= 0x20000 && r <= 0x3fffd) {
		return 2
	}
	return 1
}

func displayWidth(text string) int {
	col := 0
	for _, r := range text {
		if r == '\t' {
			col += 8 - col%8
		} else {
			col += runeWidth(r)
		}
	}
	return col
}

// Clip in terminal cells, expand tabs, and never interpret source text as escapes.
func displaySlice(text string, start, width int) string {
	var b strings.Builder
	col, used := 0, 0
	for _, r := range text {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		if unicode.IsControl(r) && r != '\t' {
			r = '�'
		}
		n := runeWidth(r)
		if r == '\t' {
			n = 8 - col%8
		}
		end := col + n
		if n == 0 {
			if col > start && col <= start+width {
				b.WriteRune(r)
			}
			continue
		}
		if col >= start+width {
			break
		}
		if end > start && col < start+width {
			visible := min(end, start+width) - max(col, start)
			if r == '\t' || col < start || end > start+width {
				b.WriteString(strings.Repeat(" ", visible))
			} else {
				b.WriteRune(r)
			}
			used += visible
		}
		col = end
	}
	b.WriteString(strings.Repeat(" ", max(0, width-used)))
	return b.String()
}

type keyDecoder struct{ escape string }

func (d *keyDecoder) feed(b byte) string {
	if b == '\x03' || b == '\x04' || b == 'q' {
		d.escape = ""
		return string(b)
	}
	if d.escape != "" {
		d.escape += string(b)
		switch d.escape {
		case "\x1b[", "\x1bO", "\x1b[5", "\x1b[6":
			return ""
		case "\x1b[A", "\x1bOA":
			d.escape = ""
			return "up"
		case "\x1b[B", "\x1bOB":
			d.escape = ""
			return "down"
		case "\x1b[C", "\x1bOC":
			d.escape = ""
			return "right"
		case "\x1b[D", "\x1bOD":
			d.escape = ""
			return "left"
		case "\x1b[5~":
			d.escape = ""
			return "pageup"
		case "\x1b[6~":
			d.escape = ""
			return "pagedown"
		}
		d.escape = ""
		return ""
	}
	if b == 0x1b {
		d.escape = "\x1b"
		return ""
	}
	return string(b)
}

func fullScreen(ctx context.Context, p *analyze.Project, name string, expansions []string, in, out *os.File) error {
	s, err := newScreen(p, name, expansions)
	if err != nil {
		return err
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	return withTerminal(in, out, func(read func() (byte, bool, error), size func() (int, int, error)) error {
		return screenLoop(ctx, s, out, read, size, !noColor)
	})
}

func screenLoop(ctx context.Context, s *screen, out io.Writer, read func() (byte, bool, error), size func() (int, int, error), bold bool) error {
	decoder := keyDecoder{}
	lastWidth, lastHeight := 0, 0
	dirty := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		width, height, err := size()
		if err != nil {
			return err
		}
		if width != lastWidth || height != lastHeight {
			if height >= 8 && width >= 41 {
				s.follow(height - 2)
			}
			dirty = true
			lastWidth, lastHeight = width, height
		}
		if dirty {
			if _, err := io.WriteString(out, s.render(width, height, bold)); err != nil {
				return err
			}
			dirty = false
		}
		b, ok, err := read()
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if !ok {
			decoder.escape = ""
			continue
		}
		key := decoder.feed(b)
		if key == "" {
			continue
		}
		if width < 41 || height < 8 {
			if key == "q" || key == "\x03" || key == "\x04" {
				return nil
			}
			continue
		}
		done, err := s.key(key, height-2, width-1-(width-1)/2)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		dirty = true
	}
}
