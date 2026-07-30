import React, { useEffect, useRef } from 'react';
import { Globe } from 'lucide-react';
import type { Translations } from '../i18n/translations';

interface NetworkStatusProps {
  networkHeight: number;
  networkHashrate: number;
  t: Translations;
}

const formatHashrate = (hr: number) => {
  if (!hr || hr === 0) return '0 H/s';
  if (hr >= 1e12) return (hr / 1e12).toFixed(2) + ' TH/s';
  if (hr >= 1e9) return (hr / 1e9).toFixed(2) + ' GH/s';
  if (hr >= 1e6) return (hr / 1e6).toFixed(2) + ' MH/s';
  if (hr >= 1e3) return (hr / 1e3).toFixed(2) + ' KH/s';
  return hr.toLocaleString() + ' H/s';
};

const NetworkStatus: React.FC<NetworkStatusProps> = ({ networkHeight, networkHashrate, t }) => {
  const canvasHeightRef = useRef<HTMLCanvasElement>(null);
  const canvasHashRef = useRef<HTMLCanvasElement>(null);

  const initSparkline = (canvas: HTMLCanvasElement | null, color: string) => {
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const width = canvas.width = canvas.parentElement?.clientWidth || 240;
    const height = canvas.height = canvas.parentElement?.clientHeight || 38;
    let points = Array.from({ length: 30 }, () => Math.random() * 0.3 + 0.1);

    const draw = () => {
      ctx.clearRect(0, 0, width, height);
      ctx.beginPath();
      ctx.strokeStyle = color;
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
      grad.addColorStop(0, color.replace('rgb', 'rgba').replace(')', ', 0.25)'));
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
  };

  useEffect(() => {
    const c1 = initSparkline(canvasHeightRef.current, 'rgb(34, 197, 94)');
    const c2 = initSparkline(canvasHashRef.current, 'rgb(56, 189, 248)');
    return () => {
      c1 && c1();
      c2 && c2();
    };
  }, []);

  const isConnected = networkHeight > 0;

  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-4 shadow-xl hover:border-sky-500/30 transition-all flex-1">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-bold uppercase text-xs tracking-wider">
          <Globe size={18} />
          <span>{t.networkStatus}</span>
        </div>
        <div className={`text-[10px] font-bold uppercase flex items-center gap-1.5 px-2.5 py-1 rounded-full border ${
          isConnected 
            ? 'text-green-400 bg-green-500/10 border-green-500/30' 
            : 'text-red-400 bg-red-500/10 border-red-500/30'
        }`}>
          <span className={`w-2 h-2 rounded-full ${
            isConnected ? 'bg-green-500 animate-pulse' : 'bg-red-500 animate-ping'
          }`} />
          <span>{isConnected ? '🟢 VPS Live (110.172.28.103:28888)' : '🔴 Mất kết nối VPS (110.172.28.103:28888)'}</span>
        </div>
      </div>

      <div className="flex flex-col gap-1 mt-1">
        <span className="text-xs text-slate-400 font-medium">{t.networkHeight}</span>
        <div className="flex items-center gap-2">
          <span className={`font-mono text-2xl font-black ${isConnected ? 'text-white' : 'text-red-400'}`}>
            #{networkHeight ? networkHeight.toLocaleString() : '0'}
          </span>
          {!isConnected && (
            <span className="text-[10px] font-bold bg-red-500/20 text-red-400 px-2 py-0.5 rounded border border-red-500/30 uppercase">
              Chưa kết nối được Node
            </span>
          )}
        </div>
        <div className="w-full h-9">
          <canvas ref={canvasHeightRef} />
        </div>
      </div>

      <div className="flex flex-col gap-1 border-t border-slate-800 pt-3">
        <div className="flex justify-between items-center">
          <span className="text-xs text-slate-400 font-medium">{t.networkHashrate}</span>
          <span className={`text-[10px] font-bold ${isConnected ? 'text-green-400' : 'text-slate-500'}`}>
            {isConnected ? 'Live VPS' : 'Offline'}
          </span>
        </div>
        <span className="font-mono text-2xl font-black text-white">
          {formatHashrate(networkHashrate)}
        </span>
        <div className="w-full h-9">
          <canvas ref={canvasHashRef} />
        </div>
      </div>
    </article>
  );
};

export default NetworkStatus;
