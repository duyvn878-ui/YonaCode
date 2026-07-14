package mining_pool

import (
	"encoding/binary"
	"encoding/json"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"btc_genz/2_miner_core/go_bridge"
)

type WorkerInfo struct {
	Address     string          `json:"address"`
	Shares      float64         `json:"shares"`
	Hashrate    uint64          `json:"hashrate"`
	LastSeen    int64           `json:"last_seen"`
	Difficulty  uint64          `json:"difficulty"`
	LastSubmit  time.Time       `json:"-"`
	SubmitTimes []time.Duration `json:"-"`
}

type BlockPayoutInfo struct {
	Height      uint64             `json:"height"`
	Shares      map[string]float64 `json:"shares"`
}

type MiningPool struct {
	PoolAddress   string
	PoolKeyHex    string
	PoolFee       float64
	ShareDiffMult uint64
	DbPath        string

	Workers map[string]*WorkerInfo
	Mu      sync.RWMutex

	TotalHashrate uint64
	SolvedBlocks  uint64
	MinedBlocks   []uint64 // [VAR-SECURITY] Danh sách chiều cao các khối đào trúng thực tế trên chuỗi

	// [VAR-SECURITY] Lưu các nonce đã sử dụng để chống double submit share
	usedNonces    map[uint64]bool
	currentHeight uint64

	// [VAR-SECURITY] Danh sách các khối trúng đang chờ thanh toán kèm bản chụp shares
	PendingPayouts []BlockPayoutInfo
}

type PoolStats struct {
	SolvedBlocks uint64   `json:"solved_blocks"`
	MinedBlocks  []uint64 `json:"mined_blocks"`
}

func NewMiningPool(poolAddress, poolKeyHex string, poolFee float64, shareDiffMult uint64, dbPath string) *MiningPool {
	p := &MiningPool{
		PoolAddress:   poolAddress,
		PoolKeyHex:    poolKeyHex,
		PoolFee:       poolFee,
		ShareDiffMult: shareDiffMult,
		DbPath:        dbPath,
		Workers:       make(map[string]*WorkerInfo),
		usedNonces:    make(map[uint64]bool),
		MinedBlocks:   []uint64{},
	}
	p.loadShares()
	p.loadStats() // Khôi phục thống kê số lượng block đã giải của Pool
	p.loadPendingPayouts() // Khôi phục danh sách payout chờ xử lý
	return p
}

func (p *MiningPool) loadStats() {
	filePath := filepath.Join(p.DbPath, "pool_stats.json")
	file, err := os.Open(filePath)
	if err != nil {
		p.SolvedBlocks = 0
		p.MinedBlocks = []uint64{}
		return
	}
	defer file.Close()

	var stats PoolStats
	if err := json.NewDecoder(file).Decode(&stats); err == nil {
		p.SolvedBlocks = stats.SolvedBlocks
		p.MinedBlocks = stats.MinedBlocks
		if p.MinedBlocks == nil {
			p.MinedBlocks = []uint64{}
		}
		log.Printf("[POOL] 📂 Đã khôi phục số lượng khối đã giải từ pool_stats.json: %d khối | Danh sách: %v", p.SolvedBlocks, p.MinedBlocks)
	} else {
		p.SolvedBlocks = 0
		p.MinedBlocks = []uint64{}
	}
}

func (p *MiningPool) SaveStats() {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	filePath := filepath.Join(p.DbPath, "pool_stats.json")
	os.MkdirAll(p.DbPath, 0755)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("[POOL] ⚠️ Không thể lưu trữ pool_stats: %v", err)
		return
	}
	defer file.Close()

	stats := PoolStats{
		SolvedBlocks: p.SolvedBlocks,
		MinedBlocks:  p.MinedBlocks,
	}
	json.NewEncoder(file).Encode(stats)
}


func (p *MiningPool) loadShares() {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	filePath := filepath.Join(p.DbPath, "pool_shares.json")
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	var data map[string]*WorkerInfo
	if err := json.NewDecoder(file).Decode(&data); err == nil {
		p.Workers = data
	}
}

func (p *MiningPool) SaveShares() {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	filePath := filepath.Join(p.DbPath, "pool_shares.json")
	os.MkdirAll(p.DbPath, 0755)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("[POOL] ⚠️ Không thể lưu trữ pool_shares: %v", err)
		return
	}
	defer file.Close()

	json.NewEncoder(file).Encode(p.Workers)
}

