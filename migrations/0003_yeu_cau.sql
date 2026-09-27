-- 0003_yeu_cau.sql — yêu cầu tư vấn / gọi lại, lịch sử trạng thái, và vết gửi ZNS.
--
-- Chạy: psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0003_yeu_cau.sql
-- Toàn bộ trong một giao dịch: hỏng giữa chừng thì không để lại nửa lược đồ.
--
-- ===========================================================================
-- ĐÂY LÀ BƯỚC ỨNG DỤNG ĐỔI TƯ CÁCH PHÁP LÝ, KHÔNG PHẢI MỘT BẢNG MỚI.
--
-- Tới 0002, Mini App chỉ giữ ĐỊNH DANH: một số điện thoại người dùng bấm đồng ý
-- chia sẻ, để đăng nhập. Từ 0003 nó giữ thêm DỮ LIỆU BÁN HÀNG do chính người
-- dùng khai — họ quan tâm sản phẩm nào, quy mô ra sao, và một ô ghi chú tự do.
-- Ô ghi chú ấy là chỗ nguy hiểm nhất của cả lược đồ này: người ta gõ vào đó tên
-- công ty, tên mình, số máy bàn, đôi khi cả số của đồng nghiệp. Mọi rào chắn
-- dưới đây tồn tại vì một cột văn bản tự do là một cột dữ liệu cá nhân cho tới
-- khi chứng minh được ngược lại.
--
-- BA QUYẾT ĐỊNH CỦA CHỦ SẢN PHẨM, 22/09/2026 — ghi ở đây vì lược đồ cưỡng chế
-- chúng, và một quyết định chỉ nằm trong tài liệu là một quyết định sẽ trôi:
--
--   1. HẠN LƯU 24 THÁNG, rồi ẨN DANH HOÁ — không xoá hàng. Vòng đời bán B2B
--      của OmiCall/OMICRM kéo 6-18 tháng, nên dưới 24 tháng là cắt mất lịch sử
--      của một khách hàng quay lại. Xem mục 4.
--   2. GỌI LẠI 24/7, TRẦN 3 LƯỢT. Xem mục 2 về vì sao trần đếm theo cửa sổ
--      TRƯỢT 24 giờ chứ không theo ngày lịch.
--   3. CHƯA NỐI OMICRM. Cột `ma_omicrm` được chừa sẵn nhưng không có dây nào
--      ghi vào nó ở lượt này — xem mục 1.
-- ===========================================================================

BEGIN;

-- Chạy nhầm thứ tự thì DỪNG HẲN kèm câu nói rõ, thay vì hỏng ở một REFERENCES
-- với thông báo không ai đoán ra.
DO $$
BEGIN
    IF to_regclass('nguoi_dung') IS NULL THEN
        RAISE EXCEPTION '0003 can 0001 chay truoc: chua co bang nguoi_dung';
    END IF;
    IF to_regclass('nhat_ky_an_danh') IS NULL THEN
        RAISE EXCEPTION '0003 can 0002 chay truoc: chua co ha tang an danh hoa';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 1. yeu_cau — một lần người dùng giơ tay.
