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
