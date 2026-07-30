# 📑 THIẾT KẾ KIẾN TRÚC HỆ THỐNG ĐÀO NHẸ (LIGHT MINING DESIGN)
> **Tài liệu đặc tả nguyên lý vận hành của Phần mềm Đào Nhẹ và VPS Node đóng khối**

Tài liệu này ghi nhận chi tiết cơ chế hoạt động của phân hệ **Đào Nhẹ (Light Mining)** và sự phân chia vai trò giữa **Phần mềm đào nhẹ trên máy trạm** và **VPS Node đóng khối (Block Packager)**.

---

## ⚡ 1. NGUYÊN LÝ HOẠT ĐỘNG CỐT LÕI
Hệ thống Đào Nhẹ được thiết kế để giải phóng thợ đào khỏi việc vận hành Full Node cồng kềnh, nhưng vẫn bảo đảm **phần thưởng khối được chuyển thẳng 100% về địa chỉ ví của chính người đào nhẹ**, không chạy qua bể đào (Pool) và không bị giữ lại ở ví của VPS Node.

```
[Thợ đào nhẹ (Ví: 0xThoDao)]
       │
       ├─► 1. Lấy mẫu block: GET /getwork?address=0xThoDao
       │
  [VPS Node (Lấy txs từ Mempool & Tạo template cho 0xThoDao)]
       │
       ├─► 2. Trả mẫu block thực (chứa Coinbase thanh toán cho 0xThoDao)
       │
[Thợ đào nhẹ (Băm GPU tìm Nonce)]
       │
       ├─► 3. Nộp kết quả: POST /submitwork {"nonce": 12345, "address": "0xThoDao"}
       │
  [VPS Node (Lắp Nonce, đóng khối & Phát sóng P2P)]
```

---

## 🛠️ 2. PHÂN CHIA VAI TRÒ HỆ THỐNG

### A. Vai trò của Phần mềm Đào Nhẹ (Light Miner)
1. **Thiết lập ví riêng:** Người dùng truyền địa chỉ ví Ed25519 cá nhân của họ (`--wallet 0x...`) vào tham số khởi chạy của phần mềm đào nhẹ.
2. **Yêu cầu mẫu block cá nhân hóa:** Gửi yêu cầu lấy mẫu block kèm theo địa chỉ ví nhận thưởng của mình tới VPS Node:
   `GET /api/v1/pool/getwork?address=0xThoDao`
3. **Thực thi GPU CUDA:** Nhận mẫu block thực từ VPS Node, sử dụng card đồ họa GPU để giải toán băm tìm Nonce.
4. **Nộp khối hợp lệ:** Khi tìm thấy Nonce đạt độ khó của mạng, gửi Nonce kèm địa chỉ ví của mình lên VPS Node để hoàn thiện khối.

### B. Vai trò của VPS Node đóng khối (Block Packager)
1. **Gom giao dịch từ Mempool (Bắt buộc):** Khi tạo mẫu block template, VPS Node có nhiệm vụ gom đầy đủ các giao dịch đang chờ xử lý từ Mempool của nó vào block body. **Tuyệt đối không được để khối trống (empty block)** nhằm bảo đảm năng lực xử lý giao dịch của toàn mạng lưới.
2. **Dựng mẫu block cá nhân hóa:** 
   - Nhận địa chỉ ví `0xThoDao` từ thợ đào nhẹ thông qua tham số `address` của yêu cầu `getwork`.
   - Gọi Rust Core (`BuildVanguardBlockTemplate`) để sinh ra block template có giao dịch Coinbase trả thưởng **thẳng về địa chỉ ví của thợ đào nhẹ đó**.
3. **Quản lý phiên (Sessions):** Lưu trữ block template này ứng với địa chỉ ví và mã phiên (`session_id`) để đối chiếu khi thợ đào nộp kết quả.
4. **Đóng khối & Phát sóng P2P:**
   - Tiếp nhận Nonce và địa chỉ ví từ yêu cầu nộp khối của thợ đào nhẹ.
   - Ghép Nonce vào mẫu block template tương ứng để đóng gói khối hoàn chỉnh.
   - Gọi Consensus Engine (`ProcessChain`) để tích hợp khối vào chuỗi blockchain cục bộ.
   - Phát sóng (Gossip Broadcast) khối mới này sang toàn bộ các node ngang hàng trong mạng lưới P2P để cập nhật sổ cái toàn mạng.

---

## ⚠️ 3. CÁC ĐIỀU KHOẢN BẮT BUỘC KHI TRIỂN KHAI CODE

1. **Không sử dụng ví của Node:** Mẫu block template gửi về cho thợ đào nhẹ nào băm thì phải chứa giao dịch Coinbase trả tiền cho ví của thợ đào nhẹ đó. Không được dùng ví mặc định của VPS Node để đào thay thế, tránh trường hợp thợ đào nhẹ "đào hộ" cho Node.
2. **Không bỏ qua gom giao dịch:** VPS Node phải đóng gói các giao dịch thực tế từ Mempool vào khối, không được tạo các khối trống không chứa giao dịch nhằm duy trì tính ổn định của mạng lưới thanh toán.
3. **Tách biệt với Pool đào:** Đây là tính năng đào đơn từ xa thông qua trung gian đóng khối (Remote Solo Mining), không sử dụng logic chia sẻ cổ phần (shares) hay tính toán độ khó bể đào (Pool difficulty).
