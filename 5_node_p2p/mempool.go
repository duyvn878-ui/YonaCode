package node_p2p

import (
	pb_tx "btc_genz/proto"
	"container/heap"
	"context"
	"encoding/hex"

	"sync"
	"time"

	"btc_genz/2_miner_core/go_bridge"
	"log"
	"sort"

	"google.golang.org/protobuf/proto"

	"btc_genz/6_user_interface/i18n"
)

const (
	MinTransactionFee    = 250          // VNT (Phí tối thiểu để chống Spam)
	MaxTransactionSize   = 100 * 1024   // 100 KB (Giới hạn kích thước giao dịch)
	MinTransactionAmount = 100          // VNT (Chống bụi - Dust Protection)

	// [ANTI-SPAM-CONFIG] Hằng số cấu hình Mempool và Phí nhằm tránh các giá trị Magic Numbers
	MempoolMaxBytes         = 150 * 1024 * 1024 // 150 MB (Giới hạn dung lượng Mempool)
	MempoolEvictThreshold   = 5000              // Số lượng giao dịch cần giải phóng khi mempool chạm ngưỡng tối đa
	CongestionThresholdHigh = 5000             // Ngưỡng tắc nghẽn cao để áp dụng mức phí VIP
	CongestionThresholdLow  = 1000             // Ngưỡng tắc nghẽn thấp để áp dụng mức phí Priority

	FeeVIP      = 1000 // Phí VIP (VNT)
	FeePriority = 500  // Phí Ưu tiên (VNT)
	FeeStandard = 250  // Phí Tiêu chuẩn (VNT)
)

type mempoolOpType int
const (
	opAdd mempoolOpType = iota
	opRemove
)

type mempoolPersistOp struct {
	opType mempoolOpType
	txHash []byte
	txRaw  []byte
}

type Mempool struct {
	mu             sync.RWMutex
	pendingTxs     map[string][]byte
	txTimestamp    map[string]time.Time
	pendingSpend   map[string]uint64
	txBySender     map[string][]*txItem
	txItems        map[string]*txItem // [OPTIMIZATION] Cache txItem by hash for O(1) lookups in GetPendingTxList
	txSender       map[string]string // [V6.8] Cache Sender cho UI Tracker
	txReceiver     map[string]string // [V6.8] Cache Receiver cho UI Tracker
	projectedNonce map[string]uint64 // [V5.0] Theo dõi Nonce dự phóng cho Concurrency
	totalBytes     uint64            // [VANGUARD-OPTIMIZATION] Tổng dung lượng byte của các giao dịch trong Mempool
	
	recentBlockHashes   map[string]uint64 // [VANGUARD-OPTIMIZATION] Cache Block Hash trên RAM để chống DoS gRPC Storm
	recentBlockHashesMu sync.RWMutex      // Khóa bảo vệ bộ nhớ đệm cache Block Hash
	lastCacheHeight     uint64            // Đỉnh cao khối tại thời điểm làm mới cache

	bridge       BridgeInterface
	baseFee      uint64
	OnUpdate     func()

	// [2-SECOND-BUS] RAM Channel TxBus chứa các giao dịch đang chờ xe buýt gom
	TxBus            chan []byte
	// [2-SECOND-BUS] Callback cập nhật UI Tracker in-memory và đối soát trạng thái giao dịch cho cả lô
	OnTxBatchValidated func(results []TxValidatedResult)

	// [ASYNC-PERSISTENCE] Kênh đồng bộ mempool vật lý xuống Rust Core (RocksDB)
	// Tại sao thiết kế như vậy: Để tránh việc gọi gRPC đồng bộ (chậm và có độ trễ) trong khi đang giữ
	// khóa ghi mempool.mu.Lock(), giúp giải phóng hoàn toàn luồng chính và luồng miner.
	persistChan chan mempoolPersistOp

	// MaxTxsPerBlock cấu hình số giao dịch tối đa đóng gói vào một khối (CLI configurable)
	MaxTxsPerBlock int
}

func NewMempool(bridge BridgeInterface, fee uint64) *Mempool {
	m := &Mempool{
		pendingTxs:        make(map[string][]byte),
		txTimestamp:       make(map[string]time.Time),
		pendingSpend:      make(map[string]uint64),
		txBySender:        make(map[string][]*txItem),
		txItems:           make(map[string]*txItem),
		txSender:          make(map[string]string),
		txReceiver:        make(map[string]string),
		projectedNonce:    make(map[string]uint64),
		recentBlockHashes: make(map[string]uint64), // Khởi tạo cache
		bridge:            bridge,
		baseFee:           fee,
		totalBytes:        0,
		TxBus:             make(chan []byte, 625000), // Sức chứa 625k giao dịch trên RAM (Đồng bộ với dung lượng 150 MB)
		persistChan:       make(chan mempoolPersistOp, 625000), // Đồng bộ sức chứa với TxBus
		MaxTxsPerBlock:    1000, // Giá trị mặc định ban đầu là 1000 giao dịch/khối
	}
	// [V1.50] Khôi phục Mempool từ Rust Core (RocksDB) thay vì JSON cũ
	m.loadMempoolFromRust()
	
	go m.StartMonitor()
	// [ANTI-SPAM-P2P] Kích hoạt luồng dọn dẹp mempool định kỳ cho toàn bộ các Node ngầm
	go m.StartEvictionWorker(context.Background())
	// [2-SECOND-BUS] Kích hoạt luồng xe buýt ngầm định kỳ 2s gom giao dịch
	go m.StartTxBus(context.Background())

	// Khởi động 8 worker đồng bộ hóa RocksDB bất đồng bộ nhằm giới hạn số lượng gRPC đồng thời
	// tránh làm quá tải Rust Core gRPC server.
	for i := 0; i < 8; i++ {
		go m.startPersistWorker()
	}
	return m
}

func (m *Mempool) startPersistWorker() {
	var addBatch []mempoolPersistOp
	var removeBatch []mempoolPersistOp
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case op, ok := <-m.persistChan:
			if !ok {
				// Tại sao: Kênh truyền bị đóng, flush nốt các giao dịch còn lại để tránh mất mát dữ liệu mempool
				if len(addBatch) > 0 {
					m.flushAddBatch(addBatch)
				}
				if len(removeBatch) > 0 {
					m.flushRemoveBatch(removeBatch)
				}
				return
			}
			if op.opType == opAdd {
				addBatch = append(addBatch, op)
				if len(addBatch) >= 1000 {
					m.flushAddBatch(addBatch)
					addBatch = make([]mempoolPersistOp, 0, 1000)
				}
			} else if op.opType == opRemove {
				// [GRPC-STORM-FIX] Gom lô lệnh xóa thay vì gọi gRPC đơn lẻ cho từng TX
				// Tại sao: Khi commit block chứa hàng ngàn TX, việc gọi RemoveFromMempool tuần tự
				// tạo ra bão gRPC nghiêm trọng. Gom lô giảm từ N roundtrips xuống còn ceil(N/1000).
				removeBatch = append(removeBatch, op)
				if len(removeBatch) >= 1000 {
					m.flushRemoveBatch(removeBatch)
					removeBatch = make([]mempoolPersistOp, 0, 1000)
				}
			}
		case <-ticker.C:
			// Tại sao: Nếu quá 100ms mà chưa đủ 1.000 giao dịch thì vẫn flush để giảm thiểu thời gian trễ giao dịch
			if len(addBatch) > 0 {
				m.flushAddBatch(addBatch)
				addBatch = make([]mempoolPersistOp, 0, 1000)
			}
			if len(removeBatch) > 0 {
				m.flushRemoveBatch(removeBatch)
				removeBatch = make([]mempoolPersistOp, 0, 1000)
			}
		}
	}
}


func (m *Mempool) flushAddBatch(batch []mempoolPersistOp) {
	if m.bridge == nil || len(batch) == 0 {
		return
	}
	hashes := make([][]byte, len(batch))
	raws := make([][]byte, len(batch))
	for i, op := range batch {
		hashes[i] = op.txHash
		raws[i] = op.txRaw
	}
	// Tại sao: Gọi API gom lô AddBatchToMempool của Bridge để Rust Core xử lý trong 1 RocksDB WriteBatch duy nhất
	_, err := m.bridge.AddBatchToMempool(hashes, raws)
	if err != nil {
		P2PLog("[MEMPOOL-ERROR] ❌ Lỗi lưu trữ lô mempool xuống RocksDB: %v", err)
	}
}