--
-- id là UUID sinh phía ứng dụng, cùng lý do với nguoi_dung.id (0001): đây là mã
-- ĐI RA NGOÀI — nó hiện trên màn "Yêu cầu của tôi", đi vào phiếu chăm sóc, và
-- được đọc qua điện thoại cho người dùng. Số tăng dần thì vừa đoán được (người
-- này đọc được yêu cầu của người khác chỉ bằng cách trừ đi 1) vừa để lộ ViHAT
-- nhận bao nhiêu lead mỗi tuần cho bất kỳ ai đếm.
--
-- KHÔNG CÓ CỘT SỐ ĐIỆN THOẠI, VÀ SẼ KHÔNG BAO GIỜ CÓ. Số nằm ở đúng một chỗ
-- trong cả CSDL — nguoi_dung.so_dien_thoai — và bảng này trỏ tới đó bằng khoá
-- ngoại. Chép số sang đây "cho tiện gọi ra" là dựng chỗ thứ hai giữ dữ liệu cá
-- nhân, và là chỗ mà lệnh ẩn danh hoá của 0002 không với tới.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS yeu_cau (
    id               uuid        PRIMARY KEY,
    nguoi_dung_id    uuid        NOT NULL REFERENCES nguoi_dung (id),

    -- loai: hai việc khác hẳn nhau về hệ quả, nên chúng là hai giá trị chứ
    -- không phải một cờ. 'goi_lai' làm TỔNG ĐÀI QUAY SỐ RA MỘT MÁY THẬT; 'tu_van'
    -- thì không đụng vào ai cho tới khi có người mở phiếu ra đọc.
    loai             text        NOT NULL,

    trang_thai       text        NOT NULL DEFAULT 'moi',

    -- quan_tam: mã sản phẩm người dùng chọn trên màn hình, ví dụ {'omicall'}.
    -- Mảng chứ không bảng nối: đây là câu trả lời của MỘT biểu mẫu tại MỘT thời
    -- điểm, không phải một quan hệ sống. Không có truy vấn nào cần join nó.
    quan_tam         text[]      NOT NULL DEFAULT '{}',

    -- quy_mo: mã ngắn do ứng dụng đặt ('duoi-10', '10-50'…), KHÔNG phải chữ
    -- người dùng gõ. Một mã thì đếm được; một câu thì chỉ đọc được.
    quy_mo           text,

    -- ghi_chu: Ô VĂN BẢN TỰ DO — cột đắt nhất của bảng này. Xem khối đầu tệp.
    -- Nó KHÔNG BAO GIỜ được vào log, vào thông điệp lỗi, hay ra khỏi máy chủ
    -- qua bất kỳ tuyến nào ngoài màn quản trị nội bộ. Nó là thứ đầu tiên bị ẩn
    -- danh hoá ở mục 4.
    ghi_chu          text,

    -- nguon_chien_dich: mã chiến dịch app đọc được từ tham số mở app (QR gian
    -- hàng, tờ rơi, chữ ký email). KHÔNG phải dữ liệu cá nhân, nhưng LÀ chuỗi
    -- do client gửi lên, nên nó bị chặn độ dài và bộ ký tự ở ràng buộc dưới.
    nguon_chien_dich text,

    -- ma_omicrm: CHỪA CHỖ, CHƯA CÓ DÂY NÀO GHI VÀO (quyết định 22/09/2026).
    --
    -- Để cột trống sẵn chứ không chờ tới lúc nối dây là có chủ đích và rẻ: thêm
    -- một cột NULL được vào bảng đang chạy là một ALTER tức thời, nhưng thêm nó
    -- MUỘN nghĩa là mọi yêu cầu nhận trong khoảng giữa không có chỗ ghi mã đối
    -- tượng bên OMICRM, và việc đối soát ngược về sau phải làm bằng tay.
    ma_omicrm        text,

    tao_luc          timestamptz NOT NULL DEFAULT now(),
    cap_nhat_luc     timestamptz NOT NULL DEFAULT now(),

    -- an_danh_luc: NULL = còn nguyên văn. Xem mục 4.
    an_danh_luc      timestamptz,

    CONSTRAINT yeu_cau_loai_hop_le CHECK (loai IN ('tu_van', 'goi_lai')),
    CONSTRAINT yeu_cau_trang_thai_hop_le CHECK (
        trang_thai IN ('moi', 'dang_xu_ly', 'da_lien_he', 'dong')
    ),

    -- Ba trần độ dài. Chúng KHÔNG phải để "dữ liệu cho gọn": một tuyến công
    -- khai nhận chuỗi không giới hạn là một đường làm phình CSDL bằng một vòng
    -- lặp curl. Trần ở CSDL chứ không chỉ ở tầng HTTP, vì tầng HTTP là thứ sẽ
    -- có tuyến thứ hai, thứ ba — còn bảng thì chỉ có một.
    CONSTRAINT yeu_cau_ghi_chu_co_tran CHECK (ghi_chu IS NULL OR length(ghi_chu) <= 2000),
    CONSTRAINT yeu_cau_quy_mo_co_tran CHECK (quy_mo IS NULL OR length(quy_mo) <= 32),
    CONSTRAINT yeu_cau_quan_tam_co_tran CHECK (
        array_length(quan_tam, 1) IS NULL OR array_length(quan_tam, 1) <= 8
    ),

    -- nguon_chien_dich: mã, không phải câu. Bộ ký tự hẹp vì giá trị này đi vào
    -- báo cáo hiệu quả chiến dịch và có ngày sẽ được ghép vào một truy vấn ở
    -- một công cụ nào đó không ai kiểm soát được.
    CONSTRAINT yeu_cau_nguon_dinh_dang CHECK (
        nguon_chien_dich IS NULL OR nguon_chien_dich ~ '^[a-z0-9][a-z0-9_-]{0,63}$'
    ),

    -- Đã ẩn danh thì ghi_chu phải RỖNG. Ràng buộc này biến "đã dọn" từ một lời
    -- hứa của mã nguồn thành một bất biến của CSDL: một lệnh dọn viết sai chỉ
    -- đánh dấu mà quên xoá chữ sẽ bị từ chối ngay, thay vì chạy xanh và để lại
    -- nguyên văn dữ liệu cá nhân dưới một cái cờ nói rằng đã xoá.
    CONSTRAINT yeu_cau_an_danh_thi_sach CHECK (an_danh_luc IS NULL OR ghi_chu IS NULL)
);

