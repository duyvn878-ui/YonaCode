/**
 * @file network_manager.go
 * @brief Bộ quản lý mạng P2P cốt lõi (YonaCode Network Manager).
 * @details Quản lý kết nối Libp2p, trao đổi luồng thông tin, định tuyến, phát sóng giao dịch/khối,
 * tích hợp bộ lọc và trừng phạt DDoS theo cơ chế Ban lũy tiến (IP & Peer ID).
 *
 * @author  Vô Nhật Thiên - YonaCode V1.1 Security
 * @date 2026-05-18
 */

package node_p2p

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"btc_genz/2_miner_core/go_bridge"
	pb_block "btc_genz/proto"
	"btc_genz/6_user_interface/audit"

	"os"
	"path/filepath"
	"strings"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"google.golang.org/protobuf/proto"
)

const (
	HandshakeProtocol = "/btc_gen_z/handshake/1.0.0"
	SyncProtocol      = "/btc_gen_z/sync/1.0.0"
	PexProtocol       = "/btc_gen_z/pex/1.0.0"
	FileChunkProtocol = "/btc_gen_z/file_chunk/1.0"
	ManifestProtocol  = "/btc_gen_z/manifest/1.0"
)

type BridgeInterface interface {
	GetCurrentVersion() uint64
	GetFinalizedHeight() uint64
	GetOldestHeight() uint64
	SetFinalizedHeight(h uint64)
	ForceSetFinalizedHeight(h uint64)
	GetMedianTimePast(h uint64) uint64
	GetHeaderRaw(hash []byte) []byte
	VerifyPow(header []byte, nonce uint64, difficulty []byte, height uint64) (bool, error)
	GetBlockHash(h uint64) []byte
	ProcessChain(blocksRaw [][]byte) (*pb_block.SyncChainResponse, error)
	EvaluateHeaderChain(headers [][]byte) (*pb_block.EvaluateHeaderChainResponse, error)
	VerifyTimestampFirewall(ts, mtp, now uint64) bool
	CalculateAbsoluteWeight(parent []byte, diff []byte) []byte
	GetCanonicalBlockHeaderHash(headerBytes []byte, height uint64) []byte
	CalculateBlockHeaderHash(data []byte) []byte
	CalculateShortTxIdFfi(txHash []byte, nonce uint64) uint64
	GetCanonicalTxHash(txBytes []byte, height uint64) []byte
	VerifyBlockReconstruction(root []byte, hashes [][]byte) bool
	GetBlock(h uint64) []byte
	SaveBlockRaw(height uint64, hash []byte, data []byte, isCanonical bool) bool
	GetStateRoot() []byte

	GetSpendableBalance(addr []byte) uint64
	GetAccountState(addr []byte) *pb_block.AccountSnapshot
	GetBalance(ctx any, addr []byte, version uint32) uint64
	GetBalanceBatch(addresses [][]byte) ([]*pb_block.BalanceEntry, error)
	GetNonce(ctx any, addr []byte) uint64
	GetSigningHash(tx *pb_block.Transaction) []byte
	VerifySignature(addr []byte, hash []byte, sig []byte) bool
	AddToMempool(hash []byte, data []byte) (bool, error)
	AddBatchToMempool(hashes [][]byte, raws [][]byte) (bool, error)
	RemoveFromMempool(hash []byte) (bool, error)
	RemoveFromMempoolBatch(hashes [][]byte) (bool, error)
	SetMiningPause(pause bool)
	GetMempoolEntries() ([]*pb_block.MempoolEntry, error)
	ExportStateSnapshotAtHeightRaw(height uint64) []byte
	IsValidFee(fee uint64) bool
	GetRawByHash(hash []byte) []byte
	CommitBlockHash(height uint64, hash []byte)
	RollbackState(context any, currentH, targetH uint64) bool
	GetHashrate() uint64
	ClearStagingArea() (bool, error)
	ResetStateCompletely() (bool, error)
	ImportStateSnapshotPath(path string, version uint64) []byte

	// ValidateTransactionBatch gửi loạt giao dịch xuống Rust Core để kiểm duyệt song song/tuần tự nhằm tối ưu hóa hiệu năng, giảm nghẽn cổ chai.
	ValidateTransactionBatch(rawTxs [][]byte) (*pb_block.ValidateTxBatchResponse, error)

	// CalculateTxHashesBatch băm hàng loạt giao dịch song song trên đa nhân Rust Core để đảm bảo an toàn đồng thuận
	CalculateTxHashesBatch(rawTxs [][]byte, height uint64) ([][]byte, error)
	GetNodeConfig() ([]byte, error)
}

type NetworkManager struct {
	Ctx                  context.Context
	Host                 host.Host
	PubSub               *pubsub.PubSub
	TopicMap             map[string]*pubsub.Topic
	TopicMu              sync.Mutex
	PeerHeights          map[peer.ID]uint64
	PeerFinalizedHeights map[peer.ID]uint64 // [V38.2] Lưu trữ các điểm chốt (Checkpoint) Snapshot của Peer
	PeerOldestHeights    map[peer.ID]uint64 // [V38.3] Lưu trữ khối cũ nhất Peer còn giữ (Sau Purge)
	PeerRTT              map[peer.ID]time.Duration
	PeerMutex            sync.RWMutex
	GenesisHash          []byte
	AbsoluteHeight       uint64
	AbsoluteWeight       *big.Int
	DbPath               string             // [VANGUARD-FILE-SYNC] Đường dẫn thư mục dữ liệu để định vị snapshot

	Bridge  BridgeInterface
	Mempool MempoolInterface

	NetworkHeight uint64
	Discovery     P2PDiscovery
	SyncEngine    SyncEngineInterface

	// [V35 CONCORDANCE] Callback khi khối mới được chốt hạ (Commit)
	OnBlockCommitted func(height uint64)
	OnRollback       func(targetHeight uint64) // [V1.60] Tín hiệu đồng bộ lại UI khi có Reorg

	// [V2.0 DASHBOARD] Lưu lượng băng thông (Atomic counters)
	BytesSent uint64
	BytesRecv uint64

	// [V1.1.4.2] Chế độ đồng bộ P2P: "snap" (Nhảy cóc - Mặc định) hoặc "full" (Cày cuốc)
	SyncMode string

	// [VANGUARD-LEAKY-BUCKET] Hệ thống trừng phạt Peer theo mô hình Leaky Bucket (Chống DDoS)
	// Tại sao dùng Leaky Bucket: Tránh leo thang vĩnh viễn do lỗi mạng thoáng qua.
	// Mỗi 5 phút không vi phạm, penalty tự giảm 1 điểm (decay). Chỉ ban khi vi phạm liên tục.
	PeerPenalties    map[peer.ID]int
	PeerPenaltyTimes map[peer.ID]time.Time // Thời điểm phạt cuối cùng để tính Forgiveness Decay
	PenaltyMu        sync.Mutex
	BanMgr           *BanManager // [VANGUARD-DDoS-PROTECTION]

	// Mock phục vụ Unit Test
	RequestBlockTxnMock    func(ctx context.Context, p peer.ID, blockHash []byte, missingIndexes []uint32) ([]*pb_block.Transaction, error)
	RequestBlockByHashMock func(p peer.ID, hash []byte) ([]byte, error)

	// [NO-TICKER-FIX] Kích hoạt handshake ngay lập tức khi nhận được khối mới
	TriggerHandshakeChan chan struct{}

	// [NAT-AUDIT] Trạng thái NAT do AutoNAT xác định: 0=Chưa xác định, 1=Public, 2=Private
	NatStatus uint32

	// [SECURITY-HARDENING] Rate limiting cho các yêu cầu header
	LastHeaderRequest    map[peer.ID]time.Time
	HeaderRequestMu      sync.Mutex

	// [SECURITY-HARDENING] Rate limiting & Concurrency control cho PEX
	LastPexRequest       map[peer.ID]time.Time
	PexRequestMu         sync.Mutex
	PexSem               chan struct{}

	// [ANTI-SPAM-P2P] Cache Nonce trên RAM để tránh bão gRPC GetNonce khi có Gossip flood
	NonceCache     map[string]uint64
	NonceCacheTime map[string]time.Time
	NonceCacheMu   sync.RWMutex

	// [PEX-LIMIT] Khống chế tần suất tự gửi GossipPeers (tối đa 1 lần/15s)
	LastGossipTime time.Time
	LastGossipMu   sync.Mutex

	// [VANGUARD-DIFF-FAST-REJECT] Chốt chặn độ khó tối thiểu DAA
	minDifficulty  *big.Int

	// [TARGET-HASH-SYNC]
	TargetPathHashes map[string]bool
	TargetPathMu     sync.RWMutex
}

func (n *NetworkManager) RecordSent(b uint64) {
	atomic.AddUint64(&n.BytesSent, b)
}

func (n *NetworkManager) RecordRecv(b uint64) {
	atomic.AddUint64(&n.BytesRecv, b)
}

type HeaderStore interface {
	GetHeader(height uint64) ([]byte, error)
	GetHighestHeight() (uint64, error)
	GetAbsoluteWeight() *big.Int
	UpdateAbsoluteWeight(weight *big.Int) error
	SaveHeader(height uint64, hash []byte, data []byte) error
	GetBlockBody(height uint64) ([]byte, error)
	SaveBlockBody(height uint64, hash []byte, data []byte) error
	SetCanonical(height uint64, hash []byte) error
	SaveZKProof(height uint64, proofBytes []byte) error
	SaveBlockBatch(height uint64, hash []byte, headerBytes []byte, bodyBytes []byte, isCanonical bool, weight *big.Int) error
	PruneOldBodies(currentHeight uint64, bridge interface{ PurgeOldHistory(start, end uint64) bool }) error
}

type TxValidatedResult struct {
	TxHash      string
	IsValid     bool
	StatusCode  uint32
	ErrorMsg    string
	TxData      []byte
	Tx          *pb_block.Transaction
	CreationFee uint64
	SenderBalance uint64 // [VANGUARD-OPTIMIZATION] Số dư người gửi được cache sẵn từ GetBalanceBatch để tránh bão gRPC GetBalance đơn lẻ
}

type MempoolInterface interface {
	AddValidatedTx(txHash string, txData []byte, senderHex string, tx *pb_block.Transaction, creationFee uint64) bool
	RemoveTransactions(txHashes []string)
	RemoveStaleNonceTxsBatch(senders []string, nonces []uint64) int // [V1.60-RACE-FIX] Xóa TX stale theo lô sau block commit
	GetShortIDMap(nonce uint64) (map[uint64][]byte, map[uint64]string)
	SetOnUpdate(f func())
	GetPendingTxList() []PendingTxInfo
	GetRecommendedFee(amount uint64) uint64
	GetPendingSpend(senderHex string) uint64
	GetNextNonce(senderHex string, currentNonce uint64) uint64
	GetExpectedNonce(senderHex string, currentNonce uint64) uint64
	GetAndReserveNonces(senderHex string, currentNonce uint64, count uint64) uint64
	ClearProjectedNonce(senderHex string)
	GetTransaction(txHash string) ([]byte, bool)
	GetSequentialTxsToPublish(senderHex string) [][]byte
	Purge()
	GetTxsToRebroadcast() [][]byte

	// [2-SECOND-BUS] Đẩy giao dịch thô vào TxBus RAM Channel
	PushToTxBus(txData []byte, isLocal bool) bool
	// [2-SECOND-BUS] Đăng ký callback khi loạt giao dịch được xe buýt xác thực xong
	SetOnTxBatchValidated(f func(results []TxValidatedResult))
}

type P2PDiscovery interface {
	DiscoveryLoop()
}

type SyncEngineInterface interface {
	IsSynced() bool
	CheckFinality(height uint64)
	GetFinalizedHeight() uint64
	HandleBlockArrival(block *pb_block.Block, from peer.ID)
	GetSyncProgress() (uint64, uint64, string)
	GetSnapshotProgress() (uint32, uint32)
	UpdateHeight(height uint64)
	GetLastSyncActivity() time.Time // [VANGUARD-DYNAMISM] Truy xuất nhịp đập đồng bộ cuối cùng
	StartSync(targetHeight uint64)
}

func NewNetworkManager(ctx context.Context, h host.Host, ps *pubsub.PubSub, genesisHash []byte, bridge BridgeInterface, mp MempoolInterface, banMgr *BanManager) *NetworkManager {
	n := &NetworkManager{
		Ctx:                  ctx,
		Host:                 h,
		PubSub:               ps,
		TopicMap:             make(map[string]*pubsub.Topic),
		PeerHeights:          make(map[peer.ID]uint64),
		PeerFinalizedHeights: make(map[peer.ID]uint64),
		PeerOldestHeights:    make(map[peer.ID]uint64),
		PeerRTT:              make(map[peer.ID]time.Duration),
		GenesisHash:          genesisHash,
		Bridge:               bridge,
		Mempool:              mp,
		AbsoluteWeight:       big.NewInt(0), // Khởi tạo tạm, sẽ cập nhật qua Handshake/Sync
		Discovery:            nil,
		SyncEngine:           nil,
		BanMgr:               banMgr,
		PeerPenaltyTimes:     make(map[peer.ID]time.Time),
		TriggerHandshakeChan: make(chan struct{}, 1),
		LastHeaderRequest:    make(map[peer.ID]time.Time),
		LastPexRequest:       make(map[peer.ID]time.Time),
		PexSem:               make(chan struct{}, 16),
		NonceCache:           make(map[string]uint64),
		NonceCacheTime:       make(map[string]time.Time),
		minDifficulty:        big.NewInt(10000000000), // Mặc định fallback
		TargetPathHashes:     make(map[string]bool),
	}

	n.UpdateMinDifficultyFromGenesis()

	// [P2P-SHIELD] Lắng nghe sự kiện ngắt kết nối thực tế để dọn Ghost Peers khỏi RAM
	if h != nil && h.Network() != nil {
		h.Network().Notify(&network.NotifyBundle{
			DisconnectedF: func(net network.Network, conn network.Conn) {
				p := conn.RemotePeer()
				n.PeerMutex.Lock()
				delete(n.PeerHeights, p)
				delete(n.PeerFinalizedHeights, p)
				delete(n.PeerOldestHeights, p)
				delete(n.PeerRTT, p)
				n.PeerMutex.Unlock()

				n.HeaderRequestMu.Lock()
				delete(n.LastHeaderRequest, p)
				n.HeaderRequestMu.Unlock()

				n.PexRequestMu.Lock()
				delete(n.LastPexRequest, p)
				n.PexRequestMu.Unlock()

				log.Printf("[P2P-SHIELD] 🧹 Đã dọn sạch 100%% cấu trúc bộ nhớ của Ghost Peer: %s", p.String()[:12])
			},
		})
	}

	return n
}

// UpdateMinDifficultyFromGenesis cập nhật độ khó tối thiểu tự động từ khối Genesis (Block 0)
func (n *NetworkManager) UpdateMinDifficultyFromGenesis() {
	genesisHash := n.Bridge.GetBlockHash(0)
	if len(genesisHash) == 32 {
		genesisHeaderBytes := n.Bridge.GetHeaderRaw(genesisHash)
		if len(genesisHeaderBytes) > 0 {
			var genesisHeader pb_block.BlockHeader
			if err := proto.Unmarshal(genesisHeaderBytes, &genesisHeader); err == nil && len(genesisHeader.Difficulty) > 0 {
				genesisDiff := go_bridge.BytesToBigInt(genesisHeader.Difficulty)
				n.minDifficulty = genesisDiff
				log.Printf("[P2P-MIN-DIFF] 🔄 Đã cập nhật độ khó tối thiểu tự động từ Genesis Block: %s", genesisDiff.String())
			}
		}
	}
}

