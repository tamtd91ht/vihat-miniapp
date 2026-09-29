package config

import (
	"errors"
	"strings"
	"testing"
)

// Secret giả — có chữ cái như secret thật của Zalo, không phải của app nào.
const (
	khoaAppXaGia1 = "khoa-app-xa-gia-lap-1"
	khoaAppXaGia2 = "khoa=app=xa=gia=lap=2" // có dấu '=' như base64
)

func dayDuCoCau() map[string]string {
	m := dayDu()
	m[EnvVigovCauDiaChi], m[EnvVigovCauKhoa] = "identity-cau:9091", khoaCauGia
	return m
}

func TestAppXa_KhongKhaiThiRongVaHanhViCu(t *testing.T) {
	cfg, err := Nap(tra(dayDuCoCau()))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AppXa) != 0 {
		t.Errorf("không khai mà có %d app riêng", len(cfg.AppXa))
	}
	// Cặp app chung giữ nguyên, không bị kéo vào danh sách app riêng.
	if cfg.ZaloAppID != "1234567890" || cfg.ZaloSecretKey.Lo() != "gia-lap-secret-trong-test" {
		t.Error("cặp app chung bị đổi")
	}
}

func TestAppXa_TachDungCapVaGiuDauBangTrongSecret(t *testing.T) {
	m := dayDuCoCau()
	m[EnvAppXa] = " 3291993990104489440 = " + khoaAppXaGia1 + " , 1111111111=" + khoaAppXaGia2 + ","
	cfg, err := Nap(tra(m))
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if len(cfg.AppXa) != 2 {
		t.Fatalf("số app = %d, mong 2", len(cfg.AppXa))
	}
	if cfg.AppXa[0].AppID != "3291993990104489440" || cfg.AppXa[0].SecretKey.Lo() != khoaAppXaGia1 {
		t.Errorf("cặp 1 sai: %s", cfg.AppXa[0].AppID)
	}
	if cfg.AppXa[1].AppID != "1111111111" || cfg.AppXa[1].SecretKey.Lo() != khoaAppXaGia2 {
		t.Errorf("cặp 2 sai — tách ở dấu '=' đầu tiên: %s", cfg.AppXa[1].AppID)
	}
}

// Mọi dạng hỏng đều TỪ CHỐI KHỞI ĐỘNG, nêu tên biến, và KHÔNG in secret.
func TestAppXa_SaiHinhDangThiTuChoiMaKhongInSecret(t *testing.T) {
	cases := map[string]string{
		"thiếu dấu bằng":           "3291993990104489440" + khoaAppXaGia1,
		"thiếu secret":             "3291993990104489440=",
		"cặp gõ ngược":             khoaAppXaGia1 + "=3291993990104489440",
		"app id rỗng":              "=" + khoaAppXaGia1,
		"trùng app chung":          "1234567890=" + khoaAppXaGia1,
		"khai hai lần":             "1111111111=" + khoaAppXaGia1 + ",1111111111=" + khoaAppXaGia2,
		"chỉ toàn dấu phẩy":        " , ,",
		"một phần tử hỏng là hỏng": "1111111111=" + khoaAppXaGia1 + ",khong-co-dau-bang",
	}
	for ten, gia := range cases {
		t.Run(ten, func(t *testing.T) {
			m := dayDuCoCau()
			m[EnvAppXa] = gia
			_, err := Nap(tra(m))
			if err == nil {
				t.Fatal("mong từ chối khởi động")
			}
			if !errors.Is(err, ErrThieuBien) || !strings.Contains(err.Error(), EnvAppXa) {
				t.Errorf("lỗi phải gói ErrThieuBien và nêu %s: %s", EnvAppXa, err)
			}
			for _, cam := range []string{khoaAppXaGia1, khoaAppXaGia2} {
				if strings.Contains(err.Error(), cam) {
					t.Errorf("thông điệp lỗi chứa GIÁ TRỊ secret: %s", err)
				}
			}
		})
	}
}

// App riêng chỉ đăng nhập được qua cầu: có app riêng mà cầu tắt là nửa cấu hình.
func TestAppXa_CoAppMaCauTatThiTuChoiKhoiDong(t *testing.T) {
	m := dayDu()
	m[EnvAppXa] = "3291993990104489440=" + khoaAppXaGia1
	_, err := Nap(tra(m))
	if err == nil {
		t.Fatal("có app riêng mà cầu tắt vẫn khởi động")
	}
	if !strings.Contains(err.Error(), EnvAppXa) || !strings.Contains(err.Error(), EnvVigovCauDiaChi) {
		t.Errorf("thông điệp phải nêu cả %s lẫn biến cầu: %s", EnvAppXa, err)
	}
	if strings.Contains(err.Error(), khoaAppXaGia1) {
		t.Error("thông điệp lỗi chứa secret")
	}
}

// Khai mà để trống (dòng `X=` trong .env.local) là không có app riêng, không phải lỗi.
func TestAppXa_KhaiRongLaKhongCo(t *testing.T) {
	m := dayDu()
	m[EnvAppXa] = "   "
	cfg, err := Nap(tra(m))
	if err != nil || len(cfg.AppXa) != 0 {
		t.Fatalf("biến rỗng: err=%v, số app=%d", err, len(cfg.AppXa))
	}
}
