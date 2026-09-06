// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// alertVerb dispatches the alerts verbs. handled is false for any other verb.
func alertVerb(verb string, args []string) (handled bool, err error) {
	switch verb {
	case "catalogue":
		return true, catalogue(args)
	case "alerts":
		return true, listAlerts(args)
	case "arm":
		return true, arm(args)
	case "edit":
		return true, editAlert(args)
	case "disarm":
		return true, disarm(args)
	}
	return false, nil
}

// armFlags is the command line of arm and edit, parsed. The positional words
// are the path and operator for arm, and the id, then optionally a new path and
// operator, for edit.
type armFlags struct {
	req        dcr.ArmRequest
	asJSON     bool
	dryRun     bool
	positional []string
}

func parseArmFlags(args []string) (armFlags, error) {
	var f armFlags
	text := func(i int, what string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s needs %s", args[i], what)
		}
		return args[i+1], nil
	}
	number := func(i int) (*float64, error) {
		v, err := text(i, "a number")
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", args[i], v)
		}
		return &n, nil
	}
	p := &f.req.Params
	for i := 0; i < len(args); i++ {
		var err error
		switch a := args[i]; a {
		case "--above", "--below":
			p.Bound, err = number(i)
			p.Direction = strings.TrimPrefix(a, "--")
			i++
		case "--rearm":
			p.Rearm, err = number(i)
			i++
		case "--by":
			p.By, err = number(i)
			i++
		case "--percent":
			p.Percent = true
		case "--value":
			p.Value, err = text(i, "the value to wait for")
			i++
		case "--hold":
			p.Hold, err = text(i, "a duration, for example 5m")
			i++
		case "--for":
			p.For, err = text(i, "a duration, for example 45m")
			i++
		case "--where":
			var w string
			w, err = text(i, "field=value, or field~=text for a substring")
			if err == nil {
				k, v, ok := strings.Cut(w, "=")
				if !ok || k == "" {
					err = fmt.Errorf("--where %q: write field=value, or field~=text for a substring", w)
				} else {
					if p.Where == nil {
						p.Where = map[string]string{}
					}
					p.Where[k] = v
				}
			}
			i++
		case "--key":
			var k string
			k, err = text(i, "a field name")
			p.Key = append(p.Key, strings.Split(k, ",")...)
			i++
		case "--expires":
			f.req.Expires, err = text(i, "a span such as 4d or 12h, a date, or an RFC 3339 time")
			i++
		case "--reason":
			f.req.Reason, err = text(i, "some text")
			i++
		case "--deliver":
			f.req.DeliverTo, err = text(i, "a herdr agent name or pane id, or you")
			i++
		case "--connection":
			f.req.Connection, err = text(i, "a connection id from: demarchy-setup list")
			i++
		case "--standing":
			f.req.Once = boolPtr(false)
		case "--once":
			f.req.Once = boolPtr(true)
		case "--json":
			f.asJSON = true
		case "--dry-run":
			f.dryRun = true
		default:
			if strings.HasPrefix(a, "-") {
				return f, fmt.Errorf("unknown flag %q; try --help", a)
			}
			f.positional = append(f.positional, a)
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

func boolPtr(b bool) *bool { return &b }

// identify fills in who is arming. Inside a herdr pane the agent is known and
// gets the alarm back unless told otherwise; anywhere else it is you.
func identify(req *dcr.ArmRequest) {
	if pane := strings.TrimSpace(os.Getenv("HERDR_PANE_ID")); pane != "" {
		req.ArmedBy = pane
		if req.DeliverTo == "" {
			req.DeliverTo = pane
		}
	}
}

func arm(args []string) error {
	f, err := parseArmFlags(args)
	if err != nil {
		return err
	}
	if len(f.positional) != 2 {
		return errors.New("usage: demarchy-setup arm <path> <operator> [params] --expires <4d|12h|date> " +
			"[--reason <text>] [--deliver <agent|you>] [--standing] [--dry-run]; see: demarchy-setup catalogue")
	}
	f.req.Path, f.req.Operator = f.positional[0], f.positional[1]
	identify(&f.req)
	if f.dryRun {
		t, warnings, err := dcr.PrepareArm(f.req, time.Now())
		if err != nil {
			return err
		}
		return printDryRun(t, warnings)
	}
	t, warnings, err := dcr.Arm(f.req, time.Now())
	if err != nil {
		return err
	}
	return printArmed("Armed", t, warnings, f.asJSON)
}

func editAlert(args []string) error {
	f, err := parseArmFlags(args)
	if err != nil {
		return err
	}
	switch len(f.positional) {
	case 1:
	case 3:
		f.req.Path, f.req.Operator = f.positional[1], f.positional[2]
	default:
		return errors.New("usage: demarchy-setup edit <id> [<path> <operator>] [params] [--expires ...] [--reason ...] [--deliver ...] [--standing|--once] [--dry-run]")
	}
	if f.dryRun {
		t, warnings, err := dcr.PrepareEdit(f.positional[0], f.req, time.Now())
		if err != nil {
			return err
		}
		return printDryRun(t, warnings)
	}
	t, warnings, err := dcr.EditTrigger(f.positional[0], f.req, time.Now())
	if err != nil {
		return err
	}
	return printArmed("Edited", t, warnings, f.asJSON)
}

func disarm(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: demarchy-setup disarm <id>")
	}
	t, err := dcr.Disarm(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Disarmed %s: %s %s %s, was delivering to %s.\n",
		t.ID, t.Path, t.Operator, dcr.DescribeParams(&t), t.DeliverTo)
	return nil
}

// printDryRun shows what arm or edit would have written, in the shape the store
// keeps, and confirms that nothing was. An arm carries no id: the store mints
// one only when it saves, so nothing here can be disarmed later by mistake.
func printDryRun(t dcr.Trigger, warnings []string) error {
	if err := printJSON(struct {
		Trigger  dcr.Trigger `json:"trigger"`
		Warnings []string    `json:"warnings,omitempty"`
	}{t, warnings}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "dry run: valid, nothing saved")
	return nil
}

// printArmed shows the trigger as stored, and the warnings, which are the part
// an agent most needs to read: a valid trigger nobody is watching.
func printArmed(did string, t dcr.Trigger, warnings []string, asJSON bool) error {
	now := time.Now()
	if asJSON {
		return printJSON(struct {
			dcr.TriggerView
			Warnings []string `json:"warnings,omitempty"`
		}{t.View(now, "", true), warnings})
	}
	fmt.Printf("%s %s: %s %s %s on connection %s\n", did, t.ID, t.Path, t.Operator, dcr.DescribeParams(&t), t.Connection)
	shots := "rings once, then it is spent"
	if !t.IsOnce() {
		shots = "standing: rings each time"
	}
	fmt.Printf("  delivers to %s, %s, expires %s (in %s)\n", t.DeliverTo, shots,
		t.ExpiresAt.Local().Format("2006-01-02 15:04"), untilText(t.ExpiresAt.Sub(now)))
	if t.Reason != "" {
		fmt.Printf("  reason: %s\n", t.Reason)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	return nil
}

func listAlerts(args []string) error {
	asJSON, err := onlyJSONFlag(args, "alerts")
	if err != nil {
		return err
	}
	list, err := dcr.LoadTriggers()
	if err != nil {
		return err
	}
	now := time.Now()
	active := activeConnectionID()
	views := make([]dcr.TriggerView, 0, len(list.Triggers))
	for i := range list.Triggers {
		views = append(views, list.Triggers[i].View(now, active, true))
	}
	if asJSON {
		return printJSON(views)
	}
	if len(views) == 0 {
		fmt.Println("No alerts armed. Arm one with: demarchy-setup arm <path> <operator> ... --expires 4d")
		return nil
	}

	// Grouped by recipient, you first, so the board reads as "who hears what".
	groups := map[string][]dcr.TriggerView{}
	for _, v := range views {
		groups[v.DeliverTo] = append(groups[v.DeliverTo], v)
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == dcr.You || names[j] == dcr.You {
			return names[i] == dcr.You
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		fmt.Printf("%s\n", name)
		for _, v := range groups[name] {
			when := "expires in " + untilText(v.ExpiresAt.Sub(now))
			switch v.Status {
			case "fired":
				when = "fired " + v.FiredAt.Local().Format("Jan 2 15:04")
				if v.Delivered != "" {
					when += ", " + v.Delivered
				}
			case "expired":
				when = "expired " + v.ExpiresAt.Local().Format("Jan 2 15:04")
			case "delivery-failed":
				when = "NOT DELIVERED: " + v.DeliveryError
			}
			fmt.Printf("  %s  %-16s %s %s %s\n", v.ID, v.Status, v.Path, v.Operator, v.Params)
			fmt.Printf("  %11s  %s", "", when)
			if v.Connection != active && active != "" {
				fmt.Printf(", on connection %s", v.Connection)
			}
			if v.Reason != "" {
				fmt.Printf("  %q", v.Reason)
			}
			fmt.Println()
		}
	}
	if !dcr.BoolSetting("monitoring", false) {
		fmt.Fprintln(os.Stderr, "warning: monitoring is off, so none of these are being watched")
	}
	return nil
}

func catalogue(args []string) error {
	asJSON, err := onlyJSONFlag(args, "catalogue")
	if err != nil {
		return err
	}
	leaves := dcr.Catalogue()
	if asJSON {
		return printJSON(leaves)
	}
	for _, l := range leaves {
		kind := l.Kind.String()
		if l.Integer {
			kind = "integer"
		}
		fmt.Printf("%-30s %-8s %s\n", l.Path, kind, strings.Join(l.Operators, ", "))
		if len(l.Fields) > 0 {
			fmt.Printf("%-30s %-8s fields: %s; identity: %s\n", "", "",
				strings.Join(l.Fields, ", "), strings.Join(l.Identity, ", "))
		}
	}
	return nil
}

func onlyJSONFlag(args []string, verb string) (bool, error) {
	switch {
	case len(args) == 0:
		return false, nil
	case len(args) == 1 && args[0] == "--json":
		return true, nil
	}
	return false, fmt.Errorf("usage: demarchy-setup %s [--json]", verb)
}

func printJSON(v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// activeConnectionID is what the widget watches, or "" when that cannot be
// told, in which case nothing is marked as being on another connection.
func activeConnectionID() string {
	list, err := dcr.LoadConnections()
	if err != nil {
		return ""
	}
	conn, err := list.Active(dcr.StringSetting("activeConnection", ""))
	if err != nil {
		return ""
	}
	return conn.ID
}

// untilText says how long is left in the coarsest unit that is still honest.
func untilText(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	d = d.Round(time.Minute)
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	mins := int(d % time.Hour / time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
