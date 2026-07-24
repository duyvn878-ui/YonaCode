/**
 * @file sync_engine.go
 * @brief Cơ chế đồng bộ hóa blockchain cho YonaCode (YonaCode Security Sync Engine).
 * @details Thực thi Giao thức Đồng thuận Võ Nhật Thiên (VNT Consensus 2.0) với cơ chế phòng thủ nâng cấp:
 *  - Phân tầng Sổ cái: Vùng Đá Tảng (Boulder Zone) và Vùng Linh Hoạt (Flexible Zone) lùi 5 khối đỉnh.
 *  - Tường lửa Bất biến: Ngăn chặn Reorg sâu tại Boulder Zone bằng Trọng tài Năng lượng x10.
 *  - Đồng bộ mồ côi nâng cấp: Đồng bộ chùm dây chuyền (Recursive Debt Collection) cho cả Sync và Gossip.
 *
 * @author Võ Nhật Thiên & Cộng sự - YonaCode V2.0 Security
 * @date 2026-07-13
 */

package node_p2p

import (
	pb_block "btc_genz/proto"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug" // [DEBUG] Dùng để in Stack Trace khi recover panic
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"btc_genz/6_user_interface/audit"
	"btc_genz/6_user_interface/i18n"

	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"
)

var (
	peerRootErrors   = make(map[peer.ID]int)
	peerRootErrorsMu sync.Mutex
)

type SyncState int

const (
	Syncing SyncState = iota
	Synced
	Stalled
	Bootstrapping
)

const (
	// [CONSTITUTIONAL-CONSTANTS]
	// Tại sao gom cụm: Để dễ dàng cấu hình và nâng cấp hệ thống khi có Hardfork đồng thuận.
	MaxDebtDepth      = 5                 // Số khối tối đa cho đồng bộ chùm lùi hoặc lệch chuỗi ngắn
	SnapshotInterval  = 1152              // Khoảng cách giữa các Snapshot nhảy cóc (đã đồng bộ bằng 1 Epoch)
	EpochLength       = 1152              // Độ dài một Epoch (khối lượng khối trong 24 giờ)
	CatchUpThreshold  = 5                 // Lệch bao nhiêu khối thì kích hoạt CatchUp tải chùm
	RoutineTimeout    = 3 * time.Minute   // [MAINNET-TIMEOUT] Tăng từ 20s lên 3 phút để tránh ngắt kết nối oan với Peer khi mạng chập chờn hoặc đĩa IO bị block tạm thời trong quá trình đồng bộ chùm khối mồ côi.
	BodySyncTimeout   = 5 * time.Minute   // [MAINNET-TIMEOUT] Giảm từ 30p xuống 5p để tránh kẹt kết nối TCP (Tarpit DoS) khi gặp Peer truyền dữ liệu nhỏ giọt.
)

// [VANGUARD-ORPHAN] Cấu trúc điều tra khối mồ côi
type OrphanInvestigation struct {
	MissingHash []byte
	Sender      peer.ID
	LastActive  time.Time
	Height      uint64
	IsBodySync  bool // [WATCHDOG-FEEDING-V3] True nếu đang tải Body nặng (30 phút timeout thay vì 20s)
}

type SyncEngine struct {
	mu                sync.RWMutex
	ctx               context.Context
	state             SyncState
	targetHeight      uint64
	currentHeight     uint64
	netManager        *NetworkManager
	mempool           MempoolInterface
	finalizedHeight   uint64
	pendingBlocks     map[uint64][][]byte
	fetchMu           sync.Mutex
	isProverNode      bool
	startupTime       time.Time
	LastSyncActivity  time.Time // [VANGUARD-DYNAMISM] Ghi lại nhịp đập đồng bộ cuối cùng
	syncFailures      int       // [VANGUARD-FIX] Đếm số lần đồng bộ thất bại liên tiếp
	lastFailedHeight  uint64    // [VANGUARD-FIX] Ghi lại cao độ khối gây lỗi gần nhất
	deepRecoveryCount int       // [SYNC-HEAL] Số lần đã thực hiện hồi phục sâu (Deep Recovery)
	snapshotChunksLoaded uint32    // [SNAP-SYNC-PROGRESS] Số lượng chunk snapshot đã tải
	snapshotChunksTotal  uint32    // [SNAP-SYNC-PROGRESS] Tổng số lượng chunk snapshot cần tải
	downloadingHeight    uint64    // [SYNC-STAGE-UX] Chiều cao khối đang tải trong Phase 1
	initialSyncDone      bool      // [SYNC-DDoS-PROTECTION] Đánh dấu đồng bộ khởi đầu thành công để chống DDoS ngắt đào

	// [LIGHTWEIGHT ORPHAN CACHE] Chỉ lưu trữ tiêu đề khối mồ côi gần
	orphanHeaders   map[string]*pb_block.BlockHeader // Key: Hash Hex
	orphanTxIDs     map[string][][]byte              // Key: Hash Hex -> Danh sách TxIDs của khối
	orphanCoinbase  map[string][]byte                // Key: Hash Hex -> Giao dịch Coinbase thô
	orphanHeadersMu sync.RWMutex

		orphanTracker    map[string]*OrphanInvestigation // [VANGUARD-ORPHAN] Theo dõi các vụ điều tra mồ côi
	orphanMu         sync.Mutex
	lastCatchUpTime  time.Time // [SYNC-CATCHUP] Lần cuối cùng kích hoạt đồng bộ lùi
	bootstrapRunning bool      // [FAST-SYNC] Chốt chặn chống chạy song song FastSync
	catchUpRunning   int32     // [ANTI-RACE] Atomic counter: Giới hạn tối đa 10 CatchUpSync chạy đồng thời để tránh làm nghẽn CPU/Network.
	TriggerSyncChan  chan struct{} // [EVENT-DRIVEN] Tín hiệu đánh thức syncLoop không dùng ticker
	failedOrphanAttempts   map[string]time.Time
	failedOrphanAttemptsMu sync.Mutex
	unconnectableHashes    map[string]time.Time

	// [3-STRIKE] Ghi sổ số lần mỗi Peer gửi mồ côi kích hoạt cân chỉnh chuỗi mồ côi
	// Tại sao theo dõi theo Peer ID: Ta muốn biết hành vi tổng thể của từng node trên mạng.
	// Peer trung thực chỉ gửi mồ côi 1-2 lần rồi chuỗi sẽ được đồng bộ thành công.
	// Peer xấu (hoặc lỗi phần mềm) sẽ liên tục gửi mồ côi mà không bao giờ cung cấp được chuỗi hợp lệ.
	orphanSyncStrikes   map[peer.ID]int
	orphanSyncStrikesTs map[peer.ID]time.Time // Thời điểm ghi nhận strike cuối cùng
	orphanSyncStrikesMu sync.Mutex

	// [VÁ LỖI KẸT CHUỒI NHẸ CHÚNG] Ghi nhớ các block sidechain và peer tương ứng để tránh kẹt loop
	knownSidechains     map[string]time.Time // Thời điểm ghi nhận sidechain để tự động giải phóng bộ nhớ sau 1 giờ.
	sidechainPeers      map[peer.ID]time.Time
}

// checkAndRecordOrphanAttempt kiểm tra xem Hash mồ côi này đã được xử lý gần đây hoặc bị cấm chưa.
// Tại sao: Chốt chặn này ghi nhận theo hash khối bị thiếu thay vì chặn peer (node). Nếu một hash mồ côi 
// đang chờ cân chỉnh chuỗi mồ côi hoặc đã cân chỉnh chuỗi mồ côi thất bại, ta từ chối yêu cầu lần 2 cho cùng hash đó từ mọi peer 
// để tránh spam và lãng phí băng thông mạng.
func (s *SyncEngine) checkAndRecordOrphanAttempt(peerId peer.ID, hash []byte) bool {
	s.failedOrphanAttemptsMu.Lock()
	defer s.failedOrphanAttemptsMu.Unlock()

	hashStr := hex.EncodeToString(hash)
	// 1. Tạm dừng xử lý trong 5 phút nếu hash này không thể liên kết (bad fork)
	if lastTime, exists := s.unconnectableHashes[hashStr]; exists {
		if time.Since(lastTime) < 5*time.Minute {
			return false
		}
		delete(s.unconnectableHashes, hashStr)
	}

	// 2. Chặn cooldown 2 phút cho hash này
	if lastTime, exists := s.failedOrphanAttempts[hashStr]; exists {
		if time.Since(lastTime) < 2*time.Minute {
			return false
		}
	}
	s.failedOrphanAttempts[hashStr] = time.Now()

	// Dọn dẹp cache cũ để tránh phình RAM
	if len(s.failedOrphanAttempts) > 2000 {
		now := time.Now()
		for k, v := range s.failedOrphanAttempts {
			if now.Sub(v) > 5*time.Minute {
				delete(s.failedOrphanAttempts, k)
			}
		}
	}
	return true
}

// markOrphanHashAsUnconnectable tạm thời đánh dấu chuỗi hash mồ côi này là không thể liên kết (bad fork) trong 5 phút.
// Tại sao: Nếu không tìm thấy điểm rẽ nhánh cho hash này trong DB, nghĩa là chuỗi khối này bị đứt gãy 
// hoặc không tương thích. Ta tạm dừng 5 phút không cân chỉnh chuỗi mồ côi hay xử lý lại hash này nữa để tránh spam tài nguyên. 
// Hết 5 phút, ta cho phép node thử lại để tự khôi phục nếu dữ liệu local đã được cập nhật đầy đủ.
func (s *SyncEngine) markOrphanHashAsUnconnectable(hash []byte) {
	s.failedOrphanAttemptsMu.Lock()
	defer s.failedOrphanAttemptsMu.Unlock()
	key := hex.EncodeToString(hash)
	s.unconnectableHashes[key] = time.Now()
	log.Printf("[SYNC-ORPHAN] 🛑 Đã ghi nhận chuỗi Hash mồ côi không thể liên kết: %s. Tạm dừng xử lý chuỗi này trong 5 phút.", key[:12])
}





// SYNC_GRACE_PERIOD: Thời gian chờ tối thiểu sau khởi động trước khi cho phép đào.
// Tại sao: 5 khối đầu có độ khó cực thấp, node đào trong vài giây gây Fork.
// Phải đợi P2P tìm peer và bắt đầu sync trước.
const SYNC_GRACE_PERIOD = 30 * time.Second

func NewSyncEngine(ctx context.Context, netMgr *NetworkManager, mempool MempoolInterface) *SyncEngine {
	highest := netMgr.Bridge.GetCurrentVersion()

	s := &SyncEngine{
		ctx:              ctx,
		state:            Stalled,
		startupTime:      time.Now(),
		LastSyncActivity: time.Now(),
		netManager:       netMgr,
		mempool:          mempool,
		currentHeight:    highest,
		targetHeight:     netMgr.NetworkHeight,
		finalizedHeight:  netMgr.Bridge.GetFinalizedHeight(),
		pendingBlocks:    make(map[uint64][][]byte),
		orphanHeaders:    make(map[string]*pb_block.BlockHeader), // [LIGHTWEIGHT ORPHAN CACHE]
		orphanTxIDs:      make(map[string][][]byte),              // [RECONSTRUCTION-CACHE]
		orphanCoinbase:   make(map[string][]byte),                // [RECONSTRUCTION-CACHE]
		isProverNode:     true,
		orphanTracker:    make(map[string]*OrphanInvestigation),
		TriggerSyncChan:  make(chan struct{}, 1), // [EVENT-DRIVEN] Khởi tạo TriggerSyncChan với buffer = 1
		failedOrphanAttempts:  make(map[string]time.Time),
		unconnectableHashes:   make(map[string]time.Time),
		orphanSyncStrikes:     make(map[peer.ID]int),
		orphanSyncStrikesTs:   make(map[peer.ID]time.Time),
		knownSidechains:       make(map[string]time.Time),
		sidechainPeers:        make(map[peer.ID]time.Time),
	}
	netMgr.SyncEngine = s

	// [EVENT-DRIVEN-SYNC] Lắng nghe sự kiện TopicBlockReceived qua GlobalEventBus để đánh thức syncLoop
	go func() {
		ch, err := SubscribeEvent(s.ctx, TopicBlockReceived)
		if err != nil {
			log.Printf("[SYNC-BUS] ⚠️ Hệ thống Event Bus chưa được khởi tạo (Môi trường Test): %v", err)
			return
		}
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ch:
				s.TriggerSync()
			}
		}
	}()

	// [V2.1 FIX] KHÔNG set Synced ngay tại đây nữa.
	// Phải đợi Grace Period trôi qua trong IsSynced() để P2P có thời gian tìm peer.
	// Trước V2.1: nếu currentHeight >= targetHeight (0 khi 0 peer) → Synced ngay → đào ngay → FORK RISK
	if s.currentHeight >= s.targetHeight {
		// Đánh dấu tạm là Synced nhưng IsSynced() sẽ chặn bởi Grace Period
		s.state = Synced
		log.Printf("[SYNC] ⏳ Grace Period %v kích hoạt: Chờ P2P tìm Peer trước khi cho phép đào...", SYNC_GRACE_PERIOD)
	}

	// [V2.5 FIX] LUÔN khởi chạy vòng lặp bắt nhịp ngầm 1 lần duy nhất để theo dõi Peer trọn đời
	// [V2.0 SATOSHI-PUSH] Lắng nghe thông báo Inventory (INV) từ mạng lưới
	go func() {
		topic, err := s.netManager.JoinTopic("btc_genz_v1_inv")
		if err != nil {
			return
		}
		sub, err := topic.Subscribe()
		if err != nil {
			return
		}

		for {
			msg, err := sub.Next(s.ctx)
			if err != nil {
				return
			}
			if msg.ReceivedFrom == s.netManager.Host.ID() {
				continue
			}

			var inv pb_block.InventoryMsg
			if err := proto.Unmarshal(msg.Data, &inv); err == nil {
				// Cập nhật chiều cao Peer realtime để không bị mù đỉnh mạng lưới
				s.netManager.UpdatePeerHeight(msg.ReceivedFrom, inv.Height, s.netManager.Bridge.GetFinalizedHeight(), 0)

				delta := int64(inv.Height) - int64(s.currentHeight)
				log.Printf("[SYNC-INV] 📡 Nhận thông báo Khối mới #%d từ Peer %s. (Lệch: %+d khối)", inv.Height, msg.ReceivedFrom.String()[:12], delta)
				if inv.Height > s.currentHeight {
					// [VANGUARD-GUARD] Luôn đồng bộ theo đỉnh mạng, không có ngoại lệ đào độc lập
					s.StartSync(inv.Height)
				}
			}
		}
	}()

	if s.netManager.Host != nil {
		// [VANGUARD-STRICT-ORDER] Chạy Bootstrap xong mới cho phép chạy vồng lặp đồng bộ
		go func() {
			// ⏱️ CỬA SỔ AN TOÀN 15-30 (The 15-30 Window): Tránh đồng bộ vội vã khi vừa khởi động.
			// Tại sao: Khi node vừa chạy, các luồng P2P cần thời gian để thiết lập kết nối và trao đổi dữ liệu chiều cao qua giao thức.
			// Nếu đồng bộ ngay lập tức, node có thể bị kẹt hoặc nhảy cóc nhầm vào một mỏ neo cũ do chưa cập nhật đầy đủ danh sách Peer.
			// Việc trì hoãn 15 giây này đóng vai trò như một bộ đệm an toàn để thu thập chính xác đỉnh mạng thực tế từ toàn bộ Peer liên kết.
			log.Printf("[SAFETY-WINDOW] ⏱️ Kích hoạt Cửa sổ an toàn 15 giây (The 15-30 Window) để dò quét đỉnh mạng...")
			for i := 1; i <= 15; i++ {
				time.Sleep(1 * time.Second)
				s.netManager.PeerMutex.RLock()
				maxPeerH := uint64(0)
				peerCount := len(s.netManager.PeerHeights)
				for _, h := range s.netManager.PeerHeights {
					if h > maxPeerH {
						maxPeerH = h
					}
				}
				s.netManager.PeerMutex.RUnlock()
				log.Printf("[SAFETY-WINDOW] ⏱️ Quét giây thứ %d/15 | Đã kết nối: %d Peer | Đỉnh cao nhất phát hiện: #%d", i, peerCount, maxPeerH)
			}
			log.Printf("[SAFETY-WINDOW] ✅ Đã hoàn thành 15 giây cửa sổ an toàn. Bắt đầu kiểm tra điều kiện kích hoạt FastSync.")

			// Tìm đỉnh cao nhất của Peer sau cửa sổ an toàn để đánh giá khoảng cách đồng bộ
			s.netManager.PeerMutex.RLock()
			maxPeerH := uint64(0)
			for _, h := range s.netManager.PeerHeights {
				if h > maxPeerH {
					maxPeerH = h
				}
			}
			s.netManager.PeerMutex.RUnlock()

			currentHeight := s.netManager.Bridge.GetCurrentVersion()
			// [STARTUP-SYNC-LIMIT] Chỉ kích hoạt FastSync khi khởi động nếu đỉnh mạng xa quá trên 1000 khối
			// Tại sao: Đồng bộ tuần tự 1000 khối (tải block header + block body) có chi phí hiệu năng (CPU/RAM/Disk IO) nhẹ hơn rất nhiều so với việc tải cả tệp snapshot lớn rồi nạp và tái thiết lập toàn bộ JMT State Root trong RocksDB qua FFI. Do đó, ta nâng ngưỡng kích hoạt snapshot sync khi khởi động lên 1000 khối.
			if maxPeerH > currentHeight && maxPeerH - currentHeight > 1000 {
				log.Printf("[STARTUP-SYNC] 🚀 Lệch đỉnh mạng xa quá (%d - %d = %d > 1000 khối). Kích hoạt FastSync Bootstrap khi khởi động!", maxPeerH, currentHeight, maxPeerH - currentHeight)
				s.FastSyncBootstrap()
			} else {
				diff := int64(maxPeerH) - int64(currentHeight)
				log.Printf("[STARTUP-SYNC] 🚜 Đỉnh mạng không quá xa (Lệch: %+d khối). Bỏ qua FastSync tại startup, bắt đầu đồng bộ tuần tự.", diff)
			}
			s.syncLoop()
		}()

		go s.investigationRoutine() // [VANGUARD-ORPHAN] Kích hoạt thám tử điều tra mồ côi
	}

	return s
}

func (s *SyncEngine) TriggerSync() {
	select {
	case s.TriggerSyncChan <- struct{}{}:
	default:
		// Đã có tín hiệu chờ xử lý, bỏ qua để debounce tránh spam
	}
}

func (s *SyncEngine) StartSync(targetHeight uint64) {
	s.mu.Lock()
	s.targetHeight = targetHeight
	s.state = Syncing
	s.mu.Unlock()
	// Kích hoạt đánh thức syncLoop ngay lập tức
	s.TriggerSync()
}

