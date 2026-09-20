// Package secret giữ đúng một kiểu: giá trị không được phép xuất hiện ở chỗ
// người ta đọc được. Để riêng một gói để gói nào cũng dùng được mà không phải
// kéo theo cả gói config.
package secret

// Secret bọc một giá trị không được phép xuất hiện ở bất cứ đâu người ta đọc được:
// log, thông điệp lỗi, báo cáo sự cố, tài liệu.
//
// Vì sao cần một kiểu riêng thay vì string: một `%v` trên struct cấu hình là đủ để
// đẩy secret key của Mini App vào log tập trung, và log thì đã nhân bản đi khắp nơi
// trước khi ai đó kịp nhận ra. Kiểu này làm cho mọi đường in ra mặc định là AN TOÀN,
// còn lấy giá trị thật phải viết rõ `.Lo()` — một lời gọi mà người review nhìn thấy.
type Secret string

const secretMask = "[đã ẩn]"

// String cho fmt.Stringer — chặn %v, %s, %q.
func (s Secret) String() string { return secretMask }

// GoString cho %#v.
func (s Secret) GoString() string { return secretMask }

// MarshalJSON chặn con đường json.Marshal(cfg).
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + secretMask + `"`), nil }

// Lo trả giá trị thật. Chỉ gọi ngay tại chỗ dùng nó (ký, đặt header), không gán
// vào biến trung gian, không log, không đưa vào thông điệp lỗi.
func (s Secret) Lo() string { return string(s) }
