# 🏦 Tài liệu Hướng dẫn Tích hợp API Sàn Giao dịch (YonaCode Exchange API & EBP Integration Guide)

Tài liệu này cung cấp hướng dẫn chi tiết dành cho đội ngũ Kỹ sư Hệ thống và Lập trình viên của Sàn giao dịch (Crypto Exchanges) để tích hợp nạp/rút tiền, quản lý ví nóng (Hot Wallet), và sử dụng **Giao thức Đóng gói Lô Tuần tự EBP (Exchange Batch Protocol - `TXSQ`)** với hiệu năng xử lý cực cao trên mạng lưới YonaCode Blockchain.

---

## 📑 Mục lục
1. [Tổng quan về Giao thức EBP (Exchange Batch Protocol)](#1-tổng-quan-về-giao-thức-ebp)
2. [Cấu trúc Đóng gói Nhị phân `TXSQ`](#2-cấu-trúc-đóng-gói-nhị-phân-txsq)
3. [Quy trình Quản lý Ví Nóng & Nonce Sàn](#3-quy-trình-quản-lý-ví-nóng--nonce-sàn)
4. [Danh sách API RPC dành cho Sàn Giao dịch](#4-danh-sách-api-rpc-dành-cho-sàn-giao-dịch)
5. [Ví dụ Mã Nguồn Tích hợp (Go / Python / Node.js)](#5-ví-dụ-mã-nguồn-tích-hợp)
6. [Tiêu chuẩn An toàn & Nguyên tắc Vận hành Ví Nóng](#6-tiêu-chuẩn-an-toàn--nguyên-tắc-vận-hành-ví-nóng)

---

## 1. Tổng quan về Giao thức EBP

Trong các đợt rút tiền hàng loạt (Mass Withdrawal), việc gửi từng giao dịch đơn lẻ gây ra nguy cơ nghẽn mạng và lệch thứ tự Nonce. YonaCode Blockchain cung cấp chuẩn **EBP (`TXSQ`)**, cho phép Sàn:
* **Gộp hàng trăm giao dịch rút tiền thành 1 gói nhị phân duy nhất.**
* **Đảm bảo tính tuần tự tuyệt đối (Strict Nonce Order):** Các giao dịch rút tiền được sắp xếp chuẩn theo thứ tự Nonce tăng dần (`N`, `N+1`, `N+2`...) ngay tại đầu phát.
* **Xử lý siêu tốc qua luồng Xe Buýt Mempool (2-Second Bus):** Node bỏ qua các bước kiểm tra rác thừa, giải nén và đưa thẳng lô giao dịch vào Rust Core để chốt sổ nguyên tử.

---

## 2. Cấu trúc Đóng gói Nhị phân `TXSQ`

Một gói giao dịch EBP gửi lên mạng P2P hoặc qua cổng API RPC phải được đóng gói đúng định dạng nhị phân sau:

| Trường (Field) | Kích thước | Kiểu dữ liệu | Mô tả |
| :--- | :--- | :--- | :--- |
| **Magic Header** | 4 Bytes | ASCII String | Cố định là `"TXSQ"` (0x54 0x58 0x53 0x51) |
| **Exchange Address** | 32 Bytes | Raw Bytes | Địa chỉ ví nóng (Hot Wallet) của Sàn |
| **Batch ID / SeqNum** | 8 Bytes | Big-Endian uint64 | Mã định danh lô / Số thứ tự |
| **Start Nonce** | 8 Bytes | Big-Endian uint64 | Nonce của giao dịch đầu tiên trong lô |
| **End Nonce** | 8 Bytes | Big-Endian uint64 | Nonce của giao dịch cuối cùng trong lô |
| **Tx Count** | 4 Bytes | Big-Endian uint32 | Tổng số lượng giao dịch trong lô (tối đa 200 TX/lô) |
| **Payload Data** | Variable | Length-prefixed | Danh sách mảng byte của từng giao dịch thô |

---

## 3. Quy trình Quản lý Ví Nóng & Nonce Sàn

Để tránh lỗi lệch Nonce (Code 105 / Code 106) khi xử lý rút tiền quy mô lớn, Sàn cần tuân thủ quy trình 3 bước:

```
[Bước 1: Truy vấn Nonce kỳ vọng] ➡️ [Bước 2: Ký chuỗi Giao dịch] ➡️ [Bước 3: Phát sóng Lô EBP]
      (GetExpectedNonce)             (Nonce N, N+1, N+2...)            (Broadcast TXSQ)
```

1. **Bước 1:** Gọi RPC `GetExpectedNonce(hot_wallet_address)` để lấy Nonce chuẩn bị ký. Hàm này sẽ tự động tính toán dồn Nonce của các giao dịch đang chờ trong RAM Mempool.
2. **Bước 2:** Gán Nonce liên tục tăng dần cho danh sách người rút tiền:
   * Giao dịch rút 1: `Nonce = N`
   * Giao dịch rút 2: `Nonce = N + 1`
   * Giao dịch rút 3: `Nonce = N + 2`
3. **Bước 3:** Đóng gói toàn bộ thành định dạng `TXSQ` và gửi đến API RPC Node.

---

## 4. Danh sách API RPC dành cho Sàn Giao dịch

Node YonaCode cung cấp cổng REST/JSON-RPC (mặc định cổng `9090`) và gRPC (mặc định cổng `18080`).

### 4.1. Truy vấn số dư & Nonce ví nóng
* **Endpoint REST:** `GET /api/v1/account/{address}`
* **gRPC Method:** `SclService/GetNonce` & `SclService/GetAccountState`
* **Mẫu Response JSON:**
```json
{
  "address": "0x680303fe459c4622e35c279347755db9b1139776fab81f83d8eaa141fa080146",
  "balance": "1000000000000",
  "nonce": 34,
  "expected_nonce": 34
}
```

### 4.2. Gửi lô giao dịch EBP rút tiền (Bulk Withdrawal)
* **Endpoint REST:** `POST /api/v1/ebp/broadcast`
* **Body:** Binary payload gói `TXSQ` hoặc JSON array các giao dịch thô.
* **Mẫu Response JSON:**
```json
{
  "success": true,
  "batch_id": 1052,
  "processed_count": 50,
  "start_nonce": 34,
  "end_nonce": 83,
  "status": "ACCEPTED_MEMPOOL_BUS"
}
```

### 4.3. Đăng ký nhận sự kiện Nạp tiền (Deposit Webhook / SSE)
* **Endpoint SSE:** `GET /api/v1/events/stream`
* **Mô tả:** Lắng nghe thời gian thực các sự kiện nạp tiền vào các địa chỉ ví của Sàn ngay khi khối được chốt sổ nguyên tử (Nuclear Shield State Finality).

---

## 5. Ví dụ Mã Nguồn Tích hợp

### 5.1. Đóng gói Lô `TXSQ` bằng Go (Golang)

```go
package main

import (
	"encoding/binary"
)

// PackSequentialBatch đóng gói lô giao dịch EBP chuẩn Sàn
func PackSequentialBatch(exchangeAddr []byte, batchId uint64, startNonce uint64, endNonce uint64, txsBytes [][]byte) []byte {
	var buf []byte

	// 1. Header "TXSQ"
	buf = append(buf, []byte("TXSQ")...)

	// 2. Địa chỉ Sàn (32 bytes)
	addrBytes := make([]byte, 32)
	copy(addrBytes, exchangeAddr)
	buf = append(buf, addrBytes...)

	// 3. Batch ID (8 bytes)
	bId := make([]byte, 8)
	binary.BigEndian.PutUint64(bId, batchId)
	buf = append(buf, bId...)

	// 4. Start Nonce (8 bytes)
	sNonce := make([]byte, 8)
	binary.BigEndian.PutUint64(sNonce, startNonce)
	buf = append(buf, sNonce...)

	// 5. End Nonce (8 bytes)
	eNonce := make([]byte, 8)
	binary.BigEndian.PutUint64(eNonce, endNonce)
	buf = append(buf, eNonce...)

	// 6. Số lượng giao dịch (4 bytes)
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, uint32(len(txsBytes)))
	buf = append(buf, count...)

	// 7. Ghi các giao dịch thô kèm độ dài
	for _, tx := range txsBytes {
		txLen := make([]byte, 4)
		binary.BigEndian.PutUint32(txLen, uint32(len(tx)))
		buf = append(buf, txLen...)
		buf = append(buf, tx...)
	}

	return buf
}
```

### 5.2. Đóng gói Lô `TXSQ` bằng Python

```python
import struct

def pack_sequential_batch(exchange_addr_bytes, batch_id, start_nonce, end_nonce, txs_bytes_list):
    buf = bytearray()
    
    # 1. Magic Header "TXSQ"
    buf.extend(b"TXSQ")
    
    # 2. Address (32 bytes)
    addr_padded = exchange_addr_bytes.ljust(32, b'\x00')[:32]
    buf.extend(addr_padded)
    
    # 3. Batch ID, Start Nonce, End Nonce (Big-Endian uint64)
    buf.extend(struct.pack(">Q", batch_id))
    buf.extend(struct.pack(">Q", start_nonce))
    buf.extend(struct.pack(">Q", end_nonce))
    
    # 4. Tx Count (Big-Endian uint32)
    buf.extend(struct.pack(">I", len(txs_bytes_list)))
    
    # 5. Raw Transactions
    for tx in txs_bytes_list:
        buf.extend(struct.pack(">I", len(tx)))
        buf.extend(tx)
        
    return bytes(buf)
```

---

## 6. Tiêu chuẩn An toàn & Nguyên tắc Vận hành Ví Nóng

1. **Giới hạn kích thước Lô (Batch Chunking):** Mỗi lô `TXSQ` nên chia nhỏ tối đa **200 giao dịch/lô** để tận dụng tối đa băng thông luồng xe buýt 2 giây của Node.
2. **Xác nhận Độ sâu Khối (Confirmation Height):** Đối với các khoản Nạp tiền (Deposit), khuyến nghị Sàn chờ tối thiểu **3 Block Confirmations** trước khi cộng số dư cho người dùng (mặc dù mạng lưới YonaCode đã áp dụng cơ chế chống Reorg nguyên tử).
3. **Phí tạo ví mới (Creation Fee):** Nếu rút tiền đến một địa chỉ ví hoàn toàn mới (chưa từng có lịch sử trên sổ cái), hệ thống sẽ tính thêm phí khởi tạo trạng thái `1000 nanoVNT`. Sàn cần kiểm tra số dư ví nhận để dự trù phí chuẩn xác.
