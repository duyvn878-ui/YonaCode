package main

/**
 * @file main.go
 * @brief Máy chủ Cổng kết nối Ví Độc lập (Yona Wallet Gateway)
 * @details Tách biệt giao diện ví trên di động/web (frontend) khỏi các cổng P2P/RPC của nút xác thực chính (core validator node).
 *          Đóng vai trò như một cổng trung gian kết nối tới Nút Yona (Yona Node) qua gRPC và phục vụ ứng dụng React Client.
 *          Lập chỉ mục (index) các khối trong nền (background) để cung cấp lịch sử giao dịch nhanh chóng cho các ví.
 * @date 2026-07-17
 */

import (
	pb_block "btc_genz/proto"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"
)

const RustCryptoContext = "BTC GenZ Toi Gian PoW v1.0"

// GatewayTx đại diện cho một giao dịch chuẩn hóa phục vụ giao diện người dùng (UI) của ví
type GatewayTx struct {
	TxID          string `json:"txid"`
	Sender        string `json:"sender"`
	Receiver      string `json:"receiver"`
	Amount        uint64 `json:"amount"`
	Fee           uint64 `json:"fee"`
	Nonce         uint64 `json:"nonce"`
	Timestamp     uint64 `json:"timestamp"`
	Status        int    `json:"status"` // 0: Thành công, 99: Đang chờ, 1: Lỗi
	BlockHeight   uint64 `json:"blockHeight,omitempty"`
	Confirmations uint64 `json:"confirmations,omitempty"`
}

// MemoryDB lưu trữ cơ sở dữ liệu lịch sử dưới dạng JSON trong bộ nhớ
type MemoryDB struct {
	mu           sync.RWMutex
	LastHeight   uint64                `json:"last_height"`
	Transactions map[string]*GatewayTx `json:"transactions"` // khóa: txid
	AddressIndex map[string][]string   `json:"-"`            // khóa: địa chỉ (đã làm sạch), giá trị: danh sách txids
	FilePath     string                `json:"-"`
}

var (
	nodeAddr   string
	listenPort string
	nodeToken  string
	dbPath     string

	grpcClient pb_block.BlockchainServiceClient
	db         *MemoryDB

	sseClients []chan string
	sseMu      sync.Mutex
)

func main() {
	flag.StringVar(&nodeAddr, "node-addr", "localhost:18080", "Địa chỉ của máy chủ gRPC Yona Node")
	flag.StringVar(&listenPort, "port", "9090", "Cổng HTTP cho Máy chủ Wallet Gateway")
	flag.StringVar(&nodeToken, "token", "", "Mã xác thực tùy chọn cho gRPC Node")
	flag.StringVar(&dbPath, "db", "./data/gateway_index.json", "Đường dẫn đến cơ sở dữ liệu lịch sử giao dịch")
	flag.Parse()

	log.Printf("🚀 Đang khởi động Yona Wallet Gateway trên cổng %s", listenPort)
	log.Printf("🔗 Nút Yona gRPC đích: %s", nodeAddr)

	// Đảm bảo thư mục lưu trữ cơ sở dữ liệu (DB) tồn tại
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Fatalf("❌ Không thể tạo thư mục DB: %v", err)
	}

	// Khởi tạo DB cục bộ
	db = &MemoryDB{
		Transactions: make(map[string]*GatewayTx),
		FilePath:     dbPath,
	}
	db.load()

	// Kết nối tới Nút gRPC
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if nodeToken != "" {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-auth-token", nodeToken)
			return invoker(ctx, method, req, reply, cc, opts...)
		}))
	}

	conn, err := grpc.Dial(nodeAddr, dialOpts...)
	if err != nil {
		log.Fatalf("❌ Kết nối tới Nút gRPC thất bại: %v", err)
	}
	defer conn.Close()

	grpcClient = pb_block.NewBlockchainServiceClient(conn)

	// Khởi chạy tiến trình lập chỉ mục khối chạy ngầm
	go runBlockIndexer()

	// Khởi chạy máy chủ HTTP
	r := mux.NewRouter()

	// Bộ lọc trung gian CORS toàn cục (Global CORS Wrapper)
	// Tại sao thiết kế như vậy: Sử dụng bộ xử lý toàn cục bọc ngoài router (r) thay vì r.Use().
	// Nếu dùng r.Use(), Gorilla Mux sẽ chặn các yêu cầu OPTIONS gửi tới các API chỉ định phương thức POST (như /api/v1/send_raw_tx)
	// và trả về lỗi 405 Method Not Allowed trước khi chạy qua middleware CORS, gây ra lỗi "Failed to fetch" trên trình duyệt do preflight bị chặn.
	globalCORS := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Wallet-Token")
		if req.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		r.ServeHTTP(w, req)
	})

	// Đăng ký các cổng API REST
	r.HandleFunc("/api/v1/status", handleStatus).Methods("GET")
	r.HandleFunc("/api/v1/balance/{address}", handleGetBalance).Methods("GET")
	r.HandleFunc("/api/v1/address/{address}/history", handleAddressHistory).Methods("GET")
	r.HandleFunc("/api/v1/tx/{txid}", handleGetTxDetail).Methods("GET")
	r.HandleFunc("/api/v1/prepare_tx", handlePrepareTx).Methods("POST")
	r.HandleFunc("/api/v1/send_raw_tx", handleSendRawTx).Methods("POST")
	r.HandleFunc("/api/v1/network/watch-status", handleWatchStatus).Methods("GET")

	srv := &http.Server{
		Handler:      globalCORS,
		Addr:         ":" + listenPort,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Printf("🟢 Wallet Gateway đang chạy thành công tại http://localhost:%s", listenPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("❌ Lỗi máy chủ HTTP: %v", err)
	}
}

