// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func fp(x float64) *float64 { return &x }
func bp(b bool) *bool       { return &b }

// oneShot is a valid one-shot trigger on connection "c" that expires in a month.
func oneShot(path, op string, p Params) *Trigger {
	return &Trigger{
		ID: NewTriggerID(), Rev: 1, Connection: "c", Path: path, Operator: op, Params: p,
		DeliverTo: "you", ExpiresAt: t0.Add(30 * 24 * time.Hour), ArmedAt: t0, ArmedBy: "you",
	}
}

func standing(path, op string, p Params) *Trigger {
	tr := oneShot(path, op, p)
	tr.Once = bp(false)
	return tr
}

type sample struct {
	v  Value
	ok bool
	at time.Duration
}

func n(f float64, at time.Duration) sample {
	return sample{Value{Kind: KindNumber, Num: f}, true, at}
}

func txt(s string, at time.Duration) sample {
	return sample{Value{Kind: KindText, Text: s}, true, at}
}

func flag(b bool, at time.Duration) sample {
	return sample{Value{Kind: KindBool, Bool: b}, true, at}
}

func lst(at time.Duration, items ...Item) sample {
	return sample{Value{Kind: KindList, Items: items}, true, at}
}

// gap is a pass on which the path could not be resolved.
func gap(at time.Duration) sample { return sample{Value{}, false, at} }

// item builds a list member the way Resolve would, from the leaf's own
// identity, so a test cannot drift from the fields the catalogue names.
func item(t *testing.T, path string, fields map[string]string) Item {
	t.Helper()
	leaf, ok := LookupLeaf(path)
	if !ok {
		t.Fatalf("%s is not in the catalogue", path)
	}
	for k := range fields {
		if !containsString(leaf.Fields, k) {
			t.Fatalf("%s has no field %q, it has %v", path, k, leaf.Fields)
		}
	}
	id := make([]string, 0, len(leaf.Identity))
	for _, k := range leaf.Identity {
		id = append(id, fields[k])
	}
	return Item{Fields: fields, ID: id}
}

func msg(t *testing.T, nick, text string) Item {
	return item(t, "br.messages", map[string]string{"type": "pm", "fromNick": nick, "text": text, "gcid": ""})
}

func channel(t *testing.T, alias string) Item {
	return item(t, "lightning.list", map[string]string{"alias": alias})
}

// textPath finds a text leaf so the becomes tests follow the catalogue.
func textPath(t *testing.T) string {
	t.Helper()
	for _, l := range Catalogue() {
		if l.Kind == KindText && containsString(l.Operators, "becomes") {
			return l.Path
		}
	}
	t.Fatal("the catalogue has no text leaf")
	return ""
}

// run feeds every sample through Step and returns the indexes that fired.
func run(t *testing.T, tr *Trigger, samples []sample) []int {
	t.Helper()
	if err := ValidateTrigger(tr); err != nil {
		t.Fatalf("test trigger is invalid: %v", err)
	}
	var fires []int
	for i, s := range samples {
		if fired, _, _ := Step(tr, s.v, s.ok, t0.Add(s.at)); fired {
			fires = append(fires, i)
		}
	}
	return fires
}

func wantFires(t *testing.T, got, want []int) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("fired at samples %v, want %v", got, want)
	}
}

const s = time.Second

func TestCrossesFiresOnTheTransitionOnly(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	got := run(t, tr, []sample{n(15, 0), n(15.5, 8*s), n(16.2, 16*s), n(16.5, 24*s), n(17, 32*s)})
	wantFires(t, got, []int{2})
	if tr.State.FireCount != 1 || tr.State.FiredAt == nil || !tr.State.FiredAt.Equal(t0.Add(16*s)) {
		t.Fatalf("state after the fire: %+v", tr.State)
	}
}

// Sitting exactly on the bound is not past it: "below 4" means fewer than 4.
func TestCrossesIsStrict(t *testing.T) {
	tr := standing("node.peers", "crosses", Params{Bound: fp(4), Direction: "below"})
	wantFires(t, run(t, tr, []sample{n(6, 0), n(4, 8*s), n(4, 16*s), n(3, 24*s)}), []int{3})
}

// A value already past the bound when armed is history, not an event.
func TestFirstSampleNeverFires(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	got := run(t, tr, []sample{n(17, 0), n(17.1, 8*s), n(15, 16*s), n(16.1, 24*s)})
	wantFires(t, got, []int{3})

	tr = standing(textPath(t), "becomes", Params{Value: "stopped"})
	wantFires(t, run(t, tr, []sample{txt("stopped", 0), txt("stopped", 8*s)}), nil)

	tr = standing("lightning.list", "appears", Params{})
	wantFires(t, run(t, tr, []sample{lst(0, channel(t, "a"), channel(t, "b"))}), nil)
}

