---
name: demarchy-alerts
description: "Arm, list, edit and disarm demarchy alerts: watches on a dcrpulse value (DCR price, ticket price, block height, wallet and Lightning balances, channels, Bison Relay messages) that wake this agent through herdr, or notify the user, when a condition trips. Use when the user asks to be told, woken, alerted or notified when a Decred value crosses a level, changes by an amount, stalls, or when a list entry appears or disappears; and when a message starting 'demarchy alarm' arrives in this session. Do not use to read current prices, balances or chain state (use the dcrpulse MCP), for timers or reminders unrelated to dcrpulse data, or to edit ~/.config/demarchy/triggers.json by hand."
---

# Demarchy alerts

Demarchy alerts are a passive monitor over the dcrpulse values the bar widget already shows: every beat, the helper folds the fresh snapshot through the triggers an agent armed and rings when a condition trips. It never answers a data question, never acts on anything, and the wake-up text carries no values. It rings by handing the alarm through herdr to whatever agent runs in the arming pane, retrying for 60 seconds while that agent is blocked on a dialog, and otherwise by raising a desktop notification to the user.

## Find the tool

`demarchy-setup` is not on PATH on a normal install. Use `demarchy-setup` if `command -v demarchy-setup` finds it, otherwise `~/.config/omarchy/plugins/karamble.demarchy/bin/demarchy-setup`. Every example here writes plain `demarchy-setup`.

If `HERDR_PANE_ID` is set you are in a herdr pane: `arm` records that pane as `armedBy`, and alarms come back to this pane unless you pass `--deliver`. Outside a pane, `armedBy` is `you` and alarms go to the user's desktop.

## Before arming

- Run `demarchy-setup alerts --json` first. Do not arm a trigger that duplicates one already there on the same path, operator, params and connection; edit yours instead.
- Never disarm or edit a trigger whose `armedBy` is not you. Tell the user it exists and who armed it.
- Read the warnings `arm` prints on stderr and relay them to the user. "monitoring is off" means the trigger is stored but nobody is watching until the user switches monitoring on in the panel. The wallet warning means `wallet.*` is not fetched while showBalances is off, so that trigger cannot fire until it is on. Both leave the trigger armed.
- Write `--reason` as an instruction to your future self. The alarm carries the reason and nothing else about why you cared, so "sell leg of the rebalance" beats "price alert".

## Syntax

```bash
demarchy-setup catalogue --json
demarchy-setup alerts --json
demarchy-setup arm price.dcrUsd crosses --above 16 --expires 4d --reason "sell leg of the rebalance" --json
demarchy-setup edit t-7f3a9c21 --above 17 --expires 2d --json
demarchy-setup disarm t-7f3a9c21
```

Params by operator:

- `crosses` and `count`: `--above X` or `--below X`, with `--rearm R` to set the re-arm margin.
- `becomes`: `--value V`, with `--hold 10m` to set how long the value must stay away before it can ring again (default 5m).
- `changes`: `--by X`, with `--percent` to read X as a percentage of the baseline.
- `stalls`: `--for 45m`.
- `appears`, `disappears` and `count` on a list: `--where field=value` for an exact match or `--where field~=text` for a case-insensitive substring, repeatable and all must hold; `--key field` or `--key a,b` to choose what counts as the same entry.

Common flags:

- `--expires` is required: a span such as `4d`, `12h` or `90m`, a date such as `2026-10-01` meaning the end of that local day, or an RFC 3339 time.
- `--reason <text>`: the one piece of context the alarm carries.
- `--deliver <agent|pane|you>`: a herdr agent name or pane id, or `you` for the user's desktop. Defaults to your pane, or `you` outside one.
- `--standing` rings every time the condition trips until it expires; `--once` is the default.
- `--connection <id>`: bind to a configured connection other than the active one.
- `--json`: machine output on every verb.
- `--dry-run` on `arm` and `edit`: validate fully and print the trigger as it would be stored, with the warnings, saving nothing. The preview carries no id, since nothing was created.

**The first sample after arming never fires.** A value already past the bound is history, not an event; if you want to know where it is now, read dcrpulse before you arm.

