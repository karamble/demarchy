// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/karamble/demarchy/agents/skills"
	"github.com/karamble/demarchy/internal/dcr"
)

// The catalogue inside SKILL.md is rendered from the Go types between these
// two lines, so it can never disagree with what the binary evaluates.
const (
	catalogueBegin = "<!-- catalogue:begin -->"
	catalogueEnd   = "<!-- catalogue:end -->"
)

// harnessSkillDirs are the places Omarchy links its own skills into, one per
// agent harness it supports, relative to HOME. Claude is one of four.
var harnessSkillDirs = []string{
	".agents/skills",
	".claude/skills",
	".codex/skills",
	".pi/agent/skills",
}

const skillUsage = "usage: demarchy-setup skill [--recipes | --install | --uninstall]"

// skillVerb prints the skill, as herdr does, or links the installed copy where
// every harness looks for it, or takes those links away again.
func skillVerb(args []string) error {
	var install, uninstall, recipes bool
	for _, a := range args {
		switch a {
		case "--install":
			install = true
		case "--uninstall":
			uninstall = true
		case "--recipes":
			recipes = true
		default:
			return errors.New(skillUsage)
		}
	}
	flags := 0
	for _, on := range []bool{install, uninstall, recipes} {
		if on {
			flags++
		}
	}
	switch {
	case flags > 1:
		return errors.New(skillUsage)
	case install:
		return installSkill(os.Stdout)
	case uninstall:
		return uninstallSkill(os.Stdout)
	case recipes:
		b, err := skills.Files.ReadFile(skills.Name + "/recipes.md")
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(b)
		return err
	}
	src, err := skills.Files.ReadFile(skills.Name + "/SKILL.md")
	if err != nil {
		return err
	}
	out, err := renderSkill(src, dcr.Catalogue())
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

// renderSkill replaces the lines strictly between the two markers with the
// catalogue block. A missing, repeated or reversed marker is an error rather
// than a pass-through, so a damaged SKILL.md fails lint instead of printing
// unchanged.
func renderSkill(src []byte, leaves []dcr.Leaf) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, line := range lines {
		switch strings.TrimRight(line, " \t") {
		case catalogueBegin:
			if begin >= 0 {
				return nil, fmt.Errorf("SKILL.md: %s appears twice", catalogueBegin)
			}
			begin = i
		case catalogueEnd:
			if end >= 0 {
				return nil, fmt.Errorf("SKILL.md: %s appears twice", catalogueEnd)
			}
			end = i
		}
	}
	switch {
	case begin < 0:
		return nil, fmt.Errorf("SKILL.md: missing %s", catalogueBegin)
	case end < 0:
		return nil, fmt.Errorf("SKILL.md: missing %s", catalogueEnd)
	case end < begin:
		return nil, errors.New("SKILL.md: catalogue markers are reversed")
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:begin+1]...)
	out = append(out, strings.Split(catalogueBlock(leaves), "\n")...)
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n")), nil
}

// catalogueBlock renders the leaves grouped by their section in catalogue
// order, one bullet per leaf. Bullets rather than a table: nothing to align,
// one path per line, and a test can read the paths back.
func catalogueBlock(leaves []dcr.Leaf) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- Generated from dcr.Catalogue() by make skill. Do not edit by hand: make lint fails when this is stale. -->\n")
	fmt.Fprintf(&b, "%d paths. number and integer take crosses, changes, stalls. text and bool take becomes, stalls. "+
		"A list of records takes appears, disappears, count and filters with --where on the fields shown; "+
		"entries are the same entry when their identity fields match. A list of plain values takes count only.\n",
		len(leaves))
	section := ""
	for _, l := range leaves {
		if s := strings.SplitN(l.Path, ".", 2)[0]; s != section {
			section = s
			fmt.Fprintf(&b, "\n### %s\n", section)
		}
		kind := l.Kind.String()
		if l.Integer {
			kind = "integer"
		}
		fmt.Fprintf(&b, "- `%s` %s: %s", l.Path, kind, strings.Join(l.Operators, ", "))
		if len(l.Fields) > 0 {
			fmt.Fprintf(&b, ". Fields: %s. Identity: %s", strings.Join(l.Fields, ", "), strings.Join(l.Identity, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// skillDir is where the installed plugin keeps the skill: agents/skills next to
// the bin directory this binary runs from. The links point there rather than
// at a copy, so every install refreshes what agents read.
func skillDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(exe)), "agents", "skills", skills.Name)
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return "", fmt.Errorf("no skill beside this binary at %s; run the installed plugin's demarchy-setup (make install puts it there), not go run", dir)
	}
	return dir, nil
}