func (s *SyncEngine) syncLoop() {
	// [VANGUARD-STABILITY] Bẫy lỗi để bảo vệ goroutine đồng bộ
	// [STACK-TRACE] In đầy đủ stack trace khi crash để dễ dàng gỡ lỗi
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FATAL-SYNC] 💀 Vòng lặp đồng bộ bị sập: %v\n%s", r, string(debug.Stack()))
			log.Printf("[FATAL-SYNC] 🔄 Khởi động lại sau 5 giây...")
			time.Sleep(5 * time.Second)
			go s.syncLoop()
		}
	}()

	// [SYNC-DDoS-PROTECTION] Bộ đếm lỗi tải header theo độ cao khối
	failedHeadersCount := make(map[uint64]int)

	// [EVENT-DRIVEN] Nhịp đập heartbeat định kỳ 10 giây dự phòng
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.TriggerSyncChan:
			// Đánh thức tức thời khi nhận sự kiện khối mới
		case <-heartbeat.C:
			// Kiểm tra định kỳ để tự phục hồi
		}
			// 🧠 SOVEREIGN CORE: RUST LÀ SỰ THẬT DUY NHẤT (NO SPLIT-BRAIN)
			actualRustHeight := s.netManager.Bridge.GetCurrentVersion()

			// [DỌN RÁC PENDING BLOCKS] Giải phóng bộ nhớ RAM các khối cũ đã qua chiều cao hiện tại
			// Tại sao: Nếu Node nhảy cóc (FastSync) hoặc Reorg, các khối cũ trong pendingBlocks sẽ bị kẹt vĩnh viễn trong RAM.
			s.fetchMu.Lock()
			for h := range s.pendingBlocks {
				if h <= actualRustHeight {
					delete(s.pendingBlocks, h)
				}
			}
			s.fetchMu.Unlock()

			s.mu.Lock()
			// Nếu Go nghĩ nó ở cao độ khác Rust, Go phải cập nhật lại để đồng nhất với nguồn chân lý Rust Core.
			if s.currentHeight != actualRustHeight {
				log.Printf("[RUST-CORE-TRUTH] ⚡ Cưỡng chế đồng bộ chiều cao: Go (#%d) -> Rust (#%d)", s.currentHeight, actualRustHeight)
				s.currentHeight = actualRustHeight

				// [INVISIBLE-HAND-FIX] Nếu chiều cao mới bị hạ thấp hơn mục tiêu đồng bộ,
				// cưỡng chế kích hoạt lại trạng thái Syncing để tiếp tục đồng bộ lại chuỗi đúng.
				if s.currentHeight < s.targetHeight && s.state != Syncing {
					log.Printf("[INVISIBLE-HAND-HEAL] 🩹 Phát hiện chiều cao bị lùi xuống dưới mục tiêu (#%d < #%d). Tái kích hoạt đồng bộ!", s.currentHeight, s.targetHeight)
					s.state = Syncing
				}
			}

			currentH := s.currentHeight
			targetH := s.targetHeight
			isSyncing := s.state == Syncing
			s.mu.Unlock()

			// [VANGUARD-STALL-PROTECTION] Kiểm tra Snapshot Chasing
			// Nếu ta đang đồng bộ nhưng bị kẹt ở vùng thấp mà toàn bộ mạng lưới đã thanh trừng (Pruned)
			if EnableSnapshotJumping && isSyncing && currentH < targetH {
				s.netManager.PeerMutex.RLock()
				canProvideNextBlock := false
				maxOldestH := uint64(0)

				// [DEADLOOP-FIX] Xây tập hợp peer THỰC SỰ đang kết nối để lọc bỏ ghost peer
				// Tại sao: PeerHeights map có thể chứa peer cũ đã disconnect nhưng chưa bị xóa,
				// với oldest=0 sẽ gây false positive cho canProvideNextBlock → Snapshot Chasing
				// không bao giờ kích hoạt → vòng lặp chết.
				var activePeers []peer.ID
				if s.netManager.Host != nil && s.netManager.Host.Network() != nil {
					activePeers = s.netManager.Host.Network().Peers()
				}
				activePeerSet := make(map[peer.ID]bool, len(activePeers))
				for _, p := range activePeers {
					activePeerSet[p] = true
				}

				for p, h := range s.netManager.PeerHeights {
					// Bỏ qua peer đã disconnect nhưng chưa bị xóa khỏi map
					if !activePeerSet[p] {
						continue
					}

					oldest := s.netManager.PeerOldestHeights[p]

					if oldest > maxOldestH {
						maxOldestH = oldest
					}

					// Chỉ xét các Peer có chiều cao lớn hơn ta
					if h > currentH {
						// Nếu vùng dữ liệu của Peer này bao trùm khối tiếp theo ta cần (currentH + 1)
						if oldest == 0 || oldest <= currentH+1 {
							canProvideNextBlock = true
							break // Chỉ cần 1 Peer cung cấp được là an toàn
						}
					}
				}
				s.netManager.PeerMutex.RUnlock()

				// Nếu KHÔNG CÒN AI có dữ liệu cho khối tiếp theo -> Ta đã bị bỏ rơi -> Nhảy cóc ngay!
				if !canProvideNextBlock && maxOldestH > currentH+1 {
					log.Printf("[SYNC-CRITICAL] 🚀 Kích hoạt 'Snapshot Chasing' để bám đuổi đỉnh mạng lưới!")
					
					s.mu.Lock()
					s.state = Bootstrapping
					s.mu.Unlock()

					go s.FastSyncBootstrap()
					return
				}
			}

			// [VANGUARD-STABILITY] Lấy danh sách peer NGOÀI khóa để tránh Deadlock với libp2p internals
			var peers []peer.ID
			if s.netManager.Host != nil && s.netManager.Host.Network() != nil {
				peers = s.netManager.Host.Network().Peers()
			}
			var targetPeer peer.ID
			var maxH uint64

			// [VANGUARD-DIAGNOSTIC] Log every 10 seconds to reduce IO pressure
			if time.Now().Second()%10 == 0 {
				hostID := "unknown"
				if s.netManager.Host != nil {
					hostID = s.shortID(s.netManager.Host.ID())
				}
				log.Printf("[SYNC-NETWORK] HostID: %s | Peers In Network: %d", hostID, len(peers))
			}

			// [VANGUARD-RESILIENCE] Tìm MaxH toàn mạng để đặt mục tiêu đồng bộ
			if len(peers) > 0 {
				s.netManager.PeerMutex.RLock()
				s.mu.RLock()
				for _, p := range peers {
					if cooldown, ok := s.sidechainPeers[p]; ok && time.Now().Before(cooldown) {
						continue
					}
					h := s.netManager.PeerHeights[p]
					if h > maxH {
						maxH = h
					}
				}
				s.mu.RUnlock()
				s.netManager.PeerMutex.RUnlock()
			}

			// [STALL-RECOVERY] Cơ chế tự phục hồi và CẢNH BÁO nếu bị kẹt quá lâu

			if targetPeer == "" && isSyncing && time.Now().Second()%10 == 0 {
				log.Printf("[SYNC-DEBUG] 📡 Đang chờ thông tin Cao độ từ %d Peer...", len(peers))
			}

			// [STALL-RECOVERY] Cơ chế tự phục hồi và CẢNH BÁO nếu bị kẹt quá lâu
			if maxH > currentH && time.Since(s.LastSyncActivity) > 20*time.Second {
				log.Printf("[SYNC-STALLED] %s", i18n.T("log_sync_stalled", currentH))
				s.LastSyncActivity = time.Now() // Reset timer để không spam recovery
			}

			// [V3.0 STABILITY] Bảo vệ biến targetH để tránh Race Condition nhẹ
			if maxH > targetH {
				log.Printf("[SYNC-CHASER] 📡 Phát hiện mạng lưới đã lên cao độ #%d (Mục tiêu cũ: #%d). Đang đuổi theo...", maxH, targetH)
				// [DEADLOCK-FIX] Gọi StartSync NGOÀI RLock.
				go s.StartSync(maxH)
				targetH = maxH
				isSyncing = true
			} else if maxH < targetH {
				// [SYNC-SPOOF-HEAL] Hạ mục tiêu đồng bộ ảo (chưa xác thực) từ chuỗi giả mạo khi peer giả mạo đã bị ngắt kết nối
				// Tại sao: Khi peer giả mạo bị ngắt kết nối, targetHeight ảo vẫn bị kẹt ở mức cao (ví dụ #3000)
				// trong khi mạng thực tế chỉ ở mức thấp (ví dụ #50). Điều này khiến Node bị kẹt ở trạng thái Syncing vô hạn
				// và vô hiệu hóa chức năng đào (Miner). Việc hạ targetHeight về đúng thực tế mạng sẽ giúp giải phóng trạng thái này và cho phép Miner hoạt động trở lại.
				newTargetH := maxH
				if currentH > newTargetH {
					newTargetH = currentH
				}
				if newTargetH < targetH {
					log.Printf("[SYNC-SPOOF-HEAL] 🛡️ Hạ mục tiêu đồng bộ ảo từ #%d xuống thực tế mạng lưới #%d để giải phóng Miner.", targetH, newTargetH)
					s.mu.Lock()
					s.targetHeight = newTargetH
					s.mu.Unlock()
					targetH = newTargetH
				}
			}

			if currentH >= targetH {
				s.mu.Lock()
				s.initialSyncDone = true
				if s.state != Synced {
					if s.state == Syncing || s.state == Stalled {
						s.state = Synced
						log.Printf("[SYNC] %s", i18n.T("log_sync_success", currentH))
					}
				}
				s.mu.Unlock()
				continue
			}

			// [SYNC-CATCHUP] Kích hoạt CatchUpSync nếu lệch xa mạng (> CatchUpThreshold khối) để tải chùm nhanh
			// [GENESIS-PROTECT] Chặn kích hoạt CatchUpSync khi cơ sở dữ liệu địa phương chưa sở hữu Khối Genesis (#0).
			// Tại sao: Nếu chưa có khối 0, lõi Rust khi thẩm định chuỗi tiêu đề từ khối #1 trở đi sẽ báo lỗi không tìm thấy tiêu đề cha (điểm rẽ) trong DB, gây ra vòng lặp lỗi CatchUp vô tận. Ta cần tải khối #0 tuần tự trước.
			// [FIXED] Nếu Node đã nhảy cóc bằng Snapshot (currentH > 0), ta bỏ qua chốt chặn Genesis.
			hasGenesis := len(s.netManager.Bridge.GetBlockHash(0)) > 0 || currentH > 0
			if isSyncing && hasGenesis && targetH > currentH {
				if targetH-currentH > CatchUpThreshold {
					bestPeer := s.netManager.SelectBestPeer(peers)

					if bestPeer != "" {
						log.Printf("[SYNC-CATCHUP] %s", i18n.T("log_sync_catchup"))
						s.CatchUpSync(bestPeer)
						continue
					}
				}
			}

			if !isSyncing {
				continue
			}

			// [VANGUARD-STRICT] LUÔN lấy cao độ THẬT từ Rust Core để xác định bước đi tiếp theo.
			// Tuyệt đối không tin vào biến cache của Go.
			actualH := s.netManager.Bridge.GetCurrentVersion()
			nextHeight := actualH + 1

			// [TIÊU CHUẨN ZERO TECHNICAL DEBT] Xác thực sự tồn tại thực tế của Khối đầy đủ (gồm cả Body)
			// Tại sao: Khi chạy FastSync, Go Node đã lưu trước toàn bộ Block Header của các khối tương lai vào DB. 
			// Do đó, nếu chỉ kiểm tra existingHash hoặc độ dài GetBlock (vốn > 0 kể cả khi chỉ có Header), Go Node 
			// sẽ bị đánh lừa là đã có khối đầy đủ, tự động tăng chiều cao ảo và bỏ qua việc tải Body từ peer, 
			// gây kẹt đồng bộ. Chúng ta giải mã block và chỉ chấp nhận bỏ qua nếu:
			//   1. Khối dưới mốc Finalized (chạy Header-Only dưới vùng snapshot).
			//   2. Hoặc khối thực sự chứa Body có các giao dịch (ví dụ giao dịch Coinbase).
			existingHash := s.netManager.Bridge.GetBlockHash(nextHeight)
			isHeaderOnlyZone := nextHeight < s.netManager.Bridge.GetFinalizedHeight()
			hasFullBlock := false
			if blockBytes := s.netManager.Bridge.GetBlock(nextHeight); len(blockBytes) > 0 {
				var tempBlock pb_block.Block
				if err := proto.Unmarshal(blockBytes, &tempBlock); err == nil {
					if tempBlock.Body != nil && len(tempBlock.Body.Transactions) > 0 {
						hasFullBlock = true
					}
				}
			}

			if len(existingHash) == 32 && (isHeaderOnlyZone || hasFullBlock) {
				s.mu.Lock()
				s.currentHeight = nextHeight
				s.mu.Unlock()
				log.Printf("[SYNC-GUARD] 🛡️ Rust Core đã sở hữu khối #%d (Tải trước hoặc do Miner tự đúc). Bỏ qua việc tải từ mạng.", nextHeight)
				continue
			}

			if currentH == 0 {
				// [VANGUARD-BOOTSTRAP] Kiểm tra xem khối Genesis thực sự đã tồn tại chưa
				if gHash := s.netManager.Bridge.GetBlockHash(0); len(gHash) == 0 {
					nextHeight = 0 // Chưa có Genesis -> phải tải #0 trước
					log.Printf("[SYNC-BOOT] 🆕 Node mới hoàn toàn. Đang yêu cầu khối Genesis #0 từ mạng lưới...")
				}
			}

			// [VANGUARD-DYNAMIC-PICK] Chọn Peer mục tiêu dựa trên cao độ cần tải
			s.netManager.PeerMutex.RLock()
			s.mu.RLock()
			var candidates []peer.ID
			for _, p := range peers {
				if cooldown, ok := s.sidechainPeers[p]; ok && time.Now().Before(cooldown) {
					continue
				}
				if s.netManager.PeerHeights[p] >= nextHeight {
					candidates = append(candidates, p)
				}
			}
			s.mu.RUnlock()
			s.netManager.PeerMutex.RUnlock()

			if len(candidates) == 0 {
				if time.Now().Second()%10 == 0 {
					log.Printf("[SYNC-WAIT] ⏳ Chưa tìm thấy Peer nào có khối #%d. Đang đợi...", nextHeight)
				}
				continue
			}

			// Chọn Peer tĩnh có độ ưu tiên cao nhất, nếu không có thì fallback
			targetPeer = s.netManager.SelectBestPeer(candidates)

			s.fetchMu.Lock()
			blockData, exists := s.pendingBlocks[nextHeight]
			if exists {
				delete(s.pendingBlocks, nextHeight)
			}
			s.fetchMu.Unlock()

			if !exists {
				log.Printf("[SYNC-HEADERS-FIRST] 📡 Đang yêu cầu Tiêu đề khối #%d từ Peer %s...", nextHeight, s.shortID(targetPeer))
				headerBytes, err := s.netManager.GetHeaderHashByHeight(s.ctx, targetPeer, nextHeight)
				if err != nil {
					log.Printf("[SYNC-HEADERS-FIRST] ⏳ Không thể tải tiêu đề #%d từ %s: %v. Thử Peer khác...", nextHeight, s.shortID(targetPeer), err)
					
					failedHeadersCount[nextHeight]++
					if failedHeadersCount[nextHeight] >= 6 {
						s.mu.Lock()
						s.syncFailures++
						s.mu.Unlock()
						failedHeadersCount[nextHeight] = 0 // Reset bộ đếm sau khi đã cộng dồn lỗi hệ thống
					}

					time.Sleep(500 * time.Millisecond)
					continue
				}
				
				// Reset bộ đếm lỗi của block này khi tải thành công
				delete(failedHeadersCount, nextHeight)

				// [BẢO MẬT/TỐI ƯU HÓA] Kiểm tra xem tiêu đề khối này có khớp với sidechain đã biết không
				blockHeaderHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
				hashStr := hex.EncodeToString(blockHeaderHash)
				s.mu.RLock()
				_, isSidechain := s.knownSidechains[hashStr]
				s.mu.RUnlock()
				if isSidechain {
					log.Printf("[SYNC-GUARD] 🛡️ Tiêu đề #%d từ %s khớp với sidechain đã biết (%s). Bỏ qua và thiết lập cooldown cho Peer.", nextHeight, s.shortID(targetPeer), hashStr[:12])
					s.mu.Lock()
					s.sidechainPeers[targetPeer] = time.Now().Add(5 * time.Minute)
					s.mu.Unlock()
					time.Sleep(500 * time.Millisecond)
					continue
				}

				// Xác thực PoW ngay tại tầng Go để bảo vệ Rust Core khỏi rác gRPC
				var tempHeader pb_block.BlockHeader
				if err := proto.Unmarshal(headerBytes, &tempHeader); err != nil {
					audit.AuditLog("INVALID_HEADER_DECODE", s.shortID(targetPeer), fmt.Sprintf("Tiêu đề khối #%d không hợp lệ (Unmarshal failed)", nextHeight))
					s.netManager.punishPeer(targetPeer, "Invalid header format during Sync")
					time.Sleep(1 * time.Second)
					continue
				}

				isValid, err := s.netManager.Bridge.VerifyPow(headerBytes, tempHeader.Nonce, tempHeader.Difficulty, tempHeader.Height)
				if err != nil {
					if strings.Contains(err.Error(), "DB_BUSY") {
						log.Printf("[SYNC-HEADERS-FIRST] ⏳ DB cục bộ đang bận khi xác thực PoW #%d. Thử lại sau.", nextHeight)
						time.Sleep(500 * time.Millisecond)
						continue
					}
					log.Printf("[SYNC-HEADERS-FIRST] ⚠️ Lỗi hệ thống khi xác thực PoW #%d: %v. Thử lại sau.", nextHeight, err)
					time.Sleep(500 * time.Millisecond)
					continue
				}
				if !isValid {
					audit.AuditLog("INVALID_POW_BLOCK", s.shortID(targetPeer), fmt.Sprintf("Chặn tiêu đề khối rác PoW #%d", nextHeight))
					s.netManager.punishPeer(targetPeer, "Invalid PoW during Sync")
					time.Sleep(1 * time.Second)
					continue
				}

				// Rust Core thẩm định Header này xem có nối tiếp chuỗi tốt không
				evalResp, err := s.netManager.Bridge.EvaluateHeaderChain([][]byte{headerBytes})
				if err != nil || evalResp == nil {
					errMsg := "gRPC error or empty response"
					if err != nil {
						errMsg = err.Error()
					}
					log.Printf("[SYNC-HEADERS-FIRST] ⏳ Lỗi khi thẩm định #%d: %s. Thử peer khác...", nextHeight, errMsg)
					time.Sleep(500 * time.Millisecond)
					continue
				}

				if evalResp.Status == 2 {
					if strings.Contains(evalResp.ErrorMsg, "Không tìm thấy điểm rẽ nhánh") || strings.Contains(evalResp.ErrorMsg, "not found") || strings.Contains(evalResp.ErrorMsg, "Parent hash") {
						s.netManager.PeerMutex.RLock()
						peerH := s.netManager.PeerHeights[targetPeer]
						s.netManager.PeerMutex.RUnlock()

						actualH := s.netManager.Bridge.GetCurrentVersion()

						if peerH > actualH && peerH-actualH > CatchUpThreshold {
							log.Printf("[SYNC-ORPHAN] 🧩 Lệch chuỗi sâu (%d > %d + %d). Kích hoạt CatchUpSync để tìm điểm giao nhau!", peerH, actualH, CatchUpThreshold)
							s.CatchUpSync(targetPeer)
						} else {
							log.Printf("[SYNC-ORPHAN] %s", i18n.T("log_sync_orphan"))
							missingHashBytes := tempHeader.ParentHash.Value

							// Đăng ký điều tra
							missingParentHashStr := hex.EncodeToString(missingHashBytes)[:12]
							s.orphanMu.Lock()
							s.orphanTracker[missingParentHashStr] = &OrphanInvestigation{
								MissingHash: missingHashBytes,
								Sender:      targetPeer,
								LastActive:  time.Now(),
								Height:      nextHeight - 1,
							}
							s.orphanMu.Unlock()

							if s.checkAndRecordOrphanAttempt(targetPeer, missingHashBytes) {
								if err := s.alignOrphanChain(targetPeer, missingHashBytes, headerBytes); err != nil {
									log.Printf("[SYNC-ERROR] ❌ Lỗi cân chỉnh chuỗi mồ côi cho lệch chuỗi ngắn: %v", err)
								}
							} else {
								log.Printf("[SYNC-ORPHAN] ⚠️ Bỏ qua cân chỉnh chuỗi mồ côi trùng lặp cho cha %x từ Peer %s trong thời gian cooldown.", missingHashBytes[:6], s.shortID(targetPeer))
							}
						}
					} else {
						if strings.Contains(evalResp.ErrorMsg, "ERR_IMMUTABLE_FIREWALL_VIOLATION") || strings.Contains(evalResp.ErrorMsg, "vi phạm") || strings.Contains(evalResp.ErrorMsg, "FIREWALL") {
							audit.AuditLog("FIREWALL_VIOLATION", s.shortID(targetPeer), fmt.Sprintf("Tiêu đề khối #%d vi phạm tường lửa bất biến: %s", nextHeight, evalResp.ErrorMsg))
							s.netManager.punishPeer(targetPeer, "VI PHẠM TƯỜNG LỬA BẤT BIẾN: "+evalResp.ErrorMsg)
							s.netManager.Host.Network().ClosePeer(targetPeer)
						} else {
							audit.AuditLog("HEADER_REJECTED", s.shortID(targetPeer), fmt.Sprintf("Tiêu đề khối #%d bị Rust Core từ chối: %s", nextHeight, evalResp.ErrorMsg))
							s.netManager.punishPeer(targetPeer, "Header rejected by Rust Core: "+evalResp.ErrorMsg)
						}
					}
					time.Sleep(1 * time.Second)
					continue
				}


				// [VANGUARD-OPTIMIZATION] Áp dụng quy tắc Header-Only dưới mốc Snapshot và Đại Thanh Trừng
				fH := s.netManager.Bridge.GetFinalizedHeight()
				oldestH := s.netManager.Bridge.GetOldestHeight()

				if nextHeight < fH || nextHeight < oldestH {
					log.Printf("[SYNC-LIGHTWEIGHT] 🕊️ Khối lịch sử #%d dưới mốc Snapshot (#%d) hoặc Đại Thanh Trừng (#%d). Chạy chế độ Header-Only.", nextHeight, fH, oldestH)

					// Bọc Header vào Block (với Body = nil) để Rust tự biết là Header-Only
					fullBlock := &pb_block.Block{
						Header: &tempHeader,
						Body:   nil,
					}
					blockBytes, err := proto.Marshal(fullBlock)
					if err != nil {
						log.Printf("[SYNC-ERROR] ❌ Lỗi đóng gói khối Header-Only #%d: %v", nextHeight, err)
						time.Sleep(500 * time.Millisecond)
						continue
					}
					blockData = [][]byte{blockBytes}
				} else {
					log.Printf("[SYNC-HEADERS-FIRST] 📥 Tiêu đề #%d hợp lệ! Tiến hành tải thân khối đầy đủ...", nextHeight)
					blockBytes, err := s.netManager.GetBlockFromNetwork(targetPeer, nextHeight)
					if err != nil {
						log.Printf("[SYNC-HEADERS-FIRST] ⏳ Không thể tải thân khối #%d từ %s: %v. Thử lại...", nextHeight, s.shortID(targetPeer), err)
						time.Sleep(500 * time.Millisecond)
						continue
					}
					log.Printf("[SYNC-AUDIT] 📥 Đã tải thành công %d bytes cho Khối #%d", len(blockBytes), nextHeight)
					blockData = [][]byte{blockBytes}
				}

				// [VANGUARD-HYPER-SYNC] Kích hoạt tải trước các khối tiếp theo (Batch Window: 2 - Chống OOM cho khối 100MB)
				go func(startH uint64, p peer.ID) {
					fH := s.netManager.Bridge.GetFinalizedHeight()
					oldestH := s.netManager.Bridge.GetOldestHeight()

					for i := uint64(1); i <= 2; i++ {
						h := startH + i
						if h > targetH || h < fH || h < oldestH {
							break
						}

						s.fetchMu.Lock()
						_, alreadyInBuf := s.pendingBlocks[h]
						s.fetchMu.Unlock()

						if !alreadyInBuf {
							bData, err := s.netManager.GetBlockFromNetwork(p, h)
							if err == nil {
								s.fetchMu.Lock()
								s.pendingBlocks[h] = [][]byte{bData}
								s.fetchMu.Unlock()
							} else {
								// [HOTFIX V1.20 - SILENT DROP] Pre-fetch timeout = Bỏ qua im lặng
								// Triết lý Bitcoin Core: Không trừng phạt lỗi kết nối.
							}

						}
					}
				}(nextHeight, targetPeer)
			}

			// Nhận diện khối rỗng thuộc vùng đã bị Pruned trong syncLoop
			// Tại sao: Nếu khối đã bị cắt tỉa (Body = nil hoặc 0 Transactions) được truyền trực tiếp xuống Rust Core,
			// nó sẽ làm sai lệch xác thực Merkle Root dẫn đến lỗi LỆCH TX ROOT và trừng phạt nhầm Peer vô tội.
			// Chúng ta bắt buộc phải đánh chặn ở tầng Go và chuyển sang chế độ Snapshot Sync ngay lập tức.
			// Ngược lại, nếu khối rỗng khai thác (Empty Block) do Miner đào, nó có ít nhất 1 Coinbase Transaction
			// (len == 1), nên hasBody = true và được xử lý hoàn toàn hợp lệ.
			if len(blockData) > 0 && len(blockData[0]) > 0 {
				hasBody := HasBodyAndTransactions(blockData[0])
				if nextHeight > 0 && !hasBody {
					log.Printf("[SYNC-CRITICAL] 🧹 Phát hiện khối chuẩn bị xử lý #%d là khối bị cắt tỉa (Pruned/Header-Only). Kích hoạt Snapshot Sync!", nextHeight)
					s.mu.Lock()
					if s.state != Bootstrapping {
						s.state = Bootstrapping
						go s.FastSyncBootstrap()
					}
					s.mu.Unlock()

					s.fetchMu.Lock()
					clear(s.pendingBlocks)
					s.fetchMu.Unlock()
					continue
				}
			}

			// [VANGUARD-CONSENSUS] Uỷ quyền 100% cho Rust Core thông qua Giao thức Đồng bộ Tập trung (Sync V4)
			// Go chỉ làm nhiệm vụ vận chuyển dữ liệu.
			resp, err := s.netManager.Bridge.ProcessChain(blockData)

			// [SYNC-HEAL-V2] LUÔN đồng bộ lại chiều cao từ nguồn chân lý (Rust Core)
			actualRustHeight = s.netManager.Bridge.GetCurrentVersion()
			if actualRustHeight != s.currentHeight {
				s.mu.Lock()
				log.Printf("[SYNC-HEAL] 🩹 Phát hiện lệch chiều cao! Đã đồng bộ lại Go (#%d) theo Rust (#%d)", s.currentHeight, actualRustHeight)
				s.currentHeight = actualRustHeight
				s.mu.Unlock()

				s.fetchMu.Lock()
				clear(s.pendingBlocks)
				s.fetchMu.Unlock()
			}

			if err != nil || (resp != nil && (resp.Status == 2 || resp.Status == 4)) { // Status 2 = INVALID_CHAIN, Status 4 = INTERNAL_ERROR
				errMsg := "Unknown error"
				if err != nil {
					errMsg = err.Error()
				} else if resp != nil {
					errMsg = resp.ErrorMsg
				}

				// Nếu lỗi do cố tình truy vấn trạng thái lịch sử đã bị Pruned
				// Tại sao: Nếu gặp lỗi này, ta cần dừng đồng bộ tuần tự và chuyển qua Snap Sync ngay lập tức.
				if strings.Contains(errMsg, "ERR_STATE_PRUNED") {
					log.Printf("[SYNC-CRITICAL] 🚑 Rust Core báo lỗi Pruned State! Ngừng đồng bộ tuần tự để tránh lệch StateRoot.")
					
					s.mu.Lock()
					s.state = Bootstrapping
					go s.FastSyncBootstrap() // Nhảy cóc ngay lập tức tới mỏ neo Snapshot mới nhất của Peer
					s.mu.Unlock()
					
					s.fetchMu.Lock()
					clear(s.pendingBlocks) // Xóa sạch hàng đợi rác
					s.fetchMu.Unlock()
					continue
				}

				if resp != nil && resp.Status == 4 {
					log.Printf("[SYNC-CRITICAL] 🛑 Lỗi nội bộ từ Database của chính Node này: %s. (Peer %s - Không phạt)", errMsg, s.shortID(targetPeer))
				} else if resp != nil && resp.Status == 2 && strings.Contains(errMsg, "LỆCH TX ROOT") {
					// Tại sao: Chuyển sang log kiểm toán an ninh để giám sát độc lập, tránh trôi log khi bị tấn công spam Tx.
					audit.AuditLog("TX_ROOT_MISMATCH", s.shortID(targetPeer), fmt.Sprintf("Gửi khối có LỆCH TX ROOT: %s", errMsg))
					s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi khối lệch Tx Root: %s", errMsg))
				} else if resp != nil && resp.Status == 2 && strings.Contains(errMsg, "LỆCH STATE ROOT") {
					peerRootErrorsMu.Lock()
					peerRootErrors[targetPeer]++
					errCount := peerRootErrors[targetPeer]
					peerRootErrorsMu.Unlock()

					if errCount <= 1 {
						log.Printf("[SYNC-HEAL] ⚠️ Phát hiện LỆCH STATE ROOT từ %s (Lần %d). Nghi ngờ RAM Cache hoặc DB dơ. Đang tỉa Cache lỗi và tự kết nối lại, KHÔNG PHẠT PEER.", s.shortID(targetPeer), errCount)
						
						// [Surgical Drop] Chỉ xóa đúng khối đang lỗi để chống DDoS cạn kiệt bộ nhớ đệm
						s.fetchMu.Lock()
						delete(s.pendingBlocks, nextHeight)
						s.fetchMu.Unlock()

						// Tỉa Cache Orphan cụ thể của khối này khỏi RAM
						if len(blockData) > 0 && len(blockData[0]) > 0 {
							if headerBytes, err := ExtractHeaderBytesFromBlockBytes(blockData[0]); err == nil {
								blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
								hashStr := hex.EncodeToString(blockHash)
								s.orphanHeadersMu.Lock()
								delete(s.orphanHeaders, hashStr)
								delete(s.orphanTxIDs, hashStr)
								delete(s.orphanCoinbase, hashStr)
								s.orphanHeadersMu.Unlock()
							}
						}

						// Ngắt kết nối để hủy session hiện tại, 5s sau reconnect sẽ tải Full Block sạch
						s.netManager.Host.Network().ClosePeer(targetPeer)
					} else {
						// Lần 2 trở đi: Hacker gửi khối giả liên tiếp hoặc Miner thực sự gian lận. TRỪNG PHẠT!
						// Tại sao: Ghi nhận sự cố lệch State Root lặp đi lặp lại từ một peer vào log kiểm toán an ninh.
						audit.AuditLog("STATE_ROOT_MISMATCH_REPEATED", s.shortID(targetPeer), fmt.Sprintf("Gửi chuỗi khối lệch STATE ROOT liên tiếp (%d lần): %s", errCount, errMsg))
						s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi chuỗi khối lệch State Root liên tiếp: %s", errMsg))

						peerRootErrorsMu.Lock()
						delete(peerRootErrors, targetPeer)
						peerRootErrorsMu.Unlock()
					}
				} else {
					log.Printf("[SYNC-ERROR] ❌ Lỗi ProcessChain tại Khối #%d: %s. (Peer %s - Không trừng phạt)",
						nextHeight, errMsg, s.shortID(targetPeer))
				}

				// [VANGUARD-AUTONOMOUS-FIX] Nếu sync lỗi, không tự ý rollback trạng thái nữa (để tránh phá hủy CF_ACC_HISTORY)

				// [SYNC-HEAL] Cơ chế tự chữa lành: Nếu là lỗi TOXIC (EISD), kích hoạt tự trị ngay lập tức
				s.mu.Lock()
				if strings.Contains(errMsg, "TOXIC_BRANCH") || strings.Contains(errMsg, "EISD") {
					// Tại sao: Nhánh độc (Toxic Branch) là mối nguy hại lớn cho đồng thuận, cần ghi ngay vào log an ninh.
					audit.AuditLog("TOXIC_BRANCH_DETECTED", s.shortID(targetPeer), fmt.Sprintf("Phát hiện NHÁNH ĐỘC (Toxic Branch): %s", errMsg))
					s.syncFailures = 3 // Nhảy thẳng lên ngưỡng tự trị
				} else if s.lastFailedHeight == nextHeight {
					s.syncFailures++
				} else {
					s.lastFailedHeight = nextHeight
					s.syncFailures = 1
				}
				failures := s.syncFailures
				s.mu.Unlock()

				log.Printf("[SYNC-RETRY] ⚠️ Khối #%d có vấn đề (Có thể do 'Ghi sai' hoặc lỗi băm). Đang xóa cache và tải lại lần %d...", nextHeight, failures)

				// Xóa cache của khối lỗi để buộc phải tải lại từ Peer khác
				s.fetchMu.Lock()
				delete(s.pendingBlocks, nextHeight)
				s.fetchMu.Unlock()

				// [VANGUARD-DEEP-HEAL] Tự động lùi 1 khối nếu DB cục bộ liên tục tính sai State Root
				if failures == 5 && actualRustHeight > s.netManager.Bridge.GetFinalizedHeight() {
					log.Printf("[SYNC-DEEP-HEAL] 🚑 Node liên tục thất bại tại #%d. Nghi ngờ DB cục bộ bị dơ tại #%d. Tự động Rollback về #%d để làm sạch!", nextHeight, actualRustHeight, actualRustHeight-1)
					s.netManager.Bridge.RollbackState(nil, actualRustHeight, actualRustHeight-1)
					
					s.mu.Lock()
					s.currentHeight = actualRustHeight - 1
					s.syncFailures = 0
					s.mu.Unlock()
					
					s.fetchMu.Lock()
					clear(s.pendingBlocks)
					s.fetchMu.Unlock()
					
					time.Sleep(2 * time.Second)
					continue
				}

				if failures >= 10 {
					log.Printf("[SYNC-CRITICAL] 🚨 Thử lại quá nhiều lần tại #%d. Đang tìm nguồn dữ liệu mới...", nextHeight)
					s.mu.Lock()
					s.syncFailures = 0
					s.mu.Unlock()
				}
				time.Sleep(2 * time.Second)
				continue

			} else if resp != nil && resp.Status == 1 { // REORG_SUCCESS
				s.mu.Lock()
				s.currentHeight = resp.NewHeight
				s.LastSyncActivity = time.Now()
				s.mu.Unlock()

				log.Printf("[SYNC-AUDIT] ✅ Chuỗi đã được Rust SCL chấp nhận. Đỉnh mới: #%d", resp.NewHeight)

				// [VANGUARD-REORG] Thu hồi các giao dịch mồ côi về Mempool
				if len(resp.OrphanedTxsRaw) > 0 {
					log.Printf("[REORG-MEMPOOL] ♻️ Rust trả về %d giao dịch mồ côi. Đang khôi phục lại Mempool...", len(resp.OrphanedTxsRaw))
					for _, txRaw := range resp.OrphanedTxsRaw {
						s.mempool.PushToTxBus(txRaw, false)
					}
					log.Printf("[REORG-MEMPOOL] ✅ Đã xử lý khôi phục %d giao dịch mồ côi.", len(resp.OrphanedTxsRaw))
				}

				// [V35 CONCORDANCE] Thông báo cho Tracker
				if s.netManager.OnBlockCommitted != nil {
					go s.netManager.OnBlockCommitted(resp.NewHeight)
				}

				log.Printf("[SUCCESS] ✅ Đồng bộ thành công theo chỉ thị của Rust Core.")

			} else if resp.Status == 3 { // ORPHAN
				missingParentHashStr := resp.MissingParentHash[:12]
				log.Printf("[SYNC-AUDIT] 🚨 Khối #%d là MỒ CÔI (Thiếu cha %s).", nextHeight, missingParentHashStr)

				// 1. Chặn vòng lặp RAM
				s.fetchMu.Lock()
				clear(s.pendingBlocks)
				s.fetchMu.Unlock()

				missingHashBytes, _ := hex.DecodeString(resp.MissingParentHash)

				// Đăng ký điều tra để theo dõi peer
				s.orphanMu.Lock()
				s.orphanTracker[missingParentHashStr] = &OrphanInvestigation{
					MissingHash: missingHashBytes,
					Sender:      targetPeer,
					LastActive:  time.Now(),
					Height:      nextHeight - 1,
				}
				s.orphanMu.Unlock()

				actualH := s.netManager.Bridge.GetCurrentVersion()

				// [SPOOF-HEIGHT-SHIELD] Sử dụng nextHeight (chiều cao thực của khối mồ côi đã qua PoW) thay vì peerH tự khai báo của Peer để chống Spoofed Height DoS
				if nextHeight > actualH && nextHeight-actualH > CatchUpThreshold {
					log.Printf("[SYNC-ORPHAN] 🧩 Lệch chuỗi sâu thực tế (%d > %d + %d) phát hiện tại ProcessChain. Kích hoạt CatchUpSync để tìm điểm giao nhau!", nextHeight, actualH, CatchUpThreshold)
					s.CatchUpSync(targetPeer)
				} else {
					log.Printf("[SYNC-ORPHAN] %s", i18n.T("log_sync_orphan"))
					var orphanHeaderRaw []byte
					if len(blockData) > 0 && len(blockData[0]) > 0 {
						if headerBytes, err := ExtractHeaderBytesFromBlockBytes(blockData[0]); err == nil {
							orphanHeaderRaw = headerBytes
						}
					}
					if err := s.alignOrphanChain(targetPeer, missingHashBytes, orphanHeaderRaw); err != nil {
						log.Printf("[SYNC-ERROR] ❌ Lỗi tải lùi nhanh chuỗi đồng bộ chùm: %v", err)
					}
				}

				s.fetchMu.Lock()
				delete(s.pendingBlocks, nextHeight)
				s.fetchMu.Unlock()
				time.Sleep(1 * time.Second)
				continue
			} else if resp.Status == 0 { // ACCEPTED (Side-chain)
				log.Printf("[SYNC-SIDECHAIN] 🌾 Đã lưu khối #%d nhưng chuỗi này nhẹ hơn chuỗi chính hiện tại.", nextHeight)
				
				// [VÁ LỖI KẸT CHUỖI NHẸ CHÚNG] Ghi nhớ block sidechain này để tránh yêu cầu lại và đặt cooldown cho Peer
				if len(blockData) > 0 && len(blockData[0]) > 0 {
					if headerBytes, err := ExtractHeaderBytesFromBlockBytes(blockData[0]); err == nil {
						blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
						hashStr := hex.EncodeToString(blockHash)
						s.mu.Lock()
						s.knownSidechains[hashStr] = time.Now()
						s.sidechainPeers[targetPeer] = time.Now().Add(5 * time.Minute)
						s.mu.Unlock()
						log.Printf("[SYNC-SIDECHAIN] 🌾 Ghi nhớ block sidechain: %s và đặt cooldown 5p cho Peer %s", hashStr[:12], s.shortID(targetPeer))
					}
				}

				s.mu.Lock()
				s.syncFailures = 0 
				// [VÁ LỖI ĐỒNG BỘ CHIỀU CAO] TUYỆT ĐỐI KHÔNG gán s.currentHeight = nextHeight ở đây.
				// Tại sao: Do chuỗi này nhẹ hơn chuỗi chính hiện tại, Rust Core chưa chuyển đổi sang chuỗi này (vẫn giữ nguyên đỉnh chính).
				// Nếu Go tự ý tăng currentHeight lên nextHeight, Go sẽ bị lệch pha với Rust Core ở vòng lặp tiếp theo và bị đồng bộ đè chiều cao bởi RUST-CORE-TRUTH.
				// Chúng ta phải đồng bộ currentHeight trực tiếp từ chiều cao thực tế của Rust Core.
				s.currentHeight = s.netManager.Bridge.GetCurrentVersion()
				s.mu.Unlock()
				continue

			} else { // REJECTED (Status 2)
				log.Printf("[SYNC-REJECT] ❌ Rust Core từ chối chuỗi từ %s: %s", s.shortID(targetPeer), resp.ErrorMsg)

				// [VÁ LỖI ĐỒNG BỘ CHIỀU CAO] Nếu không có lỗi cụ thể (ErrorMsg rỗng), không phạt peer.
				if resp.ErrorMsg == "" {
					log.Printf("[SYNC-REJECT-WARN] ⚠️ Nhận phản hồi từ chối từ %s nhưng không có thông báo lỗi cụ thể. Bỏ qua, KHÔNG PHẠT PEER.", s.shortID(targetPeer))
				} else if strings.Contains(resp.ErrorMsg, "Không tìm thấy điểm rẽ nhánh") || strings.Contains(resp.ErrorMsg, "not found") || strings.Contains(resp.ErrorMsg, "Parent hash") {
					// NHÓM 2: Lệch chuỗi sâu hoặc mồ côi
					log.Printf("[SYNC-ORPHAN] 🧩 Node lệch chuỗi sâu hoặc thiếu cha so với Peer %s. KHÔNG PHẠT PEER, giữ kết nối để cơ chế phục hồi tự giải quyết.", s.shortID(targetPeer))
				} else {
					// NHÓM 1: Cố tình gian lận (PoW sai, Time-Warp, StateRoot sai...)
					// Tại sao: Lỗi từ chối nghiêm trọng từ Rust Core biểu hiện hành vi gian lận dữ liệu, cần log kiểm toán.
					audit.AuditLog("INVALID_CHAIN_DATA", s.shortID(targetPeer), fmt.Sprintf("Rust Core từ chối khối dữ liệu gian lận: %s", resp.ErrorMsg))
					s.netManager.punishPeer(targetPeer, fmt.Sprintf("Rust Core Rejected: %s", resp.ErrorMsg))
				}

				s.mu.Lock()
				s.syncFailures++
				if s.syncFailures >= 10 {
					log.Printf("[FIREWALL-CRITICAL] 🛑 Thất bại liên tiếp (%d lần). Kiểm tra lại kết nối gRPC hoặc DB.", s.syncFailures)
				}
				s.mu.Unlock()
				time.Sleep(2 * time.Second)
				continue
			}
	}
}

