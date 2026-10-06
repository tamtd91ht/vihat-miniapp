package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// BỀ MẶT "NGƯỜI DÙNG GIƠ TAY" — MỘT TÀI NGUYÊN, KHÔNG PHẢI HAI.
//
//	POST /api/v1/requests   tạo một yêu cầu (tư vấn · gọi lại · chat ·
//	                        nhận / huỷ ưu đãi SMS)
//	GET  /api/v1/requests   yêu cầu CỦA CHÍNH MÌNH
//
// Vì sao một tài nguyên với một trường `kind`, chứ không phải `/consultations`
// và `/callbacks` riêng: người dùng nhìn thấy MỘT danh sách "Yêu cầu của tôi"
// trộn cả hai loại, và hai bộ sưu tập ở tuyến ghi mà một ở tuyến đọc là chỗ hai
// bên sẽ lệch — tuyến đọc phải hợp nhất hai thứ mà tuyến ghi coi là khác nhau.
//
// ⚠ TÊN TÀI NGUYÊN VÀ TÊN TRƯỜNG VIẾT TIẾNG ANH camelCase, khác mọi tệp còn lại
// trong kho. Đó là khuôn của DÂY, không phải khuôn của ta — `/api/v1/sessions`
// đã đặt khuôn ấy, và phía Mini App đã viết theo. Việt hoá nửa bề mặt cho "đồng
// bộ với mã nguồn" là tạo ra một bản dịch hai bên phải nhớ.
//
// ⚠ GIÁ TRỊ `status` TRẢ VỀ LÀ MÃ, KHÔNG PHẢI CÂU CHỮ ("moi", "dang_xu_ly"…).
// Câu tiếng Việt hiện cho người dùng sống ở Mini App. Trả câu chữ từ đây nghĩa
// là đổi một nhãn trên màn hình phải phát hành lại máy chủ, và nghĩa là hai bản
// dịch của cùng một trạng thái khi Mini App cũng có nhãn của nó.

// Mức giới hạn của bề mặt này: 20 lượt / 5 phút / IP.
//
// Rộng hơn tuyến đăng nhập (10/5 phút) vì đây là bề mặt người dùng thao tác
// nhiều lần trong một phiên — gửi yêu cầu rồi mở lại danh sách vài lần là bình
// thường. Nó KHÔNG phải trần nghiệp vụ: trần gọi lại (3 lượt/24 giờ/người) nằm
// ở `internal/yeucau` và đếm theo NGƯỜI, không theo IP. Hai lớp cho hai việc
// khác nhau — một lớp chặn máy quét, một lớp giữ lời hứa với người dùng thật.
const (
	SoLuotYeuCauToiDa = 20
	CuaSoYeuCau       = 5 * time.Minute
)

// Giá trị hợp lệ của `kind` trên dây. ÁNH XẠ SANG MÃ CỦA CSDL Ở ĐÚNG MỘT CHỖ
// (`yeucau.LoaiTuKind` / `yeucau.KindTuLoai`): tên trên dây và tên trong cột là
// hai từ vựng, và trộn chúng lại nghĩa là đổi một cái thì cái kia gãy theo mà
// không có gì báo. Bảng ấy nằm ở `yeucau` chứ không ở đây vì webhook cũng nói
// tên dây (07/10/2026).
const (
	kindTuVan  = yeucau.KindTuVan
	kindGoiLai = yeucau.KindGoiLai
)

const (
	loiThanYeuCauHong   = "Yêu cầu không hợp lệ. Vui lòng thử lại."
	loiLoaiKhongHopLe   = "Loại yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử lại."
	loiGhiChuQuaDai     = "Phần ghi chú quá dài. Vui lòng rút ngắn còn tối đa 2000 ký tự."
	loiTruongKhongHopLe = "Thông tin gửi lên không hợp lệ. Vui lòng mở lại ứng dụng và thử lại."
	loiVuotTranGoiLai   = "Bạn đã yêu cầu gọi lại 3 lần trong 24 giờ qua. Vui lòng gọi hotline nếu cần gấp."
	loiChuaCoTongDai    = "Chức năng gọi lại đang tạm ngưng. Vui lòng gọi hotline để được hỗ trợ ngay."
)

// gioiHanThanYeuCau: thân có một ô ghi chú 2000 ký tự, mà một ký tự tiếng Việt
// tốn tới 3 byte UTF-8. 32KB đủ rộng cho thân hợp lệ dài nhất và vẫn chặn được
// một thân vô hạn. Trần ký tự THẬT nằm ở `kiemGhiChu` — đếm theo RUNE.
const gioiHanThanYeuCau = 32 << 10

