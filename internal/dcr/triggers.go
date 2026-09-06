// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// TriggersFile is the on-disk name of the armed trigger list.
const TriggersFile = "triggers.json"

// triggerSchema is bumped only if the file shape changes incompatibly.
const triggerSchema = 1

// TriggerLockFile serialises writers. It is a separate file from the data
// because WritePrivate replaces the data inode on every save: a lock taken on
// triggers.json would be a lock on whichever inode the taker opened, and a
// second writer arriving after a rename would lock a different one and see no
// contention at all.
const TriggerLockFile = "triggers.lock"

// triggerRetention is how long a finished trigger stays in the file, so the
// agent that armed it can still read the outcome after a restart, and a
// listing shows what fired recently rather than only what is pending.
const triggerRetention = 24 * time.Hour

// Params are the operator's arguments. Which ones apply is decided by the
// operator; ValidateTrigger says which are missing.
type Params struct {
	Bound     *float64          `json:"bound,omitempty"`
	Direction string            `json:"direction,omitempty"`
	Rearm     *float64          `json:"rearm,omitempty"`
	Value     string            `json:"value,omitempty"`
	Hold      string            `json:"hold,omitempty"`
	By        *float64          `json:"by,omitempty"`
	Percent   bool              `json:"percent,omitempty"`
	For       string            `json:"for,omitempty"`
	Key       []string          `json:"key,omitempty"`
	Where     map[string]string `json:"where,omitempty"`
}

// State is what the evaluator remembers between samples. Values are kept in
// their stored form (see Value.MarshalJSON) and decoded against the leaf's kind
// when read, so a trigger whose leaf changed shape starts over rather than
// comparing a number to a list.
type State struct {
	LastValue     json.RawMessage `json:"last_value,omitempty"`
	LastChangedAt *time.Time      `json:"last_changed_at,omitempty"`
	RefValue      json.RawMessage `json:"ref_value,omitempty"`
	Ready         bool            `json:"ready"`
	FiredAt       *time.Time      `json:"fired_at,omitempty"`
	FireCount     int             `json:"fire_count,omitempty"`
	Delivered     string          `json:"delivered,omitempty"`
	DeliveryError string          `json:"delivery_error,omitempty"`
}

// Trigger is one armed condition: a leaf, an operator over it, and who to wake
// when it trips.
type Trigger struct {
	ID         string `json:"id"`
	Rev        int    `json:"rev"`
	Connection string `json:"connection"`
	Path       string `json:"path"`
	Operator   string `json:"operator"`
	Params     Params `json:"params"`
	// DeliverTo is a herdr agent name or pane id, or the literal "you" for the
	// operator's own desktop.
	DeliverTo string `json:"deliver_to"`
	// Once is a pointer so an older file that never wrote it reads as the
	// default, which is to fire one time and stop.
	Once      *bool     `json:"once"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"`
	ArmedBy   string    `json:"armed_by"`
	ArmedAt   time.Time `json:"armed_at"`
	State     State     `json:"state"`
}

// Triggers is the whole file.
type Triggers struct {
	Version  int       `json:"version"`
	Triggers []Trigger `json:"triggers"`
}

// ErrTriggerNotFound is returned for an unknown id.
var ErrTriggerNotFound = errors.New("no such trigger")

// TriggersPath is the 0600 file holding every armed trigger and its state.
func TriggersPath() string { return filepath.Join(ConfigDir(), TriggersFile) }

