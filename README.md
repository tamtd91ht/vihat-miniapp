# vihat-miniapp

Backend cho Zalo Mini App **giới thiệu doanh nghiệp VihatSoftware**.

Kho này **độc lập hoàn toàn** với ViGov: không `replace`, không import, không dùng chung
`go.mod`. Kiểm được:

```
go list -deps ./...     # thư viện chuẩn, module này, pgx (+ x/text, x/sync do pgx kéo),
                        # grpc + protobuf (+ x/net, x/sys, genproto/rpc do grpc kéo)
```

Đổi tên thư mục `vigov-v2` đi thì `go build ./...` vẫn chạy — không có đường dẫn nào trỏ sang đó.

**Một điểm chạm có chủ đích với ViGov: cầu phiên công dân.** Hợp đồng
`proto/vigov/identity/v1/citizen_session_bridge.proto` là **bản chép** từ kho ViGov (bên phục
vụ sở hữu nó), kèm commit nguồn và SHA ở đầu tệp; mã client ở `internal/gen/` sinh từ bản chép
ấy (`make proto`). Chép chứ không import: vẫn không `replace`, không go.mod chung.
`internal/vigovcau/ban_chep_proto_test.go` làm `make check` đỏ khi thân bản chép bị sửa tay.
grpc/protobuf vào `go.mod` chỉ vì cầu này.

Phạm vi bước hiện tại đúng ba việc: **định danh · phiên đăng nhập · nhật ký đăng nhập**.

---

## TRẠNG THÁI

Mã đã đủ để chạy; `make check` xanh. **Chưa từng chạy với Postgres thật.** Đã gọi Zalo
thật đúng **một lần, bằng token GIẢ** (20/09/2026): đủ để xác nhận endpoint và hình dạng
phản hồi, **không** đủ để xác nhận luồng đổi `phoneToken` lấy số — việc ấy cần token thật
từ một chiếc điện thoại thật, nay chỉ còn là một thao tác 30 giây (`make thu-zalo`).
Xem mục **NỢ** cuối tệp trước khi phát hành.

| Đã xong | Nơi |
|---|---|
| Nạp cấu hình, hỏng thì đóng và nêu đích danh tên biến | `internal/config` |
| Kiểu `Secret` chặn mọi đường in ra | `internal/secret` |
| Bộ đổi token Zalo + chuẩn hoá số + phân loại lỗi | `internal/zalo` |
| Sinh bearer token và băm SHA-256 | `internal/phien` |
| Ba bảng, một giao dịch ghi cả ba; ẩn danh hoá cũng một giao dịch | `internal/store` |
| Hai tuyến, CORS, giới hạn theo IP | `internal/httpapi` |
| Lắp ráp, tắt êm, thiếu biến thì không khởi động | `cmd/server` |
| Lệnh chạy tay thực hiện yêu cầu xoá dữ liệu | `cmd/an-danh` |
| Lược đồ, nhật ký chỉ ghi thêm (cưỡng chế bằng trigger) | `migrations/0001_init.sql` |
| Nhật ký phân mảnh theo tuần + dọn 90 ngày, bảng `nhat_ky_an_danh` | `migrations/0002_…sql` |
| **Lệnh gọi THẬT Zalo một lần** — đường thử cho NỢ #1-3 | `cmd/thu-zalo` |
| **Mẫu k8s + bảng ánh xạ biến, CronJob dọn nhật ký hằng ngày** | `deploy/` |
| **Cấu hình máy local**: `.env.local`, biến shell đè tệp | `scripts/voi-env.sh` |
| **Yêu cầu tư vấn / gọi lại**: tuyến cần xác thực, hai trần, vết ZNS | `internal/yeucau`, `internal/httpapi/yeu_cau.go` |
| **Lược đồ yêu cầu + lịch sử chỉ-ghi-thêm + hạn lưu 24 tháng** | `migrations/0003_yeu_cau.sql` |
| **Ba loại chat / nhận / huỷ ưu đãi SMS + tên hiển thị Zalo** (07/10/2026) | `migrations/0004_yeu_cau_chat_sms.sql`, `internal/yeucau` |
| **Webhook "yêu cầu mới"**, ký HMAC-SHA256, chạy nền | `internal/webhook` |
| Bộ điều hợp ZNS và tổng đài — **hình dạng dây CHƯA ĐO**, xem NỢ | `internal/zns`, `internal/tongdai` |

---

## Hợp đồng API

Phía Mini App đã viết theo khuôn này; giữ đúng.

```
POST /api/v1/sessions          công khai, không cần xác thực
  gửi: {"accessToken": "<getAccessToken()>", "phoneToken": "<token của getPhoneNumber()>",
        "appId": "<App ID của Mini App đang chạy>"}      appId TUỲ CHỌN — vắng = app chung
  201: {"token": "<bearer>", "expiresAt": "<RFC3339>"}

POST /api/v1/location          công khai, cùng lớp chắn với /sessions (xô giới hạn RIÊNG)
  gửi: {"accessToken": "<getAccessToken()>", "locationToken": "<token của getLocation()>",
        "appId": "<App ID>"}                             appId TUỲ CHỌN — vắng = app chung
  200: {"latitude": <số>, "longitude": <số>}      KHÔNG lưu gì — xem bảng lỗi bên dưới

GET  /healthz                  công khai, cho thăm dò sức khoẻ
  200: {"trang_thai": "ok"}        503 khi không chạm được CSDL

POST /api/v1/requests          CẦN XÁC THỰC — Authorization: Bearer <token của /sessions>
  gửi: {"kind": "consult" | "callback" | "chat" | "sms_promo" | "sms_optout",
        "interests": ["omicall", ...],   tối đa 8 mã, khuôn ^[a-z0-9][a-z0-9_-]{0,63}$
        "scale": "<mã>", "source": "<mã chiến dịch>",
        "note": "<tối đa 2000 KÝ TỰ>",
        "displayName": "<tên hiển thị Zalo>"}   CHỈ với kind=chat, tối đa 100 KÝ TỰ, tuỳ chọn
  201: {"requestId": "<uuid>", "kind": "...", "status": "moi", "createdAt": "<RFC3339>"}

GET  /api/v1/requests          CẦN XÁC THỰC — CHỈ yêu cầu của chính người đăng nhập
  200: {"items": [{"requestId", "kind", "status", "createdAt"}]}   rỗng là [], không phải null
```

⚠ **Thân yêu cầu KHÔNG có, và sẽ không bao giờ có, một trường nói "tôi là ai".** Mã định danh
đến từ phiên đăng nhập. `internal/httpapi/yeu_cau_test.go` có một ca nhồi đủ
`nguoi_dung_id` / `userId` / `nguoiDungId` / `phone` vào thân và khẳng định cả bốn bị bỏ qua.

⚠ **`status` trả về là MÃ, không phải câu chữ** (`moi` · `dang_xu_ly` · `da_lien_he` · `dong`).
Câu tiếng Việt hiện cho người dùng sống ở Mini App: đổi một nhãn trên màn hình không được phép
là một lần phát hành lại máy chủ.

| `kind` | Mã cột `yeu_cau.loai` | Hệ quả ở máy chủ này |
|---|---|---|
| `consult` | `tu_van` | ghi phiếu · ZNS xác nhận (nếu cấu hình) · webhook (nếu cấu hình) |
| `callback` | `goi_lai` | trần 3 lượt/24 giờ/người · ghi phiếu · tổng đài quay ra · webhook |
| `chat` | `chat` | ghi phiếu (kèm `displayName` nếu có) · webhook. Bấm "Chat với chuyên viên" |
| `sms_promo` | `nhan_uu_dai_sms` | ghi phiếu · webhook. Đăng ký nhận SMS ưu đãi |
| `sms_optout` | `huy_uu_dai_sms` | ghi phiếu · webhook. Huỷ nhận SMS ưu đãi |