// [V2.5 ULTRA] FastSyncBootstrap: Quy trình đồng bộ siêu tốc 5 giai đoạn (Ultralight Bootstrap)
func (s *SyncEngine) FastSyncBootstrap() {
	if !EnableSnapshotJumping {
		log.Printf("[FAST-SYNC] 🛑 Lệnh kích hoạt FastSync bị từ chối: Tính năng Nhảy vọt Snapshot đã bị vô hiệu hóa rõ ràng.")
		return
	}

	s.mu.Lock()
	if s.bootstrapRunning {
		s.mu.Unlock()
		log.Printf("[FAST-SYNC] 🚨 Một phiên FastSyncBootstrap khác đang chạy. Bỏ qua yêu cầu này.")
		return
	}
	s.bootstrapRunning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.bootstrapRunning = false
		s.snapshotChunksLoaded = 0
		s.snapshotChunksTotal = 0
		// [SYNC-HEAL] Luôn đồng bộ lại chiều cao từ nguồn chân lý Rust Core khi kết thúc phiên FastSync
		actualRustHeight := s.netManager.Bridge.GetCurrentVersion()
		s.currentHeight = actualRustHeight
		// [ANTI-BRICK FIX] Nếu thất bại và state vẫn ở Bootstrapping, trả lại trạng thái Syncing để syncLoop tiếp tục hoạt động
		if s.state == Bootstrapping {
			s.state = Syncing
		}
		s.mu.Unlock()

		if r := recover(); r != nil {
			log.Printf("[FATAL-FAST-SYNC] 💀 FastSync sập: %v\n%s", r, string(debug.Stack()))
		}
	}()

	if s.netManager.Host == nil {
		log.Printf("[FAST-SYNC-TEST] 🧪 Phát hiện môi trường Test Mock (Host is nil). Thoát Bootstrap để phục vụ Unit Tests.")
		return
	}

	// [PEER-BLACKLIST-FILTER] Ghi nhớ các peer đã thử nhưng không giữ snapshot/manifest hoặc bị lỗi tải.
	// Nhằm loại bỏ việc hỏi lặp đi lặp lại một peer không có dữ liệu, tránh bị kẹt trong vòng lặp vô tận.
	triedPeers := make(map[peer.ID]bool)
	maxAttemptedAnchor := make(map[peer.ID]uint64) // [FALLBACK-SNAPSHOT] Theo dõi mốc mỏ neo cao nhất đã thử thất bại của từng peer

	for retry := 1; retry <= 60; retry++ {
		time.Sleep(1 * time.Second)

		syncMode := s.netManager.SyncMode
		if syncMode == "" {
			syncMode = "snap"
		}

		// [INVISIBLE-HAND-FIX] Bỏ qua kiểm tra currentH > 0 thô bạo ở đây để cho phép Snapshot Chasing
		// khi node đã có dữ liệu nhưng bị kẹt ở vùng thấp và cần đuổi theo (chase) mỏ neo cao hơn của mạng.
		// Việc kiểm tra sẽ được thực hiện sau khi xác định được bestAnchor.

		peers := s.netManager.Host.Network().Peers()
		if len(peers) == 0 {
			log.Printf("[FAST-SYNC] ⚠️ Lần thử %d: Chưa tìm thấy Peer nào.", retry)
			continue
		}

		var bestPeer peer.ID
		var tipHeight uint64
		var bestAnchor uint64

		s.netManager.PeerMutex.RLock()
		log.Printf("[DEBUG-SYNC] 🔍 Kiểm tra danh sách Peer (Tổng: %d)", len(s.netManager.PeerHeights))
		for p, h := range s.netManager.PeerHeights {
			if triedPeers[p] {
				continue
			}
			fh := s.netManager.PeerFinalizedHeights[p]
			oh := s.netManager.PeerOldestHeights[p]
			log.Printf("[DEBUG-SYNC-DETAIL] Peer: %s | H: %d | FH: %d | OH: %d", p.String(), h, fh, oh)

			if syncMode == "full" {
				// Chế độ FULL: Tìm mỏ neo cũ nhất mà Peer thực sự còn giữ
				// Căn lề theo Epoch để đảm bảo tính toàn vẹn của kỉ nguyên dữ liệu
				// Thêm 10 khối đệm an toàn.
				peerOldestAligned := ((oh + 10 + (EpochLength - 1)) / EpochLength) * EpochLength

				if h > tipHeight {
					tipHeight = h
					bestPeer = p
					bestAnchor = peerOldestAligned
					if bestAnchor > h {
						bestAnchor = 0
					} // Fallback nếu Peer quá lùn
				}
			} else {
				// Chế độ SNAP (Nhảy cóc): Tìm mỏ neo mới nhất thực tế (bội số SnapshotInterval đã có snapshot)
				// Snapshot tại X chỉ tồn tại nếu peer đạt chiều cao X + 300 (tức là h >= X + 300)
				// Cần lùi 300 khối làm vùng đệm an toàn để tránh Reorg
				var peerAnchor uint64
				if h >= SnapshotInterval + 1152 {
					peerAnchor = ((h - 1152) / SnapshotInterval) * SnapshotInterval + 1
				}

				// [FALLBACK-SNAPSHOT] Điều chỉnh mỏ neo dựa trên lịch sử đã thử thất bại của peer đó
				if limit, ok := maxAttemptedAnchor[p]; ok {
					if peerAnchor >= limit {
						if limit >= SnapshotInterval {
							peerAnchor = limit - SnapshotInterval
						} else {
							peerAnchor = 0
						}
					}
				}

				log.Printf("[FAST-SYNC-SNAP] 🔍 Peer: %s | Chiều cao: %d | Mỏ neo lý thuyết (fh): %d | Mỏ neo thực tế khả dụng sau điều chỉnh: %d", p.String()[:12], h, fh, peerAnchor)
				if peerAnchor > bestAnchor {
					bestAnchor = peerAnchor
					bestPeer = p
					tipHeight = h
				}
			}
		}
		s.netManager.PeerMutex.RUnlock()

		if bestPeer == "" || (bestAnchor == 0 && syncMode == "snap") {
			log.Printf("[FAST-SYNC] ⚠️ Lần thử %d: Chưa tìm thấy mỏ neo phù hợp cho chế độ %s.", retry, syncMode)
			if syncMode == "full" {
				log.Printf("[FAST-SYNC] 🚜 Chuyển sang cày cuốc từ khối 0.")
				return
			}
			continue
		}

		// [INVISIBLE-HAND-FIX] Nếu chiều cao Finalized thực tế đã đạt hoặc vượt qua mỏ neo, ta không cần nhảy cóc nữa
		actualFinalized := s.netManager.Bridge.GetFinalizedHeight()
		if actualFinalized >= bestAnchor {
			log.Printf("[FAST-SYNC] 🛡️ Chiều cao Finalized thực tế (#%d) đã lớn hơn hoặc bằng mỏ neo #%d. Hủy bỏ Snapshot Chasing.", actualFinalized, bestAnchor)
			return
		}

		// [FAST-SYNC-PRECHECK] Kiểm tra manifest nhanh trước khi tải headers
		// Tại sao: Nếu peer không có snapshot này (trả về 36 byte 0), ta lập tức lùi mỏ neo của peer mà không cần
		// tốn công tải và ghi hàng vạn headers vào RocksDB, giúp lùi mỏ neo siêu tốc về mốc khả dụng thực tế.
		if syncMode == "snap" {
			for {
				manifestData, err := s.netManager.DownloadSnapshotManifest(bestPeer, bestAnchor)
				if err != nil {
					log.Printf("[FAST-SYNC-PRECHECK] ❌ Không thể tải manifest từ %s: %v. Bỏ qua precheck cho mỏ neo #%d.", bestPeer.String()[:12], err, bestAnchor)
					break
				}
				if len(manifestData) < 36 {
					break
				}
				numChunks := binary.LittleEndian.Uint32(manifestData[32:36])
				// [BẢO MẬT] Lớp 1: Xác thực độ lớn và giới hạn cứng (Anti-DoS & Anti-Panic)
				// Đặt giới hạn cứng 25,000 chunks (tương đương 50GB) và kiểm tra độ dài manifest khớp chính xác
				if numChunks > 25000 {
					log.Printf("[FAST-SYNC-PRECHECK] 🚨 Số lượng chunk của Peer %s vượt giới hạn an toàn (%d > 25000).", bestPeer.String()[:12], numChunks)
					break
				}
				if len(manifestData) != 36+int(numChunks)*32 {
					log.Printf("[FAST-SYNC-PRECHECK] 🚨 Kích thước manifest của Peer %s không khớp với số chunk khai báo (%d != %d).", bestPeer.String()[:12], len(manifestData), 36+numChunks*32)
					break
				}
				manifestRoot := manifestData[0:32]
				isAllZeroRoot := true
				for _, b := range manifestRoot {
					if b != 0 {
						isAllZeroRoot = false
						break
					}
				}
				if isAllZeroRoot || numChunks == 0 {
					log.Printf("[FAST-SYNC-PRECHECK] ℹ️ Peer %s báo không có snapshot tại #%d. Lùi mỏ neo...", bestPeer.String()[:12], bestAnchor)
					maxAttemptedAnchor[bestPeer] = bestAnchor
					if bestAnchor >= SnapshotInterval {
						bestAnchor -= SnapshotInterval
					} else {
						bestAnchor = 0
						break
					}
				} else {
					// Tìm thấy mỏ neo có manifest thực sự! Thoát vòng lặp con để tiến hành tải headers
					log.Printf("[FAST-SYNC-PRECHECK] ✅ Tìm thấy mỏ neo khả dụng thực tế: #%d trên Peer %s", bestAnchor, bestPeer.String()[:12])
					break
				}
			}
			if bestAnchor == 0 {
				log.Printf("[FAST-SYNC] ⚠️ Peer %s không có bất kỳ snapshot khả dụng nào.", bestPeer.String()[:12])
				triedPeers[bestPeer] = true
				continue
			}
			// Kiểm tra lại sau khi điều chỉnh lùi mỏ neo
			if actualFinalized >= bestAnchor {
				log.Printf("[FAST-SYNC] 🛡️ Chiều cao Finalized thực tế (#%d) đã lớn hơn hoặc bằng mỏ neo điều chỉnh #%d. Hủy bỏ Snapshot Chasing.", actualFinalized, bestAnchor)
				return
			}
		}

		log.Printf("[FAST-SYNC] %s", i18n.T("log_fast_sync_start", bestAnchor))

		// [VANGUARD-DYNAMISM] Cập nhật ngay mục tiêu đồng bộ và trạng thái Syncing lên màn hình để Commander đỡ hóng
		s.mu.Lock()
		s.targetHeight = tipHeight
		s.state = Syncing
		s.mu.Unlock()

		log.Printf("[VANGUARD-SYNC] 🛡️ Bắt đầu thẩm định năng lượng từ khối #0 đến tận đỉnh chuỗi #%d để làm Tấm khiên năng lượng...", tipHeight)

		// [VANGUARD-FIX] Tải TOÀN BỘ Header từ 0 đến tận đỉnh tipHeight để làm "Tấm khiên năng lượng" PoW bảo vệ mỏ neo.
		// Tại sao: Bắt buộc Peer phải cung cấp PoW của 100 khối tương lai kể từ mỏ neo để chứng minh đây là chuỗi sống, ngăn chặn việc hacker tạo chuỗi giả mỏ neo rồi lừa nạp snapshot bậy.
		// [MAINNET-TIMEOUT] Không đặt timeout tổng (300s) cho cả chuỗi Header vì khi chain quá dài
		// sẽ gây lỗi vĩnh viễn không đồng bộ được. Hãy sử dụng s.ctx và để từng batch con tự quản lý
		// bằng Read Deadline (30s) của chính nó trong DownloadHeaderBatch.
		hBatch, err := s.netManager.DownloadHeaderBatch(s.ctx, bestPeer, 0, uint32(tipHeight+1))
		if err != nil {
			log.Printf("[FAST-SYNC] ❌ Tải chuỗi Header Vanguard thất bại từ %s: %v. Thử Peer khác...", bestPeer.String()[:12], err)
			triedPeers[bestPeer] = true // Ghi nhớ lỗi kết nối/tải để chuyển qua thử peer khác trong phiên này
			continue
		}

		// Gọi Rust thẩm định toàn bộ chuỗi Header đến tận đỉnh chuỗi (PoW + LWMA)
		evalResp, err := s.netManager.Bridge.EvaluateHeaderChain(hBatch)
		if err != nil {
			log.Printf("[FAST-SYNC] ❌ Lỗi gọi thẩm định Header: %v", err)
			continue
		}
		if evalResp.Status == 2 { // INVALID
			if strings.Contains(evalResp.ErrorMsg, "ERR_IMMUTABLE_FIREWALL_VIOLATION") || strings.Contains(evalResp.ErrorMsg, "vi phạm") || strings.Contains(evalResp.ErrorMsg, "FIREWALL") {
				// Tại sao: Vi phạm tường lửa bất biến là nỗ lực Reorg trái phép lịch sử đã hoàn thành, thuộc mức độ an ninh tối cao.
				audit.AuditLog("SNAPSHOT_FIREWALL_VIOLATION", s.shortID(bestPeer), fmt.Sprintf("Vi phạm tường lửa bất biến khi kiểm tra Snapshot: %s", evalResp.ErrorMsg))
				s.netManager.punishPeer(bestPeer, fmt.Sprintf("VI PHẠM TƯỜNG LỬA BẤT BIẾN TRONG SNAPSHOT: %s", evalResp.ErrorMsg))
				s.netManager.Host.Network().ClosePeer(bestPeer)
			} else {
				// Tại sao: Chuỗi tiêu đề giả mạo PoW nhằm đánh lừa mỏ neo nhảy vọt.
				audit.AuditLog("SNAPSHOT_INVALID_HEADER", s.shortID(bestPeer), fmt.Sprintf("Peer gửi chuỗi Header giả mạo trong Snapshot: %s", evalResp.ErrorMsg))
			}
			continue
		}

		// [VANGUARD-ENERGY-SHIELD] Trích xuất tiêu đề tại mốc mỏ neo (bestAnchor) nằm trong chùm để lấy StateRoot.
		// Tại sao: Do hBatch tải đến tận đỉnh chuỗi (tipHeight), tiêu đề mỏ neo sẽ nằm chính xác tại chỉ số bestAnchor trong mảng.
		// [FIXED] Tránh panic index out of range nếu hBatch nhận được có kích thước nhỏ hơn bestAnchor.
		// Nếu kích thước hBatch nhỏ hơn mỏ neo, tức là peer "nổ" chiều cao ảo hoặc thiếu headers lịch sử,
		// ta tự động lùi mỏ neo của peer này về mốc thực tế mà peer có thể phục vụ được.
		if int(bestAnchor) >= len(hBatch) {
			log.Printf("[FAST-SYNC] ❌ Kích thước hBatch (%d) nhỏ hơn mỏ neo yêu cầu (%d) từ %s. Tự động lùi mỏ neo của peer này về mốc khả thi thực tế...", len(hBatch), bestAnchor, bestPeer.String()[:12])
			var validAnchor uint64
			if len(hBatch) >= int(SnapshotInterval) {
				validAnchor = uint64(len(hBatch)/int(SnapshotInterval)) * SnapshotInterval + 1
			}
			maxAttemptedAnchor[bestPeer] = validAnchor + SnapshotInterval
			continue
		}
		var anchorHeader pb_block.BlockHeader
		if err := proto.Unmarshal(hBatch[bestAnchor], &anchorHeader); err != nil {
			log.Printf("[FAST-SYNC] ❌ Lỗi giải mã Header mỏ neo: %v", err)
			continue
		}

		// [V38.2 CHỐNG PANIC] Kiểm tra StateRoot trước khi truy cập
		if anchorHeader.StateRoot == nil {
			log.Printf("[FAST-SYNC] ❌ Header mỏ neo #%d không có StateRoot. Bỏ qua.", bestAnchor)
			continue
		}

		// [VANGUARD-HOTFIX] BƯỚC 1: LƯU TOÀN BỘ HEADER TỪ GENESIS ĐẾN MỎ NEO VÀO DB
		// Mạng lưới nhảy cóc Snapshot nhưng không được quên cội nguồn (Header).
		// LWMA và ParentHash cần chuỗi Header này để hoạt động ổn định.
		log.Printf("[FAST-SYNC] 🛠️ BƯỚC 1: Đang ghi %d Headers từ Genesis vào RocksDB...", len(hBatch))
		for i, headerBytes := range hBatch {
			var hdr pb_block.BlockHeader
			if err := proto.Unmarshal(headerBytes, &hdr); err != nil {
				continue
			}

			// Bọc Header vào Block (với Body rỗng) để tương thích với hàm SaveBlockRaw của Rust
			fullBlock := &pb_block.Block{
				Header: &hdr,
				Body:   nil, // [V2.5.1] Không gán Body giả mạo, để Rust tự biết là Header-Only
			}
			fullBlockBytes, _ := proto.Marshal(fullBlock)

			// [CRITICAL-FIX] Sử dụng hàm tính Hash chuẩn của Rust Core
			hash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
			if len(hash) == 0 {
				continue
			}

			// [IPC-OPTIMIZATION] Tối ưu hóa IPC: Tránh gọi IPC xuống Rust Core nếu Block Header này đã tồn tại và khớp Hash
			existHash := s.netManager.Bridge.GetBlockHash(hdr.Height)
			if len(existHash) == 32 && bytes.Equal(existHash, hash) {
				continue
			}

			// Lưu vào Rust Core
			s.netManager.Bridge.SaveBlockRaw(hdr.Height, hash, fullBlockBytes, true)
			s.netManager.Bridge.CommitBlockHash(hdr.Height, hash)

			// [VANGUARD-YIELD] Nhường CPU định kỳ cho Go Scheduler để giữ kết nối P2P và xử lý Heartbeat
			if i%500 == 0 {
				time.Sleep(2 * time.Millisecond)
			}
		}
		log.Printf("[FAST-SYNC] ✅ Đã hoàn tất BƯỚC 1 (Header Persistence).")

		// [VANGUARD-SYNC-V2] BƯỚC 2: Kích hoạt Đồng bộ Phân mảnh V2 (BitTorrent-style Monolithic Byte-Range File Sync)
		// Tại sao: 
		//   - Tránh việc tải cả file lớn bị gián đoạn giữa chừng, hỗ trợ resume từ mốc byte sạch gần nhất.
		//   - So khớp Blake3 tức thời của từng chunk 2MB để phát hiện dữ liệu lỗi/giả mạo của peer trước khi ghi.
		//   - Tách biệt logic tải dữ liệu (Go) và nạp Sổ cái (Rust) thông qua ImportStateSnapshotPath.
		log.Printf("[FAST-SYNC-V2] 📥 BƯỚC 2: Tải Manifest và File Snapshot tại #%d từ %s... (Root: %x)",
			bestAnchor, bestPeer.String()[:12], anchorHeader.StateRoot.Value)

		// 1. Tải Manifest từ bestPeer
		manifestData, err := s.netManager.DownloadSnapshotManifest(bestPeer, bestAnchor)
		if err != nil {
			log.Printf("[FAST-SYNC-V2] ❌ Không thể tải Manifest từ %s: %v. Thử Peer khác...", bestPeer.String()[:12], err)
			if !isNetworkError(err) {
				s.netManager.punishPeer(bestPeer, fmt.Sprintf("Lỗi tải Manifest (Không phải timeout): %v", err))
			}
			triedPeers[bestPeer] = true // Ghi nhớ lỗi để không hỏi lại peer này trong phiên sync hiện tại
			continue
		}
		if len(manifestData) < 36 {
			log.Printf("[FAST-SYNC-V2] ❌ Dữ liệu Manifest từ %s không hợp lệ (Độ dài: %d). Trừng phạt peer.", bestPeer.String()[:12], len(manifestData))
			s.netManager.punishPeer(bestPeer, fmt.Sprintf("Kích thước Manifest quá ngắn (%d bytes)", len(manifestData)))
			triedPeers[bestPeer] = true
			continue
		}

		numChunks := binary.LittleEndian.Uint32(manifestData[32:36])
		// [BẢO MẬT] Lớp 1: Xác thực độ lớn và giới hạn cứng (Anti-DoS & Anti-Panic)
		// Đặt giới hạn cứng 25,000 chunks (tương đương 50GB) và kiểm tra độ dài manifest khớp chính xác
		if numChunks > 25000 {
			log.Printf("[FAST-SYNC-V2] 🚨 Số lượng chunk từ %s vượt giới hạn an toàn (%d > 25000). Trừng phạt peer.", bestPeer.String()[:12], numChunks)
			s.netManager.punishPeer(bestPeer, "Manifest numChunks limit exceeded")
			triedPeers[bestPeer] = true
			continue
		}
		if len(manifestData) != 36+int(numChunks)*32 {
			log.Printf("[FAST-SYNC-V2] 🚨 Kích thước manifest từ %s không khớp với số chunk khai báo (%d != %d). Trừng phạt peer.", bestPeer.String()[:12], len(manifestData), 36+numChunks*32)
			s.netManager.punishPeer(bestPeer, "Manifest size mismatch")
			triedPeers[bestPeer] = true
			continue
		}

		manifestRoot := manifestData[0:32]

		// [MANIFEST-EMPTY-CHECK] Kiểm tra nếu root rỗng hoặc numChunks = 0 (tức là peer báo không có snapshot)
		// Tại sao: Đây là phản hồi hợp pháp của peer chưa có snapshot. Ta chỉ cần bỏ qua tĩnh lặng và thử peer khác,
		// tránh việc trừng phạt nhầm peer tốt gây phân rã mạng P2P.
		isAllZeroRoot := true
		for _, b := range manifestRoot {
			if b != 0 {
				isAllZeroRoot = false
				break
			}
		}
		if isAllZeroRoot || numChunks == 0 {
			log.Printf("[FAST-SYNC-V2] ℹ️ Peer %s báo không nắm giữ snapshot/manifest tại #%d. Thử lùi mỏ neo của peer này...", bestPeer.String()[:12], bestAnchor)
			maxAttemptedAnchor[bestPeer] = bestAnchor // Lưu lại mốc đã thử thất bại để lùi tiếp
			// Không gán triedPeers[bestPeer] = true vì ta muốn thử tiếp peer này ở mốc thấp hơn
			continue
		}

		// Đối chiếu StateRoot trong Manifest với StateRoot của Block Header Vanguard
		if !bytes.Equal(manifestRoot, anchorHeader.StateRoot.Value) {
			// Tại sao: Sai lệch StateRoot của Manifest biểu thị sự không đồng nhất dữ liệu hoặc nỗ lực lừa đảo snapshot giả.
			audit.AuditLog("SNAPSHOT_MANIFEST_MISMATCH", s.shortID(bestPeer), fmt.Sprintf("Manifest StateRoot không khớp với Block Header Vanguard. Manifest: %x, Header: %x", manifestRoot, anchorHeader.StateRoot.Value))
			s.netManager.punishPeer(bestPeer, "Manifest StateRoot Mismatch")
			continue
		}

		log.Printf("[FAST-SYNC-V2] 📜 Nhận diện Manifest hợp lệ. Số mảnh cần tải: %d", numChunks)

		// Xác định đường dẫn file ghi tạm và file chính thức
		tmpFile := filepath.Join(s.netManager.DbPath, "snapshots", fmt.Sprintf("snapshot_%d.bin.tmp", bestAnchor))
		snapFile := filepath.Join(s.netManager.DbPath, "snapshots", fmt.Sprintf("snapshot_%d.bin", bestAnchor))
		os.MkdirAll(filepath.Dir(tmpFile), 0755)

		// Resume logic: Đo kích thước file tạm thời hiện tại
		var cleanSize int64 = 0
		if fileInfo, err := os.Stat(tmpFile); err == nil {
			size := fileInfo.Size()
			chunkSize := int64(2 * 1024 * 1024) // 2MB
			cleanSize = (size / chunkSize) * chunkSize
			// Truncate file tạm về mốc byte sạch gần nhất nếu file bị lửng lơ ở giữa chunk
			if size != cleanSize {
				if err := os.Truncate(tmpFile, cleanSize); err != nil {
					log.Printf("[FAST-SYNC-V2] ⚠️ Truncate file tạm thất bại: %v. Tải lại từ đầu.", err)
					cleanSize = 0
				}
			}
		}

		// Mở file tạm để tiếp tục ghi hoặc tạo mới hoàn toàn
		var f *os.File
		if cleanSize > 0 {
			f, err = os.OpenFile(tmpFile, os.O_WRONLY|os.O_APPEND, 0644)
			log.Printf("[FAST-SYNC-V2] 🔄 Tải tiếp tục từ offset: %d bytes (Chunk %d/%d)", cleanSize, cleanSize/(2*1024*1024), numChunks)
		} else {
			f, err = os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		}
		if err != nil {
			log.Printf("[FAST-SYNC-V2] ❌ Không thể mở tệp ghi tạm thời: %v", err)
			continue
		}

		startChunkIdx := uint32(cleanSize / (2 * 1024 * 1024))

		s.mu.Lock()
		s.snapshotChunksTotal = numChunks
		s.snapshotChunksLoaded = startChunkIdx
		s.state = Bootstrapping
		s.mu.Unlock()

		syncSuccess := true

		for i := startChunkIdx; i < numChunks; i++ {
			offset := uint64(i) * 2 * 1024 * 1024
			expectedHash := manifestData[36 + i*32 : 36 + (i+1)*32]

			var chunkData []byte
			var downloadErr error

			// Thử tải từ bestPeer, nếu lỗi thử các peer khác trong danh sách khả dụng
			peersToTry := append([]peer.ID{bestPeer}, s.netManager.FindAvailablePeers()...)
			chunkDownloaded := false

			for _, p := range peersToTry {
				if p == s.netManager.Host.ID() {
					continue
				}
				chunkData, downloadErr = s.netManager.DownloadSnapshotFileChunk(p, bestAnchor, offset, 2*1024*1024)
				if downloadErr != nil {
					log.Printf("[FAST-SYNC-V2] ⚠️ Lỗi tải mảnh %d từ %s: %v", i, p.String()[:12], downloadErr)
					continue
				}

				// Xác thực tức thời băm Blake3 của mảnh vừa tải
				h := blake3.Sum256(chunkData)
				if !bytes.Equal(h[:], expectedHash) {
					// Tại sao: Snapshot chunk bị hỏng hoặc bị sửa đổi, cần ghi nhận để phân tích hành vi tấn công truyền tải rác.
					audit.AuditLog("SNAPSHOT_CHUNK_CORRUPT", s.shortID(p), fmt.Sprintf("Mảnh %d bị sai lệch băm BLAKE3. Nhận: %x, Mong đợi: %x", i, h[:], expectedHash))
					// [VÁ LỖI MẠNG] Ghi nhận 1 Strike (Cảnh cáo) cho hành vi gửi dữ liệu hỏng
					s.netManager.punishPeer(p, "Gửi chunk Snapshot giả mạo / chứa rác")
					s.netManager.Host.Network().ClosePeer(p)
					continue
				}

				chunkDownloaded = true
				break
			}

			if !chunkDownloaded {
				log.Printf("[FAST-SYNC-V2] ❌ Không thể tải mảnh %d từ bất kỳ peer nào.", i)
				syncSuccess = false
				f.Close()
				break
			}

			// Ghi mảnh sạch vào file tạm
			if _, err := f.Write(chunkData); err != nil {
				log.Printf("[FAST-SYNC-V2] ❌ Lỗi ghi file tạm: %v", err)
				syncSuccess = false
				f.Close()
				break
			}

			s.mu.Lock()
			s.snapshotChunksLoaded = i + 1
			s.mu.Unlock()
		}

		if !syncSuccess {
			// [VÁ LỖI LOGIC] Peer này đã cung cấp file không hoàn thiện hoặc chứa rác.
			// Loại nó khỏi danh sách hỏi xin Manifest/Chunk trong các lần Retry tiếp theo.
			log.Printf("[FAST-SYNC-V2] ⚠️ Peer %s cung cấp dữ liệu lỗi. Loại khỏi danh sách ưu tiên.", bestPeer.String()[:12])
			triedPeers[bestPeer] = true
			continue
		}
		f.Close()

		// Đổi tên file tạm thành file chính thức
		if err := os.Rename(tmpFile, snapFile); err != nil {
			log.Printf("[FAST-SYNC-V2] ❌ Lỗi đổi tên file snapshot: %v", err)
			continue
		}

		log.Printf("[FAST-SYNC-V2] 🏁 Tải Sổ cái hoàn tất. Đang chuyển giao file để Rust Core nạp RocksDB...")

		// Gọi Rust Core nạp snapshot
		importRes := s.netManager.Bridge.ImportStateSnapshotPath(snapFile, bestAnchor)
		if len(importRes) == 0 {
			log.Printf("[FAST-SYNC-V2] ❌ Rust Core từ chối nạp Snapshot hoặc file bị hỏng.")
			continue
		}

		// Sau khi nạp thành công, kiểm tra chéo StateRoot thực tế
		stateRoot := s.netManager.Bridge.GetStateRoot()

		if !bytes.Equal(stateRoot, anchorHeader.StateRoot.Value) {
			log.Printf("[FAST-SYNC-V2] ❌ LỖI VẬT LÝ: StateRoot sau khi nạp không khớp! Dự kiến: %x, Thực tế: %x",
				anchorHeader.StateRoot.Value, stateRoot)
			
			// [FAIL-SAFE] Tẩy rửa trạng thái bị nhiễm độc
			log.Printf("[FAIL-SAFE] ☢️ Node bị nhiễm độc trạng thái! Đang kích hoạt Tẩy Rửa Sổ Cái (Reset State)...")
			s.netManager.Bridge.ResetStateCompletely()

			// [PROACTIVE-CLEANUP] Xóa file snapshot lỗi khỏi đĩa để chống tốn tài nguyên và nhiễm độc lặp lại
			os.Remove(snapFile)
			os.Remove(tmpFile)
			
			s.netManager.punishPeer(bestPeer, "Gửi Snapshot có StateRoot sai lệch sau khi nạp")
			s.netManager.Host.Network().ClosePeer(bestPeer)
			continue
		}

		// [VANGUARD-FIX] Trả lại quyền quyết định CurrentHeight cho Rust Core
		actualRustHeight := s.netManager.Bridge.GetCurrentVersion()

		s.mu.Lock()
		s.currentHeight = actualRustHeight // Đồng bộ theo Rust
		s.finalizedHeight = actualRustHeight

		// [VANGUARD-CRITICAL-FIX] CƯỠNG CHẾ RUST CORE CẬP NHẬT MỎ NEO
		// [GAP-FILL-V2] Đảm bảo cập nhật mỏ neo khớp với chiều cao thực tế đã tải
		actualRustHeight = s.netManager.Bridge.GetCurrentVersion()
		s.netManager.Bridge.ForceSetFinalizedHeight(actualRustHeight)

		log.Printf("[FAST-SYNC] ✅ Hoàn tất nhảy cóc tới #%d. Đang chờ syncLoop nạp bù %d khối còn thiếu để tới đỉnh...",
			actualRustHeight, tipHeight-actualRustHeight)
		s.mu.Unlock()

		log.Printf("[FAST-SYNC] 🎉 Hoàn tất Bootstrap chế độ %s tại #%d (Rust Core xác nhận: #%d)!", syncMode, bestAnchor, actualRustHeight)

		// Tải các khối còn lại
		s.StartSync(tipHeight)
		return
	}
}


