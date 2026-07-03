import { useState, useEffect, useRef } from 'react';
import { useLanguage } from '../../LanguageContext';
import { User, Bell, Settings, Globe, Trash2, Database, Power } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import api from '../../api';

interface HeaderProps {
  title: string;
  height: number;
  syncState?: string;
  syncProgress?: number; // [VANGUARD-UI] Thêm phần trăm đồng bộ
  peerCount?: number;
  address?: string;
  isOffline?: boolean;
  targetHeight?: number; // [VANGUARD-UI] Thêm đỉnh mạng lưới
}

const Header: React.FC<HeaderProps> = ({ title, height, syncState = 'SYNCED', syncProgress = 0, peerCount = 0, address = '', isOffline = false, targetHeight = 0 }) => {
  const { t, lang, setLang } = useLanguage();
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const [showPurgeModal, setShowPurgeModal] = useState(false);
  const [purgeCode, setPurgeCode] = useState('');
  const [isPurging, setIsPurging] = useState(false);
  
  const settingsRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (settingsRef.current && !settingsRef.current.contains(event.target as Node)) {
        setIsDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handlePurge = async () => {
    if (purgeCode !== '01900') {
      alert(t.purge_error_code ? t.purge_error_code.replace('{0}', '01900') : 'Mã xác nhận sai! Vui lòng nhập đúng 01900.');
      return;
    }

    setIsPurging(true);
    try {
      const response = await api.purgeData('01900');
      alert(t.purge_success ? t.purge_success : response.message);
      setShowPurgeModal(false);
      setIsDropdownOpen(false);
      setTimeout(() => {
        window.location.reload();
      }, 2500);
    } catch (e: any) {
      alert(`${lang === 'vi' ? 'Lỗi xóa dữ liệu' : 'Purge error'}: ${e.message}`);
      setIsPurging(false);
    }
  };

  // [SHUTDOWN-V1.0] Gửi yêu cầu tắt Node đến API localhost
  const handleShutdown = async () => {
    const confirmMsg = lang === 'vi' 
      ? 'Bạn có chắc chắn muốn TẮT NODE và dừng hệ thống không?\nCửa sổ dòng lệnh của Node sẽ đóng lại.'
      : 'Are you sure you want to SHUTDOWN the node and stop the system?\nThis will close the Node CMD window.';
      
    if (window.confirm(confirmMsg)) {
      try {
        const response = await api.shutdownNode();
        alert(response.message || (lang === 'vi' ? 'Node đang tắt...' : 'Node is shutting down...'));
        setIsDropdownOpen(false);
      } catch (e: any) {
        alert(`${lang === 'vi' ? 'Lỗi khi tắt Node' : 'Shutdown error'}: ${e.message}`);
      }
    }
  };

  return (
    <header className="glass-header h-[90px] px-10 flex items-center justify-between w-full" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', whiteSpace: 'nowrap' }}>
      <div className="flex items-center gap-10" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '40px' }}>
        <div className="flex flex-col">
          <h2 className="text-[14px] font-black uppercase tracking-[0.5em] text-white italic leading-none mb-1.5">
             # {title}
          </h2>
          <div className="flex items-center gap-2" style={{ display: 'flex', flexDirection: 'row', gap: '8px' }}>
            <span className="text-[7px] font-bold text-accent-blue/40 tracking-widest px-1.5 py-0.5 bg-accent-blue/5 rounded">GENESIS_V1.4</span>
            <div className="w-1 h-1 rounded-full bg-accent-blue/30" />
            <span className="text-[7px] font-bold text-white/20 tracking-wider">SECURE_CHAIN_LAYER</span>
          </div>
        </div>

        <div className="h-12 w-[1px] bg-white/5 mx-2" />

        <div className="flex items-center gap-4" style={{ display: 'flex', flexDirection: 'row', gap: '16px' }}>
          <div className="flex flex-col items-start gap-1 p-2 min-w-[110px]">
             <span className="text-[6px] text-white/30 uppercase tracking-widest">{t.header_height}</span>
             <div className="flex items-center gap-2" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '8px' }}>
                <div className="w-1.5 h-1.5 rounded-full bg-accent-amber animate-pulse shadow-[0_0_10px_var(--accent-amber)]" />
                <span className="text-[11px] text-accent-amber font-black italic tracking-tighter">HT: {height.toLocaleString()} / {Math.max(height, targetHeight).toLocaleString()}</span>
             </div>
          </div>

          {/* [VANGUARD-UPGRADE] Sync Progress Card */}
          <div className="flex flex-col items-start gap-1 p-2 min-w-[130px] relative overflow-hidden group">
             <span className="text-[6px] text-white/30 uppercase tracking-widest">{t.header_consensus}</span>
             <div className="flex items-center gap-2" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '8px' }}>
                <div className={`w-1.5 h-1.5 rounded-full ${syncState === 'STREAMING' || syncState === 'SYNCED' ? 'bg-accent-green shadow-[0_0_10px_var(--accent-green)]' : 'bg-accent-blue animate-pulse shadow-[0_0_10px_var(--accent-blue)]'}`} />
                <div className="flex flex-col">
                  <span className={`text-[11px] font-black italic tracking-tighter ${syncState === 'STREAMING' || syncState === 'SYNCED' ? 'text-accent-green' : 'text-accent-blue'}`}>
                    {syncState === 'SYNCING' 
                       ? `${syncProgress.toFixed(1)}%` 
                       : (syncState === 'BOOTSTRAPPING' 
                           ? (syncProgress > 0 ? `SNAP: ${syncProgress.toFixed(1)}%` : "BOOTSTRAPPING")
                           : syncState.toUpperCase())}
                  </span>
                </div>
             </div>
             {/* Progress Bar Layer */}
             {(syncState === 'SYNCING' || (syncState === 'BOOTSTRAPPING' && syncProgress > 0)) && (
               <div className="absolute bottom-0 left-0 w-full h-[2px] bg-white/5">
                 <div 
                   className="h-full bg-accent-blue shadow-[0_0_10px_var(--accent-blue)] transition-all duration-1000 ease-out"
                   style={{ width: `${syncProgress}%` }}
                 />
               </div>
             )}
          </div>

          <div className="flex flex-col items-start gap-1 p-2">
             <span className="text-[6px] text-white/30 uppercase tracking-widest">{t.header_network}</span>
             <div className="flex items-center gap-2" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '8px' }}>
                <div className={`w-1.5 h-1.5 rounded-full ${peerCount > 0 ? 'bg-accent-green shadow-[0_0_8px_var(--accent-green)]' : (isOffline ? 'bg-red-500 animate-pulse shadow-[0_0_10px_rgba(239,68,68,0.4)]' : 'bg-accent-amber shadow-[0_0_8px_var(--accent-amber)]')}`} />
                <span className={`text-[11px] font-black italic tracking-tighter ${peerCount > 0 ? 'text-accent-green' : (isOffline ? 'text-red-500' : 'text-accent-amber')}`}>
                   {peerCount} {t.header_peers}
                </span>
             </div>
          </div>

          <div className="h-12 w-[1px] bg-white/5 mx-2" />

          {/* MATRIX GUARDIAN RADAR */}
          <div className="flex flex-col items-start gap-1 p-2 transition-all duration-500">
            <span className="text-[6px] text-white/30 uppercase tracking-widest">Guardian Status</span>
            <div className="flex items-center gap-3" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '12px' }}>
               <div className="relative w-4 h-4">
                  <div className={`absolute inset-0 rounded-full animate-ping ${isOffline ? 'bg-accent-red' : 'bg-accent-blue'}`} />
                  <div className={`absolute inset-0.5 rounded-full ${isOffline ? 'bg-accent-red' : 'bg-accent-blue'} shadow-[0_0_10px_rgba(0,136,255,0.4)]`} />
               </div>
               <span className={`text-[11px] font-black italic tracking-tighter uppercase ${isOffline ? 'text-accent-red' : 'text-accent-blue'}`}>
                  {isOffline ? 'Node: Critical' : 'Node: Active'}
               </span>
            </div>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-10" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', whiteSpace: 'nowrap' }}>
        <div className="h-12 w-[1px] bg-white/5" />

        <div className="flex items-center gap-6" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '24px' }}>
          <div className="flex gap-3" style={{ display: 'flex', flexDirection: 'row', gap: '12px' }}>
            {/* Language Switcher */}
            <div className="flex bg-white/[0.02] border border-white/[0.05] rounded-2xl p-1 gap-1 h-11 items-center">
              <button 
                onClick={() => setLang('vi')}
                className={`px-3 h-9 text-[9px] uppercase font-black text-center rounded-xl transition-all cursor-pointer ${lang === 'vi' ? 'bg-accent-blue text-white shadow-[0_0_10px_rgba(0,136,255,0.4)]' : 'text-white/30 hover:text-white/70'}`}
              >
                VN
              </button>
              <button 
                onClick={() => setLang('en')}
                className={`px-3 h-9 text-[9px] uppercase font-black text-center rounded-xl transition-all cursor-pointer ${lang === 'en' ? 'bg-accent-blue text-white shadow-[0_0_10px_rgba(0,136,255,0.4)]' : 'text-white/30 hover:text-white/70'}`}
              >
                EN
              </button>
            </div>

            {/* AI Assistant links */}
            <div className="flex bg-white/[0.02] border border-white/[0.05] rounded-2xl p-1 gap-1 h-11 items-center">
              <span className="text-[7px] text-white/30 font-black px-2 uppercase tracking-widest">AI</span>
              <a 
                href="https://gemini.google.com/" 
                target="_blank" 
                rel="noopener noreferrer"
                className="px-3 h-9 text-[9px] font-black uppercase flex items-center text-center text-white/40 hover:text-white hover:bg-white/5 rounded-xl border border-white/5 transition-all"
              >
                Gemini
              </a>
              <a 
                href="https://chatgpt.com/" 
                target="_blank" 
                rel="noopener noreferrer"
                className="px-3 h-9 text-[9px] font-black uppercase flex items-center text-center text-white/40 hover:text-white hover:bg-white/5 rounded-xl border border-white/5 transition-all"
              >
                ChatGPT
              </a>
            </div>

            <button className="w-11 h-11 rounded-2xl bg-white/[0.02] border border-white/[0.05] flex items-center justify-center text-text-secondary hover:text-white hover:bg-white/[0.08] transition-all hover:scale-110 active:scale-90 shadow-lg">
              <Bell size={20} />
            </button>
            <div className="relative" ref={settingsRef}>
              <button 
                onClick={() => setIsDropdownOpen(!isDropdownOpen)}
                className={`w-11 h-11 rounded-2xl border flex items-center justify-center transition-all hover:scale-110 active:scale-90 shadow-lg cursor-pointer ${isDropdownOpen ? 'bg-accent-blue/10 border-accent-blue/30 text-accent-blue' : 'bg-white/[0.02] border-white/[0.05] text-white/50 hover:text-white hover:bg-white/[0.08]'}`}
              >
                <Settings size={20} className={isDropdownOpen ? 'animate-spin' : ''} style={{ animationDuration: '6s' }} />
              </button>
              
              <AnimatePresence>
                {isDropdownOpen && (
                  <motion.div 
                    initial={{ opacity: 0, y: -10, scale: 0.95 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -10, scale: 0.95 }}
                    transition={{ duration: 0.2 }}
                    className="absolute top-[120%] right-0 z-[99] vanguard-glass bg-black/95 border border-white/10 backdrop-blur-md rounded-xl p-3 min-w-[200px] shadow-2xl flex flex-col gap-3"
                  >


                    <button 
                      onClick={() => setShowPurgeModal(true)}
                      className="w-full px-3 py-2.5 rounded-lg flex items-center gap-2 hover:bg-accent-red/10 text-white/70 hover:text-accent-red text-left transition-all text-[10px] font-black uppercase tracking-wider cursor-pointer"
                    >
                      <Trash2 size={12} className="text-white/40" />
                      <span>{t.purge_data_btn ? t.purge_data_btn.toUpperCase() : 'XÓA DATA'}</span>
                    </button>
                    {/* [SHUTDOWN-V1.0] Nút tắt Node trực quan trên giao diện */}
                    <button 
                      onClick={handleShutdown}
                      className="w-full px-3 py-2.5 rounded-lg flex items-center gap-2 hover:bg-accent-red/10 text-white/70 hover:text-accent-red text-left transition-all text-[10px] font-black uppercase tracking-wider cursor-pointer border-t border-white/5"
                    >
                      <Power size={12} className="text-white/40" />
                      <span>{lang === 'vi' ? 'TẮT NODE' : 'SHUTDOWN NODE'}</span>
                    </button>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>
          
          <div className="flex items-center gap-5 px-5 py-2.5 bg-gradient-to-br from-accent-blue/20 to-black/40 rounded-2xl border border-accent-blue/30 shadow-[inset_0_0_20px_rgba(0,136,255,0.1)]" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '20px' }}>
             <div className="w-9 h-9 rounded-xl bg-accent-blue flex items-center justify-center text-white shadow-[0_0_20px_rgba(0,136,255,0.4)]">
                <User size={18} strokeWidth={2.5} />
             </div>
             <div className="flex flex-col gap-0.5" style={{ display: 'flex', flexDirection: 'column' }}>
                <span className="text-[11px] font-black uppercase text-white italic tracking-[0.1em] leading-none">{t.header_commander}</span>
                <span className="text-[8px] text-accent-blue font-black tracking-[0.2em] opacity-60 uppercase truncate max-w-[120px]">
                  {address ? `${address.slice(0, 6)}...${address.slice(-4)}` : t.header_admin_lvl}
                </span>
             </div>
          </div>
        </div>
      </div>
      
      {showPurgeModal && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-md p-4">
          <motion.div 
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            className="w-full max-w-md vanguard-glass p-6 flex flex-col gap-6 border border-accent-red/30 bg-black/95 rounded-2xl"
          >
            <div className="flex flex-col gap-2 items-center text-center">
              <div className="w-16 h-16 rounded-full bg-accent-red/10 flex items-center justify-center mb-2">
                <Database size={32} className="text-accent-red animate-pulse" />
              </div>
              <h3 className="text-xl font-black uppercase italic tracking-wider text-accent-red">
                {t.purge_modal_title}
              </h3>
              <p className="text-xs text-white/60 mt-2 leading-relaxed">
                {t.purge_modal_desc ? t.purge_modal_desc.replace('{0}', '01900') : 'Hành động này sẽ xóa toàn bộ dữ liệu. Vui lòng nhập mã xác nhận 01900.'}
              </p>
            </div>

            <div className="flex flex-col gap-4">
              <input 
                type="text" 
                placeholder={t.purge_input_placeholder ? t.purge_input_placeholder.replace('{0}', '01900') : 'Nhập mã (01900)'}
                value={purgeCode}
                onChange={(e) => setPurgeCode(e.target.value)}
                className="w-full bg-black/60 border border-white/10 rounded-lg px-4 py-3 text-center text-lg font-black tracking-[0.5em] text-accent-red focus:border-accent-red/50 outline-none"
              />
              
              <div className="grid grid-cols-2 gap-3 mt-4">
                <button 
                  onClick={() => { setShowPurgeModal(false); setPurgeCode(''); }}
                  className="py-3 bg-white/5 border border-white/10 rounded-lg text-[10px] font-black uppercase tracking-widest hover:bg-white/10 transition-all text-white cursor-pointer"
                >
                  {t.purge_cancel_btn}
                </button>
                <button 
                  onClick={handlePurge}
                  disabled={isPurging || purgeCode !== '01900'}
                  className={`py-3 rounded-lg text-[10px] font-black uppercase tracking-widest transition-all cursor-pointer 
                    ${isPurging || purgeCode !== '01900' ? 'bg-white/5 text-white/20 cursor-not-allowed' : 'bg-accent-red text-white hover:bg-accent-red/80 shadow-[0_0_15px_var(--accent-red)]'}`}
                >
                  {isPurging ? (lang === 'vi' ? 'ĐANG XÓA...' : 'PURGING...') : (t.purge_confirm_btn ? t.purge_confirm_btn.toUpperCase() : 'XÓA NGAY')}
                </button>
              </div>
            </div>
          </motion.div>
        </div>
      )}
    </header>
  );
};

export default Header;
