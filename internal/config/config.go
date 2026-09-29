// Package config là NƠI DUY NHẤT trong kho này đọc môi trường.
//
// Mọi gói khác nhận một Config đã kiểm, kiểu hoá. Một `os.Getenv` nằm rải rác
// trong handler là một biến không có trong struct nào, không có dòng nào trong
// .env.example, và người ta chỉ phát hiện ra nó từ một stack trace lúc 2 giờ sáng.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
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

	// ---------------------------------------------------------------------
	// BỐN BIẾN CỦA BỀ MẶT "YÊU CẦU" (0003) — CẢ BỐN ĐỀU TUỲ CHỌN, CÓ CHỦ ĐÍCH.
	//
	// Chúng bật hai tính năng phụ thuộc bên thứ ba: tin ZNS xác nhận, và lệnh
	// gọi lại từ tổng đài. Thiếu một nhóm thì NHÓM ẤY TẮT, service vẫn khởi
	// động và vẫn nhận yêu cầu tư vấn bình thường.
	//
	// VÌ SAO KHÔNG BẮT BUỘC: "bắt buộc" nghĩa là service không phục vụ nổi một
	// yêu cầu nào nếu thiếu (xem DATABASE_DSN). Thiếu mẫu ZNS thì người dùng
	// vẫn gửi được yêu cầu và ViHAT vẫn nhận được — chỉ là không có tin xác
	// nhận. Đặt nó thành bắt buộc là để một tính năng phụ chặn cả dịch vụ.
	//
	// ⚠ NHÓM, KHÔNG PHẢI TỪNG BIẾN: một nửa cấu hình (có token, thiếu mẫu) là
	// tắt, không phải nửa bật. Cưỡng chế trong zns.Moi / tongdai.Moi — cả hai
	// trả nil khi thiếu bất kỳ mảnh nào.
	// ---------------------------------------------------------------------

	// ZNS: tin xác nhận gửi từ OA của ViHAT sau khi nhận một yêu cầu.
	ZNSAccessToken secret.Secret
	ZNSTemplateID  string

	// Tổng đài: đích nhận lệnh quay số ra cho tính năng "gọi lại".
	TongDaiCallbackURL string
	TongDaiAPIKey      secret.Secret

	// ---------------------------------------------------------------------
	// CẦU PHIÊN CÔNG DÂN ViGov — HAI BIẾN, CẢ HAI HOẶC KHÔNG BIẾN NÀO.
	//
	// Bật thì POST /api/v1/sessions phát PHIÊN CÔNG DÂN CỦA ViGov (gọi
	// CitizenSessionBridgeService.OpenCitizenSession của service-identity) thay
	// cho phiên của kho này — xem internal/vigovcau. Quyết định và lý do nằm ở
	// kho ViGov: kb/10-decisions/0045-cau-phien-cong-dan-mini-app.md.
	//
	// Cả hai trống: cầu TẮT, tuyến đăng nhập chạy y như trước. Một nửa: KHÔNG
	// KHỞI ĐỘNG — nửa cấu hình là hỏng, không phải nửa bật (ADR 0045 §Cấu hình).
	// Có địa chỉ mà thiếu khoá thì mọi lượt đăng nhập công dân nhận
	// UNAUTHENTICATED; có khoá mà thiếu địa chỉ thì người vận hành tưởng đã bật.
	// ---------------------------------------------------------------------

	// VigovCauDiaChi: danh sách host:port của CỔNG CẦU của service-identity —
	// không phải cổng 9090 giữa các service ViGov. DẠNG CỤM từ dòng đầu: một
	// danh sách, kể cả khi hôm nay chỉ có một địa chỉ.
	VigovCauDiaChi []string
	// VigovCauKhoa: khoá cầu gửi ở metadata `x-vigov-bridge-key`. KHÔNG BAO GIỜ
	// là GRPC_CALLER_KEY của ViGov — khoá ấy mở mọi RPC của mọi service ViGov.
	VigovCauKhoa secret.Secret

	// AppXa — app RIÊNG của từng xã (ADR 0044/0045/0047 phía ViGov): mỗi phần tử
	// một cặp App ID → secret. Cặp ZALO_MINIAPP_APP_ID / _SECRET_KEY ở trên vẫn
	// là app CHUNG và KHÔNG nằm trong danh sách này.
	//
	// Yêu cầu mang một App ID trong danh sách thì mọi lượt đổi với Zalo dùng
	// secret của app ấy, và đăng nhập đi cầu ViGov (httpapi, sessions.go). Rỗng
	// là không có app riêng nào — hành vi y như trước.
	AppXa []AppZalo
}

// AppZalo — một Mini App và secret của nó. AppID không phải bí mật (được phép
// in ra log vận hành); SecretKey thì có.
type AppZalo struct {
	AppID     string
	SecretKey secret.Secret
}

// CauPhienBat — cầu phiên ViGov có được cấu hình hay không. Nap đã bảo đảm
// hai biến đi cùng nhau, nên một điều kiện là đủ.
func (c Config) CauPhienBat() bool { return len(c.VigovCauDiaChi) > 0 }

