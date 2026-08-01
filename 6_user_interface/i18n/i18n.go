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

		// Nhóm Pool Mining CLI
		"pool_mining_short":   "⛏️ Khai thác Bể Đào (Pool Mining) kết nối tự động tới VPS mặc định",
		"pool_err_no_address": "❌ Lỗi: Vui lòng nhập địa chỉ ví của bạn để nhận thưởng. Ví dụ: YonaCode pool-mine 0xYourAddress",
		"pool_err_no_miner":   "❌ Không tìm thấy Thợ đào độc lập (%s). Vui lòng biên dịch lại dự án bằng build_all.bat trước.",
		"pool_start_gpu":      "[POOL-MINE] 🚀 Đang khởi chạy Thợ đào GPU kết nối tự động tới Pool VPS mặc định (%s)...",
		"pool_start_cpu":      "[POOL-MINE] 🚀 Đang khởi chạy Thợ đào CPU (Rust Core) kết nối tới Pool VPS (%s)...",
		"pool_wallet_info":    "[POOL-MINE] 🏦 Ví thợ đào nhận thưởng: %s",
		"pool_err_run":        "❌ Lỗi trong quá trình chạy thợ đào: %v",

		"openpool_success_title":       "🚀 CỔNG ĐÀO TRUNG (MINING POOL GATEWAY) ĐÃ ĐƯỢC KÍCH HOẠT THÀNH CÔNG!",
		"openpool_lan_ip":              "  📡 Địa chỉ IP Nội bộ (LAN IP)   : %s",
		"openpool_wan_ip":              "  🌐 Địa chỉ IP Công khai (WAN IP): %s",
		"openpool_port":                "  ⚓ Cổng Khai Thác (Mining Port) : %d",
		"openpool_pass":                "  🔐 Mật khẩu / Auth Passphrase   : %s",
		"openpool_connect_instructions": "👉 CÚ PHÁP CHO CÁC MÁY KHÁC / TRÂU ĐÀO KHÁC KẾT NỐI VÀO ĐỂ ĐÀO CHUNG:",
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

		// Nhóm Pool Mining CLI
		"pool_mining_short":   "⛏️ Pool Mining connected to the default VPS",
		"pool_err_no_address": "❌ Error: Please enter your wallet address to receive rewards. Example: YonaCode pool-mine 0xYourAddress",
		"pool_err_no_miner":   "❌ Independent miner binary (%s) not found. Please compile the project using build_all.bat first.",
		"pool_start_gpu":      "[POOL-MINE] 🚀 Launching GPU Miner connected to default Pool VPS (%s)...",
		"pool_start_cpu":      "[POOL-MINE] 🚀 Launching CPU Miner (Rust Core) connected to Pool VPS (%s)...",
		"pool_wallet_info":    "[POOL-MINE] 🏦 Reward wallet address: %s",
		"pool_err_run":        "❌ Error during miner execution: %v",

		"openpool_success_title":       "🚀 MINING POOL GATEWAY ACTIVATED SUCCESSFULLY!",
		"openpool_lan_ip":              "  📡 Local LAN IP Address   : %s",
		"openpool_wan_ip":              "  🌐 Public WAN IP Address  : %s",
		"openpool_port":                "  ⚓ Mining Pool Port        : %d",
		"openpool_pass":                "  🔐 Auth Password / Token  : %s",
		"openpool_connect_instructions": "👉 COMMANDS FOR REMOTE MINERS / RIGS TO JOIN THIS POOL:",
	},
	"zho": {
		"welcome": "🚀 YonaCode Go Lite v1.0 - 极简、不可变、超轻量",
		"dashboard_title": "=== 📊 YonaCode Go 控制面板 ===",
		"network_synced": "🟢 网络状态: 已同步 (Synced)",
		"network_offline": "🔴 网络状态: 离线 (Offline)",
		"peers": "🔗 已连接节点数: %d Nodes",
		"height": "🧱 区块高度  : #%d",
		"next_block": "⏳ 下一个区块  : ~ %d 秒",
		"finality_title": "🛡️ 不可变防火墙 (Finality Firewall)",
		"finalized_height": "   - 已确认区块 (不可变) : #%d (Rule of 5)",
		"mining_active": "   - 挖矿状态     : 🟢 正在挖矿 (%d H/s)",
		"mining_paused": "   - 挖矿状态     : 🟡 暂停",
		"wallet_title": "💎 当前账户 (默认钱包)",
		"wallet_addr": "   - 钱包地址 : %s...",
		"version": "   - 版本: %s",
		"start_node": "🚀 正在启动节点服务器...",
		"bootstrap_1": "[1/5] 🌐 寻找对等节点 (DHT Discovery)",
		"bootstrap_2": "[2/5] ⚡ 算力评估 (下载区块头)",
		"bootstrap_3": "[3/5] 📸 现实耦合 (下载状态快照)",
		"bootstrap_4": "[4/5] 🔄 实时同步 (实时区块同步)",
		"bootstrap_5": "[5/5] ✅ 成功接入网络！",
		"wallet_send_title": "🛸 YonaCode Go 安全转账界面",
		"wallet_send_confirm": "--- 🛡️ 确认交易 ---",
		"wallet_send_success": "✅ [成功] 交易已提交至交易池 (Mempool)！",
		"wallet_send_fail": "❌ 广播失败: %v",
		"mining_start": "⛏️ 正在激活矿工...",
		"mining_stop": "🛑 正在停止矿工...",

		// Nhóm Khởi động & Mạng P2P (Startup & P2P Network)
		"log_node_storage":             "📂 基础设施已标准化于: %s",
		"log_db_trace_success":         "✅ RocksDB 成功打开。",
		"log_p2p_listening":            "📡 节点监听地址: %s",
		"log_p2p_bootstrap_success":    "✅ 成功连接种子 IP %s！",
		"log_nat_audit_public_detected": "✅ 检测到公网地址: %s",

		// Nhóm Đồng bộ & Sổ cái (Sync Engine & Ledger)
		"log_sync_success":    "🎉 网络已同步至区块高度 #%d。",
		"log_sync_catchup":    "🚀 检测到网络分叉较大。激活 CatchUpSync！",
		"log_sync_orphan":     "🧩 检测到短链分叉。正在激活孤块链校对...",
		"log_fast_sync_start": "⚡ 开始跳跃至锚点区块 #%d...",
		"log_reorg_success":   "🔄 成功原子重组 (Reorg) 至区块高度 #%d！",
		"log_sync_stalled":    "🛑 检测到同步卡顿，在区块 #%d 处停留超过 20 秒！",

		// Nhóm An ninh & Trừng phạt (Security & Ban Manager)
		"log_peer_ban":            "🚫 %s 封禁对等节点 %s 持续 %v (惩罚分数: %d)。原因: %s",
		"log_peer_forgiven":       "🕊️ 宽恕对等节点 %s: 惩罚分数从 %d → %d (持续 %v 未违规扣减 %d 分)",
		"log_security_alert_pow":  "🛑 拦截来自 %s 的垃圾 PoW。区块高度=#%d",
		"log_firewall_deep_reorg": "💀 检测到来自 %s 的深度重组 (DEEP REORG) 攻击！断开连接！",
		"log_time_warp_violation": "🚨 #%d 处违反时间防火墙 (MTP-11)",

		// Nhóm Khai thác & Mempool (Miner & Mempool)
		"log_miner_preparing":       "🚀 矿工正准备计算区块 #%d 的哈希...",
		"log_miner_block_found":     "🎉 恭喜！找到区块 #%d！Nonce: %d",
		"log_mempool_spam_rejected": "🛑 Rust Core 已拒绝 %d 笔垃圾交易。",
		"log_mempool_eviction":      "🚮 已清理 %d 笔尾部交易以防内存溢出 (OOM)。",

		// Nhóm Pool Mining CLI
		"pool_mining_short":   "⛏️ 矿池挖矿自动连接到默认 VPS",
		"pool_err_no_address": "❌ 错误: 请输入您的钱包地址以接收奖励。例如: YonaCode pool-mine 0xYourAddress",
		"pool_err_no_miner":   "❌ 未找到独立矿工程序 (%s)。请先使用 build_all.bat 重新编译项目。",
		"pool_start_gpu":      "[POOL-MINE] 🚀 正在启动 GPU 矿工，自动连接到默认矿池 VPS (%s)...",
		"pool_start_cpu":      "[POOL-MINE] 🚀 正在启动 CPU 矿工 (Rust Core)，连接到矿池 VPS (%s)...",
		"pool_wallet_info":    "[POOL-MINE] 🏦 奖励接收钱包: %s",
		"pool_err_run":        "❌ 矿工运行错误: %v",

		"openpool_success_title":       "🚀 联合矿池网关 (MINING POOL GATEWAY) 激活成功！",
		"openpool_lan_ip":              "  📡 局域网 IP 地址 (LAN IP)   : %s",
		"openpool_wan_ip":              "  🌐 公网 IP 地址 (WAN IP): %s",
		"openpool_port":                "  ⚓ 挖矿端口 (Mining Port) : %d",
		"openpool_pass":                "  🔐 认证密码 / Auth Passphrase   : %s",
		"openpool_connect_instructions": "👉 其他矿机/矿工连接此矿池的命令语法:",
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

// SetLang thiết lập ngôn ngữ hiện tại (vnm, eng hoặc zho).
func SetLang(lang string) {
	if lang == "eng" || lang == "vnm" || lang == "zho" {
		CurrentLang = lang
	}
}
