// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"math"
	"time"

	"github.com/karamble/demarchy/internal/dcr"
)

// demoCandles is a wandering DCR/BTC series, so the price chart can be
// designed and screenshotted without a registered DEX server.
func demoCandles() []float64 {
	const n = 60
	out := make([]float64, n)
	v := 20100.0
	for i := 0; i < n; i++ {
		// Two out-of-phase waves plus a drift: enough shape to read as a price
		// rather than a sine, without any pretence of being real data.
		v += math.Sin(float64(i)/4.1)*70 + math.Sin(float64(i)/11.3)*130 + 12
		out[i] = v
	}
	return out
}

// demoSnapshot builds a synthetic snapshot for --demo.
//
// It exists because a wallet that has never staked cannot show the staking
// hero, and because the README screenshot should not carry a real Bison Relay
// nick or other people's group-chat messages. Every figure below is invented;
// nothing here is ever reachable from the widget, which has no way to pass
// --demo.
func demoSnapshot() *dcr.Snapshot {
	now := time.Now()

	// A staking history: two years of votes worth about 19 DCR, and six months
	// of weekly counts with the sort of variation a real ticket pool produces,
	// including a quiet fortnight, because the gaps are what the chart is for.
	const votes = 847
	running := 19.02
	weekly := []int{
		7, 9, 6, 11, 8, 10, 7, 5, 9, 12, 8, 6,
		0, 1, 4, 9, 7, 11, 8, 9, 6, 10, 7, 8, 12, 9,
	}

	return &dcr.Snapshot{
		V:         dcr.SchemaVersion,
		Stamp:     now.Unix(),
		Reachable: true,
		Domains:   []string{"node", "staking", "bisonrelay", "wallet", "lightning", "treasury", "dex"},
		// Two connections so the footer switcher has something to show.
		PlainHTTP: &dcr.PlainHTTPPublic{
			Interface: "wt0",
			Networks:  []string{"100.83.12.7/32"},
		},
		Connections: []dcr.PublicConnection{
			{ID: "home", Name: "home", Endpoint: "http://127.0.0.1:8090"},
			{ID: "vps", Name: "vps", Endpoint: "https://pulse.example:8090"},
		},
		Active: "home",
		Node: &dcr.Node{
			Status: "running", SyncProgress: 100, SyncPhase: "synced",
			SyncMessage: "Fully synced", Version: "v2.1.6",
			Height: 1112477, Peers: 8,
		},
		Staking: &dcr.Staking{
			TicketPrice: 285.90450882, NextTicketPrice: 287.97610485,
			EstimatedMin: 283.92067585, EstimatedMax: 301.91875618,
			PoolSize: 41430, Participation: 64.72897879, LockedDCR: 11385653.22,
			Circulating:    17589730.96,
			BlocksToChange: 67, HoursToChange: 5.583, WindowProgress: 53.47,
			Own: dcr.OwnStaking{
				Live: 12, Immature: 2, Voted: votes, Missed: 3, Revoked: 1,
				Reward:      running,
				LastVote:    now.Add(-3*time.Hour - 20*time.Minute).Unix(),
				VoteBuckets: weekly,
				BucketDays:  7,
				PoolShare:   12.0 / 41430.0 * 100,
				VSP:         "vsp.decredcommunity.org",
				HasHistory:  true,
			},
		},
		Wallet: &dcr.Wallet{
			Status: "synced", Synced: true,
			Total: 3461.82, Spendable: 30.91, Locked: 3430.91,
		},
		Lightning: &dcr.Lightning{
			// A node that has been running a while: several channels, and more
			// outbound than inbound, which is what a mostly-paying node looks
			// like.
			Channels: 6, Peers: 9, Synced: true,
			Outbound: 14.8231, Inbound: 5.4067, OnChain: 2.7419,
			// Deliberately uneven, including one drained and one inactive:
			// that is what a real node looks like after a few months, and it
			// is the case the per-channel view exists to show.
			List: []dcr.LnChannel{
				{Alias: "hub0.bisonrelay.org", Capacity: 8.000, Local: 6.412, Remote: 1.588, Active: true},
				{Alias: "zaphod.ln.decred", Capacity: 5.000, Local: 4.230, Remote: 0.770, Active: true},
				{Alias: "ln.decredcommunity.org", Capacity: 3.000, Local: 1.902, Remote: 1.098, Active: true},
				{Alias: "trillian", Capacity: 2.000, Local: 1.744, Remote: 0.256, Active: true},
				{Alias: "marvin.node", Capacity: 1.500, Local: 0.535, Remote: 0.965, Active: true},
				{Alias: "slartibartfast", Capacity: 1.000, Local: 0.000, Remote: 0.730, Active: false},
			},
		},
		Price: &dcr.Price{
			DcrUsd: 16.33246913, BtcUsd: 79597.92,
			Sats:   16.33246913 / 79597.92 * 1e8,
			Source: "demo", Updated: now.Format(time.RFC3339),
			Series: demoCandles(), Change: 4.2, Market: "dcr_btc",
		},
		Treasury: &dcr.Treasury{
			BalanceDCR: 877368.39, BalanceUsd: 877368.39 * 16.33246913,
		},
		BR: &dcr.BR{
			Stage: "ready", Nick: "satoshi",
			Notices: nil,
			// Invented chat, so a published screenshot never carries a real
			// nick or somebody else's messages.
			Messages: []dcr.Message{
				{Type: "gc-message", FromNick: "relay", Text: "[m] <asterix> ticket price ticked up again", GCID: "d1", GCName: "dcr-support"},
				{Type: "pm", FromNick: "hal", Text: "your ticket voted, nice one"},
				{Type: "gc-message", FromNick: "relay", Text: "[m] <obelix> pool is holding just under 42k", GCID: "d1", GCName: "dcr-support"},
			},
		},
		Unread: dcr.Unread{Private: 1, Groupchat: 2, Total: 3},
	}
}
