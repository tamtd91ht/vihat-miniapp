package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// ErrKhongCoPhien — không có phiên còn hiệu lực cho bearer vừa nhận.
//
// MỘT lỗi cho cả ba nguyên nhân (không tồn tại · hết hạn · đã thu hồi), có chủ
// đích: tầng HTTP trả 401 cho cả ba, và phân biệt chúng ra ngoài là nói cho
// người dò token biết họ đang dò đúng hướng.
var ErrKhongCoPhien = errors.New("store: không có phiên còn hiệu lực")

// ErrKhongCoNguoiDung — không tra được người dùng. Chỉ xảy ra khi dữ liệu đã
// lệch (khoá ngoại nói có, hàng thì không), nên nó là lỗi hệ thống, không phải
// một kết cục nghiệp vụ.
var ErrKhongCoNguoiDung = errors.New("store: không tra được người dùng")

const sqlTraPhien = `
	SELECT nguoi_dung_id
	  FROM phien
	 WHERE token_bam = $1
	   AND thu_hoi_luc IS NULL
	   AND het_han_luc > now()`

// TraPhienConHieuLuc đổi BẢN BĂM của bearer lấy mã định danh người dùng.
//
// Nhận bản băm chứ không nhận token: token nguyên bản không bao giờ đi vào một
// câu SQL, nên nó không bao giờ nằm lại trong `pg_stat_statements`, trong log
// truy vấn chậm, hay trong một bản kết xuất nào.
//
// Ba điều kiện kiểm CÙNG LÚC trong một câu, không tách ra Go: tách ra thì giữa
// hai lần đọc có một khoảng mà một phiên vừa bị thu hồi vẫn dùng được.
func (k *Kho) TraPhienConHieuLuc(ctx context.Context, tokenBam []byte) (string, error) {
	var nguoiDungID pgtype.UUID
	err := k.pool.QueryRow(ctx, sqlTraPhien, tokenBam).Scan(&nguoiDungID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKhongCoPhien
	}
	if err != nil {
		return "", fmt.Errorf("tra phiên: %w", err)
	}
	return chuoiUUID(nguoiDungID), nil
}

const sqlTaoYeuCau = `
	INSERT INTO yeu_cau (id, nguoi_dung_id, loai, quan_tam, quy_mo, ghi_chu, nguon_chien_dich, ten_hien_thi)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING id`

const sqlLichSuDau = `
	INSERT INTO yeu_cau_lich_su (yeu_cau_id, tu_trang_thai, den_trang_thai, boi, ly_do)
	VALUES ($1, NULL, 'moi', 'he_thong', 'nguoi_dung_gui')`

