# HƯỚNG DẪN CÀI ĐẶT DRIVER CARD ĐỒ HỌA (GPU DRIVERS GUIDE)
> Hướng dẫn xử lý sự cố cài đặt thủ công driver NVIDIA (CUDA) và AMD (ROCm/OpenCL) trên Windows và Linux khi công cụ cài đặt tự động (`yona_gpu_setup`) không thể thực thi thành công.

---

## 🛠️ PHẦN 1: CARD ĐỒ HỌA NVIDIA (CUDA TOOLKIT)

### 1. Trên Hệ Điều Hành Windows
Nếu công cụ tự động bị chặn bởi quyền quản trị hoặc tường lửa, hãy cài đặt thủ công theo trình tự:

1. **Cài đặt tự động bằng dòng lệnh CLI (Khuyên dùng)**:
   Mở PowerShell dưới quyền quản trị (Run as Administrator) và thực thi hai lệnh sau để tự động tải và cấu hình toàn bộ Driver và bộ SDK:
   ```powershell
   # Tải và cài đặt âm thầm NVIDIA GeForce Experience (để tự động cập nhật Driver đồ họa mới nhất)
   winget install --id Nvidia.GeForceExperience --silent --accept-package-agreements --accept-source-agreements

   # Tải và cài đặt âm thầm bộ thư viện NVIDIA CUDA Toolkit
   winget install --id Nvidia.CUDA --silent --accept-package-agreements --accept-source-agreements
   ```

