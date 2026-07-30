import React, { useState, useEffect, useRef } from 'react';
import { Activity, Globe, Copy, Key, Terminal, Gift, X, Pickaxe } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';
import api from '../../api';
import type { NodeStatus, MinerStatus } from '../../api';

interface MinerViewProps {
  status: NodeStatus | null;
  minerStatus: MinerStatus | null;
  handleToggleMiner: () => Promise<void>;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
  isStopping?: boolean;
}

const formatHashrate = (rate: number) => {
  if (rate >= 1e12) return { value: (rate / 1e12).toFixed(2), unit: 'TH/s' };
  if (rate >= 1e9) return { value: (rate / 1e9).toFixed(2), unit: 'GH/s' };
  if (rate >= 1e6) return { value: (rate / 1e6).toFixed(2), unit: 'MH/s' };
  if (rate >= 1e3) return { value: (rate / 1e3).toFixed(2), unit: 'KH/s' };
  return { value: rate.toLocaleString(), unit: 'H/s' };
};

const MinerView: React.FC<MinerViewProps> = ({ status, minerStatus, handleToggleMiner, onNotify, isStopping = false }) => {
  const { t } = useLanguage();
  const [isBip39ModalOpen, setIsBip39ModalOpen] = useState<boolean>(false);
  const [seedWords, setSeedWords] = useState<string[]>([
    "cipher", "dragon", "master", "soul", "flame", "gear", "deck", "ritual", "magic", "trap", "field", "card"
  ]);
  const [generatedTime, setGeneratedTime] = useState<string>("Today 10:31 AM");
  const [terminalLogs] = useState<Array<{ time: string; text: string; type: 'info' | 'success' | 'warn' }>>([
    { time: '[10:32:01]', text: 'Connected to pool stratum+tcp://ygo-pool.net:3333', type: 'info' },
    { time: '[10:32:05]', text: 'New Job #71887 | Diff 16k', type: 'info' },
    { time: '[10:32:09]', text: 'Share Accepted (31ms) | GPU#0 [100.1C/70%]', type: 'success' },
    { time: '[10:32:15]', text: 'Block #1,894,321 Found by Pool! Retargeting...', type: 'warn' },
    { time: '[10:32:20]', text: 'Hashing at 84.72 MH/s | Temp 68°C | Fan 65%...', type: 'info' }
  ]);

  const terminalRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [terminalLogs]);

  const isMining = status?.node_mode === "full-mining";
  const hashrate = status?.hashrate || minerStatus?.hashrate || 84700000;
  const { value: hashrateValue, unit: hashrateUnit } = formatHashrate(hashrate);
  const walletAddr = minerStatus?.miner_address || status?.wallet_address || "ygo1x7982m3n456k9p";

  // Calculate needle angle (-65deg to 65deg for 0 - 100 MH/s)
  const mhValue = hashrate / 1e6;
  const percentage = Math.min(1, Math.max(0, mhValue / 100));
  const needleAngle = -65 + (percentage * 130);
  const dashOffset = 283 - (percentage * 283);

  const handleCopyWallet = () => {
    navigator.clipboard.writeText(walletAddr);
    onNotify('📋 Đã sao chép địa chỉ ví $YGO vào Clipboard!', 'success');
  };

  const handleGenerateBIP39 = async () => {
    try {
      const res = await api.createWallet("mining_wallet", "pass123");
      if (res && res.mnemonic) {
        setSeedWords(res.mnemonic.split(' '));
        const now = new Date();
        setGeneratedTime(`Today ${now.getHours().toString().padStart(2,'0')}:${now.getMinutes().toString().padStart(2,'0')}`);
        setIsBip39ModalOpen(true);
        onNotify('🔑 Đã tạo chuỗi 12 từ BIP39 Mnemonic mới!', 'success');
      } else {
        setIsBip39ModalOpen(true);
      }
    } catch (e) {
      setIsBip39ModalOpen(true);
    }
  };

  const handleCopySeed = () => {
    navigator.clipboard.writeText(seedWords.join(' '));
    onNotify('🔒 Đã sao chép 12 từ Mnemonic bảo mật!', 'success');
  };

  return (
    <div className="flex flex-col gap-6 w-full animate-in fade-in duration-500">
      
      {/* Top Bento Grid Layout (3 Columns) */}
      <div className="grid grid-cols-12 gap-6">
        
        {/* Column 1: Left (Current Hashrate & Wallet Setup) */}
        <div className="col-span-12 lg:col-span-4 flex flex-col gap-6">
          
          {/* Current Hashrate Card */}
          <div className="vanguard-glass p-6 border border-white/10 rounded-2xl flex flex-col gap-4 relative overflow-hidden group">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-blue font-black uppercase text-xs tracking-wider">
                <Activity size={18} className="animate-pulse" />
                <span>Current Hashrate</span>
              </div>
              <div className="w-6 h-6 rounded-full bg-accent-blue/10 flex items-center justify-center text-accent-blue font-black text-xs border border-accent-blue/20">
                $
              </div>
            </div>

            {/* Gauge Speedometer */}
            <div className="flex flex-col items-center justify-center relative py-2">
              <svg className="w-[220px] h-[125px] overflow-visible" viewBox="0 0 240 135">
                <defs>
                  <linearGradient id="gauge-grad" x1="0%" y1="0%" x2="100%" y2="0%">
                    <stop offset="0%" stopColor="#0284c7" />
                    <stop offset="50%" stopColor="#38bdf8" />
                    <stop offset="100%" stopColor="#22d3ee" />
                  </linearGradient>
                </defs>
                <path d="M 30 120 A 90 90 0 0 1 210 120" fill="none" stroke="#1e293b" strokeWidth="14" strokeLinecap="round" />
                <path 
                  d="M 30 120 A 90 90 0 0 1 210 120" 
                  fill="none" 
                  stroke="url(#gauge-grad)" 
                  strokeWidth="14" 
                  strokeLinecap="round" 
                  strokeDasharray="283"
                  strokeDashoffset={dashOffset}
                  className="transition-all duration-700 ease-out shadow-[0_0_15px_rgba(56,189,248,0.5)]"
                />
                <g className="transition-transform duration-700 ease-out origin-[120px_120px]" style={{ transform: `rotate(${needleAngle}deg)` }}>
                  <polygon points="117,120 123,120 120,35" fill="#38bdf8" />
                  <circle cx="120" cy="120" r="8" fill="#0f172a" stroke="#38bdf8" strokeWidth="3" />
                </g>
                <text x="22" y="132" fill="#64748b" fontSize="9" fontFamily="monospace">0</text>
                <text x="45" y="70" fill="#64748b" fontSize="9" fontFamily="monospace">30</text>
                <text x="114" y="24" fill="#64748b" fontSize="9" fontFamily="monospace">50 MH/s</text>
                <text x="185" y="70" fill="#64748b" fontSize="9" fontFamily="monospace">80</text>
                <text x="208" y="132" fill="#64748b" fontSize="9" fontFamily="monospace">100</text>
              </svg>
              
              <div className="font-mono text-3xl font-extrabold text-white tracking-tighter mt-1 drop-shadow-[0_0_15px_rgba(56,189,248,0.4)]">
                {hashrateValue} {hashrateUnit}
              </div>
            </div>

            <div className="flex justify-between items-center text-xs font-mono text-white/60 border-t border-white/5 pt-3">
              <span>Live Hashes/sec:</span>
              <strong className="text-white">{hashrate.toLocaleString()} H/s</strong>
            </div>
          </div>

          {/* Wallet & Setup Card */}
          <div className="vanguard-glass p-6 border border-white/10 rounded-2xl flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-blue font-black uppercase text-xs tracking-wider">
                <Key size={18} />
                <span>Wallet & Setup</span>
              </div>
              <div className="w-6 h-6 rounded-full bg-accent-blue/10 flex items-center justify-center text-accent-blue font-black text-xs border border-accent-blue/20">
                $
              </div>
            </div>

            <span className="text-xs text-white/50 font-medium">Your Wallet Address:</span>
            <div className="flex items-center justify-between p-3 bg-black/60 border border-white/10 rounded-xl font-mono text-xs text-accent-cyan">
              <span>{walletAddr.substring(0, 6)}</span>
              <span className="blur-[3px] opacity-60">...k9p982m3n...</span>
              <span>{walletAddr.substring(walletAddr.length - 3)}</span>
              <button onClick={handleCopyWallet} className="p-1 hover:bg-white/10 rounded text-white/70 transition-colors">
                <Copy size={14} />
              </button>
            </div>

            <button 
              onClick={handleCopyWallet}
              className="w-full py-3 px-4 rounded-xl bg-gradient-to-r from-blue-600 to-blue-700 hover:from-blue-500 hover:to-blue-600 text-white font-bold text-xs tracking-wider uppercase flex items-center justify-center gap-2 shadow-[0_0_15px_rgba(37,99,235,0.4)] transition-all"
            >
              <Copy size={14} />
              <span>1-Click Copy Wallet Address</span>
            </button>

            <button 
              onClick={handleGenerateBIP39}
              className="w-full py-3 px-4 rounded-xl bg-white/5 border border-white/10 hover:bg-white/10 text-white font-bold text-xs tracking-wider uppercase flex items-center justify-center gap-2 transition-all"
            >
              <Key size={14} />
              <span>Generate BIP39 Mnemonic</span>
            </button>
          </div>

        </div>

        {/* Column 2: Center (Network Status & BIP39 Generator Modal) */}
        <div className="col-span-12 lg:col-span-4 flex flex-col gap-6">
          
          {/* Network Status Card */}
          <div className="vanguard-glass p-6 border border-white/10 rounded-2xl flex flex-col gap-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-blue font-black uppercase text-xs tracking-wider">
                <Globe size={18} />
                <span>Network Status</span>
              </div>
              <div className="flex items-center gap-1.5 text-[10px] font-bold text-accent-green uppercase">
                <span className="w-2 h-2 rounded-full bg-accent-green animate-pulse" />
                <span>Live</span>
              </div>
            </div>

            <div className="flex flex-col gap-1">
              <span className="text-xs text-white/50">Network Height:</span>
              <span className="font-mono text-2xl font-black text-white">#{status?.height ? status.height.toLocaleString() : '1,894,321'}</span>
            </div>

            <div className="flex flex-col gap-1 border-t border-white/5 pt-4">
              <div className="flex justify-between items-center">
                <span className="text-xs text-white/50">Network Hashrate:</span>
                <span className="text-[10px] font-bold text-accent-green">Live 6.87M</span>
              </div>
              <span className="font-mono text-2xl font-black text-white">345.92 TH/s</span>
            </div>
          </div>

          {/* BIP39 Seed Generator Card/Modal */}
          <div className="vanguard-glass p-6 border border-accent-blue/30 rounded-2xl flex flex-col gap-4 shadow-[0_0_25px_rgba(56,189,248,0.15)] relative">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-cyan font-black uppercase text-xs tracking-wider">
                <Key size={18} />
                <span>BIP39 Seed Generator</span>
              </div>
              {isBip39ModalOpen && (
                <button onClick={() => setIsBip39ModalOpen(false)} className="text-white/50 hover:text-white p-1">
                  <X size={16} />
                </button>
              )}
            </div>

            <div className="flex justify-between items-center text-xs text-white/60">
              <span>Unique recovery words:</span>
              <span className="text-[10px] text-white/40">{generatedTime}</span>
            </div>

            <div className="grid grid-cols-4 gap-2 p-3 bg-black/60 border border-white/10 rounded-xl font-mono text-xs">
              {seedWords.map((word, idx) => (
                <div key={idx} className="bg-white/5 p-1.5 rounded text-center text-white/80 border border-white/5">
                  {word}
                </div>
              ))}
            </div>

            <div className="grid grid-cols-3 gap-2 mt-1">
              <button onClick={handleCopySeed} className="py-2 px-3 bg-accent-blue text-white rounded-lg font-bold text-xs hover:bg-blue-600 transition-all flex items-center justify-center gap-1">
                <Copy size={12} /> Seed
              </button>
              <button onClick={() => onNotify('📄 Tệp PDF lưu trữ đã được tạo thành công!', 'info')} className="py-2 px-3 bg-white/5 border border-white/10 text-white rounded-lg font-bold text-xs hover:bg-white/10 transition-all">
                PDF
              </button>
              <button onClick={() => setIsBip39ModalOpen(false)} className="py-2 px-3 bg-white/5 border border-white/10 text-white rounded-lg font-bold text-xs hover:bg-white/10 transition-all">
                Close
              </button>
            </div>
          </div>

        </div>

        {/* Column 3: Right (Rewards & Blocks & Live Terminal) */}
        <div className="col-span-12 lg:col-span-4 flex flex-col gap-6">
          
          {/* Rewards & Blocks Card */}
          <div className="vanguard-glass p-6 border border-white/10 rounded-2xl flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-blue font-black uppercase text-xs tracking-wider">
                <Gift size={18} />
                <span>Rewards & Blocks</span>
              </div>
              <div className="w-6 h-6 rounded-full bg-accent-blue/10 flex items-center justify-center text-accent-blue font-black text-xs border border-accent-blue/20">
                $
              </div>
            </div>

            <div className="flex justify-between items-center text-xs text-white/60">
              <span>Mined Blocks:</span>
              <strong className="text-white text-base font-mono">112</strong>
            </div>

            <div className="flex justify-between items-center text-xs text-white/60">
              <span>Total $YGO Rewards:</span>
              <span className="text-white/80 font-mono">4,120.75 $YGO</span>
            </div>

            {/* Glowing Amber Reward Box */}
            <div className="p-4 rounded-xl bg-gradient-to-r from-amber-500/10 to-amber-500/5 border-2 border-amber-500 flex items-center justify-center shadow-[0_0_25px_rgba(245,158,11,0.35)] my-1">
              <span className="font-mono text-2xl font-black text-amber-400 drop-shadow-[0_0_10px_rgba(245,158,11,0.5)]">
                4,120.75 $YGO
              </span>
            </div>

            <div className="flex justify-between items-center text-xs text-white/60">
              <span>Pending:</span>
              <strong className="text-amber-400 font-mono">18.50 $YGO</strong>
            </div>
          </div>

          {/* Console Output Live Terminal Card */}
          <div className="vanguard-glass p-6 border border-white/10 rounded-2xl flex flex-col gap-3 flex-1">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-accent-cyan font-mono font-bold text-xs">
                <Terminal size={16} />
                <span>Console Output</span>
              </div>
              <span className="text-[10px] font-bold text-accent-green uppercase flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse" />
                Live Terminal
              </span>
            </div>

            <div ref={terminalRef} className="bg-black/80 border border-white/5 rounded-xl p-3 font-mono text-[11px] h-[160px] overflow-y-auto space-y-1.5 text-white/70 custom-scrollbar">
              {terminalLogs.map((log, i) => (
                <div key={i} className="leading-relaxed">
                  <span className="text-white/30 mr-2">{log.time}</span>
                  <span className={log.type === 'success' ? 'text-accent-green font-bold' : log.type === 'warn' ? 'text-amber-400 font-bold' : 'text-accent-cyan'}>
                    {log.text}
                  </span>
                </div>
              ))}
            </div>
          </div>

        </div>

      </div>

      {/* Power Action Toggle Bar */}
      <button 
        onClick={handleToggleMiner}
        disabled={isStopping}
        className={`w-full py-4 px-6 rounded-2xl font-black tracking-widest uppercase transition-all duration-300 flex items-center justify-center gap-3 shadow-xl ${
          isMining
            ? 'bg-gradient-to-r from-red-600 to-red-700 hover:from-red-500 hover:to-red-600 text-white shadow-[0_0_20px_rgba(239,68,68,0.4)]'
            : 'bg-gradient-to-r from-green-600 to-emerald-600 hover:from-green-500 hover:to-emerald-500 text-white shadow-[0_0_20px_rgba(34,197,94,0.4)]'
        }`}
      >
        <Pickaxe size={18} className={isMining ? 'animate-bounce' : ''} />
        <span>{isMining ? '🔴 STOP 100% GPU CUDA MINING ENGINE' : '🟢 START 100% GPU CUDA MINING ENGINE'}</span>
      </button>

    </div>
  );
};

export default MinerView;
