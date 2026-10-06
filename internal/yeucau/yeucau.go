// Package yeucau giữ nghiệp vụ "người dùng giơ tay": yêu cầu tư vấn, yêu cầu
// gọi lại, và ba lần "bấm rồi thôi" (chat · nhận / huỷ ưu đãi SMS, 07/10/2026).
//
// VÌ SAO NÓ LÀ MỘT GÓI RIÊNG CHỨ KHÔNG NẰM TRONG httpapi:
//
//	Ba việc ở đây CHẠM VÀO SỐ ĐIỆN THOẠI — tổng đài phải quay ra một máy thật,
//	ZNS phải gửi tới một máy thật, và webhook (07/10/2026) đưa số sang bên
//	nhận để gửi SMS / gọi lại. Ở 0001 có một bất biến đã được giữ suốt:
//	dữ liệu cá nhân DỪNG LẠI Ở TẦNG KHO, thứ đi lên tầng HTTP chỉ là mã định
//	danh (xem phien.KetQuaTao). Gói này là chỗ DUY NHẤT phá lệ ấy, và nó phá lệ
//	trong một phạm vi hẹp đo được: số đi từ kho, qua đúng một biến, thẳng xuống
//	bộ điều hợp, rồi hết. Không vào log, không vào lỗi trả về, không đi ngược
//	lên httpapi.
//
//	Đặt hai việc ấy thẳng trong handler thì bất biến kia mất mà không có gì đánh
//	dấu là nó đã mất — và lần sau sẽ có người thêm một dòng log "để dễ soát".
//
// ⚠ SỐ ĐỂ GỌI/GỬI LUÔN TRA TỪ nguoi_dung_id CỦA PHIÊN. Không một hàm nào trong
// gói này nhận một số điện thoại làm tham số từ bên ngoài. Đó là thứ giữ cho
// tổng đài của ViHAT không trở thành công cụ quay số thuê: người bấm nút chỉ
// gọi được ra đúng máy mà chính họ đã xác thực với Zalo.
package yeucau

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// CHÍNH SÁCH — chủ sản phẩm chốt 22/09/2026.
//
// Để ở đây, dạng hằng, chứ KHÔNG đưa ra biến môi trường: cùng lý do với
// config.TTLPhien — đổi một cam kết với người dùng cuối phải là một thay đổi mã
// nguồn có người review, không phải một dòng env ai sửa cũng được.
// ---------------------------------------------------------------------------

const (
	// TranGoiLai / CuaSoGoiLai — 3 lượt trong 24 giờ, phục vụ 24/7.
	//
	// ⚠ CỬA SỔ TRƯỢT, KHÔNG PHẢI NGÀY LỊCH, và khác biệt ấy không phải chi tiết:
	// "3 lượt mỗi ngày" đọc theo ngày lịch cho phép 3 lượt lúc 23:58 và 3 lượt
	// nữa lúc 00:01 — sáu cuộc gọi ra trong ba phút, tất cả đều "trong trần",
	// đúng vào khung giờ một cuộc gọi tự động gây khó chịu nhất.
	TranGoiLai  = 3
	CuaSoGoiLai = 24 * time.Hour

	// TranZNSMoiNgay — trần thứ hai của ZNS, bên cạnh ràng buộc "đúng một tin
	// mỗi yêu cầu" đã cưỡng chế ở CSDL (0003, chỉ số zns_mot_tin_moi_yeu_cau).
	//
	// Hai trần cho một việc là có chủ đích: trần ở CSDL chặn tin TRÙNG cho cùng
	// một yêu cầu; trần này chặn tin NHIỀU cho cùng một người qua nhiều yêu cầu.
	// ZNS là tin nhắn trả tiền — một vòng lặp hỏng không có trần thứ hai thì
	// thứ dừng nó lại là hoá đơn của Zalo, một tháng sau.
	TranZNSMoiNgay = 5
	CuaSoZNS       = 24 * time.Hour
)

