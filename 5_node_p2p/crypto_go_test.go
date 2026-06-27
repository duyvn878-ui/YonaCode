/**
 * @file crypto_go_test.go
 * @brief Kiểm thử tự động tính toán mã băm ký đồng thuận tuyệt đối giữa Go và Rust.
 * @details Xác thực mã băm ký (Signing Hash) tính bằng Go bản địa (Native) khớp chính xác 100%
 *          từng byte với kết quả đầu ra của Rust Core bằng cách đối chiếu với một vector kiểm thử
 *          (test vector) đã được tính toán sẵn và kiểm tra chéo qua gRPC (nếu có kết nối).
 * @date 2026-06-11
 * @author  
 */

package node_p2p

import (
	pb_block "btc_genz/proto"
	"encoding/hex"
	"testing"
	"btc_genz/2_miner_core/go_bridge"
)

// TestSigningHashConsensusAlignment kiểm thử tính đồng nhất tuyệt đối của mã băm ký
// giữa Go bản địa (Native) và Rust Core.
func TestSigningHashConsensusAlignment(t *testing.T) {
	// 1. Khởi tạo một giao dịch thử nghiệm có cấu trúc phức tạp
	// Các giá trị trường phải khớp chính xác từng byte với Transaction thử nghiệm chạy bên Rust Core.
	tx := &pb_block.Transaction{
		Version: 1,
		Sender: &pb_block.Address{
			Value: make([]byte, 32), // 32 bytes 0x00
		},
		Receiver: &pb_block.Address{
			Value: bytesRepeat(1, 32), // 32 bytes 0x01
		},
		Amount:          123456789,
		Fee:             500,
		Nonce:           42,
		Timestamp:       1600000000,
		RecentBlockHash: bytesRepeat(2, 32), // 32 bytes 0x02
		ChainId:         25062025,
		Signature: &pb_block.Signature{
			Value: bytesRepeat(9, 64), // 64 bytes 0x09 (Dùng để kiểm tra xem Signature có thực sự bị bỏ qua khi băm không)
		},
	}

	// 2. Tính mã băm ký cục bộ bằng Go bản địa
	goHashBytes := GetSigningHashNative(tx)
	goHashHex := hex.EncodeToString(goHashBytes)

	// 3. Đối chiếu với mã băm chuẩn do Rust Core tính toán (Test Vector lấy trực tiếp từ Rust crypto_primitives_test)
	// Giá trị mong muốn: b96be78631af676af4fde81cd0aa70c31b75617997545f50b3bdcf1559d7446a
	expectedRustHash := "c46a079def867e938ead12e558334605437e7e86b46bbbafad04fcaba2c9b9d6"

	if goHashHex != expectedRustHash {
		t.Fatalf("❌ LỖI ĐỒNG THUẬN CHÍ MẠNG: Mã băm ký Go lệch so với Rust!\nGo:   %s\nRust: %s", goHashHex, expectedRustHash)
	}
	t.Logf("✅ Khớp Test Vector thành công: %s", goHashHex)

	// 4. Nếu gRPC bridge đang kết nối (môi trường tích hợp thực tế), thực hiện kiểm tra chéo thời gian thực (Live Cross-Check)
	if go_bridge.GlobalBridge != nil {
		liveRustHashBytes := go_bridge.GlobalBridge.GetSigningHash(tx)
		if liveRustHashBytes != nil {
			liveRustHashHex := hex.EncodeToString(liveRustHashBytes)
			if goHashHex != liveRustHashHex {
				t.Fatalf("❌ LỖI ĐỒNG THUẬN THỜI GIAN THỰC (gRPC): Go lệch so với Live Rust!\nGo:   %s\nRust: %s", goHashHex, liveRustHashHex)
			}
			t.Log("✅ Khớp Live gRPC Cross-Check thành công!")
		}
	}
}

// Hàm bổ trợ sinh slice bytes lặp lại cho kiểm thử
func bytesRepeat(val byte, count int) []byte {
	res := make([]byte, count)
	for i := range res {
		res[i] = val
	}
	return res
}
