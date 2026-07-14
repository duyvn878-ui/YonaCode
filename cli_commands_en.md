# 📖 YONACODE CLI & REPL COMMAND GUIDE

This document provides a comprehensive reference for all CLI (Command Line Interface) commands and the interactive REPL shell of the **YonaCode Go Client (v1.0)**, used to operate, manage nodes, and process secure transactions.

---

## 🌐 GLOBAL FLAGS

These flags can be appended to **any** command to override default execution settings:

| Flag | Default Value | Description |
| :--- | :--- | :--- |
| `--node-addr` | `localhost:18080` | gRPC server address of the target Node. |
| `--json` | `false` | Formats output in JSON instead of raw text. |
| `--lang` | `vnm` | Choose display language for UI text and logs (`vnm` or `eng`). |
| `--db-path` | `node` | Physical file path of the Node's RocksDB database. |

---

## 🖥️ 1. NODE MANAGEMENT COMMANDS (`yonacode node`)

Used to launch, monitor state, connect to peers, and perform offline recovery tasks.

### 🚀 Start Node Server
```bash
./yonacode node start [flags]
# Or: ./yonacode node run
```
* **Description:** Launch the Node Server. If port configurations are omitted, the Node automatically scans and binds to available TCP ports.
* **Supported Flags:**
  * `--mining`: Automatically start mining loop when node starts.
  * `--mining-device`: Mining device to use: `cpu` (default), `gpu` (NVIDIA GPU only), or `hybrid` (combines both CPU and GPU).
  * `--reward-address`: Wallet address to receive block rewards (Coinbase).
  * `--port`: HTTP port for running the Dashboard / Web UI.
  * `--p2p-port`: Listening port for horizontal peer-to-peer connections.
  * `--scl-port`: gRPC connection port to the Rust Core (`scl_server`).
  * `--peers`: Pre-defined list of bootstrap IP/Multiaddrs to connect.
  * `--sync-mode`: Initial sync strategy: `full` (ledger download) or `snap` (snapshot chaser).
  * `--max-tx-per-block`: Hard limit on transaction capacity per block.
  * `--write-log`: Enable flushing debug logs to the local storage.

### 📊 Query Node Status Dashboard
```bash
./yonacode node status
```
* **Description:** Display a concise terminal dashboard representing current metrics: Current Block Height, Active Peer Connections count, and local CPU Hashrate.

### ℹ️ Version Info
```bash
./yonacode node info
```
* **Description:** Show software version parameters (Vanguard Edition) and linked cryptographic libraries.

### 📡 Force Connect Peer
```bash
./yonacode node connect <address>
```
* **Description:** Mandatorily instruct the Node to establish a connection to a specific peer multiaddr.

### 🔧 Offline Recovery & Maintenance (`repair`)
> [!CAUTION]
> The Node Server must be **stopped** before running any `repair` subcommands to prevent RocksDB file lock issues and corruption.

* **Rollback Chain History:**
  ```bash
  ./yonacode node repair rollback --target <height>
  ```
  * **Description:** Manually force the database to rollback to the specified block height to resolve bad forks or sync loops.
* **Database Cleanup:**
  ```bash
  ./yonacode node repair cleanup --start <height> --end <height>
  ```
  * **Description:** Clean up orphaned and garbage ledger records within the specified block height range.
* **Ledger Purification:**
  ```bash
  ./yonacode node repair purify
  ```
  * **Description:** Completely purge the JMT State Root storage and rebuild it from the canonical genesis block upward.
* **State Resynchronization:**
  ```bash
  ./yonacode node repair resync --data-root <path>
  ```
  * **Description:** Resolve State Root divergence by reloading state data from a trusted source directory.

---

## 👛 2. WALLET MANAGEMENT COMMANDS (`yonacode wallet`)

Used to generate keys, restore wallets, query balances, and process secure transfers.

### ➕ Create a New Wallet
```bash
./yonacode wallet create --name <wallet_name> [flags]
```
* **Description:** Generate a secure wallet, outputting its public address and a 12-word recovery mnemonic seed phrase.
* **Supported Flags:**
  * `--password`: Define a PIN/password to encrypt local wallet keystore files.
  * `--passphrase`: Optional 13th seed word for advanced security derivation index.

### 🔄 Restore an Existing Wallet
```bash
./yonacode wallet restore --seed "<12_words>" --name <wallet_name> [flags]
```
* **Description:** Recover a wallet using standard BIP-39 mnemonic seed phrase.
* **Supported Flags:** `--password`, `--passphrase`.

### 📂 List Wallets
```bash
./yonacode wallet list
```
* **Description:** Scans local wallet storage directories and prints out names of all configured accounts.

### 💰 Query Wallet Balance
```bash
./yonacode wallet balance --address <address>
```
* **Description:** Interrogate network state to retrieve current spendable coin balance (GO) and account Nonce.

