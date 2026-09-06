// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ArmRequest is what both arming paths hand to the store: the setup tool with
// its flags parsed, and the panel through the helper's one-shot with the form
// decoded. One shape, so the two cannot drift in what they accept.
type ArmRequest struct {
	// Connection is the id the trigger is bound to. Empty means the one the
	// widget is watching now.
	Connection string `json:"connection,omitempty"`
	Path       string `json:"path,omitempty"`
	Operator   string `json:"operator,omitempty"`
	Params     Params `json:"params"`
	// DeliverTo is a herdr agent name or pane id, or "you" for a desktop
	// notification, which is also what an empty value means.
	DeliverTo string `json:"deliverTo,omitempty"`
	// Once is nil for the default, which is a one-shot.
	Once *bool `json:"once,omitempty"`
	// Expires is as typed: 4d, 12h, a date, or an RFC 3339 time.
	Expires string `json:"expires,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// ArmedBy names who asked: a pane id when an agent did, else "you".
	ArmedBy string `json:"armedBy,omitempty"`
}

// You is the recipient that means the person at the desk rather than an agent.
const You = "you"

// NewTrigger is the pure part of arming: the trigger req describes, bound to
// conn, validated, and without an id. The store mints the id under its lock,
// so a preview never shows one that could be disarmed later. It carries no
// last_value: a baseline seeded here would be history the trigger never saw,
// and the first sample would turn it into a false event.
func NewTrigger(req ArmRequest, conn string, now time.Time) (Trigger, error) {
	expires, err := ParseExpiry(req.Expires, now)
	if err != nil {
		return Trigger{}, err
	}
	t := Trigger{
		Rev:        1,
		Connection: conn,
		Path:       strings.TrimSpace(req.Path),
		Operator:   strings.TrimSpace(req.Operator),
		Params:     req.Params,
		DeliverTo:  orYou(req.DeliverTo),
		Once:       req.Once,
		ExpiresAt:  expires,
		Reason:     strings.TrimSpace(req.Reason),
		ArmedBy:    orYou(req.ArmedBy),
		ArmedAt:    now,
		State:      State{Ready: true},
	}
	if err := ValidateTrigger(&t); err != nil {
		return Trigger{}, err
	}
	return t, nil
}

// PrepareArm resolves the connection and builds the trigger as Arm would store
// it, reading only. It is what a dry run shows.
func PrepareArm(req ArmRequest, now time.Time) (Trigger, []string, error) {
	conn, err := resolveConnection(req.Connection)
	if err != nil {
		return Trigger{}, nil, err
	}
	t, err := NewTrigger(req, conn, now)
	if err != nil {
		return Trigger{}, nil, err
	}
	return t, ArmWarnings(t), nil
}

// Arm validates a request and adds it to the store under the lock. The
// warnings say why an otherwise valid trigger might never fire.
func Arm(req ArmRequest, now time.Time) (Trigger, []string, error) {
	t, warnings, err := PrepareArm(req, now)
	if err != nil {
		return Trigger{}, nil, err
	}
	err = WithTriggerLock(func() error {
		list, err := LoadTriggers()
		if err != nil {
			return err
		}
		for t.ID == "" || list.has(t.ID) {
			t.ID = NewTriggerID()
		}
		if err := list.Add(t); err != nil {
			return err
		}
		return SaveTriggers(list)
	})
	if err != nil {
		return Trigger{}, nil, err
	}
	return t, warnings, nil
}

// applyEdit is the pure part of an edit: req laid over t, the rev bumped, and
// the trigger re-armed. Fields left empty in the request keep their value. The
// clock of a stall and the last value survive an edit of the bound, because
// they are still true; they do not survive a change of path, because a stale
// number on a new path would manufacture a transition.
func applyEdit(t Trigger, req ArmRequest, now time.Time) (Trigger, error) {
	next := t
	if p := strings.TrimSpace(req.Path); p != "" && p != t.Path {
		next.Path = p
		next.State.LastValue = nil
		next.State.LastChangedAt = nil
	}
	if op := strings.TrimSpace(req.Operator); op != "" {
		next.Operator = op
	}
	if !req.Params.IsZero() {
		next.Params = req.Params
	}
	if d := strings.TrimSpace(req.DeliverTo); d != "" {
		next.DeliverTo = d
	}
	if req.Once != nil {
		next.Once = req.Once
	}
	if req.Expires != "" {
		e, err := ParseExpiry(req.Expires, now)
		if err != nil {
			return Trigger{}, err
		}
		next.ExpiresAt = e
	}
	if r := strings.TrimSpace(req.Reason); r != "" {
		next.Reason = r
	}
	next.Rev++
	// An edit re-arms: a spent one-shot given a new expiry is live again.
	next.State.FiredAt = nil
	next.State.RefValue = nil
	next.State.Ready = true
	next.State.Delivered = ""
	next.State.DeliveryError = ""
	if err := ValidateTrigger(&next); err != nil {
		return Trigger{}, err
	}
	return next, nil
}

// PrepareEdit shows what EditTrigger would store: an unlocked read and no
// write. A concurrent save can only make the preview stale, never lose anything.
func PrepareEdit(id string, req ArmRequest, now time.Time) (Trigger, []string, error) {
	list, err := LoadTriggers()
	if err != nil {
		return Trigger{}, nil, err
	}
	t, ok := list.Find(id)
	if !ok {
		return Trigger{}, nil, ErrTriggerNotFound
	}
	next, err := applyEdit(*t, req, now)
	if err != nil {
		return Trigger{}, nil, err
	}
	return next, ArmWarnings(next), nil
}

// EditTrigger changes a trigger's definition and bumps its rev, which is how
// the running helper learns to drop the state it computed against the old one.
func EditTrigger(id string, req ArmRequest, now time.Time) (Trigger, []string, error) {
	var out Trigger
	err := WithTriggerLock(func() error {
		list, err := LoadTriggers()
		if err != nil {
			return err
		}
		t, ok := list.Find(id)
		if !ok {
			return ErrTriggerNotFound
		}
		next, err := applyEdit(*t, req, now)
		if err != nil {
			return err
		}
		*t = next
		out = next
		return SaveTriggers(list)
	})
	if err != nil {
		return Trigger{}, nil, err
	}
	return out, ArmWarnings(out), nil
}

// Disarm deletes a trigger now, spent or not, and returns what it was.
func Disarm(id string) (Trigger, error) {
	var out Trigger
	err := WithTriggerLock(func() error {
		list, err := LoadTriggers()
		if err != nil {
			return err
		}
		t, ok := list.Find(id)
		if !ok {
			return ErrTriggerNotFound
		}
		out = *t
		if err := list.Remove(id); err != nil {
			return err
		}
		return SaveTriggers(list)
	})
	return out, err
}

// ArmWarnings names the settings under which a valid trigger is not being
// watched. They are returned rather than enforced: switching monitoring on is
// the user's decision, and the agent that armed this should know to say so.
func ArmWarnings(t Trigger) []string {
	var w []string
	if !BoolSetting("monitoring", false) {
		w = append(w, "monitoring is off: nothing is watched until it is switched on in the panel")
	}
	if strings.HasPrefix(t.Path, "wallet.") && !BoolSetting("showBalances", false) {
		w = append(w, "wallet paths are not fetched while showBalances is off, so this cannot fire until it is on")
	}
	return w
}

// IsZero reports whether no parameter was given at all.
func (p Params) IsZero() bool {
	return p.Bound == nil && p.Direction == "" && p.Rearm == nil && p.Value == "" &&
		p.Hold == "" && p.By == nil && !p.Percent && p.For == "" &&
		len(p.Key) == 0 && len(p.Where) == 0
}

func orYou(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return You
	}
	return s
}

// resolveConnection turns an id, or nothing, into the connection a trigger is
// bound to. Nothing means the active one; an explicit id has to exist, since a
// trigger on a connection that is not there would never be evaluated and never
// say why.
func resolveConnection(id string) (string, error) {
	list, err := LoadConnections()
	if err != nil {
		return "", err
	}
	if id == "" {
		conn, err := list.Active(StringSetting("activeConnection", ""))
		if err != nil {
			return "", fmt.Errorf("no connection to bind the trigger to: %w", err)
		}
		return conn.ID, nil
	}
	if _, ok := list.Find(id); !ok {
		return "", fmt.Errorf("connection %q: %w", id, errors.New("not configured"))
	}
	return id, nil
}
