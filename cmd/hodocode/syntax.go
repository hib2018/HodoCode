package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

type syntaxHighlight struct {
	start, end int
	style      string
}

func sourceHighlights(source string, firstLine int) [][]syntaxHighlight {
	lines := strings.Split(source, "\n")
	result := make([][]syntaxHighlight, len(lines))
	file := token.NewFileSet().AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, []byte(source), func(token.Position, string) {}, scanner.ScanComments)
	type span struct {
		start, end int
		style      string
	}
	var tokens []span
	for {
		pos, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		style := ""
		switch {
		case tok.IsKeyword():
			style = "\x1b[35m"
		case tok == token.STRING || tok == token.CHAR:
			style = "\x1b[32m"
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			style = "\x1b[36m"
		case tok == token.COMMENT:
			style = "\x1b[90m"
		}
		if style == "" {
			continue
		}
		start := file.Offset(pos)
		end := start + len(lit)
		// The scanner removes CR bytes from raw strings and comments.
		switch {
		case tok == token.STRING && source[start] == '`':
			if n := strings.IndexByte(source[start+1:], '`'); n >= 0 {
				end = start + n + 2
			}
		case tok == token.COMMENT && strings.HasPrefix(source[start:], "/*"):
			if n := strings.Index(source[start+2:], "*/"); n >= 0 {
				end = start + n + 4
			}
		case tok == token.COMMENT:
			end = len(source)
			if n := strings.IndexByte(source[start:], '\n'); n >= 0 {
				end = start + n
			}
		}
		tokens = append(tokens, span{start, end, style})
	}
	offset := 0
	for row, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		prefix := fmt.Sprintf("%d │ ", firstLine+row)
		for _, tok := range tokens {
			start, end := max(offset, tok.start), min(offset+len(line), tok.end)
			if start < end {
				result[row] = append(result[row], syntaxHighlight{
					start: displayWidth(prefix + line[:start-offset]),
					end:   displayWidth(prefix + line[:end-offset]),
					style: tok.style,
				})
			}
		}
		offset += len(raw) + 1
	}
	return result
}

// Later spans override earlier ones, so call markers take priority over syntax.
func styledSlice(line string, start, width int, spans []syntaxHighlight) string {
	end := start + width
	bounds := []int{start, end}
	for _, h := range spans {
		if h.start < end && h.end > start {
			bounds = append(bounds, max(start, h.start), min(end, h.end))
		}
	}
	sort.Ints(bounds)
	var b strings.Builder
	for i := 1; i < len(bounds); i++ {
		left, right := bounds[i-1], bounds[i]
		if left == right {
			continue
		}
		style := ""
		for _, h := range spans {
			if h.start <= left && h.end >= right {
				style = h.style
			}
		}
		b.WriteString(style)
		b.WriteString(displaySlice(line, left, right-left))
		if style != "" {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}
