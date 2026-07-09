package user_interface

import (
	"bytes"
	"context"

	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	go_bridge "btc_genz/2_miner_core/go_bridge"
	node_p2p "btc_genz/5_node_p2p"
	pb_block "btc_genz/proto"
	pb_consensus "btc_genz/proto"

	"btc_genz/6_user_interface/audit"
	"btc_genz/6_user_interface/i18n"
	"btc_genz/6_user_interface/internal"

	"github.com/fatih/color"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2p_crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"  // [NAT-AUDIT] Event bus cho AutoNAT reachability
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	"google.golang.org/protobuf/proto"
)

type CLIApp struct {
	netMgr              *node_p2p.NetworkManager
	syncEngine          *node_p2p.SyncEngine
	initialSyncComplete bool // [VANGUARD-2-STAGE] Cờ hiệu đánh dấu đã xong giai đoạn đồng bộ khởi đầu
	snapshotMgr         *node_p2p.SnapshotManager
	mempool             *node_p2p.Mempool
	minerAddr           []byte
	minerKey            ed25519.PrivateKey
	minerMu             sync.RWMutex
	dbPath              string
	disableMdns         bool
	bridge              *go_bridge.Bridge
	rpcSrv              *RPCServer
	lastMinerReset      time.Time
	minerResetMu        sync.Mutex

	nodeMode           string // [V39.5] Chế độ vận hành (verify-only / full-mining)
	syncMode           string // [V1.1.4.2] Chế độ đồng bộ: "snap" (Nhảy cóc) hoặc "full" (Cày cuốc 48h)
	activeMiningMu     sync.Mutex
	activeBodyData     []byte
	activeTxHashes     [][]byte
	activeTxRoot       []byte // [ARCH-FIX] Lưu trữ TxRoot chuẩn từ Rust
	activeTxs          []*pb_block.Transaction
	activeParentHash   []byte
	activeDifficulty   []byte
	activeTargetHeight uint64
	activeTimestamp    uint64
	activeSessionId    uint64          // [V5.3] Session ID hiện tại đang đào
	activeBlock        *pb_block.Block // [V5.4] Template khối đầy đủ (Header + Body) để tránh Build lại
	miningStartTime    time.Time
	nodeModeMu         sync.RWMutex         // Mutex bảo vệ nodeMode (Martial Law V1.2.5)
	banMgr             *node_p2p.BanManager // [VANGUARD-DDoS-PROTECTION] Quản lý IP Banned

	// [VANGUARD-HASH-TRACKER] Theo dõi Hashrate độc lập
	lastHashCount uint64
	lastHashTime  time.Time
	mu            sync.RWMutex // Mutex tổng quát cho các trạng thái phối hợp

	// [MINER-STREAM] Kênh nhận kết quả khai thác từ các thợ đào độc lập
	miningResultChan chan *pb_block.MinerMessage

	walletServerEnabled bool
	walletToken         string
}

func (c *CLIApp) EnableWalletServer(enabled bool, token string) {
	c.walletServerEnabled = enabled
	c.walletToken = token
	if enabled {
		log.Printf("[WALLET-SERVER] 🔓 Đã kích hoạt cổng ví. Token bảo mật: %s", token)
	} else {
		log.Printf("[WALLET-SERVER] 🔒 Cổng ví bị vô hiệu hóa.")
	}
}

func (c *CLIApp) SetNodeMode(mode string) {
	c.nodeModeMu.Lock()
	defer c.nodeModeMu.Unlock()
	c.nodeMode = mode
	log.Printf("[NODE-CONTROL] 🔐 Chế độ vận hành đã được đặt thành: %s", mode)
}

func (c *CLIApp) GetNodeMode() string {
	c.nodeModeMu.RLock()
	defer c.nodeModeMu.RUnlock()
	return c.nodeMode
}

func (c *CLIApp) SetSyncMode(mode string) {
	c.syncMode = mode
	log.Printf("[SYNC-CONTROL] ⚡ Chế độ đồng bộ P2P: %s", mode)
}

func (c *CLIApp) GetSyncMode() string {
	if c.syncMode == "" {
		return "snap"
	} // Mặc định: Nhảy cóc
	return c.syncMode
}

func NewCLIApp(dbPath string, minerAddr []byte, minerKey ed25519.PrivateKey, sclPort int) *CLIApp {
	// [VANGUARD-LOGGING] Khởi tạo Hệ thống Log Kiểm toán Bảo mật chuyên dụng
	audit.InitAuditLogger(dbPath)

	bridge := go_bridge.NewBridge(sclPort)

	return &CLIApp{
		minerAddr:        minerAddr,
		minerKey:         minerKey,
		dbPath:           dbPath,
		bridge:           bridge,
		nodeMode:         "verify-only", // [Vanguard] Khởi chạy an toàn ở chế độ xác thực
		banMgr:           node_p2p.NewBanManager(dbPath),
		lastHashTime:     time.Now(),
		miningResultChan: make(chan *pb_block.MinerMessage, 100),
	}
}

func (c *CLIApp) SetMinerAddress(addr []byte, key ed25519.PrivateKey, pin string) {
	c.minerMu.Lock()
	defer c.minerMu.Unlock()
	c.minerAddr = addr
	c.minerKey = key
	log.Printf("[MINER] 🔄 Đã cập nhật ví đào: 0x%s", hex.EncodeToString(addr))

	if key == nil && pin != "" {
		c.loadPrivateKeyByAddress(hex.EncodeToString(addr), pin)
	}
}

func (c *CLIApp) loadPrivateKeyByAddress(addressHex string, pin string) {
	cleanAddr := strings.TrimPrefix(addressHex, "0x")

	walletsDir := c.dbPath + "/wallets"
	files, err := os.ReadDir(walletsDir)
	if err != nil {
		return
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(walletsDir, file.Name()))
		if err != nil {
			continue
		}

		var walletData struct {
			Address string `json:"address"`
		}
		if err := json.Unmarshal(data, &walletData); err != nil {
			continue
		}

		if strings.EqualFold(walletData.Address, cleanAddr) {
			log.Printf("[MINER] 🔑 Đã tìm thấy ví cục bộ khớp với địa chỉ đào. Đang nạp Khóa...")

			if c.rpcSrv != nil && c.rpcSrv.walletMgr != nil {
				seed, err := c.rpcSrv.walletMgr.GetSeed(cleanAddr, pin)
				if err == nil {
					c.minerMu.Lock()
					c.minerKey = ed25519.NewKeyFromSeed(seed[:32])
					c.minerMu.Unlock()
					log.Printf("[MINER] ✅ Đã nạp Khóa riêng tư thành công.")

					if c.rpcSrv != nil {
						c.rpcSrv.SetMinerAddress(c.minerAddr, c.minerKey)
					}
					return
				}
			}
		}
	}
}

func (c *CLIApp) GetMinerAddress() []byte {
	c.minerMu.RLock()
	defer c.minerMu.RUnlock()

	if len(c.minerAddr) != 32 || c.IsZeroAddress(c.minerAddr) {
		return nil
	}

	return c.minerAddr
}

func (c *CLIApp) IsZeroAddress(addr []byte) bool {
	for _, b := range addr {
		if b != 0 {
			return false
		}
	}
	return true
}

func (c *CLIApp) SetRPCServer(srv *RPCServer) {
	c.rpcSrv = srv
}

func (c *CLIApp) IsValidMinerAddress() bool {
	addr := c.GetMinerAddress()
	return addr != nil && len(addr) == 32 && !c.IsZeroAddress(addr)
}

// [V19] Gỡ bỏ EnsureGenesis và recoverStateGap của lớp Go.
// Rust Core giờ đây tự quản lý tính toàn vẹn của Genesis và Lịch sử.

