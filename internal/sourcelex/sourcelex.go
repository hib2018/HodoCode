// Package sourcelex provides conservative, byte-offset source tokens without dependencies.
// It is a lexer, not a syntax validator. Unsupported ambiguous literal syntax fails closed.
package sourcelex

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Token describes a half-open byte range in the original source. Text is exactly
// source[Start:End]. Kind is identifier, string (including character literals),
// number, punct (one rune, or =>, ::, ->, ?.), or comment. Whitespace is omitted.
type Token struct {
	Text       string
	Start, End int
	Kind       string
}

// Scan supports zig, typescript (ts), javascript (js), rust (rs), c, python (py),
// and go (golang). Comments and strings are included. On error no tokens are returned.
// Regex literals have Kind string. Formatted literals are opaque: their inner
// expressions are not indexed. If an interpolation cannot be delimited safely,
// its token conservatively consumes the remainder of the file. Mask is stricter.
func Scan(language, source string) ([]Token, error) {
	return scan(language, source, false)
}

func scan(language, source string, strict bool) ([]Token, error) {
	lang := strings.ToLower(language)
	switch lang {
	case "ts":
		lang = "typescript"
	case "js":
		lang = "javascript"
	case "rs":
		lang = "rust"
	case "py":
		lang = "python"
	case "golang":
		lang = "go"
	}
	switch lang {
	case "zig", "typescript", "javascript", "rust", "c", "python", "go":
	default:
		return nil, fmt.Errorf("sourcelex: unsupported language %q", language)
	}
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("sourcelex: invalid UTF-8")
	}
	// Trigraphs can introduce escaped newlines before C tokenization.
	if lang == "c" && strings.Contains(source, "??/") {
		return nil, fmt.Errorf("sourcelex: unsupported C trigraph")
	}
	s := source
	var offsets []int
	if lang == "c" {
		var b strings.Builder
		for i := 0; i < len(source); {
			if source[i] == '\\' {
				if i+1 < len(source) && source[i+1] == '\n' {
					i += 2
					continue
				}
				if i+2 < len(source) && source[i+1:i+3] == "\r\n" {
					i += 3
					continue
				}
			}
			offsets = append(offsets, i)
			b.WriteByte(source[i])
			i++
		}
		s = b.String()
	}
	var tokens []Token
	add := func(start, end int, kind string) {
		a, b := start, end
		if offsets != nil {
			a, b = offsets[start], offsets[end-1]+1
		}
		tokens = append(tokens, Token{Text: source[a:b], Start: a, End: b, Kind: kind})
	}
	fail := func(i int, reason string) ([]Token, error) {
		if offsets != nil && i < len(offsets) {
			i = offsets[i]
		}
		return nil, fmt.Errorf("sourcelex: %s at byte %d", reason, i)
	}
	js := lang == "javascript" || lang == "typescript"
	for i := 0; i < len(s); {
		start := i
		r, size := utf8.DecodeRuneInString(s[i:])
		if js && i == 0 && r == '\uFEFF' {
			i += size
			continue
		}
		if js && (i == 0 || i == 3 && strings.HasPrefix(s, "\uFEFF")) && strings.HasPrefix(s[i:], "#!") {
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
			add(start, i, "comment")
			continue
		}
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if js && (strings.HasPrefix(s[i:], "<!--") || strings.HasPrefix(s[i:], "-->")) {
			return fail(i, "unsupported legacy JavaScript HTML comment")
		}
		if lang == "python" && s[i] == '#' || lang != "python" && strings.HasPrefix(s[i:], "//") {
			for i < len(s) && s[i] != '\r' && s[i] != '\n' {
				i++
			}
			add(start, i, "comment")
			continue
		}
		if lang != "python" && lang != "zig" && strings.HasPrefix(s[i:], "/*") {
			i += 2
			depth := 1
			for i < len(s) && depth > 0 {
				if lang == "rust" && strings.HasPrefix(s[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(s[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return fail(start, "unterminated comment")
			}
			add(start, i, "comment")
			continue
		}
		if lang == "zig" && strings.HasPrefix(s[i:], `\\`) {
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
			add(start, i, "string")
			continue
		}
		// Rust raw strings may have any number of hash delimiters, with b/c prefixes.
		if lang == "rust" {
			p := i
			if s[p] == 'b' || s[p] == 'c' {
				p++
			}
			if p < len(s) && s[p] == 'r' {
				p++
				hashes := p
				for p < len(s) && s[p] == '#' {
					p++
				}
				if p < len(s) && s[p] == '"' {
					closing := "\"" + s[hashes:p]
					end := strings.Index(s[p+1:], closing)
					if end < 0 {
						return fail(start, "unterminated raw string")
					}
					i = p + 1 + end + len(closing)
					add(start, i, "string")
					continue
				}
			}
		}
		quoteAt := i
		formatted := false
		if lang == "python" && isASCIILetter(s[i]) {
			p := i
			for p < len(s) && strings.ContainsRune("rRbBuUfFtT", rune(s[p])) {
				p++
			}
			if p < len(s) && (s[p] == '\'' || s[p] == '"') {
				prefix := strings.ToLower(s[i:p])
				formatted = strings.ContainsAny(prefix, "ft")
				if formatted {
					if strict || (prefix != "f" && prefix != "fr" && prefix != "rf" && prefix != "t" && prefix != "tr" && prefix != "rt") {
						return fail(start, "unsupported Python string prefix")
					}
				} else if prefix != "r" && prefix != "b" && prefix != "u" && prefix != "br" && prefix != "rb" {
					return fail(start, "unsupported Python string prefix")
				}
				quoteAt = p
			}
		}
		if lang == "rust" && (s[i] == 'b' || s[i] == 'c') && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\'') {
			quoteAt = i + 1
		}
		q := s[quoteAt]
		if q == '\'' || q == '"' || q == '`' && (js || lang == "go") {
			if q == '\'' && lang == "rust" && quoteAt == start {
				// A lifetime has no closing apostrophe; character literals contain one rune or escape.
				p := quoteAt + 1
				if p < len(s) && s[p] != '\\' {
					_, n := utf8.DecodeRuneInString(s[p:])
					p += n
					if p >= len(s) || s[p] != '\'' {
						if quoteAt+1 < len(s) {
							rr, _ := utf8.DecodeRuneInString(s[quoteAt+1:])
							if identStart(rr) {
								i++
								add(start, i, "punct")
								continue
							}
						}
						return fail(start, "malformed character literal")
					}
				}
			}
			triple := lang == "python" && strings.HasPrefix(s[quoteAt:], strings.Repeat(string(q), 3))
			delimiter := string(q)
			if triple {
				delimiter = strings.Repeat(string(q), 3)
			}
			if !strict && (formatted || js && q == '`') {
				end, ok := opaqueLiteral(s, quoteAt, lang, true, 0)
				if !ok {
					return fail(start, "unterminated formatted literal")
				}
				i = end
				add(start, i, "string")
				continue
			}
			i = quoteAt + len(delimiter)
			closed := false
			for i < len(s) {
				if strings.HasPrefix(s[i:], delimiter) {
					i += len(delimiter)
					closed = true
					break
				}
				if js && q == '`' && strings.HasPrefix(s[i:], "${") {
					return fail(i, "unsupported template interpolation")
				}
				if s[i] == '\\' && !(lang == "go" && q == '`') {
					i++
					if i >= len(s) {
						break
					}
					if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
						i++
					}
					i++
					continue
				}
				if (s[i] == '\r' || s[i] == '\n') && !triple && q != '`' && lang != "rust" {
					return fail(start, "newline in string literal")
				}
				i++
			}
			if !closed {
				return fail(start, "unterminated string literal")
			}
			add(start, i, "string")
			continue
		}
		if js && s[i] == '/' {
			division, known := jsSlash(tokens)
			if !known {
				return fail(i, "unsupported ambiguous JavaScript slash syntax")
			}
			if division {
				i++
				if i < len(s) && s[i] == '=' {
					i++
				}
				add(start, i, "punct")
				continue
			}
			i++
			inClass, closed := false, false
			for i < len(s) {
				c := s[i]
				if c == '\n' || c == '\r' || strings.HasPrefix(s[i:], "\u2028") || strings.HasPrefix(s[i:], "\u2029") {
					return fail(start, "newline in regex literal")
				}
				if c == '\\' {
					if i+1 >= len(s) || strings.HasPrefix(s[i+1:], "\u2028") || strings.HasPrefix(s[i+1:], "\u2029") || s[i+1] == '\n' || s[i+1] == '\r' {
						return fail(start, "unterminated regex escape")
					}
					i += 2
					continue
				}
				if c == '[' {
					if inClass {
						return fail(i, "unsupported nested regex character class")
					}
					inClass = true
				}
				if c == ']' {
					inClass = false
				}
				if c == '/' && !inClass {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return fail(start, "unterminated regex literal")
			}
			for i < len(s) && isASCIILetter(s[i]) {
				if s[i] == 'v' {
					return fail(i, "unsupported regex v flag")
				}
				i++
			}
			add(start, i, "string")
			continue
		}
		if lang == "c" && s[i] == 'R' && i+1 < len(s) && s[i+1] == '"' {
			return fail(i, "unsupported raw string")
		}
		if identStart(r) {
			i += size
			for i < len(s) {
				rr, n := utf8.DecodeRuneInString(s[i:])
				if !identStart(rr) && !unicode.IsDigit(rr) {
					break
				}
				i += n
			}
			add(start, i, "identifier")
			continue
		}
		if s[i] >= '0' && s[i] <= '9' {
			i++
			for i < len(s) {
				c := s[i]
				if isASCIILetter(c) || c >= '0' && c <= '9' || c == '_' {
					i++
					continue
				}
				if c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
					i++
					continue
				}
				if (c == '+' || c == '-') && strings.ContainsRune("eEpP", rune(s[i-1])) {
					i++
					continue
				}
				break
			}
			add(start, i, "number")
			continue
		}
		if i+1 < len(s) {
			switch s[i : i+2] {
			case "=>", "::", "->", "?.":
				i += 2
				add(start, i, "punct")
				continue
			}
		}
		i += size
		add(start, i, "punct")
	}
	return tokens, nil
}

// opaqueLiteral is indexing-only: uncertain interpolation syntax consumes the
// rest of the source rather than letting literal text become declarations.
func opaqueLiteral(s string, quote int, lang string, formatted bool, depth int) (int, bool) {
	if depth > 128 {
		return len(s), true
	}
	q := s[quote]
	delimiter := string(q)
	if lang == "python" && strings.HasPrefix(s[quote:], strings.Repeat(delimiter, 3)) {
		delimiter = strings.Repeat(delimiter, 3)
	}
	for i := quote + len(delimiter); i < len(s); {
		if strings.HasPrefix(s[i:], delimiter) {
			return i + len(delimiter), true
		}
		if s[i] == '\\' {
			i += 2
			continue
		}
		if lang == "python" && formatted && strings.HasPrefix(s[i:], "{{") {
			i += 2
			continue
		}
		expr := formatted && lang == "python" && s[i] == '{'
		begin := i + 1
		if lang != "python" && q == '`' && strings.HasPrefix(s[i:], "${") {
			expr = true
			begin = i + 2
		}
		if expr {
			end, ok := opaqueExpression(s, begin, lang, depth+1)
			if !ok {
				return 0, false
			}
			if end == len(s) {
				return end, true
			}
			i = end
			continue
		}
		i++
	}
	return 0, false
}

func opaqueExpression(s string, i int, lang string, depth int) (int, bool) {
	braces := 1
	for i < len(s) {
		c := s[i]
		if lang == "python" && c == '#' || lang != "python" && strings.HasPrefix(s[i:], "//") {
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
			continue
		}
		if lang != "python" && strings.HasPrefix(s[i:], "/*") {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return 0, false
			}
			i += end + 4
			continue
		}
		// Regex/division inside an interpolation requires expression parsing.
		if lang != "python" && c == '/' {
			return len(s), true
		}
		if c == '\'' || c == '"' || lang != "python" && c == '`' {
			formatted := c == '`'
			if lang == "python" {
				p := i
				for p > 0 && isASCIILetter(s[p-1]) {
					p--
				}
				formatted = strings.ContainsAny(s[p:i], "fFtT")
			}
			end, ok := opaqueLiteral(s, i, lang, formatted, depth+1)
			if !ok {
				return 0, false
			}
			if end == len(s) {
				return end, true
			}
			i = end
			continue
		}
		if c == '{' {
			braces++
		}
		if c == '}' {
			braces--
			if braces == 0 {
				return i + 1, true
			}
		}
		i++
	}
	return 0, false
}

