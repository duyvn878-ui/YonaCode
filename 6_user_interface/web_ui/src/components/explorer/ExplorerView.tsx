/**
 * @file ExplorerView.tsx
 * @brief Trang Trình duyệt Chuỗi khối — Phụ Lục D §4 (Main Explorer)
 * @tính_năng:
 *   - Thanh tìm kiếm: Block Height, Hash, TxID, Address
 *   - Danh sách khối gần nhất (bảng)
 *   - Danh sách giao dịch gần nhất (bảng)
 *   - Sub-routing: click block → BlockDetailPage, click address → AddressDetailPage
 *   - [i18n] 100% đa ngôn ngữ qua translation system
 */

import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Search, Layers, Clock, Lock, ArrowRight, Hash, Fingerprint } from 'lucide-react';
import api from '../../api';
import type { NodeStatus, BlockHeader, Transaction } from '../../api';
import BlockDetailPage from './BlockDetailPage';
import AddressDetailPage from './AddressDetailPage';
import { useLanguage } from '../../LanguageContext';

interface ExplorerViewProps {
  status: NodeStatus | null;
  blocks: BlockHeader[];
  transactions: Transaction[];
  onTxClick: (tx: Transaction) => void;
}

type SubView = 
  | { type: 'list' }
  | { type: 'block'; height: number }
  | { type: 'address'; address: string };

