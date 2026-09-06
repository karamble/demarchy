// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
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

	notices notices
	// told lets a test wait for the notice deliveries in flight.
	told sync.WaitGroup
}

// notices are the two events that always deserve the desk without anyone
// arming a trigger: a bot payment waiting for the person's approval, which
// expires two minutes after it is raised, and an agent whose token the
// tripwire just revoked. Each rings "you" once, through the same ringer the
// alerts use, and is remembered only in memory: a payment outlives nothing,
// and a blocked entry that is already in the audit when the helper starts is
// history, not news. Nothing here can answer a request; that is a human's
// click in the dashboard.
type notices struct {
	pending map[string]bool
	blocked map[string]bool
	primed  bool
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
		notices: notices{pending: map[string]bool{}, blocked: map[string]bool{}},
	}
}

// notice rings the desk for what is new in the audit and the bridge.
func (a *alarms) notice(ctx context.Context, snap *dcr.Snapshot) {
	if snap == nil || !snap.Reachable {
		return
	}
	if b := snap.BRMCP; b != nil {
		seen := make(map[string]bool, len(b.Pending))
		for _, p := range b.Pending {
			seen[p.ID] = true
			if !a.notices.pending[p.ID] {
				a.tell(ctx, pendingText(p, a.now()))
			}
		}
		// Forget what was resolved: a request raised again is a new request.
		a.notices.pending = seen
	}
	if au := snap.Audit; au != nil {
		for _, e := range au.Entries {
			if e.Result != "blocked" {
				continue
			}
			key := e.Time + "\x00" + e.AgentID + "\x00" + e.Tool
			if a.notices.blocked[key] {
				continue
			}
			a.notices.blocked[key] = true
			if a.notices.primed {
				a.tell(ctx, blockedText(e))
			}
		}
		a.notices.primed = true
	}
}

// tell delivers one notice to the desk in the background.
func (a *alarms) tell(ctx context.Context, text string) {
	fmt.Fprintln(os.Stderr, "notice:", text)
	a.told.Add(1)
	go func() {
		defer a.told.Done()
		if how, err := a.ring.Ring(ctx, dcr.You, text); err != nil {
			fmt.Fprintln(os.Stderr, "notice not delivered:", err)
		} else {
			fmt.Fprintln(os.Stderr, "notice delivered by", how)
		}
	}()
}

func pendingText(p dcr.BRMCPPending, now time.Time) string {
	who := p.BotNick
	if who == "" {
		who = shortHex(p.Bot)
	}
	left := ""
	if t, err := time.Parse(time.RFC3339, p.ExpiresAt); err == nil {
		left = ", expires in " + untilSeconds(t.Sub(now))
	}
	return fmt.Sprintf("brmcp approval: %s wants %s DCR for %s%s. Approve or deny in the dashboard.",
		who, formatDcr(p.AmountDcr), p.Tool, left)
}

func blockedText(e dcr.AuditEntry) string {
	amount := ""
	if e.AmountDcr > 0 {
		amount = " " + formatDcr(e.AmountDcr) + " DCR"
	}
	target := ""
	if e.Target != "" {
		target = " to " + shortHex(e.Target)
	}
	return fmt.Sprintf("dcrpulse blocked agent %s: %s%s%s tripped the spend limit and its token is revoked. Unblock it in the dashboard if that was wrong.",
		e.Agent, e.Tool, amount, target)
}

func formatDcr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// shortHex keeps the ends of an address, uid or txid: enough to recognise,
// not enough to fill a notification with.
func shortHex(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:6] + "..." + s[len(s)-4:]
}

func untilSeconds(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	secs := int(d.Round(time.Second) / time.Second)
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm %ds", secs/60, secs%60)
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
	// The desk is told about approvals and revocations whether or not the
	// trigger store can be read: they are not triggers.
	a.notice(ctx, snap)
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
