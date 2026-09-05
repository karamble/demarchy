// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// Where a value sits inside a range, with a second marker for an estimate.
// Used for the ticket price: the network publishes a min and max for the next
// adjustment, and knowing where the current price sits between them is what
// tells a staker whether to buy now or wait.
Item {
  id: root

  property real minimum: 0
  property real maximum: 1
  property real value: 0
  property real estimate: NaN
  property color trackColor: Color.popups.text
  property color valueColor: Color.accent

  readonly property real span: Math.max(1e-9, maximum - minimum)
  function fraction(v) { return Math.max(0, Math.min(1, (v - minimum) / span)) }

  implicitHeight: Style.space(10)

  Rectangle {
    id: track
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    height: Math.max(2, Style.space(3))
    radius: height / 2
    color: Qt.rgba(root.trackColor.r, root.trackColor.g, root.trackColor.b, 0.18)
  }

  // The estimate marker: a thin upright tick, deliberately quieter than the
  // current-value dot so the two are not confused.
  Rectangle {
    visible: !isNaN(root.estimate)
    width: Math.max(1, Style.space(2))
    height: parent.height * 0.7
    radius: width / 2
    anchors.verticalCenter: parent.verticalCenter
    x: Math.round((track.width - width) * root.fraction(root.estimate))
    color: Qt.rgba(root.trackColor.r, root.trackColor.g, root.trackColor.b, 0.55)
  }

  Rectangle {
    id: dot
    width: Style.space(7)
    height: width
    radius: width / 2
    anchors.verticalCenter: parent.verticalCenter
    x: Math.round((track.width - width) * root.fraction(root.value))
    color: root.valueColor
    Behavior on x { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }
  }
}
