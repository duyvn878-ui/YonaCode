/**
 * @file bridge.go
 * @brief Cầu nối gRPC đầy đủ giữa Go và lõi Rust SCL.
 */

package go_bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	go_bridge_pb "btc_genz/proto"
	"google.golang.org/protobuf/proto"

	"context"
)

var (
	ErrCriticalFirewall = fmt.Errorf("FIREWALL_VIOLATION: Node cố gắng thay đổi lịch sử đã finalize")
	ErrDbBusy           = fmt.Errorf("DB_BUSY: Cơ sở dữ liệu đang bận hoặc thiếu dữ liệu tạm thời, thử lại sau")

	// [GLOBAL-BRIDGE-V1] Cho phép các hàm standalone (như CalculateBlake3Hash) gọi Rust Core
	GlobalBridge *Bridge
)

type MaturingReward struct {
	Amount uint64
	Height uint64
}

type AccountSnapshot struct {
	Address         []byte
	Balance         uint64
	Nonce           uint64
	NanoWeight      uint32
	LastFullCleanup uint64
	CoinID          []byte
	MaturingRewards []MaturingReward
}

type Bridge struct {
	mu             sync.Mutex
	chainID        uint32
	sclClient      *SclClient
	minerSclClient *SclClient // [MINER-ISOLATION] Kết nối gRPC biệt lập chuyên dùng cho các tác vụ của Thợ đào
	txClientPool   []*SclClient // [TX-ISOLATION-POOL] Hồ bơi luồng gRPC chuyên biệt cho Mempool & Giao dịch
	txPoolIndex    uint32       // Bộ đếm luân chuyển luồng cho Mempool & Giao dịch
	uiSclClient    *SclClient // [UI-ISOLATION] Kết nối gRPC biệt lập chuyên dùng cho Truy vấn UI / API (Read Only)
	fastSclClient  *SclClient // [FAST-ISOLATION] Kết nối gRPC biệt lập chuyên dùng cho Kiểm duyệt Nhanh P2P (In-Memory CPU)
	adminSclClient *SclClient // [ADMIN-ISOLATION] Kết nối gRPC biệt lập chuyên dùng cho Tác vụ Nặng / Bảo trì
	eventSclClient *SclClient // [EVENT-ISOLATION] Kết nối gRPC thứ 7: Lắng nghe báo động đỏ từ Rust
	eventCancel    context.CancelFunc // Dùng để đóng stream khi restart
	serverCmd      *exec.Cmd
	minerCmd       *exec.Cmd
	gpuMinerCmd    *exec.Cmd
	jobHandle      uintptr // [WINDOWS ONLY]

	lastUpdate    time.Time
	currentRate   uint64
	lastTotalHash uint64 // [V26] Lưu trữ tổng HASHRATE_COUNTER trước đó để tính Delta

	sclPort      int // Lưu lại port để restart
	isRestarting bool
	authToken    string // [SECURITY-HARDENING] Shared Secret Token từ Rust Server

	// [UI-STABILITY-FIX] Cache các thông số quan trọng để chống giật/reset về 0 khi gRPC lag
	cacheMu      sync.RWMutex
	cachedHeight uint64
	cachedSupply uint64

	// [gRPC-RECOVERY-PATH] Cache bổ sung để chống nghẽn gRPC khi sync/reorg chuỗi nặng
	isSyncing            int32  // Sử dụng atomic (0: không sync, 1: đang sync)
	cachedMiningPaused   bool   // Cache trạng thái tạm dừng đào
	cachedBlockReward    uint64 // Cache phần thưởng khối gần nhất
	cachedHighestHeader  []byte // Cache raw header của khối cao nhất
	cachedDifficulty     []byte // Cache độ khó gần nhất

	// [gRPC-RECOVERY-PATH] Lưu trữ dbPath để tự động mở RocksDB khi hồi sinh SCL Server
	dbPath string
}

// SetSyncing bật/tắt cờ trạng thái đồng bộ chuỗi nặng
func (b *Bridge) SetSyncing(syncing bool) {
	if syncing {
		atomic.StoreInt32(&b.isSyncing, 1)
	} else {
		atomic.StoreInt32(&b.isSyncing, 0)
	}
}

// IsSyncing kiểm tra xem lõi có đang bận xử lý đồng bộ hoặc Reorg hay không
func (b *Bridge) IsSyncing() bool {
	return atomic.LoadInt32(&b.isSyncing) == 1
}

// [SECURITY-HARDENING] GetAuthToken trả về Shared Secret Token để gửi trong gRPC metadata
func (b *Bridge) GetAuthToken() string {
	return b.authToken
}

// getTxClient tự động chọn luồng tiếp theo đang rảnh trong hồ bơi txClientPool theo cơ chế Round-Robin
func (b *Bridge) getTxClient() *SclClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.txClientPool) == 0 {
		return b.sclClient // Fallback về luồng chính nếu hồ bơi chưa khởi tạo hoặc sập
	}
	idx := atomic.AddUint32(&b.txPoolIndex, 1) % uint32(len(b.txClientPool))
	return b.txClientPool[idx]
}

// generateSecureToken sinh token ngẫu nhiên 32 bytes an toàn mật mã.
// Bắt buộc kiểm tra lỗi entropy từ hệ điều hành để chống token rỗng.
func generateSecureToken() string {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		log.Fatalf("[CRITICAL] Không thể khởi tạo Entropy bảo mật từ OS: %v", err)
	}
	return hex.EncodeToString(token)
}

