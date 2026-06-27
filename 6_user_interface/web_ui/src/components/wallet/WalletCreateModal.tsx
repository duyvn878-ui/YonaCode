import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, ShieldCheck, Copy, CheckCircle2, AlertTriangle, Key } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';

interface WalletCreateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onCreate: (password: string, passphrase: string) => Promise<string>;
}

const WalletCreateModal: React.FC<WalletCreateModalProps> = ({ isOpen, onClose, onCreate }) => {
  const [copied, setCopied] = useState(false);
  const [password, setPassword] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [error, setError] = useState('');
  const [mnemonic, setMnemonic] = useState('');
  const [isCreating, setIsCreating] = useState(false);
  const { t, lang } = useLanguage();

  const handleCopy = () => {
    navigator.clipboard.writeText(mnemonic);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCreateSubmit = async () => {
    if (!password) {
      setError(t.password_required || "Password cannot be empty");
      return;
    }
    setError('');
    setIsCreating(true);
    try {
      const generatedMnemonic = await onCreate(password, passphrase);
      setMnemonic(generatedMnemonic);
    } catch (e: any) {
      setError(e.message || "Failed to create wallet");
    } finally {
      setIsCreating(false);
    }
  };

  const resetAndClose = () => {
    setMnemonic('');
    setPassword('');
    setError('');
    onClose();
  };

  if (!isOpen) return null;

  const words = mnemonic ? mnemonic.split(/\s+/) : [];

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-[4500] flex items-center justify-center p-6 text-white">
        <motion.div 
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
          onClick={resetAndClose}
          className="absolute inset-0 bg-black/90 backdrop-blur-2xl"
        />
        
        <motion.div 
          initial={{ scale: 0.9, opacity: 0, y: 20 }}
          animate={{ scale: 1, opacity: 1, y: 0 }}
          exit={{ scale: 0.9, opacity: 0, y: 20 }}
          className="relative glass-card w-full max-w-2xl border-accent-blue/20 shadow-[0_0_80px_rgba(0,136,255,0.15)] overflow-hidden"
        >
          {/* Header */}
          <div className="vanguard-panel-header vanguard-flex-h justify-between items-center bg-white/[0.03]">
            <div className="vanguard-flex-h vanguard-gap-small">
              <div className="w-12 h-12 rounded-2xl bg-accent-blue/20 flex items-center justify-center text-accent-blue shadow-[0_0_20px_rgba(0,136,255,0.3)]">
                <ShieldCheck size={24} />
              </div>
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <h3 className="text-lg font-black uppercase tracking-[0.2em] italic text-white">{t.modal_create_title}</h3>
                <span className="text-[10px] font-bold text-accent-blue uppercase tracking-widest">GENESIS_VAULT_PROTOCOL</span>
              </div>
            </div>
            <button onClick={resetAndClose} className="p-3 hover:bg-white/5 rounded-xl transition-all">
              <X size={24} className="text-text-muted hover:text-white" />
            </button>
          </div>

          <div className="vanguard-flex-v vanguard-gap-medium p-8">
            <div className="p-6 bg-accent-amber/10 border border-accent-amber/20 rounded-2xl vanguard-flex-h vanguard-gap-medium items-start">
              <AlertTriangle className="text-accent-amber shrink-0 mt-1" size={24} />
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <p className="text-[12px] font-black text-accent-amber uppercase tracking-tighter italic">
                  {t.security_alert}
                </p>
                <p className="text-[11px] font-medium leading-relaxed text-text-secondary">
                  {t.modal_create_desc}
                </p>
              </div>
            </div>
            {!mnemonic ? (

                <div className="vanguard-flex-v vanguard-gap-medium py-4">
                  <div className="vanguard-flex-v vanguard-gap-tiny">
                    <label className="text-[10px] uppercase font-black tracking-widest text-text-secondary pl-1">
                      {t.password_label || "WALLET PIN (FILE ENCRYPTION)"}
                    </label>
                    <div className="relative">
                      <Key size={20} className="absolute left-5 top-1/2 -translate-y-1/2 text-accent-blue/50" />
                      <input
                        type="password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder={t.password_placeholder || "Set a secure password..."}
                        className="w-full bg-black/40 border border-white/10 rounded-2xl py-5 pl-14 pr-6 text-sm font-medium text-white placeholder-text-muted focus:outline-none focus:border-accent-blue/50 transition-all font-mono"
                      />
                    </div>
                  </div>

                  <div className="vanguard-flex-v vanguard-gap-tiny">
                    <label className="text-[10px] uppercase font-black tracking-widest text-text-secondary pl-1">
                      {t.passphrase_label || "BIP39 PASSPHRASE (OPTIONAL 13TH WORD)"}
                    </label>
                    <div className="relative">
                      <ShieldCheck size={20} className="absolute left-5 top-1/2 -translate-y-1/2 text-accent-green/50" />
                      <input
                        type="text"
                        value={passphrase}
                        onChange={(e) => setPassphrase(e.target.value)}
                        placeholder={lang === 'vi' ? "Để trống nếu không dùng passphrase..." : "Optional extra security word..."}
                        className="w-full bg-black/40 border border-white/10 rounded-2xl py-5 pl-14 pr-6 text-sm font-medium text-white placeholder-text-muted focus:outline-none focus:border-accent-green/50 transition-all font-mono"
                      />
                    </div>
                  </div>
                  
                  {error && <div className="text-accent-red text-xs px-2">{error}</div>}
                  <button 
                    onClick={handleCreateSubmit}
                    disabled={isCreating}
                    className="mt-2 px-10 py-5 bg-accent-blue/20 border border-accent-blue/50 hover:bg-accent-blue/30 rounded-2xl text-accent-blue text-[11px] font-black uppercase tracking-widest italic transition-all disabled:opacity-50"
                  >
                    {isCreating ? "..." : t.confirm_action || "CONFIRM OPERATION"}
                  </button>
                </div>
            ) : (
              <>
                {/* Mnemonic Grid */}
                <div className="grid grid-cols-3 md:grid-cols-4 gap-3 py-4">
                  {words.map((word, i) => (
                    <div key={i} className="p-3 bg-black/40 border border-white/5 rounded-xl vanguard-flex-h vanguard-gap-small items-center">
                      <span className="text-[10px] font-bold text-text-muted w-4 opacity-50">{i + 1}.</span>
                      <span className="text-xs font-black text-white mono">{word}</span>
                    </div>
                  ))}
                </div>

                <div className="vanguard-flex-h vanguard-gap-medium pt-4">
                  <button 
                    onClick={handleCopy}
                    className="vanguard-flex-h vanguard-gap-medium items-center px-8 py-5 bg-white/5 border border-white/10 hover:bg-white/10 rounded-2xl transition-all active:scale-[0.98] grow"
                  >
                    {copied ? <CheckCircle2 size={18} className="text-accent-green" /> : <Copy size={18} className="text-accent-blue" />}
                    <span className="text-[11px] font-black uppercase tracking-widest italic">
                      {copied ? t.copied_text : t.copy_mnemonic}
                    </span>
                  </button>
                  
                  <button 
                    onClick={resetAndClose}
                    className="px-10 py-5 bg-accent-blue/10 border border-accent-blue/30 hover:bg-accent-blue/20 rounded-2xl text-accent-blue text-[11px] font-black uppercase tracking-widest italic transition-all grow"
                  >
                    {t.data_sealed}
                  </button>
                </div>
              </>
            )}
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};

export default WalletCreateModal;
