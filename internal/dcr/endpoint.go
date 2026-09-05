// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// endpoint is a dcrpulse address that has been through one parser.
//
// It exists because the host used to be sliced out by counting colons, which is
// not what a URL means. In "http://127.0.0.1:1@evil.example" the loopback
// address is the userinfo and evil.example is the host, so the string that was
// checked and the host that would be dialled were different strings, and a
// bearer token went to the second one. Every decision about an endpoint now
// comes from these fields, and the stored form is rebuilt from them rather than
// carried through as it was typed.
type endpoint struct {
	scheme string
	host   string // hostname, no brackets and no port
	port   string
	path   string
	addr   netip.Addr // valid only when host is an IP literal
	// explicit records that a scheme was typed. A bare host is promoted to
	// https, so the difference decides whether plain text was actually asked
	// for or merely defaulted into.
	explicit bool
}

// parseEndpoint reads an endpoint as written, with or without a scheme.
//
// It is deliberately strict. Anything a dcrpulse endpoint has no legitimate use
// for is refused rather than normalised away, because every forgiving rule here
// is a way for the checked host and the dialled host to differ.
func parseEndpoint(raw string) (endpoint, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return endpoint{}, errors.New("endpoint is empty")
	}

	explicit := strings.Contains(s, "://")
	if !explicit {
		s = "http://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return endpoint{}, fmt.Errorf("endpoint is not a URL: %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return endpoint{}, fmt.Errorf("endpoint must be http or https, got %q", raw)
	}
	if u.Opaque != "" {
		return endpoint{}, fmt.Errorf("endpoint has no host: %q", raw)
	}
	// The whole reason this parser exists.
	if u.User != nil {
		return endpoint{}, fmt.Errorf(
			"endpoint must not carry a user before the @: in %q the host is %q, "+
				"not what comes first", raw, u.Hostname())
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return endpoint{}, fmt.Errorf("endpoint must not carry a query or fragment: %q", raw)
	}

	host := u.Hostname()
	if host == "" {
		return endpoint{}, fmt.Errorf("endpoint has no host: %q", raw)
	}
	// url.Parse strips the brackets, so an IPv6 literal that arrived without
	// them has already been read as a host and a port by the time we see it.
	if strings.Contains(host, ":") && !strings.HasPrefix(u.Host, "[") {
		return endpoint{}, fmt.Errorf(
			"an IPv6 address needs brackets: write [%s]:port", host)
	}
	if strings.Contains(host, "%") {
		return endpoint{}, fmt.Errorf("endpoint must not carry a zone identifier: %q", raw)
	}

	ep := endpoint{
		scheme:   u.Scheme,
		host:     strings.ToLower(host),
		port:     u.Port(),
		path:     strings.TrimRight(u.Path, "/"),
		explicit: explicit,
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return endpoint{}, fmt.Errorf("endpoint must not carry a zone identifier: %q", raw)
		}
		// One spelling per address: [::ffff:100.83.12.7] and 100.83.12.7 are
		// the same host and must not be able to answer a check differently.
		ep.addr = addr.Unmap()
	}

	// A rebuilt form that does not read back as the same host and port means
	// something in the original was being interpreted, and this refuses rather
	// than guessing which reading was meant.
	back, err := url.Parse(ep.String())
	if err != nil || back.Hostname() != ep.hostname() || back.Port() != ep.port {
		return endpoint{}, fmt.Errorf("endpoint could not be read back unchanged: %q", raw)
	}
	return ep, nil
}

// hostname is the host as it will be dialled, without brackets.
func (e endpoint) hostname() string {
	if e.addr.IsValid() {
		return e.addr.String()
	}
	return e.host
}

// String is the canonical form, which is what gets stored.
func (e endpoint) String() string {
	h := e.hostname()
	if e.addr.IsValid() && e.addr.Is6() {
		h = "[" + h + "]"
	}
	if e.port != "" {
		h += ":" + e.port
	}
	return e.scheme + "://" + h + e.path
}

// loopback reports whether the endpoint names this machine.
func (e endpoint) loopback() bool {
	if e.addr.IsValid() {
		return e.addr.IsLoopback()
	}
	return e.host == "localhost" || strings.HasSuffix(e.host, ".localhost")
}

// IsLoopback reports whether a host[:port] names this machine. Loopback and a
// listed mesh address are the only places a token may travel in clear text, so
// this is half of what decides that.
func IsLoopback(hostPath string) bool {
	ep, err := parseEndpoint("http://" + hostPath)
	if err != nil {
		return false
	}
	return ep.loopback()
}
