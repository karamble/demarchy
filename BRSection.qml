// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
  id: root
  property var br: null
  property var unread: null
  property int messageLimit: 6

  // Newest first for display; the helper sends the ring oldest-first.
  readonly property var recent: {
    if (!br || !br.messages) return []
    var out = br.messages.slice(-root.messageLimit)
    out.reverse()
    return out
  }
  readonly property int noticeCount: br && br.notices ? br.notices.length : 0

  spacing: Style.space(3)

  // The heading carries the connection, and the unread count sits opposite as a
  // pill. As separate full-width rows these left a void down the middle of the
  // panel: every other section lives in a half-width column where a
  // label-left/value-right row reads fine, and this one does not.
  Item {
    width: root.width
    implicitHeight: Math.max(brHeader.implicitHeight, unreadPill.implicitHeight)

    PanelSectionHeader {
      id: brHeader
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: "Bison Relay"
      foreground: Color.popups.text
    }
    Text {
      anchors.left: brHeader.right
      anchors.leftMargin: Style.space(6)
      anchors.right: unreadPill.left
      anchors.rightMargin: Style.space(8)
      anchors.baseline: brHeader.baseline
      text: Model.brLine(root.br)
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: root.br && root.br.stage === "ready" ? Color.popups.text : Color.urgent
      opacity: root.br && root.br.stage === "ready" ? 0.5 : 0.9
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }

    Rectangle {
      id: unreadPill
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      visible: !!root.unread && root.unread.total > 0
      implicitWidth: unreadText.implicitWidth + Style.space(12)
      implicitHeight: unreadText.implicitHeight + Style.space(4)
      radius: height / 2
      // The same colour the bar badge uses, so the two read as one thing.
      color: Qt.rgba(Color.urgent.r, Color.urgent.g, Color.urgent.b, 0.18)

      Text {
        id: unreadText
        anchors.centerIn: parent
        text: {
          if (!root.unread) return ""
          var parts = []
          if (root.unread.private > 0) parts.push(root.unread.private + " private")
          if (root.unread.groupchat > 0) parts.push(root.unread.groupchat + " group")
          return parts.join(" · ")
        }
        textFormat: Text.PlainText
        color: Color.urgent
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
    }
  }

  // brclientd's persisted daemon notes. These are connection and housekeeping
  // events, not chat, so they are reported separately from the unread count.
  StatRow {
    width: root.width
    visible: root.noticeCount > 0
    label: "Daemon notices"
    value: Model.count(root.noticeCount)
  }

  Item { width: 1; height: Style.space(4); visible: root.recent.length > 0 }

  Text {
    width: root.width
    visible: root.recent.length === 0
    text: "No recent messages."
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  Repeater {
    model: root.recent
    delegate: Column {
      required property var modelData
      width: root.width
      spacing: 0

      Text {
        width: parent.width
        text: Model.sender(parent.modelData) + Model.groupLabel(parent.modelData)
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: Color.popups.text
        opacity: 0.55
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
      Text {
        width: parent.width
        text: Model.body(parent.modelData)
        textFormat: Text.PlainText
        elide: Text.ElideRight
        maximumLineCount: 2
        wrapMode: Text.WordWrap
        color: Color.popups.text
        font.family: Style.font.family
        font.pixelSize: Style.font.bodySmall
        bottomPadding: Style.space(4)
      }
    }
  }
}
