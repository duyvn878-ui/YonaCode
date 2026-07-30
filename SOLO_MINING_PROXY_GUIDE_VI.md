# ⛏️ HƯỚNG DẪN KHAI THÁC SOLO QUA PROXY NODE CHUYÊN DỤNG (YONACODE)
> **Giải pháp Đào Solo Hiệu năng cao: Không cần đồng bộ Sổ cái tại máy khách — Ai đào được khối hưởng 100% phần thưởng**

Tài liệu này giải thích chi tiết cơ chế hoạt động và cách cấu hình mô hình **Đào Solo qua Proxy Node chuyên dụng** của YonaCode ($YGO). Đây là một công cụ đào chuyên biệt, giúp thợ đào không cần tốn dung lượng ổ cứng và thời gian đồng bộ blockchain mà vẫn có thể đào trực tiếp.

---

## 🚦 1. PHÂN BIỆT RÕ RÀNG: SOLO PROXY VS BỂ ĐÀO (MINING POOL)

Bạn cần phân biệt rõ mô hình này với một **Bể đào chung (Shared Mining Pool)** truyền thống:

| Đặc tính | Đào Solo qua Node Proxy (YonaCode) | Bể đào chung (Shared Pool) |
| :--- | :--- | :--- |
| **Phân chia phần thưởng** | 🏆 **Không chia đều**. Người nào tìm ra khối thì ví của người đó nhận **100% phần thưởng khối** (Block Reward). | ⚖️ Chia đều phần thưởng cho tất cả thợ đào theo tỷ lệ đóng góp năng lực băm (Hashrate/Shares). |
| **Phí dịch vụ (Fee)** | 🆓 Thường là 0% (tùy thuộc vào chủ Node chạy VPS). | 💸 Thu phí từ 1% - 3% trên tổng phần thưởng khai thác được. |
| **Đồng bộ Sổ cái** | ❌ **Không cần**. Node VPS trung tâm gánh toàn bộ dữ liệu Sổ cái 100%. | ❌ Không cần thiết lập node tại máy đào. |
| **Tính sở hữu** | 🔒 Block Template được ký trực tiếp với địa chỉ Ví của chính Bạn ngay từ khâu tính toán. | 🏢 Khối được ký với địa chỉ ví của chủ Bể đào trước, sau đó chủ bể mới chia lại tiền. |

---

## 📊 2. SƠ ĐỒ KIẾN TRÚC HOẠT ĐỘNG (WORK FLOW)

```text
[ Máy đào của Thợ đào A (Ví A) ] <── 1. Nhận bài toán cho Ví A ──┐
[ Máy đào của Thợ đào B (Ví B) ] <── 1. Nhận bài toán cho Ví B ──┼──> [ VPS NODE CHUYÊN DỤNG (Node VPS A) ] ──> 3. Phát sóng lên mạng P2P
[ Máy đào của Thợ đào C (Ví C) ] <── 1. Nhận bài toán cho Ví C ──┘     (Đã đồng bộ Sổ cái 100%)              (Ví nào tìm được khối hưởng 100%)
                                        (Gửi qua cổng Stratum/GetWork)
```

### Chi tiết các bước vận hành:
1. **Lấy bài toán (Get Work):** 
   Nhiều thợ đào từ khắp nơi trên thế giới kết nối vào VPS Node chuyên dụng (Node VPS A). Mỗi thợ đào khai báo địa chỉ ví nhận thưởng riêng của mình (Ví A, Ví B, Ví C...). VPS Node sẽ tạo ra các **Block Template cá nhân hóa** riêng biệt cho từng ví.
2. **Khai thác (Hashing Loop):** 
   Máy của thợ đào chạy thuật toán băm (hashing) để giải bài toán. 
3. **Nộp kết quả (Submit Work):** 
   Khi máy của thợ đào nào tìm thấy lời giải (Nonce) thỏa mãn độ khó của mạng lưới, nó lập tức gửi kết quả lên VPS Node. 
4. **Xác thực & Phát sóng:** 
   VPS Node kiểm tra tính hợp lệ của khối, ký và phát sóng khối đó lên mạng lưới Blockchain chính. **Ví của thợ đào tìm ra khối đó sẽ nhận 100% phần thưởng khối từ hệ thống phát hành.**

---

## 🛠️ 3. HƯỚNG DẪN CẤU HÌNH CHO THỢ ĐÀO (MINERS)

Để kết nối máy đào của bạn tới VPS Node chuyên dụng (Ví dụ VPS IP: `110.172.28.103`), chạy lệnh sau trên terminal của máy đào:

### Lệnh chạy Solo Miner (Windows / Linux):
```bash
./genz_miner --node 110.172.28.103:8080 --wallet <ĐỊA_CHỈ_VÍ_CỦA_BẠN> --device gpu
```

### Các thông số quan trọng:
* `--node 110.172.28.103:8080`: Chỉ định IP và Cổng RPC của VPS Node chuyên dụng đang duy trì sổ cái.
* `--wallet`: Địa chỉ ví YonaCode của chính bạn. **Tuyệt đối không lấy ví của người khác, vì người tìm được khối sẽ hưởng toàn bộ phần thưởng gửi thẳng về ví này.**
* `--device gpu`: Khai thác bằng Card đồ họa NVIDIA (CUDA) để đạt hiệu năng cao nhất.

---

## 🖥️ 4. HƯỚNG DẪN CẤU HÌNH CHO CHỦ NODE VPS (OPERATORS)

Nếu bạn muốn duy trì một VPS Node chuyên dụng để hỗ trợ cộng đồng hoặc bạn bè đào solo không cần đồng bộ sổ cái, hãy làm theo các bước:

1. **Khởi động Node trên VPS với tính năng Wallet Server mở rộng:**
   ```bash
   ./YonaCode node start --wallet-server --port 8080 --p2p-port 9000 --db-path ./data
   ```
2. **Mở cổng kết nối (Tường lửa):**
   Đảm bảo cổng `8080` (RPC) và `9000` (P2P) được mở công khai trên VPS để nhận kết nối từ bên ngoài.
3. **Cấu hình Nginx Proxy (Tùy chọn bảo mật):**
   Bạn có thể cấu hình Nginx làm reverse proxy để bảo vệ cổng RPC của node khỏi các đợt tấn công từ chối dịch vụ (DDoS).
