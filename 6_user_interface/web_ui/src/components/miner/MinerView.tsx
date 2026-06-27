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

const MinerView: React.FC<MinerViewProps> = ({ status, minerStatus, handleToggleMiner, onNotify, isStopping = false }) => {
  const { t } = useLanguage();
  const [intensity, setIntensity] = useState(status?.cpu_intensity || 50);

  useEffect(() => {
    if (status?.cpu_intensity) {
      setIntensity(status.cpu_intensity);
    }
  }, [status?.cpu_intensity]);

  const isMining = (status?.is_mining !== undefined) ? status.is_mining : (minerStatus?.is_mining || false);
  const hashrate = status?.hashrate || minerStatus?.hashrate || 0;
  const gracePeriodRemaining = Math.ceil(status?.grace_period_remaining || minerStatus?.grace_period_remaining || 0);

  const handleIntensityChange = async (val: number) => {
    setIntensity(val);
    try {
      await api.setCpuIntensity(val);
    } catch (e) {
      onNotify(`❌ Error: ${(e as Error).message}`, 'error');
    }
  };

  return (
    <div className="vanguard-flex-v vanguard-gap-medium h-full animate-in fade-in slide-in-from-bottom-4 duration-700">
      <div className="grid grid-cols-12 gap-6 h-[420px]">
        <div className="col-span-3 vanguard-glass p-8 border-l-4 border-l-accent-blue/40 flex flex-col justify-between overflow-hidden relative group">
           <div className="absolute -right-8 -top-8 w-32 h-32 bg-accent-blue/5 rounded-full blur-3xl group-hover:bg-accent-blue/10 transition-all duration-1000" />
           
           <div>
              <div className="flex items-center gap-3 mb-6">
                 <div className="w-10 h-10 rounded-xl bg-accent-blue/10 flex items-center justify-center text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]">
                    <Zap size={20} className={isMining ? 'animate-pulse' : ''} />
                 </div>
                 <span className="text-[12px] font-black uppercase tracking-[0.3em] text-white italic">{t.miner_status}</span>
              </div>
              
              <div className="vanguard-stats-card p-6 bg-black/40 border border-white/[0.03] rounded-2xl mb-4 relative overflow-hidden">
                  <div className="flex flex-col gap-3">
                     <span className="tactical-label text-[10px] text-white/30 uppercase font-black">SYSTEM_MODE</span>
                     
                     <div className="flex flex-col gap-3">
                        <div className="flex items-center gap-3">
                           <span className={`text-2xl font-black italic tracking-tighter ${isMining ? 'text-accent-green' : 'text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]'}`}>
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
                          const progress = isSnapshot && status.sync.snapshot_chunks_total && status.sync.snapshot_chunks_total > 0
                            ? (status.sync.snapshot_chunks_loaded || 0) / status.sync.snapshot_chunks_total * 100
                            : (status.sync.target > 0 ? (status.sync.current / status.sync.target) * 100 : 0);
                          
                          return (
                            <div className="vanguard-flex-v vanguard-gap-tiny w-full mt-2">
                               <div className="flex justify-between items-center text-[9px] font-black italic">
                                  <span className="text-accent-blue uppercase tracking-widest">
                                     {isSnapshot ? "Snapshot Progress" : "Sync Progress"}
                                  </span>
                                  <span className="text-white/60">{progress.toFixed(2)}%</span>
                               </div>
                               <div className="h-1.5 w-full bg-white/5 rounded-full border border-white/5 overflow-hidden">
                                  <motion.div 
                                    initial={{ width: 0 }}
                                    animate={{ width: `${progress}%` }}
                                    className="h-full bg-accent-blue shadow-[0_0_10px_var(--accent-blue)]"
                                  />
                               </div>
                               <div className="flex justify-between items-center text-[8px] text-white/30 font-bold uppercase tracking-tighter">
                                  {isSnapshot && status.sync.snapshot_chunks_total && status.sync.snapshot_chunks_total > 0 ? (
                                    <>
                                      <span>CHUNK: {status.sync.snapshot_chunks_loaded || 0}</span>
                                      <span>TOTAL: {status.sync.snapshot_chunks_total}</span>
                                    </>
                                  ) : (
                                    <>
                                      <span>HT: {status.sync.current}</span>
                                      <span>TG: {status.sync.target}</span>
                                    </>
                                  )}
                               </div>
                            </div>
                          );
                        })()}

                        {!isMining && status?.sync.state !== 'SYNCING' && status?.sync.state !== 'BOOTSTRAPPING' && (
                           <p className="text-[9px] font-bold text-white/40 mt-2 leading-relaxed italic">
                              {t.no_mining} <span className="text-accent-blue/60">{t.sync_to_mine_hint}</span>
                           </p>
                        )}
                        
                        {(status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') && (
                          <p className="text-[9px] font-bold text-accent-blue/80 mt-2 leading-relaxed italic animate-pulse">
                             ⚡ {t.sync_to_mine_hint}
                          </p>
                        )}
                     </div>
                  </div>
              </div>
           </div>

           <div 
             onClick={(!isStopping && gracePeriodRemaining === 0) ? handleToggleMiner : undefined}
             className={`w-full py-4 px-5 rounded-2xl transition-all relative overflow-hidden group/btn flex items-center justify-between border
               ${(isStopping || gracePeriodRemaining > 0)
                 ? 'bg-white/5 border-white/10 cursor-wait opacity-60' 
                 : (isMining 
                   ? 'bg-accent-green/5 border-accent-green/30 shadow-[0_0_30px_rgba(0,242,148,0.1)] cursor-pointer' 
                   : 'bg-black/40 border-white/[0.08] hover:bg-white/[0.04] cursor-pointer')}`}
           >
              <div className="flex items-center gap-4 relative z-10">
                 <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${(isStopping || gracePeriodRemaining > 0) ? 'bg-white/5 text-white/20' : (isMining ? 'bg-accent-green/20 text-accent-green shadow-[0_0_15px_rgba(0,242,148,0.4)]' : 'bg-white/5 text-text-muted')}`}>
                   {gracePeriodRemaining > 0 ? (
                     <Activity size={20} className="animate-spin" />
                   ) : (
                     <Pickaxe size={20} className={isStopping ? 'animate-spin' : ''} />
                   )}
                 </div>
                 <div className="flex flex-col items-start gap-1">
                    <span className="text-[13px] font-black uppercase tracking-widest text-white">
                      {/* Tại sao: Thay đổi nhãn nút động theo trạng thái isMining của Node thay vì hardcode nút Bắt đầu, tránh việc người dùng thấy nút bị kẹt khi backend thực tế đang băm */}
                      {gracePeriodRemaining > 0 ? 'ĐANG QUÉT MẠNG...' : (isStopping ? 'CỬA SỔ DỪNG...' : (isMining ? t.mining_stop_btn : t.mining_start_btn))}
                    </span>
                   <span className={`text-[10px] font-bold uppercase tracking-widest ${gracePeriodRemaining > 0 ? 'text-accent-blue' : (isStopping ? 'text-accent-amber' : (isMining ? 'text-accent-green' : 'text-text-muted'))}`}>
                      {gracePeriodRemaining > 0 ? `⌛ CÒN ${gracePeriodRemaining} GIÂY` : (isStopping ? '🟡 ĐANG TẮT...' : (isMining ? `🟢 ${t.toggle_on}` : `⚪ ${t.toggle_off}`))}
                   </span>
                 </div>
              </div>
              <div className={`w-14 h-7 rounded-full p-1 transition-all duration-300 flex items-center relative z-10 
                 ${(isStopping || gracePeriodRemaining > 0) ? 'bg-white/5 justify-center' : (isMining ? 'bg-accent-green shadow-[0_0_15px_rgba(0,242,148,0.4)] justify-end' : 'bg-white/10 justify-start')}`}>
                 <div className={`w-5 h-5 bg-white rounded-full shadow-lg transition-all duration-300 ${isStopping ? 'animate-pulse' : (isMining ? 'scale-110' : 'scale-90 opacity-50 relative right-0')}`} />
              </div>
           </div>

           <div className="w-full flex flex-col gap-3 p-4 bg-black/60 border border-white/[0.05] rounded-2xl relative z-10 mt-[-10px]">
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
        </div>

        <div className="col-span-6 vanguard-glass p-10 flex flex-col items-center justify-center relative overflow-hidden group">
           <div className={`absolute inset-0 transition-opacity duration-1000 ${isMining ? 'opacity-20' : 'opacity-0'}`}>
              <div className="absolute inset-x-0 top-0 h-[1px] bg-gradient-to-r from-transparent via-accent-blue to-transparent animate-[shimmer_3s_infinite]" />
              <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,rgba(0,136,255,0.1),transparent_70%)]" />
           </div>
           <div className="relative z-10 flex flex-col items-center gap-4 w-full">
              <div className="flex items-center gap-3 p-3 bg-white/[0.03] border border-white/[0.05] rounded-2xl mb-4 backdrop-blur-md">
                 <Activity size={16} className="text-accent-blue animate-pulse" />
                 <span className="tactical-label text-[10px] text-white/50 font-black tracking-[0.3em] uppercase">SYSTEM_THROUGHPUT_REALTIME</span>
              </div>
              <div className="relative mb-8 group/hash">
                 <div className="absolute -inset-10 bg-accent-blue/5 rounded-full blur-[80px] group-hover/hash:bg-accent-blue/15 transition-all duration-1000" />
                 <AnimatePresence mode='wait'>
                    <motion.div
                      key={hashrate}
                      initial={{ scale: 0.9, opacity: 0, filter: 'blur(20px)' }}
                      animate={{ scale: 1, opacity: 1, filter: 'blur(0px)' }}
                      className="relative z-10 text-[140px] font-black italic tracking-tighter leading-none flex items-baseline gap-6"
                    >
                       <span className="bg-gradient-to-b from-white via-white to-white/30 bg-clip-text text-transparent drop-shadow-[0_0_50px_rgba(0,136,255,0.4)]">
                          {hashrate.toLocaleString()}
                       </span>
                       <span className="text-4xl text-accent-blue font-black tracking-[0.4em] uppercase opacity-80">H/S</span>
                    </motion.div>
                 </AnimatePresence>
                 <div className="absolute -top-16 left-1/2 -translate-x-1/2 opacity-5 pointer-events-none">
                    <Globe size={300} className="text-white animate-[spin_20s_linear_infinite]" />
                 </div>
              </div>
              <div className="flex gap-16 mt-6 relative z-10">
                 <div className="flex flex-col items-center">
                    <span className="text-[12px] text-white/30 font-black tracking-[0.4em] mb-2 uppercase italic">BLOCK_REWARD</span>
                    <span className="text-[20px] text-accent-amber font-black italic shadow-text">
                      {((status?.block_reward || 0) / 100000000).toLocaleString(undefined, {minimumFractionDigits: 8})} GO
                    </span>
                 </div>
                 <div className="w-[1px] h-12 bg-white/10" />
                 <div className="flex flex-col items-center">
                    <span className="text-[12px] text-white/30 font-black tracking-[0.4em] mb-2 uppercase italic">NETWORK_DIFFICULTY</span>
                    <span className="text-[20px] text-accent-green font-black italic shadow-text">
                      {status?.difficulty?.toLocaleString() || "..."}
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

        <div className="col-span-3 vanguard-glass p-8 flex flex-col justify-between group relative overflow-hidden">
           <div>
              <div className="flex items-center gap-3 mb-10">
                 <div className="w-12 h-12 rounded-2xl bg-accent-amber/10 flex items-center justify-center text-accent-amber shadow-[0_0_25px_rgba(247,173,63,0.2)] border border-accent-amber/20">
                    <Cpu size={24} />
                 </div>
                 <div className="flex flex-col">
                    <span className="text-[14px] font-black uppercase tracking-[0.4em] text-white italic leading-none">{t.cpu_intensity}</span>
                    <span className="text-[8px] text-accent-amber font-bold tracking-widest opacity-60">HARDWARE_OPTIMIZER_ACTIVE</span>
                 </div>
              </div>
              <div className="vanguard-stats-card p-8 bg-black/60 border border-white/[0.05] rounded-[32px] relative overflow-hidden shadow-2xl">
                 <div className="flex justify-between items-end mb-6">
                    <span className="tactical-label text-[12px] text-white/40 uppercase font-black tracking-[0.2em] italic">CORE_STRESS_LVL</span>
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
                 <div className="flex items-center gap-3 mt-8 p-3 bg-accent-amber/5 rounded-xl border border-accent-amber/10 text-[10px] text-accent-amber/80 italic font-bold">
                    <AlertTriangle size={14} />
                    <span className="uppercase tracking-widest leading-relaxed">{t.thermal_warning}</span>
                 </div>
              </div>
           </div>
           <div className="p-6 rounded-[24px] bg-gradient-to-br from-accent-blue/10 to-transparent border border-accent-blue/20 flex items-center justify-between shadow-lg">
              <div className="flex flex-col gap-1">
                 <span className="text-[10px] text-white/30 font-black uppercase tracking-[0.4em] leading-none">VANGUARD_CORE</span>
                 <span className="text-[14px] text-white font-black italic">SP1_V2_PROVER: ACTIVE</span>
              </div>
              <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center text-accent-green shadow-[0_0_20px_rgba(34,197,94,0.3)]">
                 <ShieldCheck size={24} strokeWidth={2.5} />
              </div>
           </div>
        </div>
      </div>

      <div className="grid grid-cols-12 gap-6 flex-1">
        <div className="col-span-8 vanguard-glass p-8 overflow-hidden flex flex-col border-t-4 border-t-accent-blue/40 shadow-[inset_0_4px_50px_rgba(0,136,255,0.1)]">
           <div className="flex items-center justify-between mb-6 px-2">
              <div className="flex items-center gap-4">
                 <div className="w-2 h-6 bg-accent-blue rounded-full shadow-[0_0_15px_rgba(0,136,255,0.5)]" />
                 <div>
                    <span className="text-[14px] font-black uppercase tracking-[0.5em] text-white italic">TRANSMISSION_PROTOCOL</span>
                    <div className="h-[1px] w-full bg-gradient-to-r from-accent-blue/40 to-transparent mt-1" />
                 </div>
              </div>
              <div className="flex items-center gap-3 bg-black/40 px-4 py-2 rounded-full border border-white/5">
                 <span className="text-[10px] text-accent-blue font-mono font-black italic animate-pulse">LIVE_CONN</span>
                 <div className="w-1 h-1 rounded-full bg-accent-blue" />
                 <span className="text-[10px] text-white/40 font-mono italic">STREAM: SCL_SSE_8080</span>
              </div>
           </div>
           <div className="flex-1 bg-black/80 rounded-[32px] p-8 font-mono text-[12px] leading-relaxed overflow-y-auto space-y-4 custom-scrollbar border border-white/[0.05] shadow-inner">
              <div className="flex gap-6 items-start">
                 <span className="text-accent-blue font-black min-w-[90px] border-r border-white/10 pr-4">[CORE]</span>
                 <span className="text-white/50">{t.log_core_init}</span>
              </div>
              <div className="flex gap-6 items-start">
                 <span className="text-accent-amber font-black min-w-[90px] border-r border-white/10 pr-4">[DAA]</span>
                 <span className="text-white/50 italic">{t.log_daa_aligned}</span>
              </div>
              {isMining && (
                <motion.div initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }} className="flex gap-6 items-start">
                  <span className="text-accent-green font-black min-w-[90px] border-r border-white/10 pr-4">[POW]</span>
                  <span className="text-accent-green/80 italic font-bold">{t.log_pow_scanning}</span>
                </motion.div>
              )}
              <div className="h-6" />
              <div className="text-white/10 italic select-none flex items-center gap-2">
                 <div className="w-8 h-[1px] bg-white/10" />
                 <span>{t.log_waiting_peer}</span>
              </div>
           </div>
        </div>

        <div className="col-span-4 vanguard-glass p-10 flex flex-col group relative overflow-hidden">
           <div className="flex items-center gap-4 mb-8">
              <div className="w-10 h-10 rounded-xl bg-accent-green/10 flex items-center justify-center text-accent-green border border-accent-green/20">
                 <TrendingUp size={20} className="animate-bounce" />
              </div>
              <span className="text-[14px] font-black uppercase tracking-[0.4em] text-white italic">REWARD_TRACKER</span>
           </div>
           <div className="flex-1 flex flex-col justify-center items-center gap-2">
              <span className="text-[72px] font-black italic tracking-tighter text-white leading-none drop-shadow-[0_0_40px_rgba(255,255,255,0.1)]">0.00</span>
              <span className="text-[14px] text-accent-blue font-black tracking-[0.5em] uppercase mb-8 italic opacity-60">GO EARNED</span>
              <div className="w-full space-y-3 relative z-10">
                 <div className="flex justify-between p-4 bg-black/40 rounded-2xl border border-white/5 transition-all hover:border-accent-blue/30 group/stat">
                    <span className={`text-[10px] font-black uppercase tracking-widest transition-colors ${!isMining ? 'text-white' : 'text-text-muted group-hover:text-white/80'}`}>{t.verify_only}</span>
                    <span className="text-[8px] text-text-muted text-center leading-relaxed">{!isMining && <>{t.no_mining}<br/><span className="text-accent-blue/60 font-bold">{t.sync_to_mine_hint}</span></>}</span>
                 </div>
                 <div className="flex flex-col gap-3 p-5 bg-black/40 rounded-2xl border border-white/5 transition-all hover:border-accent-blue/30 group/stat">
                    <div className="flex justify-between items-center">
                       <span className="text-[11px] text-white/30 font-black uppercase tracking-[0.2em] italic group-hover/stat:text-white/60">{t.recipient_entity}</span>
                       <ShieldCheck size={14} className="text-accent-blue opacity-40" />
                    </div>
                    <span className="text-[11px] font-mono font-bold text-accent-blue/80 break-all bg-accent-blue/5 p-2 rounded-lg border border-accent-blue/10">{minerStatus?.miner_address || "0x----------------------------------------------------------------"}</span>
                 </div>
              </div>
           </div>
           <div className="mt-8 pt-6 border-t border-white/5 text-[10px] text-white/20 italic leading-relaxed text-center font-black tracking-[0.4em] uppercase">SECURED BY MATRIX V2.0 PROTOCOL</div>
        </div>
      </div>
    </div>
  );
};

export default MinerView;
