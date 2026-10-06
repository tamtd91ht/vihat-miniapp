// Command server — tiến trình phục vụ API của Mini App giới thiệu doanh nghiệp.
//
// Tệp này chỉ LẮP RÁP: đọc cấu hình, mở kho, dựng client Zalo, nối vào bộ định
// tuyến, tắt êm. Không có một dòng nghiệp vụ nào ở đây — nghiệp vụ nằm trong
// internal/, nơi test với tới được.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vihat/vihat-miniapp/internal/config"
	"github.com/vihat/vihat-miniapp/internal/httpapi"
	"github.com/vihat/vihat-miniapp/internal/store"
	"github.com/vihat/vihat-miniapp/internal/tongdai"
	"github.com/vihat/vihat-miniapp/internal/vigovcau"
	"github.com/vihat/vihat-miniapp/internal/webhook"
	"github.com/vihat/vihat-miniapp/internal/yeucau"
	"github.com/vihat/vihat-miniapp/internal/zalo"
	"github.com/vihat/vihat-miniapp/internal/zns"
)

const (
	// Thời hạn đọc/ghi của máy chủ HTTP. Không đặt thì một kết nối treo giữ chỗ
	// vô hạn, và đủ nhiều kết nối như thế là dịch vụ ngừng phục vụ.
	thoiHanDoc  = 10 * time.Second
	thoiHanGhi  = 15 * time.Second
	thoiHanRanh = 60 * time.Second

	// Thời gian chờ các yêu cầu đang dở chạy nốt khi nhận tín hiệu dừng.
	thoiHanTatEm = 15 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := chay(log); err != nil {
		// Một dòng, nói rõ việc gì hỏng, rồi thoát khác 0 để bộ điều phối biết.
		log.Error("dịch vụ dừng", "loi", err.Error())
		os.Exit(1)
	}
}