// loadOrCreatePeerPrivKey quản lý danh tính P2P của node
func (c *CLIApp) loadOrCreatePeerPrivKey(path string) (ed25519.PrivateKey, error) {
	keyPath := filepath.Join(path, "node_id.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) == ed25519.PrivateKeySize {
			log.Printf("[P2P-ID] 🔑 Đã khôi phục Bản sắc Node từ: %s", keyPath)
			return ed25519.PrivateKey(data), nil
		}
	}

	// Nếu chưa có hoặc lỗi, sinh mới
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}

	os.MkdirAll(path, 0700)
	if err := os.WriteFile(keyPath, priv, 0600); err != nil {
		log.Printf("[P2P-ID] ⚠️ Cảnh báo: Không thể lưu khóa Node: %v", err)
	} else {
		log.Printf("[P2P-ID] ✨ Đã khởi tạo Bản sắc Node mới tại: %s", keyPath)
	}

	_ = pub
	return priv, nil
}

func (c *CLIApp) StartNode(port int, p2pPort int, peers []string, minerPIN string, seederToken string, seedDomain string, disableMdns bool, writeLog bool, maxTxsPerBlock int) {
	color.Magenta("📡📡📡 YonaCode Go V1.3.0-VANGUARD-ELITE (BUILD 2026-05-13)")
	log.Printf("[VANGUARD] 🚀 Khởi chạy hạm đội Node với phiên bản Thống nhất.")

	// Khởi tạo EventBus PubSub nội bộ cho mô hình Event-Driven
	node_p2p.InitEventBus()

	// [EMERGENCY BOX] Bẫy Panic toàn cục để điều tra nguyên nhân sập nguồn đột ngột
	defer func() {
		if r := recover(); r != nil {
			errLog := fmt.Sprintf("\n[FATAL-CRASH] 💀 NODE ĐÃ SẬP NGUỒN ĐỘT NGỘT!\nLý do: %v\nThời điểm: %s\nStack Trace:\n%s\n", r, time.Now().Format(time.RFC3339), debug.Stack())
			fmt.Print(errLog)
			os.WriteFile("emergency_exit.log", []byte(errLog), 0644)
			log.Printf("[CRITICAL] 💾 Thông tin sự cố đã được lưu vào emergency_exit.log. Tự động thoát...")
			os.Exit(1)
		}
	}()

	absDbPath, _ := filepath.Abs(c.dbPath)

	// [VANGUARD-STRUCTURE-FIX] Chuẩn hóa theo sơ đồ tác chiến 3 tầng
	sclDbPath := filepath.Join(absDbPath, "scl")
	walletsPath := filepath.Join(absDbPath, "wallets")
	snapshotsPath := filepath.Join(absDbPath, "snapshots")

	// Khởi tạo hạ tầng (Tuyệt đối không xóa data cũ)
	os.MkdirAll(sclDbPath, 0700)
	os.MkdirAll(walletsPath, 0700)
	os.MkdirAll(snapshotsPath, 0700)

	log.Printf("[NODE-STORAGE] %s", i18n.T("log_node_storage", absDbPath))
	log.Printf("                ├── scl/        (Database Rust)")
	log.Printf("                ├── wallets/    (Lịch sử ví)")
	log.Printf("                └── snapshots/  (Bản sao trạng thái)")

	// [VANGUARD-LOGGING] Khởi tạo P2P Logger với cờ bảo vệ SSD
	node_p2p.InitP2PLogger(sclDbPath, writeLog)

	// 1. Kích hoạt bộ máy Rust gRPC Server trước tiên
	c.bridge.InitSCL(sclDbPath)



	// [UI-FIX] Cập nhật cao độ từ Rust Core
	rustHeight := c.bridge.GetCurrentVersion()
	log.Printf("[VANGUARD] 🛠️  Hạt nhân SCL Core đang ở cao độ: #%d", rustHeight)

	// [BOOT-AUDIT-SELF-HEALING] Kiểm toán an toàn khi khởi động (Self-Healing)
	if rustHeight > 0 {
		highest := rustHeight
		startAudit := uint64(1)
		if highest > 50 {
			startAudit = highest - 50
		}
		log.Printf("[BOOT-AUDIT] 🔍 Bắt đầu quét kiểm toán 50 khối gần nhất từ #%d đến #%d...", startAudit, highest)
		for h := highest; h >= startAudit; h-- {
			hash := c.bridge.GetBlockHash(h)
			// Phát hiện khuyết dữ liệu Header
			if len(hash) > 0 && c.bridge.GetHeaderRaw(hash) == nil {
				log.Printf("[BOOT-AUDIT] 🚨 Phát hiện khuyết dữ liệu vật lý tại khối #%d! Kích hoạt tự sửa chữa bằng Rollback...", h)
				c.bridge.ForceSetFinalizedHeight(h - 1)
				success := c.bridge.RollbackState(nil, highest, h - 1)
				if !success {
					log.Printf("[BOOT-AUDIT] ⚠️ Rollback tiêu chuẩn bị chặn. Cưỡng chế xóa vật lý...")
					c.bridge.ForceDeleteBlocks(highest, h - 1)
				}
				log.Printf("[BOOT-AUDIT] 🚑 Đã Rollback chuỗi về #%d thành công. Node sẽ tự động P2P tải lại.", h - 1)
				
				// Cập nhật lại cao độ khởi động
				rustHeight = h - 1
				break
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !c.bridge.CheckFFILink() {
		log.Panic("[CRITICAL] ❌ gRPC Linkage thất bại!")
	}

	// [V2.0 SATOSHI-ID] Nạp danh tính P2P bất biến
	rawPrivKey, err := c.loadOrCreatePeerPrivKey(c.dbPath)
	if err != nil {
		go_bridge.FatalExit("[P2P-ID] Lỗi khởi tạo danh tính: %v", err)
	}

	// [FIX] Chuyển đổi ed25519 sang libp2p crypto format
	libp2pPrivKey, _ := libp2p_crypto.UnmarshalEd25519PrivateKey(rawPrivKey)

	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", p2pPort))
	sourceMultiAddr6, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip6/::/tcp/%d", p2pPort))
	udpMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", p2pPort))
	udpMultiAddr6, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip6/::/udp/%d/quic-v1", p2pPort))

	// Cấu hình Connection Manager với Low Water 100 và High Water 150
	cm, err := connmgr.NewConnManager(
		100,
		150,
		connmgr.WithGracePeriod(1*time.Minute),
	)
	if err != nil {
		go_bridge.FatalExit("[P2P] Lỗi khởi tạo Connection Manager: %v", err)
	}

	bwc := metrics.NewBandwidthCounter()
	h, err := libp2p.New(
		libp2p.DefaultTransports,
		libp2p.NATPortMap(),
		// [IPv6-PIONEER] Ưu tiên tuyệt đối địa chỉ IPv6 khi công bố ra mạng lưới
		libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			var ip6, ip4 []multiaddr.Multiaddr
			for _, addr := range addrs {
				if _, err := addr.ValueForProtocol(multiaddr.P_IP6); err == nil {
					ip6 = append(ip6, addr)
				} else {
					ip4 = append(ip4, addr)
				}
			}
			return append(ip6, ip4...) // IPv6 lên đầu danh sách!
		}),
		libp2p.Identity(libp2pPrivKey),
		libp2p.ListenAddrs(sourceMultiAddr, sourceMultiAddr6, udpMultiAddr, udpMultiAddr6),
		libp2p.EnableRelay(),             // Hỗ trợ giao thức Relay (Client)
		libp2p.EnableRelayService(),      // Trở thành trạm Relay để hỗ trợ đục lỗ
		libp2p.EnableHolePunching(),      // Đục lỗ tường lửa DCUtR (P2P Standard)
		libp2p.EnableAutoNATv2(),         // [NAT-AUDIT] Tự xác định Public/Private qua AutoNAT v2
		libp2p.EnableNATService(),        // [NAT-AUDIT] Hỗ trợ peer khác xác định reachability
		libp2p.ConnectionGater(c.banMgr), // [VANGUARD-DDoS-FIX] Chốt chặn IP Banned
		libp2p.ConnectionManager(cm),     // Giới hạn kết nối (Low 100, High 150)
		libp2p.BandwidthReporter(bwc),
	)
	if err != nil {
		go_bridge.FatalExit("[P2P] Không thể khởi tạo Libp2p Host: %v.\nGợi ý: Cổng P2P %d có khả năng đang bị chiếm dụng bởi chương trình khác.", err, p2pPort)
	}

	// [VANGUARD-ID] Công bố địa chỉ P2P đầy đủ để các node khác kết nối
	p2pAddr := fmt.Sprintf("%s/p2p/%s", sourceMultiAddr.String(), h.ID().String())
	log.Printf("[P2P] %s", i18n.T("log_p2p_listening", p2pAddr))
	log.Printf("[SYNC-NETWORK] HostID: %s | Peers In Network: %d", h.ID().String(), len(h.Network().Peers()))

	// [V38.3 NAT-PROFESSIONAL] Khởi động trình quản lý NAT chuyên sâu
	natMgr := node_p2p.NewNATManager(p2pPort)
	natMgr.StartProActiveMapping(ctx)
	// [NAT-AUDIT] Gia hạn UPnP/NAT-PMP mapping mỗi 30 phút để tránh Router xóa mapping
	natMgr.StartPeriodicRenewal(ctx)

	// [NAT-AUDIT] Lắng nghe sự kiện thay đổi Reachability từ AutoNAT
	// Tại sao: Node cần biết mình Public hay Private để thông báo cho peer qua Handshake
	// và để DNS Seeder quyết định có nên đẩy IP của mình lên DNS hay không
	go func() {
		sub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
		if err != nil {
			log.Printf("[NAT-AUDIT] ⚠️ Không thể subscribe AutoNAT event: %v", err)
			return
		}
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-sub.Out():
				if !ok {
					return
				}
				ev := e.(event.EvtLocalReachabilityChanged)
				switch ev.Reachability {
				case network.ReachabilityPublic:
					log.Println("[NAT-AUDIT] ✅ AutoNAT xác nhận: Node này CÔNG KHAI (Public) — Các peer có thể kết nối trực tiếp")
					if c.netMgr != nil {
						c.netMgr.NatStatus = 1 // Public
					}
				case network.ReachabilityPrivate:
					log.Println("[NAT-AUDIT] 🔒 AutoNAT xác nhận: Node này ĐẰNG SAU TƯỜNG LỬA (Private) — Cần Relay/HolePunch")
					if c.netMgr != nil {
						c.netMgr.NatStatus = 2 // Private
					}
				default:
					log.Println("[NAT-AUDIT] ❓ AutoNAT: Trạng thái NAT chưa xác định (Unknown)")
					if c.netMgr != nil {
						c.netMgr.NatStatus = 0 // Unknown
					}
				}
			}
		}
	}()

	// [V38.4 HOLE-PUNCH-AUDIT] Theo dõi đục lỗ tường lửa chuẩn P2P (DCUtR)
	go func() {
		sub, err := h.EventBus().Subscribe(new(holepunch.EndHolePunchEvt))
		if err != nil {
			return
		}
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-sub.Out():
				if !ok {
					return
				}
				ev := e.(holepunch.EndHolePunchEvt)
				if ev.Success {
					log.Printf("[P2P-PUNCH] 🚀 ĐÃ ĐỤC THÔNG TƯỜNG LỬA (Chuẩn P2P-DCUtR) THÀNH CÔNG! (Thời gian: %v)", ev.EllapsedTime)
				}
			}
		}
	}()

	// [V38.5 DHT-AUDIT] Giám sát khám phá Peer hàng xóm (P2P-Standard Discovery)
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peers := h.Network().Peers()
				log.Printf("[P2P-AUDIT] 🛰️ Trạng thái mạng: Đang kết nối với %d Peer (Đã đục lỗ hoặc qua Relay)", len(peers))
			}
		}
	}()

	// [V38.1 NAT-AUDIT] Giám sát bổ trợ (Optional)
	go func() {
		time.Sleep(30 * time.Second)
		addrs := h.Addrs()
		for _, addr := range addrs {
			if manet.IsPublicAddr(addr) {
				log.Printf("[NAT-AUDIT] %s", i18n.T("log_nat_audit_public_detected", addr))
			}
		}
	}()

	// [SATOSHI-RESTORE] Khôi phục GossipSub để phục vụ giao thức INV
	// [SECURITY-LIMIT] Thiết lập giới hạn kích thước tin nhắn GossipSub toàn cục là 45MB.
	// Tại sao thiết kế như vậy: Đảm bảo các khối lớn (Full Block) lên tới 35MB-45MB trong trường hợp dự phòng
	// phát sóng trực tiếp qua GossipSub không bao giờ bị router từ chối ở tầng mạng.
	scoreParams := &pubsub.PeerScoreParams{
		AppSpecificScore: func(p peer.ID) float64 { return 0 },
		IPColocationFactorWeight:    0,
		BehaviourPenaltyWeight:      -10, // Điểm trừ nhẹ cho lỗi timeout/chậm phản hồi
		BehaviourPenaltyDecay:       0.9,
		DecayInterval:               time.Minute,
		DecayToZero:                 0.01,
		RetainScore:                 time.Hour,
	}

	scoreThresholds := &pubsub.PeerScoreThresholds{
		GossipThreshold:             -100, // Điểm dưới -100 sẽ ngừng gossip
		PublishThreshold:            -200, // Điểm dưới -200 sẽ ngừng publish block
		GraylistThreshold:           -300, // Chỉ ngắt kết nối vật lý khi điểm tụt thảm hại dưới -300
		AcceptPXThreshold:           0,
		OpportunisticGraftThreshold: 0,
	}

	// [SECURITY-LIMIT] Thiết lập giới hạn kích thước tin nhắn GossipSub toàn cục là 36MB.
	// Tại sao thiết kế như vậy: Khối thô tối đa là 35MB. Cấu hình 36MB dư sức chứa khối tối đa cùng với
	// protobuf overhead, đồng thời giảm 9MB RAM đệm tối đa để ngăn chặn tấn công OOM DoS tốt hơn.
	ps, err := pubsub.NewGossipSub(
		ctx, 
		h, 
		pubsub.WithMaxMessageSize(36*1024*1024),
		pubsub.WithPeerScore(scoreParams, scoreThresholds),
	)
	if err != nil {
		go_bridge.FatalExit("[P2P] Không thể khởi tạo GossipSub với Peer Scoring: %v", err)
	}

	// [V2.0 SATOSHI-DHT] Khởi tạo Kademlia DHT để tự động tìm Peer (Bỏ qua trong Strict Isolation Mode 3)
	var kdht *kaddht.IpfsDHT
	if c.banMgr.GetIsolationMode() != 3 {
		var errInit error
		kdht, errInit = node_p2p.InitDHT(ctx, h)
		if errInit != nil {
			log.Printf("[P2P-DHT] ⚠️ Cảnh báo: Không thể khởi tạo DHT: %v", errInit)
		}
	} else {
		log.Println("[P2P-DHT] 🔒 Chế độ Cách ly Tuyệt đối (Strict Isolation): Vô hiệu hóa DHT hoàn toàn.")
	}

	c.mempool = node_p2p.NewMempool(c.bridge, 250)
	// Tại sao thiết kế như vậy: Đảm bảo nếu cờ CLI truyền giá trị không hợp lệ (nhỏ hơn hoặc bằng 0),
	// hệ thống tự động fallback về giới hạn an toàn mặc định là 1000 giao dịch/khối để tránh thợ đào
	// tạo ra khối rỗng (0 giao dịch) hoặc khối quá tải.
	if maxTxsPerBlock <= 0 {
		c.mempool.MaxTxsPerBlock = 1000
	} else {
		c.mempool.MaxTxsPerBlock = maxTxsPerBlock
	}
	c.mempool.StartEvictionWorker(ctx)
	c.netMgr = node_p2p.NewNetworkManager(ctx, h, ps, nil, c.bridge, c.mempool, c.banMgr)
	c.netMgr.Bwc = bwc
	c.netMgr.DbPath = c.dbPath // [VANGUARD-FILE-SYNC] Truyền đường dẫn dữ liệu để định vị file snapshot
	c.snapshotMgr = node_p2p.NewSnapshotManager(c.dbPath, c.bridge)

	// [VANGUARD-AUTOMATION] Kết nối dây thần kinh tự động hóa: Sync SMT + Dashboard UI
	c.netMgr.OnBlockCommitted = func(height uint64) {
		// 1. Đồng bộ Snapshot cho Rust Core
		c.snapshotMgr.OnBlockCommitted(height)

		// 2. [VANGUARD-FIX] Cập nhật Dashboard thời gian thực
		if c.rpcSrv != nil {
			c.rpcSrv.SyncBlockToTracker(height)
		}
		// [VANGUARD-FIX-STALL] KHÔNG gọi RefreshMiningTask() ở đây!
		// Lý do: Main mining loop (minerLoop) đã tự xử lý chuyển khối rồi.
		// Gọi RefreshMiningTask tại đây tạo Session ID xung đột → main loop
		// reject kết quả do SID không khớp → thợ đào đứng hình.
	}

	c.netMgr.SyncMode = c.GetSyncMode() // [V1.1.4.2] Truyền chế độ đồng bộ xuống tầng P2P
	c.syncEngine = node_p2p.NewSyncEngine(ctx, c.netMgr, c.mempool)

	// [V2.2] Lớp 2: Khám phá Peer (Discovery Services)
	discoveryService := node_p2p.NewDiscoveryService(h, kdht, seedDomain, seederToken, c.dbPath, c.banMgr)
	c.netMgr.Discovery = discoveryService
	c.netMgr.SyncEngine = c.syncEngine
	c.netMgr.Bootstrap()
	c.netMgr.StartBlockInbox()

	// [V2.1 SATOSHI-PEX] Đăng ký bộ xử lý Trao đổi Peer
	h.SetStreamHandler(protocol.ID(node_p2p.PexProtocol), c.netMgr.HandlePeerExchange)

	// [V2.1 SATOSHI-PEX] Ticker chia sẻ địa chỉ định kỳ (1 phút/lần)
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.netMgr.GossipPeers()
			}
		}
	}()

	// [V2.2] Kích hoạt mDNS để phát hiện Peer trong mạng nội bộ (LAN)
	if !c.disableMdns {
		if err := discoveryService.InitMdns(); err != nil {
			log.Printf("[P2P-MDNS] ⚠️ Không thể khởi tạo mDNS: %v", err)
		}
	}

	go discoveryService.DiscoveryLoop()

	// [V2.0 FLEXIBLE-PEERS] Hỗ trợ cả IP:Port và Multiaddr
	for _, p := range peers {
		if p == "" {
			continue
		}

		var info *peer.AddrInfo
		if !strings.HasPrefix(p, "/") {
			// Giả định định dạng IP:Port (Ví dụ: 127.0.0.1:9000)
			parts := strings.Split(p, ":")
			if len(parts) == 2 {
				maddrStr := fmt.Sprintf("/ip4/%s/tcp/%s", parts[0], parts[1])
				maddr, _ := multiaddr.NewMultiaddr(maddrStr)
				info = &peer.AddrInfo{ID: "", Addrs: []multiaddr.Multiaddr{maddr}}
				log.Printf("[P2P-BOOT] 🌐 Chuyển đổi IP:Port thành Multiaddr: %s", maddrStr)
			}
		} else {
			// Định dạng Multiaddr chuẩn
			maddr, err := multiaddr.NewMultiaddr(p)
			if err == nil {
				info, _ = peer.AddrInfoFromP2pAddr(maddr)
			}
		}

		if info != nil {
			log.Printf("[P2P-BOOT] 🔗 Đang yêu cầu kết nối tới Peer khởi tạo: %s", p)
			go func(pi peer.AddrInfo) {
				// [AUDIT-FIX M-4] Bỏ qua kết nối khi Peer ID rỗng
				// Tại sao: Gọi Peerstore.AddAddrs() và Connect() với ID rỗng gây hành vi không xác định
				// và có thể panic. Các entry IP:Port thuần cần DHT để phân giải PeerID trước.
				if pi.ID == "" {
					log.Printf("[P2P-DIAL] ⚠️ Bỏ qua Peer không có ID (IP-only: %v). Chờ DHT phân giải.", pi.Addrs)
					return
				}
				if err := h.Connect(ctx, pi); err != nil {
					log.Printf("[P2P-BOOT] ❌ Kết nối tới %s thất bại: %v", p, err)
				} else {
					log.Printf("[P2P-BOOT] ✅ Kết nối thành công tới Peer: %s", p)
				}
			}(*info)
		}
	}

	// [V2.5 AUTO-RECONNECT-AUDIT] Goroutine tự động thăm dò và kết nối lại các bootstrap peers
	// Nhằm loại bỏ rò rỉ đồng thuận, duy trì mạng lưới ổn định khi chịu stress test cực đại.
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentPeers := h.Network().Peers()
				if len(currentPeers) < 100 {
					if len(currentPeers) < 3 {
						log.Printf("[P2P-RECONNECT] 🚑 Phát hiện số peer quá thấp (%d/100). Bắt đầu kích hoạt cơ chế tự phục hồi kết nối...", len(currentPeers))
					}
					
					// [TỐI ƯU KẾT NỐI] Gộp IP cứng VPS và danh sách peers CLI vào chung mảng cứu hộ
					var allRescuePeers []string
					allRescuePeers = append(allRescuePeers, peers...) // Các peer chỉ định qua CLI
					allRescuePeers = append(allRescuePeers, node_p2p.BootstrapPeers...) // Các IP cứng VPS
					
					for _, p := range allRescuePeers {
						if p == "" {
							continue
						}
						var info *peer.AddrInfo
						if !strings.HasPrefix(p, "/") {
							parts := strings.Split(p, ":")
							if len(parts) == 2 {
								maddrStr := fmt.Sprintf("/ip4/%s/tcp/%s", parts[0], parts[1])
								maddr, _ := multiaddr.NewMultiaddr(maddrStr)
								info = &peer.AddrInfo{ID: "", Addrs: []multiaddr.Multiaddr{maddr}}
							}
						} else {
							maddr, err := multiaddr.NewMultiaddr(p)
							if err == nil {
								info, _ = peer.AddrInfoFromP2pAddr(maddr)
							}
						}
						if info != nil {
							if h.Network().Connectedness(info.ID) != network.Connected {
								go func(pi peer.AddrInfo) {
									log.Printf("[P2P-RECONNECT] 🔗 Thử kết nối lại tới bootstrap peer: %s", p)
									if pi.ID == "" {
										for _, addr := range pi.Addrs {
											h.Peerstore().AddAddrs(pi.ID, []multiaddr.Multiaddr{addr}, time.Hour)
											_ = h.Connect(ctx, pi)
										}
									} else {
										_ = h.Connect(ctx, pi)
									}
								}(*info)
							}
						}
					}
				}
			}
		}
	}()

	wm := internal.NewWalletManager(c.dbPath + "/wallets")
	c.rpcSrv = NewRPCServer(c.bridge, c.netMgr, port, wm, c.minerAddr, c.minerKey, c)

	if c.nodeMode == "full-mining" {
		// [SECURITY LOCK] Ngăn chặn bypass qua cờ CLI nếu chưa có ví
		if !c.IsValidMinerAddress() {
			log.Printf("[SECURITY] 🛑 Yêu cầu đăng nhập bằng cách ấn vào khôi phục để xử lý hệ thống (Cần ví hợp lệ để ký giao dịch thợ đào)")
			c.nodeMode = "verify-only"
			c.bridge.SetMiningPause(true)
		} else {
			c.bridge.SetMiningPause(false)
		}
	} else {
		c.bridge.SetMiningPause(true)
	}

	go c.rpcSrv.Start()

	log.Printf("[VANGUARD] ✅ Node đã sẵn sàng tại RPC Port %d | P2P %d", port, p2pPort)
	log.Printf("[P2P] %s", i18n.T("log_p2p_listening", c.netMgr.GetAddress()))

	go c.minerLoop(ctx)

	select {}
}



