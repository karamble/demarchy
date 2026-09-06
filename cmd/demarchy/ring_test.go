// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Replies shaped like the ones herdr prints on this machine. The error and
// list envelopes are captures, trimmed to the keys that matter. The success
// body is a stand-in, since the code only asks whether "result" is there, and
// display_agent is where herdr's schema puts the name given with "herdr agent
// rename": no live agent carried one when this was written.
const (
	promptOK      = `{"id":"cli:agent:prompt","result":{"accepted":true}}`
	promptBlocked = `{"error":{"code":"agent_blocked","message":"agent target duty is blocked"},"id":"cli:agent:prompt"}`
	promptMissing = `{"error":{"code":"agent_not_found","message":"agent target duty not found"},"id":"cli:agent:prompt"}`
	agentList     = `{"id":"cli:agent:list","result":{"agents":[` +
		`{"agent":"claude","agent_status":"working","pane_id":"w7:p1","terminal_title_stripped":"demarchy-trigger-alerts","workspace_id":"w7"},` +
		`{"agent":"claude","agent_status":"idle","display_agent":"duty","pane_id":"w8:p1","terminal_title_stripped":"refund-window","workspace_id":"w8"}` +
		`]}}`
)

var exit1 = errors.New("exit status 1")

type call struct {
	name string
	args []string
}

type answer struct {
	out string
	err error
}

// fakeExec stands in for the machine. Each program has a queue of scripted
// answers; the last one repeats once the queue is used up, so "blocked for
// ever" is a single line to script. A program with no script at all is not
// installed, which is how the fallbacks get exercised.
type fakeExec struct {
	answers map[string][]answer
	calls   []call
	slept   []time.Duration
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name, args})
	q := f.answers[name]
	if len(q) == 0 {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if len(q) > 1 {
		f.answers[name] = q[1:]
	}
	return []byte(q[0].out), q[0].err
}

func (f *fakeExec) sleep(_ context.Context, d time.Duration) error {
	f.slept = append(f.slept, d)
	return nil
}

func (f *fakeExec) ringer() *herdrRinger {
	return &herdrRinger{run: f.run, retryFor: 60 * time.Second, retryEvery: 10 * time.Second, sleep: f.sleep}
}

func (f *fakeExec) count(name string) int {
	n := 0
	for _, c := range f.calls {
		if c.name == name {
			n++
		}
	}
	return n
}

// notified returns the body of the one notification that went out, whichever
// program carried it, and fails the test if there was not exactly one.
func (f *fakeExec) notified(t *testing.T) string {
	t.Helper()
	var bodies []string
	for _, c := range f.calls {
		if c.name == "omarchy-notification-send" || c.name == "notify-send" {
			bodies = append(bodies, c.args[len(c.args)-1])
		}
	}
	if len(bodies) != 1 {
		t.Fatalf("got %d notifications, want exactly 1: %v", len(bodies), f.calls)
	}
	return bodies[0]
}

func (f *fakeExec) totalSlept() time.Duration {
	var d time.Duration
	for _, s := range f.slept {
		d += s
	}
	return d
}

func desktopOnly() *fakeExec {
	return &fakeExec{answers: map[string][]answer{"omarchy-notification-send": {{}}}}
}

func withHerdr(replies ...answer) *fakeExec {
	f := desktopOnly()
	f.answers["herdr"] = replies
	return f
}

func TestRingYouSkipsHerdr(t *testing.T) {
	f := desktopOnly()
	how, err := f.ringer().Ring(context.Background(), "you", "DCR fell under 20 USD")
	if err != nil || how != "notification" {
		t.Fatalf("Ring = %q, %v; want notification, nil", how, err)
	}
	if f.count("herdr") != 0 {
		t.Fatalf("herdr was run for target you: %v", f.calls)
	}
	if body := f.notified(t); body != "DCR fell under 20 USD" {
		t.Fatalf("body = %q, want the bare text: nobody was skipped", body)
	}
}

func TestRingReachesAgent(t *testing.T) {
	f := withHerdr(answer{out: promptOK})
	text := `price "crossed" 20 USD; check the book`
	how, err := f.ringer().Ring(context.Background(), "duty", text)
	if err != nil || how != "agent" {
		t.Fatalf("Ring = %q, %v; want agent, nil", how, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %v, want exactly one herdr call and no notification", f.calls)
	}
	// The text must travel as one argv element, however it is punctuated.
	want := []string{"agent", "prompt", "duty", text}
	if got := f.calls[0].args; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("herdr argv = %q, want %q", got, want)
	}
}

