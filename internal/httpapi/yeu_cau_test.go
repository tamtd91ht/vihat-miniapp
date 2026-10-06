package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/secret"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/webhook"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// LỚP KHIẾM KHUYẾT TỆP NÀY TỒN TẠI ĐỂ BẮT:
//
//	Bề mặt này là bề mặt ĐẦU TIÊN của kho đọc và ghi dữ liệu của MỘT NGƯỜI CỤ
//	THỂ. Ba thứ hỏng được ở đây mà không có gì đỏ lên ở nơi khác:
//
//	  1. một tuyến quên bọc xác thực  -> ai cũng gọi được, và cũng chẳng thấy lỗi
//	  2. một trường của client đi thẳng vào `nguoi_dung_id` -> đọc dữ liệu người khác
//	  3. ô ghi chú (văn bản tự do) lọt vào log hoặc vào thân lỗi trả về
//
//	Cả ba đều để mọi test khác xanh, build thành công, và tính năng "chạy đúng".

const (
	nguoiA = "3f1c0c9e-0000-4000-8000-00000000000a"
	nguoiB = "3f1c0c9e-0000-4000-8000-00000000000b"
	// Chuỗi hiếm, để phép kiểm tìm nó trong log/thân lỗi mà không đụng phải một
	// từ tiếng Việt ngẫu nhiên nào khác.
	ghiChuBiMat = "ghi-chu-rieng-tu-KHONG-DUOC-LO-RA"
)

// ---------------------------------------------------------------------------
// Bản giả
// ---------------------------------------------------------------------------

// khoYeuCauGia đóng cả ba vai: `Kho` (cho `Moi`), `KhoPhien`, và `yeucau.Kho`.
type khoYeuCauGia struct {
	khoGia

	muYC sync.Mutex

	// token hợp lệ -> người dùng. Khoá là chuỗi bearer THÔ, còn tầng xác thực
	// tra bằng BẢN BĂM; ánh xạ ấy nằm ở `TraPhienConHieuLuc` bên dưới.
	phienHopLe map[string]string

	daTao     []yeucau.ThongTinTao
	demGoiLai int
	demZNS    int
	dsCuaToi  map[string][]yeucau.TomTat

	loiTaoYeuCau error
}

func moiKhoYeuCau() *khoYeuCauGia {
	return &khoYeuCauGia{
		phienHopLe: map[string]string{},
		dsCuaToi:   map[string][]yeucau.TomTat{},
	}
}

func (k *khoYeuCauGia) TraPhienConHieuLuc(_ context.Context, bam []byte) (string, error) {
	k.muYC.Lock()
	defer k.muYC.Unlock()
	for tok, nguoi := range k.phienHopLe {
		if bytes.Equal(phien.Bam(tok), bam) {
			return nguoi, nil
		}
	}
	return "", store.ErrKhongCoPhien
}

func (k *khoYeuCauGia) TaoYeuCau(_ context.Context, tt yeucau.ThongTinTao) (string, error) {
	k.muYC.Lock()
	defer k.muYC.Unlock()
	if k.loiTaoYeuCau != nil {
		return "", k.loiTaoYeuCau
	}
	k.daTao = append(k.daTao, tt)
	return "3f1c0c9e-0000-4000-8000-0000000000c1", nil
}

func (k *khoYeuCauGia) DemTrongCuaSo(context.Context, string, string, time.Time) (int, error) {
	k.muYC.Lock()
	defer k.muYC.Unlock()
	return k.demGoiLai, nil
}

func (k *khoYeuCauGia) DanhSachCuaToi(_ context.Context, nguoiDungID string, _ int) ([]yeucau.TomTat, error) {
	k.muYC.Lock()
	defer k.muYC.Unlock()
	return k.dsCuaToi[nguoiDungID], nil
}

func (k *khoYeuCauGia) SoDeLienHe(context.Context, string) (string, error) { return soGia, nil }

func (k *khoYeuCauGia) DemZNSTrongCuaSo(context.Context, string, time.Time) (int, error) {
	k.muYC.Lock()
	defer k.muYC.Unlock()
	return k.demZNS, nil
}

