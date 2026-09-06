// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package skills carries the agent skill so demarchy-setup can print and
// install it. The layout mirrors Omarchy's own default/agents/skills: one
// directory per skill, a SKILL.md every harness reads, flat sibling files.
package skills

import "embed"

// Name is the skill's directory, and the name every harness lists it under.
const Name = "demarchy-alerts"

// Files holds the skill. The build fails if a file goes missing, which is the
// point: the binary and the documentation cannot drift apart.
//
//go:embed demarchy-alerts/*.md
var Files embed.FS
