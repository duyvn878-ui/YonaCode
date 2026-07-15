package mining_pool

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb_block "btc_genz/proto"
	"lukechampine.com/blake3"
	"google.golang.org/protobuf/proto"
)

const RustCryptoContext = "BTC GenZ Toi Gian PoW v1.0"

type NodeBridge interface {
	GetCurrentVersion() uint64
	GetBlock(height uint64) []byte
	GetNonce(ctx interface{}, address []byte) uint64
	GetBlockHash(height uint64) []byte
	CalculateBlockRewardBtcZ(height uint64) uint64
}

func (p *MiningPool) StartPayoutEngine(bridge NodeBridge, pushTx func(txBytes []byte)) {
	go p.payoutLoop(bridge, pushTx)
}

func (p *MiningPool) payoutLoop(bridge NodeBridge, pushTx func(txBytes []byte)) {
	log.Printf("[POOL] 💰 Khởi động Payout Engine tự động cho ví: %s", p.PoolAddress)
	
	// Khởi tạo biến theo dõi nonce cục bộ của ví Pool để tránh trùng lặp khi quét nhanh nhiều khối
	var lastPaidNonce uint64
	
	// Giải mã khóa riêng tư ví Pool để ký giao dịch payout
	privKeyBytes, err := hex.DecodeString(p.PoolKeyHex)
	if err == nil && len(privKeyBytes) == 32 {
		privKeyBytes = ed25519.NewKeyFromSeed(privKeyBytes)
	}
	if err != nil || len(privKeyBytes) != ed25519.PrivateKeySize {
		log.Printf("[POOL-FATAL] ❌ Khóa riêng tư pool không hợp lệ hoặc thiếu (Yêu cầu 32 hoặc 64 bytes). Payout Engine dừng hoạt động!")
		return
	}
	poolPubBytes := make([]byte, 32)
	copy(poolPubBytes, privKeyBytes[32:])

	poolAddrBytes, _ := hex.DecodeString(strings.TrimPrefix(p.PoolAddress, "0x"))

	for {
		// Đọc chiều cao khối hiện tại từ mạng
		currH := bridge.GetCurrentVersion()
		
		// Đọc chiều cao khối payout cuối cùng đã lưu trong cơ sở dữ liệu
		lastH := p.loadLastPaidHeight()

		// Đợi 20 khối confirm để tránh fork mồ côi (reorg)
		if currH < 20 || lastH >= currH - 20 {
			time.Sleep(15 * time.Second)
			continue
		}

		targetH := currH - 20
		log.Printf("[POOL-PAYOUT] ⚡ Phát hiện Pool bị tụt hậu so với mạng lưới. Bắt đầu quét nhanh từ khối #%d đến khối #%d...", lastH+1, targetH)

		lastSavedH := lastH

		// Vòng lặp quét nhanh liên tục không sleep để bắt kịp chiều cao mạng
		for nextH := lastH + 1; nextH <= targetH; nextH++ {
			blockRaw := bridge.GetBlock(nextH)
			if len(blockRaw) == 0 {
				log.Printf("[POOL-PAYOUT] ⚠️ Khối #%d chưa sẵn sàng trên Node. Tạm dừng đợt quét nhanh để chờ đồng bộ.", nextH)
				break
			}

			var block pb_block.Block
			if err := proto.Unmarshal(blockRaw, &block); err != nil {
				log.Printf("[POOL-ERROR] ❌ Lỗi giải mã khối #%d: %v. Bỏ qua khối lỗi này.", nextH, err)
				lastSavedH = nextH
				continue
			}

			if block.Body != nil && len(block.Body.Transactions) > 0 {
				// Giao dịch Coinbase ở index 0
				coinbaseTx := block.Body.Transactions[0]
				if coinbaseTx.Sender == nil {
					coinbaseReceiverHex := hex.EncodeToString(coinbaseTx.Receiver.Value)
					expectedPoolHex := strings.TrimPrefix(p.PoolAddress, "0x")

					if strings.ToLower(coinbaseReceiverHex) == strings.ToLower(expectedPoolHex) {
						// Tính tổng phần thưởng thực tế: Block Reward (hỏi Rust qua bridge) + Phí giao dịch (fees)
						var totalFees uint64
						if block.Body != nil {
							for _, tx := range block.Body.Transactions {
								totalFees += tx.Fee
							}
						}
						blockReward := bridge.CalculateBlockRewardBtcZ(nextH)
						actualReward := blockReward + totalFees

						log.Printf("[POOL-WIN] 🏆 Phát hiện Pool đã đào thành công khối #%d! Phần thưởng thực tế: %.8f GO (Block: %.8f, Fees: %.8f). Tiến hành chia thưởng...", nextH, float64(actualReward)/1e8, float64(blockReward)/1e8, float64(totalFees)/1e8)
						
						// Tìm bản chụp shares cho khối nextH
						p.Mu.Lock()
						var targetShares map[string]float64
						targetIdx := -1
						for idx, info := range p.PendingPayouts {
							if info.Height == nextH {
								targetShares = info.Shares
								targetIdx = idx
								break
							}
						}
						p.Mu.Unlock()

						if targetShares != nil {
							// Thực hiện chia thưởng dựa trên bản chụp, truyền con trỏ lastPaidNonce
							p.processBlockPayout(actualReward, nextH, targetShares, poolAddrBytes, privKeyBytes, bridge, pushTx, &lastPaidNonce)
							
							// Xóa bản ghi đã payout khỏi danh sách
							p.Mu.Lock()
							if targetIdx >= 0 && targetIdx < len(p.PendingPayouts) {
								p.PendingPayouts = append(p.PendingPayouts[:targetIdx], p.PendingPayouts[targetIdx+1:]...)
								p.savePendingPayouts()
							}
							p.Mu.Unlock()
						} else {
							log.Printf("[POOL-PAYOUT] ⚠️ Không tìm thấy bản chụp shares cho khối #%d (Có thể do khối cũ trước nâng cấp). Bỏ qua chia thưởng.", nextH)
						}
					} else {
						// Khối tại chiều cao nextH trên chuỗi chính không phải của Pool.
						// Kiểm tra xem trong danh sách chờ có bản chụp nào của nextH không.
						// Nếu có, chứng tỏ khối của Pool tại chiều cao này đã bị mồ côi vĩnh viễn (Orphaned).
						p.Mu.Lock()
						targetIdx := -1
						for idx, info := range p.PendingPayouts {
							if info.Height == nextH {
								targetIdx = idx
								break
							}
						}
						if targetIdx >= 0 {
							p.PendingPayouts = append(p.PendingPayouts[:targetIdx], p.PendingPayouts[targetIdx+1:]...)
							p.savePendingPayouts()
							log.Printf("[POOL-REORG] ⚠️ Phát hiện khối #%d do Pool giải trước đó đã bị mồ côi vĩnh viễn (Orphaned). Đã dọn dẹp bản chụp shares để giải phóng bộ nhớ.", nextH)
						}
						p.Mu.Unlock()
					}
				}
			}

			lastSavedH = nextH

			// Tối ưu hóa I/O: Chỉ ghi file lưu trạng thái sau mỗi 100 khối để giảm tải ghi SSD
			if nextH%100 == 0 {
				p.saveLastPaidHeight(nextH)
			}
		}

		// Lưu chiều cao khối cuối cùng đã quét thành công của đợt này
		p.saveLastPaidHeight(lastSavedH)
		log.Printf("[POOL-PAYOUT] 🏁 Hoàn thành đợt quét nhanh khối. Chiều cao quét đã được đồng bộ lên khối #%d.", lastSavedH)

		// Nghỉ ngơi 5 giây trước khi bắt đầu đợt check tiếp theo
		time.Sleep(5 * time.Second)
	}
}

