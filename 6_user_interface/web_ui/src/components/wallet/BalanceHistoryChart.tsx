import React, { useMemo, useState, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Crosshair, Activity, Clock } from 'lucide-react';
import type { Transaction } from '../../api';

interface BalanceHistoryChartProps {
  transactions: Transaction[];
  address: string;
  height?: number;
}

const BalanceHistoryChart: React.FC<BalanceHistoryChartProps> = ({ transactions, address, height = 240 }) => {
  const [hoveredPoint, setHoveredPoint] = useState<{ x: number, y: number, bal: number, time: string, delta: number } | null>(null);
  const chartRef = useRef<SVGSVGElement>(null);

  const normalize = (addr: string) => addr?.toLowerCase().trim().replace(/^0x/, '');
  const activeAddr = useMemo(() => normalize(address), [address]);
  
  const isSameAddress = (a: string, b: string) => normalize(a) === normalize(b);

  const chartData = useMemo(() => {
    const relevantTxs = transactions
      .filter(tx => isSameAddress(tx.sender, activeAddr) || isSameAddress(tx.receiver, activeAddr))
      .sort((a, b) => a.height - b.height || a.timestamp - b.timestamp);

    if (relevantTxs.length === 0) return [];

    let current = 0;
    const points: any[] = [];
    
    // Initial zero point for better visual flow
    points.push({ x: 0, y: 0, bal: 0, time: "GENESIS", delta: 0 });

    relevantTxs.forEach((tx, idx) => {
      const isOut = isSameAddress(tx.sender, activeAddr);
      const amount = (Number(tx.amount) || 0) / 100_000_000;
      const prevBal = current;
      
      if (isOut) current -= amount;
      else current += amount;

      points.push({ 
        x: idx + 1, 
        y: current, 
        bal: current, 
        time: tx.timestamp > 0 ? new Date(tx.timestamp * 1000).toLocaleTimeString() : "PENDING",
        delta: current - prevBal
      });
    });

    return points;
  }, [transactions, address]);

  if (chartData.length < 2) {
    return (
      <div className="h-[240px] flex flex-col items-center justify-center border border-white/5 bg-black/60 rounded-[2.5rem] group overflow-hidden relative shadow-2xl">
        {/* Active Radar Animation */}
        <div className="absolute inset-0 flex items-center justify-center opacity-20">
            <motion.div 
                animate={{ scale: [1, 1.5, 2], opacity: [0.5, 0.2, 0] }}
                transition={{ repeat: Infinity, duration: 4, ease: "easeOut" }}
                className="absolute w-32 h-32 border border-accent-blue rounded-full" 
            />
            <motion.div 
                animate={{ scale: [1, 1.5, 2], opacity: [0.5, 0.2, 0] }}
                transition={{ repeat: Infinity, duration: 4, ease: "easeOut", delay: 1.5 }}
                className="absolute w-32 h-32 border border-accent-blue rounded-full" 
            />
            <motion.div 
                animate={{ rotate: 360 }}
                transition={{ repeat: Infinity, duration: 3, ease: "linear" }}
                className="absolute w-64 h-64 border-t-2 border-accent-blue/30 rounded-full"
            />
        </div>
        
        <Activity size={32} className="text-accent-blue/40 mb-4 animate-pulse relative z-10" />
        <div className="vanguard-flex-v items-center vanguard-gap-tiny relative z-10">
            <span className="text-[10px] font-black uppercase text-accent-blue tracking-[0.4em] font-mono italic animate-pulse">
            [ RADAR_SCANNING_ACTIVE ]
            </span>
            <span className="text-[7px] font-bold text-white/20 uppercase tracking-widest">Awaiting_Genesis_Chain_Data</span>
        </div>
        
        {/* Matrix background for empty state */}
        <div className="absolute inset-0 opacity-5 pointer-events-none" 
             style={{ backgroundImage: 'linear-gradient(rgba(0, 136, 255, 0.2) 1px, transparent 1px), linear-gradient(90deg, rgba(0, 136, 255, 0.2) 1px, transparent 1px)', backgroundSize: '20px 20px' }} />
      </div>
    );
  }

  const minBal = Math.min(...chartData.map(p => p.bal));
  const maxBal = Math.max(...chartData.map(p => p.bal));
  const range = (maxBal - minBal) || 1;
  const width = 800; // SVG internal width
  const paddingX = 40;
  const paddingY = 40;

  const getX = (index: number) => paddingX + (index / (chartData.length - 1)) * (width - 2 * paddingX);
  const getY = (val: number) => height - paddingY - ((val - minBal) / range) * (height - 2 * paddingY);

  const pathD = chartData.map((p, i) => `${i === 0 ? 'M' : 'L'} ${getX(i)} ${getY(p.y)}`).join(' ');
  const areaD = `${pathD} L ${getX(chartData.length - 1)} ${height} L ${getX(0)} ${height} Z`;

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!chartRef.current) return;
    const rect = chartRef.current.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * width;
    
    // Find closest point
    let closest = chartData[0];
    let minDist = Math.abs(getX(0) - x);
    
    chartData.forEach((p, i) => {
        const d = Math.abs(getX(i) - x);
        if (d < minDist) {
            minDist = d;
            closest = { ...p, px: getX(i), py: getY(p.y) };
        }
    });

    setHoveredPoint(closest);
  };

  return (
    <div className="relative group p-4 bg-black/40 border border-white/[0.05] rounded-3xl overflow-hidden glass-card">
      {/* Background Grid */}
      <div className="absolute inset-0 opacity-10 pointer-events-none" 
           style={{ backgroundImage: 'radial-gradient(circle, #fff 1px, transparent 1px)', backgroundSize: '30px 30px' }} />

      <svg 
        ref={chartRef}
        viewBox={`0 0 ${width} ${height}`} 
        className="w-full h-auto overflow-visible select-none cursor-crosshair"
        preserveAspectRatio="none"
        onMouseMove={handleMouseMove}
        onMouseLeave={() => setHoveredPoint(null)}
      >
        <defs>
          <linearGradient id="glowGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="rgba(0, 136, 255, 0.4)" />
            <stop offset="100%" stopColor="rgba(0, 136, 255, 0)" />
          </linearGradient>
          <filter id="neonBlur" x="-20%" y="-20%" width="140%" height="140%">
            <feGaussianBlur stdDeviation="4" result="blur" />
            <feComposite in="SourceGraphic" in2="blur" operator="over" />
          </filter>
        </defs>

        {/* Scanline Shadow Grid */}
        {[0, 0.25, 0.5, 0.75, 1].map(v => (
            <line 
                key={v}
                x1={paddingX} y1={paddingY + v * (height - 2 * paddingY)} 
                x2={width - paddingX} y2={paddingY + v * (height - 2 * paddingY)}
                stroke="rgba(255,255,255,0.05)" strokeWidth="1"
            />
        ))}

        {/* Area fill */}
        <motion.path 
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          d={areaD} 
          fill="url(#glowGradient)" 
        />

        {/* Main Neon Line */}
        <motion.path
          initial={{ pathLength: 0, opacity: 0 }}
          animate={{ pathLength: 1, opacity: 1 }}
          transition={{ duration: 2, ease: "easeInOut" }}
          d={pathD}
          fill="none"
          stroke="#0088ff"
          strokeWidth="3"
          strokeLinecap="round"
          strokeLinejoin="round"
          filter="url(#neonBlur)"
        />

        {/* Pulse at the end */}
        <motion.circle 
            cx={getX(chartData.length - 1)} 
            cy={getY(chartData[chartData.length - 1].y)} 
            r="6" 
            fill="#0088ff"
            initial={{ scale: 1, opacity: 0.5 }}
            animate={{ scale: [1, 2, 1], opacity: [0.5, 0, 0.5] }}
            transition={{ repeat: Infinity, duration: 2 }}
        />

        {/* Hover Crosshair / Scanner */}
        <AnimatePresence>
            {hoveredPoint && (
                <>
                    <motion.line 
                        initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
                        x1={getX(hoveredPoint.x)} y1={paddingY}
                        x2={getX(hoveredPoint.x)} y2={height - paddingY}
                        stroke="#0088ff" strokeWidth="1" strokeDasharray="4 2"
                    />
                    <motion.circle 
                        initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }}
                        cx={getX(hoveredPoint.x)} cy={getY(hoveredPoint.y)} r="5"
                        fill="white" stroke="#0088ff" strokeWidth="2"
                    />
                </>
            )}
        </AnimatePresence>
      </svg>
      
      {/* HUD Tooltip Overlay */}
      <AnimatePresence>
        {hoveredPoint && (
            <motion.div 
                initial={{ opacity: 0, x: 10 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0 }}
                className="absolute right-8 top-8 vanguard-flex-v p-4 bg-black/60 border border-accent-blue/40 rounded-2xl backdrop-blur-md shadow-2xl pointer-events-none z-50"
            >
                <div className="vanguard-flex-h vanguard-gap-small items-center mb-2">
                    <div className="w-2 h-2 rounded-full bg-accent-blue animate-pulse" />
                    <span className="text-[10px] font-black text-white italic tracking-[0.1em] uppercase">Tactical_Stats_HUD</span>
                </div>
                <div className="vanguard-grid-2 vanguard-gap-medium">
                    <div className="vanguard-flex-v">
                        <span className="text-[7px] font-bold text-accent-blue/60 uppercase">TIMESTAMP</span>
                        <div className="vanguard-flex-h vanguard-gap-tiny items-center">
                            <Clock size={8} className="text-white/40" />
                            <span className="text-[11px] font-black text-white italic">{hoveredPoint.time}</span>
                        </div>
                    </div>
                    <div className="vanguard-flex-v">
                        <span className="text-[7px] font-bold text-accent-blue/60 uppercase">DELTA_INTEL</span>
                        <span className={`text-[11px] font-black italic ${hoveredPoint.delta >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
                            {hoveredPoint.delta >= 0 ? '+' : ''}{hoveredPoint.delta.toFixed(4)}
                        </span>
                    </div>
                </div>
                <div className="mt-3 pt-3 border-t border-white/10 flex flex-col">
                    <span className="text-[7px] font-bold text-accent-blue/60 uppercase">ACCUMULATED_BALANCE</span>
                    <span className="text-2xl font-black text-white italic drop-shadow-[0_0_15px_rgba(255,255,255,0.2)]">
                        {hoveredPoint.bal.toFixed(8)} <span className="text-[10px] opacity-20">GO</span>
                    </span>
                </div>
            </motion.div>
        )}
      </AnimatePresence>

      {/* Floating Info Labels */}
      <div className="absolute bottom-6 left-8 vanguard-flex-h vanguard-gap-medium opacity-40">
        <div className="vanguard-flex-h vanguard-gap-tiny items-center">
            <Activity size={10} className="text-accent-blue" />
            <span className="text-[8px] font-black uppercase tracking-widest">REALTIME_LEDGER_FEED</span>
        </div>
        <div className="vanguard-flex-h vanguard-gap-tiny items-center">
            <Crosshair size={10} className="text-accent-amber" />
            <span className="text-[8px] font-black uppercase tracking-widest">HUD_ACTIVE</span>
        </div>
      </div>
    </div>
  );
};

export default BalanceHistoryChart;
