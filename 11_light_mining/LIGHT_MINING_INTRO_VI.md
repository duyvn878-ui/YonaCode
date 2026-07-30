# Giới thiệu & Tính năng giải pháp Đào Nhẹ (Light Mining) - YonaCode

Tệp tài liệu này mô tả chi tiết về khái niệm **Đào Nhẹ (Light Mining)**, mô hình hoạt động và các tính năng công nghệ vượt trội của giải pháp phần mềm Light Mining được thiết kế cho mạng lưới blockchain YonaCode.

---

## 1. Đào Nhẹ (Light Mining) là gì?

Trong các mạng lưới blockchain Proof-of-Work (PoW) truyền thống, để thực hiện khai thác Solo (Solo Mining), thợ đào bắt buộc phải tự vận hành một **Full Node** (Nút đầy đủ) trên máy tính của mình. Việc này mang lại nhiều rào cản lớn:
* **Tốn tài nguyên phần cứng:** Full Node đòi hỏi dung lượng ổ cứng lớn để lưu trữ toàn bộ lịch sử sổ cái (Ledger), dung lượng RAM cao để duy trì mempool và CPU mạnh để xử lý đồng bộ chuỗi.
* **Băng thông mạng lớn:** Full Node phải liên tục đồng bộ dữ liệu blockchain và phát sóng giao dịch với các peer khác trong mạng lưới P2P 24/7.
* **Rủi ro bảo mật khóa riêng tư:** Để nhận phần thưởng khối (Coinbase), thợ đào phải khôi phục ví của mình (nhập 12 từ khóa bảo mật Seed Phrase) trực tiếp trên máy chủ hoặc VPS vận hành Node để Node có thể ký giao dịch Coinbase. Điều này cực kỳ nguy hiểm nếu Node VPS bị tấn công hoặc lộ quyền truy cập.

**Đào Nhẹ (Light Mining)** là một kiến trúc cải tiến giúp tách biệt hoàn toàn phần cứng khai thác (GPU) khỏi phần xử lý dữ liệu blockchain (Full Node):
> **Light Mining** cho phép thợ đào vận hành một chương trình **Local Proxy Client** siêu nhẹ trên máy đào GPU cục bộ. Chương trình này sẽ kết nối từ xa đến một VPS Full Node duy nhất được kích hoạt cờ lệnh Máy chủ hỗ trợ Đào Nhẹ (`--light-miner-server`). Thợ đào nhẹ chỉ cần cung cấp địa chỉ ví công khai (`0x...`) để nhận thưởng, hoàn toàn không cần tải dữ liệu blockchain và không cần khôi phục khóa bảo mật trên VPS.

---

## 2. Các Tính năng Nổi bật của Phần mềm

### 🛡️ Bảo mật Tuyệt đối tài sản (Zero-Private-Key Risk)
* Thợ đào nhẹ kết nối vào hệ thống chỉ cần khai báo địa chỉ ví nhận tiền công khai (`0x...`). 
* **Khóa riêng tư (Private Key) và Seed Phrase của ví thợ đào được bảo vệ 100%**, chỉ lưu ở ví cá nhân của thợ đào, không bao giờ được gửi qua internet hay lưu trữ trên VPS Node.

### 🔌 WebSocket Tunnel Push (Băng thông tối ưu & Chống DoS)
* Hệ thống loại bỏ hoàn toàn cơ chế HTTP Polling truyền thống (nơi máy đào liên tục gửi yêu cầu lấy việc mới lên VPS mỗi giây một lần, dễ gây quá tải DoS đường truyền khi có nhiều máy đào).
* Máy trạm cục bộ duy trì **duy nhất 1 kết nối WebSocket Tunnel** đến VPS Proxy Gateway. 
* Khi blockchain xuất hiện khối mới, VPS sẽ chủ động **đẩy (Push)** mẫu khối mới trực tiếp qua WebSocket về máy trạm. Máy trạm tự phục vụ GPU Miner băm local trong mạng nội bộ, giúp **giảm 99.9% lưu lượng WAN** và bảo vệ đường truyền của VPS luôn thông suốt.

### 💾 Inline RAM TTL Cleanup (Quản lý RAM thông minh)
* Trên VPS Node, các mẫu block template tùy chỉnh dành riêng cho ví của từng thợ đào nhẹ được quản lý bằng cơ chế dọn dẹp RAM tự động Inline dựa theo thời gian sống (TTL = 2 phút).
* Cơ chế này giúp VPS Node tự giải phóng bộ nhớ RAM chứa các phiên đào đã ngừng kết nối hoặc hết hạn, cho phép máy chủ chịu tải hàng trăm thợ đào kết nối đồng thời mà không lo bị tràn bộ nhớ (Out-Of-Memory).

### 📊 Web Dashboard Quản lý cao cấp & Trực quan
* Tích hợp sẵn một máy chủ Web UI cục bộ hiển thị bảng điều khiển đào trực quan:
  * **Hashrate Realtime:** Hiển thị tốc độ băm thực tế của card GPU CUDA cùng biểu đồ trực quan.
  * **Mined Blocks Counter:** Đếm số khối đã giải quyết thành công trên máy trạm.
  * **Ledger Balance Reconciler:** Truy vấn trực tiếp số dư ví thực tế từ blockchain Ledger (`/api/v1/balance/{address}`) và hiển thị số tiền khả dụng thời gian thực.
  * **Live Console Logs:** Dòng log hệ thống hiển thị chi tiết trạng thái kết nối mạng và hoạt động của trình đào GPU.

---

## 3. Sơ đồ Luồng Dữ liệu (Data Flow)

1. **Kết nối:** `Local Proxy` (Máy trạm) mở WebSocket kết nối lên `VPS Proxy` (VPS).
2. **Cấp việc:** `VPS Go Node` tạo Block Template tùy chỉnh chứa phần thưởng hướng về ví thợ đào nhẹ $\rightarrow$ đẩy qua WebSocket về bộ đệm RAM của `Local Proxy` $\rightarrow$ GPU Miner lấy mẫu khối này băm local.
3. **Nộp khối:** GPU giải được khối $\rightarrow$ gửi Nonce lên VPS $\rightarrow$ Node nạp template tương ứng trong RAM và đẩy thẳng lên Consensus Engine của Rust Core $\rightarrow$ Ghi nhận khối lên chuỗi chính và phát thưởng về ví thợ đào.
