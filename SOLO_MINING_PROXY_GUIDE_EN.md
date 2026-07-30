# ⛏️ SOLO MINING VIA SPECIALIZED NODE PROXY GUIDE (YONACODE)
> **High-Performance Solo Mining: Mine directly without synchronizing the ledger locally — The block finder claims 100% of the reward**

This guide describes the architecture, advantages, and configuration details for **Solo Mining via a Specialized Node Proxy** in YonaCode ($YGO). This tool allows miners to avoid heavy disk space usage and long ledger synchronization times while still mining blocks directly.

---

## 🚦 1. CORE DISTINCTION: SOLO PROXY VS MINING POOL

It is crucial to distinguish this setup from a traditional **Shared Mining Pool**:

| Feature | Solo Mining via Node Proxy (YonaCode) | Shared Mining Pool |
| :--- | :--- | :--- |
| **Reward Distribution** | 🏆 **No Shared Split**. The specific miner who finds the block receives **100% of the block reward** directly to their wallet. | ⚖️ Splits the reward proportionally among all connected miners based on submitted shares / hashpower. |
| **Service Fee** | 🆓 Typically 0% (depending on the VPS Node operator). | 💸 Charges a pool fee of 1% to 3% on all block rewards. |
| **Ledger Sync** | ❌ **Not Required**. The remote VPS Node maintains the entire 100% synced blockchain state. | ❌ No local node required. |
| **Ownership** | 🔒 Block Templates are constructed and signed with the miner's target wallet address from step one. | 🏢 Blocks are signed with the pool operator's wallet, then redistributed by the pool. |

---

## 📊 2. SYSTEM ARCHITECTURE DIAGRAM

```text
[ Miner A Machine (Wallet A) ] <── 1. Fetch Work for Wallet A ──┐
[ Miner B Machine (Wallet B) ] <── 1. Fetch Work for Wallet B ──┼──> [ REMOTE VPS SEED NODE (VPS A) ] ──> 3. Broadcast to P2P Net
[ Miner C Machine (Wallet C) ] <── 1. Fetch Work for Wallet C ──┘     (100% Synced Blockchain Node)       (Finder wallet gets 100%)
                                       (Communicated via RPC/Stratum)
```

### Hashing Workflow Steps:
1. **Fetch Work:** 
   Multiple miners connect to the specialized VPS Node (VPS A). Each miner declares their own payout wallet address. The VPS Node generates customized **Block Templates** tailored specifically for each address.
2. **Mining (Hashing Loop):** 
   Miner machines execute high-speed hash calculations on CPU/GPU.
3. **Submit Work:** 
   As soon as a miner finds a solution matching the target network difficulty, they submit the block headers containing the valid nonce back to the VPS Node.
4. **Validation & Broadcast:** 
   The VPS Node validates the block's mathematical proof, signs it, and broadcasts it to the main P2P network. **The block finder's wallet address is credited with 100% of the block reward.**

---

## 🛠️ 3. MINER CONFIGURATION INSTRUCTIONS

To connect your miner to a specialized VPS Node (e.g., VPS IP: `110.172.28.103`), execute the following command:

### Launch Command (Windows / Linux):
```bash
./genz_miner --node 110.172.28.103:8080 --wallet <YOUR_WALLET_ADDRESS> --device gpu
```

### Parameter Reference:
* `--node 110.172.28.103:8080`: Specifies the remote RPC address of the specialized VPS Node.
* `--wallet`: Your YonaCode address. **Double check this address; whoever finds the block has the reward sent directly to this address.**
* `--device gpu`: Enables NVIDIA CUDA mining to optimize hashing speeds.

---

## 🖥️ 4. VPS NODE OPERATOR CONFIGURATION

To run a specialized VPS Node supporting community solo mining without local ledger synchronization:

1. **Start the Node with Wallet Server APIs enabled:**
   ```bash
   ./YonaCode node start --wallet-server --port 8080 --p2p-port 9000 --db-path ./data
   ```
2. **Open Firewall Ports:**
   Ensure ports `8080` (RPC) and `9000` (P2P) accept incoming connections.
3. **Nginx Reverse Proxy (Optional):**
   Set up Nginx to act as a reverse proxy for the RPC port to add a security boundary against DDoS attacks.
