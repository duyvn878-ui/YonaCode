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

## 🚦 4. VERIFY OPERATION

Since all integration scripts (`h-manifest.conf`, `h-run.sh`, `h-stats.sh`) and the miner binary are pre-packaged at the root of the official `YonaCode_Linux.zip` archive, HiveOS will automatically download, extract, and configure the custom miner to the path `/hive/miners/custom/yona_gpu_miner/` upon applying the Flight Sheet. No manual shell setup is required.

To verify operation on your rig:
1. Apply the Flight Sheet to your rig by clicking the **Rocket** icon 🚀.
2. Watch the live console output on your worker:
   ```bash
   tail -f /var/log/miner/custom/yona_gpu_miner.log
   ```
3. If you see block templates loading correctly:
   `[GPU-MINER] 🔨 Mining Block #...`
   Your Solo Mining is fully working!