-- Màn "Yêu cầu của tôi": của CHÍNH người đang đăng nhập, mới nhất trước.
CREATE INDEX IF NOT EXISTS yeu_cau_theo_nguoi_dung
    ON yeu_cau (nguoi_dung_id, tao_luc DESC);

-- Trần chống lạm dụng của tuyến gọi lại đếm trên đúng chỉ số này (mục 2), và
-- lệnh dọn 24 tháng quét trên nó (mục 4).
CREATE INDEX IF NOT EXISTS yeu_cau_theo_loai_thoi_gian
    ON yeu_cau (loai, tao_luc DESC);

-- ---------------------------------------------------------------------------
-- 2. TRẦN CỦA TUYẾN GỌI LẠI — vì sao nó là CỬA SỔ TRƯỢT, không phải ngày lịch
--
-- Chủ sản phẩm chốt 22/09/2026: gọi lại phục vụ 24/7, trần 3 lượt. "3 lượt mỗi
-- NGÀY" đọc theo ngày lịch thì cho phép 3 lượt lúc 23:58 và 3 lượt nữa lúc
-- 00:01 — sáu cuộc gọi ra trong ba phút, đúng vào khung giờ mà một cuộc gọi tự
-- động gây khó chịu nhất, và tất cả đều "trong trần". Cửa sổ trượt 24 giờ không
-- có đường ấy.
--
-- Trần được cưỡng chế ở tầng ứng dụng (internal/yeucau) chứ không bằng một
-- ràng buộc CSDL: đếm trong một cửa sổ trượt là một truy vấn, không phải một
-- CHECK. Chỉ số `yeu_cau_theo_loai_thoi_gian` ở trên tồn tại cho đúng truy vấn ấy.
--
-- ⚠ SỐ ĐỂ QUAY RA LẤY TỪ PHIÊN ĐĂNG NHẬP, KHÔNG BAO GIỜ TỪ THÂN YÊU CẦU. Không
-- có cột nào trong lược đồ này nhận một số điện thoại do client gửi lên, và đó
-- là điều kiện để tổng đài của ViHAT không trở thành công cụ quấy rối thuê:
-- người bấm nút chỉ gọi được ra đúng máy mà chính họ đã xác thực với Zalo.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 3. yeu_cau_lich_su — mỗi lần đổi trạng thái, một dòng. CHỈ GHI THÊM.
--
-- Vì sao một bảng riêng chứ không phải một cột `cap_nhat_luc` là đủ: câu người
-- ta hỏi khi có tranh chấp không bao giờ là "bây giờ phiếu ở trạng thái nào",
-- mà là "ai đổi nó, lúc nào, từ gì sang gì". Một cột thì trả lời được câu thứ
-- nhất và xoá mất câu thứ hai.
--
-- Dùng lại nguyên hàm trigger của 0001/0002 — nó đã in TG_TABLE_NAME nên câu
-- báo lỗi tự nói đúng bảng vừa bị từ chối.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS yeu_cau_lich_su (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    yeu_cau_id  uuid        NOT NULL REFERENCES yeu_cau (id),
    tu_trang_thai text,
    den_trang_thai text      NOT NULL,

    -- boi: 'he_thong' cho việc do máy làm, hoặc mã nhân sự của người bấm.
    -- "Có người ký" nghĩa là chỗ này không được để trống.
    boi         text        NOT NULL,

    -- ly_do: MÃ NGẮN do ứng dụng đặt. KHÔNG phải câu văn, và tuyệt đối không
    -- chép nội dung ghi_chu sang đây — đó là cách dữ liệu cá nhân sống sót qua
    -- lệnh ẩn danh hoá.
    ly_do       text,
    tao_luc     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT lich_su_boi_khong_rong CHECK (btrim(boi) <> ''),
    CONSTRAINT lich_su_ly_do_co_tran CHECK (ly_do IS NULL OR length(ly_do) <= 64)
);

