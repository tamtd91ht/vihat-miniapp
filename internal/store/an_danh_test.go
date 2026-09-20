package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
)

// Chạm CSDL thật — SKIP khi thiếu TEST_DATABASE_DSN (xem kho_test.go).

var dangDaAnDanh = regexp.MustCompile(`^0[0-9]{13}$`)

func TestAnDanhHoa_XoaDinhDanhNhungGiuNhatKy(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t)

	// Hai lần đăng nhập: một phiên còn hiệu lực, một phiên đã hết hạn.
	conHan, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-con-han-"+so), time.Now().Add(time.Hour), ipTest())
	if err != nil {
		t.Fatalf("mở phiên còn hạn: %s", err)
	}
	if _, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-het-han-"+so), time.Now().Add(time.Second), nil); err != nil {
		t.Fatalf("mở phiên thứ hai: %s", err)
	}

	kq, err := k.AnDanhHoa(ctx, YeuCauAnDanh{
		SoDienThoai:   so,
		NguonYeuCau:   "hotline",
		NguoiThucHien: "NV-017",
		GhiChu:        "PH-2026-0412",
	})
	if err != nil {
		t.Fatalf("ẩn danh: %s", err)
	}
	if kq.NguoiDungID != conHan.NguoiDungID {
		t.Errorf("ẩn danh nhầm người: %s vs %s", kq.NguoiDungID, conHan.NguoiDungID)
	}
	if kq.SoPhienThuHoi < 1 {
		t.Errorf("thu hồi %d phiên, mong ít nhất 1 (phiên còn hiệu lực)", kq.SoPhienThuHoi)
	}

	// 1. Số điện thoại không còn trong CSDL.
	var conSo bool
	if err := k.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM nguoi_dung WHERE so_dien_thoai = $1)`, so).Scan(&conSo); err != nil {
		t.Fatalf("soi nguoi_dung: %s", err)
	}
	if conSo {
		t.Error("số điện thoại vẫn còn sau khi ẩn danh")
	}

	// 2. Hàng nguoi_dung vẫn còn, mang giá trị thay thế hợp lệ và nhận ra được.
	var thayThe string
	if err := k.pool.QueryRow(ctx,
		`SELECT so_dien_thoai FROM nguoi_dung WHERE id = $1`, kq.NguoiDungID).Scan(&thayThe); err != nil {
		t.Fatalf("đọc nguoi_dung sau khi ẩn danh: %s", err)
	}
	if !dangDaAnDanh.MatchString(thayThe) {
		t.Errorf("giá trị thay thế %q không đúng dạng '0' + 13 chữ số", thayThe)
	}

	// 3. Mọi phiên còn hiệu lực đã bị thu hồi — ẩn danh mà token cũ vẫn vào
	//    được thì chưa xoá gì cả.
	var conSong int
	if err := k.pool.QueryRow(ctx,
		`SELECT count(*) FROM phien
		  WHERE nguoi_dung_id = $1 AND thu_hoi_luc IS NULL AND het_han_luc > now()`,
		kq.NguoiDungID).Scan(&conSong); err != nil {
		t.Fatalf("soi phien: %s", err)
	}
	if conSong != 0 {
		t.Errorf("còn %d phiên đăng nhập được sau khi ẩn danh", conSong)
	}

	// 4. Nhật ký đăng nhập GIỮ NGUYÊN và vẫn gắn vào mã định danh ấy.
	var demNhatKy int
	if err := k.pool.QueryRow(ctx,
		`SELECT count(*) FROM nhat_ky_dang_nhap WHERE nguoi_dung_id = $1`,
		kq.NguoiDungID).Scan(&demNhatKy); err != nil {
		t.Fatalf("đếm nhật ký: %s", err)
	}
	if demNhatKy != 2 {
		t.Errorf("còn %d dòng nhật ký, mong giữ nguyên 2", demNhatKy)
	}

	// 5. Có bằng chứng ai đã ra lệnh.
	var nguoi, nguon string
	if err := k.pool.QueryRow(ctx,
		`SELECT nguoi_thuc_hien, nguon_yeu_cau FROM nhat_ky_an_danh WHERE nguoi_dung_id = $1`,
		kq.NguoiDungID).Scan(&nguoi, &nguon); err != nil {
		t.Fatalf("đọc nhat_ky_an_danh: %s", err)
	}
	if nguoi != "NV-017" || nguon != "hotline" {
		t.Errorf("bằng chứng sai: %q / %q", nguoi, nguon)
	}

	// 6. Chạy lại với số cũ thì không tìm thấy ai nữa.
	if _, err := k.AnDanhHoa(ctx, YeuCauAnDanh{
		SoDienThoai: so, NguonYeuCau: "email", NguoiThucHien: "NV-017",
	}); !errors.Is(err, ErrKhongTimThayNguoiDung) {
		t.Errorf("lần hai: lỗi = %v, mong ErrKhongTimThayNguoiDung", err)
	}
}

func TestAnDanhHoa_KhongCoAiThiKhongDongGi(t *testing.T) {
	k := moKhoTest(t)
	_, err := k.AnDanhHoa(context.Background(), YeuCauAnDanh{
		SoDienThoai: soTest(t), NguonYeuCau: "email", NguoiThucHien: "NV-017",
	})
	if !errors.Is(err, ErrKhongTimThayNguoiDung) {
		t.Fatalf("lỗi = %v, mong ErrKhongTimThayNguoiDung", err)
	}
}

// Người thực hiện bỏ trống thì CSDL phải từ chối: "có người ký" là ràng buộc,
// không phải quy ước.
func TestAnDanhHoa_PhaiCoNguoiKy(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t)

	if _, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-ky-"+so), time.Now().Add(time.Hour), nil); err != nil {
		t.Fatalf("mở phiên: %s", err)
	}
	if _, err := k.AnDanhHoa(ctx, YeuCauAnDanh{
		SoDienThoai: so, NguonYeuCau: "hotline", NguoiThucHien: "   ",
	}); err == nil {
		t.Fatal("thiếu người thực hiện mà vẫn ẩn danh được")
	}

	// Giao dịch phải cuộn lại trọn vẹn: số cũ còn nguyên.
	var con bool
	if err := k.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM nguoi_dung WHERE so_dien_thoai = $1)`, so).Scan(&con); err != nil {
		t.Fatalf("soi nguoi_dung: %s", err)
	}
	if !con {
		t.Error("ghi nhật ký hỏng nhưng định danh đã bị ghi đè — ba việc không nằm chung một giao dịch")
	}
}