func NewBridge(sclPort int) *Bridge {
	b := &Bridge{lastUpdate: time.Now()}

	// [VANGUARD-FIX] Hỗ trợ nhận token từ môi trường (Decoupled Startup)
	if forced := os.Getenv("SCL_FORCE_TOKEN"); forced != "" {
		b.authToken = forced
		log.Printf("[BRIDGE-SECURITY] 🔐 Sử dụng Shared Secret Token từ môi trường.")
	} else {
		b.authToken = generateSecureToken()
		log.Printf("[BRIDGE-SECURITY] 🔐 Go Node đã chủ động tạo Shared Secret Token mới.")
	}

	// [V1.2.6 TCP] Khởi động SCL Server trước khi kết nối gRPC
	if err := b.ensureSclServer(sclPort); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ [CẢNH BÁO KHỞI CHẠY SCL] Không thể khởi động Hạt nhân Rust: %v\n", err)
		log.Printf("[BRIDGE] ⚠️ Cảnh báo khởi động SCL Server: %v", err)
	}

	targetAddr := fmt.Sprintf("127.0.0.1:%d", sclPort)
	log.Printf("[BRIDGE] 📡 Đang kết nối SCL Core qua TCP: %s", targetAddr)

	// Đợi server sẵn sàng
	time.Sleep(1500 * time.Millisecond)

	client, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ❌ Không thể kết nối RPC Core: %v. Đang thử lại...", err)
		time.Sleep(2000 * time.Millisecond)
		client, err = NewSclClient(targetAddr)
	}

	if err != nil {
		FatalExit("Không thể kết nối SCL Core sau các lần thử gRPC: %v.\nGợi ý: Đảm bảo tiến trình 'scl_server' không bị phần mềm diệt virus hoặc tường lửa chặn.", err)
	}

	b.sclClient = client
	b.sclPort = sclPort

	// [MINER-ISOLATION] Khởi tạo kết nối gRPC thứ hai chuyên biệt cho thợ đào
	// Tại sao: Tạo kết nối gRPC thứ hai chuyên biệt cho thợ đào (minerSclClient). Kênh truyền này hoàn toàn độc lập với kênh truyền đồng bộ dữ liệu P2P (sclClient). Nhờ đó, ngay cả khi Node nhận dữ liệu Sync/Reorg cực nặng làm nghẽn stream HTTP/2 của sclClient, các lệnh gRPC gọi từ miner như lấy Block Template hoặc nộp Task vẫn đi qua kênh minerSclClient thông suốt, ngăn ngừa thợ đào bị timeout và dừng đào.
	minerClient, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ⚠️ Cảnh báo: Không thể tạo kết nối gRPC riêng biệt cho Miner, sử dụng chung kết nối chính. Lỗi: %v", err)
		b.minerSclClient = client
	} else {
		b.minerSclClient = minerClient
		b.minerSclClient.SetAuthToken(b.authToken)
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo thành công kết nối gRPC riêng biệt cho Miner.")
	}

	// [TX-ISOLATION-POOL] Khởi tạo hồ bơi 5 luồng gRPC chuyên biệt cho Mempool & Giao dịch
	// Tại sao: Tạo hồ bơi luân phiên (Round-Robin Pool) gồm 5 luồng gRPC chuyên biệt cho giao dịch (txClientPool).
	// Giúp phân tải và xử lý song song các yêu cầu từ sàn giao dịch (EBP) với tần suất cao (ValidateTransactionBatch, AddToMempool, v.v.),
	// ngăn ngừa triệt để hiện tượng nghẽn cổ chai HTTP/2 flow control khi sàn bơm số lượng lớn giao dịch đồng thời.
	poolSize := 5
	for i := 0; i < poolSize; i++ {
		txClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE] ⚠️ Cảnh báo: Không thể tạo luồng gRPC TX thứ %d trong hồ bơi, Lỗi: %v", i, err)
		} else {
			txClient.SetAuthToken(b.authToken)
			b.txClientPool = append(b.txClientPool, txClient)
		}
	}
	if len(b.txClientPool) == 0 {
		log.Printf("[BRIDGE] ⚠️ Hồ bơi luồng TX hoàn toàn trống, fallback về sử dụng kết nối chính.")
		b.txClientPool = append(b.txClientPool, client)
	} else {
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo thành công HỒ BƠI %d luồng gRPC cho Mempool & Giao dịch.", len(b.txClientPool))
	}

	// [UI-ISOLATION] Khởi tạo kết nối gRPC thứ tư chuyên biệt cho Truy vấn UI / API (Read Only)
	uiClient, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ⚠️ Cảnh báo: Không thể tạo kết nối gRPC riêng biệt cho UI, sử dụng chung kết nối chính. Lỗi: %v", err)
		b.uiSclClient = client
	} else {
		b.uiSclClient = uiClient
		b.uiSclClient.SetAuthToken(b.authToken)
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo thành công kết nối gRPC riêng biệt cho UI.")
	}

	// [FAST-ISOLATION] Khởi tạo kết nối gRPC thứ năm chuyên biệt cho Kiểm duyệt Nhanh P2P (In-Memory CPU)
	fastClient, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ⚠️ Cảnh báo: Không thể tạo kết nối gRPC riêng biệt cho Fast Query, sử dụng chung kết nối chính. Lỗi: %v", err)
		b.fastSclClient = client
	} else {
		b.fastSclClient = fastClient
		b.fastSclClient.SetAuthToken(b.authToken)
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo thành công kết nối gRPC riêng biệt cho Fast Query.")
	}

	// [ADMIN-ISOLATION] Khởi tạo kết nối gRPC thứ sáu chuyên biệt cho Tác vụ Nặng / Bảo trì
	adminClient, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ⚠️ Cảnh báo: Không thể tạo kết nối gRPC riêng biệt cho Admin Tasks, sử dụng chung kết nối chính. Lỗi: %v", err)
		b.adminSclClient = client
	} else {
		b.adminSclClient = adminClient
		b.adminSclClient.SetAuthToken(b.authToken)
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo thành công kết nối gRPC riêng biệt cho Admin Tasks.")
	}

	// [EVENT-ISOLATION] Kênh thứ 7: Kênh nghe báo động đỏ từ Rust Core
	eventClient, err := NewSclClient(targetAddr)
	if err != nil {
		log.Printf("[BRIDGE] ⚠️ Không thể tạo kênh Event, dùng chung kết nối chính. Lỗi: %v", err)
		b.eventSclClient = client
	} else {
		b.eventSclClient = eventClient
		b.eventSclClient.SetAuthToken(b.authToken)
		log.Printf("[BRIDGE] 🔐 Đã khởi tạo Kênh Báo Động (Event Stream) từ Rust.")
	}

	// Truyền Auth Token xuống gRPC Client lập tức (Không cần vòng lặp 60s)
	b.sclClient.SetAuthToken(b.authToken)

	// [SINGLETON-INIT] Gán Bridge hiện tại vào global để phục vụ băm đồng nhất
	GlobalBridge = b

	go b.startHashrateTicker()
	go b.startHealthMonitor() // [VANGUARD-HEALTH] Giám sát sự sống
	go b.startEventStreamListener() // [VANGUARD-EVENT] Lắng nghe sự kiện từ Rust
	return b
}

func (b *Bridge) startHealthMonitor() {
	// [VANGUARD-HEALTH-FIX] Tăng nhịp tim lên 10s để tránh báo động giả khi Node đang bận xử lý Batch nặng
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		if b.serverCmd == nil || b.serverCmd.Process == nil {
			continue
		}

		// 1. Kiểm tra tiến trình OS
		state := b.serverCmd.ProcessState
		if state != nil && state.Exited() {
			log.Printf("[HEALTH-CRITICAL] 💀 SCL Server đã thoát! Đang khởi động lại...")
			b.restartServer()
			continue
		}

		// 2. Kiểm tra phản hồi gRPC (Heartbeat) với Timeout lớn hơn (5s thay vì 2s)
		if !b.IsSyncing() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := b.sclClient.client.GetFinalizedHeight(ctx, &go_bridge_pb.Empty{})
			cancel()

			if err != nil {
				// [VANGUARD-HEALTH-FIX] Không restart ngay lập tức nếu lỗi là "Canceled" hoặc "DeadlineExceeded"
				// vì có thể server chỉ đang bận (Heavy Load). Chỉ restart nếu lỗi kết nối thực sự.
				log.Printf("[HEALTH-WARN] ⚠️ SCL Server đang bận hoặc không phản hồi gRPC: %v.", err)

				// Kiểm tra lỗi có phải do "connection refused" hay không (Hỗ trợ tốt hơn trên Windows/connectex)
				errStr := err.Error()
				if strings.Contains(errStr, "connection refused") ||
					strings.Contains(errStr, "refused") ||
					strings.Contains(errStr, "connectex") ||
					strings.Contains(errStr, "all SubConns are in TransientFailure") {
					log.Printf("[HEALTH-RECOVERY] 🚑 Phát hiện mất kết nối vật lý. Đang khởi động lại SCL Server...")
					b.restartServer()
				}
			}
		} else {
			log.Printf("[HEALTH-SYNC] 🕊️ Lõi Rust đang bận thực hiện tác vụ nặng (Sync/Reorg). Bỏ qua kiểm tra nhịp tim gRPC.")
		}
	}
}

