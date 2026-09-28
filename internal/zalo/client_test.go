package zalo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vihat/vihat-miniapp/internal/secret"
)

// ĐỌC TRƯỚC KHI TIN NHỮNG PHÉP KIỂM NÀY:
//
// Máy chủ giả ở đây trả đúng hình dạng mà wire.go GIẢ ĐỊNH Zalo trả. Vì vậy các
// phép kiểm dưới đây chứng minh được đúng một điều: mã nguồn khớp với giả định
// của chính nó — gọi đúng đường dẫn, đặt đúng tên header, phân loại lỗi đúng,
// và không để dữ liệu cá nhân rò ra lỗi.
//
// Chúng KHÔNG chứng minh giả định đúng. Chỉ một lần thử trên máy thật mới làm
// được việc đó (xem mục NỢ trong README).

const (
	soGiaLap    = "84900000000" // số giả đã thống nhất: 0900000000
	tokenGiaLap = "access-token-gia-lap"
	phoneGiaLap = "phone-token-gia-lap"
	khoaGiaLap  = "secret-key-gia-lap"
)

func mayChuGia(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, secret.Secret(khoaGiaLap), VoiHTTPClient(srv.Client()))
}

func TestLaySoDienThoai_ThanhCong(t *testing.T) {
	var duongDan, method string
	var hdr http.Header
	var query string

	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		duongDan, method, hdr, query = r.URL.Path, r.Method, r.Header.Clone(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `"},"error":0,"message":"Success"}`))
	})

	so, err := c.LaySoDienThoai(context.Background(), tokenGiaLap, phoneGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if so != soGiaLap {
		t.Errorf("số = %q, mong số đã chuẩn hoá", so)
	}
	if method != http.MethodGet || duongDan != DuongDanLayThongTin {
		t.Errorf("gọi %s %s, mong GET %s", method, duongDan, DuongDanLayThongTin)
	}
	if hdr.Get(HeaderAccessToken) != tokenGiaLap ||
		hdr.Get(HeaderCode) != phoneGiaLap ||
		hdr.Get(HeaderSecretKey) != khoaGiaLap {
		t.Error("ba header của giao thức chưa được đặt đúng")
	}
	// Bí mật và token không bao giờ được nằm trong URL: URL đi vào access log,
	// vào proxy, vào Referer.
	if query != "" {
		t.Errorf("URL mang query %q — token/bí mật phải đi bằng header", query)
	}
	// Mặc định TẮT: xem ĐIỀU CHƯA RÕ #1.
	if hdr.Get(HeaderAppSecretProof) != "" {
		t.Error("appsecret_proof phải tắt mặc định khi chưa thử trên máy thật")
	}
}

func TestLaySoDienThoai_BatAppSecretProof(t *testing.T) {
	var hdr http.Header
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `"},"error":0}`))
	})
	c.GuiAppSecretProof = true

	if _, err := c.LaySoDienThoai(context.Background(), tokenGiaLap, phoneGiaLap); err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	mong := TinhAppSecretProof(tokenGiaLap, secret.Secret(khoaGiaLap))
	if got := hdr.Get(HeaderAppSecretProof); got != mong {
		t.Errorf("appsecret_proof = %q, mong %q", got, mong)
	}
}

func TestLaySoDienThoai_PhanLoaiLoi(t *testing.T) {
	cases := []struct {
		ten     string
		handler http.HandlerFunc
		mong    error
	}{
		{
			"error khác 0 -> token hỏng (401)",
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":{},"error":-201,"message":"Invalid code"}`))
			},
			ErrTokenKhongHopLe,
		},
		{
			"HTTP 500 -> không với tới Zalo (502)",
			func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			ErrKhongVoiToiZalo,
		},
		{
			"HTTP 403 -> không với tới Zalo (502), không đổ lỗi cho người dùng",
			func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
			ErrKhongVoiToiZalo,
		},
		{
			"body không phải JSON -> 502",
			func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>lỗi</html>")) },
			ErrKhongVoiToiZalo,
		},
		{
			"số rỗng dù error=0 -> 502, không đoán bừa",
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":{"number":""},"error":0}`))
			},
			ErrKhongVoiToiZalo,
		},
		{
			"số rác dù error=0 -> 502",
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":{"number":"khong-phai-so"},"error":0}`))
			},
			ErrKhongVoiToiZalo,
		},
	}

	for _, tc := range cases {
		t.Run(tc.ten, func(t *testing.T) {
			c := mayChuGia(t, tc.handler)
			_, err := c.LaySoDienThoai(context.Background(), tokenGiaLap, phoneGiaLap)
			if !errors.Is(err, tc.mong) {
				t.Fatalf("lỗi = %v, mong %v", err, tc.mong)
			}
		})
	}
}

