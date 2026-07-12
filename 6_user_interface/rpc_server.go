/**
 * @file rpc_server.go
 * @brief API RESTful/JSON-RPC cho YonaCode Minimalist V1.0.
 * @details Tận dụng VT-proto (Optimized) và UniFFI Bridge.
 * @tính_năng:
 *   - In-memory Tx Tracker: Theo dõi 100 giao dịch gần nhất (Mempool + Block)
 *   - SSE Finality Stream: Phát sóng newly_finalized_txids khi khối mới vượt ngưỡng n+5
 *   - Balance API: Trả về Coin ID (BTC_Z) và Nonce cho frontend
 */

package user_interface

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"math/big"

	"github.com/tyler-smith/go-bip39"

	"btc_genz/2_miner_core/go_bridge"
	node_p2p "btc_genz/5_node_p2p"
	"btc_genz/6_user_interface/audit"
	"btc_genz/6_user_interface/internal"
	pb_block "btc_genz/proto"

	"github.com/gorilla/mux"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"embed"
	"io/fs"
	"net"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

//go:embed web_ui/dist/*
var staticFiles embed.FS

func init() {
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".svg", "image/svg+xml")
}

// safeShortID: Cắt ngắn ID giao dịch an toàn để hiển thị log, tránh lỗi hoảng loạn slice out of range.
func safeShortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// SetMinerAddress cập nhật thông tin thợ đào cho RPC Server truy cập
func (s *RPCServer) SetMinerAddress(addr []byte, key ed25519.PrivateKey) {
	s.minerAddr = addr
	s.minerKey = key
	if key != nil {
		log.Printf("[RPC] 🔑 Đã nạp Miner Key (%d bytes) cho các tác vụ ký xác thực.", len(key))
	}
}

// ============================================================================
// TrackedTx: Cấu trúc theo dõi giao dịch trong bộ nhớ (In-memory Tx Tracker)
// Tại sao dùng in-memory: Đảm bảo phản hồi SSE < 50ms, không cần I/O đĩa.
// Giới hạn: 100 giao dịch gần nhất, mất khi restart (chấp nhận cho kiểm thử).
// ============================================================================
type TrackedTx struct {
	TxID          string `json:"id"`
	RawTxID       []byte `json:"-"`
	Sender        string `json:"sender"`
	Receiver      string `json:"receiver"`
	Amount        uint64 `json:"amount"`
	Fee           uint64 `json:"fee"`
	Timestamp     int64  `json:"timestamp"`
	BlockHeight   uint64 `json:"block_height"` // 0 = chưa vào block (mempool)
	Nonce         uint64 `json:"nonce"`
	Status        uint32 `json:"status_code"` // 1: SUCCESS, 2: INVALID_SIG, v.v.
	IsFinalized   bool   `json:"is_finalized"`
	Confirmations uint64 `json:"confirmations"`
	ErrorMessage  string `json:"error_message"` // Thông điệp lỗi chi tiết (nếu có)
	PrevBalance   uint64 `json:"prev_balance"`  // [V37.1] Số dư TRƯỚC khi thực hiện
	PostBalance   uint64 `json:"post_balance"`  // [V37] Số dư SAU khi thực hiện (Snapshot)
}

// MinerStats: Thống kê hashrate ước tính cho từng địa chỉ ví thợ đào
type MinerStats struct {
	Address     string  `json:"address"`
	BlocksMined int     `json:"blocks_mined"`
	Percentage  float64 `json:"percentage"`
	HashrateEst uint64  `json:"hashrate_est"`
}

// TxResponse: Cấu trúc trả về JSON chuẩn hóa và tối ưu hóa bộ nhớ cho API giao dịch
type TxResponse struct {
	ID            string `json:"id"`
	Sender        string `json:"sender"`
	Receiver      string `json:"receiver"`
	Amount        uint64 `json:"amount"`
	Fee           uint64 `json:"fee"`
	Timestamp     int64  `json:"timestamp"`
	Height        uint64 `json:"height"`
	Confirmations uint64 `json:"confirmations"`
	Status        string `json:"status"`
	StatusCode    uint32 `json:"status_code"`
	ErrorMessage  string `json:"error_message,omitempty"`
	Direction     string `json:"direction"`
	IsSelf        bool   `json:"is_self"`
	PrevBalance   uint64 `json:"prev_balance"`
	PostBalance   uint64 `json:"post_balance"`
	ReceiverPrev  uint64 `json:"receiver_prev,omitempty"`
	ReceiverPost  uint64 `json:"receiver_post,omitempty"`
	Nonce         uint64 `json:"nonce"`
}

type RPCServer struct {
	pb_block.UnimplementedBlockchainServiceServer
	pb_block.UnimplementedMinerGatewayServer
	bridge    *go_bridge.Bridge
	netMgr    *node_p2p.NetworkManager
	port      int
	walletMgr *internal.WalletManager
	minerAddr []byte
	minerKey  ed25519.PrivateKey
	cliApp    *CLIApp // [V2.0] Tham chiếu tới CLIApp để thay đổi ví đào runtime

	// [VANGUARD-CACHE] Cache địa chỉ ví phẳng để tránh I/O nghẽn đĩa sync block
	cachedWallets    map[string]bool
	lastWalletUpdate time.Time
	walletCacheMu    sync.RWMutex

	// [FINALITY-UI] In-memory Tx Tracker
	txTracker         map[string]*TrackedTx // key: TxID hex
	txTrackerMu       sync.RWMutex
	txOrder           []string // Thứ tự TxID theo thời gian
	lastFinalizedH    uint64   // Cache chiều cao finalized gần nhất để phát hiện thay đổi
	lastTrackedHeight uint64   // [VANGUARD-OPTIMIZED] Cao độ cuối cùng đã được quét giao dịch (Dashboard)

	// [DASHBOARD V3] Hashrate History Ring Buffer
	// Tại sao ring buffer: Giới hạn bộ nhớ cố định 60 điểm (2 phút dữ liệu mỗi 2s)
	// Phục vụ biểu đồ hashrate realtime trên frontend
	hashrateHistory   [60]float64
	hashrateHistIdx   int
	hashrateHistCount int
	hashrateHistMu    sync.RWMutex

	// [DASHBOARD V3] Node Operating Mode
	// "verify-only": Chỉ xác minh block, không đào (tiết kiệm tài nguyên)
	// "full-mining": Full node + đào Blake3-PoW
	nodeMode   string
	nodeModeMu sync.RWMutex

	miningDevice      string // "cpu", "gpu", or "hybrid"
	miningDeviceMu    sync.RWMutex
	gpuEnvError       string
	gpuEnvErrorMu     sync.RWMutex
	cpuHashrate       uint64
	gpuHashrate       uint64
	lastGpuHashTime   time.Time

	// [MINER V3] Startup Guard & Grace Period
	launchTime     time.Time
	cpuIntensity   int
	cpuIntensityMu sync.RWMutex

	// [V4.0 ANTI-SPAM] Rate Limiter - Token Bucket per Wallet Address
	rateLimiters         map[string]*tokenBucket
	currentHashrate      uint64
	lastCumulativeHashes uint64
	lastHashTime         time.Time
	rateLimitersMu       sync.Mutex

	// [V5.0 ELITE] Kênh thông báo cập nhật giao diện tức thì
	txUpdateChan    chan struct{}
	blockUpdateChan chan struct{} // [REALTIME-BLOCK-FIX] Đánh thức UI khi có khối mới

	// [V7.0 PERFORMANCE] Seed Cache: Lưu tạm seed đã giải mã để tránh Argon2id lặp lại
	seedCache   map[string][]byte
	seedCacheMu sync.RWMutex

	// [V7.3 PERFORMANCE] Balance Cache: Giảm tải FFI Bridge
	balanceCache     map[string]uint64
	balanceCacheTime map[string]time.Time
	balanceCacheMu   sync.RWMutex

	// [V7.4 PERFORMANCE] Blockchain Status Cache
	heightCache    uint64
	hashCache      []byte
	blockCacheTime time.Time
	blockCacheMu   sync.Mutex

	historyWriteChan chan struct{}
	dbPath           string // [V1.1] Đường dẫn dữ liệu động

	// [VANGUARD-CACHE] Bộ nhớ đệm 200 khối gần nhất để tránh nghẽn RPC khi F5
	recentBlocksCache   []interface{}
	recentBlocksCacheMu sync.RWMutex

	// [AUTO-GAP-HEAL-MUTEX] Khóa đồng bộ theo từng Sender để tuần tự hóa việc gán nonce và ký giao dịch
	senderLocks   map[string]*countedMutex
	senderLocksMu sync.Mutex

	// [MINER-STREAM] Các trường quản lý kết nối thợ đào độc lập
	minerStreams     map[uint64]pb_block.MinerGateway_ConnectMinerServer
	minerHashrates   map[uint64]uint64
	nextStreamId     uint64
	minerStreamsMu   sync.RWMutex
	internetAutoPaused bool
	internetOffline    int32 // 0: Online, 1: Offline
}

type countedMutex struct {
	sync.Mutex
	refCount int32
}

type tokenBucket struct {
	tokens     float64
	lastUpdate time.Time
}

// [V1.5 PERSISTENCE] NodeConfig: Cấu trúc lưu trữ các thiết lập người dùng
type NodeConfig struct {
	CpuIntensity  int    `json:"cpu_intensity"`
	NodeMode      string `json:"node_mode"`
	RewardAddress string `json:"reward_address"`
	NodeID        string `json:"node_id,omitempty"` // Thêm NodeID để định danh cấu hình này thuộc về node nào
	MiningDevice  string `json:"mining_device"`    // Thiết bị khai thác (cpu, gpu, hybrid)
}

const maxTrackedTxs = 5000 // [VANGUARD-OPTIMIZATION] Giới hạn bộ đệm RAM 5,000 giao dịch để tránh phình to bộ nhớ và đĩa cứng dưới tải cao khi stress test

func NewRPCServer(br *go_bridge.Bridge, netMgr *node_p2p.NetworkManager, port int, wm *internal.WalletManager, minerAddr []byte, minerKey ed25519.PrivateKey, app *CLIApp) *RPCServer {
	s := &RPCServer{
		bridge:            br,
		netMgr:            netMgr,
		port:              port,
		walletMgr:         wm,
		minerAddr:         minerAddr,
		minerKey:          minerKey,
		cliApp:            app,
		txTracker:         make(map[string]*TrackedTx),
		txOrder:           make([]string, 0),
		nodeMode:          app.GetNodeMode(),
		cpuIntensity:      50,
		launchTime:        time.Now(),
		txUpdateChan:      make(chan struct{}, 100),
		blockUpdateChan:   make(chan struct{}, 100), // [REALTIME-BLOCK-FIX]
		historyWriteChan:  make(chan struct{}, 1),
		dbPath:            filepath.Clean(app.dbPath), // [V1.65 PORTABLE] Chuẩn hóa đường dẫn để hoạt động trên mọi máy
		rateLimiters:      make(map[string]*tokenBucket),
		seedCache:         make(map[string][]byte),
		balanceCache:      make(map[string]uint64),
		balanceCacheTime:  make(map[string]time.Time),
		recentBlocksCache: make([]interface{}, 0, 200),
		cachedWallets:     make(map[string]bool),
		lastWalletUpdate:  time.Time{},
		senderLocks:       make(map[string]*countedMutex),
		minerStreams:      make(map[uint64]pb_block.MinerGateway_ConnectMinerServer),
		minerHashrates:    make(map[uint64]uint64),
		miningDevice:      "cpu",
	}

	// [MATRIX CONNECT] Liên kết CLI App để đồng bộ Intensity
	if app != nil {
		app.SetRPCServer(s)
	}

	// [V1.5 PERSISTENCE] Tải cấu hình người dùng trước khi Hook
	s.loadNodeConfig()

	// [VANGUARD-SMART-INIT] Nếu ví đào đang là Zero, thử nạp ví cục bộ đầu tiên
	if s.cliApp != nil && s.cliApp.IsZeroAddress(s.minerAddr) {
		wallets, _ := s.walletMgr.ListWallets()
		if len(wallets) > 0 {
			addrBytes, _ := hex.DecodeString(strings.TrimPrefix(wallets[0].Address, "0x"))
			if len(addrBytes) == 32 {
				s.minerAddr = addrBytes
				s.cliApp.SetMinerAddress(addrBytes, nil, "") // Pin rỗng vì chưa có PIN, chỉ nạp địa chỉ
				log.Printf("[RPC-INIT] 📂 Đã tự động nạp ví cục bộ đầu tiên làm địa chỉ đào: %s", wallets[0].Address)
			}
		}
	}

	s.loadHistory() // [V2.5] Nạp lịch sử ngay khi khởi động

	// [HOOK] Đăng ký callback từ Mempool để bắt giao dịch mới
	if netMgr.Mempool != nil {
		netMgr.Mempool.SetOnUpdate(func() {
			// Trigger SSE update only, skip heavy sync
			select {
			case s.txUpdateChan <- struct{}{}:
			default:
			}
		})
		netMgr.Mempool.SetOnTxBatchValidated(func(results []node_p2p.TxValidatedResult) {
			txsBySender := make(map[string][]node_p2p.TxValidatedResult)
			userAddrs := s.getUserWalletAddresses()

			// [VÁ LỖI MUTEX] Khóa 1 lần duy nhất cho toàn bộ giao dịch lỗi
			// Tại sao thiết kế như vậy: Việc gọi s.txTrackerMu.Lock() và Unlock() lặp đi lặp lại hàng chục ngàn lần trong
			// vòng lặp xử lý batch sẽ gây ra nghẽn khóa (Lock Contention) nghiêm trọng làm đóng băng toàn bộ tiến trình.
			// Gom các kết quả lỗi vào mảng và cập nhật chúng dưới một lần Lock duy nhất giúp tối ưu hóa hiệu năng và tránh nghẽn luồng.
			var invalidResults []node_p2p.TxValidatedResult
			for _, res := range results {
				senderHex := hex.EncodeToString(res.Tx.Sender.Value)
				receiverHex := ""
				if res.Tx.Receiver != nil {
					receiverHex = hex.EncodeToString(res.Tx.Receiver.Value)
				}

				if res.IsValid {
					// [VANGUARD-OPTIMIZATION] Sử dụng updateTxTrackerWithCachedData kết hợp số dư đã cache sẵn để tránh bão gRPC GetBalance đơn lẻ.
					s.updateTxTrackerWithCachedData(res.TxHash, senderHex, receiverHex, res.Tx.Amount, res.Tx.Fee, res.Tx.Nonce, 0, time.Now().Unix(), "", res.SenderBalance, userAddrs)
					// [BUS-STATUS-FIX] Xóa trạng thái tạm WAITING_FOR_BUS (99) khi giao dịch đã được Rust Core xác thực thành công.
					// Tại sao: Nếu không xóa, ErrorMessage "Đang chờ xe buýt" sẽ tồn tại vĩnh viễn khiến Frontend
					// hiển thị sai trạng thái "BỊ TỪ CHỐI" do logic isRejected dựa trên error_message.
					s.txTrackerMu.Lock()
					if tracked, exists := s.txTracker[res.TxHash]; exists && tracked.Status == 99 {
						tracked.Status = 0
						tracked.ErrorMessage = ""
					}
					s.txTrackerMu.Unlock()
					txsBySender[senderHex] = append(txsBySender[senderHex], res)
				} else {
					invalidResults = append(invalidResults, res)
				}
			}

			// Cập nhật in-memory cho toàn bộ TX lỗi trong ĐÚNG 1 LẦN LOCK
			if len(invalidResults) > 0 {
				s.txTrackerMu.Lock()
				for _, res := range invalidResults {
					if tracked, exists := s.txTracker[res.TxHash]; exists {
						// [RACE-CONDITION-FIX] Bảo vệ tuyệt đối: Không ghi đè trạng thái giao dịch
						// đã được xác nhận trên blockchain (BlockHeight > 0 hoặc Status == 1).
						// Tại sao: TX có thể quay lại qua P2P gossip sau khi đã được đào vào block,
						// Rust re-validate sẽ thấy nonce cũ đã tiêu → báo "Nonce quá thấp" SAI.
						if tracked.BlockHeight > 0 || tracked.Status == 1 {
							continue
						}
						tracked.Status = res.StatusCode
						tracked.ErrorMessage = res.ErrorMsg
					}
				}
				s.txTrackerMu.Unlock()
			}

			for sender, resList := range txsBySender {
				if len(resList) >= 2 {
					senderBytes, _ := hex.DecodeString(sender)
					var txsBytes [][]byte
					var nonces []uint64
					for _, r := range resList {
						txsBytes = append(txsBytes, r.TxData)
						nonces = append(nonces, r.Tx.Nonce)
					}
					sort.Slice(nonces, func(i, j int) bool { return nonces[i] < nonces[j] })
					startNonce := nonces[0]
					endNonce := nonces[len(nonces)-1]
					seqNum := startNonce

					batchData := node_p2p.PackSequentialBatch(senderBytes, seqNum, startNonce, endNonce, txsBytes)
					if s.netMgr.PubSub != nil {
						go func(data []byte) {
							if err := s.netMgr.PubSub.Publish("txs", data); err != nil {
								log.Printf("[P2P-BROADCAST] ❌ Phát sóng lô TXSQ thất bại sau khi xe buýt duyệt: %v", err)
							} else {
								log.Printf("[P2P-BROADCAST] 🚀 Đã phát sóng lô TXSQ của ví %s (%d giao dịch) lên GossipSub.", sender[:8], len(txsBytes))
							}
						}(batchData)
					}
				} else if len(resList) == 1 {
					go func(data []byte) {
						if err := s.netMgr.BroadcastTransaction(data); err != nil {
							log.Printf("[P2P-BROADCAST] ❌ Phát sóng thất bại sau khi xe buýt duyệt: %v", err)
						}
					}(resList[0].TxData)
				}
			}
			s.triggerSave()
		})
		// [V7.1 PERFORMANCE] Chuyển đổi sang cơ chế Event-driven lắng nghe blockUpdateChan để đồng bộ Mempool Tracker
		// Tại sao: Loại bỏ hoàn toàn việc polling định kỳ mỗi 2 giây vốn gây nghẽn CPU không cần thiết.
		// Chỉ đồng bộ khi có khối mới (blockUpdateChan) hoặc định kỳ 30 giây làm chốt chặn an toàn (backupTicker).
		go func() {
			backupTicker := time.NewTicker(30 * time.Second)
			defer backupTicker.Stop()
			for {
				select {
				case <-s.blockUpdateChan:
					s.syncMempoolToTracker()
				case <-backupTicker.C:
					s.syncMempoolToTracker()
				}
			}
		}()
	}

	// [V35 CONCORDANCE] Đăng ký "Đường dây nóng" đồng bộ khối
	if netMgr != nil {
		oldOnCommitted := netMgr.OnBlockCommitted
		netMgr.OnBlockCommitted = func(height uint64) {
			// 1. Thực hiện các nhiệm vụ của RPC Server (Bao gồm đồng bộ hóa Dashboard và dọn dẹp Mempool đồng bộ)
			s.SyncBlockToTracker(height)
			s.StartGreatPurge(height) // [AUTOMATIC-PURGE] Đại thanh trừng 48H (Epoch-based)
			if netMgr.SyncEngine != nil {
				netMgr.SyncEngine.UpdateHeight(height)
			}

			// 3. Đánh thức giao diện Dashboard tức thì (Real-time Block Notification)
			select {
			case s.blockUpdateChan <- struct{}{}:
			default:
			}

			// [VANGUARD-CACHE] Cập nhật bộ đệm khối tức thì
			go s.updateRecentBlocksCache(height)

			// 2. Chuyển tiếp tới CLIApp (để kích hoạt SnapshotManager)
			if oldOnCommitted != nil {
				oldOnCommitted(height)
			}
		}
		// [V1.60] Xử lý Rollback: Tái cấu trúc RAM tracker để UI khớp với chuỗi mới
		netMgr.OnRollback = func(targetHeight uint64) {
			log.Printf("[RPC-SYNC] 🔄 Phát hiện ROLLBACK về #%d. Đang tái cấu trúc Dashboard...", targetHeight)
			s.txTrackerMu.Lock()
			// 1. Xóa các giao dịch thuộc khối bị rollback trong RAM
			for txid, tx := range s.txTracker {
				if tx.BlockHeight > targetHeight {
					delete(s.txTracker, txid)
				}
			}
			// 2. Cập nhật lại txOrder (Duy trì thứ tự hiển thị)
			newOrder := make([]string, 0)
			for _, txid := range s.txOrder {
				if _, exists := s.txTracker[txid]; exists {
					newOrder = append(newOrder, txid)
				}
			}
			s.txOrder = newOrder
			s.txTrackerMu.Unlock()

			// 3. Nạp lại dữ liệu chuẩn từ Rust để đảm bảo tính toàn vẹn ( Source of TruSingleth)
			s.loadHistory()
		}
	}

	// [V2.7 PERSISTENCE] Tải lịch sử từ đĩa và khôi phục từ Blockchain
	s.loadHistory()
	go s.recoverHistory()

	// [VANGUARD-WARMUP] Nạp sẵn 200 khối gần nhất vào RAM để Dashboard hiện ngay lập tức
	go func() {
		log.Printf("[VANGUARD-WARMUP] 🏎️ Đang nạp sẵn 200 khối gần nhất vào RAM...")
		s.prepopulateBlockCache()
	}()

	// [V1.61 - UI STORAGE] Khởi động Luồng ghi Lịch sử Ví Độc Lập (Có cơ chế Cooldown bảo vệ đĩa)
	go func() {
		for range s.historyWriteChan {
			s.saveHistoryToFile()
			time.Sleep(3 * time.Second) // [VANGUARD-COOLDOWN] Tránh nghẽn đĩa bằng cách ngủ 3 giây trước khi xử lý yêu cầu tiếp theo
		}
	}()

	// [ANTI-SPAM-EVICTION] Goroutine dọn dẹp Token Bucket định kỳ mỗi 10 phút
	// Tại sao: Hacker spam hàng triệu request với senderHex ngẫu nhiên sẽ tạo ra hàng triệu entry
	// trong map rateLimiters mà không bao giờ bị xóa → rò rỉ RAM → OOM sập Node.
	// Giải pháp: Quét dọn định kỳ, xóa bucket không hoạt động quá 1 giờ.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.rateLimitersMu.Lock()
			now := time.Now()
			evicted := 0
			for key, bucket := range s.rateLimiters {
				if now.Sub(bucket.lastUpdate) > 1*time.Hour {
					delete(s.rateLimiters, key)
					evicted++
				}
			}
			s.rateLimitersMu.Unlock()
			if evicted > 0 {
				log.Printf("[ANTI-SPAM-EVICTION] 🧹 Đã dọn dẹp %d Token Bucket hết hạn (>1h không hoạt động). Còn lại: %d", evicted, len(s.rateLimiters))
			}
		}
	}()

	return s
}

// [V35 CONCORDANCE] SyncBlockToTracker: Cập nhật trạng thái giao dịch ngay khi khối được băm xong
// [V38.0 FIX-RACE] Thêm cơ chế Retry + Fallback để xử lý Race Condition khi RocksDB chưa flush xong
// [V39.0 MEGA-BLOCKS] Tối ưu hóa hiệu năng cực cao bằng cách tự tính TxID qua Go Native Hashing và Lazy Evaluation.
func (s *RPCServer) SyncBlockToTracker(height uint64) {
	// Tại sao thiết kế như vậy: Trì hoãn việc quét khối UI Tracker 2 giây để tránh bão gRPC/RocksDB 
	// tranh chấp CPU/IO với thợ đào ngay khoảnh khắc chuyển khối, giúp thợ đào tạo Block Template mới
	// và bắt đầu băm khối mới trên genz_miner.exe tức thì mà không gặp bất kỳ độ trễ nào.
	time.Sleep(2 * time.Second)

	log.Printf("[RPC-SYNC] 🔄 Đang quét Khối #%d (Tối ưu hóa Bulk) để cập nhật Dashboard...", height)

	// [V38.0 FIX-RACE] Retry tối đa 5 lần nếu block chưa sẵn sàng trong RocksDB
	// Tại sao: OnBlockCommitted chạy bằng goroutine, Rust Core có thể chưa flush xong dữ liệu
	var blockRaw []byte
	for attempt := 0; attempt < 5; attempt++ {
		blockRaw = s.bridge.GetBlock(height)
		if blockRaw != nil {
			break
		}
		if attempt < 4 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if blockRaw == nil {
		// [V38.0 FALLBACK] GetBlock thất bại sau 5 lần → Fallback trực tiếp về Rust Core
		log.Printf("[RPC-SYNC] ⚠️ GetBlock(#%d) thất bại sau 5 lần thử. Kích hoạt Fallback từ Rust Core...", height)
		s.syncBlockFromRustCore(height)
		return
	}

	var block pb_block.Block
	if err := proto.Unmarshal(blockRaw, &block); err != nil {
		return
	}
	body := block.Body
	if body == nil {
		return
	}

	userAddrs := s.getUserWalletAddresses()
	minerAddrHex := ""
	if s.minerAddr != nil {
		minerAddrHex = hex.EncodeToString(s.minerAddr)
	}

	// [V37.9.3] Tính tổng phí trong khối để cộng vào phần thưởng thợ đào
	totalFees := uint64(0)
	for _, tx := range body.Transactions {
		totalFees += tx.Fee
	}

	if block.Header == nil {
		return
	}
	ts := int64(block.Header.Timestamp)

	txIDs := make([]string, 0, len(body.Transactions))

	type pendingUpdate struct {
		txHashBytes []byte
		txHashStr   string
		sender      string
		receiver    string
		amount      uint64
		fee         uint64
		nonce       uint64
		isNew       bool
	}
	var userTxsToUpdate []pendingUpdate

	// [PHASE 1] QUÉT NHANH TRÊN RAM (ZERO gRPC & Zero Write Lock)
	for _, tx := range body.Transactions {
		// Băm TxID bằng Go Native (Chớp nhoáng, không gọi Rust qua gRPC)
		txHashBytes := node_p2p.GetSigningHashNative(tx)
		txHashStr := hex.EncodeToString(txHashBytes)
		txIDs = append(txIDs, txHashStr)

		sender := ""
		if tx.Sender != nil {
			sender = strings.ToLower(hex.EncodeToString(tx.Sender.Value))
		}
		receiver := ""
		if tx.Receiver != nil {
			receiver = strings.ToLower(hex.EncodeToString(tx.Receiver.Value))
		}

		// [V37.9.3] Nhận diện Coinbase
		isCoinbase := s.isTxCoinbase(tx)
		amount := tx.Amount
		if isCoinbase {
			blockReward := s.bridge.CalculateBlockRewardBtcZ(height)
			amount = blockReward + totalFees
			sender = ""
			if receiver == "" {
				receiver = strings.ToLower(minerAddrHex)
			}
		}

		isUserTx := userAddrs[sender] || userAddrs[receiver]

		s.txTrackerMu.RLock()
		tracked, existsInTracker := s.txTracker[txHashStr]
		var needsUpdate bool
		var isNew bool
		if existsInTracker {
			if tracked.Status == 0 || tracked.BlockHeight == 0 {
				needsUpdate = true
			}
		} else if isUserTx {
			needsUpdate = true
			isNew = true
		}
		s.txTrackerMu.RUnlock()

		if needsUpdate {
			userTxsToUpdate = append(userTxsToUpdate, pendingUpdate{
				txHashBytes: txHashBytes,
				txHashStr:   txHashStr,
				sender:      sender,
				receiver:    receiver,
				amount:      amount,
				fee:         tx.Fee,
				nonce:       tx.Nonce,
				isNew:       isNew,
			})
		}
	}

	// [PHASE 2] GỌI gRPC VÀ CẬP NHẬT THEO LÔ (Chỉ gọi cho User Txs đã được lọc)
	addedCount := 0

	var batchHashes [][]byte
	for _, item := range userTxsToUpdate {
		batchHashes = append(batchHashes, item.txHashBytes)
	}

	if len(batchHashes) > 0 {
		statusEntries, err := s.GetTransactionStatusBatchChunked(batchHashes)
		if err == nil {
			statusMap := make(map[string]*pb_block.TxStatusEntry)
			for _, entry := range statusEntries {
				statusMap[hex.EncodeToString(entry.Hash)] = entry
			}

			for _, item := range userTxsToUpdate {
				entry, ok := statusMap[item.txHashStr]
				if !ok {
					continue
				}

				statCode := entry.Status
				finalized := entry.IsFinalized
				confirmations := entry.Confirmations
				senderPrev := entry.SenderPrevBalance
				senderPost := entry.SenderPostBalance
				receiverPrev := entry.ReceiverPrevBalance
				receiverPost := entry.ReceiverPostBalance

				s.txTrackerMu.Lock()
				if item.isNew {
					// Thêm mới vào tracker
					errMsg := ""
					if statCode != 1 {
						errMsg = s.getTxStatusMessage(statCode)
					}
					t := &TrackedTx{
						TxID:          item.txHashStr,
						Sender:        item.sender,
						Receiver:      item.receiver,
						Amount:        item.amount,
						Fee:           item.fee,
						Nonce:         item.nonce,
						BlockHeight:   height,
						Status:        statCode,
						Timestamp:     ts,
						IsFinalized:   finalized,
						Confirmations: confirmations,
						ErrorMessage:  errMsg,
					}

					// Cập nhật số dư chốt sổ cho đối tượng mới thêm
					cleanReceiver := strings.ToLower(strings.TrimPrefix(item.receiver, "0x"))
					isReceiver := false
					if cleanReceiver != "" {
						isReceiver = userAddrs[cleanReceiver]
					}
					if isReceiver {
						t.PrevBalance = receiverPrev
						t.PostBalance = receiverPost
					} else {
						t.PrevBalance = senderPrev
						t.PostBalance = senderPost
					}

					s.txTracker[item.txHashStr] = t
					s.txOrder = append(s.txOrder, item.txHashStr)
					addedCount++
					log.Printf("[RPC-SYNC] 🆕 Thu nạp giao dịch mới của user từ khối: %s", safeShortID(item.txHashStr))
				} else {
					// Cập nhật giao dịch cũ sẵn có
					if t, ok := s.txTracker[item.txHashStr]; ok {
						t.BlockHeight = height
						t.Status = statCode
						t.IsFinalized = finalized
						t.Confirmations = confirmations

						if statCode != 1 {
							t.ErrorMessage = s.getTxStatusMessage(statCode)
						} else {
							t.ErrorMessage = ""
						}

						cleanReceiver := strings.ToLower(strings.TrimPrefix(t.Receiver, "0x"))
						isReceiver := false
						if cleanReceiver != "" {
							isReceiver = userAddrs[cleanReceiver]
						}
						if isReceiver {
							t.PrevBalance = receiverPrev
							t.PostBalance = receiverPost
						} else {
							t.PrevBalance = senderPrev
							t.PostBalance = senderPost
						}
						addedCount++
						log.Printf("[RPC-SYNC] 🔄 Cập nhật trạng thái giao dịch cũ: %s", safeShortID(item.txHashStr))
					}
				}
				s.txTrackerMu.Unlock()
			}
		}
	}

	// [VANGUARD-OPTIMIZATION] FIFO Ring Buffer bảo vệ RAM
	s.txTrackerMu.Lock()
	if len(s.txOrder) > maxTrackedTxs {
		toRemove := len(s.txOrder) - maxTrackedTxs
		for i := 0; i < toRemove; i++ {
			delete(s.txTracker, s.txOrder[i])
		}
		s.txOrder = s.txOrder[toRemove:]
	}
	s.txTrackerMu.Unlock()

	// [PHASE 3] DỌN DẸP MEMPOOL 1 LẦN DUY NHẤT (Bao gồm dọn theo hash và dọn theo nonce của sender)
	if s.netMgr != nil && s.netMgr.Mempool != nil {
		if len(txIDs) > 0 {
			s.netMgr.Mempool.RemoveTransactions(txIDs)
		}

		// [AUTOMATIC-STALE-CLEANUP-FIX] Tự động dọn dẹp các giao dịch stale trong Mempool khi có block mới
		// Tại sao: Xóa các giao dịch cũ có nonce nhỏ hơn nonce hiện tại của ví gửi khi có block mới.
		// Bằng cách thực hiện trực tiếp tại đây, chúng ta loại bỏ hoàn toàn race condition do RocksDB chưa kịp ghi block body 
		// khi gọi GetBlock(height) ở callback ngoài. Tại đây block body đã được unmarshal thành công.
		sendersSeen := make(map[string]bool)
		for _, tx := range body.Transactions {
			if tx.Sender != nil && len(tx.Sender.Value) > 0 {
				senderHex := hex.EncodeToString(tx.Sender.Value)
				sendersSeen[senderHex] = true
			}
		}
		// [VANGUARD-OPTIMIZATION] Gom tất cả địa chỉ thành 1 mảng và gọi Batch API
		// Triệt tiêu hoàn toàn bão gRPC (gRPC Storm) gây nghẽn Thread Pool của Rust
		var batchAddrs [][]byte
		for senderHex := range sendersSeen {
			if addr, err := hex.DecodeString(senderHex); err == nil {
				batchAddrs = append(batchAddrs, addr)
			}
		}

		if len(batchAddrs) > 0 {
			balances, err := s.GetBalanceBatchChunked(batchAddrs)
			if err == nil {
				senders := make([]string, len(balances))
				nonces := make([]uint64, len(balances))
				for idx, entry := range balances {
					senders[idx] = hex.EncodeToString(entry.Address)
					nonces[idx] = entry.Nonce
				}
				removed := s.netMgr.Mempool.RemoveStaleNonceTxsBatch(senders, nonces)
				if removed > 0 {
					log.Printf("[MEMPOOL-RPC-CLEANUP] 🧹 Đã dọn dẹp hàng loạt %d TX rác cho %d ví", removed, len(senders))
				}
			} else {
				log.Printf("[MEMPOOL-RPC-CLEANUP] ❌ Lỗi gọi Batch API dọn dẹp Mempool: %v", err)
			}
		}
	}

	// [V38.0] Nếu không tìm thấy giao dịch nào của người dùng qua GetBlock,
	// nhưng có thể đây là khối thợ đào với coinbase → fallback để chắc chắn
	if addedCount == 0 {
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.syncBlockFromRustCore(height)
		}()
	}

	// Lưu lịch sử bền vững ngay lập tức khi có khối mới
	s.triggerSave()
}

