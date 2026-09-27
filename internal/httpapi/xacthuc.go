package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/store"
)

// TUYẾN CẦN XÁC THỰC — bề mặt này ra đời cùng 0003.
//
// Tới đây kho mã chỉ có tuyến CÔNG KHAI: đăng nhập, healthz, webhook. Từ lượt
// này có tuyến đọc và ghi dữ liệu CỦA MỘT NGƯỜI CỤ THỂ, và câu hỏi "người nào"
// chỉ có đúng một câu trả lời hợp lệ.
//
// ⚠ MÃ ĐỊNH DANH ĐẾN TỪ PHIÊN, KHÔNG BAO GIỜ TỪ YÊU CẦU.
//
//	Không một handler nào được đọc `nguoi_dung_id` từ thân, từ chuỗi truy vấn,
//	hay từ một header. Định danh ở đây YẾU — nó là một số điện thoại cộng một
//	lần bấm đồng ý trong Zalo — nên nó không chịu nổi thêm bất kỳ chỗ nào để
//	người gọi tự khai mình là ai. Một tham số `?nguoiDung=` là một tuyến đọc dữ
//	liệu của người khác bằng cách đổi một chữ số.
//
//	Cách duy nhất lấy mã ra là `nguoiDungTu(ctx)`, và giá trị ấy chỉ do
//	`doiPhien` đặt vào sau khi đã đối chiếu bản băm của bearer với bảng `phien`.

// khoaNguoiDung là kiểu khoá RIÊNG của gói, không xuất ra.
//
// Kiểu riêng chứ không phải một chuỗi: hai gói cùng dùng chuỗi "nguoi_dung" làm
// khoá context sẽ ghi đè lẫn nhau mà không có gì báo, và giá trị đọc ra là giá
// trị của gói nào chạy sau. Kiểu riêng thì va chạm ấy không tồn tại được.
type khoaNguoiDung struct{}

// nguoiDungTu lấy mã định danh mà `doiPhien` đã đặt vào.
//
// Trả chuỗi rỗng khi không có — và mọi bên gọi phải coi chuỗi rỗng là "chưa xác
// thực" rồi dừng lại. Không có đường nào để một handler chạy tiếp với mã rỗng.
func nguoiDungTu(ctx context.Context) string {
	ma, _ := ctx.Value(khoaNguoiDung{}).(string)
	return ma
}

// KhoPhien là phần kho tầng xác thực cần. Hẹp đúng một hàm.
type KhoPhien interface {
	TraPhienConHieuLuc(ctx context.Context, tokenBam []byte) (string, error)
}

const loiChuaDangNhap = "Bạn cần đăng nhập để dùng chức năng này. Vui lòng mở màn hình Liên hệ và đăng nhập bằng số Zalo."

// doiPhien bọc một handler, đòi bearer hợp lệ trước khi cho đi tiếp.
//
// BA ĐIỀU KIỆN (tồn tại · chưa hết hạn · chưa thu hồi) được kiểm trong MỘT câu
// SQL ở tầng kho, không tách ra đây — xem `store.TraPhienConHieuLuc`.
//
// KHÔNG PHÂN BIỆT "token sai" VỚI "token hết hạn" TRONG CÂU TRẢ VỀ, và đó là
// chủ đích: hai câu khác nhau nói cho người đang dò token biết họ dò đúng hướng
// hay chưa. Người dùng thật thì làm cùng một việc trong cả hai ca — đăng nhập lại.
func (s *Server) doiPhien(tiep http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearerTu(r)
		if !ok {
			s.traLoi(w, http.StatusUnauthorized, loiChuaDangNhap)
			return
		}

		// Chỉ BẢN BĂM đi xuống kho. Token nguyên bản không bao giờ vào một câu
		// SQL, nên nó không nằm lại trong pg_stat_statements hay log truy vấn.
		nguoiDungID, err := s.khoPhien.TraPhienConHieuLuc(r.Context(), phien.Bam(tok))
		if errors.Is(err, store.ErrKhongCoPhien) {
			s.traLoi(w, http.StatusUnauthorized, loiChuaDangNhap)
			return
		}
		if err != nil {
			s.log.Error("không tra được phiên", "loi", err.Error())
			s.traLoi(w, http.StatusInternalServerError, loiHeThong)
			return
		}

		ctx := context.WithValue(r.Context(), khoaNguoiDung{}, nguoiDungID)
		tiep(w, r.WithContext(ctx))
	}
}

// bearerTu đọc `Authorization: Bearer <token>`.
//
// So sánh lược đồ KHÔNG PHÂN BIỆT HOA THƯỜNG (RFC 7235 nói lược đồ là
// case-insensitive), nhưng token thì giữ NGUYÊN VĂN: nó là base64url, và
// `strings.ToLower` lên một token là biến mọi token có chữ hoa thành token sai —
// một lỗi chỉ xuất hiện với khoảng một nửa số người dùng.
func bearerTu(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	phan := strings.SplitN(h, " ", 2)
	if len(phan) != 2 || !strings.EqualFold(phan[0], "Bearer") {
		return "", false
	}
	tok := strings.TrimSpace(phan[1])
	if tok == "" {
		return "", false
	}
	return tok, true
}
