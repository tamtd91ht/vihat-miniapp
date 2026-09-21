package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Ba ca dưới đây canh đúng ba điều làm nên endpoint này, và cả ba đều là những điều một bản
// "sửa cho tử tế hơn" sẽ phá mà không ai thấy: thêm `switch` theo phương thức, đọc thân để
// ghi log, trả 400 khi JSON hỏng. Mỗi thứ đều biến webhook thành endpoint Zalo sẽ TẮT sau
// vài lần thử lại — và lúc ấy không có gì báo cho ai, webhook chỉ lặng đi.

// dungServerWebhook dựng Server với hai phụ thuộc rỗng: bộ nhận webhook không chạm kho và
// không gọi Zalo, nên nếu một bản sửa sau này khiến nó chạm tới, ca test đổ vì nil chứ không
// lặng lẽ đi qua.
func dungServerWebhook(t *testing.T) http.Handler {
	t.Helper()
	s, _ := dungServer(t, nil, nil)
	return s.Handler()
}

func TestWebhookZaloLuonTraVe200(t *testing.T) {
	h := dungServerWebhook(t)

	ca := []struct {
		ten    string
		phuong string
		than   string
	}{
		// Console có thể thăm dò bằng GET lúc lưu URL. Một 405 ở đây là URL bị từ chối.
		{"GET thăm dò lúc lưu URL", http.MethodGet, ""},
		{"POST sự kiện bình thường", http.MethodPost, `{"event_name":"test"}`},
		{"POST thân rỗng", http.MethodPost, ""},
		{"POST thân KHÔNG phải JSON", http.MethodPost, "khong-phai-json{{{"},
		{"HEAD", http.MethodHead, ""},
		{"PUT — phương thức Zalo không dùng, vẫn không được từ chối", http.MethodPut, "{}"},
	}

	for _, c := range ca {
		t.Run(c.ten, func(t *testing.T) {
			r := httptest.NewRequest(c.phuong, DuongDanWebhookZalo, strings.NewReader(c.than))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("mã trả về = %d, muốn 200. Zalo tắt webhook cứ trả mã lỗi", w.Code)
			}
		})
	}
}

// thanDemDoc đếm số byte đã bị đọc khỏi thân yêu cầu.
//
// Đây là phép đo THẬT chứ không phải đọc mã: một bản sửa gọi io.ReadAll(r.Body) rồi vứt kết
// quả đi trông vô hại trong diff, nhưng nó là bước đầu tiên của đường đưa định danh người
// dùng vào log.
type thanDemDoc struct {
	r     io.Reader
	daDoc int
	daGoi int
}

func (t *thanDemDoc) Read(p []byte) (int, error) {
	t.daGoi++
	n, err := t.r.Read(p)
	t.daDoc += n
	return n, err
}

func (t *thanDemDoc) Close() error { return nil }

func TestWebhookZaloKhongDocThan(t *testing.T) {
	h := dungServerWebhook(t)

	than := &thanDemDoc{r: strings.NewReader(`{"user_id_by_app":"khong-duoc-doc","event_name":"test"}`)}
	r := httptest.NewRequest(http.MethodPost, DuongDanWebhookZalo, nil)
	r.Body = than

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if than.daGoi != 0 || than.daDoc != 0 {
		t.Fatalf("thân yêu cầu đã bị đọc (%d lượt, %d byte) — payload của Zalo mang định danh "+
			"người dùng, và thứ đã đọc là thứ sẽ có ngày bị ghi log", than.daGoi, than.daDoc)
	}
}

// Đường dẫn không được nằm dưới `/api/v1`: tiền tố ấy là bề mặt Mini App gọi lên, nơi mọi
// tuyến đều nói về MỘT người dùng đang mở app. Webhook không có người dùng nào.
func TestDuongDanWebhookNamNgoaiBeMatNguoiDung(t *testing.T) {
	if strings.HasPrefix(DuongDanWebhookZalo, "/api/") {
		t.Fatalf("đường dẫn %q nằm dưới /api/ — xem chú thích ở webhook_zalo.go", DuongDanWebhookZalo)
	}
	if !strings.HasPrefix(DuongDanWebhookZalo, "/") {
		t.Fatalf("đường dẫn %q phải bắt đầu bằng /", DuongDanWebhookZalo)
	}
}

// Bộ giới hạn theo IP bảo vệ tuyến đăng nhập khỏi người dò số điện thoại. Mắc nó vào webhook
// thì một cơn bão sự kiện thật từ Zalo sẽ nhận một chuỗi 429 — đúng chuỗi lỗi khiến Zalo tắt
// webhook. Ca này canh việc đó bằng cách bắn quá trần rồi đòi mọi lượt vẫn phải 200.
func TestWebhookZaloKhongBiGioiHanTheoIP(t *testing.T) {
	h := dungServerWebhook(t)

	for i := 0; i < SoLuotToiDa+5; i++ {
		r := httptest.NewRequest(http.MethodPost, DuongDanWebhookZalo, strings.NewReader("{}"))
		r.RemoteAddr = "203.0.113.7:51000" // TEST-NET-3, RFC 5737
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("lượt %d trả %d, muốn 200 — webhook không được đi qua bộ giới hạn theo IP",
				i+1, w.Code)
		}
	}
}