CREATE INDEX IF NOT EXISTS lich_su_theo_yeu_cau ON yeu_cau_lich_su (yeu_cau_id, tao_luc);

DROP TRIGGER IF EXISTS lich_su_chan_sua_xoa ON yeu_cau_lich_su;
CREATE TRIGGER lich_su_chan_sua_xoa
    BEFORE UPDATE OR DELETE ON yeu_cau_lich_su
    FOR EACH ROW EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

DROP TRIGGER IF EXISTS lich_su_chan_truncate ON yeu_cau_lich_su;
CREATE TRIGGER lich_su_chan_truncate
    BEFORE TRUNCATE ON yeu_cau_lich_su
    FOR EACH STATEMENT EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

-- ---------------------------------------------------------------------------
-- 4. zns_da_gui — vết mỗi lượt gửi ZNS. CHỈ GHI THÊM.
--
-- BA VIỆC, VÀ CẢ BA ĐỀU LÀ LÝ DO BẢNG NÀY TỒN TẠI:
--
--   1. KHỬ TRÙNG LẶP. Chủ sản phẩm chốt 22/09/2026: ZNS có làm, NHƯNG CÓ TRẦN —
--      tránh làm phiền và tránh tốn. Chỉ số duy nhất từng phần dưới đây cưỡng
--      chế ĐÚNG MỘT tin thành công cho mỗi yêu cầu, ở tầng CSDL. Cưỡng chế ở
--      tầng ứng dụng thì hai tiến trình chạy song song (một lần bấm hai lần,
--      hoặc một lần thử lại của hạ tầng) vẫn lọt được hai tin.
--   2. TRẦN NGÀY THEO NGƯỜI. Đếm trên chỉ số theo người + thời gian.
--   3. CHI PHÍ SOÁT ĐƯỢC. ZNS là tin NHẮN TRẢ TIỀN. Một vòng lặp hỏng gửi vài
--      nghìn tin thì thứ đầu tiên người ta hỏi là "đã gửi bao nhiêu, cho ai,
--      lúc nào". Không có bảng này thì câu trả lời nằm ở hoá đơn của Zalo, một
--      tháng sau.
--
-- KHÔNG CHỨA SỐ ĐIỆN THOẠI và không chứa nội dung tin: tin ZNS dựng từ mẫu đã
-- duyệt cộng mã yêu cầu, nên hai thứ ấy tra lại được từ yeu_cau_id.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zns_da_gui (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    yeu_cau_id    uuid        NOT NULL REFERENCES yeu_cau (id),
    nguoi_dung_id uuid        NOT NULL REFERENCES nguoi_dung (id),

    -- ma_mau: mã mẫu ZNS đã được Zalo duyệt. Ghi lại vì một mẫu bị đổi hay bị
    -- thu hồi là câu hỏi "những tin đã gửi dùng mẫu nào".
    ma_mau        text        NOT NULL,
    ket_qua       text        NOT NULL,

    -- ly_do: MÃ NGẮN của ứng dụng khi hỏng. Không phải thông điệp của Zalo —
    -- thông điệp ấy có thể chép lại nguyên văn thứ ta vừa gửi lên.
    ly_do         text,
    tao_luc       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT zns_ket_qua_hop_le CHECK (ket_qua IN ('thanh_cong', 'that_bai', 'bo_qua_vuot_tran')),
    CONSTRAINT zns_ma_mau_khong_rong CHECK (btrim(ma_mau) <> '')
);