const (
	EnvDatabaseDSN        = "DATABASE_DSN"
	EnvListenAddr         = "LISTEN_ADDR"
	EnvZaloAppID          = "ZALO_MINIAPP_APP_ID"
	EnvZaloSecretKey      = "ZALO_MINIAPP_SECRET_KEY"
	EnvCORSAllowedOrigins = "CORS_ALLOWED_ORIGINS"

	// Bốn biến tuỳ chọn của bề mặt "yêu cầu" (0003). Tên mang tiền tố theo BÊN
	// CUNG CẤP (`ZALO_ZNS_`, `TONGDAI_`) chứ không theo tính năng, vì đó là thứ
	// người vận hành đi tìm: họ cầm trong tay một khoá của Zalo hoặc một khoá
	// của tổng đài, không cầm một "tính năng".
	EnvZNSAccessToken     = "ZALO_ZNS_ACCESS_TOKEN"
	EnvZNSTemplateID      = "ZALO_ZNS_TEMPLATE_ID"
	EnvTongDaiCallbackURL = "TONGDAI_CALLBACK_URL"
	EnvTongDaiAPIKey      = "TONGDAI_API_KEY"

	// Hai biến của cầu phiên ViGov. Tiền tố theo BÊN CUNG CẤP (`VIGOV_`), cùng
	// quy ước `ZALO_ZNS_` / `TONGDAI_`. Tên nói VAI TRÒ (cổng cầu phiên công
	// dân), không nói cụm hay số thứ tự máy chủ nào.
	EnvVigovCauDiaChi = "VIGOV_CITIZEN_SESSION_BRIDGE_ADDRESS"
	EnvVigovCauKhoa   = "VIGOV_CITIZEN_SESSION_BRIDGE_KEY"

	// EnvAppXa — N cặp App ID → secret của app riêng từng xã, MỘT biến, dạng
	// `<app_id>=<secret>,<app_id>=<secret>`. Cùng tiền tố với cặp app chung.
	//
	// Vì sao MỘT biến chứ không phải biến đánh số (`..._1`, `..._2`): thêm một xã
	// thì chỉ sửa một khoá trong Secret, không sửa deployment.yaml, không phát
	// hành lại — và không có biến số thứ tự nào để một ngày bị bỏ sót (luật tên
	// theo VAI TRÒ, không theo thứ tự). Cả biến là BÍ MẬT: nó chứa secret.
	EnvAppXa = "ZALO_MINIAPP_COMMUNE_APP_SECRETS"

	// VigovCauKhoaToiThieu — độ dài tối thiểu (byte) của khoá cầu. TRÙNG với
	// core/grpcx.BridgeKeyMinLen phía ViGov: máy chủ từ chối khởi động với khoá
	// ngắn hơn, nên một khoá ngắn ở đây chắc chắn là khoá sai. Bắt ngay lúc
	// khởi động thay vì để mọi lượt đăng nhập nhận UNAUTHENTICATED.
	VigovCauKhoaToiThieu = 32

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

	// Bốn biến TUỲ CHỌN — không vào `thieu`, và đó là cả khác biệt: thiếu chúng
	// thì tính năng tắt, không phải service chết. Xem khối chú thích ở Config.
	tuyChon := func(ten string) string {
		v, _ := look(ten)
		return strings.TrimSpace(v)
	}
	cfg.ZNSAccessToken = secret.Secret(tuyChon(EnvZNSAccessToken))
	cfg.ZNSTemplateID = tuyChon(EnvZNSTemplateID)
	cfg.TongDaiCallbackURL = tuyChon(EnvTongDaiCallbackURL)
	cfg.TongDaiAPIKey = secret.Secret(tuyChon(EnvTongDaiAPIKey))

	// Cầu phiên ViGov: tuỳ chọn như nhóm trên, nhưng NỬA NHÓM THÌ TỪ CHỐI chứ
	// không lặng lẽ tắt — khác zns/tongdai có chủ đích. Nửa nhóm ZNS chỉ mất
	// một tin xác nhận; nửa nhóm cầu là người vận hành tin mình đã bật một
	// đường đăng nhập mà thực ra không có.
	diaChiCau, khoaCau := tuyChon(EnvVigovCauDiaChi), tuyChon(EnvVigovCauKhoa)
	switch {
	case diaChiCau == "" && khoaCau == "":
		// Tắt. Tuyến đăng nhập giữ nguyên hành vi cũ.
	case diaChiCau == "" || khoaCau == "":
		loi = append(loi, "cầu phiên ViGov nửa cấu hình: "+EnvVigovCauDiaChi+" và "+
			EnvVigovCauKhoa+" phải cùng có hoặc cùng trống")
	default:
		ds, err := phanTichDiaChiCum(diaChiCau)
		if err != nil {
			loi = append(loi, EnvVigovCauDiaChi+": "+err.Error())
		}
		// Chỉ nói ĐỘ DÀI TỐI THIỂU, không nói độ dài đang có: độ dài của một bí
		// mật cũng là thông tin về nó.
		if len(khoaCau) < VigovCauKhoaToiThieu {
			loi = append(loi, fmt.Sprintf("%s: khoá cầu phải dài ít nhất %d byte", EnvVigovCauKhoa, VigovCauKhoaToiThieu))
		}
		cfg.VigovCauDiaChi = ds
		cfg.VigovCauKhoa = secret.Secret(khoaCau)
	}

	// App riêng của xã: tuỳ chọn. Có mà cầu TẮT thì TỪ CHỐI KHỞI ĐỘNG — cùng lý
	// do nửa nhóm cầu: app riêng chỉ đăng nhập được qua cầu, nên người vận hành
	// sẽ tin đã bật đăng nhập cho các xã ấy trong khi không có đường nào.
	if rawApp := tuyChon(EnvAppXa); rawApp != "" {
		apps, err := phanTichAppXa(rawApp, cfg.ZaloAppID)
		if err != nil {
			loi = append(loi, EnvAppXa+": "+err.Error())
		}
		if diaChiCau == "" || khoaCau == "" {
			loi = append(loi, EnvAppXa+" có giá trị nhưng cầu phiên ViGov chưa cấu hình ("+
				EnvVigovCauDiaChi+", "+EnvVigovCauKhoa+") — app riêng chỉ đăng nhập được qua cầu")
		}
		cfg.AppXa = apps
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

// phanTichDiaChiCum tách danh sách "host:port" ngăn bằng dấu phẩy.
//
// Giữ NGUYÊN CẢ DANH SÁCH, không bao giờ cắt lấy một host: cắt thì đúng suốt
// thời gian còn một node và sai đúng vào ngày có node thứ hai.
//
// Từ chối scheme ("http://", "dns:///"): giá trị là địa chỉ mạng, cách quay số
// là việc của internal/vigovcau. Thông điệp lỗi nêu địa chỉ hỏng — địa chỉ cổng
// trong cụm không phải bí mật.
func phanTichDiaChiCum(raw string) ([]string, error) {
	var out []string
	for _, phan := range strings.Split(raw, ",") {
		d := strings.TrimSpace(phan)
		if d == "" {
			continue
		}
		if strings.Contains(d, "/") {
			return nil, fmt.Errorf("%q không phải host:port (không kèm scheme hay đường dẫn)", d)
		}
		host, cong, err := net.SplitHostPort(d)
		if err != nil || host == "" {
			return nil, fmt.Errorf("%q không phải host:port", d)
		}
		if n, err := strconv.Atoi(cong); err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("%q có cổng không hợp lệ", d)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, errors.New("danh sách rỗng")
	}
	return out, nil
}

// appIDHopLe — App ID của Zalo Mini App là một dãy chữ số.
//
// Kiểm khuôn không phải vì thẩm mỹ: App ID ĐƯỢC IN ra log (khởi động, mỗi lượt
// đi cầu). Một cặp gõ ngược (`<secret>=<app_id>`) mà lọt qua thì secret bị in
// ra log ở lượt đầu tiên. Secret của Zalo có chữ cái, nên khuôn chữ số chặn
// đúng lỗi ấy.
func appIDHopLe(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// phanTichAppXa tách `<app_id>=<secret>,<app_id>=<secret>`.
//
// Tách ở dấu `=` ĐẦU TIÊN: App ID không bao giờ chứa `=`, còn secret thì có thể
// (base64). Dấu phẩy là ký tự ngăn cặp, nên secret KHÔNG được chứa dấu phẩy —
// nói trong .env.example.
//
// Thông điệp lỗi chỉ nêu VỊ TRÍ phần tử (thứ mấy), không bao giờ nêu giá trị:
// một phần tử hỏng có thể là một secret bị gõ nhầm chỗ. Ngoại lệ duy nhất là
// App ID đã qua kiểm khuôn chữ số — nó không phải bí mật.
func phanTichAppXa(raw, appIDChung string) ([]AppZalo, error) {
	var out []AppZalo
	daCo := map[string]bool{}
	for i, phan := range strings.Split(raw, ",") {
		phan = strings.TrimSpace(phan)
		if phan == "" {
			continue
		}
		thu := i + 1
		appID, khoa, co := strings.Cut(phan, "=")
		appID, khoa = strings.TrimSpace(appID), strings.TrimSpace(khoa)
		switch {
		case !co || khoa == "":
			return nil, fmt.Errorf("phần tử thứ %d không có dạng <app_id>=<secret>", thu)
		case !appIDHopLe(appID):
			return nil, fmt.Errorf("phần tử thứ %d: app id phải là dãy chữ số (cặp gõ ngược?)", thu)
		case appID == appIDChung:
			// Hai secret cho một App ID: không ai biết cái nào đang được dùng.
			return nil, fmt.Errorf("app id %s trùng %s — app chung không khai lại ở đây", appID, EnvZaloAppID)
		case daCo[appID]:
			return nil, fmt.Errorf("app id %s khai hai lần", appID)
		}
		daCo[appID] = true
		out = append(out, AppZalo{AppID: appID, SecretKey: secret.Secret(khoa)})
	}
	if len(out) == 0 {
		return nil, errors.New("danh sách rỗng")
	}
	return out, nil
}

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
