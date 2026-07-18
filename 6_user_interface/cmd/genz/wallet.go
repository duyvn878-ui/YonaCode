package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fatih/color"
	"github.com/AlecAivazis/survey/v2"
	"btc_genz/6_user_interface/i18n"
	"btc_genz/6_user_interface/internal"
	pb_block "btc_genz/proto"
	node_p2p "btc_genz/5_node_p2p"
	"google.golang.org/protobuf/proto"
)

var walletManager = internal.NewWalletManager("./data/wallets")

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Quản lý ví và địa chỉ định định (Phụ lục E)",
}

var walletCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Tạo ví mới và hiển thị seed phrase",
	Run: func(cmd *cobra.Command, args []string) {
		name, _ := cmd.Flags().GetString("name")
		pass, _ := cmd.Flags().GetString("password")
		passphrase, _ := cmd.Flags().GetString("passphrase")
		mnemonic, addr, _ := walletManager.CreateWallet(name, pass, passphrase)
		fmt.Printf("[WALLET] ✅ Tạo ví '%s' thành công!\n", name)
		fmt.Printf("[WALLET] 🔑 Địa chỉ: %s\n", addr)
		fmt.Printf("[WALLET] 🛡️ Seed Phrase (12 từ): %s\n", mnemonic)
		fmt.Printf("[WALLET] ⚠️ CẢNH BÁO: Hãy lưu trữ seed phrase này ngoại tuyến.\n")
	},
}

var walletRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Khôi phục ví từ seed phrase",
	Run: func(cmd *cobra.Command, args []string) {
		seed, _ := cmd.Flags().GetString("seed")
		name, _ := cmd.Flags().GetString("name")
		pass, _ := cmd.Flags().GetString("password")
		passphrase, _ := cmd.Flags().GetString("passphrase")
		addr, err := walletManager.RestoreWallet(seed, name, pass, passphrase)
		if err != nil { fmt.Println("Lỗi:", err); return }
		fmt.Printf("[WALLET] ✅ Khôi phục ví thành công: %s\n", addr)
	},
}

var walletBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Kiểm tra số dư của ví",
	Run: func(cmd *cobra.Command, args []string) {
		addrHex, _ := cmd.Flags().GetString("address")
		addrHex = strings.TrimPrefix(addrHex, "0x")
		addrBytes, err := hex.DecodeString(addrHex)
		if err != nil || len(addrBytes) != 32 {
			color.Red("❌ Lỗi: Địa chỉ không hợp lệ (cần 32 bytes hex)")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.GetAccount(ctx, &pb_block.GetAccountRequest{Address: addrBytes})
		if err != nil {
			color.Red("❌ Lỗi truy vấn từ Node: %v", err)
			return
		}

		color.Cyan("\n📊 THÔNG TIN TÀI KHOẢN (VANGUARD LEDGER)")
		fmt.Printf("-------------------------------------------\n")
		fmt.Printf("🔑 Địa chỉ : 0x%s\n", addrHex)
		fmt.Printf("💰 Số dư   : %.8f GO\n", float64(resp.Balance)/100_000_000)
		fmt.Printf("🔢 Nonce   : %d\n", resp.Nonce)
		
		fmt.Printf("-------------------------------------------\n")
	},
}

var walletSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Gửi GO tới địa chỉ khác (Send GO)",
	Run: func(cmd *cobra.Command, args []string) {
		color.Cyan("\n" + i18n.T("wallet_send_title") + "\n")

		var from, to, pass string
		var amount float64

		// Lấy giá trị từ Flags trước
		from, _ = cmd.Flags().GetString("from")
		to, _ = cmd.Flags().GetString("to")
		amount, _ = cmd.Flags().GetFloat64("amount")
		pass, _ = cmd.Flags().GetString("password")

		// Nếu thiếu thông tin bắt buộc, mới hỏi qua Survey
		if from == "" || to == "" || amount == 0 {
			qs := []*survey.Question{
				{
					Name: "from",
					Prompt: &survey.Input{Message: "Wallet Name (standard):", Default: "standard"},
				},
				{
					Name: "to",
					Prompt: &survey.Input{Message: "Receiver Address (btz1q.../0x...):"},
				},
				{
					Name: "amount",
					Prompt: &survey.Input{Message: "Amount (GO):"},
				},
				{
					Name: "pass",
					Prompt: &survey.Password{Message: "Wallet Password:"},
				},
			}

			answers := struct {
				From   string `survey:"from"`
				To     string `survey:"to"`
				Amount string `survey:"amount"`
				Pass   string `survey:"pass"`
			}{}

			err := survey.Ask(qs, &answers)
			if err != nil { fmt.Println(err.Error()); return }

			from = answers.From
			fmt.Sscanf(answers.Amount, "%f", &amount)
			to = answers.To
			pass = answers.Pass
		}

		seed, err := walletManager.GetSeed(from, pass)
		if err != nil { color.Red("❌ Error: Invalid password or wallet not found"); return }
		
		color.Yellow("🛰️ Signing transaction...")
		privKey := ed25519.NewKeyFromSeed(seed)
		pubKey := privKey.Public().(ed25519.PublicKey)
		senderAddr := make([]byte, 32); copy(senderAddr, pubKey)
		
		receiverAddr, _ := hex.DecodeString(strings.TrimPrefix(to, "0x"))
		
		// 1. Truy vấn Nonce & Số dư thực tế (Audit S3 FIX)
		accResp, err := client.GetAccount(context.Background(), &pb_block.GetAccountRequest{Address: senderAddr})
		if err != nil { color.Red("❌ Error: Could not fetch account state from Node: %v", err); return }
		
		statusResp, err := client.GetStatus(context.Background(), &pb_block.GetStatusRequest{})
		if err != nil { color.Red("❌ Error: Could not connect to Node RPC: %v", err); return }
		
		blockHeight := statusResp.CurrentHeight
		if blockHeight > 0 { blockHeight-- } // Lấy hash của khối trước đó để làm RecentBlockHash
		
		blockResp, err2 := client.GetBlock(context.Background(), &pb_block.GetBlockRequest{Height: blockHeight})
		var recentHash []byte
		if err2 == nil && blockResp.Found {
			headerBytes, _ := proto.Marshal(blockResp.Block.Header)
			hashResp, err := client.CalculateBlockHeaderHash(context.Background(), &pb_block.RawBytes{Data: headerBytes})
			if err == nil {
				recentHash = hashResp.Value
			} else {
				recentHash = make([]byte, 32)
			}
		} else {
			recentHash = make([]byte, 32)
		}
		
		vntAmount := uint64(amount * 100_000_000)
		fee := uint64(1000)
 
		// Kiểm tra số dư (Layer 1 Guard)
		if vntAmount + fee > accResp.Balance {
			color.Red("❌ Error: Insufficient balance. Available: %.8f GO", float64(accResp.Balance)/100_000_000)
			return
		}
 
		fmt.Printf("\n%s\n", i18n.T("wallet_send_confirm"))
		fmt.Printf("From   : %s (Address: %s)\n", from, hex.EncodeToString(senderAddr))
		fmt.Printf("To     : %s\n", to)
		fmt.Printf("Amount : %.8f GO\n", amount)
		fmt.Printf("Fee    : %.8f GO\n", float64(fee)/100_000_000)
		fmt.Printf("Nonce  : %d\n", accResp.Nonce)
 
		confirm := false
		// Tự động confirm nếu chạy qua cmd flag (automation)
		shouldConfirm, _ := cmd.Flags().GetBool("yes")
		if shouldConfirm {
			confirm = true
		} else {
			prompt := &survey.Confirm{Message: "Confirm send?", Default: true}
			survey.AskOne(prompt, &confirm)
		}
 
		if !confirm {
			color.Yellow("🛑 Cancelled.")
			return
		}
 
		// Tạo giao dịch với Nonce chuẩn từ SMT
		tx := &pb_block.Transaction{
			Version:         1,
			Sender:          &pb_block.Address{Value: senderAddr},
			Receiver:        &pb_block.Address{Value: receiverAddr},
			Amount:          vntAmount,
			Fee:             fee,
			Nonce:           accResp.Nonce,
			Timestamp:       uint64(time.Now().Unix()),
			RecentBlockHash: recentHash,
			ChainId:         25062025, // [VANGUARD] Định danh mạng YonaCode Mainnet
		}

		// 2. Ký với cơ chế Vanguard (Bất biến cao độ)
		signingHash := node_p2p.GetSigningHashNative(tx)
		signature := ed25519.Sign(privKey, signingHash)
		tx.Signature = &pb_block.Signature{Value: signature}

		// 3. Phát sóng
		color.Yellow("📡 Broadcasting...")
		_, err = client.SubmitTransaction(context.Background(), tx)
		if err != nil {
			color.Red(i18n.T("wallet_send_fail"), err)
		} else {
			color.Green(i18n.T("wallet_send_success"))
		}
	},
}

