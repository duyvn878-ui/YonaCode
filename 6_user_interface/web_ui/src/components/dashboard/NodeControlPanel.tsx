/**
 * @file NodeControlPanel.tsx
 * @brief Panel Điều khiển Node — Dashboard Controls V3.0
 * @tính_năng:
 *   - Chế độ Chỉ Xác minh (Verify-Only): Nút chuyển đổi, gọi API /api/v1/node/mode
 *   - Kích hoạt Thợ đào (Mining Mode): Nút bật/tắt Blake3-PoW
 *   - Biểu đồ Hashrate Realtime: Canvas drawing 60 điểm dữ liệu từ SSE ring buffer
 *   - Mode indicator: Hiển thị mô tả chế độ hiện tại + trạng thái tài nguyên
 */

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Shield, Pickaxe, Cpu, Zap, TrendingUp, Trash2 } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import api, { type NodeStatus, type MinerStatus } from '../../api';

interface NodeControlPanelProps {
  status: NodeStatus | null;
  miner: MinerStatus | null;
  onToggleMiner: () => void;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
  isStopping?: boolean;
}

const NodeControlPanel: React.FC<NodeControlPanelProps> = ({ status, miner, onToggleMiner, onNotify, isStopping = false }) => {
  const [nodeMode, setNodeMode] = useState<string>('verify-only');
  const [cpuIntensity, setCpuIntensity] = useState<number>(50);
  const [showPurgeModal, setShowPurgeModal] = useState(false);
  const [purgeCode, setPurgeCode] = useState('');
  const [isPurging, setIsPurging] = useState(false);

  const { lang, t } = useLanguage();
  const canvasRef = useRef<HTMLCanvasElement>(null);

  // Đồng bộ nodeMode và cpuIntensity từ SSE
  useEffect(() => {
    if (status?.node_mode) {
      setNodeMode(status.node_mode);
    }
    if (status?.cpu_intensity) {
      setCpuIntensity(status.cpu_intensity);
    }
  }, [status?.node_mode, status?.cpu_intensity]);

  // Xử lý kéo thanh trượt CPU (Realtime state update)
  const handleIntensityChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setCpuIntensity(parseInt(e.target.value));
  };

  const handleIntensityCommit = async () => {
    try {
      await api.setCpuIntensity(cpuIntensity);
      onNotify(`${t.node_intel}: ${cpuIntensity}%`, 'success');
    } catch (e) {
      onNotify(`${t.node_intel} Failed: ${e}`, 'error');
    }
  };

  // Vẽ biểu đồ Hashrate trên Canvas
  const drawHashrateChart = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const history = status?.hashrate_history || [];
    const w = canvas.width;
    const h = canvas.height;

    // Clear canvas
    ctx.clearRect(0, 0, w, h);

    if (history.length < 2) {
      // Không đủ dữ liệu, vẽ baseline
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.05)';
      ctx.beginPath();
      ctx.moveTo(0, h - 10);
      ctx.lineTo(w, h - 10);
      ctx.stroke();
      
      ctx.fillStyle = 'rgba(255, 255, 255, 0.15)';
      ctx.font = '10px Inter, sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText(t.collecting_hashrate, w / 2, h / 2);
      return;
    }

    // Tìm max hashrate để scale
    const maxRate = Math.max(...history, 1);
    const padding = 10;
    const chartH = h - padding * 2;
    const chartW = w - padding * 2;
    const stepX = chartW / (history.length - 1);

    // Vẽ grid lines (3 đường ngang)
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.03)';
    ctx.lineWidth = 1;
    for (let i = 1; i <= 3; i++) {
      const y = padding + (chartH / 4) * i;
      ctx.beginPath();
      ctx.moveTo(padding, y);
      ctx.lineTo(w - padding, y);
      ctx.stroke();
    }

    // Vẽ fill gradient dưới đường chart
    const gradient = ctx.createLinearGradient(0, padding, 0, h - padding);
    gradient.addColorStop(0, 'rgba(0, 136, 255, 0.15)');
    gradient.addColorStop(1, 'rgba(0, 136, 255, 0)');
    
    ctx.beginPath();
    ctx.moveTo(padding, h - padding);
    history.forEach((rate, i) => {
      const x = padding + i * stepX;
      const y = padding + chartH - (rate / maxRate) * chartH;
      if (i === 0) ctx.lineTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.lineTo(padding + (history.length - 1) * stepX, h - padding);
    ctx.closePath();
    ctx.fillStyle = gradient;
    ctx.fill();

    // Vẽ đường chart chính
    ctx.beginPath();
    ctx.strokeStyle = '#0088ff';
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    history.forEach((rate, i) => {
      const x = padding + i * stepX;
      const y = padding + chartH - (rate / maxRate) * chartH;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();

    // Vẽ điểm cuối (live indicator)
    if (history.length > 0) {
      const lastI = history.length - 1;
      const x = padding + lastI * stepX;
      const y = padding + chartH - (history[lastI] / maxRate) * chartH;
      
      // Glow halo
      const glow = ctx.createRadialGradient(x, y, 0, x, y, 12);
      glow.addColorStop(0, 'rgba(0, 136, 255, 0.4)');
      glow.addColorStop(1, 'rgba(0, 136, 255, 0)');
      ctx.fillStyle = glow;
      ctx.fillRect(x - 12, y - 12, 24, 24);
      
      // Live dot
      ctx.beginPath();
      ctx.arc(x, y, 4, 0, Math.PI * 2);
      ctx.fillStyle = '#0088ff';
      ctx.fill();
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 1.5;
      ctx.stroke();
    }
  }, [status?.hashrate_history, lang]);

  useEffect(() => {
    drawHashrateChart();
  }, [drawHashrateChart]);

  const handlePurge = async () => {
    if (purgeCode !== '01900') {
      onNotify(t.purge_error_code.replace('{0}', '01900'), 'error');
      return;
    }

    setIsPurging(true);
    try {
      const response = await api.purgeData('01900');
      onNotify(t.lang === 'vi' ? response.message : t.purge_success, 'success');
      setShowPurgeModal(false);
      // Đợi node thoát
      setTimeout(() => {
        window.location.reload();
      }, 3000);
    } catch (e: any) {
      onNotify(`${t.lang === 'vi' ? 'Lỗi xóa dữ liệu' : 'Purge error'}: ${e.message}`, 'error');
      setIsPurging(false);
    }
  };

  const hashrate = status?.hashrate || miner?.hashrate || 0;
  const isMining = status?.is_mining ?? miner?.is_mining ?? false;
  const isVerifyOnly = nodeMode === 'verify-only';

  // Hiển thị hashrate dạng đọc được
  const formatHashrate = (h: number): string => {
    if (h >= 1e15) return `${(h / 1e15).toFixed(2)} PH/s`;
    if (h >= 1e12) return `${(h / 1e12).toFixed(2)} TH/s`;
    if (h >= 1e9) return `${(h / 1e9).toFixed(2)} GH/s`;
    if (h >= 1e6) return `${(h / 1e6).toFixed(2)} MH/s`;
    if (h >= 1e3) return `${(h / 1e3).toFixed(2)} KH/s`;
    return `${h.toFixed(0)} H/s`;
  };

  return (
    <div className="glass-card vanguard-flex-v vanguard-gap-medium">
      {/* Header */}
      <div className="vanguard-panel-header vanguard-flex-h justify-between items-center px-2">
        <div className="vanguard-flex-h vanguard-gap-medium">
          <div className="vanguard-flex-v vanguard-gap-tiny">
            <span className="text-[10px] font-black uppercase tracking-[0.2em] italic text-white flex items-center gap-2">
              <Shield size={14} className="text-accent-blue" />
              {t.miner_title}
            </span>
            <span className="text-[8px] font-bold text-text-muted uppercase tracking-widest">
              BLAKE3_POW_V1.0
            </span>
          </div>
        </div>
        <div className="vanguard-flex-h vanguard-gap-small items-center">
          {isMining && (
            <div className="vanguard-flex-h items-center gap-2 px-3 py-1 bg-accent-amber/10 border border-accent-amber/20 rounded-lg">
              <div className="w-1.5 h-1.5 rounded-full bg-accent-amber animate-pulse shadow-[0_0_8px_var(--accent-amber)]" />
              <span className="text-[9px] font-black uppercase text-accent-amber tracking-widest">
                {t.miner_status}
              </span>
            </div>
          )}
        </div>
      </div>

      {/* 🛡️ INDUSTRIAL NODE ACTIONS */}
      <div className="grid grid-cols-3 gap-4 border-t border-white/5 pt-6">
        {/* Nút Đầy đủ (Full Node) */}
        <div className="p-4 bg-white/[0.01] border border-white/5 rounded text-center vanguard-flex-v items-center justify-center vanguard-gap-tiny">
           <span className="text-[10px] font-black text-white/40 uppercase italic tracking-widest">{t.full_node}</span>
           <div className="w-2 h-2 rounded-full bg-accent-green shadow-[0_0_8px_var(--accent-green)]" />
        </div>

        {/* Bật Đào (Mining Toggle) */}
        <button 
           onClick={onToggleMiner}
           disabled={isStopping}
           className={`p-4 border rounded text-center transition-all vanguard-flex-v items-center justify-center vanguard-gap-tiny 
             ${isStopping ? 'bg-white/5 border-white/10 opacity-50 cursor-wait' : (isMining ? 'bg-accent-green/5 border-accent-green/20' : 'bg-white/[0.01] border-white/5')}`}
        >
           <span className={`text-[10px] font-black uppercase italic tracking-widest ${isStopping ? 'text-white/40' : (isMining ? 'text-accent-green' : 'text-white/40')}`}>
              {isStopping ? (t.lang === 'vi' ? 'ĐANG TẮT...' : 'STOPPING...') : `${t.start_pow} [${isMining ? t.toggle_on : t.toggle_off}]`}
           </span>
           <Pickaxe size={14} className={isStopping ? 'text-white/20 animate-spin' : (isMining ? 'text-accent-green animate-pulse' : 'text-white/20')} />
        </button>

        {/* Dọn dẹp (Purge) */}
        <button 
           onClick={() => setShowPurgeModal(true)}
           className="p-4 bg-white/[0.01] border border-white/5 rounded text-center transition-all hover:bg-accent-red/5 hover:border-accent-red/20 group vanguard-flex-v items-center justify-center vanguard-gap-tiny"
        >
           <span className="text-[10px] font-black text-white/40 group-hover:text-accent-red uppercase italic tracking-widest">{t.purge_data_btn}</span>
           <Trash2 size={14} className="text-white/20 group-hover:text-accent-red" />
        </button>
      </div>

      {/* MODAL XÁC NHẬN XÓA DATA (01900) */}
      {showPurgeModal && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-md p-4">
          <motion.div 
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            className="w-full max-w-md glass-card p-6 vanguard-flex-v vanguard-gap-large border-accent-red/30"
          >
            <div className="vanguard-flex-v vanguard-gap-tiny items-center text-center">
              <div className="w-16 h-16 rounded-full bg-accent-red/10 flex items-center justify-center mb-2">
                <Trash2 size={32} className="text-accent-red" />
              </div>
              <h3 className="text-xl font-black uppercase italic tracking-wider text-accent-red">{t.purge_modal_title}</h3>
              <p className="text-xs text-text-muted mt-2">
                {t.purge_modal_desc.split('{0}')[0]}
                <span className="text-accent-red text-lg">01900</span>
                {t.purge_modal_desc.split('{0}')[1]}
              </p>
            </div>

            <div className="vanguard-flex-v vanguard-gap-small">
              <input 
                type="text" 
                placeholder={t.purge_input_placeholder.replace('{0}', '01900')}
                value={purgeCode}
                onChange={(e) => setPurgeCode(e.target.value)}
                className="w-full bg-black/40 border border-white/10 rounded-lg px-4 py-3 text-center text-lg font-black tracking-[0.5em] text-accent-red focus:border-accent-red/50 outline-none"
              />
              
              <div className="grid grid-cols-2 gap-3 mt-4">
                <button 
                  onClick={() => { setShowPurgeModal(false); setPurgeCode(''); }}
                  className="py-3 bg-white/5 border border-white/10 rounded-lg text-[10px] font-black uppercase tracking-widest hover:bg-white/10 transition-all"
                >
                  {t.purge_cancel_btn}
                </button>
                <button 
                  onClick={handlePurge}
                  disabled={isPurging || purgeCode !== '01900'}
                  className={`py-3 rounded-lg text-[10px] font-black uppercase tracking-widest transition-all 
                    ${isPurging || purgeCode !== '01900' ? 'bg-white/5 text-white/20 cursor-not-allowed' : 'bg-accent-red text-white hover:bg-accent-red/80 shadow-[0_0_15px_var(--accent-red)]'}`}
                >
                  {isPurging ? (t.lang === 'vi' ? 'ĐANG XÓA...' : 'PURGING...') : t.purge_confirm_btn}
                </button>
              </div>
            </div>
          </motion.div>
        </div>
      )}

      {/* Mode Description */}
      <div className={`p-4 rounded-xl border flex items-start gap-3 ${isVerifyOnly ? 'bg-accent-blue/5 border-accent-blue/10' : 'bg-accent-amber/5 border-accent-amber/10'}`}>
        <Cpu size={16} className={`mt-0.5 shrink-0 ${isVerifyOnly ? 'text-accent-blue' : 'text-accent-amber'}`} />
        <div className="vanguard-flex-v vanguard-gap-tiny">
          <span className={`text-[9px] font-black uppercase tracking-wider ${isVerifyOnly ? 'text-accent-blue' : 'text-accent-amber'}`}>
            {isVerifyOnly ? t.eco_mode : t.max_mining_mode}
          </span>
          <p className="text-[9px] text-text-muted leading-relaxed">
            {isVerifyOnly ? t.eco_mode_desc : t.max_mining_desc.replace('{0}', String(cpuIntensity))}
          </p>
        </div>
      </div>

      {/* [MINER V2] CPU INTENSITY CONTROL */}
      {!isVerifyOnly && (
        <div className="vanguard-flex-v vanguard-gap-tiny p-4 rounded-xl border bg-black/40 border-accent-amber/10">
          <div className="vanguard-flex-h justify-between items-center mb-1">
            <div className="vanguard-flex-h vanguard-gap-tiny items-center text-accent-amber">
              <Zap size={14} className="animate-pulse" />
              <span className="text-[10px] font-black uppercase tracking-widest">{t.cpu_intensity}</span>
            </div>
            <span className="text-xs font-black text-accent-amber">{cpuIntensity}%</span>
          </div>
          <input
            type="range"
            min="1"
            max="100"
            step="1"
            value={cpuIntensity}
            onChange={handleIntensityChange}
            onMouseUp={handleIntensityCommit}
            onTouchEnd={handleIntensityCommit}
            className="w-full accent-accent-amber h-1.5 bg-white/10 rounded-full appearance-none outline-none cursor-pointer"
          />
          <div className="vanguard-flex-h justify-between items-center mt-1 text-[8px] text-text-muted font-bold uppercase">
            <span>{t.standby_label}</span>
            <span>{t.overdrive_label}</span>
          </div>
        </div>
      )}

      {/* HASHRATE REALTIME CHART */}
      <div className="vanguard-flex-v vanguard-gap-small">
        <div className="vanguard-flex-h justify-between items-center">
          <div className="vanguard-flex-h vanguard-gap-small items-center">
            <TrendingUp size={14} className={isMining ? 'text-accent-amber' : 'text-text-muted'} />
            <span className="text-[9px] font-black uppercase tracking-widest text-text-muted">
              {hashrate > 0 ? t.hashrate : t.searching_network}
            </span>
          </div>
          <div className="vanguard-flex-h vanguard-gap-small items-center">
            {isMining && <div className="w-1.5 h-1.5 rounded-full bg-accent-amber animate-pulse" />}
            <span className={`text-sm font-black italic ${isMining ? 'text-accent-amber' : 'text-text-muted'}`}>
              {formatHashrate(hashrate)}
            </span>
          </div>
        </div>

        <div className="relative p-3 bg-black/40 border border-white/5 rounded-2xl overflow-hidden">
          {/* Background glow khi đang đào */}
          {isMining && (
            <motion.div
              className="absolute bottom-0 left-0 right-0 h-1/2 bg-gradient-to-t from-accent-amber/5 to-transparent"
              animate={{ opacity: [0.3, 0.6, 0.3] }}
              transition={{ duration: 3, repeat: Infinity }}
            />
          )}
          <canvas
            ref={canvasRef}
            width={600}
            height={120}
            className="w-full h-[120px] relative z-10"
            style={{ imageRendering: 'auto' }}
          />
        </div>

        {/* Hashrate Stats */}
        <div className="grid grid-cols-3 gap-3">
          <div className="p-3 bg-black/30 border border-white/5 rounded-xl">
            <span className="tactical-label text-[8px] block mb-1">{t.hashrate}</span>
            <span className={`text-xs font-black italic ${isMining ? 'text-accent-amber' : 'text-text-muted'}`}>
              {formatHashrate(hashrate)}
            </span>
          </div>
          <div className="p-3 bg-black/30 border border-white/5 rounded-xl">
            <span className="tactical-label text-[8px] block mb-1">{t.miner_status}</span>
            <span className={`text-xs font-black italic ${isMining ? 'text-accent-green' : 'text-text-muted'}`}>
              {isMining ? t.miner_active : t.miner_paused}
            </span>
          </div>
          <div className="p-3 bg-black/30 border border-white/5 rounded-xl">
            <span className="tactical-label text-[8px] block mb-1">{t.power_label}</span>
            <span className="text-xs font-black italic text-white">{isMining ? `${cpuIntensity}%` : '--'}</span>
          </div>
        </div>
      </div>
    </div>
  );
};

export default NodeControlPanel;
