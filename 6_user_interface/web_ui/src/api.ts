/**
 * @file api.ts
 * @brief Giao diện kết nối REST & SSE - PHIÊN BẢN V2 (FINALITY-UI)
 * @tính_năng:
 *   - SSE Stream với newly_finalized_txids phát hiện Finality thời gian thực
 *   - API chi tiết giao dịch getTxDetail(txid)
 *   - getBalance trả về coin_id + nonce cho hiển thị Coin ID
 *   - calculateFee tính phí chống spam lũy tiến
 */

export interface StaticPeer {
  id: string;
  address: string;
  priority: number;
  name: string;
}

export interface MinerStats {
  address: string;
  blocks_mined: number;
  percentage: number;
  hashrate_est: number;
}

export interface NodeStatus {
  highest_height: number;
  finalized: number;
  network: string;
  consensus: string;
  sync: { 
    current: number; 
    target: number; 
    state: string;
    snapshot_chunks_loaded?: number;
    snapshot_chunks_total?: number;
    executing?: boolean;
    downloading?: number;
  };
  peers: { count: number };
  bandwidth: { sent: number; recv: number };
  pending_tx_count: number;
  hashrate: number;
  network_hashrate: number; // Tốc độ băm toàn mạng
  network_hashrate_history: number[]; // Lịch sử tốc độ băm toàn mạng của 20 khối gần nhất
  hashrate_history: number[];
  is_mining: boolean;
  node_mode: string;
  cpu_intensity: number;    // [MINER V2] Hiển thị cường độ CPU
  mining_device?: string;   // [MINER GPU/CPU] cpu, gpu, hybrid
  difficulty: string | number;       // [PHỤ LỤC D] Độ khó hiện tại (Chuỗi BigInt hoặc số)
  avg_block_time: number;   // [PHỤ LỤC D] Thời gian khối trung bình (60s)
  block_reward: number;
  grace_period_remaining?: number;
  mining_warning?: string; // [VANGUARD-SECURITY] Cảnh báo thợ đào chưa đăng nhập
  top_miners: MinerStats[];
}

export interface MinerStatus {
  hashrate: number;
  is_mining: boolean;
  miner_address: string;
  grace_period_remaining?: number;
}

export interface BlockHeader {
  height: number;
  hash: string;
  timestamp: number;
  tx_count: number;
  state_root: string;
  confirmations: number;
}

export interface RestoreParams {
    mnemonic: string;
    name: string;
    password: string;
    passphrase?: string;
    index?: number;
}

export interface WalletDiscovery {
    address: string;
    index: number;
    balance: number;
}

export interface Transaction {
  id: string;
  sender: string;
  receiver: string;
  amount: number;
  fee: number;
  timestamp: number;
  status: string;
  // [V39-AUTHORITATIVE] status_code là nguồn sự thật duy nhất (Single Source of Truth) cho trạng thái giao dịch
  // 0 = Đang chờ (Mempool), 1 = Thành công (Đã vào khối), 2+ = Lỗi/Bị từ chối
  status_code?: number;
  error_message?: string;
  confirmations: number;
  height: number;
  nonce: number;
  direction?: string; // [PHỤ LỤC D] IN/OUT cho address history
  is_self?: boolean; // Self-send transaction
  post_balance?: number; // [V37] Số dư sau khi thực hiện (VNT)
  prev_balance?: number; // [V37.1] Số dư TRƯỚC khi thực hiện (VNT)
}

// [PHỤ LỤC D] Chi tiết Block đầy đủ
export interface BlockDetail {
  height: number;
  hash: string;
  parent_hash: string;
  timestamp: number;
  nonce: number;
  difficulty: number;
  state_root: string;
  tx_root: string;
  miner: string;
  zk_proof_status: string;
  tx_count: number;
  transactions: Transaction[];
}

// [PHỤ LỤC D] Kết quả tìm kiếm
export interface SearchResult {
  type: 'block' | 'tx' | 'address';
  height?: number;
  txid?: string;
  address?: string;
}

// [PHỤ LỤC D] Lịch sử địa chỉ
export interface AddressHistoryResponse {
  address: string;
  tx_count: number;
  history: Transaction[];
}