func (b *Bridge) restartServer() {
	b.mu.Lock()
	if b.isRestarting {
		b.mu.Unlock()
		return
	}
	b.isRestarting = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.isRestarting = false
		b.mu.Unlock()
	}()

	// Đóng client cũ
	if b.eventCancel != nil {
		b.eventCancel()
	}
	if b.sclClient != nil && b.sclClient.conn != nil {
		b.sclClient.conn.Close()
	}
	if b.minerSclClient != nil && b.minerSclClient != b.sclClient && b.minerSclClient.conn != nil {
		b.minerSclClient.conn.Close()
	}
	for _, txClient := range b.txClientPool {
		if txClient != nil && txClient != b.sclClient && txClient.conn != nil {
			txClient.conn.Close()
		}
	}
	b.txClientPool = nil
	if b.uiSclClient != nil && b.uiSclClient != b.sclClient && b.uiSclClient.conn != nil {
		b.uiSclClient.conn.Close()
	}
	if b.fastSclClient != nil && b.fastSclClient != b.sclClient && b.fastSclClient.conn != nil {
		b.fastSclClient.conn.Close()
	}
	if b.adminSclClient != nil && b.adminSclClient != b.sclClient && b.adminSclClient.conn != nil {
		b.adminSclClient.conn.Close()
	}
	if b.eventSclClient != nil && b.eventSclClient != b.sclClient && b.eventSclClient.conn != nil {
		b.eventSclClient.conn.Close()
	}

	// Giết process cũ nếu còn
	if b.serverCmd != nil && b.serverCmd.Process != nil {
		_ = b.serverCmd.Process.Kill()
	}
	if b.minerCmd != nil && b.minerCmd.Process != nil {
		_ = b.minerCmd.Process.Kill()
	}
	if b.gpuMinerCmd != nil && b.gpuMinerCmd.Process != nil {
		_ = b.gpuMinerCmd.Process.Kill()
	}

	log.Printf("[BRIDGE-RECOVERY] ♻️ Đang hồi sinh SCL Server tại cổng %d...", b.sclPort)
	if b.authToken == "" {
		b.authToken = generateSecureToken()
	}
	if err := b.ensureSclServer(b.sclPort); err != nil {
		log.Printf("[BRIDGE-RECOVERY] ❌ Hồi sinh thất bại: %v", err)
		return
	}

	targetAddr := fmt.Sprintf("127.0.0.1:%d", b.sclPort)
	client, err := NewSclClient(targetAddr)
	if err == nil {
		client.SetAuthToken(b.authToken)
		b.sclClient = client

		// [MINER-ISOLATION] Khởi tạo lại kết nối gRPC riêng biệt cho Miner khi hồi sinh server
		minerClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại kết nối gRPC cho Miner, sử dụng chung kết nối chính. Lỗi: %v", err)
			b.minerSclClient = client
		} else {
			minerClient.SetAuthToken(b.authToken)
			b.minerSclClient = minerClient
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại kết nối gRPC riêng biệt cho Miner.")
		}

		// [TX-ISOLATION-POOL] Khởi tạo lại hồ bơi 5 luồng gRPC chuyên biệt cho Mempool & Giao dịch khi hồi sinh server
		poolSize := 5
		for i := 0; i < poolSize; i++ {
			txClient, err := NewSclClient(targetAddr)
			if err != nil {
				log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại luồng gRPC TX thứ %d trong hồ bơi. Lỗi: %v", i, err)
			} else {
				txClient.SetAuthToken(b.authToken)
				b.txClientPool = append(b.txClientPool, txClient)
			}
		}
		if len(b.txClientPool) == 0 {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Hồ bơi luồng TX trống khi hồi sinh, fallback sử dụng kết nối chính.")
			b.txClientPool = append(b.txClientPool, client)
		} else {
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại thành công HỒ BƠI %d luồng gRPC cho Mempool & Giao dịch.", len(b.txClientPool))
		}

		// [UI-ISOLATION] Khởi tạo lại kết nối gRPC riêng biệt cho UI khi hồi sinh server
		uiClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại kết nối gRPC cho UI, sử dụng chung kết nối chính. Lỗi: %v", err)
			b.uiSclClient = client
		} else {
			uiClient.SetAuthToken(b.authToken)
			b.uiSclClient = uiClient
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại kết nối gRPC riêng biệt cho UI.")
		}

		// [FAST-ISOLATION] Khởi tạo lại kết nối gRPC riêng biệt cho Fast Query khi hồi sinh server
		fastClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại kết nối gRPC cho Fast Query, sử dụng chung kết nối chính. Lỗi: %v", err)
			b.fastSclClient = client
		} else {
			fastClient.SetAuthToken(b.authToken)
			b.fastSclClient = fastClient
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại kết nối gRPC riêng biệt cho Fast Query.")
		}

		// [ADMIN-ISOLATION] Khởi tạo lại kết nối gRPC riêng biệt cho Admin Tasks khi hồi sinh server
		adminClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại kết nối gRPC cho Admin Tasks, sử dụng chung kết nối chính. Lỗi: %v", err)
			b.adminSclClient = client
		} else {
			adminClient.SetAuthToken(b.authToken)
			b.adminSclClient = adminClient
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại kết nối gRPC riêng biệt cho Admin Tasks.")
		}

		// [EVENT-ISOLATION] Khởi tạo lại kết nối gRPC riêng biệt cho Event Stream khi hồi sinh server
		eventClient, err := NewSclClient(targetAddr)
		if err != nil {
			log.Printf("[BRIDGE-RECOVERY] ⚠️ Không thể khởi tạo lại kết nối gRPC cho Event Stream, sử dụng chung kết nối chính. Lỗi: %v", err)
			b.eventSclClient = client
		} else {
			eventClient.SetAuthToken(b.authToken)
			b.eventSclClient = eventClient
			log.Printf("[BRIDGE-RECOVERY] 🔐 Đã khởi tạo lại kết nối gRPC riêng biệt cho Event Stream.")
		}

		// [SECURITY-FIX] Chờ 2s để đảm bảo pipe parser bắt được token khi DB nạp nặng trước khi gọi destructive APIs
		time.Sleep(2000 * time.Millisecond)
		log.Printf("[BRIDGE-RECOVERY] ✅ Hồi sinh THÀNH CÔNG! SCL Server đã sẵn sàng.")

		// [gRPC-RECOVERY-INIT] Tự động khởi tạo lại StateManager cho Rust Server mới
		// Tại sao: Khi Rust Server được hồi sinh bởi cơ chế recovery, nó cần được mở lại RocksDB
		// để có thể tiếp tục phục vụ gRPC và các yêu cầu P2P từ mạng lưới, tránh lỗi kẹt "StateManager not initialized".
		if b.dbPath != "" {
			log.Printf("[BRIDGE-RECOVERY] 📂 Tự động mở lại RocksDB tại: %s", b.dbPath)
			b.InitSCL(b.dbPath)
		}
	} else {
		log.Printf("[BRIDGE-RECOVERY] ❌ Không thể kết nối RPC sau khi restart: %v", err)
	}
}

func (b *Bridge) ensureSclServer(port int) error {
	// 1. Phân giải đường dẫn thông minh
	exePath, _ := os.Executable()
	searchDir := filepath.Dir(exePath)
	curr, _ := os.Getwd()

	// [VANGUARD-SCL-PATH] Tự động xác định phần mở rộng theo hệ điều hành (Windows vs Linux)
	var sclBin string
	if runtime.GOOS == "windows" {
		sclBin = "scl_server.exe"
	} else {
		sclBin = "scl_server"
	}

	paths := []string{
		filepath.Join(curr, sclBin),
		filepath.Join(searchDir, sclBin),
		filepath.Join(searchDir, "bin", sclBin),
		filepath.Join(searchDir, "bbuild", sclBin),
		filepath.Join(curr, "bbuild", sclBin),
	}

	serverPath := ""
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			serverPath = p
			break
		}
	}

	if serverPath == "" {
		return fmt.Errorf("❌ Không tìm thấy Hạt nhân Rust (%s) tại: %v", sclBin, paths)
	}

	fmt.Printf("⚓ [SCL-ANCHOR] Sử dụng Hạt nhân tại: %s\n", serverPath)

	// [V1.2.9] Kiểm tra an toàn cổng SCL
	if port <= 0 {
		port = 50051
	}

	args := []string{"--port", fmt.Sprintf("%d", port)}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(serverPath, 0755)
	}
	cmd := exec.Command(serverPath, args...)

	log.Printf("[BRIDGE] 🚀 Khởi chạy SCL Server: %s %v", serverPath, args)

	// [SECURITY-HARDENING] Ghi trực tiếp log của SCL Server ra console và file log
	if os.Getenv("SCL_LOG_TO_FILE") == "true" {
		logDir := "node"
		if b.dbPath != "" {
			logDir = b.dbPath
		}
		os.MkdirAll(logDir, 0755)
		logFile, err := os.OpenFile(filepath.Join(logDir, "scl_server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		} else {
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
		}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, log.Writer())
	}

	// [VANGUARD-FIX] Kế thừa môi trường OS và bơm Token bảo mật trực tiếp cho Rust Core nhận
	env := append(os.Environ(), "RUST_LOG=info")
	env = append(env, fmt.Sprintf("SCL_FORCE_TOKEN=%s", b.authToken))
	cmd.Env = env

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("lỗi Start process (%s): %w", serverPath, err)
	}

	// [WINDOWS/UNIX PROCESS REAPING] Đảm bảo gọi Wait trong goroutine riêng để tránh ProcessState bị nil
	go func() {
		_ = cmd.Wait()
	}()

	b.serverCmd = cmd

	// [WINDOWS ONLY] Triển khai Job Objects để scl_server tự chết khi btcgenz đóng thông qua hàm trừu tượng
	b.jobHandle = assignProcessToJob(cmd.Process.Pid)

	log.Printf("[BRIDGE] 🚀 Đã khởi chạy SCL Server (PID: %d) - TCP Mode", cmd.Process.Pid)
	time.Sleep(2 * time.Second) // Chờ TCP Port được mở
	return nil
}