// Loại yêu cầu — đúng các giá trị ràng buộc CHECK của yeu_cau.loai chấp nhận
// (migrations/0003, nới ở 0004). Lệch một chữ thì CSDL từ chối chứ không âm
// thầm nhận.
const (
	LoaiTuVan  = "tu_van"
	LoaiGoiLai = "goi_lai"

	// Ba loại "bấm rồi thôi" (chủ sản phẩm chốt 07/10/2026): không biểu mẫu,
	// không ZNS, không tổng đài — chỉ ghi phiếu và báo sang webhook. Xem
	// `TaoBamNut`.
	LoaiChat         = "chat"
	LoaiNhanUuDaiSMS = "nhan_uu_dai_sms"
	LoaiHuyUuDaiSMS  = "huy_uu_dai_sms"
)

// Tên `kind` trên DÂY (/api/v1/requests và webhook) — một từ vựng khác với mã
// trong cột. ÁNH XẠ HAI CHIỀU Ở ĐÚNG MỘT CHỖ (`LoaiTuKind` / `KindTuLoai`): tầng
// HTTP và bộ báo webhook cùng nói tên dây, và hai bảng ánh xạ là hai bảng sẽ
// lệch — một loại mới thêm vào bảng này mà quên bảng kia thì webhook gửi đi mã
// cột thay cho tên dây, và không gì đỏ lên.
const (
	KindTuVan        = "consult"
	KindGoiLai       = "callback"
	KindChat         = "chat"
	KindNhanUuDaiSMS = "sms_promo"
	KindHuyUuDaiSMS  = "sms_optout"
)

var loaiTheoKind = map[string]string{
	KindTuVan:        LoaiTuVan,
	KindGoiLai:       LoaiGoiLai,
	KindChat:         LoaiChat,
	KindNhanUuDaiSMS: LoaiNhanUuDaiSMS,
	KindHuyUuDaiSMS:  LoaiHuyUuDaiSMS,
}

// LoaiTuKind đổi tên trên dây thành mã của CSDL. Tên lạ → false.
func LoaiTuKind(kind string) (string, bool) {
	loai, ok := loaiTheoKind[kind]
	return loai, ok
}

// KindTuLoai là chiều ngược lại. Mã lạ trả về chính nó thay vì chuỗi rỗng: một
// hàng mang mã ta chưa biết vẫn hiện lên được, thay vì biến mất.
func KindTuLoai(loai string) string {
	for k, l := range loaiTheoKind {
		if l == loai {
			return k
		}
	}
	return loai
}

// laBamNut — ba loại đi đường `TaoBamNut`.
func laBamNut(loai string) bool {
	return loai == LoaiChat || loai == LoaiNhanUuDaiSMS || loai == LoaiHuyUuDaiSMS
}

// TranTenHienThi — trần ký tự (RUNE) của tên hiển thị Zalo. Trùng CHECK
// yeu_cau_ten_hien_thi_co_tran ở migrations/0004.
const TranTenHienThi = 100

// Kết quả một lượt gửi ZNS — đúng các giá trị CHECK của zns_da_gui.ket_qua.
const (
	ZNSThanhCong     = "thanh_cong"
	ZNSThatBai       = "that_bai"
	ZNSBoQuaVuotTran = "bo_qua_vuot_tran"
)

// ErrVuotTranGoiLai là lớp lỗi "đã dùng hết lượt gọi lại trong cửa sổ".
//
// Là một lỗi RIÊNG chứ không phải một `bool`: tầng HTTP phải phân biệt được nó
// với một lỗi hệ thống để trả 429 thay vì 500, và để nói một câu khác hẳn.
var ErrVuotTranGoiLai = errors.New("yeucau: vượt trần gọi lại trong cửa sổ")

// ---------------------------------------------------------------------------
// Các giao diện gói này cần. Cố ý hẹp — một giao diện hẹp là một giao diện giả
// lập được trong test mà không phải dựng CSDL, tổng đài, hay tài khoản Zalo.
// ---------------------------------------------------------------------------