// [VANGUARD-CATCHUP] Kích hoạt cơ chế "Mặt dày" (Debounce) để đồng bộ lùi
// [V2.1 FIX] IsSynced kiểm tra cả Grace Period trước khi cho phép đào.
// Luồng logic:
//  1. Trong Grace Period (15s đầu) → LUÔN false → chặn đào
//  2. Sau Grace Period, nếu có peer mới → cập nhật targetHeight → phải sync trước
//  3. Sau Grace Period, không có peer → cho phép đào (solo mining)
func (s *SyncEngine) GetLastSyncActivity() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastSyncActivity
}

func (s *SyncEngine) GetSyncFailures() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.syncFailures
}

func (s *SyncEngine) IsSynced() bool {
	// [ZERO-DDoS-ABSOLUTE] Loại bỏ hoàn toàn 100% tất cả các chốt chặn tạm dừng đào.
	// IsSynced() luôn trả về true để đảm bảo thợ đào tuyệt đối không bao giờ bị dừng hoặc gián đoạn
	// bởi bất kỳ trạng thái P2P, đồng bộ hay Snapshot nào từ phía mạng lưới.
	return true
}

func (s *SyncEngine) UpdateHeight(height uint64) {
	// [RUST-BRAIN] Không tự tiện cập nhật chiều cao. Luôn hỏi lại Rust.
	actualH := s.netManager.Bridge.GetCurrentVersion()

	s.mu.Lock()
	s.currentHeight = actualH

	// [VANGUARD-FINALITY] Việc tính toán Finality ĐÃ ĐƯỢC RUST LÀM KHI COMMIT BLOCK (H-5).
	// Go tuyệt đối không được phép tự ý ép Rust ghi đè mỏ neo để tránh mất vùng linh hoạt Reorg.
	s.finalizedHeight = s.netManager.Bridge.GetFinalizedHeight()

	if s.currentHeight >= s.targetHeight && s.state != Synced {
		s.state = Synced
		log.Printf("[SYNC] 🎉 Rust Core báo cáo đã đạt đỉnh mạng lưới tại #%d.", actualH)
		if s.netManager != nil {
			s.netManager.TargetPathMu.Lock()
			if len(s.netManager.TargetPathHashes) > 0 {
				log.Printf("[TARGET-HASH-SYNC] ✅ Đã đồng bộ hoàn tất tới Target Hash. Giải phóng bộ lọc nhánh.")
				s.netManager.TargetPathHashes = make(map[string]bool)
			}
			s.netManager.TargetPathMu.Unlock()
		}
	}
	s.mu.Unlock()
}


