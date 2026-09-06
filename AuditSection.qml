// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// What the agents have written through dcrpulse since it started: every
// mutating tool call, who made it, and whether the policy let it through. The
// helper sends the ring newest first; the panel shows the head of it and
// counts the rest in the header, so a refused write is never off the bottom.
Column {
  id: root

  property var audit: null
  // The panel's clock, for the "ago" column. Ticks only while the panel is
  // open, like every other freshness line in the card.
  property double now: Date.now()
  // Three rows: the column is shared with the node and the bridge, and the
  // header already says how many the log holds.
  property int rowLimit: 3

  readonly property var entries: audit && audit.entries ? audit.entries.slice(0, root.rowLimit) : []
  readonly property int denied: audit ? (Number(audit.denied) || 0) : 0

  spacing: Style.space(3)

  // The heading carries the totals opposite it, the way the price hero pairs
  // its two figures on one line. Two Text items rather than one: the denied
  // count turns urgent on its own, and rich text is not allowed in here.
  Item {
    width: root.width
    implicitHeight: Math.max(header.implicitHeight, deniedText.implicitHeight)

    PanelSectionHeader {
      id: header
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: "MCP Audit"
      foreground: Color.popups.text
    }
    Text {
      id: deniedText
      anchors.right: parent.right
      anchors.baseline: header.baseline
      text: root.audit ? Model.count(root.denied) + " denied" : ""
      textFormat: Text.PlainText
      color: root.denied > 0 ? Color.urgent : Color.popups.text
      opacity: root.denied > 0 ? 0.9 : 0.5
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
    Text {
      anchors.right: deniedText.left
      anchors.baseline: header.baseline
      text: root.audit ? Model.count(root.audit.count) + " in log · " : ""
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.5
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
  }

  Text {
    width: root.width
    visible: root.entries.length === 0
    text: "No agent writes since dcrpulse started"
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  // One entry per two lines: who did what, with the sum and the age on the
  // right, then the target and the policy's reason underneath in the dim
  // caption. A refused write paints its first line urgent from glyph to tool.
  Repeater {
    model: root.entries
    delegate: Column {
      id: auditRow
      required property var modelData
      readonly property string result: String(auditRow.modelData.result || "")
      readonly property bool urgent: Model.auditUrgent(auditRow.result)
      readonly property string detailLine: {
        var parts = []
        if (auditRow.modelData.target) parts.push(Model.shortId(auditRow.modelData.target))
        if (auditRow.modelData.detail) parts.push(String(auditRow.modelData.detail))
        return parts.join(" · ")
      }
      width: root.width
      spacing: 0

      Item {
        width: parent.width
        implicitHeight: Math.max(whoRow.implicitHeight, sumRow.implicitHeight)

        Row {
          id: whoRow
          anchors.left: parent.left
          anchors.right: sumRow.left
          anchors.rightMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(5)

          Text {
            id: glyphText
            text: Model.auditGlyph(auditRow.result)
            textFormat: Text.PlainText
            color: auditRow.urgent ? Color.urgent : Color.popups.text
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
          Text {
            id: agentText
            text: String(auditRow.modelData.agent || auditRow.modelData.agentId || "unknown")
            textFormat: Text.PlainText
            color: auditRow.urgent ? Color.urgent : Color.popups.text
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
            font.weight: Font.Medium
          }
          Text {
            width: Math.max(0, whoRow.width - glyphText.width - agentText.width - whoRow.spacing * 2)
            text: "· " + String(auditRow.modelData.tool || "")
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: auditRow.urgent ? Color.urgent : Color.popups.text
            opacity: 0.75
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
        }

        Row {
          id: sumRow
          anchors.right: parent.right
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(5)

          Text {
            visible: Number(auditRow.modelData.amountDcr) > 0
            text: Model.preciseDcr(auditRow.modelData.amountDcr)
            textFormat: Text.PlainText
            color: auditRow.urgent ? Color.urgent : Color.popups.text
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
          Text {
            text: Model.ago(Model.isoToUnix(auditRow.modelData.time), root.now)
            textFormat: Text.PlainText
            color: Color.popups.text
            opacity: 0.5
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }
        }
      }

      Text {
        width: parent.width
        visible: text !== ""
        text: auditRow.detailLine
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: Color.popups.text
        opacity: 0.6
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
    }
  }

  // What the header counts and the rows leave out.
  Text {
    width: root.width
    visible: !!root.audit && root.audit.count > root.rowLimit
    text: root.audit ? "+" + (root.audit.count - root.rowLimit) + " more in the log" : ""
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
