package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"btc_genz/2_miner_core/go_bridge"
	node_p2p "btc_genz/5_node_p2p"
	user_interface "btc_genz/6_user_interface"
	"btc_genz/6_user_interface/internal"
	pb_block "btc_genz/proto"

	"github.com/fatih/color"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	port        int
	p2pPort     int
	sclPort     int
	dbPath      string
	peersStr    string
	mining      bool
	syncMode    string
	rewardAddrHex string
	minerPIN    string
)

func main() {
	// 1. Parse command-line flags
	flag.IntVar(&port, "port", 8080, "RPC/Web API Port")
	flag.IntVar(&p2pPort, "p2p-port", 9000, "P2P Network Port")
	flag.IntVar(&sclPort, "scl-port", 0, "SCL Engine Port")
	flag.StringVar(&dbPath, "db-path", "node", "Database Directory Path")
	flag.StringVar(&peersStr, "peers", "", "Comma-separated initial peer addresses")
	flag.BoolVar(&mining, "mining", false, "Enable mining (PoW)")
	flag.StringVar(&syncMode, "sync-mode", "snap", "Sync mode: 'snap' or 'full'")
	flag.StringVar(&rewardAddrHex, "reward-address", "0000000000000000000000000000000000000000000000000000000000000000", "Mining reward recipient address")
	flag.StringVar(&minerPIN, "miner-pin", "", "Wallet PIN/Password for mining key decryption")
	flag.Parse()

	// 2. Resolve database path to absolute
	isAbs := filepath.IsAbs(dbPath)
	if !isAbs && len(dbPath) > 2 && dbPath[1] == ':' {
		isAbs = true
	}
	if !isAbs {
		cwd, err := os.Getwd()
		if err == nil {
			dbPath = filepath.Join(cwd, dbPath)
		}
	}
	os.MkdirAll(dbPath, 0755)

	// 3. Silent Mode: Redirect all system logs to node_system.log
	logFile := &lumberjack.Logger{
		Filename:   filepath.Join(dbPath, "node_system.log"),
		MaxSize:    50, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Enable environment variable to tell Rust Core to write logs to scl_server.log
	os.Setenv("SCL_LOG_TO_FILE", "true")

	color.Cyan("🚀 [YonaCode CLI Node] Starting background services...")
	fmt.Printf("⚓ Data Directory: %s\n", dbPath)

	// 4. Port allocation checks
	if sclPort == 0 {
		sclPort = port + 42000
	}
	availableP2PPort, err := go_bridge.FindAvailableP2PPort(p2pPort)
	if err == nil {
		p2pPort = availableP2PPort
	}

	// 5. Decode mining reward address
	rewardAddr, _ := hex.DecodeString(rewardAddrHex)
	if len(rewardAddr) == 0 {
		rewardAddr = make([]byte, 32)
	}

	var minerKey ed25519.PrivateKey
	if len(rewardAddr) == 32 && mining {
		wm := internal.NewWalletManager(filepath.Join(dbPath, "wallets"))
		addrHex := hex.EncodeToString(rewardAddr)
		seed, err := wm.GetSeed(addrHex, minerPIN)
		if err == nil && len(seed) >= 32 {
			minerKey = ed25519.NewKeyFromSeed(seed[:32])
			log.Printf("[MINER] Loaded private mining key for: 0x%s", addrHex[:16])
		}
	}

	// Parse initial peers list
	var peers []string
	if peersStr != "" {
		peers = strings.Split(peersStr, ",")
	}

	// 6. Initialize and start Headless Go Node
	app := user_interface.NewCLIApp(dbPath, rewardAddr, minerKey, sclPort)
	if mining {
		app.SetNodeMode("full-mining")
	} else {
		app.SetNodeMode("verify-only")
	}
	app.SetSyncMode(syncMode)

	// Run Go Node in the background
	go app.StartNode(port, p2pPort, peers, minerPIN, "", "none", false, true, 1000)

	// Wait 3 seconds for internal gRPC server to start
	time.Sleep(3 * time.Second)

	// 7. Establish internal gRPC connection
	tokenFile := filepath.Join(dbPath, ".auth_token")
	var token string
	if data, err := os.ReadFile(tokenFile); err == nil {
		token = strings.TrimSpace(string(data))
	}

	dialOpts := []grpc.DialOption{grpc.WithInsecure()}
	if token != "" {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-auth-token", token)
			return invoker(ctx, method, req, reply, cc, opts...)
		}))
	}

	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", port+10000), dialOpts...)
	if err != nil {
		color.Red("❌ Error: Could not connect to internal node gRPC server: %v", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := pb_block.NewBlockchainServiceClient(conn)

	// 8. Run interactive REPL Shell
	runInteractiveShell(client, dbPath)
}

func runInteractiveShell(client pb_block.BlockchainServiceClient, dbPath string) {
	// Print Welcome Banner
	// Sửa ký tự backtick thành dấu nháy đơn để tránh lỗi cú pháp Go raw string
	welcomeBanner := color.CyanString(`
  __   __              _   _        ____          _         ____       
  \ \ / /__  _ __   __ _| \ | |      / ___|___   __| | ___   / ___| ___  
   \ V / _ \| '_ \ / _' |  \| |     | |   / _ \ / _' |/ _ \ | |  _ / _ \ 
    | | (_) | | | | (_| | |\  |     | |___ (_) | (_| |  __/ | |_| | (_) |
    |_|\___/|_| |_|\__,_|_| \_|      \____\___/ \__,_|\___|  \____|\___/ 
  -----------------------------------------------------------------------
  ⚓ [YonaCode CLI Node] - READY & RUNNING (Silent background logging)
  💡 Type 'help' to view available commands, type 'exit' to quit.
  -----------------------------------------------------------------------`)
	fmt.Println(welcomeBanner)

	scanner := bufio.NewScanner(os.Stdin)
	walletManager := internal.NewWalletManager(filepath.Join(dbPath, "wallets"))

	for {
		fmt.Print("cli_yona_code > ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		tokens := strings.Fields(line)
		cmd := strings.ToLower(tokens[0])

		switch cmd {
		case "help":
			printHelp()
		case "status", "info":
			showStatus(client)
		case "wallets":
			showWallets(client, walletManager)
		case "send":
			handleGuidedSend(client, walletManager, scanner)
		case "exit", "quit":
			color.Yellow("🛑 Closing database and disconnecting P2P peers. Please wait...")
			time.Sleep(1 * time.Second)
			color.Green("👋 Safely exited.")
			os.Exit(0)
		default:
			color.Red("❌ Error: Invalid command. Type 'help' to view available commands.")
		}
	}
}

func printHelp() {
	color.Green("\n📖 AVAILABLE CLI COMMANDS:")
	fmt.Printf("  %-15s : Display node status (Height, connected peers, hashrate, mempool)\n", "status")
	fmt.Printf("  %-15s : List all local wallets with actual balances\n", "wallets")
	fmt.Printf("  %-15s : Transfer GO (Guided step-by-step input)\n", "send")
	fmt.Printf("  %-15s : Display this help menu\n", "help")
	fmt.Printf("  %-15s : Stop services and safely exit the node\n", "exit / quit")
	fmt.Println()
}

func showStatus(client pb_block.BlockchainServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.GetStatus(ctx, &pb_block.GetStatusRequest{})
	if err != nil {
		color.Red("❌ Error: Node offline or gRPC server not responding: %v", err)
		return
	}

	color.Cyan("\n📊 SYSTEM STATUS:")
	fmt.Println("-------------------------------------------")
	fmt.Printf("  Block Height      : #%d\n", resp.CurrentHeight)
	fmt.Printf("  Connected Peers   : %d\n", resp.PeerCount)
	fmt.Printf("  Network Hashrate  : %d H/s\n", resp.Hashrate)
	modeStr := "verify-only (Safe Mode)"
	if resp.IsMining {
		modeStr = "full-mining (PoW Mining Active)"
	}
	fmt.Printf("  Operating Mode    : %s\n", modeStr)
	fmt.Println("-------------------------------------------")
	fmt.Println()
}

func showWallets(client pb_block.BlockchainServiceClient, wm *internal.WalletManager) {
	wallets, err := wm.ListWallets()
	if err != nil {
		color.Red("❌ Error: Could not read wallet files: %v", err)
		return
	}

	if len(wallets) == 0 {
		color.Yellow("⚠️ No local wallets found on this device.")
		return
	}

	color.Green("\n👛 LOCAL WALLETS & BALANCES:")
	fmt.Println("---------------------------------------------------------------------------------")
	fmt.Printf("  %-3s | %-12s | %-66s | %-15s\n", "No.", "Wallet Name", "Wallet Address", "Balance (GO)")
	fmt.Println("---------------------------------------------------------------------------------")
	
	for i, w := range wallets {
		addrBytes, err := hex.DecodeString(w.Address)
		balance := 0.0
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			accResp, err := client.GetAccount(ctx, &pb_block.GetAccountRequest{Address: addrBytes})
			cancel()
			if err == nil {
				balance = float64(accResp.Balance) / 100_000_000.0
			}
		}
		fmt.Printf("  %-3d | %-12s | 0x%-64s | %.8f GO\n", i+1, w.Name, w.Address, balance)
	}
	fmt.Println("---------------------------------------------------------------------------------")
	fmt.Println()
}

func handleGuidedSend(client pb_block.BlockchainServiceClient, wm *internal.WalletManager, scanner *bufio.Scanner) {
	color.Yellow("\n💸 STARTING GUIDED TRANSACTION PROCESS")

	wallets, err := wm.ListWallets()
	if err != nil || len(wallets) == 0 {
		color.Red("❌ Error: No local wallets available to send from.")
		return
	}

	// 1. Select sender wallet
	fmt.Println("Available sending wallets:")
	for i, w := range wallets {
		fmt.Printf("  [%d] %s (0x%s...)\n", i+1, w.Name, w.Address[:8])
	}
	fmt.Print("👉 Enter sending wallet number: ")
	if !scanner.Scan() { return }
	choiceIdx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choiceIdx < 1 || choiceIdx > len(wallets) {
		color.Red("❌ Error: Invalid selection.")
		return
	}
	senderWallet := wallets[choiceIdx-1]
	senderAddrBytes, _ := hex.DecodeString(senderWallet.Address)

	// Fetch sender account state (nonce + balance) via gRPC
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	accResp, err := client.GetAccount(ctx, &pb_block.GetAccountRequest{Address: senderAddrBytes})
	cancel()
	if err != nil {
		color.Red("❌ Error: Could not fetch sender account state: %v", err)
		return
	}

	// 2. Input receiver wallet address
	fmt.Print("👉 Enter receiver wallet address (32-bytes hex / 0x...): ")
	if !scanner.Scan() { return }
	receiverStr := strings.TrimSpace(scanner.Text())
	receiverStr = strings.TrimPrefix(receiverStr, "0x")
	receiverAddrBytes, err := hex.DecodeString(receiverStr)
	if err != nil || len(receiverAddrBytes) != 32 {
		color.Red("❌ Error: Invalid receiver address format.")
		return
	}

	// 3. Input amount
	fmt.Print("👉 Enter amount of GO to transfer: ")
	if !scanner.Scan() { return }
	amountGO, err := strconv.ParseFloat(strings.TrimSpace(scanner.Text()), 64)
	if err != nil || amountGO <= 0 {
		color.Red("❌ Error: Invalid amount value.")
		return
	}

	vntAmount := uint64(amountGO * 100_000_000.0)
	fee := uint64(1000) // Default fee: 1000 nanoGO

	if vntAmount+fee > accResp.Balance {
		color.Red("❌ Error: Insufficient balance. Available: %.8f GO (Fee: %.8f GO)", float64(accResp.Balance)/100_000_000.0, float64(fee)/100_000_000.0)
		return
	}

	// 4. Input PIN/Password
	fmt.Print("👉 Enter wallet PIN/Password: ")
	if !scanner.Scan() { return }
	password := strings.TrimSpace(scanner.Text())

	seed, err := wm.GetSeed(senderWallet.Address, password)
	if err != nil {
		color.Red("❌ Error: Invalid password or wallet decryption failed.")
		return
	}

	// Get latest block hash
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	statusResp, err := client.GetStatus(ctx, &pb_block.GetStatusRequest{})
	cancel()
	if err != nil {
		color.Red("❌ Error: Could not fetch network status: %v", err)
		return
	}

	blockHeight := statusResp.CurrentHeight
	if blockHeight > 0 {
		blockHeight--
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	blockResp, err2 := client.GetBlock(ctx, &pb_block.GetBlockRequest{Height: blockHeight})
	cancel()

	var recentHash []byte
	if err2 == nil && blockResp.Found {
		headerBytes, _ := proto.Marshal(blockResp.Block.Header)
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		hashResp, err := client.CalculateBlockHeaderHash(ctx, &pb_block.RawBytes{Data: headerBytes})
		cancel()
		if err == nil {
			recentHash = hashResp.Value
		} else {
			recentHash = make([]byte, 32)
		}
	} else {
		recentHash = make([]byte, 32)
	}

	// 5. Confirm transaction
	fmt.Println("\n-------------------------------------------")
	fmt.Printf("Sender  : %s (0x%s...)\n", senderWallet.Name, senderWallet.Address[:16])
	fmt.Printf("Receiver: 0x%s\n", receiverStr)
	fmt.Printf("Amount  : %.8f GO\n", amountGO)
	fmt.Printf("Fee     : %.8f GO\n", float64(fee)/100_000_000.0)
	fmt.Printf("Nonce   : %d\n", accResp.Nonce)
	fmt.Println("-------------------------------------------")
	fmt.Print("👉 Confirm transaction? [y/N]: ")
	if !scanner.Scan() { return }
	confirm := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if confirm != "y" && confirm != "yes" {
		color.Yellow("🛑 Transaction cancelled.")
		return
	}

	// 6. Sign and broadcast transaction
	tx := &pb_block.Transaction{
		Version:         1,
		Sender:          &pb_block.Address{Value: senderAddrBytes},
		Receiver:        &pb_block.Address{Value: receiverAddrBytes},
		Amount:          vntAmount,
		Fee:             fee,
		Nonce:           accResp.Nonce,
		Timestamp:       uint64(time.Now().Unix()),
		RecentBlockHash: recentHash,
		ChainId:         25062025, // YonaCode Mainnet
	}

	privKey := ed25519.NewKeyFromSeed(seed[:32])
	signingHash := node_p2p.GetSigningHash(tx)
	signature := ed25519.Sign(privKey, signingHash)
	tx.Signature = &pb_block.Signature{Value: signature}

	color.Yellow("📡 Broadcasting transaction to P2P network...")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	submitResp, err := client.SubmitTransaction(ctx, tx)
	cancel()

	if err != nil {
		color.Red("❌ Transaction failed: %v", err)
	} else {
		color.Green("✅ Transaction submitted successfully!")
		color.Cyan("🔗 TxHash: 0x%s", hex.EncodeToString(submitResp.Value))
	}
	fmt.Println()
}
