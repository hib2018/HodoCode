package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTUIResolvesAcrossRepositoryModules(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"app/go.mod":               "module example.com/app\n",
		"app/main.go":              "package app\nimport \"example.com/library/worker\"\nfunc Entry() { worker.Work() }\n",
		"library/go.mod":           "module example.com/library\n",
		"library/worker/worker.go": "package worker\nfunc Work() { Next() }\nfunc Next() {}\n",
	} {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"tui", "--root", root, "--func", "example.com/app.Entry"}
	var out bytes.Buffer
	if err := run(context.Background(), args, strings.NewReader("q\n"), &out, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "└─ 1: worker.Work\n") || strings.Contains(out.String(), "外部ソース") || strings.Contains(out.String(), "func Work()") || strings.Contains(out.String(), "1: Next") {
		t.Fatalf("direct call should resolve across modules without expanding its body: %s", out.String())
	}
	out.Reset()
	if err := run(context.Background(), args, strings.NewReader("1\nq\n"), &out, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "└─ ▶ Work（現在）\n   └─ 1: Next\n") || !strings.Contains(out.String(), "library/worker/worker.go:2-2\n") || !strings.Contains(out.String(), "func Work() { Next() }") || strings.Contains(out.String(), "func Next()") {
		t.Fatalf("selected cross-module function should display source and its own direct calls only: %s", out.String())
	}
}
