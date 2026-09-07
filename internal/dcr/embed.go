// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Bison Relay carries attachments inside the message text. A picture pasted
// into a chat travels as
//
//	--embed[alt=cat on the sofa,type=image/png,data=<base64>]--
//
// a client that has stored the bytes rewrites the tag with localfilename= in
// their place, a shared file arrives as
//
//	--embed[download=<fid>,filename=report.pdf,size=...,cost=...]--
//
// and a quoted post as --embed[type=quote,from=...,post=...]--. The grammar is
// companyzero/bisonrelay internal/mdembeds: comma-separated key=value pairs,
// the alt text URL-escaped. brclientd adds one chip of its own for a file it
// has received, --download[nick=,name=,size=]--, and a Lightning invoice is a
// plain lnpay://lndcr... link that clients turn into a pay button.
//
// None of that is for a two-line preview or a trigger sample, and the inline
// image form can weigh hundreds of kilobytes. foldMarkup folds each tag into
// an icon and the one thing a person wants to read: the caption, else the
// filename, else what kind of thing it is; an invoice becomes its amount.
// Everything around a tag is kept, so "look at this --embed[...]--" reads
// "look at this 🖼️ cat on the sofa", and a text~= filter still matches the
// caption and the filename.
var embedTag = regexp.MustCompile(`--(?:embed|download)\[.*?\]--`)

// invoiceRE matches a BOLT11 invoice for Decred mainnet, testnet or simnet,
// with or without the lnpay:// scheme brclient wraps it in. The human-readable
// part carries the amount: digits and a multiplier before the "1" separator.
var invoiceRE = regexp.MustCompile(`\b(?:lnpay://)?ln([st]?)dcr(\d*)([munp]?)1[02-9ac-hj-np-z]{40,}`)

// messageTextCap bounds a message after its tags are folded. A tag that never
// closes is left as it is, and this keeps such a message, or a plain wall of
// text, from carrying its whole weight into every snapshot.
const messageTextCap = 2000

func foldMarkup(s string) string {
	if strings.Contains(s, "--embed[") || strings.Contains(s, "--download[") {
		s = embedTag.ReplaceAllStringFunc(s, embedPlaceholder)
	}
	if strings.Contains(s, "dcr") {
		s = invoiceRE.ReplaceAllStringFunc(s, invoicePlaceholder)
	}
	if r := []rune(s); len(r) > messageTextCap {
		s = string(r[:messageTextCap]) + "…"
	}
	return s
}

// embedPlaceholder turns one tag into "<icon> <label>".
func embedPlaceholder(tag string) string {
	inner := strings.TrimSuffix(tag[strings.Index(tag, "[")+1:], "]--")
	var alt, typ, filename, local string
	var cost uint64
	for _, kv := range strings.Split(inner, ",") {
		k, v, _ := strings.Cut(kv, "=")
		switch strings.TrimSpace(k) {
		case "alt":
			alt = unescapeAlt(v)
		case "type":
			typ = strings.ToLower(strings.TrimSpace(v))
		case "filename", "name":
			filename = v
		case "localfilename":
			local = v
		case "cost":
			cost, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	if typ == "quote" {
		return "💬 quoted post"
	}
	name := filename
	if name == "" && local != "" {
		name = path.Base(local)
	}
	icon, kind := embedIcon(typ, name)
	label := strings.TrimSpace(alt)
	if label == "" {
		label = name
	}
	if label == "" {
		label = kind
	}
	out := icon + " " + label
	if cost > 0 {
		// Cost is in atoms; a shared file that must be paid for says so.
		out += " (" + strconv.FormatFloat(float64(cost)/1e8, 'f', -1, 64) + " DCR)"
	}
	return out
}

// invoicePlaceholder turns an invoice into "⚡ invoice <amount> DCR". The
// multiplier follows BOLT11: m, u, n and p for milli, micro, nano and pico
// DCR. An invoice with no amount, one the payer fills in, says so.
func invoicePlaceholder(inv string) string {
	m := invoiceRE.FindStringSubmatch(inv)
	if m == nil {
		return inv
	}
	out := "⚡ invoice"
	if m[2] != "" {
		n, _ := strconv.ParseFloat(m[2], 64)
		scale := map[string]float64{"": 1, "m": 1e-3, "u": 1e-6, "n": 1e-9, "p": 1e-12}[m[3]]
		// Twelve decimals reach the pico multiplier; the trim drops the
		// zeros and the float noise a product like 10 * 1e-6 leaves behind.
		amount := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(n*scale, 'f', 12, 64), "0"), ".")
		out += " " + amount + " DCR"
	} else {
		out += ", any amount"
	}
	switch m[1] {
	case "t":
		out += " (testnet)"
	case "s":
		out += " (simnet)"
	}
	return out
}

// unescapeAlt undoes the URL escaping the sender applied. QueryUnescape also
// turns the "+" the bruig help text shows into a space; an alt that fails to
// decode is shown as sent rather than dropped.
func unescapeAlt(v string) string {
	if u, err := url.QueryUnescape(v); err == nil {
		return u
	}
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

// embedIcon picks an icon and a word for the kind of attachment, from the
// declared type first and the filename's extension when there is no type.
func embedIcon(typ, name string) (icon, kind string) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	switch {
	case strings.HasPrefix(typ, "image/"), isExt(ext, "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "avif", "heic"):
		return "🖼️", "image"
	case strings.HasPrefix(typ, "video/"), isExt(ext, "mp4", "mkv", "webm", "mov", "avi"):
		return "🎞️", "video"
	case strings.HasPrefix(typ, "audio/"), isExt(ext, "mp3", "ogg", "flac", "wav", "m4a", "opus"):
		return "🎵", "audio"
	case typ == "application/pdf", isExt(ext, "pdf", "doc", "docx", "odt", "rtf", "epub"):
		return "📄", "document"
	case strings.HasPrefix(typ, "text/"), isExt(ext, "txt", "md", "csv", "json", "log", "yaml", "yml", "toml"):
		return "📝", "text"
	case isExt(ext, "zip", "tar", "gz", "tgz", "xz", "bz2", "7z", "rar", "zst"):
		return "📦", "archive"
	}
	return "📎", "attachment"
}

func isExt(ext string, options ...string) bool {
	for _, o := range options {
		if ext == o {
			return true
		}
	}
	return false
}
