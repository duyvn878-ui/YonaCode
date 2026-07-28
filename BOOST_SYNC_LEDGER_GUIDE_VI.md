# ⚡ HƯỚNG DẪN ĐỒNG BỘ SIÊU NHANH SỔ CÁI BLOCKCHAIN YONACODE ($YGO)
> **Giải pháp Tải trước Dữ liệu Sổ cái Bootstrap (Ledger Snapshot) & Lệnh CLI 1 dòng giúp Node mới bỏ qua quá trình đồng bộ lâu từ Khối 0**

Tài liệu này hướng dẫn chi tiết từng bước cho 2 phương án: **Phương án A (Tải gói cài đặt tích hợp sẵn)** và **Phương án B (Tải tệp dữ liệu sổ cái độc lập `YonaCode_Ledger_Data.zip`)** giúp Node của bạn đồng bộ siêu tốc trên cả Windows và Linux/VPS.

---

## 🚀 1. ĐỒNG BỘ SIÊU NHANH BẰNG 1 DÒNG LỆNH CLI (DÀNH CHO LINUX / VPS)

Mở Terminal hoặc phiên SSH trên máy tính / VPS của bạn và dán **1 dòng lệnh duy nhất**:

```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/sync_ledger_fast.sh | bash
```

### Tiến trình Script xử lý tự động trong vài giây:
1. Tải bản snapshot dữ liệu sổ cái `YonaCode_Ledger_Data.zip` từ GitHub Release v2.0.0.
2. Giải nén cơ sở dữ liệu RocksDB thẳng vào `./node/scl/`.
3. Dọn dẹp file tạm và thiết lập phân quyền thực thi (`chmod -R 755 node/scl`).
4. Sẵn sàng cho Node khởi chạy và đồng bộ nối tiếp các khối mới nhất tức thì.

---

## 📦 2. DANH SÁCH TỆP TẢI VỀ THỦ CÔNG TỪ GITHUB RELEASES (TAG `v2.0.0`)

Truy cập trang [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0) và chọn tệp phù hợp với nhu cầu của bạn:

* 🪟 **[Gói A] Windows Đầy đủ:** [YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip) (Bao gồm file chạy `.exe` + Thư mục dữ liệu `node/scl/`).
* 🐧 **[Gói A] Linux Đầy đủ:** [YonaCode_Linux.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip) (Bao gồm file nhị phân Linux + Thư mục dữ liệu `node/scl/`).
* 🌐 **[Gói B] Dữ liệu Sổ cái Độc lập:** [YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip) (Chứa duy nhất thư mục `node/scl/` - Dành cho người đã có sẵn file chạy Node).

---

## 🛠️ 3. HƯỚNG DẪN BƯỚC NÀY CHO PHƯƠNG ÁN A (TẢI GÓI TÍCH HỢP ĐẦY ĐỦ)

Phương án A phù hợp với người dùng mới chưa có sẵn ứng dụng Node.

### Hướng dẫn cài đặt gói A trên Windows:
1. Tải file `YonaCode_Windows.zip`.
2. Giải nén file ZIP ra bất kỳ thư mục nào (Ví dụ: `C:\YonaCode\`).
3. Mở thư mục đã giải nén, kiểm tra đã có sẵn thư mục `node\scl` nằm cùng cấp với `YonaCode.exe`.
4. Nhấp đúp chuột vào `YonaCode.exe` hoặc `scl_server.exe` để chạy. Node sẽ đọc dữ liệu có sẵn và đồng bộ tức thì.

### Hướng dẫn cài đặt gói A trên Linux:
1. Tải file `YonaCode_Linux.zip` về máy:
   ```bash
   wget https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip
   ```
2. Giải nén file nén:
   ```bash
   unzip YonaCode_Linux.zip -d /opt/yonacode/
   cd /opt/yonacode/
   chmod +x YonaCode scl_server
   ```
3. Khởi chạy `./YonaCode` hoặc `./scl_server`.

---

## 🛠️ 4. HƯỚNG DẪN RÕ RÀNG CHI TIẾT CHO PHƯƠNG ÁN B (TẢI TỆP SỔ CÁI ĐỘC LẬP)

Phương án B dành cho người dùng **đã biên dịch Node từ mã nguồn** hoặc **đã có sẵn file chạy Node** và chỉ muốn cập nhật/thêm dữ liệu sổ cái mới nhất mà không cần tải lại phần mềm Node.

### Hướng dẫn chi tiết Phương án B trên Windows:
1. Tải tệp dữ liệu rời **[YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip)**.
2. Tìm đến thư mục chứa file chạy `YonaCode.exe` của bạn (Ví dụ: `D:\BTC\`).
3. Đóng ứng dụng `YonaCode.exe` hoặc `scl_server.exe` nếu đang chạy.
4. Giải nén nội dung tệp `YonaCode_Ledger_Data.zip` **thẳng vào thư mục chứa file chạy**.
5. Kiểm tra đường dẫn chính xác sau khi giải nén:
   ```text
   D:\BTC\                      <-- Thư mục gốc chứa file chạy Node
   ├── YonaCode.exe
   ├── scl_server.exe
   └── node\                    <-- Thư mục node được giải nén ra
       └── scl\                 <-- Thư mục chứa các tệp cơ sở dữ liệu .log, .sst
   ```
6. Khởi động lại `YonaCode.exe`. Node sẽ nhận diện ngay lập tức cơ sở dữ liệu RocksDB trong `node\scl\` và bỏ qua việc nạp lại từ Khối 0.

---

### Hướng dẫn chi tiết Phương án B trên Linux / VPS / Headless:
1. Mở Terminal / SSH tại thư mục làm việc hiện tại chứa file chạy `./YonaCode` của bạn:
   ```bash
   cd /path/to/your/yonacode/
   ```
2. Dừng tiến trình Node nếu đang chạy:
   ```bash
   pkill -f scl_server || pkill -f YonaCode
   ```
3. Tải tệp dữ liệu sổ cái rời `YonaCode_Ledger_Data.zip`:
   ```bash
   curl -sSL -o YonaCode_Ledger_Data.zip https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip
   ```
4. Giải nén đè thư mục `node/scl` vào đúng vị trí:
   ```bash
   unzip -o YonaCode_Ledger_Data.zip -d ./
   rm -f YonaCode_Ledger_Data.zip
   ```
5. Phân quyền truy cập đọc/ghi chuẩn cho cơ sở dữ liệu:
   ```bash
   chmod -R 755 node/scl
   ```
6. Khởi chạy lại Node:
   ```bash
   ./YonaCode --mining
   ```

---

## 🚦 5. KIỂM TRA TRẠNG THÁI ĐỒNG BỘ
Mở màn hình điều khiển Node hoặc kiểm tra nhật ký console log. Nếu thấy thông báo:
```text
[SYNC-ENGINE] 🚀 Detected pre-packaged RocksDB state at block height #38500. Resuming fast P2P sync...
```
Hệ thống đã nhận diện dữ liệu Bootstrap thành công và sẵn sàng vận hành!