// ThongTinTao là thứ người dùng vừa khai trên màn hình.
//
// KHÔNG CÓ TRƯỜNG SỐ ĐIỆN THOẠI, và không bao giờ được có — xem khối đầu tệp.
type ThongTinTao struct {
	NguoiDungID    string
	Loai           string
	QuanTam        []string
	QuyMo          string
	GhiChu         string
	NguonChienDich string

	// TenHienThi — tên hiển thị Zalo, CHỈ cho loại chat (CSDL cưỡng chế bằng
	// yeu_cau_ten_hien_thi_chi_cho_chat). Người dùng TỰ KHAI, không phải định
	// danh — định danh vẫn là NguoiDungID của phiên. Dữ liệu cá nhân: không log.
	TenHienThi string
}

// SuKienYeuCau — thứ đi sang webhook sau khi một yêu cầu đã được ghi.
//
// Struct NGHIỆP VỤ, không mang thẻ JSON: hình dạng dây sống ở bộ điều hợp
// (`internal/webhook`), cùng khuôn với zns/tongdai.
//
// ⚠ MANG SỐ ĐIỆN THOẠI, TÊN và GHI CHÚ. Nó được dựng ở đúng một chỗ
// (`baoYeuCauMoi`), đi thẳng xuống bộ điều hợp, và không bao giờ vào log.
type SuKienYeuCau struct {
	MaYeuCau       string
	Kind           string // tên trên DÂY, không phải mã cột
	TaoLuc         time.Time
	SoDienThoai    string
	TenHienThi     string
	QuanTam        []string
	QuyMo          string
	GhiChu         string
	NguonChienDich string
}

// TomTat — một dòng trên màn "Yêu cầu của tôi".
//
// KHÔNG mang `GhiChu`: ô ghi chú là thứ người dùng gõ tự do, và trả nó ngược ra
// một tuyến API là dựng thêm một đường cho dữ liệu cá nhân rời khỏi máy chủ mà
// không việc gì trên màn hình cần tới. Người dùng cần biết yêu cầu của họ ĐANG
// Ở ĐÂU, không cần đọc lại chữ chính họ vừa gõ.
type TomTat struct {
	Ma        string
	Loai      string
	TrangThai string
	TaoLuc    time.Time
}

// Kho là phần kho dữ liệu gói này cần.
type Kho interface {
	// TaoYeuCau ghi yêu cầu + dòng lịch sử đầu tiên trong MỘT giao dịch.
	TaoYeuCau(ctx context.Context, tt ThongTinTao) (string, error)
	// DemTrongCuaSo đếm yêu cầu một loại của một người kể từ mốc `tu`.
	DemTrongCuaSo(ctx context.Context, nguoiDungID, loai string, tu time.Time) (int, error)
	// DanhSachCuaToi trả yêu cầu của ĐÚNG người này, mới nhất trước.
	DanhSachCuaToi(ctx context.Context, nguoiDungID string, tran int) ([]TomTat, error)
	// SoDeLienHe trả số điện thoại đã chuẩn hoá của một người dùng.
	//
	// ⚠ ĐÂY LÀ ĐƯỜNG DUY NHẤT DỮ LIỆU CÁ NHÂN ĐI LÊN KHỎI TẦNG KHO trong cả kho
	// mã này. Bên gọi phải chuyền thẳng nó xuống bộ điều hợp và không giữ lại.
	SoDeLienHe(ctx context.Context, nguoiDungID string) (string, error)
	// DemZNSTrongCuaSo đếm số tin đã gửi cho một người kể từ mốc `tu`.
	DemZNSTrongCuaSo(ctx context.Context, nguoiDungID string, tu time.Time) (int, error)
	// GhiVetZNS ghi một dòng vào zns_da_gui. Bảng chỉ ghi thêm.
	GhiVetZNS(ctx context.Context, yeuCauID, nguoiDungID, maMau, ketQua, lyDo string) error
}

// TongDai là bộ điều hợp quay số ra. `maYeuCau` đi kèm để đối soát cuộc gọi
// với phiếu, và vì nó KHÔNG phải dữ liệu cá nhân nên nó được phép vào log.
type TongDai interface {
	GoiLai(ctx context.Context, soDienThoai, maYeuCau string) error
}

// BoGuiZNS là bộ điều hợp gửi tin ZNS từ OA của ViHAT.
type BoGuiZNS interface {
	Gui(ctx context.Context, soDienThoai, maYeuCau string) error
	// MaMau trả mã mẫu ZNS đang dùng — ghi vào vết để trả lời được câu "những
	// tin đã gửi dùng mẫu nào" khi một mẫu bị đổi hoặc bị thu hồi.
	MaMau() string
}

