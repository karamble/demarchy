// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// withConnection gives a test a config dir holding one connection, which is
// what arming binds to when no id is given.
func withConnection(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// shell.json is read from HOME, and this machine's has monitoring on.
	t.Setenv("HOME", t.TempDir())
	list := &Connections{}
	conn, err := list.Add("home", "https://pulse.example", "token-for-tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveConnections(list); err != nil {
		t.Fatal(err)
	}
	return conn.ID
}

func crossReq() ArmRequest {
	return ArmRequest{
		Path: "price.dcrUsd", Operator: "crosses",
		Params:  Params{Bound: fp(16), Direction: "above"},
		Expires: "4d", Reason: "smoke",
	}
}

func TestArmBindsToTheActiveConnectionAndSeedsNothing(t *testing.T) {
	home := withConnection(t)
	tr, warnings, err := Arm(crossReq(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Connection != home || tr.DeliverTo != You || tr.ArmedBy != You || !tr.IsOnce() || tr.Rev != 1 {
		t.Fatalf("armed trigger: %+v", tr)
	}
	if !tr.ExpiresAt.Equal(t0.Add(96 * time.Hour)) {
		t.Fatalf("expiry %v", tr.ExpiresAt)
	}
	if tr.State.LastValue != nil || tr.State.LastChangedAt != nil || !tr.State.Ready {
		t.Fatalf("arming must not seed a baseline: %+v", tr.State)
	}
	list, err := LoadTriggers()
	if err != nil || len(list.Triggers) != 1 || list.Triggers[0].ID != tr.ID {
		t.Fatalf("store after arm: %+v %v", list, err)
	}
	// Monitoring is not on in a bare config dir, and the agent should hear so.
	if len(warnings) == 0 || !strings.Contains(warnings[0], "monitoring is off") {
		t.Fatalf("warnings: %v", warnings)
	}
	if tr.View(t0, home, true).Status != "armed" {
		t.Fatal("a fresh trigger reads as armed before its first sample")
	}
}

func TestArmTakesAgentDetails(t *testing.T) {
	withConnection(t)
	req := crossReq()
	req.DeliverTo, req.ArmedBy, req.Once = "w8:p1", "w8:p1", bp(false)
	tr, _, err := Arm(req, t0)
	if err != nil {
		t.Fatal(err)
	}
	if tr.DeliverTo != "w8:p1" || tr.ArmedBy != "w8:p1" || tr.IsOnce() {
		t.Fatalf("agent details lost: %+v", tr)
	}
}

func TestArmRefuses(t *testing.T) {
	withConnection(t)
	bad := map[string]func(*ArmRequest){
		"unknown connection": func(r *ArmRequest) { r.Connection = "nowhere" },
		"no expiry":          func(r *ArmRequest) { r.Expires = "" },
		"bad expiry":         func(r *ArmRequest) { r.Expires = "soon" },
		"unknown path":       func(r *ArmRequest) { r.Path = "price.moon" },
		"wrong operator":     func(r *ArmRequest) { r.Operator = "appears" },
		"missing bound":      func(r *ArmRequest) { r.Params = Params{Direction: "above"} },
	}
	for why, tweak := range bad {
		req := crossReq()
		tweak(&req)
		if _, _, err := Arm(req, t0); err == nil {
			t.Errorf("%s: should be refused", why)
		}
	}
	if list, _ := LoadTriggers(); len(list.Triggers) != 0 {
		t.Fatalf("refused requests reached the store: %+v", list.Triggers)
	}
}

func TestWalletPathWarnsWhileBalancesAreHidden(t *testing.T) {
	withConnection(t)
	var wallet string
	for _, l := range Catalogue() {
		if strings.HasPrefix(l.Path, "wallet.") && l.Kind == KindNumber {
			wallet = l.Path
			break
		}
	}
	if wallet == "" {
		t.Skip("no numeric wallet leaf in the catalogue")
	}
	req := crossReq()
	req.Path = wallet
	_, warnings, err := Arm(req, t0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w, "showBalances")
	}
	if !found {
		t.Fatalf("no showBalances warning in %v", warnings)
	}
}

func TestEditBumpsRevAndResetsWhatItMust(t *testing.T) {
	withConnection(t)
	tr, _, err := Arm(crossReq(), t0)
	if err != nil {
		t.Fatal(err)
	}
	// The helper ran for a while: a baseline, a fire, a delivery.
	err = WithTriggerLock(func() error {
		list, err := LoadTriggers()
		if err != nil {
			return err
		}
		got, _ := list.Find(tr.ID)
		fired := t0.Add(time.Minute)
		got.State.LastValue = []byte("17")
		got.State.LastChangedAt = &fired
		got.State.RefValue = []byte("15")
		got.State.FiredAt = &fired
		got.State.FireCount = 1
		got.State.Delivered = "agent"
		got.State.Ready = false
		return SaveTriggers(list)
	})
	if err != nil {
		t.Fatal(err)
	}

	edited, _, err := EditTrigger(tr.ID, ArmRequest{Params: Params{Bound: fp(18), Direction: "above"}}, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	st := edited.State
	if edited.Rev != 2 || *edited.Params.Bound != 18 || edited.Path != "price.dcrUsd" || edited.Reason != "smoke" {
		t.Fatalf("edit did not keep or change what it should: %+v", edited)
	}
	if st.FiredAt != nil || st.RefValue != nil || !st.Ready || st.Delivered != "" {
		t.Fatalf("edit must re-arm: %+v", st)
	}
	if string(st.LastValue) != "17" || st.LastChangedAt == nil || st.FireCount != 1 {
		t.Fatalf("edit of a bound must keep the baseline and the history: %+v", st)
	}
	if edited.Spent() {
		t.Fatal("an edited one-shot is live again")
	}

	// A change of path drops the baseline: a stale number on a new path would
	// be a false transition.
	moved, _, err := EditTrigger(tr.ID, ArmRequest{Path: "node.height", Operator: "changes", Params: Params{By: fp(1)}}, t0.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if moved.Rev != 3 || moved.State.LastValue != nil || moved.State.LastChangedAt != nil {
		t.Fatalf("a path change must drop the baseline: %+v", moved)
	}

	// An edit that would leave the trigger invalid changes nothing.
	if _, _, err := EditTrigger(tr.ID, ArmRequest{Operator: "stalls"}, t0); err == nil {
		t.Fatal("stalls without params.for should be refused")
	}
	if disk, _ := LoadTriggers(); disk.Triggers[0].Rev != 3 || disk.Triggers[0].Operator != "changes" {
		t.Fatalf("a refused edit reached the store: %+v", disk.Triggers[0])
	}
}

func TestEditAndDisarmUnknown(t *testing.T) {
	withConnection(t)
	if _, _, err := EditTrigger("t-00000000", ArmRequest{Reason: "x"}, t0); !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("edit of unknown: %v", err)
	}
	if _, err := Disarm("t-00000000"); !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("disarm of unknown: %v", err)
	}
}

func TestDisarmRemovesNow(t *testing.T) {
	withConnection(t)
	tr, _, err := Arm(crossReq(), t0)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := Disarm(tr.ID)
	if err != nil || gone.ID != tr.ID {
		t.Fatalf("disarm: %+v %v", gone, err)
	}
	if list, _ := LoadTriggers(); len(list.Triggers) != 0 {
		t.Fatalf("still in the store: %+v", list.Triggers)
	}
	if _, err := Disarm(tr.ID); !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("second disarm: %v", err)
	}
}

