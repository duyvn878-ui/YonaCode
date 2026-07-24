# 🚀 HIVEOS SOLO MINING CONFIGURATION GUIDE - YONACODE ($YGO)
> **HiveOS Solo Mining Configuration Guide for YonaCode ($YGO) GPU Mining**

This guide provides detailed instructions on how to configure the **Flight Sheet** and run **CLI** commands on **HiveOS** for Solo Mining **YonaCode ($YGO)** directly to your Node using a GPU.

> [!IMPORTANT]
> **CORE MINING MODE NOTICE:**
> Pool Mining is currently disabled on the Node system (`s.pool == nil`). Miners must run in **Solo Mining** mode (connecting directly to your Node IP/Port, block rewards will be sent to the Proposer wallet address configured on the Node itself).

---

## ⚡ 1. GPU DRIVERS ON HIVEOS
* **HiveOS Default**: HiveOS is a dedicated OS for mining and comes pre-loaded with **fully operational GPU Drivers** (NVIDIA CUDA and AMD OpenCL/ROCm). You **do not need to install drivers**.
* **Driver Upgrades (If using new GPUs or requiring a higher CUDA version)**:
  * **NVIDIA**: Run the driver update command via Hive Shell / SSH:
    ```bash
    # Update to the latest recommended NVIDIA driver
    nvidia-driver-update
    ```
  * **AMD**: Update the HiveOS system to upgrade the integrated AMD drivers:
    ```bash
    selfupdate
    ```

---

## 📋 2. CUSTOM MINER PACKAGE STRUCTURE FOR HIVEOS
For HiveOS to recognize and run the miner in Solo mode, the configuration scripts and the miner binary are packaged inside the official release archive (`YonaCode_Linux.zip`) at the root level:
1. **`yona_gpu_miner`**: The Linux GPU miner binary.
2. **`h-manifest.conf`**: Miner identification manifest.
3. **`h-run.sh`**: Solo mining execution script.
4. **`h-stats.sh`**: Telemetry script reporting hash rate, temperatures, and fan speeds to the HiveOS dashboard.

---

## 🛠️ 3. HIVEOS FLIGHT SHEET CONFIGURATION (STEP-BY-STEP)
Follow these steps to configure your Solo Mining Flight Sheet in the HiveOS web dashboard:

### Step 1: Create a Placeholder Wallet
1. Go to your HiveOS Farm $\rightarrow$ Select **Wallets** $\rightarrow$ Click **Add Wallet**.
2. **Coin**: Enter and select **`YGO`** (or create a custom coin if not found).
3. **Address**: Enter any wallet address starting with `0x` as a placeholder ( rewards go to the Node's proposer address).
4. **Name**: Enter a name (e.g., `Yona Placeholder Wallet`).
5. Click **Create**.

### Step 2: Create the Flight Sheet
1. Select the **Flight Sheets** tab in your Farm or Worker.
2. Set the main options as follows:

| Field | Configuration Value |
| :--- | :--- |
| **Coin** | Select **YGO** (or your custom YGO coin) |
| **Wallet** | Select the **Yona Placeholder Wallet** created in Step 1 |
| **Pool** | Select **Configure in miner** |
| **Miner** | Select **Custom** (at the bottom of the miner list) |

3. Click **Setup Miner Config** to open the advanced custom miner settings.

### Step 3: Configure Setup Miner Config
Fill in the fields exactly as follows:

* **Miner name**: `yona_gpu_miner`
* **Installation URL**: Paste the official release ZIP URL:
  ```text
  https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip
  ```
* **Hash algorithm**: `blake3`
* **Pool URL**: The IP address and Port of your YonaCode Node (e.g., `110.172.28.103:8080`).
* **Wallet and worker template**: *Leave Blank* (Do not pass a wallet template to force solo mode).
* **Extra config arguments**: *Leave Blank*.

---

## 💾 4. MINER SCRIPT FILE DETAILS

### 1. `h-manifest.conf`
```bash
# Custom miner identifier
CUSTOM_NAME=yona_gpu_miner

# Miner release version
CUSTOM_VERSION=1.0.0

# Base path for miner logs
CUSTOM_LOG_BASENAME=/var/log/miner/custom/$CUSTOM_NAME
```

### 2. `h-run.sh`
```bash
#!/usr/bin/env bash

# Load variables
. h-manifest.conf
. colors

# Navigate to miner directory
cd $MINER_DIR

# Parse IP and Port from the Pool URL ($CUSTOM_URL)
if [[ "$CUSTOM_URL" == *":"* ]]; then
  POOL_IP=$(echo "$CUSTOM_URL" | cut -d':' -f1)
  POOL_PORT=$(echo "$CUSTOM_URL" | cut -d':' -f2)
else
  POOL_IP="$CUSTOM_URL"
  POOL_PORT="8080"
fi

# Apply fallback defaults if empty
POOL_IP=${POOL_IP:-"110.172.28.103"}
POOL_PORT=${POOL_PORT:-"8080"}

# Execute miner in Solo Mining mode (no wallet parameter passed)
./yona_gpu_miner $POOL_IP $POOL_PORT > $CUSTOM_LOG_BASENAME.log 2>&1
```

### 3. `h-stats.sh`
```bash
#!/usr/bin/env bash

. h-manifest.conf

LOG_FILE="${CUSTOM_LOG_BASENAME}.log"

if [ ! -f "$LOG_FILE" ]; then
  echo "khs=0"
  echo "stats=\"\""
  exit 0
fi

# Extract the hashrate (MH/s) from the log file
hashrate_mhs=$(tail -n 100 "$LOG_FILE" | grep "Hashrate:" | tail -n 1 | awk '{print $4}')

if [ -z "$hashrate_mhs" ]; then
  khs=0
else
  # Convert MH/s to KH/s for HiveOS telemetry format
  khs=$(echo "$hashrate_mhs" | awk '{print $1 * 1000}')
fi

# Query temperature and fan speeds for NVIDIA and AMD GPUs
nvidia_temps=$(nvidia-smi --query-gpu=temperature.gpu --format=csv,noheader,nounits 2>/dev/null | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
nvidia_fans=$(nvidia-smi --query-gpu=fan.speed --format=csv,noheader,nounits 2>/dev/null | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')

amd_temps=""
amd_fans=""
if which rocm-smi >/dev/null 2>&1; then
  amd_temps=$(rocm-smi --showtemp 2>/dev/null | grep -E "Temp" | awk '{print $2}' | tr -d 'C' | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
  amd_fans=$(rocm-smi --showfan 2>/dev/null | grep -E "Fan" | awk '{print $2}' | tr -d '%' | tr '\n' ' ' | sed 's/ $//' | sed 's/ /,/g')
fi

# Merge telemetry metrics
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

# Form the hashrate array for HiveOS dashboard
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

# Output stats JSON to stdout
if [ -z "$all_temps" ]; then
  stats="{\"hs\": [$hs_array], \"temp\": [], \"fan\": [], \"uptime\": $uptime}"
else
  stats="{\"hs\": [$hs_array], \"temp\": [$all_temps], \"fan\": [$all_fans], \"uptime\": $uptime}"
fi

echo "khs=$khs"
echo "stats='$stats'"
```

---

## 💻 5. CLI MANIFEST SETUP & TESTING (HIVE SHELL / SSH)
To setup the miner manually on the rig using CLI:

### Step 1: Extract GPU Miner
Run this command from your project directory to extract only the GPU miner executable:
```bash
# Create HiveOS Custom Miner directory
mkdir -p /hive/miners/custom/yona_gpu_miner

# Extract the miner binary directly from the project ZIP
unzip -o zip/YonaCode_Linux.zip yona_gpu_miner -d /hive/miners/custom/yona_gpu_miner/

# Make the miner executable
chmod +x /hive/miners/custom/yona_gpu_miner/yona_gpu_miner
```
*(Afterwards, create `h-manifest.conf`, `h-run.sh`, and `h-stats.sh` files inside `/hive/miners/custom/yona_gpu_miner/` as described in **Section 4**).*

### Step 2: Run Manual Solo Test
Verify connection and bhashrate directly on the command line:
```bash
/hive/miners/custom/yona_gpu_miner/yona_gpu_miner 110.172.28.103 8080
```

---

## 🚦 6. VERIFY OPERATION
1. Apply the Flight Sheet to your rig by clicking the **Rocket** icon 🚀.
2. Watch the live console output on your worker:
   ```bash
   tail -f /var/log/miner/custom/yona_gpu_miner.log
   ```
3. If you see block templates loading correctly:
   `[GPU-MINER] 🔨 Mining Block #...`
   Your Solo Mining is fully working!