// TaoYeuCau ghi phiếu và dòng lịch sử đầu tiên trong MỘT giao dịch.
//
// Cùng vào hoặc cùng không. Một phiếu không có dòng lịch sử nào là một phiếu
// không trả lời được câu "nó bắt đầu lúc nào, do đâu" — và đó đúng là câu được
// hỏi khi có tranh chấp, tức là lúc không còn dựng lại được nữa.
func (k *Kho) TaoYeuCau(ctx context.Context, tt yeucau.ThongTinTao) (string, error) {
	nguoiDungID, err := uuidTu(tt.NguoiDungID)
	if err != nil {
		return "", err
	}
	id, err := sinhUUID()
	if err != nil {
		return "", err
	}

	tx, err := k.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	quanTam := tt.QuanTam
	if quanTam == nil {
		// Cột là NOT NULL DEFAULT '{}'. Gửi nil xuống thì pgx mã hoá thành NULL
		// và câu INSERT hỏng — một mảng rỗng và một mảng không có là hai thứ
		// khác nhau với Postgres, dù trong Go chúng cùng là `nil`.
		quanTam = []string{}
	}

	if err := tx.QueryRow(ctx, sqlTaoYeuCau,
		id, nguoiDungID, tt.Loai, quanTam,
		// `rongThanhNil` là helper đã có (`an_danh.go`) và nó KHÔNG cắt khoảng
		// trắng. Cắt ở đây chứ không sửa nó: một helper dùng chung sửa để phục vụ
		// chỗ gọi mới là một thay đổi hành vi của đường ẩn danh hoá, thứ không
		// liên quan gì tới lượt này.
		rongThanhNil(strings.TrimSpace(tt.QuyMo)),
		rongThanhNil(strings.TrimSpace(tt.GhiChu)),
		rongThanhNil(strings.TrimSpace(tt.NguonChienDich)),
		// ten_hien_thi (0004): rỗng → NULL, vì CHECK cấm chuỗi rỗng — "không
		// có tên" chỉ có một cách viết. CHECK cũng cấm tên trên loại khác chat.
		rongThanhNil(strings.TrimSpace(tt.TenHienThi)),
	).Scan(&id); err != nil {
		// Lỗi của pgx nêu tên bảng/ràng buộc, không nêu giá trị tham số — nên
		// nó được phép gói lại và đi lên. Nếu điều đó đổi, chỗ này phải đổi:
		// `ghi_chu` là văn bản người dùng gõ.
		return "", fmt.Errorf("ghi yeu_cau: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlLichSuDau, id); err != nil {
		return "", fmt.Errorf("ghi yeu_cau_lich_su: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit giao dịch yêu cầu: %w", err)
	}
	return chuoiUUID(id), nil
}

const sqlDemTrongCuaSo = `
	SELECT count(*)
	  FROM yeu_cau
	 WHERE nguoi_dung_id = $1 AND loai = $2 AND tao_luc >= $3`

// DemTrongCuaSo đếm yêu cầu một loại của một người kể từ mốc `tu`.
//
// Mốc do BÊN GỌI truyền xuống chứ không tính bằng `now()` trong SQL: nghiệp vụ
// sở hữu cửa sổ trượt (yeucau.CuaSoGoiLai), và một khoảng thời gian nằm rải
// giữa Go và SQL là một khoảng không ai kiểm được bằng test.
func (k *Kho) DemTrongCuaSo(ctx context.Context, nguoiDungID, loai string, tu time.Time) (int, error) {
	id, err := uuidTu(nguoiDungID)
	if err != nil {
		return 0, err
	}
	var so int
	if err := k.pool.QueryRow(ctx, sqlDemTrongCuaSo, id, loai, tu).Scan(&so); err != nil {
		return 0, fmt.Errorf("đếm yeu_cau trong cửa sổ: %w", err)
	}
	return so, nil
}

const sqlDanhSachCuaToi = `
	SELECT id, loai, trang_thai, tao_luc
	  FROM yeu_cau
	 WHERE nguoi_dung_id = $1
	 ORDER BY tao_luc DESC
	 LIMIT $2`

// DanhSachCuaToi trả yêu cầu của ĐÚNG người này.
//
// ⚠ `nguoi_dung_id` LÀ ĐIỀU KIỆN BẮT BUỘC TRONG CHÍNH CÂU SQL, không phải một
// bộ lọc ở tầng trên. Lọc ở tầng trên nghĩa là có một khoảnh khắc mà hàng của
// người khác đã nằm trong bộ nhớ tiến trình, và khoảnh khắc ấy là thứ một lần
// sửa vô ý biến thành một tuyến rò dữ liệu.
//
// KHÔNG trả `ghi_chu` — xem yeucau.TomTat.
func (k *Kho) DanhSachCuaToi(ctx context.Context, nguoiDungID string, tran int) ([]yeucau.TomTat, error) {
	id, err := uuidTu(nguoiDungID)
	if err != nil {
		return nil, err
	}
	if tran <= 0 {
		tran = 20
	}

	hang, err := k.pool.Query(ctx, sqlDanhSachCuaToi, id, tran)
	if err != nil {
		return nil, fmt.Errorf("đọc yeu_cau: %w", err)
	}
	defer hang.Close()

	ds := make([]yeucau.TomTat, 0, tran)
	for hang.Next() {
		var ma pgtype.UUID
		var t yeucau.TomTat
		if err := hang.Scan(&ma, &t.Loai, &t.TrangThai, &t.TaoLuc); err != nil {
			return nil, fmt.Errorf("đọc dòng yeu_cau: %w", err)
		}
		t.Ma = chuoiUUID(ma)
		ds = append(ds, t)
	}
	if err := hang.Err(); err != nil {
		return nil, fmt.Errorf("duyệt yeu_cau: %w", err)
	}
	return ds, nil
}

const sqlSoDeLienHe = `SELECT so_dien_thoai FROM nguoi_dung WHERE id = $1`

// SoDeLienHe trả số điện thoại đã chuẩn hoá của một người dùng.
//
// ⚠ ĐÂY LÀ HÀM DUY NHẤT TRONG GÓI NÀY ĐƯA DỮ LIỆU CÁ NHÂN ĐI LÊN. Nó tồn tại vì
// tổng đài phải quay ra một máy thật và ZNS phải gửi tới một máy thật — không có
// đường nào làm hai việc ấy mà không biết số.
//
// Bên gọi duy nhất được phép là `internal/yeucau`, và ở đó số đi thẳng xuống bộ
// điều hợp rồi hết. Nếu một ngày có bên gọi thứ hai, hãy đọc lại khối chú thích
// đầu gói `yeucau` trước khi thêm nó.
func (k *Kho) SoDeLienHe(ctx context.Context, nguoiDungID string) (string, error) {
	id, err := uuidTu(nguoiDungID)
	if err != nil {
		return "", err
	}
	var so string
	err = k.pool.QueryRow(ctx, sqlSoDeLienHe, id).Scan(&so)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKhongCoNguoiDung
	}
	if err != nil {
		// KHÔNG gói `err` kèm gì có thể mang giá trị cột — chỉ tên việc.
		return "", fmt.Errorf("tra số liên hệ: %w", err)
	}
	return so, nil
}

