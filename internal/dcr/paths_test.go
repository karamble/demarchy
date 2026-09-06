// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

func mustLeaf(t *testing.T, path string) Leaf {
	t.Helper()
	l, ok := LookupLeaf(path)
	if !ok {
		t.Fatalf("%s is not in the catalogue", path)
	}
	return l
}

// The catalogue is what an agent reads to learn what it may arm, so these are
// the shapes the CLI documentation promises.
func TestCatalogueShapes(t *testing.T) {
	number := []string{"crosses", "changes", "stalls"}
	scalar := []string{"becomes", "stalls"}
	list := []string{"appears", "disappears", "count"}
	cases := []struct {
		path     string
		kind     Kind
		integer  bool
		ops      []string
		fields   []string
		identity []string
	}{
		{"price.dcrUsd", KindNumber, false, number, nil, nil},
		{"node.height", KindNumber, true, number, nil, nil},
		{"wallet.synced", KindBool, false, scalar, nil, nil},
		{"node.status", KindText, false, scalar, nil, nil},
		{"staking.own.live", KindNumber, true, number, nil, nil},
		// Identity is in declaration order; gcName is a field but not identity,
		// because it is filled from a cache and can differ between two samples
		// of the same message.
		{"br.messages", KindList, false, list,
			[]string{"type", "fromNick", "text", "gcid", "gcName"},
			[]string{"type", "fromNick", "text", "gcid"}},
		{"br.notices", KindList, false, list,
			[]string{"id", "ts", "severity", "subject", "detail"},
			[]string{"id"}},
		{"lightning.list", KindList, false, list,
			[]string{"alias", "capacity", "local", "remote", "active"},
			[]string{"alias"}},
		// A list of numbers has nothing to tell one element from another.
		{"price.series", KindList, false, []string{"count"}, nil, nil},
		{"staking.own.voteBuckets", KindList, false, []string{"count"}, nil, nil},
	}
	for _, c := range cases {
		l := mustLeaf(t, c.path)
		if l.Kind != c.kind || l.Integer != c.integer {
			t.Errorf("%s: kind %s integer %v, want %s %v", c.path, l.Kind, l.Integer, c.kind, c.integer)
		}
		if !reflect.DeepEqual(l.Operators, c.ops) {
			t.Errorf("%s: operators %v, want %v", c.path, l.Operators, c.ops)
		}
		if !reflect.DeepEqual(l.Fields, c.fields) {
			t.Errorf("%s: fields %v, want %v", c.path, l.Fields, c.fields)
		}
		if !reflect.DeepEqual(l.Identity, c.identity) {
			t.Errorf("%s: identity %v, want %v", c.path, l.Identity, c.identity)
		}
		if want := regexp.MustCompile(`^[^.]+`).FindString(c.path); l.Section != want {
			t.Errorf("%s: section %q, want %q", c.path, l.Section, want)
		}
	}
}

func TestCatalogueSortedAndUnique(t *testing.T) {
	all := Catalogue()
	if len(all) == 0 {
		t.Fatal("empty catalogue")
	}
	if !sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Path < all[j].Path }) {
		t.Error("catalogue is not sorted by path")
	}
	seen := map[string]bool{}
	for _, l := range all {
		if seen[l.Path] {
			t.Errorf("%s listed twice", l.Path)
		}
		seen[l.Path] = true
		if _, ok := LookupLeaf(l.Path); !ok {
			t.Errorf("%s is in Catalogue but not LookupLeaf", l.Path)
		}
	}
	// A leaf inside a list element is not a path: elements are addressed by
	// identity, not position.
	for _, bad := range []string{"br.messages.text", "lightning.list.alias", "price", "br", ""} {
		if _, ok := LookupLeaf(bad); ok {
			t.Errorf("%q should not be a leaf", bad)
		}
	}
}

// Every pointer-to-struct field of Snapshot is either a section an agent may
// watch or helper metadata it may not. A new one has to be placed in one list
// before it builds, so nothing becomes armable by accident.
func TestSnapshotSectionsClassified(t *testing.T) {
	st := reflect.TypeOf(Snapshot{})
	listed := map[string]int{}
	for _, n := range sections {
		listed[n]++
	}
	for _, n := range meta {
		listed[n]++
	}
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.Struct {
			if listed[f.Name] > 0 {
				t.Errorf("Snapshot.%s is listed but is not a pointer to a struct", f.Name)
			}
			continue
		}
		if listed[f.Name] != 1 {
			t.Errorf("Snapshot.%s appears %d times across sections and meta; classify it exactly once",
				f.Name, listed[f.Name])
		}
	}
	for name := range listed {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("%s is listed but Snapshot has no such field", name)
		}
	}
}

