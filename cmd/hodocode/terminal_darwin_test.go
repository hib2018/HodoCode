package main

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func terminalPTYName(t *testing.T, master *os.File) string {
	t.Helper()
	for _, request := range []uintptr{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK} {
		if err := terminalIOCTL(master, request, nil); err != nil {
			t.Fatal(err)
		}
	}
	var name [128]byte
	if err := terminalIOCTL(master, syscall.TIOCPTYGNAME, unsafe.Pointer(&name[0])); err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimRight(name[:], "\x00"))
}
