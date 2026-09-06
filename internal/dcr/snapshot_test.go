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

// The live reply of dcrpulse://mcp/audit after one message was sent.
const auditLive = `[{"time":"2026-09-06T01:49:25.56315081Z","agentId":"95c2ef004368d113","agent":"omarchy","tool":"br_send_message","account":0,"amountDcr":0,"target":"karamble","result":"ok"}]`

func TestParseAudit(t *testing.T) {
	a := parseAudit([]byte(auditLive))
	if a == nil || a.Count != 1 || a.Denied != 0 || len(a.Entries) != 1 {
		t.Fatalf("live audit: %+v", a)
	}
	e := a.Entries[0]
	if e.Agent != "omarchy" || e.Tool != "br_send_message" || e.Target != "karamble" || e.Result != "ok" ||
		e.AgentID != "95c2ef004368d113" || a.Last != e.Time || e.Time != "2026-09-06T01:49:25.56315081Z" {
		t.Fatalf("entry: %+v", e)
	}
	// The ring is counted whole, the panel carries only the newest.
	var b []byte
	b = append(b, '[')
	for i := 0; i < 25; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		result := "ok"
		switch i % 5 {
		case 1:
			result = "denied"
		case 2:
			result = "blocked"
		}
		b = append(b, []byte(`{"time":"2026-09-06T01:00:`+string(rune('0'+i/10))+string(rune('0'+i%10))+`Z","agentId":"a","agent":"duty","tool":"wallet_send","amountDcr":1.5,"target":"DsXk3QfMhbnkJ7Yz2sGw9vQxvA7eXk3Qf4","result":"`+result+`","detail":"x"}`)...)
	}
	b = append(b, ']')
	a = parseAudit(b)
	if a == nil || a.Count != 25 || a.Denied != 10 || len(a.Entries) != auditCarry || a.Last != a.Entries[0].Time {
		t.Fatalf("ring of 25: %+v", a)
	}
	for _, raw := range []string{`null`, `{not json`, `{"a":1}`} {
		if parseAudit([]byte(raw)) != nil {
			t.Errorf("%s should be no sample", raw)
		}
	}
	if a := parseAudit([]byte(`[]`)); a == nil || a.Count != 0 || a.Last != "" || len(a.Entries) != 0 {
		t.Fatalf("an empty ring is a sample with nothing in it: %+v", a)
	}
}

// The golden reply of dcrpulse://bisonrelay/mcp agreed with the dcrpulse side.
const brmcpGolden = `{"enabled":true,"mode":"approval","perCallCapDcr":0.05,"perDayCapDcr":0.5,"approvalTimeoutSecs":120,"todayDcr":0.012,"lastDenied":{"ip":"10.0.0.9","at":"2026-09-05T20:00:00Z"},"pending":[{"id":"p-7f3a","bot":"8cafda06372331b1","botNick":"braibot","tool":"image","amountDcr":0.004,"created":"2026-09-06T02:10:00Z","expiresAt":"2026-09-06T02:12:00Z"}],"spend":[{"ts":"2026-09-06T01:40:00Z","bot":"8cafda06372331b1","botNick":"braibot","tool":"image","rail":"tip","amountDcr":0.004,"status":"paid"},{"ts":"2026-09-06T01:35:00Z","bot":"87df4e08913b6383","tool":"search","rail":"tip","amountDcr":0.001,"status":"failed","err":"tip timed out"}]}`

func TestParseBRMCP(t *testing.T) {
	b := parseBRMCP([]byte(brmcpGolden))
	if b == nil || !b.Enabled || b.Mode != "approval" || b.TodayDcr != 0.012 || b.PerDayCapDcr != 0.5 {
		t.Fatalf("golden: %+v", b)
	}
	if b.PendingCount != 1 || len(b.Pending) != 1 || b.Pending[0].ID != "p-7f3a" || b.Pending[0].BotNick != "braibot" ||
		b.Pending[0].ExpiresAt != "2026-09-06T02:12:00Z" || b.Pending[0].AmountDcr != 0.004 {
		t.Fatalf("pending: %+v", b.Pending)
	}
	if len(b.Spend) != 2 || b.Spend[0].Status != "paid" || b.Spend[1].Err != "tip timed out" || b.Spend[1].BotNick != "" {
		t.Fatalf("spend: %+v", b.Spend)
	}
	if b.LastDenied != "10.0.0.9 at 2026-09-05T20:00:00Z" || b.Error != "" {
		t.Fatalf("denied/error: %q %q", b.LastDenied, b.Error)
	}

	down := parseBRMCP([]byte(`{"enabled":false,"pending":[],"spend":[],"error":"brclientd /settings/mcpclient: dial tcp 127.0.0.1:7677: connection refused"}`))
	if down == nil || down.Enabled || down.Error == "" || down.PendingCount != 0 || down.Pending == nil || down.Spend == nil {
		t.Fatalf("unreachable bridge: %+v", down)
	}
	off := parseBRMCP([]byte(`{"enabled":false}`))
	if off == nil || off.Enabled || off.Error != "" || len(off.Pending) != 0 || len(off.Spend) != 0 {
		t.Fatalf("switched off: %+v", off)
	}
	for _, raw := range []string{`null`, `{not json`} {
		if parseBRMCP([]byte(raw)) != nil {
			t.Errorf("%s should be no sample", raw)
		}
	}
}
