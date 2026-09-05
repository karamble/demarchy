// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Decred's stake governance: the price of a vote, how many are outstanding, how
// much of the supply is committed to voting, and the treasury those votes fund.
//
// Deliberately not called "Network": none of this is peers or sync, which live
// in the node section. What a ticket costs and how long is left to buy one at
// that price is a governance decision, not a plumbing statistic.
Column {
  id: root

  property var staking: null
  property var price: null
  property var treasury: null

  readonly property bool hasRange: !!staking && staking.estimatedMax > staking.estimatedMin

  spacing: Style.space(4)

  PanelSectionHeader { text: "Staking Governance"; foreground: Color.popups.text }

  // ---- ticket price, with the move and the countdown
  Item {
    width: root.width
    implicitHeight: priceNow.implicitHeight

    Text {
      id: priceNow
      anchors.left: parent.left
      text: root.staking ? Model.dcr(root.staking.ticketPrice) : "-"
      textFormat: Text.PlainText
      color: Color.popups.text
      font.family: Style.font.family
      font.pixelSize: Style.font.subtitle
    }
    Text {
      anchors.right: parent.right
      anchors.baseline: priceNow.baseline
      text: {
        if (!root.staking) return ""
        return "→ " + root.staking.nextTicketPrice.toFixed(2) + "  "
               + Model.delta(root.staking.ticketPrice, root.staking.nextTicketPrice)
      }
      textFormat: Text.PlainText
      color: root.staking && Model.deltaRising(root.staking.ticketPrice, root.staking.nextTicketPrice)
             ? Color.accent : Color.popups.text
      font.family: Style.font.family
      font.pixelSize: Style.font.bodySmall
    }
  }

  RangeBar {
    width: root.width
    visible: root.hasRange
    minimum: root.staking ? root.staking.estimatedMin : 0
    maximum: root.staking ? root.staking.estimatedMax : 1
    value: root.staking ? root.staking.ticketPrice : 0
    estimate: root.staking ? root.staking.nextTicketPrice : NaN
    trackColor: Color.popups.text
    valueColor: Color.accent
  }

  Item {
    width: root.width
    visible: root.hasRange
    implicitHeight: rangeLo.implicitHeight

    Text {
      id: rangeLo
      anchors.left: parent.left
      text: root.staking ? root.staking.estimatedMin.toFixed(0) : ""
      color: Color.popups.text; opacity: 0.4
      font.family: Style.font.family; font.pixelSize: Style.font.caption
    }
    Text {
      anchors.horizontalCenter: parent.horizontalCenter
      text: root.staking ? "next in " + Model.hoursAway(root.staking.hoursToChange)
                           + " · " + root.staking.blocksToChange + " blocks" : ""
      color: Color.popups.text; opacity: 0.55
      font.family: Style.font.family; font.pixelSize: Style.font.caption
    }
    Text {
      anchors.right: parent.right
      text: root.staking ? root.staking.estimatedMax.toFixed(0) : ""
      color: Color.popups.text; opacity: 0.4
      font.family: Style.font.family; font.pixelSize: Style.font.caption
    }
  }

  Item { width: 1; height: Style.space(2) }

  StatRow {
    width: root.width
    label: "Pool"
    value: root.staking ? Model.count(root.staking.poolSize) + " tickets" : "-"
  }
  StatRow {
    width: root.width
    label: "Staked"
    value: root.staking
           ? Model.percent(root.staking.participation) + "  ·  " + Model.compactDcr(root.staking.lockedDcr)
           : "-"
  }
  StatRow {
    width: root.width
    visible: !!root.treasury && root.treasury.balanceUsd > 0
    label: "Treasury"
    value: root.treasury ? Model.usd(root.treasury.balanceUsd) : "-"
  }
}
