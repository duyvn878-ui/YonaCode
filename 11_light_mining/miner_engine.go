package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/ed25519"
	"github.com/fatih/color"
)

type LogEntry struct {
	Time string `json:"time"`
	Text string `json:"text"`
	Type string `json:"type"` // "info", "success", "warn"
}

type MinedBlockEntry struct {
	Height    uint64  `json:"height"`
	Reward    float64 `json:"reward"`
	Hash      string  `json:"hash"`
	Timestamp string  `json:"timestamp"`
	Status    string  `json:"status"`
}

type MinerEngine struct {
	cpuIntensity        int
	cpuThreads          int
	mu                  sync.Mutex
	isMining            bool
	nodeAddr            string
	wallet              string
	cmd                 *exec.Cmd
	startTime           time.Time
	hashrate            uint64
	networkHashrate     uint64
	networkHeight       uint64
	blocksFound         uint64
	orphanedBlocks      uint64
	walletBalance       float64
	goEarned            float64
	logs                []LogEntry
	minedBlocksHistory  []MinedBlockEntry
	httpClient          *http.Client
	wsConn              *websocket.Conn
	wsActive            bool
	lastWorkBytes       []byte
	lastWorkMu          sync.RWMutex
	wsStopChan          chan struct{}
	sessionBlocks       map[uint64]bool
}

type MinerConfig struct {
	Wallet       string `json:"wallet"`
	NodeAddr     string `json:"node_addr"`
	CPUThreads   int    `json:"cpu_threads"`
	CPUIntensity int    `json:"cpu_intensity"`
}

func getConfigPath() string {
	if execPath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(execPath), "miner_config.json")
	}
	return "miner_config.json"
}

func (m *MinerEngine) LoadConfig() {
	cfgPath := getConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		m.cpuThreads = runtime.NumCPU()
		m.cpuIntensity = 100
		return
	}
	var cfg MinerConfig
	if json.Unmarshal(data, &cfg) == nil {
		m.mu.Lock()
		if cfg.Wallet != "" {
			m.wallet = cfg.Wallet
		}
		if cfg.NodeAddr != "" {
			m.nodeAddr = cfg.NodeAddr
		}
		m.cpuThreads = runtime.NumCPU()
		m.cpuIntensity = 100
		m.mu.Unlock()
	}
}

func (m *MinerEngine) SaveConfig() {
	m.mu.Lock()
	cfg := MinerConfig{
		Wallet:       m.wallet,
		NodeAddr:     m.nodeAddr,
		CPUThreads:   m.cpuThreads,
		CPUIntensity: m.cpuIntensity,
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		_ = os.WriteFile(getConfigPath(), data, 0644)
	}
}

func (m *MinerEngine) SetWallet(wallet string) {
	cleanW := strings.ReplaceAll(strings.TrimSpace(wallet), " ", "")
	cleanW = strings.ReplaceAll(cleanW, "\r", "")
	cleanW = strings.ReplaceAll(cleanW, "\n", "")
	cleanW = strings.ReplaceAll(cleanW, "\t", "")
	m.mu.Lock()
	m.wallet = cleanW
	m.mu.Unlock()
	m.SaveConfig()
}

func NewMinerEngine() *MinerEngine {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	engine := &MinerEngine{
		httpClient:         &http.Client{Transport: tr, Timeout: 4 * time.Second},
		logs:               make([]LogEntry, 0, 100),
		minedBlocksHistory: make([]MinedBlockEntry, 0, 50),
		cpuIntensity:       100,
		cpuThreads:         runtime.NumCPU(),
	}
	engine.LoadConfig()
	engine.cpuIntensity = 100
	engine.cpuThreads = runtime.NumCPU()
	// Initial poll loop for live VPS Node data
	go engine.pollNodeStatsLoop()
	return engine
}

func (m *MinerEngine) addLog(text string, logType string) {
	now := time.Now().Format("15:04:05")
	entry := LogEntry{
		Time: fmt.Sprintf("[%s]", now),
		Text: text,
		Type: logType,
	}
	m.mu.Lock()
	m.logs = append(m.logs, entry)
	if len(m.logs) > 100 {
		m.logs = m.logs[len(m.logs)-100:]
	}
	m.mu.Unlock()

	// Print directly to console stdout for instant CLI visibility
	fmt.Printf("%s %s\n", entry.Time, entry.Text)
}

