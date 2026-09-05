// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Shapes
import qs.Commons

// The Decred mark, drawn as vector paths so it can be painted in the bar's
// foreground colour like every other bar icon. Redrawing rather than loading
// the SVG is the house rule here (see TailscaleIcon.qml, DropboxIcon.qml):
// Qt's SVG renderer misbehaves at bar-slot sizes, and a loaded SVG keeps its
// baked-in fill, so it cannot follow the theme.
//
// Geometry is the official two-swoosh mark, taken verbatim from
// dcrpulse/dashboard/web/public/images/dcrpulse.svg. That file declares
// viewBox="130 110 300 300", but the art only occupies x 144..422, y 142..377
// inside it: so the box below is tightened to the ink and the Shape is
// offset to match. Using the declared box instead would pad the mark with
// dead space and leave it looking smaller than its neighbours in the bar.
//
// `monochrome: false` restores the brand blues for places that can afford
// them, such as the panel header.
Item {
  id: root

  property real iconSize: Style.font.icon
  property color color: Color.foreground
  property bool monochrome: true

  readonly property real viewWidth: 278
  readonly property real viewHeight: 235

  width: Math.round(iconSize * viewWidth / viewHeight)
  height: iconSize
  implicitWidth: width
  implicitHeight: height

  Item {
    id: canvas
    width: root.viewWidth
    height: root.viewHeight
    anchors.centerIn: parent
    scale: Math.min(root.width / root.viewWidth, root.height / root.viewHeight)

    Shape {
      // The paths carry their original absolute coordinates; this shifts the
      // ink's top-left corner onto the canvas origin.
      x: -144
      y: -142
      width: 422
      height: 377
      antialiasing: true
      preferredRendererType: Shape.CurveRenderer

      // Upper-right swoosh.
      Mark {
        brandColor: "#5da3ff"
        PathSvg { path: "M365,320l57,57H366l-94-94h58.16a47,47,0,0,0,0-94H311.2l-47-47H328a94,94,0,0,1,94,94C422,274.12,405.2,304,365,320Z" }
      }
      // Lower-left swoosh.
      Mark {
        brandColor: "#2970ff"
        PathSvg { path: "M201,199l-57-57h56.05l94,94H235.89a47,47,0,1,0,0,94H254.8l47,47H238a94,94,0,0,1-94-94C144,244.88,160.8,215,201,199Z" }
      }
    }
  }

  component Mark: ShapePath {
    id: mark
    property color brandColor: "#2970ff"

    fillColor: root.monochrome ? root.color : mark.brandColor
    strokeWidth: 0
    fillRule: ShapePath.WindingFill
  }
}