// BaoWebhook là bộ điều hợp báo "có yêu cầu mới" sang hệ thống bên nhận (bên gửi
// SMS, bên gọi lại, bên mở cuộc chat). Cài đặt: internal/webhook.
//
// Gui chạy ĐỒNG BỘ (kể cả lần thử lại của nó); việc tách khỏi tuyến HTTP là của
// `DichVu` — xem `baoYeuCauMoi`. Lỗi trả về KHÔNG được mang nội dung sự kiện.
type BaoWebhook interface {
	Gui(ctx context.Context, sk SuKienYeuCau) error
}

// ThoiHanBaoWebhook — trần cho CẢ việc nền một lần báo: tra số + mọi lượt gửi.
// Rộng hơn hai lượt 5 giây của bộ điều hợp cộng một nhịp chờ, để chính bộ điều
// hợp là thứ quyết định khi nào bỏ cuộc, không phải trần này.
const ThoiHanBaoWebhook = 20 * time.Second

// ---------------------------------------------------------------------------

// DichVu điều phối bốn việc: ghi phiếu, gọi ra, gửi tin, báo webhook.
//
// `tongDai`, `zns` và `webhook` ĐƯỢC PHÉP nil, và đó không phải một thiếu sót:
// cả ba cần cấu hình của bên thứ ba. Thiếu cấu hình thì tính năng TẮT — hỏng về
// phía đóng — chứ không phải chạy giả vờ. Xem `CoGoiLai`.
type DichVu struct {
	kho     Kho
	tongDai TongDai
	zns     BoGuiZNS
	webhook BaoWebhook
	log     *slog.Logger
	now     func() time.Time

	// viecNen đếm các lần báo webhook đang chạy nền — để tắt êm chờ được chúng
	// (`ChoViecNen`) thay vì cắt ngang giữa một lần gửi.
	viecNen sync.WaitGroup
}

func Moi(kho Kho, tongDai TongDai, zns BoGuiZNS, log *slog.Logger) *DichVu {
	if log == nil {
		log = slog.Default()
	}
	return &DichVu{kho: kho, tongDai: tongDai, zns: zns, log: log, now: time.Now}
}

// VoiWebhook bật việc báo mọi yêu cầu mới sang webhook. nil là tắt.
//
// Tách khỏi `Moi` cùng lý do httpapi.VoiYeuCau: mọi chỗ gọi `Moi` hiện có (và
// test của chúng) giữ nguyên, và một hàm dựng năm tham số là hàm sẽ bị truyền
// nhầm thứ tự. ⚠ Truyền GIAO DIỆN nil, không truyền con trỏ nil — xem khối
// nil-interface ở cmd/server.
func (d *DichVu) VoiWebhook(w BaoWebhook) *DichVu {
	d.webhook = w
	return d
}

