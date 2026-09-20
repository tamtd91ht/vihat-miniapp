package phien

import (
	"encoding/base64"
	"testing"
)

func TestSinhToken_DuNgauNhienVaKhongTrungNhau(t *testing.T) {
	daThay := make(map[string]struct{}, 512)
	for i := 0; i < 512; i++ {
		tok, err := SinhToken()
		if err != nil {
			t.Fatalf("SinhToken lỗi: %s", err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token không phải base64url không đệm: %s", err)
		}
		if len(raw) != soByteToken {
			t.Fatalf("token dài %d byte, mong %d", len(raw), soByteToken)
		}
		if _, trung := daThay[tok]; trung {
			t.Fatal("hai token trùng nhau — nguồn ngẫu nhiên hỏng")
		}
		daThay[tok] = struct{}{}
	}
}

// 32 byte là ràng buộc của cột phien.token_bam (CHECK octet_length = 32).
func TestBam_Dung32ByteVaOnDinh(t *testing.T) {
	b1 := Bam("mot-token-bat-ky")
	if len(b1) != 32 {
		t.Fatalf("băm dài %d byte, mong 32 — lệch ràng buộc CSDL", len(b1))
	}
	if string(b1) != string(Bam("mot-token-bat-ky")) {
		t.Fatal("băm không ổn định")
	}
	if string(b1) == string(Bam("mot-token-khac")) {
		t.Fatal("hai token khác nhau cho cùng một băm")
	}
}

// Băm phải KHÁC token: cả nghĩa lẫn nội dung. Đây là điều kiện để một bản sao
// CSDL rò ra ngoài vẫn vô dụng với kẻ lấy được nó.
func TestBam_KhongChuaToken(t *testing.T) {
	const tok = "token-mau-trong-test"
	if string(Bam(tok)) == tok {
		t.Fatal("băm trả về chính token")
	}
}
