/**
 * @file discovery.go
 * @brief Triển khai logic khám phá peer qua DNS và Guardian Node.
 * @details Hỗ trợ:
 *  - Truy vấn DNS TXT record cho libp2p multiaddrs.
 *  - Phân giải Guardian Node domain thành multiaddr.
 *  - Cập nhật DDNS thực tế qua Cloudflare API.
 * 
 * @author Vô Nhật Thiên (Khởi tạo)
 * @date 2026-03-18
 */

package node_p2p

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"

	"github.com/cloudflare/cloudflare-go"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	madns "github.com/multiformats/go-multiaddr-dns"
)

const DiscoveryServiceTag = "btc-genz-v13-net"

// InitDHT khởi tạo Kademlia DHT cho Node
func InitDHT(ctx context.Context, h host.Host) (*kaddht.IpfsDHT, error) {
	kdht, err := kaddht.New(ctx, h)
	if err != nil {
		return nil, err
	}

	// [VANGUARD-GLOBAL] Kết nối tới các Bootstrap Peers đại thụ để vươn vòi ra thế giới
	for _, addrStr := range BootstrapPeers {
		addr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			log.Printf("[P2P-WARN] ⚠️ Địa chỉ Bootstrap không hợp lệ: %s", addrStr)
			continue
		}
		peerinfo, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Printf("[P2P-WARN] ⚠️ Không thể trích xuất PeerInfo từ: %s", addrStr)
			continue
		}
		go h.Connect(ctx, *peerinfo)
	}

	if err = kdht.Bootstrap(ctx); err != nil {
		return nil, err
	}

	return kdht, nil
}

// [V2.2-RECOVERY] Khôi phục mDNS cho môi trường Lab/LAN
func (d *DiscoveryService) InitMdns() error {
	ser := mdns.NewMdnsService(d.Host, DiscoveryServiceTag, &MdnsHandler{Host: d.Host})
	return ser.Start()
}

type DiscoveryService struct {
	Host           host.Host
	Ctx            context.Context
	DHT            *kaddht.IpfsDHT
	CF_Token       string
	SeedDomain     string
	dbPath         string
	TriggerChan    chan struct{} // [MAINNET-RECONNECT] Kênh nhận tín hiệu kích hoạt reconnection khẩn cấp
	reconnectTimer *time.Timer   // [MAINNET-DEBOUNCE] Timer tối ưu hóa debounce tránh gửi tín hiệu dồn dập
	timerMutex     sync.Mutex    // Mutex bảo vệ reconnectTimer tránh Data Race
	isCrawling     atomic.Bool   // [ANTI-SPAM-SEEDS] Bảo vệ tiến trình tránh chạy đè nhiều luồng quét song song
	BanMgr         *BanManager   // Quản lý chặn/lọc các peer
}

func NewDiscoveryService(h host.Host, dht *kaddht.IpfsDHT, seedDomain, cfToken string, dbPath string, banMgr *BanManager) *DiscoveryService {
	d := &DiscoveryService{
		Host:        h,
		DHT:         dht,
		SeedDomain:  seedDomain,
		CF_Token:    cfToken,
		Ctx:         context.Background(),
		dbPath:      dbPath,
		TriggerChan: make(chan struct{}, 10), // Sử dụng buffer size 10 để chống tràn khi mất peer đồng loạt
		BanMgr:      banMgr,
	}

	// [MAINNET-RECONNECT] Đăng ký lắng nghe sự kiện để chủ động kết nối lại khi số lượng peer tụt sâu
	d.registerDisconnectNotifee()

	return d
}

func (d *DiscoveryService) DiscoveryLoop() {
	if d.BanMgr != nil && d.BanMgr.GetIsolationMode() == 3 {
		log.Println("[DISCOVERY] 🔒 Chế độ Cách ly Tuyệt đối (Strict Isolation): Vô hiệu hóa Discovery Loop.")
		return
	}

	if d.CF_Token != "" {
		log.Printf("[DISCOVERY] 🌟 CHẾ ĐỘ SEEDER (Guardian Mode) - Đang bảo vệ DNS hạt giống tại: %s", d.SeedDomain)
	} else {
		log.Printf("[DISCOVERY] 👤 CHẾ ĐỘ FOLLOWER (Follower Mode) - Đang truy vấn hạt giống từ: %s", d.SeedDomain)
	}
	
	resolver, _ := madns.NewResolver()
	var routingDiscovery *drouting.RoutingDiscovery
	if d.DHT != nil {
		routingDiscovery = drouting.NewRoutingDiscovery(d.DHT)
		dutil.Advertise(d.Ctx, routingDiscovery, DiscoveryServiceTag)
	} else {
		log.Println("[DISCOVERY] 🛡️ Chế độ DNS-Only: Bỏ qua DHT Advertise.")
	}

	// [V1.3] Vòng lặp tự động cập nhật DDNS & Seeding
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	// Cập nhật lần đầu ngay khi khởi động
	d.runDiscoveryCycle(resolver, routingDiscovery)

	for {
		select {
		case <-d.Ctx.Done():
			return
		case <-ticker.C:
			d.runDiscoveryCycle(resolver, routingDiscovery)
		case <-d.TriggerChan:
			log.Println("[DISCOVERY] ⚡ Kích hoạt chu kỳ khám phá khẩn cấp (Event-Driven Reconnect) do mất kết nối...")
			d.runDiscoveryCycle(resolver, routingDiscovery)
		}
	}
}

