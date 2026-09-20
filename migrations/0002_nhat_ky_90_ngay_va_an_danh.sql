-- 0002 — hai quyết định của chủ sản phẩm (20/09/2026), cả hai là cam kết in
-- trong Chính sách riêng tư gửi kèm hồ sơ duyệt Zalo:
--
--   1. XOÁ NGHĨA LÀ ẨN DANH HOÁ. Lược đồ 0001 giữ nguyên: không nới trigger,
--      không thêm ON DELETE. Ở đây chỉ thêm thứ hạ tầng mà việc ẩn danh cần.
--   2. NHẬT KÝ ĐĂNG NHẬP LƯU 90 NGÀY.
--
-- Chạy: psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0002_nhat_ky_90_ngay_va_an_danh.sql
-- Cần PostgreSQL >= 13 (0001 chỉ cần >= 10): xem ghi chú ở mục 2.
--
-- ===========================================================================
-- VÌ SAO DỌN NHẬT KÝ LẠI PHẢI ĐỔI SANG BẢNG PHÂN MẢNH
--
-- Dọn 90 ngày nghĩa là DELETE, mà DELETE trên nhat_ky_dang_nhap bị trigger
-- nhat_ky_chan_sua_xoa từ chối — đúng như thiết kế. Hai đường đi tiếp:
--
--   (a) Cho trigger một cờ mức phiên để nó nhận ra "đây là lần dọn định kỳ".
--       Nhanh, ít việc. Nhưng nó là MỘT CÁNH CỬA MỞ SẴN trong đúng cơ chế làm
--       cho nhật ký có giá trị chứng cứ: từ hôm đó trở đi, bất kỳ ai đặt được
--       cờ ấy đều xoá được nhật ký, và không có gì trong CSDL phân biệt được
--       lần dọn định kỳ với một lần xoá dấu vết. Cánh cửa đó không tự đóng lại.
--
--   (b) Chia phân mảnh theo thời gian rồi DROP cả phân mảnh đã quá hạn.
--       DROP là DDL, không chạy qua trigger mức dòng, nên tính CHỈ-GHI-THÊM
--       không bị khoét lỗ nào: vẫn không ai UPDATE hay DELETE được một dòng.
--       Thứ bỏ đi là CẢ MỘT KHOẢNG THỜI GIAN đã quá hạn lưu, một hành vi nhìn
--       thấy được trong danh sách bảng, không phải một hàng lặng lẽ biến mất.
--
-- CHỌN (b). Cái giá phải trả, nói rõ ở đây để không ai ngạc nhiên sau:
--   - PHÂN MẢNH THEO TUẦN, DROP khi ĐẦU khoảng đã quá 90 ngày. Nghĩa là mỗi
--     dòng sống TỐI ĐA 90 NGÀY, TỐI THIỂU 83 — dọn theo lô thì không thể đúng
--     90 cho mọi dòng, và phần lệch được chọn đi về phía XOÁ SỚM: 90 ngày là
--     trần đã hứa với người dùng, vượt trần là chuyện không được phép, còn xoá
--     sớm chỉ làm ngắn cửa sổ truy một đợt lạm dụng tối đa 7 ngày.
--     Chia theo tháng thì biên độ thành 60-90 ngày (mất tới một tháng dữ liệu
--     lạm dụng); chia theo ngày thì sát nhất nhưng lỡ một ngày là hỏng. Tuần là
--     chỗ đứng giữa.
--   - Khoá chính đổi từ (id) thành (id, tao_luc): Postgres đòi khoá chính của
--     bảng phân mảnh phải chứa khoá phân mảnh. Không bảng nào tham chiếu
--     nhat_ky_dang_nhap nên thay đổi này không làm gãy gì.
--   - PHẢI CHẠY HẰNG NGÀY `make don-nhat-ky`. Hai lý do, cả hai đều cứng:
--     khoảng cách giữa hai lần chạy CỘNG THẲNG vào tuổi của dòng cũ nhất, nên
--     chạy hằng tuần là trần thành 97 ngày — sai đúng cái vừa sửa; và bỏ bẵng
--     việc tạo phân mảnh thì dòng mới rơi vào phân mảnh mặc định, không mất dữ
--     liệu nhưng phải dọn tay trước khi tạo lại được phân mảnh của tuần đó.
-- ===========================================================================

BEGIN;

