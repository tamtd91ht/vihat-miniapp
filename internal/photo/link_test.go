package photo

import (
	"strconv"
	"testing"
	"time"
)

// The defect class: a download link that still opens after it expired, opens another
// photo, or opens with a signature nobody issued.

var linkKey = []byte("khoa-ky-link-anh-gia-lap-test-0032")

func TestLink_ValidUntilExpiry(t *testing.T) {
	now := time.Unix(1760000000, 0)
	exp := now.Add(LinkTTL).Unix()
	id := "3f1c0c9e-0000-4000-8000-00000000f001"
	sig := SignLink(linkKey, id, exp)
	e := strconv.FormatInt(exp, 10)

	if !VerifyLink(linkKey, id, e, sig, now) {
		t.Fatal("fresh link refused")
	}
	if VerifyLink(linkKey, id, e, sig, time.Unix(exp, 0)) {
		t.Fatal("link still opens AT its expiry")
	}
	if VerifyLink(linkKey, id, e, sig, now.Add(25*time.Hour)) {
		t.Fatal("expired link opens")
	}
}

func TestLink_TamperedIsRefused(t *testing.T) {
	now := time.Unix(1760000000, 0)
	exp := now.Add(time.Hour).Unix()
	id := "3f1c0c9e-0000-4000-8000-00000000f001"
	sig := SignLink(linkKey, id, exp)
	e := strconv.FormatInt(exp, 10)
	flipped := []byte(sig)
	if flipped[0] == 'a' {
		flipped[0] = 'b'
	} else {
		flipped[0] = 'a'
	}

	for _, c := range []struct{ name, id, exp, sig string }{
		{"other photo", "3f1c0c9e-0000-4000-8000-00000000f002", e, sig},
		{"later expiry", id, strconv.FormatInt(exp+86400, 10), sig},
		{"leading zero expiry", id, "0" + e, sig},
		{"plus-signed expiry", id, "+" + e, sig},
		{"flipped signature", id, e, string(flipped)},
		{"short signature", id, e, sig[:32]},
		{"not hex", id, e, "zz" + sig[2:]},
		{"empty signature", id, e, ""},
		{"empty expiry", id, "", sig},
		{"signed with another key", id, e, SignLink([]byte("khoa-khac-hoan-toan-gia-lap-00032"), id, exp)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if VerifyLink(linkKey, c.id, c.exp, c.sig, now) {
				t.Fatal("tampered link accepted")
			}
		})
	}
	if VerifyLink(nil, id, e, sig, now) {
		t.Fatal("empty key accepted a link")
	}
}
