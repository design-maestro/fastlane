//go:build linux

package app

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func interfaceBoundDialContext(device string) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		if err := raw.Control(func(fd uintptr) {
			socketErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, device)
		}); err != nil {
			return err
		}
		return socketErr
	}}
	return func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}
}