// registerDisconnectNotifee lắng nghe sự kiện ngắt kết nối của các peer trong mạng P2P
func (d *DiscoveryService) registerDisconnectNotifee() {
	d.Host.Network().Notify(&network.NotifyBundle{
		DisconnectedF: func(net network.Network, conn network.Conn) {
			// Đếm số lượng peer thực tế đang kết nối
			peers := net.Peers()
			if len(peers) < 3 {
				log.Printf("[RECOVERY] 🚨 Số lượng Peer tụt dưới ngưỡng an toàn (%d/3)!", len(peers))
				
				d.timerMutex.Lock()
				defer d.timerMutex.Unlock()
				
				// [DEBOUNCE-RECOVERY] Sử dụng Timer.Reset thay vì sinh goroutine + Sleep 1 giây.
				// Tại sao: Nếu 100 peer ngắt kết nối cùng lúc, cơ chế Reset sẽ dời cuộc hẹn chạy d.Trigger()
				// về sau 1 giây kể từ lần rớt mạng cuối cùng, loại bỏ việc tạo 100 goroutine dư thừa.
				if d.reconnectTimer != nil {
					d.reconnectTimer.Reset(1 * time.Second)
				} else {
					d.reconnectTimer = time.AfterFunc(1 * time.Second, func() {
						d.Trigger()
					})
				}
			}
		},
	})
}

// Trigger gửi tín hiệu kích hoạt chu kỳ Discovery khẩn cấp thông qua TriggerChan
func (d *DiscoveryService) Trigger() {
	select {
	case d.TriggerChan <- struct{}{}:
	default:
		// Bỏ qua nếu channel đã đầy để tránh nghẽn
	}
}

