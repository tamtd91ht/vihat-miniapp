package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// The defect class this file exists to catch:
//
//  1. a photo route reachable without a session, or reading SOMEONE ELSE's photo by id;
//  2. a non-image stored because the client labelled it image/jpeg;
//  3. a photo stored with its EXIF (GPS of where it was taken), or the client's file name
//     (often a person's name or number) reaching a log line;
//  4. a refusal whose `code` drifts — the Mini App maps codes, not sentences.

const (
	reqOfA = "3f1c0c9e-0000-4000-8000-0000000000a1"
	reqOfB = "3f1c0c9e-0000-4000-8000-0000000000b1"
	// Rare strings so the leak checks cannot match by accident.
	exifSecret = "GPS-10.7769N-106.7009E-KHONG-DUOC-LUU"
	fileName   = "Nguyen-Van-A-0900000000.jpg"
)

type fakeReq struct {
	owner, kind string
	anonymised  bool
}

type fakePhotoStore struct {
	mu       sync.Mutex
	requests map[string]fakeReq
	photos   map[string][]store.PhotoMeta
	bytes    map[string][]byte
	failWith error
	calls    int
}

func newFakePhotoStore() *fakePhotoStore {
	return &fakePhotoStore{
		requests: map[string]fakeReq{
			reqOfA: {owner: nguoiA, kind: yeucau.LoaiTuVan},
			reqOfB: {owner: nguoiB, kind: yeucau.LoaiTuVan},
		},
		photos: map[string][]store.PhotoMeta{},
		bytes:  map[string][]byte{},
	}
}

// own mirrors the store's rule: not yours / absent / anonymised are ONE answer.
func (f *fakePhotoStore) own(userID, requestID string) (fakeReq, bool) {
	r, ok := f.requests[requestID]
	return r, ok && r.owner == userID && !r.anonymised
}

func (f *fakePhotoStore) AddRequestPhoto(_ context.Context, userID, requestID, ct string, data []byte) (store.PhotoMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failWith != nil {
		return store.PhotoMeta{}, f.failWith
	}
	r, ok := f.own(userID, requestID)
	if !ok {
		return store.PhotoMeta{}, store.ErrPhotoNotFound
	}
	if r.kind != yeucau.LoaiTuVan {
		return store.PhotoMeta{}, store.ErrPhotoNotAllowed
	}
	if len(f.photos[requestID]) >= photo.MaxPerRequest {
		return store.PhotoMeta{}, store.ErrPhotoLimit
	}
	n := len(f.photos[requestID]) + 1
	m := store.PhotoMeta{
		ID: "3f1c0c9e-0000-4000-8000-00000000f00" + string(rune('0'+n)), RequestID: requestID,
		Position: n, ContentType: ct, SizeBytes: len(data), CreatedAt: time.Unix(1760000000, 0),
	}
	f.photos[requestID] = append(f.photos[requestID], m)
	f.bytes[m.ID] = append([]byte(nil), data...)
	return m, nil
}

func (f *fakePhotoStore) ListRequestPhotos(_ context.Context, userID, requestID string) ([]store.PhotoMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.own(userID, requestID); !ok {
		return nil, store.ErrPhotoNotFound
	}
	return f.photos[requestID], nil
}

func (f *fakePhotoStore) ReadRequestPhoto(_ context.Context, userID, requestID, photoID string) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.own(userID, requestID); !ok {
		return "", nil, store.ErrPhotoNotFound
	}
	for _, m := range f.photos[requestID] {
		if m.ID == photoID {
			return m.ContentType, f.bytes[photoID], nil
		}
	}
	return "", nil, store.ErrPhotoNotFound
}

// ---------------------------------------------------------------------------

func photoServer(t *testing.T) (*Server, *fakePhotoStore, *bytes.Buffer) {
	t.Helper()
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.phienHopLe["tok-b"] = nguoiB
	s, log := dungServerYeuCau(t, kho, &tongDaiGia{})
	ps := newFakePhotoStore()
	s.WithRequestPhotos(ps)
	return s, ps, log
}

// jpegWithEXIF — a decodable JPEG carrying an APP1 Exif segment with exifSecret.
func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	raw := enc.Bytes()
	payload := "Exif\x00\x00" + exifSecret
	seg := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg...)
	return append(out, raw[2:]...)
}