// Jitter around the bound rings once; clearing the band by the default 1%
// re-arms, and the next crossing rings again.
func TestCrossesHysteresis(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	got := run(t, tr, []sample{
		n(15.9, 0), n(16.01, 8*s), n(15.95, 16*s), n(16.05, 24*s), n(15.9, 32*s), n(16.2, 40*s),
		n(15.5, 48*s), n(16.1, 56*s),
	})
	wantFires(t, got, []int{1, 7})
}

func TestCrossesExplicitRearm(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above", Rearm: fp(2)})
	got := run(t, tr, []sample{n(15, 0), n(16.5, 8*s), n(14.5, 16*s), n(16.5, 24*s), n(13.9, 32*s), n(16.5, 40*s)})
	wantFires(t, got, []int{1, 5})
}

// Integers re-arm by one whole step, so peers must climb back to 6 before a
// second drop below 4 rings.
func TestCrossesIntegerRearm(t *testing.T) {
	tr := standing("node.peers", "crosses", Params{Bound: fp(4), Direction: "below"})
	got := run(t, tr, []sample{n(6, 0), n(3, 8*s), n(4, 16*s), n(5, 24*s), n(3, 32*s), n(6, 40*s), n(3, 48*s)})
	wantFires(t, got, []int{1, 6})
}

func TestMissingSampleTouchesNothing(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	Step(tr, n(15, 0).v, true, t0)
	before, _ := json.Marshal(tr.State)
	fired, flush, dirty := Step(tr, Value{}, false, t0.Add(8*s))
	after, _ := json.Marshal(tr.State)
	if fired || flush || dirty || string(before) != string(after) {
		t.Fatalf("a missing sample changed something: %v %v %v\n%s\n%s", fired, flush, dirty, before, after)
	}
	if fired, _, _ := Step(tr, n(16.5, 16*s).v, true, t0.Add(16*s)); !fired {
		t.Fatal("the transition after a gap should fire")
	}
}

func TestBecomesReArmsAfterTheHold(t *testing.T) {
	tr := standing(textPath(t), "becomes", Params{Value: "stopped", Hold: "1m"})
	got := run(t, tr, []sample{
		txt("running", 0), txt("stopped", 10*s), txt("running", 20*s), txt("stopped", 30*s),
		txt("running", 40*s), txt("stopped", 50*s), txt("running", 60*s), txt("running", 119*s),
		txt("stopped", 125*s), txt("running", 130*s), txt("running", 190*s), txt("stopped", 200*s),
	})
	wantFires(t, got, []int{1, 11})
}

func TestBecomesOnABool(t *testing.T) {
	tr := oneShot("wallet.synced", "becomes", Params{Value: "true"})
	wantFires(t, run(t, tr, []sample{flag(false, 0), flag(true, 8*s), flag(false, 16*s), flag(true, 24*s)}), []int{1})
	if !tr.Spent() {
		t.Fatal("a one-shot that fired is spent")
	}
}

func TestChangesMeasuresFromABaseline(t *testing.T) {
	tr := standing("node.height", "changes", Params{By: fp(1)})
	wantFires(t, run(t, tr, []sample{n(100, 0), n(100, 8*s), n(101, 16*s), n(101, 24*s), n(102, 32*s)}), []int{2, 4})

	// A slow drift of small steps fires when the sum reaches the threshold,
	// which comparing with the previous sample alone never would.
	tr = standing("price.dcrUsd", "changes", Params{By: fp(1)})
	wantFires(t, run(t, tr, []sample{n(20, 0), n(20.4, 8*s), n(20.8, 16*s), n(21.2, 24*s), n(21.6, 32*s)}), []int{3})

	tr = standing("price.dcrUsd", "changes", Params{By: fp(5), Percent: true})
	wantFires(t, run(t, tr, []sample{n(20, 0), n(20.5, 8*s), n(21, 16*s), n(21.5, 24*s), n(22.1, 32*s), n(20, 40*s)}), []int{2, 4, 5})
}

func TestChangesPercentFromZeroNeverFires(t *testing.T) {
	tr := standing("price.dcrUsd", "changes", Params{By: fp(10), Percent: true})
	wantFires(t, run(t, tr, []sample{n(0, 0), n(1, 8*s), n(100, 16*s)}), nil)
}

func TestStallsOncePerEpisode(t *testing.T) {
	tr := standing("node.height", "stalls", Params{For: "1m"})
	got := run(t, tr, []sample{
		n(100, 0), n(100, 30*s), n(100, 59*s), n(100, 60*s), n(100, 70*s), n(100, 600*s),
		n(101, 610*s), n(101, 669*s), n(101, 670*s),
	})
	wantFires(t, got, []int{3, 8})
}