Ba loại cuối (chủ sản phẩm chốt 07/10/2026) là "bấm rồi thôi": **không ZNS** (tin xác nhận cho
lần bấm huỷ SMS là đúng thứ người dùng vừa từ chối), **không trần riêng** (trần IP đủ), và
máy chủ này **không** suy ra "đang đăng ký hay đã huỷ" — việc gửi SMS và giữ danh sách là của
bên nhận webhook; ở đây chỉ có bằng chứng từng lần bấm. Ánh xạ `kind` ↔ mã cột: một bảng duy
nhất, `internal/yeucau` (`LoaiTuKind` / `KindTuLoai`).

| Mã | Khi nào (tuyến `/requests`) | Câu trả về |
|---|---|---|
| 400 | thân không đọc được · `kind` lạ · mã sai khuôn · ghi chú quá 2000 ký tự · `displayName` khác rỗng với `kind` ≠ `chat` · `displayName` quá 100 ký tự | Yêu cầu / thông tin không hợp lệ… |
| 401 | không có bearer, hoặc phiên đã hết hạn / bị thu hồi | Bạn cần đăng nhập để dùng chức năng này… |
| 429 | vượt trần theo IP (20/5 phút) **hoặc** trần gọi lại (3 lượt/24 giờ/người) | hai câu khác nhau, xem `yeu_cau.go` |
| 503 | `kind=callback` mà chưa cấu hình tổng đài, hoặc chưa gọi `VoiYeuCau` | Chức năng … đang tạm ngưng. Vui lòng gọi hotline… |

Lỗi trả về `{"message": "<câu tiếng Việt nói người dùng làm gì tiếp>"}`.

### Webhook "yêu cầu mới" — khi `REQUEST_WEBHOOK_*` được đặt

Sau khi một yêu cầu **đã commit** (mọi `kind`), máy chủ POST một sự kiện sang
`REQUEST_WEBHOOK_URL`. Không cấu hình thì không gửi gì — chỉ ghi CSDL. Mã: `internal/webhook`
(dây), `internal/yeucau` (`baoYeuCauMoi`, khi nào và với dữ liệu gì).

```
POST <REQUEST_WEBHOOK_URL>
  Content-Type: application/json
  X-Vihat-Signature: sha256=<hex HMAC-SHA256(REQUEST_WEBHOOK_SECRET, thân THÔ)>
  {"event": "request.created", "requestId": "<uuid>", "kind": "<tên dây như trên>",
   "createdAt": "<RFC3339>", "phone": "84xxxxxxxxx",
   "displayName": "...",            VẮNG khi rỗng
   "interests": [], "scale": "", "note": "", "source": ""}
  2xx = đã nhận. Mọi mã khác = hỏng.
```

| Điều | Giá trị | Vì sao |
|---|---|---|
| Chạy | **nền**, sau commit | phản hồi 201 cho app **không bao giờ** chờ webhook; webhook hỏng không làm hỏng yêu cầu |
| `phone` | tra từ **phiên** (`nguoi_dung.so_dien_thoai`), không bao giờ từ thân | cùng luật với tổng đài / ZNS — thân yêu cầu không đặt được số |
| Vận chuyển | **chỉ https**, TLS kiểm mặc định, **không theo chuyển hướng** | thân mang số điện thoại, tên, ghi chú |
| Thời hạn | 5 giây/lượt, **tối đa 2 lượt** (thử lại một lần khi lỗi mạng, 5xx, 429; nghỉ 1 giây); không thử lại 4xx khác | không có hàng đợi bền — hai lượt hỏng thì sự kiện **mất**, phiếu vẫn còn trong `yeu_cau` |
| Giao | **ít nhất một lần** | lượt thử lại sau một phản hồi bị mất = sự kiện tới hai lần → bên nhận **khử trùng theo `requestId`** |
| Log | chỉ `ma_yeu_cau` + kết cục (mã HTTP) | **không bao giờ** thân, số, tên, ghi chú |
| Tắt êm | `cmd/server` chờ các lần báo đang chạy trong phần còn lại của 15 giây | sự kiện của các yêu cầu cuối không mất khi rollout |

Bên nhận kiểm chữ ký trên **đúng byte nhận được** (không parse rồi serialize lại), so bằng hàm
so sánh thời gian hằng định. Chữ ký **không** kèm dấu thời gian, nên nó không chống phát lại
— xem NỢ #17.

| Mã | Khi nào | Câu trả về |
|---|---|---|
| 400 | thân yêu cầu không đọc được, thiếu `accessToken`/`phoneToken` | Yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử lại. |
| 401 | Zalo từ chối token (sai hoặc hết hạn) | Phiên Zalo đã hết hạn. Vui lòng đóng và mở lại ứng dụng để đăng nhập lại. |
| 429 | vượt giới hạn theo IP | Bạn thử đăng nhập quá nhiều lần. Vui lòng chờ vài phút rồi thử lại. |
| 502 | không với tới được Zalo, hoặc Zalo trả thứ không hiểu được | Hiện chưa kết nối được tới Zalo. Vui lòng thử lại sau ít phút. |
| 500 | lỗi phía hệ thống | Hệ thống đang bận. Vui lòng thử lại sau ít phút. |

Không bao giờ có mã lỗi kỹ thuật, tên cột, thông điệp của Zalo hay số điện thoại trong thân
phản hồi.

### `POST /api/v1/location` — đổi token của `getLocation()` lấy toạ độ

zmp-sdk `getLocation()` chỉ còn trả một token (toạ độ phía SDK đã bị khai tử). Máy chủ đổi
token ấy bằng **cùng** lời gọi `GET graph.zalo.me/v2.0/me/info` như `phoneToken`, bằng
secret key **của app mà `appId` chỉ tới** (`internal/zalo`, `LayViTri`; chọn app:
`internal/httpapi/app_zalo.go`). **Không lưu, không log** token lẫn toạ độ: hỏi
một người đang đứng đâu không phải là ghi lại nơi họ đứng.

Không đòi phiên của kho này: công dân đi cầu ViGov cầm phiên ViGov, và họ chính là người
cần vị trí cho phản ánh. Lớp chắn giống `/sessions`: token do Zalo cấp + **10 lượt / 5 phút
/ IP, xô riêng** (hết hạn mức vị trí không chặn đăng nhập).

Lỗi của tuyến này mang **thêm** khoá `code` — `{"message": "...", "code": "..."}` — để phía
app chọn nhánh mà không so câu chữ. Các tuyến khác giữ nguyên `{"message"}`.

| Mã | `code` | Khi nào |
|---|---|---|
| 400 | `invalid_request` | thân không đọc được, thiếu `accessToken`/`locationToken` |
| 405 | `method_not_allowed` | không phải POST (`Allow: POST`) |
| 422 | `app_not_configured` | `appId` không phải app chung và không có trong `ZALO_MINIAPP_COMMUNE_APP_SECRETS` |
| 429 | `rate_limited` | vượt 10 lượt / 5 phút / IP |
| 502 | `zalo_location_unavailable` | **mọi** thất bại của Zalo: token bị từ chối, không với tới, trả toạ độ không đọc được hoặc ngoài trái đất |
| 503 | `unavailable` | chưa gọi `VoiViTri` ở `cmd/server` |

Vì sao Zalo từ chối token là **502, không 401** như `/sessions`: ở lượt này `error != 0` không
tách được "access token hết hạn" với "token vị trí hết hạn" (dùng một lần, ~2 phút); 401 bị
phía app đọc thành "đăng nhập lại" — sai với ca hay gặp hơn. Việc người dân làm tiếp là như
nhau: thử lại, hoặc tự gõ địa chỉ.

