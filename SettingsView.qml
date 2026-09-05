// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui

// The plugin's own settings page. Omarchy parses the manifest `schema` block
// and exposes it through BarWidgetRegistry, but ships no renderer for it, so
// every third-party plugin that wants a settings UI draws its own.
//
// A sibling Item inside the same panel card rather than a second window, which
// is how omavoice keeps Settings/Help/Consent in one place.
Column {
  id: root

  property bool monitoring: false
  property bool countPrivate: true
  property bool countGroupchat: true
  property bool showBalances: false
  property string endpoint: ""
  property string tokenState: "unknown"   // ok | missing | rejected | unknown

  // Keyboard cursor. -1 means the panel is being driven by shortcuts or the
  // mouse and nothing is focused.
  property int cursor: -1

  // Rows in cursor order: the four toggles, one per connection, Add
  // connection, then the terminal button. The list grows and shrinks, so the
  // last two are derived rather than numbered.
  readonly property int connFirst: 4
  readonly property int addRow: root.connFirst + root.connections.length
  readonly property int setupRow: root.addRow + 1
  readonly property int rowCount: root.setupRow + 1

  // Which of a connection row's buttons is under the cursor.
  property int actionIndex: 0
  onCursorChanged: root.actionIndex = 0

  readonly property bool formFocused: connectionsSettings.formFocused

  // The connection list is rendered here but owned by the panel, which is what
  // talks to the helper. These forward straight through.
  property var connections: []
  property string activeConnection: ""
  property var connResult: null

  signal connAdd(string name, string endpoint, string token)
  signal connEdit(string id, string name, string endpoint, string token)
  signal connRemove(string id, string name)
  signal connSwitch(string id)

  // The panel closes the form when the helper confirms a save.
  property alias connectionsView: connectionsSettings

  signal changed(string key, bool value)
  signal runSetup()
  signal focusReleased()

  // activateRow keeps the mapping from cursor index to action in one place, so
  // the key handler upstream does not have to know the row order.
  function activateRow(index) {
    if (index >= root.connFirst && index < root.addRow) {
      connectionsSettings.runAction(index - root.connFirst, root.actionIndex)
      return
    }
    switch (index) {
    case 0: root.changed("monitoring", !root.monitoring); break
    case 1: root.changed("countPrivate", !root.countPrivate); break
    case 2: root.changed("countGroupchat", !root.countGroupchat); break
    case 3: root.changed("showBalances", !root.showBalances); break
    case root.addRow: connectionsSettings.beginAdd(); break
    case root.setupRow: root.runSetup(); break
    }
  }

  // Left and right walk the buttons on a connection row. Every other row
  // carries a single action, so they do nothing there.
  function moveAction(dx) {
    var n = connectionsSettings.actionCount(root.cursor - root.connFirst)
    if (n <= 0) return
    root.actionIndex = (root.actionIndex + dx + n) % n
  }

  spacing: Style.space(6)

  PanelSectionHeader { text: "Settings"; foreground: Color.popups.text }

  Toggle {
    width: root.width
    label: "Monitor dcrpulse"
    description: "Off opens no connection at all."
    checked: root.monitoring
    hasCursor: root.cursor === 0
    onClicked: root.changed("monitoring", !root.monitoring)
  }
  Toggle {
    width: root.width
    label: "Private messages light the bar"
    checked: root.countPrivate
    hasCursor: root.cursor === 1
    onClicked: root.changed("countPrivate", !root.countPrivate)
  }
  Toggle {
    width: root.width
    label: "Group chats light the bar"
    description: "A busy group can keep the mark lit; turn this off to let private messages stand out."
    checked: root.countGroupchat
    hasCursor: root.cursor === 2
    onClicked: root.changed("countGroupchat", !root.countGroupchat)
  }
  Toggle {
    width: root.width
    label: "Show wallet balances"
    description: "Masked until revealed, and re-masked when the panel closes."
    checked: root.showBalances
    hasCursor: root.cursor === 3
    onClicked: root.changed("showBalances", !root.showBalances)
  }

  Item { width: 1; height: Style.space(6) }

  ConnectionsSettings {
    id: connectionsSettings
    width: root.width
    connections: root.connections
    activeId: root.activeConnection
    result: root.connResult
    cursorRow: (root.cursor >= root.connFirst && root.cursor < root.addRow)
               ? root.cursor - root.connFirst : -1
    actionIndex: root.actionIndex
    addHasCursor: root.cursor === root.addRow
    onFocusReleased: root.focusReleased()
    onAddRequested: function (name, endpoint, token) { root.connAdd(name, endpoint, token) }
    onEditRequested: function (id, name, endpoint, token) { root.connEdit(id, name, endpoint, token) }
    onRemoveRequested: function (id, name) { root.connRemove(id, name) }
    onSwitchRequested: function (id) { root.connSwitch(id) }
  }

  Item { width: 1; height: Style.space(6) }

  PanelSectionHeader { text: "Where tokens go"; foreground: Color.popups.text }

  Text {
    width: root.width
    wrapMode: Text.WordWrap
    // This used to say the shell never handled a token, which stopped being
    // true when connections became editable here. Say what is actually so.
    text: "A token typed above passes through this process on its way to the "
          + "0600 file the helper writes. It never reaches an argument list or "
          + "the environment. If you would rather the shell never saw it at all, "
          + "add the connection from a terminal instead."
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  Button {
    text: "Open setup in a terminal"
    bordered: true
    hasCursor: root.cursor === root.setupRow
    foreground: Color.popups.text
    onClicked: root.runSetup()
  }
}
