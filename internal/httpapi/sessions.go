package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// gioiHanThan: thân yêu cầu chỉ có hai chuỗi token. 8KB đã rộng rãi; không chặn
// thì một tuyến công khai nhận được thân vô hạn.
const gioiHanThan = 8 << 10

type yeuCauTaoPhien struct {
	AccessToken string `json:"accessToken"`
	PhoneToken  string `json:"phoneToken"`

	// Hai trường CHỈ có nghĩa khi cầu phiên ViGov bật (sessions_vigov.go); cầu
	// tắt thì bị bỏ qua. Kho này không diễn giải chúng: tên miền xã và cờ xác
	// nhận đi NGUYÊN VĂN sang ViGov, máy chủ ấy kiểm và quyết.
	CommuneHostHint  string `json:"communeHostHint"`
	CommuneConfirmed bool   `json:"communeConfirmed"`

	// AppID — App ID của Mini App đang chạy (client đọc từ môi trường Zalo).
	// CHỈ chọn secret; không cấp gì cho tới khi một lượt đổi có secret của app
	// ấy thành công — xem app_zalo.go. Vắng = app chung.
	AppID string `json:"appId"`
}

// taoPhien — POST /api/v1/sessions. CÔNG KHAI: đây chính là tuyến đăng nhập,
// người gọi chưa có gì để xác thực. Thứ đứng chắn là token do Zalo cấp (ta
// không tự tạo được) và bộ giới hạn theo IP.
//
// Đăng nhập MỘT CHẠM: không OTP, không mã sáu số, không SMS.
//
// Số điện thoại xuất hiện đúng một lần trong hàm này, trong đúng một biến, và
// đi thẳng xuống kho. Nó không vào log, không vào phản hồi, không vào lỗi.
func (s *Server) taoPhien(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.traLoi(w, http.StatusMethodNotAllowed, loiSaiPhuongThuc)
		return
	}

	ctx := r.Context()
	ip := ipCuaKhach(r)

	if choPhep, lanDauTuChoi := s.gioiHan.Cho(khoaGioiHan(ip)); !choPhep {
		if lanDauTuChoi {
			// Đúng một dòng nhật ký cho mỗi đợt bị chặn — xem GioiHanIP.Cho.
			s.ghiThatBai(r, phien.KetQuaQuaNhieuLan, "vuot_gioi_han_ip", ip)
		}
		s.traLoi(w, http.StatusTooManyRequests, loiQuaNhieuLan)
		return
	}

	var yc yeuCauTaoPhien
	if err := json.NewDecoder(io.LimitReader(r.Body, gioiHanThan)).Decode(&yc); err != nil {
		// KHÔNG trả err ra ngoài: thông điệp của bộ giải mã JSON có thể chứa
		// nguyên văn thứ client gửi lên.
		s.traLoi(w, http.StatusBadRequest, loiYeuCauHong)
		return
	}
	// CHỌN NHÁNH THEO YÊU CẦU, không theo cấu hình (chủ dự án chốt 27/09/2026,
	// nhánh app riêng chốt 29/09/2026). Cầu bật mà nuốt MỌI lượt đăng nhập thì
	// phần thương mại (Tư vấn / Yêu cầu của tôi — /api/v1/requests) mất phiên:
	// thân cầu không có `token` gốc.
	//
	//   appId là app RIÊNG của xã      -> cầu ViGov, gửi App ID của app ấy;
	//                                     ViGov tra xã từ App ID (chế độ riêng).
	//                                     Không cần tên miền. Xác minh App ID:
	//                                     app_zalo.go.
	//   appId lạ                        -> 422, không lùi về app chung.
	//   app chung + communeHostHint     -> cầu ViGov với App ID app chung — công
	//                                     dân vừa xác nhận xã từ QR.
	//   app chung, không communeHostHint -> phiên thương mại, y như khi cầu tắt.
	//
	// "Khác rỗng" là so NGUYÊN VĂN, không trim: kho này không diễn giải tên miền.
	// Một chuỗi sai khuôn (kể cả toàn khoảng trắng) vẫn đi cầu như trước, và
	// ViGov trả INVALID_ARGUMENT -> 400.
	switch loai, zApp := s.chonApp(yc.AppID); loai {
	case appLa:
		s.log.Warn("đăng nhập từ một App ID chưa cấu hình — từ chối (giá trị không log: do client đặt)")
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "app_id_chua_cau_hinh", ip)
		s.traLoi(w, http.StatusUnprocessableEntity, loiCauChuaSanSang)
		return
	case appRieng:
		if s.cau == nil {
			// config.Nap không cho khởi động với app riêng mà cầu tắt; tới được
			// đây là lắp ráp sai. KHÔNG BAO GIỜ lùi về phiên thương mại.
			s.log.Error("app riêng của xã nhưng cầu phiên ViGov chưa lắp ráp — thiếu VoiCauPhienViGov", "app_id", yc.AppID)
			s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_chua_lap_rap", ip)
			s.traLoi(w, http.StatusServiceUnavailable, loiCauTamNgung)
			return
		}
		s.taoPhienViGov(w, r, yc, ip, nhanhCau{appID: yc.AppID, doiSo: zApp, appRieng: true})
		return
	}
	if s.cau != nil && yc.CommuneHostHint != "" {
		s.taoPhienViGov(w, r, yc, ip, nhanhCau{appID: s.appIDCau, doiSo: s.zalo})
		return
	}
	if yc.AccessToken == "" || yc.PhoneToken == "" {
		s.traLoi(w, http.StatusBadRequest, loiYeuCauHong)
		return
	}

	so, err := s.zalo.LaySoDienThoai(ctx, yc.AccessToken, yc.PhoneToken)
	if err != nil {
		switch {
		case errors.Is(err, zalo.ErrTokenKhongHopLe):
			s.ghiThatBai(r, phien.KetQuaTokenZaloHong, "zalo_tu_choi_token", ip)
			s.traLoi(w, http.StatusUnauthorized, loiTokenHetHan)
		default:
			// Lỗi phía ta hoặc phía Zalo — KHÔNG bảo người dùng đăng nhập lại,
			// vì họ làm gì cũng không sửa được. Nói họ thử lại sau.
			s.log.Error("không đổi được token với Zalo", "loi", err.Error())
			s.ghiThatBai(r, phien.KetQuaLoiZalo, "khong_voi_toi_zalo", ip)
			s.traLoi(w, http.StatusBadGateway, loiKhongVoiZalo)
		}
		return
	}

	tok, err := phien.SinhToken()
	if err != nil {
		s.log.Error("không sinh được token phiên", "loi", err.Error())
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "sinh_token_hong", ip)
		s.traLoi(w, http.StatusInternalServerError, loiHeThong)
		return
	}

	hetHan := s.now().Add(s.ttl).UTC()
	kq, err := s.kho.TaoPhienDangNhap(ctx, so, phien.Bam(tok), hetHan, ip)
	if err != nil {
		s.log.Error("không ghi được phiên đăng nhập", "loi", err.Error())
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "ghi_phien_hong", ip)
		s.traLoi(w, http.StatusInternalServerError, loiHeThong)
		return
	}

	// Chỉ ghi MÃ ĐỊNH DANH. Không bao giờ ghi số điện thoại, kể cả ở mức debug.
	s.log.Info("đăng nhập thành công", "nguoi_dung_id", kq.NguoiDungID, "phien_id", kq.PhienID)

	s.traJSON(w, http.StatusCreated, phanHoiPhien{
		Token:     tok,
		ExpiresAt: kq.HetHanLuc.UTC().Format(time.RFC3339),
	})
}

// ghiThatBai ghi nhật ký nghiệp vụ cho một lần đăng nhập hỏng.
//
// Dùng context RIÊNG, không dùng context của yêu cầu: client ngắt kết nối là
// context của yêu cầu bị huỷ, và khi đó dòng nhật ký — thứ duy nhất còn lại của
// lần thử ấy — sẽ không bao giờ được ghi.
//
// Nhật ký hỏng thì KHÔNG làm hỏng phản hồi: người dùng không chịu trách nhiệm
// cho việc CSDL của ta trục trặc. Nhưng nó phải để lại một dòng log ở mức Error
// để cảnh báo nổ.
func (s *Server) ghiThatBai(r *http.Request, ketQua, lyDo string, ip *netip.Addr) {
	ctx, huy := contextRoi(r)
	defer huy()
	if err := s.kho.GhiNhatKyThatBai(ctx, ketQua, lyDo, ip); err != nil {
		s.log.Error("không ghi được nhật ký đăng nhập", "ket_qua", ketQua, "loi", err.Error())
	}
}