⚠ Hình dạng `data.latitude` / `data.longitude` **CHƯA đo với Zalo thật** — mức chứng cứ THẤP,
nguồn duy nhất là bản tham chiếu ở kho yêu cầu (xem `internal/zalo/wire.go`, ĐIỀU CHƯA RÕ #5–#6).

### Cầu phiên ViGov — khi `VIGOV_CITIZEN_SESSION_BRIDGE_*` được đặt

Cầu **tắt** (mặc định, hai biến trống): mọi thứ ở trên đứng nguyên; `communeHostHint` và
`communeConfirmed` bị bỏ qua (app riêng thì không có — config từ chối khởi động). Cầu
**bật**: nhánh được chọn **theo từng yêu cầu** (chủ dự án chốt 27/09/2026, nhánh app riêng
chốt 29/09/2026, `internal/httpapi/sessions.go`):

| Thân yêu cầu | Nhánh |
|---|---|
| `appId` là một app **riêng** của xã (`ZALO_MINIAPP_COMMUNE_APP_SECRETS`) | **phiên công dân của ViGov**, gửi **App ID của app riêng** — ViGov tra xã từ App ID (chế độ riêng). **Không cần** `communeHostHint`. `phoneToken` **BẮT BUỘC** (xem "App ID đã xác minh" dưới) |
| `appId` lạ (không phải app chung, không trong danh sách) | **422**, không gọi Zalo, không lùi về app chung |
| app chung (`appId` vắng hoặc = `ZALO_MINIAPP_APP_ID`) + `communeHostHint` **khác rỗng** (so nguyên văn, không trim) | **phiên công dân của ViGov** với App ID app chung (gọi `OpenCitizenSession` trên cổng cầu của `service-identity`); **không** phát phiên của kho này |
| app chung, không có / rỗng `communeHostHint` | **phiên của kho này**, y như cầu tắt — Tư vấn / Yêu cầu của tôi giữ đăng nhập |

Quyết định: ADR 0045 + 0047 ở kho ViGov. Mã: `internal/httpapi/sessions_vigov.go`.

**"App ID đã xác minh" nghĩa là gì ở kho này** — nguồn duy nhất: khối đầu
`internal/httpapi/app_zalo.go`. Tóm một dòng: Zalo không có lời gọi nào trả "token này của
app nào", nên `appId` do client khai **chỉ chọn secret**, và App ID coi là đã xác minh khi
lượt đổi `phoneToken` **bằng secret của chính app ấy thành công**. Điều đó chỉ đúng nếu Zalo
từ chối secret sai app — **chưa đo** (ADR 0045 UNKNOWN #1, NỢ #14).

```
POST /api/v1/sessions          (cầu BẬT, có communeHostHint)
  gửi: {"accessToken": "...",            bắt buộc
        "phoneToken": "...",             TUỲ CHỌN với app chung — chỉ khi công dân gửi thứ gì đó;
                                         BẮT BUỘC với app riêng
        "appId": "...",                  tuỳ chọn — App ID của Mini App đang chạy; vắng = app chung
        "communeHostHint": "xa-a.vigov.vn",  app chung: BẮT BUỘC để đi cầu, NGUYÊN VĂN tham số
                                         của QR; app riêng: không cần (chuyển nguyên văn nếu có)
        "communeConfirmed": true}        tuỳ chọn — công dân đã bấm xác nhận xã
  201: {"vigovSession": {"token": "<bearer ViGov>", "expiresAt": "<RFC3339>",
                          "tenantDisplayName": "<tên xã>", "phoneVerified": true,
                          "communePrimaryHost": "<tên miền chính của xã>"}}
       phiên không xã: không có token/expiresAt, tenantDisplayName = "" → màn giới thiệu
       communePrimaryHost LUÔN có mặt; "" khi không xã hoặc xã chưa có tên miền chính
```

| Mã | Khi nào (cầu bật) |
|---|---|
| 400 | thiếu `accessToken` · app riêng mà thiếu `phoneToken` · ViGov trả `INVALID_ARGUMENT` (tên miền sai khuôn, xác nhận thiếu tên miền) |
| 401 | Zalo từ chối token (ở bước mã tài khoản, hoặc lượt đổi `phoneToken` bằng secret của app) |
| 422 | `appId` lạ · ViGov trả `FAILED_PRECONDITION` — app chưa gắn xã (thiếu dòng `mini_app`), xã ngừng hoạt động, tên miền không thuộc xã nào: **một câu** cho mọi nhánh |
| 502 | không với tới Zalo, hoặc Zalo trả mã tài khoản không đọc được |
| 503 | ViGov không phục vụ được (thử lại một lần khi `ABORTED`, không thử lại mã nào khác) · khoá cầu sai |

Kho này **không biết xã**: tên miền đi nguyên văn, ViGov kiểm và quyết. Token ViGov chuyển
**nguyên**, không lưu, không log; số điện thoại đi thẳng sang ViGov, không vào CSDL của kho này.
Thân 201 dùng khoá `vigovSession` chứ không dùng `token`: hai phiên do hai hệ thống ký không
được trùng tên khoá.

Khoá `message` của thân lỗi **đã được phía Mini App xác nhận** là khoá họ đọc. Năm nhánh
phía app — mã hết hạn (401) · Zalo không trả lời (502) · chưa khai host · không gọi được ·
xong — khớp với bảng trên; hai nhánh "chưa khai host" và "không gọi được" xảy ra trước khi
yêu cầu tới được máy chủ này, nên không có mã HTTP nào ở đây.

---

## Bố cục

```
cmd/server/          nối dây, không chứa nghiệp vụ
cmd/an-danh/         lệnh chạy tay: thực hiện một yêu cầu xoá dữ liệu
cmd/thu-zalo/        lệnh chạy tay: gọi THẬT graph.zalo.me một lần, in chẩn đoán
internal/config/     NƠI DUY NHẤT đọc môi trường
internal/secret/     kiểu Secret — chặn rò bí mật qua log
internal/zalo/       wire.go = toàn bộ giao thức với Zalo, client.go = cách gọi,
                     chandoan.go = đường chẩn đoán (cùng một lời gọi, không số điện thoại)
internal/phien/      token, băm token, từ vựng kết quả đăng nhập
internal/store/      NƠI DUY NHẤT biết SQL
internal/httpapi/    tuyến, CORS, giới hạn theo IP
internal/vigovcau/   client cầu phiên công dân ViGov (gRPC, khoá cầu ở metadata)
internal/gen/        mã SINH từ proto/ — không sửa tay, `make proto`
proto/               hợp đồng CHÉP từ ViGov + cấu hình buf
migrations/          lược đồ, chạy bằng `make migrate`
deploy/              mẫu Kubernetes + BẢNG ÁNH XẠ biến → khoá k8s (deploy/README.md)
scripts/voi-env.sh   nạp .env.local cho các đích `make` chạy tay
```

`internal/httpapi` chứ không phải `internal/http`: một gói tên `http` buộc mọi tệp trong đó
phải đặt bí danh cho `net/http`, và bí danh khác nhau giữa các tệp là cách nhanh nhất để đọc
nhầm gói.

---

## Biến môi trường

Mỗi biến có đúng một dòng trong `.env.example` kèm lý do bắt buộc hay không. Thiếu biến bắt
buộc thì service **không khởi động** và in ra **đích danh** tên biến còn thiếu (tất cả, trong
một lần, không bắt người vận hành khởi động lại năm lượt).

| Biến | Bắt buộc | Vì sao |
|---|---|---|
| `DATABASE_DSN` | có | không có CSDL thì không phục vụ nổi một yêu cầu |
| `LISTEN_ADDR` | không | mặc định `:8080` |
| `ZALO_MINIAPP_APP_ID` | có | một secret không kèm app id thì không ai xoay vòng hay chẩn đoán được; in ra lúc khởi động |
| `ZALO_MINIAPP_SECRET_KEY` | có, **bí mật** | không có thì không đổi được `phoneToken`, tức không ai đăng nhập được |
| `CORS_ALLOWED_ORIGINS` | có | thiếu thì nút đăng nhập chết im lặng trên máy thật |
| `VIGOV_CITIZEN_SESSION_BRIDGE_ADDRESS` | không, **đi cặp** | cổng cầu phiên của identity, danh sách `host:port`; một nửa cặp thì **không khởi động** |
| `VIGOV_CITIZEN_SESSION_BRIDGE_KEY` | không, **đi cặp**, **bí mật** | ≥ 32 byte; **không bao giờ** là `GRPC_CALLER_KEY` của ViGov |
| `ZALO_MINIAPP_COMMUNE_APP_SECRETS` | không, **bí mật** | app **riêng** của xã: `<app_id>=<secret>,<app_id>=<secret>`. Có giá trị mà cầu tắt thì **không khởi động**; App ID phải là chữ số, không trùng app chung, không lặp |
| `REQUEST_WEBHOOK_URL` | không, **đi cặp** | đích nhận sự kiện `request.created`; **chỉ `https://`** tuyệt đối có host, không `user:pass` — sai là **không khởi động**; một nửa cặp là **không khởi động** |
| `REQUEST_WEBHOOK_SECRET` | không, **đi cặp**, **bí mật** | khoá ký HMAC-SHA256, ≥ 32 byte (`openssl rand -hex 32`); bên nhận giữ cùng khoá |
| `TEST_DATABASE_DSN` | không | chỉ cho test chạm CSDL |

Bí mật **không vào mã nguồn, không vào tài liệu, không vào `.env.example`** — mẫu chỉ có
placeholder. `.gitignore` chặn `.env` và `.env.*`, trừ `.env.example`.

### Cấu hình: ba đường vào, một thứ tự

| Ưu tiên | Đường vào | Ở đâu dùng |
|---|---|---|
| 1 | **Biến shell** (`DATABASE_DSN=... make migrate`) | chạy một lần với giá trị khác, không phải sửa tệp |
| 2 | **`.env.local`** — giá trị thật của MỘT máy | máy của người phát triển / người triển khai |
| 3 | không có gì | service **không khởi động**, log nêu **đích danh** mọi biến còn thiếu |

Trên cụm **chỉ có đường 1**: giá trị đến từ Secret/ConfigMap → `env:` của pod.
`.env.local` **không tồn tại ở đó và cũng không được cần tới**.

```
cp .env.example .env.local      # rồi điền ba giá trị thật
chmod 600 .env.local            # tệp này chứa secret key thật và DSN có mật khẩu
```

**MỘT bản kê, không phải hai.** `.env.example` vừa là bản kê mọi biến (kèm lý do bắt
buộc hay không) vừa là tệp để chép ra thành `.env.local`. Cố ý **không** có
`.env.local.example`: hai tệp cùng liệt kê một danh sách là hai tệp sẽ lệch nhau, và
khi lệch thì không ai biết tệp nào đúng. Danh sách bị khoá bằng phép kiểm —
`internal/config/ban_ke_bien_test.go` làm `make check` đỏ khi một biến thiếu dòng
trong `.env.example` hoặc thiếu khoá trong `deploy/*.example.yaml`.

**Ai đọc `.env.local`.** Không phải nhị phân — `internal/config` chỉ đọc môi trường,
đúng một nguồn. Việc dựng môi trường ấy trên máy local là của `scripts/voi-env.sh`,
được các đích `make run · migrate · don-nhat-ky · an-danh · thu-zalo · test-csdl`
gọi. Một nhị phân biết tự đọc tệp cấu hình là một nhị phân có **đường nạp thứ hai
không ai kiểm**, và cái ngày một tệp `.env` lạc vào ảnh container là ngày nó lặng lẽ
đè cấu hình của cụm.

`make check` **cố ý không** nạp `.env.local`: một phép kiểm đổi kết quả theo máy đang
chạy là một phép kiểm không nói được điều gì. Không đích nào in **giá trị** biến ra
màn hình — chỉ in **tên** biến; scrollback của terminal và log của CI sống lâu hơn
phiên làm việc.

### Đưa lên Kubernetes

Cùng năm biến ấy, đích đến khác: **`deploy/`** — mẫu ConfigMap/Secret/Deployment/
Service, CronJob dọn nhật ký, và **bảng ánh xạ** biến Go → khoá k8s → Secret hay
ConfigMap → bắt buộc hay không → hỏng thế nào khi thiếu. Nguồn duy nhất của bảng ấy:
**`deploy/README.md`**.

`cmd/an-danh` chỉ đọc `DATABASE_DSN` (qua `config.NapChiDSN`): bắt người vận hành đặt cả
secret của Zalo để chạy một lệnh không gọi Zalo là cách nhanh nhất khiến họ điền bừa một giá
trị giả.

### Ba con số là CHÍNH SÁCH, không phải tham số

Chủ sản phẩm chốt cả ba ngày 20/09/2026. Chúng cố ý nằm trong mã/lược đồ có người review,
không nằm ngoài môi trường cho ai sửa cũng được.

| Con số | Nguồn duy nhất | Nghĩa |
|---|---|---|
| **TTL phiên 7 ngày** | `config.TTLPhien` | phiên đăng nhập hết hạn sau 7 ngày |
| **Nhật ký đăng nhập 90 ngày** (trần: mỗi dòng sống tối đa 90, tối thiểu 83) | mặc định của hàm `nhat_ky_don_qua_han` trong `migrations/0002_…sql` | `make don-nhat-ky` gọi không tham số, chạy **hằng ngày** |
| **Số điện thoại: giữ tới khi người dùng yêu cầu xoá** | không có hạn tự động trong mã | xoá = ẩn danh hoá, xem `make an-danh` |

---

## CORS — chọn origin nào và vì sao

Danh sách đọc từ `CORS_ALLOWED_ORIGINS`, **không bao giờ `*`**, và config từ chối `*` ngay
lúc nạp. Không đặt `Access-Control-Allow-Credentials`: API xác thực bằng bearer token trong
header, không dùng cookie, nên bật credentials chỉ mở thêm bề mặt CSRF.

Đề xuất cho sản xuất — **mức chứng cứ TRUNG BÌNH, phải xác nhận bằng một lần mở DevTools
trên máy thật**:

| Origin | Vì sao |
|---|---|
| `https://h5.zdn.vn` | webview của Zalo Mini App phục vụ ứng dụng từ tên miền này |
| `https://zalo.me` | một số luồng mở Mini App trong ngữ cảnh zalo.me |
| `http://localhost:3000` | chỉ môi trường phát triển (`zmp start`), **không đưa vào sản xuất** |

Yêu cầu **không có** header `Origin` (gọi từ máy chủ, thăm dò sức khoẻ) đi tiếp bình thường:
CORS là cơ chế của trình duyệt, không phải cơ chế xác thực. Ai cần chặn thì dùng xác thực.

---

## Chặn lạm dụng

`POST /api/v1/sessions` là tuyến công khai và **mỗi lượt gọi tốn một lời gọi sang Zalo**.
Giới hạn dự kiến: **10 lượt / 5 phút / IP** (token bucket trong bộ nhớ).

Giới hạn của cách làm này — nói trước, đừng để ai tưởng nó là tường lửa:

1. **Chỉ đúng với một tiến trình.** Chạy 3 bản sao thì trần thực tế là 30 lượt/5 phút/IP.
   Muốn đúng thật thì phải đếm tập trung (Redis) — chưa làm ở bước này.
2. **Khởi động lại là xoá sạch bộ đếm.**
3. **IP lấy từ `RemoteAddr`.** Đứng sau proxy/CDN thì đó là IP của proxy, và cả thế giới
   chung một xô. Tin `X-Forwarded-For` khi chưa cấu hình proxy tin cậy thì còn tệ hơn: ai
   cũng giả được header đó và bộ giới hạn thành vô dụng. → **NỢ**: khai báo proxy tin cậy
   trước khi đặt sau load balancer.
4. Nhiều người dùng sau cùng một NAT (văn phòng, wifi công cộng) dùng chung hạn mức.

Một lần bị từ chối **đầu tiên trong mỗi cửa sổ** được ghi vào `nhat_ky_dang_nhap`
(`ket_qua = 'qua_nhieu_lan'`). Không ghi mọi lượt bị từ chối: một tuyến đang bị lạm dụng mà
vẫn ghi CSDL từng lượt thì chính nó là đường khuếch đại tấn công.

---

## CSDL

```
make migrate        # cần DATABASE_DSN trong môi trường và có psql
```

| Bảng | Cột |
|---|---|
| `nguoi_dung` | `id` uuid PK · `so_dien_thoai` text UNIQUE (đã chuẩn hoá `84xxxxxxxxx`) · `tao_luc` · `cap_nhat_luc` |
| `phien` | `id` uuid PK · `nguoi_dung_id` → `nguoi_dung` · `token_bam` bytea UNIQUE (32 byte) · `tao_luc` · `het_han_luc` · `thu_hoi_luc` |
| `nhat_ky_dang_nhap` | `id` bigint identity · `nguoi_dung_id` (NULL khi thất bại) · `phien_id` · `ket_qua` · `ly_do` (mã ngắn) · `dia_chi_ip` inet · `tao_luc` · PK `(id, tao_luc)`, **phân mảnh theo tuần** |
| `nhat_ky_an_danh` (0002) | `id` bigint identity PK · `nguoi_dung_id` → `nguoi_dung` · `nguon_yeu_cau` (`hotline`/`email`) · `nguoi_thuc_hien` · `ghi_chu` · `tao_luc` |

Ba điều không thương lượng trong lược đồ:

- **`phien` lưu bản băm SHA-256 của token, không lưu token.** Một bản sao CSDL rò ra mà chứa
  token nguyên bản là kẻ cầm nó đăng nhập thay mọi người dùng ngay lập tức.
- **`nhat_ky_dang_nhap` chỉ ghi thêm**, cưỡng chế bằng trigger chặn `UPDATE`/`DELETE`/
  `TRUNCATE` — không bằng lời hứa trong tài liệu, và không bằng `GRANT` (ứng dụng thường chạy
  bằng chính chủ sở hữu bảng, mà chủ sở hữu thì bỏ qua `GRANT`).
- **Nhật ký không bao giờ chứa số điện thoại**, chỉ chứa `nguoi_dung_id`.

Một lần đăng nhập thành công ghi người dùng + phiên + nhật ký trong **một giao dịch**. Không
có nhánh "ghi nhật ký sau nếu được".

### Dọn nhật ký đăng nhập — 90 ngày

```
make don-nhat-ky        # CRON HẰNG NGÀY — không được thưa hơn, xem bên dưới
```

Một lệnh, hai việc: tạo trước phân mảnh cho các tuần sắp tới, rồi **DROP** các phân mảnh đã
quá 90 ngày.

**Mỗi dòng sống tối đa 90 ngày, tối thiểu 83** — dọn theo lô tuần thì không thể đúng 90 cho
mọi dòng, và phần lệch được chọn đi về phía **xoá sớm**.

**Vì sao DROP phân mảnh chứ không DELETE.** Dọn 90 ngày là DELETE, mà DELETE trên nhật ký bị
trigger từ chối — đúng như thiết kế. Đường còn lại là cho trigger một cờ để nó nhận ra "lần
dọn định kỳ": nhanh hơn, nhưng đó là **một cánh cửa mở sẵn trong đúng cơ chế làm nhật ký có
giá trị chứng cứ**, và từ hôm ấy không gì phân biệt được lần dọn định kỳ với một lần xoá dấu
vết. DROP là DDL, không đi qua trigger mức dòng, nên tính chỉ-ghi-thêm **không bị khoét lỗ**:
vẫn không ai sửa hay xoá được một dòng nào.

Cái giá, nói trước:

- **Biên độ 83–90 ngày, không phải đúng 90.** Điều kiện là `d <= hôm_nay − 90`: DROP khi
  **đầu** phân mảnh đã quá hạn. Viết thành `d + 7 <=` (**đuôi** quá hạn) nghe an toàn hơn —
  "không xoá sớm dòng nào" — nhưng nó đẩy dòng cũ nhất lên **97 ngày**, tức hệ thống vượt qua
  chính cái trần in trong văn bản pháp lý; lệch bảy ngày về phía giữ lâu hơn là thứ không
  được để lọt. Hai ca test khoá cả hai phía ranh giới (90 ngày → phải dọn · 89 ngày → không
  đụng). Chia theo tháng thì biên độ thành 60–90 ngày (mất tới một tháng dữ liệu truy lạm
  dụng); chia theo ngày thì sát nhất nhưng lỡ một ngày là hỏng.
  → Câu đúng cho văn bản pháp lý: **"nhật ký đăng nhập được xoá chậm nhất 90 ngày kể từ khi
  ghi"**. Đừng viết "đúng 90 ngày", cũng đừng viết "không quá 90 ngày, dọn theo lô hằng
  tuần" — câu sau là câu cũ và nó sai.
- **Chạy hằng ngày, không phải hằng tuần.** Khoảng cách giữa hai lần chạy cộng thẳng vào tuổi
  của dòng cũ nhất: cron hằng tuần biến trần 90 thành 97, đúng cái vừa sửa.
- **Bỏ bẵng `make don-nhat-ky`** thì dòng mới rơi vào phân mảnh mặc định: không mất dữ liệu,
  nhưng phải dọn tay trước khi tạo lại được phân mảnh của tuần đó. Hàm dọn **cảnh báo** khi
  phân mảnh mặc định có dòng.
- Khoá chính đổi từ `(id)` thành `(id, tao_luc)` — Postgres đòi khoá chính chứa khoá phân
  mảnh. Không bảng nào tham chiếu nhật ký nên không gãy gì.
- `migrations/0002` cần **PostgreSQL >= 13** (0001 chỉ cần >= 10).

### Yêu cầu xoá dữ liệu — `make an-danh`

```
DATABASE_DSN=... make an-danh
```

Chạy tay, **có người ký**: lệnh hỏi số điện thoại (qua stdin, **không** qua tham số dòng
lệnh — tham số nằm trong `ps` và trong lịch sử shell), nguồn yêu cầu (`hotline`/`email`),
tên người tiếp nhận, số phiếu, rồi bắt gõ `AN DANH` để xác nhận.

Một giao dịch, bốn việc: ghi đè `so_dien_thoai` bằng một giá trị vô danh duy nhất · thu hồi
mọi phiên **còn hiệu lực** của người ấy · xoá `ghi_chu` và `ten_hien_thi` trên **mọi phiếu
`yeu_cau`** của người ấy (giữ hàng, đặt `an_danh_luc`) · ghi một dòng `nhat_ky_an_danh` mang
tên người tiếp nhận. **Cần lược đồ tới 0004.** Dữ liệu **đã gửi sang bên nhận webhook** thì
lệnh này không với tới — bên ấy phải xoá theo quy trình của họ. Đầu ra chỉ có **mã định danh**, số phiên đã thu hồi và thời điểm — **không bao
giờ có số điện thoại**, kể cả khi báo lỗi.

**Cố ý không có tuyến API cho việc này.** Một tuyến nhận số điện thoại rồi xoá dữ liệu ứng
với số ấy là một tuyến xoá dữ liệu *người khác*.

---

## Thử Zalo thật — `make thu-zalo`

```
make thu-zalo                  # KHÔNG gửi appsecret_proof (đúng như sản xuất)
make thu-zalo DOI=--proof      # CÓ gửi — phải dùng CẶP TOKEN MỚI
```

Lời gọi sang Zalo đã nằm trong mã từ đầu (`internal/zalo/client.go`, gọi từ
`internal/httpapi/sessions.go`). Thứ **chưa từng xảy ra** là một lần chạm máy chủ
Zalo thật — vì `accessToken` và `phoneToken` do máy người dùng sinh ra **bên trong
Zalo** và **hết hạn sau ~2 phút**: không ai ở phía máy chủ tạo được chúng. Lệnh này
biến NỢ #1-3 từ "phải dựng một buổi thử" thành **một thao tác 30 giây cho người đang
cầm điện thoại**.

Lệnh đi qua **đúng hàm** mà đường phục vụ dùng (`Client.goi`), không phải một bản sao
— nếu nó dựng lời gọi riêng thì một lần chạy xanh chỉ chứng minh cho bản sao ấy.

### Các bước người cầm máy phải làm

Hai token sống ~2 phút, nên **người cầm điện thoại và người gõ lệnh phải ngồi cạnh
nhau hoặc đang trên cùng một cuộc gọi**. Gửi token qua chat rồi mới chạy là gần như
chắc chắn hết hạn — và là gửi thông tin xác thực của một người dùng thật qua một kênh
lưu lại vĩnh viễn.

1. Mở **Mini App bản phát triển** trong Zalo trên điện thoại thật (bản `zmp deploy`
   dạng thử nghiệm, hoặc quét QR từ công cụ phát triển Mini App).
2. Bấm đúng nút đăng nhập — nút gọi `getAccessToken()` rồi `getPhoneNumber()`.
3. **Lấy hai chuỗi ấy ra**. Hai cách, chọn một:
   - **Màn hình gỡ lỗi tạm** trong app: in hai token ra màn hình kèm nút sao chép.
     Chắc chắn nhất. **Gỡ bỏ trước khi phát hành** — một màn hình hiện `phoneToken`
     là một màn hình hiện thông tin xác thực.
   - **Bộ công cụ phát triển của Zalo**: xem `console.log` hoặc thân của yêu cầu
     `POST /api/v1/sessions` mà app vừa gửi.
4. Trên máy đã có `ZALO_MINIAPP_APP_ID` và `ZALO_MINIAPP_SECRET_KEY` (trong
   `.env.local` hoặc trong shell), chạy `make thu-zalo`, dán **accessToken** rồi
   Enter, dán **phoneToken** rồi Enter. Token nhập **qua stdin**, không qua tham số
   dòng lệnh: tham số nằm trong `ps` và trong lịch sử shell.
5. Chép **nguyên khối kết quả** vào mục NỢ tương ứng dưới đây.

### Lệnh in ra những gì

Khối đầu ra được thiết kế để **dán vào phiếu**: không số điện thoại, không token,
không secret key — kể cả khi Zalo nhắc lại số trong `message` (chỗ ấy bị gạch).

| Dòng | Để làm gì |
|---|---|
| thời điểm, app id, endpoint, `appsecret_proof` **BẬT/TẮT** | biết lần chạy này là lần chạy nào, ở chế độ nào |
| **mã HTTP**, **thời gian phản hồi** | phân biệt "Zalo từ chối" với "không với tới được" |
| **`error` và `message` NGUYÊN VĂN** của Zalo | thứ duy nhất đóng được NỢ #3 (bảng mã lỗi) |
| **`thân JSON đọc được: có/KHÔNG`** | hình dạng wire trong `wire.go` có đúng không |
| số điện thoại: lấy được hay không, **độ dài + hai ký tự đầu**, cả **dạng thô** lẫn **sau chuẩn hoá** | đủ biết Zalo trả `84…` hay `09…` hay `+84…` (đóng ĐIỀU CHƯA RÕ #3) — **không in cả số** (Nghị định 13) |
| **người dùng sẽ thấy: 201 / 401 / 502** | lời gọi này thành mã nào ở tuyến thật |
| **Kết luận** | lần chạy này đóng được mục nợ nào, và **không** đóng được mục nào |

**`phoneToken` rất có thể dùng MỘT LẦN.** Muốn biết Zalo có **đòi** `appsecret_proof`
hay không thì phải chạy **hai lượt, mỗi lượt một cặp token mới** — một lượt
`make thu-zalo`, một lượt `make thu-zalo DOI=--proof`. Hai lượt trên cùng một cặp
token **không so sánh được với nhau**: lượt sau sẽ hỏng vì token đã tiêu, và lỗi ấy
trông y hệt "Zalo đòi proof". Chính lệnh cũng nhắc lại điều này ở cuối mỗi lần chạy.

Test của `cmd/thu-zalo` và `internal/zalo` **không thay được** lần chạy ấy: chúng dùng
`httptest` và chỉ chứng minh mã khớp giả định của chính nó.

---

## Dữ liệu cá nhân — Nghị định 13/2023

Số điện thoại là dữ liệu cá nhân. Trong kho này:

- **không vào log** ở bất kỳ mức nào, kể cả debug;
- **không vào URL, tên tệp, khoá cache** (đó là lý do token đi bằng header chứ không bằng
  query khi gọi Zalo);
- **không vào thông điệp lỗi** trả ra ngoài, cũng không vào lỗi nội bộ — lỗi đi thẳng vào log;
- **không có tuyến nào trả nó ra** ở bước này;
- ví dụ và test dùng số giả đã thống nhất `0900000000` (`84900000000` sau chuẩn hoá).

Kiểu `secret.Secret` khoá mọi đường in mặc định (`%s`, `%v`, `%#v`, `json.Marshal`); lấy giá
trị thật phải viết rõ `.Lo()` — một lời gọi người review nhìn thấy.

### Máy chủ thực sự lưu những gì

Bảng này phải **khớp từng dòng** với văn bản Chính sách riêng tư của app: văn bản ấy đi kèm
hồ sơ duyệt Zalo, và khai thiếu một mục cũng là vi phạm chính Nghị định 13/2023.

| Lưu gì | Ở đâu | Chính sách riêng tư đang khai? |
|---|---|---|
| Số điện thoại | `nguoi_dung.so_dien_thoai` | **có** |
| Thời điểm đăng nhập / cập nhật | `nguoi_dung.tao_luc`, `.cap_nhat_luc`, `phien.tao_luc` | **chưa — phải bổ sung** |
| **Địa chỉ IP** của mỗi lượt đăng nhập (kể cả lượt thất bại) | `nhat_ky_dang_nhap.dia_chi_ip` | **chưa — phải bổ sung** |
| Kết quả từng lượt đăng nhập, thành công lẫn thất bại | `nhat_ky_dang_nhap.ket_qua`, `.ly_do` | **chưa — phải bổ sung** |
| Bản băm của token phiên (không phải token) | `phien.token_bam` | không cần khai riêng — dữ liệu kỹ thuật, không nhận dạng được ai |
| Việc đã xử lý một yêu cầu xoá: ai tiếp nhận, nguồn, thời điểm | `nhat_ky_an_danh` | **chưa — nên khai**, kèm câu "chúng tôi lưu bằng chứng đã xử lý yêu cầu của bạn" |
| **Tên hiển thị Zalo** khi bấm "Chat với chuyên viên" (0004) | `yeu_cau.ten_hien_thi` | **chưa — phải bổ sung** |
| **Lần đăng ký / huỷ nhận SMS ưu đãi** (0004) | `yeu_cau.loai` | **chưa — phải bổ sung** |
| Số điện thoại, tên hiển thị, ghi chú **gửi sang hệ thống bên nhận webhook** | không lưu ở đây — **chuyển cho bên thứ ba** | **chưa — phải bổ sung**, kèm tên bên nhận |

Không lưu: email, vị trí, thông tin thiết bị, danh bạ, ảnh. Không có bộ theo dõi
(analytics/SDK bên thứ ba) nào trong dịch vụ này.

### Thời hạn lưu — chủ sản phẩm đã chốt 20/09/2026

**Số điện thoại giữ tới khi người dùng yêu cầu xoá.** Không có hạn tự động, không có việc
tự dọn sau N tháng. Ba hệ quả phải nói ra:

1. **Phải có một đường nhận yêu cầu xoá thật.** Hiện là **hotline và email trên màn Liên hệ**
   của app. Một chính sách "xoá khi được yêu cầu" mà không có chỗ để yêu cầu thì không phải
   là một chính sách.
2. **XOÁ NGHĨA LÀ ẨN DANH HOÁ** — chủ sản phẩm chốt sau khi phía Mini App phát hiện lược đồ
   không cho xoá thật, và điều đó là **cố ý**: `nhat_ky_dang_nhap.nguoi_dung_id` tham chiếu
   `nguoi_dung(id)`, còn nhật ký thì chỉ được ghi thêm, nên một hàng `nguoi_dung` đã từng
   đăng nhập là không xoá được và cũng không null hoá khoá ngoại đi được. Dấu vết *"có một
   lần đăng nhập lúc 14:02"* phải còn; *"người ấy là ai"* thì biến mất.
   → Lệnh: **`make an-danh`** (xem mục "Yêu cầu xoá dữ liệu" ở trên).
3. **Nhật ký đăng nhập thì GIỮ** — nó không chứa số điện thoại, chỉ chứa `nguoi_dung_id`,
   nên sau khi định danh bị ghi đè thì dòng nhật ký không còn chỉ về ai được nữa. Nó vẫn
   theo chính sách 90 ngày của riêng nó.

**Nhật ký đăng nhập và `dia_chi_ip`: chậm nhất 90 ngày** (thực tế 83–90), dọn bằng `make don-nhat-ky` chạy hằng ngày. `nhat_ky_an_danh`
thì **giữ vô thời hạn**: nó là bằng chứng đã xử lý một yêu cầu xoá, và một bằng chứng có hạn
tự huỷ thì không phải bằng chứng. Nó không chứa số điện thoại.

---

## Chạy test

```
make check      # gofmt + go vet + go test -race
```

`make check` **không gọi mạng và không cần CSDL**. Test của `internal/zalo` dựng máy chủ giả
bằng `httptest`; test của `internal/store` **tự SKIP kèm lý do** khi không có
`TEST_DATABASE_DSN`:

```
--- SKIP: TestTaoPhienDangNhap_GhiCaBaBangTrongMotGiaoDich (0.00s)
    kho_test.go:55: thiếu TEST_DATABASE_DSN — test chạm CSDL không chạy
```

Chạy phần chạm CSDL (CSDL **dùng riêng cho test**, đã chạy **mọi** migration (`make migrate`) — các ca này
có `DROP` phân mảnh):

```
TEST_DATABASE_DSN='<dsn>' go test ./internal/store
make test-csdl                     # tương đương, nhưng lấy DSN từ .env.local
```

Mười hai ca đó (mười ba, tính cả hai ca con của ranh giới 90 ngày) kiểm thứ chỉ CSDL mới trả lời được: ba lần ghi nằm trong một giao dịch (hỏng thì
không còn `nguoi_dung` nào) · trigger từ chối `UPDATE`/`DELETE` trên nhật ký · cùng một số
thì cùng một `nguoi_dung_id` · nhật ký không chứa số điện thoại · ẩn danh xong thì số cũ
biến mất, phiên còn hiệu lực bị thu hồi, nhật ký đăng nhập còn nguyên · `nhat_ky_don_qua_han()`
DROP đúng phân mảnh quá hạn và không đụng phân mảnh còn hạn.

Bỏ qua thì phải **nhìn thấy là đã bỏ qua**. Một gói in `ok` trong khi chưa chạy gì là cách
tệ nhất để mất niềm tin vào bộ test.

Trên máy chưa có `make` (Windows): `mingw32-make check`, hoặc chạy thẳng
`go vet ./... && go test -race ./...`.

---

## NỢ — phải trả trước khi phát hành

**NỢ #1-3 nay CÓ ĐƯỜNG THỬ: `make thu-zalo`** (xem mục "Thử Zalo thật"). Chúng vẫn là
nợ — có đường thử không phải là đã thử.

**Đã chạy một lần, 20/09/2026, bằng token GIẢ** (để kiểm chính đường dây của lệnh).
Máy chủ trả lời thật, nên có ba điều **đã hết là suy đoán**: endpoint
`GET /v2.0/me/info` có thật và trả HTTP 200 · thân đúng hình dạng JSON mà `wire.go`
mô tả · `error=452` = access_token sai, tức lỗi phía người dùng và ánh xạ 401 hiện tại
**đúng** cho mã ấy. Nguyên văn quan sát nằm trong `internal/zalo/wire.go` — **nguồn duy
nhất** của bảng mã lỗi, đừng chép sang đây.
**Không** chứng minh được gì về: tên hai header `code`/`secret_key`, trường
`data.number`, và `appsecret_proof` — lời gọi dừng ở access_token sai trước khi chạm
tới chúng. Đó đúng là phần chỉ **token thật** mới mở được.

1. **Thử bộ đổi token Zalo trên máy thật.** Toàn bộ hình dạng giao thức nằm trong
   `internal/zalo/wire.go` ở **mức chứng cứ TRUNG BÌNH**: gom từ nhiều nguồn **thứ cấp** nhất
   quán, vì trang tài liệu chính thức render bằng JS nên không đọc được nguyên văn (20/09/2026).
   **Chưa một dòng nào chạm máy chủ Zalo.** Test trong gói đó dùng `httptest` và chỉ chứng
   minh *mã khớp với giả định của chính nó* — nó không chứng minh giả định đúng.
   → Chạy `make thu-zalo` một lần với token thật, rồi chép khối kết quả vào đây và **sửa mức
   chứng cứ trong `wire.go`**.
2. **`appsecret_proof`.** Từ 01/01/2024 Zalo yêu cầu tham số này khi lấy thông tin người dùng
   từ máy chủ. Chưa rõ có áp cho luồng Mini App này không, gửi bằng header hay query, và ký
   trên chuỗi nào. Hiện **tắt mặc định**, có sẵn công tắc `Client.GuiAppSecretProof` để lật
   khi thử trên máy thật.
   → `make thu-zalo` (tắt) và `make thu-zalo DOI=--proof` (bật), **mỗi lượt một cặp token
   mới**. Lượt tắt mà thành công là Zalo **không đòi** proof.
3. **Bảng mã lỗi của Zalo.** Hiện mọi `error != 0` đều coi là token hỏng → 401. Nếu có mã
   nghĩa là "secret key sai" thì đó là lỗi **phía ta**, phải tách thành 502 + cảnh báo vận
   hành, chứ không được bảo người dùng đăng nhập lại.
   → `make thu-zalo` in `error` và `message` **nguyên văn**: mỗi mã gặp được thì ghi một dòng
   vào đây, kèm việc nó là lỗi phía người dùng hay phía ta.
4. **Hình dạng dây của ZNS — CHƯA ĐO.** `internal/zns/client.go` viết từ nguồn **thứ cấp**:
   `POST /message/template`, token ở header `access_token`, thành công là `error: 0`. Chưa một
   lời gọi thật nào. Tính năng **tắt** chừng nào `ZALO_ZNS_ACCESS_TOKEN` + `ZALO_ZNS_TEMPLATE_ID`
   còn trống, nên nợ này không thể lặng lẽ ra môi trường thật.
   → Cần: một `template_id` **đã được Zalo duyệt**, có tham số `ma_yeu_cau`; gọi thật một lần;
   đối chiếu tên trường; rồi mới điền hai biến.
5. **Vòng đời access token của OA — CHƯA CÓ.** Token ZNS có hạn và phải làm mới bằng refresh
   token; gói `zns` nhận một token **tĩnh** và không tự làm mới. Hết hạn thì yêu cầu vẫn ghi
   nhận bình thường, chỉ tin xác nhận không tới — mỗi lượt để lại một dòng `that_bai` trong
   `zns_da_gui`, nên nó **đếm được** và dựng cảnh báo được.
   → Viết vòng làm mới sau khi nợ #4 xong; viết trước là viết mù.
6. **Hình dạng dây của tổng đài — CHƯA ĐO.** `internal/tongdai/client.go` cố ý **không** mang
   tên OmiCall: nó POST một thân tối giản `{phone, requestId}` tới một URL cấu hình được, kèm
   `Authorization: Bearer`. Đó là thứ biết chắc; phần còn lại phải đo.
   → Xác nhận hình dạng thật rồi sửa **đúng tệp ấy**. Đừng rải hình dạng mới ra tầng khác.
7. **CronJob ẩn danh hoá yêu cầu quá 24 tháng — CHƯA CÓ.** `migrations/0003` có hàm
   `an_danh_yeu_cau_qua_han()` nhưng **không có gì gọi nó**. Một hàm dọn không ai gọi là một
   hàm làm người đọc lược đồ tin rằng dữ liệu đang được dọn.
   → Thêm một CronJob hằng ngày, cùng khuôn `deploy/cronjob-don-nhat-ky.yaml`.
8. **Chính sách riêng tư phải khai thêm dữ liệu bán hàng.** Từ `0003` máy chủ lưu thêm: sản
   phẩm quan tâm, quy mô, **ô ghi chú tự do**, mã chiến dịch, và vết từng tin ZNS đã gửi. Văn
   bản hiện hành chỉ khai số điện thoại và nhật ký đăng nhập.
   → Đây là một thay đổi **pháp lý**, không phải một dòng tài liệu: nó đi kèm hồ sơ duyệt Zalo.
9. **Xác nhận origin CORS thật** bằng DevTools trên thiết bị.
10. **Proxy tin cậy** cho bộ giới hạn theo IP, trước khi đặt sau load balancer.
11. **Bổ sung Chính sách riêng tư của app.** IP, thời điểm đăng nhập và kết quả từng lượt
   đăng nhập đang được lưu, trong khi văn bản hiện chỉ khai số điện thoại — xem bảng
   "Máy chủ thực sự lưu những gì". Văn bản ấy đi kèm hồ sơ duyệt Zalo, khai thiếu là vi phạm
   chính Nghị định 13.
12. ~~Thời hạn lưu nhật ký~~ — **ĐÃ CHỐT: chậm nhất 90 ngày** (thực tế 83–90), cưỡng chế bằng
   `make don-nhat-ky`. ~~Việc còn lại là cắm nó vào cron HẰNG NGÀY~~ — **ĐÃ CÓ:
   `deploy/cronjob-don-nhat-ky.yaml`**, chạy `15 3 * * *` **hằng ngày**, `concurrencyPolicy:
   Forbid`, chạy bù trong một giờ nếu lỡ cửa sổ.
   Việc **còn lại thật sự**: (a) áp dụng nó lên cụm, và (b) **một cảnh báo khi nó ngừng
   chạy** — CronJob bị xoá, bị `suspend`, hoặc Job hỏng nhiều ngày liền thì nhật ký âm thầm
   sống quá 90 ngày, và triệu chứng đầu tiên là một câu hỏi từ phía kiểm tra. `make check`
   không thấy được chuyện này và manifest cũng không. Xem `deploy/README.md`, mục CronJob.
8. **Chạy thử CẢ HAI migration trên một Postgres thật.** Máy dựng kho không có `psql` và
   không có Docker đang chạy, nên lược đồ **chưa từng được áp dụng lần nào** — cú pháp mới
   chỉ được đọc bằng mắt, và **mười hai ca test chạm CSDL trong `internal/store` chưa từng
   chạy thật lần nào**. Đây là mục nợ nặng nhất trong danh sách này. Khi chạy, kiểm bốn việc:
   `UPDATE`/`DELETE` trên `nhat_ky_dang_nhap` bị trigger từ chối (kể cả sau khi đã phân
   mảnh) · `nhat_ky_don_qua_han()` DROP đúng phân mảnh quá hạn · `nhat_ky_tao_phan_manh()`
   chạy lại được · `make an-danh` ẩn danh xong thì phiên cũ hết vào được. **Cần PostgreSQL
   >= 13** (trigger mức dòng trên bảng phân mảnh).
9. ~~Đường xử lý yêu cầu xoá~~ — **ĐÃ CÓ: `make an-danh`** (một giao dịch: ghi đè định danh,
   thu hồi phiên, ghi bằng chứng). Việc còn lại là **quy trình của người**: ai trực hotline,
   ai được phép chạy lệnh, lưu phiếu ở đâu, và trả lời người yêu cầu trong bao lâu. Nghị
   định 13 có thời hạn trả lời — **chưa ai chốt con số ấy cho sản phẩm này**.
10. **Chưa có middleware xác thực bearer token.** Bước này chỉ CẤP phiên; chưa tuyến nào
    tiêu thụ nó. Khi thêm tuyến cần đăng nhập: tra `phien` theo `token_bam`, loại phiên đã
    `het_han_luc` hoặc có `thu_hoi_luc`, và so sánh băm bằng hàm so sánh thời gian hằng định.
13. **Cầu phiên ViGov: MÃ TÀI KHOẢN ZALO — ĐÃ CÀI 29/09/2026, CHƯA ĐO.** Chủ dự án chốt làm
    theo bản tham chiếu ở kho yêu cầu (`vigov-require` commit `0053854`,
    `apps/api/app/integrations/zalo/graph.py:113-134`): `GET /v2.0/me?fields=id`, header
    `access_token`, không secret, chỉ xin `id`, 6 giây (`internal/zalo/ma_tai_khoan.go`). Hình
    dạng và mức chứng cứ THẤP: `wire.go`, ĐIỀU CHƯA RÕ #7–#8.
    → **Còn nợ:** một lần gọi thật với token thật từ điện thoại (`cmd/thu-zalo` chưa có nhánh
    này); ghi `id` là chuỗi hay số, và mã lỗi thấy được, vào `wire.go`.
14. **Cầu phiên ViGov: N App ID — ĐÃ CÀI 29/09/2026; CÁCH XÁC MINH CHƯA ĐO.** N cặp app riêng
    ở `ZALO_MINIAPP_COMMUNE_APP_SECRETS`; `appId` trong thân chọn secret; mọi lượt đổi có secret
    (số, vị trí) dùng secret của app ấy. "App ID đã xác minh" = lượt đổi `phoneToken` bằng
    secret của app ấy thành công (`internal/httpapi/app_zalo.go`). **Chỉ đúng nếu Zalo từ chối
    token của app X đổi bằng secret app Y** — ADR 0045 UNKNOWN #1, **chưa đo có kiểm soát**, và
    chứng cứ đang có trái chiều (ADR 0047 §6: đổi token app xã bằng secret app chung "đã chạy
    được" 27/09; lỗi 502 của `/api/v1/location` ở app xã thì gợi ý ngược lại).
    → Đo: trên điện thoại, mở app riêng, lấy `accessToken` + `phoneToken`, đổi bằng secret **app
    chung**; một cặp token mới, đổi bằng secret **app riêng**. Lượt đầu mà thành công thì cách
    xác minh này **không** xác minh gì — phải báo chủ dự án trước khi phát hành app riêng.
    Hệ quả nếu vậy (ADR 0045:286): người khai `appId` xã B vào được xã B, không đọc được hồ sơ
    của ai khác. Cái giá đã chọn: đăng nhập từ app riêng **luôn cần `phoneToken`**, kể cả lần
    mở lại — không có lượt đổi có secret nào khác để xác minh.
15. ~~Cầu bật thì bề mặt `/api/v1/requests` mất phiên~~ — **ĐÃ SỬA 27/09/2026** (chọn nhánh theo
    `communeHostHint`). ~~App riêng của xã rơi về phiên thương mại~~ — **ĐÃ SỬA 29/09/2026**:
    `appId` là app riêng thì đi cầu với App ID của app ấy, không cần tên miền
    (`internal/httpapi/sessions.go`). Phụ thuộc #14 cho phần "đã xác minh".
16. **Đổi token vị trí (`POST /api/v1/location`) — CHƯA ĐO với Zalo thật.** Tên trường
    `data.latitude`/`data.longitude`, kiểu (chuỗi hay số) và mã lỗi khi token vị trí hết hạn
    đều lấy từ bản tham chiếu ở kho yêu cầu, không từ quan sát (`wire.go`, ĐIỀU CHƯA RÕ #5–#6).
    `cmd/thu-zalo` chưa có nhánh vị trí. Đóng bằng một lần gọi thật với token lấy từ điện thoại.
    Từ 29/09/2026 tuyến đổi bằng secret của app mà `appId` chỉ tới — lỗi 502 ở app riêng
    (đổi bằng secret app chung) phải hết; **chưa thử trên máy thật**.
17. **Webhook "yêu cầu mới" (07/10/2026) — bên nhận CHƯA CHỐT, ba điều còn mở.**
    (a) **Chống phát lại**: chữ ký không kèm dấu thời gian, nên một thân bị bắt được gửi lại được
    nguyên văn; bên nhận khử trùng theo `requestId` thì phát lại vô hại — phải xác nhận bên nhận
    làm vậy. (b) **Không hàng đợi bền**: hai lượt hỏng thì sự kiện mất (log Error kèm
    `ma_yeu_cau`); dựng lại từ `yeu_cau`. Nếu "huỷ nhận SMS" mà mất sự kiện là không chấp nhận
    được thì phải có outbox. (c) **Chính sách riêng tư** phải khai việc chuyển số, tên, ghi chú
    sang bên nhận (bảng "Máy chủ thực sự lưu những gì").
18. **Migration 0004 chưa chạy trên Postgres thật** (cùng nợ với 0001–0003, mục 8): hai ca
    `internal/store` mới (`TestAnDanhHoa_XoaGhiChuVaTenTrenYeuCau`, `TestYeuCau_RangBuocTenHienThi`)
    SKIP khi thiếu `TEST_DATABASE_DSN`.
