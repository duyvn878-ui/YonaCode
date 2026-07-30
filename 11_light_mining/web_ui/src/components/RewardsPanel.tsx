import React from 'react';
import { Gift } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface RewardsPanelProps {
  blocksFound: number;
  walletBalance: number;
  t: Translations;
}

const RewardsPanel: React.FC<RewardsPanelProps> = ({ blocksFound, walletBalance, t }) => {
  const balStr = (walletBalance || 0).toFixed(4);

  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-3.5 shadow-xl hover:border-sky-500/30 transition-all">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
          <Gift size={18} />
          <span>{t.rewardsBlocks}</span>
        </div>
        <div className="w-5 h-5 rounded-full bg-sky-500/10 flex items-center justify-center text-sky-400 font-bold text-xs border border-sky-500/20">
          $
        </div>
      </div>

      <div className="flex justify-between items-center text-xs font-mono text-slate-400">
        <span>{t.minedBlocks}</span>
        <strong className="text-white text-base font-mono">{blocksFound}</strong>
      </div>

      <div className="flex justify-between items-center text-xs font-mono text-slate-400">
        <span>{t.totalRewards}</span>
        <span className="text-slate-300 font-mono">{balStr} $YGO</span>
      </div>

      <div className="bg-gradient-to-r from-amber-500/10 to-amber-500/5 border-2 border-amber-500 rounded-xl p-4 flex items-center justify-center shadow-[0_0_20px_rgba(245,158,11,0.35)] my-0.5">
        <span className="font-mono text-2xl font-black text-amber-400 drop-shadow-[0_0_10px_rgba(245,158,11,0.5)]">
          {balStr} $YGO
        </span>
      </div>

      <div className="flex justify-between items-center text-xs font-mono text-slate-400">
        <span>{t.pendingRewards}</span>
        <strong className="text-amber-400 font-mono">0.00 $YGO</strong>
      </div>
    </article>
  );
};

export default RewardsPanel;