-- Chạy hai lần thì DỪNG HẲN kèm câu nói rõ, thay vì hỏng nửa chừng ở lệnh
-- RENAME với một thông báo không ai đoán ra.
DO $$
BEGIN
    IF to_regclass('nhat_ky_dang_nhap') IS NULL THEN
        RAISE EXCEPTION '0002 can 0001 chay truoc: chua co bang nhat_ky_dang_nhap';
    END IF;
    IF (SELECT relkind FROM pg_class WHERE oid = 'nhat_ky_dang_nhap'::regclass) = 'p' THEN
        RAISE EXCEPTION '0002 da chay roi: nhat_ky_dang_nhap da la bang phan manh';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 1. Hạ tầng cho việc ẩn danh hoá
-- ---------------------------------------------------------------------------

-- nguoi_dung.so_dien_thoai là NOT NULL UNIQUE CHECK (~'^[0-9]{9,15}$'), nên giá
-- trị thay thế không được là NULL, không được là chuỗi rỗng, và không được là
-- một hằng dùng chung cho mọi người. Chuỗi sinh từ dãy số này thoả cả ba:
--
--   '0' || 13 chữ số  ->  14 chữ số, hợp CHECK, duy nhất vĩnh viễn.
--
-- Vì sao bắt đầu bằng '0': số thật luôn được chuẩn hoá về dạng có mã quốc gia
-- (84...) trước khi ghi, nên không một số thật nào bắt đầu bằng 0. Nhìn vào cột
-- là biết ngay hàng nào đã ẩn danh, không cần thêm cờ.
CREATE SEQUENCE IF NOT EXISTS nguoi_dung_an_danh_seq AS bigint START 1;

COMMENT ON SEQUENCE nguoi_dung_an_danh_seq IS
    'Sinh gia tri thay the cho so_dien_thoai khi an danh hoa. Khong bao gio reset.';

-- Rào chắn chỉ-ghi-thêm nay canh hai bảng, nên thông điệp phải nói đúng bảng
-- nào vừa bị từ chối. 0001 viết cứng tên nhat_ky_dang_nhap vào câu báo lỗi.
CREATE OR REPLACE FUNCTION nhat_ky_chi_duoc_ghi_them() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% chi duoc ghi them: % bi tu choi', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$;

-- nhat_ky_an_danh — ai đã ẩn danh cho ai, theo yêu cầu đến từ đâu.
--
-- Đây là BẰNG CHỨNG đã xử lý một yêu cầu xoá theo Nghị định 13, và là chỗ trả
-- lời câu "ai cho phép xoá dữ liệu của người này". Không chứa số điện thoại —
-- sau khi ẩn danh thì chính hệ thống cũng không còn biết số ấy là gì nữa.
--
-- Chỉ ghi thêm, giữ VÔ THỜI HẠN: một bằng chứng có hạn tự huỷ thì không phải
-- bằng chứng. Bảng này không nằm trong chính sách 90 ngày của nhật ký đăng nhập.
CREATE TABLE IF NOT EXISTS nhat_ky_an_danh (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nguoi_dung_id  uuid        NOT NULL REFERENCES nguoi_dung (id),
    nguon_yeu_cau  text        NOT NULL,
    -- nguoi_thuc_hien: tên hoặc mã nhân sự của người bấm lệnh. "Có người ký"
    -- nghĩa là chỗ này không được để trống.
    nguoi_thuc_hien text       NOT NULL,
    -- ghi_chu: số phiếu, mã cuộc gọi, tiêu đề email. KHÔNG chép nội dung yêu
    -- cầu vào đây — nội dung ấy thường có số điện thoại.
    ghi_chu        text,
    tao_luc        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT an_danh_nguon_hop_le CHECK (nguon_yeu_cau IN ('hotline', 'email')),
    CONSTRAINT an_danh_nguoi_thuc_hien_khong_rong CHECK (btrim(nguoi_thuc_hien) <> '')
);

CREATE INDEX IF NOT EXISTS an_danh_theo_thoi_gian ON nhat_ky_an_danh (tao_luc DESC);

DROP TRIGGER IF EXISTS an_danh_chan_sua_xoa ON nhat_ky_an_danh;
CREATE TRIGGER an_danh_chan_sua_xoa
    BEFORE UPDATE OR DELETE ON nhat_ky_an_danh
    FOR EACH ROW EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