func (m *MinerEngine) findMinerBinary() string {
	candidates := []string{
		"yona_gpu_miner.exe",
		"../yona_gpu_miner.exe",
		"bin/yona_gpu_miner.exe",
		"genz_miner.exe",
		"../genz_miner.exe",
		"bin/genz_miner.exe",
		"yona_gpu_miner",
		"../yona_gpu_miner",
		"bin/yona_gpu_miner",
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append([]string{
			filepath.Join(execDir, "yona_gpu_miner.exe"),
			filepath.Join(execDir, "..", "yona_gpu_miner.exe"),
			filepath.Join(execDir, "genz_miner.exe"),
			filepath.Join(execDir, "..", "genz_miner.exe"),
			filepath.Join(execDir, "yona_gpu_miner"),
			filepath.Join(execDir, "..", "yona_gpu_miner"),
		}, candidates...)
	}

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			abs, err := filepath.Abs(p)
			if err == nil {
				return abs
			}
			return p
		}
	}
	return ""
}

func (m *MinerEngine) RunGPUCheck() {
	setupPath := m.findSetupBinary()
	if setupPath != "" {
		setupCmd := exec.Command(setupPath)
		setupCmd.Stdin = strings.NewReader("\n")
		out, _ := setupCmd.CombinedOutput()
		if len(out) > 0 {
			lines := strings.Split(string(out), "\n")
			for _, l := range lines {
				lStr := strings.TrimSpace(l)
				if lStr != "" {
					if strings.Contains(lStr, "THÀNH CÔNG") || strings.Contains(lStr, "SUCCESS") {
						color.Green("[SETUP] %s", lStr)
					} else if strings.Contains(lStr, "LƯU Ý") || strings.Contains(lStr, "WARNING") || strings.Contains(lStr, "⚠️") {
						color.Yellow("[SETUP] %s", lStr)
					} else {
						color.Cyan("[SETUP] %s", lStr)
					}
				}
			}
		}
	}
}

func (m *MinerEngine) findSetupBinary() string {
	candidates := []string{
		"yona_gpu_setup.exe",
		"../yona_gpu_setup.exe",
		"bin/yona_gpu_setup.exe",
		"yona_gpu_setup",
		"../yona_gpu_setup",
		"bin/yona_gpu_setup",
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append([]string{
			filepath.Join(execDir, "yona_gpu_setup.exe"),
			filepath.Join(execDir, "..", "yona_gpu_setup.exe"),
			filepath.Join(execDir, "yona_gpu_setup"),
			filepath.Join(execDir, "..", "yona_gpu_setup"),
		}, candidates...)
	}

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
	}
	return ""
}

func (m *MinerEngine) Start(nodeAddr, wallet, device string, threads int) error {
	m.mu.Lock()
	if m.isMining {
		m.mu.Unlock()
		return nil
	}

	if wallet == "" {
		_, autoAddr, err := GenerateNewBIP39Wallet()
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to auto-generate wallet: %v", err)
		}
		wallet = autoAddr
	}

	if nodeAddr == "" {
		nodeAddr = "110.172.28.103:9090"
	}

	m.nodeAddr = nodeAddr
	m.wallet = wallet
	m.isMining = true
	m.startTime = time.Now()
	m.hashrate = 0
	m.sessionBlocks = make(map[uint64]bool)
	m.mu.Unlock()

	m.addLog(fmt.Sprintf("🚀 Kích hoạt động cơ đào GPU CUDA | Ví nhận thưởng: %s", wallet), "info")

	// Khởi tạo WebSocket Tunnel để đẩy block template từ VPS về
	m.startWebSocketTunnel()

	// Launch setup check and process asynchronously to prevent blocking status polls
	go m.launchMinerAsync(nodeAddr, wallet, device, threads)
	return nil
}