func (p *MiningPool) processBlockPayout(totalReward uint64, height uint64, shares map[string]float64, poolAddrBytes []byte, poolPrivKey []byte, bridge NodeBridge, pushTx func(txBytes []byte), lastPaidNonce *uint64) {
	// 1. Tính tổng shares và đếm số lượng worker hợp lệ
	var totalShares float64
	var activeWorkerAddrs []string
	for addr, shareVal := range shares {
		if shareVal > 0 {
			totalShares += shareVal
			activeWorkerAddrs = append(activeWorkerAddrs, addr)
		}
	}

	if totalShares <= 0 || len(activeWorkerAddrs) == 0 {
		log.Printf("[POOL-PAYOUT] ⚠️ Không có shares thợ đào nào được ghi nhận cho khối #%d. Thưởng thuộc về Pool Owner.", height)
		return
	}

	log.Printf("[POOL-PAYOUT] 📊 Thực hiện chia thưởng khối #%d cho thợ đào (Dựa trên bản chụp). Tổng shares: %.2f | Số thợ đào: %d", height, totalShares, len(activeWorkerAddrs))

	// Lấy nonce từ Node và đồng bộ với nonce cục bộ của ví Pool
	nodeNonce := bridge.GetNonce(nil, poolAddrBytes)
	if nodeNonce > *lastPaidNonce {
		*lastPaidNonce = nodeNonce
	}
	currentNonce := *lastPaidNonce
	recentBlockHash := bridge.GetBlockHash(height - 1)

	// Trừ phí bể đào (ví dụ 1%)
	poolFeeAmount := uint64(float64(totalReward) * p.PoolFee)
	netReward := totalReward - poolFeeAmount

	var allocatedReward uint64 // Theo dõi tổng số tiền đã phân phối

	// 2. Sinh các Payout Txs và chia thưởng chính xác 100%
	for idx, addr := range activeWorkerAddrs {
		shareVal := shares[addr]

		var workerAmount uint64
		if idx == len(activeWorkerAddrs)-1 {
			// Worker cuối cùng nhận toàn bộ phần còn dư để triệt tiêu sai số làm tròn số lẻ
			workerAmount = netReward - allocatedReward
		} else {
			// Tính theo tỷ lệ đóng góp share
			workerAmount = uint64(float64(netReward) * (shareVal / totalShares))
		}

		allocatedReward += workerAmount

		if workerAmount <= 250 {
			// Bỏ qua nếu nhỏ hơn hoặc bằng phí giao dịch 250 VNT
			continue
		}

		workerPayable := workerAmount - 250 // Trừ phí giao dịch 250 VNT

		workerAddrBytes, err := hex.DecodeString(strings.TrimPrefix(addr, "0x"))
		if err != nil || len(workerAddrBytes) != 32 {
			continue
		}

		// Tạo giao dịch payout
		tx := &pb_block.Transaction{
			Version: 1,
			Sender: &pb_block.Address{
				Value: poolAddrBytes,
			},
			Receiver: &pb_block.Address{
				Value: workerAddrBytes,
			},
			Amount:          workerPayable,
			Fee:             250,
			Nonce:           currentNonce,
			Timestamp:       uint64(time.Now().Unix()),
			RecentBlockHash: recentBlockHash,
			ChainId:         25062025,
		}

		// Ký giao dịch
		hash := GetSigningHashNative(tx)
		sig := ed25519.Sign(poolPrivKey, hash)
		tx.Signature = &pb_block.Signature{
			Value: sig,
		}

		txBytes, _ := proto.Marshal(tx)
		pushTx(txBytes)

		log.Printf("[POOL-PAYOUT] 💸 Payout %.8f GO gửi tới ví thợ đào %s (Shares bản chụp: %.2f, Nonce: %d)", float64(workerPayable)/1e8, addr, shareVal, currentNonce)
		currentNonce++

		// Tạo độ giãn cách 15 giây giữa các giao dịch payout (ngoại trừ worker cuối cùng)
		if idx < len(activeWorkerAddrs)-1 {
			log.Printf("[POOL-PAYOUT] ⏳ Nghỉ 15 giây trước khi thực hiện giao dịch tiếp theo...")
			time.Sleep(15 * time.Second)
		}
	}
	// Lưu lại nonce mới nhất sau khi hoàn tất payout khối này cho toàn bộ workers
	*lastPaidNonce = currentNonce
}

