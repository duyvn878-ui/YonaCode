# 📖 HƯỚNG DẪN SỬ DỤNG LỆNH CLI YONACODE

Tài liệu này cung cấp danh sách đầy đủ toàn bộ các lệnh CLI (Command Line Interface) và giao diện REPL tương tác của **YonaCode Go Client (v1.0)** để phục vụ quản trị, vận hành Node và ví giao dịch.

---

## 🌐 CÁC CỜ TOÀN CỤC (Global Flags)

Các cờ này có thể được đính kèm vào **bất kỳ** lệnh nào để thay đổi cấu hình thực thi chung:

| Cờ (Flag) | Giá trị mặc định | Mô tả tính năng |
| :--- | :--- | :--- |
| `--node-addr` | `localhost:18080` | Địa chỉ gRPC kết nối tới Node Server. |
| `--json` | `false` | Định dạng đầu ra (Output) dưới dạng JSON thay vì văn bản thuần túy. |
| `--lang` | `vnm` | Chọn ngôn ngữ hiển thị giao diện và log (`vnm` hoặc `eng`). |
| `--db-path` | `node` | Đường dẫn vật lý của thư mục Database của Node. |

---

## 🖥️ 1. NHÓM LỆNH QUẢN LÝ NODE (`yonacode node`)

Dùng để khởi chạy, giám sát trạng thái, kết nối mạng và thực hiện các nghiệp vụ cứu hộ, bảo trì Node ngoại tuyến.

### 🚀 Khởi chạy Node Server
```bash
./yonacode node start [cờ_hỗ_trợ]
# Hoặc: ./yonacode node run
```
* **Chức năng:** Khởi động Node Server. Node sẽ tự động quét và mở cổng P2P/gRPC nếu không được chỉ định thủ công.
* **Các cờ hỗ trợ:**
  * `--mining`: Bật trình khai thác (miner) ngay khi chạy Node.
  * `--mining-device`: Thiết bị khai thác sử dụng: `cpu` (mặc định), `gpu` (chỉ dùng card đồ họa), hoặc `hybrid` (kết hợp cả hai).
  * `--reward-address`: Địa chỉ ví nhận phần thưởng khối (Coinbase).
  * `--port`: Cổng HTTP để chạy Dashboard / Web UI.
  * `--p2p-port`: Cổng lắng nghe kết nối ngang hàng P2P.
  * `--scl-port`: Cổng kết nối gRPC tới lõi Rust Core (`scl_server`).
  * `--peers`: Danh sách IP/Multiaddr các Node khởi tạo để kết nối nhanh.
  * `--sync-mode`: Chế độ đồng bộ ban đầu: `full` (Cày cuốc) hoặc `snap` (Nhảy vọt).
  * `--max-tx-per-block`: Giới hạn tối đa số giao dịch trong một khối.
  * `--write-log`: Bật ghi log chi tiết ra đĩa cứng (file log).

### 📊 Xem nhanh Dashboard Node
```bash
./yonacode node status
```
* **Chức năng:** Hiển thị bảng điều khiển thu gọn về các chỉ số trực tiếp của máy chủ gồm: Cao độ khối hiện tại, Số lượng Peer kết nối, và Tốc độ băm CPU cục bộ (Hashrate).

### ℹ️ Xem thông tin phiên bản
```bash
./yonacode node info
```
* **Chức năng:** Hiển thị chi tiết phiên bản Client (Vanguard Edition) và các thư viện liên kết.

### 📡 Ép buộc kết nối Peer
```bash
./yonacode node connect <address>
```
* **Chức năng:** Chỉ thị Node kết nối chủ động và cưỡng chế tới một địa chỉ Peer ngang hàng cụ thể (định dạng IP/Multiaddr).

### 🔧 Công cụ bảo trì & Cứu hộ ngoại tuyến (`repair`)
> [!CAUTION]
> Các lệnh trong nhóm `repair` yêu cầu đóng Node Server trước khi thực thi để tránh xung đột đọc/ghi RocksDB.

* **Lùi lịch sử chuỗi (Rollback):**
  ```bash
  ./yonacode node repair rollback --target <height>
  ```
  * **Chức năng:** Ép buộc Node lùi (rollback) cơ sở dữ liệu về đúng cao độ khối chỉ định để khắc phục phân nhánh hoặc lỗi dữ liệu.
* **Dọn dẹp dữ liệu rác (Cleanup):**
  ```bash
  ./yonacode node repair cleanup --start <height> --end <height>
  ```
  * **Chức năng:** Quét dọn và xóa bỏ dữ liệu rác/thừa của sổ cái trong khoảng khối chỉ định.
