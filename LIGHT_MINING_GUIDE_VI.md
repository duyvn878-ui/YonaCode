# 🚀 HƯỚNG DẪN ĐÀO NHẸ (LIGHT MINING) - YONACODE ($YGO)
> **Tài liệu Chi tiết về Công nghệ Remote Proxy Mining & Thuật toán Yona Hash (Height ≥ 38500)**

Tài liệu này giải thích chi tiết **Đào Nhẹ (Light Mining) là gì**, nguyên lý vận hành của kiến trúc Remote Proxy, lộ trình chuyển đổi qua thuật toán **Yona Hash (Block Height ≥ 38500)**, danh mục đầy đủ các cờ lệnh CLI và hướng dẫn khởi chạy trên Windows, Linux và HiveOS.

---

## 💡 1. ĐÀO NHẸ (LIGHT MINING) LÀ GÌ?

**Đào Nhẹ (Light Mining / Remote Proxy Mining)** là giải pháp công nghệ đào coin thế hệ mới của YonaCode ($YGO). 

Thay vì buộc máy thợ đào phải tải toàn bộ dữ liệu chuỗi khối Blockchain (hàng chục GB), vận hành Full Node tốn dung lượng ổ cứng, CPU và băng thông mạng, **Đào Nhẽ chỉ tập trung 100% công suất phần cứng vào việc giải toán băm GPU CUDA**.

### 🌟 ƯU ĐIỂM VƯỢT TRỘI CỦA ĐÀO NHẸ:
1. **Dung lượng siêu nhẹ (< 30 MB)**: Không cần tải hay đồng bộ sổ cái Blockchain. Khởi động ứng dụng là có thể đào ngay lập tức trong 2 giây.
2. **Tiết kiệm tài nguyên máy**: Không tốn dung lượng ổ đĩa SSD/HDD, không tiêu tốn RAM hệ thống hay tài nguyên CPU để lưu trữ giao dịch.
3. **Hiệu suất GPU CUDA tối đa (2.3+ GH/s)**: Toàn bộ công suất card đồ họa NVIDIA/AMD được dồn 100% cho thuật toán băm GPU CUDA.
4. **Kết nối VPS Node sản xuất an toàn**: Thợ đào nhẹ tự động kết nối và lấy mẫu khối băm từ VPS Node mạng chính (`110.172.28.103:8080`).
5. **Bảo mật phần thưởng trực tiếp**: Khi băm thành công một khối, phần thưởng $YGO được trả thẳng về địa chỉ ví Ed25519 (`0x...`) duy nhất của bạn.

---

## ⚡ 2. LỘ TRÌNH CHUYỂN ĐỔI THUẬT TOÁN "YONA HASH" (HEIGHT ≥ 38500)

> [!IMPORTANT]
> **THÔNG BÁO CHUYỂN ĐỔI THUẬT TOÁN YONA HASH:**
> Từ chiều cao khối **Block Height ≥ 38500**, mạng lưới YonaCode ($YGO) sẽ chính thức kích hoạt thuật toán băm chống ASIC thế hệ mới **Yona Hash** (kết hợp `parent_hash` và `merkle_root`).
> 
> * **Tự động chuyển đổi**: Bộ đào GPU CUDA Engine trong bản Đào Nhẹ đã được tích hợp sẵn logic chuyển đổi tự động. Khi Node phát mẫu khối Height $\ge 38500$, trình đào sẽ tự động chuyển sang hạt băm `Yona Hash` mà **không cần người dùng phải dừng hay cấu hình lại thợ đào**.

---

## 🛠️ 3. BẢNG TRA CỨU ĐẦY ĐỦ CÁC LỆNH VÀ CỜ LỆNH CLI (CLI COMMAND REFERENCE)

| Cờ lệnh (Flag) | Mô tả chi tiết | Giá trị mặc định | Ví dụ sử dụng |
| :--- | :--- | :--- | :--- |
| **`--wallet`** | Địa chỉ ví nhận thưởng $YGO chuẩn `0x...` | *(Tự động hỏi hoặc sinh ví)* | `--wallet 0x680303fe12345678...` |
| **`--node`** | IP và Port của VPS Node sản xuất mạng chính | `110.172.28.103:8080` | `--node 110.172.28.103:8080` |
| **`--lang`** | Ngôn ngữ hiển thị Terminal (`vn`, `en`, `zh`) | `vn` | `--lang en` *(Tiếng Anh)* <br> `--lang zh` *(Tiếng Trung)* |
| **`--cli`** | Ép buộc ứng dụng chạy ở chế độ Terminal dòng lệnh | `false` | `--cli` |
| **`--device`** | Thiết bị khai thác (`gpu`) | `gpu` | `--device gpu` |
| **`--port`** | Cổng máy chủ Web Dashboard UI proxy nội bộ | `18888` | `--port 18888` |
| **`--no-browser`** | Không tự động bật trình duyệt Web UI khi khởi chạy | `false` | `--no-browser` |
| **`--help`** | Hiển thị bảng trợ giúp và danh sách cờ lệnh | `false` | `--help` |

---

## 💻 4. HƯỚNG DẪN KHỞI CHẠY CHI TIẾT TRÊN WINDOWS, LINUX & HIVEOS

### A. Trên Windows:

#### Cách 1: Sử dụng Giao diện Web Dashboard (GUI)
1. Giải nén `YonaCode_Light_Mining_Windows.zip`.
2. Nhấp kép chuột vào `YonaCode_Light_Miner.exe`.
3. Trình duyệt tự động mở `http://127.0.0.1:18888` $\rightarrow$ Nhập ví $YGO $\rightarrow$ Nhấn **"KÍCH HOẠT GPU CUDA MINER"**.

#### Cách 2: Chạy Giao diện Dòng lệnh (CLI Terminal)
Mở cửa sổ Command Prompt (cmd) hoặc PowerShell:
```cmd
# Chạy dòng lệnh với ví chỉ định bằng Tiếng Việt
yona_light_miner_cli.exe --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang vn

# Chạy với IP Node tùy chỉnh và giao diện Tiếng Anh
yona_light_miner_cli.exe --cli --node 110.172.28.103:8080 --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en
```

---

### B. Trên Linux Terminal (CLI):
```bash
# Cấp quyền thực thi
chmod +x yona_light_miner_cli

# Chạy dòng lệnh với giao diện Tiếng Việt
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang vn

# Chạy dòng lệnh với giao diện Tiếng Trung (中文)
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang zh
```

---

### C. Cấu hình tự động 1-Click trên HiveOS Rig:
Truy cập SSH hoặc Hive Shell trên HiveOS Rig và dán dòng lệnh:
```bash
bash hiveos_setup.sh --node 110.172.28.103:8080
```
*hoặc qua curl:*
```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
```

---

## 📊 5. THEO DÕI LOG REALTIME

Kiểm tra nhật ký đào realtime trên Linux/HiveOS:
```bash
tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log
```

Màn hình hiển thị khi chuyển sang Yona Hash (Height $\ge 38500$):
```text
[GPU-MINER] 🔨 Mining Block #38505 (Yona Hash) | Speed: 2.31 GH/s | Nonce: 0x4a1f...
[GPU-MINER] 🏆 Success! Found valid solution for Block #38505!
```
Hệ thống Đào Nhẹ của bạn đã sẵn sàng 100% cho Yona Hash!
