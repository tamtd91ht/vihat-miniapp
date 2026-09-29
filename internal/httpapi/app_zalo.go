package httpapi

// ===========================================================================
//  YÊU CẦU NÀY THUỘC APP NÀO — và "đã xác minh" nghĩa là gì ở kho này.
//
//  Một bản chạy phục vụ app CHUNG (ZALO_MINIAPP_APP_ID) và N app RIÊNG của xã
//  (ZALO_MINIAPP_COMMUNE_APP_SECRETS). Mọi lượt đổi có secret với Zalo — số
//  điện thoại, vị trí — phải dùng secret của đúng app của yêu cầu.
//
//  Zalo KHÔNG cho ta cách nào hỏi "token này của app nào":
//   - accessToken là chuỗi mờ; lời gọi mã tài khoản (/v2.0/me) không nhận
//     secret và không trả app (internal/zalo/ma_tai_khoan.go);
//   - chỉ /v2.0/me/info (số, vị trí) nhận secret_key.
//  Vì thế:
//
//   1. App ID ĐẾN TỪ CLIENT — trường `appId` của thân. Nó chỉ CHỌN SECRET, không
//      cấp gì. Không có hoặc bằng app chung -> app chung, y như trước.
//   2. "App ID ĐÃ XÁC MINH" (ADR 0044/0045 phía ViGov) = một lượt đổi CÓ SECRET
//      của chính app ấy THÀNH CÔNG trong cùng yêu cầu. Với đăng nhập, đó là lượt
//      đổi phoneToken — nên đăng nhập từ app riêng BẮT BUỘC có phoneToken; không
//      có thì không có gì để xác minh, và kho này từ chối chứ không tin lời khai.
//   3. Cách xác minh ấy CHỈ ĐÚNG nếu Zalo từ chối token của app X khi đổi bằng
//      secret của app Y (ADR 0045 UNKNOWN #1) — CHƯA ĐO có kiểm soát. Chứng cứ
//      hai chiều: ADR 0047 §6 ghi đổi token app xã bằng secret app chung "đã chạy
//      được" 27/09; lỗi 502 của /api/v1/location ở app xã thì gợi ý Zalo CÓ kiểm.
//      Nếu Zalo không kiểm: người khai `appId` của xã B chỉ vào được xã B — đúng
//      thứ QR công khai của xã B đã cho, không đọc được hồ sơ của ai khác (ADR
//      0045:286). Hậu quả được chặn ở đó, không ở đây.
//
//  App ID lạ (không phải app chung, không trong danh sách) -> TỪ CHỐI. Không có
//  secret nào để đổi, và lùi về app chung là đăng nhập một người dân của xã vào
//  bề mặt thương mại.
// ===========================================================================

// ZaloCuaApp — mọi lượt đổi CÓ SECRET của MỘT app. Cài đặt: *zalo.Client dựng
// với secret của app ấy.
type ZaloCuaApp interface {
	DoiTokenZalo
	DoiViTriZalo
}

// VoiAppXa nối các app riêng của xã: App ID → bộ đổi mang secret của app ấy.
// Không gọi (hoặc map rỗng) là không có app riêng — mọi `appId` khác app chung
// bị từ chối như App ID lạ.
func (s *Server) VoiAppXa(apps map[string]ZaloCuaApp) *Server {
	s.appXa = apps
	return s
}

type loaiApp int

const (
	appChung loaiApp = iota // không khai, hoặc khai đúng ZALO_MINIAPP_APP_ID
	appRieng                // có trong ZALO_MINIAPP_COMMUNE_APP_SECRETS
	appLa                   // còn lại — từ chối
)

// chonApp phân loại App ID client khai. So NGUYÊN VĂN: App ID là mã, không
// phải chữ để chuẩn hoá. Giá trị lạ KHÔNG được log — nó là chuỗi client tuỳ ý
// đặt, và có thể mang bất cứ thứ gì.
func (s *Server) chonApp(appID string) (loaiApp, ZaloCuaApp) {
	if appID == "" || appID == s.appIDChung {
		return appChung, nil
	}
	if z, ok := s.appXa[appID]; ok && z != nil {
		return appRieng, z
	}
	return appLa, nil
}
