package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"btc_genz/2_miner_core/go_bridge"
	pb_block "btc_genz/proto"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

// --- NHÓM LỆNH TRUY VẤN SỔ CÁI (QUERY ROOT COMMAND) ---
var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "🔍 Truy vấn thông tin Sổ cái (Blockchain, Tx, Balance, Supply)",
}

// 1. Truy vấn Khối (query block <height/hash>)
var queryBlockCmd = &cobra.Command{
	Use:   "block <height hoặc hash>",
	Short: "Lấy chi tiết một khối theo chiều cao hoặc mã băm",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var req *pb_block.GetBlockRequest
		var height uint64
		// Kiểm tra nếu tham số đầu vào là số nguyên (chiều cao khối)
		if _, err := fmt.Sscanf(target, "%d", &height); err == nil {
			req = &pb_block.GetBlockRequest{Height: height}
			color.Cyan("[BLOCKCHAIN] 📡 Đang truy vấn khối theo cao độ: #%d...", height)
		} else {
			// Ngược lại, xem như là mã băm Hex
			cleanHash := strings.TrimPrefix(target, "0x")
			hashBytes, err := hex.DecodeString(cleanHash)
			if err != nil || len(hashBytes) != 32 {
				color.Red("❌ Tham số khối không hợp lệ. Vui lòng truyền số (height) hoặc mã băm hex 32-bytes.")
				return
			}
			req = &pb_block.GetBlockRequest{Hash: hashBytes}
			color.Cyan("[BLOCKCHAIN] 📡 Đang truy vấn khối theo mã băm: 0x%s...", cleanHash)
		}

		resp, err := client.GetBlock(ctx, req)
		if err != nil {
			color.Red("❌ Lỗi truy vấn từ Node RPC: %v", err)
			return
		}

		if resp == nil || !resp.Found || resp.Block == nil {
			color.Yellow("⚠️ Không tìm thấy khối yêu cầu.")
			return
		}

		// Tính toán Block Hash để đối soát đồng bộ
		blockHash := "N/A"
		headerRaw, err := proto.Marshal(resp.Block.Header)
		if err == nil {
			hashResp, err := client.CalculateBlockHeaderHash(ctx, &pb_block.RawBytes{Data: headerRaw})
			if err == nil {
				blockHash = hex.EncodeToString(hashResp.Value)
			}
		}

		if jsonOutput {
			bz, _ := json.MarshalIndent(resp.Block, "", "  ")
			fmt.Println(string(bz))
			return
		}

		color.Green("\n💎 CHI TIẾT KHỐI (VANGUARD BLOCK)")
		fmt.Printf("-------------------------------------------\n")
		fmt.Printf("📦 Cao độ    : #%d\n", resp.Block.Header.Height)
		fmt.Printf("🆔 Hash      : 0x%s\n", blockHash)
		fmt.Printf("📅 Thời gian : %s\n", time.Unix(int64(resp.Block.Header.Timestamp), 0).Format(time.RFC3339))
		
		minerAddr := "0x"
		if resp.Block.Header.MinerAddress != nil {
			minerAddr += hex.EncodeToString(resp.Block.Header.MinerAddress.Value)
		}
		fmt.Printf("👤 Người đào : %s\n", minerAddr)

		stateRoot := "0x"
		if resp.Block.Header.StateRoot != nil {
			stateRoot += hex.EncodeToString(resp.Block.Header.StateRoot.Value)
		}
		fmt.Printf("🌳 StateRoot : %s\n", stateRoot)
		
		txCount := 0
		if resp.Block.Body != nil {
			txCount = len(resp.Block.Body.Transactions)
		}
		fmt.Printf("💸 Giao dịch : %d giao dịch\n", txCount)
		fmt.Printf("-------------------------------------------\n")
		
		if resp.Block.Body != nil {
			for i, tx := range resp.Block.Body.Transactions {
				sender := "COINBASE"
				if tx.Sender != nil {
					sender = "0x" + hex.EncodeToString(tx.Sender.Value)
				}
				receiver := "UNKNOWN"
				if tx.Receiver != nil {
					receiver = "0x" + hex.EncodeToString(tx.Receiver.Value)
				}
				fmt.Printf("  [%d] From: %s | To: %s | Amount: %.8f GO\n", 
					i, sender, receiver, float64(tx.Amount)/100_000_000)
			}
		}
		fmt.Printf("-------------------------------------------\n")
	},
}

