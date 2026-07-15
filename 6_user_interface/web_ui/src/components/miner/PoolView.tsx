import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Network, Zap, Users, Shield, Copy, Check, Terminal, Cpu, Play, Square, Info, Activity, Award, AlertTriangle } from 'lucide-react';
import api from '../../api';
import { useLanguage } from '../../LanguageContext';

interface PoolViewProps {
  status: any;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
}

interface WorkerData {
  address: string;
  shares: number;
  hashrate: number;
  active: boolean;
}

interface PoolStatusData {
  pool_address: string;
  pool_fee: number;
  share_diff_mult: number;
  active_workers: number;
  total_hashrate: number;
  solved_blocks: number;
  workers: WorkerData[];
}

const formatHashrate = (rate: number) => {
  if (rate >= 1e12) return { value: (rate / 1e12).toFixed(2), unit: 'TH/s' };
  if (rate >= 1e9) return { value: (rate / 1e9).toFixed(2), unit: 'GH/s' };
  if (rate >= 1e6) return { value: (rate / 1e6).toFixed(2), unit: 'MH/s' };
  if (rate >= 1e3) return { value: (rate / 1e3).toFixed(2), unit: 'KH/s' };
  return { value: rate.toLocaleString(), unit: 'H/s' };
};

const PoolView: React.FC<PoolViewProps> = ({ status, onNotify }) => {
  const { t, lang } = useLanguage();
  const vpsIp = "110.172.28.103";
  const [walletAddress, setWalletAddress] = useState(() => {
    return localStorage.getItem('vanguard_wallet_address') || '';
  });
  const [intensity, setIntensity] = useState(status?.cpu_intensity || 50);
  const [deviceMode, setDeviceMode] = useState<'cpu' | 'gpu'>(() => {
    return (localStorage.getItem('pool_mining_device') as 'cpu' | 'gpu') || 'cpu';
  });
  const [isMining, setIsMining] = useState(status?.node_mode === "full-mining");
  const [isPoolMiningActive, setIsPoolMiningActive] = useState(false);
  const [poolStatus, setPoolStatus] = useState<PoolStatusData | null>(null);
  const [copiedText, setCopiedText] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [hasInitialized, setHasInitialized] = useState(false);

  // Fetch pool status from RPC server (pointing to public VPS Pool)
  const fetchPoolStatus = async () => {
    try {
      const res = await fetch(`http://${vpsIp}:8080/api/v1/pool/status`);
      if (res.ok) {
        const data = await res.json();
        setPoolStatus(data);
      } else {
        // Fallback to local node if VPS fails
        const data = await api.getPoolStatus();
        setPoolStatus(data);
      }
    } catch (e) {
      // Fallback to local node if fetch fails
      try {
        const data = await api.getPoolStatus();
        setPoolStatus(data);
      } catch (err) {
        console.warn("Pool status fetch error:", err);
      }
    }
  };

  useEffect(() => {
    fetchPoolStatus();
    const interval = setInterval(() => {
      fetchPoolStatus();
    }, 3000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    setIsMining(status?.node_mode === "full-mining");
    if (status?.cpu_intensity) {
      setIntensity(status.cpu_intensity);
    }
    if (status?.is_pool_mining !== undefined) {
      setIsPoolMiningActive(status.is_pool_mining);
    }
    if (status?.pool_miner_device && (status.pool_miner_device === 'cpu' || status.pool_miner_device === 'gpu')) {
      if (!hasInitialized || status.is_pool_mining) {
        setDeviceMode(status.pool_miner_device as 'cpu' | 'gpu');
        localStorage.setItem('pool_mining_device', status.pool_miner_device);
        setHasInitialized(true);
      }
    }
  }, [status?.node_mode, status?.cpu_intensity, status?.is_pool_mining, status?.pool_miner_device, hasInitialized]);

  const isPoolMiningDisabled = true; // [V8.0] Cấu hình vô hiệu hóa pool mining
  
  if (isPoolMiningDisabled) {
    const disabledTitle = lang === 'vi' 
      ? 'TÍNH NĂNG KHAI THÁC BỂ ĐÀO ĐÃ BỊ VÔ HIỆU HÓA' 
      : 'POOL MINING HAS BEEN DISABLED';
    const disabledDesc = lang === 'vi'
      ? 'Tính năng khai thác qua Pool hiện đang bị vô hiệu hóa trên hệ thống và không khả dụng ở thời điểm hiện tại.'
      : 'Pool mining features are currently disabled on the system and not available at this moment.';

    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] vanguard-glass p-8 border border-white/10 rounded-2xl relative overflow-hidden shadow-2xl animate-in fade-in duration-500 w-full">
        <div className="absolute inset-0 bg-gradient-to-br from-accent-red/5 via-transparent to-transparent pointer-events-none" />
        <div className="w-16 h-16 rounded-full bg-accent-red/10 border border-accent-red/20 flex items-center justify-center text-accent-red mb-6 shadow-[0_0_30px_rgba(239,68,68,0.2)] animate-pulse">
          <AlertTriangle size={32} />
        </div>
        <h2 className="text-xl font-black text-white uppercase tracking-wider mb-2 text-center drop-shadow-[0_0_15px_rgba(255,255,255,0.1)]">
          {disabledTitle}
        </h2>
        <p className="text-sm text-white/60 text-center max-w-md leading-relaxed">
          {disabledDesc}
        </p>
      </div>
    );
  }

  const handleCopy = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedText(id);
    onNotify("Copied to clipboard!", "success");
    setTimeout(() => setCopiedText(null), 2000);
  };

  const handleIntensityChange = async (val: number) => {
    setIntensity(val);
    try {
      await api.setCpuIntensity(val);
      onNotify(`Updated CPU Intensity to ${val}%`, 'success');
    } catch (e) {
      onNotify(`❌ Error: ${(e as Error).message}`, 'error');
    }
  };

  const handleDeviceChange = async (device: 'cpu' | 'gpu') => {
    setDeviceMode(device);
    localStorage.setItem('pool_mining_device', device);
    try {
      await api.setMiningDevice(device);
      onNotify(`Switched target device to ${device.toUpperCase()}`, 'success');
    } catch (e) {
      onNotify(`❌ Error syncing device: ${(e as Error).message}`, 'error');
    }
  };

  const handleToggleMiner = async () => {

    if (!walletAddress || walletAddress.trim().length < 10) {
      onNotify("Please enter a valid wallet address first!", "error");
      return;
    }

    setIsLoading(true);
    try {
      localStorage.setItem('vanguard_wallet_address', walletAddress);
      
      const payload = {
        enabled: !isPoolMiningActive,
        device: deviceMode,
        pool_url: `${vpsIp}:8080`,
        address: walletAddress,
        threads: calculatedThreads
      };

      const res = await api.togglePoolMiner(payload);
      setIsPoolMiningActive(res.is_pool_mining);
      onNotify(res.is_pool_mining ? "Local CPU/GPU pool miner started successfully!" : "Stopped pool miner.", "success");
    } catch (e) {
      const errMsg = (e as Error).message;
      onNotify(`❌ Error: ${errMsg}`, 'error');
    } finally {
      setIsLoading(false);
    }
  };

  // Calculate dynamic threads based on intensity percentage (assuming 8 cores as standard)
  const calculatedThreads = Math.max(1, Math.round(8 * (intensity / 100)));

  const poolGpuCmd = `./yona_gpu_miner ${vpsIp} 8080 ${walletAddress || "YOUR_WALLET_ADDRESS"}`;
  const poolCpuCmd = `./genz_miner --url http://${vpsIp}:18080 --threads ${calculatedThreads} --address ${walletAddress || "YOUR_WALLET_ADDRESS"}`;
  const poolCliCmd = deviceMode === 'gpu'
    ? `./YonaCode pool-mine --device gpu --url ${vpsIp}:8080 ${walletAddress || "YOUR_WALLET_ADDRESS"}`
    : `./YonaCode pool-mine --device cpu --threads ${calculatedThreads} --url ${vpsIp}:8080 ${walletAddress || "YOUR_WALLET_ADDRESS"}`;

  const { value: poolHashrateVal, unit: poolHashrateUnit } = formatHashrate(poolStatus?.total_hashrate || 0);
  const myWorker = poolStatus?.workers?.find(w => w.address.toLowerCase() === walletAddress.toLowerCase());
  const localHash = myWorker ? myWorker.hashrate : (status?.hashrate || 0);
  const { value: localHashrateVal, unit: localHashrateUnit } = formatHashrate(localHash);
  const contributionPercent = poolStatus?.total_hashrate && poolStatus.total_hashrate > 0 
    ? ((localHash / poolStatus.total_hashrate) * 100).toFixed(2) 
    : '0.00';

  return (
    <div className="grid grid-cols-12 gap-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
      
      {/* CỘT TRÁI - THIẾT LẬP KẾT NỐI KHAI THÁC */}
      <div className="col-span-6 vanguard-glass p-6 border border-white/10 hover:border-accent-blue/30 transition-all duration-300 relative group flex flex-col justify-between min-h-[520px]">
        <div>
          <div className="flex items-center gap-3 mb-6">
            <div className="w-10 h-10 rounded-xl bg-accent-blue/10 flex items-center justify-center text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]">
              <Zap size={20} className={isMining ? 'animate-pulse' : ''} />
            </div>
            <div>
              <span className="text-[10px] font-black uppercase tracking-[0.2em] text-white/40">{t.pool_mining_action}</span>
              <h2 className="text-lg font-black text-white leading-none mt-1 uppercase italic tracking-tight">{t.pool_mining_title}</h2>
            </div>
          </div>

          <div className="vanguard-flex-v vanguard-gap-medium">
            {/* NHẬP ĐỊA CHỈ VÍ */}
            <div className="vanguard-flex-v vanguard-gap-small">
              <label className="text-[10px] uppercase font-bold tracking-widest text-text-secondary">
                {t.enter_wallet_pool}
              </label>
              <input
                type="text"
                value={walletAddress}
                onChange={(e) => setWalletAddress(e.target.value)}
                placeholder="0x..."
                className="w-full bg-black/60 border border-white/10 rounded-xl px-4 py-3 text-sm text-white font-mono focus:border-accent-blue/50 outline-none transition-all duration-300"
              />
            </div>

            {/* THIẾT BỊ KHAI THÁC */}
            <div className="vanguard-flex-v vanguard-gap-small">
              <label className="text-[10px] uppercase font-bold tracking-widest text-text-secondary">
                {t.select_device}
              </label>
              <div className="grid grid-cols-2 gap-2 bg-black/40 p-1 border border-white/5 rounded-xl">
                <button
                  onClick={() => handleDeviceChange('cpu')}
                  className={`py-2 text-[10px] font-black uppercase rounded-lg transition-all ${deviceMode === 'cpu' ? 'bg-accent-blue text-white shadow-[0_0_10px_rgba(0,136,255,0.3)]' : 'text-white/40 hover:text-white'}`}
                >
                  CPU
                </button>
                <button
                  onClick={() => handleDeviceChange('gpu')}
                  className={`py-2 text-[10px] font-black uppercase rounded-lg transition-all ${deviceMode === 'gpu' ? 'bg-accent-blue text-white shadow-[0_0_10px_rgba(0,136,255,0.3)]' : 'text-white/40 hover:text-white'}`}
                >
                  GPU
                </button>
              </div>
            </div>

            {/* CORE_STRESS_LVL SLIDER */}
            <div className="vanguard-stats-card p-5 bg-black/60 border border-white/[0.05] rounded-2xl relative overflow-hidden shadow-2xl transition-all duration-500">
              <div className="flex justify-between items-end mb-4">
                <span className="text-[9px] text-white/30 font-black tracking-[0.2em] uppercase">CORE_STRESS_LVL</span>
                <span className="text-4xl font-black text-accent-amber italic tracking-tighter drop-shadow-[0_0_20px_rgba(247,173,63,0.4)]">{intensity}%</span>
              </div>
              <div className="relative h-16 flex items-center px-3">
                <div className="absolute inset-x-0 h-2 bg-black/50 rounded-full border border-white/5 overflow-hidden">
                  <div className="absolute inset-0 bg-gradient-to-r from-accent-blue via-accent-amber to-accent-red" style={{ width: `${intensity}%` }} />
                </div>
                <input type="range" min="1" max="100" value={intensity} onChange={(e) => setIntensity(parseInt(e.target.value))} onMouseUp={(e) => handleIntensityChange(parseInt((e.target as HTMLInputElement).value))} className="absolute inset-0 w-full h-full opacity-0 cursor-pointer z-10" />
                <div className="absolute w-8 h-8 bg-white border-[6px] border-accent-amber rounded-full shadow-[0_0_30px_rgba(247,173,63,0.8)] pointer-events-none transition-all duration-300 transform -translate-x-1/2 flex items-center justify-center" style={{ left: `${intensity}%` }}>
                  <div className="w-1 h-3 bg-accent-amber/40 rounded-full" />
                </div>
              </div>
              <div className="flex items-center gap-3 mt-4 p-3 bg-accent-amber/5 rounded-xl border border-accent-amber/10 text-[10px] text-accent-amber/80 italic font-bold">
                <AlertTriangle size={14} />
                <span className="uppercase tracking-widest leading-relaxed">{t.thermal_warning}</span>
              </div>
            </div>

            {/* GIỚI THIỆU VPS BOOTSTRAP */}
            <div className="bg-white/[0.02] border border-white/5 p-4 rounded-xl flex gap-3 items-start">
              <Info size={16} className="text-accent-blue shrink-0 mt-0.5" />
              <div className="text-[11px] text-white/60 leading-relaxed">
                <span className="font-bold text-white uppercase tracking-wider block mb-1">{t.vps_bootstrap_title}</span>
                {t.vps_bootstrap_desc.split('{0}')[0]}<span className="text-accent-blue font-mono font-bold">{vpsIp}</span>{t.vps_bootstrap_desc.split('{0}')[1]}
              </div>
            </div>

            {/* TRẠNG THÁI KẾT NỐI BỂ ĐÀO CHI TIẾT */}
            {poolStatus ? (
              <div className="bg-accent-green/10 border border-accent-green/30 p-4 rounded-xl flex gap-3 items-center">
                <Shield size={16} className="text-accent-green shrink-0" />
                <div className="text-[11px] text-accent-green font-bold uppercase tracking-wider">
                  {t.pool_conn_success_msg ? t.pool_conn_success_msg.replace('{0}', vpsIp) : `🟢 Kết nối thành công tới bể đào VPS (${vpsIp}) | Hệ thống đã sẵn sàng khai thác!`}
                </div>
              </div>
            ) : (
              <div className="bg-accent-orange/10 border border-accent-orange/30 p-4 rounded-xl flex gap-3 items-center">
                <Info size={16} className="text-accent-orange shrink-0 animate-spin" />
                <div className="text-[11px] text-accent-orange font-bold uppercase tracking-wider">
                  {t.pool_conn_pending_msg ? t.pool_conn_pending_msg.replace('{0}', vpsIp) : `🟡 Đang kết nối tới bể đào VPS (${vpsIp}). Vui lòng chờ...`}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* NÚT BẮT ĐẦU */}
        <button
          onClick={handleToggleMiner}
          disabled={isLoading}
          className={`w-full py-4 rounded-xl font-black uppercase tracking-[0.2em] transition-all duration-300 flex items-center justify-center gap-3 ${
            isPoolMiningActive 
              ? 'bg-accent-red hover:bg-accent-red/80 text-white shadow-[0_0_20px_rgba(239,68,68,0.3)]' 
              : 'bg-accent-green hover:bg-accent-green/80 text-black shadow-[0_0_20px_rgba(16,185,129,0.3)]'
          }`}
        >
          {isPoolMiningActive ? <Square size={16} fill="white" /> : <Play size={16} fill="black" />}
          {isPoolMiningActive ? t.stop_pool_mining : t.start_pool_mining}
        </button>

        {/* CHÚ THÍCH CÁCH KẾT NỐI VÀ HÌNH THỨC HIỂN THỊ WORKERS */}
        <div className="mt-4 p-4 bg-white/[0.02] border border-white/5 rounded-xl text-[10px] text-white/50 leading-relaxed">
          <span className="font-bold text-white uppercase tracking-wider block mb-1">
            {t.pool_connect_guide_title || "💡 HƯỚNG DẪN KẾT NỐI BỂ ĐÀO:"}
          </span>
          {t.pool_connect_guide_desc ? t.pool_connect_guide_desc.replace('{0}', vpsIp) : `Nút START MINING ON POOL ở trên dùng để kích hoạt trình đào cục bộ của bạn kết nối và đào trực tiếp vào VPS Pool (${vpsIp}). Ngoài ra, bạn cũng có thể mở rộng quy mô đào bằng cách chạy các dòng lệnh CLI tương ứng ở cột bên phải trên các máy tính đào khác. Khi máy đào bắt đầu gửi shares hợp lệ lên bể, địa chỉ ví của bạn sẽ xuất hiện đầy đủ trong danh sách thợ đào bên dưới!`}
        </div>
      </div>

      {/* CỘT PHẢI - THÔNG SỐ VÀ TRÌNH ĐÀO NGOÀI */}
      <div className="col-span-6 vanguard-flex-v vanguard-gap-medium">
        
        {/* TELEMETRY BỂ ĐÀO */}
        <div className="vanguard-glass p-6 border border-white/10">
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <Network size={18} className="text-accent-blue" />
              <h3 className="text-xs font-black uppercase tracking-[0.15em] text-white/50">{t.pool_mining_title}</h3>
            </div>
            <div className="flex items-center gap-2 bg-black/60 border border-white/5 px-3 py-1 rounded-lg">
              <div className={`w-1.5 h-1.5 rounded-full ${poolStatus ? 'bg-accent-green animate-pulse' : 'bg-accent-orange animate-pulse'}`} />
              <span className="text-[9px] font-mono text-white/70">
                {poolStatus ? `${t.pool_connected || "Connected"}: ${vpsIp}` : (t.pool_connecting || "Connecting...")}
              </span>
            </div>
          </div>
          
          <div className="grid grid-cols-3 gap-4">
            <div className="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
              <span className="text-[9px] uppercase font-bold text-white/30 tracking-wider">{t.active_workers}</span>
              <div className="flex items-end gap-2 mt-2">
                <span className="text-xl font-black text-white">{poolStatus?.active_workers || 0}</span>
                <Users size={14} className="text-accent-blue mb-1" />
              </div>
            </div>
            
            <div className="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
              <span className="text-[9px] uppercase font-bold text-white/30 tracking-wider">{t.pool_hashrate}</span>
              <div className="flex items-end gap-1 mt-2">
                <span className="text-xl font-black text-white">{poolHashrateVal}</span>
                <span className="text-[9px] font-bold text-accent-blue mb-1">{poolHashrateUnit}</span>
              </div>
            </div>

            <div className="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
              <span className="text-[9px] uppercase font-bold text-white/30 tracking-wider">{t.solved_blocks}</span>
              <div className="flex items-end gap-2 mt-2">
                <span className="text-xl font-black text-accent-green">{poolStatus?.solved_blocks || 0}</span>
                <Award size={14} className="text-accent-green mb-1" />
              </div>
            </div>
          </div>

          {/* SO SÁNH TỐC ĐỘ BĂM CỦA TÔI VS BỂ ĐÀO */}
          <div className="mt-4 p-4 bg-gradient-to-r from-accent-blue/10 to-transparent border border-white/5 rounded-xl flex justify-between items-center">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-accent-blue/10 flex items-center justify-center text-accent-blue">
                <Cpu size={16} />
              </div>
              <div>
                <span className="text-[8px] font-black uppercase text-white/30 block tracking-widest">{t.my_local_hashrate}</span>
                <span className="text-sm font-black text-white">{localHashrateVal} <span className="text-[10px] text-accent-blue font-bold">{localHashrateUnit}</span></span>
              </div>
            </div>
            <div className="text-right">
              <span className="text-[8px] font-black uppercase text-white/30 block tracking-widest">{t.my_pool_share}</span>
              <span className="text-sm font-black text-accent-green">{contributionPercent}%</span>
            </div>
          </div>
        </div>

        {/* LỆNH LÀM VIỆC DÀNH CHO TOOL ĐÀO NGOÀI (CLI / GPU / CPU) */}
        <div className="vanguard-glass p-6 border border-white/10 flex-1 flex flex-col justify-between">
          <div>
            <div className="flex items-center gap-3 mb-4">
              <Terminal size={18} className="text-accent-blue" />
              <h3 className="text-xs font-black uppercase tracking-[0.15em] text-white/50">{t.cli_commands_title}</h3>
            </div>
            
            <div className="vanguard-flex-v vanguard-gap-small text-xs">
              
              {/* LỆNH NODE CLI */}
              <div className="vanguard-flex-v vanguard-gap-tiny bg-black/60 p-3 rounded-lg border border-white/5">
                <div className="flex justify-between items-center mb-1">
                  <span className="text-[9px] font-black uppercase tracking-wider text-accent-blue">
                    {t.node_cli_cmd_label || "NODE CLI COMMAND (YONASYSTEM)"}
                  </span>
                  <button 
                    onClick={() => handleCopy(poolCliCmd, 'cli')}
                    className="p-1 text-white/40 hover:text-white transition-colors"
                  >
                    {copiedText === 'cli' ? <Check size={12} className="text-accent-green" /> : <Copy size={12} />}
                  </button>
                </div>
                <pre className="font-mono text-[10px] text-white/80 overflow-x-auto whitespace-pre-wrap select-all">
                  {poolCliCmd}
                </pre>
              </div>

              {/* LỆNH GPU */}
              <div className="vanguard-flex-v vanguard-gap-tiny bg-black/60 p-3 rounded-lg border border-white/5">
                <div className="flex justify-between items-center mb-1">
                  <span className="text-[9px] font-black uppercase tracking-wider text-accent-blue">
                    {t.gpu_miner_cmd_label || "GPU MINER COMMAND (NVIDIA ONLY)"}
                  </span>
                  <button 
                    onClick={() => handleCopy(poolGpuCmd, 'gpu')}
                    className="p-1 text-white/40 hover:text-white transition-colors"
                  >
                    {copiedText === 'gpu' ? <Check size={12} className="text-accent-green" /> : <Copy size={12} />}
                  </button>
                </div>
                <pre className="font-mono text-[10px] text-white/80 overflow-x-auto whitespace-pre-wrap select-all">
                  {poolGpuCmd}
                </pre>
              </div>

              {/* LỆNH CPU */}
              <div className="vanguard-flex-v vanguard-gap-tiny bg-black/60 p-3 rounded-lg border border-white/5">
                <div className="flex justify-between items-center mb-1">
                  <span className="text-[9px] font-black uppercase tracking-wider text-accent-blue">
                    {t.cpu_miner_cmd_label || "CPU MINER COMMAND (RUST CORE)"}
                  </span>
                  <button 
                    onClick={() => handleCopy(poolCpuCmd, 'cpu')}
                    className="p-1 text-white/40 hover:text-white transition-colors"
                  >
                    {copiedText === 'cpu' ? <Check size={12} className="text-accent-green" /> : <Copy size={12} />}
                  </button>
                </div>
                <pre className="font-mono text-[10px] text-white/80 overflow-x-auto whitespace-pre-wrap select-all">
                  {poolCpuCmd}
                </pre>
              </div>

            </div>
          </div>

          <div className="text-[10px] text-white/30 flex items-center gap-2 mt-4">
            <Shield size={12} />
            {t.pool_fee_note}
          </div>
        </div>

      </div>

      {/* DANH SÁCH THỢ ĐÀO TRONG BỂ */}
      <div className="col-span-12 vanguard-glass p-6 border border-white/10">
        <div className="flex items-center gap-3 mb-4">
          <Activity size={18} className="text-accent-blue" />
          <h3 className="text-xs font-black uppercase tracking-[0.15em] text-white/50">{t.active_workers_list}</h3>
        </div>
        
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b border-white/10 text-white/40">
                <th className="py-3 font-black uppercase">{t.worker_address_col}</th>
                <th className="py-3 font-black uppercase text-right">{t.worker_shares_col}</th>
                <th className="py-3 font-black uppercase text-right">{t.worker_hashrate_col}</th>
                <th className="py-3 font-black uppercase text-right">{t.worker_status_col}</th>
              </tr>
            </thead>
            <tbody>
              {poolStatus?.workers && poolStatus.workers.length > 0 ? (
                poolStatus.workers.map((w, idx) => {
                  const { value: wHashVal, unit: wHashUnit } = formatHashrate(w.hashrate);
                  return (
                    <tr key={idx} className="border-b border-white/5 text-white/80 hover:bg-white/[0.01]">
                      <td className="py-3 font-mono">{w.address}</td>
                      <td className="py-3 text-right font-mono font-bold text-accent-blue">{w.shares.toFixed(2)}</td>
                      <td className="py-3 text-right font-mono">{wHashVal} {wHashUnit}</td>
                      <td className="py-3 text-right">
                        <span className={`px-2 py-0.5 rounded text-[9px] font-black uppercase ${w.active ? 'bg-accent-green/20 text-accent-green' : 'bg-white/10 text-white/40'}`}>
                          {w.active ? 'ACTIVE' : 'OFFLINE'}
                        </span>
                      </td>
                    </tr>
                  );
                })
              ) : (
                <tr>
                  <td colSpan={4} className="py-8 text-center text-white/30 uppercase tracking-widest text-[10px]">
                    {t.no_workers_connected}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

    </div>
  );
};

export default PoolView;