**One-shot is the default, and a fired one-shot is spent.** It stays in the list as `fired` until you disarm it or edit it back to life. Pass `--standing` when every occurrence matters.

## Picking an operator

| Kind of leaf | Operators |
| --- | --- |
| number, integer | crosses, changes, stalls |
| text, bool | becomes, stalls |
| list of records | appears, disappears, count |
| list of plain values | count |

- `crosses`: fires on the sample that carries the value from one side of the bound to the other, never while it sits past it. After a fire it re-arms only once the value is back on the far side by more than the margin: `--rearm R`, else 1 for an integer, else 1% of the bound. The re-arming sample itself never fires.
- `becomes`: fires when a text or bool takes `--value` having not had it on the previous sample. It re-arms after the value has been away for `--hold` (default 5m), so a flapping status rings once.
- `changes`: measures against a baseline, not the previous sample, so a slow drift fires as surely as a spike. A fire moves the baseline to the current value, and it never disarms. With `--percent`, a baseline of zero never fires until it moves.
- `stalls`: fires once the value has not moved for `--for`. The clock is a persisted last-changed time that survives helper restarts and dcrpulse outages. Movement re-arms it, so each stall episode rings once.
- `appears` and `disappears`: compare the set of entry identities before and after the `--where` filter; order is ignored. Every new or missing entry is its own event, so these never disarm, and a one-shot is spent by the first one.
- `count`: `crosses` over the number of entries left after the filter, with an integer re-arm margin of 1.

When dcrpulse is unreachable, the section is not granted, or the connection is not the one being watched, there is no sample and nothing moves: a missing sample is not a transition.

## When the alarm arrives

The wake-up is one line that starts with `demarchy alarm`:

```
demarchy alarm t-7f3a9c21: price.dcrUsd crosses above 16 on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "sell leg of the rebalance". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. This was a one-shot and is now spent.
```

A standing trigger ends instead with `It stays armed until <expiry>; disarm with: demarchy-setup disarm <id>.` Then:

- Read dcrpulse yourself with your own MCP tools. The alarm says the condition tripped at that time, not what the value is now.
- Act on the reason you wrote, and report to the user.
- When a standing trigger has done its job, disarm it. To move a level or extend an expiry, edit it instead of arming a second trigger.
- Check `demarchy-setup alerts --json` for its status: `armed` is watched and ready; `rearming` has fired and waits for the condition to be clearly false again; `no-sample` means the last pass could not resolve the path; `other-connection` means it is bound to a connection that is not the one being watched; `fired` is a spent one-shot; `expired` passed its expiry without firing; `delivery-failed` means it rang and neither herdr nor a desktop notification could be raised, so tell the user.

## Recipes

Worked examples for every operator, with the exact text each one delivers, are in [`recipes.md`](recipes.md).

## Catalogue

Everything below is what the panel already fetches: `wallet.*` is only fetched while showBalances is on, a list filters with `--where` on the fields shown and compares entries by the identity shown, and a path is bound to the connection that is active when it is armed.

<!-- catalogue:begin -->
<!-- Generated from dcr.Catalogue() by make skill. Do not edit by hand: make lint fails when this is stale. -->
68 paths. number and integer take crosses, changes, stalls. text and bool take becomes, stalls. A list of records takes appears, disappears, count and filters with --where on the fields shown; entries are the same entry when their identity fields match. A list of plain values takes count only.

### br
- `br.messages` list: appears, disappears, count. Fields: type, fromNick, text, gcid, gcName. Identity: type, fromNick, text, gcid
- `br.nick` text: becomes, stalls
- `br.notices` list: appears, disappears, count. Fields: id, ts, severity, subject, detail. Identity: id
- `br.stage` text: becomes, stalls

### dex
- `dex.change24` number: crosses, changes, stalls
- `dex.high24` integer: crosses, changes, stalls
- `dex.host` text: becomes, stalls
- `dex.low24` integer: crosses, changes, stalls
- `dex.market` text: becomes, stalls
- `dex.premium` number: crosses, changes, stalls
- `dex.rate` integer: crosses, changes, stalls
- `dex.rateUsd` number: crosses, changes, stalls
- `dex.updated` text: becomes, stalls
- `dex.volume24` number: crosses, changes, stalls

