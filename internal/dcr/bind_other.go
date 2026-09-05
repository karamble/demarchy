// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build !linux

package dcr

import (
	"fmt"
	"runtime"
	"syscall"
)

// bindToDevice has no portable equivalent, so the exception is unusable off
// Linux rather than silently unenforced. The plugin is a Quickshell widget and
// only runs on Linux; this keeps `go build` and `go vet` honest elsewhere.
func bindToDevice(iface string) func(network, address string, c syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error {
		return fmt.Errorf("%w: binding a socket to %s needs Linux, this is %s",
			ErrMeshDown, iface, runtime.GOOS)
	}
}
