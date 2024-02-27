//go:build !windows
// +build !windows

package remote

import (
	"context"
	"net"
)

func DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(ctx, network, addr)

	return conn, err
}