// [VANGUARD-CACHE] updateRecentBlocksCache: Tự động cập nhật 200 khối gần nhất vào RAM
// Tại sao: Tránh việc UI phải gọi gRPC 200 lần khi người dùng nhấn F5, giúp Dashboard hiện ra tức thì.
func (s *RPCServer) updateRecentBlocksCache(height uint64) {
	blockRaw := s.bridge.GetBlock(height)
	if blockRaw == nil {
		return
	}
	var b pb_block.Block
	if err := proto.Unmarshal(blockRaw, &b); err != nil {
		return
	}
	if b.Header == nil {
		return
	}

	header := b.Header
	minerAddr := ""
	if header.MinerAddress != nil {
		minerAddr = hex.EncodeToString(header.MinerAddress.Value)
	}
	stateRoot := ""
	if header.StateRoot != nil {
		stateRoot = hex.EncodeToString(header.StateRoot.Value)
	}
	txRoot := ""
	if header.TxRoot != nil {
		txRoot = hex.EncodeToString(header.TxRoot.Value)
	}

	headerBytes, _ := proto.Marshal(header)
	blockHash := s.bridge.GetCanonicalBlockHeaderHash(headerBytes, header.Height)

	newBlock := map[string]interface{}{
		"height":      height,
		"hash":        hex.EncodeToString(blockHash),
		"parent_hash": hex.EncodeToString(header.ParentHash.Value),
		"timestamp":   header.Timestamp,
		"miner":       minerAddr,
		"state_root":  stateRoot,
		"tx_root":     txRoot,
		"nonce":       header.Nonce,
		"tx_count":    len(b.Body.Transactions),
		"difficulty":  hex.EncodeToString(header.Difficulty),
	}

	s.recentBlocksCacheMu.Lock()
	defer s.recentBlocksCacheMu.Unlock()

	// 1. Kiểm tra trùng lặp
	exists := false
	for _, cachedObj := range s.recentBlocksCache {
		if cachedMap, ok := cachedObj.(map[string]interface{}); ok {
			if cachedMap["height"] == height {
				exists = true
				break
			}
		}
	}

	if !exists {
		// 2. Chèn vào mảng
		s.recentBlocksCache = append(s.recentBlocksCache, newBlock)

		// 3. Sắp xếp mảng giảm dần theo chiều cao (height)
		sort.SliceStable(s.recentBlocksCache, func(i, j int) bool {
			hI := s.recentBlocksCache[i].(map[string]interface{})["height"].(uint64)
			hJ := s.recentBlocksCache[j].(map[string]interface{})["height"].(uint64)
			return hI > hJ // Giảm dần
		})

		// 4. Giới hạn tối đa 200 khối để tiết kiệm RAM
		if len(s.recentBlocksCache) > 200 {
			s.recentBlocksCache = s.recentBlocksCache[:200]
		}
	}

	log.Printf("[VANGUARD-CACHE] ✅ Đã cập nhật Khối #%d vào bộ đệm RAM.", height)
}

// [VANGUARD-WARMUP] prepopulateBlockCache: Quét 200 khối gần nhất từ DB và đưa vào RAM
func (s *RPCServer) prepopulateBlockCache() {
	highest := s.bridge.GetCurrentVersion()
	limit := uint64(200)
	if highest < limit {
		limit = highest
	}

	var blocks []interface{}
	for i := uint64(0); i < limit; i++ {
		h := highest - i
		blockRaw := s.bridge.GetBlock(h)
		if blockRaw == nil {
			continue
		}
		var b pb_block.Block
		if err := proto.Unmarshal(blockRaw, &b); err != nil {
			continue
		}
		if b.Header == nil {
			continue
		}

		header := b.Header
		minerAddr := ""
		if header.MinerAddress != nil {
			minerAddr = hex.EncodeToString(header.MinerAddress.Value)
		}
		stateRoot := ""
		if header.StateRoot != nil {
			stateRoot = hex.EncodeToString(header.StateRoot.Value)
		}
		txRoot := ""
		if header.TxRoot != nil {
			txRoot = hex.EncodeToString(header.TxRoot.Value)
		}

		headerBytes, _ := proto.Marshal(header)
		blockHash := s.bridge.GetCanonicalBlockHeaderHash(headerBytes, header.Height)

		blocks = append(blocks, map[string]interface{}{
			"height":      h,
			"hash":        hex.EncodeToString(blockHash),
			"parent_hash": hex.EncodeToString(header.ParentHash.Value),
			"timestamp":   header.Timestamp,
			"miner":       minerAddr,
			"state_root":  stateRoot,
			"tx_root":     txRoot,
			"nonce":       header.Nonce,
			"tx_count":    len(b.Body.Transactions),
			"difficulty":  hex.EncodeToString(header.Difficulty),
		})
	}

	s.recentBlocksCacheMu.Lock()
	s.recentBlocksCache = blocks
	s.recentBlocksCacheMu.Unlock()
	log.Printf("[VANGUARD-WARMUP] ✅ Đã nạp xong %d khối vào bộ đệm RAM.", len(blocks))
}

// [V38.0 FIX-RACE] syncBlockFromRustCore: Fallback khi GetBlock thất bại hoặc không tìm thấy giao dịch
func (s *RPCServer) syncBlockFromRustCore(height uint64) {
	userAddrs := s.getUserWalletAddresses()
	if len(userAddrs) == 0 {
		return
	}

	addedCount := 0
	for addrHex := range userAddrs {
		addrBytes, err := hex.DecodeString(addrHex)
		if err != nil {
			continue
		}

		txs, err := s.bridge.GetTransactionsByAddress(addrBytes)
		if err != nil {
			continue
		}

		s.txTrackerMu.Lock()
		for _, tx := range txs {
			// Chỉ xử lý giao dịch thuộc khối mục tiêu
			if tx.BlockHeight != height {
				continue
			}

			txID := hex.EncodeToString(tx.TxId)

			// [CRITICAL FIX] Nếu đã có trong tracker, PHẢI cập nhật nó thành Thành Công (Thoát Mempool)
			if existing, exists := s.txTracker[txID]; exists {
				if existing.BlockHeight == 0 {
					existing.BlockHeight = tx.BlockHeight
					existing.Status = tx.Status
					existing.IsFinalized = tx.IsFinalized
					existing.Confirmations = tx.Confirmations
					// Cập nhật số dư chốt sổ
					existing.PrevBalance = tx.SenderPrevBalance
					existing.PostBalance = tx.SenderPostBalance
					log.Printf("[RPC-SYNC-FALLBACK] 🔄 Đã cập nhật giao dịch %s từ Mempool vào Khối #%d", safeShortID(txID), height)
				}
				continue
			}

			sender := strings.ToLower(hex.EncodeToString(tx.Sender))
			receiver := strings.ToLower(hex.EncodeToString(tx.Receiver))

			// [V37.9.3] Nhận diện Coinbase
			isZeroSender := sender == "0000000000000000000000000000000000000000000000000000000000000000" || sender == ""
			if isZeroSender {
				sender = "" // Để UI hiển thị "PHẦN THƯỞNG KHỐI"
			}

			tracked := &TrackedTx{
				TxID:          txID,
				Sender:        sender,
				Receiver:      receiver,
				Amount:        tx.Amount,
				Fee:           tx.Fee,
				Timestamp:     tx.Timestamp,
				BlockHeight:   tx.BlockHeight,
				Nonce:         tx.Nonce,
				Status:        tx.Status,
				IsFinalized:   tx.IsFinalized,
				Confirmations: tx.Confirmations,
				ErrorMessage:  tx.ErrorMessage,
			}

			// Snapshot số dư
			if isZeroSender {
				tracked.PrevBalance = tx.ReceiverPrevBalance
				tracked.PostBalance = tx.ReceiverPostBalance
			} else if strings.ToLower(receiver) == addrHex {
				tracked.PrevBalance = tx.ReceiverPrevBalance
				tracked.PostBalance = tx.ReceiverPostBalance
			} else {
				tracked.PrevBalance = tx.SenderPrevBalance
				tracked.PostBalance = tx.SenderPostBalance
			}

			s.txTracker[txID] = tracked
			s.txOrder = append(s.txOrder, txID)
			addedCount++
			log.Printf("[RPC-SYNC-FALLBACK] 🆕 Thu nạp giao dịch tại Khối #%d: %s (Amount=%d)", height, safeShortID(txID), tx.Amount)

			// [VANGUARD-OPTIMIZATION] FIFO Ring Buffer bảo vệ RAM và đĩa cứng khi đồng bộ hàng loạt
			if len(s.txOrder) > maxTrackedTxs {
				toRemove := len(s.txOrder) - maxTrackedTxs
				for i := 0; i < toRemove; i++ {
					delete(s.txTracker, s.txOrder[i])
				}
				s.txOrder = s.txOrder[toRemove:]
			}
		}
		s.txTrackerMu.Unlock()
	}

	if addedCount > 0 {
		// Kích hoạt SSE để UI biết có dữ liệu mới
		select {
		case s.txUpdateChan <- struct{}{}:
		default:
		}
		s.triggerSave()
		log.Printf("[RPC-SYNC-FALLBACK] ✅ Đã thu nạp %d giao dịch cho Khối #%d từ Rust Core.", addedCount, height)
	}
}

// [V1.60] Quản lý cấu hình Node thông qua Rust Core (CF_META)
func (s *RPCServer) loadNodeConfig() {
	log.Printf("[CONFIG-V1.60] 📥 Đang nạp cấu hình từ Rust Core...")
	data, err := s.bridge.GetNodeConfig()
	if err != nil || len(data) == 0 {
		log.Printf("[CONFIG] ℹ️ Chưa có cấu hình trong Rust. Sử dụng chế độ hiện tại: %s.", s.nodeMode)
		if s.cpuIntensity == 0 {
			s.cpuIntensity = 50
		}
		return
	}

	var cfg NodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[CONFIG] ❌ Lỗi giải mã cấu hình từ Rust: %v", err)
		return
	}
	s.cpuIntensity = cfg.CpuIntensity
	if cfg.MiningDevice != "" {
		s.miningDevice = cfg.MiningDevice
	} else {
		s.miningDevice = "cpu"
	}
	s.updateGpuEnvCheck()
	// [VANGUARD-DISCIPLINE] Mặc định tắt đào khi khởi động để đảm bảo an toàn,
	// TRỪ KHI người dùng đã chủ động bật qua cờ --mining từ CLI.
	if s.nodeMode != "full-mining" {
		s.nodeMode = "verify-only"
		s.updateMiningState()
		s.saveNodeConfig() // [V2.5] Ghi đè cấu hình để lần sau khởi động vẫn Tắt
		log.Printf("[CONFIG] 🛡️ Kỷ luật: Chế độ đào đã được ép TẮT và lưu trữ.")
	} else {
		s.updateMiningState()
		log.Printf("[CONFIG] 🔥 Chế độ đào được giữ nguyên từ lệnh CLI: %s (Thiết bị: %s)", s.nodeMode, s.miningDevice)
	}

	// [VANGUARD-NODEID-AUDIT] Chỉ nạp địa chỉ ví đào từ database nếu NodeID trùng khớp
	var currentNodeID string
	if s.netMgr != nil && s.netMgr.Host != nil {
		currentNodeID = s.netMgr.Host.ID().String()
	}

	if cfg.RewardAddress != "" {
		// Chỉ khôi phục địa chỉ ví nhận thưởng nếu cấu hình này thuộc về chính NodeID hiện tại,
		// Tránh trường hợp nạp database scl từ máy khác sang nhận nhầm ví đào cũ.
		if cfg.NodeID == "" || cfg.NodeID == currentNodeID {
			addr, err := hex.DecodeString(strings.TrimPrefix(cfg.RewardAddress, "0x"))
			if err == nil && len(addr) == 32 {
				s.minerAddr = addr
				if s.cliApp != nil {
					s.cliApp.SetMinerAddress(addr, nil, "")
				}
				log.Printf("[CONFIG] ✅ Đã khôi phục RewardAddress từ cấu hình: %s", cfg.RewardAddress)
			}
		} else {
			log.Printf("[CONFIG-WARN] ⚠️ Phát hiện NodeID cấu hình cũ (%s) không khớp với NodeID hiện tại (%s). Bỏ qua RewardAddress cũ để bảo vệ tài sản.", cfg.NodeID, currentNodeID)
		}
	}

	if s.cliApp != nil {
		s.cliApp.SetNodeMode(s.nodeMode)
	}
	log.Printf("[CONFIG] ✅ Đã khôi phục cấu hình từ Rust: Mode=%s, Intensity=%d%%.", s.nodeMode, s.cpuIntensity)
}

func (s *RPCServer) updateMiningState() {
	s.nodeModeMu.RLock()
	mode := s.nodeMode
	s.nodeModeMu.RUnlock()

	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	if mode == "full-mining" && (device == "cpu" || device == "hybrid") {
		s.bridge.SetMiningPause(false)
	} else {
		s.bridge.SetMiningPause(true)
	}
}

func (s *RPCServer) isMiningAllowed() bool {
	s.nodeModeMu.RLock()
	mode := s.nodeMode
	s.nodeModeMu.RUnlock()
	if mode != "full-mining" {
		return false
	}

	// Genesis bootloader: luôn cho phép đào để kích hoạt chuỗi
	if s.bridge.GetCurrentVersion() == 0 {
		return true
	}

	// Đọc số lượng peer hiện tại
	var peerCount int
	s.minerStreamsMu.RLock()
	if s.netMgr != nil && s.netMgr.Host != nil {
		peerCount = len(s.netMgr.Host.Network().Peers())
	}
	s.minerStreamsMu.RUnlock()

	// Cho phép đào nếu: có internet hoạt động HOẶC có ít nhất 1 peer kết nối (cho mạng local/private)
	isOffline := atomic.LoadInt32(&s.internetOffline) == 1
	if isOffline {
		return false
	}

	// [VANGUARD-SYNC-SHIELD] Chỉ cho phép đào khi đồng bộ hoàn tất (hoặc có vi phạm đồng bộ từ peer)
	if s.netMgr != nil && s.netMgr.SyncEngine != nil && !s.netMgr.SyncEngine.IsSynced() {
		return false
	}

	return !isOffline || peerCount > 0
}

func (s *RPCServer) isMiningActive() bool {
	s.nodeModeMu.RLock()
	mode := s.nodeMode
	s.nodeModeMu.RUnlock()
	if mode != "full-mining" {
		return false
	}

	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	// Nếu mất kết nối mạng thì băm tự động bị tạm dừng ở tất cả thiết bị
	if !s.isMiningAllowed() {
		return false
	}

	if device == "gpu" {
		return true
	}

	return !s.bridge.IsMiningPaused()
}

func (s *RPCServer) StartConfiguredMiners() {
	s.nodeModeMu.RLock()
	isFullMining := s.nodeMode == "full-mining"
	s.nodeModeMu.RUnlock()

	if !isFullMining {
		return
	}

	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	go func() {
		time.Sleep(1 * time.Second) // Chờ gRPC / REST Server hoàn toàn lắng nghe

		if device == "cpu" || device == "hybrid" {
			s.minerStreamsMu.Lock()
			activeStreams := len(s.minerStreams)
			s.minerStreamsMu.Unlock()
			if activeStreams == 0 {
				s.bridge.StopGenzMiner() // Đảm bảo dừng tiến trình cũ để tránh xung đột cổng
				if err := s.bridge.StartGenzMiner(s.port + 10000); err != nil {
					log.Printf("[BRIDGE] ⚠️ Không thể tự động khởi chạy thợ đào genz_miner: %v", err)
				}
			}
		} else {
			// Nếu chỉ chọn GPU, dừng CPU miner cũ nếu đang chạy
			s.bridge.StopGenzMiner()
		}

		if device == "gpu" || device == "hybrid" {
			s.bridge.StopGpuMiner() // Đảm bảo dừng tiến trình cũ tránh chiếm dụng CUDA
			if err := s.bridge.StartGpuMiner(s.port); err != nil {
				log.Printf("[BRIDGE] ⚠️ Không thể tự động khởi chạy thợ đào yona_gpu_miner: %v", err)
			}
		} else {
			// Nếu chỉ chọn CPU, dừng GPU miner cũ nếu đang chạy
			s.bridge.StopGpuMiner()
		}
	}()
}

func (s *RPCServer) StopConfiguredMiners() {
	s.bridge.StopGenzMiner()
	s.bridge.StopGpuMiner()
}

func (s *RPCServer) saveNodeConfig() {
	var currentNodeID string
	if s.netMgr != nil && s.netMgr.Host != nil {
		currentNodeID = s.netMgr.Host.ID().String()
	}

	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	cfg := NodeConfig{
		CpuIntensity:  s.cpuIntensity,
		NodeMode:      s.nodeMode,
		RewardAddress: hex.EncodeToString(s.minerAddr),
		NodeID:        currentNodeID, // Lưu kèm NodeID hiện tại vào database cấu hình
		MiningDevice:  device,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}

	if err := s.bridge.SetNodeConfig(data); err != nil {
		log.Printf("[CONFIG] ❌ Không thể lưu cấu hình vào Rust: %v", err)
	} else {
		log.Printf("[CONFIG] ✅ Đã đồng bộ cấu hình vào Rust Core (NodeID: %s).", currentNodeID)
	}
}

// [V1.60] Đã chuyển giao toàn bộ gánh nặng lưu trữ sang Rust Core (Atomic Persistence)
// Go giờ đây chỉ là lớp hiển thị (Stateless Display Layer)

// [V1.61] Khôi phục từ UI Storage và Đối soát với Rust Core
func (s *RPCServer) loadHistory() {
	log.Printf("[RPC-UI] 🛰️ Đang khôi phục lịch sử giao diện từ Kho lưu trữ Độc lập...")
	s.txTrackerMu.Lock()
	s.txTracker = make(map[string]*TrackedTx)
	s.txOrder = make([]string, 0)
	s.txTrackerMu.Unlock()

	// 1. ĐỌC DỮ LIỆU TỪ JSON UI STORAGE
	currentVersion := s.bridge.GetCurrentVersion()
	filePath := filepath.Join(s.dbPath, "wallets", "ui_tx_history.json")
	zombieCount := 0
	if data, err := os.ReadFile(filePath); err == nil {
		var savedHistory []*TrackedTx
		if json.Unmarshal(data, &savedHistory) == nil {
			s.txTrackerMu.Lock()
			for _, tx := range savedHistory {
				// [VANGUARD-PURGE] Xóa bỏ "Giao dịch ma" (Zombie Txs)
				// Đây là các giao dịch thuộc về các khối "Tương lai" đã bị Rollback hoặc do DB lỗi.
				if tx.BlockHeight > currentVersion {
					zombieCount++
					continue
				}
				// [AUTO-HEAL STATUS CODE] Chữa lành mã lỗi hiển thị stale nhầm sang tự gửi
				if tx.Status == 9 && tx.Sender != tx.Receiver {
					tx.Status = 3
					tx.ErrorMessage = "Sai số thứ tự giao dịch (Nonce Mismatch / Stale)"
				}
				s.txTracker[tx.TxID] = tx
				s.txOrder = append(s.txOrder, tx.TxID)
			}
			s.txTrackerMu.Unlock()

			if zombieCount > 0 {
				log.Printf("[RPC-UI] 📂 Đã nạp lịch sử: %d thực thể (Đã quét sạch %d giao dịch ma thuộc khối tương lai > #%d)", len(s.txOrder), zombieCount, currentVersion)
				s.triggerSave() // Ghi đè file sạch xuống đĩa
			} else {
				log.Printf("[RPC-UI] 📂 Đã nạp %d giao dịch từ ui_tx_history.json", len(savedHistory))
			}
		}
	}

	// 2. ĐỐI SOÁT VỚI SỰ THẬT TỪ RUST CORE (Cập nhật từ Ledger)
	userAddrs := s.getUserWalletAddresses()
	for addrStr := range userAddrs {
		addrBytes, _ := hex.DecodeString(addrStr)

		// [VANGUARD-RECOVERY] Tự động khôi phục lịch sử thợ đào nếu chưa được index
		s.nodeModeMu.RLock()
		isMining := s.nodeMode == "full-mining"
		s.nodeModeMu.RUnlock()
		if isMining {
			go func(ab []byte) {
				s.bridge.ReindexMinerHistory(ab)
			}(addrBytes)
		}

		rustTxs, err := s.bridge.GetTransactionsByAddress(addrBytes)
		if err != nil {
			continue
		}

		s.txTrackerMu.Lock()
		for _, rt := range rustTxs {
			txHash := hex.EncodeToString(rt.TxId)
			tracked, exists := s.txTracker[txHash]
			if !exists {
				tracked = &TrackedTx{TxID: txHash, Sender: hex.EncodeToString(rt.Sender), Receiver: hex.EncodeToString(rt.Receiver)}
				s.txTracker[txHash] = tracked
				s.txOrder = append(s.txOrder, txHash)
			}

			// Cập nhật trạng thái từ Rust Ledger
			tracked.Amount = rt.Amount
			tracked.Fee = rt.Fee
			tracked.Timestamp = rt.Timestamp
			tracked.BlockHeight = rt.BlockHeight
			tracked.Nonce = rt.Nonce
			tracked.IsFinalized = rt.IsFinalized
			tracked.Confirmations = rt.Confirmations
			tracked.Status = 1 // Success if in Ledger

			// Cập nhật lỗi (nếu có)
			if rt.Status != 1 && rt.Status != 0 {
				tracked.ErrorMessage = s.getTxStatusMessage(rt.Status)
			} else {
				tracked.ErrorMessage = ""
			}
		}
		s.txTrackerMu.Unlock()
	}

	// 3. ĐỐI SOÁT VỚI MEMPOOL (Giao dịch đang chờ)
	if s.netMgr != nil && s.netMgr.Mempool != nil {
		pendingList := s.netMgr.Mempool.GetPendingTxList()
		s.txTrackerMu.Lock()
		for _, pt := range pendingList {
			// [V38.8] Nạp TẤT CẢ mempool vào tracker (không lọc địa chỉ)
			// Vì Tracker là Global, việc nạp all giúp UI luôn đồng bộ cho mọi ví.

			tracked, exists := s.txTracker[pt.Hash]
			if !exists {
				tracked = &TrackedTx{
					TxID:        pt.Hash,
					Sender:      pt.Sender,
					Receiver:    pt.Receiver,
					Amount:      pt.Amount,
					Fee:         pt.Fee,
					Timestamp:   pt.Timestamp,
					BlockHeight: 0,
					Nonce:       pt.Nonce,
					Status:      0, // Mempool Pending
				}
				s.txTracker[pt.Hash] = tracked
				s.txOrder = append(s.txOrder, pt.Hash)
				log.Printf("[RPC-UI] 🧩 Đã khôi phục giao dịch Mempool từ P2P: %s", safeShortID(pt.Hash))
			} else {
				// [CRITICAL-FIX] Nếu đã tồn tại nhưng mempool báo vẫn đang chờ -> Ép Height = 0
				// Điều này xử lý trường hợp JSON lưu sai hoặc bị Reorg.
				if tracked.BlockHeight != 0 {
					log.Printf("[RPC-UI] ⚠️ Reset Height=0 cho TX %s (Vẫn trong Mempool)", safeShortID(pt.Hash))
					tracked.BlockHeight = 0
					tracked.Status = 0
				}
			}
		}
		s.txTrackerMu.Unlock()

		// [AUTOMATIC-CLEANUP-ZOMBIE-PENDING] Bước 3.5: Dọn dẹp giao dịch Pending cũ bị kẹt
		// Tại sao thiết kế như vậy: Khi khởi động lại Node, Mempool đã được dọn sạch hoàn toàn khỏi RocksDB.
		// Các giao dịch Pending cũ còn lưu trong file JSON UI history sẽ không bao giờ được gửi đi hay đóng khối.
		// Xóa sạch chúng giúp giao diện UI hiển thị chính xác trạng thái sạch sẽ từ đầu.
		pendingHashes := make(map[string]bool)
		for _, pt := range pendingList {
			pendingHashes[pt.Hash] = true
		}

		s.txTrackerMu.Lock()
		var cleanOrder []string
		zombieCleanupCount := 0
		for _, txid := range s.txOrder {
			tx := s.txTracker[txid]
			if tx.BlockHeight == 0 && tx.Status == 0 && !pendingHashes[txid] {
				delete(s.txTracker, txid)
				zombieCleanupCount++
			} else {
				cleanOrder = append(cleanOrder, txid)
			}
		}
		s.txOrder = cleanOrder
		s.txTrackerMu.Unlock()

		if zombieCleanupCount > 0 {
			log.Printf("[RPC-UI] 🧹 Đã dọn sạch %d giao dịch Pending cũ bị kẹt khỏi giao diện (Zombie Cleanup).", zombieCleanupCount)
			s.triggerSave()
		}
	}

	// 4. SẮP XẾP LẠI (Cũ nhất lên đầu để append đúng thứ tự thời gian)
	s.txTrackerMu.Lock()
	sort.Slice(s.txOrder, func(i, j int) bool {
		return s.txTracker[s.txOrder[i]].Timestamp < s.txTracker[s.txOrder[j]].Timestamp
	})
	s.txTrackerMu.Unlock()

	s.triggerSave()
	log.Printf("[RPC-V1.60] ✅ Đã hoàn tất khôi phục %d giao dịch chính chủ.", len(s.txTracker))
}

// recoverHistory: (V1.60) Đã hợp nhất vào loadHistory. Không còn cần quét Blockchain thủ công.
func (s *RPCServer) recoverHistory() {
	// [VANGUARD-FIX] Đợi 3 giây để đảm bảo gRPC Bridge đã xác thực thành công trước khi quét nặng
	time.Sleep(3 * time.Second)

	log.Printf("[RPC-RECOVER] 🔍 Bắt đầu quét Sổ cái để khôi phục lịch sử cho toàn bộ ví...")
	userAddrs := s.getUserWalletAddresses()
	if len(userAddrs) == 0 {
		log.Printf("[RPC-RECOVER] ℹ️ Không có địa chỉ ví nào để khôi phục.")
		return
	}

	count := 0
	for addrHex := range userAddrs {
		addrBytes, err := hex.DecodeString(addrHex)
		if err != nil {
			continue
		}

		// [VANGUARD-RECOVERY-FIX] Khôi phục lịch sử thợ đào dựa trên 'Tổng cung phát hành' (Issuance Schedule)
		// Lệnh này quét lại các khối cũ để tìm phần thưởng miner bị thiếu index
		s.nodeModeMu.RLock()
		isMining := s.nodeMode == "full-mining"
		s.nodeModeMu.RUnlock()
		if isMining {
			if err := s.bridge.ReindexMinerHistory(addrBytes); err != nil {
				log.Printf("[RPC-RECOVER] ⚠️ Lỗi khi reindex miner history cho %s: %v", addrHex[:8], err)
			}
		}

		// Gọi Rust Core lấy lịch sử giao dịch từ RocksDB (CF_RECEIPTS) thông qua gRPC
		txs, err := s.bridge.GetTransactionsByAddress(addrBytes)
		if err != nil {
			log.Printf("[RPC-RECOVER] ⚠️ Không thể lấy lịch sử cho %s: %v", addrHex[:8], err)
			continue
		}

		// Lấy số dư hiện tại của địa chỉ này làm mốc tính ngược
		currentBal := s.bridge.GetBalance(nil, addrBytes, 0)

		// Sắp xếp txs từ mới nhất đến cũ nhất (giảm dần theo BlockHeight, sau đó theo Timestamp)
		sort.Slice(txs, func(i, j int) bool {
			if txs[i].BlockHeight != txs[j].BlockHeight {
				return txs[i].BlockHeight > txs[j].BlockHeight
			}
			return txs[i].Timestamp > txs[j].Timestamp
		})

		// Tính toán số dư ngược lũy tiến trong bộ nhớ
		type balanceSnapshot struct {
			prev uint64
			post uint64
		}
		calculatedBalances := make(map[string]balanceSnapshot)
		runningBal := currentBal

		for _, tx := range txs {
			txID := hex.EncodeToString(tx.TxId)
			sender := strings.ToLower(hex.EncodeToString(tx.Sender))
			isZeroSender := sender == "0000000000000000000000000000000000000000000000000000000000000000" || sender == ""

			isOut := sender == addrHex && !isZeroSender

			post := runningBal
			var prev uint64
			if isOut {
				// Nếu gửi đi thành công: Số dư trước đó = Số dư sau đó + Số tiền + Phí
				if tx.Status == 1 {
					prev = runningBal + tx.Amount + tx.Fee
				} else {
					prev = runningBal
				}
			} else {
				// Nếu nhận về thành công: Số dư trước đó = Số dư sau đó - Số tiền
				if tx.Status == 1 {
					if runningBal >= tx.Amount {
						prev = runningBal - tx.Amount
					} else {
						prev = 0
					}
				} else {
					prev = runningBal
				}
			}
			calculatedBalances[txID] = balanceSnapshot{prev: prev, post: post}
			runningBal = prev
		}

		s.txTrackerMu.Lock()
		for _, tx := range txs {
			txID := hex.EncodeToString(tx.TxId)
			sender := strings.ToLower(hex.EncodeToString(tx.Sender))
			receiver := strings.ToLower(hex.EncodeToString(tx.Receiver))
			isZeroSender := sender == "0000000000000000000000000000000000000000000000000000000000000000" || sender == ""

			// Nếu đã có trong tracker, ta cập nhật các trường và gán số dư nếu bằng 0
			if existing, exists := s.txTracker[txID]; exists {
				if existing.BlockHeight > 0 {
					if existing.PostBalance == 0 {
						if snapshot, ok := calculatedBalances[txID]; ok {
							existing.PrevBalance = snapshot.prev
							existing.PostBalance = snapshot.post
						}
					}
					continue
				}
				// Cập nhật trạng thái mới nhất từ Rust Core
				existing.BlockHeight = tx.BlockHeight
				existing.Status = tx.Status
				existing.IsFinalized = tx.IsFinalized
				existing.Confirmations = tx.Confirmations
				if existing.PostBalance == 0 {
					if snapshot, ok := calculatedBalances[txID]; ok {
						existing.PrevBalance = snapshot.prev
						existing.PostBalance = snapshot.post
					}
				}
				continue
			}

			tracked := &TrackedTx{
				TxID:          txID,
				Sender:        sender,
				Receiver:      receiver,
				Amount:        tx.Amount,
				Fee:           tx.Fee,
				Timestamp:     tx.Timestamp,
				BlockHeight:   tx.BlockHeight,
				Nonce:         tx.Nonce,
				Status:        tx.Status,
				IsFinalized:   tx.IsFinalized,
				Confirmations: tx.Confirmations,
				ErrorMessage:  tx.ErrorMessage,
			}

			if isZeroSender {
				tracked.Sender = ""
			}

			// Nạp balance từ calculatedBalances
			if snapshot, ok := calculatedBalances[txID]; ok {
				tracked.PrevBalance = snapshot.prev
				tracked.PostBalance = snapshot.post
			} else {
				if strings.ToLower(receiver) == strings.ToLower(addrHex) && !isZeroSender {
					tracked.PrevBalance = tx.ReceiverPrevBalance
					tracked.PostBalance = tx.ReceiverPostBalance
				} else {
					tracked.PrevBalance = tx.SenderPrevBalance
					tracked.PostBalance = tx.SenderPostBalance
				}
			}

			s.txTracker[txID] = tracked

			// Thêm vào txOrder nếu chưa có (Dùng để hiển thị theo thứ tự)
			found := false
			for _, id := range s.txOrder {
				if id == txID {
					found = true
					break
				}
			}
			if !found {
				s.txOrder = append(s.txOrder, txID)
			}
			count++
		}
		s.txTrackerMu.Unlock()
	}

	if count > 0 {
		log.Printf("[RPC-RECOVER] ✅ Khôi phục thành công %d giao dịch từ Sổ cái vật lý.", count)
		s.triggerSave() // [V1.63] Lưu ngay lập tức sau khi khôi phục thành công
		// Sắp xếp lại txOrder theo timestamp giảm dần (Mới nhất lên đầu)
		s.txTrackerMu.Lock()
		sort.Slice(s.txOrder, func(i, j int) bool {
			// Phòng thủ nil
			txI := s.txTracker[s.txOrder[i]]
			txJ := s.txTracker[s.txOrder[j]]
			if txI == nil || txJ == nil {
				return false
			}
			return txI.Timestamp > txJ.Timestamp
		})
		s.txTrackerMu.Unlock()
		s.triggerSave()
	} else {
		log.Printf("[RPC-RECOVER] ℹ️ Không tìm thấy giao dịch mới nào trên chuỗi.")
	}
}

// [V1.61 UI STORAGE] Kích hoạt luồng ghi đĩa
func (s *RPCServer) triggerSave() {
	select {
	case s.historyWriteChan <- struct{}{}:
	default: // Nếu kênh đang bận (đã có lệnh ghi đang chờ), bỏ qua để không nghẽn
	}
}

func (s *RPCServer) snapshotBalances(tracked *TrackedTx) {
	// [V37.9.8] Cho phép cập nhật nếu PostBalance = 0 để phục vụ khôi phục trạng thái
	if tracked.PostBalance != 0 {
		return
	}

	// Tránh snapshot khi SCL đang ở trạng thái 0 (mới khởi động hoặc đang recovery)
	if s.bridge.GetCurrentVersion() == 0 && tracked.BlockHeight > 1 {
		return
	}
	cleanSender := strings.TrimPrefix(tracked.Sender, "0x")
	cleanReceiver := strings.TrimPrefix(tracked.Receiver, "0x")

	walletAddrs := s.getUserWalletAddresses()
	snapshotAddr := cleanSender
	isOut := walletAddrs[strings.ToLower(cleanSender)]
	isIn := walletAddrs[strings.ToLower(cleanReceiver)]

	if isIn && !isOut {
		snapshotAddr = cleanReceiver
	}

	addrBytes, err := hex.DecodeString(snapshotAddr)
	if err == nil && len(addrBytes) == 32 {
		// [VANGUARD-TRUTH FIX] Lấy số dư thực tế từ Ledger và tính toán ngược
		currentBal := s.bridge.GetBalance(nil, addrBytes, 0)
		tracked.PostBalance = currentBal

		isOut := walletAddrs[strings.ToLower(cleanSender)]
		if isOut && tracked.Status == 1 {
			// Nếu là tiền gửi đi thành công: Số dư Trước = Số dư Sau + (Tiền + Phí)
			tracked.PrevBalance = currentBal + tracked.Amount + tracked.Fee
		} else if !isOut && tracked.Status == 1 {
			// Nếu là tiền nhận về thành công: Số dư Trước = Số dư Sau - Tiền
			if currentBal >= tracked.Amount {
				tracked.PrevBalance = currentBal - tracked.Amount
			} else {
				tracked.PrevBalance = currentBal
			}
		} else {
			// Trường hợp chưa xác định hoặc thất bại: Để trung thực (Prev == Post)
			tracked.PrevBalance = currentBal
		}

		// [VANGUARD-THROTTLE] Tạm thời tắt log audit quá dày đặc để tránh nghẽn I/O khi khôi phục lịch sử
		// log.Printf("[RPC-AUDIT] 🏦 REAL-SNAPSHOT: Tx=%s, Prev=%d, Post=%d (Truth Mode)", tracked.TxID[:8], tracked.PrevBalance, tracked.PostBalance)
	}
}

func iif64(cond bool, a, b uint64) uint64 {
	if cond {
		return a
	}
	return b
}