func (d *DiscoveryService) runDiscoveryCycle(resolver *madns.Resolver, routingDiscovery *drouting.RoutingDiscovery) {
	if d.BanMgr != nil && d.BanMgr.GetIsolationMode() == 3 {
		log.Println("[DISCOVERY] 🔒 Chế độ Cách ly Tuyệt đối: Bỏ qua chu kỳ khám phá DHT/DNS.")
		return
	}

	// 1. [V2.1 SATOSHI-AUTO] Tự động cập nhật ID và IP lên DNS riêng của Node
	if d.CF_Token != "" && d.SeedDomain != "" {
		publicIP, err := GetPublicIP()
		if err == nil {
			peerID := d.Host.ID().String()
			p2pPort := 9000 // Cổng mặc định làm fallback

			addrs := d.Host.Addrs()
			for _, addr := range addrs {
				addrStr := addr.String()
				// Chỉ phân tích địa chỉ chứa IP công cộng (địa chỉ do UPnP ánh xạ ngoại vi)
				if strings.Contains(addrStr, publicIP) {
					if p, err := addr.ValueForProtocol(multiaddr.P_TCP); err == nil {
						if portVal, errScan := strconv.Atoi(p); errScan == nil {
							p2pPort = portVal
							log.Printf("[NAT-UPNP-PATCH] 🎯 Phát hiện cổng ngoài thực tế do Router chỉ định: %d", p2pPort)
							break
						}
					}
				}
			}

			// [V2.2 SATOSHI-HARD-DDNS] Tự động chọn mỏ neo: Ưu tiên d.SeedDomain, mặc định GUARDIAN_NODE_DOMAIN
			targetDomain := d.SeedDomain
			if targetDomain == "" { targetDomain = GUARDIAN_NODE_DOMAIN }
			// [VANGUARD-MULTIADDR] Lấy toàn bộ địa chỉ lắng nghe để công bố
			allAddrs := d.Host.Addrs()
			UpdateDDNS(d.Ctx, d.CF_Token, targetDomain, publicIP, peerID, p2pPort, allAddrs)
		}
	}

	// 2. [V2.2 SATOSHI-HARD-PRIORITY] Phase 1: LUÔN LUÔN hỏi DNS Node đầu tiên (node.ghostcoi.com)
	nodeDNSAddr, _ := multiaddr.NewMultiaddr("/dnsaddr/" + GUARDIAN_NODE_DOMAIN)
	if addrs, err := resolver.Resolve(d.Ctx, nodeDNSAddr); err == nil {
		log.Printf("[DISCOVERY-PRIORITY] 👑 Ưu tiên vấn tin DNS Node: %s -> %d địa chỉ", GUARDIAN_NODE_DOMAIN, len(addrs))
		
		// [IPv6-PIONEER] Sắp xếp địa chỉ để IPv6 lên trước sử dụng hàm dùng chung đã được Unit Test kiểm chứng
		sortedAddrs := SortAddressesByIPv6Priority(addrs)

		for _, addr := range sortedAddrs {
			info, _ := peer.AddrInfoFromP2pAddr(addr)
			if info != nil && info.ID != d.Host.ID() {
				log.Printf("[P2P-CONNECT] 🔗 [PRIORITY] Đang thử kết nối tới: %s", addr.String())
				if err := d.Host.Connect(d.Ctx, *info); err != nil {
					log.Printf("[P2P-CONNECT] ❌ [PRIORITY] Thất bại tới %s: %v", addr.String(), err)
				} else {
					log.Printf("[P2P-CONNECT] ✅ [PRIORITY] Thành công tới %s!", addr.String())
				}
			}
		}
	}

	// 3. [V2.2 SATOSHI-HARD-PRIORITY] Phase 2: Thử Seed Node của Dự án (seed.ghostcoi.com) nếu thiếu Peer
	currentPeers := d.Host.Network().Peers()
	if len(currentPeers) < 3 {
		log.Printf("[DISCOVERY-SEED] 🌱 Hội quân tại Project Seed: %s (Peer hiện tại: %d)", DNS_SEED_DOMAIN, len(currentPeers))
		seedMaddr, _ := multiaddr.NewMultiaddr("/dnsaddr/" + DNS_SEED_DOMAIN)
		if addrs, err := resolver.Resolve(d.Ctx, seedMaddr); err == nil {
			for _, addr := range addrs {
				info, _ := peer.AddrInfoFromP2pAddr(addr)
				if info != nil && info.ID != d.Host.ID() {
					log.Printf("[P2P-CONNECT] 🌱 [SEED] Đang thử kết nối tới: %s", addr.String())
					if err := d.Host.Connect(d.Ctx, *info); err != nil {
						log.Printf("[P2P-CONNECT] ❌ [SEED] Thất bại tới %s: %v", addr.String(), err)
					} else {
						log.Printf("[P2P-CONNECT] ✅ [SEED] Thành công tới %s!", addr.String())
					}
				}
			}
		}
	}

	// [LOCAL-RECOVERY] Cứu hộ kết nối thông qua Peerstore lịch sử
	// Tại sao thiết kế như vậy: Trong môi trường thử nghiệm localhost hoặc khi DNS/DHT không hoạt động,
	// Node không thể kết nối tới IP công cộng. Quét Peerstore của chính mình và thử kết nối lại
	// tới các địa chỉ localhost/LAN cũ từng kết nối là giải pháp an toàn và nhanh nhất.
	if len(d.Host.Network().Peers()) < 3 {
		log.Printf("[DISCOVERY-RECOVERY] 🔄 Quét Peerstore lịch sử để tìm đường cứu hộ kết nối (Peers hiện tại: %d/3)...", len(d.Host.Network().Peers()))
		for _, pID := range d.Host.Peerstore().Peers() {
			if pID == d.Host.ID() {
				continue
			}
			if d.Host.Network().Connectedness(pID) != network.Connected {
				addrs := d.Host.Peerstore().Addrs(pID)
				if len(addrs) > 0 {
					info := peer.AddrInfo{
						ID:    pID,
						Addrs: addrs,
					}
					log.Printf("[DISCOVERY-RECOVERY] 🔗 Thử kết nối lại tới Peer cũ: %s (Số addrs: %d)", pID.String()[:12], len(addrs))
					go func(p peer.AddrInfo) {
						d.Host.Connect(d.Ctx, p)
					}(info)
				}
			}
		}
	}

	// 4. [V2.2] Phase 3: Chỉ dùng SeedDomain tùy chỉnh nếu có cấu hình khác
	if d.SeedDomain != "" && d.SeedDomain != GUARDIAN_NODE_DOMAIN && d.SeedDomain != DNS_SEED_DOMAIN {
		customDNSAddr, _ := multiaddr.NewMultiaddr("/dnsaddr/" + d.SeedDomain)
		if addrs, err := resolver.Resolve(d.Ctx, customDNSAddr); err == nil {
			for _, addr := range addrs {
				info, _ := peer.AddrInfoFromP2pAddr(addr)
				if info != nil && info.ID != d.Host.ID() {
					d.Host.Connect(d.Ctx, *info)
				}
			}
		}
	}

	// [V2.1 SATOSHI-PRIORITY] Lớp 3: Kích hoạt mạng lưới DHT phi tập trung (Duy trì hội quân)
	if routingDiscovery != nil {
		peerChan, err := routingDiscovery.FindPeers(d.Ctx, DiscoveryServiceTag)
		if err == nil {
			for p := range peerChan {
				if p.ID == d.Host.ID() { continue }
				if d.Host.Network().Connectedness(p.ID) != network.Connected {
					d.Host.Connect(d.Ctx, p)
				}
			}
		}
	}

	// [V1.4 INDUSTRIAL] Chạy Seed Crawler (10 phút/lần)
	// Chạy ngay lần đầu tiên để cập nhật mạng lưới tức thì
	go d.CrawlAndPublishSeeds()
}

