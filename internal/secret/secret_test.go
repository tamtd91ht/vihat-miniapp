package secret

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const giaTriThat = "gia-tri-that-chi-co-trong-test"

// Mọi đường in ra mặc định phải an toàn: đây là lý do kiểu này tồn tại.
func TestSecret_KhongRoRaQuaDuongIn(t *testing.T) {
	s := Secret(giaTriThat)

	cases := map[string]string{
		"String()":     s.String(),
		"%s":           fmt.Sprintf("%s", s),
		"%v":           fmt.Sprintf("%v", s),
		"%q":           fmt.Sprintf("%q", s),
		"%#v":          fmt.Sprintf("%#v", s),
		"trong struct": fmt.Sprintf("%+v", struct{ Key Secret }{s}),
	}
	for ten, got := range cases {
		if strings.Contains(got, giaTriThat) {
			t.Errorf("%s rò secret: %s", ten, got)
		}
	}

	b, err := json.Marshal(struct {
		Key Secret `json:"key"`
	}{s})
	if err != nil {
		t.Fatalf("json.Marshal lỗi: %s", err)
	}
	if strings.Contains(string(b), giaTriThat) {
		t.Errorf("json.Marshal rò secret: %s", b)
	}
}

func TestSecret_LoTraGiaTriThat(t *testing.T) {
	if got := Secret(giaTriThat).Lo(); got != giaTriThat {
		t.Errorf("Lo() = %q, mong giá trị thật", got)
	}
}
