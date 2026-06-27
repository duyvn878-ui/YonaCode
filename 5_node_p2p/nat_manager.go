package node_p2p

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
	"github.com/jackpal/gateway"
	natpmp "github.com/jackpal/go-nat-pmp"
	"github.com/pion/stun/v3"
)

// NATManager xử lý việc đục tường lửa một cách chuyên nghiệp
// [NAT-AUDIT] Nâng cấp: Lưu trữ IP công cộng phát hiện từ STUN + Periodic Renewal
type NATManager struct {
	P2PPort  int
	PublicIP string // [NAT-AUDIT] IP công cộng phát hiện từ STUN (dùng cho DNS Seeder nếu UPnP thất bại)
}

func NewNATManager(port int) *NATManager {
	return &NATManager{P2PPort: port}
}

// StartPeriodicRenewal chạy UPnP/NAT-PMP/STUN định kỳ 30 phút để duy trì mapping trên Router.
// Tại sao: Nhiều Router tự xóa UPnP mapping sau 2-4 giờ dù TTL=0 (vĩnh viễn).
// NAT-PMP có TTL=3600 (1 giờ) nên cần gia hạn trước khi hết hạn.
func (m *NATManager) StartPeriodicRenewal(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Printf("[NAT-RENEWAL] 🔄 Gia hạn định kỳ UPnP/NAT-PMP mapping (30 phút/lần)...")
				m.StartProActiveMapping(ctx)
			}
		}
	}()
}

// StartProActiveMapping bắt đầu quá trình đục lỗ chủ động
func (m *NATManager) StartProActiveMapping(ctx context.Context) {
	log.Printf("[NAT-PRO] 🚀 Khởi động trình quản lý NAT chuyên nghiệp (Vanguard Edition)")

	// Trên Windows, tự động cấu hình Windows Defender Firewall để cho phép ứng dụng nhận gói tin mạng
	if runtime.GOOS == "windows" {
		m.tryConfigureWindowsFirewall()
	}

	internalIP := m.getInternalIP()
	if internalIP == "" {
		internalIP = "127.0.0.1"
	}

	// Tự động phát hiện IP Router (Gateway)
	gatewayIPNet, err := gateway.DiscoverGateway()
	var gatewayIP string
	if err != nil || gatewayIPNet == nil {
		gatewayIP = "192.168.1.1"
	} else {
		gatewayIP = gatewayIPNet.String()
	}

	// 1. Thử UPnP qua Multicast SSDP
	go m.tryUPnP(internalIP)

	// 2. Thử UPnP qua Unicast SSDP (Đặc biệt cho Router FPT/Viettel/VNPT chặn multicast)
	go m.tryUnicastUPnP(gatewayIP, internalIP)

	// 3. Thử NAT-PMP (Giao thức của Apple/modern routers)
	go m.tryNATPMP()

	// 4. Thử STUN (Xác định IP công cộng chuẩn P2P)
	go m.trySTUN()
}

func (m *NATManager) tryConfigureWindowsFirewall() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	log.Printf("[NAT-Firewall] 🛡️ Tự động cấu hình Windows Defender Firewall cho lõi Go tại: %s", exePath)

	// Thêm luật cho phép file thực thi hiện tại kết nối Inbound
	cmd1 := exec.Command("powershell", "-Command", fmt.Sprintf(
		"New-NetFirewallRule -DisplayName 'YonaCode Core Program' -Direction Inbound -Program '%s' -Action Allow -Enabled True -ErrorAction SilentlyContinue", exePath))
	_ = cmd1.Run()

	// Thêm luật cho phép các cổng P2P TCP/UDP nhận kết nối Inbound
	cmd2 := exec.Command("powershell", "-Command", fmt.Sprintf(
		"New-NetFirewallRule -DisplayName 'YonaCode Core Port %d TCP' -Direction Inbound -Protocol TCP -LocalPort %d -Action Allow -Enabled True -ErrorAction SilentlyContinue", m.P2PPort, m.P2PPort))
	_ = cmd2.Run()

	cmd3 := exec.Command("powershell", "-Command", fmt.Sprintf(
		"New-NetFirewallRule -DisplayName 'YonaCode Core Port %d UDP' -Direction Inbound -Protocol UDP -LocalPort %d -Action Allow -Enabled True -ErrorAction SilentlyContinue", m.P2PPort, m.P2PPort))
	_ = cmd3.Run()

	log.Printf("[NAT-Firewall] ✅ Đã gửi yêu cầu cấu hình Windows Firewall (PowerShell). (Lưu ý: Chỉ áp dụng thành công nếu bạn khởi chạy ứng dụng bằng quyền Administrator)")
}

