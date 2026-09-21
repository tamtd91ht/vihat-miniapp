# deploy/ — đưa vihat-miniapp lên Kubernetes

Thư mục này là **mẫu và bảng ánh xạ**. Không có gì ở đây chạm vào cụm: Secret và
ConfigMap do chủ sản phẩm tự tạo.

| Tệp | Là gì |
|---|---|
| `configmap.example.yaml` | ba khoá KHÔNG bí mật — **chỉ placeholder** |
| `secret.example.yaml` | hai khoá BÍ MẬT — **chỉ placeholder**, và tốt nhất là đừng dùng tệp này (xem dưới) |
| `deployment.yaml` | tiến trình phục vụ: năm biến `env:` tường minh, ba loại probe, chạy non-root |
| `service.yaml` | ClusterIP |
| `cronjob-don-nhat-ky.yaml` | **lịch dọn nhật ký HẰNG NGÀY** — thứ làm cho trần 90 ngày có thật |

`.gitignore` chặn mọi tệp secret/configmap **không phải** `*.example.yaml` trong
thư mục này, kể cả trong thư mục con. Kiểm bằng lệnh chứ đừng đọc bằng mắt:

```
git check-ignore -v deploy/secret.yaml        # phải bị chặn
git add -n deploy/secret.example.yaml         # phải nhận
```

---

## BẢNG ÁNH XẠ — năm biến, một danh sách, hai đích đến

`internal/config` là **nơi duy nhất** đọc môi trường. Năm biến dưới đây là toàn
bộ những gì tiến trình cần; `TEST_DATABASE_DSN` chỉ dành cho test và **không bao
giờ lên cụm**.

| Biến Go (`internal/config`) | Khoá k8s | Trên cụm | Máy local (`.env.local`) | Bắt buộc? vì sao | Thiếu thì hỏng thế nào |
|---|---|---|---|---|---|
| `DATABASE_DSN` | `DATABASE_DSN` | **Secret** `vihat-miniapp-bi-mat` | cùng tên, trong tệp | **CÓ** — không có CSDL thì không phục vụ nổi một yêu cầu | `config.Nap` từ chối, tiến trình **không khởi động**, log nêu đích danh tên biến |
| `ZALO_MINIAPP_SECRET_KEY` | `ZALO_MINIAPP_SECRET_KEY` | **Secret** `vihat-miniapp-bi-mat` | cùng tên, trong tệp | **CÓ** — không có nó thì không đổi được `phoneToken` | không khởi động; nếu lọt qua thì **không ai đăng nhập được** |
| `ZALO_MINIAPP_APP_ID` | `ZALO_MINIAPP_APP_ID` | ConfigMap `vihat-miniapp-cau-hinh` | cùng tên, trong tệp | **CÓ** — một secret không kèm app id thì không ai xoay vòng hay chẩn đoán được | không khởi động |
| `CORS_ALLOWED_ORIGINS` | `CORS_ALLOWED_ORIGINS` | ConfigMap `vihat-miniapp-cau-hinh` | cùng tên, trong tệp | **CÓ** — và config **từ chối `*`** ngay lúc nạp | không khởi động. Đặt sai origin thì **nút đăng nhập chết im lặng** trên máy thật: trình duyệt chặn, không có lỗi nào tới máy chủ |
| `LISTEN_ADDR` | `LISTEN_ADDR` | ConfigMap (`optional: true`) | tuỳ chọn | **KHÔNG** — mặc định `:8080` | chạy bình thường. Đổi mà quên `containerPort` thì probe gõ vào cổng không ai nghe, pod **không bao giờ Ready** |
| `TEST_DATABASE_DSN` | — | **không lên cụm** | tuỳ chọn, cho `make test-csdl` | **KHÔNG** | test chạm CSDL tự SKIP kèm lý do |

Danh sách này bị khoá bằng phép kiểm: `internal/config/ban_ke_bien_test.go` bắt
`make check` đỏ khi một biến thiếu dòng trong `.env.example`, thiếu khoá trong
`deploy/*.example.yaml`, nằm ở cả hai nơi cùng lúc, hoặc bị viết bằng gạch ngang.

---

## Vì sao `env:` tường minh, KHÔNG dùng `envFrom`

