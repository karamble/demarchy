// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command demarchy feeds the Omarchy Demarchy widget.
//
// It holds one subscriptions/listen stream to a local dcrpulse and writes a
// JSON snapshot to stdout whenever something changes: one object per line,
// NDJSON. The QML side stays dumb on purpose: the Quickshell process is shared
// with the whole bar, so nothing that can block belongs in it.
//
// It exits 0 in almost every circumstance, reporting trouble as a snapshot with
// an "error" code rather than a non-zero status, so the panel can explain
// itself instead of showing an empty box.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

func main() {
	var (
		once      = flag.Bool("once", false, "print one snapshot and exit")
		endpoint  = flag.String("endpoint", "", "dcrpulse MCP endpoint (default: shell.json, else "+dcr.DefaultEndpoint+")")
		tokenFile = flag.String("token-file", "", "path to the bearer token file (default: ~/.config/demarchy/token)")
		balances  = flag.Bool("balances", false, "include wallet balances (default: the showBalances setting)")
		messages  = flag.Int("messages", 20, "chat ring entries to carry into the panel")
		force     = flag.Bool("force", false, "ignore the monitoring switch (debugging only)")
		apply1    = flag.Bool("apply", false, "read one JSON command from stdin, apply it, print the result and exit")
		triggers1 = flag.Bool("triggers", false, "read one JSON alerts command from stdin, apply it, print the result and exit")
		demo      = flag.Bool("demo", false, "emit a synthetic snapshot and exit; for building the panel, never used by the widget")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	emit := func(s *dcr.Snapshot) {
		b, err := json.Marshal(s)
		if err != nil {
			return
		}
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	fail := func(code, detail string) {
		emit(&dcr.Snapshot{V: dcr.SchemaVersion, Stamp: time.Now().Unix(), Error: code, Detail: detail})
	}

	// --demo short-circuits everything: no switch, no token, no network. It
	// exists so the populated layout can be built and screenshotted on a wallet
	// that has never staked.
	if *demo {
		emit(demoSnapshot())
		if *once {
			return
		}
		// Stay alive so the panel keeps the snapshot on screen, the same way a
		// real watch would.
		<-ctx.Done()
		return
	}

	// The monitoring switch is re-read here, independently of QML, before the
	// connection file is opened. A stale keybind or a QML regression therefore
	// still makes no request.
	if !*force && !dcr.BoolSetting("monitoring", false) {
		fail("off", "monitoring is switched off")
		return
	}

	// Connection changes run as a one-shot, not over the running helper's
	// stdin. The helper only runs while monitoring is on, so routing add and
	// edit through it meant the settings page silently did nothing whenever it
	// was off, which is exactly when someone is setting their first connection
	// up. Adding a connection contacts dcrpulse to validate the token, but the
	// person asking for it is the consent the switch exists to require.
	if *apply1 {
		applyOnce(ctx, out)
		return
	}
	// The alerts board is managed the same way, for the same reason: an agent
	// arms a trigger, or the panel edits one, whether or not monitoring is on.
	// Evaluating them needs the running helper; listing and arming do not.
	if *triggers1 {
		triggersOnce(ctx, out)
		return
	}

	list, err := dcr.LoadConnections()
	if err != nil {
		code, detail := dcr.Classify(err)
		fail(code, detail)
		return
	}
	sess := &session{
		list: list,
		opt: dcr.FetchOptions{
			Balances: *balances || dcr.BoolSetting("showBalances", false),
			Messages: *messages,
		},
		// Command-line overrides exist for debugging a connection that is not
		// in the list; they win over whatever is stored.
		overrideEndpoint: *endpoint,
		overrideToken:    *tokenFile,
	}
	// Installed before the first client, which reads it on every request.
	sess.setPolicy(list.Policy())
	// A connection that fails at startup is reported and then retried, not a
	// reason to quit. Exiting here left the panel with nothing: the process was
	// gone before its one line could be read, so a rejected token looked
	// exactly like "still connecting", and it could never recover on its own
	// once the token was fixed.
	if err := sess.use(ctx, dcr.StringSetting("activeConnection", "")); err != nil {
		code, detail := dcr.Classify(err)
		if *once || sess.client == nil {
			fail(code, detail)
			return
		}
	}

	if *once {
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		emit(sess.decorate(dcr.Fetch(callCtx, sess.client, sess.opt)))
		return
	}

	// Alarms ring only from the long-running watch: a one-shot has no loop to
	// take a delivery result on.
	sess.alarms = newAlarms(newHerdrRinger())
	watch(ctx, sess, emit)
}

// session is the currently selected connection and everything derived from it.
// Switching rebuilds this in place rather than restarting the process, so the
// panel keeps its stream lifecycle and the user sees the new connection in the
// time one capabilities call takes.
type session struct {
	list   *dcr.Connections
	conn   *dcr.Connection
	client *dcr.Client
	opt    dcr.FetchOptions

	// policy is handed to the client as a function rather than a value, so a
	// range added or removed on disk takes effect on the requests already in
	// flight instead of waiting for something to rebuild the client.
	policy atomic.Pointer[dcr.Policy]
	// built is what the live client was made from. When the file no longer
	// agrees with it, the connection is rebuilt.
	built string
	// loadErr is the last failure to read connections.json. While it is set the
	// list in memory is stale, so no write verb may run.
	loadErr error

	overrideEndpoint string
	overrideToken    string

	// alarms is the trigger board; nil in the one-shots, which never evaluate.
	alarms *alarms
}

// connID is the id triggers are bound to, or "" before a connection is chosen.
func (s *session) connID() string {
	if s.conn == nil {
		return ""
	}
	return s.conn.ID
}

// livePolicy is what the client consults per request and per dial.
func (s *session) livePolicy() dcr.Policy {
	if p := s.policy.Load(); p != nil {
		return *p
	}
	return dcr.Policy{}
}

// setPolicy installs a policy, or the default rule when the file is unreadable.
// A broken file fails closed for exactly the traffic it governs: https and
// loopback connections carry on working.
func (s *session) setPolicy(p dcr.Policy) { s.policy.Store(&p) }

// fingerprint is what a rebuild is decided on: the endpoint, the token and the
// policy in force. Any of the three moving means the live client is wrong.
func (s *session) fingerprint() string {
	if s.conn == nil {
		return ""
	}
	p := s.livePolicy()
	return s.conn.Endpoint + "\x00" + s.conn.Token + "\x00" +
		p.Interface() + "\x00" + strings.Join(p.Networks(), ",")
}

// resync re-reads the file and rebuilds the connection when anything it was
// built from has changed.
//
// Three separate failures share this one fix. Connection edits are applied by a
// one-shot process, so the running helper never saw them. A range removed from
// the policy left the old client sending plain text anyway. And a range added
// could never rescue a refused connection, because the switcher declines to
// switch to the id that is already active, so no gesture in the panel could
// rebuild it.
func (s *session) resync(ctx context.Context) bool {
	// Triggers armed or disarmed by the CLI are picked up here too, on the
	// same cadence, without ever restarting the stream.
	if s.alarms != nil {
		s.alarms.sync()
	}
	fresh, err := dcr.LoadConnections()
	if err != nil {
		s.loadErr = err
		s.setPolicy(dcr.Policy{})
		return false
	}
	s.loadErr = nil
	s.list = fresh
	s.setPolicy(fresh.Policy())

	if s.overrideEndpoint != "" || s.conn == nil {
		return false
	}
	if conn, ok := fresh.Find(s.conn.ID); ok {
		s.conn = conn
	}
	if s.fingerprint() == s.built {
		return false
	}
	id := s.conn.ID
	if err := s.use(ctx, id); err != nil {
		code, detail := dcr.Classify(err)
		fmt.Fprintln(os.Stderr, "resync:", code, detail)
	}
	return true
}

// use selects a connection by id and reads its grant. An unknown id falls back
// to the first configured connection, so a stale activeConnection setting
// degrades to something working.
func (s *session) use(ctx context.Context, id string) error {
	endpoint, token := "", ""

	switch {
	case s.overrideEndpoint != "" || s.overrideToken != "":
		// Debugging path: --endpoint / --token-file bypass the list entirely.
		endpoint = s.overrideEndpoint
		if endpoint == "" {
			endpoint = dcr.DefaultEndpoint
		}
		t, err := dcr.ReadToken(s.overrideToken)
		if err != nil {
			return err
		}
		token = t
		s.conn = &dcr.Connection{ID: "override", Name: "override", Endpoint: endpoint}
	default:
		conn, err := s.list.Active(id)
		if err != nil {
			return err
		}
		s.conn, endpoint, token = conn, conn.Endpoint, conn.Token
	}

	s.client = dcr.NewClientWithPolicy(endpoint, token, s.livePolicy)
	s.built = s.fingerprint()

	// Ask what this token may read, and fetch only that. The grant is the
	// configuration: a section the token cannot see is a section the panel
	// never draws, with nothing to switch off by hand.
	capsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	caps, err := s.client.Capabilities(capsCtx)
	cancel()
	if err != nil {
		// Keep the client so the watch loop can report the failure against this
		// connection and retry it, rather than dropping to no connection at all.
		s.opt.Domains = nil
		return err
	}
	s.opt.Domains = caps.Domains
	return nil
}

// decorate attaches the connection list and the active id to every snapshot.
// This is how the panel learns what to put in the switcher without ever reading
// the file the tokens live in.
func (s *session) decorate(snap *dcr.Snapshot) *dcr.Snapshot {
	if s.list != nil {
		snap.Connections = s.list.Public()
	}
	if s.conn != nil {
		snap.Active = s.conn.ID
	}
	snap.PlainHTTP = s.livePolicy().Public()
	if s.loadErr != nil {
		snap.ConfigError = s.loadErr.Error()
	}
	if s.alarms != nil {
		snap.Triggers, snap.TriggersError = s.alarms.views(time.Now(), snap.Active)
	}
	return snap
}

// validateConnection applies the same refusal the setup tool does: a widget may
// hold a token that can read, and nothing more.
func validateConnection(ctx context.Context, endpoint, token string, policy func() dcr.Policy) error {
	if policy == nil {
		policy = func() dcr.Policy { return dcr.Policy{} }
	}
	ep, err := dcr.NormaliseEndpointWith(endpoint, policy())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	caps, err := dcr.NewClientWithPolicy(ep, token, policy).Capabilities(ctx)
	if err != nil {
		return err
	}
	if why := caps.CheckReadOnly(); why != nil {
		return fmt.Errorf("refusing this token because %w: a bar widget "+
			"should not be able to move funds or send messages", why)
	}
	return nil
}

// counter tracks unread since the panel last acknowledged. The chat ring
// carries no ids, no timestamps and no read flag, so this is the only place the
// count can live.
type counter struct {
	private int
	group   int
	tail    string
	primed  bool
}

// tag identifies a ring entry. The ring carries no ids and no timestamps, so
// the sender, the text and the group are all there is to go on. Two identical
// lines running together are indistinguishable, which can only ever undercount.
func tag(m dcr.Message) string {
	return m.Type + "\x00" + m.FromNick + "\x00" + m.GCID + "\x00" + m.Text
}

// observe folds a fresh ring read into the counter and credits everything that
// arrived after the last entry it saw.
//
// This runs on every fetch rather than on a chat push, because dcrpulse does
// not push the chat ring. Subscribing to it yields nothing: node/sync and
// wallet/balance are what beat, and those beats are what bring a fresh ring
// along with them. Counting by content rather than by length is what makes
// running on every fetch safe. A beat carrying no new messages finds its own
// tail at the end and credits nothing, which is the failure that once made the
// badge climb on its own, and a ring that has filled up and started dropping
// from the front still shows exactly which entries are new.
func (c *counter) observe(msgs []dcr.Message, countPrivate, countGroup bool) {
	n := len(msgs)
	if n == 0 {
		return
	}
	if !c.primed {
		// Whatever is in the ring at startup is history, not unread.
		c.primed, c.tail = true, tag(msgs[n-1])
		return
	}
	if c.tail == tag(msgs[n-1]) {
		return
	}

	// Everything after the entry last seen is new. If that entry has dropped
	// off the front, the whole ring is new to us.
	fresh := msgs
	for i := n - 1; i >= 0; i-- {
		if tag(msgs[i]) == c.tail {
			fresh = msgs[i+1:]
			break
		}
	}
	c.tail = tag(msgs[n-1])
	for _, m := range fresh {
		switch {
		case m.IsGroup() && countGroup:
			c.group++
		case !m.IsGroup() && countPrivate:
			c.private++
		}
	}
}

// fold reads the chat ring out of a snapshot, if the snapshot has one.
//
// BR is absent when the token lacks the bisonrelay domain, and Reachable says
// nothing about which sections came back. A node-only token used to crash the
// helper on its first good fetch, right here.
func (c *counter) fold(snap *dcr.Snapshot) {
	if snap == nil || snap.BR == nil {
		return
	}
	c.observe(snap.BR.Messages,
		dcr.BoolSetting("countPrivate", true),
		dcr.BoolSetting("countGroupchat", true))
}

func (c *counter) unread() dcr.Unread {
	return dcr.Unread{Private: c.private, Groupchat: c.group, Total: c.private + c.group}
}

func (c *counter) reset() { c.private, c.group = 0, 0 }

func watch(ctx context.Context, sess *session, emit func(*dcr.Snapshot)) {
	var count counter
	commands := readCommands(ctx)

	alarms := sess.alarms
	if alarms == nil {
		alarms = newAlarms(newHerdrRinger())
		sess.alarms = alarms
	}
	alarms.reload()
	// A fire whose delivery never came back is rung again before anything
	// else: the alarm happened, and losing it to a crash is the one failure
	// this whole board exists to rule out.
	alarms.recover(ctx)
	defer alarms.save(true)

	refresh := make(chan struct{}, 1)
	poke := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}

	// Command handling has to be reachable from two places: the stream loop
	// below and the backoff wait after a failure. A switch issued because the
	// current connection is failing is exactly the moment it matters most, and
	// a sleep that ignored commands left the panel pinned to a dead connection
	// until the backoff happened to expire.
	handleCommand := func(cmd command) (restart, quit bool) {
		// The file may have moved under us: connection changes are applied by a
		// separate one-shot process, and the policy is edited by the wizard. A
		// rebuilt connection restarts the stream, which is what the caller does
		// with the restart return.
		rebuilt := sess.resync(ctx)
		switch cmd.Cmd {
		case "markRead":
			count.reset()
			poke()
		case "refresh":
			poke()
			if rebuilt {
				return true, false
			}
		case "quit":
			return false, true
		case "switch", "addConnection", "editConnection", "removeConnection":
			if sess.loadErr != nil && cmd.Cmd != "switch" {
				// Writing now would put the stale list in memory back over
				// whatever the operator is in the middle of fixing.
				code, detail := dcr.Classify(sess.loadErr)
				snap := sess.decorate(&dcr.Snapshot{V: dcr.SchemaVersion})
				snap.ConnResult = &dcr.ConnResult{Op: cmd.Cmd, Error: code, Detail: detail}
				emit(snap)
				return false, false
			}
			result := apply(ctx, sess, cmd)
			// The unread tally means "since you switched here", so a change of
			// connection starts it again.
			if cmd.Cmd == "switch" && result.OK {
				count = counter{}
			}
			callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			snap := dcr.Fetch(callCtx, sess.client, sess.opt)
			cancel()
			// Triggers are bound to a connection, so folding a snapshot from
			// a fresh switch through them evaluates only the ones armed there.
			alarms.observe(ctx, snap, sess.connID())
			sess.decorate(snap)
			snap.Unread = count.unread()
			snap.ConnResult = result
			emit(snap)
			// A new connection needs a new stream: the old one is bound to the
			// previous server and its grant.
			if result.OK && (cmd.Cmd == "switch" || cmd.Cmd == "removeConnection") {
				return true, false
			}
		}
		return false, false
	}

	backoff := time.Second
	for ctx.Err() == nil {
		// Every time round, so a policy or connection change made while the
		// panel was closed is picked up without a command arriving. This is
		// also what heals a connection refused for plain text once the range
		// allowing it is restored: nothing in the panel can rebuild the client
		// for the connection that is already active.
		sess.resync(ctx)
		streamCtx, cancelStream := context.WithCancel(ctx)

		go func() {
			err := sess.client.Listen(streamCtx,
				subscriptions(sess.opt),
				func([]string) { poke() },
				func(string) { poke() },
			)
			if err != nil && streamCtx.Err() == nil {
				fmt.Fprintln(os.Stderr, "listen:", err)
			}
			cancelStream()
		}()

		connected := false
	inner:
		for {
			select {
			case <-ctx.Done():
				cancelStream()
				return
			case <-streamCtx.Done():
				break inner
			case cmd := <-commands:
				restart, quit := handleCommand(cmd)
				if quit {
					cancelStream()
					return
				}
				if restart {
					cancelStream()
					break inner
				}
			case r := <-alarms.results:
				alarms.settle(r)
				// So the panel sees "delivered" without waiting for a beat.
				poke()
			case <-refresh:
				callCtx, cancel := context.WithTimeout(streamCtx, 30*time.Second)
				snap := dcr.Fetch(callCtx, sess.client, sess.opt)
				cancel()
				if snap.Reachable {
					connected = true
					backoff = time.Second
					count.fold(snap)
					// After the unread fold and before decorate, so the
					// snapshot that goes out carries the status this pass
					// produced. Deliveries run with the watch's own context,
					// not the stream's: a reconnect must not cancel a ring.
					alarms.observe(ctx, snap, sess.connID())
				}
				sess.decorate(snap)
				snap.Unread = count.unread()
				emit(snap)
				if !snap.Reachable {
					cancelStream()
					break inner
				}
			}
		}
		cancelStream()

		if ctx.Err() != nil {
			return
		}
		if !connected {
			// Report the failure so the panel can say why, then back off.
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			snap := sess.decorate(dcr.Fetch(callCtx, sess.client, sess.opt))
			cancel()
			snap.Unread = count.unread()
			emit(snap)
		}
		timer := time.NewTimer(backoff)
		for waiting := true; waiting; {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case r := <-alarms.results:
				alarms.settle(r)
			case cmd := <-commands:
				restart, quit := handleCommand(cmd)
				if quit {
					timer.Stop()
					return
				}
				if restart {
					// A new connection deserves a fresh start, not the backoff
					// the old one had earned.
					timer.Stop()
					backoff = time.Second
					waiting = false
				}
			case <-timer.C:
				waiting = false
				if backoff < 60*time.Second {
					backoff *= 2
				}
			}
		}
	}
}

