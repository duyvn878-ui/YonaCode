import requests
import time
import secrets
import sys

# Thiết lập mã hóa UTF-8 cho console để hiển thị tiếng Việt chính xác
sys.stdout.reconfigure(encoding='utf-8')

# ==========================================
# CẤU HÌNH TOOL STRESS TEST
# ==========================================
# Cổng HTTP của Node 3 là 8082
NODE_URL = "http://127.0.0.1:8082/api/v1/send_batch_tx"

# ĐỊA CHỈ VÀ MẬT KHẨU VÍ HOẠT ĐỘNG TỐT (test_sender / reward address)
SENDER_ADDRESS = "680303fe459c4622e35c279347755db9b1139776fab81f83d8eaa141fa080146" 
SENDER_PASSWORD = "0000"

# Số dư ví hiện tại rất lớn.
# Điều chỉnh BATCH_SIZE và AMOUNT_BTCZ phù hợp để tránh lỗi "Không đủ số dư" (insufficient balance)
BATCH_SIZE = 500          # Số giao dịch mỗi lô (500 giao dịch/3 giây theo yêu cầu của Bạn)
INTERVAL_SECONDS = 3      # Thời gian nghỉ giữa các lô (giây)
AMOUNT_BTCZ = "0.00001"   # Số lượng gửi mỗi giao dịch (1000 VNT, để có thể spam lâu dài mà không cạn ví)
BASE_FEE = 1000           # Phí mặc định 1000 VNT để được xác nhận nhanh
# ==========================================

def generate_random_address():
    """Tạo ra 1 địa chỉ ví 32-byte ảo để ép State Tree của Node phải phình to"""
    return secrets.token_hex(32)

