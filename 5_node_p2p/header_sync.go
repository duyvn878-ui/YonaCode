/**
 * @file header_sync.go
 * @brief Giao thức đồng bộ tiêu đề khối (Header Sync) cho YonaCode Minimalist V1.0.
 * @details Triển khai phương thức RPC 'GetHeaderHash' phục vụ thuật toán tìm điểm rẽ nhánh (Fork Choice Rule).
 * Tuyệt đối không để xảy ra tình trạng mồ côi (Orphan) không xác định.
 */

package node_p2p

import (
	"fmt"
	"log"
	pb_block "btc_genz/proto"
	"context"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"
)

const (
	HeaderSyncProtocol      = "/btc_gen_z/header_sync/1.0.0"
	HeaderBatchSyncProtocol = "/btc_gen_z/header_batch/1.0.0" // Giao thức đồng bộ hàng loạt
	HeaderByHashProtocol    = "/btc_gen_z/header_by_hash/1.0.0"  // Giao thức đồng bộ tiêu đề khối theo Hash
)

// RegisterHeaderSyncHandler đăng ký bộ xử lý yêu cầu Hash và Batch Header
func (n *NetworkManager) RegisterHeaderSyncHandler() {
	// 1. Cũ: Single Header
	// [BIG-BLOCK-OOM] Tối ưu hóa bộ nhớ: Sử dụng cơ chế phân tích byte-level để tránh Unmarshal khối 100MB gây tràn RAM.
	n.Host.SetStreamHandler(HeaderSyncProtocol, func(s network.Stream) {
		defer s.Close()
		
		peerID := s.Conn().RemotePeer()
		
		// [SECURITY-HARDENING] Chỉ phục vụ peer đã hoàn thành Handshake
		n.PeerMutex.RLock()
		_, hasHandshaked := n.PeerHeights[peerID]
		n.PeerMutex.RUnlock()
		if !hasHandshaked {
			log.Printf("[SYNC-HEADER] 🛡️ Từ chối yêu cầu từ peer chưa handshake: %s", peerID.String()[:12])
			return
		}
		
		// [VANGUARD-FIX] Bỏ hoàn toàn Rate Limit theo thời gian (100ms) theo cơ chế Bitcoin Core (Initial Block Download - IBD).
		// Tốc độ đồng bộ chỉ bị giới hạn bởi kích thước gói tin và băng thông mạng, tránh tình trạng tự ban Peer oan.
		
		// [MAINNET-TIMEOUT] Tăng read deadline lên 15 giây để tránh ngắt kết nối bắt tay sớm khi mạng có độ trễ lớn
		s.SetReadDeadline(time.Now().Add(15 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 1024))
		if err != nil { return }
		
		var req pb_block.GetBlockRequest
		if err := proto.Unmarshal(data, &req); err != nil { return }
		
		hash := n.Bridge.GetBlockHash(req.Height)
		var rData []byte
		if len(hash) > 0 {
			headerBytes := n.Bridge.GetHeaderRaw(hash)
			if len(headerBytes) > 0 && n.AuditHeaderBytesBeforeSend(req.Height, headerBytes) {
				rData = BuildHeaderOnlyResponseBytes(headerBytes)
			} else {
				log.Printf("[SYNC-HEADER] 🛡️ Từ chối gửi Header #%d hoặc lỗi kiểm toán", req.Height)
				rData = []byte{0x10, 0x00} // found = false
			}
		} else {
			rData = []byte{0x10, 0x00} // found = false
		}
		
		s.Write(rData)
		s.CloseWrite()
	})

	// 2. Mới [V5.0]: Batch Header Sync (Cho Ultralight Bootstrap)
	// [BIG-BLOCK-OOM] Tối ưu hóa bộ nhớ: Trực tiếp lấy Header từ RocksDB CF_HEADERS để tránh Unmarshal khối lớn hoặc lỗi do Snap Sync bị pruned body.
	n.Host.SetStreamHandler(HeaderBatchSyncProtocol, func(s network.Stream) {
		defer s.Close()
		
		peerID := s.Conn().RemotePeer()
		
		// [SECURITY-HARDENING] Chỉ phục vụ peer đã hoàn thành Handshake
		n.PeerMutex.RLock()
		_, hasHandshaked := n.PeerHeights[peerID]
		n.PeerMutex.RUnlock()
		if !hasHandshaked {
			log.Printf("[P2P-HEADER] 🛡️ Từ chối yêu cầu batch từ peer chưa handshake: %s", peerID.String()[:12])
			return
		}
		
		// [VANGUARD-FIX] Bỏ hoàn toàn Rate Limit theo thời gian theo cơ chế Bitcoin Core.
		
		// [MAINNET-TIMEOUT] Tăng read deadline lên 30 giây để đảm bảo node có đủ thời gian xử lý và tải các batch header lớn
		s.SetReadDeadline(time.Now().Add(30 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 1024))
		if err != nil { return }
		
		var req pb_block.GetHeaderBatchRequest
		if err := proto.Unmarshal(data, &req); err != nil { return }
		
		log.Printf("[P2P-HEADER] 📚 Peer %s yêu cầu %d Header từ #%d", s.Conn().RemotePeer(), req.Count, req.StartHeight)
		
		var resp pb_block.GetHeaderBatchResponse
		if req.Count > 10000 { req.Count = 10000 } // 
		
		for i := uint32(0); i < req.Count; i++ {
			h := req.StartHeight + uint64(i)
			hash := n.Bridge.GetBlockHash(h)
			if len(hash) == 0 { break }
			
			headerBytes := n.Bridge.GetHeaderRaw(hash)
			if len(headerBytes) == 0 { break }

			// [VANGUARD-SELF-AUDIT] Sử dụng kiểm toán Header chuyên biệt trước khi gửi
			if !n.AuditHeaderBytesBeforeSend(h, headerBytes) {
				log.Printf("[P2P-HEADER] 🛡️ Phát hiện khối lỗi #%d trong batch, dừng phục vụ batch này.", h)
				break
			}

			resp.Headers = append(resp.Headers, headerBytes)
		}
		
		rData, _ := proto.Marshal(&resp)
		s.Write(rData)
		s.CloseWrite()
	})

	// 3. Mới [V5.1]: Get Header By Hash (Cho Light-weight Backward Header Sync)
	// [BIG-BLOCK-OOM] Tối ưu hóa bộ nhớ: Trực tiếp lấy Header từ RocksDB theo Hash, giúp đồng bộ ổn định kể cả khi node nguồn chạy Snap Sync.
	n.Host.SetStreamHandler(HeaderByHashProtocol, func(s network.Stream) {
		defer s.Close()
		
		peerID := s.Conn().RemotePeer()
		
		// [SECURITY-HARDENING] Chỉ phục vụ peer đã hoàn thành Handshake
		n.PeerMutex.RLock()
		_, hasHandshaked := n.PeerHeights[peerID]
		n.PeerMutex.RUnlock()
		if !hasHandshaked {
			log.Printf("[SYNC-HEADER-HASH] 🛡️ Từ chối yêu cầu theo hash từ peer chưa handshake: %s", peerID.String()[:12])
			return
		}
		
		// [VANGUARD-FIX] Bỏ hoàn toàn Rate Limit theo thời gian theo cơ chế Bitcoin Core.
		
		// [MAINNET-TIMEOUT] Tăng read deadline lên 15 giây để xử lý yêu cầu header theo hash ổn định
		s.SetReadDeadline(time.Now().Add(15 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 1024))
		if err != nil { return }
		
		var req pb_block.GetBlockRequest
		if err := proto.Unmarshal(data, &req); err != nil { return }
		
		var rData []byte
		if len(req.Hash) == 32 {
			headerBytes := n.Bridge.GetHeaderRaw(req.Hash)
			if len(headerBytes) > 0 {
				height, errHeight := ExtractHeightFromHeaderBytes(headerBytes)
				if errHeight == nil && n.AuditHeaderBytesBeforeSend(height, headerBytes) {
					rData = BuildHeaderOnlyResponseBytes(headerBytes)
				} else {
					rData = []byte{0x10, 0x00} // found = false
				}
			} else {
				rData = []byte{0x10, 0x00} // found = false
			}
		} else {
			rData = []byte{0x10, 0x00} // found = false
		}
		
		s.Write(rData)
		s.CloseWrite()
	})
}

