// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// sections are the Snapshot fields a trigger may watch, by Go name. Each is a
// pointer to a section struct, and its json name is the first segment of every
// path beneath it: Node gives node.height, BR gives br.messages.
var sections = []string{"Node", "Staking", "Wallet", "Lightning", "Price", "Treasury", "BR"}

// meta are the pointer-to-struct fields of Snapshot that describe this helper
// rather than the dcrpulse it watches, so nothing can be armed on them. Every
// pointer-to-struct field must be in exactly one of these two lists; the test
// enforces it, so a new section cannot reach the build unclassified.
var meta = []string{"PlainHTTP", "ConnResult"}

// Kind is the shape of a leaf value, which decides what may be asked of it.
type Kind int

const (
	KindNumber Kind = iota
	KindText
	KindBool
	KindList
)

var kindNames = [...]string{KindNumber: "number", KindText: "text", KindBool: "bool", KindList: "list"}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// MarshalJSON writes the name rather than the ordinal: the catalogue is read by
// an agent, and "number" needs no legend.
func (k Kind) MarshalJSON() ([]byte, error) {
	if k < 0 || int(k) >= len(kindNames) {
		return nil, fmt.Errorf("unknown kind %d", int(k))
	}
	return json.Marshal(kindNames[k])
}

// Leaf is one thing that can be armed: a dotted path into a snapshot and what
// may be asked of the value found there.
type Leaf struct {
	Path    string `json:"path"`
	Kind    Kind   `json:"kind"`
	Integer bool   `json:"integer,omitempty"`
	// Section is the first path segment on its own, so a caller can say "not
	// granted" about the whole section rather than about one leaf.
	Section   string   `json:"section"`
	Operators []string `json:"operators"`
	// Fields and Identity describe the elements of a list. Fields are the
	// element's json names, which a where-filter may name; Identity is the
	// subset that says which element is which across two samples.
	Fields   []string `json:"fields,omitempty"`
	Identity []string `json:"identity,omitempty"`
}

// Item is one element of a list leaf, flattened to strings so a filter can
// compare against what an agent typed.
type Item struct {
	Fields map[string]string
	ID     []string
	// hash is set only on an item decoded from a stored value. Such an item has
	// no fields and no identity, just the fingerprint the identity produced, and
	// that is all a comparison against a live sample needs.
	hash string
}

// key is what a list item is known by across samples.
func (it Item) key() string {
	if it.hash != "" {
		return it.hash
	}
	return Fingerprint(it.ID)
}

// Value is one sample of a leaf. Only the field for its Kind is meaningful.
type Value struct {
	Kind  Kind
	Num   float64
	Text  string
	Bool  bool
	Items []Item
}

// operatorsFor is what a leaf of each kind can be watched for. A list of
// scalars has no identity to tell one element from another, so it can only be
// counted.
func operatorsFor(k Kind, elements bool) []string {
	switch k {
	case KindNumber:
		return []string{"crosses", "changes", "stalls"}
	case KindText, KindBool:
		return []string{"becomes", "stalls"}
	case KindList:
		if elements {
			return []string{"appears", "disappears", "count"}
		}
		return []string{"count"}
	}
	return nil
}

var snapshotType = reflect.TypeOf(Snapshot{})

var catalogue struct {
	once   sync.Once
	leaves []Leaf
	byPath map[string]Leaf
}

// Catalogue lists every leaf that can be armed, sorted by path. The walk is
// over the Go types rather than over a marshalled sample, so the list is the
// same whatever the grant happens to cover today.
func Catalogue() []Leaf {
	catalogue.once.Do(buildCatalogue)
	return append([]Leaf(nil), catalogue.leaves...)
}

// LookupLeaf finds one leaf by path.
func LookupLeaf(path string) (Leaf, bool) {
	catalogue.once.Do(buildCatalogue)
	l, ok := catalogue.byPath[path]
	return l, ok
}

func buildCatalogue() {
	var leaves []Leaf
	for _, name := range sections {
		f, ok := snapshotType.FieldByName(name)
		if !ok || f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.Struct {
			// The section list is a static table over a static type, so this
			// is a mistake in this file, not a condition to handle.
			panic("dcr: section " + name + " is not a pointer-to-struct field of Snapshot")
		}
		seg := jsonName(f)
		walk(f.Type.Elem(), seg, seg, &leaves)
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].Path < leaves[j].Path })
	byPath := make(map[string]Leaf, len(leaves))
	for _, l := range leaves {
		byPath[l.Path] = l
	}
	catalogue.leaves, catalogue.byPath = leaves, byPath
}