const sqlDemZNS = `
	SELECT count(*)
	  FROM zns_da_gui
	 WHERE nguoi_dung_id = $1 AND ket_qua = 'thanh_cong' AND tao_luc >= $2`

// DemZNSTrongCuaSo đếm số tin ĐÃ GỬI THÀNH CÔNG cho một người kể từ mốc `tu`.
//
// Chỉ đếm tin thành công: một lần Zalo từ chối không tốn tiền và không làm
// phiền ai, nên tính nó vào trần là tự khoá mình ra khỏi tính năng vì một sự
// cố của bên thứ ba.
func (k *Kho) DemZNSTrongCuaSo(ctx context.Context, nguoiDungID string, tu time.Time) (int, error) {
	id, err := uuidTu(nguoiDungID)
	if err != nil {
		return 0, err
	}
	var so int
	if err := k.pool.QueryRow(ctx, sqlDemZNS, id, tu).Scan(&so); err != nil {
		return 0, fmt.Errorf("đếm zns_da_gui: %w", err)
	}
	return so, nil
}

const sqlGhiVetZNS = `
	INSERT INTO zns_da_gui (yeu_cau_id, nguoi_dung_id, ma_mau, ket_qua, ly_do)
	VALUES ($1, $2, $3, $4, $5)`

// GhiVetZNS ghi một dòng vào `zns_da_gui`. Bảng chỉ ghi thêm (0003).
//
// Trùng một tin THÀNH CÔNG cho cùng một yêu cầu bị chỉ số duy nhất từng phần
// chặn ở CSDL, và lỗi ấy đi ngược lên nguyên vẹn: bên gọi ghi log rồi thôi.
// Nuốt nó ở đây là giấu đúng tín hiệu nói rằng có hai đường đang cùng gửi.
func (k *Kho) GhiVetZNS(ctx context.Context, yeuCauID, nguoiDungID, maMau, ketQua, lyDo string) error {
	ycID, err := uuidTu(yeuCauID)
	if err != nil {
		return err
	}
	ndID, err := uuidTu(nguoiDungID)
	if err != nil {
		return err
	}
	if _, err := k.pool.Exec(ctx, sqlGhiVetZNS, ycID, ndID, maMau, ketQua, rongThanhNil(strings.TrimSpace(lyDo))); err != nil {
		return fmt.Errorf("ghi zns_da_gui: %w", err)
	}
	return nil
}

// uuidTu phân tích dạng chuẩn 8-4-4-4-12 thành pgtype.UUID.
//
// PHÂN TÍCH Ở ĐÂY CHỨ KHÔNG ĐẨY CHUỖI XUỐNG CHO POSTGRES ÉP KIỂU, và đó là một
// quyết định hỏng-về-phía-đóng: một mã méo bị chặn ngay tại tiến trình này, với
// một lỗi nói đúng tên việc, thay vì thành một lỗi cú pháp của CSDL ở giữa một
// giao dịch đang mở.
func uuidTu(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	tho := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(tho) != 32 {
		return u, fmt.Errorf("mã định danh không đúng dạng UUID")
	}
	b, err := hex.DecodeString(tho)
	if err != nil {
		return u, fmt.Errorf("mã định danh không đúng dạng UUID")
	}
	copy(u.Bytes[:], b)
	u.Valid = true
	return u, nil
}
