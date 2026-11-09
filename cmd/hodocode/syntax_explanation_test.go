package main

import (
	"go/token"
	"strings"
	"testing"
)

func TestSourceExplanations(t *testing.T) {
	source := "func Work() {\n // if return :=\n s := `for`\n if s != \"return\" { return }\n}\n"
	notes := sourceExplanations(source)
	var keywords []syntaxExplanation
	for _, note := range notes {
		if note.token.IsKeyword() || note.token == token.DEFINE {
			keywords = append(keywords, note)
		}
		if help := note.help(); help.description == "" || help.example == "" {
			t.Fatalf("missing help for %+v", note)
		}
	}
	want := []syntaxExplanation{
		{0, 0, 0, 4, token.FUNC, "func"},
		{2, 3, 2, 5, token.DEFINE, ":="},
		{3, 1, 3, 3, token.IF, "if"},
		{3, 20, 3, 26, token.RETURN, "return"},
	}
	if len(keywords) != len(want) {
		t.Fatalf("keywords = %+v", keywords)
	}
	for i := range want {
		if keywords[i] != want[i] {
			t.Fatalf("note %d = %+v, want %+v", i, keywords[i], want[i])
		}
	}
}

func TestSourceExplanationsRunePositions(t *testing.T) {
	source := "//line other.go:500\n\t名前 := `あ\r\nい` + '界'\n// if := return\n"
	notes := sourceExplanations(source)
	want := []syntaxExplanation{
		{1, 1, 1, 3, token.IDENT, "名前"},
		{1, 4, 1, 6, token.DEFINE, ":="},
		{1, 7, 2, 2, token.STRING, "`あ\nい`"},
		{2, 3, 2, 4, token.ADD, "+"},
		{2, 5, 2, 8, token.CHAR, "'界'"},
	}
	if len(notes) != len(want) {
		t.Fatalf("notes = %+v", notes)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Errorf("note %d = %+v, want %+v", i, notes[i], want[i])
		}
	}
}

func TestSourceExplanationsLiterals(t *testing.T) {
	notes := sourceExplanations("0xff 1.25 2i 'あ' \"return\\n\" `if` // for\n")
	want := []token.Token{token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING, token.STRING}
	if len(notes) != len(want) {
		t.Fatalf("notes = %+v", notes)
	}
	for i, tok := range want {
		if notes[i].token != tok || notes[i].help().example == "" {
			t.Errorf("note %d = %+v, want %s", i, notes[i], tok)
		}
	}
}

func TestSyntaxExplanationCoverage(t *testing.T) {
	for tok := token.ADD; tok <= token.TILDE; tok++ {
		if !tok.IsKeyword() && !tok.IsOperator() && tok != token.TILDE {
			continue
		}
		note := syntaxExplanation{token: tok}
		if help := note.help(); help.description == "" || help.example == "" {
			t.Errorf("missing explanation/example for %s", tok)
		}
	}
	for _, source := range []string{"`unterminated\r\nあ", "\"unterminated", "'", "@", "", "// only a comment"} {
		for _, note := range sourceExplanations(source) {
			if note.column < 0 || note.endColumn < 0 || note.row > note.endRow {
				t.Errorf("invalid span for %q: %+v", source, note)
			}
		}
	}
}

func TestSyntaxExplanationCursor(t *testing.T) {
	s := testScreen(t, "Recur")
	s.explanations = sourceExplanations("名前 := 12 + 3\n\treturn 名前\n\n// if return\ntext := `あ\nい`\n")
	for _, tt := range []struct {
		name        string
		row, column int
		want        string
	}{
		{"identifier", 0, 1, "名前\":"},
		{"define first rune", 0, 3, ":=:"},
		{"define second rune", 0, 4, ":=:"},
		{"literal", 0, 7, "整数リテラル"},
		{"operator", 0, 9, "+:"},
		{"whitespace prefers preceding on tie", 0, 8, "整数リテラル"},
		{"before first token", 1, 0, "return:"},
		{"after line end", 1, 80, "名前\":"},
		{"blank stays on same line", 2, 0, "解説対象の構文はありません"},
		{"comment is not code", 3, 5, "解説対象の構文はありません"},
		{"multiline literal", 5, 0, "文字列リテラル"},
		{"invalid row", 99, 0, "解説対象の構文はありません"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s.cursorRow, s.cursorColumn = tt.row, tt.column
			got := strings.Join(s.explanationLines(200, 10), "")
			if !strings.Contains(got, tt.want) {
				t.Fatalf("want %q in %s", tt.want, got)
			}
			if !strings.Contains(got, "外部送信なし") {
				t.Fatal(got)
			}
		})
	}
	// Horizontal display scrolling and prefixed display text do not affect raw columns.
	s.code = []string{"999 │ 名前 := 12 + 3"}
	s.horizontal = 20
	s.cursorRow, s.cursorColumn = 0, 9
	if got := strings.Join(s.explanationLines(200, 10), ""); !strings.Contains(got, "+:") {
		t.Fatal(got)
	}
	s.top = 1
	if got := strings.Join(s.explanationLines(200, 10), ""); !strings.Contains(got, "解説対象の構文はありません") {
		t.Fatal(got)
	}
}

func TestSyntaxExplanationIdentifiers(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"true", "真偽値"}, {"false", "真偽値"}, {"nil", "値がない"},
		{"_", "空白識別子"}, {"iota", "const"}, {"MyValue", "スコープ"},
	} {
		help := (syntaxExplanation{token: token.IDENT, literal: tt.name}).help()
		if !strings.Contains(help.description, tt.want) || help.example == "" {
			t.Errorf("%s: %+v", tt.name, help)
		}
	}
}

func TestSyntaxExplanationPane(t *testing.T) {
	s := testScreen(t, "Recur")
	press(t, s, "g", "0")
	frame := s.render(120, 18, false)
	if screenCodeWidth(120) != 77 || !strings.Contains(frame, " │ 構文の解説") || !strings.Contains(frame, "func:") {
		t.Fatal(frame)
	}
	lines := s.explanationLines(13, len(s.code))
	for _, line := range lines[2:] {
		if displayWidth(line) > 13 {
			t.Fatalf("explanation overflows: %q", line)
		}
	}
	// A short terminal still lets the user read all of the wrapped explanation.
	for range 30 {
		press(t, s, "]")
		s.render(41, 8, false)
	}
	notes := s.explanationLines(41/3, 5)
	if s.explanationTop != max(0, len(notes)-5) {
		t.Fatalf("explanation scroll = %d, lines = %d", s.explanationTop, len(notes))
	}
	press(t, s, "[")
	if s.explanationTop != max(0, len(notes)-6) {
		t.Fatal("explanation did not scroll back")
	}
	s.top = len(s.code)
	if got := strings.Join(s.explanationLines(40, 5), ""); !strings.Contains(got, "解説対象の構文はありません") {
		t.Fatal(got)
	}
}