func (app *CLIApp) minerLoop(ctx context.Context) {
	log.Printf("[MINER] 🛸 Vòng lặp khai thác L1 Professional chính thức khởi chạy.")
	log.Printf("[MINER] ⚙️  Thợ đào đang ở trạng thái chờ lệnh theo Hiến pháp...")
	consecutiveExecutionFails := 0 // Chỉ đếm lỗi thực thi cục bộ (Execution Mismatch)

	for {
		// log.Printf("[MINER-LOOP-TRACE] 🔄 Vòng lặp đang chạy... Chế độ: %s", app.GetNodeMode())
		select {
		case <-ctx.Done():
			return
		default:
			// [VANGUARD-GUARD] Chốt chặn an toàn: Đợi 75 giây Warm-up để ổn định mạng P2P trước khi đào.
			// [VANGUARD-AUTONOMY-FIX] BỎ QUA Warm-up nếu đang ở chế độ Tự trị (failures >= 3)
			gracePeriod := 75 * time.Second

			app.mu.RLock()
			failures := 0
			if app.syncEngine != nil {
				failures = app.syncEngine.GetSyncFailures()
			}
			app.mu.RUnlock()

			if time.Since(app.rpcSrv.launchTime) < gracePeriod && failures < 3 {
				if time.Now().Second()%10 == 0 {
					log.Printf("[MINER-WARMUP] ⏳ Đang chờ mạng lưới ổn định... Còn %d giây.", int((gracePeriod - time.Since(app.rpcSrv.launchTime)).Seconds()))
				}
				time.Sleep(1 * time.Second)
				continue
			}

			// [VANGUARD-DISCIPLINE] Thợ đào CHỈ được phép chạy khi chế độ là "full-mining"
			if app.GetNodeMode() != "full-mining" {
				if time.Now().Second()%30 == 0 {
					log.Printf("[MINER-IDLE] 💤 Chế độ hiện tại: %s. Thợ đào đang nghỉ ngơi.", app.GetNodeMode())
				}
				time.Sleep(2 * time.Second)
				continue
			}

			// [VANGUARD-REPORT] Heartbeat Log mỗi 10 giây để giám sát sức khỏe Node
			if time.Now().Second()%10 == 0 {
				h, target, state := app.syncEngine.GetSyncProgress()
				peers := len(app.netMgr.Host.Network().Peers())

				// [FIX-V5.5] Lấy hashrate thực tế (MH/s) được cache bởi RPC Server (Single Source of Truth)
				// Để tránh tranh chấp reset counter và tính toán delta sai lệch.
				hashrateVal := app.rpcSrv.GetCurrentHashrate()
				hashrateMH := float64(hashrateVal) / 1e6

				log.Printf("[STATUS] 🚀 Node Online | Cao độ: #%d/%d | Peers: %d | Hashrate: %.2f MH/s | Trạng thái: %s",
					h, target, peers, hashrateMH, state)
			}

			// [VANGUARD-DISCIPLINE] Radar Scan Guard
			// Miner không tự ý dừng đào chỉ vì thấy số chiều cao lớn hơn.
			// Miner chỉ dừng đào khi chưa hoàn thành đồng bộ ban đầu (Initial Sync) 
			// HOẶC khi SyncEngine đang thực hiện Snapshot Sync nhảy vọt (Bootstrapping).
			_, _, syncState := app.syncEngine.GetSyncProgress()
			// Tại sao: Nếu trạng thái đã là Synced, ta tự động đánh dấu hoàn thành đồng bộ khởi đầu để mở khóa cho miner,
			// tránh việc miner bị kẹt vĩnh viễn ở trạng thái chờ [MINER-WAIT] do điều kiện kiểm tra lặp chéo.
			if syncState == "Synced" && !app.initialSyncComplete {
				app.initialSyncComplete = true
				color.Green("[STAGE-2] 🚀 ĐỒNG BỘ KHỞI ĐẦU HOÀN TẤT. Hệ thống chuyển sang chế độ vận hành Live.")
			}

			if !app.initialSyncComplete || syncState == "Bootstrapping" {
				if time.Now().Second()%10 == 0 {
					h, target, _ := app.syncEngine.GetSyncProgress()
					log.Printf("[MINER-WARN] ⚠️ Đang đào trong khi hệ thống chưa đồng bộ xong! #%d/%d (Trạng thái: %s)", h, target, syncState)
				}
			}

			log.Printf("[MINER-START] %s", i18n.T("log_miner_preparing", app.bridge.GetCurrentVersion()+1))

			// Chuyển sang giai đoạn 2 nếu lần đầu tiên đạt trạng thái Synced
			if !app.initialSyncComplete {
				app.initialSyncComplete = true
				color.Green("[STAGE-2] 🚀 ĐỒNG BỘ KHỞI ĐẦU HOÀN TẤT. Hệ thống chuyển sang chế độ vận hành Live.")
			}

			// [VANGUARD-MINING-TIP] Giờ đây h luôn là đỉnh cao nhất được mạng lưới chấp nhận
			h := app.bridge.GetCurrentVersion()

			log.Printf("[-TRACE] Miner Loop Check: Height=%d | IsSynced=%v | Mode=%s", h, app.syncEngine.IsSynced(), app.GetNodeMode())
			if !app.IsValidMinerAddress() {
				log.Printf("[MINER] 🛑 Yêu cầu đăng nhập bằng cách ấn vào khôi phục để xử lý hệ thống (Thiếu chữ ký Coinbase hợp lệ)")
				time.Sleep(10 * time.Second)
				continue
			}
			// [GENESIS-FIX] Chặn tuyệt đối việc thợ đào tự ý đào khối Genesis (#0) khi DB trống.
			// Khối Genesis phải được đồng bộ từ mạng chính hoặc nạp từ ledger cục bộ.
			var nextHeight uint64
			genHash := app.bridge.GetBlockHash(0)
			if h == 0 && len(genHash) == 0 {
				log.Printf("[MINER-GENESIS-BLOCKED] 🛑 Chưa có khối Genesis trong DB. Chặn đào khối #0 để tránh tạo fork rác. Vui lòng chờ đồng bộ từ mạng chính.")
				time.Sleep(5 * time.Second)
				continue
			} else {
				nextHeight = h + 1
			}

			app.activeMiningMu.Lock()
			app.miningStartTime = time.Now()
			app.activeMiningMu.Unlock()

			// [VANGUARD-BLOCKSIZE] Sử dụng kích thước khối mặc định 5MB (thay vì 35MB tối đa) để đảm bảo tính ổn định
			// của các node mạng trong giai đoạn đầu vận hành chạy nội bộ/thử nghiệm.
			pendingTxs := app.mempool.GetPartitionedTransactions(node_p2p.DefaultBlockMaxSize)
			taskBytes, sid := app.buildAndSubmitTemplate(nextHeight, pendingTxs)
			if taskBytes == nil {
				log.Printf("[MINER-WARN] ⚠️ Không thể xây dựng Template cho khối #%d. (Có thể do thiếu cha hoặc Mempool trống). Thử lại sau 1s...", nextHeight)
				time.Sleep(1 * time.Second)
				continue
			}

			// [V5.2-STABILITY] CHỐT CHẶN BẤT BIẾN: Capture toàn bộ thông số template hiện tại
			app.activeMiningMu.Lock()
			capturedHeight := nextHeight
			app.activeMiningMu.Unlock()

			// Dọn dẹp hàng đợi kết quả cũ trước khi đợi
			for len(app.miningResultChan) > 0 {
				<-app.miningResultChan
			}

			// Phát nhiệm vụ đào mới cho toàn bộ các thợ đào kết nối qua gRPC Stream
			if app.rpcSrv != nil {
				app.rpcSrv.BroadcastMiningTask(taskBytes, sid, app.activeDifficulty)
			}

			var result pb_consensus.MiningResult
			found := false
			log.Printf("[MINER-POLL] 🕵️ Bắt đầu theo dõi kết quả đào cho khối #%d từ các thợ đào độc lập...", capturedHeight)
			for i := 0; i < 600; i++ {
				if app.GetNodeMode() != "full-mining" {
					log.Printf("[MINER-POLL] 🛑 Dừng đào do thay đổi Node Mode.")
					break
				}

				select {
				case msg := <-app.miningResultChan:
					app.activeMiningMu.Lock()
					currentActiveSid := app.activeSessionId
					app.activeMiningMu.Unlock()

					if msg.SessionId == sid {
						log.Printf("[MINER-POLL] ✨ TÌM THẤY KẾT QUẢ TỪ MINER ĐỘC LẬP (SID: %d)!", msg.SessionId)
						result.Nonce = msg.FoundNonce
						result.BlockHash = msg.BlockHash
						result.Success = true
						result.SessionId = msg.SessionId
						found = true
					} else if msg.SessionId == currentActiveSid && currentActiveSid != 0 {
						log.Printf("[MINER-POLL] ✨ TÌM THẤY KẾT QUẢ TỪ PHIÊN ĐÃ CẬP NHẬT (SID: %d)!", msg.SessionId)
						result.Nonce = msg.FoundNonce
						result.BlockHash = msg.BlockHash
						result.Success = true
						result.SessionId = msg.SessionId
						found = true
						sid = currentActiveSid
					} else {
						log.Printf("[MINER-POLL] ⚠️ Nhận kết quả từ phiên cũ (SID: %d, Cần: %d hoặc %d). Bỏ qua.", msg.SessionId, sid, currentActiveSid)
						continue
					}
				case <-time.After(1 * time.Second):
					// Hết thời gian chờ 1s, tiếp tục loop kiểm tra chiều cao mạng
				}

				if found {
					break
				}

				// Kiểm tra xem cao độ mạng đã thay đổi chưa
				currentH := app.bridge.GetCurrentVersion()
				if capturedHeight == 0 {
					genHash := app.bridge.GetBlockHash(0)
					if len(genHash) > 0 {
						isGhost := true
						for _, b := range genHash {
							if b != 0 {
								isGhost = false
								break
							}
						}
						if !isGhost {
							log.Printf("[MINER-SYNC] ⏩ Phát hiện khối Genesis thật từ mạng. Dừng đào để đồng bộ.")
							break
						}
					}
				} else {
					if currentH >= capturedHeight {
						log.Printf("[MINER-POLL] ⏩ Mạng đã có khối mới (#%d). Bỏ qua Template cũ.", currentH)
						break
					}
				}
			}

			if !found {
				log.Printf("[MINER-POLL] ⌛ Hết thời gian đào 600s hoặc bị ngắt quãng. Đang tuần tra lại Mempool...")
				continue
			}

			// Kiểm tra lại tính hợp lệ của Height trước khi submit
			if app.activeTargetHeight != capturedHeight && capturedHeight != 0 {
				log.Printf("[MINER-L1] ⚠️ Bỏ qua kết quả cũ cho khối #%d (Hiện tại đang đào #%d)", capturedHeight, app.activeTargetHeight)
				continue
			}

			log.Printf("[MINER-L1] %s", i18n.T("log_miner_block_found", capturedHeight, result.Nonce))

			// [V5.4] CHỐT CHẶN CUỐI CÙNG: Tuyệt đối không gọi BuildVanguardBlockTemplate lại!
			// Tại sao: Việc gọi lại có thể sinh ra TxRoot khác nếu logic Rust không deterministic.
			// Giải pháp: Sử dụng đúng bản Template đã dùng để tạo MiningTask (Immutable Template).
			app.activeMiningMu.Lock()
			if app.activeBlock == nil {
				app.activeMiningMu.Unlock()
				log.Printf("[MINER-ERROR] ❌ activeBlock bị mất! Không thể nộp khối #%d.", capturedHeight)
				continue
			}

			// Deep clone để tránh race condition nếu template bị xoá/thay đổi
			minedBlockBytes, _ := proto.Marshal(app.activeBlock)
			app.activeMiningMu.Unlock()

			var minedBlock pb_block.Block
			proto.Unmarshal(minedBlockBytes, &minedBlock)

			minedBlock.Header.Nonce = result.Nonce

			// [VANGUARD-CONSENSUS] Đóng gói lại khối đã có Nonce và gửi lên Consensus Engine
			finalBlockRaw, _ := proto.Marshal(&minedBlock)

			log.Printf("[MINER] ⛓️ Đang gửi khối mới #%d lên Consensus Engine (Trọng số: %x)...", nextHeight, minedBlock.Header.AbsoluteWeight)
			resp, err := app.bridge.ProcessChain([][]byte{finalBlockRaw})
			if err != nil {
				log.Printf("[GOSSIP-ERROR] ❌ Lỗi gọi gRPC ProcessChain: %v", err)
				continue
			}

			shouldBroadcast := false
			if resp.Status == 1 { // REORG_SUCCESS
				// ==========================================================
				// [VANGUARD-SELF-AUDIT] KIỂM TOÁN NỘI BỘ TRƯỚC KHI PHÁT SÓNG
				// ==========================================================
				templateStateRoot := minedBlock.Header.StateRoot.Value
				actualStateRoot := app.bridge.GetStateRoot()

				if !bytes.Equal(templateStateRoot, actualStateRoot) {
					// PHÂN LOẠI LỖI (Tránh False Positive do Race Condition)
					currentRealHeight := app.bridge.GetCurrentVersion()
					
					if currentRealHeight > capturedHeight {
						// 1. SYNC MISMATCH (Mạng lưới đi nhanh hơn)
						// P2P Gossip vừa nạp một khối mới từ mạng vào DB ngay lúc ta đang đào.
						// Đây là hành vi TỰ NHIÊN. Môi trường KHÔNG HỎNG.
						log.Printf("⚠️ [MINER-RACE] Mạng lưới vừa nạp khối mới (#%d). Template khối #%d của ta đã lỗi thời. Hủy phát sóng và đào lại.", currentRealHeight, capturedHeight)
						
						consecutiveExecutionFails = 0 // Reset đếm lỗi
						continue // Bỏ qua và lấy template mới đào tiếp
					} else {
						// 2. EXECUTION MISMATCH (Lỗi môi trường DB/RAM dơ thực tế)
						consecutiveExecutionFails++
						log.Printf("🚨 [SELF-AUDIT] Lệch pha StateRoot tại cùng cao độ #%d (Lần %d)!", capturedHeight, consecutiveExecutionFails)

						if consecutiveExecutionFails >= 3 {
							log.Printf("💀 [FATAL-ENVIRONMENT] 3 Lần lệch StateRoot liên tiếp! Kích hoạt chế độ Cứu Thương.")
							
							// Ngắt đào để bảo vệ CPU
							app.bridge.SetMiningPause(true)
							app.nodeMode = "verify-only"

							// Lùi 1 bước an toàn nhờ Versioned State của JMT
							if currentRealHeight > 0 {
								app.bridge.RollbackState(nil, currentRealHeight, currentRealHeight-1)
							}

							// SELECTIVE PURGE + REVALIDATION MEMPOOL
							if app.mempool != nil {
								log.Printf("🧹 [MEMPOOL-REVALIDATE] Đang tái thẩm định Mempool thay vì xóa sạch...")
								senders := app.mempool.GetSenders()
								var batchSenders []string
								var batchNonces []uint64
								for _, senderHex := range senders {
									addrBytes, _ := hex.DecodeString(senderHex)
									dbNonce := app.bridge.GetNonce(nil, addrBytes)
									batchSenders = append(batchSenders, senderHex)
									batchNonces = append(batchNonces, dbNonce)
									app.mempool.ClearProjectedNonce(senderHex)
								}
								if len(batchSenders) > 0 {
									app.mempool.RemoveStaleNonceTxsBatch(batchSenders, batchNonces)
								}
							}

							// Nhường quyền cho SyncEngine tải lại khối chuẩn từ mạng
							if app.netMgr != nil && app.netMgr.SyncEngine != nil {
								if se, ok := app.netMgr.SyncEngine.(*node_p2p.SyncEngine); ok {
									se.TriggerSync()
								}
							}

							consecutiveExecutionFails = 0
							time.Sleep(5 * time.Second)
							continue
						}
					}
					continue // Thử lại vòng đào nếu chưa tới 3 lần
				}
				
				// NẾU THÀNH CÔNG: Reset biến đếm
				consecutiveExecutionFails = 0
				// ==========================================================

				log.Printf("[MINER-SUCCESS] ✅ Khối đào #%d đã được Rust SCL chấp nhận. Đỉnh mới: #%d", nextHeight, resp.NewHeight)

				// [VANGUARD-REORG] Thu hồi các giao dịch mồ côi về Mempool
				if len(resp.OrphanedTxsRaw) > 0 {
					log.Printf("[REORG-MEMPOOL] ♻️ Rust trả về %d giao dịch mồ côi. Đang khôi phục...", len(resp.OrphanedTxsRaw))
					for _, txRaw := range resp.OrphanedTxsRaw {
						app.mempool.PushToTxBus(txRaw, false)
					}
				}

				// [V1.60-RACE-FIX] DỌN DẸP MEMPOOL ĐỒNG BỘ TRƯỚC KHI MINING LOOP LẤY TX MỚI
				// Tại sao: Nếu chạy async (goroutine), mining loop sẽ lấy lại TX stale
				// trước khi cleanup kịp xóa → NONCE MISMATCH liên tục → block rỗng.
				// Giải pháp: Xóa TX stale cho tất cả sender có mặt trong block VỪA ĐÀO,
				// ĐỒNG BỘ (blocking) trước khi mining loop tiếp tục.
				if minedBlock.Body != nil && app.mempool != nil {
					sendersSeen := make(map[string]bool)
					for _, tx := range minedBlock.Body.Transactions {
						if tx.Sender != nil && tx.Amount > 0 {
							senderHex := hex.EncodeToString(tx.Sender.Value)
							sendersSeen[senderHex] = true
						}
					}
					var batchSenders []string
					var batchNonces []uint64
					for senderHex := range sendersSeen {
						senderAddr, err := hex.DecodeString(senderHex)
						if err == nil {
							dbNonce := app.bridge.GetNonce(nil, senderAddr)
							batchSenders = append(batchSenders, senderHex)
							batchNonces = append(batchNonces, dbNonce)
						}
					}
					if len(batchSenders) > 0 {
						app.mempool.RemoveStaleNonceTxsBatch(batchSenders, batchNonces)
					}
				}

				if app.netMgr.OnBlockCommitted != nil {
					go app.netMgr.OnBlockCommitted(resp.NewHeight)
				}
				shouldBroadcast = true
			} else if resp.Status == 0 {
				log.Printf("[MINER-SIDE] 🌾 Khối #%d thuộc một nhánh phụ hoặc nhẹ hơn (Cần kiểm tra lại Diff).", nextHeight)
				shouldBroadcast = true
			} else if resp.Status == 3 {
				log.Printf("[MINER-ORPHAN] ❓ Khối #%d bị Rust coi là mồ côi. Có lỗi đồng bộ cực bộ?", nextHeight)
			} else {
				log.Printf("[MINER-REJECT] ❌ Rust Core từ chối khối vừa đào #%d: %s (Status: %d)", nextHeight, resp.ErrorMsg, resp.Status)
			}

			if shouldBroadcast {
				// [V2.0 SATOSHI-PUSH] Phát thông báo INV trước, sau đó mới phát Block đầy đủ
				minedHeaderRaw, _ := proto.Marshal(minedBlock.Header)
				blockHash := app.bridge.GetCanonicalBlockHeaderHash(minedHeaderRaw, capturedHeight)

				app.netMgr.BroadcastInventory(capturedHeight, blockHash)
				app.netMgr.BroadcastBlock(minedBlock.Header, minedBlock.Body)

				if app.netMgr.OnBlockCommitted != nil {
					go app.netMgr.OnBlockCommitted(capturedHeight)
				}
			}
		}
	}
}

