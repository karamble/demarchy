// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command demarchy-setup stores and validates the MCP token the Decred
// Pulse widget uses.
//
// It is a separate binary so the Quickshell process never handles the token:
// the panel launches this in a terminal, and only ever learns whether a token
// is present and valid.
//
// The token it accepts is deliberately weak. dcrpulse issues per-agent tokens
// whose read domains, write scopes and spend caps are set in the dashboard
// (Settings -> AI Agents), so a widget can hold a credential that can read
// node and staking state and nothing else. This tool refuses to store anything
// stronger than that.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// section describes what one read domain adds to the panel. The grant is the
// widget's configuration: a domain that is not granted is a section that never
// appears, with nothing to switch off by hand. Only the first two are needed
// for the widget to be worth having; the rest are additions.
type section struct {
	domain   string
	adds     string
	required bool
}

var sections = []section{
	{"node", "chain status, height and peers", true},
	{"staking", "the staking hero, ticket price and pool", true},
	{"bisonrelay", "messages, the unread badge, and the DCR price", false},
	{"wallet", "wallet balances (hidden until switched on)", false},
	{"lightning", "channel inbound/outbound", false},
	{"treasury", "the treasury value", false},
	{"dex", "the DCR/BTC price chart, if a DEX server is registered", false},
}

func requiredDomains() []string {
	var out []string
	for _, s := range sections {
		if s.required {
			out = append(out, s.domain)
		}
	}
	return out
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `demarchy-setup: manage the dcrpulse connections the bar widget uses.

  demarchy-setup              add a connection (prompts for name, endpoint, token)
  demarchy-setup list         list the stored connections
  demarchy-setup check        re-validate every stored connection
  demarchy-setup remove <id>  delete a connection and its token
  demarchy-setup purge        delete every connection and the config directory

Tokens are stored in %s, mode 0600.
`, dcr.ConnectionsPath())
	}
	flag.Parse()

	if err := run(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	list, err := dcr.LoadConnections()
	if err != nil {
		return err
	}

	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "list":
		return listConnections(list)
	case "check":
		return checkConnections(list)
	case "remove":
		if len(args) < 2 {
			return errors.New("usage: demarchy-setup remove <id>")
		}
		if err := list.Remove(args[1]); err != nil {
			return err
		}
		if err := dcr.SaveConnections(list); err != nil {
			return err
		}
		fmt.Printf("Removed %q and its token.\n", args[1])
		return nil
	case "purge":
		return purge(args[1:])
	case "":
		return addConnection(list)
	default:
		return fmt.Errorf("unknown command %q; try --help", verb)
	}
}

// purge removes every stored credential and the directory holding them.
//
// Uninstalling the plugin takes the plugin folder away but leaves this behind,
// and what it leaves behind is bearer tokens. Removing them has to be something
// a person can actually do, so it is one command rather than a paragraph of
// paths in a README.
func purge(args []string) error {
	yes := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			yes = true
		}
	}

	dir := dcr.ConfigDir()
	var found []string
	for _, name := range []string{dcr.ConnectionsFile, "token"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		fmt.Println("Nothing stored; nothing to remove.")
		return nil
	}

	fmt.Println("This deletes every stored connection and its token:")
	for _, p := range found {
		fmt.Println("   ", p)
	}
	fmt.Println()
	fmt.Println("The tokens themselves are not revoked. Do that in the dcrpulse")
	fmt.Println("dashboard under Settings -> AI Agents if you want them dead everywhere.")

	if !yes {
		answer, err := prompt("\nType 'yes' to delete: ")
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			fmt.Println("Left alone.")
			return nil
		}
	}

	for _, p := range found {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Only if it is now empty: a person may keep other things in there.
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	fmt.Println("Removed.")
	return nil
}

func listConnections(list *dcr.Connections) error {
	if len(list.Conns) == 0 {
		fmt.Println("No connections yet. Run demarchy-setup to add one.")
		return nil
	}
	active := dcr.StringSetting("activeConnection", "")
	for _, c := range list.Conns {
		mark := " "
		if c.ID == active || (active == "" && c.ID == list.Conns[0].ID) {
			mark = "*"
		}
		fmt.Printf("%s %-14s %s\n", mark, c.ID, c.Endpoint)
	}
	return nil
}

func checkConnections(list *dcr.Connections) error {
	if len(list.Conns) == 0 {
		return dcr.ErrNoConnections
	}
	bad := 0
	for _, c := range list.Conns {
		fmt.Printf("%s (%s)\n", c.Name, c.Endpoint)
		caps, err := validate(c.Endpoint, c.Token)
		if err != nil {
			bad++
			fmt.Printf("  %v\n\n", err)
			continue
		}
		report(caps)
		fmt.Println()
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d connections did not validate", bad, len(list.Conns))
	}
	return nil
}