// flushRemoveBatch gom lô xóa mempool RocksDB để giảm bão gRPC.
// Tại sao thiết kế như vậy: Sử dụng gRPC RemoveFromMempoolBatch để xóa hàng loạt giao dịch khỏi RocksDB
// trong đúng một cuộc gọi gRPC duy nhất thay vì chạy vòng lặp tuần tự, giải quyết triệt để bão gRPC dưới tải cao.
func (m *Mempool) flushRemoveBatch(batch []mempoolPersistOp) {
	if m.bridge == nil || len(batch) == 0 {
		return
	}
	hashes := make([][]byte, len(batch))
	for i, op := range batch {
		hashes[i] = op.txHash
	}
	_, err := m.bridge.RemoveFromMempoolBatch(hashes)
	if err != nil {
		P2PLog("[MEMPOOL-ERROR] ❌ Lỗi xóa lô mempool khỏi RocksDB: %v", err)
	}
}

// StartMonitor giám sát và in thống kê mempool định kỳ.
func (m *Mempool) StartMonitor() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		m.mu.RLock()
		count := len(m.pendingTxs)
		senderCount := len(m.txBySender)
		m.mu.RUnlock()
		
		if count > 0 {
			P2PLog("[MEMPOOL-STATS] 📊 Đang giữ %d giao dịch từ %d địa chỉ người gửi.", count, senderCount)
		}
	}
}

func (m *Mempool) SetOnUpdate(f func()) {
	m.OnUpdate = f
}

// [V1.3.2 - MEMPOOL MODEL] Đào thải thông minh khi Mempool đầy
func (m *Mempool) PerformCapacityEviction() {
	// Không cần Lock ở đây vì hàm này ĐÃ ĐƯỢC GỌI BÊN TRONG Lock của hàm Add()
	
	P2PLog("[MEMPOOL-EVICTION] 🚨 Mempool chạm ngưỡng quá tải dung lượng (vượt quá 150MB). Kích hoạt Cắt đuôi (Tail Eviction)...")

	// 1. Tìm tất cả các Sender đang có giao dịch, đếm số lượng
	type senderInfo struct {
		sender string
		count  int
	}
	var spammers []senderInfo
	for sender, txs := range m.txBySender {
		spammers = append(spammers, senderInfo{sender, len(txs)})
	}

	// 2. Sắp xếp: Kẻ nào đang chiếm nhiều chỗ nhất xếp lên đầu, tie-breaker bằng địa chỉ ví
	sort.Slice(spammers, func(i, j int) bool {
		if spammers[i].count == spammers[j].count {
			return spammers[i].sender < spammers[j].sender
		}
		return spammers[i].count > spammers[j].count
	})

	toRemove := make([]string, 0, MempoolEvictThreshold)
	neededToFree := MempoolEvictThreshold // Cần giải phóng số lượng slot theo ngưỡng cấu hình

	// 3. Cắt Đuôi (Cắt Nonce cao nhất xuống) của các Spammer
	for _, spammer := range spammers {
		if neededToFree <= 0 {
			break
		}

		txs := m.txBySender[spammer.sender]
		// Cắt tối đa 20% lượng TX của spammer này từ dưới lên (từ ngọn/nonce cao nhất)
		cutCount := len(txs) / 5 
		if cutCount == 0 {
			cutCount = 1
		}
		if cutCount > neededToFree {
			cutCount = neededToFree
		}

		// Lấy các hash ở phía đuôi mảng (Nonce cao nhất vì mảng đã được sort)
		startCutIdx := len(txs) - cutCount
		for i := startCutIdx; i < len(txs); i++ {
			toRemove = append(toRemove, txs[i].hash)
		}

		neededToFree -= cutCount
	}

	// 4. Xóa thực tế bằng hàm nội bộ (không bị dính Deadlock)
	m.removeTransactionsLocked(toRemove)

	// 5. Gửi lệnh xóa RocksDB ra persistChan
	for _, hash := range toRemove {
		hBytes, _ := hex.DecodeString(hash)
		select {
		case m.persistChan <- mempoolPersistOp{opType: opRemove, txHash: hBytes}:
		default:
		}
	}

	P2PLog("[MEMPOOL-EVICTION] %s", i18n.T("log_mempool_eviction", len(toRemove)))
}

// PendingTxInfo: Thông tin giao dịch pending cho UI Tx Tracker
// Tại sao cần struct riêng: Tách biệt dữ liệu UI khỏi logic Mempool nội bộ (EISD)
type PendingTxInfo struct {
	Hash      string
	Sender    string
	Receiver  string
	Amount    uint64
	Fee       uint64
	Timestamp int64
	Nonce     uint64
}

func (m *Mempool) GetPendingSpend(senderHex string) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cacheKey := senderHex + "_0"
	return m.pendingSpend[cacheKey]
}

// getNextNonceLocked tính toán nonce tuần tự tiếp theo không có khoảng trống
func (m *Mempool) getNextNonceLocked(senderHex string, currentNonce uint64) uint64 {
	pNonce := currentNonce
	if txs, ok := m.txBySender[senderHex]; ok && len(txs) > 0 {
		expected := currentNonce
		for _, tx := range txs {
			if tx.nonce < currentNonce {
				continue
			}
			if tx.nonce == expected {
				expected++
			} else if tx.nonce > expected {
				break
			}
		}
		pNonce = expected
	}
	return pNonce
}

func (m *Mempool) GetNextNonce(senderHex string, currentNonce uint64) uint64 {
	// Lấy và giữ chỗ 1 nonce. Tại sao: Để tương thích ngược với API giao dịch đơn lỻ.
	return m.GetAndReserveNonces(senderHex, currentNonce, 1)
}

func (m *Mempool) GetAndReserveNonces(senderHex string, currentNonce uint64, count uint64) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	pNonce := m.getNextNonceLocked(senderHex, currentNonce)
	m.projectedNonce[senderHex] = pNonce + count
	return pNonce
}
func (m *Mempool) GetExpectedNonce(senderHex string, currentNonce uint64) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	pNonce := m.getNextNonceLocked(senderHex, currentNonce)
	if proj, ok := m.projectedNonce[senderHex]; ok {
		if proj > pNonce {
			pNonce = proj
		}
	}
	return pNonce
}

// GetPendingTxList: Trả về danh sách giao dịch pending cho RPC Tx Tracker
// Tại sao: RPC Server cần đồng bộ giao dịch mempool vào in-memory tracker để hiển thị trên UI
func (m *Mempool) GetPendingTxList() []PendingTxInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := 100
	result := make([]PendingTxInfo, 0, 100)
	count := 0
	
	// [MEMPOOL-PENDING-CACHE] Sử dụng cache index txItems trên RAM thay vì Unmarshal Protobuf dưới Lock
	// Tại sao thiết kế như vậy: Để loại bỏ hoàn toàn các cuộc gọi proto.Unmarshal nặng nề về CPU dưới khóa đọc mempool,
	// giúp tăng tốc độ phản hồi API RPC phục vụ giao diện người dùng (UI) và giảm tranh chấp lock với các tiến trình khác.
	for txHash := range m.pendingTxs {
		if count >= limit {
			break
		}
		item, exists := m.txItems[txHash]
		if !exists || item == nil {
			continue
		}

		senderHex := item.sender
		receiverHex := m.txReceiver[txHash]

		ts := int64(0)
		if t, ok := m.txTimestamp[txHash]; ok {
			ts = t.Unix()
		}

		result = append(result, PendingTxInfo{
			Hash:      txHash,
			Sender:    senderHex,
			Receiver:  receiverHex,
			Amount:    item.amount,
			Fee:       item.fee,
			Timestamp: ts,
			Nonce:     item.nonce,
		})
		count++
	}

	// [VANGUARD-SORT] Sắp xếp tuần tự tuyệt đối theo thứ tự Nonce tăng dần đối với mỗi Sender
	// Điều này giúp hàng chờ mempool hiển thị trên UI hoàn toàn chuẩn xác và không bị lộn xộn.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Sender == result[j].Sender {
			return result[i].Nonce < result[j].Nonce
		}
		return result[i].Timestamp < result[j].Timestamp
	})
	return result
}

	