// ChoViecNen chờ mọi lần báo webhook đang chạy nền xong, tối đa tới khi ctx hết.
// Gọi lúc tắt êm, SAU khi máy chủ HTTP đã ngừng nhận yêu cầu mới.
func (d *DichVu) ChoViecNen(ctx context.Context) error {
	xong := make(chan struct{})
	go func() {
		d.viecNen.Wait()
		close(xong)
	}()
	select {
	case <-xong:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CoGoiLai cho biết tuyến gọi lại có phục vụ được không.
//
// Tầng HTTP hỏi câu này để trả 503 kèm một câu tiếng Việt nói người dùng gọi
// hotline, thay vì nhận phiếu rồi im lặng không ai gọi lại — thứ tệ hơn hẳn
// việc nói thẳng ngay từ đầu.
func (d *DichVu) CoGoiLai() bool { return d.tongDai != nil }

// TaoTuVan ghi một yêu cầu tư vấn rồi gửi tin xác nhận.
//
// Tin xác nhận gửi SAU KHI đã ghi phiếu và KHÔNG làm hỏng kết quả nếu nó hỏng:
// người dùng đã gửi yêu cầu thành công, và một tin nhắn không đi được không làm
// điều đó thành chưa xảy ra. Nó để lại một dòng trong `zns_da_gui` để về sau
// gửi lại được.
func (d *DichVu) TaoTuVan(ctx context.Context, tt ThongTinTao) (string, error) {
	if tt.TenHienThi != "" {
		return "", ErrTenHienThiSaiLoai
	}
	tt.Loai = LoaiTuVan
	tt.QuanTam = donDanhSach(tt.QuanTam)

	ma, err := d.kho.TaoYeuCau(ctx, tt)
	if err != nil {
		return "", fmt.Errorf("ghi yêu cầu tư vấn: %w", err)
	}

	d.baoYeuCauMoi(ctx, ma, tt)
	d.guiXacNhan(ctx, ma, tt.NguoiDungID)
	return ma, nil
}

// ErrTenHienThiSaiLoai — tên hiển thị gửi kèm một loại không phải chat. Tầng
// HTTP đã chặn trước (400); lỗi này là lớp thứ hai, trước cả CHECK của CSDL.
var ErrTenHienThiSaiLoai = errors.New("yeucau: tên hiển thị chỉ đi kèm loại chat")

// TaoBamNut ghi một yêu cầu "bấm rồi thôi" — chat, nhận ưu đãi SMS, huỷ ưu đãi
// SMS — rồi báo webhook. tt.Loai PHẢI là một trong ba loại ấy.
//
// KHÔNG gửi ZNS, có chủ đích: một tin xác nhận cho lần bấm "huỷ nhận SMS" là
// đúng thứ người dùng vừa từ chối, và lần bấm "chat" thì người dùng đang chờ
// chính cuộc chat chứ không chờ một tin báo. Việc tiếp theo của cả ba thuộc về
// bên nhận webhook.
//
// KHÔNG có trần nghiệp vụ riêng (chủ sản phẩm 07/10/2026): ba việc này không
// quay số ra máy ai và không tốn tin trả tiền ở phía kho này; trần theo IP của
// tầng HTTP đủ chặn máy quét.
func (d *DichVu) TaoBamNut(ctx context.Context, tt ThongTinTao) (string, error) {
	if !laBamNut(tt.Loai) {
		return "", fmt.Errorf("yeucau: loại %q không đi đường bấm nút", tt.Loai)
	}
	if tt.TenHienThi != "" && tt.Loai != LoaiChat {
		return "", ErrTenHienThiSaiLoai
	}
	tt.QuanTam = donDanhSach(tt.QuanTam)

	ma, err := d.kho.TaoYeuCau(ctx, tt)
	if err != nil {
		return "", fmt.Errorf("ghi yêu cầu %s: %w", tt.Loai, err)
	}
	d.baoYeuCauMoi(ctx, ma, tt)
	return ma, nil
}

// baoYeuCauMoi báo một yêu cầu ĐÃ GHI sang webhook, CHẠY NỀN.
//
// Ba điều không thương lượng:
//
//  1. CHỈ SAU KHI phiếu đã commit — bên gọi gọi hàm này sau `kho.TaoYeuCau`
//     thành công. Báo trước khi ghi là báo một yêu cầu có thể không tồn tại.
//  2. KHÔNG BAO GIỜ làm chậm hay hỏng tuyến HTTP: goroutine riêng, context
//     tách khỏi yêu cầu (yêu cầu kết thúc thì context của nó bị huỷ, mà lúc ấy
//     việc báo mới bắt đầu), trần `ThoiHanBaoWebhook`, và panic bị nuốt tại
//     chỗ — một bộ điều hợp hỏng không được kéo sập tiến trình đang phục vụ.
//  3. SỐ ĐIỆN THOẠI tra từ PHIÊN (`SoDeLienHe(tt.NguoiDungID)`), không từ thân
//     yêu cầu — cùng luật với tổng đài và ZNS ở đầu gói. Log chỉ có mã yêu cầu
//     và kết cục.
func (d *DichVu) baoYeuCauMoi(ctx context.Context, ma string, tt ThongTinTao) {
	if d.webhook == nil {
		return // chưa cấu hình — tính năng tắt, chỉ ghi CSDL
	}
	taoLuc := d.now().UTC()
	ctxNen := context.WithoutCancel(ctx)

	d.viecNen.Add(1)
	go func() {
		defer d.viecNen.Done()
		defer func() {
			if r := recover(); r != nil {
				// KHÔNG in `r`: một panic giữa lúc dựng sự kiện có thể mang giá trị.
				d.log.Error("báo webhook hỏng giữa chừng (panic)", "ma_yeu_cau", ma)
			}
		}()

		ctx, huy := context.WithTimeout(ctxNen, ThoiHanBaoWebhook)
		defer huy()

		so, err := d.kho.SoDeLienHe(ctx, tt.NguoiDungID)
		if err != nil {
			d.log.Error("không tra được số để báo webhook", "ma_yeu_cau", ma, "loi", err.Error())
			return
		}

		err = d.webhook.Gui(ctx, SuKienYeuCau{
			MaYeuCau:       ma,
			Kind:           KindTuLoai(tt.Loai),
			TaoLuc:         taoLuc,
			SoDienThoai:    so,
			TenHienThi:     tt.TenHienThi,
			QuanTam:        tt.QuanTam,
			QuyMo:          strings.TrimSpace(tt.QuyMo),
			GhiChu:         strings.TrimSpace(tt.GhiChu),
			NguonChienDich: strings.TrimSpace(tt.NguonChienDich),
		})
		if err != nil {
			d.log.Error("webhook không nhận sự kiện", "ma_yeu_cau", ma, "loi", err.Error())
			return
		}
		d.log.Info("webhook đã nhận sự kiện", "ma_yeu_cau", ma)
	}()
}

// TaoGoiLai kiểm trần, ghi phiếu, rồi bảo tổng đài quay số.
//
// THỨ TỰ LÀ MỘT QUYẾT ĐỊNH: ghi phiếu TRƯỚC khi gọi. Gọi trước rồi mới ghi thì
// một lần ghi hỏng để lại một cuộc gọi đã xảy ra mà không có dòng nào nói vì
// sao nó xảy ra — và cuộc gọi ấy cũng không được tính vào trần, nên lần bấm
// tiếp theo lại lọt qua.
func (d *DichVu) TaoGoiLai(ctx context.Context, tt ThongTinTao) (string, error) {
	if d.tongDai == nil {
		return "", errors.New("yeucau: chưa cấu hình tổng đài")
	}
	if tt.TenHienThi != "" {
		return "", ErrTenHienThiSaiLoai
	}

	tu := d.now().Add(-CuaSoGoiLai)
	daDung, err := d.kho.DemTrongCuaSo(ctx, tt.NguoiDungID, LoaiGoiLai, tu)
	if err != nil {
		return "", fmt.Errorf("đếm lượt gọi lại: %w", err)
	}
	if daDung >= TranGoiLai {
		return "", ErrVuotTranGoiLai
	}

	tt.Loai = LoaiGoiLai
	tt.QuanTam = donDanhSach(tt.QuanTam)

	ma, err := d.kho.TaoYeuCau(ctx, tt)
	if err != nil {
		return "", fmt.Errorf("ghi yêu cầu gọi lại: %w", err)
	}
	d.baoYeuCauMoi(ctx, ma, tt)

	so, err := d.kho.SoDeLienHe(ctx, tt.NguoiDungID)
	if err != nil {
		// Phiếu đã ghi. Không quay được số thì vẫn còn một phiếu để người thật
		// gọi lại bằng tay — nên đây KHÔNG phải lỗi trả ra cho người dùng.
		d.log.Error("không tra được số để gọi lại", "ma_yeu_cau", ma, "loi", err.Error())
		return ma, nil
	}

	// `so` KHÔNG vào log ở bất kỳ nhánh nào dưới đây. Thứ được phép ghi là mã
	// yêu cầu — nó tra ngược ra người dùng khi cần, qua CSDL, có kiểm soát.
	if err := d.tongDai.GoiLai(ctx, so, ma); err != nil {
		d.log.Error("tổng đài không nhận lệnh gọi lại", "ma_yeu_cau", ma, "loi", err.Error())
	}
	return ma, nil
}

// DanhSachCuaToi — CHỈ của người đang đăng nhập.
//
// Không có tham số nào cho phép hỏi yêu cầu của người khác, và đó là điều kiện
// để tuyến này an toàn với một định danh yếu (số điện thoại + Zalo): mã định
// danh đến từ PHIÊN, không bao giờ từ thân yêu cầu hay chuỗi truy vấn.
func (d *DichVu) DanhSachCuaToi(ctx context.Context, nguoiDungID string, tran int) ([]TomTat, error) {
	return d.kho.DanhSachCuaToi(ctx, nguoiDungID, tran)
}

// guiXacNhan gửi ĐÚNG MỘT tin ZNS cho một yêu cầu, nếu còn trong trần.
//
// Mọi kết cục đều để lại một dòng trong `zns_da_gui`, kể cả kết cục "bỏ qua vì
// vượt trần". Một lần bỏ qua không có vết là một lần người dùng không nhận được
// tin mà không ai giải thích được vì sao.
func (d *DichVu) guiXacNhan(ctx context.Context, maYeuCau, nguoiDungID string) {
	if d.zns == nil {
		return // chưa cấu hình mẫu ZNS — tính năng tắt, không giả vờ
	}
	maMau := d.zns.MaMau()

	tu := d.now().Add(-CuaSoZNS)
	daGui, err := d.kho.DemZNSTrongCuaSo(ctx, nguoiDungID, tu)
	if err != nil {
		d.log.Error("không đếm được tin đã gửi", "ma_yeu_cau", maYeuCau, "loi", err.Error())
		return // không đếm được thì KHÔNG gửi: hỏng về phía không tốn tiền
	}
	if daGui >= TranZNSMoiNgay {
		d.ghiVet(ctx, maYeuCau, nguoiDungID, maMau, ZNSBoQuaVuotTran, "vuot_tran_ngay")
		return
	}

	so, err := d.kho.SoDeLienHe(ctx, nguoiDungID)
	if err != nil {
		d.log.Error("không tra được số để gửi tin", "ma_yeu_cau", maYeuCau, "loi", err.Error())
		d.ghiVet(ctx, maYeuCau, nguoiDungID, maMau, ZNSThatBai, "khong_tra_duoc_so")
		return
	}

	if err := d.zns.Gui(ctx, so, maYeuCau); err != nil {
		// Thông điệp của Zalo KHÔNG vào cột ly_do: nó có thể chép lại nguyên văn
		// thứ ta vừa gửi lên, kể cả số điện thoại. Vào cột là một MÃ của ta.
		d.log.Error("gửi ZNS hỏng", "ma_yeu_cau", maYeuCau, "loi", err.Error())
		d.ghiVet(ctx, maYeuCau, nguoiDungID, maMau, ZNSThatBai, "zalo_tu_choi")
		return
	}
	d.ghiVet(ctx, maYeuCau, nguoiDungID, maMau, ZNSThanhCong, "")
}

func (d *DichVu) ghiVet(ctx context.Context, maYeuCau, nguoiDungID, maMau, ketQua, lyDo string) {
	if err := d.kho.GhiVetZNS(ctx, maYeuCau, nguoiDungID, maMau, ketQua, lyDo); err != nil {
		d.log.Error("không ghi được vết ZNS", "ma_yeu_cau", maYeuCau, "ket_qua", ketQua, "loi", err.Error())
	}
}

// donDanhSach cắt khoảng trắng, bỏ mục rỗng và bỏ mục trùng, GIỮ THỨ TỰ.
//
// Giữ thứ tự vì đó là thứ tự người dùng bấm trên màn hình, và nó nói được thứ
// họ quan tâm trước. Sắp xếp lại cho "gọn" là vứt đi thông tin ấy.
func donDanhSach(vao []string) []string {
	if len(vao) == 0 {
		return nil
	}
	daCo := make(map[string]struct{}, len(vao))
	ra := make([]string, 0, len(vao))
	for _, v := range vao {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, trung := daCo[v]; trung {
			continue
		}
		daCo[v] = struct{}{}
		ra = append(ra, v)
	}
	return ra
}
