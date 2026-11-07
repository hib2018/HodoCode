package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../testdata/analyze")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func TestCommands(t *testing.T) {
	root := fixture(t)
	for _, tc := range []struct {
		args []string
		want string
		bad  bool
	}{
		{[]string{"help"}, "Usage:", false},
		{[]string{"functions", "--root", root}, "main.go:12-32", false},
		{[]string{"deps", "--root", root, "--depth", "0"}, "flowchart TD", false},
		{[]string{"flow", "--root", root, "--func", "example.com/fixture.Leaf"}, "flowchart TD\n  n0[", false},
		{[]string{"flow", "--root", root, "--func", "Recur", "--expand", "example.com/fixture.Leaf"}, "flowchart TD", false},
		{[]string{"flow", "--root", root, "--func", "Leaf"}, "", true},
		{[]string{"flow", "--root", root, "--func", "Recur", "--expand", "Hidden"}, "", true},
		{[]string{"flow", "--root", root}, "", true},
		{[]string{"deps", "--root", root, "--depth", "-2"}, "", true},
		{[]string{"deps", "--root", root, "--dir", ".."}, "", true},
		{[]string{"flow", "--root", root, "extra"}, "", true},
		{[]string{"unknown"}, "", true},
		{[]string{"flow", "--help"}, "", false},
	} {
		t.Run(strings.Join(tc.args[:1], " ")+tc.want, func(t *testing.T) {
			var out, diag bytes.Buffer
			err := run(context.Background(), tc.args, strings.NewReader(""), &out, &diag, nil)
			if (err != nil) != tc.bad {
				t.Fatalf("err=%v", err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("output=%q; want %q", out.String(), tc.want)
			}
		})
	}
}
func TestFlowOnlyExpandsChosenFunction(t *testing.T) {
	p, err := analyze.Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	s, err := newFlow(p, "CycleA", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.edges) != 1 || len(s.expanded) != 1 {
		t.Fatalf("eager recursion: %+v", s)
	}
	b, err := p.Resolve("CycleB")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.expand(b); err != nil {
		t.Fatal(err)
	}
	if len(s.edges) != 2 || len(s.expanded) != 2 {
		t.Fatalf("cycle not deduplicated: %+v", s)
	}
	if err = s.expand(b); err != nil || len(s.edges) != 2 {
		t.Fatal("repeated expansion duplicated edges")
	}
}
func TestTUI(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"CycleA", "bad\n1\nq\n", "└─ ▶ CycleB（現在）"},
		{"CycleA", "q\n", "Enterで CycleB へ進む"},
		{"", "q\n", "番号で選択"},
		{"CycleA", "", "Enterで CycleB へ進む"},
		{"Unchecked", "", "呼び出し先不明: 1件"},
		{"Unchecked", "d\nq\n", "unknown: missingSymbol（呼び出し先不明・選択不可）"},
		{"example.com/fixture.Leaf", "", "Leaf（現在）"},
	} {
		var out bytes.Buffer
		err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", tc.name}, strings.NewReader(tc.input), &out, io.Discard, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatal(out.String())
		}
	}
}
func TestCurrentLabel(t *testing.T) {
	want := "▶ example.com/fixture.Recur（現在）"
	if got := currentLabel("example.com/fixture.Recur", false); got != want {
		t.Fatalf("plain current marker: %q", got)
	}
	if got := currentLabel("example.com/fixture.Recur", true); got != "\x1b[1m"+want+"\x1b[0m" {
		t.Fatalf("current node must be bold with styling reset afterwards: %q", got)
	}
}

func TestTUITreeAndCodeNavigation(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "Recur"}, strings.NewReader("1\nb\nq\n"), &out, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule := strings.Repeat("─", 60)
	recurCode := "\nmain.go:34-37\n" + rule + "\nfunc Recur() {\n\tRecur()\n\tLeaf()\n}\n" + rule + "\n"
	leafCode := "\nmain.go:38-38\n" + rule + "\nfunc Leaf()           {}\n" + rule + "\n"
	root := "\n▶ Recur（現在）\n├─ 1: Leaf\n└─ 2: Recur\n" + recurCode + "番号で選択、qで終了 > "
	leaf := "\nRecur\n└─ ▶ Leaf（現在）\n" + leafCode + "bで戻る、qで終了 > "
	if got, want := out.String(), root+leaf+root; got != want {
		t.Fatalf("expected tree, original code and prompt, including back navigation:\ngot: %s\nwant: %s", got, want)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("redirected output should not contain ANSI styling")
	}
}

