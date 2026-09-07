// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"strings"
	"testing"
)

func TestFoldMarkup(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain text untouched", "hello there", "hello there"},
		{"inline image with caption",
			"look at this --embed[alt=cat+on+the+sofa,type=image/png,data=iVBORw0KGgoAAAANSUhEUg]-- ok?",
			"look at this 🖼️ cat on the sofa ok?"},
		{"escaped caption",
			"--embed[alt=r%C3%A9sum%C3%A9%20shot,type=image/jpeg,data=AAAA]--",
			"🖼️ résumé shot"},
		{"stored image without caption",
			"--embed[type=image/png,localfilename=/home/u/.brclient/embeds/photo.png]--",
			"🖼️ photo.png"},
		{"image with neither caption nor name",
			"--embed[type=image/webp,data=AAAA]--",
			"🖼️ image"},
		{"shared pdf with a price",
			"--embed[download=0123abcd,filename=report.pdf,size=20480,cost=1000000]--",
			"📄 report.pdf (0.01 DCR)"},
		{"shared text file",
			"--embed[download=0123abcd,filename=notes.md,size=12]--",
			"📝 notes.md"},
		{"archive by extension",
			"--embed[download=0123abcd,filename=backup.tar.gz,size=12]--",
			"📦 backup.tar.gz"},
		{"video and audio by type",
			"--embed[type=video/mp4,data=AAAA]-- and --embed[type=audio/ogg,data=AAAA]--",
			"🎞️ video and 🎵 audio"},
		{"unknown binary",
			"--embed[download=0123abcd,filename=firmware.bin,size=12]--",
			"📎 firmware.bin"},
		{"quoted post",
			"--embed[type=quote,from=abcd,post=ef01]--",
			"💬 quoted post"},
		{"caption beats filename",
			"--embed[alt=the+invoice,download=0123abcd,filename=inv-42.pdf,size=12]--",
			"📄 the invoice"},
		{"brclientd download chip",
			"--download[nick=ana,name=holiday.jpg,size=204800]--",
			"🖼️ holiday.jpg"},
		{"invoice with lnpay scheme",
			"pay me: lnpay://lndcr10u1p3xyzacdefghjklmnpqrstuvwxyz0234567890acdefghjklmnpqrstuvwxyz023456789",
			"pay me: ⚡ invoice 0.00001 DCR"},
		{"bare mainnet invoice in millis",
			"lndcr2m1p3xyzacdefghjklmnpqrstuvwxyz0234567890acdefghjklmnpqrstuvwxyz023456789 thanks",
			"⚡ invoice 0.002 DCR thanks"},
		{"testnet invoice without amount",
			"lnpay://lntdcr1p3xyzacdefghjklmnpqrstuvwxyz0234567890acdefghjklmnpqrstuvwxyz023456789",
			"⚡ invoice, any amount (testnet)"},
		{"a word that merely starts with ln is not an invoice",
			"lndcr is the node, see lndcr1short",
			"lndcr is the node, see lndcr1short"},
		{"unterminated tag left alone",
			"--embed[alt=broken,type=image/png,data=AAAA",
			"--embed[alt=broken,type=image/png,data=AAAA"},
	}
	for _, c := range cases {
		if got := foldMarkup(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestFoldMarkupCapsLongText(t *testing.T) {
	long := strings.Repeat("é", messageTextCap+50)
	got := []rune(foldMarkup(long))
	if len(got) != messageTextCap+1 || got[len(got)-1] != '…' {
		t.Fatalf("got %d runes ending %q, want %d plus an ellipsis", len(got), got[len(got)-1], messageTextCap)
	}
	if got := foldMarkup(strings.Repeat("a", messageTextCap)); len(got) != messageTextCap {
		t.Fatalf("text at the cap must pass untouched, got %d", len(got))
	}
}

// The ring hands the tag through verbatim; the fold happens where the ring
// becomes messages, so the snapshot, the panel and the triggers all see the
// folded text.
func TestRingToMessagesCollapsesEmbeds(t *testing.T) {
	entries := []ringEntry{{Type: "pm"}}
	entries[0].Payload.FromNick = "ana"
	entries[0].Payload.Message = "see --embed[alt=receipt,type=image/png,data=AAAA]--"
	got := ringToMessages(entries, 20)
	if len(got) != 1 || got[0].Text != "see 🖼️ receipt" {
		t.Fatalf("got %+v", got)
	}
}
