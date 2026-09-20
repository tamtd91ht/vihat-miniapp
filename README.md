# vihat-miniapp

Backend cho Zalo Mini App **giới thiệu doanh nghiệp VihatSoftware**.

Kho này **độc lập hoàn toàn** với ViGov: không `replace`, không import, không dùng chung
`go.mod`. Kiểm được:

```
go list -deps ./...     # chỉ có: thư viện chuẩn, module này, pgx (+ x/text, x/sync do pgx kéo)
```

Đổi tên thư mục `vigov-v2` đi thì `go build ./...` vẫn chạy — không có đường dẫn nào trỏ sang đó.

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

---

## Hợp đồng API

Phía Mini App đã viết theo khuôn này; giữ đúng.

```
POST /api/v1/sessions          công khai, không cần xác thực
  gửi: {"accessToken": "<getAccessToken()>", "phoneToken": "<token của getPhoneNumber()>"}
  201: {"token": "<bearer>", "expiresAt": "<RFC3339>"}

GET  /healthz                  công khai, cho thăm dò sức khoẻ
  200: {"trang_thai": "ok"}        503 khi không chạm được CSDL
```

Lỗi trả về `{"message": "<câu tiếng Việt nói người dùng làm gì tiếp>"}`.

| Mã | Khi nào | Câu trả về |
|---|---|---|
| 400 | thân yêu cầu không đọc được, thiếu `accessToken`/`phoneToken` | Yêu cầu không hợp lệ. Vui lòng mở lại ứng dụng và thử lại. |
| 401 | Zalo từ chối token (sai hoặc hết hạn) | Phiên Zalo đã hết hạn. Vui lòng đóng và mở lại ứng dụng để đăng nhập lại. |
| 429 | vượt giới hạn theo IP | Bạn thử đăng nhập quá nhiều lần. Vui lòng chờ vài phút rồi thử lại. |
| 502 | không với tới được Zalo, hoặc Zalo trả thứ không hiểu được | Hiện chưa kết nối được tới Zalo. Vui lòng thử lại sau ít phút. |
| 500 | lỗi phía hệ thống | Hệ thống đang bận. Vui lòng thử lại sau ít phút. |

Không bao giờ có mã lỗi kỹ thuật, tên cột, thông điệp của Zalo hay số điện thoại trong thân
phản hồi.

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

Một giao dịch, ba việc: ghi đè `so_dien_thoai` bằng một giá trị vô danh duy nhất · thu hồi
mọi phiên **còn hiệu lực** của người ấy · ghi một dòng `nhat_ky_an_danh` mang tên người
tiếp nhận. Đầu ra chỉ có **mã định danh**, số phiên đã thu hồi và thời điểm — **không bao
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

Không lưu: tên, email, vị trí, thông tin thiết bị, danh bạ, ảnh. Không có bộ theo dõi
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

Chạy phần chạm CSDL (CSDL **dùng riêng cho test**, đã chạy **cả hai** migration — các ca này
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
4. **Xác nhận origin CORS thật** bằng DevTools trên thiết bị.
5. **Proxy tin cậy** cho bộ giới hạn theo IP, trước khi đặt sau load balancer.
6. **Bổ sung Chính sách riêng tư của app.** IP, thời điểm đăng nhập và kết quả từng lượt
   đăng nhập đang được lưu, trong khi văn bản hiện chỉ khai số điện thoại — xem bảng
   "Máy chủ thực sự lưu những gì". Văn bản ấy đi kèm hồ sơ duyệt Zalo, khai thiếu là vi phạm
   chính Nghị định 13.
7. ~~Thời hạn lưu nhật ký~~ — **ĐÃ CHỐT: chậm nhất 90 ngày** (thực tế 83–90), cưỡng chế bằng
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
