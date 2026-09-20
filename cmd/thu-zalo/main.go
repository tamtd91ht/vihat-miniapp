// Command thu-zalo — gọi THẬT graph.zalo.me một lần và in ra thứ chẩn đoán được.
//
// VÌ SAO CÓ LỆNH NÀY. Lời gọi đổi phoneToken lấy số điện thoại đã nằm trong mã
// (internal/zalo/client.go, gọi từ internal/httpapi/sessions.go) và có test
// httptest bao quanh — nhưng test ấy chỉ chứng minh mã khớp với GIẢ ĐỊNH của
// chính nó. Thứ chưa từng xảy ra là một lần chạm máy chủ Zalo thật, vì
// accessToken/phoneToken do máy người dùng sinh ra bên trong Zalo và HẾT HẠN
// SAU ~2 PHÚT: không ai ở phía máy chủ tạo được chúng. Lệnh này biến NỢ #1-3
// từ "phải dựng một buổi thử" thành một thao tác 30 giây cho người đang cầm
// điện thoại. Các bước lấy hai token: xem README, mục "Thử Zalo thật".
//
// HAI TOKEN NHẬP QUA STDIN, không qua tham số dòng lệnh: tham số nằm trong `ps`
// của mọi tiến trình trên máy và nằm lại trong lịch sử shell. Chúng là thông
// tin xác thực dùng một lần của MỘT người dùng thật.
//
// ĐẦU RA AN TOÀN ĐỂ DÁN VÀO PHIẾU: không số điện thoại (chỉ độ dài + hai ký tự
// đầu — đủ biết Zalo trả "84…" hay "09…"), không token, không secret key.
//
//	ZALO_MINIAPP_APP_ID=... ZALO_MINIAPP_SECRET_KEY=... go run ./cmd/thu-zalo
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// thoiHan bao cả lời gọi lẫn thời gian DNS/TLS. Client có timeout 8s của riêng
// nó; con số này chỉ là chốt chặn ngoài cùng để lệnh không treo trên terminal.
const thoiHan = 30 * time.Second

func main() {
	batProof := flag.Bool("proof", false, "gửi appsecret_proof (ĐIỀU CHƯA RÕ #1 trong internal/zalo/wire.go)")
	tatProof := flag.Bool("khong-proof", false, "KHÔNG gửi appsecret_proof — mặc định, giống hệt đường phục vụ hôm nay")
	flag.Parse()

	if *batProof && *tatProof {
		fmt.Fprintln(os.Stderr, "Lỗi: chọn một trong hai, --proof hoặc --khong-proof.")
		os.Exit(2)
	}

	cz, err := config.NapChiZaloTuMoiTruong()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(1)
	}

	// Nhắc nhở đi ra STDERR, khối chẩn đoán đi ra STDOUT. Nhờ thế
	// `make thu-zalo > ketqua.txt` cho ra đúng khối để đính vào phiếu, mà người
	// gõ vẫn nhìn thấy câu hỏi.
	accessToken, phoneToken, err := docToken(os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(1)
	}

	c := zalo.New("", cz.SecretKey)
	c.GuiAppSecretProof = *batProof

	ctx, dung := context.WithTimeout(context.Background(), thoiHan)
	defer dung()

	cd, loiGoi := c.ChanDoan(ctx, accessToken, phoneToken)
	inKetQua(os.Stdout, cz.AppID, cd, loiGoi)

	// Mã thoát khác 0 khi lời gọi hỏng: người chạy thấy ngay, và một lần chạy
	// trong script không bị coi là thành công.
	if loiGoi != nil {
		os.Exit(1)
	}
}

