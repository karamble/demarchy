// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"strings"
	"testing"
)

// capsFrom builds Capabilities from the wire shape the server actually sends,
// so the test exercises the JSON tags rather than a hand-built struct.
func capsFrom(t *testing.T, body string) *Capabilities {
	t.Helper()
	var c Capabilities
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &c
}

func TestCheckReadOnlyAcceptsReadOnlyToken(t *testing.T) {
	// The shape dcrpulse returns for an agent with no grant.
	caps := capsFrom(t, `{
		"agent":"omarchy",
		"domains":["node","staking","bisonrelay","wallet"],
		"spend":{"granted":false,"perTxDcr":0,"dailyDcr":0,"writeScopes":null}
	}`)
	if err := caps.CheckReadOnly(); err != nil {
		t.Fatalf("read-only token refused: %v", err)
	}
}

func TestCheckReadOnlyRefusesSpendGrant(t *testing.T) {
	caps := capsFrom(t, `{
		"agent":"trader",
		"domains":["wallet"],
		"spend":{"granted":true,"perTxDcr":2.5,"dailyDcr":10,"writeScopes":null}
	}`)
	err := caps.CheckReadOnly()
	if err == nil {
		t.Fatal("a token with a spend grant was accepted")
	}
	if !strings.Contains(err.Error(), "spend grant") {
		t.Fatalf("reason should name the spend grant, got: %v", err)
	}
}

func TestCheckReadOnlyRefusesWriteScopes(t *testing.T) {
	caps := capsFrom(t, `{
		"agent":"chatty",
		"domains":["bisonrelay"],
		"spend":{"granted":false,"writeScopes":["bisonrelay","staking"]}
	}`)
	err := caps.CheckReadOnly()
	if err == nil {
		t.Fatal("a token with write scopes was accepted")
	}
	// bisonrelay write is the one that matters here: it unlocks br_send_message
	// alongside the notification-clearing tools.
	if !strings.Contains(err.Error(), "bisonrelay") {
		t.Fatalf("reason should name the scopes, got: %v", err)
	}
}

func TestCheckReadOnlyRefusesBoth(t *testing.T) {
	caps := capsFrom(t, `{
		"spend":{"granted":true,"perTxDcr":1,"dailyDcr":1,"writeScopes":["dex.spend"]}
	}`)
	err := caps.CheckReadOnly()
	if err == nil {
		t.Fatal("a fully privileged token was accepted")
	}
	if !strings.Contains(err.Error(), "and") {
		t.Fatalf("both reasons should be reported, got: %v", err)
	}
}

func TestMissingDomains(t *testing.T) {
	caps := capsFrom(t, `{"domains":["node","wallet"]}`)
	got := caps.MissingDomains([]string{"node", "staking", "bisonrelay", "wallet"})
	if strings.Join(got, ",") != "staking,bisonrelay" {
		t.Fatalf("missing domains wrong: %v", got)
	}
}

// The chat ring is the only place unread can be derived from, so its shape is
// worth pinning: no id, no timestamp, no read flag.
func TestRingCountsSplitsGroupFromPrivate(t *testing.T) {
	msgs := []Message{
		{Type: "pm", FromNick: "someone"},
		{Type: "gc-message", FromNick: "brbot"},
		{Type: "gcm", FromNick: "brbot"},
		{Type: "pm", FromNick: "other"},
	}
	private, group := RingCounts(msgs)
	if private != 2 || group != 2 {
		t.Fatalf("want 2 private / 2 group, got %d / %d", private, group)
	}
}

func TestClassifyCodes(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrNoToken, "no-token"},
		{ErrUnauthorized, "auth"},
	}
	for _, tc := range cases {
		if got, _ := Classify(tc.err); got != tc.want {
			t.Errorf("Classify(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