* **Thanh tẩy Sổ cái (Purify):**
  ```bash
  ./yonacode node repair purify
  ```
  * **Chức năng:** Xóa sạch toàn bộ cấu trúc trạng thái JMT State Root và tái thiết lập (rebuild) lại toàn bộ trạng thái từ dữ liệu khối gốc.
* **Tái đồng bộ trạng thái (Resync):**
  ```bash
  ./yonacode node repair resync --data-root <path>
  ```
  * **Chức năng:** Khắc phục lỗi lệch State Root bằng cách đồng bộ lại toàn bộ trạng thái từ một thư mục dữ liệu đích.

---

## 👛 2. NHÓM LỆNH QUẢN LÝ VÍ (`yonacode wallet`)

Nhóm lệnh quản lý khóa, địa chỉ ví, số dư và thực hiện chuyển khoản an toàn.

### ➕ Tạo ví mới
```bash
./yonacode wallet create --name <tên_ví> [cờ_hỗ_trợ]
```
* **Chức năng:** Sinh một ví ngẫu nhiên mới, hiển thị Địa chỉ ví công khai và 12 từ khóa khôi phục (Seed Phrase).
* **Các cờ hỗ trợ:**
  * `--password`: Thiết lập mật khẩu/PIN để mã hóa file ví cục bộ.
  * `--passphrase`: Cụm mật khẩu bổ sung (từ thứ 13) để tăng cường bảo mật.

### 🔄 Khôi phục ví cũ
```bash
./yonacode wallet restore --seed "<12_từ_khóa>" --name <tên_ví> [cờ_hỗ_trợ]
```
* **Chức năng:** Nhập cụm 12 từ khóa khôi phục chuẩn BIP-39 để phục hồi địa chỉ ví.
* **Các cờ hỗ trợ:** `--password`, `--passphrase`.

### 📂 Liệt kê các ví cục bộ
```bash
./yonacode wallet list
```
* **Chức năng:** Quét thư mục lưu trữ ví cục bộ trên máy chủ và hiển thị danh sách các tài khoản đang khả dụng.

### 💰 Kiểm tra số dư và Nonce
```bash
./yonacode wallet balance --address <address>
```
* **Chức năng:** Truy vấn số dư khả dụng (GO) và giá trị Nonce hiện tại của một ví cụ thể qua mạng lưới.

### 💸 Chuyển tiền (Gửi GO)
```bash
./yonacode wallet send [cờ_hỗ_trợ]
```
* **Chức năng:** Ký và phát sóng giao dịch chuyển tiền. Nếu chạy không kèm cờ, CLI sẽ tự động kích hoạt **Guided UI** (giao diện dẫn đường từng bước) để nhập thông tin an toàn.
* **Các cờ hỗ trợ:**
  * `--from`: Tên tệp ví gửi.
  * `--to`: Địa chỉ ví nhận tiền.
  * `--amount`: Số lượng coin GO cần chuyển.
  * `--password`: Mật khẩu giải khóa ví gửi.
  * `--yes`: Bỏ qua bước xác nhận lại thông tin, tự động phát sóng.

### ❌ Xóa ví khỏi máy chủ
```bash
./yonacode wallet delete --address <address>
```
* **Chức năng:** Xóa file lưu trữ ví tương ứng khỏi thiết bị máy chủ (tương đương đăng xuất tài khoản).

---

## ⛏️ 3. NHÓM LỆNH KHAI THÁC (`yonacode mine`)

Nhóm lệnh điều khiển động cơ băm Proof of Work (PoW) trực tiếp từ giao diện CLI.

### ⛏️ Bắt đầu khai thác
```bash
./yonacode mine start --reward-address <address> [cờ_hỗ_trợ]
```
* **Chức năng:** Chỉ thị Node bắt đầu tiến trình đào khối mới và gửi phần thưởng Coinbase về địa chỉ chỉ định.
* **Các cờ hỗ trợ:**
  * `--threads`: Thiết lập số luồng CPU sử dụng để băm (Mặc định: 4).

### 🛑 Dừng khai thác
```bash
./yonacode mine stop
```
* **Chức năng:** Phát lệnh ngắt khẩn cấp tiến trình khai thác của thợ đào, giải phóng ngay lập tức tài nguyên CPU cho hệ thống.