// 2. Truy vấn Giao dịch (query tx <txid>)
var queryTxCmd = &cobra.Command{
	Use:   "tx <txid>",
	Short: "Lấy chi tiết một giao dịch theo TxID (Offline DB Read)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		txid := args[0]
		if len(txid) > 2 && txid[:2] == "0x" {
			txid = txid[2:]
		}

		hash, err := hex.DecodeString(txid)
		if err != nil {
			color.Red("❌ Tx Hash không hợp lệ: %v", err)
			return
		}

		// Giải quyết đường dẫn CSDL
		pathFlag, _ := cmd.Flags().GetString("path")
		sclPath := pathFlag
		if cmd.Flags().Changed("db-path") || !cmd.Flags().Changed("path") {
			sclPath = filepath.Join(dbPath, "scl")
		}

		color.Yellow("[TX] 🔍 Đang truy vấn giao dịch: 0x%s tại: %s...\n", txid, sclPath)
		
		br := go_bridge.NewBridge(50099)
		br.InitSCL(sclPath)
		defer br.Close()

		height, status, finalized, confirms, _, _, _, _ := br.GetTransactionStatus(hash)

		color.Cyan("\n[TRANSACTION INFO] 🧾 Tx Hash: 0x%s", txid)
		if height == 0 && status == 0 {
			color.Red("   => Trạng thái: ❌ KHÔNG TÌM THẤY (Hoặc chưa được đưa vào khối)")
			return
		}

		statusStr := "Đang chờ (Pending)"
		if status == 1 { statusStr = "Thành công (Success)" }
		if status == 2 { statusStr = "Thất bại (Failed)" }

		if status == 1 {
			color.Green("   => Trạng thái:   %s", statusStr)
		} else {
			color.Red("   => Trạng thái:   %s", statusStr)
		}

		color.Yellow("   => Tại khối:     #%d", height)
		color.Yellow("   => Xác nhận:     %d Confirmations", confirms)
		
		blockRaw := br.GetBlock(height)
		if blockRaw != nil {
			var fullBlock pb_block.Block
			if err := proto.Unmarshal(blockRaw, &fullBlock); err == nil && fullBlock.Body != nil {
				for _, tx := range fullBlock.Body.Transactions {
					txData, _ := proto.Marshal(tx)
					h_full := br.GetCanonicalTxHash(txData, height)
					
					if hex.EncodeToString(h_full) == txid {
						senderHex := hex.EncodeToString(tx.Sender.Value)
						receiverHex := hex.EncodeToString(tx.Receiver.Value)
						amountBTC := float64(tx.Amount) / 100000000.0

						color.Cyan("\n[Dòng tiền - Ledger Flow]:")
						color.Red("   [-] Người gửi (Sender):   0x%s...", senderHex[:16])
						color.Green("   [+] Người nhận (Receiver): 0x%s...", receiverHex[:16])
						color.Yellow("   [=] Số tiền (Amount):     %.8f GO", amountBTC)
						
						if status == 1 {
							color.Green("\n✅ KẾT LUẬN: Số dư ĐÃ được khấu trừ từ ví Sender và ĐÃ ghi nhận vào ví Receiver trong Sổ cái.")
						}
						break
					}
				}
			}
		}

		if finalized {
			color.Green("   => Kết toán:     ✅ ĐÃ CHỐT (Finalized)")
		} else {
			color.Magenta("   => Kết toán:     ⏳ CHƯA CHỐT (Unfinalized)")
		}
	},
}

