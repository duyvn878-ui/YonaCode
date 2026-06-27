/**
 * @file UnifiedExplorerPanel.tsx
 * @brief Panel Khám phá — V6.0.1 Phoenix Restoration (Fix Type-Safety)
 * @tính_năng:
 *   - Ma trận 2x2 HUD cao cấp: Hashrate, Ledger, Nodes, Next Block.
 *   - [V6.0.1] Fixed Property 'blocks' mismatch.
 *   - [V6.0.1] Fixed Property 'explorer_tab' mismatch (Use 'explorer').
 */

import { Activity, Database, Globe, Zap, Hexagon } from 'lucide-react';
import type { NodeStatus, MinerStatus, Transaction, BlockHeader } from '../../api';
import { useLanguage } from '../../LanguageContext';

interface UnifiedExplorerPanelProps {
  status: NodeStatus | null;
  miner: MinerStatus | null;
  transactions: Transaction[];
  blocks: BlockHeader[];
  supply: number;
  onTransactionClick: (tx: Transaction) => void;
}

const ExplorerStatBlock = ({ 
  title, 
  value, 
  label, 
  icon: Icon, 
  colorClass, 
  bgClass, 
  shimmer = false 
}: { 
  title: string, 
  value: string | number, 
  label: string, 
  icon: any, 
  colorClass: string, 
  bgClass: string,
  shimmer?: boolean
}) => (
  <div className={`p-8 bg-white/[0.03] border border-white/10 rounded-[2.5rem] relative overflow-hidden group hover:border-${colorClass}/40 transition-all duration-500 hover:shadow-[0_0_40px_rgba(255,255,255,0.05)]`}>
    <div className={`absolute top-0 right-0 w-32 h-32 ${bgClass} opacity-5 blur-[80px] -mr-16 -mt-16 transition-transform group-hover:scale-150 duration-1000`} />
    
    <div className="flex flex-col h-full relative z-10">
      <div className="flex items-center justify-between mb-6">
        <div className={`w-12 h-12 rounded-2xl flex items-center justify-center border ${bgClass} bg-opacity-20 border-opacity-30 ${colorClass}`}>
          <Icon size={24} className={shimmer ? 'animate-pulse' : ''} />
        </div>
        <span className={`text-[11px] font-black uppercase tracking-[0.4em] ${colorClass} italic opacity-60`}>{title}</span>
      </div>
      
      <div className="mt-auto flex flex-col gap-2">
        <span className="text-4xl font-black italic tracking-tighter text-white drop-shadow-[0_0_15px_rgba(255,255,255,0.1)] tabular-nums">
          {value}
        </span>
        <div className="flex items-center gap-2">
           <span className="text-[10px] font-bold text-white/30 uppercase tracking-[0.25em]">{label}</span>
           <div className={`h-[1px] flex-1 bg-gradient-to-r from-white/10 to-transparent`} />
        </div>
      </div>
    </div>
    
    {/* Tactical Scanline Effect */}
    <div className="absolute inset-x-0 h-[1.5px] bg-white opacity-[0.03] top-1/2 -translate-y-1/2" />
  </div>
);

