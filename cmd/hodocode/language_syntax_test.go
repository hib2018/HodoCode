package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func TestLanguageSyntaxBasics(t *testing.T) {
	for _, tt := range []struct{ language, source, keyword, meaning string }{
		{"javascript", "function entry() { const 名前 = 'hello'; return 名前; } // tail", "const", "再代入できない"},
		{"typescript", "function entry(): number { let 名前: number = 42; return 名前; } // tail", "let", "再代入可能"},
		{"python", "def entry():\r\n    名前 = 'hello' # tail\r\n    return 名前", "def", "インデント"},
		{"rust", "fn entry() { let 名前 = 42; } // tail", "let", "既定で不変"},
		{"zig", "fn entry() void { const 名前 = 42; } // tail", "const", "再代入できない"},
		{"c", "int entry(void) { return 42; } // tail", "int", "符号付き整数"},
	} {
		t.Run(tt.language, func(t *testing.T) {
			notes := languageExplanations(tt.language, tt.source)
			found := false
			for _, note := range notes {
				help := note.help()
				if strings.Contains(help.reference, "go.dev") || strings.Contains(help.description, "Go") || strings.HasPrefix(note.topic, "standard:") {
					t.Fatalf("Go help leaked: %+v", help)
				}
				if note.literal == tt.keyword {
					found = true
					if !strings.Contains(help.description, tt.meaning) {
						t.Fatalf("missing language basic: %+v", help)
					}
				}
			}
			if !found {
				t.Fatal("keyword not explained")
			}
			rows := languageHighlights(tt.language, tt.source, 9)
			var styled string
			for row, line := range strings.Split(tt.source, "\n") {
				styled += styledSlice(fmt.Sprintf("%d │ %s", 9+row, strings.TrimSuffix(line, "\r")), 0, 200, rows[row])
			}
			if !strings.Contains(styled, "\x1b[35m"+tt.keyword+"\x1b[0m") || !strings.Contains(styled, "\x1b[90m") {
				t.Fatalf("missing language highlighting: %q", styled)
			}
		})
	}
}

func TestLanguageHelpNeverFallsBackToGo(t *testing.T) {
	for _, language := range []string{"javascript", "typescript", "python", "rust", "zig", "c", "unsupported"} {
		for _, note := range languageExplanations(language, "make len nil map func") {
			help := note.help()
			if strings.Contains(help.reference, "go.dev") || strings.Contains(help.description, "Go") {
				t.Fatalf("%s: Go help leaked: %+v", language, help)
			}
		}
	}
	if help := languageExplanations("python", "def entry(): return 'unterminated")[0].help(); !strings.Contains(help.description, "利用できません") {
		t.Fatalf("ambiguous source did not get explicit unsupported help: %+v", help)
	}
}

func TestPythonSyntaxRunePositions(t *testing.T) {
	notes := languageExplanations("python", "名前 = 'あ'\r\nreturn 名前")
	for _, note := range notes {
		if note.literal == "'あ'" && (note.row != 0 || note.column != 5 || note.endColumn != 8) {
			t.Fatalf("literal position = %+v", note)
		}
		if note.literal == "return" && (note.row != 1 || note.column != 0 || note.endColumn != 6) {
			t.Fatalf("keyword position = %+v", note)
		}
	}
}

func TestLanguageScreenAndReloadSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("def entry():\n    return 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := analyze.LoadLanguage(root, "python")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for key := range p.Functions {
		id = key
		break
	}
	s, err := newScreen(p, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.explanations) == 0 || !strings.Contains(s.explanations[0].help().description, "インデント") {
		t.Fatal("screen uses wrong language help")
	}
	if err := os.WriteFile(filepath.Join(root, "other.js"), []byte("function other() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.reload(24, 120); err != nil {
		t.Fatal(err)
	}
	if s.p.Language != "python" || len(s.p.Functions) != 1 {
		t.Fatalf("reload lost explicit selection: %s, %+v", s.p.Language, s.p.Functions)
	}
}
