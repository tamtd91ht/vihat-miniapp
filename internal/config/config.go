// Package config là NƠI DUY NHẤT trong kho này đọc môi trường.
//
// Mọi gói khác nhận một Config đã kiểm, kiểu hoá. Một `os.Getenv` nằm rải rác
// trong handler là một biến không có trong struct nào, không có dòng nào trong
// .env.example, và người ta chỉ phát hiện ra nó từ một stack trace lúc 2 giờ sáng.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/secret"
)

// TTLPhien — thời gian sống của một phiên đăng nhập.
//
// CHÍNH SÁCH SẢN PHẨM, không phải hằng kỹ thuật: người dùng (chủ sản phẩm phía
// VihatSoftware) chốt 7 ngày ngày 20/09/2026. Để ở đây chứ không đưa ra biến môi
// trường là có chủ đích: đổi cam kết với người dùng cuối phải là một thay đổi mã
// nguồn có người review, không phải một dòng env ai sửa cũng được.
//
// Đổi con số này = đổi chính sách. Phải hỏi lại chủ sản phẩm trước.
const TTLPhien = 7 * 24 * time.Hour

// Config là toàn bộ cấu hình tiến trình cần để phục vụ.
type Config struct {
	DatabaseDSN string
	ListenAddr  string

	ZaloAppID     string // không phải bí mật — được phép in ra log vận hành
	ZaloSecretKey secret.Secret

	// CORSAllowedOrigins: danh sách trắng tuyệt đối. Rỗng là không hợp lệ,
	// "*" là không hợp lệ. Xem internal/httpapi/cors.go.
	CORSAllowedOrigins []string
}

const (
	EnvDatabaseDSN        = "DATABASE_DSN"
	EnvListenAddr         = "LISTEN_ADDR"
	EnvZaloAppID          = "ZALO_MINIAPP_APP_ID"
	EnvZaloSecretKey      = "ZALO_MINIAPP_SECRET_KEY"
	EnvCORSAllowedOrigins = "CORS_ALLOWED_ORIGINS"

	listenAddrMacDinh = ":8080"
)

// ErrThieuBien là lớp lỗi "cấu hình không dùng được". Gói lại bằng %w để
// cmd/server phân biệt "cấu hình sai" với "hạ tầng chưa lên".
var ErrThieuBien = errors.New("cấu hình môi trường không hợp lệ")

// Nap dựng Config từ một hàm tra cứu bất kỳ (os.LookupEnv trong sản xuất, map
// trong test). Hỏng thì ĐÓNG: trả lỗi nêu ĐÍCH DANH mọi biến còn thiếu, một lần,
// để người vận hành sửa hết trong một vòng chứ không phải khởi động lại năm lần.
//
// Thông điệp lỗi chỉ chứa TÊN biến, không bao giờ chứa GIÁ TRỊ — nó sẽ đi thẳng
// vào log khởi động.
func Nap(look func(string) (string, bool)) (Config, error) {
	var thieu []string
	var loi []string

	batBuoc := func(ten string) string {
		v, ok := look(ten)
		v = strings.TrimSpace(v)
		if !ok || v == "" {
			thieu = append(thieu, ten)
			return ""
		}
		return v
	}

	cfg := Config{
		DatabaseDSN: batBuoc(EnvDatabaseDSN),
		ZaloAppID:   batBuoc(EnvZaloAppID),
	}
	cfg.ZaloSecretKey = secret.Secret(batBuoc(EnvZaloSecretKey))

	cfg.ListenAddr = listenAddrMacDinh
	if v, ok := look(EnvListenAddr); ok && strings.TrimSpace(v) != "" {
		cfg.ListenAddr = strings.TrimSpace(v)
	}

	rawOrigins := batBuoc(EnvCORSAllowedOrigins)
	if rawOrigins != "" {
		origins, err := phanTichOrigins(rawOrigins)
		if err != nil {
			loi = append(loi, EnvCORSAllowedOrigins+": "+err.Error())
		}
		cfg.CORSAllowedOrigins = origins
	}

	if len(thieu) > 0 {
		loi = append(loi, "thiếu biến bắt buộc: "+strings.Join(thieu, ", "))
	}
	if len(loi) > 0 {
		return Config{}, fmt.Errorf("%s: %w", strings.Join(loi, "; "), ErrThieuBien)
	}
	return cfg, nil
}