func (m *MinerEngine) launchMinerAsync(nodeAddr, wallet, device string, threads int) {
	// First clean up any old process locks
	m.killExistingProcesses()

	setupPath := m.findSetupBinary()
	if setupPath != "" {
		m.addLog(fmt.Sprintf("🔍 Thực thi công cụ kiểm tra môi trường GPU tự động: %s", setupPath), "info")
		setupCmd := exec.Command(setupPath)
		setupCmd.Stdin = strings.NewReader("\n")
		out, err := setupCmd.CombinedOutput()
		if err != nil {
			m.addLog(fmt.Sprintf("⚠️ Kiểm tra môi trường GPU hoàn tất (%v)", err), "info")
		} else {
			m.addLog("✅ Tự động kiểm tra phần cứng & Driver GPU CUDA thành công 100%", "success")
		}
		if len(out) > 0 {
			lines := strings.Split(string(out), "\n")
			for _, l := range lines {
				lStr := strings.TrimSpace(l)
				if lStr != "" {
					m.addLog(fmt.Sprintf("[SETUP] %s", lStr), "info")
				}
			}
		}
	}

	m.mu.Lock()
	if !m.isMining {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	binaryPath := m.findMinerBinary()
	if binaryPath != "" {
		m.addLog(fmt.Sprintf("📂 Tìm thấy tệp thực thi GPU Miner: %s", binaryPath), "info")
		
		targetIP := "127.0.0.1"
		targetPort := "28888"

		var cmd *exec.Cmd
		if strings.Contains(binaryPath, "genz_miner") {
			cmd = exec.Command(binaryPath, "--node", "127.0.0.1:28888", "--wallet", wallet, "--device", "gpu")
		} else {
			cmd = exec.Command(binaryPath, targetIP, targetPort, wallet)
		}

		stdout, err := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()

		if err == nil && cmd.Start() == nil {
			m.mu.Lock()
			m.cmd = cmd
			m.mu.Unlock()

			m.addLog(fmt.Sprintf("⚡ Đã khởi chạy GPU CUDA Miner (PID: %d)", cmd.Process.Pid), "success")

			go m.readMinerOutput(stdout)
			if stderr != nil {
				go m.readMinerOutput(stderr)
			}
		} else {
			m.addLog(fmt.Sprintf("⚠️ Lỗi khi khởi chạy %s: %v", binaryPath, err), "warn")
		}
	} else {
		m.addLog("⚠️ Không tìm thấy yona_gpu_miner trên ổ đĩa. Lắng nghe HTTP Proxy", "warn")
	}
}

func (m *MinerEngine) killExistingProcesses() {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/IM", "genz_miner.exe").Run()
		_ = exec.Command("taskkill", "/F", "/IM", "yona_gpu_miner.exe").Run()
	} else {
		_ = exec.Command("killall", "-9", "genz_miner").Run()
		_ = exec.Command("killall", "-9", "yona_gpu_miner").Run()
	}
}

func (m *MinerEngine) Stop() {
	m.mu.Lock()
	if !m.isMining {
		m.mu.Unlock()
		return
	}
	m.isMining = false
	m.hashrate = 0
	cmdToKill := m.cmd
	m.cmd = nil

	if m.wsStopChan != nil {
		close(m.wsStopChan)
		m.wsStopChan = nil
	}
	if m.wsConn != nil {
		m.wsConn.Close()
		m.wsConn = nil
	}
	m.wsActive = false

	m.mu.Unlock()

	m.addLog("🛑 Đã dừng động cơ đào GPU CUDA", "warn")

	go func() {
		if cmdToKill != nil && cmdToKill.Process != nil {
			_ = cmdToKill.Process.Kill()
		}
		m.killExistingProcesses()
	}()
}

