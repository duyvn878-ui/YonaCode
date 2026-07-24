#!/usr/bin/env bash
# =====================================================================
# YonaCode GPU Miner HiveOS Runner Script
# Feature: Signal handling, input validation, log rotation, process guard
# Date: 2026-07-23
# =====================================================================

. h-manifest.conf
. colors

# Hàm xử lý khi nhận tín hiệu dừng từ HiveOS
stop_miner() {
  echo -e "${RED}[HIVEOS-RUN] Nhận tín hiệu dừng từ hệ thống. Đang dừng yona_gpu_miner (PID: $MINER_PID)...${NOCOLOR}"
  if [ ! -z "$MINER_PID" ]; then
    kill -15 "$MINER_PID" 2>/dev/null
    wait "$MINER_PID" 2>/dev/null
  fi
  rm -f "${CUSTOM_LOG_BASENAME}.pid"
  echo -e "${GREEN}[HIVEOS-RUN] Miner đã được tắt sạch sẽ.${NOCOLOR}"
  exit 0
}

# Đăng ký bẫy tín hiệu (Signal Trap)
trap 'stop_miner' SIGTERM SIGINT SIGHUP

# Di chuyển vào thư mục làm việc của Custom Miner
cd $MINER_DIR

# 1. Kiểm tra sự tồn tại của file chạy
if [ ! -f "yona_gpu_miner" ]; then
  echo -e "${RED}[HIVEOS-RUN] LỖI CỰC KỲ NGHIÊM TRỌNG: Không tìm thấy tệp thực thi yona_gpu_miner tại $(pwd)${NOCOLOR}"
  exit 1
fi

# Cấp quyền thực thi nếu chưa có
chmod +x yona_gpu_miner

# 2. Quản lý thư mục và tệp tin log
LOG_DIR=$(dirname "$CUSTOM_LOG_BASENAME")
mkdir -p "$LOG_DIR"

LOG_FILE="${CUSTOM_LOG_BASENAME}.log"

# Xoay vòng log nếu kích thước vượt quá 10MB để tránh tràn dung lượng đĩa cứng
if [ -f "$LOG_FILE" ]; then
  log_size=$(stat -c%s "$LOG_FILE" 2>/dev/null || echo 0)
  if [ "$log_size" -gt 10485760 ]; then
    echo -e "${YELLOW}[HIVEOS-RUN] Nhật ký vượt quá 10MB, đang dọn dẹp và làm mới log...${NOCOLOR}"
    echo "[HIVEOS-RUN] Khởi động lại luồng ghi log mới ngày $(date)" > "$LOG_FILE"
  fi
fi

# 3. Phân tách IP và Port từ trường Pool URL ($CUSTOM_URL)
if [ -z "$CUSTOM_URL" ]; then
  echo -e "${YELLOW}[HIVEOS-RUN] Cảnh báo: Địa chỉ Pool URL trống. Đang sử dụng Node mặc định...${NOCOLOR}"
  POOL_IP="110.172.28.103"
  POOL_PORT="8080"
else
  if [[ "$CUSTOM_URL" == *":"* ]]; then
    POOL_IP=$(echo "$CUSTOM_URL" | cut -d':' -f1)
    POOL_PORT=$(echo "$CUSTOM_URL" | cut -d':' -f2)
  else
    POOL_IP="$CUSTOM_URL"
    POOL_PORT="8080"
  fi
fi

# Loại bỏ ký tự xuống dòng hoặc khoảng trắng thừa nếu có
POOL_IP=$(echo "$POOL_IP" | xargs)
POOL_PORT=$(echo "$POOL_PORT" | xargs)

echo -e "${GREEN}[HIVEOS-RUN] Khởi chạy Solo Mining tới Node: $POOL_IP:$POOL_PORT${NOCOLOR}"

# 4. Khởi chạy tiến trình Miner ngầm để có thể theo dõi và bẫy tín hiệu dừng
./yona_gpu_miner "$POOL_IP" "$POOL_PORT" >> "$LOG_FILE" 2>&1 &
MINER_PID=$!

# Ghi nhận PID của tiến trình
echo "$MINER_PID" > "${CUSTOM_LOG_BASENAME}.pid"

# Chờ tiến trình Miner chạy xong hoặc nhận signal dừng
wait "$MINER_PID"
stop_miner