// CrawlAndPublishSeeds quét mạng lưới và đẩy IP sống lên DNS
func (d *DiscoveryService) CrawlAndPublishSeeds() {
	// [ANTI-SPAM-SEEDS] Tránh chạy đè nếu tiến trình crawl trước đó chưa kết thúc
	// Tại sao: Các chu kỳ trigger kết nối lại diễn ra liên tục có thể kích hoạt nhiều goroutine Crawl song song, 
	// gây cạn kiệt tài nguyên mạng và vi phạm giới hạn tần suất (rate limit) của API Cloudflare.
	if !d.isCrawling.CompareAndSwap(false, true) {
		log.Println("[SEEDER-ICS] ⏳ Tiến trình Crawl khác đang chạy. Bỏ qua yêu cầu trùng lặp.")
		return
	}
	defer d.isCrawling.Store(false)

	if d.CF_Token == "" { return }
	
	log.Println("[SEEDER-ICS] 🕵️ Đang quét mạng lưới (Crawl) để tìm 50 thành viên khỏe mạnh nhất...")
	healthyIPs := make([]string, 0)
	
	// Thêm IP của chính mình (IPv4 & IPv6) vào đầu danh sách Hạt giống
	if myIP, err := GetPublicIP(); err == nil {
		healthyIPs = append(healthyIPs, myIP)
	}
	if myIPv6, err := GetPublicIPv6(); err == nil {
		healthyIPs = append(healthyIPs, myIPv6)
	}
	
	// Thu thập từ Peerstore (Ưu tiên những node đang có kết nối sống)
	peers := d.Host.Network().Peers()
	if len(peers) < 5 { // Nếu ít peers đang kết nối, lấy thêm từ Peerstore lịch sử
		peers = d.Host.Peerstore().Peers()
	}

	for _, p := range peers {
		if p == d.Host.ID() { continue }
		
		// NẾU LIBP2P BÁO ĐANG KẾT NỐI, NGHĨA LÀ NODE ĐÓ ĐANG SỐNG (KHÔNG CẦN PING LẠI TRÁNH HAIRPINNING)
		if d.Host.Network().Connectedness(p) != network.Connected {
			continue
		}

		addrs := d.Host.Peerstore().Addrs(p)
		// [BUG-FIX #1 IPv6-DUAL-STACK] Quét TẤT CẢ địa chỉ của Peer, thu thập cả IPv4 lẫn IPv6
		// Tại sao: Phiên bản cũ dùng break sau khi tìm thấy IPv4 → nhánh IPv6 không bao giờ chạy
		// được cho node Dual Stack → DNS Seeder không bao giờ đẩy bản ghi AAAA (IPv6)
		var foundIPv4, foundIPv6 bool
		for _, addr := range addrs {
			// [VANGUARD-P2P-CHECK] Chỉ thu thập địa chỉ định tuyến được (Public IP) bằng thư viện manet chuẩn
			if !manet.IsPublicAddr(addr) {
				continue
			}

			// Lấy cổng P2P thực tế từ địa chỉ thay vì hardcode 9000
			peerPort := DEFAULT_P2P_PORT
			isUDP := false
			if pStr, err := addr.ValueForProtocol(multiaddr.P_TCP); err == nil {
				if pVal, errScan := strconv.Atoi(pStr); errScan == nil {
					peerPort = pVal
				}
			} else if pStr, err := addr.ValueForProtocol(multiaddr.P_UDP); err == nil {
				if pVal, errScan := strconv.Atoi(pStr); errScan == nil {
					peerPort = pVal
					isUDP = true
				}
			}

			protoName := "TCP"
			if isUDP {
				protoName = "UDP/QUIC"
			}

			// Thu thập IPv4 (nếu chưa tìm thấy)
			if !foundIPv4 {
				if ip4, err := addr.ValueForProtocol(multiaddr.P_IP4); err == nil {
					if isPortOpen(ip4, peerPort, isUDP) {
						healthyIPs = append(healthyIPs, ip4)
						log.Printf("[SEEDER-ICS] 💎 Phát hiện Node IPv4: %s (IP: %s) — Đã xác nhận cổng P2P %d (%s) mở", p.String()[:12], ip4, peerPort, protoName)
						foundIPv4 = true
					} else {
						log.Printf("[SEEDER-ICS] ⚠️ Bỏ qua Node IPv4 %s (IP: %s) — Cổng P2P %d (%s) không mở (đằng sau NAT?)", p.String()[:12], ip4, peerPort, protoName)
					}
				}
			}
			// Thu thập IPv6 (nếu chưa tìm thấy) — KHÔNG dùng else if để quét song song
			if !foundIPv6 {
				if ip6, err := addr.ValueForProtocol(multiaddr.P_IP6); err == nil {
					if isPortOpen(ip6, peerPort, isUDP) {
						healthyIPs = append(healthyIPs, ip6)
						log.Printf("[SEEDER-ICS] 💎 Phát hiện Node IPv6: %s (IP: %s) — Đã xác nhận cổng P2P %d (%s) mở", p.String()[:12], ip6, peerPort, protoName)
						foundIPv6 = true
					} else {
						log.Printf("[SEEDER-ICS] ⚠️ Bỏ qua Node IPv6 %s (IP: %s) — Cổng P2P %d (%s) không mở", p.String()[:12], ip6, peerPort, protoName)
					}
				}
			}
			// Thoát sớm khi đã thu thập đủ cả 2 loại cho Peer này
			if foundIPv4 && foundIPv6 {
				break
			}
		}
		if len(healthyIPs) >= MAX_DNS_SEEDS { break }
	}

	if len(healthyIPs) > 0 {
		log.Printf("[SEEDER-DNS] 🔗 Đã lọc được %d Node tinh anh. Đang đẩy lên Cloudflare DNS (%s)...", len(healthyIPs), d.SeedDomain)
		UpdateSeedDNS(d.Ctx, d.CF_Token, d.SeedDomain, healthyIPs)
	}
}

