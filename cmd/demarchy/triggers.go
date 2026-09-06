// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// triggerCommand is one instruction to the alerts board over the one-shot. The
// arming fields are the same ones the setup tool fills from its flags.
type triggerCommand struct {
	Cmd string `json:"cmd"` // list, catalogue, arm, edit, disarm
	ID  string `json:"id,omitempty"`
	dcr.ArmRequest
}

// triggerReply answers every command with the whole board, so the alerts view
// needs one round trip whatever it did: the list after the change, the
// catalogue for the form, and which recipients herdr knows about.
type triggerReply struct {
	Op     string `json:"op"`
	OK     bool   `json:"ok"`
	ID     string `json:"id,omitempty"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Warnings are reasons a trigger that was accepted is still not being
	// watched, such as monitoring being off.
	Warnings []string `json:"warnings,omitempty"`

	Monitoring bool              `json:"monitoring"`
	Active     string            `json:"active,omitempty"`
	Triggers   []dcr.TriggerView `json:"triggers"`
	Catalogue  []dcr.Leaf        `json:"catalogue,omitempty"`
	// Agents maps every herdr agent name and pane id to its status, so a row
	// can say whether its recipient is still there. Empty without herdr.
	Agents map[string]string `json:"agents,omitempty"`
}

// triggersOnce reads a single alerts command from stdin, applies it and prints
// the result. It is what the alerts view talks to, so arming works whether or
// not monitoring is on; only evaluation needs the running helper.
func triggersOnce(ctx context.Context, out *bufio.Writer) {
	reply := func(r *triggerReply) {
		if b, err := json.Marshal(r); err == nil {
			out.Write(b)
			out.WriteByte('\n')
			out.Flush()
		}
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 8*1024), 1<<20)
	if !sc.Scan() {
		reply(&triggerReply{Op: "triggers", Error: "error", Detail: "no command on stdin"})
		return
	}
	var cmd triggerCommand
	// Unknown keys are refused for the same reason the store refuses them: a
	// misspelt parameter would arm something other than what was meant.
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(sc.Text())))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cmd); err != nil {
		reply(&triggerReply{Op: "triggers", Error: "error", Detail: "unreadable command: " + err.Error()})
		return
	}
	reply(runTriggerCommand(ctx, cmd, time.Now()))
}

func runTriggerCommand(ctx context.Context, cmd triggerCommand, now time.Time) *triggerReply {
	r := &triggerReply{Op: cmd.Cmd, Monitoring: dcr.BoolSetting("monitoring", false)}
	fail := func(err error) *triggerReply {
		r.OK = false
		r.Error, r.Detail = dcr.Classify(err)
		if r.Error == "error" || r.Error == "" {
			r.Detail = err.Error()
		}
		return r
	}

	switch cmd.Cmd {
	case "list", "catalogue":
	case "arm":
		t, warnings, err := dcr.Arm(cmd.ArmRequest, now)
		if err != nil {
			return fail(err)
		}
		r.ID, r.Warnings = t.ID, warnings
	case "edit":
		if cmd.ID == "" {
			return fail(errors.New("edit needs the id of the trigger to change"))
		}
		t, warnings, err := dcr.EditTrigger(cmd.ID, cmd.ArmRequest, now)
		if err != nil {
			return fail(err)
		}
		r.ID, r.Warnings = t.ID, warnings
	case "disarm":
		if cmd.ID == "" {
			return fail(errors.New("disarm needs the id of the trigger to remove"))
		}
		if _, err := dcr.Disarm(cmd.ID); err != nil {
			return fail(err)
		}
		r.ID = cmd.ID
	default:
		return fail(fmt.Errorf("unknown command %q", cmd.Cmd))
	}

	list, err := dcr.LoadTriggers()
	if err != nil {
		return fail(err)
	}
	r.OK = true
	r.Active = activeConnectionID()
	r.Triggers = make([]dcr.TriggerView, 0, len(list.Triggers))
	for i := range list.Triggers {
		// Sampled is unknown from outside the helper; the running helper's
		// own snapshots are where no-sample is reported.
		r.Triggers = append(r.Triggers, list.Triggers[i].View(now, r.Active, true))
	}
	r.Catalogue = dcr.Catalogue()
	agentCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if agents, err := agentStates(agentCtx, execRunner); err == nil {
		r.Agents = agents
	}
	return r
}

// activeConnectionID is the connection the widget is watching, or "" when
// that cannot be told, in which case every trigger reads as on this one.
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
