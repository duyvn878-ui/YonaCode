# ⚡ ULTRA-FAST LEDGER SYNC GUIDE FOR YONACODE BLOCKCHAIN ($YGO)
> **Pre-packaged Bootstrap Ledger Snapshot & 1-Line CLI Command to Skip Long Genesis Sync**

This document provides step-by-step instructions for 2 options: **Option A (Full Integrated Package)** and **Option B (Independent Ledger Package `YonaCode_Ledger_Data.zip`)** for fast node deployment on Windows and Linux/VPS.

---

## 🚀 1. 1-LINE FAST CLI SYNC (FOR LINUX / VPS)

Open your Terminal or SSH session on your VPS/server and run the following **single command**:

```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/sync_ledger_fast.sh | bash
```

---

## 📦 2. COMPLETE FILE LISTINGS IN RELEASE PACKAGES (TAG `v2.0.0`)

Visit the [GitHub Release v2.0.0](https://github.com/duyvn878-ui/YonaCode/releases/tag/v2.0.0) page and choose the appropriate package:

### 🪟 Package 1: Full Windows Package (`YonaCode_Windows.zip`)
Includes all 11 binary/DLL files + RocksDB ledger database directory `node/scl/`:

```text
YonaCode_Windows.zip
├── YonaCode.exe               <-- Main Node UI & Browser Controller
├── scl_server.exe             <-- Ledger Sync Core & P2P Engine
├── genz_miner.exe             <-- Dedicated CPU Miner
├── cli_yona_code.exe          <-- CLI Management Tool
├── yona_gpu_miner.exe         <-- GPU Miner (Yona Hash Anti-ASIC)
├── yona_gpu_setup.exe         <-- GPU Configuration Setup Tool
├── yona_wallet_server.exe     <-- Wallet Server & Transaction Gateway
├── btc_genz_scl.dll           <-- Rust FFI Consensus Engine Library
├── msvcp140.dll               <-- Microsoft C++ Runtime Library
├── vcruntime140.dll           <-- Microsoft C++ Runtime Library
├── vcruntime140_1.dll         <-- Microsoft C++ Runtime Library
└── node/
    └── scl/                   <-- RocksDB Database (.sst, .log, MANIFEST)
```

---

### 🐧 Package 2: Full Linux Package (`YonaCode_Linux.zip`)
Includes all 7 Linux binary executables + RocksDB ledger database directory `node/scl/`:

```text
YonaCode_Linux.zip
├── YonaCode                 <-- Main Node UI & Browser Controller
├── scl_server               <-- Ledger Sync Core & P2P Engine
├── genz_miner               <-- Dedicated CPU Miner
├── cli_yona_code            <-- CLI Management Tool
├── yona_gpu_miner           <-- GPU Miner (Yona Hash)
├── yona_gpu_setup           <-- GPU Setup Tool
├── yona_wallet_server       <-- Wallet Server & Transaction Gateway
└── node/
    └── scl/                 <-- RocksDB Database (.sst, .log, MANIFEST)
```

---

### 🌐 Package 3: Independent Ledger Data (`YonaCode_Ledger_Data.zip`)
Contains **only the ledger database folder `node/scl/`** (For users who already have existing `.exe` binaries or built from source):

```text
YonaCode_Ledger_Data.zip
└── node/
    └── scl/                 <-- RocksDB Database (.sst, .log, MANIFEST)
```

---

## 🛠️ 3. INSTRUCTIONS FOR OPTION A (FULL PACKAGE INSTALLATION)

### Windows Option A Setup:
1. Download **[YonaCode_Windows.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Windows.zip)**.
2. Extract the ZIP to any directory (e.g. `D:\BTC\`).
3. Verify that all 11 binary/DLL files and the `node\scl\` folder are present at the root level.
4. Double-click `YonaCode.exe` or `scl_server.exe` to run.

---

## 🛠️ 4. INSTRUCTIONS FOR OPTION B (INDEPENDENT LEDGER DATA)

### Windows Option B Setup:
1. Download **[YonaCode_Ledger_Data.zip](https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip)**.
2. Navigate to your application root directory (e.g. `D:\BTC\`).
3. Stop `YonaCode.exe` or `scl_server.exe` if running.
4. Extract `YonaCode_Ledger_Data.zip` **directly into your application root directory**.
5. Verify the file structure after extraction:
   ```text
   D:\BTC\                               <-- Root application folder
   ├── YonaCode.exe
   ├── scl_server.exe
   ├── genz_miner.exe
   ├── cli_yona_code.exe
   ├── yona_gpu_miner.exe
   ├── yona_wallet_server.exe
   ├── btc_genz_scl.dll
   ├── msvcp140.dll
   ├── vcruntime140.dll
   ├── vcruntime140_1.dll
   └── node\                             <-- Extracted node directory
       └── scl\                          <-- Extracted RocksDB database folder
   ```
6. Restart `YonaCode.exe` or `scl_server.exe`.

---

## 🚦 5. SYNC STATUS VERIFICATION
Check the node console log. You should see:
```text
[SYNC-ENGINE] 🚀 Detected pre-packaged RocksDB state at block height #38500. Resuming fast P2P sync...
```
