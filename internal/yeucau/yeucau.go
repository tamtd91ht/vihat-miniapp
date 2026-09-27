// Package yeucau giữ nghiệp vụ "người dùng giơ tay": yêu cầu tư vấn và yêu cầu
// gọi lại.
//
// VÌ SAO NÓ LÀ MỘT GÓI RIÊNG CHỨ KHÔNG NẰM TRONG httpapi:
//
//	Hai việc ở đây CHẠM VÀO SỐ ĐIỆN THOẠI — tổng đài phải quay ra một máy thật,
//	và ZNS phải gửi tới một máy thật. Ở 0001 có một bất biến đã được giữ suốt:
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
// (migrations/0003). Lệch một chữ thì CSDL từ chối chứ không âm thầm nhận.
const (
	LoaiTuVan  = "tu_van"
	LoaiGoiLai = "goi_lai"
)

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

// ---------------------------------------------------------------------------

// DichVu điều phối ba việc: ghi phiếu, gọi ra, gửi tin.
//
// `tongDai` và `zns` ĐƯỢC PHÉP nil, và đó không phải một thiếu sót: cả hai cần
// cấu hình của bên thứ ba (tổng đài OmiCall, mẫu ZNS đã duyệt). Thiếu cấu hình
// thì tính năng TẮT — hỏng về phía đóng — chứ không phải chạy giả vờ. Xem
// `CoGoiLai`.
type DichVu struct {
	kho     Kho
	tongDai TongDai
	zns     BoGuiZNS
	log     *slog.Logger
	now     func() time.Time
}

func Moi(kho Kho, tongDai TongDai, zns BoGuiZNS, log *slog.Logger) *DichVu {
	if log == nil {
		log = slog.Default()
	}
	return &DichVu{kho: kho, tongDai: tongDai, zns: zns, log: log, now: time.Now}
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
	tt.Loai = LoaiTuVan
	tt.QuanTam = donDanhSach(tt.QuanTam)

	ma, err := d.kho.TaoYeuCau(ctx, tt)
	if err != nil {
		return "", fmt.Errorf("ghi yêu cầu tư vấn: %w", err)
	}

	d.guiXacNhan(ctx, ma, tt.NguoiDungID)
	return ma, nil
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