// punishPeer thực hiện trừng phạt Peer gửi rác theo cơ chế LEAKY BUCKET + BAN LŨY TIẾN.
// Tại sao dùng Leaky Bucket thay vì hard-coded penalty:
//   - Lỗi mạng thoáng qua (packet loss, timeout gRPC) có thể gây vi phạm 1-2 lần rồi tự khỏi.
//   - Penalty cứng sẽ leo thang vĩnh viễn → ban nhầm Peer trung thực → Network Partition.
//   - Leaky Bucket cho phép điểm phạt tự giảm (decay) theo thời gian, chỉ trừng phạt nặng
//     khi vi phạm liên tục trong cửa sổ ngắn (5 phút).
//
// Cơ chế hoạt động:
//   1. Mỗi 5 phút không vi phạm → giảm 1 điểm phạt (Forgiveness Decay).
//   2. Penalty chỉ leo thang nếu Peer vi phạm liên tục trong cửa sổ 5 phút.
//   3. Mức phạt lũy tiến: Cảnh cáo → 5 phút → 30 phút → 2 giờ → 24 giờ (cứng, tối đa).
func (n *NetworkManager) punishPeer(id peer.ID, reason string) {
	n.PenaltyMu.Lock()
	if n.PeerPenalties == nil {
		n.PeerPenalties = make(map[peer.ID]int)
	}
	if n.PeerPenaltyTimes == nil {
		n.PeerPenaltyTimes = make(map[peer.ID]time.Time)
	}

	// --- [LEAKY BUCKET DECAY] Tha thứ theo thời gian ---
	// Tại sao: Ngăn chặn leo thang vĩnh viễn do các sự cố mạng thoáng qua.
	// Cách hoạt động: Mỗi cửa sổ 5 phút không có vi phạm mới, giảm 1 điểm phạt.
	const forgivenessWindow = 5 * time.Minute
	now := time.Now()
	if lastTime, exists := n.PeerPenaltyTimes[id]; exists {
		elapsed := now.Sub(lastTime)
		if elapsed > forgivenessWindow {
			// Số cửa sổ 5 phút đã trôi qua kể từ lần phạt cuối
			decaySteps := int(elapsed / forgivenessWindow)
			oldPenalty := n.PeerPenalties[id]
			if decaySteps > 0 && oldPenalty > 0 {
				newPenalty := oldPenalty - decaySteps
				if newPenalty < 0 {
					newPenalty = 0
				}
				n.PeerPenalties[id] = newPenalty
				log.Printf("[PEER-SHIELD] 🕊️ Tha thứ Peer %s: %d → %d điểm phạt (-%d sau %v không vi phạm)",
					id.String()[:12], oldPenalty, newPenalty, decaySteps, elapsed.Round(time.Second))
			}
		}
	}

	// Tăng điểm phạt và cập nhật thời gian
	n.PeerPenalties[id]++
	count := n.PeerPenalties[id]
	n.PeerPenaltyTimes[id] = now
	n.PenaltyMu.Unlock()

	var duration time.Duration
	var banType string

	// Mức phạt lũy tiến (Progressive Banning) với ngưỡng hợp lý hơn:
	// - Mức 1-3: Cảnh cáo + ngắt kết nối → Discovery tự reconnect sau 5 giây.
	// - Mức 4: Tạm giam 5 phút → Đủ để Peer cập nhật lại trạng thái.
	// - Mức 5: Tạm giam 30 phút → Peer có vấn đề nghiêm trọng hơn.
	// - Mức 6: Trục xuất 2 giờ → Nghi ngờ hành vi tấn công có chủ đích.
	// - Mức 7+: Trục xuất 24 giờ → Xác định hành vi tấn công. Giảm từ 72h cũ
	//           để tránh trường hợp Peer bị ban quá lâu do lỗi phần mềm (patch xong quay lại).
	switch count {
	case 1, 2, 3:
		log.Printf("[PEER-SHIELD] ⚠️ Cảnh cáo Peer %s (Lần %d/%d). Lý do: %s",
			id.String()[:12], count, 3, reason)
		// Chỉ ngắt kết nối để dọn dẹp state, Discovery tự reconnect sau 5 giây
		n.Host.Network().ClosePeer(id)
		return // Thoát, chưa cấm IP/Peer ID
	case 4:
		duration = 5 * time.Minute
		banType = "Tạm giam (Sơ cấp)"
	case 5:
		duration = 30 * time.Minute
		banType = "Tạm giam (Trung cấp)"
	case 6:
		duration = 2 * time.Hour
		banType = "Trục xuất (2h)"
	default:
		// [LEAKY-BUCKET-CAP] Giới hạn tối đa 24h thay vì 72h.
		// Tại sao: 72h quá dài cho trường hợp Peer bị lỗi phần mềm → patch xong → quay lại.
		// Với Leaky Bucket, nếu Peer đã im lặng 24h, penalty cũng đã decay hết về 0.
		duration = 24 * time.Hour
		banType = "Trục xuất tối đa (24h)"
	}

	log.Printf("[PEER-SHIELD] 🚫 %s Peer %s trong %v (điểm phạt: %d). Lý do: %s",
		banType, id.String()[:12], duration, count, reason)

	// Truy tìm IP và áp dụng lệnh cấm
	conns := n.Host.Network().ConnsToPeer(id)
	for _, conn := range conns {
		remoteAddr := conn.RemoteMultiaddr()
		if ip, err := manet.ToIP(remoteAddr); err == nil {
			if n.BanMgr != nil {
				n.BanMgr.BanIP(ip.String(), duration)
			}
		}
	}

	// [SECURITY-SHIELD] Cấm thêm cả Peer ID thực tế để cơ chế trừng phạt có tác dụng 100% trên Localhost cluster
	if n.BanMgr != nil {
		n.BanMgr.BanPeer(id, duration)
	}

	n.Host.Network().ClosePeer(id)
}

func (n *NetworkManager) BroadcastBlock(header *pb_block.BlockHeader, body *pb_block.BlockBody) error {
	topic, ok := n.TopicMap["blocks"]
	if !ok {
		var err error
		topic, err = n.JoinTopic("blocks")
		if err != nil {
			return err
		}
	}

	// [V1.0 FINAL] Compact Block Propagation
	cb := &pb_block.CompactBlock{
		Header: header,
	}

	if len(body.Transactions) > 0 {
		cb.CoinbaseTx = body.Transactions[0]

		var rawTxs [][]byte
		for _, tx := range body.Transactions[1:] {
			d, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
			rawTxs = append(rawTxs, d)
		}

		if len(rawTxs) > 0 {
			// Tại sao: Gọi API gRPC CalculateTxHashesBatch một lần để băm song song toàn bộ giao dịch dưới Rust Core.
			// Tiết kiệm hàng ngàn gRPC roundtrip tuần tự khi khối lớn được broadcast.
			hashes, err := n.Bridge.CalculateTxHashesBatch(rawTxs, header.Height)
			if err != nil || len(hashes) != len(rawTxs) {
				log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed in BroadcastBlock: %v. Fallback to native.", err)
				hashes = make([][]byte, len(rawTxs))
				for idx, d := range rawTxs {
					hashes[idx] = GetTxIDNative(d)
				}
			}

			for idx, hash := range hashes {
				tx := body.Transactions[idx+1]
				sid := n.Bridge.CalculateShortTxIdFfi(hash, tx.Nonce)
				cb.ShortIds = append(cb.ShortIds, sid)
			}
		}
	}

	compactData, _ := proto.Marshal(cb)
	return topic.Publish(n.Ctx, compactData)
}

func (n *NetworkManager) GetAddress() string {
	addrs := n.Host.Addrs()
	if len(addrs) == 0 {
		return ""
	}

	// Cứ đúng giây thứ 0 của mỗi phút mới in ra màn hình (60 giây in 1 lần)
	if time.Now().Second() == 0 {
		log.Printf("[P2P-ADDRS] Thực tế Host đang sở hữu %d địa chỉ: %v", len(addrs), addrs)
	}

	bestAddr := addrs[0]
	for _, addr := range addrs {
		addrStr := addr.String()
		// Tránh địa chỉ loopback
		if strings.Contains(addrStr, "/ip4/") && !strings.Contains(addrStr, "127.0.0.1") {
			bestAddr = addr
		}
		// Ưu tiên cao nhất địa chỉ IPv6 công cộng (không chứa ::1 hoặc fe80)
		if strings.Contains(addrStr, "/ip6/") && !strings.Contains(addrStr, "::1") && !strings.Contains(addrStr, "fe80:") {
			bestAddr = addr
			break
		}
	}

	return fmt.Sprintf("%s/p2p/%s", bestAddr.String(), n.Host.ID().String())
}

func (n *NetworkManager) BroadcastTransaction(txData []byte) error {
	topic, ok := n.TopicMap["txs"]
	if !ok {
		var err error
		topic, err = n.JoinTopic("txs")
		if err != nil {
			return err
		}
	}
	return topic.Publish(n.Ctx, txData)
}

func (n *NetworkManager) BroadcastInventory(height uint64, hash []byte) error {
	topic, _ := n.JoinTopic("btc_genz_v1_inv")
	if topic == nil {
		return fmt.Errorf("INV_TOPIC_NULL")
	}

	msg := &pb_block.InventoryMsg{Height: height, Hash: hex.EncodeToString(hash)}
	data, _ := proto.Marshal(msg)
	return topic.Publish(n.Ctx, data)
}

// [ZK-REMOVAL] BroadcastSOS and BroadcastZKProof were removed

