// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// The Bison Relay payment bridge: what the bots have been paid today against
// the daily cap, what is waiting on the owner, and the last few payments.
//
// This section reports and nothing more. Approving a spend is the owner's act,
// taken in the dashboard; neither this widget nor an agent gets a control for
// it, so there is no button here and never will be.
Column {
  id: root

  property var brmcp: null
  // The panel's clock, for the expiry countdown on a pending request.
  property double now: Date.now()
  // Three rows per list: the column is shared, and an overflow line says what
  // the rows leave out.
  property int rowLimit: 3

  // An error means brclientd or the bridge could not be reached; enabled false
  // with no error means the bridge is switched off. Both leave the lists empty.
  readonly property bool unreachable: !!brmcp && !!brmcp.error
  readonly property bool bridgeOn: !!brmcp && brmcp.enabled === true && !root.unreachable
  readonly property int pendingTotal: root.bridgeOn && brmcp.pending ? brmcp.pending.length : 0
  readonly property var pending: root.bridgeOn && brmcp.pending ? brmcp.pending.slice(0, root.rowLimit) : []
  readonly property int spendTotal: root.bridgeOn && brmcp.spend ? brmcp.spend.length : 0
  readonly property var spend: root.bridgeOn && brmcp.spend ? brmcp.spend.slice(0, root.rowLimit) : []

  readonly property string stateLine: {
    if (!root.brmcp) return ""
    if (root.unreachable) return "unreachable"
    if (root.brmcp.enabled !== true) return "off"
    // One unit for the pair, so the column has room: "0.012 of 0.50 DCR today".
    var today = Model.preciseDcr(root.brmcp.todayDcr).replace(" DCR", "") + " of "
                + Model.preciseDcr(root.brmcp.perDayCapDcr) + " today"
    return (root.brmcp.mode === "approval" ? "approval · " : "caps only · ") + today
  }

  spacing: Style.space(3)

  // The heading carries the bridge's state opposite it: the mode and the day's
  // spend against the cap, or the one word that says why there is nothing
  // underneath.
  Item {
    width: root.width
    implicitHeight: Math.max(header.implicitHeight, stateText.implicitHeight)

    PanelSectionHeader {
      id: header
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: "BRMCP"
      foreground: Color.popups.text
    }
    Text {
      id: stateText
      anchors.left: header.right
      anchors.leftMargin: Style.space(6)
      anchors.right: parent.right
      anchors.baseline: header.baseline
      horizontalAlignment: Text.AlignRight
      text: root.stateLine
      textFormat: Text.PlainText
      elide: Text.ElideRight
      color: root.unreachable ? Color.urgent : Color.popups.text
      opacity: root.unreachable ? 0.9 : 0.5
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
  }

  Text {
    width: root.width
    visible: root.unreachable
    text: root.unreachable ? "bridge unreachable: " + String(root.brmcp.error) : ""
    textFormat: Text.PlainText
    elide: Text.ElideRight
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }
  Text {
    width: root.width
    visible: !!root.brmcp && !root.unreachable && root.brmcp.enabled !== true
    text: "Bridge is off"
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }
  Text {
    width: root.width
    visible: root.bridgeOn && root.pending.length === 0 && root.spend.length === 0
    text: "No bot payments yet"
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  // Requests waiting on the owner come first and sit on the same tint the bar
  // badge uses, because they are the one thing in this section with a clock
  // running: an unanswered request expires and the bot goes unpaid.
  Repeater {
    model: root.pending
    delegate: Item {
      id: pendingRow
      required property var modelData
      readonly property string who: pendingRow.modelData.botNick
                                    ? String(pendingRow.modelData.botNick)
                                    : Model.shortId(pendingRow.modelData.bot)
      width: root.width
      implicitHeight: pendingBody.implicitHeight + Style.space(8)

      Rectangle {
        anchors.fill: parent
        radius: Style.cornerRadius
        color: Util.alpha(Color.urgent, 0.12)
      }

      Column {
        id: pendingBody
        anchors.left: parent.left
        anchors.leftMargin: Style.space(4)
        anchors.right: parent.right
        anchors.rightMargin: Style.space(4)
        anchors.verticalCenter: parent.verticalCenter
        spacing: 0

        Item {
          width: parent.width
          implicitHeight: Math.max(pendingWho.implicitHeight, pendingSum.implicitHeight)

          Row {
            id: pendingWho
            anchors.left: parent.left
            anchors.right: pendingSum.left
            anchors.rightMargin: Style.space(6)
            anchors.verticalCenter: parent.verticalCenter
            spacing: Style.space(5)

            Text {
              id: pendingGlyph
              text: "!"
              textFormat: Text.PlainText
              color: Color.urgent
              font.family: Style.font.family
              font.pixelSize: Style.font.bodySmall
            }
            Text {
              id: pendingName
              text: pendingRow.who
              textFormat: Text.PlainText
              color: Color.urgent
              font.family: Style.font.family
              font.pixelSize: Style.font.bodySmall
              font.weight: Font.Medium
            }
            Text {
              width: Math.max(0, pendingWho.width - pendingGlyph.width - pendingName.width
                                 - pendingWho.spacing * 2)
              text: "· " + String(pendingRow.modelData.tool || "")
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: Color.urgent
              opacity: 0.75
              font.family: Style.font.family
              font.pixelSize: Style.font.bodySmall
            }
          }
          Text {
            id: pendingSum
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            text: Model.preciseDcr(pendingRow.modelData.amountDcr)
            textFormat: Text.PlainText
            color: Color.urgent
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
        }
        Text {
          width: parent.width
          // The countdown says "now" once the window has closed; the row says
          // so in a word, since "expires in now" is not a sentence.
          text: {
            var left = Model.countdownSeconds(pendingRow.modelData.expiresAt, root.now)
            if (left === "") return "waiting for approval"
            if (left === "now") return "waiting for approval · expired"
            return "waiting for approval · expires in " + left
          }
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: Color.urgent
          opacity: 0.9
          font.family: Style.font.family
          font.pixelSize: Style.font.caption
        }
        Text {
          width: parent.width
          text: "Approve or deny in the dashboard"
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: Color.popups.text
          opacity: 0.5
          font.family: Style.font.family
          font.pixelSize: Style.font.caption
        }
      }
    }
  }

  // Requests the rows leave out are still requests: the count stays urgent.
  Text {
    width: root.width
    visible: root.pendingTotal > root.rowLimit
    text: "+" + (root.pendingTotal - root.rowLimit) + " more waiting for approval"
    textFormat: Text.PlainText
    color: Color.urgent
    opacity: 0.9
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  // The last few payments, newest first. A failure keeps its reason on a
  // second line so the row above it stays the same shape as its neighbours.
  Repeater {
    model: root.spend
    delegate: Column {
      id: spendRow
      required property var modelData
      readonly property string status: String(spendRow.modelData.status || "")
      readonly property bool failed: spendRow.status === "failed"
      readonly property string who: spendRow.modelData.botNick
                                    ? String(spendRow.modelData.botNick)
                                    : Model.shortId(spendRow.modelData.bot)
      width: root.width
      spacing: 0

      Item {
        width: parent.width
        implicitHeight: Math.max(spendWho.implicitHeight, spendSum.implicitHeight)

        Row {
          id: spendWho
          anchors.left: parent.left
          anchors.right: spendSum.left
          anchors.rightMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(5)

          Text {
            id: spendName
            text: spendRow.who
            textFormat: Text.PlainText
            color: Color.popups.text
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
            font.weight: Font.Medium
          }
          Text {
            width: Math.max(0, spendWho.width - spendName.width - spendWho.spacing)
            text: "· " + String(spendRow.modelData.tool || "")
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: Color.popups.text
            opacity: 0.75
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
        }

        Row {
          id: spendSum
          anchors.right: parent.right
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(5)

          Text {
            text: Model.preciseDcr(spendRow.modelData.amountDcr)
            textFormat: Text.PlainText
            color: spendRow.failed ? Color.urgent : Color.popups.text
            font.family: Style.font.family
            font.pixelSize: Style.font.bodySmall
          }
          Text {
            visible: spendRow.status !== ""
            text: spendRow.status
            textFormat: Text.PlainText
            color: spendRow.failed ? Color.urgent : Color.popups.text
            opacity: spendRow.status === "pending" ? 0.5 : (spendRow.failed ? 0.9 : 1.0)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
          }
        }
      }

      Text {
        width: parent.width
        visible: spendRow.failed && !!spendRow.modelData.err
        text: String(spendRow.modelData.err || "")
        textFormat: Text.PlainText
        elide: Text.ElideRight
        color: Color.urgent
        opacity: 0.75
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
    }
  }

  // Payments the rows leave out.
  Text {
    width: root.width
    visible: root.spendTotal > root.rowLimit
    text: "+" + (root.spendTotal - root.rowLimit) + " more payments"
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
