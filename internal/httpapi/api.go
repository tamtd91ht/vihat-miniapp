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
	"github.com/vihat/vihat-miniapp/internal/vigovcau"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
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

	// Ba trường của bề mặt "yêu cầu" — xem `VoiYeuCau`. Cả ba ĐƯỢC PHÉP nil, và
	// `Handler` vẫn gắn tuyến trong ca ấy; xem chú thích ở đó.
	khoPhien      KhoPhien
	yeuCau        *yeucau.DichVu
	gioiHanYeuCau *GioiHanIP

	// Ba trường của cầu phiên ViGov — xem `VoiCauPhienViGov`. nil là cầu TẮT:
	// tuyến đăng nhập chạy đúng hành vi cũ.
	cau      CauPhienViGov
	maZalo   MaTaiKhoanZalo
	appIDCau string

	// Hai trường của tuyến đổi vị trí — xem `VoiViTri`. nil là chưa lắp ráp:
	// tuyến vẫn gắn và trả 503.
	doiViTri     DoiViTriZalo
	gioiHanViTri *GioiHanIP
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

// VoiYeuCau nối bề mặt "người dùng giơ tay" vào máy chủ.
//
// TÁCH KHỎI `Moi` CHỨ KHÔNG THÊM HAI THAM SỐ NỮA, vì hai lý do độc lập: bề mặt
// phiên đăng nhập phải test được mà không phải dựng cả tầng yêu cầu
// (`sessions_test.go` gọi `Moi` trần), và một hàm dựng sáu tham số là hàm mà lần
// thêm thứ bảy sẽ truyền nhầm thứ tự.
func (s *Server) VoiYeuCau(khoPhien KhoPhien, dv *yeucau.DichVu) *Server {
	s.khoPhien = khoPhien
	s.yeuCau = dv
	s.gioiHanYeuCau = MoiGioiHan(SoLuotYeuCauToiDa, CuaSoYeuCau)
	return s
}

// CauPhienViGov mở một phiên công dân ở ViGov. Cài đặt: internal/vigovcau.
type CauPhienViGov interface {
	MoPhien(ctx context.Context, yc vigovcau.YeuCau) (vigovcau.KetQua, error)
}

// MaTaiKhoanZalo xác minh accessToken bằng secret của app và trả mã tài khoản
// Zalo. Hôm nay chỉ có zalo.MaTaiKhoanChuaDo — luôn từ chối (ADR 0045 UNKNOWN #2).
type MaTaiKhoanZalo interface {
	LayMaTaiKhoan(ctx context.Context, accessToken string) (string, error)
}

// VoiCauPhienViGov bật cầu phiên: từ đây POST /api/v1/sessions có mang
// communeHostHint phát PHIÊN CÔNG DÂN CỦA ViGov; lượt không mang nó vẫn là
// phiên của kho này — xem sessions.go (chọn nhánh) và sessions_vigov.go.
//
// appID là App ID mà secret của nó xác minh token — hôm nay chỉ có MỘT cặp
// (ZALO_MINIAPP_APP_ID / ZALO_MINIAPP_SECRET_KEY). N app riêng cần N cặp và
// cách chọn cặp theo yêu cầu (ADR 0045 UNKNOWN #1) — chưa làm.
//
// Tách khỏi `Moi` vì cùng lý do `VoiYeuCau`: test của phiên cũ gọi `Moi` trần.
func (s *Server) VoiCauPhienViGov(cau CauPhienViGov, maZalo MaTaiKhoanZalo, appID string) *Server {
	s.cau, s.maZalo, s.appIDCau = cau, maZalo, appID
	return s
}

// Handler dựng bộ định tuyến. Ba tuyến CÔNG KHAI và một tuyến CẦN XÁC THỰC:
//
//	POST /api/v1/sessions — công khai vì đây CHÍNH LÀ tuyến đăng nhập: người gọi
//	                        chưa có gì để xác thực. Thứ bảo vệ nó là token của
//	                        Zalo (chỉ Zalo cấp được) + giới hạn theo IP.
//	POST /api/v1/location — công khai, cùng lớp chắn với /sessions (token của
//	                        Zalo + giới hạn theo IP, xô riêng). Đổi token của
//	                        getLocation() lấy toạ độ; không lưu gì — vi_tri.go.
//	GET  /healthz         — công khai cho thăm dò sức khoẻ của hạ tầng. Phản hồi
//	                        không mang thông tin nội bộ: chỉ "ok" hoặc 503.
//	     /webhooks/zalo   — công khai vì Zalo gọi từ hạ tầng của họ, không mang
//	                        phiên nào. Hôm nay nó không đọc và không lưu gì, và
//	                        đó là điều kiện để "không kiểm chứng" còn chấp nhận
//	                        được — xem webhook_zalo.go.
//
//	/api/v1/requests      — CẦN XÁC THỰC. POST tạo một yêu cầu, GET đọc yêu cầu
//	                        CỦA CHÍNH MÌNH. Bọc bởi `doiPhien`, và hai lớp giới
//	                        hạn: theo IP ở đây, theo NGƯỜI ở internal/yeucau.
//
// ⚠ TUYẾN `/api/v1/requests` LUÔN ĐƯỢC GẮN, KỂ CẢ KHI `VoiYeuCau` CHƯA ĐƯỢC GỌI.
//
//	Gắn có điều kiện thì một lần lắp ráp thiếu ở `cmd/server` biến thành 404, và
//	404 trên một tuyến ĐÚNG là thứ phía Mini App sẽ đọc thành "mình gọi sai
//	đường dẫn" — rồi đi sửa đúng chỗ không hỏng. Gắn luôn thì cùng lỗi ấy hiện
//	ra thành 503 kèm một câu tiếng Việt nói chức năng tạm ngưng: đúng triệu
//	chứng, đúng phía.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions", s.taoPhien)
	mux.HandleFunc("/api/v1/location", s.viTri)
	mux.HandleFunc("/api/v1/requests", s.gacYeuCau(s.requests))
	mux.HandleFunc("/healthz", s.healthz)
	s.mountWebhookZalo(mux)
	return s.cors.boc(mux)
}

const loiChuaLapRap = "Chức năng này đang tạm ngưng. Vui lòng gọi hotline để được hỗ trợ ngay."

// gacYeuCau đặt hai thứ trước mọi tuyến của bề mặt yêu cầu: giới hạn theo IP,
// rồi xác thực phiên. Thứ tự KHÔNG đổi được — kiểm phiên trước nghĩa là một
// máy quét làm ta tra CSDL mỗi lượt nó gõ cửa.
func (s *Server) gacYeuCau(tiep http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.yeuCau == nil || s.khoPhien == nil || s.gioiHanYeuCau == nil {
			s.log.Error("tuyến yêu cầu chưa được lắp ráp — thiếu VoiYeuCau ở cmd/server")
			s.traLoi(w, http.StatusServiceUnavailable, loiChuaLapRap)
			return
		}
		if choPhep, _ := s.gioiHanYeuCau.Cho(khoaGioiHan(ipCuaKhach(r))); !choPhep {
			// KHÔNG ghi vào `nhat_ky_dang_nhap`: bảng ấy là nhật ký ĐĂNG NHẬP, và
			// nhét một sự kiện khác loại vào nó làm hỏng chính con số mà cảnh báo
			// đăng nhập đang đếm. Một dòng log là đủ cho lớp chặn theo IP.
			s.traLoi(w, http.StatusTooManyRequests, loiQuaNhieuLan)
			return
		}
		s.doiPhien(tiep)(w, r)
	}
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
