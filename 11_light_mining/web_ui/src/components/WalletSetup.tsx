import React from 'react';
import { Key, Copy } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface WalletSetupProps {
  wallet: string;
  onWalletChange: (w: string) => void;
  onCopyWallet: () => void;
  onOpenBip39Modal: () => void;
  t: Translations;
}

const WalletSetup: React.FC<WalletSetupProps> = ({ wallet, onWalletChange, onCopyWallet, onOpenBip39Modal, t }) => {
  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-4 shadow-xl hover:border-sky-500/30 transition-all">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
          <Key size={18} />
          <span>{t.walletSetup}</span>
        </div>
        <div className="w-5 h-5 rounded-full bg-sky-500/10 flex items-center justify-center text-sky-400 font-bold text-xs border border-sky-500/20">
          $
        </div>
      </div>

      <div className="text-xs text-slate-400 font-medium">{t.yourWallet}</div>
      <div className="bg-black/60 border border-slate-800 rounded-xl p-3 flex items-center justify-between gap-2">
        <input 
          type="text"
          value={wallet}
          onChange={(e) => onWalletChange(e.target.value)}
          placeholder="0x..."
          className="bg-transparent border-none text-sky-400 font-mono text-xs w-full outline-none"
        />
        <button 
          onClick={onCopyWallet}
          className="w-7 h-7 rounded bg-white/5 border border-slate-800 text-slate-400 hover:text-white flex items-center justify-center transition-colors"
          title="Copy"
        >
          <Copy size={14} />
        </button>
      </div>

      <button 
        onClick={onCopyWallet}
        className="w-full py-3 px-4 rounded-xl bg-gradient-to-r from-blue-600 to-blue-700 hover:from-blue-500 hover:to-blue-600 text-white font-bold text-xs tracking-wider uppercase flex items-center justify-center gap-2 shadow-[0_0_15px_rgba(37,99,235,0.4)] transition-all"
      >
        <Copy size={14} />
        <span>{t.btnCopyWallet}</span>
      </button>

      <button 
        onClick={onOpenBip39Modal}
        className="w-full py-3 px-4 rounded-xl bg-white/5 border border-slate-800 hover:bg-white/10 text-white font-bold text-xs tracking-wider uppercase flex items-center justify-center gap-2 transition-all"
      >
        <Key size={14} />
        <span>{t.btnGenBip39}</span>
      </button>
    </article>
  );
};

export default WalletSetup;
