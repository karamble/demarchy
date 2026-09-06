// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is bumped when the NDJSON shape changes incompatibly. The
// panel refuses a line it does not recognise rather than drawing nonsense.
const SchemaVersion = 2

// atomsPerDCR converts the integer amounts the Lightning RPCs report.
const atomsPerDCR = 1e8

// ticketWindow is mainnet's stake difficulty window: the ticket price is
// recalculated every 144 blocks. dcrpulse exposes no parameter for it, so it is
// stated here rather than guessed from observed price changes.
const ticketWindow = 144

// targetBlockSeconds is Decred's target spacing. Observed intervals are far too
// noisy to average over ten blocks: they range from seconds to twenty minutes,
// so the countdown is quoted against the target and marked approximate.
const targetBlockSeconds = 300

// Snapshot is exactly one line of the helper's NDJSON output. Every section is
// a pointer: a section the token cannot read is absent, and the panel draws
// only what it is given.
type Snapshot struct {
	V         int      `json:"v"`
	Stamp     int64    `json:"stamp"`
	Reachable bool     `json:"reachable"`
	Error     string   `json:"error,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Domains   []string `json:"domains,omitempty"`

	// The connection list and which one is live, so the panel can draw the
	// switcher. Tokens are stripped: see Connections.Public.
	Connections []PublicConnection `json:"connections,omitempty"`
	// PlainHTTP is the exception in force, so the panel can say plainly that
	// one exists. Never editable there; the note is not carried.
	PlainHTTP *PlainHTTPPublic `json:"plainHttp,omitempty"`
	// ConfigError is set when connections.json cannot be read, which the panel
	// shows instead of pretending the list is empty.
	ConfigError string `json:"configError,omitempty"`
	Active      string `json:"activeConnection,omitempty"`
	// ConnResult reports the outcome of the last add/edit/remove, so a refused
	// token shows a reason instead of the form silently doing nothing.
	ConnResult *ConnResult `json:"connResult,omitempty"`
	Node       *Node       `json:"node,omitempty"`
	Staking    *Staking    `json:"staking,omitempty"`
	Wallet     *Wallet     `json:"wallet,omitempty"`
	Lightning  *Lightning  `json:"lightning,omitempty"`
	Price      *Price      `json:"price,omitempty"`
	Dex        *Dex        `json:"dex,omitempty"`
	Treasury   *Treasury   `json:"treasury,omitempty"`
	BR         *BR         `json:"br,omitempty"`
	Unread     Unread      `json:"unread"`
	// Triggers is the alerts board as the panel sees it: definitions and a
	// status each, never a sampled value. TriggersError says why the store
	// could not be read, so the view shows the reason instead of an empty list.
	Triggers      []TriggerView `json:"triggers,omitempty"`
	TriggersError string        `json:"triggersError,omitempty"`
}

// ConnResult is the answer to a connection command from the panel.
type ConnResult struct {
	Op     string `json:"op"`
	OK     bool   `json:"ok"`
	ID     string `json:"id,omitempty"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Node struct {
	Status       string  `json:"status"`
	SyncProgress float64 `json:"syncProgress"`
	SyncPhase    string  `json:"syncPhase"`
	SyncMessage  string  `json:"syncMessage"`
	Version      string  `json:"version"`
	Height       int64   `json:"height"`
	Peers        int     `json:"peers"`
}

type Staking struct {
	TicketPrice     float64 `json:"ticketPrice"`
	NextTicketPrice float64 `json:"nextTicketPrice"`
	EstimatedMin    float64 `json:"estimatedMin"`
	EstimatedMax    float64 `json:"estimatedMax"`
	PoolSize        int     `json:"poolSize"`
	Participation   float64 `json:"participation"`
	LockedDCR       float64 `json:"lockedDcr"`
	Circulating     float64 `json:"circulating"`

	// BlocksToChange counts down to the next stake difficulty recalculation;
	// HoursToChange is that in hours at the target block time, so it is an
	// estimate and the panel says so.
	BlocksToChange int     `json:"blocksToChange"`
	HoursToChange  float64 `json:"hoursToChange"`
	WindowProgress float64 `json:"windowProgress"`

	Own OwnStaking `json:"own"`
}

// OwnStaking is this wallet's position and history, derived from the ticket
// list rather than the summary counters, because only the list carries vote
// timestamps and per-ticket rewards.
type OwnStaking struct {
	Live     int `json:"live"`
	Immature int `json:"immature"`
	Unmined  int `json:"unmined"`
	Voted    int `json:"voted"`
	Missed   int `json:"missed"`
	Revoked  int `json:"revoked"`

	// Reward is everything this wallet has earned from voting, all time.
	Reward float64 `json:"reward"`
	// LastVote is the unix time of the most recent vote, 0 if never.
	LastVote int64 `json:"lastVote"`
	// VoteBuckets counts votes per week over the recent past, oldest first.
	//
	// This replaced a cumulative-rewards line, which was the wrong shape for
	// the data: per-vote rewards barely differ, so the curve was a straight
	// rise that said nothing you could not read off the total. Votes per week
	// shows the thing a staker actually wants to see: whether tickets are
	// still voting, and where the quiet stretches were.
	VoteBuckets []int `json:"voteBuckets,omitempty"`
	// BucketDays is how wide one bucket is, so the panel can label the axis.
	BucketDays int `json:"bucketDays,omitempty"`
	// PoolShare is live tickets as a fraction of the whole pool, in percent.
	PoolShare float64 `json:"poolShare"`
	// VSP is the voting service provider most of the tickets use.
	VSP string `json:"vsp,omitempty"`
	// HasHistory distinguishes "never staked" from "staked, nothing live".
	HasHistory bool `json:"hasHistory"`
}

type Wallet struct {
	Status    string  `json:"status"`
	Synced    bool    `json:"synced"`
	Total     float64 `json:"total"`
	Spendable float64 `json:"spendable"`
	Locked    float64 `json:"lockedByTickets"`
}

// Lightning is the channel picture: outbound is what this node can send,
// inbound what it can receive.
type Lightning struct {
	Channels int     `json:"channels"`
	Peers    int     `json:"peers"`
	Outbound float64 `json:"outbound"`
	Inbound  float64 `json:"inbound"`
	OnChain  float64 `json:"onChain"`
	Pending  float64 `json:"pending"`
	Synced   bool    `json:"synced"`

	// List is the individual channels, largest first. The totals above say how
	// much liquidity there is; this says how it is spread, which is the part
	// that decides whether a given payment will actually go through.
	List []LnChannel `json:"list,omitempty"`
}

// LnChannel is one channel's share of the picture.
type LnChannel struct {
	Alias    string  `json:"alias" trig:"id"`
	Capacity float64 `json:"capacity"`
	Local    float64 `json:"local"`
	Remote   float64 `json:"remote"`
	Active   bool    `json:"active"`
}

type Price struct {
	DcrUsd  float64 `json:"dcrUsd"`
	BtcUsd  float64 `json:"btcUsd"`
	Sats    float64 `json:"sats"`
	Source  string  `json:"source,omitempty"`
	Updated string  `json:"updated,omitempty"`

	// Series is the DCR/BTC close of each candle, oldest first, and it only
	// exists when a DEX server is registered: there is no price-history
	// endpoint otherwise, and this widget does not invent one by sampling.
	//
	// The values are raw message-rates, not converted to a conventional rate:
	// the conversion needs a per-market factor, and the chart only needs the
	// shape. Change is a ratio, so it is correct either way, and the absolute
	// price above comes from the rate feed.
	Series []float64 `json:"series,omitempty"`
	Change float64   `json:"change,omitempty"`
	Market string    `json:"market,omitempty"`
}

// Dex is the DCRDEX DCR/BTC market as the client sees it: the one price a
// trade actually clears at, beside the exchange feed in Price. Present only
// with the dex grant, a registered server and a dcr_btc market; otherwise nil,
// which the alerts read as no sample rather than as zeros.
type Dex struct {
	Host   string `json:"host"`
	Market string `json:"market"`
	// Rate is the last trade in sats per DCR, the unit Price.Sats uses, so the
	// two read side by side. High24 and Low24 are in the same unit.
	Rate     int64   `json:"rate"`
	RateUsd  float64 `json:"rateUsd"`
	Change24 float64 `json:"change24"` // percent over 24h
	High24   int64   `json:"high24"`
	Low24    int64   `json:"low24"`
	Volume24 float64 `json:"volume24"` // DCR traded in 24h
	// Premium is the DEX rate against the exchange feed's implied rate, in
	// percent: how much more, or less, a DCR fetches on the DEX right now.
	// Zero while the exchange feed is unavailable.
	Premium float64 `json:"premium"`
	Updated string  `json:"updated"` // RFC 3339, the last trade
}

// Treasury carries only the USD sum; the DCR balance is what produces it.
type Treasury struct {
	BalanceDCR float64 `json:"balanceDcr"`
	BalanceUsd float64 `json:"balanceUsd"`
}

type BR struct {
	Stage    string    `json:"stage"`
	Nick     string    `json:"nick"`
	Notices  []Notice  `json:"notices"`
	Messages []Message `json:"messages"`
}

// Notice is one entry of brclientd's persisted daemon-note bell list. These are
// connection and housekeeping events: NOT chat, which is why the unread badge
// is not built on them.
type Notice struct {
	ID       int64  `json:"id" trig:"id"`
	TS       string `json:"ts"`
	Severity string `json:"severity"`
	Subject  string `json:"subject"`
	Detail   string `json:"detail"`
}

// Message is one entry of the in-memory chat ring. It carries no id, no
// timestamp and no read flag, which is why unread is counted by this helper
// rather than read from the server.
type Message struct {
	Type     string `json:"type" trig:"id"`
	FromNick string `json:"fromNick" trig:"id"`
	Text     string `json:"text" trig:"id"`
	GCID     string `json:"gcid,omitempty" trig:"id"`
	GCName   string `json:"gcName,omitempty"`
}

// IsGroup reports whether a ring entry is group traffic.
func (m Message) IsGroup() bool { return m.Type == "gcm" || m.Type == "gc-message" }

type Unread struct {
	Private   int `json:"private"`
	Groupchat int `json:"groupchat"`
	Total     int `json:"total"`
}

// FetchOptions selects the parts of a snapshot the user has asked for. What the
// token is allowed to read is decided separately, by the grant.
type FetchOptions struct {
	// Balances fetches wallet_dashboard. Off by default: it is the only call
	// that returns money, and the panel hides those rows unless asked.
	Balances bool
	// Messages caps how many ring entries are carried into the panel.
	Messages int
	// Domains is the token's grant. An empty list means "try everything",
	// which is only used before capabilities has been read.
	Domains []string
}

func (o FetchOptions) allows(domain string) bool {
	if len(o.Domains) == 0 {
		return true
	}
	for _, d := range o.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

// Classify turns a transport or protocol failure into a stable code the panel
// can branch on, plus a human detail. Codes: no-token, auth, insecure,
// unreachable, error.
//
// The identity checks come first on purpose. These errors carry text a
// substring rule would misread: a refusal naming an endpoint contains "dial",
// and a config error can quote a value containing "EOF", either of which would
// otherwise be reported to the panel as "unreachable".
func Classify(err error) (string, string) {
	switch {
	case err == nil:
		return "", ""
	case errors.Is(err, ErrNoToken):
		return "no-token", "no token configured; run demarchy-setup"
	case errors.Is(err, ErrUnauthorized):
		return "auth", "dcrpulse rejected the token"
	case errors.Is(err, ErrPlaintextRefused):
		return "insecure", err.Error()
	case errors.Is(err, ErrMeshDown):
		return "unreachable", err.Error()
	case errors.Is(err, ErrConfig):
		return "error", err.Error()
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return "unreachable", err.Error()
	}
	msg := err.Error()
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF") || strings.Contains(msg, "dial ") {
		return "unreachable", msg
	}
	return "error", msg
}

// Fetch builds one snapshot. Sections the grant does not cover are skipped
// entirely, and an individual failure costs one section rather than the panel.
// Only node_status decides reachability.
func Fetch(ctx context.Context, c *Client, opt FetchOptions) *Snapshot {
	snap := &Snapshot{V: SchemaVersion, Stamp: time.Now().Unix(), Domains: opt.Domains}

	raw, err := c.Call(ctx, "node_status", nil)
	if err != nil {
		snap.Error, snap.Detail = Classify(err)
		return snap
	}
	snap.Reachable = true

	var ns struct {
		Status       string  `json:"status"`
		SyncProgress float64 `json:"syncProgress"`
		SyncPhase    string  `json:"syncPhase"`
		SyncMessage  string  `json:"syncMessage"`
		Version      string  `json:"version"`
	}
	_ = json.Unmarshal(raw, &ns)
	node := &Node{
		Status:       ns.Status,
		SyncProgress: ns.SyncProgress,
		SyncPhase:    ns.SyncPhase,
		SyncMessage:  ns.SyncMessage,
		Version:      ns.Version,
	}
	if raw, err := c.Call(ctx, "node_peers", nil); err == nil {
		var peers []struct{}
		if json.Unmarshal(raw, &peers) == nil {
			node.Peers = len(peers)
		}
	}
	snap.Node = node

	if opt.allows("staking") || opt.allows("node") {
		snap.Staking = fetchStaking(ctx, c, snap.Node, opt)
	}
	if opt.Balances && opt.allows("wallet") {
		snap.Wallet = fetchWallet(ctx, c)
	}
	if opt.allows("lightning") {
		snap.Lightning = fetchLightning(ctx, c)
	}
	if opt.allows("bisonrelay") {
		snap.Price = fetchPrice(ctx, c)
		snap.BR = fetchBR(ctx, c, opt.Messages)
	}
	if opt.allows("dex") {
		snap.Dex = fetchDex(ctx, c, snap.Price)
	}
	if opt.allows("treasury") {
		snap.Treasury = fetchTreasury(ctx, c, snap.Price)
	}
	if opt.allows("dex") && snap.Price != nil {
		attachCandles(ctx, c, snap.Price)
	}
	return snap
}

func fetchStaking(ctx context.Context, c *Client, node *Node, opt FetchOptions) *Staking {
	st := &Staking{}
	if raw, err := c.Call(ctx, "node_staking_overview", nil); err == nil {
		var so struct {
			TicketPrice     float64 `json:"ticketPrice"`
			NextTicketPrice float64 `json:"nextTicketPrice"`
			PoolSize        int     `json:"poolSize"`
			Participation   float64 `json:"participationRate"`
			LockedDCR       float64 `json:"lockedDCR"`
		}
		if json.Unmarshal(raw, &so) == nil {
			st.TicketPrice = so.TicketPrice
			st.NextTicketPrice = so.NextTicketPrice
			st.PoolSize = so.PoolSize
			st.Participation = so.Participation
			st.LockedDCR = so.LockedDCR
		}
	}
	if raw, err := c.Call(ctx, "node_supply", nil); err == nil {
		var sup struct {
			Circulating   float64 `json:"circulatingSupply"`
			Staked        float64 `json:"stakedSupply"`
			StakedPercent float64 `json:"stakedPercent"`
		}
		if json.Unmarshal(raw, &sup) == nil {
			st.Circulating = sup.Circulating
			if st.LockedDCR == 0 {
				st.LockedDCR = sup.Staked
			}
			if st.Participation == 0 {
				st.Participation = sup.StakedPercent
			}
		}
	}
	if raw, err := c.Call(ctx, "staking_info", nil); err == nil {
		var si struct {
			BlockHeight  int64   `json:"blockHeight"`
			EstimatedMin float64 `json:"estimatedMin"`
			EstimatedMax float64 `json:"estimatedMax"`
		}
		if json.Unmarshal(raw, &si) == nil {
			st.EstimatedMin, st.EstimatedMax = si.EstimatedMin, si.EstimatedMax
			if node != nil {
				node.Height = si.BlockHeight
			}
			into := int(si.BlockHeight % ticketWindow)
			st.BlocksToChange = ticketWindow - into
			st.WindowProgress = float64(into) / float64(ticketWindow) * 100
			st.HoursToChange = float64(st.BlocksToChange) * targetBlockSeconds / 3600
		}
	}
	if raw, err := c.Call(ctx, "staking_tickets", nil); err == nil {
		st.Own = summariseTickets(raw, st.PoolSize)
	}
	return st
}

// ticketRecord is the subset of dcrpulse's TicketRecord the hero needs.
type ticketRecord struct {
	Status      string  `json:"status"`
	VSPHost     string  `json:"vspHost"`
	BlockTime   int64   `json:"blockTime"`
	SpenderTime int64   `json:"spenderTime"`
	TicketPrice float64 `json:"ticketPrice"`
	Reward      float64 `json:"reward"`
}

// summariseTickets folds the ticket list into the personal staking figures.
// The list is the only source with vote timestamps and per-ticket rewards; the
// staking_info counters cannot answer "when did I last vote".
func summariseTickets(raw json.RawMessage, poolSize int) OwnStaking {
	var tickets []ticketRecord
	if json.Unmarshal(raw, &tickets) != nil {
		return OwnStaking{}
	}
	var own OwnStaking
	own.HasHistory = len(tickets) > 0

	voted := make([]ticketRecord, 0, len(tickets))
	vsps := map[string]int{}
	for _, t := range tickets {
		switch strings.ToLower(t.Status) {
		case "live":
			own.Live++
		case "immature":
			own.Immature++
		case "unmined":
			own.Unmined++
		case "voted":
			own.Voted++
			own.Reward += t.Reward
			voted = append(voted, t)
			if t.SpenderTime > own.LastVote {
				own.LastVote = t.SpenderTime
			}
		case "missed":
			own.Missed++
		case "revoked":
			own.Revoked++
		}
		if t.VSPHost != "" {
			vsps[t.VSPHost]++
		}
	}

	sort.Slice(voted, func(i, j int) bool { return voted[i].SpenderTime < voted[j].SpenderTime })
	own.VoteBuckets, own.BucketDays = weeklyVotes(voted, voteWeeks)

	if poolSize > 0 {
		own.PoolShare = float64(own.Live) / float64(poolSize) * 100
	}
	best := 0
	for host, n := range vsps {
		if n > best {
			best, own.VSP = n, host
		}
	}
	return own
}

// voteWeeks is how far back the vote chart looks: six months of weekly bars is
// enough to show a rhythm and any gap in it, and still fits a panel column.
const voteWeeks = 26

// weeklyVotes buckets votes into weeks ending now, oldest first. An empty week
// is a real zero and stays in the series: the gaps are the point.
func weeklyVotes(voted []ticketRecord, weeks int) ([]int, int) {
	if len(voted) == 0 || weeks <= 0 {
		return nil, 0
	}
	const day = 24 * 60 * 60
	week := int64(7 * day)
	now := time.Now().Unix()
	start := now - int64(weeks)*week

	buckets := make([]int, weeks)
	any := false
	for _, t := range voted {
		if t.SpenderTime < start || t.SpenderTime > now {
			continue
		}
		i := int((t.SpenderTime - start) / week)
		if i < 0 || i >= weeks {
			continue
		}
		buckets[i]++
		any = true
	}
	if !any {
		// Every vote is older than the window. A row of zeroes would read as a
		// broken setup rather than an old one, so draw nothing.
		return nil, 0
	}
	return buckets, 7
}

func fetchWallet(ctx context.Context, c *Client) *Wallet {
	raw, err := c.Call(ctx, "wallet_dashboard", nil)
	if err != nil {
		return nil
	}
	var wd struct {
		WalletStatus struct {
			Status       string  `json:"status"`
			SyncProgress float64 `json:"syncProgress"`
		} `json:"walletStatus"`
		AccountInfo struct {
			TotalBalance     float64 `json:"totalBalance"`
			SpendableBalance float64 `json:"spendableBalance"`
			LockedByTickets  float64 `json:"lockedByTickets"`
		} `json:"accountInfo"`
	}
	if json.Unmarshal(raw, &wd) != nil {
		return nil
	}
	return &Wallet{
		Status:    wd.WalletStatus.Status,
		Synced:    wd.WalletStatus.SyncProgress >= 100,
		Total:     wd.AccountInfo.TotalBalance,
		Spendable: wd.AccountInfo.SpendableBalance,
		Locked:    wd.AccountInfo.LockedByTickets,
	}
}

func fetchLightning(ctx context.Context, c *Client) *Lightning {
	raw, err := c.Call(ctx, "lightning_balance", nil)
	if err != nil {
		return nil
	}
	var lb struct {
		OnChainTotal   int64 `json:"onChainTotal"`
		ChannelLocal   int64 `json:"channelLocal"`
		ChannelRemote  int64 `json:"channelRemote"`
		ChannelPending int64 `json:"channelPending"`
	}
	if json.Unmarshal(raw, &lb) != nil {
		return nil
	}
	ln := &Lightning{
		Outbound: float64(lb.ChannelLocal) / atomsPerDCR,
		Inbound:  float64(lb.ChannelRemote) / atomsPerDCR,
		OnChain:  float64(lb.OnChainTotal) / atomsPerDCR,
		Pending:  float64(lb.ChannelPending) / atomsPerDCR,
	}
	if raw, err := c.Call(ctx, "lightning_channels", nil); err == nil {
		ln.List = parseChannels(raw)
	}
	if raw, err := c.Call(ctx, "lightning_info", nil); err == nil {
		var li struct {
			ActiveChannels int  `json:"numActiveChannels"`
			Peers          int  `json:"numPeers"`
			SyncedToChain  bool `json:"syncedToChain"`
		}
		if json.Unmarshal(raw, &li) == nil {
			ln.Channels = li.ActiveChannels
			ln.Peers = li.Peers
			ln.Synced = li.SyncedToChain
		}
	}
	return ln
}

// maxChannels caps the per-channel list. A node with thirty channels would
// otherwise push everything below it off the panel, and the largest few are
// where the liquidity actually is.
const maxChannels = 6

func parseChannels(raw json.RawMessage) []LnChannel {
	var wrapped struct {
		Channels []struct {
			RemoteAlias   string `json:"remoteAlias"`
			Capacity      int64  `json:"capacity"`
			LocalBalance  int64  `json:"localBalance"`
			RemoteBalance int64  `json:"remoteBalance"`
			Active        bool   `json:"active"`
		} `json:"channels"`
	}
	if json.Unmarshal(raw, &wrapped) != nil || len(wrapped.Channels) == 0 {
		return nil
	}
	out := make([]LnChannel, 0, len(wrapped.Channels))
	for _, c := range wrapped.Channels {
		out = append(out, LnChannel{
			Alias:    c.RemoteAlias,
			Capacity: float64(c.Capacity) / atomsPerDCR,
			Local:    float64(c.LocalBalance) / atomsPerDCR,
			Remote:   float64(c.RemoteBalance) / atomsPerDCR,
			Active:   c.Active,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Capacity > out[j].Capacity })
	if len(out) > maxChannels {
		out = out[:maxChannels]
	}
	return out
}

func fetchPrice(ctx context.Context, c *Client) *Price {
	raw, err := c.Call(ctx, "br_rates", nil)
	if err != nil {
		return nil
	}
	var r struct {
		DcrUsd    float64 `json:"dcr_usd"`
		BtcUsd    float64 `json:"btc_usd"`
		Source    string  `json:"source"`
		UpdatedAt string  `json:"updated_at"`
	}
	if json.Unmarshal(raw, &r) != nil || r.DcrUsd <= 0 {
		return nil
	}
	p := &Price{DcrUsd: r.DcrUsd, BtcUsd: r.BtcUsd, Source: r.Source, Updated: r.UpdatedAt}
	if r.BtcUsd > 0 {
		p.Sats = r.DcrUsd / r.BtcUsd * 1e8
	}
	return p
}

// fetchTreasury reports the treasury in USD. dcrpulse's own balanceUsd field is
// not populated, so the figure is derived from the balance and the rate feed,
// and is therefore omitted when there is no price.
func fetchTreasury(ctx context.Context, c *Client, price *Price) *Treasury {
	raw, err := c.Call(ctx, "treasury_info", nil)
	if err != nil {
		return nil
	}
	var ti struct {
		Balance float64 `json:"balance"`
	}
	if json.Unmarshal(raw, &ti) != nil || ti.Balance <= 0 {
		return nil
	}
	t := &Treasury{BalanceDCR: ti.Balance}
	if price != nil && price.DcrUsd > 0 {
		t.BalanceUsd = ti.Balance * price.DcrUsd
	}
	return t
}

func fetchBR(ctx context.Context, c *Client, limit int) *BR {
	br := &BR{}
	if raw, err := c.Call(ctx, "br_status", nil); err == nil {
		var bs struct {
			Stage string `json:"stage"`
			Nick  string `json:"nick"`
		}
		if json.Unmarshal(raw, &bs) == nil {
			br.Stage, br.Nick = bs.Stage, bs.Nick
		}
	}
	if raw, err := c.Call(ctx, "br_notifications", nil); err == nil {
		var bn struct {
			Notifications []Notice `json:"notifications"`
		}
		if json.Unmarshal(raw, &bn) == nil {
			br.Notices = bn.Notifications
		}
	}
	br.Messages = fetchMessages(ctx, c, limit)
	nameGroups(ctx, c, br.Messages)
	return br
}

// nameGroups fills in the group name for each group message.
//
// The ring carries a gcid and nothing else, so without this the panel has
// nothing to label a row with but the word "group". br_groupchats is a read
// tool in the domain the messages already need, so this asks for nothing extra
// of a token, and the client caches the answer.
func nameGroups(ctx context.Context, c *Client, msgs []Message) {
	var ids []string
	for _, m := range msgs {
		if m.GCID != "" {
			ids = append(ids, m.GCID)
		}
	}
	if len(ids) == 0 {
		return
	}
	names := c.resolveGroups(ctx, ids)
	for i := range msgs {
		if n := names[msgs[i].GCID]; n != "" {
			msgs[i].GCName = n
		}
	}
}

// ringEntry is the wire shape of one chat-ring element.
type ringEntry struct {
	Type    string `json:"type"`
	Payload struct {
		FromNick string `json:"fromNick"`
		Message  string `json:"message"`
		GCID     string `json:"gcid"`
	} `json:"payload"`
}

// fetchMessages reads the chat ring and returns it oldest first, holding the
// newest limit entries.
func fetchMessages(ctx context.Context, c *Client, limit int) []Message {
	if limit <= 0 {
		limit = 20
	}
	raw, err := c.ReadResource(ctx, ResBRMessages)
	if err != nil {
		return nil
	}
	var entries []ringEntry
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	return ringToMessages(entries, limit)
}

// ringToMessages caps a ring read to the newest limit entries and turns it the
// right way round.
//
// dcrpulse reverses its ring on read, so the resource hands back newest first.
// Everything downstream reads the slice as a chat log that grows at the end:
// the panel shows the last few, and the unread counter remembers the last one
// it saw and credits whatever follows it. Taking the tail of the raw response
// keeps the oldest messages and pins that counter to an entry that never moves,
// which is a stuck badge and a panel full of yesterday.
func ringToMessages(entries []ringEntry, limit int) []Message {
	if len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]Message, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		out = append(out, Message{
			Type:     e.Type,
			FromNick: e.Payload.FromNick,
			Text:     e.Payload.Message,
			GCID:     e.Payload.GCID,
		})
	}
	return out
}