func (b *Bridge) startHashrateTicker() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		b.updateHashrate()
	}
}

func (b *Bridge) updateHashrate() {
	if b.sclClient == nil {
		return
	}

	// [V27] Hỏi Hashrate qua gRPC TRƯỚC khi khóa Mutex.
	// Tại sao: Lệnh gọi mạng gRPC có thể chậm. Rust Core (lib.rs:743) trả về
	// SỐ LƯỢNG BĂM MỚI (DELTAS) kể từ lần gọi cuối và tự động reset về 0.
	delta := b.sclClient.GetHashrate()

	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()

	diffTime := now.Sub(b.lastUpdate).Seconds()
	// Giới hạn thời gian tối thiểu (0.2s) để tránh nhiễu số liệu
	if diffTime > 0.2 {
		if delta > 0 {
			// Tính toán tỷ lệ dựa trên thực tế thời gian trôi qua
			newRate := uint64(float64(delta) / diffTime)

			// [EMA SMOOTHING] Làm mượt chỉ số (30% cũ, 70% mới)
			if b.currentRate == 0 {
				b.currentRate = newRate
			} else {
				b.currentRate = uint64(float64(b.currentRate)*0.3 + float64(newRate)*0.7)
			}

			// log.Printf("[AUDIT-BRIDGE] 📈 Tín hiệu đồng bộ: delta=%d, diffTime=%.2fs, currentRate=%d H/S", delta, diffTime, b.currentRate)
		} else {
			// [DEBUG] Log nhịp đập ngay cả khi delta = 0 để xác nhận Ticker còn sống
			// log.Printf("[DEBUG-BRIDGE] ❤️ Ticker Heartbeat: delta=0, currentRate=%d", b.currentRate)
			if b.currentRate > 0 {
				b.currentRate = uint64(float64(b.currentRate) * 0.8)
				if b.currentRate < 100 {
					b.currentRate = 0
				}
			}
		}

		b.lastUpdate = now
		// b.lastTotalHash không còn cần thiết vì Rust trả về Delta
	}
}

func (b *Bridge) InitSCL(path string) {
	b.mu.Lock()
	b.dbPath = path
	b.mu.Unlock()

	// [SECURITY-HARDENING] Ghi file .auth_token vào thư mục cha của database (thường là node/) để CLI đọc
	parentDir := filepath.Dir(path)
	tokenFile := filepath.Join(parentDir, ".auth_token")
	if err := os.WriteFile(tokenFile, []byte(b.authToken), 0600); err != nil {
		log.Printf("[BRIDGE-SECURITY] ⚠️ Không thể lưu .auth_token tại %s: %v", tokenFile, err)
	} else {
		log.Printf("[BRIDGE-SECURITY] 💾 Đã lưu .auth_token tại: %s", tokenFile)
	}

	if b.sclClient != nil {
		log.Printf("[BRIDGE] 📥 Đang yêu cầu khởi tạo SCL Core tại: %s", path)
		if err := b.sclClient.InitScl(path); err != nil {
			log.Printf("[BRIDGE] ❌ Yêu cầu khởi tạo SCL Core thất bại: %v", err)
		} else {
			log.Printf("[BRIDGE] ✅ SCL Core đã tiếp nhận yêu cầu khởi tạo.")
		}
	}
}

func (b *Bridge) ExecuteBlockTransactions(_ any, txData []byte, _ [][]byte, minerAddr []byte, parentHash []byte, height uint64, timestamp uint64, isSimulation bool) ([]byte, bool, string, int32, error) {
	root, success, err_msg, failing_tx := b.sclClient.ExecuteBlock(txData, minerAddr, parentHash, height, isSimulation, timestamp)
	return root, success, err_msg, failing_tx, nil
}

func (b *Bridge) GetAccountState(addr []byte) *go_bridge_pb.AccountSnapshot {
	if b.uiSclClient == nil {
		return nil
	}
	return b.uiSclClient.GetAccountState(addr)
}

func (b *Bridge) GetBalance(_ any, addr []byte, _ uint32) uint64 {
	if b.uiSclClient == nil {
		return 0
	}
	return b.uiSclClient.GetBalance(addr)
}
func (b *Bridge) GetNonce(_ any, addr []byte) uint64 {
	if b.uiSclClient == nil {
		return 0
	}
	return b.uiSclClient.GetNonce(addr)
}
func (b *Bridge) GetStateRoot() []byte {
	if b.uiSclClient == nil {
		return nil
	}
	return b.uiSclClient.GetStateRoot()
}
func (b *Bridge) GetSpendableBalance(addr []byte) uint64 {
	if b.uiSclClient == nil {
		return 0
	}
	return b.uiSclClient.GetSpendableBalance(addr)
}
func (b *Bridge) GetFinalizedHeight() uint64 {
	if b.sclClient == nil {
		return 0
	}
	return b.sclClient.GetFinalizedHeight()
}
func (b *Bridge) SetFinalizedHeight(h uint64) {
	if b.sclClient == nil {
		return
	}
	b.sclClient.SetFinalizedHeight(h)
}

// [SOCIAL-CONSENSUS] Cưỡng chế hạ Tường lửa Bất biến (Finality Firewall).
// Chỉ dùng khi nhà vận hành Node chủ động can thiệp trong tình huống chia cắt mạng.
// Tại sao: SetFinalizedHeight bình thường chỉ cho phép TIẾN LÊN, hàm này cho phép LÙI LẠI.
func (b *Bridge) ForceSetFinalizedHeight(h uint64) {
	if b.sclClient == nil {
		return
	}
	b.sclClient.ForceSetFinalizedHeight(h)
}
func (b *Bridge) GetCurrentVersion() uint64 {
	if b.IsSyncing() {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedHeight
	}
	if b.sclClient == nil {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedHeight
	}
	v := b.sclClient.GetCurrentVersion()
	if v > 0 {
		b.cacheMu.Lock()
		b.cachedHeight = v
		b.cacheMu.Unlock()
	} else {
		b.cacheMu.RLock()
		v = b.cachedHeight
		b.cacheMu.RUnlock()
	}
	return v
}
func (b *Bridge) GetOldestHeight() uint64 {
	if b.sclClient == nil {
		return 0
	}
	return b.sclClient.GetOldestHeight()
}
func (b *Bridge) GetMedianTimePast(h uint64) uint64 {
	if b.sclClient == nil {
		return 0
	}
	return b.sclClient.GetMedianTimePast(h)
}

func (b *Bridge) GetTransactionStatus(hash []byte) (uint64, uint32, bool, uint64, uint64, uint64, uint64, uint64) {
	if b.uiSclClient == nil {
		return 0, 0, false, 0, 0, 0, 0, 0
	}
	return b.uiSclClient.GetTransactionStatus(hash)
}

func (b *Bridge) RollbackState(_ any, currentHeight uint64, targetHeight uint64) bool {
	b.SetSyncing(true)
	defer b.SetSyncing(false)
	if b.adminSclClient == nil {
		return false
	}
	return b.adminSclClient.RollbackState(currentHeight, targetHeight)
}

// [BÀN TAY VÔ HÌNH] Xóa khối vật lý — bỏ qua Tường lửa Bất biến.
func (b *Bridge) ForceDeleteBlocks(currentHeight uint64, targetHeight uint64) bool {
	if b.adminSclClient == nil {
		return false
	}
	return b.adminSclClient.ForceDeleteBlocks(currentHeight, targetHeight)
}

