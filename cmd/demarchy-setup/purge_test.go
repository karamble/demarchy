// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/karamble/demarchy/internal/dcr"
)

// Purge is the documented first step of removing the plugin, so it has to take
// this plugin's own files with it and nothing of anyone else's. It no longer
// has links to chase: the alerts guide is printed on request, never installed,
// so there is nothing of demarchy's outside its own config directory.
func TestPurgeRemovesTheBoardAndNothingElse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", home)

	if err := dcr.WithTriggerLock(func() error { return dcr.SaveTriggers(&dcr.Triggers{}) }); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dcr.ConfigDir(), dcr.TriggerLockFile)
	for _, p := range []string{dcr.TriggersPath(), lock} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}

	// Somebody else's skill, which purge has no business touching and now has
	// no code that could.
	other := filepath.Join(home, ".claude", "skills", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := purge([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dcr.TriggersPath(), lock, dcr.ConfigDir()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge: %v", p, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("purge reached outside its own config directory")
	}
	if err := purge([]string{"--yes"}); err != nil {
		t.Fatalf("a second purge should find nothing: %v", err)
	}
}
