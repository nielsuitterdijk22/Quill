package egress

import "syscall"

// MarkSocket tags an upstream socket with Mark (needs CAP_NET_ADMIN).
func MarkSocket(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, Mark)
}