func (m *MinerEngine) readMinerOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	hashRegex := regexp.MustCompile(`([\d\.]+)\s*(MH/s|GH/s|KH/s|H/s)`)
	blockRegex := regexp.MustCompile(`(?i)Block\s*#?(\d+)\s*(Found|Accepted)`)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		logType := "info"
		if regexp.MustCompile(`(?i)(accept|found|block|share|initialized)`).MatchString(line) {
			logType = "success"
			if matches := blockRegex.FindStringSubmatch(line); len(matches) >= 2 {
				heightVal, _ := strconv.ParseUint(matches[1], 10, 64)
				m.RecordMinedBlock(heightVal, 0)
			}
		} else if regexp.MustCompile(`(?i)(error|fail|reject|warn)`).MatchString(line) {
			logType = "warn"
		}

		m.addLog(line, logType)

		// Parse real GPU Hashes/sec output
		matches := hashRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			val, _ := strconv.ParseFloat(matches[1], 64)
			unit := matches[2]
			var hr uint64
			switch unit {
			case "TH/s":
				hr = uint64(val * 1e12)
			case "GH/s":
				hr = uint64(val * 1e9)
			case "MH/s":
				hr = uint64(val * 1e6)
			case "KH/s":
				hr = uint64(val * 1e3)
			default:
				hr = uint64(val)
			}
			m.mu.Lock()
			m.hashrate = hr
			m.mu.Unlock()
		}
	}
}

func (m *MinerEngine) RecordMinedBlock(height uint64, reward float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocksFound++
	if m.sessionBlocks == nil {
		m.sessionBlocks = make(map[uint64]bool)
	}
	m.sessionBlocks[height] = true

	if reward == 0 {
		reward = CalculateBlockReward(height)
	}
	if reward > 0 {
		m.walletBalance += reward
		m.goEarned += reward
	}
	newBlock := MinedBlockEntry{
		Height:    height,
		Reward:    reward,
		Hash:      fmt.Sprintf("0x%x", sha256.Sum256([]byte(fmt.Sprintf("%d-%d", height, time.Now().UnixNano()))))[:16],
		Timestamp: time.Now().Format("15:04:05"),
		Status:    "Confirmed",
	}
	m.minedBlocksHistory = append([]MinedBlockEntry{newBlock}, m.minedBlocksHistory...)
	if len(m.minedBlocksHistory) > 50 {
		m.minedBlocksHistory = m.minedBlocksHistory[:50]
	}
}

func CalculateBlockReward(height uint64) float64 {
	if height <= 99 {
		return 10000.0
	}
	if height <= 420579 {
		if height < 17000 {
			return 0.475647
		}
		if height == 17000 {
			return float64(121899404+159680) / 1e8
		}
		return 1.21899404
	}
	if height <= 841059 {
		return 3.864631
	}
	if height <= 1261539 {
		return 5.053748
	}
	if height <= 1682019 {
		return 6.242865
	}
	if height <= 2102499 {
		return 7.431982
	}
	if height <= 2522979 {
		start_reward := uint64(743198200)
		end_reward := uint64(347222200)
		start_height := uint64(2102500)
		end_height := uint64(2522979)

		h_delta := height - start_height
		h_total := end_height - start_height
		r_delta := start_reward - end_reward

		reduction := (r_delta * h_delta) / h_total
		return float64(start_reward-reduction) / 1e8
	}
	if height <= 2943459 {
		start_reward := uint64(347222200)
		end_reward := uint64(9513000)
		start_height := uint64(2522980)
		end_height := uint64(2943459)

		h_delta := height - start_height
		h_total := end_height - start_height
		r_delta := start_reward - end_reward

		reduction := (r_delta * h_delta) / h_total
		return float64(start_reward-reduction) / 1e8
	}
	if height <= 105694901 {
		if height == 105694901 {
			return 1434078.0 / 1e8
		}
		start_reward := uint64(9513000)
		end_reward := uint64(0)
		start_height := uint64(2943460)
		end_height := uint64(134500269)

		h_delta := height - start_height
		h_total := end_height - start_height
		r_delta := start_reward - end_reward

		reduction := (r_delta * h_delta) / h_total
		return float64(start_reward-reduction) / 1e8
	}
	return 0.0
}

func (m *MinerEngine) pollNodeStatsLoop() {
	m.fetchRealNodeStatus()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.fetchRealNodeStatus()
	}
}

