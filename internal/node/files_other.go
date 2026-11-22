//go:build !unix

package node

// openNoBlock is 0 where the open has no such flag (#447).
const openNoBlock = 0
