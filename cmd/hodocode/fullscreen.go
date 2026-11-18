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
	explanationTop, syntaxTop int
	cursorRow, cursorColumn   int
}

type callHighlight struct {
	row, start, end int
}

type screen struct {
	p    *analyze.Project
	flow *flow
	screenPosition
	history                     []screenPosition
	sites                       []analyze.CallSite
	highlights                  []callHighlight
	code                        []string
	display                     []inlineDisplayRow
	displayFunction             string
	syntax                      [][]syntaxHighlight
	location                    string
	explanations                []syntaxExplanation
	definitions                 map[int]analyze.Definition
	variables                   []variableHighlight
	variableOffset, variableTop int
	sourceOffsets               []int
	agent                       *agentPane
	tree                        *callTree
	search                      codeSearch
	nvimServer                  string
	notice                      string
	notePercent                 int
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
	s.jumpCall()
	return s, nil
}

func (s *screen) refresh() error {
	s.search = codeSearch{}
	s.sites = s.flow.sites[s.current]
	s.selected = max(0, min(s.selected, len(s.sites)-1))
	source, err := s.p.Source(s.current)
	if err != nil {
		return err
	}
	fn := s.p.Functions[s.current]
	s.location = fmt.Sprintf("%s:%d-%d", fn.File, fn.Line, fn.End)
	if sourceURL := s.p.SourceURL(s.current); sourceURL != "" {
		s.location += fmt.Sprintf(" | SHA %s | %s", s.p.CommitSHA, sourceURL)
	}
	s.code = nil
	s.sourceOffsets = nil
	language := s.p.FunctionLanguage(s.current)
	s.syntax = languageHighlights(language, source, fn.Line)
	symbols := map[int]string{}
	for _, site := range s.sites {
		symbols[site.Offset] = "identifier"
		if !s.p.LocalFunction(site.To) && !site.Unknown && standardHelp[site.To].title != "" {
			symbols[site.Offset] = "standard:" + site.To
		}
	}
	s.explanations = languageExplanations(language, source, symbols)
	s.definitions, err = s.p.Definitions(s.current)
	if err != nil {
		return err
	}
	s.highlights = make([]callHighlight, len(s.sites))
	s.variables = nil
	s.variableOffset, s.variableTop = -1, 0
	offset := 0
	for row, raw := range strings.Split(source, "\n") {
		s.sourceOffsets = append(s.sourceOffsets, offset)
		prefix := fmt.Sprintf("%d │ ", fn.Line+row)
		line := prefix + strings.TrimSuffix(raw, "\r")
		s.code = append(s.code, line)
		for pos, definition := range s.definitions {
			column := pos - offset
			if (definition.VariableID == 0 && !definition.Unresolved) || definition.Length == 0 || column < 0 || column+definition.Length > len(strings.TrimSuffix(raw, "\r")) {
				continue
			}
			s.variables = append(s.variables, variableHighlight{
				callHighlight: callHighlight{row: row, start: displayWidth(prefix + raw[:column]), end: displayWidth(prefix + raw[:column+definition.Length])},
				offset:        pos,
			})
		}
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
		reason := site.Reason
		if reason == "" {
			reason = "呼び出し先を静的に特定できない"
		}
		if len(site.Candidates) > 0 {
			reason += "（候補: " + strings.Join(site.Candidates, ", ") + "）"
		}
		return reason
	}
	if err := s.p.EnsureFunction(site.To); err != nil {
		return "対象範囲内に利用できるソースがない"
	}
	return ""
}