DROP TRIGGER IF EXISTS an_danh_chan_truncate ON nhat_ky_an_danh;
CREATE TRIGGER an_danh_chan_truncate
    BEFORE TRUNCATE ON nhat_ky_an_danh
    FOR EACH STATEMENT EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

-- ---------------------------------------------------------------------------
-- 2. Chuyển nhat_ky_dang_nhap sang bảng phân mảnh theo tuần
-- ---------------------------------------------------------------------------

-- Đổi tên bảng cũ. Chỉ số và dãy số của nó giữ nguyên tên, nên phải đổi tên cả
-- hai, nếu không bảng mới không tạo được vì trùng tên.
ALTER TABLE nhat_ky_dang_nhap RENAME TO nhat_ky_dang_nhap_truoc_0002;
ALTER INDEX nhat_ky_theo_thoi_gian RENAME TO nhat_ky_theo_thoi_gian_truoc_0002;
ALTER INDEX nhat_ky_theo_nguoi_dung RENAME TO nhat_ky_theo_nguoi_dung_truoc_0002;
ALTER SEQUENCE nhat_ky_dang_nhap_id_seq RENAME TO nhat_ky_dang_nhap_id_seq_truoc_0002;

-- Bảng mới: y hệt bảng cũ, trừ khoá chính phải chứa khoá phân mảnh.
CREATE TABLE nhat_ky_dang_nhap (
    id            bigint      GENERATED ALWAYS AS IDENTITY,
    nguoi_dung_id uuid        REFERENCES nguoi_dung (id),
    phien_id      uuid        REFERENCES phien (id),
    ket_qua       text        NOT NULL,
    ly_do         text,
    dia_chi_ip    inet,
    tao_luc       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT nhat_ky_ket_qua_hop_le CHECK (
        ket_qua IN ('thanh_cong', 'token_zalo_hong', 'loi_zalo', 'qua_nhieu_lan', 'loi_he_thong')
    ),
    CONSTRAINT nhat_ky_thanh_cong_phai_du_ma CHECK (
        ket_qua <> 'thanh_cong' OR (nguoi_dung_id IS NOT NULL AND phien_id IS NOT NULL)
    ),
    PRIMARY KEY (id, tao_luc)
) PARTITION BY RANGE (tao_luc);

CREATE INDEX nhat_ky_theo_thoi_gian ON nhat_ky_dang_nhap (tao_luc DESC);
CREATE INDEX nhat_ky_theo_nguoi_dung ON nhat_ky_dang_nhap (nguoi_dung_id, tao_luc DESC);