func (k *khoYeuCauGia) GhiVetZNS(context.Context, string, string, string, string, string) error {
	return nil
}

type tongDaiGia struct {
	mu     sync.Mutex
	soNhan []string
	loi    error
}

func (t *tongDaiGia) GoiLai(_ context.Context, so, _ string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.soNhan = append(t.soNhan, so)
	return t.loi
}

// ---------------------------------------------------------------------------

func dungServerYeuCau(t *testing.T, kho *khoYeuCauGia, td yeucau.TongDai) (*Server, *bytes.Buffer) {
	t.Helper()
	var log bytes.Buffer
	cfg := config.Config{CORSAllowedOrigins: []string{"https://h5.zdn.vn"}}
	// Mức Debug có chủ đích: một dòng `log.Debug` mang ghi chú của người dùng
	// vẫn ra tới máy chủ thật, nên phép kiểm phải nhìn thấy nó.
	lg := slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := Moi(kho, &zaloGia{so: soGia}, cfg, lg).
		VoiYeuCau(kho, yeucau.Moi(kho, td, nil, lg))
	return s, &log
}

func goiYeuCau(t *testing.T, s *Server, phuongThuc, bearer, than string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if than == "" {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(than)
	}
	r := httptest.NewRequest(phuongThuc, "/api/v1/requests", body)
	r.RemoteAddr = "203.0.113.9:51000" // TEST-NET-3, RFC 5737
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// ---------------------------------------------------------------------------
// Xác thực
// ---------------------------------------------------------------------------

func TestYeuCau_KhongCoTokenThi401(t *testing.T) {
	kho := moiKhoYeuCau()
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	for _, ca := range []struct {
		ten    string
		bearer string
		header string
	}{
		{"không có header", "", ""},
		{"token không có phiên", "token-bia-ra", ""},
	} {
		t.Run(ca.ten, func(t *testing.T) {
			w := goiYeuCau(t, s, http.MethodPost, ca.bearer, `{"kind":"consult"}`)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("mã %d, mong 401", w.Code)
			}
			if len(kho.daTao) != 0 {
				t.Fatal("đã ghi một yêu cầu cho một người chưa xác thực")
			}
		})
	}
}

// Lược đồ `Bearer` không phân biệt hoa thường (RFC 7235); TOKEN thì có.
func TestYeuCau_LuocDoBearerKhongPhanBietHoaThuong(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["Token-Co-Chu-Hoa"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	r := httptest.NewRequest(http.MethodPost, "/api/v1/requests", strings.NewReader(`{"kind":"consult"}`))
	r.RemoteAddr = "203.0.113.9:51000"
	r.Header.Set("Authorization", "bEaReR Token-Co-Chu-Hoa")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201 — lược đồ phải so không phân biệt hoa thường", w.Code)
	}
}

// ⚠ CA ĐẮT NHẤT TỆP NÀY: client KHÔNG tự khai được mình là ai.
func TestYeuCau_ThanKhongDatDuocNguoiDung(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	// Thân cố tình nhồi đủ mọi tên trường một người sẽ thử.
	than := `{"kind":"consult","nguoi_dung_id":"` + nguoiB + `","userId":"` + nguoiB +
		`","phone":"84900000001","nguoiDungId":"` + nguoiB + `"}`
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", than)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	if len(kho.daTao) != 1 {
		t.Fatalf("ghi %d yêu cầu, mong 1", len(kho.daTao))
	}
	if kho.daTao[0].NguoiDungID != nguoiA {
		t.Fatalf("yêu cầu ghi cho %q, mong %q — thân yêu cầu ĐÃ đặt được người dùng, đó là một tuyến đọc/ghi dữ liệu người khác",
			kho.daTao[0].NguoiDungID, nguoiA)
	}
}

