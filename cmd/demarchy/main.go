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
	"sync"
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
	if err := sess.use(ctx, dcr.StringSetting("activeConnection", "")); err != nil {
		code, detail := dcr.Classify(err)
		fail(code, detail)
		return
	}

	if *once {
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		emit(sess.decorate(dcr.Fetch(callCtx, sess.client, sess.opt)))
		return
	}

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

	overrideEndpoint string
	overrideToken    string
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

	s.client = dcr.NewClient(endpoint, token)

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
	return snap
}

// validateConnection applies the same refusal the setup tool does: a widget may
// hold a token that can read, and nothing more.
func validateConnection(ctx context.Context, endpoint, token string) error {
	ep, err := dcr.NormaliseEndpoint(endpoint)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	caps, err := dcr.NewClient(ep, token).Capabilities(ctx)
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
	ringLen int
	primed  bool
}

// observe folds a fresh ring read into the counter and reports how many entries
// were appended since the previous read.
//
// The ring grows to its cap and then drops from the front, so a length increase
// gives the exact number appended. Once it is full the length stops moving, and
// a push we were woken by means at least one entry arrived: hence the floor of
// one. The first read only primes the baseline: entries already in the ring
// when the helper starts are history, not unread.
func (c *counter) observe(msgs []dcr.Message, countPrivate, countGroup bool) {
	n := len(msgs)
	if !c.primed {
		c.primed, c.ringLen = true, n
		return
	}
	appended := n - c.ringLen
	if appended <= 0 {
		appended = 1
	}
	if appended > n {
		appended = n
	}
	c.ringLen = n
	for _, m := range msgs[n-appended:] {
		switch {
		case m.IsGroup() && countGroup:
			c.group++
		case !m.IsGroup() && countPrivate:
			c.private++
		}
	}
}

func (c *counter) unread() dcr.Unread {
	return dcr.Unread{Private: c.private, Groupchat: c.group, Total: c.private + c.group}
}

func (c *counter) reset() { c.private, c.group = 0, 0 }

func watch(ctx context.Context, sess *session, emit func(*dcr.Snapshot)) {
	var count counter
	commands := readCommands(ctx)

	// Which resources changed since the last fetch. The unread counter may only
	// act on a chat-ring push: node/sync alone beats every 20 seconds, and
	// counting those as messages would make the badge climb on its own.
	var mu sync.Mutex
	pending := map[string]bool{}

	refresh := make(chan struct{}, 1)
	poke := func(uri string) {
		if uri != "" {
			mu.Lock()
			pending[uri] = true
			mu.Unlock()
		}
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	takePending := func() map[string]bool {
		mu.Lock()
		defer mu.Unlock()
		p := pending
		pending = map[string]bool{}
		return p
	}

	backoff := time.Second
	for ctx.Err() == nil {
		streamCtx, cancelStream := context.WithCancel(ctx)

		go func() {
			err := sess.client.Listen(streamCtx,
				subscriptions(sess.opt),
				func([]string) { poke("") },
				func(uri string) { poke(uri) },
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
				switch cmd.Cmd {
				case "markRead":
					count.reset()
					poke("")
				case "refresh":
					poke("")
				case "quit":
					cancelStream()
					return
				case "switch", "addConnection", "editConnection", "removeConnection":
					result := apply(ctx, sess, cmd)
					// The unread tally means "since you switched here", so a
					// change of connection starts it again.
					if cmd.Cmd == "switch" && result.OK {
						count = counter{}
					}
					callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
					snap := sess.decorate(dcr.Fetch(callCtx, sess.client, sess.opt))
					cancel()
					snap.Unread = count.unread()
					snap.ConnResult = result
					emit(snap)
					// A new connection needs a new stream: the old one is
					// bound to the previous server and its grant.
					if result.OK && (cmd.Cmd == "switch" || cmd.Cmd == "removeConnection") {
						cancelStream()
						break inner
					}
				}
			case <-refresh:
				changed := takePending()
				callCtx, cancel := context.WithTimeout(streamCtx, 30*time.Second)
				snap := sess.decorate(dcr.Fetch(callCtx, sess.client, sess.opt))
				cancel()
				if snap.Reachable {
					connected = true
					backoff = time.Second
					// Fold the ring in only when the ring is what moved, or
					// when the baseline has not been taken yet.
					if changed[dcr.ResBRMessages] || !count.primed {
						count.observe(snap.BR.Messages,
							dcr.BoolSetting("countPrivate", true),
							dcr.BoolSetting("countGroupchat", true))
					}
				}
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
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 60*time.Second {
			backoff *= 2
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
		if err := validateConnection(ctx, cmd.Endpoint, cmd.Token); err != nil {
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
		if err := validateConnection(ctx, endpoint, token); err != nil {
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
		}
	}
	if len(uris) == 0 {
		uris = append(uris, dcr.ResNodeSync)
	}
	return uris
}
