// Pipeline của `vihat-miniapp`.
//
//   · build CÁI GÌ   — đúng một ảnh, `vihat-miniapp`
//   · build KHI NÀO  — mọi lần `main` nhích lên. Xem §"Vì sao KHÔNG có bộ lọc dựng lại"
//   · đẩy ĐI ĐÂU     — tham số registry của riêng lượt chạy này
//
// PHẠM VI DỪNG Ở ẢNH. Không `kubectl apply`, không đụng cụm — đó là việc của devops, và một
// job vừa đóng ảnh vừa triển khai là một job không ai dám bấm lại.

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

  parameters {
    string(name: 'REGISTRY', defaultValue: 'registry.vihat.vn',
           description: 'Máy chủ Harbor, không kèm https://')
    string(name: 'PROJECT', defaultValue: 'vihat',
           description: 'Project trong Harbor — ảnh là <REGISTRY>/<PROJECT>/vihat-miniapp')
    string(name: 'REGISTRY_CRED', defaultValue: 'harbor-vihat',
           description: 'ID credentials trong Jenkins. CHỈ LÀ ID, không bao giờ là giá trị thật.')
  }

  environment {
    DOCKER_BUILDKIT = '1'
    TEN_ANH = 'vihat-miniapp'
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
        script {
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
    // của kho này. `docker` CLI cộng `withCredentials` là thứ có ở mọi bản Jenkins, và đổi lại
    // ta phải tự làm ba việc plugin vốn làm hộ — cả ba đều ghi lý do tại chỗ bên dưới.
    stage('Đóng ảnh') {
      steps {
        script { env.ANH = "${params.REGISTRY}/${params.PROJECT}/${env.TEN_ANH}:${env.TAG}" }

        // (1) DOCKER_CONFIG riêng cho từng lượt build. `docker login` mặc định ghi vào
        // ~/.docker/config.json của user `jenkins` — MỘT tệp dùng chung cho mọi job trên máy.
        // Trên một Jenkins dùng chung, `docker logout` ở cuối lượt này sẽ đá văng phiên đăng
        // nhập của job dự án khác đang đẩy ảnh giữa chừng. Tách thư mục cấu hình thì không ai
        // đụng ai, và xoá thư mục ở `post` chính là logout.
        withEnv(["DOCKER_CONFIG=${env.WORKSPACE}/.docker-cau-hinh"]) {

          // (2) Mật khẩu registry KHÔNG BAO GIỜ nằm trên dòng lệnh và không đi qua nội suy
          // Groovy: script để trong nháy đơn (Groovy không nội suy), giá trị vào bằng biến môi
          // trường, và `--password-stdin` thay cho `-p`. `set +x` vì bước `sh` của Jenkins chạy
          // `sh -xe`, mà `-x` in ra ĐỐI SỐ ĐÃ KHAI TRIỂN — `echo "$REG_PASS"` sẽ hiện nguyên
          // mật khẩu trong log. Bộ lọc che của Jenkins bắt được, nhưng một bí mật đã ra tới
          // chỗ cần bộ lọc thì chỉ còn một lớp giữa nó và log (luật 8).
          withCredentials([usernamePassword(credentialsId: params.REGISTRY_CRED,
                                            usernameVariable: 'REG_USER',
                                            passwordVariable: 'REG_PASS')]) {
            sh '''
              set +x
              echo "$REG_PASS" | docker login -u "$REG_USER" --password-stdin "$REGISTRY"
              set -x

              # --pull: lấy bản mới nhất của `golang:1.26-bookworm` và của ảnh nền runtime.
              # Không có nó, một ảnh nền đã nằm sẵn trong cache máy chủ từ nhiều tuần trước sẽ
              # được dùng lại, và bản vá CVE của ảnh nền không bao giờ vào tới ảnh phát hành.
              docker build --pull --build-arg VERSION="$TAG" -t "$ANH" -f Dockerfile .
              docker push "$ANH"

              # (3) Bỏ thẻ ảnh khỏi máy chủ sau khi đã đẩy. Máy dùng chung, và mỗi commit sinh
              # một thẻ mới — không dọn thì đĩa đầy vì kho này. Chỉ bỏ THẺ; các tầng nằm lại
              # trong cache và lượt sau vẫn dựng nhanh.
              docker image rm "$ANH" || true
            '''
          }
        }

        // Ghi lại NGAY SAU khi push thành công (bước `sh` ở trên đỏ thì không tới được đây).
        // Đây là nhật ký "commit nào đã thành ảnh", và nó là thứ duy nhất trả lời được câu ấy
        // khi có người hỏi sáu tuần sau.
        script { currentBuild.description = "anh-tu-commit:${env.TAG}" }
        echo "DA DAY  ${env.ANH}"
      }
    }
  }

  post {
    always {
      // `config.json` là tệp `docker login` ghi token registry vào. Xoá ĐÚNG nó chứ không xoá
      // cả thư mục: một lệnh xoá đệ quy dựng từ biến môi trường là một lệnh chỉ đúng chừng nào
      // biến ấy đúng. Chạy cả khi build đỏ — một lượt hỏng GIỮA login và push là đúng lượt để
      // lại token nằm trên đĩa.
      sh 'rm -f "$WORKSPACE/.docker-cau-hinh/config.json"'
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
