// Package zns gửi tin ZNS từ Official Account của ViHAT.
//
// ⚠ ĐỌC KHỐI NÀY TRƯỚC KHI BẬT TÍNH NĂNG TRÊN MÔI TRƯỜNG THẬT.
//
// HÌNH DẠNG DÂY Ở ĐÂY VIẾT TỪ NGUỒN THỨ CẤP, CHƯA ĐO BẰNG MỘT LỜI GỌI THẬT —
// khác hẳn `internal/zalo/wire.go`, nơi từng dòng được đo bằng `cmd/thu-zalo`.
// Sự khác biệt ấy được nói ra ở đây thay vì giấu đi, vì nó quyết định thứ tự
// việc phải làm:
//
//  1. lấy access token của OA và một `template_id` ĐÃ ĐƯỢC ZALO DUYỆT;
//  2. gọi thật MỘT lần, đối chiếu tên trường và hình dạng phản hồi với tệp này;
//  3. chỉ sau đó mới bật `ZALO_ZNS_*` trên môi trường thật.
//
// Thiếu cấu hình thì tính năng TẮT (xem `Moi` trả nil) — không có đường nào để
// nó chạy giả vờ, và không có mẫu nào được gửi đi bằng một hình dạng đoán.
//
// ⚠ NỢ ĐÃ BIẾT — VÒNG ĐỜI ACCESS TOKEN CỦA OA.
//
//	Access token của OA có hạn và phải làm mới bằng refresh token. Gói này nhận
//	một token TĨNH từ cấu hình và KHÔNG tự làm mới. Hệ quả thẳng thắn: tới ngày
//	token hết hạn, mọi lượt gửi hỏng, mỗi lượt để lại một dòng `that_bai` trong
//	`zns_da_gui` với lý do `zalo_tu_choi` — yêu cầu của người dùng vẫn được ghi
//	nhận bình thường, chỉ tin xác nhận là không tới.
//
//	Viết vòng làm mới ngay bây giờ sẽ là viết nó mù: nó cần chính những giá trị
//	mà bước 1 ở trên chưa có. Nợ này được ghi ở đây và trong README, chứ không
//	được che bằng một vòng thử lại trông như đang xử lý.
//
// ⚠ SỐ ĐIỆN THOẠI ĐI VÀO ĐÂY VÀ KHÔNG ĐI RA. Không `log` nó ở bất kỳ nhánh nào,
// kể cả nhánh lỗi; thứ được phép ghi là `maYeuCau`. Thông điệp lỗi của Zalo
// cũng KHÔNG được chuyển nguyên văn lên trên — nó chép lại thân ta vừa gửi.
package zns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vihat/vihat-miniapp/internal/secret"
)

// BaseURLMacDinh — cổng gửi tin của Zalo Business Open API.
const BaseURLMacDinh = "https://business.openapi.zalo.me"

// DuongDanGuiMau — tuyến gửi một tin dựng từ mẫu đã duyệt.
const DuongDanGuiMau = "/message/template"

// ErrZaloTuChoi — Zalo nhận được yêu cầu và từ chối nó (mẫu sai, token hết hạn,
// số không nhận ZNS). Một lớp lỗi riêng để bên gọi ghi đúng mã lý do.
var ErrZaloTuChoi = errors.New("zns: Zalo từ chối yêu cầu gửi")

type Client struct {
	baseURL string
	token   secret.Secret
	maMau   string
	hc      *http.Client
}

// TuyChon để test thay HTTP client — cùng khuôn với internal/zalo.
type TuyChon func(*Client)

func VoiHTTPClient(hc *http.Client) TuyChon { return func(c *Client) { c.hc = hc } }

// Moi dựng bộ gửi, hoặc trả nil khi CHƯA ĐỦ CẤU HÌNH.
//
// TRẢ nil CHỨ KHÔNG TRẢ LỖI, và đó là chủ đích: thiếu mẫu ZNS không được phép
// chặn cả service khởi động. Tin xác nhận là thứ TỐT khi có; yêu cầu của người
// dùng vẫn phải ghi nhận được khi chưa có. Bên gọi kiểm nil và tắt tính năng —
// xem `yeucau.DichVu`.
func Moi(baseURL string, token secret.Secret, maMau string, tuyChon ...TuyChon) *Client {
	if token.Lo() == "" || maMau == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = BaseURLMacDinh
	}
	c := &Client{
		baseURL: baseURL,
		token:   token,
		maMau:   maMau,
		// Timeout BẮT BUỘC: không có nó thì một lần Zalo treo sẽ giữ một
		// goroutine và một kết nối CSDL cho tới khi tiến trình chết.
		hc: &http.Client{Timeout: 10 * time.Second},
	}
	for _, t := range tuyChon {
		t(c)
	}
	return c
}

// MaMau trả mã mẫu đang dùng — ghi vào vết `zns_da_gui.ma_mau`.
func (c *Client) MaMau() string { return c.maMau }

type thanGui struct {
	Phone        string            `json:"phone"`
	TemplateID   string            `json:"template_id"`
	TemplateData map[string]string `json:"template_data"`
	TrackingID   string            `json:"tracking_id"`
}

// phanHoi — Zalo trả `error: 0` khi thành công. Trường `message` KHÔNG được
// chuyển lên trên: nó chép lại thân ta vừa gửi, trong đó có số điện thoại.
type phanHoi struct {
	Error int `json:"error"`
}

// Gui gửi ĐÚNG MỘT tin xác nhận cho một yêu cầu.
//
// `maYeuCau` đi vào cả `template_data` (để người nhận đọc được mã của mình) lẫn
// `tracking_id` (để đối soát với hoá đơn của Zalo). Nó KHÔNG phải dữ liệu cá
// nhân nên nó được phép nằm ở cả hai chỗ và được phép vào log.
func (c *Client) Gui(ctx context.Context, soDienThoai, maYeuCau string) error {
	than, err := json.Marshal(thanGui{
		Phone:        soDienThoai,
		TemplateID:   c.maMau,
		TemplateData: map[string]string{"ma_yeu_cau": maYeuCau},
		TrackingID:   maYeuCau,
	})
	if err != nil {
		return fmt.Errorf("zns: dựng thân yêu cầu: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+DuongDanGuiMau, bytes.NewReader(than))
	if err != nil {
		// KHÔNG gói `err` kèm URL đã dựng: nó không mang số, nhưng giữ thói quen
		// ấy ở đây là giữ cho nhánh lỗi không bao giờ thành chỗ rò.
		return errors.New("zns: dựng yêu cầu HTTP hỏng")
	}
	req.Header.Set("Content-Type", "application/json")
	// Zalo Business Open API đọc token ở header riêng, KHÔNG phải `Authorization`.
	req.Header.Set("access_token", c.token.Lo())

	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("zns: không gọi được Zalo: %w", err)
	}
	defer res.Body.Close()

	// Đọc có TRẦN: một phản hồi khổng lồ từ một đích ta không kiểm soát là một
	// đường làm cạn bộ nhớ tiến trình.
	tho, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("zns: đọc phản hồi hỏng: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		// Mã trạng thái được phép vào lỗi — nó là con số của ta, không phải chữ
		// của Zalo.
		return fmt.Errorf("%w: HTTP %d", ErrZaloTuChoi, res.StatusCode)
	}

	var ph phanHoi
	if err := json.Unmarshal(tho, &ph); err != nil {
		return fmt.Errorf("zns: phản hồi không đúng khuôn JSON")
	}
	if ph.Error != 0 {
		return fmt.Errorf("%w: mã %d", ErrZaloTuChoi, ph.Error)
	}
	return nil
}