func iifString(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// getUserWalletAddresses: Trả về danh sách địa chỉ ví của người dùng từ RAM Cache.
// [VANGUARD-CACHE] Tại sao cần Cache: Khi sync block có 10.000 giao dịch, hàm này bị gọi
// hàng nghìn lần liên tục. Nếu mỗi lần đều đọc file JSON/DB từ đĩa cứng qua ListWallets(),
// sẽ tạo ra nút thắt I/O cổ chai nghiêm trọng làm "nghẽn chết" Node.
// Giải pháp: Cache danh sách ví trong RAM với Cooldown 10 giây. Chỉ đọc đĩa khi Cache hết hạn.
func (s *RPCServer) getUserWalletAddresses() map[string]bool {
	s.walletCacheMu.RLock()
	if time.Since(s.lastWalletUpdate) < 10*time.Second && len(s.cachedWallets) > 0 {
		// Cache còn hiệu lực → Trả về bản sao an toàn từ RAM (Zero I/O)
		result := make(map[string]bool, len(s.cachedWallets))
		for k, v := range s.cachedWallets {
			result[k] = v
		}
		s.walletCacheMu.RUnlock()
		return result
	}
	s.walletCacheMu.RUnlock()

	s.walletCacheMu.Lock()
	defer s.walletCacheMu.Unlock()

	// Double-check sau khi lấy Write Lock để tránh tranh chấp từ các Goroutine khác
	if time.Since(s.lastWalletUpdate) < 10*time.Second && len(s.cachedWallets) > 0 {
		result := make(map[string]bool, len(s.cachedWallets))
		for k, v := range s.cachedWallets {
			result[k] = v
		}
		return result
	}

	// Chỉ duy nhất một Goroutine thực hiện đọc đĩa khi hết hạn cache
	wallets := make(map[string]bool)
	all, _ := s.walletMgr.ListWallets()
	for _, w := range all {
		addr := strings.ToLower(strings.TrimPrefix(w.Address, "0x"))
		wallets[addr] = true
	}
	if s.minerAddr != nil {
		wallets[strings.ToLower(hex.EncodeToString(s.minerAddr))] = true
	}

	s.cachedWallets = wallets
	s.lastWalletUpdate = time.Now()

	result := make(map[string]bool, len(wallets))
	for k, v := range wallets {
		result[k] = v
	}
	return result
}

// [V37.9.3] Kiểm tra giao dịch có phải là Coinbase (Phần thưởng)
func (s *RPCServer) isTxCoinbase(tx *pb_block.Transaction) bool {
	if tx == nil || tx.Sender == nil || len(tx.Sender.Value) == 0 {
		return true
	}
	for _, b := range tx.Sender.Value {
		if b != 0 {
			return false
		}
	}
	return true
}

func iif(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// [V1.61 UI STORAGE] Ghi Lịch sử Ví ra kho lưu trữ độc lập
// [GC-FRIENDLY] Sử dụng Stream Encoder ghi thẳng xuống đĩa thay vì tạo mảng byte khổng lồ trong RAM.
// Tại sao: json.Marshal(history) tạo ra một slice byte có kích thước bằng toàn bộ lịch sử giao dịch,
// gây shock GC và tăng áp lực bộ nhớ không cần thiết. Stream Encoder ghi từng phần nhỏ xuống file.
func (s *RPCServer) saveHistoryToFile() {
	s.txTrackerMu.RLock()
	// Sao chép txOrder để tránh giữ lock lâu khi ghi đĩa
	orderCopy := make([]string, len(s.txOrder))
	copy(orderCopy, s.txOrder)
	s.txTrackerMu.RUnlock()

	dir := filepath.Join(s.dbPath, "wallets")
	os.MkdirAll(dir, 0755)
	savePath := filepath.Join(dir, "ui_tx_history.json")

	file, err := os.Create(savePath)
	if err != nil {
		log.Printf("[UI-STORAGE] ❌ Không thể tạo file lịch sử: %v", err)
		return
	}
	defer file.Close()

	// [GC-FRIENDLY] Ghi trực tiếp từng phần tử của mảng JSON xuống đĩa (True Streaming)
	// Tại sao: Bằng cách ghi thủ công "[" và "]" và mã hóa từng giao dịch đơn lẻ, ta tránh việc
	// Go Reflection phải tải toàn bộ mảng 100.000 phần tử vào RAM cùng lúc, giảm tối đa áp lực cho GC.
	writer := bufio.NewWriter(file)
	if _, err := writer.WriteString("[\n"); err != nil {
		log.Printf("[UI-STORAGE] ❌ Lỗi ghi JSON header: %v", err)
		return
	}

	encoder := json.NewEncoder(writer)
	first := true

	for _, id := range orderCopy {
		s.txTrackerMu.RLock()
		tx, exists := s.txTracker[id]
		var txCopy *TrackedTx
		if exists && tx != nil {
			// Sao chép nông để giải phóng RLock nhanh chóng trước khi thực hiện ghi đĩa chậm
			tmp := *tx
			txCopy = &tmp
		}
		s.txTrackerMu.RUnlock()

		if txCopy == nil {
			continue
		}

		if !first {
			if _, err := writer.WriteString(","); err != nil {
				log.Printf("[UI-STORAGE] ❌ Lỗi ghi JSON separator: %v", err)
				return
			}
		}
		first = false

		if err := encoder.Encode(txCopy); err != nil {
			log.Printf("[UI-STORAGE] ❌ Lỗi encode giao dịch: %v", err)
			return
		}
	}

	if _, err := writer.WriteString("\n]"); err != nil {
		log.Printf("[UI-STORAGE] ❌ Lỗi ghi JSON footer: %v", err)
		return
	}

	if err := writer.Flush(); err != nil {
		log.Printf("[UI-STORAGE] ❌ Lỗi flush file: %v", err)
		return
	}

	log.Printf("[UI-STORAGE] 💾 Đã sao lưu %d giao dịch của người dùng vào ui_tx_history.json (Stream Mode)", len(orderCopy))
}

func (s *RPCServer) isTxCoinbaseProto(tx *pb_block.TrackedTx) bool {
	if tx == nil || len(tx.Sender) == 0 {
		return true
	}
	for _, b := range tx.Sender {
		if b != 0 {
			return false
		}
	}
	return true
}
func (s *RPCServer) getTxStatusText(code uint32, confirmations uint64, finalized bool) string {
	// [VANGUARD-CONSENSUS] Rust Core là thực thể duy nhất quyết định trạng thái.
	if finalized && code == 1 {
		return "FINALIZED"
	}

	if confirmations > 0 {
		if code == 1 {
			return fmt.Sprintf("Đã xác nhận (%d confirmations)", confirmations)
		} else if code == 0 {
			return "LỖI: Ledger từ chối (Vui lòng kiểm tra ví/chữ ký)"
		}
	}

	switch code {
	case 1:
		return "Thành công"
	case 2:
		return "Lỗi: Chữ ký không hợp lệ (Signature Error)"
	case 3:
		return "Lỗi: Sai thứ tự Nonce (Nonce Mismatch)"
	case 5:
		return "Lỗi: Hash khối cũ không khớp (Outdated Block)"
	case 6:
		return "Lỗi: Thiếu Hash khối gần nhất (Missing Context)"
	case 7:
		return "Lỗi: Số dư không khả dụng hoặc bị khóa (Insufficient Balance)"
	case 8:
		return "Lỗi: Coinbase chưa đủ tuổi (Unripe Rewards)"
	case 0:
		if confirmations > 0 {
			return "LỖI: Ledger từ chối (Skipped)"
		}
		mSize := 0
		if s.netMgr != nil && s.netMgr.Mempool != nil {
			mSize = len(s.netMgr.Mempool.GetPendingTxList())
		}
		if mSize > 1000 {
			return "⏳ Mạng đang tắc nghẽn (Đang xếp hàng chờ khối tiếp theo)"
		}
		return "⏳ Đang chờ xác nhận (Mempool)"
	case 9:
		return "BỊ TỪ CHỐI (Mempool): Lỗi nghiệp vụ hoặc sai chữ ký"
	default:
		return "Bị từ chối (REJECTED)"
	}
}

// [VANGUARD-INTERPRETER] getTxStatusMessage: Dịch mã lỗi từ Rust Core sang Tiếng Việt
func (s *RPCServer) getTxStatusMessage(statusCode uint32) string {
	switch statusCode {
	case 1:
		return "" // Thành công - Không cần hiện lỗi
	case 2:
		return "Chữ ký không hợp lệ (Bảo mật thất bại)"
	case 3:
		return "Sai số thứ tự giao dịch (Nonce Mismatch)"
	case 4, 5:
		return "Giao dịch bị trùng lặp hoặc đã quá hạn (Replay/Outdated)"
	case 6:
		return "Thiếu mã băm xác thực mạng (Missing Recent Hash)"
	case 7:
		return "Số dư không đủ để thực hiện (Insufficient Balance)"
	case 8:
		return "Số tiền không hợp lệ"
	case 9:
		return "Giao dịch tự gửi cho chính mình bị từ chối"
	case 10:
		return "Lỗi thực thi mã lệnh (Script Failure)"
	case 99:
		return "⏳ Đang chờ xe buýt (WAITING_FOR_BUS)"
	default:
		return "" // Không có lỗi đặc biệt
	}
}

// updateTxTracker: (V2.8 IDENTITY SYNC) Quản lý Sổ theo dõi Giao dịch UI với khả năng gộp trùng lặp.
// Tại sao: Thiết kế này được tối ưu hóa để tránh gọi GetBalance gRPC khi h > 0, 
// bằng cách lấy trực tiếp số dư trước/sau từ biên lai (receipt) của khối đã xác nhận, 
// giảm tải hoàn toàn I/O đĩa và FFI bridge.
func (s *RPCServer) updateTxTracker(txHash string, sender, receiver string, amount, fee, nonce uint64, h uint64, ts int64, errMsg string) {
	sender = strings.ToLower(strings.TrimPrefix(sender, "0x"))
	receiver = strings.ToLower(strings.TrimPrefix(receiver, "0x"))

	// [VANGUARD-OPTIMIZED] BƯỚC 1: Thu thập dữ liệu từ Bridge NGOÀI Mutex Lock để tránh nghẽn
	var statusCode uint32 = 0
	var finalized bool = false
	var confirmations uint64 = 0
	var currentBal uint64 = 0

	userAddrs := s.getUserWalletAddresses()
	auditAddr := sender
	if userAddrs[receiver] && !userAddrs[sender] {
		auditAddr = receiver
	}

	if h > 0 {
		hashBytes, _ := hex.DecodeString(txHash)
		// Trích xuất trực tiếp số dư từ biên lai giao dịch trên chuỗi (h > 0)
		_, stat, fin, confs, _, senderPost, _, receiverPost := s.bridge.GetTransactionStatus(hashBytes)
		statusCode = stat
		finalized = fin
		confirmations = confs
		if auditAddr == receiver {
			currentBal = receiverPost
		} else {
			currentBal = senderPost
		}
	} else {
		// Chỉ gọi GetBalance khi giao dịch đang ở Mempool (h == 0) và thực hiện ngoài lock
		auditBytes, _ := hex.DecodeString(strings.TrimPrefix(auditAddr, "0x"))
		currentBal = s.bridge.GetBalance(nil, auditBytes, 0)
	}

	// BƯỚC 2: Cập nhật Tracker trong Mutex Lock (Nhanh, chỉ thao tác Memory)
	s.txTrackerMu.Lock()
	defer s.txTrackerMu.Unlock()

	s.updateTxTrackerWithDataUnlocked(txHash, sender, receiver, amount, fee, nonce, h, ts, errMsg, statusCode, finalized, confirmations, currentBal, userAddrs)
}

// updateTxTrackerWithCachedData: Cập nhật tracker sử dụng dữ liệu số dư và danh sách ví đã được cache sẵn.
// Tại sao: Thiết kế này loại bỏ hoàn toàn việc gọi GetBalance gRPC và getUserWalletAddresses nhiều lần trong vòng lặp
// khi xử lý hàng loạt giao dịch (Batch), giải quyết triệt để vấn đề nghẽn CPU và mạng (gRPC Storm).
func (s *RPCServer) updateTxTrackerWithCachedData(txHash string, sender, receiver string, amount, fee, nonce uint64, h uint64, ts int64, errMsg string, currentBal uint64, userAddrs map[string]bool) {
	sender = strings.ToLower(strings.TrimPrefix(sender, "0x"))
	receiver = strings.ToLower(strings.TrimPrefix(receiver, "0x"))

	var statusCode uint32 = 0
	var finalized bool = false
	var confirmations uint64 = 0

	if h > 0 {
		hashBytes, _ := hex.DecodeString(txHash)
		// Trích xuất trực tiếp số dư từ biên lai giao dịch trên chuỗi (h > 0)
		_, stat, fin, confs, _, senderPost, _, receiverPost := s.bridge.GetTransactionStatus(hashBytes)
		statusCode = stat
		finalized = fin
		confirmations = confs
		auditAddr := sender
		if userAddrs[receiver] && !userAddrs[sender] {
			auditAddr = receiver
		}
		if auditAddr == receiver {
			currentBal = receiverPost
		} else {
			currentBal = senderPost
		}
	}

	s.txTrackerMu.Lock()
	defer s.txTrackerMu.Unlock()

	s.updateTxTrackerWithDataUnlocked(txHash, sender, receiver, amount, fee, nonce, h, ts, errMsg, statusCode, finalized, confirmations, currentBal, userAddrs)
}

func (s *RPCServer) updateTxTrackerWithDataUnlocked(txHash string, sender, receiver string, amount, fee, nonce uint64, h uint64, ts int64, errMsg string, statusCode uint32, finalized bool, confirmations uint64, currentBal uint64, userAddrs map[string]bool) {

	// [V6.7 IDENTITY FIX] KHÔNG gộp trùng lặp dựa trên Nonce khi khôi phục từ Blockchain.
	// Mỗi TxID trên Ledger là một thực thể độc lập và duy nhất.
	var duplicateTxID string
	if h == 0 {
		// Chỉ gộp trùng lặp cho Mempool dựa trên Sender + Nonce (Để cập nhật TxID khi hash thay đổi)
		for id, tx := range s.txTracker {
			if tx.Sender == sender && tx.Nonce == nonce && tx.BlockHeight == 0 {
				duplicateTxID = id
				break
			}
		}
	}

	if duplicateTxID != "" {
		existing := s.txTracker[duplicateTxID]
		// [V2.8.8 FIX] Luôn ưu tiên dữ liệu từ Blockchain (h > 0)
		if h > 0 {
			existing.BlockHeight = h
			existing.Timestamp = ts

			// [TRUTH FIX V12.0 - NO MORE COUNTING]
			// Hệ thống buộc phải đọc trực tiếp từ Sổ cái Rust (SCL Core).
			existing.Status = statusCode
			existing.IsFinalized = finalized
			existing.Confirmations = confirmations

			// [V12.2] Cập nhật thông điệp lỗi trực quan
			if statusCode != 1 {
				existing.ErrorMessage = s.getTxStatusMessage(statusCode)
			} else {
				// [V12.3] PHÁT HIỆN ĐẢO NGƯỢC CHUỖI (REORG DETECTION)
				// Nếu trước đó đã có BlockHeight > 0 nhưng bây giờ Rust báo h = 0 hoặc khác biệt
				if existing.BlockHeight > 0 && h == 0 {
					existing.ErrorMessage = "⚠️ CẢNH BÁO: Giao dịch đã bị đảo ngược (Reorg) do biến động mạng lưới. Hệ thống đang tự động xử lý lại."
					log.Printf("[REORG-ALERT] 🚨 Giao dịch %s bị đẩy ra khỏi khối và quay về Mempool!", safeShortID(txHash))
				} else {
					existing.ErrorMessage = ""
				}
			}

			// [FIX V12.1] Cập nhật lại số dư đối soát khi giao dịch được chốt vào khối
			// [V1.60 AUDIT] Tự động nhận diện hướng giao dịch để đối soát số dư đúng người
			cleanReceiver := strings.ToLower(strings.TrimPrefix(existing.Receiver, "0x"))
			isReceiver := false
			if cleanReceiver != "" {
				isReceiver = userAddrs[cleanReceiver]
			}

			if statusCode == 1 {
				existing.PostBalance = currentBal
				if isReceiver {
					// Nếu là người nhận: Số dư trước đó = Hiện tại - Số tiền nhận
					if currentBal >= existing.Amount {
						existing.PrevBalance = currentBal - existing.Amount
					} else {
						existing.PrevBalance = 0
					}
				} else {
					// Nếu là người gửi: Số dư trước đó = Hiện tại + Tiền gửi + Phí
					existing.PrevBalance = currentBal + existing.Amount + existing.Fee
				}
			} else {
				existing.PrevBalance = currentBal
				existing.PostBalance = currentBal
			}

			if h > 0 && statusCode != 1 {
				log.Printf("[BLOCK-AUDIT] 🚨 CẢNH BÁO: Giao dịch %s trong khối #%d bị Rust bác bỏ (Mã lỗi: %d)!", safeShortID(txHash), h, statusCode)
			}

			if errMsg != "" {
				existing.ErrorMessage = errMsg
			}

			// Cưỡng chế cập nhật Amount/Fee nếu bản ghi cũ sai lệnh
			if amount > 0 {
				existing.Amount = amount
			}
			if fee > 0 {
				existing.Fee = fee
			}
			existing.Sender = sender
			existing.Receiver = receiver

			// Đồng bộ TxID nếu có sự thay đổi logic hash
			if duplicateTxID != txHash {
				delete(s.txTracker, duplicateTxID)
				existing.TxID = txHash
				s.txTracker[txHash] = existing
				for i, id := range s.txOrder {
					if id == duplicateTxID {
						s.txOrder[i] = txHash
						break
					}
				}
			}
		} else {
			// [V38.5] Nếu h=0 (Mempool update), cập nhật Timestamp và ErrorMessage nếu có
			if ts > 0 {
				existing.Timestamp = ts
			}
			if errMsg != "" {
				existing.ErrorMessage = errMsg
			}
			if amount > 0 {
				existing.Amount = amount
			}
			if fee > 0 {
				existing.Fee = fee
			}
		}
		// [V5.1] Tín hiệu cập nhật UI ngay cả khi chỉ gộp trùng lặp
		select {
		case s.txUpdateChan <- struct{}{}:
		default:
		}
		return
	}

	// Nếu không tìm thấy theo Sender+Nonce, kiểm tra theo TxHash (Phòng thủ đa tầng)
	if existing, exists := s.txTracker[txHash]; exists {
		if h > 0 {
			if existing.BlockHeight == 0 || existing.BlockHeight > h {
				existing.BlockHeight = h
				existing.Timestamp = ts
				existing.Status = statusCode
				// [V12.2] Cập nhật lỗi
				if statusCode != 1 {
					existing.ErrorMessage = s.getTxStatusMessage(statusCode)
				}
			}
			// [V15.3 TRUTH-OVER-CACHE] Nếu dữ liệu đến từ Blockchain (h > 0),
			// chúng ta BUỘC phải tin vào con số này và ghi đè lên giá trị Mempool (thường là 0).
			existing.Amount = amount
			existing.Fee = fee
			existing.IsFinalized = finalized
			existing.Confirmations = confirmations
		}
		if errMsg != "" {
			existing.ErrorMessage = errMsg
		}
		// [V5.1] Tín hiệu cập nhật UI ngay cả khi chỉ cập nhật trạng thái (vd: Pending -> Confirm)
		select {
		case s.txUpdateChan <- struct{}{}:
		default:
		}
		return
	}

	// Thêm giao dịch mới
	tracked := &TrackedTx{
		TxID:          txHash,
		Sender:        sender,
		Receiver:      receiver,
		Amount:        amount,
		Fee:           fee,
		Timestamp:     ts,
		BlockHeight:   h,
		Nonce:         nonce,
		Status:        statusCode,
		IsFinalized:   finalized,
		Confirmations: confirmations,
		ErrorMessage:  iifString(errMsg != "", errMsg, s.getTxStatusMessage(statusCode)),
	}

	// [VANGUARD-LOGIC FIX] Tính toán số dư Trước/Sau dựa trên trạng thái thực tế
	if h > 0 && statusCode == 1 {
		// Đã chốt vào khối thành công: Số dư hiện tại là số dư SAU KHI TRỪ
		tracked.PostBalance = currentBal
		isReceiver := userAddrs[strings.ToLower(strings.TrimPrefix(receiver, "0x"))]
		if isReceiver {
			if currentBal >= amount {
				tracked.PrevBalance = currentBal - amount
			} else {
				tracked.PrevBalance = 0
			}
		} else {
			tracked.PrevBalance = currentBal + amount + fee
		}
	} else {
		// Đang chờ hoặc thất bại: Số dư hiện tại là số dư TRƯỚC KHI TRỪ
		tracked.PrevBalance = currentBal
		if amount+fee <= currentBal {
			tracked.PostBalance = currentBal - (amount + fee)
		} else {
			tracked.PostBalance = currentBal // Phòng thủ Underflow
		}
	}

	s.txTracker[txHash] = tracked
	s.txOrder = append(s.txOrder, txHash)

	// [SAFE-LOG] Sử dụng helper an toàn để tránh panic "slice bounds out of range"
	// logSender := safeShortID(sender)
	// if sender == "" {
	// 	logSender = "COINBASE"
	// }

	// [ANTI-SPAM-LOG] Tắt log tracker add để tránh nghẽn I/O terminal Windows
	// log.Printf("[TRACKER-ADD] ✅ Đã thêm giao dịch MỚI vào Tracker: %s (Sender: %s, Nonce: %d, Height: %d, Total: %d)",
	// 	safeShortID(txHash), logSender, nonce, h, len(s.txOrder))

	// [V5.0 ELITE] Phát tín hiệu đánh thức SSE để cập nhật UI ngay lập tức
	select {
	case s.txUpdateChan <- struct{}{}:
	default:
	}

	// [VANGUARD-OPTIMIZATION] FIFO Ring Buffer O(1) siêu tốc, giải phóng hoàn toàn việc lặp RAM và quét ví đĩa cứng
	if len(s.txOrder) > maxTrackedTxs {
		toRemove := len(s.txOrder) - maxTrackedTxs
		for i := 0; i < toRemove; i++ {
			delete(s.txTracker, s.txOrder[i])
		}
		s.txOrder = s.txOrder[toRemove:]
	}

	// [V2.9.5] Ghi xuống đĩa cứng ngay lập tức bằng worker chuyên trách
	s.triggerSave()
}

// syncMempoolToTracker: Đồng bộ giao dịch từ Mempool vào Tracker.
// Tại sao: Hàm này được thiết kế để tránh hoàn toàn lock starvation và treo UI bằng cách:
// 1. Chỉ lọc và nạp các giao dịch liên quan đến ví người dùng (isUserTx) để tránh tràn bộ đệm RAM bởi spam.
// 2. Chuyển các cuộc gọi GetBalance gRPC và GetTransactionStatus ra NGOÀI Mutex Lock.
// 3. Chỉ Lock Mutex khi thực hiện các tác vụ cập nhật in-memory cực nhanh, tăng độ nhạy bén của HTTP server.
func (s *RPCServer) syncMempoolToTracker() {
	if s.netMgr == nil || s.netMgr.Mempool == nil {
		return
	}
	pendingTxs := s.netMgr.Mempool.GetPendingTxList()
	pendingMap := make(map[string]bool)
	now := time.Now().Unix()

	// Thu thập các địa chỉ ví người dùng để lọc giao dịch liên quan
	userAddrs := s.getUserWalletAddresses()

	type newPendingTx struct {
		Hash      string
		Sender    string
		Receiver  string
		Amount    uint64
		Fee       uint64
		Nonce     uint64
		Timestamp int64
		AuditAddr string
	}
	var newTxs []newPendingTx

	// BƯỚC 1: Lọc nhanh các giao dịch mempool mới trong Read Lock (Zero I/O, không chặn các API khác)
	s.txTrackerMu.RLock()
	for _, p := range pendingTxs {
		if p.Hash == "" {
			continue
		}
		pendingMap[p.Hash] = true

		cleanSender := strings.ToLower(strings.TrimPrefix(p.Sender, "0x"))
		cleanReceiver := strings.ToLower(strings.TrimPrefix(p.Receiver, "0x"))
		isUserTx := userAddrs[cleanSender] || userAddrs[cleanReceiver]

		// Chỉ theo dõi giao dịch thuộc về người dùng nhằm tránh rò rỉ bộ nhớ dưới tải stress test cao
		if !isUserTx {
			continue
		}

		if _, exists := s.txTracker[p.Hash]; !exists {
			auditAddr := cleanSender
			if userAddrs[cleanReceiver] && !userAddrs[cleanSender] {
				auditAddr = cleanReceiver
			}
			txTime := p.Timestamp
			if txTime == 0 {
				txTime = now
			}
			newTxs = append(newTxs, newPendingTx{
				Hash:      p.Hash,
				Sender:    cleanSender,
				Receiver:  cleanReceiver,
				Amount:    p.Amount,
				Fee:       p.Fee,
				Nonce:     p.Nonce,
				Timestamp: txTime,
				AuditAddr: auditAddr,
			})
		}
	}
	s.txTrackerMu.RUnlock()

	// BƯỚC 2: Thực hiện truy cập mạng gRPC GetBalance ngoài Lock để loại bỏ Lock Contention
	type newPendingTxWithBal struct {
		tx         newPendingTx
		currentBal uint64
	}
	newTxsWithBal := make([]newPendingTxWithBal, 0, len(newTxs))

	var batchAddrs [][]byte
	for _, nt := range newTxs {
		auditBytes, err := hex.DecodeString(nt.AuditAddr)
		if err == nil {
			batchAddrs = append(batchAddrs, auditBytes)
		}
	}

	if len(batchAddrs) > 0 {
		balances, err := s.bridge.GetBalanceBatch(batchAddrs)
		if err == nil {
			balMap := make(map[string]uint64)
			for _, entry := range balances {
				balMap[hex.EncodeToString(entry.Address)] = entry.Balance
			}
			for _, nt := range newTxs {
				newTxsWithBal = append(newTxsWithBal, newPendingTxWithBal{
					tx:         nt,
					currentBal: balMap[nt.AuditAddr],
				})
			}
		} else {
			// Tại sao: Khi batch gốc lỗi do hệ thống tải cao, ta sử dụng cơ chế chia nhỏ lô (chunking fallback)
			// thay vì chạy vòng lặp gRPC đơn lẻ để triệt tiêu hoàn toàn nguy cơ gây bão gRPC (gRPC Fallback Storm).
			// Nếu việc chia nhỏ lô vẫn thất bại, ta bỏ qua việc nạp số dư và gán số dư bằng 0 để bảo vệ Node.
			log.Printf("[RPC-SYNC-WARN] GetBalanceBatch failed: %v. Falling back to chunked batch queries.", err)
			balances, chunkErr := s.GetBalanceBatchChunked(batchAddrs)
			if chunkErr == nil {
				balMap := make(map[string]uint64)
				for _, entry := range balances {
					balMap[hex.EncodeToString(entry.Address)] = entry.Balance
				}
				for _, nt := range newTxs {
					newTxsWithBal = append(newTxsWithBal, newPendingTxWithBal{
						tx:         nt,
						currentBal: balMap[nt.AuditAddr],
					})
				}
			} else {
				log.Printf("[RPC-SYNC-ERROR] GetBalanceBatchChunked failed: %v. Skipping balance loading to avoid gRPC storm.", chunkErr)
				for _, nt := range newTxs {
					newTxsWithBal = append(newTxsWithBal, newPendingTxWithBal{
						tx:         nt,
						currentBal: 0,
					})
				}
			}
		}
	}

	// BƯỚC 3: Nạp các giao dịch mới vào Tracker trong Write Lock (chỉ thao tác bộ nhớ)
	if len(newTxsWithBal) > 0 {
		s.txTrackerMu.Lock()
		for _, item := range newTxsWithBal {
			nt := item.tx
			// Kiểm tra lại tính tồn tại sau khi lấy Write Lock
			if _, exists := s.txTracker[nt.Hash]; !exists {
				log.Printf("[RPC-SYNC] 📥 Phát hiện giao dịch tồn đọng trong Mempool: %s. Đang nạp vào Dashboard...", safeShortID(nt.Hash))
				s.updateTxTrackerWithDataUnlocked(
					nt.Hash, nt.Sender, nt.Receiver, nt.Amount, nt.Fee, nt.Nonce,
					0, nt.Timestamp, "", 0, false, 0, item.currentBal, userAddrs,
				)
			}
		}
		s.txTrackerMu.Unlock()
	}

	// BƯỚC 4: Thu thập các giao dịch cần đối soát trạng thái dưới RLock
	s.txTrackerMu.RLock()
	var candidates []*TrackedTx
	nowTime := time.Now().Unix()
	for _, tx := range s.txTracker {
		if tx.Status == 0 && tx.BlockHeight == 0 && !pendingMap[tx.TxID] {
			candidates = append(candidates, tx)
		}
	}
	s.txTrackerMu.RUnlock()

	// BƯỚC 5: Thực hiện đối soát trạng thái gRPC ngoài Lock
	type auditResult struct {
		tx            *TrackedTx
		height        uint64
		status        uint32
		shouldExpired bool
	}
	audited := make([]auditResult, 0, len(candidates))

	var validCandidates []*TrackedTx
	var batchHashes [][]byte

	for _, tx := range candidates {
		txAge := nowTime - tx.Timestamp
		if txAge > 180 {
			audited = append(audited, auditResult{tx: tx, shouldExpired: true})
		} else {
			hBytes, _ := hex.DecodeString(tx.TxID)
			batchHashes = append(batchHashes, hBytes)
			validCandidates = append(validCandidates, tx)
		}
	}

	if len(batchHashes) > 0 {
		statusEntries, err := s.GetTransactionStatusBatchChunked(batchHashes)
		if err == nil {
			statusMap := make(map[string]*pb_block.TxStatusEntry)
			for _, entry := range statusEntries {
				statusMap[hex.EncodeToString(entry.Hash)] = entry
			}
			for _, tx := range validCandidates {
				entry, ok := statusMap[tx.TxID]
				if ok {
					audited = append(audited, auditResult{
						tx:     tx,
						height: entry.Height,
						status: entry.Status,
					})
				}
			}
		}
	}

	// BƯỚC 6: Cập nhật kết quả đối soát trạng thái vào Tracker dưới Write Lock
	if len(audited) > 0 {
		s.txTrackerMu.Lock()
		for _, res := range audited {
			if res.shouldExpired {
				res.tx.Status = 3 // EXPIRED
				res.tx.ErrorMessage = "Sai số thứ tự giao dịch (Nonce Mismatch / Stale)"
				log.Printf("[RPC-SYNC] 🧹 Tự động dọn dẹp giao dịch stale trong tracker (giảm tải FFI): %s", safeShortID(res.tx.TxID))
			} else if res.height > 0 {
				res.tx.BlockHeight = res.height
				res.tx.Status = res.status
				log.Printf("[RPC-SYNC] 🔄 Giao dịch %s đã vào khối #%d (thoát Mempool)", safeShortID(res.tx.TxID), res.height)
			} else {
				if res.status > 0 {
					res.tx.Status = res.status
					res.tx.ErrorMessage = s.getTxStatusMessage(res.status)
				} else {
					res.tx.Status = 3
					res.tx.ErrorMessage = "Sai số thứ tự giao dịch (Nonce Mismatch / Stale)"
				}
			}
		}
		s.txTrackerMu.Unlock()
	}

	// BƯỚC 7: Đồng bộ hóa dữ liệu xuống đĩa cứng bằng cơ chế Cooldown
	s.triggerSave()
}

// trackBlockTxs: Quét giao dịch từ block mới đào/đồng bộ để cập nhật BlockHeight
func (s *RPCServer) trackBlockTxs(height uint64) {
	userAddrs := s.getUserWalletAddresses()
	blockRaw := s.bridge.GetBlock(height)
	if blockRaw == nil {
		return
	}

	var block pb_block.Block
	if err := proto.Unmarshal(blockRaw, &block); err != nil {
		return
	}
	if block.Header == nil || block.Body == nil {
		return
	}

	blockReward := s.bridge.CalculateBlockRewardBtcZ(height)
	minerAddrHex := ""
	if block.Header.MinerAddress != nil {
		minerAddrHex = hex.EncodeToString(block.Header.MinerAddress.Value)
	}

	for _, tx := range block.Body.Transactions {
		data, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
		h := s.bridge.GetCanonicalTxHash(data, height)
		txHash := hex.EncodeToString(h)

		senderHex := ""
		receiverHex := ""
		if tx.Sender != nil {
			senderHex = strings.ToLower(hex.EncodeToString(tx.Sender.Value))
		}
		if tx.Receiver != nil {
			receiverHex = strings.ToLower(hex.EncodeToString(tx.Receiver.Value))
		}
		amount := tx.Amount

		// Coinbase detection logic
		isCoinbase := s.isTxCoinbase(tx)
		if isCoinbase {
			amount = blockReward
			senderHex = ""
			if receiverHex == "" {
				receiverHex = minerAddrHex
			}
		}

		isUserTx := userAddrs[senderHex] || userAddrs[receiverHex]
		if isUserTx {
			s.updateTxTracker(txHash, senderHex, receiverHex, amount, tx.Fee, tx.Nonce, height, int64(block.Header.Timestamp), "")
		}

		// [VANGUARD-PURGE] Xóa giao dịch khỏi Mempool ngay lập tức khi đã vào khối
		if s.netMgr != nil && s.netMgr.Mempool != nil {
			s.netMgr.Mempool.RemoveTransactions([]string{txHash})
		}
	}
}

// ============================================================================
// LƯU Ý BẢO TRÌ: Cơ chế Đại Thanh Trừng (Great Purge) theo Epoch và tự động kích hoạt
// Snapshot đã hoàn thiện ổn định. Hệ thống tạm thời ngắt cơ chế này để bảo tồn lịch sử
// giao dịch đầy đủ (Full History) phục vụ mục đích kiểm toán sổ cái trong giai đoạn đầu.
// Khi blockchain phình to sau này, việc kích hoạt lại sẽ giúp dọn dẹp dung lượng đĩa cứng.
// Cách kích hoạt lại: Thay đổi node_p2p.EnableGreatPurge trong constants.go.
// ============================================================================

// [VANGUARD-PURGE-V2] ĐẠI THANH TRỪNG 48H (Epoch-based)
// Mục tiêu: Giải phóng "Nhà tù dữ liệu" quá khứ theo từng Kỷ nguyên (1.440 khối).
func (s *RPCServer) StartGreatPurge(currentHeight uint64) {
	// 🛡️ [MASTER SWITCH] Vô hiệu hóa Đại Thanh Trừng trong giai đoạn mạng lưới còn nhẹ.
	if !node_p2p.EnableGreatPurge {
		return
	}

	const EpochSize = 1152
	const GracePeriod = 2 * EpochSize // 48 giờ bảo tồn (2304 khối)

	// Kiểm tra nếu chúng ta vừa hoàn thành một Epoch mới và vượt ngưỡng bảo tồn
	if currentHeight >= GracePeriod && currentHeight%EpochSize == 0 {
		epochToPurge := (currentHeight / EpochSize) - 2
		startBlock := (epochToPurge * EpochSize) + 1
		endBlock := (epochToPurge + 1) * EpochSize

		if epochToPurge >= 0 && startBlock > 0 {
			log.Printf("💀 [GREAT-PURGE-48H] Kích hoạt máy chém Kỷ nguyên #%d: Khối #%d -> #%d", epochToPurge+1, startBlock, endBlock)
			success, err := s.bridge.PurgeHistoricalData(startBlock, uint64(endBlock))
			if err != nil || !success {
				log.Printf("❌ [GREAT-PURGE] Thất bại tại Epoch #%d: %v", epochToPurge+1, err)
			} else {
				log.Printf("✅ [GREAT-PURGE] Kỷ nguyên #%d đã bị xóa sạch khỏi lịch sử mạng lưới!", epochToPurge+1)
			}
		}
	}
}

// checkGlobalInternet: Kiểm tra xem node có kết nối được với Internet toàn cầu hay không bằng cách gọi HTTP GET tới các dịch vụ DNS IP
func (s *RPCServer) checkGlobalInternet() bool {
	dnsServers := []string{
		"1.1.1.1:53",
		"8.8.8.8:53",
		"9.9.9.9:53",
		"208.67.222.222:53",
	}
	for _, addr := range dnsServers {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return true // Kết nối thành công tới ít nhất 1 trung tâm mạng độc lập
		}
	}
	return false
}

func (s *RPCServer) Start() {
	r := mux.NewRouter()

	// [V1.1.2] Tự động mở trình duyệt sau khi Server lên đèn
	go func() {
		time.Sleep(300 * time.Millisecond)
		url := fmt.Sprintf("http://localhost:%d", s.port)
		s.openBrowser(url)
	}()

	// [VANGUARD-INTERNET-CHECK] Khởi chạy luồng nền định kỳ 10 giây kiểm tra kết nối internet toàn cầu thực tế
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		isInternetDown := false
		for range ticker.C {
			hasInternet := s.checkGlobalInternet()
			if !hasInternet {
				atomic.StoreInt32(&s.internetOffline, 1)
				if !isInternetDown {
					isInternetDown = true
					log.Printf("[INTERNET-WARN] ⚠️ Phát hiện mất kết nối Internet toàn cầu thực tế (Lỗi nhà mạng!).")
					if s.cliApp.GetNodeMode() == "full-mining" {
						log.Printf("[INTERNET-WARN] ⏸️ Tự động tạm dừng khai thác Blake3-PoW và đóng các tiến trình máy đào để bảo vệ chuỗi.")
						s.bridge.SetMiningPause(true)
						s.StopConfiguredMiners()
						
						// Xóa template khối cũ để ngăn chặn việc đào tiếp tục trên getwork cũ
						s.cliApp.activeMiningMu.Lock()
						s.cliApp.activeBlock = nil
						s.cliApp.activeMiningMu.Unlock()
						
						s.internetAutoPaused = true
					}
				}
			} else {
				atomic.StoreInt32(&s.internetOffline, 0)
				if isInternetDown {
					isInternetDown = false
					log.Printf("[INTERNET-OK] 💚 Đã khôi phục kết nối Internet toàn cầu thực tế.")
					if s.cliApp.GetNodeMode() == "full-mining" && s.internetAutoPaused {
						log.Printf("[INTERNET-OK] ▶️ Tự động khôi phục khai thác Blake3-PoW.")
						s.bridge.SetMiningPause(false)
						
						// Tạo lại template khối mới từ mempool và phát sóng
						s.cliApp.RefreshMiningTask()
						
						// Khởi động lại các tiến trình đào GPU/CPU
						s.StartConfiguredMiners()
						
						s.internetAutoPaused = false
					}
				}
			}
		}
	}()
	// Tại sao: Thay vì để mỗi client SSE tự quét lịch sử từ 0 (gây Freeze),
	// hệ thống giờ đây chỉ dùng duy nhất 1 luồng nền để cập nhật txTracker.
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			highest := s.bridge.GetCurrentVersion()
			if highest > s.lastTrackedHeight {
				// [V5.5] Khởi động bộ giám sát Hashrate ngầm một lần duy nhất
				if s.lastHashTime.IsZero() {
					s.lastHashTime = time.Now()
					go s.startHashrateMonitor()
				}
				// [CACHE-INVALIDATION] Chủ động xóa bỏ bộ đệm khối gần nhất khi phát hiện có khối mới
				// Tại sao làm vậy: Tránh việc giao diện Dashboard bị treo đỉnh ở phiên bản cũ khi gọi /recent/blocks.
				s.recentBlocksCacheMu.Lock()
				s.recentBlocksCache = nil
				s.recentBlocksCacheMu.Unlock()

				diff := highest - s.lastTrackedHeight
				log.Printf("[RPC-SYNC] 🛰️ Phát hiện %d khối mới. Đang đồng bộ hóa Dashboard...", diff)
				for h := s.lastTrackedHeight + 1; h <= highest; h++ {
					s.trackBlockTxs(h)
					s.lastTrackedHeight = h
				}
				s.triggerSave()
			}
		}
	}()

	// WEB UI STORAGE
	r.HandleFunc("/", s.handleHome).Methods("GET")

	// WALLET API (V2.0)
	r.HandleFunc("/api/v1/wallet/create", s.localhostOnly(s.handleWalletCreate)).Methods("POST")
	r.HandleFunc("/api/v1/wallet/restore", s.localhostOnly(s.handleWalletRestore)).Methods("POST")
	r.HandleFunc("/api/v1/wallet/preview", s.localhostOnly(s.handleWalletPreview)).Methods("POST")
	r.HandleFunc("/api/v1/wallet/list", s.localhostOnly(s.handleWalletList)).Methods("GET")
	r.HandleFunc("/api/v1/wallet/delete", s.localhostOnly(s.handleWalletDelete)).Methods("POST")
	r.HandleFunc("/api/v1/fees/calculate", s.walletGate(s.handleFeeCalculate)).Methods("GET")

	// STATIC ASSETS (V2.1 - REACT DIST)
	// [V2.2 TACTICAL FIX] Phục vụ file tĩnh linh hoạt (Hỗ trợ chạy từ bin/ hoặc root)
	localAssets := "6_user_interface/web_ui/dist/assets"
	if _, err := os.Stat(localAssets); err != nil {
		// Thử tìm tương đối với EXE (Trường hợp chạy từ bin/)
		if exePath, err := os.Executable(); err == nil {
			altPath := filepath.Join(filepath.Dir(filepath.Dir(exePath)), localAssets)
			if _, err := os.Stat(altPath); err == nil {
				localAssets = altPath
			}
		}
	}

	if _, err := os.Stat(localAssets); err == nil {
		r.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir(localAssets))))
	} else {
		distFS, _ := fs.Sub(staticFiles, "web_ui/dist")
		r.PathPrefix("/assets/").Handler(http.FileServer(http.FS(distFS)))
	}

	// CORE RPC API
	r.HandleFunc("/api/v1/send_tx", s.localhostOnly(s.handleSendTx)).Methods("POST")
	r.HandleFunc("/api/v1/send_raw_tx", s.walletGate(s.handleSendRawTx)).Methods("POST")
	r.HandleFunc("/api/v1/send_batch_tx", s.localhostOnly(s.handleSendBatchTx)).Methods("POST")
	r.HandleFunc("/api/v1/block/{height}", s.handleGetBlock).Methods("GET")
	r.HandleFunc("/api/v1/balance/{address}", s.walletGate(s.handleGetBalance)).Methods("GET")
	r.HandleFunc("/api/v1/status", s.handleStatus).Methods("GET")
	r.HandleFunc("/api/v1/search/{query}", s.handleSearch).Methods("GET")
	r.HandleFunc("/api/v1/recent/blocks", s.handleRecentBlocks).Methods("GET")
	r.HandleFunc("/api/v1/recent/txs", s.handleRecentTransactions).Methods("GET")
	r.HandleFunc("/api/v1/address/{address}/history", s.handleAddressHistory).Methods("GET")
	r.HandleFunc("/api/v1/supply", s.handleSupply).Methods("GET")
	r.HandleFunc("/api/v1/miner/status", s.handleMinerStatus).Methods("GET")
	r.HandleFunc("/api/v1/miner/getwork", s.handleMinerGetWork).Methods("GET")
	r.HandleFunc("/api/v1/miner/submitwork", s.handleMinerSubmitWork).Methods("POST")
	// [SECURITY-HARDENING] Các API điều khiển Node ĐÒI HỎI truy cập từ localhost
	// Tại sao: Ngăn chặn kẻ tấn công từ Internet điều khiển Node (bật/tắt đào, đổi ví, đổi mode)
	r.HandleFunc("/api/v1/miner/toggle", s.localhostOnly(func(w http.ResponseWriter, r *http.Request) {
		// [VANGUARD-GUARD] Chốt chặn an toàn: Đợi 60 giây Radar Scan để bảo vệ mạng lưới khỏi Fork
		gracePeriod := 60 * time.Second
		if time.Since(s.launchTime) < gracePeriod {
			http.Error(w, fmt.Sprintf("Node warming up (Radar Scan), please wait %d s", int((gracePeriod-time.Since(s.launchTime)).Seconds())), http.StatusServiceUnavailable)
			return
		}
		s.handleMinerToggle(w, r)
	})).Methods("POST")
	r.HandleFunc("/api/v1/miner/set-address", s.localhostOnly(s.handleSetMinerAddress)).Methods("POST")
	r.HandleFunc("/api/v1/tx/{txid}", s.walletGate(s.handleGetTxDetail)).Methods("GET")
	r.HandleFunc("/api/v1/node/mode", s.localhostOnly(s.handleNodeMode)).Methods("POST", "GET")
	r.HandleFunc("/api/v1/node/cpu", s.localhostOnly(s.handleCpuIntensity)).Methods("POST", "GET")
	r.HandleFunc("/api/v1/miner/hashrate", s.handleMinerHashrate).Methods("POST")
	r.HandleFunc("/api/v1/node/mining-device", s.localhostOnly(s.handleMiningDevice)).Methods("POST", "GET")

	// [V12.2] REAL-TIME SSE: Stream trạng thái mạng (C#9 TACTICAL)
	r.HandleFunc("/api/v1/network/watch-status", s.handleWatchStatus).Methods("GET")

	// STATIC PEERS & ISOLATION MODE MANAGEMENT
	r.HandleFunc("/api/v1/network/static-peers", s.localhostOnly(s.handleGetStaticPeers)).Methods("GET")
	r.HandleFunc("/api/v1/network/static-peers", s.localhostOnly(s.handleUpdateStaticPeers)).Methods("POST")
	r.HandleFunc("/api/v1/network/isolation-mode", s.localhostOnly(s.handleSetIsolationMode)).Methods("POST")

	// [SECURITY-HARDENING] Endpoint debug/verify_balances chỉ cho phép từ localhost
	// Tại sao: Tránh lộ thông tin kiểm toán nội bộ ra Internet
	r.HandleFunc("/api/v1/node/purge", s.localhostOnly(s.handlePurgeData)).Methods("POST")
	r.HandleFunc("/api/v1/node/install-env", s.localhostOnly(s.handleInstallEnvironment)).Methods("POST")
	r.HandleFunc("/api/v1/snapshot/import", s.localhostOnly(s.handleManualSnapshotImport)).Methods("POST")
	r.HandleFunc("/api/v1/snapshot/export", s.localhostOnly(s.handleManualSnapshotExport)).Methods("POST")
	r.HandleFunc("/api/v1/mempool/purge", s.localhostOnly(s.handleMempoolPurge)).Methods("POST")
	// [SOCIAL-CONSENSUS] Bàn tay vô hình: Xóa N khối gần nhất + Gỡ ban toàn mạng
	// Tại sao: Cho phép nhà vận hành Node can thiệp khi xảy ra chia cắt mạng (Network Partition)
	r.HandleFunc("/api/v1/node/emergency-reset", s.localhostOnly(s.handleEmergencyReset)).Methods("POST")
	r.HandleFunc("/api/v1/node/shutdown", s.localhostOnly(s.handleNodeShutdown)).Methods("POST")
	r.HandleFunc("/api/v1/debug/verify_balances", s.localhostOnly(s.handleDebugVerifyBalances)).Methods("GET")

	// [V11.2] Middleware Kiểm toán & Kết nối
	r.Use(s.recoveryMiddleware) // [V11.6] Bảo vệ máy chủ khỏi sập (Panic)
	r.Use(s.loggingMiddleware)
	r.Use(s.corsMiddleware)

	// Start HTTP Server
	// [SECURITY-HARDENING] Chỉ lắng nghe localhost (127.0.0.1) để tắt giao diện Web UI công khai và bảo vệ hệ thống.
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("[DEBUG-RPC] 📡 Chuẩn bị khởi động HTTP Server tại cổng: %s", addr)
	go func() {
		log.Printf("[RPC] 🌐 Web UI & API Server đang lắng nghe tải: http://%s", addr)
		if err := http.ListenAndServe(addr, r); err != nil {
			log.Printf("[DEBUG-RPC] ❌ Lỗi lắng nghe HTTP: %v", err)
			go_bridge.FatalExit("[RPC] Lỗi khởi thông HTTP Server: %v.\nGợi ý: Cổng HTTP %d đã bị chiếm dụng bởi chương trình khác.", err, s.port)
		}
	}()

	// Start GRPC Server
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", s.port+10000)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Printf("[CRITICAL] ❌ gRPC Listener thất bại tại %s: %v", grpcAddr, err)
		return
	}
	// [V1.5.0 BIG-BLOCK] Nâng cấp gRPC Message Limit lên 45MB cho chuẩn 35MB
	grpcSrv := grpc.NewServer(
		grpc.MaxRecvMsgSize(45*1024*1024),
		grpc.MaxSendMsgSize(45*1024*1024),
	)
	// [V1.2.1] KÍCH HOẠT LÕI gRPC: Dashboard Elite chính thức hòa mạng
	pb_block.RegisterBlockchainServiceServer(grpcSrv, s)
	pb_block.RegisterMinerGatewayServer(grpcSrv, s)
	reflection.Register(grpcSrv)
	log.Printf("[RPC-GRPC] 🚀 Server V1.0 Minimalist đang lắng nghe tại %s", grpcAddr)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			log.Printf("[CRITICAL] ❌ gRPC Serve thất bại: %v", err)
		}
	}()

	// Tự động khởi chạy thợ đào phù hợp cấu hình (CPU/GPU/Hybrid)
	s.StartConfiguredMiners()
}

