package zalo

import (
	"errors"
	"regexp"
	"strings"
)

// ===========================================================================
//  HÌNH DẠNG WIRE CỦA LỜI GỌI ĐỔI phoneToken -> SỐ ĐIỆN THOẠI
//
//  ĐÂY LÀ TỆP DUY NHẤT MÔ TẢ GIAO THỨC VỚI ZALO. Sửa giao thức thì sửa ở đây,
//  không rải hằng chuỗi sang chỗ khác.
//
//  MỨC CHỨNG CỨ: TRUNG BÌNH.
//  - Nguồn: nhiều nguồn THỨ CẤP nhất quán với nhau (bài viết, SDK cộng đồng,
//    trích dẫn lại tài liệu Zalo). Trang tài liệu chính thức render bằng JS nên
//    KHÔNG đọc được nguyên văn trong phiên dựng kho này (20/09/2026).
//  - CHƯA THỬ TRÊN MÁY THẬT. Không một dòng nào dưới đây từng chạm máy chủ Zalo.
//  - Test trong gói này dùng httptest và do đó CHỈ chứng minh mã khớp với giả
//    định ở tệp này. Nó KHÔNG chứng minh giả định đúng.
//
//  ĐIỀU CHƯA RÕ — phải đóng trước khi phát hành (xem mục NỢ trong README):
//   1. appsecret_proof: từ 01/01/2024 Zalo yêu cầu tham số này khi lấy thông tin
//      người dùng từ máy chủ. CHƯA RÕ nó có áp cho luồng Mini App này không,
//      CHƯA RÕ gửi bằng header hay query, và CHƯA RÕ chuỗi được ký là gì
//      (access_token? secret? cả hai?). Ở đây để mặc định TẮT — xem GuiAppSecretProof.
//   2. Mã lỗi: chưa có bảng mã lỗi tin cậy. Hiện mọi error != 0 đều được coi là
//      "token sai/hết hạn" (-> 401 theo hợp đồng). Nếu thực tế có mã nghĩa là
//      "secret key của app sai" thì đó là lỗi PHÍA TA, không phải lỗi người dùng,
//      và phải tách ra thành 502 + cảnh báo vận hành.
//   3. Định dạng số trả về: các nguồn cho thấy "84xxxxxxxxx". Chưa rõ có trường
//      hợp trả "0xxxxxxxxx" hay có dấu "+" không. ChuanHoaSo() chịu cả ba, và
//      từ chối mọi thứ khác thay vì đoán.
//   4. Chưa rõ Zalo có giới hạn tần suất (rate limit) nào trên endpoint này.
// ===========================================================================

// BaseURLMacDinh là máy chủ thật. Tiêm được để test bằng httptest.
const BaseURLMacDinh = "https://graph.zalo.me"

// DuongDanLayThongTin — GET trên đường dẫn này, KHÔNG có body.
const DuongDanLayThongTin = "/v2.0/me/info"

// Ba header của lời gọi. Tên header là một phần của giao thức, không phải
// chuyện phong cách — viết sai hoa/thường vẫn chạy (HTTP header không phân biệt),
// nhưng viết sai tên thì Zalo trả lỗi khó đoán.
const (
	HeaderAccessToken = "access_token" // getAccessToken() phía Mini App
	HeaderCode        = "code"         // token của getPhoneNumber() — phoneToken trong hợp đồng của ta
	HeaderSecretKey   = "secret_key"   // secret key của Mini App — BÍ MẬT

	// HeaderAppSecretProof: xem ĐIỀU CHƯA RÕ #1. Mặc định không gửi.
	HeaderAppSecretProof = "appsecret_proof"
)

// phanHoiLayThongTin là hình dạng JSON quan sát được:
//
//	{"data": {"number": "84900000000"}, "error": 0, "message": "Success"}
//
// Khi lỗi, "data" có thể rỗng hoặc vắng mặt, "error" khác 0.
type phanHoiLayThongTin struct {
	Data struct {
		Number string `json:"number"`
	} `json:"data"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

// ErrSoKhongHopLe — Zalo trả 200 và error=0 nhưng số không dùng được.
// Không đoán, không chữa cháy: một số sai còn tệ hơn một lần đăng nhập hỏng.
var ErrSoKhongHopLe = errors.New("zalo: số điện thoại trả về không đúng định dạng mong đợi")

var chiSo = regexp.MustCompile(`^[0-9]{9,15}$`)

// ChuanHoaSo đưa số Zalo trả về một dạng duy nhất để lưu: chuỗi chữ số bắt đầu
// bằng mã quốc gia 84, không dấu cộng, không khoảng trắng.
//
// Chuẩn hoá tại ĐÚNG MỘT CHỖ là điều kiện để cột duy nhất trên nguoi_dung có
// nghĩa: "0900000000" và "84900000000" mà lọt vào CSDL thành hai hàng thì cùng
// một người sẽ có hai danh tính, và không cách nào gộp lại mà không mất dữ liệu.
//
// KHÔNG log tham số lẫn kết quả của hàm này: cả hai đều là dữ liệu cá nhân.
func ChuanHoaSo(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.TrimPrefix(s, "+")

	switch {
	case s == "":
		return "", ErrSoKhongHopLe
	case strings.HasPrefix(s, "0"):
		s = "84" + strings.TrimPrefix(s, "0")
	}
	if !chiSo.MatchString(s) {
		// Thông điệp lỗi KHÔNG chứa giá trị — nó sẽ đi vào log.
		return "", ErrSoKhongHopLe
	}
	return s, nil
}