func TestTUIHierarchyTracksRecursiveNavigation(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "Recur"}, strings.NewReader("2\n2\nb\nq\n"), &out, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule := strings.Repeat("─", 60)
	code := "\nmain.go:34-37\n" + rule + "\nfunc Recur() {\n\tRecur()\n\tLeaf()\n}\n" + rule + "\n"
	root := "\n▶ Recur（現在）\n├─ 1: Leaf\n└─ 2: Recur\n" + code + "番号で選択、qで終了 > "
	child := "\nRecur\n└─ ▶ Recur（現在）\n   ├─ 1: Leaf\n   └─ 2: Recur\n" + code + "番号で選択、bで戻る、qで終了 > "
	if out.String() != root+strings.Repeat(child, 3) {
		t.Fatalf("recursive navigation should show only one parent: %s", out.String())
	}
}

func TestTUIOnlyShowsCurrentCallees(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "Entry"}, strings.NewReader("2\n1\nb\nq\n"), &out, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, child, ok := strings.Cut(out.String(), "\nEntry\n└─ ▶ Recur（現在）\n")
	if !ok || !strings.HasPrefix(child, "   ├─ 1: Leaf\n   └─ 2: Recur\n") || strings.Contains(child, "1: Generic") || strings.Contains(child, "helper.Work") {
		t.Fatalf("choices leaked from caller into next level: %s", out.String())
	}
	leaf := "\nRecur\n└─ ▶ Leaf（現在）\n"
	if !strings.Contains(child, leaf) {
		t.Fatalf("grandparent should be hidden at leaf: %s", child)
	}
	out.Reset()
	err = run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "helper.Work"}, strings.NewReader(""), &out, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule := strings.Repeat("─", 60)
	want := "\n▶ Work（現在）\n   外部ソース: 1件、呼び出し先不明: 0件（選択不可・dで詳細表示）\n\nhelper/helper.go:5-5\n" + rule + "\n" + `func Work() { _ = strings.TrimSpace(" work ") }` + "\n" + rule + "\ndで詳細切替、qで終了 > "
	if out.String() != want {
		t.Fatalf("external call should be nonselectable, with original code below: %s", out.String())
	}
}

func TestExplainPreviewAndSend(t *testing.T) {
	args := []string{"explain", "--root", fixture(t), "--func", "Recur", "--purpose", "再帰処理を説明"}
	calls := 0
	runner := func(ctx context.Context, prompt string) (string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no timeout")
		}
		if calls == 1 {
			if strings.Contains(prompt, "func Recur()") {
				t.Fatal("code sent before scope selection")
			}
			selection, _ := json.Marshal([]string{"example.com/fixture.Recur"})
			return string(selection), nil
		}
		if !strings.Contains(prompt, "func Recur()") || strings.Contains(prompt, "func Entry(") {
			t.Fatal("wrong code scope")
		}
		return "事実: 再帰呼び出し (main.go:35)", nil
	}
	var out bytes.Buffer
	if err := run(context.Background(), args, strings.NewReader(""), &out, io.Discard, runner); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !strings.Contains(out.String(), "WARNING") {
		t.Fatal("preview transmitted code")
	}
	out.Reset()
	if err := run(context.Background(), append(args, "--send"), strings.NewReader(""), &out, io.Discard, runner); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(out.String(), "main.go:35") {
		t.Fatalf("calls %d output %s", calls, out.String())
	}
}
func TestErrorsPropagate(t *testing.T) {
	args := []string{"explain", "--root", fixture(t), "--func", "Recur", "--purpose", "flow", "--send"}
	want := errors.New("offline")
	err := run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard, func(context.Context, string) (string, error) { return "", want })
	if !errors.Is(err, want) {
		t.Fatalf("lost error: %v", err)
	}
	if err = run(context.Background(), append(args, "--timeout", "0s"), strings.NewReader(""), io.Discard, io.Discard, nil); err == nil {
		t.Fatal("invalid timeout accepted")
	}
}
