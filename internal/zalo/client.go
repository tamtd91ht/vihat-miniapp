// Package zalo đổi phoneToken của Mini App lấy số điện thoại người dùng.
//
// Toàn bộ hình dạng giao thức nằm ở wire.go, kèm mức chứng cứ và danh sách điều
// chưa rõ. Tệp này chỉ lo cách gọi cho an toàn: timeout, chặn body khổng lồ, và
// phân loại lỗi thành hai nhóm mà tầng HTTP cần phân biệt.
package zalo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/secret"
)

// Hai lớp lỗi, vì tầng HTTP phải trả hai mã khác nhau và nói hai câu khác nhau
// với người dùng:
//
//	ErrTokenKhongHopLe -> 401, "hãy mở lại ứng dụng và đăng nhập lại"  (lỗi phía phiên)
//	ErrKhongVoiToiZalo -> 502, "thử lại sau ít phút"                   (lỗi phía hạ tầng)
//
// Gộp hai cái này lại là bảo người dùng đăng nhập lại trong khi lỗi nằm ở ta —
// họ sẽ thử mãi và không bao giờ vào được.
var (
	ErrTokenKhongHopLe = errors.New("zalo: token không hợp lệ hoặc đã hết hạn")
	ErrKhongVoiToiZalo = errors.New("zalo: không gọi được dịch vụ Zalo")
)

const (
	timeoutMacDinh = 8 * time.Second
	// gioiHanBody chặn một phản hồi khổng lồ (hoặc một máy chủ giả mạo) nuốt hết bộ nhớ.
	gioiHanBody = 64 << 10
)

type Client struct {
	baseURL   string
	secretKey secret.Secret
	hc        *http.Client

	// GuiAppSecretProof — xem ĐIỀU CHƯA RÕ #1 trong wire.go.
	// MẶC ĐỊNH TẮT vì chưa có chứng cứ luồng Mini App cần nó, và gửi thừa một
	// header ký sai có thể làm hỏng lời gọi đang chạy được. Khi thử trên máy
	// thật, đây là một công tắc để lật.
	GuiAppSecretProof bool
}

type TuyChon func(*Client)

// VoiHTTPClient thay http.Client (dùng trong test, hoặc khi cần proxy).
func VoiHTTPClient(hc *http.Client) TuyChon { return func(c *Client) { c.hc = hc } }

// New dựng client. baseURL rỗng nghĩa là dùng máy chủ thật.
func New(baseURL string, secretKey secret.Secret, tuyChon ...TuyChon) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = BaseURLMacDinh
	}
	c := &Client{
		baseURL:   strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		secretKey: secretKey,
		hc:        &http.Client{Timeout: timeoutMacDinh},
	}
	for _, t := range tuyChon {
		t(c)
	}
	return c
}

// LaySoDienThoai trả số đã chuẩn hoá (dạng 84xxxxxxxxx).
//
// KHÔNG log tham số, KHÔNG log kết quả, KHÔNG nhét body phản hồi vào lỗi: body
// chứa số điện thoại, và một lỗi có body là một lỗi đưa dữ liệu cá nhân vào log
// tập trung — đúng thứ Nghị định 13/2023 cấm.
func (c *Client) LaySoDienThoai(ctx context.Context, accessToken, phoneToken string) (string, error) {
	if accessToken == "" || phoneToken == "" {
		return "", fmt.Errorf("thiếu accessToken hoặc phoneToken: %w", ErrTokenKhongHopLe)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+DuongDanLayThongTin, nil)
	if err != nil {
		return "", fmt.Errorf("dựng yêu cầu: %w", ErrKhongVoiToiZalo)
	}
	req.Header.Set(HeaderAccessToken, accessToken)
	req.Header.Set(HeaderCode, phoneToken)
	req.Header.Set(HeaderSecretKey, c.secretKey.Lo())
	if c.GuiAppSecretProof {
		req.Header.Set(HeaderAppSecretProof, TinhAppSecretProof(accessToken, c.secretKey))
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		// err có thể chứa URL nhưng không chứa header — không có dữ liệu cá nhân
		// trong URL này (đó là lý do token đi bằng header, không bằng query).
		return "", fmt.Errorf("gọi Zalo: %w", ErrKhongVoiToiZalo)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// 4xx ở tầng HTTP vẫn là "ta gọi sai / Zalo từ chối", không phải lỗi người
		// dùng phải sửa -> 502. Chỉ error!=0 trong body mới là token hỏng.
		return "", fmt.Errorf("Zalo trả HTTP %d: %w", resp.StatusCode, ErrKhongVoiToiZalo)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, gioiHanBody))
	if err != nil {
		return "", fmt.Errorf("đọc phản hồi: %w", ErrKhongVoiToiZalo)
	}

	var ph phanHoiLayThongTin
	if err := json.Unmarshal(body, &ph); err != nil {
		// Cố tình KHÔNG kèm body vào lỗi.
		return "", fmt.Errorf("phản hồi không phải JSON như mong đợi: %w", ErrKhongVoiToiZalo)
	}
	if ph.Error != 0 {
		// Chỉ ghi MÃ lỗi (số), không ghi message của Zalo — message là chuỗi ta
		// không kiểm soát và có thể mang theo dữ liệu người dùng.
		return "", fmt.Errorf("Zalo báo lỗi mã %d: %w", ph.Error, ErrTokenKhongHopLe)
	}

	so, err := ChuanHoaSo(ph.Data.Number)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrKhongVoiToiZalo, err)
	}
	return so, nil
}

// TinhAppSecretProof = hex(HMAC-SHA256(access_token, secret_key)).
//
// MỨC CHỨNG CỨ: THẤP — đây là hình dạng quen thuộc của họ Graph API, CHƯA xác
// nhận được Zalo ký đúng chuỗi này. Không bật GuiAppSecretProof trước khi thử
// trên máy thật.
func TinhAppSecretProof(accessToken string, secretKey secret.Secret) string {
	mac := hmac.New(sha256.New, []byte(secretKey.Lo()))
	mac.Write([]byte(accessToken))
	return hex.EncodeToString(mac.Sum(nil))
}
