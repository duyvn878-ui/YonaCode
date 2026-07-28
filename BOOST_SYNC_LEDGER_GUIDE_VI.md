# ⚡ HƯỚNG DẪN ĐỒNG BỘ SIÊU NHANH SỔ CÁI BLOCKCHAIN YONACODE ($YGO)
> **Giải pháp Tải trước Dữ liệu Sổ cái Bootstrap (Ledger Snapshot) & Lệnh CLI 1 dòng giúp Node mới bỏ qua quá trình đồng bộ lâu từ Khối 0**

Tài liệu này hướng dẫn chi tiết cách sử dụng **lệnh CLI 1 dòng tự động** hoặc tải trực tiếp bản chụp dữ liệu sổ cái đã nén từ GitHub Releases để đồng bộ siêu tốc cho Node mới trên Windows và Linux.

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

## 💡 2. NGUYÊN LÝ HOẠT ĐỘNG CỦA BOOTSTRAP LEDGER SNAPSHOT
Khi chạy Node YonaCode lần đầu, bình thường hệ thống sẽ phải tạo và tải toàn bộ lịch sử giao dịch từ Khối 0 (Genesis Block) thông qua mạng P2P, gây tốn nhiều thời gian và băng thông.

Bằng cách tải trước dữ liệu sổ cái **Bootstrap Ledger Snapshot** (`node/scl/`), Node mới của bạn sẽ:
* ✅ Tải trực tiếp khối dữ liệu sổ cái RocksDB đã được nén sẵn từ GitHub với tốc độ cao.
* ✅ Bỏ qua 100% thời gian tạo và tải lại các khối cũ.
* ✅ Khởi chạy Node là có ngay dữ liệu lịch sử, chỉ cần tải tiếp vài khối mới nhất từ mạng P2P.

---

## 📦 3. CÁC TỆP TẢI VỀ THỦ CÔNG TỪ GITHUB RELEASES (TAG `v2.0.0`)

Bạn có thể chọn 1 trong 2 phương án tải về từ trang [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0):

### Phương án A: Tải gói cài đặt đầy đủ (Đã tích hợp sẵn `node/scl`) - Khuyên dùng
* 🪟 **Windows:** Tải file [YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip) (Bao gồm file chạy `.exe` + Thư mục dữ liệu `node/scl/`).
* 🐧 **Linux:** Tải file [YonaCode_Linux.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip) (Bao gồm file nhị phân Linux + Thư mục dữ liệu `node/scl/`).

### Phương án B: Tải tệp dữ liệu Sổ cái độc lập (Nếu bạn đã có sẵn file chạy Node)
* 🌐 **Tệp Dữ liệu Sổ cái rời:** [YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip) (Chứa duy nhất thư mục `node/scl/`).

---

## 🛠️ 4. HƯỚNG DẪN CÀI ĐẶT DỮ LIỆU THỦ CÔNG

### Đối với Windows:
1. Tải file `YonaCode_Windows.zip` (hoặc `YonaCode_Ledger_Data.zip`) từ GitHub.
2. Giải nén file ZIP ra thư mục làm việc của bạn.
3. Đảm bảo cấu trúc thư mục dạng:
   ```text
   C:\YourFolder\
   ├── YonaCode.exe
   ├── scl_server.exe
   └── node/
       └── scl/          <-- (Thư mục dữ liệu chứa các file .log, .sst)
   ```
4. Chạy `YonaCode.exe` hoặc `scl_server.exe`. Node sẽ nhận diện ngay dữ liệu trong `node/scl/` và đồng bộ tốc độ cao.

---

## 🚦 5. KIỂM TRA TRẠNG THÁI ĐỒNG BỘ
Mở màn hình điều khiển Node hoặc kiểm tra nhật ký console log. Nếu thấy thông báo:
```text
[SYNC-ENGINE] 🚀 Detected pre-packaged RocksDB state at block height #38500. Resuming fast P2P sync...
```
Hệ thống đã nhận diện dữ liệu Bootstrap thành công và sẵn sàng vận hành!