func (n *NetworkManager) StartBlockInbox() {
	n.RegisterHeaderSyncHandler()
	n.RegisterBlockSyncHandler()
	n.RegisterFileChunkSyncHandler()
	n.RegisterManifestSyncHandler()

	// [V1.0 FINAL] Đăng ký bộ xử lý Handshake để trao đổi chiều cao khối
	n.Host.SetStreamHandler(HandshakeProtocol, n.HandleHandshake)

	// [HOTFIX V1.18] THE SILENT DROP PRINCIPLE - Validator ngăn chặn rác truyền tải
	n.PubSub.RegisterTopicValidator("blocks", func(ctx context.Context, id peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		if id == n.Host.ID() {
			return pubsub.ValidationAccept
		}

		var block pb_block.Block
		var compact pb_block.CompactBlock
		if err := proto.Unmarshal(msg.Data, &compact); err == nil && compact.Header != nil {
			block.Header = compact.Header
		} else if err := proto.Unmarshal(msg.Data, &block); err != nil || block.Header == nil {
			n.punishPeer(id, "Dữ liệu Block không hợp lệ hoặc thiếu Header")
			return pubsub.ValidationReject // [SECURITY-FIX] Trừng phạt Peer gửi dữ liệu rác
		}

		// [STATIC-PEERS-FILTER] Kiểm tra chế độ cách ly & lọc khối tĩnh
		if n.BanMgr != nil {
			isolationMode := n.BanMgr.GetIsolationMode()
			isStatic := n.BanMgr.IsStaticPeerID(id)

			// Chế độ 3: Strict Isolation (chặn mọi thứ không phải static)
			// Chế độ 2: Block Trust Mode (chỉ nhận block từ static peers)
			if isolationMode == 3 || isolationMode == 2 {
				if !isStatic {
					log.Printf("[BLOCK-TRUST] 🛡️ Chặn khối #%d từ node lạ %s (Chế độ Block Trust)", block.Header.Height, id.String()[:12])
					return pubsub.ValidationReject
				}
			}

			// Chế độ 1: Anchor Mode
			if isolationMode == 1 {
				// Nếu tất cả static peers đều offline và id gửi khối không phải static peer -> Drop
				if n.BanMgr.IsAllStaticOffline() && !isStatic {
					log.Printf("[ANCHOR-MODE] 🚨 Trạng thái mất kết nối mỏ neo! Chặn khối #%d từ node lạ %s.", block.Header.Height, id.String()[:12])
					return pubsub.ValidationReject
				}
			}
		}

		// [VANGUARD-DIFF-FAST-REJECT] Chốt chặn độ khó tối thiểu (MIN_DIFFICULTY)
		// Tránh việc kẻ tấn công spam khối mồ côi giả mạo với độ khó cực thấp (ví dụ: difficulty = 1)
		if n.minDifficulty == nil {
			n.minDifficulty = big.NewInt(10000000000)
		}
		if n.minDifficulty.Cmp(big.NewInt(10000000000)) == 0 {
			n.UpdateMinDifficultyFromGenesis()
		}
		blockDiff := go_bridge.BytesToBigInt(block.Header.Difficulty)
		if blockDiff.Cmp(n.minDifficulty) < 0 {
			n.punishPeer(id, fmt.Sprintf("Khối #%d có độ khó nhỏ hơn độ khó tối thiểu! (Gửi: %s, MIN: %s)", block.Header.Height, blockDiff.String(), n.minDifficulty.String()))
			return pubsub.ValidationReject
		}

		headerBuf, _ := proto.Marshal(block.Header)

		// [VANGUARD-CHECKPOINT] Kiểm tra mỏ neo lịch sử (Phải check cho mọi khối kể cả mồ côi)
		headerHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, block.Header.Height)

		// [TARGET-HASH-SYNC-FILTER] Kiểm tra xem block có thuộc nhánh đồng bộ chỉ định không
		n.TargetPathMu.RLock()
		if len(n.TargetPathHashes) > 0 {
			blockHashStr := hex.EncodeToString(headerHash)
			if !n.TargetPathHashes[blockHashStr] {
				n.TargetPathMu.RUnlock()
				log.Printf("[TARGET-HASH-SYNC] 🛡️ Chặn khối #%d (%s) vì không nằm trên nhánh đồng bộ chỉ định.", block.Header.Height, blockHashStr[:12])
				return pubsub.ValidationReject
			}
		}
		n.TargetPathMu.RUnlock()

		if !IsValidCheckpoint(block.Header.Height, headerHash) {
			// Tại sao: Chặn và ghi log kiểm toán ngay khi có khối vi phạm mỏ neo lịch sử gửi qua Gossip.
			audit.AuditLog("CHECKPOINT_VIOLATION", id.String()[:12], fmt.Sprintf("Khối #%d vi phạm mỏ neo lịch sử (Gossip)", block.Header.Height))
			n.punishPeer(id, fmt.Sprintf("Vi phạm Mỏ neo lịch sử tại khối #%d (Mã băm giả mạo)", block.Header.Height))
			return pubsub.ValidationReject
		}

		// Xác thực PoW (Chốt chặn năng lượng) - PHẢI CHECK CHO MỌI KHỐI KỂ CẢ MỒ CÔI
		isValid, err := n.Bridge.VerifyPow(headerBuf, block.Header.Nonce, block.Header.Difficulty, block.Header.Height)
		if err != nil {
			if err == go_bridge.ErrCriticalFirewall {
				// Tại sao: Việc phát sóng khối cũ trên Gossip là nỗ lực Reorg bất hợp pháp hoặc spam, log vào kiểm toán.
				audit.AuditLog("GOSSIP_OLD_BLOCK_ATTEMPT", id.String()[:12], fmt.Sprintf("Từ chối khối cũ #%d truyền qua GossipSub", block.Header.Height))
				return pubsub.ValidationIgnore // [VANGUARD-FIX] Tha bổng, chỉ từ chối im lặng, không trừng phạt IP
			}
			log.Printf("[SYSTEM-WARN] ⚠️ Lỗi nội bộ không thể check PoW cho khối #%d: %v. Bỏ qua không ban Peer.", block.Header.Height, err)
			return pubsub.ValidationIgnore // [VANGUARD-FIX] Lỗi nội bộ gRPC -> KHÔNG BAN, CHỈ IGNORE!
		}
		if !isValid {
			n.punishPeer(id, fmt.Sprintf("PoW không hợp lệ tại khối #%d", block.Header.Height))
			return pubsub.ValidationReject
		}

		// 2. Sau khi vượt qua trạm PoW và checkpoint, mới kiểm tra mã băm cha (ParentHash)
		parentHeaderBytes := n.Bridge.GetHeaderRaw(block.Header.ParentHash.Value)
		if parentHeaderBytes == nil {
			log.Printf("[P2P-ORPHAN] 🧩 Nhận khối mồ côi #%d (Cha: %x). Đã vượt qua PoW và checkpoint. Chấp nhận để kích hoạt đồng bộ lùi...", block.Header.Height, block.Header.ParentHash.Value[:8])
			return pubsub.ValidationAccept
		}

		var parentHeader pb_block.BlockHeader
		proto.Unmarshal(parentHeaderBytes, &parentHeader)

		// [Audit S1 FIX] Tường lửa Thời gian (Time Firewall) với quy tắc MTP-11
		mtp := n.Bridge.GetMedianTimePast(block.Header.Height)
		if !n.Bridge.VerifyTimestampFirewall(block.Header.Timestamp, mtp, uint64(time.Now().Unix())) {
			// Tại sao: Tấn công Time-Warp thay đổi thời gian để thao túng độ khó hoặc lịch sử khối, cần ghi vết log kiểm toán.
			audit.AuditLog("TIME_WARP_ATTEMPT", id.String()[:12], fmt.Sprintf("Khối #%d vi phạm tường lửa thời gian MTP-11 (Gossip)", block.Header.Height))
			n.punishPeer(id, fmt.Sprintf("Vi phạm Tường lửa thời gian (MTP-11) tại khối #%d", block.Header.Height))
			return pubsub.ValidationReject
		}

		return pubsub.ValidationAccept
	})

	n.PubSub.RegisterTopicValidator("txs", func(ctx context.Context, id peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		if id == n.Host.ID() {
			return pubsub.ValidationAccept
		}

		// [SECURITY-VANGUARD] Chốt chặn kích thước gói giao dịch tối đa tại lớp mạng
		// Tại sao thiết kế như vậy: Cưỡng chế giới hạn tối đa cho mọi tin nhắn trên topic txs là 5 MB.
		// Lý do nâng cấp: Khi thực hiện stress test với các lô giao dịch lớn (500 giao dịch EBP từ Sàn),
		// kích thước dữ liệu thực tế có thể lên tới 2.2 MB do thông tin chữ ký và metadata.
		// Việc giữ giới hạn cũ 500 KB làm cho các node phạt nhầm peer hợp lệ và gây ra mất kết nối P2P.
		// Đặt giới hạn mới 5 MB (5 * 1024 * 1024) để đảm bảo đồng bộ mượt mà dưới tải trọng stress test cao
		// mà vẫn giữ được lá chắn bảo vệ an ninh chống tin nhắn rác dung lượng khổng lồ.
		maxSize := 5 * 1024 * 1024 // 5 MB
		if len(msg.Data) > maxSize {
			n.punishPeer(id, fmt.Sprintf("Gói giao dịch quá lớn (%d bytes)", len(msg.Data)))
			return pubsub.ValidationReject
		}

		// Giải nén gói giao dịch (hỗ trợ cả gói gộp Magic TXBT và giao dịch đơn lẻ)
		txsBytes := UnpackTransactions(msg.Data)
		if len(txsBytes) == 0 {
			n.punishPeer(id, "Gói giao dịch rỗng")
			return pubsub.ValidationReject
		}

		for _, txData := range txsBytes {
			// [SECURITY-VANGUARD] Chốt chặn kích thước giao dịch đơn lẻ sau giải nén
			if len(txData) > 100*1024 { // 100 KB
				n.punishPeer(id, fmt.Sprintf("Giao dịch đơn lẻ quá lớn (%d bytes)", len(txData)))
				return pubsub.ValidationReject
			}

			var tx pb_block.Transaction
			if err := proto.Unmarshal(txData, &tx); err != nil {
				n.punishPeer(id, "Dữ liệu Giao dịch không hợp lệ")
				return pubsub.ValidationReject
			}

			// [SECURITY-VANGUARD] Chốt chặn phí tối thiểu và Tầng phí (Consensus Rule) - Tự check ở lớp Go để tránh I/O penalty và gRPC timeout
			if tx.Fee != 250 && tx.Fee != 500 && tx.Fee != 1000 {
				n.punishPeer(id, fmt.Sprintf("Phí giao dịch không hợp lệ (%d VNT). Phải là 250, 500 hoặc 1000.", tx.Fee))
				return pubsub.ValidationReject
			}

			// [BẢO MẬT] Xác thực chữ ký bằng Go Native (Chốt chặn phòng thủ Gossip)
			// Tại sao: Ngăn chặn kẻ tấn công phát tán giao dịch giả mạo chữ ký (Signature Bomb DoS) qua mạng P2P,
			// bảo vệ gRPC IPC không bị nghẽn và loại bỏ việc lan truyền giao dịch giả mạo sang các peer khác.
			if !VerifySignatureNative(&tx) {
				n.punishPeer(id, "Giao dịch giả mạo chữ ký (Signature Mismatch)")
				return pubsub.ValidationReject
			}

			// [ANTI-SPAM-STALE] Chốt chặn giao dịch cũ đã thành công trên sổ cái để tránh Gossip Loop
			// Tại sao thiết kế như vậy: Nếu một giao dịch có nonce nhỏ hơn nonce hiện tại của tài khoản trên sổ cái,
			// có nghĩa là giao dịch này đã được đóng gói thành công vào blockchain từ trước.
			// Việc chấp nhận và lan truyền tiếp giao dịch cũ này sẽ tạo ra bão Gossip làm nghẽn CPU và mạng lưới.
			if tx.Sender != nil && n.Bridge != nil {
				senderHex := hex.EncodeToString(tx.Sender.Value)
				n.NonceCacheMu.Lock()
				if n.NonceCache == nil {
					n.NonceCache = make(map[string]uint64)
					n.NonceCacheTime = make(map[string]time.Time)
				}
				cachedVal, exists := n.NonceCache[senderHex]
				cachedTime, timeExists := n.NonceCacheTime[senderHex]
				n.NonceCacheMu.Unlock()

				var dbNonce uint64
				// Tại sao: Cache Nonce trong 2 giây để tránh việc phát tán cùng lúc hàng nghìn giao dịch rác (Gossip flood)
				// kích hoạt validator gọi GetNonce gRPC đơn lẻ liên tục gây nghẽn IPC gRPC của Rust.
				if exists && timeExists && time.Since(cachedTime) < 2*time.Second {
					dbNonce = cachedVal
				} else {
					dbNonce = n.Bridge.GetNonce(nil, tx.Sender.Value)
					n.NonceCacheMu.Lock()
					n.NonceCache[senderHex] = dbNonce
					n.NonceCacheTime[senderHex] = time.Now()
					n.NonceCacheMu.Unlock()
				}

				if tx.Nonce < dbNonce {
					P2PLog("[P2P-TX-VALIDATOR] 🛑 Từ chối giao dịch cũ đã thành công (Nonce: %d, Ledger Nonce: %d) của ví %s", tx.Nonce, dbNonce, senderHex[:8])
					return pubsub.ValidationIgnore // Dùng ValidationIgnore để tránh hạ Peer Score của cụm local
				}
			}
		}

		return pubsub.ValidationAccept
	})

	// [ZK-REMOVAL] Topic validators for zk_proofs and zk_sos were removed

	topic, err := n.JoinTopic("blocks")
	if err != nil || topic == nil {
		log.Printf("[P2P-ERROR] ❌ Không thể Join topic 'blocks': %v", err)
		return
	}
	sub, err := topic.Subscribe()
	if err != nil {
		log.Printf("[P2P-ERROR] ❌ Không thể Subscribe topic 'blocks': %v", err)
		return
	}

	go func() {
		for {
			msg, err := sub.Next(n.Ctx)
			if err != nil {
				return
			}
			if msg.ReceivedFrom == n.Host.ID() {
				continue
			}

			// [P2P-AUDIT] Giám sát tin nhắn đến
			log.Printf("[P2P-AUDIT] 📡 Nhận tin nhắn Gossip (%d bytes) từ Peer %s trên topic 'blocks'", len(msg.Data), msg.ReceivedFrom.String()[:12])

			// 1. Thử giải nén Compact Block
			var compact pb_block.CompactBlock
			if err := proto.Unmarshal(msg.Data, &compact); err == nil && compact.Header != nil {
				log.Printf("[P2P-AUDIT] 🧩 Nhận diện CompactBlock #%d. Đang xác thực...", compact.Header.Height)

				// [NO-TICKER-FIX] Kích hoạt handshake ngay lập tức khi nhận khối mới
				select {
				case n.TriggerHandshakeChan <- struct{}{}:
				default:
				}

				// [EVENT-DRIVEN-SYNC] Phát sự kiện TopicBlockReceived qua GlobalEventBus
				PublishEvent(TopicBlockReceived, []byte{})

				// [ZK-REMOVAL] Logic trích xuất ZK-Proof từ Header đã bị loại bỏ

				n.processCompactBlock(msg.ReceivedFrom, &compact)
			} else {
				// 2. Dự phòng: Xử lý khối đầy đủ
				var block pb_block.Block
				if err := proto.Unmarshal(msg.Data, &block); err == nil && block.Header != nil {
					log.Printf("[P2P-AUDIT] 📦 Nhận diện Full Block #%d. Đang xác thực...", block.Header.Height)

					// [NO-TICKER-FIX] Kích hoạt handshake ngay lập tức khi nhận khối mới
					select {
					case n.TriggerHandshakeChan <- struct{}{}:
					default:
					}

					// [EVENT-DRIVEN-SYNC] Phát sự kiện TopicBlockReceived qua GlobalEventBus
					PublishEvent(TopicBlockReceived, []byte{})

					// [ZK-REMOVAL] Logic trích xuất ZK-Proof từ Header đã bị loại bỏ

					n.SyncEngine.HandleBlockArrival(&block, msg.ReceivedFrom)
				}
			}
		}
	}()

	// Topic cho Giao dịch (Gossip)
	txTopic, err := n.JoinTopic("txs")
	if err != nil || txTopic == nil {
		log.Printf("[P2P-ERROR] ❌ Không thể Join topic 'txs': %v", err)
	} else {
		txSub, _ := txTopic.Subscribe()
		go func() {
			for {
				msg, err := txSub.Next(n.Ctx)
				if err != nil {
					return
				}
				if msg.ReceivedFrom == n.Host.ID() {
					continue
				}

				// [EBP-SEQUENTIAL-BATCH-PROCESSING] Kiểm tra định dạng TXSQ của Sàn trước tiên
				// TẠM THỜI VÔ HIỆU HÓA
				/*
				if len(msg.Data) >= 64 && string(msg.Data[0:4]) == "TXSQ" {
					exchangeAddr, batchId, startNonce, endNonce, txsBytes, err := UnpackSequentialBatch(msg.Data)
					if err == nil {
						exchangeHex := hex.EncodeToString(exchangeAddr)
						P2PLog("[P2P-BATCH] 📥 Nhận lô giao dịch EBP từ Sàn %s (Batch ID: #%d, Nonces: %d -> %d, %d giao dịch) - Đang đẩy vào TxBus...",
							exchangeHex[:8], batchId, startNonce, endNonce, len(txsBytes))
						for _, txData := range txsBytes {
							n.Mempool.PushToTxBus(txData, false)
						}
						continue // Đã xử lý qua EBP Transport, bỏ qua luồng thường
					}
				}
				*/

				// Giải nén gói giao dịch (hỗ trợ cả gói gộp Magic TXBT và giao dịch đơn lẻ)
				txsBytes := UnpackTransactions(msg.Data)

				for _, txData := range txsBytes {
					n.Mempool.PushToTxBus(txData, false)
				}
			}
		}()
	}

	n.startStaticPeersHeartbeat()
}

