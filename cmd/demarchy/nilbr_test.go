// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"github.com/karamble/demarchy/internal/dcr"
)

// A token without the bisonrelay domain yields a reachable snapshot with no BR
// section at all. Folding it into the unread counter must be a no-op, not a nil
// dereference: that was a crash on first fetch for every node-only token.
func TestFoldSkipsASnapshotWithoutBR(t *testing.T) {
	var count counter
	count.fold(&dcr.Snapshot{Reachable: true})
	count.fold(nil)
	if got := count.unread().Total; got != 0 {
		t.Fatalf("nothing to count, got %d", got)
	}

	// And a snapshot that does carry a ring still primes and counts as before.
	with := &dcr.Snapshot{Reachable: true, BR: &dcr.BR{Messages: []dcr.Message{gc("a", "one")}}}
	count.fold(with)
	with.BR.Messages = append(with.BR.Messages, gc("b", "two"))
	count.fold(with)
	if got := count.unread().Total; got != 1 {
		t.Fatalf("one arrival after priming, got %d", got)
	}
}