const UnifiedExplorerPanel: React.FC<UnifiedExplorerPanelProps> = ({ status, miner, supply }) => {
  const { t } = useLanguage();
  
  const hashrateNum = status?.hashrate || miner?.hashrate || 0;
  const formatH = (h: number) => {
    if (h >= 1e6) return `${(h / 1e6).toFixed(2)} MH/s`;
    if (h >= 1e3) return `${(h / 1e3).toFixed(2)} KH/s`;
    return `${h.toFixed(0)} H/s`;
  };

  return (
    <div className="glass-card flex flex-col gap-10 min-h-[600px] shadow-[0_0_50px_rgba(0,0,0,0.8)] relative z-20">
      
      {/* 🚀 1. EXPLORER HEADER */}
      <div className="flex flex-col border-b border-white/[0.08] pb-10">
         <div className="flex items-center gap-5 mb-4">
            <div className="w-12 h-12 rounded-2xl bg-accent-blue/10 flex items-center justify-center text-accent-blue border border-accent-blue/20 shadow-[0_0_15px_rgba(0,136,255,0.2)]">
               <Globe size={24} className="animate-spin-slow" />
            </div>
            <div className="flex flex-col">
               {/* Fixed property: t.explorer_tab -> t.explorer */}
               <h2 className="text-3xl font-black text-white italic tracking-tighter uppercase">{t.explorer}</h2>
               <span className="text-[10px] font-bold text-accent-blue/50 uppercase tracking-[0.5em]">SYSTEM_NETWORK_MATRIX_V2.0</span>
            </div>
         </div>
      </div>

      {/* 🏥 2. THE 2x2 HUD MATRIX (V6.0 Phoenix Edition) */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-8 flex-1">
         <ExplorerStatBlock 
            title={t.mining_intel || "HASH_RATE"}
            value={formatH(hashrateNum)}
            label="THROUGHPUT_EFFICIENCY"
            icon={Zap}
            colorClass="text-accent-blue"
            bgClass="bg-accent-blue"
            shimmer={hashrateNum > 0}
         />
         
         <ExplorerStatBlock 
            title={t.storage_label || "LEDGER_CIRCULATION"}
            value={supply.toLocaleString(undefined, { maximumFractionDigits: 2 })}
            label="GO_SUPPLY_L0"
            icon={Database}
            colorClass="text-accent-amber"
            bgClass="bg-accent-amber"
         />
         
         <ExplorerStatBlock 
            title={t.network_intel || "NODE_CONSENSUS"}
            value={(status?.highest_height === 0 && status?.sync.state !== 'SYNCED' && status?.sync.state !== 'STREAMING') ? 'SCANNING...' : `#${status?.highest_height || 0}/${Math.max(status?.highest_height || 0, status?.sync?.target || 0)}`}
            label="VERIFIED_BLOCK_STAMP"
            icon={Hexagon}
            colorClass="text-accent-green"
            bgClass="bg-accent-green"
         />
         
         <ExplorerStatBlock 
            title="NEXT_BLOCK_EST"
            value={new Date().toLocaleTimeString()}
            label="L1_BLOCK_PRODUCTION"
            icon={Activity}
            colorClass="text-purple-400"
            bgClass="bg-purple-500"
            shimmer
         />
      </div>

      {/* ☢️ 3. LIVE DIAGNOSTIC BOX */}
      <div className="mt-auto p-8 bg-black/50 border border-white/10 rounded-[2rem] relative overflow-hidden">
         <div className="absolute top-0 left-0 w-full h-[2px] bg-gradient-to-r from-transparent via-accent-blue/40 to-transparent" />
         <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
               <span className="text-[11px] font-black text-white/40 uppercase tracking-[0.4em] italic">CORE_DIAGNOSTICS</span>
               <div className="flex items-center gap-3">
                  <span className="text-[10px] font-black text-accent-blue uppercase tracking-widest animate-pulse">LIVE_STREAM</span>
                  <div className="w-2 h-2 rounded-full bg-accent-blue shadow-[0_0_10px_var(--accent-blue)]" />
               </div>
            </div>
            
            <div className="grid grid-cols-2 gap-6 pt-2">
               <div className="flex flex-col gap-1">
                  <span className="text-[9px] font-bold text-white/20 uppercase tracking-widest leading-loose">PROTOCOL_VERSION</span>
                  <span className="text-[12px] font-black text-white mono italic">GENZ_SCL_V1.1_BLAKE3</span>
               </div>
               <div className="flex flex-col gap-1">
                  <span className="text-[9px] font-bold text-white/20 uppercase tracking-widest leading-loose">MINER_PROVER</span>
                  <span className="text-[12px] font-black text-accent-green mono italic uppercase">SP1_V2_READY</span>
               </div>
            </div>
         </div>
      </div>

    </div>
  );
};

export default UnifiedExplorerPanel;
