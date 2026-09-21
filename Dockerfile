# syntax=docker/dockerfile:1.7
#
# Ảnh của `vihat-miniapp` — backend đăng nhập một chạm của Zalo Mini App.
#
# Dựng, NGỮ CẢNH LÀ GỐC KHO NÀY (kho này là một module Go duy nhất, không có go.work và không
# `replace` sang kho nào khác — khác hẳn ViGov, nơi mỗi dịch vụ phải lấy ngữ cảnh ở gốc kho vì
# nó dùng chung `core/`):
#
#   DOCKER_BUILDKIT=1 docker build \
#     --build-arg VERSION=$(git rev-parse --short=12 HEAD) \
#     -t <registry>/<project>/vihat-miniapp:<sha> .
#
# CHỈ ĐÓNG GÓI `cmd/server`, KHÔNG đóng `cmd/an-danh` và `cmd/thu-zalo`. Cả hai cái sau là
# lệnh CHẠY TAY, có người ngồi trước màn hình:
#
#   · `an-danh` thực hiện một yêu cầu xoá dữ liệu theo Nghị định 13 — nó phải có người ký,
#     và nhét nó vào ảnh đang phục vụ nghĩa là pod nào cũng mang sẵn một công cụ ẩn danh hoá
#     dữ liệu cá nhân. Không có lý do nào để nó nằm đó.
#   · `thu-zalo` gọi thật `graph.zalo.me` bằng cặp token sống ~2 phút do người đang cầm điện
#     thoại nhập vào. Nó vô nghĩa nếu không có người.
#
# Việc SQL trên cụm cũng không đi qua ảnh này: `deploy/cronjob-don-nhat-ky.yaml` dùng
# `postgres:16-alpine` + psql. Ảnh này chỉ có đúng một việc — phục vụ HTTP.

# 1.26 chứ không phải 1.25 như dòng `go` trong go.mod: dòng ấy là SÀN (phiên bản tối thiểu
# biên dịch được), không phải phiên bản dùng để dựng. Ghim sàn ở đây thì ngày ai đó viết một
# dòng dùng tính năng 1.26 — thứ máy của họ có — ảnh sẽ đỏ trong khi máy họ xanh, và lỗi hiện
# ra ở CI chứ không ở chỗ gây ra nó. Ghim đúng toolchain đang dùng thì không có khoảng lệch ấy.
ARG GO_VERSION=1.26
ARG RUNTIME=gcr.io/distroless/static-debian12:nonroot   # không shell, không root

# ---------------------------------------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS build
ARG VERSION=dev
WORKDIR /src

# Chép manifest trước mã nguồn: tầng tải phụ thuộc chỉ đổi khi go.mod/go.sum đổi, nên một lần
# sửa mã không kéo theo một lần tải lại toàn bộ module.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0: nhị phân tĩnh, chạy được trên ảnh nền không libc. `-race` là thứ duy nhất
# cần cgo, và nó thuộc cổng kiểm (`make check`) chứ không thuộc ảnh phát hành.
# -trimpath: bỏ đường dẫn tuyệt đối của máy build khỏi stack trace.
ENV CGO_ENABLED=0 GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

# ---------------------------------------------------------------------------------------
FROM ${RUNTIME}
ARG VERSION=dev

# MÚI GIỜ. `distroless/static` không có `/usr/share/zoneinfo`, nên mọi `time.LoadLocation`
# trả lỗi và giờ rơi về UTC — lệch 7 tiếng.
#
# Ở kho này nó không phải chuyện thẩm mỹ: `nhat_ky_dang_nhap` ghi mốc thời gian của từng lượt
# đăng nhập, và trần giữ nhật ký 90 ngày là một con số ĐÃ IN trong Chính sách riêng tư gửi tới
# người dùng. Một cột thời gian lệch múi giờ làm sai chính cam kết ấy, và nó sai lặng lẽ.
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo

# CHỨNG CHỈ GỐC — bắt buộc ở kho này, và đây là khác biệt lớn nhất so với ảnh của ViGov.
#
# `distroless/static` không mang CA nào. Dịch vụ này gọi RA Internet bằng HTTPS
# (`graph.zalo.me`, xem internal/zalo/client.go) để đổi token lấy số điện thoại. Thiếu CA thì
# mọi lượt đổi token hỏng với `x509: certificate signed by unknown authority` — tức KHÔNG AI
# ĐĂNG NHẬP ĐƯỢC, trên môi trường thật, vì một dòng thiếu trong Dockerfile.
#
# ViGov không cần dòng này vì các dịch vụ của nó chỉ nói chuyện trong cụm; chép ảnh của nó
# sang đây mà bỏ dòng này là đúng cái bẫy.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /out/server /server

# 8080 — HTTP. Trùng với `LISTEN_ADDR` mặc định trong deploy/configmap.example.yaml và với
# `containerPort: http` trong deploy/deployment.yaml. EXPOSE chỉ là tài liệu; thứ chặn thật là
# Service kiểu ClusterIP cộng NetworkPolicy.
EXPOSE 8080

# distroless:nonroot chạy UID 65532. Khai lại để đọc được từ chính tệp này, và để khớp
# `runAsUser` trong deployment.yaml.
USER 65532:65532

# KHÔNG HEALTHCHECK: ảnh không có shell lẫn curl. Dịch vụ phục vụ `GET /healthz` (có chạm
# CSDL), và probe phía k8s dùng httpGet — xem deploy/deployment.yaml.

LABEL org.opencontainers.image.title="vihat-miniapp" \
      org.opencontainers.image.description="Backend đăng nhập một chạm cho Zalo Mini App" \
      org.opencontainers.image.revision="${VERSION}" \
      org.opencontainers.image.source="https://github.com/tamtd91ht/vihat-miniapp" \
      org.opencontainers.image.vendor="ViHAT Group"

ENTRYPOINT ["/server"]