// walk appends a leaf for every scalar and list reachable from t. A value
// struct is descended into, so Staking.Own yields staking.own.live; a slice is
// a leaf in itself, because its elements are addressed by identity, not by
// position, and a path cannot name one.
func walk(t reflect.Type, prefix, section string, out *[]Leaf) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		path := prefix + "." + name
		switch f.Type.Kind() {
		case reflect.Struct:
			walk(f.Type, path, section, out)
		case reflect.Slice:
			leaf := Leaf{Path: path, Kind: KindList, Section: section}
			elements := f.Type.Elem().Kind() == reflect.Struct
			if elements {
				leaf.Fields, leaf.Identity = elementShape(f.Type.Elem())
			}
			leaf.Operators = operatorsFor(KindList, elements)
			*out = append(*out, leaf)
		default:
			k, integer, ok := scalarKind(f.Type.Kind())
			if !ok {
				continue
			}
			*out = append(*out, Leaf{
				Path: path, Kind: k, Integer: integer, Section: section,
				Operators: operatorsFor(k, false),
			})
		}
	}
}

// elementShape reads a list element type: every json name, and the subset that
// identifies an element. Identity is what the type tags trig:"id"; a type that
// tags nothing falls back to its strings and integers, since a float is a
// measurement and a bool is a state, and neither says which element this is.
func elementShape(t reflect.Type) (fields, identity []string) {
	var natural []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		fields = append(fields, name)
		if f.Tag.Get("trig") == "id" {
			identity = append(identity, name)
		}
		if k, integer, ok := scalarKind(f.Type.Kind()); ok && (k == KindText || integer) {
			natural = append(natural, name)
		}
	}
	if len(identity) == 0 {
		identity = natural
	}
	return fields, identity
}

// scalarKind maps a Go kind to a leaf kind. Anything else (a map, an
// interface, a pointer) has no comparison an operator could make, and Snapshot
// carries none today.
func scalarKind(k reflect.Kind) (kind Kind, integer, ok bool) {
	switch k {
	case reflect.Float32, reflect.Float64:
		return KindNumber, false, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return KindNumber, true, true
	case reflect.String:
		return KindText, false, true
	case reflect.Bool:
		return KindBool, false, true
	}
	return 0, false, false
}

// jsonName is the name encoding/json would write for a field, or "" for one it
// would not write at all. Paths use these rather than Go names so an agent can
// read a path straight off the NDJSON it already sees.
func jsonName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	switch name {
	case "-":
		return ""
	case "":
		return f.Name
	}
	return name
}

// fieldByJSON finds the field of a struct value by its json name.
func fieldByJSON(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if jsonName(t.Field(i)) == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// Resolve reads one leaf out of a snapshot. ok is false when there is no
// sample to speak of: no snapshot, an unreachable dcrpulse, a section the
// grant did not cover, or a path that is not a leaf.
//
// This walks the struct rather than marshalling and reading the JSON back
// because omitempty erases the distinction that matters here. A price whose
// change is 0 and a lightning section with no channels both vanish from the
// JSON, and would read exactly like a section that was never fetched. In the
// struct they are a zero and an empty list under a non-nil pointer, which is a
// sample, not the absence of one.
func Resolve(snap *Snapshot, path string) (Value, bool) {
	if snap == nil || !snap.Reachable {
		return Value{}, false
	}
	leaf, ok := LookupLeaf(path)
	if !ok {
		return Value{}, false
	}
	segs := strings.Split(path, ".")
	v := fieldByJSON(reflect.ValueOf(snap).Elem(), segs[0])
	if !v.IsValid() || v.IsNil() {
		return Value{}, false
	}
	v = v.Elem()
	for _, seg := range segs[1:] {
		if v = fieldByJSON(v, seg); !v.IsValid() {
			return Value{}, false
		}
	}
	return valueOf(v, leaf), true
}

func valueOf(v reflect.Value, leaf Leaf) Value {
	switch leaf.Kind {
	case KindNumber:
		return Value{Kind: KindNumber, Num: number(v)}
	case KindText:
		return Value{Kind: KindText, Text: v.String()}
	case KindBool:
		return Value{Kind: KindBool, Bool: v.Bool()}
	}
	// A nil slice is an empty list, not a missing one; see Resolve.
	items := make([]Item, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		items = append(items, itemOf(v.Index(i), leaf.Identity))
	}
	return Value{Kind: KindList, Items: items}
}

// itemOf flattens one list element. A scalar element is its own identity, so a
// list of numbers still yields distinct items even though nothing can be armed
// on them but the count.
func itemOf(e reflect.Value, identity []string) Item {
	if e.Kind() != reflect.Struct {
		return Item{ID: []string{formatScalar(e)}}
	}
	t := e.Type()
	fields := make(map[string]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name := jsonName(t.Field(i)); name != "" {
			fields[name] = formatScalar(e.Field(i))
		}
	}
	id := make([]string, 0, len(identity))
	for _, name := range identity {
		id = append(id, fields[name])
	}
	return Item{Fields: fields, ID: id}
}

func number(v reflect.Value) float64 {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	}
	return float64(v.Int())
}

