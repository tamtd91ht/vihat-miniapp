package config

import (
	"os"
	"strings"
	"testing"
)

// BẢN KÊ BIẾN — phép kiểm này tồn tại vì một biến không có trong bản kê là một
// biến không ai tra ra được: nó chỉ lộ ra từ một stack trace lúc 2 giờ sáng,
// hoặc từ một pod không chịu khởi động mà không ai biết thiếu gì.
//
// Ba nơi phải nói CÙNG MỘT danh sách:
//
//	internal/config  — nơi duy nhất đọc môi trường (nguồn sự thật)
//	.env.example     — BẢN KÊ: mọi biến, kèm lý do bắt buộc hay không
//	deploy/*.example.yaml — hai đích đến trên cụm (Secret / ConfigMap)
//
// Không có phép kiểm này thì lần thêm biến thứ ba sẽ quên một trong ba nơi, và
// không gì báo cho ai biết. `make check` bắt được ngay tại máy người viết.

// banKe là danh sách duy nhất phải khớp ở cả ba nơi.
var banKe = []struct {
	ten      string
	biMat    bool // vào Secret (và giá trị trong tệp mẫu phải là placeholder)
	trenCum  bool // có mặt trong deploy/ hay không
	viTriCum string
}{
	{EnvDatabaseDSN, true, true, "secret.example.yaml"},
	{EnvZaloSecretKey, true, true, "secret.example.yaml"},
	{EnvZaloAppID, false, true, "configmap.example.yaml"},
	{EnvCORSAllowedOrigins, false, true, "configmap.example.yaml"},
	{EnvListenAddr, false, true, "configmap.example.yaml"},
	// Chỉ dùng cho `go test ./internal/store`. Không bao giờ lên cụm: một CSDL
	// test khai báo trong môi trường sản xuất là một CSDL test sẽ bị ai đó ghi vào.
	{"TEST_DATABASE_DSN", false, false, ""},
}

func TestBanKe_MoiBienCoMotDongTrongEnvExample(t *testing.T) {
	noiDung := doc(t, "../../.env.example")

	for _, b := range banKe {
		dong := timDong(noiDung, b.ten+"=")
		if dong == "" {
			t.Errorf("%s KHÔNG có dòng nào trong .env.example — biến không có trong bản kê là biến không ai tra ra được", b.ten)
			continue
		}
		if b.biMat {
			gia := strings.TrimSpace(strings.SplitN(dong, "=", 2)[1])
			if !strings.HasPrefix(gia, "<") {
				t.Errorf("%s trong .env.example mang giá trị không phải placeholder — tệp mẫu nằm trong git, và một giá trị thật đã vào git thì không gọi về được", b.ten)
			}
		}
	}
}

// Chiều ngược lại: một dòng trong bản kê mà không mã nào đọc là một biến người
// vận hành sẽ đặt, sẽ tin là có tác dụng, và nó không có tác dụng gì.
func TestBanKe_EnvExampleKhongCoDongThua(t *testing.T) {
	for _, dong := range strings.Split(doc(t, "../../.env.example"), "\n") {
		dong = strings.TrimSpace(dong)
		if dong == "" || strings.HasPrefix(dong, "#") || !strings.Contains(dong, "=") {
			continue
		}
		ten := strings.SplitN(dong, "=", 2)[0]
		if !coTrongBanKe(ten) {
			t.Errorf("%s có trong .env.example nhưng không nằm trong bản kê của internal/config — hoặc thêm vào config, hoặc bỏ dòng ấy đi", ten)
		}
	}
}

func TestBanKe_MoiBienCoDungMotDichDenTrenCum(t *testing.T) {
	tep := map[string]string{
		"secret.example.yaml":    doc(t, "../../deploy/secret.example.yaml"),
		"configmap.example.yaml": doc(t, "../../deploy/configmap.example.yaml"),
	}

	for _, b := range banKe {
		if !b.trenCum {
			for ten, noi := range tep {
				if strings.Contains(noi, b.ten+":") {
					t.Errorf("%s xuất hiện trong deploy/%s nhưng không được lên cụm", b.ten, ten)
				}
			}
			continue
		}

		khoa := b.ten + ":"
		if !strings.Contains(tep[b.viTriCum], khoa) {
			t.Errorf("%s thiếu khoá trong deploy/%s — pod sẽ không khởi động và thông điệp chỉ nói tên biến, không nói thiếu ở đâu", b.ten, b.viTriCum)
		}
		// Một biến nằm ở CẢ HAI nơi là hai nguồn cho một giá trị: sửa một nơi,
		// nơi kia lặng lẽ giữ giá trị cũ.
		for ten, noi := range tep {
			if ten != b.viTriCum && strings.Contains(noi, khoa) {
				t.Errorf("%s có mặt ở cả deploy/%s lẫn deploy/%s — mỗi biến đúng một đích đến", b.ten, b.viTriCum, ten)
			}
		}
	}
}

