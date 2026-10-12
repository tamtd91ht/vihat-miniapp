package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/store"
)

// PHOTOS ON A "TƯ VẤN VÀ BÁO GIÁ" REQUEST (owner decision 12/10/2026, migrations/0005).
//
//	POST /api/v1/requests/{requestId}/photos            multipart/form-data, ONE part `photo`
//	GET  /api/v1/requests/{requestId}/photos            metadata of the request's photos
//	GET  /api/v1/requests/{requestId}/photos/{photoId}  the bytes
//
// All three sit behind `gacYeuCau` — the SAME IP bucket (20 / 5 min) and the same bearer
// check as /api/v1/requests. One photo per POST, after the request exists: the Mini App
// already sends its scene photos that way (one multipart request per photo, by code), and a
// photo that fails never fails the request it belongs to.
//
// ⚠ THE OWNER IS THE SESSION. requestId and photoId come from the path, but every store call
// also takes the session's user and every SQL statement requires it. Someone else's id —
// guessed, leaked, or real — answers exactly what a non-existent id answers: 404 not_found.
//
// ⚠ NOTHING ABOUT THE PHOTO IS LOGGED but its ids, type and size. Not the client's file name
// (a phone names files after people and numbers), never a byte.
//
// Errors carry a STABLE `code` the client maps, plus the Vietnamese `message` this server
// writes everywhere else. 401 and 429 come from the shared wrappers and carry `message` only:
// their status code is the stable part.

// RequestPhotoStore — the slice of the store these routes need. Implemented by *store.Kho.
type RequestPhotoStore interface {
	AddRequestPhoto(ctx context.Context, userID, requestID, contentType string, data []byte) (store.PhotoMeta, error)
	ListRequestPhotos(ctx context.Context, userID, requestID string) ([]store.PhotoMeta, error)
	ReadRequestPhoto(ctx context.Context, userID, requestID, photoID string) (string, []byte, error)
}

// WithRequestPhotos wires the photo routes. Separate from VoiYeuCau for the same reason
// VoiYeuCau is separate from Moi: existing tests build the server without it. Not wired =
// the routes still exist and answer 503, never 404 (see the note on Handler).
func (s *Server) WithRequestPhotos(p RequestPhotoStore) *Server {
	s.photos = p
	return s
}

const requestPhotosPrefix = "/api/v1/requests/"

// photoBodyOverhead — room for the multipart boundary and part headers on top of the
// photo itself. The photo cap is checked separately, on the part, so the 413 says the
// right thing.
const photoBodyOverhead = 64 << 10

// Stable error codes — the client's contract. Never rename one; add a new one.
const (
	photoCodeInvalidUpload   = "invalid_upload"
	photoCodeTooLarge        = "file_too_large"
	photoCodeUnsupportedType = "unsupported_photo_type"
	photoCodeUnreadable      = "photo_unreadable"
	photoCodeNotFound        = "not_found"
	photoCodeNotAllowed      = "photo_not_allowed"
	photoCodeLimit           = "photo_limit"
	photoCodeMethod          = "method_not_allowed"
	photoCodeUnavailable     = "photo_unavailable"
	photoCodeInternal        = "internal_error"
)

const (
	msgPhotoInvalid     = "Không đọc được ảnh gửi lên. Vui lòng chọn lại ảnh và thử lại."
	msgPhotoTooLarge    = "Ảnh quá lớn. Mỗi ảnh tối đa 5 MB — vui lòng chọn ảnh khác hoặc chụp lại."
	msgPhotoType        = "Chỉ nhận ảnh JPEG, PNG hoặc WebP. Vui lòng chọn ảnh khác."
	msgPhotoUnreadable  = "Ảnh bị hỏng hoặc không đọc được. Vui lòng chọn ảnh khác."
	msgPhotoNotFound    = "Không tìm thấy yêu cầu này. Vui lòng mở lại danh sách Yêu cầu của tôi."
	msgPhotoNotAllowed  = "Yêu cầu này không nhận ảnh đính kèm."
	msgPhotoLimit       = "Mỗi yêu cầu đính kèm tối đa 3 ảnh."
	msgPhotoWrongMethod = "Yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử lại."
)

type photoErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type photoOut struct {
	PhotoID     string `json:"photoId"`
	RequestID   string `json:"requestId"`
	Position    int    `json:"position"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	CreatedAt   string `json:"createdAt"`
}

type photoListOut struct {
	Items []photoOut `json:"items"`
}

func (s *Server) photoError(w http.ResponseWriter, status int, code, msg string) {
	s.traJSON(w, status, photoErrorBody{Code: code, Message: msg})
}

func toPhotoOut(m store.PhotoMeta) photoOut {
	return photoOut{
		PhotoID:     m.ID,
		RequestID:   m.RequestID,
		Position:    m.Position,
		ContentType: m.ContentType,
		Size:        m.SizeBytes,
		CreatedAt:   m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// requestSubtree routes /api/v1/requests/{id}/photos[/{photoId}]. Runs inside gacYeuCau.
func (s *Server) requestSubtree(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, requestPhotosPrefix), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] != "photos" || (len(parts) == 3 && parts[2] == "") {
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	}
	if s.photos == nil {
		s.log.Error("tuyến ảnh yêu cầu chưa được lắp ráp — thiếu WithRequestPhotos ở cmd/server")
		s.photoError(w, http.StatusServiceUnavailable, photoCodeUnavailable, loiChuaLapRap)
		return
	}
	userID := nguoiDungTu(r.Context())
	if userID == "" {
		// Unreachable when wrapped correctly; fail closed rather than run with no owner.
		s.traLoi(w, http.StatusUnauthorized, loiChuaDangNhap)
		return
	}
	requestID := parts[0]

	switch {
	case len(parts) == 2 && r.Method == http.MethodPost:
		s.uploadRequestPhoto(w, r, userID, requestID)
	case len(parts) == 2 && r.Method == http.MethodGet:
		s.listRequestPhotos(w, r, userID, requestID)
	case len(parts) == 3 && r.Method == http.MethodGet:
		s.readRequestPhoto(w, r, userID, requestID, parts[2])
	default:
		if len(parts) == 2 {
			w.Header().Set("Allow", "GET, POST")
		} else {
			w.Header().Set("Allow", "GET")
		}
		s.photoError(w, http.StatusMethodNotAllowed, photoCodeMethod, msgPhotoWrongMethod)
	}
}

// uploadRequestPhoto — POST. Order: shape → size → type (from bytes) → strip metadata →
// ownership + kind + limit (one transaction in the store).
func (s *Server) uploadRequestPhoto(w http.ResponseWriter, r *http.Request, userID, requestID string) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		s.photoError(w, http.StatusBadRequest, photoCodeInvalidUpload, msgPhotoInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, photo.MaxBytes+photoBodyOverhead)
	mr := multipart.NewReader(r.Body, params["boundary"])

	var data []byte
	seen := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.uploadReadError(w, err)
			return
		}
		// Exactly one part, named `photo`. Any other part — or a second photo — is a client
		// that misunderstands the contract, and an extra field is how something about the
		// person rides along unseen.
		if part.FormName() != "photo" || seen {
			_ = part.Close()
			s.photoError(w, http.StatusBadRequest, photoCodeInvalidUpload, msgPhotoInvalid)
			return
		}
		seen = true
		data, err = io.ReadAll(io.LimitReader(part, photo.MaxBytes+1))
		_ = part.Close()
		if err != nil {
			s.uploadReadError(w, err)
			return
		}
		if len(data) > photo.MaxBytes {
			s.photoError(w, http.StatusRequestEntityTooLarge, photoCodeTooLarge, msgPhotoTooLarge)
			return
		}
	}
	if !seen || len(data) == 0 {
		s.photoError(w, http.StatusBadRequest, photoCodeInvalidUpload, msgPhotoInvalid)
		return
	}

	contentType, ok := photo.Sniff(data)
	if !ok {
		s.photoError(w, http.StatusUnsupportedMediaType, photoCodeUnsupportedType, msgPhotoType)
		return
	}
	clean, err := photo.StripMetadata(contentType, data)
	if err != nil {
		s.photoError(w, http.StatusUnprocessableEntity, photoCodeUnreadable, msgPhotoUnreadable)
		return
	}

	m, err := s.photos.AddRequestPhoto(r.Context(), userID, requestID, contentType, clean)
	switch {
	case errors.Is(err, store.ErrPhotoNotFound):
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	case errors.Is(err, store.ErrPhotoNotAllowed):
		s.photoError(w, http.StatusConflict, photoCodeNotAllowed, msgPhotoNotAllowed)
		return
	case errors.Is(err, store.ErrPhotoLimit):
		s.photoError(w, http.StatusConflict, photoCodeLimit, msgPhotoLimit)
		return
	case err != nil:
		s.log.Error("không ghi được ảnh yêu cầu", "ma_yeu_cau", requestID, "loi", err.Error())
		s.photoError(w, http.StatusInternalServerError, photoCodeInternal, loiHeThong)
		return
	}

	s.log.Info("nhận ảnh yêu cầu", "ma_yeu_cau", m.RequestID, "ma_anh", m.ID,
		"kieu", m.ContentType, "so_byte", m.SizeBytes, "thu_tu", m.Position)
	s.traJSON(w, http.StatusCreated, toPhotoOut(m))
}

// uploadReadError: the body cap tripping is a 413; anything else while reading multipart is
// a malformed upload. The error text is not returned — it can quote what the client sent.
func (s *Server) uploadReadError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		s.photoError(w, http.StatusRequestEntityTooLarge, photoCodeTooLarge, msgPhotoTooLarge)
		return
	}
	s.photoError(w, http.StatusBadRequest, photoCodeInvalidUpload, msgPhotoInvalid)
}

func (s *Server) listRequestPhotos(w http.ResponseWriter, r *http.Request, userID, requestID string) {
	ds, err := s.photos.ListRequestPhotos(r.Context(), userID, requestID)
	if errors.Is(err, store.ErrPhotoNotFound) {
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	}
	if err != nil {
		s.log.Error("không đọc được danh sách ảnh", "ma_yeu_cau", requestID, "loi", err.Error())
		s.photoError(w, http.StatusInternalServerError, photoCodeInternal, loiHeThong)
		return
	}
	items := make([]photoOut, 0, len(ds))
	for _, m := range ds {
		items = append(items, toPhotoOut(m))
	}
	s.traJSON(w, http.StatusOK, photoListOut{Items: items})
}

func (s *Server) readRequestPhoto(w http.ResponseWriter, r *http.Request, userID, requestID, photoID string) {
	ct, data, err := s.photos.ReadRequestPhoto(r.Context(), userID, requestID, photoID)
	if errors.Is(err, store.ErrPhotoNotFound) {
		s.photoError(w, http.StatusNotFound, photoCodeNotFound, msgPhotoNotFound)
		return
	}
	if err != nil {
		s.log.Error("không đọc được ảnh", "ma_yeu_cau", requestID, "ma_anh", photoID, "loi", err.Error())
		s.photoError(w, http.StatusInternalServerError, photoCodeInternal, loiHeThong)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	// A personal photo behind a bearer: no cache on the device or any proxy.
	h.Set("Cache-Control", "private, no-store")
	// The stored type was sniffed, but a browser must not second-guess it, and a photo
	// opened directly must not run anything.
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", `inline; filename="photo"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
