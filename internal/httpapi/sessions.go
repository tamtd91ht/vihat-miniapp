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