func TestYeuCau_DanhSachChiCuaChinhMinh(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.dsCuaToi[nguoiA] = []yeucau.TomTat{
		{Ma: "ma-cua-a", Loai: yeucau.LoaiTuVan, TrangThai: "moi", TaoLuc: time.Now()},
	}
	kho.dsCuaToi[nguoiB] = []yeucau.TomTat{
		{Ma: "ma-cua-b", Loai: yeucau.LoaiGoiLai, TrangThai: "dong", TaoLuc: time.Now()},
	}
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	w := goiYeuCau(t, s, http.MethodGet, "tok-a", "")
	if w.Code != http.StatusOK {
		t.Fatalf("mã %d, mong 200", w.Code)
	}
	if strings.Contains(w.Body.String(), "ma-cua-b") {
		t.Fatal("danh sách trả về mang yêu cầu của người khác")
	}

	var ph phanHoiDanhSach
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân không đọc được: %s", err)
	}
	if len(ph.Items) != 1 || ph.Items[0].RequestID != "ma-cua-a" {
		t.Fatalf("thân = %s", w.Body.String())
	}
}

// Danh sách rỗng phải là `[]`, không phải `null` — xem chú thích ở handler.
func TestYeuCau_DanhSachRongLaMangRong(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	w := goiYeuCau(t, s, http.MethodGet, "tok-a", "")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf(`mong "items":[], nhận %s`, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Trần gọi lại và cấu hình thiếu
// ---------------------------------------------------------------------------

func TestGoiLai_VuotTranThi429VaKhongGoiRa(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.demGoiLai = yeucau.TranGoiLai // đã dùng hết
	td := &tongDaiGia{}
	s, _ := dungServerYeuCau(t, kho, td)

	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"callback"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("mã %d, mong 429", w.Code)
	}
	if len(td.soNhan) != 0 {
		t.Fatal("tổng đài vẫn quay số dù đã vượt trần")
	}
	if len(kho.daTao) != 0 {
		t.Fatal("vẫn ghi phiếu dù đã vượt trần — lần bấm sau sẽ đếm sai")
	}
}

func TestGoiLai_DuoiTranThiQuaySoLayTuPhien(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.demGoiLai = yeucau.TranGoiLai - 1
	td := &tongDaiGia{}
	s, _ := dungServerYeuCau(t, kho, td)

	// Thân cố tình gửi kèm một số khác — nó phải bị bỏ qua hoàn toàn.
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"callback","phone":"84900000009"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	if len(td.soNhan) != 1 || td.soNhan[0] != soGia {
		t.Fatalf("tổng đài quay %v, mong đúng số của phiên (%s)", td.soNhan, soGia)
	}
}

// Chưa cấu hình tổng đài thì tuyến NÓI RA, chứ không nhận phiếu rồi im lặng.
func TestGoiLai_ChuaCauHinhTongDaiThi503(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, nil)

	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"callback"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã %d, mong 503", w.Code)
	}
	if len(kho.daTao) != 0 {
		t.Fatal("ghi một phiếu gọi lại mà không ai gọi lại được")
	}
	// Tư vấn KHÔNG bị kéo theo: thiếu tổng đài chỉ tắt đúng tính năng của nó.
	w2 := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"consult"}`)
	if w2.Code != http.StatusCreated {
		t.Fatalf("tư vấn trả %d — thiếu tổng đài đã kéo theo cả tuyến tư vấn", w2.Code)
	}
}

// Quên `VoiYeuCau` ở cmd/server phải hiện ra thành 503, KHÔNG phải 404.
func TestYeuCau_ChuaLapRapThi503ChuKhong404(t *testing.T) {
	var log bytes.Buffer
	cfg := config.Config{CORSAllowedOrigins: []string{"https://h5.zdn.vn"}}
	s := Moi(moiKhoYeuCau(), &zaloGia{so: soGia}, cfg,
		slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))

	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"consult"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã %d, mong 503 — 404 sẽ bị đọc thành 'gọi sai đường dẫn'", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Kiểm đầu vào
// ---------------------------------------------------------------------------

func TestYeuCau_TuChoiMaKhongDungKhuon(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	for _, ca := range []struct{ ten, than string }{
		{"kind lạ", `{"kind":"kind-la"}`},
		{"kind rỗng", `{}`},
		{"scale có khoảng trắng", `{"kind":"consult","scale":"duoi 10"}`},
		{"source có ký tự lạ", `{"kind":"consult","source":"qr'; DROP"}`},
		{"interests quá 8 mục", `{"kind":"consult","interests":["a","b","c","d","e","f","g","h","i"]}`},
		{"interests có mục rỗng", `{"kind":"consult","interests":[""]}`},
		{"thân không phải JSON", `khong-phai-json`},
	} {
		t.Run(ca.ten, func(t *testing.T) {
			w := goiYeuCau(t, s, http.MethodPost, "tok-a", ca.than)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("mã %d, mong 400", w.Code)
			}
		})
	}
	if len(kho.daTao) != 0 {
		t.Fatalf("ghi %d yêu cầu từ thân không hợp lệ", len(kho.daTao))
	}
}

func TestYeuCau_GhiChuQuaDaiThi400(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	// 2001 ký tự tiếng Việt: vượt trần RUNE nhưng vẫn dưới trần BYTE của thân.
	dai, _ := json.Marshal(map[string]string{"kind": kindTuVan, "note": strings.Repeat("ữ", 2001)})
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", string(dai))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mã %d, mong 400 — trần phải đếm theo RUNE", w.Code)
	}

	// Và 2000 ký tự thì PHẢI qua: đếm theo byte sẽ chặn nhầm ở đây.
	vua, _ := json.Marshal(map[string]string{"kind": kindTuVan, "note": strings.Repeat("ữ", 2000)})
	w2 := goiYeuCau(t, s, http.MethodPost, "tok-a", string(vua))
	if w2.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201 — 2000 ký tự tiếng Việt bị chặn nhầm vì đếm theo byte", w2.Code)
	}
}

// ---------------------------------------------------------------------------
// Dữ liệu cá nhân không rò ra
// ---------------------------------------------------------------------------

// ⚠ Ô GHI CHÚ LÀ VĂN BẢN TỰ DO — nó KHÔNG được vào log, không vào thân trả về.
func TestYeuCau_GhiChuKhongVaoLogVaKhongVaoThanTraVe(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, log := dungServerYeuCau(t, kho, &tongDaiGia{})

	than, _ := json.Marshal(map[string]string{"kind": kindTuVan, "note": ghiChuBiMat})
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", string(than))
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	if strings.Contains(log.String(), ghiChuBiMat) {
		t.Fatal("ghi chú của người dùng đã vào log — log chảy vào hệ thống gom log, không gỡ lại được")
	}
	if strings.Contains(w.Body.String(), ghiChuBiMat) {
		t.Fatal("ghi chú vọng lại trong thân trả về")
	}
}

// Thân JSON hỏng: thông điệp của bộ giải mã chép lại nguyên văn thứ client gửi.
func TestYeuCau_ThanHongKhongVongLaiNoiDung(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, log := dungServerYeuCau(t, kho, &tongDaiGia{})

	// JSON hỏng ở giữa, và phần hỏng mang chính chuỗi bí mật.
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"consult","note":"`+ghiChuBiMat+`"`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mã %d, mong 400", w.Code)
	}
	if strings.Contains(w.Body.String(), ghiChuBiMat) || strings.Contains(log.String(), ghiChuBiMat) {
		t.Fatal("thông điệp lỗi của bộ giải mã JSON đã chép nội dung người dùng ra ngoài")
	}
}

