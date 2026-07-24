# 🛠️ FLIGHT SHEET CREATION GUIDE FOR NEW L1 COINS (CUSTOM MINER)
> **HiveOS Flight Sheet configuration guide for L1 Custom Miners**

Please share this guide with partners or miners to help them configure their setups correctly using the following parameters:

---

## 📋 1. MAIN FLIGHT SHEET CREATION INTERFACE

Configure the basic options on the main Flight Sheet creation screen:

* **Coin**: Type your L1 coin ticker (e.g., `MYL1`) $\rightarrow$ Select **Create "MYL1"**.
* **Wallet**: Select your L1 wallet address.
* **Pool**: Select **Configure in miner** (Configure pool settings in the miner options).
* **Miner**: Select **Custom** (To load your custom miner binary archive).

---

## ⚙️ 2. SETUP MINER CONFIG DETAILS

Click the yellow **Setup Miner Config** button and fill in the fields exactly as follows:

| Field | Setting Value | Configuration Details |
| :--- | :--- | :--- |
| **Miner name** | Choose any name (e.g., `my-l1-miner`) | Name used to identify your custom miner on HiveOS |
| **Installation URL** | Paste the `.tar.gz` or `.zip` download link of your Miner | URL to download the pre-compiled Linux/HiveOS miner package |
| **Hash algorithm** | Select your L1 hash algorithm (or choose `custom`) | Hash algorithm used by the L1 blockchain |
| **Wallet and worker template** | `%WAL%` or `%WAL%.%WORKER_NAME%` | HiveOS will auto-fill your wallet address and worker name |
| **Pool URL** | Enter the IP Address and Port of your L1 Node/Stratum Server | Example: `123.45.67.89:8888` or `node.myl1project.com:8888` |
| **Extra config arguments** | Enter additional miner flags/arguments if required | Custom parameters (such as stress level, GPU mining configs, etc.) |

---

> [!TIP]
> **How to start mining:**
> 1. Once all configurations above are filled, click **Create Flight Sheet**.
> 2. Go to your Worker $\rightarrow$ Select the **Flight Sheet** tab $\rightarrow$ Locate the Flight Sheet you created and click the **Rocket** icon 🚀 to apply and start mining.
