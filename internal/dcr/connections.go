// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// ConnectionsFile is the on-disk name of the connection list.
//
// These are called connections, not nodes, throughout. "Node" already means the
// Decred daemon everywhere else in this codebase and in the panel's own "Node
// Status" section; a dcrpulse instance is a different thing and needs a
// different word.
const ConnectionsFile = "connections.json"

// connSchema is bumped only if the file shape changes incompatibly.
const connSchema = 1

// connSchemaPlainHTTP is the version a file carrying a plain-http exception is
// written as. An older binary refuses it by version rather than reading around
// the field it does not know and dropping it on the next save.
const connSchemaPlainHTTP = 2

// Connection is one dcrpulse this widget can talk to. Token is present on disk and in
// memory in the helper; it is never emitted in a snapshot and never read by
// QML: see PublicNode.
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

// PublicConnection is a connection with the secret removed: what the panel is told, so it
// can render the switcher without the token ever reaching the shell process.
type PublicConnection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
}

// Connections is the whole file.
type Connections struct {
	Version int `json:"version"`
	// PlainHTTP is the expert exception to the https rule, absent unless the
	// operator has set one. Its presence is what makes the file version 2, so
	// a file nobody opted into marshals exactly as it did before this existed.
	PlainHTTP *PlainHTTP   `json:"plainHttp,omitempty"`
	Conns     []Connection `json:"connections"`
}

// Policy returns the parsed exception. The file is validated at load, so this
// cannot fail in practice; a failure yields the default rule, where only
// loopback may be reached over plain http.
func (l *Connections) Policy() Policy {
	if l == nil {
		return Policy{}
	}
	p, err := ParsePolicy(l.PlainHTTP)
	if err != nil {
		return Policy{}
	}
	return p
}

// SetPlainHTTP validates and installs the exception, or clears it when nil.
func (l *Connections) SetPlainHTTP(in *PlainHTTP) error {
	if in == nil {
		l.PlainHTTP = nil
		return nil
	}
	if _, err := ParsePolicy(in); err != nil {
		return err
	}
	l.PlainHTTP = in
	return nil
}

var (
	// ErrNoConnections means nothing is configured yet.
	ErrNoConnections = errors.New("no dcrpulse connections configured")
	// ErrConnNotFound is returned for an unknown id.
	ErrConnNotFound = errors.New("no such connection")
)

// ConnectionsPath is the 0600 file holding every connection and its token.
func ConnectionsPath() string { return filepath.Join(ConfigDir(), ConnectionsFile) }

// LoadConnections reads the connection list, migrating a single legacy token file
// into it the first time.
func LoadConnections() (*Connections, error) {
	list, err := readConnections()
	switch {
	case err == nil:
		return list, nil
	case !os.IsNotExist(err):
		return nil, err
	}

	// No list yet. A previous single-connection install has a bare token file
	// and an endpoint in shell.json; carry both over rather than making the
	// user re-enter a credential that already works.
	token, terr := ReadToken("")
	if terr != nil {
		return &Connections{Version: connSchema}, nil
	}
	migrated := &Connections{
		Version: connSchema,
		Conns: []Connection{{
			ID:       "default",
			Name:     "default",
			Endpoint: StringSetting("endpoint", DefaultEndpoint),
			Token:    token,
		}},
	}
	if err := SaveConnections(migrated); err != nil {
		return nil, err
	}
	// The old token file is deliberately left in place: silently deleting a
	// working credential during an upgrade is not a thing to do quietly.
	return migrated, nil
}

func readConnections() (*Connections, error) {
	f, err := os.OpenFile(ConnectionsPath(), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is group- or world-readable (%04o); run: chmod 600 %s",
			ConnectionsPath(), info.Mode().Perm(), ConnectionsPath())
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	var list Connections
	// Unknown fields are refused rather than dropped: this file is hand-edited
	// by whoever sets the exception, and a silently ignored typo would read as
	// a setting that took effect.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, ConnectionsPath(), err)
	}
	if list.Version != connSchema && list.Version != connSchemaPlainHTTP {
		return nil, fmt.Errorf("%w: %s: unknown version %d", ErrConfig, ConnectionsPath(), list.Version)
	}
	if _, err := ParsePolicy(list.PlainHTTP); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrConfig, ConnectionsPath(), err)
	}
	return &list, nil
}

