package zalo

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// ĐỌC TRƯỚC KHI TIN NHỮNG PHÉP KIỂM NÀY — nhắc lại, vì đây chính là gói mà
// cmd/thu-zalo dựa vào để KẾT LUẬN về giao thức:
//
// Máy chủ giả dưới đây trả đúng hình dạng mà wire.go GIẢ ĐỊNH Zalo trả. Các ca
// dưới đây vì thế chỉ chứng minh MỘT điều: phần phân loại kết quả khớp với giả
// định của chính mã này — đúng mã HTTP, đúng error/message, đúng hình dạng số,
// và không có số điện thoại nào rò ra KetQuaChanDoan.
//
// Chúng KHÔNG chứng minh giả định đúng. Hình dạng wire vẫn ở mức chứng cứ
// TRUNG BÌNH cho tới khi `make thu-zalo` chạy THẬT một lần với token thật lấy
// từ một chiếc điện thoại thật (NỢ #1 trong README). Một bộ test xanh ở đây
// tuyệt đối không được dùng để gạch mục nợ ấy.

func TestChanDoan_ThanhCong_KhongMangSoRaNgoai(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `"},"error":0,"message":"Success"}`))
	})

	cd, err := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if cd.HTTPStatus != http.StatusOK || !cd.CoThanJSON {
		t.Errorf("HTTP = %d, thân JSON = %v; mong 200 và đọc được", cd.HTTPStatus, cd.CoThanJSON)
	}
	if cd.ZaloError != 0 || cd.ZaloMessage != "Success" {
		t.Errorf("error/message = %d/%q, mong 0/\"Success\" — hai trường này là thứ đóng NỢ #3", cd.ZaloError, cd.ZaloMessage)
	}
	if !cd.CoSo {
		t.Error("CoSo = false trong khi Zalo trả số hợp lệ")
	}
	if cd.DangTho.DoDai != 11 || cd.DangTho.HaiKyTuDau != "84" {
		t.Errorf("hình dạng thô = %+v, mong {11 84}", cd.DangTho)
	}
	if cd.ThoiGian <= 0 {
		t.Error("ThoiGian phải được đo — người chạy cần biết Zalo trả lời nhanh hay chậm")
	}
	kiemKhongMangSo(t, cd)
}

// Ca đóng ĐIỀU CHƯA RÕ #3 của wire.go: nếu Zalo trả "09…" thì hình dạng THÔ
// phải nói ra điều đó, chứ không bị chuẩn hoá che mất.
func TestChanDoan_DangThoVaDangChuanKhacNhau(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"number":"0900000000"},"error":0,"message":"Success"}`))
	})

	cd, err := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if cd.DangTho.HaiKyTuDau != "09" || cd.DangTho.DoDai != 10 {
		t.Errorf("hình dạng thô = %+v, mong {10 09} — che mất dạng thô là che mất chính thứ cần biết", cd.DangTho)
	}
	if cd.DangChuan.HaiKyTuDau != "84" || cd.DangChuan.DoDai != 11 {
		t.Errorf("hình dạng chuẩn = %+v, mong {11 84}", cd.DangChuan)
	}
	kiemKhongMangSo(t, cd)
}

func TestChanDoan_PhanLoaiGiongDuongPhucVu(t *testing.T) {
	cases := []struct {
		ten      string
		handler  http.HandlerFunc
		mongLoi  error
		mongHTTP int
		mongErr  int
	}{
		{
			"error khác 0 -> 401, giữ nguyên mã và câu của Zalo",
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":{},"error":-201,"message":"Invalid code"}`))
			},
			ErrTokenKhongHopLe, http.StatusOK, -201,
		},
		{
			"HTTP 403 -> 502, không đổ lỗi cho người dùng",
			func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
			ErrKhongVoiToiZalo, http.StatusForbidden, 0,
		},
		{
			"body không phải JSON -> 502, và ghi nhận là không đọc được thân",
			func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>loi</html>")) },
			ErrKhongVoiToiZalo, http.StatusOK, 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.ten, func(t *testing.T) {
			c := mayChuGia(t, tc.handler)
			cd, err := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
			if !errors.Is(err, tc.mongLoi) {
				t.Fatalf("lỗi = %v, mong %v", err, tc.mongLoi)
			}
			if cd.HTTPStatus != tc.mongHTTP {
				t.Errorf("HTTPStatus = %d, mong %d", cd.HTTPStatus, tc.mongHTTP)
			}
			if cd.ZaloError != tc.mongErr {
				t.Errorf("ZaloError = %d, mong %d", cd.ZaloError, tc.mongErr)
			}
			if cd.CoSo {
				t.Error("CoSo = true trong ca lỗi")
			}
			kiemKhongMangSo(t, cd)
		})
	}
}