func (m *NATManager) tryUPnP(internalIP string) {
	log.Printf("[NAT-UPnP] 🔍 Đang tìm kiếm thiết bị Gateway UPnP...")
	log.Printf("[NAT-UPnP] 🌐 IP LAN xác định để ánh xạ UPnP: %s", internalIP)

	// Khởi chạy các goroutine song song thám thính và mở cổng để tránh chặn lẫn nhau và tăng tốc độ khởi động
	go func() {
		clients, _, err := internetgateway1.NewWANIPConnection1Clients()
		if err != nil {
			log.Printf("[NAT-UPnP-IP1] ❌ Lỗi thám thính WANIPConnection1: %v", err)
			return
		}
		if len(clients) == 0 {
			log.Printf("[NAT-UPnP-IP1] ⚠️ Không tìm thấy thiết bị hỗ trợ WANIPConnection1 trên mạng LAN.")
			return
		}
		log.Printf("[NAT-UPnP-IP1] 📡 Đã phát hiện %d thiết bị WANIPConnection1. Bắt đầu ánh xạ...", len(clients))
		for _, client := range clients {
			m.performMapping(client, internalIP, "UPnP-IP1")
		}
	}()

	go func() {
		clients2, _, err := internetgateway2.NewWANIPConnection1Clients()
		if err != nil {
			log.Printf("[NAT-UPnP-IP2] ❌ Lỗi thám thính WANIPConnection1 (v2): %v", err)
			return
		}
		if len(clients2) == 0 {
			log.Printf("[NAT-UPnP-IP2] ⚠️ Không tìm thấy thiết bị hỗ trợ WANIPConnection1 (v2) trên mạng LAN.")
			return
		}
		log.Printf("[NAT-UPnP-IP2] 📡 Đã phát hiện %d thiết bị WANIPConnection1 (v2). Bắt đầu ánh xạ...", len(clients2))
		for _, client := range clients2 {
			m.performMappingV2(client, internalIP, "UPnP-IP2")
		}
	}()

	go func() {
		clients3, _, err := internetgateway2.NewWANIPConnection2Clients()
		if err != nil {
			log.Printf("[NAT-UPnP-IP2v2] ❌ Lỗi thám thính WANIPConnection2: %v", err)
			return
		}
		if len(clients3) == 0 {
			log.Printf("[NAT-UPnP-IP2v2] ⚠️ Không tìm thấy thiết bị hỗ trợ WANIPConnection2 trên mạng LAN.")
			return
		}
		log.Printf("[NAT-UPnP-IP2v2] 📡 Đã phát hiện %d thiết bị WANIPConnection2. Bắt đầu ánh xạ...", len(clients3))
		for _, client := range clients3 {
			m.performMappingIP2(client, internalIP, "UPnP-IP2v2")
		}
	}()

	go func() {
		pppClients, _, err := internetgateway1.NewWANPPPConnection1Clients()
		if err != nil {
			log.Printf("[NAT-UPnP-PPP] ❌ Lỗi thám thính WANPPPConnection1: %v", err)
			return
		}
		if len(pppClients) == 0 {
			log.Printf("[NAT-UPnP-PPP] ⚠️ Không tìm thấy thiết bị hỗ trợ WANPPPConnection1 (PPP) trên mạng LAN.")
			return
		}
		log.Printf("[NAT-UPnP-PPP] 📡 Đã phát hiện %d thiết bị WANPPPConnection1. Bắt đầu ánh xạ...", len(pppClients))
		for _, client := range pppClients {
			m.performMappingPPP(client, internalIP, "UPnP-PPP")
		}
	}()

	go func() {
		pppClients2, _, err := internetgateway2.NewWANPPPConnection1Clients()
		if err != nil {
			log.Printf("[NAT-UPnP-PPPv2] ❌ Lỗi thám thính WANPPPConnection1 (v2): %v", err)
			return
		}
		if len(pppClients2) == 0 {
			log.Printf("[NAT-UPnP-PPPv2] ⚠️ Không tìm thấy thiết bị hỗ trợ WANPPPConnection1 (PPPv2) trên mạng LAN.")
			return
		}
		log.Printf("[NAT-UPnP-PPPv2] 📡 Đã phát hiện %d thiết bị WANPPPConnection1 (PPPv2). Bắt đầu ánh xạ...", len(pppClients2))
		for _, client := range pppClients2 {
			m.performMappingPPPV2(client, internalIP, "UPnP-PPPv2")
		}
	}()
}

