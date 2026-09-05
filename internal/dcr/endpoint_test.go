// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"strings"
	"testing"
)

// The host is whatever a URL says it is, not whatever comes before the first
// colon. Every string here was accepted before this parser existed, and the
// first three were stored as plain http and then dialled at a host that is not
// the one the check looked at.
func TestEndpointRefusesASplitHost(t *testing.T) {
	smuggled := []string{
		"http://127.0.0.1:1@evil.example",
		"http://127.0.0.1:8090@evil.example",
		"localhost:1@evil.example",
		"http://user:pw@127.0.0.1:8090",
		"http://100.83.12.7:8090@evil.example",
		"http://100.83.12.7:@evil.example",
		"http://100.83.12.7:8090@203.0.113.5:8090",
		"http://[fd7a:115c::1]:8090@evil.example",
		"http://[::ffff:127.0.0.1]@evil.example",
	}
	for _, in := range smuggled {
		if got, err := NormaliseEndpoint(in); err == nil {
			t.Errorf("NormaliseEndpoint(%q) = %q, want a refusal: the host is not "+
				"what comes before the @", in, got)
		}
		if IsLoopback(strings.TrimPrefix(in, "http://")) {
			t.Errorf("IsLoopback(%q) = true: a userinfo section is not a host", in)
		}
	}
}

// Anything a dcrpulse endpoint has no use for is refused rather than
// normalised, because each forgiving rule is another way for the checked host
// and the dialled host to differ.
func TestEndpointRefusesMalformed(t *testing.T) {
	bad := map[string]string{
		"http://fd7a:115c::1:8090":     "unbracketed IPv6",
		"http://fd00::1:8090":          "unbracketed IPv6",
		"http://[fd7a:115c::1]junk:80": "trailing junk after the brackets",
		"http://127.0.0.1:8090x":       "a port that is not a number",
		"http://127.0.0.1:8090?a=b":    "a query",
		"http://127.0.0.1:8090#frag":   "a fragment",
		"http://[fe80::1%25eth0]:8090": "a zone identifier",
		"http:127.0.0.1:8090":          "an opaque URL with no host",
		"ftp://127.0.0.1:8090":         "a scheme that is not http or https",
		"":                             "an empty string",
		"   ":                          "whitespace only",
	}
	for in, why := range bad {
		if got, err := NormaliseEndpoint(in); err == nil {
			t.Errorf("NormaliseEndpoint(%q) = %q, want a refusal: %s", in, got, why)
		}
	}
}

// One address has one spelling, so a check cannot be answered differently by
// two forms of the same host.
func TestEndpointCanonicalises(t *testing.T) {
	same := map[string]string{
		"https://[::ffff:203.0.113.5]:8090": "https://203.0.113.5:8090",
		"HTTPS://Pulse.Example":             "https://pulse.example",
		"https://pulse.example/":            "https://pulse.example",
		"http://[::1]:8090":                 "http://[::1]:8090",
	}
	for in, want := range same {
		got, err := NormaliseEndpoint(in)
		if err != nil {
			t.Errorf("NormaliseEndpoint(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}
