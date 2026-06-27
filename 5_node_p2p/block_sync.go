/**
 * @file block_sync.go
 * @brief Giao thức đồng bộ toàn bộ khối (Full Block Sync) cho YonaCode Go V1.0.
 * @details Triển khai phương thức RPC 'GetBlock' phục vụ việc tải dữ liệu khối đầy đủ và giao dịch thiếu hụt.
 *  - Lớp 1: Cảnh vệ vòng ngoài - Kiểm tra kích thước và định dạng yêu cầu.
 *  - Lớp 4: Tình báo (ICS) - Ghi log yêu cầu phục vụ hậu kiểm.
 */

package node_p2p

import (
	pb_block "btc_genz/proto"
	"context"
	"io"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"
	"errors"
)

const (
	BlockTxnProtocol = "/btc_gen_z/block_txn/1.0.0" // Giao thức đồng bộ giao dịch thiếu hụt trong Compact Block
)


// RegisterBlockSyncHandler đăng ký bộ xử lý yêu cầu tải khối đầy đủ qua SyncProtocol và các giao dịch thiếu qua BlockTxnProtocol
func (n *NetworkManager) RegisterBlockSyncHandler() {
	n.Host.SetStreamHandler(SyncProtocol, func(s network.Stream) {
		defer s.Close()
		
		// Lớp 1: Cảnh vệ vòng ngoài - Giới hạn kích thước request ( GetBlockRequest rất nhỏ)
		// [VANGUARD-STABILITY] Thiết lập Read Deadline tránh treo vĩnh viễn khi peer chậm/treo
		s.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 1024))
		if err != nil {
			return
		}
		
		var req pb_block.GetBlockRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			log.Printf("[P2P-SYNC-ERR] ❌ Lỗi giải mã yêu cầu từ %s: %v", s.Conn().RemotePeer().String()[:12], err)
			return
		}
		
		// Lớp 4: Tình báo (ICS) - Ghi nhận yêu cầu
		// log.Printf("[P2P-SYNC] 📥 Peer %s yêu cầu toàn bộ Khối #%d", s.Conn().RemotePeer().String()[:12], req.Height)
		
		// Truy vấn dữ liệu từ Unified Ledger (Rust Core)
		var blockRaw []byte
		if len(req.Hash) > 0 {
			// [VANGUARD-ORPHAN] Truy vấn theo mã băm cho các khối mồ côi
			blockRaw = n.Bridge.GetRawByHash(req.Hash)
		} else {
			// Truy vấn theo chiều cao truyền thống
			blockRaw = n.Bridge.GetBlock(req.Height)
		}
		
		var rData []byte
		if blockRaw != nil {
			// [BIG-BLOCK-OOM] Tối ưu hóa bộ nhớ: Tự đóng gói bytes thô mà không cần proto.Unmarshal + proto.Marshal
			// GetBlockResponse: tag 0x0a (field 1, length-delimited block) + length + blockRaw + tag 0x10 (field 2, varint found) + 0x01
			bLenBytes := encodeVarintBytes(uint64(len(blockRaw)))
			rData = append(rData, 0x0a)
			rData = append(rData, bLenBytes...)
			rData = append(rData, blockRaw...)
			rData = append(rData, 0x10, 0x01) // found = true
		} else {
			rData = []byte{0x10, 0x00} // found = false
		}
		
		_, err = s.Write(rData)
		if err != nil {
			// Peer có thể đã đóng stream sớm
			return
		}
	})

	// [VANGUARD-COMPACT] Register BlockTxn Handler phục vụ yêu cầu giao dịch thiếu hụt
	n.Host.SetStreamHandler(BlockTxnProtocol, func(s network.Stream) {
		defer s.Close()
		
		// [VANGUARD-STABILITY] Thiết lập Read Deadline tránh treo vĩnh viễn khi peer chậm/treo
		s.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 64*1024)) // Đủ rộng cho missing_indexes
		if err != nil { return }
		
		var req pb_block.GetBlockTxn
		if err := proto.Unmarshal(data, &req); err != nil {
			log.Printf("[P2P-TXN-ERR] ❌ Lỗi giải mã GetBlockTxn từ %s: %v", s.Conn().RemotePeer().String()[:12], err)
			return
		}
		
		var blockRaw []byte
		if len(req.BlockHash) == 32 {
			blockRaw = n.Bridge.GetRawByHash(req.BlockHash)
		}
		
		if blockRaw != nil {
			txs, err := ExtractTransactionsFromBlockBytes(blockRaw)
			if err == nil && len(txs) > 0 {
				for _, idx := range req.MissingIndexes {
					if int(idx) < len(txs) {
						txBytes := txs[idx]
						length := uint64(len(txBytes))
						var varintBuf [10]byte
						idxVar := 0
						val := length
						for val >= 0x80 {
							varintBuf[idxVar] = byte(val | 0x80)
							val >>= 7
							idxVar++
						}
						varintBuf[idxVar] = byte(val)
						lengthBuf := varintBuf[:idxVar+1]

						// Gửi tag 0x0a cho field transactions của BlockTxn, sau đó là length và txBytes
						if _, err := s.Write(append(append([]byte{0x0a}, lengthBuf...), txBytes...)); err != nil {
							break
						}
					}
				}
			}
		}
	})
	
	log.Printf("[P2P-BOOT] ✅ Đã kích hoạt Giao thức Đồng bộ Khối & Giao dịch thiếu hụt (Compact Block Txn Protocol)")
}

// RequestBlockTxn gửi yêu cầu lấy các giao dịch bị thiếu trong Compact Block từ Peer qua P2P
func (n *NetworkManager) RequestBlockTxn(ctx context.Context, p peer.ID, blockHash []byte, missingIndexes []uint32) ([]*pb_block.Transaction, error) {
	if n.RequestBlockTxnMock != nil {
		return n.RequestBlockTxnMock(ctx, p, blockHash, missingIndexes)
	}
	if n.Host == nil {
		return nil, errors.New("host is nil (mock environment)")
	}
	s, err := n.Host.NewStream(ctx, p, BlockTxnProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	req := &pb_block.GetBlockTxn{
		BlockHash:      blockHash,
		MissingIndexes: missingIndexes,
	}
	reqData, _ := proto.Marshal(req)
	if _, err := s.Write(reqData); err != nil {
		return nil, err
	}
	s.CloseWrite()

	// [VANGUARD-STABILITY] Thiết lập Read Deadline tránh treo vĩnh viễn khi peer chậm/treo
	s.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := io.ReadAll(io.LimitReader(s, 36*1024*1024)) // Giới hạn 36MB cho giao dịch trả về
	if err != nil {
		return nil, err
	}

	var resp pb_block.BlockTxn
	if err := proto.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	return resp.Transactions, nil
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