func (m *MinerEngine) fetchRealNodeStatus() {
	m.mu.Lock()
	customNode := m.nodeAddr
	m.mu.Unlock()

	nodeRPCs := []string{}
	if customNode != "" {
		if !strings.HasPrefix(customNode, "http://") && !strings.HasPrefix(customNode, "https://") {
			customNode = "http://" + customNode
		}
		if strings.Contains(customNode, ":9090") {
			customNode = strings.Replace(customNode, ":9090", ":28888", 1)
		}
		nodeRPCs = append(nodeRPCs, customNode)
	}

	nodeRPCs = append(nodeRPCs,
		"http://110.172.28.103:28888",
		"http://110.172.28.103:8080",
	)

	var activeURL string
	var statusData struct {
		CurrentHeight   uint64  `json:"current_height"`
		HighestHeight   uint64  `json:"highest_height"`
		NetworkHashrate uint64  `json:"network_hashrate"`
		Hashrate        uint64  `json:"hashrate"`
		TotalSupply     float64 `json:"total_supply"`
	}

	for _, url := range nodeRPCs {
		url = strings.TrimSuffix(url, "/")
		if strings.Contains(url, ":9090") {
			continue // Skip validator ports entirely for statistics
		}
		resp, err := m.httpClient.Get(url + "/api/v1/status")
		if err == nil {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				var rawData map[string]interface{}
				if json.Unmarshal(body, &rawData) == nil {
					if h, ok := rawData["current_height"].(float64); ok && uint64(h) > 0 {
						statusData.CurrentHeight = uint64(h)
					} else if h, ok := rawData["highest_height"].(float64); ok && uint64(h) > 0 {
						statusData.CurrentHeight = uint64(h)
					}

					var rawNetHash uint64
					if nh, ok := rawData["network_hashrate"].(float64); ok && uint64(nh) > 0 {
						rawNetHash = uint64(nh)
					} else if hr, ok := rawData["hashrate"].(float64); ok && uint64(hr) > 0 {
						rawNetHash = uint64(hr)
					}

					if rawNetHash == 0 {
						diff := float64(0)
						if d, ok := rawData["difficulty"].(float64); ok {
							diff = d
						} else if dS, ok := rawData["difficulty"].(string); ok {
							if dV, err := strconv.ParseFloat(dS, 64); err == nil {
								diff = dV
							}
						}
						if diff > 0 {
							avgBT := float64(70.0)
							if abt, ok := rawData["avg_block_time"].(float64); ok && abt > 0 {
								avgBT = abt
							}
							rawNetHash = uint64(diff / avgBT)
						}
					}

					if rawNetHash > 0 {
						statusData.NetworkHashrate = rawNetHash
					}

					if statusData.CurrentHeight > 0 && statusData.NetworkHashrate > 0 {
						activeURL = url
						break
					}
				}
			}
		} else {
			m.addLog(fmt.Sprintf("⚠️ Connection error for %s: %v", url, err), "warn")
		}
		
		// Second try /api/v1/miner/getwork
		respGw, errGw := m.httpClient.Get(url + "/api/v1/miner/getwork")
		if errGw == nil {
			bodyGw, errGw := io.ReadAll(respGw.Body)
			respGw.Body.Close()
			if errGw == nil {
				var gwData struct {
					Height          uint64 `json:"height"`
					NetworkHashrate uint64 `json:"network_hashrate"`
				}
				if json.Unmarshal(bodyGw, &gwData) == nil && gwData.Height > 0 {
					statusData.CurrentHeight = gwData.Height
					if gwData.NetworkHashrate > 0 {
						statusData.NetworkHashrate = gwData.NetworkHashrate
					}
					if statusData.NetworkHashrate > 0 {
						activeURL = url
						break
					}
				}
			}
		}
	}

	if activeURL != "" {
		m.mu.Lock()
		prevHeight := m.networkHeight
		if statusData.CurrentHeight > 0 {
			m.networkHeight = statusData.CurrentHeight
		} else if statusData.HighestHeight > 0 {
			m.networkHeight = statusData.HighestHeight
		}
		if statusData.NetworkHashrate > 0 {
			m.networkHashrate = statusData.NetworkHashrate
		} else {
			m.networkHashrate = 0
		}
		currentH := m.networkHeight
		m.mu.Unlock()

		if prevHeight == 0 && currentH > 0 {
			m.addLog(fmt.Sprintf("🟢 Kết nối VPS Node (%s) thành công! Khối mạng: #%d", activeURL, currentH), "success")
		}

		// Query REAL Wallet balance and block history if wallet specified
		m.mu.Lock()
		wallet := m.wallet
		m.mu.Unlock()

		if wallet != "" {
			// 1. Fetch balance
			balURL := fmt.Sprintf("%s/api/v1/balance/%s", activeURL, wallet)
			balResp, err := m.httpClient.Get(balURL)
			if err == nil {
				body, err := io.ReadAll(balResp.Body)
				balResp.Body.Close()
				if err == nil {
					var balData struct {
						Balances map[string]uint64 `json:"balances"`
					}
					if json.Unmarshal(body, &balData) == nil && balData.Balances != nil {
						totalVal := float64(balData.Balances["btc_z"]) / 1e8
						m.mu.Lock()
						m.walletBalance = totalVal
						m.goEarned = totalVal
						m.mu.Unlock()
					}
				}
			}

			// 2. Query address history to rebuild mined blocks list (coinbase transactions)
			var onChainMined []MinedBlockEntry
			walletClean := strings.ToLower(strings.TrimPrefix(wallet, "0x"))

			histURL := fmt.Sprintf("%s/api/v1/address/%s/history", activeURL, wallet)
			histResp, err := m.httpClient.Get(histURL)
			if err == nil {
				body, err := io.ReadAll(histResp.Body)
				histResp.Body.Close()
				if err == nil {
					var histData struct {
						History []struct {
							TxID        string `json:"txid"`
							ID          string `json:"id"`
							Sender      string `json:"sender"`
							Receiver    string `json:"receiver"`
							BlockHeight uint64 `json:"blockHeight"`
							Height      uint64 `json:"height"`
							Timestamp   uint64 `json:"timestamp"`
						} `json:"history"`
					}
					if json.Unmarshal(body, &histData) == nil && histData.History != nil {
						for _, tx := range histData.History {
							// Coinbase transaction: sender is empty or "0x"
							senderClean := strings.ToLower(strings.TrimPrefix(tx.Sender, "0x"))
							if senderClean == "" {
								h := tx.BlockHeight
								if h == 0 {
									h = tx.Height
								}
								if h > 0 {
									txHash := tx.TxID
									if txHash == "" {
										txHash = tx.ID
									}
									shortHash := txHash
									if len(shortHash) > 16 {
										shortHash = shortHash[:16]
									}
									if !strings.HasPrefix(shortHash, "0x") {
										shortHash = "0x" + shortHash
									}
									onChainMined = append(onChainMined, MinedBlockEntry{
										Height:    h,
										Reward:    CalculateBlockReward(h),
										Hash:      shortHash,
										Timestamp: time.Unix(int64(tx.Timestamp), 0).Format("15:04:05"),
										Status:    "Confirmed",
									})
								}
							}
						}
					}
				}
			}

			// 3. Query recent blocks to catch any blocks mined recently by this address
			recentURL := fmt.Sprintf("%s/api/v1/recent/blocks?limit=250", activeURL)
			recentResp, err := m.httpClient.Get(recentURL)
			if err == nil {
				body, err := io.ReadAll(recentResp.Body)
				recentResp.Body.Close()
				if err == nil {
					var recentData []struct {
						Height    uint64 `json:"height"`
						Hash      string `json:"hash"`
						Timestamp uint64 `json:"timestamp"`
						Miner     string `json:"miner"`
					}
					if json.Unmarshal(body, &recentData) == nil {
						for _, b := range recentData {
							minerClean := strings.ToLower(strings.TrimPrefix(b.Miner, "0x"))
							if minerClean == walletClean {
								shortHash := b.Hash
								if len(shortHash) > 16 {
									shortHash = shortHash[:16]
								}
								if !strings.HasPrefix(shortHash, "0x") {
									shortHash = "0x" + shortHash
								}
								onChainMined = append(onChainMined, MinedBlockEntry{
									Height:    b.Height,
									Reward:    CalculateBlockReward(b.Height),
									Hash:      shortHash,
									Timestamp: time.Unix(int64(b.Timestamp), 0).Format("15:04:05"),
									Status:    "Confirmed",
								})
							}
						}
					}
				}
			}

			// 4. Merge all on-chain blocks and local in-memory history
			m.mu.Lock()
			currentHeight := m.networkHeight
			merged := make(map[uint64]MinedBlockEntry)

			// Add on-chain ones (these are 100% Confirmed)
			for _, b := range onChainMined {
				merged[b.Height] = b
			}

			// Add in-memory ones found in current session
			for _, b := range m.minedBlocksHistory {
				if b.Reward == 0 {
					b.Reward = CalculateBlockReward(b.Height)
				}
				if _, exists := merged[b.Height]; !exists {
					// This block was found locally but is NOT on-chain (yet).
					// If the network height has advanced beyond this block's height (e.g. by 2 or more blocks),
					// it means the block was orphaned / rejected!
					if currentHeight > b.Height + 2 {
						b.Status = "Orphaned"
						b.Reward = 0.0
					} else {
						b.Status = "Confirming"
					}
					merged[b.Height] = b
				}
			}

			// Reconstruct sorted slice
			var finalHistory []MinedBlockEntry
			for _, b := range merged {
				finalHistory = append(finalHistory, b)
			}
			sort.Slice(finalHistory, func(i, j int) bool {
				return finalHistory[i].Height > finalHistory[j].Height
			})

			if len(finalHistory) > 50 {
				finalHistory = finalHistory[:50]
			}

			m.minedBlocksHistory = finalHistory

			// Count only confirmed and confirming blocks as mined
			confirmedCount := uint64(0)
			orphanedCount := uint64(0)
			for _, b := range merged {
				if b.Status == "Confirmed" || b.Status == "Confirming" {
					confirmedCount++
				} else if b.Status == "Orphaned" {
					orphanedCount++
				}
			}
			m.blocksFound = confirmedCount
			m.orphanedBlocks = orphanedCount

			// Calculate session coins (goEarned): sum of all blocks in finalHistory that were found in the current session and not orphaned
			sessionCoins := 0.0
			if m.sessionBlocks != nil {
				for _, b := range finalHistory {
					if m.sessionBlocks[b.Height] && b.Status != "Orphaned" {
						sessionCoins += b.Reward
					}
				}
			}
			m.goEarned = sessionCoins
			m.mu.Unlock()
		}
	}
}