// Số điện thoại KHÔNG bao giờ vào log, kể cả khi tổng đài từ chối.
func TestGoiLai_SoDienThoaiKhongVaoLogKeCaKhiHong(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	td := &tongDaiGia{loi: errors.New("cổng từ chối")}
	s, log := dungServerYeuCau(t, kho, td)

	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"callback"}`)
	// Phiếu vẫn được ghi: người thật còn gọi lại bằng tay được.
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201 — tổng đài hỏng không làm mất phiếu", w.Code)
	}
	if strings.Contains(log.String(), soGia) {
		t.Fatalf("số điện thoại đã vào log: %s", log.String())
	}
}

// ---------------------------------------------------------------------------
// Ba loại "bấm rồi thôi" (07/10/2026): chat · sms_promo · sms_optout
// ---------------------------------------------------------------------------

func TestBamNut_BaLoaiMoiDuocNhanVaGhiDungMa(t *testing.T) {
	for _, ca := range []struct{ kind, loai string }{
		{"chat", yeucau.LoaiChat},
		{"sms_promo", yeucau.LoaiNhanUuDaiSMS},
		{"sms_optout", yeucau.LoaiHuyUuDaiSMS},
	} {
		t.Run(ca.kind, func(t *testing.T) {
			kho := moiKhoYeuCau()
			kho.phienHopLe["tok-a"] = nguoiA
			// Tổng đài nil: ba loại này KHÔNG được phụ thuộc tổng đài.
			s, _ := dungServerYeuCau(t, kho, nil)

			w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"`+ca.kind+`"}`)
			if w.Code != http.StatusCreated {
				t.Fatalf("mã %d, mong 201 — thân %s", w.Code, w.Body.String())
			}
			var ph phanHoiYeuCau
			if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
				t.Fatalf("thân không đọc được: %s", err)
			}
			if ph.Kind != ca.kind || ph.Status != "moi" || ph.RequestID == "" || ph.CreatedAt == "" {
				t.Fatalf("thân 201 sai hình dạng: %+v", ph)
			}
			if len(kho.daTao) != 1 || kho.daTao[0].Loai != ca.loai {
				t.Fatalf("ghi %+v, mong đúng một phiếu loại %q", kho.daTao, ca.loai)
			}
			if kho.daTao[0].NguoiDungID != nguoiA {
				t.Fatal("phiếu không thuộc người của phiên")
			}
		})
	}
}

