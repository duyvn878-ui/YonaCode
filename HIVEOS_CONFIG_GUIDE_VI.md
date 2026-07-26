# 🚀 HƯỚNG DẪN CẤU HÌNH HIVEOS KHAI THÁC GPU SOLO - YONACODE ($YGO)
> **Tài liệu Hướng dẫn Cấu hình Flight Sheet & Tự động hóa Dòng lệnh (CLI) cho HiveOS**

Tài liệu này cung cấp hướng dẫn chi tiết bằng hình ảnh mô phỏng và dòng lệnh tự động giúp bạn cấu hình **Flight Sheet** hoặc **chạy 1 dòng lệnh duy nhất** trên **HiveOS** để đào Solo $YGO bằng Card đồ họa (GPU) kết nối trực tiếp về Node.

---

## 🖼️ 1. HÌNH ẢNH MÔ PHỎNG CẤU HÌNH FLIGHT SHEET TRÊN HIVEOS

Khi thao tác trên giao diện Web Dashboard của HiveOS, hãy tạo và điền các thông tin **chính xác 100%** theo bảng bên dưới:

### BƯỚC 1: TẠO VÍ GIẢ LẬP & KHỞI TẠO FLIGHT SHEET
1. Chọn **Wallets** $\rightarrow$ **Add Wallet**:
   * **Coin**: Điền `YGO`
   * **Address**: Điền một địa chỉ bất kỳ bắt đầu bằng `0x` (Ví dụ: `0x1111111111111111111111111111111111111111`)
   * **Name**: Điền `YGO Placeholder Wallet`
2. Chọn **Flight Sheets** $\rightarrow$ Cấu hình các trường:

| Trường (Field) | Giá trị Điền (Value) |
| :--- | :--- |
| **Coin** | Chọn **YGO** |
| **Wallet** | Chọn **YGO Placeholder Wallet** |
| **Pool** | Chọn **Configure in miner** |
| **Miner** | Chọn **Custom** (Nằm dưới cùng danh sách thợ đào) |

---

### BƯỚC 2: CẤU HÌNH CHI TIẾT TRONG "Setup Miner Config"
Nhấn vào nút **Setup Miner Config** và điền chính xác theo bản vẽ dưới đây:

```text
===========================================================================================
                          ⚙️ HIVEOS CUSTOM MINER CONFIGURATION
===========================================================================================

 [Tên thợ đào / Miner name] ---------------------------->  yona_gpu_miner

 [Đường dẫn tải về / Installation URL] ----------------->  https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip

 [Thuật toán / Hash algorithm] --------------------------->  blake3

 [Địa chỉ Pool / Pool URL] ------------------------------->  <IP_CỦA_NODE_BẠN>:8080   (Ví dụ: 110.172.28.103:8080)

 [Mẫu ví & Worker / Wallet and worker template] -------->  (ĐỂ TRỐNG - LEAVE BLANK)

 [Cấu hình bổ sung / Extra config arguments] ------------>  (ĐỂ TRỐNG - LEAVE BLANK)

===========================================================================================
```

> [!NOTE]
> **Lưu ý về Solo Mining:** Thợ đào GPU kết nối trực tiếp tới cổng gRPC của Node. Phần thưởng khối khi giải mã thành công sẽ được trả trực tiếp về ví Proposer thiết lập sẵn trên Node của bạn.

---

## ⚡ 2. TÍNH NĂNG TỰ ĐỘNG CẤU HÌNH BẰNG 1 DÒNG LỆNH CLI

Nếu bạn không muốn thao tác trên giao diện Web, hãy mở **Hive Shell Start** hoặc truy cập **SSH** trên HiveOS và dán **1 dòng lệnh duy nhất**:

```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
```
*(Thay `110.172.28.103:8080` bằng IP và Port Node của bạn).*

### Tiến trình Script xử lý tự động:
1. Khởi tạo thư mục chuẩn HiveOS: `/hive/miners/custom/yona_gpu_miner`.
2. Tải bản nén GPU Miner chính thức từ GitHub Release v2.0.0.
3. Tự động sinh file cấu hình `h-manifest.conf`, `h-run.sh`, `h-stats.sh` và thiết lập quyền chạy.
4. Kích hoạt dịch vụ đào ngầm kết nối tức thì về Node.

---

## 📊 3. KIỂM TRA & THEO DÕI NHẬT KÝ ĐÀO REALTIME

Sau khi kích hoạt tên lửa 🚀 trên Flight Sheet hoặc chạy lệnh CLI tự động, bạn có thể kiểm tra nhật ký đào realtime trên Rig bằng lệnh:

```bash
tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log
```

Khi màn hình hiển thị:
```text
[GPU-MINER] 🔨 Mining Block #38501 | Hashrate: 1.45 GH/s | Nonce: 0x4a1f...
```
Hệ thống đào GPU trên HiveOS của bạn đã đi vào vận hành ổn định 100%!
