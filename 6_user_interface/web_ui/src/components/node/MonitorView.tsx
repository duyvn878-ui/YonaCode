import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { 
  Shield, 
  Activity, 
  Globe, 
  Database, 
  Cpu, 
  Zap, 
  Share2, 
  BarChart3, 
  Terminal,
  Lock,
  Layers,
  AlertTriangle,
  RotateCcw,
  CheckCircle2
} from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import api, { type NodeStatus } from '../../api';

interface MonitorViewProps {
  status: NodeStatus | null;
}

const MonitorStatCard = ({ 
  icon: Icon, 
  label, 
  value, 
  subValue, 
  colorClass, 
  bgClass,
  trend
}: { 
  icon: any, 
  label: string, 
  value: string | number, 
  subValue?: string, 
  colorClass: string, 
  bgClass: string,
  trend?: string
}) => (
  <div className="vanguard-glass p-6 relative overflow-hidden group hover:border-white/20 transition-all duration-500">
    <div className={`absolute -right-8 -bottom-8 w-32 h-32 ${bgClass} opacity-5 rounded-full blur-3xl group-hover:opacity-10 transition-all duration-1000`} />
    
    <div className="flex flex-col gap-4 relative z-10">
      <div className="flex items-center justify-between">
        <div className={`w-10 h-10 rounded-xl flex items-center justify-center border ${bgClass}/20 bg-opacity-10 ${colorClass}`}>
          <Icon size={20} />
        </div>
        {trend && (
          <span className="text-[9px] font-black uppercase text-accent-green bg-accent-green/10 px-2 py-0.5 rounded-full tracking-widest">
            {trend}
          </span>
        )}
      </div>
      
      <div className="flex flex-col">
        <span className="text-[10px] font-black text-white/30 uppercase tracking-[0.3em] mb-1">{label}</span>
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-black italic tracking-tighter text-white tabular-nums">
            {value}
          </span>
          {subValue && (
            <span className={`text-[11px] font-bold uppercase tracking-widest ${colorClass}`}>
              {subValue}
            </span>
          )}
        </div>
      </div>
    </div>
  </div>
);