// command is one instruction from the panel.
//
// Anything carrying a token arrives as JSON rather than as whitespace-separated
// words: a bearer token is opaque and splitting on spaces would corrupt it. The
// bare verbs predate this and still work.
type command struct {
	Cmd      string `json:"cmd"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Token    string `json:"token,omitempty"`
}

// readCommands turns stdin lines into commands. The panel writes "markRead"
// when it opens, which is what returns the badge to zero.
//
// This is also the path a token takes from the settings form to disk: a pipe,
// so it never reaches argv or the environment where any other process could
// read it.
func readCommands(ctx context.Context) <-chan command {
	ch := make(chan command, 4)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(os.Stdin)
		// A token makes a line longer than the scanner's default.
		sc.Buffer(make([]byte, 0, 8*1024), 1<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var cmd command
			if strings.HasPrefix(line, "{") {
				if json.Unmarshal([]byte(line), &cmd) != nil || cmd.Cmd == "" {
					continue
				}
			} else {
				cmd = command{Cmd: line}
			}
			select {
			case ch <- cmd:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// apply runs a connection command and reports what happened, so the settings
// form can show a reason rather than silently doing nothing.
//
// Every add and edit is validated against the live server before it is written:
// the same read-only refusal the setup tool applies, so a token that can spend
// is rejected from the panel too.
func apply(ctx context.Context, sess *session, cmd command) *dcr.ConnResult {
	res := &dcr.ConnResult{Op: cmd.Cmd, ID: cmd.ID}
	fail := func(err error) *dcr.ConnResult {
		res.OK = false
		res.Error, res.Detail = dcr.Classify(err)
		if res.Error == "error" || res.Error == "" {
			res.Detail = err.Error()
		}
		return res
	}

	switch cmd.Cmd {
	case "switch":
		if err := sess.use(ctx, cmd.ID); err != nil {
			// Still count as switched: the panel should show the new
			// connection failing, not stay silently on the old one.
			res.OK = true
			res.ID = cmd.ID
			res.Error, res.Detail = dcr.Classify(err)
			return res
		}
		res.OK = true
		res.ID = sess.conn.ID

	case "addConnection":
		if err := validateConnection(ctx, cmd.Endpoint, cmd.Token, sess.list.Policy); err != nil {
			return fail(err)
		}
		conn, err := sess.list.Add(cmd.Name, cmd.Endpoint, cmd.Token)
		if err != nil {
			return fail(err)
		}
		if err := dcr.SaveConnections(sess.list); err != nil {
			return fail(err)
		}
		res.OK, res.ID = true, conn.ID

	case "editConnection":
		existing, ok := sess.list.Find(cmd.ID)
		if !ok {
			return fail(dcr.ErrConnNotFound)
		}
		// An empty token means "keep the stored one", so validate against
		// whichever token this connection will actually end up using.
		token := cmd.Token
		if strings.TrimSpace(token) == "" {
			token = existing.Token
		}
		endpoint := cmd.Endpoint
		if strings.TrimSpace(endpoint) == "" {
			endpoint = existing.Endpoint
		}
		if err := validateConnection(ctx, endpoint, token, sess.list.Policy); err != nil {
			return fail(err)
		}
		if _, err := sess.list.Edit(cmd.ID, cmd.Name, cmd.Endpoint, cmd.Token); err != nil {
			return fail(err)
		}
		if err := dcr.SaveConnections(sess.list); err != nil {
			return fail(err)
		}
		res.OK = true
		// Editing the live connection changes where we are pointed.
		if sess.conn != nil && sess.conn.ID == cmd.ID {
			_ = sess.use(ctx, cmd.ID)
		}

	case "removeConnection":
		if err := sess.list.Remove(cmd.ID); err != nil {
			return fail(err)
		}
		if err := dcr.SaveConnections(sess.list); err != nil {
			return fail(err)
		}
		res.OK = true
		// Removing the live connection has to land somewhere; Active falls
		// back to the first remaining one.
		if sess.conn != nil && sess.conn.ID == cmd.ID {
			if err := sess.use(ctx, ""); err != nil {
				res.Error, res.Detail = dcr.Classify(err)
			}
		}
	}
	return res
}

// subscriptions lists the resource feeds this token may watch. Subscribing to a
// URI outside the grant is refused, so asking for one would cost the whole
// stream.
func subscriptions(opt dcr.FetchOptions) []string {
	var uris []string
	if opt.Domains == nil {
		return []string{dcr.ResNodeSync}
	}
	for _, d := range opt.Domains {
		switch d {
		case "node":
			uris = append(uris, dcr.ResNodeSync)
		case "staking":
			uris = append(uris, dcr.ResStaking)
		case "bisonrelay":
			uris = append(uris, dcr.ResBRMessages)
		case "wallet":
			uris = append(uris, dcr.ResWalletBal)
		case "lightning":
			uris = append(uris, dcr.ResLightning)
		case "audit":
			uris = append(uris, dcr.ResAudit)
		case "brmcp":
			uris = append(uris, dcr.ResBRMCP)
		}
	}
	if len(uris) == 0 {
		uris = append(uris, dcr.ResNodeSync)
	}
	return uris
}

// applyOnce reads a single command from stdin, applies it and prints the
// result. It is what the settings page talks to, so managing connections works
// whether or not monitoring is on.
func applyOnce(ctx context.Context, out *bufio.Writer) {
	reply := func(res *dcr.ConnResult) {
		if b, err := json.Marshal(res); err == nil {
			out.Write(b)
			out.WriteByte('\n')
			out.Flush()
		}
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 8*1024), 1<<20)
	if !sc.Scan() {
		reply(&dcr.ConnResult{Op: "apply", Error: "error", Detail: "no command on stdin"})
		return
	}
	var cmd command
	if err := json.Unmarshal([]byte(strings.TrimSpace(sc.Text())), &cmd); err != nil {
		reply(&dcr.ConnResult{Op: "apply", Error: "error", Detail: "unreadable command"})
		return
	}

	list, err := dcr.LoadConnections()
	if err != nil {
		code, detail := dcr.Classify(err)
		reply(&dcr.ConnResult{Op: cmd.Cmd, Error: code, Detail: detail})
		return
	}
	sess := &session{list: list}
	// The one-shot builds a client too, for the capability check and for a
	// switch, and a client with no policy refuses every plain-http endpoint the
	// file allows. The running helper installs this at startup; this path has
	// to do the same.
	sess.setPolicy(list.Policy())
	reply(apply(ctx, sess, cmd))
}