// installSkill links the skill into every harness directory Omarchy uses for
// its own skills. The links point into the plugin folder, which omarchy plugin
// update pulls in place, so they never need making again.
func installSkill(w io.Writer) error {
	src, err := skillDir()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return installLinks(w, home, src)
}

// uninstallSkill removes the links installSkill made and nothing else. It does
// not need the plugin folder to still exist: after omarchy plugin remove the
// links dangle, and this is what cleans them up.
func uninstallSkill(w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	n, err := removeLinks(w, home)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintln(w, "no skill links to remove")
	}
	return nil
}

// installLinks makes the link in every harness directory under home, reporting
// each. A real directory in the way is refused and left alone: it is somebody's
// content, and a plain ln -sfn would have nested the link inside it.
func installLinks(w io.Writer, home, src string) error {
	var refused []string
	for _, rel := range harnessSkillDirs {
		parent := filepath.Join(home, rel)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		link := filepath.Join(parent, skills.Name)
		what, err := ensureLink(link, src)
		if err != nil {
			fmt.Fprintf(w, "refused %s: %v\n", link, err)
			refused = append(refused, link)
			continue
		}
		fmt.Fprintf(w, "%s %s\n", what, link)
	}
	if len(refused) > 0 {
		return fmt.Errorf("%d of %d skill links not made", len(refused), len(harnessSkillDirs))
	}
	fmt.Fprintf(w, "agents list it as %s from their next start; print it with: demarchy-setup skill\n", skills.Name)
	return nil
}

// ourLinks lists the skill links present under home that point at a demarchy
// skill folder, wherever that folder is or was.
func ourLinks(home string) []string {
	var links []string
	for _, rel := range harnessSkillDirs {
		link := filepath.Join(home, rel, skills.Name)
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if target, err := os.Readlink(link); err == nil && isOurSkill(target) {
			links = append(links, link)
		}
	}
	return links
}

// removeLinks removes every link ourLinks finds and says what it left: a link
// pointing somewhere else, or a real directory of the same name, is somebody
// else's.
func removeLinks(w io.Writer, home string) (int, error) {
	removed := 0
	for _, rel := range harnessSkillDirs {
		link := filepath.Join(home, rel, skills.Name)
		info, err := os.Lstat(link)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			fmt.Fprintf(w, "left %s: not a symlink\n", link)
			continue
		}
		target, _ := os.Readlink(link)
		if !isOurSkill(target) {
			fmt.Fprintf(w, "left %s: points at %s\n", link, target)
			continue
		}
		if err := os.Remove(link); err != nil {
			return removed, err
		}
		fmt.Fprintf(w, "unlinked %s\n", link)
		removed++
	}
	return removed, nil
}

// isOurSkill recognises a link target as a demarchy skill folder by its tail,
// so a plugin folder that has since moved or gone still counts.
func isOurSkill(target string) bool {
	return strings.HasSuffix(filepath.Clean(target), filepath.Join("agents", "skills", skills.Name))
}

// ensureLink makes path a symlink to target and says what it did: "linked",
// "already linked", or "relinked from <old>". Anything at path that is not a
// symlink is an error, never removed.
func ensureLink(path, target string) (string, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Symlink(target, path); err != nil {
			return "", err
		}
		return "linked", nil
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink == 0:
		return "", errors.New("exists and is not a symlink; move it aside and rerun")
	}
	old, _ := os.Readlink(path)
	if old == target {
		return "already linked", nil
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if err := os.Symlink(target, path); err != nil {
		return "", err
	}
	return "relinked from " + old, nil
}

// hasLongDash reports an em or en dash, which this repository keeps out of
// every file, generated ones included.
func hasLongDash(b []byte) bool {
	return bytes.Contains(b, []byte("\u2014")) || bytes.Contains(b, []byte("\u2013"))
}
