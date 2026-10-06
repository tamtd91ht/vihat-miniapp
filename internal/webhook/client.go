// Package webhook báo "có yêu cầu mới" sang một hệ thống bên nhận, bằng một
// POST JSON có chữ ký HMAC-SHA256.
//
// VÌ SAO CÓ GÓI NÀY (chủ sản phẩm chốt 07/10/2026): ba nút mới của Mini App —
// chat với chuyên viên, nhận / huỷ ưu đãi SMS — chỉ có nghĩa khi một hệ thống
// khác HÀNH ĐỘNG theo chúng (mở cuộc chat, thêm / bỏ số khỏi danh sách gửi
// SMS). Kho này không gửi SMS và không giữ danh sách ấy; nó ghi bằng chứng vào
// yeu_cau rồi báo sang đây. Mọi loại yêu cầu đều được báo, kể cả tư vấn / gọi
// lại, để bên nhận có MỘT nguồn sự kiện thay vì phải dò CSDL.
//
// ⚠ THÂN SỰ KIỆN MANG SỐ ĐIỆN THOẠI, TÊN HIỂN THỊ và GHI CHÚ — bên nhận cần số để
// gửi SMS / gọi lại. Vì thế:
//
//   - CHỈ https, kiểm TLS mặc định của Go (không bao giờ InsecureSkipVerify);
//     URL không phải https thì `Moi` trả nil và config từ chối khởi động trước đó.
//   - KHÔNG đi theo chuyển hướng: một 302 sang http:// hay sang máy khác là thân
//     có số điện thoại đi tới nơi không ai cấu hình.
//   - Thân KHÔNG vào log, KHÔNG vào lỗi trả về. Lỗi chỉ nói mã HTTP hoặc tên việc.
//   - Thân phản hồi của bên nhận bị đọc-bỏ có trần, không bao giờ đưa lên trên.
//
// CHỮ KÝ: `X-Vihat-Signature: sha256=<hex>` = HMAC-SHA256(khoá, THÂN THÔ). Bên
// nhận phải tính lại trên đúng byte nhận được (không parse rồi serialize lại) và
// so bằng hàm so sánh thời gian hằng định.
//
// GIAO ÍT NHẤT MỘT LẦN, KHÔNG PHẢI ĐÚNG MỘT LẦN: một lần thử lại sau khi bên nhận
// đã xử lý nhưng phản hồi bị mất là một sự kiện tới HAI lần. Bên nhận khử trùng
// theo `requestId`. Ngược lại, hai lượt hỏng liền thì sự kiện MẤT (không có hàng
// đợi bền) — phiếu vẫn nằm trong yeu_cau, nên dựng lại được bằng tay.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

const (
	// TenSuKien — giá trị `event` của sự kiện duy nhất hôm nay. Tên có chấm và
	// thì quá khứ để ngày có sự kiện thứ hai (đổi trạng thái…) bên nhận rẽ nhánh
	// được mà không phải đoán.
	TenSuKien = "request.created"

	// HeaderChuKy — tên header mang chữ ký. Một phần của hợp đồng với bên nhận.
	HeaderChuKy = "X-Vihat-Signature"

	// thoiHanMoiLuot — 5 giây mỗi lượt gửi. Bên nhận chỉ cần NHẬN rồi xếp việc;
	// chờ lâu hơn chỉ giữ một goroutine nền.
	thoiHanMoiLuot = 5 * time.Second

	// soLuotToiDa — một lượt + MỘT lần thử lại. Không hơn: không có hàng đợi bền,
	// và thử lại dày đặc vào một bên nhận đang chết chỉ làm nó chết lâu hơn.
	soLuotToiDa     = 2
	nghiGiuaHaiLuot = time.Second
)

// ErrTuChoi — bên nhận trả lời nhưng không phải 2xx.
var ErrTuChoi = errors.New("webhook: bên nhận từ chối sự kiện")

type Client struct {
	url  string
	khoa secret.Secret
	hc   *http.Client
	nghi time.Duration
}

type TuyChon func(*Client)

// VoiHTTPClient thay http.Client (test dùng client của httptest.NewTLSServer,
// mang CA của máy chủ test). Chính sách KHÔNG theo chuyển hướng vẫn được áp lên
// bản sao của client ấy.
func VoiHTTPClient(hc *http.Client) TuyChon { return func(c *Client) { c.hc = hc } }

// VoiNghiGiuaHaiLuot đổi nhịp chờ trước lần thử lại (test đặt 0).
func VoiNghiGiuaHaiLuot(d time.Duration) TuyChon { return func(c *Client) { c.nghi = d } }