const MonitorView: React.FC<MonitorViewProps> = ({ status }) => {
  const { t } = useLanguage();
  const [showPurgeModal, setShowPurgeModal] = useState(false);
  const [purgeCode, setPurgeCode] = useState('');
  const [isPurging, setIsPurging] = useState(false);

  // [SOCIAL-CONSENSUS] Bàn tay vô hình - State
  const [resetMode, setResetMode] = useState<'blocks' | 'height' | 'hash'>('blocks');
  const [blocksToRemove, setBlocksToRemove] = useState('');
  const [targetHeightVal, setTargetHeightVal] = useState('');
  const [targetHashVal, setTargetHashVal] = useState('');
  const [showResetModal, setShowResetModal] = useState(false);
  const [resetCode, setResetCode] = useState('');
  const [resetConfirmed, setResetConfirmed] = useState(false);
  const [isResetting, setIsResetting] = useState(false);
  const [resetResult, setResetResult] = useState<{ success: boolean; message: string } | null>(null);

  // States quản lý Node Tĩnh & Chế độ cách ly
  const [staticPeers, setStaticPeers] = useState<api.StaticPeer[]>([]);
  const [isolationMode, setIsolationMode] = useState<number>(1);
  const [newPeerName, setNewPeerName] = useState('');
  const [newPeerAddress, setNewPeerAddress] = useState('');
  const [newPeerPriority, setNewPeerPriority] = useState<number>(1);
  const [isSavingConfig, setIsSavingConfig] = useState(false);
  const [configMessage, setConfigMessage] = useState('');

  useEffect(() => {
    const fetchStaticPeers = async () => {
      try {
        const data = await api.getStaticPeers();
        if (data.success) {
          setStaticPeers(data.static_peers || []);
          setIsolationMode(data.isolation_mode || 1);
        }
      } catch (err) {
        console.error("Lỗi lấy thông tin static peers:", err);
      }
    };
    fetchStaticPeers();
  }, []);

  const extractPeerIdFromMultiaddr = (addr: string): string => {
    const parts = addr.split('/p2p/');
    if (parts.length > 1) {
      return parts[parts.length - 1].trim();
    }
    return "";
  };

  const handleAddStaticPeer = () => {
    if (!newPeerName || !newPeerAddress) {
      alert(t.lang === 'vi' ? 'Vui lòng nhập tên và địa chỉ node!' : 'Please enter node name and address!');
      return;
    }
    const pid = extractPeerIdFromMultiaddr(newPeerAddress);
    if (!pid) {
      alert(t.lang === 'vi' ? 'Địa chỉ node tĩnh phải chứa định dạng /p2p/PeerID' : 'Static peer address must contain /p2p/PeerID format');
      return;
    }
    const newPeer: api.StaticPeer = {
      id: pid,
      address: newPeerAddress,
      priority: newPeerPriority,
      name: newPeerName
    };
    setStaticPeers([...staticPeers, newPeer]);
    setNewPeerName('');
    setNewPeerAddress('');
    setNewPeerPriority(1);
  };

  const handleRemoveStaticPeer = (idx: number) => {
    const list = [...staticPeers];
    list.splice(idx, 1);
    setStaticPeers(list);
  };

  const handleSaveConfig = async () => {
    setIsSavingConfig(true);
    setConfigMessage("");
    try {
      await api.updateStaticPeers(staticPeers);
      await api.setIsolationMode(isolationMode);
      setConfigMessage(t.static_peers_update_success || "Đã lưu cấu hình thành công!");
      setTimeout(() => setConfigMessage(""), 4000);
    } catch (err: any) {
      setConfigMessage((t.lang === 'vi' ? "Lỗi lưu cấu hình: " : "Error saving config: ") + err.message);
    } finally {
      setIsSavingConfig(false);
    }
  };

  const handleEmergencyReset = async () => {
    if (!status) return;

    let numBlocks = 0;
    let targetH = 0;
    let targetHash = '';

    if (resetMode === 'blocks') {
      numBlocks = parseInt(blocksToRemove);
      if (!numBlocks || numBlocks <= 0) return;
    } else if (resetMode === 'height') {
      targetH = parseInt(targetHeightVal);
      if (!targetH || targetH <= 0 || targetH >= status.highest_height) {
        alert('Chiều cao đích không hợp lệ!');
        return;
      }
      numBlocks = status.highest_height - targetH;
    } else {
      targetHash = targetHashVal.trim();
      if (!targetHash || targetHash.length < 64) {
        alert('Mã băm Target Hash không hợp lệ!');
        return;
      }
    }

    if (resetCode !== '01900') {
      alert(t.purge_error_code ? t.purge_error_code.replace('{0}', '01900') : 'Mã xác nhận sai!');
      return;
    }
    if (!resetConfirmed) return;

    setIsResetting(true);
    setResetResult(null);
    try {
      const result = await api.emergencyReset(numBlocks, '01900', targetH, targetHash);
      setResetResult({ success: true, message: result.message });
      setShowResetModal(false);
      setResetCode('');
      setResetConfirmed(false);
      setBlocksToRemove('');
      setTargetHeightVal('');
      setTargetHashVal('');
    } catch (e: any) {
      setResetResult({ success: false, message: e.message });
    } finally {
      setIsResetting(false);
    }
  };

  const handlePurge = async () => {
    if (purgeCode !== '01900') {
      alert(t.purge_error_code ? t.purge_error_code.replace('{0}', '01900') : 'Mã xác nhận sai! Vui lòng nhập đúng 01900.');
      return;
    }

    setIsPurging(true);
    try {
      const response = await api.purgeData('01900');
      alert(t.purge_success ? t.purge_success : response.message);
      setShowPurgeModal(false);
      setTimeout(() => {
        window.location.reload();
      }, 2500);
    } catch (e: any) {
      alert(`${t.lang === 'vi' ? 'Lỗi xóa dữ liệu' : 'Purge error'}: ${e.message}`);
      setIsPurging(false);
    }
  };

  if (!status) return (
    <div className="flex-1 flex items-center justify-center vanguard-glass m-10">
      <div className="flex flex-col items-center gap-4 text-white/20">
        <Activity size={48} className="animate-pulse" />
        <span className="text-xs font-black uppercase tracking-[0.5em]">ĐANG TẢI DỮ LIỆU TỪ MA TRẬN...</span>
      </div>
    </div>
  );

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const syncPercent = status.sync.target > 0 ? (status.sync.current / status.sync.target) * 100 : 0;

  return (
    <div className="vanguard-flex-v vanguard-gap-medium h-full animate-in fade-in slide-in-from-bottom-4 duration-700">
      
      {/* 🛡️ TOP HEADER HUD */}
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-4">
          <div className="w-12 h-12 rounded-2xl bg-accent-blue/10 flex items-center justify-center text-accent-blue border border-accent-blue/20 shadow-[0_0_20px_rgba(0,136,255,0.2)]">
            <Shield size={24} />
          </div>
          <div className="flex flex-col">
            <h2 className="text-3xl font-black text-white italic tracking-tighter uppercase">{t.sidebar_monitor}</h2>
            <div className="flex items-center gap-2">
               <div className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse" />
               <span className="text-[9px] font-black text-accent-blue uppercase tracking-[0.4em]">GENZ_NODE_TELEMETRY_V2.0</span>
            </div>
          </div>
        </div>
        
        <div className="flex items-center gap-6">
           <div className="flex flex-col items-end">
              <span className="text-[10px] font-black text-white/30 uppercase tracking-widest">NETWORK_CONSENSUS</span>
              <span className="text-[14px] font-black text-accent-amber italic uppercase tracking-tighter">NO_DAY_SKY_READY</span>
           </div>
           <div className="w-[1px] h-10 bg-white/10" />
           <div className="flex flex-col items-end">
              <span className="text-[10px] font-black text-white/30 uppercase tracking-widest">PROTOCOL_VERSION</span>
              <span className="text-[14px] font-black text-white italic uppercase tracking-tighter">SCL_V1.1_BLAKE3</span>
           </div>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <MonitorStatCard 
          icon={Activity}
          label="ĐỘ KHÓ MẠNG LƯỚI"
          value={status.difficulty.toLocaleString()}
          subValue="DAA_LWMA"
          colorClass="text-accent-amber"
          bgClass="bg-accent-amber"
          trend="+2.4%"
        />
        <MonitorStatCard 
          icon={Layers}
          label="PHIÊN BẢN SỔ CÁI"
          value={`#${status.highest_height}`}
          subValue="CANONICAL"
          colorClass="text-accent-blue"
          bgClass="bg-accent-blue"
        />
        <MonitorStatCard 
          icon={Globe}
          label="HẠM ĐỘI ĐỒNG ĐẲNG"
          value={status.peers.count}
          subValue="NODES"
          colorClass="text-accent-green"
          bgClass="bg-accent-green"
          trend="STABLE"
        />
        <MonitorStatCard 
          icon={Zap}
          label="XUNG NHỊP KHỐI"
          value={status.avg_block_time}
          subValue="SECONDS"
          colorClass="text-purple-400"
          bgClass="bg-purple-500"
        />
      </div>

      <div className="grid grid-cols-12 gap-6 flex-1">
        
        {/* 🛰️ SYNC & LEDGER INTEGRITY PANEL */}
        <div className="col-span-8 vanguard-glass p-8 flex flex-col justify-between overflow-hidden relative">
           <div className="absolute top-0 right-0 w-64 h-64 bg-accent-blue/5 rounded-full blur-[100px] pointer-events-none" />
           
           <div className="flex items-center justify-between mb-10 relative z-10">
              <div className="flex items-center gap-4">
                 <div className="w-10 h-10 rounded-xl bg-accent-blue/10 flex items-center justify-center text-accent-blue border border-accent-blue/20">
                    <Database size={20} />
                 </div>
                 <div className="flex flex-col">
                    <span className="text-[14px] font-black uppercase tracking-[0.3em] text-white italic">MA TRẬN ĐỒNG BỘ</span>
                    <span className="text-[9px] text-accent-blue font-bold tracking-widest uppercase opacity-60">LEDGER_INTEGRITY_CHECK</span>
                 </div>
              </div>
              <div className="flex items-center gap-3">
                 <button 
                    onClick={() => setShowPurgeModal(true)}
                    className="px-4 py-2 bg-accent-red/10 border border-accent-red/20 rounded-full flex items-center gap-2 hover:bg-accent-red/20 hover:border-accent-red/30 transition-all text-[9px] font-black uppercase tracking-widest text-accent-red cursor-pointer"
                 >
                    <Database size={10} className="animate-spin" style={{ animationDuration: '3s' }} />
                    {t.purge_data_btn ? t.purge_data_btn.toUpperCase() : 'RE-SYNC'}
                 </button>
                 <div className="px-4 py-2 bg-black/40 rounded-full border border-white/5 flex items-center gap-3">
                    <span className="text-[10px] font-black text-white/50 uppercase tracking-widest italic">{status.sync.state}</span>
                    <div className={`w-2 h-2 rounded-full ${status.sync.state === 'SYNCED' || status.sync.state === 'STREAMING' ? 'bg-accent-green animate-pulse' : 'bg-accent-blue animate-ping'}`} />
                 </div>
              </div>
           </div>

           <div className="flex flex-col gap-8 flex-1 justify-center px-10 relative z-10">
              <div className="flex flex-col gap-4">
                 <div className="flex justify-between items-end mb-2">
                    <div className="flex flex-col">
                       <span className="text-[40px] font-black italic tracking-tighter text-white leading-none">
                          {syncPercent.toFixed(2)}%
                       </span>
                       <span className="text-[10px] font-black text-accent-blue uppercase tracking-[0.5em] mt-1">PERCENT_COMPLETE</span>
                    </div>
                    <div className="flex flex-col items-end">
                       <span className="text-[14px] font-black text-white italic">{status.sync.current} / {status.sync.target}</span>
                       <span className="text-[9px] font-bold text-white/30 uppercase tracking-widest">BLOCK_HEIGHT_PROGRESS</span>
                    </div>
                 </div>
                 <div className="h-4 w-full bg-white/5 rounded-full border border-white/5 overflow-hidden relative">
                    <motion.div 
                      initial={{ width: 0 }}
                      animate={{ width: `${syncPercent}%` }}
                      className="h-full bg-gradient-to-r from-accent-blue via-accent-blue/80 to-accent-blue/40 shadow-[0_0_20px_var(--accent-blue)]"
                    />
                    <div className="absolute inset-0 bg-[linear-gradient(90deg,transparent_0%,rgba(255,255,255,0.05)_50%,transparent_100%)] animate-[shimmer_2s_infinite]" />
                 </div>
              </div>

              <div className="grid grid-cols-3 gap-6">
                 <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl flex flex-col gap-1">
                    <span className="text-[9px] font-black text-white/20 uppercase tracking-widest">FINALIZED_HEIGHT</span>
                    <span className="text-[16px] font-black text-white italic tracking-tighter">#{status.finalized}</span>
                 </div>
                 <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl flex flex-col gap-1">
                    <span className="text-[9px] font-black text-white/20 uppercase tracking-widest">MEMPOOL_DEPTH</span>
                    <span className="text-[16px] font-black text-accent-amber italic tracking-tighter">{status.pending_tx_count} TXs</span>
                 </div>
                 <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl flex flex-col gap-1">
                    <span className="text-[9px] font-black text-white/20 uppercase tracking-widest">BLOCK_REWARD</span>
                    <span className="text-[16px] font-black text-accent-green italic tracking-tighter">{(status.block_reward || 0).toLocaleString(undefined, {minimumFractionDigits: 4, maximumFractionDigits: 8})} GO</span>
                 </div>
              </div>
           </div>

           <div className="mt-10 p-4 bg-accent-blue/5 border border-accent-blue/10 rounded-2xl flex items-center gap-4 relative z-10">
              <Lock size={18} className="text-accent-blue/60" />
              <div className="flex flex-col">
                 <span className="text-[9px] font-black text-accent-blue uppercase tracking-[0.3em]">STATE_ROOT_HASH</span>
                 <span className="text-[11px] font-mono font-bold text-white/60 break-all">
                    {status.highest_height > 0 ? "0x3fb8c82f9d...74a2e09" : "0000000000000000000000000000000000000000"}
                 </span>
              </div>
           </div>
        </div>

        {/* 📡 P2P TRAFFIC & RESOURCE PANEL */}
        <div className="col-span-4 vanguard-glass p-8 flex flex-col gap-8 relative overflow-hidden">
           <div className="absolute bottom-0 left-0 w-full h-1/2 bg-gradient-to-t from-accent-blue/5 to-transparent pointer-events-none" />
           
           <div className="flex items-center gap-4">
              <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center text-accent-green border border-accent-green/20">
                 <Share2 size={20} />
              </div>
              <div className="flex flex-col">
                 <span className="text-[14px] font-black uppercase tracking-[0.3em] text-white italic leading-none">BĂNG THÔNG P2P</span>
                 <span className="text-[8px] text-accent-green font-bold tracking-widest uppercase opacity-60">NETWORK_THROUGHPUT</span>
              </div>
           </div>

           <div className="flex flex-col gap-6">
              <div className="vanguard-stats-card p-5 bg-black/40 border border-white/5 rounded-2xl">
                 <div className="flex justify-between items-center mb-3">
                    <span className="text-[10px] font-black text-white/30 uppercase tracking-widest">SENT (ĐÃ GỬI)</span>
                    <BarChart3 size={14} className="text-accent-blue/40" />
                 </div>
                 <div className="flex items-baseline gap-2">
                    <span className="text-3xl font-black italic tracking-tighter text-white tabular-nums">
                       {formatBytes(status.bandwidth.sent)}
                    </span>
                    <span className="text-[10px] font-bold text-accent-blue uppercase animate-pulse">UP</span>
                 </div>
              </div>

              <div className="vanguard-stats-card p-5 bg-black/40 border border-white/5 rounded-2xl">
                 <div className="flex justify-between items-center mb-3">
                    <span className="text-[10px] font-black text-white/30 uppercase tracking-widest">RECV (ĐÃ NHẬN)</span>
                    <BarChart3 size={14} className="text-accent-green/40 rotate-180" />
                 </div>
                 <div className="flex items-baseline gap-2">
                    <span className="text-3xl font-black italic tracking-tighter text-white tabular-nums">
                       {formatBytes(status.bandwidth.recv)}
                    </span>
                    <span className="text-[10px] font-bold text-accent-green animate-pulse">DOWN</span>
                 </div>
              </div>

              <div className="p-6 bg-accent-blue/5 border border-accent-blue/20 rounded-2xl flex flex-col gap-4">
                 <div className="flex items-center gap-3">
                    <Cpu size={16} className="text-accent-blue" />
                    <span className="text-[10px] font-black text-white uppercase tracking-widest italic">HỆ THỐNG CỐT LÕI</span>
                 </div>
                 <div className="flex flex-col gap-2">
                    <div className="flex justify-between text-[11px] font-black italic">
                       <span className="text-white/40">CPU INTENSITY</span>
                       <span className="text-accent-blue">{status.cpu_intensity}%</span>
                    </div>
                    <div className="h-1 bg-white/5 rounded-full overflow-hidden">
                       <div className="h-full bg-accent-blue" style={{ width: `${status.cpu_intensity}%` }} />
                    </div>
                 </div>
              </div>
           </div>

           <div className="mt-auto flex items-center gap-3 p-4 bg-black/60 border border-white/5 rounded-xl font-mono text-[10px] text-white/30 italic">
              <Terminal size={14} />
              <span>LOG: SEEDER_DNS_RESOLVED_OK</span>
           </div>
        </div>
      </div>

      {/* ============================================================ */}
      {/* ⚓ STATIC PEERS & ISOLATION MODE PANEL */}
      {/* ============================================================ */}
      <div className="mt-8 relative">
        <div className="absolute -inset-1 bg-gradient-to-r from-accent-blue/10 via-accent-green/5 to-accent-blue/10 blur-xl rounded-3xl pointer-events-none" />
        <div className="relative vanguard-glass border border-white/10 rounded-3xl overflow-hidden p-6">
          <div className="flex items-center gap-4 mb-6 border-b border-white/5 pb-4">
            <div className="w-12 h-12 rounded-2xl bg-accent-blue/10 flex items-center justify-center text-accent-blue border border-accent-blue/20">
              <Globe size={24} />
            </div>
            <div className="flex flex-col">
              <h3 className="text-xl font-black text-white italic tracking-tight uppercase">
                {t.static_peers_panel_title}
              </h3>
              <span className="text-[9px] font-black text-white/30 uppercase tracking-[0.4em]">
                {t.static_peers_panel_subtitle}
              </span>
            </div>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-12 gap-8">
            {/* Cột trái: Isolation Mode */}
            <div className="lg:col-span-5 flex flex-col gap-4">
              <span className="text-[10px] font-black text-white/40 uppercase tracking-[0.3em]">
                {t.isolation_mode_label}
              </span>
              
              <div className="flex flex-col gap-3">
                {[
                  { mode: 1, label: t.isolation_mode_anchor, desc: t.isolation_mode_anchor_desc },
                  { mode: 2, label: t.isolation_mode_trust, desc: t.isolation_mode_trust_desc },
                  { mode: 3, label: t.isolation_mode_strict, desc: t.isolation_mode_strict_desc }
                ].map((item) => (
                  <label
                    key={item.mode}
                    onClick={() => setIsolationMode(item.mode)}
                    className={`p-4 rounded-2xl border transition-all cursor-pointer flex flex-col gap-1 relative ${
                      isolationMode === item.mode
                        ? 'bg-accent-blue/5 border-accent-blue/40 shadow-[0_0_15px_rgba(0,136,255,0.1)]'
                        : 'bg-black/20 border-white/5 hover:border-white/10'
                    }`}
                  >
                    <span className="text-xs font-black text-white flex items-center gap-2">
                      <input
                        type="radio"
                        name="isolation_mode"
                        checked={isolationMode === item.mode}
                        onChange={() => {}}
                        className="accent-accent-blue mr-1"
                      />
                      {item.label}
                    </span>
                    <span className="text-[10px] text-white/50 leading-relaxed pl-6">
                      {item.desc}
                    </span>
                  </label>
                ))}
              </div>
            </div>

            {/* Cột phải: Static Peers List & Add Form */}
            <div className="lg:col-span-7 flex flex-col gap-6">
              <div className="flex flex-col gap-4">
                <span className="text-[10px] font-black text-white/40 uppercase tracking-[0.3em]">
                  {t.static_peers_list_title}
                </span>

                <div className="max-h-[220px] overflow-y-auto pr-2 flex flex-col gap-2">
                  {staticPeers.length === 0 ? (
                    <div className="p-4 bg-white/[0.01] border border-dashed border-white/10 rounded-xl text-center text-xs text-white/30 italic">
                      {t.lang === 'vi' ? 'Chưa có Node tĩnh nào được thiết lập' : 'No static peers configured'}
                    </div>
                  ) : (
                    staticPeers
                      .sort((a, b) => a.priority - b.priority)
                      .map((peer, idx) => (
                        <div key={idx} className="p-3 bg-black/40 border border-white/5 rounded-xl flex items-center justify-between gap-4 hover:border-white/10 transition-all">
                          <div className="flex flex-col gap-0.5 min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="text-xs font-black text-white truncate">{peer.name}</span>
                              <span className="text-[9px] font-black text-accent-blue bg-accent-blue/10 px-2 py-0.5 rounded-full">
                                P{peer.priority}
                              </span>
                            </div>
                            <span className="text-[10px] font-mono text-white/40 truncate">{peer.address}</span>
                          </div>
                          <button
                            onClick={() => handleRemoveStaticPeer(idx)}
                            className="text-[9px] font-black text-accent-red hover:text-accent-red/80 uppercase tracking-widest cursor-pointer px-2 py-1 bg-accent-red/5 hover:bg-accent-red/10 rounded-lg border border-accent-red/10 transition-all"
                          >
                            {t.lang === 'vi' ? 'Xóa' : 'Delete'}
                          </button>
                        </div>
                      ))
                  )}
                </div>
              </div>

              {/* Form thêm mới */}
              <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl flex flex-col gap-3">
                <div className="grid grid-cols-1 sm:grid-cols-12 gap-3">
                  <div className="sm:col-span-4">
                    <input
                      type="text"
                      placeholder={t.static_peer_name}
                      value={newPeerName}
                      onChange={(e) => setNewPeerName(e.target.value)}
                      className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-xs font-bold text-white focus:border-accent-blue/40 outline-none placeholder:text-white/20"
                    />
                  </div>
                  <div className="sm:col-span-6">
                    <input
                      type="text"
                      placeholder={t.static_peer_address + " (e.g. /ip4/...)"}
                      value={newPeerAddress}
                      onChange={(e) => setNewPeerAddress(e.target.value)}
                      className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-xs font-mono text-white focus:border-accent-blue/40 outline-none placeholder:text-white/20"
                    />
                  </div>
                  <div className="sm:col-span-2">
                    <select
                      value={newPeerPriority}
                      onChange={(e) => setNewPeerPriority(parseInt(e.target.value))}
                      className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-xs font-bold text-white focus:border-accent-blue/40 outline-none cursor-pointer"
                    >
                      {[1, 2, 3, 4, 5].map(p => (
                        <option key={p} value={p} className="bg-bg-secondary text-white">P{p}</option>
                      ))}
                    </select>
                  </div>
                </div>
                
                <button
                  onClick={handleAddStaticPeer}
                  className="py-2.5 bg-white/5 hover:bg-white/10 border border-white/10 rounded-xl text-[9px] font-black uppercase tracking-widest text-white transition-all cursor-pointer"
                >
                  {t.static_peer_add_btn}
                </button>
              </div>
            </div>
          </div>

          {/* Action Footer */}
          <div className="mt-8 pt-4 border-t border-white/5 flex items-center justify-between gap-4">
            <span className="text-xs font-bold text-accent-green">
              {configMessage}
            </span>
            <button
              onClick={handleSaveConfig}
              disabled={isSavingConfig}
              className={`px-6 py-3 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all duration-300 flex items-center gap-2 cursor-pointer
                ${isSavingConfig 
                  ? 'bg-white/5 text-white/20 border border-white/5 cursor-not-allowed'
                  : 'bg-accent-blue text-white shadow-[0_0_15px_rgba(0,136,255,0.25)] hover:shadow-[0_0_25px_rgba(0,136,255,0.4)] border border-accent-blue/30 hover:scale-105'}`}
            >
              {isSavingConfig ? "..." : t.static_peer_save_btn}
            </button>
          </div>
        </div>
      </div>

      {/* ============================================================ */}
      {/* 👋 BÀN TAY VÔ HÌNH - DANGER ZONE (INVISIBLE HAND) */}
      {/* ============================================================ */}
      <div className="mt-8 relative">
        {/* Ambient glow */}
        <div className="absolute -inset-1 bg-gradient-to-r from-red-600/10 via-orange-500/5 to-red-600/10 blur-xl rounded-3xl pointer-events-none" />
        
        <div className="relative vanguard-glass border-2 border-accent-red/20 rounded-3xl overflow-hidden">
          {/* Header strip */}
          <div className="bg-gradient-to-r from-accent-red/10 via-accent-red/5 to-transparent p-6 border-b border-accent-red/10">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-4">
                <div className="w-12 h-12 rounded-2xl bg-accent-red/10 flex items-center justify-center text-accent-red border border-accent-red/20 shadow-[0_0_20px_rgba(255,59,48,0.15)]">
                  <AlertTriangle size={24} className="animate-pulse" />
                </div>
                <div className="flex flex-col">
                  <h3 className="text-xl font-black text-accent-red italic tracking-tight uppercase">
                    {t.danger_zone_title}
                  </h3>
                  <div className="flex items-center gap-2">
                    <div className="w-1.5 h-1.5 rounded-full bg-accent-red animate-pulse" />
                    <span className="text-[9px] font-black text-accent-red/60 uppercase tracking-[0.4em]">
                      {t.danger_zone_subtitle} — SOCIAL_CONSENSUS_OVERRIDE
                    </span>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-4">
                <div className="flex flex-col items-end">
                  <span className="text-[9px] font-black text-white/30 uppercase tracking-widest">{t.danger_current_height}</span>
                  <span className="text-sm font-black text-white italic">#{status.highest_height}</span>
                </div>
                <div className="w-[1px] h-8 bg-white/10" />
                <div className="flex flex-col items-end">
                  <span className="text-[9px] font-black text-white/30 uppercase tracking-widest">{t.danger_finalized}</span>
                  <span className="text-sm font-black text-accent-amber italic">#{status.finalized}</span>
                </div>
              </div>
            </div>
          </div>

          {/* Content area */}
          <div className="p-6">
            <p className="text-xs text-white/50 mb-6 leading-relaxed max-w-2xl">
              {t.danger_zone_desc}
            </p>

            {/* Chế độ lựa chọn */}
            <div className="flex gap-2 mb-4">
              <button
                onClick={() => setResetMode('blocks')}
                className={`px-4 py-1.5 rounded-lg text-[9px] font-black uppercase tracking-wider transition-all border cursor-pointer ${
                  resetMode === 'blocks'
                    ? 'bg-accent-red/10 border-accent-red/40 text-accent-red shadow-[0_0_15px_rgba(255,59,48,0.15)] font-bold'
                    : 'bg-white/5 border-white/10 text-white/40 hover:text-white/60'
                }`}
              >
                {t.lang === 'vi' ? 'Theo số lượng khối' : 'By block count'}
              </button>
              <button
                onClick={() => setResetMode('height')}
                className={`px-4 py-1.5 rounded-lg text-[9px] font-black uppercase tracking-wider transition-all border cursor-pointer ${
                  resetMode === 'height'
                    ? 'bg-accent-red/10 border-accent-red/40 text-accent-red shadow-[0_0_15px_rgba(255,59,48,0.15)] font-bold'
                    : 'bg-white/5 border-white/10 text-white/40 hover:text-white/60'
                }`}
              >
                {t.lang === 'vi' ? 'Theo chiều cao đích' : 'By target height'}
              </button>
              <button
                onClick={() => setResetMode('hash')}
                className={`px-4 py-1.5 rounded-lg text-[9px] font-black uppercase tracking-wider transition-all border cursor-pointer ${
                  resetMode === 'hash'
                    ? 'bg-accent-red/10 border-accent-red/40 text-accent-red shadow-[0_0_15px_rgba(255,59,48,0.15)] font-bold'
                    : 'bg-white/5 border-white/10 text-white/40 hover:text-white/60'
                }`}
              >
                {t.lang === 'vi' ? 'Theo mã băm' : 'By target hash'}
              </button>
            </div>

            <div className="flex items-end gap-4">
              <div className="flex-1 max-w-xs">
                <label className="text-[9px] font-black text-white/40 uppercase tracking-[0.3em] mb-2 block">
                  {resetMode === 'blocks'
                    ? (t.danger_blocks_input || 'SỐ KHỐI MUỐN XÓA')
                    : resetMode === 'height'
                    ? (t.lang === 'vi' ? 'CHIỀU CAO ĐÍCH QUAY VỀ' : 'TARGET HEIGHT')
                    : (t.lang === 'vi' ? 'MÃ BĂM NHÁNH ĐÚNG' : 'TARGET HASH')}
                </label>
                {resetMode === 'blocks' && (
                  <input
                    type="number"
                    min="1"
                    placeholder={t.danger_blocks_placeholder || 'Ví dụ: 5'}
                    value={blocksToRemove}
                    onChange={(e) => setBlocksToRemove(e.target.value)}
                    className="w-full bg-black/60 border border-accent-red/20 rounded-xl px-4 py-3 text-lg font-black text-white tracking-wider focus:border-accent-red/50 outline-none transition-all placeholder:text-white/15"
                  />
                )}
                {resetMode === 'height' && (
                  <input
                    type="number"
                    min="0"
                    max={status.highest_height - 1}
                    placeholder={`Ví dụ: ${status.highest_height - 5}`}
                    value={targetHeightVal}
                    onChange={(e) => setTargetHeightVal(e.target.value)}
                    className="w-full bg-black/60 border border-accent-red/20 rounded-xl px-4 py-3 text-lg font-black text-white tracking-wider focus:border-accent-red/50 outline-none transition-all placeholder:text-white/15"
                  />
                )}
                {resetMode === 'hash' && (
                  <input
                    type="text"
                    placeholder="Ví dụ: 0xabc123..."
                    value={targetHashVal}
                    onChange={(e) => setTargetHashVal(e.target.value)}
                    className="w-full bg-black/60 border border-accent-red/20 rounded-xl px-4 py-3 text-xs font-bold text-white tracking-wider focus:border-accent-red/50 outline-none transition-all placeholder:text-white/15"
                  />
                )}
              </div>

              <button
                onClick={() => {
                  if (resetMode === 'blocks') {
                    const n = parseInt(blocksToRemove);
                    if (!n || n <= 0) {
                      alert(t.danger_error_no_blocks || 'Vui lòng nhập số khối hợp lệ!');
                      return;
                    }
                  } else if (resetMode === 'height') {
                    const h = parseInt(targetHeightVal);
                    if (isNaN(h) || h <= 0 || h >= status.highest_height) {
                      alert(t.lang === 'vi' ? 'Vui lòng nhập chiều cao đích hợp lệ!' : 'Please enter a valid target height!');
                      return;
                    }
                  } else {
                    if (!targetHashVal || targetHashVal.trim().length < 64) {
                      alert(t.lang === 'vi' ? 'Vui lòng nhập mã băm Target Hash hợp lệ (64 kí tự hex)!' : 'Please enter a valid target hash (64 hex chars)!');
                      return;
                    }
                  }
                  setShowResetModal(true);
                  setResetCode('');
                  setResetConfirmed(false);
                  setResetResult(null);
                }}
                disabled={
                  resetMode === 'blocks'
                    ? (!blocksToRemove || parseInt(blocksToRemove) <= 0)
                    : resetMode === 'height'
                    ? (!targetHeightVal || isNaN(parseInt(targetHeightVal)) || parseInt(targetHeightVal) <= 0 || parseInt(targetHeightVal) >= status.highest_height)
                    : (!targetHashVal || targetHashVal.trim().length < 64)
                }
                className={`px-8 py-3 rounded-xl text-[11px] font-black uppercase tracking-widest transition-all duration-300 flex items-center gap-3 cursor-pointer
                  ${(resetMode === 'blocks' ? (!blocksToRemove || parseInt(blocksToRemove) <= 0) : resetMode === 'height' ? (!targetHeightVal || isNaN(parseInt(targetHeightVal)) || parseInt(targetHeightVal) <= 0 || parseInt(targetHeightVal) >= status.highest_height) : (!targetHashVal || targetHashVal.trim().length < 64))
                    ? 'bg-white/5 text-white/20 border border-white/5 cursor-not-allowed' 
                    : 'bg-gradient-to-r from-red-600 to-orange-600 text-white shadow-[0_0_25px_rgba(255,59,48,0.3)] hover:shadow-[0_0_40px_rgba(255,59,48,0.5)] hover:scale-105 border border-red-400/30'}`}
              >
                <RotateCcw size={16} />
                {t.danger_reset_btn}
              </button>
            </div>

            {/* Kết quả sau khi thực hiện */}
            <AnimatePresence>
              {resetResult && (
                <motion.div
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: 'auto', opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                  className={`mt-4 p-4 rounded-xl border flex items-center gap-3 overflow-hidden
                    ${resetResult.success 
                      ? 'bg-accent-green/10 border-accent-green/20 text-accent-green' 
                      : 'bg-accent-red/10 border-accent-red/20 text-accent-red'}`}
                >
                  {resetResult.success 
                    ? <CheckCircle2 size={18} /> 
                    : <AlertTriangle size={18} />}
                  <span className="text-xs font-bold">{resetResult.message}</span>
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        </div>
      </div>

      {/* MODAL XÁC NHẬN XÓA DATA (01900) */}
      {showPurgeModal && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-md p-4">
          <motion.div 
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            className="w-full max-w-md vanguard-glass p-6 flex flex-col gap-6 border border-accent-red/30 bg-black/95 rounded-2xl"
          >
            <div className="flex flex-col gap-2 items-center text-center">
              <div className="w-16 h-16 rounded-full bg-accent-red/10 flex items-center justify-center mb-2">
                <Database size={32} className="text-accent-red animate-pulse" />
              </div>
              <h3 className="text-xl font-black uppercase italic tracking-wider text-accent-red">
                {t.purge_modal_title}
              </h3>
              <p className="text-xs text-white/60 mt-2 leading-relaxed">
                {t.purge_modal_desc ? t.purge_modal_desc.replace('{0}', '01900') : 'Hành động này sẽ xóa toàn bộ dữ liệu. Vui lòng nhập mã xác nhận 01900.'}
              </p>
            </div>

            <div className="flex flex-col gap-4">
              <input 
                type="text" 
                placeholder={t.purge_input_placeholder ? t.purge_input_placeholder.replace('{0}', '01900') : 'Nhập mã (01900)'}
                value={purgeCode}
                onChange={(e) => setPurgeCode(e.target.value)}
                className="w-full bg-black/60 border border-white/10 rounded-lg px-4 py-3 text-center text-lg font-black tracking-[0.5em] text-accent-red focus:border-accent-red/50 outline-none"
              />
              
              <div className="grid grid-cols-2 gap-3 mt-4">
                <button 
                  onClick={() => { setShowPurgeModal(false); setPurgeCode(''); }}
                  className="py-3 bg-white/5 border border-white/10 rounded-lg text-[10px] font-black uppercase tracking-widest hover:bg-white/10 transition-all text-white cursor-pointer"
                >
                  {t.purge_cancel_btn}
                </button>
                <button 
                  onClick={handlePurge}
                  disabled={isPurging || purgeCode !== '01900'}
                  className={`py-3 rounded-lg text-[10px] font-black uppercase tracking-widest transition-all cursor-pointer 
                    ${isPurging || purgeCode !== '01900' ? 'bg-white/5 text-white/20 cursor-not-allowed' : 'bg-accent-red text-white hover:bg-accent-red/80 shadow-[0_0_15px_var(--accent-red)]'}`}
                >
                  {isPurging ? (t.lang === 'vi' ? 'ĐANG XÓA...' : 'PURGING...') : (t.purge_confirm_btn ? t.purge_confirm_btn.toUpperCase() : 'XÓA NGAY')}
                </button>
              </div>
            </div>
          </motion.div>
        </div>
      )}

      {/* MODAL XÁC NHẬN ĐẢO NGƯỢC THỰC TẠI (INVISIBLE HAND) */}
      <AnimatePresence>
        {showResetModal && (
          <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/85 backdrop-blur-xl p-4">
            <motion.div 
              initial={{ scale: 0.85, opacity: 0, y: 20 }}
              animate={{ scale: 1, opacity: 1, y: 0 }}
              exit={{ scale: 0.85, opacity: 0, y: 20 }}
              transition={{ type: 'spring', damping: 25, stiffness: 300 }}
              className="w-full max-w-lg vanguard-glass flex flex-col border-2 border-accent-red/30 bg-black/98 rounded-3xl overflow-hidden"
            >
              {/* Modal Header */}
              <div className="bg-gradient-to-r from-red-900/30 via-orange-900/20 to-red-900/30 p-6 border-b border-accent-red/20">
                <div className="flex items-center gap-4">
                  <div className="w-14 h-14 rounded-2xl bg-accent-red/15 flex items-center justify-center border border-accent-red/30">
                    <AlertTriangle size={28} className="text-accent-red animate-pulse" />
                  </div>
                  <div className="flex flex-col">
                    <h3 className="text-lg font-black uppercase italic tracking-wider text-accent-red">
                      {t.danger_confirm_title}
                    </h3>
                    <span className="text-[9px] font-bold text-white/30 uppercase tracking-widest">
                      IRREVERSIBLE_OPERATION
                    </span>
                  </div>
                </div>
              </div>

              {/* Modal Body */}
              <div className="p-6 flex flex-col gap-5">
                {/* Thông tin chi tiết */}
                <div className="p-4 bg-accent-red/5 border border-accent-red/15 rounded-xl">
                  <p className="text-xs text-white/70 leading-relaxed">
                    {resetMode === 'blocks'
                      ? (t.danger_confirm_desc || '')
                          .replace('{0}', blocksToRemove)
                          .replace('{1}', String(status.highest_height))
                          .replace('{2}', String(status.highest_height - parseInt(blocksToRemove || '0')))
                      : resetMode === 'height'
                      ? (t.lang === 'vi' 
                          ? `Hành động này sẽ xóa các khối để quay về chính xác khối #${targetHeightVal} (xóa ${status.highest_height - parseInt(targetHeightVal)} khối từ #${status.highest_height} về #${targetHeightVal}), gỡ ban toàn mạng và dọn mempool.`
                          : `This action will delete blocks to revert precisely to block #${targetHeightVal} (removing ${status.highest_height - parseInt(targetHeightVal)} blocks from #${status.highest_height} to #${targetHeightVal}), clear all network bans, and purge mempool.`)
                      : (t.lang === 'vi'
                          ? `Hành động này sẽ xóa các khối cục bộ từ chiều cao #${status.highest_height} ngược về Fork Point tương thích với mã băm ${targetHashVal}, sau đó tải lại chuỗi đúng từ Node tĩnh.`
                          : `This action will delete local blocks from #${status.highest_height} back to the Fork Point corresponding to hash ${targetHashVal}, and then download the correct chain from static nodes.`)}
                  </p>
                </div>

                {/* Hiển thị thị giác: Từ → Về */}
                <div className="flex items-center justify-center gap-4 py-3">
                  <div className="flex flex-col items-center">
                    <span className="text-[9px] font-black text-accent-red/60 uppercase tracking-widest mb-1">HIỆN TẠI</span>
                    <span className="text-2xl font-black text-white italic">#{status.highest_height}</span>
                  </div>
                  <div className="flex items-center gap-2 text-accent-red">
                    <div className="w-8 h-[2px] bg-accent-red/40" />
                    <RotateCcw size={16} className="animate-spin" style={{ animationDuration: '3s' }} />
                    <div className="w-8 h-[2px] bg-accent-red/40" />
                  </div>
                  <div className="flex flex-col items-center">
                    <span className="text-[9px] font-black text-accent-green/60 uppercase tracking-widest mb-1">MỤC TIÊU</span>
                    <span className="text-sm font-black text-accent-green italic max-w-[120px] truncate text-center">
                      {resetMode === 'blocks' 
                        ? `#${status.highest_height - parseInt(blocksToRemove || '0')}`
                        : resetMode === 'height' 
                        ? `#${targetHeightVal}`
                        : `${targetHashVal.slice(0, 12)}...`}
                    </span>
                  </div>
                </div>

                {/* Checkbox xác nhận */}
                <div className="flex items-start gap-3 p-3 bg-black/40 border border-white/5 rounded-xl hover:border-accent-red/20 transition-all">
                  <input
                    id="reset-confirm-checkbox"
                    type="checkbox"
                    checked={resetConfirmed}
                    onChange={(e) => setResetConfirmed(e.target.checked)}
                    className="mt-0.5 w-4 h-4 accent-red-500 cursor-pointer"
                  />
                  <label 
                    htmlFor="reset-confirm-checkbox"
                    className="text-[11px] text-white/60 font-bold leading-relaxed cursor-pointer select-none"
                  >
                    {t.danger_confirm_warning}
                  </label>
                </div>

                {/* Input mã xác nhận */}
                <div className="flex flex-col gap-2">
                  <label className="text-[9px] font-black text-white/40 uppercase tracking-[0.3em]">
                    {t.danger_code_label} (01900)
                  </label>
                  <input
                    type="text"
                    placeholder={t.purge_input_placeholder ? t.purge_input_placeholder.replace('{0}', '01900') : 'Nhập mã (01900)'}
                    value={resetCode}
                    onChange={(e) => setResetCode(e.target.value)}
                    className="w-full bg-black/60 border border-accent-red/20 rounded-xl px-4 py-3 text-center text-xl font-black tracking-[0.5em] text-accent-red focus:border-accent-red/50 outline-none transition-all placeholder:text-white/20 placeholder:tracking-normal placeholder:font-normal placeholder:text-sm"
                  />
                </div>

                {/* Hiển thị lỗi/thành công trực tiếp trong Modal */}
                {resetResult && !resetResult.success && (
                  <div className="p-3 bg-red-500/10 border border-red-500/30 rounded-xl flex items-start gap-3">
                    <AlertTriangle size={16} className="text-red-400 mt-0.5 flex-shrink-0" />
                    <p className="text-xs text-red-300 font-bold leading-relaxed">{resetResult.message}</p>
                  </div>
                )}

                {/* Action buttons */}
                <div className="grid grid-cols-2 gap-4 mt-2">
                  <button
                    onClick={() => {
                      setShowResetModal(false);
                      setResetCode('');
                      setResetConfirmed(false);
                      setResetResult(null);
                    }}
                    className="py-3.5 bg-white/5 border border-white/10 rounded-xl text-[10px] font-black uppercase tracking-widest hover:bg-white/10 transition-all text-white cursor-pointer"
                  >
                    {t.danger_cancel_btn}
                  </button>
                  <button
                    onClick={handleEmergencyReset}
                    disabled={isResetting || resetCode !== '01900' || !resetConfirmed}
                    className={`py-3.5 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all duration-300 flex items-center justify-center gap-2 cursor-pointer
                      ${isResetting || resetCode !== '01900' || !resetConfirmed
                        ? 'bg-white/5 text-white/20 border border-white/5 cursor-not-allowed'
                        : 'bg-gradient-to-r from-red-600 to-orange-600 text-white shadow-[0_0_20px_rgba(255,59,48,0.4)] hover:shadow-[0_0_35px_rgba(255,59,48,0.6)] border border-red-400/30'}`}
                  >
                    {isResetting ? (
                      <>
                        <RotateCcw size={14} className="animate-spin" />
                        {t.danger_processing}
                      </>
                    ) : (
                      <>
                        <AlertTriangle size={14} />
                        {t.danger_confirm_btn}
                      </>
                    )}
                  </button>
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default MonitorView;
