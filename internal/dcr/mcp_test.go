// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// The credential must not be attached to a request the policy would refuse,
// and refusing must cost no I/O at all: the check runs before the header and
// before the dial.
func TestNoTokenOnARefusedRequest(t *testing.T) {
	var sawAuth, sawAny int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sawAny, 1)
		if r.Header.Get("Authorization") != "" {
			atomic.AddInt32(&sawAuth, 1)
		}
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	// A mesh address with no policy: refused, and the listener never hears it.
	c := NewClientWithPolicy("http://100.83.12.7:8090", "mcp_secret", nil)
	if _, err := c.Call(context.Background(), "node_status", nil); err == nil {
		t.Fatal("a plain-http call to an unlisted address should be refused")
	} else if !errors.Is(err, ErrPlaintextRefused) {
		t.Fatalf("want ErrPlaintextRefused, got %v", err)
	}
	if atomic.LoadInt32(&sawAny) != 0 || atomic.LoadInt32(&sawAuth) != 0 {
		t.Fatal("a refused request must not reach the network")
	}
}

// dcrpulse never redirects, so a 3xx can only be an attempt to move the token.
// The stdlib keeps the Authorization header across a same-host https to http
// downgrade, which is exactly the hop worth refusing.
func TestRedirectsAreNotFollowed(t *testing.T) {
	var landed int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&landed, 1)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/mcp", http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "mcp_secret")
	if _, err := c.Call(context.Background(), "node_status", nil); err == nil {
		t.Fatal("a redirect should surface as a failure, not be chased")
	}
	if atomic.LoadInt32(&landed) != 0 {
		t.Fatal("the redirect was followed; the token went to the second host")
	}
}
