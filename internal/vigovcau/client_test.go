package vigovcau

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/vihat/vihat-miniapp/internal/gen/vigov/identity/v1"
	"github.com/vihat/vihat-miniapp/internal/secret"
)

// Khoá giả 32 byte — không phải khoá của môi trường nào.
const khoaGia = "khoa-cau-gia-lap-trong-test-0032"

// mayChuGia là cổng cầu giả, chạy THẬT qua gRPC (bufconn): phép kiểm metadata
// chỉ có nghĩa khi khoá đi qua đúng đường dây, không phải qua một giao diện giả.
type mayChuGia struct {
	identityv1.UnimplementedCitizenSessionBridgeServiceServer

	mu    sync.Mutex
	md    []metadata.MD
	nhan  []*identityv1.OpenCitizenSessionRequest
	loi   []error // lỗi trả theo thứ tự từng lượt; hết danh sách thì thành công
	traVe *identityv1.OpenCitizenSessionResponse
}

func (m *mayChuGia) OpenCitizenSession(ctx context.Context, req *identityv1.OpenCitizenSessionRequest) (*identityv1.OpenCitizenSessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	md, _ := metadata.FromIncomingContext(ctx)
	m.md = append(m.md, md)
	m.nhan = append(m.nhan, req)
	if n := len(m.nhan); n <= len(m.loi) && m.loi[n-1] != nil {
		return nil, m.loi[n-1]
	}
	if m.traVe != nil {
		return m.traVe, nil
	}
	return &identityv1.OpenCitizenSessionResponse{}, nil
}

func (m *mayChuGia) soLuot() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.nhan)
}

func dungCau(t *testing.T, m *mayChuGia) *Client {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	identityv1.RegisterCitizenSessionBridgeServiceServer(srv, m)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return moiTuGoi(identityv1.NewCitizenSessionBridgeServiceClient(cc), secret.Secret(khoaGia))
}

func yeuCauMau() YeuCau {
	return YeuCau{
		AppID:       "1234567890",
		MaTaiKhoan:  "ma-tai-khoan-gia-lap",
		IPKhach:     "203.0.113.7",
		ThietBi:     "Zalo/Test",
		TenMienXa:   "xa-a.vigov.vn",
		DaXacNhanXa: true,
	}
}

// Khoá đi ở ĐÚNG MỘT chỗ — metadata x-vigov-bridge-key, một giá trị — và
// không ở metadata khoá giữa các service ViGov.
func TestMoPhien_KhoaCauDiDungMetadata(t *testing.T) {
	m := &mayChuGia{}
	c := dungCau(t, m)

	if _, err := c.MoPhien(context.Background(), yeuCauMau()); err != nil {
		t.Fatalf("mong thành công, nhận: %s", err)
	}
	md := m.md[0]
	if got := md.Get(MetadataKhoaCau); len(got) != 1 || got[0] != khoaGia {
		t.Fatalf("metadata %s = %q, mong đúng một giá trị là khoá cầu", MetadataKhoaCau, got)
	}
	if got := md.Get("x-vigov-caller-key"); len(got) != 0 {
		t.Error("gửi metadata khoá giữa các service ViGov — kho này không bao giờ cầm khoá ấy")
	}
	for ten, vals := range md {
		if ten == MetadataKhoaCau {
			continue
		}
		for _, v := range vals {
			if strings.Contains(v, khoaGia) {
				t.Errorf("khoá cầu lọt vào metadata %q", ten)
			}
		}
	}
}

func TestMoPhien_ChuyenTiepNguyenVanVaKhongDatTenantHint(t *testing.T) {
	m := &mayChuGia{}
	c := dungCau(t, m)

	yc := yeuCauMau()
	// Nguyên văn — kho này KHÔNG diễn giải tên miền: không trim, không hạ chữ.
	// Máy chủ kiểm khuôn và từ chối; sửa hộ ở đây là để hai đầu hiểu khác nhau.
	yc.TenMienXa = " Xa-A.vigov.vn "
	if _, err := c.MoPhien(context.Background(), yc); err != nil {
		t.Fatal(err)
	}
	req := m.nhan[0]
	if req.GetCommuneHostHint() != " Xa-A.vigov.vn " {
		t.Errorf("commune_host_hint = %q, mong nguyên văn client gửi", req.GetCommuneHostHint())
	}
	if !req.GetCommuneConfirmed() {
		t.Error("commune_confirmed không được chuyển tiếp")
	}
	if req.GetTenantHint() != "" {
		t.Error("tenant_hint đã ngừng dùng — khác rỗng là INVALID_ARGUMENT")
	}
	if req.GetAppId() != yc.AppID || req.GetZaloUserId() != yc.MaTaiKhoan ||
		req.GetClientIp() != yc.IPKhach || req.GetDevice() != yc.ThietBi {
		t.Error("một trường yêu cầu không được chuyển tiếp đúng")
	}
	if req.GetVerifiedPhone() != "" {
		t.Error("không có số trong yêu cầu mà verified_phone lại có giá trị")
	}
}

