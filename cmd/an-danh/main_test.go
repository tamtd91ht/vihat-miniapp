package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/store"
)

// Số giả đã thống nhất và dạng đã chuẩn hoá của nó.
const (
	soNhap     = "0900000000"
	soChuanHoa = "84900000000"
)

type khoGia struct {
	nhan store.YeuCauAnDanh
	goi  int
	loi  error
}

func (k *khoGia) AnDanhHoa(_ context.Context, yc store.YeuCauAnDanh) (store.KetQuaAnDanh, error) {
	k.goi++
	k.nhan = yc
	if k.loi != nil {
		return store.KetQuaAnDanh{}, k.loi
	}
	return store.KetQuaAnDanh{
		NguoiDungID:   "3f1c0c9e-0000-4000-8000-000000000001",
		SoPhienThuHoi: 2,
		ThoiDiem:      time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
	}, nil
}

func dauVao(dong ...string) string { return strings.Join(dong, "\n") + "\n" }

func TestChay_AnDanhThanhCong(t *testing.T) {
	kho := &khoGia{}
	var ra bytes.Buffer

	err := chay(context.Background(),
		strings.NewReader(dauVao(soNhap, "hotline", "NV-017", "PH-2026-0412", xacNhanCanGo)),
		&ra, kho)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}

	if kho.goi != 1 {
		t.Fatalf("gọi kho %d lần, mong 1", kho.goi)
	}
	if kho.nhan.SoDienThoai != soChuanHoa {
		t.Errorf("số gửi xuống kho = %q, mong dạng đã chuẩn hoá — tra bằng dạng khác thì không ra ai", kho.nhan.SoDienThoai)
	}
	if kho.nhan.NguonYeuCau != "hotline" || kho.nhan.NguoiThucHien != "NV-017" || kho.nhan.GhiChu != "PH-2026-0412" {
		t.Errorf("yêu cầu gửi xuống kho sai: %+v", struct{ Nguon, Nguoi, GhiChu string }{
			kho.nhan.NguonYeuCau, kho.nhan.NguoiThucHien, kho.nhan.GhiChu})
	}

	// Nghị định 13: đầu ra của chính lệnh này cũng không được mang số điện thoại.
	kiemKhongCoSo(t, ra.String())
	if !strings.Contains(ra.String(), "3f1c0c9e-0000-4000-8000-000000000001") {
		t.Error("đầu ra thiếu mã định danh — người vận hành cần nó để dán vào phiếu")
	}
}

func TestChay_ChuaXacNhanThiKhongDongGi(t *testing.T) {
	kho := &khoGia{}
	var ra bytes.Buffer

	err := chay(context.Background(),
		strings.NewReader(dauVao(soNhap, "hotline", "NV-017", "", "co")),
		&ra, kho)
	if err == nil {
		t.Fatal("gõ sai chuỗi xác nhận mà vẫn chạy tiếp")
	}
	if kho.goi != 0 {
		t.Error("đã chạm vào kho dù chưa xác nhận")
	}
	kiemKhongCoSo(t, ra.String()+err.Error())
}

func TestChay_DauVaoSaiThiDungLai(t *testing.T) {
	cases := map[string][]string{
		"số không hợp lệ":       {"khong-phai-so", "hotline", "NV-017", "", xacNhanCanGo},
		"nguồn lạ":              {soNhap, "zalo", "NV-017", "", xacNhanCanGo},
		"thiếu người tiếp nhận": {soNhap, "hotline", "", "", xacNhanCanGo},
		"đầu vào cụt":           {soNhap, "hotline"},
	}
	for ten, dong := range cases {
		t.Run(ten, func(t *testing.T) {
			kho := &khoGia{}
			var ra bytes.Buffer
			err := chay(context.Background(), strings.NewReader(dauVao(dong...)), &ra, kho)
			if err == nil {
				t.Fatal("mong dừng lại kèm lỗi")
			}
			if kho.goi != 0 {
				t.Error("đã chạm vào kho dù đầu vào chưa hợp lệ")
			}
			if !strings.Contains(err.Error(), "không có gì được thay đổi") &&
				!strings.Contains(err.Error(), "đầu vào") {
				t.Errorf("thông điệp lỗi nên nói rõ chưa có gì bị đổi: %s", err)
			}
			kiemKhongCoSo(t, ra.String()+err.Error())
		})
	}
}

// Lỗi từ kho cũng không được mang số điện thoại ra ngoài.
func TestChay_LoiTuKhoKhongMangSo(t *testing.T) {
	kho := &khoGia{loi: store.ErrKhongTimThayNguoiDung}
	var ra bytes.Buffer

	err := chay(context.Background(),
		strings.NewReader(dauVao(soNhap, "email", "NV-017", "", xacNhanCanGo)),
		&ra, kho)
	if !errors.Is(err, store.ErrKhongTimThayNguoiDung) {
		t.Fatalf("lỗi = %v, mong ErrKhongTimThayNguoiDung", err)
	}
	kiemKhongCoSo(t, ra.String()+err.Error())
}

func kiemKhongCoSo(t *testing.T, vanBan string) {
	t.Helper()
	for _, cam := range []string{soNhap, soChuanHoa} {
		if strings.Contains(vanBan, cam) {
			t.Errorf("số điện thoại lọt ra đầu ra/lỗi: %s", vanBan)
		}
	}
}
