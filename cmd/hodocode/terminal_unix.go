//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func terminalSupported(in, out *os.File) bool {
	for _, f := range []*os.File{in, out} {
		var state syscall.Termios
		if terminalIOCTL(f, terminalGetAttr, unsafe.Pointer(&state)) != nil {
			return false
		}
		if _, _, err := terminalSize(f); err != nil {
			return false
		}
	}
	return true
}

func withTerminal(in, out *os.File, run func(read func() (byte, bool, error), size func() (int, int, error)) error) (err error) {
	if !terminalSupported(in, out) {
		return errors.New("terminal requires tty input and output")
	}
	var original syscall.Termios
	if err = terminalIOCTL(in, terminalGetAttr, unsafe.Pointer(&original)); err != nil {
		return fmt.Errorf("read terminal attributes: %w", err)
	}
	defer func() {
		if restoreErr := terminalIOCTL(in, terminalSetAttr, unsafe.Pointer(&original)); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal attributes: %w", restoreErr))
		}
	}()

	raw := original
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1
	if err = terminalIOCTL(in, terminalSetAttr, unsafe.Pointer(&raw)); err != nil {
		return fmt.Errorf("set raw terminal attributes: %w", err)
	}

	// Register cleanup before entry: even a failed write may have changed the screen.
	defer func() {
		if _, restoreErr := io.WriteString(out, "\x1b[?25h\x1b[?1049l"); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore terminal screen: %w", restoreErr))
		}
	}()
	if _, err = io.WriteString(out, "\x1b[?1049h\x1b[?25l"); err != nil {
		return fmt.Errorf("enter terminal screen: %w", err)
	}

	read := func() (byte, bool, error) {
		var b [1]byte
		n, readErr := syscall.Read(int(in.Fd()), b[:])
		return b[0], n > 0, readErr
	}
	return run(read, func() (int, int, error) { return terminalSize(out) })
}

func terminalSize(f *os.File) (int, int, error) {
	var size struct {
		rows, cols, xpixel, ypixel uint16
	}
	if err := terminalIOCTL(f, syscall.TIOCGWINSZ, unsafe.Pointer(&size)); err != nil {
		return 0, 0, err
	}
	return int(size.cols), int(size.rows), nil
}

func terminalIOCTL(f *os.File, request uintptr, arg unsafe.Pointer) error {
	if f == nil {
		return os.ErrInvalid
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(arg))
	runtime.KeepAlive(f)
	if errno != 0 {
		return errno
	}
	return nil
}