func (m *MinerEngine) GetStatus() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	var uptimeStr string
	if m.isMining {
		dur := time.Since(m.startTime)
		days := int(dur.Hours()) / 24
		hours := int(dur.Hours()) % 24
		mins := int(dur.Minutes()) % 60
		secs := int(dur.Seconds()) % 60
		uptimeStr = fmt.Sprintf("%02dd %02dh %02dm %02ds", days, hours, mins, secs)
	} else {
		uptimeStr = "00d 00h 00m 00s"
	}

	logsCopy := make([]LogEntry, len(m.logs))
	copy(logsCopy, m.logs)

	blocksHistoryCopy := make([]MinedBlockEntry, len(m.minedBlocksHistory))
	copy(blocksHistoryCopy, m.minedBlocksHistory)

	return map[string]interface{}{
		"is_mining":            m.isMining,
		"is_connected":         m.networkHeight > 0,
		"wallet":               m.wallet,
		"hashrate":             m.hashrate,
		"network_hashrate":     m.networkHashrate,
		"network_height":       m.networkHeight,
		"blocks_found":         m.blocksFound,
		"orphaned_blocks":      m.orphanedBlocks,
		"wallet_balance":       m.walletBalance,
		"go_earned":            m.goEarned,
		"uptime":               uptimeStr,
		"cpu_cores":            runtime.NumCPU(),
		"cpu_threads":          m.cpuThreads,
		"cpu_intensity":        m.cpuIntensity,
		"logs":                 logsCopy,
		"mined_blocks_history": blocksHistoryCopy,
	}
}