func TestLaySoDienThoai_ThieuThamSo(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("không được gọi sang Zalo khi thiếu tham số")
	})
	if _, err := c.LaySoDienThoai(context.Background(), "", phoneGiaLap); !errors.Is(err, ErrTokenKhongHopLe) {
		t.Errorf("thiếu accessToken: lỗi = %v", err)
	}
	if _, err := c.LaySoDienThoai(context.Background(), tokenGiaLap, ""); !errors.Is(err, ErrTokenKhongHopLe) {
		t.Errorf("thiếu phoneToken: lỗi = %v", err)
	}
}

// Nghị định 13/2023: lỗi đi thẳng vào log, nên lỗi không được mang số điện thoại,
// không mang token, không mang secret key.
func TestLoi_KhongMangDuLieuCaNhanHayBiMat(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		// Zalo trả số hợp lệ nhưng sai định dạng -> lỗi được dựng TỪ body.
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `123456789012"},"error":0,"message":"` + soGiaLap + `"}`))
	})
	_, err := c.LaySoDienThoai(context.Background(), tokenGiaLap, phoneGiaLap)
	if err == nil {
		t.Fatal("mong có lỗi")
	}
	for _, cam := range []string{soGiaLap, tokenGiaLap, phoneGiaLap, khoaGiaLap} {
		if strings.Contains(err.Error(), cam) {
			t.Errorf("thông điệp lỗi rò %q: %s", cam, err)
		}
	}
}

func TestChuanHoaSo(t *testing.T) {
	cases := []struct {
		vao, ra string
		loi     bool
	}{
		{"84900000000", "84900000000", false},
		{"+84900000000", "84900000000", false},
		{"0900000000", "84900000000", false},
		{" 84 900 000 000 ", "84900000000", false},
		{"84-900-000-000", "84900000000", false},
		{"", "", true},
		{"khong-phai-so", "", true},
		{"8490000000000000000000", "", true},
		{"8490", "", true},
	}
	for _, tc := range cases {
		ra, err := ChuanHoaSo(tc.vao)
		if tc.loi {
			if err == nil {
				t.Errorf("ChuanHoaSo(%q) mong lỗi, nhận %q", tc.vao, ra)
			}
			continue
		}
		if err != nil || ra != tc.ra {
			t.Errorf("ChuanHoaSo(%q) = %q, %v; mong %q, nil", tc.vao, ra, err, tc.ra)
		}
	}
}

// ---------------------------------------------------------------------------
// LayViTri — đổi token của getLocation() lấy toạ độ.
//
// Cùng lời cảnh báo ở đầu tệp, nặng hơn một bậc: hình dạng "data" của lượt đổi
// vị trí CHƯA từng được quan sát trực tiếp (wire.go, MỨC CHỨNG CỨ: THẤP).
// ---------------------------------------------------------------------------

const (
	viTriGiaLap = "location-token-gia-lap"
	// Toạ độ giả — giữa Biển Đông, không trỏ vào nhà ai.
	viDoGia   = "12.3456789"
	kinhDoGia = "112.3456789"
)

func TestLayViTri_ThanhCong_CungMotLoiGoiVoiSoDienThoai(t *testing.T) {
	var duongDan, method, query string
	var hdr http.Header
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		duongDan, method, hdr, query = r.URL.Path, r.Method, r.Header.Clone(), r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":{"latitude":"` + viDoGia + `","longitude":"` + kinhDoGia + `"},"error":0,"message":"Success"}`))
	})

	viDo, kinhDo, err := c.LayViTri(context.Background(), tokenGiaLap, viTriGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if viDo != 12.3456789 || kinhDo != 112.3456789 {
		t.Errorf("toạ độ = (%v, %v), mong (12.3456789, 112.3456789)", viDo, kinhDo)
	}
	if method != http.MethodGet || duongDan != DuongDanLayThongTin {
		t.Errorf("gọi %s %s, mong GET %s", method, duongDan, DuongDanLayThongTin)
	}
	if hdr.Get(HeaderAccessToken) != tokenGiaLap ||
		hdr.Get(HeaderCode) != viTriGiaLap ||
		hdr.Get(HeaderSecretKey) != khoaGiaLap {
		t.Error("ba header của giao thức chưa được đặt đúng")
	}
	if query != "" {
		t.Errorf("URL mang query %q — token/bí mật phải đi bằng header", query)
	}
}

