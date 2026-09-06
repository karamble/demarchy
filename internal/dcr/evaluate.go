// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Step folds one sample of a trigger's leaf into its state and says whether it
// rang.
//
// It is pure: no I/O, and the clock comes in as now. Everything the evaluator
// remembers between two samples lives in t.State, which the caller persists, so
// a helper restart changes nothing about when a stall fires.
//
// fired means ring now. flush means the state must be written immediately
// rather than on the caller's debounce: a first baseline, a fire, or a change
// in readiness, each of which would be wrong to lose to a restart. dirty means
// something in the state moved at all.
func Step(t *Trigger, cur Value, ok bool, now time.Time) (fired, flush, dirty bool) {
	// A missing sample is not a transition. The section is not granted, or the
	// fetch failed, or the connection is not this one. Touching nothing is what
	// keeps a stall clock honest across an outage: if dcrpulse is gone for two
	// hours and comes back with the height unchanged, it has been stalled for
	// two hours, and the first sample says so.
	if !ok || t.Spent() || t.Expired(now) {
		return false, false, false
	}
	leaf, found := LookupLeaf(t.Path)
	if !found || !containsString(leaf.Operators, t.Operator) {
		return false, false, false
	}
	cur, kind := reduce(t, leaf, cur)
	if cur.Kind != kind {
		return false, false, false
	}
	st := &t.State

	// A clock that went backwards must not turn into a stall that already
	// happened.
	if st.LastChangedAt != nil && st.LastChangedAt.After(now) {
		st.LastChangedAt = ptrTime(now)
		dirty = true
	}

	prev, havePrev := decodeValue(st.LastValue, kind)
	if !havePrev {
		// The first sample after arming is history, not an event. A trigger
		// reports transitions it witnessed, so a value already past the bound
		// does not ring: an agent that wanted "is it already there" could
		// have looked when it armed.
		st.LastValue = encode(cur)
		st.LastChangedAt = ptrTime(now)
		if t.Operator == "changes" {
			st.RefValue = encode(cur)
		}
		st.Ready = true
		return false, true, true
	}

	moved := !equalValue(prev, cur)
	if moved {
		st.LastChangedAt = ptrTime(now)
	}
	wasReady := st.Ready

	switch t.Operator {
	case "crosses":
		fired = stepCrosses(t, prev, cur, leaf.Integer)
	case "count":
		fired = stepCrosses(t, prev, cur, true)
	case "becomes":
		fired = stepBecomes(t, prev, cur, now)
	case "changes":
		fired = stepChanges(t, prev, cur)
	case "stalls":
		fired = stepStalls(t, moved, now)
	case "appears", "disappears":
		fired = stepList(t, prev, cur)
	}

	if fired {
		st.FiredAt = ptrTime(now)
		st.FireCount++
		st.Delivered = ""
		st.DeliveryError = ""
		// Every fire disarms, and each operator re-arms on its own terms once
		// the condition has been clearly false again. That one bit is the whole
		// of the hysteresis: a price wobbling across a bound rings once. The
		// operators where every event is distinct stay armed.
		if !alwaysReady(t.Operator) {
			st.Ready = false
		}
	}
	st.LastValue = encode(cur)

	readyMoved := st.Ready != wasReady
	flush = fired || readyMoved
	dirty = dirty || moved || fired || readyMoved
	return fired, flush, dirty
}

// reduce turns a list sample into what its operator actually compares: the
// filtered members for appears and disappears, or their number for count.
// Scalars pass straight through. The kind returned is the kind the stored
// state must decode as.
func reduce(t *Trigger, leaf Leaf, cur Value) (Value, Kind) {
	if leaf.Kind != KindList {
		return cur, leaf.Kind
	}
	items := filterItems(cur.Items, t.Params)
	if t.Operator == "count" {
		// Counted directly rather than through the key set, so a series that
		// repeats a value still counts every entry.
		return Value{Kind: KindNumber, Num: float64(len(items))}, KindNumber
	}
	return Value{Kind: KindList, Items: items}, KindList
}

// filterItems keeps the members a where clause names, and re-keys them when the
// trigger asked for its own identity fields. The filter runs before keying, so
// the stored state is only ever the members that matter to this trigger and a
// disappearance can be judged without persisting any field of any item.
func filterItems(items []Item, p Params) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if !matches(it, p.Where) {
			continue
		}
		if len(p.Key) > 0 {
			id := make([]string, 0, len(p.Key))
			for _, k := range p.Key {
				id = append(id, it.Fields[k])
			}
			it.ID = id
		}
		out = append(out, it)
	}
	return out
}

// matches applies a where clause: every entry must hold. A key ending in a
// tilde is a case-insensitive substring match, which is the difference between
// "the invoice message" and "a message whose text is exactly invoice".
func matches(it Item, where map[string]string) bool {
	for k, want := range where {
		if strings.HasSuffix(k, "~") {
			have := strings.ToLower(it.Fields[strings.TrimSuffix(k, "~")])
			if !strings.Contains(have, strings.ToLower(want)) {
				return false
			}
			continue
		}
		if it.Fields[k] != want {
			return false
		}
	}
	return true
}

