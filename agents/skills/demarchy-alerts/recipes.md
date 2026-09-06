# Demarchy alert recipes

One recipe per operator, each with the command, what makes it fire, what happens after, and the exact line the woken agent receives. Every sample text below uses the same fixed values: trigger id `t-7f3a9c21`, connection `omarchy`, armed by pane `w7:p1` at `2026-09-05T22:10:00Z`, fired at `2026-09-06T00:42:02Z`. A standing trigger's expiry is the arming time plus its `--expires` span. Find the tool as described in [`SKILL.md`](SKILL.md); the commands are written as plain `demarchy-setup`.

## crosses: wake me when the price passes a level

Situation: you are running a rebalance and the sell leg goes when DCR trades above 16 USD.

```bash
demarchy-setup arm price.dcrUsd crosses --above 16 --expires 4d --reason "sell leg of the rebalance"
```

What fires it: a sample above 16 following one at or below 16. A price already above 16 when you arm is history and does not ring. A price that wobbles across the bound rings once; the trigger re-arms only after the price has been below 15.84 again, which is the bound less the default margin of 1%.

After it fires: the one-shot is spent and shows as `fired` in the list until you disarm it. Edit it with a new bound and expiry to bring it back.

What you receive:

```
demarchy alarm t-7f3a9c21: price.dcrUsd crosses above 16 on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "sell leg of the rebalance". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. This was a one-shot and is now spent.
```

Then read the price from dcrpulse yourself before placing anything; it may have moved again since the sample.

## becomes: wake me when a status takes a value

Situation: dcrlnd should stay synced to the chain, and you want to hear about it when it falls behind, but not on every hiccup.

```bash
demarchy-setup arm lightning.synced becomes --value false --hold 10m --expires 7d --standing --reason "dcrlnd fell behind the chain"
```

What fires it: a sample where `lightning.synced` reads `false` after one where it read `true`. Booleans are compared as the words `true` and `false`.

After it fires: it disarms until synced has read `true` for 10 minutes without changing, then it is ready to ring again. Standing, so it keeps doing that until it expires seven days after arming.

What you receive:

```
demarchy alarm t-7f3a9c21: lightning.synced becomes "false" hold 10m on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "dcrlnd fell behind the chain". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. It stays armed until 2026-09-12T22:10:00Z; disarm with: demarchy-setup disarm t-7f3a9c21.
```

Then check `lightning_info` on dcrpulse, and disarm the trigger once the node is fixed and the watch is no longer wanted.

## changes: wake me on every move of a size

Situation: your ticket buyer prices its bids off the ticket price and should re-price whenever it moves two percent from where it last looked.

```bash
demarchy-setup arm staking.ticketPrice changes --by 2 --percent --expires 30d --standing --reason "re-price the ticket buyer"
```

What fires it: a sample at least 2% away from the baseline. The baseline is the first sample after arming, not the previous sample, so four steps of half a percent in the same direction fire as surely as one jump of two.

After it fires: the baseline moves to the value that fired and the trigger is ready again at once. It never disarms; standing, each further 2% step rings until it expires thirty days after arming. Without `--percent`, `--by 2` would mean two DCR. A percent trigger whose baseline is zero never fires until the baseline moves.

What you receive:

```
demarchy alarm t-7f3a9c21: staking.ticketPrice changes by 2% on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "re-price the ticket buyer". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. It stays armed until 2026-10-05T22:10:00Z; disarm with: demarchy-setup disarm t-7f3a9c21.
```

Then read `staking_info` on dcrpulse for the new price and re-price. Disarm it when the buyer is retired.

## stalls: wake me when a value stops moving

Situation: blocks should keep coming, and you want to know if the chain your node sees has not advanced for 45 minutes.

```bash
demarchy-setup arm node.height stalls --for 45m --expires 2d --reason "chain stopped advancing"
```

What fires it: 45 minutes since the height last changed. The clock is the persisted last-changed time, not the helper's uptime, so a restart of the shell or an outage of dcrpulse in the middle does not reset it: if dcrpulse comes back two hours later with the same height, the first sample fires.

After it fires: one-shot, so it is spent. Standing, it would re-arm on the next height change and ring again after the next 45 quiet minutes.

What you receive:

```
demarchy alarm t-7f3a9c21: node.height stalls for 45m on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "chain stopped advancing". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. This was a one-shot and is now spent.
```

Then check `node_status` and `node_peers` on dcrpulse to tell a stalled network from a node that lost its peers.

## appears: wake me when a list gains an entry

Situation: you are waiting for a Bison Relay message about an invoice, and separately want to know when anyone new writes.

```bash
demarchy-setup arm br.messages appears --where text~=invoice --expires 3d --standing --reason "watch for the invoice from acme"
demarchy-setup arm br.messages appears --key fromNick --expires 3d --standing --reason "someone new wrote"
```

What fires them: a new entry in the filtered set. The identity of a `br.messages` entry is `type`, `fromNick`, `text` and `gcid` together, so a repeated identical message is the same entry and does not ring again, while the same nick saying something else does. `~=` is a case-insensitive substring match, so `text~=invoice` catches "Invoice 42 attached". The second command replaces the identity with `--key fromNick`: the set is then the set of nicks, and it rings when a nick that was not in the previous sample writes. The list holds only the most recent messages, so a nick whose messages have all scrolled off counts as new again.

