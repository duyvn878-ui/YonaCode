# ⚡ ULTRA-FAST BLOCKCHAIN LEDGER SYNC GUIDE - YONACODE ($YGO)
> **Official Bootstrap Ledger Snapshot Installation Guide for Instant P2P Sync**

This guide provides clear step-by-step instructions for **Option A (Full Bundled Release Packages)** and **Option B (Standalone Ledger Data Package `YonaCode_Ledger_Data.zip`)** to help your node synchronize at ultra-fast speeds on both Windows and Linux/VPS.

---

## 🚀 1. ULTRA-FAST 1-LINE CLI BOOTSTRAP (RECOMMENDED FOR LINUX / VPS)

Open your terminal or SSH session and paste this single command:

```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/sync_ledger_fast.sh | bash
```

### What the 1-Line Script Does:
1. Downloads `YonaCode_Ledger_Data.zip` directly from GitHub Release v2.0.0.
2. Unpacks the RocksDB state database into `./node/scl/`.
3. Cleans up temporary archives and sets optimal execution permissions (`chmod -R 755 node/scl`).
4. Prepares your node to launch and sync immediately.

---

## 📦 2. MANUAL DIRECT DOWNLOAD LINKS (GITHUB RELEASE v2.0.0)

Visit the official [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0) and choose the package that fits your setup:

* 🪟 **[Option A] Windows Full Bundle:** [YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip) (Contains `.exe` binaries + pre-packaged `node/scl/` data).
* 🐧 **[Option A] Linux Full Bundle:** [YonaCode_Linux.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip) (Contains Linux binaries + pre-packaged `node/scl/` data).
* 🌐 **[Option B] Standalone Ledger Data:** [YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip) (Contains only `./node/scl/` - For users who already compiled or have node binaries).

---

## 🛠️ 3. OPTION A INSTALLATION STEPS (FULL BUNDLE PACKAGES)

Option A is recommended for new users who do not have node binaries.

### Option A Steps for Windows:
1. Download `YonaCode_Windows.zip`.
2. Extract the archive into any folder (e.g., `C:\YonaCode\`).
3. Verify that the `node\scl` folder is present alongside `YonaCode.exe`.
4. Double-click `YonaCode.exe` or `scl_server.exe` to launch. The node will read the pre-packaged ledger and sync instantly.

### Option A Steps for Linux:
1. Download `YonaCode_Linux.zip`:
   ```bash
   wget https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip
   ```
2. Extract the archive:
   ```bash
   unzip YonaCode_Linux.zip -d /opt/yonacode/
   cd /opt/yonacode/
   chmod +x YonaCode scl_server
   ```
3. Run `./YonaCode` or `./scl_server`.

---

## 🛠️ 4. DETAILED OPTION B INSTALLATION STEPS (STANDALONE LEDGER DATA)

Option B is specifically designed for users who **already compiled node binaries from source** or **have existing node executables** and only wish to update or install the pre-built ledger database without re-downloading node software.

### Detailed Option B Steps for Windows:
1. Download the standalone ledger package **[YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip)**.
2. Navigate to your working directory containing `YonaCode.exe` (e.g., `D:\BTC\`).
3. Stop `YonaCode.exe` or `scl_server.exe` if currently running.
4. Extract the contents of `YonaCode_Ledger_Data.zip` **directly into your working directory**.
5. Verify exact path structure after extraction:
   ```text
   D:\BTC\                      <-- Working directory containing Node binaries
   ├── YonaCode.exe
   ├── scl_server.exe
   └── node\                    <-- Extracted node directory
       └── scl\                 <-- Directory containing RocksDB .log, .sst files
   ```
6. Relaunch `YonaCode.exe`. The node will instantly detect the RocksDB database in `node\scl\` and bypass block-0 rebuilding.

---

### Detailed Option B Steps for Linux / VPS / Headless:
1. Open Terminal / SSH in your current node directory containing `./YonaCode`:
   ```bash
   cd /path/to/your/yonacode/
   ```
2. Stop running node processes:
   ```bash
   pkill -f scl_server || pkill -f YonaCode
   ```
3. Download the standalone ledger archive:
   ```bash
   curl -sSL -o YonaCode_Ledger_Data.zip https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip
   ```
4. Unpack `./node/scl` directly into place:
   ```bash
   unzip -o YonaCode_Ledger_Data.zip -d ./
   rm -f YonaCode_Ledger_Data.zip
   ```
5. Set permissions for database directory:
   ```bash
   chmod -R 755 node/scl
   ```
6. Relaunch node:
   ```bash
   ./YonaCode --mining
   ```

---

## 🚦 5. VERIFY SYNC STATUS
Launch your node console or inspect startup logs. You should see:
```text
[SYNC-ENGINE] 🚀 Detected pre-packaged RocksDB state at block height #38500. Resuming fast P2P sync...
```
Your node is now running with Ultra-Fast Bootstrap active!
