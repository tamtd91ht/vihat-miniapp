package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/webhook"
)

// The defect class: a specialist's link that keeps opening after it expired, opens with a
// forged or edited signature, opens a photo the citizen had erased — or an event that is
// lost, sent twice per photo, carries the citizen's phone, or puts the signed URL in a log.

const (
	linkKeyFake = "khoa-ky-link-anh-gia-lap-test-0032"
	// No path prefix here: a prefix in PUBLIC_API_BASE_URL is the ingress's to strip.
	apiBaseFake = "https://api.vihat.test"
)

type photoReceiver struct {
	mu      sync.Mutex
	bodies  [][]byte
	sigs    []string
	idemKey []string
}

func newPhotoReceiver(t *testing.T) (*httptest.Server, *photoReceiver) {
	t.Helper()
	pr := &photoReceiver{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		pr.mu.Lock()
		pr.bodies = append(pr.bodies, b)
		pr.sigs = append(pr.sigs, r.Header.Get(webhook.HeaderChuKy))
		pr.idemKey = append(pr.idemKey, r.Header.Get(webhook.HeaderIdempotency))
		pr.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, pr
}

// linkServer — the full real path: handler → yeucau (background) → webhook client → TLS
// receiver. The clock is pinned so expiry is testable.
func linkServer(t *testing.T) (*Server, *fakePhotoStore, *photoReceiver, *dongBoGhi, func()) {
	t.Helper()
	srv, pr := newPhotoReceiver(t)
	bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia), webhook.VoiHTTPClient(srv.Client()))
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, dv, log := dungServerBaoYeuCau(t, kho, bao)
	ps := newFakePhotoStore()
	s.WithRequestPhotos(ps).WithPhotoLinks(apiBaseFake, secret.Secret(linkKeyFake))
	s.now = func() time.Time { return time.Unix(1760000000, 0) }
	return s, ps, pr, log, func() { choNen(t, dv) }
}

type photoEventBody struct {
	Event, RequestID, PhotoID, ContentType, CreatedAt, DownloadURL, ExpiresAt string
	Position, Size                                                            int
}

func download(t *testing.T, s *Server, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	r.RemoteAddr = "198.51.100.20:44000" // TEST-NET-2, the receiver
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestPhotoEvent_OncePerStoredPhotoSignedAndWithoutPersonalData(t *testing.T) {
	s, ps, pr, log, wait := linkServer(t)

	for i := 0; i < 2; i++ {
		if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
			t.Fatalf("upload %d: %d", i, w.Code)
		}
	}
	// Refused uploads send nothing.
	_ = upload(t, s, "tok-a", reqOfA, photoPart([]byte("GIF89a....")))
	_ = upload(t, s, "tok-a", reqOfB, photoPart(jpegWithEXIF(t)))
	wait()

	if len(pr.bodies) != 2 {
		t.Fatalf("receiver got %d events, want exactly one per stored photo (2)", len(pr.bodies))
	}
	seen := map[string]bool{}
	for i, body := range pr.bodies {
		if pr.sigs[i] != webhook.KyThan([]byte(khoaWebhookGia), body) {
			t.Fatalf("event %d: signature does not match the raw body", i)
		}
		var ev photoEventBody
		if err := json.Unmarshal(body, &ev); err != nil {
			t.Fatal(err)
		}
		// Background sends may arrive in any order: match by photoId, and each photo once.
		var want store.PhotoMeta
		for _, m := range ps.photos[reqOfA] {
			if m.ID == ev.PhotoID {
				want = m
			}
		}
		if seen[want.ID] {
			t.Fatalf("photo %s reported twice", want.ID)
		}
		seen[want.ID] = true
		if ev.Event != webhook.TenSuKienAnh || ev.RequestID != reqOfA || want.ID == "" ||
			ev.Position != want.Position || ev.ContentType != photo.TypeJPEG || ev.Size != want.SizeBytes ||
			ev.CreatedAt == "" || ev.ExpiresAt != "2025-10-10T08:53:20Z" {
			t.Fatalf("event %d: %s", i, body)
		}
		if pr.idemKey[i] != want.ID {
			t.Fatalf("Idempotency-Key %q, want photoId %q", pr.idemKey[i], want.ID)
		}
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		for _, forbidden := range []string{"phone", "displayName", "note", "name"} {
			if _, ok := raw[forbidden]; ok {
				t.Fatalf("photo event carries %q", forbidden)
			}
		}
		if !strings.HasPrefix(ev.DownloadURL, apiBaseFake+"/api/v1/request-photos/"+want.ID+"?") {
			t.Fatalf("downloadUrl %q", ev.DownloadURL)
		}
		// The link opens exactly that photo, without a bearer.
		w := download(t, s, ev.DownloadURL)
		if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), ps.bytes[want.ID]) {
			t.Fatalf("download: %d", w.Code)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("download headers %v", w.Header())
		}
		// The signature never reaches a log line.
		sig := ev.DownloadURL[strings.Index(ev.DownloadURL, "sig=")+4:]
		if strings.Contains(log.String(), sig) || strings.Contains(log.String(), "request-photos/") {
			t.Fatal("the signed link reached the log")
		}
	}
	if strings.Contains(log.String(), soGia) {
		t.Fatal("phone in the log")
	}
}

