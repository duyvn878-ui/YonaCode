# -*- coding: utf-8 -*-
"""
Tool ký thông điệp cảnh báo khẩn cấp (Yona Alert Signer)
Sử dụng thư viện cryptography của Python.
"""
import sys
import json
import time

try:
    from cryptography.hazmat.primitives.asymmetric import ed25519
except ImportError:
    print("[-] Vui lòng cài đặt thư viện cryptography: pip install cryptography")
    sys.exit(1)

def main():
    print("=== YONA CODE ALERT SIGNER ===")
    
    # 1. Nhập Private Key ví Pool dạng Hex (32 bytes)
    priv_key_hex = input("Nhập Private Key của bạn (dạng hex): ").strip()
    try:
        priv_bytes = bytes.fromhex(priv_key_hex)
        if len(priv_bytes) != 32:
            print("[-] Lỗi: Private Key phải đúng 32 bytes (64 ký tự hex).")
            return
        private_key = ed25519.Ed25519PrivateKey.from_private_bytes(priv_bytes)
        public_key = private_key.public_key()
        pub_hex = public_key.public_bytes_raw().hex()
        print(f"[+] Khóa công khai tương ứng (Root of Trust): {pub_hex}")
    except Exception as e:
        print(f"[-] Lỗi nạp khóa riêng tư: {e}")
        return

    # 2. Nhập thông tin Alert
    alert_id = int(input("Nhập ID cảnh báo (ví dụ: 1): ").strip())
    msg_vi = input("Nhập thông điệp Tiếng Việt: ").strip()
    msg_en = input("Nhập thông điệp Tiếng Anh: ").strip()
    github_url = input("Nhập liên kết GitHub cập nhật (để trống nếu không có): ").strip()
    expiration_block = int(input("Nhập chiều cao khối hết hạn (ví dụ: 25000): ").strip())
    
    # 3. Tạo Signing Payload: id|message_vi|message_en|github_url|expiration_block
    payload = f"{alert_id}|{msg_vi}|{msg_en}|{github_url}|{expiration_block}"
    payload_bytes = payload.encode('utf-8')
    
    # 4. Ký payload bằng Ed25519
    signature_bytes = private_key.sign(payload_bytes)
    signature_hex = signature_bytes.hex()
    
    # 5. Xuất ra cấu trúc JSON hoàn chỉnh
    alert_data = {
        "id": alert_id,
        "message_vi": msg_vi,
        "message_en": msg_en,
        "github_url": github_url,
        "expiration_block": expiration_block,
        "signature": signature_hex
    }
    
    output_file = "alert.json"
    with open(output_file, 'w', encoding='utf-8') as f:
        json.dump(alert_data, f, ensure_ascii=False, indent=2)
        
    print(f"\n[+] Đã ký thành công và xuất ra tệp {output_file}!")
    print(json.dumps(alert_data, ensure_ascii=False, indent=2))
    print("\n>>> Hãy đẩy tệp alert.json này lên nhánh chính (main) của Github duyvn878/YonaCode để phát sóng cảnh báo!")

if __name__ == "__main__":
    main()