// isPortOpen thực hiện TCP Ping để xác thực sức khỏe Node
// [BUG-FIX #2 IPv6-SOCKET] Sử dụng net.JoinHostPort thay vì fmt.Sprintf để xử lý đúng IPv6
// Tại sao: fmt.Sprintf("%s:%d", ipv6, port) tạo ra "2001:db8::1:9000" — Go parse sai
// vì không phân biệt được dấu ":" của IPv6 và dấu ":" phân cách port.
// Chuẩn RFC yêu cầu IPv6 phải bọc trong ngoặc vuông: "[2001:db8::1]:9000"
func isPortOpen(ip string, port int, isUDP bool) bool {
	// [UDP-QUIC-Bypass] UDP không có cơ chế bắt tay ba bước, việc mở socket thử luôn báo thành công.
	// Do đó, nếu là giao thức UDP/QUIC và node đã được kết nối ổn định với ta thì xem như cổng đang mở.
	if isUDP {
		return true
	}
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil { return false }
	conn.Close()
	return true
}

// isPublicIP kiểm tra xem IP có phải là IP công cộng (không thuộc RFC 1918)
func isPublicIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil { return false }
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() { return false }
	
	privateBlocks := []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	}
	for _, block := range privateBlocks {
		_, subnet, _ := net.ParseCIDR(block)
		if subnet.Contains(ip) { return false }
	}
	return true
}

// UpdateSeedDNS quản lý danh sách Multi-Record (A/AAAA) trên Cloudflare
func UpdateSeedDNS(ctx context.Context, apiToken string, domain string, healthyIPs []string) error {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return fmt.Errorf("tên miền hạt giống không hợp lệ: %s", domain)
	}
	zoneName := strings.Join(parts[len(parts)-2:], ".")
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil { return err }

	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil { return err }

	// 1. Phân loại IP để xử lý A và AAAA riêng biệt bằng net.ParseIP để tránh chèn mã độc (SQL/Script Injection)
	var ipv4s, ipv6s []string
	for _, ipStr := range healthyIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue // Địa chỉ IP không hợp lệ, bỏ qua để bảo mật
		}
		if ip.To4() != nil {
			ipv4s = append(ipv4s, ipStr)
		} else if ip.To16() != nil {
			ipv6s = append(ipv6s, ipStr)
		}
	}

	// 2. Cập nhật bản ghi A (IPv4)
	updateSpecificDNS(ctx, api, zoneID, domain, "A", ipv4s)
	
	// 3. Cập nhật bản ghi AAAA (IPv6)
	updateSpecificDNS(ctx, api, zoneID, domain, "AAAA", ipv6s)

	return nil
}

