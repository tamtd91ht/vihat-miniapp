package config

import (
	"errors"
	"strings"
	"testing"
)

// Khoá giả 32 byte — đủ độ dài tối thiểu, không phải khoá thật của môi trường nào.
const khoaWebhookGia = "khoa-ky-webhook-gia-lap-test-032"

func TestWebhook_CaHaiTrongThiTat(t *testing.T) {
	cfg, err := Nap(tra(dayDu()))
	if err != nil {
		t.Fatalf("thiếu cả hai biến webhook mà không khởi động: %s", err)
	}
	if cfg.WebhookBat() {
		t.Error("không khai biến webhook nào mà webhook lại bật")
	}
}

func TestWebhook_DuHaiBienThiBat(t *testing.T) {
	m := dayDu()
	m[EnvWebhookURL], m[EnvWebhookKhoa] = " https://nhan.vihat.vn/su-kien ", khoaWebhookGia
	cfg, err := Nap(tra(m))
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if !cfg.WebhookBat() || cfg.WebhookURL != "https://nhan.vihat.vn/su-kien" || cfg.WebhookKhoa.Lo() != khoaWebhookGia {
		t.Fatalf("cấu hình webhook sai: url=%q bat=%v", cfg.WebhookURL, cfg.WebhookBat())
	}
}

// Nửa cấu hình, URL không phải https, khoá ngắn: KHÔNG KHỞI ĐỘNG — và lỗi không
// mang giá trị khoá hay URL.
func TestWebhook_CauHinhSaiThiTuChoiKhoiDong(t *testing.T) {
	cases := map[string]struct{ url, khoa, bienNeu string }{
		"có URL, thiếu khoá":    {"https://nhan.vihat.vn/x", "", EnvWebhookURL},
		"có khoá, thiếu URL":    {"", khoaWebhookGia, EnvWebhookKhoa},
		"URL http":              {"http://nhan.vihat.vn/x?token=bi-mat-trong-url", khoaWebhookGia, EnvWebhookURL},
		"URL không có host":     {"https:///x", khoaWebhookGia, EnvWebhookURL},
		"URL tương đối":         {"nhan.vihat.vn/x", khoaWebhookGia, EnvWebhookURL},
		"URL kèm user:pass":     {"https://ai:mat-khau@nhan.vihat.vn/x", khoaWebhookGia, EnvWebhookURL},
		"khoá ngắn hơn 32 byte": {"https://nhan.vihat.vn/x", "khoa-ngan-31-byte-gia-lap-00031", EnvWebhookKhoa},
	}
	for ten, ca := range cases {
		t.Run(ten, func(t *testing.T) {
			m := dayDu()
			m[EnvWebhookURL], m[EnvWebhookKhoa] = ca.url, ca.khoa
			_, err := Nap(tra(m))
			if err == nil {
				t.Fatal("cấu hình webhook sai mà vẫn khởi động")
			}
			if !errors.Is(err, ErrThieuBien) {
				t.Errorf("lỗi phải gói ErrThieuBien: %s", err)
			}
			if !strings.Contains(err.Error(), ca.bienNeu) {
				t.Errorf("thông điệp không nêu %s: %s", ca.bienNeu, err)
			}
			for _, cam := range []string{ca.khoa, "bi-mat-trong-url", "mat-khau"} {
				if cam != "" && strings.Contains(err.Error(), cam) {
					t.Errorf("thông điệp lỗi chứa giá trị %q: %s", cam, err)
				}
			}
		})
	}
}
