// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// One headline figure in the staking hero: a large value over a quiet label.
Column {
  id: root

  property string value: ""
  property string label: ""
  property color valueColor: Color.popups.text
  spacing: 0

  Text {
    text: root.value
    textFormat: Text.PlainText
    color: root.valueColor
    font.family: Style.font.family
    font.pixelSize: Style.font.heading
    font.bold: true
  }
  Text {
    text: root.label
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.5
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
