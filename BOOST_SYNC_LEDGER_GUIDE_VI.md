# ⚡ HƯỚNG DẪN ĐỒNG BỘ SIÊU NHANH SỔ CÁI BLOCKCHAIN YONACODE ($YGO)
> **Giải pháp Tải trước Dữ liệu Sổ cái Bootstrap (Ledger Snapshot) & Lệnh CLI 1 dòng giúp Node mới bỏ qua quá trình đồng bộ lâu từ Khối 0**

Tài liệu này hướng dẫn chi tiết từng bước cho 2 phương án: **Phương án A (Tải gói cài đặt tích hợp sẵn đầy đủ phần mềm + dữ liệu)** và **Phương án B (Tải tệp dữ liệu sổ cái độc lập `YonaCode_Ledger_Data.zip`)** giúp Node của bạn đồng bộ siêu tốc trên cả Windows và Linux/VPS.

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

## 📦 2. DANH SÁCH TOÀN BỘ TỆP TRONG CÁC GÓI RELEASES (TAG `v2.0.0`)

Truy cập trang [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0) và chọn gói tệp phù hợp với nhu cầu của bạn:

### 🪟 Gói 1: Windows Đầy đủ (`YonaCode_Windows.zip`)
Bao gồm toàn bộ 11 tệp chương trình chạy + thư viện DLL + Thư mục cơ sở dữ liệu RocksDB `node/scl/`:

```text
YonaCode_Windows.zip
├── YonaCode.exe               <-- Chỉnh duyệt & Giao diện điều khiển Node chính
├── scl_server.exe             <-- Lõi máy chủ đồng bộ sổ cái RocksDB & P2P Engine
├── genz_miner.exe             <-- Công cụ đào CPU chuyên dụng
├── cli_yona_code.exe          <-- Công cụ dòng lệnh CLI quản trị node
├── yona_gpu_miner.exe         <-- Công cụ đào GPU (Chống ASIC Yona Hash)
├── yona_gpu_setup.exe         <-- Trình cài đặt cấu hình GPU
├── yona_wallet_server.exe     <-- Máy chủ ví tiền & Gateway giao dịch
├── btc_genz_scl.dll           <-- Thư viện Rust FFI tính toán đồng thuận
├── msvcp140.dll               <-- Thư viện runtime Microsoft C++
├── vcruntime140.dll           <-- Thư viện runtime Microsoft C++
├── vcruntime140_1.dll         <-- Thư viện runtime Microsoft C++
└── node/
    └── scl/                   <-- Cơ sở dữ liệu RocksDB (chứa các file .sst, .log, MANIFEST)
```

---

### 🐧 Gói 2: Linux Đầy đủ (`YonaCode_Linux.zip`)
Bao gồm toàn bộ 7 tệp thực thi Linux + Thư mục cơ sở dữ liệu RocksDB `node/scl/`:

```text
YonaCode_Linux.zip
├── YonaCode                 <-- Chỉnh duyệt & Giao diện điều khiển Node
├── scl_server               <-- Lõi máy chủ đồng bộ sổ cái RocksDB & P2P Engine
├── genz_miner               <-- Công cụ đào CPU
├── cli_yona_code            <-- Công cụ dòng lệnh CLI
├── yona_gpu_miner           <-- Công cụ đào GPU (Yona Hash)
├── yona_gpu_setup           <-- Trình cài đặt cấu hình GPU
├── yona_wallet_server       <-- Máy chủ ví tiền & Gateway giao dịch
└── node/
    └── scl/                 <-- Cơ sở dữ liệu RocksDB (chứa các file .sst, .log, MANIFEST)
```

---

### 🌐 Gói 3: Dữ liệu Sổ cái Độc lập (`YonaCode_Ledger_Data.zip`)
Chứa **duy nhất thư mục dữ liệu cơ sở dữ liệu `node/scl/`** (Dành cho người đã có sẵn các tệp `.exe` hoặc đã tự biên dịch từ mã nguồn):

```text
YonaCode_Ledger_Data.zip
└── node/
    └── scl/                 <-- Cơ sở dữ liệu RocksDB (chứa các file .sst, .log, MANIFEST)
```

---

## 🛠️ 3. HƯỚNG DẪN CHO PHƯƠNG ÁN A (TẢI GÓI TÍCH HỢP ĐẦY ĐỦ)

Phương án A phù hợp với người dùng mới chưa có sẵn ứng dụng Node.

### Hướng dẫn cài đặt gói A trên Windows:
1. Tải file **[YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip)**.
2. Giải nén toàn bộ tệp ZIP ra một thư mục bất kỳ (Ví dụ: `D:\BTC\`).
3. Mở thư mục đã giải nén (`D:\BTC\`), đảm bảo có đủ 11 tệp `.exe`/`.dll` và thư mục `node\scl\` nằm cùng cấp.
4. Nhấp đúp chuột vào `YonaCode.exe` hoặc `scl_server.exe` để chạy. Node sẽ tự động đọc dữ liệu có sẵn và đồng bộ nối tiếp tức thì.

### Hướng dẫn cài đặt gói A trên Linux:
1. Tải file **[YonaCode_Linux.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip)** về máy:
   ```bash
   wget https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip
   ```
2. Giải nén file nén vào thư mục cài đặt:
   ```bash
   unzip YonaCode_Linux.zip -d /opt/yonacode/
   cd /opt/yonacode/
   chmod +x YonaCode scl_server genz_miner cli_yona_code yona_gpu_miner yona_wallet_server
   ```
3. Khởi chạy `./YonaCode` hoặc `./scl_server`.

---

## 🛠️ 4. HƯỚNG DẪN CHI TIẾT CHO PHƯƠNG ÁN B (TẢI TỆP SỔ CÁI ĐỘC LẬP)

Phương án B dành cho người dùng **đã biên dịch Node từ mã nguồn** hoặc **đã có sẵn bộ tệp chạy Node** và chỉ muốn cập nhật/thêm dữ liệu sổ cái Bootstrap mà không cần tải lại toàn bộ phần mềm.

### Hướng dẫn chi tiết Phương án B trên Windows:
1. Tải tệp dữ liệu rời **[YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip)**.
2. Mở thư mục chứa bộ ứng dụng Node của bạn (Ví dụ: `D:\BTC\`).
3. Đóng ứng dụng `YonaCode.exe` hoặc `scl_server.exe` nếu đang chạy.
4. Giải nén nội dung tệp `YonaCode_Ledger_Data.zip` **thẳng vào thư mục chứa tệp chạy** (Ví dụ: `D:\BTC\`).
5. Kiểm tra cây thư mục chuẩn xác sau khi giải nén:
   ```text
   D:\BTC\                               <-- Thư mục gốc chứa các file chạy Node
   ├── YonaCode.exe
   ├── scl_server.exe
   ├── genz_miner.exe
   ├── cli_yona_code.exe
   ├── yona_gpu_miner.exe
   ├── yona_wallet_server.exe
   ├── btc_genz_scl.dll
   ├── msvcp140.dll
   ├── vcruntime140.dll
   ├── vcruntime140_1.dll
   └── node\                             <-- Thư mục node được giải nén ra
       └── scl\                          <-- Thư mục chứa các tệp RocksDB (.sst, .log, MANIFEST)
   ```
6. Khởi động lại `YonaCode.exe` hoặc `scl_server.exe`. Node sẽ nhận diện ngay lập tức dữ liệu trong `node\scl\` và tiếp tục đồng bộ P2P siêu nhanh.

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
