package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// DoiViTriZalo đổi token của getLocation() lấy (vĩ độ, kinh độ).
// Cài đặt: *zalo.Client.
type DoiViTriZalo interface {
	LayViTri(ctx context.Context, accessToken, locationToken string) (float64, float64, error)
}

// Mức giới hạn của tuyến vị trí: 10 lượt / 5 phút / IP — CÙNG con số với tuyến
// đăng nhập, vì cùng một lý do: mỗi lượt tốn một lời gọi sang Zalo. Công dân
// bấm "lấy vị trí của tôi" vài lần cho một phản ánh là cùng.
//
// XÔ RIÊNG, không dùng chung xô đăng nhập: dùng chung thì vài lần bấm lấy vị
// trí ăn mất hạn mức đăng nhập của chính người ấy.
const (
	SoLuotViTriToiDa = 10
	CuaSoViTri       = 5 * time.Minute
)

// VoiViTri nối bề mặt đổi vị trí vào máy chủ. Tách khỏi `Moi` vì cùng lý do
// `VoiYeuCau`: test của phiên đăng nhập gọi `Moi` trần.
//
// Tuyến LUÔN được gắn; chưa gọi VoiViTri thì nó trả 503 chứ không 404 — xem chú
// thích ở `Handler`.
func (s *Server) VoiViTri(z DoiViTriZalo) *Server {
	s.doiViTri = z
	s.gioiHanViTri = MoiGioiHan(SoLuotViTriToiDa, CuaSoViTri)
	return s
}

type yeuCauViTri struct {
	AccessToken   string `json:"accessToken"`   // getAccessToken()
	LocationToken string `json:"locationToken"` // token của getLocation()
	// AppID — cùng nghĩa với POST /api/v1/sessions (app_zalo.go): chọn secret
	// của app đang chạy. Vắng = app chung.
	AppID string `json:"appId"`
}

type phanHoiViTri struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// phanHoiLoiCoMa — CÙNG khoá "message" như mọi tuyến (khoá phía Mini App đọc),
// THÊM khoá "code" để phía app chọn nhánh mà không phải so câu chữ tiếng Việt.
// Chỉ tuyến này có "code": thêm nó vào `phanHoiLoi` chung là đổi hợp đồng của
// những tuyến phía app đã viết xong.
type phanHoiLoiCoMa struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Mã lỗi của tuyến vị trí — hợp đồng với phía Mini App, không đổi tuỳ tiện.
const (
	maViTriYeuCauHong    = "invalid_request"
	maViTriZaloKhongTra  = "zalo_location_unavailable"
	maViTriQuaNhieuLan   = "rate_limited"
	maViTriSaiPhuongThuc = "method_not_allowed"
	maViTriChuaLapRap    = "unavailable"
	maViTriAppLa         = "app_not_configured"
)

const (
	loiViTriAppLa      = "Ứng dụng chưa sẵn sàng lấy vị trí. Vui lòng tự nhập địa chỉ."
	loiViTriYeuCauHong = "Yêu cầu không hợp lệ. Vui lòng thử lại, hoặc tự nhập địa chỉ."
	loiViTriZalo       = "Chưa lấy được vị trí từ Zalo. Vui lòng thử lại, hoặc tự nhập địa chỉ."
	loiViTriQuaNhieu   = "Bạn đã thử lấy vị trí quá nhiều lần. Vui lòng chờ vài phút, hoặc tự nhập địa chỉ."
)