func TestPhotoLink_ExpiredTamperedUnknownAllSame404(t *testing.T) {
	s, ps, _, _, wait := linkServer(t)
	if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
		t.Fatalf("upload: %d", w.Code)
	}
	wait()
	id := ps.photos[reqOfA][0].ID
	now := s.now()
	link := func(id string, exp int64, sig string) string {
		return apiBaseFake + "/api/v1/request-photos/" + id + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig
	}
	key := []byte(linkKeyFake)
	exp := now.Add(time.Hour).Unix()
	good := photo.SignLink(key, id, exp)
	if w := download(t, s, link(id, exp, good)); w.Code != http.StatusOK {
		t.Fatalf("valid link: %d", w.Code)
	}

	past := now.Add(-time.Second).Unix()
	other := "3f1c0c9e-0000-4000-8000-00000000f009"
	cases := map[string]string{
		"expired":               link(id, past, photo.SignLink(key, id, past)),
		"exp moved later":       link(id, exp+86400, good),
		"signature edited":      link(id, exp, "0"+good[1:]),
		"no signature":          apiBaseFake + "/api/v1/request-photos/" + id + "?exp=" + strconv.FormatInt(exp, 10),
		"signed by webhook key": link(id, exp, photo.SignLink([]byte(khoaWebhookGia), id, exp)),
		"valid sig, unknown id": link(other, exp, photo.SignLink(key, other, exp)),
		"other id, this sig":    link(other, exp, good),
		"extra path segment":    apiBaseFake + "/api/v1/request-photos/" + id + "/x?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + good,
	}
	var first string
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			w := download(t, s, u)
			wantCode(t, w, http.StatusNotFound, photoCodeNotFound)
			if first == "" {
				first = w.Body.String()
			} else if w.Body.String() != first {
				t.Fatalf("404 body differs — it tells which part was right: %s", w.Body.String())
			}
		})
	}
	// Later in time, the once-good link stops.
	s.now = func() time.Time { return time.Unix(exp, 0) }
	wantCode(t, download(t, s, link(id, exp, good)), http.StatusNotFound, photoCodeNotFound)
}

func TestPhotoLink_AnonymisedIs404(t *testing.T) {
	s, ps, pr, _, wait := linkServer(t)
	if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
		t.Fatalf("upload: %d", w.Code)
	}
	wait()
	var ev photoEventBody
	_ = json.Unmarshal(pr.bodies[0], &ev)

	ps.mu.Lock()
	r := ps.requests[reqOfA]
	r.anonymised = true
	ps.requests[reqOfA] = r
	ps.mu.Unlock()

	wantCode(t, download(t, s, ev.DownloadURL), http.StatusNotFound, photoCodeNotFound)
}

func TestPhotoLink_OffMeansNoEventAndEvery404(t *testing.T) {
	srv, pr := newPhotoReceiver(t)
	bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia), webhook.VoiHTTPClient(srv.Client()))
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, dv, _ := dungServerBaoYeuCau(t, kho, bao)
	ps := newFakePhotoStore()
	s.WithRequestPhotos(ps) // no WithPhotoLinks
	if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
		t.Fatalf("upload must still work with links off: %d", w.Code)
	}
	choNen(t, dv)
	if len(pr.bodies) != 0 {
		t.Fatalf("event sent with links off: %d", len(pr.bodies))
	}
	id := ps.photos[reqOfA][0].ID
	exp := time.Now().Add(time.Hour).Unix()
	// Even a link signed with an empty key opens nothing.
	u := apiBaseFake + "/api/v1/request-photos/" + id + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + photo.SignLink(nil, id, exp)
	wantCode(t, download(t, s, u), http.StatusNotFound, photoCodeNotFound)
}

func TestPhotoLink_OwnRateLimitBucket(t *testing.T) {
	s, _, _, _, _ := linkServer(t)
	u := apiBaseFake + "/api/v1/request-photos/x?exp=1&sig=00"
	for i := 0; i < SoLuotYeuCauToiDa; i++ {
		_ = download(t, s, u)
	}
	wantCode(t, download(t, s, u), http.StatusTooManyRequests, photoCodeRateLimited)
}
