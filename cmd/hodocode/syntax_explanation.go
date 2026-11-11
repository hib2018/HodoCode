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
	var lines []string
	var selected *syntaxExplanation
	best := int(^uint(0) >> 1)
	if height > 0 && s.cursorRow >= s.top && s.cursorRow < s.top+height {
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
	texts = append([]string{"カーソル位置（外部送信なし）", ""}, texts...)
	width = max(1, width)
	for _, text := range strings.Split(strings.Join(texts, "\n"), "\n") {
		text = strings.ReplaceAll(text, "\t", "    ")
		line, cells := "", 0
		for _, r := range text {
			n := runeWidth(r)
			// Retain even a wide rune when the requested width is only one cell.
			if cells+n > width && line != "" {
				lines = append(lines, line)
				line, cells = "", 0
			}
			line += string(r)
			cells += n
		}
		lines = append(lines, line)
	}
	return lines
}

func screenCodeWidth(width int) int {
	return width - width/3 - 3
}