`envFrom` nạp **mọi khoá** của một ConfigMap/Secret thành biến môi trường, giữ
**nguyên văn tên khoá**. Ngắn hơn, và đúng một cái bẫy:

> Kho ViGov khai quy ước "khoá ConfigMap/Secret viết **GẠCH-NGANG**"
> (`.claude/rules/critical/11-infra-config-contract.md`, bất biến 4), trong khi
> manifest của nó nạp bằng `envFrom`
> (`vigov-v2/deploy/base/identity/deployment.yaml:66-68`). Hai cách viết ấy
> **không đi cùng nhau được**: `DATABASE-DSN` không phải định danh shell hợp lệ,
> nên Kubernetes **bỏ qua nó trong im lặng** — chỉ một event
> `InvalidVariableNames` trên pod. Triệu chứng: service từ chối khởi động và kêu
> thiếu `DATABASE_DSN`, trong khi người vận hành mở Secret ra thì **thấy nó nằm
> ngay đó**. Đây là loại sự cố ăn hết một buổi chiều.

Kho này chọn **`env:` + `valueFrom` tường minh**, ba lý do:

1. **Năm biến thì bảng tường minh đọc hết trong một màn hình.** Mỗi biến hiện rõ
   nó đến từ Secret hay ConfigMap — chính là cột mà người vận hành cần.
2. **`optional: false` là cột "bắt buộc" ở dạng máy đọc được.** kubelet từ chối
   khởi động container khi khoá vắng mặt, và thông điệp chỉ thẳng vào Secret
   hoặc ConfigMap còn thiếu — sớm hơn và cụ thể hơn là để service tự phát hiện.
3. **`envFrom` nạp cả những khoá ta không khai.** Ai đó thêm một khoá vào
   ConfigMap dùng chung là tiến trình này nhận thêm một biến môi trường mà không
   tệp nào trong kho nhắc tới.

Cái giá: dài hơn, và thêm một biến là thêm **bảy dòng** thay vì không dòng nào.
Với năm biến, đổi lại được tính đọc-hiểu-ngay thì đáng.

### Và vì sao khoá vẫn viết `GẠCH_DƯỚI`

`env:` tường minh **cho phép** khoá viết gạch ngang — tên biến nằm ở `name:`,
tách khỏi `key:`. Kho này **cố ý không dùng quyền tự do ấy**: khoá viết đúng như
tên biến Go.

Một cách viết duy nhất nghĩa là không có bảng dịch nào để gõ sai, **và** cái bẫy
ở trên không còn chỗ để nảy — ngày nào có người rút gọn tệp này thành `envFrom`,
mọi thứ vẫn chạy. Chọn gạch ngang thì đúng ngày ấy là ngày sự cố, và người rút
gọn sẽ không có lý do gì để ngờ.

Nếu về sau kho này **đổi sang `envFrom`**: khoá **bắt buộc** giữ `GẠCH_DƯỚI`, và
phải ghi ngay tại chỗ vì sao — đó là điều kiện để `envFrom` không im lặng.

---

## Tạo Secret và ConfigMap

**Cách khuyến nghị — `--from-file`.** Giá trị không bao giờ nằm trong dòng lệnh,
nên không vào lịch sử shell và không hiện trong `ps` của người khác trên cùng máy.

```
# 1. Ghi từng giá trị vào một tệp tạm. printf '%s' — KHÔNG phải echo:
#    echo thêm một ký tự xuống dòng vào cuối, ký tự ấy đi thẳng vào Secret, và
#    một "\n" dính đuôi DSN gây lỗi kết nối mà nhìn vào Secret không thấy gì lạ.
umask 077
printf '%s' 'DSN-THAT-DAN-VAO-DAY'        > /tmp/dsn
printf '%s' 'SECRET-KEY-THAT-DAN-VAO-DAY' > /tmp/zalo-key

# 2. Tạo Secret từ tệp.
kubectl -n <namespace> create secret generic vihat-miniapp-bi-mat \
  --from-file=DATABASE_DSN=/tmp/dsn \
  --from-file=ZALO_MINIAPP_SECRET_KEY=/tmp/zalo-key

# 3. XOÁ NGAY hai tệp tạm.
shred -u /tmp/dsn /tmp/zalo-key 2>/dev/null || rm -f /tmp/dsn /tmp/zalo-key

# 4. ConfigMap — ba giá trị này không bí mật, dòng lệnh là chỗ hợp lý cho chúng.
kubectl -n <namespace> create configmap vihat-miniapp-cau-hinh \
  --from-literal=ZALO_MINIAPP_APP_ID='<app-id>' \
  --from-literal=CORS_ALLOWED_ORIGINS='<origin-1>,<origin-2>' \
  --from-literal=LISTEN_ADDR=':8080'
```

