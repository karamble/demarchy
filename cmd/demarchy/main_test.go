// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"github.com/karamble/demarchy/internal/dcr"
)

func gc(nick, text string) dcr.Message {
	return dcr.Message{Type: "gc-message", FromNick: nick, Text: text, GCID: "g1"}
}

func pm(nick, text string) dcr.Message {
	return dcr.Message{Type: "pm", FromNick: nick, Text: text}
}

// The counter runs on every fetch, and most fetches carry a ring that has not
// moved: those must credit nothing. Getting this wrong once made the badge
// climb on its own every 20 seconds, and getting it wrong the other way left it
// stuck at zero while messages arrived.
func TestCounterOnlyCountsWhatIsNew(t *testing.T) {
	ring := []dcr.Message{gc("a", "one"), gc("b", "two")}

	var c counter
	c.observe(ring, true, true)
	if got := c.unread().Total; got != 0 {
		t.Fatalf("priming counted %d, want 0: what is already in the ring is history", got)
	}

	c.observe(ring, true, true)
	if got := c.unread().Total; got != 0 {
		t.Fatalf("an unchanged ring counted %d, want 0", got)
	}

	ring = append(ring, gc("c", "three"), pm("d", "four"))
	c.observe(ring, true, true)
	if got := c.unread(); got.Total != 2 || got.Groupchat != 1 || got.Private != 1 {
		t.Fatalf("after two arrivals got %+v, want 1 group and 1 private", got)
	}

	c.observe(ring, true, true)
	if got := c.unread().Total; got != 2 {
		t.Fatalf("re-reading the same ring moved the count to %d, want 2", got)
	}
}

// Once the ring is full it stops growing and drops from the front, so a length
// comparison sees nothing at all. The tail it saw last is still in there.
func TestCounterSurvivesAFullRing(t *testing.T) {
	ring := []dcr.Message{gc("a", "one"), gc("b", "two"), gc("c", "three")}

	var c counter
	c.observe(ring, true, true)

	rolled := []dcr.Message{gc("b", "two"), gc("c", "three"), gc("d", "four")}
	c.observe(rolled, true, true)
	if got := c.unread().Total; got != 1 {
		t.Fatalf("a rolled ring of the same length counted %d, want 1", got)
	}
}

// Away long enough and everything it saw has been pushed off the front. All
// that can be said then is that the visible ring is new.
func TestCounterHandlesAFullyRolledRing(t *testing.T) {
	var c counter
	c.observe([]dcr.Message{gc("a", "one")}, true, true)

	fresh := []dcr.Message{gc("x", "nine"), gc("y", "ten")}
	c.observe(fresh, true, true)
	if got := c.unread().Total; got != 2 {
		t.Fatalf("a fully rolled ring counted %d, want 2", got)
	}
}

func TestCounterRespectsTheToggles(t *testing.T) {
	ring := []dcr.Message{gc("a", "one")}

	var c counter
	c.observe(ring, true, true)
	ring = append(ring, gc("b", "group"), pm("c", "private"))

	c.observe(ring, true, false)
	if got := c.unread(); got.Total != 1 || got.Private != 1 {
		t.Fatalf("with group chats off got %+v, want the private one only", got)
	}
}

// Opening the panel clears the tally but must not re-count the backlog it just
// dismissed.
func TestResetKeepsTheBaseline(t *testing.T) {
	ring := []dcr.Message{gc("a", "one")}

	var c counter
	c.observe(ring, true, true)
	ring = append(ring, gc("b", "two"))
	c.observe(ring, true, true)

	c.reset()
	c.observe(ring, true, true)
	if got := c.unread().Total; got != 0 {
		t.Fatalf("after reset an unchanged ring counted %d, want 0", got)
	}
}
