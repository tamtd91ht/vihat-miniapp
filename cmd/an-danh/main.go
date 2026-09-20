// Command an-danh — thực hiện một yêu cầu xoá dữ liệu theo Nghị định 13.
//
// CHẠY TAY, CÓ NGƯỜI KÝ. Yêu cầu xoá tới qua hotline hoặc email trên màn Liên
// hệ của app; người tiếp nhận chạy lệnh này và tên họ được ghi vào
// nhat_ky_an_danh.
//
// CỐ Ý KHÔNG CÓ TUYẾN API: một tuyến nhận số điện thoại rồi xoá dữ liệu ứng với
// số ấy là một tuyến xoá dữ liệu NGƯỜI KHÁC — ai cũng gửi được số của người ta.
//
// Số điện thoại NHẬP QUA STDIN, không qua tham số dòng lệnh: tham số nằm trong
// `ps` của mọi tiến trình trên máy và nằm lại trong lịch sử shell. Lệnh này
// không in số điện thoại ra bất cứ đâu, kể cả khi báo lỗi.
//
//	DATABASE_DSN=... go run ./cmd/an-danh
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/zalo"
)

// khoAnDanh là phần kho mà lệnh này cần — hẹp, để test được không cần CSDL.
type khoAnDanh interface {
	AnDanhHoa(ctx context.Context, yc store.YeuCauAnDanh) (store.KetQuaAnDanh, error)
}

const xacNhanCanGo = "AN DANH"

func main() {
	ctx, dung := context.WithTimeout(context.Background(), 5*time.Minute)
	defer dung()

	dsn, err := config.NapChiDSNTuMoiTruong()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(1)
	}

	kho, err := store.Mo(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(1)
	}
	defer kho.Dong()

	if err := chay(ctx, os.Stdin, os.Stdout, kho); err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(1)
	}
}

// chay hỏi bốn thứ, xác nhận, rồi gọi kho. Tách khỏi main để test được.
func chay(ctx context.Context, in io.Reader, out io.Writer, kho khoAnDanh) error {
	doc := bufio.NewScanner(in)

	hoi := func(cau string) (string, error) {
		fmt.Fprint(out, cau)
		if !doc.Scan() {
			if err := doc.Err(); err != nil {
				return "", fmt.Errorf("đọc đầu vào: %w", err)
			}
			return "", errors.New("đầu vào kết thúc giữa chừng — không có gì được thay đổi")
		}
		return strings.TrimSpace(doc.Text()), nil
	}

	fmt.Fprintln(out, "Ẩn danh hoá theo yêu cầu xoá dữ liệu (Nghị định 13/2023).")
	fmt.Fprintln(out, "Số điện thoại chỉ dùng để tra, không được in lại ở bất kỳ đâu.")
	fmt.Fprintln(out, "")

	thoSo, err := hoi("Số điện thoại người yêu cầu: ")
	if err != nil {
		return err
	}
	// Chuẩn hoá bằng đúng hàm mà đường đăng nhập dùng — hai cách chuẩn hoá khác
	// nhau nghĩa là tra không ra người cần tìm.
	so, err := zalo.ChuanHoaSo(thoSo)
	if err != nil {
		// Thông điệp KHÔNG nhắc lại thứ vừa nhập.
		return errors.New("số điện thoại không đúng định dạng — không có gì được thay đổi")
	}

	nguon, err := hoi("Yêu cầu đến từ đâu (hotline/email): ")
	if err != nil {
		return err
	}
	nguon = strings.ToLower(nguon)
	if nguon != "hotline" && nguon != "email" {
		return errors.New(`nguồn yêu cầu phải là "hotline" hoặc "email" — không có gì được thay đổi`)
	}

	nguoi, err := hoi("Người tiếp nhận (tên hoặc mã nhân sự): ")
	if err != nil {
		return err
	}
	if nguoi == "" {
		return errors.New("phải ghi người tiếp nhận — không có gì được thay đổi")
	}

	ghiChu, err := hoi("Số phiếu / mã cuộc gọi (bỏ trống nếu không có): ")
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Việc sắp làm, KHÔNG HOÀN TÁC ĐƯỢC:")
	fmt.Fprintln(out, "  - ghi đè số điện thoại của người dùng ứng với số vừa nhập")
	fmt.Fprintln(out, "  - thu hồi mọi phiên đăng nhập còn hiệu lực của người ấy")
	fmt.Fprintln(out, "  - ghi một dòng nhật ký ẩn danh mang tên người tiếp nhận")
	fmt.Fprintln(out, "  (nhật ký đăng nhập được GIỮ NGUYÊN — nó không chứa số điện thoại)")
	fmt.Fprintln(out, "")

	xacNhan, err := hoi("Gõ " + xacNhanCanGo + " để xác nhận: ")
	if err != nil {
		return err
	}
	if xacNhan != xacNhanCanGo {
		return errors.New("chưa xác nhận — không có gì được thay đổi")
	}

	kq, err := kho.AnDanhHoa(ctx, store.YeuCauAnDanh{
		SoDienThoai:   so,
		NguonYeuCau:   nguon,
		NguoiThucHien: nguoi,
		GhiChu:        ghiChu,
	})
	if err != nil {
		return err
	}

	// Đầu ra chỉ có MÃ ĐỊNH DANH. Người vận hành dán mã này vào phiếu xử lý.
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Xong.")
	fmt.Fprintln(out, "  người dùng     : "+kq.NguoiDungID)
	fmt.Fprintf(out, "  phiên thu hồi  : %d\n", kq.SoPhienThuHoi)
	fmt.Fprintln(out, "  thời điểm      : "+kq.ThoiDiem.UTC().Format(time.RFC3339))
	return nil
}
