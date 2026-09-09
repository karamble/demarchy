# Demarchy alerts

Demarchy alerts are a passive monitor over the dcrpulse values the bar widget already shows: every beat, the helper folds the fresh snapshot through the triggers an agent armed and rings when a condition trips. It never answers a data question, never acts on anything, and the wake-up text carries no values. It rings by handing the alarm through herdr to whatever agent runs in the arming pane, retrying for 60 seconds while that agent is blocked on a dialog, and otherwise by raising a desktop notification to the user.

This guide is printed by `demarchy-setup skill`, and `--recipes` prints the
worked examples. It is never installed anywhere: nothing writes it into an
agent's directories, so an agent reads it when it is asked to and not otherwise.

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

Worked examples for every operator, with the exact text each one delivers, are in [`alert-recipes.md`](alert-recipes.md).

## Catalogue

Every watchable path, with its kind and the operators it takes, comes from
the binary itself so it cannot drift from what the code actually offers:

```bash
demarchy-setup catalogue
demarchy-setup catalogue --json
```

## Limits

- Do not hand-edit `~/.config/demarchy/triggers.json`. The loader rejects unknown fields, so one typo makes the whole file unreadable and nothing in it is watched. Use `arm`, `edit` and `disarm`.
- A trigger is evaluated only on the connection it was armed on. When the user switches the active connection it shows as `other-connection` and waits. `edit` cannot move it; disarm and arm again.
- `dex.*` exists only with a DEX server registered in dcrpulse that carries a dcr_btc market; `dex.rate`, `dex.high24` and `dex.low24` are sats per DCR like `price.sats`, and `dex.premium` reads zero while the exchange feed is unavailable.
- `audit.*` needs the `audit` grant and `brmcp.*` the `brmcp` grant, each its own toggle in the dashboard's agent settings; without the grant the section is absent and its paths read no sample. `audit.entries` is dcrpulse's in-memory ring of every agent's write attempts, empty after a dcrpulse restart; `brmcp.pending` is the queue of bot payments waiting for the person's approval, and `brmcp.pending appears` is how you hear about one. You are being informed: nothing in demarchy or in this skill approves or denies a payment, and you must never try to. The person answers in the dashboard.
- `lightning.list` holds only the 6 largest channels by capacity. A channel pushed out of the top 6 reads as a disappearance, and one that climbs into it reads as an appearance. `--where alias=<name>` keeps other channels from ringing you, but that channel still reads as gone if it drops out of the six.
