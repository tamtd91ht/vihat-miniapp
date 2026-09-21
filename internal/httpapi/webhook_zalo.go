package httpapi

import "net/http"

// DuongDanWebhookZalo là đường dẫn khai trong Zalo Developer Console.
//
// Một HẰNG chứ không phải chuỗi rải rác: đường dẫn này có mặt ở mã, ở test, và ở một ô nhập
// trên Console. Hai chỗ đầu phải không bao giờ lệch nhau; chỗ thứ ba là chỗ con người phải
// đối chiếu bằng mắt, nên nó cần một nơi duy nhất để đối chiếu VỚI.
//
// KHÔNG nằm dưới `/api/v1`: tiền tố ấy là bề mặt Mini App gọi lên, và mọi tuyến ở đó đều nói
// về MỘT người dùng đang mở app. Webhook này không có người dùng nào — người gọi là hạ tầng
// của Zalo. Một đường dẫn trông giống bề mặt người dùng là đường dẫn người đến sau sẽ mắc
// vào chuỗi middleware của bề mặt ấy.
const DuongDanWebhookZalo = "/webhooks/zalo"

// VÌ SAO ENDPOINT NÀY Ở KHO NÀY chứ không ở ViGov — ADR 0032 của kho `vigov-v2`.
//
// Nó TỪNG nằm ở `service-platform` của ViGov, biện hộ bằng câu "platform = nhà cung cấp vận
// hành". Câu ấy nói về nhà cung cấp vận hành NỀN TẢNG ViGov — sổ đăng ký xã, vòng đời xã,
// siêu dữ liệu — chứ không phải "mọi thứ thuộc nhà cung cấp".
//
// Phép thử đã chốt: một bề mặt tích hợp Zalo thuộc hệ thống nào là do KHOÁ BÍ MẬT NÀO KÝ NÓ
// quyết định.
//
//	webhook + sự kiện của MINI APP   ký bằng app secret của bên đứng tên app  -> kho NÀY
//	đổi accessToken/phoneToken       cùng app secret ấy                       -> kho NÀY
//	ZNS gửi từ OA của TỪNG XÃ        ký bằng khoá của xã                      -> ViGov
//
// Dòng thứ ba đáng gạch chân: KHÔNG suy rộng thành "mọi thứ dính chữ Zalo đều thuộc kho này".
//
// Cái giá của việc để nó ở ViGov, đo được chứ không phải cảm tính: `service-platform` là
// service các dịch vụ khác quay số tới để phân giải `Host` -> xã, nó chết thì MỌI XÃ TRẢ 404,
// nên nó chạy 3 bản sao với `maxUnavailable: 0`. Sửa một dòng xử lý cảnh báo của Zalo mà phải
// lăn lại đúng service ấy là tiêu lượt triển khai đắt nhất của một hệ thống hành chính cho một
// việc không thuộc xã nào.
//
// KHÔNG ghi một CON SỐ dịch vụ ở đây, có chủ ý: con số ấy đổi mỗi lần thêm hoặc bớt một
// service, và một con số nằm trong chú thích thì không có gì bắt nó phải đúng — kho ViGov
// đang có đúng lỗi ấy ở ba chỗ. Dữ kiện chịu lực là "platform chết thì mọi xã 404", và nó
// đúng với mọi con số.

// mountWebhookZalo đăng ký bộ nhận webhook của Zalo Mini App.
//
// ⚠ NÓ TRẢ 200 CHO MỌI THỨ, KỂ CẢ GET VÀ THÂN HỎNG — và đó là chủ ý, không phải một bản nháp
// quên viết nốt.
//
//	Zalo thử lại khi nhận mã lỗi, rồi TẮT HẲN một endpoint cứ hỏng mãi. Hôm nay bộ nhận này
//	không có việc gì để làm — khai URL là một đòi hỏi của hồ sơ đăng ký, không phải một tính
//	năng — nên câu trả lời trung thực duy nhất là "đã nhận".
//
//	Không có `switch` theo phương thức, cũng có lý do: Console có thể thăm dò bằng GET lúc
//	lưu URL rồi mới POST sự kiện về sau, và một 405 trên lượt thăm dò là một URL Console từ
//	chối nhận.
//
// ⚠ NÓ KHÔNG KIỂM CHỨNG BẤT CỨ ĐIỀU GÌ. Ai trên Internet cũng POST vào đây được, và điều đó
// chấp nhận được ĐÚNG CHỪNG NÀO nó còn không đọc gì và không lưu gì. NGÀY NÓ BẮT ĐẦU XỬ LÝ
// PAYLOAD, nó cần phép kiểm chữ ký của Zalo — và khuôn chữ ký thật cho loại webhook này
// CHƯA AI TRA: phải lấy từ Developer Console hoặc tài liệu, không lấy từ trí nhớ. Người viết
// dòng hành vi đầu tiên viết luôn phép kiểm ấy trong CÙNG một thay đổi, nếu không endpoint
// này thành một đường ghi không xác thực.
//
// ⚠ THÂN YÊU CẦU KHÔNG BAO GIỜ ĐƯỢC ĐỌC VÀ KHÔNG BAO GIỜ ĐƯỢC GHI LOG. Payload sự kiện của
// Zalo mang định danh người dùng; một dòng gỡ lỗi in cả thân sẽ đẩy chúng vào log tập trung,
// vào bản sao lưu và vào một nhà cung cấp giám sát bên thứ ba cùng một lúc — từ đó không thu
// lại được. Không có gì để được từ việc đọc một thân ta không dùng.
func (s *Server) mountWebhookZalo(mux *http.ServeMux) {
	mux.HandleFunc(DuongDanWebhookZalo, s.nhanWebhookZalo)
}

// nhanWebhookZalo trả lời mọi yêu cầu bằng 200 và thân rỗng.
//
// KHÔNG đi qua GioiHanIP: bộ giới hạn bảo vệ tuyến đăng nhập khỏi người dò số điện thoại.
// Ở đây không có gì để dò, và một cơn bão sự kiện thật từ Zalo bị siết lại sẽ thành đúng
// chuỗi lỗi khiến Zalo tắt webhook.
func (s *Server) nhanWebhookZalo(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
