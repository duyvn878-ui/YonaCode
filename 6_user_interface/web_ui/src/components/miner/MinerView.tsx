import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Pickaxe, Zap, Activity, AlertTriangle, ShieldCheck, Cpu, TrendingUp, Globe } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import api from '../../api';
import type { NodeStatus, MinerStatus } from '../../api';

interface MinerViewProps {
  status: NodeStatus | null;
  minerStatus: MinerStatus | null;
  handleToggleMiner: () => Promise<void>;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
  isStopping?: boolean;
}

const formatHashrate = (rate: number) => {
  if (rate >= 1e12) return { value: (rate / 1e12).toFixed(2), unit: 'TH/s' };
  if (rate >= 1e9) return { value: (rate / 1e9).toFixed(2), unit: 'GH/s' };
  if (rate >= 1e6) return { value: (rate / 1e6).toFixed(2), unit: 'MH/s' };
  if (rate >= 1e3) return { value: (rate / 1e3).toFixed(2), unit: 'KH/s' };
  return { value: rate.toLocaleString(), unit: 'H/s' };
};

const MinerView: React.FC<MinerViewProps> = ({ status, minerStatus, handleToggleMiner, onNotify, isStopping = false }) => {
  const { t } = useLanguage();
  const [intensity, setIntensity] = useState(status?.cpu_intensity || 50);
  const [deviceMode, setDeviceMode] = useState<string>(() => {
    return localStorage.getItem('solo_mining_device') || status?.mining_device || 'cpu';
  });

  const [hasInitializedDevice, setHasInitializedDevice] = useState(false);

  useEffect(() => {
    if (status?.cpu_intensity) {
      setIntensity(status.cpu_intensity);
    }
  }, [status?.cpu_intensity]);

  useEffect(() => {
    if (status?.mining_device && !hasInitializedDevice) {
      const savedDevice = localStorage.getItem('solo_mining_device');
      if (savedDevice && (savedDevice === 'cpu' || savedDevice === 'gpu' || savedDevice === 'hybrid') && savedDevice !== status.mining_device) {
        setDeviceMode(savedDevice);
        api.setMiningDevice(savedDevice).catch(console.warn);
      } else {
        setDeviceMode(status.mining_device);
        localStorage.setItem('solo_mining_device', status.mining_device);
      }
      setHasInitializedDevice(true);
    }
  }, [status?.mining_device, hasInitializedDevice]);

  const isMining = status?.node_mode === "full-mining";
  const hashrate = status?.hashrate || minerStatus?.hashrate || 0;
  const { value: hashrateValue, unit: hashrateUnit } = formatHashrate(hashrate);
  const gracePeriodRemaining = Math.ceil(status?.grace_period_remaining || minerStatus?.grace_period_remaining || 0);

  const handleIntensityChange = async (val: number) => {
    setIntensity(val);
    try {
      await api.setCpuIntensity(val);
    } catch (e) {
      onNotify(`❌ Error: ${(e as Error).message}`, 'error');
    }
  };

  const handleDeviceChange = async (device: string) => {
    // Cập nhật giao diện lập tức
    setDeviceMode(device);
    localStorage.setItem('solo_mining_device', device);
    try {
      const res = await api.setMiningDevice(device);
      if (res.mining_device !== device) {
        setDeviceMode(res.mining_device);
        localStorage.setItem('solo_mining_device', res.mining_device);
      }
      onNotify(`⚡ Đã chuyển sang thiết bị khai thác: ${device.toUpperCase()}`, 'success');
    } catch (e) {
      // Khôi phục giá trị cũ nếu lỗi
      const saved = localStorage.getItem('solo_mining_device') || 'cpu';
      setDeviceMode(saved);
      onNotify(`❌ Lỗi: ${(e as Error).message}`, 'error');
    }
  };

  return (
    <div className="vanguard-flex-v vanguard-gap-medium h-full animate-in fade-in slide-in-from-bottom-4 duration-700">
      <div className="grid grid-cols-12 gap-6 h-[500px]">
        <div className="col-span-4 vanguard-glass p-6 border border-white/10 hover:border-accent-blue/30 transition-all duration-300 flex flex-col justify-between overflow-hidden relative group">
           <div className="absolute -right-8 -top-8 w-32 h-32 bg-accent-blue/5 rounded-full blur-3xl group-hover:bg-accent-blue/10 transition-all duration-1000" />
           
           <div>
              <div className="flex items-center gap-3 mb-4">
                 <div className="w-10 h-10 rounded-xl bg-accent-blue/10 flex items-center justify-center text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]">
                    <Zap size={20} className={isMining ? 'animate-pulse' : ''} />
                 </div>
                 <span className="text-[10px] font-black uppercase tracking-[0.2em] text-white/40">{t.miner_status}</span>
              </div>
              
              <div className="vanguard-stats-card p-5 bg-black/50 border border-white/5 rounded-2xl mb-4 relative overflow-hidden">
                  <div className="flex flex-col gap-3">
                     <span className="text-[9px] text-white/30 uppercase font-black tracking-[0.2em]">SYSTEM_MODE</span>
                     
                     <div className="flex flex-col gap-3">
                        <div className="flex items-center gap-3">
                           <span className={`text-xl font-black italic tracking-tighter whitespace-nowrap ${isMining ? 'text-accent-green' : 'text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]'}`}>
                              {isMining ? t.miner_mode_active : ((status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') ? "SYNCING_DATA" : t.verify_only)}
                           </span>
                           {(isMining || status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') && (
                            <div className="flex gap-1">
                               <div className={`w-1.5 h-4 ${isMining ? 'bg-accent-green' : 'bg-accent-blue'} rounded-full animate-bounce [animation-delay:-0.3s]`} />
                               <div className={`w-1.5 h-6 ${isMining ? 'bg-accent-green' : 'bg-accent-blue'} rounded-full animate-bounce [animation-delay:-0.1s]`} />
                               <div className={`w-1.5 h-4 ${isMining ? 'bg-accent-green' : 'bg-accent-blue'} rounded-full animate-bounce`} />
                            </div>
                          )}
                        </div>

                        {(status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') && (() => {
                          const isSnapshot = status.sync.state === 'BOOTSTRAPPING';
                          const isExecuting = status.sync.executing === true;
                          const hasDownloading = status.sync.downloading !== undefined && status.sync.downloading > 0;
                          const progress = isSnapshot && status.sync.snapshot_chunks_total && status.sync.snapshot_chunks_total > 0
                            ? (status.sync.snapshot_chunks_loaded || 0) / status.sync.snapshot_chunks_total * 100
                            : (isExecuting 
                                ? (status.sync.target > 0 ? (status.sync.current / status.sync.target) * 100 : 0) 
                                : (hasDownloading ? (status.sync.target > 0 ? (status.sync.downloading! / status.sync.target) * 100 : 0) : 0));
                          const isDownloadingPhase = !isSnapshot && status.sync.state === 'SYNCING' && !isExecuting;
                          
                          return (
                            <div className="vanguard-flex-v vanguard-gap-tiny w-full mt-2">
                               <div className="flex justify-between items-center text-[9px] font-black">
                                  <span className="text-accent-blue uppercase tracking-widest">
                                     {isSnapshot ? "Snapshot Progress" : (isDownloadingPhase ? "Phase 1: Downloading" : "Phase 2: Validating")}
                                  </span>
                                  <span className="text-white/60">
                                    {isDownloadingPhase && !hasDownloading ? "LOADING..." : `${progress.toFixed(2)}%`}
                                  </span>
                               </div>
                               <div className="h-1.5 w-full bg-white/5 rounded-full border border-white/5 overflow-hidden relative">
                                  <motion.div 
                                    initial={{ width: 0 }}
                                    animate={{ width: `${progress}%` }}
                                    transition={{ ease: "linear" }}
                                    className="h-full bg-accent-blue shadow-[0_0_10px_var(--accent-blue)]"
                                  />
                                  {isDownloadingPhase && (
                                    <motion.div 
                                      className="absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-white/10 to-transparent absolute"
                                      animate={{ x: ["-100%", "300%"] }}
                                      transition={{ repeat: Infinity, duration: 1.5, ease: "linear" }}
                                    />
                                  )}
                               </div>
                               <div className="flex justify-between items-center text-[8px] text-white/30 font-bold uppercase tracking-tighter">
                                  {isSnapshot && status.sync.snapshot_chunks_total && status.sync.snapshot_chunks_total > 0 ? (
                                    <>
                                      <span>CHUNK: {status.sync.snapshot_chunks_loaded || 0}</span>
                                      <span>TOTAL: {status.sync.snapshot_chunks_total}</span>
                                    </>
                                  ) : (
                                    isDownloadingPhase ? (
                                      hasDownloading ? (
                                        <>
                                          <span>DL: {status.sync.downloading}</span>
                                          <span>TG: {status.sync.target}</span>
                                        </>
                                      ) : (
                                        <span>Downloading block indexes from network...</span>
                                      )
                                    ) : (
                                      <>
                                        <span>HT: {status.sync.current}</span>
                                        <span>TG: {status.sync.target}</span>
                                      </>
                                    )
                                  )}
                               </div>
                            </div>
                          );
                        })()}

                        {!isMining && status?.sync.state !== 'SYNCING' && status?.sync.state !== 'BOOTSTRAPPING' && (
                           <p className="text-[9px] font-bold text-white/40 mt-2 leading-relaxed">
                              {t.no_mining} <span className="text-accent-blue/60">{t.sync_to_mine_hint}</span>
                           </p>
                        )}
                        
                        {(status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') && (
                           <div className="flex flex-col gap-2 mt-2 bg-accent-blue/5 border border-accent-blue/10 p-3 rounded-xl">
                              <p className="text-[9px] font-bold text-accent-blue/90 leading-relaxed animate-pulse flex items-center gap-1.5">
                                 ⚡ {t.sync_to_mine_hint}
                              </p>
                              <p className="text-[9px] font-semibold text-white/50 leading-relaxed">
                                 ℹ️ {t.sync_warning}
                              </p>
                              <p className="text-[9.5px] font-black text-red-400 border-t border-white/5 pt-2 leading-relaxed">
                                 {t.mining_sync_warning}
                              </p>
                           </div>
                         )}
                     </div>
                  </div>
              </div>
           </div>

           <button 
             onClick={(!isStopping && gracePeriodRemaining === 0 && status?.sync.state !== 'SYNCING' && status?.sync.state !== 'BOOTSTRAPPING') ? handleToggleMiner : undefined}
             className={`w-full py-4 px-6 rounded-xl font-black tracking-widest uppercase transition-all duration-300 flex items-center justify-center gap-3 relative overflow-hidden group
               ${(isStopping || gracePeriodRemaining > 0 || status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING')
                 ? 'bg-white/5 border border-white/10 cursor-not-allowed opacity-60 text-white/40' 
                 : (isMining 
                   ? 'bg-red-500/10 border border-red-500/30 hover:bg-red-500/20 text-red-500 shadow-[0_0_20px_rgba(239,68,68,0.1)] cursor-pointer' 
                   : 'bg-accent-green/10 border border-accent-green/30 hover:bg-accent-green/20 text-accent-green shadow-[0_0_20px_rgba(0,242,148,0.1)] cursor-pointer')}`}
           >
              <div className={`w-2 h-2 rounded-full animate-pulse ${isMining ? 'bg-red-500 shadow-[0_0_10px_#ef4444]' : 'bg-accent-green shadow-[0_0_10px_#00f294]'}`} />
              <span className="text-[12px] flex items-center gap-2">
                 {gracePeriodRemaining > 0 ? (
                   <>
                     <Activity size={14} className="animate-spin" />
                     <span>SCANNING NETWORK...</span>
                   </>
                 ) : (
                   (status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') ? (
                     <>
                       <Activity size={14} className="animate-spin" />
                       <span>{t.mining_blocked_sync?.toUpperCase() || "SYNCING..."}</span>
                     </>
                   ) : (
                     <>
                       <Pickaxe size={14} className={isStopping ? 'animate-spin' : ''} />
                       <span>{isStopping ? 'STOPPING...' : (isMining ? t.mining_stop_btn : t.mining_start_btn)}</span>
                     </>
                   )
                 )}
              </span>
           </button>

           <div className="w-full flex flex-col gap-2.5 p-4 bg-black/60 border border-white/[0.05] rounded-2xl relative z-10 mt-0">
              <span className="text-[9px] font-black uppercase text-accent-blue/50 tracking-[0.2em] mb-1">{t.telemetry_title}</span>
              <div className="flex items-center justify-between">
                 <div className="flex items-center gap-2">
                    <Globe size={14} className="text-accent-blue" />
                    <span className="text-[11px] font-bold text-white/70">{t.telemetry_peers}</span>
                 </div>
                 <div className="flex items-center gap-2">
                    <span className="text-[13px] font-black text-white">{status?.peers?.count || 1}</span>
                    <span className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse shadow-[0_0_5px_var(--accent-green)]" />
                 </div>
              </div>
              <div className="h-[1px] w-full bg-gradient-to-r from-transparent via-white/10 to-transparent" />
              <div className="flex items-center justify-between">
                 <div className="flex items-center gap-2">
                    <Activity size={14} className="text-accent-amber" />
                    <span className="text-[11px] font-bold text-white/70">{t.telemetry_diff}</span>
                 </div>
                 <div className="flex items-center gap-1 bg-accent-amber/10 border border-accent-amber/20 px-2 py-0.5 rounded-lg">
                    <span className="text-[11px] font-black text-accent-amber">
                      {(status?.difficulty && Number(status.difficulty) > 50) ? t.telemetry_high : (status?.difficulty ? t.telemetry_mid : t.telemetry_auto)}
                    </span>
                    <TrendingUp size={12} className="text-accent-amber ml-1" />
                 </div>
              </div>
           </div>
        </div>        <div className="col-span-5 vanguard-glass p-10 border border-white/10 flex flex-col items-center justify-center relative overflow-hidden group">
           <div className={`absolute inset-0 transition-opacity duration-1000 ${isMining ? 'opacity-20' : 'opacity-0'}`}>
              <div className="absolute inset-x-0 top-0 h-[1px] bg-gradient-to-r from-transparent via-accent-blue to-transparent animate-[shimmer_3s_infinite]" />
              <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,rgba(0,136,255,0.1),transparent_70%)]" />
           </div>
           <div className="relative z-10 flex flex-col items-center gap-4 w-full">
              <div className="flex items-center gap-3 p-3 bg-white/[0.03] border border-white/[0.05] rounded-2xl mb-4 backdrop-blur-md">
                 <Activity size={16} className="text-accent-blue animate-pulse" />
                 <span className="text-[9px] text-white/40 font-black tracking-[0.25em] uppercase">SYSTEM_THROUGHPUT_REALTIME</span>
              </div>
              <div className="relative mb-8 group/hash">
                 <div className="absolute -inset-10 bg-accent-blue/5 rounded-full blur-[80px] group-hover/hash:bg-accent-blue/15 transition-all duration-1000" />
                 <AnimatePresence mode='wait'>
                    <motion.div
                      key={hashrate}
                      initial={{ scale: 0.9, opacity: 0, filter: 'blur(20px)' }}
                      animate={{ scale: 1, opacity: 1, filter: 'blur(0px)' }}
                      className="relative z-10 flex flex-row items-baseline justify-center gap-3"
                    >
                       <span className="text-[100px] md:text-[110px] font-black italic text-white leading-none tracking-tighter drop-shadow-[0_0_50px_rgba(0,136,255,0.4)]">
                          {hashrateValue}
                       </span>
                       <span className="text-2xl md:text-3xl font-black italic text-accent-blue tracking-widest uppercase opacity-90">{hashrateUnit}</span>
                    </motion.div>
                 </AnimatePresence>
                 <div className="absolute -top-16 left-1/2 -translate-x-1/2 opacity-5 pointer-events-none">
                    <Globe size={300} className="text-white animate-[spin_20s_linear_infinite]" />
                 </div>
              </div>
              <div className="flex gap-16 mt-6 relative z-10">
                 <div className="flex flex-col items-center">
                    <span className="text-[10px] text-white/30 font-black tracking-[0.25em] mb-2 uppercase">BLOCK_REWARD</span>
                    <div className="flex items-baseline gap-1">
                       <span className="text-[20px] text-accent-amber font-black italic shadow-text">
                          {(status?.block_reward || 0).toLocaleString(undefined, {minimumFractionDigits: 4, maximumFractionDigits: 8})}
                       </span>
                       <span className="text-xs text-accent-amber font-bold uppercase">GO</span>
                    </div>
                 </div>
                 <div className="w-[1px] h-12 bg-white/10" />
                 <div className="flex flex-col items-center">
                    <span className="text-[10px] text-white/30 font-black tracking-[0.25em] mb-2 uppercase">NETWORK_DIFFICULTY</span>
                    <span className="text-[20px] text-accent-green font-black italic shadow-text">
                      {status?.difficulty ? Number(status.difficulty).toLocaleString() : "..."}
                    </span>
                 </div>
              </div>
           </div>
           <motion.div 
             animate={isMining ? { rotate: [0, -35, 0], y: [0, -10, 0] } : {}} 
             transition={{ repeat: Infinity, duration: 1, ease: "easeInOut" }}
             className={`absolute bottom-10 right-10 p-6 rounded-3xl border border-white/5 bg-black/40 backdrop-blur-xl transition-all duration-500 ${isMining ? 'text-accent-blue opacity-40 scale-125' : 'text-white/5 opacity-10'}`}
           >
              <Pickaxe size={80} strokeWidth={1} />
           </motion.div>
        </div>

        <div className="col-span-3 vanguard-glass p-6 flex flex-col justify-between border border-white/10 group relative overflow-hidden">
           <div>
              <div className="flex items-center gap-3 mb-4">
                 <div className="w-12 h-12 rounded-2xl bg-accent-amber/10 flex items-center justify-center text-accent-amber shadow-[0_0_25px_rgba(247,173,63,0.2)] border border-accent-amber/20">
                    <Cpu size={24} />
                 </div>
                 <div className="flex flex-col">
                    <span className="text-[11px] font-black uppercase tracking-[0.25em] text-white/40 leading-none">{t.miner_title}</span>
                    <span className="text-[8px] text-accent-amber font-bold tracking-widest opacity-60 mt-1">HARDWARE_OPTIMIZER_ACTIVE</span>
                 </div>
              </div>

              {/* BỘ CHỌN THIẾT BỊ KHAI THÁC */}
              <div className="mb-4 bg-black/40 border border-white/[0.05] p-4 rounded-2xl relative overflow-hidden">
                 <span className="text-[9px] text-white/30 font-black tracking-[0.2em] uppercase block mb-3">{t.mining_device}</span>
                 <div className="grid grid-cols-3 gap-2 bg-black/30 p-1 rounded-xl border border-white/5 mb-3">
                    {(['cpu', 'gpu', 'hybrid'] as const).map((dev) => (
                       <button
                          key={dev}
                          onClick={() => handleDeviceChange(dev)}
                          className={`py-2 px-1 text-[9px] font-black uppercase rounded-lg transition-all duration-300 ${
                             deviceMode === dev
                               ? 'bg-accent-blue text-white shadow-[0_0_15px_rgba(0,136,255,0.4)]'
                               : 'text-white/40 hover:text-white/80 hover:bg-white/5'
                          }`}
                       >
                          {dev === 'cpu' ? t.mining_device_cpu : dev === 'gpu' ? t.mining_device_gpu : t.mining_device_hybrid}
                       </button>
                    ))}
                 </div>
                 {(deviceMode === 'gpu' || deviceMode === 'hybrid') && (
                    <div className="text-[8px] text-accent-blue/80 font-bold uppercase tracking-wider leading-relaxed bg-accent-blue/5 border border-accent-blue/10 p-2 rounded-lg flex items-start gap-1">
                       <span>ℹ️</span>
                       <span>{t.gpu_nvidia_only}</span>
                    </div>
                 )}
              </div>

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

              {deviceMode === 'gpu' && (
                 <motion.div 
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mt-3 p-3 bg-accent-blue/10 border border-accent-blue/20 rounded-xl text-[10px] text-accent-blue font-bold flex items-center gap-2"
                 >
                    <Activity size={14} className="animate-pulse text-accent-blue" />
                    <span>{t.cpu_mining_disabled}</span>
                 </motion.div>
              )}
           </div>

           <div className="p-6 rounded-[24px] bg-gradient-to-br from-accent-blue/10 to-transparent border border-accent-blue/20 flex items-center justify-between shadow-lg">
              <div className="flex flex-col gap-1">
                 <span className="text-[9px] text-white/30 font-black uppercase tracking-[0.25em] leading-none">VANGUARD_CORE</span>
                 <span className="text-[12px] text-white font-black">SP1_V2_PROVER: ACTIVE</span>
              </div>
              <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center text-accent-green shadow-[0_0_20px_rgba(34,197,94,0.3)]">
                 <ShieldCheck size={24} strokeWidth={2.5} />
              </div>
           </div>
        </div>
      </div>

      <div className="grid grid-cols-12 gap-6 flex-1">
        <div className="col-span-8 vanguard-glass p-8 overflow-hidden flex flex-col border border-white/10 shadow-[inset_0_4px_50px_rgba(0,136,255,0.05)] transition-all duration-300">
           <div className="flex items-center justify-between mb-6 px-2">
              <div className="flex items-center gap-4">
                 <div className="w-2 h-6 bg-accent-blue rounded-full shadow-[0_0_15px_rgba(0,136,255,0.5)]" />
                 <div>
                    <span className="text-[11px] font-black uppercase tracking-[0.3em] text-white/40">TRANSMISSION_PROTOCOL</span>
                    <div className="h-[1px] w-full bg-gradient-to-r from-accent-blue/40 to-transparent mt-1" />
                 </div>
              </div>
              <div className="flex items-center gap-3 bg-black/40 px-4 py-2 rounded-full border border-white/5">
                 <span className="text-[10px] text-accent-blue font-mono font-black animate-pulse">LIVE_CONN</span>
                 <div className="w-1 h-1 rounded-full bg-accent-blue" />
                 <span className="text-[9px] text-white/40 font-mono">STREAM: SCL_SSE_8080</span>
              </div>
           </div>
           <div className="flex-1 bg-black/80 rounded-[32px] p-8 font-mono text-[12px] leading-relaxed overflow-y-auto space-y-4 custom-scrollbar border border-white/[0.05] shadow-inner">
              <div className="flex gap-6 items-start">
                 <span className="text-accent-blue font-black min-w-[90px] border-r border-white/10 pr-4">[CORE]</span>
                 <span className="text-white/50">{t.log_core_init}</span>
              </div>
              <div className="flex gap-6 items-start">
                 <span className="text-accent-amber font-black min-w-[90px] border-r border-white/10 pr-4">[DAA]</span>
                 <span className="text-white/50">{t.log_daa_aligned}</span>
              </div>
              {isMining && (
                <motion.div initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }} className="flex gap-6 items-start">
                   <span className="text-accent-green font-black min-w-[90px] border-r border-white/10 pr-4">[POW]</span>
                   <span className="text-accent-green/80 font-bold">
                      {deviceMode === 'gpu' ? t.log_pow_scanning_gpu : deviceMode === 'hybrid' ? t.log_pow_scanning_hybrid : t.log_pow_scanning_cpu}
                   </span>
                </motion.div>
              )}
              <div className="h-6" />
              <div className="text-white/10 select-none flex items-center gap-2">
                 <div className="w-8 h-[1px] bg-white/10" />
                 <span>{t.log_waiting_peer}</span>
              </div>
           </div>
        </div>

        <div className="col-span-4 vanguard-glass p-10 border border-white/10 flex flex-col group relative overflow-hidden">
           <div className="flex items-center gap-4 mb-8">
              <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center text-accent-green border border-accent-green/20">
                 <TrendingUp size={20} className="animate-bounce" />
              </div>
              <span className="text-[11px] font-black uppercase tracking-[0.3em] text-white/40">REWARD_TRACKER</span>
           </div>
           <div className="flex-1 flex flex-col justify-center items-center gap-2">
              <span className="text-[72px] font-black italic tracking-tighter text-white leading-none drop-shadow-[0_0_40px_rgba(255,255,255,0.1)]">0.00</span>
              <span className="text-[11px] text-accent-blue font-black tracking-[0.3em] uppercase mb-8 opacity-60">GO EARNED</span>
              <div className="w-full space-y-3 relative z-10">
                 <div className="flex justify-between p-4 bg-black/40 rounded-2xl border border-white/5 transition-all hover:border-accent-blue/30 group/stat">
                    <span className={`text-[10px] font-black uppercase tracking-widest transition-colors ${!isMining ? 'text-white' : 'text-text-muted group-hover:text-white/80'}`}>{t.verify_only}</span>
                    <span className="text-[8px] text-text-muted text-center leading-relaxed">{!isMining && <>{t.no_mining}<br/><span className="text-accent-blue/60 font-bold">{t.sync_to_mine_hint}</span></>}</span>
                 </div>
                 <div className="flex flex-col gap-3 p-5 bg-black/40 rounded-2xl border border-white/5 transition-all hover:border-accent-blue/30 group/stat">
                    <div className="flex justify-between items-center">
                       <span className="text-[9px] text-white/30 font-black uppercase tracking-[0.2em] group-hover/stat:text-white/60">{t.recipient_entity}</span>
                       <ShieldCheck size={14} className="text-accent-blue opacity-40" />
                    </div>
                    <span className="text-[11px] font-mono font-bold text-accent-blue/80 break-all bg-accent-blue/5 p-2 rounded-lg border border-accent-blue/10">{minerStatus?.miner_address || "0x----------------------------------------------------------------"}</span>
                 </div>
              </div>
           </div>
           <div className="mt-8 pt-6 border-t border-white/5 text-[9px] text-white/20 leading-relaxed text-center font-black tracking-[0.3em] uppercase">SECURED BY MATRIX V2.0 PROTOCOL</div>
        </div>
      </div>
    </div>
  );
};

export default MinerView;
