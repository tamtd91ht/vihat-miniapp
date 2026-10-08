package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postClientError(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/client-errors", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:51000" // TEST-NET-3, RFC 5737
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// The line Rancher shows must carry the prefix and the three things that tell the -1401 causes apart.
func TestClientErrors_LogsPrefixCodeAndSdkMessage(t *testing.T) {
	s, log := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	w := postClientError(t, s, `{"capability":"access-token","code":-1401,
		"message":"Login failed: Zalo app has not been activated","appId":"1234567890","host":"xa-mau.vigov.vn"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	out := log.String()
	for _, want := range []string{clientErrorLogPrefix, "access-token", "-1401", "Zalo app has not been activated", "1234567890", "xa-mau.vigov.vn"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "203.0.113.9") {
		t.Errorf("client IP must not be logged:\n%s", out)
	}
}

// A client must not be able to write a fake second log line, nor an unbounded one, nor pick the
// capability label or forge app/host values.
func TestClientErrors_SanitisesClientValues(t *testing.T) {
	s, log := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	long := strings.Repeat("x", 500)
	w := postClientError(t, s, `{"capability":"evil","code":1,"message":"a\nlevel=ERROR forged `+long+`",
		"appId":"abc","host":"Bad Host/x","phone":"0900000000"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	out := log.String()
	if strings.Count(out, "\n") != 1 {
		t.Errorf("expected exactly one log line:\n%s", out)
	}
	if strings.Contains(out, strings.Repeat("x", clientErrorMaxMessage)) {
		t.Errorf("message not cut to %d characters", clientErrorMaxMessage)
	}
	for _, bad := range []string{"evil", "abc", "Bad Host", "0900000000"} {
		if strings.Contains(out, bad) {
			t.Errorf("log carries client value %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "unknown") {
		t.Errorf("unknown capability not labelled:\n%s", out)
	}
}

func TestClientErrors_400WithoutCode(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	for _, body := range []string{`{"capability":"phone"}`, `not json`} {
		if w := postClientError(t, s, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, w.Code)
		}
	}
}

func TestClientErrors_405OnGet(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/client-errors", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

// The report is sent from the Zalo webview, a cross-origin page: without CORS it never arrives.
func TestClientErrors_CORS(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	r := httptest.NewRequest(http.MethodOptions, "/api/v1/client-errors", nil)
	r.Header.Set("Origin", "https://h5.zdn.vn")
	r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://h5.zdn.vn" {
		t.Fatalf("preflight: status = %d, Allow-Origin = %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestClientErrors_429After30PerHour(t *testing.T) {
	s, _ := dungServer(t, &khoGia{}, &zaloGia{so: soGia})
	body := `{"capability":"phone","code":-1401}`
	for i := 0; i < clientErrorMaxReports; i++ {
		if w := postClientError(t, s, body); w.Code != http.StatusNoContent {
			t.Fatalf("report %d: status = %d, want 204", i+1, w.Code)
		}
	}
	if w := postClientError(t, s, body); w.Code != http.StatusTooManyRequests {
		t.Fatalf("report %d: status = %d, want 429", clientErrorMaxReports+1, w.Code)
	}
}