type formPart struct {
	name, fileName, contentType string
	data                        []byte
}

func multipartBody(t *testing.T, parts ...formPart) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		cd := `form-data; name="` + p.name + `"`
		if p.fileName != "" {
			cd += `; filename="` + p.fileName + `"`
		}
		h.Set("Content-Disposition", cd)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(p.data)
	}
	_ = mw.Close()
	return &b, mw.FormDataContentType()
}

func callPhotos(t *testing.T, s *Server, method, path, bearer string, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	t.Helper()
	if body == nil {
		body = &bytes.Buffer{}
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = "203.0.113.9:51000"
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func upload(t *testing.T, s *Server, bearer, requestID string, parts ...formPart) *httptest.ResponseRecorder {
	t.Helper()
	b, ct := multipartBody(t, parts...)
	return callPhotos(t, s, http.MethodPost, "/api/v1/requests/"+requestID+"/photos", bearer, b, ct)
}

func photoPart(data []byte) formPart {
	// The label LIES (says PNG) on purpose: the server must go by the bytes.
	return formPart{name: "photo", fileName: fileName, contentType: "image/png", data: data}
}

func wantCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d — body %s", w.Code, status, w.Body.String())
	}
	var e photoErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body not JSON: %s", w.Body.String())
	}
	if e.Code != code || strings.TrimSpace(e.Message) == "" {
		t.Fatalf("code %q message %q, want code %q and a message", e.Code, e.Message, code)
	}
}

// ---------------------------------------------------------------------------

func TestPhotos_NoSessionIs401AndStoresNothing(t *testing.T) {
	s, ps, _ := photoServer(t)
	for _, bearer := range []string{"", "token-bia-ra"} {
		w := upload(t, s, bearer, reqOfA, photoPart(jpegWithEXIF(t)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("bearer %q: status %d, want 401", bearer, w.Code)
		}
		for _, path := range []string{"/photos", "/photos/x"} {
			if w := callPhotos(t, s, http.MethodGet, "/api/v1/requests/"+reqOfA+path, bearer, nil, ""); w.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s bearer %q: status %d, want 401", path, bearer, w.Code)
			}
		}
	}
	if ps.calls != 0 {
		t.Fatal("store reached without a session")
	}
}

func TestPhotos_UploadSniffsStripsAndAnswers201(t *testing.T) {
	s, ps, log := photoServer(t)
	w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201 — %s", w.Code, w.Body.String())
	}
	var out photoOut
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.RequestID != reqOfA || out.Position != 1 || out.ContentType != photo.TypeJPEG || out.PhotoID == "" || out.Size <= 0 || out.CreatedAt == "" {
		t.Fatalf("body %s", w.Body.String())
	}
	stored := ps.bytes[out.PhotoID]
	if bytes.Contains(stored, []byte(exifSecret)) {
		t.Fatal("EXIF stored — the GPS of where the photo was taken is now in the database")
	}
	if _, err := jpeg.Decode(bytes.NewReader(stored)); err != nil {
		t.Fatalf("stored JPEG does not decode: %v", err)
	}
	if out.Size != len(stored) {
		t.Fatalf("size %d, stored %d — size must describe the stored bytes", out.Size, len(stored))
	}
	for _, leak := range []string{fileName, "0900000000", exifSecret} {
		if strings.Contains(log.String(), leak) || strings.Contains(w.Body.String(), leak) {
			t.Fatalf("%q reached the log or the response", leak)
		}
	}
}