// RingCounts returns how many private and group entries a ring read holds.
func RingCounts(msgs []Message) (private, group int) {
	for _, m := range msgs {
		if m.IsGroup() {
			group++
		} else {
			private++
		}
	}
	return
}

// attachCandles adds a DCR/BTC price history when a DEX server is registered.
//
// Everything here degrades to "no chart": no dex grant, no registered server,
// no DCR/BTC market, or no candles all leave Price.Series empty and the panel
// simply does not draw a chart. That is the same rule the rest of the snapshot
// follows: a section appears only when its data does.
func attachCandles(ctx context.Context, c *Client, p *Price) {
	raw, err := c.Call(ctx, "dex_exchanges", nil)
	if err != nil {
		return
	}
	var exchanges map[string]struct {
		Host    string `json:"host"`
		Markets map[string]struct {
			Name        string `json:"name"`
			BaseID      uint32 `json:"baseid"`
			BaseSymbol  string `json:"basesymbol"`
			QuoteID     uint32 `json:"quoteid"`
			QuoteSymbol string `json:"quotesymbol"`
		} `json:"markets"`
		CandleDurs []string `json:"candleDurs"`
	}
	if json.Unmarshal(raw, &exchanges) != nil {
		return
	}

	for host, ex := range exchanges {
		if host == "" {
			host = ex.Host
		}
		for _, m := range ex.Markets {
			if !strings.EqualFold(m.BaseSymbol, "dcr") || !strings.EqualFold(m.QuoteSymbol, "btc") {
				continue
			}
			dur := pickDuration(ex.CandleDurs)
			raw, err := c.Call(ctx, "dex_candles", map[string]any{
				"host": host, "baseId": m.BaseID, "quoteId": m.QuoteID, "dur": dur,
			})
			if err != nil {
				continue
			}
			series := parseCandles(raw)
			if len(series) < 2 {
				continue
			}
			p.Series = downsample(series, 60)
			if series[0] > 0 {
				p.Change = (series[len(series)-1] - series[0]) / series[0] * 100
			}
			p.Market = m.Name
			if p.Market == "" {
				p.Market = "dcr/btc"
			}
			return
		}
	}
}

