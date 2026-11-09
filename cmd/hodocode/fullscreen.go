package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode"

	"github.com/hib2018/HodoCode/internal/analyze"
)

type screenPosition struct {
	current                   string
	selected, top, horizontal int
	explanationTop            int
}

type callHighlight struct {
	row, start, end int
}

type screen struct {
	p    *analyze.Project
	flow *flow
	screenPosition
	history      []screenPosition
	sites        []analyze.CallSite
	highlights   []callHighlight
	code         []string
	syntax       [][]syntaxHighlight
	location     string
	explanations []syntaxExplanation
	agent        *agentPane
}

func newScreen(p *analyze.Project, name string, expansions []string) (*screen, error) {
	if name == "" {
		mains := p.MainFunctions()
		if len(mains) == 0 {
			return nil, fmt.Errorf("package mainのmain関数がありません。--funcで起点を指定してください")
		}
		if len(mains) != 1 {
			return nil, fmt.Errorf("main関数が複数あります。--funcで起点を指定してください: %s", strings.Join(mains, ", "))
		}
		name = mains[0]
	}
	f, err := newFlow(p, name, expansions)
	if err != nil {
		return nil, err
	}
	s := &screen{p: p, flow: f, screenPosition: screenPosition{current: name}}
	s.current, err = p.Resolve(name)
	if err != nil {
		return nil, err
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *screen) refresh() error {
	s.sites = s.flow.sites[s.current]
	s.selected = max(0, min(s.selected, len(s.sites)-1))
	source, err := s.p.Source(s.current)
	if err != nil {
		return err
	}
	fn := s.p.Functions[s.current]
	s.location = fmt.Sprintf("%s:%d-%d", fn.File, fn.Line, fn.End)
	s.code = nil
	s.syntax = sourceHighlights(source, fn.Line)
	s.explanations = sourceExplanations(source)
	s.highlights = make([]callHighlight, len(s.sites))
	offset := 0
	for row, raw := range strings.Split(source, "\n") {
		prefix := fmt.Sprintf("%d │ ", fn.Line+row)
		line := prefix + strings.TrimSuffix(raw, "\r")
		s.code = append(s.code, line)
		for i, site := range s.sites {
			if site.Line != fn.Line+row {
				continue
			}
			column := site.Offset - offset
			if column < 0 || column+len(site.Name) > len(raw) {
				continue
			}
			start := displayWidth(prefix + raw[:column])
			end := displayWidth(prefix + raw[:column+len(site.Name)])
			s.highlights[i] = callHighlight{row: row, start: start, end: end}
		}
		offset += len(raw) + 1
	}
	return nil
}

func (s *screen) reason(site analyze.CallSite) string {
	if site.Unknown {
		return "呼び出し先を静的に特定できない"
	}
	if _, ok := s.p.Functions[site.To]; !ok {
		return "対象範囲内にソースがない"
	}
	return ""
}

func (s *screen) follow(height, width int) {
	if len(s.highlights) == 0 {
		return
	}
	h := s.highlights[s.selected]
	if h.row < s.top {
		s.top = h.row
	}
	if h.row >= s.top+height {
		s.top = h.row - height + 1
	}
	s.top = max(0, min(s.top, max(0, len(s.code)-height)))
	if h.start < s.horizontal {
		s.horizontal = h.start
	}
	if h.end > s.horizontal+width {
		s.horizontal = max(0, h.end-width)
	}
	// A name wider than the viewport is shown from its beginning.
	if h.end-h.start > width {
		s.horizontal = h.start
	}
}

func (s *screen) key(key string, height, codeWidth int) (bool, error) {
	switch key {
	case "q", "\x03", "\x04":
		return true, nil
	case "up", "k", "down", "j":
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		s.selected = max(0, min(len(s.sites)-1, s.selected+delta))
		s.follow(height, codeWidth)
	case "[":
		s.explanationTop = max(0, s.explanationTop-1)
	case "]":
		s.explanationTop++
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
	case "b":
		if len(s.history) > 0 {
			s.screenPosition = s.history[len(s.history)-1]
			s.history = s.history[:len(s.history)-1]
			return false, s.refresh()
		}
	case "\r", "\n":
		if len(s.sites) == 0 {
			return false, nil
		}
		site := s.sites[s.selected]
		if s.reason(site) != "" {
			return false, nil
		}
		if err := s.flow.expand(site.To); err != nil {
			return false, err
		}
		s.history = append(s.history, s.screenPosition)
		s.screenPosition = screenPosition{current: site.To}
		if err := s.refresh(); err != nil {
			return false, err
		}
		s.follow(height, codeWidth)
	}
	return false, nil
}

func (s *screen) codeLine(row, width int, emphasis bool) string {
	line := s.code[row]
	if !emphasis {
		return displaySlice(line, s.horizontal, width)
	}
	var spans []syntaxHighlight
	if row < len(s.syntax) {
		spans = append(spans, s.syntax[row]...)
	}
	for i, h := range s.highlights {
		if h.row != row || h.end <= h.start {
			continue
		}
		style := "\x1b[30;46m"
		if s.reason(s.sites[i]) != "" {
			style = "\x1b[30;43m\x1b[4m"
		}
		if i == s.selected {
			style = "\x1b[97;44m\x1b[1m"
			if s.reason(s.sites[i]) != "" {
				style += "\x1b[4m"
			}
		}
		spans = append(spans, syntaxHighlight{start: h.start, end: h.end, style: style})
	}
	return styledSlice(line, s.horizontal, width, spans)
}

func (s *screen) render(width, height int, emphasis bool) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	if width < 41 || height < 8 {
		b.WriteString(displaySlice("端末を広げてください（41列×8行以上）。qで終了", 0, max(0, width)))
		b.WriteString("\x1b[J")
		return b.String()
	}
	body := height - 3
	codeWidth := screenCodeWidth(width)
	noteWidth := width / 3
	s.top = max(0, min(s.top, max(0, len(s.code)-body)))
	longest := 0
	for _, line := range s.code {
		longest = max(longest, displayWidth(line))
	}
	s.horizontal = max(0, min(s.horizontal, max(0, longest-codeWidth)))
	writeRow := func(n int, text string) {
		fmt.Fprintf(&b, "\x1b[%d;1H", n)
		b.WriteString(displaySlice(text, 0, width))
	}
	writeRow(1, s.location)
	notes := s.paneLines(noteWidth, body)
	s.explanationTop = min(s.explanationTop, max(0, len(notes)-body))
	for i := 0; i < body; i++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", i+2)
		if row := s.top + i; row < len(s.code) {
			b.WriteString(s.codeLine(row, codeWidth, emphasis))
		} else {
			b.WriteString(strings.Repeat(" ", codeWidth))
		}
		b.WriteString(" │ ")
		note := ""
		if index := s.explanationTop + i; index < len(notes) {
			note = notes[index]
		}
		b.WriteString(displaySlice(note, 0, noteWidth))
	}
	status := "呼び出し箇所なし"
	if len(s.sites) > 0 {
		site := s.sites[s.selected]
		status = fmt.Sprintf("%d/%d %s:%d:%d %s", s.selected+1, len(s.sites), s.p.Functions[s.current].File, site.Line, site.Column, site.To)
		if reason := s.reason(site); reason != "" {
			status = fmt.Sprintf("%d/%d %s (%s:%d:%d %s)", s.selected+1, len(s.sites), reason, s.p.Functions[s.current].File, site.Line, site.Column, site.Name)
		}
	}
	writeRow(height-1, status)
	writeRow(height, "e:AI解説 s:構文 []:解説スクロール ↑↓:選択 Enter:進む b:戻る ←→:横 PgUp/Dn:縦 q:終了")
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

