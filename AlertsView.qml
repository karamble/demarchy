// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// The plugin's alerts page: which triggers are armed, for whom, and a form to
// arm one. A sibling of SettingsView inside the same panel card, with the same
// cursor model, so the panel drives both the same way.
//
// The page never asks anyone for a live value. The helper evaluates triggers
// and wakes whoever armed them; this is where the user sees what is waiting.
Column {
  id: root

  // Keyboard cursor. -1 means the panel is being driven by shortcuts or the
  // mouse and nothing is focused.
  property int cursor: -1

  // Rows in cursor order: one per trigger in the order the list draws them,
  // then Add alert. Group headers are not rows.
  readonly property int addRow: root.triggers.length
  readonly property int rowCount: root.addRow + 1

  // Which of a trigger row's buttons is under the cursor.
  property int actionIndex: 0
  onCursorChanged: root.actionIndex = 0

  readonly property bool formFocused: alertsList.formFocused

  // The list is rendered here but owned by the panel, which is what talks to
  // the helper. These forward straight through.
  property var triggers: []
  property var catalogue: []
  property string triggersError: ""
  property bool monitoring: false
  property var agentStates: ({})
  property string activeConnection: ""
  property var result: null
  property double now: Date.now()

  readonly property int armedCount: {
    var n = 0
    for (var i = 0; i < root.triggers.length; i++)
      if (Model.alertArmed(String(root.triggers[i].status))) n++
    return n
  }
  readonly property int spentCount: {
    var n = 0
    for (var i = 0; i < root.triggers.length; i++)
      if (Model.alertSpent(String(root.triggers[i].status))) n++
    return n
  }

  signal arm(var spec)
  signal edit(string id, var spec)
  signal disarm(string id, string label)
  signal refreshRequested()
  signal focusReleased()

  // The panel closes the form when the helper confirms a save.
  readonly property alias list: alertsList

  // The page asks for a fresh list each time it comes up, so it is current
  // even when monitoring is off and no snapshot is arriving.
  onVisibleChanged: if (root.visible) root.refreshRequested()

  // activateRow keeps the mapping from cursor index to action in one place, so
  // the key handler upstream does not have to know the row order.
  function activateRow(index) {
    if (index >= 0 && index < root.addRow) {
      alertsList.runAction(index, root.actionIndex)
      return
    }
    if (index === root.addRow) alertsList.beginAdd()
  }

  // Left and right walk the buttons on a trigger row. The Add row carries a
  // single action, so they do nothing there.
  function moveAction(dx) {
    var n = alertsList.actionCount(root.cursor)
    if (n <= 0) return
    root.actionIndex = (root.actionIndex + dx + n) % n
  }

  spacing: Style.space(6)

  Item {
    width: root.width
    implicitHeight: Math.max(header.implicitHeight, tally.implicitHeight)

    PanelSectionHeader {
      id: header
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: "Alerts"
      foreground: Color.popups.text
    }
    Text {
      id: tally
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      text: root.armedCount + " armed · " + root.spentCount + " spent"
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.45
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
  }

  // Off means off. With the helper stopped nothing evaluates these, and a page
  // that listed them calmly would be lying about it.
  Text {
    width: root.width
    visible: !root.monitoring && root.armedCount > 0
    wrapMode: Text.WordWrap
    text: "Monitoring is off, so " + root.armedCount + " armed "
          + (root.armedCount === 1 ? "alert is" : "alerts are") + " NOT being watched."
    textFormat: Text.PlainText
    color: Color.urgent
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  AlertsList {
    id: alertsList
    width: root.width
    triggers: root.triggers
    catalogue: root.catalogue
    agentStates: root.agentStates
    activeConnection: root.activeConnection
    triggersError: root.triggersError
    result: root.result
    now: root.now
    cursorRow: (root.cursor >= 0 && root.cursor < root.addRow) ? root.cursor : -1
    actionIndex: root.actionIndex
    addHasCursor: root.cursor === root.addRow
    onFocusReleased: root.focusReleased()
    onArmRequested: function (spec) { root.arm(spec) }
    onEditRequested: function (id, spec) { root.edit(id, spec) }
    onDisarmRequested: function (id, label) { root.disarm(id, label) }
  }
}
