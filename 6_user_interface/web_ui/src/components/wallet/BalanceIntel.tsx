import React, { useMemo, useEffect, useState } from 'react';
import { Pickaxe, ArrowRightLeft, TrendingUp, Activity } from 'lucide-react';
import { motion } from 'framer-motion';
import { useLanguage } from '../../LanguageContext';
import type { Transaction } from '../../api';

interface BalanceIntelProps {
  transactions: Transaction[];
  address: string;
}

const AnimatedNumber = ({ value }: { value: number }) => {
  const [displayValue, setDisplayValue] = useState(0);

  useEffect(() => {
    let start = 0;
    const end = value;
    if (start === end) return;
    
    // Smooth counter animation
    const duration = 1500;
    const startTime = performance.now();

    const animate = (time: number) => {
        const elapsed = time - startTime;
        const progress = Math.min(elapsed / duration, 1);
        // Easing: easeOutExpo
        const eased = progress === 1 ? 1 : 1 - Math.pow(2, -10 * progress);
        
        const current = end * eased;
        setDisplayValue(current);

        if (progress < 1) {
            requestAnimationFrame(animate);
        }
    };

    requestAnimationFrame(animate);
  }, [value]);

  return <span>{displayValue.toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 8 })}</span>;
};

const SegmentedBar = ({ percent, colorClass }: { percent: number, colorClass: string }) => {
    return (
        <div className="flex gap-[2px] h-3 w-full bg-black/40 p-[2px] rounded-sm border border-white/5">
            {Array.from({ length: 30 }).map((_, i) => (
                <motion.div 
                    key={i} 
                    initial={{ opacity: 0.1 }}
                    animate={{ 
                        opacity: i/30 * 100 < percent ? 1 : 0.1,
                        scaleY: i/30 * 100 < percent ? [1, 1.2, 1] : 1
                    }}
                    transition={{ 
                        duration: 0.5, 
                        delay: i * 0.02,
                        opacity: { duration: 0.1 }
                    }}
                    className={`h-full flex-1 rounded-[1px] ${i/30 * 100 < percent ? colorClass : 'bg-white/10'}`} 
                />
            ))}
        </div>
    );
};

