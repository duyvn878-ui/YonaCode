import React from 'react';
import { Settings, Power } from 'lucide-react';
import type { Language, Translations } from '../i18n/translations';

interface HeaderProps {
  currentLang: Language;
  onSetLanguage: (lang: Language) => void;
  isMining: boolean;
  uptime: string;
  t: Translations;
  onShowToast: (msg: string, icon?: string) => void;
}

const Header: React.FC<HeaderProps> = ({ currentLang, onSetLanguage, isMining, uptime, t, onShowToast }) => {
  const handleExitApp = async () => {
    if (window.confirm('Bạn có chắc chắn muốn TẮT động cơ và THOÁT ứng dụng không?')) {
      onShowToast('Đang tắt động cơ và đóng Server...', '🔴');
      try {
        await fetch('/api/shutdown', { method: 'POST' });
      } catch (e) {}
      setTimeout(() => {
        window.close();
      }, 800);
    }
  };

  return (
    <header className="flex flex-wrap items-center justify-between gap-4 bg-slate-900/80 backdrop-blur-md border border-slate-800 p-4 rounded-2xl shadow-xl">
      <div className="flex items-center gap-3">
        <div className="w-9 h-9 rounded-full bg-gradient-to-br from-blue-500 to-blue-700 flex items-center justify-center text-white font-extrabold text-base shadow-[0_0_12px_rgba(59,130,246,0.5)] border border-white/20">
          $
        </div>
        <span className="font-bold text-lg tracking-tight text-white">$YGO Miner Dashboard</span>
      </div>

      <div className={`flex items-center gap-2.5 px-4 py-1.5 rounded-full text-xs font-semibold tracking-wider border transition-all ${
        isMining 
          ? 'bg-green-500/10 border-green-500/30 text-green-400' 
          : 'bg-slate-800/50 border-slate-700 text-slate-400'
      }`}>
        <span className={`w-2.5 h-2.5 rounded-full transition-all ${
          isMining ? 'bg-green-500 shadow-[0_0_10px_#22c55e] animate-pulse' : 'bg-slate-500'
        }`} />
        <span>{isMining ? t.statusActive : t.statusPaused}</span>
      </div>

      <div className="flex items-center gap-3">
        {/* Trilingual Selector */}
        <div className="flex bg-black/40 border border-slate-800 rounded-lg p-1 gap-1">
          {(['vn', 'zh', 'en'] as const).map((lang) => (
            <button
              key={lang}
              onClick={() => onSetLanguage(lang)}
              className={`px-2.5 py-1 rounded text-xs font-bold uppercase transition-all ${
                currentLang === lang 
                  ? 'bg-blue-600 text-white shadow-[0_0_10px_rgba(37,99,235,0.5)]' 
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {lang}
            </button>
          ))}
        </div>

        <div className="bg-white/[0.04] border border-slate-800 px-3.5 py-1.5 rounded-lg font-mono text-xs text-slate-400">
          Uptime: {uptime}
        </div>

        <button 
          onClick={handleExitApp}
          className="px-3 py-1.5 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 hover:bg-red-500/20 hover:text-red-300 flex items-center gap-1.5 text-xs font-bold transition-all shadow-[0_0_10px_rgba(239,68,68,0.2)]"
          title="Tắt động cơ & Thoát ứng dụng"
        >
          <Power size={16} />
          <span>TẮT & THOÁT</span>
        </button>

        <button 
          onClick={() => onShowToast('Settings Dialog', '⚙️')}
          className="w-9 h-9 rounded-lg bg-white/5 border border-slate-800 text-slate-400 hover:text-white hover:bg-white/10 flex items-center justify-center transition-all"
        >
          <Settings size={18} />
        </button>
      </div>
    </header>
  );
};

export default Header;