def main():
    print("======================================================")
    print("🔥 KHỞI ĐỘNG CỖ MÁY SPAM GIAO DỊCH EBP (BTC GENZ) 🔥")
    print(f"🎯 Target Node: {NODE_URL}")
    print(f"📦 Batch Size : {BATCH_SIZE} TXs / {INTERVAL_SECONDS} giây")
    print(f"💸 Lượng gửi  : {AMOUNT_BTCZ} BTC_Z / TX")
    print(f"🔑 Ví gửi     : 0x{SENDER_ADDRESS[:16]}...")
    print("======================================================")

    # 0. Quét thông tin tài khoản trước khi spam để đối soát trạng thái số dư và nonce (Chỉ Node 3)
    print("\n🔍 ĐANG QUÉT THÔNG TIN TÀI KHOẢN TRƯỚC KHI SPAM (CHỈ NODE 3)...")
    base_url_node3 = "http://127.0.0.1:8082"
    
    try:
        bal_response = requests.get(f"{base_url_node3}/api/v1/balance/{SENDER_ADDRESS}", timeout=5)
        if bal_response.status_code == 200:
            data = bal_response.json()
            balances = data.get("balances", {})
            btc_z = balances.get("btc_z", 0) / 100000000
            spendable = balances.get("spendable", 0) / 100000000
            pending = balances.get("pending", 0) / 100000000
            expected_nonce = data.get("expected_nonce", 0)
            print(f"📊 [Node 3] Tổng số dư: {btc_z} BTC_Z | Khả dụng: {spendable} BTC_Z | Chờ chín: {pending} BTC_Z")
            print(f"📊 [Node 3] Nonce hiện tại: {data.get('nonce', 0)} | Nonce mong đợi: {expected_nonce}")
        else:
            print(f"⚠️ [Node 3] Không thể lấy số dư (HTTP {bal_response.status_code})")
    except Exception as e:
        print(f"❌ [Node 3] Không thể kết nối tới Node: {e}")
    print("======================================================\n")

    seq_num = 1 # Số thứ tự lô (Sàn đánh dấu)
    start_spam_time = time.time()
    max_duration = 8 * 3600 # 8 tiếng (28800 giây)
    next_expected_nonce = None

    while True:
        if time.time() - start_spam_time > max_duration:
            print("======================================================")
            print("⏱️ ĐÃ HOÀN THÀNH SPAM GIAO DỊCH TRONG 8 TIẾNG! DỪNG TOOL.")
            print("======================================================")
            break

        start_time = time.time()
        
        # 1. Đồng bộ hóa nonce thông minh: Đợi Node cập nhật nonce của lô trước (Phương án B)
        base_nonce = None
        base_url = NODE_URL.split("/api/v1/")[0]

        if next_expected_nonce is not None:
            max_retries = 100 # Tối đa 10 giây (100 * 0.1s)
            print(f"⏳ Đang đồng bộ hóa nonce... Đợi Node 3 cập nhật expected_nonce lên {next_expected_nonce}...")
            for attempt in range(max_retries):
                try:
                    bal_response = requests.get(f"{base_url}/api/v1/balance/{SENDER_ADDRESS}", timeout=3)
                    if bal_response.status_code == 200:
                        res_data = bal_response.json()
                        node_nonce = res_data.get("expected_nonce", 0)
                        if node_nonce >= next_expected_nonce:
                            base_nonce = node_nonce
                            break
                except Exception:
                    pass
                time.sleep(0.1)
            
            if base_nonce is None:
                print(f"⚠️ Cảnh báo: Đã quá 10 giây Node chưa cập nhật expected_nonce lên {next_expected_nonce}. Tự động đồng bộ và sử dụng nonce thực tế từ Node...")

        # Nếu chưa lấy được base_nonce (lần chạy đầu hoặc đợi bị timeout), lấy trực tiếp từ Node
        if base_nonce is None:
            try:
                bal_response = requests.get(f"{base_url}/api/v1/balance/{SENDER_ADDRESS}", timeout=5)
                if bal_response.status_code == 200:
                    res_data = bal_response.json()
                    base_nonce = res_data.get("expected_nonce")
                    balances = res_data.get("balances", {})
                    spendable = balances.get("spendable", 0) / 100000000
                    if spendable <= 0:
                        print(f"⚠️ Cảnh báo: Số dư khả dụng của Ví gửi bằng 0 BTC_Z (đang chờ chín). Tạm nghỉ 5 giây...")
                        time.sleep(5)
                        continue
            except Exception as e:
                print(f"⚠️ Cảnh báo: Không thể lấy nonce dự phóng từ Node: {e}")

        if base_nonce is None:
            print("⚠️ Cảnh báo: Không lấy được expected_nonce hợp lệ từ Node. Bỏ qua lô này và thử lại...")
            time.sleep(2)
            continue

        # 2. Sinh danh sách giao dịch với nonce tuần tự chuẩn xác
        transactions = []
        for i in range(BATCH_SIZE):
            tx = {
                "receiver": "0x" + generate_random_address(),
                "amount": AMOUNT_BTCZ,
                "base_fee": BASE_FEE
            }
            if base_nonce is not None:
                tx["nonce"] = base_nonce + i
            transactions.append(tx)

        # 3. Đóng gói Payload theo chuẩn EBP của Go Bridge
        payload = {
            "sender": SENDER_ADDRESS,
            "seq_num": seq_num,
            "password": SENDER_PASSWORD,
            "transactions": transactions
        }

        print(f"\n[🚀 Lô #{seq_num}] Đang nã đạn {BATCH_SIZE} giao dịch vào Node (Base Nonce: {base_nonce})...")

        try:
            # Gửi Request lên Node (Timeout 60s để phù hợp với thời gian Go Node xếp hàng xử lý gRPC dưới tải cao)
            response = requests.post(NODE_URL, json=payload, timeout=60)
            
            try:
                data = response.json()
            except ValueError:
                data = {"message": response.text}

            if response.status_code == 200 and data.get("status") == "Success":
                print(f"✅ THÀNH CÔNG! Đã nhồi {data.get('tx_count')} TXs vào Mempool.")
                print(f"⚡ Thời gian Node (Go + Rust) xử lý và ký lô: {data.get('duration_ms')} ms")
                # Đặt nonce dự phóng tiếp theo để lô sau polling chờ Node cập nhật
                next_expected_nonce = base_nonce + BATCH_SIZE
            else:
                # Bắt lỗi từ hệ thống bảo vệ của Node
                print(f"❌ BỊ TỪ CHỐI (Mã {response.status_code}): {data.get('message')}")
                if "audit_logs" in data and data["audit_logs"]:
                    for log in data["audit_logs"]:
                        print(f"   {log}")
                # Reset nonce dự phóng nếu bị từ chối
                next_expected_nonce = None

        except requests.exceptions.RequestException as e:
            print(f"🛑 LỖI MẠNG: Không thể kết nối tới Node (Node có thể đã sập hoặc quá tải): {e}")
            next_expected_nonce = None

        seq_num += 1

        # 3. Tính toán thời gian ngủ để duy trì đúng nhịp
        elapsed = time.time() - start_time
        sleep_time = INTERVAL_SECONDS - elapsed
        
        if sleep_time > 0:
            print(f"⏳ Chờ nạp đạn {sleep_time:.2f}s...")
            time.sleep(sleep_time)
        else:
            print("⚠️ WARNING: Node phản hồi chậm hơn thời gian nghỉ, bắn lô tiếp theo ngay lập tức!")

if __name__ == "__main__":
    main()