// updateSpecificDNS là hàm trợ giúp để quản lý Multi-Record cho một loại (A hoặc AAAA)
func updateSpecificDNS(ctx context.Context, api *cloudflare.API, zoneID string, domain string, recordType string, ips []string) {
	records, _, err := api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
		Type: recordType, Name: domain,
	})
	if err != nil { return }

	existingIPMap := make(map[string]string)
	for _, r := range records { existingIPMap[r.Content] = r.ID }

	var wg sync.WaitGroup

	// [SEEDER-DNS-PARALLEL] Sử dụng goroutines và sync.WaitGroup để song song hóa các cuộc gọi API Cloudflare.
	// Tại sao thiết kế như vậy: Mỗi yêu cầu HTTP đến Cloudflare API mất hàng trăm mili-giây, nếu thực hiện tuần tự
	// sẽ làm nghẽn tiến trình của Node một cách nghiêm trọng khi số lượng IP thay đổi nhiều.
	// Song song hóa giúp rút ngắn đáng kể thời gian cập nhật tổng thể từ O(N) xuống O(1) roundtrip.
	for _, ip := range ips {
		if _, ok := existingIPMap[ip]; !ok {
			wg.Add(1)
			go func(ipVal string) {
				defer wg.Done()
				proxied := false
				_, err := api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
					Type: recordType, Name: domain, Content: ipVal, TTL: 60, Proxied: &proxied,
				})
				if err == nil {
					log.Printf("[SEEDER] ➕ Thêm IP %s mới vào DNS %s: %s (Parallel)", recordType, domain, ipVal)
				} else {
					log.Printf("[SEEDER-WARN] ⚠️ Lỗi thêm IP %s mới vào DNS %s (%s): %v", recordType, domain, ipVal, err)
				}
			}(ip)
		}
		delete(existingIPMap, ip)
	}

	for ip, recordID := range existingIPMap {
		wg.Add(1)
		go func(ipVal string, recID string) {
			defer wg.Done()
			err := api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), recID)
			if err == nil {
				log.Printf("[SEEDER] ➖ Gỡ bỏ IP %s 'chết' khỏi DNS %s: %s (Parallel)", recordType, domain, ipVal)
			} else {
				log.Printf("[SEEDER-WARN] ⚠️ Lỗi gỡ bỏ IP %s 'chết' khỏi DNS %s (%s): %v", recordType, domain, ipVal, err)
			}
		}(ip, recordID)
	}

	wg.Wait()
}

func (d *DiscoveryService) DiscoverDNSPeers() ([]peer.AddrInfo, error) {
	domains := []string{DNS_SEED_DOMAIN, GUARDIAN_NODE_DOMAIN}
	var allPeers []peer.AddrInfo

	resolver, _ := madns.NewResolver()

	for _, domain := range domains {
		dnsMaddr, _ := multiaddr.NewMultiaddr("/dnsaddr/" + domain)
		addrs, err := resolver.Resolve(d.Ctx, dnsMaddr)
		if err != nil { continue }
		for _, addr := range addrs {
			info, err := peer.AddrInfoFromP2pAddr(addr)
			if err == nil {
				allPeers = append(allPeers, *info)
			}
		}
	}
	return allPeers, nil
}

// GetGuardianNodeAddr phân giải domain của Guardian Node thành Multiaddr
func GetGuardianNodeAddr(ctx context.Context, domain string) (multiaddr.Multiaddr, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain Guardian Node không được để trống")
	}

	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("không thể phân giải IP cho %s: %v", domain, err)
	}

	var targetIP string
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			targetIP = ipv4.String()
			break
		}
	}

	if targetIP == "" {
		targetIP = ips[0].String()
	}

	// [VÁ LỖI IPV6] Phân biệt IPv4 và IPv6 để nhét đúng /ip4/ hoặc /ip6/
	var addrStr string
	if strings.Contains(targetIP, ":") {
		addrStr = fmt.Sprintf("/ip6/%s/tcp/%d", targetIP, DEFAULT_P2P_PORT)
	} else {
		addrStr = fmt.Sprintf("/ip4/%s/tcp/%d", targetIP, DEFAULT_P2P_PORT)
	}

	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return nil, err
	}

	return addr, nil
}

var httpClient = &http.Client{
	Timeout: 5 * time.Second,
}

// Helper kiểm tra một chuỗi có phải là địa chỉ IPv4 công cộng hợp lệ hay không
func isValidPublicIPv4(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified()
}

// Helper kiểm tra một chuỗi có phải là địa chỉ IPv6 công cộng hợp lệ hay không
func isValidPublicIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To16() == nil || ip.To4() != nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