func TestPhotos_SomeoneElsesRequestIsIndistinguishableFromNone(t *testing.T) {
	s, ps, _ := photoServer(t)
	if w := upload(t, s, "tok-b", reqOfB, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
		t.Fatalf("B uploads own: %d", w.Code)
	}
	bPhoto := ps.photos[reqOfB][0].ID

	missing := "3f1c0c9e-0000-4000-8000-0000000000ff"
	cases := []struct{ name, method, path string }{
		{"upload to B's", http.MethodPost, "/api/v1/requests/" + reqOfB + "/photos"},
		{"list B's", http.MethodGet, "/api/v1/requests/" + reqOfB + "/photos"},
		{"read B's", http.MethodGet, "/api/v1/requests/" + reqOfB + "/photos/" + bPhoto},
		{"read B's photo via A's request", http.MethodGet, "/api/v1/requests/" + reqOfA + "/photos/" + bPhoto},
		{"list missing", http.MethodGet, "/api/v1/requests/" + missing + "/photos"},
		{"not a uuid", http.MethodGet, "/api/v1/requests/not-a-uuid/photos"},
	}
	var first string
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var w *httptest.ResponseRecorder
			if c.method == http.MethodPost {
				b, ct := multipartBody(t, photoPart(jpegWithEXIF(t)))
				w = callPhotos(t, s, c.method, c.path, "tok-a", b, ct)
			} else {
				w = callPhotos(t, s, c.method, c.path, "tok-a", nil, "")
			}
			wantCode(t, w, http.StatusNotFound, photoCodeNotFound)
			if first == "" {
				first = w.Body.String()
			} else if w.Body.String() != first {
				t.Fatalf("body differs from the other 404s — it tells a guesser something: %s", w.Body.String())
			}
		})
	}
	if len(ps.photos[reqOfB]) != 1 {
		t.Fatal("A added a photo to B's request")
	}
}

func TestPhotos_Refusals(t *testing.T) {
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	tooBig := append([]byte{0xff, 0xd8, 0xff}, make([]byte, photo.MaxBytes)...)
	truncated := jpegWithEXIF(t)[:40]

	for _, c := range []struct {
		name   string
		body   func(t *testing.T) (*bytes.Buffer, string)
		status int
		code   string
	}{
		{"json, not multipart", func(*testing.T) (*bytes.Buffer, string) {
			return bytes.NewBufferString(`{"photo":"x"}`), "application/json"
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"no photo part", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t)
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"empty photo", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t, photoPart(nil))
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"extra field rides along", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t, photoPart(jpegWithEXIF(t)), formPart{name: "phone", data: []byte("0900000000")})
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"two photos in one request", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t, photoPart(jpegWithEXIF(t)), photoPart(jpegWithEXIF(t)))
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"wrong field name", func(t *testing.T) (*bytes.Buffer, string) {
			p := photoPart(jpegWithEXIF(t))
			p.name = "file"
			return multipartBody(t, p)
		}, http.StatusBadRequest, photoCodeInvalidUpload},
		{"over 5 MB", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t, photoPart(tooBig))
		}, http.StatusRequestEntityTooLarge, photoCodeTooLarge},
		{"gif labelled jpeg", func(t *testing.T) (*bytes.Buffer, string) {
			p := photoPart(gif)
			p.contentType = "image/jpeg"
			return multipartBody(t, p)
		}, http.StatusUnsupportedMediaType, photoCodeUnsupportedType},
		{"truncated jpeg", func(t *testing.T) (*bytes.Buffer, string) {
			return multipartBody(t, photoPart(truncated))
		}, http.StatusUnprocessableEntity, photoCodeUnreadable},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, ps, _ := photoServer(t)
			b, ct := c.body(t)
			w := callPhotos(t, s, http.MethodPost, "/api/v1/requests/"+reqOfA+"/photos", "tok-a", b, ct)
			wantCode(t, w, c.status, c.code)
			if len(ps.photos[reqOfA]) != 0 {
				t.Fatal("a refused upload was stored")
			}
		})
	}
}

func TestPhotos_AtMostThreeThen409(t *testing.T) {
	s, _, _ := photoServer(t)
	for i := 1; i <= photo.MaxPerRequest; i++ {
		if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
			t.Fatalf("photo %d: status %d", i, w.Code)
		}
	}
	wantCode(t, upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))), http.StatusConflict, photoCodeLimit)
}

func TestPhotos_StoreErrorsMapToStableCodes(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrPhotoNotAllowed, http.StatusConflict, photoCodeNotAllowed},
		{store.ErrPhotoLimit, http.StatusConflict, photoCodeLimit},
		{store.ErrPhotoNotFound, http.StatusNotFound, photoCodeNotFound},
		{errors.New("ghi request_photo: connection reset"), http.StatusInternalServerError, photoCodeInternal},
	} {
		t.Run(c.code, func(t *testing.T) {
			s, ps, log := photoServer(t)
			ps.failWith = c.err
			w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t)))
			wantCode(t, w, c.status, c.code)
			if strings.Contains(w.Body.String(), "connection reset") {
				t.Fatal("internal error text returned to the client")
			}
			if strings.Contains(log.String(), fileName) {
				t.Fatal("file name logged")
			}
		})
	}
}

