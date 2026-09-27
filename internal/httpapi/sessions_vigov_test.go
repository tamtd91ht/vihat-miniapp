package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/vigovcau"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// Giá trị giả dễ nhận ra trong log/thân phản hồi. Không cái nào là của ai.
const (
	maTaiKhoanGia = "ma-tai-khoan-zalo-gia-lap-7f3a"
	tokenViGovGia = "token-phien-vigov-gia-lap-9c1e"
	appIDGia      = "1234567890"
)

type cauGia struct {
	nhan []vigovcau.YeuCau
	kq   vigovcau.KetQua
	loi  error
}

func (c *cauGia) MoPhien(_ context.Context, yc vigovcau.YeuCau) (vigovcau.KetQua, error) {
	c.nhan = append(c.nhan, yc)
	if c.loi != nil {
		return vigovcau.KetQua{}, c.loi
	}
	return c.kq, nil
}

type maZaloGia struct {
	ma  string
	loi error
	goi int
}

func (m *maZaloGia) LayMaTaiKhoan(context.Context, string) (string, error) {
	m.goi++
	return m.ma, m.loi
}

func ketQuaCoXa() vigovcau.KetQua {
	return vigovcau.KetQua{
		Token: tokenViGovGia, PhienID: "sid-gia",
		HetHan: time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC),
		TenXa:  "Xã A", DaXacThucSo: true,
	}
}

type bo struct {
	s   *Server
	log *bytes.Buffer
	kho *khoGia
	z   *zaloGia
	ma  *maZaloGia
	cau *cauGia
}

func dungServerCau(t *testing.T, cau *cauGia, ma *maZaloGia) bo {
	t.Helper()
	b := bo{kho: &khoGia{}, z: &zaloGia{so: soGia}, ma: ma, cau: cau}
	b.s, b.log = dungServer(t, b.kho, b.z)
	b.s.VoiCauPhienViGov(cau, ma, appIDGia)
	return b
}

// khongRo kiểm thứ không được rò: số điện thoại, mã tài khoản Zalo, token ViGov
// — trong log (mức Debug), và hai thứ đầu cả trong thân phản hồi.
func (b bo) khongRo(t *testing.T, than string) {
	t.Helper()
	for _, cam := range []string{soGia, maTaiKhoanGia, tokenViGovGia} {
		if strings.Contains(b.log.String(), cam) {
			t.Errorf("log chứa %q: %s", cam, b.log.String())
		}
	}
	for _, cam := range []string{soGia, maTaiKhoanGia} {
		if strings.Contains(than, cam) {
			t.Errorf("thân phản hồi chứa %q", cam)
		}
	}
}

