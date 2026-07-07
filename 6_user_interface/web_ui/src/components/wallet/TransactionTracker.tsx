/**
 * @file TransactionTracker.tsx
 * @brief Bảng Theo dõi Giao dịch Phụ lục I - V5.6 (6-Step Finality Model)
 * @tính_năng:
 *   - Layout List HUD chuẩn Industrial (Không dùng Table)
 *   - Trạng thái xác minh trực quan: Đã vào khối, Đang gia cố (5 khối đè lên), Đã bất biến
 *   - [V5.6] Sửa logic finality: cần 6 confirmations (1 khối chứa + 5 khối đè lên)
 */

import React, { useMemo } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { ArrowUpRight, ArrowDownLeft, Pickaxe, Info, Clock, CheckCircle2, Shield } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import { formatBtcZ, formatTime } from '../../utils';
import type { Transaction } from '../../api';

interface TransactionTrackerProps {
  transactions: Transaction[];
  address: string;
  onTransactionClick: (tx: Transaction) => void;
  maxHeight?: string;
  filterMode?: 'all' | 'in' | 'out';
}

const shortenAddress = (addr: string) => {
  if (!addr) return "GENESIS / COINBASE";
  const clean = addr.trim().replace(/^0x/, '');
  if (clean === "0".repeat(64) || clean === "") {
    return "GENESIS / COINBASE";
  }
  return `0x${clean.slice(0, 8)}...${clean.slice(-8)}`;
};

