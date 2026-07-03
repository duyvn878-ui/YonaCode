/**
 * @file AddressDetailPage.tsx
 * @brief Trang Chi tiết Địa chỉ Ví — Phụ Lục D §4
 * @tính_năng:
 *   - Hiển thị địa chỉ đầy đủ + QR code + Copy
 *   - Tổng số dư hiện tại
 *   - Lịch sử giao dịch (bảng: TxID, Ngày, Hướng, Số tiền, Trạng thái Bất biến)
 *   - Biểu đồ số dư theo thời gian (Canvas sparkline)
 *   - [i18n] 100% đa ngôn ngữ qua translation system
 */

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { motion } from 'framer-motion';
import { ArrowLeft, Copy, Check, Shield, Lock, Clock, ArrowUpRight, ArrowDownLeft, Layers, TrendingUp } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import api from '../../api';
import type { Transaction } from '../../api';
import { useLanguage } from '../../LanguageContext';
import { formatBtcZ } from '../../utils';

interface AddressDetailPageProps {
  address: string;
  onBack: () => void;
  onTxClick: (tx: Transaction) => void;
}

const AddressDetailPage: React.FC<AddressDetailPageProps> = ({ address, onBack, onTxClick }) => {
  const [balance, setBalance] = useState(0);
  const [history, setHistory] = useState<Transaction[]>([]);
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState(false);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const { t, getLocale } = useLanguage();

  useEffect(() => {
    setLoading(true);
    Promise.all([
      api.getBalance(address),
      api.getAddressHistory(address)
    ]).then(([bal, hist]) => {
      setBalance(bal);
      setHistory(hist.history);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, [address]);

  const copyAddr = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const truncHash = (h: string) => h.length > 16 ? `${h.slice(0, 8)}...${h.slice(-8)}` : h;

  // Vẽ biểu đồ số dư theo thời gian (sparkline)
  const drawBalanceChart = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas || history.length < 2) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);

    // Tính số dư tích lũy theo thời gian
    const sorted = [...history].sort((a, b) => a.timestamp - b.timestamp);
    let runningBalance = 0;
    const points: number[] = [];
    sorted.forEach(tx => {
      if (tx.direction === 'IN') runningBalance += tx.amount;
      else runningBalance -= tx.amount;
      points.push(runningBalance);
    });

    if (points.length < 2) return;
    const maxVal = Math.max(...points, 1);
    const padding = 10;
    const chartH = h - padding * 2;
    const chartW = w - padding * 2;
    const stepX = chartW / (points.length - 1);

    // Gradient fill
    const gradient = ctx.createLinearGradient(0, padding, 0, h - padding);
    gradient.addColorStop(0, 'rgba(0, 242, 148, 0.15)');
    gradient.addColorStop(1, 'rgba(0, 242, 148, 0)');

    ctx.beginPath();
    ctx.moveTo(padding, h - padding);
    points.forEach((val, i) => {
      const x = padding + i * stepX;
      const y = padding + chartH - (val / maxVal) * chartH;
      ctx.lineTo(x, y);
    });
    ctx.lineTo(padding + (points.length - 1) * stepX, h - padding);
    ctx.closePath();
    ctx.fillStyle = gradient;
    ctx.fill();

    // Line
    ctx.beginPath();
    ctx.strokeStyle = '#00f294';
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    points.forEach((val, i) => {
      const x = padding + i * stepX;
      const y = padding + chartH - (val / maxVal) * chartH;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    });
    ctx.stroke();
  }, [history]);

  useEffect(() => { drawBalanceChart(); }, [drawBalanceChart]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[400px]">
        <div className="flex flex-col items-center gap-4 opacity-40">
          <Shield size={40} className="animate-pulse" />
          <span className="tactical-label">{t.loading_address}</span>
        </div>
      </div>
    );
  }

  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="vanguard-flex-v vanguard-gap-medium">
      {/* Back + Header */}
      <div className="vanguard-flex-h items-center gap-4">
        <button onClick={onBack} className="p-2 rounded-xl bg-white/5 border border-white/10 hover:bg-white/10 transition-all">
          <ArrowLeft size={18} />
        </button>
        <div className="vanguard-flex-v vanguard-gap-tiny">
          <h2 className="text-xl font-black italic text-white uppercase tracking-tight">{t.address_title}</h2>
          <span className="text-[9px] font-bold text-text-muted uppercase tracking-widest">
            {history.length} {t.tx_count_suffix}
          </span>
        </div>
      </div>

      {/* Address Card + QR */}
      <div className="glass-card vanguard-grid-2 items-center">
        <div className="vanguard-flex-v vanguard-gap-medium">
          {/* Address */}
          <div className="vanguard-flex-v vanguard-gap-tiny">
            <span className="tactical-label text-[8px]">{t.public_address}</span>
            <div className="flex items-center gap-2">
              <span className="text-[10px] mono text-accent-blue break-all">{address}</span>
              <button onClick={copyAddr} className="p-1.5 rounded-lg bg-white/5 hover:bg-white/10 transition-all shrink-0">
                {copied ? <Check size={14} className="text-accent-green" /> : <Copy size={14} className="text-text-muted" />}
              </button>
            </div>
          </div>
          {/* Balance */}
          <div className="vanguard-flex-v vanguard-gap-tiny">
            <span className="tactical-label text-[8px]">{t.total_balance}</span>
            <div className="flex items-baseline gap-2">
              <span className="text-4xl font-black italic text-white">{formatBtcZ(balance)}</span>
              <span className="text-lg font-black text-accent-blue italic">GO</span>
            </div>
          </div>
        </div>

        {/* QR Code */}
        <div className="flex justify-center">
          <div className="p-4 bg-white rounded-2xl shadow-2xl">
            <QRCodeSVG value={address} size={140} bgColor="#ffffff" fgColor="#000000" level="M" />
          </div>
        </div>
      </div>

      {/* Balance Chart */}
      {history.length >= 2 && (
        <div className="glass-card vanguard-flex-v vanguard-gap-small">
          <div className="flex items-center gap-2">
            <TrendingUp size={14} className="text-accent-green" />
            <span className="tactical-label text-[9px]">{t.balance_chart}</span>
          </div>
          <div className="p-3 bg-black/40 border border-white/5 rounded-2xl">
            <canvas ref={canvasRef} width={600} height={100} className="w-full h-[100px]" />
          </div>
        </div>
      )}

      {/* Transaction History */}
      <div className="glass-card vanguard-flex-v vanguard-gap-small">
        <span className="tactical-label text-[9px]">{t.tx_history}</span>

        {history.length === 0 ? (
          <div className="flex flex-col items-center py-12 opacity-30">
            <Layers size={28} className="mb-3" />
            <span className="tactical-label text-[9px]">{t.no_tx_yet}</span>
          </div>
        ) : (
          <div className="vanguard-flex-v vanguard-gap-tiny">
            {/* Table Header */}
            <div className="grid grid-cols-5 gap-2 px-3 py-2 bg-white/[0.02] rounded-xl">
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_txid}</span>
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_datetime}</span>
              <span className="text-[8px] font-black text-text-muted uppercase">{t.col_direction}</span>
              <span className="text-[8px] font-black text-text-muted uppercase text-right">{t.col_amount}</span>
              <span className="text-[8px] font-black text-text-muted uppercase text-right">{t.col_status}</span>
            </div>
            {history.map(tx => (
              <div
                key={tx.id}
                onClick={() => onTxClick(tx)}
                className="grid grid-cols-5 gap-2 px-3 py-3 border border-white/[0.03] rounded-xl cursor-pointer hover:border-white/10 transition-all"
              >
                <span className="text-[9px] mono text-accent-blue truncate">{truncHash(tx.id)}</span>
                <span className="text-[9px] text-white/60">
                  {tx.timestamp > 0 ? new Date(tx.timestamp * 1000).toLocaleDateString(getLocale()) : '--'}
                </span>
                <div className="flex items-center gap-1">
                  {tx.direction === 'IN' ? (
                    <><ArrowDownLeft size={10} className="text-accent-green" /><span className="text-[8px] font-black text-accent-green">{t.direction_in}</span></>
                  ) : (
                    <><ArrowUpRight size={10} className="text-accent-amber" /><span className="text-[8px] font-black text-accent-amber">{t.direction_out}</span></>
                  )}
                </div>
                <span className={`text-[9px] font-black text-right ${tx.direction === 'IN' ? 'text-accent-green' : 'text-white'}`}>
                  {tx.direction === 'IN' ? '+' : '-'}{formatBtcZ(tx.amount)} GO
                </span>
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

export default AddressDetailPage;