func TestPhienViGov_201_KhongPhoneToken_KhongCoTruongSo(t *testing.T) {
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})

	w := goiDangNhap(t, b.s, `{"accessToken":"at-gia-lap","communeHostHint":"xa-a.vigov.vn","communeConfirmed":true}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã = %d, mong 201; thân = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q — thân mang bearer token của ViGov", got)
	}
	if len(b.cau.nhan) != 1 {
		t.Fatalf("cầu được gọi %d lần, mong 1", len(b.cau.nhan))
	}
	yc := b.cau.nhan[0]
	if yc.SoDaXacThuc != "" {
		t.Error("không có phoneToken mà yêu cầu sang ViGov lại mang số")
	}
	if b.z.goi != 0 {
		t.Error("không có phoneToken mà vẫn gọi đổi số với Zalo")
	}
	if yc.AppID != appIDGia || yc.MaTaiKhoan != maTaiKhoanGia || yc.IPKhach != "203.0.113.7" {
		t.Errorf("yêu cầu sang cầu = app %q, ip %q", yc.AppID, yc.IPKhach)
	}
	if yc.TenMienXa != "xa-a.vigov.vn" || !yc.DaXacNhanXa {
		t.Errorf("tên miền / xác nhận không được chuyển tiếp: %q %v", yc.TenMienXa, yc.DaXacNhanXa)
	}
	// Không phát phiên thương mại, không lưu gì vào CSDL của kho này.
	if b.kho.soLanTao != 0 {
		t.Error("cầu bật mà vẫn phát phiên của kho này")
	}

	var ph map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatal(err)
	}
	if _, co := ph["token"]; co {
		t.Error("thân có khoá `token` ở gốc — khoá của phiên thương mại; phiên ViGov phải nằm ở `vigovSession`")
	}
	var ra phanHoiPhienViGov
	_ = json.Unmarshal(w.Body.Bytes(), &ra)
	if ra.VigovSession.Token != tokenViGovGia {
		t.Errorf("token = %q, mong NGUYÊN VĂN token ViGov trả", ra.VigovSession.Token)
	}
	if ra.VigovSession.TenantDisplayName != "Xã A" || !ra.VigovSession.PhoneVerified ||
		ra.VigovSession.ExpiresAt != "2026-10-27T01:02:03Z" {
		t.Errorf("phiên = %+v", ra.VigovSession)
	}
	b.khongRo(t, w.Body.String())
}

func TestPhienViGov_201_CoPhoneToken_SoDiSangViGovMaKhongRo(t *testing.T) {
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})

	w := goiDangNhap(t, b.s, thanHopLe)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã = %d; thân = %s", w.Code, w.Body.String())
	}
	if b.cau.nhan[0].SoDaXacThuc != soGia {
		t.Error("có phoneToken mà số đã xác thực không sang ViGov")
	}
	if b.kho.soLanTao != 0 {
		t.Error("số điện thoại công dân bị lưu vào CSDL thương mại")
	}
	b.khongRo(t, w.Body.String())
}

// Tên miền đi NGUYÊN VĂN: kho này không trim, không hạ chữ, không kiểm — máy
// chủ ViGov kiểm khuôn và trả INVALID_ARGUMENT.
func TestPhienViGov_TenMienDiNguyenVan(t *testing.T) {
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})
	goiDangNhap(t, b.s, `{"accessToken":"at","communeHostHint":" HTTPS://Xa-A.vigov.vn:443 "}`)
	if got := b.cau.nhan[0].TenMienXa; got != " HTTPS://Xa-A.vigov.vn:443 " {
		t.Errorf("tên miền = %q, mong nguyên văn", got)
	}
	if b.cau.nhan[0].DaXacNhanXa {
		t.Error("không gửi communeConfirmed mà cờ lại bật")
	}
}

// Phiên không xã: hợp đồng không có token — thân không được bịa token hay hạn.
func TestPhienViGov_KhongXaThiKhongToken(t *testing.T) {
	b := dungServerCau(t, &cauGia{kq: vigovcau.KetQua{PhienID: "sid"}}, &maZaloGia{ma: maTaiKhoanGia})
	w := goiDangNhap(t, b.s, `{"accessToken":"at"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("mã = %d", w.Code)
	}
	than := w.Body.String()
	if strings.Contains(than, `"token"`) || strings.Contains(than, `"expiresAt"`) {
		t.Errorf("phiên không xã mà thân có token/hạn: %s", than)
	}
	if !strings.Contains(than, `"tenantDisplayName":""`) {
		t.Errorf("thiếu tenantDisplayName rỗng: %s", than)
	}
}

func TestPhienViGov_MaLoiCau(t *testing.T) {
	cases := []struct {
		loi  error
		ma   int
		lyDo string
	}{
		{vigovcau.ErrYeuCauSai, http.StatusBadRequest, "cau_yeu_cau_sai"},
		{vigovcau.ErrChuaSanSang, http.StatusUnprocessableEntity, "cau_chua_san_sang"},
		{vigovcau.ErrSaiKhoaCau, http.StatusServiceUnavailable, "cau_sai_khoa"},
		{fmt.Errorf("%w (mã Unavailable)", vigovcau.ErrTamNgung), http.StatusServiceUnavailable, "cau_tam_ngung"},
		{errors.New("lỗi lạ"), http.StatusServiceUnavailable, "cau_tam_ngung"},
	}
	for _, c := range cases {
		t.Run(c.lyDo+"/"+c.loi.Error(), func(t *testing.T) {
			b := dungServerCau(t, &cauGia{loi: c.loi}, &maZaloGia{ma: maTaiKhoanGia})
			w := goiDangNhap(t, b.s, thanHopLe)
			if w.Code != c.ma {
				t.Fatalf("mã = %d, mong %d", w.Code, c.ma)
			}
			var ph phanHoiLoi
			if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil || ph.Message == "" {
				t.Fatalf("thân lỗi thiếu message: %s", w.Body.String())
			}
			for _, cam := range []string{"vigovcau", "INVALID", "FAILED", "UNAUTH", "grpc", "Unavailable"} {
				if strings.Contains(ph.Message, cam) {
					t.Errorf("câu trả về lộ chi tiết kỹ thuật %q: %s", cam, ph.Message)
				}
			}
			if b.kho.dem(phien.KetQuaLoiHeThong) != 1 {
				t.Error("lần hỏng ở cầu phải để lại đúng một dòng nhật ký")
			}
			if b.kho.soLanTao != 0 {
				t.Error("cầu hỏng mà lùi về phiên của kho này — hợp đồng cấm")
			}
			b.khongRo(t, w.Body.String())
		})
	}
}

