# 📖 HIVEOS MINER ADMINISTRATION & TROUBLESHOOTING GUIDE
> **HiveOS Custom Miner Administration & Troubleshooting Reference Guide**

This document summarizes all important CLI (Command Line Interface) commands and system paths for managing, monitoring, updating drivers, and troubleshooting the Solo **YonaCode GPU Miner** on the **HiveOS** operating system.

---

## ⚡ 1. MINER CONTROL COMMANDS
When accessing your mining rig via **Hive Shell** or an **SSH** connection, you can use the built-in HiveOS CLI commands to control the miner:

| CLI Command | Function | Detailed Description |
| :--- | :--- | :--- |
| `miner` | View Console Screen | Opens the live console screen to monitor hashrate and output of the miner |
| `miner start` | Start Miner | Instructs the system to start the miner according to the current Flight Sheet |
| `miner stop` | Stop Miner | Safely stops the currently running miner process |
| `miner restart` | Restart Miner | Terminates the active miner process and starts a fresh one |
| `miner log` | View System Log | Displays the HiveOS custom miner management log |

---

## 🔍 2. SYSTEM PATHS & LOG FILES
To debug and verify the actual runtime status of the miner, please refer to the following directories and files:

### 📂 Miner Working Directory
* **Physical Path:** `/hive/miners/custom/yona_gpu_miner/`
* **Directory Contents:** Contains the `yona_gpu_miner` executable and the control scripts:
  * [`h-manifest.conf`](./10_miner_hiveos/h-manifest.conf): Manifest configuration file.
  * [`h-run.sh`](./10_miner_hiveos/h-run.sh): Miner runner script.
  * [`h-stats.sh`](./10_miner_hiveos/h-stats.sh): Statistics/telemetry collector script.

### 📝 Reading Miner Logs
The miner's output logs are saved directly to the HiveOS log directory:
* **Log File Path:** `/var/log/miner/custom/yona_gpu_miner.log`
* **Command to monitor logs in real-time:**
  ```bash
  tail -f /var/log/miner/custom/yona_gpu_miner.log
  ```
* **Command to view the last 50 lines of logs:**
  ```bash
  tail -n 50 /var/log/miner/custom/yona_gpu_miner.log
  ```

---

## 🖥️ 3. PROCESS & GPU TELEMETRY MONITORING

### 📊 Check Background Process Status
Verify if the miner executable is running and view its Process ID (PID):
```bash
ps aux | grep yona_gpu_miner
```

### 🌡️ GPU Hardware Monitoring
* **For NVIDIA GPUs:**
  View power consumption, temperature, fan speed, and VRAM utilization:
  ```bash
  nvidia-smi
  ```
  To monitor real-time GPU statistics updated every second:
  ```bash
  watch -n 1 nvidia-smi
  ```
* **For AMD GPUs:**
  View AMD GPU status via the ROCm tool:
  ```bash
  rocm-smi
  ```
* **List all GPUs recognized by HiveOS:**
  ```bash
  gpu-detect list
  ```

---

## 🔧 4. GPU DRIVER UPDATE FALLBACKS

If the miner requires a newer CUDA or ROCm version than the one installed in your current HiveOS image, execute the following commands:

### 🟢 1. Update NVIDIA Drivers
```bash
# List all available NVIDIA driver versions on the HiveOS server
nvidia-driver-update --list

# Upgrade to the latest recommended NVIDIA driver version
nvidia-driver-update

# Install a specific driver version (e.g., version 535.113.01)
nvidia-driver-update 535.113.01
```

### 🔴 2. Update AMD Drivers (OpenCL/ROCm)
Because AMD drivers are tightly coupled with the Linux kernel, the best way is to update the entire HiveOS image:
```bash
# Update the OS kernel and AMD drivers
selfupdate
```

---

> [!WARNING]
> **TROUBLESHOOTING HASH RATE = 0:**
> 1. Check if the log file is actively updating using `tail -f`.
> 2. If the logs show: `Failed to fetch work (Status: 503)`, check if your **YonaCode Node** (configured at the Pool URL address) is online and fully synchronized with the blockchain.
> 3. Verify if the miner process was terminated due to out-of-memory (OOM) errors by running: `dmesg -T | grep -i oom`.
