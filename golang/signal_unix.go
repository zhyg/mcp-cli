//go:build !windows

package main

import "syscall"

// syscall0 is signal 0, used to check if a process exists.
var syscall0 = syscall.Signal(0)
