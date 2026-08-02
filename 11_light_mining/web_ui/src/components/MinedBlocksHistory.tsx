import React from 'react';
import { Award } from 'lucide-react';
import type { MinedBlockEntry } from '../api';
import type { Translations } from '../i18n/translations';

interface MinedBlocksHistoryProps {
  blocks: MinedBlockEntry[];
  t: Translations;
}

const MinedBlocksHistory: React.FC<MinedBlocksHistoryProps> = ({ blocks, t }) => {
  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-3 shadow-xl hover:border-sky-500/30 transition-all flex-1">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
          <Award size={18} />
          <span>{t.minedBlocksHistory}</span>
        </div>
        <span className="text-[10px] font-bold text-green-400 uppercase flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />
          Realtime
        </span>
      </div>

      <div className="bg-black/60 border border-slate-800 rounded-xl overflow-hidden max-h-[170px] overflow-y-auto font-mono text-xs custom-scrollbar">
        <table className="w-full text-left border-collapse">
          <thead>
            <tr className="bg-white/[0.04] text-slate-400 border-b border-slate-800">
              <th className="py-2 px-3 font-semibold">{t.blockHeight}</th>
              <th className="py-2 px-3 font-semibold">{t.blockReward}</th>
              <th className="py-2 px-3 font-semibold">{t.blockTime}</th>
              <th className="py-2 px-3 font-semibold">Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {blocks && blocks.length > 0 ? (
              blocks.map((b, idx) => (
                <tr key={idx} className="border-b border-white/[0.03] hover:bg-white/[0.02] transition-colors">
                  <td className="py-2 px-3 font-bold text-white">#{b.height}</td>
                  <td className="py-2 px-3 text-amber-400 font-bold">+{b.reward.toFixed(4)} $YGO</td>
                  <td className="py-2 px-3 text-slate-500">{b.timestamp}</td>
                  <td className="py-2 px-3">
                    <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${
                      b.status === "Confirmed" ? "bg-green-500/15 text-green-400" :
                      b.status === "Orphaned" ? "bg-red-500/15 text-red-400" :
                      "bg-amber-500/15 text-amber-400"
                    }`}>
                      {b.status === "Confirmed" ? "Confirmed" :
                       b.status === "Orphaned" ? "Orphaned" : "Confirming"}
                    </span>
                  </td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={4} className="text-center text-slate-500 py-6 font-sans text-xs">
                  {t.noMinedBlocks}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </article>
  );
};

export default MinedBlocksHistory;
