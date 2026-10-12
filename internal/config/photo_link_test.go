package config

import (
	"errors"
	"strings"
	"testing"
)

const photoLinkKeyFake = "khoa-ky-link-anh-gia-lap-test-0032"

func withWebhook() map[string]string {
	m := dayDu()
	m[EnvWebhookURL], m[EnvWebhookKhoa] = "https://nhan.vihat.vn/su-kien", khoaWebhookGia
	return m
}

func TestPhotoLinks_BothEmptyIsOff(t *testing.T) {
	cfg, err := Nap(tra(withWebhook()))
	if err != nil {
		t.Fatalf("no photo-link vars must still start: %s", err)
	}
	if cfg.PhotoLinksOn() {
		t.Fatal("photo links on with no variable set")
	}
}

func TestPhotoLinks_BothSetIsOn(t *testing.T) {
	m := withWebhook()
	m[EnvPublicAPIBaseURL], m[EnvPhotoLinkKey] = " https://api.vihat.vn/miniapp/ ", photoLinkKeyFake
	cfg, err := Nap(tra(m))
	if err != nil {
		t.Fatalf("unexpected: %s", err)
	}
	if !cfg.PhotoLinksOn() || cfg.PublicAPIBaseURL != "https://api.vihat.vn/miniapp" || cfg.PhotoLinkKey.Lo() != photoLinkKeyFake {
		t.Fatalf("config wrong: base=%q", cfg.PublicAPIBaseURL)
	}
}

func TestPhotoLinks_BadConfigRefusesToStart(t *testing.T) {
	cases := map[string]struct {
		webhook        bool
		base, key, env string
	}{
		"base without key":      {true, "https://api.vihat.vn", "", EnvPublicAPIBaseURL},
		"key without base":      {true, "", photoLinkKeyFake, EnvPhotoLinkKey},
		"on without webhook":    {false, "https://api.vihat.vn", photoLinkKeyFake, EnvWebhookURL},
		"http base":             {true, "http://api.vihat.vn", photoLinkKeyFake, EnvPublicAPIBaseURL},
		"base with query":       {true, "https://api.vihat.vn/?x=1", photoLinkKeyFake, EnvPublicAPIBaseURL},
		"short key":             {true, "https://api.vihat.vn", "khoa-ngan-31-byte-gia-lap-00031", EnvPhotoLinkKey},
		"same key as webhook":   {true, "https://api.vihat.vn", khoaWebhookGia, EnvPhotoLinkKey},
		"base with credentials": {true, "https://ai:mat-khau@api.vihat.vn", photoLinkKeyFake, EnvPublicAPIBaseURL},
		"base without host":     {true, "https:///x", photoLinkKeyFake, EnvPublicAPIBaseURL},
		"relative base":         {true, "api.vihat.vn", photoLinkKeyFake, EnvPublicAPIBaseURL},
		"base with fragment":    {true, "https://api.vihat.vn/#a", photoLinkKeyFake, EnvPublicAPIBaseURL},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := dayDu()
			if c.webhook {
				m = withWebhook()
			}
			m[EnvPublicAPIBaseURL], m[EnvPhotoLinkKey] = c.base, c.key
			_, err := Nap(tra(m))
			if err == nil || !errors.Is(err, ErrThieuBien) {
				t.Fatalf("bad photo-link config started: %v", err)
			}
			if !strings.Contains(err.Error(), c.env) {
				t.Errorf("message does not name %s: %s", c.env, err)
			}
			for _, secretValue := range []string{photoLinkKeyFake, khoaWebhookGia, "mat-khau"} {
				if strings.Contains(err.Error(), secretValue) {
					t.Errorf("message carries a secret value: %s", err)
				}
			}
		})
	}
}