// [V2] Thông tin số dư mở rộng với Coin ID
export interface BalanceInfo {
  address: string;
  balance: number;   // Tổng số dư
  spendable: number; // Số dư đã sẵn sàng (Maturity > 6)
  pending: number;   // Số dư đang trong hàng chờ
  coin_id: string;
  coin_index: number;
  nonce: number;
}

// [V2] Callback SSE mở rộng với sự kiện Finality
export interface SSECallbacks {
  onStatus: (status: NodeStatus) => void;
  onFinality?: (txIds: string[]) => void;
}

const api = {
  // Lấy trạng thái Node qua REST API
  async getStatus(): Promise<NodeStatus> {
    try {
      const response = await fetch('/api/v1/status');
      const data = await response.json();
      return {
        highest_height: data.highest_height,
        finalized: data.finalized,
        network: data.network, 
        consensus: data.consensus,
        sync: data.sync ? {
          current: data.sync.current,
          target: data.sync.target,
          state: (data.sync.state || '').toUpperCase(),
          snapshot_chunks_loaded: data.sync.snapshot_chunks_loaded,
          snapshot_chunks_total: data.sync.snapshot_chunks_total,
          executing: data.sync.executing,
          downloading: data.sync.downloading
        } : { current: 0, target: 0, state: 'STALLED' },
        peers: data.peers,
        bandwidth: data.bandwidth,
        pending_tx_count: data.pending_tx_count || 0,
        hashrate: data.hashrate || 0,
        network_hashrate: data.network_hashrate || 0,
        network_hashrate_history: data.network_hashrate_history || [],
        hashrate_history: data.hashrate_history || [],
        is_mining: data.is_mining || false,
        node_mode: data.node_mode || 'verify-only',
        difficulty: data.difficulty || 0,
        avg_block_time: data.avg_block_time || 60,
        cpu_intensity: data.cpu_intensity || 50,
        block_reward: data.block_reward || 0.05,
        grace_period_remaining: data.grace_period_remaining || 0,
        mining_warning: data.mining_warning || "",
        top_miners: data.top_miners || [],
      };
    } catch (e) {
      console.error("REST Status Error:", e);
      throw e;
    }
  },

  // [V2.1] WATCH STATUS STREAM (SSE REAL-TIME) — Loại bỏ Hardcoded Network/Consensus
  watchStatus(
    callback: (status: NodeStatus) => void,
    onFinality?: (txIds: string[]) => void
  ): () => void {
    let eventSource: EventSource | null = null;
    let retryTimeout: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      eventSource = new EventSource('/api/v1/network/watch-status');

      eventSource.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);

          // 1. Phát sóng trạng thái mạng (Dữ liệu thực từ SSE)
          callback({
            highest_height: data.current_height,
            finalized: data.finalized_height,
            network: data.version || "YonaCode", 
            consensus: "No-Day-Sky",
            sync: {
              current: data.current_height,
              target: data.target_height || data.current_height,
              state: (data.sync_state ? data.sync_state : (data.current_height < (data.target_height || 0) ? "SYNCING" : "STREAMING")).toUpperCase(),
              snapshot_chunks_loaded: data.snapshot_chunks_loaded,
              snapshot_chunks_total: data.snapshot_chunks_total,
              executing: data.sync_executing,
              downloading: data.sync_downloading,
            },
            peers: { count: data.peer_count || 0 },
            bandwidth: data.bandwidth || { sent: 0, recv: 0 },
            pending_tx_count: data.pending_tx_count || 0,
            hashrate: data.hashrate || 0,
            network_hashrate: data.network_hashrate || 0,
            network_hashrate_history: data.network_hashrate_history || [],
            hashrate_history: data.hashrate_history || [],
            is_mining: data.is_mining || false,
            node_mode: data.node_mode || 'verify-only',
            cpu_intensity: data.cpu_intensity || 50,
            difficulty: data.difficulty || 0,
            avg_block_time: data.avg_block_time || 60,
            block_reward: data.block_reward || 0.05,
            grace_period_remaining: data.grace_period_remaining || 0,
            mining_warning: data.mining_warning || "",
            top_miners: data.top_miners || [],
          });

          // 2. [V2] Phát sóng sự kiện Finality mới
          if (onFinality && data.newly_finalized_txids && data.newly_finalized_txids.length > 0) {
            onFinality(data.newly_finalized_txids);
          }
        } catch (e) {
          console.error("SSE Data Parse Error:", e);
        }
      };

      eventSource.onerror = () => {
        console.warn("SSE Connection Lost, retrying in 3s...");
        eventSource?.close();
        if (retryTimeout) clearTimeout(retryTimeout);
        retryTimeout = setTimeout(connect, 3000);
      };
    };

    connect();

    return () => {
      if (eventSource) eventSource.close();
      if (retryTimeout) clearTimeout(retryTimeout);
      console.log("[SSE] 🔌 Luồng WatchStatus V2 đã được giải phóng.");
    };
  },

  async getMinerStatus(): Promise<MinerStatus> {
    const res = await fetch('/api/v1/miner/status');
    const data = await res.json();
    return {
      hashrate: data.hashrate,
      is_mining: data.is_mining,
      miner_address: data.miner_address,
      grace_period_remaining: data.grace_period_remaining || 0
    };
  },

  async toggleMiner(): Promise<void> {
    const res = await fetch('/api/v1/miner/toggle', { method: 'POST' });
    if (!res.ok) {
      if (res.status === 403) {
        const errorData = await res.json().catch(() => ({}));
        throw new Error(errorData.error_code || 'SYNC_REQUIRED');
      }
      throw new Error(`Không thể kết nối đến Node: HTTP ${res.status}`);
    }
  },

  // [V2.0] Đồng bộ ví đang đăng nhập → ví đào trên backend
  // Tại sao: Đảm bảo phần thưởng đào luôn đi vào ví mà người dùng đang sử dụng
  async setMinerAddress(address: string, pin: string): Promise<{ status: string; address: string }> {
    const res = await fetch('/api/v1/miner/set-address', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address, pin })
    });
    if (!res.ok) {
      const errData = await res.json();
      throw new Error(errData.message || 'Không thể cập nhật ví đào');
    }
    return res.json();
  },

  // [V1.1.8] Chuyển đổi chế độ Node
  async setNodeMode(mode: 'verify-only' | 'full-mining'): Promise<{status: string, mode: string}> {
    const res = await fetch(`/api/v1/node/mode`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode })
    });
    if (!res.ok) {
      if (res.status === 403) {
        throw new Error('SYNC_REQUIRED');
      }
      throw new Error(`Command failed: ${res.statusText}`);
    }
    return res.json();
  },

  // [MINER V2] Điều chỉnh cường độ đào CPU
  async setCpuIntensity(intensity: number): Promise<{status: string, cpu_intensity: number}> {
    const res = await fetch(`/api/v1/node/cpu`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ intensity })
    });
    if (!res.ok) {
      throw new Error(`Failed to set CPU intensity: ${res.statusText}`);
    }
    return res.json();
  },

  // [MINER GPU/CPU Selector]
  async getMiningDevice(): Promise<{status: string, mining_device: string}> {
    const res = await fetch(`/api/v1/node/mining-device`);
    if (!res.ok) {
      throw new Error(`Failed to fetch mining device: ${res.statusText}`);
    }
    return res.json();
  },

  async setMiningDevice(device: string): Promise<{status: string, mining_device: string}> {
    const res = await fetch(`/api/v1/node/mining-device`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device })
    });
    if (!res.ok) {
      throw new Error(`Failed to set mining device: ${res.statusText}`);
    }
    return res.json();
  },

  async getRecentBlocks(): Promise<BlockHeader[]> {
    const s = await this.getStatus();
    const res = await fetch('/api/v1/recent/blocks');
    const data = await res.json();
    return data.map((b: any) => ({
      height: b.height,
      hash: b.hash,
      timestamp: b.timestamp,
      tx_count: b.tx_count,
      state_root: b.state_root,
      confirmations: Math.max(0, s.highest_height - b.height + 1)
    }));
  },

  // [V2] Nâng cấp: nhận dữ liệu thực từ Backend Tx Tracker
  async getRecentTxs(): Promise<Transaction[]> {
    const res = await fetch('/api/v1/recent/txs');
    const data = await res.json();
    return (data || []).map((tx: any) => ({
      id: tx.id || '',
      sender: tx.sender || '',
      receiver: tx.receiver || '',
      amount: tx.amount || 0,
      fee: tx.fee || 0,
      timestamp: tx.timestamp || 0,
      height: tx.height || 0,
      nonce: tx.nonce || 0,
      confirmations: tx.confirmations || 0,
      status: tx.status || 'PENDING',
      status_code: tx.status_code ?? 0,
      error_message: tx.error_message || '',
      direction: tx.direction || 'IN',
      is_self: !!tx.is_self,
      post_balance: tx.post_balance || 0,
      prev_balance: tx.prev_balance || 0,
    }));
  },

  // [V2 NEW] Chi tiết giao dịch theo TxID
  async getTxDetail(txid: string): Promise<Transaction | null> {
    try {
      const res = await fetch(`/api/v1/tx/${txid}`);
      if (!res.ok) return null;
      const tx = await res.json();
      return {
        id: tx.id || '',
        sender: tx.sender || '',
        receiver: tx.receiver || '',
        amount: tx.amount || 0,
        fee: tx.fee || 0,
        timestamp: tx.timestamp || 0,
        height: tx.height || 0,
        nonce: tx.nonce || 0,
        confirmations: tx.confirmations || 0,
        status: tx.status || 'PENDING',
        status_code: tx.status_code ?? 0,
        error_message: tx.error_message || '',
        direction: tx.direction || 'IN',
        is_self: !!tx.is_self,
        post_balance: tx.post_balance || 0,
        prev_balance: tx.prev_balance || 0,
      };
    } catch {
      return null;
    }
  },

  async sendTransaction(sender: string, receiver: string, amount: number, password: string = "", baseFee: number = 250): Promise<string> {
    const res = await fetch('/api/v1/send_tx', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sender, receiver, amount, password, base_fee: baseFee })
    });
    const data = await res.json();
    if (data.status !== "Success") {
      let errMsg = data.message || "Lỗi giao dịch từ Node";
      if (res.status === 401) errMsg = "[AUTH_REQUIRED] " + errMsg;
      throw new Error(errMsg);
    }
    return data.txid || "PENDING";
  },

  async getWallets(): Promise<any[]> {
    const res = await fetch('/api/v1/wallet/list');
    const data = await res.json();
    return data || [];
  },

  async createWallet(name: string = "default", password: string = "", passphrase: string = ""): Promise<{ mnemonic: string; address: string }> {
    const res = await fetch('/api/v1/wallet/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, password, passphrase })
    });
    if (!res.ok) {
      let errText = await res.text();
      if (errText.includes('<html') || errText.includes('<!DOCTYPE')) {
        errText = `Lỗi mạng hoặc Node ngoại tuyến (HTTP ${res.status})`;
      }
      throw new Error(errText);
    }
    return await res.json();
  },

  async restoreWallet(params: { mnemonic: string, name?: string, password?: string, passphrase?: string, index?: number }): Promise<{ address: string }> {
    const res = await fetch('/api/v1/wallet/restore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params)
    });
    if (!res.ok) {
      let errText = await res.text();
      if (errText.includes('<html') || errText.includes('<!DOCTYPE')) {
        errText = `Lỗi mạng hoặc Node ngoại tuyến (HTTP ${res.status})`;
      }
      throw new Error(errText);
    }
    return await res.json();
  },

  async deleteWallet(address: string): Promise<{ status: string; message: string }> {
    const res = await fetch('/api/v1/wallet/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address })
    });
    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || "Không thể xóa ví");
    }
    return await res.json();
  },

  // [V2] Nâng cấp: Trả về BalanceInfo đầy đủ với Coin ID
  async getBalance(address: string): Promise<number> {
    try {
      const res = await fetch(`/api/v1/balance/${address}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      console.log(`[API] 💰 Số dư nhận được cho ${address}:`, data.balances?.btc_z);
      return data.balances?.btc_z || 0;
    } catch (e) {
      console.error("[API] ❌ Lỗi lấy số dư:", e);
      return 0;
    }
  },

  // [V2 NEW] Lấy thông tin chi tiết số dư bao gồm Coin ID + Nonce
  async getBalanceInfo(address: string): Promise<BalanceInfo> {
    try {
      const res = await fetch(`/api/v1/balance/${address}`);
      const data = await res.json();
      return {
        address: data.address,
        balance: data.balances.btc_z || 0,
        spendable: data.balances.spendable || 0,
        pending: data.balances.pending || 0,
        coin_id: data.coin_id || 'BTC_Z',
        coin_index: data.coin_index || 0,
        nonce: data.nonce || 0,
      };
    } catch {
      return { address, balance: 0, spendable: 0, pending: 0, coin_id: 'BTC_Z', coin_index: 0, nonce: 0 };
    }
  },

  async getSupply(): Promise<number> {
    const res = await fetch('/api/v1/supply');
    const data = await res.json();
    return data.total_supply;
  },

  // [V3 NEW] Lấy chế độ Node hiện tại
  async getNodeMode(): Promise<{ mode: string; is_mining: boolean; description: string }> {
    const res = await fetch('/api/v1/node/mode');
    return await res.json();
  },

  // [PHỤ LỤC D] Chi tiết block theo chiều cao
  async getBlockDetail(height: number): Promise<BlockDetail | null> {
    try {
      const res = await fetch(`/api/v1/block/${height}`);
      if (!res.ok) return null;
      const data = await res.json();
      return {
        height: data.height,
        hash: data.hash || '',
        parent_hash: data.parent_hash || '',
        timestamp: data.timestamp || 0,
        nonce: data.nonce || 0,
        difficulty: data.difficulty || 0,
        state_root: data.state_root || '',
        tx_root: data.tx_root || '',
        miner: data.miner || '',
        zk_proof_status: data.zk_proof_status || 'Chưa có ZK-Proof',
        tx_count: data.tx_count || 0,
        transactions: (data.transactions || []).map((tx: any) => ({
          id: tx.id || '',
          sender: tx.sender || '',
          receiver: tx.receiver || '',
          amount: tx.amount || 0,
          fee: tx.fee || 0,
          timestamp: 0,
          height: height,
          nonce: tx.nonce || 0,
          confirmations: tx.confirmations || 0,
          status: tx.status || 'PENDING',
          error_message: tx.error_message || '',
        })),
      };
    } catch {
      return null;
    }
  },

  // [PHỤ LỤC D] Lịch sử giao dịch theo địa chỉ
  // direction: 'in' | 'out' | '' (all)
  // search: mã giao dịch để tìm kiếm
  async getAddressHistory(address: string, direction: string = '', search: string = ''): Promise<AddressHistoryResponse> {
    try {
      let url = `/api/v1/address/${address}/history`;
      const params = new URLSearchParams();
      if (direction) params.set('direction', direction);
      if (search) params.set('search', search);
      const qs = params.toString();
      if (qs) url += '?' + qs;

      const res = await fetch(url);
      const data = await res.json();
      const history = (data.history || []).map((tx: any) => ({
        id: tx.id || '',
        sender: tx.sender || '',
        receiver: tx.receiver || '',
        amount: tx.amount || 0,
        fee: tx.fee || 0,
        timestamp: tx.timestamp || 0,
        height: tx.height || 0,
        nonce: tx.nonce || 0,
        confirmations: tx.confirmations || 0,
        status: tx.status || 'PENDING',
        status_code: tx.status_code ?? 0,
        error_message: tx.error_message || '',
        direction: tx.direction || 'IN',
        is_self: !!tx.is_self,
        post_balance: tx.post_balance || 0,
        prev_balance: tx.prev_balance || 0,
      }));
      // Sắp xếp theo timestamp giảm dần (mới nhất lên đầu)
      history.sort((a: Transaction, b: Transaction) => b.timestamp - a.timestamp);
      return {
        address: data.address || address,
        tx_count: data.tx_count || 0,
        history,
      };
    } catch {
      return { address, tx_count: 0, history: [] };
    }
  },

  // [PHỤ LỤC D] Tìm kiếm toàn cầu
  async searchQuery(query: string): Promise<SearchResult | null> {
    try {
      const res = await fetch(`/api/v1/search/${encodeURIComponent(query)}`);
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  },

  async previewWallet(mnemonic: string, passphrase: string): Promise<{ address: string; valid: boolean }> {
    const res = await fetch('/api/v1/wallet/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mnemonic, passphrase })
    });
    if (!res.ok) {
      return { address: '', valid: false };
    }
    return await res.json();
  },

  async calculateFee(amount: number, baseFee: number = 0): Promise<{ fee: number; recommended: number }> {
    const res = await fetch(`/api/v1/fees/calculate?amount=${amount}&base_fee=${baseFee}`);
    return await res.json();
  },

  // [PURGE] Xóa sạch dữ liệu Node (Hard Reset)
  async purgeData(code: string): Promise<{ success: boolean; message: string }> {
    const res = await fetch('/api/v1/node/purge', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code })
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: 'Không thể xóa dữ liệu' }));
      throw new Error(err.message || 'Lỗi không xác định');
    }
    return res.json();
  },

  // [SOCIAL-CONSENSUS] Bàn tay vô hình: Xóa N khối gần nhất + Gỡ ban toàn mạng
  // Dùng khi mạng lưới bị chia cắt và Node bị kẹt ở nhánh sai
  async emergencyReset(blocksToRemove: number, code: string, targetHeight?: number, targetHash?: string): Promise<{
    success: boolean;
    message: string;
    previous_height?: number;
    new_height?: number;
  }> {
    const res = await fetch('/api/v1/node/emergency-reset', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ 
        blocks_to_remove: blocksToRemove, 
        target_height: targetHeight || 0,
        target_hash: targetHash || '',
        code 
      })
    });
    const data = await res.json();
    if (!res.ok || !data.success) {
      throw new Error(data.message || 'Lệnh Đảo Ngược Thực Tại thất bại');
    }
    return data;
  },

  // Quản lý Node Tĩnh (Static Peers) & Chế độ Cách ly
  async getStaticPeers(): Promise<{ success: boolean; static_peers: StaticPeer[]; isolation_mode: number }> {
    const res = await fetch('/api/v1/network/static-peers');
    if (!res.ok) {
      throw new Error(`Failed to fetch static peers: HTTP ${res.status}`);
    }
    return res.json();
  },

  async updateStaticPeers(staticPeers: StaticPeer[]): Promise<{ success: boolean; message: string }> {
    const res = await fetch('/api/v1/network/static-peers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ static_peers: staticPeers })
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Failed to update static peers' }));
      throw new Error(err.error || 'Failed to update static peers');
    }
    return res.json();
  },

  async setIsolationMode(isolationMode: number): Promise<{ success: boolean; isolation_mode: number }> {
    const res = await fetch('/api/v1/network/isolation-mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ isolation_mode: isolationMode })
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Failed to set isolation mode' }));
      throw new Error(err.error || 'Failed to set isolation mode');
    }
    return res.json();
  },

  // [EBP NEW] Gửi Lô Giao Dịch Tuần Tự Cho Sàn
  async sendBatchTx(params: {
    sender: string;
    seq_num: number;
    password?: string;
    transactions?: {
      receiver: string;
      amount: string;
      base_fee: number;
      nonce?: number;
    }[];
    signed_txs?: string[];
  }): Promise<{
    status: string;
    sequence: number;
    tx_count: number;
    tx_hashes: string[];
    audit_logs: string[];
    duration_ms: number;
  }> {
    const res = await fetch('/api/v1/send_batch_tx', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params)
    });
    const data = await res.json();
    if (data.status !== "Success") {
      throw new Error(data.message || "Giao dịch lô bị từ chối từ Node");
    }
    return data;
  },

  // [SHUTDOWN-V1.0] Gửi yêu cầu tắt Node tới backend
  async shutdownNode(): Promise<{ success: boolean; message: string }> {
    const res = await fetch('/api/v1/node/shutdown', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' }
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: 'Không thể gửi yêu cầu tắt Node' }));
      throw new Error(err.message || 'Lỗi không xác định');
    }
    return res.json();
  }
};

export default api;
