package main

import (
	"go/token"
	"strings"
	"testing"
)

func TestKnownNamesWithoutCallContext(t *testing.T) {
	for _, tt := range []struct{ name, topic string }{
		{"println", "builtin_println"}, {"print", "builtin_print"}, {"len", "builtin_len"},
		{"append", "builtin_append"}, {"error", "type_error"}, {"string", "type_string"},
	} {
		for _, source := range []string{tt.name, "func f() { _ = " + tt.name + " }", "func f() { (" + tt.name + ")(nil) }"} {
			found := false
			for _, note := range sourceExplanations(source) {
				if note.token == token.IDENT && note.literal == tt.name {
					found = true
					if help := note.help(); help.title != syntaxTopics[tt.topic].title {
						t.Fatalf("%q: got %q, want %q", source, help.title, syntaxTopics[tt.topic].title)
					}
				}
			}
			if !found {
				t.Fatalf("missing name in %q", source)
			}
		}
	}
}

func TestKnownNamesDoNotOverrideLocalNames(t *testing.T) {
	for _, source := range []string{
		"func f(println func()) { println() }",
		"func f() { println := func() {}; println() }",
		"func println() {}; func f() { println() }",
		"func f(v struct{ println int }) { _ = v.println }",
		"func f() { _ = struct{ println int }{println: 1} }",
		"v.println", "v /* comment */ . /* comment */ println()",
		"func f() { type string int; var v string; _ = v }",
	} {
		for _, note := range sourceExplanations(source) {
			if note.literal == "println" || note.literal == "string" {
				if note.help().category != syntaxDescriptions[token.IDENT].category {
					t.Fatalf("local/member name was treated as predeclared in %q: %+v", source, note)
				}
			}
		}
	}
}

func TestKnownStandardCallsInScreen(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": `package main
import (
 out "fmt"
 "errors"
 "strings"
)
func main() {
 out.Println("hello")
 _ = errors.Is(nil,nil)
 _ = strings.TrimSpace(" x ")
 println("debug")
}
`})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, title, reference string }{
		{"Println", standardHelp["fmt.Println"].title, "https://pkg.go.dev/fmt#Println"},
		{"Is", standardHelp["errors.Is"].title, "https://pkg.go.dev/errors#Is"},
		{"TrimSpace", standardHelp["strings.TrimSpace"].title, "https://pkg.go.dev/strings#TrimSpace"},
		{"println", syntaxTopics["builtin_println"].title, "https://go.dev/ref/spec#Bootstrapping"},
	} {
		found := false
		for _, note := range s.explanations {
			if note.literal != tt.name {
				continue
			}
			found = true
			help := note.help()
			if help.title != tt.title || help.reference != tt.reference {
				t.Fatalf("%s: %+v", tt.name, help)
			}
			s.cursorRow, s.cursorColumn = note.row, note.column
			s.top = 0
			pane := strings.Join(s.explanationLines(300, len(s.code)), "\n")
			if !strings.Contains(pane, tt.title) || !strings.Contains(pane, tt.reference) {
				t.Fatalf("pane missing role/reference: %s", pane)
			}
		}
		if !found {
			t.Fatalf("missing name %s", tt.name)
		}
	}
}

func TestKnownStandardDoesNotMatchSpellingAlone(t *testing.T) {
	for _, files := range []map[string]string{
		{"main.go": "package main\ntype Printer struct{}\nfunc (Printer) Println(...any) {}\nfunc main() { fmt := Printer{}; fmt.Println(1) }\n"},
		{"main.go": "package main\nfunc println(...any) {}\nfunc main() { println(1) }\n"},
		{"main.go": "package main\nfunc main() { println(1) }\n", "other.go": "package main\nfunc println(...any) {}\n"},
	} {
		p := screenProject(t, files)
		s, err := newScreen(p, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, note := range s.explanations {
			if note.literal == "Println" || note.literal == "println" {
				if note.help().category != syntaxDescriptions[token.IDENT].category {
					t.Fatalf("user-defined function received builtin/standard help: %+v", note)
				}
			}
		}
	}
}