// --------------------------------------------------------------------------
// gRPC BLOCKCHAIN SERVICE IMPLEMENTATION (Elite V1.2)
// --------------------------------------------------------------------------

func (s *RPCServer) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	height, _ := strconv.ParseUint(vars["height"], 10, 64)

	blockRaw := s.bridge.GetBlock(height)
	if blockRaw == nil {
		http.Error(w, "Block not found", http.StatusNotFound)
		return
	}

	var block pb_block.Block
	if err := proto.Unmarshal(blockRaw, &block); err != nil {
		http.Error(w, "Block decode error", http.StatusInternalServerError)
		return
	}
	if block.Header == nil {
		http.Error(w, "Block header missing", http.StatusInternalServerError)
		return
	}

	headerBuf, _ := proto.Marshal(block.Header)
	blockHash := s.bridge.GetCanonicalBlockHeaderHash(headerBuf, block.Header.Height)
	if len(blockHash) == 0 {
		blockHash = make([]byte, 32)
	}

	var txList []map[string]interface{}
	if block.Body != nil && len(block.Body.Transactions) > 0 {
		txHashes := make([][]byte, len(block.Body.Transactions))
		txHashStrs := make([]string, len(block.Body.Transactions))
		for i, tx := range block.Body.Transactions {
			// Băm TxID bằng Go Native (Chớp nhoáng, không gọi Rust qua gRPC)
			h := node_p2p.GetSigningHashNative(tx)
			txHashes[i] = h
			txHashStrs[i] = hex.EncodeToString(h)
		}

		// Gọi gRPC theo lô (Batch)
		statusEntries, err := s.GetTransactionStatusBatchChunked(txHashes)
		statusMap := make(map[string]*pb_block.TxStatusEntry)
		if err == nil {
			for _, entry := range statusEntries {
				if entry != nil && len(entry.Hash) > 0 {
					statusMap[hex.EncodeToString(entry.Hash)] = entry
				}
			}
		}

		for i, tx := range block.Body.Transactions {
			txHash := txHashStrs[i]
			var statCode uint32 = 0
			var finalized bool = false
			var confs uint64 = 0

			if entry, ok := statusMap[txHash]; ok && entry != nil {
				statCode = entry.Status
				finalized = entry.IsFinalized
				confs = entry.Confirmations
			}

			status := s.getTxStatusText(statCode, confs, finalized)

			// [V15.2 ELITE FIX] Tính toán phần thưởng thực tế cho Coinbase thay vì hiển thị 0 Z
			amount := tx.Amount
			isCoinbase := tx.Sender == nil || len(tx.Sender.GetValue()) == 0
			if !isCoinbase {
				allZero := true
				for _, b := range tx.Sender.GetValue() {
					if b != 0 {
						allZero = false
						break
					}
				}
				isCoinbase = allZero
			}
			if isCoinbase {
				amount = s.bridge.CalculateBlockRewardBtcZ(height)
			}

			txList = append(txList, map[string]interface{}{
				"id":            txHash,
				"sender":        hex.EncodeToString(tx.Sender.GetValue()),
				"receiver":      hex.EncodeToString(tx.Receiver.GetValue()),
				"amount":        amount,
				"fee":           tx.Fee,
				"nonce":         tx.Nonce,
				"confirmations": confs,
				"status":        status,
				"status_code":   statCode,
			})
		}
	}

	// ZK-Proof status (Deprecated in V1.0 Minimalist)
	zkStatus := "Chưa có ZK-Proof"

	// Parent hash
	parentHash := "0000000000000000"
	if height > 0 {
		parentH := s.bridge.GetBlockHash(height - 1)
		if parentH != nil {
			parentHash = hex.EncodeToString(parentH)
		}
	}

	resp := map[string]interface{}{
		"height":          height,
		"hash":            hex.EncodeToString(blockHash),
		"parent_hash":     parentHash,
		"timestamp":       block.Header.Timestamp,
		"nonce":           block.Header.Nonce,
		"difficulty":      hex.EncodeToString(block.Header.Difficulty),
		"state_root":      hex.EncodeToString(block.Header.StateRoot.GetValue()),
		"tx_root":         hex.EncodeToString(block.Header.TxRoot.GetValue()),
		"miner":           hex.EncodeToString(block.Header.MinerAddress.GetValue()),
		"zk_proof_status": zkStatus,
		"tx_count":        len(txList),
		"transactions":    txList,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *RPCServer) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	addrStr := vars["address"]
	addr, err := hex.DecodeString(strings.TrimPrefix(addrStr, "0x"))
	if err != nil || len(addr) != 32 {
		http.Error(w, "Invalid address format", http.StatusBadRequest)
		return
	}

	// [V1.2.7 RECOVERY] Lấy cả Số dư Tổng và Số dư có thể chi tiêu
	total := s.bridge.GetBalance(nil, addr, uint32(s.bridge.GetCurrentVersion()))
	spendable := s.bridge.GetSpendableBalance(addr)
	nonce := s.bridge.GetNonce(nil, addr)

	pending := uint64(0)
	if total > spendable {
		pending = total - spendable
	}

	// [FINALITY-UI V2] Trả thêm coin_id và nonce cho frontend hiển thị Coin ID trên giao diện ví
	// [VANGUARD-RECONCILIATION] Lấy nonce dự phóng chính xác từ Mempool
	// Tại sao thiết kế như vậy: Theo yêu cầu nâng cấp của Bạn, API sẽ trả về expected_nonce
	// để các bộ bơm stress test tự động đồng bộ chuẩn xác khi gặp lỗi/mất mạng tạm thời.
	cleanAddrStr := hex.EncodeToString(addr)
	expectedNonce := s.netMgr.Mempool.GetExpectedNonce(cleanAddrStr, nonce)

	resp := map[string]interface{}{
		"address":        addrStr,
		"nonce":          nonce,
		"expected_nonce": expectedNonce, // Trường expected_nonce hỗ trợ đồng bộ stress test hoàn hảo
		"coin_id":        "BTC_Z",
		"coin_index":     0,
		"balances": map[string]uint64{
			"btc_z":     total,     // Tổng số dư thực tế trên Ledger
			"spendable": spendable, // Số dư đã qua 6 khối Maturity
			"pending":   pending,   // Số dư đang đợi trưởng thành
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *RPCServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	height := s.bridge.GetCurrentVersion()
	fH := s.netMgr.SyncEngine.GetFinalizedHeight()

	curr, target, state := s.netMgr.SyncEngine.GetSyncProgress()

	peers := s.netMgr.Host.Network().Peers()
	peerInfos := make([]map[string]interface{}, 0, len(peers))
	s.netMgr.PeerMutex.RLock()
	for _, p := range peers {
		rtt := s.netMgr.PeerRTT[p]
		peerInfos = append(peerInfos, map[string]interface{}{
			"id":         p.String(),
			"latency_ms": rtt.Milliseconds(),
		})
	}
	s.netMgr.PeerMutex.RUnlock()

	s.nodeModeMu.RLock()
	mode := s.nodeMode
	s.nodeModeMu.RUnlock()

	miningWarning := ""
	if mode == "full-mining" && (s.minerAddr == nil || s.cliApp.IsZeroAddress(s.minerAddr)) {
		wallets, _ := s.walletMgr.ListWallets()
		if len(wallets) > 0 {
			miningWarning = "Vui lòng chọn ví nhận thưởng để bắt đầu khai thác"
		} else {
			miningWarning = "Yêu cầu Khôi phục ví (12 từ khóa) để xử lý hệ thống"
		}
	}

	resp := map[string]interface{}{
		"highest_height": height,
		"current_height": height,
		"p2p_address":    s.netMgr.GetAddress(),
		"finalized":      fH,
		"network":        "YonaCode Go Minimalist (V1.0)",
		"consensus":      "Vanguard (Roll-to-Finality)",
		"sync": func() map[string]interface{} {
			downloading := uint64(0)
			if s.netMgr.SyncEngine != nil {
				downloading = s.netMgr.SyncEngine.GetDownloadingHeight()
			}
			m := map[string]interface{}{
				"current":     curr,
				"target":      target,
				"state":       state,
				"executing":   s.bridge.IsSyncing(),
				"downloading": downloading,
			}
			if s.netMgr.SyncEngine != nil {
				loaded, total := s.netMgr.SyncEngine.GetSnapshotProgress()
				m["snapshot_chunks_loaded"] = loaded
				m["snapshot_chunks_total"] = total
			}
			return m
		}(),
		"peers": map[string]interface{}{
			"count": len(peers),
			"list":  peerInfos,
		},
		"is_mining":              s.isMiningActive(),
		"node_mode":              mode,
		"cpu_intensity":          s.GetCpuIntensity(),
		"mining_device":          s.miningDevice,
		"grace_period_remaining": s.getGracePeriodRemaining(),
		"mining_warning":         miningWarning,
		"bandwidth": func() map[string]interface{} {
			sent := atomic.LoadUint64(&s.netMgr.BytesSent)
			recv := atomic.LoadUint64(&s.netMgr.BytesRecv)
			if s.netMgr.Bwc != nil {
				stats := s.netMgr.Bwc.GetBandwidthTotals()
				sent = uint64(stats.TotalOut)
				recv = uint64(stats.TotalIn)
			}
			return map[string]interface{}{
				"sent": sent,
				"recv": recv,
			}
		}(),
		"difficulty":       getDifficulty(s.bridge),
		"avg_block_time":   s.calculateAvgBlockTime(),
		"block_reward":     float64(s.bridge.CalculateBlockRewardBtcZ(height)) / 1e8,
		"total_supply":     float64(s.bridge.GetActualTotalSupply()) / 1e8,
		"hashrate":         atomic.LoadUint64(&s.currentHashrate),
		"network_hashrate": s.calculateNetworkHashrate(),
		"network_hashrate_history": s.getNetworkHashrateHistory(),
		"top_miners":       s.getTopMiners(),
		"pending_tx_count": len(s.netMgr.Mempool.GetPendingTxList()),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// calculateAvgBlockTime: (Audit V2.9) Tính toán thời gian tạo khối trung bình thực tế
func (s *RPCServer) calculateAvgBlockTime() float64 {
	highest := s.bridge.GetCurrentVersion()
	if highest < 2 {
		return 75.0 // Mặc định nếu chuỗi quá ngắn
	}

	// Lấy 11 block gần nhất để tính 10 khoảng thời gian
	count := uint64(10)
	if highest < count {
		count = highest
	}

	var totalTime uint64
	lastTs := uint64(0)

	// Lấy timestamp khối cao nhất
	blockRaw := s.bridge.GetBlock(highest)
	if blockRaw != nil {
		var block pb_block.Block
		proto.Unmarshal(blockRaw, &block)
		if block.Header != nil {
			lastTs = block.Header.Timestamp
		}
	}

	if lastTs == 0 {
		return 75.0
	}

	// Lấy timestamp khối (highest - count)
	blockOldRaw := s.bridge.GetBlock(highest - count)
	if blockOldRaw != nil {
		var blockOld pb_block.Block
		proto.Unmarshal(blockOldRaw, &blockOld)
		if blockOld.Header != nil && blockOld.Header.Timestamp > 0 && lastTs > blockOld.Header.Timestamp {
			totalTime = lastTs - blockOld.Header.Timestamp
			return float64(totalTime) / float64(count)
		}
	}

	return 75.0
}

// calculateNetworkHashrate: Ước tính hashrate toàn mạng dựa trên Độ khó và Thời gian khối trung bình
func (s *RPCServer) calculateNetworkHashrate() uint64 {
	highestHeight := s.bridge.GetCurrentVersion()
	hash := s.bridge.GetBlockHash(highestHeight)
	difficulty := big.NewInt(1200000000) // Khởi đầu mặc định 1.2 tỷ VNT

	if len(hash) > 0 {
		headerRaw := s.bridge.GetHeaderRaw(hash)
		if headerRaw != nil {
			var header pb_block.BlockHeader
			if err := proto.Unmarshal(headerRaw, &header); err == nil {
				difficulty = go_bridge.BytesLEToBigInt(header.Difficulty)
			}
		}
	}

	avgBlockTime := s.calculateAvgBlockTime()
	if avgBlockTime <= 0 {
		avgBlockTime = 75.0
	}

	// Hashrate = Difficulty / AvgBlockTime
	hashrate := new(big.Int).Div(difficulty, big.NewInt(int64(avgBlockTime)))
	return hashrate.Uint64()
}


func getDifficulty(bridge *go_bridge.Bridge) string {
	// 1. Thử lấy Header của khối cao nhất hiện tại (sử dụng mã băm thực tế thay vì nil)
	highestHeight := bridge.GetCurrentVersion()
	hash := bridge.GetBlockHash(highestHeight)
	if len(hash) > 0 {
		headerRaw := bridge.GetHeaderRaw(hash)
		if headerRaw != nil {
			var header pb_block.BlockHeader
			if err := proto.Unmarshal(headerRaw, &header); err == nil {
				// Trích xuất độ khó thực tế từ Header khối và chuyển đổi sang BigInt dạng chuỗi
				return go_bridge.BytesLEToBigInt(header.Difficulty).String()
			}
		}
	}

	// 2. [VANGUARD-REALISM] Nếu chưa có khối (Khối 0) hoặc lấy thất bại, hỏi Rust Core về độ khó khởi thủy (MIN_DIFFICULTY)
	// Việc này đảm bảo Dashboard luôn khớp 100% với logic trong difficulty_logic.rs
	diffBytes := bridge.CalculateNextDifficultyV2(nil, nil, uint64(time.Now().Unix()), 0)
	if len(diffBytes) > 0 {
		return go_bridge.BytesLEToBigInt(diffBytes).String()
	}

	// 3. Fallback cuối cùng nếu mọi thứ đều thất bại
	return "0"
}

// [V4.0 ANTI-SPAM] Kiểm tra Rate Limit theo thuật toán Token Bucket
func (s *RPCServer) allowTransactions(sender string, count int) bool {
	s.rateLimitersMu.Lock()
	defer s.rateLimitersMu.Unlock()

	bucket, exists := s.rateLimiters[sender]
	if !exists {
		// Khởi tạo: 50.000 token, tối đa 50.000, nạp 10.000/s (Đáp ứng stress test lớn)
		bucket = &tokenBucket{tokens: 50000, lastUpdate: time.Now()}
		s.rateLimiters[sender] = bucket
	}

	now := time.Now()
	elapsed := now.Sub(bucket.lastUpdate).Seconds()

	// Nạp lại 10.000 token mỗi giây
	bucket.tokens += elapsed * 10000
	if bucket.tokens > 50000 {
		bucket.tokens = 50000
	}
	bucket.lastUpdate = now

	if bucket.tokens >= float64(count) {
		bucket.tokens -= float64(count)
		return true
	}
	return false
}

func (s *RPCServer) handleSendTx(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	log.Printf("[RPC-DEBUG] 🏁 Bắt đầu handleSendTx từ %s", r.RemoteAddr)
	var req struct {
		Sender   string          `json:"sender"`
		Receiver string          `json:"receiver"`
		Amount   json.RawMessage `json:"amount"`
		Password string          `json:"password"`
		BaseFee  uint64          `json:"base_fee"` // [V1.3 - PHỤ LỤC H]
		Nonce    *uint64         `json:"nonce"`    // Hỗ trợ truyền nonce thủ công cho kiểm thử và ví ngoài
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": err.Error()})
		return
	}
	defer r.Body.Close()

	senderHex := strings.TrimPrefix(req.Sender, "0x")

	receiverHex := strings.TrimPrefix(req.Receiver, "0x")

	// [LỚP 0] Rate Limiting Check
	if !s.allowTransactions(senderHex, 1) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Rate limit exceeded. Too many transactions from this address."})
		return
	}
	log.Printf("[RPC-DEBUG] ✅ Rate Limit OK (%v)", time.Since(start))

	senderBytes, err := hex.DecodeString(senderHex)
	if err != nil || len(senderBytes) != 32 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ người gửi không hợp lệ"})
		return
	}
	receiverBytes, err := hex.DecodeString(receiverHex)
	if err != nil || len(receiverBytes) != 32 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ người nhận không hợp lệ"})
		return
	}

	// 1. Lấy Private Key của ví gửi
	_, err = s.walletMgr.LoadWallet(senderHex)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "ERR_WATCH_ONLY"})
		return
	}

	// [V7.0 PERFORMANCE] Thử lấy seed từ cache để tránh Argon2id (Stress test bottleneck)
	s.seedCacheMu.Lock()
	cacheKey := senderHex + ":" + req.Password
	seed, cached := s.seedCache[cacheKey]
	s.seedCacheMu.Unlock()

	if !cached {
		var err error
		seed, err = s.walletMgr.GetSeed(senderHex, req.Password)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Sai mật khẩu bảo vệ ví!"})
			return
		}
		// Cache lại seed
		s.seedCacheMu.Lock()
		s.seedCache[cacheKey] = seed
		s.seedCacheMu.Unlock()
		log.Printf("[RPC-DEBUG] 🔐 Seed Cache MISS (Argon2id calculated) - %v", time.Since(start))
	} else {
		log.Printf("[RPC-DEBUG] 🚀 Seed Cache HIT (Instant decryption) - %v", time.Since(start))
	}

	// 2. [V1.3 - PHỤ LỤC H] Tính toán Phí đa tầng và Anti-Spam
	amountStr := strings.Trim(string(req.Amount), "\"")
	amountVNT, err := s.AmountToVNT(amountStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Định dạng số tiền không hợp lệ!"})
		return
	}

	baseFee := req.BaseFee
	if baseFee == 0 {
		baseFee = 250 // Mặc định Standard
	}

	// [LỚP 1] Validate Phí 3 Tầng cố định
	if !s.bridge.IsValidFee(baseFee) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Mức phí không hợp lệ. Phải là 250, 500 hoặc 1000."})
		return
	}

	// [LỚP 2] Chặn Spam Nano-dust: Nếu < 10 VNT, buộc dùng ít nhất PRIORITY (500)
	if amountVNT < 10 && baseFee < 500 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Giao dịch Nano-dust (<10 VNT) yêu cầu phí tối thiểu PRIORITY (500)."})
		return
	}

	// [V7.3 PERFORMANCE] Balance Cache: Giảm tải FFI Bridge
	s.balanceCacheMu.Lock()
	spendable, exists := s.balanceCache[senderHex]
	bTime := s.balanceCacheTime[senderHex]
	s.balanceCacheMu.Unlock()

	if !exists || time.Since(bTime) > 1*time.Second {
		spendable = s.bridge.GetSpendableBalance(senderBytes)
		s.balanceCacheMu.Lock()
		s.balanceCache[senderHex] = spendable
		s.balanceCacheTime[senderHex] = time.Now()
		s.balanceCacheMu.Unlock()
	}
	feeVNT := s.bridge.CalculateNanoFee(amountVNT, uint32(baseFee))

	// Tính tổng số tiền đang "treo" trong Mempool của địa chỉ này (O(1) Optimized)
	mempoolPending := s.netMgr.Mempool.GetPendingSpend(senderHex)

	totalNeeded := amountVNT + feeVNT
	if spendable < (mempoolPending + totalNeeded) {
		log.Printf("[RPC-AUDIT] ❌ Giao dịch bị từ chối: Tổng chi tiêu (Mempool: %d + New: %d) vượt quá số dư Spendable: %d", mempoolPending, totalNeeded, spendable)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "Error",
			"message": fmt.Sprintf("Số dư không đủ để bao phủ các giao dịch đang chờ. Cần thêm: %.8f GO", float64(mempoolPending+totalNeeded-spendable)/1e8),
		})
		return
	}
	// [VANGUARD-FORENSIC] KHỞI CHẠY QUY TRÌNH KIỂM TOÁN CHUYÊN SÂU (DEEP AUDIT)
	auditLogs := []string{
		"🛡️ [PHASE 1] Khám phá: Đã nhận dạng địa chỉ và xác thực số dư khả dụng.",
	}

	// [V7.4 PERFORMANCE] Blockchain Status Cache: Giảm tải FFI Bridge
	s.blockCacheMu.Lock()
	highestHeight := s.heightCache
	recentHash := s.hashCache
	lastBTime := s.blockCacheTime
	s.blockCacheMu.Unlock()

	if time.Since(lastBTime) > 1*time.Second {
		highestHeight = s.bridge.GetCurrentVersion()
		recentHash = s.bridge.GetBlockHash(highestHeight)
		if recentHash == nil {
			recentHash = make([]byte, 32)
		}

		s.blockCacheMu.Lock()
		s.heightCache = highestHeight
		s.hashCache = recentHash
		s.blockCacheTime = time.Now()
		s.blockCacheMu.Unlock()
	}

	// 4. [VANGUARD-RECONCILIATION] Cơ chế Tự động Chữa lành (Auto-Heal) cho lỗi Nonce
	maxRetries := 1
	var tx *pb_block.Transaction
	var txHashStr string
	var txBytes []byte
	var nextNonce uint64

	log.Printf("[RPC-SEND] 🧪 [PHASE 4] Bắt đầu mô phỏng giao dịch (Sender: %s)...", senderHex[:8])
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// [V7.2] Tối ưu: GetNextNonce sẽ tự động lấy Nonce từ Ledger nếu chưa có trong Cache
		currentNonce := s.bridge.GetNonce(nil, senderBytes)
		if req.Nonce != nil {
			nextNonce = *req.Nonce
		} else {
			nextNonce = s.netMgr.Mempool.GetNextNonce(senderHex, currentNonce)
		}

		if attempt > 0 {
			auditLogs = append(auditLogs, fmt.Sprintf("🔄 [RETRY] Thử lại lần %d với Nonce mới: #%d", attempt, nextNonce))
		} else {
			if req.Nonce != nil {
				auditLogs = append(auditLogs, fmt.Sprintf("🛡️ [PHASE 2] Thủ công Nonce: Sử dụng Nonce thủ công #%d theo yêu cầu.", nextNonce))
			} else {
				auditLogs = append(auditLogs, fmt.Sprintf("🛡️ [PHASE 2] Dự phóng Nonce: Sử dụng Nonce #%d để tránh xung đột Mempool.", nextNonce))
			}
		}

		// Gọi Rust Core để TỰ TAY tạo và ký Giao dịch (Source of Truth)
		var err error
		tx, err = s.bridge.PrepareTransaction(
			senderBytes,
			receiverBytes,
			amountVNT,
			feeVNT,
			nextNonce,
			seed[:32], // Private Key (Seed)
			recentHash,
		)

		if err != nil {
			auditLogs = append(auditLogs, "❌ [CRITICAL] Rust Core từ chối chuẩn bị giao dịch: "+err.Error())
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "Error", "message": "Lỗi từ lõi Ledger", "audit_logs": auditLogs})
			return
		}

		// Lấy TxID (Full Hash) bằng Go Native, phản hồi nhanh dưới 1ms
		txBytes, _ = proto.MarshalOptions{Deterministic: true}.Marshal(tx)
		h_full := node_p2p.GetTxIDNative(txBytes)
		txHashStr = hex.EncodeToString(h_full)
		auditLogs = append(auditLogs, "🛡️ [PHASE 4] Go Native: Tính toán thành công TxID ổn định (Stable TxID).")
		break // Bỏ qua simulation và nạp mempool, thoát vòng lặp lập tức
	}

	// Cập nhật Tracker trạng thái ĐANG CHỜ XE BUÝT (99)
	s.updateTxTracker(txHashStr, senderHex, receiverHex, amountVNT, feeVNT, nextNonce, 0, time.Now().Unix(), "")
	s.txTrackerMu.Lock()
	if tracked, exists := s.txTracker[txHashStr]; exists {
		tracked.Status = 99
		tracked.ErrorMessage = s.getTxStatusMessage(99)
	}
	s.txTrackerMu.Unlock()
	s.triggerSave()

	// Ném vào Trạm chờ (TxBus RAM Channel)
	s.netMgr.Mempool.PushToTxBus(txBytes, true)
	auditLogs = append(auditLogs, "🛡️ [PHASE 5] Ném vào trạm chờ: Đã chuyển giao dịch vào hàng đợi TxBus (RAM).")

	log.Printf("[RPC-SEND] ✅ [ASYNC-SUCCESS] Giao dịch %s đã xếp hàng chờ xe buýt.", safeShortID(txHashStr))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "Success",
		"txid":       txHashStr,
		"audit_logs": auditLogs,
	})
}

