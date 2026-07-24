# 🛠️ HƯỚNG DẪN TẠO FLIGHT SHEET CHO COIN L1 MỚI (CUSTOM MINER)
> **Tài liệu hướng dẫn thiết lập Flight Sheet cho thợ đào trên hệ điều hành HiveOS**

Hãy gửi hướng dẫn chi tiết này cho các đối tác hoặc thợ đào để họ thiết lập theo đúng các bước cấu hình sau:

---

## 📋 1. Ở GIAO DIỆN TẠO FLIGHT SHEET CHÍNH

Thiết lập các thông số cơ bản tại màn hình khởi tạo Flight Sheet chính:

* **Coin**: Gõ tên Ticker coin L1 của bạn (Ví dụ: `MYL1`) $\rightarrow$ Chọn **Create "MYL1"**.
* **Wallet**: Chọn Ví nhận coin L1 đã tạo.
* **Pool**: Chọn **Configure in miner** (Cấu hình trực tiếp trong phần mềm).
* **Miner**: Chọn **Custom** (Để nạp file chạy Custom Miner của bạn vào).

---

## ⚙️ 2. CÀI ĐẶT THAM SỐ CHI TIẾT (SETUP MINER CONFIG)

Bấm vào nút **Setup Miner Config** (Cài đặt tham số Custom) màu vàng và điền đầy đủ các thông số sau:

| Trường (Field) | Giá trị thiết lập (Setting Value) | Chi tiết cấu hình |
| :--- | :--- | :--- |
| **Miner name** | Đặt tên tùy ý (Ví dụ: `my-l1-miner`) | Tên nhận diện bộ Miner tùy chỉnh của bạn trên HiveOS |
| **Installation URL** | Dán đường dẫn link tải file nén `.tar.gz` hoặc `.zip` của bộ Miner | Link tải phần mềm đào đã build sẵn cho hệ điều hành Linux/HiveOS |
| **Hash algorithm** | Chọn thuật toán đào của L1 (nếu có) hoặc chọn `custom` | Thuật toán băm tương ứng của chuỗi khối L1 |
| **Wallet and worker template** | `%WAL%` hoặc `%WAL%.%WORKER_NAME%` | HiveOS tự động điền địa chỉ ví nhận và tên của Worker |
| **Pool URL** | Dán địa chỉ IP Node / Stratum Server của L1 của bạn | Ví dụ: `123.45.67.89:8888` hoặc `node.myl1project.com:8888` |
| **Extra config arguments** | Nhập thêm các dòng lệnh phụ nếu phần mềm của bạn yêu cầu | Truyền các tham số bổ sung (stress level, GPU tuning...) |

---

> [!TIP]
> **Hướng dẫn khởi chạy:**
> 1. Sau khi điền xong bảng cấu hình trên, click **Create Flight Sheet**.
> 2. Truy cập vào Worker (trâu đào) của bạn $\rightarrow$ Chọn tab **Flight Sheet** $\rightarrow$ Tìm Flight Sheet vừa tạo và bấm vào biểu tượng **Tên lửa** 🚀 để kích hoạt chạy đào.