func (m *NATManager) performMapping(client *internetgateway1.WANIPConnection1, internalIP string, label string) {
	err := client.AddPortMapping("", uint16(m.P2PPort), "TCP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if err == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng TCP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng TCP %d: %v", label, m.P2PPort, err)
	}
	errUDP := client.AddPortMapping("", uint16(m.P2PPort), "UDP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if errUDP == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng UDP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng UDP %d: %v", label, m.P2PPort, errUDP)
	}
}

func (m *NATManager) performMappingV2(client *internetgateway2.WANIPConnection1, internalIP string, label string) {
	err := client.AddPortMapping("", uint16(m.P2PPort), "TCP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if err == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng TCP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng TCP %d: %v", label, m.P2PPort, err)
	}
	errUDP := client.AddPortMapping("", uint16(m.P2PPort), "UDP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if errUDP == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng UDP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng UDP %d: %v", label, m.P2PPort, errUDP)
	}
}

func (m *NATManager) performMappingPPP(client *internetgateway1.WANPPPConnection1, internalIP string, label string) {
	err := client.AddPortMapping("", uint16(m.P2PPort), "TCP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if err == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng TCP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng TCP %d: %v", label, m.P2PPort, err)
	}
	errUDP := client.AddPortMapping("", uint16(m.P2PPort), "UDP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if errUDP == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng UDP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng UDP %d: %v", label, m.P2PPort, errUDP)
	}
}

func (m *NATManager) performMappingPPPV2(client *internetgateway2.WANPPPConnection1, internalIP string, label string) {
	err := client.AddPortMapping("", uint16(m.P2PPort), "TCP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if err == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng TCP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng TCP %d: %v", label, m.P2PPort, err)
	}
	errUDP := client.AddPortMapping("", uint16(m.P2PPort), "UDP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if errUDP == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng UDP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng UDP %d: %v", label, m.P2PPort, errUDP)
	}
}

func (m *NATManager) performMappingIP2(client *internetgateway2.WANIPConnection2, internalIP string, label string) {
	err := client.AddPortMapping("", uint16(m.P2PPort), "TCP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if err == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng TCP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng TCP %d: %v", label, m.P2PPort, err)
	}
	errUDP := client.AddPortMapping("", uint16(m.P2PPort), "UDP", uint16(m.P2PPort), internalIP, true, "YonaCode", 0)
	if errUDP == nil {
		log.Printf("[NAT-%s] ✅ Thành công mở cổng UDP %d", label, m.P2PPort)
	} else {
		log.Printf("[NAT-%s] ❌ Lỗi mở cổng UDP %d: %v", label, m.P2PPort, errUDP)
	}
}