// Khối chẩn đoán được thiết kế để DÁN VÀO PHIẾU. Nếu Zalo nhắc lại số điện
// thoại trong message thì message phải bị gạch — một khối "an toàn để dán" mà
// thỉnh thoảng mang theo số thì nguy hiểm hơn một khối luôn mang.
func TestChanDoan_MessageNhacLaiSoThiBiGach(t *testing.T) {
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `"},"error":0,"message":"ok cho ` + soGiaLap + `"}`))
	})

	cd, err := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if strings.Contains(cd.ZaloMessage, soGiaLap) {
		t.Errorf("message còn nguyên số điện thoại: %q", cd.ZaloMessage)
	}
	if !strings.Contains(cd.ZaloMessage, "[số đã ẩn]") {
		t.Errorf("message = %q, mong thấy dấu vết đã gạch để người đọc biết có gì bị ẩn", cd.ZaloMessage)
	}
	kiemKhongMangSo(t, cd)
}

func TestChanDoan_MessageQuaDaiThiCat(t *testing.T) {
	dai := strings.Repeat("x", gioiHanMessage+50)
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{},"error":-9999,"message":"` + dai + `"}`))
	})

	cd, _ := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if len([]rune(cd.ZaloMessage)) > gioiHanMessage+len([]rune("…[cắt]")) {
		t.Errorf("message dài %d ký tự — phải cắt, không thì nó che mất phần chẩn đoán bên dưới", len([]rune(cd.ZaloMessage)))
	}
}

func TestChanDoan_GhiNhanProofBatHayTat(t *testing.T) {
	var hdr http.Header
	c := mayChuGia(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		_, _ = w.Write([]byte(`{"data":{"number":"` + soGiaLap + `"},"error":0}`))
	})

	cd, _ := c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if cd.GuiProof {
		t.Error("GuiProof = true khi công tắc đang tắt — người chạy sẽ kết luận sai về NỢ #2")
	}
	if hdr.Get(HeaderAppSecretProof) != "" {
		t.Error("gửi appsecret_proof dù công tắc tắt")
	}

	c.GuiAppSecretProof = true
	cd, _ = c.ChanDoan(context.Background(), tokenGiaLap, phoneGiaLap)
	if !cd.GuiProof {
		t.Error("GuiProof = false khi công tắc đang bật")
	}
	if hdr.Get(HeaderAppSecretProof) != TinhAppSecretProof(tokenGiaLap, c.secretKey) {
		t.Error("appsecret_proof không được ký như wire.go mô tả")
	}
}

// Bất biến của cả đường chẩn đoán: KetQuaChanDoan không bao giờ mang số điện
// thoại, token hay secret key — bất kể Zalo trả gì.
func kiemKhongMangSo(t *testing.T, cd KetQuaChanDoan) {
	t.Helper()
	van := cd.ZaloMessage + " " + cd.DangTho.HaiKyTuDau + " " + cd.DangChuan.HaiKyTuDau
	for _, cam := range []string{soGiaLap, "900000000", tokenGiaLap, phoneGiaLap, khoaGiaLap} {
		if strings.Contains(van, cam) {
			t.Errorf("kết quả chẩn đoán rò %q: %+v", cam, cd)
		}
	}
}
