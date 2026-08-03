package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

const SERVER_BANNER = `
 ===============================================================
   Y O N A C O D E   V P S   P R O X Y   S E R V E R   ( v 1 . 0 )
 ---------------------------------------------------------------
   Lightweight Gateway • Isolates Full Node Core • 100% Secure
 ===============================================================
`

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type VPSProxyServer struct {
	port       int
	nodeURL    string
	httpClient *http.Client
	mu         sync.Mutex
	lastHeight uint64
	lastTip    string
	clients    map[string][]*websocket.Conn
	clientsMu  sync.Mutex
}

func NewVPSProxyServer(port int, nodeURL string) *VPSProxyServer {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if strings.Contains(nodeURL, ":9090") {
		nodeURL = strings.Replace(nodeURL, ":9090", ":8080", 1)
	}
	return &VPSProxyServer{
		port:       port,
		nodeURL:    strings.TrimSuffix(nodeURL, "/"),
		httpClient: &http.Client{Transport: tr, Timeout: 5 * time.Second},
		clients:    make(map[string][]*websocket.Conn),
	}
}

func (s *VPSProxyServer) registerClient(address string, conn *websocket.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[address] = append(s.clients[address], conn)
	log.Printf("[VPS-PROXY] 👤 Client connected for address %s (Total clients for this wallet: %d)", address, len(s.clients[address]))
}

func (s *VPSProxyServer) unregisterClient(address string, conn *websocket.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	conns := s.clients[address]
	for i, c := range conns {
		if c == conn {
			s.clients[address] = append(conns[:i], conns[i+1:]...)
			log.Printf("[VPS-PROXY] 👤 Client disconnected for address %s", address)
			break
		}
	}
	if len(s.clients[address]) == 0 {
		delete(s.clients, address)
	}
}

func (s *VPSProxyServer) pushTemplateToAddress(address string) {
	targetURL := fmt.Sprintf("%s/api/v1/miner/getwork?address=%s", s.nodeURL, address)
	resp, err := s.httpClient.Get(targetURL)
	if err != nil {
		log.Printf("[VPS-PROXY] ❌ Error fetching template for %s: %v", address, err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		log.Printf("[VPS-PROXY] ❌ Node returned status %d for template %s", resp.StatusCode, address)
		return
	}

	s.clientsMu.Lock()
	conns, exists := s.clients[address]
	if !exists || len(conns) == 0 {
		s.clientsMu.Unlock()
		return
	}
	connsCopy := make([]*websocket.Conn, len(conns))
	copy(connsCopy, conns)
	s.clientsMu.Unlock()

	for _, conn := range connsCopy {
		err := conn.WriteMessage(websocket.TextMessage, body)
		if err != nil {
			log.Printf("[VPS-PROXY] ❌ Fail to push template to WS client: %v", err)
			conn.Close()
			s.unregisterClient(address, conn)
		}
	}
}

func (s *VPSProxyServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if address == "" {
		http.Error(w, "Missing address parameter", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[VPS-PROXY] ❌ WebSocket Upgrade failed: %v", err)
		return
	}

	s.registerClient(address, conn)

	// Send initial template
	go s.pushTemplateToAddress(address)

	// Read loop to detect client disconnects and handle work requests
	go func() {
		defer func() {
			conn.Close()
			s.unregisterClient(address, conn)
		}()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var msg struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(message, &msg) == nil {
				if msg.Action == "request_work" {
					go s.pushTemplateToAddress(address)
				}
			}
		}
	}()
}

func (s *VPSProxyServer) startBlockMonitor() {
	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		for range ticker.C {
			resp, err := s.httpClient.Get(s.nodeURL + "/api/v1/status")
			if err != nil {
				continue
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}

			var stData map[string]interface{}
			if json.Unmarshal(body, &stData) == nil {
				if h, ok := stData["current_height"].(float64); ok {
					height := uint64(h)
					s.mu.Lock()
					oldHeight := s.lastHeight
					s.lastHeight = height
					s.mu.Unlock()

					if height > oldHeight && oldHeight > 0 {
						log.Printf("[VPS-PROXY] 🔔 New height detected: #%d (was #%d). Pushing new templates to all clients...", height, oldHeight)
						s.clientsMu.Lock()
						activeAddresses := make([]string, 0, len(s.clients))
						for addr := range s.clients {
							activeAddresses = append(activeAddresses, addr)
						}
						s.clientsMu.Unlock()

						for _, addr := range activeAddresses {
							go s.pushTemplateToAddress(addr)
						}
					}
				}
			}
		}
	}()
}