// handleSendBatchTx: (EBP - Exchange Batch Protocol) API xử lý và đóng gói lô giao dịch tuần tự của Sàn
func (s *RPCServer) handleSendBatchTx(w http.ResponseWriter, r *http.Request) {
	// ==========================================
	// [VANGUARD-DISABLED] TẠM THỜI VÔ HIỆU HÓA TXSQ
	// ==========================================
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Error",
		"message": "Tính năng Gửi lô giao dịch (EBP/TXSQ) đang tạm thời bị vô hiệu hóa trên mạng lưới.",
	})
	return
	// ==========================================

	start := time.Now()
	log.Printf("[RPC-BATCH] 🏁 Bắt đầu xử lý Giao dịch Lô Tuần Tự (EBP) từ %s", r.RemoteAddr)

	var req struct {
		Sender       string `json:"sender"`
		SeqNum       uint64 `json:"seq_num"`
		Password     string `json:"password"`
		Transactions []struct {
			Receiver string          `json:"receiver"`
			Amount   json.RawMessage `json:"amount"`
			BaseFee  uint64          `json:"base_fee"`
			Nonce    *uint64         `json:"nonce"`
		} `json:"transactions"`
		SignedTxs []string `json:"signed_txs"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 512*1024)).Decode(&req); err != nil { // 0.5MB limit cho lô lớn
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": err.Error()})
		return
	}
	defer r.Body.Close()

	senderHex := strings.TrimPrefix(req.Sender, "0x")
	senderBytes, err := hex.DecodeString(senderHex)
	if err != nil || len(senderBytes) != 32 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ ví Sàn (Sender) không hợp lệ"})
		return
	}

	// [AUTO-GAP-HEAL-MUTEX] Khóa đồng bộ theo từng Sender
	// Tại sao thiết kế như vậy: Khi client gửi request spam song song hoặc gối đầu nhau rất nhanh, 
	// việc ký giao dịch tốn thời gian (300ms+) dẫn tới việc dự phóng nonce bị gối đầu và xáo trộn 
	// thứ tự đẩy vào TxBus, gây ra lỗi Nonce Gap nghiêm trọng. Tuần tự hóa xử lý theo từng sender 
	// giải quyết triệt để race condition này.
	s.senderLocksMu.Lock()
	if s.senderLocks == nil {
		s.senderLocks = make(map[string]*countedMutex)
	}
	lock, exists := s.senderLocks[senderHex]
	if !exists {
		lock = &countedMutex{}
		s.senderLocks[senderHex] = lock
	}
	atomic.AddInt32(&lock.refCount, 1)
	s.senderLocksMu.Unlock()

	lock.Lock()
	defer func() {
		lock.Unlock()
		s.senderLocksMu.Lock()
		if atomic.AddInt32(&lock.refCount, -1) == 0 {
			delete(s.senderLocks, senderHex)
		}
		s.senderLocksMu.Unlock()
	}()

	// [LỚP 1] CẢNH VỆ VÒNG NGOÀI (Giới hạn tối đa 2,500 giao dịch/lô để tránh phồng mempool và nghẽn CPU xử lý)
	totalTxCount := len(req.Transactions) + len(req.SignedTxs)
	if totalTxCount == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Lô giao dịch trống"})
		return
	}
	if totalTxCount > 2500 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Số lượng giao dịch vượt quá giới hạn 2,500 giao dịch/lô"})
		return
	}

	// [HSSD LAYER 1 & SECURITY-FIX] Áp dụng Rate Limiter cho API Batch để chống cạn kiệt tài nguyên / DoS
	// Tại sao: Ngăn chặn kẻ tấn công lợi dụng batch gửi hàng nghìn giao dịch liên tục mà không bị hạn chế, 
	// gây nghẽn kết nối gRPC và Starvation khóa txTrackerMu.
	if !s.allowTransactions(senderHex, totalTxCount) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Yêu cầu bị từ chối do vượt quá giới hạn tần suất gửi (Rate limit exceeded). Vui lòng thử lại sau."})
		return
	}

	var txsBytes [][]byte
	var txHashes []string
	currentNonce := s.netMgr.Mempool.GetExpectedNonce(senderHex, s.bridge.GetNonce(nil, senderBytes))
	auditLogs := []string{
		fmt.Sprintf("🛡️ [EBP-PHASE 1] Nhận dạng lô giao dịch từ Sàn: %s, Số lượng: %d, Sequence: #%d", senderHex[:8], totalTxCount, req.SeqNum),
	}

	// [LỚP 2] XỬ LÝ LÔ GIAO DỊCH
	if len(req.SignedTxs) > 0 {
		// --- TRƯỜNG HỢP 1: Lô giao dịch đã ký sẵn offline ---
		auditLogs = append(auditLogs, "🛡️ [EBP-PHASE 2] Khởi chạy kiểm tra lô giao dịch ký sẵn (Offline Batch)...")
		// Sử dụng currentNonce đã định nghĩa ở ngoài
		spendable := s.bridge.GetSpendableBalance(senderBytes)
		mempoolPending := s.netMgr.Mempool.GetPendingSpend(senderHex)
		if spendable >= mempoolPending {
			spendable -= mempoolPending
		} else {
			spendable = 0
		}

		type txEvalResult struct {
			idx     int
			tx      *pb_block.Transaction
			data    []byte
			txHash  []byte
			err     error
		}

		results := make([]txEvalResult, len(req.SignedTxs))
		var wg sync.WaitGroup

		for idx, txHex := range req.SignedTxs {
			wg.Add(1)
			go func(i int, hexStr string) {
				defer wg.Done()
				res := &results[i]
				res.idx = i

				txHexClean := strings.TrimPrefix(hexStr, "0x")
				data, err := hex.DecodeString(txHexClean)
				if err != nil {
					res.err = fmt.Errorf("Giao dịch index %d có định dạng hex không hợp lệ: %v", i, err)
					return
				}
				res.data = data

				var tx pb_block.Transaction
				if err := proto.Unmarshal(data, &tx); err != nil {
					res.err = fmt.Errorf("Không thể giải mã giao dịch index %d: %v", i, err)
					return
				}
				res.tx = &tx

				// Kiểm tra chữ ký Ed25519 bằng Go Native để tránh bão gRPC Storm mà vẫn bảo đảm an ninh nghiêm ngặt
				if !node_p2p.VerifySignatureNative(&tx) {
					audit.AuditLog("SIGNATURE_SPOOFING", "local", fmt.Sprintf("Phát hiện giao dịch Batch index %d có chữ ký Ed25519 giả mạo", i))
					res.err = fmt.Errorf("Giao dịch index %d có chữ ký không hợp lệ (Native Go)", i)
					return
				}

				// [SECURITY-VANGUARD] Xác thực người gửi giao dịch phải trùng khớp với ví Sàn để tránh giả mạo
				if tx.Sender == nil || !bytes.Equal(tx.Sender.Value, senderBytes) {
					res.err = fmt.Errorf("Giao dịch index %d có người gửi (Sender) không khớp với địa chỉ ví Sàn", i)
					return
				}

				// Tính toán TxID Native để tránh gọi GetCanonicalTxHash gRPC
				res.txHash = node_p2p.GetTxIDNative(data)
			}(idx, txHex)
		}
		wg.Wait()

		// Kiểm tra kết quả và thực thi tuần tự
		totalRequired := uint64(0)
		for i := 0; i < len(req.SignedTxs); i++ {
			res := results[i]
			if res.err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "Error", "message": res.err.Error(), "audit_logs": auditLogs,
				})
				return
			}

			// Thống kê tổng số dư cần (Tuần tự hóa tránh race condition)
			totalRequired += res.tx.Amount + res.tx.Fee
			if spendable < totalRequired {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "Error", "message": fmt.Sprintf("Số dư không đủ tại giao dịch index %d (Cần tích lũy: %d, Khả dụng: %d)", i, totalRequired, spendable), "audit_logs": auditLogs,
				})
				return
			}

			// Kiểm tra Nonce liên tục
			expectedNonce := currentNonce + uint64(i)
			if res.tx.Nonce != expectedNonce {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "Error", "message": fmt.Sprintf("Giao dịch index %d có Nonce lệch thứ tự (Có: %d, Mong đợi: %d)", i, res.tx.Nonce, expectedNonce), "audit_logs": auditLogs,
				})
				return
			}

			txHashes = append(txHashes, hex.EncodeToString(res.txHash))
			txsBytes = append(txsBytes, res.data)
		}

		auditLogs = append(auditLogs, fmt.Sprintf("🛡️ [EBP-PHASE 3] Thành công: Đã thẩm định thành công toàn bộ chữ ký & Nonce của %d giao dịch.", len(req.SignedTxs)))

	} else {
		// --- TRƯỜNG HỢP 2: Lô giao dịch cần Node ký tự động (Online Batch) ---
		auditLogs = append(auditLogs, "🛡️ [EBP-PHASE 2] Khởi chạy quy trình chuẩn bị và ký lô giao dịch tự động...")
		
		// Lấy Private Key của ví gửi
		_, err = s.walletMgr.LoadWallet(senderHex)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Ví sàn chưa được đăng ký hoặc Watch-Only"})
			return
		}

		seed, err := s.walletMgr.GetSeed(senderHex, req.Password)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Mật khẩu ví Sàn không chính xác!"})
			return
		}

		highestHeight := s.bridge.GetCurrentVersion()
		recentHash := s.bridge.GetBlockHash(highestHeight)
		if recentHash == nil {
			recentHash = make([]byte, 32)
		}

		// Sử dụng currentNonce đã định nghĩa ở ngoài
		spendable := s.bridge.GetSpendableBalance(senderBytes)
		mempoolPending := s.netMgr.Mempool.GetPendingSpend(senderHex)
		if spendable >= mempoolPending {
			spendable -= mempoolPending
		} else {
			spendable = 0
		}

		type txSignResult struct {
			idx       int
			amountVNT uint64
			feeVNT    uint64
			txBytes   []byte
			txHash    []byte
			err       error
		}

		results := make([]txSignResult, len(req.Transactions))
		var wg sync.WaitGroup

		// Tại sao: Sử dụng Semaphore channel để giới hạn tối đa 20 goroutines đồng thời gọi gRPC PrepareTransaction
		// sang Rust Core. Điều này giúp ngăn chặn hoàn toàn hiện tượng nghẽn hàng đợi IPC (gRPC Concurrency Storm),
		// loại bỏ hoàn toàn lỗi gRPC Timeout (context deadline exceeded) và giữ cho kết nối P2P luôn ổn định dưới tải stress test lớn.
		sem := make(chan struct{}, 20)

		for idx, item := range req.Transactions {
			wg.Add(1)
			go func(i int, txItem struct {
				Receiver string          `json:"receiver"`
				Amount   json.RawMessage `json:"amount"`
				BaseFee  uint64          `json:"base_fee"`
				Nonce    *uint64         `json:"nonce"`
			}) {
				defer wg.Done()
				sem <- struct{}{}        // Lấy token semaphore
				defer func() { <-sem }() // Trả token semaphore

				res := &results[i]
				res.idx = i

				receiverHexClean := strings.TrimPrefix(txItem.Receiver, "0x")
				receiverBytes, err := hex.DecodeString(receiverHexClean)
				if err != nil || len(receiverBytes) != 32 {
					res.err = fmt.Errorf("Giao dịch index %d có địa chỉ người nhận không hợp lệ", i)
					return
				}

				amountStr := strings.Trim(string(txItem.Amount), "\"")
				amountVNT, err := s.AmountToVNT(amountStr)
				if err != nil {
					res.err = fmt.Errorf("Giao dịch index %d có số tiền sai định dạng", i)
					return
				}
				res.amountVNT = amountVNT

				baseFee := txItem.BaseFee
				if baseFee == 0 {
					baseFee = 250
				}
				feeVNT := s.bridge.CalculateNanoFee(amountVNT, uint32(baseFee))
				res.feeVNT = feeVNT

				expectedNonce := currentNonce + uint64(i)
				if txItem.Nonce != nil && *txItem.Nonce != expectedNonce {
					res.err = fmt.Errorf("Giao dịch index %d có Nonce thủ công không tuần tự (Có: %d, Mong đợi: %d)", i, *txItem.Nonce, expectedNonce)
					return
				}

				// Tạo và ký giao dịch bằng Rust
				tx, err := s.bridge.PrepareTransaction(
					senderBytes,
					receiverBytes,
					amountVNT,
					feeVNT,
					expectedNonce,
					seed[:32],
					recentHash,
				)
				if err != nil {
					res.err = fmt.Errorf("Rust Core từ chối chuẩn bị giao dịch index %d: %v", i, err)
					return
				}

				txBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
				res.txBytes = txBytes
				// Tại sao: Tính toán TxID Native trên Go Core để tránh gọi GetCanonicalTxHash gRPC sang Rust Core,
				// giảm thiểu 50% số lượng cuộc gọi gRPC dội vào Rust khi chuẩn bị lô giao dịch lớn (triệt tiêu gRPC Storm).
				res.txHash = node_p2p.GetTxIDNative(txBytes)
			}(idx, item)
		}
		wg.Wait()

		// Kiểm tra kết quả và tích lũy tuần tự
		totalRequired := uint64(0)
		for i := 0; i < len(req.Transactions); i++ {
			res := results[i]
			if res.err != nil {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(res.err.Error(), "Không hợp lệ") || strings.Contains(res.err.Error(), "sai định dạng") || strings.Contains(res.err.Error(), "Nonce") {
					w.WriteHeader(http.StatusBadRequest)
				} else {
					w.WriteHeader(http.StatusInternalServerError)
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "Error", "message": res.err.Error(), "audit_logs": auditLogs,
				})
				return
			}

			totalRequired += res.amountVNT + res.feeVNT
			if spendable < totalRequired {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "Error", "message": fmt.Sprintf("Số dư sàn không đủ tại giao dịch index %d (Yêu cầu tích lũy: %d, Khả dụng: %d)", i, totalRequired, spendable), "audit_logs": auditLogs,
				})
				return
			}

			txHashes = append(txHashes, hex.EncodeToString(res.txHash))
			txsBytes = append(txsBytes, res.txBytes)
		}
		auditLogs = append(auditLogs, fmt.Sprintf("🛡️ [EBP-PHASE 3] Thành công: Đã tự động tạo và ký kết thành công %d giao dịch.", len(req.Transactions)))
	}

	// [LỚP 3] XÁC THỰC LÔ GIAO DỊCH ĐỒNG BỘ VỚI RUST CORE
	auditLogs = append(auditLogs, "🛡️ [EBP-PHASE 4] Khởi động kiểm duyệt đồng bộ toàn bộ lô giao dịch...")
	resp, err := s.bridge.ValidateTransactionBatch(txsBytes)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "Error", "message": fmt.Sprintf("Lỗi kết nối gRPC khi validate batch: %v", err), "audit_logs": auditLogs,
		})
		return
	}

	userAddrs := s.getUserWalletAddresses()
	senderBal := s.bridge.GetBalance(nil, senderBytes, 0)
	var validTxsBytes [][]byte

	for idx, result := range resp.Results {
		txBytes := txsBytes[idx]
		txHashStr := txHashes[idx]
		
		var tx pb_block.Transaction
		if err := proto.Unmarshal(txBytes, &tx); err != nil {
			continue
		}
		receiverHex := hex.EncodeToString(tx.Receiver.Value)

		isValid := result.IsValid
		statusCode := result.StatusCode
		errorMsg := result.ErrorMsg

		// [EBP-BATCH-HEAL] Chữa lành cho giao dịch bị lệch nonce do độ trễ đóng khối
		if !isValid && statusCode == 106 {
			expectedNonce := currentNonce + uint64(idx)
			if tx.Nonce == expectedNonce {
				isValid = true
				statusCode = 0
				errorMsg = ""
				log.Printf("[RPC-BATCH] 🩹 Chữa lành giao dịch Batch tuần tự: Hash=%s Nonce=%d (Expect=%d).", txHashStr[:12], tx.Nonce, expectedNonce)
			}
		}

		if isValid {
			// Xác định creationFee nếu là ví mới tinh
			creationFee := uint64(0)
			if receiverState := s.bridge.GetAccountState(tx.Receiver.Value); receiverState != nil {
				isNewWallet := receiverState.Balance == 0 && receiverState.Nonce == 0 && len(receiverState.MaturingRewards) == 0
				if isNewWallet {
					creationFee = 1000
				}
			}
			
			// Nạp trực tiếp vào mempool Go (và Rust Core) đồng bộ
			success := s.netMgr.Mempool.AddValidatedTx(txHashStr, txBytes, senderHex, &tx, creationFee)
			if success {
				validTxsBytes = append(validTxsBytes, txBytes)
				// Cập nhật Tracker với trạng thái Success (0)
				s.updateTxTrackerWithCachedData(txHashStr, senderHex, receiverHex, tx.Amount, tx.Fee, tx.Nonce, 0, time.Now().Unix(), "", senderBal, userAddrs)
				s.txTrackerMu.Lock()
				if tracked, exists := s.txTracker[txHashStr]; exists {
					tracked.Status = 0
					tracked.ErrorMessage = ""
				}
				s.txTrackerMu.Unlock()
			} else {
				s.netMgr.Mempool.ClearProjectedNonce(senderHex)
				s.updateTxTrackerWithCachedData(txHashStr, senderHex, receiverHex, tx.Amount, tx.Fee, tx.Nonce, 0, time.Now().Unix(), "Lỗi nạp mempool Go sau khi Rust đã phê duyệt", senderBal, userAddrs)
				s.txTrackerMu.Lock()
				if tracked, exists := s.txTracker[txHashStr]; exists {
					tracked.Status = 998
					tracked.ErrorMessage = "Lỗi nạp mempool Go sau khi Rust đã phê duyệt"
				}
				s.txTrackerMu.Unlock()
			}
		} else {
			// Clear projected nonce của ví ngay lập tức để tự sửa đổi
			s.netMgr.Mempool.ClearProjectedNonce(senderHex)
			
			// Đăng ký tracker với mã lỗi thực tế của Rust
			s.updateTxTrackerWithCachedData(txHashStr, senderHex, receiverHex, tx.Amount, tx.Fee, tx.Nonce, 0, time.Now().Unix(), errorMsg, senderBal, userAddrs)
			s.txTrackerMu.Lock()
			if tracked, exists := s.txTracker[txHashStr]; exists {
				tracked.Status = statusCode
				tracked.ErrorMessage = errorMsg
			}
			s.txTrackerMu.Unlock()
			
			// [HSSD LỚP 5] Ngắt mạch tự động (Circuit Breaker) nếu phát hiện lỗi lệch nonce hoặc số dư nghiêm trọng
			log.Printf("[EBP-WARNING] Giao dịch %s bị từ chối đồng bộ: Mã=%d, Lỗi=%s", txHashStr[:12], statusCode, errorMsg)
		}
	}
	s.triggerSave()

	// [LỚP 4] PHÁT SÓNG GIAO DỊCH HỢP LỆ LÊN MẠNG LƯỚI P2P
	// Ghi chú: Đã loại bỏ dòng gọi GetAndReserveNonces dư thừa làm lệch Nonce dự phóng.
	// AddValidatedTx ở trên đã tự động cập nhật projectedNonce khi nạp mempool thành công.

	if len(validTxsBytes) >= 2 {
		// SỬA ĐỔI: Chia nhỏ lô để phát sóng, mỗi gói tối đa 500 TX (~350KB) chống nghẽn P2P và khớp với maxSize (500KB) của P2P Shield
		const maxGossipBatch = 500
		for i := 0; i < len(validTxsBytes); i += maxGossipBatch {
			end := i + maxGossipBatch
			if end > len(validTxsBytes) {
				end = len(validTxsBytes)
			}
			chunkBytes := validTxsBytes[i:end]

			var nonces []uint64
			for _, txBytes := range chunkBytes {
				var tx pb_block.Transaction
				proto.Unmarshal(txBytes, &tx)
				nonces = append(nonces, tx.Nonce)
			}
			sort.Slice(nonces, func(i, j int) bool { return nonces[i] < nonces[j] })
			startNonce := nonces[0]
			endNonce := nonces[len(nonces)-1]
			seqNum := startNonce

			batchData := node_p2p.PackSequentialBatch(senderBytes, seqNum, startNonce, endNonce, chunkBytes)
			if s.netMgr.PubSub != nil {
				go func(data []byte, count int, sNonce uint64) {
					if err := s.netMgr.PubSub.Publish("txs", data); err != nil {
						log.Printf("[P2P-BROADCAST] ❌ Phát sóng lô TXSQ thất bại: %v", err)
					} else {
						log.Printf("[P2P-BROADCAST] 🚀 Đã phát sóng chunk TXSQ (%d TXs, Start: %d) lên GossipSub.", count, sNonce)
					}
				}(batchData, len(chunkBytes), startNonce)
			}
		}
	} else if len(validTxsBytes) == 1 {
		go func(data []byte) {
			if err := s.netMgr.BroadcastTransaction(data); err != nil {
				log.Printf("[P2P-BROADCAST] ❌ Phát sóng đơn lẻ thất bại đồng bộ: %v", err)
			}
		}(validTxsBytes[0])
	}

	auditLogs = append(auditLogs, fmt.Sprintf("🚀 [EBP-PHASE 5] Đã xử lý đồng bộ xong lô giao dịch. Thành công: %d/%d giao dịch.", len(validTxsBytes), len(txsBytes)))

	// Trả về kết quả cho client
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "Success",
		"sequence":    req.SeqNum,
		"tx_count":    len(txsBytes),
		"tx_hashes":   txHashes,
		"audit_logs":  auditLogs,
		"duration_ms": time.Since(start).Milliseconds(),
	})
}

func (s *RPCServer) handleSetMinerAddress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Address string `json:"address"`
		Pin     string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Payload không hợp lệ"})
		return
	}

	// [LỚP 1] Validate Format: Phải là 32 bytes hex (64 ký tự)
	cleanAddr := strings.TrimPrefix(req.Address, "0x")
	addrBytes, err := hex.DecodeString(cleanAddr)
	if err != nil || len(addrBytes) != 32 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ phải là 32 bytes hex hợp lệ (64 ký tự)"})
		return
	}

	// [LỚP 2] Validate: (Cho phép ví Zero cho test local)
	isNull := true
	for _, b := range addrBytes {
		if b != 0 {
			isNull = false
			break
		}
	}
	if isNull {
		log.Printf("[MINER-CONTROL] ⚠️ Cảnh báo: Sử dụng ví Zero (toàn số 0) để đào.")
	}

	// [LỚP 3] Cập nhật
	if s.cliApp != nil {
		// Lấy Private Key từ WalletManager nếu có PIN (Không bắt buộc cho thợ đào)
		var key ed25519.PrivateKey
		if req.Pin != "" {
			seed, _ := s.walletMgr.GetSeed(cleanAddr, req.Pin)
			if len(seed) >= 32 {
				key = ed25519.NewKeyFromSeed(seed[:32])
				s.minerKey = key
				log.Printf("[RPC-MINER] 🔐 Đã nạp Private Key cho thợ đào 0x%s", cleanAddr[:8])
			}
		} else {
			log.Printf("[RPC-MINER] ℹ️ Cập nhật địa chỉ nhận thưởng (Chế độ Không PIN): 0x%s", cleanAddr[:8])
		}
		s.cliApp.SetMinerAddress(addrBytes, key, req.Pin)
	}

	// [FINAL] Duy trì bản sao tại RPCServer để tương thích ngược
	s.minerAddr = addrBytes

	// [AUTO-GENESIS] Đã được xử lý bởi Rust Core
	h := s.bridge.GetCurrentVersion()
	if h == 0 {
		genHash := s.bridge.GetBlockHash(0)
		if genHash == nil && s.cliApp != nil {
			log.Printf("[VANGUARD] 🚀 Yêu cầu Rust khởi tạo Genesis...")
		}
	}

	// [V1.5 PERSISTENCE] Lưu lại ví đào mặc định
	s.saveNodeConfig()

	log.Printf("[RPC] 🛡️ Đã cập nhật Ví nhận thưởng đào: 0x%s", cleanAddr)
	json.NewEncoder(w).Encode(map[string]string{"status": "Success", "address": cleanAddr})
}

func (s *RPCServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := mux.Vars(r)["query"]
	w.Header().Set("Content-Type", "application/json")

	// 1. Kiểm tra nếu là Độ cao (Numeric)
	if h, err := strconv.ParseUint(query, 10, 64); err == nil {
		if hash := s.bridge.GetBlockHash(h); hash != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"type": "block", "height": h, "hash": hex.EncodeToString(hash)})
			return
		}
	}

	cleanQuery := strings.TrimPrefix(query, "0x")

	// 2. Kiểm tra định dạng Hex (32 bytes = 64 ký tự)
	if data, err := hex.DecodeString(cleanQuery); err == nil && len(data) == 32 {
		// A. Thử tìm kiếm Giao dịch (TxID) sâu trong Blockchain
		h, status, finalized, confs, _, _, _, _ := s.bridge.GetTransactionStatus(data)
		if h > 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"type":          "tx",
				"txid":          cleanQuery,
				"height":        h,
				"status_code":   status,
				"finalized":     finalized,
				"confirmations": confs,
			})
			return
		}

		// B. Thử tìm kiếm Khối (Block Hash)
		// Cách tốt nhất là dùng GetRawByHash và thử parse Block
		raw := s.bridge.GetRawByHash(data)
		if raw != nil {
			var b pb_block.Block
			if err := proto.Unmarshal(raw, &b); err == nil && b.Header != nil {
				// Tìm thấy Block bằng Hash
				// Lưu ý: Rust Core có thể không trả về height trong raw data,
				// nhưng ta có thể tìm height nếu cần. Hiện tại trả về hash là đủ.
				json.NewEncoder(w).Encode(map[string]interface{}{"type": "block", "hash": cleanQuery, "found": true})
				return
			}
		}

		// C. Mặc định coi là Địa chỉ ví nếu không khớp gì ở trên
		json.NewEncoder(w).Encode(map[string]interface{}{"type": "address", "address": "0x" + cleanQuery})
		return
	}

	// 3. Tìm kiếm trong Tx Tracker (Dành cho các Tx vừa gửi/mempool chưa vào khối)
	s.txTrackerMu.RLock()
	for _, tx := range s.txTracker {
		if strings.HasPrefix(tx.TxID, cleanQuery) || tx.TxID == cleanQuery {
			s.txTrackerMu.RUnlock()
			json.NewEncoder(w).Encode(map[string]interface{}{"type": "tx", "txid": tx.TxID, "height": tx.BlockHeight})
			return
		}
	}
	s.txTrackerMu.RUnlock()

	http.Error(w, "Không tìm thấy kết quả cho: "+query, http.StatusNotFound)
}

func (s *RPCServer) handleRecentBlocks(w http.ResponseWriter, r *http.Request) {
	highest := s.bridge.GetCurrentVersion()

	// [V4.0 OPTIMIZATION] Hỗ trợ phân trang limit/offset
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, _ := strconv.ParseUint(limitStr, 10, 64)
	if limit == 0 || limit > 500 {
		limit = 200
	} // Mặc định 200, tối đa 500

	offset, _ := strconv.ParseUint(offsetStr, 10, 64)

	var blocks []interface{}
	startHeight := uint64(0)
	if highest > offset {
		startHeight = highest - offset
	} else if offset > highest {
		// Nếu offset lớn hơn chiều cao hiện tại, trả về mảng rỗng
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blocks)
		return
	}

	// [VANGUARD-CACHE] Thử lấy từ bộ nhớ đệm trước
	s.recentBlocksCacheMu.RLock()
	cacheValid := false
	if offset == 0 && uint64(len(s.recentBlocksCache)) >= limit && limit <= 200 {
		// [SAFE-CHECK] Kiểm tra xem chiều cao của khối đầu tiên trong cache có khớp với đỉnh hiện tại không
		// Tại sao làm vậy: Đảm bảo khi blockchain tăng chiều cao, cache cũ bị loại bỏ ngay lập tức để lấy dữ liệu mới từ Rust Ledger.
		if len(s.recentBlocksCache) > 0 {
			if firstBlock, ok := s.recentBlocksCache[0].(map[string]interface{}); ok {
				if cachedHeight, ok := firstBlock["height"].(uint64); ok && cachedHeight == highest {
					cacheValid = true
				}
			}
		}
	}
	if cacheValid {
		// Trả về bản sao từ cache (Lấy top 'limit' khối)
		blocks = s.recentBlocksCache[:limit]
		s.recentBlocksCacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blocks)
		return
	}
	s.recentBlocksCacheMu.RUnlock()

	for i := uint64(0); i < limit; i++ {
		if startHeight < i {
			break
		}
		h := startHeight - i
		blockRaw := s.bridge.GetBlock(h)
		if blockRaw != nil {
			var b pb_block.Block
			proto.Unmarshal(blockRaw, &b)
			if b.Header != nil {
				header := b.Header
				minerAddr := ""
				if header.MinerAddress != nil {
					minerAddr = hex.EncodeToString(header.MinerAddress.Value)
				}
				stateRoot := ""
				if header.StateRoot != nil {
					stateRoot = hex.EncodeToString(header.StateRoot.Value)
				}
				txRoot := ""
				if header.TxRoot != nil {
					txRoot = hex.EncodeToString(header.TxRoot.Value)
				}

				headerBytes, _ := proto.Marshal(header)
				blockHash := s.bridge.GetCanonicalBlockHeaderHash(headerBytes, header.Height)

				blocks = append(blocks, map[string]interface{}{
					"height":      h,
					"hash":        hex.EncodeToString(blockHash),
					"parent_hash": hex.EncodeToString(header.ParentHash.Value),
					"timestamp":   header.Timestamp,
					"miner":       minerAddr,
					"state_root":  stateRoot,
					"tx_root":     txRoot,
					"nonce":       header.Nonce,
					"tx_count":    len(b.Body.Transactions),
					"difficulty":  hex.EncodeToString(header.Difficulty),
				})
			}
		}
	}

	// [VANGUARD-CACHE] Nếu là trang đầu, cập nhật lại cache
	if offset == 0 {
		s.recentBlocksCacheMu.Lock()
		s.recentBlocksCache = blocks
		s.recentBlocksCacheMu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(blocks)
}

func (s *RPCServer) handleRecentTransactions(w http.ResponseWriter, r *http.Request) {
	// [VANGUARD-LOCK-OPTIMIZATION] Bước 1: Thu thập các ID chưa finalized dưới Write Lock để cập nhật lười RawTxID
	// Tại sao thiết kế như vậy: Sử dụng Write Lock để có thể giải mã lười (lazy decode) TxID sang RawTxID một lần duy nhất
	// và lưu vào cache của struct TrackedTx. Việc này hoàn toàn loại bỏ vòng lặp decode hex lặp đi lặp lại ngoài Lock,
	// giúp tăng tốc độ phản hồi đáng kể và giảm phân bổ bộ nhớ (heap allocation).
	s.txTrackerMu.Lock()

	mempoolSet := make(map[string]bool)
	if s.netMgr != nil && s.netMgr.Mempool != nil {
		pendingTxs := s.netMgr.Mempool.GetPendingTxList()
		for _, p := range pendingTxs {
			mempoolSet[p.Hash] = true
		}
	}

	var unfinalizedIDs []string
	var batchHashes [][]byte
	maxDisplay := 100
	count := 0
	for i := len(s.txOrder) - 1; i >= 0 && count < maxDisplay; i-- {
		txID := s.txOrder[i]
		if mempoolSet[txID] {
			continue
		}
		tx, ok := s.txTracker[txID]
		if !ok {
			continue
		}
		count++
		if tx.BlockHeight > 0 && !tx.IsFinalized {
			unfinalizedIDs = append(unfinalizedIDs, txID)
			if tx.RawTxID == nil {
				tx.RawTxID, _ = hex.DecodeString(txID)
			}
			batchHashes = append(batchHashes, tx.RawTxID)
		}
	}
	s.txTrackerMu.Unlock()

	// Bước 2: Gọi FFI GetTransactionStatusBatch ngoài Lock để tối ưu hóa hiệu năng và ngăn chặn gRPC Storm
	type txStatusUpdate struct {
		txID          string
		statusCode    uint32
		isFinalized   bool
		confirmations uint64
	}
	updates := make([]txStatusUpdate, 0, len(unfinalizedIDs))
	if len(unfinalizedIDs) > 0 {
		statusEntries, err := s.GetTransactionStatusBatchChunked(batchHashes)
		if err == nil {
			statusMap := make(map[string]*pb_block.TxStatusEntry)
			for _, entry := range statusEntries {
				statusMap[hex.EncodeToString(entry.Hash)] = entry
			}
			for _, id := range unfinalizedIDs {
				if entry, ok := statusMap[id]; ok {
					updates = append(updates, txStatusUpdate{
						txID:          id,
						statusCode:    entry.Status,
						isFinalized:   entry.IsFinalized,
						confirmations: entry.Confirmations,
					})
				}
			}
		}
	}

	// Bước 3: Cập nhật cache và dựng danh sách phản hồi dưới Write Lock
	s.txTrackerMu.Lock()
	for _, up := range updates {
		if tx, ok := s.txTracker[up.txID]; ok {
			tx.Status = up.statusCode
			tx.IsFinalized = up.isFinalized
			tx.Confirmations = up.confirmations
		}
	}

	var result []TxResponse
	var confirmedTxs []TxResponse
	countConfirmed := 0

	// Lấy pending từ Mempool
	if s.netMgr != nil && s.netMgr.Mempool != nil {
		pendingTxs := s.netMgr.Mempool.GetPendingTxList()
		for _, p := range pendingTxs {
			txTime := p.Timestamp
			if txTime == 0 {
				txTime = time.Now().Unix()
			}
			result = append(result, TxResponse{
				ID:            p.Hash,
				Sender:        p.Sender,
				Receiver:      p.Receiver,
				Amount:        p.Amount,
				Fee:           p.Fee,
				Timestamp:     txTime,
				Height:        0,
				Confirmations: 0,
				Status:        "ĐANG CHỜ (MEMPOOL)",
				StatusCode:    0,
				Direction:     "OUT",
				PrevBalance:   0,
				PostBalance:   0,
				Nonce:         p.Nonce,
			})
		}
	}

	count = 0
	for i := len(s.txOrder) - 1; i >= 0 && count < maxDisplay; i-- {
		txID := s.txOrder[i]
		if mempoolSet[txID] {
			continue
		}
		tx, ok := s.txTracker[txID]
		if !ok {
			continue
		}
		count++

		statCode := tx.Status
		finalized := tx.IsFinalized
		confs := tx.Confirmations

		if tx.BlockHeight > 0 {
			status := s.getTxStatusText(statCode, confs, finalized)
			if countConfirmed < 250 {
				confirmedTxs = append(confirmedTxs, TxResponse{
					ID:            tx.TxID,
					Sender:        tx.Sender,
					Receiver:      tx.Receiver,
					Amount:        tx.Amount,
					Fee:           tx.Fee,
					Timestamp:     tx.Timestamp,
					Height:        tx.BlockHeight,
					Confirmations: confs,
					PrevBalance:   tx.PrevBalance,
					PostBalance:   tx.PostBalance,
					Status:        status,
					StatusCode:    statCode,
					Nonce:         tx.Nonce,
					Direction:     s.getTxDirection(tx),
				})
				countConfirmed++
			}
		} else {
			isStillInMempool := mempoolSet[txID]
			txAge := time.Now().Unix() - tx.Timestamp

			if !isStillInMempool && txAge > 900 {
				result = append(result, TxResponse{
					ID:            tx.TxID,
					Sender:        tx.Sender,
					Receiver:      tx.Receiver,
					Amount:        tx.Amount,
					Fee:           tx.Fee,
					Timestamp:     tx.Timestamp,
					Height:        0,
					Confirmations: 0,
					Status:        "HẾT HẠN (EXPIRED)",
					StatusCode:    3,
					Direction:     s.getTxDirection(tx),
					PrevBalance:   tx.PrevBalance,
					PostBalance:   tx.PostBalance,
					Nonce:         tx.Nonce,
				})
			} else {
				statusText := "ĐANG CHỜ (MEMPOOL)"
				statusCode := tx.Status

				if tx.Status != 0 {
					statusText = s.getTxStatusText(tx.Status, 0, false)
					statusCode = tx.Status
				} else {
					statusCode = 0
				}

				result = append(result, TxResponse{
					ID:            tx.TxID,
					Sender:        tx.Sender,
					Receiver:      tx.Receiver,
					Amount:        tx.Amount,
					Fee:           tx.Fee,
					Timestamp:     tx.Timestamp,
					Height:        0,
					Confirmations: 0,
					Status:        statusText,
					StatusCode:    statusCode,
					Direction:     s.getTxDirection(tx),
					PrevBalance:   tx.PrevBalance,
					PostBalance:   tx.PostBalance,
					Nonce:         tx.Nonce,
				})
			}
		}
	}
	s.txTrackerMu.Unlock()

	result = append(result, confirmedTxs...)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleGetTxDetail: API chi tiết giao dịch theo TxID
// Endpoint: /api/v1/tx/{txid}
func (s *RPCServer) handleGetTxDetail(w http.ResponseWriter, r *http.Request) {
	txID := mux.Vars(r)["txid"]

	s.txTrackerMu.RLock()
	tx, ok := s.txTracker[txID]
	s.txTrackerMu.RUnlock()

	if !ok {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	// [VANGUARD-AUTHORITATIVE]
	h_bytes, _ := hex.DecodeString(tx.TxID)
	_, statCode, finalized, confs, s_prev, s_post, r_prev, r_post := s.bridge.GetTransactionStatus(h_bytes)

	// [CRITICAL FIX] Tránh Race Condition DB
	if statCode == 0 && tx.BlockHeight > 0 {
		statCode = 1
	}

	// [MEMPOOL-PURGE-UI-FIX] Nếu Rust báo pending (0) nhưng mempool thực tế không chứa giao dịch này,
	// và nó cũng chưa vào block (BlockHeight == 0), thì nó đã bị đào thải/xóa khỏi Mempool!
	if statCode == 0 && tx.BlockHeight == 0 && s.netMgr != nil && s.netMgr.Mempool != nil {
		if mp, ok := s.netMgr.Mempool.(*node_p2p.Mempool); ok {
			if _, exists := mp.GetTransaction(tx.TxID); !exists {
				statCode = 3 // Sai số thứ tự giao dịch (Nonce Mismatch)
			}
		}
	}

	status := s.getTxStatusText(statCode, confs, finalized)
	errDetail := ""
	if statCode != 1 && statCode != 0 {
		errDetail = s.getTxStatusMessage(statCode)
	} else if statCode != 1 && tx.ErrorMessage != "" {
		errDetail = tx.ErrorMessage
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TxResponse{
		ID:            tx.TxID,
		Sender:        tx.Sender,
		Receiver:      tx.Receiver,
		Amount:        tx.Amount,
		Fee:           tx.Fee,
		Timestamp:     tx.Timestamp,
		Height:        tx.BlockHeight,
		Confirmations: confs,
		Status:        status,
		StatusCode:    statCode,
		ErrorMessage:  errDetail,
		Nonce:         tx.Nonce,
		Direction:     s.getTxDirection(tx),
		PrevBalance:   iif64(s_prev > 0 || s_post > 0, s_prev, tx.PrevBalance),
		PostBalance:   iif64(s_prev > 0 || s_post > 0, s_post, tx.PostBalance),
		ReceiverPrev:  iif64(r_prev > 0 || r_post > 0, r_prev, 0),
		ReceiverPost:  iif64(r_prev > 0 || r_post > 0, r_post, 0),
	})
}

func (s *RPCServer) handleAddressHistory(w http.ResponseWriter, r *http.Request) {
	addrStr := mux.Vars(r)["address"]
	filter := r.URL.Query().Get("direction")                   // "in", "out" hoặc "" (all)
	searchTxID := strings.ToLower(r.URL.Query().Get("search")) // Tra cứu theo mã giao dịch

	cleanAddr := strings.ToLower(strings.TrimPrefix(addrStr, "0x"))

	log.Printf("[HISTORY-AUDIT] 🔍 Request for address: %s | Filter: %s", cleanAddr, filter)

	// [VANGUARD-THREAD-SAFE] Bước 1: Thu thập ứng viên và các giao dịch chưa finalized dưới RLock
	s.txTrackerMu.RLock()

	mempoolSet := make(map[string]bool)
	var pendingList []TxResponse
	if s.netMgr != nil && s.netMgr.Mempool != nil {
		pendingTxs := s.netMgr.Mempool.GetPendingTxList()
		for _, p := range pendingTxs {
			senderClean := strings.ToLower(strings.TrimPrefix(p.Sender, "0x"))
			receiverClean := strings.ToLower(strings.TrimPrefix(p.Receiver, "0x"))

			isSender := (senderClean == cleanAddr)
			isReceiver := (receiverClean == cleanAddr)
			if !isSender && !isReceiver {
				continue
			}

			matchesFilter := false
			if filter == "" {
				matchesFilter = true
			} else if filter == "in" && isReceiver {
				matchesFilter = true
			} else if filter == "out" && isSender {
				matchesFilter = true
			}
			if searchTxID != "" && !strings.Contains(strings.ToLower(p.Hash), searchTxID) {
				continue
			}
			if !matchesFilter {
				continue
			}

			directionLabel := "IN"
			if isSender {
				directionLabel = "OUT"
			}
			if isSender && isReceiver {
				directionLabel = "self"
			}

			mempoolSet[p.Hash] = true
			txTime := p.Timestamp
			if txTime == 0 {
				txTime = time.Now().Unix()
			}
			pendingList = append(pendingList, TxResponse{
				ID:            p.Hash,
				Sender:        p.Sender,
				Receiver:      p.Receiver,
				Amount:        p.Amount,
				Fee:           p.Fee,
				Timestamp:     txTime,
				Height:        0,
				Confirmations: 0,
				Status:        "ĐANG CHỜ (MEMPOOL)",
				StatusCode:    0,
				Direction:     directionLabel,
				IsSelf:        isSender && isReceiver,
				PrevBalance:   0,
				PostBalance:   0,
				Nonce:         p.Nonce,
			})
		}
	}

	type trackerCandidate struct {
		txID       string
		isSender   bool
		isReceiver bool
	}
	var candidates []trackerCandidate
	var unfinalizedIDs []string

	for i := len(s.txOrder) - 1; i >= 0 && len(candidates) < 500; i-- {
		txID := s.txOrder[i]
		if mempoolSet[txID] {
			continue
		}
		tx := s.txTracker[txID]
		if tx == nil {
			continue
		}

		senderClean := strings.ToLower(strings.TrimPrefix(tx.Sender, "0x"))
		receiverClean := strings.ToLower(strings.TrimPrefix(tx.Receiver, "0x"))

		isSender := (senderClean == cleanAddr)
		isReceiver := (receiverClean == cleanAddr)

		if !isSender && !isReceiver {
			continue
		}

		matchesFilter := false
		if filter == "" {
			matchesFilter = true
		} else if filter == "in" && isReceiver {
			matchesFilter = true
		} else if filter == "out" && isSender {
			matchesFilter = true
		}

		if !matchesFilter {
			continue
		}

		if searchTxID != "" && !strings.Contains(strings.ToLower(tx.TxID), searchTxID) {
			continue
		}

		candidates = append(candidates, trackerCandidate{
			txID:       txID,
			isSender:   isSender,
			isReceiver: isReceiver,
		})

		if tx.BlockHeight > 0 && !tx.IsFinalized {
			unfinalizedIDs = append(unfinalizedIDs, txID)
		}
	}
	s.txTrackerMu.RUnlock()

	// Bước 2: Gọi FFI GetTransactionStatusBatch ngoài Lock để tối ưu hóa hiệu năng và ngăn chặn gRPC Storm
	type txStatusUpdate struct {
		txID          string
		statusCode    uint32
		isFinalized   bool
		confirmations uint64
		s_prev        uint64
		s_post        uint64
		r_prev        uint64
		r_post        uint64
	}
	updates := make([]txStatusUpdate, 0, len(unfinalizedIDs))
	if len(unfinalizedIDs) > 0 {
		var batchHashes [][]byte
		for _, id := range unfinalizedIDs {
			hBytes, _ := hex.DecodeString(id)
			batchHashes = append(batchHashes, hBytes)
		}
		statusEntries, err := s.GetTransactionStatusBatchChunked(batchHashes)
		if err == nil {
			statusMap := make(map[string]*pb_block.TxStatusEntry)
			for _, entry := range statusEntries {
				statusMap[hex.EncodeToString(entry.Hash)] = entry
			}
			for _, id := range unfinalizedIDs {
				if entry, ok := statusMap[id]; ok {
					updates = append(updates, txStatusUpdate{
						txID:          id,
						statusCode:    entry.Status,
						isFinalized:   entry.IsFinalized,
						confirmations: entry.Confirmations,
						s_prev:        entry.SenderPrevBalance,
						s_post:        entry.SenderPostBalance,
						r_prev:        entry.ReceiverPrevBalance,
						r_post:        entry.ReceiverPostBalance,
					})
				}
			}
		}
	}

	// Bước 3: Cập nhật cache và dựng danh sách phản hồi dưới Write Lock
	s.txTrackerMu.Lock()
	type updateInfo struct {
		s_prev uint64
		s_post uint64
		r_prev uint64
		r_post uint64
	}
	updateBalances := make(map[string]updateInfo)
	for _, up := range updates {
		if tx, ok := s.txTracker[up.txID]; ok {
			tx.Status = up.statusCode
			tx.IsFinalized = up.isFinalized
			tx.Confirmations = up.confirmations
			updateBalances[up.txID] = updateInfo{
				s_prev: up.s_prev,
				s_post: up.s_post,
				r_prev: up.r_prev,
				r_post: up.r_post,
			}
		}
	}

	type txEntry struct {
		data      TxResponse
		timestamp int64
	}
	entries := make([]txEntry, 0, len(pendingList)+len(candidates))

	// Thêm mempool entries
	for _, p := range pendingList {
		entries = append(entries, txEntry{
			timestamp: p.Timestamp,
			data:      p,
		})
	}

	// Thêm tracker entries
	for _, c := range candidates {
		tx := s.txTracker[c.txID]
		if tx == nil {
			continue
		}

		statCode := tx.Status
		finalized := tx.IsFinalized
		confs := tx.Confirmations

		var s_prev, s_post, r_prev, r_post uint64
		if up, ok := updateBalances[c.txID]; ok {
			s_prev, s_post, r_prev, r_post = up.s_prev, up.s_post, up.r_prev, up.r_post
		}

		if statCode == 0 && tx.BlockHeight > 0 {
			statCode = 1
		}

		status := s.getTxStatusText(statCode, confs, finalized)
		errDetail := ""
		if statCode != 1 && statCode != 0 {
			errDetail = s.getTxStatusMessage(statCode)
		} else if statCode != 1 && tx.ErrorMessage != "" {
			errDetail = tx.ErrorMessage
		}

		directionLabel := "IN"
		if c.isSender {
			directionLabel = "OUT"
		}

		entries = append(entries, txEntry{
			timestamp: tx.Timestamp,
			data: TxResponse{
				ID:            tx.TxID,
				Sender:        tx.Sender,
				Receiver:      tx.Receiver,
				Amount:        tx.Amount,
				Fee:           tx.Fee,
				Timestamp:     tx.Timestamp,
				Height:        tx.BlockHeight,
				Confirmations: confs,
				Status:        status,
				StatusCode:    statCode,
				ErrorMessage:  errDetail,
				Direction:     directionLabel,
				IsSelf:        c.isSender && c.isReceiver,
				PrevBalance:   iif64(s_prev > 0 || s_post > 0, s_prev, tx.PrevBalance),
				PostBalance:   iif64(s_prev > 0 || s_post > 0, s_post, tx.PostBalance),
				ReceiverPrev:  iif64(r_prev > 0 || r_post > 0, r_prev, 0),
				ReceiverPost:  iif64(r_prev > 0 || r_post > 0, r_post, 0),
				Nonce:         tx.Nonce,
			},
		})
	}
	s.txTrackerMu.Unlock()

	// 4. Sắp xếp thông minh (Mempool lên đầu -> Chiều cao khối giảm dần -> Thời gian)
	sort.Slice(entries, func(i, j int) bool {
		h1 := entries[i].data.Height
		h2 := entries[j].data.Height

		// ƯU TIÊN 1: Khối 0 (Đang chờ trong Mempool) LUÔN LUÔN nằm trên cùng
		if h1 == 0 && h2 != 0 {
			return true
		}
		if h2 == 0 && h1 != 0 {
			return false
		}

		// ƯU TIÊN 2: Khi cả hai giao dịch đều ở Mempool (h1 == 0 && h2 == 0)
		if h1 == 0 && h2 == 0 {
			s1 := entries[i].data.Sender
			s2 := entries[j].data.Sender
			n1 := entries[i].data.Nonce
			n2 := entries[j].data.Nonce
			if s1 == s2 {
				return n1 < n2
			}
			return entries[i].timestamp < entries[j].timestamp
		}

		// ƯU TIÊN 3: Sắp xếp theo chiều cao khối (Block Height) giảm dần (Mới nhất lên trước)
		if h1 != h2 {
			return h1 > h2
		}

		// ƯU TIÊN 4: Nếu nằm trong cùng 1 khối đã chốt, sắp xếp theo Timestamp thực tế giảm dần (mới nhất lên đầu)
		return entries[i].timestamp > entries[j].timestamp
	})

	// 5. [OPTIMIZED] Giới hạn 50 giao dịch gần nhất
	if len(entries) > 50 {
		entries = entries[:50]
	}

	history := make([]TxResponse, len(entries))
	for i, e := range entries {
		history[i] = e.data
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address":  addrStr,
		"filter":   filter,
		"tx_count": len(history),
		"history":  history,
	})
}

func (s *RPCServer) getTxDirection(tx *TrackedTx) string {
	wallets := s.getUserWalletAddresses()
	if wallets[strings.ToLower(strings.TrimPrefix(tx.Sender, "0x"))] {
		return "OUT"
	}
	return "IN"
}

func (s *RPCServer) handleSupply(w http.ResponseWriter, r *http.Request) {
	// [LỖI CẤP S #4 FIX] Lấy Tổng cung thực tế (VNT) bằng cách quét Sổ cái (SCL) thông qua Rust Bridge
	// Loại bỏ hoàn toàn mô phỏng tính toán qua công thức tích phân.
	supplyVNT := s.bridge.CalculateActualTotalSupply()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_supply":  supplyVNT,
		"total_vnt":     supplyVNT,
		"max_supply":    2100000000000000, // 21 triệu * 10^8
		"unit":          "BTC_Z",
	})
}

func (s *RPCServer) handleMinerStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	addr := s.cliApp.GetMinerAddress()
	isMining := s.isMiningActive()

	s.nodeModeMu.RLock()
	mode := s.nodeMode
	s.nodeModeMu.RUnlock()

	h, target, state := s.netMgr.SyncEngine.GetSyncProgress()
	isSynced := s.netMgr.SyncEngine.IsSynced()

	miningWarning := ""
	if mode == "full-mining" && (s.minerAddr == nil || s.cliApp.IsZeroAddress(s.minerAddr)) {
		wallets, _ := s.walletMgr.ListWallets()
		if len(wallets) > 0 {
			miningWarning = "Vui lòng chọn ví nhận thưởng để bắt đầu khai thác"
		} else {
			miningWarning = "Yêu cầu Khôi phục ví (12 từ khóa) để xử lý hệ thống"
		}
	}

	s.gpuEnvErrorMu.RLock()
	gpuErr := s.gpuEnvError
	s.gpuEnvErrorMu.RUnlock()
	if gpuErr != "" {
		if miningWarning != "" {
			miningWarning = gpuErr + " | " + miningWarning
		} else {
			miningWarning = gpuErr
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"hashrate":               atomic.LoadUint64(&s.currentHashrate),
		"is_mining":              isMining,
		"miner_address":          hex.EncodeToString(addr),
		"threads":                runtime.NumCPU(),
		"grace_period_remaining": s.getGracePeriodRemaining(),
		"mining_warning":         miningWarning,
		"is_synced":              isSynced,
		"current_height":         h,
		"target_height":          target,
		"sync_state":             state,
		"node_mode":              mode,
		"mining_device":          s.miningDevice,
	})
}

func (s *RPCServer) getGracePeriodRemaining() float64 {
	// [VANGUARD-GUARD] Luôn hiển thị thời gian chờ bảo vệ 60s khi mới khởi chạy
	gracePeriod := 60 * time.Second
	timeElapsed := time.Since(s.launchTime)

	if timeElapsed < gracePeriod {
		remaining := (gracePeriod - timeElapsed).Seconds()
		if remaining < 0.1 {
			return 0
		}
		return remaining
	}
	return 0
}

func (s *RPCServer) handleMinerToggle(w http.ResponseWriter, r *http.Request) {
	// [VANGUARD-STRICT] Lệnh Giới Nghiêm 90 giây Quét mạng
	remaining := s.getGracePeriodRemaining()
	if remaining > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "Error",
			"error":  fmt.Sprintf("Hệ thống đang quét mạng lưới (Radar Scan). Vui lòng đợi thêm %d giây.", int(remaining)),
		})
		return
	}

	paused := s.bridge.IsMiningPaused()
	if paused {
		// [VANGUARD-SYNC-BLOCK] Chặn cứng không cho phép bật đào khi chưa đồng bộ hoàn tất
		peerCount := s.netMgr.GetPeerCount()
		if peerCount > 0 && s.netMgr.SyncEngine != nil && !s.netMgr.SyncEngine.IsSynced() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "Error",
				"message":    "Hệ thống chưa đồng bộ xong. Vui lòng đợi đồng bộ hoàn tất trước khi bật đào!",
				"error_code": "MINER_BLOCKED_SYNC",
			})
			return
		}

		// [V2.0 SAFETY] Kiểm tra ví đào hợp lệ trước khi bật
		if s.cliApp != nil && !s.cliApp.IsValidMinerAddress() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "Error",
				"message":    "Vui lòng chọn ví hợp lệ trước khi bật đào. Ví hiện tại không hợp lệ (toàn số 0).",
				"error_code": "INVALID_MINER_WALLET",
			})
			return
		}
	}

	// [V37.4 PRO] Thực hiện đảo trạng thái (Toggle) thay vì hardcode false
	newPausedState := !paused
	log.Printf("[MINER-CONTROL] 🕹️ Nhận lệnh từ UI: %s đào (PAUSE = %v)",
		func() string {
			if newPausedState {
				return "TẮT"
			} else {
				return "BẬT"
			}
		}(), newPausedState)
	s.bridge.SetMiningPause(newPausedState)

	// [V24 FORCE] Đồng bộ nodeMode tương ứng
	s.nodeModeMu.Lock()
	if newPausedState {
		s.nodeMode = "verify-only"
		if s.cliApp != nil {
			s.cliApp.SetNodeMode("verify-only")
		}
	} else {
		s.nodeMode = "full-mining"
		if s.cliApp != nil {
			s.cliApp.SetNodeMode("full-mining")
		}
	}
	s.nodeModeMu.Unlock()

	// Tự động kích hoạt hoặc dừng thợ đào tương ứng cấu hình
	if !newPausedState {
		s.updateGpuEnvCheck()
		s.StartConfiguredMiners()
	} else {
		s.StopConfiguredMiners()
	}

	// [VANGUARD-STREAM] Phát sóng lệnh dừng/bắt đầu đào tới các miner gRPC
	s.BroadcastPauseState(newPausedState)

	s.saveNodeConfig() // Lưu lại cấu hình để restart vẫn giữ chế độ đào

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "Success", "is_mining": !newPausedState})
}

// [MINER V2] handleCpuIntensity: API điều chỉnh công suất CPU
func (s *RPCServer) GetCpuIntensity() int {
	s.cpuIntensityMu.RLock()
	defer s.cpuIntensityMu.RUnlock()
	return s.cpuIntensity
}

// GetCurrentHashrate: (V5.6) Trả về hashrate hiện tại được cache bởi bộ giám sát an toàn
func (s *RPCServer) GetCurrentHashrate() uint64 {
	return atomic.LoadUint64(&s.currentHashrate)
}

func (s *RPCServer) handleCpuIntensity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		s.cpuIntensityMu.RLock()
		intensity := s.cpuIntensity
		s.cpuIntensityMu.RUnlock()
		json.NewEncoder(w).Encode(map[string]interface{}{"cpu_intensity": intensity})
		return
	}

	var req struct {
		Intensity int `json:"intensity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid payload"})
		return
	}

	// Chặn các mức không hợp lệ
	if req.Intensity < 1 {
		req.Intensity = 1
	}
	if req.Intensity > 100 {
		req.Intensity = 100
	}

	log.Printf("[RPC-UI] 🎛️ Nhận yêu cầu thay đổi cường độ CPU: %d%% từ %s", req.Intensity, r.RemoteAddr)

	s.cpuIntensityMu.Lock()
	s.cpuIntensity = req.Intensity
	s.cpuIntensityMu.Unlock()

	// [V37.4 FIX] Kích hoạt nạp lại mẫu khối ngay lập tức để áp dụng cường độ mới
	if s.cliApp != nil {
		s.cliApp.RefreshMiningTask()
	}

	// [V1.5 PERSISTENCE] Lưu lại ngay khi thay đổi
	s.saveNodeConfig()

	log.Printf("[MINER] 🎛️ Đã áp dụng vòng tua CPU mới: %d%%", req.Intensity)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "Success", "cpu_intensity": req.Intensity})
}

