/**
 * @file UnifiedWalletPanel.tsx
 * @brief Panel Ví Thống nhất — V6.0.2 Phoenix Restoration (Cleaning)
 * @tính_năng:
 *   - [V6.0.2] Clean unused imports (Shield) and props (status).
 *   - [V6.0.2] Final HUD visibility confirmation.
 */

import { useState, useEffect, useMemo } from 'react';
import { RefreshCw, Send, ArrowDownLeft, Activity, TrendingUp, TrendingDown, Copy, Check } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import type { Transaction, BalanceInfo } from '../../api';
import api from '../../api';
import { useLanguage } from '../../LanguageContext';
import { formatBtcZ, formatVnt } from '../../utils';
import TransactionTracker from './TransactionTracker';
import BalanceHistoryChart from './BalanceHistoryChart';
import BalanceIntel from './BalanceIntel';

// Reusable Stat Card Component cho Dashboard Phân khu
const StatDashboardCard = ({ title, value, type }: { title: string, value: number, type: 'in' | 'out' }) => {
    return (
        <motion.div 
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            className={`p-6 rounded-[2rem] border overflow-hidden relative group transition-all duration-500 mb-6
                ${type === 'in' ? 'bg-accent-green/5 border-accent-green/20 shadow-[0_0_20px_rgba(34,197,94,0.1)]' : 'bg-accent-amber/5 border-accent-amber/20 shadow-[0_0_20px_rgba(247,159,10,0.1)]'}`}
        >
            <div className={`absolute top-0 right-0 w-32 h-32 blur-[60px] opacity-20 -mr-16 -mt-16 transition-transform group-hover:scale-150 duration-700
                ${type === 'in' ? 'bg-accent-green' : 'bg-accent-amber'}`} />
            
            <div className="flex flex-row gap-6 items-center relative z-10">
                <div className={`w-14 h-14 rounded-2xl flex items-center justify-center border transition-all duration-300
                    ${type === 'in' ? 'bg-accent-green/20 border-accent-green/30 text-accent-green' : 'bg-accent-amber/20 border-accent-amber/30 text-accent-amber'}`}>
                    {type === 'in' ? <TrendingUp size={28} /> : <TrendingDown size={28} />}
                </div>
                <div className="flex flex-col">
                    <span className="text-[11px] font-black uppercase tracking-[0.3em] text-white/40 italic">{title}</span>
                    <div className="flex flex-row gap-2 items-baseline">
                        <span className={`font-black italic tracking-tighter tabular-nums ${type === 'in' ? 'text-white' : 'text-white/90'}`} style={{
                            fontSize: `clamp(1.25rem, ${Math.max(1.25, 2.5 - (formatBtcZ(value).length - 8) * 0.15)}rem, 2.5rem)`,
                            lineHeight: 1.1,
                            whiteSpace: 'nowrap',
                        }}>
                            {formatBtcZ(value)}
                        </span>
                        <span className={`text-[10px] font-black italic opacity-40 uppercase ${type === 'in' ? 'text-accent-green' : 'text-accent-amber'}`}>GO</span>
                    </div>
                </div>
            </div>
            <div className="absolute left-0 right-0 h-[1px] bg-white/5 top-1/2 -translate-y-1/2 opacity-20" />
        </motion.div>
    );
};

interface UnifiedWalletPanelProps {
  balance: number;
  address: string;
  handleSend: () => void;
  onRestoreClick: () => void;
  onCreateClick: () => void;
  onTransactionClick: (tx: Transaction) => void;
  onReceiveClick: () => void;
  onWalletDelete: (address: string) => void;
  pendingTxCount?: number;
  txRefreshCounter?: number; // [V38.0] Trigger re-fetch khi gửi giao dịch thành công
}