// The clock a stall reads lives in the store, so a helper restart in the
// middle of the episode changes nothing about when it fires.
func TestStallsAcrossARestart(t *testing.T) {
	tr := standing("node.height", "stalls", Params{For: "1m"})
	wantFires(t, run(t, tr, []sample{n(100, 0), n(100, 30*s)}), nil)

	raw, err := json.Marshal(Triggers{Version: 1, Triggers: []Trigger{*tr}})
	if err != nil {
		t.Fatal(err)
	}
	var fresh Triggers
	if err := json.Unmarshal(raw, &fresh); err != nil {
		t.Fatal(err)
	}
	again := &fresh.Triggers[0]
	wantFires(t, run(t, again, []sample{n(100, 59*s), n(100, 60*s), n(100, 61*s)}), []int{1})
}

// An outage is not movement: dcrpulse gone for two hours and back with the
// same height has been stalled for two hours.
func TestStallsAfterAGapFiresOnTheFirstSample(t *testing.T) {
	tr := standing("node.height", "stalls", Params{For: "45m"})
	got := run(t, tr, []sample{n(100, 0), gap(60 * s), gap(120 * s), n(100, 2*time.Hour)})
	wantFires(t, got, []int{3})
}

func TestAppearsWithAFilterAndRingRollover(t *testing.T) {
	tr := standing("br.messages", "appears", Params{Where: map[string]string{"fromNick": "alice"}})
	got := run(t, tr, []sample{
		lst(0, msg(t, "bob", "hi")),
		lst(8*s, msg(t, "bob", "hi"), msg(t, "alice", "yo")),
		lst(16*s, msg(t, "alice", "yo")),
		lst(24*s, msg(t, "alice", "yo"), msg(t, "alice", "again")),
		lst(32*s, msg(t, "bob", "x"), msg(t, "alice", "again")),
		lst(40*s, msg(t, "bob", "y"), msg(t, "bob", "z")),
	})
	wantFires(t, got, []int{1, 3})
}

func TestAppearsSubstringFilter(t *testing.T) {
	tr := standing("br.messages", "appears", Params{Where: map[string]string{"text~": "INVOICE"}})
	got := run(t, tr, []sample{
		lst(0, msg(t, "bob", "hi")),
		lst(8*s, msg(t, "bob", "hi"), msg(t, "bob", "the invoice is due")),
		lst(16*s, msg(t, "bob", "hi"), msg(t, "bob", "the invoice is due"), msg(t, "bob", "no")),
	})
	wantFires(t, got, []int{1})
}

func TestDisappears(t *testing.T) {
	tr := standing("lightning.list", "disappears", Params{})
	got := run(t, tr, []sample{
		lst(0, channel(t, "a"), channel(t, "b")),
		lst(8*s, channel(t, "b"), channel(t, "a")),
		lst(16*s, channel(t, "a")),
		lst(24*s, channel(t, "a")),
		lst(32 * s),
		lst(40*s, channel(t, "c")),
	})
	wantFires(t, got, []int{2, 4})
}

// Two identical messages are one member of the set. Documented, not fixed:
// the alternative is storing every message.
func TestDuplicateMessagesCollapse(t *testing.T) {
	tr := standing("br.messages", "appears", Params{})
	got := run(t, tr, []sample{
		lst(0, msg(t, "alice", "yo")),
		lst(8*s, msg(t, "alice", "yo"), msg(t, "alice", "yo")),
	})
	wantFires(t, got, nil)
}

func TestCountCrossesTheFilteredLength(t *testing.T) {
	a := func(i int) Item { return msg(t, "alice", fmt.Sprint("m", i)) }
	tr := standing("br.messages", "count", Params{Bound: fp(2), Direction: "above", Where: map[string]string{"fromNick": "alice"}})
	got := run(t, tr, []sample{
		lst(0, a(1)),
		lst(8*s, a(1), a(2), msg(t, "bob", "x")),
		lst(16*s, a(1), a(2), a(3)),
		lst(24*s, a(1), a(2), a(3), a(4)),
		lst(32*s, a(1)),
		lst(40 * s),
		lst(48*s, a(1), a(2), a(3)),
	})
	wantFires(t, got, []int{2, 6})
}

// A series that repeats a value still counts every entry.
func TestCountOfScalarsCountsRepeats(t *testing.T) {
	leaf, ok := LookupLeaf("price.series")
	if !ok || !containsString(leaf.Operators, "count") {
		t.Fatalf("price.series should offer count: %+v", leaf)
	}
	same := func(k int) []Item {
		items := make([]Item, k)
		for i := range items {
			items[i] = Item{ID: []string{"1"}}
		}
		return items
	}
	tr := standing("price.series", "count", Params{Bound: fp(2), Direction: "above"})
	wantFires(t, run(t, tr, []sample{lst(0, same(1)...), lst(8*s, same(3)...)}), []int{1})
}