func (p *MiningPool) GetWorkerDifficulty(workerAddr string) uint64 {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	w, exists := p.Workers[workerAddr]
	if !exists {
		w = &WorkerInfo{
			Address:    workerAddr,
			Difficulty: p.ShareDiffMult,
		}
		p.Workers[workerAddr] = w
	}
	if w.Difficulty == 0 {
		w.Difficulty = p.ShareDiffMult
	}
	if w.Difficulty == 0 {
		w.Difficulty = 1
	}
	return w.Difficulty
}

func (p *MiningPool) RegisterShare(workerAddr string, submittedDiff uint64) {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	w, exists := p.Workers[workerAddr]
	if !exists {
		w = &WorkerInfo{
			Address:    workerAddr,
			Difficulty: submittedDiff,
		}
		p.Workers[workerAddr] = w
	}
	if w.Difficulty == 0 {
		w.Difficulty = submittedDiff
	}
	if w.Difficulty == 0 {
		w.Difficulty = p.ShareDiffMult
	}
	if w.Difficulty == 0 {
		w.Difficulty = 1
	}

	w.Shares += float64(submittedDiff)
	w.LastSeen = time.Now().Unix()

	now := time.Now()
	if !w.LastSubmit.IsZero() {
		interval := now.Sub(w.LastSubmit)
		// Giới hạn buffer submit times để tránh rò rỉ bộ nhớ
		if len(w.SubmitTimes) < 20 {
			w.SubmitTimes = append(w.SubmitTimes, interval)
		}
	}
	w.LastSubmit = now

	if len(w.SubmitTimes) >= 10 {
		var totalDuration time.Duration
		for _, d := range w.SubmitTimes {
			totalDuration += d
		}
		avgInterval := totalDuration / 10
		w.SubmitTimes = nil

		oldDiff := w.Difficulty
		if avgInterval < 10*time.Second {
			w.Difficulty = w.Difficulty * 2
			log.Printf("[POOL-VARDIFF] Thợ đào %s nộp share quá nhanh (tb %v). Tăng độ khó: %d -> %d", workerAddr, avgInterval, oldDiff, w.Difficulty)
		} else if avgInterval > 20*time.Second {
			if w.Difficulty > 1 {
				w.Difficulty = w.Difficulty / 2
				log.Printf("[POOL-VARDIFF] Thợ đào %s nộp share quá chậm (tb %v). Giảm độ khó: %d -> %d", workerAddr, avgInterval, oldDiff, w.Difficulty)
			}
		}
	}
}

func (p *MiningPool) UpdateHashrate(workerAddr string, hashrate uint64) {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	w, exists := p.Workers[workerAddr]
	if exists {
		w.Hashrate = hashrate
		w.LastSeen = time.Now().Unix()
	}
}

func (p *MiningPool) ResetShares() {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	for addr, w := range p.Workers {
		if w.LastSeen < cutoff {
			delete(p.Workers, addr)
		} else {
			w.Shares = 0
		}
	}
	p.SaveShares()
}

func (p *MiningPool) VerifyShare(headerHash []byte, nonce uint64, difficulty []byte, height uint64, workerDiff uint64) (isPoolShare bool, isNetworkBlock bool) {
	if len(headerHash) != 32 {
		return false, false
	}

	material := make([]byte, 40)
	copy(material[:32], headerHash)
	binary.LittleEndian.PutUint64(material[32:], nonce)

	hashResult := make([]byte, 32)
	go_bridge.CalculateBlake3Hash(material, hashResult, height)

	hashBig := new(big.Int).SetBytes(reverseBytes(hashResult))
	netTarget := difficultyToTarget(difficulty)
	
	maxU256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	shareTarget := maxU256
	if workerDiff > 1 {
		shareTarget = new(big.Int).Div(maxU256, big.NewInt(int64(workerDiff)))
	}

	isPoolShare = hashBig.Cmp(shareTarget) < 0
	isNetworkBlock = hashBig.Cmp(netTarget) < 0

	return isPoolShare, isNetworkBlock
}

func difficultyToTarget(difficultyBytes []byte) *big.Int {
	diffPadded := make([]byte, 32)
	copy(diffPadded, difficultyBytes)

	reversed := reverseBytes(diffPadded)
	diffBig := new(big.Int).SetBytes(reversed)

	maxU256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if diffBig.Cmp(big.NewInt(1)) <= 0 {
		return maxU256
	}

	target := new(big.Int).Div(maxU256, diffBig)
	return target
}

