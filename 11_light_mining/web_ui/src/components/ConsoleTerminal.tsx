import React, { useEffect, useRef } from 'react';
import { Terminal } from 'lucide-react';
import type { LogEntry } from '../api';
import type { Translations } from '../i18n/translations';

interface ConsoleTerminalProps {
  logs: LogEntry[];
  t: Translations;
}

const ConsoleTerminal: React.FC<ConsoleTerminalProps> = ({ logs, t }) => {
  const terminalRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <article className="bg-slate-900/80 backdrop-blur-md border border-slate-800 p-5 rounded-2xl flex flex-col gap-3 shadow-xl hover:border-sky-500/30 transition-all flex-1">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sky-400 font-mono font-bold text-xs">
          <Terminal size={16} />
          <span>{t.consoleOutput}</span>
        </div>
        <span className="text-[10px] font-bold text-green-400 uppercase flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />
          Live Terminal
        </span>
      </div>

      <div ref={terminalRef} className="bg-black/90 border border-slate-800 rounded-xl p-3 font-mono text-[11px] h-[170px] overflow-y-auto space-y-1.5 text-slate-300 custom-scrollbar">
        {logs && logs.length > 0 ? (
          logs.map((log, idx) => (
            <div key={idx} className="leading-relaxed">
              <span className="text-slate-600 mr-2">{log.time}</span>
              <span className={
                log.type === 'success' ? 'text-green-400 font-semibold' : 
                log.type === 'warn' ? 'text-amber-400 font-semibold' : 'text-sky-400'
              }>
                {log.text}
              </span>
            </div>
          ))
        ) : (
          <div className="text-slate-600 text-xs italic">Waiting for miner logs stream...</div>
        )}
      </div>
    </article>
  );
};

export default ConsoleTerminal;