### 📈 Xem trạng thái thợ đào
```bash
./yonacode mine status
```
* **Chức năng:** Kiểm tra xem thợ đào đang ở trạng thái chạy (`ACTIVE`) hay tạm dừng (`PAUSED`), đồng thời hiển thị tốc độ băm thời gian thực (Hashrate: KH/s hoặc MH/s).

### ⛏️ Các trình đào độc lập (Standalone Miners)
Bên cạnh việc ra lệnh đào qua CLI của Node, bạn có thể chạy trực tiếp các tệp thực thi thợ đào độc lập để kết nối và đóng góp năng lực băm cho Node chính:

#### 💻 1. Trình đào CPU độc lập (`genz_miner`)
* **Cách khởi chạy:**
  ```bash
  ./genz_miner --port <cổng_scl_của_node>
  ```
  *(Cổng SCL mặc định là cổng RPC + 42000, ví dụ nếu RPC port là 8080 thì cổng SCL kết nối là 50080)*

#### ⚡ 2. Trình đào GPU độc lập (`yona_gpu_miner`)
*Chú ý: Chỉ hỗ trợ card đồ họa NVIDIA tương thích CUDA.*
* **Kiểm tra tương thích CUDA:**
  ```bash
  ./yona_gpu_miner --check
  ```
* **Cách khởi chạy:**
  ```bash
  ./yona_gpu_miner [địa_chỉ_ip_node] [cổng_rpc_node]
  ```
  *(Ví dụ kết nối tới Node chạy ở local: `./yona_gpu_miner 127.0.0.1 8080`)*

---

## 🔍 4. NHÓM LỆNH TRUY VẤN SỔ CÁI (`yonacode query`)

Nhóm lệnh truy cập, đọc dữ liệu trực tiếp từ tệp cơ sở dữ liệu RocksDB (CSDL vật lý), có khả năng chạy offline không cần kết nối Node.

> [!TIP]
> Bạn có thể sử dụng cờ chung `--path <đường_dẫn>` trong nhóm lệnh này để trỏ tới thư mục RocksDB tùy ý.

* **Truy vấn thông tin Khối (Block):**
  ```bash
  ./yonacode query block <height_hoặc_hash>
  ```
  * **Chức năng:** Giải mã và in ra thông tin chi tiết của một khối thông qua chiều cao hoặc mã băm của nó.
* **Truy vấn giao dịch (Tx):**
  ```bash
  ./yonacode query tx <txid>
  ```
  * **Chức năng:** Tìm kiếm giao dịch qua mã băm giao dịch (TxID) để xem trạng thái đã được chốt (Finalized) hay chưa và chi tiết người gửi/nhận.
* **Đọc số dư Offline:**
  ```bash
  ./yonacode query balance <address>
  ```
  * **Chức năng:** Fallback tự động đọc số dư tài khoản trực tiếp từ RocksDB của máy chủ nếu Node đang ở trạng thái ngoại tuyến (Offline).
* **Kiểm toán Cán cân Kinh tế (Inflation check):**
  ```bash
  ./yonacode query supply
  ```
  * **Chức năng:** So sánh Tổng cung thực tế lưu trên CSDL với Tổng cung lý thuyết của thuật toán để phát hiện các lỗi lạm phát ngoài ý muốn.
* **Xem hàng đợi giao dịch (Mempool):**
  ```bash
  ./yonacode query mempool
  ```
  * **Chức năng:** Hiển thị danh sách toàn bộ các giao dịch chưa được xác nhận đang xếp hàng chờ đóng khối.
* **Quét địa chỉ sổ cái:**
  ```bash
  ./yonacode query scan
  ```
  * **Chức năng:** Quét toàn bộ cơ sở dữ liệu và liệt kê tất cả các địa chỉ ví có số dư lớn hơn 0 trên chuỗi.
* **Xem trạng thái cơ sở dữ liệu gốc:**
  ```bash
  ./yonacode query root
  ```
  * **Chức năng:** Xem thông tin mã băm Rễ Merkle (State Root) hiện tại và chiều cao của RocksDB.

---

## 🛠️ 5. NHÓM LỆNH TIỆN ÍCH (`yonacode util`)

Các công cụ kiểm tra tĩnh và tiện ích bổ trợ.

* **Tính toán mã băm Blake3:**
  ```bash
  ./yonacode util hash <chuỗi_ký_tự>
  ```
  * **Chức năng:** Tính toán và in ra mã băm chuẩn Blake3 của một chuỗi văn bản bất kỳ.
