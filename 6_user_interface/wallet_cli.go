package user_interface

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	pb_tx "btc_genz/proto"
	pb_common "btc_genz/proto"
	go_bridge "btc_genz/2_miner_core/go_bridge"
	node_p2p "btc_genz/5_node_p2p"
)

type WalletCLI struct {
	netMgr *node_p2p.NetworkManager
	bridge *go_bridge.Bridge
}

func NewWalletCLI(netMgr *node_p2p.NetworkManager, bridge *go_bridge.Bridge) *WalletCLI {
	return &WalletCLI{netMgr: netMgr, bridge: bridge}
}

// EnsureInit đảm bảo Bridge được khởi tạo nếu chưa có (Dùng cho lệnh standalone)
func (w *WalletCLI) EnsureInit() {
	if w.bridge == nil {
		w.bridge = go_bridge.NewBridge(50051)
	}
}

func (w *WalletCLI) CreateWallet() {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fmt.Printf("[WALLET] ✅ Tạo ví mới thành công!\n")
	fmt.Printf("[WALLET] 🔑 Địa chỉ (Pub): %s\n", hex.EncodeToString(pub))
	fmt.Printf("[WALLET] 🛡️ Khóa bí mật (Seed): %s\n", hex.EncodeToString(priv.Seed()))
	fmt.Printf("[WALLET] ⚠️ CẢNH BÁO: Hãy lưu trữ khóa bí mật cẩn thận. Không chia sẻ cho bất kỳ ai.\n")
}

func (w *WalletCLI) GetBalance(addrStr string) {
	w.EnsureInit()
	addr, _ := hex.DecodeString(addrStr)
	bal := w.bridge.GetBalance(nil, addr, 0)
	nonce := w.bridge.GetNonce(nil, addr)
	fmt.Printf("[BALANCE] 🏦 Địa chỉ: %s\n", addrStr)
	fmt.Printf("[BALANCE] 💰 Số dư: %d Satoshi (BTC_Z)\n", bal)
	fmt.Printf("[BALANCE] 🔢 Nonce: %d\n", nonce)
}

func (w *WalletCLI) GetTransactionStatus(hashStr string) {
	w.EnsureInit()
	hash, _ := hex.DecodeString(hashStr)
	h, status, finalized, confs, _, _, _, _ := w.bridge.GetTransactionStatus(hash)
	
	statusText := "Chưa xác định"
	if finalized && status == 1 {
		statusText = "Đã Finalized (Blockchain SAFE)"
	} else if confs > 0 {
		statusText = fmt.Sprintf("Đã xác nhận (%d confirmations)", confs)
	} else if status == 1 {
		statusText = "Trong Mempool"
	}
	
	fmt.Printf("[TX] 🔍 Hash: %s\n", hashStr)
	fmt.Printf("[TX] 📏 Chiều cao: #%d\n", h)
	fmt.Printf("[TX] 🛡️ Trạng thái: %s\n", statusText)
}

func (w *WalletCLI) SendTransaction(privKeyHex string, receiverHex string, amount uint64, fee uint64) error {
	w.EnsureInit()
	
	seed, _ := hex.DecodeString(privKeyHex)
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	senderAddr := make([]byte, 32); copy(senderAddr, pubKey)
	
	receiverAddr, _ := hex.DecodeString(receiverHex)
	nonce := w.bridge.GetNonce(nil, senderAddr) + 1

	tx := &pb_tx.Transaction{
		Version:   1,
		Sender:    &pb_common.Address{Value: senderAddr},
		Receiver:  &pb_common.Address{Value: receiverAddr},
		Amount:    amount,
		Fee:       fee,
		Nonce:     nonce,
		Timestamp: uint64(time.Now().Unix()),
	}

	signingHash := w.bridge.GetSigningHash(tx)
	signature := ed25519.Sign(privKey, signingHash)
	tx.Signature = &pb_common.Signature{Value: signature}

	txData, err := proto.Marshal(tx)
	if err != nil { return err }

	if w.netMgr != nil {
		fmt.Printf("[P2P] 📡 Đang phát sóng giao dịch đến mạng lưới...\n")
		return w.netMgr.BroadcastTransaction(txData)
	}
	
	fmt.Printf("[INFO] 💾 Lệnh Standalone: Chỉ hiển thị dữ liệu đã ký.\n")
	fmt.Printf("[INFO] Hex: %s\n", hex.EncodeToString(txData))
	return nil
}

func (w *WalletCLI) ShowStatus(addr []byte) {
	bal := w.bridge.GetBalance(nil, addr, 0)
	nonce := w.bridge.GetNonce(nil, addr)
	fmt.Printf("Địa chỉ: %s | Số dư: %d BTC_Z | Nonce: %d\n", hex.EncodeToString(addr), bal, nonce)
}