func TestKindJSON(t *testing.T) {
	b, err := json.Marshal(Leaf{Path: "x", Kind: KindList, Operators: []string{"count"}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &got); err != nil || got.Kind != "list" {
		t.Fatalf("kind marshalled as %s, want \"list\"", b)
	}
	if _, err := json.Marshal(Kind(9)); err == nil {
		t.Error("an unknown kind should not marshal")
	}
}

// Zero and empty are samples; nil and unreachable are not. The whole reason
// Resolve walks the struct instead of the JSON is to keep those apart.
func TestResolveDistinguishesEmptyFromAbsent(t *testing.T) {
	snap := &Snapshot{
		Reachable: true,
		Node:      &Node{Height: 900000, Status: "synced"},
		Price:     &Price{DcrUsd: 16, Change: 0},
		Lightning: &Lightning{List: nil},
	}
	if _, ok := Resolve(snap, "wallet.total"); ok {
		t.Error("a nil section should not resolve")
	}
	if v, ok := Resolve(snap, "lightning.list"); !ok || v.Kind != KindList || len(v.Items) != 0 {
		t.Errorf("a nil slice under a present section should be an empty list, got %+v ok=%v", v, ok)
	}
	if v, ok := Resolve(snap, "price.change"); !ok || v.Kind != KindNumber || v.Num != 0 {
		t.Errorf("a zero under a present section should be a zero, got %+v ok=%v", v, ok)
	}
	if v, ok := Resolve(snap, "price.dcrUsd"); !ok || v.Num != 16 {
		t.Errorf("price.dcrUsd = %+v ok=%v", v, ok)
	}
	if v, ok := Resolve(snap, "node.height"); !ok || v.Num != 900000 {
		t.Errorf("node.height = %+v ok=%v", v, ok)
	}
	if v, ok := Resolve(snap, "node.status"); !ok || v.Kind != KindText || v.Text != "synced" {
		t.Errorf("node.status = %+v ok=%v", v, ok)
	}
	if _, ok := Resolve(snap, "price.nothing"); ok {
		t.Error("an unknown path should not resolve")
	}
	if _, ok := Resolve(nil, "price.dcrUsd"); ok {
		t.Error("a nil snapshot should not resolve")
	}
	snap.Reachable = false
	if _, ok := Resolve(snap, "price.dcrUsd"); ok {
		t.Error("an unreachable snapshot should not resolve, whatever it carries")
	}
}

func TestResolveListItems(t *testing.T) {
	snap := &Snapshot{
		Reachable: true,
		BR: &BR{Messages: []Message{
			{Type: "pm", FromNick: "alice", Text: "hi"},
			{Type: "gcm", FromNick: "bob", Text: "yo", GCID: "abc", GCName: "lounge"},
		}},
		Lightning: &Lightning{List: []LnChannel{{Alias: "hub", Capacity: 16, Local: 0.5, Active: true}}},
		Price:     &Price{Series: []float64{1, 2, 2}},
		Staking:   &Staking{Own: OwnStaking{Live: 3}},
	}
	v, ok := Resolve(snap, "br.messages")
	if !ok || len(v.Items) != 2 {
		t.Fatalf("br.messages = %+v ok=%v", v, ok)
	}
	if want := []string{"gcm", "bob", "yo", "abc"}; !reflect.DeepEqual(v.Items[1].ID, want) {
		t.Errorf("identity %v, want %v", v.Items[1].ID, want)
	}
	if v.Items[1].Fields["gcName"] != "lounge" || v.Items[0].Fields["gcid"] != "" {
		t.Errorf("fields %v", v.Items[1].Fields)
	}

	// Numbers print the way an agent types them, so a where-filter matches.
	v, ok = Resolve(snap, "lightning.list")
	if !ok || len(v.Items) != 1 {
		t.Fatalf("lightning.list = %+v ok=%v", v, ok)
	}
	want := map[string]string{"alias": "hub", "capacity": "16", "local": "0.5", "remote": "0", "active": "true"}
	if !reflect.DeepEqual(v.Items[0].Fields, want) {
		t.Errorf("fields %v, want %v", v.Items[0].Fields, want)
	}
	if !reflect.DeepEqual(v.Items[0].ID, []string{"hub"}) {
		t.Errorf("identity %v, want [hub]", v.Items[0].ID)
	}

	if v, ok = Resolve(snap, "price.series"); !ok || len(v.Items) != 3 {
		t.Errorf("price.series should count three, got %+v ok=%v", v, ok)
	}
	if v, ok = Resolve(snap, "staking.own.live"); !ok || v.Num != 3 {
		t.Errorf("staking.own.live = %+v ok=%v", v, ok)
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint([]string{"gcm", "bob", "yo", "abc"})
	if a != Fingerprint([]string{"gcm", "bob", "yo", "abc"}) {
		t.Error("fingerprint is not stable")
	}
	if !regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(a) {
		t.Errorf("fingerprint %q is not 24 hex chars", a)
	}
	if a == Fingerprint([]string{"gcm", "bob", "yo", "abd"}) {
		t.Error("different identities collide")
	}
	// The separator keeps ["ab", "c"] apart from ["a", "bc"].
	if Fingerprint([]string{"ab", "c"}) == Fingerprint([]string{"a", "bc"}) {
		t.Error("field boundaries are not part of the fingerprint")
	}
}

// A stored last_value has to come back as something the next sample can be
// compared against, for every kind, or the evaluator restarts from nothing
// after every helper restart.
func TestValueRoundTrip(t *testing.T) {
	items := []Item{
		{Fields: map[string]string{"alias": "hub"}, ID: []string{"hub"}},
		{Fields: map[string]string{"alias": "spoke"}, ID: []string{"spoke"}},
	}
	for _, v := range []Value{
		{Kind: KindNumber, Num: 16.25},
		{Kind: KindNumber, Num: 0},
		{Kind: KindText, Text: "synced"},
		{Kind: KindText, Text: ""},
		{Kind: KindBool, Bool: true},
		{Kind: KindBool, Bool: false},
		{Kind: KindList, Items: items},
		{Kind: KindList, Items: nil},
	} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %+v: %v", v, err)
		}
		back, ok := decodeValue(raw, v.Kind)
		if !ok {
			t.Errorf("%s did not decode as %s", raw, v.Kind)
			continue
		}
		if !equalValue(v, back) {
			t.Errorf("%+v round-tripped through %s to %+v", v, raw, back)
		}
		if v.Kind == KindList && len(back.Items) != len(v.Items) {
			t.Errorf("list of %d came back as %d", len(v.Items), len(back.Items))
		}
	}

	// A decoded list must also tell a changed sample apart.
	raw, _ := json.Marshal(Value{Kind: KindList, Items: items})
	back, _ := decodeValue(raw, KindList)
	if equalValue(back, Value{Kind: KindList, Items: items[:1]}) {
		t.Error("a decoded list matched a sample missing an item")
	}
	if !equalValue(back, Value{Kind: KindList, Items: []Item{items[1], items[0]}}) {
		t.Error("list order should not matter")
	}

	for _, bad := range []struct {
		raw  string
		kind Kind
	}{
		{"", KindNumber}, {"null", KindNumber}, {`"16"`, KindNumber}, {"16", KindText},
		{"1", KindBool}, {`{"a":1}`, KindList}, {"[1,2]", KindList}, {"16", Kind(9)},
	} {
		if v, ok := decodeValue(json.RawMessage(bad.raw), bad.kind); ok {
			t.Errorf("decodeValue(%q, %s) = %+v, want no value", bad.raw, bad.kind, v)
		}
	}
	if _, err := json.Marshal(Value{Kind: Kind(9)}); err == nil {
		t.Error("an unknown kind should not marshal")
	}
}

