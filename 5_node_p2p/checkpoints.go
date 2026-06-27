package node_p2p

import (
	"encoding/hex"
	"log"
)

// Checkpoint định nghĩa một cột mốc xác thực tĩnh trên Go.
type Checkpoint struct {
	Height    uint64
	Hash      string
	StateRoot string
}

// Cấu hình checkpoints tĩnh trên Go để tránh gọi gRPC xuống Rust.
var checkpoints = map[uint64]Checkpoint{}


// IsValidCheckpoint kiểm tra một khối có khớp với mỏ neo lịch sử hay không.
func IsValidCheckpoint(height uint64, hash []byte) bool {
	if cp, ok := checkpoints[height]; ok {
		hashHex := hex.EncodeToString(hash)
		if cp.Hash != hashHex {
			log.Printf("[CHECKPOINT-AUTH] ❌ Khối #%d có Hash %s không khớp với mỏ neo cấu hình tĩnh!", height, hashHex)
			return false
		}
	}
	return true
}

// IsValidStateRoot kiểm tra rễ trạng thái có khớp với mỏ neo lịch sử hay không.
func IsValidStateRoot(height uint64, root []byte) bool {
	if cp, ok := checkpoints[height]; ok {
		rootHex := hex.EncodeToString(root)
		if cp.StateRoot != rootHex {
			log.Printf("[CHECKPOINT-AUTH] ❌ Khối #%d có StateRoot %s không khớp với mỏ neo cấu hình tĩnh!", height, rootHex)
			return false
		}
	}
	return true
}
