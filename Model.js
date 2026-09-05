// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

.pragma library

// Pure formatting and state helpers for the Demarchy panel. Kept out of the
// QML so it can be reasoned about, and tested, on its own.

// ---- numbers

function dcr(value, decimals) {
  var n = Number(value)
  if (!isFinite(n)) return "-"
  var d = decimals === undefined ? 2 : decimals
  return n.toLocaleString(Qt.locale(), "f", d) + " DCR"
}

function count(value) {
  var n = Number(value)
  if (!isFinite(n)) return "-"
  return Math.round(n).toLocaleString(Qt.locale(), "f", 0)
}

function percent(value, decimals) {
  var n = Number(value)
  if (!isFinite(n)) return "-"
  var d = decimals === undefined ? 1 : decimals
  return n.toFixed(d) + "%"
}

// mask renders a hidden balance. The width is fixed rather than proportional
// to the value: a mask whose length tracked the number would leak its
// magnitude, which is the one thing hiding it is meant to prevent.
function mask() {
  return "•••••"
}

// ---- node

function nodeLine(node) {
  if (!node) return "unknown"
  if (node.status !== "running") return node.status || "stopped"
  var p = Number(node.syncProgress)
  if (isFinite(p) && p < 100) return "syncing " + p.toFixed(1) + "%"
  return node.syncMessage || node.syncPhase || "synced"
}

function nodeHealthy(node) {
  return !!node && node.status === "running" && Number(node.syncProgress) >= 100
}

// ---- bison relay

// brLine describes the connection, not the message count.
function brLine(br) {
  if (!br) return "unknown"
  if (!br.stage) return "not connected"
  if (br.stage === "ready") return br.nick ? "connected as " + br.nick : "connected"
  return br.stage
}

// sender is what the panel shows as the author of a ring entry. Group traffic
// arriving through a relay bot carries the real author inside the text as
// "[m] <someone> ...", so prefer that over the bot's own nick.
function sender(msg) {
  if (!msg) return ""
  var m = /^\[m\]\s*<([^>]{1,64})>\s*/.exec(msg.text || "")
  if (m) return m[1]
  return msg.fromNick || "unknown"
}

function body(msg) {
  if (!msg) return ""
  return String(msg.text || "").replace(/^\[m\]\s*<[^>]{1,64}>\s*/, "")
}

function isGroup(msg) {
  return !!msg && (msg.type === "gcm" || msg.type === "gc-message")
}

// groupLabel names the group a message came from. The helper resolves the id to
// a name; a group it could not resolve still says what kind of message this is.
function groupLabel(msg) {
  if (!isGroup(msg)) return ""
  return "  \u00b7  " + (msg.gcName ? msg.gcName : "group")
}

// ---- errors

// errorLine turns the helper's machine-readable code into something the panel
// can show without the user needing to know what MCP is.
function errorLine(code, detail) {
  switch (code) {
  case "off":         return "Monitoring is off."
  case "no-token":    return "No token yet. Run the setup to connect this widget to dcrpulse."
  case "auth":        return "dcrpulse rejected the token. It may have been revoked."
  case "unreachable": return "dcrpulse is not reachable. Is the stack running?"
  case "":
  case undefined:
  case null:          return ""
  default:            return detail ? String(detail) : "Something went wrong."
  }
}

// ---- freshness

function ago(stamp, now) {
  var t = Number(stamp)
  if (!isFinite(t) || t <= 0) return "never"
  var secs = Math.max(0, Math.floor((now - t * 1000) / 1000))
  if (secs < 10) return "just now"
  if (secs < 60) return secs + "s ago"
  var mins = Math.floor(secs / 60)
  if (mins < 60) return mins + "m ago"
  var hours = Math.floor(mins / 60)
  if (hours < 24) return hours + "h ago"
  return Math.floor(hours / 24) + "d ago"
}

// pill is the short status word beside the title: what the widget is doing
// right now, in one glance.
function pill(monitoring, reachable, code) {
  if (!monitoring) return "PAUSED"
  if (code === "no-token") return "NO TOKEN"
  if (code === "auth") return "REJECTED"
  if (code === "unreachable") return "OFFLINE"
  if (!reachable) return "CONNECTING"
  return "LIVE"
}

// switchHint says what the switch will do, and when it is about to be turned
// off, that off really means off rather than merely quieter.
function switchHint(monitoring) {
  return monitoring
    ? "Pause monitoring: zero requests while off"
    : "Resume monitoring"
}

// ---- money and time, for the staking hero and network rows

// usd renders a dollar sum, compacting only above a million so ordinary
// balances keep their cents.
function usd(value) {
  var n = Number(value)
  if (!isFinite(n) || n <= 0) return "-"
  if (n >= 1e9) return "$" + (n / 1e9).toFixed(2) + "B"
  if (n >= 1e6) return "$" + (n / 1e6).toFixed(1) + "M"
  if (n >= 1e4) return "$" + Math.round(n).toLocaleString(Qt.locale(), "f", 0)
  return "$" + n.toFixed(2)
}

function sats(value) {
  var n = Number(value)
  if (!isFinite(n) || n <= 0) return "-"
  return Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " sats"
}

// compactDcr keeps large DCR figures readable in a narrow panel.
function compactDcr(value) {
  var n = Number(value)
  if (!isFinite(n)) return "-"
  if (n >= 1e6) return (n / 1e6).toFixed(2) + "M DCR"
  if (n >= 1e3) return Math.round(n).toLocaleString(Qt.locale(), "f", 0) + " DCR"
  return n.toFixed(2) + " DCR"
}

// signedDcr is for earnings, where the sign is the point.
function signedDcr(value, decimals) {
  var n = Number(value)
  if (!isFinite(n)) return "-"
  var d = decimals === undefined ? 2 : decimals
  return (n > 0 ? "+" : "") + n.toLocaleString(Qt.locale(), "f", d)
}

// delta describes the move from one price to the next.
function delta(from, to) {
  var a = Number(from), b = Number(to)
  if (!isFinite(a) || !isFinite(b) || a <= 0) return ""
  var pct = (b - a) / a * 100
  if (Math.abs(pct) < 0.005) return "unchanged"
  return (pct > 0 ? "▲ " : "▼ ") + Math.abs(pct).toFixed(1) + "%"
}

function deltaRising(from, to) { return Number(to) > Number(from) }

// hours renders the countdown to the next ticket price. Observed block times
// are far too noisy to average, so this is derived from the target spacing and
// is always shown as approximate.
function hoursAway(h) {
  var n = Number(h)
  if (!isFinite(n) || n <= 0) return "due now"
  if (n < 1) return "≈ " + Math.round(n * 60) + "m"
  var whole = Math.floor(n)
  var mins = Math.round((n - whole) * 60)
  if (mins === 0) return "≈ " + whole + "h"
  return "≈ " + whole + "h " + mins + "m"
}

// sinceUnix is "how long ago", from a unix timestamp.
function sinceUnix(stamp, now) {
  var t = Number(stamp)
  if (!isFinite(t) || t <= 0) return "never"
  return ago(t, now)
}

// connError turns the helper's connection-command answer into a sentence. The
// helper reports a stable code plus a detail; the codes are shared with the
// snapshot's own error field.
function connError(result) {
  if (!result || result.ok) return ""
  var detail = String(result.detail || "")
  switch (result.error) {
  case "auth":        return "That token was rejected by dcrpulse."
  case "unreachable": return "Could not reach that endpoint. Is dcrpulse running there?"
  case "no-token":    return "No token given."
  }
  return detail !== "" ? detail : "That did not work."
}
