//go:build !darwin && !linux

package main

import "testing"

func TestTerminalUnsupported(t *testing.T) {
	if terminalSupported(nil, nil) {
		t.Fatal("terminal unexpectedly supported")
	}
	called := false
	err := withTerminal(nil, nil, func(func() (byte, bool, error), func() (int, int, error)) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("withTerminal = %v, callback called = %v", err, called)
	}
}