func (b *Bridge) CommitBlockHash(height uint64, hash []byte) {
	if b.sclClient == nil {
		return
	}
	b.sclClient.CommitBlockHash(height, hash)
}

func (b *Bridge) VerifyPow(headerBytes []byte, nonce uint64, difficulty []byte, height uint64) (bool, error) {
	if b.fastSclClient == nil {
		return false, fmt.Errorf("SCL Client unavailable")
	}
	res := b.fastSclClient.VerifyPow(headerBytes, nonce, difficulty, height)
	if res == 0 {
		return true, nil
	}
	if res == 2 {
		return false, ErrCriticalFirewall
	} // [V37.9.7] Mã 2 = Firewall Violation
	if res == 3 || res == -1 {
		return false, ErrDbBusy
	}
	return false, nil // Các lỗi khác (Invalid PoW, v.v.)
}

func (b *Bridge) CalculateNextDifficultyV2(timestamps []uint64, difficulties [][]byte, currentTs uint64, height uint64) []byte {
	if b.IsSyncing() {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedDifficulty
	}
	if b.fastSclClient == nil {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedDifficulty
	}
	diff := b.fastSclClient.CalculateNextDifficulty(timestamps, difficulties, currentTs, height)
	if len(diff) > 0 {
		b.cacheMu.Lock()
		b.cachedDifficulty = diff
		b.cacheMu.Unlock()
	} else {
		b.cacheMu.RLock()
		diff = b.cachedDifficulty
		b.cacheMu.RUnlock()
	}
	return diff
}

func (b *Bridge) CalculateBlockHeaderHash(data []byte) []byte {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính toán in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateBlockHeaderHash(data)
}

func (b *Bridge) SetMiningPause(pause bool) {
	b.cacheMu.Lock()
	b.cachedMiningPaused = pause
	b.cacheMu.Unlock()
}

func (b *Bridge) IsMiningPaused() bool {
	b.cacheMu.RLock()
	defer b.cacheMu.RUnlock()
	return b.cachedMiningPaused
}

func (b *Bridge) GetHashrate() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentRate
}

// [V19 UNIFIED STORAGE] Hệ thống Quản trị Sổ cái Nhất thể (Rust Core)

func (b *Bridge) SaveBlockRaw(h uint64, hash []byte, data []byte, isCanonical bool) bool {
	if b.sclClient == nil {
		return false
	}
	return b.sclClient.SaveBlockRaw(h, hash, data, isCanonical)
}

func (b *Bridge) GetBlock(h uint64) []byte {
	if b.IsSyncing() {
		return nil
	}
	if b.uiSclClient == nil {
		return nil
	}
	return b.uiSclClient.GetBlockRaw(h)
}

func (b *Bridge) GetBlockHash(h uint64) []byte {
	if b.uiSclClient == nil {
		return nil
	}
	return b.uiSclClient.GetBlockHash(h)
}

func (b *Bridge) GetRawByHash(hash []byte) []byte {
	if b.uiSclClient == nil {
		return nil
	}
	return b.uiSclClient.GetRawByHash(hash)
}

func (b *Bridge) DeleteByHash(hash []byte) bool {
	if b.sclClient == nil {
		return false
	}
	return b.sclClient.DeleteByHash(hash)
}

func (b *Bridge) PutHeader(hash []byte, headerRaw []byte) {
	// [V19] Header giờ đây được lưu tự động thông qua SaveBlock.
	// Phương thức này chỉ giữ lại để tương thích ngược.
	if b.sclClient != nil {
		b.sclClient.SaveBlockRaw(0, hash, headerRaw, true) // Không biết height thì dùng 0 hoặc wrap lại
	}
}

func (b *Bridge) ExportStateSnapshotRaw() []byte {
	if b.adminSclClient == nil {
		return nil
	}
	return b.adminSclClient.ExportStateSnapshotRaw()
}
func (b *Bridge) ExportStateSnapshotAtHeightRaw(height uint64) []byte {
	if b.adminSclClient == nil {
		return nil
	}
	return b.adminSclClient.ExportStateSnapshotAtHeightRaw(height)
}

func (b *Bridge) GetHeaderRaw(hash []byte) []byte {
	if len(hash) == 0 && b.IsSyncing() {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedHighestHeader
	}
	if b.uiSclClient == nil {
		if len(hash) == 0 {
			b.cacheMu.RLock()
			defer b.cacheMu.RUnlock()
			return b.cachedHighestHeader
		}
		return nil
	}
	headerRaw := b.uiSclClient.GetHeaderRaw(hash)
	if len(hash) == 0 && len(headerRaw) > 0 {
		b.cacheMu.Lock()
		b.cachedHighestHeader = headerRaw
		b.cacheMu.Unlock()
	}
	return headerRaw
}



func (b *Bridge) PurgeHistoricalData(start, end uint64) (bool, error) {
	if b.adminSclClient == nil {
		return false, fmt.Errorf("adminSclClient not connected")
	}
	return b.adminSclClient.PurgeHistoricalData(start, end)
}

func (b *Bridge) GetCanonicalTxHash(txBytes []byte, height uint64) []byte {
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateTxHash(txBytes, height)
}
func (b *Bridge) GetRawHash(data []byte) []byte {
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateBlake3HashWithHeight(data, 0)
}
func (b *Bridge) GetCanonicalBlockHeaderHash(headerBytes []byte, height uint64) []byte {
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateBlockHeaderHash(headerBytes)
}

// [DEPRECATED] Không sử dụng sclDLL và procCalcSigningHash để đảm bảo tương thích đa nền tảng

func (b *Bridge) GetSigningHash(tx *go_bridge_pb.Transaction) []byte {
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateSigningHash(tx)
}

func (b *Bridge) AuthoritativeSign(data []byte, privKey []byte) ([]byte, error) {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để ký dữ liệu in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return nil, fmt.Errorf("fastSclClient not connected")
	}
	return b.fastSclClient.AuthoritativeSign(data, privKey)
}

func (b *Bridge) AddToMempool(txHash []byte, txRaw []byte) (bool, error) {
	client := b.getTxClient()
	if client == nil {
		return false, fmt.Errorf("txSclClient pool not connected")
	}
	return client.AddToMempool(txHash, txRaw)
}

func (b *Bridge) AddBatchToMempool(hashes [][]byte, raws [][]byte) (bool, error) {
	client := b.getTxClient()
	if client == nil {
		return false, fmt.Errorf("txSclClient pool not connected")
	}
	return client.AddBatchToMempool(hashes, raws)
}

func (b *Bridge) RemoveFromMempool(txHash []byte) (bool, error) {
	client := b.getTxClient()
	if client == nil {
		return false, fmt.Errorf("txSclClient pool not connected")
	}
	return client.RemoveFromMempool(txHash)
}

func (b *Bridge) RemoveFromMempoolBatch(txHashes [][]byte) (bool, error) {
	client := b.getTxClient()
	if client == nil {
		return false, fmt.Errorf("txSclClient pool not connected")
	}
	return client.RemoveFromMempoolBatch(txHashes)
}

func (b *Bridge) GetMempoolEntries() ([]*go_bridge_pb.MempoolEntry, error) {
	client := b.getTxClient()
	if client == nil {
		return nil, fmt.Errorf("txSclClient pool not connected")
	}
	return client.GetMempoolEntries()
}

func (b *Bridge) GetTransactionsByAddress(addr []byte) ([]*go_bridge_pb.TrackedTx, error) {
	// [UI-ISOLATION] Định tuyến qua uiSclClient để truy vấn lịch sử giao dịch phục vụ UI/API
	if b.uiSclClient == nil {
		return nil, fmt.Errorf("uiSclClient not connected")
	}
	return b.uiSclClient.GetTransactionsByAddress(addr)
}