// maNgan: bộ ký tự của mọi MÃ do ứng dụng đặt (`interests`, `scale`, `source`).
//
// HẸP CÓ CHỦ ĐÍCH. Ba giá trị này là mã chọn từ một danh sách trên màn hình,
// không phải chữ người dùng gõ — nên chúng không cần dấu tiếng Việt, không cần
// khoảng trắng, và không được phép có gì khác. Chúng đi vào báo cáo hiệu quả
// chiến dịch, và một ngày nào đó sẽ có người ghép chúng vào một truy vấn ở một
// công cụ không ai kiểm soát được.
var maNgan = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type yeuCauTao struct {
	Kind      string   `json:"kind"`
	Interests []string `json:"interests"`
	Scale     string   `json:"scale"`
	Note      string   `json:"note"`
	Source    string   `json:"source"`
	// DisplayName — tên hiển thị Zalo, CHỈ với kind "chat". Người dùng tự khai,
	// KHÔNG phải định danh: định danh vẫn đến từ phiên. Dữ liệu cá nhân.
	DisplayName string `json:"displayName"`
}

type phanHoiYeuCau struct {
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type phanHoiDanhSach struct {
	Items []phanHoiYeuCau `json:"items"`
}

// taoYeuCau — POST /api/v1/requests. CẦN XÁC THỰC (bọc bởi `doiPhien`).
//
// Mã định danh người dùng đến TỪ PHIÊN. Thân yêu cầu không có, và sẽ không bao
// giờ có, một trường nào nói "tôi là ai" — xem khối đầu `xacthuc.go`.
func (s *Server) taoYeuCau(w http.ResponseWriter, r *http.Request) {
	nguoiDungID := nguoiDungTu(r.Context())
	if nguoiDungID == "" {
		// Không thể xảy ra nếu tuyến được bọc đúng. Hỏng về phía ĐÓNG thay vì
		// chạy tiếp với một mã rỗng — một yêu cầu không thuộc về ai là một hàng
		// không ai tra ra được, và nó ghi vào CSDL thì ở lại đó.
		s.traLoi(w, http.StatusUnauthorized, loiChuaDangNhap)
		return
	}

	var yc yeuCauTao
	if err := json.NewDecoder(io.LimitReader(r.Body, gioiHanThanYeuCau)).Decode(&yc); err != nil {
		// KHÔNG trả `err` ra ngoài: thông điệp của bộ giải mã JSON chép lại
		// nguyên văn thứ client gửi lên, và ở tuyến này thứ ấy là ô ghi chú.
		s.traLoi(w, http.StatusBadRequest, loiThanYeuCauHong)
		return
	}

	loai, ok := loaiTu(yc.Kind)
	if !ok {
		s.traLoi(w, http.StatusBadRequest, loiLoaiKhongHopLe)
		return
	}

	ghiChu := strings.TrimSpace(yc.Note)
	if utf8.RuneCountInString(ghiChu) > 2000 {
		// Đếm theo RUNE, không theo byte: "quá 2000 ký tự" là câu người dùng
		// đọc, và một câu tiếng Việt 800 chữ có thể vượt 2000 BYTE mà chưa tới
		// 2000 ký tự. Trần byte đã chặn ở `gioiHanThanYeuCau` cho việc khác.
		s.traLoi(w, http.StatusBadRequest, loiGhiChuQuaDai)
		return
	}
	if !hopLeMaNgan(yc.Scale) || !hopLeMaNgan(yc.Source) || !hopLeDanhSachMa(yc.Interests) {
		s.traLoi(w, http.StatusBadRequest, loiTruongKhongHopLe)
		return
	}

	// Tên hiển thị: CHỈ với chat, tối đa 100 KÝ TỰ (rune, như ghi chú). Gửi kèm
	// loại khác là 400 chứ không lặng lẽ bỏ: một client gửi tên ở chỗ không cần
	// là một client đang hiểu sai hợp đồng, và nói ra bây giờ rẻ hơn để một cái
	// tên người nằm lại trên phiếu "huỷ nhận SMS".
	tenHienThi := strings.TrimSpace(yc.DisplayName)
	if tenHienThi != "" && (loai != yeucau.LoaiChat || utf8.RuneCountInString(tenHienThi) > yeucau.TranTenHienThi) {
		s.traLoi(w, http.StatusBadRequest, loiTruongKhongHopLe)
		return
	}

	tt := yeucau.ThongTinTao{
		NguoiDungID:    nguoiDungID,
		Loai:           loai,
		QuanTam:        yc.Interests,
		QuyMo:          strings.TrimSpace(yc.Scale),
		GhiChu:         ghiChu,
		NguonChienDich: strings.TrimSpace(yc.Source),
		TenHienThi:     tenHienThi,
	}

	var ma string
	var err error
	switch loai {
	case yeucau.LoaiGoiLai:
		if !s.yeuCau.CoGoiLai() {
			// Nhận phiếu rồi không ai gọi lại là tệ hơn hẳn việc nói thẳng ngay
			// bây giờ, kèm một đường đi được: hotline.
			s.traLoi(w, http.StatusServiceUnavailable, loiChuaCoTongDai)
			return
		}
		ma, err = s.yeuCau.TaoGoiLai(r.Context(), tt)
	case yeucau.LoaiTuVan:
		ma, err = s.yeuCau.TaoTuVan(r.Context(), tt)
	default:
		// chat · nhận / huỷ ưu đãi SMS — ghi phiếu rồi báo webhook, thôi.
		ma, err = s.yeuCau.TaoBamNut(r.Context(), tt)
	}

	if errors.Is(err, yeucau.ErrVuotTranGoiLai) {
		s.traLoi(w, http.StatusTooManyRequests, loiVuotTranGoiLai)
		return
	}
	if err != nil {
		s.log.Error("không ghi được yêu cầu", "loai", loai, "loi", err.Error())
		s.traLoi(w, http.StatusInternalServerError, loiHeThong)
		return
	}

	// Chỉ ghi MÃ. Không ghi `interests`, không ghi `scale`, và tuyệt đối không
	// ghi `note` hay `displayName` — văn bản tự do và tên người.
	s.log.Info("nhận yêu cầu", "ma_yeu_cau", ma, "loai", loai)

	s.traJSON(w, http.StatusCreated, phanHoiYeuCau{
		RequestID: ma,
		Kind:      yc.Kind,
		Status:    "moi",
		CreatedAt: s.now().UTC().Format(time.RFC3339),
	})
}

// danhSachYeuCau — GET /api/v1/requests. CẦN XÁC THỰC.
//
// CHỈ trả yêu cầu của người đang đăng nhập. Không có tham số nào — không
// `?nguoiDung=`, không `?phone=` — và việc KHÔNG CÓ chúng chính là tính năng:
// một tuyến không nhận định danh từ client là một tuyến không đọc nhầm được dữ
// liệu của ai.
func (s *Server) danhSachYeuCau(w http.ResponseWriter, r *http.Request) {
	nguoiDungID := nguoiDungTu(r.Context())
	if nguoiDungID == "" {
		s.traLoi(w, http.StatusUnauthorized, loiChuaDangNhap)
		return
	}

	ds, err := s.yeuCau.DanhSachCuaToi(r.Context(), nguoiDungID, 20)
	if err != nil {
		s.log.Error("không đọc được danh sách yêu cầu", "loi", err.Error())
		s.traLoi(w, http.StatusInternalServerError, loiHeThong)
		return
	}

	// Mảng RỖNG, không phải `null`: `null` buộc phía Mini App phải kiểm thêm một
	// nhánh, và nhánh ấy là nhánh sẽ quên.
	items := make([]phanHoiYeuCau, 0, len(ds))
	for _, t := range ds {
		items = append(items, phanHoiYeuCau{
			RequestID: t.Ma,
			Kind:      kindTu(t.Loai),
			Status:    t.TrangThai,
			CreatedAt: t.TaoLuc.UTC().Format(time.RFC3339),
		})
	}
	s.traJSON(w, http.StatusOK, phanHoiDanhSach{Items: items})
}

// requests định tuyến theo phương thức. Một đường dẫn, hai việc.
func (s *Server) requests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.taoYeuCau(w, r)
	case http.MethodGet:
		s.danhSachYeuCau(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.traLoi(w, http.StatusMethodNotAllowed, loiThanYeuCauHong)
	}
}

// loaiTu / kindTu — lối tắt tới bảng ánh xạ DUY NHẤT ở `yeucau`.
func loaiTu(kind string) (string, bool) { return yeucau.LoaiTuKind(kind) }
func kindTu(loai string) string         { return yeucau.KindTuLoai(loai) }

// hopLeMaNgan: rỗng là hợp lệ (trường tuỳ chọn), có thì phải đúng khuôn mã.
func hopLeMaNgan(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || maNgan.MatchString(v)
}

func hopLeDanhSachMa(ds []string) bool {
	if len(ds) > 8 {
		return false
	}
	for _, v := range ds {
		if strings.TrimSpace(v) == "" || !maNgan.MatchString(strings.TrimSpace(v)) {
			return false
		}
	}
	return true
}
