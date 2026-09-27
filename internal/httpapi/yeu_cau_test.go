package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/store"
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
