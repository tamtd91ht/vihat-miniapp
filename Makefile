# vihat-miniapp — đích duy nhất cần nhớ: `make check`.
#
# `check` phải XANH trước khi nói xong bất cứ việc gì. Nó không gọi mạng, không
# cần CSDL: test chạm CSDL tự SKIP khi thiếu TEST_DATABASE_DSN (xem README).

GO ?= go

.PHONY: check fmt vet test build run tidy migrate don-nhat-ky an-danh

check: fmt vet test

## fmt — CHỈ BÁO, KHÔNG sửa tại chỗ. `go fmt` thì ghi đè tệp, và một đích kiểm
## tra tự sửa cây mã là đích làm cho CI xanh trong khi commit vẫn lệch chuẩn.
## Thông điệp để tiếng Anh: bảng mã console của Windows làm nát tiếng Việt.
fmt:
	@echo ">> gofmt"
	@out="$$(gofmt -l . )"; \
	if [ -n "$$out" ]; then \
		echo "not gofmt-formatted (run: gofmt -w .):"; echo "$$out"; exit 1; \
	fi

vet:
	@echo ">> go vet"
	@$(GO) vet ./...

## -race bắt lỗi tranh chấp trong bộ giới hạn theo IP (map + mutex dùng chung
## giữa các goroutine phục vụ). Không có -race thì loại lỗi đó chỉ lộ ra trên máy
## thật, dưới tải, và không tái hiện được.
test:
	@echo ">> go test"
	@$(GO) test -race -count=1 ./...

build:
	@$(GO) build -o bin/server ./cmd/server

run:
	@$(GO) run ./cmd/server

tidy:
	@$(GO) mod tidy

## migrate — chạy lược đồ theo thứ tự. Cần psql và biến DATABASE_DSN.
## ON_ERROR_STOP=1: hỏng ở câu nào thì dừng ngay, không chạy tiếp nửa lược đồ.
migrate:
	@test -n "$$DATABASE_DSN" || { echo "missing DATABASE_DSN"; exit 1; }
	psql "$$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
	psql "$$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0002_nhat_ky_90_ngay_va_an_danh.sql

## don-nhat-ky — CHẠY HẰNG NGÀY trong cron. KHÔNG phải "hằng tuần cũng được":
## khoảng cách giữa hai lần chạy cộng thẳng vào tuổi của dòng cũ nhất, nên chạy
## hằng tuần biến trần 90 ngày thành 97 — vượt qua chính cam kết đã in trong
## Chính sách riêng tư.
##
## Hai việc, đúng thứ tự đó: tạo trước phân mảnh cho các tuần sắp tới, rồi bỏ
## các phân mảnh đã quá 90 NGÀY. Bỏ bẵng việc tạo thì dòng mới rơi vào phân
## mảnh mặc định — không mất dữ liệu, nhưng phải dọn tay mới tạo lại được.
##
## 90 ngày là mặc định của hàm nhat_ky_don_qua_han trong migrations/0002 —
## NGUỒN DUY NHẤT của con số. Ở đây cố ý gọi không tham số.
don-nhat-ky:
	@test -n "$$DATABASE_DSN" || { echo "missing DATABASE_DSN"; exit 1; }
	psql "$$DATABASE_DSN" -v ON_ERROR_STOP=1 \
		-c "SELECT nhat_ky_tao_phan_manh() AS phan_manh_moi" \
		-c "SELECT nhat_ky_don_qua_han() AS phan_manh_da_xoa"

## an-danh — thực hiện MỘT yêu cầu xoá dữ liệu (Nghị định 13). Chạy tay, có
## người ký. Số điện thoại nhập qua stdin, không qua tham số dòng lệnh.
an-danh:
	@$(GO) run ./cmd/an-danh