func (m *Mempool) GetRecommendedFee(amount uint64) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := len(m.pendingTxs)

	if count > CongestionThresholdHigh {
		return FeeVIP
	}
	if count > CongestionThresholdLow {
		return FeePriority
	}

	return FeeStandard
}

// [V1.0 FINAL] Lấy bản đồ Short ID để lắp ráp Compact Block (BIP152)
// Đã tối ưu O(1) giải mã - Không cần Unmarshal lại Protobuf trong khi giữ RLock để giảm nghẽn
func (m *Mempool) GetShortIDMap(nonce uint64) (map[uint64][]byte, map[uint64]string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	idMap := make(map[uint64][]byte)
	hashMap := make(map[uint64]string)

	for _, txs := range m.txBySender {
		for _, item := range txs {
			hBytes, _ := hex.DecodeString(item.hash)
			shortID := m.bridge.CalculateShortTxIdFfi(hBytes, item.nonce)
			idMap[shortID] = item.data
			hashMap[shortID] = item.hash
		}
	}
	return idMap, hashMap
}

type txItem struct {
	hash              string
	data              []byte
	fee               uint64
	amount            uint64
	creationFee       uint64
	recentBlockHash   string
	size              int
	priority          float64
	timestamp         int64
	nonce             uint64
	sender            string
	index             int
	isGossipPublished bool
}

type TxHeap []*txItem

func (h TxHeap) Len() int           { return len(h) }
func (h TxHeap) Less(i, j int) bool { 
	if h[i].priority == h[j].priority {
		return h[i].timestamp < h[j].timestamp
	}
	return h[i].priority > h[j].priority 
}
func (h TxHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *TxHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*txItem)
	item.index = n
	*h = append(*h, item)
}
func (h *TxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

func (m *Mempool) GetPartitionedTransactions(maxStandardSize int) []*pb_tx.Transaction {
	m.mu.RLock()
	senders := make([]string, 0, len(m.txBySender))
	for sender := range m.txBySender {
		senders = append(senders, sender)
	}
	m.mu.RUnlock()

	// Tại sao thiết kế như vậy: Sử dụng getSenderNoncesBatch để lấy Nonce hàng loạt cho tất cả các senders
	// thông qua một cuộc gọi gRPC duy nhất (GetBalanceBatch), tránh bão gRPC gây nghẽn và timeout block template.
	senderNonces := m.getSenderNoncesBatch(senders)

	m.mu.RLock()
	defer m.mu.RUnlock()

	h := &TxHeap{}
	heap.Init(h)
	senderIdx := make(map[string]int)

	for sender, txs := range m.txBySender {
		if len(txs) == 0 {
			continue
		}

		startIdx := 0
		dbNonce := senderNonces[sender]
		for startIdx < len(txs) && txs[startIdx].nonce < dbNonce {
			startIdx++
		}

		if startIdx < len(txs) {
			heap.Push(h, txs[startIdx])
			senderIdx[sender] = startIdx
		}
	}

	var standardTxs []*pb_tx.Transaction
	currentSize := 0

	for h.Len() > 0 {
		item := heap.Pop(h).(*txItem)
		var tx pb_tx.Transaction
		if err := proto.Unmarshal(item.data, &tx); err != nil {
			continue
		}

		if currentSize+item.size <= maxStandardSize {
			standardTxs = append(standardTxs, &tx)
			currentSize += item.size
		} else {
			m.pushNextTx(h, item, senderIdx)
			continue
		}

		m.pushNextTx(h, item, senderIdx)

		// Tại sao thiết kế như vậy: Giới hạn số lượng giao dịch tối đa trong khối dựa theo cấu hình MaxTxsPerBlock
		// (truyền qua flag dòng lệnh CLI), mặc định là 1,000 giao dịch, nhằm tránh việc dry-run JMT trong Rust Core
		// vượt quá thời gian gRPC deadline và gây quá tải RocksDB khi stress test tải cao.
		if len(standardTxs) >= m.MaxTxsPerBlock {
			break
		}

		if currentSize >= maxStandardSize-1024 {
			break
		}
	}

	return standardTxs
}

func (m *Mempool) pushNextTx(h *TxHeap, item *txItem, senderIdx map[string]int) {
	sender := item.sender
	nextIdx := senderIdx[sender] + 1
	allSenderTxs := m.txBySender[sender]
	if nextIdx < len(allSenderTxs) {
		nextTx := allSenderTxs[nextIdx]
		if nextTx.nonce == item.nonce+1 {
			heap.Push(h, nextTx)
			senderIdx[sender] = nextIdx
		}
	}
}

func (m *Mempool) RemoveTransactions(txHashes []string) {
	m.mu.Lock()
	m.removeTransactionsLocked(txHashes)
	m.mu.Unlock() // GIẢI PHÓNG KHÓA NGAY LẬP TỨC!

	// [VÁ LỖI BẢO MẬT]: Gửi lệnh xóa RocksDB ra persistChan
	for _, hash := range txHashes {
		hBytes, _ := hex.DecodeString(hash)
		select {
		case m.persistChan <- mempoolPersistOp{opType: opRemove, txHash: hBytes}:
		default:
		}
	}
}

func (m *Mempool) removeTransactionsLocked(txHashes []string) {
	if len(txHashes) == 0 {
		return
	}

	// Tại sao thiết kế như vậy: Sử dụng map hashesToRemove để tra cứu nhanh O(1) các hash cần xóa,
	// tránh việc phải quét tuyến tính mảng giao dịch của người gửi nhiều lần.
	hashesToRemove := make(map[string]bool, len(txHashes))
	for _, hash := range txHashes {
		hashesToRemove[hash] = true
	}

	// affectedSenders lưu trữ danh sách những ví bị tác động trong lô xóa giao dịch này.
	affectedSenders := make(map[string]bool)
	txAmounts := make(map[string]uint64)
	txFees := make(map[string]uint64)
	txCreationFees := make(map[string]uint64)
	hasItemMap := make(map[string]bool)

	// Gom thông tin số dư dự chi và xác định sender của từng giao dịch cần xóa
	for _, hash := range txHashes {
		data, ok := m.pendingTxs[hash]
		if !ok {
			continue
		}

		senderHex, existsSender := m.txSender[hash]
		if existsSender {
			affectedSenders[senderHex] = true
			
			// Lấy thông tin chi tiết của transaction từ cache txItems trực tiếp với độ phức tạp O(1)
			if item, existsItem := m.txItems[hash]; existsItem {
				txAmounts[hash] = item.amount
				txFees[hash] = item.fee
				txCreationFees[hash] = item.creationFee
				hasItemMap[hash] = true
			} else {
				// Fallback slow path trong trường hợp không tìm thấy trong txItems
				senderTxs := m.txBySender[senderHex]
				for _, item := range senderTxs {
					if item.hash == hash {
						txAmounts[hash] = item.amount
						txFees[hash] = item.fee
						txCreationFees[hash] = item.creationFee
						hasItemMap[hash] = true
						break
					}
				}
			}
		} else {
			// Fallback slow path trong trường hợp không tìm thấy cache txSender (ví dụ khi khởi động lại node)
			var tx pb_tx.Transaction
			if err := proto.Unmarshal(data, &tx); err == nil {
				senderHex = hex.EncodeToString(tx.Sender.Value)
				affectedSenders[senderHex] = true
				txAmounts[hash] = tx.Amount
				txFees[hash] = tx.Fee
				if tx.Receiver != nil && m.bridge != nil {
					rState := m.bridge.GetAccountState(tx.Receiver.Value)
					if rState != nil && rState.Balance == 0 && rState.Nonce == 0 && len(rState.MaturingRewards) == 0 {
						txCreationFees[hash] = 1000
					}
				}
				hasItemMap[hash] = true
			}
		}
	}

	// Tại sao thiết kế như vậy: Thay vì lặp qua từng giao dịch cần xóa rồi quét tuyến tính và cắt lát (append)
	// mảng senderTxs cho từng giao dịch (dẫn đến độ phức tạp O(N^2) dưới lock độc quyền), ta gom các giao dịch
	// theo người gửi và thực hiện lọc (filter) mảng senderTxs đúng một lần duy nhất cho mỗi ví bị tác động.
	// Cách này giảm độ phức tạp xuống O(N) tuyến tính, giảm thiểu tối đa thời gian chiếm giữ Lock.
	for senderHex := range affectedSenders {
		senderTxs, exists := m.txBySender[senderHex]
		if !exists || len(senderTxs) == 0 {
			continue
		}

		newTxs := make([]*txItem, 0, len(senderTxs))
		for _, item := range senderTxs {
			if !hashesToRemove[item.hash] {
				newTxs = append(newTxs, item)
			}
		}

		if len(newTxs) == 0 {
			delete(m.txBySender, senderHex)
		} else {
			m.txBySender[senderHex] = newTxs
		}
	}

	// Cập nhật số dư dự chi pendingSpend và dọn dẹp các map lưu trữ RAM
	for _, hash := range txHashes {
		data, ok := m.pendingTxs[hash]
		if !ok {
			continue
		}

		senderHex, _ := m.txSender[hash]
		if senderHex == "" {
			var tx pb_tx.Transaction
			if err := proto.Unmarshal(data, &tx); err == nil {
				senderHex = hex.EncodeToString(tx.Sender.Value)
			}
		}

		if hasItemMap[hash] && senderHex != "" {
			cacheKey := senderHex + "_0"
			deduct := txAmounts[hash] + txFees[hash] + txCreationFees[hash]
			if m.pendingSpend[cacheKey] >= deduct {
				m.pendingSpend[cacheKey] -= deduct
			} else {
				m.pendingSpend[cacheKey] = 0
			}
		}

		m.totalBytes -= uint64(len(data))
		delete(m.pendingTxs, hash)
		delete(m.txItems, hash)
		delete(m.txTimestamp, hash)
		delete(m.txSender, hash)
		delete(m.txReceiver, hash)
	}

	if m.OnUpdate != nil {
		go m.OnUpdate()
	}
}

func (m *Mempool) StartEvictionWorker(ctx context.Context) {
	// [VANGUARD-FIX] Giảm chu kỳ dọn dẹp từ 5 phút xuống 30 giây
	// Tại sao: Với tải trọng cao (hàng trăm TX/giây), chu kỳ 5 phút quá chậm
	// khiến TX stale tích tụ và chặn giới hạn MaxTxPerAccount một cách sai lệch.
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done(): return
			case <-ticker.C: m.performEviction()
			}
		}
	}()
}

