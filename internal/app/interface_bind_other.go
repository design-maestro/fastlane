//go:build !linux

package app

import (
	"context"
	"fmt"
	"net"
)

func interfaceBoundDialContext(device string) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("binding an egress probe to interface %q is unsupported on this platform", device)
	}
}
