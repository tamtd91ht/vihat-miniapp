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
        // `make` nằm trong danh sách vì stage sau gọi `make check`. Thiếu nó thì lỗi rơi vào
        // giữa cổng kiểm dưới dạng "make: not found" — một dòng trông như cổng kiểm hỏng chứ
        // không phải như máy chủ build thiếu công cụ. Hỏng sớm, và nói đúng thứ bị thiếu.
        sh 'command -v go && command -v docker && command -v make'
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
        sh 'make check'
      }
    }

    stage('Đóng ảnh') {
      steps {
        script {
          def ten = "${params.REGISTRY}/${params.PROJECT}/${env.TEN_ANH}"
          docker.withRegistry("https://${params.REGISTRY}", params.REGISTRY_CRED) {
            def anh = docker.build("${ten}:${env.TAG}",
                                   "--build-arg VERSION=${env.TAG} -f Dockerfile .")
            anh.push()
            // Ghi lại NGAY SAU khi push thành công. Đây là nhật ký "commit nào đã thành ảnh",
            // và nó là thứ duy nhất trả lời được câu ấy khi có người hỏi sáu tuần sau.
            currentBuild.description = "anh-tu-commit:${env.TAG}"
          }
          echo "DA DAY  ${ten}:${env.TAG}"
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