func (n *NetworkManager) processCompactBlock(from peer.ID, compact *pb_block.CompactBlock) {
	if compact == nil || compact.Header == nil {
		return
	}

	// =====================================================================
	// [BẢN VÁ HIỆU NĂNG - EARLY DROP]
	// Đánh chặn từ vòng gửi xe: Vứt khối ngay lập tức nếu đang nạp Snapshot
	// =====================================================================
	if n.SyncEngine != nil {
		if se, ok := n.SyncEngine.(*SyncEngine); ok {
			se.mu.RLock()
			isBootstrapping := se.bootstrapRunning
			se.mu.RUnlock()

			if isBootstrapping {
				// In log báo hiệu đã tối ưu thành công
				log.Printf("[P2P-OPTIMIZE] 🛡️ Tiết kiệm CPU/RAM: Vứt bỏ khối rút gọn #%d từ %s mà KHÔNG CẦN RÁP, vì hệ thống đang nạp Snapshot.", compact.Header.Height, from.String()[:12])
				
				// Vẫn phải cập nhật đỉnh mạng (Target Height) để sau khi Snapshot xong, Node biết đường đuổi theo
				se.mu.Lock()
				if compact.Header.Height > se.targetHeight {
					se.targetHeight = compact.Header.Height
				}
				se.mu.Unlock()
				
				return // THOÁT NGAY LẬP TỨC! Không tốn thêm 1 byte RAM nào để ráp khối.
			}
		}
	}

	nonce := compact.Header.Nonce

	headerBuf, _ := proto.Marshal(compact.Header)
	headerHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, compact.Header.Height)

	// [VANGUARD-CHECKPOINT] Kiểm tra mỏ neo lịch sử
	if !IsValidCheckpoint(compact.Header.Height, headerHash) {
		// Tại sao: Khối rút gọn vi phạm mỏ neo lịch sử chỉ ra hành vi sai lệch đồng thuận nghiêm trọng.
		audit.AuditLog("CHECKPOINT_VIOLATION", from.String()[:12], fmt.Sprintf("Khối rút gọn #%d vi phạm mỏ neo lịch sử", compact.Header.Height))
		return
	}

	isValid, err := n.Bridge.VerifyPow(headerBuf, nonce, compact.Header.Difficulty, compact.Header.Height)
	if err != nil {
		if err == go_bridge.ErrCriticalFirewall {
			// Tại sao: Chặn đứng nỗ lực đồng bộ khối rút gọn cũ hơn mốc tường lửa.
			audit.AuditLog("GOSSIP_OLD_BLOCK_ATTEMPT", from.String()[:12], fmt.Sprintf("Từ chối khối rút gọn cũ #%d truyền qua GossipSub", compact.Header.Height))
			return
		}
		log.Printf("[SYSTEM-WARN] ⚠️ Lỗi nội bộ không thể check PoW cho khối rút gọn #%d: %v. Bỏ qua không ban Peer.", compact.Header.Height, err)
		return // [VANGUARD-FIX] Lỗi nội bộ gRPC -> KHÔNG BAN, CHỈ IGNORE!
	}
	if !isValid {
		n.punishPeer(from, fmt.Sprintf("PoW invalid for block #%d", compact.Header.Height))
		return
	}

	if compact.CoinbaseTx == nil {
		log.Printf("[P2P-COMPACT] 🛑 Chặn CompactBlock #%d: Thiếu CoinbaseTx (Node cũ hoặc gian lận).", compact.Header.Height)
		n.punishPeer(from, "Invalid CompactBlock: Missing CoinbaseTx")
		return
	}

	shortIDToData, _ := n.Mempool.GetShortIDMap(nonce)
	transactions := make([]*pb_block.Transaction, 0)
	transactions = append(transactions, compact.CoinbaseTx)
	txHashesList := make([][]byte, 0)

	marshalOpts := proto.MarshalOptions{Deterministic: true}
	cbData, _ := marshalOpts.Marshal(compact.CoinbaseTx)
	// [GRPC-STORM-FIX] Tính TxID Coinbase trực tiếp trên RAM bằng Go native Blake3
	cbHash := GetTxIDNative(cbData)
	txHashesList = append(txHashesList, cbHash)

	missingIndexes := make([]uint32, 0)
	for i, sid := range compact.ShortIds {
		if _, ok := shortIDToData[sid]; !ok {
			missingIndexes = append(missingIndexes, uint32(i+1)) // coinbase_tx là index 0
		}
	}

	if len(missingIndexes) > 0 {
		log.Printf("[P2P-COMPACT] 🧩 Thiếu %d giao dịch trong Mempool. Đang yêu cầu các giao dịch thiếu từ Peer %s...", len(missingIndexes), from.String()[:12])

		var ctx context.Context
		if n.SyncEngine != nil {
			if se, ok := n.SyncEngine.(*SyncEngine); ok {
				ctx = se.ctx
			}
		}
		if ctx == nil {
			ctx = context.Background()
		}

		missingTxs, err := n.RequestBlockTxn(ctx, from, headerHash, missingIndexes)

		if err == nil && len(missingTxs) == len(missingIndexes) {
			// Kiểm tra an toàn chống nil transaction để tránh lỗi panic khi lắp ráp và mã hóa
			hasNilTx := false
			for _, tx := range missingTxs {
				if tx == nil {
					hasNilTx = true
					break
				}
			}
			if hasNilTx {
				log.Printf("[P2P-COMPACT-ERROR] ❌ Nhận được giao dịch nil từ Peer %s. Hủy bỏ và fallback tải Full Block theo Hash...", from.String()[:12])
				// [SAFETY-FIX-HASH] Thay thế GetBlockFromNetwork (tải theo Height - gây kẹt hoặc ban nhầm) bằng RequestBlockByHash
				// Giải thích: RequestBlockByHash bảo vệ Node khỏi tải sai khối Canonical của Peer trong trường hợp rẽ nhánh (Fork/Reorg).
				data, err := n.RequestBlockByHash(from, headerHash)
				if err == nil {
					var downloadedBlock pb_block.Block
					if err := proto.Unmarshal(data, &downloadedBlock); err == nil && downloadedBlock.Header != nil {
						log.Printf("[P2P-COMPACT-FALLBACK] 📦 Tải thành công Full Block #%d (sau fallback nil tx). Chuyển giao khối đầy đủ cho SyncEngine...", compact.Header.Height)
						n.SyncEngine.HandleBlockArrival(&downloadedBlock, from)
					}
				}
				return
			}

			log.Printf("[P2P-COMPACT] ✅ Đã nhận đủ %d giao dịch bị thiếu từ Peer %s. Tiến hành lắp ráp khối...", len(missingTxs), from.String()[:12])

			// Bản đồ hóa các giao dịch nhận về theo index bị thiếu
			missingTxMap := make(map[uint32]*pb_block.Transaction)
			for idx, tx := range missingTxs {
				missingTxMap[missingIndexes[idx]] = tx
			}

			// Tái cấu trúc chuỗi giao dịch và mã băm
			var rawTxs [][]byte
			for i, sid := range compact.ShortIds {
				actualIdx := uint32(i + 1)
				if data, ok := shortIDToData[sid]; ok {
					rawTxs = append(rawTxs, data)
				} else {
					tx := missingTxMap[actualIdx]
					txData, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
					rawTxs = append(rawTxs, txData)
				}
			}

			hashes, err := n.Bridge.CalculateTxHashesBatch(rawTxs, compact.Header.Height)
			if err != nil || len(hashes) != len(rawTxs) {
				log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed for reconstruction: %v. Fallback to native.", err)
				hashes = make([][]byte, len(rawTxs))
				for idx, d := range rawTxs {
					hashes[idx] = GetTxIDNative(d)
				}
			}

			for i, sid := range compact.ShortIds {
				actualIdx := uint32(i + 1)
				h := hashes[i]
				txHashesList = append(txHashesList, h)

				if data, ok := shortIDToData[sid]; ok {
					var tx pb_block.Transaction
					proto.Unmarshal(data, &tx)
					transactions = append(transactions, &tx)
				} else {
					tx := missingTxMap[actualIdx]
					transactions = append(transactions, tx)
				}
			}
		} else {
			// [SAFETY-FALLBACK] Nếu xin txn thất bại hoặc thiếu hụt dữ liệu -> Fallback tải Full Block theo Hash
			log.Printf("[P2P-COMPACT-FALLBACK] ⚠️ Lỗi khi xin giao dịch thiếu từ Peer %s (%v). Fallback tải Full Block theo Hash...", from.String()[:12], err)
			// [SAFETY-FIX-HASH] Thay thế GetBlockFromNetwork (tải theo Height - gây kẹt hoặc ban nhầm) bằng RequestBlockByHash
			// Giải thích: RequestBlockByHash bảo vệ Node khỏi tải sai khối Canonical của Peer trong trường hợp rẽ nhánh (Fork/Reorg).
			data, err := n.RequestBlockByHash(from, headerHash)
			if err == nil {
				var downloadedBlock pb_block.Block
				if err := proto.Unmarshal(data, &downloadedBlock); err == nil && downloadedBlock.Header != nil {
					log.Printf("[P2P-COMPACT-FALLBACK] 📦 Tải thành công Full Block #%d (sau fallback xin txn thất bại). Chuyển giao khối đầy đủ cho SyncEngine...", compact.Header.Height)
					n.SyncEngine.HandleBlockArrival(&downloadedBlock, from)
				}
			}
			return
		}
	} else {
		// Không thiếu giao dịch nào -> Tái lập nhanh tại chỗ từ Mempool
		var rawTxs [][]byte
		for _, sid := range compact.ShortIds {
			if data, ok := shortIDToData[sid]; ok {
				rawTxs = append(rawTxs, data)
			}
		}

		hashes, err := n.Bridge.CalculateTxHashesBatch(rawTxs, compact.Header.Height)
		if err != nil || len(hashes) != len(rawTxs) {
			log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed for reconstruction: %v. Fallback to native.", err)
			hashes = make([][]byte, len(rawTxs))
			for idx, d := range rawTxs {
				hashes[idx] = GetTxIDNative(d)
			}
		}

		for i, sid := range compact.ShortIds {
			if data, ok := shortIDToData[sid]; ok {
				var tx pb_block.Transaction
				proto.Unmarshal(data, &tx)
				transactions = append(transactions, &tx)
				txHashesList = append(txHashesList, hashes[i])
			}
		}
	}

	if !n.Bridge.VerifyBlockReconstruction(compact.Header.TxRoot.Value, txHashesList) {
		log.Printf("[P2P-COMPACT] ⚠️ Lệch Tx Root tại khối rút gọn #%d (có thể do mempool lệch pha). Tiến hành tải Full Block từ Peer %s...", compact.Header.Height, from.String()[:12])
		
		data, err := n.RequestBlockByHash(from, headerHash)
		if err == nil {
			var downloadedBlock pb_block.Block
			if err := proto.Unmarshal(data, &downloadedBlock); err == nil && downloadedBlock.Header != nil {
				log.Printf("[P2P-COMPACT-FALLBACK] 📦 Tải thành công Full Block #%d qua fallback lệch Tx Root. Chuyển giao khối đầy đủ cho SyncEngine...", compact.Header.Height)
				n.SyncEngine.HandleBlockArrival(&downloadedBlock, from)
			}
		} else {
			log.Printf("[P2P-COMPACT-ERROR] ❌ Không thể tải Full Block #%d từ Peer %s sau khi lệch Tx Root: %v", compact.Header.Height, from.String()[:12], err)
		}
		return
	}

	fullBlock := &pb_block.Block{Header: compact.Header, Body: &pb_block.BlockBody{Transactions: transactions}}
	log.Printf("[P2P-COMPACT] 🧩 Tái tạo THÀNH CÔNG Khối rút gọn #%d. Chuyển giao khối đầy đủ cho SyncEngine...", compact.Header.Height)
	n.SyncEngine.HandleBlockArrival(fullBlock, from)
}

func (n *NetworkManager) handleBlockProcessing(from peer.ID, data []byte) {
	// [VANGUARD-CONSENSUS] Uỷ quyền 100% cho Rust Core thông qua Giao thức Đồng bộ Tập trung (Sync V4)
	resp, err := n.Bridge.ProcessChain([][]byte{data})
	if err != nil {
		log.Printf("[P2P-ERROR] ❌ Lỗi gRPC ProcessChain: %v", err)
		return
	}

	if resp.Status == 1 { // REORG_SUCCESS
		log.Printf("[P2P-SYNC] ✅ Khối mới từ Gossip đã được chấp nhận. Đỉnh mới: #%d", resp.NewHeight)

		// [VANGUARD-REORG] Thu hồi các giao dịch mồ côi về Mempool
		if len(resp.OrphanedTxsRaw) > 0 {
			log.Printf("[REORG-MEMPOOL] ♻️ Rust trả về %d giao dịch mồ côi. Đang khôi phục...", len(resp.OrphanedTxsRaw))
			for _, txRaw := range resp.OrphanedTxsRaw {
				n.Mempool.PushToTxBus(txRaw, false)
			}
		}

		if resp.NewHeight > n.NetworkHeight {
			n.NetworkHeight = resp.NewHeight
		}

		// [VANGUARD-FIX-STALL] Kích hoạt callback báo hiệu khối mới đã được chốt hạ
		if n.OnBlockCommitted != nil {
			go n.OnBlockCommitted(resp.NewHeight)
		}

	} else if resp.Status == 3 {
		// [GOSSIP-ORPHAN-FIX] Kích hoạt cơ chế xử lý mồ côi từ lớp Gossip
		// thay vì bỏ qua hoàn toàn. Áp dụng cùng logic phân loại lệch chuỗi với syncLoop.
		// Tại sao: Trước đây chỉ in log rồi bỏ qua, khiến toàn bộ cơ chế cân chỉnh chuỗi mồ côi
		// (alignOrphanChain) và CatchUpSync trở nên vô dụng khi khối đến từ Gossip realtime.
		missingHashBytes, _ := hex.DecodeString(resp.MissingParentHash)
		if len(missingHashBytes) != 32 {
			log.Printf("[P2P-ORPHAN] ⚠️ MissingParentHash không hợp lệ (%d bytes). Bỏ qua.", len(missingHashBytes))
			return
		}

		log.Printf("[P2P-ORPHAN] 🧩 Khối mồ côi qua Gossip (Thiếu cha %s). Kích hoạt xử lý...",
			resp.MissingParentHash[:12])

		if se, ok := n.SyncEngine.(*SyncEngine); ok {
			actualH := n.Bridge.GetCurrentVersion()

			// Giải mã khối để lấy Header (chiều cao + tiêu đề thô cho alignOrphanChain)
			var orphanBlock pb_block.Block
			var orphanHeight uint64
			var orphanHeaderRaw []byte
			if err := proto.Unmarshal(data, &orphanBlock); err == nil && orphanBlock.Header != nil {
				orphanHeight = orphanBlock.Header.Height
				orphanHeaderRaw, _ = proto.Marshal(orphanBlock.Header)
			}

			if orphanHeight > actualH && orphanHeight-actualH > 5 {
				// Lệch sâu (> 5 khối): Nhường quyền cho CatchUpSync tải chùm Header từ mốc Finalized
				log.Printf("[P2P-ORPHAN] 🧩 Lệch chuỗi sâu (%d > %d + 5). Kích hoạt CatchUpSync...", orphanHeight, actualH)
				go se.CatchUpSync(from)
			} else {
				// Lệch ngắn (≤ 5 khối): Kích hoạt cân chỉnh chuỗi mồ côi (Recursive Debt Collection)
				log.Printf("[P2P-ORPHAN] 🕵️ Lệch chuỗi ngắn (%d - %d ≤ 5). Kích hoạt cân chỉnh chuỗi mồ côi...", orphanHeight, actualH)

				// [LIGHTWEIGHT-ORPHAN-CACHE] Lưu Header siêu nhẹ vào RAM cache trước khi cân chỉnh chuỗi mồ côi
				if len(orphanHeaderRaw) > 0 && orphanBlock.Header != nil {
					blockHeaderHash := n.Bridge.CalculateBlockHeaderHash(orphanHeaderRaw)
					if len(blockHeaderHash) > 0 {
						hashStr := hex.EncodeToString(blockHeaderHash)
						se.orphanHeadersMu.Lock()
						// Dọn dẹp các khối mồ côi quá hạn trong RAM cache (Height <= actualH)
						for hStr, hdr := range se.orphanHeaders {
							if hdr.Height <= actualH {
								delete(se.orphanHeaders, hStr)
								delete(se.orphanTxIDs, hStr)
								delete(se.orphanCoinbase, hStr)
							}
						}
						se.orphanHeaders[hashStr] = orphanBlock.Header
						// [RECONSTRUCTION-CACHE] Trích xuất và cache TxIDs + Coinbase transaction thô
						if orphanBlock.Body != nil && len(orphanBlock.Body.Transactions) > 0 {
							var rawTxs [][]byte
							for _, tx := range orphanBlock.Body.Transactions {
								txBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(tx)
								rawTxs = append(rawTxs, txBytes)
							}

							hashes, err := n.Bridge.CalculateTxHashesBatch(rawTxs, orphanBlock.Header.Height)
							if err != nil || len(hashes) != len(rawTxs) {
								log.Printf("[P2P-BATCH-WARN] CalculateTxHashesBatch failed for orphan: %v. Fallback to native.", err)
								hashes = make([][]byte, len(rawTxs))
								for idx, d := range rawTxs {
									hashes[idx] = GetTxIDNative(d)
								}
							}
							se.orphanTxIDs[hashStr] = hashes

							// Cache Coinbase transaction bytes
							// [DETERMINISTIC-FIX] Ep buoc dong goi bat bien cho Coinbase transaction
							coinbaseBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(orphanBlock.Body.Transactions[0])
							se.orphanCoinbase[hashStr] = coinbaseBytes
							log.Printf("[P2P-ORPHAN] 🧠 Đã cache %d TxIDs và Coinbase cho khối mồ côi ngắn #%d (%s) trong handleBlockProcessing để tái tạo sau.", len(hashes), orphanBlock.Header.Height, hashStr[:12])
						}
						se.orphanHeadersMu.Unlock()
					}
				}

				// Đăng ký điều tra mồ côi để investigationRoutine giám sát timeout
				missingParentHashStr := resp.MissingParentHash[:12]
				se.orphanMu.Lock()
				se.orphanTracker[missingParentHashStr] = &OrphanInvestigation{
					MissingHash: missingHashBytes,
					Sender:      from,
					LastActive:  time.Now(),
					Height:      orphanHeight - 1,
				}
				se.orphanMu.Unlock()

				// Chạy trong goroutine riêng: alignOrphanChain thực hiện I/O nặng (gRPC + P2P),
				// nếu chạy đồng bộ sẽ block toàn bộ Gossip listener, chặn luôn các khối mới tiếp theo.
				go func() {
					if err := se.alignOrphanChain(from, missingHashBytes, orphanHeaderRaw); err != nil {
						log.Printf("[GOSSIP-ERROR] ❌ Lỗi cân chỉnh chuỗi mồ côi từ Gossip: %v", err)
					}
				}()
			}
		}
	}
}