// pickDuration prefers an hourly bin, so a chart covers days rather than years.
func pickDuration(durs []string) string {
	for _, want := range []string{"1h", "4h", "24h"} {
		for _, d := range durs {
			if d == want {
				return d
			}
		}
	}
	if len(durs) > 0 {
		return durs[0]
	}
	return "1h"
}

// parseCandles pulls the closing rate out of each candle. The payload is
// accepted both wrapped and bare, because this path cannot be exercised without
// a registered DEX server and a wrong guess should cost the chart, not the
// snapshot.
func parseCandles(raw json.RawMessage) []float64 {
	type candle struct {
		EndRate   float64 `json:"endRate"`
		StartRate float64 `json:"startRate"`
	}
	var wrapped struct {
		Candles []candle `json:"candles"`
	}
	list := []candle(nil)
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Candles) > 0 {
		list = wrapped.Candles
	} else if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	out := make([]float64, 0, len(list))
	for _, c := range list {
		rate := c.EndRate
		if rate == 0 {
			rate = c.StartRate
		}
		if rate > 0 {
			out = append(out, rate)
		}
	}
	return out
}

// downsample reduces a series to at most max points by sampling, which keeps
// the extremes a moving average would flatten.
func downsample(in []float64, max int) []float64 {
	if len(in) <= max {
		return in
	}
	out := make([]float64, max)
	for i := range out {
		idx := int(math.Round(float64(i) * float64(len(in)-1) / float64(max-1)))
		out[i] = in[idx]
	}
	return out
}

