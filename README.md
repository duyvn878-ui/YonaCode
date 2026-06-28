# YonaCode Go 🚀
> **Coded Manifesto: A Countervailing Power**  
> *Minimalist - Immutable - Ultralight*

![Version](https://img.shields.io/badge/Version-V1.0_Vanguard_Elite-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![Rust](https://img.shields.io/badge/Rust-1.75+-dea584?logo=rust)
![License](https://img.shields.io/badge/License-Open_Source-green)

**YonaCode Go** (Ticker: **$YGO / GO**) is not just a new layer-1 blockchain; it is a "Coded Manifesto" for Generation Z. The project is designed to replace (not inherit) the outdated legacy systems, acting as a countervailing power against financial centralization and inflation. Its architecture has been completely built from scratch using **Rust** (Consensus Core) and **Go** (P2P Network).

Author / Founder: **Vo Nhat Thien**

---

## 🌟 Core Pillars & Philosophy

YonaCode resolves the fatal flaws of traditional consensus mechanisms through proprietary and revolutionary technologies:

1. **Vo Nhat Thien Consensus (VNT Consensus)**: 
   - Integrates the **"5-Block Finality Firewall"**. It permanently strips Hashrate of its retroactive power. Once a block sinks 5 layers deep, it is frozen permanently by code laws. No 51% attack can ever reverse it.
2. **Ultralight Architecture**:
   - **48H Great Purge**: Automatically deletes detailed Block Body data after 48 hours. Only Block Headers and State Roots are retained. Running a Full Node is now a basic right for everyone using a standard personal computer hard drive.
3. **Pure Account Model & JMT**:
   - Eliminates the fragmented UTXO model. Utilizes the **Jellyfish Merkle Tree (JMT)** for state storage. Combined with the Anti-Bloat Shield (charging a 1,000 VNT creation fee for new wallets) to prevent state bloat.
4. **The Invisible Hand Theory**:
   - In the event of a global submarine cable cut or widespread network split, the system refuses to use blind algorithms to wipe out users' assets on the weaker branch. The decision of which chain is the main chain belongs to the **Free Market** through pricing and social consensus.
5. **Custom Blake3-PoW Algorithm**:
   - Highly optimized for multi-threading and ultra-fast verification. Uses a *Context String* mechanism to invalidate old ASICs, triggering a fair hardware race for new miners.

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
└── go.mod / Cargo.toml   # Dependency management
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
