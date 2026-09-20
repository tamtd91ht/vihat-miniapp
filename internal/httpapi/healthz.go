package httpapi

import (
	"context"
	"net/http"
	"time"
)

// thoiHanNgoaiYeuCau: công việc không được sống lâu hơn thế này khi nó không
// còn gắn với một yêu cầu đang mở.
const thoiHanNgoaiYeuCau = 3 * time.Second

// contextRoi tạo một context tách khỏi vòng đời của yêu cầu nhưng vẫn mang
// theo các giá trị của nó. Dùng cho việc PHẢI xong kể cả khi client đã ngắt —
// ở đây là dòng nhật ký đăng nhập thất bại.
func contextRoi(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), thoiHanNgoaiYeuCau)
}

type phanHoiSucKhoe struct {
	TrangThai string `json:"trang_thai"`
}

// healthz — GET /healthz. CÔNG KHAI, cho bộ thăm dò của hạ tầng (k8s, load
// balancer) vốn không cầm được token nào.
//
// Có chạm CSDL: một service "còn sống" nhưng mất kết nối CSDL thì không phục vụ
// nổi một lượt đăng nhập nào, và báo 200 lúc đó chỉ khiến bộ điều phối giữ
// nguyên một bản sao đã hỏng trong vòng phục vụ.
//
// Phản hồi không mang thông tin nội bộ: không tên máy chủ, không phiên bản,
// không thông điệp lỗi. Đây là tuyến công khai.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.traLoi(w, http.StatusMethodNotAllowed, loiSaiPhuongThuc)
		return
	}

	ctx, huy := context.WithTimeout(r.Context(), thoiHanNgoaiYeuCau)
	defer huy()

	if err := s.kho.Ping(ctx); err != nil {
		s.log.Error("healthz: không chạm được CSDL", "loi", err.Error())
		s.traJSON(w, http.StatusServiceUnavailable, phanHoiSucKhoe{TrangThai: "csdl_khong_san_sang"})
		return
	}
	s.traJSON(w, http.StatusOK, phanHoiSucKhoe{TrangThai: "ok"})
}