// Khoá trong tệp mẫu k8s phải viết GẠCH_DƯỚI, trùng khít tên biến Go.
// Vì sao: deploy/deployment.yaml dùng `env:` + `valueFrom` tường minh, nên khoá
// GẠCH-NGANG cũng chạy được — nhưng ngày nào có người rút gọn thành `envFrom`
// thì khoá gạch-ngang KHÔNG phải định danh shell hợp lệ, Kubernetes bỏ qua nó
// TRONG IM LẶNG (chỉ một event InvalidVariableNames), và người ta nhìn vào
// Secret thấy giá trị nằm đó trong khi service kêu thiếu biến.
// Một cách viết duy nhất thì cái bẫy ấy không có chỗ nào để nảy.
func TestBanKe_KhoaK8sKhongDungGachNgang(t *testing.T) {
	for _, ten := range []string{"secret.example.yaml", "configmap.example.yaml"} {
		for _, dong := range strings.Split(doc(t, "../../deploy/"+ten), "\n") {
			for _, b := range banKe {
				sai := strings.ReplaceAll(b.ten, "_", "-") + ":"
				if strings.Contains(dong, sai) {
					t.Errorf("deploy/%s dùng khoá gạch-ngang %q — phải là %q", ten, sai, b.ten+":")
				}
			}
		}
	}
}

// deploy/deployment.yaml phải khai ĐỦ năm biến, mỗi biến trỏ về ĐÚNG loại
// nguồn. Đây là chỗ dễ lệch nhất và lệch trong im lặng: thêm một khoá vào Secret
// mà quên thêm khối `env:` thì giá trị nằm trong cụm còn tiến trình không thấy
// nó, và thông điệp lỗi chỉ nói "thiếu biến" chứ không nói thiếu ở đâu.
func TestBanKe_DeploymentTroDungNguon(t *testing.T) {
	dong := strings.Split(doc(t, "../../deploy/deployment.yaml"), "\n")

	for _, b := range banKe {
		if !b.trenCum {
			continue
		}
		loaiRef := "configMapKeyRef"
		if b.biMat {
			loaiRef = "secretKeyRef"
		}

		thay := false
		for i, d := range dong {
			if strings.TrimSpace(d) != "- name: "+b.ten {
				continue
			}
			// Khối valueFrom của một biến gọn trong vài dòng ngay sau nó.
			het := min(i+6, len(dong))
			khoi := strings.Join(dong[i:het], "\n")
			if !strings.Contains(khoi, loaiRef) {
				t.Errorf("deploy/deployment.yaml: %s không lấy từ %s", b.ten, loaiRef)
			}
			if !strings.Contains(khoi, "key: "+b.ten) {
				t.Errorf("deploy/deployment.yaml: %s không trỏ tới khoá cùng tên", b.ten)
			}
			thay = true
			break
		}
		if !thay {
			t.Errorf("deploy/deployment.yaml thiếu hẳn khối env cho %s — giá trị sẽ nằm trong cụm mà tiến trình không thấy", b.ten)
		}
	}
}

// Tệp mẫu Secret KHÔNG BAO GIỜ được mang giá trị thật. Kiểm thô nhưng đủ: mọi
// giá trị trong đó phải là placeholder <...>.
func TestBanKe_SecretMauChiCoPlaceholder(t *testing.T) {
	for _, dong := range strings.Split(doc(t, "../../deploy/secret.example.yaml"), "\n") {
		for _, b := range banKe {
			if !b.biMat || !strings.HasPrefix(strings.TrimSpace(dong), b.ten+":") {
				continue
			}
			gia := strings.TrimSpace(strings.SplitN(dong, ":", 2)[1])
			gia = strings.Trim(gia, `"'`)
			if !strings.HasPrefix(gia, "<") {
				t.Errorf("deploy/secret.example.yaml: %s mang giá trị không phải placeholder", b.ten)
			}
		}
	}
}

func coTrongBanKe(ten string) bool {
	for _, b := range banKe {
		if b.ten == ten {
			return true
		}
	}
	return false
}

func timDong(noiDung, tienTo string) string {
	for _, dong := range strings.Split(noiDung, "\n") {
		if strings.HasPrefix(strings.TrimSpace(dong), tienTo) {
			return strings.TrimSpace(dong)
		}
	}
	return ""
}

func doc(t *testing.T, duongDan string) string {
	t.Helper()
	b, err := os.ReadFile(duongDan)
	if err != nil {
		t.Fatalf("không đọc được %s: %s", duongDan, err)
	}
	return string(b)
}
