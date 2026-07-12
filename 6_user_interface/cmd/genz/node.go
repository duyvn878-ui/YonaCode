package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"btc_genz/2_miner_core/go_bridge"
	node_p2p "btc_genz/5_node_p2p"
	user_interface "btc_genz/6_user_interface"
	"btc_genz/6_user_interface/i18n"
	"btc_genz/6_user_interface/internal"
	pb_block "btc_genz/proto"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"gopkg.in/natefinch/lumberjack.v2"
)

// --- NHÓM LỆNH NODE (NODE ROOT COMMAND) ---
var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "🖥️ Quản lý Máy chủ Node (Vanguard Protocol)",
}

// openBrowser tự động mở trình duyệt đến URL chỉ định tùy theo hệ điều hành đang chạy.
// Lý do thiết kế: Tạo trải nghiệm mượt mà, tự động hóa mở Web UI cho người dùng khi chạy exe.
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "darwin": // macOS
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("hệ điều hành %s không hỗ trợ mở trình duyệt tự động", runtime.GOOS)
	}

	if err != nil {
		log.Printf("[UX] ⚠️ Không thể mở trình duyệt tự động: %v", err)
	}
}

// checkIfNodeRunning thăm dò xem có Node  nào đang hoạt động tại cổng chỉ định hay không.
// Lý do thiết kế: Ngăn chặn việc chạy đè làm sập thợ đào/lõi cũ đang vận hành ổn định trên máy người dùng.
func checkIfNodeRunning(port int) bool {
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
	}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/status", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	netVal, ok := result["network"].(string)
	// [TƯƠNG THÍCH NGƯỢC] Chấp nhận cả tên mạng cũ "BTC GenZ", "BYTECODE GO" và mới "YonaCode Go" để tránh kẹt cổng khi chạy xen kẽ phiên bản.
	return ok && (strings.Contains(netVal, "BTC GenZ") || strings.Contains(netVal, "BYTECODE GO") || strings.Contains(netVal, "YonaCode Go"))
}

// cleanupOldProcesses quét dọn sạch các tiến trình Rust core (scl_server) và miner (genz_miner) 
// cũ từ lần chạy trước để tránh xung đột cổng và lỗi khóa tệp binary trên Windows.
func cleanupOldProcesses() {
	if runtime.GOOS == "windows" {
		// Tắt các tiến trình Rust core và Miner cũ đang chạy ngầm
		exec.Command("taskkill", "/f", "/im", "scl_server.exe").Run()
		exec.Command("taskkill", "/f", "/im", "genz_miner.exe").Run()
	} else {
		exec.Command("killall", "-9", "scl_server").Run()
		exec.Command("killall", "-9", "genz_miner").Run()
	}
	// Chờ 500ms để hệ điều hành giải phóng hoàn toàn cổng mạng
	time.Sleep(500 * time.Millisecond)
}

// findAvailablePort tự động dò tìm cổng TCP trống bắt đầu từ startPort.
// Nó kiểm tra cả cổng HTTP và cổng gRPC tương ứng (port + 10000) để đảm bảo cả hai đều trống.
func findAvailablePort(startPort int) int {
	port := startPort
	for {
		// Kiểm tra cổng HTTP
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			lis.Close()
			// Kiểm tra cổng gRPC tương ứng
			lisGrpc, errGrpc := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+10000))
			if errGrpc == nil {
				lisGrpc.Close()
				return port
			}
		}
		port++
		// Giới hạn dò quét tối đa 100 cổng tránh vòng lặp vô hạn
		if port > startPort+100 {
			break
		}
	}
	return startPort
}