// ==========================================
// BỘ LẬP CHỈ MỤC KHỐI CHẠY NGẦM & THEO DÕI SỰ KIỆN
// ==========================================

func runBlockIndexer() {
	log.Println("🔍 Tiến trình lập chỉ mục khối chạy ngầm đã khởi động")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status, err := grpcClient.GetStatus(ctx, &pb_block.GetStatusRequest{})
		cancel()
		if err != nil {
			log.Printf("⚠️ Lấy trạng thái Nút qua gRPC thất bại: %v", err)
			continue
		}

		db.mu.Lock()
		currentHeight := status.CurrentHeight
		lastIndexed := db.LastHeight
		db.mu.Unlock()

		if currentHeight < lastIndexed {
			log.Printf("⚠️ Phát hiện tái cấu trúc chuỗi (Reorg)! Chiều cao Nút: %d | Chỉ mục cục bộ: %d. Đang hoàn tác...", currentHeight, lastIndexed)
			db.mu.Lock()
			for txid, tx := range db.Transactions {
				if tx.BlockHeight > currentHeight {
					delete(db.Transactions, txid)
				}
			}
			db.LastHeight = currentHeight
			db.rebuildAddressIndex()
			db.mu.Unlock()
			db.save()
		} else if currentHeight > lastIndexed {
			log.Printf("📈 Chiều cao Nút: %d | Chỉ mục cục bộ: %d. Đang đồng bộ các khối mới...", currentHeight, lastIndexed)
			for h := lastIndexed + 1; h <= currentHeight; h++ {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				blockRes, err := grpcClient.GetBlock(ctx, &pb_block.GetBlockRequest{Height: h})
				cancel()
				if err != nil {
					log.Printf("❌ Lấy khối #%d thất bại: %v", h, err)
					break
				}

				if blockRes.Found && blockRes.Block != nil {
					db.indexBlock(blockRes.Block, h, currentHeight)
				}
			}
			broadcastSSE("update")
		} else {
			// Cập nhật số khối xác nhận (confirmations) cho các giao dịch hiện tại chưa hoàn tất
			db.mu.Lock()
			for _, tx := range db.Transactions {
				if tx.Status == 0 && tx.BlockHeight > 0 {
					tx.Confirmations = currentHeight - tx.BlockHeight + 1
				}
			}
			db.mu.Unlock()
		}

		// Quét và dọn dẹp các giao dịch đang chờ (Pending) bị quá hạn hoặc nonce thấp
		db.mu.Lock()
		var pendingTxs []*GatewayTx
		for _, tx := range db.Transactions {
			if tx.Status == 99 {
				pendingTxs = append(pendingTxs, tx)
			}
		}
		db.mu.Unlock()

		if len(pendingTxs) > 0 {
			dbUpdated := false
			for _, tx := range pendingTxs {
				senderBytes, err := parseAddressBytes(tx.Sender)
				if err != nil || len(senderBytes) != 32 {
					continue
				}
				ctxAcc, cancelAcc := context.WithTimeout(context.Background(), 2*time.Second)
				accState, err := grpcClient.GetAccount(ctxAcc, &pb_block.GetAccountRequest{Address: senderBytes})
				cancelAcc()

				if err == nil {
					// 1. Kiểm tra Nonce của ledger đã vượt quá (tx.Nonce + 1) chưa.
					// Lưu ý: Khi giao dịch Nonce N được đào vào khối thành công, ledger Nonce sẽ tăng từ N lên N+1.
					// Do đó chỉ hủy giao dịch nếu ledger Nonce > N + 1 (nghĩa là đã bị bỏ qua hoàn toàn).
					if accState.Nonce > tx.Nonce+1 {
						db.mu.Lock()
						tx.Status = 1 // Thất bại
						log.Printf("[GATEWAY-CLEANUP] ❌ Giao dịch %s bị hủy: Nonce %d đã bị bỏ qua (Ledger Nonce hiện tại: %d)", tx.TxID[:10], tx.Nonce, accState.Nonce)
						db.mu.Unlock()
						dbUpdated = true
					} else {
						// 2. Kiểm tra nếu giao dịch đã chờ quá 600 giây (10 phút) mà chưa được đào vào khối
						// thì mới đánh dấu hết hạn để giải phóng nonce cho người dùng gửi lại.
						nowUnix := uint64(time.Now().Unix())
						if nowUnix > tx.Timestamp && nowUnix-tx.Timestamp > 600 {
							db.mu.Lock()
							tx.Status = 1 // Thất bại
							log.Printf("[GATEWAY-CLEANUP] ⏳ Giao dịch %s bị hủy: Hết hạn chờ trong mempool (> 10 phút)", tx.TxID[:10])
							db.mu.Unlock()
							dbUpdated = true
						}
					}
				}
			}
			if dbUpdated {
				db.save()
				broadcastSSE("update")
			}
		}
	}
}

