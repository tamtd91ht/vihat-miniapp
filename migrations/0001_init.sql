-- 0001_init.sql — định danh + phiên đăng nhập + nhật ký đăng nhập.
-- Phạm vi bước này đúng ba bảng, không hơn.
--
-- Chạy: psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
-- Toàn bộ nằm trong một giao dịch: hỏng giữa chừng thì không để lại nửa lược đồ.

BEGIN;

-- ---------------------------------------------------------------------------
-- nguoi_dung — danh tính. Với Mini App giới thiệu doanh nghiệp, danh tính
-- CHÍNH LÀ số điện thoại người dùng bấm đồng ý chia sẻ.
--
-- id là UUID sinh phía ứng dụng, không phải số tăng dần: đây là mã định danh
-- duy nhất được phép đi ra ngoài (log nghiệp vụ, hỗ trợ, thống kê). Số tăng dần
-- thì đoán được và tiết lộ quy mô người dùng.
--
-- so_dien_thoai là DỮ LIỆU CÁ NHÂN (Nghị định 13/2023). Nó được lưu vì không có
-- nó thì không có danh tính; nhưng nó KHÔNG được đi vào log, URL, tên tệp, khoá
-- cache, thông điệp lỗi, và ở bước này KHÔNG có tuyến API nào trả nó ra.
-- Lưu dạng đã chuẩn hoá 84xxxxxxxxx — xem internal/zalo/wire.go ChuanHoaSo.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS nguoi_dung (
    id            uuid        PRIMARY KEY,
    so_dien_thoai text        NOT NULL,
    tao_luc       timestamptz NOT NULL DEFAULT now(),
    cap_nhat_luc  timestamptz NOT NULL DEFAULT now(),

    -- Một người một hàng. Chuẩn hoá ở đúng một chỗ phía ứng dụng là điều kiện
    -- để ràng buộc này có nghĩa.
    CONSTRAINT nguoi_dung_so_dien_thoai_duy_nhat UNIQUE (so_dien_thoai),
    CONSTRAINT nguoi_dung_so_dien_thoai_dinh_dang CHECK (so_dien_thoai ~ '^[0-9]{9,15}$')
);

-- ---------------------------------------------------------------------------
-- phien — phiên đăng nhập. TTL 7 ngày là chính sách sản phẩm, do ứng dụng tính
-- và ghi vào het_han_luc (xem internal/config.TTLPhien).
--
-- token_bam giữ SHA-256 CỦA bearer token, không giữ token.
-- Vì sao: một bản sao CSDL rò ra ngoài mà bảng này chứa token nguyên bản thì kẻ
-- lấy được nó đăng nhập thay mọi người dùng ngay lập tức, không cần mật khẩu,
-- không cần Zalo. Với bản băm thì bản sao đó vô dụng.
--
-- thu_hoi_luc NULL = còn hiệu lực. Thu hồi là ĐÁNH DẤU, không xoá hàng: một
-- phiên đã tồn tại là một sự kiện đã xảy ra.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS phien (
    id            uuid        PRIMARY KEY,
    nguoi_dung_id uuid        NOT NULL REFERENCES nguoi_dung (id),
    token_bam     bytea       NOT NULL,
    tao_luc       timestamptz NOT NULL DEFAULT now(),
    het_han_luc   timestamptz NOT NULL,
    thu_hoi_luc   timestamptz,

    CONSTRAINT phien_token_bam_duy_nhat UNIQUE (token_bam),
    CONSTRAINT phien_token_bam_dung_sha256 CHECK (octet_length(token_bam) = 32),
    CONSTRAINT phien_het_han_sau_tao CHECK (het_han_luc > tao_luc)
);

-- Tra phiên còn hiệu lực của một người: dùng khi thêm tuyến cần xác thực.
CREATE INDEX IF NOT EXISTS phien_theo_nguoi_dung
    ON phien (nguoi_dung_id, het_han_luc DESC);

-- ---------------------------------------------------------------------------
-- nhat_ky_dang_nhap — DỮ LIỆU NGHIỆP VỤ, không phải log kỹ thuật.
-- Log xoay vòng theo dung lượng và mất được; bảng này thì không.
--
-- CHỈ GHI THÊM. Không sửa, không xoá — được cưỡng chế bằng trigger bên dưới chứ
-- không bằng lời hứa trong tài liệu. Một nhật ký sửa được là một nhật ký không
-- chứng minh được gì khi có tranh chấp.
--
-- KHÔNG BAO GIỜ chứa số điện thoại. Chỉ chứa nguoi_dung_id (mã định danh).
-- Lần thất bại thì chưa biết là ai, nguoi_dung_id để NULL — đó là sự thật,
-- không phải thiếu sót.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS nhat_ky_dang_nhap (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nguoi_dung_id uuid        REFERENCES nguoi_dung (id),
    phien_id      uuid        REFERENCES phien (id),
    ket_qua       text        NOT NULL,
    -- ly_do: MÃ NGẮN do ứng dụng đặt (ví dụ 'token_zalo_tu_choi'), để đếm và
    -- dựng cảnh báo. Không phải câu văn, không mang dữ liệu người dùng.
    ly_do         text,
    -- dia_chi_ip: cần để nhận ra lạm dụng. Xem README mục "Dữ liệu cá nhân"
    -- về thời hạn lưu — hiện CHƯA CHỐT.
    dia_chi_ip    inet,
    tao_luc       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT nhat_ky_ket_qua_hop_le CHECK (
        ket_qua IN ('thanh_cong', 'token_zalo_hong', 'loi_zalo', 'qua_nhieu_lan', 'loi_he_thong')
    ),
    -- Thành công thì phải biết là ai và phiên nào; thất bại thì không được phép
    -- vờ là có. Ràng buộc này chặn một nhật ký "thành công" rỗng nghĩa.
    CONSTRAINT nhat_ky_thanh_cong_phai_du_ma CHECK (
        ket_qua <> 'thanh_cong' OR (nguoi_dung_id IS NOT NULL AND phien_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS nhat_ky_theo_thoi_gian ON nhat_ky_dang_nhap (tao_luc DESC);
CREATE INDEX IF NOT EXISTS nhat_ky_theo_nguoi_dung ON nhat_ky_dang_nhap (nguoi_dung_id, tao_luc DESC);

-- Cưỡng chế "chỉ ghi thêm" ở tầng CSDL. Quyền GRANT không đủ: ứng dụng thường
-- chạy bằng chính chủ sở hữu bảng, và chủ sở hữu thì bỏ qua GRANT.
CREATE OR REPLACE FUNCTION nhat_ky_chi_duoc_ghi_them() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'nhat_ky_dang_nhap chi duoc ghi them: % bi tu choi', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$;

DROP TRIGGER IF EXISTS nhat_ky_chan_sua_xoa ON nhat_ky_dang_nhap;
CREATE TRIGGER nhat_ky_chan_sua_xoa
    BEFORE UPDATE OR DELETE ON nhat_ky_dang_nhap
    FOR EACH ROW EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

DROP TRIGGER IF EXISTS nhat_ky_chan_truncate ON nhat_ky_dang_nhap;
CREATE TRIGGER nhat_ky_chan_truncate
    BEFORE TRUNCATE ON nhat_ky_dang_nhap
    FOR EACH STATEMENT EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

COMMIT;
