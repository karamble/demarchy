// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	Version int          `json:"version"`
	Conns   []Connection `json:"connections"`
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
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", ConnectionsPath(), err)
	}
	if list.Version != connSchema {
		return nil, fmt.Errorf("%s: unknown version %d", ConnectionsPath(), list.Version)
	}
	return &list, nil
}

// SaveConnections writes the list atomically, 0600 inside a 0700 directory.
func SaveConnections(list *Connections) error {
	list.Version = connSchema
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

// NormaliseEndpoint accepts what a person would actually type,
// "10.8.0.4:8090", "localhost:8090", a bare host, and returns a URL.
//
// **Plain HTTP is refused for anything but loopback.** The bearer token rides in
// an Authorization header on every call, so http:// to a remote host puts a
// credential that can read your wallet on the wire in clear text. A bare remote
// host is therefore promoted to https rather than http; a bare loopback host
// stays http, because there is no wire to sniff and dcrpulse publishes its MCP
// port without TLS.
func NormaliseEndpoint(in string) (string, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", errors.New("endpoint is empty")
	}

	explicit := strings.Contains(s, "://")
	if !explicit {
		// Decide the scheme from the host, once we can see it.
		s = "http://" + s
	}
	scheme := ""
	switch {
	case strings.HasPrefix(s, "http://"):
		scheme = "http://"
	case strings.HasPrefix(s, "https://"):
		scheme = "https://"
	default:
		return "", fmt.Errorf("endpoint must be http or https, got %q", in)
	}
	// The scheme is settled before any trimming. Stripping trailing slashes
	// first turns a bare "http://" into "http:", which then looks like it has
	// no scheme and gets another one prepended.
	hostPath := strings.TrimRight(strings.TrimPrefix(s, scheme), "/")
	if hostPath == "" {
		return "", fmt.Errorf("endpoint has no host: %q", in)
	}

	local := IsLoopback(hostPath)
	switch {
	case local:
		// Loopback keeps whatever was asked for; http is the normal case.
	case !explicit:
		// A bare remote host defaults to TLS rather than to plain text.
		scheme = "https://"
	case scheme == "http://":
		return "", fmt.Errorf(
			"refusing plain http to %s: the bearer token would cross the network "+
				"in clear text. Use https, or reach it through an SSH tunnel or VPN "+
				"so the endpoint is localhost", hostOf(hostPath))
	}
	return scheme + hostPath, nil
}

// hostOf drops the port and any path, for error messages.
func hostOf(hostPath string) string {
	h := hostPath
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	return h
}

// IsLoopback reports whether a host[:port] names this machine. Only loopback
// may be reached over plain http, so this decides whether a token is allowed
// to travel unencrypted.
func IsLoopback(hostPath string) bool {
	h := hostOf(hostPath)
	// Strip the port, taking care with bracketed IPv6 literals.
	if strings.HasPrefix(h, "[") {
		if i := strings.Index(h, "]"); i >= 0 {
			h = h[1:i]
		}
	} else if i := strings.LastIndexByte(h, ':'); i >= 0 && strings.Count(h, ":") == 1 {
		h = h[:i]
	}
	h = strings.ToLower(strings.TrimSpace(h))

	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// Add appends a connection. The caller validates the token first; this only
// owns naming and shape.
func (l *Connections) Add(name, endpoint, token string) (*Connection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is empty")
	}
	ep, err := NormaliseEndpoint(endpoint)
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
		ep, err := NormaliseEndpoint(s)
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
