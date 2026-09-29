package zalo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ===========================================================================
//  MÃ TÀI KHOẢN ZALO — GET /v2.0/me?fields=id, header access_token.
//
//  Chủ dự án chốt 29/09/2026 (ADR 0045 UNKNOWN #2 phía ViGov): làm theo bản
//  tham chiếu ở kho yêu cầu (vigov-require, commit 0053854,
//  apps/api/app/integrations/zalo/graph.py:113-134, hàm verify). Hình dạng dây
//  nằm ở wire.go cùng MỨC CHỨNG CỨ — THẤP: chưa một lời gọi nào từ kho này chạm
//  máy chủ Zalo thật với token thật.
//
//  Ba điều cài đặt này giữ đúng như bản tham chiếu, cả ba có lý do:
//
//   1. CHỈ xin trường `id`. Xin thêm name/picture thì Zalo từ chối CẢ lời gọi
//      khi IP bên gọi ở ngoài Việt Nam — và máy chủ này không hứa đứng ở đâu.
//   2. KHÔNG gửi secret key. Lời gọi này không cần nó; và vì thế nó KHÔNG xác
//      minh được token thuộc app nào. Mã trả về là mã theo app (một người dùng
//      hai app là hai mã) — Zalo tự biết app từ token, ta thì không. Việc xác
//      minh App ID nằm ở lượt đổi CÓ secret (httpapi, sessions.go).
//   3. Zalo trả 200 kèm trường `error` khi hỏng: error != 0 là token hỏng
//      (cùng quy ước với /me/info — wire.go, ĐIỀU CHƯA RÕ #2), mọi thứ khác
//      (HTTP khác 200, thân không đọc được, thiếu id) là lỗi hạ tầng.
//
//  Mã tài khoản là DỮ LIỆU CÁ NHÂN theo cách kho này đối xử (ADR 0045 §Dữ liệu
//  cá nhân đi trên dây): không vào log, không vào lỗi, không vào phản hồi.
// ===========================================================================

// timeoutMaTaiKhoan — ngắn hơn timeoutMacDinh: lời gọi này đứng trước MỌI lượt
// đi cầu, kể cả lượt mở app âm thầm; người dân chờ một vòng xoay 6 giây rồi
// nhận lỗi và thử lại tốt hơn là chờ 8. Cùng con số với bản tham chiếu.
const timeoutMaTaiKhoan = 6 * time.Second

// chiChuSo — mã tài khoản trả dưới dạng SỐ JSON thì phải là một số nguyên viết
// bằng chữ số. Không bao giờ đi qua float64: mã Zalo dài hơn 15 chữ số, float64
// làm tròn chúng — hai người dùng khác nhau thành một mã, hỏng IM LẶNG.
var chiChuSo = regexp.MustCompile(`^[0-9]+$`)

// LayMaTaiKhoan xác minh accessToken với Zalo và trả mã tài khoản Zalo (theo
// app) của người cầm token.
//
// KHÔNG log tham số lẫn kết quả; lỗi trả về không mang token, mã hay thân phản
// hồi của Zalo.
func (c *Client) LayMaTaiKhoan(ctx context.Context, accessToken string) (string, error) {
	if accessToken == "" {
		return "", fmt.Errorf("thiếu accessToken: %w", ErrTokenKhongHopLe)
	}

	ctx, huy := context.WithTimeout(ctx, timeoutMaTaiKhoan)
	defer huy()

	q := url.Values{ThamSoTruong: {TruongMaTaiKhoan}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+DuongDanMaTaiKhoan+"?"+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("dựng yêu cầu: %w", ErrKhongVoiToiZalo)
	}
	req.Header.Set(HeaderAccessToken, accessToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		// err mang URL (chỉ có fields=id) — không mang header, tức không mang token.
		return "", fmt.Errorf("gọi Zalo: %w", ErrKhongVoiToiZalo)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Zalo trả HTTP %d: %w", resp.StatusCode, ErrKhongVoiToiZalo)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, gioiHanBody))
	if err != nil {
		return "", fmt.Errorf("đọc phản hồi: %w", ErrKhongVoiToiZalo)
	}

	var ph phanHoiMaTaiKhoan
	if err := json.Unmarshal(body, &ph); err != nil {
		// Cố tình KHÔNG kèm body: nó có thể mang mã tài khoản.
		return "", fmt.Errorf("phản hồi không phải JSON như mong đợi: %w", ErrKhongVoiToiZalo)
	}
	if ph.Error != 0 {
		// Chỉ MÃ lỗi (số); message của Zalo là chuỗi ta không kiểm soát.
		return "", fmt.Errorf("Zalo báo lỗi mã %d: %w", ph.Error, ErrTokenKhongHopLe)
	}

	ma, ok := docMaTaiKhoan(ph.ID)
	if !ok {
		// error == 0 mà không có mã: ta đọc sai phản hồi, hoặc Zalo đổi hình dạng.
		// Lỗi phía hạ tầng — không bảo người dân đăng nhập lại.
		return "", fmt.Errorf("Zalo không trả mã tài khoản đọc được: %w", ErrKhongVoiToiZalo)
	}
	return ma, nil
}

// docMaTaiKhoan nhận mã là CHUỖI khác rỗng, hoặc SỐ nguyên JSON giữ NGUYÊN văn
// bản chữ số (xem chiChuSo). Mọi thứ khác — vắng, null, rỗng, số thực, đối
// tượng — là không đọc được, không đoán.
func docMaTaiKhoan(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", false
	}
	if strings.HasPrefix(s, `"`) {
		var chuoi string
		if err := json.Unmarshal(raw, &chuoi); err != nil {
			return "", false
		}
		chuoi = strings.TrimSpace(chuoi)
		return chuoi, chuoi != ""
	}
	if chiChuSo.MatchString(s) {
		return s, true
	}
	return "", false
}
