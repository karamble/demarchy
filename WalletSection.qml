// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Balances are masked until revealed, and re-mask when the panel closes: the
// panel is one click from any screen share, so the default has to be hidden.
// Whether this section renders at all is a separate setting.
Column {
  id: root
  property var wallet: null
  property bool revealed: false
  signal toggleReveal()

  spacing: Style.space(3)

  Item {
    width: root.width
    implicitHeight: header.implicitHeight

    PanelSectionHeader { id: header; anchors.left: parent.left; anchors.verticalCenter: parent.verticalCenter; text: "Wallet"; foreground: Color.popups.text }

    Text {
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      text: root.revealed ? "󰛐" : "󰛑"
      color: Color.popups.text
      opacity: hover.hovered ? 0.9 : 0.5
      font.family: Style.font.family
      font.pixelSize: Style.font.iconSmall
      HoverHandler { id: hover; cursorShape: Qt.PointingHandCursor }
      TapHandler { onTapped: root.toggleReveal() }
    }
  }

  StatRow {
    width: root.width
    label: "Spendable"
    value: !root.wallet ? "-" : (root.revealed ? Model.dcr(root.wallet.spendable) : Model.mask())
  }
  StatRow {
    width: root.width
    label: "Locked in tickets"
    value: !root.wallet ? "-" : (root.revealed ? Model.dcr(root.wallet.lockedByTickets) : Model.mask())
  }
  StatRow {
    width: root.width
    label: "Total"
    value: !root.wallet ? "-" : (root.revealed ? Model.dcr(root.wallet.total) : Model.mask())
  }
}
