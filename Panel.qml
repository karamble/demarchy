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
  property bool settingsOpen: false
  property bool balancesRevealed: false
  // Keyboard cursor into the settings rows; -1 means nothing is focused.
  property int cursor: -1
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
    onExited: function (code) { root.helperMissing = code !== 0 }
  }

  Timer {
    id: restartTimer
    interval: 1500
    repeat: false
    onTriggered: if (root.effectiveMonitoring && !helperProc.running) helperProc.running = true
  }

  Process { id: setupProc }

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

  // The single choke point for the kill switch: panel toggle, middle-click and
  // IPC all land here.
  function setMonitoring(value) {
    var v = value === true
    root.pendingMonitoring = v
    root.helperRetries = 0
    if (v) root.probeHelper()
    if (!v) root.snap = null
    persist({ monitoring: v })
  }

  onSettingsChanged: {
    if (root.pendingMonitoring !== null && root.monitoring === root.pendingMonitoring)
      root.pendingMonitoring = null
  }

  onOpenedChanged: {
    if (root.opened) {
      root.probeHelper()
      root.markRead()
    } else {
      // Re-mask on close, so a revealed balance never survives into the next
      // time the panel is opened.
      root.balancesRevealed = false
      root.settingsOpen = false
      root.cursor = -1
    }
  }

  // Opening the switcher is its own action so a keybind can reach it, and so
  // the "n" key and the trigger share one path.
  function openSwitcher() {
    if (root.connections.length === 0) return
    if (!root.opened) root.open()
    root.settingsOpen = false
    Qt.callLater(function () {
      var scene = connTrigger.mapToGlobal(0, 0)
      connSwitcher.openAt(scene.x, scene.y)
    })
  }

  function openSettings() {
    root.settingsOpen = true
    root.cursor = 0
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
    // Unread paints the mark in bar.urgent through the base's 160ms fade.
    active: root.unreadTotal > 0
    tooltipText: {
      if (!root.effectiveMonitoring) return "Demarchy: monitoring off"
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
      onCloseRequested: root.close()
      onTabRequested: function (direction) { root.switchPanel(direction) }

      // Omarchy is keyboard-first, so every action here has a key. The mouse
      // is a convenience, never the only way in.
      onTextKey: function (t) {
        switch (String(t).toLowerCase()) {
        case "s":
          root.settingsOpen = !root.settingsOpen
          root.cursor = root.settingsOpen ? 0 : -1
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
          break
        case "n":
          // Opened from the keyboard, so there is no pointer to hang it off;
          // anchor it to the trigger, which is where it would have appeared.
          root.openSwitcher()
          break
        }
      }
      onMoveRequested: function (dx, dy) {
        if (!root.settingsOpen || dy === 0) return
        var n = settingsView.rowCount
        root.cursor = root.cursor < 0 ? (dy > 0 ? 0 : n - 1)
                                      : (root.cursor + dy + n) % n
      }
      onActivateRequested: {
        if (root.settingsOpen && root.cursor >= 0) settingsView.activateRow(root.cursor)
      }

      ConnectionSwitcher {
        id: connSwitcher
        connections: root.connections
        activeId: root.activeConnection
        onChosen: function (id) { root.switchConnection(id) }
        onManageRequested: {
          root.settingsOpen = true
          root.cursor = 0
        }
      }
      onReturnRequested: {
        if (root.settingsOpen && root.cursor >= 0) settingsView.activateRow(root.cursor)
      }

      Column {
        id: column
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
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

              Text {
                anchors.verticalCenter: parent.verticalCenter
                text: "\uf013"
                color: Color.popups.text
                opacity: root.settingsOpen ? 0.9 : (gearHover.hovered ? 0.8 : 0.45)
                font.family: Style.font.family
                font.pixelSize: Style.font.icon
                HoverHandler { id: gearHover; cursorShape: Qt.PointingHandCursor }
                TapHandler {
                  onTapped: {
                    root.settingsOpen = !root.settingsOpen
                    root.cursor = root.settingsOpen ? 0 : -1
                  }
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
          visible: text !== "" && !root.settingsOpen
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
          visible: !root.settingsOpen && root.errorCode !== "" && !!root.snap && root.snap.stamp > 0
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
          visible: !root.settingsOpen && root.reachable && !!root.snap && !!root.snap.staking
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
          visible: !root.settingsOpen && root.reachable && !!root.snap

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
              visible: !!root.snap && !!root.snap.price
              price: root.snap ? root.snap.price : null
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
          visible: !root.settingsOpen && root.reachable && !!root.snap && !!root.snap.br
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
          connResult: root.connResult
          onConnAdd: function (name, endpoint, token) {
            root.sendCommand({ cmd: "addConnection", name: name, endpoint: endpoint, token: token })
          }
          onConnEdit: function (id, name, endpoint, token) {
            root.sendCommand({ cmd: "editConnection", id: id, name: name, endpoint: endpoint, token: token })
          }
          onConnRemove: function (id) { root.sendCommand({ cmd: "removeConnection", id: id }) }
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
            text: root.settingsOpen
                  ? "↑↓ move · enter toggle · s back · esc close"
                  : "n connection · s settings · m monitor" +
                    (root.showBalances && root.reachable ? " · b reveal" : "") +
                    " · r refresh"
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
  }
}
