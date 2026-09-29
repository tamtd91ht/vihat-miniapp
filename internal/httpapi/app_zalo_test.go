package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// App ID giả của một app riêng — dãy chữ số, không phải app của xã nào.
const (
	appIDXaGia  = "9990000000000000001"
	appIDChungG = "1234567890" // = appIDGia: App ID app chung trong các test cầu
)

// appXaGia — bộ đổi mang "secret của app riêng". Đếm lượt riêng để test chứng
// minh được lượt đổi đi qua ĐÚNG app, không qua bộ đổi của app chung.
type appXaGia struct {
	so       string
	loi      error
	goiSo    int
	goiViTri int
}

func (a *appXaGia) LaySoDienThoai(context.Context, string, string) (string, error) {
	a.goiSo++
	if a.loi != nil {
		return "", a.loi
	}
	return a.so, nil
}

func (a *appXaGia) LayViTri(context.Context, string, string) (float64, float64, error) {
	a.goiViTri++
	if a.loi != nil {
		return 0, 0, a.loi
	}
	return viDoGia, kinhDoGia, nil
}

func dungServerAppXa(t *testing.T, xa *appXaGia) bo {
	t.Helper()
	b := dungServerCau(t, &cauGia{kq: ketQuaCoXa()}, &maZaloGia{ma: maTaiKhoanGia})
	b.s.appIDChung = appIDChungG
	b.s.VoiAppXa(map[string]ZaloCuaApp{appIDXaGia: xa})
	return b
}

// Nhánh app riêng (chủ dự án chốt 29/09/2026): KHÔNG cần tên miền; cầu nhận
// App ID CỦA APP RIÊNG; số đổi bằng secret của app riêng — và lượt đổi ấy là
// bước xác minh App ID.
func TestAppXa_DiCauVoiAppIDCuaXa(t *testing.T) {
	xa := &appXaGia{so: soGia}
	b := dungServerAppXa(t, xa)

	w := goiDangNhap(t, b.s, `{"accessToken":"at","phoneToken":"pt","appId":"`+appIDXaGia+`"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã = %d; thân = %s", w.Code, w.Body.String())
	}
	if len(b.cau.nhan) != 1 {
		t.Fatalf("cầu được gọi %d lần, mong 1", len(b.cau.nhan))
	}
	yc := b.cau.nhan[0]
	if yc.AppID != appIDXaGia {
		t.Errorf("App ID sang cầu = %q, mong App ID của app riêng %q (không phải app chung)", yc.AppID, appIDXaGia)
	}
	if yc.TenMienXa != "" {
		t.Errorf("app riêng không gửi tên miền mà cầu nhận %q", yc.TenMienXa)
	}
	if yc.SoDaXacThuc != soGia || yc.MaTaiKhoan != maTaiKhoanGia {
		t.Error("số hoặc mã tài khoản không sang cầu")
	}
	if xa.goiSo != 1 || b.z.goi != 0 {
		t.Errorf("đổi số: app riêng %d lượt, app chung %d lượt — mong 1 và 0 (secret của đúng app)", xa.goiSo, b.z.goi)
	}
	if b.kho.soLanTao != 0 {
		t.Error("app riêng mà phát phiên thương mại")
	}
	var ra phanHoiPhienViGov
	if err := json.Unmarshal(w.Body.Bytes(), &ra); err != nil || ra.VigovSession.Token != tokenViGovGia {
		t.Errorf("thân không mang phiên ViGov: %s", w.Body.String())
	}
	b.khongRo(t, w.Body.String())
}

// Không phoneToken = không có lượt đổi có secret nào để xác minh App ID: 400
// TRƯỚC mọi lời gọi ra ngoài, không lùi về phiên thương mại.
func TestAppXa_ThieuPhoneTokenThi400KhongGoiGi(t *testing.T) {
	xa := &appXaGia{so: soGia}
	b := dungServerAppXa(t, xa)

	w := goiDangNhap(t, b.s, `{"accessToken":"at","appId":"`+appIDXaGia+`","communeHostHint":"xa-a.vigov.vn","communeConfirmed":true}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("mã = %d, mong 400", w.Code)
	}
	if b.ma.goi != 0 || xa.goiSo != 0 || b.z.goi != 0 || len(b.cau.nhan) != 0 || b.kho.soLanTao != 0 {
		t.Error("app riêng thiếu phoneToken mà vẫn gọi Zalo / cầu / phát phiên")
	}
	var ph phanHoiLoi
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil || ph.Message != loiCanSoDeXacMinhApp {
		t.Errorf("câu trả về = %q", ph.Message)
	}
}

