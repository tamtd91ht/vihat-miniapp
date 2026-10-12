package photo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// SIGNED DOWNLOAD LINKS — how a ViHAT specialist reads a photo (owner decision 12/10/2026,
// README NỢ #19 (a): "Webhook kèm link tải có hạn").
//
// The `request.photo_added` webhook carries a URL of the form
//
//	<PUBLIC_API_BASE_URL>/api/v1/request-photos/{photoId}?exp=<unix seconds>&sig=<hex>
//
// sig = HMAC-SHA256(REQUEST_PHOTO_URL_SECRET, "request-photo:v1\n" + photoId + "\n" + exp).
// The link IS the credential: whoever holds it reads that one photo until exp, and nothing
// else. Hence: a dedicated key (not the webhook key — the receiver holds that one and could
// then mint links for any photoId), a short life, never logged.

// LinkTTL — how long a download link works. ~24h: long enough for a specialist to open the
// event the next working morning, short enough that a forwarded chat message or a leaked
// receiver log stops opening the photo by the day after. Policy, not configuration.
const LinkTTL = 24 * time.Hour

// linkDomain separates this MAC from any other use of the same key, and versions it.
const linkDomain = "request-photo:v1\n"

func linkMAC(key []byte, photoID string, exp int64) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(linkDomain))
	m.Write([]byte(photoID))
	m.Write([]byte{'\n'})
	m.Write([]byte(strconv.FormatInt(exp, 10)))
	return m.Sum(nil)
}

// SignLink returns the hex signature for (photoID, exp).
func SignLink(key []byte, photoID string, exp int64) string {
	return hex.EncodeToString(linkMAC(key, photoID, exp))
}

// VerifyLink reports whether sig is valid for (photoID, exp) and exp is still in the future.
// Constant-time compare. One boolean on purpose: the caller answers expired, tampered and
// unknown with the same 404, so there is nothing to tell apart.
func VerifyLink(key []byte, photoID, expRaw, sig string, now time.Time) bool {
	if len(key) == 0 || photoID == "" || len(expRaw) == 0 || len(expRaw) > 19 {
		return false
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	// Re-format and compare: "+17…" or "0017…" parse to the same number but were not signed.
	if err != nil || strconv.FormatInt(exp, 10) != expRaw {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	if !hmac.Equal(got, linkMAC(key, photoID, exp)) {
		return false
	}
	return now.Unix() < exp
}