-- Gắn rào chắn chỉ-ghi-thêm lên MỘT phân mảnh.
--
-- Vì sao gắn lên từng phân mảnh chứ không chỉ lên bảng cha: trigger mức dòng
-- trên bảng cha có lan xuống phân mảnh (PG >= 13), nhưng trigger TRUNCATE thì
-- không. Gắn thẳng lên phân mảnh là cách chắc chắn cho cả hai, và nó vẫn đúng
-- khi ai đó đụng trực tiếp vào một phân mảnh thay vì qua bảng cha.
CREATE OR REPLACE FUNCTION nhat_ky_gan_rao_chan(ten text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE format(
        'CREATE TRIGGER nhat_ky_chan_sua_xoa BEFORE UPDATE OR DELETE ON %I
         FOR EACH ROW EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them()', ten);
    EXECUTE format(
        'CREATE TRIGGER nhat_ky_chan_truncate BEFORE TRUNCATE ON %I
         FOR EACH STATEMENT EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them()', ten);
END;
$$;

-- Phân mảnh mặc định — LƯỚI AN TOÀN, không phải chỗ chứa bình thường.
--
-- Không có nó thì một dòng có tao_luc nằm ngoài mọi khoảng sẽ bị TỪ CHỐI, và vì
-- nhật ký ghi chung giao dịch với việc mở phiên, lần đăng nhập ấy hỏng theo:
-- không phải "mất một dòng nhật ký" mà là "người dùng không đăng nhập được".
--
-- Có dòng rơi vào đây = việc tạo phân mảnh đã bị bỏ bẵng. Hàm dọn sẽ cảnh báo.
CREATE TABLE nhat_ky_dang_nhap_mac_dinh PARTITION OF nhat_ky_dang_nhap DEFAULT;
SELECT nhat_ky_gan_rao_chan('nhat_ky_dang_nhap_mac_dinh');

-- Tạo phân mảnh cho các tuần sắp tới. Idempotent: gọi lại chỉ tạo phần thiếu.
--
-- Mốc tuần tính theo UTC để không phụ thuộc vào TimeZone của phiên đang chạy —
-- cùng một lệnh chạy ở hai máy khác múi giờ phải cho ra cùng một ranh giới.
CREATE OR REPLACE FUNCTION nhat_ky_tao_phan_manh(so_tuan int DEFAULT 12)
RETURNS int LANGUAGE plpgsql AS $$
DECLARE
    dau_tuan date := (date_trunc('week', (now() AT TIME ZONE 'UTC')))::date;
    d        date;
    ten      text;
    i        int;
    da_tao   int := 0;
BEGIN
    IF so_tuan < 1 THEN
        RAISE EXCEPTION 'so_tuan phai >= 1';
    END IF;
    -- Lùi lại 1 tuần: bọc luôn các dòng vừa ghi ngay trước lúc chạy lệnh này.
    FOR i IN -1 .. so_tuan LOOP
        d := dau_tuan + (i * 7);
        ten := 'nhat_ky_dang_nhap_t' || to_char(d, 'YYYY_MMDD');
        IF to_regclass(ten) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF nhat_ky_dang_nhap FOR VALUES FROM (%L) TO (%L)',
                ten,
                (d::timestamp AT TIME ZONE 'UTC'),
                ((d + 7)::timestamp AT TIME ZONE 'UTC'));
            PERFORM nhat_ky_gan_rao_chan(ten);
            da_tao := da_tao + 1;
        END IF;
    END LOOP;
    RETURN da_tao;
END;
$$;

COMMENT ON FUNCTION nhat_ky_tao_phan_manh(int) IS
    'Tao truoc cac phan manh tuan cho nhat_ky_dang_nhap. Chay dinh ky (xem make don-nhat-ky).';

-- Dọn nhật ký quá hạn.
--
-- THỜI HẠN LƯU NHẬT KÝ ĐĂNG NHẬP = 90 NGÀY. Chủ sản phẩm chốt 20/09/2026, và
-- con số ấy nằm trong Chính sách riêng tư gửi kèm hồ sơ duyệt Zalo. Đây là
-- NGUỒN DUY NHẤT của con số: nơi khác chỉ được gọi hàm này không tham số.
--
-- DROP khi ĐẦU khoảng của phân mảnh đã quá hạn (`d <= nguong`), nên MỖI DÒNG
-- SỐNG TỐI ĐA 90 NGÀY, tối thiểu 83 — dọn theo lô tuần thì không thể đúng 90
-- cho mọi dòng, và phần lệch phải đi về phía XOÁ SỚM.
--
-- Điều kiện này từng viết là `d + 7 <= nguong` (DROP khi ĐUÔI khoảng quá hạn).
-- Nghe thì "không bao giờ xoá sớm một dòng nào", nhưng nó đẩy dòng cũ nhất lên
-- tới 97 ngày — tức HỆ THỐNG VƯỢT QUA chính cái trần đã hứa trong văn bản pháp
-- lý. Một cam kết lệch bảy ngày về phía giữ lâu hơn là thứ không được để lọt;
-- lệch về phía xoá sớm thì chỉ mất tối đa 7 ngày cửa sổ truy một đợt lạm dụng,
-- và 90 ngày là TRẦN cam kết với người dùng chứ không phải sàn nghiệp vụ.
--
-- Trần 90 ngày chỉ đúng nếu hàm này CHẠY HẰNG NGÀY: khoảng cách giữa hai lần
-- chạy cộng thẳng vào tuổi của dòng cũ nhất.
CREATE OR REPLACE FUNCTION nhat_ky_don_qua_han(so_ngay int DEFAULT 90)
RETURNS int LANGUAGE plpgsql AS $$
DECLARE
    nguong  date := (now() AT TIME ZONE 'UTC')::date - so_ngay;
    r       record;
    m       text[];
    d       date;
    da_xoa  int := 0;
    con_sot bigint;
BEGIN
    IF so_ngay < 1 THEN
        RAISE EXCEPTION 'so_ngay phai >= 1 — khong xoa nhat ky cua hom nay';
    END IF;

    FOR r IN
        SELECT c.relname AS ten
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = 'nhat_ky_dang_nhap'
    LOOP
        m := regexp_match(r.ten, '^nhat_ky_dang_nhap_t([0-9]{4})_([0-9]{4})$');
        CONTINUE WHEN m IS NULL;  -- bỏ qua phân mảnh mặc định
        d := to_date(m[1] || m[2], 'YYYYMMDD');
        -- `d <=`, KHÔNG phải `d + 7 <=`: xem khối chú thích trên hàm.
        IF d <= nguong THEN
            EXECUTE format('DROP TABLE %I', r.ten);
            da_xoa := da_xoa + 1;
        END IF;
    END LOOP;

    EXECUTE 'SELECT count(*) FROM nhat_ky_dang_nhap_mac_dinh' INTO con_sot;
    IF con_sot > 0 THEN
        RAISE WARNING 'phan manh mac dinh dang giu % dong: nhat_ky_tao_phan_manh() da bi bo bang', con_sot;
    END IF;

    RETURN da_xoa;
END;
$$;

COMMENT ON FUNCTION nhat_ky_don_qua_han(int) IS
    'Xoa cac phan manh nhat ky da qua han. Mac dinh 90 ngay (chinh sach chot 20/09/2026): '
    'moi dong song TOI DA 90 ngay, toi thieu 83 vi don theo lo tuan. Phai chay HANG NGAY.';

SELECT nhat_ky_tao_phan_manh();

-- Phân mảnh cho KHOẢNG THỜI GIAN của dữ liệu cũ.
--
-- Không có bước này, dòng cũ rơi hết vào phân mảnh mặc định — không mất dữ
-- liệu, nhưng phân mảnh mặc định thì không bao giờ bị dọn, nên chính sách 90
-- ngày sẽ không bao giờ chạm tới đám dữ liệu có trước lần chuyển đổi này.
DO $$
DECLARE tu date; den date; d date; ten text;
BEGIN
    SELECT date_trunc('week', (min(tao_luc) AT TIME ZONE 'UTC'))::date,
           date_trunc('week', (max(tao_luc) AT TIME ZONE 'UTC'))::date
      INTO tu, den
      FROM nhat_ky_dang_nhap_truoc_0002;

    IF tu IS NULL THEN
        RETURN;  -- bảng cũ rỗng: không có gì để bọc
    END IF;

    d := tu;
    WHILE d <= den LOOP
        ten := 'nhat_ky_dang_nhap_t' || to_char(d, 'YYYY_MMDD');
        IF to_regclass(ten) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF nhat_ky_dang_nhap FOR VALUES FROM (%L) TO (%L)',
                ten,
                (d::timestamp AT TIME ZONE 'UTC'),
                ((d + 7)::timestamp AT TIME ZONE 'UTC'));
            PERFORM nhat_ky_gan_rao_chan(ten);
        END IF;
        d := d + 7;
    END LOOP;
END $$;

-- Chép dữ liệu cũ sang. OVERRIDING SYSTEM VALUE vì id là cột IDENTITY ALWAYS.
INSERT INTO nhat_ky_dang_nhap (id, nguoi_dung_id, phien_id, ket_qua, ly_do, dia_chi_ip, tao_luc)
OVERRIDING SYSTEM VALUE
SELECT id, nguoi_dung_id, phien_id, ket_qua, ly_do, dia_chi_ip, tao_luc
FROM nhat_ky_dang_nhap_truoc_0002;

-- Đếm lại TRƯỚC khi bỏ bảng cũ. Không có bước này thì một lần chép thiếu sẽ
-- lặng lẽ thành mất nhật ký vĩnh viễn.
DO $$
DECLARE cu bigint; moi bigint; lon_nhat bigint;
BEGIN
    SELECT count(*) INTO cu FROM nhat_ky_dang_nhap_truoc_0002;
    SELECT count(*) INTO moi FROM nhat_ky_dang_nhap;
    IF cu <> moi THEN
        RAISE EXCEPTION 'chep nhat ky thieu: bang cu % dong, bang moi % dong', cu, moi;
    END IF;

    -- Dãy số phải tiếp tục từ id lớn nhất, nếu không lần ghi kế tiếp đụng khoá chính.
    SELECT max(id) INTO lon_nhat FROM nhat_ky_dang_nhap;
    IF lon_nhat IS NOT NULL THEN
        PERFORM setval(pg_get_serial_sequence('nhat_ky_dang_nhap', 'id'), lon_nhat);
    END IF;
END $$;

DROP TABLE nhat_ky_dang_nhap_truoc_0002;

COMMIT;