func TestBamNut_DanhSachTraTenDay(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	kho.dsCuaToi[nguoiA] = []yeucau.TomTat{
		{Ma: "m1", Loai: yeucau.LoaiChat, TrangThai: "moi", TaoLuc: time.Now()},
		{Ma: "m2", Loai: yeucau.LoaiNhanUuDaiSMS, TrangThai: "moi", TaoLuc: time.Now()},
		{Ma: "m3", Loai: yeucau.LoaiHuyUuDaiSMS, TrangThai: "moi", TaoLuc: time.Now()},
		{Ma: "m4", Loai: yeucau.LoaiTuVan, TrangThai: "moi", TaoLuc: time.Now()},
		{Ma: "m5", Loai: yeucau.LoaiGoiLai, TrangThai: "moi", TaoLuc: time.Now()},
	}
	s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

	w := goiYeuCau(t, s, http.MethodGet, "tok-a", "")
	var ph phanHoiDanhSach
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân không đọc được: %s", err)
	}
	mong := []string{"chat", "sms_promo", "sms_optout", "consult", "callback"}
	if len(ph.Items) != len(mong) {
		t.Fatalf("thân = %s", w.Body.String())
	}
	for i, k := range mong {
		if ph.Items[i].Kind != k {
			t.Errorf("mục %d: kind %q, mong %q — mã cột lọt ra dây", i, ph.Items[i].Kind, k)
		}
	}
}

// Ánh xạ hai chiều phải khép kín: mọi tên dây đi một vòng về đúng nó.
func TestBamNut_AnhXaHaiChieuKhepKin(t *testing.T) {
	for _, k := range []string{"consult", "callback", "chat", "sms_promo", "sms_optout"} {
		loai, ok := loaiTu(k)
		if !ok || kindTu(loai) != k {
			t.Errorf("%q -> %q -> %q", k, loai, kindTu(loai))
		}
	}
}