// Moi dựng bộ báo webhook, hoặc trả nil khi THIẾU CẤU HÌNH hay URL KHÔNG PHẢI
// https. Trả nil chứ không trả lỗi: config.Nap đã từ chối khởi động với mọi cấu
// hình sai; nil ở đây chỉ còn nghĩa "tắt", và yeucau.DichVu khi ấy chỉ ghi CSDL.
func Moi(dich string, khoa secret.Secret, tuyChon ...TuyChon) *Client {
	if dich == "" || khoa.Lo() == "" || !LaHTTPS(dich) {
		return nil
	}
	c := &Client{
		url:  dich,
		khoa: khoa,
		hc:   &http.Client{Timeout: thoiHanMoiLuot},
		nghi: nghiGiuaHaiLuot,
	}
	for _, t := range tuyChon {
		t(c)
	}
	// Bản sao, để không sửa client của bên gọi; rồi khoá chuyển hướng.
	hc := *c.hc
	if hc.Timeout == 0 || hc.Timeout > thoiHanMoiLuot {
		hc.Timeout = thoiHanMoiLuot
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.hc = &hc
	return c
}

// LaHTTPS — URL tuyệt đối, scheme https, có host, không kèm user:pass. Lớp chắn
// thứ hai: config.Nap đã từ chối khởi động với cùng các điều kiện ấy (config
// không import gói này — gói cấu hình không phụ thuộc bộ điều hợp nào).
func LaHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, "https") && u.Host != "" && u.User == nil
}

// thanSuKien — HÌNH DẠNG DÂY, hợp đồng với bên nhận (README, mục webhook).
// Mảng `interests` luôn là [] chứ không null; `displayName` vắng khi rỗng.
type thanSuKien struct {
	Event       string   `json:"event"`
	RequestID   string   `json:"requestId"`
	Kind        string   `json:"kind"`
	CreatedAt   string   `json:"createdAt"`
	Phone       string   `json:"phone"`
	DisplayName string   `json:"displayName,omitempty"`
	Interests   []string `json:"interests"`
	Scale       string   `json:"scale"`
	Note        string   `json:"note"`
	Source      string   `json:"source"`
}

// KyThan trả giá trị header chữ ký cho một thân. Xuất ra để test (và bên nhận
// viết bằng Go, nếu có) tính lại đúng một cách.
func KyThan(khoa, than []byte) string {
	m := hmac.New(sha256.New, khoa)
	m.Write(than)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Gui POST một sự kiện, thử lại TỐI ĐA MỘT LẦN khi lỗi mạng hoặc bên nhận trả
// 5xx / 429. 4xx khác thì không thử lại: gửi lại đúng thân ấy cũng nhận đúng
// lời từ chối ấy.
func (c *Client) Gui(ctx context.Context, sk yeucau.SuKienYeuCau) error {
	quanTam := sk.QuanTam
	if quanTam == nil {
		quanTam = []string{}
	}
	than, err := json.Marshal(thanSuKien{
		Event:       TenSuKien,
		RequestID:   sk.MaYeuCau,
		Kind:        sk.Kind,
		CreatedAt:   sk.TaoLuc.UTC().Format(time.RFC3339),
		Phone:       sk.SoDienThoai,
		DisplayName: sk.TenHienThi,
		Interests:   quanTam,
		Scale:       sk.QuyMo,
		Note:        sk.GhiChu,
		Source:      sk.NguonChienDich,
	})
	if err != nil {
		return errors.New("webhook: dựng thân sự kiện hỏng")
	}
	chuKy := KyThan([]byte(c.khoa.Lo()), than)

	var loiCuoi error
	for luot := 1; luot <= soLuotToiDa; luot++ {
		thuLai, err := c.guiMotLuot(ctx, than, chuKy)
		if err == nil {
			return nil
		}
		loiCuoi = err
		if !thuLai || luot == soLuotToiDa {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("webhook: hết thời hạn trước lần thử lại: %w", loiCuoi)
		case <-time.After(c.nghi):
		}
	}
	return loiCuoi
}

// guiMotLuot trả (có nên thử lại không, lỗi).
func (c *Client) guiMotLuot(ctx context.Context, than []byte, chuKy string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(than))
	if err != nil {
		return false, errors.New("webhook: dựng yêu cầu HTTP hỏng")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderChuKy, chuKy)

	res, err := c.hc.Do(req)
	if err != nil {
		// Lỗi của net/http nêu URL và nguyên nhân mạng, không nêu thân.
		return true, fmt.Errorf("webhook: không gọi được bên nhận: %w", err)
	}
	defer res.Body.Close()
	// Đọc-bỏ có TRẦN: để kết nối tái dùng được, và để một phản hồi khổng lồ
	// không làm cạn bộ nhớ. Nội dung KHÔNG đi đâu cả — nó có thể chép lại thân.
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return false, nil
	}
	thuLai := res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests
	return thuLai, fmt.Errorf("%w: HTTP %d", ErrTuChoi, res.StatusCode)
}
