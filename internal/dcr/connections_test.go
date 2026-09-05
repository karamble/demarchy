// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"strings"
	"testing"
)

// The panel is told the connection list so it can draw the switcher. If a token
// ever rode along in that projection it would reach the shell process for every
// connection, not just the one being typed into: so this is the test that
// matters most in this file.
func TestPublicStripsTokens(t *testing.T) {
	list := &Connections{Conns: []Connection{
		{ID: "home", Name: "home", Endpoint: "http://127.0.0.1:8090", Token: "mcp_secret_home"},
		{ID: "vps", Name: "vps", Endpoint: "http://10.8.0.4:8090", Token: "mcp_secret_vps"},
	}}

	encoded, err := json.Marshal(list.Public())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(encoded)
	for _, secret := range []string{"mcp_secret_home", "mcp_secret_vps", "token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("public projection leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "home") || !strings.Contains(got, "10.8.0.4") {
		t.Fatalf("public projection dropped what the panel needs: %s", got)
	}
}

func TestSlugDeduplicates(t *testing.T) {
	var list Connections
	for _, name := range []string{"My Node", "My Node", "my node"} {
		if _, err := list.Add(name, "127.0.0.1:8090", "mcp_x"); err != nil {
			t.Fatalf("add %q: %v", name, err)
		}
	}
	seen := map[string]bool{}
	for _, c := range list.Conns {
		if seen[c.ID] {
			t.Fatalf("duplicate id %q in %+v", c.ID, list.Conns)
		}
		seen[c.ID] = true
	}
	if list.Conns[0].ID != "my-node" {
		t.Fatalf("first id should slug cleanly, got %q", list.Conns[0].ID)
	}
}

func TestNormaliseEndpoint(t *testing.T) {
	ok := map[string]string{
		// Loopback keeps plain http: there is no wire to sniff, and dcrpulse
		// publishes its MCP port without TLS.
		"127.0.0.1:8090":         "http://127.0.0.1:8090",
		"http://127.0.0.1:8090/": "http://127.0.0.1:8090",
		"localhost:8090":         "http://localhost:8090",
		"[::1]:8090":             "http://[::1]:8090",
		// A bare remote host is promoted to TLS rather than to clear text.
		"  10.8.0.4:8090  ":     "https://10.8.0.4:8090",
		"pulse.example":         "https://pulse.example",
		"https://pulse.example": "https://pulse.example",
	}
	for in, want := range ok {
		got, err := NormaliseEndpoint(in)
		if err != nil {
			t.Errorf("NormaliseEndpoint(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "ftp://host:1", "http://"} {
		if got, err := NormaliseEndpoint(bad); err == nil {
			t.Errorf("NormaliseEndpoint(%q) should fail, got %q", bad, got)
		}
	}
}

// The token travels in an Authorization header on every call, so plain http to
// anywhere but this machine would put a wallet-reading credential on the wire.
func TestNormaliseEndpointRefusesPlainHTTPOffBox(t *testing.T) {
	for _, remote := range []string{
		"http://10.8.0.4:8090",
		"http://pulse.example",
		"http://192.168.1.20:8090",
	} {
		got, err := NormaliseEndpoint(remote)
		if err == nil {
			t.Errorf("NormaliseEndpoint(%q) should refuse plain http, got %q", remote, got)
			continue
		}
		if !strings.Contains(err.Error(), "clear text") {
			t.Errorf("NormaliseEndpoint(%q) should say why, got: %v", remote, err)
		}
	}
	// ...and explicit http to loopback is still fine.
	for _, local := range []string{"http://127.0.0.1:8090", "http://localhost:8090"} {
		if _, err := NormaliseEndpoint(local); err != nil {
			t.Errorf("NormaliseEndpoint(%q) should be allowed: %v", local, err)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	yes := []string{"127.0.0.1:8090", "localhost", "localhost:8090", "[::1]:8090", "127.5.5.5"}
	no := []string{"10.8.0.4:8090", "pulse.example", "192.168.1.20", "example.localhost.evil.com"}
	for _, h := range yes {
		if !IsLoopback(h) {
			t.Errorf("IsLoopback(%q) = false, want true", h)
		}
	}
	for _, h := range no {
		if IsLoopback(h) {
			t.Errorf("IsLoopback(%q) = true, want false", h)
		}
	}
}

// An empty token on edit means "keep the stored one", so renaming a connection
// does not force the secret to be typed again.
func TestEditKeepsTokenWhenBlank(t *testing.T) {
	var list Connections
	if _, err := list.Add("home", "127.0.0.1:8090", "mcp_original"); err != nil {
		t.Fatal(err)
	}
	if _, err := list.Edit("home", "renamed", "127.0.0.1:9999", ""); err != nil {
		t.Fatal(err)
	}
	c, _ := list.Find("home")
	if c.Token != "mcp_original" {
		t.Fatalf("blank token should keep the stored one, got %q", c.Token)
	}
	if c.Name != "renamed" || c.Endpoint != "http://127.0.0.1:9999" {
		t.Fatalf("edit did not apply: %+v", c)
	}
}

func TestEditReplacesTokenWhenGiven(t *testing.T) {
	var list Connections
	if _, err := list.Add("home", "127.0.0.1:8090", "mcp_original"); err != nil {
		t.Fatal(err)
	}
	if _, err := list.Edit("home", "", "", "mcp_new"); err != nil {
		t.Fatal(err)
	}
	c, _ := list.Find("home")
	if c.Token != "mcp_new" {
		t.Fatalf("token not replaced, got %q", c.Token)
	}
}

// A stale activeConnection setting must degrade to something that works rather
// than to a blank panel.
func TestActiveFallsBackToFirst(t *testing.T) {
	var list Connections
	list.Add("home", "127.0.0.1:8090", "mcp_a")
	list.Add("vps", "https://10.8.0.4:8090", "mcp_b")

	c, err := list.Active("gone")
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if c.ID != "home" {
		t.Fatalf("unknown id should fall back to the first, got %q", c.ID)
	}
	if c, _ = list.Active("vps"); c.ID != "vps" {
		t.Fatalf("known id should be honoured, got %q", c.ID)
	}
	if _, err := (&Connections{}).Active(""); err != ErrNoConnections {
		t.Fatalf("empty list should report ErrNoConnections, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	var list Connections
	list.Add("home", "127.0.0.1:8090", "mcp_a")
	list.Add("vps", "https://10.8.0.4:8090", "mcp_b")

	if err := list.Remove("home"); err != nil {
		t.Fatal(err)
	}
	if _, ok := list.Find("home"); ok {
		t.Fatal("removed connection still present")
	}
	if len(list.Conns) != 1 || list.Conns[0].ID != "vps" {
		t.Fatalf("wrong survivor: %+v", list.Conns)
	}
	if err := list.Remove("nope"); err != ErrConnNotFound {
		t.Fatalf("removing an unknown id should report ErrConnNotFound, got %v", err)
	}
}

func TestAddRejectsIncomplete(t *testing.T) {
	var list Connections
	cases := []struct{ name, endpoint, token string }{
		{"", "127.0.0.1:8090", "mcp_x"},
		{"home", "", "mcp_x"},
		{"home", "127.0.0.1:8090", ""},
		{"home", "ftp://x", "mcp_x"},
	}
	for _, c := range cases {
		if _, err := list.Add(c.name, c.endpoint, c.token); err == nil {
			t.Errorf("Add(%q,%q,%q) should fail", c.name, c.endpoint, c.token)
		}
	}
	if len(list.Conns) != 0 {
		t.Fatalf("rejected adds must not be appended: %+v", list.Conns)
	}
}