func TestPhotos_NonConsultRequestIs409(t *testing.T) {
	s, ps, _ := photoServer(t)
	ps.requests[reqOfA] = fakeReq{owner: nguoiA, kind: yeucau.LoaiHuyUuDaiSMS}
	wantCode(t, upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))), http.StatusConflict, photoCodeNotAllowed)
}

func TestPhotos_ListAndReadOwn(t *testing.T) {
	s, ps, _ := photoServer(t)
	if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusCreated {
		t.Fatalf("upload: %d", w.Code)
	}
	id := ps.photos[reqOfA][0].ID

	w := callPhotos(t, s, http.MethodGet, "/api/v1/requests/"+reqOfA+"/photos", "tok-a", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var list photoListOut
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].PhotoID != id {
		t.Fatalf("list body %s", w.Body.String())
	}

	w = callPhotos(t, s, http.MethodGet, "/api/v1/requests/"+reqOfA+"/photos/"+id, "tok-a", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), ps.bytes[id]) {
		t.Fatal("read returned other bytes than stored")
	}
	h := w.Header()
	if h.Get("Content-Type") != photo.TypeJPEG || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(h.Get("Cache-Control"), "no-store") || strings.Contains(h.Get("Content-Disposition"), fileName) {
		t.Fatalf("headers %v", h)
	}
}

func TestPhotos_EmptyListIsArray(t *testing.T) {
	s, _, _ := photoServer(t)
	w := callPhotos(t, s, http.MethodGet, "/api/v1/requests/"+reqOfA+"/photos", "tok-a", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestPhotos_BadPathAndMethod(t *testing.T) {
	s, _, _ := photoServer(t)
	for _, p := range []string{"/api/v1/requests/" + reqOfA, "/api/v1/requests/" + reqOfA + "/notes", "/api/v1/requests/" + reqOfA + "/photos/a/b"} {
		wantCode(t, callPhotos(t, s, http.MethodGet, p, "tok-a", nil, ""), http.StatusNotFound, photoCodeNotFound)
	}
	w := callPhotos(t, s, http.MethodDelete, "/api/v1/requests/"+reqOfA+"/photos", "tok-a", nil, "")
	wantCode(t, w, http.StatusMethodNotAllowed, photoCodeMethod)
	if w.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("Allow %q", w.Header().Get("Allow"))
	}
}

func TestPhotos_NotWiredIs503NotA404(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})
	wantCode(t, upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))), http.StatusServiceUnavailable, photoCodeUnavailable)
}

// The photo routes share the /api/v1/requests IP bucket — an upload loop cannot run beside
// the request limit.
func TestPhotos_ShareTheRequestIPBucket(t *testing.T) {
	s, _, _ := photoServer(t)
	for i := 0; i < SoLuotYeuCauToiDa; i++ {
		_ = callPhotos(t, s, http.MethodGet, "/api/v1/requests/"+reqOfA+"/photos", "tok-a", nil, "")
	}
	if w := upload(t, s, "tok-a", reqOfA, photoPart(jpegWithEXIF(t))); w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429 after %d calls", w.Code, SoLuotYeuCauToiDa)
	}
	if w := goiYeuCau(t, s, http.MethodGet, "tok-a", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("/requests status %d, want 429 — the buckets are not shared", w.Code)
	}
}

func TestRequests_ListCarriesPhotoCount(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.dsCuaToi[nguoiA] = []yeucau.TomTat{{Ma: reqOfA, Loai: yeucau.LoaiTuVan, TrangThai: "moi", TaoLuc: time.Now(), PhotoCount: 2}}
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})
	w := goiYeuCau(t, s, http.MethodGet, "tok-a", "")
	if !strings.Contains(w.Body.String(), `"photoCount":2`) {
		t.Fatalf("body %s", w.Body.String())
	}
}