func TestBamNut_TenHienThi(t *testing.T) {
	for _, ca := range []struct {
		ten  string
		than string
		ma   int
		mong string // TenHienThi ghi xuống, khi 201
	}{
		{"chat có tên", `{"kind":"chat","displayName":"  Nguyễn Văn A  "}`, http.StatusCreated, "Nguyễn Văn A"},
		{"chat không tên", `{"kind":"chat"}`, http.StatusCreated, ""},
		{"chat tên rỗng", `{"kind":"chat","displayName":"   "}`, http.StatusCreated, ""},
		{"chat đúng 100 ký tự", `{"kind":"chat","displayName":"` + strings.Repeat("ữ", 100) + `"}`, http.StatusCreated, strings.Repeat("ữ", 100)},
		{"chat 101 ký tự", `{"kind":"chat","displayName":"` + strings.Repeat("ữ", 101) + `"}`, http.StatusBadRequest, ""},
		{"consult có tên", `{"kind":"consult","displayName":"A"}`, http.StatusBadRequest, ""},
		{"callback có tên", `{"kind":"callback","displayName":"A"}`, http.StatusBadRequest, ""},
		{"sms_promo có tên", `{"kind":"sms_promo","displayName":"A"}`, http.StatusBadRequest, ""},
		{"sms_optout có tên", `{"kind":"sms_optout","displayName":"A"}`, http.StatusBadRequest, ""},
		{"sms_optout tên rỗng", `{"kind":"sms_optout","displayName":""}`, http.StatusCreated, ""},
	} {
		t.Run(ca.ten, func(t *testing.T) {
			kho := moiKhoYeuCau()
			kho.phienHopLe["tok-a"] = nguoiA
			s, _ := dungServerYeuCau(t, kho, &tongDaiGia{})

			w := goiYeuCau(t, s, http.MethodPost, "tok-a", ca.than)
			if w.Code != ca.ma {
				t.Fatalf("mã %d, mong %d", w.Code, ca.ma)
			}
			if ca.ma != http.StatusCreated {
				if len(kho.daTao) != 0 {
					t.Fatal("thân bị từ chối mà vẫn ghi phiếu")
				}
				var ph phanHoiLoi
				if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil || ph.Message != loiTruongKhongHopLe {
					t.Fatalf("câu trả về %s, mong loiTruongKhongHopLe", w.Body.String())
				}
				return
			}
			if len(kho.daTao) != 1 || kho.daTao[0].TenHienThi != ca.mong {
				t.Fatalf("ghi %+v, mong tên %q", kho.daTao, ca.mong)
			}
		})
	}
}

// Ba loại mới cũng không cho client tự khai mình là ai.
func TestBamNut_ThanKhongDatDuocNguoiDung(t *testing.T) {
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, _ := dungServerYeuCau(t, kho, nil)

	than := `{"kind":"sms_optout","nguoi_dung_id":"` + nguoiB + `","userId":"` + nguoiB +
		`","phone":"84900000001","nguoiDungId":"` + nguoiB + `"}`
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", than)
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	if len(kho.daTao) != 1 || kho.daTao[0].NguoiDungID != nguoiA {
		t.Fatalf("phiếu ghi cho %+v, mong %q", kho.daTao, nguoiA)
	}
}

// ---------------------------------------------------------------------------
// Webhook "yêu cầu mới" — đi đủ đường thật: handler -> yeucau -> webhook ->
// máy chủ TLS. Client của httptest mang CA của máy chủ test: kiểm TLS VẪN BẬT.
// ---------------------------------------------------------------------------

const (
	khoaWebhookGia = "khoa-ky-webhook-gia-lap-test-032"
	tenBiMat       = "ten-hien-thi-KHONG-DUOC-VAO-LOG"
)

type mayNhanWebhook struct {
	mu      sync.Mutex
	than    [][]byte
	chuKy   []string
	maTraVe int
}

func moiMayNhan(t *testing.T, maTraVe int) (*httptest.Server, *mayNhanWebhook) {
	t.Helper()
	m := &mayNhanWebhook{maTraVe: maTraVe}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.than = append(m.than, b)
		m.chuKy = append(m.chuKy, r.Header.Get(webhook.HeaderChuKy))
		m.mu.Unlock()
		w.WriteHeader(m.maTraVe)
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func (m *mayNhanWebhook) soLan() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.than)
}

// dongBoGhi — log được ghi từ goroutine nền của webhook; bytes.Buffer trần sẽ
// làm -race đỏ.
type dongBoGhi struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (d *dongBoGhi) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.b.Write(p)
}