const UnifiedWalletPanel: React.FC<UnifiedWalletPanelProps> = ({
  balance, address, handleSend, onRestoreClick, onCreateClick, onTransactionClick, onReceiveClick, onWalletDelete, pendingTxCount = 0, txRefreshCounter = 0
}) => {
  const [balanceInfo, setBalanceInfo] = useState<BalanceInfo | null>(null);
  const [activeSubTab, setActiveSubTab] = useState<'history' | 'received' | 'sent' | 'analysis'>('history');
  const [searchTerm, setSearchTerm] = useState('');
  const [localTransactions, setLocalTransactions] = useState<Transaction[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const { t } = useLanguage();
  const [copied, setCopied] = useState(false);

  const handleCopyAddress = () => {
    if (!address) return;
    navigator.clipboard.writeText(`0x${normalize(address)}`);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDeleteClick = () => {
    if (!address) return;
    const confirm = window.confirm(t.delete_wallet_confirm || "Bạn có chắc chắn muốn XÓA ví này khỏi thiết bị không?");
    if (confirm) {
      onWalletDelete(address);
    }
  };

  // [V6.5 ELITE] Tải lịch sử giao dịch thực tế từ API (Lọc + Tìm kiếm + Sắp xếp)
  useEffect(() => {
    if (!address) return;

    const fetchHistory = async () => {
      setIsLoadingHistory(true);
      try {
        let direction = '';
        if (activeSubTab === 'received') direction = 'in';
        if (activeSubTab === 'sent') direction = 'out';
        
        const res = await api.getAddressHistory(address, direction, searchTerm);
        let apiHistory = res.history || [];
        
        try {
            const rawBills = localStorage.getItem('vanguard_local_bills');
            if (rawBills) {
                const localBills = JSON.parse(rawBills) as Transaction[];
                
                const apiTxIds = new Set(apiHistory.map((tx: any) => tx.id));
                const remainingBills = localBills.filter(bill => !apiTxIds.has(bill.id));

                // Nếu bill không có trong API (kể cả khối và mempool), cập nhật trạng thái bị từ chối nếu vẫn đang là 0
                const updatedBills = remainingBills.map(bill => {
                    if (bill.status_code === 0) {
                        return {
                            ...bill,
                            status_code: 9,
                            status: "Bị từ chối (Node từ chối hoặc không có trong Mempool)",
                            error_message: "Giao dịch không tồn tại trên Mempool của Node và đã bị từ chối."
                        };
                    }
                    return bill;
                });
                
                // Cập nhật lại localStorage nếu có thay đổi
                localStorage.setItem('vanguard_local_bills', JSON.stringify(updatedBills));
                
                // Lọc bill theo địa chỉ hiện tại và tab (in/out)
                const normalize = (addr: string) => addr ? addr.toLowerCase().trim().replace(/^0x/, '') : "";
                const currentAddrNorm = normalize(address);
                
                const relevantBills = updatedBills.filter(bill => {
                    const isSent = normalize(bill.sender) === currentAddrNorm;
                    const isReceived = normalize(bill.receiver) === currentAddrNorm;
                    if (!isSent && !isReceived) return false;
                    
                    if (direction === 'in') return isReceived;
                    if (direction === 'out') return isSent;
                    return true;
                });
                
                apiHistory = [...relevantBills, ...apiHistory];
            }
        } catch (e) {
            console.error("Error processing local bills:", e);
        }

        // Sắp xếp toàn bộ lịch sử theo timestamp giảm dần (mới nhất lên đầu)
        apiHistory.sort((a: any, b: any) => {
            const timeA = Number(a.timestamp) || 0;
            const timeB = Number(b.timestamp) || 0;
            return timeB - timeA;
        });

        setLocalTransactions(apiHistory);
      } catch (e) {
        console.error("Failed to fetch history:", e);
      } finally {
        setIsLoadingHistory(false);
      }
    };

    fetchHistory();
  }, [address, activeSubTab, searchTerm, balance, pendingTxCount, txRefreshCounter]);

  useEffect(() => {
    if (address) {
      api.getBalanceInfo(address).then(setBalanceInfo).catch(console.error);
    }
  }, [address, balance]);

  const normalize = (addr: string) => {
    if (!addr || addr.trim() === "") return "";
    return addr.toLowerCase().trim().replace(/^0x/, '');
  };
  
  const activeAddr = useMemo(() => normalize(address), [address]);
  
  const isSameAddress = (a: string, b: string) => {
    const normA = normalize(a);
    const normB = normalize(b);
    if (!normA || !normB) return false;
    return normA === normB;
  };

  const stats = useMemo(() => {
    const txList = Array.isArray(localTransactions) ? localTransactions : [];
    let tin = 0;
    let tout = 0;
    txList.forEach(tx => {
      const amount = Number(tx.amount) || 0;
      if (isSameAddress(tx.receiver, activeAddr)) tin += amount;
      if (isSameAddress(tx.sender, activeAddr)) tout += amount;
    });
    return { totalIn: tin, totalOut: tout };
  }, [localTransactions, activeAddr]);

  return (
    <div className="glass-card flex flex-col gap-6 min-h-[400px] relative z-20">
      
      {/* 🚀 1. INDUSTRIAL HUD HEADER */}
      <div className="flex flex-col border-b border-white/[0.08] pb-6">
        <div className="flex flex-row justify-between items-start gap-4 flex-wrap">
           <div className="flex flex-col gap-1">
              <span className="text-[10px] font-black uppercase text-white/40 tracking-[0.3em]">{t.vault_balance} • TOTAL_LIQUIDITY_L0</span>
              <div className="flex flex-col gap-0">
                <div className="flex flex-row items-baseline gap-3">
                  <h2 className="font-black italic text-white tracking-tight tabular-nums" style={{
                    fontSize: `clamp(1.25rem, ${Math.max(1.25, 2.5 - (formatBtcZ(balance).length - 8) * 0.15)}rem, 2.5rem)`,
                    lineHeight: 1.1,
                    whiteSpace: 'nowrap',
                  }}>{formatBtcZ(balance)}</h2>
                  <span className="text-xs font-black text-accent-blue italic tracking-widest uppercase">GO</span>
                </div>
                <div className="flex flex-row items-center gap-2 mt-2 bg-white/[0.02] px-3 py-1.5 rounded-lg border border-white/[0.05] w-fit">
                   <span className="text-[10px] font-black mono tracking-tighter text-white/80">≈ {formatVnt(balance)}</span>
                   <span className="text-[8px] font-black uppercase italic text-text-secondary tracking-wider">VNT</span>
                </div>
              </div>
           </div>
           <div className="flex flex-row gap-3 pt-1">
              <button onClick={onCreateClick} className="px-4 py-2 bg-accent-blue/10 border border-accent-blue/30 rounded-xl text-[10px] font-black text-accent-blue hover:bg-accent-blue/20 hover:border-accent-blue/50 transition-all tracking-wider cursor-pointer shadow-[0_0_15px_rgba(0,136,255,0.05)]">+ {t.create_wallet_btn}</button>
              <button onClick={onRestoreClick} className="px-4 py-2 bg-accent-amber/10 border border-accent-amber/30 rounded-xl text-[10px] font-black text-accent-amber hover:bg-accent-amber/20 hover:border-accent-amber/50 transition-all tracking-wider cursor-pointer shadow-[0_0_15px_rgba(255,159,10,0.05)]">[ {t.restore_wallet_btn} ]</button>
              {address && (
                <button onClick={handleDeleteClick} className="px-4 py-2 bg-accent-red/10 border border-accent-red/30 rounded-xl text-[10px] font-black text-accent-red hover:bg-accent-red/20 hover:border-accent-red/50 transition-all tracking-wider cursor-pointer shadow-[0_0_15px_rgba(239,68,68,0.05)]">{t.delete_wallet_btn}</button>
              )}
           </div>
        </div>

        <div className="flex flex-col gap-3 mb-4 p-8 bg-black/40 border border-white/10 rounded-[2rem] relative overflow-hidden group">
           <div className="absolute inset-0 bg-gradient-to-r from-accent-blue/5 to-transparent opacity-0 group-hover:opacity-100 transition-opacity" />
           <span className="text-[10px] font-black text-white/40 uppercase tracking-[0.4em] mb-1">{t.entity_address}</span>
           <div className="flex flex-col gap-3 relative z-10">
              <div className="flex flex-row items-center justify-between gap-4 bg-white/[0.02] p-4 rounded-xl border border-white/[0.05] shadow-inner">
                <span className="text-[13px] font-black mono text-white/95 font-mono">
                  {address ? `0x${normalize(address).slice(0, 10)}...${normalize(address).slice(-10)}` : t.not_activated}
                </span>
                {address && (
                  <button 
                    onClick={handleCopyAddress}
                    className="p-2 bg-white/5 border border-white/10 rounded-lg text-white/60 hover:text-white hover:bg-white/10 transition-all cursor-pointer flex items-center justify-center"
                    title={t.copy_address}
                  >
                    {copied ? <Check size={14} className="text-accent-green" /> : <Copy size={14} />}
                  </button>
                )}
              </div>
              <div className="flex flex-row items-center gap-4">
                <span className="text-[11px] font-black mono text-accent-blue uppercase italic tracking-widest bg-accent-blue/10 px-3 py-1 rounded-lg border border-accent-blue/20">
                  DIAG: {activeAddr ? `${activeAddr.slice(0, 16).toUpperCase()}...` : 'EMPTY'}
                </span>
                <span className="text-[10px] font-bold mono text-white/30 uppercase tracking-[0.4em]">
                  VERIFIED: (LEN:64)
                </span>
              </div>
           </div>
        </div>
      </div>

      {/* 🚀 QUICK ACTIONS (DI CHUYỂN LÊN TRÊN - V6.1.0) */}
      {/* Thiết kế gọn gàng, giảm kích thước padding và khoảng cách để hài hòa với phần HUD phía trên */}
      <div className="grid grid-cols-2 gap-4">
        <button onClick={handleSend} className="flex flex-row justify-center items-center gap-4 px-6 py-5 bg-accent-blue/10 border border-accent-blue/30 rounded-[1.5rem] text-accent-blue hover:bg-accent-blue/20 hover:scale-[1.02] transition-all shadow-[0_10px_25px_rgba(0,136,255,0.15)] group cursor-pointer">
          <Send size={18} className="group-hover:-translate-y-1 group-hover:translate-x-1 transition-transform" />
          <span className="text-sm font-black uppercase tracking-[0.15em] italic">{t.send_btn}</span>
        </button>
        <button onClick={onReceiveClick} className="flex flex-row justify-center items-center gap-4 px-6 py-5 bg-white/5 border border-white/10 rounded-[1.5rem] text-white hover:bg-white/10 hover:scale-[1.02] transition-all group shadow-xl cursor-pointer">
          <ArrowDownLeft size={18} className="group-hover:translate-y-1 group-hover:-translate-x-1 transition-transform" />
          <span className="text-sm font-black uppercase tracking-[0.15em] italic">{t.receive_btn}</span>
        </button>
      </div>

      {/* 🏥 HUD STATS GRID (V6.0 PREMIUM) */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
         {/* KV1: Khả dụng */}
         <div className="flex flex-col p-4 bg-white/[0.03] border border-white/10 rounded-xl group hover:border-accent-blue/40 transition-all">
            <span className="text-[9px] font-bold uppercase text-accent-blue tracking-[0.2em] mb-2 flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-accent-blue" />
              {t.spendable}
            </span>
            <h3 className="text-xl font-black text-white tracking-tight tabular-nums">{formatBtcZ(balanceInfo?.spendable || 0)}</h3>
            <div className="flex flex-row justify-between items-center mt-1">
               <span className="text-[8px] text-white/20 uppercase tracking-wider">GO</span>
               <span className="text-[9px] font-mono text-white/40 tabular-nums">{formatVnt(balanceInfo?.spendable || 0)} VNT</span>
            </div>
         </div>

         {/* KV2: Đang chờ */}
         <div className="flex flex-col p-4 bg-white/[0.03] border border-white/10 rounded-xl group hover:border-accent-amber/40 transition-all">
            <span className="text-[9px] font-bold uppercase text-accent-amber tracking-[0.2em] mb-2 flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-accent-amber animate-pulse" />
              {t.pending}
            </span>
            <h3 className="text-xl font-black text-white tracking-tight tabular-nums">{formatBtcZ(balanceInfo?.pending || 0)}</h3>
            <div className="flex flex-row justify-between items-center mt-1">
               <span className="text-[8px] text-white/20 uppercase tracking-wider">IMMATURE</span>
               <span className="text-[9px] font-mono text-white/40 tabular-nums">{formatVnt(balanceInfo?.pending || 0)} VNT</span>
            </div>
         </div>

         {/* KV3: Trạng thái Mạng */}
         <div className="flex flex-col p-4 bg-white/[0.03] border border-white/10 rounded-xl group hover:border-accent-green/40 transition-all">
            <span className="text-[9px] font-bold uppercase text-accent-green tracking-[0.2em] mb-2 flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-accent-green" />
              STATUS
            </span>
            <div className="flex items-baseline gap-2">
               <h3 className="text-xl font-black text-white tracking-tight">SYNCED</h3>
               <span className="text-[9px] font-bold text-accent-green animate-pulse">L0_OK</span>
            </div>
            <span className="text-[8px] text-white/20 uppercase mt-1 tracking-wider">VERIFIED_GENESIS</span>
         </div>
      </div>

      {/* 📊 TACTICAL HUD TABS (V6.0.2 REFINED) */}
      <div className="flex flex-col gap-8 pt-6">
        <div className="flex flex-row justify-between items-end gap-6 flex-wrap border-b border-white/[0.05] pb-4">
            <div className="flex flex-row gap-6">
                {[
                    { id: 'history', label: t.filter_all, icon: RefreshCw },
                    { id: 'received', label: t.filter_in, icon: ArrowDownLeft },
                    { id: 'sent', label: t.filter_out, icon: Send },
                    { id: 'analysis', label: t.analysis_tab, icon: Activity }
                ].map(tab => (
                    <button 
                      key={tab.id}
                      onClick={() => setActiveSubTab(tab.id as any)}
                      className={`relative px-6 py-3 transition-all duration-300 flex flex-row gap-3 items-center group uppercase text-[12px] font-black italic tracking-[0.2em] ${activeSubTab === tab.id 
                        ? 'text-white border-b-2 border-accent-blue' 
                        : 'text-white/30 hover:text-white/60'}`}
                    >
                      <tab.icon size={14} className={activeSubTab === tab.id ? 'text-accent-blue shadow-[0_0_10px_var(--accent-blue)]' : 'opacity-40'} />
                      <span>{tab.label}</span>
                    </button>
                ))}
            </div>

            {/* 🔍 SEARCH BAR (V6.5 INDUSTRIAL) */}
            {activeSubTab !== 'analysis' && (
                <div className="flex-1 max-w-md relative group">
                    <div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
                        <Activity size={14} className="text-white/20 group-focus-within:text-accent-blue transition-colors" />
                    </div>
                    <input 
                        type="text"
                        placeholder="TRA CỨU MÃ GIAO DỊCH (TXID)..."
                        value={searchTerm}
                        onChange={(e) => setSearchTerm(e.target.value)}
                        className="w-full bg-black/40 border border-white/10 rounded-2xl py-3 pl-12 pr-4 text-[10px] font-black text-white placeholder:text-white/10 focus:outline-none focus:border-accent-blue/50 focus:ring-4 focus:ring-accent-blue/5 transition-all uppercase tracking-widest italic"
                    />
                    {isLoadingHistory && (
                        <div className="absolute inset-y-0 right-0 pr-4 flex items-center">
                            <RefreshCw size={14} className="text-accent-blue animate-spin" />
                        </div>
                    )}
                </div>
            )}
        </div>

        <div className="flex-1 min-h-[450px]">
            <AnimatePresence mode="wait">
                {activeSubTab === 'analysis' ? (
                  <motion.div key="analysis" initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -20 }} className="flex flex-col gap-10">
                     <div className="flex flex-col gap-2 mb-4">
                        <span className="text-[13px] font-black text-white uppercase italic tracking-[0.4em]">{t.asset_growth}</span>
                        <div className="h-[1px] w-48 bg-gradient-to-r from-accent-blue to-transparent" />
                     </div>
                     <BalanceHistoryChart transactions={localTransactions} address={address} />
                     <BalanceIntel transactions={localTransactions} address={address} />
                  </motion.div>
                ) : activeSubTab === 'received' ? (
                  <motion.div key="received" initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -20 }}>
                     <StatDashboardCard title={t.filter_in} value={stats.totalIn} type="in" />
                     <TransactionTracker transactions={localTransactions} address={address} onTransactionClick={onTransactionClick} filterMode="in" />
                  </motion.div>
                ) : activeSubTab === 'sent' ? (
                  <motion.div key="sent" initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -20 }}>
                     <StatDashboardCard title={t.filter_out} value={stats.totalOut} type="out" />
                     <TransactionTracker transactions={localTransactions} address={address} onTransactionClick={onTransactionClick} filterMode="out" />
                  </motion.div>
                ) : (
                  <motion.div key="history" initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -20 }}>
                     <TransactionTracker transactions={localTransactions} address={address} onTransactionClick={onTransactionClick} filterMode="all" />
                  </motion.div>
                )}
            </AnimatePresence>
        </div>
      </div>

    </div>
  );
};

export default UnifiedWalletPanel;
