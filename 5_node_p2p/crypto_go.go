/**
 * @file crypto_go.go
 * @brief Bộ công cụ mã hóa nội bộ cho tầng mạng Go P2P.
 * @details Tính toán Signing Hash và xác thực chữ ký cục bộ (Native Go) nhằm ngăn chặn bão gRPC Storm DoS.
 *          Sử dụng thư viện blake3 của Go với Context tương thích 100% với lõi Rust.
 * @date 2026-06-11
 * @author 
 */

package node_p2p

import (
	pb_block "btc_genz/proto"
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"lukechampine.com/blake3"
	"google.golang.org/protobuf/proto"
)

// RustCryptoContext PHẢI trùng khớp hoàn toàn với GENZ_POW_CONTEXT trong Rust Core.
// Tại sao: Blake3 DeriveKey sử dụng Context này làm khóa dẫn xuất. Chỉ cần lệch một ký tự,
// mã băm ký được tạo ra sẽ khác hoàn toàn và gây lỗi từ chối khối hàng loạt.
const RustCryptoContext = "BTC GenZ Toi Gian PoW v1.0"

// GetSigningHashNative tính toán mã băm ký của một giao dịch ngay tại tầng Go bằng cách sử dụng
// cơ chế phái sinh khóa Blake3 (DeriveKey) mà không cần gọi FFI/gRPC sang Rust Core.
// Tại sao: Tiết kiệm tối đa chi phí IPC gRPC (context switch, serialize) khi duyệt hàng loạt giao dịch.
func GetSigningHashNative(tx *pb_block.Transaction) []byte {
	if tx == nil {
		return nil
	}

	// 1. Version (uint64)
	var buf bytes.Buffer
	var tmp8 [8]byte
	binary.LittleEndian.PutUint64(tmp8[:], tx.Version)
	buf.Write(tmp8[:])

	// 2. Sender
	var tmp4 [4]byte
	if tx.Sender != nil && len(tx.Sender.Value) > 0 {
		binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.Sender.Value)))
		buf.Write(tmp4[:])
		buf.Write(tx.Sender.Value)
	} else {
		binary.LittleEndian.PutUint32(tmp4[:], 0)
		buf.Write(tmp4[:])
	}

	// 3. Receiver
	if tx.Receiver != nil && len(tx.Receiver.Value) > 0 {
		binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.Receiver.Value)))
		buf.Write(tmp4[:])
		buf.Write(tx.Receiver.Value)
	} else {
		binary.LittleEndian.PutUint32(tmp4[:], 0)
		buf.Write(tmp4[:])
	}

	// 4. Amount (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.Amount)
	buf.Write(tmp8[:])

	// 5. Fee (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.Fee)
	buf.Write(tmp8[:])

	// 6. Nonce (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.Nonce)
	buf.Write(tmp8[:])

	// 7. Timestamp (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.Timestamp)
	buf.Write(tmp8[:])

	// 8. Recent Block Hash
	binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.RecentBlockHash)))
	buf.Write(tmp4[:])
	buf.Write(tx.RecentBlockHash)

	// 9. Chain ID (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.ChainId)
	buf.Write(tmp8[:])

	// 10. Băm Blake3 Vanguard
	hash := make([]byte, 32)
	blake3.DeriveKey(hash, RustCryptoContext, buf.Bytes())
	return hash
}

// VerifySignatureNative xác thực chữ ký của giao dịch ở tầng Go dùng Ed25519 cục bộ.
func VerifySignatureNative(tx *pb_block.Transaction) bool {
	if tx == nil || tx.Sender == nil || tx.Signature == nil {
		return false
	}

	senderPubkey := tx.Sender.Value
	if len(senderPubkey) != ed25519.PublicKeySize {
		return false
	}

	// [SECURITY-VANGUARD] Chặn đứng các giao dịch từ các chuỗi khác hoặc chuỗi rác (Replay Attack Prevention)
	if tx.ChainId != 25062025 {
		return false
	}

	sig := tx.Signature.Value
	if len(sig) != ed25519.SignatureSize {
		return false
	}

	signingHash := GetSigningHashNative(tx)
	if signingHash == nil {
		return false
	}

	return ed25519.Verify(senderPubkey, signingHash, sig)
}

// GetTxIDNative tính toán mã băm giao dịch (Stable TxID) bằng cách unmarshal bytes thô và gọi GetSigningHashNative.
// Điều này đảm bảo TxID tính toán ở Go trùng khớp 100% với hàm calculate_tx_hash của Rust Core (SegWit-TxID).
func GetTxIDNative(txBytes []byte) []byte {
	var tx pb_block.Transaction
	if err := proto.Unmarshal(txBytes, &tx); err != nil {
		// Fallback nếu không unmarshal được
		hash := make([]byte, 32)
		blake3.DeriveKey(hash, RustCryptoContext, txBytes)
		return hash
	}
	return GetSigningHashNative(&tx)
}
