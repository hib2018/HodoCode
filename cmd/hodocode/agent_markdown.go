package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var markdownList = regexp.MustCompile(`^( *)([-+*]|[0-9]+[.)]) +`)
var markdownHeading = regexp.MustCompile(`^(#{1,6}) +(.+)$`)
var markdownRule = regexp.MustCompile(`^:?-{3,}:?$`)

type markdownRun struct{ text, style string }
type markdownOutput struct {
	lines []string
	spans [][]syntaxHighlight
}

// Keep escapes out of the text: only trusted styles reach styledSlice.
func (out *markdownOutput) wrap(runs []markdownRun, prefix, continuation string, width int) {
	width = max(1, width)
	// A narrow pane must still leave room for a wide rune.
	prefix = displaySlice(prefix, 0, min(displayWidth(prefix), max(0, width-2)))
	continuation = displaySlice(continuation, 0, min(displayWidth(continuation), max(0, width-2)))
	line, cells := prefix, displayWidth(prefix)
	var spans []syntaxHighlight
	flush := func() {
		out.lines = append(out.lines, line)
		out.spans = append(out.spans, spans)
		line, cells, spans = continuation, displayWidth(continuation), nil
	}
	for _, run := range runs {
		for _, r := range run.text {
			n := runeWidth(r)
			if n > width { // A two-cell rune cannot fit in a one-cell pane.
				r, n = '�', 1
			}
			if cells+n > width {
				flush()
			}
			start := cells
			line += string(r)
			cells += n
			if run.style != "" {
				if len(spans) > 0 && spans[len(spans)-1].end == start && spans[len(spans)-1].style == run.style {
					spans[len(spans)-1].end = cells
				} else {
					spans = append(spans, syntaxHighlight{start, cells, run.style})
				}
			}
		}
	}
	flush()
}