func TestExpiredIsNotEvaluated(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	tr.ExpiresAt = t0.Add(30 * s)
	got := run(t, tr, []sample{n(15, 0), n(15.5, 20*s), n(16.5, 60*s)})
	wantFires(t, got, nil)
	if !tr.Expired(t0.Add(60 * s)) {
		t.Fatal("should read as expired")
	}
}

func TestOneShotIsSpentAndStandingRefires(t *testing.T) {
	samples := []sample{n(15, 0), n(16.5, 8*s), n(15, 16*s), n(17, 24*s)}
	once := oneShot("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	wantFires(t, run(t, once, samples), []int{1})
	if !once.Spent() || once.State.FireCount != 1 {
		t.Fatalf("one-shot state: %+v", once.State)
	}
	// Spent means untouched, not merely unfired.
	before, _ := json.Marshal(once.State)
	Step(once, n(15, 32*s).v, true, t0.Add(32*s))
	if after, _ := json.Marshal(once.State); string(before) != string(after) {
		t.Fatalf("a spent trigger kept evaluating:\n%s\n%s", before, after)
	}

	std := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	wantFires(t, run(t, std, samples), []int{1, 3})
	if std.Spent() || std.State.FireCount != 2 {
		t.Fatalf("standing state: %+v", std.State)
	}
}

func TestFlushAndDirty(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	check := func(when time.Duration, v float64, wantFired, wantFlush, wantDirty bool) {
		t.Helper()
		fired, flush, dirty := Step(tr, n(v, when).v, true, t0.Add(when))
		if fired != wantFired || flush != wantFlush || dirty != wantDirty {
			t.Fatalf("at %s value %v: fired %v flush %v dirty %v, want %v %v %v",
				when, v, fired, flush, dirty, wantFired, wantFlush, wantDirty)
		}
	}
	check(0, 15, false, true, true)       // baseline is flushed
	check(8*s, 15, false, false, false)   // nothing moved
	check(16*s, 15.5, false, false, true) // ordinary movement is debounced
	check(24*s, 16.5, true, true, true)   // a fire is flushed
	check(32*s, 16.6, false, false, true) // sitting past the bound, disarmed
	check(40*s, 15.5, false, true, true)  // re-arming is flushed
}

// A clock that went backwards must not make a stall that already happened.
func TestClockSkewIsClamped(t *testing.T) {
	tr := standing("node.height", "stalls", Params{For: "1m"})
	Step(tr, n(100, 0).v, true, t0.Add(60*s))
	fired, _, _ := Step(tr, n(100, 0).v, true, t0)
	if fired || !tr.State.LastChangedAt.Equal(t0) {
		t.Fatalf("skew: fired %v, last change %v", fired, tr.State.LastChangedAt)
	}
	if fired, _, _ := Step(tr, n(100, 0).v, true, t0.Add(59*s)); fired {
		t.Fatal("fired before a minute had passed from the clamped clock")
	}
	if fired, _, _ := Step(tr, n(100, 0).v, true, t0.Add(60*s)); !fired {
		t.Fatal("should fire a minute after the clamped clock")
	}
}

func TestFireResetsDelivery(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	Step(tr, n(15, 0).v, true, t0)
	tr.State.Delivered = "agent"
	tr.State.DeliveryError = "stale"
	if fired, _, _ := Step(tr, n(17, 8*s).v, true, t0.Add(8*s)); !fired {
		t.Fatal("should fire")
	}
	if tr.State.Delivered != "" || tr.State.DeliveryError != "" {
		t.Fatalf("a fresh fire must clear the old delivery: %+v", tr.State)
	}
}

// An operator the leaf does not offer, or a sample of the wrong kind, is a
// no-op rather than a fire or a panic.
func TestMismatchedSampleIsIgnored(t *testing.T) {
	tr := standing("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	Step(tr, n(15, 0).v, true, t0)
	if fired, flush, dirty := Step(tr, Value{Kind: KindText, Text: "17"}, true, t0.Add(8*s)); fired || flush || dirty {
		t.Fatal("a text sample on a number leaf changed something")
	}
	if fired, _, _ := Step(tr, n(17, 16*s).v, true, t0.Add(16*s)); !fired {
		t.Fatal("the next good sample should still fire")
	}
	bad := standing("price.dcrUsd", "appears", Params{})
	if fired, _, _ := Step(bad, n(1, 0).v, true, t0); fired || bad.State.LastValue != nil {
		t.Fatal("an operator the leaf does not offer must be inert")
	}
}
