//go:build unix

package node

import "syscall"

// openNoBlock keeps an open from waiting on a FIFO swapped in after the
// type check (#447); a regular file reads as usual.
const openNoBlock = syscall.O_NONBLOCK
