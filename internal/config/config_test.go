package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func tra(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func dayDu() map[string]string {
	return map[string]string{
		EnvDatabaseDSN:        "postgres://host/db",
		EnvZaloAppID:          "1234567890",
		EnvZaloSecretKey:      "gia-lap-secret-trong-test",
		EnvCORSAllowedOrigins: "https://h5.zdn.vn, https://zalo.me/",
	}
}

func TestNap_DuBien(t *testing.T) {
	cfg, err := Nap(tra(dayDu()))
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if cfg.ListenAddr != listenAddrMacDinh {
		t.Errorf("LISTEN_ADDR mặc định = %q, mong %q", cfg.ListenAddr, listenAddrMacDinh)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[1] != "https://zalo.me" {
		t.Errorf("origin phải được trim và bỏ dấu / cuối, nhận %q", cfg.CORSAllowedOrigins)
	}
	if cfg.ZaloSecretKey.Lo() != "gia-lap-secret-trong-test" {
		t.Error("Lo() phải trả giá trị thật")
	}
}

// Hỏng thì ĐÓNG, và lỗi phải nêu ĐÍCH DANH từng biến — người vận hành đọc đúng
// một dòng là sửa được hết, không phải khởi động lại năm lần.
func TestNap_ThieuBienThiNeuDichDanh(t *testing.T) {
	m := dayDu()
	delete(m, EnvDatabaseDSN)
	delete(m, EnvZaloSecretKey)

	_, err := Nap(tra(m))
	if err == nil {
		t.Fatal("thiếu biến bắt buộc mà vẫn nạp được cấu hình")
	}
	if !errors.Is(err, ErrThieuBien) {
		t.Errorf("lỗi phải gói ErrThieuBien, nhận: %s", err)
	}
	for _, ten := range []string{EnvDatabaseDSN, EnvZaloSecretKey} {
		if !strings.Contains(err.Error(), ten) {
			t.Errorf("thông điệp lỗi thiếu tên biến %s: %s", ten, err)
		}
	}
	if strings.Contains(err.Error(), "gia-lap-secret-trong-test") {
		t.Error("thông điệp lỗi chứa GIÁ TRỊ biến — chỉ được chứa tên biến")
	}
}

func TestNap_BienRongCoiNhuThieu(t *testing.T) {
	m := dayDu()
	m[EnvZaloAppID] = "   "
	_, err := Nap(tra(m))
	if err == nil || !strings.Contains(err.Error(), EnvZaloAppID) {
		t.Fatalf("biến rỗng phải bị coi là thiếu, nhận: %v", err)
	}
}

func TestNap_CORSSaoKhongDuocPhep(t *testing.T) {
	m := dayDu()
	m[EnvCORSAllowedOrigins] = "*"
	_, err := Nap(tra(m))
	if err == nil || !strings.Contains(err.Error(), EnvCORSAllowedOrigins) {
		t.Fatalf(`"*" phải bị từ chối kèm tên biến, nhận: %v`, err)
	}
}

func TestNap_CORSThieuScheme(t *testing.T) {
	m := dayDu()
	m[EnvCORSAllowedOrigins] = "h5.zdn.vn"
	if _, err := Nap(tra(m)); err == nil {
		t.Fatal("origin thiếu scheme phải bị từ chối")
	}
}

// TTL phiên là chính sách sản phẩm đã chốt (7 ngày, 20/09/2026). Test này tồn tại
// để một lần đổi vô tình phải đi kèm một lần sửa test có người đọc.
func TestTTLPhien_LaChinhSach7Ngay(t *testing.T) {
	if TTLPhien != 7*24*time.Hour {
		t.Fatalf("TTLPhien = %s, chính sách đã chốt là 7 ngày — đổi phải hỏi chủ sản phẩm", TTLPhien)
	}
}

// NapChiZalo: lệnh chẩn đoán chỉ cần app id + secret, và KHÔNG được đòi DSN —
// đòi thừa là mời người vận hành gõ một DSN giả cho xong.
func TestNapChiZalo_KhongDoiDSN(t *testing.T) {
	cz, err := NapChiZalo(tra(map[string]string{
		EnvZaloAppID:     "1234567890",
		EnvZaloSecretKey: "gia-lap-secret-trong-test",
	}))
	if err != nil {
		t.Fatalf("mong không lỗi khi vắng DATABASE_DSN, nhận: %s", err)
	}
	if cz.AppID != "1234567890" || cz.SecretKey.Lo() != "gia-lap-secret-trong-test" {
		t.Errorf("cấu hình Zalo nạp sai: app id = %q", cz.AppID)
	}
}

func TestNapChiZalo_ThieuThiNeuDichDanhVaKhongInGiaTri(t *testing.T) {
	_, err := NapChiZalo(tra(map[string]string{EnvZaloSecretKey: "gia-lap-secret-trong-test"}))
	if err == nil || !errors.Is(err, ErrThieuBien) {
		t.Fatalf("thiếu app id mà vẫn nạp được: %v", err)
	}
	if !strings.Contains(err.Error(), EnvZaloAppID) {
		t.Errorf("lỗi phải nêu đích danh %s: %s", EnvZaloAppID, err)
	}
	if strings.Contains(err.Error(), "gia-lap-secret-trong-test") {
		t.Error("thông điệp lỗi chứa GIÁ TRỊ biến — chỉ được chứa tên biến")
	}
}
