package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"strings"
	"unicode/utf8"
)

type syntaxExplanation struct {
	row, column, endRow, endColumn int // Zero-based rune positions; end is exclusive.
	token                          token.Token
	literal                        string
	topic                          string
}

func sourceExplanations(source string, symbols ...map[int]string) []syntaxExplanation {
	file := token.NewFileSet().AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, []byte(source), func(token.Position, string) {}, 0)
	// Compute columns from raw bytes, not scanner columns (which count bytes).
	lineStarts := []int{0}
	for i, b := range []byte(source) {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	position := func(offset int) (int, int) {
		p := file.PositionFor(file.Pos(offset), false)
		return p.Line - 1, utf8.RuneCountInString(source[lineStarts[p.Line-1]:offset])
	}
	var notes []syntaxExplanation
	topics := sourceTopics(source)
	previous := token.ILLEGAL
	for {
		pos, tok, literal := scan.Scan()
		if tok == token.EOF {
			return notes
		}
		if tok == token.ILLEGAL || tok == token.SEMICOLON && literal == "\n" {
			continue
		}
		if literal == "" {
			literal = tok.String()
		}
		offset := file.Offset(pos)
		end := offset + len(literal)
		// The scanner removes CR bytes from raw string literals; retain the actual span.
		if tok == token.STRING && source[offset] == '`' {
			if closing := strings.IndexByte(source[offset+1:], '`'); closing >= 0 {
				end = offset + closing + 2
			} else {
				end = len(source)
			}
		}
		topic := topics[offset]
		if tok == token.IDENT {
			if previous == token.PERIOD {
				topic = "identifier"
			}
			if len(symbols) > 0 && symbols[0][offset] != "" {
				topic = symbols[0][offset]
			}
		}
		previous = tok
		row, column := position(offset)
		endRow, endColumn := position(min(end, len(source)))
		notes = append(notes, syntaxExplanation{row, column, endRow, endColumn, tok, literal, topic})
	}
}

func (s *screen) explanationLines(width, height int) []string {
	lines, _ := s.localExplanation(width, height)
	return lines
}

func (s *screen) localExplanation(width, height int) ([]string, [][]syntaxHighlight) {
	var lines []string
	var styles [][]syntaxHighlight
	var selected *syntaxExplanation
	best := int(^uint(0) >> 1)
	if s.cursorRow >= 0 {
		for i := range s.explanations {
			note := &s.explanations[i]
			if s.cursorRow < note.row || s.cursorRow > note.endRow || note.help().description == "" {
				continue
			}
			start, end := 0, int(^uint(0)>>1)
			if s.cursorRow == note.row {
				start = note.column
			}
			if s.cursorRow == note.endRow {
				end = note.endColumn
			}
			if start <= s.cursorColumn && s.cursorColumn < end {
				selected = note
				break
			}
			distance := start - s.cursorColumn
			if s.cursorColumn >= end {
				distance = s.cursorColumn - end + 1
			}
			// Equal distances prefer the preceding token, matching reading order.
			if distance < best {
				selected, best = note, distance
			}
		}
	}
	texts := []string{"解説対象の構文はありません。"}
	if selected != nil {
		help := selected.help()
		// Quote literals so embedded newlines and tabs cannot disrupt the pane.
		label := selected.literal
		if selected.token.IsLiteral() || selected.token == token.IDENT {
			label = fmt.Sprintf("%q", label)
		}
		texts = []string{
			label,
			help.category + " · " + help.title,
			fmt.Sprintf("%d行 %d列", s.p.Functions[s.current].Line+s.cursorRow, s.cursorColumn+1),
			"",
			"意味",
			help.description,
			"",
			"書き方",
			help.syntax,
			"",
			"使用例",
			help.example,
			"",
			"注意点",
			help.caution,
			"",
			"公式リファレンス",
			help.reference,
		}
	}
	textStyles := map[int][]syntaxHighlight{}
	if selected != nil && selected.token == token.IDENT && selected.row < len(s.sourceOffsets) {
		// Resolve the selected token, using byte offsets rather than rune columns.
		offset := s.sourceOffsets[selected.row]
		if selected.row < len(s.code) {
			raw := []rune(s.rawLine(selected.row))
			offset += len(string(raw[:min(selected.column, len(raw))]))
		}
		if definition, ok := s.definitions[offset]; ok {
			if definition.PopupOnly {
				definition.Snippets = nil
			}
			var prefix []string
			for _, snippet := range definition.Snippets {
				prefix = append(prefix, snippet.File, "")
				source := strings.ReplaceAll(snippet.Source, "\t", "    ")
				highlights := sourceHighlights(source, snippet.Line)
				for row, raw := range strings.Split(source, "\n") {
					textStyles[len(prefix)+2] = highlights[row]
					prefix = append(prefix, fmt.Sprintf("%d │ %s", snippet.Line+row, strings.TrimSuffix(raw, "\r")))
				}
				prefix = append(prefix, "")
			}
			if definition.Reason != "" {
				prefix = append(prefix, definition.Reason, "")
				texts = append(prefix, texts...)
			} else if len(prefix) > 0 {
				if strings.HasPrefix(selected.topic, "standard:") {
					texts = append(prefix, texts...)
				} else {
					texts = prefix
				}
			}
		}
	}
	texts = append([]string{"カーソル位置（外部送信なし）", ""}, texts...)
	width = max(1, width)
	for index, block := range texts {
		for _, text := range strings.Split(block, "\n") {
			spans := textStyles[index]
			consumed := 0
			appendLine := func(line string, cells int) {
				lines = append(lines, line)
				var wrapped []syntaxHighlight
				for _, span := range spans {
					start, end := max(consumed, span.start), min(consumed+cells, span.end)
					if start < end {
						wrapped = append(wrapped, syntaxHighlight{start - consumed, end - consumed, span.style})
					}
				}
				styles = append(styles, wrapped)
				consumed += cells
			}
			text = strings.ReplaceAll(text, "\t", "    ")
			line, cells := "", 0
			for _, r := range text {
				n := runeWidth(r)
				// Retain even a wide rune when the requested width is only one cell.
				if cells+n > width && line != "" {
					appendLine(line, cells)
					line, cells = "", 0
				}
				line += string(r)
				cells += n
			}
			appendLine(line, cells)
		}
	}
	if s.agent != nil && s.agent.comments[s.current].stale {
		advice := wrapPaneText(staleCommentsAdvice, width)
		lines = append(advice, lines...)
		styles = append(make([][]syntaxHighlight, len(advice)), styles...)
	}
	return lines, styles
}

func screenCodeWidth(width int) int {
	return width - width/3 - 3
}