// tryUnicastUPnP thực hiện gửi gói tin SSDP M-SEARCH qua UDP Unicast trực tiếp tới Gateway.
// Tại sao: Các gói tin UDP Multicast thường bị Firewall Windows âm thầm drop hoặc bị định tuyến đi hướng khác
// nếu máy có nhiều card mạng (như WSL, Docker, VMware). Bằng cách gửi gói tin trực tiếp tới Gateway IP,
// Firewall Windows sẽ nhận biết đây là kết nối Outbound được phản hồi từ Gateway và cho phép gói tin trả về đi qua.
func (m *NATManager) tryUnicastUPnP(gatewayIP string, internalIP string) {
	log.Printf("[NAT-Unicast] 📡 Khởi động thám thính SSDP Unicast trực tiếp tới Gateway %s:1900...", gatewayIP)

	addr, err := net.ResolveUDPAddr("udp", gatewayIP+":1900")
	if err != nil {
		log.Printf("[NAT-Unicast] ❌ Lỗi phân giải địa chỉ Gateway UDP: %v", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("[NAT-Unicast] ❌ Lỗi thiết lập kết nối UDP Unicast tới Gateway: %v", err)
		return
	}
	defer conn.Close()

	// Danh sách các Service Type (ST) tiêu chuẩn của UPnP Internet Gateway Device (IGD)
	searchTargets := []string{
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANIPConnection:2",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
		"ssdp:all",
	}

	foundURLs := make(map[string]bool)

	for _, st := range searchTargets {
		// Đặt thời hạn timeout để khớp với thời gian MX của SSDP (3 giây)
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

		mSearch := fmt.Sprintf(
			"M-SEARCH * HTTP/1.1\r\n"+
				"HOST: 239.255.255.250:1900\r\n"+
				"MAN: \"ssdp:discover\"\r\n"+
				"MX: 3\r\n"+
				"ST: %s\r\n"+
				"\r\n", st)

		_, err = conn.Write([]byte(mSearch))
		if err != nil {
			log.Printf("[NAT-Unicast] ❌ Lỗi gửi tin M-SEARCH (%s): %v", st, err)
			continue
		}

		// Nhận dữ liệu phản hồi liên tục cho đến khi hết hạn deadline 1 giây
		buf := make([]byte, 2048)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				// Hết thời gian chờ hoặc có lỗi -> chuyển sang ST tiếp theo
				break
			}

			resp := string(buf[:n])
			lines := strings.Split(resp, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(strings.ToLower(line), "location:") {
					locStr := strings.TrimSpace(line[len("location:"):])
					if locStr != "" && !foundURLs[locStr] {
						foundURLs[locStr] = true
						log.Printf("[NAT-Unicast] 🔍 Tìm thấy URL thiết bị qua SSDP Unicast: %s", locStr)
					}
				}
			}
		}
	}

	if len(foundURLs) == 0 {
		log.Printf("[NAT-Unicast] ⚠️ Không phát hiện thiết bị UPnP nào phản hồi qua cơ chế Unicast SSDP.")
		return
	}

	// Thực hiện ánh xạ cổng cho từng URL thiết bị tìm thấy
	for rawURL := range foundURLs {
		m.mapDeviceFromURL(rawURL, internalIP)
	}
}

// mapDeviceFromURL tải cấu hình XML của thiết bị UPnP qua HTTP và đăng ký ánh xạ cổng.
// Tại sao: Bỏ qua hoàn toàn việc quét multicast rủi ro bằng cách truy cập trực tiếp URL đặc tả thiết bị
// và gọi hàm khởi tạo API tương ứng của thư viện goupnp.
func (m *NATManager) mapDeviceFromURL(rawURL string, internalIP string) {
	loc, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("[NAT-Unicast] ❌ Lỗi phân tích cú pháp URL %s: %v", rawURL, err)
		return
	}

	log.Printf("[NAT-Unicast] ⚙️ Đang tiến hành tạo client UPnP và ánh xạ từ URL: %s", rawURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Thử WANIPConnection1 (v1)
	clients1, err1 := internetgateway1.NewWANIPConnection1ClientsByURLCtx(ctx, loc)
	if err1 == nil && len(clients1) > 0 {
		log.Printf("[NAT-Unicast] 📡 Đã kết nối với %d dịch vụ WANIPConnection1 (v1)", len(clients1))
		for _, client := range clients1 {
			m.performMapping(client, internalIP, "Unicast-IP1")
		}
	}

	// 2. Thử WANIPConnection1 (v2)
	clients2, err2 := internetgateway2.NewWANIPConnection1ClientsByURLCtx(ctx, loc)
	if err2 == nil && len(clients2) > 0 {
		log.Printf("[NAT-Unicast] 📡 Đã kết nối với %d dịch vụ WANIPConnection1 (v2)", len(clients2))
		for _, client := range clients2 {
			m.performMappingV2(client, internalIP, "Unicast-IP2")
		}
	}

	// 3. Thử WANIPConnection2 (v2 - IGDv2 tiêu chuẩn mới)
	clients3, err3 := internetgateway2.NewWANIPConnection2ClientsByURLCtx(ctx, loc)
	if err3 == nil && len(clients3) > 0 {
		log.Printf("[NAT-Unicast] 📡 Đã kết nối với %d dịch vụ WANIPConnection2 (v2)", len(clients3))
		for _, client := range clients3 {
			m.performMappingIP2(client, internalIP, "Unicast-IP2v2")
		}
	}

	// 4. Thử WANPPPConnection1 (v1)
	clients4, err4 := internetgateway1.NewWANPPPConnection1ClientsByURLCtx(ctx, loc)
	if err4 == nil && len(clients4) > 0 {
		log.Printf("[NAT-Unicast] 📡 Đã kết nối với %d dịch vụ WANPPPConnection1", len(clients4))
		for _, client := range clients4 {
			m.performMappingPPP(client, internalIP, "Unicast-PPP1")
		}
	}
}

