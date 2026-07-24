#!/usr/bin/env bash
# =====================================================================
# YonaCode GPU Miner HiveOS Telemetry Stats Script
# Feature: Multi-GPU support (NVIDIA/AMD), dead miner detection, uptime tracking
# Date: 2026-07-23
# =====================================================================

. h-manifest.conf

LOG_FILE="${CUSTOM_LOG_BASENAME}.log"
PID_FILE="${CUSTOM_LOG_BASENAME}.pid"

# 1. Kiểm tra sự tồn tại của file nhật ký
if [ ! -f "$LOG_FILE" ]; then
  echo "khs=0"
  echo "stats=\"\""
  exit 0
fi

# 2. Phát hiện Miner bị treo (Dead Miner Detection)
# Nếu file log không được cập nhật quá 60 giây, coi như miner đã dừng hoạt động
last_update=$(stat -c %Y "$LOG_FILE" 2>/dev/null || echo 0)
now=$(date +%s)
diff=$((now - last_update))

if [ "$diff" -gt 60 ]; then
  echo "khs=0"
  echo "stats=\"{\\\"hs\\\": [0], \\\"temp\\\": [], \\\"fan\\\": [], \\\"uptime\\\": 0}\""
  exit 0
fi

# 3. Tính toán thời gian hoạt động liên tục (Uptime)
uptime=0
if [ -f "$PID_FILE" ]; then
  pid=$(cat "$PID_FILE")
  if ps -p "$pid" > /dev/null 2>&1; then
    uptime=$(ps -o etimes= -p "$pid" 2>/dev/null | xargs || echo 0)
  fi
fi

# 4. Quét tốc độ băm (MH/s) từ dòng log mới nhất của miner
hashrate_mhs=$(tail -n 100 "$LOG_FILE" | grep "Hashrate:" | tail -n 1 | awk '{print $4}')

if [ -z "$hashrate_mhs" ]; then
  khs=0
else
  # Đổi sang KH/s để hiển thị chuẩn trên giao diện HiveOS
  khs=$(echo "$hashrate_mhs" | awk '{print $1 * 1000}')
fi

# 5. Thu thập thông số phần cứng từ NVIDIA GPU
nvidia_temps=$(nvidia-smi --query-gpu=temperature.gpu --format=csv,noheader,nounits 2>/dev/null | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
nvidia_fans=$(nvidia-smi --query-gpu=fan.speed --format=csv,noheader,nounits 2>/dev/null | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')

# 6. Thu thập thông số phần cứng từ AMD GPU (nếu dùng rocm-smi)
amd_temps=""
amd_fans=""
if which rocm-smi >/dev/null 2>&1; then
  # Đọc nhiệt độ và tốc độ quạt từ rocm-smi
  amd_temps=$(rocm-smi --showtemp 2>/dev/null | grep -E "Temp" | awk '{print $2}' | tr -d 'C' | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
  amd_fans=$(rocm-smi --showfan 2>/dev/null | grep -E "Fan" | awk '{print $2}' | tr -d '%' | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
fi

# Gộp thông số phần cứng của cả NVIDIA và AMD
all_temps=""
all_fans=""
if [ ! -z "$nvidia_temps" ] && [ ! -z "$amd_temps" ]; then
  all_temps="${nvidia_temps},${amd_temps}"
  all_fans="${nvidia_fans},${amd_fans}"
elif [ ! -z "$nvidia_temps" ]; then
  all_temps="$nvidia_temps"
  all_fans="$nvidia_fans"
else
  all_temps="$amd_temps"
  all_fans="$amd_fans"
fi

# 7. Xây dựng mảng tốc độ băm cho từng GPU
# Vì miner chỉ báo tổng tốc độ băm, chúng ta báo tổng hashrate trên GPU đầu tiên, và 0 trên các GPU khác
gpu_count=$(echo "$all_temps" | tr ',' '\n' | grep -v "^$" | wc -l)
if [ "$gpu_count" -le 0 ]; then
  gpu_count=1
fi

hs_array=""
for ((i=0; i<gpu_count; i++)); do
  if [ $i -eq 0 ]; then
    hs_array="$khs"
  else
    hs_array="${hs_array},0"
  fi
done

# 8. Định dạng kết quả JSON trả về cho HiveOS Dashboard
if [ -z "$all_temps" ]; then
  stats="{\"hs\": [$hs_array], \"temp\": [], \"fan\": [], \"uptime\": $uptime}"
else
  stats="{\"hs\": [$hs_array], \"temp\": [$all_temps], \"fan\": [$all_fans], \"uptime\": $uptime}"
fi

echo "khs=$khs"
echo "stats='$stats'"
