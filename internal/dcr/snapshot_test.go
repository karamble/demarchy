// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import "testing"

func ring(nicks ...string) []ringEntry {
	out := make([]ringEntry, 0, len(nicks))
	for _, n := range nicks {
		var e ringEntry
		e.Type = "gc-message"
		e.Payload.FromNick = n
		e.Payload.Message = n + " said something"
		out = append(out, e)
	}
	return out
}

func nicks(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.FromNick)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dcrpulse reverses its ring on read, so the resource arrives newest first.
// Reading it as though it grew at the end showed the panel the oldest messages
// and left the unread badge pinned to an entry that never moved.
func TestRingToMessagesTurnsTheRingTheRightWayRound(t *testing.T) {
	// Newest first, the way the resource sends it.
	raw := ring("newest", "middle", "oldest")

	got := nicks(ringToMessages(raw, 20))
	want := []string{"oldest", "middle", "newest"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The cap has to drop the oldest, not the newest.
func TestRingToMessagesKeepsTheNewest(t *testing.T) {
	raw := ring("n1", "n2", "n3", "n4", "n5")

	got := nicks(ringToMessages(raw, 2))
	want := []string{"n2", "n1"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v: the cap must drop the oldest", got, want)
	}
}

func TestRingToMessagesHandlesAnEmptyRing(t *testing.T) {
	if got := ringToMessages(nil, 20); len(got) != 0 {
		t.Fatalf("got %d messages from an empty ring, want 0", len(got))
	}
}