// A dry run must leave nothing behind, not even an id: a minted id in a
// preview would invite a later disarm of a trigger that never existed.
func TestPrepareArmWritesNothing(t *testing.T) {
	home := withConnection(t)
	tr, warnings, err := PrepareArm(crossReq(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if tr.ID != "" || tr.Rev != 1 || tr.Connection != home || !tr.State.Ready {
		t.Fatalf("prepared trigger: %+v", tr)
	}
	if _, err := os.Stat(TriggersPath()); !os.IsNotExist(err) {
		t.Fatalf("a dry run touched the store: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("warnings should still be reported on a dry run")
	}
	if _, _, err := PrepareArm(ArmRequest{Path: "price.moon", Operator: "crosses", Expires: "1h"}, t0); err == nil {
		t.Fatal("a dry run must validate as strictly as an arm")
	}
}

func TestApplyEditIsPure(t *testing.T) {
	fired := t0.Add(time.Minute)
	orig := Trigger{
		ID: "t-0000abcd", Rev: 1, Connection: "c", Path: "price.dcrUsd", Operator: "crosses",
		Params: Params{Bound: fp(16), Direction: "above"}, DeliverTo: You,
		ExpiresAt: t0.Add(time.Hour), ArmedAt: t0, ArmedBy: You, Reason: "smoke",
		State: State{LastValue: []byte("17"), LastChangedAt: &fired, RefValue: []byte("15"),
			FiredAt: &fired, FireCount: 1, Delivered: "agent"},
	}
	before := fmt.Sprintf("%+v", orig)
	next, err := applyEdit(orig, ArmRequest{Params: Params{Bound: fp(18), Direction: "above"}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", orig) != before {
		t.Fatal("applyEdit changed its input")
	}
	st := next.State
	if next.Rev != 2 || *next.Params.Bound != 18 || st.FiredAt != nil || st.RefValue != nil ||
		!st.Ready || st.Delivered != "" || string(st.LastValue) != "17" || st.LastChangedAt == nil {
		t.Fatalf("edit result: %+v", next)
	}
	moved, err := applyEdit(orig, ArmRequest{Path: "node.height", Operator: "changes", Params: Params{By: fp(1)}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if moved.State.LastValue != nil || moved.State.LastChangedAt != nil {
		t.Fatalf("a path change must drop the baseline: %+v", moved.State)
	}
	if _, err := applyEdit(orig, ArmRequest{Operator: "stalls"}, t0); err == nil {
		t.Fatal("stalls without params.for should be refused")
	}
}

func TestPrepareEditWritesNothing(t *testing.T) {
	withConnection(t)
	tr, _, err := Arm(crossReq(), t0)
	if err != nil {
		t.Fatal(err)
	}
	was := mustRead(t, TriggersPath())
	next, _, err := PrepareEdit(tr.ID, ArmRequest{Params: Params{Bound: fp(18), Direction: "above"}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Rev != 2 || next.ID != tr.ID {
		t.Fatalf("preview: %+v", next)
	}
	if string(mustRead(t, TriggersPath())) != string(was) {
		t.Fatal("a dry run edit wrote to the store")
	}
	if disk, _ := LoadTriggers(); disk.Triggers[0].Rev != 1 {
		t.Fatal("rev on disk moved")
	}
	if _, _, err := PrepareEdit("t-00000000", ArmRequest{}, t0); !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}