func TestEqualValue(t *testing.T) {
	if equalValue(Value{Kind: KindNumber, Num: 1}, Value{Kind: KindText, Text: "1"}) {
		t.Error("kinds differ")
	}
	if !equalValue(Value{Kind: KindNumber, Num: 1}, Value{Kind: KindNumber, Num: 1}) {
		t.Error("equal numbers")
	}
	if equalValue(Value{Kind: KindBool, Bool: true}, Value{Kind: KindBool}) {
		t.Error("bools differ")
	}
	if !equalValue(Value{Kind: KindList}, Value{Kind: KindList, Items: []Item{}}) {
		t.Error("nil and empty lists are the same sample")
	}
}

// The DEX spot is its own section, so a token without the grant yields no
// sample rather than a row of zeros.
func TestDexSectionLeaves(t *testing.T) {
	rate, ok := LookupLeaf("dex.rate")
	if !ok || rate.Kind != KindNumber || !rate.Integer || fmt.Sprint(rate.Operators) != "[crosses changes stalls]" {
		t.Fatalf("dex.rate: %+v %v", rate, ok)
	}
	if host, ok := LookupLeaf("dex.host"); !ok || host.Kind != KindText {
		t.Fatalf("dex.host: %+v %v", host, ok)
	}
	if premium, ok := LookupLeaf("dex.premium"); !ok || premium.Kind != KindNumber || premium.Integer {
		t.Fatalf("dex.premium: %+v %v", premium, ok)
	}
	if _, ok := Resolve(&Snapshot{Reachable: true}, "dex.rate"); ok {
		t.Fatal("no Dex section must be no sample")
	}
	v, ok := Resolve(&Snapshot{Reachable: true, Dex: &Dex{Rate: 19980}}, "dex.rate")
	if !ok || v.Num != 19980 {
		t.Fatalf("dex.rate resolved to %+v %v", v, ok)
	}
}

