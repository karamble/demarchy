// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestParsePolicyAccepts(t *testing.T) {
	p, err := ParsePolicy(&PlainHTTP{
		Interface: "wt0",
		Networks:  []string{"100.83.12.7/32", "100.83.40.0/24"},
	})
	if err != nil {
		t.Fatalf("a mesh policy should parse: %v", err)
	}
	if !p.Enabled() || p.Interface() != "wt0" {
		t.Fatalf("policy did not take: %+v", p.Networks())
	}
}

// The zero policy is the rule everyone who never opts in keeps.
func TestParsePolicyNilIsTheDefaultRule(t *testing.T) {
	p, err := ParsePolicy(nil)
	if err != nil || p.Enabled() {
		t.Fatalf("no setting must mean no exception, got %+v %v", p, err)
	}
}

// Every one of these is a way to end up trusting more than a mesh, so each is
// refused with a message that says what to write instead.
func TestParsePolicyRefuses(t *testing.T) {
	cases := map[string]*PlainHTTP{
		"no interface":         {Networks: []string{"100.83.12.7/32"}},
		"no networks":          {Interface: "wt0"},
		"interface with slash": {Interface: "../etc", Networks: []string{"100.83.12.7/32"}},
		"interface too long":   {Interface: strings.Repeat("x", 16), Networks: []string{"100.83.12.7/32"}},
		"the whole internet":   {Interface: "wt0", Networks: []string{"0.0.0.0/0"}},
		"public space":         {Interface: "wt0", Networks: []string{"95.216.110.0/24"}},
		"rfc1918":              {Interface: "wt0", Networks: []string{"192.168.1.0/24"}},
		"loopback":             {Interface: "wt0", Networks: []string{"127.0.0.0/8"}},
		"host bits set":        {Interface: "wt0", Networks: []string{"100.83.89.245/10"}},
		"no width given":       {Interface: "wt0", Networks: []string{"100.83.12.7"}},
		"ipv6 ula":             {Interface: "wt0", Networks: []string{"fd00::/8"}},
		"ipv4 mapped":          {Interface: "wt0", Networks: []string{"::ffff:100.64.0.0/106"}},
		"not a range":          {Interface: "wt0", Networks: []string{"wat"}},
		"listed twice":         {Interface: "wt0", Networks: []string{"100.83.12.7/32", "100.83.12.7/32"}},
	}
	for why, in := range cases {
		if p, err := ParsePolicy(in); err == nil {
			t.Errorf("%s: should be refused, got %v via %s", why, p.Networks(), p.Interface())
		}
	}
}

// A range wider than the mesh block cannot be smuggled in by giving a short
// prefix that happens to contain it.
func TestParsePolicyRefusesAWideningPrefix(t *testing.T) {
	if _, err := ParsePolicy(&PlainHTTP{Interface: "wt0", Networks: []string{"100.0.0.0/8"}}); err == nil {
		t.Fatal("100.0.0.0/8 contains public space and must be refused")
	}
}

func TestCheckURL(t *testing.T) {
	p, err := ParsePolicy(&PlainHTTP{Interface: "wt0", Networks: []string{"100.83.12.7/32"}})
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{
		"https://pulse.example/mcp",
		"http://127.0.0.1:8090/mcp",
		"http://localhost:8090/mcp",
		"http://100.83.12.7:8090/mcp",
	}
	refused := []string{
		"http://100.83.12.8:8090/mcp", // one address along, not listed
		"http://10.8.0.4:8090/mcp",    // not the mesh
		"http://pulse.example/mcp",    // a name never qualifies
		"http://acekool.netbird.selfhosted:8090/mcp",
	}
	for _, raw := range allowed {
		u, _ := url.Parse(raw)
		if err := p.CheckURL(u); err != nil {
			t.Errorf("CheckURL(%q) = %v, want allowed", raw, err)
		}
	}
	for _, raw := range refused {
		u, _ := url.Parse(raw)
		err := p.CheckURL(u)
		if err == nil {
			t.Errorf("CheckURL(%q) = nil, want refused", raw)
			continue
		}
		if !errors.Is(err, ErrPlaintextRefused) {
			t.Errorf("CheckURL(%q) should be an ErrPlaintextRefused, got %v", raw, err)
		}
	}
}

// The knob is only ever named for an address it could actually cover, so the
// advice never invites a change that would be refused anyway.
func TestRefusalOnlyAdvertisesTheKnobWhereItApplies(t *testing.T) {
	var none Policy
	if msg := none.Refusal("100.83.12.7"); !strings.Contains(msg, "allow-http") {
		t.Errorf("a mesh address with no policy should name the knob, got: %s", msg)
	}
	if msg := none.Refusal("pulse.example"); strings.Contains(msg, "allow-http") {
		t.Errorf("a hostname can never be covered, so must not name the knob: %s", msg)
	}
	if msg := none.Refusal("10.8.0.4"); strings.Contains(msg, "allow-http") {
		t.Errorf("a non-CGNAT address can never be covered: %s", msg)
	}
}

// A policy allows plain http to be typed for a listed peer, and still promotes
// a bare host: the exception is asked for, not fallen into.
func TestNormaliseEndpointWithPolicy(t *testing.T) {
	p, err := ParsePolicy(&PlainHTTP{Interface: "wt0", Networks: []string{"100.83.12.7/32"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NormaliseEndpointWith("http://100.83.12.7:8090", p)
	if err != nil || got != "http://100.83.12.7:8090" {
		t.Fatalf("a listed peer should be allowed over http, got %q %v", got, err)
	}
	got, err = NormaliseEndpointWith("100.83.12.7:8090", p)
	if err != nil || got != "https://100.83.12.7:8090" {
		t.Fatalf("a bare host must still be promoted to https, got %q %v", got, err)
	}
	if _, err := NormaliseEndpointWith("http://100.83.12.8:8090", p); err == nil {
		t.Fatal("an unlisted mesh address must still be refused")
	}
}