func (m *Mempool) performEviction() {
	// [VANGUARD-LOCK-OPTIMIZATION] Tách biệt I/O và lock bảo vệ mempool
	// Tại sao: Nếu chúng ta giữ Lock() và gọi gRPC hoặc FFI (như GetNonce, GetBlockHash) hàng ngàn lần
	// cho từng sender, luồng chính của Mempool sẽ bị khoá hoàn toàn (starvation/deadlock) trong suốt thời gian bão giao dịch.
	m.mu.RLock()
	senders := make([]string, 0, len(m.txBySender))
	for sender := range m.txBySender {
		senders = append(senders, sender)
	}
	m.mu.RUnlock()

	// Tại sao thiết kế như vậy: Sử dụng getSenderNoncesBatch để gom lô GetNonce của các senders
	// thành 1 cuộc gọi gRPC duy nhất sang Rust Core, triệt tiêu bão gRPC khi dọn dẹp mempool.
	senderNonces := m.getSenderNoncesBatch(senders)

	highestHeight := uint64(0)
	if m.bridge != nil {
		highestHeight = m.bridge.GetCurrentVersion()
	}

	recentBlockHashes := make(map[string]bool)
	startHeight := uint64(0)
	if highestHeight > 100 {
		startHeight = highestHeight - 100
	}
	if m.bridge != nil {
		for h := highestHeight; h >= startHeight; h-- {
			bHash := m.bridge.GetBlockHash(h)
			if len(bHash) > 0 {
				recentBlockHashes[string(bHash)] = true
			}
			if h == 0 {
				break
			}
		}
	}

	// Chỉ thực sự ghi khoá (Write Lock) khi thay đổi trạng thái nội bộ của Mempool Go
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var toRemove []string

	// [VANGUARD-CASCADING-EVICTION] Thuật toán Xóa Dây chuyền Phòng chống Nonce Gap
	// Tại sao thiết kế như vậy: Việc dọn dẹp các giao dịch lỗi (như expired, outdated block hash) hoặc
	// khoảng trống nonce quá lớn (nonce gap > maxAllowedNonceGap) một cách riêng lẻ sẽ để lại các lỗ hổng nonce
	// trong mempool, khiến Rust Core mô phỏng đóng khối bị lỗi. Do đó, nếu phát hiện giao dịch bị các lỗi trên,
	// ta sẽ xóa dây chuyền từ vị trí đó trở đi để đảm bảo tính liên tục của mempool.
	// Riêng với các giao dịch stale (nonce cũ hơn ledger), ta chỉ xóa riêng lẻ chúng vì chúng đã được xử lý xong
	// và không gây ra lỗ hổng nonce (nonce gap) cho các giao dịch mới xếp hàng phía sau.
	for senderHex, txs := range m.txBySender {
		if len(txs) == 0 {
			continue
		}

		currentNonce := senderNonces[senderHex]

		expectedNonce := currentNonce
		firstInvalidIdx := -1
		for i, item := range txs {
			// 1. Kiểm tra stale (nonce cũ hơn ledger)
			// Tại sao thiết kế như vậy: Giao dịch stale đã có trong ledger hoặc đã lỗi thời,
			// việc xóa nó không ảnh hưởng tới chuỗi nonce phía sau (các giao dịch phía sau có nonce >= currentNonce).
			// Do đó, ta chỉ xóa riêng lẻ giao dịch này và dùng continue để tiếp tục duyệt các giao dịch tiếp theo.
			if item.nonce < currentNonce {
				toRemove = append(toRemove, item.hash)
				continue
			}

			// [AUTO-GAP-HEAL] Tự động phát hiện và dọn dẹp các giao dịch kẹt do lệch nonce lớn (Nonce Gap)
			// Tại sao thiết kế như vậy: Để loại bỏ hoàn toàn các giao dịch rác cũ bị kẹt trong mempool 
			// (ví dụ khi reset ledger hoặc fork mạng) vốn có nonce lớn hơn ledger rất nhiều.
			// Nếu có một khoảng trống nonce (nonce gap) lớn hơn 20 ở đầu hàng đợi hoặc giữa hàng đợi,
			// chúng sẽ bị coi là kẹt vĩnh viễn và bị quét sạch ngay lập tức để giải phóng mempool.
			maxAllowedNonceGap := uint64(20)
			if item.nonce > expectedNonce {
				gap := item.nonce - expectedNonce
				if gap > maxAllowedNonceGap {
					firstInvalidIdx = i
					P2PLog("[MEMPOOL-HEAL] 🩹 Phát hiện Nonce Gap quá lớn tại ví %s.. (Giao dịch nonce: %d, Expected nonce: %d, Gap: %d). Tự động dọn dẹp các giao dịch bị kẹt phía sau.",
						senderHex[:8], item.nonce, expectedNonce, gap)
					break
				}
			} else if item.nonce == expectedNonce {
				expectedNonce = item.nonce + 1
			}

			// 2. Kiểm tra expired (quá 3 giờ để giải phóng dung lượng rác)
			isExpired := false
			if ts, ok := m.txTimestamp[item.hash]; ok {
				if now.Sub(ts) > 3*time.Hour {
					isExpired = true
				}
			}

			// 3. Kiểm tra outdated blockhash (ngoài 80 khối) - Sử dụng cached recentBlockHash
			isOutdated := false
			if m.bridge != nil && len(item.recentBlockHash) > 0 {
				if !recentBlockHashes[item.recentBlockHash] {
					isOutdated = true
				}
			}

			if isExpired || isOutdated {
				firstInvalidIdx = i
				break
			}
		}

		// Nếu phát hiện giao dịch lỗi đầu tiên, thực hiện xóa dây chuyền tất cả các giao dịch từ đó trở đi
		if firstInvalidIdx != -1 {
			for i := firstInvalidIdx; i < len(txs); i++ {
				toRemove = append(toRemove, txs[i].hash)
			}
			P2PLog("[MEMPOOL-CASCADING] 🌊 Phát hiện lỗi hoặc kẹt nonce tại nonce #%d của ví %s.. (Index: %d/%d). Đang dọn sạch %d giao dịch phía sau để chống Nonce Gap.",
				txs[firstInvalidIdx].nonce, senderHex[:8], firstInvalidIdx, len(txs), len(txs)-firstInvalidIdx)
		}
	}

	if len(toRemove) > 0 {
		// Loại bỏ các hash trùng lặp để tối ưu hóa việc xóa dưới Lock()
		uniqueToRemove := make(map[string]bool)
		var finalToRemove []string
		for _, hash := range toRemove {
			if !uniqueToRemove[hash] {
				uniqueToRemove[hash] = true
				finalToRemove = append(finalToRemove, hash)
			}
		}

		m.removeTransactionsLocked(finalToRemove)
		// Thực hiện dọn dẹp RocksDB bất đồng bộ ngoài luồng Lock chính
		for _, hash := range finalToRemove {
			hBytes, _ := hex.DecodeString(hash)
			select {
			case m.persistChan <- mempoolPersistOp{opType: opRemove, txHash: hBytes}:
			default:
			}
		}
		P2PLog("[MEMPOOL-CLEANUP] 🧹 Máy hút bụi đã dọn xong tổng cộng %d giao dịch lỗi/stale/outdated bằng thuật toán Cascading.", len(finalToRemove))
	}
}

