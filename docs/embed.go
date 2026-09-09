// Package docs carries the alerts guide, embedded so the binary can print it on
// request.
//
// It used to be an agent skill: the same words, plus front matter, symlinked
// into ~/.claude/skills and ~/.agents/skills. That is what a marketplace review
// objected to, and rightly — installing it changed the instructions every
// coding agent on the machine follows, from inside a bar widget, whether or not
// the agent had anything to do with Decred.
//
// Printing it does not. `demarchy-setup skill` writes the guide to stdout and
// nothing else happens: an agent that wants it asks for it, the way it would
// ask for --help, and nothing outside this folder is touched.
package docs

import _ "embed"

//go:embed alerts.md
var Alerts string

//go:embed alert-recipes.md
var Recipes string