// ==========================================
// CÁC BỘ XỬ LÝ REST CHO FRONTEND REACT
// ==========================================

func handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := grpcClient.GetStatus(ctx, &pb_block.GetStatusRequest{})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%v"}`, err), http.StatusInternalServerError)
		return
	}

	tipHashHex := "0000000000000000000000000000000000000000000000000000000000000000"
	blockHeight := status.CurrentHeight
	if blockHeight > 0 {
		blockHeight--
	}
	blockResp, err2 := grpcClient.GetBlock(ctx, &pb_block.GetBlockRequest{Height: blockHeight})
	if err2 == nil && blockResp.Found {
		headerBytes, errMarshal := proto.Marshal(blockResp.Block.Header)
		if errMarshal == nil {
			hashResp, errHash := grpcClient.CalculateBlockHeaderHash(ctx, &pb_block.RawBytes{Data: headerBytes})
			if errHash == nil {
				tipHashHex = hex.EncodeToString(hashResp.Value)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current_height":   status.CurrentHeight,
		"finalized_height": status.FinalizedHeight,
		"peer_count":       status.PeerCount,
		"difficulty":       status.Difficulty,
		"version":          status.Version,
		"is_mining":        status.IsMining,
		"hashrate":         status.Hashrate,
		"sync_progress":    100, // Gateway mặc định là đã đồng bộ hóa hoàn toàn khi có kết quả trả về
		"tip_hash":         tipHashHex,
	})
}

func parseAddressBytes(addrStr string) ([]byte, error) {
	clean := strings.ToLower(strings.TrimPrefix(addrStr, "0x"))
	if len(clean)%2 != 0 {
		clean = "0" + clean
	}
	addrBytes, err := hex.DecodeString(clean)
	if err != nil {
		return nil, err
	}
	if len(addrBytes) == 33 {
		return addrBytes[1:], nil
	}
	if len(addrBytes) > 32 {
		return addrBytes[len(addrBytes)-32:], nil
	}
	if len(addrBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(addrBytes):], addrBytes)
		return padded, nil
	}
	return addrBytes, nil
}

func handleGetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	addrStr := vars["address"]
	addrBytes, err := parseAddressBytes(addrStr)
	if err != nil || len(addrBytes) == 0 {
		http.Error(w, `{"error": "Định dạng địa chỉ không hợp lệ"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acc, err := grpcClient.GetAccount(ctx, &pb_block.GetAccountRequest{Address: addrBytes})
	if err != nil {
		// Trở về mặc định là 0 nếu địa chỉ ví chưa được khởi tạo trên dữ liệu sổ cái
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"address": addrStr,
			"nonce":   0,
			"balances": map[string]uint64{
				"btc_z":     0,
				"spendable": 0,
				"pending":   0,
			},
		})
		return
	}

	// Tính toán expected_nonce dựa trên các giao dịch đang chờ (Pending - status 99)
	db.mu.RLock()
	expectedNonce := acc.Nonce
	cleanQuery := hex.EncodeToString(addrBytes)
	for _, tx := range db.Transactions {
		sBytes, err := parseAddressBytes(tx.Sender)
		if err == nil && hex.EncodeToString(sBytes) == cleanQuery && tx.Status == 99 {
			if tx.Nonce >= expectedNonce {
				expectedNonce = tx.Nonce + 1
			}
		}
	}
	db.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address":        addrStr,
		"nonce":          acc.Nonce,
		"expected_nonce": expectedNonce,
		"balances": map[string]uint64{
			"btc_z":     acc.Balance,
			"spendable": acc.Balance, // Phiên bản cổng độc lập hiện tại chỉ hiển thị số dư tài khoản đơn giản
			"pending":   0,
		},
	})
}

func handleAddressHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	addrBytes, err := parseAddressBytes(vars["address"])
	if err != nil {
		http.Error(w, `{"error": "Định dạng địa chỉ không hợp lệ"}`, http.StatusBadRequest)
		return
	}
	cleanQuery := hex.EncodeToString(addrBytes)

	db.mu.RLock()
	defer db.mu.RUnlock()

	var history []GatewayTx
	txids := db.AddressIndex[cleanQuery]

	// Lấy chi tiết thông tin giao dịch (bao gồm cả giao dịch thất bại để người dùng theo dõi lý do)
	limit := 100
	count := 0
	for i := len(txids) - 1; i >= 0 && count < limit; i-- {
		txid := strings.TrimPrefix(txids[i], "0x")
		if tx, exists := db.Transactions[txid]; exists {
			history = append(history, *tx)
			count++
		}
	}

	// Sắp xếp lịch sử giao dịch theo timestamp giảm dần (mới nhất lên đầu)
	sort.Slice(history, func(i, j int) bool {
		if history[i].Timestamp != history[j].Timestamp {
			return history[i].Timestamp > history[j].Timestamp
		}
		return history[i].Nonce > history[j].Nonce
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tx_count": len(history),
		"history":  history,
	})
}

func handleGetTxDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := strings.TrimPrefix(vars["txid"], "0x")

	db.mu.RLock()
	tx, exists := db.Transactions[txid]
	db.mu.RUnlock()

	if !exists {
		http.Error(w, `{"error": "Không tìm thấy giao dịch"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Phản hồi theo định dạng chuẩn hóa tương thích tuyệt đối với App.tsx
	json.NewEncoder(w).Encode(map[string]interface{}{
		"txid":          tx.TxID,
		"ID":            tx.TxID,
		"sender":        tx.Sender,
		"Sender":        tx.Sender,
		"receiver":      tx.Receiver,
		"Receiver":      tx.Receiver,
		"amount":        tx.Amount,
		"Amount":        tx.Amount,
		"fee":           tx.Fee,
		"Fee":           tx.Fee,
		"nonce":         tx.Nonce,
		"Nonce":         tx.Nonce,
		"timestamp":     tx.Timestamp,
		"Timestamp":     tx.Timestamp,
		"status":        tx.Status,
		"status_code":   mapGatewayStatus(tx.Status),
		"StatusCode":    mapGatewayStatus(tx.Status),
		"blockHeight":   tx.BlockHeight,
		"Height":        tx.BlockHeight,
		"confirmations": tx.Confirmations,
		"Confirmations": tx.Confirmations,
	})
}

func mapGatewayStatus(status int) int {
	if status == 0 {
		return 1 // Ánh xạ sang thành công (success)
	}
	if status == 99 {
		return 0 // Ánh xạ sang đang chờ xử lý trong bộ nhớ đệm (pending/mempool)
	}
	return 2 // Ánh xạ sang lỗi (error)
}

// FrontendTx đại diện cho giao dịch phẳng nhận được từ ví React Client
type FrontendTx struct {
	Version         uint64 `json:"version"`
	Sender          string `json:"sender"`
	Receiver        string `json:"receiver"`
	Amount          uint64 `json:"amount"`
	Fee             uint64 `json:"fee"`
	Nonce           uint64 `json:"nonce"`
	Timestamp       uint64 `json:"timestamp"`
	RecentBlockHash string `json:"recent_block_hash"`
	Signature       string `json:"signature"`
	ChainId         uint64 `json:"chain_id"`
}

func handleSendRawTx(w http.ResponseWriter, r *http.Request) {
	var ftarg FrontendTx
	if err := json.NewDecoder(r.Body).Decode(&ftarg); err != nil {
		http.Error(w, `{"error": "Tải trọng giao dịch không hợp lệ"}`, http.StatusBadRequest)
		return
	}

	senderBytes, err := parseAddressBytes(ftarg.Sender)
	if err != nil || len(senderBytes) != 32 {
		http.Error(w, `{"error": "Địa chỉ người gửi không hợp lệ (cần 32 bytes)"}`, http.StatusBadRequest)
		return
	}
	receiverBytes, err := parseAddressBytes(ftarg.Receiver)
	if err != nil || len(receiverBytes) != 32 {
		http.Error(w, `{"error": "Địa chỉ người nhận không hợp lệ (cần 32 bytes)"}`, http.StatusBadRequest)
		return
	}
	recentBlockHashBytes, err := hex.DecodeString(strings.TrimPrefix(ftarg.RecentBlockHash, "0x"))
	if err != nil {
		http.Error(w, `{"error": "Mã băm khối gần đây không hợp lệ"}`, http.StatusBadRequest)
		return
	}
	signatureBytes, err := hex.DecodeString(strings.TrimPrefix(ftarg.Signature, "0x"))
	if err != nil {
		http.Error(w, `{"error": "Chữ ký giao dịch không hợp lệ"}`, http.StatusBadRequest)
		return
	}

	tx := pb_block.Transaction{
		Version:         ftarg.Version,
		Sender:          &pb_block.Address{Value: senderBytes},
		Receiver:        &pb_block.Address{Value: receiverBytes},
		Amount:          ftarg.Amount,
		Fee:             ftarg.Fee,
		Nonce:           ftarg.Nonce,
		Timestamp:       ftarg.Timestamp,
		RecentBlockHash: recentBlockHashBytes,
		Signature:       &pb_block.Signature{Value: signatureBytes},
		ChainId:         ftarg.ChainId,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hashRes, err := grpcClient.SubmitTransaction(ctx, &tx)

	txTimestamp := tx.Timestamp
	if txTimestamp == 0 {
		txTimestamp = uint64(time.Now().Unix())
	}

	if err != nil {
		// Ngay cả khi thợ đào/node từ chối giao dịch, vẫn phải tính toán txid và lưu lịch sử thất bại vào DB
		txidBytes := calculateSigningHash(&tx)
		txidHex := hex.EncodeToString(txidBytes)
		txidWithPrefix := "0x" + txidHex

		db.mu.Lock()
		failedTx := &GatewayTx{
			TxID:          txidWithPrefix,
			Sender:        "0x" + hex.EncodeToString(tx.Sender.Value),
			Receiver:      "0x" + hex.EncodeToString(tx.Receiver.Value),
			Amount:        tx.Amount,
			Fee:           tx.Fee,
			Nonce:         tx.Nonce,
			Timestamp:     txTimestamp,
			Status:        1, // Thất bại / Bị từ chối
			Confirmations: 0,
		}
		db.Transactions[txidHex] = failedTx
		db.addTxToAddressIndex(failedTx)
		db.mu.Unlock()
		db.save()
		broadcastSSE("update")

		http.Error(w, fmt.Sprintf(`{"error": "%v", "txid": "%s"}`, err, txidWithPrefix), http.StatusInternalServerError)
		return
	}

	txidHex := hex.EncodeToString(hashRes.Value)
	txidWithPrefix := "0x" + txidHex

	// Lưu vào chỉ mục cơ sở dữ liệu cục bộ với trạng thái đang chờ xử lý
	db.mu.Lock()
	newTx := &GatewayTx{
		TxID:          txidWithPrefix,
		Sender:        "0x" + hex.EncodeToString(tx.Sender.Value),
		Receiver:      "0x" + hex.EncodeToString(tx.Receiver.Value),
		Amount:        tx.Amount,
		Fee:           tx.Fee,
		Nonce:         tx.Nonce,
		Timestamp:     txTimestamp,
		Status:        99, // Đang chờ trong Mempool
		Confirmations: 0,
	}
	db.Transactions[txidHex] = newTx
	db.addTxToAddressIndex(newTx)
	db.mu.Unlock()
	db.save()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"txid":    txidWithPrefix,
	})

	// Kích hoạt phát tín hiệu cập nhật thời gian thực cục bộ ngay lập tức

	broadcastSSE("update")
}

// Cấu trúc yêu cầu tạo bản nháp giao dịch
type PrepareTxRequest struct {
	Sender   string `json:"sender"`
	Receiver string `json:"receiver"`
	Amount   uint64 `json:"amount"` // Số tiền tính bằng VNT (1 GO = 100,000,000 VNT)
}

// Cấu trúc phản hồi bản nháp giao dịch đã đóng gói
type PrepareTxResponse struct {
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	Version         uint64 `json:"version"`
	Sender          string `json:"sender"`
	Receiver        string `json:"receiver"`
	Amount          uint64 `json:"amount"`
	Fee             uint64 `json:"fee"`
	CreationFee     uint64 `json:"creation_fee"`
	Nonce           uint64 `json:"nonce"`
	Timestamp       uint64 `json:"timestamp"`
	RecentBlockHash string `json:"recent_block_hash"`
	SigningHash     string `json:"signing_hash"`
	ChainId         uint64 `json:"chain_id"`
}

// calculateSigningHash: Tính toán mã băm Blake3 của giao dịch bằng DeriveKey
func calculateSigningHash(tx *pb_block.Transaction) []byte {
	if tx == nil {
		return nil
	}
	var buf bytes.Buffer
	var tmp8 [8]byte
	var tmp4 [4]byte

	binary.LittleEndian.PutUint64(tmp8[:], tx.Version)
	buf.Write(tmp8[:])

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

func getFullNodeHTTPAddr() string {
	host := nodeAddr
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		host = parts[0]
	}
	if host == "" {
		host = "localhost"
	}
	return "http://" + host + ":8080"
}

func handlePrepareTx(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req PrepareTxRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2048)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(PrepareTxResponse{Success: false, Error: "JSON request body không hợp lệ"})
		return
	}
	defer r.Body.Close()

	fullNodeHTTP := getFullNodeHTTPAddr()
	reqBytes, _ := json.Marshal(req)

	resp, err := http.Post(fullNodeHTTP+"/api/v1/prepare_unsigned_tx", "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(PrepareTxResponse{Success: false, Error: "Không thể kết nối tới Full Node để đóng gói giao dịch: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ==========================================
// CÁC PHƯƠNG THỨC HỖ TRỢ LƯU TRỮ VÀ MÃ HÓA
// ==========================================

func (db *MemoryDB) load() {
	db.mu.Lock()
	defer db.mu.Unlock()

	data, err := os.ReadFile(db.FilePath)
	if err != nil {
		db.AddressIndex = make(map[string][]string)
		return
	}

	json.Unmarshal(data, db)
	if db.Transactions == nil {
		db.Transactions = make(map[string]*GatewayTx)
	}
	db.rebuildAddressIndex()
}

func handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Không hỗ trợ phát luồng dữ liệu (streaming)", http.StatusInternalServerError)
		return
	}

	ch := make(chan string, 10)
	sseMu.Lock()
	sseClients = append(sseClients, ch)
	sseMu.Unlock()

	// Gửi sự kiện khởi tạo ban đầu
	fmt.Fprintf(w, "data: init\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			sseMu.Lock()
			for i, c := range sseClients {
				if c == ch {
					sseClients = append(sseClients[:i], sseClients[i+1:]...)
					break
				}
			}
			sseMu.Unlock()
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func broadcastSSE(msg string) {
	sseMu.Lock()
	defer sseMu.Unlock()
	for _, ch := range sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (db *MemoryDB) save() {
	db.mu.RLock()
	defer db.mu.RUnlock()

	data, _ := json.MarshalIndent(db, "", "  ")
	os.WriteFile(db.FilePath, data, 0600)
}

func (db *MemoryDB) indexBlock(block *pb_block.Block, height uint64, tip uint64) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if block.Body == nil || len(block.Body.Transactions) == 0 {
		db.LastHeight = height
		return
	}

	for _, tx := range block.Body.Transactions {
		txid := hex.EncodeToString(GetSigningHashNative(tx))
		senderAddr := "0x"
		if tx.Sender != nil && len(tx.Sender.Value) > 0 {
			senderAddr = "0x" + hex.EncodeToString(tx.Sender.Value)
		}
		receiverAddr := "0x"
		if tx.Receiver != nil && len(tx.Receiver.Value) > 0 {
			receiverAddr = "0x" + hex.EncodeToString(tx.Receiver.Value)
		}
		txTimestamp := tx.Timestamp
		if txTimestamp == 0 {
			if block.Header != nil && block.Header.Timestamp > 0 {
				txTimestamp = block.Header.Timestamp
			} else if height > 0 {
				// Mốc thời gian mạng Genesis + height * 3s
				txTimestamp = 1750800000 + (height * 3)
			} else {
				txTimestamp = uint64(time.Now().Unix())
			}
		}
		newTx := &GatewayTx{
			TxID:          "0x" + txid,
			Sender:        senderAddr,
			Receiver:      receiverAddr,
			Amount:        tx.Amount,
			Fee:           tx.Fee,
			Nonce:         tx.Nonce,
			Timestamp:     txTimestamp,
			Status:        0, // Thành công (Đã khai thác vào khối)
			BlockHeight:   height,
			Confirmations: tip - height + 1,
		}
		db.Transactions[txid] = newTx
		db.addTxToAddressIndex(newTx)
	}

	db.LastHeight = height
	go db.save()
}

// Tái xây dựng chỉ mục địa chỉ (address index) từ bản đồ các giao dịch
func (db *MemoryDB) rebuildAddressIndex() {
	db.AddressIndex = make(map[string][]string)
	for _, tx := range db.Transactions {
		db.addTxToAddressIndex(tx)
	}
}

// Thêm một giao dịch vào bản đồ chỉ mục địa chỉ tương ứng (Chuẩn hóa 32-byte hex)
func (db *MemoryDB) addTxToAddressIndex(tx *GatewayTx) {
	if db.AddressIndex == nil {
		db.AddressIndex = make(map[string][]string)
	}
	sBytes, errS := parseAddressBytes(tx.Sender)
	rBytes, errR := parseAddressBytes(tx.Receiver)

	appendUnique := func(addr, txid string) {
		if addr == "" {
			return
		}
		list := db.AddressIndex[addr]
		for _, id := range list {
			if id == txid {
				return
			}
		}
		db.AddressIndex[addr] = append(db.AddressIndex[addr], txid)
	}

	if errS == nil {
		appendUnique(hex.EncodeToString(sBytes), tx.TxID)
	}
	if errR == nil {
		appendUnique(hex.EncodeToString(rBytes), tx.TxID)
	}
}

// Tính toán mã băm chữ ký (signing hash) của giao dịch trực tiếp bằng ngôn ngữ Go
func GetSigningHashNative(tx *pb_block.Transaction) []byte {
	if tx == nil {
		return nil
	}

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
