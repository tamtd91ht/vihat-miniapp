// Package httpapi giữ bề mặt HTTP: tuyến, CORS, giới hạn theo IP, và việc dịch
// lỗi kỹ thuật thành một câu tiếng Việt nói người dùng làm gì tiếp.
//
// Gói này KHÔNG biết SQL và KHÔNG biết hình dạng giao thức của Zalo — nó chỉ
// thấy hai giao diện dưới đây. Nhờ vậy test của nó chạy không cần CSDL, không
// cần mạng, và vẫn kiểm được đúng thứ đáng kiểm: mã trạng thái, câu trả về, và
// việc dữ liệu cá nhân không rò ra ngoài.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/phien"
)

// Kho là phần kho dữ liệu mà tầng HTTP cần. Cố ý hẹp: tầng này không được phép
// đọc lung tung, và một giao diện hẹp là một giao diện giả lập được trong test
// mà không phải dựng cả CSDL.
type Kho interface {
	TaoPhienDangNhap(ctx context.Context, soDienThoai string, tokenBam []byte, hetHanLuc time.Time, ip *netip.Addr) (phien.KetQuaTao, error)
	GhiNhatKyThatBai(ctx context.Context, ketQua, lyDo string, ip *netip.Addr) error
	Ping(ctx context.Context) error
}

// DoiTokenZalo đổi phoneToken lấy số điện thoại đã chuẩn hoá.
type DoiTokenZalo interface {
	LaySoDienThoai(ctx context.Context, accessToken, phoneToken string) (string, error)
}

// Mức giới hạn của tuyến đăng nhập: 10 lượt / 5 phút / IP.
//
// Con số này là ƯỚC LƯỢNG kỹ thuật, không phải chính sách: một người dùng thật
// bấm đăng nhập vài lần là cùng, kể cả khi mạng chập chờn. Nếu số liệu thật cho
// thấy người dùng bình thường chạm trần, hãy nới — đừng bỏ.
const (
	SoLuotToiDa  = 10
	CuaSoGioiHan = 5 * time.Minute
)

type Server struct {
	kho     Kho
	zalo    DoiTokenZalo
	gioiHan *GioiHanIP
	cors    *cors
	log     *slog.Logger
	ttl     time.Duration
	now     func() time.Time
}

func Moi(kho Kho, zalo DoiTokenZalo, cfg config.Config, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		kho:     kho,
		zalo:    zalo,
		gioiHan: MoiGioiHan(SoLuotToiDa, CuaSoGioiHan),
		cors:    moCORS(cfg.CORSAllowedOrigins),
		log:     log,
		ttl:     config.TTLPhien,
		now:     time.Now,
	}
}

// Handler dựng bộ định tuyến. Ba tuyến, cả ba CÔNG KHAI và nói rõ vì sao:
//
//	POST /api/v1/sessions — công khai vì đây CHÍNH LÀ tuyến đăng nhập: người gọi
//	                        chưa có gì để xác thực. Thứ bảo vệ nó là token của
//	                        Zalo (chỉ Zalo cấp được) + giới hạn theo IP.
//	GET  /healthz         — công khai cho thăm dò sức khoẻ của hạ tầng. Phản hồi
//	                        không mang thông tin nội bộ: chỉ "ok" hoặc 503.
//	     /webhooks/zalo   — công khai vì Zalo gọi từ hạ tầng của họ, không mang
//	                        phiên nào. Hôm nay nó không đọc và không lưu gì, và
//	                        đó là điều kiện để "không kiểm chứng" còn chấp nhận
//	                        được — xem webhook_zalo.go.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions", s.taoPhien)
	mux.HandleFunc("/healthz", s.healthz)
	s.mountWebhookZalo(mux)
	return s.cors.boc(mux)
}

// ---------------------------------------------------------------------------
// Thân phản hồi
// ---------------------------------------------------------------------------

type phanHoiPhien struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// phanHoiLoi — khoá "message", đã được phía Mini App xác nhận là khoá họ đọc.
type phanHoiLoi struct {
	Message string `json:"message"`
}

// Câu trả về cho người dùng. Tiếng Việt, nói việc phải làm tiếp, KHÔNG mã lỗi
// kỹ thuật, KHÔNG tên cột, KHÔNG thông điệp của Zalo, KHÔNG số điện thoại.
const (
	loiYeuCauHong    = "Yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử đăng nhập lại."
	loiTokenHetHan   = "Phiên đăng nhập Zalo đã hết hạn. Vui lòng đóng và mở lại ứng dụng để đăng nhập lại."
	loiKhongVoiZalo  = "Hiện chưa kết nối được tới Zalo. Vui lòng thử lại sau ít phút."
	loiQuaNhieuLan   = "Bạn đã thử đăng nhập quá nhiều lần. Vui lòng chờ vài phút rồi thử lại."
	loiHeThong       = "Hệ thống đang bận. Vui lòng thử lại sau ít phút."
	loiSaiPhuongThuc = "Yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử đăng nhập lại."
)

func (s *Server) traJSON(w http.ResponseWriter, ma int, than any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// no-store: thân phản hồi 201 mang bearer token. Không để nó nằm lại trong
	// cache của webview hay của bất kỳ proxy nào trên đường.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(ma)
	if err := json.NewEncoder(w).Encode(than); err != nil {
		// Client đã ngắt giữa chừng — không còn gì để trả, chỉ ghi lại.
		s.log.Warn("không ghi được thân phản hồi", "loi", err.Error())
	}
}

func (s *Server) traLoi(w http.ResponseWriter, ma int, cau string) {
	s.traJSON(w, ma, phanHoiLoi{Message: cau})
}

// ipCuaKhach lấy IP từ RemoteAddr. KHÔNG đọc X-Forwarded-For: header đó ai cũng
// giả được, và một bộ giới hạn tin vào nó là một bộ giới hạn vô dụng. Đặt sau
// load balancer thì phải khai proxy tin cậy trước (xem README, mục NỢ).
//
// Trả nil khi không phân tích được — bên gọi coi đó là "không rõ" chứ không đoán.
func ipCuaKhach(r *http.Request) *netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	a = a.Unmap()
	return &a
}

// khoaGioiHan: IP không rõ thì tất cả dùng chung một xô. Hỏng về phía ĐÓNG —
// thà siết nhầm còn hơn mở một lối đi miễn kiểm chỉ bằng cách giấu IP.
func khoaGioiHan(ip *netip.Addr) string {
	if ip == nil {
		return "khong-ro"
	}
	return ip.String()
}
