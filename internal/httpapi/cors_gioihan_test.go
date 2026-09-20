package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCORS(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia}) // danh sách: https://h5.zdn.vn

	t.Run("origin được phép thì có header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.Header.Set("Origin", "https://h5.zdn.vn")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://h5.zdn.vn" {
			t.Errorf("Allow-Origin = %q", got)
		}
		if !strings.Contains(w.Header().Get("Vary"), "Origin") {
			t.Error("thiếu Vary: Origin — proxy có thể phục vụ lại phản hồi gắn origin của người khác")
		}
	})

	t.Run("origin lạ thì không có header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.Header.Set("Origin", "https://ke-tan-cong.example")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, mong rỗng", got)
		}
	})

	t.Run("preflight trả 204 và không lộ danh sách", func(t *testing.T) {
		for _, origin := range []string{"https://h5.zdn.vn", "https://ke-tan-cong.example"} {
			r := httptest.NewRequest(http.MethodOptions, "/api/v1/sessions", nil)
			r.Header.Set("Origin", origin)
			r.Header.Set("Access-Control-Request-Method", http.MethodPost)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)

			if w.Code != http.StatusNoContent {
				t.Errorf("origin %s: mã = %d, mong 204 cho mọi origin — 403 biến CORS thành kênh dò", origin, w.Code)
			}
		}
	})

	t.Run("không có Origin thì đi tiếp bình thường", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("mã = %d — CORS là cơ chế của trình duyệt, không phải xác thực", w.Code)
		}
	})

	t.Run("không bao giờ sao, không bao giờ credentials", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(thanHopLe))
		r.Header.Set("Origin", "https://h5.zdn.vn")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)

		if w.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Error(`Allow-Origin = "*"`)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Errorf("Allow-Credentials = %q — API dùng bearer token, bật cái này chỉ mở thêm bề mặt CSRF", got)
		}
	})
}

func TestGioiHanIP(t *testing.T) {
	dongHo := time.Now()
	g := MoiGioiHan(3, time.Minute)
	g.now = func() time.Time { return dongHo }

	for i := 0; i < 3; i++ {
		if ok, _ := g.Cho("a"); !ok {
			t.Fatalf("lượt %d phải được cho đi", i+1)
		}
	}

	ok, lanDau := g.Cho("a")
	if ok || !lanDau {
		t.Fatalf("lượt thứ 4 phải bị chặn và là lần từ chối đầu tiên (ok=%v lanDau=%v)", ok, lanDau)
	}
	if _, lanDau := g.Cho("a"); lanDau {
		t.Error("lần từ chối thứ hai không còn là lần đầu — nếu không thì mỗi lượt bị chặn lại ghi một dòng CSDL")
	}

	// Xô của IP khác không bị ảnh hưởng.
	if ok, _ := g.Cho("b"); !ok {
		t.Error("IP khác phải có xô riêng")
	}

	// Nạp lại theo thời gian: sau 1/3 cửa sổ thì có lại 1 lượt.
	dongHo = dongHo.Add(21 * time.Second)
	if ok, _ := g.Cho("a"); !ok {
		t.Error("sau khi nạp lại phải cho đi tiếp")
	}
	if ok, lanDau := g.Cho("a"); ok || !lanDau {
		t.Error("hết lượt nạp thì lại bị chặn, và đó là lần đầu của đợt mới")
	}
}

// Xô của IP lâu không dùng phải bị dọn, nếu không map lớn dần theo số IP từng
// ghé qua — một cách rò bộ nhớ chỉ máy chủ thật, sau vài tuần, mới cho thấy.
func TestGioiHanIP_DonXoCu(t *testing.T) {
	dongHo := time.Now()
	g := MoiGioiHan(2, time.Minute)
	g.now = func() time.Time { return dongHo }

	g.Cho("cu-1")
	g.Cho("cu-2")
	dongHo = dongHo.Add(5 * time.Minute)
	g.Cho("moi")

	g.mu.Lock()
	con := len(g.xo)
	g.mu.Unlock()
	if con != 1 {
		t.Errorf("còn %d xô, mong 1 — xô cũ chưa được dọn", con)
	}
}
