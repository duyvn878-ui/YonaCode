import React, { useState } from 'react';
import { Key, X, CheckCircle, AlertTriangle } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface RecoverModalProps {
  isOpen: boolean;
  onClose: () => void;
  onRecover: (mnemonic: string) => Promise<boolean>;
  t: Translations;
}

const RecoverModal: React.FC<RecoverModalProps> = ({
  isOpen,
  onClose,
  onRecover,
  t
}) => {
  const [mnemonicInput, setMnemonicInput] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(false);
  const [errorMsg, setErrorMsg] = useState<string>('');

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg('');
    const clean = mnemonicInput.trim();
    if (!clean) return;

    setLoading(true);
    const success = await onRecover(clean);
    setLoading(false);

    if (success) {
      setMnemonicInput('');
      onClose();
    } else {
      setErrorMsg(t.msgRecoverError);
    }
  };

  return (
    <div className="fixed inset-0 bg-slate-950/80 backdrop-blur-md flex items-center justify-center z-50 p-4 animate-in fade-in duration-200">
      <div className="bg-[#0e1422] border-2 border-sky-500/30 rounded-2xl p-6 w-full max-w-lg flex flex-col gap-4 shadow-[0_20px_50px_rgba(0,0,0,0.8),0_0_30px_rgba(56,189,248,0.2)]">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
            <Key size={18} />
            <span>{t.recoverModalTitle}</span>
          </div>
          <button 
            onClick={onClose}
            className="w-7 h-7 rounded-lg bg-white/5 border border-slate-800 text-slate-400 hover:text-white flex items-center justify-center transition-colors"
          >
            <X size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs text-slate-400 font-medium">BIP39 Seed Phrase (12 Words):</label>
            <textarea
              value={mnemonicInput}
              onChange={(e) => setMnemonicInput(e.target.value)}
              placeholder={t.placeholderMnemonic}
              rows={3}
              disabled={loading}
              className="w-full bg-slate-950/90 border border-slate-800 focus:border-sky-500/50 rounded-xl p-3 font-mono text-xs text-slate-200 outline-none resize-none transition-all"
            />
          </div>

          {errorMsg && (
            <div className="bg-red-500/10 border border-red-500/20 rounded-xl p-3 flex items-center gap-2 text-xs text-red-400 animate-in fade-in duration-200">
              <AlertTriangle size={16} className="shrink-0" />
              <span>{errorMsg}</span>
            </div>
          )}

          <div className="flex gap-2.5 mt-1">
            <button
              type="submit"
              disabled={loading || !mnemonicInput.trim()}
              className="flex-1 py-2.5 px-4 bg-gradient-to-r from-blue-600 to-blue-700 hover:from-blue-500 hover:to-blue-600 disabled:from-blue-800 disabled:to-blue-900 disabled:opacity-50 text-white rounded-xl font-bold text-xs shadow-lg flex items-center justify-center gap-1.5 transition-all"
            >
              <CheckCircle size={14} />
              <span>{loading ? 'Processing...' : t.btnRecover}</span>
            </button>
            <button 
              type="button"
              onClick={onClose}
              disabled={loading}
              className="px-4 py-2.5 bg-white/5 border border-slate-800 hover:bg-white/10 text-white rounded-xl font-bold text-xs transition-all"
            >
              {t.btnClose}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

export default RecoverModal;