func (n *NetworkManager) VerifyBlockLight(block *pb_block.Block) (*big.Int, error) {
	if block.Header == nil {
		return nil, fmt.Errorf("thiếu header")
	}

	// [VANGUARD-BOOTSTRAP] Trường hợp đặc biệt cho Khối Genesis (#0)
	if block.Header.Height == 0 {
		hBytes, _ := proto.Marshal(block.Header)
		actualHash := n.Bridge.GetCanonicalBlockHeaderHash(hBytes, 0)

		// [S#3 TRUTH] Nếu khớp Checkpoint thì mặc định tin tưởng Genesis
		if IsValidCheckpoint(0, actualHash) {
			currentWeight := go_bridge.BytesToBigInt(n.Bridge.CalculateAbsoluteWeight(nil, block.Header.Difficulty))
			return currentWeight, nil
		}

		// Nếu không khớp Checkpoint, vẫn thử xác thực PoW như bình thường
		headerBuf, _ := proto.Marshal(block.Header)
		isValidPoW, err := n.Bridge.VerifyPow(headerBuf, block.Header.Nonce, block.Header.Difficulty, 0)
		if err != nil {
			return nil, err
		}
		if !isValidPoW {
			return nil, fmt.Errorf("pow_invalid_genesis")
		}
		currentWeight := go_bridge.BytesToBigInt(n.Bridge.CalculateAbsoluteWeight(nil, block.Header.Difficulty))
		return currentWeight, nil
	}

	// [Satoshi-Style] Tải Header của khối cha từ Rust Core
	parentHeaderBytes := n.Bridge.GetHeaderRaw(block.Header.ParentHash.Value)
	if parentHeaderBytes == nil {
		return nil, fmt.Errorf("mồ côi")
	}

	var parentHeader pb_block.BlockHeader
	proto.Unmarshal(parentHeaderBytes, &parentHeader)

	// 1. Xác thực Bằng chứng công việc (PoW) - Chốt chặn bảo mật tối thượng
	headerBuf, _ := proto.Marshal(block.Header)

	isValidPoW, err := n.Bridge.VerifyPow(headerBuf, block.Header.Nonce, block.Header.Difficulty, block.Header.Height)
	if err != nil {
		return nil, err
	}
	if !isValidPoW {
		log.Printf("[POW-FAIL] H#%d | Nonce: %d | Diff: %x | HeaderBufLen: %d",
			block.Header.Height, block.Header.Nonce, block.Header.Difficulty, len(headerBuf))
		return nil, fmt.Errorf("pow_invalid")
	}

	// 2. Tự tính toán trọng lượng tích lũy mới dựa trên quy tắc đồng thuận chung
	currentWeight := go_bridge.BytesToBigInt(n.Bridge.CalculateAbsoluteWeight(nil, block.Header.Difficulty))
	expectedWeight := new(big.Int).Add(go_bridge.BytesToBigInt(parentHeader.AbsoluteWeight), currentWeight)

	return expectedWeight, nil
}

// CalculateHeaderHashDeterministic: Tạo mã băm Header chuẩn (Vanguard 112-byte + Rust Context)
func (n *NetworkManager) CalculateHeaderHashDeterministic(h *pb_block.BlockHeader) []byte {
	buf, _ := proto.Marshal(h)
	// [VANGUARD-CONSENSUS] Gửi thẳng vào Rust để băm với đúng GENZ_POW_CONTEXT
	return n.Bridge.CalculateBlockHeaderHash(buf)
}

func (n *NetworkManager) VerifyBlockHeavy(from peer.ID, block *pb_block.Block) error {
	parentHeaderBytes := n.Bridge.GetHeaderRaw(block.Header.ParentHash.Value)
	if parentHeaderBytes == nil {
		return fmt.Errorf("parent_not_found")
	}
	var parentHeader pb_block.BlockHeader
	proto.Unmarshal(parentHeaderBytes, &parentHeader)

	// [Audit S1 FIX] Tường lửa Thời gian (Time Firewall) với quy tắc MTP-11
	mtp := n.Bridge.GetMedianTimePast(block.Header.Height)
	if !n.Bridge.VerifyTimestampFirewall(block.Header.Timestamp, mtp, uint64(time.Now().Unix())) {
		// Tại sao: Khối đầy đủ (Heavy Block) vi phạm tường lửa thời gian, ghi nhận sự cố kiểm toán.
		audit.AuditLog("TIME_WARP_ATTEMPT", from.String()[:12], fmt.Sprintf("Khối đầy đủ #%d vi phạm tường lửa thời gian MTP-11 (Heavy)", block.Header.Height))
		return fmt.Errorf("firewall_violation: timestamp_spoofing")
	}

	nonce := block.Header.Nonce
	headerBuf, _ := proto.Marshal(block.Header)
	headerHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, block.Header.Height)

	// [VANGUARD-CHECKPOINT] Kiểm tra mỏ neo lịch sử
	if !IsValidCheckpoint(block.Header.Height, headerHash) {
		// Tại sao: Khối đầy đủ vi phạm checkpoint mỏ neo, ghi nhận sự cố kiểm toán.
		audit.AuditLog("CHECKPOINT_VIOLATION", from.String()[:12], fmt.Sprintf("Khối đầy đủ #%d vi phạm mỏ neo lịch sử", block.Header.Height))
		return fmt.Errorf("firewall_violation: checkpoint_mismatch")
	}

	isValid, err := n.Bridge.VerifyPow(headerBuf, nonce, block.Header.Difficulty, block.Header.Height)
	if err != nil {
		if err == go_bridge.ErrCriticalFirewall {
			// Tại sao: Khối đầy đủ cũ bị từ chối bởi tường lửa.
			audit.AuditLog("GOSSIP_OLD_BLOCK_ATTEMPT", from.String()[:12], fmt.Sprintf("Từ chối khối đầy đủ cũ #%d truyền qua GossipSub", block.Header.Height))
			return err
		}
		return err
	}
	if !isValid {
		return fmt.Errorf("PoW sai")
	}
	return nil
}

func (n *NetworkManager) GetBlockFromNetwork(p peer.ID, h uint64) ([]byte, error) {
	return n.RequestBlockWithContext(n.Ctx, p, h, nil)
}

// [VANGUARD-ORPHAN] RequestBlockByHash hỗ trợ đồng bộ chùm khối cha cho các khối mồ côi
func (n *NetworkManager) RequestBlockByHash(p peer.ID, hash []byte) ([]byte, error) {
	if n.RequestBlockByHashMock != nil {
		return n.RequestBlockByHashMock(p, hash)
	}
	return n.RequestBlockWithContext(n.Ctx, p, 0, hash)
}

func (n *NetworkManager) RequestBlockWithContext(ctx context.Context, p peer.ID, height uint64, hash []byte) ([]byte, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host is nil (mock environment)")
	}
	// [MAINNET-TIMEOUT] Tăng timeout lên 180 giây để đảm bảo tải an toàn các khối dung lượng lớn (lên tới 35MB)
	// từ các Peer ở xa hoặc trong điều kiện mạng chậm, tránh bị hủy giữa chừng do quá thời gian chờ.
	tCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	s, err := n.Host.NewStream(tCtx, p, SyncProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	req := &pb_block.GetBlockRequest{
		Height: height,
		Hash:   hash, // [VANGUARD-ORPHAN] Truyền mã băm nếu có
	}
	reqData, _ := proto.Marshal(req)
	s.Write(reqData)
	s.CloseWrite()

	// [MAINNET-TIMEOUT] Thiết lập Read Deadline lên 180 giây tương thích với timeout tải khối lớn (35MB),
	// tránh ngắt kết nối sớm khi đường truyền mạng P2P bị chậm hoặc nghẽn.
	s.SetReadDeadline(time.Now().Add(180 * time.Second))

	// [V1.5.0 BIG-BLOCK] Nâng cấp bộ đệm nhận khối lên 36MB phù hợp cho chuẩn 35MB tối đa mới
	data, err := io.ReadAll(io.LimitReader(s, 36*1024*1024))
	if err != nil {
		return nil, err
	}
	// [BIG-BLOCK-OOM] Tối ưu hóa bộ nhớ: Trích xuất thô block từ bytes mà không cần Unmarshal toàn bộ response
	blockBytes, found, err := ExtractBlockFromResponseBytes(data)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("không tìm thấy khối (found=false)")
	}
	return blockBytes, nil
}

// [CLEANUP] Đã xóa giao thức Snapshot cũ (SnapshotProtocol) tải toàn bộ file để nhường chỗ cho Sync phân mảnh qua ManifestProtocol và FileChunkProtocol nhằm tối ưu hóa bộ nhớ và băng thông truyền tải.

func (n *NetworkManager) JoinTopic(name string) (*pubsub.Topic, error) {
	if n.PubSub == nil {
		return nil, fmt.Errorf("PubSub chưa được khởi tạo")
	}
	n.TopicMu.Lock()
	defer n.TopicMu.Unlock()

	// [V11.13 HOTFIX] Kiểm tra nhanh xem đã tồn tại chưa để tránh Join lặp lại
	if n.TopicMap != nil {
		if t, ok := n.TopicMap[name]; ok {
			return t, nil
		}
	}

	t, err := n.PubSub.Join(name)
	if err != nil {
		return nil, err
	}

	if n.TopicMap == nil {
		n.TopicMap = make(map[string]*pubsub.Topic)
	}
	n.TopicMap[name] = t
	return t, nil
}

func (n *NetworkManager) UpdatePeerHeight(p peer.ID, h uint64, fh uint64, oh uint64) {
	if n.Host == nil {
		return
	}
	if p == n.Host.ID() {
		return
	}
	n.PeerMutex.Lock()
	defer n.PeerMutex.Unlock()

	oldH := n.PeerHeights[p]
	n.PeerHeights[p] = h

	// [P2P-METADATA-PROTECTION] Tránh ghi đè các mốc Finalized và Oldest hợp lệ bằng 0 
	// khi nhận tin nhắn gossip/inv/block (chỉ truyền height mà không có metadata đầy đủ).
	if fh > 0 || n.PeerFinalizedHeights[p] == 0 {
		n.PeerFinalizedHeights[p] = fh
	}
	if oh > 0 || n.PeerOldestHeights[p] == 0 {
		n.PeerOldestHeights[p] = oh
	}

	if h > oldH {
		log.Printf("[P2P-HEIGHT] 📊 Peer %s: Tip=%d, Finalized=%d, Oldest=%d", p.String()[:12], h, n.PeerFinalizedHeights[p], n.PeerOldestHeights[p])
	}

	if h > n.AbsoluteHeight {
		n.AbsoluteHeight = h
	}
}

func (n *NetworkManager) HandleHandshake(s network.Stream) {
	defer s.Close()
	remotePeer := s.Conn().RemotePeer()
	// Cứ mỗi 30 giây mới in log nhận bắt tay 1 lần để báo hiệu Node vẫn đang giao tiếp
	if time.Now().Second() % 30 == 0 {
		log.Printf("[P2P-HANDSHAKE] 🤝 Nhận yêu cầu bắt tay từ: %s", remotePeer.String()[:12])
	}

	// [VANGUARD-STABILITY] Thiết lập Read Deadline để tránh việc io.ReadAll bị kẹt vô hạn khi bắt tay
	s.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := io.ReadAll(io.LimitReader(s, 2048))
	if err != nil {
		return
	}
	var req pb_block.Handshake
	if err := proto.Unmarshal(data, &req); err != nil {
		log.Printf("[P2P-HANDSHAKE] ❌ Lỗi giải mã: %v", err)
		return
	}

	// [AUDIT-FIX M-2] Kiểm tra Genesis Hash TRƯỚC khi gửi phản hồi
	// Tại sao: Nếu gửi phản hồi trước rồi mới từ chối, Node độc hại vẫn nhận được
	// Height/Finalized/Oldest của ta → lộ thông tin nội bộ không cần thiết
	n.PeerMutex.Lock()
	isLocalEmpty := true
	for _, b := range n.GenesisHash {
		if b != 0 {
			isLocalEmpty = false
			break
		}
	}

	isRemoteEmpty := true
	for _, b := range req.GenesisHash {
		if b != 0 {
			isRemoteEmpty = false
			break
		}
	}

	// [IMMUTABLE-GENESIS] Tuyệt đối không học Genesis qua mạng P2P.
	// Genesis Hash phải được lấy từ ledger cục bộ thông qua bridge.
	if isLocalEmpty {
		gHash := n.Bridge.GetBlockHash(0)
		if len(gHash) > 0 {
			n.GenesisHash = gHash
			isLocalEmpty = false
		}
	}

	localGenesisHash := make([]byte, len(n.GenesisHash))
	copy(localGenesisHash, n.GenesisHash)
	n.PeerMutex.Unlock()

	if !isLocalEmpty && !isRemoteEmpty && !bytes.Equal(req.GenesisHash, localGenesisHash) {
		log.Printf("[P2P-WARN] 🛡️ Từ chối Peer %s: Sai khác Genesis Hash (Mạng khác).", s.Conn().RemotePeer().String()[:12])
		return
	}

	// Chỉ gửi phản hồi SAU khi đã xác nhận Genesis hợp lệ
	currentH := n.Bridge.GetCurrentVersion()
	finalizedH := n.Bridge.GetFinalizedHeight()
	// [VANGUARD-SYNC-FIX] Nếu CurrentVersion=0 (chưa thực thi khối) nhưng có Finality (Seeder nạp thô),
	// hãy sử dụng Finality để báo cáo cao độ mạng.
	if currentH < finalizedH {
		currentH = finalizedH
	}
	oldestH := n.Bridge.GetOldestHeight()
	checkpointH := (currentH / 1152) * 1152

	resp := &pb_block.Handshake{
		CurrentHeight:   currentH,
		GenesisHash:     localGenesisHash,
		FinalizedHeight: checkpointH,
		OldestHeight:    oldestH,
		NatStatus:       n.NatStatus, // [NAT-AUDIT] Thông báo trạng thái NAT cho peer
	}
	rData, _ := proto.Marshal(resp)
	s.Write(rData)
	// [FIX-DOUBLE-CLOSE] Đã có defer s.Close() ở đầu hàm, không gọi Close() thủ công ở đây
	// để tránh double-close gây log error ẩn trên stream.

	n.UpdatePeerHeight(s.Conn().RemotePeer(), req.CurrentHeight, req.FinalizedHeight, req.OldestHeight)

	// [V2.2 SATOSHI-IMMEDIATE-PEX] Chia sẻ ngay địa chỉ sau khi bắt tay thành công (Inbound)
	go n.GossipPeers()
}