// LoadTriggers reads the trigger list. A missing file is an empty list, since
// nothing has been armed yet; every other failure is reported, because a file
// that exists and cannot be read means triggers that were armed are not being
// watched.
func LoadTriggers() (*Triggers, error) {
	f, err := os.OpenFile(TriggersPath(), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return &Triggers{Version: triggerSchema}, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s is group- or world-readable (%04o); run: chmod 600 %s",
			ErrConfig, TriggersPath(), info.Mode().Perm(), TriggersPath())
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	var list Triggers
	// Unknown fields are refused rather than dropped: an agent that misspells a
	// parameter has armed something other than what it meant, and the next save
	// would erase the evidence.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, TriggersPath(), err)
	}
	if list.Version != triggerSchema {
		return nil, fmt.Errorf("%w: %s: unknown version %d", ErrConfig, TriggersPath(), list.Version)
	}
	seen := map[string]bool{}
	for i := range list.Triggers {
		t := &list.Triggers[i]
		if t.ID == "" {
			return nil, fmt.Errorf("%w: %s: triggers[%d]: id is empty", ErrConfig, TriggersPath(), i)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("%w: %s: triggers[%d]: id %q listed twice", ErrConfig, TriggersPath(), i, t.ID)
		}
		seen[t.ID] = true
		if err := ValidateTrigger(t); err != nil {
			return nil, fmt.Errorf("%w: %s: triggers[%d]: %v", ErrConfig, TriggersPath(), i, err)
		}
	}
	return &list, nil
}

// SaveTriggers writes the list atomically, 0600 inside a 0700 directory,
// dropping what has been finished for longer than triggerRetention.
func SaveTriggers(l *Triggers) error {
	for i := range l.Triggers {
		if err := ValidateTrigger(&l.Triggers[i]); err != nil {
			return fmt.Errorf("%w: triggers[%d]: %v", ErrConfig, i, err)
		}
	}
	l.Prune(time.Now())
	l.Version = triggerSchema
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(TriggersPath(), append(b, '\n'))
}

// WithTriggerLock runs fn holding the exclusive writer lock, so a load, change
// and save by one process cannot interleave with another's. The evaluator and
// every CLI invocation share the file, and a lost update here is a trigger
// that was armed and then silently gone.
func WithTriggerLock(fn func() error) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, TriggerLockFile), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

var triggerIDPattern = regexp.MustCompile(`^t-[0-9a-f]{8}$`)

// ValidateTrigger checks a trigger against the catalogue and the operator's
// needs. Every failure names the field, because the CLI shows this to an agent
// that has to correct what it typed.
func ValidateTrigger(t *Trigger) error {
	if t == nil {
		return errors.New("trigger is nil")
	}
	if t.ID != "" && !triggerIDPattern.MatchString(t.ID) {
		return fmt.Errorf("id %q: not of the form t-xxxxxxxx", clip(t.ID))
	}
	if t.Rev < 1 {
		return fmt.Errorf("rev %d: must be at least 1", t.Rev)
	}
	if strings.TrimSpace(t.Connection) == "" {
		return errors.New("connection is empty")
	}
	leaf, ok := LookupLeaf(t.Path)
	if !ok {
		return fmt.Errorf("path %q: not in the catalogue", clip(t.Path))
	}
	if !containsString(leaf.Operators, t.Operator) {
		return fmt.Errorf("operator %q: %s offers %s", clip(t.Operator), t.Path,
			strings.Join(leaf.Operators, ", "))
	}

	p := &t.Params
	switch t.Operator {
	case "crosses", "count":
		if p.Bound == nil {
			return fmt.Errorf("%s needs params.bound", t.Operator)
		}
		if p.Direction != "above" && p.Direction != "below" {
			return fmt.Errorf(`params.direction %q: "above" or "below"`, clip(p.Direction))
		}
	case "becomes":
		if p.Value == "" {
			return errors.New("becomes needs params.value")
		}
	case "changes":
		if p.By == nil || *p.By <= 0 {
			return errors.New("changes needs params.by greater than zero")
		}
	case "stalls":
		if d, err := time.ParseDuration(p.For); err != nil || d <= 0 {
			return fmt.Errorf(`params.for %q: not a duration greater than zero, for example "30m"`, clip(p.For))
		}
	}

	// A filter may name only what an element actually carries, so a typo is
	// refused here rather than matching nothing forever.
	where := make([]string, 0, len(p.Where))
	for k := range p.Where {
		where = append(where, k)
	}
	sort.Strings(where)
	for _, k := range where {
		if name := strings.TrimSuffix(k, "~"); !containsString(leaf.Fields, name) {
			return fmt.Errorf("params.where key %q: %s", clip(k), fieldsOf(t.Path, leaf))
		}
	}
	for _, k := range p.Key {
		if !containsString(leaf.Fields, k) {
			return fmt.Errorf("params.key %q: %s", clip(k), fieldsOf(t.Path, leaf))
		}
	}

	if p.Hold != "" {
		if d, err := time.ParseDuration(p.Hold); err != nil || d < 0 {
			return fmt.Errorf(`params.hold %q: not a duration, for example "5m"`, clip(p.Hold))
		}
	}
	if p.Rearm != nil && *p.Rearm < 0 {
		return fmt.Errorf("params.rearm %v: must not be negative", *p.Rearm)
	}
	if strings.TrimSpace(t.DeliverTo) == "" {
		return errors.New("deliver_to is empty")
	}
	if t.ExpiresAt.IsZero() {
		return errors.New("expires_at is missing")
	}
	if !t.ExpiresAt.After(t.ArmedAt) {
		return fmt.Errorf("expires_at %s: not after armed_at %s",
			t.ExpiresAt.Format(time.RFC3339), t.ArmedAt.Format(time.RFC3339))
	}
	return nil
}