// getModeDescription: Cung cấp mô tả cho giao diện người dùng dựa trên chế độ hiện tại
// POST: Chuyển đổi chế độ (verify-only / full-mining)
// Tại sao cần: Cho phép frontend điều khiển mức tiêu hao tài nguyên
func (s *RPCServer) handleNodeMode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		s.nodeModeMu.RLock()
		mode := s.nodeMode
		s.nodeModeMu.RUnlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"mode":        mode,
			"is_mining":   s.isMiningActive(),
			"description": s.getModeDescription(mode),
		})
		return
	}

	// POST: Chuyển đổi chế độ
	var req struct {
		Mode string `json:"mode"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// [VANGUARD-SMART-TOGGLE] Nếu không gửi Mode, tự động đảo ngược trạng thái hiện tại
	if req.Mode == "" {
		if s.nodeMode == "full-mining" {
			req.Mode = "verify-only"
		} else {
			req.Mode = "full-mining"
		}
	}

	if req.Mode == "full-mining" && s.netMgr.SyncEngine != nil && !s.netMgr.SyncEngine.IsSynced() {
		log.Printf("[MINER-WARN] ⚠️ CẢNH BÁO AN NINH: Chuyển sang chế độ full-mining khi chưa đồng bộ mạng lưới!")
	}

	switch req.Mode {
	case "verify-only":
		s.nodeModeMu.Lock()
		s.nodeMode = "verify-only"
		if s.cliApp != nil {
			s.cliApp.SetNodeMode("verify-only")
		}
		s.nodeModeMu.Unlock()
		s.updateMiningState()
		log.Printf("[NODE] 🔒 Chuyển sang chế độ CHỈ XÁC MINH (Verify-Only)")

	case "full-mining":
		// [SECURITY LOCKDOWN V1.2.3] Kiểm tra ví chính chủ trước khi bật đào
		validMiner := false
		if len(s.minerAddr) == 32 {
			for _, b := range s.minerAddr {
				if b != 0 {
					validMiner = true
					break
				}
			}
		}

		if !validMiner {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "Error",
				"message":    "Không thể bật Đào: Bạn PHẢI Tạo hoặc Khôi phục ví chính chủ trước!",
				"error_code": "INVALID_MINER_WALLET",
			})
			return
		}

		// [GENESIS DELAY]
		h := s.bridge.GetCurrentVersion()
		if h == 0 {
			genHash := s.bridge.GetBlockHash(0)
			if genHash == nil {
				// Nếu chưa có Genesis, tiến hành đúc khối #0 thông qua Rust Core (V19)
				log.Printf("[VANGUARD] 🚀 Yêu cầu Rust khởi tạo Genesis...")
			}
		}

		s.nodeModeMu.Lock()
		s.nodeMode = "full-mining"
		if s.cliApp != nil {
			s.cliApp.SetNodeMode("full-mining")
		}
		s.nodeModeMu.Unlock()
		s.updateMiningState()
		log.Printf("[NODE] ⛏️ Kích hoạt động cơ Blake3-PoW (Full Mining)")

	default:
		http.Error(w, "Mode không hợp lệ. Chấp nhận: verify-only, full-mining", http.StatusBadRequest)
		return
	}

	// [V1.5 PERSISTENCE] Lưu lại ngay khi thay đổi
	s.saveNodeConfig()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "Success",
		"mode":        req.Mode,
		"is_mining":   s.isMiningActive(),
		"description": s.getModeDescription(req.Mode),
	})
}

func (s *RPCServer) getModeDescription(mode string) string {
	switch mode {
	case "verify-only":
		return "Node chỉ xác minh block từ mạng lưới. Không tiêu hao CPU cho PoW. Phù hợp thiết bị yếu."
	case "full-mining":
		return "Node đào Blake3-PoW song song với xác minh. Sử dụng 100% năng lực CPU."
	}
	return "Không xác định"
}

// recordHashrate: Ghi nhận hashrate vào ring buffer (gọi từ SSE loop)
func (s *RPCServer) recordHashrate(rate float64) {
	s.hashrateHistMu.Lock()
	defer s.hashrateHistMu.Unlock()
	s.hashrateHistory[s.hashrateHistIdx] = rate
	s.hashrateHistIdx = (s.hashrateHistIdx + 1) % 60
	if s.hashrateHistCount < 60 {
		s.hashrateHistCount++
	}
}

// getHashrateHistory: Trả về lịch sử hashrate theo thứ tự thời gian
func (s *RPCServer) getHashrateHistory() []float64 {
	s.hashrateHistMu.RLock()
	defer s.hashrateHistMu.RUnlock()

	result := make([]float64, s.hashrateHistCount)
	start := 0
	if s.hashrateHistCount >= 60 {
		start = s.hashrateHistIdx // Ring buffer đã đầy, bắt đầu từ vị trí hiện tại
	}
	for i := 0; i < s.hashrateHistCount; i++ {
		result[i] = s.hashrateHistory[(start+i)%60]
	}
	return result
}

// getNetworkHashrateHistory: Trả về lịch sử hashrate toàn mạng của 20 khối gần nhất
func (s *RPCServer) getNetworkHashrateHistory() []uint64 {
	highest := s.bridge.GetCurrentVersion()
	count := uint64(20)
	if highest < count {
		count = highest
	}
	if count == 0 {
		return []uint64{16000000} // Mặc định 16 MH/s
	}

	result := make([]uint64, count)
	avgBlockTime := s.calculateAvgBlockTime()
	if avgBlockTime <= 0 {
		avgBlockTime = 75.0
	}

	// Lặp qua 20 khối gần nhất
	for i := uint64(0); i < count; i++ {
		h := highest - (count - 1 - i)
		hash := s.bridge.GetBlockHash(h)
		difficulty := big.NewInt(1200000000)
		if len(hash) > 0 {
			headerRaw := s.bridge.GetHeaderRaw(hash)
			if headerRaw != nil {
				var header pb_block.BlockHeader
				if err := proto.Unmarshal(headerRaw, &header); err == nil {
					difficulty = go_bridge.BytesLEToBigInt(header.Difficulty)
				}
			}
		}

		// Network Hashrate = Difficulty / AvgBlockTime (hoặc 75 nếu block time lỗi)
		hashrate := new(big.Int).Div(difficulty, big.NewInt(int64(avgBlockTime)))
		result[i] = hashrate.Uint64()
	}
	return result
}

// getTopMiners: Tính toán danh sách các địa chỉ ví thợ đào có tốc độ đào cao nhất trong 100 khối gần nhất
func (s *RPCServer) getTopMiners() []MinerStats {
	highest := s.bridge.GetCurrentVersion()
	window := uint64(100)
	if highest < window {
		window = highest
	}
	if window == 0 {
		return []MinerStats{}
	}

	minerCounts := make(map[string]int)
	totalBlocks := 0

	for h := highest; h > highest - window; h-- {
		blockRaw := s.bridge.GetBlock(h)
		if blockRaw == nil {
			continue
		}
		var block pb_block.Block
		if err := proto.Unmarshal(blockRaw, &block); err == nil && block.Body != nil {
			if len(block.Body.Transactions) > 0 {
				coinbaseTx := block.Body.Transactions[0]
				if s.isTxCoinbase(coinbaseTx) && coinbaseTx.Receiver != nil {
					minerAddr := "0x" + strings.ToLower(hex.EncodeToString(coinbaseTx.Receiver.Value))
					minerCounts[minerAddr]++
					totalBlocks++
				}
			}
		}
	}

	if totalBlocks == 0 {
		return []MinerStats{}
	}

	networkHashrate := s.calculateNetworkHashrate()

	var stats []MinerStats
	for addr, count := range minerCounts {
		percentage := (float64(count) / float64(totalBlocks)) * 100.0
		hashrateEst := uint64((float64(count) / float64(totalBlocks)) * float64(networkHashrate))
		stats = append(stats, MinerStats{
			Address:     addr,
			BlocksMined: count,
			Percentage:  percentage,
			HashrateEst: hashrateEst,
		})
	}

	// Sắp xếp giảm dần theo số khối đào được (tức là hashrate ước tính)
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].BlocksMined > stats[j].BlocksMined
	})

	return stats
}



// ----------------------------------------------------------------------------
// Nhóm hàm Web UI & Wallet (Integrated)
// ----------------------------------------------------------------------------

func (s *RPCServer) handleHome(w http.ResponseWriter, r *http.Request) {
	// [VANGUARD-RESTORE] Ưu tiên sử dụng bản build React từ ổ đĩa để đồng bộ với Assets
	localIndex := "6_user_interface/web_ui/dist/index.html"
	if _, err := os.Stat(localIndex); err != nil {
		if exePath, err := os.Executable(); err == nil {
			altPath := filepath.Join(filepath.Dir(filepath.Dir(exePath)), localIndex)
			if _, err := os.Stat(altPath); err == nil {
				localIndex = altPath
			}
		}
	}

	if data, err := os.ReadFile(localIndex); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
		return
	} else {
		log.Printf("[UI-DEBUG] ⚠️ Không thể đọc index.html từ đĩa (%s): %v", localIndex, err)
	}

	// Nếu không có trên đĩa, dùng bản build React chính thức từ Embed FS
	distFS, _ := fs.Sub(staticFiles, "web_ui/dist")
	html, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		log.Printf("[UI-ERROR] ❌ Không thể đọc index.html từ Embed FS: %v", err)
		http.Error(w, "Giao diện React YonaCode Go không tìm thấy!", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}

func (s *RPCServer) handleWalletCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	mnemonic, addr, err := s.walletMgr.CreateWallet(req.Name, req.Password, req.Passphrase)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"mnemonic": mnemonic, "address": "0x" + addr})

	// [V4.2 AUTOMATION] Kích hoạt quét lại lịch sử để cập nhật các giao dịch cho ví mới
	go s.recoverHistory()
}

func (s *RPCServer) handleWalletRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mnemonic   string `json:"mnemonic"`
		Name       string `json:"name"`
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	addr, err := s.walletMgr.RestoreWallet(req.Mnemonic, req.Name, req.Password, req.Passphrase)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"address": "0x" + addr})
}

func (s *RPCServer) handleWalletPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mnemonic   string `json:"mnemonic"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	addr, _ := s.walletMgr.DeriveAddressOnly(req.Mnemonic, req.Passphrase)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address": addr,
		"valid":   bip39.IsMnemonicValid(req.Mnemonic),
	})
}

func (s *RPCServer) handleWalletList(w http.ResponseWriter, r *http.Request) {
	wallets, _ := s.walletMgr.ListWallets()

	// [V3.0 TACTICAL] Tự động thêm Ví Đào (Miner Reward) vào danh sách để UI hiển thị số dư
	minerAddrHex := hex.EncodeToString(s.minerAddr)
	exists := false
	for _, wal := range wallets {
		if wal.Address == minerAddrHex {
			exists = true
			break
		}
	}

	if !exists {
		// Chèn vào đầu danh sách để người dùng dễ thấy nhất
		minerWallet := internal.Wallet{
			Name:    "Ví Đào (Miner Reward)",
			Address: minerAddrHex,
		}
		wallets = append([]internal.Wallet{minerWallet}, wallets...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wallets)
}

func (s *RPCServer) handleWalletDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := s.walletMgr.DeleteWallet(req.Address)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Ví đã được xóa thành công cục bộ!"})
}

func (s *RPCServer) handleFeeCalculate(w http.ResponseWriter, r *http.Request) {
	amountStr := r.URL.Query().Get("amount")
	amountFloat, _ := strconv.ParseFloat(amountStr, 64)
	amountVNT := uint64(amountFloat * 100_000_000)

	// [V1.3.1 - PHỤ LỤC H] Lấy phí khuyến nghị dựa trên độ tắc nghẽn của Mempool
	recommended := s.netMgr.GetMempool().GetRecommendedFee(amountVNT)

	// Trả về phí dựa trên lựa chọn người dùng (250/500/1000)
	baseFeeStr := r.URL.Query().Get("base_fee")
	baseFee, _ := strconv.ParseUint(baseFeeStr, 10, 64)
	if baseFee == 0 {
		baseFee = recommended // Mặc định dùng phí khuyến nghị nếu chưa chọn
	}

	// [V1.19 - HOTFIX] Mọi giao dịch bình đẳng. Loại bỏ Anti-Spam floor 500 VNT cho Nano-dust.

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]uint64{
		"fee":         baseFee,
		"recommended": recommended,
	})
}

func (s *RPCServer) GetBlock(ctx context.Context, req *pb_block.GetBlockRequest) (*pb_block.GetBlockResponse, error) {
	blockRaw := s.bridge.GetBlock(req.Height)
	if blockRaw == nil {
		return &pb_block.GetBlockResponse{Found: false}, nil
	}

	var block pb_block.Block
	if err := proto.Unmarshal(blockRaw, &block); err != nil {
		return &pb_block.GetBlockResponse{Found: false}, nil
	}

	return &pb_block.GetBlockResponse{Found: true, Block: &block}, nil
}