// fetchDex reads the DCR/BTC spot from the DEX client. The summary is public
// market data the client already holds, so this costs no unlock and no round
// trip to the server.
func fetchDex(ctx context.Context, c *Client, price *Price) *Dex {
	raw, err := c.Call(ctx, "dex_market_summary", nil)
	if err != nil {
		return nil
	}
	return parseDexSummary(raw, price)
}

// parseDexSummary picks the first dcr_btc market out of a dex_market_summary
// reply. Rates arrive conventional, BTC per DCR, and leave as sats per DCR so
// they sit beside Price.Sats; change24 arrives as a ratio and leaves as a
// percent. The premium needs the exchange feed and is left at zero without it.
func parseDexSummary(raw json.RawMessage, price *Price) *Dex {
	var markets []struct {
		Host        string  `json:"host"`
		Market      string  `json:"market"`
		BaseSymbol  string  `json:"baseSymbol"`
		QuoteSymbol string  `json:"quoteSymbol"`
		LastRate    float64 `json:"lastRate"`
		LastRateUsd float64 `json:"lastRateUsd"`
		Change24    float64 `json:"change24"`
		High24      float64 `json:"high24"`
		Low24       float64 `json:"low24"`
		Vol24Base   float64 `json:"vol24Base"`
		Stamp       int64   `json:"stamp"`
	}
	if json.Unmarshal(raw, &markets) != nil {
		return nil
	}
	for _, m := range markets {
		if !strings.EqualFold(m.BaseSymbol, "dcr") || !strings.EqualFold(m.QuoteSymbol, "btc") || m.LastRate <= 0 {
			continue
		}
		d := &Dex{
			Host:     m.Host,
			Market:   m.Market,
			Rate:     toSats(m.LastRate),
			RateUsd:  m.LastRateUsd,
			Change24: m.Change24 * 100,
			High24:   toSats(m.High24),
			Low24:    toSats(m.Low24),
			Volume24: m.Vol24Base,
		}
		if d.Market == "" {
			d.Market = "dcr_btc"
		}
		if m.Stamp > 0 {
			d.Updated = time.UnixMilli(m.Stamp).UTC().Format(time.RFC3339)
		}
		if price != nil && price.Sats > 0 {
			d.Premium = (float64(d.Rate)/price.Sats - 1) * 100
		}
		return d
	}
	return nil
}

// toSats turns a conventional BTC-per-DCR rate into whole sats per DCR.
func toSats(conventional float64) int64 { return int64(math.Round(conventional * 1e8)) }