func (c *CLIApp) getMiningHistory() ([]uint64, [][]byte, error) {
	highest := c.bridge.GetCurrentVersion()
	n := 120

	timestamps := make([]uint64, 0, n+1)
	difficulties := make([][]byte, 0, n)

	startH := uint64(0)
	if highest >= uint64(n) {
		startH = highest - uint64(n)
	} else {
		startH = 0
	}

	for h := startH; h <= highest; h++ {
		// [V19] Cần lấy Header từ Rust Core
		blockRaw := c.bridge.GetBlock(h)
		if blockRaw == nil {
			if highest == 0 && h == 0 {
				continue
			}
			return nil, nil, fmt.Errorf("không thể đọc block #%d từ database (SCL server bận hoặc lỗi gRPC)", h)
		}
		var block pb_block.Block
		err := proto.Unmarshal(blockRaw, &block)
		if err != nil {
			return nil, nil, fmt.Errorf("không thể giải mã block #%d: %v", h, err)
		}
		if block.Header == nil {
			return nil, nil, fmt.Errorf("block #%d thiếu Header", h)
		}
		timestamps = append(timestamps, block.Header.Timestamp)
		// [FIX V1.7.1] Sửa h > startH thành h >= startH để lấy ĐỦ n+1 elements difficulties
		// Đảm bảo toán học LWMA đồng nhất 100% với Rust Core khi tính AbsoluteWeight
		if h >= startH || (startH == 0 && len(difficulties) < n) {
			difficulties = append(difficulties, block.Header.Difficulty)
		}
	}

	if len(difficulties) == 0 {
		// [VANGUARD-FIX] Không tự ý gán 1.2B, phải hỏi Rust Core về độ khó tối thiểu
		diffBytes := c.bridge.CalculateNextDifficultyV2(nil, nil, uint64(time.Now().Unix()), 0)
		difficulties = append(difficulties, diffBytes)
	}
	if len(timestamps) == 0 {
		timestamps = append(timestamps, uint64(time.Now().Unix()))
	}

	return timestamps, difficulties, nil
}

