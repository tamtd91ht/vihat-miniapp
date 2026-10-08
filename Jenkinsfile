// Pipeline của `vihat-miniapp`.
//
//   · build CÁI GÌ   — đúng một ảnh, `vihat-miniapp`
//   · build KHI NÀO  — mọi lần `main` nhích lên. Xem §"Vì sao KHÔNG có bộ lọc dựng lại"
//   · đẩy ĐI ĐÂU     — hằng số REGISTRY/PROJECT trong khối `environment` ngay dưới
//   · chạy lên ĐÂU    — hằng số KUBECONFIG/NS/TEN_K8S cùng khối
//
// TỪ 08/10/2026 JOB ĐẶT ẢNH LÊN CỤM (chủ dự án: "build jenkins thì k8s tự kéo ảnh từ harbor về
// deploy luôn"), cùng cách các job dịch vụ của ViGov đang chạy: `kubectl set image` +
// `rollout status`, đỏ thì `rollout undo`. Chỉ đổi ảnh, KHÔNG `kubectl apply`: Deployment,
// Service, Secret, ConfigMap vẫn dựng tay (deploy/README.md). KHÔNG chạy migration — xem cuối tệp.

pipeline {
  agent any

  options {
    timestamps()
    timeout(time: 20, unit: 'MINUTES')
    buildDiscarder(logRotator(numToKeepStr: '30'))
    // Hai lượt song song đẩy cùng một thẻ ảnh là hai lượt ghi đè nhau ở Harbor, và bản
    // thắng là bản ngẫu nhiên.
    disableConcurrentBuilds()
  }

  // REGISTRY và PROJECT là HẰNG SỐ, không phải tham số của lượt chạy.
  //
  // Chúng từng là `parameters {}`, và hệ quả là mỗi lần bấm Build tay Jenkins đều hiện một form
  // bắt điền lại — `defaultValue` chỉ tự áp khi build do SCM kích hoạt. Một cái form phải điền
  // mỗi lượt để luôn điền đúng một giá trị là ma sát thuần tuý.
  //
  // Và với hệ thống hành chính còn hơn thế: registry chọn được lúc bấm nút nghĩa là câu "ảnh
  // này nằm ở đâu" phụ thuộc vào thứ người bấm gõ vào ô nhập, một thứ không nằm trong kho mã và
  // không ai xem lại được sau sáu tuần. Đổi registry là ĐỔI MÃ, đi qua commit và review.
  environment {
    REGISTRY = 'harbor.omicrm.services'
    PROJECT  = 'ci'
    DOCKER_BUILDKIT = '1'
    TEN_ANH = 'vihat-miniapp'

    // Cùng cụm, cùng namespace với ViGov: cầu phiên tới identity đi không TLS nên chỉ chấp nhận
    // được khi hai bên cùng namespace (ADR 0045 của ViGov). KUBECONFIG là tệp trên đĩa máy chủ
    // mà các job ViGov đang dùng; cạnh nó là `rancher-omi.yaml` của dự án khác — đừng chép nhầm.
    KUBECONFIG = '/u01/rancher/rancher-vigov.yaml'
    NS = 'vigov-prod'
    // Tên Deployment THẬT trên cụm (dựng tay trong Rancher). Khác tên này thì stage 'Chuẩn bị'
    // dừng trước khi dựng và in cách tìm tên đúng.
    TEN_K8S = 'vihat-miniapp'
  }

  stages {

    stage('Chuẩn bị') {
      steps {
        // BỐN công cụ, và `gcc` nằm trong danh sách vì một lý do không hiển nhiên: `make
        // check` chạy `go test -race`, mà `-race` cần cgo, tức cần một trình biên dịch C.
        // Thiếu nó thì lỗi rơi vào giữa cổng kiểm dưới dạng "race is only supported on ...
        // with cgo" — một dòng trông như mã hỏng chứ không phải như máy chủ thiếu công cụ.
        // Hỏng sớm, và nói đúng thứ bị thiếu.
        sh 'command -v go && command -v docker && command -v make && command -v gcc'

        // In ra để lượt build tự làm chứng về môi trường nó chạy. Hai con số cần nhìn:
        //   · Go phải ≥ 1.25 (`go 1.25.0` trong go.mod). Thấp hơn thì hoặc GOTOOLCHAIN tự tải
        //     bản mới — cần mạng — hoặc `go vet` đỏ với "go.mod requires go >= 1.25".
        //   · Docker phải ≥ 18.09, ngưỡng có BuildKit. Dockerfile dùng `# syntax=` và
        //     `--mount=type=cache`; docker 1.13.1 (bản mặc định của CentOS 7) không hiểu cả
        //     hai, và stage đóng ảnh sẽ đỏ SAU KHI cổng kiểm đã xanh.
        sh 'go version; docker version --format "server {{.Server.Version}} · client {{.Client.Version}}" || docker version'

        // MÁY CHỦ ĐÃ ĐĂNG NHẬP REGISTRY CHƯA. Stage đóng ảnh không `docker login` — nó dựa vào
        // phiên đăng nhập sẵn có của user `jenkins` (xem lý do đầy đủ ở stage ấy). Đó là một
        // trạng thái nằm ngoài kho này, nên nó phải được kiểm ra mặt chứ không được giả định.
        //
        // Kiểm ở đây chứ không để `docker push` tự đỏ: push đỏ sau khi đã dựng xong ảnh, tức
        // mất hai phút, và câu nó in ra là "denied: requested access to the resource is denied"
        // — đọc như lỗi phân quyền của tài khoản, chứ không như "máy này chưa đăng nhập bao giờ".
        sh '''
          cfg="${DOCKER_CONFIG:-$HOME/.docker}/config.json"
          if ! grep -q "$REGISTRY" "$cfg" 2>/dev/null; then
            echo ""
            echo "MÁY CHỦ BUILD CHƯA ĐĂNG NHẬP $REGISTRY"
            echo "Pipeline này cố ý không mang credentials: nó dùng phiên đăng nhập sẵn của"
            echo "user jenkins, giống các job khác trên máy chủ. Chạy MỘT LẦN dưới user ấy:"
            echo "    sudo -u jenkins docker login $REGISTRY"
            echo "Hoặc, nếu muốn kho này có danh tính đẩy ảnh riêng, tạo một mục credentials"
            echo "kiểu Username with password ở phạm vi Global rồi bọc bước push bằng"
            echo "withCredentials — xem chú thích ở stage 'Đóng ảnh'."
            echo ""
            exit 1
          fi
        '''
        // CỤM kiểm RA MẶT, TRƯỚC khi dựng: thiếu KUBECONFIG thì `kubectl` rơi về `~/.kube/config`
        // (có thể là cụm khác), và thiếu Deployment thì một lượt tốn cả cổng kiểm lẫn đóng ảnh rồi
        // mới biết không có chỗ đặt. In NGUYÊN VĂN lỗi của cụm: Forbidden và NotFound sửa ở hai chỗ.
        sh '''
          set -eu
          command -v kubectl >/dev/null 2>&1 || { echo "MÁY CHỦ BUILD THIẾU: kubectl"; exit 1; }
          if [ ! -r "$KUBECONFIG" ]; then
            echo "KHÔNG ĐỌC ĐƯỢC KUBECONFIG: $KUBECONFIG"
            exit 1
          fi
          echo "cụm: $(kubectl config current-context) · namespace: $NS"
          if ! loi=$(kubectl -n "$NS" get deploy/"$TEN_K8S" -o name 2>&1); then
            echo ""
            echo "cụm trả lời: $loi"
            echo "KHÔNG ĐỌC ĐƯỢC deploy/$TEN_K8S TRONG NAMESPACE $NS."
            echo "Deployment trong Rancher mang tên khác? Xem:  kubectl -n $NS get deploy | grep -i miniapp"
            echo "rồi sửa hằng số TEN_K8S trong Jenkinsfile này."
            echo ""
            exit 1
          fi
        '''
        script {
          // Cờ cho `post { failure }`: chỉ rút lại khi CỤM ĐÃ ĐỔI.
          env.DA_DAT = 'chua'
          env.TAG = sh(script: 'git rev-parse --short=12 HEAD', returnStdout: true).trim()
          if (!env.TAG) { error('Không lấy được commit hiện tại — không có thẻ ảnh nào để đặt.') }
          echo "vihat-miniapp · commit ${env.TAG}"
        }
      }
    }

    // GỌI `make check`, KHÔNG CHÉP LẠI BA LỆNH CỦA NÓ.
    //
    // Chép ra đây thì dự án có HAI định nghĩa của "đã kiểm": một cái người chạy ở máy, một
    // cái CI chạy — và bản LỎNG HƠN luôn là bản thắng, vì nó là bản không ai thấy đỏ.
    //
    // `make check` cố ý KHÔNG nạp `.env.local` và các ca chạm CSDL tự SKIP khi thiếu
    // `TEST_DATABASE_DSN`, nên nó chạy được trên máy chủ build mà không cần Postgres.
    stage('Cổng kiểm') {
      steps {
        // CẢNH BÁO, KHÔNG CHẶN. Go của máy chủ build và Go đóng ảnh (`ARG GO_VERSION` trong
        // Dockerfile) là hai thứ khác nhau: cái sau kho này ghim được, cái trước thì không —
        // Jenkins dùng chung với dự án khác, phiên bản Go trên đó không phải quyết định của
        // kho này. Chặn ở đây là chặn một lượt phát hành vì một thứ không ai ở đây sửa được.
        //
        // Nhưng cũng KHÔNG im lặng: lệch phiên bản là cách CI xanh trên một toolchain còn ảnh
        // dựng bằng toolchain khác, và ngày nó cắn thì dòng này là thứ duy nhất trong log nói
        // trước được. `tr -d` vì kho này được sửa trên Windows và không có .gitattributes.
        sh '''
          mong="$(sed -n 's/^ARG GO_VERSION=//p' Dockerfile | tr -d '\\r')"
          case "$(go env GOVERSION)" in
            go"$mong"|go"$mong".*) ;;
            *) echo "CANH BAO: cong kiem chay Go $(go env GOVERSION), anh dung ARG GO_VERSION=$mong" ;;
          esac
        '''

        sh 'make check'
      }
    }

    // KHÔNG DÙNG `docker.build` / `docker.withRegistry`. Hai lệnh ấy thuộc plugin Docker
    // Pipeline, và Jenkins này KHÔNG CÓ nó: lượt chạy ngày 2026-09-21 chết ngay lúc biên dịch
    // Jenkinsfile với "Invalid agent type docker. Must be one of [any, label, none]" — ba tên
    // ấy là những loại agent Jenkins core tự biết, tức không plugin nào đăng ký thêm loại nào.
    //
    // Máy chủ Jenkins dùng chung với dự án khác, nên "cài thêm plugin" không phải quyết định
    // của kho này. `docker` CLI là thứ có sẵn.
    //
    // KHÔNG `docker login`, KHÔNG credentials — VÀ ĐÓ LÀ MỘT PHỤ THUỘC, không phải một chỗ
    // thiếu. Máy chủ Jenkins này đã đăng nhập sẵn và lâu dài vào Harbor bằng tài khoản của user
    // `jenkins`: token nằm trong `~jenkins/.docker/config.json`, do ai đó đăng nhập một lần và
    // không phiên bản hoá ở đâu cả. Pipeline `cloud-vihat-saas-omicrm-callbot-service` trên
    // cùng máy chủ đẩy ảnh đúng theo cách này và đã chạy nhiều tháng — 21/09/2026 người dùng
    // chốt dùng chung cơ chế ấy thay vì tạo mục credentials riêng.
    //
    // CÁI GIÁ, ghi ra để người sau biết mình đang đổi cái gì lấy cái gì:
    //   · Nhật ký chỉ trả lời được "Jenkins đẩy", không trả lời được "TÀI KHOẢN NÀO đẩy".
    //   · Ngày token trên máy chủ hết hạn hoặc bị thu hồi, MỌI job đóng ảnh đỏ cùng lúc, và
    //     không kho nào chứa manh mối vì trạng thái ấy không nằm trong kho nào.
    // Muốn lấy lại danh tính riêng cho kho này: thêm một mục credentials kiểu Username with
    // password ở phạm vi Global rồi bọc phần `sh` bên dưới bằng `withCredentials`.
    //
    // Vì phụ thuộc ấy vô hình, stage 'Chuẩn bị' kiểm nó tường minh — hỏng sớm với đúng câu
    // giải thích, thay vì đỏ ở `docker push` sau hai phút dựng với "denied: requested access to
    // the resource is denied", một dòng đọc như lỗi phân quyền chứ không như máy chưa đăng nhập.
    stage('Đóng ảnh') {
      steps {
        script { env.ANH = "${env.REGISTRY}/${env.PROJECT}/${env.TEN_ANH}:${env.TAG}" }

        sh '''
          # --pull: lấy bản mới nhất của `golang:1.26-bookworm` và của ảnh nền runtime. Không có
          # nó, một ảnh nền đã nằm sẵn trong cache máy chủ từ nhiều tuần trước sẽ được dùng lại,
          # và bản vá CVE của ảnh nền không bao giờ vào tới ảnh phát hành.
          docker build --pull --build-arg VERSION="$TAG" -t "$ANH" -f Dockerfile .
          docker push "$ANH"

          # Bỏ thẻ ảnh khỏi máy chủ sau khi đã đẩy. Máy dùng chung, và mỗi commit sinh một thẻ
          # mới — không dọn thì đĩa đầy vì kho này. Chỉ bỏ THẺ; các tầng nằm lại trong cache và
          # lượt sau vẫn dựng nhanh.
          docker image rm "$ANH" || true
        '''

        // Ghi lại NGAY SAU khi push thành công (bước `sh` ở trên đỏ thì không tới được đây).
        // Đây là nhật ký "commit nào đã thành ảnh", và nó là thứ duy nhất trả lời được câu ấy
        // khi có người hỏi sáu tuần sau.
        script { currentBuild.description = "anh-tu-commit:${env.TAG}" }
        echo "DA DAY  ${env.ANH}"
      }
    }

    stage('Triển khai') {
      steps {
        // Ghi ảnh ĐANG chạy trước khi đổi: sau một lượt đỏ, câu đầu tiên là "trước đó chạy bản nào".
        sh '''
          set -eu
          echo "trước lượt này: $(kubectl -n "$NS" get deploy/"$TEN_K8S" \
              -o jsonpath='{.spec.template.spec.containers[*].image}')"
        '''

        // TỪ DÒNG NÀY CỤM ĐÃ ĐỔI — từ đây `post { failure }` mới được phép rút lại.
        script { env.DA_DAT = 'roi' }

        // `*=`: mọi container của Deployment — tên container trên Deployment dựng tay không chắc
        // là `server` như manifest, và Deployment này chỉ có một container.
        sh 'kubectl -n "$NS" set image deploy/"$TEN_K8S" "*=$ANH"'
        // startupProbe của manifest cho tối đa 60 × 5s chờ CSDL.
        sh 'kubectl -n "$NS" rollout status deploy/"$TEN_K8S" --timeout=6m'

        script { currentBuild.displayName = "#${env.BUILD_NUMBER} ${env.TAG} → ${env.NS}" }
        echo "XONG  ${env.ANH} đang chạy ở ${env.NS}/${env.TEN_K8S}"
      }
    }
  }

  post {
    failure {
      // Chạy cho MỌI stage đỏ, kể cả cổng kiểm. Không có cờ `DA_DAT` thì một test đỏ cũng
      // `rollout undo` — rút lại đúng bản ĐANG CHẠY TỐT.
      script {
        if (env.DA_DAT != 'roi') {
          echo 'Đỏ TRƯỚC khi chạm cụm — không có gì để rút lại.'
        } else {
          sh '''
            set +e
            echo "THẤT BẠI — quay lại bản trước ở cụm"
            kubectl -n "$NS" rollout undo deploy/"$TEN_K8S"
            kubectl -n "$NS" rollout status deploy/"$TEN_K8S" --timeout=5m
            exit 0
          '''
          currentBuild.displayName = "#${env.BUILD_NUMBER} ĐỎ ${env.TAG} → ${env.NS} · đã rollout undo"
        }
      }
    }
  }
}

