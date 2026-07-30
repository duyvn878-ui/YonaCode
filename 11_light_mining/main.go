package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
)

const BANNER = `
 ===============================================================
   Y O N A C O D E   L I G H T   M I N I N G   ( v 1 . 0 )
 ---------------------------------------------------------------
  Lightweight Remote Proxy Mining • 100% CUDA GPU • Zero Sync
 ===============================================================
`

type CLITranslation struct {
	Title        string
	WalletPrompt string
	OptionAuto   string
	OptionManual string
	SelectChoice string
	WalletNotice string
	MnemonicMsg  string
	EnterWallet  string
	ErrEmpty     string
	NodeTarget   string
	RewardWallet string
	DeviceType   string
	StartMining  string
	StopNotice   string
	Stopping     string
	Goodbye      string
}

var cliTranslations = map[string]CLITranslation{
	"vn": {
		Title:        "💻 KÍCH HOẠT GIAO DIỆN DÒNG LỆNH CLI TERMINAL TRÊN LINUX/WINDOWS",
		WalletPrompt: "👉 Chưa nhập địa chỉ ví. Bạn muốn làm gì?",
		OptionAuto:   "   [1] Tự động tạo ví BIP39 mới",
		OptionManual: "   [2] Nhập địa chỉ ví 0x... thủ công",
		SelectChoice: "👉 Lựa chọn của bạn (1/2): ",
		WalletNotice: "🎉 Đã tự động tạo địa chỉ ví mới: %s",
		MnemonicMsg:  "🔑 Mnemonic 12 từ khôi phục: %s\n",
		EnterWallet:  "👉 Nhập địa chỉ ví $YGO của bạn: ",
		ErrEmpty:     "❌ Địa chỉ ví không được để trống!",
		NodeTarget:   "   Node Target: %s",
		RewardWallet: "   Ví nhận thưởng: %s",
		DeviceType:   "   Thiết bị đào: GPU CUDA 100%",
		StartMining:  "🚀 Đã khởi chạy tiến trình băm GPU CUDA Solo Mining!",
		StopNotice:   "   Nhấn Ctrl+C để dừng thợ đào an toàn.\n",
		Stopping:     "\n🛑 Đang phát lệnh dừng GPU CUDA Miner...",
		Goodbye:      "✅ Đã dừng hoàn toàn. Tạm biệt!",
	},
	"en": {
		Title:        "💻 ACTIVATING CLI TERMINAL INTERFACE ON LINUX/WINDOWS",
		WalletPrompt: "👉 No wallet address provided. Please choose an option:",
		OptionAuto:   "   [1] Auto-generate new BIP39 seed wallet",
		OptionManual: "   [2] Manually enter 0x... wallet address",
		SelectChoice: "👉 Enter your choice (1/2): ",
		WalletNotice: "🎉 Successfully generated new wallet: %s",
		MnemonicMsg:  "🔑 12-Word Recovery Mnemonic: %s\n",
		EnterWallet:  "👉 Enter your $YGO wallet address: ",
		ErrEmpty:     "❌ Wallet address cannot be empty!",
		NodeTarget:   "   Target Node: %s",
		RewardWallet: "   Reward Wallet: %s",
		DeviceType:   "   Mining Hardware: 100% CUDA GPU",
		StartMining:  "🚀 GPU CUDA Solo Mining Engine Started Successfully!",
		StopNotice:   "   Press Ctrl+C to safely stop miner.\n",
		Stopping:     "\n🛑 Issuing stop signal to GPU CUDA Miner...",
		Goodbye:      "✅ Stopped completely. Goodbye!",
	},
	"zh": {
		Title:        "💻 在 LINUX/WINDOWS 上启动 CLI 命令行界面",
		WalletPrompt: "👉 未提供钱包地址。请选择操作：",
		OptionAuto:   "   [1] 自动生成新的 BIP39 助记词钱包",
		OptionManual: "   [2] 手动输入 0x... 钱包地址",
		SelectChoice: "👉 请输入您的选择 (1/2): ",
		WalletNotice: "🎉 成功生成新钱包地址: %s",
		MnemonicMsg:  "🔑 12 个助记词恢复短语: %s\n",
		EnterWallet:  "👉 请输入您的 $YGO 钱包地址: ",
		ErrEmpty:     "❌ 钱包地址不能为空！",
		NodeTarget:   "   目标节点: %s",
		RewardWallet: "   奖励钱包: %s",
		DeviceType:   "   挖矿设备: 100% CUDA GPU",
		StartMining:  "🚀 GPU CUDA 独挖引擎已成功启动！",
		StopNotice:   "   按 Ctrl+C 安全停止矿机。\n",
		Stopping:     "\n🛑 正在向 GPU CUDA 矿机发送停止信号...",
		Goodbye:      "✅ 已完全停止。再见！",
	},
}