func (m *MinerEngine) SetCPUIntensity(intensity int) {
	m.mu.Lock()
	if intensity < 10 {
		intensity = 10
	}
	if intensity > 100 {
		intensity = 100
	}
	m.cpuIntensity = intensity
	m.mu.Unlock()
	m.SaveConfig()
	m.addLog(fmt.Sprintf("⚡ Đã điều chỉnh công suất/cường độ thợ đào GPU CUDA: %d%%", intensity), "info")
}

func (m *MinerEngine) GetCPUIntensity() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cpuIntensity <= 0 {
		return 100
	}
	return m.cpuIntensity
}

func (m *MinerEngine) SetCPUThreads(threads int) {
	m.mu.Lock()
	maxCores := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}
	if threads > maxCores {
		threads = maxCores
	}
	m.cpuThreads = threads
	m.mu.Unlock()
	m.SaveConfig()
	m.addLog(fmt.Sprintf("⚙️ Đã điều chỉnh số luồng thợ đào: %d/%d luồng", threads, maxCores), "info")
}

func (m *MinerEngine) GetCPUThreads() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cpuThreads <= 0 {
		return runtime.NumCPU()
	}
	return m.cpuThreads
}

// GenerateNewBIP39Wallet creates a 12-word BIP39 mnemonic and Ed25519 derived wallet address
func GenerateNewBIP39Wallet() (string, string, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", "", err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", "", err
	}

	seed := bip39.NewSeed(mnemonic, "")
	hash := sha256.Sum256(seed)
	privKey := ed25519.NewKeyFromSeed(hash[:])
	pubKey := privKey.Public().(ed25519.PublicKey)

	address := fmt.Sprintf("0x%x", pubKey)
	return mnemonic, address, nil
}

