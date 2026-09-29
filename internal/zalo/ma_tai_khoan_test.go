package zalo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vihat/vihat-miniapp/internal/secret"
)

// Cùng giới hạn như client_test.go: bộ chuyển giả trả đúng hình dạng mà
// wire.go GIẢ ĐỊNH (theo bản tham chiếu ở kho yêu cầu). Các ca dưới chứng minh
// mã khớp giả định — KHÔNG chứng minh giả định đúng.

// maTaiKhoanGiaLap — dài hơn 15 chữ số, đúng loại số float64 làm tròn.
const maTaiKhoanGiaLap = "8123456789012345678"

// chuyenGia là http.RoundTripper giả: không mở cổng, không chạm mạng.
type chuyenGia func(*http.Request) (*http.Response, error)

func (f chuyenGia) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientChuyenGia(f chuyenGia) *Client {
	return New("https://graph.zalo.me.invalid", secret.Secret(khoaGiaLap),
		VoiHTTPClient(&http.Client{Transport: f}))
}

func traThan(ma int, than string) chuyenGia {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: ma, Body: io.NopCloser(strings.NewReader(than)), Header: http.Header{}}, nil
	}
}

func TestLayMaTaiKhoan_ThanhCong_ChiXinIDKhongGuiSecret(t *testing.T) {
	var nhan *http.Request
	c := clientChuyenGia(func(r *http.Request) (*http.Response, error) {
		nhan = r
		return traThan(http.StatusOK, `{"id":"`+maTaiKhoanGiaLap+`","error":0,"message":"Success"}`)(r)
	})

	ma, err := c.LayMaTaiKhoan(context.Background(), tokenGiaLap)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if ma != maTaiKhoanGiaLap {
		t.Errorf("mã = %q", ma)
	}
	if nhan.Method != http.MethodGet || nhan.URL.Path != DuongDanMaTaiKhoan {
		t.Errorf("gọi %s %s, mong GET %s", nhan.Method, nhan.URL.Path, DuongDanMaTaiKhoan)
	}
	// CHỈ id: xin thêm trường cá nhân thì Zalo từ chối cả lời gọi từ IP ngoài VN.
	if got := nhan.URL.RawQuery; got != "fields=id" {
		t.Errorf("query = %q, mong đúng fields=id", got)
	}
	if nhan.Header.Get(HeaderAccessToken) != tokenGiaLap {
		t.Error("accessToken phải đi bằng header access_token")
	}
	// Không cần secret, nên không gửi: một bí mật gửi thừa là một bí mật đi xa hơn cần.
	for _, h := range []string{HeaderSecretKey, HeaderCode, HeaderAppSecretProof} {
		if nhan.Header.Get(h) != "" {
			t.Errorf("header %s không được gửi ở lời gọi mã tài khoản", h)
		}
	}
	if strings.Contains(nhan.URL.String(), tokenGiaLap) || strings.Contains(nhan.URL.String(), khoaGiaLap) {
		t.Error("URL mang token hoặc secret")
	}
}

// Số JSON dài hơn 15 chữ số phải giữ NGUYÊN — đi qua float64 là thành mã khác.
func TestLayMaTaiKhoan_IDLaSoJSONKhongBiLamTron(t *testing.T) {
	c := clientChuyenGia(traThan(http.StatusOK, `{"id":`+maTaiKhoanGiaLap+`,"error":0}`))
	ma, err := c.LayMaTaiKhoan(context.Background(), tokenGiaLap)
	if err != nil || ma != maTaiKhoanGiaLap {
		t.Fatalf("mã = %q, err = %v — mong nguyên văn %s", ma, err, maTaiKhoanGiaLap)
	}
}

// Zalo trả 200 kèm error != 0: token hỏng -> ErrTokenKhongHopLe (tầng HTTP: 401).
// Lỗi không mang message của Zalo (chuỗi ta không kiểm soát), không mang token.
func TestLayMaTaiKhoan_LoiTrongThan200(t *testing.T) {
	for _, than := range []string{
		`{"error":-216,"message":"Access token is invalid ` + tokenGiaLap + `"}`,
		`{"id":"` + maTaiKhoanGiaLap + `","error":452,"message":"Session key invalid"}`,
	} {
		c := clientChuyenGia(traThan(http.StatusOK, than))
		ma, err := c.LayMaTaiKhoan(context.Background(), tokenGiaLap)
		if !errors.Is(err, ErrTokenKhongHopLe) {
			t.Fatalf("%s: err = %v, mong ErrTokenKhongHopLe", than, err)
		}
		if ma != "" {
			t.Errorf("%s: báo lỗi mà vẫn trả mã %q", than, ma)
		}
		for _, cam := range []string{tokenGiaLap, maTaiKhoanGiaLap, "Access token", "Session key"} {
			if strings.Contains(err.Error(), cam) {
				t.Errorf("lỗi chứa %q: %s", cam, err)
			}
		}
	}
}

