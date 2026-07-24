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
var checkpoints = map[uint64]Checkpoint{
	10000: {
		Height:    10000,
		Hash:      "cc13de01301374cf2ad7d587f9ce3409f0ed30a0d717e99227be536c815a0db8",
		StateRoot: "64ab05ff170530f614d9d9ba15c5dd6378bd52ab7ba8086ebbdfadb1bba455f2",
	},
	31264: {
		Height:    31264,
		// Hard Checkpoint tại khối chuẩn #31264 để vô hiệu hóa mọi chuỗi rẽ nhánh từ #31265 trở về sau
	},
	31570: {
		Height:    31570,
		// Hard Checkpoint mốc Tường lửa #31570
	},
}


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