func (b *Bridge) GetNodeConfig() ([]byte, error) {
	if b.sclClient == nil {
		return nil, fmt.Errorf("sclClient not connected")
	}
	return b.sclClient.GetNodeConfig()
}

func (b *Bridge) SetNodeConfig(data []byte) error {
	if b.sclClient == nil {
		return fmt.Errorf("sclClient not connected")
	}
	return b.sclClient.SetNodeConfig(data)
}

func (b *Bridge) PrepareTransaction(sender, receiver []byte, amount, fee, nonce uint64, privKey []byte, recentHash []byte) (*go_bridge_pb.Transaction, error) {
	// [TX-ISOLATION-POOL] Định tuyến qua hồ bơi txClientPool luân phiên chuyên biệt cho việc dựng transaction
	client := b.getTxClient()
	if client == nil {
		return nil, fmt.Errorf("txSclClient pool not initialized")
	}
	return client.PrepareTransaction(sender, receiver, amount, fee, nonce, privKey, recentHash)
}

func (b *Bridge) CalculateTxRoot(txs []*go_bridge_pb.Transaction, height uint64) []byte {
	hashes := make([]byte, 0, len(txs)*32)
	for _, t := range txs {
		data, _ := proto.Marshal(t)
		h := b.GetCanonicalTxHash(data, height)
		hashes = append(hashes, h...)
	}
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính Merkle Root in-memory
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateMerkleRoot(hashes)
}

func (b *Bridge) CalculateMerkleRoot(flatHashes []byte) []byte {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính Merkle Root in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateMerkleRoot(flatHashes)
}
func (b *Bridge) CalculateBlockRewardBtcZ(h uint64) uint64 {
	if b.IsSyncing() {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedBlockReward
	}
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính toán phần thưởng khối
	if b.fastSclClient == nil {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedBlockReward
	}
	reward := b.fastSclClient.CalculateBlockRewardBtcZ(h)
	if reward > 0 {
		b.cacheMu.Lock()
		b.cachedBlockReward = reward
		b.cacheMu.Unlock()
	} else {
		b.cacheMu.RLock()
		reward = b.cachedBlockReward
		b.cacheMu.RUnlock()
	}
	return reward
}

// [V37.9.13] Thực hiện Đại Thanh Trừng (Batch Purge) qua gRPC
func (b *Bridge) PurgeOldHistory(start, end uint64) bool {
	success, _ := b.PurgeHistoricalData(start, end)
	return success
}

// --- Network Specific Proxy Methods ---
func (b *Bridge) CalculateShortTxIdFfi(txHash []byte, nonce uint64) uint64 {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính toán in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return 0
	}
	return b.fastSclClient.CalculateShortTxId(txHash, nonce)
}

func (b *Bridge) VerifyBlockReconstruction(root []byte, hashes [][]byte) bool {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để xác minh in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return false
	}
	return b.fastSclClient.VerifyBlockReconstruction(root, hashes)
}

func (b *Bridge) VerifyTimestampFirewall(ts, mtp, now uint64) bool {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để xác minh in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return false
	}
	return b.fastSclClient.VerifyTimestampFirewall(ts, mtp, now)
}

func (b *Bridge) VerifySignature(address, message, signature []byte) bool {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để xác minh in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return false
	}
	return b.fastSclClient.VerifySignature(address, message, signature)
}

func (b *Bridge) ImportStateSnapshot(data []byte, version uint64) []byte {
	// [ADMIN-ISOLATION] Định tuyến qua adminSclClient chuyên biệt cho nạp Snapshot cực nặng
	if b.adminSclClient == nil {
		return nil
	}
	return b.adminSclClient.ImportStateSnapshot(data, version)
}

func (b *Bridge) ImportStateSnapshotPath(path string, version uint64) []byte {
	// [ADMIN-ISOLATION] Định tuyến qua adminSclClient chuyên biệt cho nạp Snapshot từ file cực nặng
	if b.adminSclClient == nil {
		return nil
	}
	return b.adminSclClient.ImportStateSnapshotPath(path, version)
}


// --- Economics & Fee Proxy Methods ---
func (b *Bridge) CalculateAbsoluteWeight(parent []byte, diff []byte) []byte {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính toán in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return nil
	}
	return b.fastSclClient.CalculateAbsoluteWeight(parent, diff)
}
func (b *Bridge) CalculateExpectedSupply(h uint64) uint64 {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để tính toán in-memory CPU tránh nghẽn
	if b.fastSclClient == nil {
		return 0
	}
	return b.fastSclClient.CalculateExpectedSupply(h)
}
func (b *Bridge) SetExpectedSupply(supply uint64) {
	// [ADMIN-ISOLATION] Thiết lập thông số cung, thuộc luồng admin
	if b.adminSclClient != nil {
		b.adminSclClient.SetExpectedSupply(supply)
	}
}
func (b *Bridge) CalculateActualTotalSupply() uint64 { return b.GetActualTotalSupply() }
func (b *Bridge) GetActualTotalSupply() uint64 {
	if b.IsSyncing() {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedSupply
	}
	// [UI-ISOLATION] Truy vấn tổng cung thực tế phục vụ UI/Explorer
	if b.uiSclClient == nil {
		b.cacheMu.RLock()
		defer b.cacheMu.RUnlock()
		return b.cachedSupply
	}
	s := b.uiSclClient.GetActualTotalSupply()
	if s > 0 {
		b.cacheMu.Lock()
		b.cachedSupply = s
		b.cacheMu.Unlock()
	} else {
		b.cacheMu.RLock()
		s = b.cachedSupply
		b.cacheMu.RUnlock()
	}
	return s
}
func (b *Bridge) ExportStateSnapshot() []AccountSnapshot {
	// [ADMIN-ISOLATION] Xuất snapshot, thuộc luồng admin
	if b.adminSclClient == nil {
		return nil
	}
	return b.adminSclClient.ExportStateSnapshot()
}
func (b *Bridge) DebugDumpSmtNodes() string {
	// [UI-ISOLATION] Dump nodes phục vụ UI/Debugging
	if b.uiSclClient == nil {
		return ""
	}
	return b.uiSclClient.DebugDumpSmtNodes()
}
func (b *Bridge) GetAddressType(addr []byte) int32 {
	// [UI-ISOLATION] Kiểm tra loại địa chỉ phục vụ UI/API
	if b.uiSclClient == nil {
		return -1
	}
	return b.uiSclClient.GetAddressType(addr)
}
func (b *Bridge) IsValidFee(fee uint64) bool {
	// [FAST-ISOLATION] Kiểm tra phí hợp lệ (In-Memory CPU)
	if b.fastSclClient == nil {
		return false
	}
	return b.fastSclClient.IsValidFee(fee)
}
func (b *Bridge) CalculateNanoFee(amount uint64, weight uint32) uint64 {
	// [FAST-ISOLATION] Tính toán phí nano (In-Memory CPU)
	if b.fastSclClient == nil {
		return 0
	}
	return b.fastSclClient.CalculateNanoFee(amount, weight)
}
func (b *Bridge) GetNanoWeight(rawTx []byte) uint64 {
	// [FAST-ISOLATION] Lấy trọng lượng nano (In-Memory CPU)
	if b.fastSclClient == nil {
		return 0
	}
	return b.fastSclClient.GetNanoWeight(rawTx)
}


func (b *Bridge) CheckFFILink() bool {
	root, err := b.sclClient.GetStateRootDetailed()
	if err != nil {
		log.Printf("[BRIDGE] ❌ gRPC Linkage lỗi: %v", err)
		return false
	}
	if root == nil {
		log.Printf("[BRIDGE] ❌ gRPC Linkage lỗi: Nhận phản hồi rỗng từ SCL")
		return false
	}
	if len(root) != 32 {
		log.Printf("[BRIDGE] ❌ gRPC Linkage lỗi: StateRoot không hợp lệ (%d bytes)", len(root))
		return false
	}
	return true
}

