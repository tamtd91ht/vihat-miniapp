package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// LỚP KHIẾM KHUYẾT TỆP NÀY BẮT: thân sự kiện mang số điện thoại. Ba cách nó đi
// sai chỗ mà mọi ca khác vẫn xanh — đi qua http, đi theo một chuyển hướng, hoặc
// chui vào thông điệp lỗi (rồi vào log). Và một chữ ký tính trên thứ khác với
// thân thô là một chữ ký bên nhận không bao giờ kiểm khớp.

const khoaGia = "khoa-ky-webhook-gia-lap-test-032"

func suKienGia() yeucau.SuKienYeuCau {
	return yeucau.SuKienYeuCau{
		MaYeuCau:    "3f1c0c9e-0000-4000-8000-0000000000c1",
		Kind:        yeucau.KindHuyUuDaiSMS,
		TaoLuc:      time.Date(2026, 10, 7, 3, 4, 5, 0, time.UTC),
		SoDienThoai: "84900000000",
		GhiChu:      "ghi-chu-KHONG-DUOC-VAO-LOI",
	}
}

func TestMoi_ChiNhanHTTPSDayDu(t *testing.T) {
	for _, u := range []string{
		"",
		"http://nhan.vihat.vn/su-kien",
		"ftp://nhan.vihat.vn",
		"https://",
		"/duong-dan-tuong-doi",
		"https://ai:matkhau@nhan.vihat.vn/x",
	} {
		if Moi(u, secret.Secret(khoaGia)) != nil {
			t.Errorf("Moi(%q) phải trả nil", u)
		}
	}
	if Moi("https://nhan.vihat.vn/su-kien", "") != nil {
		t.Error("thiếu khoá mà vẫn dựng được bộ báo")
	}
	if Moi("HTTPS://nhan.vihat.vn/su-kien", secret.Secret(khoaGia)) == nil {
		t.Error("scheme viết hoa vẫn là https")
	}
}

func TestGui_ChuKyLaHMACCuaThanTho(t *testing.T) {
	var than []byte
	var chuKy, loaiNoiDung string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		than, _ = io.ReadAll(r.Body)
		chuKy = r.Header.Get(HeaderChuKy)
		loaiNoiDung = r.Header.Get("Content-Type")
	}))
	defer srv.Close()

	c := Moi(srv.URL, secret.Secret(khoaGia), VoiHTTPClient(srv.Client()))
	if err := c.Gui(context.Background(), suKienGia()); err != nil {
		t.Fatalf("Gui: %s", err)
	}

	// Tính lại ĐỘC LẬP, không qua KyThan: một KyThan sai sẽ khớp với chính nó.
	m := hmac.New(sha256.New, []byte(khoaGia))
	m.Write(than)
	if mong := "sha256=" + hex.EncodeToString(m.Sum(nil)); chuKy != mong {
		t.Fatalf("chữ ký %q, mong %q", chuKy, mong)
	}
	if loaiNoiDung != "application/json" {
		t.Errorf("Content-Type %q", loaiNoiDung)
	}
	s := string(than)
	for _, can := range []string{
		`"event":"request.created"`, `"kind":"sms_optout"`, `"createdAt":"2026-10-07T03:04:05Z"`,
		`"phone":"84900000000"`, `"interests":[]`,
	} {
		if !strings.Contains(s, can) {
			t.Errorf("thân thiếu %s: %s", can, s)
		}
	}
	if strings.Contains(s, "displayName") {
		t.Error("displayName rỗng phải vắng mặt")
	}
}

// Một 302 sang nơi khác KHÔNG được theo: thân có số điện thoại.
func TestGui_KhongTheoChuyenHuong(t *testing.T) {
	var dichBiGoi atomic.Int32
	dich := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		dichBiGoi.Add(1)
	}))
	defer dich.Close()
	nguon := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dich.URL, http.StatusTemporaryRedirect)
	}))
	defer nguon.Close()

	c := Moi(nguon.URL, secret.Secret(khoaGia), VoiHTTPClient(nguon.Client()), VoiNghiGiuaHaiLuot(0))
	if err := c.Gui(context.Background(), suKienGia()); err == nil {
		t.Fatal("307 phải là lỗi, không phải thành công")
	}
	if dichBiGoi.Load() != 0 {
		t.Fatal("đã đi theo chuyển hướng — thân có số điện thoại tới một đích không ai cấu hình")
	}
}

func TestGui_ThuLaiDungMotLanKhi5xxRoiThanhCong(t *testing.T) {
	var dem atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if dem.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	c := Moi(srv.URL, secret.Secret(khoaGia), VoiHTTPClient(srv.Client()), VoiNghiGiuaHaiLuot(0))
	if err := c.Gui(context.Background(), suKienGia()); err != nil {
		t.Fatalf("lượt hai thành công mà vẫn báo lỗi: %s", err)
	}
	if dem.Load() != 2 {
		t.Fatalf("%d lượt, mong 2", dem.Load())
	}
}

func TestGui_4xxKhongThuLaiVaLoiKhongMangThan(t *testing.T) {
	var dem atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dem.Add(1)
		// Bên nhận VỌNG LẠI thân trong phản hồi lỗi — chuyện thường gặp.
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := Moi(srv.URL, secret.Secret(khoaGia), VoiHTTPClient(srv.Client()), VoiNghiGiuaHaiLuot(0))
	err := c.Gui(context.Background(), suKienGia())
	if err == nil {
		t.Fatal("400 phải là lỗi")
	}
	if dem.Load() != 1 {
		t.Fatalf("400 bị thử lại: %d lượt", dem.Load())
	}
	for _, cam := range []string{"84900000000", "ghi-chu-KHONG-DUOC-VAO-LOI", khoaGia} {
		if strings.Contains(err.Error(), cam) {
			t.Fatalf("lỗi mang %q: %s", cam, err)
		}
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("lỗi phải nêu mã HTTP: %s", err)
	}
}

// TLS được kiểm: client mặc định không tin CA tự ký của máy chủ test.
func TestGui_KiemChungChiTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	c := Moi(srv.URL, secret.Secret(khoaGia), VoiNghiGiuaHaiLuot(0))
	if err := c.Gui(context.Background(), suKienGia()); err == nil {
		t.Fatal("chứng chỉ không tin cậy mà vẫn gửi được — TLS không được kiểm")
	}
}
