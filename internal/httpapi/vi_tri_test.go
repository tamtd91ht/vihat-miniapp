package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// Toạ độ giả — giữa Biển Đông, không trỏ vào nhà ai.
const (
	viDoGia   = 12.3456789
	kinhDoGia = 112.3456789

	atGia = "at-gia-lap"
	ltGia = "lt-gia-lap"
)

const thanViTriHopLe = `{"accessToken":"` + atGia + `","locationToken":"` + ltGia + `"}`

type viTriGia struct {
	loi       error
	goi       int
	atNhan    string
	tokenNhan string
}

func (v *viTriGia) LayViTri(_ context.Context, at, lt string) (float64, float64, error) {
	v.goi++
	v.atNhan, v.tokenNhan = at, lt
	if v.loi != nil {
		return 0, 0, v.loi
	}
	return viDoGia, kinhDoGia, nil
}

func dungServerViTri(t *testing.T, z DoiViTriZalo) (*Server, *bytes.Buffer) {
	t.Helper()
	s, log := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	if z != nil {
		s.VoiViTri(z)
	}
	return s, log
}

func goiViTri(t *testing.T, s *Server, than string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/location", strings.NewReader(than))
	r.RemoteAddr = "203.0.113.7:51000" // TEST-NET-3, RFC 5737
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func docLoiCoMa(t *testing.T, w *httptest.ResponseRecorder) phanHoiLoiCoMa {
	t.Helper()
	var ph phanHoiLoiCoMa
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân lỗi không phải JSON: %s", err)
	}
	if ph.Message == "" {
		t.Error("thiếu khoá message — khoá phía Mini App đọc")
	}
	return ph
}

func TestViTri_200(t *testing.T) {
	z := &viTriGia{}
	s, _ := dungServerViTri(t, z)

	w := goiViTri(t, s, thanViTriHopLe)

	if w.Code != http.StatusOK {
		t.Fatalf("mã = %d, mong 200; thân = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, mong no-store: toạ độ là vị trí của một người", got)
	}
	var ph phanHoiViTri
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân không phải JSON: %s", err)
	}
	if ph.Latitude != viDoGia || ph.Longitude != kinhDoGia {
		t.Errorf("toạ độ = (%v, %v), mong (%v, %v)", ph.Latitude, ph.Longitude, viDoGia, kinhDoGia)
	}
	if z.atNhan != atGia || z.tokenNhan != ltGia {
		t.Error("token chuyển sang Zalo không đúng nguyên văn")
	}
	// Hợp đồng khoá: đúng hai khoá latitude/longitude.
	var tho map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &tho)
	if len(tho) != 2 || tho["latitude"] == nil || tho["longitude"] == nil {
		t.Errorf("thân = %s, mong đúng hai khoá latitude/longitude", w.Body.String())
	}
}

func TestViTri_502_MotMaChoMoiThatBaiCuaZalo(t *testing.T) {
	for ten, loi := range map[string]error{
		"Zalo từ chối token": zalo.ErrTokenKhongHopLe,
		"không với tới Zalo": zalo.ErrKhongVoiToiZalo,
	} {
		t.Run(ten, func(t *testing.T) {
			s, _ := dungServerViTri(t, &viTriGia{loi: loi})
			w := goiViTri(t, s, thanViTriHopLe)
			if w.Code != http.StatusBadGateway {
				t.Fatalf("mã = %d, mong 502 — 401 ở đây bị phía app đọc thành 'đăng nhập lại'", w.Code)
			}
			ph := docLoiCoMa(t, w)
			if ph.Code != "zalo_location_unavailable" {
				t.Errorf("code = %q, mong zalo_location_unavailable", ph.Code)
			}
			if strings.Contains(ph.Message, "đăng nhập lại") {
				t.Errorf("bảo người dùng đăng nhập lại cho một lần lấy vị trí hỏng: %s", ph.Message)
			}
		})
	}
}

func TestViTri_400_ThanHongHoacThieuTruong(t *testing.T) {
	cases := map[string]string{
		"không phải JSON":     `khong-phai-json`,
		"thiếu locationToken": `{"accessToken":"at-gia-lap"}`,
		"thiếu accessToken":   `{"locationToken":"lt-gia-lap"}`,
		"rỗng":                `{}`,
		"tên khoá kiểu cũ":    `{"access_token":"at-gia-lap","code":"lt-gia-lap"}`,
	}
	for ten, than := range cases {
		t.Run(ten, func(t *testing.T) {
			z := &viTriGia{}
			s, _ := dungServerViTri(t, z)
			w := goiViTri(t, s, than)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("mã = %d, mong 400", w.Code)
			}
			if ph := docLoiCoMa(t, w); ph.Code != "invalid_request" {
				t.Errorf("code = %q, mong invalid_request", ph.Code)
			}
			if z.goi != 0 {
				t.Error("không được gọi sang Zalo khi yêu cầu đã sai từ đầu")
			}
		})
	}
}

