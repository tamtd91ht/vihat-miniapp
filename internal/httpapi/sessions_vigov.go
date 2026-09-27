package httpapi

import (
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/vigovcau"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// taoPhienViGov — nhánh của POST /api/v1/sessions khi cầu phiên ViGov BẬT.
//
// Kho này xác minh token Zalo bằng secret của app, rồi gọi cầu phiên của ViGov
// (ADR 0045 bước 3–8) và chuyển NGUYÊN token phiên ViGov cho Mini App.
//
// KHÁC nhánh cũ ở bốn điểm, cả bốn có chủ đích:
//
//  1. phoneToken TUỲ CHỌN. App mở âm thầm chỉ có accessToken; số điện thoại chỉ
//     xin khi công dân gửi thứ gì đó (ADR 0045 câu 2). Có phoneToken thì số đi
//     sang ViGov; không có thì trường số RỖNG — không bao giờ một số cũ.
//  2. KHÔNG phát phiên của kho này và KHÔNG lưu số: số điện thoại công dân đi
//     thẳng sang ViGov, không dừng lại trong CSDL thương mại. Token ViGov không
//     lưu, không log (bước 8).
//  3. Thân 201 mang khoá `vigovSession`, KHÔNG mang `token` ở gốc. Hai loại
//     phiên do hai hệ thống ký không được trùng một tên khoá: phía Mini App cấm
//     gửi phiên thương mại tới ViGov, và một bên đọc `token` cũ gặp thân mới thì
//     phải hỏng ồn ào chứ không cầm nhầm token của hệ thống kia.
//  4. Không biết xã. communeHostHint / communeConfirmed đi nguyên văn; ViGov kiểm
//     khuôn và quyết xã. Kho này không có danh sách xã nào để kiểm.
//
// Thứ tự gọi Zalo: MÃ TÀI KHOẢN TRƯỚC, SỐ SAU. phoneToken rất có thể dùng một
// lần; hỏng ở bước mã mà đã tiêu phoneToken là bắt công dân cấp quyền lại.
func (s *Server) taoPhienViGov(w http.ResponseWriter, r *http.Request, yc yeuCauTaoPhien, ip *netip.Addr) {
	ctx := r.Context()

	if yc.AccessToken == "" {
		s.traLoi(w, http.StatusBadRequest, loiYeuCauHong)
		return
	}
	if s.maZalo == nil || s.appIDCau == "" {
		s.log.Error("cầu phiên ViGov lắp ráp thiếu — VoiCauPhienViGov không có nguồn mã tài khoản hoặc app id")
		s.traLoi(w, http.StatusServiceUnavailable, loiCauTamNgung)
		return
	}

	// Mã tài khoản Zalo là DỮ LIỆU CÁ NHÂN: biến này chỉ đi vào YeuCau bên dưới.
	maTaiKhoan, err := s.maZalo.LayMaTaiKhoan(ctx, yc.AccessToken)
	if err == nil && maTaiKhoan == "" {
		// Không bao giờ gửi mã rỗng: máy chủ trả INVALID_ARGUMENT, và lỗi thật
		// (ta đọc sai phản hồi của Zalo) bị che thành "yêu cầu sai".
		err = zalo.ErrKhongVoiToiZalo
	}
	if err != nil {
		switch {
		case errors.Is(err, zalo.ErrTokenKhongHopLe):
			s.ghiThatBai(r, phien.KetQuaTokenZaloHong, "zalo_tu_choi_token", ip)
			s.traLoi(w, http.StatusUnauthorized, loiTokenHetHan)
		case errors.Is(err, zalo.ErrMaTaiKhoanChuaDo):
			// Lỗi PHÍA TA, đúng như thiết kế hôm nay — xem internal/zalo/ma_tai_khoan.go.
			s.log.Error("cầu phiên ViGov từ chối: chưa có cách đã đo để lấy mã tài khoản Zalo (ADR 0045 UNKNOWN #2)")
			s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_chua_do_ma_zalo", ip)
			s.traLoi(w, http.StatusServiceUnavailable, loiCauTamNgung)
		default:
			s.log.Error("không lấy được mã tài khoản Zalo", "loi", err.Error())
			s.ghiThatBai(r, phien.KetQuaLoiZalo, "khong_voi_toi_zalo", ip)
			s.traLoi(w, http.StatusBadGateway, loiKhongVoiZalo)
		}
		return
	}

	// Số điện thoại: CHỈ khi lượt này có phoneToken. Biến này là chỗ duy nhất
	// số xuất hiện, và nó đi thẳng vào YeuCau.
	var so string
	if yc.PhoneToken != "" {
		so, err = s.zalo.LaySoDienThoai(ctx, yc.AccessToken, yc.PhoneToken)
		if err != nil {
			switch {
			case errors.Is(err, zalo.ErrTokenKhongHopLe):
				s.ghiThatBai(r, phien.KetQuaTokenZaloHong, "zalo_tu_choi_token", ip)
				s.traLoi(w, http.StatusUnauthorized, loiTokenHetHan)
			default:
				s.log.Error("không đổi được token với Zalo", "loi", err.Error())
				s.ghiThatBai(r, phien.KetQuaLoiZalo, "khong_voi_toi_zalo", ip)
				s.traLoi(w, http.StatusBadGateway, loiKhongVoiZalo)
			}
			return
		}
	}

	ipChuoi := ""
	if ip != nil {
		ipChuoi = ip.String()
	}
	kq, err := s.cau.MoPhien(ctx, vigovcau.YeuCau{
		AppID:       s.appIDCau,
		MaTaiKhoan:  maTaiKhoan,
		SoDaXacThuc: so,
		IPKhach:     ipChuoi,
		ThietBi:     r.UserAgent(),
		TenMienXa:   yc.CommuneHostHint,
		DaXacNhanXa: yc.CommuneConfirmed,
	})
	if err != nil {
		s.traLoiLoiCau(w, r, err, ip)
		return
	}

	// Chỉ MÃ PHIÊN (không cấp quyền gì) và app id. Không token, không mã tài
	// khoản, không số, không tên miền xã (nó lộ người này đang làm việc với xã nào).
	s.log.Info("mở phiên công dân ViGov", "app_id", s.appIDCau, "phien_vigov_id", kq.PhienID,
		"co_xa", kq.TenXa != "", "da_xac_thuc_so", kq.DaXacThucSo, "gui_so", so != "")

	ph := phienViGov{
		Token:             kq.Token,
		TenantDisplayName: kq.TenXa,
		PhoneVerified:     kq.DaXacThucSo,
	}
	if !kq.HetHan.IsZero() {
		ph.ExpiresAt = kq.HetHan.UTC().Format(time.RFC3339)
	}
	s.traJSON(w, http.StatusCreated, phanHoiPhienViGov{VigovSession: ph})
}

// traLoiLoiCau dịch lỗi của cầu thành mã HTTP.
//
//	ErrYeuCauSai   -> 400  lỗi nối dây (tên miền sai khuôn…). Log Error: người
//	                       sửa là lập trình viên, không phải công dân.
//	ErrChuaSanSang -> 422  MỘT câu cho mọi nhánh FAILED_PRECONDITION — hợp đồng
//	                       cố ý không để bên gọi phân biệt "xã không tồn tại" với
//	                       "xã ngừng hoạt động".
//	ErrSaiKhoaCau  -> 503  triển khai sai. Log Error kèm chữ CẢNH BÁO để nổ chuông.
//	còn lại        -> 503  "không có gì được phát" (hợp đồng). Không bao giờ lùi
//	                       về một phiên của kho này.
func (s *Server) traLoiLoiCau(w http.ResponseWriter, r *http.Request, err error, ip *netip.Addr) {
	switch {
	case errors.Is(err, vigovcau.ErrYeuCauSai):
		s.log.Error("cầu phiên ViGov từ chối yêu cầu như lỗi nối dây", "loi", err.Error())
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_yeu_cau_sai", ip)
		s.traLoi(w, http.StatusBadRequest, loiYeuCauHong)
	case errors.Is(err, vigovcau.ErrChuaSanSang):
		s.log.Warn("cầu phiên ViGov: app hoặc xã chưa sẵn sàng", "app_id", s.appIDCau)
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_chua_san_sang", ip)
		s.traLoi(w, http.StatusUnprocessableEntity, loiCauChuaSanSang)
	case errors.Is(err, vigovcau.ErrSaiKhoaCau):
		s.log.Error("CẢNH BÁO VẬN HÀNH: cổng cầu ViGov từ chối khoá cầu — kiểm VIGOV_CITIZEN_SESSION_BRIDGE_KEY và địa chỉ cổng cầu")
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_sai_khoa", ip)
		s.traLoi(w, http.StatusServiceUnavailable, loiCauTamNgung)
	default:
		s.log.Error("cầu phiên ViGov không phục vụ được", "loi", err.Error())
		s.ghiThatBai(r, phien.KetQuaLoiHeThong, "cau_tam_ngung", ip)
		s.traLoi(w, http.StatusServiceUnavailable, loiCauTamNgung)
	}
}

// phanHoiPhienViGov — thân 201 khi cầu bật. Xem điểm 3 ở taoPhienViGov.
type phanHoiPhienViGov struct {
	VigovSession phienViGov `json:"vigovSession"`
}

// phienViGov — NGUYÊN VĂN những gì ViGov trả, không thêm bớt.
//
// token và expiresAt VẮNG MẶT khi phiên không xã: hợp đồng không phát token
// cho phiên ấy, và Mini App khi đó chỉ hiện màn giới thiệu. tenantDisplayName
// "" là một câu trả lời thật ("không xã nào"), không bao giờ thay bằng xã khác.
// Không có tenant_id: xã của công dân đến TỪ PHIÊN ở phía ViGov, client không
// cần và không được cầm mã xã.
type phienViGov struct {
	Token             string `json:"token,omitempty"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
	TenantDisplayName string `json:"tenantDisplayName"`
	PhoneVerified     bool   `json:"phoneVerified"`
}

const (
	loiCauTamNgung    = "Chức năng đăng nhập đang tạm ngưng. Vui lòng thử lại sau ít phút."
	loiCauChuaSanSang = "Ứng dụng chưa sẵn sàng cho địa phương này. Vui lòng quét lại mã QR do địa phương cung cấp hoặc thử lại sau."
)
