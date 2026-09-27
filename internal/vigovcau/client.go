// Package vigovcau gọi CẦU PHIÊN CÔNG DÂN của ViGov:
// CitizenSessionBridgeService.OpenCitizenSession trên cổng cầu của
// service-identity.
//
// Kho này là bên gọi DUY NHẤT của cầu ấy. Nó khẳng định đúng ba điều đã tự xác
// minh — "token này thuộc app X, tài khoản Zalo Y, (số Z)" — rồi chuyển NGUYÊN
// token phiên ViGov cho Mini App. Nó KHÔNG BIẾT XÃ: tên miền xã từ client chỉ
// được chuyển tiếp, không đọc, không sửa, không kiểm (máy chủ ViGov kiểm).
// Quyết định và lý do: ADR 0045 + 0047 ở kho ViGov.
//
// ⚠ KHÔNG BAO GIỜ LOG MỘT MESSAGE CỦA HỢP ĐỒNG NÀY. String() sinh ra in MỌI
// trường: yêu cầu mang mã tài khoản Zalo và có thể cả số điện thoại (Nghị định
// 13/2023), phản hồi mang một bearer token đang dùng được. Log trường đã chọn,
// không bao giờ log message — và lỗi của gói này chỉ mang MÃ gRPC.
package vigovcau

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	identityv1 "github.com/vihat/vihat-miniapp/internal/gen/vigov/identity/v1"
	"github.com/vihat/vihat-miniapp/internal/secret"
)

// MetadataKhoaCau — tên metadata mang khoá cầu. PHẦN CỦA HỢP ĐỒNG: trùng
// core/grpcx.MetadataBridgeKey phía ViGov. Cố ý KHÁC tên metadata khoá giữa các
// service ViGov — kho này không cầm và không bao giờ gửi khoá ấy.
const MetadataKhoaCau = "x-vigov-bridge-key"

// thoiHanGoi — trần cho MỘT lượt gọi cầu (gồm cả lượt thử lại khi ABORTED).
// Phía máy chủ còn gọi platform hai lần rồi mở một giao dịch; vài giây là rộng.
// Không đặt thì một cổng cầu treo giữ lượt đăng nhập tới timeout ghi của HTTP.
const thoiHanGoi = 8 * time.Second

// Các lớp lỗi tầng HTTP phải phân biệt. KHÔNG mang thông điệp của máy chủ: câu
// ấy không do ta kiểm soát, và hai nhánh FAILED_PRECONDITION của máy chủ phải
// thành MỘT câu trả lời (hợp đồng: không để bên gọi dò xã nào tồn tại).
var (
	// ErrYeuCauSai — INVALID_ARGUMENT: lỗi nối dây phía bên gọi (tên miền sai
	// khuôn, xác nhận mà thiếu tên miền, trường đã ngừng dùng...).
	ErrYeuCauSai = errors.New("vigovcau: máy chủ từ chối yêu cầu (INVALID_ARGUMENT)")
	// ErrChuaSanSang — FAILED_PRECONDITION: app chưa gắn / xã không hoạt động /
	// tên miền không thuộc xã nào đang hoạt động. Một câu, không phân biệt.
	ErrChuaSanSang = errors.New("vigovcau: app hoặc xã chưa sẵn sàng (FAILED_PRECONDITION)")
	// ErrSaiKhoaCau — UNAUTHENTICATED: chỉ interceptor khoá cầu sinh ra, tức
	// TRIỂN KHAI sai (khoá lệch, trỏ nhầm cổng). Phải cảnh báo vận hành.
	ErrSaiKhoaCau = errors.New("vigovcau: cổng cầu từ chối khoá cầu (UNAUTHENTICATED)")
	// ErrTamNgung — UNAVAILABLE, ABORTED hai lần liền, DEADLINE, INTERNAL... :
	// hợp đồng nói "không có gì được phát", bên gọi trả 503.
	ErrTamNgung = errors.New("vigovcau: cầu phiên tạm không phục vụ được")
)

// YeuCau — những gì kho này khẳng định với ViGov. Hình dạng CỦA TA, không phải
// message sinh ra: tầng HTTP không chạm kiểu sinh ra, nên không có đường nào vô
// tình log nó.
type YeuCau struct {
	AppID       string // App ID có secret vừa xác minh token. Không phải dữ liệu cá nhân
	MaTaiKhoan  string // mã tài khoản Zalo — DỮ LIỆU CÁ NHÂN
	SoDaXacThuc string // "84…" hoặc "" khi lượt này không có phoneToken — DỮ LIỆU CÁ NHÂN
	IPKhach     string // "" khi không phân tích được
	ThietBi     string // user agent, máy chủ tự cắt
	TenMienXa   string // NGUYÊN VĂN client gửi; kho này không diễn giải
	DaXacNhanXa bool
}

// KetQua — phản hồi của cầu. Token là CHỨNG CỨ: chuyển cho client, không lưu,
// không log.
type KetQua struct {
	Token       string    // "" khi phiên không xã (hợp đồng: không xã thì không token)
	PhienID     string    // không cấp quyền gì; được log
	HetHan      time.Time // zero khi không có token
	TenXa       string    // "" đúng khi không xã
	DaXacThucSo bool
}