// --- LỆNH KHỞI CHẠY (NODE START) ---
// Tại sao thiết kế như vậy: Đổi Use thành "start" và đặt Aliases là "run" để đảm bảo 
// tính tương thích ngược với toàn bộ các tập lệnh khởi chạy cũ mà không làm gãy luồng chạy.
var nodeStartCmd = &cobra.Command{
	Use:     "start",
	Aliases: []string{"run"},
	Short:   "🚀 Khởi chạy Node Server",
	Run: func(cmd *cobra.Command, args []string) {
		rewardAddrHex, _ := cmd.Flags().GetString("reward-address")
		port, _ := cmd.Flags().GetInt("port")
		p2pPort, _ := cmd.Flags().GetInt("p2p-port")
		if dbPath == "" {
			dbPath = "node"
		}
		peers, _ := cmd.Flags().GetStringSlice("peers")
		sclPort, _ := cmd.Flags().GetInt("scl-port")
		minerPIN, _ := cmd.Flags().GetString("miner-pin")

		// [VANGUARD-AUTO-PORT] Tự động dò tìm cổng trống hoặc tái sử dụng Node đang chạy nếu dùng cấu hình mặc định (nhấp đúp)
		// Lý do thiết kế: Nếu Node đã chạy trên cổng mặc định (8080), ta chỉ việc mở trình duyệt web lên và thoát tiến trình mới.
		// Điều này ngăn chặn việc mở chồng chéo nhiều tiến trình đen xì và giữ cho hệ thống cũ hoạt động liên tục.
		if !cmd.Flags().Changed("port") {
			if checkIfNodeRunning(port) {
				color.Green("🌐 [UX] Phát hiện một Node YonaCode Go đã hoạt động sẵn sàng tại cổng %d.", port)
				color.Green("🔗 Đang tự động chuyển hướng mở giao diện Web UI: http://localhost:%d", port)
				openBrowser(fmt.Sprintf("http://localhost:%d", port))
				time.Sleep(1 * time.Second)
				return // Thoát an toàn
			}
		}

		// [VANGUARD-AUTO-CLEAN] Tự động giải phóng các tiến trình cũ mồ côi
		cleanupOldProcesses()

		// [VANGUARD-AUTO-PORT] Tự động dò tìm cổng trống nếu dùng cấu hình mặc định (nhấp đúp hoặc không chỉ định cổng cụ thể)
		if !cmd.Flags().Changed("port") {
			oldPort := port
			port = findAvailablePort(port)
			if port != oldPort {
				color.Cyan("🔄 [TỰ ĐỘNG] Cổng HTTP %d bị kẹt. Đã tự động chuyển sang cổng trống: %d", oldPort, port)
			}
		}

		if !cmd.Flags().Changed("p2p-port") {
			// Dò tìm cổng P2P trống
			baseP2P := 9000
			if port != 8080 {
				baseP2P = port + 920 // Dịch chuyển tương đối để tránh xung đột cổng P2P
			}
			availableP2P, err := go_bridge.FindAvailableP2PPort(baseP2P)
			if err == nil {
				p2pPort = availableP2P
			}
		}

		// [V1.2.9 EMERGENCY] Bảo vệ Cổng RPC và P2P khỏi Hố đen Port 0
		if port == 0 {
			port = 8080
		}
		if p2pPort == 0 {
			p2pPort = 9000
		}

		// [V26.1 AUTO-PORT] Ngăn chặn xung đột băm khi khởi chạy đa Node (Đa tiến trình scl_server)
		if sclPort == 0 {
			sclPort = port + 42000
			color.Cyan("⚓ [AUTO-PORT] Đã thiết lập Cổng SCL lõi Thép thành: %d (Đồng bộ với HTTP: %d)", sclPort, port)
		}

		seederToken, _ := cmd.Flags().GetString("seeder-token")
		seedDomain, _ := cmd.Flags().GetString("seed-domain")

		// [V1.3.1] Ưu tiên cờ lệnh, sau đó đến biến môi trường cho chế độ Guardian
		if seederToken == "" {
			seederToken = os.Getenv("CF_TOKEN")
			if seederToken != "" {
				color.Green("🛡️  GUARDIAN MODE: Phát hiện Seeder Token từ biến môi trường. Hệ thống sẽ tự động công bố danh tính lên DNS.")
			}
		} else if seederToken == "off" {
			seederToken = "" // Vô hiệu hóa chủ động
			color.Yellow("👤 FOLLOWER MODE: Chế độ Seeder đã được vô hiệu hóa chủ động qua tham số 'off'.")
		}
		if seedDomain == "" {
			seedDomain = os.Getenv("SEED_DOMAIN")
			if seedDomain == "" {
				seedDomain = "none" // Domain mặc định của mạng lưới (Vô hiệu hóa để an toàn Genesis)
			}
		}

		rewardAddrHex = strings.TrimPrefix(strings.TrimSpace(rewardAddrHex), "0x")
		rewardAddr, _ := hex.DecodeString(rewardAddrHex)
		if len(rewardAddr) == 0 {
			rewardAddr = make([]byte, 32)
		}

		color.Yellow("🔍 [DEBUG] Tham số db-path nhận được: %s", dbPath)

		// [VANGUARD-PORTABLE-FIX] Đảm bảo tính đóng gói khi chạy trên máy khác
		// Chốt chặn 2 lớp: filepath.IsAbs và kiểm tra ký tự ổ đĩa Windows (ví dụ D:\)
		isAbs := filepath.IsAbs(dbPath)
		if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
			isAbs = true // Bổ cứu cho trường hợp filepath.IsAbs thất bại trên Windows
		}

		if !isAbs {
			cwd, err := os.Getwd()
			if err == nil {
				// Neo dbPath vào thư mục làm việc hiện tại (CWD) để đảm bảo dữ liệu đúng vị trí
				dbPath = filepath.Join(cwd, dbPath)
			}
		}

		// Khởi tạo thư mục nếu chưa tồn tại
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			os.MkdirAll(dbPath, 0755)
		}
		color.Cyan("⚓ [DATA-ANCHOR] Dữ liệu được chốt tại: %s", dbPath)

		var minerKey ed25519.PrivateKey
		if len(rewardAddr) == 32 {
			wm := internal.NewWalletManager(filepath.Join(dbPath, "wallets"))
			addrHex := hex.EncodeToString(rewardAddr)
			seed, err := wm.GetSeed(addrHex, minerPIN)
			if err == nil && len(seed) >= 32 {
				minerKey = ed25519.NewKeyFromSeed(seed[:32])
				color.Green("🔑 Đã nạp Private Key cho ví đào: 0x%s", addrHex[:16])
			}
		}

		disableMdns, _ := cmd.Flags().GetBool("disable-mdns")
		mining, _ := cmd.Flags().GetBool("mining")
		miningDevice, _ := cmd.Flags().GetString("mining-device")
		syncMode, _ := cmd.Flags().GetString("sync-mode")
		writeLog, _ := cmd.Flags().GetBool("write-log")
		walletServer, _ := cmd.Flags().GetBool("wallet-server")
		walletToken, _ := cmd.Flags().GetString("wallet-token")

		// Cấu hình Logger Lumberjack động dựa trên cờ `--write-log`
		if writeLog {
			log.SetOutput(&lumberjack.Logger{
				Filename:   filepath.Join(dbPath, "node_system.log"),
				MaxSize:    50, // megabytes
				MaxBackups: 3,
				MaxAge:     28,   // days
				Compress:   true, // nén gzip
			})
			log.Println("[NODE] 📝 Đã kích hoạt ghi log hệ thống ra file node_system.log")
		} else {
			log.SetOutput(os.Stderr)
			log.Println("[NODE] 🕊️ Chế độ bảo vệ SSD: Chỉ ghi nhận log hệ thống ra Console.")
		}

		// [PRE-FLIGHT PORT CHECK & AUTO-P2P]
		// Tại sao: Nếu cổng P2P (mặc định 9000) bị chiếm dụng, ta tự động dò tìm cổng trống tiếp theo (tối đa +10)
		// nhằm tối ưu trải nghiệm khởi chạy, giảm thiểu xung đột khi chạy nhiều Node trên cùng máy.
		availableP2PPort, err := go_bridge.FindAvailableP2PPort(p2pPort)
		if err != nil {
			go_bridge.FatalExit("Lỗi mạng: %v", err)
		}

		if availableP2PPort != p2pPort {
			color.Cyan("🔄 [TỰ ĐỘNG] Cổng %d bị kẹt. Hệ thống tự động chuyển sang cổng %d để tối ưu trải nghiệm!", p2pPort, availableP2PPort)
			p2pPort = availableP2PPort // Cập nhật cổng P2P mới
		}

		if err := go_bridge.CheckPortsFree(port, port+10000, p2pPort, sclPort); err != nil {
			go_bridge.FatalExit("Không thể khởi động Node do xung đột cổng: %v", err)
		}

		app := user_interface.NewCLIApp(dbPath, rewardAddr, minerKey, sclPort)
		if miningDevice != "" {
			miningDevice = strings.ToLower(strings.TrimSpace(miningDevice))
			if miningDevice != "cpu" && miningDevice != "gpu" && miningDevice != "hybrid" {
				log.Fatalf("❌ Lỗi: Thiết bị đào không hợp lệ '%s'. Chỉ chấp nhận: cpu, gpu, hybrid", miningDevice)
			}
			app.SetMiningDevice(miningDevice)
		}
		app.EnableWalletServer(walletServer, walletToken)

		// [VANGUARD-CONTROL] Thiết lập chế độ Node dựa trên cờ lệnh
		if mining {
			app.SetNodeMode("full-mining")
			color.Red("🔥 WARNING: Chế độ KHAI THÁC (Mining) đã được kích hoạt chủ động.")
		} else {
			app.SetNodeMode("verify-only")
			color.Green("🛡️  SAFE MODE: Node hoạt động ở chế độ XÁC THỰC (Verify-Only).")
		}

		// [V1.1.4.2] Cấu hình chế độ Đồng bộ P2P
		if syncMode == "snap" && !node_p2p.EnableSnapshotJumping {
			color.Yellow("⚠️  Cảnh báo: Chế độ 'snap' đã bị vô hiệu hóa rõ ràng trong cấu hình mạng.")
			color.Cyan("🔄 Hệ thống tự động chuyển đổi sang chế độ đồng bộ Toàn phần (Full Sync).")
			syncMode = "full"
		}

		if syncMode == "full" {
			app.SetSyncMode("full")
			color.Yellow("🚜 [SYNC-MODE] Đồng bộ Toàn phần (Full Sync). Sẽ tải và xác minh từng khối từ Genesis.")
		} else {
			app.SetSyncMode("snap")
			color.Cyan("⚡ [SYNC-MODE] Đồng bộ Nhảy cóc (Snap Sync).")
		}

		maxTxsPerBlock, _ := cmd.Flags().GetInt("max-tx-per-block")
		app.StartNode(port, p2pPort, peers, minerPIN, seederToken, seedDomain, disableMdns, writeLog, maxTxsPerBlock)

		// [V38.1] CHỐT CHẶN BẤT BIẾN: Giữ cho Node luôn sống để phục vụ mạng lưới
		select {}
	},
}

