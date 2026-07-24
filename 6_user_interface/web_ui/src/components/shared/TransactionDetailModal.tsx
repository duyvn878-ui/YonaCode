/**
 * @file TransactionDetailModal.tsx
 * @brief Modal Chi tiết Giao dịch — Finality UI V2.1 (6-Step Correct Model)
 * @tính_năng:
 *   - 6-Step Confirmation Visualization: 1 nút "Đã vào khối" + 5 nút "Khối đè lên" = Bất biến
 *   - Block Countdown: Đếm ngược số khối ĐÈ LÊN còn lại đến BẤT BIẾN
 *   - Auto-Lock Animation: Tự động kích hoạt animation khóa xanh khi confirmations >= 6
 *   - Real-time Update: Cập nhật tự động qua SSE (không cần F5)
 *   - Thời gian ước tính: Hiển thị thời gian dự kiến cho mỗi khối còn lại (~60s/khối)
 *   - Lý do 6: Backend tính confirmations = highest - blockHeight + 1 (tính CẢ khối chứa TX)
 *     Nên cần 6 confirmations = 1 (khối chứa) + 5 (khối đè lên) để đạt Bất biến thực sự
 */

import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { 
  X, 
  Shield, 
  Clock, 
  ArrowRightLeft, 
  Lock as LockIcon, 
  CheckCircle, 
  Copy, 
  Receipt
} from 'lucide-react';
import type { Transaction } from '../../api';
import { useLanguage } from '../../LanguageContext';

/**
 * formatBtcZ: Chuyển đổi VNT sang BTC_Z với đúng 8 chữ số thập phân.
 * Tại sao: tx.amount là VNT (1 BTC_Z = 10^8 VNT)
 */
const formatBtcZ = (zatoshi: any): string => {
  const num = Number(zatoshi);
  if (isNaN(num)) return "0.00000000";
  const btcValue = num / 100_000_000;
  return btcValue.toFixed(8);
};

/**
 * formatDate: Định dạng timestamp Unix sang chuỗi ngày tháng đọc được.
 */
const formatDate = (timestamp: number, lang: string): string => {
  if (!timestamp) return "N/A";
  const date = new Date(timestamp * 1000); // Backend dùng giây
  return date.toLocaleString(lang === 'vi' ? 'vi-VN' : 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  });
};

interface TransactionDetailModalProps {
  tx: Transaction | null;
  onClose: () => void;
  currentHeight?: number; // Chiều cao chuỗi hiện tại từ SSE
}

// Tại sao 6 mà không phải 5?
// Backend: confirmations = highest_height - tx.blockHeight + 1 → TÍNH CẢ KHỐI CHỨA TX
// Ví dụ: TX ở khối #3, chuỗi ở khối #8 → confirmations = 8-3+1 = 6
// Thực tế: 1 khối chứa TX (khối #3) + 5 khối đè lên (4,5,6,7,8) = 6 confirmations
const FINALITY_THRESHOLD = 6; // 1 khối chứa TX + 5 khối đè lên = Bất biến
const OVERLAY_BLOCKS_NEEDED = 5; // Số khối ĐÈ LÊN cần thiết (hiển thị cho người dùng)
const AVG_BLOCK_TIME_SEC = 60; // Thời gian trung bình 1 khối (giây)