func (s *SyncEngine) GetFinalizedHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.finalizedHeight
}

func (s *SyncEngine) GetCurrentHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentHeight
}

func (s *SyncEngine) GetSyncProgress() (uint64, uint64, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stateStr := "Stalled"
	switch s.state {
	case Syncing:
		stateStr = "Syncing"
	case Synced:
		stateStr = "Synced"
	case Bootstrapping:
		stateStr = "Bootstrapping"
	}
	return s.currentHeight, s.targetHeight, stateStr
}

// [SNAP-SYNC-PROGRESS] GetSnapshotProgress trả về số lượng chunk đã tải và tổng số chunk của snapshot
func (s *SyncEngine) GetSnapshotProgress() (uint32, uint32) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotChunksLoaded, s.snapshotChunksTotal
}

// GetDownloadingHeight: Trả về chiều cao khối đang tải ở Phase 1
func (s *SyncEngine) GetDownloadingHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.downloadingHeight
}

func (s *SyncEngine) HandleBlockArrival(block *pb_block.Block, from peer.ID) {
	if block == nil || block.Header == nil {
		return
	}

	// Cập nhật chiều cao Peer realtime khi nhận Gossip Block để không bị mù đỉnh mạng lưới
	s.netManager.UpdatePeerHeight(from, block.Header.Height, s.netManager.Bridge.GetFinalizedHeight(), 0)

	// =====================================================================
	// [VANGUARD-FIX] CHẾ ĐỘ KHÔNG LÀM PHIỀN (DO NOT DISTURB) BẢO VỆ SNAPSHOT
	// Tuyệt đối CẤM xử lý khối mồ côi hay kích hoạt cân chỉnh chuỗi mồ côi/CatchUpSync 
	// khi hệ thống đang trong quá trình nhảy cóc (FastSyncBootstrap).
	// =====================================================================
	s.mu.RLock()
	isBootstrapping := s.bootstrapRunning
	s.mu.RUnlock()

	if isBootstrapping {
		log.Printf("[SYNC-GUARD] 🛡️ Bỏ qua khối Gossip #%d từ %s vì hệ thống đang bận nạp Snapshot...", block.Header.Height, s.shortID(from))
		
		// Chỉ lẳng lặng cập nhật targetHeight để sau khi Snapshot xong, syncLoop biết đường chạy tiếp.
		s.mu.Lock()
		if block.Header.Height > s.targetHeight {
			s.targetHeight = block.Header.Height
		}
		s.mu.Unlock()
		
		return // THOÁT NGAY LẬP TỨC!
	}

	// Nhận diện khối rỗng thuộc vùng đã bị Pruned
	// Tại sao: Khi khối rỗng (không phải Genesis), tức là peer đã prune và chỉ gửi được header.
	// Chúng ta buộc phải dừng đồng bộ tuần tự để tránh kẹt hoặc lệch State Root, chuyển sang Snap Sync.
	hasBody := block.Body != nil && len(block.Body.Transactions) > 0
	if block.Header.Height > 0 && !hasBody {
		log.Printf("[SYNC-GUARD] 🧹 Phát hiện khối rỗng #%d (Đã bị mạng chính Pruned).", block.Header.Height)
		s.mu.Lock()
		if s.state != Bootstrapping {
			s.state = Bootstrapping
			log.Printf("[SYNC-CRITICAL] 🚀 BẮT BUỘC KÍCH HOẠT SNAPSHOT SYNC! Chuỗi lịch sử đã bị Pruned tại #%d", block.Header.Height)
			go s.FastSyncBootstrap()
		}
		s.mu.Unlock()
		return
	}

	headerBuf, _ := proto.Marshal(block.Header)
	isValid, err := s.netManager.Bridge.VerifyPow(
		headerBuf,
		block.Header.Nonce,
		block.Header.Difficulty,
		block.Header.Height,
	)

	if err != nil {
		if strings.Contains(err.Error(), "DB_BUSY") {
			log.Printf("[SYNC-WARN] ⏳ DB cục bộ đang bận khi xác thực PoW Gossip #%d. Thử lại sau.", block.Header.Height)
			return
		}
		log.Printf("[SYSTEM-WARN] ⚠️ Lỗi hệ thống khi xác thực PoW Gossip #%d: %v. Thử lại sau.", block.Header.Height, err)
		return
	}

	if !isValid {
		log.Printf("[SECURITY-ALERT] %s", i18n.T("log_security_alert_pow", s.shortID(from), block.Header.Height))
		s.netManager.punishPeer(from, "Invalid PoW on Gossip Header")
		return
	}

	s.mu.Lock()
	if block.Header.Height > s.targetHeight {
		s.targetHeight = block.Header.Height
		s.state = Syncing
		log.Printf("[SYNC-CHASER] 📡 Nhận khối #%d qua Gossip. Rust Core sẽ quyết định số phận khối này.", block.Header.Height)
	}
	s.mu.Unlock()

	localHeight := s.netManager.Bridge.GetCurrentVersion()

	// 2. Phân loại khối mồ côi (Height > localHeight + 1)
	if block.Header.Height > localHeight+1 {
		if block.Header.Height > localHeight+5 {
			// Mồ côi xa: Tải chùm
			log.Printf("[P2P-ORPHAN] 🚀 Mồ côi xa (#%d > %d + 5). Kích hoạt CatchUpSync...", block.Header.Height, localHeight)
			go s.CatchUpSync(from)
		} else {
			// Mồ côi gần: Chỉ lưu trữ Header siêu nhẹ (~150 bytes) và TxIDs vào RAM phục vụ tái tạo khối, sau đó kích hoạt đồng bộ chùm
			log.Printf("[P2P-ORPHAN] 🕵️ Mồ côi gần (#%d - %d <= 5). Lưu Header/TxIDs vào RAM và kích hoạt đồng bộ chùm từ mốc bất biến...", block.Header.Height, localHeight)

			blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBuf)
			hashStr := hex.EncodeToString(blockHash)

			s.orphanHeadersMu.Lock()
			// Dọn dẹp các khối mồ côi quá hạn trong RAM cache (Height <= localHeight)
			for hStr, hdr := range s.orphanHeaders {
				if hdr.Height <= localHeight {
					delete(s.orphanHeaders, hStr)
					delete(s.orphanTxIDs, hStr)
					delete(s.orphanCoinbase, hStr)
				}
			}
			s.orphanHeaders[hashStr] = block.Header
			// [RECONSTRUCTION-CACHE] Trích xuất và cache TxIDs + Coinbase transaction thô
			if block.Body != nil && len(block.Body.Transactions) > 0 {
				var rawTxs [][]byte
				for _, tx := range block.Body.Transactions {
					txBytes, _ := proto.Marshal(tx)
					rawTxs = append(rawTxs, txBytes)
				}

				hashes, err := s.netManager.Bridge.CalculateTxHashesBatch(rawTxs, block.Header.Height)
				if err != nil || len(hashes) != len(rawTxs) {
					log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed for HandleBlockArrival: %v. Fallback to native.", err)
					hashes = make([][]byte, len(rawTxs))
					for idx, d := range rawTxs {
						hashes[idx] = GetTxIDNative(d)
					}
				}
				s.orphanTxIDs[hashStr] = hashes

				// Cache Coinbase transaction bytes
				coinbaseBytes, _ := proto.Marshal(block.Body.Transactions[0])
				s.orphanCoinbase[hashStr] = coinbaseBytes
				log.Printf("[P2P-ORPHAN] 🧠 Đã cache %d TxIDs và Coinbase cho khối mồ côi gần #%d (%s) trong HandleBlockArrival để tái tạo sau.", len(hashes), block.Header.Height, hashStr[:12])
			}
			s.orphanHeadersMu.Unlock()

			missingHashBytes := block.Header.ParentHash.Value

			// Đăng ký thám tử điều tra mồ côi
			missingParentHashStr := hex.EncodeToString(missingHashBytes)[:12]
			s.orphanMu.Lock()
			s.orphanTracker[missingParentHashStr] = &OrphanInvestigation{
				MissingHash: missingHashBytes,
				Sender:      from,
				LastActive:  time.Now(),
				Height:      block.Header.Height - 1,
			}
			s.orphanMu.Unlock()

			if s.checkAndRecordOrphanAttempt(from, missingHashBytes) {
				go func() {
					if err := s.alignOrphanChain(from, missingHashBytes, headerBuf); err != nil {
						log.Printf("[SYNC-ERROR] ❌ Lỗi kích hoạt đồng bộ chùm từ Gossip: %v", err)
					}
				}()
			} else {
				log.Printf("[SYNC-ORPHAN] ⚠️ Bỏ qua đồng bộ chùm trùng lặp cho cha %x từ Peer %s trong thời gian cooldown.", missingHashBytes[:6], s.shortID(from))
			}
		}
		return // Thoát ngay, tuyệt đối không lưu full block vào pendingBlocks (RAM)
	}

	// 3. Chỉ lưu khối đầy đủ vào RAM nếu nó là khối tiếp nối trực tiếp chuỗi chính (Height == localHeight + 1)
	if block.Header.Height == localHeight+1 {
		data, _ := proto.Marshal(block)
		s.fetchMu.Lock()
		s.pendingBlocks[block.Header.Height] = [][]byte{data}
		s.fetchMu.Unlock()
	}
}

func (s *SyncEngine) shortID(id peer.ID) string {
	str := id.String()
	if len(str) > 12 {
		return str[:12]
	}
	return str
}
func (s *SyncEngine) investigationRoutine() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Struct tạm chỉ chứa dữ liệu giá trị (Value semantics) để chống Data Race.
	// Tại sao không dùng con trỏ *OrphanInvestigation: Vì syncLoop có thể ghi đè inv.LastActive
	// trong khi ta đang đọc nó ngoài Lock → Go Race Detector sẽ báo lỗi và crash Node.
	type snapshotInvestigation struct {
		hStr        string
		missingHash []byte  // An toàn vì nội dung băm là bất biến (immutable content)
		sender      peer.ID // Value type (string) - an toàn khi copy
		lastActive  time.Time
		isBodySync  bool
		height      uint64
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Dọn dẹp knownSidechains định kỳ (mỗi 5 giây) để tránh phình RAM (OOM Attack)
			// Tại sao: sidechain rác có thể được gửi liên tục bởi kẻ tấn công. Chúng ta tự động xóa
			// các sidechain được ghi nhận từ 1 giờ trước để bảo vệ bộ nhớ.
			s.mu.Lock()
			nowTime := time.Now()
			for hashStr, ts := range s.knownSidechains {
				if nowTime.Sub(ts) > 1*time.Hour {
					delete(s.knownSidechains, hashStr)
				}
			}
			s.mu.Unlock()

			// ==========================================
			// PHA 1: DEEP COPY TRONG LOCK (Siêu tốc - Nano-giây)
			// Tại sao: Giảm Mutex Contention xuống 0. syncLoop không bao giờ bị nghẽn
			// khi cần thêm khối mồ côi mới vào tracker.
			// ==========================================
			s.orphanMu.Lock()
			if len(s.orphanTracker) == 0 {
				s.orphanMu.Unlock()
				continue
			}

			targets := make([]snapshotInvestigation, 0, len(s.orphanTracker))
			for hStr, inv := range s.orphanTracker {
				targets = append(targets, snapshotInvestigation{
					hStr:        hStr,
					missingHash: inv.MissingHash,
					sender:      inv.Sender,
					lastActive:  inv.LastActive,
					isBodySync:  inv.IsBodySync,
					height:      inv.Height,
				})
			}
			s.orphanMu.Unlock() // MỞ KHÓA NGAY LẬP TỨC!

			// ==========================================
			// PHA 2: I/O NGOÀI LOCK (An toàn không gây treo)
			// Tại sao: gRPC (GetHeaderRaw) và mạng (ClosePeer) có thể treo vài giây
			// nếu Rust Core bận hoặc mạng chập chờn. Nếu giữ Lock trong lúc này,
			// syncLoop sẽ bị Deadlock dây chuyền.
			// ==========================================
			now := time.Now()
			var resolvedCases []string
			var timeoutCases []snapshotInvestigation

			for _, target := range targets {
				// Gọi gRPC thoải mái xuống Rust Core (không giữ Lock)
				if hdr := s.netManager.Bridge.GetHeaderRaw(target.missingHash); hdr != nil {
					log.Printf("[INVESTIGATION] ✅ Vụ án %s đã khép lại. Khối cha đã xuất hiện.", target.hStr)
					resolvedCases = append(resolvedCases, target.hStr)
					continue
				}

				// Kiểm tra quá hạn
				timeout := RoutineTimeout
				if target.isBodySync {
					timeout = BodySyncTimeout
				}

				if now.Sub(target.lastActive) > timeout {
					log.Printf("[SYNC-TIMEOUT] ⏳ Peer %s quá hạn %v không phản hồi khối cha %s cho khối #%d. Cắt đứt TCP.",
						target.sender.String()[:12], timeout, target.hStr, target.height+1)

					// Gọi I/O mạng (ClosePeer) ngoài Lock - an toàn tuyệt đối
					s.netManager.Host.Network().ClosePeer(target.sender)
					timeoutCases = append(timeoutCases, target)
				}
			}

			// ==========================================
			// PHA 3: XÓA CÓ ĐIỀU KIỆN - COMPARE AND SWAP (Bảo vệ dữ liệu mới)
			// Tại sao: Trong lúc Pha 2 chạy (vài giây), syncLoop có thể đã gán lại
			// vụ án cho một Peer MỚI. Nếu xóa mù quáng, ta sẽ xóa mất phiên điều
			// tra mới → khối mồ côi bị bỏ rơi vĩnh viễn (Stale Deletion).
			// ==========================================
			if len(resolvedCases) > 0 || len(timeoutCases) > 0 {
				s.orphanMu.Lock()

				// 1. Khối đã có trong Rust → Xóa vô điều kiện (đã giải quyết xong)
				for _, hStr := range resolvedCases {
					delete(s.orphanTracker, hStr)
				}

				// 2. Peer quá hạn → Chỉ xóa NẾU Sender trong Map vẫn khớp với Sender cũ (CAS)
				// Nếu syncLoop đã gán Peer mới cho vụ án này → BỎ QUA lệnh xóa để bảo vệ phiên mới
				for _, t := range timeoutCases {
					if currentInv, exists := s.orphanTracker[t.hStr]; exists && currentInv.Sender == t.sender {
						delete(s.orphanTracker, t.hStr)
					}
				}

				s.orphanMu.Unlock()
			}
		}
	}
}

