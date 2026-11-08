package main

import "syscall"

const (
	terminalGetAttr = syscall.TIOCGETA
	terminalSetAttr = syscall.TIOCSETA
)
