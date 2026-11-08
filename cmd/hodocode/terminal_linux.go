package main

import "syscall"

const (
	terminalGetAttr = syscall.TCGETS
	terminalSetAttr = syscall.TCSETS
)