// [MONERO-LOCATOR] CatchUpSync: Tìm điểm rẽ nhánh nhanh nhất và tải bù khối (Headers-First Sync V2)
// [ANTI-RACE] Sử dụng atomic flag để đảm bảo chỉ có 1 goroutine CatchUpSync chạy tại bất kỳ thời điểm nào.
// Tại sao: Khi khối đào nhanh (1s/khối), nhiều sự kiện Gossip/Orphan kích hoạt đồng thời nhiều goroutine CatchUpSync.
// Nhiều CatchUpSync song song → gửi nhiều batch Reorg chồng chéo xuống Rust Core → State Root bị tính sai → Node kẹt vòng lặp rollback vô tận.
func (s *SyncEngine) CatchUpSync(targetPeer peer.ID) {
	// =====================================================================
	// [VANGUARD-FIX] BẢO VỆ FAST SYNC
	// =====================================================================
	s.mu.RLock()
	if s.bootstrapRunning {
		s.mu.RUnlock()
		log.Printf("[SYNC-GUARD] 🛡️ Hủy lệnh CatchUpSync vì hệ thống đang bận nạp Snapshot.")
		return
	}
	s.mu.RUnlock()

	// [GENESIS-PROTECT] Hủy lệnh CatchUpSync ngay lập tức nếu node địa phương chưa sở hữu Khối Genesis (#0).
	// Tại sao: Nếu chưa có khối 0, lõi Rust sẽ không có tiêu đề cha để so khớp điểm rẽ nhánh, khiến CatchUpSync chắc chắn thất bại và tạo phản ứng phụ là spam mạng liên tục. Ta phải đợi syncLoop tải xong khối 0 tuần tự trước.
	// [FIXED] CHỈ áp dụng chốt chặn bảo vệ nếu Node thực sự đang ở Genesis (currentHeight == 0) và chưa có Block Hash #0.
	s.mu.RLock()
	currentH := s.currentHeight
	s.mu.RUnlock()
	if currentH == 0 && len(s.netManager.Bridge.GetBlockHash(0)) == 0 {
		log.Printf("[SYNC-GUARD] 🛡️ Hủy lệnh CatchUpSync vì node mới chưa có Khối Genesis #0.")
		return
	}

	// [ANTI-RACE] Chỉ 1 CatchUpSync được phép chạy tại 1 thời điểm
	// CompareAndSwap: Nếu flag đang = 0 (không ai chạy) → đặt thành 1 và tiếp tục.
	// Nếu flag đang = 1 (có goroutine khác đang chạy) → bỏ qua ngay lập tức.
	if !atomic.CompareAndSwapInt32(&s.catchUpRunning, 0, 1) {
		log.Printf("[ANTI-RACE] ⏸️ CatchUpSync đã có goroutine đang chạy. Bỏ qua yêu cầu trùng lặp từ Peer %s.", s.shortID(targetPeer))
		return
	}
	defer atomic.StoreInt32(&s.catchUpRunning, 0) // Giải phóng flag khi hoàn tất hoặc lỗi

	// [SELF-HEAL-HEIGHT] Tự chữa lành chiều cao & Trạng thái Go Sync Engine khi CatchUpSync kết thúc
	// Tại sao: Khi CatchUpSync thoát (dù thành công, thất bại, bị từ chối do fork < 10x, hoặc bị timeout giữa chừng), 
	// việc khôi phục currentHeight & targetHeight thực tế và trả lại trạng thái Synced (khi initialSyncDone == true) 
	// sẽ giải phóng đứt gãy miner getwork, giúp miner tiếp tục khai thác mượt mà không bị kẹt ở trạng thái Syncing ảo.
	defer func() {
		actualH := s.netManager.Bridge.GetCurrentVersion()
		s.mu.Lock()
		s.currentHeight = actualH
		s.downloadingHeight = 0
		if s.initialSyncDone {
			maxPeerH := actualH
			if s.netManager != nil && s.netManager.Host != nil {
				peers := s.netManager.Host.Network().Peers()
				s.netManager.PeerMutex.RLock()
				for _, p := range peers {
					if cooldown, ok := s.sidechainPeers[p]; ok && time.Now().Before(cooldown) {
						continue
					}
					if h := s.netManager.PeerHeights[p]; h > maxPeerH {
						maxPeerH = h
					}
				}
				s.netManager.PeerMutex.RUnlock()
			}
			s.targetHeight = maxPeerH
			if s.currentHeight >= s.targetHeight {
				s.state = Synced
			}
		}
		s.mu.Unlock()
	}()

	s.mu.Lock()
	s.syncFailures = 0
	s.mu.Unlock()

	// 1. Lấy các mốc an toàn từ sổ cái Rust Core
	fH := s.netManager.Bridge.GetFinalizedHeight()
	actualH := s.netManager.Bridge.GetCurrentVersion()

	// [VANGUARD-SIMPLIFIED] Luôn bắt đầu từ finalizedHeight + 1 (mốc bất biến).
	// Tại sao: Finalized Height (fH) là mốc đã được chốt và bất biến, không thể bị reorg. Bất kỳ nhánh rẽ (fork) hợp lệ nào
	// cũng bắt buộc phải bắt đầu từ fH + 1 trở đi. Quăng lưới từ fH + 1 với tối đa 10000 headers chắc chắn sẽ tìm được điểm rẽ nhánh
	// mà không cần lùi 50 khối phức tạp hay lo ngại bị kẹt treadmill.
	startH := fH + 1

	s.netManager.PeerMutex.RLock()
	peerH := s.netManager.PeerHeights[targetPeer]
	oldestH := s.netManager.PeerOldestHeights[targetPeer] // Lấy mốc Purge của Peer
	s.netManager.PeerMutex.RUnlock()

	stepBackAttempts := 0
	var evalResp *pb_block.EvaluateHeaderChainResponse
	var hBatch [][]byte

	for {
		if peerH < startH {
			log.Printf("[SYNC-GUARD] 🛡️ Hủy CatchUpSync vì Peer Height (%d) thấp hơn mốc bắt đầu sync (%d).", peerH, startH)
			return
		}

		// [DEADLOOP-FIX] Chống xin mồ côi mù quáng vào vùng đã bị Pruned
		// V2: Kích hoạt FastSyncBootstrap trực tiếp thay vì return im lặng.
		if oldestH > startH {
			log.Printf("[SYNC-LOCATOR] ⚠️ Peer %s đã Pruned dữ liệu tại #%d (Oldest: %d). Hủy CatchUpSync.", s.shortID(targetPeer), startH, oldestH)
			log.Printf("[SYNC-LOCATOR] 🚀 Kích hoạt FastSyncBootstrap trực tiếp để nhảy cóc qua vùng Pruned!")
			go s.FastSyncBootstrap()
			return
		}

		// [VÁ LỖI TREADMILL] Tăng sải bước tải Header lên 10000 khối thay vì kẹt ở 100
		count := uint32(10000)
		if peerH >= startH {
			needed := uint32(peerH - startH + 1)
			if needed < count {
				count = needed
			}
		}

		log.Printf("[SYNC-LOCATOR] 📡 Xin chùm %d Header từ mốc #%d để phân xử... (Vòng lặp lùi: %d)", count, startH, stepBackAttempts)
		// [MAINNET-TIMEOUT] Tăng timeout từ 15s lên 30s để tránh việc treo cờ catchUpRunning hoặc hủy đồng bộ
		// giữa chừng do đường truyền mạng P2P bị nghẽn hoặc Peer phản hồi chậm khi tải Header Batch.
		timeoutCtx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		var err error
		hBatch, err = s.netManager.DownloadHeaderBatch(timeoutCtx, targetPeer, startH, count)
		cancel()

		if err != nil || len(hBatch) == 0 {
			if len(hBatch) == 0 && err == nil {
				if peerH >= startH {
					log.Printf("[SYNC-LOCATOR] ⚠️ Peer %s cung cấp Header Batch rỗng mặc dù quảng bá chiều cao %d >= %d. Đánh 1 Strike.", s.shortID(targetPeer), peerH, startH)
					s.netManager.punishPeer(targetPeer, "Gửi Header Batch rỗng khi được yêu cầu (Peer quảng bá chiều cao đủ)")
				} else {
					log.Printf("[SYNC-LOCATOR] 🕊️ Peer %s trả về Header Batch rỗng vì chiều cao quảng bá %d < %d (không đủ dữ liệu). Bỏ qua không phạt.", s.shortID(targetPeer), peerH, startH)
				}
			} else if isNetworkError(err) {
				// [BAO DUNG VỚI LỖI MẠNG] Không phạt IP, chỉ ngắt kết nối để thử Peer khác
				log.Printf("[SYNC-TIMEOUT] ⏳ Peer %s mạng chậm/timeout/mất kết nối khi tải Header. Cắt TCP, không phạt IP.", s.shortID(targetPeer))
				s.netManager.Host.Network().ClosePeer(targetPeer)
			} else {
				// Lỗi rác/giải mã -> Ác ý -> Phạt
				log.Printf("[SYNC-LOCATOR] 🛑 Peer %s gửi dữ liệu rác. Đánh 1 Strike. Lỗi: %v", s.shortID(targetPeer), err)
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("Lỗi tải Header Batch (Dữ liệu rác/hỏng): %v", err))
			}
			s.mu.Lock()
			s.syncFailures++
			s.mu.Unlock()
			return
		}

		// 2. Nhờ Rust đánh giá chuỗi Header này
		var bridgeErr error
		evalResp, bridgeErr = s.netManager.Bridge.EvaluateHeaderChain(hBatch)
		if bridgeErr != nil || evalResp == nil {
			s.mu.Lock()
			s.syncFailures++
			s.mu.Unlock()
			return
		}

		if evalResp.Status != 0 {
			errMsg := evalResp.ErrorMsg
			// Tại sao: Nếu Rust Core trả về lỗi thiếu cha (không tìm thấy điểm rẽ nhánh), điều đó chứng tỏ điểm rẽ nhánh (LCA) nằm sâu hơn mốc startH hiện tại.
			// Lúc này, Go Node sẽ kích hoạt cơ chế lùi bước lũy tiến (Step-Back LCA Search) để tải các Header cũ hơn từ Peer, 
			// tiếp tục quăng lưới ngược dòng lịch sử cho đến khi tìm thấy khối cha chung đã được lưu trữ cục bộ.
			if evalResp.Status == 2 && (strings.Contains(errMsg, "Không tìm thấy điểm rẽ nhánh") || strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "Parent hash")) {
				stepBackAttempts++
				stepBack := uint64(100)
				if stepBackAttempts > 1 {
					stepBack = 500
				}
				if stepBackAttempts > 2 {
					stepBack = 2000
				}

				if startH > stepBack {
					startH -= stepBack
				} else {
					startH = 0
				}
				log.Printf("[SYNC-LOCATOR] 📉 Phát hiện rẽ nhánh sâu nhưng thiếu cha. Đang lùi mốc tìm kiếm về #%d để tìm lại điểm rẽ nhánh...", startH)
				continue
			}

			// Từ chối do lỗi khác
			log.Printf("[SYNC-REJECT] ⚠️ Rust từ chối chuỗi Header từ %s: Mã lỗi %d - %s", s.shortID(targetPeer), evalResp.Status, evalResp.ErrorMsg)
			s.mu.Lock()
			s.syncFailures++
			s.mu.Unlock()
			if evalResp.Status != 0 {
				s.netManager.punishPeer(targetPeer, "FIREWALL_VIOLATION: Malicious 51% deep reorg attempt: "+evalResp.ErrorMsg)
				s.netManager.Host.Network().ClosePeer(targetPeer)
			}
			return
		}

		// Status == 0: Hợp lệ, tiến hành thực thi tải khối
		break
	}

	if evalResp.Status == 0 { // Nhánh rẽ NẶNG HƠN (Hợp lệ)
		// Tìm height cao nhất trong hBatch để biết đích đến và lập bản đồ headers bytes
		var highestH uint64
		hdrMap := make(map[uint64][]byte)
		for _, hBytes := range hBatch {
			var hdr pb_block.BlockHeader
			if err := proto.Unmarshal(hBytes, &hdr); err == nil {
				hdrMap[hdr.Height] = hBytes
				if hdr.Height > highestH {
					highestH = hdr.Height
				}
			}
		}

		// [VÁ LỖI THOÁT NON CHÍ MẠNG]
		// Tại sao: Nếu đỉnh của chùm Header tải về (highestH) nhỏ hơn hoặc bằng ForkPoint do Rust Core đánh giá (nghĩa là toàn bộ chùm Header này đã khớp hoàn toàn với DB nội bộ), ta không cần tải thêm thân khối của chùm này nữa. Nhưng ta phải cập nhật lại chiều cao của Go (currentHeight) khớp với Rust Core để chuẩn bị cho chu kỳ tải tiếp theo, tránh bị kẹt trong vòng lặp vô hạn.
		if highestH <= evalResp.ForkPoint {
			// Chỉ thoát sớm nếu chiều cao sổ cái thực tế trước đó (actualH) đã vượt qua hoặc bằng đỉnh chùm Header
			if actualH >= highestH {
				log.Printf("[SYNC-CATCHUP] ℹ️ Chùm Header tải về và Body đã khớp hoàn toàn với DB (ForkPoint = %d). Mở đường tải đợt tiếp theo...", evalResp.ForkPoint)
				s.mu.Lock()
				s.currentHeight = s.netManager.Bridge.GetCurrentVersion() // Cập nhật lại chuẩn theo Rust để tránh lệch pha chiều cao
				s.mu.Unlock()
				return
			}
			log.Printf("[SYNC-CATCHUP] ⚠️ Chùm Header khớp với DB nhưng thiếu Body (Đỉnh Sổ cái: #%d < Đỉnh chùm: #%d). Tiếp tục tải Body...", actualH, highestH)
		}

		// ===================================================================
		// [VÁ LỖI LỆCH STATE ROOT] - TÌM ĐIỂM BẮT ĐẦU TẢI BODY THỰC TẾ
		// EvaluateHeaderChain trả về ForkPoint dựa trên HEADER đã lưu.
		// Nhưng nếu Node chưa chạy Body đến đó, ta BẮT BUỘC phải tải Body 
		// từ đỉnh Sổ cái (actualH), chứ không được nhảy cóc!
		// ===================================================================
		effectiveForkPoint := evalResp.ForkPoint
		// Sử dụng actualH ban đầu ở đầu hàm (trước khi EvaluateHeaderChain làm tăng version giả của Rust) để ép lùi
		if effectiveForkPoint > actualH {
			log.Printf("[SYNC-CORRECTION] ⚠️ ForkPoint Header (#%d) vượt quá Đỉnh Sổ cái (#%d). Ép lùi điểm bắt đầu tải Body về #%d để tránh lệch StateRoot!", effectiveForkPoint, actualH, actualH)
			effectiveForkPoint = actualH
		}

		// [SYNC-LOOP-CATCHUP] Lặp tải thân khối theo các mẻ 100 khối cho đến khi tải hết toàn bộ chùm 10,000 headers
		for effectiveForkPoint < highestH {
			// 1. Thử dùng tính năng tải khối thông minh (Smart P2P Sync) nếu Peer hỗ trợ
			smartSuccess := false
			protocols, pErr := s.netManager.Host.Peerstore().SupportsProtocols(targetPeer, SmartSyncProtocol)
			if pErr == nil && len(protocols) > 0 {
				log.Printf("[SMART-SYNC] 🧠 Phát hiện Peer %s hỗ trợ SmartSyncProtocol 1.1.0! Tiến hành tải thông minh...", s.shortID(targetPeer))
				// Tải tối đa toàn bộ chùm còn lại, giới hạn dung lượng 10MB (cho phép sai số 1MB, thiết lập MaxBytes = 10MB)
				maxSmartBytes := uint32(10 * 1024 * 1024)
				smartBlocks, err := s.netManager.RequestBlockBatchSmart(s.netManager.Ctx, targetPeer, effectiveForkPoint+1, highestH, maxSmartBytes)
				if err == nil && len(smartBlocks) > 0 {
					log.Printf("[SMART-SYNC] 📥 Đã tải thành công %d khối thông minh (từ #%d đến #%d). Đang thẩm định qua Rust Core...", len(smartBlocks), effectiveForkPoint+1, effectiveForkPoint+uint64(len(smartBlocks)))
					
					// Thẩm định qua ProcessChain theo từng mẻ nhỏ 10 khối
					chunkSize := 10
					var chainErr error
					var lastResp *pb_block.SyncChainResponse
					for i := 0; i < len(smartBlocks); i += chunkSize {
						end := i + chunkSize
						if end > len(smartBlocks) {
							end = len(smartBlocks)
						}
						chunk := smartBlocks[i:end]
						resp, err := s.netManager.Bridge.ProcessChain(chunk)
						if err != nil {
							chainErr = err
							break
						}
						if resp != nil && (resp.Status == 2 || resp.Status == 4) {
							lastResp = resp
							chainErr = fmt.Errorf("ProcessChain failed status %d: %s", resp.Status, resp.ErrorMsg)
							break
						}
						lastResp = resp
						if resp != nil && resp.Status == 1 {
							s.mu.Lock()
							s.currentHeight = resp.NewHeight
							s.mu.Unlock()
						}
					}
					
					if chainErr == nil && lastResp != nil && (lastResp.Status == 1 || lastResp.Status == 0) {
						if lastResp.Status == 1 {
							log.Printf("[REORG-SUCCESS] 🔄 Reorg thông minh thành công lên cao độ #%d!", lastResp.NewHeight)
							s.mu.Lock()
							s.currentHeight = lastResp.NewHeight
							s.mu.Unlock()
						}
						effectiveForkPoint = effectiveForkPoint + uint64(len(smartBlocks))
						smartSuccess = true
					} else {
						log.Printf("[SMART-SYNC] ⚠️ Thẩm định chuỗi thông minh thất bại: %v. Sẽ lùi về tải mẻ 100 cũ.", chainErr)
					}
				} else {
					log.Printf("[SMART-SYNC] ⚠️ Tải mẻ thông minh thất bại hoặc rỗng: %v. Sẽ lùi về tải mẻ 100 cũ.", err)
				}
			}

			if smartSuccess {
				continue
			}

			// [ANTI-OOM-PATCH] Giới hạn số lượng thân khối (Block Body) tải vào RAM tối đa 100 khối mỗi chu kỳ.
			// Lý do: Nếu tải toàn bộ 10,000 khối vào RAM cùng một lúc, với kích thước khối từ 5MB - 35MB,
			// sẽ tiêu tốn hàng chục GB RAM dẫn đến sập (OOM) Node. Giới hạn 100 khối giúp RAM nhẹ nhàng.
			limitH := highestH
		if limitH > effectiveForkPoint+100 {
			limitH = effectiveForkPoint + 100
			log.Printf("[ANTI-OOM-PATCH] 🛡️ Giới hạn tải thân khối tối đa 100 khối từ #%d đến #%d (Cắt giảm từ mốc gốc #%d để bảo vệ RAM).", effectiveForkPoint+1, limitH, highestH)
		}

		log.Printf("[SYNC-REORG] 🔄 Nhánh rẽ hợp lệ tại #%d. Bắt đầu tải thân khối từ #%d đến #%d...", effectiveForkPoint, effectiveForkPoint+1, limitH)

		// 3. [TÍNH NĂNG PHỤC HỒI] Gom Full Block đi thẳng tới trước để đưa cho Rust
		debtChain := make([][]byte, 0, int(limitH-effectiveForkPoint))

		type downloadResult struct {
			height uint64
			data   []byte
			err    error
		}

		batchSize := int(limitH - effectiveForkPoint)
		resChan := make(chan downloadResult, batchSize)
		sem := make(chan struct{}, 8) // Giới hạn tối đa 8 luồng tải song song để tối ưu hóa thời gian chờ phản hồi mạng

		for h := effectiveForkPoint + 1; h <= limitH; h++ {
			fH := s.netManager.Bridge.GetFinalizedHeight()
			oldestH := s.netManager.Bridge.GetOldestHeight()

			// Tạo Header-Only nếu nằm dưới mốc Snapshot/Purge
			if h < fH || h < oldestH {
				log.Printf("[SYNC-LIGHTWEIGHT-CATCHUP] Khối lịch sử #%d dưới mốc Snapshot (#%d) hoặc Đại Thanh Trừng (#%d). Tạo Header-Only block.", h, fH, oldestH)
				hBytes := hdrMap[h]
				var hdr pb_block.BlockHeader
				if err := proto.Unmarshal(hBytes, &hdr); err == nil {
					fullBlock := &pb_block.Block{
						Header: &hdr,
						Body:   nil,
					}
					blockRaw, _ := proto.Marshal(fullBlock)
					resChan <- downloadResult{height: h, data: blockRaw}
				}
				continue
			}

			// Kiểm tra RAM cache
			s.fetchMu.Lock()
			cachedBlocks, ok := s.pendingBlocks[h]
			s.fetchMu.Unlock()
			if ok && len(cachedBlocks) > 0 && len(cachedBlocks[0]) > 0 {
				resChan <- downloadResult{height: h, data: cachedBlocks[0]}
				continue
			}

			// Kích hoạt tải song song
			sem <- struct{}{}
			go func(height uint64, hBytes []byte) {
				defer func() { <-sem }()
				blockRaw, err := s.getOrReconstructBlock(targetPeer, height, hBytes)
				resChan <- downloadResult{height: height, data: blockRaw, err: err}
			}(h, hdrMap[h])
		}

		// Thu thập kết quả tải về
		results := make(map[uint64][]byte)
		var syncErr error
		for i := 0; i < batchSize; i++ {
			res := <-resChan
			if res.err != nil {
				syncErr = res.err
			} else {
				results[res.height] = res.data
			}
		}

		if syncErr != nil {
			if isNetworkError(syncErr) {
				log.Printf("[SYNC-TIMEOUT] Peer %s timeout/mất kết nối khi tải song song thân khối. Ngắt TCP, không phạt IP.", s.shortID(targetPeer))
				s.netManager.Host.Network().ClosePeer(targetPeer)
			} else {
				log.Printf("[SYNC-ERROR] Tải song song thân khối thất bại: %v", syncErr)
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("Lỗi tải song song thân khối: %v", syncErr))
			}
			return
		}

		// Xây dựng lại chuỗi tuần tự và xác thực chữ ký
		for h := effectiveForkPoint + 1; h <= limitH; h++ {
			blockRaw := results[h]
			var block pb_block.Block
			if err := proto.Unmarshal(blockRaw, &block); err != nil {
				log.Printf("[SYNC-ERROR] Không thể giải mã dữ liệu khối #%d: %v. Hủy bỏ quy trình Reorg.", h, err)
				return
			}
			if !s.verifyBlockSignatures(&block) {
				audit.AuditLog("CATCHUP_SIGNATURE_SPOOFING", s.shortID(targetPeer), fmt.Sprintf("Phát hiện khối rác có chữ ký giao dịch giả mạo tại #%d! Hủy tải chuỗi và trừng phạt Peer.", h))
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi khối #%d có chữ ký giao dịch không hợp lệ tại CatchUpSync", h))
				s.netManager.Host.Network().ClosePeer(targetPeer)
				return
			}

			debtChain = append(debtChain, blockRaw)

			s.mu.Lock()
			s.downloadingHeight = h
			s.mu.Unlock()

			// Cập nhật Watchdog
			s.orphanMu.Lock()
			for _, inv := range s.orphanTracker {
				if inv.Sender == targetPeer {
					inv.IsBodySync = true
					inv.LastActive = time.Now()
				}
			}
			s.orphanMu.Unlock()
		}

		// 4. Ném cả mẻ khối cho Rust ProcessChain xử lý theo mẻ nhỏ (Chunking)
		// Tại sao thiết kế như vậy: Khi lệch quá sâu (ví dụ 1000+ khối), việc đẩy toàn bộ các khối thô qua gRPC
		// trong một lần gọi duy nhất sẽ khiến Rust Core mất rất nhiều thời gian ghi đĩa RocksDB tuần tự,
		// dễ gây ra gRPC deadline timeout (5 phút). Chia thành mẻ nhỏ 100 khối giúp hoàn thành nhanh và an toàn.
		if len(debtChain) > 0 {
			log.Printf("[SYNC-LOCATOR] 🧩 Đã gom đủ %d khối. Bắt đầu xử lý theo mẻ nhỏ qua Rust Core...", len(debtChain))
			
			chunkSize := 10
			var pErr error
			var lastResp *pb_block.SyncChainResponse
			
			for i := 0; i < len(debtChain); i += chunkSize {
				end := i + chunkSize
				if end > len(debtChain) {
					end = len(debtChain)
				}
				chunk := debtChain[i:end]
				log.Printf("[SYNC-LOCATOR] 📦 Gửi mẻ khối từ %d đến %d (Tổng %d khối) xuống Rust Core...", i+1, end, len(chunk))
				
				resp, err := s.netManager.Bridge.ProcessChain(chunk)
				if err != nil {
					pErr = err
					break
				}
				if resp != nil && (resp.Status == 2 || resp.Status == 4) { // INVALID_CHAIN (2) hoặc INTERNAL_ERROR (4)
					lastResp = resp
					pErr = fmt.Errorf("process chain chunk failed with status %d: %s", resp.Status, resp.ErrorMsg)
					break
				}
				lastResp = resp
				// Cập nhật currentHeight của Go ngay sau mỗi mẻ thành công để đồng bộ tiến trình
				if resp != nil && resp.Status == 1 {
					s.mu.Lock()
					s.currentHeight = resp.NewHeight
					s.mu.Unlock()
				}
			}

			if pErr == nil && lastResp != nil && (lastResp.Status == 1 || lastResp.Status == 0) { // [VANGUARD-FIX] Chấp nhận cả REORG_SUCCESS (1) và ACCEPTED (0 - sidechain hợp lệ nhưng nhẹ hơn)
				if lastResp.Status == 1 {
					log.Printf("[REORG-SUCCESS] %s", i18n.T("log_reorg_success", lastResp.NewHeight))
					s.mu.Lock()
					s.currentHeight = lastResp.NewHeight
					s.mu.Unlock()

					// [VNT-CONSENSUS POST-CLEANUP] KIỂM TRA XEM CÓ PHẢI LÀ DEEP REORG KHÔNG?
					if effectiveForkPoint < fH {
						log.Printf("🌪️ [INVISIBLE-HAND] CƠN BÃO ĐI QUA: Bàn tay vô hình vừa cắt bỏ lịch sử sâu (từ #%d về #%d). Kích hoạt dọn dẹp quy mô lớn!", fH, effectiveForkPoint)
						
						// 1. Dọn sạch Mempool hoàn toàn (Giao dịch cũ có thể lỗi Nonce/Số dư)
						if s.mempool != nil {
							s.mempool.Purge()
						}
						// 2. Gỡ bỏ mọi án phạt mạng (Đại Ân Xá)
						if s.netManager.BanMgr != nil {
							s.netManager.BanMgr.ClearAllBans()
						}
						s.netManager.PenaltyMu.Lock()
						s.netManager.PeerPenalties = make(map[peer.ID]int)
						s.netManager.PeerPenaltyTimes = make(map[peer.ID]time.Time)
						s.netManager.PeerUnbanTimes = make(map[peer.ID]time.Time)
						s.netManager.PenaltyMu.Unlock()

						// 3. Tín hiệu cho RPC Server làm sạch bộ đệm UI (OnRollback)
						if s.netManager.OnRollback != nil {
							go s.netManager.OnRollback(effectiveForkPoint)
						}
					} else {
						// Reorg bình thường trong vùng linh hoạt 5 khối
						if s.mempool != nil {
							s.mempool.Purge()
						}
					}
				} else {
					log.Printf("[SYNC-SUCCESS] 🌾 Gom khối Locator thành công nhưng chuỗi rẽ nhánh nhẹ hơn/bằng chuỗi chính. Đã lưu side-chain an toàn tại cao độ #%d. Không phạt peer.", lastResp.NewHeight)
					
					// [VÁ LỖI KẸT CHUỖI NHẸ CHÚNG] Ghi nhớ các block sidechain và thiết lập cooldown
					s.mu.Lock()
					for _, blockBytes := range debtChain {
						if headerBytes, err := ExtractHeaderBytesFromBlockBytes(blockBytes); err == nil {
							blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
							hashStr := hex.EncodeToString(blockHash)
							s.knownSidechains[hashStr] = time.Now()
						}
					}
					s.sidechainPeers[targetPeer] = time.Now().Add(5 * time.Minute)
					s.mu.Unlock()
					log.Printf("[SYNC-SUCCESS] 🌾 Đã ghi nhớ %d blocks sidechain và đặt cooldown cho Peer %s", len(debtChain), s.shortID(targetPeer))
				}
			} else {
				errMsg := "unknown"
				if pErr != nil {
					errMsg = pErr.Error()
				} else if lastResp != nil {
					errMsg = lastResp.ErrorMsg
				}
				log.Printf("[SYNC-REJECT] ⚠️ Rust từ chối chuỗi khối từ %s: %v", s.shortID(targetPeer), errMsg)

				if lastResp != nil && lastResp.Status == 4 {
					log.Printf("[SYNC-ERROR] 🛑 DB nội bộ bị lỗi/hỏng khi xử lý chuỗi từ Peer %s: %s. KHÔNG BAN PEER!", s.shortID(targetPeer), errMsg)
				} else if lastResp != nil && lastResp.Status == 2 && (strings.Contains(errMsg, "Không tìm thấy điểm rẽ nhánh") || strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "Parent hash")) {
					log.Printf("[SYNC-ORPHAN] 🧩 Node lệch chuỗi sâu hoặc thiếu cha so với Peer %s. KHÔNG PHẠT PEER, giữ kết nối để cơ chế phục hồi tự giải quyết.", s.shortID(targetPeer))
				} else if lastResp != nil && lastResp.Status == 2 && strings.Contains(errMsg, "LỆCH TX ROOT") {
					log.Printf("[SECURITY] 🛑 Peer %s gửi chuỗi khối có LỆCH TX ROOT tại CatchUpSync. 100%% Miner gian lận hoặc gửi block hỏng. TRỪNG PHẠT NGAY LẬP TỨC!", s.shortID(targetPeer))
					s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi khối lệch Tx Root tại CatchUpSync: %s", errMsg))
				} else if lastResp != nil && lastResp.Status == 2 && strings.Contains(errMsg, "LỆCH STATE ROOT") {
					peerRootErrorsMu.Lock()
					peerRootErrors[targetPeer]++
					errCount := peerRootErrors[targetPeer]
					peerRootErrorsMu.Unlock()

					if errCount <= 1 {
						log.Printf("[SYNC-HEAL] ⚠️ Phát hiện LỆCH STATE ROOT từ %s tại CatchUpSync (Lần %d). Đang tỉa Cache lỗi và tự kết nối lại, KHÔNG PHẠT PEER.", s.shortID(targetPeer), errCount)
						
						// [Surgical Drop] Chỉ tỉa các khối thuộc chùm bị lỗi
						s.fetchMu.Lock()
						for h := evalResp.ForkPoint + 1; h <= highestH; h++ {
							delete(s.pendingBlocks, h)
						}
						s.fetchMu.Unlock()

						s.orphanHeadersMu.Lock()
						for _, hBytes := range hBatch {
							blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(hBytes)
							if len(blockHash) > 0 {
								hashStr := hex.EncodeToString(blockHash)
								delete(s.orphanHeaders, hashStr)
								delete(s.orphanTxIDs, hashStr)
								delete(s.orphanCoinbase, hashStr)
							}
						}
						s.orphanHeadersMu.Unlock()

						s.netManager.Host.Network().ClosePeer(targetPeer)
					} else {
						log.Printf("[SECURITY] 🛑 Peer %s gửi chuỗi khối lệch STATE ROOT liên tiếp tại CatchUpSync (%d lần). TRỪNG PHẠT!", s.shortID(targetPeer), errCount)
						s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi chuỗi khối lệch State Root liên tiếp tại CatchUpSync: %s", errMsg))

						peerRootErrorsMu.Lock()
						delete(peerRootErrors, targetPeer)
						peerRootErrorsMu.Unlock()
					}
				} else {
					log.Printf("[SECURITY] 🛑 Peer %s gửi dữ liệu gian lận: %s. TRỪNG PHẠT!", s.shortID(targetPeer), errMsg)
					s.netManager.punishPeer(targetPeer, fmt.Sprintf("Rust Core Rejected: %s", errMsg))
				}
			}
		}

		// Cập nhật điểm tiếp theo để loop tải tiếp
		effectiveForkPoint = limitH
	}
} else if evalResp.Status == 4 { // INTERNAL_ERROR
		log.Printf("[SYNC-ERROR] 🛑 DB nội bộ bị lỗi/hỏng khi thẩm định chuỗi từ Peer %s: %s. KHÔNG BAN PEER!", s.shortID(targetPeer), evalResp.ErrorMsg)
	} else if evalResp.Status == 2 { // INVALID
		if strings.Contains(evalResp.ErrorMsg, "ERR_IMMUTABLE_FIREWALL_VIOLATION") || strings.Contains(evalResp.ErrorMsg, "vi phạm") || strings.Contains(evalResp.ErrorMsg, "FIREWALL") {
			log.Printf("[SECURITY-ALERT] 🛑 VI PHẠM TƯỜNG LỬA BẤT BIẾN: Peer %s gửi chuỗi Header vi phạm tường lửa! TRỪNG PHẠT NGHIÊM KHẮC! Lỗi: %s", s.shortID(targetPeer), evalResp.ErrorMsg)
			s.netManager.punishPeer(targetPeer, "VI PHẠM TƯỜNG LỬA BẤT BIẾN: "+evalResp.ErrorMsg)
			s.netManager.Host.Network().ClosePeer(targetPeer)
		} else {
			log.Printf("[SECURITY] 🛑 Peer %s gửi nhánh rẽ giả mạo. Ngắt kết nối! Lỗi: %s", s.shortID(targetPeer), evalResp.ErrorMsg)
			s.netManager.punishPeer(targetPeer, "INVALID_FORK_DATA: "+evalResp.ErrorMsg)
			s.netManager.Host.Network().ClosePeer(targetPeer)
		}
	} else {
		log.Printf("[SYNC-REORG] 🌾 Peer %s đang ở chuỗi ngắn hơn hoặc bằng. Bỏ qua.", s.shortID(targetPeer))
	}
}

