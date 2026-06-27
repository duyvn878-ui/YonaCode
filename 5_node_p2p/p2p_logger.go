package node_p2p

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	p2pLogFile *os.File
	p2pLogMu   sync.Mutex // Cầu dao đồng bộ bảo vệ việc ghi file từ nhiều luồng đồng thời
)

// InitP2PLogger khởi tạo logger chuyên dụng cho các kết nối P2P
func InitP2PLogger(dbPath string, writeLog bool) {
	if !writeLog {
		log.Println("[P2P-LOGGER] 🕊️ Chế độ bảo vệ SSD: Chỉ ghi nhận log P2P ra Console.")
		return
	}

	logDir := filepath.Join(dbPath, "p2p_log")
	os.MkdirAll(logDir, 0755)
	
	filePath := filepath.Join(logDir, "p2p_connect.log")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[P2P-LOGGER-WARN] ⚠️ Không thể tạo file log P2P riêng biệt: %v", err)
		return
	}
	
	p2pLogMu.Lock()
	p2pLogFile = file
	p2pLogMu.Unlock()
	
	P2PLog("==================================================================")
	P2PLog("🚀 KHỞI ĐỘNG HỆ THỐNG GIÁM SÁT KẾT NỐI P2P (IPv6 VANGUARD PATCH)")
	P2PLog("==================================================================")
}

// P2PLog ghi log kết nối vào file log P2P riêng biệt
func P2PLog(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05.000000")
	logLine := fmt.Sprintf("%s %s\n", timestamp, msg)
	
	// Bảo vệ ghi file song song tránh tranh chấp tài nguyên (Race Condition)
	p2pLogMu.Lock()
	if p2pLogFile != nil {
		p2pLogFile.WriteString(logLine)
	}
	p2pLogMu.Unlock()
	
	// Đồng thời vẫn in ra logger hệ thống chung để hiển thị trên Dashboard
	log.Print(msg)
}
