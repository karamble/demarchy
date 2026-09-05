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
  property bool busy: false

  // "" when the list is showing, "new" while adding, else the id being edited.
  property string editing: ""

  signal addRequested(string name, string endpoint, string token)
  signal editRequested(string id, string name, string endpoint, string token)
  signal removeRequested(string id)
  signal switchRequested(string id)

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
      width: root.width
      implicitHeight: Style.space(30)

      readonly property bool isActive: connRow.modelData.id === root.activeId

      Rectangle {
        anchors.fill: parent
        anchors.margins: Style.space(1)
        radius: Style.cornerRadius
        color: rowHover.hovered
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
          fontSize: Style.font.caption
          foreground: Color.popups.text
          onClicked: root.switchRequested(connRow.modelData.id)
        }
        Button {
          text: "Edit"
          focusable: true
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
          fontSize: Style.font.caption
          foreground: Color.urgent
          onClicked: confirmRemove.openFor(connRow.modelData)
        }
      }
    }
  }

  Button {
    visible: root.editing === ""
    text: "+ Add connection"
    bordered: true
    focusable: true
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
    }
    TextField {
      id: endpointField
      width: parent.width
      placeholderText: "127.0.0.1:8090"
      foreground: Color.popups.text
    }
    TextField {
      id: tokenField
      width: parent.width
      password: true
      placeholderText: root.editing === "new"
                       ? "MCP bearer token" : "token, leave blank to keep"
      foreground: Color.popups.text
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      // Say the rule before it is broken, rather than only in the refusal.
      text: "Anything but localhost must be https: the token is sent on every "
            + "request, and plain http would put it on the wire in clear text."
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.4
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }

    Row {
      spacing: Style.space(6)
      Button {
        text: root.busy ? "Checking…" : "Verify and save"
        bordered: true
        focusable: true
        foreground: Color.accent
        onClicked: root.submit()
      }
      Button {
        text: "Cancel"
        focusable: true
        foreground: Color.popups.text
        onClicked: root.cancel()
      }
    }
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

  ConfirmDialog {
    id: confirmRemove
    property var pending: null
    // Keep both id and name, so a confirmation cannot delete whichever
    // connection has since moved into that row.
    function openFor(conn) {
      pending = { id: conn.id, name: conn.name }
      message = "Remove \"" + conn.name + "\"? Its stored token is deleted too."
      confirmText = "Remove"
      opened = true
    }
    onConfirmed: {
      var value = pending
      pending = null
      opened = false
      if (value) root.removeRequested(value.id)
    }
    onCanceled: { pending = null; opened = false }
  }
}
