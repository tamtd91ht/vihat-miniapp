package zalo

import (
	"context"
	"strings"
	"time"
)

// ===========================================================================
//  ĐƯỜNG CHẨN ĐOÁN — dùng bởi cmd/thu-zalo, KHÔNG dùng trong đường phục vụ.
//
//  Vì sao nó nằm trong gói này chứ không nằm trong cmd/thu-zalo: một lệnh thử
//  tự dựng lấy lời gọi HTTP của riêng nó thì một lần chạy xanh KHÔNG chứng minh
//  được gì về đường chạy thật — nó chỉ chứng minh cái bản sao ấy chạy được.
//  ChanDoan đi qua ĐÚNG hàm goi() mà LaySoDienThoai dùng: cùng đường dẫn, cùng
//  tên header, cùng cách ký appsecret_proof, cùng cách phân loại lỗi.
//
//  ĐIỀU KIỆN KHÔNG ĐƯỢC PHÁ: KetQuaChanDoan KHÔNG BAO GIỜ mang số điện thoại.
//  Nó chỉ mang HÌNH DẠNG của số (độ dài + hai ký tự đầu) — đủ để biết Zalo trả
//  "84…" hay "09…" hay "+84…", không đủ để nhận ra ai. Số nguyên văn không rời
//  khỏi gói này qua đường chẩn đoán.
// ===========================================================================

// HinhDangSo là thứ duy nhất được phép nói ra về số điện thoại trên đường chẩn đoán.
type HinhDangSo struct {
	DoDai int // số ký tự

	// HaiKyTuDau — CỐ Ý là "ký tự" chứ không phải "chữ số": chuỗi thô của Zalo có
	// thể bắt đầu bằng "+", và biết được điều đó chính là thứ đóng ĐIỀU CHƯA RÕ #3
	// trong wire.go.
	HaiKyTuDau string
}

// gioiHanMessage cắt message của Zalo trước khi in ra terminal. Đây là chuỗi ta
// không kiểm soát; một phản hồi rác dài vài chục KB làm hỏng màn hình người chạy
// và che mất phần chẩn đoán bên dưới.
const gioiHanMessage = 200

// KetQuaChanDoan là toàn bộ thứ quan sát được từ một lời gọi thật sang Zalo.
//
// Dùng để IN RA TERMINAL của người đang chạy lệnh, và để dán vào phiếu. Vì thế
// nó không mang số điện thoại, không mang token, không mang secret key — dán
// nguyên khối đầu ra này vào đâu cũng an toàn.
type KetQuaChanDoan struct {
	GuiProof bool // lời gọi này có gửi appsecret_proof hay không

	HTTPStatus int           // 0 nghĩa là không với tới được máy chủ
	ThoiGian   time.Duration // đo quanh đúng lời gọi HTTP

	// ZaloError / ZaloMessage: NGUYÊN VĂN Zalo trả, vì đóng được ĐIỀU CHƯA RÕ #2
	// (bảng mã lỗi) thì phải nhìn thấy mã và câu nguyên bản.
	//
	// KHÔNG BAO GIỜ ĐƯA HAI TRƯỜNG NÀY VÀO log.*: message là chuỗi của bên thứ ba
	// và không có gì bảo đảm nó không nhắc lại dữ liệu người dùng. Đường duy nhất
	// của chúng là stdout của một người đang ngồi trước máy.
	ZaloError   int
	ZaloMessage string
	CoThanJSON  bool // phản hồi có phân tích được thành JSON như wire.go giả định không

	CoSo      bool       // Zalo có trả về một số dùng được không
	DangTho   HinhDangSo // hình dạng chuỗi Zalo trả nguyên văn
	DangChuan HinhDangSo // hình dạng sau ChuanHoaSo — lệch với DangTho là có chuẩn hoá xảy ra
}

// ChanDoan gọi THẬT sang Zalo và trả về thứ quan sát được.
//
// Lỗi trả về là lỗi ĐÃ PHÂN LOẠI (ErrTokenKhongHopLe / ErrKhongVoiToiZalo) —
// đúng lỗi mà tầng HTTP sẽ nhận trong sản xuất, để người chạy thấy được lời gọi
// này sẽ thành 401 hay 502 với người dùng thật.
func (c *Client) ChanDoan(ctx context.Context, accessToken, phoneToken string) (KetQuaChanDoan, error) {
	kq := c.goi(ctx, accessToken, phoneToken)

	cd := KetQuaChanDoan{
		GuiProof:    kq.GuiProof,
		HTTPStatus:  kq.HTTPStatus,
		ThoiGian:    kq.ThoiGian,
		ZaloError:   kq.ZaloError,
		ZaloMessage: donMessage(kq.ZaloMessage, kq.SoTho, kq.SoChuan),
		CoThanJSON:  kq.CoThanJSON,
		CoSo:        kq.SoChuan != "",
		DangTho:     hinhDang(kq.SoTho),
		DangChuan:   hinhDang(kq.SoChuan),
	}
	return cd, kq.Loi
}

// hinhDang rút đúng hai thông tin được phép nói ra về một số điện thoại.
func hinhDang(s string) HinhDangSo {
	r := []rune(s)
	hd := HinhDangSo{DoDai: len(r)}
	if len(r) >= 2 {
		hd.HaiKyTuDau = string(r[:2])
	} else if len(r) == 1 {
		hd.HaiKyTuDau = string(r[:1])
	}
	return hd
}

// donMessage chuẩn bị message của Zalo cho màn hình: cắt ngắn, và GẠCH số điện
// thoại nếu Zalo nhắc lại nó trong message.
//
// Vì sao phải gạch dù đầu ra chỉ tới terminal: đầu ra của lệnh này được thiết
// kế để DÁN VÀO PHIẾU. Một khối văn bản "an toàn để dán" mà thỉnh thoảng lại
// mang theo số điện thoại thì nguy hiểm hơn một khối luôn mang — vì không ai
// còn kiểm nữa.
func donMessage(msg, soTho, soChuan string) string {
	msg = strings.TrimSpace(msg)
	for _, so := range []string{soTho, soChuan} {
		if len(so) >= 6 {
			msg = strings.ReplaceAll(msg, so, "[số đã ẩn]")
		}
	}
	r := []rune(msg)
	if len(r) > gioiHanMessage {
		return string(r[:gioiHanMessage]) + "…[cắt]"
	}
	return msg
}
