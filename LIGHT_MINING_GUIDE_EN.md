# 🚀 YONACODE ($YGO) LIGHT MINING GUIDE
> **Comprehensive Guide to Remote Proxy Mining & Yona Hash Algorithm (Block Height ≥ 38500)**

This document provides detailed information on **what Light Mining is**, the core technical mechanics of the Remote Proxy architecture, the upcoming algorithm transition to **Yona Hash (Block Height ≥ 38500)**, full CLI command & flag reference tables, and step-by-step setup guides for Windows, Linux, and HiveOS.

---

## 💡 1. WHAT IS LIGHT MINING?

**Light Mining (Remote Proxy Mining)** is YonaCode's ($YGO) next-generation mining solution.

Instead of requiring miners to download and store tens of gigabytes of blockchain ledger data or run a heavy Full Node consuming disk space and CPU bandwidth, **Light Mining concentrates 100% of local hardware resources strictly on CUDA GPU hashing**.

### 🌟 KEY ADVANTAGES OF LIGHT MINING:
1. **Ultra Lightweight (< 30 MB Package)**: Zero blockchain ledger download required. Start mining instantly within 2 seconds.
2. **Minimal Resource Usage**: Zero SSD/HDD storage consumption, minimal RAM footprint, and zero CPU waste on block validation.
3. **Maximum CUDA GPU Hashrate (2.3+ GH/s)**: 100% of NVIDIA/AMD GPU processing power is dedicated to CUDA GPU hash calculations.
4. **Direct Production VPS Node Connectivity**: Light miners fetch block templates automatically from the official VPS Node (`110.172.28.103:8080`).
5. **Secure Direct Payouts**: Once a block is solved, block rewards are credited directly to your unique Ed25519 wallet address (`0x...`).

---

## ⚡ 2. ALGORITHM TRANSITION NOTICE: "YONA HASH" (HEIGHT ≥ 38500)

> [!IMPORTANT]
> **YONA HASH ALGORITHM TRANSITION NOTICE:**
> Starting from **Block Height ≥ 38500**, the YonaCode network officially transitions to the next-generation ASIC-resistant **Yona Hash** algorithm (incorporating `parent_hash` and `merkle_root`).
> 
> * **Automatic Hot-Switching**: The GPU CUDA Mining Engine in this Light Miner release includes automatic algorithm hot-switching. When the Node broadcasts a block template at Height $\ge 38500$, the miner switches seamlessly to `Yona Hash` **without requiring any miner restart or manual re-configuration**.

---

## 🛠️ 3. FULL CLI COMMAND & FLAG REFERENCE TABLE

| Flag / Option | Description & Purpose | Default Value | Example Usage |
| :--- | :--- | :--- | :--- |
| **`--wallet`** | Reward $YGO wallet address (`0x...`) | *(Auto-prompts or generates)* | `--wallet 0x680303fe12345678...` |
| **`--node`** | Target Production VPS Node IP & Port | `110.172.28.103:8080` | `--node 110.172.28.103:8080` |
| **`--lang`** | CLI Terminal UI Language (`vn`, `en`, `zh`) | `vn` | `--lang en` *(English)* <br> `--lang zh` *(Chinese)* |
| **`--cli`** | Force Command Line Interface (CLI) Mode | `false` | `--cli` |
| **`--device`** | Mining Hardware Target (`gpu`) | `gpu` | `--device gpu` |
| **`--port`** | Internal Proxy Web Dashboard Port | `18888` | `--port 18888` |
| **`--no-browser`** | Disable auto-opening web browser on launch | `false` | `--no-browser` |
| **`--help`** | Display full flag help list and descriptions | `false` | `--help` |

---

## 💻 4. STEP-BY-STEP SETUP GUIDES FOR WINDOWS, LINUX & HIVEOS

### A. On Windows:

#### Method 1: Web UI Dashboard (GUI)
1. Extract `YonaCode_Light_Mining_Windows.zip`.
2. Double-click `YonaCode_Light_Miner.exe`.
3. Browser opens automatically at `http://127.0.0.1:18888` $\rightarrow$ Enter wallet $\rightarrow$ Click **"START GPU CUDA MINER"**.

#### Method 2: Command Line Interface (CLI Terminal)
Open Command Prompt (cmd) or PowerShell:
```cmd
# Run CLI mode with wallet in English
yona_light_miner_cli.exe --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en

# Run CLI mode with custom Node IP in English
yona_light_miner_cli.exe --cli --node 110.172.28.103:8080 --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en
```

---

### B. On Linux Terminal (CLI):
```bash
# Grant execution permissions
chmod +x yona_light_miner_cli

# Run CLI miner in English
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en

# Run CLI miner in Chinese (中文)
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang zh
```

---

### C. 1-Click Automated Setup on HiveOS Rigs:
Connect via SSH or Hive Shell on your HiveOS Rig and run:
```bash
bash hiveos_setup.sh --node 110.172.28.103:8080
```
*or via curl:*
```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
```

---

## 📊 5. REAL-TIME LOG MONITORING

Monitor live logs on Linux/HiveOS:
```bash
tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log
```

Console output upon Yona Hash activation (Height $\ge 38500$):
```text
[GPU-MINER] 🔨 Mining Block #38505 (Yona Hash) | Speed: 2.31 GH/s | Nonce: 0x4a1f...
[GPU-MINER] 🏆 Success! Found valid solution for Block #38505!
```
Your Light Mining client is 100% ready for Yona Hash!