**Nếu vẫn muốn `--from-literal` cho Secret** — biết trước cái giá:

```
kubectl -n <namespace> create secret generic vihat-miniapp-bi-mat \
  --from-literal=DATABASE_DSN='<dsn>' \
  --from-literal=ZALO_MINIAPP_SECRET_KEY='<secret-key>'
```

Giá trị nằm trong **argv** (mọi tiến trình trên máy đọc được bằng `ps`) và nằm
lại trong **lịch sử shell** (`~/.bash_history`, `~/.zsh_history`) — một tệp
không mã hoá, thường trôi vào bản sao lưu thư mục nhà. Cách chặn:

```
# Cách 1 — gõ MỘT DẤU CÁCH trước lệnh, shell sẽ không ghi nó vào lịch sử.
#          Cần HISTCONTROL có "ignorespace" (bash) hoặc setopt HIST_IGNORE_SPACE (zsh).
export HISTCONTROL=ignorespace:ignoredups

# Cách 2 — lỡ gõ rồi thì xoá dòng vừa gõ, rồi ghi lịch sử xuống đĩa NGAY.
history -d "$(history 1 | awk '{print $1}')" && history -w
```

Cách 2 chỉ xoá trong phiên đang mở: đóng terminal trước khi `history -w` thì
dòng ấy vẫn được ghi xuống. Và **đổi secret là việc phải làm** nếu nó đã kịp vào
một tệp nào đó không kiểm soát được.

### Thứ tự áp dụng

```
kubectl apply -f deploy/service.yaml            # sau khi điền <namespace>
kubectl apply -f deploy/deployment.yaml         # sau khi điền <namespace> và ảnh
kubectl apply -f deploy/cronjob-don-nhat-ky.yaml
```

Secret và ConfigMap phải **có trước** Deployment: `optional: false` nghĩa là pod
sẽ ở `CreateContainerConfigError` cho tới khi khoá xuất hiện. Đó là fail closed,
đúng như mong muốn.

**Đổi giá trị trong Secret/ConfigMap KHÔNG tự khởi động lại pod.** Biến môi
trường được đọc đúng một lần lúc container khởi động. Sau khi sửa:

```
kubectl -n <namespace> rollout restart deployment/vihat-miniapp
```

Quên bước này là giá trị mới nằm trong cụm còn tiến trình vẫn chạy giá trị cũ —
và không có gì trên màn hình nói ra điều đó.

---

## Thăm dò — vì sao ba probe không dùng chung một đường dẫn

`/healthz` **có chạm CSDL** (`internal/httpapi/healthz.go`): nó trả `503` khi
không ping được. Nghĩa của nó là **"còn nói chuyện được với CSDL"**, không phải
"tiến trình còn sống". Hai nghĩa ấy dẫn tới ba quyết định khác nhau:

| Probe | Dùng gì | Vì sao |
|---|---|---|
| `startupProbe` | `/healthz`, 60 × 5s | Pod chưa coi là khởi động xong chừng nào chưa chạm được CSDL. Trong lúc probe này còn chạy, readiness/liveness **bị tạm ngưng**, nên một CSDL lên chậm không bị hiểu nhầm thành pod hỏng |
| `readinessProbe` | `/healthz` | Mất CSDL thì pod **rời khỏi Service**, không nhận yêu cầu mới |
| `livenessProbe` | **TCP, không phải `/healthz`** | Xem dưới |

**Vì sao `livenessProbe` KHÔNG dùng `/healthz`.** Liveness trả lời đúng một câu:
*tiến trình này có treo hẳn không*. Nếu nó hỏi "CSDL còn sống không" thì một sự
cố CSDL sẽ khiến kubelet **giết mọi bản sao**, cả cụm vào `CrashLoopBackOff`, và
khi CSDL trở lại thì các pod đang nằm trong backoff nên còn phải chờ thêm mới
phục vụ lại. **Giết một tiến trình khoẻ mạnh không sửa được một CSDL đang chết —
nó chỉ kéo dài sự cố**, đúng vào lúc người ta cần dịch vụ quay lại nhanh nhất.
Cổng TCP trả lời đúng câu liveness cần hỏi.