const BalanceIntel: React.FC<BalanceIntelProps> = ({ transactions, address }) => {
  const { t } = useLanguage();

  const normalize = (addr: string) => addr?.toLowerCase().trim().replace(/^0x/, '');
  const activeAddr = useMemo(() => normalize(address), [address]);
  
  const isSameAddress = (a: string, b: string) => normalize(a) === normalize(b);

  const stats = useMemo(() => {
    let mined = 0;
    let traded = 0;

    transactions.forEach(tx => {
      const isMyTx = isSameAddress(tx.sender, activeAddr) || isSameAddress(tx.receiver, activeAddr);
      if (!isMyTx) return;

      const isCoinbase = !tx.sender || normalize(tx.sender) === "" || normalize(tx.sender) === "0".repeat(64);
      const amount = Number(tx.amount) || 0;

      if (isCoinbase) {
        mined += amount;
      } else {
        if (isSameAddress(tx.receiver, activeAddr)) traded += amount;
        if (isSameAddress(tx.sender, activeAddr)) traded -= amount;
      }
    });

    return {
      mined,
      traded,
      minedRatio: (mined / (mined + Math.max(0, traded) || 1)) * 100,
      tradeRatio: (Math.max(0, traded) / (mined + Math.max(0, traded) || 1)) * 100
    };
  }, [transactions, activeAddr]);

  return (
    <div className="vanguard-flex-v vanguard-gap-medium pt-4">
      <div className="vanguard-flex-h vanguard-gap-tiny items-center opacity-40 px-2">
         <Activity size={12} className="text-accent-blue" />
         <span className="text-[10px] font-black uppercase tracking-[0.3em] font-mono italic">Sector_Analytics_V3.0</span>
      </div>

      <div className="grid grid-cols-2 gap-4">
        {/* Mined Intel */}
        <motion.div 
            initial={{ opacity: 0, x: -20 }} animate={{ opacity: 1, x: 0 }}
            className="p-8 bg-accent-amber/5 border border-accent-amber/10 rounded-[2.5rem] relative overflow-hidden group shadow-inner flex flex-col"
        >
          <div className="absolute top-0 right-0 w-32 h-32 bg-accent-amber/10 blur-[80px] -mr-16 -mt-16 group-hover:scale-150 transition-transform duration-1000" />
          <div className="vanguard-flex-h vanguard-gap-medium items-center mb-6">
            <div className="w-10 h-10 rounded-2xl bg-accent-amber/20 flex items-center justify-center text-accent-amber shadow-[0_0_20px_rgba(255,159,10,0.15)] border border-accent-amber/20 group-hover:scale-110 transition-all">
              <Pickaxe size={20} />
            </div>
            <div className="vanguard-flex-v">
                <span className="text-[10px] font-black uppercase tracking-[0.2em] italic text-white/40">{t.mined_rewards}</span>
                <span className="text-[7px] font-bold text-accent-amber/60 uppercase">SOURCE: BLAKE3_ENGINE</span>
            </div>
          </div>
          <div className="vanguard-flex-v">
            <div className="text-3xl font-black text-white italic drop-shadow-[0_0_15px_rgba(255,159,10,0.2)] tracking-tighter mb-1">
                <AnimatedNumber value={stats.mined / 100_000_000} /> <span className="text-sm opacity-20 not-italic ml-1">GO</span>
            </div>
            <div className="vanguard-flex-h justify-between items-center mt-6 mb-2">
                <span className="text-[9px] font-black text-white/20 uppercase italic tracking-widest">{t.mined_ratio}</span>
                <span className="text-[12px] font-black text-accent-amber italic font-mono">{stats.minedRatio.toFixed(1)}%</span>
            </div>
            <SegmentedBar percent={stats.minedRatio} colorClass="bg-accent-amber shadow-[0_0_8px_rgba(255,159,10,0.4)]" />
          </div>
        </motion.div>

        {/* Trade Intel */}
        <motion.div 
            initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }}
            className="p-8 bg-accent-blue/5 border border-accent-blue/10 rounded-[2.5rem] relative overflow-hidden group shadow-inner flex flex-col"
        >
          <div className="absolute top-0 right-0 w-32 h-32 bg-accent-blue/10 blur-[80px] -mr-16 -mt-16 group-hover:scale-150 transition-transform duration-1000" />
          <div className="vanguard-flex-h vanguard-gap-medium items-center mb-6">
            <div className="w-10 h-10 rounded-2xl bg-accent-blue/20 flex items-center justify-center text-accent-blue shadow-[0_0_20px_rgba(0,136,255,0.15)] border border-accent-blue/20 group-hover:scale-110 transition-all">
              <ArrowRightLeft size={20} />
            </div>
            <div className="vanguard-flex-v">
                <span className="text-[10px] font-black uppercase tracking-[0.2em] italic text-white/40">{t.trade_balance}</span>
                <span className="text-[7px] font-bold text-accent-blue/60 uppercase">SOURCE: EXTERN_LEDGER</span>
            </div>
          </div>
          <div className="vanguard-flex-v">
             <div className={`text-3xl font-black italic drop-shadow-[0_0_15px_rgba(0,136,255,0.2)] tracking-tighter mb-1 ${stats.traded >= 0 ? 'text-white' : 'text-accent-red'}`}>
                <AnimatedNumber value={stats.traded / 100_000_000} /> <span className="text-sm opacity-20 not-italic ml-1">GO</span>
            </div>
            <div className="vanguard-flex-h justify-between items-center mt-6 mb-2">
                <span className="text-[9px] font-black text-white/20 uppercase italic tracking-widest">{t.trade_ratio}</span>
                <span className="text-[12px] font-black text-accent-blue italic font-mono">{stats.tradeRatio.toFixed(1)}%</span>
            </div>
            <SegmentedBar percent={stats.tradeRatio} colorClass="bg-accent-blue shadow-[0_0_8px_rgba(0,136,255,0.4)]" />
          </div>
        </motion.div>
      </div>

      <motion.div 
        initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }}
        className="p-8 bg-gradient-to-r from-accent-blue/10 to-black/60 border border-white/10 rounded-[2.5rem] vanguard-flex-h justify-between items-center shadow-2xl relative group hover:border-white/20 transition-all"
      >
         <div className="absolute inset-0 bg-white/[0.01] pointer-events-none" 
              style={{ backgroundImage: 'linear-gradient(rgba(255,255,255,0.05) 1px, transparent 1px)', backgroundSize: '100% 4px' }} />
         
         <div className="vanguard-flex-h vanguard-gap-medium items-center relative z-10">
            <div className="w-14 h-14 rounded-2xl bg-accent-green/10 flex items-center justify-center text-accent-green shadow-[0_0_30px_rgba(0,255,136,0.1)] border border-accent-green/20 group-hover:scale-105 transition-all">
                <TrendingUp size={28} />
            </div>
            <div className="vanguard-flex-v vanguard-gap-tiny">
                <span className="text-sm font-black text-white uppercase italic tracking-[0.2em]">{t.total_accumulated}</span>
                <div className="vanguard-flex-h vanguard-gap-tiny items-center opacity-40">
                    <div className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse" />
                    <span className="text-[10px] font-bold text-text-muted uppercase italic">DATA_SEALED_VIA_ZKP</span>
                </div>
            </div>
         </div>
         <div className="text-right relative z-10">
             <div className="text-5xl font-black text-white italic tracking-tighter drop-shadow-[0_0_30px_rgba(255,255,255,0.3)]">
                <AnimatedNumber value={(stats.mined + stats.traded) / 100_000_000} />
             </div>
             <span className="text-xs font-black text-text-muted mt-2 uppercase tracking-widest block opacity-50 font-mono italic">TOTAL_GO_RESERVE</span>
         </div>
      </motion.div>
    </div>
  );
};

export default BalanceIntel;
