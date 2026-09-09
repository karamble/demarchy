// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// The recipes are the sample values every wake-up text in recipes.md is built
// from. Change them there and here together.
var (
	recipeID      = "t-7f3a9c21"
	recipeConn    = "omarchy"
	recipeArmedBy = "w7:p1"
	recipeArmedAt = time.Date(2026, 9, 5, 22, 10, 0, 0, time.UTC)
	recipeFiredAt = time.Date(2026, 9, 6, 0, 42, 2, 0, time.UTC)
	triggerID     = regexp.MustCompile(`^t-[0-9a-f]{8}$`)
)

// Every command the skill shows an agent is run through the real flag parser
// and the real validator, and every recipe's wake-up text has to be the exact
// one FireText would produce. A recipe that names a dropped path, a wrong flag
// or a paraphrased alarm fails the build instead of misleading an agent.
func TestRecipesParseValidateAndMatchFireText(t *testing.T) {
	docs := map[string]string{}
	for _, name := range []string{"alerts.md", "alert-recipes.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		docs[name] = string(b)
	}
	seen := map[string]bool{}
	for name, text := range docs {
		for _, line := range commandsIn(text) {
			// Angle brackets mark syntax, not an example.
			if strings.Contains(line, "<") {
				continue
			}
			words := splitWords(line)
			if len(words) < 2 || words[0] != "demarchy-setup" {
				continue
			}
			switch words[1] {
			case "arm":
				f, err := parseArmFlags(words[2:])
				if err != nil {
					t.Errorf("%s: %q: %v", name, line, err)
					continue
				}
				if len(f.positional) != 2 {
					t.Errorf("%s: %q: arm takes a path and an operator", name, line)
					continue
				}
				f.req.Path, f.req.Operator = f.positional[0], f.positional[1]
				f.req.ArmedBy = recipeArmedBy
				if f.req.DeliverTo == "" {
					f.req.DeliverTo = recipeArmedBy
				}
				tr, err := dcr.NewTrigger(f.req, recipeConn, recipeArmedAt)
				if err != nil {
					t.Errorf("%s: %q: %v", name, line, err)
					continue
				}
				seen[tr.Operator] = true
				if name == "recipes.md" && !f.dryRun {
					tr.ID = recipeID
					want := dcr.FireText(&tr, recipeFiredAt)
					if !strings.Contains(docs[name], want) {
						t.Errorf("recipes.md lacks the exact wake-up text for %q:\n%s", line, want)
					}
				}
			case "edit":
				f, err := parseArmFlags(words[2:])
				if err != nil || (len(f.positional) != 1 && len(f.positional) != 3) || !triggerID.MatchString(f.positional[0]) {
					t.Errorf("%s: %q: not a valid edit: %v", name, line, err)
				}
			case "disarm":
				if len(words) != 3 || !triggerID.MatchString(words[2]) {
					t.Errorf("%s: %q: disarm takes one id", name, line)
				}
			case "alerts", "catalogue", "skill", "list", "check":
			default:
				t.Errorf("%s: %q: unknown verb", name, line)
			}
		}
	}
	for _, op := range []string{"crosses", "becomes", "changes", "stalls", "appears", "disappears", "count"} {
		if !seen[op] {
			t.Errorf("no recipe arms %s", op)
		}
	}
}

// commandsIn returns every demarchy-setup command inside a fenced block, with
// backslash continuations joined.
func commandsIn(text string) []string {
	var out []string
	inFence, pending := false, ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			pending = ""
			continue
		}
		if !inFence {
			continue
		}
		if pending != "" {
			line = pending + " " + line
			pending = ""
		}
		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSpace(strings.TrimSuffix(line, "\\"))
			continue
		}
		if strings.HasPrefix(line, "demarchy-setup ") {
			out = append(out, line)
		}
	}
	return out
}

// splitWords splits a command line the way a shell would for the quoting the
// recipes use: double and single quotes group, nothing else is interpreted.
func splitWords(line string) []string {
	var words []string
	var cur strings.Builder
	quote := byte(0)
	inWord := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}
