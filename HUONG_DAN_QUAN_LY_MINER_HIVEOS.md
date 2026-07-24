# 📖 HƯỚNG DẪN CÁC LỆNH QUẢN TRỊ & GIÁM SÁT MINER TRÊN HIVEOS
> **HiveOS Custom Miner Administration & Troubleshooting Reference Guide**

Tài liệu này tổng hợp toàn bộ các lệnh CLI (Command Line Interface) và đường dẫn quan trọng để quản trị, giám sát, cấu hình lại driver và gỡ lỗi (troubleshooting) hệ thống đào Solo **YonaCode GPU Miner** trên hệ điều hành **HiveOS**.

---

## ⚡ 1. CÁC LỆNH ĐIỀU KHIỂN MINER (MINER CONTROL COMMANDS)
Khi truy cập vào trâu đào thông qua **Hive Shell** hoặc kết nối **SSH**, bạn có thể sử dụng các lệnh tích hợp sẵn của HiveOS để điều khiển miner:

| Lệnh CLI | Chức năng | Mô tả chi tiết |
| :--- | :--- | :--- |
| `miner` | Xem màn hình console | Mở giao diện theo dõi trực tiếp tiến trình băm và kết xuất màn hình của miner |
| `miner start` | Khởi chạy Miner | Ra lệnh cho hệ thống khởi chạy lại miner theo Flight Sheet hiện tại |
| `miner stop` | Dừng Miner | Ngắt khẩn cấp tiến trình miner đang chạy ngầm |
| `miner restart` | Khởi động lại | Tắt miner cũ và khởi chạy lại tiến trình mới để làm sạch bộ nhớ |
| `miner log` | Xem log hệ thống | Xem log quản lý của HiveOS đối với trình Custom Miner |

---

## 🔍 2. ĐƯỜNG DẪN VÀ FILE NHẬT KÝ QUAN TRỌNG (SYSTEM PATHS & LOGS)
Để gỡ lỗi và kiểm tra trạng thái hoạt động thực tế của miner, hãy ghi nhớ các thư mục và tệp tin sau:

### 📂 Thư mục hoạt động của Miner
* **Đường dẫn vật lý:** `/hive/miners/custom/yona_gpu_miner/`
* **Nội dung thư mục:** Chứa tệp thực thi `yona_gpu_miner` và các tệp script điều khiển:
  * [`h-manifest.conf`](./10_miner_hiveos/h-manifest.conf): Tệp khai báo thông tin.
  * [`h-run.sh`](./10_miner_hiveos/h-run.sh): Tệp điều khiển tiến trình.
  * [`h-stats.sh`](./10_miner_hiveos/h-stats.sh): Tệp thu thập chỉ số.

### 📝 Xem nhật ký hoạt động (Log file)
Nhật ký băm của thợ đào được xuất trực tiếp ra thư mục log chung của HiveOS:
* **Tệp log:** `/var/log/miner/custom/yona_gpu_miner.log`
* **Lệnh xem log thời gian thực:**
  ```bash
  tail -f /var/log/miner/custom/yona_gpu_miner.log
  ```
* **Lệnh kiểm tra 50 dòng log cuối cùng:**
  ```bash
  tail -n 50 /var/log/miner/custom/yona_gpu_miner.log
  ```

---

## 🖥️ 3. GIÁM SÁT TIẾN TRÌNH & PHẦN CỨNG (PROCESS & GPU MONITORING)

### 📊 Kiểm tra trạng thái tiến trình chạy ngầm
Kiểm tra xem file thực thi có thực sự đang chạy ngầm trên CPU/GPU hay không và xem PID của nó:
```bash
ps aux | grep yona_gpu_miner
```

### 🌡️ Giám sát thông số phần cứng Card đồ họa (GPU Telemetry)
* **Đối với Card đồ họa NVIDIA:**
  Xem chi tiết mức độ sử dụng điện, nhiệt độ, tốc độ quạt và lượng VRAM bị chiếm dụng:
  ```bash
  nvidia-smi
  ```
  Để cập nhật liên tục thông số GPU sau mỗi 1 giây:
  ```bash
  watch -n 1 nvidia-smi
  ```
* **Đối với Card đồ họa AMD:**
  Xem chi tiết hoạt động của các card AMD thông qua công cụ ROCm:
  ```bash
  rocm-smi
  ```
* **Xem danh sách toàn bộ GPU được nhận diện bởi HiveOS:**
  ```bash
  gpu-detect list
  ```

---

## 🔧 4. CẬP NHẬT DRIVER GPU DỰ PHÒNG (DRIVER UPDATE FALLBACKS)

Nếu phần mềm đào yêu cầu CUDA hoặc thư viện ROCm mới hơn phiên bản sẵn có trong ảnh đĩa HiveOS của bạn, hãy chạy các lệnh sau:

### 🟢 1. Nâng cấp Driver NVIDIA
```bash
# Xem danh sách các phiên bản driver NVIDIA khả dụng trên máy chủ của HiveOS
nvidia-driver-update --list

# Nâng cấp lên phiên bản driver khuyên dùng mới nhất
nvidia-driver-update

# Cài đặt một phiên bản cụ thể (Ví dụ: phiên bản 535.113.01)
nvidia-driver-update 535.113.01
```

### 🔴 2. Nâng cấp Driver AMD (OpenCL/ROCm)
Do driver AMD liên kết chặt chẽ với kernel của hệ điều hành, cách tốt nhất là cập nhật toàn bộ HiveOS lên bản phân phối mới nhất:
```bash
# Cập nhật toàn bộ OS và Driver AMD đi kèm
selfupdate
```

---

> [!WARNING]
> **XỬ LÝ SỰ CỐ KHI HASH RATE BÁO = 0:**
> 1. Kiểm tra xem file log có cập nhật liên tục hay không bằng lệnh `tail -f`.
> 2. Nếu log hiển thị lỗi: `Failed to fetch work (Status: 503)`, hãy kiểm tra xem **Node YonaCode** của bạn (tại địa chỉ cấu hình Pool URL) đã được bật và đã đồng bộ xong dữ liệu khối hay chưa.
> 3. Kiểm tra xem tiến trình miner có bị tắt đột ngột do tràn VRAM bằng cách chạy lệnh `dmesg -T | grep -i oom` trên console.