// NapTuMoiTruong là lối vào dùng trong sản xuất.
func NapTuMoiTruong() (Config, error) { return Nap(os.LookupEnv) }

// NapChiDSN cho các lệnh vận hành chạy tay (cmd/an-danh) — chúng chỉ cần CSDL.
//
// Vì sao không dùng Nap: bắt người vận hành đặt cả ZALO_MINIAPP_SECRET_KEY chỉ
// để chạy một lệnh không hề gọi Zalo là cách nhanh nhất khiến họ điền bừa một
// giá trị giả, và giá trị giả ấy rồi sẽ theo ai đó sang môi trường thật.
//
// Vẫn đi qua gói này: config là NƠI DUY NHẤT đọc môi trường.
func NapChiDSN(look func(string) (string, bool)) (string, error) {
	v, ok := look(EnvDatabaseDSN)
	if v = strings.TrimSpace(v); !ok || v == "" {
		return "", fmt.Errorf("thiếu biến bắt buộc: %s: %w", EnvDatabaseDSN, ErrThieuBien)
	}
	return v, nil
}

// NapChiDSNTuMoiTruong là lối vào dùng trong sản xuất.
func NapChiDSNTuMoiTruong() (string, error) { return NapChiDSN(os.LookupEnv) }

// CauHinhZalo là phần cấu hình đủ để gọi Zalo, không hơn.
type CauHinhZalo struct {
	AppID     string // không phải bí mật — được phép in ra
	SecretKey secret.Secret
}

// NapChiZalo cho lệnh chẩn đoán chạy tay (cmd/thu-zalo) — nó gọi Zalo và
// KHÔNG chạm CSDL.
//
// Cùng một lý do như NapChiDSN, lật ngược: bắt người chạy đặt DATABASE_DSN và
// CORS_ALLOWED_ORIGINS chỉ để thử một lời gọi sang Zalo là cách nhanh nhất
// khiến họ gõ bừa một DSN giả — và một DSN giả trong shell của người vận hành
// rồi sẽ được dán vào chỗ khác.
//
// Vẫn đi qua gói này: config là NƠI DUY NHẤT đọc môi trường.
func NapChiZalo(look func(string) (string, bool)) (CauHinhZalo, error) {
	var thieu []string
	lay := func(ten string) string {
		v, ok := look(ten)
		if v = strings.TrimSpace(v); !ok || v == "" {
			thieu = append(thieu, ten)
			return ""
		}
		return v
	}

	cz := CauHinhZalo{AppID: lay(EnvZaloAppID)}
	cz.SecretKey = secret.Secret(lay(EnvZaloSecretKey))

	if len(thieu) > 0 {
		// Nêu đích danh cả hai trong một lần, như Nap — và KHÔNG in giá trị nào.
		return CauHinhZalo{}, fmt.Errorf("thiếu biến bắt buộc: %s: %w", strings.Join(thieu, ", "), ErrThieuBien)
	}
	return cz, nil
}

// NapChiZaloTuMoiTruong là lối vào dùng trong sản xuất.
func NapChiZaloTuMoiTruong() (CauHinhZalo, error) { return NapChiZalo(os.LookupEnv) }

// phanTichOrigins tách danh sách origin và từ chối "*".
//
// "*" bị cấm chứ không chỉ bị khuyên tránh: tuyến đăng nhập nhận token của Zalo,
// mở cho mọi origin nghĩa là bất kỳ trang web nào cũng gọi được nó từ trình duyệt
// của người dùng.
func phanTichOrigins(raw string) ([]string, error) {
	var out []string
	for _, phan := range strings.Split(raw, ",") {
		o := strings.TrimSuffix(strings.TrimSpace(phan), "/")
		if o == "" {
			continue
		}
		if o == "*" {
			return nil, errors.New(`"*" không được phép — phải liệt kê origin cụ thể`)
		}
		if !strings.HasPrefix(o, "https://") && !strings.HasPrefix(o, "http://") {
			return nil, fmt.Errorf("origin %q thiếu scheme http:// hoặc https://", o)
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil, errors.New("danh sách rỗng")
	}
	return out, nil
}
