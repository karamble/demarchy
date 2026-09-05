// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// Two quantities that share a total, drawn as one split bar: Lightning
// outbound (what this node can send) against inbound (what it can receive).
// A channel's usefulness is the balance between them, which a pair of separate
// bars would not show.
Item {
  id: root

  property real outbound: 0
  property real inbound: 0
  property color outColor: Color.accent
  property color inColor: Color.popups.text

  readonly property real total: Math.max(1e-9, outbound + inbound)

  implicitHeight: Style.space(8)

  // Both halves fade along the bar rather than down it. This one is a single
  // horizontal run, not a series of columns, so a top-to-bottom fade fought its
  // shape: across the length it follows the direction the bar already reads
  // in, out from the left as sendable liquidity and in from the right.
  Rectangle {
    anchors.fill: parent
    radius: height / 2
    gradient: Gradient {
      orientation: Gradient.Horizontal
      GradientStop { position: 0.0; color: Qt.rgba(root.inColor.r, root.inColor.g, root.inColor.b, 0.10) }
      GradientStop { position: 1.0; color: Qt.rgba(root.inColor.r, root.inColor.g, root.inColor.b, 0.28) }
    }
  }

  Rectangle {
    id: out
    height: parent.height
    width: Math.round(parent.width * root.outbound / root.total)
    radius: height / 2
    gradient: Gradient {
      orientation: Gradient.Horizontal
      GradientStop { position: 0.0; color: Qt.rgba(root.outColor.r, root.outColor.g, root.outColor.b, 1.0) }
      GradientStop { position: 1.0; color: Qt.rgba(root.outColor.r, root.outColor.g, root.outColor.b, 0.45) }
    }
    Behavior on width { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }
  }
}
