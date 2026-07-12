# Hướng Dẫn Cài Đặt và Khai Thác Bằng GPU trên Linux (Ubuntu/Debian) ⛏️⚡

Tài liệu này hướng dẫn chi tiết cách thiết lập môi trường driver, bộ công cụ lập trình CUDA và biên dịch dự án GPU Miner của YonaCode trên hệ điều hành Linux.

---

## ⚠️ Yêu Cầu Phần Cứng & Phần Mềm
1. **Card đồ họa (GPU):** Phải sử dụng GPU của NVIDIA (kiến trúc Maxwell, Pascal, Turing, Ampere, Ada Lovelace trở lên - ví dụ: GTX 10xx, RTX 20xx/30xx/40xx, Tesla T4/A100,...). *Không hỗ trợ GPU AMD hoặc Intel.*
2. **Hệ điều hành:** Khuyên dùng Ubuntu 20.04 LTS hoặc Ubuntu 22.04 LTS / 24.04 LTS.
3. **Quyền truy cập:** Quyền quản trị tối cao (`sudo` / `root`).

---

## 🛠️ Bước 1: Cài đặt Driver NVIDIA
Để GPU hoạt động tốt nhất và tương thích với CUDA, bạn cần cài đặt bản driver NVIDIA chính thức và ổn định nhất.

