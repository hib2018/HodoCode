package main

import (
	"go/token"
	"strings"
	"testing"
)

func TestSourceExplanations(t *testing.T) {
	source := "func Work() {\n // if return :=\n s := `for`\n if s != \"return\" { return }\n}\n"
	notes := sourceExplanations(source)
	want := []syntaxExplanation{{0, token.FUNC}, {2, token.DEFINE}, {3, token.IF}, {3, token.RETURN}}
	if len(notes) != len(want) {
		t.Fatalf("notes = %+v", notes)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Fatalf("note %d = %+v, want %+v", i, notes[i], want[i])
		}
	}
}

func TestSyntaxExplanationPane(t *testing.T) {
	s := testScreen(t, "Recur")
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