func cleanCLIInput(input string) string {
	input = strings.ReplaceAll(input, "\r", "")
	input = strings.ReplaceAll(input, "\n", "")
	input = strings.ReplaceAll(input, "\b", "")
	input = strings.ReplaceAll(input, "\t", "")
	return strings.TrimSpace(input)
}

func sanitizeWalletAddress(wallet string) string {
	wallet = cleanCLIInput(wallet)
	return strings.ReplaceAll(wallet, " ", "")
}

func main() {
	nodeAddr := flag.String("node", "110.172.28.103:9090", "Remote Production VPS Node RPC Address")
	wallet := flag.String("wallet", "", "Reward Wallet Address ($YGO)")
	device := flag.String("device", "gpu", "Mining Device (gpu)")
	lang := flag.String("lang", "vn", "Language choice: vn (Vietnamese), en (English), zh (Chinese)")
	forceCLI := flag.Bool("cli", false, "Force Command Line Interface (CLI) Mode")
	port := flag.Int("port", 28888, "GUI Web Server Port")
	noBrowser := flag.Bool("no-browser", false, "Do not auto-open web browser")
	flag.Parse()

	selectedLang := strings.ToLower(cleanCLIInput(*lang))
	t, exists := cliTranslations[selectedLang]
	if !exists {
		t = cliTranslations["en"]
	}

	engine := NewMinerEngine()

	execName := strings.ToLower(filepath.Base(os.Args[0]))
	isCLIBinary := strings.Contains(execName, "cli")

	// Disable standard logger timestamp prefixing to prevent terminal prompt splicing
	log.SetFlags(0)
	log.SetOutput(io.Discard)

	_ = GetGlobalProxyServer(engine, *port)

	// If binary is CLI, or --cli flag is set, or wallet flag specified, run 100% CLI Console Mode
	if *forceCLI || *wallet != "" || isCLIBinary {
		reader := bufio.NewReader(os.Stdin)

		// Proactive interactive language selection prompt on CLI startup
		color.Cyan("\n🌐 CHỌN NGÔN NGỮ / SELECT LANGUAGE / 选择语言:")
		color.Yellow("   [1] 🇻🇳 Tiếng Việt (Default)")
		color.Yellow("   [2] 🇺🇸 English")
		color.Yellow("   [3] 🇨🇳 中文")
		fmt.Print("👉 Lựa chọn của bạn / Your choice (1/2/3): ")

		inputLang, _ := reader.ReadString('\n')
		inputLang = cleanCLIInput(inputLang)

		switch inputLang {
		case "2", "en", "EN", "english":
			selectedLang = "en"
		case "3", "zh", "ZH", "chinese":
			selectedLang = "zh"
		default:
			selectedLang = "vn"
		}
		t = cliTranslations[selectedLang]

		color.Green("\n" + t.Title)
		engine.RunGPUCheck()
		
		targetWallet := sanitizeWalletAddress(*wallet)
		if targetWallet == "" {
			color.Yellow(t.WalletPrompt)
			color.Yellow(t.OptionAuto)
			color.Yellow(t.OptionManual)
			fmt.Print(t.SelectChoice)
			
			input, _ := reader.ReadString('\n')
			input = cleanCLIInput(input)

			if input == "1" || input == "" {
				mnemonic, addr, err := GenerateNewBIP39Wallet()
				if err != nil {
					color.Red("❌ Error creating BIP39 wallet: %v", err)
					os.Exit(1)
				}
				targetWallet = sanitizeWalletAddress(addr)
				color.Green("\n"+t.WalletNotice, targetWallet)
				color.Cyan(t.MnemonicMsg, mnemonic)
			} else {
				fmt.Print(t.EnterWallet)
				inputAddr, _ := reader.ReadString('\n')
				targetWallet = sanitizeWalletAddress(inputAddr)
				if targetWallet == "" {
					color.Red(t.ErrEmpty)
					os.Exit(1)
				}
			}
		}

		color.Yellow("---------------------------------------------------------------")
		color.Green("📋 CLI CONFIGURATION SUMMARY & INTERACTIVE CONTROLS")
		color.Yellow(t.NodeTarget, *nodeAddr)
		color.Yellow(t.RewardWallet, targetWallet)
		color.Yellow(t.DeviceType)
		color.Cyan("---------------------------------------------------------------")
		color.White("  [1/b] 🚀 Start GPU Mining  |  [2/x] 🛑 Stop Mining")
		color.White("  [3/w] 🔑 Gen New BIP39 Wallet  |  [4/s] 📊 Print Status")
		color.White("  [5/l] 🌐 Change Language (VN/EN/ZH)  |  [q/Ctrl+C] 🚪 Exit Application")
		color.Cyan("---------------------------------------------------------------\n")
		// Mining engine is kept OFF by default on startup. User must press [1] or [b] to activate.
		engine.SetCPUIntensity(100)

		// Stream live engine logs directly to CLI terminal console
		go func() {
			var lastLogCount int
			for {
				time.Sleep(500 * time.Millisecond)
				st := engine.GetStatus()
				logs, ok := st["logs"].([]LogEntry)
				if ok && len(logs) > lastLogCount {
					for i := lastLogCount; i < len(logs); i++ {
						entry := logs[i]
						switch entry.Type {
						case "success":
							color.Green("%s %s", entry.Time, entry.Text)
						case "error", "warn":
							color.Red("%s %s", entry.Time, entry.Text)
						default:
							color.Cyan("%s %s", entry.Time, entry.Text)
						}
					}
					lastLogCount = len(logs)
				}
			}
		}()

		// Handle keyboard input in background
		go func() {
			for {
				cmdStr, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				cmdStr = strings.ToLower(strings.TrimSpace(cmdStr))
				switch cmdStr {
				case "1", "b", "start":
					_ = engine.Start(*nodeAddr, targetWallet, *device, runtime.NumCPU())
					color.Green("🚀 GPU CUDA Mining Engine Started!")
				case "2", "x", "stop":
					engine.Stop()
					color.Yellow("🛑 GPU CUDA Mining Engine Stopped.")
				case "3", "w", "wallet":
					mn, ad, err := GenerateNewBIP39Wallet()
					if err == nil {
						color.Green("🔑 New BIP39 Wallet Address: %s", ad)
						color.Cyan("   Mnemonic: %s", mn)
					}
				case "4", "s", "status":
					st := engine.GetStatus()
					color.Cyan("📊 STATUS: Connected: %v | Height: #%v | Hashes: %v H/s | Blocks Found: %v",
						st["is_connected"], st["network_height"], st["hashrate"], st["blocks_found"])
				case "5", "l", "lang", "language":
					color.Cyan("🌐 SELECT LANGUAGE / CHỌN NGÔN NGỮ: [1] VN | [2] EN | [3] ZH: ")
					inL, _ := reader.ReadString('\n')
					inL = strings.TrimSpace(inL)
					switch inL {
					case "2", "en":
						selectedLang = "en"
					case "3", "zh":
						selectedLang = "zh"
					default:
						selectedLang = "vn"
					}
					t = cliTranslations[selectedLang]
					color.Green("✅ Language changed to: %s", strings.ToUpper(selectedLang))
				case "q", "exit", "quit":
					color.Yellow("🛑 Shutting down CLI Console...")
					engine.Stop()
					color.Green(t.Goodbye)
					os.Exit(0)
				}
			}
		}()

		// Handle OS signals (Ctrl+C)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-sigChan:
				color.Yellow("\n" + t.Stopping)
				engine.Stop()
				color.Green(t.Goodbye)
				os.Exit(0)
			case <-ticker.C:
				st := engine.GetStatus()
				isMining := st["is_mining"].(bool)
				isConnected := st["is_connected"].(bool)
				hr := st["hashrate"].(uint64)
				found := st["blocks_found"].(uint64)
				height := st["network_height"].(uint64)
				earned := st["wallet_balance"].(float64)

				nodeStatusStr := fmt.Sprintf("🟢 Live (#%d)", height)
				if !isConnected {
					nodeStatusStr = "🔴 Disconnected"
				}

				if isMining {
					hrStr := fmt.Sprintf("%d H/s", hr)
					if hr >= 1000000000 {
						hrStr = fmt.Sprintf("%.2f GH/s", float64(hr)/1000000000.0)
					} else if hr >= 1000000 {
						hrStr = fmt.Sprintf("%.2f MH/s", float64(hr)/1000000.0)
					} else if hr >= 1000 {
						hrStr = fmt.Sprintf("%.2f KH/s", float64(hr)/1000.0)
					}

					color.Cyan("[CLI-CONSOLE] ⚡ Speed: %s | VPS Node: %s | Blocks: %d | Earned: %.4f $YGO",
						hrStr, nodeStatusStr, found, earned)
				} else {
					color.Yellow("[CLI-CONSOLE] ⏸️ Engine Paused | VPS Node: %s | Enter [1] to start mining", nodeStatusStr)
				}
			}
		}
	} else {
		// Default: Launch Web UI Dashboard & auto-open web browser
		color.Green("🌐 LAUNCHING DEDICATED WEB DASHBOARD SERVER")
		webURL := fmt.Sprintf("http://127.0.0.1:%d", *port)
		color.Yellow("   Web Interface URL: %s\n", webURL)

		_ = GetGlobalProxyServer(engine, *port)

		if !*noBrowser {
			go func() {
				time.Sleep(500 * time.Millisecond)
				OpenBrowser(webURL)
			}()
		}

		color.Cyan("---------------------------------------------------------------")
		color.Green("💡 TERMINAL CONSOLE LIVE MONITORING ACTIVE")
		color.Yellow("   Press Ctrl+C in this console or click [TẮT & THOÁT] on Web UI to exit.")
		color.Cyan("---------------------------------------------------------------\n")

		// Handle graceful shutdown via Ctrl+C
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		lastLogIndex := 0

		for {
			select {
			case <-sigChan:
				color.Yellow("\n🛑 Shutting down YonaCode Light Mining Server...")
				engine.Stop()
				color.Green("✅ Server stopped. Goodbye!")
				os.Exit(0)

			case <-ticker.C:
				st := engine.GetStatus()
				isMining := st["is_mining"].(bool)
				isConnected := st["is_connected"].(bool)
				height := st["network_height"].(uint64)
				hr := st["hashrate"].(uint64)
				found := st["blocks_found"].(uint64)

				// 1. Print connection state line
				if isConnected {
					color.Green("[NODE-STATUS] 🟢 VPS Node Live (110.172.28.103:9090) | Network Height: #%d", height)
				} else {
					color.Red("[NODE-STATUS] 🔴 VPS Node Disconnected (110.172.28.103:9090) | Retrying...")
				}

				// 2. Print mining state line if mining
				if isMining {
					hrStr := fmt.Sprintf("%d H/s", hr)
					if hr >= 1000000000 {
						hrStr = fmt.Sprintf("%.2f GH/s", float64(hr)/1000000000.0)
					} else if hr >= 1000000 {
						hrStr = fmt.Sprintf("%.2f MH/s", float64(hr)/1000000.0)
					} else if hr >= 1000 {
						hrStr = fmt.Sprintf("%.2f KH/s", float64(hr)/1000.0)
					}
					color.Cyan("[GPU-ENGINE] ⚡ Mining Speed: %s | Blocks Found: %d", hrStr, found)
				}

				// 3. Print new log entries to console
				logsRaw, ok := st["logs"].([]LogEntry)
				if ok && len(logsRaw) > lastLogIndex {
					for i := lastLogIndex; i < len(logsRaw); i++ {
						entry := logsRaw[i]
						if entry.Type == "success" {
							color.Green("  LOG %s %s", entry.Time, entry.Text)
						} else if entry.Type == "warn" {
							color.Yellow("  LOG %s %s", entry.Time, entry.Text)
						} else {
							color.White("  LOG %s %s", entry.Time, entry.Text)
						}
					}
					lastLogIndex = len(logsRaw)
				}
			}
		}
	}
}