2. **Cài đặt thủ công (Dự phòng nếu lệnh CLI gặp sự cố)**:
   - **Driver NVIDIA**: Truy cập trang chủ [NVIDIA Driver Downloads](https://www.nvidia.com/Download/index.aspx), chọn đúng dòng card đồ họa của bạn và cài đặt.
   - **CUDA Toolkit (Yêu cầu phiên bản 11.8 đến 12.x)**: Truy cập [NVIDIA CUDA Toolkit Archive](https://developer.nvidia.com/cuda-toolkit-archive). Khuyên dùng **CUDA 12.2** hoặc **CUDA 12.4** để đạt hiệu năng băm tốt nhất. Chọn `Windows` -> `x86_64` -> Tải bản `exe (local)` và cài đặt ở chế độ **Express**.

3. **Kiểm tra trạng thái**:
   - Mở Terminal (PowerShell hoặc Command Prompt) và chạy lệnh:
     ```cmd
     nvidia-smi
     ```
   - Nếu màn hình hiển thị danh sách Card đồ họa và phiên bản CUDA Driver, quá trình cài đặt đã thành công.

---

### 2. Trên Hệ Điều Hành Linux
Đảm bảo bạn chạy các lệnh sau dưới quyền `root` hoặc dùng tiền tố `sudo`.

#### A. Bản phân phối Ubuntu / Debian
```bash
# 1. Cập nhật chỉ mục gói
sudo apt-get update

# 2. Cài đặt Nvidia Driver khuyến nghị (bản 535 hoặc mới hơn)
sudo apt-get install -y nvidia-driver-535 nvidia-utils-535

# 3. Cài đặt bộ công cụ CUDA Toolkit
sudo apt-get install -y nvidia-cuda-toolkit
```

#### B. Bản phân phối Fedora / RedHat / CentOS
```bash
# 1. Thêm kho lưu trữ RPM Fusion (nếu chưa có)
sudo dnf install -y https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
                    https://mirrors.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm

# 2. Cài đặt Driver và CUDA
sudo dnf clean all
sudo dnf install -y akmod-nvidia xorg-x11-drv-nvidia-cuda cuda-toolkit
```

#### C. Bản phân phối Arch Linux / Manjaro
```bash
# Cài đặt trình điều khiển độc quyền NVIDIA và CUDA
sudo pacman -Syu --noconfirm nvidia nvidia-utils cuda
```

**⚠️ Lưu ý đặc biệt sau khi cài đặt trên Linux**: Bạn bắt buộc phải khởi động lại máy tính bằng lệnh `sudo reboot` để hệ thống tải Driver Module vào Nhân Kernel.

---

## ⚡ PHẦN 2: CARD ĐỒ HỌA AMD (ROCm / OPENCL)

### 1. Trên Hệ Điều Hành Windows
Windows hiện không hỗ trợ bộ SDK ROCm chính thức của AMD (chỉ hỗ trợ Linux). Do đó hệ thống sẽ sử dụng API **OpenCL** để chạy tính năng khai thác:

1. **Cài đặt tự động bằng dòng lệnh CLI (Khuyên dùng)**:
   Mở PowerShell dưới quyền quản trị (Run as Administrator) và chạy lệnh sau để tự động tải và cài đặt AMD Adrenalin (tích hợp sẵn OpenCL Runtime):
   ```powershell
   winget install --id AMD.Adrenalin --silent --accept-package-agreements --accept-source-agreements
   ```

2. **Cài đặt thủ công (Dự phòng nếu lệnh CLI gặp sự cố)**:
   - Tải bộ cài **AMD Software: Adrenalin Edition** tại trang chủ [AMD Drivers & Support](https://www.amd.com/en/support).
   - Khởi chạy tệp `.exe` đã tải để tự động tích hợp trình điều khiển đồ họa và driver runtime OpenCL.
   - Đảm bảo tệp `OpenCL.dll` đã tồn tại trong thư mục `C:\Windows\System32\`.

---

### 2. Trên Hệ Điều Hành Linux (Ubuntu / Debian / Arch)
AMD cung cấp bộ công cụ **ROCm** và **HIP SDK** để tối ưu hóa hiệu năng đào coin trên Linux.

#### A. Trình cài đặt chính thức của AMD (Khuyên dùng cho Ubuntu/Debian)
```bash
# 1. Tải về gói cài đặt AMDGPU từ trang chủ
wget https://repo.radeon.com/amdgpu-install/6.1.2/ubuntu/jammy/amdgpu-install_6.1.60102-1_all.deb

# 2. Cài đặt trình quản lý driver của AMD
sudo apt-get install ./amdgpu-install_6.1.60102-1_all.deb

# 3. Kích hoạt cài đặt ROCm và OpenCL runtime cho việc đào coin
sudo amdgpu-install --usecase=rocm,opencl -y
```

#### B. Cài đặt trên Arch Linux
```bash
# Cài đặt OpenCL runtime AMD và HIP SDK
sudo pacman -Syu --noconfirm opencl-amd rocm-hip-sdk clinfo
```

---

## 🔍 KIỂM TRA & KHẮC PHỤC LỖI (TROUBLESHOOTING)

### 1. Lỗi "Secure Boot" trên Linux (Không nhận Driver sau khi cài)
Nếu bạn đã cài driver thành công nhưng lệnh `nvidia-smi` hoặc `rocm-smi` báo lỗi không kết nối được với driver:
* **Nguyên nhân**: Chế độ **Secure Boot** trong BIOS ngăn chặn hệ thống nạp các module driver chưa được ký số (unsigned kernel modules).
* **Khắc phục**: 
  1. Khởi động lại máy tính, bấm phím liên tục (F2, F12, Del) để vào cài đặt BIOS/UEFI.
  2. Tìm mục **Secure Boot** và chuyển sang trạng thái **Disabled** (Vô hiệu hóa).
  3. Lưu lại (F10) và khởi động lại vào Linux.

### 2. Kiểm tra phần cứng card đồ họa rời
Sử dụng các lệnh sau để đảm bảo hệ thống đã nhận diện chính xác phần cứng:
- **Windows**: Chạy lệnh `wmic path win32_videocontroller get name`.
- **Linux**: Chạy lệnh `lspci | grep -E "VGA|3D|Display"`.
