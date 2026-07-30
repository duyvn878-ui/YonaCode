import React, { useState, useEffect } from 'react';
import Header from './components/Header';
import GaugeSpeedometer from './components/GaugeSpeedometer';
import WalletSetup from './components/WalletSetup';
import NetworkStatus from './components/NetworkStatus';
import RewardsPanel from './components/RewardsPanel';
import MinedBlocksHistory from './components/MinedBlocksHistory';
import ConsoleTerminal from './components/ConsoleTerminal';
import Bip39Modal from './components/Bip39Modal';
import PowerControlBar from './components/PowerControlBar';

import api, { MinerEngineStatus } from './api';
import { translations, Language } from './i18n/translations';

const App: React.FC = () => {
  const [lang, setLangState] = useState<Language>(() => {
    const saved = localStorage.getItem('ygo_miner_lang') as Language;
    return (saved === 'vn' || saved === 'en' || saved === 'zh') ? saved : 'vn';
  });

  const setLang = (newLang: Language) => {
    setLangState(newLang);
    localStorage.setItem('ygo_miner_lang', newLang);
  };

  const [status, setStatus] = useState<MinerEngineStatus | null>(null);
  const [walletInput, setWalletInput] = useState<string>('');
  const [isBip39Open, setIsBip39Open] = useState<boolean>(false);
  const [seedWords, setSeedWords] = useState<string[]>([]);
  const [generatedTime, setGeneratedTime] = useState<string>('');
  const [toast, setToast] = useState<{ show: boolean; msg: string; icon: string }>({ show: false, msg: '', icon: '📋' });

  const t = translations[lang];

  const showToast = (msg: string, icon = '📋') => {
    setToast({ show: true, msg, icon });
    setTimeout(() => setToast({ show: false, msg: '', icon: '📋' }), 3000);
  };

  const fetchStatus = async () => {
    try {
      const data = await api.getStatus();
      setStatus(data);
      if (data.wallet && !walletInput) {
        setWalletInput(data.wallet);
      }
    } catch (e) {
      // Backend status poll fallback
    }
  };

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 1500);
    return () => clearInterval(interval);
  }, []);

  const handleToggleMining = async () => {
    const isMining = status?.is_mining || false;
    let targetWallet = walletInput.trim();

    if (!isMining && !targetWallet) {
      try {
        const res = await api.createWallet();
        if (res.success) {
          targetWallet = res.address;
          setWalletInput(res.address);
          setSeedWords(res.mnemonic.split(' '));
          setGeneratedTime(`Generated: ${new Date().toLocaleTimeString()}`);
          showToast(`Đã tự động khởi tạo địa chỉ ví: ${res.address.slice(0, 10)}...`, '🔑');
        }
      } catch (e) {
        showToast('Không thể tạo địa chỉ ví tự động', '❌');
        return;
      }
    }

    try {
      if (isMining) {
        await api.stopMining();
        showToast('Đã phát lệnh dừng GPU CUDA Miner', '🛑');
      } else {
        await api.startMining(targetWallet);
        showToast('Đã kích hoạt GPU CUDA Mining Engine!', '🚀');
      }
      setTimeout(fetchStatus, 300);
    } catch (e) {
      showToast('Lỗi khi gửi lệnh điều khiển Miner', '❌');
    }
  };

  const handleCopyWallet = () => {
    if (walletInput.trim()) {
      navigator.clipboard.writeText(walletInput.trim());
      showToast('Đã sao chép địa chỉ ví!', '📋');
    } else {
      showToast('Chưa nhập hoặc khởi tạo địa chỉ ví!', '⚠️');
    }
  };

  const handleOpenBip39Modal = async () => {
    try {
      const res = await api.createWallet();
      if (res.success) {
        setWalletInput(res.address);
        setSeedWords(res.mnemonic.split(' '));
        setGeneratedTime(`Generated: ${new Date().toLocaleTimeString()}`);
        setIsBip39Open(true);
      }
    } catch (e) {
      showToast('Không thể tạo ví BIP39', '❌');
    }
  };

  const handleCopySeed = () => {
    if (seedWords.length > 0) {
      navigator.clipboard.writeText(seedWords.join(' '));
      showToast('Đã sao chép 12 từ khôi phục Seed Phrase!', '📋');
    }
  };

  const handleDownloadPDF = () => {
    showToast('Tải xuống bản sao lưu an toàn...', '📄');
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 p-4 md:p-6 pb-28 space-y-6 font-sans">
      <Header 
        currentLang={lang}
        onSetLanguage={setLang}
        isMining={status?.is_mining || false}
        uptime={status?.uptime || '00d 00h 00m 00s'}
        t={t}
        onShowToast={showToast}
      />

      {/* Bento Grid Layout - 3 Equal Columns */}
      <main className="grid grid-cols-1 lg:grid-cols-3 gap-6 max-w-[1720px] mx-auto">
        
        {/* Column 1: Speedometer Arc & Wallet Configuration */}
        <section className="flex flex-col gap-5">
          <GaugeSpeedometer 
            hashrate={status?.hashrate || 0}
            t={t}
          />
          <WalletSetup 
            wallet={walletInput}
            onWalletChange={setWalletInput}
            onCopyWallet={handleCopyWallet}
            onOpenBip39Modal={handleOpenBip39Modal}
            t={t}
          />
        </section>

        {/* Column 2: Network Status & Mined Blocks History Table */}
        <section className="flex flex-col gap-5">
          <NetworkStatus 
            networkHeight={status?.network_height || 0}
            networkHashrate={status?.network_hashrate || 0}
            t={t}
          />
          <MinedBlocksHistory 
            blocks={status?.mined_blocks_history || []}
            t={t}
          />
        </section>

        {/* Column 3: Rewards Panel & Console Terminal */}
        <section className="flex flex-col gap-5">
          <RewardsPanel 
            blocksFound={status?.blocks_found || 0}
            walletBalance={status?.wallet_balance || 0}
            t={t}
          />
          <ConsoleTerminal 
            logs={status?.logs || []}
            t={t}
          />
        </section>

      </main>

      <PowerControlBar 
        isMining={status?.is_mining || false}
        onToggleMining={handleToggleMining}
        t={t}
      />

      {/* Floating Pop-up Overlay Modal for BIP39 Generator */}
      <Bip39Modal 
        isOpen={isBip39Open}
        onClose={() => setIsBip39Open(false)}
        seedWords={seedWords}
        generatedTime={generatedTime}
        onCopySeed={handleCopySeed}
        onDownloadPDF={handleDownloadPDF}
        t={t}
      />

      {/* Toast Notification */}
      {toast.show && (
        <div className="fixed bottom-6 right-6 bg-slate-800/95 border border-sky-500 text-white px-5 py-3 rounded-xl shadow-2xl flex items-center gap-2.5 z-50 animate-in slide-in-from-bottom duration-200 text-sm font-semibold">
          <span>{toast.icon}</span>
          <span>{toast.msg}</span>
        </div>
      )}
    </div>
  );
};

export default App;
