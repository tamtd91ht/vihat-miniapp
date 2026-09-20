package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// Số giả đã thống nhất: 0900000000 -> 84900000000 sau chuẩn hoá.
const soGia = "84900000000"

// ---------------------------------------------------------------------------
// Bản giả của hai phụ thuộc
// ---------------------------------------------------------------------------

type ghiChepThatBai struct{ ketQua, lyDo string }

type khoGia struct {
	mu sync.Mutex

	loiTao  error
	loiPing error
	loiGhi  error

	soNhan    string
	bamNhan   []byte
	hetHanVao time.Time
	ipNhan    *netip.Addr
	soLanTao  int

	thatBai []ghiChepThatBai
}

func (k *khoGia) TaoPhienDangNhap(_ context.Context, so string, bam []byte, hetHan time.Time, ip *netip.Addr) (phien.KetQuaTao, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.soLanTao++
	k.soNhan, k.bamNhan, k.hetHanVao, k.ipNhan = so, bam, hetHan, ip
	if k.loiTao != nil {
		return phien.KetQuaTao{}, k.loiTao
	}
	return phien.KetQuaTao{
		NguoiDungID: "3f1c0c9e-0000-4000-8000-000000000001",
		PhienID:     "3f1c0c9e-0000-4000-8000-000000000002",
		HetHanLuc:   hetHan,
	}, nil
}

func (k *khoGia) GhiNhatKyThatBai(_ context.Context, ketQua, lyDo string, _ *netip.Addr) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.thatBai = append(k.thatBai, ghiChepThatBai{ketQua, lyDo})
	return k.loiGhi
}

func (k *khoGia) Ping(context.Context) error { return k.loiPing }

func (k *khoGia) dem(ketQua string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, g := range k.thatBai {
		if g.ketQua == ketQua {
			n++
		}
	}
	return n
}

type zaloGia struct {
	so  string
	loi error
	goi int
}

func (z *zaloGia) LaySoDienThoai(context.Context, string, string) (string, error) {
	z.goi++
	if z.loi != nil {
		return "", z.loi
	}
	return z.so, nil
}

// ---------------------------------------------------------------------------

func dungServer(t *testing.T, kho Kho, z DoiTokenZalo) (*Server, *bytes.Buffer) {
	t.Helper()
	var log bytes.Buffer
	cfg := config.Config{CORSAllowedOrigins: []string{"https://h5.zdn.vn"}}
	// Mức Debug: nếu có dòng log nào lỡ mang dữ liệu cá nhân, phép kiểm phải
	// thấy nó — bắt ở mức Info thì một log.Debug vẫn lọt ra máy chủ thật.
	s := Moi(kho, z, cfg, slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return s, &log
}

func goiDangNhap(t *testing.T, s *Server, than string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(than))
	r.RemoteAddr = "203.0.113.7:51000" // TEST-NET-3, RFC 5737
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

const thanHopLe = `{"accessToken":"at-gia-lap","phoneToken":"pt-gia-lap"}`

func TestTaoPhien_201(t *testing.T) {
	kho := &khoGia{}
	s, log := dungServer(t, kho, &zaloGia{so: soGia})
	truoc := time.Now()

	w := goiDangNhap(t, s, thanHopLe)

	if w.Code != http.StatusCreated {
		t.Fatalf("mã = %d, mong 201; thân = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, mong no-store: thân 201 mang bearer token", got)
	}

	var ph phanHoiPhien
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân không phải JSON: %s", err)
	}
	if ph.Token == "" {
		t.Error("thiếu khoá token")
	}
	hetHan, err := time.Parse(time.RFC3339, ph.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt không phải RFC3339: %s", err)
	}

	// TTL 7 ngày — chính sách sản phẩm.
	mong := truoc.Add(config.TTLPhien)
	if lech := hetHan.Sub(mong); lech > time.Minute || lech < -time.Minute {
		t.Errorf("expiresAt lệch %s so với mốc 7 ngày", lech)
	}

	// Kho phải nhận BẢN BĂM của đúng token vừa trả, không phải token.
	if string(kho.bamNhan) != string(phien.Bam(ph.Token)) {
		t.Error("kho không nhận được băm của token vừa cấp")
	}
	if string(kho.bamNhan) == ph.Token {
		t.Error("kho nhận token nguyên bản — phải là bản băm")
	}
	if kho.soNhan != soGia {
		t.Errorf("số gửi xuống kho = %q, mong số đã chuẩn hoá", kho.soNhan)
	}
	if kho.ipNhan == nil || kho.ipNhan.String() != "203.0.113.7" {
		t.Errorf("IP ghi vào nhật ký = %v, mong IP của khách", kho.ipNhan)
	}

	// Nghị định 13: số điện thoại không ra thân phản hồi, không vào log.
	if strings.Contains(w.Body.String(), soGia) {
		t.Error("thân phản hồi chứa số điện thoại")
	}
	if strings.Contains(log.String(), soGia) {
		t.Errorf("log chứa số điện thoại: %s", log.String())
	}
	// Token phiên cũng không được vào log: log là thứ nhân bản đi khắp nơi.
	if strings.Contains(log.String(), ph.Token) {
		t.Error("log chứa bearer token")
	}
}