// alignOrphanChain thực hiện kích hoạt đồng bộ chùm (Batch Sync) từ mốc bất biến để xử lý khối mồ côi từ Peer mục tiêu.
// [3-STRIKE RULE] Hệ thống cảnh cáo lũy tiến dựa trên KẾT QUẢ đồng bộ chùm:
//   - Strike chỉ tăng SAU KHI CatchUpSync thực sự chạy xong và THẤT BẠI (peer không cung cấp được chuỗi).
//   - Strike 1: Tha thứ, ghi sổ
//   - Strike 2: Cảnh cáo
//   - Strike 3+: Xử phạt Peer (punishPeer) + ngắt kết nối. KHÔNG đồng bộ chùm nữa.
// Tại sao: Peer trung thực gửi mồ côi là chuyện bình thường (do mạng lệch pha). Ta chỉ phạt khi
// peer KHÔNG BAO GIỜ cung cấp được chuỗi hợp lệ sau nhiều lần đồng bộ chùm thực tế.
// Reset strike về 0 khi CatchUpSync thành công (đỉnh chuỗi tăng) để không phạt oan peer trung thực.
func (s *SyncEngine) alignOrphanChain(targetPeer peer.ID, startMissingHash []byte, orphanHeaderRaw []byte) error {
	peerShort := s.shortID(targetPeer)
	hashHex := hex.EncodeToString(startMissingHash)
	if len(hashHex) > 12 {
		hashHex = hashHex[:12]
	}

	// [3-STRIKE] Dọn dẹp entry quá 30 phút không hoạt động để tránh phình RAM
	s.orphanSyncStrikesMu.Lock()
	now := time.Now()
	for pid, ts := range s.orphanSyncStrikesTs {
		if now.Sub(ts) > 30*time.Minute {
			delete(s.orphanSyncStrikes, pid)
			delete(s.orphanSyncStrikesTs, pid)
		}
	}
	currentStrike := s.orphanSyncStrikes[targetPeer]
	s.orphanSyncStrikesMu.Unlock()

	// ═══════════════════════════════════════════════════════════════════
	// CHỐT CHẶN: Nếu peer đã đạt strike 3+ → phạt ngay, KHÔNG đồng bộ chùm nữa
	// Tại sao: Peer này đã 3 lần liên tiếp không cung cấp được chuỗi hợp lệ
	// sau khi CatchUpSync thực sự chạy. Không lãng phí tài nguyên thêm nữa.
	// ═══════════════════════════════════════════════════════════════════
	if currentStrike >= 3 {
		log.Printf("[3-STRIKE] 🔴 XỬ PHẠT Peer %s (Strike %d/3): Đã %d lần đồng bộ chùm thất bại (Hash: %s). NGẮT KẾT NỐI!", peerShort, currentStrike, currentStrike, hashHex)
		s.netManager.punishPeer(targetPeer, fmt.Sprintf("3-Strike: %d lần đồng bộ chùm thất bại mà không cung cấp chuỗi hợp lệ (Hash: %s)", currentStrike, hashHex))
		s.netManager.Host.Network().ClosePeer(targetPeer)
		return nil
	}

	// ═══════════════════════════════════════════════════════════════════
	// CHỐT CHẶN: Nếu CatchUpSync đang bận → bỏ qua, KHÔNG tăng strike
	// Tại sao: CatchUpSync có cờ atomic chỉ cho phép 1 goroutine chạy.
	// Nếu đang bận mà ta vẫn tăng strike → peer trung thực bị phạt oan
	// chỉ vì gửi nhiều khối mồ côi liên tiếp (chuyện bình thường trên mạng).
	// ═══════════════════════════════════════════════════════════════════
	if atomic.LoadInt32(&s.catchUpRunning) >= 1 {
		log.Printf("[3-STRIKE] ⏸️ CatchUpSync đang bận. Bỏ qua yêu cầu đồng bộ chùm từ Peer %s, KHÔNG tăng strike.", peerShort)
		return nil
	}

	// ═══════════════════════════════════════════════════════════════════
	// THỰC HIỆN ĐÒI NỢ bất đồng bộ: CatchUpSync từ mốc bất biến fH + 1
	// Strike chỉ tăng SAU KHI CatchUpSync chạy xong và THẤT BẠI.
	// Tại sao bất đồng bộ: alignOrphanChain được gọi từ syncLoop (đồng bộ)
	// và HandleBlockArrival (trong goroutine). Nếu chạy đồng bộ CatchUpSync ở đây
	// sẽ block syncLoop hàng chục giây → hệ thống đông cứng.
	// ═══════════════════════════════════════════════════════════════════
	log.Printf("[3-STRIKE] 📡 Đồng bộ chùm Peer %s (Strike hiện tại: %d/3, Hash: %s). Kích hoạt CatchUpSync từ mốc bất biến...", peerShort, currentStrike, hashHex)

	go func() {
		heightBefore := s.netManager.Bridge.GetCurrentVersion()
		s.CatchUpSync(targetPeer)
		heightAfter := s.netManager.Bridge.GetCurrentVersion()

		if heightAfter > heightBefore {
			// ✅ THÀNH CÔNG: Peer cung cấp chuỗi hợp lệ → reset strike về 0
			s.orphanSyncStrikesMu.Lock()
			delete(s.orphanSyncStrikes, targetPeer)
			delete(s.orphanSyncStrikesTs, targetPeer)
			s.orphanSyncStrikesMu.Unlock()
			log.Printf("[3-STRIKE] ✅ Peer %s đã cung cấp chuỗi hợp lệ! Đỉnh tăng #%d → #%d. Reset strike.", peerShort, heightBefore, heightAfter)
		} else {
			// ❌ THẤT BẠI: Peer không cung cấp được chuỗi → LÚC NÀY mới tăng strike
			s.orphanSyncStrikesMu.Lock()
			s.orphanSyncStrikes[targetPeer]++
			newStrike := s.orphanSyncStrikes[targetPeer]
			s.orphanSyncStrikesTs[targetPeer] = time.Now()
			s.orphanSyncStrikesMu.Unlock()

			switch newStrike {
			case 1:
				// Lần 1 thất bại: Tha thứ, chỉ ghi sổ
				log.Printf("[3-STRIKE] 📝 Tha thứ lần 1 cho Peer %s: CatchUpSync không thành công (Hash: %s). Ghi sổ, chờ lần tới.", peerShort, hashHex)
			case 2:
				// Lần 2 thất bại: Cảnh cáo nghiêm khắc
				log.Printf("[3-STRIKE] 🟡 CẢNH CÁO Peer %s (2/3): Lần thứ 2 đồng bộ chùm CatchUpSync thất bại (Hash: %s). Lần tới sẽ bị XỬ PHẠT!", peerShort, hashHex)
			default:
				// Lần 3+ thất bại: Xử phạt + Ngắt kết nối
				log.Printf("[3-STRIKE] 🔴 XỬ PHẠT Peer %s (Strike %d/3): %d lần đồng bộ chùm thất bại liên tiếp (Hash: %s). NGẮT KẾT NỐI!", peerShort, newStrike, newStrike, hashHex)
				// Khôi phục lệnh phạt vì CatchUpSync không còn tự động đánh Strike khi Timeout
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("3-Strike: %d lần đồng bộ chùm thất bại (Mạng kém/Lỗi liên tục)", newStrike))
				s.netManager.Host.Network().ClosePeer(targetPeer)
			}
		}
	}()

	return nil
}