func (s *VPSProxyServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.httpClient.Get(s.nodeURL + "/api/v1/status")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "YonaCode Core Full Node unreachable on local VPS port 9090",
			"status": "node_offline",
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err == nil {
		var stData map[string]interface{}
		if json.Unmarshal(body, &stData) == nil {
			if h, ok := stData["current_height"].(float64); ok && uint64(h) > 0 {
				s.mu.Lock()
				s.lastHeight = uint64(h)
				if tip, ok := stData["tip_hash"].(string); ok {
					s.lastTip = tip
				}
				s.mu.Unlock()
			}

			netHash := uint64(0)
			if nh, ok := stData["network_hashrate"].(float64); ok && uint64(nh) > 0 {
				netHash = uint64(nh)
			}

			// Parse difficulty from status to calculate network_hashrate if 0
			if netHash == 0 {
				diff := float64(0)
				// Try "difficulty" field first
				if dStr, ok := stData["difficulty"].(string); ok {
					if dVal, err := strconv.ParseFloat(dStr, 64); err == nil {
						diff = dVal
					}
				} else if dNum, ok := stData["difficulty"].(float64); ok {
					diff = dNum
				}

				// Try "network_difficulty" field as fallback
				if diff == 0 {
					if ndStr, ok := stData["network_difficulty"].(string); ok {
						if ndVal, err := strconv.ParseFloat(ndStr, 64); err == nil {
							diff = ndVal
						}
					} else if ndNum, ok := stData["network_difficulty"].(float64); ok {
						diff = ndNum
					}
				}

				// If still 0, query /api/v1/explorer/stats as last resort and parse its difficulty string
				if diff == 0 {
					respExp, errExp := s.httpClient.Get(s.nodeURL + "/api/v1/explorer/stats")
					if errExp == nil {
						bodyExp, errExp := io.ReadAll(respExp.Body)
						respExp.Body.Close()
						if errExp == nil {
							var expData map[string]interface{}
							if json.Unmarshal(bodyExp, &expData) == nil {
								if nh, ok := expData["network_hashrate"].(float64); ok && uint64(nh) > 0 {
									netHash = uint64(nh)
								} else {
									// Extract difficulty from explorer stats
									var expDiff float64
									if dS, ok := expData["difficulty"].(string); ok {
										if dV, err := strconv.ParseFloat(dS, 64); err == nil {
											expDiff = dV
										}
									} else if dN, ok := expData["difficulty"].(float64); ok {
										expDiff = dN
									}
									if expDiff > 0 {
										diff = expDiff
									}
								}
							}
						}
					}
				}

				if netHash == 0 && diff > 0 {
					avgBT := float64(75.0)
					if abt, ok := stData["avg_block_time"].(float64); ok && abt > 0 {
						avgBT = abt
					}
					netHash = uint64(diff / avgBT)
				}
			}

			stData["network_hashrate"] = netHash
			stData["cpu_intensity"] = 100
			stData["intensity"] = 100
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			json.NewEncoder(w).Encode(stData)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (s *VPSProxyServer) handleGetWork(w http.ResponseWriter, r *http.Request) {
	// Truy cập trực tiếp cổng RPC Node Go trên VPS để lấy mẫu block thực tế (real block template)
	// Tránh tạo hash ảo và target cực thấp khiến thợ đào hiểu nhầm và bị từ chối khối khi nộp.
	targetURL := s.nodeURL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "reject",
			"message": "Failed to create proxy request: " + err.Error(),
		})
		return
	}

	// Sao chép headers từ yêu cầu gốc của thợ đào
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "reject",
			"message": "YonaCode Core Full Node unreachable: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Cập nhật lại chỉ số chiều cao từ Node chính để đồng bộ dữ liệu giám sát
	body, readErr := io.ReadAll(resp.Body)
	if readErr == nil && resp.StatusCode == 200 {
		var gw struct {
			Height uint64 `json:"height"`
		}
		if json.Unmarshal(body, &gw) == nil && gw.Height > 0 {
			s.mu.Lock()
			s.lastHeight = gw.Height - 1
			s.mu.Unlock()
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	} else {
		w.WriteHeader(resp.StatusCode)
	}
}

func (s *VPSProxyServer) handleSubmitWork(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)

	reqPath := r.URL.Path
	if strings.Contains(reqPath, "/pool/submitwork") {
		reqPath = "/api/v1/miner/submitwork"
	}

	targetURL := s.nodeURL + reqPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	resp, err := s.httpClient.Post(targetURL, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "reject",
			"message": "VPS Full Node connection failed: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	port := flag.Int("port", 28888, "Public VPS Light Proxy Server Port")
	nodeURL := flag.String("node", "http://127.0.0.1:8080", "Local Full Node RPC Address on VPS")
	flag.Parse()

	color.Cyan(SERVER_BANNER)
	color.Green("📡 STARTING STANDALONE VPS LIGHT MINING PROXY SERVER")
	color.Yellow("   Listening Public Port : :%d", *port)
	color.Yellow("   Target Full Node RPC : %s\n", *nodeURL)

	srv := NewVPSProxyServer(*port, *nodeURL)
	srv.startBlockMonitor()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", srv.handleStatus)
	mux.HandleFunc("/api/v1/miner/status", srv.handleStatus)
	mux.HandleFunc("/api/v1/miner/getwork", srv.handleGetWork)
	mux.HandleFunc("/api/v1/pool/getwork", srv.handleGetWork)
	mux.HandleFunc("/api/v1/miner/submitwork", srv.handleSubmitWork)
	mux.HandleFunc("/api/v1/pool/submitwork", srv.handleSubmitWork)
	mux.HandleFunc("/api/v1/balance/", srv.handleGetWork)
	mux.HandleFunc("/api/v1/block/", srv.handleGetWork)
	mux.HandleFunc("/api/v1/recent/blocks", srv.handleGetWork)
	mux.HandleFunc("/api/v1/address/", srv.handleGetWork)
	mux.HandleFunc("/ws/mining", srv.handleWebSocket)

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	log.Printf("[VPS-PROXY] 🚀 Light Mining Proxy Server active on %s", addr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[VPS-PROXY] ❌ Server Error: %v", err)
		}
	}()

	<-sigChan
	color.Yellow("\n🛑 Shutting down VPS Light Mining Proxy Server...")
	os.Exit(0)
}
