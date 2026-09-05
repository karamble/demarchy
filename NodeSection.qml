// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Node health, compressed to two rows. Once the hero and the network section
// exist this is the least-consulted part of the panel: it matters when
// something is wrong, and the rest of the time it just needs to say so
// quietly. Four rows of it pushed the card past the screen.
Column {
  id: root
  property var node: null
  spacing: Style.space(3)

  PanelSectionHeader { text: "Node Status"; foreground: Color.popups.text }

  StatRow {
    width: root.width
    label: "Status"
    value: {
      if (!root.node) return "-"
      var v = Model.nodeLine(root.node)
      return root.node.version ? v + "  ·  " + root.node.version : v
    }
    valueColor: Model.nodeHealthy(root.node) ? Color.popups.text : Color.urgent
  }
  StatRow {
    width: root.width
    label: "Height"
    value: root.node
           ? Model.count(root.node.height) + "  ·  " + Model.count(root.node.peers) + " peers"
           : "-"
  }
}
