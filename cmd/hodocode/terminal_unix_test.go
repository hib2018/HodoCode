//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const terminalScreenEnter = "\x1b[?1049h\x1b[?25l"
const terminalScreenLeave = "\x1b[?25h\x1b[?1049l"

func terminalPTY(t *testing.T) (master, in, out *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENODEV) {
			t.Skipf("PTY unavailable: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { master.Close() })
	name := terminalPTYName(t, master)
	open := func() *os.File {
		f, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	in, out = open(), open()
	terminalSetSize(t, out, 91, 33)
	return master, in, out
}

func terminalSetSize(t *testing.T, f *os.File, cols, rows uint16) {
	t.Helper()
	size := [4]uint16{rows, cols, 0, 0}
	if err := terminalIOCTL(f, syscall.TIOCSWINSZ, unsafe.Pointer(&size)); err != nil {
		t.Fatal(err)
	}
}

func terminalAttributes(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()
	var state syscall.Termios
	if err := terminalIOCTL(f, terminalGetAttr, unsafe.Pointer(&state)); err != nil {
		t.Fatal(err)
	}
	return state
}

func terminalAssertRestored(t *testing.T, f *os.File, original syscall.Termios) {
	t.Helper()
	if got := terminalAttributes(t, f); !reflect.DeepEqual(got, original) {
		t.Errorf("termios not restored: got %+v, want %+v", got, original)
	}
}

func terminalAssertOutput(t *testing.T, master *os.File, want string) {
	t.Helper()
	fd := int(master.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	var got []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var buf [256]byte
		n, err := syscall.Read(fd, buf[:])
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			t.Fatal(err)
		}
		if len(got) >= len(want) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if string(got) != want {
		t.Errorf("screen output = %q, want %q", got, want)
	}
}

func TestTerminalRejectsNonTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for _, pair := range [][2]*os.File{{nil, nil}, {r, w}} {
		if terminalSupported(pair[0], pair[1]) {
			t.Error("non-tty accepted")
		}
		called := false
		err := withTerminal(pair[0], pair[1], func(func() (byte, bool, error), func() (int, int, error)) error {
			called = true
			return nil
		})
		if err == nil || called {
			t.Errorf("withTerminal = %v, callback called = %v", err, called)
		}
	}
	_, in, out := terminalPTY(t)
	if !terminalSupported(in, out) || terminalSupported(r, out) || terminalSupported(in, w) {
		t.Error("both input and output must be real ttys")
	}
}

func TestTerminalRawReadAndSize(t *testing.T) {
	master, in, out := terminalPTY(t)
	original := terminalAttributes(t, in)
	err := withTerminal(in, out, func(read func() (byte, bool, error), size func() (int, int, error)) error {
		raw := terminalAttributes(t, in)
		if raw.Lflag&(syscall.ECHO|syscall.ICANON|syscall.ISIG|syscall.IEXTEN) != 0 || raw.Oflag&syscall.OPOST != 0 || raw.Cc[syscall.VMIN] != 0 || raw.Cc[syscall.VTIME] != 1 {
			t.Errorf("unexpected raw mode: %+v", raw)
		}
		for _, dimensions := range [][2]uint16{{91, 33}, {105, 42}} {
			terminalSetSize(t, out, dimensions[0], dimensions[1])
			cols, rows, err := size()
			if err != nil || cols != int(dimensions[0]) || rows != int(dimensions[1]) {
				t.Errorf("size = (%d,%d,%v), want %v", cols, rows, err, dimensions)
			}
		}
		start := time.Now()
		_, ok, err := read()
		if err != nil || ok || time.Since(start) < 50*time.Millisecond {
			t.Errorf("timeout read = (_, %v, %v), elapsed %v", ok, err, time.Since(start))
		}
		// Queue both bytes: each read must consume exactly one, including Ctrl+C.
		if _, err := master.Write([]byte{3, 'x'}); err != nil {
			return err
		}
		for _, want := range []byte{3, 'x'} {
			b, ok, err := read()
			if err != nil || !ok || b != want {
				t.Errorf("read = (%d,%v,%v), want (%d,true,nil)", b, ok, err, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	terminalAssertRestored(t, in, original)
	terminalAssertOutput(t, master, terminalScreenEnter+terminalScreenLeave)
}

func TestTerminalRestoration(t *testing.T) {
	for _, mode := range []string{"normal", "error", "panic", "entry failure", "screen cleanup failure", "termios cleanup failure"} {
		t.Run(mode, func(t *testing.T) {
			master, in, out := terminalPTY(t)
			original := terminalAttributes(t, in)
			sentinel := errors.New("callback error")
			if mode == "entry failure" {
				readonly, err := os.OpenFile(out.Name(), os.O_RDONLY|syscall.O_NOCTTY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer readonly.Close()
				out = readonly
			}
			called := false
			var err error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				err = withTerminal(in, out, func(func() (byte, bool, error), func() (int, int, error)) error {
					called = true
					switch mode {
					case "panic":
						panic(sentinel)
					case "error":
						return sentinel
					case "screen cleanup failure":
						out.Close()
						return sentinel
					case "termios cleanup failure":
						in.Close()
						return sentinel
					}
					return nil
				})
			}()
			if mode == "panic" {
				if panicked != sentinel {
					t.Errorf("panic = %v, want sentinel", panicked)
				}
			} else if panicked != nil {
				t.Fatalf("unexpected panic: %v", panicked)
			}
			switch mode {
			case "normal":
				if err != nil {
					t.Error(err)
				}
			case "error":
				if !errors.Is(err, sentinel) {
					t.Errorf("callback error lost: %v", err)
				}
			case "entry failure":
				if called || err == nil || !strings.Contains(err.Error(), "enter terminal screen") || !strings.Contains(err.Error(), "restore terminal screen") {
					t.Errorf("entry and cleanup errors = %v, callback called = %v", err, called)
				}
			case "screen cleanup failure", "termios cleanup failure":
				label := "restore terminal screen"
				if mode == "termios cleanup failure" {
					label = "restore terminal attributes"
				}
				if !errors.Is(err, sentinel) || !errors.Is(err, syscall.EBADF) && !errors.Is(err, os.ErrClosed) || !strings.Contains(err.Error(), label) {
					t.Errorf("joined callback and cleanup errors = %v", err)
				}
			}
			if mode != "termios cleanup failure" {
				terminalAssertRestored(t, in, original)
			}
			want := terminalScreenEnter + terminalScreenLeave
			if mode == "entry failure" {
				want = ""
			}
			if mode == "screen cleanup failure" {
				want = terminalScreenEnter
			}
			terminalAssertOutput(t, master, want)
		})
	}
}
