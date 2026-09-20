package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// ĐỌC TRƯỚC KHI TIN NHỮNG PHÉP KIỂM NÀY:
//
// Ở đây không có một byte nào ra mạng. Các ca dưới đây chỉ chứng minh phần ĐỌC
// ĐẦU VÀO và phần PHÂN LOẠI/IN KẾT QUẢ hành xử như mã này giả định — đúng thứ
// tự hỏi, không in lại token, không in số điện thoại, và câu kết luận trỏ đúng
// mục nợ.
//
// Chúng KHÔNG chứng minh giả định về giao thức Zalo đúng. Chừng nào lệnh này
// chưa chạy THẬT một lần với token thật từ một chiếc điện thoại thật, hình dạng
// wire trong internal/zalo/wire.go vẫn ở mức chứng cứ TRUNG BÌNH và NỢ #1-3
// trong README vẫn còn nguyên. Một bộ test xanh ở đây không gạch được mục nợ
// nào — đó chính là lý do lệnh này tồn tại.

const (
	accessGiaLap = "access-token-gia-lap"
	phoneGiaLap  = "phone-token-gia-lap"
	soGiaLap     = "84900000000" // số giả đã thống nhất: 0900000000
)

func TestDocToken_HaiDongVaKhongInLaiToken(t *testing.T) {
	var nhac bytes.Buffer
	a, p, err := docToken(strings.NewReader("  "+accessGiaLap+"  \n"+phoneGiaLap+"\n"), &nhac)
	if err != nil {
		t.Fatalf("mong không lỗi, nhận: %s", err)
	}
	if a != accessGiaLap || p != phoneGiaLap {
		t.Errorf("token đọc sai: %q / %q", a, p)
	}
	// Token là thông tin xác thực của một người dùng thật: in lại là để nó nằm
	// trong scrollback và trong bản chụp màn hình gửi qua chat.
	for _, cam := range []string{accessGiaLap, phoneGiaLap} {
		if strings.Contains(nhac.String(), cam) {
			t.Errorf("lời nhắc in lại token %q", cam)
		}
	}
}

func TestDocToken_DauVaoSaiThiDungTruocKhiGoi(t *testing.T) {
	cases := map[string]string{
		"không có gì":             "",
		"chỉ một dòng":            accessGiaLap + "\n",
		"accessToken rỗng":        "\n" + phoneGiaLap + "\n",
		"phoneToken rỗng":         accessGiaLap + "\n\n",
		"hai token cùng một dòng": accessGiaLap + " " + phoneGiaLap + "\n\n",
	}
	for ten, vao := range cases {
		t.Run(ten, func(t *testing.T) {
			var nhac bytes.Buffer
			_, _, err := docToken(strings.NewReader(vao), &nhac)
			if err == nil {
				t.Fatal("mong dừng lại kèm lỗi, trước khi tiêu mất một cặp token sống 2 phút")
			}
		})
	}
}

func TestInKetQua_ThanhCong_KhongInSoDienThoai(t *testing.T) {
	var ra bytes.Buffer
	inKetQua(&ra, "1234567890", zalo.KetQuaChanDoan{
		HTTPStatus:  200,
		ThoiGian:    412 * time.Millisecond,
		CoThanJSON:  true,
		ZaloMessage: "Success",
		CoSo:        true,
		DangTho:     zalo.HinhDangSo{DoDai: 11, HaiKyTuDau: "84"},
		DangChuan:   zalo.HinhDangSo{DoDai: 11, HaiKyTuDau: "84"},
	}, nil)

	out := ra.String()
	// Khối này được thiết kế để dán vào phiếu: không số điện thoại, chỉ hình dạng.
	if strings.Contains(out, soGiaLap) || strings.Contains(out, "900000000") {
		t.Fatalf("đầu ra mang số điện thoại:\n%s", out)
	}
	for _, can := range []string{"200", "412ms", "LẤY ĐƯỢC", "11 ký tự", `"84"`, "1234567890", "201"} {
		if !strings.Contains(out, can) {
			t.Errorf("đầu ra thiếu %q — người chạy cần nó để kết luận:\n%s", can, out)
		}
	}
}

func TestKetLuan_TroDungMucNo(t *testing.T) {
	cases := []struct {
		ten string
		cd  zalo.KetQuaChanDoan
		loi error
		can string
		cam string
		// canToken: câu phải nói về số phận của cặp token sau lần chạy này.
		// Không với tới Zalo thì token nhiều khả năng CÒN DÙNG ĐƯỢC; đã gọi tới
		// nơi thì phải coi như đã tiêu.
		canToken string
	}{
		{
			"không với tới được thì KHÔNG kết luận gì về giao thức",
			zalo.KetQuaChanDoan{HTTPStatus: 0},
			zalo.ErrKhongVoiToiZalo,
			"NỢ #1 vẫn còn nguyên",
			"NỢ #2 đóng được",
			"CÒN DÙNG ĐƯỢC",
		},
		{
			"thành công mà không cần proof -> đóng được NỢ #2",
			zalo.KetQuaChanDoan{HTTPStatus: 200, CoThanJSON: true, CoSo: true,
				DangTho:   zalo.HinhDangSo{DoDai: 11, HaiKyTuDau: "84"},
				DangChuan: zalo.HinhDangSo{DoDai: 11, HaiKyTuDau: "84"}},
			nil,
			"NỢ #2 đóng được",
			"",
			"DÙNG MỘT LẦN",
		},
		{
			"Zalo trả error != 0 -> chỉ thẳng vào NỢ #3, không vội kết luận token hỏng",
			zalo.KetQuaChanDoan{HTTPStatus: 200, CoThanJSON: true, ZaloError: -201},
			zalo.ErrTokenKhongHopLe,
			"NỢ #3",
			"NỢ #1 đóng được",
			"DÙNG MỘT LẦN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.ten, func(t *testing.T) {
			van := strings.Join(ketLuan(tc.cd, tc.loi), "\n")
			if !strings.Contains(van, tc.can) {
				t.Errorf("kết luận thiếu %q:\n%s", tc.can, van)
			}
			if tc.cam != "" && strings.Contains(van, tc.cam) {
				t.Errorf("kết luận nói %q trong khi lần chạy này không chứng minh được điều đó:\n%s", tc.cam, van)
			}
			// Số phận cặp token phải luôn được nói ra: thiếu nó là hai lần chạy
			// trên cùng một cặp token bị đem ra so sánh với nhau.
			if !strings.Contains(van, tc.canToken) {
				t.Errorf("kết luận thiếu câu về cặp token (%q):\n%s", tc.canToken, van)
			}
		})
	}
}

// Zalo trả "09…" là một phát hiện phải nói ra, không được để chuẩn hoá che mất.
func TestKetLuan_DangThoKhacDangChuanThiNoiRa(t *testing.T) {
	van := strings.Join(ketLuan(zalo.KetQuaChanDoan{
		HTTPStatus: 200, CoThanJSON: true, CoSo: true,
		DangTho:   zalo.HinhDangSo{DoDai: 10, HaiKyTuDau: "09"},
		DangChuan: zalo.HinhDangSo{DoDai: 11, HaiKyTuDau: "84"},
	}, nil), "\n")

	if !strings.Contains(van, "ĐIỀU CHƯA RÕ #3") || !strings.Contains(van, "09") {
		t.Errorf("kết luận không nói ra dạng thô khác 84…:\n%s", van)
	}
}
