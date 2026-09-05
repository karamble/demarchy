// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package dcr talks to a local dcrpulse over its MCP port. It is a plain
// JSON-RPC client: MCP over streamable HTTP needs no SDK and no LLM, and this
// package deliberately depends on nothing outside the standard library.
package dcr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// DefaultEndpoint is dcrpulse's MCP listener as the compose file publishes it.
const DefaultEndpoint = "http://127.0.0.1:8090"

// ModuleID is the Omarchy plugin id, used to find our entry in shell.json.
const ModuleID = "karamble.demarchy"

// ConfigDir is where the setup wizard keeps the token. It is deliberately not
// the plugin directory: plugin dirs are synced from a git checkout.
func ConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "demarchy")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "demarchy")
}

// TokenPath is the 0600 file holding the MCP bearer token.
func TokenPath() string { return filepath.Join(ConfigDir(), "token") }

// ErrNoToken means the wizard has not been run.
var ErrNoToken = errors.New("no token configured")

// ReadToken reads the bearer token descriptor-first, refusing anything that is
// not a private regular file. The token is never passed through argv or the
// environment: only the path is, and a path is not a secret.
func ReadToken(path string) (string, error) {
	if path == "" {
		path = TokenPath()
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoToken
		}
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is group- or world-readable (%04o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	// A token is a short opaque string; the cap stops a wrong path (a log, a
	// core file) from being read into memory wholesale.
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", ErrNoToken
	}
	return tok, nil
}

// ShellSetting reads one of our inline settings out of Omarchy's shell.json.
//
// The helper re-reads the monitoring switch here, independently of QML, before
// it opens the token file. A stale keybind, a hand-run copy of this binary or a
// QML regression therefore still makes no request. Checking in two places is
// deliberate: the switch is the one promise worth making twice.
func ShellSetting(key string) (any, bool) {
	path := filepath.Join(os.Getenv("HOME"), ".config", "omarchy", "shell.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc struct {
		Bar struct {
			Layout map[string][]map[string]any `json:"layout"`
		} `json:"bar"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, false
	}
	for _, section := range doc.Bar.Layout {
		for _, entry := range section {
			if id, _ := entry["id"].(string); id != ModuleID {
				continue
			}
			v, ok := entry[key]
			return v, ok
		}
	}
	return nil, false
}

// BoolSetting resolves a shell.json boolean, falling back when absent. It
// accepts the string forms too, because shell.json is hand-editable.
func BoolSetting(key string, fallback bool) bool {
	v, ok := ShellSetting(key)
	if !ok {
		return fallback
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

// StringSetting resolves a shell.json string, falling back when absent or empty.
func StringSetting(key, fallback string) string {
	v, ok := ShellSetting(key)
	if !ok {
		return fallback
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fallback
}
