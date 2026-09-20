package httpapi

import (
	"sync"
	"time"
)

// GioiHanIP — xô token theo IP cho tuyến đăng nhập.
//
// Vì sao cần: POST /api/v1/sessions là tuyến CÔNG KHAI và mỗi lượt gọi tốn một
// lời gọi sang Zalo. Không giới hạn thì bất kỳ ai cũng biến API của ta thành
// máy bơm lưu lượng vào Zalo, bằng tiền và hạn mức của ta.
//
// GIỚI HẠN CỦA CÁCH LÀM NÀY — nói trước, đừng để ai tưởng đây là tường lửa:
//
//  1. CHỈ ĐÚNG VỚI MỘT TIẾN TRÌNH. Bộ đếm nằm trong bộ nhớ của tiến trình này.
//     Chạy 3 bản sao thì trần thực tế là 3 lần con số cấu hình. Muốn đúng thật
//     thì phải đếm tập trung (Redis) — chưa làm ở bước này.
//  2. KHỞI ĐỘNG LẠI LÀ XOÁ SẠCH bộ đếm.
//  3. Khoá là IP lấy từ RemoteAddr. Đứng sau proxy/CDN thì đó là IP của proxy
//     và cả thế giới chung một xô. Tin X-Forwarded-For khi chưa khai proxy tin
//     cậy còn tệ hơn: ai cũng giả được header đó và bộ giới hạn thành vô dụng.
//  4. Nhiều người sau cùng một NAT (văn phòng, wifi công cộng) dùng chung hạn mức.
type GioiHanIP struct {
	mu       sync.Mutex
	sucChua  float64
	tocDoNap float64 // token mỗi giây
	cuaSo    time.Duration
	xo       map[string]*xoIP
	quetCuoi time.Time

	// now tiêm được: test thời gian bằng đồng hồ thật là test chậm và hay chớp nháy.
	now func() time.Time
}

type xoIP struct {
	con         float64
	capNhat     time.Time
	daBaoTuChoi bool
}

// MoiGioiHan: sucChua lượt trong mỗi cuaSo, nạp lại đều đặn (không phải reset
// theo mốc): nạp đều thì không có "giờ vàng" ngay sau mỗi mốc để dồn gấp đôi.
func MoiGioiHan(sucChua int, cuaSo time.Duration) *GioiHanIP {
	if sucChua < 1 {
		sucChua = 1
	}
	if cuaSo <= 0 {
		cuaSo = time.Minute
	}
	return &GioiHanIP{
		sucChua:  float64(sucChua),
		tocDoNap: float64(sucChua) / cuaSo.Seconds(),
		cuaSo:    cuaSo,
		xo:       make(map[string]*xoIP),
		now:      time.Now,
	}
}

// Cho trả (có cho đi tiếp không, có phải lần từ chối ĐẦU TIÊN của đợt này không).
//
// Giá trị thứ hai để gọi bên ngoài ghi ĐÚNG MỘT dòng nhật ký cho mỗi đợt bị
// chặn. Ghi mọi lượt bị từ chối thì chính tuyến đang bị lạm dụng trở thành
// đường khuếch đại: kẻ tấn công không gọi được Zalo nhưng vẫn bắt ta ghi CSDL.
func (g *GioiHanIP) Cho(khoa string) (bool, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	bayGio := g.now()
	g.quetRac(bayGio)

	x, co := g.xo[khoa]
	if !co {
		x = &xoIP{con: g.sucChua, capNhat: bayGio}
		g.xo[khoa] = x
	}

	// Nạp theo thời gian đã trôi.
	if troi := bayGio.Sub(x.capNhat).Seconds(); troi > 0 {
		x.con += troi * g.tocDoNap
		if x.con > g.sucChua {
			x.con = g.sucChua
		}
		x.capNhat = bayGio
	}

	if x.con < 1 {
		lanDau := !x.daBaoTuChoi
		x.daBaoTuChoi = true
		return false, lanDau
	}

	x.con--
	x.daBaoTuChoi = false
	return true, false
}

// quetRac bỏ các xô đã đầy lại và lâu không dùng.
//
// Không có bước này thì map lớn theo số IP từng ghé qua — một cách rò bộ nhớ mà
// chỉ máy chủ thật, sau vài tuần, mới cho thấy.
func (g *GioiHanIP) quetRac(bayGio time.Time) {
	if bayGio.Sub(g.quetCuoi) < g.cuaSo {
		return
	}
	g.quetCuoi = bayGio
	nguong := bayGio.Add(-2 * g.cuaSo)
	for khoa, x := range g.xo {
		if x.capNhat.Before(nguong) {
			delete(g.xo, khoa)
		}
	}
}
