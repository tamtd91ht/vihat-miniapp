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

Mã đã đủ để chạy; `make check` xanh. **Chưa từng chạy với Postgres thật và chưa từng gọi
Zalo thật** — xem mục **NỢ** cuối tệp trước khi phát hành.

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
internal/config/     NƠI DUY NHẤT đọc môi trường
internal/secret/     kiểu Secret — chặn rò bí mật qua log
internal/zalo/       wire.go = toàn bộ giao thức với Zalo, client.go = cách gọi
internal/phien/      token, băm token, từ vựng kết quả đăng nhập
internal/store/      NƠI DUY NHẤT biết SQL
internal/httpapi/    tuyến, CORS, giới hạn theo IP
migrations/          lược đồ, chạy bằng `make migrate`
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

1. **Thử bộ đổi token Zalo trên máy thật.** Toàn bộ hình dạng giao thức nằm trong
   `internal/zalo/wire.go` ở **mức chứng cứ TRUNG BÌNH**: gom từ nhiều nguồn **thứ cấp** nhất
   quán, vì trang tài liệu chính thức render bằng JS nên không đọc được nguyên văn (20/09/2026).
   **Chưa một dòng nào chạm máy chủ Zalo.** Test trong gói đó dùng `httptest` và chỉ chứng
   minh *mã khớp với giả định của chính nó* — nó không chứng minh giả định đúng.
2. **`appsecret_proof`.** Từ 01/01/2024 Zalo yêu cầu tham số này khi lấy thông tin người dùng
   từ máy chủ. Chưa rõ có áp cho luồng Mini App này không, gửi bằng header hay query, và ký
   trên chuỗi nào. Hiện **tắt mặc định**, có sẵn công tắc `Client.GuiAppSecretProof` để lật
   khi thử trên máy thật.
3. **Bảng mã lỗi của Zalo.** Hiện mọi `error != 0` đều coi là token hỏng → 401. Nếu có mã
   nghĩa là "secret key sai" thì đó là lỗi **phía ta**, phải tách thành 502 + cảnh báo vận
   hành, chứ không được bảo người dùng đăng nhập lại.
4. **Xác nhận origin CORS thật** bằng DevTools trên thiết bị.
5. **Proxy tin cậy** cho bộ giới hạn theo IP, trước khi đặt sau load balancer.
6. **Bổ sung Chính sách riêng tư của app.** IP, thời điểm đăng nhập và kết quả từng lượt
   đăng nhập đang được lưu, trong khi văn bản hiện chỉ khai số điện thoại — xem bảng
   "Máy chủ thực sự lưu những gì". Văn bản ấy đi kèm hồ sơ duyệt Zalo, khai thiếu là vi phạm
   chính Nghị định 13.
7. ~~Thời hạn lưu nhật ký~~ — **ĐÃ CHỐT: chậm nhất 90 ngày** (thực tế 83–90), cưỡng chế bằng
   `make don-nhat-ky`. Việc còn lại là **cắm nó vào cron HẰNG NGÀY** của môi trường thật:
   chạy thưa hơn thì khoảng cách giữa hai lần chạy cộng thẳng vào trần, và không chạy thì
   không có gì tự dọn. `make check` không phát hiện được chuyện này.
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