**Cái giá của `readinessProbe` chạm CSDL, nói trước:** CSDL chết toàn cục thì
**mọi** bản sao cùng `NotReady`, Service còn 0 endpoint, và người dùng nhận lỗi
của tầng ingress chứ không nhận được câu tiếng Việt `502` mà API đã soạn sẵn.
Đây là đánh đổi có chủ ý: một bản sao không chạm được CSDL thì không phục vụ nổi
một lượt đăng nhập nào: giữ nó trong vòng phục vụ chỉ để trả lỗi đẹp hơn là giữ
một bản sao đã hỏng, và nó còn che mất sự cố khỏi cảnh báo của cụm.

---

## CronJob dọn nhật ký — NỢ #7

`cronjob-don-nhat-ky.yaml` chạy **hằng ngày** đúng hai câu lệnh mà
`make don-nhat-ky` chạy: tạo trước phân mảnh tuần tới, rồi DROP phân mảnh đã quá
90 ngày. Con số 90 **không có trong manifest**: nó là mặc định của hàm
`nhat_ky_don_qua_han` trong `migrations/0002` — nguồn duy nhất. Viết lại 90 vào
đây là tạo bản sao thứ hai, và bản sao thứ hai là bản sẽ lệch.

| Điều | Giá trị | Vì sao |
|---|---|---|
| Tần suất | **hằng ngày**, `15 3 * * *` | Khoảng cách giữa hai lần chạy cộng **thẳng** vào tuổi dòng cũ nhất. Hằng tuần biến trần 90 thành 97 — vượt qua chính câu in trong Chính sách riêng tư |
| `timeZone` | `Asia/Ho_Chi_Minh` | Cần k8s **>= 1.27**. Cụm cũ hơn thì **xoá dòng ấy**: lịch đọc theo UTC, vẫn hằng ngày nên trần không đổi |
| `concurrencyPolicy` | `Forbid` | Hai lần dọn chồng nhau sẽ tranh nhau DROP cùng một phân mảnh |
| `startingDeadlineSeconds` | 3600 | Lỡ cửa sổ vẫn chạy bù trong một giờ. Bỏ hẳn một ngày là cộng một ngày vào trần |
| `failedJobsHistoryLimit` | 7 (> số lần thành công giữ lại) | Một lần dọn hỏng là thứ không ai nhận ra, cho tới khi có người đi hỏi vì sao nhật ký còn dữ liệu tháng trước |
| Ảnh | `postgres:16-alpine` | Chỉ cần máy khách `psql`. Nhét `psql` vào ảnh phục vụ nghĩa là mọi pod Internet-facing đều mang sẵn công cụ nói chuyện thẳng với CSDL |

**Thứ CronJob này KHÔNG tự lo được — nói thẳng:** không có gì **cảnh báo** khi
nó ngừng chạy. CronJob bị xoá, `suspend: true`, Job hỏng bảy ngày liền: nhật ký
âm thầm sống quá 90 ngày, và triệu chứng đầu tiên là một câu hỏi từ phía kiểm
tra. `make check` không thấy được chuyện này, và manifest cũng không.
→ **Cần một cảnh báo** trên `kube_job_status_failed` / "không có Job thành công
nào trong 36 giờ". Chưa có trong kho này (xem "Những gì thư mục này KHÔNG có").

Kiểm tay sau khi áp dụng:

```
kubectl -n <namespace> get cronjob vihat-miniapp-don-nhat-ky
kubectl -n <namespace> create job --from=cronjob/vihat-miniapp-don-nhat-ky thu-mot-lan
kubectl -n <namespace> logs job/thu-mot-lan
```

**Hạn chế đã biết:** `psql "$DATABASE_DSN"` đặt DSN vào **argv**, nên nó nhìn
thấy được bằng `ps` **bên trong chính pod đó**. Chấp nhận, vì pod ấy chỉ chạy
đúng một tiến trình và DSN vốn đã nằm trong môi trường của nó. Muốn bỏ hẳn thì
phải tách DSN thành `PGHOST`/`PGUSER`/`PGDATABASE` + `PGPASSFILE` — đánh đổi lại
là DSN dạng cụm bị xé thành nhiều mảnh, tức mất đúng thứ mục dưới đây bảo vệ.