// UNKNOWN #2: chỗ nối hôm nay luôn từ chối. Đường cầu phải DỪNG trước khi tiêu
// phoneToken và trước khi gọi ViGov.
func TestPhienViGov_503_KhiMaTaiKhoanChuaDo(t *testing.T) {
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, nil)
	b.s.VoiCauPhienViGov(b.cau, zalo.MaTaiKhoanChuaDo{}, appIDGia)

	w := goiDangNhap(t, b.s, thanHopLe)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã = %d, mong 503", w.Code)
	}
	if len(b.cau.nhan) != 0 {
		t.Error("chưa có mã tài khoản mà vẫn gọi cầu")
	}
	if b.z.goi != 0 {
		t.Error("đã tiêu phoneToken trong khi đường cầu chắc chắn hỏng")
	}
	if b.kho.dem(phien.KetQuaLoiHeThong) != 1 {
		t.Error("phải ghi nhật ký loi_he_thong")
	}
}

func TestPhienViGov_LoiZalo(t *testing.T) {
	t.Run("token hỏng ở bước mã tài khoản -> 401", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{loi: zalo.ErrTokenKhongHopLe})
		if w := goiDangNhap(t, b.s, thanHopLe); w.Code != http.StatusUnauthorized {
			t.Fatalf("mã = %d, mong 401", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("Zalo từ chối token mà vẫn gọi cầu")
		}
	})
	t.Run("mã tài khoản rỗng -> 502, không gửi mã rỗng", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{ma: ""})
		if w := goiDangNhap(t, b.s, thanHopLe); w.Code != http.StatusBadGateway {
			t.Fatalf("mã = %d, mong 502", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("gửi mã tài khoản rỗng sang ViGov")
		}
	})
	t.Run("đổi số hỏng -> 401, không gọi cầu", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{ma: maTaiKhoanGia})
		b.z.loi = zalo.ErrTokenKhongHopLe
		if w := goiDangNhap(t, b.s, thanHopLe); w.Code != http.StatusUnauthorized {
			t.Fatalf("mã = %d, mong 401", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("phoneToken hỏng mà vẫn mở phiên — phiên ấy sẽ mang trạng thái số sai")
		}
	})
}

func TestPhienViGov_400_ThieuAccessToken(t *testing.T) {
	for _, than := range []string{`{}`, `{"phoneToken":"pt"}`, `khong-phai-json`} {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{ma: maTaiKhoanGia})
		if w := goiDangNhap(t, b.s, than); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: mã = %d, mong 400", than, w.Code)
		}
		if b.ma.goi != 0 || len(b.cau.nhan) != 0 {
			t.Errorf("%s: yêu cầu sai từ đầu mà vẫn gọi ra ngoài", than)
		}
	}
}

// Cầu TẮT: hai trường mới bị bỏ qua, hành vi cũ nguyên vẹn (phoneToken vẫn bắt buộc).
func TestPhienViGov_CauTatThiHanhViCu(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{so: soGia})
	if w := goiDangNhap(t, s, `{"accessToken":"at","communeHostHint":"xa-a.vigov.vn"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("cầu tắt, thiếu phoneToken: mã = %d, mong 400 như cũ", w.Code)
	}
	w := goiDangNhap(t, s, `{"accessToken":"at","phoneToken":"pt","communeHostHint":"xa-a.vigov.vn","communeConfirmed":true}`)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("cầu tắt: mã = %d, thân = %s — mong 201 phiên của kho này", w.Code, w.Body.String())
	}
	if kho.soLanTao != 1 {
		t.Error("cầu tắt mà không phát phiên của kho này")
	}
}