* **Xác thực địa chỉ ví:**
  ```bash
  ./yonacode util validateaddress <address>
  ```
  * **Chức năng:** Kiểm tra tính hợp lệ của chuỗi Hex xem có đúng định dạng địa chỉ ví YonaCode (32 bytes) hay không.

---

## 💻 6. VỎ TƯƠNG TÁC BÊN TRONG (Interactive REPL Shell)

Nếu bạn nhấp đúp trực tiếp vào tệp thực thi `yonacode.exe` (hoặc chạy `./yonacode` mà không truyền bất kỳ tham số hay lệnh con nào), hệ thống sẽ mở ra một vỏ tương tác REPL có dấu nhắc: `cli_yona_code >`

Trong màn hình giao diện dòng lệnh liên tục này, bạn có thể thực hiện nhanh các lệnh tắt sau:

* `help`: Hiển thị menu hướng dẫn tổng quát.
* `status` (hoặc `info`): Xem nhanh Dashboard trạng thái Node.
* `wallets`: Liệt kê tất cả các ví cục bộ đang lưu trữ.
* `send`: Kích hoạt trình dẫn đường chuyển tiền từng bước (Guided Send UI).
* `exit` (hoặc `quit`): Tắt an toàn các luồng dữ liệu, đóng Database và thoát chương trình.

---

## 🐧 7. HƯỚNG DẪN KHỞI CHẠY NODE TRÊN LINUX (VPS)

Để vận hành hệ thống node YonaCode Go trên môi trường Linux (ví dụ máy chủ VPS Ubuntu/Debian), hãy đảm bảo chuẩn bị đầy đủ 4 file chạy nhị phân (`YonaCode`, `scl_server`, `genz_miner`, `cli_yona_code`) nằm chung một thư mục làm việc (ví dụ: `/root/btc_node/`).

### ⚙️ Bước 1: Cấp quyền chạy cho toàn bộ các file (Quan trọng)
Khi tải file chạy lên Linux, mặc định hệ điều hành sẽ chặn quyền thực thi. Bạn bắt buộc phải chạy lệnh sau để cấp quyền:
```bash
cd /root/btc_node
chmod +x YonaCode scl_server genz_miner cli_yona_code
```

### 🚀 Bước 2: Khởi chạy Node

#### Cách 1: Khởi chạy thủ công trực tiếp (Để kiểm tra và xem log)
*Chú ý: Bạn phải `cd` vào đúng thư mục chứa các file thực thi rồi mới khởi chạy, để Go Core có thể tìm và gọi tiến trình con Rust Core (`scl_server`) nằm ngay cạnh nó.*
```bash
cd /root/btc_node
./YonaCode node start --port 8080 --p2p-port 9000 --db-path ./data
```
Nếu muốn bật trình đào ngay khi chạy node:
```bash
./YonaCode node start --port 8080 --p2p-port 9000 --db-path ./data --mining --reward-address <ví_nhận_thưởng_hex> --miner-pin <mã_pin_ví>
```

#### Cách 2: Thiết lập chạy ngầm 24/7 qua systemd (Khuyên dùng cho VPS)
Để dịch vụ tự chạy ngầm, tự động bật lại khi khởi động lại VPS hoặc khi gặp sự cố, hãy cấu hình dịch vụ systemd:

1. Tạo file cấu hình dịch vụ:
   ```bash
   nano /etc/systemd/system/yonacode-node.service
   ```
2. Dán nội dung cấu hình sau vào (nhớ thay đổi token Cloudflare và domain của bạn):
   ```ini
   [Unit]
   Description=YonaCode Genz Seed Node Service
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/root/btc_node
   ExecStart=/root/btc_node/YonaCode node start --port 8080 --p2p-port 9000 --db-path /root/btc_node/data
   # Biến môi trường Cloudflare tự động cập nhật/dọn dẹp DNS (nếu dùng chế độ Guardian)
   Environment="CF_TOKEN=3BnkeMrBaYD5bN0CO3sfsZvlb6um93NpP4yxz41v"
   Environment="SEED_DOMAIN=seed.ghostcoi.com"
   Restart=always
   RestartSec=5
   LimitNOFILE=65535

   [Install]
   WantedBy=multi-user.target
   ```
3. Nhấn `Ctrl + O` -> `Enter` để lưu, `Ctrl + X` để thoát.
4. Chạy các lệnh sau để kích hoạt chạy ngầm:
   ```bash
   systemctl daemon-reload
   systemctl enable yonacode-node.service
   systemctl start yonacode-node.service
   ```
5. Xem log chạy thời gian thực:
   ```bash
   journalctl -u yonacode-node.service -f
   ```