const TransactionTracker: React.FC<TransactionTrackerProps> = ({ 
  transactions, 
  address, 
  onTransactionClick,
  maxHeight = "1200px",
  filterMode = 'all'
}) => {
  const { t } = useLanguage();

  const normalize = (addr: string) => {
    if (!addr) return "";
    return addr.toLowerCase().trim().replace(/^0x/, '');
  };
  
  const activeAddr = useMemo(() => normalize(address), [address]);
  
  const isSameAddress = (a: string, b: string) => {
    if (!activeAddr) return false;
    const normA = normalize(a);
    const normB = normalize(b);
    if (!normA || !normB) return false;
    return normA === normB;
  };

  const filteredTransactions = useMemo(() => {
    if (!activeAddr || !Array.isArray(transactions)) return [];
    return transactions.filter(tx => {
      const isSent = isSameAddress(tx.sender, activeAddr);
      const isReceived = isSameAddress(tx.receiver, activeAddr);
      const senderNorm = normalize(tx.sender);
      const isCoinbase = (senderNorm === "" || senderNorm === "0".repeat(64));
      
      if (filterMode === 'in') return isReceived || (isCoinbase && isReceived);
      if (filterMode === 'out') return isSent;
      return true;
    });
  }, [transactions, activeAddr, filterMode]);

  if (!filteredTransactions || filteredTransactions.length === 0) {
    return (
      <div className="vanguard-flex-v items-center justify-center py-20 border border-dashed border-white/5 rounded-3xl bg-black/20 gap-4">
        <div className="w-16 h-16 rounded-full bg-white/[0.02] flex items-center justify-center opacity-10">
            <Pickaxe size={32} />
        </div>
        <span className="text-[10px] text-text-muted italic lowercase">{t.no_transactions}</span>
      </div>
    );
  }

  return (
    <div className="vanguard-flex-v vanguard-gap-medium h-full">
      {/* 🚀 INDUSTRIAL LIST HUD - V6.8 "Ảnh 1" Style */}
      <div 
        className="vanguard-flex-v vanguard-gap-small overflow-y-auto pr-2 custom-scrollbar"
        style={{ maxHeight }}
      >
        <AnimatePresence>
          {filteredTransactions.map((tx, index) => {
            const isSent = isSameAddress(tx.sender, activeAddr);
            const isReceived = isSameAddress(tx.receiver, activeAddr);
            const amount = Number(tx.amount) || 0;
            const confirms = Number(tx.confirmations) || 0;
            const senderNorm = normalize(tx.sender);
            const isCoinbase = (senderNorm === "" || senderNorm === "0".repeat(64));
            
            // Xác định hiển thị dựa trên filterMode (Context-Aware UI)
            const isSelf = !!tx.is_self;
            let displayAsSent = isSent;
            
            if (isSelf) {
                // Nếu là self-send, hiển thị theo tab đang đứng
                displayAsSent = (filterMode === 'out');
            } else {
                if (filterMode === 'in' && isReceived) displayAsSent = false;
                if (filterMode === 'out' && isSent) displayAsSent = true;
            }
            
            // Trạng thái HUD
            // [BUS-STATUS-FIX] Loại trừ trạng thái WAITING_FOR_BUS khỏi logic "bị từ chối"
            // Tại sao: Giao dịch đang chờ xe buýt xử lý (tối đa 2 giây) không phải là bị từ chối,
            // nhưng error_message tạm thời không chứa "(Mempool)" nên bị hiện nhầm thành đỏ.
            const isRejected = !!(tx.error_message && !tx.error_message.includes("(Mempool)") && !tx.error_message.includes("WAITING_FOR_BUS"));
            const isMempool = confirms === 0 && !isRejected;
            const isFinalized = confirms >= 6 && !isRejected; 
            const overlayBlocksDone = Math.max(0, confirms - 1);
            const progressWidth = Math.min((confirms / 6) * 100, 100);

            return (
              <motion.div 
                key={tx.txid || index}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.03 }}
                onClick={() => onTransactionClick(tx)}
                className={`group p-5 rounded-[1.5rem] border transition-all cursor-pointer relative overflow-hidden flex-shrink-0
                  ${isRejected ? 'bg-accent-red/5 border-accent-red/20 hover:bg-accent-red/10' :
                    (isCoinbase ? 'bg-accent-amber/5 border-accent-amber/20 hover:bg-accent-amber/10' : 
                      (displayAsSent ? 'bg-accent-red/5 border-accent-red/10 hover:bg-accent-red/10' : 
                                      'bg-accent-green/5 border-accent-green/10 hover:bg-accent-green/10'))}`}
              >
                {/* Accent direction line */}
                <div className={`absolute left-0 top-0 bottom-0 w-1.5 ${isRejected ? 'bg-accent-red shadow-[0_0_10px_rgba(255,59,48,0.5)]' : (displayAsSent ? 'bg-accent-red shadow-[0_0_10px_rgba(255,59,48,0.5)]' : (isCoinbase ? 'bg-accent-amber shadow-[0_0_10px_rgba(245,158,11,0.5)]' : 'bg-accent-green shadow-[0_0_10px_rgba(0,242,148,0.5)]'))}`} />
                
                <div className="vanguard-flex-h justify-between items-center relative z-10 gap-3">
                  <div className="vanguard-flex-h vanguard-gap-medium items-center min-w-0 flex-1">
                    {/* Icon HUD */}
                    <div className={`w-10 h-10 rounded-lg flex items-center justify-center border transition-all duration-500 flex-shrink-0
                      ${isRejected ? 'bg-accent-red/10 border-accent-red/20 text-accent-red shadow-[0_0_10px_rgba(255,59,48,0.1)]' :
                        (displayAsSent ? 'bg-accent-red/10 border-accent-red/20 text-accent-red shadow-[0_0_10px_rgba(255,59,48,0.1)]' : 
                          (isCoinbase ? 'bg-accent-amber/10 border-accent-amber/20 text-accent-amber shadow-[0_0_10px_rgba(245,158,11,0.1)]' : 
                                       'bg-accent-green/10 border-accent-green/20 text-accent-green shadow-[0_0_10px_rgba(0,242,148,0.1)]'))}`}>
                      {isRejected ? <Shield size={18} /> : (isCoinbase ? <Pickaxe size={18} /> : 
                       displayAsSent ? <ArrowUpRight size={18} /> : <ArrowDownLeft size={18} />)}
                    </div>

                    <div className="vanguard-flex-v gap-0.5 min-w-0">
                      <div className="vanguard-flex-h items-center gap-2">
                        <span className={`text-[11px] font-black uppercase tracking-tight ${isRejected ? 'text-accent-red/80' : (displayAsSent ? 'text-accent-red/80' : 'text-accent-green/80')}`}>
                          {isRejected ? (t.status_rejected || 'Bị từ chối') : (isCoinbase ? t.mining_reward : (displayAsSent ? t.filter_out : t.filter_in))}
                        </span>
                        <span className="text-[10px] font-bold text-white/40 mono">
                          {formatTime(tx.timestamp)}
                        </span>
                      </div>
                      <span className="text-[12px] font-bold text-white/80 truncate mono">
                        {displayAsSent ? `TO: ${shortenAddress(tx.receiver)}` : `FROM: ${shortenAddress(tx.sender)}`}
                      </span>
                    </div>
                  </div>

                  <div className="vanguard-flex-v items-end gap-1 flex-shrink-0">
                     <span className={`text-base font-black tracking-tighter whitespace-nowrap ${isRejected ? 'text-accent-red' : (displayAsSent ? 'text-accent-red' : (isCoinbase ? 'text-accent-amber' : 'text-accent-green'))}`}>
                        {displayAsSent ? '-' : '+'}{formatBtcZ(amount)} <span className="text-[10px] opacity-60">GO</span>
                     </span>
                     
                     {/* Status Badge HUD */}
                     <div className={`px-2 py-0.5 rounded-full flex items-center gap-1.5 border whitespace-nowrap
                        ${isRejected ? 'bg-accent-red/20 border-accent-red/30 text-accent-red shadow-[0_0_10px_rgba(255,59,48,0.1)]' :
                          (isFinalized ? 'bg-accent-blue/20 border-accent-blue/30 text-accent-blue shadow-[0_0_10px_rgba(0,136,255,0.1)]' : 
                            (isMempool ? 'bg-accent-amber/20 border-accent-amber/30 text-accent-amber animate-pulse' : 
                                         'bg-accent-green/20 border-accent-green/30 text-accent-green shadow-[0_0_10px_rgba(34,197,94,0.1)]'))}`}>
                        {isRejected ? <Shield size={10} className="text-accent-red" /> : (isFinalized ? <CheckCircle2 size={10} /> : <Clock size={10} />)}
                        <span className="text-[9px] font-black uppercase tracking-wider">
                           {isRejected ? (t.status_rejected || 'Bị từ chối') : (isFinalized ? t.status_immutable : (isMempool ? t.status_mempool : `${overlayBlocksDone}/5 CONF`))}
                        </span>
                     </div>
                  </div>
                </div>

                {/* Micro Progress Bar */}
                {!isFinalized && !isRejected && (
                  <div className="mt-3 h-1 w-full bg-white/5 rounded-full overflow-hidden">
                    <motion.div 
                      initial={{ width: 0 }} 
                      animate={{ width: `${progressWidth}%` }}
                      className={`h-full ${isMempool ? 'bg-accent-amber/60 shadow-[0_0_5px_rgba(245,158,11,0.5)]' : 'bg-accent-green/60 shadow-[0_0_5px_rgba(34,197,94,0.5)]'}`}
                    />
                  </div>
                )}
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>

      {/* 🚀 APPENDIX DESCRIPTION (Mô tả chi tiết các giai đoạn) */}
      <div className="p-8 bg-white/[0.02] border border-white/5 rounded-2xl vanguard-flex-v vanguard-gap-small mt-4">
          <div className="vanguard-flex-h vanguard-gap-small items-center mb-2 border-b border-white/10 pb-2">
            <Info size={14} className="text-accent-blue" />
            <h4 className="text-[11px] font-black uppercase text-white tracking-[0.1em]">{t.tx_detail_title}: Phân tích Giai đoạn Năng lượng</h4>
          </div>
          
          <div className="vanguard-grid-2 vanguard-gap-medium">
            <div className="vanguard-flex-v vanguard-gap-tiny">
               <span className="text-[10px] font-black text-accent-blue uppercase tracking-widest">• {t.tx_track_mempool}</span>
               <p className="text-[8px] font-bold text-white/40 leading-relaxed italic">{t.tx_track_mempool_desc}</p>
            </div>
            <div className="vanguard-flex-v vanguard-gap-tiny">
               <span className="text-[10px] font-black text-accent-blue uppercase tracking-widest">• {t.tx_track_blocked}</span>
               <p className="text-[8px] font-bold text-white/40 leading-relaxed italic">{t.tx_track_blocked_desc}</p>
            </div>
            <div className="vanguard-flex-v vanguard-gap-tiny">
               <span className="text-[10px] font-black text-accent-amber uppercase tracking-widest">• {t.tx_track_reinforcing}</span>
               <p className="text-[8px] font-bold text-white/40 leading-relaxed italic">{t.tx_track_reinforcing_desc}</p>
            </div>
            <div className="vanguard-flex-v vanguard-gap-tiny">
               <span className="text-[10px] font-black text-accent-green uppercase tracking-widest">• {t.tx_track_immutable}</span>
               <p className="text-[8px] font-bold text-white/40 leading-relaxed italic">{t.tx_track_immutable_desc}</p>
            </div>
          </div>
      </div>
    </div>
  );
};

export default TransactionTracker;
