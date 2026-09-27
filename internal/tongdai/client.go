// Package tongdai kích một cuộc gọi ra từ tổng đài của ViHAT.
//
// ⚠ ĐÂY LÀ BỘ ĐIỀU HỢP TỚI MỘT ĐÍCH CẤU HÌNH ĐƯỢC, KHÔNG PHẢI MỘT CLIENT CỦA
// OMICALL — và khác biệt ấy là chủ đích, không phải sự lười.
//
//	Hình dạng dây của OmiCall chưa được đo trong kho này: chưa có một lời gọi
//	thật nào, chưa có khoá, chưa có tài liệu nào được đọc và ghim lại. Viết một
//	`client.go` mang tên OmiCall với những tên trường đoán ra sẽ trông như đã
//	tích hợp xong, và người đọc sau sẽ tin vào nó.
//
//	Nên gói này làm đúng thứ nó biết chắc: POST một thân JSON tối giản tới một
//	URL trong cấu hình, kèm một khoá ở header `Authorization`. Đó là hình dạng
//	mà mọi cổng kích cuộc gọi đều nhận được, trực tiếp hoặc qua một lớp mỏng —
//	và ngày hình dạng thật được đo, thứ phải sửa nằm gọn trong tệp này.
//
// ⚠ TỔNG ĐÀI QUAY RA MỘT MÁY THẬT. Đó là lý do mọi rào chắn quanh nó nằm ở tầng
// trên chứ không ở đây: số LUÔN lấy từ phiên đăng nhập (`yeucau`), trần 3
// lượt/24 giờ/người đếm trước khi gọi tới đây, và bản thân gói này không có
// đường nào nhận một số từ bên ngoài luồng ấy.
//
// ⚠ SỐ ĐIỆN THOẠI KHÔNG VÀO LOG, kể cả nhánh lỗi. Thứ được phép ghi là `maYeuCau`.
package tongdai

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

// ErrTuChoi — cổng nhận được yêu cầu và không nhận lệnh gọi.
var ErrTuChoi = errors.New("tongdai: cổng từ chối lệnh gọi ra")

type Client struct {
	url  string
	khoa secret.Secret
	hc   *http.Client
}

type TuyChon func(*Client)

func VoiHTTPClient(hc *http.Client) TuyChon { return func(c *Client) { c.hc = hc } }

// Moi dựng bộ kích cuộc gọi, hoặc trả nil khi CHƯA ĐỦ CẤU HÌNH.
//
// TRẢ nil CHỨ KHÔNG TRẢ LỖI: thiếu cấu hình tổng đài không được chặn service
// khởi động — tuyến tư vấn vẫn phải phục vụ. Thứ xảy ra là tuyến gọi lại trả
// 503 kèm một câu nói người dùng gọi hotline, thay vì nhận phiếu rồi im lặng
// không ai gọi lại. Xem `yeucau.DichVu.CoGoiLai`.
func Moi(url string, khoa secret.Secret, tuyChon ...TuyChon) *Client {
	if url == "" || khoa.Lo() == "" {
		return nil
	}
	c := &Client{
		url:  url,
		khoa: khoa,
		// Timeout ngắn hơn ZNS: đây là một lệnh KÍCH, không phải một việc chờ
		// kết quả. Cổng nhận lệnh rồi quay số bất đồng bộ; chờ lâu ở đây chỉ
		// giữ một goroutine của tuyến HTTP mà người dùng đang đợi.
		hc: &http.Client{Timeout: 5 * time.Second},
	}
	for _, t := range tuyChon {
		t(c)
	}
	return c
}

type thanGoi struct {
	Phone     string `json:"phone"`
	RequestID string `json:"requestId"`
}

// GoiLai bảo tổng đài quay ra `soDienThoai`.
//
// `maYeuCau` đi kèm để đối soát cuộc gọi với phiếu — nó KHÔNG phải dữ liệu cá
// nhân nên nó được phép nằm trong thân, trong log, và trong bản ghi của tổng đài.
//
// KHÔNG THỬ LẠI Ở ĐÂY. Một lần thử lại mù trên một lệnh quay số là một cuộc gọi
// thứ hai tới cùng một người, và người nhận không phân biệt được nó với việc bị
// gọi hai lần. Hỏng thì để lại một dòng log ở mức Error và một phiếu để người
// thật gọi lại bằng tay — xem `yeucau.TaoGoiLai`.
func (c *Client) GoiLai(ctx context.Context, soDienThoai, maYeuCau string) error {
	than, err := json.Marshal(thanGoi{Phone: soDienThoai, RequestID: maYeuCau})
	if err != nil {
		return fmt.Errorf("tongdai: dựng thân yêu cầu: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(than))
	if err != nil {
		return errors.New("tongdai: dựng yêu cầu HTTP hỏng")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.khoa.Lo())

	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("tongdai: không gọi được cổng: %w", err)
	}
	defer res.Body.Close()
	// Đọc và bỏ có TRẦN: không đọc thì kết nối không tái dùng được; đọc không
	// trần thì một phản hồi khổng lồ làm cạn bộ nhớ tiến trình.
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))

	// 2xx là "đã nhận lệnh". Mọi thứ khác là từ chối, và THÂN PHẢN HỒI KHÔNG
	// ĐƯỢC ĐƯA LÊN TRÊN: nó chép lại thứ ta vừa gửi, trong đó có số điện thoại.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", ErrTuChoi, res.StatusCode)
	}
	return nil
}
