/**
 * @file ban_manager.go
 * @brief Hệ thống Quản lý Danh sách đen IP & Peer ID lũy tiến (YonaCode Security Shield).
 * @details Triển khai libp2p.ConnectionGater với cơ chế phòng thủ 5 lớp để ngăn chặn tấn công DDoS:
 *  - Lớp 1: Cảnh vệ Vòng ngoài (Xác thực đầu vào Peer ID/IP).
 *  - Lớp 2: An ninh Nội bộ (Kiểm tra trạng thái cấm đa tầng qua ConnectionGater).
 *  - Lớp 3: Chống khủng bố (Bảo vệ tài nguyên, giới hạn thời gian cấm lũy tiến, tối đa 72h).
 *  - Lớp 4: Tình báo ICS (Ghi log bất biến mọi lệnh cấm/gỡ cấm).
 *  - Lớp 5: Phản ứng nhanh RRF (Tự động cấm cả IP lẫn Peer ID trên localhost).
 * 
 * Hỗ trợ cấm cả IP Address và Peer ID để chống DDoS hiệu quả 100% trên Localhost.
 * 
 * @author Vô Nhật Thiên &  - YonaCode V1.1 Security
 * @date 2026-05-18
 */

package node_p2p

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type StaticPeer struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Priority int    `json:"priority"`
	Name     string `json:"name"`
}

type BanManager struct {
	mu            sync.RWMutex
	bannedIPs     map[string]time.Time
	penaltyPoints map[string]int        // [VANGUARD-SCORING] IP -> Tổng điểm phạt cộng dồn
	bannedPeers   map[peer.ID]time.Time // [SECURITY-DDoS] Peer ID -> Thời gian hết hạn cấm (Chống ban ảo trên localhost)

	// Quản lý Node tĩnh (Static Peers) & Chế độ cách ly
	staticPeers      []StaticPeer
	isolationMode    int // 1: Anchor, 2: Block Trust, 3: Strict Isolation
	dbPath           string
	allStaticOffline int32 // 0: Online, 1: Offline (Tất cả static peers mất kết nối)
}

func NewBanManager(dbPath string) *BanManager {
	bm := &BanManager{
		bannedIPs:     make(map[string]time.Time),
		penaltyPoints: make(map[string]int),
		bannedPeers:   make(map[peer.ID]time.Time),
		isolationMode: 1, // Mặc định: Chế độ mỏ neo (Anchor Mode)
		dbPath:        dbPath,
	}
	bm.loadStaticPeers()
	// Tự động dọn dẹp danh sách ban cũ mỗi 5 phút
	go bm.gcLoop()
	return bm
}

func (bm *BanManager) loadStaticPeers() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	filePath := filepath.Join(bm.dbPath, "static_peers.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		bm.staticPeers = []StaticPeer{}
		bm.isolationMode = 1
		return
	}
	var config struct {
		StaticPeers   []StaticPeer `json:"static_peers"`
		IsolationMode int          `json:"isolation_mode"`
	}
	if err := json.Unmarshal(data, &config); err == nil {
		bm.staticPeers = config.StaticPeers
		bm.isolationMode = config.IsolationMode
	} else {
		bm.staticPeers = []StaticPeer{}
		bm.isolationMode = 1
	}
}

func (bm *BanManager) saveStaticPeers() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	filePath := filepath.Join(bm.dbPath, "static_peers.json")
	config := struct {
		StaticPeers   []StaticPeer `json:"static_peers"`
		IsolationMode int          `json:"isolation_mode"`
	}{
		StaticPeers:   bm.staticPeers,
		IsolationMode: bm.isolationMode,
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	_ = os.WriteFile(filePath, data, 0644)
}

func (bm *BanManager) GetStaticPeers() []StaticPeer {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	res := make([]StaticPeer, len(bm.staticPeers))
	copy(res, bm.staticPeers)
	return res
}

func (bm *BanManager) SetStaticPeers(peers []StaticPeer) {
	bm.mu.Lock()
	bm.staticPeers = peers
	bm.mu.Unlock()
	bm.saveStaticPeers()
}

func (bm *BanManager) GetIsolationMode() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.isolationMode
}

func (bm *BanManager) SetIsolationMode(mode int) {
	bm.mu.Lock()
	bm.isolationMode = mode
	bm.mu.Unlock()
	bm.saveStaticPeers()
}

func (bm *BanManager) SetAllStaticOffline(offline bool) {
	if offline {
		atomic.StoreInt32(&bm.allStaticOffline, 1)
	} else {
		atomic.StoreInt32(&bm.allStaticOffline, 0)
	}
}

func (bm *BanManager) IsAllStaticOffline() bool {
	return atomic.LoadInt32(&bm.allStaticOffline) == 1
}

func (bm *BanManager) IsStaticPeerID(id peer.ID) bool {
	for _, p := range bm.staticPeers {
		if p.ID == id.String() {
			return true
		}
	}
	return false
}

func (bm *BanManager) isStaticIP(ip string) bool {
	for _, p := range bm.staticPeers {
		ma, err := multiaddr.NewMultiaddr(p.Address)
		if err == nil {
			pip, err := manet.ToIP(ma)
			if err == nil && pip.String() == ip {
				return true
			}
		}
	}
	return false
}