### 💸 Send Funds (Transfer GO)
```bash
./yonacode wallet send [flags]
```
* **Description:** Draft, sign, and broadcast a transaction. If run without parameters, CLI drops into a step-by-step **Guided UI** helper.
* **Supported Flags:**
  * `--from`: Local wallet name.
  * `--to`: Recipient wallet address.
  * `--amount`: Amount of GO to transfer.
  * `--password`: Password to unlock private keys.
  * `--yes`: Automate confirmation, skips double-check prompts.

### ❌ Delete Wallet
```bash
./yonacode wallet delete --address <address>
```
* **Description:** Unregister the wallet by removing the local keystore file from the device (logout).

---

## ⛏️ 3. MINING OPERATIONS (`yonacode mine`)

Adjust Proof of Work parameters on the CPU threads.

### ⛏️ Start Miner
```bash
./yonacode mine start --reward-address <address> [flags]
```
* **Description:** Trigger the miner loop on CPU to solve blocks and direct rewards to the configured recipient address.
* **Supported Flags:**
  * `--threads`: Define maximum CPU cores allocated to hashing (Default: 4).

### 🛑 Stop Miner
```bash
./yonacode mine stop
```
* **Description:** Instantly terminate local CPU mining loops, freeing up machine hardware resources.

### 📈 Get Mining Status
```bash
./yonacode mine status
```
* **Description:** Check miner state (`ACTIVE` / `PAUSED`) and inspect current real-time Hashrate (KH/s or MH/s).

### ⛏️ Standalone Miners
In addition to triggering mining via the Node's CLI, you can directly launch the standalone miner executables to connect and contribute hashing power to the main Node:

#### 💻 1. Standalone CPU Miner (`genz_miner`)
* **How to run:**
  ```bash
  ./genz_miner --port <node_scl_port>
  ```
  *(Default SCL port is RPC port + 42000. For example, if the Node RPC port is 8080, the target SCL connection port is 50080)*

#### ⚡ 2. Standalone GPU Miner (`yona_gpu_miner`)
*Note: Supports CUDA-compatible NVIDIA graphics cards only.*
* **Verify CUDA compatibility:**
  ```bash
  ./yona_gpu_miner --check
  ```
* **How to run:**
  ```bash
  ./yona_gpu_miner [node_ip_address] [node_rpc_port]
  ```
  *(Example connecting to a local Node: `./yona_gpu_miner 127.0.0.1 8080`)*

---

## 🔍 4. LEDGER RAW DATA QUERIES (`yonacode query`)

Read database structures directly from the RocksDB backend. Works offline.

> [!TIP]
> You may use the universal `--path <db_path>` flag within this category to inspect alternate database folders.

* **Inspect Block details:**
  ```bash
  ./yonacode query block <height_or_hash>
  ```
  * **Description:** Read and decode full block schemas (proposer, State Root, list of txs).
* **Get Transaction details:**
  ```bash
  ./yonacode query tx <txid>
  ```
  * **Description:** Query a specific transaction by its ID (TxID) to view finality state and input/output values.
* **Offline Balance Query:**
  ```bash
  ./yonacode query balance <address>
  ```
  * **Description:** Fallback directly to reading local RocksDB state files to check balances if Node Server is offline.
* **Economic Audit:**
  ```bash
  ./yonacode query supply
  ```
  * **Description:** Audit the tokenomics by comparing the actual supply on DB against theoretical algorithmic curve to detect any inflation anomalies.
* **Inspect Mempool:**
  ```bash
  ./yonacode query mempool
  ```
  * **Description:** List unconfirmed transactions currently queued for block proposing.
* **Scan Non-zero Balances:**
  ```bash
  ./yonacode query scan
  ```
  * **Description:** Walk the entire database trie and print out all address keys holding a balance > 0.
* **Get Database Headers:**
  ```bash
  ./yonacode query root
  ```
  * **Description:** Display the current state Merkle root hash and highest finalized database block height.

---

## 🛠️ 5. UTILITY COMMANDS (`yonacode util`)

Static checking tools and helper utilities.

* **Compute Blake3 Hash:**
  ```bash
  ./yonacode util hash <text_string>
  ```
  * **Description:** Computes and prints the raw Blake3 hash checksum of any plain text.
* **Address Format Validation:**
  ```bash
  ./yonacode util validateaddress <address>
  ```
  * **Description:** Checks if a hex-encoded string matches format rules of a valid YonaCode public key (32 bytes).

---

## 💻 6. INTERACTIVE REPL SHELL

If you double-click the `yonacode.exe` executable directly (or run `./yonacode` from command line with no subcommands or arguments), the terminal transitions into a REPL loop with a prompt: `cli_yona_code >`

Inside this session, the following quick commands are recognized:

* `help`: Prints command manual directory.
* `status` (or `info`): Check Node Dashboard state.
* `wallets`: Print out list of local wallet names.
* `send`: Triggers the Guided Send transaction interface.
* `exit` (or `quit`): Safely release RocksDB file locks, stop Node, and quit process.

---

## 🐧 7. DEPLOYING & RUNNING NODE ON LINUX (VPS)

