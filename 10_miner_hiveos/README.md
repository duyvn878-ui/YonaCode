# 🛠️ HƯỚNG DẪN TÍCH HỢP HIVEOS CUSTOM MINER SCRIPTS

Thư mục này chứa các tệp tin cấu hình và điều khiển cần thiết để tích hợp **YonaCode GPU Miner** làm một **Custom Miner** trên hệ điều hành HiveOS.

---

## 📂 1. Cấu trúc các tệp tin

| Tên tệp tin | Chức năng chính | Cơ chế hoạt động |
| :--- | :--- | :--- |
| [`h-manifest.conf`](./h-manifest.conf) | Tệp khai báo thông tin | Định nghĩa các biến môi trường cho HiveOS nhận diện tên miner (`yona_gpu_miner`), phiên bản (`1.0.0`), thuật toán (`blake3`), tên tệp chạy và đường dẫn ghi log. |
| [`h-run.sh`](./h-run.sh) | Kịch bản điều khiển chạy | Chịu trách nhiệm khởi chạy, quản lý tiến trình đào ngầm, phân tách địa chỉ IP/Cổng của Node từ Flight Sheet, quản lý xoay vòng log chống tràn đĩa, và thực hiện tắt tiến trình sạch sẽ khi nhận lệnh dừng. |
| [`h-stats.sh`](./h-stats.sh) | Bộ thu thập thông số GPU | Đo đạc tỷ lệ băm (hashrate), nhiệt độ (temp), tốc độ quạt (fan) của tất cả GPU NVIDIA & AMD lắp trên trâu đào, định dạng kết quả thành chuỗi JSON tiêu chuẩn để hiển thị lên Dashboard của HiveOS. |

---

## ⚙️ 2. Chi tiết các cơ chế kỹ thuật vận hành

### A. Quản lý tiến trình & Tín hiệu dừng (Signal Trapping trong `h-run.sh`)
HiveOS tắt miner bằng cách gửi tín hiệu `SIGTERM` hoặc `SIGINT`. Kịch bản `h-run.sh` đăng ký bẫy tín hiệu:
```bash
trap 'stop_miner' SIGTERM SIGINT SIGHUP
```
Khi nhận tín hiệu, hàm `stop_miner()` sẽ gửi lệnh kết thúc tiến trình sạch sẽ (`kill -15`) tới phần mềm đào ngầm để tránh lỗi rò rỉ bộ nhớ hoặc xung đột tài nguyên driver GPU.

### B. Giới hạn dung lượng Log (Log Rotation trong `h-run.sh`)
Để ngăn việc tệp log phình to làm đầy bộ nhớ SSD của máy đào sau nhiều ngày chạy liên tục, script tự động kiểm tra dung lượng tệp tin log (`yona_gpu_miner.log`):
* Nếu kích thước vượt quá **10MB (10,485,760 bytes)**, tiến trình sẽ tự động xóa sạch nội dung cũ và khởi tạo luồng ghi nhật ký mới.

### C. Phân tách IP & Cổng thông minh (trong `h-run.sh`)
Trường **Pool URL** điền từ Flight Sheet của HiveOS (biến `$CUSTOM_URL`) được tự động bóc tách thành địa chỉ IP (`POOL_IP`) và Cổng (`POOL_PORT`) để khớp với cấu trúc tham số của trình đào Solo:
* Nếu người dùng chỉ điền IP, cổng mặc định `8080` sẽ được áp dụng tự động.

### D. Đo lường chỉ số GPU đa nền tảng (`h-stats.sh`)
Hệ thống sử dụng các lệnh truy vấn driver phần cứng chính thức để lấy dữ liệu thời gian thực:
* **NVIDIA**: Gọi `nvidia-smi` để lấy nhiệt độ và tốc độ quạt.
* **AMD**: Gọi các lệnh đọc tệp trạng thái `/sys/class/drm/card*/device/hwmon/hwmon*/` để lấy thông số tương ứng.
* Dữ liệu băm tổng và từng card được định dạng thành đối tượng JSON chuẩn của HiveOS.

---

## 📦 3. Hướng dẫn đóng gói và Cài đặt cho thợ đào

Để tạo gói cài đặt đưa lên Flight Sheet của HiveOS:
1. Trình biên dịch sẽ đặt tệp thực thi Linux của GPU Miner (`yona_gpu_miner`) trực tiếp vào trong thư mục này.
2. Nén toàn bộ các tệp tin trong thư mục này thành tệp tin định dạng `.tar.gz` hoặc `.zip` (Ví dụ: `YonaCode_Linux.zip`).
3. Tải tệp nén này lên máy chủ lưu trữ (Ví dụ: GitHub Releases).
4. Tại ô cấu hình **Installation URL** trên Flight Sheet HiveOS, dán đường dẫn tải trực tiếp tệp nén trên để cài đặt tự động.
