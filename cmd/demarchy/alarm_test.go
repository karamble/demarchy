// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

var base = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

type ringCall struct{ target, text string }

// fakeRinger records every delivery and answers with what it was told to.
type fakeRinger struct {
	mu    sync.Mutex
	calls []ringCall
	how   string
	err   error
}

func (f *fakeRinger) Ring(_ context.Context, target, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ringCall{target, text})
	return f.how, f.err
}

func (f *fakeRinger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func f64(v float64) *float64 { return &v }
func no() *bool              { b := false; return &b }

// priceAbove16 is a standing trigger on connection "c" delivered to an agent.
func priceAbove16() dcr.Trigger {
	return dcr.Trigger{
		ID: dcr.NewTriggerID(), Rev: 1, Connection: "c",
		Path: "price.dcrUsd", Operator: "crosses",
		Params:    dcr.Params{Bound: f64(16), Direction: "above"},
		DeliverTo: "w8:p1", Once: no(),
		ExpiresAt: base.Add(24 * time.Hour), ArmedAt: base, ArmedBy: "w8:p1",
		State: dcr.State{Ready: true},
	}
}

func store(t *testing.T, triggers ...dcr.Trigger) {
	t.Helper()
	if err := dcr.SaveTriggers(&dcr.Triggers{Triggers: triggers}); err != nil {
		t.Fatal(err)
	}
}

func onDisk(t *testing.T, id string) dcr.Trigger {
	t.Helper()
	list, err := dcr.LoadTriggers()
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := list.Find(id)
	if !ok {
		t.Fatalf("%s is not in the store", id)
	}
	return *tr
}

func price(v float64) *dcr.Snapshot {
	return &dcr.Snapshot{Reachable: true, Price: &dcr.Price{DcrUsd: v}}
}

// board sets up a temp store holding the given triggers and an alarms bound to
// a controllable clock.
func board(t *testing.T, ring ringer, triggers ...dcr.Trigger) (*alarms, *time.Time) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store(t, triggers...)
	clock := base
	a := newAlarms(ring)
	a.now = func() time.Time { return clock }
	a.reload()
	return a, &clock
}

func awaitResult(t *testing.T, a *alarms) ringResult {
	t.Helper()
	select {
	case r := <-a.results:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery result arrived")
	}
	return ringResult{}
}

func TestFireDispatchesOnceAndSettles(t *testing.T) {
	ring := &fakeRinger{how: "agent"}
	tr := priceAbove16()
	a, clock := board(t, ring, tr)
	ctx := context.Background()

	a.observe(ctx, price(15), "c")
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17), "c")
	r := awaitResult(t, a)
	if ring.count() != 1 || ring.calls[0].target != "w8:p1" ||
		!strings.Contains(ring.calls[0].text, "price.dcrUsd crosses above 16") {
		t.Fatalf("ring calls: %+v", ring.calls)
	}
	if r.id != tr.ID || r.how != "agent" || r.err != nil {
		t.Fatalf("result: %+v", r)
	}
	a.settle(r)

	disk := onDisk(t, tr.ID)
	if disk.State.Delivered != "agent" || disk.State.FiredAt == nil || disk.State.FireCount != 1 {
		t.Fatalf("delivery not recorded on disk: %+v", disk.State)
	}

	// Sitting past the bound is not another crossing.
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17.5), "c")
	if ring.count() != 1 {
		t.Fatalf("rang again while sitting past the bound: %d calls", ring.count())
	}
	views, msg := a.views(*clock, "c")
	if msg != "" || len(views) != 1 || views[0].Status != "rearming" || views[0].Delivered != "agent" {
		t.Fatalf("views: %+v %q", views, msg)
	}
}

func TestDeliveryFailureIsRecorded(t *testing.T) {
	ring := &fakeRinger{err: context.DeadlineExceeded}
	tr := priceAbove16()
	a, clock := board(t, ring, tr)
	ctx := context.Background()
	a.observe(ctx, price(15), "c")
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17), "c")
	a.settle(awaitResult(t, a))
	disk := onDisk(t, tr.ID)
	if disk.State.Delivered != "" || disk.State.DeliveryError == "" {
		t.Fatalf("failure not recorded: %+v", disk.State)
	}
	if v, _ := a.views(*clock, "c"); v[0].Status != "delivery-failed" {
		t.Fatalf("status %q, want delivery-failed", v[0].Status)
	}
}

