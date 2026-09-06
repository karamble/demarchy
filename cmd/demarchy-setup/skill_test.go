// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/karamble/demarchy/agents/skills"
	"github.com/karamble/demarchy/internal/dcr"
)

// skillSource is the checked-in skill directory, relative to this package.
const skillSource = "../../agents/skills/" + skills.Name

func TestRenderSkillIsIdempotentAndComplete(t *testing.T) {
	src := []byte("---\nname: x\n---\n\n## Catalogue\n\n" + catalogueBegin + "\nstale line\n" + catalogueEnd + "\n\ntail\n")
	leaves := dcr.Catalogue()
	once, err := renderSkill(src, leaves)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := renderSkill(once, leaves)
	if err != nil || !bytes.Equal(once, twice) {
		t.Fatalf("render is not a fixed point: %v", err)
	}
	if bytes.Contains(once, []byte("stale line")) || !bytes.HasSuffix(once, []byte("tail\n")) || !bytes.HasPrefix(once, []byte("---\nname: x\n")) {
		t.Fatalf("render touched more than the block:\n%s", once)
	}
	if hasLongDash(once) {
		t.Fatal("the rendered block carries a long dash")
	}
	re := regexp.MustCompile("(?m)^- `([^`]+)`")
	var got []string
	for _, m := range re.FindAllStringSubmatch(string(once), -1) {
		got = append(got, m[1])
	}
	want := make([]string, 0, len(leaves))
	for _, l := range leaves {
		want = append(want, l.Path)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("block lists %d paths, catalogue has %d:\n%v", len(got), len(want), got)
	}
	if !strings.Contains(string(once), "- `node.height` integer: crosses, changes, stalls") ||
		!strings.Contains(string(once), "- `br.messages` list: appears, disappears, count. Fields: type, fromNick, text, gcid, gcName. Identity: type, fromNick, text, gcid") {
		t.Fatalf("a known leaf is not rendered as expected:\n%s", once)
	}
}

func TestRenderSkillRefusesBadMarkers(t *testing.T) {
	bad := map[string]string{
		"no markers":      "# x\n",
		"only begin":      catalogueBegin + "\n",
		"only end":        catalogueEnd + "\n",
		"reversed":        catalogueEnd + "\n" + catalogueBegin + "\n",
		"duplicate begin": catalogueBegin + "\n" + catalogueBegin + "\n" + catalogueEnd + "\n",
	}
	for why, src := range bad {
		if _, err := renderSkill([]byte(src), dcr.Catalogue()); err == nil {
			t.Errorf("%s: should be refused", why)
		}
	}
}

