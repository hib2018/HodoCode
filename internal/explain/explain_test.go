package explain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func write(t *testing.T, root, name, text string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) *analyze.Project {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.org/demo\n\ngo 1.23\n")
	write(t, root, "main.go", "package demo\nfunc First() {\n println(\"PASSWORD_123\") // COMMENT_SECRET\n Second()\n}\nfunc Second() { println('X') }\nfunc Unselected() { println(\"OTHER_SECRET\") }\n")
	write(t, root, "skip/worker.go", "package skip\nfunc Worker() {}\n")
	write(t, root, "credentials.go", "package demo\nfunc Credentials() {}\n")
	p, err := analyze.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func prepare(t *testing.T, p *analyze.Project, edges []analyze.Edge) *Request {
	t.Helper()
	r, err := Prepare(p, "example.org/demo.First", "処理の流れを知りたい", edges, []string{"skip"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExplainTwoStepsAndSnapshot(t *testing.T) {
	p := fixture(t)
	first, second := "example.org/demo.First", "example.org/demo.Second"
	r := prepare(t, p, []analyze.Edge{
		{From: first, To: second},
		{From: first, To: "example.org/demo.Credentials"},
		{From: first, To: "example.org/demo/skip.Worker"},
		{From: first, To: "external.CREDENTIAL_ABC"},
		{From: first, To: "lookup(\"RAW_CREDENTIAL\")", Unknown: true},
		{From: "example.org/demo/skip.Worker", To: first},
	})
	preview := r.Preview()
	for _, bad := range []string{"PASSWORD_123", "println", "Credentials", "skip/", "RAW_CREDENTIAL", "CREDENTIAL_ABC"} {
		if strings.Contains(preview, bad) {
			t.Fatalf("preview leaked %q", bad)
		}
	}
	if !strings.Contains(preview, "main.go:2-5") || !strings.Contains(preview, "User audit required") {
		t.Fatal(preview)
	}
	// Neither the project nor files remain authoritative after Prepare.
	p.Functions = nil
	write(t, p.Root, "main.go", "NEW_RAW_CONTENT")
	if err := os.RemoveAll(p.Root); err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, err := r.Explain(context.Background(), func(ctx context.Context, prompt string) (string, error) {
		calls++
		for _, bad := range []string{"PASSWORD_123", "COMMENT_SECRET", "OTHER_SECRET", "RAW_CREDENTIAL", "CREDENTIAL_ABC", "Credentials", "skip.Worker", "NEW_RAW_CONTENT"} {
			if strings.Contains(prompt, bad) {
				t.Errorf("step %d leaked %q", calls, bad)
			}
		}
		if !strings.Contains(prompt, `"target":"`+first+`"`) {
			t.Errorf("step %d missing target", calls)
		}
		if calls == 1 {
			if strings.Contains(prompt, "println") {
				t.Error("step1 contains source")
			}
			if !strings.Contains(prompt, second) || !strings.Contains(prompt, "unknown") {
				t.Error("missing graph")
			}
			return `["` + first + `"]`, nil
		}
		if calls != 2 {
			t.Fatalf("unexpected call %d", calls)
		}
		if !strings.Contains(prompt, "println") || strings.Contains(prompt, "func Second") || strings.Contains(prompt, "Unselected") || strings.Contains(prompt, `"to":"`+second+`"`) {
			t.Error("wrong selected scope")
		}
		if !strings.Contains(prompt, "日本語") || !strings.Contains(prompt, "file:line") {
			t.Error("missing instructions")
		}
		return "説明", nil
	})
	if err != nil || got != "説明" || calls != 2 {
		t.Fatalf("got %q, %v (%d calls)", got, err, calls)
	}
}

func TestSelectionsFailClosed(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	many := make([]string, 13)
	for i := range many {
		many[i] = "example.org/demo.First"
	}
	encoded, _ := json.Marshal(many)
	for _, reply := range []string{
		"", "null", "[]", `{}`, `"example.org/demo.First"`, `[null]`, `[1]`,
		`["main.go"]`, `["../../outside.go"]`, `["rm -rf /"]`,
		`["example.org/demo.Credentials"]`, `["example.org/demo/skip.Worker"]`,
		`["example.org/demo.First","example.org/demo.First"]`,
		`["example.org/demo.Second"]`, // Valid candidate, but omits the target.
		"```json\n[\"example.org/demo.First\"]\n```", `["example.org/demo.First"] trailing`,
		string(encoded), strings.Repeat("x", maxPrompt+1),
	} {
		t.Run(fmt.Sprintf("reply-%d-%s", len(reply), reply[:min(len(reply), 30)]), func(t *testing.T) {
			calls := 0
			_, err := r.Explain(context.Background(), func(context.Context, string) (string, error) { calls++; return reply, nil })
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestRedactEveryLiteralCommentAndPreserveOffsets(t *testing.T) {
	source := "package p\r\n// LINE_SECRET\r\nfunc F() {\r\n a := `RAW_SECRET\r\nSECOND_SECRET`\r\n b := \"QUOTED_SECRET\\\"TAIL\"\r\n c := '\\''\r\n /* BLOCK_SECRET\r\n END_SECRET */\r\n _, _, _ = a, b, c\r\n}\r\n"
	clean, err := redact([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(clean) != len(source) {
		t.Fatal("byte offsets changed")
	}
	for i := range source {
		if (source[i] == '\n' || source[i] == '\r') && clean[i] != source[i] {
			t.Fatalf("newline changed at %d", i)
		}
	}
	for _, bad := range []string{"SECRET", "SECOND", "TAIL", "\\'"} {
		if strings.Contains(clean, bad) {
			t.Fatalf("redaction leaked %q: %s", bad, clean)
		}
	}
	if !strings.Contains(clean, "func F()") || !strings.Contains(clean, "_, _, _ = a, b, c") {
		t.Fatal("non-token content lost")
	}
	for _, invalid := range []string{"package p\nvar x = \"unterminated", "package p\n/* unfinished", "package p\nvar x = '\\q'"} {
		if _, err := redact([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid source %q", invalid)
		}
	}
}

func TestPrepareValidation(t *testing.T) {
	p := fixture(t)
	for _, purpose := range []string{"", " \n\t", strings.Repeat("x", maxPurpose+1)} {
		if _, err := Prepare(p, "example.org/demo.First", purpose, nil, nil); err == nil {
			t.Error("accepted invalid purpose")
		}
	}
	for _, pattern := range []string{"", "/tmp/*", "../*", "a/../b", "./a", "a//b", "a/", "[", "a[", `C:\private`, `a\b`, "a\x00b", "a\nb"} {
		if _, err := Prepare(p, "example.org/demo.First", "purpose", nil, []string{pattern}); err == nil {
			t.Errorf("accepted exclude %q", pattern)
		}
	}
	if _, err := Prepare(nil, "example.org/demo.First", "purpose", nil, nil); err == nil {
		t.Error("accepted nil project")
	}
	for _, pattern := range []string{"skip", "s*", "skip/*.go", "*.go"} {
		t.Run(pattern, func(t *testing.T) {
			target := "example.org/demo.First"
			if pattern == "*.go" {
				target = "example.org/demo/skip.Worker"
			}
			r, err := Prepare(p, target, "purpose", nil, []string{pattern})
			if err != nil {
				t.Fatal(err)
			}
			if pattern == "*.go" {
				if strings.Contains(r.Preview(), "main.go") || !strings.Contains(r.Preview(), "skip/worker.go") {
					t.Fatal("filepath.Match patterns must not match across separators")
				}
				return
			}
			if strings.Contains(r.Preview(), "skip/") {
				t.Fatal("directory-prefix exclusion missed")
			}
		})
	}
	for _, name := range []string{"../outside.go", "/tmp/outside.go", `C:\outside.go`} {
		bad := &analyze.Project{Root: p.Root, Functions: map[string]analyze.Function{"x.F": {ID: "x.F", File: name, Line: 1, End: 1}}}
		if _, err := Prepare(bad, "x.F", "purpose", nil, nil); err == nil {
			t.Fatalf("accepted source path %q", name)
		}
	}
}

func TestSecretPathsAndSymlinkBoundaries(t *testing.T) {
	root := t.TempDir()
	p := &analyze.Project{Root: root, Functions: map[string]analyze.Function{}}
	for i, name := range []string{".env", ".env.local.go", "Credentials/a.go", "secret_store/a.go", "token.go", "private/a.go", "key.go", "certs/a.go", "password.go", "id_rsa.go"} {
		// They need not exist: blocked paths must never be opened.
		id := fmt.Sprintf("p.Blocked%d", i)
		p.Functions[id] = analyze.Function{ID: id, File: name, Line: 1, End: 1}
	}
	write(t, root, "safe.go", "package p\nfunc Safe() {}\n")
	p.Functions["p.Safe"] = analyze.Function{ID: "p.Safe", File: "safe.go", Line: 2, End: 2}
	r, err := Prepare(p, "p.Safe", "purpose", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.candidates) != 1 {
		t.Fatal(r.Preview())
	}
	outside := t.TempDir()
	write(t, outside, "outside.go", "package p\nfunc Outside() {}\n")
	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	p.Functions["p.Link"] = analyze.Function{ID: "p.Link", File: "link.go", Line: 2, End: 2}
	if _, err := Prepare(p, "p.Safe", "purpose", nil, nil); err == nil {
		t.Fatal("accepted escaping symlink")
	}
	delete(p.Functions, "p.Link")
	if err := os.Remove(filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "secret.go", "package p\nfunc Hidden() {}\n")
	if err := os.Symlink(filepath.Join(root, "secret.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	p.Functions["p.Link"] = analyze.Function{ID: "p.Link", File: "link.go", Line: 2, End: 2}
	r, err = Prepare(p, "p.Safe", "purpose", nil, nil)
	if err != nil || len(r.candidates) != 1 {
		t.Fatalf("secret target not blocked: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "safe.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	delete(p.Functions, "p.Link")
	p.Functions["alias.Safe"] = analyze.Function{ID: "alias.Safe", File: "link.go", Line: 2, End: 2}
	r, err = Prepare(p, "p.Safe", "purpose", nil, nil)
	if err != nil || len(r.candidates) != 2 {
		t.Fatalf("safe internal symlink: %v", err)
	}
	// Root-boundary checking must not accept sibling names sharing a prefix.
	sibling := root + "-sibling"
	if err := os.Mkdir(sibling, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sibling) })
	write(t, sibling, "source.go", "package p\nfunc F() {}\n")
	if err := os.Remove(filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sibling, "source.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(p, "p.Safe", "purpose", nil, nil); err == nil {
		t.Fatal("accepted prefix sibling")
	}
}

func TestLimitsAndErrors(t *testing.T) {
	p := fixture(t)
	r := prepare(t, p, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	run := func(context.Context, string) (string, error) { calls++; return "", errors.New("runner failed") }
	if _, err := r.Explain(ctx, run); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancellation: %v calls=%d", err, calls)
	}
	if _, err := r.Explain(context.Background(), run); err == nil || !strings.Contains(err.Error(), "runner failed") {
		t.Fatal(err)
	}
	if _, err := r.Explain(context.Background(), nil); err == nil {
		t.Fatal("nil runner")
	}
	if _, err := (&Request{}).Explain(context.Background(), run); err == nil {
		t.Fatal("zero request")
	}
	// Large retained source errors before sending step two, never truncates it.
	write(t, p.Root, "main.go", "package demo\nfunc Huge() {\n"+strings.Repeat(" println(1)\n", 14000)+"}\n")
	p.Functions = map[string]analyze.Function{"p.Huge": {ID: "p.Huge", File: "main.go", Line: 2, End: 14003}}
	r, err := Prepare(p, "p.Huge", "purpose", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	_, err = r.Explain(context.Background(), func(context.Context, string) (string, error) { calls++; return `["p.Huge"]`, nil })
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "prompt exceeds") {
		t.Fatalf("limit: %v calls=%d", err, calls)
	}
	// Oversized candidate metadata is rejected before any invocation.
	id := strings.Repeat("x", maxPrompt)
	p.Functions = map[string]analyze.Function{id: {ID: id, File: "main.go", Line: 2, End: 14003}}
	if _, err := Prepare(p, id, "purpose", nil, nil); err == nil {
		t.Fatal("oversized metadata accepted")
	}
}