// DownloadHeaderBatch yêu cầu một loạt Header từ Peer (Hỗ trợ tải hàng nghìn Header bằng cách chia nhỏ batch)
func (n *NetworkManager) DownloadHeaderBatch(ctx context.Context, p peer.ID, start uint64, count uint32) ([][]byte, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host is nil (mock environment)")
	}
	var allHeaders [][]byte
	remaining := count
	currentStart := start


	for remaining > 0 {
		batchSize := uint32(10000)
		if remaining < batchSize {
			batchSize = remaining
		}

		log.Printf("[VANGUARD-DL] 📥 Đang tải %d Header từ #%d (Còn lại: %d)...", batchSize, currentStart, remaining-batchSize)
		
		s, err := n.Host.NewStream(ctx, p, HeaderBatchSyncProtocol)
		if err != nil { return nil, err }
		
		req := &pb_block.GetHeaderBatchRequest{StartHeight: currentStart, Count: batchSize}
		reqData, _ := proto.Marshal(req)
		s.Write(reqData)
		s.CloseWrite()

		// [MAINNET-TIMEOUT] Tăng read deadline lên 30 giây khi tải hàng loạt header qua P2P để chống nghẽn đường truyền
		s.SetReadDeadline(time.Now().Add(30 * time.Second))
		data, err := io.ReadAll(io.LimitReader(s, 16*1024*1024)) // Tăng buffer lên 16MB cho batch 10,000 headers
		s.Close()
		if err != nil { return nil, err }
		
		var resp pb_block.GetHeaderBatchResponse
		if err := proto.Unmarshal(data, &resp); err != nil { return nil, err }
		
		if len(resp.Headers) == 0 {
			break
		}

		allHeaders = append(allHeaders, resp.Headers...)
		currentStart += uint64(len(resp.Headers))
		remaining -= uint32(len(resp.Headers))

		// [VANGUARD-DYNAMISM] Cập nhật tiến độ tải header thời gian thực lên SyncEngine để UI hiển thị % động mượt mà
		if n.SyncEngine != nil {
			if se, ok := n.SyncEngine.(*SyncEngine); ok {
				se.mu.Lock()
				se.currentHeight = currentStart
				se.mu.Unlock()
			}
		}

		// [SYNC-HEAL] Nghỉ một khoảng thời gian ngắn (10ms) giữa các lần tải batch để đảm bảo không kích hoạt cơ chế Rate Limit bảo vệ ở server
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Chỉ báo lỗi nếu không lấy được MỘT khối nào cả
	if len(allHeaders) == 0 {
		return nil, fmt.Errorf("không tải được header nào từ Peer")
	}
	
	// CÓ BAO NHIÊU TRẢ VỀ BẤY NHIÊU
	log.Printf("[SYNC-HEADERS] 📦 Đã lấy thành công %d Headers từ mạng lưới.", len(allHeaders))
	return allHeaders, nil
}