// 3. Truy vấn Số dư (query balance <address>)
// Tại sao thiết kế như vậy: Tự động phát hiện trạng thái gRPC Client. Nếu Node đang bật, 
// ta lấy số dư trực tiếp qua RPC mạng lưới. Nếu Node đang tắt, ta fallback truy vấn 
// ngoại tuyến trực tiếp từ file CSDL Sổ cái cục bộ để giúp thợ đào/lập trình viên cứu hộ nhanh.
var queryBalanceCmd = &cobra.Command{
	Use:   "balance <address>",
	Short: "Kiểm tra số dư và Nonce của một địa chỉ ví (Tự động Fallback Online/Offline)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		addrHex := args[0]
		addrHex = strings.TrimPrefix(addrHex, "0x")
		addrBytes, err := hex.DecodeString(addrHex)
		if err != nil || len(addrBytes) != 32 {
			color.Red("❌ Lỗi: Địa chỉ ví không hợp lệ (cần 32 bytes hex)")
			return
		}

		// Bước 1: Thử truy vấn online qua gRPC
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if client != nil {
			resp, err := client.GetAccount(ctx, &pb_block.GetAccountRequest{Address: addrBytes})
			if err == nil {
				color.Green("\n📊 THÔNG TIN TÀI KHOẢN (ONLINE RPC)")
				fmt.Printf("-------------------------------------------\n")
				fmt.Printf("🔑 Địa chỉ : 0x%s\n", addrHex)
				fmt.Printf("💰 Số dư   : %.8f GO\n", float64(resp.Balance)/100_000_000)
				fmt.Printf("🔢 Nonce   : %d\n", resp.Nonce)
				fmt.Printf("-------------------------------------------\n")
				return
			}
		}

		// Bước 2: Node offline -> Tự động Fallback sang CSDL local
		pathFlag, _ := cmd.Flags().GetString("path")
		sclPath := pathFlag
		if cmd.Flags().Changed("db-path") || !cmd.Flags().Changed("path") {
			sclPath = filepath.Join(dbPath, "scl")
		}

		color.Yellow("📡 Node RPC offline. Đang tự động chuyển sang đọc Sổ cái cục bộ tại: %s", sclPath)
		if _, err := os.Stat(sclPath); os.IsNotExist(err) {
			color.Red("❌ Lỗi: Không tồn tại thư mục dữ liệu Sổ cái tại: %s. Vui lòng kiểm tra lại cờ --db-path.", sclPath)
			return
		}

		br := go_bridge.NewBridge(50099)
		br.InitSCL(sclPath)
		defer br.Close()

		balance := br.GetBalance(nil, addrBytes, 0)
		spendable := br.GetSpendableBalance(addrBytes)
		nonce := br.GetNonce(nil, addrBytes)

		color.Cyan("\n👛 THÔNG TIN TÀI KHOẢN (OFFLINE DATABASE)")
		fmt.Printf("-------------------------------------------\n")
		fmt.Printf("🔑 Địa chỉ : 0x%s\n", addrHex)
		fmt.Printf("💰 Số dư tổng (Total):     %.8f GO\n", float64(balance)/100_000_000)
		fmt.Printf("   Khả dụng (Spendable):   %.8f GO\n", float64(spendable)/100_000_000)
		fmt.Printf("🔢 Số thứ tự (Nonce):     %d\n", nonce)
		if balance > spendable {
			color.Magenta("⚠️  Lưu ý: Có %.8f GO đang trong trạng thái 'chín' (Maturing).", float64(balance-spendable)/100_000_000)
		}
		fmt.Printf("-------------------------------------------\n")
	},
}

// 4. Kiểm toán Tổng cung (query supply)
var querySupplyCmd = &cobra.Command{
	Use:   "supply",
	Short: "Kiểm toán tổng cung thực tế trong cơ sở dữ liệu Sổ cái",
	Run: func(cmd *cobra.Command, args []string) {
		pathFlag, _ := cmd.Flags().GetString("path")
		sclPath := pathFlag
		if cmd.Flags().Changed("db-path") || !cmd.Flags().Changed("path") {
			sclPath = filepath.Join(dbPath, "scl")
		}

		color.Yellow("[SUPPLY] 🔎 Đang tiến hành kiểm toán tổng cung ngoại tuyến từ: %s", sclPath)
		if _, err := os.Stat(sclPath); os.IsNotExist(err) {
			color.Red("❌ Lỗi: Thư mục CSDL không tồn tại: %s", sclPath)
			return
		}

		br := go_bridge.NewBridge(50099)
		br.InitSCL(sclPath)
		defer br.Close()
		
		height := br.GetCurrentVersion()
		expected := br.CalculateExpectedSupply(height)
		actual := br.CalculateActualTotalSupply()
		
		color.Cyan("\n[SUPPLY AUDIT] 💰 Kiểm toán Cán cân Kế toán:")
		fmt.Printf("   => Chiều cao hiện tại : #%d\n", height)
		fmt.Printf("   => Tổng cung lý thuyết (Expected): %.2f GO\n", float64(expected)/100000000.0)
		fmt.Printf("   => Tổng cung thực tế (Actual):    %.2f GO\n", float64(actual)/100000000.0)
		
		if expected > actual {
			color.Red("\n⚠️ CẢNH BÁO: Phát hiện 'Lệch Cán Cân'! %.2f GO chưa được ghi nhận vào Sổ cái.", float64(expected-actual)/100000000.0)
			color.Yellow("💡 Gợi ý: Hãy chạy lệnh 'node repair resync --db-path %s' để tái thiết lập lại cán cân.", dbPath)
		} else {
			color.Green("\n✅ Hệ thống cân bằng tuyệt đối.")
		}
	},
}

