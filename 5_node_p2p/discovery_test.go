/**
 * @file discovery_test.go
 * @brief Unit Test cho module Khám phá Peer (Discovery Services) và ưu tiên IPv6.
 * @details Kiểm tra tính chính xác của thuật toán phân chia tải và ưu tiên địa chỉ IPv6
 *          khi kết nối P2P, đảm bảo các node IPv6 luôn được ưu tiên đi trước để đục lỗ NAT.
 * 
 * @date 2026-06-02
 */

package node_p2p

import (
	"testing"
	"github.com/multiformats/go-multiaddr"
)

// TestSortAddressesByIPv6Priority kiểm tra tính đúng đắn của hàm sắp xếp ưu tiên IPv6
func TestSortAddressesByIPv6Priority(t *testing.T) {
	// Khởi tạo các địa chỉ thử nghiệm (IPv4 và IPv6) dưới dạng Multiaddr
	ipv4_1, err := multiaddr.NewMultiaddr("/ip4/192.168.1.100/tcp/9000")
	if err != nil {
		t.Fatalf("Không thể tạo IPv4 Multiaddr: %v", err)
	}

	ipv4_2, err := multiaddr.NewMultiaddr("/ip4/8.8.8.8/tcp/9001")
	if err != nil {
		t.Fatalf("Không thể tạo IPv4 Multiaddr: %v", err)
	}

	ipv6_1, err := multiaddr.NewMultiaddr("/ip6/2405:4800:102:abc::1/tcp/9000")
	if err != nil {
		t.Fatalf("Không thể tạo IPv6 Multiaddr: %v", err)
	}

	ipv6_2, err := multiaddr.NewMultiaddr("/ip6/2001:db8::ff00:42:8329/tcp/9002")
	if err != nil {
		t.Fatalf("Không thể tạo IPv6 Multiaddr: %v", err)
	}

	// Trường hợp 1: Trộn lẫn cả IPv4 và IPv6
	// Đầu vào: [IPv4_1, IPv6_1, IPv4_2, IPv6_2]
	inputAddrs := []multiaddr.Multiaddr{ipv4_1, ipv6_1, ipv4_2, ipv6_2}
	
	// Thực hiện sắp xếp theo độ ưu tiên IPv6
	sorted := SortAddressesByIPv6Priority(inputAddrs)

	// Lớp 5 kiểm soát: Đảm bảo số lượng địa chỉ trước và sau khi sắp xếp không đổi
	if len(sorted) != len(inputAddrs) {
		t.Errorf("Kích thước danh sách thay đổi! Trước: %d, Sau: %d", len(inputAddrs), len(sorted))
	}

	// Xác minh logic sắp xếp: IPv6 phải nằm ở các vị trí đầu tiên
	// Vì ta đưa IPv6 lên đầu bằng cách append(ipv6, sorted...), thứ tự IPv6 sẽ bị đảo nhưng chúng phải ở đầu.
	// Với đầu vào [ipv4_1, ipv6_1, ipv4_2, ipv6_2]:
	// - ipv4_1 -> [ipv4_1]
	// - ipv6_1 -> [ipv6_1, ipv4_1]
	// - ipv4_2 -> [ipv6_1, ipv4_1, ipv4_2]
	// - ipv6_2 -> [ipv6_2, ipv6_1, ipv4_1, ipv4_2]
	// Vậy 2 phần tử đầu phải là IPv6, 2 phần tử sau phải là IPv4.
	
	for i := 0; i < 2; i++ {
		if _, err := sorted[i].ValueForProtocol(multiaddr.P_IP6); err != nil {
			t.Errorf("Phần tử ở vị trí %d đáng nhẽ phải là IPv6, nhưng nhận được: %s", i, sorted[i].String())
		}
	}

	for i := 2; i < 4; i++ {
		if _, err := sorted[i].ValueForProtocol(multiaddr.P_IP4); err != nil {
			t.Errorf("Phần tử ở vị trí %d đáng nhẽ phải là IPv4, nhưng nhận được: %s", i, sorted[i].String())
		}
	}

	// Trường hợp 2: Danh sách chỉ toàn IPv4
	ipv4Only := []multiaddr.Multiaddr{ipv4_1, ipv4_2}
	sortedIpv4Only := SortAddressesByIPv6Priority(ipv4Only)
	for i, addr := range sortedIpv4Only {
		if addr.String() != ipv4Only[i].String() {
			t.Errorf("Thứ tự danh sách IPv4-only bị thay đổi ngoài ý muốn: vị trí %d", i)
		}
	}

	// Trường hợp 3: Danh sách chỉ toàn IPv6
	ipv6Only := []multiaddr.Multiaddr{ipv6_1, ipv6_2}
	sortedIpv6Only := SortAddressesByIPv6Priority(ipv6Only)
	// Do logic đảo thứ tự: ipv6_1 -> [ipv6_1], ipv6_2 -> [ipv6_2, ipv6_1]
	if sortedIpv6Only[0].String() != ipv6_2.String() || sortedIpv6Only[1].String() != ipv6_1.String() {
		t.Errorf("Logic đảo thứ tự IPv6 không khớp với mong đợi")
	}
}