func chay(log *slog.Logger) error {
	// HỎNG THÌ ĐÓNG: thiếu biến bắt buộc là không khởi động, và thông điệp nêu
	// ĐÍCH DANH mọi biến còn thiếu trong một lần. Không có giá trị nào được in ra.
	cfg, err := config.NapTuMoiTruong()
	if err != nil {
		return err
	}

	// app id KHÔNG phải bí mật, và in nó ra là có ích: người vận hành nhìn một
	// dòng log là biết tiến trình này đang phục vụ Mini App nào. Secret key thì
	// không bao giờ — kiểu secret.Secret chặn sẵn mọi đường in.
	log.Info("khởi động", "zalo_app_id", cfg.ZaloAppID, "listen_addr", cfg.ListenAddr,
		"so_origin_cors", len(cfg.CORSAllowedOrigins), "ttl_phien", config.TTLPhien.String())

	ctxMo, huyMo := context.WithTimeout(context.Background(), 10*time.Second)
	defer huyMo()

	kho, err := store.Mo(ctxMo, cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	defer kho.Dong()

	// Hai bộ điều hợp của bên thứ ba. CẢ HAI ĐƯỢC PHÉP nil — `Moi` của chúng trả
	// nil khi thiếu cấu hình, và `yeucau.DichVu` tắt đúng tính năng tương ứng.
	// Thiếu mẫu ZNS không được chặn một yêu cầu tư vấn; thiếu tổng đài không
	// được chặn cả dịch vụ.
	boGuiZNS := zns.Moi("", cfg.ZNSAccessToken, cfg.ZNSTemplateID)
	congTongDai := tongdai.Moi(cfg.TongDaiCallbackURL, cfg.TongDaiAPIKey)

	// In ra TRẠNG THÁI BẬT/TẮT, không in giá trị. Người vận hành mở log lúc khởi
	// động là biết ngay hai tính năng ấy có sống hay không — thay vì phát hiện
	// ra từ một người dùng nói "tôi không nhận được tin nhắn nào".
	log.Info("bề mặt yêu cầu",
		"zns", boGuiZNS != nil,
		"tong_dai", congTongDai != nil,
		"tran_goi_lai", yeucau.TranGoiLai,
		"cua_so_goi_lai", yeucau.CuaSoGoiLai.String())

	// ⚠ nil-INTERFACE LÀ MỘT CÁI BẪY CÓ THẬT Ở ĐÚNG CHỖ NÀY, và sáu dòng dưới
	// tồn tại vì nó: gán một con trỏ nil vào một tham số kiểu GIAO DIỆN cho ra
	// một giao diện KHÁC nil (nó mang kiểu, chỉ giá trị bên trong là nil). Khi
	// ấy `d.zns == nil` trong `yeucau` là false, tính năng lẽ ra "tắt" lại chạy,
	// và nó hỏng ở lời gọi đầu tiên bằng một nil pointer dereference — giữa một
	// tuyến đang phục vụ một người dùng thật.
	var guiZNS yeucau.BoGuiZNS
	if boGuiZNS != nil {
		guiZNS = boGuiZNS
	}
	var goiRa yeucau.TongDai
	if congTongDai != nil {
		goiRa = congTongDai
	}

	// Webhook "yêu cầu mới" — cùng cái bẫy nil-interface ở trên. config.Nap đã
	// từ chối nửa cấu hình và URL không phải https, nên ở đây nil chỉ còn nghĩa
	// "tắt". In BẬT/TẮT, không in URL (có thể mang token ở chuỗi truy vấn).
	var baoWebhook yeucau.BaoWebhook
	if wh := webhook.Moi(cfg.WebhookURL, cfg.WebhookKhoa); wh != nil {
		baoWebhook = wh
	}
	log.Info("webhook yêu cầu mới", "bat", baoWebhook != nil)

	dvYeuCau := yeucau.Moi(kho, goiRa, guiZNS, log).VoiWebhook(baoWebhook)

	// MỘT client Zalo cho app CHUNG, dùng cho cả đổi số điện thoại lẫn đổi vị
	// trí: cùng secret, cùng lời gọi (internal/zalo, goiThongTin).
	clientZalo := zalo.New("", cfg.ZaloSecretKey)
	api := httpapi.Moi(kho, clientZalo, cfg, log).
		VoiYeuCau(kho, dvYeuCau).
		VoiViTri(clientZalo)

	// App RIÊNG của xã: mỗi app một client mang secret của chính nó. In SỐ app
	// và App ID (không phải bí mật — config đã kiểm khuôn chữ số để một cặp gõ
	// ngược không đưa secret vào dòng này), không bao giờ in secret.
	appXa := make(map[string]httpapi.ZaloCuaApp, len(cfg.AppXa))
	dsAppID := make([]string, 0, len(cfg.AppXa))
	for _, a := range cfg.AppXa {
		appXa[a.AppID] = zalo.New("", a.SecretKey)
		dsAppID = append(dsAppID, a.AppID)
	}
	api.VoiAppXa(appXa)
	log.Info("app riêng của xã", "so_app", len(dsAppID), "app_id", dsAppID)

	// Cầu phiên ViGov — chỉ khi cả hai biến có (config đã từ chối nửa cấu hình,
	// và app riêng khi cầu tắt). In BẬT/TẮT và SỐ địa chỉ, không bao giờ in khoá.
	log.Info("cầu phiên ViGov", "bat", cfg.CauPhienBat(), "so_dia_chi", len(cfg.VigovCauDiaChi))
	if cfg.CauPhienBat() {
		cau, err := vigovcau.Mo(cfg.VigovCauDiaChi, cfg.VigovCauKhoa)
		if err != nil {
			return err
		}
		defer func() { _ = cau.Dong() }()
		// Mã tài khoản Zalo: GET /v2.0/me?fields=id, không secret — lời gọi ấy
		// như nhau cho mọi app, nên client app chung đủ dùng. ⚠ Hình dạng lấy từ
		// bản tham chiếu, CHƯA đo từ kho này; và việc xác minh App ID của app
		// riêng dựa trên giả định Zalo từ chối secret sai app (ADR 0045 UNKNOWN
		// #1, chưa đo). Nói ra ở log khởi động, không để người dùng phát hiện.
		log.Warn("cầu phiên ViGov BẬT — mã tài khoản Zalo lấy qua /v2.0/me?fields=id (hình dạng CHƯA đo với Zalo thật); " +
			"app riêng xác minh App ID bằng lượt đổi phoneToken với secret của app ấy (UNKNOWN #1 CHƯA đo) — xem internal/httpapi/app_zalo.go")
		api.VoiCauPhienViGov(cau, clientZalo, cfg.ZaloAppID)
	}

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      api.Handler(),
		ReadTimeout:  thoiHanDoc,
		WriteTimeout: thoiHanGhi,
		IdleTimeout:  thoiHanRanh,
	}

	// Nhận SIGINT/SIGTERM thì ngừng nhận yêu cầu mới và chờ việc đang dở.
	// Cắt ngang giữa một lần đăng nhập nghĩa là người dùng mất phiên vừa mở,
	// còn nhật ký thì đã ghi — một trạng thái không ai giải thích được.
	ctx, dung := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer dung()

	loiChay := make(chan error, 1)
	go func() {
		log.Info("bắt đầu nhận yêu cầu", "listen_addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			loiChay <- err
			return
		}
		loiChay <- nil
	}()

	select {
	case err := <-loiChay:
		return err
	case <-ctx.Done():
		log.Info("nhận tín hiệu dừng, đang tắt êm")
	}

	ctxTat, huyTat := context.WithTimeout(context.Background(), thoiHanTatEm)
	defer huyTat()
	if err := srv.Shutdown(ctxTat); err != nil {
		return err
	}
	// Máy chủ đã ngừng nhận yêu cầu mới; chờ các lần báo webhook đang chạy nền
	// xong trong phần thời hạn còn lại — không thì kho đóng (defer) giữa lúc
	// chúng tra số, và sự kiện của những yêu cầu cuối cùng mất trong im lặng.
	if err := dvYeuCau.ChoViecNen(ctxTat); err != nil {
		log.Warn("tắt êm: còn lần báo webhook chưa xong", "loi", err.Error())
	}
	return <-loiChay
}
