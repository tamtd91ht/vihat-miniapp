package config

import (
	"errors"
	"strings"
	"testing"
)

// Khoá giả 32 byte — đủ độ dài tối thiểu, không phải khoá thật của môi trường nào.
const khoaCauGia = "khoa-cau-gia-lap-trong-test-0032"

func TestCauPhien_CaHaiTrongThiTatVaKhoiDongBinhThuong(t *testing.T) {
	cfg, err := Nap(tra(dayDu()))
	if err != nil {
		t.Fatalf("thiếu cả hai biến cầu mà không khởi động: %s", err)
	}
	if cfg.CauPhienBat() {
		t.Error("không khai biến cầu nào mà cầu lại bật")
	}

	// Khai mà để trống (dòng `X=` trong .env.local) cũng là tắt.
	m := dayDu()
	m[EnvVigovCauDiaChi], m[EnvVigovCauKhoa] = "  ", ""
	if cfg, err := Nap(tra(m)); err != nil || cfg.CauPhienBat() {
		t.Fatalf("hai biến rỗng phải là tắt, nhận bat=%v err=%v", cfg.CauPhienBat(), err)
	}
}

func TestCauPhien_DuHaiBienThiBatVaGiuNguyenDanhSach(t *testing.T) {
	m := dayDu()
	m[EnvVigovCauDiaChi] = " identity-cau-1.vigov.svc:9091 , identity-cau-2.vigov.svc:9091,"
	m[EnvVigovCauKhoa] = khoaCauGia

	cfg, err := Nap(tra(m))
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if !cfg.CauPhienBat() {
		t.Fatal("đủ hai biến mà cầu không bật")
	}
	// Dạng cụm: giữ CẢ HAI địa chỉ, không bao giờ cắt lấy một.
	if len(cfg.VigovCauDiaChi) != 2 || cfg.VigovCauDiaChi[1] != "identity-cau-2.vigov.svc:9091" {
		t.Errorf("danh sách địa chỉ = %q", cfg.VigovCauDiaChi)
	}
	if cfg.VigovCauKhoa.Lo() != khoaCauGia {
		t.Error("Lo() phải trả khoá thật")
	}
}

// Nửa cấu hình là HỎNG, không phải nửa bật: service không khởi động, và thông
// điệp nêu tên CẢ HAI biến để người vận hành biết đang thiếu cặp nào.
func TestCauPhien_NuaCauHinhThiTuChoiKhoiDong(t *testing.T) {
	cases := map[string]map[string]string{
		"có địa chỉ, thiếu khoá": {EnvVigovCauDiaChi: "identity-cau:9091"},
		"có khoá, thiếu địa chỉ": {EnvVigovCauKhoa: khoaCauGia},
		"địa chỉ chỉ toàn dấu phẩy nhưng có khoá": {
			EnvVigovCauDiaChi: " , ,", EnvVigovCauKhoa: khoaCauGia},
	}
	for ten, them := range cases {
		t.Run(ten, func(t *testing.T) {
			m := dayDu()
			for k, v := range them {
				m[k] = v
			}
			_, err := Nap(tra(m))
			if err == nil {
				t.Fatal("nửa cấu hình cầu mà vẫn khởi động")
			}
			if !errors.Is(err, ErrThieuBien) {
				t.Errorf("lỗi phải gói ErrThieuBien: %s", err)
			}
			if !strings.Contains(err.Error(), EnvVigovCauDiaChi) {
				t.Errorf("thông điệp không nêu %s: %s", EnvVigovCauDiaChi, err)
			}
			if strings.Contains(err.Error(), khoaCauGia) {
				t.Error("thông điệp lỗi chứa GIÁ TRỊ khoá cầu")
			}
		})
	}
}

func TestCauPhien_DiaChiSaiHinhDangThiTuChoi(t *testing.T) {
	for _, sai := range []string{
		"identity-cau",                    // thiếu cổng
		"http://identity-cau:9091",        // có scheme
		"dns:///identity-cau:9091",        // có scheme của gRPC
		"identity-cau:9091/duong-dan",     // có đường dẫn
		":9091",                           // thiếu host
		"identity-cau:0",                  // cổng ngoài khoảng
		"identity-cau:cong",               // cổng không phải số
		"identity-cau:9091,khong-co-cong", // MỘT phần tử hỏng là cả danh sách hỏng
	} {
		t.Run(sai, func(t *testing.T) {
			m := dayDu()
			m[EnvVigovCauDiaChi], m[EnvVigovCauKhoa] = sai, khoaCauGia
			if _, err := Nap(tra(m)); err == nil || !strings.Contains(err.Error(), EnvVigovCauDiaChi) {
				t.Fatalf("địa chỉ %q phải bị từ chối, nhận err=%v", sai, err)
			}
		})
	}
}

// Máy chủ ViGov không mở cổng cầu với khoá < 32 byte, nên một khoá ngắn ở đây
// chắc chắn là khoá sai — bắt lúc khởi động, không để mọi lượt đăng nhập hỏng.
func TestCauPhien_KhoaNganThiTuChoiMaKhongInKhoa(t *testing.T) {
	m := dayDu()
	ngan := "khoa-ngan-31-byte-gia-lap-00031"
	m[EnvVigovCauDiaChi], m[EnvVigovCauKhoa] = "identity-cau:9091", ngan
	_, err := Nap(tra(m))
	if err == nil || !strings.Contains(err.Error(), EnvVigovCauKhoa) {
		t.Fatalf("khoá ngắn phải bị từ chối và nêu tên biến, nhận: %v", err)
	}
	if strings.Contains(err.Error(), ngan) {
		t.Error("thông điệp lỗi chứa GIÁ TRỊ khoá cầu")
	}
}
