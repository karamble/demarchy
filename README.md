# Demarchy

> **de·mar·chy** *(n.)*: government by officials picked at random rather than
> elected. The Athenians ran most of their city this way.
>
> It is also, rather wonderfully, how Decred works. Every block, five tickets are
> drawn from the live pool and their holders vote on whether the last block
> stands. No campaigning, no queue-jumping: just the lottery.
>
> And the drawn tickets are not there to rubber-stamp. Three of five against and
> a miner's block loses its transactions. They gate every change to the consensus
> rules, and off-chain they choose what the treasury pays for.

A Decred mark for the [Omarchy](https://omarchy.org/) bar. Click it and the first
thing you see is your own staking record: tickets live, votes cast, DCR earned,
and when one of yours last voted. Under that, the network you are voting in:
ticket price and where it is heading, the pool, the staked supply, the DCR price,
your Lightning channels and Bison Relay.

The mark lights up when new messages arrive in
[Bison Relay](https://github.com/companyzero/bisonrelay), the zero-knowledge
messenger built on Decred.

![preview](preview.png)

Everything comes from [dcrpulse](https://github.com/karamble/dcrpulse) over its
MCP interface, with a token that can only read.

## Install

```bash
omarchy plugin add https://github.com/karamble/demarchy.git --enable
cd ~/.config/omarchy/plugins/karamble.demarchy && make build
```

**The second line matters.** No binaries are shipped here, so the two small Go
helpers are compiled on your own machine. It needs Go 1.24 or newer and builds
nothing else. The panel tells you if you skip it.

Then add a connection, flip the switch in the panel, and you are done.

## Connections

Got more than one dcrpulse? A home box and a VPS, say. Demarchy holds as many as
you like: the active one is named in the panel footer, and `n` opens the list.

Add, edit and remove them in **Settings → Connections**, or from a terminal:

```bash
demarchy-setup              # add one
demarchy-setup list         # what is stored
demarchy-setup check        # re-validate them
demarchy-setup remove vps   # delete one and its token
```

They live in `~/.config/demarchy/connections.json` at mode 0600.

**Anything that isn't localhost has to be https**, since the token rides along on
every request. Type a bare remote host and it gets https automatically.

## The token

Each connection carries its own MCP token. In the dcrpulse dashboard, under
**Settings → AI Agents**, make an agent like this:

```
domains:      node, staking, bisonrelay, wallet
              (add lightning, treasury or dex if you want those bits)
write scopes: (none)
spend:        do not grant
allowed IPs:  127.0.0.1
```

**Anything that can write or spend is turned down.** A thing that lives in your
status bar has no business moving your money.

**Use the allowed-IPs field.** It is an IP whitelist, and it is the control that
makes a stolen token worthless: dcrpulse will only accept it from an address you
listed. For a dcrpulse on this machine that is `127.0.0.1`. For one across a
tunnel or a VPN, list the address your machine actually reaches it from.

A token typed into the settings page passes through the Quickshell process on
its way to the helper: down a pipe, never an argument list or the environment.
If you would rather it never touched the shell at all, add the connection from a
terminal.

## Removing it

```bash
demarchy-setup purge              # delete every stored connection and its token
omarchy plugin remove karamble.demarchy
```

Do the purge first. Removing the plugin takes the plugin folder away but leaves
`~/.config/demarchy/` behind, and what is in there is bearer tokens.

Purging deletes them from this machine; it does not revoke them. If you want a
token dead everywhere, delete its agent in the dcrpulse dashboard under
**Settings, AI Agents**.

## What you see depends on the token

Demarchy shows exactly what your token may read, so granting another domain in
the dashboard makes a section appear on its own.

| domain | what turns up |
|---|---|
| `node` | chain status, height, peers |
| `staking` | your staking record, ticket price, countdown, pool, staked supply |
| `bisonrelay` | messages, the unread badge, and the DCR price |
| `wallet` | balances, hidden until you ask for them |
| `lightning` | channel inbound/outbound |
| `treasury` | the treasury, in dollars |
| `dex` | the DCR/BTC chart, if you have a DEX account |

`node` and `staking` are the two you really want. `demarchy-setup check` tells
you what your actual token unlocks.

## Settings

| | default | |
|---|---|---|
| `monitoring` | off | A proper off switch: off means no connection is opened at all |
| `countPrivate` | on | Private messages raise the unread count |
| `countGroupchat` | on | So do group messages, so a lively group will keep the mark lit |
| `showBalances` | off | Whether balances appear; masked until you reveal them |
| `activeConnection` | first stored | Set by the footer switcher |

## Keys

Omarchy is keyboard-first, and so is this.

| | |
|---|---|
| `n` | connection switcher |
| `s` | settings, and back |
| `m` | monitoring on/off |
| `b` | reveal balances |
| `r` | refresh now |
| `↑ ↓` `enter` | move and choose |
| `esc` | close |

The bar item does one thing: click opens the panel. No hidden right- or
middle-click tricks. Wire up your own keybinds if you like:

```bash
omarchy-shell karamble.demarchy toggle
omarchy-shell karamble.demarchy connections
omarchy-shell karamble.demarchy useConnection vps
omarchy-shell karamble.demarchy toggleMonitor
```

## Requirements

- Omarchy 4.x with the Quickshell bar
- A dcrpulse you can reach, with its MCP interface on
- Go 1.24+ to build: the helpers use nothing outside the standard library

`bin/demarchy --demo` prints a made-up snapshot and holds it open, for laying out
the staking panel on a wallet that has never bought a ticket.

♥ to [DHH](https://github.com/dhh) and [Omarchy](https://github.com/basecamp/omarchy).

ISC licensed, as Decred software is. See `LICENSE`.