// SaveConnections writes the list atomically, 0600 inside a 0700 directory.
//
// The version tracks the exception rather than the code: a file without one
// stays version 1 and is byte-identical to what an older build wrote, and a
// file with one is version 2, which an older build refuses loudly instead of
// unmarshalling around the field and erasing it on its next save.
func SaveConnections(list *Connections) error {
	if _, err := ParsePolicy(list.PlainHTTP); err != nil {
		return fmt.Errorf("%w: %v", ErrConfig, err)
	}
	list.Version = connSchema
	if list.PlainHTTP != nil {
		list.Version = connSchemaPlainHTTP
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(ConnectionsPath(), append(b, '\n'))
}

// WritePrivate writes a secret-bearing file atomically: private directory,
// 0600 before any content lands, fsync, then rename. Shared by the helper and
// the setup tool so there is one implementation of this to get right.
func WritePrivate(path string, content []byte) error {
	dir := filepath.Dir(path)
	if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to write a secret through it", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Public strips the tokens, giving the form the panel is allowed to see.
func (l *Connections) Public() []PublicConnection {
	out := make([]PublicConnection, 0, len(l.Conns))
	for _, n := range l.Conns {
		out = append(out, PublicConnection{ID: n.ID, Name: n.Name, Endpoint: n.Endpoint})
	}
	return out
}

// Find returns a node by id.
func (l *Connections) Find(id string) (*Connection, bool) {
	for i := range l.Conns {
		if l.Conns[i].ID == id {
			return &l.Conns[i], true
		}
	}
	return nil, false
}

// Active resolves which node to use: the requested id when it exists, else the
// first configured one, so a stale activeNode setting degrades to something
// working rather than to nothing.
func (l *Connections) Active(id string) (*Connection, error) {
	if len(l.Conns) == 0 {
		return nil, ErrNoConnections
	}
	if n, ok := l.Find(id); ok {
		return n, nil
	}
	return &l.Conns[0], nil
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a display name into an id: lowercase, dashes, deduplicated
// against what is already there.
func (l *Connections) slug(name string) string {
	base := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "node"
	}
	id, n := base, 2
	for {
		if _, taken := l.Find(id); !taken {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
		n++
	}
}

// NormaliseEndpoint settles the scheme and returns the canonical form.
//
// Loopback may be reached over plain http, because nothing leaves the machine.
// A bare remote host is promoted to https rather than defaulting into clear
// text, and an explicit plain-http remote is refused: the bearer token rides on
// every request.
func NormaliseEndpoint(in string) (string, error) {
	return NormaliseEndpointWith(in, Policy{})
}

// NormaliseEndpointWith is NormaliseEndpoint against a configured exception.
//
// A bare host is still promoted to https even when the policy would cover it:
// the exception is something to ask for, not to fall into, so plain text has to
// be typed.
func NormaliseEndpointWith(in string, p Policy) (string, error) {
	ep, err := parseEndpoint(in)
	if err != nil {
		return "", err
	}
	switch {
	case ep.loopback():
		// Loopback keeps whatever was asked for; http is the normal case.
	case !ep.explicit:
		// A bare remote host defaults to TLS rather than to plain text.
		ep.scheme = "https"
	case ep.scheme == "http" && ep.addr.IsValid() && p.Covers(ep.addr):
		// A listed mesh address, reached over a bound interface.
	case ep.scheme == "http" && !ep.addr.IsValid() && p.CoversName(ep.hostname()):
		// A mesh name. Whether it actually points inside the policy is settled
		// when it is dialled, because that is when the answer is true. Adding a
		// connection dials it, so a name pointing elsewhere is refused here too,
		// with the reason, rather than being stored and failing later.
	case ep.scheme == "http":
		return "", fmt.Errorf("%w: %s", ErrPlaintextRefused, p.Refusal(ep.hostname()))
	}
	return ep.String(), nil
}

// Add appends a connection. The caller validates the token first; this only
// owns naming and shape.
func (l *Connections) Add(name, endpoint, token string) (*Connection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is empty")
	}
	ep, err := NormaliseEndpointWith(endpoint, l.Policy())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("token is empty")
	}
	n := Connection{ID: l.slug(name), Name: name, Endpoint: ep, Token: strings.TrimSpace(token)}
	l.Conns = append(l.Conns, n)
	return &l.Conns[len(l.Conns)-1], nil
}

// Edit updates a connection. An empty token keeps the stored one, so renaming
// does not mean re-entering the secret.
func (l *Connections) Edit(id, name, endpoint, token string) (*Connection, error) {
	n, ok := l.Find(id)
	if !ok {
		return nil, ErrConnNotFound
	}
	if s := strings.TrimSpace(name); s != "" {
		n.Name = s
	}
	if s := strings.TrimSpace(endpoint); s != "" {
		ep, err := NormaliseEndpointWith(s, l.Policy())
		if err != nil {
			return nil, err
		}
		n.Endpoint = ep
	}
	if s := strings.TrimSpace(token); s != "" {
		n.Token = s
	}
	return n, nil
}

// Remove deletes a connection.
func (l *Connections) Remove(id string) error {
	for i := range l.Conns {
		if l.Conns[i].ID == id {
			l.Conns = append(l.Conns[:i], l.Conns[i+1:]...)
			return nil
		}
	}
	return ErrConnNotFound
}