// ─────────────────────────────────────────────────────────────────────────────────────────
// VÌ SAO KHÔNG CÓ BỘ LỌC DỰNG LẠI — và vì sao KHÔNG chép cơ chế của kho ViGov sang đây.
//
// Mỗi `Jenkinsfile` của ViGov mang một `canDungLai()` khoảng năm mươi dòng: nó tìm ngược lịch
// sử build lấy commit đã thành ảnh, rồi `git diff` xem thư mục của dịch vụ mình có đổi không.
// Cơ chế ấy tồn tại vì ở đó CHÍN JOB DÙNG CHUNG MỘT KHO: một lần push đụng một dịch vụ mà làm
// chín lượt đóng ảnh thì tám lượt là rác.
//
// Kho này có MỘT job và MỘT kho. Mọi commit vào `main` đều là thay đổi của chính dịch vụ này,
// nên bộ lọc sẽ trả `true` ở mọi lượt — tức năm mươi dòng logic, một trạng thái ẩn nằm trong
// `description` của lượt build, và một lớp mà người sau phải đọc xong mới dám sửa, tất cả để
// đi tới đúng câu trả lời mà không có nó cũng có.
//
// Chép một cơ chế vì "kho kia làm thế" là cách một kho nhỏ thừa hưởng chi phí của một kho lớn
// mà không thừa hưởng lý do. Ngày kho này có job thứ hai thì quay lại đây — lúc ấy mới có bài
// toán để giải.
//
// ─────────────────────────────────────────────────────────────────────────────────────────
// HAI THỨ CỐ Ý KHÔNG CHẠY Ở ĐÂY, ghi ra thay vì để người sau tưởng là bỏ sót:
//
//   · `make test-csdl` — cần một Postgres DÙNG RIÊNG đã chạy cả hai migration, và các ca ấy
//     DROP phân mảnh. Cắm nó vào CI mà không cấp CSDL riêng thì hoặc nó luôn SKIP (một cổng
//     không kiểm gì), hoặc nó DROP nhầm phân mảnh của một CSDL thật.
//
//   · `make migrate` — di trú ở kho này KHÔNG chạy trong tiến trình lúc khởi động (khác
//     ViGov). Nó là một bước có người bấm, trước khi lăn bản mới. Một job đóng ảnh mà tự chạy
//     migration là một job đổi lược đồ của môi trường thật mà không ai yêu cầu.
// ─────────────────────────────────────────────────────────────────────────────────────────