### lightning
- `lightning.channels` integer: crosses, changes, stalls
- `lightning.inbound` number: crosses, changes, stalls
- `lightning.list` list: appears, disappears, count. Fields: alias, capacity, local, remote, active. Identity: alias
- `lightning.onChain` number: crosses, changes, stalls
- `lightning.outbound` number: crosses, changes, stalls
- `lightning.peers` integer: crosses, changes, stalls
- `lightning.pending` number: crosses, changes, stalls
- `lightning.synced` bool: becomes, stalls

### node
- `node.height` integer: crosses, changes, stalls
- `node.peers` integer: crosses, changes, stalls
- `node.status` text: becomes, stalls
- `node.syncMessage` text: becomes, stalls
- `node.syncPhase` text: becomes, stalls
- `node.syncProgress` number: crosses, changes, stalls
- `node.version` text: becomes, stalls

### price
- `price.btcUsd` number: crosses, changes, stalls
- `price.change` number: crosses, changes, stalls
- `price.dcrUsd` number: crosses, changes, stalls
- `price.market` text: becomes, stalls
- `price.sats` number: crosses, changes, stalls
- `price.series` list: count
- `price.source` text: becomes, stalls
- `price.updated` text: becomes, stalls

### staking
- `staking.blocksToChange` integer: crosses, changes, stalls
- `staking.circulating` number: crosses, changes, stalls
- `staking.estimatedMax` number: crosses, changes, stalls
- `staking.estimatedMin` number: crosses, changes, stalls
- `staking.hoursToChange` number: crosses, changes, stalls
- `staking.lockedDcr` number: crosses, changes, stalls
- `staking.nextTicketPrice` number: crosses, changes, stalls
- `staking.own.bucketDays` integer: crosses, changes, stalls
- `staking.own.hasHistory` bool: becomes, stalls
- `staking.own.immature` integer: crosses, changes, stalls
- `staking.own.lastVote` integer: crosses, changes, stalls
- `staking.own.live` integer: crosses, changes, stalls
- `staking.own.missed` integer: crosses, changes, stalls
- `staking.own.poolShare` number: crosses, changes, stalls
- `staking.own.revoked` integer: crosses, changes, stalls
- `staking.own.reward` number: crosses, changes, stalls
- `staking.own.unmined` integer: crosses, changes, stalls
- `staking.own.voteBuckets` list: count
- `staking.own.voted` integer: crosses, changes, stalls
- `staking.own.vsp` text: becomes, stalls
- `staking.participation` number: crosses, changes, stalls
- `staking.poolSize` integer: crosses, changes, stalls
- `staking.ticketPrice` number: crosses, changes, stalls
- `staking.windowProgress` number: crosses, changes, stalls

### treasury
- `treasury.balanceDcr` number: crosses, changes, stalls
- `treasury.balanceUsd` number: crosses, changes, stalls

### wallet
- `wallet.lockedByTickets` number: crosses, changes, stalls
- `wallet.spendable` number: crosses, changes, stalls
- `wallet.status` text: becomes, stalls
- `wallet.synced` bool: becomes, stalls
- `wallet.total` number: crosses, changes, stalls
<!-- catalogue:end -->

## Limits

- Do not hand-edit `~/.config/demarchy/triggers.json`. The loader rejects unknown fields, so one typo makes the whole file unreadable and nothing in it is watched. Use `arm`, `edit` and `disarm`.
- A trigger is evaluated only on the connection it was armed on. When the user switches the active connection it shows as `other-connection` and waits. `edit` cannot move it; disarm and arm again.
- `dex.*` exists only with a DEX server registered in dcrpulse that carries a dcr_btc market; `dex.rate`, `dex.high24` and `dex.low24` are sats per DCR like `price.sats`, and `dex.premium` reads zero while the exchange feed is unavailable.
- `lightning.list` holds only the 6 largest channels by capacity. A channel pushed out of the top 6 reads as a disappearance, and one that climbs into it reads as an appearance. `--where alias=<name>` keeps other channels from ringing you, but that channel still reads as gone if it drops out of the six.
