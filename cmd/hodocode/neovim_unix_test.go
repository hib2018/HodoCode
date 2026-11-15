//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNeovimTerminal(t *testing.T) {
	for _, mode := range []string{"success", "failure", "missing", "remote success", "remote failure"} {
		t.Run(mode, func(t *testing.T) {
			remote := strings.HasPrefix(mode, "remote ")
			failure := strings.HasSuffix(mode, "failure")
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("NO_COLOR", "")
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			argsPath := filepath.Join(bin, "args")
			t.Setenv("HODOCODE_NVIM_ARGS", argsPath)
			if mode != "missing" {
				exit := "0"
				if failure {
					exit = "7"
				}
				// Block local launches in cooked mode so the test can inspect termios.
				body := "printf 'NVIM-RUNNING\\n'\nread answer\n"
				if remote {
					body = ""
					if failure {
						body = "printf 'E42: server unavailable\\n' >&2\n"
					}
				}
				script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$HODOCODE_NVIM_ARGS\"\n" + body + "exit " + exit + "\n"
				if err := os.WriteFile(filepath.Join(bin, "nvim"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}

			master, in, out := terminalPTY(t)
			original := terminalAttributes(t, in)
			if original.Lflag&(syscall.ECHO|syscall.ICANON) != syscall.ECHO|syscall.ICANON {
				t.Fatal("PTY did not start in cooked mode")
			}
			fd := int(master.Fd())
			if err := syscall.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			root := fixture(t)
			args := []string{"tui", "--root", root, "--func", "Entry"}
			if remote {
				args = append(args, "--nvim-server", "/tmp/explicit nvim")
			}
			go func() {
				done <- run(ctx, args, in, out, io.Discard, nil)
			}()
			var transcript strings.Builder
			drain := func() error {
				for {
					var buf [8192]byte
					n, err := syscall.Read(fd, buf[:])
					if n > 0 {
						transcript.Write(buf[:n])
					}
					if errors.Is(err, syscall.EINTR) {
						continue
					}
					if errors.Is(err, syscall.EAGAIN) || n == 0 && err == nil {
						return nil
					}
					if err != nil {
						return err
					}
				}
			}
			finished := false
			t.Cleanup(func() {
				cancel()
				if finished {
					return
				}
				_, _ = syscall.Write(fd, []byte("\nq\n"))
				deadline := time.Now().Add(time.Second)
				for time.Now().Before(deadline) {
					_ = drain()
					select {
					case <-done:
						return
					default:
						time.Sleep(time.Millisecond)
					}
				}
				t.Error("run did not exit during cleanup")
			})
			write := func(keys string) {
				t.Helper()
				if n, err := syscall.Write(fd, []byte(keys)); err != nil || n != len(keys) {
					t.Fatalf("write input = %d, %v", n, err)
				}
			}
			wait := func(want string) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if err := drain(); err != nil {
						t.Fatal(err)
					}
					text := transcript.String()
					if i := strings.LastIndex(text, terminalScreenEnter); i >= 0 {
						text = text[i:]
					}
					if strings.Contains(text, want) {
						return
					}
					select {
					case err := <-done:
						finished = true
						t.Fatalf("run exited before %q: %v; output: %q", want, err, transcript.String())
					default:
						time.Sleep(time.Millisecond)
					}
				}
				t.Fatalf("waiting for %q; output: %q", want, transcript.String())
			}

			wait("\x1b[?25h")
			transcript.Reset()
			// Enter Recur from Entry, then move off its initial call position.
			write("\rlv")
			if mode != "missing" && !remote {
				wait("NVIM-RUNNING")
				terminalAssertRestored(t, in, original)
				if !strings.Contains(transcript.String(), terminalScreenLeave) || strings.Count(transcript.String(), terminalScreenEnter) != 0 {
					t.Errorf("Neovim ran before leaving the alternate screen: %q", transcript.String())
				}
				write("return\n")
			}
			wait(terminalScreenEnter)
			notice := "Neovimから復帰しました"
			if remote {
				notice = "Neovimへ送信しました"
			}
			if failure || mode == "missing" {
				notice = "Neovimの起動に失敗しました"
			}
			wait(notice)
			wait("\x1b[3;10H\x1b[?25h")
			if failure && !strings.Contains(transcript.String(), "exit status 7") {
				t.Errorf("missing process error: %q", transcript.String())
			}
			if remote && failure && !strings.Contains(transcript.String(), "E42: server unavailable") {
				t.Errorf("missing server stderr: %q", transcript.String())
			}
			if got := terminalAttributes(t, in); got.Lflag&(syscall.ECHO|syscall.ICANON) != 0 {
				t.Errorf("terminal not raw after reentry: %+v", got)
			}
			transcript.Reset()
			write("b")
			wait("main.go:14:3")
			wait("\x1b[?25h")
			write("q")
			select {
			case err := <-done:
				finished = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("q did not exit after Neovim return")
			}
			if err := drain(); err != nil {
				t.Fatal(err)
			}
			if mode != "missing" {
				got, err := os.ReadFile(argsPath)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "main.go")
				want := "+call cursor(35,3)\n--\n" + path + "\n"
				if remote {
					quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
					want = "--server\n/tmp/explicit nvim\n--remote-expr\nexecute('edit ' . fnameescape(" + quoted + ")) . cursor(35,3)\n"
				}
				// Appending arguments makes any fallback or duplicate launch visible.
				if string(got) != want {
					t.Errorf("nvim arguments = %q, want one call with %q", got, want)
				}
			}
			terminalAssertRestored(t, in, original)
			if !strings.HasSuffix(transcript.String(), terminalScreenLeave) {
				t.Errorf("alternate screen not restored on exit: %q", transcript.String())
			}
		})
	}
}