// error == 0 mà không có mã đọc được: lỗi phía hạ tầng (502), KHÔNG bảo người
// dân đăng nhập lại, và KHÔNG BAO GIỜ trả một mã rỗng/đoán.
func TestLayMaTaiKhoan_ThieuHoacSaiID(t *testing.T) {
	for _, than := range []string{
		`{"error":0,"message":"Success"}`,
		`{"id":"","error":0}`,
		`{"id":"   ","error":0}`,
		`{"id":null,"error":0}`,
		`{"id":1.5e18,"error":0}`,
		`{"id":-12,"error":0}`,
		`{"id":{"v":"1"},"error":0}`,
		`{"data":{"id":"` + maTaiKhoanGiaLap + `"},"error":0}`, // hình dạng phong bì của /me/info — không phải của /me
		`khong-phai-json`,
	} {
		c := clientChuyenGia(traThan(http.StatusOK, than))
		ma, err := c.LayMaTaiKhoan(context.Background(), tokenGiaLap)
		if !errors.Is(err, ErrKhongVoiToiZalo) {
			t.Errorf("%s: err = %v, mong ErrKhongVoiToiZalo", than, err)
		}
		if ma != "" {
			t.Errorf("%s: trả mã %q", than, ma)
		}
		if err != nil && strings.Contains(err.Error(), maTaiKhoanGiaLap) {
			t.Errorf("%s: lỗi mang mã tài khoản", than)
		}
	}
}

func TestLayMaTaiKhoan_LoiVanChuyenVaHTTP(t *testing.T) {
	cases := map[string]chuyenGia{
		"không với tới": func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: kết nối bị từ chối")
		},
		"HTTP 500": traThan(http.StatusInternalServerError, `{"error":0,"id":"1"}`),
		"HTTP 403": traThan(http.StatusForbidden, ``),
	}
	for ten, f := range cases {
		t.Run(ten, func(t *testing.T) {
			_, err := clientChuyenGia(f).LayMaTaiKhoan(context.Background(), tokenGiaLap)
			if !errors.Is(err, ErrKhongVoiToiZalo) {
				t.Fatalf("err = %v, mong ErrKhongVoiToiZalo", err)
			}
			if errors.Is(err, ErrTokenKhongHopLe) {
				t.Error("lỗi hạ tầng bị phân loại thành token hỏng — người dân sẽ bị bảo đăng nhập lại mãi")
			}
		})
	}
}

// Lời gọi mang hạn riêng ngắn (timeoutMaTaiKhoan): context truyền xuống bộ
// chuyển phải có deadline dù context của bên gọi không có.
func TestLayMaTaiKhoan_CoHanNgan(t *testing.T) {
	c := clientChuyenGia(func(r *http.Request) (*http.Response, error) {
		if _, co := r.Context().Deadline(); !co {
			t.Error("lời gọi mã tài khoản không có deadline")
		}
		return traThan(http.StatusOK, `{"id":"1","error":0}`)(r)
	})
	if _, err := c.LayMaTaiKhoan(context.Background(), tokenGiaLap); err != nil {
		t.Fatal(err)
	}
}

func TestLayMaTaiKhoan_ThieuTokenKhongGoiMang(t *testing.T) {
	goi := 0
	c := clientChuyenGia(func(r *http.Request) (*http.Response, error) {
		goi++
		return traThan(http.StatusOK, `{"id":"1","error":0}`)(r)
	})
	if _, err := c.LayMaTaiKhoan(context.Background(), ""); !errors.Is(err, ErrTokenKhongHopLe) {
		t.Fatalf("err = %v", err)
	}
	if goi != 0 {
		t.Error("thiếu accessToken mà vẫn gọi Zalo")
	}
}