func (d *dongBoGhi) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.b.String()
}

func dungServerBaoYeuCau(t *testing.T, kho *khoYeuCauGia, bao yeucau.BaoWebhook) (*Server, *yeucau.DichVu, *dongBoGhi) {
	t.Helper()
	log := &dongBoGhi{}
	cfg := config.Config{CORSAllowedOrigins: []string{"https://h5.zdn.vn"}}
	lg := slog.New(slog.NewJSONHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dv := yeucau.Moi(kho, &tongDaiGia{}, nil, lg).VoiWebhook(bao)
	return Moi(kho, &zaloGia{so: soGia}, cfg, lg).VoiYeuCau(kho, dv), dv, log
}

func choNen(t *testing.T, dv *yeucau.DichVu) {
	t.Helper()
	ctx, huy := context.WithTimeout(context.Background(), 15*time.Second)
	defer huy()
	if err := dv.ChoViecNen(ctx); err != nil {
		t.Fatalf("việc nền chưa xong: %s", err)
	}
}

func TestWebhook_DuocGoiVoiChuKyVaThanDung(t *testing.T) {
	srv, may := moiMayNhan(t, http.StatusNoContent)
	bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia), webhook.VoiHTTPClient(srv.Client()))
	if bao == nil {
		t.Fatal("webhook.Moi trả nil với URL https hợp lệ")
	}
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, dv, _ := dungServerBaoYeuCau(t, kho, bao)

	// Thân nhồi cả một số điện thoại KHÁC: số đi sang webhook phải là số của PHIÊN.
	w := goiYeuCau(t, s, http.MethodPost, "tok-a",
		`{"kind":"chat","displayName":"Nguyễn Văn A","phone":"84900000009","source":"qr-gian-hang"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	choNen(t, dv)

	if may.soLan() != 1 {
		t.Fatalf("webhook nhận %d lần, mong 1", may.soLan())
	}
	than, chuKy := may.than[0], may.chuKy[0]
	if chuKy != webhook.KyThan([]byte(khoaWebhookGia), than) {
		t.Fatalf("chữ ký %q không khớp HMAC-SHA256 của thân thô", chuKy)
	}
	if !strings.HasPrefix(chuKy, "sha256=") || len(chuKy) != len("sha256=")+64 {
		t.Fatalf("chữ ký sai khuôn sha256=<hex>: %q", chuKy)
	}

	var sk map[string]any
	if err := json.Unmarshal(than, &sk); err != nil {
		t.Fatalf("thân webhook không phải JSON: %s", err)
	}
	mong := map[string]any{
		"event":       "request.created",
		"requestId":   "3f1c0c9e-0000-4000-8000-0000000000c1",
		"kind":        "chat",
		"phone":       soGia,
		"displayName": "Nguyễn Văn A",
		"scale":       "",
		"note":        "",
		"source":      "qr-gian-hang",
	}
	for k, v := range mong {
		if sk[k] != v {
			t.Errorf("%s = %v, mong %v", k, sk[k], v)
		}
	}
	if ds, ok := sk["interests"].([]any); !ok || len(ds) != 0 {
		t.Errorf("interests = %v, mong [] (không phải null)", sk["interests"])
	}
	if ts, _ := sk["createdAt"].(string); ts == "" {
		t.Error("thiếu createdAt")
	} else if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("createdAt không phải RFC3339: %q", ts)
	}
}

// Mọi loại đều được báo, kể cả consult / callback đã có từ trước.
func TestWebhook_MoiLoaiDeuDuocBao(t *testing.T) {
	for _, kind := range []string{"consult", "callback", "chat", "sms_promo", "sms_optout"} {
		t.Run(kind, func(t *testing.T) {
			srv, may := moiMayNhan(t, http.StatusOK)
			bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia), webhook.VoiHTTPClient(srv.Client()))
			kho := moiKhoYeuCau()
			kho.phienHopLe["tok-a"] = nguoiA
			s, dv, _ := dungServerBaoYeuCau(t, kho, bao)

			if w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"`+kind+`"}`); w.Code != http.StatusCreated {
				t.Fatalf("mã %d, mong 201", w.Code)
			}
			choNen(t, dv)
			if may.soLan() != 1 {
				t.Fatalf("webhook nhận %d lần, mong 1", may.soLan())
			}
			if !strings.Contains(string(may.than[0]), `"kind":"`+kind+`"`) {
				t.Fatalf("thân không mang tên dây %q: %s", kind, may.than[0])
			}
			if strings.Contains(string(may.than[0]), "displayName") {
				t.Fatal("displayName rỗng phải vắng mặt trong thân")
			}
		})
	}
}

