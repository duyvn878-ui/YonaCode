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

### 🛡️ 1. Vo Nhat Thien Consensus Protocol (VNT Consensus 2.0)
The Vo Nhat Thien Consensus Protocol (VNT Consensus 2.0) is the core architecture governing the macro network of YonaCode ($YGO$). It is a layered consensus system that uses dynamic mathematics to completely resolve P2P infrastructural vulnerability. The ultimate goal of VNT Consensus is not to establish a rigid set of locking rules, but to achieve a supreme form of security through fragmented Proof-of-Work energy combined with social intent: **MAD Immutability**.

#### A. Layered Ledger Architecture and Dynamic Checkpoint Boundary
At each independently running node in the network, the ledger state is continuously split into two logical partitions based on the node's current block height $N$ and the dynamic Checkpoint boundary rule:

$$\text{Checkpoint} = N - 5$$

```
    [BOULDER ZONE - IMMUTABLE]      |     [FLEXIBLE ZONE - POW CONVERGENCE]
... --- 44 --- 45 ------------------|--- 46 --- 47 --- 48 --- 49 --- 50 (Tip N)
               ^                    |
        Checkpoint (N-5)            |
```

* **Flexible Zone ($\le 5$ blocks from the tip):** From block $N-4$ to the current tip $N$. This acts as a short-range buffer for natural network convergence. Any micro-forks resulting from physical network latency (a few seconds) between honest miners are resolved using traditional Proof-of-Work rules ($\times 1$). The chain with the highest accumulated cumulative difficulty ($U256$) wins. Empirical data shows natural forks only go 1 to 2 blocks deep.
* **Boulder Zone ($> 5$ blocks from the tip):** From block $N-5$ backward to the Genesis block. This region is frozen under normal operation. Here, VNT Consensus implements a dynamic energy arbiter filter called the **Invisible Hand** to regulate chain reorganization and defense.

#### B. Dynamic Fork Resolution and the $\times 10$ Energy Filter
When a node receives an alternative chain structure via GossipSub, it performs the **LCA (Lowest Common Ancestor)** algorithm to find the last common block hash between the local chain and the incoming chain.

If $\text{Fork\_Point} < \text{Checkpoint}$ (a deep reorg attempting to alter the Boulder Zone), the **Fragmented Energy Arbitration Process** is triggered. The node isolates and measures the accumulated difficulty since the block immediately following the common ancestor ($\text{LCA} + 1$) to the respective tip of each branch:

$$\text{Work}_{\text{New\_Fork}} \ge 10 \times \text{Work}_{\text{Current\_Branch}}$$

Where:
* $\text{Work}_{\text{Current\_Branch}}$: Cumulative difficulty ($U256$) from block $\text{LCA}+1$ to the current local tip $N$.
* $\text{Work}_{\text{New\_Fork}}$: Cumulative difficulty ($U256$) from block $\text{LCA}+1$ to the incoming branch tip $M$.