// GetPublicIP lấy địa chỉ IP công cộng IPv4 của Node hiện tại sử dụng cơ chế gọi song song (fan-out) để tối ưu thời gian chờ
// Tại sao: Nếu gọi tuần tự, khi một hoặc nhiều dịch vụ đầu tiên bị sập, node sẽ bị block 5s - 10s rất lãng phí.
// Cơ chế fan-out giúp truy vấn song song tất cả các nguồn và lấy kết quả nhanh nhất trả về ngay lập tức.
func GetPublicIP() (string, error) {
	endpoints := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com/",
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resultChan := make(chan string, len(endpoints))
	
	for _, url := range endpoints {
		go func(targetURL string) {
			req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
			if err != nil {
				return
			}
			resp, err := httpClient.Do(req)
			if err == nil {
				ip, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr == nil {
					ipStr := strings.TrimSpace(string(ip))
					if isValidPublicIPv4(ipStr) {
						select {
						case resultChan <- ipStr:
						default:
						}
					}
				}
			}
		}(url)
	}

	select {
	case ip := <-resultChan:
		return ip, nil
	case <-ctx.Done():
		return "", fmt.Errorf("tất cả các dịch vụ phân giải IP IPv4 đều thất bại hoặc hết hạn chờ (timeout)")
	}
}