// fieldsOf is the tail of a refusal naming a field a list element does not
// have, so the message says what could have been written instead.
func fieldsOf(path string, leaf Leaf) string {
	if len(leaf.Fields) == 0 {
		return path + " has no fields to filter on"
	}
	return path + " has fields " + strings.Join(leaf.Fields, ", ")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// NewTriggerID mints an id. Random rather than sequential so two processes
// arming at once cannot both pick the next number.
func NewTriggerID() string {
	var b [4]byte
	// crypto/rand.Read cannot fail as of Go 1.24: it stops the process rather
	// than return short randomness, so there is nothing to check.
	rand.Read(b[:])
	return "t-" + hex.EncodeToString(b[:])
}

// Find returns a trigger by id.
func (l *Triggers) Find(id string) (*Trigger, bool) {
	for i := range l.Triggers {
		if l.Triggers[i].ID == id {
			return &l.Triggers[i], true
		}
	}
	return nil, false
}

// Add validates and appends a trigger, minting an id and a first revision when
// the caller left them empty.
func (l *Triggers) Add(t Trigger) error {
	for t.ID == "" {
		if id := NewTriggerID(); !l.has(id) {
			t.ID = id
		}
	}
	if t.Rev == 0 {
		t.Rev = 1
	}
	if err := ValidateTrigger(&t); err != nil {
		return err
	}
	if l.has(t.ID) {
		return fmt.Errorf("id %q: already armed", t.ID)
	}
	l.Triggers = append(l.Triggers, t)
	return nil
}

func (l *Triggers) has(id string) bool {
	_, ok := l.Find(id)
	return ok
}

// Remove deletes a trigger.
func (l *Triggers) Remove(id string) error {
	for i := range l.Triggers {
		if l.Triggers[i].ID == id {
			l.Triggers = append(l.Triggers[:i], l.Triggers[i+1:]...)
			return nil
		}
	}
	return ErrTriggerNotFound
}

// Prune drops triggers that finished more than triggerRetention ago: a
// one-shot that fired, and anything that expired without firing. A repeating
// trigger that is still inside its window stays, fired or not.
func (l *Triggers) Prune(now time.Time) {
	keep := l.Triggers[:0]
	for _, t := range l.Triggers {
		switch {
		case t.Spent() && !now.Before(t.State.FiredAt.Add(triggerRetention)):
			continue
		case t.Expired(now) && !now.Before(t.ExpiresAt.Add(triggerRetention)):
			continue
		}
		keep = append(keep, t)
	}
	l.Triggers = keep
}

// IsOnce reports whether the trigger disarms after its first fire, which is the
// default an absent field reads as.
func (t *Trigger) IsOnce() bool { return t.Once == nil || *t.Once }

// Spent reports whether a one-shot has done its one thing.
func (t *Trigger) Spent() bool { return t.IsOnce() && t.State.FiredAt != nil }

// Expired reports whether the window closed on a trigger that is not spent. A
// spent trigger is finished for a different reason and is not also expired.
func (t *Trigger) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) && !t.Spent() }
