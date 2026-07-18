/**
 * @file BlockDetailPage.tsx
 * @brief Trang Chi tiết Khối — Phụ Lục D §4
 * @tính_năng:
 *   - Header info: Height, Hash, Parent Hash, Timestamp, Nonce, Difficulty
 *   - State Root, Tx Root (rút gọn + copy)
 *   - ZK-Proof status badge
 *   - Danh sách giao dịch trong block (bảng đầy đủ cột)
 *   - [i18n] 100% đa ngôn ngữ qua translation system
 */

import React, { useState, useEffect } from 'react';
import { motion } from 'framer-motion';
import { ArrowLeft, Copy, Check, Shield, Clock, Lock, Layers, Cpu, Fingerprint } from 'lucide-react';
import api from '../../api';
import type { BlockDetail, Transaction } from '../../api';
import { useLanguage } from '../../LanguageContext';
import { formatBtcZ, formatDifficulty } from '../../utils';

interface BlockDetailPageProps {
  height: number;
  onBack: () => void;
  onTxClick: (tx: Transaction) => void;
  onAddressClick: (address: string) => void;
}

const BlockDetailPage: React.FC<BlockDetailPageProps> = ({ height, onBack, onTxClick, onAddressClick }) => {
  const [block, setBlock] = useState<BlockDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [copiedField, setCopiedField] = useState<string | null>(null);
  const { t, getLocale } = useLanguage();

  useEffect(() => {
    setLoading(true);
    api.getBlockDetail(height).then(data => {
      setBlock(data);
      setLoading(false);
    });
  }, [height]);

  const copyToClipboard = (text: string, field: string) => {
    navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const truncHash = (h: string) => h.length > 16 ? `${h.slice(0, 8)}...${h.slice(-8)}` : h;

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[400px]">
        <div className="flex flex-col items-center gap-4 opacity-40">
          <Layers size={40} className="animate-pulse" />
          <span className="tactical-label">{t.loading_block} #{height}...</span>
        </div>
      </div>
    );
  }

  if (!block) {
    return (
      <div className="flex flex-col items-center justify-center h-[400px] gap-4">
        <Shield size={48} className="text-red-400 opacity-40" />
        <span className="text-sm text-text-muted">Block #{height} {t.block_not_found}</span>
        <button onClick={onBack} className="text-accent-blue text-xs font-black uppercase hover:underline">{t.go_back}</button>
      </div>
    );
  }

  const infoRows = [
    { label: t.col_height, value: `#${block.height}`, icon: <Layers size={14} /> },
    { label: 'Block Hash', value: block.hash, copyable: true, icon: <Fingerprint size={14} /> },
    { label: 'Parent Hash', value: block.parent_hash, copyable: true },
    { label: 'Timestamp', value: block.timestamp > 0 ? new Date(block.timestamp * 1000).toLocaleString(getLocale()) : 'Genesis' },
    { label: 'Nonce', value: block.nonce.toString() },
    { label: 'Difficulty', value: formatDifficulty(block.difficulty), icon: <Cpu size={14} /> },
    { label: 'State Root', value: block.state_root, copyable: true },
    { label: 'Tx Root', value: block.tx_root, copyable: true },
    { label: t.col_miner, value: block.miner, copyable: true, clickable: true },
  ];

  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="vanguard-flex-v vanguard-gap-medium">
      {/* Back + Header */}
      <div className="vanguard-flex-h items-center gap-4">
        <button onClick={onBack} className="p-2 rounded-xl bg-white/5 border border-white/10 hover:bg-white/10 transition-all">
          <ArrowLeft size={18} />
        </button>
        <div className="vanguard-flex-v vanguard-gap-tiny">
          <h2 className="text-xl font-black italic text-white uppercase tracking-tight">Block #{block.height}</h2>
          <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest">{t.block_detail_subtitle} • {block.tx_count} {t.tx_count_suffix}</span>
        </div>
        {/* ZK-Proof Badge */}
        <div className={`ml-auto px-4 py-2 rounded-xl border text-[9px] font-black uppercase tracking-widest ${block.zk_proof_status.includes('Đã') ? 'bg-accent-green/10 border-accent-green/20 text-accent-green' : 'bg-accent-amber/10 border-accent-amber/20 text-accent-amber'}`}>
          {block.zk_proof_status.includes('Đã') ? '✅' : '⏳'} {block.zk_proof_status}
        </div>
      </div>

      {/* Header Info Table */}
      <div className="glass-card vanguard-flex-v vanguard-gap-tiny p-0 overflow-hidden">
        <div className="p-4 border-b border-white/5">
          <span className="tactical-label text-[9px] uppercase tracking-widest">{t.header_info}</span>
        </div>
        {infoRows.map((row, i) => (
          <div key={i} className={`flex items-center px-4 py-3 ${i % 2 === 0 ? 'bg-white/[0.01]' : ''} border-b border-white/[0.03]`}>
            <div className="flex items-center gap-2 w-[140px] shrink-0">
              {row.icon && <span className="text-text-muted">{row.icon}</span>}
              <span className="text-[9px] font-black text-text-muted uppercase tracking-wider">{row.label}</span>
            </div>
            <div className="flex-1 flex items-center gap-2 min-w-0">
              <span
                className={`text-[10px] font-bold mono truncate ${row.clickable ? 'text-accent-blue cursor-pointer hover:underline' : 'text-white/80'}`}
                onClick={row.clickable ? () => onAddressClick('0x' + row.value) : undefined}
              >
                {row.copyable ? truncHash(row.value) : row.value}
              </span>
              {row.copyable && (
                <button
                  onClick={() => copyToClipboard(row.value, row.label)}
                  className="p-1 rounded hover:bg-white/10 transition-all shrink-0"
                >
                  {copiedField === row.label ? <Check size={12} className="text-accent-green" /> : <Copy size={12} className="text-text-muted" />}
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* Transaction List */}
      <div className="glass-card vanguard-flex-v vanguard-gap-small">
        <div className="vanguard-flex-h justify-between items-center">
          <span className="tactical-label text-[9px]">{t.tx_in_block} ({block.tx_count})</span>
        </div>

        {block.transactions.length === 0 ? (
          <div className="flex flex-col items-center py-12 opacity-30">
            <Layers size={28} className="mb-3" />
            <span className="tactical-label text-[9px]">{t.empty_block}</span>
          </div>
        ) : (
          <div className="vanguard-flex-v vanguard-gap-tiny">
            {/* Table Header */}
            <div className="grid grid-cols-5 gap-2 px-3 py-2 bg-white/[0.02] rounded-xl">
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_txid}</span>
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_sender}</span>
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_receiver}</span>
              <span className="text-[8px] font-black text-text-muted uppercase text-right">{t.col_amount}</span>
              <span className="text-[8px] font-black text-text-muted uppercase text-right">{t.col_status}</span>
            </div>
            {block.transactions.map(tx => (
              <div
                key={tx.id}
                onClick={() => onTxClick(tx)}
                className="grid grid-cols-5 gap-2 px-3 py-3 border border-white/[0.03] rounded-xl cursor-pointer hover:border-white/10 transition-all"
              >
                <span className="text-[9px] mono text-accent-blue truncate">{truncHash(tx.id)}</span>
                <span
                  className="text-[9px] mono text-white/60 truncate cursor-pointer hover:text-accent-blue"
                  onClick={(e) => { e.stopPropagation(); onAddressClick('0x' + tx.sender); }}
                >{truncHash(tx.sender)}</span>
                <span
                  className="text-[9px] mono text-white/60 truncate cursor-pointer hover:text-accent-blue"
                  onClick={(e) => { e.stopPropagation(); onAddressClick('0x' + tx.receiver); }}
                >{truncHash(tx.receiver)}</span>
                <span className="text-[9px] font-black text-white text-right">{formatBtcZ(tx.amount)} GO</span>
                <div className="flex justify-end">
                  <span className={`text-[8px] font-black px-2 py-0.5 rounded ${tx.status === 'FINALIZED' ? 'bg-accent-green/10 text-accent-green' : 'bg-accent-amber/10 text-accent-amber'}`}>
                    {tx.status === 'FINALIZED' ? <><Lock size={8} className="inline mr-1" />{t.immutable}</> : <><Clock size={8} className="inline mr-1" />{Math.max(0, tx.confirmations - 1)}/5</>}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </motion.div>
  );
};

export default BlockDetailPage;
