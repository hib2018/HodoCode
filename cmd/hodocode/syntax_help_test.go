package main

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSyntaxHelpCatalog(t *testing.T) {
	check := func(t *testing.T, help syntaxHelp) {
		t.Helper()
		for _, field := range []struct{ name, value string }{
			{"category", help.category}, {"title", help.title},
			{"description", help.description}, {"syntax", help.syntax},
			{"example", help.example}, {"caution", help.caution},
			{"reference", help.reference},
		} {
			if strings.TrimSpace(field.value) == "" {
				t.Errorf("empty %s", field.name)
			}
		}
		u, err := url.Parse(help.reference)
		if err != nil || u.Scheme != "https" || u.Host != "go.dev" || u.Path != "/ref/spec" || u.Fragment == "" || u.User != nil || u.RawQuery != "" || u.Opaque != "" || u.RawFragment != "" {
			t.Errorf("reference must be an official Go specification section: %q", help.reference)
		}
	}
	// Token boundaries contain private sentinel values, not real tokens.
	for tok := token.ILLEGAL; tok <= token.TILDE; tok++ {
		if tok > token.COMMENT && !tok.IsLiteral() && !tok.IsOperator() && !tok.IsKeyword() && tok != token.TILDE {
			continue
		}
		t.Run("token/"+tok.String(), func(t *testing.T) {
			check(t, (syntaxExplanation{token: tok}).help())
		})
	}
	// Also check stored entries, even if lookup would mask a malformed entry.
	tokens := make([]int, 0, len(syntaxDescriptions))
	for tok := range syntaxDescriptions {
		tokens = append(tokens, int(tok))
	}
	sort.Ints(tokens)
	for _, n := range tokens {
		tok := token.Token(n)
		t.Run("stored/"+tok.String(), func(t *testing.T) { check(t, syntaxDescriptions[tok]) })
	}
	for _, topic := range syntaxHelpTopicNames() {
		t.Run("topic/"+topic, func(t *testing.T) {
			help := syntaxTopics[topic]
			check(t, help)
			if got := (syntaxExplanation{topic: topic}).help(); got != help {
				t.Errorf("topic lookup = %+v, want %+v", got, help)
			}
			if strings.HasPrefix(topic, "builtin_") {
				if !strings.Contains(help.description, "推定") || !strings.Contains(help.caution, "同名") {
					t.Error("builtin help must identify syntactic inference and warn about shadowing")
				}
			}
		})
	}
}

func syntaxHelpTopicNames() []string {
	names := make([]string, 0, len(syntaxTopics))
	for name := range syntaxTopics {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func syntaxHelpExampleFile(t *testing.T, example string) (*token.FileSet, *ast.File) {
	t.Helper()
	// Package/import topics deliberately show a complete source file.
	if strings.HasPrefix(strings.TrimSpace(example), "package ") {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "example.go", example, parser.AllErrors)
		if err != nil {
			t.Fatalf("complete example does not parse: %v\n%s", err, example)
		}
		return fset, file
	}
	declaration := "package main\n" + example + "\n"
	fset := token.NewFileSet()
	file, declarationErr := parser.ParseFile(fset, "example.go", declaration, parser.AllErrors)
	if declarationErr == nil {
		return fset, file
	}
	statements := "package main\nfunc example() {\n" + example + "\n}\n"
	fset = token.NewFileSet()
	file, err := parser.ParseFile(fset, "example.go", statements, parser.AllErrors)
	if err != nil {
		t.Fatalf("example parses as neither declarations (%v) nor statements (%v):\n%s", declarationErr, err, example)
	}
	return fset, file
}

func TestSyntaxHelpExamples(t *testing.T) {
	check := func(t *testing.T, help syntaxHelp) {
		t.Helper()
		fset, file := syntaxHelpExampleFile(t, help.example)
		config := types.Config{Importer: importer.Default(), GoVersion: "go1.23"}
		if _, err := config.Check("catalog/example", fset, []*ast.File{file}, nil); err != nil {
			t.Errorf("example is not self-contained and well-typed: %v\n%s", err, help.example)
		}
	}
	for tok := token.ILLEGAL; tok <= token.TILDE; tok++ {
		if tok > token.COMMENT && !tok.IsLiteral() && !tok.IsOperator() && !tok.IsKeyword() && tok != token.TILDE {
			continue
		}
		t.Run("token/"+tok.String(), func(t *testing.T) { check(t, (syntaxExplanation{token: tok}).help()) })
	}
	for _, topic := range syntaxHelpTopicNames() {
		t.Run("topic/"+topic, func(t *testing.T) { check(t, syntaxTopics[topic]) })
	}
}

func TestSyntaxHelpExecutableExamples(t *testing.T) {
	cases := []struct{ topic, want string }{
		{"pointer_type", "3\n"}, {"dereference", "4\n"}, {"address_of", "5\n"},
		{"receive", "7 true 0 false\n"}, {"send", "8\n"},
		{"chan_send", "2\n"}, {"chan_receive", "2\n"},
		{"for_clause", "0\n1\n2\n"}, {"for_condition", "0\n1\n"},
		{"for_infinite", "1\n"}, {"range_loop", "0\n1\n2\n"},
		{"slice_type", "true 0 0\n4\n"}, {"array_type", "3 9\n"},
		{"slice", "9 2\n"}, {"full_slice", "2 9\n"},
		{"builtin_len", "3 0\n"}, {"builtin_cap", "2 5\n"},
		{"builtin_make", "3\n"}, {"builtin_new", "0\ntrue\n"},
		{"builtin_append", "2 3\n"}, {"builtin_copy", "2 5\n"},
		{"builtin_delete", "0\n"}, {"builtin_clear", "2 0\n"},
		{"builtin_close", "0 false\n"}, {"builtin_min", "1\na\n"},
		{"builtin_max", "3\nb\n"}, {"builtin_complex", "true\n"},
		{"builtin_real", "true\n"}, {"builtin_imag", "true\n"},
	}
	var source, want strings.Builder
	source.WriteString("package main\nfunc main() {\n")
	for _, tt := range cases {
		help, ok := syntaxTopics[tt.topic]
		if !ok {
			t.Fatalf("missing executable topic %q", tt.topic)
		}
		source.WriteString("{\n" + help.example + "\n}\n")
		want.WriteString(tt.want)
	}
	// Generated compound assignments must also agree with their result comments.
	for _, tt := range []struct {
		tok  token.Token
		want string
	}{
		{token.ADD_ASSIGN, "9"}, {token.SUB_ASSIGN, "3"}, {token.MUL_ASSIGN, "18"},
		{token.QUO_ASSIGN, "2"}, {token.REM_ASSIGN, "0"}, {token.AND_ASSIGN, "2"},
		{token.OR_ASSIGN, "7"}, {token.XOR_ASSIGN, "5"}, {token.SHL_ASSIGN, "12"},
		{token.SHR_ASSIGN, "3"}, {token.AND_NOT_ASSIGN, "4"},
	} {
		source.WriteString("{\n" + (syntaxExplanation{token: tt.tok}).help().example + "\n}\n")
		want.WriteString(tt.want + "\n")
	}
	source.WriteString("}\n")
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", path)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("catalog examples failed: %v\n%s", err, out)
	}
	if string(out) != want.String() {
		t.Errorf("catalog output = %q, want %q", out, want.String())
	}
}