// jsSlash recognizes only unambiguous expression boundaries; braces and control
// statement parentheses need a full parser and intentionally remain unsupported.
func jsSlash(tokens []Token) (division, known bool) {
	n := len(tokens) - 1
	for n >= 0 && tokens[n].Kind == "comment" {
		n--
	}
	if n < 0 {
		return false, true
	}
	t := tokens[n]
	if t.Kind == "number" || t.Kind == "string" || t.Text == "]" {
		return true, true
	}
	if t.Kind == "identifier" {
		switch t.Text {
		case "return", "throw", "case", "yield", "await", "typeof", "void", "delete", "new", "instanceof", "in", "of":
			return false, true
		case "break", "continue", "debugger", "else", "do", "try", "finally", "function", "class", "const", "let", "var", "import", "export", "default", "extends":
			return false, false
		default:
			return true, true
		}
	}
	if (t.Text == "+" || t.Text == "-") && n > 0 && tokens[n-1].Text == t.Text {
		return false, false
	}
	switch t.Text {
	case "=", "(", "[", ",", ":", ";", "!", "?", "=>", "+", "-", "*", "%", "&", "|", "^", "~", "<", ">":
		return false, true
	case ")":
		depth := 1
		for j := n - 1; j >= 0; j-- {
			if tokens[j].Kind == "comment" || tokens[j].Kind == "string" {
				continue
			}
			if tokens[j].Text == ")" {
				depth++
			}
			if tokens[j].Text == "(" {
				depth--
				if depth == 0 {
					k := j - 1
					for k >= 0 && tokens[k].Kind == "comment" {
						k--
					}
					if k >= 0 {
						switch tokens[k].Text {
						case "if", "while", "for", "with", "switch", "catch":
							return false, false
						}
					}
					return true, true
				}
			}
		}
	}
	return false, false
}

func identStart(r rune) bool    { return r == '_' || r == '$' || unicode.IsLetter(r) }
func isASCIILetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// Mask blanks every byte of comments and strings except CR and LF, preserving
// source length and line endings. An error returns an empty string, never a partial mask.
// Unlike Scan, Mask rejects Python f/t strings and JavaScript template interpolation.
func Mask(language, source string) (string, error) {
	tokens, err := scan(language, source, true)
	if err != nil {
		return "", err
	}
	out := []byte(source)
	for _, t := range tokens {
		if t.Kind != "string" && t.Kind != "comment" {
			continue
		}
		for i := t.Start; i < t.End; i++ {
			if out[i] != '\r' && out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out), nil
}
