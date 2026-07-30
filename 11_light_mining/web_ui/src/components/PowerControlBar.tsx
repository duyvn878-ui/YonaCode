import React from 'react';
import { Pickaxe } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface PowerControlBarProps {
  isMining: boolean;
  onToggleMining: () => void;
  t: Translations;
}

const PowerControlBar: React.FC<PowerControlBarProps> = ({ isMining, onToggleMining, t }) => {
  return (
    <footer className="w-full mt-auto bg-slate-900/90 backdrop-blur-md border border-slate-800 rounded-3xl p-5 shadow-2xl">
      {/* Main Start / Stop Mining Toggle Button */}
      <button
        onClick={onToggleMining}
        className={`w-full py-4 px-6 rounded-2xl font-black text-base tracking-wider uppercase transition-all duration-300 flex items-center justify-center gap-3 shadow-xl ${
          isMining
            ? 'bg-gradient-to-r from-red-600 to-red-700 hover:from-red-500 hover:to-red-600 text-white shadow-[0_0_20px_rgba(239,68,68,0.4)]'
            : 'bg-gradient-to-r from-green-600 to-emerald-600 hover:from-green-500 hover:to-emerald-500 text-white shadow-[0_0_20px_rgba(34,197,94,0.4)]'
        }`}
      >
        <Pickaxe size={20} className={isMining ? 'animate-bounce' : ''} />
        <span>{isMining ? t.btnStop : t.btnStart}</span>
      </button>
    </footer>
  );
};

export default PowerControlBar;
