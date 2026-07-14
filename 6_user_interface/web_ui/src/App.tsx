import { useState, useEffect, useCallback } from 'react';
import type { NodeStatus, MinerStatus, BlockHeader, Transaction } from './api';
import api from './api';
import { useLanguage } from './LanguageContext';
import { Shield } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

// V6.2 UI COMPONENTS
import Sidebar from './components/layout/Sidebar';
import Header from './components/layout/Header';
import UnifiedDashboard from './components/layout/UnifiedDashboard';
import UnifiedWalletPanel from './components/wallet/UnifiedWalletPanel';
import ExplorerView from './components/explorer/ExplorerView';
import MinerView from './components/miner/MinerView';
import MonitorView from './components/node/MonitorView';
import PoolView from './components/miner/PoolView';

// MODALS
import WalletRestoreModal from './components/wallet/WalletRestoreModal';
import WalletCreateModal from './components/wallet/WalletCreateModal';
import SendModal from './components/wallet/SendModal';
import ReceiveModal from './components/wallet/ReceiveModal';
import TransactionDetailModal from './components/shared/TransactionDetailModal';
import ErrorBoundary from './components/shared/ErrorBoundary';

interface Notification {
  id: string;
  msg: string;
  type: 'info' | 'success' | 'error' | 'finality';
}

type TabType = 'dashboard' | 'wallet' | 'explorer' | 'miner' | 'monitor' | 'pool';