// --- LỆNH TRẠNG THÁI (NODE STATUS) ---
var nodeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "📊 " + i18n.T("dashboard_title"),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		resp, err := client.GetStatus(ctx, &pb_block.GetStatusRequest{})
		if err != nil {
			color.Red("❌ Node Offline!")
			return
		}
		color.Green("🚀 Node Online | Cao độ: #%d | Peers: %d | Hashrate: %d H/s", resp.CurrentHeight, resp.PeerCount, resp.Hashrate)
		if resp.IsMining {
			color.Yellow("⚒️  Trạng thái: Đang khai thác khối mới...")
		}
	},
}

// --- LỆNH PHIÊN BẢN (NODE INFO) ---
var nodeInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "ℹ️ Thông tin phiên bản Vanguard",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("YonaCode Go Vanguard Edition V1.0 - Ported 112-byte - FIX ĐẶC NHIỆM 1")
	},
}

// --- LỆNH KẾT NỐI P2P (NODE CONNECT) ---
var nodeConnectCmd = &cobra.Command{
	Use:   "connect [address]",
	Short: "🔗 Gợi ý kết nối tới một Peer (Vanguard P2P)",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		peerAddr := args[0]
		color.Cyan("🔗 Đang yêu cầu tầng mạng P2P kết nối tới: %s", peerAddr)
		color.Yellow("💡 Gợi ý: Quá trình kết nối và bắt tay đồng thuận sẽ diễn ra tự động.")
	},
}