// docToken đọc hai dòng: accessToken rồi phoneToken.
//
// Tách khỏi main để test được mà không cần terminal — và để phần "không bao giờ
// in lại token" có chỗ kiểm.
func docToken(in io.Reader, nhac io.Writer) (string, string, error) {
	fmt.Fprintln(nhac, "Thử gọi THẬT graph.zalo.me. Hai token sống ~2 phút — dán nhanh.")
	fmt.Fprintln(nhac, "Không có gì được ghi vào CSDL và không có gì được in lại.")
	fmt.Fprintln(nhac, "")

	doc := bufio.NewScanner(in)
	doc.Buffer(make([]byte, 0, 4<<10), 1<<20) // token có thể rất dài

	hoi := func(cau string) (string, error) {
		fmt.Fprint(nhac, cau)
		if !doc.Scan() {
			if err := doc.Err(); err != nil {
				return "", fmt.Errorf("đọc đầu vào: %w", err)
			}
			return "", errors.New("đầu vào kết thúc giữa chừng — chưa gọi Zalo lần nào")
		}
		return strings.TrimSpace(doc.Text()), nil
	}

	accessToken, err := hoi("accessToken (getAccessToken): ")
	if err != nil {
		return "", "", err
	}
	if accessToken == "" {
		return "", "", errors.New("accessToken rỗng — chưa gọi Zalo lần nào")
	}
	// Dán cả hai token vào một dòng là lỗi dễ mắc nhất khi đang vội, và nó tạo
	// ra một lỗi 401 của Zalo trông y hệt "token hết hạn" — tức một kết luận sai
	// về NỢ #2/#3. Bắt tại đây thay vì để Zalo trả lời.
	if strings.ContainsAny(accessToken, " \t") {
		return "", "", errors.New("accessToken chứa khoảng trắng — hình như hai token bị dán vào cùng một dòng; mỗi token một dòng")
	}

	phoneToken, err := hoi("phoneToken (token của getPhoneNumber): ")
	if err != nil {
		return "", "", err
	}
	if phoneToken == "" {
		return "", "", errors.New("phoneToken rỗng — chưa gọi Zalo lần nào")
	}
	if strings.ContainsAny(phoneToken, " \t") {
		return "", "", errors.New("phoneToken chứa khoảng trắng — dán lại cho đúng một dòng")
	}

	fmt.Fprintln(nhac, "")
	return accessToken, phoneToken, nil
}

// inKetQua in khối chẩn đoán. Mọi thứ trong khối này an toàn để dán vào phiếu.
func inKetQua(out io.Writer, appID string, cd zalo.KetQuaChanDoan, loi error) {
	p := func(nhan, giaTri string) { fmt.Fprintf(out, "  %-22s %s\n", nhan+":", giaTri) }

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "=== THỬ ZALO THẬT ===")
	p("thời điểm", time.Now().UTC().Format(time.RFC3339))
	p("Mini App (app id)", appID)
	p("endpoint", zalo.BaseURLMacDinh+zalo.DuongDanLayThongTin)
	p("appsecret_proof", bat(cd.GuiProof))

	if cd.HTTPStatus == 0 {
		p("HTTP", "KHÔNG VỚI TỚI (không có phản hồi)")
	} else {
		p("HTTP", fmt.Sprintf("%d", cd.HTTPStatus))
	}
	p("thời gian phản hồi", cd.ThoiGian.Round(time.Millisecond).String())
	p("thân JSON đọc được", co(cd.CoThanJSON))
	p("error (Zalo)", fmt.Sprintf("%d", cd.ZaloError))
	p("message (Zalo)", strconvQ(cd.ZaloMessage))

	if cd.CoSo {
		// KHÔNG in số. Độ dài + hai ký tự đầu là đủ để biết đúng dạng 84…
		p("số điện thoại", fmt.Sprintf("LẤY ĐƯỢC — thô: %d ký tự, bắt đầu %q; sau chuẩn hoá: %d ký tự, bắt đầu %q",
			cd.DangTho.DoDai, cd.DangTho.HaiKyTuDau, cd.DangChuan.DoDai, cd.DangChuan.HaiKyTuDau))
	} else if cd.DangTho.DoDai > 0 {
		p("số điện thoại", fmt.Sprintf("KHÔNG DÙNG ĐƯỢC — thô: %d ký tự, bắt đầu %q (ChuanHoaSo từ chối)",
			cd.DangTho.DoDai, cd.DangTho.HaiKyTuDau))
	} else {
		p("số điện thoại", "KHÔNG LẤY ĐƯỢC")
	}

	if loi == nil {
		p("người dùng sẽ thấy", "201 — đăng nhập thành công")
	} else {
		p("người dùng sẽ thấy", maHTTPSePhucVu(loi))
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Kết luận:")
	for _, dong := range ketLuan(cd, loi) {
		fmt.Fprintln(out, "  - "+dong)
	}
	fmt.Fprintln(out, "")
}

// maHTTPSePhucVu dịch lỗi đã phân loại sang đúng mã mà tuyến /api/v1/sessions
// sẽ trả cho người dùng thật — đó mới là thứ người chạy cần đối chiếu.
func maHTTPSePhucVu(loi error) string {
	switch {
	case errors.Is(loi, zalo.ErrTokenKhongHopLe):
		return `401 — "Phiên Zalo đã hết hạn. Vui lòng đóng và mở lại ứng dụng…"`
	case errors.Is(loi, zalo.ErrKhongVoiToiZalo):
		return `502 — "Hiện chưa kết nối được tới Zalo. Vui lòng thử lại sau ít phút."`
	default:
		return "500 — lỗi không phân loại được (đây là một phát hiện, chép lại)"
	}
}

