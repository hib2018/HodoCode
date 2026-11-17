package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/hib2018/HodoCode/internal/sourcelex"
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

func languageHighlights(language, source string, firstLine int) [][]syntaxHighlight {
	if language == "" || language == "go" {
		return sourceHighlights(source, firstLine)
	}
	lines := strings.Split(source, "\n")
	result := make([][]syntaxHighlight, len(lines))
	tokens, err := sourcelex.Scan(language, source)
	if err != nil {
		return result
	}
	styles := map[string]string{
		"keyword": "\x1b[35m", "string": "\x1b[32m", "number": "\x1b[36m", "comment": "\x1b[90m",
	}
	offset := 0
	for row, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		prefix := fmt.Sprintf("%d │ ", firstLine+row)
		for _, tok := range tokens {
			kind := tok.Kind
			if kind == "identifier" && languageKeyword(language, tok.Text) {
				kind = "keyword"
			}
			if styles[kind] == "" {
				continue
			}
			start, end := max(offset, tok.Start), min(offset+len(line), tok.End)
			if start < end {
				result[row] = append(result[row], syntaxHighlight{
					start: displayWidth(prefix + line[:start-offset]),
					end:   displayWidth(prefix + line[:end-offset]),
					style: styles[kind],
				})
			}
		}
		offset += len(raw) + 1
	}
	return result
}

func languageKeyword(language, text string) bool {
	if language == "" || language == "go" {
		return token.Lookup(text).IsKeyword()
	}
	words := map[string]string{
		"javascript": "await break case catch class const continue debugger default delete do else export extends false finally for function if import in instanceof let new null of return static super switch this throw true try typeof var void while with yield async",
		"typescript": "abstract any as asserts await boolean break case catch class const constructor continue debugger declare default delete do else enum export extends false finally for from function get if implements import in infer instanceof interface is keyof let module namespace never new null number of private protected public readonly require return satisfies set static string super switch symbol this throw true try type typeof undefined unique unknown var void while with yield async",
		"python":     "False None True and as assert async await break case class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield",
		"zig":        "addrspace align allowzero and anyframe anytype asm async await break callconv catch comptime const continue defer else enum errdefer error export extern fn for if inline linksection noalias noinline nosuspend opaque or orelse packed pub resume return section struct suspend switch test threadlocal try union unreachable usingnamespace var volatile while",
		"c":          "auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while _Alignas _Alignof _Atomic _Bool _Complex _Generic _Imaginary _Noreturn _Static_assert _Thread_local",
		"rust":       "Self as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self static struct super trait true type unsafe use where while",
	}[language]
	return strings.Contains(" "+words+" ", " "+text+" ")
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