// GetPublicIPv6 lấy địa chỉ IP công cộng IPv6 của Node hiện tại sử dụng cơ chế gọi song song (fan-out) để tối ưu thời gian chờ
// Tại sao: Tương tự như IPv4, gọi song song giúp thu hồi IP nhanh chóng, ngăn ngừa chặn đơ luồng discovery.
func GetPublicIPv6() (string, error) {
	endpoints := []string{
		"https://api64.ipify.org",
		"https://ident.me/",
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resultChan := make(chan string, len(endpoints))
	
	for _, url := range endpoints {
		go func(targetURL string) {
			req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
			if err != nil {
				return
			}
			resp, err := httpClient.Do(req)
			if err == nil {
				ip, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr == nil {
					ipStr := strings.TrimSpace(string(ip))
					if isValidPublicIPv6(ipStr) {
						select {
						case resultChan <- ipStr:
						default:
						}
					}
				}
			}
		}(url)
	}

	select {
	case ip := <-resultChan:
		return ip, nil
	case <-ctx.Done():
		return "", fmt.Errorf("tất cả các dịch vụ phân giải IP IPv6 đều thất bại hoặc hết hạn chờ (timeout)")
	}
}

// UpdateDDNS cập nhật IP và toàn bộ Multiaddrs của Node lên Cloudflare
func UpdateDDNS(ctx context.Context, apiToken string, domain string, publicIP string, peerID string, port int, allAddrs []multiaddr.Multiaddr) error {
	// Lớp 1: Cảnh vệ Vòng ngoài (Validate Input)
	if apiToken == "" || domain == "" || publicIP == "" {
		return fmt.Errorf("thiếu thông tin cấu hình DDNS: token=%s, domain=%s, ip=%s", 
			maskToken(apiToken), domain, publicIP)
	}

	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return fmt.Errorf("tên miền không hợp lệ để phân giải Zone: %s", domain)
	}
	zoneName := strings.Join(parts[len(parts)-2:], ".")
	log.Printf("[INFO] Đang cập nhật DDNS cho %s (Zone: %s)...", domain, zoneName)

	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return fmt.Errorf("lỗi khởi tạo Cloudflare API: %v", err)
	}

	// 1. Tìm Zone ID
	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		return fmt.Errorf("lỗi lấy Zone ID: %v", err)
	}

	// 2. Tìm DNS Record ID
	records, _, err := api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
		Type: "A",
		Name: domain,
	})
	if err != nil {
		return fmt.Errorf("lỗi lấy Record ID: %v", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("không tìm thấy DNS record cho %s", domain)
	}
	recordID := records[0].ID

	// 3. Cập nhật bản ghi A
	proxied := false
	params := cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Type:    "A",
		Name:    domain,
		Content: publicIP,
		Proxied: &proxied,
		TTL:     1, // Automatic
	}
	
	_, err = api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), params)
	if err != nil {
		log.Printf("[DDNS-WARN] ⚠️ Không thể cập nhật bản ghi A: %v", err)
	} else {
		log.Printf("[SUCCESS] Đã cập nhật thành công IP IPv4 %s cho %s", publicIP, domain)
	}

	// [VANGUARD-IPv6] Thử cập nhật bản ghi AAAA (IPv6) nếu có
	if ipv6, err := GetPublicIPv6(); err == nil {
		records6, _, _ := api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
			Type: "AAAA", Name: domain,
		})
		proxied6 := false
		if len(records6) > 0 {
			api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.UpdateDNSRecordParams{
				ID: records6[0].ID, Type: "AAAA", Name: domain, Content: ipv6, Proxied: &proxied6, TTL: 1,
			})
		} else {
			api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
				Type: "AAAA", Name: domain, Content: ipv6, Proxied: &proxied6, TTL: 60,
			})
		}
		log.Printf("[SUCCESS] Đã cập nhật thành công IP IPv6 %s cho %s", ipv6, domain)
	}

	// [V1.19 DNS-FIX] Cập nhật/Tạo bản ghi TXT cho _dnsaddr (Hỗ trợ đa địa chỉ)
	txtDomain := "_dnsaddr." + domain
	
	var txtEntries []string

	// Duyệt qua tất cả các địa chỉ mà Libp2p đang nắm giữ (Bao gồm cả cổng do UPnP tự cấp)
	for _, addr := range allAddrs {
		// CHỈ công bố những địa chỉ Public (Đã được kiểm chứng là gọi từ ngoài vào được)
		if manet.IsPublicAddr(addr) {
			addrStr := addr.String()
			
			// Thêm bản ghi TCP
			txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=%s/p2p/%s", addrStr, peerID))
			
			// Chuyển đổi TCP sang UDP để thêm bản ghi QUIC (Tối ưu tốc độ)
			if strings.Contains(addrStr, "/tcp/") {
				quicAddr := strings.Replace(addrStr, "/tcp/", "/udp/", 1) + "/quic-v1"
				txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=%s/p2p/%s", quicAddr, peerID))
			}
		}
	}

	// Lưới an toàn (Fallback): Lỡ libp2p chưa kịp nhận diện IP Public, ta ép ghép IP + Cổng UPnP đã lấy ở trên
	if len(txtEntries) == 0 {
		// Trích xuất cổng listen thực tế của ứng dụng (không phải cổng UPnP được NAT của IPv4)
		listenPort := DEFAULT_P2P_PORT
		for _, addr := range allAddrs {
			addrStr := addr.String()
			if strings.Contains(addrStr, "/ip4/0.0.0.0/tcp/") || strings.Contains(addrStr, "/ip6/::/tcp/") {
				if pStr, err := addr.ValueForProtocol(multiaddr.P_TCP); err == nil {
					if pVal, errScan := strconv.Atoi(pStr); errScan == nil {
						listenPort = pVal
						break
					}
				}
			}
		}

		txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=/ip4/%s/tcp/%d/p2p/%s", publicIP, port, peerID))
		txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=/ip4/%s/udp/%d/quic-v1/p2p/%s", publicIP, port, peerID))
		
		if publicIPv6, err := GetPublicIPv6(); err == nil && publicIPv6 != "" {
			txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=/ip6/%s/tcp/%d/p2p/%s", publicIPv6, listenPort, peerID))
			txtEntries = append(txtEntries, fmt.Sprintf("dnsaddr=/ip6/%s/udp/%d/quic-v1/p2p/%s", publicIPv6, listenPort, peerID))
		}
	}
	
	if len(txtEntries) > 1 {
		// Trong thực tế, Libp2p có thể đọc nhiều bản ghi TXT cùng tên.
		log.Printf("[SEEDER-MULTI] 💎 Đang công bố %d con đường tới Node.", len(txtEntries))
	}
	
	txtRecords, _, err := api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
		Type: "TXT",
		Name: txtDomain,
	})
	
	if err == nil && len(txtRecords) > 0 {
		// [VANGUARD-CLEANUP] Xóa toàn bộ bản ghi TXT cũ để tránh trùng lặp và rác DNS
		for _, record := range txtRecords {
			api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), record.ID)
		}
		log.Printf("[SEEDER-CLEAN] 🧹 Đã dọn dẹp %d bản ghi cũ.", len(txtRecords))
	}

	// Tạo mới toàn bộ hệ thống bản ghi TXT định danh đa con đường
	for _, content := range txtEntries {
		proxiedTxt := false
		_, err := api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
			Type: "TXT", Name: txtDomain, Content: content, TTL: 60, Proxied: &proxiedTxt,
		})
		if err != nil {
			log.Printf("[SEEDER-ERROR] ❌ Không thể tạo bản ghi %s: %v", content, err)
		}
	}
	log.Printf("[SEEDER] ➕ Đã tái thiết lập hệ thống bản ghi TXT đa con đường (%d con đường).", len(txtEntries))
	return nil
}

// maskToken làm mờ token để bảo mật log
func maskToken(token string) string {
	if len(token) < 8 {
		return "****"
	}
	return token[:4] + "...." + token[len(token)-4:]
}

// SortAddressesByIPv6Priority sắp xếp và ưu tiên các địa chỉ IPv6 lên trước để đục lỗ NAT
func SortAddressesByIPv6Priority(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var ipv6 []multiaddr.Multiaddr
	var ipv4 []multiaddr.Multiaddr
	for _, addr := range addrs {
		if _, err := addr.ValueForProtocol(multiaddr.P_IP6); err == nil {
			ipv6 = append([]multiaddr.Multiaddr{addr}, ipv6...)
		} else {
			ipv4 = append(ipv4, addr)
		}
	}
	return append(ipv6, ipv4...)
}