func TestWebhook_ChuaCauHinhThiKhongGoi(t *testing.T) {
	_, may := moiMayNhan(t, http.StatusOK)
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	// Giao diện nil — đúng như cmd/server lắp khi hai biến trống.
	s, dv, _ := dungServerBaoYeuCau(t, kho, nil)

	if w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"sms_promo"}`); w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	choNen(t, dv)
	if may.soLan() != 0 {
		t.Fatal("webhook chưa cấu hình mà vẫn có lời gọi đi ra")
	}
	if len(kho.daTao) != 1 {
		t.Fatal("tắt webhook thì phiếu vẫn phải được ghi")
	}
}

// Bên nhận hỏng không được chạm tới người dùng: vẫn 201, phiếu vẫn ghi, và
// log chỉ có mã yêu cầu + mã HTTP — không số, không tên, không ghi chú.
func TestWebhook_HongKhongAnhHuong201VaKhongRoDuLieu(t *testing.T) {
	srv, may := moiMayNhan(t, http.StatusInternalServerError)
	bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia),
		webhook.VoiHTTPClient(srv.Client()), webhook.VoiNghiGiuaHaiLuot(0))
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, dv, log := dungServerBaoYeuCau(t, kho, bao)

	than, _ := json.Marshal(map[string]string{"kind": "chat", "displayName": tenBiMat, "note": ghiChuBiMat})
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", string(than))
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201 — webhook hỏng đã chạm tới người dùng", w.Code)
	}
	choNen(t, dv)

	if may.soLan() != 2 {
		t.Fatalf("bên nhận trả 500: nhận %d lượt, mong đúng 2 (một lần thử lại)", may.soLan())
	}
	if len(kho.daTao) != 1 {
		t.Fatal("phiếu phải được ghi dù webhook hỏng")
	}
	nk := log.String()
	for _, cam := range []string{soGia, tenBiMat, ghiChuBiMat, khoaWebhookGia} {
		if strings.Contains(nk, cam) {
			t.Fatalf("log chứa %q:\n%s", cam, nk)
		}
	}
	if !strings.Contains(nk, "3f1c0c9e-0000-4000-8000-0000000000c1") || !strings.Contains(nk, "HTTP 500") {
		t.Fatalf("log phải nêu mã yêu cầu và mã HTTP:\n%s", nk)
	}
}

// Bên nhận treo: phản hồi 201 KHÔNG được chờ webhook.
func TestWebhook_KhongLamChamPhanHoi(t *testing.T) {
	nha := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-nha:
		case <-r.Context().Done():
		}
	}))
	// srv.Close cắt kết nối, nên bộ xử lý thoát qua r.Context() kể cả khi ca
	// hỏng trước lúc `nha` được đóng.
	t.Cleanup(srv.Close)

	bao := webhook.Moi(srv.URL, secret.Secret(khoaWebhookGia), webhook.VoiHTTPClient(srv.Client()))
	kho := moiKhoYeuCau()
	kho.phienHopLe["tok-a"] = nguoiA
	s, dv, _ := dungServerBaoYeuCau(t, kho, bao)

	batDau := time.Now()
	w := goiYeuCau(t, s, http.MethodPost, "tok-a", `{"kind":"chat"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("mã %d, mong 201", w.Code)
	}
	if d := time.Since(batDau); d > time.Second {
		t.Fatalf("phản hồi mất %s — tuyến HTTP đang chờ webhook", d)
	}
	close(nha)
	choNen(t, dv)
}
