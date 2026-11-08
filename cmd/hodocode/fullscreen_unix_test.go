//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFullScreenTerminal(t *testing.T) {
	for _, mode := range []string{"q", "Ctrl+C", "context cancellation", "TERM=dumb", "redirected input", "redirected output"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			if mode == "TERM=dumb" {
				t.Setenv("TERM", "dumb")
			}
			master, in, out := terminalPTY(t)
			original := terminalAttributes(t, in)
			fd := int(master.Fd())
			if err := syscall.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}

			input, output := in, out
			if mode == "redirected input" {
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { r.Close() })
				if _, err := w.WriteString("q\n"); err != nil {
					w.Close()
					t.Fatal(err)
				}
				w.Close()
				input = r
			}
			if mode == "redirected output" {
				var err error
				output, err = os.CreateTemp(t.TempDir(), "output")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { output.Close() })
			}
			fallback := mode == "TERM=dumb" || mode == "redirected input" || mode == "redirected output"
			args := []string{"tui", "--root", fixture(t), "--func", "Recur"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			baseline := runtime.NumGoroutine()
			var runErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				runErr = run(ctx, args, input, output, io.Discard, nil)
			}()

			var transcript strings.Builder
			// Drain every available byte, not just the entry escape: a frame can
			// exceed the PTY buffer and block run before it starts reading keys.
			drain := func() error {
				for {
					var buf [8192]byte
					n, err := syscall.Read(fd, buf[:])
					if n > 0 && mode != "redirected output" {
						transcript.Write(buf[:n])
					}
					if errors.Is(err, syscall.EINTR) {
						continue
					}
					if errors.Is(err, syscall.EAGAIN) || n == 0 && err == nil {
						break
					}
					if err != nil {
						return err
					}
				}
				if mode == "redirected output" {
					data, err := os.ReadFile(output.Name())
					if err != nil {
						return err
					}
					transcript.Reset()
					transcript.Write(data)
				}
				return nil
			}
			finished := false
			t.Cleanup(func() {
				cancel()
				if finished {
					return
				}
				// Also release a line-input fallback if an assertion failed early;
				// that path deliberately does not consume the context.
				_, _ = syscall.Write(fd, []byte("q\n"))
				timer := time.NewTimer(time.Second)
				defer timer.Stop()
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for {
					_ = drain()
					select {
					case <-done:
						return
					case <-tick.C:
					case <-timer.C:
						t.Error("run goroutine did not exit during cleanup")
						return
					}
				}
			})

			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			acted := false
			for !finished {
				if err := drain(); err != nil {
					t.Fatal(err)
				}
				text := transcript.String()
				ready := strings.Contains(text, terminalScreenEnter) && strings.Contains(text, "Tab:")
				if fallback {
					ready = strings.Contains(text, "qで終了 > ")
				}
				if ready && !acted {
					acted = true
					switch mode {
					case "context cancellation":
						cancel()
					case "redirected input": // q is already queued in the pipe.
					default:
						key := "q"
						if mode == "Ctrl+C" {
							key = "\x03"
						} else if fallback {
							key = "q\n"
						}
						if n, err := syscall.Write(fd, []byte(key)); err != nil || n != len(key) {
							t.Fatalf("write input = %d, %v", n, err)
						}
					}
				}
				select {
				case <-done:
					finished = true
				case <-tick.C:
				case <-timer.C:
					t.Fatalf("run did not exit within 5s; output: %q", transcript.String())
				}
			}
			if err := drain(); err != nil {
				t.Fatal(err)
			}
			if runErr != nil {
				t.Fatal(runErr)
			}
			terminalAssertRestored(t, in, original)
			text := transcript.String()
			for _, want := range []string{"main.go:34-37", "func Recur()", "Recur()", "Leaf()"} {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q: %q", want, text)
				}
			}
			if fallback {
				for _, escape := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?25l"} {
					if strings.Contains(text, escape) {
						t.Errorf("line fallback emitted %q", escape)
					}
				}
				if !strings.Contains(text, "qで終了 > ") {
					t.Error("line-input prompt missing")
				}
			} else {
				if !acted {
					t.Error("run exited before a full-screen frame was observed")
				}
				if !strings.HasPrefix(text, terminalScreenEnter) || !strings.HasSuffix(text, terminalScreenLeave) {
					t.Errorf("alternate screen/cursor not entered and restored: %q", text)
				}
				// The header's divider is separate from the source line-number bars.
				if !strings.Contains(text, "│main.go:34-37") {
					t.Error("source pane location and vertical divider missing")
				}
			}

			// The only test-owned goroutine is joined above. Wait for it to
			// disappear from the runtime count too, catching leaked reader workers.
			leakTimer := time.NewTimer(time.Second)
			defer leakTimer.Stop()
			for runtime.NumGoroutine() > baseline {
				select {
				case <-tick.C:
				case <-leakTimer.C:
					var stacks [65536]byte
					n := runtime.Stack(stacks[:], true)
					t.Fatalf("goroutines grew from %d to %d:\n%s", baseline, runtime.NumGoroutine(), stacks[:n])
				}
			}
		})
	}
}