func (m *Mempool) ResetNanoWeights() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingSpend = make(map[string]uint64)
	m.txBySender = make(map[string][]*txItem)
	m.txItems = make(map[string]*txItem) // Rebuild txItems cache
	m.totalBytes = 0 // Reset dung lượng byte trước khi tính toán lại

	for txHash, txData := range m.pendingTxs {
		var tx pb_tx.Transaction
		if err := proto.Unmarshal(txData, &tx); err == nil {
			senderHex := hex.EncodeToString(tx.Sender.Value)
			cacheKey := senderHex + "_0"
			
			// Tính creationFee động
			creationFee := uint64(0)
			if tx.Receiver != nil && m.bridge != nil {
				rState := m.bridge.GetAccountState(tx.Receiver.Value)
				if rState != nil && rState.Balance == 0 && rState.Nonce == 0 && len(rState.MaturingRewards) == 0 {
					creationFee = 1000
				}
			}
			
			m.pendingSpend[cacheKey] += tx.Amount + tx.Fee + creationFee
			item := &txItem{
				hash: txHash, data: txData, fee: tx.Fee, amount: tx.Amount, creationFee: creationFee,
				recentBlockHash: string(tx.RecentBlockHash), size: len(txData),
				priority: float64(tx.Fee), // V1.19: Ưu tiên tuyệt đối theo Tầng phí
				timestamp: time.Now().UnixNano(), nonce: tx.Nonce, sender: senderHex,
			}
			m.txBySender[senderHex] = append(m.txBySender[senderHex], item)
			m.txItems[txHash] = item // Store in cache index
		}
		m.totalBytes += uint64(len(txData)) // Cộng dồn dung lượng byte của các giao dịch được nạp lại
	}

	// [VANGUARD-FIX] Sắp xếp lại txBySender theo Nonce tăng dần sau khi load ngẫu nhiên từ map
	for _, txs := range m.txBySender {
		sort.Slice(txs, func(i, j int) bool {
			return txs[i].nonce < txs[j].nonce
		})
	}
}

func GetSigningHash(tx *pb_tx.Transaction) []byte {
	return GetSigningHashVanguard(tx)
}

func GetSigningHashVanguard(tx *pb_tx.Transaction) []byte {
	// [V11.5 FIX] ĐỒNG BỘ HÓA TỐI THƯỢNG VỚI RUST CORE (0_shared_lib)
	
	if go_bridge.GlobalBridge != nil {
		return go_bridge.GlobalBridge.GetSigningHash(tx)
	}

	// [FATAL] Không có bridge thì không được phép tính băm Consensus.
	log.Fatalf("[FATAL-CONSENSUS] 💀 Lỗi nghiêm trọng: GlobalBridge không khả dụng để tính toán mã băm chữ ký. Dừng Node.")
	return make([]byte, 32)
}



func GetChainID() uint32 { return 0x47454E5A }

// ============================================================================
// [V1.50] Cơ chế khôi phục Mempool từ RocksDB (Rust Core)
// ============================================================================

func (m *Mempool) loadMempoolFromRust() {
	log.Printf("[MEMPOOL-V1.50] 🧹 Đang dọn sạch toàn bộ giao dịch cũ tồn đọng trong Rust Core...")
	
	entries, err := m.bridge.GetMempoolEntries()
	if err != nil {
		log.Printf("[MEMPOOL-V1.50] ❌ Không thể kết nối Rust Core để dọn dẹp Mempool: %v", err)
		return
	}

	if len(entries) == 0 {
		log.Printf("[MEMPOOL-V1.50] ✨ RocksDB của Rust Core sạch sẽ. Không có giao dịch tồn đọng.")
		return
	}

	// Xóa sạch các giao dịch cũ tồn đọng trong database của Rust Core
	// Tại sao thiết kế như vậy: Khi khởi động lại, phần lớn giao dịch cũ trong RocksDB đã lỗi thời hoặc đã được đóng vào block thành công.
	// Việc khôi phục hàng chục ngàn giao dịch rác này vào RAM rồi mới chạy bộ lọc dọn dẹp gây lãng phí tài nguyên CPU/RAM
	// và làm treo luồng khởi động của Node. Dọn sạch từ đầu giúp Node sẵn sàng ngay lập tức ở trạng thái sạch sẽ.
	count := 0
	for _, entry := range entries {
		m.bridge.RemoveFromMempool(entry.TxHash)
		count++
	}

	log.Printf("[MEMPOOL-V1.50] 🧹 Đã dọn sạch %d giao dịch cũ khỏi RocksDB của Rust Core. Mempool khởi động trống hoàn toàn.", count)
}

// [VANGUARD-HOTFIX] Purge: Xóa sạch mempool để gỡ nghẽn hệ thống (Async & No Lock Bottleneck)
func (m *Mempool) Purge() {
	m.mu.Lock()
	
	count := len(m.pendingTxs)
	
	// Thu thập tất cả các transaction hash để thực hiện dọn dẹp RocksDB bất đồng bộ
	hashes := make([]string, 0, count)
	for hashStr := range m.pendingTxs {
		hashes = append(hashes, hashStr)
	}

	m.pendingTxs = make(map[string][]byte)
	m.txTimestamp = make(map[string]time.Time)
	m.pendingSpend = make(map[string]uint64)
	m.txBySender = make(map[string][]*txItem)
	m.txSender = make(map[string]string)
	m.txReceiver = make(map[string]string)
	m.projectedNonce = make(map[string]uint64)
	m.totalBytes = 0 // Reset dung lượng byte mempool về 0

	// [VANGUARD-FIX] Dọn sạch các giao dịch đang xếp hàng chờ trong RAM TxBus channel
	drainedCount := 0
	for {
		select {
		case <-m.TxBus:
			drainedCount++
		default:
			goto DRAIN_DONE
		}
	}
DRAIN_DONE:
	if drainedCount > 0 {
		log.Printf("[MEMPOOL-PURGE] 🧹 Đã loại bỏ %d giao dịch đang xếp hàng trong RAM TxBus.", drainedCount)
	}

	m.mu.Unlock()

	// [VANGUARD-FIX] Xóa sạch các giao dịch trong RocksDB của Rust Core qua bridge (chạy async ngoài lock)
	for _, hashStr := range hashes {
		hBytes, err := hex.DecodeString(hashStr)
		if err == nil {
			select {
			case m.persistChan <- mempoolPersistOp{opType: opRemove, txHash: hBytes}:
			default:
			}
		}
	}

	log.Printf("[MEMPOOL-PURGE] 🧹 Đã xóa sạch %d giao dịch khỏi Mempool và RocksDB (Async).", count)
	
	if m.OnUpdate != nil {
		go m.OnUpdate()
	}
}