func (s *screen) follow(height, width int) {
	rows := s.displayRows(width)
	if len(s.highlights) == 0 {
		return
	}
	h := s.highlights[s.selected]
	row := codeVisualRow(rows, h.row)
	if row < s.top {
		s.top = row
	}
	if row >= s.top+height {
		s.top = row - height + 1
	}
	s.top = max(0, min(s.top, max(0, len(rows)-height)))
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
	if s.search.input != nil || s.search.query != "" {
		switch {
		case key == "\x1b":
			s.search = codeSearch{}
		case s.search.input != nil:
			// Text input is consumed as bytes before navigation decoding.
		case key == "/":
			s.search.input = &agentInput{}
		case key == "\x0e":
			s.moveSearch(1, height, codeWidth)
		case key == "\x10":
			s.moveSearch(-1, height, codeWidth)
		}
		return false, nil
	}
	if a := s.agent; a != nil && a.visible {
		top := s.explanationTop
		defer func() {
			if key != "[" && key != "]" {
				s.explanationTop = top
			}
		}()
	}
	if s.tree != nil && s.tree.visible {
		return s.treeKey(key, height, codeWidth)
	}
	s.notice = ""
	s.syncVariable()
	defer s.syncVariable()
	switch key {
	case "{":
		s.variableTop = max(0, s.variableTop-1)
	case "}":
		if s.variableOffset >= 0 {
			s.variableTop++
		}
	case "/":
		s.search.input = &agentInput{}
	case "\x0e":
		s.moveSearch(1, height, codeWidth)
	case "\x10":
		s.moveSearch(-1, height, codeWidth)
	case "v":
		return false, errOpenNeovim
	case "r":
		if err := s.reload(height, codeWidth); err != nil {
			s.notice = "再読み込みに失敗しました: " + err.Error()
		}
	case "t":
		return false, s.openTree()
	case "q", "\x03", "\x04":
		return true, nil
	case "up", "k":
		s.cursorRow--
		s.explanationTop = 0
		s.followCursor(height, codeWidth)
	case "down", "j":
		s.cursorRow++
		s.explanationTop = 0
		s.followCursor(height, codeWidth)
	case "n", "N":
		delta := 1
		if key == "N" {
			delta = -1
		}
		s.selected = max(0, min(len(s.sites)-1, s.selected+delta))
		s.jumpCall()
		s.explanationTop = 0
		s.follow(height, codeWidth)
	case "[":
		if s.agent != nil && s.agent.visible {
			s.agent.followTail = false
		}
		s.explanationTop = max(0, s.explanationTop-1)
	case "]":
		if s.agent != nil && s.agent.visible {
			s.agent.followTail = false
		}
		s.explanationTop++
	case "pageup", "pagedown":
		delta := height
		if key == "pageup" {
			delta = -height
		}
		rows := s.displayRows(codeWidth)
		visual := codeVisualRow(rows, s.cursorRow)
		s.top = max(0, min(max(0, len(rows)-height), s.top+delta))
		target := max(s.top, min(s.top+height-1, visual+delta))
		target = max(0, min(len(rows)-1, target))
		s.cursorRow = rows[target].codeRow
		top := s.top
		s.followCursor(height, codeWidth)
		// A page may contain only comments; keep scrolling even if its anchor is above it.
		if rows[target].comment {
			s.top = top
		}
		s.explanationTop = 0
	case "left", "h", "right", "l", "0", "$", "w", "B", "g", "G":
		switch key {
		case "left", "h":
			s.cursorColumn--
		case "right", "l":
			s.cursorColumn++
		case "0":
			s.cursorColumn = 0
		case "$":
			s.cursorColumn = len([]rune(s.rawLine(s.cursorRow))) - 1
		case "w", "B":
			s.moveWord(key == "B")
		case "g":
			s.cursorRow, s.cursorColumn = 0, 0
		case "G":
			s.cursorRow, s.cursorColumn = len(s.code)-1, 0
		}
		s.explanationTop = 0
		s.followCursor(height, codeWidth)
	case "b":
		if len(s.history) > 0 {
			s.screenPosition = s.history[len(s.history)-1]
			s.history = s.history[:len(s.history)-1]
			if err := s.refresh(); err != nil {
				return false, err
			}
			s.restoreHistoryTop(codeWidth)
			s.followCursor(height, codeWidth)
			return false, nil
		}
	case "\r", "\n":
		index := s.callAtCursor()
		if index < 0 {
			return false, nil
		}
		s.selected = index
		site := s.sites[index]
		if s.reason(site) != "" {
			return false, nil
		}
		if err := s.flow.expand(site.To); err != nil {
			return false, err
		}
		s.history = append(s.history, s.savedPosition(codeWidth))
		s.screenPosition = screenPosition{current: site.To}
		if err := s.refresh(); err != nil {
			return false, err
		}
		s.jumpCall()
		s.followCursor(height, codeWidth)
	}
	return false, nil
}

