package internal

import "fmt"

var CurrentLang = "vnm"

var translations = map[string]map[string]string{
	"vnm": {
		"welcome": "🚀 YonaCode Go Lite v1.0 - Tối Giản, Bất Biến, Siêu Nhẹ",
		"dashboard_title": "=== 📊 BẢNG ĐIỀU KHIỂN YonaCode Go ===",
		"network_synced": "🟢 Trạng thái Mạng: ĐỒNG BỘ (Synced)",
		"network_offline": "🔴 Trạng thái Mạng: MẤT KẾ NỐI (Offline)",
		"peers": "🔗 Số Peers kết nối: %d Nodes",
		"height": "🧱 Chiều cao khối  : #%d",
		"next_block": "⏳ Khối tiếp theo  : ~ %d giây",
		"finality_title": "🛡️ TƯỜNG LỬA BẤT BIẾN (Finality Firewall)",
		"finalized_height": "   - Khối Đã Chốt (Bất biến) : #%d (Rule of 5)",
		"mining_active": "   - Trạng thái Khai thác     : 🟢 ĐANG ĐÀO (%d H/s)",
		"mining_paused": "   - Trạng thái Khai thác     : 🟡 TẠM DỪNG",
		"wallet_title": "💎 TÀI KHOẢN HIỆN TẠI (Ví Mặc định)",
		"wallet_addr": "   - Địa chỉ : %s...",
		"version": "   - Phiên bản: %s",
		"start_node": "🚀 Đang khởi chạy Node Server...",
		"bootstrap_1": "[1/5] 🌐 Tìm kiếm Đồng loại (DHT Discovery)",
		"bootstrap_2": "[2/5] ⚡ Thẩm định Năng lượng (Tải Header)",
		"bootstrap_3": "[3/5] 📸 Khớp nối Thực tại (Tải State Snapshot)",
		"bootstrap_4": "[4/5] 🔄 Hội quân Bắt nhịp (Đồng bộ Real-time)",
		"bootstrap_5": "[5/5] ✅ NHẬP CUỘC THÀNH CÔNG!",
		"wallet_send_title": "🛸 YonaCode Go Giao diện Chuyển tiền an toàn",
		"wallet_send_confirm": "--- 🛡️ XÁC NHẬN GIAO DỊCH ---",
		"wallet_send_success": "✅ [THANH CÔNG] Giao dịch đã được đẩy vào Mempool!",
		"wallet_send_fail": "❌ Phát sóng thất bại: %v",
		"mining_start": "⛏️ Đang kích hoạt thợ đào...",
		"mining_stop": "🛑 Đang dừng thợ đào...",
	},
	"eng": {
		"welcome": "🚀 YonaCode Go Lite v1.0 - Minimalist, Immutable, Ultralight",
		"dashboard_title": "=== 📊 YonaCode Go DASHBOARD ===",
		"network_synced": "🟢 Network Status: SYNCED",
		"network_offline": "🔴 Network Status: OFFLINE",
		"peers": "🔗 Connected Peers: %d Nodes",
		"height": "🧱 Block Height   : #%d",
		"next_block": "⏳ Next Block     : ~ %d secs",
		"finality_title": "🛡️ FINALITY FIREWALL",
		"finalized_height": "   - Finalized Block (Immutable): #%d (Rule of 5)",
		"mining_active": "   - Mining Status           : 🟢 ACTIVE (%d H/s)",
		"mining_paused": "   - Mining Status           : 🟡 PAUSED",
		"wallet_title": "💎 CURRENT ACCOUNT (Default Wallet)",
		"wallet_addr": "   - Address : %s...",
		"version": "   - Version : %s",
		"start_node": "🚀 Launching Node Server...",
		"bootstrap_1": "[1/5] 🌐 Peer Discovery (DHT)",
		"bootstrap_2": "[2/5] ⚡ Energy Valuation (Headers)",
		"bootstrap_3": "[3/5] 📸 Reality Coupling (State Snapshot)",
		"bootstrap_4": "[4/5] 🔄 Joining the Rhythm (Real-time Sync)",
		"bootstrap_5": "[5/5] ✅ SYNC SUCCESSFUL!",
		"wallet_send_title": "🛸 YonaCode Go Secure Transfer Interface",
		"wallet_send_confirm": "--- 🛡️ TRANSACTION CONFIRMATION ---",
		"wallet_send_success": "✅ [SUCCESS] Transaction pushed to Mempool!",
		"wallet_send_fail": "❌ Broadcast failed: %v",
		"mining_start": "⛏️ Activating miner...",
		"mining_stop": "🛑 Stopping miner...",
	},
}

func T(key string, args ...interface{}) string {
	langDict, ok := translations[CurrentLang]
	if !ok {
		langDict = translations["vnm"]
	}
	
	val, ok := langDict[key]
	if !ok {
		return key
	}
	
	if len(args) > 0 {
		return fmt.Sprintf(val, args...)
	}
	return val
}

func SetLang(lang string) {
	if lang == "eng" || lang == "vnm" {
		CurrentLang = lang
	}
}