func (s *RPCServer) GetHeaderBatch(ctx context.Context, req *pb_block.GetHeaderBatchRequest) (*pb_block.GetHeaderBatchResponse, error) {
	headers := make([][]byte, 0, req.Count)
	for i := uint32(0); i < req.Count; i++ {
		hash := s.bridge.GetBlockHash(req.StartHeight + uint64(i))
		if hash != nil {
			headerRaw := s.bridge.GetHeaderRaw(hash)
			if headerRaw != nil {
				headers = append(headers, headerRaw)
			}
		}
	}
	return &pb_block.GetHeaderBatchResponse{Headers: headers}, nil
}

func (s *RPCServer) SubmitTransaction(ctx context.Context, tx *pb_block.Transaction) (*pb_block.Hash, error) {
	// [LAYER 1 CARDINAL] Cảnh vệ Vòng Ngoài (Input Validation)
	if tx == nil {
		return nil, fmt.Errorf("Giao dịch không hợp lệ (Null Payload)")
	}
	if tx.Sender == nil || len(tx.Sender.Value) != 32 || tx.Receiver == nil || len(tx.Receiver.Value) != 32 {
		return nil, fmt.Errorf("Địa chỉ Ví (Sender/Receiver) sai định dạng hoặc không chuẩn Ed25519")
	}

	// [LAYER 3 SECURITY] Chống khủng bố (Anti-Overflow & Zero-spam)
	if tx.Amount <= 0 || tx.Amount > 2100000000000000 {
		// [LAYER 4 INTEL] Tình báo lưu dấu vết tấn công
		audit.AuditLog("INTEGER_OVERFLOW_ATTEMPT", "local", fmt.Sprintf("Chặn đứng nỗ lực tấn công tràn số: Amount=%d", tx.Amount))
		return nil, fmt.Errorf("Khối lượng giao dịch (Amount) bằng Không hoặc Vượt quá Tổng cung")
	}

	data, _ := tx.MarshalVT()

	// [SECURITY-FIX] Phát sóng giao dịch được di chuyển xuống SAU khi xác thực chữ ký + số dư + nạp Mempool.
	// Tại sao: Phiên bản cũ phát sóng TRƯỚC khi xác thực, cho phép kẻ tấn công spam giao dịch giả
	// lên toàn mạng P2P mà không cần chữ ký hợp lệ.

	// 2. Đưa vào Mempool cục bộ để thợ đào xử lý ngay
	nextH := s.bridge.GetCurrentVersion() + 1
	h := s.bridge.GetCanonicalTxHash(data, nextH)
	txHashStr := hex.EncodeToString(h)
	senderHex := ""
	if tx.Sender != nil {
		senderHex = hex.EncodeToString(tx.Sender.Value)
	}
	receiverHex := ""
	if tx.Receiver != nil {
		receiverHex = hex.EncodeToString(tx.Receiver.Value)
	}

	// [FORENSIC-AUDIT V13.6] Đưa giao dịch vào TxBus của Mempool để xác thực song song hàng loạt
	log.Printf("[AUDIT] 🛡️ Xếp hàng giao dịch %s vào TxBus...", txHashStr[:10])

	if s.netMgr.Mempool != nil {
		success := s.netMgr.Mempool.PushToTxBus(data, true)
		if !success {
			err := fmt.Errorf("mempool busy or TxBus full")
			log.Printf("[AUDIT] ❌ THẤT BẠI: TxBus từ chối - %v", err)
			s.updateTxTracker(txHashStr, senderHex, receiverHex, tx.Amount, tx.Fee, tx.Nonce, 0, time.Now().Unix(), err.Error())
			return nil, err
		}

		log.Printf("[RPC] ✅ Giao dịch %s đã được xếp hàng thành công vào TxBus", txHashStr[:10])
	}

	// [EBP-SINGLE-IMMEDIATE] Phát sóng trực tiếp lên mạng Gossip P2P mà không trì hoãn 100ms
	if s.netMgr != nil {
		if err := s.netMgr.BroadcastTransaction(data); err != nil {
			log.Printf("[RPC-SEND] ⚠️ Phát sóng trực tiếp thất bại: %v", err)
		}
	}

	// Cập nhật Tracker trạng thái PENDING
	s.updateTxTracker(txHashStr, senderHex, receiverHex, tx.Amount, tx.Fee, tx.Nonce, 0, time.Now().Unix(), "")

	return &pb_block.Hash{Value: h}, nil
}


func (s *RPCServer) GetStatus(ctx context.Context, req *pb_block.GetStatusRequest) (*pb_block.GetStatusResponse, error) {
	h := s.bridge.GetCurrentVersion()
	f := s.netMgr.SyncEngine.GetFinalizedHeight()

	return &pb_block.GetStatusResponse{
		CurrentHeight:   h,
		FinalizedHeight: f,
		Hashrate:        atomic.LoadUint64(&s.currentHashrate),
		IsMining:        s.isMiningActive(),
		Version:         "YonaCode Go V1.2.1-Ready",
		PeerCount:       uint32(s.netMgr.GetPeerCount()),
		OldestHeight:    s.bridge.GetOldestHeight(),
	}, nil
}

func (s *RPCServer) GetAccount(ctx context.Context, req *pb_block.GetAccountRequest) (*pb_block.AccountResponse, error) {
	if req == nil || len(req.Address) != 32 {
		return nil, fmt.Errorf("Địa chỉ không hợp lệ")
	}

	balance := s.bridge.GetBalance(nil, req.Address, 0)
	nonce := s.bridge.GetNonce(nil, req.Address)

	return &pb_block.AccountResponse{
		Balance: balance,
		Nonce:   nonce,
		Address: req.Address,
	}, nil
}

// CalculateBlockHeaderHash: (V1.5) Proxy tính toán hash cho CLI
func (s *RPCServer) CalculateBlockHeaderHash(ctx context.Context, req *pb_block.RawBytes) (*pb_block.Hash, error) {
	if req == nil || len(req.Data) == 0 {
		return nil, fmt.Errorf("Dữ liệu Header không hợp lệ")
	}

	h := s.bridge.CalculateBlockHeaderHash(req.Data)
	return &pb_block.Hash{Value: h}, nil
}

// [SECURITY-HARDENING] verifyAuthToken: Trích xuất x-auth-token trong metadata gRPC và so khớp với token của Node.
// Trả về error Unauthenticated/PermissionDenied nếu không khớp.
func (s *RPCServer) verifyAuthToken(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "Unauthorized: Missing gRPC metadata")
	}
	tokens := md.Get("x-auth-token")
	if len(tokens) == 0 {
		return status.Errorf(codes.Unauthenticated, "Unauthorized: Missing auth token")
	}
	expected := s.bridge.GetAuthToken()
	if tokens[0] != expected {
		return status.Errorf(codes.PermissionDenied, "Unauthorized: Invalid auth token")
	}
	return nil
}

// [MINING] Kích hoạt hỏa lực đào khối từ xa qua gRPC
func (s *RPCServer) StartMining(ctx context.Context, req *pb_block.StartMiningRequest) (*pb_block.MiningResponse, error) {
	if err := s.verifyAuthToken(ctx); err != nil {
		log.Printf("[SECURITY] 🚨 Từ chối lệnh StartMining từ CLI: %v", err)
		return nil, err
	}
	log.Printf("[RPC-GRPC] ⛏️ Nhận lệnh KHỞI HỎA từ CLI: Address=%s, Threads=%d", req.MinerAddress, req.Threads)

	addr, err := hex.DecodeString(strings.TrimPrefix(req.MinerAddress, "0x"))
	if err != nil || len(addr) != 32 {
		return &pb_block.MiningResponse{Success: false, Message: "Địa chỉ ví không hợp lệ (Phải là 32 bytes hex)"}, nil
	}

	// Cập nhật ví đào và kích hoạt cầu dao băm khối
	s.minerAddr = addr
	s.nodeModeMu.Lock()
	s.nodeMode = "full-mining"
	s.nodeModeMu.Unlock()
	s.bridge.SetMiningPause(false)

	// Đồng bộ hóa trạng thái với CLIApp nội bộ (nếu có)
	if s.cliApp != nil {
		s.cliApp.SetMinerAddress(addr, nil, "")
		s.cliApp.SetNodeMode("full-mining")
	}

	return &pb_block.MiningResponse{Success: true, Message: "Đã kích hoạt hỏa lực đào khối thành công!"}, nil
}

// [MINING] Ngừng khai thác từ xa qua gRPC
func (s *RPCServer) StopMining(ctx context.Context, req *pb_block.StopMiningRequest) (*pb_block.MiningResponse, error) {
	if err := s.verifyAuthToken(ctx); err != nil {
		log.Printf("[SECURITY] 🚨 Từ chối lệnh StopMining từ CLI: %v", err)
		return nil, err
	}
	log.Printf("[RPC-GRPC] 🛑 Nhận lệnh NGỪNG KHAI HỎA từ CLI")

	s.nodeModeMu.Lock()
	s.nodeMode = "verify-only"
	s.nodeModeMu.Unlock()
	s.bridge.SetMiningPause(true)

	if s.cliApp != nil {
		s.cliApp.SetNodeMode("verify-only")
	}

	return &pb_block.MiningResponse{Success: true, Message: "Đã dừng đào khối thành công!"}, nil
}

func (s *RPCServer) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Printf("[SSE] 📡 Client mới kết nối tới WatchStatus")

	// Lưu trạng thái trước đó của client này để phát hiện finality mới
	var prevFinalizedH uint64
	var prevHeight uint64
	var lastBroadcast time.Time

	for {
		select {
		case <-ticker.C:
			s.broadcastStatusUpdate(w, &prevFinalizedH, &prevHeight)
			lastBroadcast = time.Now()
		case <-s.txUpdateChan:
			s.broadcastStatusUpdate(w, &prevFinalizedH, &prevHeight)
			lastBroadcast = time.Now()
		case <-s.blockUpdateChan:
			// [ULTRA-REALTIME-FIX] Đánh thức cực nhanh khi sync hàng trăm khối/giây
			// Giới hạn 30ms (33fps) để UI mượt mà nhưng không làm treo trình duyệt
			if time.Since(lastBroadcast) > 30*time.Millisecond {
				// Clear tín hiệu thừa
				for len(s.blockUpdateChan) > 0 {
					<-s.blockUpdateChan
				}
				s.broadcastStatusUpdate(w, &prevFinalizedH, &prevHeight)
				lastBroadcast = time.Now()
			}
		case <-r.Context().Done():
			log.Printf("[SSE] 🔌 Client đã ngắt kết nối")
			return
		}
	}
}

// broadcastStatusUpdate: Đóng gói logic phát trạng thái để dùng chung cho Ticker và Event-trigger
func (s *RPCServer) broadcastStatusUpdate(w http.ResponseWriter, prevFinalizedH, prevHeight *uint64) {
	height := s.bridge.GetCurrentVersion()
	fH := s.netMgr.SyncEngine.GetFinalizedHeight()

	// [FINALITY-UI V2] Phát hiện giao dịch mới đạt Finality
	var newlyFinalized []string
	if fH > *prevFinalizedH || height > *prevHeight {
		s.txTrackerMu.RLock()
		for _, tx := range s.txTracker {
			if tx.BlockHeight == 0 || tx.IsFinalized { // [OPTIMIZED] Bỏ qua tx đã Finalized
				continue
			}
			var confs uint64
			if height >= tx.BlockHeight {
				confs = height - tx.BlockHeight + 1
			}
			if confs >= 6 {
				newlyFinalized = append(newlyFinalized, tx.TxID)
				tx.IsFinalized = true // Đánh dấu luôn để lần loop sau bỏ qua
			}
		}
		s.txTrackerMu.RUnlock()
	}

	*prevFinalizedH = fH
	*prevHeight = height

	s.txTrackerMu.RLock()
	pendingCount := 0
	for _, tx := range s.txTracker {
		if tx.BlockHeight == 0 {
			pendingCount++
		}
	}
	s.txTrackerMu.RUnlock()

	// [FIX-V5.5] Lấy hashrate đã được tính toán bởi bộ giám sát an toàn (Single Source of Truth)
	// Để tránh tranh chấp reset bộ đếm trong lõi Rust (swap(0)).
	calculatedRate := float64(atomic.LoadUint64(&s.currentHashrate))

	s.nodeModeMu.RLock()
	currentMode := s.nodeMode
	s.nodeModeMu.RUnlock()

	s.cpuIntensityMu.RLock()
	currentCpu := s.cpuIntensity
	s.cpuIntensityMu.RUnlock()

	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	// [TARGET-HEIGHT-FIX] Lấy chiều cao mục tiêu từ SyncEngine và đối soát
	var targetHeight uint64
	var syncState = "Stalled"
	var chunksLoaded, chunksTotal uint32
	if s.netMgr.SyncEngine != nil {
		_, targetHeight, syncState = s.netMgr.SyncEngine.GetSyncProgress()
		chunksLoaded, chunksTotal = s.netMgr.SyncEngine.GetSnapshotProgress()
	}
	if netH := s.netMgr.GetNetworkHeight(); netH > targetHeight {
		targetHeight = netH
	}
	if height > targetHeight {
		targetHeight = height
	}

	// [UX-WARNING] Tính toán cảnh báo thợ đào chưa chọn ví nhận thưởng đào
	miningWarning := ""
	if currentMode == "full-mining" && (s.minerAddr == nil || (s.cliApp != nil && s.cliApp.IsZeroAddress(s.minerAddr))) {
		wallets, _ := s.walletMgr.ListWallets()
		if len(wallets) > 0 {
			miningWarning = "Vui lòng chọn ví nhận thưởng để bắt đầu khai thác"
		} else {
			miningWarning = "Yêu cầu Khôi phục ví (12 từ khóa) để xử lý hệ thống"
		}
	}

	s.gpuEnvErrorMu.RLock()
	gpuErr := s.gpuEnvError
	s.gpuEnvErrorMu.RUnlock()
	if gpuErr != "" {
		if miningWarning != "" {
			miningWarning = gpuErr + " | " + miningWarning
		} else {
			miningWarning = gpuErr
		}
	}

	downloading := uint64(0)
	if s.netMgr.SyncEngine != nil {
		downloading = s.netMgr.SyncEngine.GetDownloadingHeight()
	}

	resp := map[string]interface{}{
		"current_height":         height,
		"target_height":          targetHeight,
		"sync_state":             syncState,
		"sync_executing":         s.bridge.IsSyncing(),
		"sync_downloading":       downloading,
		"snapshot_chunks_loaded": chunksLoaded,
		"snapshot_chunks_total":  chunksTotal,
		"finalized_height":       fH,
		"timestamp":              time.Now().Unix(),
		"hashrate":               calculatedRate,
		"hashrate_history":       s.getHashrateHistory(),
		"network_hashrate":       s.calculateNetworkHashrate(),
		"network_hashrate_history": s.getNetworkHashrateHistory(),
		"top_miners":             s.getTopMiners(),
		"is_mining":              s.isMiningActive(),
		"peer_count":             s.netMgr.GetPeerCount(),
		"pending_tx_count":       pendingCount,
		"block_reward":           float64(s.bridge.CalculateBlockRewardBtcZ(height)) / 1e8,
		"network_difficulty":     getDifficulty(s.bridge),
		"difficulty":             getDifficulty(s.bridge), // Đáp ứng trường difficulty mong đợi trong api.ts
		"total_supply":           float64(s.bridge.GetActualTotalSupply()) / 1e8,
		"node_mode":              currentMode,
		"cpu_intensity":          currentCpu,
		"mining_device":          device,
		"oldest_height":          s.bridge.GetOldestHeight(),
		"version":                "YonaCode Go V1.2.1-Ready",
		"avg_block_time":         s.calculateAvgBlockTime(),
		"grace_period_remaining": s.getGracePeriodRemaining(),
		"mining_warning":         miningWarning,
		"bandwidth": func() map[string]interface{} {
			sent := atomic.LoadUint64(&s.netMgr.BytesSent)
			recv := atomic.LoadUint64(&s.netMgr.BytesRecv)
			if s.netMgr.Bwc != nil {
				stats := s.netMgr.Bwc.GetBandwidthTotals()
				sent = uint64(stats.TotalOut)
				recv = uint64(stats.TotalIn)
			}
			return map[string]interface{}{
				"sent": sent,
				"recv": recv,
			}
		}(),
	}
	jsonData, _ := json.Marshal(resp)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *RPCServer) openBrowser(url string) {
	log.Printf("[UX] 🌐 Đang tự động mở trình duyệt tại: %s", url)
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "linux":
		err = exec.Command("xdg-open", url).Start() // Tiêu chuẩn của Linux Desktop
	case "darwin": // macOS
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("hệ điều hành %s không hỗ trợ mở trình duyệt tự động", runtime.GOOS)
	}

	if err != nil {
		log.Printf("[UX] ⚠️ Không thể mở trình duyệt tự động (Có thể bạn đang chạy trên VPS không có giao diện đồ họa): %v", err)
	}
}

// [V11.2] Middlewares để chẩn đoán kết nối
func (s *RPCServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// [ANTI-SPAM-LOG] Tắt log audit HTTP để tránh nghẽn I/O terminal Windows dưới tải cao
		// log.Printf("[RPC-AUDIT] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func (s *RPCServer) PanicRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[RPC-PANIC] 🛡️ Đã đánh chặn lỗi sập nguồn từ Request %s: %v", r.URL.Path, err)
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"status":  "Error",
					"message": "Internal Server Error (Panic Isolated)",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *RPCServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// [SECURITY-HARDENING] Chỉ cho phép CORS từ localhost hoặc 127.0.0.1 để chống tấn công CSRF thông qua trình duyệt
			valid := false
			u, err := url.Parse(origin)
			if err == nil {
				host := u.Hostname()
				if host == "localhost" || host == "127.0.0.1" || host == "::1" {
					valid = true
				}
			}
			if valid || s.cliApp.walletServerEnabled {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				log.Printf("[RPC-CORS] 🚨 CHẶN yêu cầu CORS không hợp lệ từ Origin: %s cho %s", origin, r.URL.Path)
				http.Error(w, "CORS Forbidden", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Origin, Cache-Control, X-Requested-With, X-Wallet-Token")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			log.Printf("[RPC-CORS] 🛡️ Đã xử lý OPTIONS Preflight cho %s", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// [SECURITY-HARDENING] localhostOnly: Middleware bảo vệ các API nhạy cảm
// Tại sao: Ngăn chặn kẻ tấn công từ Internet truy cập vào các chức năng điều khiển Node
// (bật/tắt đào, đổi ví miner, đổi chế độ node, CPU intensity)
// Chỉ cho phép request từ 127.0.0.1 hoặc ::1 (localhost)
func (s *RPCServer) localhostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Kiểm tra IP vật lý (Localhost IP)
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host != "127.0.0.1" && host != "::1" && host != "[::1]" {
			audit.AuditLog("UNAUTHORIZED_ADMIN_ACCESS_ATTEMPT", r.RemoteAddr, fmt.Sprintf("CHẶN truy cập API nhạy cảm tới %s (Không phải Localhost IP)", r.URL.Path))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "Forbidden",
				"message": "API này chỉ được phép truy cập từ localhost (127.0.0.1)",
			})
			return
		}

		// 2. CHỐT CHẶN REVERSE PROXY BYPASS (NGĂN NGINX/APACHE)
		// Nếu request có các header này, 100% nó được forward từ ngoài internet vào
		if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" || r.Header.Get("Forwarded") != "" {
			log.Printf("[SECURITY] 🚨 Phát hiện nỗ lực Bypass qua Reverse Proxy tới API nhạy cảm!")
			audit.AuditLog("UNAUTHORIZED_ADMIN_PROXY_BYPASS", r.RemoteAddr, fmt.Sprintf("CHẶN truy cập qua Reverse Proxy tới %s", r.URL.Path))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "Forbidden",
				"message": "Cấm truy cập API nhạy cảm qua Proxy",
			})
			return
		}

		next(w, r)
	}
}

// [V11.6] Middleware bảo vệ máy chủ khỏi Panic
func (s *RPCServer) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[CRITICAL-PANIC] 💀 Phát hiện sự cố nghiêm trọng tại %s: %v", r.URL.Path, err)
				debug.PrintStack()
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"status": "Panic", "message": fmt.Sprintf("%v", err)})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// [V4.4 ECONOMY] API kiểm tra tính hợp lệ của số dư dựa trên Bản đồ phát hành 300 năm
func (s *RPCServer) handleDebugVerifyBalances(w http.ResponseWriter, r *http.Request) {
	highest := s.bridge.GetCurrentVersion()
	totalSupply := s.bridge.CalculateActualTotalSupply()

	theoreticalSupply := s.bridge.CalculateExpectedSupply(highest)

	actualReward := s.bridge.CalculateBlockRewardBtcZ(highest)

	diff := int64(totalSupply) - int64(theoreticalSupply)
	absDiff := diff
	if diff < 0 {
		absDiff = -diff
	}

	resp := map[string]interface{}{
		"height":              highest,
		"actual_total_supply": totalSupply,
		"theoretical_supply":  theoreticalSupply,
		"actual_reward_now":   actualReward,
		"diff":                diff,
		"abs_diff":            absDiff,
		"is_valid":            diff == 0, // [VANGUARD-STRICT] Tuyệt đối không chấp nhận sai lệch (Bit-perfect required)
		"status":              "Phân tích Cung tiền 300 năm hoàn tất.",
	}

	if diff == 0 {
		resp["status"] = "✅ TUYỆT ĐỐI KHỚP: Cán cân kinh tế GenZ đạt trạng thái hoàn hảo."
	} else {
		resp["status"] = fmt.Sprintf("🚨 CẢNH BÁO: Phát hiện sai lệch cung tiền (%d VNT)! Hệ thống đã mất tính toàn vẹn.", diff)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// AmountToVNT: (V4.5 ELITE) Chuyển đổi chuỗi số tiền BTC_Z sang uint64 VNT liêm chính.
// Cấm sử dụng float64 để tránh lỗi làm tròn.
func (s *RPCServer) AmountToVNT(val string) (uint64, error) {
	if val == "" {
		return 0, fmt.Errorf("empty amount")
	}

	val = strings.TrimSpace(val)
	parts := strings.Split(val, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid format")
	}

	// 1. Phần nguyên
	integerPart, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	res := integerPart * 100_000_000
	// Kiểm tra tràn số (integer overflow) phép nhân phần nguyên
	if integerPart > 0 && res/100_000_000 != integerPart {
		return 0, fmt.Errorf("amount overflow")
	}

	// 2. Phần thập phân (tối đa 8 chữ số)
	if len(parts) == 2 {
		decStr := parts[1]
		if len(decStr) > 8 {
			decStr = decStr[:8] // Chặt cụt nếu quá 8 chữ số, không làm tròn
		} else {
			// Bù số 0 cho đủ 8 chữ số
			for len(decStr) < 8 {
				decStr += "0"
			}
		}
		decimalPart, err := strconv.ParseUint(decStr, 10, 64)
		if err != nil {
			return 0, err
		}
		// Kiểm tra tràn số (integer overflow) phép cộng gộp phần thập phân
		if res+decimalPart < res {
			return 0, fmt.Errorf("amount overflow")
		}
		res += decimalPart
	}

	return res, nil
}

func (s *RPCServer) startHashrateMonitor() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.minerStreamsMu.RLock()
		var grpcRate uint64
		for _, hr := range s.minerHashrates {
			grpcRate += hr
		}
		hasActiveMiners := len(s.minerStreams) > 0
		s.minerStreamsMu.RUnlock()

		s.miningDeviceMu.RLock()
		device := s.miningDevice
		s.miningDeviceMu.RUnlock()

		var cpuRate uint64
		if device == "cpu" || device == "hybrid" {
			if !hasActiveMiners {
				cpuRate = s.bridge.GetHashrate()
			}
		}
		atomic.StoreUint64(&s.cpuHashrate, cpuRate)

		// Check if GPU hashrate has timed out (10s)
		gpuRate := atomic.LoadUint64(&s.gpuHashrate)
		if gpuRate > 0 && time.Since(s.lastGpuHashTime) > 10*time.Second {
			gpuRate = 0
			atomic.StoreUint64(&s.gpuHashrate, 0)
		}

		// Tổng hashrate = Giao thức gRPC (Các máy đào CPU ngoài/cục bộ) + CPU FFI + GPU Miner (REST)
		totalRate := grpcRate + cpuRate + gpuRate
		atomic.StoreUint64(&s.currentHashrate, totalRate)
		
		s.recordHashrate(float64(totalRate)) // Ghi nhận hashrate định kỳ vào ring buffer cho biểu đồ Web UI
	}
}

// [PURGE-MEMPOOL] handleMempoolPurge: Xóa sạch các giao dịch kẹt trong Mempool (RAM & RocksDB)
func (s *RPCServer) handleMempoolPurge(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PURGE-MEMPOOL] 🚨 Nhận lệnh xóa sạch mempool từ localhost")
	s.netMgr.Mempool.Purge()

	// [VANGUARD-CLEANUP] Dọn dẹp các giao dịch pending (BlockHeight == 0) trong UI Tracker
	// giúp giao diện người dùng hoàn toàn sạch sẽ, không hiển thị rác "Bị từ chối" sau khi Reset/Purge.
	s.txTrackerMu.Lock()
	var newOrder []string
	for _, txid := range s.txOrder {
		if tx, exists := s.txTracker[txid]; exists {
			if tx.BlockHeight == 0 {
				delete(s.txTracker, txid)
			} else {
				newOrder = append(newOrder, txid)
			}
		}
	}
	s.txOrder = newOrder
	s.txTrackerMu.Unlock()
	s.triggerSave()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "Success",
		"message": "Đã dọn dẹp sạch sẽ Mempool (RAM & RocksDB) và làm sạch UI Tracker thành công.",
	})
}

// [PURGE] handlePurgeData: Xóa sạch dữ liệu Node (Hard Reset) khi bị kẹt
func (s *RPCServer) handlePurgeData(w http.ResponseWriter, r *http.Request) {
	// 1. Kiểm tra mã xác nhận trong body
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Yêu cầu không hợp lệ", http.StatusBadRequest)
		return
	}

	// Yêu cầu mã xác nhận đúng như : 01900
	// Ghi chú: Mã "01900" ở đây hoạt động tương tự như việc GitHub yêu cầu gõ chữ "DELETE"
	// trước khi xóa một repository. Nó là một bước xác nhận (Confirmation PIN) trên giao diện Web UI
	// để ngăn chặn người dùng vô tình bấm nhầm nút "Xóa dữ liệu" hoặc "Reset".
	if req.Code != "01900" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Mã xác nhận sai! Vui lòng nhập đúng mã '01900' để thực hiện xóa dữ liệu.",
		})
		return
	}

	log.Printf("[PURGE] 🚨 NHẬN LỆNH XÓA DỮ LIỆU TỪ UI (Xác nhận bởi 01900)")

	// 2. Dừng Bridge để giải phóng RocksDB
	if s.bridge != nil {
		log.Printf("[PURGE] 🛑 Đang đóng gRPC Bridge...")
		s.bridge.Close()
	}

	// 3. Xóa thư mục data
	// dbPath thường là "data" hoặc đường dẫn tuyệt đối tới folder chứa DB
	dataDir := s.dbPath
	log.Printf("[PURGE] 🧨 Đang quét sạch toàn bộ dữ liệu tại: %s", dataDir)

	// Thực hiện xóa (Phải cực kỳ cẩn thận)
	err := os.RemoveAll(dataDir)
	if err != nil {
		log.Printf("[PURGE] ❌ Lỗi xóa dữ liệu: %v", err)
		http.Error(w, fmt.Sprintf("Lỗi xóa dữ liệu: %v", err), http.StatusInternalServerError)
		return
	}

	// 4. Phản hồi thành công trước khi thoát
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Toàn bộ dữ liệu đã được quét sạch. Node sẽ tự động thoát sau 2 giây. Vui lòng khởi động lại thủ công.",
	})

	// 5. Tự động thoát sau một khoảng thời gian ngắn để UI kịp nhận phản hồi
	go func() {
		time.Sleep(2 * time.Second)
		log.Printf("[PURGE] 💀 Sạch sẽ! Tự động thoát theo lệnh .")
		os.Exit(0)
	}()
}

// ============================================================================
// [SOCIAL-CONSENSUS] BÀN TAY VÔ HÌNH (INVISIBLE HAND) - Emergency Reality Reset
// Mục đích: Xóa N khối gần nhất + Gỡ ban toàn mạng khi xảy ra chia cắt mạng.
// Triết lý: "Code is Law, but Social Consensus is Supreme" - Trao quyền phán
//
//	quyết cuối cùng cho con người thay vì để thuật toán tự động quyết định.
//
// Bảo mật: Chỉ cho phép từ localhost + Yêu cầu mã xác nhận 01900.
// ============================================================================
func (s *RPCServer) handleEmergencyReset(w http.ResponseWriter, r *http.Request) {
	// =================== [LỚP 1] CẢNH VỆ VÒNG NGOÀI ===================
	// Xác thực đầu vào: Kiểm tra định dạng, kích thước, kiểu dữ liệu
	var req struct {
		BlocksToRemove uint64 `json:"blocks_to_remove"` // Số khối muốn xóa (lùi về)
		TargetHeight   uint64 `json:"target_height"`    // Chiều cao khối đích muốn quay về (tùy chọn)
		TargetHash     string `json:"target_hash"`      // Mã băm nhánh đúng muốn quay về (tùy chọn mới)
		Code           string `json:"code"`             // Mã xác nhận (01900)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Dữ liệu đầu vào không hợp lệ.",
		})
		return
	}
	defer r.Body.Close()

	// =================== [LỚP 2] AN NINH NỘI BỘ ===================
	if req.Code != "01900" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Mã xác nhận sai! Lệnh Đảo Ngược Thực Tại bị từ chối.",
		})
		return
	}

	currentH := s.bridge.GetCurrentVersion()

	// Trường hợp 3: Quay về theo Target Hash (Cách thông minh mới)
	if req.TargetHash != "" {
		targetHashClean := strings.TrimPrefix(req.TargetHash, "0x")
		targetHashBytes, err := hex.DecodeString(targetHashClean)
		if err != nil || len(targetHashBytes) != 32 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Mã băm Target Hash không hợp lệ (Phải là 32 bytes hex).",
			})
			return
		}

		log.Printf("🚨 [INVISIBLE-HAND] Bắt đầu đồng bộ theo Target Hash: %s", targetHashClean)

		// Tạm dừng đào để đảm bảo an toàn
		s.bridge.SetMiningPause(true)
		if s.cliApp != nil {
			s.cliApp.SetNodeMode("verify-only")
		}

		// Tạo context timeout 30 giây
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		currentHash := targetHashBytes
		var pathHashes []string
		var forkPointHeight uint64
		var foundForkPoint = false
		var targetHeight uint64
		var firstHeader = true

		// Vòng lặp Trace Backward
		for {
			select {
			case <-ctx.Done():
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Không tìm thấy khối này trên mạng lưới (Hết thời gian 30 giây truy vấn).",
				})
				return
			default:
			}

			if s.netMgr == nil || s.netMgr.BanMgr == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Mạng P2P chưa được khởi tạo.",
				})
				return
			}

			staticPeers := s.netMgr.BanMgr.GetStaticPeers()
			var onlineStaticPeers []peer.ID
			for _, sp := range staticPeers {
				pid, err := peer.Decode(sp.ID)
				if err == nil && s.netMgr.Host.Network().Connectedness(pid) == network.Connected {
					onlineStaticPeers = append(onlineStaticPeers, pid)
				}
			}

			if len(onlineStaticPeers) == 0 {
				log.Printf("[INVISIBLE-HAND] ⏳ Không có Node tĩnh nào trực tuyến, đang đợi...")
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// Chọn best static peer
			targetPeer := s.netMgr.SelectBestPeer(onlineStaticPeers)
			if targetPeer == "" {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// Yêu cầu Header của currentHash
			hBytes, err := s.netMgr.RequestHeaderByHash(ctx, targetPeer, currentHash)
			if err != nil {
				log.Printf("[INVISIBLE-HAND] ⚠️ Lỗi khi tải header của %x từ %s: %v. Thử lại...", currentHash[:4], targetPeer.String()[:8], err)
				time.Sleep(500 * time.Millisecond)
				continue
			}

			var header pb_block.BlockHeader
			if err := proto.Unmarshal(hBytes, &header); err != nil {
				log.Printf("[INVISIBLE-HAND] ❌ Lỗi unmarshal header từ %s: %v", targetPeer.String()[:8], err)
				time.Sleep(500 * time.Millisecond)
				continue
			}

			if firstHeader {
				targetHeight = header.Height
				firstHeader = false
			}

			hashStr := hex.EncodeToString(currentHash)
			pathHashes = append(pathHashes, hashStr)

			parentHashBytes := header.ParentHash.Value

			// Kiểm tra parentHash có trong DB cục bộ không
			parentRaw := s.bridge.GetRawByHash(parentHashBytes)
			if parentRaw != nil {
				var parentBlock pb_block.Block
				if err := proto.Unmarshal(parentRaw, &parentBlock); err == nil && parentBlock.Header != nil {
					forkPointHeight = parentBlock.Header.Height
					foundForkPoint = true
					log.Printf("🎯 [INVISIBLE-HAND] Tìm thấy điểm rẽ nhánh (Fork Point) tại chiều cao #%d (Hash cha: %x)", forkPointHeight, parentHashBytes[:6])
					break
				}
			}

			if header.Height == 0 {
				log.Printf("[INVISIBLE-HAND] ⚠️ Đã lùi về tận Genesis nhưng không khớp dữ liệu local.")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Không tìm thấy điểm giao nhau của nhánh với dữ liệu cục bộ.",
				})
				return
			}

			currentHash = parentHashBytes
		}

		if !foundForkPoint {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Không tìm thấy điểm rẽ nhánh hợp lệ.",
			})
			return
		}

		// 3. Cắt bỏ chính xác (Surgical Delete)
		success := s.bridge.ForceDeleteBlocks(currentH, forkPointHeight)
		if !success {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Không thể xóa khối vật lý tại Rust Core.",
			})
			return
		}

		// Thiết lập Target Path Hashes
		s.netMgr.TargetPathMu.Lock()
		s.netMgr.TargetPathHashes = make(map[string]bool)
		for _, ph := range pathHashes {
			s.netMgr.TargetPathHashes[ph] = true
		}
		s.netMgr.TargetPathHashes[targetHashClean] = true
		s.netMgr.TargetPathMu.Unlock()

		log.Printf("🧹 [INVISIBLE-HAND] Đã xóa khối lùi về #%d và thiết lập bộ lọc %d hashes nhánh đúng.", forkPointHeight, len(pathHashes))

		// Gỡ ban toàn mạng & Dọn sạch Mempool & Reset UI Tracker
		if s.netMgr.BanMgr != nil {
			s.netMgr.BanMgr.ClearAllBans()
		}
		if s.netMgr.Mempool != nil {
			if mp, ok := s.netMgr.Mempool.(*node_p2p.Mempool); ok {
				mp.Purge()
			}
		}
		s.txTrackerMu.Lock()
		s.txTracker = make(map[string]*TrackedTx)
		s.txOrder = make([]string, 0)
		s.lastTrackedHeight = forkPointHeight
		s.txTrackerMu.Unlock()

		s.recentBlocksCacheMu.Lock()
		s.recentBlocksCache = nil
		s.recentBlocksCacheMu.Unlock()

		// Kích hoạt đồng bộ lại từ mạng lưới
		if s.netMgr.SyncEngine != nil {
			s.netMgr.SyncEngine.StartSync(targetHeight)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"message":         fmt.Sprintf("Đã tìm thấy điểm rẽ nhánh ở #%d. Đã xóa dữ liệu cũ và bắt đầu đồng bộ theo nhánh đúng lên tới #%d!", forkPointHeight, targetHeight),
			"previous_height": currentH,
			"new_height":      forkPointHeight,
			"target_height":   targetHeight,
		})
		return
	}

	// Tính toán target height và blocks to remove thông minh (Cách cũ)
	var targetHeight uint64
	var blocksToRemove uint64

	if req.TargetHeight > 0 {
		if req.TargetHeight >= currentH {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Chiều cao đích (#%d) phải nhỏ hơn chiều cao hiện tại (#%d).", req.TargetHeight, currentH),
			})
			return
		}
		targetHeight = req.TargetHeight
		blocksToRemove = currentH - targetHeight
	} else {
		if req.BlocksToRemove == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Vui lòng nhập số khối muốn xóa hoặc chiều cao đích.",
			})
			return
		}
		if req.BlocksToRemove >= currentH {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Không thể xóa %d khối vì Node chỉ có %d khối. Vui lòng nhập số nhỏ hơn.", req.BlocksToRemove, currentH),
			})
			return
		}
		blocksToRemove = req.BlocksToRemove
		targetHeight = currentH - blocksToRemove
	}

	log.Printf("🚨 [INVISIBLE-HAND] KÍCH HOẠT BÀN TAY VÔ HÌNH: Xóa %d khối (từ #%d về chính xác #%d)", blocksToRemove, currentH, targetHeight)

	// =================== [LỚP 3] THỰC THI ===================
	s.bridge.SetMiningPause(true)
	if s.cliApp != nil {
		s.cliApp.SetNodeMode("verify-only")
	}
	log.Printf("⏸️ [INVISIBLE-HAND] Đã tạm dừng khai thác.")

	success := s.bridge.ForceDeleteBlocks(currentH, targetHeight)
	if !success {
		log.Printf("❌ [INVISIBLE-HAND] Rust Core từ chối xóa khối từ #%d về #%d!", currentH, targetHeight)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Rust Core từ chối xóa khối. Vui lòng kiểm tra log hệ thống.",
		})
		return
	}
	log.Printf("✅ [INVISIBLE-HAND] Đã xóa vật lý %d khối thành công (về #%d)", blocksToRemove, targetHeight)

	// Bước 4: Gỡ Ban toàn mạng (Lệnh Ân xá)
	if s.netMgr != nil {
		if s.netMgr.BanMgr != nil {
			s.netMgr.BanMgr.ClearAllBans()
		}
		// Dọn sạch Mempool (các giao dịch cũ có thể không hợp lệ ở trạng thái mới)
		// Tại sao dùng type assertion: Purge() là phương thức trên concrete *Mempool, không nằm trong MempoolInterface
		if s.netMgr.Mempool != nil {
			if mp, ok := s.netMgr.Mempool.(*node_p2p.Mempool); ok {
				mp.Purge()
				log.Printf("🧹 [INVISIBLE-HAND] Đã dọn sạch Mempool.")
			}
		}
	}

	// Bước 5: Reset UI Tracker (Xóa cache giao dịch cũ)
	s.txTrackerMu.Lock()
	s.txTracker = make(map[string]*TrackedTx)
	s.txOrder = make([]string, 0)
	s.lastTrackedHeight = targetHeight
	s.txTrackerMu.Unlock()

	// Xóa cache khối gần nhất
	s.recentBlocksCacheMu.Lock()
	s.recentBlocksCache = nil
	s.recentBlocksCacheMu.Unlock()

	log.Printf("✅ [INVISIBLE-HAND] THÀNH CÔNG! Đã xóa %d khối gần nhất. Node đang ở #%d. Hệ thống sẽ tự động đồng bộ lại.", blocksToRemove, targetHeight)

	// =================== [LỚP 4] TÌNH BÁO (LOG) ===================
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"message":         fmt.Sprintf("Đã xóa %d khối gần nhất (từ #%d về #%d) và gỡ ban toàn mạng. Hệ thống đang tự động đồng bộ lại!", blocksToRemove, currentH, targetHeight),
		"previous_height": currentH,
		"new_height":      targetHeight,
	})
}

