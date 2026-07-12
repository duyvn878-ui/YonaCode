# YonaCode Go 🚀
> **Coded Manifesto: A Countervailing Power**  
> *Minimalist - Immutable - Ultralight*

![Version](https://img.shields.io/badge/Version-V1.0_Vanguard_Elite-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![Rust](https://img.shields.io/badge/Rust-1.75+-dea584?logo=rust)
![License](https://img.shields.io/badge/License-Open_Source-green)

**YonaCode Go** (Ticker: **$YGO / GO**) is not just a new layer-1 blockchain; it is a "Coded Manifesto" for Generation Z. The project is explicitly designed to create a new generation of "digital gold"—a pure store of value and speculative instrument, fundamentally different from a transactional currency—acting as a countervailing power against financial centralization and inflation. Its architecture has been completely built from scratch using **Rust** (Consensus Core) and **Go** (P2P Network).

Author / Founder: **Vo Nhat Thien**

> [!IMPORTANT]
> **Announcement regarding project status:**  
> Actually, the features were 100% complete from the day they launched. I am only mentioning it today because I was afraid people would think the project is dead, but in reality, I have been diligently posting. It's just that this group updates information that I haven't finished yet, so I don't know what to do with it. That's why I'm posting this to prevent people from misunderstanding that the project is dead.

> [!TIP]
> **Pre-packaged Ledger Data:**  
> The zip file already contains the `node` folder with the ledger data, so you can synchronize extremely quickly without having to download it via p2p.
> 
> Please note the configuration required for the node to accept this ledger. Although the code is already designed to recognize it automatically, you generally just need to extract the zip file, open the extracted folder, and launch it to get it working.
> It will be a bit more challenging on Linux—if you are running it headless (without a GUI), you will need to manually point the node to the ledger directory.

---

## 🎯 Vision & Strategic Positioning

### 1. Competing with Bitcoin Instead of Tech Altcoins (Why We Don't Fight Solana or Sui)
If it comes to technology, we absolutely cannot compete with projects like Solana or Sui. Since I will have to leave, after the project is decentralized there will be no one to lead it. Therefore, my only path is to compete with Bitcoin. Running after technology requires continuous upgrades, but once I am gone, who will be responsible for upgrading it? Thus, choosing to compete with Bitcoin is the most logical choice. We want to create a new generation of digital gold that functions more clearly as a store of value, positioned to compete with Bitcoin.

### 2. A Pure Tool for Speculation (Why We Need a "New Bitcoin")
When asked: *"Why do we need a new Bitcoin?"*
The answer is profit and competition. Currently, the competition among altcoins is too fierce. A "new Bitcoin" can obviously provide a much higher potential profit margin compared to the legacy Bitcoin (which has already grown too large), while avoiding the fierce competition of current altcoins.

> [!WARNING]
> **RISK WARNING:** We will not make any explicit promises or guarantees about profits—doing so would be deceitful. The risks are always enormous. Participating in this project may very well be a crazy gamble, like throwing money out the window, and becoming one of the most risky speculative ventures in history.
> However, it cannot be denied that blockchain is the ultimate speculative tool in human history. If only judged by technology and degree of decentralization, how can Bitcoin compare to Monero? Yet Bitcoin remains King because it is the ultimate symbol of global speculative belief. Legacy Bitcoin is already too large to be an attractive speculative asset from an economic perspective, so our project's concept forces us to compete with Bitcoin.

### 3. Long-term Goal: Decentralization (Founder Exit in 6 Months)
To achieve true decentralization:
* **Founder Exit:** Mr. Duy will completely step away from the project **6 months** after launch (which can be extended up to a maximum of 2 times).
* **True Autonomy:** The purpose of this exit is for the project to achieve true decentralization, remaining entirely independent of the founder's influence and avoiding any personal dependence.

## 🌟 Core Pillars & Philosophy

YonaCode resolves the fatal flaws of traditional consensus mechanisms through proprietary and revolutionary technologies:

### 🛡️ 1. VNT Consensus: True Immutability Guided by the "Invisible Hand"
At its core, VNT Consensus is not a new cryptographic algorithm, but a philosophical breakthrough: embedding an economic theory (The Invisible Hand) directly into the consensus rules of the Code. This enables a PoW system to achieve "true immutability" without relying on the massive, tyrannical energy consumption (Hashrate) typical of legacy systems.

* **Market as the Arbiter, Not the Code**: Legacy systems utilize automated algorithms to wipe out the assets of a weaker fork during macroeconomic disruptions (such as undersea fiber-optic cable cuts that split the network). YonaCode does not allow machines to make such executive decisions. The determination of the "canonical chain" is left to the "Invisible Hand"—meaning the recognition and valuation by the Community, Miners, and Capital Inflow.
* **Absolute Asset Preservation**: User assets remain intact across parallel forks; no one loses their funds unjustly due to a cold, emotionless line of code.
* **5-Block Firewall & Ultra-Low Activation Rate**: To allow the Invisible Hand to operate safely, the system uses mathematics to hard-lock all transactions that are deeper than 5 blocks (~6 minutes). No matter how massive, malicious hashrate cannot rewrite finalized history.
* **Proven by Empirical Data**: The probability of the network needing to rely on the Invisible Hand is extremely low. Over 10 years of operational history from high-speed PoW networks (like Dogecoin with a 60s block time) shows that natural forks caused by network latency only go 1 to 2 blocks deep. A natural fork exceeding 5 blocks is practically impossible (0%).

Therefore, the 5-block rule acts as a redundant safety buffer. The Invisible Hand is essentially an "Emergency Evacuation Protocol," invoked only during Black Swan disasters (such as a global internet blackout). Machines handle short-term deviations, while major crises are left to the free market.

👉 **Note**: To quickly review technical counterarguments or deep dives into the network split paradox and Game Theory, please refer to our Whitepaper or drop your questions right below in the comment section!

### ⚡ 2. Ultralight Architecture (48H Great Purge)
Automatically deletes detailed Block Body data after 48 hours. Only Block Headers and State Roots are retained. Running a Full Node is now a basic right for everyone using a standard personal computer hard drive.

### 🧬 3. Pure Account Model & JMT
Eliminates the fragmented UTXO model. Utilizes the **Jellyfish Merkle Tree (JMT)** for state storage. Combined with the Anti-Bloat Shield (charging a 1,000 VNT creation fee for new wallets) to prevent state bloat.

### ⛏️ 4. Custom Blake3-PoW Algorithm
Highly optimized for multi-threading and ultra-fast verification. Uses a *Context String* mechanism to invalidate old ASICs, triggering a fair hardware race for new miners.

---

## ⚙️ Tech Stack

The system is engineered based on a **3-Tier Tactical Model**:
- **Consensus & Mathematics Core (Rust)**: Handles all signature verification (Ed25519-dalek), Hashing (Blake3), State Storage (RocksDB + JMT), and Difficulty Adjustment (LWMA DAA).
- **P2P Network & Telecommunications (Go)**: Utilizes Libp2p, GossipSub, NAT traversal (UPnP/NAT-PMP/STUN/Hole Punching), and DNS Seeders.
- **IPC Communication**: Rust and Go communicate at ultra-high speeds via **gRPC / Unix Sockets / Named Pipes** (using Tonic + Protobuf).
- **Interfaces & APIs**: RESTful APIs and a static Web UI (React) embedded directly into the Go executable.

---

## 💰 Cryptoeconomics

- **Max Total Supply**: `20,000,000 GO` (Permanently hard-capped)
- **Sub-units**: `1 GO = 100,000,000 VNT`
- **Block Time**: `75 seconds`
- **Emission Schedule**: A fair cross-generational distribution schedule spanning **300 years**. Absolutely no long-term inflation after reaching the PoW peak (Year 5).
- **Reward Mechanism**: *Single Winner* - The miner who successfully mines a block receives 100% of the block reward and all transaction fees.

---

## 📂 Project Structure

```text
YonaCode/
├── 0_shared_lib/         # (Rust) SCL (Shared Calculation Library): RocksDB, JMT, PoW, Crypto
├── 1_proto_defs/         # (Protobuf) Data structure definitions for Go and Rust
├── 2_miner_core/         # (Go/Rust) Independent gRPC Miner Station & Go-Rust Bridge Logic
├── 5_node_p2p/           # (Go) P2P Network (Libp2p), Sync Engine, Mempool, NAT, Banning
├── 6_user_interface/     # (Go) CLI App, HTTP REST API, SSE, Static Web UI
├── 8_miner_gpu/          # (C++/CUDA) High-performance GPU Miner (Only supports NVIDIA cards)
└── go.mod / Cargo.toml   # Dependency management

> [!WARNING]
> Trình đào GPU Miner (`8_miner_gpu`) **chỉ hỗ trợ card đồ họa NVIDIA** (GTX/RTX series). Không hỗ trợ card đồ họa AMD, Intel hay các dòng card khác.

🛠️ Build & Installation Guide
1. Prerequisites
Go (v1.21 or higher recommended)
Rust & Cargo (Latest stable version)
C/C++ Compiler (GCC/MinGW for Windows, build-essential for Linux)
Protoc (Automatically handled via protoc-bin-vendored in Rust).
2. Build the Rust Core (SCL Core & Miner)
The Rust Core is responsible for heavy database operations and cryptographic validations.
code
Bash
cd 0_shared_lib
cargo build --release
(The executables scl_server and genz_miner will be generated inside 0_shared_lib/target/release)
3. Build the Go Node (CLI & P2P)
code
Bash
cd 6_user_interface/cmd/genz
go build -o yonacode .
🚀 CLI Usage Guide
The program provides a powerful Command Line Interface (CLI) to manage the Node and Wallets.
Start the Node
code
Bash
# Start a basic Node (Verify-only mode)
./yonacode node start

# Start the Node with Mining enabled (Full-mining mode)
./yonacode node start --mining --reward-address <YOUR_WALLET_ADDRESS>
Wallet Management
code
Bash
# Create a new wallet
./yonacode wallet create --name mywallet --password "123456"

# List local wallets
./yonacode wallet list

# Check balance
./yonacode wallet balance --address <WALLET_ADDRESS>

# Send a transaction
./yonacode wallet send --from mywallet --to <RECEIVER_ADDRESS> --amount 10.5 --password "123456"
Query the Ledger
code
Bash
# Check network and node status
./yonacode node status

# Query a block by height or hash
./yonacode query block 100

# Query a transaction
./yonacode query tx <TX_HASH>

# Audit actual total supply
./yonacode query supply
Repair & Maintenance Tools
The system provides a repair command group for offline database troubleshooting:
code
Bash
# Force rollback to a specific block height
./yonacode node repair rollback --target 5000

# Manually purge historical data
./yonacode node repair cleanup --start 0 --end 1000

# Purify the Ledger (Rebuild the entire JMT State from historical Block Headers)
./yonacode node repair purify
🛡️ Security & Anti-Attack Mechanisms
YonaCode implements a strict, multi-layered defense system:
Time-Warp Shield (MTP-11): A time firewall preventing malicious miners from manipulating the difficulty adjustment algorithm.
Anti-Spam Mempool: Features a Token Bucket algorithm for rate limiting, smart tail-eviction, and an Exchange Batch Protocol (EBP) for centralized exchanges.
Progressive Banning: A progressive IP and PeerID banning system combined with a Leaky Bucket to neutralize P2P network DDoS attacks.
Deep Reorg Protection: The Rust Core strictly rejects any attempt to reorganize the blockchain deeper than the 5-block Finality Firewall.
📜 License
This project is open-source. Please refer to the LICENSE file (if available) for more details.
"We don't have the free time to fix a machine that has lost its ambition. We are creating a completely new order."
— The Manifesto of Generation Z
