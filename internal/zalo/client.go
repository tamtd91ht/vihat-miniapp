// Package zalo đổi phoneToken của Mini App lấy số điện thoại người dùng, token
// của getLocation() lấy toạ độ, và accessToken lấy mã tài khoản Zalo.
//
// Một Client giữ secret của ĐÚNG MỘT Mini App. N app thì N Client — chọn
// Client nào cho một yêu cầu là việc của internal/httpapi.
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
	kq := c.goi(ctx, accessToken, phoneToken)
	if kq.Loi != nil {
		return "", kq.Loi
	}
	return kq.SoChuan, nil
}

// ketQuaGoi là toàn bộ thứ quan sát được từ MỘT lời gọi sang Zalo.
//
// KHÔNG XUẤT RA NGOÀI GÓI: nó mang SoTho/SoChuan, tức dữ liệu cá nhân. Đường
// phục vụ lấy SoChuan qua LaySoDienThoai; đường chẩn đoán lấy HÌNH DẠNG của số
// qua ChanDoan. Không có đường thứ ba.
type ketQuaGoi struct {
	ketQuaThongTin

	SoTho   string // nguyên văn Zalo trả
	SoChuan string // sau ChuanHoaSo; rỗng khi không lấy được
}

// ketQuaThongTin là phần của một lời gọi /me/info KHÔNG phụ thuộc thứ được
// đổi (số điện thoại hay vị trí): phong bì, mã HTTP, thời gian, lỗi đã phân loại.
type ketQuaThongTin struct {
	GuiProof   bool
	HTTPStatus int
	ThoiGian   time.Duration

	CoThanJSON  bool
	ZaloError   int
	ZaloMessage string

	Loi error // đã phân loại: ErrTokenKhongHopLe / ErrKhongVoiToiZalo / nil
}

// goi đổi phoneToken: gọi chung qua goiThongTin, rồi chuẩn hoá số.
func (c *Client) goi(ctx context.Context, accessToken, phoneToken string) ketQuaGoi {
	var data duLieuSo
	kq := ketQuaGoi{ketQuaThongTin: goiThongTin(ctx, c, accessToken, phoneToken, &data)}
	kq.SoTho = data.Number
	if kq.Loi != nil {
		return kq
	}

	so, err := ChuanHoaSo(data.Number)
	if err != nil {
		kq.Loi = fmt.Errorf("%w: %w", ErrKhongVoiToiZalo, err)
		return kq
	}
	kq.SoChuan = so
	return kq
}

// LayViTri đổi token của getLocation() lấy (vĩ độ, kinh độ).
//
// KHÔNG lưu, KHÔNG log tham số lẫn kết quả: toạ độ là nơi một người đang đứng —
// dữ liệu cá nhân theo đúng nghĩa của Nghị định 13/2023. Lỗi trả về không bao
// giờ mang toạ độ, token hay body của Zalo.
//
// Toạ độ sai còn tệ hơn không có toạ độ: nó đặt một phản ánh vào chỗ không ai
// phản ánh gì. Vì thế mọi thứ không đọc được, hoặc nằm ngoài trái đất, đều thành
// ErrKhongVoiToiZalo chứ không được đoán.
func (c *Client) LayViTri(ctx context.Context, accessToken, locationToken string) (float64, float64, error) {
	var data duLieuViTri
	kq := goiThongTin(ctx, c, accessToken, locationToken, &data)
	if kq.Loi != nil {
		return 0, 0, kq.Loi
	}
	viDo, err := docToaDo(data.Latitude)
	if err != nil {
		return 0, 0, fmt.Errorf("vĩ độ: %w: %w", ErrKhongVoiToiZalo, err)
	}
	kinhDo, err := docToaDo(data.Longitude)
	if err != nil {
		return 0, 0, fmt.Errorf("kinh độ: %w: %w", ErrKhongVoiToiZalo, err)
	}
	// So sánh với NaN luôn sai, nên "NaN" cũng rơi vào nhánh này.
	if !(viDo >= -90 && viDo <= 90 && kinhDo >= -180 && kinhDo <= 180) {
		return 0, 0, fmt.Errorf("%w: %w", ErrKhongVoiToiZalo, ErrViTriKhongHopLe)
	}
	return viDo, kinhDo, nil
}

