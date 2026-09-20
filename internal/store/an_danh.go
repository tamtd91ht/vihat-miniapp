package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ẨN DANH HOÁ — cách kho này thực hiện một yêu cầu xoá theo Nghị định 13.
//
// Chủ sản phẩm chốt 20/09/2026: XOÁ NGHĨA LÀ ẨN DANH HOÁ. Lược đồ cố ý không
// cho xoá thật — nhat_ky_dang_nhap tham chiếu nguoi_dung(id) và nhật ký thì
// chỉ được ghi thêm, nên một hàng nguoi_dung đã từng đăng nhập là không xoá
// được, cũng không null hoá khoá ngoại đi được. Đó là thiết kế, không phải
// thiếu sót: dấu vết "có một lần đăng nhập lúc 14:02" phải còn, còn "người ấy
// là ai" thì biến mất.
//
// Sau khi chạy: hàng nguoi_dung còn đó nhưng không còn chỉ về một con người
// nào; các dòng nhật ký vẫn gắn vào nó bằng mã định danh, và không dòng nào
// trong số đó từng chứa số điện thoại.

// YeuCauAnDanh — một yêu cầu xoá đã nhận, kèm người chịu trách nhiệm.
type YeuCauAnDanh struct {
	// SoDienThoai đã chuẩn hoá (84xxxxxxxxx). Chỉ sống trong RAM và trong tham
	// số truy vấn: không log, không in, không đưa vào thông điệp lỗi.
	SoDienThoai string

	NguonYeuCau   string // 'hotline' hoặc 'email' — CSDL kiểm lại bằng CHECK
	NguoiThucHien string // tên/mã nhân sự của người bấm lệnh
	GhiChu        string // số phiếu, mã cuộc gọi. KHÔNG chép nội dung yêu cầu
}

// KetQuaAnDanh — thứ được phép nói ra sau khi chạy. Không có số điện thoại.
type KetQuaAnDanh struct {
	NguoiDungID   string
	SoPhienThuHoi int
	ThoiDiem      time.Time
}

// ErrKhongTimThayNguoiDung: không có ai ứng với số ấy — có thể vì chưa từng
// đăng nhập, hoặc vì đã ẩn danh trước đó rồi. Không phân biệt hai trường hợp
// trong thông điệp: người gọi lệnh không cần biết, và mỗi lần phân biệt là một
// lần tiết lộ "số này có trong hệ thống".
var ErrKhongTimThayNguoiDung = errors.New("không tìm thấy người dùng ứng với số đã nhập")

// Giá trị thay thế: '0' + 13 chữ số từ dãy nguoi_dung_an_danh_seq.
//
// Cột so_dien_thoai là NOT NULL UNIQUE CHECK (~'^[0-9]{9,15}$') nên không dùng
// được NULL, chuỗi rỗng, hay một hằng chung cho mọi người. Số thật luôn đã
// chuẩn hoá về dạng có mã quốc gia nên không bao giờ bắt đầu bằng '0' — nhìn
// cột là biết hàng nào đã ẩn danh, không cần thêm cờ.
const sqlAnDanhNguoiDung = `
	UPDATE nguoi_dung
	   SET so_dien_thoai = '0' || lpad(nextval('nguoi_dung_an_danh_seq')::text, 13, '0'),
	       cap_nhat_luc  = now()
	 WHERE so_dien_thoai = $1
	RETURNING id`

// Thu hồi mọi phiên CÒN HIỆU LỰC. Ẩn danh xong mà phiên cũ vẫn đăng nhập được
// thì chưa xoá gì cả — người cầm token vẫn vào được tài khoản ấy.
// Phiên đã hết hạn thì để nguyên: chúng không mở được cửa nào nữa.
const sqlThuHoiPhien = `
	UPDATE phien
	   SET thu_hoi_luc = now()
	 WHERE nguoi_dung_id = $1
	   AND thu_hoi_luc IS NULL
	   AND het_han_luc > now()`

const sqlGhiNhatKyAnDanh = `
	INSERT INTO nhat_ky_an_danh (nguoi_dung_id, nguon_yeu_cau, nguoi_thuc_hien, ghi_chu)
	VALUES ($1, $2, $3, $4)
	RETURNING tao_luc`

// AnDanhHoa chạy cả ba việc trong MỘT giao dịch: ghi đè định danh, thu hồi
// phiên, ghi bằng chứng đã xử lý yêu cầu.
//
// Một giao dịch vì hai nửa của việc này không được phép rời nhau: ẩn danh mà
// không thu hồi phiên là chưa xoá; thu hồi phiên mà không ghi ai đã ra lệnh là
// một thay đổi không ai chịu trách nhiệm.
//
// KHÔNG log, KHÔNG đưa số điện thoại vào bất kỳ lỗi nào trả ra từ đây.
func (k *Kho) AnDanhHoa(ctx context.Context, yc YeuCauAnDanh) (KetQuaAnDanh, error) {
	var kq KetQuaAnDanh

	tx, err := k.pool.Begin(ctx)
	if err != nil {
		return kq, fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	if err := tx.QueryRow(ctx, sqlAnDanhNguoiDung, yc.SoDienThoai).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return kq, ErrKhongTimThayNguoiDung
		}
		return kq, fmt.Errorf("ẩn danh nguoi_dung: %w", err)
	}

	the, err := tx.Exec(ctx, sqlThuHoiPhien, id)
	if err != nil {
		return kq, fmt.Errorf("thu hồi phiên: %w", err)
	}

	if err := tx.QueryRow(ctx, sqlGhiNhatKyAnDanh,
		id, yc.NguonYeuCau, yc.NguoiThucHien, rongThanhNil(yc.GhiChu)).Scan(&kq.ThoiDiem); err != nil {
		return kq, fmt.Errorf("ghi nhat_ky_an_danh: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return kq, fmt.Errorf("commit giao dịch ẩn danh: %w", err)
	}

	kq.NguoiDungID = id
	kq.SoPhienThuHoi = int(the.RowsAffected())
	return kq, nil
}

func rongThanhNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
