// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// The panel's hero: what this wallet has done, not what the network has done.
// Live tickets, lifetime votes and lifetime earnings, over a cumulative reward
// curve, with the last vote as the freshness cue.
Column {
  id: root

  property var own: null
  property var staking: null
  property double now: Date.now()

  readonly property bool hasHistory: !!own && own.hasHistory === true
  readonly property int live: own ? (own.live || 0) : 0
  readonly property int immature: own ? (own.immature || 0) : 0

  spacing: Style.space(6)

  PanelSectionHeader { text: "Your Staking"; foreground: Color.popups.text }

  // ---- never staked. This is the common case on a fresh wallet, so it says
  // why rather than printing three zeroes.
  Text {
    width: root.width
    visible: !root.hasHistory
    wrapMode: Text.WordWrap
    text: {
      if (!root.staking) return "No tickets yet."
      return "No tickets yet: a ticket costs "
             + Model.dcr(root.staking.ticketPrice) + "."
    }
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.6
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  // ---- the three figures
  Row {
    width: root.width
    visible: root.hasHistory
    spacing: Style.space(18)

    HeroStat {
      value: Model.count(root.live)
      label: root.immature > 0 ? "live · " + root.immature + " immature" : "live"
    }
    HeroStat {
      value: Model.count(root.own ? root.own.voted : 0)
      label: "voted"
    }
    HeroStat {
      value: Model.signedDcr(root.own ? root.own.reward : 0, 2)
      label: "DCR earned"
      valueColor: Color.accent
    }
  }

  Column {
    width: root.width
    visible: root.hasHistory && !!root.own
             && !!root.own.voteBuckets && root.own.voteBuckets.length > 1
    spacing: Style.space(2)

    BarChart {
      width: parent.width
      values: root.own && root.own.voteBuckets ? root.own.voteBuckets : []
      barColor: Color.accent
    }
    Text {
      text: {
        if (!root.own || !root.own.voteBuckets) return ""
        var b = root.own.voteBuckets
        var peak = 0
        for (var i = 0; i < b.length; i++) if (b[i] > peak) peak = b[i]
        // Without the peak the bars are only suggestive: a tall one could be
        // eight votes or twenty, and there is nothing on the chart to say.
        return "votes per week · last " + b.length + " weeks · peak " + peak
      }
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.4
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
  }

  // ---- the freshness line, which is what says the setup is still working.
  // Only the missed count is coloured as a problem; colouring the whole line
  // would make a healthy setup with one old miss look broken.
  Row {
    width: root.width
    visible: root.hasHistory
    spacing: 0

    Text {
      text: {
        if (!root.own) return ""
        var bits = []
        if (root.own.lastVote > 0)
          bits.push("last vote " + Model.sinceUnix(root.own.lastVote, root.now))
        if (root.own.poolShare > 0)
          bits.push(root.own.poolShare.toFixed(3) + "% of pool")
        return bits.join(" · ")
      }
      textFormat: Text.PlainText
      color: Color.popups.text
      opacity: 0.6
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
    Text {
      visible: !!root.own && (root.own.missed + root.own.revoked) > 0
      text: {
        if (!root.own) return ""
        return "  ·  " + (root.own.missed + root.own.revoked) + " missed"
      }
      textFormat: Text.PlainText
      color: Color.urgent
      opacity: 0.9
      font.family: Style.font.family
      font.pixelSize: Style.font.caption
    }
  }
}
