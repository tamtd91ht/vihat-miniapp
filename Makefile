# vihat-miniapp — đích duy nhất cần nhớ: `make check`.
#
# `check` phải XANH trước khi nói xong bất cứ việc gì. Nó không gọi mạng, không
# cần CSDL: test chạm CSDL tự SKIP khi thiếu TEST_DATABASE_DSN (xem README).

GO ?= go

.PHONY: check fmt vet test build run tidy migrate

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

## migrate — chạy lược đồ. Cần psql và biến DATABASE_DSN trong môi trường.
## ON_ERROR_STOP=1: hỏng ở câu nào thì dừng ngay, không chạy tiếp nửa lược đồ.
migrate:
	@test -n "$$DATABASE_DSN" || { echo "thiếu biến DATABASE_DSN"; exit 1; }
	psql "$$DATABASE_DSN" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
