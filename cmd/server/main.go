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
	"github.com/vihat/vihat-miniapp/internal/zalo"
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

	api := httpapi.Moi(kho, zalo.New("", cfg.ZaloSecretKey), cfg, log)

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
	return <-loiChay
}