// --- NHÓM LỆNH BẢO TRÌ VÀ CỨU HỘ DATABASE (NODE REPAIR) ---
// Tại sao thiết kế như vậy: Tập hợp toàn bộ các lệnh bảo trì CSDL ngoại tuyến và cứu hộ hệ thống 
// vào nhóm lệnh repair để người dùng dễ ghi nhớ và quản trị tập trung.
var nodeRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "🛠️ Công cụ bảo trì và sửa lỗi Database (Offline Tools)",
}

// 1. Lệnh Quay lui trạng thái (repair rollback)
var repairRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "⏪ Ép Node quay lui về một khối chỉ định (Chế độ Cứu hộ)",
	Run: func(cmd *cobra.Command, args []string) {
		target, _ := cmd.Flags().GetUint64("target")
		sclPort, _ := cmd.Flags().GetInt("scl-port")

		if target == 0 {
			color.Red("❌ Lỗi: Vui lòng chỉ định cao độ đích bằng --target")
			return
		}

		color.Yellow("⚠️  CẢNH BÁO: Quá trình Rollback đang bắt đầu. Đảm bảo Node đã được TẮT hoàn toàn.")

		// Phân giải đường dẫn dbPath chuẩn xác
		isAbs := filepath.IsAbs(dbPath)
		if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
			isAbs = true
		}
		if !isAbs {
			cwd, err := os.Getwd()
			if err == nil {
				dbPath = filepath.Join(cwd, dbPath)
			}
		}

		color.Cyan("📂 Thư mục dữ liệu: %s", dbPath)

		// Khởi động Bridge tạm thời để ép lùi
		bridge := go_bridge.NewBridge(sclPort)

		color.Yellow("📡 Đang nạp Hạt nhân Rust...")
		sclPath := filepath.Join(dbPath, "scl")
		bridge.InitSCL(sclPath)
		time.Sleep(5 * time.Second)

		currentH := bridge.GetCurrentVersion()
		color.Cyan("📊 Cao độ hiện tại: #%d | Mục tiêu quay lui: #%d", currentH, target)

		if currentH <= target {
			color.Yellow("ℹ️  Không cần rollback (H#%d <= Target#%d)", currentH, target)
			bridge.Close()
			return
		}

		color.Yellow("🕊️ [SOCIAL-CONSENSUS] Đang cưỡng chế hạ Tường lửa Bất biến (Finality Firewall) xuống #%d...", target)
		bridge.ForceSetFinalizedHeight(target)
		time.Sleep(1 * time.Second)

		success := bridge.RollbackState(nil, currentH, target)
		if !success {
			color.Yellow("⚠️  Rollback tiêu chuẩn bị chặn. Kích hoạt [BÀN TAY VÔ HÌNH] cưỡng chế xóa khối vật lý...")
			success = bridge.ForceDeleteBlocks(currentH, target)
		}

		if success {
			color.Green("✅ THÀNH CÔNG! Node đã được đưa về khối #%d.", target)
		} else {
			color.Red("❌ THẤT BẠI! Không thể thực hiện Rollback. Vui lòng kiểm tra log SCL.")
		}

		bridge.Close()
		color.Green("🏁 Đã hoàn tất cứu hộ. Bạn có thể khởi động lại Node ngay bây giờ.")
	},
}

