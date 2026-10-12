package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// HOW A VIHAT SPECIALIST READS A PHOTO (owner decision 12/10/2026, README NỢ #19 (a):
// "Webhook kèm link tải có hạn").
//
//	after each 201 of POST /api/v1/requests/{id}/photos
//	  → webhook `request.photo_added` (same destination, key and retry as request.created)
//	    carrying downloadUrl = <PUBLIC_API_BASE_URL>/api/v1/request-photos/{photoId}?exp=&sig=
//	GET /api/v1/request-photos/{photoId}?exp=<unix>&sig=<hex>   PUBLIC, the link is the credential
//
// ⚠ The query carries the signature. Nothing here logs r.URL or the link; keep it that way.

const requestPhotoDownloadPrefix = "/api/v1/request-photos/"

const msgPhotoRateLimited = "Bạn đã thao tác quá nhiều lần. Vui lòng chờ vài phút rồi thử lại."

// WithPhotoLinks turns on the signed links and with them the photo event. baseURL is
// config.PublicAPIBaseURL (no trailing slash); empty = off: photos are still stored, no
// event is sent, and every link answers 404.
func (s *Server) WithPhotoLinks(baseURL string, key secret.Secret) *Server {
	s.photoLinkBase, s.photoLinkKey = baseURL, key
	return s
}

// photoEvent hands the event of a photo just stored to the background sender.
func (s *Server) photoEvent(r *http.Request, m store.PhotoMeta) {
	if s.photoLinkBase == "" || s.photoLinkKey.Lo() == "" || s.yeuCau == nil || !s.yeuCau.CoBaoAnh() {
		return
	}
	exp := s.now().Add(photo.LinkTTL).Truncate(time.Second)
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp.Unix(), 10))
	q.Set("sig", photo.SignLink([]byte(s.photoLinkKey.Lo()), m.ID, exp.Unix()))
	s.yeuCau.ReportPhotoAdded(r.Context(), yeucau.SuKienAnh{
		RequestID:   m.RequestID,
		PhotoID:     m.ID,
		Position:    m.Position,
		ContentType: m.ContentType,
		Size:        m.SizeBytes,
		CreatedAt:   m.CreatedAt,
		DownloadURL: s.photoLinkBase + requestPhotoDownloadPrefix + url.PathEscape(m.ID) + "?" + q.Encode(),
		ExpiresAt:   exp,
	})
}

// downloadRequestPhoto — GET /api/v1/request-photos/{photoId}?exp=&sig=.
//
// What guards it: the HMAC (constant-time), the expiry, its OWN IP bucket of the same
// GioiHanIP family and size as /api/v1/requests (own, so a receiver fetching photos and
// citizens behind the same NAT do not starve each other), and that an anonymised photo has
// no bytes left (store.ReadPhotoForDownload).
//
// EVERY refusal — links off, bad path, expired, tampered, unknown id, anonymised — is the
// SAME 404 body: telling them apart tells a guesser which part was right.
func (s *Server) downloadRequestPhoto(w http.ResponseWriter, r *http.Request) {
	if choPhep, _ := s.downloadLimit.Cho(khoaGioiHan(ipCuaKhach(r))); !choPhep {
		s.photoError(w, http.StatusTooManyRequests, photoCodeRateLimited, msgPhotoRateLimited)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.photoError(w, http.StatusMethodNotAllowed, photoCodeMethod, msgPhotoWrongMethod)
		return
	}
	photoID := strings.TrimPrefix(r.URL.Path, requestPhotoDownloadPrefix)
	q := r.URL.Query()
	if s.photos == nil || photoID == "" || strings.Contains(photoID, "/") ||
		!photo.VerifyLink([]byte(s.photoLinkKey.Lo()), photoID, q.Get("exp"), q.Get("sig"), s.now()) {
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	}
	ct, data, err := s.photos.ReadPhotoForDownload(r.Context(), photoID)
	if errors.Is(err, store.ErrPhotoNotFound) {
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	}
	if err != nil {
		s.log.Error("không đọc được ảnh theo link", "ma_anh", photoID, "loi", err.Error())
		s.photoError(w, http.StatusInternalServerError, photoCodeInternal, loiHeThong)
		return
	}
	s.writePhoto(w, ct, data)
}