func (c *CLIApp) buildAndSubmitTemplate(nextHeight uint64, txs []*pb_block.Transaction) ([]byte, uint64) {
	c.minerResetMu.Lock()
	c.lastMinerReset = time.Now()
	c.minerResetMu.Unlock()

	currentMinerAddr := c.GetMinerAddress()

	// 1. Lấy thông tin block cha từ Rust
	// [VANGUARD-HOTFIX] Ngăn chặn Underflow (0 - 1) khi đào khối Genesis
	var parentHash []byte
	if nextHeight > 0 {
		parentHash = c.bridge.GetBlockHash(nextHeight - 1)
	}

	// Fallback an toàn nếu không có Hash cha (Dành cho Genesis)
	if parentHash == nil || len(parentHash) != 32 {
		parentHash = make([]byte, 32)
	}

	// 2. Chuyển danh sách giao dịch sang bytes để gửi qua gRPC
	txsBytes := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		d, _ := proto.Marshal(tx)
		txsBytes = append(txsBytes, d)
	}

	// 3. Tính toán độ khó cho khối tiếp theo
	hTimestamps, hDifficulties, err := c.getMiningHistory()
	if err != nil {
		log.Printf("[MINER-WARN] ⚠️ Không thể lấy lịch sử khai thác: %v. Hủy bỏ và thử lại sau...", err)
		return nil, 0
	}
	nowTs := uint64(time.Now().Unix())
	difficulty := c.bridge.CalculateNextDifficultyV2(hTimestamps, hDifficulties, nowTs, nextHeight)

	// 4. Yêu cầu RUST CORE xây dựng Block Template chuẩn (Bao gồm Coinbase & Dry-run)
	log.Printf("[VANGUARD] 🏗️ Đang yêu cầu Rust Core xây dựng Template cho khối #%d...", nextHeight)

	var blockRaw []byte
	var failIdx int32
	var errMsg string

	// Gọi gRPC 1 lần duy nhất để tạo Template. Rust Core tự động lọc bỏ các giao dịch lỗi
	// trong dry-run một lượt duy nhất và trả về khối Template chứa các giao dịch hợp lệ.
	blockRaw, failIdx, errMsg = c.bridge.BuildVanguardBlockTemplate(nextHeight, parentHash, currentMinerAddr, txsBytes, nowTs, difficulty)

	if blockRaw == nil || len(blockRaw) == 0 {
		log.Printf("[VANGUARD-CRITICAL] ❌ Thất bại trong việc tạo Template từ Rust Core: %s (FailIdx: %d)", errMsg, failIdx)
		return nil, 0
	}

	// 5. Giải mã khối từ Rust để tạo MiningTask cho Go
	var finalBlock pb_block.Block
	if err := proto.Unmarshal(blockRaw, &finalBlock); err != nil {
		log.Printf("[VANGUARD-CRITICAL] ❌ Lỗi giải mã Template từ Rust: %v", err)
		return nil, 0
	}

	// [VANGUARD-DDoS-SHIELD] Đối chiếu danh sách giao dịch ban đầu với danh sách được đóng gói trong Template.
	// Bất kỳ giao dịch nào bị thiếu (do bị Rust Core loại bỏ vì lỗi trong dry-run) sẽ được dọn dẹp sạch sẽ khỏi Mempool.
	// Tại sao: Cơ chế này dọn dẹp mempool hàng loạt (bulk eviction) chỉ sau 1 cuộc gọi gRPC, ngăn ngừa hoàn toàn nguy cơ nghẽn
	// Miner bởi hàng ngàn giao dịch rác lệch nonce/thiếu số dư từ kẻ tấn công.
	if c.mempool != nil && len(txs) > 0 && finalBlock.Body != nil {
		finalTxs := finalBlock.Body.Transactions
		packedHashes := make(map[string]bool)
		for _, tx := range finalTxs {
			if tx.Sender == nil { // Bỏ qua Coinbase
				continue
			}
			txData, _ := proto.Marshal(tx)
			// [VANGUARD-OPTIMIZATION] Tính TxID cục bộ bằng Go Native để tránh gRPC storm GetCanonicalTxHash.
			h := node_p2p.GetTxIDNative(txData)
			packedHashes[hex.EncodeToString(h)] = true
		}

		var removedHashes []string
		for _, tx := range txs {
			txData, _ := proto.Marshal(tx)
			// [VANGUARD-OPTIMIZATION] Tính TxID cục bộ bằng Go Native để tránh gRPC storm GetCanonicalTxHash.
			h := node_p2p.GetTxIDNative(txData)
			hStr := hex.EncodeToString(h)
			if !packedHashes[hStr] {
				removedHashes = append(removedHashes, hStr)
			}
		}

		if len(removedHashes) > 0 {
			log.Printf("[VANGUARD-DDoS-SHIELD] 🛡️ Phát hiện %d giao dịch bị Rust Core loại bỏ do lỗi. Đang dọn dẹp khỏi Mempool...", len(removedHashes))
			c.mempool.RemoveTransactions(removedHashes)
		}
	}

	header := finalBlock.Header
	if header == nil {
		log.Printf("[VANGUARD-CRITICAL] ❌ Template từ Rust không có Header!")
		return nil, 0
	}

	// 6. Tạo MiningTask
	intensity := 100
	if c.rpcSrv != nil {
		intensity = c.rpcSrv.GetCpuIntensity()
	}

	stateRootHex := "N/A"
	if header.StateRoot != nil && header.StateRoot.Value != nil {
		stateRootHex = hex.EncodeToString(header.StateRoot.Value)
	}

	log.Printf("[MINER-TASK] ⛏ Gửi Task chuẩn từ RUST Core: #%d | Cường độ: %d%% | StateRoot: %s",
		nextHeight, intensity, stateRootHex)

	// [VANGUARD-COMMAND] Trust the AbsoluteWeight calculated by Rust Core.

	// [V5.3] Sinh Session ID duy nhất cho Task này
	sid := uint64(time.Now().UnixNano())

	task := &pb_consensus.MiningTask{
		Header:    header,
		Intensity: uint32(intensity),
		Threads:   uint32(2), // [VANGUARD-OPTIMIZATION] Giới hạn tối đa 2 threads để tránh overload CPU, treo HTTP server và lag P2P
		SessionId: sid,
	}
	taskBytes, _ := proto.Marshal(task)

	// 7. Lưu lại trạng thái đào hiện tại (Để phục vụ Submit Mining Result sau này)
	c.activeMiningMu.Lock()
	bodyData, _ := proto.Marshal(finalBlock.Body)
	c.activeBodyData = bodyData

	// Tính TxHashes từ Body của Rust (Để Go đồng nhất)
	finalTxs := finalBlock.Body.Transactions
	txHashesSlice := make([][]byte, 0, len(finalTxs))
	for _, t := range finalTxs {
		d, _ := proto.Marshal(t)
		txHash := make([]byte, 32)
		go_bridge.CalculateBlake3Hash(d, txHash, nextHeight)
		txHashesSlice = append(txHashesSlice, txHash)
	}

	c.activeTxHashes = txHashesSlice
	c.activeTxRoot = header.TxRoot.Value // [ARCH-FIX] Nhận TxRoot từ Rust
	c.activeTxs = finalTxs
	c.activeParentHash = parentHash
	c.activeDifficulty = difficulty
	c.activeTargetHeight = nextHeight
	c.activeTimestamp = header.Timestamp
	c.activeSessionId = sid     // [V5.3] Lưu SID để hậu kiểm
	c.activeBlock = &finalBlock // [V5.4] Lưu trữ bản gốc để nộp khối sau này
	c.activeMiningMu.Unlock()

	// [V5.1-FIX] Không gọi SubmitMiningTask ở đây — StartMiningV2 sẽ gọi
	return taskBytes, sid
}

func (c *CLIApp) ShowStatus() {
	h := c.bridge.GetCurrentVersion()
	fmt.Printf("   YonaCode Go V1.0 - CAO: #%d (Unified Ledger)\n", h)
}

func (c *CLIApp) RefreshMiningTask() {
	log.Printf("[MINER-CONTROL] 🔄 Đang tái khởi động tác vụ đào để áp dụng cấu hình mới...")
	h := c.bridge.GetCurrentVersion()
	var nextHeight uint64
	genHash := c.bridge.GetBlockHash(0)
	if h == 0 && len(genHash) == 0 {
		nextHeight = 0
	} else {
		nextHeight = h + 1
	}
	// [VANGUARD-BLOCKSIZE] Sử dụng kích thước khối mặc định 5MB
	pendingTxs := c.mempool.GetPartitionedTransactions(node_p2p.DefaultBlockMaxSize)
	taskBytes, sid := c.buildAndSubmitTemplate(nextHeight, pendingTxs)
	if len(taskBytes) > 0 {
		c.bridge.StartMiningV2(taskBytes)
		if c.rpcSrv != nil {
			c.rpcSrv.BroadcastMiningTask(taskBytes, sid, c.activeDifficulty)
		}
	}
}