// Secret của app riêng bị Zalo từ chối (token của app khác, hoặc token hỏng):
// App ID KHÔNG được xác minh -> 401, cầu không được gọi.
func TestAppXa_DoiSoBangSecretCuaXaHongThiKhongDiCau(t *testing.T) {
	xa := &appXaGia{loi: zalo.ErrTokenKhongHopLe}
	b := dungServerAppXa(t, xa)

	w := goiDangNhap(t, b.s, `{"accessToken":"at","phoneToken":"pt","appId":"`+appIDXaGia+`"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("mã = %d, mong 401", w.Code)
	}
	if len(b.cau.nhan) != 0 {
		t.Error("App ID chưa xác minh mà vẫn gọi cầu với nó")
	}
	if b.z.goi != 0 {
		t.Error("lùi về secret app chung sau khi secret app riêng hỏng")
	}
}

// App ID lạ: 422, không gọi Zalo, không gọi cầu, KHÔNG lùi về app chung/phiên
// thương mại, và giá trị client đặt không vào log.
func TestAppXa_AppIDLaThi422(t *testing.T) {
	const la = "5550000000000000009-la"
	b := dungServerAppXa(t, &appXaGia{so: soGia})

	w := goiDangNhap(t, b.s, `{"accessToken":"at","phoneToken":"pt","appId":"`+la+`"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mã = %d, mong 422", w.Code)
	}
	if b.ma.goi != 0 || b.z.goi != 0 || len(b.cau.nhan) != 0 || b.kho.soLanTao != 0 {
		t.Error("App ID lạ mà vẫn gọi ra ngoài hoặc phát phiên")
	}
	if strings.Contains(b.log.String(), la) {
		t.Error("App ID do client đặt bị ghi vào log")
	}
}

// appId = app chung: y như không khai — không tên miền thì phiên thương mại.
func TestAppXa_AppIDChungThiHanhViCu(t *testing.T) {
	b := dungServerAppXa(t, &appXaGia{so: soGia})
	w := goiDangNhap(t, b.s, `{"accessToken":"at","phoneToken":"pt","appId":"`+appIDChungG+`"}`)
	if w.Code != http.StatusCreated || b.kho.soLanTao != 1 || len(b.cau.nhan) != 0 {
		t.Fatalf("app chung khai tường minh: mã = %d, phiên thương mại = %d, cầu = %d", w.Code, b.kho.soLanTao, len(b.cau.nhan))
	}
	// Có tên miền: cầu với App ID app chung, như trước.
	w = goiDangNhap(t, b.s, `{"accessToken":"at","appId":"`+appIDChungG+`","communeHostHint":"xa-a.vigov.vn"}`)
	if w.Code != http.StatusCreated || len(b.cau.nhan) != 1 || b.cau.nhan[0].AppID != appIDGia {
		t.Fatalf("app chung + tên miền: mã = %d, cầu = %+v", w.Code, b.cau.nhan)
	}
}

// Cầu chưa lắp ráp mà yêu cầu đến từ app riêng: 503 — không bao giờ phiên
// thương mại cho người dân của xã.
func TestAppXa_CauChuaLapRapThi503(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{so: soGia})
	xa := &appXaGia{so: soGia}
	s.VoiAppXa(map[string]ZaloCuaApp{appIDXaGia: xa})

	w := goiDangNhap(t, s, `{"accessToken":"at","phoneToken":"pt","appId":"`+appIDXaGia+`"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã = %d, mong 503", w.Code)
	}
	if kho.soLanTao != 0 || xa.goiSo != 0 {
		t.Error("cầu tắt mà app riêng vẫn đổi số hoặc nhận phiên thương mại")
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/location — secret của đúng app.
// ---------------------------------------------------------------------------

func TestViTri_AppXaDungSecretCuaXa(t *testing.T) {
	chung := &viTriGia{}
	s, _ := dungServerViTri(t, chung)
	xa := &appXaGia{}
	s.VoiAppXa(map[string]ZaloCuaApp{appIDXaGia: xa})

	w := goiViTri(t, s, `{"accessToken":"at","locationToken":"lt","appId":"`+appIDXaGia+`"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("mã = %d; thân = %s", w.Code, w.Body.String())
	}
	if xa.goiViTri != 1 || chung.goi != 0 {
		t.Errorf("đổi vị trí: app riêng %d, app chung %d — mong 1 và 0", xa.goiViTri, chung.goi)
	}

	// Không khai appId: app chung, như trước.
	if w := goiViTri(t, s, thanViTriHopLe); w.Code != http.StatusOK || chung.goi != 1 {
		t.Fatalf("không appId: mã = %d, app chung %d lượt", w.Code, chung.goi)
	}
}

func TestViTri_AppIDLaThi422(t *testing.T) {
	chung := &viTriGia{}
	s, _ := dungServerViTri(t, chung)

	w := goiViTri(t, s, `{"accessToken":"at","locationToken":"lt","appId":"123"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mã = %d, mong 422", w.Code)
	}
	if ph := docLoiCoMa(t, w); ph.Code != "app_not_configured" {
		t.Errorf("code = %q, mong app_not_configured", ph.Code)
	}
	if chung.goi != 0 {
		t.Error("App ID lạ mà lùi về secret app chung")
	}
}