// Bản tham chiếu đọc được cả chuỗi lẫn số; ở đây cũng vậy.
func TestLayViTri_ChapNhanSoJSON(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"latitude":-12.5,"longitude":-100},"error":0}`))
	})
	viDo, kinhDo, err := c.LayViTri(context.Background(), tokenGiaLap, viTriGiaLap)
	if err != nil || viDo != -12.5 || kinhDo != -100 {
		t.Fatalf("= (%v, %v, %v), mong (-12.5, -100, nil)", viDo, kinhDo, err)
	}
}

func TestLayViTri_PhanLoaiLoi(t *testing.T) {
	cases := []struct {
		ten  string
		than string
		ma   int
		mong error
	}{
		{"error khác 0", `{"data":{},"error":-201,"message":"Invalid code"}`, 200, ErrTokenKhongHopLe},
		{"HTTP 500", ``, 500, ErrKhongVoiToiZalo},
		{"body không phải JSON", `<html>lỗi</html>`, 200, ErrKhongVoiToiZalo},
		{"không có data", `{"error":0}`, 200, ErrKhongVoiToiZalo},
		{"data null", `{"data":null,"error":0}`, 200, ErrKhongVoiToiZalo},
		{"thiếu kinh độ", `{"data":{"latitude":"10.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"vĩ độ rỗng", `{"data":{"latitude":"","longitude":"106.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"vĩ độ rác", `{"data":{"latitude":"abc","longitude":"106.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"vĩ độ NaN", `{"data":{"latitude":"NaN","longitude":"106.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"vĩ độ ngoài trái đất", `{"data":{"latitude":"91","longitude":"106.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"kinh độ ngoài trái đất", `{"data":{"latitude":"10","longitude":"-180.5"},"error":0}`, 200, ErrKhongVoiToiZalo},
		{"toạ độ là object", `{"data":{"latitude":{},"longitude":"106.1"},"error":0}`, 200, ErrKhongVoiToiZalo},
	}
	for _, tc := range cases {
		t.Run(tc.ten, func(t *testing.T) {
			c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.ma)
				_, _ = w.Write([]byte(tc.than))
			})
			_, _, err := c.LayViTri(context.Background(), tokenGiaLap, viTriGiaLap)
			if !errors.Is(err, tc.mong) {
				t.Fatalf("lỗi = %v, mong %v", err, tc.mong)
			}
		})
	}
}

func TestLayViTri_ThieuThamSo_KhongGoiZalo(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("không được gọi sang Zalo khi thiếu tham số")
	})
	if _, _, err := c.LayViTri(context.Background(), "", viTriGiaLap); !errors.Is(err, ErrTokenKhongHopLe) {
		t.Errorf("thiếu accessToken: lỗi = %v", err)
	}
	if _, _, err := c.LayViTri(context.Background(), tokenGiaLap, ""); !errors.Is(err, ErrTokenKhongHopLe) {
		t.Errorf("thiếu token vị trí: lỗi = %v", err)
	}
}

// Toạ độ là vị trí của một người: lỗi đi vào log nên lỗi không được mang toạ
// độ, token hay secret key — kể cả khi chính toạ độ là thứ làm hỏng.
func TestLayViTri_LoiKhongMangToaDoHayBiMat(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"latitude":"` + viDoGia + `","longitude":"999` + kinhDoGia + `"},"error":0,"message":"` + viDoGia + `"}`))
	})
	_, _, err := c.LayViTri(context.Background(), tokenGiaLap, viTriGiaLap)
	if err == nil {
		t.Fatal("mong có lỗi")
	}
	for _, cam := range []string{viDoGia, kinhDoGia, tokenGiaLap, viTriGiaLap, khoaGiaLap} {
		if strings.Contains(err.Error(), cam) {
			t.Errorf("thông điệp lỗi rò %q: %s", cam, err)
		}
	}
}