func (n *NetworkManager) Bootstrap() error {
	// [V1.0 FINAL] Đăng ký các trình xử lý giao thức P2P
	n.Host.SetStreamHandler(HandshakeProtocol, n.HandleHandshake)
	n.Host.SetStreamHandler(SyncProtocol, n.HandleSyncRequest)

	// [VANGUARD-REORG-FIX] Kích hoạt trình xử lý Header Sync để tìm điểm rẽ nhánh
	n.RegisterHeaderSyncHandler()

	// [FIX] DiscoveryLoop() được gọi tại cli_app.go sau khi Bootstrap() hoàn tất,
	// không gọi ở đây để tránh 2 goroutine chạy song song → race condition Cloudflare DNS.

	// [FIX-INBOUND-HANDSHAKE] Kích hoạt Handshake ngay lập tức khi có Peer mới kết nối vào (Inbound).
	// Tại sao cần: Trước đây node chỉ gửi Handshake định kỳ 10 giây/lần qua triggerHandshakeAll().
	// Nếu Node A dial vào giữa 2 tick, nó có thể phải chờ tới 10 giây mà không được cập nhật PeerHeight.
	// Giải pháp: Lắng nghe sự kiện Connected và gửi Handshake ngược lại ngay lập tức.
	n.Host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(net network.Network, conn network.Conn) {
			remotePeer := conn.RemotePeer()
			log.Printf("[P2P-BOOT] ⚡ Peer mới kết nối vào: %s. Gửi Handshake lập tức...", remotePeer.String()[:12])
			go n.SendHandshake(remotePeer)
		},
	})

	// [V1.0 FINAL] Chu kỳ Handshake định kỳ để cập nhật chiều cao mạng (tăng lên 10 giây để tránh overhead và kết hợp cơ chế Event-Driven)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-n.Ctx.Done():
				return
			case <-ticker.C:
				n.triggerHandshakeAll()
			case <-n.TriggerHandshakeChan:
				n.triggerHandshakeAll()
			}
		}
	}()

	// [VANGUARD-REBROADCAST] Chu kỳ Rebroadcast định kỳ 15 giây để tự động phát sóng lại giao dịch đầu hàng đợi.
	// Tại sao thiết kế như vậy: Để giải quyết lỗi kẹt mempool khi gossip bị thất lạc hoặc khi node khởi động lại bị mất đồng bộ,
	// Node sẽ định kỳ quét mempool và phát sóng lại giao dịch đầu hàng đợi (nonce == ledger nonce) của từng tài khoản.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-n.Ctx.Done():
				return
			case <-ticker.C:
				n.RebroadcastPendingTxs()
			}
		}
	}()

	return nil
}

// [NO-TICKER-FIX] Gửi yêu cầu bắt tay tới toàn bộ các Peer để đồng bộ chiều cao
func (n *NetworkManager) triggerHandshakeAll() {
	peers := n.Host.Network().Peers()
	for _, p := range peers {
		go n.SendHandshake(p)
	}
}

func (n *NetworkManager) SendHandshake(p peer.ID) {
	// Cứ mỗi 30 giây mới in log gửi bắt tay 1 lần
	if time.Now().Second() % 30 == 0 {
		log.Printf("[P2P-HANDSHAKE] 📡 Đang gửi yêu cầu bắt tay tới: %s", p)
	}
	s, err := n.Host.NewStream(n.Ctx, p, HandshakeProtocol)
	if err != nil {
		log.Printf("[P2P-HANDSHAKE] ❌ Lỗi mở luồng tới %s: %v", p, err)
		return
	}
	defer s.Close()

	// [FIX-GOROUTINE-LEAK] Đặt deadline toàn phần 10 giây cho cả Write lẫn Read.
	// Tại sao: Trước đây không có deadline → nếu peer không trả lời, goroutine treo vĩnh viễn.
	// Mỗi lần triggerHandshakeAll() gọi N goroutine, tích lũy đủ sẽ gây memory leak nghiêm trọng.
	s.SetDeadline(time.Now().Add(10 * time.Second))

	currentH := n.Bridge.GetCurrentVersion()
	finalizedH := n.Bridge.GetFinalizedHeight()
	if currentH < finalizedH {
		currentH = finalizedH
	}
	oldestH := n.Bridge.GetOldestHeight()
	checkpointH := (currentH / 1152) * 1152
	n.PeerMutex.RLock()
	localGenesisHash := make([]byte, len(n.GenesisHash))
	copy(localGenesisHash, n.GenesisHash)
	n.PeerMutex.RUnlock()

	// log.Printf("[P2P-HANDSHAKE-DEBUG] 📤 Gửi yêu cầu Handshake tới %s: Height=%d, Checkpoint=%d, Oldest=%d", p, currentH, checkpointH, oldestH)
	req := &pb_block.Handshake{
		Version:         "1.0.0",
		GenesisHash:     localGenesisHash,
		CurrentHeight:   currentH,
		FinalizedHeight: checkpointH,
		OldestHeight:    oldestH,
		Timestamp:       uint64(time.Now().Unix()),
		NatStatus:       n.NatStatus, // [NAT-AUDIT] Thông báo trạng thái NAT cho peer
	}
	data, _ := proto.Marshal(req)
	s.Write(data)
	s.CloseWrite()

	respData, err := io.ReadAll(io.LimitReader(s, 1024))
	if err != nil {
		return
	}
	var resp pb_block.Handshake
	if err := proto.Unmarshal(respData, &resp); err == nil {
		// log.Printf("[P2P-HANDSHAKE-DEBUG] 📥 Nhận phản hồi từ %s: Height=%d, Finalized=%d", p.String()[:12], resp.CurrentHeight, resp.FinalizedHeight)
		// Lý do: Genesis Hash bắt buộc phải được cấu hình tin cậy qua Local Ledger (RocksDB) của Bridge.

		n.UpdatePeerHeight(p, resp.CurrentHeight, resp.FinalizedHeight, resp.OldestHeight)

		// [V2.2 SATOSHI-IMMEDIATE-PEX] Chia sẻ ngay địa chỉ sau khi bắt tay thành công (Outbound)
		go n.GossipPeers()
	}
}
func (n *NetworkManager) GetPeerCount() int { return len(n.Host.Network().Peers()) }
func (n *NetworkManager) GetNetworkHeight() uint64 {
	n.PeerMutex.RLock()
	defer n.PeerMutex.RUnlock()
	max := n.AbsoluteHeight
	for _, h := range n.PeerHeights {
		if h > max {
			max = h
		}
	}
	return max
}
func (n *NetworkManager) GetMempool() MempoolInterface { return n.Mempool }

// [V2.2-RECOVERY] Khôi phục mDNS cho môi trường Lab/LAN
type MdnsHandler struct {
	Host host.Host
}

func (m *MdnsHandler) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == m.Host.ID() {
		return
	}
	log.Printf("[P2P-MDNS] 📢 Tìm thấy Peer mới qua mDNS: %s", pi.ID.String())
	if err := m.Host.Connect(context.Background(), pi); err != nil {
		log.Printf("[P2P-MDNS] ❌ Kết nối thất bại tới %s: %v", pi.ID.String(), err)
	}
}

// [V1.0.1] HandleSyncRequest: Trả lời các yêu cầu lấy khối từ Peer (Cực kỳ quan trọng cho Historical Sync)
func (n *NetworkManager) HandleSyncRequest(s network.Stream) {
	defer s.Close()
	data, err := io.ReadAll(io.LimitReader(s, 1024))
	if err != nil {
		return
	}
	var req pb_block.GetBlockRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		log.Printf("[SYNC-PULL] ❌ Lỗi giải mã yêu cầu từ %s", s.Conn().RemotePeer())
		return
	}

	// [V3.2 DEBUG] Theo dõi sát sao yêu cầu khối 11 hoặc khối mồ côi
	if req.Height == 11 || len(req.Hash) > 0 {
		log.Printf("[SYNC-AUDIT] 🛰️ Nhận yêu cầu Khối #%d (Hash: %x) từ Peer %s. Đang truy xuất Hạt nhân...",
			req.Height, req.Hash, s.Conn().RemotePeer().String()[:12])
	}

	var blockRaw []byte
	if len(req.Hash) == 32 {
		blockRaw = n.Bridge.GetRawByHash(req.Hash)
	} else {
		blockRaw = n.Bridge.GetBlock(req.Height)
	}

	// [V1.0.2 RECONSTRUCTION] Đóng gói vào GetBlockResponse để Client giải mã đúng chuẩn
	resp := &pb_block.GetBlockResponse{}
	if blockRaw == nil {
		resp.Found = false
		if len(req.Hash) == 32 {
			log.Printf("[SYNC-PULL] ❌ Không tìm thấy Khối theo Hash %x trong Rust Core", req.Hash[:8])
		} else {
			log.Printf("[SYNC-PULL] ❌ Không tìm thấy Khối #%d trong Rust Core", req.Height)
		}
	} else {
		var block pb_block.Block
		if err := proto.Unmarshal(blockRaw, &block); err == nil {
			var audited bool
			if len(req.Hash) == 32 {
				// Tự kiểm toán khối rẽ nhánh bằng cách so sánh trực tiếp mã băm header khối đã tính toán lại với req.Hash
				headerBuf, _ := proto.Marshal(block.Header)
				calculatedHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, block.Header.Height)
				if bytes.Equal(req.Hash, calculatedHash) {
					oldestH := n.Bridge.GetOldestHeight()
					if block.Body != nil && len(block.Body.Transactions) > 0 {
						audited = true
					} else if block.Header.Height < oldestH {
						// [PRUNED-BLOCK-EXCEPTION] Cho phép gửi các khối lịch sử đã bị Pruned dưới dạng Header-Only
						audited = true
						log.Printf("[SYNC-PULL] 📦 Cho phép gửi khối %x đã Pruned ở cao độ #%d (Header-Only).", req.Hash[:8], block.Header.Height)
					} else {
						log.Printf("[SYNC-PULL] 🧹 Chặn gửi khối %x do thiếu dữ liệu thân khối.", req.Hash[:8])
					}
				} else {
					log.Printf("[SYNC-PULL] 🛡️ Từ chối gửi Khối %x do băm không trùng khớp (DB: %x, Tính lại: %x)",
						req.Hash[:8], req.Hash[:8], calculatedHash[:8])
				}
			} else {
				// Kiểm toán khối canonical bình thường
				audited = n.AuditBlockBeforeSend(req.Height, &block)
			}

			if audited {
				resp.Found = true
				resp.Block = &block
			} else {
				resp.Found = false
				log.Printf("[SYNC-PULL] 🛡️ Từ chối gửi Khối do vi phạm kiểm toán nội bộ")
			}
		} else {
			resp.Found = false
			log.Printf("[SYNC-PULL] ❌ Lỗi giải mã Khối từ Rust Core")
		}
	}

	resData, _ := proto.Marshal(resp)
	s.Write(resData)
}

// [V2.1 SATOSHI-PEX] HandlePeerExchange xử lý việc nhận danh sách Peer từ đồng đội
func (n *NetworkManager) HandlePeerExchange(s network.Stream) {
	defer s.Close()
	remotePeer := s.Conn().RemotePeer()

	// 1. Rate Limiting: Giới hạn tần suất nhận PEX từ mỗi Peer tối đa 1 lần mỗi 5 giây
	n.PexRequestMu.Lock()
	lastReq, ok := n.LastPexRequest[remotePeer]
	now := time.Now()
	if ok && now.Sub(lastReq) < 5*time.Second {
		n.PexRequestMu.Unlock()
		log.Printf("[PEX-LIMIT] ⚠️ Peer %s gửi PEX quá nhanh. Bỏ qua để chống spam.", remotePeer.String()[:12])
		return
	}
	n.LastPexRequest[remotePeer] = now
	n.PexRequestMu.Unlock()

	data, err := io.ReadAll(io.LimitReader(s, 8192))
	if err != nil {
		return
	}

	// Xác định xem peer gửi kết nối có thuộc IP Public (Internet công cộng) không
	isRemotePublic := false
	remoteAddr := s.Conn().RemoteMultiaddr()
	if ip, err := manet.ToIP(remoteAddr); err == nil {
		if !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() {
			isRemotePublic = true
		}
	}

	peers := strings.Split(string(data), "\n")
	for _, p := range peers {
		if p == "" {
			continue
		}

		// [BẢO MẬT PEX] Lọc địa chỉ rác ngay từ chuỗi raw TRƯỚC khi parse multiaddr
		if strings.Contains(p, "fe80") || strings.Contains(p, "/ip6/::/tcp/") || strings.Contains(p, "/ip6/::/udp/") || strings.Contains(p, "0.0.0.0") {
			continue
		}

		maddr, err := multiaddr.NewMultiaddr(p)
		if err == nil {
			// 2. SSRF Shield: Nếu peer gửi là Public, chặn học IP Private/Localhost/Unspecified
			if isRemotePublic {
				var hasPrivateIP bool
				multiaddr.ForEach(maddr, func(c multiaddr.Component) bool {
					switch c.Protocol().Code {
					case multiaddr.P_IP4, multiaddr.P_IP6:
						ip := net.IP(c.RawValue())
						if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
							hasPrivateIP = true
						}
					}
					return !hasPrivateIP
				})
				if hasPrivateIP {
					continue
				}
			}

			info, _ := peer.AddrInfoFromP2pAddr(maddr)
			if info != nil && info.ID != n.Host.ID() {
				if n.Host.Network().Connectedness(info.ID) != network.Connected {
					// [PEX-IPv6-PRIORITY] Sắp xếp ưu tiên IPv6 trước khi kết nối bằng hàm được test
					info.Addrs = SortAddressesByIPv6Priority(info.Addrs)
					
					// 3. Concurrency Control (Semaphore): Giới hạn tối đa 16 goroutines quay số song song
					go func(pi peer.AddrInfo) {
						select {
						case n.PexSem <- struct{}{}:
							defer func() { <-n.PexSem }()
						default:
							// Hàng đợi đầy, bỏ qua để chống nghẽn và cạn kiệt File Descriptor
							return
						}

						ctx, cancel := context.WithTimeout(n.Ctx, 3*time.Second)
						defer cancel()
						n.Host.Connect(ctx, pi)
					}(*info)
				}
			}
		}
	}
}

// [V2.1 SATOSHI-PEX] GossipPeers chủ động chia sẻ "danh thiếp" cho đồng đội
func (n *NetworkManager) GossipPeers() {
	// [PEX-LIMIT] Chống bão mạng (Broadcast Storm) bằng cách giới hạn tần suất GossipPeers tối đa 1 lần mỗi 15 giây
	n.LastGossipMu.Lock()
	now := time.Now()
	if now.Sub(n.LastGossipTime) < 15*time.Second {
		n.LastGossipMu.Unlock()
		return
	}
	n.LastGossipTime = now
	n.LastGossipMu.Unlock()

	connectedPeers := n.Host.Network().Peers()

	var peerList []string
	// 1. Thu thập địa chỉ của mình (Lọc IP khả dụng)
	for _, addr := range n.Host.Addrs() {
		// [BUGFIX-GOSSIP-IPV6] Sử dụng thư viện manet để kiểm tra địa chỉ Public (Internet), Private (LAN) hoặc Loopback (phục vụ test)
		// nhằm bảo vệ toàn vẹn địa chỉ IPv6 hợp lệ không bị lọc bỏ nhầm bởi logic chuỗi thô.
		if manet.IsPublicAddr(addr) || manet.IsPrivateAddr(addr) || manet.IsIPLoopback(addr) {
			addrStr := addr.String()
			peerList = append(peerList, fmt.Sprintf("%s/p2p/%s", addrStr, n.Host.ID().String()))
		}
	}

	// 2. Thu thập địa chỉ các Peer đang kết nối
	for _, p := range connectedPeers {
		for _, addr := range n.Host.Network().Peerstore().Addrs(p) {
			addrStr := addr.String()
			if strings.Contains(addrStr, "0.0.0.0") {
				addrStr = strings.Replace(addrStr, "0.0.0.0", "127.0.0.1", 1)
			}
			peerList = append(peerList, fmt.Sprintf("%s/p2p/%s", addrStr, p.String()))
		}
	}

	if len(peerList) == 0 {
		return
	}
	// [AUDIT-FIX M-1] Giới hạn tối đa 30 mục để tránh vượt quá LimitReader 8KB ở phía nhận
	// Tại sao: Nếu node có 150 peers × 3-5 multiaddr → payload PEX lên 50-100KB, bị cắt ngắn gây lỗi parse
	const maxPEXEntries = 30
	if len(peerList) > maxPEXEntries {
		peerList = peerList[:maxPEXEntries]
	}
	data := strings.Join(peerList, "\n")

	// 3. Lan truyền cho tối đa 5 đồng đội
	for i, target := range connectedPeers {
		if i > 5 {
			break
		}
		go func(p peer.ID) {
			// [BUGFIX-GOSSIP-LEAK] Thiết lập timeout 5 giây khi kết nối để ngăn ngừa rò rỉ Goroutine khi gặp Peer hỏng
			tCtx, cancel := context.WithTimeout(n.Ctx, 5*time.Second)
			defer cancel()
			s, err := n.Host.NewStream(tCtx, p, protocol.ID(PexProtocol))
			if err == nil {
				defer s.Close()
				s.Write([]byte(data))
			}
		}(target)
	}
}