func (m *NATManager) tryNATPMP() {
	log.Printf("[NAT-PMP] 🔍 Đang thử giao thức NAT-PMP...")
	gatewayIP, err := gateway.DiscoverGateway()
	var gw net.IP
	if err != nil || gatewayIP == nil {
		log.Printf("[NAT-PMP] ⚠️ Không thể tự động phát hiện IP Router: %v. Fallback về 192.168.1.1", err)
		gw = net.ParseIP("192.168.1.1")
	} else {
		log.Printf("[NAT-PMP] 🌐 Đã phát hiện IP Router (Gateway): %s", gatewayIP.String())
		gw = gatewayIP
	}

	client := natpmp.NewClient(gw)
	_, err = client.AddPortMapping("tcp", m.P2PPort, m.P2PPort, 3600)
	if err != nil {
		log.Printf("[NAT-PMP] ❌ Router không phản hồi NAT-PMP.")
	} else {
		log.Printf("[NAT-PMP] ✅ Đã mở thành công cổng TCP %d qua NAT-PMP!", m.P2PPort)
	}
}

func (m *NATManager) trySTUN() {
	log.Printf("[NAT-STUN] 📡 Đang thám thính IP công cộng qua Google STUN Server...")
	// Dùng udp thay vì udp4 để OS tự động thương thảo và hoạt động hoàn hảo trên cả IPv6-only
	c, err := stun.Dial("udp", "stun.l.google.com:19302")
	if err != nil {
		log.Printf("[NAT-STUN] ❌ Không thể kết nối STUN Server.")
		return
	}

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if err := c.Do(message, func(res stun.Event) {
		if res.Error != nil {
			log.Printf("[NAT-STUN] ❌ Lỗi: %v", res.Error)
			return
		}
		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(res.Message); err != nil {
			log.Printf("[NAT-STUN] ❌ Lỗi lấy địa chỉ: %v", err)
			return
		}
		// [NAT-AUDIT FIX] Lưu IP công cộng vào struct thay vì chỉ in log
		// Tại sao: IP từ STUN cần được DNS Seeder và AddrsFactory sử dụng
		// khi UPnP thất bại nhưng STUN thành công (ví dụ: Symmetric NAT)
		m.PublicIP = xorAddr.IP.String()
		log.Printf("[NAT-STUN] 🌐 IP Công cộng xác định bởi STUN: %s (đã lưu vào bộ nhớ)", m.PublicIP)
	}); err != nil {
		log.Printf("[NAT-STUN] ❌ Lỗi giao thức STUN.")
	}
}

func (m *NATManager) getInternalIP() string {
	// Sử dụng cơ chế kết nối UDP ảo tới 8.8.8.8 để hệ điều hành tự động chọn
	// interface mạng chính xác kết nối với Internet.
	// Tại sao: Tránh việc lấy nhầm IP của card mạng ảo (như WSL, VirtualBox, VMware, Docker)
	// vốn không thể định tuyến UPnP trên Router thật.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		// Fallback về phương pháp duyệt danh sách interface cũ nếu không có kết nối mạng
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return ""
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
		return ""
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return localAddr.IP.String()
}
