//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
)

func terminalSupported(in, out *os.File) bool { return false }

func withTerminal(in, out *os.File, run func(read func() (byte, bool, error), size func() (int, int, error)) error) error {
	return errors.New("terminal mode is unsupported on this platform")
}
