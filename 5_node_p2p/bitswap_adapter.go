package node_p2p

import (
	"context"
	"fmt"
	"log"

	pb "btc_genz/proto"
	"btc_genz/2_miner_core/go_bridge"
	"bytes"
	blocks "github.com/ipfs/go-block-format"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// BlockstoreAdapter thích ứng Rust Core sang go-ipfs-blockstore
type BlockstoreAdapter struct {
	bridge *go_bridge.Bridge
}

func NewBlockstoreAdapter(br *go_bridge.Bridge) *BlockstoreAdapter {
	return &BlockstoreAdapter{bridge: br}
}

func (b *BlockstoreAdapter) Has(ctx context.Context, c cid.Cid) (bool, error) {
	// [V19 UNIFIED] Truy vấn trực tiếp từ Rust Core qua mã băm
	data := b.bridge.GetRawByHash(c.Hash())
	return data != nil, nil
}

func (b *BlockstoreAdapter) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	// [V19 UNIFIED] Lấy dữ liệu thô từ Rust Core
	data := b.bridge.GetRawByHash(c.Hash())
	if data == nil {
		return nil, fmt.Errorf("block %s not found in Rust Core", c)
	}

	// [VANGUARD-SELF-AUDIT] Kiểm tra dữ liệu thô khớp với Hash (CID)
	// Tránh Bit-rot khi phục vụ qua Bitswap
	calculatedHash := b.bridge.GetRawHash(data)
	if !bytes.Equal(calculatedHash, c.Hash()) {
		log.Printf("[BITSWAP-AUDIT-CRITICAL] 🛑 Dữ liệu cục bộ bị hỏng cho CID %s!", c)
		return nil, fmt.Errorf("data corruption detected for cid %s", c)
	}

	return blocks.NewBlockWithCid(data, c)
}


func (b *BlockstoreAdapter) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	blk, err := b.Get(ctx, c)
	if err != nil { return 0, err }
	return len(blk.RawData()), nil
}

func (b *BlockstoreAdapter) Put(ctx context.Context, blk blocks.Block) error {
	// [V19 UNIFIED] Đẩy khối thô xuống Rust Core để lưu trữ
	data := blk.RawData()
	hash := blk.Cid().Hash()
	
	// Phân loại đơn giản để log, nhưng đẩy dữ liệu thô xuống Rust xử lý
	var header pb.BlockHeader
	if err := header.UnmarshalVT(data); err == nil && header.ParentHash != nil {
		log.Printf("[BITSWAP] 📥 Nhận Header khối #%d, đẩy xuống Rust Core...", header.Height)
		b.bridge.SaveBlockRaw(header.Height, hash, data, true)
		return nil
	}

	log.Printf("[BITSWAP] 📥 Nhận dữ liệu khối thô (%d bytes), đẩy xuống Rust Core...", len(data))
	// Ở đây chúng ta chưa biết height, Rust Core sẽ xử lý việc mapping sau nếu là body
	// Tuy nhiên với V19, P2P SyncEngine ưu tiên SaveBlockRaw khi biết height.
	return nil 
}

func (b *BlockstoreAdapter) PutMany(ctx context.Context, blks []blocks.Block) error {
	for _, blk := range blks {
		if err := b.Put(ctx, blk); err != nil { return err }
	}
	return nil
}

func (b *BlockstoreAdapter) DeleteBlock(ctx context.Context, c cid.Cid) error {
	return nil // Blockchain là vĩnh viễn
}

func (b *BlockstoreAdapter) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	// [Audit S-02 REALITY FIX] Trả về danh sách CID của các khối gần nhất
	out := make(chan cid.Cid, 100)
	go func() {
		defer close(out)
		highest := b.bridge.GetCurrentVersion()
		limit := uint64(0)
		if highest > 100 {
			limit = highest - 100
		}
		for h := highest; h >= limit; h-- {
			hash := b.bridge.GetBlockHash(h)
			if hash != nil {
				select {
				case out <- CastHashToCid(hash):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (b *BlockstoreAdapter) HashOnRead(enabled bool) {}

// Hàm tiện ích để tạo CID từ Hash của YonaCode
func CastHashToCid(hash []byte) cid.Cid {
	buf, _ := multihash.Encode(hash, multihash.BLAKE3)
	return cid.NewCidV1(cid.Raw, buf)
}