const ExplorerView: React.FC<ExplorerViewProps> = ({ status, blocks, transactions, onTxClick }) => {
  const { t, getLocale } = useLanguage();
  const [subView, setSubView] = useState<SubView>({ type: 'list' });
  const [searchQuery, setSearchQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState('');

  const handleSearch = async () => {
    if (!searchQuery.trim()) return;
    setSearching(true);
    setSearchError('');

    const result = await api.searchQuery(searchQuery.trim());
    setSearching(false);

    if (!result) {
      setSearchError(t.search_no_result + searchQuery);
      return;
    }

    switch (result.type) {
      case 'block':
        setSubView({ type: 'block', height: result.height! });
        break;
      case 'tx':
        // Mở Transaction Detail Modal
        const tx = await api.getTxDetail(result.txid!);
        if (tx) onTxClick(tx);
        break;
      case 'address':
        setSubView({ type: 'address', address: result.address! });
        break;
    }
    setSearchQuery('');
  };

  const truncHash = (h: string) => h.length > 16 ? `${h.slice(0, 8)}...${h.slice(-8)}` : h;

  // Sub-routing
  if (subView.type === 'block') {
    return (
      <BlockDetailPage
        height={subView.height}
        onBack={() => setSubView({ type: 'list' })}
        onTxClick={onTxClick}
        onAddressClick={(addr) => setSubView({ type: 'address', address: addr })}
      />
    );
  }

  if (subView.type === 'address') {
    return (
      <AddressDetailPage
        address={subView.address}
        onBack={() => setSubView({ type: 'list' })}
        onTxClick={onTxClick}
      />
    );
  }

  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="vanguard-flex-v vanguard-gap-medium">

      {/* Search Bar */}
      <div className="glass-card p-4">
        <div className="flex items-center gap-3">
          <div className="flex-1 relative">
            <Search size={16} className="absolute left-4 top-1/2 -translate-y-1/2 text-text-muted" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => { setSearchQuery(e.target.value); setSearchError(''); }}
              onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
              placeholder={t.search_placeholder}
              className="w-full pl-11 pr-4 py-3 bg-black/40 border border-white/10 rounded-xl text-xs text-white placeholder-text-muted focus:border-accent-blue focus:outline-none transition-all"
            />
          </div>
          <button
            onClick={handleSearch}
            disabled={searching}
            className="px-6 py-3 bg-accent-blue/20 border border-accent-blue/30 rounded-xl text-accent-blue text-[10px] font-black uppercase tracking-wider hover:bg-accent-blue/30 transition-all disabled:opacity-50"
          >
            {searching ? '...' : t.search_btn}
          </button>
        </div>
        <AnimatePresence>
          {searchError && (
            <motion.p initial={{ opacity: 0, y: -5 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} className="text-[9px] text-red-400 mt-2 pl-4">
              ⚠ {searchError}
            </motion.p>
          )}
        </AnimatePresence>
      </div>

      {/* Stats Bar */}
      <div className="grid grid-cols-4 gap-3">
        <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl">
          <span className="tactical-label text-[8px] block mb-1">{t.block_height_label}</span>
          <span className="text-lg font-black text-white italic">
            {(status?.highest_height === 0 && status?.sync.state !== 'SYNCED' && status?.sync.state !== 'STREAMING') ? 'SCANNING...' : `#${status?.highest_height || 0}`}
          </span>
        </div>
        <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl">
          <span className="tactical-label text-[8px] block mb-1">{t.difficulty_label}</span>
          <span className="text-lg font-black text-accent-amber italic">{status?.difficulty || 0}</span>
        </div>
        <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl">
          <span className="tactical-label text-[8px] block mb-1">{t.block_time_label}</span>
          <span className="text-lg font-black text-accent-green italic">{status?.avg_block_time ? `~${status.avg_block_time.toFixed(1)}s` : 'WAIT...'}</span>
        </div>
        <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl">
          <span className="tactical-label text-[8px] block mb-1">{t.finalized_label}</span>
          <span className="text-lg font-black text-accent-blue italic">#{status?.finalized || 0}</span>
        </div>
      </div>

      {/* Two-column: Blocks + Transactions */}
      <div className="vanguard-grid-2">
        {/* Recent Blocks */}
        <div className="glass-card vanguard-flex-v vanguard-gap-small">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Layers size={14} className="text-accent-blue" />
              <span className="tactical-label text-[9px]">{t.explorer_blocks}</span>
            </div>
          </div>
          <div className="vanguard-flex-v vanguard-gap-tiny">
            {blocks.slice(0, 8).map(block => (
              <div
                key={block.height}
                onClick={() => setSubView({ type: 'block', height: block.height })}
                className="flex items-center justify-between p-3 border border-white/[0.03] rounded-xl cursor-pointer hover:border-accent-blue/20 transition-all group"
              >
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-lg bg-accent-blue/10 flex items-center justify-center text-accent-blue">
                    <Hash size={14} />
                  </div>
                  <div className="flex flex-col">
                    <span className="text-[10px] font-black text-white group-hover:text-accent-blue transition-colors">Block #{block.height}</span>
                    <span className="text-[8px] text-text-muted mono">{truncHash(block.hash)}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-[8px] text-text-muted">
                    {block.timestamp > 0 ? new Date(block.timestamp * 1000).toLocaleTimeString(getLocale()) : 'Genesis'}
                  </span>
                  <ArrowRight size={12} className="text-text-muted group-hover:text-accent-blue transition-colors" />
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Recent Transactions */}
        <div className="glass-card vanguard-flex-v vanguard-gap-small">
          <div className="flex items-center gap-2">
            <Fingerprint size={14} className="text-accent-green" />
            <span className="tactical-label text-[9px]">{t.explorer_txs}</span>
          </div>
          <div className="vanguard-flex-v vanguard-gap-tiny">
            {transactions.length === 0 ? (
              <div className="flex flex-col items-center py-12 opacity-30">
                <Layers size={24} className="mb-2" />
                <span className="text-[9px] tactical-label">{t.no_tx_yet}</span>
              </div>
            ) : (
              transactions.slice(0, 8).map(tx => (
                <div
                  key={tx.id}
                  onClick={() => onTxClick(tx)}
                  className="flex items-center justify-between p-3 border border-white/[0.03] rounded-xl cursor-pointer hover:border-accent-green/20 transition-all group"
                >
                  <div className="flex items-center gap-3">
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${tx.status === 'FINALIZED' ? 'bg-accent-green/10 text-accent-green' : 'bg-accent-amber/10 text-accent-amber'}`}>
                      {tx.status === 'FINALIZED' ? <Lock size={14} /> : <Clock size={14} />}
                    </div>
                    <div className="flex flex-col">
                      <span className="text-[10px] font-black mono text-white group-hover:text-accent-blue transition-colors truncate max-w-[120px]">{truncHash(tx.id)}</span>
                      <span className="text-[8px] text-text-muted">{tx.amount} GO • {t.fee_suffix} {tx.fee} GO</span>
                    </div>
                  </div>
                  <div className="flex flex-col items-end">
                    <span className={`text-[8px] font-black px-2 py-0.5 rounded ${tx.status === 'FINALIZED' ? 'bg-accent-green/10 text-accent-green' : 'bg-accent-amber/10 text-accent-amber'}`}>
                      {tx.status === 'FINALIZED' ? `🔒 ${t.immutable}` : `⏳ ${Math.max(0, tx.confirmations - 1)}/5`}
                    </span>
                    <span className="text-[7px] text-text-muted mt-1">Block #{tx.height || 'Mempool'}</span>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </motion.div>
  );
};

export default ExplorerView;
