package vigovcau

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// BẢN CHÉP HỢP ĐỒNG PHẢI KHỚP NGUỒN. Tệp .proto ở proto/ là bản chép từ ViGov;
// mã ở internal/gen sinh từ nó. Một lần "sửa cho gọn" thân bản chép là mã
// client lệch khỏi máy chủ mà không test nào khác thấy — cho tới lượt đăng
// nhập thật đầu tiên.
//
// SHA dưới đây là của thân tệp nguồn ở commit ViGov ghi trong đầu tệp. Đổi nó
// chỉ khi chép lại NGUYÊN tệp nguồn ở một commit mới (xem đầu tệp .proto).
const (
	tepBanChep   = "../../proto/vigov/identity/v1/citizen_session_bridge.proto"
	dauMucHetDau = "// ==== HẾT ĐẦU TỆP ===="
	shaThanNguon = "61f4462db8e9e4594b905883c07e2a51d93f765f07b2841416c9ba84a6afff43"
)

func TestBanChepProto_ThanKhopNguon(t *testing.T) {
	b, err := os.ReadFile(tepBanChep)
	if err != nil {
		t.Fatalf("không đọc được bản chép: %s", err)
	}
	// Bỏ \r: một máy Windows với core.autocrlf=true lấy ra CRLF, và lệch dòng
	// kết thúc không phải là lệch hợp đồng.
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))

	dau, than, ok := bytes.Cut(b, []byte(dauMucHetDau+"\n"))
	if !ok {
		t.Fatalf("bản chép thiếu dòng %q — không tách được đầu tệp khỏi thân", dauMucHetDau)
	}
	if !bytes.Contains(dau, []byte(shaThanNguon)) {
		t.Error("đầu tệp .proto không ghi cùng SHA với phép kiểm này — hai nơi phải nói một con số")
	}
	tong := sha256.Sum256(than)
	if got := hex.EncodeToString(tong[:]); got != shaThanNguon {
		t.Errorf("thân bản chép đã lệch nguồn: sha256 = %s, mong %s. Không sửa tay — chép lại nguyên tệp nguồn rồi `make proto`", got, shaThanNguon)
	}
}
