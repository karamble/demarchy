// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Channel liquidity. Outbound is what this node can send, inbound what it can
// receive; the split between them is what decides whether a payment will go
// through, so they share one bar rather than getting one each.
Column {
  id: root

  property var lightning: null
  readonly property bool hasChannels: !!lightning && (lightning.outbound + lightning.inbound) > 0

  spacing: Style.space(4)

  PanelSectionHeader { text: "Lightning Channels"; foreground: Color.popups.text }

  Text {
    width: root.width
    visible: !root.hasChannels
    text: "No channels open."
    textFormat: Text.PlainText
    color: Color.popups.text; opacity: 0.5
    font.family: Style.font.family; font.pixelSize: Style.font.bodySmall
  }

  BiBar {
    width: root.width
    visible: root.hasChannels
    outbound: root.lightning ? root.lightning.outbound : 0
    inbound: root.lightning ? root.lightning.inbound : 0
    outColor: Color.accent
    inColor: Color.popups.text
  }

  Item {
    width: root.width
    visible: root.hasChannels
    implicitHeight: outLabel.implicitHeight

    Text {
      id: outLabel
      anchors.left: parent.left
      text: root.lightning ? "send " + root.lightning.outbound.toFixed(3) : ""
      color: Color.accent
      font.family: Style.font.family; font.pixelSize: Style.font.caption
    }
    Text {
      anchors.right: parent.right
      text: root.lightning ? "receive " + root.lightning.inbound.toFixed(3) : ""
      color: Color.popups.text; opacity: 0.6
      font.family: Style.font.family; font.pixelSize: Style.font.caption
    }
  }

  StatRow {
    width: root.width
    visible: root.hasChannels
    label: "Channels"
    value: root.lightning
           ? Model.count(root.lightning.channels) + " · " + Model.count(root.lightning.peers) + " peers"
           : "-"
  }

  // One line per channel. The totals above say how much liquidity exists; this
  // says how it is split, which is what decides whether a payment goes through
  //: two channels of 1 DCR each will not carry a 1.5 DCR payment.
  Repeater {
    model: root.lightning && root.lightning.list ? root.lightning.list : []

    delegate: Item {
      id: chanRow
      required property var modelData
      width: root.width
      implicitHeight: Style.space(15)

      Text {
        id: chanAlias
        anchors.left: parent.left
        anchors.right: chanBar.left
        anchors.rightMargin: Style.space(6)
        anchors.verticalCenter: parent.verticalCenter
        text: chanRow.modelData.alias !== "" ? chanRow.modelData.alias : "unnamed peer"
        textFormat: Text.PlainText
        elide: Text.ElideMiddle
        color: Color.popups.text
        opacity: chanRow.modelData.active ? 0.6 : 0.3
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
      BiBar {
        id: chanBar
        anchors.right: chanAmount.left
        anchors.rightMargin: Style.space(6)
        anchors.verticalCenter: parent.verticalCenter
        width: Math.round(root.width * 0.3)
        implicitHeight: Style.space(5)
        outbound: chanRow.modelData.local
        inbound: chanRow.modelData.remote
        // An inactive channel carries nothing, so it is drawn as an empty
        // track rather than as liquidity that is not there.
        outColor: chanRow.modelData.active ? Color.accent : Color.popups.text
        inColor: Color.popups.text
        opacity: chanRow.modelData.active ? 1.0 : 0.4
      }
      Text {
        id: chanAmount
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        text: chanRow.modelData.local.toFixed(3)
        textFormat: Text.PlainText
        horizontalAlignment: Text.AlignRight
        color: Color.popups.text
        opacity: chanRow.modelData.active ? 0.7 : 0.35
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
    }
  }
  StatRow {
    width: root.width
    visible: !!root.lightning && root.lightning.onChain > 0
    label: "On-chain"
    value: root.lightning ? root.lightning.onChain.toFixed(4) + " DCR" : "-"
  }
}
