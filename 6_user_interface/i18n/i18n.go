package i18n

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

		// Nhóm Khởi động & Mạng P2P (Startup & P2P Network)
		"log_node_storage":             "📂 Hạ tầng đã được chuẩn hóa tại: %s",
		"log_db_trace_success":         "✅ Mở RocksDB thành công.",
		"log_p2p_listening":            "📡 Địa chỉ lắng nghe của Node: %s",
		"log_p2p_bootstrap_success":    "✅ Kết nối IP Hạt giống %s thành công!",
		"log_nat_audit_public_detected": "✅ Phát hiện địa chỉ công cộng: %s",

		// Nhóm Đồng bộ & Sổ cái (Sync Engine & Ledger)
		"log_sync_success":    "🎉 Mạng lưới đã đồng bộ đỉnh #%d.",
		"log_sync_catchup":    "🚀 Phát hiện lệch xa mạng lưới. Kích hoạt CatchUpSync!",
		"log_sync_orphan":     "🧩 Lệch chuỗi ngắn. Kích hoạt cân chỉnh chuỗi mồ côi...",
		"log_fast_sync_start": "⚡ Bắt đầu nhảy tới mỏ neo #%d...",
		"log_reorg_success":   "🔄 Reorg nguyên tử thành công lên cao độ #%d!",
		"log_sync_stalled":    "🛑 PHÁT HIỆN KẸT ĐỒNG BỘ tại khối #%d quá 20s!",

		// Nhóm An ninh & Trừng phạt (Security & Ban Manager)
		"log_peer_ban":            "🚫 %s Peer %s trong %v (điểm phạt: %d). Lý do: %s",
		"log_peer_forgiven":       "🕊️ Tha thứ Peer %s: %d → %d điểm phạt (-%d sau %v không vi phạm)",
		"log_security_alert_pow":  "🛑 Chặn rác PoW từ %s. Height=#%d",
		"log_firewall_deep_reorg": "💀 TẤN CÔNG DEEP REORG TỪ %s! Cắt kết nối!",
		"log_time_warp_violation": "🚨 Vi phạm Tường lửa thời gian (MTP-11) tại #%d",

		// Nhóm Khai thác & Mempool (Miner & Mempool)
		"log_miner_preparing":       "🚀 Thợ đào chuẩn bị băm khối #%d...",
		"log_miner_block_found":     "🎉 CHÚC MỪNG! Đã tìm thấy Khối #%d! Nonce: %d",
		"log_mempool_spam_rejected": "🛑 Rust Core đã chém %d giao dịch spam.",
		"log_mempool_eviction":      "🚮 Đã cắt bỏ %d giao dịch 'ngọn' để chống sập RAM.",
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

		// Nhóm Khởi động & Mạng P2P (Startup & P2P Network)
		"log_node_storage":             "📂 Infrastructure initialized at: %s",
		"log_db_trace_success":         "✅ RocksDB opened successfully.",
		"log_p2p_listening":            "📡 Node listening on: %s",
		"log_p2p_bootstrap_success":    "✅ Bootstrap peer %s connected successfully!",
		"log_nat_audit_public_detected": "✅ Public address detected: %s",

		// Nhóm Đồng bộ & Sổ cái (Sync Engine & Ledger)
		"log_sync_success":    "🎉 Network synced at tip #%d.",
		"log_sync_catchup":    "🚀 Deep chain divergence detected. CatchUpSync activated!",
		"log_sync_orphan":     "🧩 Short chain divergence. Resolving orphan chain...",
		"log_fast_sync_start": "⚡ Snapshot jumping to anchor #%d...",
		"log_reorg_success":   "🔄 Atomic Reorg successful to height #%d!",
		"log_sync_stalled":    "🛑 SYNC STALLED at block #%d for over 20s!",

		// Nhóm An ninh & Trừng phạt (Security & Ban Manager)
		"log_peer_ban":            "🚫 %s Peer %s for %v (penalty points: %d). Reason: %s",
		"log_peer_forgiven":       "🕊️ Forgiven Peer %s: %d → %d penalty points (-%d after %v with no violations)",
		"log_security_alert_pow":  "🛑 Rejected invalid PoW from %s. Height=#%d",
		"log_firewall_deep_reorg": "💀 DEEP REORG ATTACK from %s! Connection dropped!",
		"log_time_warp_violation": "🚨 Time firewall (MTP-11) violation at #%d",

		// Nhóm Khai thác & Mempool (Miner & Mempool)
		"log_miner_preparing":       "🚀 Miner preparing to hash block #%d...",
		"log_miner_block_found":     "🎉 CONGRATULATIONS! Block #%d found! Nonce: %d",
		"log_mempool_spam_rejected": "🛑 Rust Core rejected %d spam transactions.",
		"log_mempool_eviction":      "🚮 Evicted %d tail transactions to prevent OOM.",
	},
}

// T thực hiện dịch một key sang ngôn ngữ hiện tại.
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

// SetLang thiết lập ngôn ngữ hiện tại (vnm hoặc eng).
func SetLang(lang string) {
	if lang == "eng" || lang == "vnm" {
		CurrentLang = lang
	}
}