func fullScreen(ctx context.Context, p *analyze.Project, name string, expansions []string, in, out *os.File, options agentOptions) error {
	s, err := newScreen(p, name, expansions)
	if err != nil {
		return err
	}
	s.agent = newAgentPane(options)
	_, noColor := os.LookupEnv("NO_COLOR")
	return withTerminal(in, out, func(read func() (byte, bool, error), size func() (int, int, error)) error {
		return screenLoop(ctx, s, out, read, size, !noColor)
	})
}

func screenLoop(ctx context.Context, s *screen, out io.Writer, read func() (byte, bool, error), size func() (int, int, error), bold bool) error {
	defer func() {
		if s.agent != nil && s.agent.cancel != nil {
			s.agent.cancel()
		}
	}()
	decoder := keyDecoder{}
	lastWidth, lastHeight := 0, 0
	dirty := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		if s.receiveAgentResult() {
			dirty = true
		}
		width, height, err := size()
		if err != nil {
			return err
		}
		if width != lastWidth || height != lastHeight {
			if height >= 8 && width >= 41 {
				s.follow(height-3, screenCodeWidth(width))
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
		if s.agentKey(ctx, key) {
			dirty = true
			continue
		}
		done, err := s.key(key, height-3, screenCodeWidth(width))
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		dirty = true
	}
}