func (p *MiningPool) loadLastPaidHeight() uint64 {
	filePath := filepath.Join(p.DbPath, "pool_payout_height.json")
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return 0
	}
	var res struct {
		Height uint64 `json:"height"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return 0
	}
	return res.Height
}

func (p *MiningPool) saveLastPaidHeight(height uint64) {
	filePath := filepath.Join(p.DbPath, "pool_payout_height.json")
	os.MkdirAll(p.DbPath, 0755)
	
	res := struct {
		Height uint64 `json:"height"`
	}{Height: height}
	
	data, _ := json.Marshal(res)
	ioutil.WriteFile(filePath, data, 0644)
}

func GetSigningHashNative(tx *pb_block.Transaction) []byte {
	var buf bytes.Buffer
	var tmp8 [8]byte
	binary.LittleEndian.PutUint64(tmp8[:], tx.Version)
	buf.Write(tmp8[:])

	var tmp4 [4]byte
	if tx.Sender != nil && len(tx.Sender.Value) > 0 {
		binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.Sender.Value)))
		buf.Write(tmp4[:])
		buf.Write(tx.Sender.Value)
	} else {
		binary.LittleEndian.PutUint32(tmp4[:], 0)
		buf.Write(tmp4[:])
	}

	if tx.Receiver != nil && len(tx.Receiver.Value) > 0 {
		binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.Receiver.Value)))
		buf.Write(tmp4[:])
		buf.Write(tx.Receiver.Value)
	} else {
		binary.LittleEndian.PutUint32(tmp4[:], 0)
		buf.Write(tmp4[:])
	}

	binary.LittleEndian.PutUint64(tmp8[:], tx.Amount)
	buf.Write(tmp8[:])

	binary.LittleEndian.PutUint64(tmp8[:], tx.Fee)
	buf.Write(tmp8[:])

	binary.LittleEndian.PutUint64(tmp8[:], tx.Nonce)
	buf.Write(tmp8[:])

	binary.LittleEndian.PutUint64(tmp8[:], tx.Timestamp)
	buf.Write(tmp8[:])

	binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.RecentBlockHash)))
	buf.Write(tmp4[:])
	buf.Write(tx.RecentBlockHash)

	binary.LittleEndian.PutUint64(tmp8[:], tx.ChainId)
	buf.Write(tmp8[:])

	hash := make([]byte, 32)
	blake3.DeriveKey(hash, RustCryptoContext, buf.Bytes())
	return hash
}