// [SHUTDOWN-V1.0] handleNodeShutdown: Xử lý yêu cầu tắt Node từ giao diện điều khiển
// Tại sao thiết kế goroutine và delay 1 giây: Cần trả về phản hồi HTTP OK (JSON) cho frontend trước để giao diện hiển thị thông báo thành công,
// tránh việc frontend bị lỗi kết nối ngang (network error) khi máy chủ đột ngột chết trước khi gửi gói tin response.
// Tại sao phải gọi s.bridge.Close(): Giải phóng hoàn toàn các file lock của database RocksDB trong Rust Core và tắt tiến trình con scl_server.exe,
// tránh hiện tượng lock dữ liệu khi khởi chạy lại.
func (s *RPCServer) handleNodeShutdown(w http.ResponseWriter, r *http.Request) {
	log.Printf("[RPC] 💀 Nhận lệnh TẮT NODE từ giao diện điều khiển. Đang dọn dẹp tài nguyên...")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Node đang dừng hoạt động... Cửa sổ dòng lệnh sẽ đóng lại sau giây lát.",
	})

	go func() {
		time.Sleep(1 * time.Second) // Chờ response được truyền đi hoàn tất
		s.bridge.Close()            // Tắt RocksDB và kill Rust server con
		log.Printf("[RPC] 🏁 Đã dọn dẹp xong. Tiến hành tắt chương trình.")
		os.Exit(0)
	}()
}

// handleManualSnapshotImport: Xử lý nạp snapshot thủ công.
func (s *RPCServer) handleManualSnapshotImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !node_p2p.EnableSnapshotJumping {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Tính năng nạp Snapshot thủ công đã bị vô hiệu hóa rõ ràng trên Node này.",
		})
		return
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tính năng nạp Snapshot thủ công đang hoạt động.",
	})
}

// handleManualSnapshotExport: Xử lý xuất snapshot thủ công.
func (s *RPCServer) handleManualSnapshotExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !node_p2p.EnableSnapshotJumping {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Tính năng xuất Snapshot thủ công đã bị vô hiệu hóa rõ ràng trên Node này.",
		})
		return
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tính năng xuất Snapshot thủ công đang hoạt động.",
	})
}

// GetTransactionStatusBatchChunked: Truy vấn trạng thái giao dịch theo lô sử dụng cơ chế chia nhỏ lô (Chunking Fallback)
// Tại sao: Khi hệ thống quá tải hoặc số lượng giao dịch đối soát quá lớn, batch gốc có thể bị timeout hoặc thất bại.
// Thay vì chạy vòng lặp gRPC đơn lẻ (gây bão gRPC Fallback Storm làm sập Node), chúng ta chia thành các lô phụ tối đa 5000 phần tử.
// Nếu bất kỳ lô phụ nào tiếp tục thất bại, chúng ta sẽ dừng lại và trả về lỗi để bảo vệ hệ thống.
func (s *RPCServer) GetTransactionStatusBatchChunked(batchHashes [][]byte) ([]*pb_block.TxStatusEntry, error) {
	if len(batchHashes) == 0 {
		return nil, nil
	}

	// 1. Thử gọi lô gốc trước để đạt hiệu năng tối ưu
	statusEntries, err := s.bridge.GetTransactionStatusBatch(batchHashes)
	if err == nil {
		return statusEntries, nil
	}

	log.Printf("[RPC-FALLBACK-WARN] GetTransactionStatusBatch thất bại với %d giao dịch: %v. Bắt đầu chia nhỏ lô...", len(batchHashes), err)

	// 2. Chia nhỏ thành các chunk nhỏ hơn (mỗi chunk tối đa 5000 phần tử)
	const chunkSize = 5000
	var allEntries []*pb_block.TxStatusEntry

	for i := 0; i < len(batchHashes); i += chunkSize {
		end := i + chunkSize
		if end > len(batchHashes) {
			end = len(batchHashes)
		}
		chunk := batchHashes[i:end]

		log.Printf("[RPC-FALLBACK] Đang thực hiện lại gRPC Batch cho chunk [%d -> %d] (%d hashes)...", i, end, len(chunk))
		entries, chunkErr := s.bridge.GetTransactionStatusBatch(chunk)
		if chunkErr != nil {
			log.Printf("[RPC-FALLBACK-ERROR] Chunk batch [%d -> %d] vẫn bị lỗi: %v. Hủy toàn bộ tiến trình để tránh bão gRPC.", i, end, chunkErr)
			return nil, chunkErr
		}
		allEntries = append(allEntries, entries...)
	}

	return allEntries, nil
}

// GetBalanceBatchChunked: Truy vấn số dư tài khoản theo lô sử dụng cơ chế chia nhỏ lô (Chunking Fallback)
// Tại sao: Khi lượng địa chỉ đối soát quá lớn, batch gốc có thể bị timeout hoặc thất bại.
// Thay vì chạy vòng lặp gRPC đơn lẻ (gây bão gRPC Fallback Storm làm sập Node), chúng ta chia thành các lô phụ tối đa 5000 phần tử.
// Nếu bất kỳ lô phụ nào tiếp tục thất bại, chúng ta sẽ dừng lại và trả về lỗi để bảo vệ hệ thống.
func (s *RPCServer) GetBalanceBatchChunked(batchAddrs [][]byte) ([]*pb_block.BalanceEntry, error) {
	if len(batchAddrs) == 0 {
		return nil, nil
	}

	// 1. Thử gọi lô gốc trước để đạt hiệu năng tối ưu
	balances, err := s.bridge.GetBalanceBatch(batchAddrs)
	if err == nil {
		return balances, nil
	}

	log.Printf("[RPC-FALLBACK-WARN] GetBalanceBatch thất bại với %d địa chỉ: %v. Bắt đầu chia nhỏ lô...", len(batchAddrs), err)

	// 2. Chia nhỏ thành các chunk nhỏ hơn (mỗi chunk tối đa 5000 phần tử)
	const chunkSize = 5000
	var allBalances []*pb_block.BalanceEntry

	for i := 0; i < len(batchAddrs); i += chunkSize {
		end := i + chunkSize
		if end > len(batchAddrs) {
			end = len(batchAddrs)
		}
		chunk := batchAddrs[i:end]

		log.Printf("[RPC-FALLBACK] Đang thực hiện lại gRPC GetBalanceBatch cho chunk [%d -> %d] (%d addresses)...", i, end, len(chunk))
		entries, chunkErr := s.bridge.GetBalanceBatch(chunk)
		if chunkErr != nil {
			log.Printf("[RPC-FALLBACK-ERROR] Chunk GetBalanceBatch [%d -> %d] vẫn bị lỗi: %v. Hủy toàn bộ tiến trình để tránh bão gRPC.", i, end, chunkErr)
			return nil, chunkErr
		}
		allBalances = append(allBalances, entries...)
	}

	return allBalances, nil
}

// --------------------------------------------------------------------------
// gRPC MINER GATEWAY SERVICE IMPLEMENTATION (Independent Miner Stream V1.0)
// --------------------------------------------------------------------------

// ConnectMiner handles the bidirectional gRPC stream connection from genz_miner.exe.
func (s *RPCServer) ConnectMiner(stream pb_block.MinerGateway_ConnectMinerServer) error {
	s.minerStreamsMu.Lock()
	if s.minerStreams == nil {
		s.minerStreams = make(map[uint64]pb_block.MinerGateway_ConnectMinerServer)
	}
	sid := s.nextStreamId
	s.nextStreamId++
	s.minerStreams[sid] = stream
	s.minerStreamsMu.Unlock()

	defer func() {
		s.minerStreamsMu.Lock()
		delete(s.minerStreams, sid)
		delete(s.minerHashrates, sid)
		activeStreams := len(s.minerStreams)
		s.minerStreamsMu.Unlock()
		log.Printf("[RPC-GRPC] 🔌 Thợ đào #%d đã ngắt kết nối.", sid)

		// [BRIDGE-RECOVERY] Tự động hồi sinh thợ đào genz_miner.exe nếu bị rụng kết nối đột ngột khi đang bật đào
		s.nodeModeMu.RLock()
		isFullMining := s.nodeMode == "full-mining"
		s.nodeModeMu.RUnlock()

		s.miningDeviceMu.RLock()
		device := s.miningDevice
		s.miningDeviceMu.RUnlock()

		if activeStreams == 0 && isFullMining && (device == "cpu" || device == "hybrid") {
			log.Printf("[BRIDGE-RECOVERY] ♻️ Phát hiện thợ đào mất kết nối trong lúc Node đang bật đào. Đang tự động hồi sinh genz_miner...")
			go func() {
				time.Sleep(3 * time.Second) // Chờ 3 giây để giải phóng cổng cũ
				s.minerStreamsMu.Lock()
				stillZero := len(s.minerStreams) == 0
				s.minerStreamsMu.Unlock()
				if stillZero {
					s.nodeModeMu.RLock()
					miningEnabled := s.nodeMode == "full-mining"
					s.nodeModeMu.RUnlock()
					if miningEnabled {
						if err := s.bridge.StartGenzMiner(s.port + 10000); err != nil {
							log.Printf("[BRIDGE-RECOVERY] ⚠️ Lỗi khi tự động hồi sinh genz_miner: %v", err)
						} else {
							log.Printf("[BRIDGE-RECOVERY] ✅ Đã hồi sinh genz_miner thành công.")
						}
					}
				}
			}()
		}
	}()

	log.Printf("[RPC-GRPC] 🔌 Thợ đào mới #%d đã kết nối.", sid)

	// Lấy cường độ CPU và trạng thái pause hiện tại để đồng bộ
	s.cpuIntensityMu.RLock()
	intensity := s.cpuIntensity
	s.cpuIntensityMu.RUnlock()

	s.nodeModeMu.RLock()
	paused := s.nodeMode != "full-mining"
	s.nodeModeMu.RUnlock()

	// Khởi tạo lệnh cấu hình đầu tiên
	var activeTaskBytes []byte
	var activeSessionId uint64
	if s.cliApp != nil {
		s.cliApp.activeMiningMu.Lock()
		if len(s.cliApp.activeBodyData) > 0 && s.cliApp.activeBlock != nil {
			// Xây dựng lại MiningTask bytes hiện tại
			task := &pb_block.MiningTask{
				Header:    s.cliApp.activeBlock.Header,
				Intensity: uint32(intensity),
				Threads:   uint32(2),
				SessionId: s.cliApp.activeSessionId,
			}
			activeTaskBytes, _ = proto.Marshal(task)
			activeSessionId = s.cliApp.activeSessionId
		}
		s.cliApp.activeMiningMu.Unlock()
	}

	initCmd := &pb_block.NodeCommand{
		IsPaused:      paused,
		CpuIntensity:  uint32(intensity),
		BlockTemplate: activeTaskBytes,
		SessionId:     activeSessionId,
	}

	if err := stream.Send(initCmd); err != nil {
		return err
	}

	// Lắng nghe thông điệp báo cáo từ thợ đào gửi lên
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Cập nhật hashrate
		if msg.CurrentHashrate >= 0 {
			s.minerStreamsMu.Lock()
			s.minerHashrates[sid] = msg.CurrentHashrate
			s.minerStreamsMu.Unlock()
		}

		// Nhận nonce khi thợ đào giải quyết được khối
		if msg.FoundNonce > 0 {
			if !s.isMiningAllowed() {
				log.Printf("[RPC-GRPC] ⚠️ Bỏ qua nonce từ thợ đào #%d vì mạng đang offline", sid)
				continue
			}
			log.Printf("[RPC-GRPC] 🏆 Nhận được nonce hợp lệ %d (SID: %d) từ thợ đào #%d", msg.FoundNonce, msg.SessionId, sid)
			if s.cliApp != nil {
				s.cliApp.miningResultChan <- msg
			}
		}
	}

	return nil
}

// BroadcastCommand phát sóng NodeCommand tới toàn bộ thợ đào đang kết nối.
func (s *RPCServer) BroadcastCommand(cmd *pb_block.NodeCommand) {
	s.minerStreamsMu.RLock()
	defer s.minerStreamsMu.RUnlock()

	for sid, stream := range s.minerStreams {
		go func(id uint64, st pb_block.MinerGateway_ConnectMinerServer) {
			if err := st.Send(cmd); err != nil {
				log.Printf("[RPC-GRPC] ❌ Không thể gửi lệnh tới thợ đào #%d: %v", id, err)
			}
		}(sid, stream)
	}
}

// BroadcastMiningTask đóng gói và phát nhiệm vụ đào mới cho các thợ đào.
func (s *RPCServer) BroadcastMiningTask(taskBytes []byte, sessionID uint64, difficulty []byte) {
	s.cpuIntensityMu.RLock()
	intensity := s.cpuIntensity
	s.cpuIntensityMu.RUnlock()

	s.nodeModeMu.RLock()
	paused := s.nodeMode != "full-mining"
	s.nodeModeMu.RUnlock()

	var targetDiff uint64
	if len(difficulty) >= 8 {
		targetDiff = binary.LittleEndian.Uint64(difficulty[:8])
	}

	cmd := &pb_block.NodeCommand{
		IsPaused:         paused,
		CpuIntensity:     uint32(intensity),
		BlockTemplate:    taskBytes,
		TargetDifficulty: targetDiff,
		SessionId:        sessionID,
	}

	s.BroadcastCommand(cmd)
}

// BroadcastPauseState phát sóng trạng thái pause cập nhật tới toàn bộ thợ đào.
func (s *RPCServer) BroadcastPauseState(paused bool) {
	s.cpuIntensityMu.RLock()
	intensity := s.cpuIntensity
	s.cpuIntensityMu.RUnlock()

	var sessionID uint64
	if s.cliApp != nil {
		s.cliApp.activeMiningMu.Lock()
		sessionID = s.cliApp.activeSessionId
		s.cliApp.activeMiningMu.Unlock()
	}

	cmd := &pb_block.NodeCommand{
		IsPaused:     paused,
		CpuIntensity: uint32(intensity),
		SessionId:    sessionID,
	}
	s.BroadcastCommand(cmd)
}

func (s *RPCServer) handleGetStaticPeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.netMgr == nil || s.netMgr.BanMgr == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "BanManager not initialized"})
		return
	}
	peers := s.netMgr.BanMgr.GetStaticPeers()
	mode := s.netMgr.BanMgr.GetIsolationMode()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"static_peers":   peers,
		"isolation_mode": mode,
	})
}

func (s *RPCServer) handleUpdateStaticPeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.netMgr == nil || s.netMgr.BanMgr == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "BanManager not initialized"})
		return
	}

	var req struct {
		StaticPeers []node_p2p.StaticPeer `json:"static_peers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	for _, p := range req.StaticPeers {
		if p.ID == "" || p.Address == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Peer ID and Address are required"})
			return
		}
	}

	s.netMgr.BanMgr.SetStaticPeers(req.StaticPeers)

	// Sau khi cập nhật static peers, thử kết nối P2P ngay lập tức tới các node này
	go func() {
		for _, sp := range req.StaticPeers {
			ma, err := multiaddr.NewMultiaddr(sp.Address)
			if err == nil {
				addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = s.netMgr.Host.Connect(ctx, *addrInfo)
					cancel()
				}
			}
		}
	}()

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Updated static peers successfully"})
}

func (s *RPCServer) handleSetIsolationMode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.netMgr == nil || s.netMgr.BanMgr == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "BanManager not initialized"})
		return
	}

	var req struct {
		IsolationMode int `json:"isolation_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if req.IsolationMode < 1 || req.IsolationMode > 3 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Invalid isolation mode (1-3)"})
		return
	}

	s.netMgr.BanMgr.SetIsolationMode(req.IsolationMode)
	log.Printf("[ISOLATION-MODE] 🔒 Đã thay đổi Chế độ Cách ly thành: %d", req.IsolationMode)

	// Nếu chuyển sang chế độ 3 (Strict Isolation), ngắt kết nối các peer không phải node tĩnh ngay lập tức
	if req.IsolationMode == 3 && s.netMgr != nil && s.netMgr.Host != nil {
		go func() {
			peers := s.netMgr.Host.Network().Peers()
			for _, p := range peers {
				if !s.netMgr.BanMgr.IsStaticPeerID(p) {
					log.Printf("[ISOLATION] 🔌 Ngắt kết nối với peer lạ: %s", p.String()[:12])
					s.netMgr.Host.Network().ClosePeer(p)
				}
			}
		}()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "isolation_mode": req.IsolationMode})
}

// walletGate: Middleware chặn và xác thực các kết nối từ ví Client-side
func (s *RPCServer) walletGate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Nếu là request từ Localhost, cho phép qua luôn (phục vụ Node Monitor cục bộ)
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		isLocal := (host == "127.0.0.1" || host == "::1" || host == "[::1]") && r.Header.Get("X-Forwarded-For") == ""
		if isLocal {
			next(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		
		// 2. Nếu không phải localhost, kiểm tra xem cổng ví có được bật không
		if !s.cliApp.walletServerEnabled {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "Forbidden",
				"message": "Cổng kết nối ví (Wallet Server) chưa được kích hoạt trên Node này. Vui lòng chạy node với cờ --wallet-server.",
			})
			return
		}

		// 3. Kiểm tra Token xác thực nếu có cấu hình
		if s.cliApp.walletToken != "" {
			token := r.Header.Get("X-Wallet-Token")
			if token == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					token = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if token != s.cliApp.walletToken {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"status":  "Unauthorized",
					"message": "Mã xác thực ví (Wallet Token) không chính xác hoặc chưa được cung cấp.",
				})
				return
			}
		}

		next(w, r)
	}
}

// handleSendRawTx: Nhận giao dịch signed offline từ ví client và phát sóng lên mempool
func (s *RPCServer) handleSendRawTx(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	var req struct {
		Version          uint64 `json:"version"`
		Sender           string `json:"sender"`
		Receiver         string `json:"receiver"`
		Amount           uint64 `json:"amount"`
		Fee              uint64 `json:"fee"`
		Nonce            uint64 `json:"nonce"`
		Timestamp        uint64 `json:"timestamp"`
		RecentBlockHash  string `json:"recent_block_hash"`
		ChainId          uint64 `json:"chain_id"`
		Signature        string `json:"signature"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "JSON request body không hợp lệ: " + err.Error()})
		return
	}
	defer r.Body.Close()

	senderHex := strings.TrimPrefix(req.Sender, "0x")
	receiverHex := strings.TrimPrefix(req.Receiver, "0x")
	recentBlockHex := strings.TrimPrefix(req.RecentBlockHash, "0x")
	sigHex := strings.TrimPrefix(req.Signature, "0x")

	senderBytes, err := hex.DecodeString(senderHex)
	if err != nil || len(senderBytes) != 32 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ người gửi không hợp lệ (Phải là 32 bytes hex)"})
		return
	}

	receiverBytes, err := hex.DecodeString(receiverHex)
	if err != nil || len(receiverBytes) != 32 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Địa chỉ người nhận không hợp lệ (Phải là 32 bytes hex)"})
		return
	}

	recentBlockBytes, err := hex.DecodeString(recentBlockHex)
	if err != nil || len(recentBlockBytes) != 32 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Mã băm recent_block_hash không hợp lệ (Phải là 32 bytes hex)"})
		return
	}

	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != 64 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Chữ ký signature không hợp lệ (Phải là 64 bytes hex)"})
		return
	}

	// [RATE LIMIT CHECK] Chống Spam giao dịch
	if !s.allowTransactions(senderHex, 1) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Rate limit exceeded. Quá nhiều giao dịch từ địa chỉ này."})
		return
	}

	// [VALIDATE FEE] Mức phí 3 tầng cố định: 250, 500, hoặc 1000 VNT
	if !s.bridge.IsValidFee(req.Fee) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Mức phí không hợp lệ. Phải là 250, 500 hoặc 1000 VNT."})
		return
	}

	// [NANO-DUST GUARD] Nếu số tiền chuyển < 10 VNT, yêu cầu phí tối thiểu 500 VNT (Priority)
	if req.Amount < 10 && req.Fee < 500 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Giao dịch quá nhỏ (< 10 VNT) yêu cầu phí tối thiểu PRIORITY (500 VNT)."})
		return
	}

	// [BALANCE CHECK] Kiểm tra số dư spendable
	spendable := s.bridge.GetSpendableBalance(senderBytes)
	mempoolPending := s.netMgr.Mempool.GetPendingSpend(senderHex)
	totalNeeded := req.Amount + req.Fee

	if spendable < (mempoolPending + totalNeeded) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "Error",
			"message": fmt.Sprintf("Số dư khả dụng không đủ. Cần thêm: %.8f BTC_Z", float64(mempoolPending+totalNeeded-spendable)/1e8),
		})
		return
	}

	// [NONCE CHECK] Kiểm tra Nonce hợp lệ
	currentNonce := s.bridge.GetNonce(nil, senderBytes)
	nextExpectedNonce := s.netMgr.Mempool.GetNextNonce(senderHex, currentNonce)
	if req.Nonce < nextExpectedNonce {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Error",
			"message": fmt.Sprintf("Nonce quá thấp. Nonce mong đợi tiếp theo: %d, Nhận được: %d", nextExpectedNonce, req.Nonce),
		})
		return
	}

	// [RECONSTRUCT PB TRANSACTION]
	tx := &pb_block.Transaction{
		Version: req.Version,
		Sender: &pb_block.Address{
			Value: senderBytes,
		},
		Receiver: &pb_block.Address{
			Value: receiverBytes,
		},
		Amount: req.Amount,
		Fee:    req.Fee,
		Nonce:  req.Nonce,
		Timestamp: req.Timestamp,
		RecentBlockHash: recentBlockBytes,
		Signature: &pb_block.Signature{
			Value: sigBytes,
		},
		ChainId: req.ChainId,
	}

	// [VERIFY SIGNATURE NATIVE GO]
	if !node_p2p.VerifySignatureNative(tx) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "Error", "message": "Chữ ký giao dịch không hợp lệ!"})
		return
	}

	// [SERIALIZE AND PUSH TO MEMPOOL]
	txBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
	h_full := node_p2p.GetTxIDNative(txBytes)
	txHashStr := hex.EncodeToString(h_full)

	// Đăng ký vào Tx Tracker với status 99 (Pending)
	s.updateTxTracker(txHashStr, senderHex, receiverHex, req.Amount, req.Fee, req.Nonce, 0, time.Now().Unix(), "")
	s.txTrackerMu.Lock()
	if tracked, exists := s.txTracker[txHashStr]; exists {
		tracked.Status = 99
		tracked.ErrorMessage = s.getTxStatusMessage(99)
	}
	s.txTrackerMu.Unlock()
	s.triggerSave()

	// Gửi vào hàng đợi mempool
	s.netMgr.Mempool.PushToTxBus(txBytes, true)

	log.Printf("[RPC-SEND-RAW] ✅ Nhận thành công giao dịch signed offline %s từ ví client.", safeShortID(txHashStr))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "Success",
		"txid":   txHashStr,
	})
}

// --------------------------------------------------------------------------
// HTTP ENDPOINTS FOR PURE C++ GPU MINER (ASIC RESISTANT)
// --------------------------------------------------------------------------

func (s *RPCServer) handleMinerGetWork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.cliApp == nil {
		http.Error(w, "CLI app not initialized", http.StatusInternalServerError)
		return
	}

	if !s.isMiningAllowed() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.cliApp.activeMiningMu.Lock()
	activeBlock := s.cliApp.activeBlock
	activeSessionId := s.cliApp.activeSessionId
	s.cliApp.activeMiningMu.Unlock()

	if activeBlock == nil || activeBlock.Header == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Marshals block header
	headerRaw, err := proto.Marshal(activeBlock.Header)
	if err != nil {
		http.Error(w, "Failed to marshal block header", http.StatusInternalServerError)
		return
	}
	headerHash := s.bridge.CalculateBlockHeaderHash(headerRaw)

	// Calculates Target U256 (32 bytes) from difficulty
	targetBytes := difficultyToTarget(activeBlock.Header.Difficulty)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"header_hash": hex.EncodeToString(headerHash),
		"target":      hex.EncodeToString(targetBytes),
		"height":      activeBlock.Header.Height,
		"session_id":  activeSessionId,
		"intensity":   s.GetCpuIntensity(),
	})
}

func (s *RPCServer) handleMinerSubmitWork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.cliApp == nil {
		http.Error(w, "CLI app not initialized", http.StatusInternalServerError)
		return
	}

	if !s.isMiningAllowed() {
		http.Error(w, "Mining is paused due to network disconnection", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Nonce     uint64 `json:"nonce"`
		SessionId uint64 `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Nonce == 0 {
		http.Error(w, "Invalid nonce", http.StatusBadRequest)
		return
	}

	msg := &pb_block.MinerMessage{
		FoundNonce: req.Nonce,
		SessionId:  req.SessionId,
	}

	select {
	case s.cliApp.miningResultChan <- msg:
		log.Printf("[RPC-HTTP] 🏆 Nhận được nonce hợp lệ %d (SID: %d) từ GPU Miner", req.Nonce, req.SessionId)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	default:
		log.Printf("[RPC-HTTP] ⚠️ Hàng đợi kết quả đào đầy, bỏ qua nonce %d", req.Nonce)
		http.Error(w, "Result queue full", http.StatusServiceUnavailable)
	}
}

func difficultyToTarget(difficultyBytes []byte) []byte {
	diffPadded := make([]byte, 32)
	copy(diffPadded, difficultyBytes)

	// Convert little-endian bytes to big.Int for division
	diffBig := new(big.Int).SetBytes(reverseBytes(diffPadded))
	if diffBig.Cmp(big.NewInt(1)) <= 0 {
		targetBytes := make([]byte, 32)
		for i := range targetBytes {
			targetBytes[i] = 0xff
		}
		return targetBytes
	}

	maxU256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	targetBig := new(big.Int).Div(maxU256, diffBig)

	targetBytes := make([]byte, 32)
	tb := targetBig.Bytes()
	copy(targetBytes[32-len(tb):], tb)
	return reverseBytes(targetBytes)
}

func reverseBytes(b []byte) []byte {
	r := make([]byte, len(b))
	for i, v := range b {
		r[len(b)-1-i] = v
	}
	return r
}

func (s *RPCServer) handleMiningDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		s.miningDeviceMu.RLock()
		device := s.miningDevice
		s.miningDeviceMu.RUnlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "Success",
			"mining_device": device,
		})
		return
	}

	// POST: Cập nhật thiết bị đào
	var req struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Device != "cpu" && req.Device != "gpu" && req.Device != "hybrid" {
		http.Error(w, "Thiết bị không hợp lệ. Chấp nhận: cpu, gpu, hybrid", http.StatusBadRequest)
		return
	}

	s.miningDeviceMu.Lock()
	s.miningDevice = req.Device
	s.miningDeviceMu.Unlock()

	s.updateGpuEnvCheck()
	s.updateMiningState()
	s.saveNodeConfig()
	s.StartConfiguredMiners()

	log.Printf("[RPC-UI] 🎛️ Đã cập nhật thiết bị khai thác: %s", req.Device)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "Success",
		"mining_device": req.Device,
	})
}

func (s *RPCServer) updateGpuEnvCheck() {
	s.miningDeviceMu.RLock()
	device := s.miningDevice
	s.miningDeviceMu.RUnlock()

	if device == "gpu" || device == "hybrid" {
		errStr := s.checkGpuEnvironment()
		s.gpuEnvErrorMu.Lock()
		s.gpuEnvError = errStr
		s.gpuEnvErrorMu.Unlock()
	} else {
		s.gpuEnvErrorMu.Lock()
		s.gpuEnvError = ""
		s.gpuEnvErrorMu.Unlock()
	}
}

func (s *RPCServer) checkGpuEnvironment() string {
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
		return "❌ Không tìm thấy tệp tin chạy yona_gpu_miner.exe!"
	}

	// Chạy thử yona_gpu_miner.exe --check để chẩn đoán môi trường
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, minerPath, "--check")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		outStr := strings.TrimSpace(out.String())
		// Kiểm tra mã lỗi thiếu DLL trên Windows (0xc0000135)
		if strings.Contains(err.Error(), "0xc0000135") || strings.Contains(err.Error(), "status 3221225781") {
			return "❌ Môi trường thiếu: Thiếu thư viện Microsoft Visual C++ Redistributable hoặc Driver NVIDIA (Lỗi DLL 0xc0000135)."
		}
		if outStr != "" {
			if strings.Contains(outStr, "[CUDA-ERROR]") {
				lines := strings.Split(outStr, "\n")
				for _, line := range lines {
					if strings.Contains(line, "[CUDA-ERROR]") {
						return "❌ " + strings.TrimSpace(strings.ReplaceAll(line, "[CUDA-ERROR]", ""))
					}
				}
			}
			return "❌ Lỗi GPU: " + outStr
		}
		return "❌ Không thể khởi chạy GPU Miner: " + err.Error()
	}

	return "" // Môi trường OK
}

func (s *RPCServer) handleMinerHashrate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Hashrate uint64 `json:"hashrate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	atomic.StoreUint64(&s.gpuHashrate, req.Hashrate)
	s.lastGpuHashTime = time.Now()
	
	log.Printf("[RPC-UI] ⚡ Nhận báo cáo hashrate từ GPU Miner: %d H/s (%.2f MH/s)", req.Hashrate, float64(req.Hashrate)/1000000.0)

	json.NewEncoder(w).Encode(map[string]interface{}{"status": "success"})
}

func (s *RPCServer) handleInstallEnvironment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	log.Printf("[RPC-UI] ⚙️ Nhận lệnh tải và cài đặt Microsoft VC++ Redistributable...")

	// Chạy goroutine ngầm để không block HTTP request
	go func() {
		// 1. Tải bộ cài vc_redist.x64.exe từ Microsoft
		dest := filepath.Join(os.TempDir(), "vc_redist.x64.exe")
		
		log.Printf("[AUTO-INSTALL] 📥 Đang tải VC++ Redistributable từ Microsoft...")
		err := s.downloadFile("https://aka.ms/vs/17/release/vc_redist.x64.exe", dest)
		if err != nil {
			log.Printf("[AUTO-INSTALL] ❌ Lỗi tải VC++ Redistributable: %v", err)
			s.gpuEnvErrorMu.Lock()
			s.gpuEnvError = "❌ Tải VC++ Redistributable thất bại: " + err.Error()
			s.gpuEnvErrorMu.Unlock()
			return
		}

		// 2. Chạy cài đặt chế độ im lặng (Silent Install)
		log.Printf("[AUTO-INSTALL] 🛠️ Đang chạy cài đặt im lặng (vc_redist.x64.exe /q /norestart)...")
		cmd := exec.Command(dest, "/q", "/norestart")
		runErr := cmd.Run()
		if runErr != nil {
			log.Printf("[AUTO-INSTALL] ❌ Cài đặt VC++ Redistributable thất bại: %v", runErr)
			s.gpuEnvErrorMu.Lock()
			s.gpuEnvError = "❌ Cài đặt VC++ Redistributable thất bại: " + runErr.Error()
			s.gpuEnvErrorMu.Unlock()
			return
		}

		log.Printf("[AUTO-INSTALL] 🎉 Cài đặt VC++ Redistributable THÀNH CÔNG! Đang quét lại môi trường...")
		s.updateGpuEnvCheck()
	}()

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Success",
		"message": "Đang bắt đầu tải và cài đặt VC++ Redistributable chạy ngầm. Vui lòng theo dõi trong vài phút.",
	})
}

// downloadFile tải file từ URL về dest
func (s *RPCServer) downloadFile(url string, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status không hợp lệ: %s", resp.Status)
	}

	_, err = io.Copy(out, resp.Body)
	return err
}


