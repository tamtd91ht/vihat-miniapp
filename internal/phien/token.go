// Package phien giữ phần "phiên đăng nhập" không dính CSDL và không dính HTTP:
// sinh bearer token và băm nó.
package phien

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// soByteToken = 32 byte ngẫu nhiên từ crypto/rand.
// 256 bit là ngưỡng không ai dò được bằng vét cạn; ngắn hơn thì một tuyến công
// khai như POST /api/v1/sessions biến thành chỗ thử token.
const soByteToken = 32

// SinhToken trả bearer token dạng base64url không đệm.
//
// Đây là chuỗi DUY NHẤT người dùng cầm. CSDL chỉ giữ bản băm của nó (xem Bam),
// nên sau lời gọi này không có đường nào lấy lại token từ hệ thống.
func SinhToken() (string, error) {
	b := make([]byte, soByteToken)
	if _, err := rand.Read(b); err != nil {
		// Hỏng thì ĐÓNG: thà không cấp phiên còn hơn cấp một phiên đoán được.
		return "", fmt.Errorf("sinh token phiên: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Bam trả SHA-256 của token — đúng 32 byte, khớp ràng buộc của cột phien.token_bam.
//
// Không dùng bcrypt/argon2 ở đây là có chủ đích: token đã là 256 bit ngẫu nhiên,
// không phải mật khẩu người đặt, nên không có gì để "dò từ điển". Đổi lại,
// SHA-256 đủ nhanh để tra phiên trên mỗi yêu cầu.
func Bam(token string) []byte {
	tong := sha256.Sum256([]byte(token))
	return tong[:]
}