func TestRingWaitsOutBlockedAgent(t *testing.T) {
	f := withHerdr(answer{promptBlocked, exit1}, answer{promptBlocked, exit1}, answer{out: promptOK})
	how, err := f.ringer().Ring(context.Background(), "duty", "x")
	if err != nil || how != "agent" {
		t.Fatalf("Ring = %q, %v; want agent, nil", how, err)
	}
	if f.count("herdr") != 3 {
		t.Fatalf("herdr called %d times, want 3", f.count("herdr"))
	}
	if len(f.slept) != 2 || f.slept[0] != 10*time.Second || f.slept[1] != 10*time.Second {
		t.Fatalf("slept %v, want two of retryEvery", f.slept)
	}
	if f.count("omarchy-notification-send") != 0 {
		t.Fatal("notified although the agent took the alarm in the end")
	}
}

// The three ways herdr can fail to deliver all end at the desktop, and the
// body has to say which one it was: the human reading it decides whether to
// fix a name, wait for an agent, or install herdr.
func TestRingFallsBackWithReason(t *testing.T) {
	cases := []struct {
		name   string
		fake   *fakeExec
		calls  int // herdr calls expected, or -1 to only require retries
		reason string
	}{
		{"blocked for ever", withHerdr(answer{promptBlocked, exit1}), -1, "blocked"},
		{"not found", withHerdr(answer{promptMissing, exit1}), 1, "not found"},
		{"herdr missing", desktopOnly(), 1, "herdr is not available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			how, err := tc.fake.ringer().Ring(context.Background(), "duty", "x")
			if err != nil || how != "notification" {
				t.Fatalf("Ring = %q, %v; want notification, nil", how, err)
			}
			body := tc.fake.notified(t)
			if !strings.HasPrefix(body, "x\n") || !strings.Contains(body, tc.reason) {
				t.Fatalf("body = %q, want the text, then a line saying %q", body, tc.reason)
			}
			n := tc.fake.count("herdr")
			if tc.calls >= 0 && n != tc.calls {
				t.Fatalf("herdr called %d times, want %d", n, tc.calls)
			}
			if tc.calls < 0 {
				if n < 2 {
					t.Fatalf("herdr called %d times, want it retried", n)
				}
				if slept := tc.fake.totalSlept(); slept > 60*time.Second {
					t.Fatalf("waited %v in total, want no more than retryFor", slept)
				}
				if !strings.Contains(body, "duty") {
					t.Fatalf("body = %q, want the agent named", body)
				}
			}
		})
	}
}

func TestRingFallsBackToNotifySend(t *testing.T) {
	f := &fakeExec{answers: map[string][]answer{"notify-send": {{}}}}
	how, err := f.ringer().Ring(context.Background(), "you", "x")
	if err != nil || how != "notification" {
		t.Fatalf("Ring = %q, %v; want notification, nil", how, err)
	}
	if f.count("omarchy-notification-send") != 1 || f.count("notify-send") != 1 {
		t.Fatalf("calls = %v, want omarchy-notification-send tried first, then notify-send", f.calls)
	}
}

func TestRingFailsOnlyWhenNothingCanNotify(t *testing.T) {
	f := &fakeExec{answers: map[string][]answer{}}
	how, err := f.ringer().Ring(context.Background(), "duty", "x")
	if err == nil || how != "" {
		t.Fatalf("Ring = %q, %v; want \"\" and an error", how, err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want the missing binary still visible in the chain", err)
	}
}

// Shutting down while waiting for a blocked agent is not a delivery failure.
// Escalating it would ring the desktop every time the helper restarts mid
// retry, which is exactly when the human is already at the keyboard.
func TestRingStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := withHerdr(answer{promptBlocked, exit1})
	r := f.ringer()
	r.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	how, err := r.Ring(ctx, "duty", "x")
	if !errors.Is(err, context.Canceled) || how != "" {
		t.Fatalf("Ring = %q, %v; want \"\" and the cancellation", how, err)
	}
	if f.count("herdr") != 1 {
		t.Fatalf("herdr called %d times after cancel, want 1", f.count("herdr"))
	}
	if f.count("omarchy-notification-send") != 0 || f.count("notify-send") != 0 {
		t.Fatalf("notified on cancel: %v", f.calls)
	}
}

func TestAgentStates(t *testing.T) {
	f := withHerdr(answer{out: agentList})
	got, err := agentStates(context.Background(), f.run)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"w7:p1": "working", "w8:p1": "idle", "duty": "idle"}
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("states[%q] = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if args := strings.Join(f.calls[0].args, " "); args != "agent list" {
		t.Fatalf("herdr argv = %q, want agent list", args)
	}
}

func TestAgentStatesWithoutHerdr(t *testing.T) {
	f := &fakeExec{answers: map[string][]answer{}}
	got, err := agentStates(context.Background(), f.run)
	if err != nil {
		t.Fatalf("err = %v, want nil: no herdr is a normal state", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("states = %v, want an empty map", got)
	}

	// A herdr that is installed but unhappy is a different matter.
	f = withHerdr(answer{`{"error":{"code":"server_unavailable","message":"no server"},"id":"cli:agent:list"}`, exit1})
	if _, err := agentStates(context.Background(), f.run); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Fatalf("err = %v, want herdr's own message", err)
	}
}
