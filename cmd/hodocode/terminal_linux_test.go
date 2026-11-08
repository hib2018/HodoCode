package main

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func terminalPTYName(t *testing.T, master *os.File) string {
	t.Helper()
	var unlock int32
	if err := terminalIOCTL(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatal(err)
	}
	var number uint32
	if err := terminalIOCTL(master, syscall.TIOCGPTN, unsafe.Pointer(&number)); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("/dev/pts/%d", number)
}
