package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// Photos attached to a "Tư vấn và báo giá" request — migrations/0005.
//
// ⚠ OWNERSHIP IS A CONDITION INSIDE EVERY SQL STATEMENT, never a filter in Go: each query
// joins yeu_cau and requires `nguoi_dung_id = <the session's user>`. A request that is not
// yours, does not exist, or was anonymised answers the SAME ErrPhotoNotFound — telling the
// three apart would tell a guesser which ids are real.

var (
	// ErrPhotoNotFound — no such request/photo FOR THIS USER (or anonymised).
	ErrPhotoNotFound = errors.New("store: photo or request not found")
	// ErrPhotoNotAllowed — the request exists and is yours, but its kind takes no photos.
	ErrPhotoNotAllowed = errors.New("store: this request kind takes no photos")
	// ErrPhotoLimit — the request already holds photo.MaxPerRequest photos.
	ErrPhotoLimit = errors.New("store: photo limit reached")
)

// PhotoMeta — what may be said about a stored photo. Never the bytes.
type PhotoMeta struct {
	ID          string
	RequestID   string
	Position    int
	ContentType string
	SizeBytes   int
	CreatedAt   time.Time
}

// FOR UPDATE: two uploads racing for the third slot serialise on the request row, so the
// count below is exact. The UNIQUE (request_id, position) of 0005 is the second wall.
const sqlLockOwnRequest = `
	SELECT loai
	  FROM yeu_cau
	 WHERE id = $1 AND nguoi_dung_id = $2 AND an_danh_luc IS NULL
	   FOR UPDATE`

const sqlCountPhotos = `SELECT count(*) FROM request_photo WHERE request_id = $1`

const sqlInsertPhoto = `
	INSERT INTO request_photo (id, request_id, position, content_type, size_bytes, sha256, data)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING created_at`

// AddRequestPhoto stores one photo on the user's own consult request. `data` must already
// be sniffed and stripped (internal/photo); the type is re-checked by the CHECK of 0005.
func (k *Kho) AddRequestPhoto(ctx context.Context, userID, requestID, contentType string, data []byte) (PhotoMeta, error) {
	var m PhotoMeta
	uid, err := uuidTu(userID)
	if err != nil {
		return m, err
	}
	rid, err := uuidTu(requestID)
	if err != nil {
		// A malformed id from the path is "not found", not a system error.
		return m, ErrPhotoNotFound
	}
	pid, err := sinhUUID()
	if err != nil {
		return m, err
	}

	tx, err := k.pool.Begin(ctx)
	if err != nil {
		return m, fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var loai string
	if err := tx.QueryRow(ctx, sqlLockOwnRequest, rid, uid).Scan(&loai); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return m, ErrPhotoNotFound
		}
		return m, fmt.Errorf("khoá yeu_cau: %w", err)
	}
	if loai != yeucau.LoaiTuVan {
		return m, ErrPhotoNotAllowed
	}

	var n int
	if err := tx.QueryRow(ctx, sqlCountPhotos, rid).Scan(&n); err != nil {
		return m, fmt.Errorf("đếm request_photo: %w", err)
	}
	if n >= photo.MaxPerRequest {
		return m, ErrPhotoLimit
	}

	sum := sha256.Sum256(data)
	// pgx errors name the table/constraint, never parameter values — the bytes cannot
	// reach a log line through this wrap.
	if err := tx.QueryRow(ctx, sqlInsertPhoto, pid, rid, n+1, contentType, len(data), sum[:], data).
		Scan(&m.CreatedAt); err != nil {
		return m, fmt.Errorf("ghi request_photo: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return m, fmt.Errorf("commit request_photo: %w", err)
	}

	m.ID = chuoiUUID(pid)
	m.RequestID = chuoiUUID(rid)
	m.Position = n + 1
	m.ContentType = contentType
	m.SizeBytes = len(data)
	return m, nil
}

const sqlListOwnPhotos = `
	SELECT p.id, p.position, p.content_type, p.size_bytes, p.created_at
	  FROM request_photo p
	  JOIN yeu_cau y ON y.id = p.request_id
	 WHERE p.request_id = $1 AND y.nguoi_dung_id = $2
	   AND y.an_danh_luc IS NULL AND p.anonymised_at IS NULL
	 ORDER BY p.position`

