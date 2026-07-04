import React from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Network, Database, Server } from 'lucide-react';

interface SyncOverlayProps {
  sync: { 
    current: number; 
    target: number; 
    state: string;
    snapshot_chunks_loaded?: number;
    snapshot_chunks_total?: number;
    executing?: boolean;
    downloading?: number;
  } | null;
}

import { useLanguage } from '../../LanguageContext';

const SyncOverlay: React.FC<SyncOverlayProps> = ({ sync }) => {
  const { t } = useLanguage();
  
  if (!sync || sync.state === 'STREAMING' || sync.state === 'SYNCED') return null;

  const isSnapshot = sync.state === 'SNAPSHOT_SYNC' || sync.state === 'BOOTSTRAPPING';
  const isExecuting = sync.executing === true;
  const hasDownloading = sync.downloading !== undefined && sync.downloading > 0;
  const progress = isSnapshot && sync.snapshot_chunks_total && sync.snapshot_chunks_total > 0
    ? (sync.snapshot_chunks_loaded || 0) / sync.snapshot_chunks_total * 100
    : (isExecuting 
        ? (sync.target > 0 ? (sync.current / sync.target) * 100 : 0) 
        : (hasDownloading ? (sync.target > 0 ? (sync.downloading! / sync.target) * 100 : 0) : 0));
  const isInitial = sync.state === 'WAITING_FOR_PEERS' || (!isSnapshot && sync.target === 0);

  const renderContent = () => {
    if (isInitial) {
      return (
        <div className="flex flex-col items-center">
          <motion.div
            animate={{ rotate: 360 }}
            transition={{ repeat: Infinity, duration: 2, ease: "linear" }}
            className="mb-8"
          >
            <Network size={64} className="text-accent-blue opacity-80" />
          </motion.div>
          <h2 className="text-2xl font-black italic text-white uppercase tracking-widest mb-2">
            {t.sync_scanning_network}
          </h2>
          <p className="text-sm text-text-muted mt-2 max-w-md text-center">
            {t.sync_connecting}
          </p>
        </div>
      );
    }

    const Icon = isSnapshot ? Database : Server;
    const accentColor = isSnapshot ? 'text-accent-green' : 'text-accent-amber';
    const barColor = isSnapshot ? 'bg-accent-green' : 'bg-accent-amber';

    let titleText = t.sync_header_title;
    let descText = t.sync_header_desc;
    if (sync.state === 'SYNCING') {
      if (isExecuting) {
        titleText = t.sync_stage_executing;
        descText = `${t.sync_est_time}: Block ${sync.current.toLocaleString()} / ${sync.target.toLocaleString()}`;
      } else {
        titleText = t.sync_stage_downloading;
        descText = hasDownloading
          ? `ĐANG TẢI: KHỐI ${sync.downloading!.toLocaleString()} / ${sync.target.toLocaleString()}`
          : t.sync_fetching_ledger;
      }
    } else if (isSnapshot) {
      titleText = t.sync_snapshot_title;
      descText = t.sync_snapshot_desc;
    }

    const isDownloadingPhase = !isSnapshot && sync.state === 'SYNCING' && !isExecuting;

    return (
      <div className="flex flex-col items-center w-full max-w-xl">
        <Icon size={64} className={`${accentColor} mb-8 opacity-80`} />
        <h2 className="text-2xl font-black italic text-white uppercase tracking-widest mb-2 text-center px-6">
          {titleText}
        </h2>
        <p className={`text-sm ${isSnapshot ? 'text-accent-green/80' : 'text-text-muted'} mt-2 mb-8 text-center max-w-md uppercase font-bold tracking-tighter`}>
          {descText}
        </p>
        
        <div className="w-full bg-black/50 border border-white/10 rounded-full h-4 overflow-hidden relative">
          <motion.div
            className={`absolute inset-y-0 left-0 ${barColor} rounded-full`}
            animate={{ width: `${progress}%` }}
            transition={{ ease: "linear" }}
          />
          {isDownloadingPhase && (
            <motion.div 
              className="absolute inset-y-0 w-1/3 bg-gradient-to-r from-transparent via-white/20 to-transparent rounded-full"
              animate={{ x: ["-100%", "300%"] }}
              transition={{ repeat: Infinity, duration: 1.5, ease: "linear" }}
            />
          )}
          <div className="absolute inset-0 bg-[url('data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAiIGhlaWdodD0iMjAiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+PHBhdGggZD0iTTAgMjBMMjAgMEwyMCAyMEgweiIgZmlsbD0iI2ZmZiIgZmlsbC1vcGFjaXR5PSIuMSIvPjwvc3ZnPg==')] opacity-30 animate-[slide_1s_linear_infinite]" />
        </div>

        <div className={`w-full flex justify-between mt-3 px-1 text-[10px] font-black ${accentColor} uppercase tracking-widest`}>
          <span>
            {isSnapshot && sync.snapshot_chunks_total && sync.snapshot_chunks_total > 0
              ? `MẢNH: ${sync.snapshot_chunks_loaded?.toLocaleString() || 0} / ${sync.snapshot_chunks_total.toLocaleString()}`
              : (isDownloadingPhase 
                  ? (hasDownloading ? `Đang tải: ${sync.downloading?.toLocaleString()} / ${sync.target.toLocaleString()}` : `Đang kết nối tải khối...`)
                  : `BLOCK: ${sync.current.toLocaleString()} / ${sync.target.toLocaleString()}`
                )
            }
          </span>
          <span>
            {isDownloadingPhase && !hasDownloading ? "LOADING..." : `${Math.floor(progress)}%`}
          </span>
        </div>
      </div>
    );
  };

  return (
    <AnimatePresence>
      <motion.div
        key="sync-overlay"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        className="fixed inset-0 z-[1000] bg-vanguard-bg/95 backdrop-blur-3xl flex items-center justify-center pointer-events-auto"
      >
        {/* Background Gradients */}
        <div className="absolute inset-0 overflow-hidden pointer-events-none">
          <div className="absolute top-[-20%] left-[-10%] w-[50%] h-[50%] bg-accent-blue/10 rounded-full blur-[150px]" />
          <div className="absolute bottom-[-20%] right-[-10%] w-[60%] h-[60%] bg-[#8B5CF6]/10 rounded-full blur-[150px]" />
        </div>

        <div className="relative z-10 w-full flex justify-center">
          <AnimatePresence mode="wait">
            <motion.div
              key={sync.state}
              initial={{ opacity: 0, y: 30, scale: 0.95 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: -30, scale: 0.95 }}
              transition={{ duration: 0.4 }}
              className="flex justify-center flex-col items-center w-full"
            >
              {renderContent()}
            </motion.div>
          </AnimatePresence>
        </div>
      </motion.div>
    </AnimatePresence>
  );
};

export default SyncOverlay;
