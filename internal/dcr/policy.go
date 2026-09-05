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

// cgnat is the only block a plain-http exception may name.
//
// The purpose is a WireGuard mesh, and NetBird addresses peers out of
// carrier-grade NAT space. RFC1918 is what hotels, cafes and hostile DHCP hand
// out, so trusting 192.168.1.0/24 would send the token to whoever holds that
// address on the next network joined; IPv6 ULA is what Docker and OpenWrt
// assign by default. CGNAT is the one block ordinary LAN DHCP never gives out,
// which is what makes it narrow enough to be worth allowing.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// maxNetworks is a sanity bound, not a security one. A mesh with more than
// thirty-two dcrpulse peers is a different design conversation.
const maxNetworks = 32

var (
	// ErrPlaintextRefused means an endpoint would have sent the bearer token in
	// clear text somewhere it is not allowed to go.
	ErrPlaintextRefused = errors.New("plain http refused")
	// ErrMeshDown means the exception is configured but its interface is not
	// there to carry the traffic.
	ErrMeshDown = errors.New("mesh interface unavailable")
	// ErrConfig means connections.json could not be read or does not validate.
	ErrConfig = errors.New("configuration error")
)

// PlainHTTP is the expert setting that relaxes the https rule.
//
// It is an object rather than a bare list of ranges because the interface and
// the ranges are one decision. An exception without an interface is a beacon:
// with the tunnel down, a permitted address is reached over whatever route is
// left, which is the public internet. Keeping them in one object makes it
// impossible to state one without the other.
type PlainHTTP struct {
	Interface string   `json:"interface"`
	Networks  []string `json:"networks"`
	Note      string   `json:"note,omitempty"`
}

// PlainHTTPPublic is what the panel is told about the exception: enough to
// state that one exists and name it, without the operator's note.
type PlainHTTPPublic struct {
	Interface string   `json:"interface"`
	Networks  []string `json:"networks"`
}

// Public renders the policy for the panel, or nil when none is in force.
func (p Policy) Public() *PlainHTTPPublic {
	if !p.Enabled() {
		return nil
	}
	return &PlainHTTPPublic{Interface: p.iface, Networks: p.Networks()}
}

// Policy is a parsed PlainHTTP. The zero value is the default rule, where only
// loopback may be reached over plain http.
type Policy struct {
	iface string
	nets  []netip.Prefix
}

// Enabled reports whether any exception is in force.
func (p Policy) Enabled() bool { return p.iface != "" && len(p.nets) > 0 }

// Interface is the netdevice exception traffic must leave by.
func (p Policy) Interface() string { return p.iface }

// Networks is the configured list, for display.
func (p Policy) Networks() []string {
	out := make([]string, 0, len(p.nets))
	for _, n := range p.nets {
		out = append(out, n.String())
	}
	return out
}

// Covers reports whether an address is one the operator listed.
func (p Policy) Covers(addr netip.Addr) bool {
	if !p.Enabled() || !addr.IsValid() {
		return false
	}
	a := addr.Unmap()
	for _, n := range p.nets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

// Equal reports whether two policies say the same thing, so a reload can tell
// whether anything actually moved.
func (p Policy) Equal(q Policy) bool {
	if p.iface != q.iface || len(p.nets) != len(q.nets) {
		return false
	}
	for i := range p.nets {
		if p.nets[i] != q.nets[i] {
			return false
		}
	}
	return true
}

// CheckURL decides whether a request may go out as written.
//
// This runs against the URL actually being sent, immediately before the
// Authorization header is attached, which is what makes it cover endpoints that
// were stored before the policy changed as well as ones typed just now.
func (p Policy) CheckURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("%w: no address", ErrPlaintextRefused)
	}
	ep, err := parseEndpoint(u.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPlaintextRefused, err)
	}
	if ep.scheme == "https" || ep.loopback() {
		return nil
	}
	if ep.addr.IsValid() && p.Covers(ep.addr) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPlaintextRefused, p.Refusal(ep.hostname()))
}