func TestTaoPhien_401_KhiZaloTuChoiToken(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{loi: zalo.ErrTokenKhongHopLe})

	w := goiDangNhap(t, s, thanHopLe)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("mã = %d, mong 401", w.Code)
	}
	var ph phanHoiLoi
	if err := json.Unmarshal(w.Body.Bytes(), &ph); err != nil {
		t.Fatalf("thân lỗi không phải JSON: %s", err)
	}
	if ph.Message == "" {
		t.Fatal("thiếu khoá message — khoá phía Mini App đọc")
	}
	// Không mã lỗi kỹ thuật, không thông điệp của Zalo lọt ra ngoài.
	for _, cam := range []string{"zalo:", "error", "401", "token"} {
		if strings.Contains(strings.ToLower(ph.Message), cam) {
			t.Errorf("câu trả về lộ chi tiết kỹ thuật %q: %s", cam, ph.Message)
		}
	}
	if kho.dem(phien.KetQuaTokenZaloHong) != 1 {
		t.Error("lần thất bại phải để lại đúng một dòng nhật ký token_zalo_hong")
	}
	if kho.soLanTao != 0 {
		t.Error("không được mở phiên khi Zalo từ chối token")
	}
}

func TestTaoPhien_502_KhiKhongVoiToiZalo(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{loi: zalo.ErrKhongVoiToiZalo})

	w := goiDangNhap(t, s, thanHopLe)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("mã = %d, mong 502", w.Code)
	}
	if kho.dem(phien.KetQuaLoiZalo) != 1 {
		t.Error("phải ghi nhật ký loi_zalo")
	}
	// Lỗi phía ta thì KHÔNG được bảo người dùng đăng nhập lại — họ làm gì cũng vô ích.
	var ph phanHoiLoi
	_ = json.Unmarshal(w.Body.Bytes(), &ph)
	if strings.Contains(ph.Message, "đăng nhập lại") {
		t.Errorf("502 mà bảo người dùng đăng nhập lại: %s", ph.Message)
	}
}

func TestTaoPhien_400_ThanHongHoacThieuTruong(t *testing.T) {
	cases := map[string]string{
		"không phải JSON":   `khong-phai-json`,
		"thiếu phoneToken":  `{"accessToken":"at-gia-lap"}`,
		"thiếu accessToken": `{"phoneToken":"pt-gia-lap"}`,
		"rỗng":              `{}`,
	}
	for ten, than := range cases {
		t.Run(ten, func(t *testing.T) {
			z := &zaloGia{so: soGia}
			s, _ := dungServer(t, &khoGia{}, z)
			w := goiDangNhap(t, s, than)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("mã = %d, mong 400", w.Code)
			}
			if z.goi != 0 {
				t.Error("không được gọi sang Zalo khi yêu cầu đã sai từ đầu")
			}
		})
	}
}

func TestTaoPhien_500_KhiKhoHong(t *testing.T) {
	kho := &khoGia{loiTao: errors.New("csdl đứt kết nối")}
	s, log := dungServer(t, kho, &zaloGia{so: soGia})

	w := goiDangNhap(t, s, thanHopLe)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("mã = %d, mong 500", w.Code)
	}
	var ph phanHoiLoi
	_ = json.Unmarshal(w.Body.Bytes(), &ph)
	if strings.Contains(ph.Message, "csdl") {
		t.Errorf("lỗi nội bộ rò ra ngoài: %s", ph.Message)
	}
	if strings.Contains(log.String(), soGia) {
		t.Error("log chứa số điện thoại")
	}
	if kho.dem(phien.KetQuaLoiHeThong) != 1 {
		t.Error("phải ghi nhật ký loi_he_thong")
	}
}

func TestTaoPhien_429_VaChiMotDongNhatKyMoiDot(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{so: soGia})

	for i := 0; i < SoLuotToiDa; i++ {
		if w := goiDangNhap(t, s, thanHopLe); w.Code != http.StatusCreated {
			t.Fatalf("lượt %d: mã = %d, mong 201", i+1, w.Code)
		}
	}
	for i := 0; i < 3; i++ {
		if w := goiDangNhap(t, s, thanHopLe); w.Code != http.StatusTooManyRequests {
			t.Fatalf("lượt vượt trần thứ %d: mã = %d, mong 429", i+1, w.Code)
		}
	}
	if n := kho.dem(phien.KetQuaQuaNhieuLan); n != 1 {
		t.Errorf("có %d dòng nhật ký qua_nhieu_lan, mong đúng 1 cho cả đợt — ghi mọi lượt là tự khuếch đại tấn công", n)
	}
}

func TestTaoPhien_405_KhiKhongPhaiPOST(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mã = %d, mong 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, mong POST", got)
	}
}

// Nhật ký phải được ghi kể cả khi client đã ngắt kết nối: dòng đó là thứ duy
// nhất còn lại của lần thử ấy.
func TestTaoPhien_GhiNhatKyKhiClientNgat(t *testing.T) {
	kho := &khoGia{}
	s, _ := dungServer(t, kho, &zaloGia{loi: zalo.ErrTokenKhongHopLe})

	ctx, huy := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(thanHopLe)).WithContext(ctx)
	r.RemoteAddr = "203.0.113.7:51000"
	huy() // client biến mất trước khi handler ghi nhật ký

	s.Handler().ServeHTTP(httptest.NewRecorder(), r)

	if kho.dem(phien.KetQuaTokenZaloHong) != 1 {
		t.Error("client ngắt kết nối mà nhật ký không được ghi")
	}
}

func TestHealthz(t *testing.T) {
	t.Run("200 khi CSDL còn sống", func(t *testing.T) {
		s, _ := dungServer(t, &khoGia{}, &zaloGia{})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("mã = %d, mong 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"ok"`) {
			t.Errorf("thân = %s", w.Body.String())
		}
	})

	t.Run("503 khi mất CSDL", func(t *testing.T) {
		s, _ := dungServer(t, &khoGia{loiPing: errors.New("mat ket noi")}, &zaloGia{})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("mã = %d, mong 503: bản sao mất CSDL không phục vụ nổi một lượt đăng nhập", w.Code)
		}
		if strings.Contains(w.Body.String(), "mat ket noi") {
			t.Error("healthz công khai mà lộ lỗi nội bộ")
		}
	})
}
