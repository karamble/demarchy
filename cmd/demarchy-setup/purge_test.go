// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/karamble/demarchy/agents/skills"
	"github.com/karamble/demarchy/internal/dcr"
)

// Purge is the documented first step of removing the plugin, so it has to take
// the alerts board and the skill links with it, and nothing of anyone else's.
func TestPurgeRemovesBoardAndSkillLinks(t *testing.T) {
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
	src := filepath.Join(t.TempDir(), "agents", "skills", skills.Name)
	os.MkdirAll(src, 0o755)
	if err := installLinks(io.Discard, home, src); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, ".claude", "skills", "other")
	os.MkdirAll(other, 0o755)

	if err := purge([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dcr.TriggersPath(), lock, dcr.ConfigDir()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge: %v", p, err)
		}
	}
	for _, rel := range harnessSkillDirs {
		if _, err := os.Lstat(filepath.Join(home, rel, skills.Name)); !os.IsNotExist(err) {
			t.Fatalf("%s: skill link survived purge", rel)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another skill was removed")
	}
	if err := purge([]string{"--yes"}); err != nil {
		t.Fatalf("a second purge should find nothing: %v", err)
	}
}
