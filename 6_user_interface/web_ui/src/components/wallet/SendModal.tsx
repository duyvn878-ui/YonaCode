/**
 * @file SendModal.tsx
 * @brief Modal Gửi Tiền — Anti-Spam Fee V2.0
 * @tính_năng:
 *   - Phí minh bạch: Hiển thị phí cơ bản 5 VNT cho giao dịch bình thường
 *   - Công thức lũy tiến: Hiển thị rõ "Phí = 5 × 1.05^n" khi nano-dust (<10 đơn vị)
 *   - Cảnh báo nâng cao: Animation shake + biểu tượng lớn cho giao dịch spam
 *   - Debounce 300ms: Tính phí realtime khi nhập số tiền
 * 
 */

import React, { useState, useEffect, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, Send, ShieldCheck, Key } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import api from '../../api';

interface SendModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: (msg: string, txBill?: any) => void;
  sender: string;
  balance: number;
}

// [V1.3 - PHỤ LỤC H] Cấu trúc Phí 3 Tầng
const FEE_TIERS = {
  STANDARD: 250,
  PRIORITY: 500,
  VIP: 1000
};

const SendModal: React.FC<SendModalProps> = ({ isOpen, onClose, onSuccess, sender, balance }) => {
  const { t, lang } = useLanguage();
  const [receiver, setReceiver] = useState('');
  const [amount, setAmount] = useState('');
  const [password, setPassword] = useState('');
  const [fee, setFee] = useState<number>(FEE_TIERS.STANDARD);
  const [selectedTier, setSelectedTier] = useState<number>(FEE_TIERS.STANDARD);
  const [manuallySelected, setManuallySelected] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // [V1.3.1] Cập nhật phí và đề xuất thông minh từ Backend
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    
    debounceRef.current = setTimeout(async () => {
        const val = parseFloat(amount);
        if (isNaN(val) || val <= 0) return;

        try {
            const { fee: calculatedFee, recommended } = await api.calculateFee(val, manuallySelected ? selectedTier : 0);
            setFee(calculatedFee);
            
            // Tự động chọn mức phí khuyến nghị nếu người dùng chưa can thiệp thủ công
            if (!manuallySelected) {
                setSelectedTier(recommended);
            }
        } catch (e) {
            console.error("Lỗi tính phí:", e);
        }
    }, 300);

    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [amount, selectedTier, manuallySelected]);

  const handleSend = async () => {
    const val = parseFloat(amount);
    if (!receiver || isNaN(val) || val <= 0) {
      setError(t.send_err_invalid);
      return;
    }
    // [VANGUARD-UX-LIMIT] Chốt chặn số lượng tiền tối đa có thể biểu diễn bằng uint64 (184,467,440,737 GO)
    if (val > 184467440737) {
      setError(lang === 'vi' ? "Số tiền vượt quá giới hạn tối đa hệ thống (184,467,440,737 GO)." : "Amount exceeds the system maximum limit (184,467,440,737 GO).");
      return;
    }
    if (!password) {
      setError(t.password_required || "Password cannot be empty");
      return;
    }

    setIsSending(true);
    setError(null);
    try {
      if (!sender) {
        throw new Error(t.send_err_no_wallet);
      }
      const txid = await api.sendTransaction(sender, receiver, val, password, selectedTier);
      
      const txBill = {
          id: txid,
          sender: sender,
          receiver: receiver,
          amount: Math.floor(val * 100_000_000),
          fee: fee,
          timestamp: Math.floor(Date.now() / 1000),
          height: 0,
          confirmations: 0,
          status: "Đang xử lý (Local Bill)",
          status_code: 0,
          direction: "OUT",
          is_local_bill: true
      };
      
      onSuccess(t.send_success.replace('{0}', String(val)).replace('{1}', String(fee)), txBill);
      setReceiver('');
      setAmount('');
      setPassword('');
      onClose();
    } catch (e: any) {
      const msg = (e.message || '').toLowerCase();
      if (msg.includes('401') || msg.includes('unauthorized') || msg.includes('chưa đăng nhập') || msg.includes('auth_required') || msg.includes('không thể ký') || msg.includes('err_watch_only')) {
        setError(t.send_err_auth);
      } else if (msg.includes('amount overflow')) {
        setError(lang === 'vi' ? "Số tiền vượt quá giới hạn tối đa hệ thống." : "Amount exceeds the system maximum limit.");
      } else {
        setError(e.message || t.send_err_general);
      }
    } finally {
      setIsSending(false);
    }
  };

  if (!isOpen) return null;


  // V1.19: Đã loại bỏ logic isNano

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
          className="relative glass-card w-full max-w-lg overflow-hidden border-white/[0.08] shadow-[0_0_50px_rgba(0,0,0,0.5)] flex flex-col"
        >
          <div className="vanguard-panel-header vanguard-flex-h justify-between items-center bg-black/80 backdrop-blur-md sticky top-0 z-[50] border-b border-white/5 p-6">
             <div className="vanguard-flex-h vanguard-gap-small">
                <div className="w-10 h-10 rounded-xl bg-accent-blue/20 flex items-center justify-center text-accent-blue shadow-[0_0_15px_rgba(0,136,255,0.2)]">
                  <Send size={20} />
                </div>
                <div className="vanguard-flex-v vanguard-gap-tiny">
                  <h3 className="text-md font-black uppercase tracking-[0.2em] italic text-white">{t.modal_send_title_new || "KÍCH HOẠT ĐIỀU CHUYỂN"}</h3>
                  <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest">PROTOCOL: DISPATCH_V2.5</span>
                </div>
             </div>
             <button onClick={onClose} className="p-3 hover:bg-white/10 rounded-xl transition-all group/close">
                <X size={24} className="text-text-muted group-hover/close:text-white group-hover/close:rotate-90 transition-all duration-300" />
             </button>
          </div>

          {/* Scrollable Body */}
          <div className="vanguard-flex-v vanguard-gap-medium p-8 overflow-y-auto max-h-[calc(90vh-160px)]">
            <div className="p-4 bg-accent-blue/5 border border-accent-blue/10 rounded-2xl vanguard-flex-h vanguard-gap-medium items-center">
              <ShieldCheck className="text-accent-blue shrink-0" size={20} />
              <p className="text-[10px] font-medium leading-relaxed text-text-secondary">
                {String((t as any).security_alert_send || "Giao dịch được mã hóa và xác thực trên Ledger. Vui lòng kiểm tra kỹ địa chỉ nhận.")}
              </p>
            </div>

            {/* Địa chỉ nhận */}
            <div className="vanguard-flex-v vanguard-gap-tiny">
              <label className="tactical-label text-[10px]">{t.receive}</label>
              <div className="relative">
                <input
                  type="text"
                  value={receiver}
                  onChange={(e) => setReceiver(e.target.value)}
                  placeholder="0x..."
                  className="w-full bg-black/40 border border-white/10 rounded-xl py-4 px-4 text-sm text-white placeholder:text-gray-600 focus:border-accent-blue/40 transition-all font-mono outline-none"
                />
              </div>
            </div>

            {/* Số lượng */}
            <div className="vanguard-flex-v vanguard-gap-tiny">
              <div className="vanguard-flex-h justify-between items-end mb-1">
                <label className="tactical-label text-[10px]">{t.amount_label_input}</label>
                <button 
                  onClick={() => setAmount(((balance - fee) / 100_000_000).toString())}
                  className="text-[9px] font-black text-accent-blue uppercase tracking-widest hover:underline"
                >
                  {t.max_button} ({(balance / 100_000_000).toLocaleString()} GO)
                </button>
              </div>
              <div className="relative">
                <input
                  type="number"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="0.00"
                  className="w-full bg-black/40 border border-white/10 rounded-xl py-4 px-4 text-white focus:border-accent-blue/40 transition-all font-black text-xl outline-none italic"
                />
                <div className="absolute right-4 top-1/2 -translate-y-1/2 text-[10px] font-black text-white/20">GO</div>
              </div>
            </div>

            {/* Fee Tiers */}
            <div className="grid grid-cols-3 gap-2">
              {[
                { label: "Standard", val: FEE_TIERS.STANDARD, color: "accent-blue" },
                { label: "Priority", val: FEE_TIERS.PRIORITY, color: "accent-amber" },
                { label: "VIP", val: FEE_TIERS.VIP, color: "accent-red" }
              ].map((tier) => (
                <button
                  key={tier.label}
                  onClick={() => { setSelectedTier(tier.val); setManuallySelected(true); }}
                  className={`p-2 rounded-lg border text-[8px] font-black uppercase transition-all ${
                    selectedTier === tier.val ? `bg-${tier.color}/20 border-${tier.color}/50 text-white` : 'bg-black/20 border-white/5 text-text-muted'
                  }`}
                >
                  {tier.label}
                </button>
              ))}
            </div>

            {/* MẬT KHẨU */}
            <div className="vanguard-flex-v vanguard-gap-tiny">
              <label className="tactical-label text-[10px]">{t.password_label || "MẬT KHẨU BẢO VỆ VÍ (MÃ PIN)"}</label>
              <div className="relative">
                <Key size={16} className="absolute left-4 top-1/2 -translate-y-1/2 text-accent-amber/50" />
                <input 
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="****"
                  className="w-full p-4 pl-12 bg-black/40 border border-white/5 rounded-xl text-sm font-black mono text-white outline-none"
                />
              </div>
            </div>

            {/* Summary Block */}
            <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5 vanguard-flex-v vanguard-gap-tiny">
               <div className="vanguard-flex-h justify-between text-[10px]">
                  <span className="text-text-muted">PHÍ MẠNG:</span>
                  <span className="text-accent-green font-black">{fee} VNT</span>
               </div>
               <div className="vanguard-flex-h justify-between text-[11px] font-black">
                  <span className="text-white italic uppercase">{t.summary_total}:</span>
                  <span className="text-accent-blue italic">{((parseFloat(amount) || 0) + (fee / 100_000_000)).toLocaleString()} GO</span>
               </div>
            </div>

            {error && (
              <div className="p-3 bg-accent-red/10 border border-accent-red/20 rounded-xl text-accent-red text-[9px] font-black uppercase">
                {error}
              </div>
            )}
          </div>

          {/* Fixed Footer */}
          <div className="p-6 bg-black/60 border-t border-white/5 grid grid-cols-2 gap-4">
             <button 
              onClick={onClose}
              className="py-4 bg-white/5 hover:bg-white/10 rounded-xl text-text-muted text-[10px] font-black uppercase tracking-widest transition-all"
             >
               {t.cancel_action}
             </button>
             <button 
              onClick={handleSend}
              disabled={isSending}
              className={`py-4 bg-accent-blue hover:bg-accent-blue-light rounded-xl text-white text-[10px] font-black uppercase tracking-[0.1em] italic transition-all shadow-[0_0_20px_rgba(0,136,255,0.2)] ${isSending ? 'opacity-50' : ''}`}
             >
               {isSending ? t.processing : t.confirm_action}
             </button>
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};

export default SendModal;
