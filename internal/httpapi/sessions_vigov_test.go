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

// thanCauHopLe — thân đi CẦU: có communeHostHint (công dân vừa xác nhận xã).
// Không có tên miền thì cầu bật cũng đi phiên thương mại — xem sessions.go.
const thanCauHopLe = `{"accessToken":"at-gia-lap","phoneToken":"pt-gia-lap","communeHostHint":"xa-a.vigov.vn","communeConfirmed":true}`

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

	w := goiDangNhap(t, b.s, thanCauHopLe)

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
	// Có tên miền nhưng CHƯA xác nhận: app chính theo xã đã nhớ, ở đây là không xã.
	w := goiDangNhap(t, b.s, `{"accessToken":"at","communeHostHint":"xa-a.vigov.vn"}`)
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
	if !strings.Contains(than, `"communePrimaryHost":""`) {
		t.Errorf("thiếu communePrimaryHost rỗng: %s", than)
	}
}

// communePrimaryHost: có thì đi NGUYÊN VĂN; rỗng thì "" — không bao giờ lấy
// tên miền client gửi (communeHostHint) để điền thay.
func TestPhienViGov_TenMienChinh(t *testing.T) {
	t.Run("có", func(t *testing.T) {
		kq := ketQuaCoXa()
		kq.TenMienChinh = "xa-b.vigov.vn" // khác hint: xã kế thừa sau sáp nhập
		b := dungServerCau(t, &cauGia{kq: kq}, &maZaloGia{ma: maTaiKhoanGia})
		w := goiDangNhap(t, b.s, thanCauHopLe)
		if w.Code != http.StatusCreated {
			t.Fatalf("mã = %d; thân = %s", w.Code, w.Body.String())
		}
		var ra phanHoiPhienViGov
		if err := json.Unmarshal(w.Body.Bytes(), &ra); err != nil {
			t.Fatal(err)
		}
		if ra.VigovSession.CommunePrimaryHost != "xa-b.vigov.vn" {
			t.Errorf("communePrimaryHost = %q, mong nguyên văn ViGov trả", ra.VigovSession.CommunePrimaryHost)
		}
	})
	t.Run("rỗng", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})
		w := goiDangNhap(t, b.s, thanCauHopLe)
		if w.Code != http.StatusCreated {
			t.Fatalf("mã = %d; thân = %s", w.Code, w.Body.String())
		}
		if than := w.Body.String(); !strings.Contains(than, `"communePrimaryHost":""`) {
			t.Errorf("ViGov trả rỗng mà thân không mang \"\" (hoặc bị điền thay): %s", than)
		}
	})
}

// CHỌN NHÁNH THEO YÊU CẦU (27/09/2026): cầu bật, KHÔNG tên miền -> phiên thương
// mại, cùng hình dạng thân như cầu tắt. Đây là đường của Tư vấn / Yêu cầu của tôi.
func TestPhienViGov_CauBatKhongTenMienThiPhienThuongMai(t *testing.T) {
	for _, than := range []string{
		thanHopLe,
		// communeConfirmed không có tên miền: không đủ để đi cầu.
		`{"accessToken":"at-gia-lap","phoneToken":"pt-gia-lap","communeConfirmed":true}`,
		`{"accessToken":"at-gia-lap","phoneToken":"pt-gia-lap","communeHostHint":""}`,
	} {
		b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})
		w := goiDangNhap(t, b.s, than)
		if w.Code != http.StatusCreated {
			t.Fatalf("%s: mã = %d; thân = %s", than, w.Code, w.Body.String())
		}
		var ph map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
			t.Fatal(err)
		}
		if _, co := ph["token"]; !co {
			t.Errorf("%s: thân thiếu `token` gốc — phần thương mại mất phiên", than)
		}
		if _, co := ph["expiresAt"]; !co {
			t.Errorf("%s: thân thiếu `expiresAt`", than)
		}
		if _, co := ph["vigovSession"]; co || len(ph) != 2 {
			t.Errorf("%s: thân khác hình dạng cũ: %s", than, w.Body.String())
		}
		if len(b.cau.nhan) != 0 || b.ma.goi != 0 {
			t.Errorf("%s: không tên miền mà vẫn đi cầu", than)
		}
		if b.kho.soLanTao != 1 {
			t.Errorf("%s: không phát phiên của kho này", than)
		}
	}
	// Thiếu phoneToken: như phiên thương mại cũ, 400 — không lẳng lặng đi cầu.
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})
	if w := goiDangNhap(t, b.s, `{"accessToken":"at"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("cầu bật, không tên miền, thiếu phoneToken: mã = %d, mong 400", w.Code)
	}
	if len(b.cau.nhan) != 0 {
		t.Error("thiếu phoneToken mà rơi sang cầu")
	}
}

// Tên miền sai khuôn vẫn đi cầu như trước (so khác rỗng NGUYÊN VĂN, không trim):
// ViGov từ chối INVALID_ARGUMENT -> 400, không lùi về phiên thương mại.
func TestPhienViGov_TenMienSaiKhuonVanDiCau(t *testing.T) {
	for _, hint := range []string{"   ", "https://xa-a.vigov.vn:443/x", "XA-A.VIGOV.VN"} {
		b := dungServerCau(t, &cauGia{loi: vigovcau.ErrYeuCauSai}, &maZaloGia{ma: maTaiKhoanGia})
		than, _ := json.Marshal(map[string]any{"accessToken": "at", "communeHostHint": hint, "communeConfirmed": true})
		w := goiDangNhap(t, b.s, string(than))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%q: mã = %d, mong 400", hint, w.Code)
		}
		if len(b.cau.nhan) != 1 || b.cau.nhan[0].TenMienXa != hint {
			t.Errorf("%q: không đi cầu nguyên văn", hint)
		}
		if b.kho.soLanTao != 0 {
			t.Errorf("%q: cầu từ chối mà lùi về phiên thương mại", hint)
		}
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
			w := goiDangNhap(t, b.s, thanCauHopLe)
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

	w := goiDangNhap(t, b.s, thanCauHopLe)

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
		if w := goiDangNhap(t, b.s, thanCauHopLe); w.Code != http.StatusUnauthorized {
			t.Fatalf("mã = %d, mong 401", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("Zalo từ chối token mà vẫn gọi cầu")
		}
	})
	t.Run("mã tài khoản rỗng -> 502, không gửi mã rỗng", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{ma: ""})
		if w := goiDangNhap(t, b.s, thanCauHopLe); w.Code != http.StatusBadGateway {
			t.Fatalf("mã = %d, mong 502", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("gửi mã tài khoản rỗng sang ViGov")
		}
	})
	t.Run("đổi số hỏng -> 401, không gọi cầu", func(t *testing.T) {
		b := dungServerCau(t, &cauGia{}, &maZaloGia{ma: maTaiKhoanGia})
		b.z.loi = zalo.ErrTokenKhongHopLe
		if w := goiDangNhap(t, b.s, thanCauHopLe); w.Code != http.StatusUnauthorized {
			t.Fatalf("mã = %d, mong 401", w.Code)
		}
		if len(b.cau.nhan) != 0 {
			t.Error("phoneToken hỏng mà vẫn mở phiên — phiên ấy sẽ mang trạng thái số sai")
		}
	})
}

func TestPhienViGov_400_ThieuAccessToken(t *testing.T) {
	for _, than := range []string{
		`{}`, `{"phoneToken":"pt"}`, `khong-phai-json`,
		`{"communeHostHint":"xa-a.vigov.vn"}`,
		`{"phoneToken":"pt","communeHostHint":"xa-a.vigov.vn","communeConfirmed":true}`,
	} {
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
