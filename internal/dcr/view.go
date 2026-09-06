// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TriggerView is a trigger as the panel and the CLI list see it: the
// definition and a status, never a sampled value. A masked wallet figure must
// not be able to reach the panel through last_value, so the state is
// summarised rather than copied.
type TriggerView struct {
	ID         string    `json:"id"`
	Rev        int       `json:"rev"`
	Connection string    `json:"connection"`
	Path       string    `json:"path"`
	Operator   string    `json:"operator"`
	Params     string    `json:"params"`
	DeliverTo  string    `json:"deliverTo"`
	Once       bool      `json:"once"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Reason     string    `json:"reason,omitempty"`
	ArmedBy    string    `json:"armedBy"`
	ArmedAt    time.Time `json:"armedAt"`

	// Status is one of armed, rearming, no-sample, other-connection, fired,
	// expired or delivery-failed.
	Status        string     `json:"status"`
	FiredAt       *time.Time `json:"firedAt,omitempty"`
	LastChangedAt *time.Time `json:"lastChangedAt,omitempty"`
	FireCount     int        `json:"fireCount,omitempty"`
	Delivered     string     `json:"delivered,omitempty"`
	DeliveryError string     `json:"deliveryError,omitempty"`
}

// View summarises a trigger for display. active is the connection being
// watched, or empty when that is not known, as in the one-shot list; sampled
// says whether the last pass could resolve the path.
func (t *Trigger) View(now time.Time, active string, sampled bool) TriggerView {
	return TriggerView{
		ID:            t.ID,
		Rev:           t.Rev,
		Connection:    t.Connection,
		Path:          t.Path,
		Operator:      t.Operator,
		Params:        DescribeParams(t),
		DeliverTo:     t.DeliverTo,
		Once:          t.IsOnce(),
		ExpiresAt:     t.ExpiresAt,
		Reason:        t.Reason,
		ArmedBy:       t.ArmedBy,
		ArmedAt:       t.ArmedAt,
		Status:        t.status(now, active, sampled),
		FiredAt:       t.State.FiredAt,
		LastChangedAt: t.State.LastChangedAt,
		FireCount:     t.State.FireCount,
		Delivered:     t.State.Delivered,
		DeliveryError: t.State.DeliveryError,
	}
}

// status picks the one word the row leads with. A failed delivery outranks
// everything but the wrong connection, because it is the one state where the
// alarm did its job and nobody heard it.
func (t *Trigger) status(now time.Time, active string, sampled bool) string {
	switch {
	case active != "" && t.Connection != active:
		return "other-connection"
	case t.State.DeliveryError != "" && t.State.Delivered == "":
		return "delivery-failed"
	case t.Spent():
		return "fired"
	case t.Expired(now):
		return "expired"
	case !sampled:
		return "no-sample"
	case !t.State.Ready:
		return "rearming"
	}
	return "armed"
}

// DescribeParams renders the parameters the way the CLI accepts them, so a
// row in the panel doubles as the command that would recreate it.
func DescribeParams(t *Trigger) string {
	p := t.Params
	var parts []string
	switch t.Operator {
	case "crosses", "count":
		if p.Bound != nil {
			parts = append(parts, p.Direction+" "+formatNum(*p.Bound))
		}
		if p.Rearm != nil {
			parts = append(parts, "rearm "+formatNum(*p.Rearm))
		}
	case "becomes":
		parts = append(parts, strconv.Quote(p.Value))
		if p.Hold != "" {
			parts = append(parts, "hold "+p.Hold)
		}
	case "changes":
		if p.By != nil {
			s := "by " + formatNum(*p.By)
			if p.Percent {
				s += "%"
			}
			parts = append(parts, s)
		}
	case "stalls":
		parts = append(parts, "for "+p.For)
	}
	if w := describeWhere(p.Where); w != "" {
		parts = append(parts, "where "+w)
	}
	if len(p.Key) > 0 {
		parts = append(parts, "key "+strings.Join(p.Key, ","))
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, " ")
}

func describeWhere(where map[string]string) string {
	keys := make([]string, 0, len(where))
	for k := range where {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+where[k])
	}
	return strings.Join(parts, " ")
}

func formatNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// FireText is the whole of what a woken agent receives. It names the trigger,
// the condition and who armed it, and deliberately carries no value: the agent
// has its own dcrpulse to look with, and this text is written into a prompt,
// where a balance would be one paste away from anywhere.
func FireText(t *Trigger, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "demarchy alarm %s: %s %s %s on connection %q fired at %s.",
		t.ID, t.Path, t.Operator, DescribeParams(t), t.Connection, now.UTC().Format(time.RFC3339))
	if t.Reason != "" {
		fmt.Fprintf(&b, " Reason: %q.", t.Reason)
	}
	fmt.Fprintf(&b, " Armed by %s at %s.", t.ArmedBy, t.ArmedAt.UTC().Format(time.RFC3339))
	b.WriteString(" This message carries no values; read dcrpulse yourself.")
	if t.IsOnce() {
		b.WriteString(" This was a one-shot and is now spent.")
	} else {
		fmt.Fprintf(&b, " It stays armed until %s; disarm with: demarchy-setup disarm %s.",
			t.ExpiresAt.UTC().Format(time.RFC3339), t.ID)
	}
	return b.String()
}

var daysPattern = regexp.MustCompile(`^([0-9]{1,4})d$`)

// ParseExpiry reads the ways an expiry is written: a span such as 4d, 12h or
// 90m counted from now, a date meaning the end of that local day, or an RFC
// 3339 time. Whatever it is, it must lie ahead.
func ParseExpiry(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("expiry is empty: every trigger expires, for example --expires 4d")
	}
	var at time.Time
	if m := daysPattern.FindStringSubmatch(s); m != nil {
		days, _ := strconv.Atoi(m[1])
		at = now.Add(time.Duration(days) * 24 * time.Hour)
	} else if d, err := time.ParseDuration(s); err == nil {
		at = now.Add(d)
	} else if ts, err := time.Parse(time.RFC3339, s); err == nil {
		at = ts
	} else if day, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		at = day.AddDate(0, 0, 1)
	} else {
		return time.Time{}, fmt.Errorf("expiry %q: write a span such as 4d, 12h or 90m, a date, or an RFC 3339 time", s)
	}
	if !at.After(now) {
		return time.Time{}, fmt.Errorf("expiry %s: already past", at.Format(time.RFC3339))
	}
	return at, nil
}
