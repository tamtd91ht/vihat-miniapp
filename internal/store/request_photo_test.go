package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vihat/vihat-miniapp/internal/phien"
	"github.com/vihat/vihat-miniapp/internal/photo"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
)

// Touches a real database — SKIPs without TEST_DATABASE_DSN (see kho_test.go). Needs
// migrations up to 0005. What only the database can answer: the ownership condition inside
// the SQL, the 3-photo cap, and that erasure leaves no byte behind.

func newUserWithRequest(t *testing.T, k *Kho, kind string) (string, string) {
	t.Helper()
	ctx := context.Background()
	so := soTest(t)
	kq, err := k.TaoPhienDangNhap(ctx, so, phien.Bam("tok-photo-"+so), time.Now().Add(time.Hour), ipTest())
	if err != nil {
		t.Fatalf("session: %s", err)
	}
	id, err := k.TaoYeuCau(ctx, yeucau.ThongTinTao{NguoiDungID: kq.NguoiDungID, Loai: kind})
	if err != nil {
		t.Fatalf("request: %s", err)
	}
	return kq.NguoiDungID, id
}

var fakeJPEG = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 2, 0xff, 0xda, 0, 2, 1, 2, 3, 0xff, 0xd9}

func TestRequestPhoto_OwnerCapAndIsolation(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	userA, reqA := newUserWithRequest(t, k, yeucau.LoaiTuVan)
	userB, reqB := newUserWithRequest(t, k, yeucau.LoaiTuVan)

	var ids []string
	for i := 1; i <= photo.MaxPerRequest; i++ {
		m, err := k.AddRequestPhoto(ctx, userA, reqA, photo.TypeJPEG, fakeJPEG)
		if err != nil {
			t.Fatalf("photo %d: %s", i, err)
		}
		if m.Position != i || m.SizeBytes != len(fakeJPEG) {
			t.Fatalf("photo %d meta %+v", i, m)
		}
		ids = append(ids, m.ID)
	}
	if _, err := k.AddRequestPhoto(ctx, userA, reqA, photo.TypeJPEG, fakeJPEG); !errors.Is(err, ErrPhotoLimit) {
		t.Fatalf("4th: %v, want ErrPhotoLimit", err)
	}

	// B cannot add to, list or read A's request.
	if _, err := k.AddRequestPhoto(ctx, userB, reqA, photo.TypeJPEG, fakeJPEG); !errors.Is(err, ErrPhotoNotFound) {
		t.Fatalf("B add to A: %v", err)
	}
	if _, err := k.ListRequestPhotos(ctx, userB, reqA); !errors.Is(err, ErrPhotoNotFound) {
		t.Fatalf("B list A: %v", err)
	}
	if _, _, err := k.ReadRequestPhoto(ctx, userB, reqA, ids[0]); !errors.Is(err, ErrPhotoNotFound) {
		t.Fatalf("B read A: %v", err)
	}
	// A photo id paired with the wrong request is not found either.
	if _, _, err := k.ReadRequestPhoto(ctx, userB, reqB, ids[0]); !errors.Is(err, ErrPhotoNotFound) {
		t.Fatalf("A's photo via B's request: %v", err)
	}

	ct, data, err := k.ReadRequestPhoto(ctx, userA, reqA, ids[0])
	if err != nil || ct != photo.TypeJPEG || string(data) != string(fakeJPEG) {
		t.Fatalf("A read own: %q %v", ct, err)
	}
	list, err := k.ListRequestPhotos(ctx, userA, reqA)
	if err != nil || len(list) != photo.MaxPerRequest {
		t.Fatalf("A list own: %d %v", len(list), err)
	}
	ds, err := k.DanhSachCuaToi(ctx, userA, 20)
	if err != nil || len(ds) != 1 || ds[0].PhotoCount != photo.MaxPerRequest {
		t.Fatalf("my-requests photoCount: %+v %v", ds, err)
	}
}

func TestRequestPhoto_OnlyConsultTakesPhotos(t *testing.T) {
	k := moKhoTest(t)
	user, req := newUserWithRequest(t, k, yeucau.LoaiHuyUuDaiSMS)
	if _, err := k.AddRequestPhoto(context.Background(), user, req, photo.TypeJPEG, fakeJPEG); !errors.Is(err, ErrPhotoNotAllowed) {
		t.Fatalf("err %v, want ErrPhotoNotAllowed", err)
	}
}

func TestAnDanhHoa_ErasesPhotoBytes(t *testing.T) {
	k := moKhoTest(t)
	ctx := context.Background()
	user, req := newUserWithRequest(t, k, yeucau.LoaiTuVan)
	m, err := k.AddRequestPhoto(ctx, user, req, photo.TypeJPEG, fakeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if _, data, err := k.ReadPhotoForDownload(ctx, m.ID); err != nil || string(data) != string(fakeJPEG) {
		t.Fatalf("download read before erasure: %v", err)
	}
	defer func() {
		// A signed link issued before the erasure must stop working with it.
		if _, _, err := k.ReadPhotoForDownload(ctx, m.ID); !errors.Is(err, ErrPhotoNotFound) {
			t.Errorf("download read after erasure: %v, want ErrPhotoNotFound", err)
		}
	}()
	var so string
	if err := k.pool.QueryRow(ctx, `SELECT so_dien_thoai FROM nguoi_dung WHERE id = $1`, user).Scan(&so); err != nil {
		t.Fatal(err)
	}
	kq, err := k.AnDanhHoa(ctx, YeuCauAnDanh{SoDienThoai: so, NguonYeuCau: "hotline", NguoiThucHien: "NV-017"})
	if err != nil {
		t.Fatalf("anonymise: %s", err)
	}
	if kq.PhotosErased != 1 {
		t.Fatalf("PhotosErased %d, want 1", kq.PhotosErased)
	}
	var left int
	if err := k.pool.QueryRow(ctx,
		`SELECT count(*) FROM request_photo WHERE request_id = $1 AND (data IS NOT NULL OR sha256 IS NOT NULL)`,
		req).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d photos still hold bytes after erasure", left)
	}
	var rows int
	if err := k.pool.QueryRow(ctx, `SELECT count(*) FROM request_photo WHERE request_id = $1`, req).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("photo row must stay (the fact remains, the photo goes): %d %v", rows, err)
	}
}