// 5. Xem Mempool (query mempool)
var queryMempoolCmd = &cobra.Command{
	Use:   "mempool",
	Short: "Hiển thị các giao dịch đang chờ xác nhận trên hàng đợi",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("[MEMPOOL] Các giao dịch đang xếp hàng (VNT-Queue)...")
	},
}

// 6. Quét Ví có số dư (query scan)
var queryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "🔎 Tìm kiếm và liệt kê tất cả địa chỉ ví có số dư trong Sổ cái",
	Run: func(cmd *cobra.Command, args []string) {
		pathFlag, _ := cmd.Flags().GetString("path")
		sclPath := pathFlag
		if cmd.Flags().Changed("db-path") || !cmd.Flags().Changed("path") {
			sclPath = filepath.Join(dbPath, "scl")
		}

		color.Yellow("\n[AUDIT] 🔎 Đang quét Sổ cái Ledger cục bộ tại: %s", sclPath)
		if _, err := os.Stat(sclPath); os.IsNotExist(err) {
			color.Red("❌ Lỗi: Thư mục CSDL không tồn tại: %s", sclPath)
			return
		}
		
		br := go_bridge.NewBridge(50099)
		br.InitSCL(sclPath)
		defer br.Close()
		
		snapshot := br.ExportStateSnapshot()
		if len(snapshot) == 0 {
			color.Red("🔴 KHÔNG TÌM THẤY DỮ LIỆU TÀI KHOẢN TRONG SỔ CÁI!")
			return
		}
		
		fmt.Printf("\n%-20s | %-20s | %-10s\n", "ĐỊA CHỈ (ADDRESS)", "SỐ DƯ (BALANCE)", "UNIT")
		fmt.Println("----------------------------------------------------------------------")
		
		var totalConfirmed uint64 = 0
		for _, acc := range snapshot {
			addrStr := hex.EncodeToString(acc.Address[:])
			balanceBTC := float64(acc.Balance) / 100000000.0
			
			if acc.Balance > 0 {
				color.Green("%-20s | %-12.8f GO | %s", 
					"0x"+addrStr[:8]+"...", 
					balanceBTC,
					"GO")
				totalConfirmed += acc.Balance
			}
		}
		
		color.Cyan("\n[TỔNG KẾT] 🛡️ Tổng cung thực tế lưu trong Sổ cái: %.8f GO", float64(totalConfirmed)/100000000.0)
	},
}

// 7. Xem State Root (query root)
var queryRootCmd = &cobra.Command{
	Use:   "root",
	Short: "🌳 Kiểm tra chiều cao và Rễ Merkle (State Root) hiện tại của Sổ cái",
	Run: func(cmd *cobra.Command, args []string) {
		pathFlag, _ := cmd.Flags().GetString("path")
		sclPath := pathFlag
		if cmd.Flags().Changed("db-path") || !cmd.Flags().Changed("path") {
			sclPath = filepath.Join(dbPath, "scl")
		}

		if _, err := os.Stat(sclPath); os.IsNotExist(err) {
			color.Red("❌ Lỗi: Thư mục CSDL không tồn tại: %s", sclPath)
			return
		}

		br := go_bridge.NewBridge(50099)
		br.InitSCL(sclPath)
		defer br.Close()
		
		root := br.GetStateRoot()
		height := br.GetCurrentVersion()
		
		color.Cyan("\n[STATE ROOT] 🌲 Thông tin Sổ cái hiện tại:")
		fmt.Printf("   => Chiều cao (Height): #%d\n", height)
		fmt.Printf("   => Rễ Merkle (Root):   0x%s\n", hex.EncodeToString(root))
	},
}

func init() {
	// Đăng ký các cờ toàn cục cho nhóm lệnh query (PersistentFlags)
	queryCmd.PersistentFlags().String("path", "./data/scl", "Đường dẫn trực tiếp tới CSDL Sổ cái (SCL Path)")

	// Gắn các lệnh con vào queryCmd
	queryCmd.AddCommand(queryBlockCmd, queryTxCmd, queryBalanceCmd, querySupplyCmd, queryMempoolCmd, queryScanCmd, queryRootCmd)

	// Gắn queryCmd vào rootCmd toàn cục
	rootCmd.AddCommand(queryCmd)
}
