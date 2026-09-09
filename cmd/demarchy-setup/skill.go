package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/karamble/demarchy/docs"
)

const skillUsage = "usage: demarchy-setup skill [--recipes]"

// skillVerb prints the alerts guide. It only prints: there is deliberately no
// --install, because linking this into an agent's own directories is a plugin
// reaching outside itself to change how every agent on the machine behaves.
// Read on request, it is documentation; written into ~/.claude/skills, it is
// something else.
func skillVerb(args []string) error {
	recipes := false
	for _, a := range args {
		switch a {
		case "--recipes":
			recipes = true
		default:
			return errors.New(skillUsage)
		}
	}
	return printGuide(os.Stdout, recipes)
}

func printGuide(w io.Writer, recipes bool) error {
	text := docs.Alerts
	if recipes {
		text = docs.Recipes
	}
	_, err := fmt.Fprint(w, text)
	return err
}
