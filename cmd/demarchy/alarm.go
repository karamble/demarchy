// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// saveEvery is how often ordinary movement, a new last_value with no fire and
// no change of readiness, is written back. A baseline, a fire, a re-arm and a
// delivery result are written at once. The cost of the debounce is that a
// stall which spans a restart can fire up to this much early in the
// oscillate-and-return case; it can never fire spuriously.
const saveEvery = 60 * time.Second

// ringResult is what a delivery goroutine reports back to the loop.
type ringResult struct {
	id  string
	how string
	err error
}

// alarms folds every snapshot through the armed triggers, rings the ones that
// fire, and keeps the store's runtime state current. It is driven from the
// watch loop's goroutine only: deliveries run in the background and come back
// through results, so no field here needs a lock.
type alarms struct {
	ring    ringer
	results chan ringResult
	now     func() time.Time

	// list is the store as last read, with our State laid over it. The CLI
	// owns definitions and rev; we own State; the merge is by rev.
	list    *dcr.Triggers
	loadErr error
	// stamp is the file as last read or written, so an arm from the CLI is
	// noticed on the next pass without re-reading an unchanged file every
	// eight seconds.
	stamp fileStamp
	// sampled remembers, per trigger, whether the last pass could resolve its
	// path, which is what the panel shows as no-sample.
	sampled  map[string]bool
	dirty    bool
	lastSave time.Time
}

type fileStamp struct {
	exists bool
	size   int64
	mod    time.Time
}

func (s fileStamp) same(o fileStamp) bool {
	return s.exists == o.exists && s.size == o.size && s.mod.Equal(o.mod)
}

func statTriggers() fileStamp {
	info, err := os.Stat(dcr.TriggersPath())
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, size: info.Size(), mod: info.ModTime()}
}

func newAlarms(r ringer) *alarms {
	return &alarms{
		ring:    r,
		results: make(chan ringResult, 16),
		now:     time.Now,
		list:    &dcr.Triggers{},
		sampled: map[string]bool{},
	}
}

// sync re-reads the store if it has changed on disk since we last saw it.
func (a *alarms) sync() {
	if statTriggers().same(a.stamp) {
		return
	}
	a.reload()
}

// reload reads the store and lays our state over every trigger whose
// definition has not moved. A matching rev means the definition on disk is the
// one our state was computed against; a bumped rev means the CLI edited it and
// reset the state itself.
func (a *alarms) reload() {
	disk, err := dcr.LoadTriggers()
	a.stamp = statTriggers()
	if err != nil {
		a.loadErr = err
		return
	}
	a.loadErr = nil
	a.list = a.merge(disk)
}

func (a *alarms) merge(disk *dcr.Triggers) *dcr.Triggers {
	mine := make(map[string]*dcr.Trigger, len(a.list.Triggers))
	for i := range a.list.Triggers {
		t := &a.list.Triggers[i]
		mine[t.ID] = t
	}
	sampled := make(map[string]bool, len(disk.Triggers))
	for i := range disk.Triggers {
		d := &disk.Triggers[i]
		if m, ok := mine[d.ID]; ok && m.Rev == d.Rev {
			d.State = m.State
		}
		if v, ok := a.sampled[d.ID]; ok {
			sampled[d.ID] = v
		}
	}
	a.sampled = sampled
	return disk
}

// observe folds one snapshot through every trigger armed on conn. Triggers on
// other connections are left alone entirely: a switch turns every field into a
// different number, and none of those are transitions.
func (a *alarms) observe(ctx context.Context, snap *dcr.Snapshot, conn string) {
	a.sync()
	if a.loadErr != nil || snap == nil || !snap.Reachable || conn == "" {
		return
	}
	now := a.now()
	flush := false
	for i := range a.list.Triggers {
		t := &a.list.Triggers[i]
		if t.Connection != conn || t.Spent() || t.Expired(now) {
			continue
		}
		cur, ok := dcr.Resolve(snap, t.Path)
		a.sampled[t.ID] = ok
		fired, f, dirty := dcr.Step(t, cur, ok, now)
		a.dirty = a.dirty || dirty
		flush = flush || f
		if fired {
			a.dispatch(ctx, t, now)
		}
	}
	a.save(flush)
}

// dispatch hands one fire to the ringer in the background. Everything the
// goroutine needs is copied out first: the slice it points into is replaced on
// the next reload.
func (a *alarms) dispatch(ctx context.Context, t *dcr.Trigger, at time.Time) {
	id, target, text := t.ID, t.DeliverTo, dcr.FireText(t, at)
	fmt.Fprintln(os.Stderr, "alarm:", id, t.Path, t.Operator, "to", target)
	go func() {
		how, err := a.ring.Ring(ctx, target, text)
		select {
		case a.results <- ringResult{id: id, how: how, err: err}:
		case <-ctx.Done():
		}
	}()
}

// settle records where a fire landed and writes it at once, so a crash after
// this point does not ring the agent twice on the next start.
func (a *alarms) settle(r ringResult) {
	t, ok := a.list.Find(r.id)
	if !ok {
		return
	}
	if r.err != nil {
		t.State.DeliveryError = r.err.Error()
		fmt.Fprintln(os.Stderr, "alarm:", r.id, "not delivered:", r.err)
	} else {
		t.State.Delivered = r.how
		t.State.DeliveryError = ""
		fmt.Fprintln(os.Stderr, "alarm:", r.id, "delivered by", r.how)
	}
	a.dirty = true
	a.save(true)
}

// recover re-rings whatever fired and was never delivered, so a crash between
// the fire and its delivery does not lose the alarm. Fired at is kept as the
// time in the text: the event happened then, not now.
func (a *alarms) recover(ctx context.Context) {
	for i := range a.list.Triggers {
		t := &a.list.Triggers[i]
		if t.State.FiredAt != nil && t.State.Delivered == "" {
			a.dispatch(ctx, t, *t.State.FiredAt)
		}
	}
}

// save writes our state back under the lock. Definitions are taken from disk
// on the way, so an arm or disarm that landed since the last read is neither
// lost nor reverted by us.
func (a *alarms) save(force bool) {
	if !a.dirty || a.loadErr != nil {
		return
	}
	now := a.now()
	if !force && now.Sub(a.lastSave) < saveEvery {
		return
	}
	err := dcr.WithTriggerLock(func() error {
		disk, err := dcr.LoadTriggers()
		if err != nil {
			return err
		}
		merged := a.merge(disk)
		if err := dcr.SaveTriggers(merged); err != nil {
			return err
		}
		a.list = merged
		return nil
	})
	a.stamp = statTriggers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "triggers:", err)
		return
	}
	a.dirty = false
	a.lastSave = now
}

// views is what the panel gets on every snapshot: each trigger's definition and
// status, and the reason if the store could not be read.
func (a *alarms) views(now time.Time, active string) ([]dcr.TriggerView, string) {
	views := make([]dcr.TriggerView, 0, len(a.list.Triggers))
	for i := range a.list.Triggers {
		t := &a.list.Triggers[i]
		sampled, seen := a.sampled[t.ID]
		views = append(views, t.View(now, active, sampled || !seen))
	}
	msg := ""
	if a.loadErr != nil {
		msg = a.loadErr.Error()
	}
	return views, msg
}
