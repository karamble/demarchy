// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// What a DCR is worth, and where it has been.
//
// The number always shows; the DCRDEX row and the chart only appear when a DEX
// server is registered, because that is the only place a real trade price and
// a real price history come from. This widget will not sample a series of its
// own to fill the gap.
Column {
  id: root

  property var price: null
  // The DCRDEX spot, when the token may read the DEX and a server carries a
  // dcr_btc market. It sits beside the exchange feed in the same unit.
  property var dex: null
  readonly property bool hasSeries: !!price && !!price.series && price.series.length > 1
  readonly property bool hasDex: !!dex && dex.rate > 0
  // The premium against the exchange feed is only a number when both prices
  // exist; the helper leaves it at zero otherwise.
  readonly property bool hasPremium: root.hasDex && !!price && price.sats > 0

  spacing: Style.space(4)

  PanelSectionHeader { text: "DCR Price"; foreground: Color.popups.text }

  Item {
    width: root.width
    visible: !!root.price
    implicitHeight: usdText.implicitHeight

    Text {
      id: usdText
      anchors.left: parent.left
      text: root.price ? Model.usd(root.price.dcrUsd) : "-"
      textFormat: Text.PlainText
      color: Color.popups.text
      font.family: Style.font.family
      font.pixelSize: Style.font.subtitle
    }
    Text {
      anchors.right: parent.right
      anchors.baseline: usdText.baseline
      text: root.price ? Model.sats(root.price.sats) : ""
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.6
      font.family: Style.font.family
      font.pixelSize: Style.font.bodySmall
    }
  }

  // The price a trade actually clears at, and how it sits against the feed.
  StatRow {
    width: root.width
    visible: root.hasDex
    // The label carries the premium in words: the left column has the room,
    // and a coloured number beside the rate would read as the rate moving.
    label: root.hasPremium ? "DCRDEX · " + Model.premiumWords(root.dex.premium) : "DCRDEX"
    value: root.hasDex ? Model.sats(root.dex.rate) : "-"
  }

  Text {
    width: root.width
    visible: root.hasDex
    text: {
      if (!root.hasDex) return ""
      var d = root.dex
      var dir = d.change24 >= 0 ? "▲ " : "▼ "
      return Model.usd(d.rateUsd) + " · " + dir + Math.abs(d.change24).toFixed(1) + "% 24h · "
             + Model.compactDcr(d.volume24) + " traded"
    }
    textFormat: Text.PlainText
    elide: Text.ElideRight
    // The same rule as the chart caption: red when the day is down.
    color: root.hasDex && root.dex.change24 < 0 ? Color.urgent : Color.popups.text
    opacity: root.hasDex && root.dex.change24 < 0 ? 0.85 : 0.6
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  Sparkline {
    width: root.width
    visible: root.hasSeries
    values: root.hasSeries ? root.price.series : []
    // Price wanders in both directions, so the fill reads as a shape here
    // rather than the flat wedge a cumulative curve produces.
    filled: true
    lineColor: root.price && root.price.change < 0 ? Color.urgent : Color.accent
  }

  Text {
    width: root.width
    visible: root.hasSeries
    text: {
      if (!root.price) return ""
      var dir = root.price.change >= 0 ? "▲ " : "▼ "
      return (root.price.market || "dcr/btc") + "   "
             + dir + Math.abs(root.price.change).toFixed(1) + "%"
    }
    textFormat: Text.PlainText
    color: root.price && root.price.change < 0 ? Color.urgent : Color.accent
    opacity: 0.85
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