// shortcut: this is the terminal's Markdown subset, not a CommonMark/HTML renderer;
// use a full parser if reference links or embedded HTML become required.
func markdownInline(text, style string, depth int) []markdownRun {
	if depth > 16 {
		return []markdownRun{{text, style}}
	}
	original := text
	var runs []markdownRun
	plain := func(s string) {
		if s != "" {
			runs = append(runs, markdownRun{s, style})
		}
	}
	for len(text) > 0 {
		if text[0] == '\\' && len(text) > 1 && strings.ContainsRune("\\`*_{}[]()#+-.!>|", rune(text[1])) {
			plain(text[1:2])
			text = text[2:]
			continue
		}
		if text[0] == '`' {
			n := len(text) - len(strings.TrimLeft(text, "`"))
			if end := markdownTicksEnd(text, n); end >= 0 {
				runs = append(runs, markdownRun{"`" + text[n:end] + "`", style + "\x1b[7m"})
				text = text[end+n:]
				continue
			}
		}
		if text[0] == '[' {
			if mid := strings.Index(text, "]("); mid > 0 {
				if end := markdownLinkEnd(text[mid+2:]); end >= 0 {
					runs = append(runs, markdownInline(text[1:mid], style+"\x1b[4m", depth+1)...)
					plain(" (" + text[mid+2:mid+2+end] + ")")
					text = text[mid+3+end:]
					continue
				}
			}
		}
		matched := false
		for _, d := range []struct{ mark, code string }{{"***", "\x1b[1;3m"}, {"___", "\x1b[1;3m"}, {"**", "\x1b[1m"}, {"__", "\x1b[1m"}, {"*", "\x1b[3m"}, {"_", "\x1b[3m"}} {
			if !strings.HasPrefix(text, d.mark) {
				continue
			}
			after, _ := utf8.DecodeRuneInString(text[len(d.mark):])
			before, _ := utf8.DecodeLastRuneInString(original[:len(original)-len(text)])
			if unicode.IsSpace(after) || (d.mark[0] == '_' && (unicode.IsLetter(before) || unicode.IsDigit(before))) {
				continue
			}
			if end := markdownEmphasisEnd(text, d.mark); end > 0 {
				last, _ := utf8.DecodeLastRuneInString(text[len(d.mark) : len(d.mark)+end])
				next, _ := utf8.DecodeRuneInString(text[2*len(d.mark)+end:])
				if unicode.IsSpace(last) || (d.mark[0] == '_' && (unicode.IsLetter(next) || unicode.IsDigit(next))) {
					continue
				}
				runs = append(runs, markdownInline(text[len(d.mark):len(d.mark)+end], style+d.code, depth+1)...)
				text = text[2*len(d.mark)+end:]
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Copy a complete rune, including Japanese text, without interpreting it.
		n := 1
		for n < len(text) && text[n]&0xc0 == 0x80 {
			n++
		}
		plain(text[:n])
		text = text[n:]
	}
	return runs
}

// Backtick runs close only runs of the same length.
func markdownTicksEnd(text string, count int) int {
	for i := count; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		n := len(text[i:]) - len(strings.TrimLeft(text[i:], "`"))
		if n == count {
			return i
		}
		i += n
	}
	return -1
}

func markdownEmphasisEnd(text, mark string) int {
	for i := len(mark); i < len(text); {
		if text[i] == '\\' {
			i += 2
			continue
		}
		if text[i] == '`' {
			n := len(text[i:]) - len(strings.TrimLeft(text[i:], "`"))
			if end := markdownTicksEnd(text[i:], n); end >= 0 {
				i += end + n
				continue
			}
			i += n
			continue
		}
		if text[i] != mark[0] {
			i++
			continue
		}
		n := len(text[i:]) - len(strings.TrimLeft(text[i:], mark[:1]))
		// Double delimiters inside italic belong to nested bold text.
		if n >= len(mark) && !(len(mark) == 1 && n%2 == 0) {
			return i + n - 2*len(mark)
		}
		i += n
	}
	return -1
}

func markdownLinkEnd(text string) int {
	depth := 0
	escaped := false
	for i, r := range text {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			escaped = true
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func markdownCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var b strings.Builder
	code := 0
	escaped := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && code == 0 {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if r == '`' {
			n := 1
			for i+n < len(runes) && runes[i+n] == '`' {
				n++
			}
			if code == 0 {
				code = n
			} else if code == n {
				code = 0
			}
			b.WriteString(strings.Repeat("`", n))
			i += n - 1
			continue
		}
		if r == '|' && code == 0 {
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		} else {
			b.WriteRune(r)
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

func markdownTable(raw []string, width int, out *markdownOutput) int {
	if len(raw) < 2 || !strings.Contains(raw[0], "|") {
		return 0
	}
	header, rule := markdownCells(raw[0]), markdownCells(raw[1])
	if len(header) != len(rule) {
		return 0
	}
	for _, c := range rule {
		if !markdownRule.MatchString(c) {
			return 0
		}
	}
	rows := [][]string{header}
	consumed := 2
	for consumed < len(raw) && strings.Contains(raw[consumed], "|") && strings.TrimSpace(raw[consumed]) != "" {
		trim := strings.TrimSpace(raw[consumed])
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			break
		}
		rows = append(rows, markdownCells(raw[consumed]))
		consumed++
	}
	columns := len(header)
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	render := func(s string) string {
		var b strings.Builder
		for _, r := range markdownInline(s, "", 0) {
			b.WriteString(r.text)
		}
		return b.String()
	}
	for _, row := range rows {
		for c, v := range row {
			widths[c] = max(widths[c], displayWidth(render(v)))
		}
	}
	total := 3 * (columns - 1)
	for _, w := range widths {
		total += w
	}
	if total <= width {
		for i, row := range rows {
			var runs []markdownRun
			for c, w := range widths {
				if c > 0 {
					runs = append(runs, markdownRun{" │ ", ""})
				}
				value := ""
				if c < len(row) {
					value = row[c]
				}
				style := ""
				if i == 0 {
					style = "\x1b[1m"
				}
				runs = append(runs, markdownInline(value, style, 0)...)
				runs = append(runs, markdownRun{strings.Repeat(" ", w-displayWidth(render(value))), ""})
			}
			out.wrap(runs, "", "", width)
			if i == 0 {
				out.wrap([]markdownRun{{strings.Repeat("─", total), ""}}, "", "", width)
			}
		}
	} else {
		// Vertical records keep every cell readable even on narrow panes.
		for i, row := range rows[1:] {
			out.wrap([]markdownRun{{fmt.Sprintf("行 %d", i+1), "\x1b[1m"}}, "", "", width)
			for c := 0; c < columns; c++ {
				label := fmt.Sprintf("列%d", c+1)
				if c < len(header) {
					label = header[c]
				}
				value := ""
				if c < len(row) {
					value = row[c]
				}
				runs := markdownInline(label+": ", "\x1b[1m", 0)
				runs = append(runs, markdownInline(value, "", 0)...)
				out.wrap(runs, "", "  ", width)
			}
		}
		if len(rows) == 1 {
			for _, cell := range header {
				out.wrap(markdownInline(cell, "\x1b[1m", 0), "", "", width)
			}
		}
	}
	return consumed
}

func markdownLines(text string, width int) ([]string, [][]syntaxHighlight) {
	// Sanitize before parsing, but preserve spaces and blank code lines.
	raw := strings.Split(text, "\n")
	for i, line := range raw {
		raw[i] = displaySlice(line, 0, displayWidth(line))
	}
	var out markdownOutput
	fence, fenceGutter, fenceIndent := "", "", ""
	fenceLen, fenceQuotes := 0, 0
	listIndent := ""
	for i := 0; i < len(raw); i++ {
		line := raw[i]
		prefix := ""
		quotes := 0
		// Do not interpret quote markers inside an ordinary code fence.
		for (fence == "" || quotes < fenceQuotes) && strings.HasPrefix(strings.TrimLeft(line, " "), ">") {
			line = strings.TrimLeft(line, " ")
			line = strings.TrimPrefix(line, ">")
			line = strings.TrimPrefix(line, " ")
			prefix += "│ "
			quotes++
		}
		continuation := prefix
		if fence == "" {
			if match := markdownList.FindStringSubmatch(line); match != nil {
				bullet := match[2]
				if bullet == "-" || bullet == "+" || bullet == "*" {
					bullet = "•"
				}
				indent := match[1]
				prefix += indent + bullet + " "
				listIndent = indent + strings.Repeat(" ", displayWidth(bullet)+1)
				continuation += listIndent
				line = line[len(match[0]):]
			} else if listIndent != "" && strings.HasPrefix(line, listIndent) {
				continuation += listIndent
			} else if strings.TrimSpace(line) != "" {
				listIndent = ""
			}
		} else {
			line = strings.TrimPrefix(line, fenceIndent)
		}
		trim := strings.TrimSpace(line)
		marker, n := "", 0
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			marker = trim[:1]
			n = len(trim) - len(strings.TrimLeft(trim, marker))
		}
		if fence != "" {
			if marker == fence && n >= fenceLen && strings.TrimSpace(trim[n:]) == "" {
				out.wrap([]markdownRun{{"└─", "\x1b[90m"}}, fenceGutter, fenceGutter, width)
				fence = ""
			} else {
				out.wrap([]markdownRun{{line, "\x1b[36m"}}, fenceGutter+"│ ", fenceGutter+"│ ", width)
			}
			continue
		}
		if marker != "" {
			fence, fenceLen, fenceQuotes = marker, n, quotes
			fenceGutter, fenceIndent = continuation, listIndent
			out.wrap([]markdownRun{{"┌─ " + strings.TrimSpace(trim[n:]), "\x1b[90m"}}, prefix, continuation, width)
			continue
		}
		if used := markdownTable(raw[i:], width, &out); used > 0 {
			i += used - 1
			listIndent = ""
			continue
		}
		style := ""
		if match := markdownHeading.FindStringSubmatch(line); match != nil {
			prefix += match[1] + " "
			continuation += strings.Repeat(" ", len(match[1])+1)
			line, style = match[2], "\x1b[1m"
		}
		out.wrap(markdownInline(strings.TrimRight(line, " "), style, 0), prefix, continuation, width)
	}
	return out.lines, out.spans
}