// ---------------------------------------------------------------------------
// Dọn nhật ký 90 ngày (migrations/0002)
// ---------------------------------------------------------------------------

// taoPhanManhTest dựng một phân mảnh tuần bắt đầu từ `dau`, và dọn nó khi ca
// kết thúc — không dọn thì lần chạy hôm sau đụng phải khoảng chồng nhau.
func taoPhanManhTest(t *testing.T, k *Kho, dau time.Time) string {
	t.Helper()
	ten := "nhat_ky_dang_nhap_t" + dau.Format("2006_0102")
	_, err := k.pool.Exec(context.Background(), fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF nhat_ky_dang_nhap
		 FOR VALUES FROM ('%s') TO ('%s')`,
		ten,
		dau.Format("2006-01-02 15:04:05-07"),
		dau.AddDate(0, 0, 7).Format("2006-01-02 15:04:05-07")))
	if err != nil {
		t.Fatalf("tạo phân mảnh %s: %s", ten, err)
	}
	t.Cleanup(func() {
		_, _ = k.pool.Exec(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, ten))
	})
	return ten
}

func conTonTai(t *testing.T, k *Kho, ten string) bool {
	t.Helper()
	var s *string
	if err := k.pool.QueryRow(context.Background(), `SELECT to_regclass($1)::text`, ten).Scan(&s); err != nil {
		t.Fatalf("tra bảng %s: %s", ten, err)
	}
	return s != nil
}

// ngayUTC: mốc 00:00 UTC của N ngày trước — đúng dạng mốc mà hàm dọn so sánh.
func ngayUTC(truocNgay int) time.Time {
	n := time.Now().UTC().AddDate(0, 0, -truocNgay)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// Kiểm đúng cơ chế đang gánh cam kết 90 ngày: DROP phân mảnh đã quá hạn, và
// KHÔNG đụng tới phân mảnh còn trong hạn.
func TestDonNhatKy_XoaPhanManhQuaHanGiuPhanManhTrongHan(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()

	// Một phân mảnh cho tuần của 120 ngày trước — quá hạn 90 ngày.
	cu := ngayUTC(120)
	tenCu := taoPhanManhTest(t, k, cu)

	// Một dòng nhật ký nằm trong phân mảnh ấy.
	if _, err := k.pool.Exec(ctx,
		`INSERT INTO nhat_ky_dang_nhap (ket_qua, ly_do, tao_luc) VALUES ($1, $2, $3)`,
		phien.KetQuaLoiZalo, "kiem_thu_don_nhat_ky", cu.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("ghi dòng nhật ký cũ: %s", err)
	}

	// Một dòng của hôm nay, phải sống sót.
	if err := k.GhiNhatKyThatBai(ctx, phien.KetQuaLoiZalo, "kiem_thu_con_han", nil); err != nil {
		t.Fatalf("ghi dòng nhật ký mới: %s", err)
	}

	var daXoa int
	if err := k.pool.QueryRow(ctx, `SELECT nhat_ky_don_qua_han()`).Scan(&daXoa); err != nil {
		t.Fatalf("dọn nhật ký: %s", err)
	}
	if daXoa < 1 {
		t.Errorf("dọn được %d phân mảnh, mong ít nhất 1", daXoa)
	}
	if conTonTai(t, k, tenCu) {
		t.Errorf("phân mảnh quá hạn %s vẫn còn", tenCu)
	}

	var conHan int
	if err := k.pool.QueryRow(ctx,
		`SELECT count(*) FROM nhat_ky_dang_nhap WHERE ly_do = 'kiem_thu_con_han'`).Scan(&conHan); err != nil {
		t.Fatalf("đếm dòng còn hạn: %s", err)
	}
	if conHan < 1 {
		t.Error("dòng nhật ký còn trong hạn 90 ngày đã bị dọn mất")
	}
}

// RANH GIỚI — ca dễ lệch một ngày nhất, và là ca gánh cả cam kết pháp lý.
//
// Điều kiện trong hàm là `d <= hôm_nay - 90`, tức DROP khi ĐẦU phân mảnh đã
// quá hạn. Hệ quả: mỗi dòng sống TỐI ĐA 90 ngày, tối thiểu 83.
//
// Viết thành `d + 7 <=` (DROP khi ĐUÔI quá hạn) nghe có vẻ an toàn hơn — "không
// xoá sớm dòng nào" — nhưng nó đẩy dòng cũ nhất lên 97 ngày, tức vượt qua chính
// cái trần đã hứa trong văn bản pháp lý. Hai ca dưới đây khoá cả hai phía.
func TestDonNhatKy_RanhGioiDung90Ngay(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()

	t.Run("phân mảnh bắt đầu đúng 90 ngày trước thì PHẢI dọn", func(t *testing.T) {
		dau := ngayUTC(90)
		ten := taoPhanManhTest(t, k, dau)

		// Dòng ở đúng mép: 90 ngày tuổi chẵn.
		if _, err := k.pool.Exec(ctx,
			`INSERT INTO nhat_ky_dang_nhap (ket_qua, ly_do, tao_luc) VALUES ($1, $2, $3)`,
			phien.KetQuaLoiZalo, "kiem_thu_ranh_gioi_90", dau); err != nil {
			t.Fatalf("ghi dòng 90 ngày tuổi: %s", err)
		}

		if _, err := k.pool.Exec(ctx, `SELECT nhat_ky_don_qua_han()`); err != nil {
			t.Fatalf("dọn nhật ký: %s", err)
		}
		if conTonTai(t, k, ten) {
			t.Error("dòng 90 ngày tuổi vẫn còn — hệ thống đang giữ lâu hơn trần đã hứa")
		}
	})

	t.Run("phân mảnh bắt đầu 89 ngày trước thì KHÔNG đụng", func(t *testing.T) {
		dau := ngayUTC(89)
		ten := taoPhanManhTest(t, k, dau)

		if _, err := k.pool.Exec(ctx, `SELECT nhat_ky_don_qua_han()`); err != nil {
			t.Fatalf("dọn nhật ký: %s", err)
		}
		if !conTonTai(t, k, ten) {
			t.Error("dọn sớm một ngày — 89 ngày chưa tới hạn")
		}
	})
}

// Hàm dọn không được nhận số ngày vô lý — một cú gõ nhầm `0` là xoá sạch nhật
// ký của hôm nay.
func TestDonNhatKy_TuChoiSoNgayVoLy(t *testing.T) {
	k := moKhoTest(t)
	if _, err := k.pool.Exec(context.Background(), `SELECT nhat_ky_don_qua_han(0)`); err == nil {
		t.Fatal("nhat_ky_don_qua_han(0) phải bị từ chối")
	}
}

// Tạo phân mảnh phải chạy lại được nhiều lần mà không hỏng: nó nằm trong cron.
func TestTaoPhanManh_ChayLaiDuoc(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	if _, err := k.pool.Exec(ctx, `SELECT nhat_ky_tao_phan_manh()`); err != nil {
		t.Fatalf("lần 1: %s", err)
	}
	var lanHai int
	if err := k.pool.QueryRow(ctx, `SELECT nhat_ky_tao_phan_manh()`).Scan(&lanHai); err != nil {
		t.Fatalf("lần 2: %s", err)
	}
	if lanHai != 0 {
		t.Errorf("lần 2 tạo thêm %d phân mảnh, mong 0", lanHai)
	}
}