1. Cập nhật danh sách gói phần mềm của hệ thống:
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```

2. Kiểm tra các phiên bản driver NVIDIA khả dụng trên thiết bị của bạn:
   ```bash
   sudo ubuntu-drivers devices
   ```

3. Cài đặt phiên bản driver khuyên dùng (ví dụ phiên bản 535 hoặc mới hơn):
   ```bash
   sudo apt install -y nvidia-driver-535
   ```
   *(Hoặc cài đặt tự động phiên bản driver tốt nhất bằng lệnh: `sudo ubuntu-drivers install`)*

4. **Khởi động lại hệ thống** để driver có hiệu lực:
   ```bash
   sudo reboot
   ```

5. Sau khi khởi động lại, kiểm tra xem Driver đã hoạt động chính xác chưa bằng lệnh:
   ```bash
   nvidia-smi
   ```
   Nếu hiển thị bảng thông số card đồ họa NVIDIA cùng phiên bản Driver thì quá trình cài đặt Driver đã thành công.

---

## 📦 Bước 2: Cài đặt CUDA Toolkit
CUDA Toolkit chứa trình biên dịch `nvcc` dùng để biên dịch mã nguồn `.cu` của GPU Miner.

### Cách 1: Cài đặt trực tiếp qua kho ứng dụng APT (Đơn giản nhất)
```bash
sudo apt update
sudo apt install -y nvidia-cuda-toolkit
```

### Cách 2: Cài đặt bản chính thức từ NVIDIA (Khuyên dùng cho hiệu suất tối đa)
Truy cập trang chủ [NVIDIA CUDA Downloads](https://developer.nvidia.com/cuda-downloads) chọn nền tảng Linux -> x86_64 -> Ubuntu và làm theo hướng dẫn.
Ví dụ cho Ubuntu 22.04:
```bash
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt update
sudo apt install -y cuda-toolkit
```

Sau khi cài đặt xong, bạn hãy thêm đường dẫn của CUDA vào biến môi trường hệ thống trong file `~/.bashrc`:
```bash
echo 'export PATH=/usr/local/cuda/bin${PATH:+:${PATH}}' >> ~/.bashrc
echo 'export LD_LIBRARY_PATH=/usr/local/cuda/lib64${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}' >> ~/.bashrc
source ~/.bashrc
```

Kiểm tra sự tồn tại của trình biên dịch `nvcc`:
```bash
nvcc --version
```

---

## 🏗️ Bước 3: Cài đặt Dependencies & Biên dịch GPU Miner
Để biên dịch mã nguồn C++, bạn cần cài đặt CMake và bộ công cụ phát triển `build-essential`.

1. Cài đặt GCC, G++ và CMake:
   ```bash
   sudo apt update
   sudo apt install -y build-essential cmake
   ```

2. Di chuyển vào thư mục dự án chứa GPU Miner:
   ```bash
   cd 8_miner_gpu
   ```

3. Tạo thư mục biên dịch riêng biệt:
   ```bash
   mkdir -p build && cd build
   ```

4. Sinh cấu hình biên dịch bằng CMake:
   ```bash
   cmake -DCMAKE_BUILD_TYPE=Release ..
   ```

5. Tiến hành biên dịch chương trình:
   ```bash
   make -j$(nproc)
   ```
   *Sau khi quá trình hoàn thành, file chạy thực thi `yona_gpu_miner` sẽ được sinh ra ngay tại thư mục hiện tại (`build/`).*

---

## 🚀 Bước 4: Vận hành khai thác (Mining)

Mạng lưới YonaCode hỗ trợ hai hình thức khai thác bằng GPU:

### Cách 1: Tự động chạy GPU Miner cùng với Node (Khuyên dùng)
Đây là cách đơn giản nhất. Khi khởi động Node chính (`YonaCode`), bạn chỉ cần thêm cờ `--mining` và chỉ định thiết bị đào `--mining-device gpu` (hoặc `--mining-device hybrid` nếu muốn đào bằng cả CPU và GPU). Hệ thống sẽ tự động gọi tiến trình `yona_gpu_miner` chạy ngầm.

Khởi chạy bằng lệnh sau:
```bash
./YonaCode node start --mining --mining-device gpu --reward-address <VÍ_CỦA_BẠN> --miner-pin <PIN_CỦA_BẠN>
```
* **--mining**: Kích hoạt bộ máy đào PoW.
* **--mining-device**: Chọn thiết bị đào: `gpu` (chỉ dùng GPU), `hybrid` (kết hợp CPU + GPU), hoặc `cpu` (mặc định chỉ CPU).
* **--reward-address**: Địa chỉ ví nhận phần thưởng khối (32 bytes Hex).
* **--miner-pin**: PIN bảo mật của ví.

### Cách 2: Khởi chạy GPU Miner độc lập (Standalone)
Nếu bạn chạy Node ở một máy và muốn dùng GPU ở máy khác để đào, bạn có thể chạy GPU Miner độc lập và kết nối từ xa đến Node.

1. **Kiểm tra tính tương thích CUDA:**
   ```bash
   ./yona_gpu_miner --check
   ```
   *Nếu thành công, màn hình sẽ hiển thị `[CUDA-SUCCESS] CUDA is fully operational.`.*

2. **Xem hướng dẫn sử dụng CLI trợ giúp:**
   ```bash
   ./yona_gpu_miner --help
   ```

3. **Chạy kết nối đến Node từ xa:**
   ```bash
   ./yona_gpu_miner [IP_NODE] [RPC_PORT]
   ```
   * **IP_NODE**: Địa chỉ IP của YonaCode Node (Mặc định: `127.0.0.1`).
   * **RPC_PORT**: Cổng RPC của Node (Mặc định: `8080`).

   Ví dụ kết nối tới Node tại địa chỉ `192.168.1.100` cổng `8080`:
   ```bash
   ./yona_gpu_miner 192.168.1.100 8080
   ```

### 3. Vận hành chạy ngầm trong nền
Để chạy ngầm Miner dưới dạng tiến trình nền độc lập ngay cả khi tắt Terminal, bạn có thể sử dụng công cụ `screen` hoặc `nohup`:
```bash
# Cài đặt screen
sudo apt install -y screen

# Tạo một phiên làm việc mới tên là 'miner'
screen -S miner

# Chạy trình đào
./yona_gpu_miner 127.0.0.1 8080

# Nhấn tổ hợp phím 'Ctrl + A' rồi nhấn 'D' để ẩn (detach) phiên làm việc đó đi.
# Khi cần quay lại giám sát:
screen -r miner
```
