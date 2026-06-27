/**
 * @file ReceiveModal.tsx
 * @brief Modal Nhận Tiền — Phụ Lục D §3
 * @tính_năng:
 *   - Hiển thị địa chỉ Ví công khai (text đầy đủ)
 *   - QR Code lớn (qrcode.react)
 *   - Nút Copy (clipboard API) với animation xác nhận
 *   - Animation entrance/exit mượt mà
 */

import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, Copy, Check, QrCode } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';

interface ReceiveModalProps {
  isOpen: boolean;
  onClose: () => void;
  address: string;
}

import { useLanguage } from '../../LanguageContext';

const ReceiveModal: React.FC<ReceiveModalProps> = ({ isOpen, onClose, address }) => {
  const [copied, setCopied] = useState(false);
  const { t } = useLanguage();

  const handleCopy = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 3000);
  };

  if (!isOpen) return null;

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-[4000] flex items-center justify-center p-6 text-white">
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="absolute inset-0 bg-black/80 backdrop-blur-xl"
          onClick={onClose}
        />
        <motion.div
          initial={{ opacity: 0, scale: 0.9, y: 20 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.9, y: 20 }}
          className="relative glass-card w-full max-w-lg overflow-hidden border-white/[0.08] shadow-[0_0_50px_rgba(0,0,0,0.5)]"
          onClick={e => e.stopPropagation()}
        >
          {/* Header */}
          <div className="vanguard-panel-header vanguard-flex-h justify-between items-center bg-white/[0.03]">
             <div className="vanguard-flex-h vanguard-gap-small">
                <div className="w-10 h-10 rounded-xl bg-accent-green/20 flex items-center justify-center text-accent-green shadow-[0_0_15px_rgba(0,242,148,0.2)]">
                  <QrCode size={20} />
                </div>
                <div className="vanguard-flex-v vanguard-gap-tiny">
                  <h3 className="text-md font-black uppercase tracking-[0.2em] italic text-white">{t.modal_receive_title}</h3>
                  <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest">PROTOCOL: RECEPTION_V2.0</span>
                </div>
             </div>
             <button onClick={onClose} className="p-3 hover:bg-white/5 rounded-xl transition-all">
                <X size={24} className="text-text-muted hover:text-white" />
             </button>
          </div>

          <div className="vanguard-flex-v vanguard-gap-medium p-8">
            {/* QR Code */}
            <div className="flex justify-center p-6 bg-white/[0.02] border border-white/5 rounded-3xl relative group">
              <div className="absolute inset-0 bg-accent-green/5 blur-3xl rounded-full opacity-0 group-hover:opacity-100 transition-opacity" />
              <div className="p-4 bg-white rounded-3xl relative z-10">
                <QRCodeSVG
                  value={address || 'btc_genz_no_address'}
                  size={180}
                  bgColor="#ffffff"
                  fgColor="#0a0b0f"
                  level="H"
                  includeMargin={false}
                />
              </div>
            </div>

            {/* Address Display */}
            <div className="vanguard-flex-v vanguard-gap-tiny">
              <span className="tactical-label text-[10px] text-center opacity-40">{t.entity_address}</span>
              <div className="p-4 bg-black/40 border border-white/10 rounded-xl group relative overflow-hidden">
                <div className="absolute inset-0 bg-accent-blue/5 opacity-0 group-hover:opacity-100 transition-opacity" />
                <p className="text-[10px] mono text-accent-blue text-center break-all leading-relaxed font-black transition-colors group-hover:text-white">
                  {address || t.wallet_not_activated}
                </p>
              </div>
            </div>

            {/* Action Buttons */}
            <div className="vanguard-grid-2 vanguard-gap-medium pt-4">
               <button 
                onClick={onClose}
                className="py-5 bg-white/5 hover:bg-white/10 border border-white/10 rounded-2xl text-white text-[11px] font-black uppercase tracking-widest transition-all"
               >
                 {t.cancel_action}
               </button>
               <button 
                onClick={handleCopy}
                disabled={!address}
                className={`py-5 flex items-center justify-center gap-3 rounded-2xl border text-[11px] font-black uppercase tracking-widest italic transition-all ${
                  copied
                    ? 'bg-accent-green/20 border-accent-green/30 text-accent-green'
                    : 'bg-accent-blue/10 border-accent-blue/30 text-accent-blue hover:bg-accent-blue/20'
                } disabled:opacity-30 disabled:cursor-not-allowed grow`}
               >
                 {copied ? <Check size={16} /> : <Copy size={16} />}
                 {copied ? t.copied_text : t.copy_address}
               </button>
            </div>

            {/* ⏳ WAITING STATUS INDICATOR */}
            <div className="p-4 bg-accent-blue/5 border border-accent-blue/10 rounded-2xl vanguard-flex-h vanguard-gap-medium items-center mb-2">
               <div className="relative w-10 h-10 flex items-center justify-center">
                  <div className="absolute inset-0 bg-accent-blue/20 rounded-full animate-ping" />
                  <div className="relative z-10 w-6 h-6 rounded-full border-2 border-accent-blue border-t-transparent animate-spin" />
               </div>
               <div className="vanguard-flex-v vanguard-gap-tiny">
                  <span className="text-[10px] font-black text-white uppercase tracking-wider">{t.receive_waiting.split('...')[0]}...</span>
                  <p className="text-[9px] font-bold text-white/40 italic leading-tight">
                    {t.receive_waiting.split('...')[1]}
                  </p>
               </div>
            </div>

            {/* Security Note */}
            <div className="p-4 bg-accent-amber/5 border border-accent-amber/10 rounded-xl vanguard-flex-h vanguard-gap-small items-center">
              <div className="w-1.5 h-1.5 rounded-full bg-accent-amber animate-pulse" />
              <p className="text-[9px] font-bold text-accent-amber italic grow">
                {t.send_only_warning}
              </p>
            </div>
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};

export default ReceiveModal;

