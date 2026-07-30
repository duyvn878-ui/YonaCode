import React from 'react';
import { Key, Copy, Download, X } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface Bip39ModalProps {
  isOpen: boolean;
  onClose: () => void;
  seedWords: string[];
  generatedTime: string;
  onCopySeed: () => void;
  onDownloadPDF: () => void;
  t: Translations;
}

const Bip39Modal: React.FC<Bip39ModalProps> = ({
  isOpen,
  onClose,
  seedWords,
  generatedTime,
  onCopySeed,
  onDownloadPDF,
  t
}) => {
  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 bg-slate-950/80 backdrop-blur-md flex items-center justify-center z-50 p-4 animate-in fade-in duration-200">
      <div className="bg-[#0e1422] border-2 border-sky-500/30 rounded-2xl p-6 w-full max-w-lg flex flex-col gap-4 shadow-[0_20px_50px_rgba(0,0,0,0.8),0_0_30px_rgba(56,189,248,0.2)]">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
            <Key size={18} />
            <span>BIP39 Seed Generator</span>
          </div>
          <button 
            onClick={onClose}
            className="w-7 h-7 rounded-lg bg-white/5 border border-slate-800 text-slate-400 hover:text-white flex items-center justify-center transition-colors"
          >
            <X size={16} />
          </button>
        </div>

        <div className="flex justify-between items-center text-xs text-slate-400">
          <span>{t.uniqueWords}</span>
          <span className="text-[10px] text-slate-500">{generatedTime}</span>
        </div>

        <div className="grid grid-cols-4 gap-2 bg-slate-950/90 border border-slate-800 rounded-xl p-4 font-mono text-xs text-slate-200">
          {seedWords && seedWords.length > 0 ? (
            seedWords.map((word, idx) => (
              <div key={idx} className="bg-white/5 p-2 rounded text-center border border-white/5 font-semibold">
                {word}
              </div>
            ))
          ) : (
            Array.from({ length: 12 }).map((_, idx) => (
              <div key={idx} className="bg-white/5 p-2 rounded text-center border border-white/5 text-slate-600">
                ----
              </div>
            ))
          )}
        </div>

        <div className="flex gap-2.5 mt-1">
          <button 
            onClick={onCopySeed}
            className="flex-1 py-2.5 px-4 bg-gradient-to-r from-blue-600 to-blue-700 hover:from-blue-500 hover:to-blue-600 text-white rounded-xl font-bold text-xs shadow-lg flex items-center justify-center gap-1.5 transition-all"
          >
            <Copy size={14} />
            <span>{t.btnCopySeed}</span>
          </button>
          <button 
            onClick={onDownloadPDF}
            className="flex-1 py-2.5 px-4 bg-white/5 border border-slate-800 hover:bg-white/10 text-white rounded-xl font-bold text-xs flex items-center justify-center gap-1.5 transition-all"
          >
            <Download size={14} />
            <span>{t.btnDownloadPDF}</span>
          </button>
          <button 
            onClick={onClose}
            className="px-4 py-2.5 bg-white/5 border border-slate-800 hover:bg-white/10 text-white rounded-xl font-bold text-xs transition-all"
          >
            {t.btnClose}
          </button>
        </div>
      </div>
    </div>
  );
};

export default Bip39Modal;