// GetHeaderHashByHeight (Giữ nguyên logic cũ cho Single Request)
func (n *NetworkManager) GetHeaderHashByHeight(ctx context.Context, p peer.ID, height uint64) ([]byte, error) {
	// [MAINNET-TIMEOUT] Tăng timeout truy vấn header theo cao độ lên 15 giây để giảm tỷ lệ timeout mạng P2P
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	s, err := n.Host.NewStream(ctx, p, HeaderSyncProtocol)
	if err != nil { return nil, err }
	defer s.Close()

	req := &pb_block.GetBlockRequest{Height: height}
	reqData, _ := proto.Marshal(req)
	if _, err := s.Write(reqData); err != nil { return nil, err }
	s.CloseWrite()

	// [MAINNET-TIMEOUT] Tăng read deadline lên 15 giây tương thích với timeout context mới
	s.SetReadDeadline(time.Now().Add(15 * time.Second))
	data, err := io.ReadAll(io.LimitReader(s, 10*1024)) // Header nhỏ
	if err != nil { return nil, err }
	
	var resp pb_block.GetBlockResponse
	if err := proto.Unmarshal(data, &resp); err != nil { return nil, err }
	
	if !resp.Found || resp.Block == nil || resp.Block.Header == nil {
		return nil, io.EOF
	}
	
	// Trả về Header nguyên bản (Protobuf) để Rust có thể giải mã
	hBytes, _ := proto.Marshal(resp.Block.Header)
	return hBytes, nil
}

// RequestHeaderByHash gửi request lấy duy nhất Header theo mã băm từ Peer qua P2P
func (n *NetworkManager) RequestHeaderByHash(ctx context.Context, p peer.ID, hash []byte) ([]byte, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host is nil (mock environment)")
	}
	// [MAINNET-TIMEOUT] Tăng timeout truy vấn header theo hash lên 15 giây
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	s, err := n.Host.NewStream(ctx, p, HeaderByHashProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	req := &pb_block.GetBlockRequest{Hash: hash}
	reqData, _ := proto.Marshal(req)
	if _, err := s.Write(reqData); err != nil {
		return nil, err
	}
	s.CloseWrite()

	// [MAINNET-TIMEOUT] Tăng read deadline lên 15 giây tương thích với timeout context mới
	s.SetReadDeadline(time.Now().Add(15 * time.Second))
	data, err := io.ReadAll(io.LimitReader(s, 10*1024)) // Header nhỏ
	if err != nil {
		return nil, err
	}

	var resp pb_block.GetBlockResponse
	if err := proto.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	if !resp.Found || resp.Block == nil || resp.Block.Header == nil {
		return nil, io.EOF
	}

	hBytes, _ := proto.Marshal(resp.Block.Header)
	return hBytes, nil
}
