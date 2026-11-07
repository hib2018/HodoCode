package explain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLeafTargetWithoutEdges(t *testing.T) {
	p := fixture(t)
	const target = "example.org/demo.Second"
	r, err := Prepare(p, target, "explain the leaf", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Preview(), "Target: "+target+"\n") {
		t.Fatal(r.Preview())
	}
	calls := 0
	_, err = r.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		_, raw, ok := strings.Cut(prompt, "UNTRUSTED DATA:\n")
		var data struct {
			Target  string `json:"target"`
			Graph   []link `json:"graph"`
			Sources []struct {
				ID      string `json:"id"`
				Snippet string `json:"snippet"`
			} `json:"sources"`
		}
		if !ok || json.Unmarshal([]byte(raw), &data) != nil || data.Target != target || len(data.Graph) != 0 {
			t.Fatalf("step %d lost leaf target: %s", calls, prompt)
		}
		if calls == 1 {
			if !strings.Contains(prompt, "MUST include the target ID") {
				t.Fatal("selection does not require target")
			}
			return `["` + target + `"]`, nil
		}
		if len(data.Sources) != 1 || data.Sources[0].ID != target || !strings.Contains(data.Sources[0].Snippet, "func Second()") {
			t.Fatal("leaf source missing or scope expanded")
		}
		return "done", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestInvalidOrExcludedTarget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   string
		excludes []string
		linkTo   string
	}{
		{name: "empty"},
		{name: "short name", target: "First"},
		{name: "unknown", target: "example.org/demo.Missing"},
		{name: "whitespace", target: " example.org/demo.First"},
		{name: "excluded file", target: "example.org/demo.First", excludes: []string{"main.go"}},
		{name: "excluded directory", target: "example.org/demo/skip.Worker", excludes: []string{"skip"}},
		{name: "blocked path", target: "example.org/demo.Credentials"},
		{name: "excluded symlink target", target: "alias.First", excludes: []string{"main.go"}, linkTo: "main.go"},
		{name: "blocked symlink target", target: "alias.Credentials", linkTo: "credentials.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture(t)
			if tc.linkTo != "" {
				if err := os.Symlink(filepath.Join(p.Root, tc.linkTo), filepath.Join(p.Root, "alias.go")); err != nil {
					t.Fatal(err)
				}
				original := p.Functions["example.org/demo."+strings.TrimPrefix(tc.target, "alias.")]
				original.ID, original.File = tc.target, "alias.go"
				p.Functions[tc.target] = original
			}
			r, err := Prepare(p, tc.target, "purpose", nil, tc.excludes)
			if err == nil || r != nil {
				t.Fatalf("accepted target: request=%v err=%v", r, err)
			}
			calls := 0
			_, err = r.Explain(context.Background(), func(context.Context, string) (string, error) {
				calls++
				return "", nil
			})
			if err == nil || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
