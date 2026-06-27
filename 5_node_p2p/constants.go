package node_p2p

const (
	DNS_SEED_DOMAIN      = "seed.ghostcoi.com"
	GUARDIAN_NODE_DOMAIN = "node.ghostcoi.com"
	DEFAULT_P2P_PORT     = 9000
	SEED_CRAWL_INTERVAL  = 10 * 60 // 10 minutes in seconds
	MAX_DNS_SEEDS        = 50

	// --- CÔNG TẮC VÔ HIỆU HÓA RÕ RÀNG (EXPLICIT DEACTIVATION SWITCHES) ---
	EnableGreatPurge      = false // true: Bật đại thanh trừng 48h, false: Giữ lại toàn bộ block bodies vĩnh viễn
	EnableSnapshotJumping = false // true: Cho phép nhảy vọt snapshot khi đồng bộ, false: Luôn đồng bộ tuần tự từ Genesis

	// [VANGUARD-BLOCKSIZE] Kích thước khối mặc định được thiết lập là 5MB.
	// Hệ thống đã được tối ưu hóa để xử lý khối lên tới 35MB (khoảng 140.000 TPS).
	// Tuy nhiên, trong giai đoạn mạng lưới mới khởi động, chúng tôi tạm thời giới hạn
	// kích thước khối ở mức 5MB để đảm bảo tính ổn định của node trên mọi môi trường phần cứng.
	// Mức giới hạn này sẽ được nâng cấp thông qua Hardfork trong tương lai khi mạng lưới ổn định.
	DefaultBlockMaxSize = 5 * 1024 * 1024
)

var BlacklistedMiners = map[string]bool{
	// Không có miner nào bị cấm. Trả lại quyền tự do cho mạng lưới.
}

// BootstrapPeers là danh sách các node Bootstrap toàn cầu (IPFS) dùng để routing DHT.
// Lý do: DHT chỉ cần các node trung gian uy tín để tìm đường, không nhất thiết phải là node YonaCode.
// Toàn bộ địa chỉ phải đúng định dạng Multiaddr đầy đủ của IPFS (hỗ trợ cả IPv4 và IPv6).
// Tại sao cần hỗ trợ IPv6: Nhằm đảm bảo các node hoạt động trên hạ tầng mạng IPv6-only (như VPS chỉ có IPv6)
// vẫn có thể kết nối thành công tới DHT để khám phá mạng lưới mà không bị cô lập khi DNS Seed gặp sự cố.
var BootstrapPeers = []string{
	"/ip4/110.172.28.103/tcp/9000/p2p/12D3KooWQDKMMG7uKxaMvupwGoVZrDWoNj9KaSQRUe8xj7GaJuYm",
}

