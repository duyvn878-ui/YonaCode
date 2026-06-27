// Tên file: 2_miner_core/go_bridge/diagnostics.go
// Tính năng chi tiết: Cung cấp bộ công cụ tự chẩn đoán lỗi khởi động (pre-flight checks), kiểm tra cổng mạng bị chiếm dụng và xử lý đóng cửa sổ console thân thiện (chặn silent crash).
// Ngày khởi tạo: 04/06/2026
// Cơ chế vận hành: 
//   1. Hàm CheckPortsFree thực hiện bắt đầu listen thử trên các cổng chỉ định. Nếu thất bại (trả về error), ta biết cổng đó đang bị tiến trình khác chiếm dụng.
//   2. Hàm FatalExit đảm nhiệm in cảnh báo màu đỏ trực tiếp ra terminal đồng thời ghi log vào tệp `node_debug.log`. Nếu người dùng nhấp đúp từ Windows (len(os.Args) == 1), chương trình sẽ dừng lại chờ phím bấm (Scanln) thay vì thoát ngang khiến cửa sổ biến mất ngay lập tức.

package go_bridge

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/fatih/color"
)

// FindAvailableP2PPort tự động tìm một cổng P2P trống trong dải từ basePort đến basePort + 10.
// Tại sao: Nếu cổng mặc định (như 9000) bị kẹt, thay vì dừng app ngay lập tức (FatalExit),
// ta sẽ tự động dò tìm dải cổng kế tiếp để tối ưu hóa khả năng hoạt động đa Node trên một máy.
func FindAvailableP2PPort(basePort int) (int, error) {
	for port := basePort; port <= basePort+10; port++ {
		// Kiểm tra cả TCP và UDP
		if errTCP := checkTCPPortFree(port); errTCP == nil {
			if errUDP := checkUDPPortFree(port); errUDP == nil {
				return port, nil // Cổng này trống, dùng được!
			}
		}
		log.Printf("[AUTO-PORT] Cổng %d bị chiếm dụng, đang thử cổng %d...", port, port+1)
	}
	return 0, fmt.Errorf("không thể tìm thấy cổng P2P nào trống từ dải %d đến %d", basePort, basePort+10)
}

// CheckPortsFree kiểm tra xem các cổng mạng quan trọng có thực sự khả dụng hay không trước khi khởi chạy Node.
// Tại sao: Nếu khởi động mà không check trước, các lỗi bind port trong goroutine chạy ngầm sẽ gây sập (panic) 
// hoặc kết nối lỗi mà không có thông báo cụ thể cho người dùng cuối. Việc check trước giúp đưa ra cảnh báo chính xác.
func CheckPortsFree(port, grpcPort, p2pPort, sclPort int) error {
	// 1. Kiểm tra HTTP RPC / Web UI Port (TCP)
	// Tại sao: Đây là cổng mặc định phục vụ Web UI và API (8080). Rất nhiều dịch vụ web phổ biến (Docker, Apache, Tomcat) thường dùng cổng này.
	if err := checkTCPPortFree(port); err != nil {
		return fmt.Errorf("cổng HTTP/Web UI %d đã bị chiếm dụng bởi ứng dụng khác.\nGợi ý: Vui lòng kiểm tra và đóng các ứng dụng như Skype, Docker, Torrent, IIS, hoặc các Node YonaCode Go khác đang chạy ngầm.", port)
	}

	// 2. Kiểm tra gRPC Port (TCP)
	// Tại sao: Cổng gRPC phục vụ giao diện RPC tương tác nâng cao (thường là 18080).
	if err := checkTCPPortFree(grpcPort); err != nil {
		return fmt.Errorf("cổng gRPC nội bộ %d đã bị chiếm dụng bởi tiến trình khác.\nGợi ý: Đảm bảo không có phiên bản Node cũ nào đang chạy ngầm trên máy tính này.", grpcPort)
	}

	// 3. Kiểm tra P2P Port (TCP & UDP)
	// Tại sao: Libp2p sử dụng cả TCP và UDP (cho giao thức QUIC/Mplex) trên cùng một cổng (9000). Ta cần kiểm tra cả hai.
	if err := checkTCPPortFree(p2pPort); err != nil {
		return fmt.Errorf("cổng P2P (TCP) %d đã bị chiếm dụng.\nGợi ý: Cổng này phục vụ kết nối mạng lưới ngang hàng P2P. Vui lòng tắt các phần mềm VPN hoặc client P2P khác.", p2pPort)
	}
	if err := checkUDPPortFree(p2pPort); err != nil {
		return fmt.Errorf("cổng P2P (UDP) %d đã bị chiếm dụng.\nGợi ý: Kiểm tra lại cấu hình tường lửa hoặc kết nối mạng UDP của hệ thống.", p2pPort)
	}

	// 4. Kiểm tra SCL Port (TCP)
	// Tại sao: Cổng giao tiếp gRPC giữa Go Core và Rust Kernel (scl_server.exe) (thường là 50080). Nếu cổng này bị kẹt, Go không thể ra lệnh cho Rust.
	if err := checkTCPPortFree(sclPort); err != nil {
		return fmt.Errorf("cổng SCL Rust Core %d đã bị chiếm dụng.\nGợi ý: Có thể một tiến trình 'scl_server.exe' từ lần chạy trước chưa tắt hoàn toàn. Hãy mở Task Manager (Ctrl+Shift+Esc), tìm 'scl_server.exe' và chọn End Task.", sclPort)
	}

	return nil
}

// checkTCPPortFree thử bind cổng TCP để kiểm tra tính khả dụng.
func checkTCPPortFree(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	l.Close()
	return nil
}

// checkUDPPortFree thử bind cổng UDP để kiểm tra tính khả dụng.
func checkUDPPortFree(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	l, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	l.Close()
	return nil
}

// FatalExit xử lý lỗi nghiêm trọng, ghi log kép (console + file) và giữ cửa sổ terminal không bị tự tắt.
// Tại sao: Trên Windows, khi click trực tiếp vào file .exe, nếu chương trình crash và gọi os.Exit(1),
// cửa sổ Windows Terminal/cmd sẽ lập tức tắt đi, khiến người dùng không biết bị lỗi gì. 
// Cơ chế này sẽ chặn việc tắt cửa sổ bằng cách yêu cầu người dùng nhấn Enter nếu phát hiện chạy bằng cách nhấp đúp (len(os.Args) == 1).
func FatalExit(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	// 1. Ghi lỗi vào file log chính thông qua Lumberjack để lưu lại vết kỹ thuật
	log.Printf("[FATAL-EXIT] 💀 %s", msg)

	// 2. In trực tiếp ra terminal với màu đỏ nổi bật để người dùng nhìn thấy ngay
	color.Red("\n==================================================================")
	color.Red("🚨 [LỖI KHỞI CHẠY NGHIÊM TRỌNG] 🚨")
	color.Red("  %s", msg)
	color.Red("==================================================================")

	// 3. Phát hiện chế độ nhấp đúp chạy trực tiếp (os.Args chỉ có 1 phần tử là đường dẫn file)
	if len(os.Args) == 1 {
		color.Yellow("\n[THÔNG BÁO] Hệ thống gặp sự cố và không thể tiếp tục vận hành.")
		color.Cyan("Vui lòng sửa lỗi theo gợi ý trên, sau đó nhấn phím Enter để thoát...")
		var dummy string
		fmt.Scanln(&dummy)
	}

	// 4. Kết thúc tiến trình chính thức
	os.Exit(1)
}