// 2. Lệnh Dọn dẹp dữ liệu lịch sử (repair cleanup)
var repairCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "🧹 Dọn dẹp dữ liệu lịch sử (Historical Purge)",
	Run: func(cmd *cobra.Command, args []string) {
		start, _ := cmd.Flags().GetUint64("start")
		end, _ := cmd.Flags().GetUint64("end")
		sclPort, _ := cmd.Flags().GetInt("scl-port")

		if end == 0 {
			color.Red("❌ Lỗi: Vui lòng chỉ định cao độ kết thúc bằng --end")
			return
		}

		color.Yellow("🧹 Đang thực hiện dọn dẹp dữ liệu từ khối #%d đến #%d...", start, end)

		// Phân giải đường dẫn dbPath
		isAbs := filepath.IsAbs(dbPath)
		if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
			isAbs = true
		}
		if !isAbs {
			cwd, err := os.Getwd()
			if err == nil {
				dbPath = filepath.Join(cwd, dbPath)
			}
		}

		bridge := go_bridge.NewBridge(sclPort)
		defer bridge.Close()

		sclPath := filepath.Join(dbPath, "scl")
		bridge.InitSCL(sclPath)
		time.Sleep(5 * time.Second)

		success := bridge.PurgeOldHistory(start, end)
		if success {
			color.Green("✅ THÀNH CÔNG! Dữ liệu lịch sử đã được dọn dẹp.")
		} else {
			color.Red("❌ THẤT BẠI! Không thể thực hiện dọn dẹp. Vui lòng kiểm tra log SCL.")
		}
	},
}

