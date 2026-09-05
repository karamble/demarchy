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
// The number always shows; the chart only appears when a DEX server is
// registered, because that is the only place a real price history comes from.
// This widget will not sample a series of its own to fill the gap.
Column {
  id: root

  property var price: null
  readonly property bool hasSeries: !!price && !!price.series && price.series.length > 1

  spacing: Style.space(4)

  PanelSectionHeader { text: "DCR Price"; foreground: Color.popups.text }

  Item {
    width: root.width
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