// goiThongTin là ĐƯỜNG DUY NHẤT chạm /me/info trong kho này. (Đường thứ hai
// chạm Zalo là LayMaTaiKhoan — endpoint khác, không secret, ma_tai_khoan.go;
// cmd/thu-zalo CHƯA đi qua nó.)
//
// Một đường, ba người dùng (LaySoDienThoai, ChanDoan, LayViTri) là điều kiện để
// một lần chạy thật của cmd/thu-zalo nói được điều gì đó về đường phục vụ. Hai
// bản sao của cùng lời gọi thì lần thử thật chỉ chứng minh cho chính bản sao ấy.
//
// data là nơi nhận trường "data" của phong bì; kiểu của nó là thứ DUY NHẤT khác
// nhau giữa đổi số điện thoại và đổi vị trí.
func goiThongTin[T any](ctx context.Context, c *Client, accessToken, code string, data *T) ketQuaThongTin {
	kq := ketQuaThongTin{GuiProof: c.GuiAppSecretProof}

	if accessToken == "" || code == "" {
		kq.Loi = fmt.Errorf("thiếu accessToken hoặc code: %w", ErrTokenKhongHopLe)
		return kq
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+DuongDanLayThongTin, nil)
	if err != nil {
		kq.Loi = fmt.Errorf("dựng yêu cầu: %w", ErrKhongVoiToiZalo)
		return kq
	}
	req.Header.Set(HeaderAccessToken, accessToken)
	req.Header.Set(HeaderCode, code)
	req.Header.Set(HeaderSecretKey, c.secretKey.Lo())
	if c.GuiAppSecretProof {
		req.Header.Set(HeaderAppSecretProof, TinhAppSecretProof(accessToken, c.secretKey))
	}

	batDau := time.Now()
	resp, err := c.hc.Do(req)
	kq.ThoiGian = time.Since(batDau)
	if err != nil {
		// err có thể chứa URL nhưng không chứa header — không có dữ liệu cá nhân
		// trong URL này (đó là lý do token đi bằng header, không bằng query).
		kq.Loi = fmt.Errorf("gọi Zalo: %w", ErrKhongVoiToiZalo)
		return kq
	}
	defer func() { _ = resp.Body.Close() }()
	kq.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		// 4xx ở tầng HTTP vẫn là "ta gọi sai / Zalo từ chối", không phải lỗi người
		// dùng phải sửa -> 502. Chỉ error!=0 trong body mới là token hỏng.
		kq.Loi = fmt.Errorf("Zalo trả HTTP %d: %w", resp.StatusCode, ErrKhongVoiToiZalo)
		return kq
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, gioiHanBody))
	if err != nil {
		kq.Loi = fmt.Errorf("đọc phản hồi: %w", ErrKhongVoiToiZalo)
		return kq
	}

	ph := phongBi[T]{Data: data}
	if err := json.Unmarshal(body, &ph); err != nil {
		// Cố tình KHÔNG kèm body vào lỗi.
		kq.Loi = fmt.Errorf("phản hồi không phải JSON như mong đợi: %w", ErrKhongVoiToiZalo)
		return kq
	}
	kq.CoThanJSON = true
	kq.ZaloError, kq.ZaloMessage = ph.Error, ph.Message

	if ph.Error != 0 {
		// Chỉ ghi MÃ lỗi (số), không ghi message của Zalo — message là chuỗi ta
		// không kiểm soát và có thể mang theo dữ liệu người dùng.
		kq.Loi = fmt.Errorf("Zalo báo lỗi mã %d: %w", ph.Error, ErrTokenKhongHopLe)
		return kq
	}
	return kq
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