function App() {
  const { t } = useLanguage();
  const [activeTab, setActiveTab] = useState<TabType>(() => {
    const saved = localStorage.getItem('vanguard_active_tab');
    return (saved as TabType) || 'dashboard';
  });
  const [status, setStatus] = useState<NodeStatus | null>(null);
  const [balance, setBalance] = useState(0);
  const [address, setAddress] = useState<string>(() => {
    return localStorage.getItem('vanguard_wallet_address') || '';
  });
  const [miner, setMiner] = useState<MinerStatus | null>(null);
  const [blocks, setBlocks] = useState<BlockHeader[]>([]);
  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [supply, setSupply] = useState(0);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [selectedTx, setSelectedTx] = useState<Transaction | null>(null);
  // [V38.0 FIX] Counter để buộc UnifiedWalletPanel re-fetch lịch sử khi có giao dịch mới
  const [txRefreshCounter, setTxRefreshCounter] = useState(0);

  // MODAL STATES
  const [isRestoreModalOpen, setIsRestoreModalOpen] = useState(false);
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [isSendModalOpen, setIsSendModalOpen] = useState(false);
  const [isReceiveModalOpen, setIsReceiveModalOpen] = useState(false);

  const addNotification = useCallback((msg: string, type: 'info' | 'success' | 'error' | 'finality' = 'info') => {
    const id = Math.random().toString(36).substring(2, 11) + Date.now();
    setNotifications(prev => [...prev, { id, msg, type }]);
    setTimeout(() => {
      setNotifications(prev => prev.filter(n => n.id !== id));
    }, 6000);
  }, []);

  const [offlineCounter, setOfflineCounter] = useState(0);
  const isNodeOffline = offlineCounter >= 3;

  const fetchData = useCallback(async () => {
    try {
      const s = await api.getStatus().catch(() => null);
      if (!s) {
        setOfflineCounter(prev => prev + 1);
      } else {
        setOfflineCounter(0);
      }

      if (s) {
        const [b, m, blks, txs, sup] = await Promise.all([
          address ? api.getBalance(address).catch(() => 0) : Promise.resolve(0),
          api.getMinerStatus().catch(() => null),
          api.getRecentBlocks().catch(() => []),
          api.getRecentTxs().catch(() => []),
          api.getSupply().catch(() => 0)
        ]);
        setStatus(s);
        setBalance(b);
        setMiner(m);
        setBlocks(blks);
        setTransactions(txs);
        setSupply(sup);
      } else {
        setStatus(null);
      }
    } catch (e) {
      console.error("Fetch Data Error:", e);
      setOfflineCounter(prev => prev + 1);
    }
  }, [address]);

  useEffect(() => {
    fetchData();
    const timer = setInterval(fetchData, 3000);
    return () => clearInterval(timer);
  }, [fetchData]);

  // [V6.3.2] Persistence Layer - Ghi nhớ địa chỉ và tab
  useEffect(() => {
    if (address) {
      localStorage.setItem('vanguard_wallet_address', address);
    }
  }, [address]);

  useEffect(() => {
    localStorage.setItem('vanguard_active_tab', activeTab);
  }, [activeTab]);

  // [V20.2] ĐẠI DÒ TÌM VÍ (TOTAL WALLET DISCOVERY)
  useEffect(() => {
    let active = true;
    const discoverWallets = async () => {
      try {
        // [SỬA LỖI ĐỒNG BỘ VÍ] Hệ thống cần tôn trọng tuyệt đối lựa chọn chủ động của người dùng.
        // Tại sao thiết kế như vậy: Nếu người dùng đã có địa chỉ ví hoạt động (khôi phục ví mới thành công
        // hoặc vừa tạo mới), chúng ta không được phép dùng cơ chế tự động quét để ghi đè ví của họ,
        // ngay cả khi ví cũ có số dư lớn hơn. Cơ chế dò tìm ví giàu hơn chỉ được kích hoạt khi chưa có ví nào
        // được cấu hình (address trống) nhằm mang lại trải nghiệm tiện lợi ban đầu cho người dùng mới.
        // Tại sao thêm cờ 'active': Vì discoverWallets là hàm bất đồng bộ (async), nếu người dùng khôi phục ví mới
        // trong lúc API đang chờ kết quả, state 'address' sẽ thay đổi và render mới được tạo ra.
        // Cờ 'active' giúp hủy bỏ kết quả xử lý của render cũ, ngăn chặn việc ép ghi đè ví mới về ví cũ.
        if (address) {
          return;
        }

        // 1. Lấy danh sách ví từ Backend
        const wallets = await api.getWallets();
        if (!active) return;
        if (!wallets || wallets.length === 0) return;

        console.log(`[App] 🔍 Đang quét ${wallets.length} ví cục bộ để tìm số dư...`);
        
        let bestAddr = address;
        let maxBal = balance;

        for (const w of wallets) {
          const b = await api.getBalance(w.address);
          if (!active) return;
          if (b > maxBal) {
            maxBal = b;
            bestAddr = w.address;
          }
        }

        // 2. Chuyển đổi nếu tìm thấy ví 'giàu' hơn
        if (bestAddr && bestAddr !== address && maxBal > 0) {
           console.log("[App] 🛡️ Discovery Victory: Switching to wallet with higher balance:", bestAddr);
           setAddress(bestAddr);
           localStorage.setItem('vanguard_wallet_address', bestAddr);
           addNotification(`PHỤC HỒI: Đã tìm thấy số dư ${maxBal.toLocaleString()} VNT trong ví ${bestAddr.slice(0,8)}...`, "success");
           setTimeout(fetchData, 100);
        }
      } catch (e) {
        console.warn("Wallet Discovery error:", e);
      }
    };
    
    // Chạy ngắt quãng để đảm bảo luôn cập nhật nếu có Replay mới
    const timer = setTimeout(discoverWallets, 1500);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [balance, address, fetchData]);

  const [isStopping, setIsStopping] = useState(false);

  const handleToggleMiner = async () => {
    if (isStopping) return;

    try {
      const currentlyMining = status?.node_mode === "full-mining";
      
      // [SECURITY-MINING FIX] Thợ đào không cần chữ ký, nên không cần PIN. 
      // Chỉ cần thiết lập địa chỉ ví nhận tiền thưởng.
      if (!currentlyMining) {
        if (status?.sync.state === 'SYNCING' || status?.sync.state === 'BOOTSTRAPPING') {
          const confirmText = t.lang === 'vi' 
            ? `${t.mining_sync_warning}\n\nBạn vẫn muốn tiếp tục kích hoạt đào chứ?`
            : `${t.mining_sync_warning}\n\nDo you still want to proceed with enabling mining?`;
          const proceed = window.confirm(confirmText);
          if (!proceed) return;
        }

        if (!address || address === "0000000000000000000000000000000000000000000000000000000000000000") {
          addNotification("AN NINH: Vui lòng Tạo hoặc Khôi phục ví để nhận thưởng trước khi Bắt đầu đào!", "error");
          return;
        }

        console.log("[App] ⚡ Setting miner address for rewards:", address);
        await api.setMinerAddress(address, ""); // Gửi PIN trống lên Backend
      } else {
        setIsStopping(true);
        addNotification("Hệ thống đang tắt động cơ đào, vui lòng chờ vài giây...", "info");
      }
      
      await api.toggleMiner();
      
      if (!currentlyMining) {
         addNotification(t.toggle_on, 'success');
      } else {
         addNotification(t.toggle_off, 'info');
      }
      
      setIsStopping(false);
      fetchData();
    } catch (e) {
      setIsStopping(false);
      const errMessage = (e as Error).message;
      if (errMessage === 'SYNC_REQUIRED') {
        addNotification(`Hệ thống đang đồng bộ. Vui lòng chờ 100% trước khi Bật Đào.`, 'error');
      } else if (errMessage === 'INVALID_MINER_WALLET') {
        addNotification(`Lỗi cấu hình: Vui lòng tạo hoặc khôi phục Ví trước khi Bật Đào!`, 'error');
      } else {
        addNotification(`${t.node_intel}: ${errMessage}`, 'error');
      }
    }
  };

  const handleRestore = async (mnemonic: string, password?: string, passphrase?: string) => {
    try {
      const res = await api.restoreWallet({ mnemonic, password, passphrase });
      setAddress(res.address);
      // [FIX] Đồng bộ ví khai thác với Node Backend
      await api.setMinerAddress(res.address).catch((e) => console.warn("Miner address sync warn:", e));
      setIsRestoreModalOpen(false);
      addNotification("Đã khôi phục Ví thành công!", "success");
      fetchData();
    } catch (e) {
      addNotification(`Lỗi khôi phục: ${(e as Error).message}`, "error");
    }
  };

  const handleCreateWallet = () => setIsCreateModalOpen(true);
  
  const executeCreateWallet = async (password: string, passphrase: string): Promise<string> => {
    try {
      const res = await api.createWallet("default", password, passphrase);
      setAddress(res.address);
      // [FIX] Đồng bộ ví khai thác với Node Backend
      await api.setMinerAddress(res.address).catch((e) => console.warn("Miner address sync warn:", e));
      addNotification("Đã tạo Ví thành công!", "success");
      fetchData();
      return res.mnemonic;
    } catch (e) {
      addNotification(`Lỗi tạo Ví: ${(e as Error).message}`, "error");
      throw e;
    }
  };

  const handleSend = () => setIsSendModalOpen(true);

  const handleWalletDelete = async (addrToDelete: string) => {
    try {
      await api.deleteWallet(addrToDelete);
      addNotification("Đã xóa ví thành công khỏi thiết bị!", "success");
      if (address === addrToDelete) {
        setAddress("");
        localStorage.removeItem('vanguard_wallet_address');
      }
      fetchData();
    } catch (e) {
      addNotification(`Lỗi xóa ví: ${(e as Error).message}`, "error");
    }
  };

  const handleTabChange = (tab: string) => {
      if (['dashboard', 'wallet', 'explorer', 'miner', 'monitor', 'pool'].includes(tab)) {
          setActiveTab(tab as TabType);
          localStorage.setItem('vanguard_active_tab', tab);
      }
  };

  return (
    <ErrorBoundary>
      <div className="flex bg-black min-h-screen font-sans selection:bg-accent-blue/30 relative">
        <Sidebar activeTab={activeTab} setActiveTab={handleTabChange} />
        
        <div className="flex-1 flex flex-col min-h-screen relative" style={{ marginLeft: 'var(--sidebar-width)' }}>
          <Header
            title={activeTab === 'dashboard' ? t.overview : activeTab.toUpperCase()}
            height={status?.highest_height || 0}
            targetHeight={status?.sync?.target || 0}
            syncState={status?.sync.state}
            syncProgress={
              status?.sync
                ? (status.sync.state === 'BOOTSTRAPPING' && status.sync.snapshot_chunks_total && status.sync.snapshot_chunks_total > 0
                    ? ((status.sync.snapshot_chunks_loaded || 0) / status.sync.snapshot_chunks_total) * 100
                    : (status.sync.executing === true
                        ? (status.sync.target > 0 ? (status.sync.current / status.sync.target) * 100 : 0)
                        : (status.sync.downloading && status.sync.downloading > 0
                            ? (status.sync.target > 0 ? (status.sync.downloading / status.sync.target) * 100 : 0)
                            : 0)))
                : 0
            }
            syncExecuting={status?.sync?.executing}
            syncDownloading={status?.sync?.downloading || 0}
            peerCount={status?.peers?.count || 0}
            address={address}
            isOffline={isNodeOffline}
          />

          {/* 🚨 YONA EMERGENCY NETWORK ALERT SYSTEM BANNER */}
          <AnimatePresence>
            {status?.active_alert && (
              <motion.div 
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                className="bg-red-950/80 border-b border-red-500/50 p-4 flex flex-col sm:flex-row items-center justify-center gap-3 overflow-hidden text-center shadow-[0_0_30px_rgba(239,68,68,0.2)]"
              >
                <div className="flex items-center gap-2">
                  <span className="relative flex h-3.5 w-3.5">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-3.5 w-3.5 bg-red-500 shadow-[0_0_10px_var(--accent-red)]"></span>
                  </span>
                  <span className="text-[10px] font-black uppercase tracking-[0.2em] text-red-500 animate-pulse">
                    {t.network_alert_title}:
                  </span>
                </div>
                <p className="text-[11px] font-black text-red-100 uppercase tracking-widest leading-relaxed max-w-4xl">
                  {t.lang === 'vi' ? status.active_alert.message_vi : status.active_alert.message_en}
                </p>
                {status.active_alert.github_url && (
                  <a
                    href={status.active_alert.github_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="px-3 py-1.5 bg-red-600 hover:bg-red-500 text-white text-[9px] font-black uppercase rounded transition-all duration-300 pointer-events-auto shadow-[0_0_15px_rgba(239,68,68,0.4)] cursor-pointer"
                    style={{ textDecoration: 'none' }}
                  >
                    {t.lang === 'vi' ? 'TẢI BẢN CẬP NHẬT (GITHUB)' : 'DOWNLOAD UPDATE (GITHUB)'}
                  </a>
                )}
                <span className="text-[9px] font-mono text-red-400/80">
                  ({t.lang === 'vi' ? `Hết hạn tại khối: #${status.active_alert.expiration_block}` : `Expires at block: #${status.active_alert.expiration_block}`})
                </span>
              </motion.div>
            )}
          </AnimatePresence>

          {/* ⚠️ MINING SECURITY WARNING BAR - [VANGUARD-V5] */}
          <AnimatePresence>
            {status?.mining_warning && (
              <motion.div 
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                className="bg-accent-amber/20 border-b border-accent-amber/30 p-3 flex items-center justify-center gap-3 overflow-hidden"
              >
                <div className="w-2 h-2 rounded-full bg-accent-amber animate-pulse shadow-[0_0_10px_rgba(245,158,11,0.5)]" />
                <p className="text-[10px] font-black uppercase tracking-[0.2em] text-accent-amber animate-pulse">
                  {status.mining_warning}
                </p>
                {status.mining_warning.includes("0xc0000135") && (
                  <button
                    onClick={async () => {
                      try {
                        await api.installEnv();
                        addNotification("🚀 Đang tải và cài đặt Microsoft VC++ Redistributable chạy ngầm...", "info");
                      } catch (e) {
                        addNotification(`❌ Lỗi: ${(e as Error).message}`, "error");
                      }
                    }}
                    className="ml-4 px-3 py-1 bg-accent-amber text-black text-[9px] font-black uppercase rounded hover:bg-white transition-all duration-300 pointer-events-auto shadow-[0_0_10px_rgba(245,158,11,0.3)] cursor-pointer"
                  >
                    Tự động cài đặt
                  </button>
                )}
                <div className="w-2 h-2 rounded-full bg-accent-amber animate-pulse shadow-[0_0_10px_rgba(245,158,11,0.5)]" />
              </motion.div>
            )}
          </AnimatePresence>

          <main className="flex-1 p-6 relative scroll-smooth">
            {/* 🔦 Ambient Lighting - V6.2 Diagnostic */}
            <div className="absolute top-0 left-1/2 -translate-x-1/2 w-full h-[600px] bg-accent-blue/5 blur-[120px] rounded-full pointer-events-none opacity-10" />
            
            {activeTab === 'dashboard' && (
              <UnifiedDashboard
                status={status}
                balance={balance}
                address={address}
                miner={miner}
                blocks={blocks}
                transactions={transactions}
                supply={supply}
                handleToggleMiner={handleToggleMiner}
                handleSend={handleSend}
                onRestoreClick={() => setIsRestoreModalOpen(true)}
                onCreateClick={handleCreateWallet}
                onTransactionClick={(tx) => setSelectedTx(tx)}
                onNotify={addNotification}
                onReceiveClick={() => setIsReceiveModalOpen(true)}
                isStopping={isStopping}
                pendingTxCount={status?.pending_tx_count || 0}
                txRefreshCounter={txRefreshCounter}
              />
            )}

              {activeTab === 'wallet' && (
                <UnifiedWalletPanel
                  balance={balance}
                  address={address}
                  handleSend={handleSend}
                  onWalletDelete={handleWalletDelete}
                onRestoreClick={() => setIsRestoreModalOpen(true)}
                onCreateClick={handleCreateWallet}
                onTransactionClick={(tx) => setSelectedTx(tx)}
                onReceiveClick={() => setIsReceiveModalOpen(true)}
                pendingTxCount={status?.pending_tx_count || 0}
                txRefreshCounter={txRefreshCounter}
              />
            )}

            {activeTab === 'explorer' && (
              <ExplorerView
                status={status}
                blocks={blocks}
                transactions={transactions}
                onTxClick={(tx) => setSelectedTx(tx)}
              />
            )}

            {activeTab === 'miner' && (
              <MinerView
                status={status}
                minerStatus={miner}
                handleToggleMiner={handleToggleMiner}
                onNotify={addNotification}
                isStopping={isStopping}
              />
            )}

            {activeTab === 'monitor' && (
              <MonitorView
                status={status}
              />
            )}

            {activeTab === 'pool' && (
              <PoolView
                status={status}
                onNotify={addNotification}
              />
            )}
          </main>
        </div>

        {/* NOTIFICATIONS PORTAL */}
        <div className="fixed top-8 right-8 z-[2000] flex flex-col gap-4 pointer-events-none">
          <AnimatePresence>
            {notifications.map(n => (
              <motion.div
                key={n.id}
                initial={{ x: 100, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 100, opacity: 0 }}
                className={`p-6 rounded-2xl border backdrop-blur-3xl shadow-2xl flex items-center gap-4 pointer-events-auto 
                  ${n.type === 'success' ? 'bg-accent-green/20 border-accent-green text-white shadow-[0_0_20px_rgba(34,197,94,0.2)]' : 'bg-accent-blue/10 border-accent-blue text-accent-blue'}`}
              >
                <Shield size={24} />
                <p className="text-xs font-black uppercase tracking-widest">{n.msg}</p>
              </motion.div>
            ))}
          </AnimatePresence>
        </div>
      </div>

      {/* MODALS */}
      <WalletRestoreModal isOpen={isRestoreModalOpen} onClose={() => setIsRestoreModalOpen(false)} onRestore={handleRestore} />
      <WalletCreateModal isOpen={isCreateModalOpen} onClose={() => setIsCreateModalOpen(false)} onCreate={executeCreateWallet} />
      <SendModal isOpen={isSendModalOpen} onClose={() => setIsSendModalOpen(false)} onSuccess={(msg: string, txBill?: any) => { 
        if (txBill) {
          try {
            const currentBills = JSON.parse(localStorage.getItem('vanguard_local_bills') || '[]');
            currentBills.push(txBill);
            localStorage.setItem('vanguard_local_bills', JSON.stringify(currentBills));
          } catch (e) {
            console.error("Lỗi lưu local bill:", e);
          }
        }
        addNotification(msg, 'success'); 
        setTxRefreshCounter(c => c + 1); 
        fetchData(); 
      }} sender={address} balance={balance} />
      <ReceiveModal isOpen={isReceiveModalOpen} onClose={() => setIsReceiveModalOpen(false)} address={address} />
      <TransactionDetailModal tx={selectedTx} onClose={() => setSelectedTx(null)} currentHeight={status?.highest_height || 0} />
      {/* MATRIX GUARDIAN - OFFLINE OVERLAY */}
      <AnimatePresence>
        {isNodeOffline && (
          <motion.div 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-[9999] bg-black/90 backdrop-blur-xl flex items-center justify-center p-6 text-white"
          >
            <div className="max-w-md w-full bg-accent-red/5 border border-accent-red/20 rounded-3xl p-10 vanguard-flex-v items-center text-center vanguard-gap-medium relative overflow-hidden">
               <div className="absolute inset-0 bg-accent-red/5 animate-pulse" />
               
               <div className="w-24 h-24 rounded-full bg-accent-red/20 flex items-center justify-center text-accent-red mb-4 shadow-[0_0_50px_rgba(255,59,48,0.2)]">
                  <Shield size={48} className="animate-bounce" />
               </div>
               
               <h2 className="text-2xl font-black italic uppercase tracking-tighter text-white">
                  LỖI: MẤT KẾT NỐI VỚI NODE
               </h2>
               
               <p className="text-text-muted text-xs leading-relaxed font-medium">
                  Hệ thống Matrix phát hiện Node Backend (genz.exe) đã bị dừng. Khả năng giao dịch và đào khối đã bị vô hiệu hóa.
               </p>

               <div className="vanguard-flex-v vanguard-gap-tiny w-full mt-4">
                  <div className="p-4 bg-black/40 rounded-xl border border-white/5 text-[10px] text-accent-amber font-black uppercase text-left flex items-center gap-3">
                     <span className="w-2 h-2 rounded-full bg-accent-amber animate-pulse" />
                     ĐANG KÍCH HOẠT CHẾ ĐỘ PHỤC HỒI...
                  </div>
                  <p className="text-[9px] text-text-muted italic">
                     Matrix Guardian đang tự động khởi động lại Node. Vui lòng giữ nguyên cửa sổ này.
                  </p>
               </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </ErrorBoundary>
  );
}

export default App;
