package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/semantictest"
)

func TestLanguageCommands(t *testing.T) {
	semantictest.Use(t)
	root := t.TempDir()
	for name, source := range map[string]string{
		"main.py": "import os\ndef helper():\n    return 1\ndef main():\n    helper()\n",
		"main.js": "function helper() {}\nfunction main() { helper(); }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runCommand := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(context.Background(), append(args, "--root", root), strings.NewReader("q\n"), &out, io.Discard, nil)
		return out.String(), err
	}
	out, err := runCommand("functions")
	if err != nil || !strings.Contains(out, "main.py") || !strings.Contains(out, "main.js") {
		t.Fatalf("auto: %s %v", out, err)
	}
	out, err = runCommand("functions", "--language", "js")
	if err != nil || strings.Contains(out, "main.py") || !strings.Contains(out, "main.js") {
		t.Fatalf("js: %s %v", out, err)
	}
	out, err = runCommand("flow", "--language", "python", "--func", "main")
	if err != nil || !strings.Contains(out, "helper") {
		t.Fatalf("flow: %s %v", out, err)
	}
	out, err = runCommand("deps", "--language", "python")
	if err != nil || !strings.Contains(out, "os") {
		t.Fatalf("deps: %s %v", out, err)
	}
	out, err = runCommand("tui", "--language", "python", "--func", "main")
	if err != nil || !strings.Contains(out, "helper()") {
		t.Fatalf("tui: %s %v", out, err)
	}
	if _, err := runCommand("flow", "--func", "main"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("mixed-language main must be ambiguous: %v", err)
	}
	if _, err := runCommand("functions", "--language", "unknown"); err == nil {
		t.Fatal("unknown language accepted")
	}
}