// applyDebtHeaders tải Body cho chuỗi đồng bộ chùm và nạp atomical xuống Rust Core để hoàn tất reorg
func (s *SyncEngine) applyDebtHeaders(targetPeer peer.ID, hBatch [][]byte, forkPoint uint64) error {
	var highestH uint64
	hdrMap := make(map[uint64][]byte)
	for _, hBytes := range hBatch {
		var hdr pb_block.BlockHeader
		if err := proto.Unmarshal(hBytes, &hdr); err == nil {
			hdrMap[hdr.Height] = hBytes
			if hdr.Height > highestH {
				highestH = hdr.Height
			}
		}
	}

	log.Printf("[DEBT-COLLECTION] 🔄 Chùm Header đồng bộ chùm hợp lệ tại ForkPoint #%d! Bắt đầu tải thân khối tiến lên...", forkPoint)

	debtChain := make([][]byte, 0, int(highestH-forkPoint))
	for h := forkPoint + 1; h <= highestH; h++ {
		fH := s.netManager.Bridge.GetFinalizedHeight()
		oldestH := s.netManager.Bridge.GetOldestHeight()

		var blockRaw []byte
		if h < fH || h < oldestH {
			log.Printf("[SYNC-LIGHTWEIGHT-DEBT] 🕊️ Khối lịch sử #%d dưới mốc Snapshot (#%d) hoặc Đại Thanh Trừng (#%d). Tạo Header-Only block.", h, fH, oldestH)
			hBytes := hdrMap[h]
			var hdr pb_block.BlockHeader
			if err := proto.Unmarshal(hBytes, &hdr); err == nil {
				fullBlock := &pb_block.Block{
					Header: &hdr,
					Body:   nil,
				}
				blockRaw, _ = proto.Marshal(fullBlock)
			}
		}

		if blockRaw == nil {
			// [RAM-CACHE-LOOKUP] Kiểm tra xem khối có đang nằm sẵn trong bộ nhớ RAM không
			s.fetchMu.Lock()
			cachedBlocks, ok := s.pendingBlocks[h]
			if ok && len(cachedBlocks) > 0 && len(cachedBlocks[0]) > 0 {
				blockRaw = cachedBlocks[0]
			}
			s.fetchMu.Unlock()

			if blockRaw == nil {
				var err error
				hBytes := hdrMap[h]
				blockRaw, err = s.getOrReconstructBlock(targetPeer, h, hBytes)
				if err != nil {
					log.Printf("[DEBT-COLLECTION-ERROR] ❌ Lấy thân khối #%d thất bại. Hủy bỏ Reorg.", h)
					return fmt.Errorf("failed to fetch body for block %d", h)
				}
			} else {
				log.Printf("[SYNC-CACHE] 💾 Lấy khối #%d trực tiếp từ RAM (pendingBlocks), tiết kiệm băng thông mạng!", h)
			}
		}

		// [SECURITY-SHIELD] Xác thực chữ ký giao dịch của khối ngay lập tức trước khi tiếp tục tải khối tiếp theo.
		// Tại sao: Chống tấn công Signature Bomb DoS làm cạn kiệt bộ nhớ RAM khi tải hàng loạt khối rác trước khi đẩy xuống Rust Core.
		var block pb_block.Block
		if err := proto.Unmarshal(blockRaw, &block); err != nil {
			log.Printf("[DEBT-COLLECTION-ERROR] ❌ Không thể giải mã dữ liệu khối #%d: %v. Hủy bỏ Reorg.", h, err)
			return fmt.Errorf("failed to decode block %d", h)
		}
		if !s.verifyBlockSignatures(&block) {
			log.Printf("[SECURITY-ALERT] 🛑 Phát hiện khối rác có chữ ký giả mạo tại #%d từ %s! Hủy tải chuỗi và trừng phạt Peer.", h, s.shortID(targetPeer))
			s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi khối #%d có chữ ký giao dịch không hợp lệ tại Debt Collection", h))
			s.netManager.Host.Network().ClosePeer(targetPeer)
			return fmt.Errorf("invalid transaction signature in block %d", h)
		}

		debtChain = append(debtChain, blockRaw)

		// [WATCHDOG-FEEDING-V3] Đánh dấu đang tải Body nặng và cho chó ăn thêm thời gian 30 phút!
		s.orphanMu.Lock()
		for _, inv := range s.orphanTracker {
			if inv.Sender == targetPeer {
				inv.IsBodySync = true
				inv.LastActive = time.Now()
			}
		}
		s.orphanMu.Unlock()
	}

	if len(debtChain) > 0 {
		log.Printf("[DEBT-COLLECTION] 🧩 Đã gom đủ %d khối Full. Tiến hành xử lý theo mẻ nhỏ qua Rust Core...", len(debtChain))
		var pErr error
		var resp *pb_block.SyncChainResponse
		
		chunkSize := 10
		var lastResp *pb_block.SyncChainResponse
		
		for i := 0; i < len(debtChain); i += chunkSize {
			end := i + chunkSize
			if end > len(debtChain) {
				end = len(debtChain)
			}
			chunk := debtChain[i:end]
			log.Printf("[DEBT-COLLECTION] 📦 Gửi mẻ khối từ %d đến %d (Tổng %d khối) xuống Rust Core...", i+1, end, len(chunk))
			
			var err error
			resp, err = s.netManager.Bridge.ProcessChain(chunk)
			if err != nil {
				pErr = err
				break
			}
			if resp != nil && (resp.Status == 2 || resp.Status == 4) {
				lastResp = resp
				pErr = fmt.Errorf("process chain chunk failed with status %d: %s", resp.Status, resp.ErrorMsg)
				break
			}
			lastResp = resp
			// Cập nhật currentHeight của Go ngay sau mỗi mẻ thành công để đồng bộ tiến trình
			if resp != nil && resp.Status == 1 {
				s.mu.Lock()
				s.currentHeight = resp.NewHeight
				s.mu.Unlock()
			}
		}
		resp = lastResp
		if pErr == nil && (resp.Status == 1 || resp.Status == 0) { // [VANGUARD-FIX] Chấp nhận cả REORG_SUCCESS (1) và ACCEPTED (0 - sidechain hợp lệ)
			if resp.Status == 1 {
				log.Printf("[DEBT-COLLECTION-SUCCESS] ✅ Đã nạp thành công chuỗi đồng bộ chùm %d khối đầy đủ và Reorg thành công.", len(debtChain))
				// [VANGUARD-FIX] Đồng bộ dọn dẹp Mempool sau khi Reorg thành công để xóa bỏ stale projected nonces/pending spend
				if s.mempool != nil {
					s.mempool.Purge()
				}
			} else {
				log.Printf("[DEBT-COLLECTION-SUCCESS] 🌾 Đã nạp thành công chuỗi đồng bộ chùm %d khối đầy đủ (Lưu side-chain, nhẹ hơn/bằng chuỗi chính). Không phạt peer.", len(debtChain))
				
				// [VÁ LỖI KẸT CHUỖI NHẸ CHÚNG] Ghi nhớ các block sidechain và thiết lập cooldown
				s.mu.Lock()
				for _, blockBytes := range debtChain {
					if headerBytes, err := ExtractHeaderBytesFromBlockBytes(blockBytes); err == nil {
						blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
						hashStr := hex.EncodeToString(blockHash)
						s.knownSidechains[hashStr] = time.Now()
					}
				}
				s.sidechainPeers[targetPeer] = time.Now().Add(5 * time.Minute)
				s.mu.Unlock()
				log.Printf("[DEBT-COLLECTION-SUCCESS] 🌾 Đã ghi nhớ %d blocks sidechain và đặt cooldown cho Peer %s", len(debtChain), s.shortID(targetPeer))
			}

			// [LIGHTWEIGHT-ORPHAN-CACHE] Dọn dẹp cache Header mồ côi ngay khi Reorg thành công
			s.orphanHeadersMu.Lock()
			for _, hBytes := range hBatch {
				blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(hBytes)
				if len(blockHash) > 0 {
					hashStr := hex.EncodeToString(blockHash)
					delete(s.orphanHeaders, hashStr)
					delete(s.orphanTxIDs, hashStr)
					delete(s.orphanCoinbase, hashStr)
				}
			}
			s.orphanHeadersMu.Unlock()

			return nil
		} else {
			errMsg := "unknown"
			if pErr != nil {
				errMsg = pErr.Error()
			} else if resp != nil {
				errMsg = resp.ErrorMsg
			}
			log.Printf("[DEBT-COLLECTION-REJECT] ⚠️ Rust từ chối chuỗi khối đồng bộ chùm (%d khối) từ Peer %s: %v.", len(debtChain), targetPeer.String()[:12], errMsg)

			if resp != nil && resp.Status == 4 {
				log.Printf("[SYNC-ERROR] 🛑 DB nội bộ bị lỗi/hỏng khi xử lý chuỗi đồng bộ chùm từ Peer %s: %s. KHÔNG BAN PEER!", targetPeer.String()[:12], errMsg)
			} else if resp != nil && resp.Status == 2 && strings.Contains(errMsg, "LỆCH TX ROOT") {
				log.Printf("[SECURITY] 🛑 Peer %s gửi chuỗi khối đồng bộ chùm có LỆCH TX ROOT. 100%% Miner gian lận hoặc gửi block hỏng. TRỪNG PHẠT NGAY LẬP TỨC!", targetPeer.String()[:12])
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("Gửi khối đồng bộ chùm lệch Tx Root: %s", errMsg))
			} else if resp != nil && resp.Status == 2 && strings.Contains(errMsg, "LỆCH STATE ROOT") {
				peerRootErrorsMu.Lock()
				peerRootErrors[targetPeer]++
				errCount := peerRootErrors[targetPeer]
				peerRootErrorsMu.Unlock()

				if errCount <= 1 {
					log.Printf("[SYNC-HEAL] ⚠️ Lệch STATE ROOT chuỗi đồng bộ chùm từ %s (Lần %d). Đang tỉa Cache lỗi và tự kết nối lại, KHÔNG PHẠT PEER.", targetPeer.String()[:12], errCount)
					
					// [Surgical Drop] Chỉ tỉa các khối thuộc chuỗi đồng bộ chùm bị lỗi
					s.fetchMu.Lock()
					for h := forkPoint + 1; h <= highestH; h++ {
						delete(s.pendingBlocks, h)
					}
					s.fetchMu.Unlock()

					s.orphanHeadersMu.Lock()
					for _, hBytes := range hBatch {
						blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(hBytes)
						hashStr := hex.EncodeToString(blockHash)
						delete(s.orphanHeaders, hashStr)
						delete(s.orphanTxIDs, hashStr)
						delete(s.orphanCoinbase, hashStr)
					}
					s.orphanHeadersMu.Unlock()
					
					s.netManager.Host.Network().ClosePeer(targetPeer)
				} else {
					log.Printf("[SECURITY] 🛑 Peer %s gửi chuỗi khối đồng bộ chùm lệch STATE ROOT liên tiếp (%d lần). TRỪNG PHẠT!", targetPeer.String()[:12], errCount)
					s.netManager.punishPeer(targetPeer, fmt.Sprintf("Chuỗi khối đồng bộ chùm bị Rust Core từ chối liên tiếp do lệch State Root: %s", errMsg))

					peerRootErrorsMu.Lock()
					delete(peerRootErrors, targetPeer)
					peerRootErrorsMu.Unlock()
				}
			} else if resp != nil && resp.Status == 2 && (strings.Contains(errMsg, "Không tìm thấy điểm rẽ nhánh") || strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "Parent hash")) {
				log.Printf("[SYNC-ORPHAN] 🧩 Node lệch chuỗi sâu hoặc thiếu cha so với Peer %s. KHÔNG PHẠT PEER, giữ kết nối để cơ chế phục hồi tự giải quyết.", targetPeer.String()[:12])
			} else {
				log.Printf("[SECURITY] 🛑 Peer %s gửi dữ liệu gian lận: %s. TRỪNG PHẠT!", targetPeer.String()[:12], errMsg)
				s.netManager.punishPeer(targetPeer, fmt.Sprintf("Chuỗi khối đồng bộ chùm bị Rust Core từ chối: %s", errMsg))
			}
			return fmt.Errorf("rust core rejected chain: %s", errMsg)
		}
	}

	return fmt.Errorf("empty debt chain")
}

// getOrReconstructBlock: [RECONSTRUCTION-CACHE] Thử tái tạo khối cục bộ từ Mempool trước khi tải từ mạng P2P.
// Giải thích: Thay vì đầu hàng tải Full Block ngay lập tức khi thiếu giao dịch, Node sẽ quét thu thập toàn bộ các chỉ mục bị thiếu,
// gửi ĐÚNG 1 YÊU CẦU duy nhất qua RequestBlockTxn để Peer trả về gói giao dịch thiếu, giúp tiết kiệm tối đa băng thông đường truyền.
func (s *SyncEngine) getOrReconstructBlock(targetPeer peer.ID, height uint64, headerBytes []byte) ([]byte, error) {
	if len(headerBytes) == 0 {
		return s.netManager.GetBlockFromNetwork(targetPeer, height)
	}

	var header pb_block.BlockHeader
	if err := proto.Unmarshal(headerBytes, &header); err != nil {
		return s.netManager.GetBlockFromNetwork(targetPeer, height)
	}

	blockHash := s.netManager.Bridge.CalculateBlockHeaderHash(headerBytes)
	hashStr := hex.EncodeToString(blockHash)

	s.orphanHeadersMu.RLock()
	txIDs, exists := s.orphanTxIDs[hashStr]
	coinbaseBytes, hasCoinbase := s.orphanCoinbase[hashStr]
	s.orphanHeadersMu.RUnlock()

	if !exists || len(txIDs) == 0 {
		log.Printf("[RECONSTRUCT-CACHE-MISS] 🔍 Không có TxIDs cache cho khối #%d (%s). Fallback tải từ mạng...", height, hashStr[:12])
		// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height để tránh bị lệch canonical khi Peer reorg ở vùng đỉnh
		return s.netManager.RequestBlockByHash(targetPeer, blockHash)
	}

	log.Printf("[RECONSTRUCT-TRY] 🧩 Tìm thấy %d TxIDs và trạng thái Coinbase (%t) cho khối #%d (%s). Tiến hành tái tạo cục bộ...", len(txIDs), hasCoinbase, height, hashStr[:12])

	transactions := make([]*pb_block.Transaction, len(txIDs))
	txHashesList := make([][]byte, len(txIDs))
	missingIndexes := make([]uint32, 0)

	// Bước 1: Quét nhanh để kiểm tra và thu thập các chỉ mục (indexes) bị thiếu trong Mempool/Cache
	for i, txID := range txIDs {
		var txBytes []byte
		var found bool

		if i == 0 {
			// Giao dịch coinbase lấy từ cache riêng
			if hasCoinbase && len(coinbaseBytes) > 0 {
				txBytes = coinbaseBytes
				found = true
			}
		} else {
			// Giao dịch thường lấy từ Mempool cục bộ
			txHashHex := hex.EncodeToString(txID)
			txBytes, found = s.mempool.GetTransaction(txHashHex)
		}

		if found {
			var tx pb_block.Transaction
			if err := proto.Unmarshal(txBytes, &tx); err == nil {
				transactions[i] = &tx
			} else {
				found = false // Đánh dấu là thiếu nếu giải mã bị lỗi
			}
		}

		if !found {
			missingIndexes = append(missingIndexes, uint32(i))
		}
	}

	// Bước 2: Đi đòi các giao dịch bị thiếu bằng một request đóng gói duy nhất (nếu có) trước khi quyết định tải Full Block
	if len(missingIndexes) > 0 {
		// [SECURITY-LIMIT] Nếu thiếu 100% giao dịch (không có gì trong Mempool), tải Full Block luôn để tối ưu luồng xử lý
		if len(missingIndexes) == len(txIDs) {
			log.Printf("[RECONSTRUCT-FAIL] ⚠️ Thiếu 100%% giao dịch tại khối #%d. Fallback tải thẳng FULL khối từ mạng...", height)
			// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height để tránh bị lệch canonical khi Peer reorg ở vùng đỉnh
			return s.netManager.RequestBlockByHash(targetPeer, blockHash)
		}

		log.Printf("[RECONSTRUCT] 🧩 Thiếu %d/%d giao dịch tại khối #%d (%s). Gửi đúng 1 request P2P xin gói giao dịch thiếu...", len(missingIndexes), len(txIDs), height, hashStr[:12])

		// Thiết lập timeout 3 giây chặt chẽ khi gọi RequestBlockTxn tránh bị kẹt kết nối (Chốt chặn an toàn)
		tCtx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		missingTxs, err := s.netManager.RequestBlockTxn(tCtx, targetPeer, blockHash, missingIndexes)
		cancel()

		if err == nil && len(missingTxs) == len(missingIndexes) {
			hasNilTx := false
			// Kiểm tra an toàn xem có giao dịch nào bị nil trong gói trả về hay không
			for _, tx := range missingTxs {
				if tx == nil {
					hasNilTx = true
					break
				}
			}

			if hasNilTx {
				log.Printf("[RECONSTRUCT-FAIL] ⚠️ Nhận được giao dịch nil từ Peer %s khi xin giao dịch thiếu. Trừng phạt peer và fallback tải FULL khối...", targetPeer.String()[:12])
				s.netManager.punishPeer(targetPeer, "Gửi giao dịch nil trong gói RequestBlockTxn")
				// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height
				return s.netManager.RequestBlockByHash(targetPeer, blockHash)
			}

			// Điền các giao dịch nhận được vào đúng vị trí trống trong mảng lắp ráp
			for k, idx := range missingIndexes {
				tx := missingTxs[k]
				transactions[idx] = tx
			}
			log.Printf("[RECONSTRUCT] ✅ Xin thành công và nạp %d giao dịch bị thiếu từ Peer %s.", len(missingTxs), targetPeer.String()[:12])
		} else {
			// Thất bại khi xin (lỗi mạng hoặc timeout) -> Fallback tải Full block
			log.Printf("[RECONSTRUCT-FAIL] ⚠️ Không xin được giao dịch thiếu từ Peer %s (%v). Fallback tải FULL khối...", targetPeer.String()[:12], err)
			// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height
			return s.netManager.RequestBlockByHash(targetPeer, blockHash)
		}
	}

	// Gom lô băm giao dịch (Batch Hashing) thông qua gRPC xuống Rust Core để tối ưu hóa hiệu năng và đảm bảo đồng thuận an toàn
	var rawTxs [][]byte
	for _, tx := range transactions {
		txBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
		rawTxs = append(rawTxs, txBytes)
	}

	hashes, err := s.netManager.Bridge.CalculateTxHashesBatch(rawTxs, height)
	if err != nil || len(hashes) != len(rawTxs) {
		log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed for reconstruction: %v. Fallback to native.", err)
		for idx, tx := range transactions {
			txBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
			txHashesList[idx] = GetTxIDNative(txBytes)
		}
	} else {
		for idx, h := range hashes {
			txHashesList[idx] = h
		}
	}

	// Bước 3: Xác thực Merkle root chéo thông qua Rust Core để loại bỏ rủi ro sai lệch dữ liệu hoặc tráo đổi giao dịch
	if !s.netManager.Bridge.VerifyBlockReconstruction(header.TxRoot.Value, txHashesList) {
		log.Printf("[RECONSTRUCT-FAIL] 🛡️ Merkle root mismatch cho khối tái lập #%d (%s). Fallback tải FULL...", height, hashStr[:12])
		// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height
		return s.netManager.RequestBlockByHash(targetPeer, blockHash)
	}

	// Lắp ráp khối hoàn chỉnh
	fullBlock := &pb_block.Block{
		Header: &header,
		Body: &pb_block.BlockBody{
			Transactions: transactions,
		},
	}

	fullBlockBytes, err := proto.Marshal(fullBlock)
	if err != nil {
		log.Printf("[RECONSTRUCT-FAIL] ❌ Lỗi marshalling khối mồ côi tái lập: %v. Fallback tải mạng...", err)
		// [SAFETY-FIX-HASH] Tải theo Hash thay vì theo Height
		return s.netManager.RequestBlockByHash(targetPeer, blockHash)
	}

	log.Printf("[RECONSTRUCT-SUCCESS] 🎉 TÁI TẠO CỤC BỘ THÀNH CÔNG khối #%d (%s)! Tiết kiệm 100%% băng thông tải Body.", height, hashStr[:12])
	return fullBlockBytes, nil
}

// verifyBlockSignatures thực hiện xác thực chữ ký của toàn bộ các giao dịch chuyển tiền trong khối.
// Tại sao: Chống lại các cuộc tấn công DDoS bằng Bom Chữ Ký (Signature Bomb DoS) làm tràn bộ nhớ RAM (OOM) 
// của Node trước khi khối được nạp và xử lý chính thức dưới RocksDB/Rust Core. Việc kiểm tra tĩnh (Static Check) 
// bằng ed25519 nội bộ của Go giúp loại bỏ các khối rác ngay tại tầng mạng mà không gây quá tải cho Rust FFI.
// Song song hóa xác thực chữ ký bằng Goroutines giúp tận dụng tối đa sức mạnh đa nhân của Go Node để xử lý khối lớn.
func (s *SyncEngine) verifyBlockSignatures(block *pb_block.Block) bool {
	if block.Body == nil || len(block.Body.Transactions) <= 1 {
		return true // Khối rỗng hoặc chỉ chứa Coinbase (không cần xác thực chữ ký)
	}

	// Bỏ qua giao dịch Coinbase ở Index 0 (phát sinh phần thưởng, không có chữ ký người gửi)
	txs := block.Body.Transactions[1:]
	numTxs := len(txs)

	// Tối ưu hóa: Lấy số lượng nhân CPU thực tế của máy chủ để phân chia công việc tối ưu
	numCores := runtime.NumCPU()
	if numCores > numTxs {
		numCores = numTxs
	}

	var wg sync.WaitGroup
	var isValid atomic.Bool
	isValid.Store(true) // Mặc định là True (toàn bộ chữ ký hợp lệ)

	// Chia đều các giao dịch thành các phần nhỏ (Chunks) cho từng lõi CPU xử lý song song
	chunkSize := (numTxs + numCores - 1) / numCores

	for i := 0; i < numCores; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Tính toán phạm vi (Index đầu và cuối) của mảng giao dịch cho Worker này
			start := workerID * chunkSize
			end := start + chunkSize
			if end > numTxs {
				end = numTxs
			}
			if start >= numTxs {
				return // Tránh lỗi truy cập ngoài biên mảng
			}

			// Worker tiến hành kiểm tra chữ ký của phần giao dịch được giao
			for j := start; j < end; j++ {
				// [EARLY EXIT] Nếu một Worker khác đã phát hiện chữ ký giả, lập tức dừng lại để tiết kiệm CPU
				if !isValid.Load() {
					return
				}

				if !VerifySignatureNative(txs[j]) {
					// Phát hiện chữ ký giả mạo! Bật cờ isValid = false để dừng tất cả các workers khác
					isValid.Store(false)
					log.Printf("[SECURITY-ALERT] Giao dịch có chữ ký giả mạo phát hiện bởi Worker %d!", workerID)
					return
				}
			}
		}(i)
	}

	// Chờ tất cả các nhân CPU hoàn thành công việc
	wg.Wait()

	return isValid.Load()
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	networkKeywords := []string{
		"deadline",
		"timeout",
		"reset",
		"closed",
		"wsarecv",
		"eof",
		"broken pipe",
		"refused",
		"unreachable",
	}
	for _, keyword := range networkKeywords {
		if strings.Contains(errStr, keyword) {
			return true
		}
	}
	return false
}

