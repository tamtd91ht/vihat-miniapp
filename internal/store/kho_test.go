package store

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
)

// Test trong tệp này CHẠM CSDL THẬT. Không có TEST_DATABASE_DSN thì SKIP — và
// phải NHÌN THẤY là đã skip.
//
// Vì sao không mock: thứ đáng kiểm ở gói này nằm trong CSDL chứ không nằm trong
// Go — ràng buộc UNIQUE, CHECK, trigger chặn sửa nhật ký, và việc ba lần ghi
// cùng vào một giao dịch. Một mock chỉ diễn lại niềm tin của chính tôi rồi in
// "ok", đúng kiểu phép kiểm xanh vì sai lý do.
//
// Chạy:
//
//	TEST_DATABASE_DSN='<dsn>' go test ./internal/store
//
// CSDL đó phải đã chạy migrations/0001_init.sql, và phải là CSDL DÙNG RIÊNG cho
// test: các ca dưới đây có ghi dữ liệu.
func moKhoTest(t *testing.T) *Kho {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("thiếu TEST_DATABASE_DSN — test chạm CSDL không chạy (xem README mục Chạy test)")
	}
	ctx := context.Background()
	k, err := Mo(ctx, dsn)
	if err != nil {
		t.Fatalf("mở kho: %s", err)
	}
	t.Cleanup(k.Dong)
	return k
}

// soTest sinh số giả riêng cho mỗi lần chạy, dựa trên số giả đã thống nhất
// 0900000000 -> 84900000000, để hai lần chạy không đụng ràng buộc UNIQUE.
func soTest(t *testing.T) string {
	t.Helper()
	return "84900" + time.Now().Format("000000") + "0"
}

func ipTest() *netip.Addr {
	a := netip.MustParseAddr("203.0.113.7") // TEST-NET-3, RFC 5737 — không phải IP thật
	return &a
}

func TestTaoPhienDangNhap_GhiCaBaBangTrongMotGiaoDich(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t)

	tokenBam := phien.Bam("token-mau-1-" + so)
	hetHan := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Millisecond)

	kq, err := k.TaoPhienDangNhap(ctx, so, tokenBam, hetHan, ipTest())
	if err != nil {
		t.Fatalf("tạo phiên: %s", err)
	}
	if kq.NguoiDungID == "" || kq.PhienID == "" {
		t.Fatal("thiếu mã định danh trả về")
	}
	if kq.HetHanLuc.IsZero() {
		t.Fatal("thiếu het_han_luc trả về")
	}

	// Nhật ký phải có đúng một dòng thành công, gắn đúng phiên vừa mở.
	var demNhatKy int
	err = k.pool.QueryRow(ctx,
		`SELECT count(*) FROM nhat_ky_dang_nhap WHERE phien_id = $1 AND ket_qua = $2`,
		kq.PhienID, phien.KetQuaThanhCong).Scan(&demNhatKy)
	if err != nil {
		t.Fatalf("đếm nhật ký: %s", err)
	}
	if demNhatKy != 1 {
		t.Fatalf("có %d dòng nhật ký cho phiên này, mong đúng 1", demNhatKy)
	}

	// Nhật ký KHÔNG được chứa số điện thoại ở bất kỳ cột nào.
	var coSo bool
	err = k.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM nhat_ky_dang_nhap
		                WHERE phien_id = $1 AND (coalesce(ly_do,'') LIKE '%' || $2 || '%'))`,
		kq.PhienID, so).Scan(&coSo)
	if err != nil {
		t.Fatalf("soi nhật ký: %s", err)
	}
	if coSo {
		t.Error("nhật ký đăng nhập chứa số điện thoại — vi phạm Nghị định 13")
	}

	// Phiên lưu BẢN BĂM, không lưu token.
	var bamLuu []byte
	if err := k.pool.QueryRow(ctx, `SELECT token_bam FROM phien WHERE id = $1`, kq.PhienID).Scan(&bamLuu); err != nil {
		t.Fatalf("đọc phiên: %s", err)
	}
	if string(bamLuu) != string(tokenBam) {
		t.Error("token_bam lưu sai")
	}
}

func TestTaoPhienDangNhap_DangNhapLaiDungMotNguoiDung(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t)
	hetHan := time.Now().Add(time.Hour)

	mot, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-mau-a-"+so), hetHan, nil)
	if err != nil {
		t.Fatalf("lần 1: %s", err)
	}
	hai, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-mau-b-"+so), hetHan, nil)
	if err != nil {
		t.Fatalf("lần 2: %s", err)
	}
	if mot.NguoiDungID != hai.NguoiDungID {
		t.Error("cùng một số điện thoại phải cho cùng một nguoi_dung_id")
	}
	if mot.PhienID == hai.PhienID {
		t.Error("hai lần đăng nhập phải là hai phiên khác nhau")
	}
}

// Ghi hỏng thì KHÔNG được để lại nửa việc: token_bam sai độ dài vi phạm CHECK,
// và khi đó người dùng lẫn nhật ký đều không được tạo.
func TestTaoPhienDangNhap_HongThiKhongDeLaiNuaViec(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t) + "1"

	_, err := k.TaoPhienDangNhap(ctx, so, []byte("qua-ngan"), time.Now().Add(time.Hour), nil)
	if err == nil {
		t.Fatal("token_bam sai độ dài mà vẫn ghi được — ràng buộc CHECK không chạy")
	}

	var con bool
	if err := k.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM nguoi_dung WHERE so_dien_thoai = $1)`, so).Scan(&con); err != nil {
		t.Fatalf("soi nguoi_dung: %s", err)
	}
	if con {
		t.Error("giao dịch hỏng nhưng nguoi_dung vẫn được tạo — ba lần ghi không cùng một giao dịch")
	}
}

func TestGhiNhatKyThatBai_KhongCanBietLaAi(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()

	if err := k.GhiNhatKyThatBai(ctx, phien.KetQuaTokenZaloHong, "token_zalo_tu_choi", ipTest()); err != nil {
		t.Fatalf("ghi nhật ký thất bại: %s", err)
	}
}

// Nhật ký chỉ ghi thêm — cưỡng chế bằng trigger, không bằng lời hứa.
func TestNhatKy_KhongSuaKhongXoaDuoc(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	so := soTest(t) + "2"

	kq, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("token-mau-c-"+so), time.Now().Add(time.Hour), nil)
	if err != nil {
		t.Fatalf("tạo phiên: %s", err)
	}

	if _, err := k.pool.Exec(ctx,
		`UPDATE nhat_ky_dang_nhap SET ket_qua = $1 WHERE phien_id = $2`,
		phien.KetQuaLoiHeThong, kq.PhienID); err == nil {
		t.Error("UPDATE trên nhat_ky_dang_nhap phải bị trigger từ chối")
	}
	if _, err := k.pool.Exec(ctx,
		`DELETE FROM nhat_ky_dang_nhap WHERE phien_id = $1`, kq.PhienID); err == nil {
		t.Error("DELETE trên nhat_ky_dang_nhap phải bị trigger từ chối")
	}
}
