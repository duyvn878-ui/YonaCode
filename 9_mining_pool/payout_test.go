package mining_pool

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/rand"
	"testing"

	pb_block "btc_genz/proto"

	"google.golang.org/protobuf/proto"
)

// MockBridge giả lập NodeBridge để phục vụ unit test
type MockBridge struct {
	height     uint64
	blockBytes []byte
	nonces     map[string]uint64
}

func (m *MockBridge) GetCurrentVersion() uint64 {
	return m.height
}

func (m *MockBridge) GetBlock(height uint64) []byte {
	return m.blockBytes
}

func (m *MockBridge) GetNonce(ctx interface{}, address []byte) uint64 {
	addrHex := hex.EncodeToString(address)
	return m.nonces[addrHex]
}

func (m *MockBridge) GetBlockHash(height uint64) []byte {
	h := make([]byte, 32)
	rand.Read(h)
	return h
}

func (m *MockBridge) CalculateBlockRewardBtcZ(height uint64) uint64 {
	return 125000000 // Giả lập phần thưởng khối cố định là 1.25 GO (125,000,000 VNT)
}

func TestProcessBlockPayout(t *testing.T) {
	// Khởi tạo MiningPool giả lập
	poolAddr := "0x680303fe459c4622e35c279347755db9b1139776fab81f83d8eaa141fa080146"
	poolKey := "b9eb2ced135bf21e54f8ee53373477435ccfb297b892778976e65c4267291fc0"
	
	// Tạo thư mục tạm để lưu stats
	p := NewMiningPool(poolAddr, poolKey, 0.01, 100, t.TempDir())

	// Đăng ký 3 worker với shares khác nhau để kiểm tra tỷ lệ
	workerA := "0xd253f4d1e9567a181c28bcc280f6d3ef2b8cbe373043f7bd8076aa0e15ef50c8"
	workerB := "0xa639401ae8b969ff054c14d09e5282f9dfcdbac7de8c47bb49f6fb8a0375387c"
	workerC := "0x2902a4fc82692b9b1f9de6def28ea2d167e744927eba2ab6203f58da034737b9"

	p.RegisterShare(workerA, 500) // 500 shares
	p.RegisterShare(workerB, 300) // 300 shares
	p.RegisterShare(workerC, 200) // 200 shares (Tổng cộng 1000 shares)

	// Giả lập Bridge
	mock := &MockBridge{
		height: 100,
		nonces: make(map[string]uint64),
	}
	poolAddrBytes, _ := hex.DecodeString(poolAddr[2:])
	mock.nonces[hex.EncodeToString(poolAddrBytes)] = 10 // Nonce ban đầu là 10

	// Tổng quỹ thưởng khối là 1.25 GO (125,000,000 VNT)
	totalReward := uint64(125000000)

	// Thu thập các giao dịch payout được tạo ra
	var generatedTxs []*pb_block.Transaction
	pushTxMock := func(txBytes []byte) {
		var tx pb_block.Transaction
		if err := proto.Unmarshal(txBytes, &tx); err == nil {
			generatedTxs = append(generatedTxs, &tx)
		}
	}

	// Thực hiện chụp ảnh shares (Snapshot) và reset shares trong RAM về 0
	p.SnapshotAndReset(100)

	// Kiểm tra xem bản chụp đã được lưu vào PendingPayouts và shares trong RAM đã được reset về 0 chưa
	if len(p.PendingPayouts) != 1 || p.PendingPayouts[0].Height != 100 {
		t.Fatalf("Snapshot shares thất bại cho khối #100")
	}
	targetShares := p.PendingPayouts[0].Shares

	privKeyBytes, _ := hex.DecodeString(poolKey)
	if len(privKeyBytes) == 32 {
		privKeyBytes = ed25519.NewKeyFromSeed(privKeyBytes)
	}
	var lastPaidNonce uint64
	p.processBlockPayout(totalReward, 100, targetShares, poolAddrBytes, privKeyBytes, mock, pushTxMock, &lastPaidNonce, 100)

	// 1. Kiểm toán số lượng giao dịch sinh ra
	if len(generatedTxs) != 3 {
		t.Errorf("Kỳ vọng 3 giao dịch payout, thực tế nhận được: %d", len(generatedTxs))
	}

	// 2. Kiểm toán tổng số tiền phân chia và phí chuyển tiền
	// PoolFee là 1% -> poolFeeAmount = 1,250,000 VNT -> netReward = 123,750,000 VNT
	expectedNetReward := uint64(123750000)
	var totalDistributedAmount uint64

	for _, tx := range generatedTxs {
		// Số tiền thực tế chuyển cho thợ đào + phí giao dịch (250) phải khớp với phần phân bổ
		actualWorkerReward := tx.Amount + tx.Fee
		totalDistributedAmount += actualWorkerReward

		receiverHex := "0x" + hex.EncodeToString(tx.Receiver.Value)
		
		// Kiểm tra phân chia đúng tỷ lệ share
		if receiverHex == workerA && actualWorkerReward != 61875000 {
			t.Errorf("Worker A nhận sai thưởng: kỳ vọng 61875000, thực tế %d", actualWorkerReward)
		}
		if receiverHex == workerB && actualWorkerReward != 37125000 {
			t.Errorf("Worker B nhận sai thưởng: kỳ vọng 37125000, thực tế %d", actualWorkerReward)
		}
		if receiverHex == workerC && actualWorkerReward != 24750000 {
			t.Errorf("Worker C nhận sai thưởng: kỳ vọng 24750000, thực tế %d", actualWorkerReward)
		}
	}

	// 3. Xác minh tổng số tiền chia khớp chính xác 100% với netReward (triệt tiêu sai số làm tròn số lẻ)
	if totalDistributedAmount != expectedNetReward {
		t.Errorf("Lệch số dư tổng quỹ chia: kỳ vọng %d, thực tế %d (lệch %d VNT)", expectedNetReward, totalDistributedAmount, int64(expectedNetReward)-int64(totalDistributedAmount))
	}

	// 4. Xác minh reset shares về 0 sau khi SnapshotAndReset thành công
	p.Mu.RLock()
	defer p.Mu.RUnlock()
	for addr, w := range p.Workers {
		if w.Shares != 0 {
			t.Errorf("Shares của worker %s không được reset về 0 (Thực tế: %.2f)", addr, w.Shares)
		}
	}
}