// 3. Lệnh Thanh tẩy sổ cái (repair purify)
var repairPurifyCmd = &cobra.Command{
	Use:   "purify",
	Short: "☢️ Thanh tẩy Sổ cái (Nuclear Ledger Purification)",
	Long:  "Xóa sạch toàn bộ Accounts/JMT và tái thiết lập trạng thái SMT từ lịch sử Block Headers.",
	Run: func(cmd *cobra.Command, args []string) {
		color.Red("\n[NUCLEAR] ☢️ ĐANG TIẾN HÀNH THANH TẨY SỔ CÁI...")
		sclPort, _ := cmd.Flags().GetInt("scl-port")

		// Phân giải đường dẫn dbPath
		isAbs := filepath.IsAbs(dbPath)
		if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
			isAbs = true
		}
		if !isAbs {
			cwd, err := os.Getwd()
			if err == nil {
				dbPath = filepath.Join(cwd, dbPath)
			}
		}

		br := go_bridge.NewBridge(sclPort)
		defer br.Close()

		sclPath := filepath.Join(dbPath, "scl")
		br.InitSCL(sclPath)
		time.Sleep(5 * time.Second)

		height := br.GetCurrentVersion()
		success, err := br.EmergencyStateRebuild(height)
		if err != nil || !success {
			color.Red("❌ THANH TẨY THẤT BẠI: %v", err)
			return
		}

		color.Green("✅ THANH TẨY THÀNH CÔNG! Sổ cái đã được đưa về trạng thái liêm chính tại khối #%d", height)
		actual := br.CalculateActualTotalSupply()
		color.Cyan("💰 Tổng cung thực tế sau thanh tẩy: %.2f GO", float64(actual)/100000000.0)
	},
}

