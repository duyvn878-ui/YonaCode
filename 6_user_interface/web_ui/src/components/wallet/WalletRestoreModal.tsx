import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, Key, ShieldCheck, AlertCircle } from 'lucide-react';

interface WalletRestoreModalProps {
  isOpen: boolean;
  onClose: () => void;
  onRestore: (mnemonic: string, password: string, passphrase: string) => Promise<void>;
}

import api from '../../api';
import { useLanguage } from '../../LanguageContext';
const WalletRestoreModal: React.FC<WalletRestoreModalProps> = ({ isOpen, onClose, onRestore }) => {
  const [mnemonic, setMnemonic] = useState('');
  const [password, setPassword] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [previewAddr, setPreviewAddr] = useState<string | null>(null);
  const [isRestoring, setIsRestoring] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { t, lang } = useLanguage();

  const cleanMnemonic = (raw: string) => {
    return raw.trim().replace(/\s+/g, ' ');
  };

  const handleRestore = async () => {
    const cleanedMnemonic = cleanMnemonic(mnemonic);
    if (cleanedMnemonic.split(' ').length < 12) {
      setError(lang === 'vi' ? 'Seed Phrase phải chứa ít nhất 12 từ.' : 'Seed Phrase must contain at least 12 words.');
      return;
    }
    if (!password) {
      setError(t.password_required || "Password cannot be empty");
      return;
    }
    
    setIsRestoring(true);
    setError(null);
    try {
      await onRestore(cleanedMnemonic, password, passphrase);
      onClose();
    } catch (e: any) {
      setError(e.message || (lang === 'vi' ? 'Lỗi khôi phục ví. Vui lòng kiểm tra lại Seed Phrase.' : 'Wallet restoration failed. Please check your Seed Phrase.'));
    } finally {
      setIsRestoring(false);
    }
  };

  // [V2.0 UNIFIED] Preview Address Real-time
  React.useEffect(() => {
    const fetchPreview = async () => {
      const cleanedMnemonic = cleanMnemonic(mnemonic);
      if (cleanedMnemonic.split(' ').length >= 12) {
        try {
          const res = await api.previewWallet(cleanedMnemonic, passphrase);
          if (res.valid) {
            setPreviewAddr(res.address);
          } else {
            setPreviewAddr(null);
          }
        } catch {
          setPreviewAddr(null);
        }
      } else {
        setPreviewAddr(null);
      }
    };
    const timer = setTimeout(fetchPreview, 500); // Debounce 500ms
    return () => clearTimeout(timer);
  }, [mnemonic, passphrase]);

  if (!isOpen) return null;

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-[4000] flex items-center justify-center p-6 text-white">
        <motion.div 
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
          onClick={onClose}
          className="absolute inset-0 bg-black/80 backdrop-blur-xl"
        />
        
        <motion.div 
          initial={{ scale: 0.9, opacity: 0, y: 20 }}
          animate={{ scale: 1, opacity: 1, y: 0 }}
          exit={{ scale: 0.9, opacity: 0, y: 20 }}
          className="relative glass-card w-full max-w-lg overflow-hidden border-white/[0.08] shadow-[0_0_50px_rgba(0,0,0,0.5)]"
        >
          {/* Header */}
          <div className="vanguard-panel-header vanguard-flex-h justify-between items-center bg-white/[0.03]">
            <div className="vanguard-flex-h vanguard-gap-small min-w-0 flex-1">
              <div className="w-10 h-10 rounded-xl bg-accent-blue/20 flex items-center justify-center text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]">
                <Key size={20} />
              </div>
              <div className="vanguard-flex-v vanguard-gap-tiny min-w-0 flex-1">
                <h3 className="text-md font-black uppercase tracking-[0.2em] italic text-white truncate">{t.modal_restore_title}</h3>
                <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest mt-1">PROTOCOL: RESTORE_V2.0</span>
              </div>
            </div>
            <button onClick={onClose} className="p-3 hover:bg-white/5 rounded-xl transition-all hover:rotate-90">
              <X size={24} className="text-text-muted hover:text-white" />
            </button>
          </div>

          <div className="vanguard-flex-v vanguard-gap-medium p-8">
            <div className="p-5 bg-accent-blue/5 border border-accent-blue/10 rounded-2xl vanguard-flex-h vanguard-gap-medium items-start">
              <ShieldCheck className="text-accent-blue shrink-0 mt-1" size={24} />
              <p className="text-[11px] font-medium leading-relaxed text-text-secondary">
                {t.modal_restore_desc}
              </p>
            </div>

            <div className="vanguard-flex-v vanguard-gap-tiny">
              <label className="tactical-label text-[10px]">{t.seed_phrase_label}</label>
              <textarea 
                value={mnemonic}
                onChange={(e) => setMnemonic(e.target.value)}
                placeholder={t.seed_phrase_placeholder}
                className="w-full h-32 p-4 bg-black/40 border border-white/5 rounded-2xl text-sm font-black mono text-white focus:border-accent-blue/40 transition-all outline-none resize-none"
              />
            </div>

            <div className="vanguard-flex-v vanguard-gap-tiny">
              <label className="tactical-label text-[10px]">{t.password_label || "MẬT KHẨU BẢO VỆ VÍ (MÃ PIN)"}</label>
              <div className="relative">
                <Key size={18} className="absolute left-4 top-1/2 -translate-y-1/2 text-accent-blue/50" />
                <input 
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={t.password_placeholder || "Nhập mật khẩu an toàn..."}
                  className="w-full p-4 pl-12 bg-black/40 border border-white/5 rounded-2xl text-sm font-black mono text-white focus:border-accent-blue/40 transition-all outline-none"
                />
              </div>
            </div>

            <div className="vanguard-flex-v vanguard-gap-tiny">
              <label className="tactical-label text-[10px]">{t.passphrase_label || "BIP39 PASSPHRASE (TÙY CHỌN - TỪ THỨ 13)"}</label>
              <div className="relative">
                <ShieldCheck size={18} className="absolute left-4 top-1/2 -translate-y-1/2 text-accent-green/50" />
                <input 
                  type="text"
                  value={passphrase}
                  onChange={(e) => setPassphrase(e.target.value)}
                  placeholder={lang === 'vi' ? "Để trống nếu không dùng passphrase..." : "Leave blank if not using passphrase..."}
                  className="w-full p-4 pl-12 bg-black/40 border border-white/5 rounded-2xl text-sm font-black mono text-white focus:border-accent-green/40 transition-all outline-none"
                />
              </div>
            </div>

            {/* [V2.2 ORIGINAL-SHA256] Address Preview Panel */}
            {previewAddr && (
              <motion.div 
                initial={{ opacity: 0, y: 5 }} animate={{ opacity: 1, y: 0 }}
                className="p-4 bg-white/5 border border-white/10 rounded-2xl vanguard-flex-v vanguard-gap-tiny"
              >
                <div className="vanguard-flex-h justify-between items-center">
                  <span className="text-[10px] font-black uppercase tracking-widest text-accent-blue italic">
                    PREVIEW_ADDRESS_DETECTION:
                  </span>
                </div>
                
                <span className="text-[11px] font-black text-white break-all font-mono bg-black/30 p-2 rounded-lg border border-white/5">
                  0x{previewAddr}
                </span>
                
                <span className="text-[9px] font-bold text-text-muted italic opacity-70">
                  {lang === 'vi' 
                    ? "⚠️ Đây là địa chỉ duy nhất gắn liền với Seed Phrase của bạn." 
                    : "⚠️ This is the unique address linked to your Seed Phrase."}
                </span>
              </motion.div>
            )}


            {error && (
              <div className="p-4 bg-accent-red/10 border border-accent-red/20 rounded-xl vanguard-flex-h vanguard-gap-small items-center text-accent-red">
                <AlertCircle size={16} />
                <span className="text-[10px] font-black uppercase">{error}</span>
              </div>
            )}

            <div className="vanguard-grid-2 vanguard-gap-medium pt-4">
               <button 
                onClick={onClose}
                className="py-4 bg-white/5 hover:bg-white/10 border border-white/10 rounded-2xl text-white text-[10px] font-black uppercase tracking-widest transition-all"
               >
                 {t.cancel_action}
               </button>
               <button 
                onClick={handleRestore}
                disabled={isRestoring}
                className={`py-4 bg-accent-blue/10 border border-accent-blue/30 hover:bg-accent-blue/20 rounded-2xl text-accent-blue text-[10px] font-black uppercase tracking-[0.2em] italic transition-all active:scale-[0.98] ${isRestoring ? 'opacity-50 cursor-not-allowed' : ''}`}
               >
                {isRestoring ? t.restoring : t.confirm_action}
               </button>
            </div>
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};

export default WalletRestoreModal;