// formatScalar renders a value the way an agent would type it: 16 rather than
// 16.000000, so a where-filter written from the panel matches.
func formatScalar(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.String:
		return v.String()
	}
	return fmt.Sprint(v.Interface())
}

// Fingerprint names a list element by its identity, short enough to store in
// the trigger file and to read in a log line, long enough that two channels or
// two messages do not collide by accident.
func Fingerprint(id []string) string {
	sum := sha256.Sum256([]byte(strings.Join(id, "\x00")))
	return hex.EncodeToString(sum[:])[:24]
}

// keys is the fingerprint set of a list value, sorted so two samples of the
// same list marshal identically.
func (v Value) keys() []string {
	keys := make([]string, 0, len(v.Items))
	for _, it := range v.Items {
		keys = append(keys, it.key())
	}
	sort.Strings(keys)
	return keys
}

// equalValue reports whether two samples of one leaf say the same thing. Lists
// compare as sets of identities: order is presentation, and a channel list
// re-sorted by capacity has not changed.
func equalValue(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindNumber:
		return a.Num == b.Num
	case KindText:
		return a.Text == b.Text
	case KindBool:
		return a.Bool == b.Bool
	}
	seen := make(map[string]bool, len(a.Items))
	for _, it := range a.Items {
		seen[it.key()] = true
	}
	other := make(map[string]bool, len(b.Items))
	for _, it := range b.Items {
		other[it.key()] = true
	}
	if len(seen) != len(other) {
		return false
	}
	for k := range seen {
		if !other[k] {
			return false
		}
	}
	return true
}

// MarshalJSON is the stored form of a sample. A list is kept as its
// fingerprints rather than its fields: the trigger file then holds nothing a
// message said, only enough to tell whether the same message is still there.
func (v Value) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case KindNumber:
		return json.Marshal(v.Num)
	case KindText:
		return json.Marshal(v.Text)
	case KindBool:
		return json.Marshal(v.Bool)
	case KindList:
		return json.Marshal(v.keys())
	}
	return nil, fmt.Errorf("unknown value kind %d", int(v.Kind))
}

// decodeValue reads a stored sample back for the given kind. ok is false for
// an empty or null raw value and for one of the wrong shape, either of which
// means the stored state is not usable and the evaluator should start over.
func decodeValue(raw json.RawMessage, k Kind) (Value, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return Value{}, false
	}
	switch k {
	case KindNumber:
		var n float64
		if json.Unmarshal(raw, &n) != nil {
			return Value{}, false
		}
		return Value{Kind: KindNumber, Num: n}, true
	case KindText:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return Value{}, false
		}
		return Value{Kind: KindText, Text: s}, true
	case KindBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return Value{}, false
		}
		return Value{Kind: KindBool, Bool: b}, true
	case KindList:
		var keys []string
		if json.Unmarshal(raw, &keys) != nil {
			return Value{}, false
		}
		items := make([]Item, 0, len(keys))
		for _, key := range keys {
			items = append(items, Item{hash: key})
		}
		return Value{Kind: KindList, Items: items}, true
	}
	return Value{}, false
}