-- ĐÚNG MỘT TIN THÀNH CÔNG CHO MỖI YÊU CẦU — cưỡng chế ở CSDL, xem mục 1 trên.
-- Chỉ số TỪNG PHẦN: lần gửi hỏng vẫn được ghi lại (và được thử lại), chỉ lần
-- thành công là duy nhất.
CREATE UNIQUE INDEX IF NOT EXISTS zns_mot_tin_moi_yeu_cau
    ON zns_da_gui (yeu_cau_id) WHERE ket_qua = 'thanh_cong';

-- Trần ngày theo người đếm trên chỉ số này.
CREATE INDEX IF NOT EXISTS zns_theo_nguoi_thoi_gian
    ON zns_da_gui (nguoi_dung_id, tao_luc DESC);

DROP TRIGGER IF EXISTS zns_chan_sua_xoa ON zns_da_gui;
CREATE TRIGGER zns_chan_sua_xoa
    BEFORE UPDATE OR DELETE ON zns_da_gui
    FOR EACH ROW EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

DROP TRIGGER IF EXISTS zns_chan_truncate ON zns_da_gui;
CREATE TRIGGER zns_chan_truncate
    BEFORE TRUNCATE ON zns_da_gui
    FOR EACH STATEMENT EXECUTE FUNCTION nhat_ky_chi_duoc_ghi_them();

-- ---------------------------------------------------------------------------
-- 5. HẠN LƯU 24 THÁNG — ẨN DANH HOÁ, KHÔNG XOÁ HÀNG
--
-- Cùng một nguyên tắc 0002 đã đặt cho yêu cầu xoá theo Nghị định 13: XOÁ NGHĨA
-- LÀ ẨN DANH HOÁ. Thứ biến mất là phần nhận ra được một con người — ở bảng này
-- là ô ghi chú tự do. Thứ ở lại là sự kiện đã xảy ra: có một yêu cầu loại này,
-- quan tâm sản phẩm này, đến từ chiến dịch này, ngày này. Thống kê hiệu quả
-- chiến dịch vì thế không vỡ khi dữ liệu tới hạn.
--
-- ⚠ HÀM NÀY KHÔNG TỰ CHẠY. Nó phải được gọi hằng ngày — CronJob, cùng chỗ với
-- `make don-nhat-ky` của 0002. Một hàm dọn không ai gọi là một hàm làm người
-- đọc lược đồ tin rằng dữ liệu đang được dọn.
--
-- Trả về số hàng vừa ẩn danh, để CronJob ghi lại được và để cảnh báo nổ khi con
-- số đột ngột nhảy.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION an_danh_yeu_cau_qua_han(gioi_han interval DEFAULT interval '24 months')
RETURNS bigint
LANGUAGE plpgsql AS $$
DECLARE
    so_hang bigint;
BEGIN
    UPDATE yeu_cau
       SET ghi_chu     = NULL,
           an_danh_luc = now()
     WHERE an_danh_luc IS NULL
       AND tao_luc < now() - gioi_han;
    GET DIAGNOSTICS so_hang = ROW_COUNT;
    RETURN so_hang;
END;
$$;

COMMENT ON FUNCTION an_danh_yeu_cau_qua_han(interval) IS
    'An danh hoa yeu_cau qua han luu (mac dinh 24 thang): xoa ghi_chu, giu hang. Phai duoc goi hang ngay.';

COMMIT;
