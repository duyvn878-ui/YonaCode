package audit

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"gopkg.in/natefinch/lumberjack.v2"
)

var AuditLogger *log.Logger

// InitAuditLogger khởi tạo luồng ghi Log Kiểm toán riêng biệt dưới thư mục database của node.
func InitAuditLogger(dbPath string) {
	logDir := filepath.Join(dbPath, "audit_logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("[AUDIT-INIT-ERROR] ❌ Không thể tạo thư mục audit_logs: %v", err)
	}

	auditFile := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "security_audit.log"),
		MaxSize:    50,   // 50 MB mỗi file
		MaxBackups: 365,  // Lưu trữ log trong vòng 365 ngày phục vụ điều tra
		MaxAge:     365,
		Compress:   true, // Tự động nén để tiết kiệm tối đa dung lượng ổ đĩa
	}

	// Tạo logger độc lập không ảnh hưởng đến log mặc định của hệ thống
	AuditLogger = log.New(auditFile, "", 0)
	log.Printf("[AUDIT-INIT] 🛡️ Hệ thống Log Kiểm toán Bảo mật đã được kích hoạt tại: %s", auditFile.Filename)
}

// AuditLog ghi log kiểm toán có cấu trúc phục vụ phân tích tự động (Splunk/ELK) và in ra Console.
func AuditLog(attackType, peerID, details string) {
	timestamp := time.Now().Format(time.RFC3339)
	
	// Định dạng chuẩn có cấu trúc
	logLine := fmt.Sprintf("[%s] [ATTACK_TYPE: %s] [PEER: %s] DETAILS: %s", 
		timestamp, attackType, peerID, details)
	
	if AuditLogger != nil {
		AuditLogger.Println(logLine)
	}
	
	// Hiển thị màu đỏ nổi bật trực tiếp trên console quản lý để quản trị viên phát hiện ngay lập tức
	color.Red("🚨 BÁO ĐỘNG KIỂM TOÁN: %s", logLine)
}
