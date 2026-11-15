package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNeovimCommand(t *testing.T) {
	p := screenProject(t, map[string]string{"odd ' # % | name.go": "package main\nfunc main() {\n\tprintln(\"日本語\")\n}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.cursorRow = 1
	s.cursorColumn = len([]rune("\tprintln(\"日本"))
	path := filepath.Join(p.Root, "odd ' # % | name.go")
	for _, server := range []string{"", "/tmp/nvim socket"} {
		s.nvimServer = server
		cmd, err := s.neovimCommand(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"nvim", "+call cursor(3,17)", "--", path}
		if server != "" {
			want = []string{"nvim", "--server", server, "--remote-expr", "execute('edit ' . fnameescape('" + strings.ReplaceAll(path, "'", "''") + "')) . cursor(3,17)"}
		}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
}

func TestNeovimNormalModeOnly(t *testing.T) {
	for _, mode := range []string{"normal", "search", "search input", "agent input", "tree"} {
		t.Run(mode, func(t *testing.T) {
			s := agentTestScreen(t, nil)
			switch mode {
			case "search":
				s.search.query = "something"
			case "search input":
				s.search.input = &agentInput{}
			case "agent input":
				s.agent.input = &agentInput{id: s.current}
			case "tree":
				if err := s.openTree(); err != nil {
					t.Fatal(err)
				}
			}
			keys := []byte("v")
			read := func() (byte, bool, error) {
				if len(keys) == 0 {
					return 0, false, io.EOF
				}
				b := keys[0]
				keys = keys[1:]
				return b, true, nil
			}
			err := screenLoop(context.Background(), s, io.Discard, read, func() (int, int, error) { return 120, 24, nil }, false)
			if mode == "normal" {
				if err != errOpenNeovim {
					t.Fatalf("normal: %v", err)
				}
			} else if !errors.Is(err, io.EOF) {
				t.Fatalf("input triggered editor: %v", err)
			}
		})
	}
}

func TestReloadSourceAndDeletedFunction(t *testing.T) {
	const original = "package main\nfunc main() { Leaf() }\nfunc Leaf() { println(1) }\n"
	p := screenProject(t, map[string]string{"main.go": original})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	press(t, s, "\r")
	current := s.current
	if len(s.history) != 1 {
		t.Fatal("missing history")
	}
	path := filepath.Join(p.Root, "main.go")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(strings.ReplaceAll(original, "println(1)", "println(234)"))
	press(t, s, "r")
	if s.current != current || len(s.history) != 1 || !strings.Contains(strings.Join(s.code, "\n"), "println(234)") {
		t.Fatalf("reload lost state: %+v", s)
	}
	before := s.p
	write("package main\nfunc broken(\n")
	press(t, s, "r")
	if s.p != before || !strings.Contains(s.notice, "失敗") {
		t.Fatal("failed reload did not preserve snapshot")
	}
	write("package main\nfunc main() {}\n")
	press(t, s, "r")
	if !strings.HasSuffix(s.current, ".main") || !strings.Contains(s.notice, "なくなった") {
		t.Fatalf("deleted function fallback: %s / %s", s.current, s.notice)
	}
	press(t, s, "b")
	if !strings.HasSuffix(s.current, ".main") {
		t.Fatal("history unusable after reload")
	}
}

func TestReloadInputKeysAreText(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.input = &agentInput{id: s.current}
	var out bytes.Buffer
	keys := []byte("vr")
	read := func() (byte, bool, error) {
		if len(keys) == 0 {
			return 0, false, io.EOF
		}
		b := keys[0]
		keys = keys[1:]
		return b, true, nil
	}
	old := s.p
	_ = screenLoop(context.Background(), s, &out, read, func() (int, int, error) { return 120, 24, nil }, false)
	if s.p != old || s.agent.input.text != "vr" {
		t.Fatal("normal shortcuts consumed input")
	}
}

func TestNeovimFirstLineColumn(t *testing.T) {
	p := screenProject(t, map[string]string{"main.go": "package main\nfunc A() {}; func main() {}\n"})
	s, err := newScreen(p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := s.neovimCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != "+call cursor(2,14)" {
		t.Fatalf("first-line physical column: %q", cmd.Args)
	}
}
