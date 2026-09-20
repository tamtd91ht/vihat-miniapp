package phien

import "time"

// Kết quả của một lần đăng nhập — đúng các giá trị mà ràng buộc CHECK trên
// nhat_ky_dang_nhap.ket_qua chấp nhận (migrations/0001_init.sql).
// Lệch một chữ thì CSDL từ chối chứ không âm thầm nhận: đó là chủ đích.
const (
	KetQuaThanhCong     = "thanh_cong"
	KetQuaTokenZaloHong = "token_zalo_hong"
	KetQuaLoiZalo       = "loi_zalo"
	KetQuaQuaNhieuLan   = "qua_nhieu_lan"
	KetQuaLoiHeThong    = "loi_he_thong"
)

// KetQuaTao là thứ tầng HTTP nhận được sau một lần đăng nhập thành công.
//
// KHÔNG mang số điện thoại, và đó là ràng buộc cố ý: dữ liệu cá nhân dừng lại ở
// tầng kho. Thứ đi lên trên chỉ là MÃ ĐỊNH DANH.
type KetQuaTao struct {
	NguoiDungID string
	PhienID     string
	HetHanLuc   time.Time
}
