// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ringer delivers a fired alarm. Behind an interface so the watch loop can be
// tested with a fake that never touches herdr or the desktop.
type ringer interface {
	// Ring delivers text to target. how reports where it landed: "agent" or
	// "notification". err is set only when neither worked.
	Ring(ctx context.Context, target, text string) (how string, err error)
}

// runner executes an external command and returns combined stdout+stderr and
// the exit error. It exists so tests can substitute a fake exec.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the runner the real helper uses. Every argument reaches the
// process as its own argv entry and no shell is involved: the alarm text is
// assembled from dashboard values and a condition the user typed, and no part
// of it may ever be able to turn into a command.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// herdrRinger wakes an agent through the herdr CLI and falls back to a desktop
// notification when it cannot. An alarm that reaches nobody is worse than one
// that reaches the wrong recipient, so it reports failure only when the
// notification itself could not be raised.
type herdrRinger struct {
	run        runner
	retryFor   time.Duration                              // total time to keep retrying agent_blocked; default 60s
	retryEvery time.Duration                              // default 10s
	sleep      func(context.Context, time.Duration) error // injectable; default waits on ctx or timer
}

var _ ringer = (*herdrRinger)(nil)

func newHerdrRinger() *herdrRinger {
	return &herdrRinger{
		run:        execRunner,
		retryFor:   60 * time.Second,
		retryEvery: 10 * time.Second,
		sleep:      sleepFor,
	}
}

// sleepFor waits for d unless ctx ends first, in which case it reports why.
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// alarmGlyph is the bell the notification carries, nf-md-bell-ring from the
// Nerd Fonts range Omarchy's own scripts draw from. Written as an escape so
// the source stays plain ASCII.
const alarmGlyph = "\U000F009E"

// herdrReply is the envelope every herdr command prints, on success and on
// failure alike. Exactly one of the two keys is present.
type herdrReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseHerdr reads herdr's envelope. ok is false when the output is not one,
// which is what a missing binary, a crashed one and a wrapper script's
// complaint all look like from here.
func parseHerdr(out []byte) (reply herdrReply, ok bool) {
	if err := json.Unmarshal(bytes.TrimSpace(out), &reply); err != nil {
		return reply, false
	}
	return reply, reply.Result != nil || reply.Error != nil
}

// message picks what a failed reply has to say for itself.
func (r herdrReply) message() string {
	if r.Error == nil {
		return ""
	}
	if r.Error.Message != "" {
		return r.Error.Message
	}
	return r.Error.Code
}

// describe turns a command failure into one line for a human: the exit error,
// kept in the chain so callers can still ask what kind it was, then whatever
// the command printed first, if anything.
func describe(err error, out []byte) error {
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, line)
}

func (r *herdrRinger) Ring(ctx context.Context, target, text string) (string, error) {
	// skipped explains why the agent did not get the alarm. It stays empty for
	// "you", where nobody was skipped: the human is the recipient.
	var skipped string
	if target != "you" {
		var err error
		skipped, err = r.prompt(ctx, target, text)
		if err != nil {
			return "", err
		}
		if skipped == "" {
			return "agent", nil
		}
	}
	if err := r.notify(ctx, text, skipped); err != nil {
		return "", err
	}
	return "notification", nil
}