func TestViTri_405_KhiKhongPhaiPOST(t *testing.T) {
	z := &viTriGia{}
	s, _ := dungServerViTri(t, z)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/location", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mã = %d, mong 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, mong POST", got)
	}
	if ph := docLoiCoMa(t, w); ph.Code != "method_not_allowed" {
		t.Errorf("code = %q", ph.Code)
	}
	if z.goi != 0 {
		t.Error("GET không được chạm Zalo")
	}
}

func TestViTri_429_XoRiengKhongAnHanMucDangNhap(t *testing.T) {
	z := &viTriGia{}
	s, _ := dungServerViTri(t, z)

	for i := 0; i < SoLuotViTriToiDa; i++ {
		if w := goiViTri(t, s, thanViTriHopLe); w.Code != http.StatusOK {
			t.Fatalf("lượt %d: mã = %d, mong 200", i+1, w.Code)
		}
	}
	w := goiViTri(t, s, thanViTriHopLe)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("lượt vượt trần: mã = %d, mong 429", w.Code)
	}
	if ph := docLoiCoMa(t, w); ph.Code != "rate_limited" {
		t.Errorf("code = %q, mong rate_limited", ph.Code)
	}
	if z.goi != SoLuotViTriToiDa {
		t.Errorf("gọi Zalo %d lần, mong %d — lượt bị chặn không được chạm Zalo", z.goi, SoLuotViTriToiDa)
	}
	// Cùng IP vẫn đăng nhập được: hai xô tách nhau.
	if w := goiDangNhap(t, s, thanHopLe); w.Code != http.StatusCreated {
		t.Errorf("đăng nhập sau khi hết hạn mức vị trí: mã = %d, mong 201", w.Code)
	}
}

func TestViTri_503_KhiChuaLapRap(t *testing.T) {
	s, _ := dungServerViTri(t, nil)
	w := goiViTri(t, s, thanViTriHopLe)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã = %d, mong 503 — 404 bị phía app đọc thành 'sai đường dẫn'", w.Code)
	}
	if ph := docLoiCoMa(t, w); ph.Code != "unavailable" {
		t.Errorf("code = %q", ph.Code)
	}
}

func TestViTri_CORS(t *testing.T) {
	s, _ := dungServerViTri(t, &viTriGia{})

	r := httptest.NewRequest(http.MethodOptions, "/api/v1/location", nil)
	r.Header.Set("Origin", "https://h5.zdn.vn")
	r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: mã = %d, mong 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://h5.zdn.vn" {
		t.Errorf("preflight Allow-Origin = %q", got)
	}

	r = httptest.NewRequest(http.MethodPost, "/api/v1/location", strings.NewReader(thanViTriHopLe))
	r.Header.Set("Origin", "https://h5.zdn.vn")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://h5.zdn.vn" {
		t.Errorf("POST Allow-Origin = %q — thiếu thì webview chặn phản hồi, nút chết im lặng", got)
	}
}

// Đi QUA client Zalo thật tới một máy chủ Zalo giả: log ở mức Debug không được
// mang token, secret key hay toạ độ — ở ca thành công lẫn ca hỏng.
func TestViTri_KhongLogTokenHayToaDo(t *testing.T) {
	const khoa = "secret-key-gia-lap"
	cases := map[string]string{
		"thành công":            `{"data":{"latitude":"12.3456789","longitude":"112.3456789"},"error":0,"message":"Success"}`,
		"Zalo báo lỗi":          `{"data":{},"error":-201,"message":"Invalid code ` + ltGia + ` 12.3456789"}`,
		"toạ độ ngoài trái đất": `{"data":{"latitude":"12.3456789","longitude":"999.3456789"},"error":0}`,
	}
	for ten, thanZalo := range cases {
		t.Run(ten, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(thanZalo))
			}))
			t.Cleanup(srv.Close)
			c := zalo.New(srv.URL, secret.Secret(khoa), zalo.VoiHTTPClient(srv.Client()))
			s, log := dungServerViTri(t, c)

			w := goiViTri(t, s, thanViTriHopLe)
			if w.Code != http.StatusOK && w.Code != http.StatusBadGateway {
				t.Fatalf("mã = %d", w.Code)
			}
			for _, cam := range []string{atGia, ltGia, khoa, "12.3456789", "112.3456789", "999.3456789"} {
				if strings.Contains(log.String(), cam) {
					t.Errorf("log chứa %q: %s", cam, log.String())
				}
			}
			if w.Code != http.StatusOK {
				for _, cam := range []string{atGia, ltGia, khoa, "12.3456789", "Invalid code"} {
					if strings.Contains(w.Body.String(), cam) {
						t.Errorf("thân lỗi chứa %q: %s", cam, w.Body.String())
					}
				}
			}
		})
	}
}