// ketLuan nói thẳng lần chạy này đóng được mục nợ nào và KHÔNG đóng được mục nào.
//
// Phần quan trọng nhất của lệnh: một khối số liệu không có câu kết luận thì mỗi
// người đọc ra một nghĩa, và mục nợ vẫn nằm đó.
func ketLuan(cd zalo.KetQuaChanDoan, loi error) []string {
	var l []string

	switch {
	case cd.HTTPStatus == 0:
		l = append(l,
			"KHÔNG với tới được graph.zalo.me — kiểm mạng/DNS/egress của chính máy đang chạy.",
			"Lần chạy này KHÔNG kết luận được gì về giao thức. NỢ #1 vẫn còn nguyên.",
			"Chưa chạm tới Zalo nên cặp token nhiều khả năng CÒN DÙNG ĐƯỢC: sửa đường mạng rồi chạy lại ngay, vẫn trong ~2 phút kể từ lúc lấy token.")
		return l

	case !cd.CoThanJSON:
		l = append(l,
			"Zalo trả thân KHÔNG phải JSON như internal/zalo/wire.go giả định — ĐÂY LÀ MỘT PHÁT HIỆN.",
			"Chép lại mã HTTP ở trên vào NỢ #1 trước khi sửa bất cứ thứ gì.")

	case loi == nil && cd.CoSo:
		l = append(l, "Hình dạng wire trong internal/zalo/wire.go ĐÚNG với chế độ vừa chạy → NỢ #1 đóng được cho chế độ này.")
		if cd.GuiProof {
			l = append(l, "Zalo CHẤP NHẬN appsecret_proof ký theo TinhAppSecretProof. 'Chấp nhận' chưa phải 'bắt buộc' — muốn biết có bắt buộc không thì chạy lại --khong-proof với CẶP TOKEN MỚI.")
		} else {
			l = append(l, "Zalo KHÔNG ĐÒI appsecret_proof cho luồng này (lời gọi không kèm proof đã thành công) → NỢ #2 đóng được, giữ nguyên mặc định TẮT.")
		}
		if cd.DangTho.HaiKyTuDau != cd.DangChuan.HaiKyTuDau || cd.DangTho.DoDai != cd.DangChuan.DoDai {
			l = append(l, fmt.Sprintf("Zalo trả dạng %q (%d ký tự) chứ không phải 84…; ChuanHoaSo đã đổi đúng → ghi vào ĐIỀU CHƯA RÕ #3 của wire.go.",
				cd.DangTho.HaiKyTuDau, cd.DangTho.DoDai))
		} else {
			l = append(l, "Số trả về đúng dạng 84… như wire.go mô tả → ĐIỀU CHƯA RÕ #3 đóng được.")
		}

	case cd.ZaloError != 0:
		l = append(l, fmt.Sprintf("Zalo trả error=%d. Đường phục vụ hôm nay xếp MỌI error != 0 thành 401 'phiên hết hạn'.", cd.ZaloError))
		l = append(l, "Nếu mã này nghĩa là secret key sai / app chưa bật quyền thì đó là lỗi PHÍA TA, phải tách thành 502 + cảnh báo vận hành — NỢ #3. Chép mã và câu message ở trên vào mục nợ ấy.")
		if !cd.GuiProof {
			l = append(l, "Chưa gửi appsecret_proof. Trước khi kết luận 'token hỏng', chạy lại --proof với CẶP TOKEN MỚI: có thể Zalo đang đòi proof (NỢ #2).")
		} else {
			l = append(l, "Đã gửi appsecret_proof và vẫn lỗi: hoặc chuỗi được ký không phải access_token (TinhAppSecretProof đang đoán), hoặc lỗi nằm chỗ khác.")
		}

	default:
		l = append(l, "Zalo từ chối ở tầng HTTP, chưa tới được thân JSON — xem mã HTTP ở trên.")
		l = append(l, "Đường phục vụ xếp ca này thành 502 (lỗi phía ta/hạ tầng), cố ý KHÔNG bảo người dùng đăng nhập lại.")
	}

	l = append(l,
		"phoneToken rất có thể DÙNG MỘT LẦN: muốn thử chế độ còn lại thì phải lấy CẶP TOKEN MỚI, chạy lại từ đầu. Hai lần chạy trên cùng một cặp token không so sánh được với nhau.",
		"Một lần chạy xanh ở đây KHÔNG thay được việc sửa mức chứng cứ trong wire.go và mục NỢ trong README — chép kết quả vào đó.")
	return l
}

func bat(b bool) string {
	if b {
		return "BẬT"
	}
	return "TẮT (mặc định, giống đường phục vụ)"
}

func co(b bool) string {
	if b {
		return "có"
	}
	return "KHÔNG"
}

// strconvQ tránh kéo cả strconv cho một lời gọi: message đã được internal/zalo
// cắt ngắn và gạch số điện thoại trước khi tới đây.
func strconvQ(s string) string { return `"` + s + `"` }