// Refusal is the sentence shown when plain http is not allowed to a host. The
// knob is named only for an address it could actually cover, so the advice is
// never an invitation to widen the rule to something it would refuse anyway.
func (p Policy) Refusal(host string) string {
	base := fmt.Sprintf("refusing plain http to %s: the bearer token would cross "+
		"the network in clear text. Use https, or reach it through an SSH tunnel "+
		"so the endpoint is localhost", host)

	addr, err := netip.ParseAddr(host)
	if err != nil || !cgnat.Contains(addr.Unmap()) {
		return base
	}
	if !p.Enabled() {
		return base + fmt.Sprintf(". Or, if %s is a WireGuard mesh peer, allow it "+
			"with: demarchy-setup allow-http %s/32 --via <mesh interface>", host, host)
	}
	return fmt.Sprintf("plain http is allowed via %s to %s only. Add this peer "+
		"with: demarchy-setup allow-http %s/32", p.iface, strings.Join(p.Networks(), ", "), host)
}

// ParsePolicy validates the expert setting. Every failure names the entry and
// the fix, because this file is hand-readable and a rejected one has to say
// what to type instead.
func ParsePolicy(in *PlainHTTP) (Policy, error) {
	if in == nil {
		return Policy{}, nil
	}

	iface := strings.TrimSpace(in.Interface)
	switch {
	case iface == "":
		return Policy{}, fmt.Errorf(`plainHttp.interface is empty: name the mesh ` +
			`netdevice, for example "wt0"`)
	case len(iface) > 15:
		return Policy{}, fmt.Errorf("plainHttp.interface %q: an interface name is at most 15 bytes", clip(iface))
	case iface == "." || iface == "..":
		return Policy{}, fmt.Errorf("plainHttp.interface %q: not an interface name", clip(iface))
	case strings.ContainsAny(iface, "/ \t\n"):
		return Policy{}, fmt.Errorf("plainHttp.interface %q: no slashes or whitespace in an interface name", clip(iface))
	}

	if len(in.Networks) == 0 {
		return Policy{}, fmt.Errorf("plainHttp.networks is empty: list the mesh " +
			"addresses plain http may reach, for example \"100.83.12.7/32\"")
	}
	if len(in.Networks) > maxNetworks {
		return Policy{}, fmt.Errorf("plainHttp.networks has %d entries, at most %d",
			len(in.Networks), maxNetworks)
	}

	seen := map[netip.Prefix]bool{}
	nets := make([]netip.Prefix, 0, len(in.Networks))
	for i, raw := range in.Networks {
		where := fmt.Sprintf("plainHttp.networks[%d] %q", i, clip(raw))

		if _, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil {
			return Policy{}, fmt.Errorf("%s: give the width too, write %s/32 for a single host",
				where, strings.TrimSpace(raw))
		}
		n, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return Policy{}, fmt.Errorf("%s: not an address range", where)
		}
		if !n.Addr().Is4() {
			return Policy{}, fmt.Errorf("%s: only IPv4 ranges, the mesh addresses this "+
				"allows are IPv4", where)
		}
		if n.Masked() != n {
			return Policy{}, fmt.Errorf("%s: host bits are set, did you mean %s?",
				where, n.Masked())
		}
		if n.Bits() < cgnat.Bits() || !cgnat.Contains(n.Addr()) {
			return Policy{}, fmt.Errorf("%s: not inside %s, the carrier-grade NAT range "+
				"that is the only space plain http may be allowed to", where, cgnat)
		}
		if seen[n] {
			return Policy{}, fmt.Errorf("%s: listed twice", where)
		}
		seen[n] = true
		nets = append(nets, n)
	}
	return Policy{iface: iface, nets: nets}, nil
}

// clip keeps a hostile config value from filling the panel or the journal.
func clip(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
