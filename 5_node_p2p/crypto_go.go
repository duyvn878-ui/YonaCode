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

	// 1. Sao chép sâu đối tượng Transaction để tránh làm thay đổi/bẩn dữ liệu gốc đang được sử dụng ở luồng khác
	var buf bytes.Buffer

	// 1. Version (uint64)
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
	if len(tx.RecentBlockHash) > 0 {
		binary.LittleEndian.PutUint32(tmp4[:], uint32(len(tx.RecentBlockHash)))
		buf.Write(tmp4[:])
		buf.Write(tx.RecentBlockHash)
	} else {
		binary.LittleEndian.PutUint32(tmp4[:], 0)
		buf.Write(tmp4[:])
	}

	// 9. Chain ID (uint64)
	binary.LittleEndian.PutUint64(tmp8[:], tx.ChainId)
	buf.Write(tmp8[:])

	// 10. Băm Blake3 phái sinh với Context ứng dụng
	hash := make([]byte, 32)
	blake3.DeriveKey(hash, RustCryptoContext, buf.Bytes())

	return hash
}

// VerifySignatureNative thực hiện xác thực chữ ký Ed25519 cho giao dịch cục bộ tại tầng Go.
// Tại sao: Làm chốt chặn phòng thủ vòng ngoài (Pre-check Shield) để lọc sạch các khối rác hoặc khối tấn công
// Signature Bomb DoS trước khi chuyển dữ liệu xuống cho Rust Core xử lý.
func VerifySignatureNative(tx *pb_block.Transaction) bool {
	if tx == nil || tx.Sender == nil || tx.Signature == nil {
		return false
	}

	// [SECURITY-VANGUARD] Chặn đứng các giao dịch từ các chuỗi khác hoặc chuỗi rác (Replay Attack Prevention)
	if tx.ChainId != 25062025 {
		return false
	}

	senderPubkey := tx.Sender.Value
	if len(senderPubkey) != ed25519.PublicKeySize {
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

// GetTxIDNative tính toán mã băm giao dịch (Stable TxID) từ dữ liệu bytes protobuf thô mà không cần gọi FFI/gRPC.
// Tại sao: Tăng hiệu năng tính toán TxID ở ngoài luồng gRPC xuống dưới 1ms.
func GetTxIDNative(txBytes []byte) []byte {
	hash := make([]byte, 32)
	blake3.DeriveKey(hash, RustCryptoContext, txBytes)
	return hash
}