func TestOtherConnectionIsLeftAlone(t *testing.T) {
	ring := &fakeRinger{how: "agent"}
	tr := priceAbove16()
	a, clock := board(t, ring, tr)
	ctx := context.Background()
	a.observe(ctx, price(15), "d")
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17), "d")
	if ring.count() != 0 {
		t.Fatalf("rang on another connection: %+v", ring.calls)
	}
	if disk := onDisk(t, tr.ID); disk.State.LastValue != nil {
		t.Fatalf("state was touched on another connection: %+v", disk.State)
	}
	if v, _ := a.views(*clock, "d"); v[0].Status != "other-connection" {
		t.Fatalf("status %q, want other-connection", v[0].Status)
	}
}

func TestRecoverReringsOnlyTheUndelivered(t *testing.T) {
	ring := &fakeRinger{how: "notification"}
	lost := priceAbove16()
	lost.State.FiredAt = &base
	lost.State.FireCount = 1
	heard := priceAbove16()
	heard.State.FiredAt = &base
	heard.State.Delivered = "agent"
	a, _ := board(t, ring, lost, heard)
	a.recover(context.Background())
	r := awaitResult(t, a)
	if ring.count() != 1 || r.id != lost.ID {
		t.Fatalf("recover rang %d times, result %+v", ring.count(), r)
	}
	if !strings.Contains(ring.calls[0].text, "fired at 2026-09-06T12:00:00Z") {
		t.Fatalf("a recovered ring should carry the original time: %s", ring.calls[0].text)
	}
}

// The CLI owns the definition. A bumped rev between two passes means our state
// for the old definition is dropped and the new bound is what counts.
func TestEditOnDiskIsPickedUpBetweenPasses(t *testing.T) {
	ring := &fakeRinger{how: "agent"}
	tr := priceAbove16()
	a, clock := board(t, ring, tr)
	ctx := context.Background()
	a.observe(ctx, price(15), "c")

	edited := onDisk(t, tr.ID)
	edited.Rev = 2
	edited.Params.Bound = f64(18.5)
	store(t, edited)

	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17), "c")
	if ring.count() != 0 {
		t.Fatalf("fired on the old bound after an edit: %+v", ring.calls)
	}
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(19), "c")
	awaitResult(t, a)
	if ring.count() != 1 || !strings.Contains(ring.calls[0].text, "above 18.5") {
		t.Fatalf("the edited bound should be the one that fires: %+v", ring.calls)
	}
}

func TestDisarmOnDiskStopsEvaluation(t *testing.T) {
	ring := &fakeRinger{how: "agent"}
	tr := priceAbove16()
	a, clock := board(t, ring, tr)
	ctx := context.Background()
	a.observe(ctx, price(15), "c")
	store(t)
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(17), "c")
	if ring.count() != 0 {
		t.Fatalf("a disarmed trigger fired: %+v", ring.calls)
	}
	if v, _ := a.views(*clock, "c"); len(v) != 0 {
		t.Fatalf("a disarmed trigger is still listed: %+v", v)
	}
}

// Ordinary movement is written once a minute; the baseline, a fire and a
// re-arm are written at once.
func TestSaveIsDebounced(t *testing.T) {
	tr := priceAbove16()
	a, clock := board(t, &fakeRinger{how: "agent"}, tr)
	ctx := context.Background()
	a.observe(ctx, price(15), "c")
	if got := string(onDisk(t, tr.ID).State.LastValue); got != "15" {
		t.Fatalf("baseline not flushed, disk has %q", got)
	}
	*clock = clock.Add(8 * time.Second)
	a.observe(ctx, price(15.5), "c")
	if got := string(onDisk(t, tr.ID).State.LastValue); got != "15" {
		t.Fatalf("movement was written before the debounce, disk has %q", got)
	}
	*clock = clock.Add(61 * time.Second)
	a.observe(ctx, price(15.6), "c")
	if got := string(onDisk(t, tr.ID).State.LastValue); got != "15.6" {
		t.Fatalf("movement not written after the debounce, disk has %q", got)
	}
}

func TestBrokenStoreIsReportedNotEvaluated(t *testing.T) {
	a, clock := board(t, &fakeRinger{how: "agent"}, priceAbove16())
	if err := dcr.WritePrivate(dcr.TriggersPath(), []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	a.observe(context.Background(), price(17), "c")
	if _, msg := a.views(*clock, "c"); msg == "" {
		t.Fatal("a broken store should be reported to the panel")
	}
}
