// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDescribeParamsReadsLikeTheCommand(t *testing.T) {
	cases := []struct {
		op   string
		p    Params
		want string
	}{
		{"crosses", Params{Bound: fp(16), Direction: "above"}, "above 16"},
		{"crosses", Params{Bound: fp(4), Direction: "below", Rearm: fp(0.5)}, "below 4 rearm 0.5"},
		{"becomes", Params{Value: "stopped", Hold: "10m"}, `"stopped" hold 10m`},
		{"changes", Params{By: fp(5), Percent: true}, "by 5%"},
		{"changes", Params{By: fp(0.5)}, "by 0.5"},
		{"stalls", Params{For: "45m"}, "for 45m"},
		{"appears", Params{Where: map[string]string{"fromNick": "alice"}}, "where fromNick=alice"},
		{"appears", Params{Where: map[string]string{"text~": "invoice", "fromNick": "alice"}}, "where fromNick=alice text~=invoice"},
		{"disappears", Params{}, "any"},
		{"disappears", Params{Key: []string{"alias"}}, "key alias"},
		{"count", Params{Bound: fp(2), Direction: "above", Where: map[string]string{"fromNick": "alice"}}, "above 2 where fromNick=alice"},
	}
	for _, c := range cases {
		got := DescribeParams(&Trigger{Operator: c.op, Params: c.p})
		if got != c.want {
			t.Errorf("%s %+v: got %q, want %q", c.op, c.p, got, c.want)
		}
	}
}

func TestFireTextCarriesNoValue(t *testing.T) {
	tr := oneShot("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
	tr.Reason = "smoke"
	tr.ArmedBy = "w8:p1"
	run(t, tr, []sample{n(15.87, 0), n(16.5, 8*time.Second)})
	text := FireText(tr, t0.Add(8*time.Second))
	for _, want := range []string{
		"demarchy alarm " + tr.ID + ":",
		"price.dcrUsd crosses above 16",
		`on connection "c"`,
		"fired at 2026-09-06T12:00:08Z",
		`Reason: "smoke"`,
		"Armed by w8:p1 at 2026-09-06T12:00:00Z",
		"carries no values",
		"one-shot and is now spent",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("fire text lacks %q:\n%s", want, text)
		}
	}
	for _, leak := range []string{"15.87", "16.5"} {
		if strings.Contains(text, leak) {
			t.Errorf("fire text carries the value %s:\n%s", leak, text)
		}
	}
	if strings.Contains(text, "\n") {
		t.Error("fire text must be a single line")
	}

	tr.Once = bp(false)
	tr.Reason = ""
	text = FireText(tr, t0)
	if !strings.Contains(text, "stays armed until 2026-10-06T12:00:00Z") ||
		!strings.Contains(text, "demarchy-setup disarm "+tr.ID) {
		t.Errorf("standing text should say how to disarm:\n%s", text)
	}
	if strings.Contains(text, "Reason") {
		t.Errorf("no reason given, none should be printed:\n%s", text)
	}
}

func TestParseExpiry(t *testing.T) {
	good := map[string]time.Time{
		"4d":                   t0.Add(96 * time.Hour),
		"12h":                  t0.Add(12 * time.Hour),
		"90m":                  t0.Add(90 * time.Minute),
		" 1h ":                 t0.Add(time.Hour),
		"2026-09-13T20:00:00Z": time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC),
		"2026-09-10":           time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range good {
		got, err := ParseExpiry(in, t0)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseExpiry(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "yesterday", "-1h", "0s", "2026-09-05", "2026-09-06T11:59:59Z", "4dd", "d"} {
		if got, err := ParseExpiry(in, t0); err == nil {
			t.Errorf("ParseExpiry(%q) = %v, want an error", in, got)
		}
	}
}

func TestViewStatus(t *testing.T) {
	future := t0.Add(time.Hour)
	fired := t0.Add(-time.Minute)
	cases := []struct {
		name    string
		tweak   func(*Trigger)
		active  string
		sampled bool
		want    string
	}{
		{"fresh", func(*Trigger) {}, "c", true, "armed"},
		{"active unknown", func(*Trigger) {}, "", true, "armed"},
		{"rearming", func(tr *Trigger) { tr.Once = bp(false); tr.State.Ready = false }, "c", true, "rearming"},
		{"no sample", func(*Trigger) {}, "c", false, "no-sample"},
		{"other connection", func(*Trigger) {}, "elsewhere", true, "other-connection"},
		{"spent", func(tr *Trigger) { tr.State.FiredAt = &fired }, "c", true, "fired"},
		{"expired", func(tr *Trigger) { tr.ExpiresAt = t0.Add(-time.Second) }, "c", true, "expired"},
		{"delivery failed", func(tr *Trigger) {
			tr.State.FiredAt = &fired
			tr.State.DeliveryError = "herdr: agent_not_found"
		}, "c", true, "delivery-failed"},
		{"delivered", func(tr *Trigger) {
			tr.State.FiredAt = &fired
			tr.State.Delivered = "agent"
		}, "c", true, "fired"},
	}
	for _, c := range cases {
		tr := oneShot("price.dcrUsd", "crosses", Params{Bound: fp(16), Direction: "above"})
		tr.ExpiresAt = future
		tr.State.Ready = true
		c.tweak(tr)
		if got := tr.View(t0, c.active, c.sampled).Status; got != c.want {
			t.Errorf("%s: status %q, want %q", c.name, got, c.want)
		}
	}
}

// The view is what crosses into the panel, so it must not carry the value.
func TestViewOmitsTheValue(t *testing.T) {
	tr := oneShot("wallet.total", "crosses", Params{Bound: fp(100), Direction: "below"})
	run(t, tr, []sample{n(123.456, 0)})
	raw, err := json.Marshal(tr.View(t0, "c", true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "123.456") || strings.Contains(string(raw), "last_value") {
		t.Fatalf("view leaks the sampled value: %s", raw)
	}
	for _, want := range []string{`"deliverTo":"you"`, `"status":"armed"`, `"params":"below 100"`, `"once":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("view lacks %s: %s", want, raw)
		}
	}
}
