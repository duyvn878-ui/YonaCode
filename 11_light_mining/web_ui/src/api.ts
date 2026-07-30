import axios from 'axios';

export interface LogEntry {
  time: string;
  text: string;
  type: 'info' | 'success' | 'warn';
}

export interface MinedBlockEntry {
  height: number;
  reward: number;
  hash: string;
  timestamp: string;
  status: string;
}

export interface MinerEngineStatus {
  is_mining: boolean;
  is_connected: boolean;
  wallet: string;
  hashrate: number;
  network_hashrate: number;
  network_height: number;
  blocks_found: number;
  wallet_balance: number;
  go_earned: number;
  uptime: string;
  cpu_cores: number;
  cpu_threads?: number;
  cpu_intensity?: number;
  logs: LogEntry[];
  mined_blocks_history: MinedBlockEntry[];
}

export interface CreateWalletResponse {
  success: boolean;
  address: string;
  mnemonic: string;
  error?: string;
}

const api = {
  async getStatus(): Promise<MinerEngineStatus> {
    const res = await axios.get<MinerEngineStatus>('/api/status');
    return res.data;
  },

  async startMining(wallet: string): Promise<{ success: boolean; error?: string }> {
    const res = await axios.post<{ success: boolean; error?: string }>('/api/start', { wallet });
    return res.data;
  },

  async stopMining(): Promise<{ success: boolean }> {
    const res = await axios.post<{ success: boolean }>('/api/stop');
    return res.data;
  },

  async createWallet(): Promise<CreateWalletResponse> {
    const res = await axios.post<CreateWalletResponse>('/api/wallet/create');
    return res.data;
  },

  async setCPUConfig(threads: number, intensity: number): Promise<{ success: boolean }> {
    const res = await axios.post<{ success: boolean }>('/api/cpu/config', {
      cpu_threads: threads,
      cpu_intensity: intensity
    });
    return res.data;
  }
};

export default api;
