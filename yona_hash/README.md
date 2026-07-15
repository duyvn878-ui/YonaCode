# Đặc tả Kỹ thuật: Thuật toán băm Yona (Yona Hash)

**Yona Hash** là thuật toán băm mật mã học được tối ưu hóa đặc biệt cho mạng lưới YonaCode. Thuật toán kế thừa cấu trúc cây Merkle siêu nhanh của Blake3 nhưng thay đổi toàn diện hàm trộn lõi $G$ (Mix Function) sang hệ hằng số dịch chuyển mới và chèn thêm khóa nhiễu định danh $Y_{key}$ nhằm vô hiệu hóa hoàn toàn các dòng máy ASIC thương mại có sẵn.

---

## 1. Tham số hằng số (Constants)
*   **Khóa nhiễu (Magic Key):** $Y_{key} = \text{0x594F4E41}$ (Đại diện cho chuỗi ký tự ASCII "YONA").
*   **Bộ hằng số xoay bit mới (New Rotation Shifts):** $R = \{17, 13, 9, 5\}$ (Thay thế cho bộ $\{16, 12, 8, 7\}$ truyền thống).

---

## 2. Hàm nén lõi (The Compression G Function)
Với $A, B, C, D$ là các từ trạng thái 32-bit (32-bit state words), và $X, Y$ là các từ thông điệp đầu vào (message words):

$$A = A + B + (X \oplus Y_{key}) \pmod{2^{32}}$$

$$D = (D \oplus A) \ggg 17$$

$$C = C + D \pmod{2^{32}}$$

$$B = (B \oplus C) \ggg 13$$

$$A = A + B + (Y \oplus Y_{key}) \pmod{2^{32}}$$

$$D = (D \oplus A) \ggg 9$$

$$C = C + D \pmod{2^{32}}$$

$$B = (B \oplus C) \ggg 5$$

*(Trong đó $\oplus$ là phép toán XOR logic, $+$ là phép cộng modulo $2^{32}$, và $\ggg$ là phép dịch xoay bit sang phải).*

---

## 3. Kiến trúc Hàm Nén Yona Compression
Hàm nén của Yona Hash nhận vào trạng thái chaining value `[u32; 8]`, khối dữ liệu đầu vào `[u32; 16]`, bộ đếm `counter` (u64), độ dài khối `block_len` (u32) và cờ `flags` (u32) để khởi tạo ma trận trạng thái 16 từ `[u32; 16]`. 

Thuật toán thực thi 7 vòng trộn (rounds). Mỗi vòng thực hiện biến đổi hàm $G$ trên các cột và các đường chéo của ma trận, sau đó hoán vị thông điệp theo bảng hoán vị chuẩn của Blake3. Kết quả đầu ra 512-bit (16 từ u32) được XOR giữa trạng thái kết thúc với các trạng thái chaining value ban đầu để đảm bảo tính lan truyền lỗi và bảo mật.
