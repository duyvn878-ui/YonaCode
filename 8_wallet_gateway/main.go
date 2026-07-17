package main

/**
 * @file main.go
 * @brief Standalone Wallet Gateway Server (Yona Wallet Gateway)
 * @details Decouples the frontend mobile/web wallet from core validator node P2P/RPC ports.
 *          Acts as a middleware gateway connecting to Yona Node via gRPC and serving React client.
 *          Indexes blocks in the background to provide transaction history for wallets.
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
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"lukechampine.com/blake3"
)

const RustCryptoContext = "BTC GenZ Toi Gian PoW v1.0"

// GatewayTx represents a normalized transaction for wallet UI
type GatewayTx struct {
	TxID          string `json:"txid"`
	Sender        string `json:"sender"`
	Receiver      string `json:"receiver"`
	Amount        uint64 `json:"amount"`
	Fee           uint64 `json:"fee"`
	Nonce         uint64 `json:"nonce"`
	Timestamp     uint64 `json:"timestamp"`
	Status        int    `json:"status"` // 0: Success, 99: Pending, 1: Error
	BlockHeight   uint64 `json:"blockHeight,omitempty"`
	Confirmations uint64 `json:"confirmations,omitempty"`
}

// MemoryDB stores history database in JSON
type MemoryDB struct {
	mu           sync.RWMutex
	LastHeight   uint64                `json:"last_height"`
	Transactions map[string]*GatewayTx `json:"transactions"` // key: txid
	FilePath     string                `json:"-"`
}

var (
	nodeAddr   string
	listenPort string
	nodeToken  string
	dbPath     string

	grpcClient pb_block.BlockchainServiceClient
	db         *MemoryDB
)

func main() {
	flag.StringVar(&nodeAddr, "node-addr", "localhost:18080", "Address of the Yona Node gRPC server")
	flag.StringVar(&listenPort, "port", "9090", "HTTP port for the Wallet Gateway Server")
	flag.StringVar(&nodeToken, "token", "", "Optional auth token for Node gRPC")
	flag.StringVar(&dbPath, "db", "./data/gateway_index.json", "Path to the transaction history database")
	flag.Parse()

	log.Printf("🚀 Starting Yona Wallet Gateway on port %s", listenPort)
	log.Printf("🔗 Target Yona Node gRPC: %s", nodeAddr)

	// Ensure DB directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Fatalf("❌ Failed to create DB directory: %v", err)
	}

	// Initialize local DB
	db = &MemoryDB{
		Transactions: make(map[string]*GatewayTx),
		FilePath:     dbPath,
	}
	db.load()

	// Connect to gRPC Node
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
		log.Fatalf("❌ Failed to connect to gRPC Node: %v", err)
	}
	defer conn.Close()

	grpcClient = pb_block.NewBlockchainServiceClient(conn)

	// Start block indexer background job
	go runBlockIndexer()

	// Start HTTP Server
	r := mux.NewRouter()

	// CORS Middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Wallet-Token")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Register REST API endpoints
	r.HandleFunc("/api/v1/status", handleStatus).Methods("GET")
	r.HandleFunc("/api/v1/balance/{address}", handleGetBalance).Methods("GET")
	r.HandleFunc("/api/v1/address/{address}/history", handleAddressHistory).Methods("GET")
	r.HandleFunc("/api/v1/tx/{txid}", handleGetTxDetail).Methods("GET")
	r.HandleFunc("/api/v1/send_raw_tx", handleSendRawTx).Methods("POST")

	srv := &http.Server{
		Handler:      r,
		Addr:         ":" + listenPort,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Printf("🟢 Wallet Gateway successfully running on http://localhost:%s", listenPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("❌ HTTP server error: %v", err)
	}
}

// ==========================================
// BACKGROUND BLOCK INDEXER & EVENT TRACKING
// ==========================================

func runBlockIndexer() {
	log.Println("🔍 Background Block Indexer thread started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status, err := grpcClient.GetStatus(ctx, &pb_block.GetStatusRequest{})
		cancel()
		if err != nil {
			log.Printf("⚠️ Failed to query Node status via gRPC: %v", err)
			continue
		}

		db.mu.Lock()
		currentHeight := status.CurrentHeight
		lastIndexed := db.LastHeight
		db.mu.Unlock()

		if currentHeight > lastIndexed {
			log.Printf("📈 Node height: %d | Local index: %d. Syncing new blocks...", currentHeight, lastIndexed)
			for h := lastIndexed + 1; h <= currentHeight; h++ {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				blockRes, err := grpcClient.GetBlock(ctx, &pb_block.GetBlockRequest{Height: h})
				cancel()
				if err != nil {
					log.Printf("❌ Failed to fetch block #%d: %v", h, err)
					break
				}

				if blockRes.Found && blockRes.Block != nil {
					db.indexBlock(blockRes.Block, h, currentHeight)
				}
			}
		} else {
			// Update confirmations for existing non-finalized transactions
			db.mu.Lock()
			for _, tx := range db.Transactions {
				if tx.Status == 0 && tx.BlockHeight > 0 {
					tx.Confirmations = currentHeight - tx.BlockHeight + 1
				}
			}
			db.mu.Unlock()
		}
	}
}

// ==========================================
// REST HANDLERS FOR REACT FRONTEND
// ==========================================

func handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := grpcClient.GetStatus(ctx, &pb_block.GetStatusRequest{})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%v"}`, err), http.StatusInternalServerError)
		return
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
		"sync_progress":    100, // Gateway assumes fully synced once status returns
	})
}

func handleGetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	addrStr := vars["address"]
	addrBytes, err := hex.DecodeString(strings.TrimPrefix(addrStr, "0x"))
	if err != nil || len(addrBytes) != 32 {
		http.Error(w, `{"error": "Invalid address format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acc, err := grpcClient.GetAccount(ctx, &pb_block.GetAccountRequest{Address: addrBytes})
	if err != nil {
		// Fallback to 0 if address is not initialized in ledger state
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"address": addrStr,
		"nonce":   acc.Nonce,
		"balances": map[string]uint64{
			"btc_z":     acc.Balance,
			"spendable": acc.Balance, // Standalone gateway exposes simple account balance
			"pending":   0,
		},
	})
}

func handleAddressHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	addrStr := strings.ToLower(strings.TrimPrefix(vars["address"], "0x"))

	db.mu.RLock()
	defer db.mu.RUnlock()

	var history []GatewayTx
	for _, tx := range db.Transactions {
		cleanSender := strings.ToLower(strings.TrimPrefix(tx.Sender, "0x"))
		cleanReceiver := strings.ToLower(strings.TrimPrefix(tx.Receiver, "0x"))
		if cleanSender == addrStr || cleanReceiver == addrStr {
			history = append(history, *tx)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tx_count": len(history),
		"history":  history,
	})
}

func handleGetTxDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txid := vars["txid"]

	db.mu.RLock()
	tx, exists := db.Transactions[txid]
	db.mu.RUnlock()

	if !exists {
		http.Error(w, `{"error": "Transaction not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Respond in mapped format matching App.tsx's expectations
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ID":            tx.TxID,
		"Sender":        tx.Sender,
		"Receiver":      tx.Receiver,
		"Amount":        tx.Amount,
		"Fee":           tx.Fee,
		"Nonce":         tx.Nonce,
		"Timestamp":     tx.Timestamp,
		"StatusCode":    mapGatewayStatus(tx.Status),
		"Height":        tx.BlockHeight,
		"Confirmations": tx.Confirmations,
	})
}

func mapGatewayStatus(status int) int {
	if status == 0 {
		return 1 // Mapped to success
	}
	if status == 99 {
		return 0 // Mapped to pending/mempool
	}
	return 2 // Mapped to error
}

func handleSendRawTx(w http.ResponseWriter, r *http.Request) {
	var tx pb_block.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, `{"error": "Invalid transaction payload"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hashRes, err := grpcClient.SubmitTransaction(ctx, &tx)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%v"}`, err), http.StatusInternalServerError)
		return
	}

	txidHex := hex.EncodeToString(hashRes.Value)

	// Save to local index as pending
	db.mu.Lock()
	db.Transactions[txidHex] = &GatewayTx{
		TxID:          txidHex,
		Sender:        "0x" + hex.EncodeToString(tx.Sender.Value),
		Receiver:      "0x" + hex.EncodeToString(tx.Receiver.Value),
		Amount:        tx.Amount,
		Fee:           tx.Fee,
		Nonce:         tx.Nonce,
		Timestamp:     tx.Timestamp,
		Status:        99, // Pending in Mempool
		Confirmations: 0,
	}
	db.mu.Unlock()
	db.save()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"txid":    "0x" + txidHex,
	})
}

// ==========================================
// HELPER STORAGE AND ENCODING METHODS
// ==========================================

func (db *MemoryDB) load() {
	db.mu.Lock()
	defer db.mu.Unlock()

	data, err := os.ReadFile(db.FilePath)
	if err != nil {
		return
	}

	json.Unmarshal(data, db)
	if db.Transactions == nil {
		db.Transactions = make(map[string]*GatewayTx)
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
		db.Transactions[txid] = &GatewayTx{
			TxID:          "0x" + txid,
			Sender:        "0x" + hex.EncodeToString(tx.Sender.Value),
			Receiver:      "0x" + hex.EncodeToString(tx.Receiver.Value),
			Amount:        tx.Amount,
			Fee:           tx.Fee,
			Nonce:         tx.Nonce,
			Timestamp:     tx.Timestamp,
			Status:        0, // Success (Mined)
			BlockHeight:   height,
			Confirmations: tip - height + 1,
		}
	}

	db.LastHeight = height
	// Unlock before save to avoid deadlock, or simply defer save at caller.
	// We save right after indexing in calling thread
	go db.save()
}

// Calculate signing hash of transaction natively in Go
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
