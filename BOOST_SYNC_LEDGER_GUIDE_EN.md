# ⚡ ULTRA-FAST BLOCKCHAIN LEDGER SYNC GUIDE - YONACODE ($YGO)
> **Official Bootstrap Ledger Snapshot Installation Guide for Instant P2P Sync**

This guide provides instructions, direct download links, and 1-line automated CLI commands to bootstrap your **YonaCode ($YGO)** node with pre-packaged RocksDB ledger data, bypassing lengthy Genesis Block synchronization.

---

## 💡 HOW ULTRA-FAST LEDGER BOOTSTRAP WORKS
When initializing a new node, standard blockchain synchronization requires rebuilding the entire transaction history block-by-block from Block 0 (Genesis), which consumes significant time and network bandwidth.

By downloading the pre-packaged **Bootstrap Ledger Snapshot** (`node/scl/`):
* ✅ Your node instantly loads verified state headers and RocksDB tables from high-speed release servers.
* ✅ Skips 100% of historical block generation and past validation overhead.
* ✅ Node starts operating immediately, only fetching the latest recent blocks from P2P peers.

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

If you prefer downloading packages manually, choose one of the official options from [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0):

### Option A: Complete Release Packages (Includes Pre-Packaged `node/scl`)
* 🪟 **Windows Bundle:** [YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip) (Contains executables + `node/scl/` data).
* 🐧 **Linux Bundle:** [YonaCode_Linux.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Linux.zip) (Contains Linux binaries + `node/scl/` data).

### Option B: Standalone Ledger Data Only (If you already have binaries)
* 🌐 **Standalone Ledger Archive:** [YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip) (Contains only `./node/scl/`).

---

## 🛠️ 3. MANUAL INSTALLATION STEPS

### For Windows:
1. Download `YonaCode_Windows.zip` (or `YonaCode_Ledger_Data.zip`).
2. Extract the archive into your working folder.
3. Ensure directory structure matches:
   ```text
   C:\YourFolder\
   ├── YonaCode.exe
   ├── scl_server.exe
   └── node/
       └── scl/          <-- (Database directory containing .log, .sst files)
   ```
4. Run `YonaCode.exe` or `scl_server.exe`. The node will detect `./node/scl/` and resume fast sync.

---

## 🚦 4. VERIFY SYNC STATUS
Launch your node console or inspect startup logs. You should see:
```text
[SYNC-ENGINE] 🚀 Detected pre-packaged RocksDB state at block height #38500. Resuming fast P2P sync...
```
Your node is now running with Ultra-Fast Bootstrap active!