// [VANGUARD-SELF-AUDIT] AuditBlockBeforeSend: Kiểm tra dữ liệu nội bộ trước khi gửi cho Peer
// Giúp node tự phát hiện lỗi Bit-rot hoặc dữ liệu hỏng trong chính DB của mình.
func (n *NetworkManager) AuditBlockBeforeSend(height uint64, block *pb_block.Block) bool {
	if block == nil || block.Header == nil {
		return false
	}

	// 1. Lấy mã băm chuẩn (Canonical) từ chỉ mục DB
	canonicalHash := n.Bridge.GetBlockHash(height)
	if len(canonicalHash) == 0 {
		log.Printf("[SELF-AUDIT] ⚠️ Cảnh báo: Không tìm thấy mã băm chuẩn cho khối #%d trong chỉ mục.", height)
		return false
	}

	// 2. Tính toán lại mã băm của khối hiện tại
	headerBuf, _ := proto.Marshal(block.Header)
	calculatedHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, height)

	// 3. So sánh
	if !bytes.Equal(canonicalHash, calculatedHash) {
		log.Printf("[SELF-AUDIT-CRITICAL] 🛑 CẢNH BÁO NGUY HIỂM: Dữ liệu cục bộ của Khối #%d bị hỏng! (DB: %x, Tính lại: %x)",
			height, canonicalHash[:8], calculatedHash[:8])
		return false
	}

	// 4. [VANGUARD-FIX] Không gửi khối nếu thân khối rỗng (0 giao dịch) - Đây là tàn dư Snap Sync
	// Khối hợp lệ LUÔN phải có ít nhất 1 giao dịch (Coinbase).
	// [PRUNED-BLOCK-EXCEPTION] Cho phép gửi các khối lịch sử đã bị Pruned dưới dạng Header-Only.
	if block.Body == nil || len(block.Body.Transactions) == 0 {
		oldestH := n.Bridge.GetOldestHeight()
		if height < oldestH {
			log.Printf("[SELF-AUDIT] 📦 Cho phép gửi khối #%d đã Pruned (Header-Only).", height)
			return true
		}
		log.Printf("[SELF-AUDIT] 🧹 Chặn gửi khối #%d do thiếu dữ liệu thân khối (Header-Only placeholder).", height)
		return false
	}

	return true
}

// [VANGUARD-LIGHT-AUDIT] AuditHeaderBeforeSend: Chỉ kiểm tra tính toàn vẹn của Header.
// Dùng cho việc đồng bộ chuỗi mỏ neo (Header Sync) khi chưa có đầy đủ Body.
func (n *NetworkManager) AuditHeaderBeforeSend(height uint64, block *pb_block.Block) bool {
	if block == nil || block.Header == nil {
		return false
	}

	// 1. Lấy mã băm chuẩn (Canonical) từ chỉ mục DB
	canonicalHash := n.Bridge.GetBlockHash(height)
	if len(canonicalHash) == 0 {
		return false
	}

	// 2. Tính toán lại mã băm của Header hiện tại
	headerBuf, _ := proto.Marshal(block.Header)
	calculatedHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBuf, height)

	// 3. So sánh (Chỉ cần Header khớp là đủ cho Header Sync)
	if !bytes.Equal(canonicalHash, calculatedHash) {
		log.Printf("[HEADER-AUDIT-FAIL] 🛑 Mã băm Header khối #%d không khớp!", height)
		return false
	}

	return true
}

// =====================================================================
// [VANGUARD-FILE-SYNC] CƠ CHẾ ĐỒNG BỘ FILE SNAPSHOT MONOLITHIC (BITTORRENT-STYLE)
// Tại sao sử dụng cơ chế này:
//   - Loại bỏ hoàn toàn logic tính Merkle Range Proof phức tạp dễ gây nghẽn ở Rust/Go.
//   - Tải file snapshot phẳng trực tiếp theo từng byte-range (chunk 2MB) giúp tăng tốc độ I/O.
//   - Xác thực tức thời băm Blake3 của từng mảnh giúp chặn đứng dữ liệu giả mạo ngay lập tức.
//   - Hỗ trợ tải tiếp tục (Resume) khi mất kết nối mạng.
// =====================================================================

// RegisterFileChunkSyncHandler phục vụ các phân mảnh (chunk) của tệp snapshot phẳng theo byte-range
func (n *NetworkManager) RegisterFileChunkSyncHandler() {
	log.Printf("[P2P-INIT] 🛠️ Đăng ký bộ xử lý FileChunkProtocol: %s", FileChunkProtocol)
	n.Host.SetStreamHandler(protocol.ID(FileChunkProtocol), func(s network.Stream) {
		defer s.Close()

		// Đọc Request: 8 byte Height + 8 byte Offset + 4 byte ChunkSize
		headerBuf := make([]byte, 20)
		if _, err := io.ReadFull(s, headerBuf); err != nil {
			return
		}

		height := binary.LittleEndian.Uint64(headerBuf[0:8])
		offset := binary.LittleEndian.Uint64(headerBuf[8:16])
		chunkSize := binary.LittleEndian.Uint32(headerBuf[16:20])

		// Chống spam: Giới hạn tối đa 5MB cho mỗi chunk yêu cầu
		if chunkSize > 5*1024*1024 {
			log.Printf("[FILE-CHUNK-HANDLER] ❌ Yêu cầu chunk size quá lớn: %d", chunkSize)
			return
		}

		snapFile := filepath.Join(n.DbPath, "snapshots", fmt.Sprintf("snapshot_%d.bin", height))
		file, err := os.Open(snapFile)
		if err != nil {
			// Trả về header 4 byte 0 báo lỗi hoặc không tìm thấy file
			respHeader := make([]byte, 4)
			s.Write(respHeader)
			return
		}
		defer file.Close()

		// Nhảy tới vị trí offset byte được yêu cầu
		if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
			respHeader := make([]byte, 4)
			s.Write(respHeader)
			return
		}

		buffer := make([]byte, chunkSize)
		bytesRead, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			respHeader := make([]byte, 4)
			s.Write(respHeader)
			return
		}

		// Trả về: 4 byte độ dài thực tế đọc được + Dữ liệu chunk
		respHeader := make([]byte, 4)
		binary.LittleEndian.PutUint32(respHeader, uint32(bytesRead))
		s.Write(respHeader)
		if bytesRead > 0 {
			s.Write(buffer[:bytesRead])
		}
	})
}

// RegisterManifestSyncHandler phục vụ tệp Manifest để kiểm tra chéo StateRoot và danh sách băm Blake3 của các mảnh
func (n *NetworkManager) RegisterManifestSyncHandler() {
	log.Printf("[P2P-INIT] 🛠️ Đăng ký bộ xử lý ManifestProtocol: %s", ManifestProtocol)
	n.Host.SetStreamHandler(protocol.ID(ManifestProtocol), func(s network.Stream) {
		defer s.Close()

		// Đọc Request: 8 byte Height
		reqBuf := make([]byte, 8)
		if _, err := io.ReadFull(s, reqBuf); err != nil {
			return
		}

		height := binary.LittleEndian.Uint64(reqBuf)
		manifestFile := filepath.Join(n.DbPath, "snapshots", fmt.Sprintf("snapshot_%d.manifest", height))

		data, err := os.ReadFile(manifestFile)
		if err != nil {
			log.Printf("[MANIFEST-REJECT] ❌ Không tìm thấy file manifest gốc cho height #%d", height)
			resp := make([]byte, 36)
			s.Write(resp)
			return
		}
		s.Write(data)
	})
}

