// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

pragma ComponentBehavior: Bound

import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// The armed triggers, grouped by who they wake, and the form that arms one.
//
// A blotter and a form, nothing more: no condition is evaluated here and no
// live value is read. The helper owns the triggers file and the samples; this
// file draws what it reports and hands back what the user typed.
Column {
  id: root

  property var triggers: []
  // The leaves a trigger can watch, with the operators each one takes. Comes
  // from the one-shot only: the snapshot does not carry it.
  property var catalogue: []
  // herdr pane id, or an agent's display name, -> { pane, status, title }.
  // Empty when herdr is not installed.
  property var agentStates: ({})
  property string activeConnection: ""
  // Why the triggers file could not be read, shown in place of the list.
  property string triggersError: ""
  property var result: null          // the helper's answer to the last command
  // Wall clock for the countdowns, ticked by the panel.
  property double now: Date.now()

  // Keyboard cursor, driven by the view. cursorRow indexes the rows in the
  // order they are drawn, group by group, or is -1 when the cursor is
  // elsewhere; actionIndex picks one of that row's buttons.
  property int cursorRow: -1
  property int actionIndex: 0
  property bool addHasCursor: false

  // While a field holds focus the panel hands it every key, so typing a reason
  // does not trip the panel's letter shortcuts. A dropdown counts twice over:
  // when its trigger has the focus and while its popup is open.
  readonly property bool formFocused: pathDropdown.activeFocus || pathDropdown.popupOpen
                                      || operatorDropdown.activeFocus || operatorDropdown.popupOpen
                                      || directionDropdown.activeFocus || directionDropdown.popupOpen
                                      || whereKeyDropdown.activeFocus || whereKeyDropdown.popupOpen
                                      || deliverDropdown.activeFocus || deliverDropdown.popupOpen
                                      || boundField.activeFocus || rearmField.activeFocus
                                      || valueField.activeFocus || holdField.activeFocus
                                      || byField.activeFocus || percentSwitch.activeFocus
                                      || forField.activeFocus || whereValueField.activeFocus
                                      || expiresField.activeFocus || reasonField.activeFocus
                                      || armButton.activeFocus || cancelButton.activeFocus

  // "" when the list is showing, "new" while arming, else the id being edited.
  property string editing: ""
  // Blank parameter fields keep their stored value on an edit, and the
  // placeholders say so instead of suggesting an example.
  readonly property bool adding: root.editing === "new"

  signal armRequested(var spec)
  signal editRequested(string id, var spec)
  // The panel hosts the confirmation, as it does for connections: ConfirmDialog
  // is an overlay Item and needs a surface it can fill, which a Column cannot
  // give it.
  signal disarmRequested(string id, string label)
  // Raised when the form lets go of the keyboard, so the panel can take it
  // back and the row cursor works again.
  signal focusReleased()

  spacing: Style.space(6)

  // ---- grouping
  //
  // Rows are grouped by recipient so a glance answers "what is waiting on this
  // agent". Groups keep the order they first appear in, except the user's own,
  // which goes last: an agent's alerts are the reason the list exists, and the
  // desktop notifications are the fallback.
  readonly property var groups: {
    var order = [], byKey = {}
    for (var i = 0; i < root.triggers.length; i++) {
      var t = root.triggers[i]
      var k = String(t.deliverTo || "you")
      if (!byKey[k]) {
        byKey[k] = { key: k, triggers: [] }
        order.push(k)
      }
      byKey[k].triggers.push(t)
    }
    var out = []
    for (var j = 0; j < order.length; j++)
      if (order[j] !== "you") out.push(byKey[order[j]])
    if (byKey["you"]) out.push(byKey["you"])
    // The cursor walks one flat index across every group, so each group
    // remembers where its rows start.
    var first = 0
    for (var g = 0; g < out.length; g++) {
      out[g].first = first
      first += out[g].triggers.length
    }
    return out
  }

  // The same triggers in drawn order, which is what the cursor indexes.
  readonly property var ordered: {
    var out = []
    for (var g = 0; g < root.groups.length; g++)
      out = out.concat(root.groups[g].triggers)
    return out
  }

  // ---- actions
  //
  // A row's buttons follow its state, so the keyboard has to walk the same set
  // the mouse sees: a spent trigger (fired, expired, or one whose delivery
  // failed) has nothing left to edit, only a record to clear.
  function editable(status) {
    return status === "armed" || status === "rearming"
        || status === "no-sample" || status === "other-connection"
  }

  function actionsFor(trigger) {
    var acts = []
    if (trigger && root.editable(String(trigger.status))) acts.push("edit")
    acts.push("disarm")
    return acts
  }

  function actionCount(rowIndex) {
    if (rowIndex < 0 || rowIndex >= root.ordered.length) return 0
    return root.actionsFor(root.ordered[rowIndex]).length
  }

  function actionAt(rowIndex, index) {
    var n = root.actionCount(rowIndex)
    if (n === 0) return ""
    return root.actionsFor(root.ordered[rowIndex])[Math.max(0, Math.min(index, n - 1))]
  }

  // label is how the confirmation names a trigger: the condition as the
  // helper words it, which is what the user armed.
  function label(trigger) {
    return String(trigger.path || "") + " " + String(trigger.operator || "") + " " + String(trigger.params || "")
  }

  function runAction(rowIndex, index) {
    var t = root.ordered[rowIndex]
    if (!t) return
    switch (root.actionAt(rowIndex, index)) {
    case "edit": root.beginEdit(t); break
    case "disarm": root.disarmRequested(t.id, root.label(t)); break
    }
  }

  // ---- form state
  //
  // The dropdowns hold their own value, so the chosen path and operator are
  // mirrored here where the rest of the form can bind to them.
  property string formPath: ""
  property string formOperator: ""
  // The recipient of the trigger being edited, kept on the menu even when
  // herdr no longer lists it, so saving does not silently move the alert.
  property string editingDeliverTo: ""

  function leafFor(path) {
    for (var i = 0; i < root.catalogue.length; i++)
      if (root.catalogue[i].path === path) return root.catalogue[i]
    return null
  }

  readonly property var leaf: root.leafFor(root.formPath)
  readonly property var pathOptions: {
    var out = []
    for (var i = 0; i < root.catalogue.length; i++) out.push(String(root.catalogue[i].path))
    return out
  }
  readonly property var operatorOptions: (root.leaf && root.leaf.operators) ? root.leaf.operators : []
  readonly property var whereFields: (root.leaf && root.leaf.fields) ? root.leaf.fields : []

  // Which parameters an operator takes. Mirrors the helper's own table; a
  // field that is not offered is simply not sent.
  readonly property bool wantsBound: root.formOperator === "crosses" || root.formOperator === "count"
  readonly property bool wantsValue: root.formOperator === "becomes"
  readonly property bool wantsBy: root.formOperator === "changes"
  readonly property bool wantsFor: root.formOperator === "stalls"
  readonly property bool wantsWhere: (root.formOperator === "appears" || root.formOperator === "disappears")
                                     && root.whereFields.length > 0

  // Direction can be left alone on an edit, which a two-way menu cannot say;
  // the blank third entry is how it says it.
  readonly property var directionOptions: root.editing === "new"
                                          ? ["above", "below"]
                                          : [{ value: "", label: "unchanged" }, "above", "below"]

  // Who can be woken: the user, then every agent herdr can see.
  readonly property var deliverOptions: {
    var out = [{ value: "you", label: "you" }]
    var seen = { you: true }
    for (var id in root.agentStates) {
      var a = root.agentStates[id]
      // A pane id and a display name point at the same agent; offer it once,
      // addressed by the pane id, which every agent has.
      if (a && typeof a === "object" && a.pane && a.pane !== id) continue
      if (seen[id]) continue
      seen[id] = true
      out.push({ value: id, label: Model.recipientLabel(id, root.agentStates) })
    }
    if (root.editingDeliverTo !== "" && !seen[root.editingDeliverTo])
      out.push({ value: root.editingDeliverTo, label: root.editingDeliverTo })
    return out
  }

  function beginAdd() {
    root.editing = "new"
    root.editingDeliverTo = ""
    root.setPath(root.pathOptions.length > 0 ? root.pathOptions[0] : "")
    root.clearParams()
    directionDropdown.value = "above"
    expiresField.text = ""
    deliverDropdown.value = "you"
    reasonField.text = ""
    pathDropdown.forceActiveFocus()
  }

  function beginEdit(trigger) {
    root.editing = String(trigger.id)
    root.editingDeliverTo = String(trigger.deliverTo || "you")
    root.setPath(String(trigger.path || ""))
    root.setOperator(String(trigger.operator || ""))
    // The parameters arrive as one human sentence and cannot be split back
    // into fields, so they start blank, and blank means keep.
    root.clearParams()
    directionDropdown.value = ""
    expiresField.text = String(trigger.expiresAt || "")
    deliverDropdown.value = root.editingDeliverTo
    reasonField.text = String(trigger.reason || "")
    pathDropdown.forceActiveFocus()
  }

  function setPath(path) {
    root.formPath = path
    pathDropdown.value = path
    // A new leaf may not take the operator chosen for the old one, and has
    // its own fields to filter on.
    if (root.operatorOptions.indexOf(root.formOperator) < 0)
      root.setOperator(root.operatorOptions.length > 0 ? String(root.operatorOptions[0]) : "")
    whereKeyDropdown.value = root.whereFields.length > 0 ? String(root.whereFields[0]) : ""
  }

  function setOperator(op) {
    root.formOperator = op
    operatorDropdown.value = op
  }

  function clearParams() {
    boundField.text = ""
    rearmField.text = ""
    valueField.text = ""
    holdField.text = ""
    byField.text = ""
    percentSwitch.checked = false
    forField.text = ""
    whereValueField.text = ""
  }

  function cancel() {
    root.editing = ""
    root.focusReleased()
  }

  // Builds the command the helper expects. Parameter keys that were left blank
  // are omitted rather than sent empty: on an edit that is how a value is
  // kept, and on an arm the helper says which ones it needed.
  function submit() {
    var spec = { path: root.formPath, operator: root.formOperator, deliverTo: deliverDropdown.value }
    var params = {}
    if (root.wantsBound) {
      if (directionDropdown.value !== "") params.direction = directionDropdown.value
      root.putNumber(params, "bound", boundField.text)
      root.putNumber(params, "rearm", rearmField.text)
    }
    if (root.wantsValue) {
      root.putText(params, "value", valueField.text)
      root.putText(params, "hold", holdField.text)
    }
    if (root.wantsBy) {
      root.putNumber(params, "by", byField.text)
      if (percentSwitch.checked) params.percent = true
    }
    if (root.wantsFor) root.putText(params, "for", forField.text)
    if (root.wantsWhere && whereKeyDropdown.value !== "" && whereValueField.text.trim() !== "") {
      params.where = {}
      params.where[whereKeyDropdown.value] = whereValueField.text.trim()
    }
    if (Object.keys(params).length > 0) spec.params = params
    if (expiresField.text.trim() !== "") spec.expires = expiresField.text.trim()
    if (reasonField.text.trim() !== "") spec.reason = reasonField.text.trim()
    if (root.editing === "new") root.armRequested(spec)
    else root.editRequested(root.editing, spec)
  }

  function putText(params, key, text) {
    var s = String(text).trim()
    if (s !== "") params[key] = s
  }

  // A number is sent as one; anything else goes through as typed, so the
  // helper's refusal names the field rather than the form guessing at it.
  function putNumber(params, key, text) {
    var s = String(text).trim()
    if (s === "") return
    var n = Number(s)
    params[key] = isFinite(n) ? n : s
  }

  // ---- the list
  Repeater {
    model: (root.editing === "" && root.triggersError === "") ? root.groups : []

    delegate: Column {
      id: group
      required property var modelData
      required property int index
      width: root.width
      spacing: 0

      readonly property string recipient: String(group.modelData.key)
      readonly property string liveness: Model.recipientState(group.recipient, root.agentStates)

      // The group header is not a row: there is nothing to do to a recipient,
      // so the cursor skips it.
      Item {
        width: parent.width
        implicitHeight: Style.space(18)

        Text {
          anchors.left: parent.left
          anchors.leftMargin: Style.space(4)
          anchors.right: livenessText.left
          anchors.rightMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          text: Model.recipientLabel(group.recipient, root.agentStates)
          textFormat: Text.PlainText
          elide: Text.ElideRight
          color: Color.popups.text
          opacity: 0.6
          font.family: Style.font.family
          font.pixelSize: Style.font.caption
          font.bold: true
        }
        Text {
          id: livenessText
          anchors.right: parent.right
          anchors.rightMargin: Style.space(4)
          anchors.verticalCenter: parent.verticalCenter
          text: group.liveness
          textFormat: Text.PlainText
          color: group.liveness === "not running" ? Color.urgent : Color.popups.text
          opacity: group.liveness === "not running" ? 0.9 : 0.45
          font.family: Style.font.family
          font.pixelSize: Style.font.caption
        }
      }

      Repeater {
        model: group.modelData.triggers

        delegate: Item {
          id: alertRow
          required property var modelData
          required property int index
          width: root.width
          implicitHeight: Style.space(30)

          readonly property int rowIndex: group.modelData.first + alertRow.index
          readonly property string status: String(alertRow.modelData.status || "")
          readonly property bool hasCursor: alertRow.rowIndex === root.cursorRow
          readonly property string cursorAction: alertRow.hasCursor
                                                 ? root.actionAt(alertRow.rowIndex, root.actionIndex) : ""
          readonly property bool urgent: Model.alertUrgent(alertRow.status)

          // The right-hand side: how long a live trigger has left, or for a
          // spent one, what became of it.
          readonly property bool countingDown: alertRow.status === "armed"
                                               || alertRow.status === "rearming"
                                               || alertRow.status === "no-sample"
          readonly property string whenText: {
            var t = alertRow.modelData
            switch (alertRow.status) {
            case "fired":
              return "fired " + Model.clockTime(t.firedAt)
                     + (t.delivered ? " · delivered to " + t.delivered : "")
            case "other-connection": return "armed on " + String(t.connection || "?")
            case "expired": return "expired"
            case "delivery-failed": return "delivery failed"
            }
            return Model.countdown(t.expiresAt, root.now)
          }
          readonly property bool whenUrgent: alertRow.urgent
                                             || (alertRow.countingDown
                                                 && Model.expiresSoon(alertRow.modelData.expiresAt, root.now))

          // The second line: why it was armed, what went wrong delivering it,
          // and whether the user has changed an agent's trigger since.
          readonly property string reasonLine: {
            var t = alertRow.modelData
            var parts = []
            if (t.reason) parts.push(String(t.reason))
            if (t.deliveryError) parts.push(String(t.deliveryError))
            if (Number(t.rev) > 1 && t.armedBy !== "you") parts.push("edited by you")
            return parts.join(" · ")
          }

          Rectangle {
            anchors.fill: parent
            anchors.margins: Style.space(1)
            radius: Style.cornerRadius
            color: rowHover.hovered || alertRow.hasCursor
                   ? Style.hoverFillFor(Color.popups.text, Color.accent) : "transparent"
          }
          HoverHandler { id: rowHover }

          Column {
            anchors.left: parent.left
            anchors.leftMargin: Style.space(4)
            anchors.right: rowRight.left
            anchors.rightMargin: Style.space(6)
            anchors.verticalCenter: parent.verticalCenter
            spacing: 0

            Row {
              id: conditionRow
              width: parent.width
              spacing: Style.space(5)

              Text {
                id: glyphText
                text: Model.alertGlyph(alertRow.status)
                textFormat: Text.PlainText
                color: alertRow.urgent ? Color.urgent : Color.popups.text
                font.family: Style.font.family
                font.pixelSize: Style.font.bodySmall
              }
              Text {
                id: pathText
                text: String(alertRow.modelData.path || "")
                textFormat: Text.PlainText
                color: Color.popups.text
                font.family: Style.font.family
                font.pixelSize: Style.font.bodySmall
                font.weight: Font.Medium
              }
              Text {
                width: Math.max(0, conditionRow.width - glyphText.width - pathText.width
                                   - conditionRow.spacing * 2)
                // Operator and parameters together: "where fromNick=alice"
                // alone cannot say whether it is an appearance or a loss.
                text: String(alertRow.modelData.operator || "") + " " + String(alertRow.modelData.params || "")
                textFormat: Text.PlainText
                elide: Text.ElideRight
                color: Color.popups.text
                opacity: 0.75
                font.family: Style.font.family
                font.pixelSize: Style.font.bodySmall
              }
            }
            Text {
              width: parent.width
              visible: text !== ""
              text: alertRow.reasonLine
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: Color.popups.text
              opacity: 0.45
              font.family: Style.font.family
              font.pixelSize: Style.font.caption
            }
          }

          Row {
            id: rowRight
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            spacing: Style.space(4)

            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: alertRow.whenText
              textFormat: Text.PlainText
              color: alertRow.whenUrgent ? Color.urgent : Color.popups.text
              opacity: alertRow.whenUrgent ? 0.9 : 0.5
              font.family: Style.font.family
              font.pixelSize: Style.font.caption
            }
            Button {
              text: "Edit"
              // A spent trigger has nothing left to change; the same rule as
              // actionsFor, so the keyboard and the mouse see one set.
              visible: root.editable(alertRow.status)
              focusable: true
              hasCursor: alertRow.cursorAction === "edit"
              fontSize: Style.font.caption
              foreground: Color.popups.text
              onClicked: root.beginEdit(alertRow.modelData)
            }
            Button {
              text: "Disarm"
              focusable: true
              hasCursor: alertRow.cursorAction === "disarm"
              fontSize: Style.font.caption
              foreground: Color.urgent
              onClicked: root.disarmRequested(alertRow.modelData.id, root.label(alertRow.modelData))
            }
          }
        }
      }
    }
  }

  // ---- nothing to list, or no list to show
  Text {
    width: root.width
    visible: root.editing === "" && root.triggersError === "" && root.triggers.length === 0
    wrapMode: Text.WordWrap
    text: "Nothing armed. An agent arms one with: demarchy-setup arm price.dcrUsd "
          + "crosses --above 16 --expires 4d --reason \"...\""
    textFormat: Text.PlainText
    color: Color.popups.text
    opacity: 0.45
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  Text {
    width: root.width
    visible: root.editing === "" && root.triggersError !== ""
    wrapMode: Text.WordWrap
    text: root.triggersError
    textFormat: Text.PlainText
    color: Color.urgent
    opacity: 0.9
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }

  Button {
    visible: root.editing === ""
    text: "+ Add alert"
    bordered: true
    focusable: true
    hasCursor: root.addHasCursor
    foreground: Color.popups.text
    onClicked: root.beginAdd()
  }

  // ---- the form
  //
  // Every field shares one escape hatch and one submit, and every menu the
  // same keyboard opening, so each is declared once. A menu's popup hands the
  // focus back to the window when it closes rather than to the control that
  // opened it; taking it back a beat later is what keeps Tab walking the form
  // after a pick.
  component FormDropdown: Dropdown {
    id: dropdown
    foreground: Color.popups.text
    hasCursor: dropdown.activeFocus
    Keys.onPressed: function (event) {
      if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter
          || event.key === Qt.Key_Space || event.key === Qt.Key_Down) {
        dropdown.open()
        event.accepted = true
      } else if (event.key === Qt.Key_Escape) {
        root.cancel()
        event.accepted = true
      }
    }
    onPopupOpenChanged: {
      if (!dropdown.popupOpen && root.editing !== "")
        Qt.callLater(function () { dropdown.forceActiveFocus() })
    }
  }

  component FormField: TextField {
    foreground: Color.popups.text
    Keys.onEscapePressed: root.cancel()
    onAccepted: root.submit()
  }

  Column {
    width: root.width
    visible: root.editing !== ""
    spacing: Style.space(5)

    Text {
      text: root.adding ? "New alert" : "Edit alert"
      color: Color.popups.text
      font.family: Style.font.family
      font.pixelSize: Style.font.bodySmall
      font.bold: true
    }

    FormDropdown {
      id: pathDropdown
      width: parent.width
      label: "path"
      options: root.pathOptions
      KeyNavigation.tab: operatorDropdown
      KeyNavigation.backtab: cancelButton
      onChanged: function (v) { root.setPath(v) }
    }
    FormDropdown {
      id: operatorDropdown
      width: parent.width
      label: "operator"
      options: root.operatorOptions
      KeyNavigation.tab: directionDropdown
      KeyNavigation.backtab: pathDropdown
      onChanged: function (v) { root.setOperator(v) }
    }

    // crosses and count: a bound, a side to cross it from, and where to
    // re-arm for a standing trigger.
    FormDropdown {
      id: directionDropdown
      width: parent.width
      visible: root.wantsBound
      label: "direction"
      options: root.directionOptions
      KeyNavigation.tab: boundField
      KeyNavigation.backtab: operatorDropdown
    }
    FormField {
      id: boundField
      width: parent.width
      visible: root.wantsBound
      placeholderText: root.adding ? "bound, e.g. 16" : "unchanged"
      KeyNavigation.tab: rearmField
      KeyNavigation.backtab: directionDropdown
    }
    FormField {
      id: rearmField
      width: parent.width
      visible: root.wantsBound
      placeholderText: root.adding ? "re-arm at, e.g. 15 (optional)" : "unchanged"
      KeyNavigation.tab: valueField
      KeyNavigation.backtab: boundField
    }

    // becomes: a value, and how long it has to hold before it counts.
    FormField {
      id: valueField
      width: parent.width
      visible: root.wantsValue
      placeholderText: root.adding ? "value, e.g. stopped" : "unchanged"
      KeyNavigation.tab: holdField
      KeyNavigation.backtab: rearmField
    }
    FormField {
      id: holdField
      width: parent.width
      visible: root.wantsValue
      placeholderText: root.adding ? "hold for, e.g. 5m (optional)" : "unchanged"
      KeyNavigation.tab: byField
      KeyNavigation.backtab: valueField
    }

    // changes: by how much, as an amount or a share.
    FormField {
      id: byField
      width: parent.width
      visible: root.wantsBy
      placeholderText: root.adding ? "by, e.g. 5" : "unchanged"
      KeyNavigation.tab: percentSwitch
      KeyNavigation.backtab: holdField
    }
    Row {
      visible: root.wantsBy
      spacing: Style.space(6)

      ToggleSwitch {
        id: percentSwitch
        anchors.verticalCenter: parent.verticalCenter
        foreground: Color.popups.text
        hasCursor: percentSwitch.activeFocus
        onToggled: percentSwitch.checked = !percentSwitch.checked
        KeyNavigation.tab: forField
        KeyNavigation.backtab: byField
        // The switch has no keyboard handling of its own, so it gets the
        // same keys a button answers to.
        Keys.onPressed: function (event) {
          if (event.key === Qt.Key_Space || event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
            percentSwitch.checked = !percentSwitch.checked
            event.accepted = true
          } else if (event.key === Qt.Key_Escape) {
            root.cancel()
            event.accepted = true
          }
        }
      }
      Text {
        anchors.verticalCenter: parent.verticalCenter
        text: "as a percentage"
        textFormat: Text.PlainText
        color: Color.popups.text
        opacity: 0.7
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
      }
    }

    // stalls: how long without a change.
    FormField {
      id: forField
      width: parent.width
      visible: root.wantsFor
      placeholderText: root.adding ? "for, e.g. 45m" : "unchanged"
      KeyNavigation.tab: whereKeyDropdown
      KeyNavigation.backtab: percentSwitch
    }

    // appears and disappears: one field=value filter, from the leaf's own
    // fields so nothing is offered that the helper cannot match on.
    Row {
      id: whereRow
      width: parent.width
      visible: root.wantsWhere
      spacing: Style.space(6)

      FormDropdown {
        id: whereKeyDropdown
        width: Math.round(whereRow.width * 0.4)
        label: "where"
        options: root.whereFields
        KeyNavigation.tab: whereValueField
        KeyNavigation.backtab: forField
      }
      FormField {
        id: whereValueField
        width: whereRow.width - whereKeyDropdown.width - whereRow.spacing
        anchors.bottom: whereKeyDropdown.bottom
        placeholderText: root.adding ? "equals, e.g. alice" : "unchanged"
        KeyNavigation.tab: expiresField
        KeyNavigation.backtab: whereKeyDropdown
      }
    }

    FormField {
      id: expiresField
      width: parent.width
      placeholderText: root.adding ? "expires: 4d, 12h, or a date" : "expires: unchanged"
      KeyNavigation.tab: deliverDropdown
      KeyNavigation.backtab: whereValueField
    }
    FormDropdown {
      id: deliverDropdown
      width: parent.width
      label: "deliver to"
      options: root.deliverOptions
      KeyNavigation.tab: reasonField
      KeyNavigation.backtab: expiresField
    }
    FormField {
      id: reasonField
      width: parent.width
      placeholderText: "reason, so whoever is woken knows why"
      KeyNavigation.tab: armButton
      KeyNavigation.backtab: deliverDropdown
    }

    Row {
      spacing: Style.space(6)
      Button {
        id: armButton
        text: root.adding ? "Arm" : "Save"
        bordered: true
        focusable: true
        foreground: Color.accent
        KeyNavigation.tab: cancelButton
        KeyNavigation.backtab: reasonField
        Keys.onEscapePressed: root.cancel()
        onClicked: root.submit()
      }
      Button {
        id: cancelButton
        text: "Cancel"
        focusable: true
        foreground: Color.popups.text
        KeyNavigation.tab: pathDropdown
        KeyNavigation.backtab: armButton
        Keys.onEscapePressed: root.cancel()
        onClicked: root.cancel()
      }
    }
  }

  // ---- what the helper said
  Text {
    width: root.width
    visible: !!root.result && !root.result.ok
    wrapMode: Text.WordWrap
    text: root.result ? Model.alertError(root.result) : ""
    textFormat: Text.PlainText
    color: Color.urgent
    font.family: Style.font.family
    font.pixelSize: Style.font.caption
  }
}