To deploy and execute the YonaCode Go node on a Linux server (e.g., Ubuntu/Debian VPS), ensure you copy all 4 compiled executable binaries (`YonaCode`, `scl_server`, `genz_miner`, `cli_yona_code`) into a single working directory (for instance, `/root/btc_node/`).

### ⚙️ Step 1: Assign Executable Permissions (Crucial)
By default, uploading binary files to Linux might strip their executable flags. You must enforce the execution permissions using the following command:
```bash
cd /root/btc_node
chmod +x YonaCode scl_server genz_miner cli_yona_code
```

### 🚀 Step 2: Run the Node

#### Method 1: Manual Run (For testing and real-time console logs)
*Note: You must explicitly `cd` into the folder holding the binaries before execution. This ensures the Go Core can properly locate and spawn the sibling Rust Core process (`scl_server`).*
```bash
cd /root/btc_node
./YonaCode node start --port 8080 --p2p-port 9000 --db-path ./data
```
To run the node with mining activated immediately:
```bash
./YonaCode node start --port 8080 --p2p-port 9000 --db-path ./data --mining --reward-address <your_reward_address_hex> --miner-pin <your_wallet_pin>
```

#### Method 2: Systemd Daemon Service (Recommended for 24/7 VPS uptime)
For automated background execution, self-healing, and automatic startup during VPS reboot, configure a systemd daemon service:

1. Create a service file:
   ```bash
   nano /etc/systemd/system/yonacode-node.service
   ```
2. Paste the following configuration block (ensure you supply your custom Cloudflare token and domain settings):
   ```ini
   [Unit]
   Description=YonaCode Genz Seed Node Service
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/root/btc_node
   ExecStart=/root/btc_node/YonaCode node start --port 8080 --p2p-port 9000 --db-path /root/btc_node/data
   # Cloudflare environment credentials for automatic DDNS synchronization (if in Guardian Mode)
   Environment="CF_TOKEN=3BnkeMrBaYD5bN0CO3sfsZvlb6um93NpP4yxz41v"
   Environment="SEED_DOMAIN=seed.ghostcoi.com"
   Restart=always
   RestartSec=5
   LimitNOFILE=65535

   [Install]
   WantedBy=multi-user.target
   ```
3. Press `Ctrl + O` -> `Enter` to save, and `Ctrl + X` to exit the text editor.
4. Activate and start the daemon:
   ```bash
   systemctl daemon-reload
   systemctl enable yonacode-node.service
   systemctl start yonacode-node.service
   ```
5. Follow live stdout/stderr log streams:
   ```bash
   journalctl -u yonacode-node.service -f
   ```

---

## ⛏️ 8. MINING POOL CLI GUIDE

YonaCode supports a complete Mining Pool solution. You can operate your own pool on a VPS server or connect nodes/workers to a pool.

### 📡 Running a Pool Server on a VPS (For Pool Owners)
To activate the Mining Pool module on your YonaCode Node Server:
```bash
./YonaCode node start --pool-enable --pool-address <pool_wallet_address> --pool-key <pool_private_key> --pool-fee 0.01 --pool-diff-mult 100
```
* **Pool Command Flags:**
  * `--pool-enable`: Enables the pool server interface.
  * `--pool-address`: Wallet address designated to receive the automatic 1% pool operator fee.
  * `--pool-key`: Private key of the pool wallet to sign automated payout transactions.
  * `--pool-fee`: Fixed pool fee percentage (e.g. `0.01` for 1%).
  * `--pool-diff-mult`: Difficulty multiplier discount for pool shares (e.g. `100` times easier than the network).

### ⛏️ Running a Client Miner connected to a Pool (For Workers)
Workers can connect directly to the Pool VPS using the main CLI:
```bash
./YonaCode pool-mine <your_reward_address> [flags]
```
> [!NOTE]
> The mining command will **automatically connect** to the default main Pool IP address of the network (**`110.172.28.103`**) by default. Users do not need to provide the `--url` flag unless they are connecting to a custom/different pool.

* **Miner Command Flags:**
  * `-d` / `--device`: Mining device to use: `cpu` (default) or `gpu` (NVIDIA CUDA GPU miner).
  * `-t` / `--threads`: Number of CPU threads to allocate (only applicable for `cpu`).
  * `-u` / `--url`: Manual Pool URL override (only used when specifying a custom pool instead of the default).

* **Automatic Connection Examples (Recommended):**
  * Mine with NVIDIA GPU (Automatically connects to your main Pool VPS):
    ```bash
    ./YonaCode pool-mine --device gpu 0xYourWalletAddress
    ```
  * Mine with CPU using 4 threads (Automatically connects to your main Pool VPS):
    ```bash
    ./YonaCode pool-mine --device cpu --threads 4 0xYourWalletAddress
    ```

* **Manual Custom Pool Connection Example:**
  ```bash
  ./YonaCode pool-mine --device gpu --url 192.168.1.100:8080 0xYourWalletAddress
  ```

