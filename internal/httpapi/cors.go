package httpapi

import (
	"net/http"
	"strings"
)

// CORS — vì sao tệp này tồn tại:
//
// Mini App chạy trong webview của Zalo, tức là JavaScript gọi API từ một origin
// KHÁC origin của API. Không có CORS thì trình duyệt chặn phản hồi và nút đăng
// nhập "chết im lặng" trên máy thật: người dùng bấm, không có gì xảy ra, còn log
// phía máy chủ vẫn thấy 201. Đây là loại lỗi chỉ lộ ra trên thiết bị thật, nên
// nó được xử lý tường minh ngay từ dòng đầu.
//
// KHÔNG dùng "*": danh sách đọc từ CORS_ALLOWED_ORIGINS, và config từ chối "*"
// ngay lúc nạp (fail fast, trước khi nhận yêu cầu đầu tiên).
//
// KHÔNG đặt Access-Control-Allow-Credentials: API xác thực bằng bearer token
// trong header Authorization, không dùng cookie. Bật credentials là mở thêm một
// bề mặt tấn công (CSRF) mà ta không cần tới.
type cors struct {
	duocPhep map[string]struct{}
}

func moCORS(origins []string) *cors {
	m := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		m[chuanHoaOrigin(o)] = struct{}{}
	}
	return &cors{duocPhep: m}
}

func chuanHoaOrigin(o string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(o), "/"))
}

const (
	maxAgePreflight = "600" // giây — đỡ một lượt preflight cho mỗi 10 phút
	headerChoPhep   = "Content-Type, Authorization"
	methodChoPhep   = "POST, GET, OPTIONS"
)

// boc gắn header CORS và trả lời preflight.
//
// Yêu cầu KHÔNG có Origin (gọi từ máy chủ, công cụ thăm dò sức khoẻ) đi tiếp
// bình thường: CORS là cơ chế của trình duyệt, không phải cơ chế xác thực. Ai
// cần chặn truy cập thì dùng xác thực, đừng trông vào CORS.
func (c *cors) boc(tiep http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Vary: Origin — thiếu dòng này thì một proxy có thể phục vụ lại phản
			// hồi đã gắn origin của người khác.
			w.Header().Add("Vary", "Origin")
			if _, ok := c.duocPhep[chuanHoaOrigin(origin)]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", methodChoPhep)
				w.Header().Set("Access-Control-Allow-Headers", headerChoPhep)
				w.Header().Set("Access-Control-Max-Age", maxAgePreflight)
			}
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			// Preflight từ origin lạ: không có header cho phép ở trên nên trình
			// duyệt tự chặn. Trả 204 chứ không 403, để CORS không trở thành kênh
			// dò xem origin nào nằm trong danh sách.
			w.WriteHeader(http.StatusNoContent)
			return
		}

		tiep.ServeHTTP(w, r)
	})
}
