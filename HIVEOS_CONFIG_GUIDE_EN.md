# 🚀 HIVEOS GPU SOLO MINING CONFIGURATION GUIDE - YONACODE ($YGO)
> **Official Flight Sheet Configuration & Automated CLI Installation Guide for HiveOS**

This guide provides detailed instructions, visual configuration blueprints, and a 1-line automated CLI command to run GPU Solo Mining for **YonaCode ($YGO)** directly to your Node on **HiveOS**.

---

## 🖼️ 1. HIVEOS FLIGHT SHEET VISUAL CONFIGURATION BLUEPRINT

When configuring your Flight Sheet in the HiveOS Web Dashboard, enter the **exact values** as specified below:

### STEP 1: CREATE PLACEHOLDER WALLET & INITIALIZE FLIGHT SHEET
1. Go to **Wallets** $\rightarrow$ **Add Wallet**:
   * **Coin**: Enter `YGO`
   * **Address**: Enter any address starting with `0x` (e.g., `0x1111111111111111111111111111111111111111`)
   * **Name**: Enter `YGO Placeholder Wallet`
2. Go to **Flight Sheets** $\rightarrow$ Configure fields:

| Field | Configuration Value |
| :--- | :--- |
| **Coin** | Select **YGO** |
| **Wallet** | Select **YGO Placeholder Wallet** |
| **Pool** | Select **Configure in miner** |
| **Miner** | Select **Custom** (at the bottom of the miner list) |

---

### STEP 2: CONFIGURE ADVANCED SETTINGS IN "Setup Miner Config"
Click **Setup Miner Config** and fill in the fields exactly as illustrated in this blueprint:

```text
===========================================================================================
                          ⚙️ HIVEOS CUSTOM MINER CONFIGURATION
===========================================================================================

 [Miner name] ---------------------------->  yona_gpu_miner

 [Installation URL] --------------------->  https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip

 [Hash algorithm] ----------------------->  blake3

 [Pool URL] ----------------------------->  <YOUR_NODE_IP>:8080   (Example: 110.172.28.103:8080)

 [Wallet and worker template] ---------->  (LEAVE BLANK)

 [Extra config arguments] -------------->  (LEAVE BLANK)

===========================================================================================
```

> [!NOTE]
> **Solo Mining Mode:** The GPU miner connects directly to your Node's gRPC gateway. Block rewards for successfully solved blocks are credited directly to the Proposer wallet address configured on your Node.

---

## ⚡ 2. 1-LINE AUTOMATED CLI SETUP (AUTOMATED INSTALLER)

If you prefer setup via command-line, open **Hive Shell Start** or access **SSH** on your HiveOS worker and paste this single command:

```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
```
*(Replace `110.172.28.103:8080` with your Node's actual IP and Port).*

### What the Automated Script Does:
1. Creates standard HiveOS custom miner directory: `/hive/miners/custom/yona_gpu_miner`.
2. Downloads official release archive from GitHub Release v2.0.0.
3. Auto-generates configuration scripts (`h-manifest.conf`, `h-run.sh`, `h-stats.sh`) and sets execution permissions.
4. Activates HiveOS miner service and connects to your Node immediately.

---

## 📊 3. REALTIME LOG MONITORING

After starting your Flight Sheet (🚀 icon) or executing the CLI setup command, monitor your live mining status with:

```bash
tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log
```

When you see:
```text
[GPU-MINER] 🔨 Mining Block #38501 | Hashrate: 1.45 GH/s | Nonce: 0x4a1f...
```
Your HiveOS GPU Solo Mining is 100% operational!