// 4. Lệnh Tái đồng bộ trạng thái (repair resync)
var repairResyncCmd = &cobra.Command{
	Use:   "resync",
	Short: "🔄 Tái đồng bộ trạng thái sổ cái (State Re-sync)",
	Run: func(cmd *cobra.Command, args []string) {
		color.Yellow("\n[RE-SYNC] 🔄 Đang chuẩn bị tái thực thi Blockchain từ bộ nhớ đệm...")
		dataRoot, _ := cmd.Flags().GetString("data-root")
		sclPort, _ := cmd.Flags().GetInt("scl-port")

		// Phân giải đường dẫn dbPath
		isAbs := filepath.IsAbs(dbPath)
		if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
			isAbs = true
		}
		if !isAbs {
			cwd, err := os.Getwd()
			if err == nil {
				dbPath = filepath.Join(cwd, dbPath)
			}
		}

		br := go_bridge.NewBridge(sclPort)
		defer br.Close()

		sclPath := filepath.Join(dbPath, "scl")
		br.InitSCL(sclPath)
		time.Sleep(5 * time.Second)

		// Lấy version hiện tại TRƯỚC KHI reset database
		highest, err := br.GetHighestBlockHeight()
		if err != nil {
			color.Red("❌ Không thể lấy cao độ khối lớn nhất trong RocksDB: %v", err)
			return
		}

		// [RE-SYNC-CLEANUP] Reset hoàn toàn trạng thái JMT/Accounts cũ trong database
		color.Yellow("[RECOVERY] 🧹 Đang dọn sạch trạng thái JMT & Accounts cũ trong Sổ cái...")
		if success, err := br.ResetStateCompletely(); err != nil || !success {
			color.Red("⚠️ Cảnh báo: Không thể dọn sạch trạng thái cũ tự động: %v. Quá trình re-sync có thể gặp lỗi.", err)
		} else {
			color.Green("✅ Đã dọn sạch trạng thái cũ thành công. Bắt đầu từ Genesis sạch.")
		}

		// Reset Expected Supply về 0 để bắt đầu tính toán lại
		br.SetExpectedSupply(0)
		color.Yellow("[RECOVERY] 🛠️ Đã đặt lại Cán cân Kế toán về 0 để chuẩn bị tái thực thi tuyệt đối.")

		// Tìm snapshot thích hợp để nhảy cóc
		var snapshotVersion uint64 = 0
		var snapshotPath string = ""

		// Snapshot dir thường nằm ở cùng cấp thư mục dbPath hoặc trong dataRoot
		snapDir := filepath.Join(dataRoot, "snapshots")
		if _, err := os.Stat(snapDir); os.IsNotExist(err) {
			snapDir = filepath.Join(filepath.Dir(sclPath), "snapshots")
		}

		if files, err := os.ReadDir(snapDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && strings.HasPrefix(file.Name(), "epoch_") && strings.HasSuffix(file.Name(), ".snap") {
					name := file.Name()
					valStr := name[len("epoch_") : len(name)-len(".snap")]
					var v uint64
					if _, err := fmt.Sscanf(valStr, "%d", &v); err == nil {
						if v <= highest && v > snapshotVersion {
							snapshotVersion = v
							snapshotPath = filepath.Join(snapDir, name)
						}
					}
				}
			}
		}

		var startHeight uint64 = 0
		if snapshotVersion > 0 && snapshotPath != "" {
			color.Yellow("[RECOVERY] 🚀 Phát hiện file snapshot tại khối #%d. Đang nạp nhanh...", snapshotVersion)
			root := br.ImportStateSnapshotPath(snapshotPath, snapshotVersion)
			if len(root) > 0 {
				color.Green("✅ Nạp snapshot thành công! Trạng thái tại #%d đã được khôi phục. Root: 0x%s", snapshotVersion, hex.EncodeToString(root[:4]))
				startHeight = snapshotVersion + 1
			} else {
				color.Red("❌ Nạp snapshot thất bại. Quá trình re-sync sẽ bắt đầu từ Genesis #0.")
			}
		}

		if startHeight > 0 {
			color.Cyan("📊 Bắt đầu Re-sync các khối từ #%d đến #%d...", startHeight, highest)
		} else {
			color.Cyan("📊 Phát hiện %d khối trong lịch sử Rust Core. Bắt đầu Re-sync...", highest+1)
		}

		var processedCount int = 0
		for h := startHeight; h <= highest; h++ {
			blockRaw := br.GetBlock(h)
			if blockRaw == nil {
				color.Red("⚠️ Bỏ qua khối #%d do không tìm thấy dữ liệu trong Rust Core", h)
				continue
			}

			var block pb_block.Block
			if err := proto.Unmarshal(blockRaw, &block); err != nil {
				color.Red("⚠️ Bỏ qua khối #%d do lỗi giải mã Proto: %v", h, err)
				continue
			}

			if block.Header == nil {
				continue
			}
			headerBytes, _ := proto.Marshal(block.Header)
			bodyBytes, _ := proto.Marshal(block.Body)

			parentHash := block.Header.ParentHash.Value
			minerAddr := block.Header.MinerAddress.Value

			// Tái thực thi khối
			var txHashes [][]byte
			if bodyBytes != nil {
				var body pb_block.BlockBody
				if err := proto.Unmarshal(bodyBytes, &body); err == nil {
					for _, tx := range body.Transactions {
						txData, _ := proto.Marshal(tx)
						h_full := br.GetCanonicalTxHash(txData, h)
						txHashes = append(txHashes, h_full)
					}
				}
			}

			stateRoot, success, _, _, err := br.ExecuteBlockTransactions(nil, bodyBytes, txHashes, minerAddr, parentHash, h, block.Header.Timestamp, false)

			if !success || err != nil {
				color.Red("❌ Thất bại tại khối #%d: %v (Success: %v)", h, err, success)
				color.Yellow("💡 Gợi ý: Kiểm tra log của Rust SCL để biết chi tiết lỗi")
				break
			}

			headerHash := br.GetCanonicalBlockHeaderHash(headerBytes, block.Header.Height)
			br.SaveBlockRaw(h, headerHash, blockRaw, true)

			if h%10 == 0 || h == highest {
				color.Green("✅ Đã xử lý khối #%d | Root: 0x%s...", h, hex.EncodeToString(stateRoot[:4]))
			}
			processedCount++
		}

		color.Green("\n✨ HOÀN TẤT TÁI ĐỒNG BỘ!")
		totalActual := br.CalculateActualTotalSupply()
		color.Cyan("💰 Tổng số GO đang lưu hành thực tế: %.2f GO", float64(totalActual)/100000000.0)
	},
}