func (b *Bridge) Close() {
	if b.sclClient != nil {
		b.sclClient.Close()
	}
	if b.serverCmd != nil && b.serverCmd.Process != nil {
		b.serverCmd.Process.Kill()
	}
	if b.dbPath != "" {
		tokenFile := filepath.Join(filepath.Dir(b.dbPath), ".auth_token")
		_ = os.Remove(tokenFile)
		log.Printf("[BRIDGE-SECURITY] 🧹 Đã dọn dẹp file .auth_token tại: %s", tokenFile)
	}
}

func (b *Bridge) EmergencyStateRebuild(version uint64) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// [ADMIN-ISOLATION] Tác vụ xây dựng lại trạng thái khẩn cấp thuộc luồng admin
	if b.adminSclClient == nil {
		return false, fmt.Errorf("adminSclClient not initialized")
	}
	return b.adminSclClient.EmergencyStateRebuild(version)
}

func (b *Bridge) ResetStateCompletely() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// [ADMIN-ISOLATION] Tác vụ reset trạng thái hoàn toàn thuộc luồng admin
	if b.adminSclClient == nil {
		return false, fmt.Errorf("adminSclClient not initialized")
	}
	return b.adminSclClient.ResetStateCompletely()
}

func (b *Bridge) ClearStagingArea() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// [ADMIN-ISOLATION] Tác vụ dọn dẹp vùng đệm staging thuộc luồng admin
	if b.adminSclClient == nil {
		return false, fmt.Errorf("adminSclClient not initialized")
	}
	return b.adminSclClient.ClearStagingArea()
}

func (b *Bridge) GetHighestBlockHeight() (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// [UI-ISOLATION] Truy vấn chiều cao khối cao nhất phục vụ UI/API
	if b.uiSclClient == nil {
		return 0, fmt.Errorf("uiSclClient not initialized")
	}
	return b.uiSclClient.GetHighestBlockHeight()
}

func BigIntToBytes(n *big.Int) []byte { return n.Bytes() }
func BigIntToBytesLE(n *big.Int) []byte {
	b := make([]byte, 32)
	copy(b, n.Bytes())
	for i := 0; i < len(n.Bytes())/2; i++ {
		b[i], b[len(n.Bytes())-1-i] = b[len(n.Bytes())-1-i], b[i]
	}
	res := make([]byte, 32)
	copy(res, b)
	return res
}
func BytesToBigInt(b []byte) *big.Int { return BytesLEToBigInt(b) }
func BytesLEToBigInt(b []byte) *big.Int {
	be := make([]byte, len(b))
	for i := 0; i < len(b); i++ {
		be[i] = b[len(b)-1-i]
	}
	return new(big.Int).SetBytes(be)
}

const GenzPowContext = "btc-genz-v112"

func CalculateBlake3Hash(data []byte, out []byte, height uint64) {
	if GlobalBridge != nil {
		for attempt := 1; attempt <= 30; attempt++ {
			GlobalBridge.mu.Lock()
			client := GlobalBridge.fastSclClient
			GlobalBridge.mu.Unlock()

			if client != nil {
				h := client.CalculateBlake3HashWithHeight(data, height)
				if len(h) == 32 {
					copy(out, h)
					return
				}
			}
			log.Printf("[BRIDGE-WARN] ⚠️ Lần thứ %d: SCL Core (Rust) bận hoặc lỗi gRPC khi băm H#%d. Thử lại sau 500ms...", attempt, height)
			time.Sleep(500 * time.Millisecond)
		}
	}

	// [TEST-STUB] Fallback cho môi trường kiểm thử Unit Test khi không chạy SCL Core (Rust)
	if GlobalBridge == nil {
		if flag.Lookup("test.v") != nil {
			h := sha256.Sum256(data)
			copy(out, h[:])
			return
		}
		log.Fatalf("[FATAL-CONSENSUS] 💀 Lỗi nghiêm trọng: SCL Core (Rust) chưa được khởi tạo. Không thể băm dữ liệu cho H#%d ở môi trường Production.", height)
	}

	log.Fatalf("[FATAL-CONSENSUS] 💀 Lỗi nghiêm trọng: SCL Core (Rust) không sẵn sàng để tính toán mã băm cho H#%d. Ngừng Node để bảo vệ dữ liệu.", height)
}

// [VANGUARD-CONSENSUS]
func (b *Bridge) EvaluateHeaderChain(headers [][]byte) (*go_bridge_pb.EvaluateHeaderChainResponse, error) {
	return b.sclClient.EvaluateHeaderChain(headers)
}

func (b *Bridge) ProcessNewBlock(blockRaw []byte) (*go_bridge_pb.ProcessNewBlockResponse, error) {
	return b.sclClient.ProcessNewBlock(blockRaw)
}

var consensusMu sync.Mutex

func (b *Bridge) ProcessChain(blocksRaw [][]byte) (*go_bridge_pb.SyncChainResponse, error) {
	consensusMu.Lock()
	defer consensusMu.Unlock()

	b.SetSyncing(true)
	defer b.SetSyncing(false)
	return b.sclClient.ProcessChain(blocksRaw)
}



func (b *Bridge) BuildVanguardBlockTemplate(height uint64, parentHash []byte, minerAddr []byte, txsBytes [][]byte, ts uint64, diff []byte) ([]byte, int32, string) {
	// Tại sao: Định tuyến qua minerSclClient chuyên biệt để đảm bảo việc dựng mẫu khối khai thác không bị ảnh hưởng bởi các yêu cầu đồng bộ nặng trên sclClient.
	if b.minerSclClient == nil {
		return nil, -1, "Miner client chưa khởi tạo"
	}
	return b.minerSclClient.BuildVanguardBlockTemplate(height, parentHash, minerAddr, txsBytes, ts, diff)
}

func (b *Bridge) ReindexMinerHistory(addr []byte) error {
	// [ADMIN-ISOLATION] Tác vụ quét lại lịch sử miner thuộc luồng admin/bảo trì dài hạn
	if b.adminSclClient == nil {
		return fmt.Errorf("adminSclClient not connected")
	}
	return b.adminSclClient.ReindexMinerHistory(addr)
}



func (b *Bridge) ValidateTransactionBatch(rawTxs [][]byte) (*go_bridge_pb.ValidateTxBatchResponse, error) {
	// [TX-ISOLATION-POOL] Định tuyến qua hồ bơi txClientPool luân phiên
	client := b.getTxClient()
	if client == nil {
		return nil, fmt.Errorf("txSclClient pool not initialized")
	}
	return client.ValidateTransactionBatch(rawTxs)
}

func (b *Bridge) GetTransactionStatusBatch(hashes [][]byte) ([]*go_bridge_pb.TxStatusEntry, error) {
	if b.uiSclClient == nil {
		return nil, fmt.Errorf("uiSclClient not initialized")
	}
	resp, err := b.uiSclClient.GetTransactionStatusBatch(hashes)
	if err != nil {
		return nil, err
	}
	return resp.Statuses, nil
}

func (b *Bridge) GetBalanceBatch(addresses [][]byte) ([]*go_bridge_pb.BalanceEntry, error) {
	if b.uiSclClient == nil {
		return nil, fmt.Errorf("uiSclClient not initialized")
	}
	resp, err := b.uiSclClient.GetBalanceBatch(addresses)
	if err != nil {
		return nil, err
	}
	return resp.Balances, nil
}

// CalculateTxHashesBatch chuyển tiếp cuộc gọi gRPC dạng lô để băm song song xuống Rust Core.
func (b *Bridge) CalculateTxHashesBatch(rawTxs [][]byte, height uint64) ([][]byte, error) {
	// [FAST-ISOLATION] Định tuyến qua fastSclClient để thực hiện phép băm song song (In-Memory CPU)
	if b.fastSclClient == nil {
		return nil, fmt.Errorf("fastSclClient not initialized")
	}
	return b.fastSclClient.CalculateTxHashesBatch(rawTxs, height)
}

