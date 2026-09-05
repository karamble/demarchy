// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons

// A cumulative line, drawn on Canvas.
//
// Per-vote staking rewards are near-identical, so a per-vote plot is a flat
// line carrying no information. This draws the running total instead: the
// height is everything earned, and the slope is the rate: which eases as the
// block subsidy steps down, so the curve bends the way the emission does.
Item {
  id: root

  property var values: []
  property color lineColor: Color.accent
  property real lineWidth: 2.0
  // Fill under the curve. Off for very short series, where a filled wedge reads
  // as noise.
  property bool filled: true
  // Alpha directly beneath the line. It fades to nothing at the foot, so the
  // chart sits on the panel rather than in a box: but it has to start strong
  // enough to be seen at all against a dark surface, which at 0.22 it was not.
  property real fillTop: 0.62

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
      if (!v || v.length < 2) return

      var lo = v[0], hi = v[0]
      for (var i = 1; i < v.length; i++) {
        if (v[i] < lo) lo = v[i]
        if (v[i] > hi) hi = v[i]
      }
      var span = hi - lo
      // A perfectly flat series would divide by zero; draw it down the middle.
      if (span <= 0) { lo -= 0.5; span = 1 }

      var pad = root.lineWidth
      var w = width - pad * 2
      var h = height - pad * 2
      function px(i) { return pad + w * i / (v.length - 1) }
      function py(k) { return pad + h - h * (k - lo) / span }

      if (root.filled) {
        ctx.beginPath()
        ctx.moveTo(px(0), height)
        for (var j = 0; j < v.length; j++) ctx.lineTo(px(j), py(v[j]))
        ctx.lineTo(px(v.length - 1), height)
        ctx.closePath()
        // The alpha has to reach zero before the bottom edge, not at it: a fill
        // that is still faintly tinted on its last row draws a crisp line under
        // the chart and reads as a slab, which is the thing a gradient is for
        // avoiding.
        var c = root.lineColor
        var grad = ctx.createLinearGradient(0, pad, 0, height)
        grad.addColorStop(0.00, root.cssRgba(c, root.fillTop))
        grad.addColorStop(0.40, root.cssRgba(c, root.fillTop * 0.34))
        grad.addColorStop(0.80, root.cssRgba(c, 0.0))
        grad.addColorStop(1.00, root.cssRgba(c, 0.0))
        ctx.fillStyle = grad
        ctx.fill()
      }

      ctx.beginPath()
      ctx.moveTo(px(0), py(v[0]))
      for (var k = 1; k < v.length; k++) ctx.lineTo(px(k), py(v[k]))
      ctx.lineWidth = root.lineWidth
      ctx.strokeStyle = String(root.lineColor)
      ctx.lineJoin = "round"
      ctx.lineCap = "round"
      ctx.stroke()
    }
  }

  // Canvas does not track the properties its paint handler reads, so every
  // input that changes the drawing has to ask for a repaint by hand.
  onValuesChanged: canvas.requestPaint()
  onLineColorChanged: canvas.requestPaint()
  onWidthChanged: canvas.requestPaint()
  onHeightChanged: canvas.requestPaint()
}
