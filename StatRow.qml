// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// One label/value line in the panel. Values right-align into a common column so
// the numbers line up down the card.
Item {
  id: root

  property string label: ""
  property string value: ""
  property color valueColor: Color.popups.text
  property bool dim: false

  implicitHeight: Math.max(labelText.implicitHeight, valueText.implicitHeight)

  Text {
    id: labelText
    anchors.left: parent.left
    anchors.verticalCenter: parent.verticalCenter
    text: root.label
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.65
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }

  Text {
    id: valueText
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    anchors.left: labelText.right
    anchors.leftMargin: Style.space(8)
    horizontalAlignment: Text.AlignRight
    elide: Text.ElideRight
    text: root.value
    textFormat: Text.PlainText
    color: root.valueColor
    opacity: root.dim ? 0.5 : 1.0
    font.family: Style.font.family
    font.pixelSize: Style.font.bodySmall
  }
}