// --- LỆNH LIỆT KÊ VÍ (WALLET LIST) ---
// Tại sao thiết kế như vậy: Sử dụng hàm ListWallets() có sẵn từ WalletManager 
// để hiển thị danh sách trực quan các ví đã tạo cục bộ, nâng cao UX cho người dùng.
var walletListCmd = &cobra.Command{
	Use:   "list",
	Short: "Danh sách tất cả các ví cục bộ",
	Run: func(cmd *cobra.Command, args []string) {
		wallets, err := walletManager.ListWallets()
		if err != nil {
			color.Red("❌ Lỗi lấy danh sách ví: %v", err)
			return
		}
		if len(wallets) == 0 {
			color.Yellow("⚠️ Chưa có ví nào được tạo cục bộ.")
			return
		}
		color.Green("\n👛 DANH SÁCH VÍ CỤC BỘ:")
		fmt.Printf("-------------------------------------------\n")
		for _, w := range wallets {
			fmt.Printf("📂 Tên ví  : %s\n", w.Name)
			fmt.Printf("🔑 Địa chỉ : 0x%s\n", w.Address)
			fmt.Printf("-------------------------------------------\n")
		}
	},
}

var walletDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Xóa một ví cục bộ khỏi thiết bị (Logout/Delete)",
	Run: func(cmd *cobra.Command, args []string) {
		addrHex, _ := cmd.Flags().GetString("address")
		if addrHex == "" {
			color.Red("❌ Lỗi: Vui lòng cung cấp địa chỉ ví cần xóa bằng flag --address")
			return
		}
		
		confirm := false
		prompt := &survey.Confirm{
			Message: fmt.Sprintf("Bạn có chắc chắn muốn XÓA ví %s khỏi thiết bị không? Hành động này không thể hoàn tác!", addrHex),
			Default: false,
		}
		survey.AskOne(prompt, &confirm)
		
		if !confirm {
			color.Yellow("🛑 Đã hủy bỏ thao tác xóa.")
			return
		}

		err := walletManager.DeleteWallet(addrHex)
		if err != nil {
			color.Red("❌ Lỗi: %v", err)
			return
		}
		color.Green("✅ Đã xóa ví %s thành công!", addrHex)
	},
}

func init() {
	rootCmd.AddCommand(walletCmd)
	walletCmd.AddCommand(walletCreateCmd, walletRestoreCmd, walletBalanceCmd, walletSendCmd, walletListCmd, walletDeleteCmd)
	
	walletCreateCmd.Flags().String("name", "standard", "Tên ví")
	walletDeleteCmd.Flags().String("address", "", "Địa chỉ ví cần xóa")
	walletCreateCmd.Flags().String("password", "", "Mật khẩu bảo vệ (Local PIN)")
	walletCreateCmd.Flags().String("passphrase", "", "BIP39 Passphrase (tùy chọn - từ thứ 13)")
	
	walletRestoreCmd.Flags().String("seed", "", "Seed phrase (12/24 từ)")
	walletRestoreCmd.Flags().String("name", "restored", "Tên ví")
	walletRestoreCmd.Flags().String("password", "", "Mật khẩu bảo vệ (Local PIN)")
	walletRestoreCmd.Flags().String("passphrase", "", "BIP39 Passphrase (tùy chọn - từ thứ 13)")
	
	walletBalanceCmd.Flags().String("address", "", "Địa chỉ ví GO")
	
	walletSendCmd.Flags().String("from", "", "Địa chỉ gửi")
	walletSendCmd.Flags().String("to", "", "Địa chỉ nhận")
	walletSendCmd.Flags().Float64("amount", 0, "Số lượng GO")
	walletSendCmd.Flags().String("password", "", "Mật khẩu ví")
	walletSendCmd.Flags().Bool("yes", false, "Tự động xác nhận giao dịch")
}
