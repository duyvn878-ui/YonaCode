#!/usr/bin/env bash
# =====================================================================
# YonaCode ($YGO) GPU Miner - HiveOS Auto-Configuration & CLI Installer
# Usage:
#   curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
# =====================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}"
echo "======================================================================="
echo "       🚀 YONACODE ($YGO) HIVEOS GPU MINER AUTOMATED CLI SETUP         "
echo "======================================================================="
echo -e "${NC}"

# Parse Node IP:PORT argument
NODE_ADDR=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --node|-n)
      NODE_ADDR="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$NODE_ADDR" ]; then
  read -p "👉 Nhập IP:Port của Node đào Solo (Mặc định: 110.172.28.103:8080): " USER_INPUT
  NODE_ADDR=${USER_INPUT:-"110.172.28.103:8080"}
fi

# Split IP and Port
if [[ "$NODE_ADDR" == *":"* ]]; then
  NODE_IP=$(echo "$NODE_ADDR" | cut -d':' -f1)
  NODE_PORT=$(echo "$NODE_ADDR" | cut -d':' -f2)
else
  NODE_IP="$NODE_ADDR"
  NODE_PORT="8080"
fi

MINER_DIR="/hive/miners/custom/yona_gpu_miner"
RELEASE_URL="https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip"

echo -e "${YELLOW}[1/4] Khởi tạo thư mục HiveOS Custom Miner tại: ${MINER_DIR}...${NC}"
mkdir -p "$MINER_DIR"
cd "$MINER_DIR"

echo -e "${YELLOW}[2/4] Tải xuống bộ cài đặt GPU Miner nguyên bản từ GitHub Releases...${NC}"
curl -sSL -o YonaCode_Linux.zip "$RELEASE_URL"

echo -e "${YELLOW}[3/4] Giải nén và cấu hình các script vận hành HiveOS (h-manifest, h-run, h-stats)...${NC}"
unzip -o YonaCode_Linux.zip -d "$MINER_DIR" > /dev/null
rm -f YonaCode_Linux.zip

chmod +x yona_gpu_miner h-run.sh h-stats.sh 2>/dev/null || true

# Tạo/Cập nhật tệp h-manifest.conf với cấu hình Node IP:PORT
cat << EOF > h-manifest.conf
# =====================================================================
# YonaCode GPU Miner HiveOS Manifest Configuration (Auto-Generated)
# =====================================================================
CUSTOM_NAME=yona_gpu_miner
CUSTOM_VERSION=1.0.0
CUSTOM_ALGO=blake3
CUSTOM_BIN_PATH="yona_gpu_miner"
CUSTOM_LOG_BASENAME=/var/log/miner/custom/\$CUSTOM_NAME
CUSTOM_URL="${NODE_IP}:${NODE_PORT}"
EOF

echo -e "${GREEN}[4/4] Khởi chạy thợ đào GPU yona_gpu_miner kết nối tới Node ${NODE_IP}:${NODE_PORT}...${NC}"

# Đặt dịch vụ custom miner của HiveOS nếu đang trên rờ-g HiveOS
if [ -f "/hive/bin/custom" ]; then
  echo -e "${CYAN}[HIVEOS] Kích hoạt dịch vụ Custom Miner trong HiveOS...${NC}"
  /hive/bin/custom stop || true
  /hive/bin/custom config "${NODE_IP}:${NODE_PORT}" || true
  /hive/bin/custom start
else
  echo -e "${CYAN}[LINUX] Khởi chạy trực tiếp tiến trình đào GPU Solo...${NC}"
  ./h-run.sh &
fi

echo -e "${GREEN}"
echo "======================================================================="
echo "       🎉 ĐÃ TỰ ĐỘNG CẤU HÌNH VÀ KHỞI CHẠY THÀNH CÔNG HIVEOS MINER!     "
echo "   Solo Mining Node: ${NODE_IP}:${NODE_PORT}"
echo "   Để xem nhật ký đào realtime, gõ lệnh:"
echo "   tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log"
echo "======================================================================="
echo -e "${NC}"