func TestMoPhien_DocPhanHoi(t *testing.T) {
	het := time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC)
	m := &mayChuGia{traVe: &identityv1.OpenCitizenSessionResponse{
		SessionToken: "tok-gia", SessionId: "sid-gia", ExpiresAt: timestamppb.New(het),
		TenantId: "01J0000000000000000000000A", TenantDisplayName: "Xã A", PhoneVerified: true,
	}}
	kq, err := dungCau(t, m).MoPhien(context.Background(), yeuCauMau())
	if err != nil {
		t.Fatal(err)
	}
	if kq.Token != "tok-gia" || kq.PhienID != "sid-gia" || !kq.HetHan.Equal(het) ||
		kq.TenXa != "Xã A" || !kq.DaXacThucSo {
		t.Errorf("kết quả = %+v", kq)
	}

	// Phiên không xã: hợp đồng không có token, không có hạn.
	m2 := &mayChuGia{traVe: &identityv1.OpenCitizenSessionResponse{SessionId: "sid-2"}}
	kq, err = dungCau(t, m2).MoPhien(context.Background(), yeuCauMau())
	if err != nil || kq.Token != "" || !kq.HetHan.IsZero() || kq.TenXa != "" {
		t.Errorf("phiên không xã = %+v, err=%v", kq, err)
	}
}

func TestMoPhien_MaLoiTheoBangTrangThai(t *testing.T) {
	for ma, mong := range map[codes.Code]error{
		codes.InvalidArgument:    ErrYeuCauSai,
		codes.FailedPrecondition: ErrChuaSanSang,
		codes.Unauthenticated:    ErrSaiKhoaCau,
		codes.Unavailable:        ErrTamNgung,
		codes.Internal:           ErrTamNgung,
		codes.DeadlineExceeded:   ErrTamNgung,
		codes.PermissionDenied:   ErrTamNgung,
	} {
		t.Run(ma.String(), func(t *testing.T) {
			// Thông điệp máy chủ mang một chuỗi dễ nhận: nó KHÔNG được đi vào lỗi.
			m := &mayChuGia{loi: []error{status.Error(ma, "thong-diep-may-chu")}}
			_, err := dungCau(t, m).MoPhien(context.Background(), yeuCauMau())
			if !errors.Is(err, mong) {
				t.Fatalf("mã %s -> %v, mong %v", ma, err, mong)
			}
			if strings.Contains(err.Error(), "thong-diep-may-chu") {
				t.Error("thông điệp của máy chủ lọt vào lỗi")
			}
			// Chỉ ABORTED được thử lại: mỗi lượt thành công là một phiên mới.
			if n := m.soLuot(); n != 1 {
				t.Errorf("mã %s gọi %d lượt, mong 1 — không được thử lại", ma, n)
			}
		})
	}
}

func TestMoPhien_AbortedThuLaiDungMotLan(t *testing.T) {
	t.Run("lần hai thành công", func(t *testing.T) {
		m := &mayChuGia{
			loi:   []error{status.Error(codes.Aborted, "x")},
			traVe: &identityv1.OpenCitizenSessionResponse{SessionToken: "tok-2", SessionId: "sid"},
		}
		kq, err := dungCau(t, m).MoPhien(context.Background(), yeuCauMau())
		if err != nil || kq.Token != "tok-2" {
			t.Fatalf("mong thành công ở lượt hai, nhận kq=%+v err=%v", kq, err)
		}
		if m.soLuot() != 2 {
			t.Errorf("gọi %d lượt, mong 2", m.soLuot())
		}
	})
	t.Run("hai lần ABORTED thì dừng", func(t *testing.T) {
		m := &mayChuGia{loi: []error{status.Error(codes.Aborted, "x"), status.Error(codes.Aborted, "x")}}
		_, err := dungCau(t, m).MoPhien(context.Background(), yeuCauMau())
		if !errors.Is(err, ErrTamNgung) {
			t.Fatalf("mong ErrTamNgung, nhận %v", err)
		}
		if m.soLuot() != 2 {
			t.Errorf("gọi %d lượt, mong đúng 2 — thử lại MỘT lần, không vòng lặp", m.soLuot())
		}
	})
}

func TestMo_TuChoiThieuCauHinh(t *testing.T) {
	if _, err := Mo(nil, secret.Secret(khoaGia)); err == nil {
		t.Error("không địa chỉ mà vẫn dựng được client")
	}
	if _, err := Mo([]string{"identity-cau:9091"}, ""); err == nil {
		t.Error("không khoá mà vẫn dựng được client")
	}
	c, err := Mo([]string{"identity-cau-1:9091", "identity-cau-2:9091"}, secret.Secret(khoaGia))
	if err != nil {
		t.Fatalf("dựng client với hai địa chỉ: %s", err)
	}
	_ = c.Dong()
}