// The audit and the bridge are sections of their own, each behind its grant,
// so a token without one yields no sample rather than an empty list.
func TestAuditAndBRMCPLeaves(t *testing.T) {
	entries, ok := LookupLeaf("audit.entries")
	if !ok || entries.Kind != KindList || fmt.Sprint(entries.Identity) != "[time agentId tool]" ||
		!containsString(entries.Fields, "result") || !containsString(entries.Fields, "agent") {
		t.Fatalf("audit.entries: %+v %v", entries, ok)
	}
	if denied, ok := LookupLeaf("audit.denied"); !ok || denied.Kind != KindNumber || !denied.Integer {
		t.Fatalf("audit.denied: %+v %v", denied, ok)
	}
	if last, ok := LookupLeaf("audit.last"); !ok || last.Kind != KindText || !containsString(last.Operators, "stalls") {
		t.Fatalf("audit.last: %+v %v", last, ok)
	}
	pending, ok := LookupLeaf("brmcp.pending")
	if !ok || pending.Kind != KindList || fmt.Sprint(pending.Identity) != "[id]" || !containsString(pending.Fields, "botNick") {
		t.Fatalf("brmcp.pending: %+v %v", pending, ok)
	}
	if spend, ok := LookupLeaf("brmcp.spend"); !ok || fmt.Sprint(spend.Identity) != "[ts bot tool]" {
		t.Fatalf("brmcp.spend: %+v %v", spend, ok)
	}
	if enabled, ok := LookupLeaf("brmcp.enabled"); !ok || enabled.Kind != KindBool {
		t.Fatalf("brmcp.enabled: %+v %v", enabled, ok)
	}
	if count, ok := LookupLeaf("brmcp.pendingCount"); !ok || !count.Integer {
		t.Fatalf("brmcp.pendingCount: %+v %v", count, ok)
	}
	for _, path := range []string{"audit.entries", "audit.denied", "brmcp.pending", "brmcp.enabled"} {
		if _, ok := Resolve(&Snapshot{Reachable: true}, path); ok {
			t.Errorf("%s: no section must be no sample", path)
		}
	}
	snap := &Snapshot{Reachable: true,
		Audit: &Audit{Entries: []AuditEntry{{Time: "t1", AgentID: "a", Tool: "wallet_send", Result: "denied"}}, Count: 1, Denied: 1},
		BRMCP: &BRMCP{Enabled: true, Pending: []BRMCPPending{{ID: "p1", BotNick: "braibot"}}, PendingCount: 1},
	}
	if v, ok := Resolve(snap, "audit.entries"); !ok || len(v.Items) != 1 || v.Items[0].Fields["result"] != "denied" {
		t.Fatalf("audit.entries resolved to %+v %v", v, ok)
	}
	if v, ok := Resolve(snap, "brmcp.pending"); !ok || len(v.Items) != 1 || v.Items[0].Fields["botNick"] != "braibot" {
		t.Fatalf("brmcp.pending resolved to %+v %v", v, ok)
	}
	if v, ok := Resolve(snap, "brmcp.pendingCount"); !ok || v.Num != 1 {
		t.Fatalf("brmcp.pendingCount resolved to %+v %v", v, ok)
	}
}
