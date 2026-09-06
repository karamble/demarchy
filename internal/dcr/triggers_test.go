// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

// armed is a valid trigger on the given leaf, expiring an hour from now.
func armed(path, op string, p Params) Trigger {
	now := time.Now().UTC().Truncate(time.Second)
	return Trigger{
		Connection: "home", Path: path, Operator: op, Params: p,
		DeliverTo: "you", Reason: "test", ArmedBy: "test",
		ArmedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func priceCross() Trigger {
	return armed("price.dcrUsd", "crosses", Params{Bound: f64(16), Direction: "above"})
}

func TestTriggersRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var list Triggers
	if err := list.Add(priceCross()); err != nil {
		t.Fatal(err)
	}
	msgs := armed("br.messages", "appears", Params{
		Where: map[string]string{"fromNick": "alice", "text~": "deploy"},
		Hold:  "0s",
	})
	msgs.Once = new(bool)
	if err := list.Add(msgs); err != nil {
		t.Fatal(err)
	}
	if list.Triggers[0].ID == "" || list.Triggers[0].Rev != 1 {
		t.Fatalf("Add should mint an id and a first revision: %+v", list.Triggers[0])
	}
	if err := SaveTriggers(&list); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(TriggersPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("saved as %04o, want 0600", info.Mode().Perm())
	}

	loaded, err := LoadTriggers()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(&list)
	got, _ := json.Marshal(loaded)
	if string(want) != string(got) {
		t.Fatalf("round trip changed the list:\n%s\n%s", want, got)
	}
	if !strings.HasSuffix(string(mustRead(t, TriggersPath())), "}\n") {
		t.Error("file should end in a newline")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l, err := LoadTriggers()
	if err != nil {
		t.Fatal(err)
	}
	if l.Version != triggerSchema || len(l.Triggers) != 0 {
		t.Fatalf("missing file should be an empty list, got %+v", l)
	}
}

// The file records who is watching what and how to reach them, so it is
// private like the connection list.
func TestLoadRefusesGroupReadable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var list Triggers
	list.Add(priceCross())
	if err := SaveTriggers(&list); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(TriggersPath(), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTriggers()
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("a group-readable file should be refused with the fix, got %v", err)
	}
}

func TestLoadRefusesUnknownField(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := WritePrivate(TriggersPath(), []byte(`{"version":1,"triggers":[],"bogus":1}`+"\n")); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTriggers()
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("unknown field should be ErrConfig, got %v", err)
	}
	if err := WritePrivate(TriggersPath(), []byte(`{"version":7,"triggers":[]}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTriggers(); !errors.Is(err, ErrConfig) {
		t.Fatalf("unknown version should be ErrConfig, got %v", err)
	}
	// A stored trigger is validated on the way in too, so a hand-edit that
	// broke one is reported rather than watched wrongly.
	broken := priceCross()
	broken.ID, broken.Rev, broken.Params.Direction = "t-0badf00d", 1, "sideways"
	b, _ := json.Marshal(Triggers{Version: triggerSchema, Triggers: []Trigger{broken}})
	if err := WritePrivate(TriggersPath(), b); err != nil {
		t.Fatal(err)
	}
	_, err = LoadTriggers()
	if !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), "direction") {
		t.Fatalf("invalid stored trigger should be ErrConfig naming the field, got %v", err)
	}
}

func TestValidateTrigger(t *testing.T) {
	good := []Trigger{
		priceCross(),
		armed("price.dcrUsd", "changes", Params{By: f64(5), Percent: true}),
		armed("price.dcrUsd", "stalls", Params{For: "30m"}),
		armed("node.status", "becomes", Params{Value: "synced"}),
		armed("wallet.synced", "becomes", Params{Value: "true"}),
		armed("br.messages", "appears", Params{Where: map[string]string{"text~": "hi"}, Key: []string{"fromNick"}}),
		armed("lightning.list", "disappears", Params{}),
		armed("lightning.list", "count", Params{Bound: f64(2), Direction: "below"}),
		armed("price.series", "count", Params{Bound: f64(10), Direction: "above", Rearm: f64(0)}),
	}
	for i := range good {
		good[i].Rev = 1
		if err := ValidateTrigger(&good[i]); err != nil {
			t.Errorf("%s %s should validate: %v", good[i].Path, good[i].Operator, err)
		}
	}

	bad := []struct {
		name string
		mut  func(*Trigger)
		want string
	}{
		{"missing expires_at", func(x *Trigger) { x.ExpiresAt = time.Time{} }, "expires_at"},
		{"expires before armed", func(x *Trigger) { x.ExpiresAt = x.ArmedAt }, "expires_at"},
		{"unknown path", func(x *Trigger) { x.Path = "price.moon" }, "path"},
		{"element field as path", func(x *Trigger) { x.Path = "br.messages.text" }, "path"},
		{"operator not for kind", func(x *Trigger) { x.Operator = "becomes"; x.Params = Params{Value: "16"} }, "operator"},
		{"crosses without bound", func(x *Trigger) { x.Params.Bound = nil }, "bound"},
		{"crosses without direction", func(x *Trigger) { x.Params.Direction = "up" }, "direction"},
		{"where key not a field", func(x *Trigger) {
			x.Path, x.Operator = "br.messages", "appears"
			x.Params = Params{Where: map[string]string{"nick": "alice"}}
		}, "where"},
		{"where on a scalar list", func(x *Trigger) {
			x.Path, x.Operator = "price.series", "count"
			x.Params = Params{Bound: f64(1), Direction: "above", Where: map[string]string{"x": "1"}}
		}, "where"},
		{"key not a field", func(x *Trigger) {
			x.Path, x.Operator = "lightning.list", "appears"
			x.Params = Params{Key: []string{"peer"}}
		}, "key"},
		{"changes without by", func(x *Trigger) { x.Operator = "changes"; x.Params = Params{} }, "by"},
		{"changes by zero", func(x *Trigger) { x.Operator = "changes"; x.Params = Params{By: f64(0)} }, "by"},
		{"stalls without for", func(x *Trigger) { x.Operator = "stalls"; x.Params = Params{} }, "for"},
		{"stalls for zero", func(x *Trigger) { x.Operator = "stalls"; x.Params = Params{For: "0s"} }, "for"},
		{"becomes without value", func(x *Trigger) {
			x.Path, x.Operator, x.Params = "node.status", "becomes", Params{}
		}, "value"},
		{"bad hold", func(x *Trigger) { x.Params.Hold = "soon" }, "hold"},
		{"negative rearm", func(x *Trigger) { x.Params.Rearm = f64(-1) }, "rearm"},
		{"empty deliver_to", func(x *Trigger) { x.DeliverTo = " " }, "deliver_to"},
		{"empty connection", func(x *Trigger) { x.Connection = "" }, "connection"},
		{"malformed id", func(x *Trigger) { x.ID = "trigger-1" }, "id"},
		{"zero rev", func(x *Trigger) { x.Rev = 0 }, "rev"},
	}
	for _, c := range bad {
		x := priceCross()
		x.Rev = 1
		c.mut(&x)
		err := ValidateTrigger(&x)
		if err == nil {
			t.Errorf("%s: should be refused", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error should name %q, got: %v", c.name, c.want, err)
		}
	}
}

func TestAddRefusesInvalidAndDuplicate(t *testing.T) {
	var list Triggers
	x := priceCross()
	x.Params.Bound = nil
	if err := list.Add(x); err == nil || len(list.Triggers) != 0 {
		t.Fatalf("an invalid trigger should not be appended, err=%v", err)
	}
	x = priceCross()
	x.ID = "t-0badf00d"
	if err := list.Add(x); err != nil {
		t.Fatal(err)
	}
	if err := list.Add(x); err == nil || len(list.Triggers) != 1 {
		t.Fatalf("a duplicate id should be refused, err=%v", err)
	}
	if got, ok := list.Find("t-0badf00d"); !ok || got.Path != "price.dcrUsd" {
		t.Fatalf("Find: %+v %v", got, ok)
	}
	if err := list.Remove("t-0badf00d"); err != nil || len(list.Triggers) != 0 {
		t.Fatalf("Remove: %v, %d left", err, len(list.Triggers))
	}
	if err := list.Remove("t-0badf00d"); !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("removing an unknown id should report ErrTriggerNotFound, got %v", err)
	}
}

func TestPrune(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	stamp := func(x *Trigger, id string) {
		x.ID, x.Rev = id, 1
		x.ArmedAt = now.Add(-time.Hour)
	}
	fired := now
	spent := priceCross()
	stamp(&spent, "t-00000001")
	spent.ExpiresAt = now.Add(48 * time.Hour)
	spent.State.FiredAt = &fired

	expired := priceCross()
	stamp(&expired, "t-00000002")
	expired.ExpiresAt = now

	live := priceCross()
	stamp(&live, "t-00000003")
	live.ExpiresAt = now.Add(1000 * time.Hour)

	// A repeating trigger that fired is not spent: it stays until its window
	// closes, and goes with the expired ones after that.
	repeat := priceCross()
	stamp(&repeat, "t-00000004")
	repeat.Once = new(bool)
	repeat.ExpiresAt = now
	repeat.State.FiredAt = &fired

	l := Triggers{Triggers: []Trigger{spent, expired, live, repeat}}
	l.Prune(now.Add(23 * time.Hour))
	if len(l.Triggers) != 4 {
		t.Fatalf("nothing is a day old yet, %d left of 4", len(l.Triggers))
	}
	l.Prune(now.Add(25 * time.Hour))
	if len(l.Triggers) != 1 || l.Triggers[0].ID != "t-00000003" {
		t.Fatalf("only the live trigger should survive, got %+v", l.Triggers)
	}
	l.Prune(now.Add(999 * time.Hour))
	if len(l.Triggers) != 1 {
		t.Fatal("an armed, unexpired trigger must never be pruned")
	}
}

// Every CLI invocation and the evaluator all read-modify-write one file. The
// lock is what keeps one arming from erasing another.
func TestWithTriggerLockSerialises(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const writers, each = 2, 50

	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				err := WithTriggerLock(func() error {
					l, err := LoadTriggers()
					if err != nil {
						return err
					}
					if err := l.Add(priceCross()); err != nil {
						return err
					}
					return SaveTriggers(l)
				})
				if err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	l, err := LoadTriggers()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Triggers) != writers*each {
		t.Fatalf("%d triggers survived, want %d", len(l.Triggers), writers*each)
	}
}

func TestOnceDefaultsToTrue(t *testing.T) {
	x := priceCross()
	if !x.IsOnce() {
		t.Error("nil once should read as true")
	}
	x.Once = new(bool)
	if x.IsOnce() {
		t.Error("once=false should read as false")
	}
	if x.Spent() {
		t.Error("a repeating trigger is never spent")
	}
	fired := time.Now()
	x.State.FiredAt = &fired
	if x.Spent() {
		t.Error("a repeating trigger is never spent, even fired")
	}
	x.Once = nil
	if !x.Spent() {
		t.Error("a fired one-shot is spent")
	}
	if x.Expired(x.ExpiresAt.Add(time.Hour)) {
		t.Error("a spent trigger is not also expired")
	}
	x.State.FiredAt = nil
	if !x.Expired(x.ExpiresAt) || x.Expired(x.ExpiresAt.Add(-time.Second)) {
		t.Error("expiry is at the boundary and not before")
	}
}