---

## `DATABASE_DSN` — dạng cụm ngay từ dòng đầu

Phần host là **danh sách** `host:port` ngăn bằng dấu phẩy, **kể cả khi hôm nay
chỉ có một node** (hình dạng đầy đủ: `secret.example.yaml`). Viết một host rồi
"sau thêm sau" là thứ trông đúng suốt thời gian còn một node và sai đúng vào
ngày chuyển sang HA — lúc ấy cụm có nhiều thành viên mà dịch vụ chỉ nói chuyện
được với một, và triệu chứng không giống lỗi cấu hình chút nào.

| Tham số | Vì sao có trong mẫu |
|---|---|
| `target_session_attrs=read-write` | Bỏ qua replica chỉ-đọc, luôn tìm primary. Thiếu nó, một lần failover có thể để service nối vào replica và **mọi lượt đăng nhập hỏng ở câu INSERT đầu tiên** |
| `sslmode=verify-full` | Mã hoá **và** xác thực máy chủ |
| `connect_timeout=5` | Thiếu nó thì một node chết làm lời gọi treo tới tận timeout của HTTP server |

`verify-full` cần CA: thêm `&sslrootcert=/etc/ssl/pg/ca.crt` vào DSN và mount CA
từ một ConfigMap vào đường dẫn ấy. Không mount được CA thì tối thiểu là
`sslmode=require` — và phải biết rằng `require` **chỉ mã hoá**, nó không chứng
minh bạn đang nói chuyện với đúng CSDL của mình.

`pgxpool` đọc thẳng DSN này (`internal/store/kho.go`); thêm `pool_max_conns=N`
vào chuỗi nếu cần giới hạn kết nối cho mỗi bản sao.

---

## Ba đường vào của cấu hình

Cùng năm biến ấy, hai đích đến. Thứ tự ưu tiên và cách chạy ở máy local:
xem **README gốc**, mục "Cấu hình: ba đường vào" — đó là nguồn duy nhất của
phần ấy, đừng chép lại vào đây.

Điều cần nhớ ở phía cụm: **`.env.local` không tồn tại trên cụm và cũng không
được cần tới.** Nhị phân không biết đọc tệp cấu hình nào cả — nó chỉ đọc môi
trường, và trên cụm môi trường ấy đến từ Secret/ConfigMap qua `env:` ở trên.

---

## Những gì thư mục này KHÔNG có — và phải quyết trước khi phát hành

| Thiếu | Hệ quả nếu bỏ qua |
|---|---|
| **Ingress / TLS** | Chưa có đường vào từ Internet. Zalo Mini App chỉ gọi được qua **HTTPS**. **HAI đường phải mở, không phải một:** `/api/v1/sessions` cho Mini App, và `/webhooks/zalo` cho hạ tầng Zalo gọi vào. Quên đường thứ hai thì webhook nhận 404 và **Zalo tắt nó** — mà lúc ấy không có gì báo cho ai, nó chỉ lặng đi |
| **Khai báo proxy tin cậy** | Đứng sau ingress thì `RemoteAddr` là IP của proxy: cả thế giới **chung một xô** trong bộ giới hạn theo IP, và `nhat_ky_dang_nhap.dia_chi_ip` ghi nhầm IP. Xem NỢ #5 ở README gốc. **Đừng** bật tin `X-Forwarded-For` trước khi khai proxy tin cậy — ai cũng giả được header đó |
| **NetworkPolicy** | Mọi pod trong namespace gọi thẳng được vào cổng 8080 |
| **PodDisruptionBudget** | Một lần drain node có thể hạ cả hai bản sao cùng lúc |
| **Cảnh báo cho CronJob** | Lịch dọn ngừng chạy mà **không ai biết** — xem mục CronJob |
| **Chạy migration lúc triển khai** | `make migrate` vẫn là việc chạy tay. Lược đồ **chưa từng được áp dụng lần nào** (NỢ #8) — chạy nó trên một Postgres thật trước khi tin manifest này |
| **HPA** | Số bản sao cố định. Nhớ: bộ giới hạn theo IP đếm trong bộ nhớ, nên **thêm bản sao là nhân trần lên** |