// DownloadSnapshotManifest tải tệp manifest từ Peer để xác định số mảnh và băm của chúng
func (n *NetworkManager) DownloadSnapshotManifest(p peer.ID, height uint64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(n.Ctx, 15*time.Second)
	defer cancel()

	s, err := n.Host.NewStream(ctx, p, protocol.ID(ManifestProtocol))
	if err != nil {
		return nil, err
	}
	defer s.Close()

	hBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(hBuf, height)
	if _, err := s.Write(hBuf); err != nil {
		return nil, err
	}
	s.CloseWrite()

	// Giới hạn đọc manifest tối đa 10MB
	data, err := io.ReadAll(io.LimitReader(s, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if len(data) < 36 {
		return nil, fmt.Errorf("tệp manifest quá ngắn hoặc không hợp lệ")
	}
	return data, nil
}

// DownloadSnapshotFileChunk tải một mảnh cụ thể của tệp snapshot từ Peer theo byte-range
func (n *NetworkManager) DownloadSnapshotFileChunk(p peer.ID, height uint64, offset uint64, chunkSize uint32) ([]byte, error) {
	ctx, cancel := context.WithTimeout(n.Ctx, 20*time.Second)
	defer cancel()

	s, err := n.Host.NewStream(ctx, p, protocol.ID(FileChunkProtocol))
	if err != nil {
		return nil, err
	}
	defer s.Close()

	reqBuf := make([]byte, 20)
	binary.LittleEndian.PutUint64(reqBuf[0:8], height)
	binary.LittleEndian.PutUint64(reqBuf[8:16], offset)
	binary.LittleEndian.PutUint32(reqBuf[16:20], chunkSize)

	if _, err := s.Write(reqBuf); err != nil {
		return nil, err
	}
	s.CloseWrite()

	// Đọc 4 byte header độ dài thực tế
	respHeader := make([]byte, 4)
	if _, err := io.ReadFull(s, respHeader); err != nil {
		return nil, err
	}
	actualSize := binary.LittleEndian.Uint32(respHeader)
	if actualSize == 0 {
		return nil, fmt.Errorf("phân mảnh không tồn tại hoặc peer gặp lỗi")
	}
	if actualSize > chunkSize {
		return nil, fmt.Errorf("peer gửi mảnh lớn hơn yêu cầu: %d > %d", actualSize, chunkSize)
	}

	data := make([]byte, actualSize)
	if _, err := io.ReadFull(s, data); err != nil {
		return nil, err
	}
	return data, nil
}

// FindAvailablePeers lọc ra danh sách peer an toàn (không bị banned)
func (n *NetworkManager) FindAvailablePeers() []peer.ID {
	peers := n.Host.Network().Peers()
	var available []peer.ID
	for _, p := range peers {
		if p == n.Host.ID() {
			continue
		}
		if n.BanMgr != nil && n.BanMgr.IsPeerBanned(p) {
			continue
		}
		available = append(available, p)
	}
	return available
}

// RebroadcastPendingTxs: Quét mempool và phát sóng lại giao dịch đầu hàng đợi của mỗi ví.
// Tại sao thiết kế như vậy: Để đảm bảo tính đồng bộ của mempool giữa các node, hàm này lấy các giao dịch
// tiếp theo cần được miner đóng gói của từng ví (từ mempool của chính node đó) và bắn thẳng lên kênh P2P Gossip "txs"
// dưới dạng các giao dịch thô (raw protobuf) độc lập, giúp miner tự động nhận lại các giao dịch bị rớt trước đó.
func (n *NetworkManager) RebroadcastPendingTxs() {
	if n.Mempool == nil || n.PubSub == nil {
		return
	}

	txsToPublish := n.Mempool.GetTxsToRebroadcast()
	if len(txsToPublish) == 0 {
		return
	}

	log.Printf("[P2P-REBROADCAST] 📡 Phát hiện %d giao dịch cũ cần phát lại. Tiến hành bắn thẳng...", len(txsToPublish))

	// Duyệt và bắn thẳng từng giao dịch thô độc lập
	for _, txData := range txsToPublish {
		if err := n.BroadcastTransaction(txData); err != nil {
			log.Printf("[P2P-REBROADCAST] ❌ Phát lại thất bại: %v", err)
		}
	}
}

// PackSequentialBatch đóng gói lô giao dịch có thứ tự (EBP - Exchange Batch Protocol) của Sàn
// Định dạng TXSQ:
// - Magic Header (4 bytes): "TXSQ"
// - Exchange ID / Address (32 bytes): Địa chỉ ví của Sàn
// - Batch ID (8 bytes uint64): Mã hiệu lô do sàn đánh dấu
// - Start Nonce (8 bytes uint64): Nonce của giao dịch đầu tiên
// - End Nonce (8 bytes uint64): Nonce của giao dịch cuối cùng
// - Transaction Count (4 bytes uint32): Số lượng giao dịch (tối đa 2500)
// - Payload: Lặp lại [Độ dài TX (4 bytes)] + [Dữ liệu raw TX protobuf]
func PackSequentialBatch(exchangeAddr []byte, batchId uint64, startNonce uint64, endNonce uint64, txsBytes [][]byte) []byte {
	if len(txsBytes) == 0 || len(exchangeAddr) != 32 {
		return nil
	}
	var buf []byte
	// 1. Ghi Magic Header "TXSQ"
	buf = append(buf, []byte("TXSQ")...)

	// 2. Ghi Exchange Address (32 bytes)
	buf = append(buf, exchangeAddr...)

	// 3. Ghi Batch ID (8 bytes uint64)
	batchIdBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(batchIdBytes, batchId)
	buf = append(buf, batchIdBytes...)

	// 4. Ghi Start Nonce (8 bytes uint64)
	startNonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(startNonceBytes, startNonce)
	buf = append(buf, startNonceBytes...)

	// 5. Ghi End Nonce (8 bytes uint64)
	endNonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(endNonceBytes, endNonce)
	buf = append(buf, endNonceBytes...)

	// 6. Ghi Transaction Count (4 bytes uint32)
	count := uint32(len(txsBytes))
	countBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(countBytes, count)
	buf = append(buf, countBytes...)

	// 7. Ghi danh sách giao dịch
	for _, txData := range txsBytes {
		length := uint32(len(txData))
		lengthBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(lengthBytes, length)
		buf = append(buf, lengthBytes...)
		buf = append(buf, txData...)
	}

	return buf
}

// UnpackSequentialBatch giải nén gói lô nhị phân TXSQ của Sàn với đầy đủ metadata
func UnpackSequentialBatch(data []byte) (exchangeAddr []byte, batchId uint64, startNonce uint64, endNonce uint64, txsBytes [][]byte, err error) {
	if len(data) < 64 { // 4 + 32 + 8 + 8 + 8 + 4 = 64 bytes header tối thiểu
		return nil, 0, 0, 0, nil, fmt.Errorf("kích thước lô quá ngắn (%d bytes)", len(data))
	}
	if string(data[0:4]) != "TXSQ" {
		return nil, 0, 0, 0, nil, fmt.Errorf("sai magic header (mong đợi TXSQ)")
	}

	exchangeAddr = make([]byte, 32)
	copy(exchangeAddr, data[4:36])

	batchId = binary.BigEndian.Uint64(data[36:44])
	startNonce = binary.BigEndian.Uint64(data[44:52])
	endNonce = binary.BigEndian.Uint64(data[52:60])
	count := binary.BigEndian.Uint32(data[60:64])

	var result [][]byte
	offset := 64
	for i := uint32(0); i < count; i++ {
		if offset+4 > len(data) {
			break
		}
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if offset+int(length) > len(data) {
			break
		}
		txData := data[offset : offset+int(length)]
		result = append(result, txData)
		offset += int(length)
	}

	return exchangeAddr, batchId, startNonce, endNonce, result, nil
}

// UnpackTransactions giải nén gói nhị phân thành danh sách các giao dịch đơn lẻ (Chỉ giữ lại TXSQ của Sàn được kích hoạt chủ động)
func UnpackTransactions(data []byte) [][]byte {
	if len(data) < 8 {
		return [][]byte{data} // Gói quá ngắn, trả về dữ liệu thô ban đầu
	}
	// TẠM THỜI VÔ HIỆU HÓA TXSQ
	/*
	if string(data[0:4]) == "TXSQ" {
		_, _, _, _, txsBytes, err := UnpackSequentialBatch(data)
		if err == nil {
			return txsBytes
		}
	}
	*/
	return [][]byte{data} // Mặc định là giao dịch đơn lẻ
}

// processSingleTransaction xử lý và nạp một giao dịch đơn lẻ nhận qua P2P vào mempool
func (n *NetworkManager) processSingleTransaction(txData []byte, from peer.ID) {
	if n.Mempool != nil {
		n.Mempool.PushToTxBus(txData, false)
		log.Printf("[P2P-TX] 📥 Nhận giao dịch từ Peer %s và đẩy vào TxBus", from.String()[:12])
	}
}

func decodeVarint(buf []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(buf); i++ {
		b := buf[i]
		v |= uint64(b&0x7F) << (i * 7)
		if b&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

func skipField(wireType uint64, buf []byte) int {
	switch wireType {
	case 0: // Varint
		_, n := decodeVarint(buf)
		return n
	case 1: // Fixed64
		return 8
	case 2: // Length-delimited
		length, n := decodeVarint(buf)
		if n <= 0 {
			return 0
		}
		return n + int(length)
	case 5: // Fixed32
		return 4
	default:
		return 0
	}
}

func ExtractBlockFromResponseBytes(data []byte) ([]byte, bool, error) {
	offset := 0
	var blockBytes []byte
	found := false
	foundFieldParsed := false

	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n <= 0 {
			break
		}
		offset += n

		fieldNum := tag >> 3
		wireType := tag & 7

		if fieldNum == 1 && wireType == 2 { // block (length-delimited)
			length, ln := decodeVarint(data[offset:])
			if ln <= 0 {
				return nil, false, fmt.Errorf("invalid length for block field")
			}
			offset += ln

			if int(length) < 0 || int(length) > len(data)-offset {
				return nil, false, fmt.Errorf("block field length out of bounds")
			}
			start := offset
			end := offset + int(length)
			blockBytes = data[start:end]
			offset = end
		} else if fieldNum == 2 && wireType == 0 { // found (varint)
			val, vn := decodeVarint(data[offset:])
			if vn <= 0 {
				return nil, false, fmt.Errorf("invalid varint for found field")
			}
			offset += vn
			found = (val != 0)
			foundFieldParsed = true
		} else {
			skipLen := skipField(wireType, data[offset:])
			if skipLen <= 0 && wireType == 2 {
				break
			}
			offset += skipLen
		}
	}

	if !foundFieldParsed && len(blockBytes) > 0 {
		found = true
	}

	return blockBytes, found, nil
}

func HasBodyAndTransactions(data []byte) bool {
	offset := 0
	for offset < len(data) {
		tag, n := decodeVarint(data[offset:])
		if n <= 0 {
			break
		}
		offset += n

		fieldNum := tag >> 3
		wireType := tag & 7

		if fieldNum == 2 && wireType == 2 { // body (length-delimited)
			length, ln := decodeVarint(data[offset:])
			if ln <= 0 {
				return false
			}
			offset += ln

			if int(length) < 0 || int(length) > len(data)-offset {
				return false
			}
			end := offset + int(length)
			bodyBytes := data[offset:end]
			bodyOffset := 0
			for bodyOffset < len(bodyBytes) {
				bodyTag, bn := decodeVarint(bodyBytes[bodyOffset:])
				if bn <= 0 {
					break
				}
				bodyOffset += bn

				bodyFieldNum := bodyTag >> 3
				bodyWireType := bodyTag & 7

				if bodyFieldNum == 1 && bodyWireType == 2 { // transactions (repeated)
					return true
				}

				skipLen := skipField(bodyWireType, bodyBytes[bodyOffset:])
				if skipLen <= 0 && bodyWireType == 2 {
					break
				}
				bodyOffset += skipLen
			}
			return false
		} else {
			skipLen := skipField(wireType, data[offset:])
			if skipLen <= 0 && wireType == 2 {
				break
			}
			offset += skipLen
		}
	}
	return false
}

func ExtractHeaderBytesFromBlockBytes(blockBytes []byte) ([]byte, error) {
	offset := 0
	for offset < len(blockBytes) {
		tag, n := decodeVarint(blockBytes[offset:])
		if n <= 0 {
			break
		}
		offset += n

		fieldNum := tag >> 3
		wireType := tag & 7

		if fieldNum == 1 && wireType == 2 { // header (length-delimited)
			length, ln := decodeVarint(blockBytes[offset:])
			if ln <= 0 {
				return nil, fmt.Errorf("invalid length for header field")
			}
			offset += ln

			if int(length) < 0 || int(length) > len(blockBytes)-offset {
				return nil, fmt.Errorf("header field out of bounds")
			}
			end := offset + int(length)
			return blockBytes[offset:end], nil
		} else {
			skipLen := skipField(wireType, blockBytes[offset:])
			if skipLen <= 0 && wireType == 2 {
				break
			}
			offset += skipLen
		}
	}
	return nil, fmt.Errorf("header field not found in block bytes")
}

func ExtractHeightFromHeaderBytes(headerBytes []byte) (uint64, error) {
	offset := 0
	for offset < len(headerBytes) {
		tag, n := decodeVarint(headerBytes[offset:])
		if n <= 0 {
			break
		}
		offset += n

		fieldNum := tag >> 3
		wireType := tag & 7

		if fieldNum == 2 && wireType == 0 { // height (varint)
			val, vn := decodeVarint(headerBytes[offset:])
			if vn <= 0 {
				return 0, fmt.Errorf("invalid varint for height field")
			}
			return val, nil
		} else {
			skipLen := skipField(wireType, headerBytes[offset:])
			if skipLen <= 0 && wireType == 2 {
				break
			}
			offset += skipLen
		}
	}
	return 0, fmt.Errorf("height field not found in header bytes")
}

func ExtractTransactionsFromBlockBytes(blockBytes []byte) ([][]byte, error) {
	offset := 0
	for offset < len(blockBytes) {
		tag, n := decodeVarint(blockBytes[offset:])
		if n <= 0 {
			break
		}
		offset += n

		fieldNum := tag >> 3
		wireType := tag & 7

		if fieldNum == 2 && wireType == 2 { // body (length-delimited)
			length, ln := decodeVarint(blockBytes[offset:])
			if ln <= 0 {
				return nil, fmt.Errorf("invalid length for body field")
			}
			offset += ln

			if int(length) < 0 || int(length) > len(blockBytes)-offset {
				return nil, fmt.Errorf("body field out of bounds")
			}
			end := offset + int(length)
			bodyBytes := blockBytes[offset:end]
			bodyOffset := 0
			var txs [][]byte
			for bodyOffset < len(bodyBytes) {
				bodyTag, bn := decodeVarint(bodyBytes[bodyOffset:])
				if bn <= 0 {
					break
				}
				bodyOffset += bn

				bodyFieldNum := bodyTag >> 3
				bodyWireType := bodyTag & 7

				if bodyFieldNum == 1 && bodyWireType == 2 { // transactions
					txLen, txn := decodeVarint(bodyBytes[bodyOffset:])
					if txn <= 0 {
						return nil, fmt.Errorf("invalid length for transaction")
					}
					bodyOffset += txn
					if int(txLen) < 0 || int(txLen) > len(bodyBytes)-bodyOffset {
						return nil, fmt.Errorf("transaction out of bounds")
					}
					txEnd := bodyOffset + int(txLen)
					txs = append(txs, bodyBytes[bodyOffset:txEnd])
					bodyOffset = txEnd
				} else {
					skipLen := skipField(bodyWireType, bodyBytes[bodyOffset:])
					if skipLen <= 0 && bodyWireType == 2 {
						break
					}
					bodyOffset += skipLen
				}
			}
			return txs, nil
		} else {
			skipLen := skipField(wireType, blockBytes[offset:])
			if skipLen <= 0 && wireType == 2 {
				break
			}
			offset += skipLen
		}
	}
	return nil, fmt.Errorf("body field not found in block bytes")
}

func encodeVarintBytes(v uint64) []byte {
	var buf [10]byte
	i := 0
	for v >= 0x80 {
		buf[i] = byte(v | 0x80)
		v >>= 7
		i++
	}
	buf[i] = byte(v)
	return buf[:i+1]
}

func BuildHeaderOnlyResponseBytes(headerBytes []byte) []byte {
	// blockBytes = [0x0a] + [len(headerBytes)] + [headerBytes]
	hLenBytes := encodeVarintBytes(uint64(len(headerBytes)))
	var blockBytes []byte
	blockBytes = append(blockBytes, 0x0a)
	blockBytes = append(blockBytes, hLenBytes...)
	blockBytes = append(blockBytes, headerBytes...)

	// responseBytes = [0x0a] + [len(blockBytes)] + [blockBytes] + [0x10] + [0x01] (found = true)
	bLenBytes := encodeVarintBytes(uint64(len(blockBytes)))
	var respBytes []byte
	respBytes = append(respBytes, 0x0a)
	respBytes = append(respBytes, bLenBytes...)
	respBytes = append(respBytes, blockBytes...)
	respBytes = append(respBytes, 0x10, 0x01)
	return respBytes
}

// AuditHeaderBytesBeforeSend thực hiện kiểm toán trực tiếp trên dữ liệu byte của header để tránh unmarshal
func (n *NetworkManager) AuditHeaderBytesBeforeSend(height uint64, headerBytes []byte) bool {
	if len(headerBytes) == 0 {
		return false
	}
	canonicalHash := n.Bridge.GetBlockHash(height)
	if len(canonicalHash) == 0 {
		return false
	}
	calculatedHash := n.Bridge.GetCanonicalBlockHeaderHash(headerBytes, height)
	return bytes.Equal(canonicalHash, calculatedHash)
}

func (n *NetworkManager) startStaticPeersHeartbeat() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		var lastStaticStatus bool = true // Mặc định coi như online

		for {
			select {
			case <-n.Ctx.Done():
				return
			case <-ticker.C:
				if n.BanMgr == nil {
					continue
				}

				staticPeers := n.BanMgr.GetStaticPeers()
				if len(staticPeers) == 0 {
					n.BanMgr.SetAllStaticOffline(false)
					continue
				}

				anyOnline := false
				for _, sp := range staticPeers {
					pid, err := peer.Decode(sp.ID)
					if err != nil {
						continue
					}

					// Kiểm tra xem đã kết nối chưa
					connected := n.Host.Network().Connectedness(pid) == network.Connected
					if !connected {
						// Thử kết nối lại
						ma, err := multiaddr.NewMultiaddr(sp.Address)
						if err == nil {
							addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
							if err == nil {
								ctx, cancel := context.WithTimeout(n.Ctx, 3*time.Second)
								if err := n.Host.Connect(ctx, *addrInfo); err == nil {
									connected = true
								}
								cancel()
							}
						}
					}

					if connected {
						anyOnline = true
					}
				}

				n.BanMgr.SetAllStaticOffline(!anyOnline)

				isolationMode := n.BanMgr.GetIsolationMode()

				// Chế độ 1: Anchor Mode
				if isolationMode == 1 {
					if !anyOnline {
						if lastStaticStatus {
							log.Printf("[ANCHOR-MODE] 🚨 MẤT KẾT NỐI TOÀN BỘ NODE TĨNH! Dừng đào, dừng nhận khối, chỉ nhận giao dịch.")
							n.Bridge.SetMiningPause(true)
							if n.SyncEngine != nil {
								if se, ok := n.SyncEngine.(*SyncEngine); ok {
									se.mu.Lock()
									se.state = Stalled
									se.mu.Unlock()
								}
							}
							lastStaticStatus = false
						}
					} else {
						if !lastStaticStatus {
							log.Printf("[ANCHOR-MODE] ⚓ Đã kết nối lại được với ít nhất một Node tĩnh. Mở lại tiến trình đồng bộ.")
							
							// Tự động bật lại miner nếu chế độ ban đầu là full-mining
							if cfgData, err := n.Bridge.GetNodeConfig(); err == nil && len(cfgData) > 0 {
								var cfg struct {
									NodeMode string `json:"node_mode"`
								}
								if json.Unmarshal(cfgData, &cfg) == nil && cfg.NodeMode == "full-mining" {
									log.Println("[ANCHOR-MODE] ⚓ Khôi phục tiến trình đào (Chế độ cấu hình: full-mining).")
									n.Bridge.SetMiningPause(false)
								}
							}

							if n.SyncEngine != nil {
								if se, ok := n.SyncEngine.(*SyncEngine); ok {
									se.mu.Lock()
									se.state = Syncing
									se.mu.Unlock()
									se.TriggerSync()
								}
							}
							lastStaticStatus = true
						}
					}
				}
			}
		}
	}()
}

func (n *NetworkManager) SelectBestPeer(candidates []peer.ID) peer.ID {
	if len(candidates) == 0 {
		return ""
	}

	if n.BanMgr != nil {
		staticPeers := n.BanMgr.GetStaticPeers()
		if len(staticPeers) > 0 {
			var bestStatic peer.ID
			bestPriority := 999999

			candidateSet := make(map[peer.ID]bool)
			for _, c := range candidates {
				candidateSet[c] = true
			}

			for _, sp := range staticPeers {
				pid, err := peer.Decode(sp.ID)
				if err == nil && candidateSet[pid] {
					if n.Host.Network().Connectedness(pid) == network.Connected {
						if sp.Priority < bestPriority {
							bestPriority = sp.Priority
							bestStatic = pid
						}
					}
				}
			}
			if bestStatic != "" {
				return bestStatic
			}
		}
	}

	// Fallback: chọn peer có chiều cao lớn nhất
	var bestFallback peer.ID
	maxHeight := uint64(0)
	n.PeerMutex.RLock()
	for _, c := range candidates {
		h := n.PeerHeights[c]
		if h >= maxHeight {
			maxHeight = h
			bestFallback = c
		}
	}
	n.PeerMutex.RUnlock()

	if bestFallback != "" {
		return bestFallback
	}

	return candidates[0]
}