// prompt hands text to the agent. It returns the reason the agent did not get
// it, or "" when it did. The error is reserved for ctx ending mid-retry: the
// helper shutting down is not something to wake the human about.
//
// A blocked agent is one sitting on an approval dialog, which clears in
// seconds to minutes, so that answer earns a retry. Not found means the pane is
// gone or renamed and will not come back on its own, so waiting would only
// delay the human hearing about it.
func (r *herdrRinger) prompt(ctx context.Context, target, text string) (string, error) {
	// Time is counted in sleeps rather than read from the clock. The sleep is
	// the one thing a test can stub, and a budget kept in the unit the test
	// controls makes the retry count exact instead of racy. The wall clock
	// overshoots by however long each herdr call takes, which is fine.
	var waited time.Duration
	for {
		out, err := r.run(ctx, "herdr", "agent", "prompt", target, text)
		if ctx.Err() != nil {
			return "", fmt.Errorf("waking %s: %w", target, ctx.Err())
		}
		reply, ok := parseHerdr(out)
		switch {
		case !ok && errors.Is(err, exec.ErrNotFound):
			return "herdr is not available", nil
		case !ok && err != nil:
			return "herdr failed: " + describe(err, out).Error(), nil
		case !ok:
			return "herdr gave an unreadable answer", nil
		case reply.Error == nil && err == nil:
			return "", nil
		case reply.Error == nil:
			// A result next to a non-zero exit is not a shape herdr produces
			// today. Trust the exit status: nothing says the agent has it.
			return "herdr failed: " + err.Error(), nil
		}
		switch reply.Error.Code {
		case "agent_not_found":
			return fmt.Sprintf("agent %s was not found", target), nil
		case "agent_blocked":
			if r.retryEvery <= 0 || waited+r.retryEvery > r.retryFor {
				return fmt.Sprintf("agent %s stayed blocked for %s", target,
					waited.Truncate(time.Second)), nil
			}
			if err := r.sleep(ctx, r.retryEvery); err != nil {
				return "", fmt.Errorf("waking %s: %w", target, err)
			}
			waited += r.retryEvery
		default:
			return "herdr refused: " + reply.message(), nil
		}
	}
}

// notify raises the desktop notification. skipped, when set, goes under the
// text so the human can tell an alarm addressed to them from one that fell
// back to them, and knows what to fix.
//
// omarchy-notification-send is what the rest of Omarchy uses, and it takes a
// body that starts with a dash as text. notify-send is the portable fallback
// and does not, so the "--" guard matters there: "-5% in an hour" is a
// plausible alarm.
func (r *herdrRinger) notify(ctx context.Context, text, skipped string) error {
	const title = "Demarchy alarm"
	body := text
	if skipped != "" {
		body += "\n" + skipped
	}
	out, err := r.run(ctx, "omarchy-notification-send",
		"-u", "critical", "-g", alarmGlyph, title, body)
	if errors.Is(err, exec.ErrNotFound) {
		out, err = r.run(ctx, "notify-send", "-u", "critical", "--", title, body)
	}
	if err != nil {
		return fmt.Errorf("desktop notification: %w", describe(err, out))
	}
	return nil
}

// agentStates asks herdr which agents exist and how they are, so the panel can
// mark a recipient as running or gone. Returns map[target]status where target
// is the pane id; names, when herdr reports one, are added as additional keys
// pointing at the same status. Never returns an error for "herdr not installed";
// it returns an empty map and a nil error, because the absence of herdr is a
// normal state on a machine that only uses desktop notifications.
func agentStates(ctx context.Context, run runner) (map[string]string, error) {
	states := map[string]string{}
	out, err := run(ctx, "herdr", "agent", "list")
	if errors.Is(err, exec.ErrNotFound) {
		return states, nil
	}
	reply, ok := parseHerdr(out)
	switch {
	case !ok && err != nil:
		return nil, fmt.Errorf("herdr agent list: %w", describe(err, out))
	case !ok:
		return nil, errors.New("herdr agent list: unreadable output")
	case reply.Error != nil:
		return nil, errors.New("herdr agent list: " + reply.message())
	}

	// The name given with "herdr agent rename" comes back as display_agent.
	// The key is absent while an agent has no name, which is most of them, so
	// the pane id is the one key every agent is guaranteed to have.
	var list struct {
		Agents []struct {
			PaneID string `json:"pane_id"`
			Name   string `json:"display_agent"`
			Status string `json:"agent_status"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(reply.Result, &list); err != nil {
		return nil, fmt.Errorf("herdr agent list: %w", err)
	}
	for _, a := range list.Agents {
		if a.PaneID != "" {
			states[a.PaneID] = a.Status
		}
		if a.Name != "" {
			states[a.Name] = a.Status
		}
	}
	return states, nil
}
