// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"math"
	"testing"
)

func ring(nicks ...string) []ringEntry {
	out := make([]ringEntry, 0, len(nicks))
	for _, n := range nicks {
		var e ringEntry
		e.Type = "gc-message"
		e.Payload.FromNick = n
		e.Payload.Message = n + " said something"
		out = append(out, e)
	}
	return out
}

func nicks(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.FromNick)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dcrpulse reverses its ring on read, so the resource arrives newest first.
// Reading it as though it grew at the end showed the panel the oldest messages
// and left the unread badge pinned to an entry that never moved.
func TestRingToMessagesTurnsTheRingTheRightWayRound(t *testing.T) {
	// Newest first, the way the resource sends it.
	raw := ring("newest", "middle", "oldest")

	got := nicks(ringToMessages(raw, 20))
	want := []string{"oldest", "middle", "newest"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The cap has to drop the oldest, not the newest.
func TestRingToMessagesKeepsTheNewest(t *testing.T) {
	raw := ring("n1", "n2", "n3", "n4", "n5")

	got := nicks(ringToMessages(raw, 2))
	want := []string{"n2", "n1"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v: the cap must drop the oldest", got, want)
	}
}

func TestRingToMessagesHandlesAnEmptyRing(t *testing.T) {
	if got := ringToMessages(nil, 20); len(got) != 0 {
		t.Fatalf("got %d messages from an empty ring, want 0", len(got))
	}
}

// The captured live reply of dex_market_summary, one market on one host.
const dexSummaryLive = `[{"host":"dex.decred.org:7232","market":"dcr_btc","baseId":42,"baseSymbol":"dcr","quoteId":0,"quoteSymbol":"btc","lastRate":0.0001998,"lastRateUsd":15.967986030000002,"change24":-0.00005004754516790951,"high24":0.0002237,"low24":0.0001963,"vol24Base":13873.31946666,"stamp":1788658840104}]`

func TestParseDexSummary(t *testing.T) {
	d := parseDexSummary([]byte(dexSummaryLive), &Price{Sats: 20011.5413})
	if d == nil {
		t.Fatal("no market parsed")
	}
	if d.Host != "dex.decred.org:7232" || d.Market != "dcr_btc" {
		t.Fatalf("host/market: %+v", d)
	}
	// Conventional BTC per DCR becomes whole sats per DCR, the unit Price.Sats
	// already uses.
	if d.Rate != 19980 || d.High24 != 22370 || d.Low24 != 19630 {
		t.Fatalf("sats: rate %d high %d low %d", d.Rate, d.High24, d.Low24)
	}
	// change24 is a ratio on the wire: an absolute reading would put the 24h
	// open above the 24h high.
	if math.Abs(d.Change24-(-0.005)) > 0.0001 {
		t.Fatalf("change24 %v, want about -0.005 percent", d.Change24)
	}
	if math.Abs(d.RateUsd-15.968) > 0.001 || math.Abs(d.Volume24-13873.32) > 0.01 {
		t.Fatalf("usd %v volume %v", d.RateUsd, d.Volume24)
	}
	if d.Updated != "2026-09-06T01:40:40Z" {
		t.Fatalf("updated %q", d.Updated)
	}
	// 19980 against 20011.54 on the exchange: about 0.16 percent under.
	if math.Abs(d.Premium-(-0.1576)) > 0.001 {
		t.Fatalf("premium %v", d.Premium)
	}
	if again := parseDexSummary([]byte(dexSummaryLive), nil); again == nil || again.Premium != 0 {
		t.Fatalf("premium without a feed should be zero: %+v", again)
	}
}

func TestParseDexSummaryRefuses(t *testing.T) {
	cases := map[string]string{
		"malformed":    `{not json`,
		"empty":        `[]`,
		"other market": `[{"host":"h","market":"ltc_btc","baseSymbol":"ltc","quoteSymbol":"btc","lastRate":0.01}]`,
		"zero rate":    `[{"host":"h","market":"dcr_btc","baseSymbol":"dcr","quoteSymbol":"btc","lastRate":0}]`,
		"not an array": `{"host":"h"}`,
	}
	for why, raw := range cases {
		if d := parseDexSummary([]byte(raw), nil); d != nil {
			t.Errorf("%s: parsed %+v", why, d)
		}
	}
	// The first dcr_btc wins when several hosts answer.
	two := `[{"host":"a","market":"ltc_btc","baseSymbol":"ltc","quoteSymbol":"btc","lastRate":0.01},` +
		`{"host":"b","market":"dcr_btc","baseSymbol":"DCR","quoteSymbol":"BTC","lastRate":0.0002},` +
		`{"host":"c","market":"dcr_btc","baseSymbol":"dcr","quoteSymbol":"btc","lastRate":0.0003}]`
	if d := parseDexSummary([]byte(two), nil); d == nil || d.Host != "b" || d.Rate != 20000 {
		t.Fatalf("first dcr_btc should win: %+v", d)
	}
}
