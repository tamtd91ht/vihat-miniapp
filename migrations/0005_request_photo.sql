-- 0005_request_photo.sql — ảnh đính kèm yêu cầu "Tư vấn và báo giá".
--
-- Chạy: psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0005_request_photo.sql
-- Toàn bộ trong một giao dịch: hỏng giữa chừng thì không để lại nửa lược đồ.
--
-- ===========================================================================
-- QUYẾT ĐỊNH CỦA CHỦ SẢN PHẨM, 12/10/2026: trên màn "Tư vấn và báo giá" người dùng
-- được đính kèm ảnh chụp hoặc chọn từ máy, gửi cùng yêu cầu cho chuyên viên ViHAT.
-- Đây là lý do dùng quyền camera / thư viện ảnh trong hồ sơ duyệt Zalo.
--
-- MẶC ĐỊNH ĐÃ CHỌN (phiên điều phối, 12/10/2026) — lược đồ cưỡng chế chúng:
--
--   1. TỐI ĐA 3 ẢNH mỗi yêu cầu — `position` 1..3, duy nhất theo yêu cầu. Trần ở
--      CSDL chứ không chỉ ở tầng HTTP: hai lần tải song song không lọt được ảnh
--      thứ tư (tầng ứng dụng còn khoá hàng yeu_cau trước khi đếm).
--   2. MỖI ẢNH <= 5 MB, CHỈ JPEG / PNG / WebP — kiểu ĐỌC TỪ BYTE (số ma thuật),
--      không tin tên tệp hay Content-Type của client.
--   3. CHỈ loại `tu_van` (consult) — đúng màn đã chốt. Ảnh trên một phiếu "huỷ
--      nhận SMS" là dữ liệu cá nhân không việc gì cần tới.
--   4. LƯU CÙNG YÊU CẦU, GIỮ BẰNG ĐÚNG THỜI HẠN CỦA YÊU CẦU (24 tháng, 0003 mục 5).
--   5. ẢNH LÀ DỮ LIỆU CÁ NHÂN (có thể có người, biển số, nhà cửa). Ẩn danh hoá
--      (yêu cầu xoá theo Nghị định 13, và hạn 24 tháng) = XOÁ BYTE ẢNH và bản băm,
--      GIỮ HÀNG: "yêu cầu này từng có 2 ảnh, lúc nào, cỡ nào" còn lại; ảnh thì không.
--   6. Máy chủ BỎ SIÊU DỮ LIỆU (EXIF/XMP — có toạ độ GPS nơi chụp) trước khi lưu;
--      `size_bytes` / `sha256` là của bản ĐÃ BỎ, tức đúng byte đang lưu.
--
-- VÌ SAO bytea TRONG POSTGRES: kho này không có kho đối tượng nào, và thêm một hạ
-- tầng mới cho tối đa 15 MB mỗi yêu cầu là cái giá không cần trả bây giờ. Ẩn danh
-- hoá vì thế nằm trong CÙNG giao dịch với phần còn lại của lệnh `make an-danh` —
-- một kho đối tượng riêng thì xoá ảnh và ghi bằng chứng là hai hệ thống, hai
-- kết cục. Nếu lưu lượng thật làm CSDL phình, chuyển ra kho đối tượng là một
-- migration có chủ đích, không phải thứ làm trước.
--
-- LÙI LẠI: chỉ THÊM một bảng và THAY hàm dọn 24 tháng bằng bản dọn thêm ảnh. Bản
-- ứng dụng cũ chạy được trên lược đồ mới. Lùi lược đồ = DROP bảng này, tức là MẤT
-- ẢNH người dùng đã gửi — chỉ làm sau khi sao lưu, và phải khôi phục hàm dọn của
-- 0004 cùng lúc.
-- ===========================================================================

BEGIN;