const sqlOwnRequestExists = `
	SELECT EXISTS (SELECT 1 FROM yeu_cau WHERE id = $1 AND nguoi_dung_id = $2 AND an_danh_luc IS NULL)`

// ListRequestPhotos — the photos of the user's own request, without bytes. A request that
// is not the user's is ErrPhotoNotFound (never an empty list: an empty list for someone
// else's id would confirm the id exists).
func (k *Kho) ListRequestPhotos(ctx context.Context, userID, requestID string) ([]PhotoMeta, error) {
	uid, err := uuidTu(userID)
	if err != nil {
		return nil, err
	}
	rid, err := uuidTu(requestID)
	if err != nil {
		return nil, ErrPhotoNotFound
	}
	var own bool
	if err := k.pool.QueryRow(ctx, sqlOwnRequestExists, rid, uid).Scan(&own); err != nil {
		return nil, fmt.Errorf("tra yeu_cau: %w", err)
	}
	if !own {
		return nil, ErrPhotoNotFound
	}

	rows, err := k.pool.Query(ctx, sqlListOwnPhotos, rid, uid)
	if err != nil {
		return nil, fmt.Errorf("đọc request_photo: %w", err)
	}
	defer rows.Close()
	out := make([]PhotoMeta, 0, photo.MaxPerRequest)
	for rows.Next() {
		var id pgtype.UUID
		m := PhotoMeta{RequestID: chuoiUUID(rid)}
		if err := rows.Scan(&id, &m.Position, &m.ContentType, &m.SizeBytes, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("đọc dòng request_photo: %w", err)
		}
		m.ID = chuoiUUID(id)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("duyệt request_photo: %w", err)
	}
	return out, nil
}

const sqlReadOwnPhoto = `
	SELECT p.content_type, p.data
	  FROM request_photo p
	  JOIN yeu_cau y ON y.id = p.request_id
	 WHERE p.id = $1 AND p.request_id = $2 AND y.nguoi_dung_id = $3
	   AND y.an_danh_luc IS NULL AND p.anonymised_at IS NULL`

// ReadRequestPhoto returns the bytes of one photo of the user's own request.
func (k *Kho) ReadRequestPhoto(ctx context.Context, userID, requestID, photoID string) (string, []byte, error) {
	uid, err := uuidTu(userID)
	if err != nil {
		return "", nil, err
	}
	rid, err := uuidTu(requestID)
	if err != nil {
		return "", nil, ErrPhotoNotFound
	}
	pid, err := uuidTu(photoID)
	if err != nil {
		return "", nil, ErrPhotoNotFound
	}
	var ct string
	var data []byte
	err = k.pool.QueryRow(ctx, sqlReadOwnPhoto, pid, rid, uid).Scan(&ct, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrPhotoNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("đọc ảnh: %w", err)
	}
	return ct, data, nil
}

// No owner condition — the caller has verified a signed link for exactly this photo id
// (httpapi/request_photo_links.go). The anonymisation conditions ARE here: an erased photo
// (or one on an anonymised request) is not found, whatever link points at it.
const sqlReadPhotoForDownload = `
	SELECT p.content_type, p.data
	  FROM request_photo p
	  JOIN yeu_cau y ON y.id = p.request_id
	 WHERE p.id = $1
	   AND y.an_danh_luc IS NULL AND p.anonymised_at IS NULL AND p.data IS NOT NULL`

// ReadPhotoForDownload returns one photo's bytes for a verified signed link.
func (k *Kho) ReadPhotoForDownload(ctx context.Context, photoID string) (string, []byte, error) {
	pid, err := uuidTu(photoID)
	if err != nil {
		return "", nil, ErrPhotoNotFound
	}
	var ct string
	var data []byte
	err = k.pool.QueryRow(ctx, sqlReadPhotoForDownload, pid).Scan(&ct, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrPhotoNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("đọc ảnh theo link: %w", err)
	}
	return ct, data, nil
}