func reverseBytes(b []byte) []byte {
	r := make([]byte, len(b))
	for i := range b {
		r[i] = b[len(b)-1-i]
	}
	return r
}

// CheckRateLimit kiểm tra tần suất submit share của thợ đào (tối thiểu 200ms)
func (p *MiningPool) CheckRateLimit(workerAddr string) bool {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	w, exists := p.Workers[workerAddr]
	if !exists {
		return true // Chưa có lịch sử, cho phép nộp
	}

	if !w.LastSubmit.IsZero() && time.Since(w.LastSubmit) < 200*time.Millisecond {
		return false // Vi phạm rate limit
	}
	return true
}

// CheckAndRegisterNonce kiểm tra và đánh dấu nonce đã sử dụng một cách nguyên tử
func (p *MiningPool) CheckAndRegisterNonce(nonce uint64, height uint64) bool {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	if p.usedNonces == nil {
		p.usedNonces = make(map[uint64]bool)
		p.currentHeight = height
	}

	// Reset cache nếu mạng lưới chuyển sang block mới
	if p.currentHeight != height {
		p.usedNonces = make(map[uint64]bool)
		p.currentHeight = height
	}

	if p.usedNonces[nonce] {
		return false // Nonce đã được sử dụng
	}

	p.usedNonces[nonce] = true // Đánh dấu đã dùng ngay lập tức
	return true
}

// UnregisterNonce mở khóa nonce trong trường hợp xác thực thất bại
func (p *MiningPool) UnregisterNonce(nonce uint64) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.usedNonces != nil {
		delete(p.usedNonces, nonce)
	}
}

func (p *MiningPool) savePendingPayouts() {
	filePath := filepath.Join(p.DbPath, "pool_pending_payouts.json")
	os.MkdirAll(p.DbPath, 0755)
	data, err := json.Marshal(p.PendingPayouts)
	if err != nil {
		log.Printf("[POOL] ❌ Lỗi mã hóa pending payouts: %v", err)
		return
	}
	ioutil.WriteFile(filePath, data, 0644)
}

func (p *MiningPool) loadPendingPayouts() {
	filePath := filepath.Join(p.DbPath, "pool_pending_payouts.json")
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		p.PendingPayouts = []BlockPayoutInfo{}
		return
	}
	if err := json.Unmarshal(data, &p.PendingPayouts); err != nil {
		p.PendingPayouts = []BlockPayoutInfo{}
	}
}

// SnapshotAndReset ghi nhận công sức đóng góp cho khối hiện tại và reset ngay lập tức
func (p *MiningPool) SnapshotAndReset(height uint64) {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	// Chụp ảnh shares
	sharesSnapshot := make(map[string]float64)
	for addr, w := range p.Workers {
		if w.Shares > 0 {
			sharesSnapshot[addr] = w.Shares
		}
	}

	// Chỉ lưu nếu có share đóng góp thực tế
	if len(sharesSnapshot) > 0 {
		info := BlockPayoutInfo{
			Height: height,
			Shares: sharesSnapshot,
		}
		p.PendingPayouts = append(p.PendingPayouts, info)
		p.savePendingPayouts()
		log.Printf("[POOL-SNAPSHOT] 📸 Đã chụp ảnh shares cho khối #%d và lưu xuống đĩa.", height)
	}

	// Reset RAM ngay lập tức về 0 để đào khối mới
	for _, w := range p.Workers {
		w.Shares = 0
	}
	p.saveSharesLocked() // Lưu shares đã reset xuống đĩa
}

// saveSharesLocked ghi shares xuống file (yêu cầu đã giữ Lock)
func (p *MiningPool) saveSharesLocked() {
	filePath := filepath.Join(p.DbPath, "pool_shares.json")
	os.MkdirAll(p.DbPath, 0755)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("[POOL] ❌ Không thể lưu trữ pool_shares: %v", err)
		return
	}
	defer file.Close()
	json.NewEncoder(file).Encode(p.Workers)
}

// AddMinedBlock ghi nhận khối trúng thành công vào danh sách
func (p *MiningPool) AddMinedBlock(height uint64) {
	p.Mu.Lock()
	// Tránh trùng lặp
	exists := false
	for _, h := range p.MinedBlocks {
		if h == height {
			exists = true
			break
		}
	}
	if !exists {
		p.MinedBlocks = append(p.MinedBlocks, height)
		p.SolvedBlocks = uint64(len(p.MinedBlocks))
	}
	p.Mu.Unlock()
	p.SaveStats()
}