After they fire: appears never disarms, since every new entry is its own event. Standing, both keep ringing for three days. A one-shot appears would be spent by the first new entry.

What you receive from the first command:

```
demarchy alarm t-7f3a9c21: br.messages appears where text~=invoice on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "watch for the invoice from acme". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. It stays armed until 2026-09-08T22:10:00Z; disarm with: demarchy-setup disarm t-7f3a9c21.
```

What you receive from the second command:

```
demarchy alarm t-7f3a9c21: br.messages appears key fromNick on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "someone new wrote". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. It stays armed until 2026-09-08T22:10:00Z; disarm with: demarchy-setup disarm t-7f3a9c21.
```

Then read the message with `br_pm_history` or `br_groupchat_history` on dcrpulse. Disarm the invoice watch once the invoice is in.

## disappears: wake me when a list loses an entry

Situation: you want to know when one of your Lightning channels goes inactive.

```bash
demarchy-setup arm lightning.list disappears --where active=true --expires 14d --reason "a channel went inactive"
```

What fires it: an alias that was in the set of active channels on the previous sample and is not in it now. The filter runs before identity, so a channel that flips to inactive leaves the set and counts as a disappearance even though the channel still exists; so does a channel that closes. `lightning.list` holds only the 6 largest channels by capacity, so a channel pushed out of the top 6 by a bigger one opening also reads as gone. Pin one channel with `--where alias=<name>` when that is the one you care about.

After it fires: one-shot, so it is spent. Standing, it would ring for every further channel that leaves the set.

What you receive:

```
demarchy alarm t-7f3a9c21: lightning.list disappears where active=true on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "a channel went inactive". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. This was a one-shot and is now spent.
```

Then read `lightning_channels` on dcrpulse to see which channel and why.

## count: wake me when a list gets short or long

Situation: payments need at least two active channels to route, and you want to hear when there are fewer.

```bash
demarchy-setup arm lightning.list count --below 2 --where active=true --expires 14d --reason "fewer than two active channels"
```

What fires it: the number of channels left after the filter dropping from 2 or more to below 2. It is `crosses` over the filtered length, so a node already down to one channel when you arm does not ring until it recovers and drops again. After a fire it re-arms once the count is above 3, the bound plus the integer margin of 1.

After it fires: one-shot, so it is spent.

What you receive:

```
demarchy alarm t-7f3a9c21: lightning.list count below 2 where active=true on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "fewer than two active channels". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. This was a one-shot and is now spent.
```

Then read `lightning_channels` on dcrpulse and decide whether to open or reconnect.

## crosses on the DEX premium: wake me when the DEX pays more

Situation: DCR sometimes trades at a different price on DCRDEX than on the exchanges, and you want to hear when a DCR fetches two percent more on the DEX than the exchange feed implies.

```bash
demarchy-setup arm dex.premium crosses --above 2 --expires 7d --standing --reason "DCR fetches more on the DEX than on the exchange"
```

What fires it: `dex.premium` is the DCRDEX last trade against the exchange feed's implied DCR/BTC rate, in percent, and this rings on the sample that carries it from at or below 2 to above 2. It re-arms once the premium has been below 1.98 again, the bound less 1%. The premium reads zero while the exchange feed is unavailable, so a feed outage can look like a drop to zero.

After it fires: standing, so it rings on each fresh crossing for seven days.

What you receive:

```
demarchy alarm t-7f3a9c21: dex.premium crosses above 2 on connection "omarchy" fired at 2026-09-06T00:42:02Z. Reason: "DCR fetches more on the DEX than on the exchange". Armed by w7:p1 at 2026-09-05T22:10:00Z. This message carries no values; read dcrpulse yourself. It stays armed until 2026-09-12T22:10:00Z; disarm with: demarchy-setup disarm t-7f3a9c21.
```

Then read `dex_market_summary` and `dex_orderbook` on dcrpulse for the rate and the depth behind it before acting.

## Check first, then change or remove

```bash
demarchy-setup arm price.dcrUsd crosses --below 12 --expires 4d --reason "buy leg" --dry-run
demarchy-setup edit t-7f3a9c21 --above 17 --expires 2d
demarchy-setup disarm t-7f3a9c21
```

`--dry-run` validates the whole command, prints the trigger as it would be stored together with the warnings, and saves nothing; the preview has no id because nothing was created. Use it to check a `--where` field or an expiry before you commit. `edit` changes a trigger in place and bumps its `rev`: it re-arms a spent one-shot, keeps the last sample and the stall clock when only a bound changes, and drops them when the path changes so a stale number cannot manufacture a transition. Params given to `edit` replace the whole set, so repeat `--where` or `--rearm` if you want to keep them; flags left out keep their value. `disarm` removes the trigger now, spent or not.

## When monitoring is paused

Off means off. While monitoring is paused in the panel nothing is evaluated: no trigger fires, no stall clock is read, and no alarm is sent. `arm` and `edit` still work and print the warning that monitoring is off, and `alerts` repeats it. Tell the user, because switching monitoring back on is their decision; a trigger armed while paused takes its baseline from the first sample after that.