func (s *screen) codeLine(row, width int, emphasis bool) string {
	line := s.code[row]
	if !emphasis {
		return displaySlice(line, s.horizontal, width)
	}
	var spans []syntaxHighlight
	prefix, _, _ := strings.Cut(line, " │ ")
	gutterStyle := "\x1b[90m"
	if row == s.cursorRow {
		gutterStyle = "\x1b[1;36m"
	}
	spans = append(spans, syntaxHighlight{start: 0, end: displayWidth(prefix + " │ "), style: gutterStyle})
	if row < len(s.syntax) {
		spans = append(spans, s.syntax[row]...)
	}
	for i, h := range s.highlights {
		if h.row != row || h.end <= h.start {
			continue
		}
		style := "\x1b[36m"
		if s.reason(s.sites[i]) != "" {
			style = "\x1b[33m\x1b[4m"
		}
		if i == s.callAtCursor() {
			style = "\x1b[97;44m\x1b[1m"
			if s.reason(s.sites[i]) != "" {
				style += "\x1b[4m"
			}
		}
		spans = append(spans, syntaxHighlight{start: h.start, end: h.end, style: style})
	}
	spans = append(spans, s.variableSpans(row)...)
	// Search overrides variable/call markers and syntax; the active match has its own color.
	for _, m := range s.search.matches {
		if m.row == row {
			spans = append(spans, syntaxHighlight{start: m.start, end: m.end, style: "\x1b[30;43m"})
		}
	}
	if i := s.search.selected; len(s.search.matches) > 0 && i >= 0 {
		m := s.search.matches[i]
		if m.row == row {
			spans = append(spans, syntaxHighlight{start: m.start, end: m.end, style: "\x1b[1;30;46m"})
		}
	}
	return styledSlice(line, s.horizontal, width, spans)
}