// stepCrosses fires on the transition past a bound, never while sitting past
// it. It is "becomes" over a derived boolean, which is why it can only ring on
// the sample where that boolean flips.
//
// Re-arming needs the value to have come back to the far side of the bound by
// a margin, so a value that jitters either side of it rings once. The margin
// defaults to one whole step for integers and one percent of the bound for
// everything else. Re-arming implies not past the bound, so the sample that
// re-arms can never itself fire.
func stepCrosses(t *Trigger, prev, cur Value, integer bool) bool {
	if t.Params.Bound == nil {
		return false
	}
	bound := *t.Params.Bound
	above := t.Params.Direction == "above"
	past := func(v float64) bool {
		if above {
			return v > bound
		}
		return v < bound
	}
	if !t.State.Ready {
		margin := rearmMargin(t.Params.Rearm, bound, integer)
		far := (above && cur.Num < bound-margin) || (!above && cur.Num > bound+margin)
		if far {
			t.State.Ready = true
		}
	}
	return t.State.Ready && !past(prev.Num) && past(cur.Num)
}

func rearmMargin(explicit *float64, bound float64, integer bool) float64 {
	if explicit != nil {
		return *explicit
	}
	if integer {
		return 1
	}
	return math.Abs(bound) * 0.01
}

// stepBecomes fires when a text or bool takes the named value, having not had
// it on the previous sample. It re-arms after the value has been away for the
// hold time, so a status flapping every few seconds rings once and a status
// that settles elsewhere for five minutes may ring again.
func stepBecomes(t *Trigger, prev, cur Value, now time.Time) bool {
	is := func(v Value) bool { return textOf(v) == t.Params.Value }
	if !t.State.Ready && !is(cur) && t.State.LastChangedAt != nil {
		if now.Sub(*t.State.LastChangedAt) >= spanOr(t.Params.Hold, 5*time.Minute) {
			t.State.Ready = true
		}
	}
	return t.State.Ready && !is(prev) && is(cur)
}

func textOf(v Value) string {
	switch v.Kind {
	case KindText:
		return v.Text
	case KindBool:
		return strconv.FormatBool(v.Bool)
	case KindNumber:
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	}
	return ""
}

// stepChanges measures against a baseline rather than the previous sample, so
// a slow drift over an hour fires as surely as a spike between two samples.
// Firing moves the baseline to the current value, which is its hysteresis.
func stepChanges(t *Trigger, prev, cur Value) bool {
	if t.Params.By == nil {
		return false
	}
	ref, ok := decodeValue(t.State.RefValue, KindNumber)
	if !ok {
		ref = prev
		t.State.RefValue = encode(prev)
	}
	var delta float64
	if t.Params.Percent {
		if ref.Num == 0 {
			// A percentage of nothing is not a number. Documented: a percent
			// trigger armed at zero never fires until the baseline moves.
			return false
		}
		delta = math.Abs(cur.Num-ref.Num) / math.Abs(ref.Num) * 100
	} else {
		delta = math.Abs(cur.Num - ref.Num)
	}
	if delta >= *t.Params.By {
		t.State.RefValue = encode(cur)
		return true
	}
	return false
}

// stepStalls fires once the value has sat still for the duration. Movement
// re-arms it, so each stall episode rings once. The clock it reads is
// last_changed_at, which lives in the store, which is what makes a stall that
// spans a helper restart still fire on time.
func stepStalls(t *Trigger, moved bool, now time.Time) bool {
	if moved {
		t.State.Ready = true
		return false
	}
	if !t.State.Ready || t.State.LastChangedAt == nil {
		return false
	}
	return now.Sub(*t.State.LastChangedAt) >= spanOr(t.Params.For, 0)
}

// stepList fires when a member enters or leaves the filtered set. Both samples
// are compared as sets of fingerprints, so order is presentation and a list
// re-sorted by size has not changed. Each new member is a distinct event, so
// these stay armed.
func stepList(t *Trigger, prev, cur Value) bool {
	before := keySet(prev)
	after := keySet(cur)
	if t.Operator == "appears" {
		for k := range after {
			if !before[k] {
				return true
			}
		}
		return false
	}
	for k := range before {
		if !after[k] {
			return true
		}
	}
	return false
}

func keySet(v Value) map[string]bool {
	set := make(map[string]bool, len(v.Items))
	for _, it := range v.Items {
		set[it.key()] = true
	}
	return set
}

// alwaysReady names the operators for which every event is its own event, so
// a fire does not disarm them.
func alwaysReady(op string) bool {
	return op == "changes" || op == "appears" || op == "disappears"
}

func encode(v Value) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// spanOr reads a Go duration, falling back when it is empty or malformed. The
// store validates these at load, so the fallback is for a zero value, never a
// typo.
func spanOr(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func ptrTime(t time.Time) *time.Time { return &t }