func (b *Bridge) startEventStreamListener() {
	for {
		if b.eventSclClient == nil {
			time.Sleep(5 * time.Second)
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		b.mu.Lock()
		b.eventCancel = cancel
		b.mu.Unlock()

		log.Printf("[BRIDGE-EVENT] 📡 Đang kết nối vào trạm phát sóng của Rust Core...")
		stream, err := b.eventSclClient.WatchCoreEvents(ctx)
		if err != nil {
			log.Printf("[BRIDGE-EVENT] ❌ Lỗi kết nối Kênh Sự Kiện: %v. Thử lại sau 5s...", err)
			cancel()
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[BRIDGE-EVENT] ✅ Đã dỏng tai nghe Rust Core!")

		// Vòng lặp chờ tin nhắn từ Rust
		for {
			event, err := stream.Recv()
			if err != nil {
				log.Printf("[BRIDGE-EVENT] 🔌 Đứt kết nối Kênh Sự Kiện: %v. Đang kết nối lại...", err)
				cancel()
				break // Thoát vòng lặp con để quay lại vòng lặp ngoài (Reconnect)
			}

			// --- XỬ LÝ SỰ KIỆN TỪ RUST Ở ĐÂY ---
			b.handleRustEvent(event)
		}
	}
}

// Xử lý logic khi Rust "hét" lên
func (b *Bridge) handleRustEvent(event *go_bridge_pb.CoreEvent) {
	switch event.Type {
	case go_bridge_pb.CoreEvent_SECURITY_ALERT:
		log.Printf("🚨 [RUST-SECURITY-ALERT] RUST BÁO ĐỘNG: %s", event.Message)

	case go_bridge_pb.CoreEvent_ROCKSDB_WARNING:
		log.Printf("💽 [RUST-DB-WARNING] %s", event.Message)

	case go_bridge_pb.CoreEvent_SYSTEM_PANIC:
		log.Printf("💀 [RUST-CRITICAL] %s", event.Message)
		// Kích hoạt Restart Rust Core ngay lập tức
		b.restartServer()

	case go_bridge_pb.CoreEvent_TX_POOL_CONGESTION:
		log.Printf("⚡ [RUST-MEMPOOL-CONGESTION] %s", event.Message)

	default:
		log.Printf("📩 [RUST-EVENT] %s", event.Message)
	}
}

// StartGenzMiner tự động khởi chạy thợ đào genz_miner độc lập để liên kết với gRPC Node
func (b *Bridge) StartGenzMiner(grpcPort int) error {
	// 1. Phân giải đường dẫn thông minh
	exePath, _ := os.Executable()
	searchDir := filepath.Dir(exePath)
	curr, _ := os.Getwd()

	var minerBin string
	if runtime.GOOS == "windows" {
		minerBin = "genz_miner.exe"
	} else {
		minerBin = "genz_miner"
	}

	paths := []string{
		filepath.Join(curr, minerBin),
		filepath.Join(searchDir, minerBin),
		filepath.Join(searchDir, "bin", minerBin),
		filepath.Join(searchDir, "bbuild", minerBin),
		filepath.Join(curr, "bbuild", minerBin),
	}

	minerPath := ""
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			minerPath = p
			break
		}
	}

	if minerPath == "" {
		return fmt.Errorf("❌ Không tìm thấy Thợ đào độc lập (%s) tại các thư mục kiểm tra: %v", minerBin, paths)
	}

	fmt.Printf("⚓ [MINER-ANCHOR] Sử dụng Thợ đào độc lập tại: %s\n", minerPath)

	args := []string{"--port", fmt.Sprintf("%d", grpcPort)}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(minerPath, 0755)
	}
	cmd := exec.Command(minerPath, args...)

	log.Printf("[BRIDGE] 🚀 Khởi chạy genz_miner: %s %v", minerPath, args)

	// Ghi log trực tiếp ra console và logger
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, log.Writer())

	// Kế thừa môi trường hệ điều hành
	cmd.Env = os.Environ()

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("lỗi khởi chạy thợ đào (%s): %w", minerPath, err)
	}

	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			log.Printf("[BRIDGE] ⚠️ genz_miner (PID: %d) thoát với lỗi: %v", cmd.Process.Pid, waitErr)
		} else {
			log.Printf("[BRIDGE] ℹ️ genz_miner (PID: %d) đã dừng thành công.", cmd.Process.Pid)
		}
	}()

	b.mu.Lock()
	b.minerCmd = cmd
	b.mu.Unlock()

	// Gán tiến trình vào Windows Job Object để tự động kết thúc khi Node chính đóng
	assignProcessToJob(cmd.Process.Pid)

	log.Printf("[BRIDGE] 🚀 Đã khởi chạy genz_miner (PID: %d)", cmd.Process.Pid)
	return nil
}

// StartGpuMiner tự động khởi chạy thợ đào GPU yona_gpu_miner.exe
func (b *Bridge) StartGpuMiner(nodePort int) error {
	// 1. Phân giải đường dẫn thông minh
	exePath, _ := os.Executable()
	searchDir := filepath.Dir(exePath)
	curr, _ := os.Getwd()

	var minerBin string
	if runtime.GOOS == "windows" {
		minerBin = "yona_gpu_miner.exe"
	} else {
		minerBin = "yona_gpu_miner"
	}

	paths := []string{
		filepath.Join(curr, minerBin),
		filepath.Join(searchDir, minerBin),
		filepath.Join(searchDir, "bin", minerBin),
		filepath.Join(searchDir, "bbuild", minerBin),
		filepath.Join(curr, "bbuild", minerBin),
	}

	minerPath := ""
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			minerPath = p
			break
		}
	}

	if minerPath == "" {
		return fmt.Errorf("❌ Không tìm thấy Thợ đào GPU (%s) tại các thư mục kiểm tra: %v", minerBin, paths)
	}

	fmt.Printf("⚓ [MINER-ANCHOR] Sử dụng Thợ đào GPU tại: %s\n", minerPath)

	args := []string{"127.0.0.1", fmt.Sprintf("%d", nodePort)}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(minerPath, 0755)
	}
	cmd := exec.Command(minerPath, args...)

	log.Printf("[BRIDGE] 🚀 Khởi chạy yona_gpu_miner: %s %v", minerPath, args)

	// Ghi log trực tiếp ra cả console (stdout/stderr) và file log hệ thống (node_system.log)
	cmd.Stdout = io.MultiWriter(os.Stdout, log.Writer())
	cmd.Stderr = io.MultiWriter(os.Stderr, log.Writer())

	// Kế thừa môi trường hệ điều hành
	cmd.Env = os.Environ()

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("lỗi khởi chạy thợ đào GPU (%s): %w", minerPath, err)
	}

	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			log.Printf("[BRIDGE] ⚠️ yona_gpu_miner (PID: %d) thoát với lỗi: %v", cmd.Process.Pid, waitErr)
		} else {
			log.Printf("[BRIDGE] ℹ️ yona_gpu_miner (PID: %d) đã dừng thành công.", cmd.Process.Pid)
		}
	}()

	b.mu.Lock()
	b.gpuMinerCmd = cmd
	b.mu.Unlock()

	// Gán tiến trình vào Windows Job Object để tự động kết thúc khi Node chính đóng
	assignProcessToJob(cmd.Process.Pid)

	log.Printf("[BRIDGE] 🚀 Đã khởi chạy yona_gpu_miner (PID: %d)", cmd.Process.Pid)
	return nil
}

// StopGpuMiner dừng tiến trình yona_gpu_miner nếu đang chạy
func (b *Bridge) StopGpuMiner() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gpuMinerCmd != nil && b.gpuMinerCmd.Process != nil {
		log.Printf("[BRIDGE] 🛑 Dừng thợ đào GPU yona_gpu_miner (PID: %d)", b.gpuMinerCmd.Process.Pid)
		_ = b.gpuMinerCmd.Process.Kill()
		b.gpuMinerCmd = nil
	}
}

// StopGenzMiner dừng tiến trình genz_miner nếu đang chạy
func (b *Bridge) StopGenzMiner() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.minerCmd != nil && b.minerCmd.Process != nil {
		log.Printf("[BRIDGE] 🛑 Dừng thợ đào CPU genz_miner (PID: %d)", b.minerCmd.Process.Pid)
		_ = b.minerCmd.Process.Kill()
		b.minerCmd = nil
	}
}

