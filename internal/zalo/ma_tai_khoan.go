package zalo

import (
	"context"
	"errors"
)

// ===========================================================================
//  MÃ TÀI KHOẢN ZALO — CHỖ NỐI, CHƯA CÓ CÀI ĐẶT THẬT. ĐỌC TRƯỚC KHI "NỐI TẠM".
//
//  Cầu phiên ViGov (internal/vigovcau) cần hai thứ từ accessToken mà kho này
//  CHƯA CÓ CHỨNG CỨ nào về cách lấy:
//
//   1. accessToken ĐƯỢC XÁC MINH bằng secret của app mà KHÔNG cần phoneToken.
//      Lời gọi duy nhất đã đo (GET /v2.0/me/info, wire.go) đòi cả hai token.
//   2. MÃ TÀI KHOẢN ZALO (user id) của token ấy. Hình dạng phản hồi đã quan sát
//      của /v2.0/me/info chỉ có `data.number` — không có trường mã nào
//      (wire.go, phongBi / duLieuSo).
//
//  Đó là UNKNOWN #2 của ADR 0045 phía ViGov: endpoint nào trả mã tài khoản mà
//  không cần quyền số điện thoại, và mã ấy theo từng app hay dùng chung.
//
//  VÌ SAO KHÔNG ĐOÁN MỘT ENDPOINT: tài liệu chính thức không đọc được nguyên
//  văn (wire.go, MỨC CHỨNG CỨ). Một endpoint đoán sai trả lỗi → chỉ là hỏng ồn
//  ào. Một TRƯỜNG đoán sai (đọc nhầm mã của app khác, mã của OA, hay một mã
//  thay đổi theo phiên) là ViGov nhớ xã và gắn danh tính công dân theo một
//  khoá không phải của người ấy — hỏng IM LẶNG, đúng loại không test nào bắt.
//
//  VIỆC PHẢI LÀM để thay chỗ nối này: đo bằng token thật (khuôn `cmd/thu-zalo`),
//  ghi hình dạng đo được vào wire.go kèm mức chứng cứ, rồi viết cài đặt thật
//  cạnh MaTaiKhoanChuaDo — đi qua cùng đường gọi với LaySoDienThoai.
// ===========================================================================

// ErrMaTaiKhoanChuaDo — chưa có cách đã đo để lấy mã tài khoản Zalo. Tầng HTTP
// dịch thành 503: lỗi phía ta, người dùng làm gì cũng không sửa được.
var ErrMaTaiKhoanChuaDo = errors.New("zalo: chưa đo được endpoint trả mã tài khoản Zalo (ADR 0045 UNKNOWN #2) — cầu phiên ViGov từ chối")

// MaTaiKhoanChuaDo là cài đặt DUY NHẤT hôm nay của chỗ nối "xác minh
// accessToken và trả mã tài khoản Zalo": nó luôn từ chối.
//
// Không bao giờ gọi mạng, không nhận secret: không có gì để gửi đi khi chưa
// biết gửi tới đâu.
type MaTaiKhoanChuaDo struct{}

// LayMaTaiKhoan luôn trả ErrMaTaiKhoanChuaDo.
func (MaTaiKhoanChuaDo) LayMaTaiKhoan(context.Context, string) (string, error) {
	return "", ErrMaTaiKhoanChuaDo
}
