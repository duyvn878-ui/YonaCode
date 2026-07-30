# HƯỚNG DẪN KIỂM SOÁT VÀ PHÂN BIỆT GIAO DIỆN ĐÀO (MINING UI RULE)

### 1. Phân biệt rõ ràng giữa 2 hệ thống giao diện:
*   **Giao diện đào của Full Node (Heavy UI - `6_user_interface/web_ui`):**
    *   Tên hiển thị: **YONACODE TACTICAL MATRIX V2.0**.
    *   Tính năng: Báo cáo tốc độ băm thời gian thực (realtime throughput), chọn cấu hình thiết bị (CPU ONLY / GPU ONLY / HYBRID), thanh trượt Core Stress Level (50%), Terminal giao thức truyền tải thô.
    *   **CẤM:** Không được đưa các tính năng đào nhẹ như bộ sinh mnemonic BIP39 Seed Generator, Recover Wallet Modal, hay các nút tạo ví đơn giản vào giao diện đào của Full node.
*   **Giao diện đào nhẹ (Light Mining UI - `11_light_mining/web_ui`):**
    *   Tính năng: Bộ sinh mnemonic BIP39 Seed Generator, nút tạo và khôi phục ví đơn giản, cấu hình ví thợ đào để chạy gọn nhẹ trên client.

### 2. Kỷ luật chỉnh sửa code:
*   Trước khi sửa bất kỳ file frontend nào, bắt buộc phải định vị chính xác đường dẫn tệp tin để tránh nhầm lẫn giữa `6_user_interface/web_ui` và `11_light_mining/web_ui`.
*   Giữ nguyên tính nhất quán của giao diện thô/chuyên sâu cho Full node và giao diện đơn giản/tiện ích cho Light Mining client.