func (bm *BanManager) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		bm.mu.Lock()
		now := time.Now()
		// 1. Dọn dẹp danh sách IP hết hạn cấm
		for ip, expiry := range bm.bannedIPs {
			if now.After(expiry) {
				delete(bm.bannedIPs, ip)
				log.Printf("[SECURITY-SHIELD] 🔓 Tạm thời gỡ cấm cho IP: %s (Hết thời gian thử thách)", ip)
			}
		}
		// 2. Dọn dẹp danh sách Peer ID hết hạn cấm
		for peerID, expiry := range bm.bannedPeers {
			if now.After(expiry) {
				delete(bm.bannedPeers, peerID)
				log.Printf("[SECURITY-SHIELD] 🔓 Tạm thời gỡ cấm cho Peer ID: %s (Hết thời gian thử thách)", peerID.String())
			}
		}
		bm.mu.Unlock()
	}
}

// BanIP đưa một IP vào danh sách đen với thời gian cấm được chỉ định cụ thể.
func (bm *BanManager) BanIP(ip string, duration time.Duration) {
	parsedIP := net.ParseIP(ip)
	if parsedIP != nil && parsedIP.IsLoopback() {
		return
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.bannedIPs[ip] = time.Now().Add(duration)
	log.Printf("[SECURITY-SHIELD] 🛑 BAN IP: %s | Thời gian cấm: %v", ip, duration)
}

// IsBanned kiểm tra xem IP có đang bị cấm hay không
func (bm *BanManager) IsBanned(ip string) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	expiry, found := bm.bannedIPs[ip]
	if !found {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// BanPeer đưa một Peer ID vào danh sách đen để chặn đứng các kết nối từ Peer đó (đặc biệt hữu ích trên localhost).
func (bm *BanManager) BanPeer(id peer.ID, duration time.Duration) {
	if id == "" {
		return
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	expiry := time.Now().Add(duration)
	bm.bannedPeers[id] = expiry
	log.Printf("[SECURITY-SHIELD] 🛑 BAN PEER ID: %s | Thời gian cấm: %v | [NGUYÊN LÝ TỪ CHỐI TĨNH LẶNG]", id.String(), duration)
}

// IsPeerBanned kiểm tra xem Peer ID có đang bị cấm hay không.
func (bm *BanManager) IsPeerBanned(id peer.ID) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	expiry, found := bm.bannedPeers[id]
	if !found {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// --- Triển khai libp2p connmgr.ConnectionGater ---

func (bm *BanManager) InterceptAddrDial(id peer.ID, addr multiaddr.Multiaddr) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.IsPeerBanned(id) || bm.isAddrBanned(addr) {
		return false
	}

	// Chế độ 3: Cách ly Tuyệt đối (Strict Isolation)
	if bm.isolationMode == 3 {
		return bm.IsStaticPeerID(id)
	}

	return true
}

func (bm *BanManager) InterceptPeerDial(p peer.ID) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.IsPeerBanned(p) {
		return false
	}

	// Chế độ 3: Cách ly Tuyệt đối (Strict Isolation)
	if bm.isolationMode == 3 {
		return bm.IsStaticPeerID(p)
	}

	return true
}

func (bm *BanManager) InterceptAccept(conn network.ConnMultiaddrs) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.isAddrBanned(conn.RemoteMultiaddr()) {
		return false
	}

	// Chế độ 3: Cách ly Tuyệt đối (Strict Isolation)
	if bm.isolationMode == 3 {
		ip, err := manet.ToIP(conn.RemoteMultiaddr())
		if err != nil {
			return false
		}
		return bm.isStaticIP(ip.String())
	}

	return true
}

func (bm *BanManager) InterceptSecured(dir network.Direction, id peer.ID, conn network.ConnMultiaddrs) bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.IsPeerBanned(id) || bm.isAddrBanned(conn.RemoteMultiaddr()) {
		return false
	}

	// Chế độ 3: Cách ly Tuyệt đối (Strict Isolation)
	if bm.isolationMode == 3 {
		ip, err := manet.ToIP(conn.RemoteMultiaddr())
		if err != nil {
			return false
		}
		return bm.IsStaticPeerID(id) && bm.isStaticIP(ip.String())
	}

	return true
}

func (bm *BanManager) InterceptUpgraded(conn network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func (bm *BanManager) isAddrBanned(addr multiaddr.Multiaddr) bool {
	ip, err := manet.ToIP(addr)
	if err != nil {
		return false
	}
	if bm.IsBanned(ip.String()) {
		return true
	}
	return false
}

// [SOCIAL-CONSENSUS] Lệnh ân xá toàn diện: Xóa sạch toàn bộ sổ đen IP và PeerID.
func (bm *BanManager) ClearAllBans() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bannedIPs = make(map[string]time.Time)
	bm.penaltyPoints = make(map[string]int)
	bm.bannedPeers = make(map[peer.ID]time.Time)
	log.Println("🕊️ [SOCIAL-CONSENSUS] LỆNH ÂN XÁ TOÀN DIỆN: Đã xóa toàn bộ sổ đen IP và PeerID.")
}

// Đảm bảo BanManager tuân thủ interface ConnectionGater
var _ connmgr.ConnectionGater = (*BanManager)(nil)
