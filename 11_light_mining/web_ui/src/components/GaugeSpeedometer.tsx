import React, { useEffect, useRef } from 'react';
import { Activity } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface GaugeSpeedometerProps {
  hashrate: number;
  t: Translations;
}

const formatHashrate = (hr: number) => {
  if (!hr || hr === 0) return { val: '0 H/s', raw: 0 };
  if (hr >= 1e12) return { val: (hr / 1e12).toFixed(2) + ' TH/s', raw: hr };
  if (hr >= 1e9) return { val: (hr / 1e9).toFixed(2) + ' GH/s', raw: hr };
  if (hr >= 1e6) return { val: (hr / 1e6).toFixed(2) + ' MH/s', raw: hr };
  if (hr >= 1e3) return { val: (hr / 1e3).toFixed(2) + ' KH/s', raw: hr };
  return { val: hr.toLocaleString() + ' H/s', raw: hr };
};

const GaugeSpeedometer: React.FC<GaugeSpeedometerProps> = ({ hashrate, t }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const mhValue = hashrate / 1000000;
  const percentage = Math.min(1, Math.max(0, mhValue / 100));
  const dashOffset = 283 - (percentage * 283);
  const needleAngle = -65 + (percentage * 130);
  const formatted = formatHashrate(hashrate);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const width = canvas.width = canvas.parentElement?.clientWidth || 240;
    const height = canvas.height = canvas.parentElement?.clientHeight || 45;

    let points = Array.from({ length: 30 }, () => Math.random() * 0.3 + 0.1);

    const draw = () => {
      ctx.clearRect(0, 0, width, height);
      ctx.beginPath();
      ctx.strokeStyle = '#38bdf8';
      ctx.lineWidth = 2;

      const step = width / (points.length - 1);
      points.forEach((pt, i) => {
        const x = i * step;
        const y = height - (pt * height);
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      });
      ctx.stroke();

      ctx.lineTo(width, height);
      ctx.lineTo(0, height);
      ctx.closePath();
      const grad = ctx.createLinearGradient(0, 0, 0, height);
      grad.addColorStop(0, 'rgba(56, 189, 248, 0.25)');
      grad.addColorStop(1, 'rgba(0, 0, 0, 0)');
      ctx.fillStyle = grad;
      ctx.fill();
    };

    draw();
    const interval = setInterval(() => {
      points.shift();
      points.push(Math.min(0.95, Math.max(0.05, points[points.length - 1] + (Math.random() - 0.5) * 0.15)));
      draw();
    }, 2000);

    return () => clearInterval(interval);
  }, []);

  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-3 shadow-xl hover:border-sky-500/30 transition-all">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
          <Activity size={18} className="animate-pulse" />
          <span>{t.currentHashrate}</span>
        </div>
        <div className="w-5 h-5 rounded-full bg-sky-500/10 flex items-center justify-center text-sky-400 font-bold text-xs border border-sky-500/20">
          $
        </div>
      </div>

      <div className="flex flex-col items-center justify-center relative py-1">
        <svg className="gauge-svg-fixed" viewBox="0 0 240 135">
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
            className="transition-all duration-700 ease-out"
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

        <div className="font-mono text-2xl font-black text-white tracking-tighter mt-1 drop-shadow-[0_0_12px_rgba(56,189,248,0.4)]">
          {formatted.val}
        </div>
      </div>

      <div className="w-full h-10">
        <canvas ref={canvasRef} />
      </div>

      <div className="flex justify-between items-center text-xs font-mono text-slate-400 border-t border-slate-800 pt-3">
        <span>{t.liveHashes}</span>
        <strong className="text-white">{hashrate.toLocaleString()} H/s</strong>
      </div>
    </article>
  );
};

export default GaugeSpeedometer;
