// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Add, edit and remove the dcrpulse connections this widget can talk to.
//
// The token field is the one place this process handles a secret. It goes
// straight to the helper over a pipe and is cleared the moment the helper
// answers: it is never written to a file from here and never put in an
// argument list.
Column {
  id: root

  property var connections: []
  property string activeId: ""
  property var result: null          // the helper's answer to the last command
  // The plain-http exception in force, and the reason the config could not be
  // read. Stated here, never editable here: it is set from a terminal.
  property var plainHttp: null
  property string configError: ""
  property bool busy: false

  // Keyboard cursor, driven by the panel. cursorRow is an index into
  // connections, or -1 when the cursor is elsewhere; actionIndex picks one of
  // that row's buttons.
  property int cursorRow: -1
  property int actionIndex: 0
  property bool addHasCursor: false

  // While a field holds focus the panel hands it every key, so typing a token
  // does not trip the panel's letter shortcuts.
  readonly property bool formFocused: nameField.activeFocus
                                      || endpointField.activeFocus
                                      || tokenField.activeFocus
                                      || saveButton.activeFocus
                                      || cancelButton.activeFocus

  // "" when the list is showing, "new" while adding, else the id being edited.
  property string editing: ""

  signal addRequested(string name, string endpoint, string token)
  signal editRequested(string id, string name, string endpoint, string token)
  // The panel hosts the confirmation: ConfirmDialog is an overlay Item and
  // needs a surface it can fill, which a Column cannot give it.
  signal removeRequested(string id, string name)
  signal switchRequested(string id)
  // Raised when the form lets go of the keyboard, so the panel can take it
  // back and the row cursor works again.
  signal focusReleased()

  spacing: Style.space(6)

  function beginAdd() {
    root.editing = "new"
    nameField.text = ""
    endpointField.text = ""
    tokenField.text = ""
    nameField.forceActiveFocus()
  }

  function beginEdit(conn) {
    root.editing = conn.id
    nameField.text = conn.name
    endpointField.text = conn.endpoint
    tokenField.text = ""       // blank means keep the stored one
    nameField.forceActiveFocus()
  }

  function cancel() {
    root.editing = ""
    tokenField.text = ""
    root.focusReleased()
  }

  // A row's buttons are conditional, so the keyboard has to walk the same set
  // the mouse sees: no Use on the connection already in use, and no Remove
  // when it is the only one left.
  function actionsFor(conn) {
    var acts = []
    if (conn && conn.id !== root.activeId) acts.push("use")
    acts.push("edit")
    if (root.connections.length > 1) acts.push("remove")
    return acts
  }

  function actionCount(rowIndex) {
    if (rowIndex < 0 || rowIndex >= root.connections.length) return 0
    return root.actionsFor(root.connections[rowIndex]).length
  }

  function actionAt(rowIndex, index) {
    var n = root.actionCount(rowIndex)
    if (n === 0) return ""
    return root.actionsFor(root.connections[rowIndex])[Math.max(0, Math.min(index, n - 1))]
  }

  function runAction(rowIndex, index) {
    var conn = root.connections[rowIndex]
    if (!conn) return
    switch (root.actionAt(rowIndex, index)) {
    case "use": root.switchRequested(conn.id); break
    case "edit": root.beginEdit(conn); break
    case "remove": root.removeRequested(conn.id, conn.name); break
    }
  }

  function submit() {
    if (root.editing === "new")
      root.addRequested(nameField.text, endpointField.text, tokenField.text)
    else
      root.editRequested(root.editing, nameField.text, endpointField.text, tokenField.text)
    // The token is dropped from this process as soon as it is handed over; the
    // form closes when the helper confirms.
    tokenField.text = ""
  }

  PanelSectionHeader { text: "Connections"; foreground: Color.popups.text }

  // ---- the list
  Repeater {
    model: root.editing === "" ? root.connections : []

    delegate: Item {
      id: connRow
      required property var modelData
      required property int index
      width: root.width
      implicitHeight: Style.space(30)

      readonly property bool isActive: connRow.modelData.id === root.activeId
      readonly property bool hasCursor: connRow.index === root.cursorRow
      readonly property string cursorAction: connRow.hasCursor
                                             ? root.actionAt(connRow.index, root.actionIndex) : ""

      Rectangle {
        anchors.fill: parent
        anchors.margins: Style.space(1)
        radius: Style.cornerRadius
        color: rowHover.hovered || connRow.hasCursor
               ? Style.hoverFillFor(Color.popups.text, Color.accent) : "transparent"
      }
      HoverHandler { id: rowHover }

      Column {
        anchors.left: parent.left
        anchors.leftMargin: Style.space(4)
        anchors.right: rowButtons.left
        anchors.rightMargin: Style.space(6)
        anchors.verticalCenter: parent.verticalCenter
        spacing: 0

        Text {
          width: parent.width
          text: (connRow.isActive ? "● " : "○ ") + connRow.modelData.name
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: Color.popups.text
          font.family: Style.font.family
          font.pixelSize: Style.font.bodySmall
          font.bold: connRow.isActive
        }
        Text {
          width: parent.width
          text: connRow.modelData.endpoint
          textFormat: Text.PlainText
          elide: Text.ElideMiddle
          color: Color.popups.text
          opacity: 0.45
          font.family: Style.font.family
          font.pixelSize: Style.font.caption
        }
      }

      Row {
        id: rowButtons
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        spacing: Style.space(4)

        Button {
          text: "Use"
          visible: !connRow.isActive
          focusable: true
          hasCursor: connRow.cursorAction === "use"
          fontSize: Style.font.caption
          foreground: Color.popups.text
          onClicked: root.switchRequested(connRow.modelData.id)
        }
        Button {
          text: "Edit"
          focusable: true
          hasCursor: connRow.cursorAction === "edit"
          fontSize: Style.font.caption
          foreground: Color.popups.text
          onClicked: root.beginEdit(connRow.modelData)
        }
        Button {
          text: "Remove"
          // Removing the only connection would leave the widget with nothing
          // to talk to and no way back except the terminal.
          visible: root.connections.length > 1
          focusable: true
          hasCursor: connRow.cursorAction === "remove"
          fontSize: Style.font.caption
          foreground: Color.urgent
          onClicked: root.removeRequested(connRow.modelData.id, connRow.modelData.name)
        }
      }
    }
  }

  Button {
    visible: root.editing === ""
    text: "+ Add connection"
    bordered: true
    focusable: true
    hasCursor: root.addHasCursor
    foreground: Color.popups.text
    onClicked: root.beginAdd()
  }

  // ---- the form
  Column {
    width: root.width
    visible: root.editing !== ""
    spacing: Style.space(5)

    Text {
      text: root.editing === "new" ? "New connection" : "Edit connection"
      color: Color.popups.text
      font.family: Style.font.family
      font.pixelSize: Style.font.bodySmall
      font.bold: true
    }

    TextField {
      id: nameField
      width: parent.width
      placeholderText: "name, e.g. home or vps"
      foreground: Color.popups.text
      KeyNavigation.tab: endpointField
      KeyNavigation.backtab: cancelButton
      Keys.onEscapePressed: root.cancel()
      onAccepted: root.submit()
    }
    TextField {
      id: endpointField
      width: parent.width
      placeholderText: "127.0.0.1:8090"
      foreground: Color.popups.text
      KeyNavigation.tab: tokenField
      KeyNavigation.backtab: nameField
      Keys.onEscapePressed: root.cancel()
      onAccepted: root.submit()
    }
    TextField {
      id: tokenField
      width: parent.width
      password: true
      placeholderText: root.editing === "new"
                       ? "MCP bearer token" : "token, leave blank to keep"
      foreground: Color.popups.text
      KeyNavigation.tab: saveButton
      KeyNavigation.backtab: endpointField
      Keys.onEscapePressed: root.cancel()
      onAccepted: root.submit()
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      // Says the rule before it is broken rather than only in the refusal, and
      // stays true whether or not a mesh exception is set: off this machine is
      // still the wrong place for a token in clear text, and the exception is
      // about a tunnel rather than about the internet.
      text: "Off this machine, use https: the token is sent on every request, "
            + "and plain http would put it on the wire in clear text."
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.4
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }

    Row {
      spacing: Style.space(6)
      Button {
        id: saveButton
        text: root.busy ? "Checking…" : "Verify and save"
        bordered: true
        focusable: true
        foreground: Color.accent
        KeyNavigation.tab: cancelButton
        KeyNavigation.backtab: tokenField
        Keys.onEscapePressed: root.cancel()
        onClicked: root.submit()
      }
      Button {
        id: cancelButton
        text: "Cancel"
        focusable: true
        foreground: Color.popups.text
        KeyNavigation.tab: nameField
        KeyNavigation.backtab: saveButton
        Keys.onEscapePressed: root.cancel()
        onClicked: root.cancel()
      }
    }
  }

  // ---- the expert exception, stated but not offered
  //
  // A relaxation nobody can see is a relaxation nobody reviews, so the panel
  // says what is in force. There is no control here and no keyboard row: it is
  // changed from a terminal, which is the point.
  Text {
    width: root.width
    visible: !!root.plainHttp || root.configError !== ""
    wrapMode: Text.WordWrap
    text: {
      if (root.configError !== "") return root.configError
      if (!root.plainHttp) return ""
      var nets = (root.plainHttp.networks || []).join(", ")
      return "Plain http is allowed via " + root.plainHttp["interface"] + " to "
             + nets + " (expert setting in connections.json; change it with "
             + "demarchy-setup allow-http)."
    }
    textFormat: Text.PlainText
    color: root.configError !== "" ? Color.urgent : Color.popups.text
    opacity: root.configError !== "" ? 0.9 : 0.45
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  // ---- what the helper said
  Text {
    width: root.width
    visible: !!root.result && !root.result.ok
    wrapMode: Text.WordWrap
    text: root.result ? Model.connError(root.result) : ""
    textFormat: Text.PlainText
    color: Color.urgent
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