// RemoveStaleNonceTxsBatch: [V1.60-RACE-FIX] Xóa tất cả TX stale (nonce < currentNonce) của nhiều senders theo lô
// Tại sao thiết kế như vậy: Chuyển đổi từ xử lý đơn lẻ sang gom lô và gộp khóa. Hàm này chỉ khóa Mutex của mempool
// đúng 1 lần duy nhất cho toàn bộ quá trình dọn dẹp sau khối, giúp triệt tiêu hoàn tượng tranh chấp khóa Mutex (lock contention)
// và bão I/O RocksDB khi commit khối lớn chứa hàng chục ngàn giao dịch từ nhiều tài khoản gửi khác nhau.
func (m *Mempool) RemoveStaleNonceTxsBatch(senders []string, nonces []uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalRemoved := 0
	var allStaleTxHashes []string

	for idx, senderHex := range senders {
		if idx >= len(nonces) {
			break
		}
		currentNonce := nonces[idx]
		senderTxs := m.txBySender[senderHex]
		if len(senderTxs) == 0 {
			continue
		}

		removed := 0
		var staleTxHashes []string

		// Duyệt qua tất cả TX của sender, thu thập TX stale
		for _, item := range senderTxs {
			if item.nonce < currentNonce {
				staleTxHashes = append(staleTxHashes, item.hash)
				allStaleTxHashes = append(allStaleTxHashes, item.hash)
			}
		}

		// Xóa từng TX stale khỏi tất cả các map
		for _, txHash := range staleTxHashes {
			var txAmount, txFee, creationFee uint64
			foundItem := false
			for _, item := range senderTxs {
				if item.hash == txHash {
					txAmount = item.amount
					txFee = item.fee
					creationFee = item.creationFee
					foundItem = true
					break
				}
			}

			if foundItem {
				cacheKey := senderHex + "_0"
				deduct := txAmount + txFee + creationFee
				if m.pendingSpend[cacheKey] >= deduct {
					m.pendingSpend[cacheKey] -= deduct
				} else {
					m.pendingSpend[cacheKey] = 0
				}
			} else {
				// Fallback slow path
				if txData, ok := m.pendingTxs[txHash]; ok {
					var tx pb_tx.Transaction
					if err := proto.Unmarshal(txData, &tx); err == nil {
						cacheKey := senderHex + "_0"
						txAmount := tx.Amount + tx.Fee
						var cFee uint64 = 0
						if tx.Receiver != nil && m.bridge != nil {
							rState := m.bridge.GetAccountState(tx.Receiver.Value)
							if rState != nil && rState.Balance == 0 && rState.Nonce == 0 && len(rState.MaturingRewards) == 0 {
								cFee = 1000
							}
						}
						deduct := txAmount + cFee
						if m.pendingSpend[cacheKey] >= deduct {
							m.pendingSpend[cacheKey] -= deduct
						} else {
							m.pendingSpend[cacheKey] = 0
						}
					}
				}
			}
			delete(m.pendingTxs, txHash)
			delete(m.txItems, txHash)
			delete(m.txTimestamp, txHash)
			delete(m.txSender, txHash)
			delete(m.txReceiver, txHash)
			removed++
		}

		// Rebuild txBySender cho sender này, loại bỏ TX stale
		if removed > 0 {
			var remaining []*txItem
			for _, item := range senderTxs {
				if item.nonce >= currentNonce {
					remaining = append(remaining, item)
				}
			}
			if len(remaining) > 0 {
				m.txBySender[senderHex] = remaining
			} else {
				delete(m.txBySender, senderHex)
			}

			// Xóa projected nonce cũ để đồng bộ lại với Ledger
			delete(m.projectedNonce, senderHex)
			totalRemoved += removed
		}
	}

	// Gửi lệnh xóa RocksDB ra persistChan một lần cho toàn bộ stale hashes
	if len(allStaleTxHashes) > 0 {
		for _, hash := range allStaleTxHashes {
			hBytes, _ := hex.DecodeString(hash)
			select {
			case m.persistChan <- mempoolPersistOp{opType: opRemove, txHash: hBytes}:
			default:
			}
		}
		log.Printf("[MEMPOOL-STALE-FIX] 🧹 Đã dọn dẹp hàng loạt %d TX stale từ %d ví...", totalRemoved, len(senders))
	}

	return totalRemoved
}

// ClearProjectedNonce: [VANGUARD-HOTFIX] Xóa bộ nhớ đệm Nonce để đồng bộ lại với Ledger
func (m *Mempool) ClearProjectedNonce(senderHex string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.projectedNonce, senderHex)
	log.Printf("[MEMPOOL-FIX] 🧹 Đã xóa Projected Nonce cho ví %s để đồng bộ lại.", senderHex[:10])
}

// getSenderNoncesBatch lấy Nonce hàng loạt cho danh sách địa chỉ gửi để tránh bão gRPC.
// Tại sao thiết kế như vậy: Loại bỏ hoàn toàn cuộc gọi gRPC GetBalanceBatch xuống Rust Core
// để triệt tiêu bão I/O RocksDB (RocksDB Read Storm) khi thợ đào yêu cầu tạo Block Template.
// Hàm sử dụng trực tiếp RAM cache của Mempool cục bộ (nonce của giao dịch đầu tiên trong hàng đợi hoặc projectedNonce).
// Rust Core khi mô phỏng và thực thi khối sẽ đóng vai trò chốt chặn cuối cùng loại bỏ giao dịch sai nonce một cách cực kỳ nhanh chóng.
func (m *Mempool) getSenderNoncesBatch(senders []string) map[string]uint64 {
	senderNonces := make(map[string]uint64)
	for _, sender := range senders {
		senderNonces[sender] = 0 // Khởi tạo mặc định
	}
	if len(senders) == 0 {
		return senderNonces
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sender := range senders {
		txs := m.txBySender[sender]
		if len(txs) > 0 {
			senderNonces[sender] = txs[0].nonce
		} else {
			senderNonces[sender] = m.projectedNonce[sender]
		}
	}
	return senderNonces
}

// addValidatedTxLocked nạp trực tiếp một giao dịch đã được Rust Core xác thực vào Mempool Go mà không giải phóng dung lượng ngay.
// Yêu cầu phải giữ Lock của mempool trước khi gọi.
func (m *Mempool) addValidatedTxLocked(txHash string, txData []byte, senderHex string, tx *pb_tx.Transaction, creationFee uint64) bool {
	if _, ok := m.pendingTxs[txHash]; ok {
		return true
	}

	m.pendingTxs[txHash] = txData
	m.totalBytes += uint64(len(txData))
	m.txTimestamp[txHash] = time.Now()

	m.txSender[txHash] = senderHex
	if tx.Receiver != nil {
		m.txReceiver[txHash] = hex.EncodeToString(tx.Receiver.Value)
	}

	item := &txItem{
		hash:            txHash,
		data:            txData,
		fee:             tx.Fee,
		amount:          tx.Amount,
		creationFee:     creationFee,
		recentBlockHash: string(tx.RecentBlockHash),
		size:            len(txData),
		priority:        float64(tx.Fee),
		timestamp:       time.Now().UnixNano(),
		nonce:           tx.Nonce,
		sender:          senderHex,
	}

	senderTxs := m.txBySender[senderHex]
	insertIdx := len(senderTxs)
	for i, existing := range senderTxs {
		if tx.Nonce < existing.nonce {
			insertIdx = i
			break
		} else if tx.Nonce == existing.nonce {
			return false
		}
	}

	senderTxs = append(senderTxs, nil)
	copy(senderTxs[insertIdx+1:], senderTxs[insertIdx:])
	senderTxs[insertIdx] = item
	m.txBySender[senderHex] = senderTxs

	m.txItems[txHash] = item

	cacheKey := senderHex + "_0"
	m.pendingSpend[cacheKey] += tx.Amount + tx.Fee + creationFee

	m.projectedNonce[senderHex] = tx.Nonce + 1

	hBytes, _ := hex.DecodeString(txHash)
	select {
	case m.persistChan <- mempoolPersistOp{opType: opAdd, txHash: hBytes, txRaw: txData}:
	default:
		log.Printf("[MEMPOOL-PERSIST] 🚨 Kênh persistChan đầy! Bỏ qua ghi đĩa cho giao dịch %s", txHash[:12])
	}

	return true
}

// GetSenders: Trả về danh sách các sender hiện có trong Mempool để hỗ trợ revalidation
func (m *Mempool) GetSenders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	senders := make([]string, 0, len(m.txBySender))
	for sender := range m.txBySender {
		senders = append(senders, sender)
	}
	return senders
}

