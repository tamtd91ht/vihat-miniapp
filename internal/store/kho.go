// Package store là nơi DUY NHẤT biết SQL. Tầng HTTP không thấy một câu lệnh nào.
//
// Nguyên tắc quan trọng nhất của gói này: một lần đăng nhập thành công ghi
// người dùng + phiên + nhật ký trong MỘT giao dịch. Ghi nghiệp vụ xong mà nhật
// ký hỏng là một sự việc đã xảy ra nhưng không còn dấu vết — đúng thứ không
// biện minh được khi có tranh chấp, và là lý do ở đây không có nhánh nào
// "ghi nhật ký sau, nếu được".
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vihat/vihat-miniapp/internal/phien"
)

type Kho struct {
	pool *pgxpool.Pool
}

// Mo mở pool và ping ngay. Hỏng thì ĐÓNG: service không được báo "sẵn sàng"
// khi chưa chạm được CSDL.
func Mo(ctx context.Context, dsn string) (*Kho, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// KHÔNG gói err gốc: chuỗi DSN mang mật khẩu, và lỗi này đi thẳng vào log.
		return nil, errors.New("DATABASE_DSN không phân tích được (giá trị cố tình không in ra)")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("không mở được pool tới CSDL")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		// err của ping mang host/port chứ không mang mật khẩu — giữ lại để chẩn đoán.
		return nil, fmt.Errorf("không ping được CSDL: %w", err)
	}
	return &Kho{pool: pool}, nil
}

func (k *Kho) Dong() {
	if k != nil && k.pool != nil {
		k.pool.Close()
	}
}

func (k *Kho) Ping(ctx context.Context) error {
	if err := k.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping CSDL: %w", err)
	}
	return nil
}

const sqlNguoiDung = `
	INSERT INTO nguoi_dung (id, so_dien_thoai)
	VALUES ($1, $2)
	ON CONFLICT (so_dien_thoai) DO UPDATE SET cap_nhat_luc = now()
	RETURNING id`

const sqlPhien = `
	INSERT INTO phien (id, nguoi_dung_id, token_bam, het_han_luc)
	VALUES ($1, $2, $3, $4)
	RETURNING het_han_luc`

const sqlNhatKy = `
	INSERT INTO nhat_ky_dang_nhap (nguoi_dung_id, phien_id, ket_qua, ly_do, dia_chi_ip)
	VALUES ($1, $2, $3, $4, $5)`

// TaoPhienDangNhap: một giao dịch, ba việc.
//
//  1. có người dùng cho số này chưa — chưa thì tạo
//  2. mở phiên mới, chỉ lưu BẢN BĂM của token
//  3. ghi nhật ký 'thanh_cong' bằng MÃ ĐỊNH DANH, không phải số điện thoại
//
// Cả ba cùng vào hoặc cùng không.
//
// soDienThoai là dữ liệu cá nhân: nó vào đúng một cột và không đi đâu khác —
// không vào lỗi trả về, không vào log, không đi ngược lên tầng HTTP.
func (k *Kho) TaoPhienDangNhap(
	ctx context.Context,
	soDienThoai string,
	tokenBam []byte,
	hetHanLuc time.Time,
	ip *netip.Addr,
) (phien.KetQuaTao, error) {
	var kq phien.KetQuaTao

	tx, err := k.pool.Begin(ctx)
	if err != nil {
		return kq, fmt.Errorf("mở giao dịch: %w", err)
	}
	// Rollback sau Commit là no-op — an toàn, và là cách duy nhất chắc chắn
	// không bỏ sót một nhánh return nào ở giữa.
	defer func() { _ = tx.Rollback(ctx) }()

	nguoiDungID, err := sinhUUID()
	if err != nil {
		return kq, err
	}
	phienID, err := sinhUUID()
	if err != nil {
		return kq, err
	}

	// ON CONFLICT ... DO UPDATE (chứ không DO NOTHING) để RETURNING luôn trả về
	// một hàng, kể cả khi người dùng đã tồn tại.
	if err := tx.QueryRow(ctx, sqlNguoiDung, nguoiDungID, soDienThoai).Scan(&nguoiDungID); err != nil {
		// Lỗi của pgx nêu tên bảng/cột chứ không nêu giá trị tham số.
		return kq, fmt.Errorf("ghi nguoi_dung: %w", err)
	}

	if err := tx.QueryRow(ctx, sqlPhien, phienID, nguoiDungID, tokenBam, hetHanLuc).Scan(&kq.HetHanLuc); err != nil {
		return kq, fmt.Errorf("ghi phien: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlNhatKy, nguoiDungID, phienID, phien.KetQuaThanhCong, nil, ip); err != nil {
		return kq, fmt.Errorf("ghi nhat_ky_dang_nhap: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return kq, fmt.Errorf("commit giao dịch đăng nhập: %w", err)
	}

	kq.NguoiDungID = chuoiUUID(nguoiDungID)
	kq.PhienID = chuoiUUID(phienID)
	return kq, nil
}

// GhiNhatKyThatBai ghi một lần đăng nhập KHÔNG thành công.
//
// Chưa biết là ai nên nguoi_dung_id để NULL — đó là sự thật của lần thử đó,
// không phải một thiếu sót cần lấp.
//
// lyDo là MÃ NGẮN của ứng dụng ('token_zalo_tu_choi'), để đếm và dựng cảnh báo.
// Không bao giờ là thông điệp của Zalo, không bao giờ mang dữ liệu người dùng.
func (k *Kho) GhiNhatKyThatBai(ctx context.Context, ketQua, lyDo string, ip *netip.Addr) error {
	var lyDoDB *string
	if lyDo != "" {
		lyDoDB = &lyDo
	}
	if _, err := k.pool.Exec(ctx, sqlNhatKy, nil, nil, ketQua, lyDoDB, ip); err != nil {
		return fmt.Errorf("ghi nhat_ky_dang_nhap (thất bại): %w", err)
	}
	return nil
}

// sinhUUID tạo UUID v4 bằng crypto/rand.
//
// Không dùng số tăng dần: mã định danh này là thứ ĐI RA NGOÀI (nhật ký, hỗ trợ,
// thống kê), mà số tăng dần thì vừa đoán được vừa để lộ quy mô người dùng.
func sinhUUID() (pgtype.UUID, error) {
	var u pgtype.UUID
	if _, err := rand.Read(u.Bytes[:]); err != nil {
		return u, fmt.Errorf("sinh UUID: %w", err)
	}
	u.Bytes[6] = (u.Bytes[6] & 0x0f) | 0x40 // phiên bản 4
	u.Bytes[8] = (u.Bytes[8] & 0x3f) | 0x80 // biến thể RFC 4122
	u.Valid = true
	return u, nil
}

func chuoiUUID(u pgtype.UUID) string {
	h := hex.EncodeToString(u.Bytes[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
