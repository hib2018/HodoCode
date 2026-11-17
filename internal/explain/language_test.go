package explain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func languageFixture(t *testing.T, language, name, source string) (*analyze.Project, string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, name, source)
	p, err := analyze.LoadLanguage(root, language)
	if err != nil {
		t.Fatal(err)
	}
	for id := range p.Functions {
		return p, id
	}
	t.Fatal("no function indexed")
	return nil, ""
}

func TestPrepareLanguageMasks(t *testing.T) {
	for _, tt := range []struct{ language, name, source string }{
		{"javascript", "app.js", "function entry() { const value = 'LITERAL_SECRET'; return value; } // COMMENT_SECRET\n"},
		{"typescript", "app.ts", "function entry(): string { const value: string = 'LITERAL_SECRET'; return value; } // COMMENT_SECRET\n"},
		{"python", "app.py", "def entry():\r\n    value = '''LITERAL_SECRET\r\nMORE_SECRET''' # COMMENT_SECRET\r\n    return value\r\n"},
		{"rust", "app.rs", "fn entry() { let value = r##\"LITERAL_SECRET\"##; } // COMMENT_SECRET\n"},
		{"zig", "app.zig", "fn entry() void { const value = \"LITERAL_SECRET\"; } // COMMENT_SECRET\n"},
		{"c", "app.c", "int entry(void) { const char *value = \"LITERAL_SECRET\"; return 42; } /* COMMENT_SECRET */\n"},
	} {
		t.Run(tt.language, func(t *testing.T) {
			p, id := languageFixture(t, tt.language, tt.name, tt.source)
			r, err := Prepare(p, id, "explain", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			snippet := r.snippets[id]
			for _, secret := range []string{"LITERAL_SECRET", "MORE_SECRET", "COMMENT_SECRET"} {
				if strings.Contains(snippet, secret) || strings.Contains(r.Preview(), secret) {
					t.Fatalf("leaked %s: %q", secret, snippet)
				}
			}
			calls := 0
			_, err = r.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
				calls++
				for _, secret := range []string{"LITERAL_SECRET", "MORE_SECRET", "COMMENT_SECRET"} {
					if strings.Contains(prompt, secret) {
						t.Fatalf("payload leaked %s", secret)
					}
				}
				if calls == 1 {
					selection, err := json.Marshal([]string{id})
					return string(selection), err
				}
				return "explanation", nil
			})
			if err != nil || calls != 2 {
				t.Fatalf("two-step explanation: calls=%d err=%v", calls, err)
			}
			original, err := p.Source(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(snippet) != len(original) {
				t.Fatal("mask changed offsets")
			}
			for i := range original {
				if (original[i] == '\n' || original[i] == '\r') && snippet[i] != original[i] {
					t.Fatal("mask changed line positions")
				}
			}
		})
	}
}

func TestPrepareLanguageFailsClosed(t *testing.T) {
	for _, change := range []string{"stale", "line", "end", "language", "wrong-supported-language", "ambiguous"} {
		t.Run(change, func(t *testing.T) {
			p, id := languageFixture(t, "python", "app.py", "def entry():\n    # COMMENT_SECRET\n    return 'LITERAL_SECRET'\n")
			fn := p.Functions[id]
			switch change {
			case "stale":
				write(t, p.Root, "app.py", "def entry():\n    return 'CHANGED_SECRET'\n")
			case "line":
				fn.Line++
			case "end":
				fn.End++
			case "language":
				fn.Language = "unsupported"
			case "wrong-supported-language":
				fn.Language = "javascript"
			case "ambiguous":
				write(t, p.Root, "app.py", "def entry():\n    return f'{LITERAL_SECRET}'\n")
			}
			p.Functions[id] = fn
			r, err := Prepare(p, id, "explain", nil, nil)
			if err == nil || r != nil {
				t.Fatalf("%s yielded sendable request: %v, %v", change, r, err)
			}
		})
	}
}

func TestPrepareLanguageExactSameLineDeclaration(t *testing.T) {
	p, id := languageFixture(t, "javascript", "app.js", "function entry() { return 1; } function other() { return 2; }\n")
	for key := range p.Functions {
		if strings.Contains(key, "entry") {
			id = key
		}
	}
	r, err := Prepare(p, id, "explain", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.snippets[id], "other") {
		t.Fatalf("adjacent declaration included: %q", r.snippets[id])
	}
	// A prepared request remains an immutable, masked snapshot after disk changes.
	if err := os.WriteFile(filepath.Join(p.Root, "app.js"), []byte("LATER_SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.snippets[id], "LATER_SECRET") {
		t.Fatal("snapshot changed")
	}
}

func TestPrepareGoRejectsTamperedLanguage(t *testing.T) {
	p := fixture(t)
	id := "example.org/demo.First"
	fn := p.Functions[id]
	fn.Language = "python"
	p.Functions[id] = fn
	if r, err := Prepare(p, id, "explain", nil, nil); err == nil || r != nil {
		t.Fatalf("wrong-language Go masking accepted: %v, %v", r, err)
	}
}
