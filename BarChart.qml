// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// Counts over time, as bars.
//
// Used for votes per week. A line was tried first and was the wrong shape: the
// interesting thing about a voting record is its rhythm and the holes in it,
// and a bar you can see is missing says that better than a line that dips.
Item {
  id: root

  property var values: []
  property color barColor: Color.accent
  // Weeks with no votes still get a stub, so a gap reads as "nothing happened
  // here" rather than as the chart having stopped.
  property real emptyStub: 1.5
  // How faint the oldest bar is. Not zero: the far end of the window is still
  // part of the record, it is just not the part you are looking for.
  property real oldestAlpha: 0.45
  // How far each bar fades toward its base.
  property real footFade: 0.55

  implicitHeight: Style.space(34)


  // Canvas gradients take CSS colour strings. Handing addColorStop a QML color
  // object does not raise anything: it just fails to take, and the gradient
  // renders as a flat fill, which is what made these charts look like blocks.
  function cssRgba(c, a) {
    return "rgba(" + Math.round(c.r * 255) + "," + Math.round(c.g * 255) + ","
           + Math.round(c.b * 255) + "," + a.toFixed(3) + ")"
  }

  Canvas {
    id: canvas
    anchors.fill: parent
    onPaint: {
      var ctx = getContext("2d")
      ctx.clearRect(0, 0, width, height)

      var v = root.values
      if (!v || v.length === 0) return

      var peak = 0
      for (var i = 0; i < v.length; i++) if (v[i] > peak) peak = v[i]
      if (peak <= 0) return

      // One-pixel gaps at this size; any more and 26 bars become stripes.
      var slot = width / v.length
      var gap = Math.min(Style.space(2), slot * 0.28)
      var w = Math.max(1, slot - gap)

      var c = root.barColor
      var last = Math.max(1, v.length - 1)

      for (var j = 0; j < v.length; j++) {
        var h = v[j] > 0 ? Math.max(root.emptyStub, height * v[j] / peak) : root.emptyStub
        var x = Math.round(j * slot)
        var top = height - h

        // Two fades, and the horizontal one carries meaning: the further back a
        // week is the fainter it sits, so the eye lands on what happened
        // recently without the older weeks disappearing.
        var recency = root.oldestAlpha + (1 - root.oldestAlpha) * (j / last)
        if (v[j] === 0) recency *= 0.35

        // The vertical one is just shape: bars lit at the top and dissolving
        // into the panel look lighter than flat blocks of colour.
        var grad = ctx.createLinearGradient(0, top, 0, height)
        grad.addColorStop(0, root.cssRgba(c, recency))
        grad.addColorStop(1, root.cssRgba(c, recency * root.footFade))
        ctx.fillStyle = grad
        ctx.fillRect(x, top, Math.round(w), h)
      }
    }
  }

  // Canvas does not track the properties its paint handler reads, so every
  // input that changes the drawing has to ask for a repaint by hand.
  onValuesChanged: canvas.requestPaint()
  onBarColorChanged: canvas.requestPaint()
  onOldestAlphaChanged: canvas.requestPaint()
  onWidthChanged: canvas.requestPaint()
  onHeightChanged: canvas.requestPaint()
}
