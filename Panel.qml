// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Bar widget and its dropdown, in one file. `Panel` (qs.Ui) is used directly as
// the barWidget entry point: the shape omarchy.power/network/audio use. It
// already supplies open/close/toggle/opened, setting() and the popout
// coordination Bar.findPanelWidget requires.
//
// All the work happens in bin/demarchy: the Quickshell process is shared
// with the whole bar, so nothing that can block belongs here. This file reads
// NDJSON from that helper and draws it.
Panel {
  id: root
  moduleName: "karamble.demarchy"
  // Ui/Panel's own IpcHandler is gated on `manageIpc && ipcTarget !== ""`.
  // Clearing the target is what actually silences it, so this file can own the
  // single handler that target permits and add methods of its own.
  ipcTarget: ""
  manageIpc: false

  // ---- settings. Every fallback mirrors a manifest default: a mismatch
  // leaves a setting looking on and behaving off.
  readonly property bool monitoring: Style.boolToken(setting("monitoring", false), false)
  readonly property bool countPrivate: Style.boolToken(setting("countPrivate", true), true)
  readonly property bool countGroupchat: Style.boolToken(setting("countGroupchat", true), true)
  readonly property bool showBalances: Style.boolToken(setting("showBalances", false), false)
  readonly property string endpoint: setting("endpoint", "http://127.0.0.1:8090")

  // The shell.json write is async; hold the intended value so the switch moves
  // on the click rather than a beat later.
  property var pendingMonitoring: null
  readonly property bool effectiveMonitoring: pendingMonitoring !== null
                                              ? pendingMonitoring === true
                                              : monitoring

  // ---- state fed by the helper
  property var snap: null
  // Which view the card is showing: "dashboard", "settings" or "alerts". A
  // string rather than a flag because there are more than two of them, and
  // the cursor has to know whose rows it is walking.
  property string view: "dashboard"
  // Kept as a derived read so the gear, its tooltip and the settings page can
  // go on asking the simple question.
  readonly property bool settingsOpen: root.view === "settings"
  property bool balancesRevealed: false
  // Keyboard cursor into the active view's rows; -1 means nothing is focused.
  property int cursor: -1
  // The view that owns the cursor, or null while the dashboard is up and there
  // are no rows to walk.
  readonly property var activeView: root.view === "settings" ? settingsView
                                    : root.view === "alerts" ? alertsView : null
  // Wall clock for the freshness line. It ticks only while the panel is open,
  // because nothing else reads it.
  property double now: Date.now()
  property int helperRetries: 0
  // Set when the helper cannot be started at all. Installing from the
  // marketplace clones the repository but builds nothing, so this is the state
  // a first-time install lands in, and it needs to say so rather than sit on
  // "connecting" for ever.
  property bool helperMissing: false

  readonly property bool reachable: !!snap && snap.reachable === true
  readonly property string errorCode: snap && snap.error ? String(snap.error) : ""
  readonly property int unreadTotal: (snap && snap.unread) ? (Number(snap.unread.total) || 0) : 0

  // The connection list arrives in every snapshot with the tokens stripped, so
  // the switcher can be drawn without this process ever reading the file they
  // live in.
  readonly property var connections: (snap && snap.connections) ? snap.connections : []
  readonly property string activeConnection: (snap && snap.activeConnection) ? snap.activeConnection : ""
  readonly property string activeName: {
    for (var i = 0; i < root.connections.length; i++)
      if (root.connections[i].id === root.activeConnection) return root.connections[i].name
    return ""
  }
  readonly property var connResult: (snap && snap.connResult) ? snap.connResult : null
  // The plain-http exception, if one is set, and the reason connections.json
  // could not be read, if it could not. Both are read-only here: this is an
  // expert setting and the panel states it rather than offering to change it.
  readonly property var plainHttp: (snap && snap.plainHttp) ? snap.plainHttp : null
  readonly property string configError: (snap && snap.configError) ? snap.configError : ""

  // ---- triggers. While monitoring is on the running helper evaluates them
  // and the snapshot is the truth. While it is off there is no helper, so the
  // one-shot's last `list` reply stands in. The catalogue only ever comes from
  // the one-shot: the snapshot does not carry it.
  property var listedTriggers: []
  property string listedTriggersError: ""
  property var catalogue: []
  readonly property var triggers: (root.effectiveMonitoring && root.snap)
                                  ? (root.snap.triggers || []) : root.listedTriggers
  readonly property string triggersError: (root.effectiveMonitoring && root.snap)
                                          ? (root.snap.triggersError ? String(root.snap.triggersError) : "")
                                          : root.listedTriggersError
  // What the bar counts when monitoring is off: armed, waiting to re-arm, or
  // armed on a section the helper has no sample for. All three want watching.
  readonly property int armedCount: {
    var n = 0
    for (var i = 0; i < root.triggers.length; i++)
      if (Model.alertArmed(String(root.triggers[i].status))) n++
    return n
  }
  // herdr pane id -> { pane, status, title }, and the same entry again under
  // the agent's display name when it has one, since a trigger may be armed
  // with either. Empty when herdr is not installed, which is a normal state
  // rather than a failure.
  property var agentStates: ({})
  // The one-shot's answer to the last arm, edit or disarm. The form shows it
  // under itself when it was a refusal.
  property var triggerResult: null

  // The settings rows grow and shrink with the connection list, and the alert
  // rows with the triggers, so a cursor parked at the bottom has to come back
  // inside when one goes away.
  onConnectionsChanged: root.clampCursor()
  onTriggersChanged: root.clampCursor()

  function clampCursor() {
    if (root.activeView && root.cursor >= root.activeView.rowCount)
      root.cursor = root.activeView.rowCount - 1
  }

  // The cursor is a property, not focus, so the row holding it is found by
  // asking: every row and add button carries hasCursor. The first hit walking
  // down from the column is the row itself, since a row is checked before
  // the buttons inside it.
  function cursorItem(item) {
    if (!item || !item.visible) return null
    if (item.hasCursor === true) return item
    var kids = item.children || []
    for (var i = 0; i < kids.length; i++) {
      var hit = root.cursorItem(kids[i])
      if (hit) return hit
    }
    return null
  }
  function revealCursor() {
    if (root.cursor < 0) return
    flick.revealItem(root.cursorItem(column))
  }
  onCursorChanged: Qt.callLater(root.revealCursor)
  onViewChanged: flick.contentY = 0

  readonly property string pluginDir: String(Qt.resolvedUrl(".")).replace(/^file:\/\//, "").replace(/\/$/, "")
  readonly property string helperPath: pluginDir + "/bin/demarchy"

  // One colour for the whole status indicator: accent when it is working,
  // urgent when it is not, and simply dim when it is off on purpose: a paused
  // widget is not a broken one.
  readonly property color statusColor: {
    if (!root.effectiveMonitoring) return Color.popups.text
    if (root.errorCode !== "") return Color.urgent
    if (!root.reachable) return Color.popups.text
    return Color.accent
  }

  readonly property string tokenState: {
    if (root.errorCode === "no-token") return "missing"
    if (root.errorCode === "auth") return "rejected"
    if (root.reachable) return "ok"
    return "unknown"
  }

  // Off and unreachable both read as a dimmed mark; the panel is what tells
  // them apart.
  readonly property real markOpacity: (root.effectiveMonitoring && root.reachable) ? 1.0 : 0.35

  implicitWidth: button.implicitWidth + (countText.visible ? countText.implicitWidth + Style.space(4) : 0)
  implicitHeight: button.implicitHeight

  // ---- the helper process

  function handleLine(line) {
    if (!line) return
    var parsed
    try {
      parsed = JSON.parse(line)
    } catch (e) {
      return
    }
    // Refuse a shape we do not understand rather than drawing nonsense.
    if (!parsed || typeof parsed !== "object" || Number(parsed.v) !== 2) return
    root.snap = parsed
    root.helperRetries = 0
    root.helperMissing = false
    // A confirmed add or edit closes the form; a refused one leaves it open
    // with the reason under it, so the entry can be corrected rather than
    // retyped from scratch.
    if (parsed.connResult && parsed.connResult.ok && settingsView.connectionsView)
      settingsView.connectionsView.cancel()
  }

  function markRead() {
    if (helperProc.running) helperProc.write("markRead\n")
  }

  function refreshNow() {
    if (helperProc.running) helperProc.write("refresh\n")
  }

  // Connection commands travel as JSON over the helper's stdin. That is a pipe,
  // so a bearer token typed into the settings form never reaches argv or the
  // environment on its way to the 0600 file the helper writes.
  function sendCommand(obj) {
    if (!helperProc.running) return false
    helperProc.write(JSON.stringify(obj) + "\n")
    return true
  }

  function switchConnection(id) {
    if (!id || id === root.activeConnection) return
    // Persist first so the choice survives a restart, then tell the running
    // helper so it does not have to be restarted to notice.
    root.persist({ activeConnection: id })
    root.sendCommand({ cmd: "switch", id: id })
  }

  Process {
    id: helperProc
    running: root.effectiveMonitoring
    command: [root.helperPath, "--messages", "40"]
    stdinEnabled: true
    stdout: SplitParser {
      splitMarker: "\n"
      onRead: function (line) { root.handleLine(line) }
    }
    onExited: function (exitCode) {
      // The helper re-reads the monitoring switch from shell.json itself, so
      // right after the switch is flipped on it can start before the write has
      // landed and exit immediately. Retry a few times rather than leaving the
      // widget dark until the next toggle.
      if (root.effectiveMonitoring && root.helperRetries < 5) {
        root.helperRetries++
        restartTimer.restart()
        return
      }
      // Note: a binary that does not exist never reaches here. Quickshell logs
      // "Process failed to start" and emits no exited signal at all, so the
      // missing-helper case is caught by startupProbe below instead.
    }
  }

  // Quickshell emits no exited signal when a binary is missing: it warns once
  // and leaves `running` false, so nothing downstream ever hears about it. Ask
  // the shell instead. `test` always starts and always answers, which turns a
  // silent non-event into an exit code.
  function probeHelper() {
    if (helperProbe.running) return
    helperProbe.command = ["test", "-x", root.helperPath]
    helperProbe.running = true
  }

  Process {
    id: helperProbe
    running: false
    onExited: function (code) {
      root.helperMissing = code !== 0
      // With monitoring off nothing else ever reads the triggers file, and the
      // bar has to know whether anything armed is going unwatched. Only once
      // the helper is known to exist: a missing binary logs a warning per
      // attempt and answers nothing.
      if (code === 0 && !root.effectiveMonitoring) root.triggerCommand({ cmd: "list" })
    }
  }

  Component.onCompleted: root.probeHelper()

  Timer {
    id: restartTimer
    interval: 1500
    repeat: false
    onTriggered: if (root.effectiveMonitoring && !helperProc.running) helperProc.running = true
  }

  Process { id: setupProc }

  // Connection changes go to a one-shot helper, not to the running one. The
  // running helper only exists while monitoring is on, so routing them through
  // it meant the settings page did nothing at all whenever it was off, which is
  // exactly when a first connection is being added.
  property var pendingApply: null

  function applyConnection(obj) {
    if (applyProc.running) return
    root.pendingApply = obj
    applyProc.command = [root.helperPath, "--apply"]
    applyProc.running = true
  }

  Process {
    id: applyProc
    running: false
    stdinEnabled: true
    stdout: StdioCollector { id: applyOut; waitForEnd: true }
    onStarted: {
      write(JSON.stringify(root.pendingApply) + "\n")
      root.pendingApply = null
    }
    onExited: {
      var res = null
      try { res = JSON.parse(String(applyOut.text || "").trim()) } catch (e) { res = null }
      root.applyResult = res
      if (res && res.ok) {
        settingsView.connectionsView.cancel()
        if (res.op === "removeConnection" && res.id === root.activeConnection) {
          // The connection being watched just went away. The one-shot has no
          // live session to fall back for us, so pick the successor here: the
          // first one left, or nothing at all if that was the last.
          var next = ""
          for (var i = 0; i < root.connections.length; i++) {
            if (root.connections[i].id !== res.id) {
              next = root.connections[i].id
              break
            }
          }
          root.persist({ activeConnection: next })
          if (next !== "") root.sendCommand({ cmd: "switch", id: next })
          else root.refreshNow()
        } else {
          // The running helper is holding a list that just changed.
          root.refreshNow()
        }
      }
    }
  }

  // The one-shot's answer, which outranks whatever the running helper last
  // said, because it is the reply to what the user just did.
  property var applyResult: null

  // Trigger commands go to their own one-shot, for the same reason connection
  // changes do: the alerts page has to work while monitoring is off, and a
  // trigger armed while it is off is the very case the bar warns about.
  property var pendingTrigger: null
  // Which command the reply in flight answers. A list reply and an arm reply
  // are told apart by what was asked, not by guessing at their shape.
  property string triggerCmd: ""

  function triggerCommand(obj) {
    if (triggersProc.running) return
    // A fresh command retires the last refusal, so a reopened form does not
    // start out under an old message.
    if (obj.cmd !== "list") root.triggerResult = null
    root.pendingTrigger = obj
    root.triggerCmd = String(obj.cmd)
    triggersProc.command = [root.helperPath, "--triggers"]
    triggersProc.running = true
  }

  // The list and the agent states together: the page shows one beside the
  // other, so they should be of the same moment.
  function refreshAlerts() {
    root.triggerCommand({ cmd: "list" })
    root.refreshAgents()
  }

  Process {
    id: triggersProc
    running: false
    stdinEnabled: true
    stdout: StdioCollector { id: triggersOut; waitForEnd: true }
    onStarted: {
      write(JSON.stringify(root.pendingTrigger) + "\n")
      root.pendingTrigger = null
    }
    onExited: {
      var res = null
      try { res = JSON.parse(String(triggersOut.text || "").trim()) } catch (e) { res = null }
      if (root.triggerCmd === "list") {
        if (res && res.ok) {
          root.listedTriggers = res.triggers || []
          root.catalogue = res.catalogue || []
          root.listedTriggersError = ""
        } else {
          root.listedTriggersError = res ? Model.alertError(res) : "The helper gave no answer."
        }
        root.refreshAgents()
        return
      }
      root.triggerResult = res
      if (res && res.ok) {
        // The store was written. Only now does the form close, never on the
        // click, and the list is read back so the page shows what was written
        // rather than what was typed. The re-run waits a beat because this
        // process is still winding down. The running helper, if there is one,
        // is holding a list that just changed.
        alertsView.list.cancel()
        Qt.callLater(function () { root.triggerCommand({ cmd: "list" }) })
        root.refreshNow()
      }
    }
  }

  // Agent liveness comes from herdr, read-only. A recipient it does not list
  // is not running, which the page says beside the group.
  function refreshAgents() {
    if (herdrProc.running) return
    herdrProc.running = true
  }

  Process {
    id: herdrProc
    running: false
    command: ["herdr", "agent", "list"]
    stdout: StdioCollector { id: herdrOut; waitForEnd: true }
    onExited: function (code) {
      // A missing herdr never gets here (see helperProc) and a failing one
      // exits non-zero. Either way there is nothing to show, and that is a
      // normal state, so the map is simply left empty.
      var states = {}
      if (code === 0) {
        var parsed = null
        try { parsed = JSON.parse(String(herdrOut.text || "").trim()) } catch (e) { parsed = null }
        var agents = (parsed && parsed.result && parsed.result.agents) ? parsed.result.agents : []
        for (var i = 0; i < agents.length; i++) {
          var a = agents[i]
          if (!a || !a.pane_id) continue
          // A renamed agent carries display_agent: its label, and a second
          // address a trigger may name. Unset, the key is absent altogether.
          var name = (typeof a.display_agent === "string") ? a.display_agent : ""
          var entry = {
            pane: String(a.pane_id),
            status: String(a.agent_status || "unknown"),
            title: String(name || a.terminal_title_stripped || a.terminal_title || a.pane_id)
          }
          states[entry.pane] = entry
          if (name !== "") states[name] = entry
        }
      }
      root.agentStates = states
    }
  }

  Timer {
    interval: 5000
    repeat: true
    running: root.opened
    triggeredOnStart: true
    onTriggered: root.now = Date.now()
  }

  // ---- persistence

  function persist(values) {
    var entry = { id: root.moduleName }
    for (var existing in root.settings)
      if (existing !== "id") entry[existing] = root.settings[existing]
    for (var key in values) entry[key] = values[key]
    root.settings = entry
    if (root.bar && root.bar.shell && typeof root.bar.shell.updateEntryInline === "function")
      root.bar.shell.updateEntryInline(root.moduleName, entry)
  }

  // Removing a connection deletes a stored credential, and disarming an alert
  // takes away something an agent is waiting on, so both take two decisions.
  // The dialog lives here rather than in the pages because it has to cover the
  // whole card. kind is "connection" or "alert"; name is what the question
  // shows.
  property var pendingConfirm: null

  function askConfirm(kind, id, name) {
    root.pendingConfirm = { kind: kind, id: id, name: name }
  }

  function askDisarm(id, label) {
    root.askConfirm("alert", id, label)
  }

  function confirmPending() {
    var p = root.pendingConfirm
    root.pendingConfirm = null
    if (!p) return
    if (p.kind === "alert") root.triggerCommand({ cmd: "disarm", id: p.id })
    else root.applyConnection({ cmd: "removeConnection", id: p.id })
  }

  // The single choke point for the kill switch: panel toggle, middle-click and
  // IPC all land here.
  function setMonitoring(value) {
    var v = value === true
    root.pendingMonitoring = v
    root.helperRetries = 0
    if (v) root.probeHelper()
    if (!v) {
      root.snap = null
      // The snapshot was the source of the trigger list; the one-shot is now.
      root.triggerCommand({ cmd: "list" })
    }
    persist({ monitoring: v })
  }

  onSettingsChanged: {
    if (root.pendingMonitoring !== null && root.monitoring === root.pendingMonitoring)
      root.pendingMonitoring = null
  }

  // Every change of view goes through here so the cursor travels with it: the
  // dashboard has no rows, so it parks at -1; any other view starts on its
  // first row.
  function setView(v) {
    root.view = v
    root.cursor = (v === "dashboard") ? -1 : 0
  }

  onOpenedChanged: {
    if (root.opened) {
      root.probeHelper()
      root.markRead()
    } else {
      // Re-mask on close, so a revealed balance never survives into the next
      // time the panel is opened; and drop any half-typed form for the same
      // reason, or the next open lands in it with a dropdown holding focus.
      root.balancesRevealed = false
      settingsView.cancelForms()
      alertsView.list.cancel()
      root.setView("dashboard")
    }
  }

  // Opening the switcher is its own action so a keybind can reach it, and so
  // the "n" key and the trigger share one path.
  function openSwitcher() {
    if (root.connections.length === 0) return
    if (!root.opened) root.open()
    root.setView("dashboard")
    Qt.callLater(function () {
      var scene = connTrigger.mapToGlobal(0, 0)
      connSwitcher.openAt(scene.x, scene.y)
    })
  }

  function openSettings() {
    root.setView("settings")
    if (!root.opened) root.open()
  }

  function openAlerts() {
    root.setView("alerts")
    if (!root.opened) root.open()
  }

  IpcHandler {
    target: "karamble.demarchy"

    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function settings(): void { root.openSettings() }
    function alerts(): void { root.openAlerts() }
    function connections(): void { root.openSwitcher() }
    function connection(): string { return root.activeConnection }
    function useConnection(id: string): void { root.switchConnection(id) }
    function monitorOn(): void { root.setMonitoring(true) }
    function monitorOff(): void { root.setMonitoring(false) }
    function toggleMonitor(): void { root.setMonitoring(!root.effectiveMonitoring) }
    function monitoring(): string { return root.effectiveMonitoring ? "on" : "off" }
    function unread(): string { return String(root.unreadTotal) }
    function refresh(): void { root.refreshNow() }
    function state(): string {
      return JSON.stringify({
        monitoring: root.effectiveMonitoring,
        reachable: root.reachable,
        error: root.errorCode,
        unread: root.unreadTotal,
        settingsOpen: root.settingsOpen,
        view: root.view,
        opened: root.opened,
        showBalances: root.showBalances
      })
    }
  }

  // ---- bar item

  BarIconButton {
    id: button
    anchors.left: parent.left
    anchors.verticalCenter: parent.verticalCenter
    bar: root.bar
    slotSize: Style.bar.statusSlot
    // Unread paints the mark in bar.urgent through the base's 160ms fade. So
    // does an armed alert that nothing is watching: off is a choice, but one
    // the bar should not let go quiet while an agent is waiting on a trigger.
    active: root.unreadTotal > 0 || (!root.effectiveMonitoring && root.armedCount > 0)
    tooltipText: {
      if (!root.effectiveMonitoring) {
        if (root.armedCount > 0)
          return "Demarchy: monitoring off, " + root.armedCount
                 + (root.armedCount === 1 ? " alert" : " alerts") + " not watched"
        return "Demarchy: monitoring off"
      }
      if (root.errorCode !== "") return "Demarchy, " + Model.errorLine(root.errorCode, root.snap ? root.snap.detail : "")
      if (!root.reachable) return "Demarchy: connecting"
      var line = "Decred · " + Model.nodeLine(root.snap ? root.snap.node : null)
      if (root.unreadTotal > 0) line += "\n" + root.unreadTotal + " unread"
      return line
    }
    iconComponent: Component {
      Item {
        DecredIcon {
          anchors.centerIn: parent
          iconSize: Style.space(11)
          color: button.active && button.useActiveColor ? button.activeColor : button.foreground
          opacity: root.markOpacity
        }
      }
    }
    // The bar item does one thing: open the panel. Everything else lives
    // behind a key or a visible control, so no behaviour is hidden in a mouse
    // button nobody thinks to press.
    onPressed: function (b) { root.toggle() }
  }

  Text {
    id: countText
    anchors.left: button.right
    anchors.leftMargin: Style.space(4)
    anchors.verticalCenter: parent.verticalCenter
    visible: root.unreadTotal > 0 && !root.vertical
    text: String(root.unreadTotal)
    textFormat: Text.PlainText
    color: root.bar ? root.bar.urgent : Color.urgent
    font.family: root.bar ? root.bar.fontFamily : Style.font.family
    font.pixelSize: Style.font.caption
    TapHandler { onTapped: root.toggle() }
  }

  // ---- the dropdown

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    // Two columns rather than one long strip: with a generous grant the
    // single-column card ran past the bottom of the screen and lost its
    // footer. Widening costs nothing here: the bar is at the top and there is
    // far more room sideways than down.
    contentWidth: panel.fittedContentWidth(Style.space(620))
    contentHeight: panel.fittedContentHeight(column.implicitHeight, Style.space(780))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      // A focused form field owns every key, so typing a name or a token does
      // not trip the panel's letter shortcuts.
      blocked: !!root.activeView && root.activeView.formFocused
      onCloseRequested: {
        if (root.pendingConfirm) root.pendingConfirm = null
        else root.close()
      }
      onTabRequested: function (direction) { root.switchPanel(direction) }

      // Omarchy is keyboard-first, so every action here has a key. The mouse
      // is a convenience, never the only way in.
      onTextKey: function (t) {
        switch (String(t).toLowerCase()) {
        case "s":
          root.setView(root.view === "settings" ? "dashboard" : "settings")
          break
        case "a":
          root.setView(root.view === "alerts" ? "dashboard" : "alerts")
          break
        case "m":
          root.setMonitoring(!root.effectiveMonitoring)
          break
        case "b":
          if (root.showBalances && root.reachable)
            root.balancesRevealed = !root.balancesRevealed
          break
        case "r":
          root.refreshNow()
          // On the alerts page a refresh is also a re-read of the list and of
          // who is there to receive it.
          if (root.view === "alerts") root.refreshAlerts()
          break
        case "n":
          // Opened from the keyboard, so there is no pointer to hang it off;
          // anchor it to the trigger, which is where it would have appeared.
          root.openSwitcher()
          break
        }
      }
      onMoveRequested: function (dx, dy) {
        if (!root.activeView) return
        if (dy !== 0) {
          var n = root.activeView.rowCount
          root.cursor = root.cursor < 0 ? (dy > 0 ? 0 : n - 1)
                                        : (root.cursor + dy + n) % n
          return
        }
        if (dx !== 0) root.activeView.moveAction(dx)
      }
      onActivateRequested: {
        if (root.pendingConfirm) { root.confirmPending(); return }
        if (root.activeView && root.cursor >= 0) root.activeView.activateRow(root.cursor)
      }

      ConfirmDialog {
        id: confirmDialog
        anchors.fill: parent
        // Without a z the dialog paints in declaration order, so the settings
        // content lands on top of it and the scrim looks see-through.
        z: 20
        opened: !!root.pendingConfirm
        message: {
          var p = root.pendingConfirm
          if (!p) return ""
          if (p.kind === "alert") return "Disarm \"" + p.name + "\"?"
          return "Remove \"" + p.name + "\"? Its stored token is deleted too."
        }
        confirmText: (root.pendingConfirm && root.pendingConfirm.kind === "alert") ? "Disarm" : "Remove"
        foreground: Color.popups.text
        background: Color.popups.background
        scrim: Util.alpha(Color.popups.background, 0.85)
        selectedText: Color.urgent
        onConfirmed: root.confirmPending()
        onCanceled: root.pendingConfirm = null
      }

      ConnectionSwitcher {
        id: connSwitcher
        connections: root.connections
        activeId: root.activeConnection
        onChosen: function (id) { root.switchConnection(id) }
        onManageRequested: root.setView("settings")
      }
      onReturnRequested: {
        if (root.activeView && root.cursor >= 0) root.activeView.activateRow(root.cursor)
      }

      // The card grows with its content up to the screen; past that the
      // content scrolls instead of painting past the border, the way the
      // shell's own list popups do. Wheel and touchpad move it, the keyboard
      // cursor keeps its row in view, and a thin mark on the right says where
      // you are while there is more.
      Flickable {
        id: flick
        anchors.fill: parent
        contentWidth: width
        contentHeight: column.implicitHeight
        interactive: contentHeight > height
        boundsBehavior: Flickable.StopAtBounds
        clip: true

        // reveal scrolls just far enough for the band y..y+h of the column
        // to be inside the viewport, with a little air around it.
        function reveal(y, h) {
          var pad = Style.space(8)
          var maxY = Math.max(0, contentHeight - height)
          if (maxY === 0) { contentY = 0; return }
          if (y < contentY + pad) contentY = Math.max(0, y - pad)
          else if (y + h > contentY + height - pad) contentY = Math.min(maxY, y + h - height + pad)
        }
        // revealItem does the same for an item anywhere inside the column.
        function revealItem(item) {
          if (!item) return
          var p = item
          while (p && p !== column) p = p.parent
          if (!p) return
          var r = item.mapToItem(column, 0, 0)
          reveal(r.y, item.height)
        }
        // A focused form field, reached by Tab, is kept in view the same way.
        readonly property Item focused: Window.activeFocusItem
        onFocusedChanged: Qt.callLater(function () { flick.revealItem(flick.focused) })
        // Content that shrinks under the viewport must not leave a gap.
        onContentHeightChanged: {
          var maxY = Math.max(0, contentHeight - height)
          if (contentY > maxY) contentY = maxY
        }

        Column {
          id: column
          width: flick.width
          spacing: Style.space(8)

          // header. PanelHero is the stock title/pill/trailing-control row every
          // first-party panel uses; the switch and the gear ride in its trailing
          // slot, and the tooltip says what the switch will do.
          PanelHero {
            width: parent.width
            title: "Demarchy"
            foreground: Color.popups.text
            iconSize: Style.font.title
            // PanelHero's `detail` draws a bordered pill, which was the heaviest
            // thing in a header that is otherwise borderless. A dot and a word in
            // the trailing row say the same and sit quieter.
            detail: ""
            meta: root.reachable && root.snap ? Model.ago(root.snap.stamp, root.now) : ""

            iconComponent: Component {
              DecredIcon { iconSize: Style.font.title; monochrome: false }
            }

            trailingControl: Component {
              Row {
                spacing: Style.space(10)

                Row {
                  anchors.verticalCenter: parent.verticalCenter
                  spacing: Style.space(5)

                  Rectangle {
                    anchors.verticalCenter: parent.verticalCenter
                    width: Style.space(6)
                    height: width
                    radius: width / 2
                    color: root.statusColor
                  }
                  Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: Model.pill(root.effectiveMonitoring, root.reachable, root.errorCode).toLowerCase()
                    textFormat: Text.PlainText
                    color: root.statusColor
                    opacity: 0.8
                    font.family: Style.font.family
                    font.pixelSize: Style.font.caption
                  }
                }

                // The bell opens the alerts board the way the gear opens
                // settings. It turns urgent for the one state the bar mark also
                // shows: alerts armed while nothing is watching them.
                Text {
                  anchors.verticalCenter: parent.verticalCenter
                  text: "\uf0f3"
                  color: (!root.effectiveMonitoring && root.armedCount > 0) ? Color.urgent : Color.popups.text
                  opacity: root.view === "alerts" ? 0.9 : (bellHover.hovered ? 0.8 : 0.45)
                  font.family: Style.font.family
                  font.pixelSize: Style.font.icon
                  HoverHandler { id: bellHover; cursorShape: Qt.PointingHandCursor }
                  TapHandler {
                    onTapped: root.setView(root.view === "alerts" ? "dashboard" : "alerts")
                  }
                  PanelToolTip {
                    visible: bellHover.hovered
                    text: {
                      if (root.view === "alerts") return "Back to status  (a)"
                      if (!root.effectiveMonitoring && root.armedCount > 0)
                        return "Alerts: " + root.armedCount + " armed, not watched  (a)"
                      return "Alerts  (a)"
                    }
                  }
                }

                Text {
                  anchors.verticalCenter: parent.verticalCenter
                  text: "\uf013"
                  color: Color.popups.text
                  opacity: root.settingsOpen ? 0.9 : (gearHover.hovered ? 0.8 : 0.45)
                  font.family: Style.font.family
                  font.pixelSize: Style.font.icon
                  HoverHandler { id: gearHover; cursorShape: Qt.PointingHandCursor }
                  TapHandler {
                    onTapped: root.setView(root.view === "settings" ? "dashboard" : "settings")
                  }
                  PanelToolTip {
                    visible: gearHover.hovered
                    text: root.settingsOpen ? "Back to status  (s)" : "Settings  (s)"
                  }
                }

                ToggleSwitch {
                  id: monitorSwitch
                  anchors.verticalCenter: parent.verticalCenter
                  checked: root.effectiveMonitoring
                  foreground: Color.popups.text
                  onToggled: root.setMonitoring(!root.effectiveMonitoring)

                  PanelToolTip {
                    visible: monitorSwitch.containsMouse
                    text: Model.switchHint(root.effectiveMonitoring) + "  (m)"
                  }
                }
              }
            }
          }

          // an explanation whenever there is nothing good to draw
          Text {
            width: parent.width
            visible: text !== "" && root.view === "dashboard"
            wrapMode: Text.WordWrap
            text: {
              if (!root.effectiveMonitoring) return "Monitoring is off. The switch above turns it on."
              if (root.helperMissing)
                return "The helper is not built yet. In a terminal:\n\n"
                       + "    cd " + root.pluginDir + " && make build\n\n"
                       + "It needs Go 1.24 or newer, and builds nothing but the two "
                       + "helpers in bin/."
              if (root.errorCode !== "") return Model.errorLine(root.errorCode, root.snap ? root.snap.detail : "")
              if (!root.snap) return "Connecting to dcrpulse…"
              return ""
            }
            textFormat: Text.PlainText
            color: Color.popups.text
            opacity: 0.7
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }

          Text {
            width: parent.width
            visible: root.view === "dashboard" && root.errorCode !== "" && !!root.snap && root.snap.stamp > 0
            text: "Last reached " + Model.ago(root.snap ? root.snap.stamp : 0, root.now)
            textFormat: Text.PlainText
            color: Color.popups.text
            opacity: 0.45
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }

          // ---- the hero spans the full width: it is the headline, and the
          // three figures plus the curve want the room.
          StakingHero {
            id: hero
            width: parent.width
            visible: root.view === "dashboard" && root.reachable && !!root.snap && !!root.snap.staking
            own: root.snap && root.snap.staking ? root.snap.staking.own : null
            staking: root.snap ? root.snap.staking : null
            now: root.now
          }

          PanelSeparator {
            width: parent.width; foreground: Color.popups.text
            visible: columns.visible
          }

          // ---- two columns. Each section still appears only when the snapshot
          // carries its data, so a narrow grant simply leaves gaps rather than
          // empty headings.
          Row {
            id: columns
            width: parent.width
            spacing: Style.space(20)
            visible: root.view === "dashboard" && root.reachable && !!root.snap

            Column {
              id: leftColumn
              width: (columns.width - columns.spacing) / 2
              spacing: Style.space(10)

              GovernanceSection {
                width: parent.width
                visible: !!root.snap && !!root.snap.staking
                staking: root.snap ? root.snap.staking : null
                price: root.snap ? root.snap.price : null
                treasury: root.snap ? root.snap.treasury : null
              }
              NodeSection {
                id: nodeSection
                width: parent.width
                visible: !!root.snap && !!root.snap.node
                node: root.snap ? root.snap.node : null
              }
            }

            Column {
              id: rightColumn
              width: (columns.width - columns.spacing) / 2
              spacing: Style.space(10)

              PriceSection {
                id: priceSection
                width: parent.width
                visible: !!root.snap && (!!root.snap.price || !!root.snap.dex)
                price: root.snap ? root.snap.price : null
                dex: root.snap ? root.snap.dex : null
              }
              LightningSection {
                id: lightning
                width: parent.width
                visible: !!root.snap && !!root.snap.lightning
                lightning: root.snap ? root.snap.lightning : null
              }
              WalletSection {
                id: walletSection
                width: parent.width
                visible: root.showBalances && !!root.snap && !!root.snap.wallet
                wallet: root.snap ? root.snap.wallet : null
                revealed: root.balancesRevealed
                onToggleReveal: root.balancesRevealed = !root.balancesRevealed
              }
            }
          }

          PanelSeparator {
            width: parent.width; foreground: Color.popups.text
            visible: brSection.visible
          }
          BRSection {
            id: brSection
            width: parent.width
            visible: root.view === "dashboard" && root.reachable && !!root.snap && !!root.snap.br
            br: root.snap ? root.snap.br : null
            unread: root.snap ? root.snap.unread : null
            messageLimit: 3
          }

          // ---- settings view
          SettingsView {
            id: settingsView
            width: parent.width
            cursor: root.cursor
            visible: root.settingsOpen
            monitoring: root.effectiveMonitoring
            countPrivate: root.countPrivate
            countGroupchat: root.countGroupchat
            showBalances: root.showBalances
            endpoint: root.endpoint
            tokenState: root.tokenState
            connections: root.connections
            activeConnection: root.activeConnection
            connResult: root.applyResult ? root.applyResult : root.connResult
            plainHttp: root.plainHttp
            configError: root.configError
            onConnAdd: function (name, endpoint, token) {
              root.applyConnection({ cmd: "addConnection", name: name, endpoint: endpoint, token: token })
            }
            onConnEdit: function (id, name, endpoint, token) {
              root.applyConnection({ cmd: "editConnection", id: id, name: name, endpoint: endpoint, token: token })
            }
            onConnRemove: function (id, name) { root.askConfirm("connection", id, name) }
            onFocusReleased: keyCatcher.forceActiveFocus()
            onConnSwitch: function (id) { root.switchConnection(id) }
            onChanged: function (key, value) {
              if (key === "monitoring") root.setMonitoring(value)
              else {
                var patch = {}
                patch[key] = value
                root.persist(patch)
              }
            }
            onRunSetup: {
              setupProc.command = ["omarchy-launch-floating-terminal-with-presentation",
                                   root.pluginDir + "/bin/demarchy-setup"]
              setupProc.running = true
            }
          }

          // ---- alerts view. The page arms, edits and disarms through the
          // one-shot; disarming goes by way of the confirmation first.
          AlertsView {
            id: alertsView
            width: parent.width
            cursor: root.cursor
            visible: root.view === "alerts"
            triggers: root.triggers
            catalogue: root.catalogue
            triggersError: root.triggersError
            monitoring: root.effectiveMonitoring
            agentStates: root.agentStates
            activeConnection: root.activeConnection
            result: root.triggerResult
            now: root.now
            onArm: function (spec) { root.triggerCommand(Object.assign({ cmd: "arm" }, spec)) }
            onEdit: function (id, spec) { root.triggerCommand(Object.assign({ cmd: "edit", id: id }, spec)) }
            onDisarm: function (id, label) { root.askDisarm(id, label) }
            onRefreshRequested: root.refreshAlerts()
            onFocusReleased: keyCatcher.forceActiveFocus()
          }

          // ---- footer: which connection this is, and the keys
          Item {
            width: parent.width
            implicitHeight: Math.max(connTrigger.implicitHeight, hintText.implicitHeight)

            // The trigger, built by hand rather than from Button so it can carry
            // the four-state fill and light up while its menu is open.
            Item {
              id: connTrigger
              anchors.left: parent.left
              anchors.verticalCenter: parent.verticalCenter
              visible: root.connections.length > 0
              width: Math.min(parent.width * 0.55, connLabel.implicitWidth + Style.space(14))
              implicitHeight: Style.space(20)

              readonly property bool selected: connSwitcher.opened

              Rectangle {
                anchors.fill: parent
                radius: Style.cornerRadius
                color: connMouse.pressed
                       ? Style.selectedFillFor(Color.popups.text, Color.accent)
                       : (connTrigger.selected || connMouse.containsMouse
                          ? Style.hoverFillFor(Color.popups.text, Color.accent) : "transparent")
              }

              Text {
                id: connLabel
                anchors.left: parent.left
                anchors.leftMargin: Style.space(6)
                anchors.right: parent.right
                anchors.rightMargin: Style.space(6)
                anchors.verticalCenter: parent.verticalCenter
                text: "▴  " + (root.activeName !== "" ? root.activeName : "connection")
                textFormat: Text.PlainText
                elide: Text.ElideRight
                color: Color.popups.text
                opacity: connTrigger.selected || connMouse.containsMouse ? 0.9 : 0.5
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
              }

              MouseArea {
                id: connMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                  var scene = connTrigger.mapToGlobal(0, 0)
                  connSwitcher.openAt(scene.x, scene.y)
                }
              }
            }

            Text {
              id: hintText
              anchors.right: parent.right
              anchors.left: connTrigger.visible ? connTrigger.right : parent.left
              anchors.leftMargin: Style.space(10)
              anchors.verticalCenter: parent.verticalCenter
              horizontalAlignment: Text.AlignRight
              // One legend per view.
              text: {
                if (root.view === "settings")
                  return "↑↓ row · ←→ action · enter do · s back · esc close"
                if (root.view === "alerts")
                  return "↑↓ row · ←→ action · enter do · a back · esc close"
                return "n connection · s settings · a alerts · m monitor" +
                       (root.showBalances && root.reachable ? " · b reveal" : "") +
                       " · r refresh"
              }
              textFormat: Text.PlainText
              elide: Text.ElideLeft
              color: Color.popups.text
              opacity: 0.35
              font.family: Style.font.family
              font.pixelSize: Style.font.caption
            }
          }

        }
      }

      // The scroll mark: only while there is more than fits. It sits in the
      // card's padding, just inside the border, rather than on the content's
      // own edge where it would crowd the rows.
      Rectangle {
        anchors.right: parent.right
        anchors.rightMargin: -(panel.padding - Style.space(3))
        width: Style.space(2)
        radius: width / 2
        visible: flick.contentHeight > flick.height
        y: flick.visibleArea.yPosition * flick.height
        height: Math.max(Style.space(16), flick.visibleArea.heightRatio * flick.height)
        color: Color.popups.text
        opacity: 0.3
      }
    }
  }
}