// Client gọi cầu phiên. An toàn cho nhiều goroutine.
type Client struct {
	goi  identityv1.CitizenSessionBridgeServiceClient
	cc   *grpc.ClientConn // nil khi dựng từ moiTuGoi (test)
	khoa secret.Secret
}

// Mo dựng client tới danh sách địa chỉ cổng cầu (đã kiểm hình dạng ở config).
//
// DẠNG CỤM: mọi địa chỉ vào một resolver thủ công, cân bằng round_robin — không
// bao giờ chỉ giữ địa chỉ đầu. grpc.NewClient không quay số ngay; lượt đăng
// nhập đầu tiên mới nối, nên cổng cầu chưa lên không chặn tiến trình khởi động.
//
// KHÔNG TLS, có chủ đích và có điều kiện: hai bên CÙNG CỤM k8s, NetworkPolicy
// phía ViGov chỉ cho pod này tới cổng cầu (ADR 0045, UNKNOWN #5 đã trả lời).
// Ngày tách cụm thì chặng này mang bearer token ở dạng rõ qua mạng ngoài — khi
// ấy phải thêm TLS TRƯỚC, không phải sau.
func Mo(diaChi []string, khoa secret.Secret) (*Client, error) {
	if len(diaChi) == 0 || khoa.Lo() == "" {
		return nil, errors.New("vigovcau: thiếu địa chỉ hoặc khoá cầu")
	}
	r := manual.NewBuilderWithScheme("vigov-cau")
	ds := make([]resolver.Address, 0, len(diaChi))
	for _, d := range diaChi {
		ds = append(ds, resolver.Address{Addr: d})
	}
	r.InitialState(resolver.State{Addresses: ds})

	cc, err := grpc.NewClient(r.Scheme()+":///cau-phien-cong-dan",
		grpc.WithResolvers(r),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
	)
	if err != nil {
		return nil, fmt.Errorf("vigovcau: dựng kết nối: %w", err)
	}
	c := moiTuGoi(identityv1.NewCitizenSessionBridgeServiceClient(cc), khoa)
	c.cc = cc
	return c, nil
}

func moiTuGoi(goi identityv1.CitizenSessionBridgeServiceClient, khoa secret.Secret) *Client {
	return &Client{goi: goi, khoa: khoa}
}

// Dong đóng kết nối.
func (c *Client) Dong() error {
	if c.cc == nil {
		return nil
	}
	return c.cc.Close()
}

// MoPhien gọi OpenCitizenSession.
//
// THỬ LẠI ĐÚNG MỘT LẦN khi ABORTED: máy chủ trả mã ấy khi xã đã nhớ của tài
// khoản vừa bị một lượt khác đổi, và "không có gì được phát" — lượt sau đi theo
// xã vừa nhớ. KHÔNG thử lại mã nào khác: mỗi lượt thành công ghi một phiên mới,
// nên thử lại sau một lỗi mơ hồ (DEADLINE) có thể để lại phiên không ai cầm
// token; hợp đồng nói bên gọi trả 503.
func (c *Client) MoPhien(ctx context.Context, yc YeuCau) (KetQua, error) {
	ctx, huy := context.WithTimeout(ctx, thoiHanGoi)
	defer huy()
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataKhoaCau, c.khoa.Lo())

	req := &identityv1.OpenCitizenSessionRequest{
		AppId:            yc.AppID,
		ZaloUserId:       yc.MaTaiKhoan,
		VerifiedPhone:    yc.SoDaXacThuc,
		ClientIp:         yc.IPKhach,
		Device:           yc.ThietBi,
		CommuneHostHint:  yc.TenMienXa,
		CommuneConfirmed: yc.DaXacNhanXa,
		// tenant_hint CỐ Ý không đặt: trường đã ngừng dùng, khác rỗng là
		// INVALID_ARGUMENT ở mọi chế độ (hợp đồng, trường 3).
	}

	ph, err := c.goi.OpenCitizenSession(ctx, req)
	if status.Code(err) == codes.Aborted {
		ph, err = c.goi.OpenCitizenSession(ctx, req)
	}
	if err != nil {
		return KetQua{}, phanLoai(err)
	}

	kq := KetQua{
		Token:       ph.GetSessionToken(),
		PhienID:     ph.GetSessionId(),
		TenXa:       ph.GetTenantDisplayName(),
		DaXacThucSo: ph.GetPhoneVerified(),
	}
	if ph.GetExpiresAt() != nil {
		kq.HetHan = ph.GetExpiresAt().AsTime()
	}
	return kq, nil
}

// phanLoai dịch mã gRPC thành lớp lỗi của gói. Chỉ MÃ đi vào lỗi — không bao
// giờ status.Message(), không bao giờ err gốc bọc %w (err gốc in cả thông điệp).
func phanLoai(err error) error {
	ma := status.Code(err)
	switch ma {
	case codes.InvalidArgument:
		return ErrYeuCauSai
	case codes.FailedPrecondition:
		return ErrChuaSanSang
	case codes.Unauthenticated:
		return ErrSaiKhoaCau
	default:
		return fmt.Errorf("%w (mã %s)", ErrTamNgung, ma)
	}
}