DO $$
BEGIN
    IF to_regclass('yeu_cau') IS NULL THEN
        RAISE EXCEPTION '0005 can 0003 chay truoc: chua co bang yeu_cau';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS request_photo (
    -- id: UUID v4 sinh phía ứng dụng (crypto/rand) — nó đi ra ngoài trên đường dẫn
    -- GET .../photos/{photoId}, nên không được đoán được. Đoán được cũng không đủ
    -- để đọc: mọi lần đọc còn khớp chủ phiếu (nguoi_dung_id của phiên).
    id            uuid        PRIMARY KEY,
    request_id    uuid        NOT NULL REFERENCES yeu_cau (id),
    position      smallint    NOT NULL,
    content_type  text        NOT NULL,
    size_bytes    integer     NOT NULL,

    -- sha256 + data: NULL sau khi ẩn danh hoá. Bản băm cũng bị xoá vì nó vẫn
    -- XÁC NHẬN được "người này đã gửi đúng tấm ảnh kia" cho ai đang cầm ảnh ấy.
    sha256        bytea,
    data          bytea,

    created_at    timestamptz NOT NULL DEFAULT now(),
    anonymised_at timestamptz,

    CONSTRAINT request_photo_position_range CHECK (position BETWEEN 1 AND 3),
    CONSTRAINT request_photo_one_per_position UNIQUE (request_id, position),
    CONSTRAINT request_photo_type_allowed CHECK (
        content_type IN ('image/jpeg', 'image/png', 'image/webp')
    ),
    CONSTRAINT request_photo_size_range CHECK (size_bytes BETWEEN 1 AND 5242880),

    -- Chưa ẩn danh: có đủ byte và bản băm, khớp cỡ đã khai. Đã ẩn danh: không còn
    -- byte nào. Biến "đã xoá ảnh" từ lời hứa của mã nguồn thành bất biến của CSDL —
    -- một lệnh dọn chỉ đánh dấu mà quên xoá byte bị từ chối ngay.
    CONSTRAINT request_photo_anonymised_is_empty CHECK (
        (anonymised_at IS NULL AND data IS NOT NULL AND sha256 IS NOT NULL
             AND length(sha256) = 32 AND octet_length(data) = size_bytes)
        OR (anonymised_at IS NOT NULL AND data IS NULL AND sha256 IS NULL)
    )
);

-- Màn "Yêu cầu của tôi" đếm ảnh theo yêu cầu; ẩn danh hoá quét theo yêu cầu.
CREATE INDEX IF NOT EXISTS request_photo_by_request ON request_photo (request_id, position);

-- Hàm dọn 24 tháng — thay bằng bản xoá thêm ảnh. Cùng chữ ký và mặc định với
-- 0003/0004. Ảnh xoá TRƯỚC, trên đúng tập yêu cầu sắp ẩn danh, trong cùng hàm (tức
-- cùng giao dịch của lời gọi): không có lúc nào phiếu đã "ẩn danh" mà ảnh còn.
CREATE OR REPLACE FUNCTION an_danh_yeu_cau_qua_han(gioi_han interval DEFAULT interval '24 months')
RETURNS bigint
LANGUAGE plpgsql AS $$
DECLARE
    so_hang bigint;
BEGIN
    UPDATE request_photo p
       SET data          = NULL,
           sha256        = NULL,
           anonymised_at = now()
      FROM yeu_cau y
     WHERE p.request_id = y.id
       AND p.anonymised_at IS NULL
       AND y.an_danh_luc IS NULL
       AND y.tao_luc < now() - gioi_han;

    UPDATE yeu_cau
       SET ghi_chu      = NULL,
           ten_hien_thi = NULL,
           an_danh_luc  = now()
     WHERE an_danh_luc IS NULL
       AND tao_luc < now() - gioi_han;
    GET DIAGNOSTICS so_hang = ROW_COUNT;
    RETURN so_hang;
END;
$$;

COMMENT ON FUNCTION an_danh_yeu_cau_qua_han(interval) IS
    'An danh hoa yeu_cau qua han luu (mac dinh 24 thang): xoa ghi_chu, ten_hien_thi va byte anh (request_photo), giu hang. Phai duoc goi hang ngay.';

COMMIT;