// viTri — POST /api/v1/location. CÔNG KHAI, cùng lý do và cùng lớp chắn như
// POST /api/v1/sessions: thứ đứng chắn là token do Zalo cấp (Zalo chỉ đổi token
// vị trí khi access token và secret của app khớp) và giới hạn theo IP.
//
// Vì sao KHÔNG đòi phiên của kho này (doiPhien): công dân đi cầu ViGov cầm
// phiên của ViGov, không phải phiên của kho này — đòi phiên ở đây thì đúng họ,
// những người cần vị trí cho phản ánh, không bao giờ gọi được.
//
// KHÔNG LƯU GÌ. Toạ độ quay về Mini App, người dân nhìn thấy, và chỉ đi tiếp nếu
// họ chọn gửi phản ánh — hỏi một người đang đứng đâu không phải là ghi lại nơi
// họ đứng. Toạ độ và token KHÔNG vào log, KHÔNG vào lỗi. Không ghi
// `nhat_ky_dang_nhap`: đây không phải một lần đăng nhập.
func (s *Server) viTri(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.traLoiCoMa(w, http.StatusMethodNotAllowed, loiViTriYeuCauHong, maViTriSaiPhuongThuc)
		return
	}
	if s.doiViTri == nil || s.gioiHanViTri == nil {
		s.log.Error("tuyến vị trí chưa được lắp ráp — thiếu VoiViTri ở cmd/server")
		s.traLoiCoMa(w, http.StatusServiceUnavailable, loiChuaLapRap, maViTriChuaLapRap)
		return
	}
	if choPhep, lanDau := s.gioiHanViTri.Cho(khoaGioiHan(ipCuaKhach(r))); !choPhep {
		if lanDau {
			// Đúng một dòng cho mỗi đợt bị chặn — xem GioiHanIP.Cho.
			s.log.Warn("tuyến vị trí vượt giới hạn theo IP")
		}
		s.traLoiCoMa(w, http.StatusTooManyRequests, loiViTriQuaNhieu, maViTriQuaNhieuLan)
		return
	}

	var yc yeuCauViTri
	if err := json.NewDecoder(io.LimitReader(r.Body, gioiHanThan)).Decode(&yc); err != nil {
		// KHÔNG trả err ra ngoài: thông điệp của bộ giải mã có thể chứa nguyên văn thân.
		s.traLoiCoMa(w, http.StatusBadRequest, loiViTriYeuCauHong, maViTriYeuCauHong)
		return
	}
	if yc.AccessToken == "" || yc.LocationToken == "" {
		s.traLoiCoMa(w, http.StatusBadRequest, loiViTriYeuCauHong, maViTriYeuCauHong)
		return
	}

	// Secret của ĐÚNG app. Trước nhánh này, app riêng đổi bằng secret app chung
	// và nhận 502 (chủ dự án báo 29/09/2026 — chưa rõ Zalo từ chối vì secret
	// sai app hay vì lý do khác; app_zalo.go điểm 3).
	doi := s.doiViTri
	switch loai, zApp := s.chonApp(yc.AppID); loai {
	case appLa:
		// Không có secret nào để đổi; lùi về app chung chỉ đổi lấy một lần Zalo
		// từ chối. 422 chứ không 502: lỗi nằm ở cấu hình của ta, không ở Zalo.
		s.log.Warn("tuyến vị trí: App ID chưa cấu hình — từ chối (giá trị không log: do client đặt)")
		s.traLoiCoMa(w, http.StatusUnprocessableEntity, loiViTriAppLa, maViTriAppLa)
		return
	case appRieng:
		doi = zApp
	}

	viDo, kinhDo, err := doi.LayViTri(r.Context(), yc.AccessToken, yc.LocationToken)
	if err != nil {
		// MỘT mã, MỘT trạng thái (502) cho MỌI thất bại của Zalo — KHÁC tuyến đăng
		// nhập, cố ý:
		//  - error != 0 ở lượt này không tách được "access token hết hạn" với "token
		//    vị trí hết hạn" (dùng một lần, ~2 phút). Trả 401 thì phía app đọc
		//    thành "phiên hỏng, đăng nhập lại" — sai với ca thứ hai, ca hay gặp hơn.
		//  - Việc người dân làm tiếp là như nhau: thử lại, hoặc tự gõ địa chỉ.
		// Lỗi chỉ mang MÃ lỗi số của Zalo — không toạ độ, không token (client.go).
		loai := "khong_voi_toi_zalo"
		if errors.Is(err, zalo.ErrTokenKhongHopLe) {
			loai = "zalo_tu_choi_token"
		}
		s.log.Warn("không đổi được token vị trí với Zalo", "loai", loai, "loi", err.Error())
		s.traLoiCoMa(w, http.StatusBadGateway, loiViTriZalo, maViTriZaloKhongTra)
		return
	}

	// Không có dòng log thành công: một dòng "ai đó hỏi vị trí lúc mấy giờ từ IP
	// nào" đã là mảnh của một dấu vết di chuyển, và không ai cần nó để vận hành.
	s.traJSON(w, http.StatusOK, phanHoiViTri{Latitude: viDo, Longitude: kinhDo})
}

func (s *Server) traLoiCoMa(w http.ResponseWriter, ma int, cau, maLoi string) {
	s.traJSON(w, ma, phanHoiLoiCoMa{Message: cau, Code: maLoi})
}