* **Scenario 1: Auto-Healing for Isolated Nodes:** If a VPS node is isolated due to local network failure, it might mine a few weak blocks locally. When connection is restored and the global main chain (mined by the entire world's hashrate) arrives, its energy exceeds the isolated node's local work by thousands of times, easily satisfying the $\times 10$ threshold. The system recognizes this as an honest but disconnected node, temporarily opens the firewall, wipes the local invalid blocks since $\text{LCA}+1$, and executes `forced_state_sync()` to merge the node into the main chain instantly.
* **Scenario 2: Thwarting Malicious Reorgs and Spam:** If a malicious peer transmits a weak attacker fork or spam chain, it cannot meet the 10x segment energy multiplier. The firewall remains locked, the data is discarded, a `GOSSIP_OLD_BLOCK_ATTEMPT` audit log is recorded, and the peer is banned.

#### C. Mathematical Proof: Invalidation of the 51% Attack
VNT Consensus mathematically guarantees that an attacker possessing 51% hashrate is powerless to silently mine a longer chain and rewrite history.

In traditional Nakamoto Consensus, the network views the chain statically, allowing a 51% attacker mining in secret to eventually reveal a longer chain and replace the public history. In VNT Consensus, the honest network is dynamic, continuously accumulating difficulty.

Suppose the public honest network runs at hashrate $H = 49\%$, and the attacker mines in secret with hashrate $X = 51\%$. The ratio of the attacker's accumulated energy to the honest network's energy over elapsed time $T$ is:

$$\frac{X}{H} = \frac{51\%}{49\%} \approx 1.04 \text{ times}$$

To break the Boulder Zone firewall, the attacker's hidden fork must accumulate at least 10 times the energy of the honest chain segment:

$$\text{Work}_{\text{Fork}} \ge 10 \times \text{Work}_{\text{Honest}}$$
$$51 \times T \ge 10 \times (49 \times T)$$
$$51 \times T \ge 490 \times T$$

This inequality has **no solution** for any $T > 0$. Because the honest network continues to actively build on the public chain, the energy ratio of the attacker's branch to the honest branch is bound at $\approx 1.04$ times. No matter how many hours or days the attacker mines, their hidden chain will be instantly rejected by the firewall because its energy cannot scale to 10 times that of the honest segment.

**Hardware Barrier:** To execute an instantaneous attack, an attacker must control at least $\ge 90.9\%$ of the global hashrate (adding 10 times the current network hashrate in hardware).

#### D. MAD Immutability and the Social Contract
Through these mathematical filters, YonaCode establishes a supreme security state: **MAD Immutability (Mutually Assured Destruction)**.

A deliberate attack is defined by its nature: mobilizing a massive amount of hidden hashrate to mine in secret to perform a double-spend. The network employs a dual-layered deterrence model to protect the ledger:

```
                  [ VNT CONSENSUS: DUAL-LAYER MAD DETERRENCE ]
                                       |
          +----------------------------+----------------------------+
          |                                                         |
[ LAYER 1: ECONOMIC DETERRENCE ]                         [ LAYER 2: SOCIAL CONTRACT ]
  (Automated by Protocol Code)                             (Executed by Human Intent)
   - Dynamic 10x energy filter.                             - Absolute refusal of reorg attacks.
   - Enforces >= 90.9% hashrate barrier.                    - Double-spend attempt = Hard Fork.
   - Bankrupts attacker economically.                       - Attacker chain value set to zero.
```

1. **Layer 1: Economic Deterrence (Protocol Level):** Enforced automatically by the protocol code (the Constitution). The dynamic 10x segment energy filter forces attackers to buy and run 10 times the hardware of the rest of the network without receiving public block rewards, ensuring economic bankruptcy before they can modify the ledger.
2. **Layer 2: Social Contract (Consensus Level):** In the extreme event of a massive, hostile double-spend attack, the community and developers will invoke social consensus to coordinate a Hard Fork to isolate and discard the attacker's chain. The network will reject the malicious branch and retain the original public chain. When the attacker's chain value is zero, their investment in electricity and hardware will be completely lost, ensuring 100% economic destruction for the attacker while keeping honest user assets fully preserved.

> **MAD IMMUTABILITY MANIFESTO:**
> We declare that we DO NOT ACCEPT any history reversal attacks, even if such an extreme hashrate violence scenario actually occurs in practice. To protect the absolute immutability of the system, if a fork attack deliberately performs double-spending, the community and developers will actively intervene through social consensus to implement a Hard Fork to isolate and reverse the attacker's chain.
> The entire network will flatly reject that malicious alternative chain, keeping the original public chain intact to protect the immutability of transactions and the assets of legitimate users.

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
- **Official Display Unit (Coin)**: `GO`
- **Official Base Unit (Cannot be divided further)**: `VNT` (Named after the initials of the architecture designer - Vo Nhat Thien).
- **Official Conversion Rate**: `1 GO = 100,000,000 VNT` ($10^8$ VNT)
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