// GetTransaction: [RECONSTRUCTION-CACHE] Truy vấn an toàn giao dịch từ Mempool theo TxHash hex
func (m *Mempool) GetTransaction(txHash string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	txData, ok := m.pendingTxs[txHash]
	return txData, ok
}

// GetSequentialTxsToPublish: [ANTI-SPAM-P2P] Lấy danh sách giao dịch tuần tự (không bị đứt quãng nonce)
// chưa được phát tán lên mạng lưới để tránh spam out-of-order.
func (m *Mempool) GetSequentialTxsToPublish(senderHex string) [][]byte {
	senderAddr, err := hex.DecodeString(senderHex)
	if err != nil {
		return nil
	}
	
	// Lấy Nonce ngoài Lock để tránh giữ Lock lâu
	var currentNonce uint64
	if m.bridge != nil {
		currentNonce = m.bridge.GetNonce(nil, senderAddr)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	txs, ok := m.txBySender[senderHex]
	if !ok || len(txs) == 0 {
		return nil
	}

	var txsToPublish [][]byte
	expected := currentNonce
	for _, item := range txs {
		if item.nonce < currentNonce {
			continue
		}
		if item.nonce == expected {
			if !item.isGossipPublished {
				item.isGossipPublished = true
				txsToPublish = append(txsToPublish, item.data)
			}
			expected++
		} else if item.nonce > expected {
			// Phát hiện đứt quãng nonce (gap), dừng quét ngay lập tức
			break
		}
	}

	return txsToPublish
}

// GetTxsToRebroadcast: [ANTI-SPAM-P2P] Quét mempool và lấy ra giao dịch đầu hàng đợi chưa được đóng gói để phát sóng lại.
// Tại sao thiết kế như vậy: Trong môi trường P2P Gossip, một số gói tin phát sóng giao dịch có thể bị thất lạc do 
// mất kết nối mạng tạm thời hoặc Node vừa mới tham gia mạng chưa kịp kết nối. Nếu không có cơ chế tự phục hồi, 
// giao dịch bị mất sẽ bị kẹt vĩnh viễn trong mempool của Node nhận trực tiếp (vì Node này đã publish gossip một lần 
// và sẽ không phát sóng lại). Hàm này giúp định kỳ quét mempool và lấy ra giao dịch đầu hàng đợi của mỗi ví 
// (nonce bằng đúng nonce hiện tại của sổ cái) để phát sóng lại, giúp miner nhận được giao dịch tiếp theo và giải phóng hàng chờ bị nghẽn.
func (m *Mempool) GetTxsToRebroadcast() [][]byte {
	// Giai đoạn 1: Lấy danh sách senders dưới RLock
	m.mu.RLock()
	senders := make([]string, 0, len(m.txBySender))
	for sender := range m.txBySender {
		senders = append(senders, sender)
	}
	m.mu.RUnlock()

	// Giai đoạn 2: Tải nonces của tất cả senders ngoài Lock để tránh nghẽn
	// Tại sao thiết kế như vậy: Sử dụng getSenderNoncesBatch để lấy Nonce hàng loạt cho tất cả các senders,
	// tránh bão gRPC gây nghẽn tiến trình phát sóng lại giao dịch.
	senderNonces := m.getSenderNoncesBatch(senders)

	// Giai đoạn 3: Thực hiện lấy giao dịch phát sóng lại dưới Lock
	m.mu.Lock()
	defer m.mu.Unlock()

	var txsToPublish [][]byte
	for senderHex, txs := range m.txBySender {
		if len(txs) == 0 {
			continue
		}
		currentNonce := senderNonces[senderHex]

		// Duyệt qua tất cả giao dịch của sender này để tìm giao dịch đầu hàng đợi
		for _, item := range txs {
			// Chỉ phát sóng lại giao dịch có nonce bằng đúng nonce hiện tại của ledger
			// (giao dịch tiếp theo đang chờ đóng gói của địa chỉ ví này)
			if item.nonce == currentNonce {
				txsToPublish = append(txsToPublish, item.data)
				break // Chỉ phát sóng giao dịch đầu tiên của mỗi ví để tránh quá tải mạng
			}
		}
	}

	return txsToPublish
}

// PushToTxBus đẩy giao dịch thô vào TxBus RAM Channel. Trả về true nếu thành công.
func (m *Mempool) PushToTxBus(txData []byte, isLocal bool) bool {
	// [SECURITY-VANGUARD] Thêm cờ đánh dấu nguồn gốc giao dịch (Local vs Gossip) để chống bão gRPC
	prefix := byte(0)
	if isLocal {
		prefix = byte(1)
	}
	data := make([]byte, 1+len(txData))
	data[0] = prefix
	copy(data[1:], txData)

	select {
	case m.TxBus <- data:
		return true
	default:
		log.Printf("[MEMPOOL-BUS] 🚨 TxBus quá tải, từ chối giao dịch!")
		return false
	}
}

// SetOnTxBatchValidated thiết lập callback thông báo thay đổi trạng thái giao dịch cho Tracker.
func (m *Mempool) SetOnTxBatchValidated(f func(results []TxValidatedResult)) {
	m.OnTxBatchValidated = f
}

// StartTxBus gom các giao dịch từ TxBus và định kỳ mỗi 2 giây gọi gRPC xuống Rust Core để xác minh hàng loạt.
func (m *Mempool) StartTxBus(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var batch [][]byte
	// Tăng giới hạn lô gộp từ 1,000 lên 50,000 giao dịch.
	// Tại sao: Do việc truyền tin giữa Go và Rust Core được thực hiện qua gRPC cục bộ (Local Loopback / Named Pipe), 
	// việc truyền tải dữ liệu dung lượng lớn cực kỳ nhanh và không bị trễ mạng. 
	// Việc tăng giới hạn giúp gom tối đa tất cả các giao dịch trong trạm chờ vào duy nhất một lệnh kiểm duyệt của Rust, 
	// tối ưu hóa hiệu năng xử lý song song bằng Rayon ở phía Rust Engine.
	const maxBatchSize = 50000

	for {
		select {
		case <-ctx.Done():
			return
		case txData, ok := <-m.TxBus:
			if !ok {
				return
			}
			batch = append(batch, txData)
			
			// [VANGUARD-OPTIMIZATION] Đọc sạch toàn bộ các giao dịch đang có sẵn trong channel tại thời điểm này
			// để gom hết vào cùng một lô validate, loại bỏ hoàn toàn race condition chia nhỏ lô giao dịch của Go Node
			// và triệt tiêu lỗi Nonce Gap / Nonce Mismatch do việc validate manh mún.
			drained := false
			for !drained && len(batch) < maxBatchSize {
				select {
				case nextTx, ok2 := <-m.TxBus:
					if !ok2 {
						drained = true
					} else {
						batch = append(batch, nextTx)
					}
				default:
					drained = true
				}
			}

			if len(batch) >= maxBatchSize {
				m.processBusBatch(batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				m.processBusBatch(batch)
				batch = nil
			}
		}
	}
}

// processBusBatch gửi loạt giao dịch xuống Rust Core qua gRPC và nạp các giao dịch hợp lệ.
func (m *Mempool) processBusBatch(batch [][]byte) {
	log.Printf("[MEMPOOL-BUS] 🚌 Xe buýt xuất phát! Gom được %d giao dịch, đang tách cờ và gọi gRPC xuống Rust...", len(batch))
	start := time.Now()

	cleanBatch := make([][]byte, len(batch))
	isLocalFlags := make([]bool, len(batch))
	for i, entry := range batch {
		if len(entry) > 0 {
			isLocalFlags[i] = entry[0] == 1
			cleanBatch[i] = entry[1:]
		} else {
			cleanBatch[i] = entry
		}
	}

	resp, err := m.bridge.ValidateTransactionBatch(cleanBatch)
	if err != nil {
		log.Printf("[MEMPOOL-BUS] ❌ Gọi gRPC ValidateTransactionBatch thất bại: %v", err)
		var validatedResults []TxValidatedResult
		for _, txBytes := range cleanBatch {
			var tx pb_tx.Transaction
			if err := proto.Unmarshal(txBytes, &tx); err != nil {
				continue
			}
			txHash := GetSigningHashNative(&tx)
			txHashStr := hex.EncodeToString(txHash)
			validatedResults = append(validatedResults, TxValidatedResult{
				TxHash:        txHashStr,
				IsValid:       false,
				StatusCode:    999,
				ErrorMsg:      "Lỗi kết nối gRPC hệ thống: " + err.Error(),
				TxData:        txBytes,
				Tx:            &tx,
				SenderBalance: 0,
			})
		}
		if m.OnTxBatchValidated != nil {
			m.OnTxBatchValidated(validatedResults)
		}
		return
	}

	log.Printf("[MEMPOOL-BUS] 🚌 Xe buýt về trạm! Nhận phản hồi trong %v", time.Since(start))

	var validatedResults []TxValidatedResult

	// Bước 1: Giải mã (Unmarshal) trước toàn bộ giao dịch để lấy thông tin ví và lọc ra các người nhận hợp lệ
	type tempTxInfo struct {
		tx         *pb_tx.Transaction
		txHashStr  string
		isValid    bool
		statusCode uint32
		errorMsg   string
		txBytes    []byte
		isLocal    bool
	}
	tempInfos := make([]tempTxInfo, len(resp.Results))
	var allAddrs [][]byte
	addrSet := make(map[string]bool) // Khử trùng địa chỉ ví để tránh gọi gRPC trùng lặp

	for i, result := range resp.Results {
		txBytes := cleanBatch[i]
		isLocal := isLocalFlags[i]
		var tx pb_tx.Transaction
		if err := proto.Unmarshal(txBytes, &tx); err != nil {
			log.Printf("[MEMPOOL-BUS] ❌ Unmarshal transaction index %d thất bại", i)
			continue
		}

		txHashStr := hex.EncodeToString(result.TxHash)
		tempInfos[i] = tempTxInfo{
			tx:         &tx,
			txHashStr:  txHashStr,
			isValid:    result.IsValid,
			statusCode: result.StatusCode,
			errorMsg:   result.ErrorMsg,
			txBytes:    txBytes,
			isLocal:    isLocal,
		}

		if tx.Sender != nil {
			senderStr := string(tx.Sender.Value)
			if !addrSet[senderStr] {
				addrSet[senderStr] = true
				allAddrs = append(allAddrs, tx.Sender.Value)
			}
		}

		if tx.Receiver != nil {
			recStr := string(tx.Receiver.Value)
			if !addrSet[recStr] {
				addrSet[recStr] = true
				allAddrs = append(allAddrs, tx.Receiver.Value)
			}
		}
	}

	// Bước 2: Gom lô và gọi GetBalanceBatch đúng 1 lần duy nhất cho toàn bộ địa chỉ ví gửi/nhận (Creation Fee Check & Nonce Window check)
	walletStateMap := make(map[string]*pb_tx.BalanceEntry)
	if len(allAddrs) > 0 {
		balanceEntries, err := m.bridge.GetBalanceBatch(allAddrs)
		if err == nil {
			for _, entry := range balanceEntries {
				walletStateMap[string(entry.Address)] = entry
			}
		} else {
			log.Printf("[MEMPOOL-BUS-WARN] GetBalanceBatch failed: %v", err)
		}
	}

	// Bước 3: Duyệt lại toàn bộ để áp dụng phí tạo ví và thêm giao dịch vào Mempool
	// Tại sao thiết kế như vậy: Việc ghi log chi tiết cho từng giao dịch bị từ chối trong một batch lớn (hàng chục ngàn giao dịch rác)
	// sẽ tạo ra lượng I/O đĩa khổng lồ, làm nghẽn luồng Mempool và Miner. Gom lại thành một biến đếm giúp tối ưu hiệu năng đáng kể.
	spamRejected := 0

	// Lock mempool 1 lần duy nhất cho toàn bộ quá trình nạp batch giao dịch hợp lệ.
	// Tại sao thiết kế như vậy: Tránh việc gọi Lock/Unlock hàng chục ngàn lần cho từng giao dịch riêng lẻ,
	// đồng thời gom việc kiểm tra giải phóng dung lượng Mempool (Eviction) về đúng 1 lần duy nhất ở cuối batch
	// nhằm triệt tiêu hoàn toàn lỗi Catastrophic Eviction Loop làm đơ CPU.
	m.mu.Lock()
	for i, info := range tempInfos {
		if info.tx == nil || !tempInfos[i].isValid {
			continue
		}

		senderHex := hex.EncodeToString(info.tx.Sender.Value)
		creationFee := uint64(0)
		if info.tx.Receiver != nil {
			recState := walletStateMap[string(info.tx.Receiver.Value)]
			if recState != nil && recState.Balance == 0 && recState.Nonce == 0 {
				creationFee = 1000
			}
		}

		success := m.addValidatedTxLocked(info.txHashStr, info.txBytes, senderHex, info.tx, creationFee)
		if !success {
			tempInfos[i].isValid = false
			tempInfos[i].statusCode = 998
			tempInfos[i].errorMsg = "Lỗi nạp mempool Go sau khi Rust đã phê duyệt"
		}
	}

	if m.totalBytes > MempoolMaxBytes {
		m.PerformCapacityEviction()
	}
	m.mu.Unlock()

	// Duyệt lại toàn bộ để cập nhật projected nonce cho giao dịch lỗi và gom validatedResults
	for i, info := range tempInfos {
		if info.tx == nil {
			continue
		}

		senderHex := hex.EncodeToString(info.tx.Sender.Value)
		if !tempInfos[i].isValid {
			m.ClearProjectedNonce(senderHex)
			spamRejected++
		}

		var senderBal uint64 = 0
		if info.tx != nil && info.tx.Sender != nil {
			senderState := walletStateMap[string(info.tx.Sender.Value)]
			if senderState != nil {
				senderBal = senderState.Balance
			}
		}

		creationFee := uint64(0)
		if tempInfos[i].isValid && info.tx.Receiver != nil {
			recState := walletStateMap[string(info.tx.Receiver.Value)]
			if recState != nil && recState.Balance == 0 && recState.Nonce == 0 {
				creationFee = 1000
			}
		}

		validatedResults = append(validatedResults, TxValidatedResult{
			TxHash:        info.txHashStr,
			IsValid:       tempInfos[i].isValid,
			StatusCode:    tempInfos[i].statusCode,
			ErrorMsg:      tempInfos[i].errorMsg,
			TxData:        info.txBytes,
			Tx:            info.tx,
			CreationFee:   creationFee,
			SenderBalance: senderBal,
		})
	}

	// In ra 1 dòng tổng kết duy nhất nếu có giao dịch rác bị từ chối để giám sát trạng thái mà không gây tải I/O.
	if spamRejected > 0 {
		log.Printf("[MEMPOOL-BUS] %s", i18n.T("log_mempool_spam_rejected", spamRejected))
	}

	if m.OnTxBatchValidated != nil {
		m.OnTxBatchValidated(validatedResults)
	}

	if m.OnUpdate != nil {
		go m.OnUpdate()
	}
}

// AddValidatedTx nạp trực tiếp một giao dịch đã được Rust Core xác thực vào Mempool Go.
func (m *Mempool) AddValidatedTx(txHash string, txData []byte, senderHex string, tx *pb_tx.Transaction, creationFee uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	success := m.addValidatedTxLocked(txHash, txData, senderHex, tx, creationFee)
	if success && m.totalBytes > MempoolMaxBytes {
		m.PerformCapacityEviction()
	}
	return success
}