func init() {
	// 1. Cấu hình cờ riêng cho từng lệnh con của repair
	repairRollbackCmd.Flags().Uint64("target", 0, "Cao độ đích muốn quay về (Target Height)")
	repairCleanupCmd.Flags().Uint64("start", 0, "Cao độ bắt đầu dọn dẹp")
	repairCleanupCmd.Flags().Uint64("end", 0, "Cao độ kết thúc dọn dẹp")
	repairResyncCmd.Flags().String("data-root", "./data", "Thư mục chứa dữ liệu Go (Headers/Bodies/Snapshots)")

	// 2. Đăng ký các cờ bảo trì toàn cục cho nhóm repair (PersistentFlags)
	// Để cho sạch sẽ, cờ database --scl-port được đóng gói trong PersistentFlags của repairCmd
	nodeRepairCmd.PersistentFlags().Int("scl-port", 55555, "Cổng SCL tạm thời cho bảo trì (SCL Temp Port)")

	// Gắn các lệnh con vào repairCmd
	nodeRepairCmd.AddCommand(repairRollbackCmd, repairCleanupCmd, repairPurifyCmd, repairResyncCmd)

	// 3. Đăng ký các cờ khởi chạy trực tiếp bằng Flags() cho nodeStartCmd
	nodeStartCmd.Flags().String("reward-address", "0000000000000000000000000000000000000000000000000000000000000000", "Ví nhận thưởng (Hex 32 bytes)")
	nodeStartCmd.Flags().Int("port", 8080, "Cổng RPC/Web UI")
	nodeStartCmd.Flags().StringSlice("peers", []string{}, "Peer khởi tạo")
	nodeStartCmd.Flags().String("miner-pin", "", "PIN bảo mật ví")
	nodeStartCmd.Flags().Int("scl-port", 0, "Cổng SCL (Auto-Port nếu = 0)")
	nodeStartCmd.Flags().String("seeder-token", "", "Token Cloudflare API để kích hoạt chế độ Guardian (Seeder)")
	nodeStartCmd.Flags().String("seed-domain", "seed.ghostcoi.com", "Tên miền danh sách hạt giống để cập nhật các Node sống")
	nodeStartCmd.Flags().Int("p2p-port", 9000, "Cổng mạng P2P")
	nodeStartCmd.Flags().Bool("disable-mdns", false, "Vô hiệu hóa khám phá nội bộ mDNS")
	nodeStartCmd.Flags().Bool("mining", false, "Kích hoạt thợ đào (Mặc định là TẮT)")
	nodeStartCmd.Flags().String("mining-device", "cpu", "Thiết bị khai thác: cpu, gpu, hoặc hybrid")
	nodeStartCmd.Flags().String("sync-mode", "full", "Chế độ đồng bộ: 'full' hoặc 'snap'")
	nodeStartCmd.Flags().Bool("write-log", false, "Kích hoạt ghi log vật lý ra file đĩa cứng")
	nodeStartCmd.Flags().Int("max-tx-per-block", 1000, "Giới hạn số giao dịch tối đa được phép đóng gói vào mỗi khối")
	nodeStartCmd.Flags().Bool("wallet-server", false, "Kích hoạt cổng kết nối RPC cho ví Yona Wallet")
	nodeStartCmd.Flags().String("wallet-token", "", "Token bảo mật xác thực kết nối ví")

	// Đăng ký các lệnh con vào nodeCmd
	nodeCmd.AddCommand(nodeStartCmd, nodeStatusCmd, nodeInfoCmd, nodeConnectCmd, nodeRepairCmd)

	// Đăng ký nodeCmd vào rootCmd toàn cục
	rootCmd.AddCommand(nodeCmd)
}