// The same gate make lint applies, so go test alone catches a stale block.
func TestCheckedInSkillMatchesCatalogue(t *testing.T) {
	disk, err := os.ReadFile(filepath.Join(skillSource, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderSkill(disk, dcr.Catalogue())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(disk, rendered) {
		t.Fatal("SKILL.md's catalogue block is stale; run: make skill")
	}
	embedded, err := skills.Files.ReadFile(skills.Name + "/SKILL.md")
	if err != nil || !bytes.Equal(embedded, disk) {
		t.Fatalf("the embedded SKILL.md is not the checked-in one: %v", err)
	}
}

func TestSkillFrontmatterAndProse(t *testing.T) {
	for _, name := range []string{"SKILL.md", "recipes.md"} {
		b, err := os.ReadFile(filepath.Join(skillSource, name))
		if err != nil {
			t.Fatal(err)
		}
		if hasLongDash(b) {
			t.Errorf("%s carries a long dash", name)
		}
		if bytes.Contains(bytes.ToLower(b), []byte("claude")) {
			t.Errorf("%s names one harness; the skill is installed for every harness Omarchy supports", name)
		}
	}
	b, _ := os.ReadFile(filepath.Join(skillSource, "SKILL.md"))
	lines := strings.SplitN(string(b), "\n", 5)
	if len(lines) < 4 || lines[0] != "---" || lines[1] != "name: "+skills.Name ||
		!strings.HasPrefix(lines[2], "description: \"") || lines[3] != "---" {
		t.Fatalf("frontmatter must be exactly name and description:\n%s", strings.Join(lines[:4], "\n"))
	}
	if !strings.Contains(string(b), "[`recipes.md`](recipes.md)") {
		t.Error("SKILL.md should link recipes.md the way Omarchy's skills link their sibling files")
	}
}

func TestEnsureLink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	other := filepath.Join(tmp, "other")
	for _, d := range []string{target, other, filepath.Join(tmp, "links")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(tmp, "links", skills.Name)

	if what, err := ensureLink(link, target); err != nil || what != "linked" {
		t.Fatalf("absent: %q %v", what, err)
	}
	if what, err := ensureLink(link, target); err != nil || what != "already linked" {
		t.Fatalf("same: %q %v", what, err)
	}
	if what, err := ensureLink(link, other); err != nil || what != "relinked from "+target {
		t.Fatalf("foreign: %q %v", what, err)
	}
	os.Remove(link)
	os.Symlink(filepath.Join(tmp, "gone"), link)
	if what, err := ensureLink(link, target); err != nil || !strings.HasPrefix(what, "relinked from") {
		t.Fatalf("dangling: %q %v", what, err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Fatalf("link points at %s", got)
	}

	os.Remove(link)
	os.MkdirAll(filepath.Join(link, "keep"), 0o755)
	if _, err := ensureLink(link, target); err == nil {
		t.Fatal("a real directory must be refused")
	}
	if _, err := os.Stat(filepath.Join(link, "keep")); err != nil {
		t.Fatal("the refused directory was touched")
	}
}

// Install makes one link per harness under HOME and remove takes exactly those
// away, leaving anything that is not ours.
func TestInstallAndRemoveLinks(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "plugin", "agents", "skills", skills.Name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installLinks(io.Discard, home, src); err != nil {
		t.Fatal(err)
	}
	for _, rel := range harnessSkillDirs {
		got, err := os.Readlink(filepath.Join(home, rel, skills.Name))
		if err != nil || got != src {
			t.Fatalf("%s: %q %v", rel, got, err)
		}
	}
	if links := ourLinks(home); len(links) != len(harnessSkillDirs) {
		t.Fatalf("ourLinks found %d", len(links))
	}

	// Somebody else's skill of the same name in one harness, and a foreign
	// link in another, must both survive.
	foreignDir := filepath.Join(home, ".codex", "skills", skills.Name)
	os.Remove(foreignDir)
	os.MkdirAll(filepath.Join(foreignDir, "keep"), 0o755)
	foreignLink := filepath.Join(home, ".pi", "agent", "skills", skills.Name)
	os.Remove(foreignLink)
	os.Symlink(filepath.Join(home, "elsewhere"), foreignLink)

	n, err := removeLinks(io.Discard, home)
	if err != nil || n != 2 {
		t.Fatalf("removed %d, %v; want 2", n, err)
	}
	for _, rel := range []string{".agents/skills", ".claude/skills"} {
		if _, err := os.Lstat(filepath.Join(home, rel, skills.Name)); !os.IsNotExist(err) {
			t.Fatalf("%s: link still there: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(foreignDir, "keep")); err != nil {
		t.Fatal("a real directory was removed")
	}
	if got, _ := os.Readlink(foreignLink); got != filepath.Join(home, "elsewhere") {
		t.Fatal("a foreign link was removed")
	}
	if n, _ := removeLinks(io.Discard, home); n != 0 {
		t.Fatal("a second removal found something")
	}
	if isOurSkill("/gone/plugin/agents/skills/"+skills.Name) != true || isOurSkill("/x/skills/other") {
		t.Fatal("isOurSkill misjudges a target")
	}
}