// RecoverWalletFromMnemonic derives the address from a 12-word BIP39 mnemonic
func RecoverWalletFromMnemonic(mnemonic string) (string, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	words := strings.Fields(mnemonic)
	if len(words) != 12 {
		return "", fmt.Errorf("mnemonic must be exactly 12 words")
	}
	cleanMnemonic := strings.Join(words, " ")

	if !bip39.IsMnemonicValid(cleanMnemonic) {
		return "", fmt.Errorf("invalid BIP39 mnemonic phrase")
	}

	seed := bip39.NewSeed(cleanMnemonic, "")
	hash := sha256.Sum256(seed)
	privKey := ed25519.NewKeyFromSeed(hash[:])
	pubKey := privKey.Public().(ed25519.PublicKey)

	address := fmt.Sprintf("0x%x", pubKey)
	return address, nil
}


func getWSURL(nodeAddr, wallet string) string {
	host := nodeAddr
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return fmt.Sprintf("ws://%s:28888/ws/mining?address=%s", host, wallet)
}

func (m *MinerEngine) startWebSocketTunnel() {
	m.mu.Lock()
	if m.wsStopChan != nil {
		close(m.wsStopChan)
	}
	m.wsStopChan = make(chan struct{})
	stopChan := m.wsStopChan
	wallet := m.wallet
	nodeAddr := m.nodeAddr
	m.mu.Unlock()

	wsURL := getWSURL(nodeAddr, wallet)

	go func() {
		m.addLog(fmt.Sprintf("🔌 Khởi tạo WebSocket Tunnel: %s", wsURL), "info")

		for {
			select {
			case <-stopChan:
				return
			default:
			}

			dialer := websocket.Dialer{
				TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
				HandshakeTimeout: 5 * time.Second,
			}

			conn, _, err := dialer.Dial(wsURL, nil)
			if err != nil {
				m.mu.Lock()
				m.wsActive = false
				m.mu.Unlock()
				select {
				case <-stopChan:
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			m.mu.Lock()
			m.wsConn = conn
			m.wsActive = true
			m.mu.Unlock()
			m.addLog("🟢 Đã kết nối WebSocket Tunnel tới VPS Node! Lắng nghe khối mới...", "success")

			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					m.addLog(fmt.Sprintf("⚠️ WebSocket Tunnel đứt kết nối: %v. Đang kết nối lại...", err), "warn")
					conn.Close()
					m.mu.Lock()
					m.wsActive = false
					m.wsConn = nil
					m.mu.Unlock()
					break
				}

				m.lastWorkMu.Lock()
				m.lastWorkBytes = msg
				m.lastWorkMu.Unlock()
			}
		}
	}()
}

func (m *MinerEngine) GetCachedWork() []byte {
	m.lastWorkMu.RLock()
	defer m.lastWorkMu.RUnlock()
	if len(m.lastWorkBytes) == 0 {
		return nil
	}
	copied := make([]byte, len(m.lastWorkBytes))
	copy(copied, m.lastWorkBytes)
	return copied
}