const TransactionDetailModal: React.FC<TransactionDetailModalProps> = ({ tx, onClose }) => {
  const { lang, t } = useLanguage();
  const [justFinalized, setJustFinalized] = useState(false);
  const [copied, setCopied] = useState(false);

  const txConfirmations = Number(tx?.confirmations) || 0;
  // [V39-AUTHORITATIVE] Dùng status_code (mã số) làm nguồn sự thật duy nhất (Single Source of Truth)
  // Tại sao không dùng error_message: Chuỗi ký tự có thể bị cache cũ, parse sai, hoặc thiếu exclusion.
  // status_code: 0 = Đang chờ, 1 = Thành công, 2+ = Lỗi/Bị từ chối bởi Rust Core.
  const txStatusCode = Number(tx?.status_code) || 0;
  const isRejected = txStatusCode >= 2;
  const isFinalized = txConfirmations >= FINALITY_THRESHOLD && !isRejected;
  // Số khối ĐÈ LÊN đã có = confirmations - 1 (trừ đi khối chứa TX)
  const overlayBlocksDone = Math.max(0, txConfirmations - 1);
  // Số khối đè lên CÒN THIẾU để đạt bất biến
  const blocksRemaining = Math.max(0, OVERLAY_BLOCKS_NEEDED - overlayBlocksDone);
  
  useEffect(() => {
    if (isFinalized && !justFinalized) {
      setJustFinalized(true);
      const timer = setTimeout(() => setJustFinalized(false), 3000);
      return () => clearTimeout(timer);
    }
  }, [isFinalized, justFinalized]);

  // [DEFENSE] Chống crash nếu tx bị null hoặc thiếu thông tin định danh (Đặt SAU hooks)
  if (!tx || !tx.id) return null;

  const formatEstimate = (seconds: number) => {
    const s = Number(seconds) || 0;
    if (s <= 0) return t.just_now;
    const m = Math.floor(s / 60);
    const sec = s % 60;
    if (lang === 'vi') {
      return m > 0 ? `~${m} phút ${sec > 0 ? `${sec}s` : ''}` : `~${sec}s`;
    }
    return m > 0 ? `~${m} min ${sec > 0 ? `${sec}s` : ''}` : `~${sec}s`;
  };

  // [SAFE DEFAULTS]
  const estimatedSeconds = blocksRemaining * AVG_BLOCK_TIME_SEC;
  const txAmount = tx.amount || 0;
  const txFee = tx.fee || 0;
  const txId = tx.id || 'GENESIS_OR_MALFORMED';
  const isOut = tx.direction === 'OUT';

  const handleCopy = () => {
    navigator.clipboard.writeText(txId);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-[5000] flex items-center justify-center p-4 text-white">
        <motion.div
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
          onClick={onClose}
          className="absolute inset-0 bg-black/60 backdrop-blur-md"
        />

        <motion.div
          initial={{ scale: 0.95, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          exit={{ scale: 0.95, opacity: 0 }}
          className={`relative glass-card w-full max-w-md border border-white/10 shadow-2xl bg-zinc-900/90 ${justFinalized ? 'ring-2 ring-accent-green' : ''}`}
        >
          {/* Header */}
          <div className="vanguard-panel-header vanguard-flex-h justify-between items-center bg-white/[0.03]">
            <div className="vanguard-flex-h vanguard-gap-small">
              <div className={`w-10 h-10 rounded-xl flex items-center justify-center shadow-[0_0_15px_rgba(0,136,255,0.2)] ${isFinalized ? 'bg-accent-green/20 text-accent-green shadow-[0_0_15px_rgba(0,242,148,0.2)]' : 'bg-accent-blue/20 text-accent-blue'}`}>
                <ArrowRightLeft size={20} />
              </div>
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <h3 className="text-md font-black uppercase tracking-[0.2em] italic text-white">{t.tx_detail_title}</h3>
                <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest">PROTOCOL: FINALITY_V2.0</span>
              </div>
            </div>
            <button onClick={onClose} className="p-3 hover:bg-white/5 rounded-xl transition-all">
              <X size={24} className="text-text-muted hover:text-white" />
            </button>
          </div>

          <div className="vanguard-flex-v vanguard-gap-small p-5">
            {/* Transaction ID */}
            <div className="vanguard-flex-v vanguard-gap-tiny">
              <span className="tactical-label text-[10px] opacity-40">{t.tx_id}</span>
              <div className="p-3 bg-black/40 border border-white/5 rounded-xl flex items-center justify-between group">
                <span className="text-[10px] font-black mono text-accent-blue truncate mr-4">{String(txId)}</span>
                <button onClick={handleCopy} className="text-[10px] font-black text-white/40 group-hover:text-white transition-opacity uppercase flex items-center gap-2">
                  {copied ? <CheckCircle size={12} className="text-accent-green" /> : <Copy size={12} />}
                  {copied ? t.copied_label : 'Copy'}
                </button>
              </div>
            </div>

            {/* Sender / Receiver */}
            <div className="grid grid-cols-2 gap-3">
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <span className="tactical-label text-[10px] opacity-40">{t.sender_label}</span>
                <p className="text-[10px] font-black mono text-white/70 truncate">{tx.sender || t.mining_reward}</p>
              </div>
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <span className="tactical-label text-[10px] opacity-40">{t.receiver_label}</span>
                <p className="text-[10px] font-black mono text-white/70 truncate">{tx.receiver || t.mining_reward}</p>
              </div>
            </div>

            {/* Amount + Fee */}
            {/* [V37.1] BANK-STYLE RECEIPT BOX */}
            <div className="vanguard-flex-v p-4 bg-black/40 border border-white/10 rounded-2xl relative overflow-hidden vanguard-gap-small shadow-2xl">
               <div className="absolute top-0 left-0 w-1 h-full bg-accent-blue" />
               <div className="vanguard-flex-h justify-between items-center opacity-40">
                  <span className="text-[10px] font-black uppercase tracking-widest">{t.receipt_title}</span>
                  <Receipt size={14} />
               </div>

               {/* Initial Balance */}
               <div className="vanguard-flex-h justify-between items-center border-b border-white/5 pb-2">
                  <span className="text-[10px] font-bold text-text-muted uppercase">{t.prev_balance_label}</span>
                  <span className="text-xs font-medium text-white/80">{formatBtcZ(tx.prev_balance || 0)} <span className="text-[8px] opacity-40">GO</span></span>
               </div>

               {/* Deduction/Addition (Amount + Fee) */}
               <div className="vanguard-flex-v vanguard-gap-tiny py-3 border-b border-white/5">
                  <div className="vanguard-flex-h justify-between items-center mb-1">
                    <span className="text-[10px] font-bold text-text-muted uppercase">{t.amount_label}</span>
                    <span className={`text-sm font-black mono ${isOut ? 'text-accent-red/80' : 'text-accent-green/80'}`}>
                      {isOut ? '-' : '+'}{formatBtcZ(txAmount)} <span className="text-[10px]">GO</span>
                    </span>
                  </div>
                  
                  {isOut && (
                    <div className="vanguard-flex-h justify-between items-center mb-1">
                      <span className="text-[10px] font-bold text-text-muted uppercase">{t.fee_label}</span>
                      <span className="text-sm font-black mono text-accent-red/80">
                        -{formatBtcZ(txFee)} <span className="text-[10px]">GO</span>
                      </span>
                    </div>
                  )}

                  <div className="vanguard-flex-h justify-between items-center pt-2 border-t border-white/5 mt-1">
                    <span className={`text-[10px] font-black uppercase ${isOut ? 'text-accent-red' : 'text-accent-green'}`}>
                      {isOut ? t.total_deducted : t.received || 'Số tiền nhận'}
                    </span>
                    <span className={`text-xl font-black italic tracking-tighter ${isOut ? 'text-accent-red' : 'text-accent-green'}`}>
                      {isOut ? '-' : '+'}{formatBtcZ(isOut ? (Number(txAmount) + Number(txFee)) : Number(txAmount))} <span className="text-xs">GO</span>
                    </span>
                  </div>
               </div>

               {/* Final Balance - THE CENTERPIECE */}
               {txConfirmations > 0 && Number(tx.post_balance) > 0 ? (
                 <div className="vanguard-flex-h justify-between items-center bg-accent-blue/10 p-4 rounded-xl border border-accent-blue/20">
                    <div className="vanguard-flex-v">
                       <span className="text-[9px] font-black text-accent-blue uppercase leading-none mb-2 tracking-tighter">{t.post_balance_label}</span>
                       <span className="text-2xl font-black text-white italic leading-none">
                         {formatBtcZ(tx.post_balance || 0)} 
                         <span className="text-xs opacity-40 ml-1">GO</span>
                       </span>
                    </div>
                    <div className="p-3 bg-accent-blue/20 rounded-full">
                       <Shield size={20} className="text-accent-blue" />
                    </div>
                 </div>
               ) : (
                 <div className="vanguard-flex-h vanguard-gap-small items-center p-5 bg-white/[0.03] rounded-xl border border-white/5 shadow-inner">
                    <div className="w-8 h-8 rounded-full bg-white/5 flex items-center justify-center text-text-muted">
                        <Clock size={16} />
                    </div>
                    <p className="text-[10px] font-bold text-text-muted leading-relaxed italic">
                        {t.tx_waiting_desc}
                    </p>
                 </div>
               )}
            </div>

            {/* LIVE FINALITY STATUS */}
            <div className={`p-4 rounded-2xl border transition-all ${isRejected ? 'bg-accent-red/5 border-accent-red/20' : (isFinalized ? 'bg-accent-green/5 border-accent-green/20' : 'bg-black/40 border-white/[0.05]')}`}>
              <div className="vanguard-flex-h justify-between items-center mb-4">
                <div className="vanguard-flex-h vanguard-gap-medium items-center">
                    {isRejected ? (
                      <div className="text-accent-red">
                        <Shield size={20} />
                      </div>
                    ) : isFinalized ? (
                      <div className={justFinalized ? 'animate-finality-lock' : ''}>
                        <LockIcon className="text-accent-green" size={20} />
                      </div>
                    ) : (
                      <Clock className="text-accent-amber animate-spin-slow" size={20} />
                    )}
                  <div className="vanguard-flex-v vanguard-gap-tiny">
                    <span className={`text-[11px] font-black uppercase italic tracking-widest ${isRejected ? 'text-accent-red' : (isFinalized ? 'text-accent-green' : 'text-accent-amber')}`}>
                      {isRejected ? (t.status_rejected || 'Bị từ chối') : (isFinalized ? t.finality_locked : t.confirming)}
                    </span>
                    <span className="text-[9px] font-bold text-text-muted">
                      {isRejected
                        ? (tx?.error_message || tx?.status || 'Giao dịch không hợp lệ và bị loại bỏ khỏi blockchain.')
                        : isFinalized
                          ? t.data_sealed_forever
                          : `Khối đè lên: ${overlayBlocksDone}/${OVERLAY_BLOCKS_NEEDED} · Còn ${blocksRemaining} khối · ${formatEstimate(estimatedSeconds)}`
                      }
                    </span>
                  </div>
                </div>
                  <div className={`px-3 py-1.5 rounded-lg border ${isRejected ? 'bg-accent-red/10 border-accent-red/20' : (isFinalized ? 'bg-accent-green/10 border-accent-green/20' : 'bg-accent-amber/10 border-accent-amber/20')}`}>
                    <span className={`text-[10px] font-black mono ${isRejected ? 'text-accent-red' : (isFinalized ? 'text-accent-green' : 'text-accent-amber')}`}>
                      {isRejected ? 'LỖI' : `${Math.min(overlayBlocksDone, OVERLAY_BLOCKS_NEEDED)}/${OVERLAY_BLOCKS_NEEDED}`}
                    </span>
                  </div>
              </div>

              {/* 6-Step Progress: 1 bước "Đã vào khối" + 5 bước "Khối đè lên" */}
              {!isRejected && (
                <div className="flex items-center gap-0 mb-4">
                  {Array.from({ length: 6 }).map((_, i) => {
                    // Bước 0: Khối chứa TX (Đã nhận)
                    // Bước 1-5: 5 khối đè lên bất biến  
                    const isConfirmed = i < txConfirmations;
                    const isCurrent = i === txConfirmations - 1 && !isFinalized;
                    const stepClass = isConfirmed ? 'confirmed' : (i === txConfirmations ? 'pending' : 'waiting');
                    const isReceivedStep = i === 0; // Bước đầu tiên = "Đã vào khối"

                    return (
                      <React.Fragment key={i}>
                        <motion.div
                          className={`finality-step ${stepClass} ${isReceivedStep && isConfirmed ? '!bg-accent-blue/20 !border-accent-blue' : ''}`}
                          initial={false}
                          animate={isConfirmed ? { scale: [1, 1.2, 1], borderColor: isReceivedStep ? 'var(--accent-blue)' : 'var(--accent-green)' } : {}}
                        >
                          {isConfirmed ? (
                            <CheckCircle size={14} className={isReceivedStep ? 'text-accent-blue' : 'text-accent-green'} />
                          ) : (
                            <span className="text-[9px] font-black">{isReceivedStep ? '📦' : i}</span>
                          )}
                          {isCurrent && (
                            <motion.div
                              className="absolute inset-0 rounded-full border-2 border-accent-amber"
                              animate={{ scale: [1, 1.4, 1], opacity: [1, 0, 1] }}
                              transition={{ duration: 1.5, repeat: Infinity }}
                            />
                          )}
                        </motion.div>
                        {i < 5 && (
                          <div className={`finality-connector ${i < txConfirmations - 1 ? 'active' : (i < txConfirmations ? 'pending-active' : '')}`} />
                        )}
                      </React.Fragment>
                    );
                  })}
                </div>
              )}
              {/* Chú thích thanh tiến trình */}
              {!isRejected && (
                <div className="flex justify-between text-[8px] font-black mb-2 px-1">
                  <div className="vanguard-flex-v items-start">
                    <span className="text-accent-blue animate-pulse">↑</span>
                    <span className="text-accent-blue uppercase">{t.tx_step_blocked || 'Đã vào khối'}</span>
                  </div>
                  <span className="text-white/20 mt-3">← 5 khối đè lên bất biến →</span>
                  <div className="vanguard-flex-v items-end">
                    <span className="text-accent-green opacity-40">↑</span>
                    <span className="text-accent-green uppercase opacity-40">{t.tx_step_immutable || 'Chốt'}</span>
                  </div>
                </div>
              )}

              {/* Finality Celebration */}
              {isFinalized && justFinalized && (
                <motion.div
                  className="flex items-center justify-center gap-3 p-4 bg-accent-green/10 border border-accent-green/20 rounded-xl mb-4"
                  initial={{ scale: 0.8, opacity: 0 }}
                  animate={{ scale: 1, opacity: 1 }}
                >
                  <Shield className="text-accent-green animate-neon" size={20} />
                  <span className="text-[10px] font-black uppercase italic tracking-widest text-accent-green">
                    {t.immutable_reached}
                  </span>
                </motion.div>
              )}

              {/* Description */}
              <div className="flex justify-between items-center">
                <p className="text-[9px] font-bold text-text-muted leading-relaxed max-w-[85%]">
                  {isFinalized
                    ? t.tx_sealed_desc
                    : tx.height > 0
                      ? t.tx_waiting_desc
                      : t.tx_mempool_desc
                  }
                </p>
                {isFinalized && <LockIcon size={16} className="text-accent-green opacity-40" />}
              </div>

              {/* [V12.9] Flexible Zone Warning Note */}
              {!isFinalized && tx.height > 0 && (
                <div className="mt-4 p-3 bg-accent-amber/5 border border-accent-amber/10 rounded-xl">
                   <p className="text-[8px] font-medium text-accent-amber/70 leading-relaxed italic">
                     {t.flexible_zone_note}
                   </p>
                </div>
              )}
            </div>

            {/* Block & Time Info */}
            <div className={`vanguard-grid-2 vanguard-gap-medium ${tx.height > 0 ? '' : 'flex'}`}>
              <div className="p-4 bg-white/5 border border-white/10 rounded-xl flex-1">
                <span className="tactical-label text-[8px] block mb-1 opacity-40 uppercase font-black">{t.submitted_time}</span>
                <span className="text-[10px] font-black text-accent-blue italic">{formatDate(tx.timestamp, lang)}</span>
              </div>
              
              {tx.height > 0 ? (
                <div className="p-4 bg-white/5 border border-white/10 rounded-xl">
                  <span className="tactical-label text-[8px] block mb-1 opacity-40 uppercase font-black">{t.height}</span>
                  <span className="text-sm font-black text-white italic">#{tx.height}</span>
                </div>
              ) : (
                <div className="p-4 bg-white/5 border border-white/10 rounded-xl flex-1">
                  <span className="tactical-label text-[8px] block mb-1 opacity-40 uppercase font-black">NONCE</span>
                  <span className="text-sm font-black text-white italic">#{tx.nonce ?? 0}</span>
                </div>
              )}
            </div>

            {tx.height > 0 && (
              <div className="vanguard-flex-v vanguard-gap-tiny mt-2">
                 <div className="p-4 bg-white/5 border border-white/10 rounded-xl">
                    <span className="tactical-label text-[8px] block mb-1 opacity-40 uppercase font-black">NONCE</span>
                    <span className="text-sm font-black text-white italic">#{tx.nonce ?? 0}</span>
                  </div>
              </div>
            )}

            {/* Error Message Visualizer - [V12.8] Chỉ hiện khi là LỖI THẬT hoặc CẢNH BÁO REORG */}
            {tx.error_message && 
             !tx.error_message.includes("(Mempool)") &&
             !tx.error_message.includes("WAITING_FOR_BUS") && (
              <div className="p-4 bg-accent-red/10 border border-accent-red/20 rounded-xl mb-4">
                <div className="vanguard-flex-h vanguard-gap-tiny items-center mb-2">
                  <Shield size={14} className="text-accent-red" />
                  <span className="text-[10px] font-black uppercase text-accent-red italic tracking-widest">{t.tx_error_title || 'LÝ DO BỊ TỪ CHỐI'}</span>
                </div>
                <p className="text-[10px] font-medium text-white/80 leading-relaxed font-mono">
                  {tx.error_message}
                </p>
              </div>
            )}

            {/* Action Buttons */}
            <div className="vanguard-flex-h justify-end pt-4">
              <button
                onClick={onClose}
                className="px-8 py-3 bg-accent-blue/10 border border-accent-blue/30 hover:bg-accent-blue/20 rounded-2xl text-accent-blue text-[11px] font-black uppercase tracking-widest italic transition-all active:scale-[0.98]"
              >
                {t.close_btn}
              </button>
            </div>
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};

export default TransactionDetailModal;

