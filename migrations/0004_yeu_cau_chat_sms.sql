-- 0004_yeu_cau_chat_sms.sql — ba loại yêu cầu mới (chat · nhận ưu đãi SMS · huỷ
-- ưu đãi SMS) và cột tên hiển thị Zalo cho loại chat.
--
-- Chạy: psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0004_yeu_cau_chat_sms.sql
-- Toàn bộ trong một giao dịch: hỏng giữa chừng thì không để lại nửa lược đồ.
--
-- ===========================================================================
-- QUYẾT ĐỊNH CỦA CHỦ SẢN PHẨM, 07/10/2026 — Mini App có ba nút mới, cả ba là
-- "bấm rồi thôi" (không biểu mẫu), và cả ba đi qua CÙNG bề mặt /api/v1/requests:
--
--   chat             người dùng bấm "Chat với chuyên viên"
--   nhan_uu_dai_sms  người dùng đăng ký nhận SMS ưu đãi
--   huy_uu_dai_sms   người dùng huỷ nhận SMS ưu đãi
--
-- Vì sao là HÀNG MỚI trong yeu_cau chứ không phải một cờ trên nguoi_dung: "huỷ
-- nhận SMS" là một lời ĐỒNG Ý / RÚT ĐỒNG Ý, và câu người ta hỏi về nó luôn là
-- "người này rút lúc nào, bằng nút nào" — một cờ chỉ trả lời "bây giờ ra sao" và
-- xoá mất lần bấm trước. Hàng mới thì giữ cả chuỗi, kèm lịch sử trạng thái mà
-- 0003 đã dựng sẵn cho mọi yêu cầu.
--
-- ⚠ LƯỢC ĐỒ KHÔNG SUY RA "TRẠNG THÁI ĐĂNG KÝ HIỆN TẠI". Bên nhận webhook (xem
-- README, REQUEST_WEBHOOK_URL) là bên gửi SMS và là bên giữ danh sách; ở đây chỉ
-- là bằng chứng từng lần bấm.
--
-- LÙI LẠI: không cần câu lệnh lùi nào, và đó là chủ đích. Mọi thay đổi dưới đây
-- là NỚI hoặc THÊM: một cột NULL mới, một CHECK rộng hơn, một bất biến ẩn danh
-- chặt hơn, và hàm dọn được thay bằng bản dọn thêm cột mới. Bản ứng dụng CŨ chạy
-- được trên lược đồ MỚI (nó không ghi cột mới, không gửi loại mới). Không bỏ cột
-- nào, không đổi kiểu cột nào — nên không có dữ liệu nào mất khi chạy, cũng không
-- có dữ liệu nào phải cứu khi lùi bản ứng dụng.
-- Nếu buộc phải thu CHECK về hai loại cũ: chỉ làm được khi không còn hàng nào
-- mang ba loại mới — và những hàng ấy là bằng chứng đồng ý, KHÔNG được xoá để
-- làm việc đó.
-- ===========================================================================

BEGIN;

DO $$
BEGIN
    IF to_regclass('yeu_cau') IS NULL THEN
        RAISE EXCEPTION '0004 can 0003 chay truoc: chua co bang yeu_cau';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 1. ten_hien_thi — TÊN HIỂN THỊ ZALO, chỉ cho loại 'chat'.
--
-- Đây là DỮ LIỆU CÁ NHÂN (một cái tên người ta tự đặt, thường là tên thật), và
-- nó có mặt chỉ vì chuyên viên nhận cuộc chat cần biết đang nói với ai. Nó do
-- client gửi lên — tức là người dùng TỰ KHAI, không phải định danh: định danh
-- vẫn là nguoi_dung_id của phiên.
--
-- NULL = không có. Chuỗi rỗng bị cấm để "không có" chỉ có MỘT cách viết.
-- ---------------------------------------------------------------------------
ALTER TABLE yeu_cau ADD COLUMN IF NOT EXISTS ten_hien_thi text;

-- Trần 100 KÝ TỰ (length() của Postgres đếm ký tự, không đếm byte) — cùng con số
-- tầng HTTP đếm theo rune. Trần ở CSDL vì cùng lý do 0003: tuyến thứ hai sẽ có.
ALTER TABLE yeu_cau DROP CONSTRAINT IF EXISTS yeu_cau_ten_hien_thi_co_tran;
ALTER TABLE yeu_cau ADD CONSTRAINT yeu_cau_ten_hien_thi_co_tran CHECK (
    ten_hien_thi IS NULL OR (length(ten_hien_thi) <= 100 AND btrim(ten_hien_thi) <> '')
);

-- Chỉ loại 'chat' được mang tên. Một tên nằm trên phiếu "huỷ nhận SMS" là dữ liệu
-- cá nhân không việc gì cần tới — và dữ liệu không cần thì không được giữ.
ALTER TABLE yeu_cau DROP CONSTRAINT IF EXISTS yeu_cau_ten_hien_thi_chi_cho_chat;
ALTER TABLE yeu_cau ADD CONSTRAINT yeu_cau_ten_hien_thi_chi_cho_chat CHECK (
    ten_hien_thi IS NULL OR loai = 'chat'
);

-- ---------------------------------------------------------------------------
-- 2. Ba loại mới. NỚI một CHECK: mọi hàng đang có (tu_van / goi_lai) vẫn thoả.
-- Phải trùng khít các hằng Loai* trong internal/yeucau.
-- ---------------------------------------------------------------------------
ALTER TABLE yeu_cau DROP CONSTRAINT IF EXISTS yeu_cau_loai_hop_le;
ALTER TABLE yeu_cau ADD CONSTRAINT yeu_cau_loai_hop_le CHECK (
    loai IN ('tu_van', 'goi_lai', 'chat', 'nhan_uu_dai_sms', 'huy_uu_dai_sms')
);

-- ---------------------------------------------------------------------------
-- 3. Bất biến ẩn danh của 0003, mở rộng: đã ẩn danh thì CẢ ghi_chu LẪN
-- ten_hien_thi phải rỗng. Một lệnh dọn chỉ xoá ghi chú mà quên tên sẽ bị CSDL
-- từ chối, thay vì chạy xanh và để lại tên người dưới một cái cờ nói đã xoá.
-- Hàng đang có đều thoả (cột mới toàn NULL).
-- ---------------------------------------------------------------------------
ALTER TABLE yeu_cau DROP CONSTRAINT IF EXISTS yeu_cau_an_danh_thi_sach;
ALTER TABLE yeu_cau ADD CONSTRAINT yeu_cau_an_danh_thi_sach CHECK (
    an_danh_luc IS NULL OR (ghi_chu IS NULL AND ten_hien_thi IS NULL)
);

-- ---------------------------------------------------------------------------
-- 4. Hàm dọn 24 tháng của 0003 — thay bằng bản xoá thêm ten_hien_thi.
--
-- BẮT BUỘC đi cùng mục 3, trong cùng giao dịch: bản cũ chỉ xoá ghi_chu, nên sau
-- mục 3 nó sẽ bị CHECK từ chối ngay ở hàng chat đầu tiên tới hạn. Cùng chữ ký
-- và mặc định với bản cũ — bên gọi (CronJob, khi có) không phải đổi gì.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION an_danh_yeu_cau_qua_han(gioi_han interval DEFAULT interval '24 months')
RETURNS bigint
LANGUAGE plpgsql AS $$
DECLARE
    so_hang bigint;
BEGIN
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
    'An danh hoa yeu_cau qua han luu (mac dinh 24 thang): xoa ghi_chu va ten_hien_thi, giu hang. Phai duoc goi hang ngay.';

COMMIT;
