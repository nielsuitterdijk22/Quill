//go:build !linux

package egress

import "errors"

// MarkSocket is Linux-only (the CI instances); elsewhere enforcement is off.
func MarkSocket(fd uintptr) error { return errors.New("egress: socket marks need Linux") }