func (s *screen) render(width, height int, emphasis bool) string {
	if s.tree != nil && s.tree.visible {
		position := s.screenPosition
		defer func() { s.screenPosition = position }()
	}
	var b strings.Builder
	b.WriteString("\x1b[H")
	if width < 41 || height < 8 {
		b.WriteString(displaySlice("端末を広げてください（41列×8行以上）。qで終了", 0, max(0, width)))
		b.WriteString("\x1b[J")
		return b.String()
	}
	body := height - 3
	codeWidth := s.codeWidth(width)
	noteWidth := width - codeWidth - 3
	rows := s.displayRows(codeWidth)
	s.top = max(0, min(s.top, max(0, len(rows)-body)))
	longest := 0
	for _, line := range s.code {
		longest = max(longest, displayWidth(line))
	}
	s.horizontal = max(0, min(s.horizontal, max(0, longest-codeWidth)))
	writeRow := func(n int, text, style string) {
		fmt.Fprintf(&b, "\x1b[%d;1H", n)
		b.WriteString(uiPaint(displaySlice(text, 0, width), style, emphasis))
	}
	fmt.Fprintf(&b, "\x1b[1;1H")
	b.WriteString(uiPaint(displaySlice(" "+s.location, 0, codeWidth), "1;38;5;252;48;5;237", emphasis))
	b.WriteString(uiPaint(" │ ", "38;5;240;48;5;237", emphasis))
	b.WriteString(uiPaint(displaySlice(" "+s.paneTitle(), 0, noteWidth), "1;38;5;110;48;5;237", emphasis))
	prompt, _, _ := s.chatPrompt(noteWidth-2, body)
	noteBody := body - len(prompt)
	var notes []string
	var noteSyntax [][]syntaxHighlight
	if s.agent == nil || !s.agent.visible {
		notes, noteSyntax = s.localExplanation(noteWidth-2, body)
	} else {
		notes, noteSyntax = s.paneContent(noteWidth-2, noteBody)
	}
	s.explanationTop = max(0, min(s.explanationTop, max(0, len(notes)-noteBody)))
	for i := 0; i < body; i++ {
		fmt.Fprintf(&b, "\x1b[%d;1H", i+2)
		if row := s.top + i; row < len(rows) {
			line := rows[row]
			if line.comment {
				text := displaySlice(line.text, 0, codeWidth)
				if emphasis {
					indent := len(line.text) - len(strings.TrimLeft(line.text, " "))
					text = styledSlice(line.text, 0, codeWidth, []syntaxHighlight{{start: indent, end: displayWidth(line.text), style: "\x1b[38;5;153;48;5;235m"}})
				}
				b.WriteString(text)
			} else {
				b.WriteString(s.codeLine(line.codeRow, codeWidth, emphasis))
			}
		} else {
			b.WriteString(strings.Repeat(" ", codeWidth))
		}
		b.WriteString(uiPaint(" │ ", "38;5;240", emphasis))
		note := ""
		if index := s.explanationTop + i; i < noteBody && index < len(notes) {
			note = notes[index]
		}
		rendered := paneTextLine(note, noteWidth-2, emphasis)
		if index := s.explanationTop + i; emphasis && i < noteBody && index < len(noteSyntax) && len(noteSyntax[index]) > 0 {
			rendered = styledSlice(note, 0, noteWidth-2, noteSyntax[index])
		}
		if i >= noteBody {
			rendered = paneTextLine(prompt[i-noteBody], noteWidth-2, emphasis)
		}
		b.WriteString(" " + rendered + " ")
	}
	status := "呼び出し箇所なし"
	if len(s.sites) > 0 {
		site := s.sites[s.selected]
		status = fmt.Sprintf("%d/%d %s:%d:%d %s", s.selected+1, len(s.sites), s.p.Functions[s.current].File, site.Line, site.Column, site.To)
		if reason := s.reason(site); reason != "" {
			status = fmt.Sprintf("%d/%d %s (%s:%d:%d %s)", s.selected+1, len(s.sites), reason, s.p.Functions[s.current].File, site.Line, site.Column, site.Name)
		}
	}
	if s.callAtCursor() < 0 {
		status = "カーソル " + s.cursorStatus()
	}
	if s.notice != "" {
		status = s.notice
	}
	mode := " NORMAL  "
	if s.agent != nil && s.agent.lineReview {
		mode = " QUESTIONS "
	}
	if s.agent != nil && s.agent.input != nil {
		mode = " INPUT   "
	}
	if s.search.query != "" || s.search.input != nil {
		status = s.searchStatus()
		mode = " SEARCH  "
	}
	progress := fmt.Sprintf(" %d%% · 解説 %d/%d ", (s.cursorRow+1)*100/max(1, len(s.code)), s.explanationTop+1, max(1, len(notes)-noteBody+1))
	help := s.help(width)
	if s.search.query != "" {
		help = " ^N/P:次/前  /:再検索  Esc:検索終了"
	}
	if s.search.input != nil {
		status = s.searchPrompt(width - displayWidth(mode) - 1)
		progress = ""
		help = " Enter:検索  Esc:取消  Ctrl+U:消去"
	}
	writeRow(height-1, uiBar(mode+status, progress, width), "38;5;252;48;5;237")
	writeRow(height, help, "38;5;245")
	return b.String() + s.renderVariable(width, height, emphasis) + s.renderTree(width, height, emphasis)
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

func fullScreen(ctx context.Context, p *analyze.Project, name string, expansions []string, in, out *os.File, options agentOptions, nvimServer string) error {
	s, err := newScreen(p, name, expansions)
	if err != nil {
		return err
	}
	s.agent = newAgentPane(options)
	s.nvimServer = nvimServer
	defer func() {
		if s.agent != nil && s.agent.cancel != nil {
			s.agent.cancel()
		}
	}()
	_, noColor := os.LookupEnv("NO_COLOR")
	for {
		err := withTerminal(in, out, func(read func() (byte, bool, error), size func() (int, int, error)) error {
			return screenLoop(ctx, s, out, read, size, !noColor)
		})
		if err != errOpenNeovim {
			return err
		}
		cmd, err := s.neovimCommand(ctx)
		if err == nil {
			if s.nvimServer == "" {
				cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, out
				err = cmd.Run()
			} else {
				var output []byte
				output, err = cmd.CombinedOutput()
				if err != nil {
					err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
				}
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			s.notice = "Neovimの起動に失敗しました: " + err.Error()
		} else if s.nvimServer != "" {
			s.notice = "Neovimへ送信しました。r:再読み込み"
		} else {
			s.notice = "Neovimから復帰しました。r:再読み込み"
		}
	}
}

func screenLoop(ctx context.Context, s *screen, out io.Writer, read func() (byte, bool, error), size func() (int, int, error), bold bool) (result error) {
	defer func() {
		if result != errOpenNeovim && s.agent != nil && s.agent.cancel != nil {
			s.agent.cancel()
		}
	}()
	decoder := keyDecoder{}
	mouse := mouseDecoder{}
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
			if lastWidth == 0 && height >= 8 && width >= 41 && (s.tree == nil || !s.tree.visible) {
				s.followCursor(height-3, s.codeWidth(width))
			}
			dirty = true
			lastWidth, lastHeight = width, height
		}
		if dirty {
			frame := "\x1b[?25l" + s.render(width, height, bold)
			cursor := s.cursorVisualRow(s.codeWidth(width))
			if width >= 41 && height >= 8 && (s.tree == nil || !s.tree.visible) &&
				cursor >= s.top && cursor < s.top+height-3 &&
				s.cursorCell() >= s.horizontal && s.cursorCell() < s.horizontal+s.codeWidth(width) {
				frame += fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", cursor-s.top+2, s.cursorCell()-s.horizontal+1)
			}
			if s.search.input != nil && width >= 41 && height >= 8 {
				column := 10 + min(displayWidth("/"+s.search.input.text), width-10)
				frame += fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", height-1, column)
			}
			if s.agent != nil && s.agent.visible && !s.agent.lineReview && s.agent.input != nil && s.agent.input.line == 0 && width >= 41 && height >= 8 {
				prompt, row, column := s.chatPrompt(width-s.codeWidth(width)-5, height-3)
				frame += fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", height-1-len(prompt)+row, s.codeWidth(width)+5+column)
			}
			if overlay := s.renderAgentInput(width, height, bold); overlay != "" {
				frame += "\x1b[?25l" + overlay
			}
			if _, err := io.WriteString(out, frame); err != nil {
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
			if mouse.pending == "\x1b" {
				mouse.pending = ""
				if !s.searchInputByte(0x1b, max(1, height-3), max(1, s.codeWidth(width))) && !s.agentInputByte(ctx, 0x1b) {
					decoder.feed(0x1b)
				}
			} else if mouse.pending != "" {
				// Allow split reports, but recover from a lost/truncated report after 1s.
				mouse.timeouts++
				if mouse.timeouts >= 10 {
					mouse = mouseDecoder{}
				}
				continue
			}
			if decoder.escape == "\x1b" && s.agent != nil && s.agent.lineReview {
				s.agent.lineReview = false
				dirty = true
			}
			if decoder.escape == "\x1b" && s.search.query != "" {
				s.search = codeSearch{}
				dirty = true
			}
			if decoder.escape == "\x1b" && s.tree != nil && s.tree.visible {
				s.tree.visible = false
				dirty = true
			}
			decoder.escape = ""
			if s.searchInputIdle() {
				dirty = true
			}
			if s.agentInputIdle() {
				dirty = true
			}
			continue
		}
		text, event := mouse.feed(b)
		if event != nil {
			dirty = s.handleMouse(*event, width, height) || dirty
		}
		for _, b := range []byte(text) {
			if s.searchInputByte(b, max(1, height-3), max(1, s.codeWidth(width))) {
				dirty = true
				continue
			}
			if s.agentInputByte(ctx, b) {
				dirty = true
				continue
			}
			key := decoder.feed(b)
			if key == "" {
				continue
			}
			if width < 41 || height < 8 {
				if s.search.query == "" && (key == "q" || key == "\x03" || key == "\x04") {
					return nil
				}
				continue
			}
			if s.search.query == "" && s.search.input == nil && (s.tree == nil || !s.tree.visible) && s.agentKey(ctx, key) {
				dirty = true
				continue
			}
			done, err := s.key(key, height-3, s.codeWidth(width))
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			dirty = true
		}
	}
}