func addConnection(list *dcr.Connections) error {
	fmt.Println("Demarchy: token setup")
	fmt.Println()
	fmt.Println("In the dcrpulse dashboard, go to Settings -> AI Agents and create an")
	fmt.Println("agent for this widget with:")
	fmt.Println()
	fmt.Println("    domains:     node, staking, bisonrelay, wallet")
	fmt.Println("                 (add lightning, treasury or dex for those sections)")
	fmt.Println("    write scopes: (none)")
	fmt.Println("    spend:        do not grant")
	fmt.Println("    allowed IPs:  127.0.0.1")
	fmt.Println()
	fmt.Println("Then paste its token below. This tool will refuse it if it turns out")
	fmt.Println("to carry write or spend authority.")
	fmt.Println()

	name, err := prompt("Name (e.g. home, vps): ")
	if err != nil {
		return err
	}
	rawEndpoint, err := prompt("Endpoint [127.0.0.1:8090]: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(rawEndpoint) == "" {
		rawEndpoint = dcr.DefaultEndpoint
	}
	endpoint, err := dcr.NormaliseEndpoint(rawEndpoint)
	if err != nil {
		return err
	}

	token, err := readToken()
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("no token entered")
	}

	fmt.Printf("\nChecking against %s ...\n", endpoint)
	caps, err := validate(endpoint, token)
	if err != nil {
		return err
	}
	conn, err := list.Add(name, endpoint, token)
	if err != nil {
		return err
	}
	if err := dcr.SaveConnections(list); err != nil {
		return err
	}
	fmt.Println()
	report(caps)
	fmt.Println()
	fmt.Printf("Stored as %q in %s (mode 0600).\n", conn.ID, dcr.ConnectionsPath())
	fmt.Println("Pick it from the switcher in the panel footer.")
	return nil
}

// prompt asks for one line of non-secret input.
func prompt(label string) (string, error) {
	fmt.Print(label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// validate refuses a token that can do more than read.
func validate(endpoint, token string) (*dcr.Capabilities, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client := dcr.NewClient(endpoint, token)
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	if why := caps.CheckReadOnly(); why != nil {
		return nil, fmt.Errorf(
			"refusing to store this token because %w.\n"+
				"A bar widget should not be able to move funds or send messages. Create a\n"+
				"separate read-only agent in Settings -> AI Agents and use its token instead", why)
	}
	return caps, nil
}

func report(caps *dcr.Capabilities) {
	fmt.Printf("Token accepted: agent %q, read-only.\n\n", caps.Agent)

	var shown, hidden []string
	for _, sec := range sections {
		line := fmt.Sprintf("    %-11s %s", sec.domain, sec.adds)
		if caps.HasDomain(sec.domain) {
			shown = append(shown, line)
		} else {
			hidden = append(hidden, line)
		}
	}

	if len(shown) > 0 {
		fmt.Println("  The panel will show:")
		for _, l := range shown {
			fmt.Println(l)
		}
	}
	if len(hidden) > 0 {
		fmt.Println("\n  Not granted, so these sections stay hidden:")
		for _, l := range hidden {
			fmt.Println(l)
		}
		fmt.Println("\n  Add a domain in the dashboard and the section appears; there is")
		fmt.Println("  nothing to configure here.")
	}

	if missing := caps.MissingDomains(requiredDomains()); len(missing) > 0 {
		fmt.Printf("\n  Warning: without %s there is very little to show.\n",
			strings.Join(missing, " and "))
	}

	var extra []string
	for _, d := range caps.Domains {
		known := false
		for _, sec := range sections {
			if sec.domain == d {
				known = true
				break
			}
		}
		if !known {
			extra = append(extra, d)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		fmt.Printf("\n  Also granted, unused by this widget: %s\n", strings.Join(extra, ", "))
	}
}

// readToken takes the token from a pipe when there is one, otherwise prompts
// with terminal echo disabled so it does not end up in a scrollback buffer.
func readToken() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	piped := info.Mode()&os.ModeCharDevice == 0

	if !piped {
		fmt.Print("Token: ")
		restore, err := echoOff()
		if err == nil {
			defer restore()
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if !piped {
		fmt.Println()
	}
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// echoOff turns off terminal echo via stty, avoiding a termios dependency for
// the one moment in this program that needs it.
func echoOff() (func(), error) {
	if err := stty("-echo"); err != nil {
		return nil, err
	}
	return func() { _ = stty("echo") }, nil
}

func stty(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