func TestSyntaxHelpChannelRules(t *testing.T) {
	var nilChannel chan int
	select {
	case <-nilChannel:
		t.Error("nil channel receive must not proceed")
	case nilChannel <- 1:
		t.Error("nil channel send must not proceed")
	default:
	}
	ch := make(chan int, 1)
	ch <- 7
	close(ch)
	if value, ok := <-ch; value != 7 || !ok {
		t.Fatalf("closed channel must drain buffered values: %d, %v", value, ok)
	}
	for i := 0; i < 2; i++ {
		if value, ok := <-ch; value != 0 || ok {
			t.Errorf("drained closed channel receive = %d, %v", value, ok)
		}
	}
	for _, tt := range []struct {
		name string
		run  func()
	}{
		{"closed send", func() { ch <- 1 }},
		{"closed close", func() { close(ch) }},
		{"nil close", func() { close(nilChannel) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("operation must panic")
				}
			}()
			tt.run()
		})
	}
}

func TestSyntaxHelpLanguageRules(t *testing.T) {
	for _, tt := range []struct {
		name, version, source string
		valid                 bool
	}{
		{"integer range before 1.22", "go1.21", "func f() { for range 3 {} }", false},
		{"integer range from 1.22", "go1.22", "func f() { for range 3 {} }", true},
		{"iterator range before 1.23", "go1.22", "func f() { for range func(yield func(int) bool) { yield(1) } {} }", false},
		{"iterator range from 1.23", "go1.23", "func f() { for range func(yield func(int) bool) { yield(1) } {} }", true},
		{"underlying type constraint", "go1.23", "type Count int; func twice[T ~int](v T) T { return v+v }; var _ = twice(Count(3))", true},
		{"overlapping union", "go1.23", "type I interface { ~int | int }", false},
		{"nonbasic interface value", "go1.23", "var x interface { ~int }", false},
		{"named underlying type", "go1.23", "type Count int; type I interface { ~Count }", false},
		{"comparable any from 1.20", "go1.20", "func equal[T comparable](a,b T) bool { return a==b }; var _ = equal[any](1,1)", true},
		{"comparable any before 1.20", "go1.19", "func equal[T comparable](a,b T) bool { return a==b }; var _ = equal[any](1,1)", false},
		{"receive only close", "go1.23", "func f(ch <-chan int) { close(ch) }", false},
		{"send only receive", "go1.23", "func f(ch chan<- int) { <-ch }", false},
		{"send only close", "go1.23", "func f(ch chan<- int) { close(ch) }", true},
		{"builtin shadow", "go1.23", "func f() { len := func(int) string { return \"shadow\" }; var s string = len(1); _ = s }", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "rule.go", "package main\n"+tt.source, parser.AllErrors)
			if err != nil {
				t.Fatal(err)
			}
			config := types.Config{GoVersion: tt.version}
			_, err = config.Check("catalog/rule", fset, []*ast.File{file}, nil)
			if (err == nil) != tt.valid {
				t.Errorf("valid = %v, want %v: %v", err == nil, tt.valid, err)
			}
		})
	}
}
