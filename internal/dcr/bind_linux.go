// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build linux

package dcr

import (
	"fmt"
	"syscall"
)

// bindToDevice pins a socket to one netdevice, which is the control that makes
// a plain-http exception safe.
//
// Checking the destination address is not enough on its own: with the tunnel
// down, a permitted mesh address is simply routed somewhere else, and a phone
// tether can even put the machine's own WAN address inside the same range. A
// bound socket cannot leave the named interface whatever the routing table
// says, so the packet is not sent rather than sent to a stranger. That also
// defeats a route injected by a hostile DHCP server.
//
// SO_BINDTODEVICE has not needed privilege since Linux 5.7.
func bindToDevice(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var setErr error
		if err := c.Control(func(fd uintptr) {
			setErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET,
				syscall.SO_BINDTODEVICE, iface)
		}); err != nil {
			return err
		}
		if setErr != nil {
			return fmt.Errorf("%w: could not bind the socket to %s: %v",
				ErrMeshDown, iface, setErr)
		}
		return nil
	}
}
