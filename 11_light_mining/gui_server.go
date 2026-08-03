package main

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web_ui/dist/*
var embeddedWebUI embed.FS

type GUIServer struct {
	engine     *MinerEngine
	port       int
	httpClient *http.Client
	mu         sync.Mutex
	isStarted  bool
}

var globalProxyServer *GUIServer
var proxyOnce sync.Once

func GetGlobalProxyServer(engine *MinerEngine, port int) *GUIServer {
	proxyOnce.Do(func() {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		if port <= 0 {
			port = 28888
		}
		globalProxyServer = &GUIServer{
			engine:     engine,
			port:       port,
			httpClient: &http.Client{
				Transport: tr,
				Timeout:   5 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}
		go globalProxyServer.EnsureStarted()
	})
	return globalProxyServer
}

func (s *GUIServer) EnsureStarted() {
	s.mu.Lock()
	if s.isStarted {
		s.mu.Unlock()
		return
	}
	s.isStarted = true
	s.mu.Unlock()

	distFS, err := fs.Sub(embeddedWebUI, "web_ui/dist")
	if err != nil {
		log.Printf("[SERVER] ⚠️ Failed to extract embedded FS: %v", err)
	}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/v1/node/cpu", s.handleNodeCPU)
	mux.HandleFunc("/api/v1/", s.handleProxyV1)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/shutdown", s.handleShutdown)
	mux.HandleFunc("/api/cpu/config", s.handleCPUConfig)
	mux.HandleFunc("/api/wallet/create", s.handleWalletCreate)
	mux.HandleFunc("/api/wallet/recover", s.handleWalletRecover)

	// Static file handler (Embedded FS with disk fallback)
	if distFS != nil {
		fileServer := http.FileServer(http.FS(distFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				return
			}
			// Try serving from embedded filesystem first
			f, err := distFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA fallback to embedded index.html
			indexFile, err := distFS.Open("index.html")
			if err == nil {
				defer indexFile.Close()
				stat, _ := indexFile.Stat()
				http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile.(io.ReadSeeker))
				return
			}
			// Disk fallback if needed
			s.handleIndexFallback(w, r)
		})
	} else {
		mux.HandleFunc("/", s.handleIndexFallback)
	}

	var l net.Listener
	for attempt := 0; attempt < 5; attempt++ {
		l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	if err != nil {
		log.Printf("[SERVER] ⚠️ Port %d busy, binding to free port...", s.port)
		l, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Printf("[SERVER] ❌ Failed to listen: %v", err)
			return
		}
		s.port = l.Addr().(*net.TCPAddr).Port
	}

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("[SERVER] 🚀 Web UI & Internal Proxy listening on http://%s", addr)
	server := &http.Server{Handler: mux}
	server.Serve(l)
}

func (s *GUIServer) handleIndexFallback(w http.ResponseWriter, r *http.Request) {
	candidates := []string{
		"11_light_mining/web_ui/dist",
		"web_ui/dist",
		"./web_ui/dist",
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append([]string{
			filepath.Join(execDir, "web_ui", "dist"),
			filepath.Join(execDir, "..", "11_light_mining", "web_ui", "dist"),
			filepath.Join(execDir, "11_light_mining", "web_ui", "dist"),
		}, candidates...)
	}

	var distDir string
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			distDir = dir
			break
		}
	}

	if distDir != "" {
		relPath := strings.TrimPrefix(r.URL.Path, "/")
		if relPath == "" {
			relPath = "index.html"
		}
		filePath := filepath.Join(distDir, relPath)

		if fi, err := os.Stat(filePath); err == nil && !fi.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}

		indexPath := filepath.Join(distDir, "index.html")
		if fi, err := os.Stat(indexPath); err == nil && !fi.IsDir() {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	http.Error(w, "Embedded Web UI not found", http.StatusNotFound)
}

func (s *GUIServer) handleProxyV1(w http.ResponseWriter, r *http.Request) {
	s.engine.mu.Lock()
	customNode := s.engine.nodeAddr
	s.engine.mu.Unlock()

	nodeCandidates := []string{}
	if customNode != "" {
		nodeAddrStr := customNode
		if !strings.HasPrefix(nodeAddrStr, "http://") && !strings.HasPrefix(nodeAddrStr, "https://") {
			nodeAddrStr = "http://" + nodeAddrStr
		}
		nodeCandidates = append(nodeCandidates, nodeAddrStr)

		// Thay thế cổng 9090 và 8080 thành cổng 28888 (cổng của Proxy Gateway trên VPS)
		// để thợ đào gửi khối thành công mà không bị tường lửa chặn hoặc lỗi chuyển hướng HTTP.
		if strings.Contains(nodeAddrStr, ":9090") {
			nodeCandidates = append(nodeCandidates, strings.Replace(nodeAddrStr, ":9090", ":28888", 1))
		}
		if strings.Contains(nodeAddrStr, ":8080") {
			nodeCandidates = append(nodeCandidates, strings.Replace(nodeAddrStr, ":8080", ":28888", 1))
		}
	}

	// Các địa chỉ mặc định dự phòng cho VPS YonaCode, ưu tiên cổng Proxy (28888) và giao thức HTTPS
	// nhằm ngăn chặn lỗi chuyển hướng 301 biến đổi phương thức HTTP POST thành GET.
	nodeCandidates = append(nodeCandidates,
		"http://110.172.28.103:28888",
		"https://explorer.yonacode.com",
		"https://110.172.28.103",
		"http://110.172.28.103",
	)


	reqPath := r.URL.Path
	isGetWork := strings.Contains(reqPath, "getwork")
	isSubmitWork := strings.Contains(reqPath, "submitwork")

	if strings.Contains(reqPath, "/pool/getwork") {
		reqPath = "/api/v1/miner/getwork"
	} else if strings.Contains(reqPath, "/pool/submitwork") {
		reqPath = "/api/v1/miner/submitwork"
	}

	bodyBytes, _ := io.ReadAll(r.Body)

	var resp *http.Response
	var err error

	for _, nodeAddr := range nodeCandidates {
		targetURL := nodeAddr + reqPath
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		req, reqErr := http.NewRequest(r.Method, targetURL, bytes.NewBuffer(bodyBytes))
		if reqErr != nil {
			continue
		}

		for k, vv := range r.Header {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}

		resp, err = s.httpClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if (err != nil || resp == nil || resp.StatusCode != 200) && isGetWork {
		if cachedWork := s.engine.GetCachedWork(); cachedWork != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(cachedWork)
			return
		}
		errMsg := "VPS Node is not ready or mining template is currently unavailable."
		if resp != nil && resp.StatusCode == 204 {
			errMsg = "VPS Node returned 204 (No Content) - Wallet might not be recovered/ready on node."
		} else if err != nil {
			errMsg = fmt.Sprintf("Failed to reach VPS Node: %v", err)
		} else if resp != nil {
			errMsg = fmt.Sprintf("VPS Node returned error status: %d", resp.StatusCode)
		}
		http.Error(w, errMsg, http.StatusServiceUnavailable)
		return
	}

	if err != nil || resp == nil {
		http.Error(w, "VPS Node Gateway Unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if isSubmitWork {
		if resp.StatusCode == 200 {
			st := s.engine.GetStatus()
			currentHeight := st["network_height"].(uint64)

			var submitResp struct {
				Reward float64 `json:"reward"`
			}
			var rewardVal float64
			if respBody, rErr := io.ReadAll(resp.Body); rErr == nil {
				_ = json.Unmarshal(respBody, &submitResp)
				rewardVal = submitResp.Reward
				for k, vv := range resp.Header {
					for _, v := range vv {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(http.StatusOK)
				w.Write(respBody)
			}
			s.engine.RecordMinedBlock(currentHeight, rewardVal)
			s.engine.addLog(fmt.Sprintf("🎉 Nộp khối #%d thành công tới VPS Node", currentHeight), "success")
			return
		} else {
			s.engine.addLog(fmt.Sprintf("⚠️ Nộp khối tới VPS Node thất bại (HTTP %d)", resp.StatusCode), "warn")
		}
	}

	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		if isGetWork {
			var workData struct {
				HeaderHash string `json:"header_hash"`
				Target     string `json:"target"`
				Height     uint64 `json:"height"`
				ParentHash string `json:"parent_hash"`
				SessionID  int64  `json:"session_id"`
				MerkleRoot string `json:"merkle_root"`
				Difficulty string `json:"difficulty"`
			}
			if json.Unmarshal(bodyBytes, &workData) == nil {
				newResp := struct {
					HeaderHash   string `json:"header_hash"`
					Target       string `json:"target"`
					Height       uint64 `json:"height"`
					ParentHash   string `json:"parent_hash"`
					SessionID    int64  `json:"session_id"`
					Intensity    int    `json:"intensity"`
					CPUIntensity int    `json:"cpu_intensity"`
					MerkleRoot   string `json:"merkle_root"`
					Difficulty   string `json:"difficulty"`
				}{
					HeaderHash:   workData.HeaderHash,
					Target:       workData.Target,
					Height:       workData.Height,
					ParentHash:   workData.ParentHash,
					SessionID:    workData.SessionID,
					Intensity:    100,
					CPUIntensity: 100,
					MerkleRoot:   workData.MerkleRoot,
					Difficulty:   workData.Difficulty,
				}
				if newBody, mErr := json.Marshal(newResp); mErr == nil {
					bodyBytes = newBody
				}
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
	} else {
		w.WriteHeader(resp.StatusCode)
	}
}

func (s *GUIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := s.engine.GetStatus()
	json.NewEncoder(w).Encode(status)
}

func (s *GUIServer) handleStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Wallet   string `json:"wallet"`
		NodeAddr string `json:"node_addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err := s.engine.Start(req.NodeAddr, req.Wallet, "gpu", 0)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *GUIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	s.engine.Stop()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *GUIServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "status": "shutting_down"})

	go func() {
		time.Sleep(500 * time.Millisecond)
		s.engine.Stop()
		os.Exit(0)
	}()
}

func (s *GUIServer) handleWalletCreate(w http.ResponseWriter, r *http.Request) {
	mnemonic, address, err := GenerateNewBIP39Wallet()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// Cập nhật địa chỉ ví mới tạo vào engine và lưu lại cấu hình ngay lập tức để tránh bị ghi đè bởi địa chỉ cũ khi UI tải lại
	s.engine.SetWallet(address)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"mnemonic": mnemonic,
		"address":  address,
	})
}

func (s *GUIServer) handleWalletRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mnemonic string `json:"mnemonic"`
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	address, err := RecoverWalletFromMnemonic(req.Mnemonic)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	// Lưu địa chỉ ví khôi phục vào engine và đồng bộ xuống miner_config.json để duy trì trạng thái nhất quán sau khi đóng/mở lại trình duyệt
	s.engine.SetWallet(address)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"address": address,
	})
}


func (s *GUIServer) handleNodeCPU(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cpu_intensity": s.engine.GetCPUIntensity(),
		"cpu_threads":   s.engine.GetCPUThreads(),
		"cpu_cores":     runtime.NumCPU(),
	})
}

func (s *GUIServer) handleCPUConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Intensity int `json:"cpu_intensity"`
		Threads   int `json:"cpu_threads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		if req.Intensity > 0 {
			s.engine.SetCPUIntensity(req.Intensity)
		}
		if req.Threads > 0 {
			s.engine.SetCPUThreads(req.Threads)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"cpu_intensity": s.engine.GetCPUIntensity(),
		"cpu_threads":   s.engine.GetCPUThreads(),
	})
}

func OpenBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	exec.Command(cmd, args...).Start()
}
