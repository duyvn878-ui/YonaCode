import { useState, useEffect } from 'react';
import { 
  Shield, Send, ArrowDownLeft, RefreshCw, Key, Lock, LogOut, Copy, Check, X, ArrowUpRight,
  Bell, Trash2, CheckCircle2, AlertCircle, Info, Clock, Sparkles, Globe, Settings
} from 'lucide-react';
import { 
  generateNewMnemonic, isValidMnemonic, deriveKeyPairFromMnemonic, 
  encryptPrivateKey, decryptPrivateKey, signPreparedHash
} from './crypto';

interface PreparedTx {
  success: boolean;
  error?: string;
  version: number;
  sender: string;
  receiver: string;
  amount: number;
  fee: number;
  creation_fee: number;
  nonce: number;
  timestamp: number;
  recent_block_hash: string;
  signing_hash: string;
  chain_id?: number;
}

interface TxHistoryItem {
  txid: string;
  sender: string;
  receiver: string;
  amount: number;
  fee: number;
  nonce: number;
  timestamp: number;
  status: number; // 0 = Success, 99 = Pending, 1 = Error/Rejected
  blockHeight?: number;
  confirmations?: number;
}

interface ImmutableNotification {
  id: string;
  title: string;
  message: string;
  type: 'success' | 'warning' | 'error' | 'info';
  timestamp: number;
  txid?: string;
  read: boolean;
}

type Lang = 'vi' | 'en' | 'zh';

const I18N = {
  vi: {
    walletTitle: 'YONA WALLET',
    walletSub: 'Ví Di Động Chuẩn Mainnet YonaCode (Fintech 2026)',
    createWallet: 'TẠO VÍ MỚI',
    restoreWallet: 'KHÔI PHỤC VÍ BẰNG MNEMONIC',
    saveMnemonicTitle: 'Cụm từ Mnemonic (12 từ)',
    saveMnemonicSub: 'Lưu giữ 12 từ này ở nơi an toàn. Đây là chìa khóa duy nhất để khôi phục tài sản!',
    iSaved: 'TÔI ĐÃ SAO LƯU AN TOÀN',
    enterMnemonicTitle: 'Nhập 12 từ Mnemonic',
    mnemonicPlaceholder: 'Nhập 12 từ khóa cách nhau bởi dấu cách...',
    next: 'TIẾP TỤC',
    pinSetupTitle: 'Thiết lập mã PIN 6 số',
    newPin: 'Nhập mã PIN 6 số mới',
    confirmPin: 'Xác nhận lại mã PIN 6 số',
    loginTitle: 'ĐĂNG NHẬP VÍ',
    switchWallet: 'Đổi ví / Tạo ví mới',
    changeWallet: 'ĐỔI VÍ',


    logout: 'ĐĂNG XUẤT',
    availableBalance: 'Số dư khả dụng',
    send: 'GỬI TIỀN',
    receive: 'NHẬN TIỀN',
    network: 'Mạng lưới',
    notifications: 'Thông báo',
    newNotif: 'mới',
    historyLogs: 'nhật ký',
    txHistory: 'Lịch sử giao dịch',
    noHistory: 'Chưa có lịch sử giao dịch nào',
    success: 'Thành công',
    pending: 'Đang xử lý',
    rejected: 'Bị từ chối',
    immutableLogsTitle: 'Nhật ký & Thông báo Bất biến',
    clearLogs: 'Xóa nhật ký',
    all: 'Tất cả',
    transactions: 'Giao dịch',
    system: 'Hệ thống',
    closeLogs: 'ĐÓNG NHẬT KÝ',
    sendTitle: 'Chuyển tiền GO 24/7',
    confirmSendTitle: 'Xác nhận chuyển tiền',
    sendAll: 'GỬI TẤT CẢ',
    receiverAddress: 'Ví nhận tiền (GO Address)',
    receiverPlaceholder: 'Nhập địa chỉ 0x...',
    sendAmount: 'Số tiền chuyển (GO)',
    baseFee: 'Phí cơ sở: ~0.00000250 GO',
    speed: 'Tốc độ xử lý',
    speedFast: 'Tức thì (~3s)',
    cancel: 'HỦY',
    back: '← LẠI',
    confirmSendBtn: 'XÁC NHẬN CHUYỂN TIỀN',
    preparing: 'ĐANG CHUẨN BỊ...',
    signing: 'ĐANG KÝ...',
    txSuccessMsg: 'Gửi giao dịch thành công!',
    done: 'HOÀN TẤT',
    copyAddress: 'SAO CHÉP ĐỊA CHỈ VÍ',
    copiedAddress: 'ĐÃ SAO CHÉP ĐỊA CHỈ!',
    close: 'ĐÓNG',
    txDetail: 'Chi tiết Giao dịch',
    closeDetail: 'ĐÓNG CHI TIẾT',
    senderAcc: 'Tài khoản người gửi',
    receiverAcc: 'Tài khoản người nhận',
    txHash: 'Mã băm giao dịch (TxID)',
    block: 'Khối (Block)',
    unconfirmed: 'Chưa xác nhận',
    nonce: 'Số thứ tự (Nonce)',
    time: 'Thời gian khởi tạo',
    fee: 'Phí giao dịch',
    amount: 'Số lượng chuyển',
    pinEnterConfirm: 'Nhập mã PIN 6 số để ký giao dịch',
    projectedNonce: 'Nonce dự phóng',
    settingsTitle: 'Cài đặt hệ thống',
    languageLabel: 'Ngôn ngữ hiển thị (Language)',
    securityLabel: 'Bảo mật & Tài khoản',
    viewAll: 'Xem tất cả >',
    allTxDisplayed: 'Đã hiển thị tất cả giao dịch',
    loadMore: 'Xem thêm giao dịch',
    immutableStatus: 'TRẠNG THÁI BẤT BIẾN',
    confirmingStatus: 'ĐANG XÁC THỰC',
    mempoolPendingStatus: 'ĐANG CHỜ ĐÓNG GÓI',
    blocksWord: 'KHỐI',
    blockWord: 'Khối',
    immutableInfoNote: '✓ Giao dịch đã đạt 5 khối xác nhận. Thông tin đã được lưu vĩnh viễn trên sổ cái không thể sửa đổi.',
    mempoolInfoNote: '⏳ Giao dịch đang nằm trong Mempool chờ thợ đào đóng gói vào Block mới.',
    confirmingInfoNote: '⏳ Đã có {confirms}/5 khối xác nhận. Cần thêm {needed} khối nữa (~{time} phút) để đạt tính Bất biến tuyệt đối.',
    myWallet: 'Ví Yona Của Tôi',
    online: 'Trực tuyến',
    offline: 'Ngoại tuyến',
    networkValue: 'PoW 75s / Khối',
  },
  en: {
    walletTitle: 'YONA WALLET',
    walletSub: 'YonaCode Mainnet Mobile Wallet (Fintech 2026)',
    createWallet: 'CREATE NEW WALLET',
    restoreWallet: 'RESTORE FROM MNEMONIC',
    saveMnemonicTitle: 'Mnemonic Phrase (12 Words)',
    saveMnemonicSub: 'Keep these 12 words safe. This is the only way to recover your assets!',
    iSaved: 'I HAVE SAVED IT SAFELY',
    enterMnemonicTitle: 'Enter 12 Mnemonic Words',
    mnemonicPlaceholder: 'Enter 12 words separated by spaces...',
    next: 'CONTINUE',
    pinSetupTitle: 'Set 6-Digit PIN',
    newPin: 'Enter a new 6-digit PIN',
    confirmPin: 'Confirm your 6-digit PIN',
    loginTitle: 'WALLET LOGIN',
    switchWallet: 'Switch / Create Wallet',
    changeWallet: 'SWITCH',

    logout: 'LOGOUT',
    availableBalance: 'Available Balance',
    send: 'SEND',
    receive: 'RECEIVE',
    network: 'Network',
    notifications: 'Notifications',
    newNotif: 'new',
    historyLogs: 'logs',
    txHistory: 'Transaction History',
    noHistory: 'No transaction history found',
    success: 'Success',
    pending: 'Pending',
    rejected: 'Rejected',
    immutableLogsTitle: 'Immutable Logs & Notifications',
    clearLogs: 'Clear Logs',
    all: 'All',
    transactions: 'Transactions',
    system: 'System',
    closeLogs: 'CLOSE LOGS',
    sendTitle: 'Send GO 24/7',
    confirmSendTitle: 'Confirm Transfer',
    sendAll: 'SEND ALL',
    receiverAddress: 'Receiver GO Address',
    receiverPlaceholder: 'Enter address 0x...',
    sendAmount: 'Transfer Amount (GO)',
    baseFee: 'Base fee: ~0.00000250 GO',
    speed: 'Processing Speed',
    speedFast: 'Instant (~3s)',
    cancel: 'CANCEL',
    back: '← BACK',
    confirmSendBtn: 'CONFIRM TRANSFER',
    preparing: 'PREPARING...',
    signing: 'SIGNING...',
    txSuccessMsg: 'Transaction Submitted Successfully!',
    done: 'DONE',
    copyAddress: 'COPY WALLET ADDRESS',
    copiedAddress: 'ADDRESS COPIED!',
    close: 'CLOSE',
    txDetail: 'Transaction Details',
    closeDetail: 'CLOSE DETAILS',
    senderAcc: 'Sender Account',
    receiverAcc: 'Receiver Account',
    txHash: 'Transaction Hash (TxID)',
    block: 'Block Height',
    unconfirmed: 'Unconfirmed',
    nonce: 'Nonce Number',
    time: 'Creation Time',
    fee: 'Transaction Fee',
    amount: 'Transfer Amount',
    pinEnterConfirm: 'Enter 6-digit PIN to sign transaction',
    projectedNonce: 'Projected Nonce',
    settingsTitle: 'System Settings',
    languageLabel: 'Display Language',
    securityLabel: 'Security & Account',
    viewAll: 'View All >',
    allTxDisplayed: 'All transactions displayed',
    loadMore: 'Load More Transactions',
    immutableStatus: 'IMMUTABLE STATUS',
    confirmingStatus: 'CONFIRMING',
    mempoolPendingStatus: 'PENDING MINING',
    blocksWord: 'BLOCKS',
    blockWord: 'Block',
    immutableInfoNote: '✓ Transaction has reached 5 confirmations. Information is permanently recorded on the immutable ledger.',
    mempoolInfoNote: '⏳ Transaction is in Mempool waiting for miners to pack into a new block.',
    confirmingInfoNote: '⏳ {confirms}/5 blocks confirmed. Need {needed} more blocks (~{time} mins) for absolute immutability.',
    myWallet: 'My Yona Wallet',
    online: 'Online',
    offline: 'Offline',
    networkValue: 'PoW 75s / Block',
  },
  zh: {
    walletTitle: 'YONA 钱包',
    walletSub: 'YonaCode 主网移动钱包 (Fintech 2026)',
    createWallet: '创建新钱包',
    restoreWallet: '通过助记词导入',
    saveMnemonicTitle: '助记词 (12个单词)',
    saveMnemonicSub: '请妥善保管这12个助记词，这是恢复资产的唯一凭证！',
    iSaved: '我已安全备份',
    enterMnemonicTitle: '输入 12 个助记词',
    mnemonicPlaceholder: '输入以空格分隔的 12 个单词...',
    next: '下一步',
    pinSetupTitle: '设置 6 位 PIN 码',
    newPin: '请输入新的 6 位 PIN 码',
    confirmPin: '请再次确认 6 位 PIN 码',
    loginTitle: '登录钱包',
    switchWallet: '切换 / 创建钱包',
    changeWallet: '切换',

    logout: '退出',
    availableBalance: '可用余额',
    send: '转账',
    receive: '收款',
    network: '网络',
    notifications: '通知',
    newNotif: '条新',
    historyLogs: '条日志',
    txHistory: '交易历史',
    noHistory: '暂无交易记录',
    success: '成功',
    pending: '处理中',
    rejected: '已拒绝',
    immutableLogsTitle: '不可变日志与通知中心',
    clearLogs: '清空日志',
    all: '全部',
    transactions: '交易',
    system: '系统',
    closeLogs: '关闭日志',
    sendTitle: '24/7 GO 转账',
    confirmSendTitle: '确认转账',
    sendAll: '全部转出',
    receiverAddress: '收款 GO 地址',
    receiverPlaceholder: '输入地址 0x...',
    sendAmount: '转账金额 (GO)',
    baseFee: '基础手续费: ~0.00000250 GO',
    speed: '处理速度',
    speedFast: '极速 (~3秒)',
    cancel: '取消',
    back: '← 返回',
    confirmSendBtn: '确认并提交转账',
    preparing: '准备中...',
    signing: '签名中...',
    txSuccessMsg: '交易已成功广播！',
    done: '完成',
    copyAddress: '复制钱包地址',
    copiedAddress: '地址已复制！',
    close: '关闭',
    txDetail: '交易详情',
    closeDetail: '关闭详情',
    senderAcc: '付款方账号',
    receiverAcc: '收款方账号',
    txHash: '交易哈希 (TxID)',
    block: '区块高度',
    unconfirmed: '未确认',
    nonce: '随机数 (Nonce)',
    time: '创建时间',
    fee: '交易手续费',
    amount: '转账金额',
    pinEnterConfirm: '请输入 6 位 PIN 码以签署交易',
    projectedNonce: '预计 Nonce',
    settingsTitle: '系统设置',
    languageLabel: '显示语言 (Language)',
    securityLabel: '安全与账户',
    viewAll: '查看全部 >',
    allTxDisplayed: '已显示所有交易',
    loadMore: '加载更多交易',
    immutableStatus: '不可变状态',
    confirmingStatus: '确认中',
    mempoolPendingStatus: '等待打包',
    blocksWord: '区块',
    blockWord: '区块',
    immutableInfoNote: '✓ 交易已达到 5 次确认。信息已永久记录在不可变账本上。',
    mempoolInfoNote: '⏳ 交易在 Mempool 中等待矿工打包进新区块。',
    confirmingInfoNote: '⏳ 已有 {confirms}/5 个区块确认。还需要 {needed} 个区块（约 {time} 分钟）达到绝对不可变。',
    myWallet: '我的 Yona 钱包',
    online: '在线',
    offline: '离线',
    networkValue: 'PoW 75s / 区块',
  }
};


export default function App() {
  const [screen, setScreen] = useState<'welcome' | 'create' | 'import' | 'pin_setup' | 'login' | 'dashboard'>('welcome');
  const [lang, setLang] = useState<Lang>(() => (localStorage.getItem('yona_wallet_lang') as Lang) || 'vi');

  const str = I18N[lang] || I18N.vi;

  const changeLang = (l: Lang) => {
    setLang(l);
    localStorage.setItem('yona_wallet_lang', l);
  };

  // Wallet Security States
  const [mnemonic, setMnemonic] = useState('');
  const [importMnemonic, setImportMnemonic] = useState('');
  const [passphrase] = useState('');
  const [address, setAddress] = useState('');
  const [walletName] = useState('Ví Yona Của Tôi');

  // PIN States
  const [pin, setPin] = useState('');
  const [confirmPin, setConfirmPin] = useState('');
  const [pinError, setPinError] = useState('');

  // Node Connection
  const storedNodeUrl = localStorage.getItem('yona_node_url');
  const nodeUrl = (!storedNodeUrl || storedNodeUrl.includes('localhost')) ? 'https://explorer.yonacode.com' : storedNodeUrl;
  const nodeToken = localStorage.getItem('yona_node_token') || '';
  const [isNodeConnected, setIsNodeConnected] = useState(false);

  // Financial States
  const [balance, setBalance] = useState<number>(0);
  const [txHistory, setTxHistory] = useState<TxHistoryItem[]>([]);
  const [selectedTx, setSelectedTx] = useState<TxHistoryItem | null>(null);

  // Immutable Notification System State
  const [notifications, setNotifications] = useState<ImmutableNotification[]>(() => {
    try {
      const saved = localStorage.getItem('yona_immutable_notifications');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [isNotificationOpen, setIsNotificationOpen] = useState(false);
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);
  const [isHistoryModalOpen, setIsHistoryModalOpen] = useState(false);
  const [notificationFilter, setNotificationFilter] = useState<'all' | 'tx' | 'system'>('all');


  // Modals & Forms
  const [isSendOpen, setIsSendOpen] = useState(false);
  const [isSendConfirmStep, setIsSendConfirmStep] = useState(false);
  const [isReceiveOpen, setIsReceiveOpen] = useState(false);
  const [sendReceiver, setSendReceiver] = useState('');
  const [sendAmount, setSendAmount] = useState('');
  const [sendPin, setSendPin] = useState('');
  const [sendError, setSendError] = useState('');
  const [isSending, setIsSending] = useState(false);
  const [sendSuccess, setSendSuccess] = useState(false);
  const [sendTxId, setSendTxId] = useState('');
  const [copied, setCopied] = useState(false);
  const [preparedTx, setPreparedTx] = useState<PreparedTx | null>(null);
  const [isPreparingTx, setIsPreparingTx] = useState(false);

  // Persistence for notifications
  const saveNotifications = (newNotifs: ImmutableNotification[]) => {
    setNotifications(newNotifs);
    try {
      localStorage.setItem('yona_immutable_notifications', JSON.stringify(newNotifs));
    } catch (e) {
      console.error('Lỗi lưu thông báo bất biến:', e);
    }
  };

  const addNotification = (item: Omit<ImmutableNotification, 'id' | 'timestamp' | 'read'>) => {
    const newNotif: ImmutableNotification = {
      ...item,
      id: 'notif_' + Date.now() + '_' + Math.random().toString(36).substring(2, 6),
      timestamp: Date.now(),
      read: false
    };
    setNotifications(prev => {
      const updated = [newNotif, ...prev];
      try {
        localStorage.setItem('yona_immutable_notifications', JSON.stringify(updated));
      } catch {}
      return updated;
    });
  };

  const markAllNotificationsRead = () => {
    saveNotifications(notifications.map(n => ({ ...n, read: true })));
  };

  const clearNotifications = () => {
    saveNotifications([]);
  };

  // Load wallet on init
  useEffect(() => {
    const savedAddr = localStorage.getItem('yona_wallet_addr');
    const isUnlocked = sessionStorage.getItem('yona_wallet_unlocked') === 'true';
    if (savedAddr) {
      setAddress(savedAddr);
      if (isUnlocked) {
        setScreen('dashboard');
      } else {
        setScreen('login');
      }
    } else {
      setScreen('welcome');
    }
  }, []);

  const fetchWithAuth = async (path: string, options: RequestInit = {}) => {
    const headers = new Headers(options.headers || {});
    if (nodeToken) {
      headers.set('Authorization', `Bearer ${nodeToken}`);
    }
    return fetch(`${nodeUrl}${path}`, { ...options, headers });
  };

  const checkNodeConnection = async () => {
    try {
      const res = await fetchWithAuth('/api/v1/status');
      if (res.ok) {
        setIsNodeConnected(true);
      } else {
        setIsNodeConnected(false);
      }
    } catch {
      setIsNodeConnected(false);
    }
  };

  const fetchAccountData = async () => {
    if (!address) return;
    try {
      const res = await fetchWithAuth(`/api/v1/balance/${address}`);
      if (res.ok) {
        const data = await res.json();
        const balVal = data.balances && data.balances.btc_z !== undefined ? data.balances.btc_z : (data.balance || 0);
        const newBal = balVal / 1e8;
        if (balance > 0 && newBal > balance) {
          addNotification({
            title: lang === 'vi' ? '💰 Số dư vừa tăng!' : lang === 'en' ? '💰 Balance Increased!' : '💰 余额已增加！',
            message: `+${(newBal - balance).toFixed(4)} GO`,
            type: 'success'
          });
        }
        setBalance(newBal);
        setIsNodeConnected(true);
      }
    } catch (e) {
      console.error('Lỗi tải số dư:', e);
    }
  };

  const loadLocalHistory = (addr: string): TxHistoryItem[] => {
    try {
      const saved = localStorage.getItem('yona_local_tx_history_' + addr);
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  };

  const saveLocalHistory = (addr: string, list: TxHistoryItem[]) => {
    try {
      localStorage.setItem('yona_local_tx_history_' + addr, JSON.stringify(list));
    } catch (e) {
      console.error('Lỗi lưu lịch sử local:', e);
    }
  };

  const fetchHistory = async () => {
    if (!address) return;
    const localSaved = loadLocalHistory(address);

    try {
      const res = await fetchWithAuth(`/api/v1/address/${address}/history`);
      let serverMapped: TxHistoryItem[] = [];
      if (res.ok) {
        const data = await res.json();
        if (data.history) {
          serverMapped = data.history.map((h: any) => {
             let mappedStatus = 99;
             const code = h.status_code !== undefined ? h.status_code : h.StatusCode;
             if (code !== undefined) {
               if (code === 1) mappedStatus = 0;
               else if (code === 0) mappedStatus = 99;
               else mappedStatus = 1;
             } else if (h.status !== undefined) {
               if (h.status === 0 || h.status === 'FINALIZED' || h.status === 'SUCCESS' || h.status === 'success') mappedStatus = 0;
               else if (h.status === 99 || h.status === 'PENDING' || h.status === 'MEMPOOL' || h.status === 'mempool') mappedStatus = 99;
               else mappedStatus = 1;
             }

            return {
              txid: h.txid || h.id || h.ID || '',
              sender: h.sender || h.Sender || '',
              receiver: h.receiver || h.Receiver || '',
              amount: h.amount !== undefined ? h.amount : (h.Amount !== undefined ? h.Amount : 0),
              fee: h.fee !== undefined ? h.fee : (h.Fee !== undefined ? h.Fee : 0),
              nonce: h.nonce !== undefined ? h.nonce : (h.Nonce !== undefined ? h.Nonce : 0),
              timestamp: h.timestamp !== undefined ? h.timestamp : (h.Timestamp !== undefined ? h.Timestamp : 0),
              status: mappedStatus,
              blockHeight: h.blockHeight !== undefined ? h.blockHeight : (h.height !== undefined ? h.height : 0),
              confirmations: h.confirmations !== undefined ? h.confirmations : 0
            };
          });
        }
      }

      // Hợp nhất lịch sử từ Server với Cache Cục bộ (Bảo đảm không bao giờ mất các giao dịch thất bại)
      const txMap = new Map<string, TxHistoryItem>();

      // Đưa cache local vào trước
      localSaved.forEach(tx => {
        if (tx.txid) txMap.set(tx.txid.toLowerCase(), tx);
      });

      // Ghi đè thông tin chính xác từ server lên
      serverMapped.forEach(tx => {
        if (tx.txid) txMap.set(tx.txid.toLowerCase(), tx);
      });

      const mergedList = Array.from(txMap.values());

      // Kiểm tra timeout cho các giao dịch pending cục bộ quá 10 phút mà không có trên server
      const nowSec = Math.floor(Date.now() / 1000);
      mergedList.forEach(tx => {
        if (tx.status === 99 && nowSec - tx.timestamp > 600) {
          tx.status = 1; // Thất bại / Hết hạn
        }
      });

      // Sắp xếp theo timestamp giảm dần
      mergedList.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));

      // Tự động phát hiện thay đổi trạng thái giao dịch để sinh Thông báo Bất biến
      txHistory.forEach(oldTx => {
        if (oldTx.status === 99) {
          const updated = mergedList.find(m => m.txid === oldTx.txid);
          if (updated && updated.status === 0) {
            addNotification({
              title: lang === 'vi' ? '🎉 Giao dịch đã vào Khối!' : lang === 'en' ? '🎉 Transaction Mined!' : '🎉 交易已被打包！',
              message: `TxID: ${oldTx.txid.slice(0, 10)}... (Block #${updated.blockHeight || ''})`,
              type: 'success',
              txid: oldTx.txid
            });
          } else if (updated && updated.status === 1) {
            addNotification({
              title: lang === 'vi' ? '❌ Giao dịch bị từ chối' : lang === 'en' ? '❌ Transaction Rejected' : '❌ 交易已被拒绝',
              message: `TxID: ${oldTx.txid.slice(0, 10)}...`,
              type: 'error',
              txid: oldTx.txid
            });
          }
        }
      });

      saveLocalHistory(address, mergedList);
      setTxHistory(mergedList);
    } catch (e) {
      console.error('Lỗi tải lịch sử:', e);
      if (localSaved.length > 0) setTxHistory(localSaved);
    }
  };

  useEffect(() => {
    if (!address || screen === 'welcome') return;
    checkNodeConnection();
    fetchAccountData();
    fetchHistory();
    const interval = setInterval(() => {
      fetchAccountData();
      fetchHistory();
    }, 4000);
    return () => clearInterval(interval);
  }, [address, screen]);

  const handleCreateNewWallet = () => {
    const newMnemonic = generateNewMnemonic();
    setMnemonic(newMnemonic);
    setScreen('create');
  };

  const handleConfirmMnemonicCreated = async () => {
    setScreen('pin_setup');
  };

  const handleImportWallet = () => {
    if (!isValidMnemonic(importMnemonic)) {
      setPinError('Mnemonic invalid!');
      return;
    }
    setMnemonic(importMnemonic.trim());
    setScreen('pin_setup');
  };

  const handleSavePinSetup = async () => {
    if (pin.length < 6) {
      setPinError('PIN must be 6 digits');
      return;
    }
    if (pin !== confirmPin) {
      setPinError('PIN mismatch');
      return;
    }

    try {
      const derived = await deriveKeyPairFromMnemonic(mnemonic, passphrase);
      const encrypted = await encryptPrivateKey(derived.privateKey, pin);

      localStorage.setItem('yona_wallet_addr', derived.address);
      localStorage.setItem('yona_wallet_enc_key', encrypted.encryptedHex);
      localStorage.setItem('yona_wallet_salt', encrypted.saltHex);
      localStorage.setItem('yona_wallet_iv', encrypted.ivHex);
      localStorage.setItem('yona_wallet_name', walletName);
      sessionStorage.setItem('yona_wallet_unlocked', 'true');

      addNotification({
        title: '🛡️ Wallet Initialized',
        message: `${derived.address.slice(0, 10)}...`,
        type: 'info'
      });

      setAddress(derived.address);
      setScreen('dashboard');
    } catch (e: any) {
      setPinError('Error: ' + e.message);
    }
  };

  const handleLoginPin = async (inputPin?: string) => {
    setPinError('');
    const pinToTry = inputPin || pin;
    const encKey = localStorage.getItem('yona_wallet_enc_key');
    const salt = localStorage.getItem('yona_wallet_salt');
    const iv = localStorage.getItem('yona_wallet_iv');

    if (!encKey || !salt || !iv) {
      setScreen('welcome');
      return;
    }

    if (pinToTry.length < 6) {
      setPinError('PIN must be 6 digits');
      return;
    }

    try {
      await decryptPrivateKey(encKey, salt, iv, pinToTry);
      sessionStorage.setItem('yona_wallet_unlocked', 'true');
      setScreen('dashboard');
    } catch {
      setPinError('PIN incorrect!');
    }
  };

  const handlePinDigitPress = (digit: string) => {
    if (screen === 'pin_setup') {
      if (pin.length < 6) setPin(prev => prev + digit);
      else if (confirmPin.length < 6) setConfirmPin(prev => prev + digit);
    } else if (screen === 'login') {
      if (pin.length < 6) {
        setPin(prev => prev + digit);
      }
    }
  };

  const handleSendPinDigitPress = (digit: string) => {
    if (sendPin.length < 6) {
      setSendPin(prev => prev + digit);
    }
  };

  const handleSendPinDelete = () => {
    if (sendPin.length > 0) {
      setSendPin(prev => prev.slice(0, -1));
    }
  };

  const handlePinDelete = () => {
    if (screen === 'pin_setup') {
      if (confirmPin.length > 0) setConfirmPin(prev => prev.slice(0, -1));
      else if (pin.length > 0) setPin(prev => prev.slice(0, -1));
    } else {
      if (pin.length > 0) setPin(prev => prev.slice(0, -1));
    }
  };

  const handlePrepareAndContinue = async () => {
    if (!sendReceiver.trim()) { setSendError('Please enter receiver address'); return; }
    const amountVal = parseFloat(sendAmount);
    if (isNaN(amountVal) || amountVal <= 0) { setSendError('Invalid amount'); return; }
    if (amountVal > balance) { setSendError('Insufficient balance'); return; }

    setSendError('');
    setIsPreparingTx(true);

    try {
      const amountVnt = Math.round(amountVal * 1e8);
      const res = await fetchWithAuth('/api/v1/prepare_tx', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sender: address,
          receiver: sendReceiver.trim(),
          amount: amountVnt
        })
      });

      if (!res.ok) {
        const txt = await res.text();
        let msg = 'Failed to prepare transaction';
        try {
          const parsed = JSON.parse(txt);
          msg = parsed.error || parsed.message || txt;
        } catch {
          msg = txt;
        }
        setSendError(msg);
        return;
      }

      const prepData: PreparedTx = await res.json();
      if (!prepData.success) {
        setSendError(prepData.error || 'Node rejected unsigned tx preparation');
        return;
      }

      setPreparedTx(prepData);
      setIsSendConfirmStep(true);
    } catch (e: any) {
      setSendError(e.message || 'Node connection error');
    } finally {
      setIsPreparingTx(false);
    }
  };

  const handleSendPinSubmit = async () => {
    if (!preparedTx) {
      setSendError('No prepared transaction. Please try again.');
      return;
    }
    if (sendPin.length < 6) {
      setSendError('PIN must be 6 digits');
      return;
    }

    setIsSending(true);
    setSendError('');

    try {
      const encKey = localStorage.getItem('yona_wallet_enc_key');
      const salt = localStorage.getItem('yona_wallet_salt');
      const iv = localStorage.getItem('yona_wallet_iv');
      if (!encKey || !salt || !iv) throw new Error('Wallet data not found');

      const privKey = await decryptPrivateKey(encKey, salt, iv, sendPin);
      const signatureHex = signPreparedHash(preparedTx.signing_hash, privKey);

      const signedTxPayload = {
        version: preparedTx.version,
        sender: preparedTx.sender,
        receiver: preparedTx.receiver,
        amount: preparedTx.amount,
        fee: preparedTx.fee,
        nonce: preparedTx.nonce,
        timestamp: preparedTx.timestamp,
        recent_block_hash: preparedTx.recent_block_hash,
        signature: signatureHex,
        chain_id: preparedTx.chain_id || 0
      };

      const res = await fetchWithAuth('/api/v1/send_raw_tx', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(signedTxPayload)
      });

      if (res.ok) {
        const data = await res.json();
        const generatedTxId = data.txid || '0x...';
        setSendTxId(generatedTxId);
        setSendSuccess(true);

        addNotification({
          title: '🚀 Transaction Dispatched',
          message: `${(preparedTx.amount / 1e8).toFixed(4)} GO -> Mempool`,
          type: 'success',
          txid: generatedTxId
        });

        // Lưu ngay vào Cache Local để đảm bảo không bị mất lịch sử kể cả khi bị từ chối
        const newLocalTx: TxHistoryItem = {
          txid: generatedTxId,
          sender: address,
          receiver: preparedTx.receiver,
          amount: preparedTx.amount,
          fee: preparedTx.fee,
          nonce: preparedTx.nonce,
          timestamp: Math.floor(Date.now() / 1000),
          status: 99,
          blockHeight: 0,
          confirmations: 0
        };
        const currentLoc = loadLocalHistory(address);
        saveLocalHistory(address, [newLocalTx, ...currentLoc]);

        setSendAmount('');
        setSendReceiver('');
        setSendPin('');
        setPreparedTx(null);
        fetchAccountData();
        fetchHistory();

      } else {
        const txt = await res.text();
        let msg = 'Failed to broadcast transaction';
        let failedTxId = '0x' + Math.random().toString(36).substring(2, 10);
        try {
          const parsed = JSON.parse(txt);
          msg = parsed.error || parsed.message || txt;
          if (parsed.txid) failedTxId = parsed.txid;
        } catch {
          msg = txt;
        }
        setSendError(msg);

        // Lưu ngay thông tin giao dịch thất bại vào Cache Local để hiển thị trung thực 100%
        const failedLocalTx: TxHistoryItem = {
          txid: failedTxId,
          sender: address,
          receiver: preparedTx.receiver,
          amount: preparedTx.amount,
          fee: preparedTx.fee,
          nonce: preparedTx.nonce,
          timestamp: Math.floor(Date.now() / 1000),
          status: 1, // Bị từ chối / Thất bại
          blockHeight: 0,
          confirmations: 0
        };
        const currentLoc = loadLocalHistory(address);
        saveLocalHistory(address, [failedLocalTx, ...currentLoc]);
        fetchHistory();
      }

    } catch (e: any) {
      setSendError(e.message || 'Signature failed or incorrect PIN');
    } finally {
      setIsSending(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleLogout = () => {
    sessionStorage.removeItem('yona_wallet_unlocked');
    setPin('');
    setScreen('login');
  };

  const handleSwitchWallet = () => {
    localStorage.removeItem('yona_wallet_addr');
    localStorage.removeItem('yona_wallet_enc_key');
    localStorage.removeItem('yona_wallet_salt');
    localStorage.removeItem('yona_wallet_iv');
    sessionStorage.removeItem('yona_wallet_unlocked');
    setPin('');
    setAddress('');
    setImportMnemonic('');
    setScreen('welcome');
  };

  const formatDate = (ts: number) => {
    if (!ts) return 'N/A';
    const date = new Date(ts > 1e11 ? ts : ts * 1000);
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}:${date.getSeconds().toString().padStart(2, '0')} ${date.getDate()}/${date.getMonth() + 1}/${date.getFullYear()}`;
  };

  const unreadCount = notifications.filter(n => !n.read).length;

  const filteredNotifications = notifications.filter(n => {
    if (notificationFilter === 'tx') return !!n.txid;
    if (notificationFilter === 'system') return !n.txid;
    return true;
  });

  return (
    <div className="h-full flex flex-col justify-between p-5 overflow-y-auto font-sans bg-white relative">
      {/* Confetti Celebration Particle Layer */}
      {sendSuccess && (
        <div className="confetti-container">
          {Array.from({ length: 35 }).map((_, i) => {
            const colors = ['#2563eb', '#10b981', '#f59e0b', '#ec4899', '#8b5cf6', '#06b6d4'];
            const color = colors[i % colors.length];
            const left = (i * 2.8) + '%';
            const delay = (i * 0.04) + 's';
            return (
              <div
                key={i}
                className="confetti-piece"
                style={{
                  left,
                  backgroundColor: color,
                  animationDelay: delay,
                }}
              />
            );
          })}
        </div>
      )}

      {screen === 'welcome' && (
        <div className="flex-1 flex flex-col items-center justify-center text-center p-2 pt-6">
          <div className="w-16 h-16 bg-blue-50 text-blue-600 rounded-3xl flex items-center justify-center mb-6 shadow-sm border border-blue-100">
            <Shield size={36} />
          </div>
          <h1 className="text-2xl font-black italic mb-2 tracking-tight text-slate-900">{str.walletTitle}</h1>
          <p className="text-slate-500 text-xs mb-8 font-medium">{str.walletSub}</p>
          <button 
            onClick={handleCreateNewWallet}
            className="w-full pill-btn pill-btn-primary mb-3 shadow-lg shadow-blue-600/20 active:scale-95"
          >
            {str.createWallet}
          </button>
          <button 
            onClick={() => setScreen('import')}
            className="w-full pill-btn pill-btn-secondary active:scale-95"
          >
            {str.restoreWallet}
          </button>
        </div>
      )}

      {screen === 'create' && (
        <div className="flex-1 flex flex-col justify-between pt-6">
          <div>
            <h2 className="text-lg font-bold mb-2 text-blue-600 flex items-center gap-2">
              <Key size={18} /> {str.saveMnemonicTitle}
            </h2>
            <p className="text-xs text-slate-500 mb-4">
              {str.saveMnemonicSub}
            </p>
            <div className="grid grid-cols-3 gap-2 bg-slate-50 p-3 rounded-2xl border border-slate-200 mb-4">
              {mnemonic.split(' ').map((word, i) => (
                <div key={i} className="text-xs p-2 bg-white rounded-xl text-slate-700 font-mono text-center shadow-sm border border-slate-100">
                  <span className="text-slate-400 mr-1">{i + 1}.</span>{word}
                </div>
              ))}
            </div>
          </div>
          <button 
            onClick={handleConfirmMnemonicCreated}
            className="w-full pill-btn pill-btn-primary shadow-md shadow-blue-600/20 active:scale-95"
          >
            {str.iSaved}
          </button>
        </div>
      )}

      {screen === 'import' && (
        <div className="flex-1 flex flex-col justify-between pt-6">
          <div>
            <h2 className="text-lg font-bold mb-2 text-blue-600 flex items-center gap-2">
              <Key size={18} /> {str.enterMnemonicTitle}
            </h2>
            <textarea 
              rows={4}
              value={importMnemonic}
              onChange={(e) => setImportMnemonic(e.target.value)}
              placeholder={str.mnemonicPlaceholder}
              className="w-full p-3.5 rounded-2xl bg-slate-50 border border-slate-200 text-slate-900 font-mono text-xs mb-3 focus:outline-none focus:border-blue-600"
            />
            {pinError && <p className="text-red-500 text-xs mb-3 font-medium">{pinError}</p>}
          </div>
          <button 
            onClick={handleImportWallet}
            className="w-full pill-btn pill-btn-primary shadow-md shadow-blue-600/20 active:scale-95"
          >
            {str.next}
          </button>
        </div>
      )}

      {screen === 'pin_setup' && (
        <div className="flex-1 flex flex-col justify-between pt-6">
          <div>
            <h2 className="text-lg font-bold mb-1 text-blue-600 flex items-center justify-center gap-2">
              <Lock size={18} /> {str.pinSetupTitle}
            </h2>
            <p className="text-xs text-slate-500 text-center mb-4">
              {pin.length < 6 ? str.newPin : str.confirmPin}
            </p>

            {/* PIN Dots */}
            <div className="pin-dots mb-4">
              {Array.from({ length: 6 }).map((_, i) => {
                const filled = pin.length < 6 ? i < pin.length : i < confirmPin.length;
                return (
                  <div key={i} className={`pin-dot ${filled ? 'filled' : ''}`}></div>
                );
              })}
            </div>

            {pinError && <p className="text-red-500 text-xs text-center mb-3 font-medium">{pinError}</p>}

            {/* PIN Pad */}
            <div className="pin-pad">
              {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map(num => (
                <button key={num} onClick={() => handlePinDigitPress(num)} className="pin-btn">
                  {num}
                </button>
              ))}
              <button onClick={handlePinDelete} className="pin-btn text-xs font-bold text-slate-500">DEL</button>
              <button onClick={() => handlePinDigitPress('0')} className="pin-btn">0</button>
              <button 
                onClick={handleSavePinSetup}
                className="pin-btn !bg-blue-600 !text-white !border-blue-600 text-xs font-bold shadow-md shadow-blue-600/30"
              >
                OK
              </button>
            </div>
          </div>
        </div>
      )}

      {screen === 'login' && (
        <div className="flex-1 flex flex-col items-center justify-between py-2 pt-6">
          <div className="text-center w-full">
            <div className="w-14 h-14 bg-blue-50 text-blue-600 rounded-3xl flex items-center justify-center mx-auto mb-3 shadow-sm border border-blue-100">
              <Lock size={26} />
            </div>
            <h2 className="text-xl font-bold text-slate-900 mb-1">{str.loginTitle}</h2>
            <p className="text-slate-500 text-[11px] font-mono truncate max-w-[260px] mx-auto bg-slate-100 p-1.5 rounded-xl border border-slate-200">
              {address}
            </p>

            {/* PIN Dots */}
            <div className="pin-dots my-4">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className={`pin-dot ${i < pin.length ? 'filled' : ''}`}></div>
              ))}
            </div>

            {pinError && <p className="text-red-500 text-xs text-center mb-2 font-medium">{pinError}</p>}
          </div>

          {/* Circular PIN Keyboard */}
          <div className="pin-pad my-auto">
            {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map(num => (
              <button key={num} onClick={() => handlePinDigitPress(num)} className="pin-btn">
                {num}
              </button>
            ))}
            <button onClick={handlePinDelete} className="pin-btn text-xs font-bold text-slate-500">DEL</button>
            <button onClick={() => handlePinDigitPress('0')} className="pin-btn">0</button>
            <button 
              onClick={() => handleLoginPin()}
              className="pin-btn !bg-blue-600 !text-white !border-blue-600 text-xs font-bold shadow-md shadow-blue-600/30 active:scale-95"
            >
              OK
            </button>
          </div>

          <div className="w-full text-center mt-3 pt-3 border-t border-slate-100 flex flex-col gap-2">
            <button 
              onClick={handleSwitchWallet}
              className="py-2.5 px-4 bg-blue-50 hover:bg-blue-100 text-blue-600 rounded-2xl text-xs font-bold transition-all border border-blue-200 flex items-center justify-center gap-1.5 shadow-sm"
            >
              {str.switchWallet}
            </button>
          </div>
        </div>
      )}

      {screen === 'dashboard' && (
        <div className="flex-1 flex flex-col justify-between gap-3 fade-in pt-3">
          <div>
            {/* Top Bar Header with Bell and Settings */}
            <div className="flex justify-between items-center pt-2 pb-3.5 mb-3 border-b border-slate-100 gap-3 shrink-0">
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="w-10 h-10 shrink-0 bg-blue-50 text-blue-600 rounded-2xl flex items-center justify-center font-bold shadow-xs border border-blue-100">
                  <Shield size={20} />
                </div>
                <div className="min-w-0">
                  <h3 className="font-extrabold text-slate-900 text-xs truncate leading-tight">{str.myWallet}</h3>
                  <div className="flex items-center gap-1.5 text-[10px] text-slate-500 font-bold mt-0.5">
                    <span className={`w-2 h-2 rounded-full shrink-0 ${isNodeConnected ? 'bg-emerald-500 animate-pulse' : 'bg-red-500'}`}></span>
                    {isNodeConnected ? str.online : str.offline}
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                {/* Immutable Notification Bell Button */}
                <button 
                  onClick={() => { setIsNotificationOpen(true); markAllNotificationsRead(); }} 
                  className="relative w-10 h-10 bg-slate-100 hover:bg-slate-200 text-slate-700 rounded-2xl flex items-center justify-center transition-all border border-slate-200/80 active:scale-95 cursor-pointer"
                  title={str.notifications}
                >
                  <Bell size={20} />
                  {unreadCount > 0 && (
                    <span className="absolute -top-1 -right-1 bg-red-500 text-white font-black text-[9px] w-4.5 h-4.5 rounded-full flex items-center justify-center border-2 border-white">
                      {unreadCount}
                    </span>
                  )}
                </button>
                {/* Settings Gear Button */}
                <button 
                  onClick={() => setIsSettingsOpen(true)} 
                  className="w-10 h-10 bg-slate-100 hover:bg-slate-200 text-slate-700 rounded-2xl flex items-center justify-center transition-all border border-slate-200/80 active:scale-95 cursor-pointer"
                  title={str.settingsTitle}
                >
                  <Settings size={20} />
                </button>
              </div>
            </div>


            {/* Pure White Balance Card (Clean Revolut / Wise Style) */}
            <div className="bg-white p-5 mb-3 text-center rounded-3xl border border-slate-200 shadow-xs">
              <div className="flex justify-between items-center mb-1">
                <span className="text-[10px] uppercase tracking-widest text-slate-400 font-extrabold">{str.availableBalance}</span>
                <span className="text-[10px] bg-blue-50 text-blue-600 px-2.5 py-0.5 rounded-full font-mono font-bold border border-blue-100">Mainnet PoW</span>
              </div>

              <div className="text-3xl font-black my-2 tracking-tight text-slate-900">
                {balance.toLocaleString('en-US', { minimumFractionDigits: 4, maximumFractionDigits: 8 })} <span className="text-sm font-bold text-blue-600">GO</span>
              </div>

              <div className="flex items-center justify-center gap-1.5 text-[10px] text-slate-600 bg-slate-50 px-3 py-1.5 rounded-full mt-2 font-mono border border-slate-200">
                <span className="truncate max-w-[210px] font-bold">{address}</span>
                <button onClick={() => copyToClipboard(address)} className="text-slate-500 hover:text-blue-600 shrink-0 p-0.5">
                  {copied ? <Check size={13} className="text-emerald-600" /> : <Copy size={13} />}
                </button>
              </div>

              <div className="grid grid-cols-2 gap-2 mt-4">
                <button 
                  onClick={() => { setIsSendOpen(true); setSendError(''); setSendSuccess(false); }}
                  className="pill-btn pill-btn-primary shadow-md shadow-blue-600/20 active:scale-95"
                >
                  <Send size={15} /> {str.send}
                </button>
                <button 
                  onClick={() => setIsReceiveOpen(true)}
                  className="pill-btn pill-btn-secondary active:scale-95"
                >
                  <ArrowDownLeft size={15} /> {str.receive}
                </button>
              </div>
            </div>

            {/* Bento Grid Stats Widgets */}
            <div className="bento-grid mb-3">
              <div className="bento-card flex items-center gap-3">
                <div className="w-9 h-9 rounded-2xl bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0 border border-emerald-100">
                  <Sparkles size={18} />
                </div>
                <div className="min-w-0">
                  <div className="text-[10px] font-bold text-slate-400 uppercase">{str.network}</div>
                  <div className="text-xs font-black text-slate-800 truncate">{str.networkValue}</div>
                </div>
              </div>

              <div 
                onClick={() => { setIsNotificationOpen(true); markAllNotificationsRead(); }}
                className="bento-card flex items-center gap-3 cursor-pointer hover:border-blue-300 transition-colors"
              >
                <div className="w-9 h-9 rounded-2xl bg-blue-50 text-blue-600 flex items-center justify-center shrink-0 border border-blue-100 relative">
                  <Bell size={18} />
                  {unreadCount > 0 && <span className="absolute top-1 right-1 w-2 h-2 rounded-full bg-red-500 animate-ping"></span>}
                </div>
                <div className="min-w-0">
                  <div className="text-[10px] font-bold text-slate-400 uppercase">{str.notifications}</div>
                  <div className="text-xs font-black text-slate-800 truncate">
                    {unreadCount > 0 ? `${unreadCount} ${str.newNotif}` : `${notifications.length} ${str.historyLogs}`}
                  </div>
                </div>
              </div>
            </div>

            {/* Transaction History Section */}
            <div className="bg-slate-50 p-4 rounded-3xl border border-slate-200">
              <div className="flex justify-between items-center mb-3">
                <h4 className="font-extrabold text-xs uppercase tracking-wider text-slate-700">{str.txHistory}</h4>
                <div className="flex items-center gap-2">
                  <button 
                    onClick={() => setIsHistoryModalOpen(true)}
                    className="text-[11px] font-extrabold text-blue-600 hover:text-blue-700 transition-colors cursor-pointer"
                  >
                    {str.viewAll}
                  </button>
                  <button onClick={fetchHistory} className="p-1.5 hover:bg-slate-200 rounded-xl text-slate-500 transition-colors cursor-pointer" title="Refresh">
                    <RefreshCw size={14} />
                  </button>
                </div>
              </div>

              {txHistory.length === 0 ? (
                <p className="text-xs text-slate-400 text-center py-6">{str.noHistory}</p>
              ) : (
                <div className="space-y-3.5 max-h-72 overflow-y-auto pr-1">
                  {txHistory.map((tx, idx) => {
                    const isOut = tx.sender.toLowerCase() === address.toLowerCase();
                    const confirms = Math.min(5, Math.max(0, tx.confirmations || (tx.status === 0 ? 1 : 0)));
                    const isImmutable = tx.status === 0 && confirms >= 5;
                    const isRejected = tx.status === 1;

                    return (
                      <div 
                        key={idx} 
                        onClick={() => setSelectedTx(tx)}
                        className="flex justify-between items-center px-4.5 py-4 bg-white hover:bg-slate-100/90 rounded-2xl border border-slate-200 text-xs cursor-pointer transition-all active:scale-[0.99] shadow-2xs gap-3"
                      >
                        <div className="flex items-center gap-3 min-w-0 flex-1">
                          <div className={`p-2.5 rounded-2xl shrink-0 ${isOut ? 'bg-red-50 text-red-600 border border-red-100' : 'bg-emerald-50 text-emerald-600 border border-emerald-100'}`}>
                            {isOut ? <ArrowUpRight size={16} /> : <ArrowDownLeft size={16} />}
                          </div>
                          <div className="space-y-0.5 min-w-0 flex-1">
                            <div className="font-mono text-slate-900 font-extrabold text-xs truncate">{tx.txid.slice(0, 8)}...{tx.txid.slice(-4)}</div>
                            <div className="text-[10px] text-slate-400 font-mono font-bold truncate">Nonce: #{tx.nonce} • {formatDate(tx.timestamp)}</div>
                          </div>
                        </div>

                        <div className="text-right space-y-1 shrink-0 flex flex-col items-end">
                          <div className={`font-black text-sm ${isOut ? 'text-red-600' : 'text-emerald-600'}`}>
                            {isOut ? '-' : '+'}{(tx.amount / 1e8).toFixed(4)} GO
                          </div>

                          {isRejected ? (
                            <span className="text-[9px] px-2.5 py-0.5 rounded-full font-extrabold bg-red-100 text-red-800 border border-red-200">
                              ❌ {str.rejected}
                            </span>
                          ) : isImmutable ? (
                            <span className="text-[9px] px-2.5 py-0.5 rounded-full font-extrabold bg-emerald-100 text-emerald-800 border border-emerald-200">
                              🛡️ {str.success} (5/5)
                            </span>
                          ) : (
                            <div className="flex items-center gap-1.5 bg-amber-50 text-amber-800 border border-amber-200/80 px-2 py-0.5 rounded-full">
                              <span className="text-[9px] font-extrabold">⏳ {confirms}/5 {str.blocksWord}</span>
                              <div className="w-8 h-1 bg-amber-200 rounded-full overflow-hidden shrink-0">
                                <div 
                                  className="h-full bg-amber-500 rounded-full transition-all" 
                                  style={{ width: `${(confirms / 5) * 100}%` }}
                                ></div>
                              </div>
                            </div>
                          )}
                        </div>
                      </div>
                    );
                  })}
                  <div className="text-center pt-2 pb-1">
                    <span className="text-[10px] text-slate-400 font-medium italic">{str.allTxDisplayed}</span>
                  </div>
                </div>
              )}



            </div>
          </div>
        </div>
      )}

      {/* Settings Modal (Cài đặt & Đổi ngôn ngữ) */}
      {isSettingsOpen && (
        <div className="absolute inset-0 bg-white z-50 flex flex-col h-full overflow-hidden fade-in">
          {/* Top Bar Header with proper top padding */}
          <div className="flex justify-between items-center px-6 pt-7 pb-4 border-b border-slate-100 shrink-0 bg-white">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 bg-blue-50 text-blue-600 rounded-2xl flex items-center justify-center font-bold border border-blue-100 shadow-xs">
                <Settings size={20} />
              </div>
              <div>
                <h3 className="text-base font-black text-slate-900 tracking-tight">{str.settingsTitle}</h3>
                <span className="text-[10px] text-slate-400 font-bold uppercase tracking-wider">System & Preferences</span>
              </div>
            </div>
            <button 
              onClick={() => setIsSettingsOpen(false)} 
              className="w-10 h-10 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center justify-center transition-colors active:scale-95 cursor-pointer border border-slate-200/60"
            >
              <X size={20} />
            </button>
          </div>


          <div className="flex-1 overflow-y-auto p-6 space-y-6">
            {/* Language Selector */}
            <div className="space-y-3">
              <label className="text-[11px] font-extrabold text-slate-400 uppercase tracking-widest flex items-center gap-1.5">
                <Globe size={14} className="text-blue-600" /> {str.languageLabel}
              </label>
              <div className="grid grid-cols-1 gap-2.5">
                <button 
                  onClick={() => changeLang('vi')}
                  className={`p-4 rounded-2xl transition-all flex items-center justify-between active:scale-[0.98] ${
                    lang === 'vi' 
                      ? 'bg-gradient-to-r from-blue-600 to-indigo-600 text-white font-extrabold shadow-lg shadow-blue-600/20 border border-blue-500' 
                      : 'bg-slate-50 hover:bg-slate-100 text-slate-800 font-bold border border-slate-200'
                  }`}
                >
                  <span className="flex items-center gap-3 text-xs">
                    <span className="text-lg">🇻🇳</span>
                    <span>Tiếng Việt <span className={`text-[10px] ml-1 font-mono ${lang === 'vi' ? 'text-blue-200' : 'text-slate-400'}`}>(Vietnamese)</span></span>
                  </span>
                  {lang === 'vi' && <CheckCircle2 size={18} className="text-white shrink-0" />}
                </button>

                <button 
                  onClick={() => changeLang('en')}
                  className={`p-4 rounded-2xl transition-all flex items-center justify-between active:scale-[0.98] ${
                    lang === 'en' 
                      ? 'bg-gradient-to-r from-blue-600 to-indigo-600 text-white font-extrabold shadow-lg shadow-blue-600/20 border border-blue-500' 
                      : 'bg-slate-50 hover:bg-slate-100 text-slate-800 font-bold border border-slate-200'
                  }`}
                >
                  <span className="flex items-center gap-3 text-xs">
                    <span className="text-lg">🇬🇧</span>
                    <span>English <span className={`text-[10px] ml-1 font-mono ${lang === 'en' ? 'text-blue-200' : 'text-slate-400'}`}>(United States)</span></span>
                  </span>
                  {lang === 'en' && <CheckCircle2 size={18} className="text-white shrink-0" />}
                </button>

                <button 
                  onClick={() => changeLang('zh')}
                  className={`p-4 rounded-2xl transition-all flex items-center justify-between active:scale-[0.98] ${
                    lang === 'zh' 
                      ? 'bg-gradient-to-r from-blue-600 to-indigo-600 text-white font-extrabold shadow-lg shadow-blue-600/20 border border-blue-500' 
                      : 'bg-slate-50 hover:bg-slate-100 text-slate-800 font-bold border border-slate-200'
                  }`}
                >
                  <span className="flex items-center gap-3 text-xs">
                    <span className="text-lg">🇨🇳</span>
                    <span>简体中文 <span className={`text-[10px] ml-1 font-mono ${lang === 'zh' ? 'text-blue-200' : 'text-slate-400'}`}>(Chinese Simplified)</span></span>
                  </span>
                  {lang === 'zh' && <CheckCircle2 size={18} className="text-white shrink-0" />}
                </button>
              </div>
            </div>

            {/* Security Management */}
            <div className="pt-6 border-t border-slate-100">
              <label className="text-[11px] font-extrabold text-slate-400 uppercase tracking-widest block mb-3">
                {str.securityLabel}
              </label>
              <div className="space-y-3.5">
                <button 
                  onClick={() => { setIsSettingsOpen(false); handleSwitchWallet(); }}
                  className="w-full px-5 py-4 min-h-[56px] bg-slate-50 hover:bg-slate-100/90 text-slate-900 font-bold text-xs rounded-2xl border border-slate-200/80 transition-all flex items-center justify-between active:scale-[0.98] cursor-pointer shadow-2xs"
                >
                  <span className="flex items-center gap-3 font-extrabold text-slate-900 my-auto">
                    <Key size={18} className="text-blue-600 shrink-0" /> 
                    <span>{str.switchWallet}</span>
                  </span>
                  <span className="text-[10px] font-extrabold bg-blue-50 text-blue-700 border border-blue-200/80 px-3 py-1 rounded-full shrink-0 my-auto">Switch</span>
                </button>
                <button 
                  onClick={() => { setIsSettingsOpen(false); handleLogout(); }}
                  className="w-full px-5 py-4 min-h-[56px] bg-red-50/50 hover:bg-red-50 text-red-600 font-bold text-xs rounded-2xl border border-red-100 transition-all flex items-center justify-between active:scale-[0.98] cursor-pointer shadow-2xs"
                >
                  <span className="flex items-center gap-3 font-extrabold text-red-600 my-auto">
                    <LogOut size={18} className="text-red-500 shrink-0" /> 
                    <span>{str.logout}</span>
                  </span>
                  <span className="text-[10px] font-extrabold bg-red-100/70 text-red-700 border border-red-200/60 px-3 py-1 rounded-full shrink-0 my-auto">Lock</span>
                </button>
              </div>
            </div>


          </div>

          <div className="p-6 border-t border-slate-100 bg-white shrink-0">
            <button onClick={() => setIsSettingsOpen(false)} className="w-full pill-btn pill-btn-primary shadow-md active:scale-95">
              {str.close}
            </button>
          </div>
        </div>
      )}


      {/* Immutable Notification Drawer Modal ("thông báo bất biến") */}
      {isNotificationOpen && (
        <div className="absolute inset-0 bg-white z-50 flex flex-col h-full overflow-hidden fade-in">
          <div className="flex justify-between items-center px-6 pt-7 pb-4 border-b border-slate-100 bg-white shrink-0">
            <div className="flex items-center gap-3">
              <button onClick={() => setIsNotificationOpen(false)} className="w-10 h-10 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center justify-center transition-colors active:scale-95 cursor-pointer border border-slate-200/60">
                <X size={20} />
              </button>
              <h3 className="text-sm font-bold text-slate-900 flex items-center gap-2">
                <Bell size={20} className="text-blue-600" /> {str.immutableLogsTitle}
              </h3>
            </div>
            <button 
              onClick={clearNotifications}
              className="text-[11px] font-bold text-red-600 hover:bg-red-50 px-3 py-1.5 rounded-xl transition-colors flex items-center gap-1 border border-red-100"
            >
              <Trash2 size={13} /> {str.clearLogs}
            </button>
          </div>


          {/* Filter Tabs */}
          <div className="flex px-5 py-2.5 border-b border-slate-100 gap-2 bg-slate-50 shrink-0 text-xs font-bold">
            <button 
              onClick={() => setNotificationFilter('all')}
              className={`px-3 py-1.5 rounded-xl transition-all ${notificationFilter === 'all' ? 'bg-blue-600 text-white shadow-sm' : 'text-slate-600 hover:bg-slate-200'}`}
            >
              {str.all} ({notifications.length})
            </button>
            <button 
              onClick={() => setNotificationFilter('tx')}
              className={`px-3 py-1.5 rounded-xl transition-all ${notificationFilter === 'tx' ? 'bg-blue-600 text-white shadow-sm' : 'text-slate-600 hover:bg-slate-200'}`}
            >
              {str.transactions}
            </button>
            <button 
              onClick={() => setNotificationFilter('system')}
              className={`px-3 py-1.5 rounded-xl transition-all ${notificationFilter === 'system' ? 'bg-blue-600 text-white shadow-sm' : 'text-slate-600 hover:bg-slate-200'}`}
            >
              {str.system}
            </button>
          </div>

          {/* Notification Items List */}
          <div className="flex-1 overflow-y-auto p-5 space-y-3">
            {filteredNotifications.length === 0 ? (
              <div className="text-center py-12 space-y-2">
                <div className="w-14 h-14 bg-slate-100 text-slate-400 rounded-2xl flex items-center justify-center mx-auto">
                  <Bell size={24} />
                </div>
                <p className="text-xs text-slate-400 font-medium">No immutable notifications logged yet</p>
              </div>
            ) : (
              filteredNotifications.map((item) => (
                <div 
                  key={item.id}
                  className={`p-3.5 rounded-2xl border text-xs space-y-1 transition-all ${
                    item.type === 'success' ? 'bg-emerald-50/70 border-emerald-200 text-emerald-950' :
                    item.type === 'error' ? 'bg-red-50/70 border-red-200 text-red-950' :
                    item.type === 'warning' ? 'bg-amber-50/70 border-amber-200 text-amber-950' :
                    'bg-slate-50 border-slate-200 text-slate-900'
                  }`}
                >
                  <div className="flex justify-between items-center">
                    <span className="font-bold flex items-center gap-1.5">
                      {item.type === 'success' && <CheckCircle2 size={15} className="text-emerald-600 shrink-0" />}
                      {item.type === 'error' && <AlertCircle size={15} className="text-red-600 shrink-0" />}
                      {item.type === 'info' && <Info size={15} className="text-blue-600 shrink-0" />}
                      {item.title}
                    </span>
                    <span className="text-[10px] text-slate-400 font-mono flex items-center gap-1">
                      <Clock size={11} /> {formatDate(item.timestamp)}
                    </span>
                  </div>
                  <p className="text-[11px] text-slate-600 leading-relaxed">{item.message}</p>
                  {item.txid && (
                    <div className="pt-1 font-mono text-[10px] text-slate-500 font-bold truncate">
                      TxID: {item.txid}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>

          <div className="p-4 border-t border-slate-100 bg-white shrink-0">
            <button 
              onClick={() => setIsNotificationOpen(false)}
              className="w-full pill-btn pill-btn-primary active:scale-95"
            >
              {str.closeLogs}
            </button>
          </div>
        </div>
      )}

      {/* Send Screen (Compact Layout Fix) */}
      {isSendOpen && (
        <div className="absolute inset-0 bg-slate-50 z-50 flex flex-col h-full overflow-hidden fade-in">
          {/* Header */}
          <div className="bg-white px-6 pt-7 pb-4 border-b border-slate-200 flex justify-between items-center gap-3 shrink-0 shadow-xs min-h-[56px]">
            <button 
              onClick={() => { if (isSendConfirmStep) setIsSendConfirmStep(false); else setIsSendOpen(false); }} 
              className="w-10 h-10 rounded-2xl bg-slate-100 hover:bg-slate-200 flex items-center justify-center text-slate-700 shrink-0 transition-colors active:scale-95 cursor-pointer border border-slate-200/60"
            >
              <X size={20} />
            </button>
            <div className="flex-1 text-center min-w-0">
              <h3 className="text-base font-black text-slate-900 truncate">
                {isSendConfirmStep ? str.confirmSendTitle : str.sendTitle}
              </h3>
            </div>
            <div className="w-10 h-10 shrink-0 flex items-center justify-end">
              <span className="text-[10px] font-black text-blue-600 bg-blue-50 px-2.5 py-1 rounded-xl border border-blue-100">
                {isSendConfirmStep ? '2/2' : '1/2'}
              </span>
            </div>
          </div>

          {/* Form Content */}
          <div className="flex-1 flex flex-col p-5 overflow-y-auto space-y-4 justify-start">
            {sendSuccess ? (
              <div className="text-center my-auto py-8 space-y-4 bg-white p-6 rounded-3xl border border-slate-200 shadow-sm">
                <div className="w-16 h-16 bg-emerald-100 text-emerald-600 rounded-full flex items-center justify-center mx-auto shadow-lg shadow-emerald-500/10">
                  <Check size={36} />
                </div>
                <h4 className="font-black text-emerald-600 text-lg">{str.txSuccessMsg}</h4>
                <p className="text-xs text-slate-500 font-mono break-all bg-slate-50 p-3.5 rounded-2xl border border-slate-200">{sendTxId}</p>
                <button 
                  onClick={() => { setIsSendOpen(false); setIsSendConfirmStep(false); setSendSuccess(false); }} 
                  className="w-full pill-btn pill-btn-primary shadow-xl shadow-blue-600/30 active:scale-95"
                >
                  {str.done}
                </button>
              </div>
            ) : !isSendConfirmStep ? (
              /* Step 1 Flex Column Layout with Auto-Height Blue Balance Card */
              <div className="space-y-4">
                {/* Blue Balance Card with height: auto & 16px padding */}
                <div className="bg-blue-600 p-4.5 rounded-2xl text-white shadow-md flex flex-col justify-between space-y-3 mb-4 shrink-0" style={{ height: 'auto' }}>
                  <div className="flex justify-between items-center w-full">
                    <span className="text-[11px] font-extrabold text-white uppercase tracking-wider">{str.availableBalance}</span>
                    <button 
                      type="button"
                      onClick={() => setSendAmount(balance.toString())}
                      className="text-[11px] font-black text-white bg-white/20 hover:bg-white/30 border border-white/30 px-3.5 py-1.5 rounded-full shadow-xs active:scale-95 transition-all cursor-pointer shrink-0"
                    >
                      {str.sendAll}
                    </button>
                  </div>
                  <div className="text-2xl font-black text-white tracking-tight flex items-baseline gap-1.5 w-full">
                    <span>{balance.toFixed(4)}</span>
                    <span className="text-sm font-bold text-white/90">GO</span>
                  </div>
                </div>

                {/* Receiver Address Container separated with 16px margin */}
                <div className="bg-white p-4.5 rounded-3xl border border-slate-200 space-y-1.5 shadow-2xs">
                  <label className="text-[11px] font-extrabold text-slate-700 uppercase tracking-wider block">{str.receiverAddress}</label>

                  <input 
                    type="text"
                    value={sendReceiver}
                    onChange={(e) => setSendReceiver(e.target.value)}
                    placeholder={str.receiverPlaceholder}
                    className="form-input font-mono text-xs"
                  />
                </div>

                <div className="bg-white p-4.5 rounded-3xl border border-slate-200 space-y-2.5 shadow-2xs">
                  <div className="flex justify-between items-center">
                    <label className="text-[11px] font-extrabold text-slate-700 uppercase tracking-wider block">{str.sendAmount}</label>
                    <span className="text-[10px] font-bold text-slate-400">{str.baseFee}</span>
                  </div>
                  <input 
                    type="number"
                    step="0.0001"
                    value={sendAmount}
                    onChange={(e) => setSendAmount(e.target.value)}
                    placeholder="0.0000"
                    className="form-input font-mono text-xl font-black"
                  />
                  <div className="grid grid-cols-4 gap-2 pt-0.5">
                    {[1, 10, 50, 100].map(amt => (
                      <button
                        key={amt}
                        type="button"
                        onClick={() => setSendAmount(amt.toString())}
                        className="py-1.5 bg-slate-100 hover:bg-blue-50 hover:text-blue-600 text-slate-800 font-extrabold rounded-xl text-xs border border-slate-200 transition-colors active:scale-95"
                      >
                        +{amt} GO
                      </button>
                    ))}
                  </div>
                </div>

                <div className="bg-white p-4 rounded-3xl border border-slate-200 space-y-2 text-xs">
                  <div className="flex justify-between items-center">
                    <span className="text-slate-500 font-bold">{str.network}</span>
                    <span className="font-extrabold text-slate-800">Yona Mainnet PoW</span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span className="text-slate-500 font-bold">{str.speed}</span>
                    <span className="font-extrabold text-emerald-600">{str.speedFast}</span>
                  </div>
                </div>

                {sendError && <p className="text-red-600 text-xs bg-red-50 p-3 rounded-2xl border border-red-200 font-bold text-center">{sendError}</p>}
              </div>
            ) : (
              /* Step 2 Balanced Centered Layout with Grouped Card & Shortened Address */
              <div className="flex-1 flex flex-col justify-between py-1 space-y-4">
                <div className="bg-white p-4.5 rounded-3xl border border-slate-200 space-y-3 text-xs shadow-2xs">
                  <div className="flex justify-between items-center">
                    <span className="text-slate-500 font-extrabold">{str.amount}</span>
                    <span className="font-black text-blue-600 text-base">{sendAmount} GO</span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span className="text-slate-500 font-extrabold">{str.fee}</span>
                    <span className="text-slate-900 font-extrabold">
                      {preparedTx ? (preparedTx.fee / 1e8).toFixed(8) : '0.00000250'} GO
                      {preparedTx && preparedTx.creation_fee > 0 ? ' (+ creation fee)' : ''}
                    </span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span className="text-slate-500 font-extrabold">{str.projectedNonce}</span>
                    <span className="text-slate-900 font-extrabold font-mono">
                      #{preparedTx ? preparedTx.nonce : 0}
                    </span>
                  </div>
                  <div className="border-t border-slate-100 pt-2.5 flex justify-between items-center">
                    <span className="text-slate-500 font-extrabold">{str.receiverAcc}</span>
                    <div className="flex items-center gap-1.5 font-mono text-xs font-bold text-slate-900 bg-slate-50 px-2.5 py-1 rounded-xl border border-slate-200">
                      <span>{sendReceiver.length > 14 ? `${sendReceiver.slice(0, 6)}...${sendReceiver.slice(-6)}` : sendReceiver}</span>
                      <button onClick={() => copyToClipboard(sendReceiver)} className="p-0.5 text-slate-500 hover:text-slate-800">
                        <Copy size={12} />
                      </button>
                    </div>
                  </div>
                </div>

                <div className="bg-white p-5 rounded-3xl border border-slate-200 text-center space-y-4 shadow-2xs my-auto">
                  <label className="text-xs text-slate-800 font-black tracking-tight block">{str.pinEnterConfirm}</label>
                  
                  {/* PIN Dots */}
                  <div className="pin-dots my-3">
                    {Array.from({ length: 6 }).map((_, i) => (
                      <div key={i} className={`pin-dot ${i < sendPin.length ? 'filled' : ''}`}></div>
                    ))}
                  </div>

                  {/* PIN Pad */}
                  <div className="pin-pad pt-1">
                    {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map(num => (
                      <button key={num} onClick={() => handleSendPinDigitPress(num)} className="pin-btn active:scale-95 transition-all">
                        {num}
                      </button>
                    ))}
                    <button onClick={handleSendPinDelete} className="pin-btn text-xs font-black text-slate-500 hover:bg-slate-200">DEL</button>
                    <button onClick={() => handleSendPinDigitPress('0')} className="pin-btn active:scale-95 transition-all">0</button>
                    <button 
                      onClick={handleSendPinSubmit}
                      className="pin-btn !bg-blue-600 !text-white !border-blue-600 text-xs font-black shadow-md shadow-blue-600/30 active:scale-95"
                    >
                      OK
                    </button>
                  </div>
                </div>

                {sendError && <p className="text-red-600 text-xs bg-red-50 p-3 rounded-2xl border border-red-200 font-bold text-center">{sendError}</p>}
              </div>
            )}
          </div>


          {/* Action Footer Buttons */}
          {!sendSuccess && (
            <div className="p-4 bg-white border-t border-slate-200 shrink-0">
              {!isSendConfirmStep ? (
                <div className="flex gap-2">
                  <button onClick={() => setIsSendOpen(false)} className="w-1/3 pill-btn pill-btn-secondary">
                    {str.cancel}
                  </button>
                  <button 
                    onClick={handlePrepareAndContinue} 
                    disabled={isPreparingTx}
                    className="w-2/3 pill-btn pill-btn-primary active:scale-95"
                  >
                    {isPreparingTx ? str.preparing : `${str.next} →`}
                  </button>
                </div>
              ) : (
                <div className="flex gap-2">
                  <button onClick={() => setIsSendConfirmStep(false)} className="w-1/3 pill-btn pill-btn-secondary">
                    {str.back}
                  </button>
                  <button 
                    onClick={handleSendPinSubmit} 
                    disabled={isSending}
                    className="w-2/3 pill-btn pill-btn-primary active:scale-95"
                  >
                    {isSending ? str.signing : str.confirmSendBtn}
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Receive Screen */}
      {isReceiveOpen && (
        <div className="absolute inset-0 bg-white z-50 flex flex-col h-full overflow-hidden fade-in">
          <div className="flex justify-between items-center px-6 pt-7 pb-4 border-b border-slate-100 shrink-0 bg-white min-h-[56px]">
            <div className="flex items-center gap-3">
              <button onClick={() => setIsReceiveOpen(false)} className="w-10 h-10 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center justify-center transition-colors active:scale-95 cursor-pointer border border-slate-200/60">
                <X size={20} />
              </button>
              <h3 className="text-sm font-bold text-slate-900 flex items-center gap-2">
                <ArrowDownLeft size={20} className="text-emerald-600" /> {str.receive} GO
              </h3>
            </div>
            <span className="text-[10px] font-bold text-emerald-600 bg-emerald-50 px-2.5 py-1 rounded-lg border border-emerald-100">{str.receive}</span>
          </div>


          <div className="flex-1 overflow-y-auto p-6 space-y-4">
            <div className="text-center py-4 space-y-4">
              <div className="w-20 h-20 bg-emerald-50 text-emerald-600 rounded-3xl flex items-center justify-center mx-auto shadow-inner border border-emerald-100">
                <ArrowDownLeft size={44} />
              </div>
              <div>
                <span className="text-xs text-slate-500 font-bold block mb-1">{str.receiverAddress}</span>
                <div className="p-4 bg-slate-50 rounded-2xl border border-slate-200 font-mono text-xs text-slate-900 font-bold break-all shadow-inner">
                  {address}
                </div>
              </div>
            </div>
          </div>

          <div className="px-6 pt-3 pb-6 border-t border-slate-100 bg-white shrink-0 flex flex-col gap-2">
            <button 
              onClick={() => copyToClipboard(address)} 
              className="w-full pill-btn pill-btn-primary shadow-md active:scale-95"
            >
              {copied ? <Check size={16} /> : <Copy size={16} />} {copied ? str.copiedAddress : str.copyAddress}
            </button>
            <button onClick={() => setIsReceiveOpen(false)} className="w-full pill-btn pill-btn-secondary">
              {str.close}
            </button>
          </div>
        </div>
      )}

      {/* Full Transaction History Modal */}

      {isHistoryModalOpen && (
        <div className="absolute inset-0 bg-white z-50 flex flex-col h-full overflow-hidden fade-in">
          <div className="flex justify-between items-center px-6 pt-7 pb-4 border-b border-slate-100 shrink-0 bg-white min-h-[56px]">
            <div className="flex items-center gap-3">
              <button onClick={() => setIsHistoryModalOpen(false)} className="w-10 h-10 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center justify-center transition-colors active:scale-95 cursor-pointer border border-slate-200/60">
                <X size={20} />
              </button>
              <h3 className="text-base font-black text-slate-900 truncate">
                {str.txHistory}
              </h3>
            </div>
            <button onClick={fetchHistory} className="p-2 hover:bg-slate-100 rounded-xl text-slate-500 transition-colors cursor-pointer" title="Refresh">
              <RefreshCw size={18} />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-5 space-y-3.5">
            {txHistory.length === 0 ? (
              <p className="text-xs text-slate-400 text-center py-10">{str.noHistory}</p>
            ) : (
              <div className="space-y-3.5">
                {txHistory.map((tx, idx) => {
                  const isOut = tx.sender.toLowerCase() === address.toLowerCase();
                  const confirms = Math.min(5, Math.max(0, tx.confirmations || (tx.status === 0 ? 1 : 0)));
                  const isImmutable = tx.status === 0 && confirms >= 5;
                  const isRejected = tx.status === 1;

                  return (
                    <div 
                      key={idx} 
                      onClick={() => { setSelectedTx(tx); setIsHistoryModalOpen(false); }}
                      className="flex justify-between items-center px-4.5 py-4 bg-slate-50 hover:bg-slate-100/90 rounded-2xl border border-slate-200 text-xs cursor-pointer transition-all active:scale-[0.99] shadow-2xs gap-3"
                    >
                      <div className="flex items-center gap-3 min-w-0 flex-1">
                        <div className={`p-2.5 rounded-2xl shrink-0 ${isOut ? 'bg-red-50 text-red-600 border border-red-100' : 'bg-emerald-50 text-emerald-600 border border-emerald-100'}`}>
                          {isOut ? <ArrowUpRight size={16} /> : <ArrowDownLeft size={16} />}
                        </div>
                        <div className="space-y-0.5 min-w-0 flex-1">
                          <div className="font-mono text-slate-900 font-extrabold text-xs truncate">{tx.txid.slice(0, 8)}...{tx.txid.slice(-4)}</div>
                          <div className="text-[10px] text-slate-400 font-mono font-bold truncate">Nonce: #{tx.nonce} • {formatDate(tx.timestamp)}</div>
                        </div>
                      </div>

                      <div className="text-right space-y-1 shrink-0 flex flex-col items-end">
                        <div className={`font-black text-sm ${isOut ? 'text-red-600' : 'text-emerald-600'}`}>
                          {isOut ? '-' : '+'}{(tx.amount / 1e8).toFixed(4)} GO
                        </div>

                        {isRejected ? (
                          <span className="text-[9px] px-2.5 py-0.5 rounded-full font-extrabold bg-red-100 text-red-800 border border-red-200">
                            ❌ {str.rejected}
                          </span>
                        ) : isImmutable ? (
                          <span className="text-[9px] px-2.5 py-0.5 rounded-full font-extrabold bg-emerald-100 text-emerald-800 border border-emerald-200">
                            🛡️ {str.success} (5/5)
                          </span>
                        ) : (
                          <div className="flex items-center gap-1.5 bg-amber-50 text-amber-800 border border-amber-200/80 px-2 py-0.5 rounded-full">
                            <span className="text-[9px] font-extrabold">⏳ {confirms}/5 {str.blocksWord}</span>
                            <div className="w-8 h-1 bg-amber-200 rounded-full overflow-hidden shrink-0">
                              <div 
                                className="h-full bg-amber-500 rounded-full transition-all" 
                                style={{ width: `${(confirms / 5) * 100}%` }}
                              ></div>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}

                <div className="text-center pt-4 border-t border-slate-100">
                  <span className="text-[11px] text-slate-400 font-medium italic">{str.allTxDisplayed}</span>
                </div>
              </div>
            )}
          </div>

          <div className="p-5 border-t border-slate-100 shrink-0 bg-white">
            <button onClick={() => setIsHistoryModalOpen(false)} className="w-full pill-btn pill-btn-secondary">
              {str.close}
            </button>
          </div>
        </div>
      )}


      {selectedTx && (

        <div className="absolute inset-0 bg-white z-50 flex flex-col justify-between p-6 pt-7 overflow-y-auto fade-in">
          <div className="space-y-4">
            {/* Header without redundant top-right badge */}
            <div className="flex items-center gap-3 pb-3.5 border-b border-slate-100">
              <button 
                onClick={() => setSelectedTx(null)} 
                className="w-9 h-9 rounded-2xl bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center justify-center shrink-0 transition-colors active:scale-95 cursor-pointer"
              >
                <X size={18} />
              </button>
              <h3 className="text-base font-black text-slate-900 truncate">{str.txDetail}</h3>
            </div>

            <div className="space-y-4 pt-1">
              {/* Card 1: Grouped Transaction Details Card */}
              <div className="bg-slate-50 p-4.5 rounded-2xl border border-slate-200/80 space-y-3 text-xs shadow-2xs">
                <div className="flex justify-between items-center">
                  <span className="text-slate-500 font-extrabold">{str.amount}</span>
                  <span className="font-black text-slate-900 text-base">{(selectedTx.amount / 1e8).toFixed(8)} GO</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-slate-500 font-extrabold">{str.fee}</span>
                  <span className="text-slate-700 font-bold">{(selectedTx.fee / 1e8).toFixed(8)} GO</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-slate-500 font-extrabold">{str.block}</span>
                  <span className="font-mono text-slate-800 font-extrabold">
                    {selectedTx.blockHeight && selectedTx.blockHeight > 0 ? `#${selectedTx.blockHeight}` : str.unconfirmed}
                  </span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-slate-500 font-extrabold">{str.nonce}</span>
                  <span className="font-mono text-slate-800 font-extrabold">#{selectedTx.nonce}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-slate-500 font-extrabold">{str.time}</span>
                  <span className="font-mono text-slate-700 font-medium">{formatDate(selectedTx.timestamp)}</span>
                </div>

                <div className="border-t border-slate-200/60 pt-2.5 space-y-2">
                  <div>
                    <span className="text-[11px] text-slate-400 font-extrabold uppercase block mb-1">{str.senderAcc}</span>
                    <div className="p-2.5 bg-white rounded-xl font-mono text-[11px] text-slate-900 flex justify-between items-center border border-slate-200/60">
                      <span className="truncate pr-2 font-bold">{selectedTx.sender || address}</span>
                      <button onClick={() => copyToClipboard(selectedTx.sender || address)} className="p-1 hover:bg-slate-100 rounded-lg text-slate-500 shrink-0">
                        <Copy size={13} />
                      </button>
                    </div>
                  </div>

                  <div>
                    <span className="text-[11px] text-slate-400 font-extrabold uppercase block mb-1">{str.receiverAcc}</span>
                    <div className="p-2.5 bg-white rounded-xl font-mono text-[11px] text-slate-900 flex justify-between items-center border border-slate-200/60">
                      <span className="truncate pr-2 font-bold">{selectedTx.receiver || 'N/A'}</span>
                      <button onClick={() => copyToClipboard(selectedTx.receiver || '')} className="p-1 hover:bg-slate-100 rounded-lg text-slate-500 shrink-0">
                        <Copy size={13} />
                      </button>
                    </div>
                  </div>

                  <div>
                    <span className="text-[11px] text-slate-400 font-extrabold uppercase block mb-1">{str.txHash}</span>
                    <div className="p-2.5 bg-white rounded-xl font-mono text-[11px] text-slate-900 flex justify-between items-center border border-slate-200/60">
                      <span className="truncate pr-2 font-bold">{selectedTx.txid}</span>
                      <button onClick={() => copyToClipboard(selectedTx.txid)} className="p-1 hover:bg-slate-100 rounded-lg text-slate-500 shrink-0">
                        <Copy size={13} />
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {/* Card 2: Web3 Sleek Immutability Stepper Progress Card */}
              {(() => {
                const confirms = Math.min(5, Math.max(0, selectedTx.confirmations || (selectedTx.status === 0 ? 1 : 0)));
                const isImmutable = confirms >= 5 && selectedTx.status === 0;

                return (
                  <div className="bg-white p-4.5 rounded-2xl border border-slate-200/80 space-y-4 shadow-2xs">
                    {/* Header line with pastel Pill badge */}
                    <div className="flex justify-between items-center">
                      <div className="flex items-center gap-2">
                        <Shield size={16} className={isImmutable ? "text-emerald-600" : "text-amber-600"} />
                        <span className="text-xs font-black uppercase tracking-wider text-slate-900">
                          {isImmutable ? str.immutableStatus : selectedTx.status === 99 ? str.mempoolPendingStatus : str.confirmingStatus}
                        </span>
                      </div>
                      <span className={`text-[10px] font-extrabold uppercase tracking-wide px-3 py-1 rounded-full border ${
                        isImmutable ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 
                        selectedTx.status === 99 ? 'bg-amber-50 text-amber-700 border-amber-200' :
                        'bg-blue-50 text-blue-700 border-blue-200'
                      }`}>
                        {confirms}/5 {str.blocksWord}
                      </span>
                    </div>

                    {/* Slim Web3 Progress Bar & Minimal Dots */}
                    <div className="px-1 py-1 space-y-3">
                      {/* Slim Bar (4px height) */}
                      <div className="w-full bg-slate-100 h-1.5 rounded-full overflow-hidden relative">
                        <div 
                          className="h-full bg-gradient-to-r from-emerald-500 via-teal-500 to-emerald-400 transition-all duration-500 rounded-full"
                          style={{ width: `${Math.min(100, (confirms / 5) * 100)}%` }}
                        ></div>
                      </div>

                      {/* 5 Minimal Block Indicators */}
                      <div className="grid grid-cols-5 gap-1 text-center">
                        {[1, 2, 3, 4, 5].map((step) => {
                          const isDone = confirms >= step;
                          const isFinalStep = step === 5;
                          return (
                            <div key={step} className="flex flex-col items-center gap-1">
                              <div className={`w-2.5 h-2.5 rounded-full transition-all ${
                                isDone 
                                  ? isFinalStep 
                                    ? 'bg-emerald-500 ring-2 ring-emerald-500/20 shadow-xs' 
                                    : 'bg-blue-500 ring-2 ring-blue-500/20 shadow-xs'
                                  : 'bg-slate-200'
                              }`}></div>
                              <span className={`text-[10px] font-mono font-medium ${isDone ? (isFinalStep ? 'text-emerald-700 font-extrabold' : 'text-blue-700 font-bold') : 'text-slate-400'}`}>
                                {str.blockWord} #{step}
                              </span>
                            </div>
                          );
                        })}

                      </div>
                    </div>

                    {/* Note Container with generous padding (16px) */}
                    <div className="p-4 bg-slate-50 rounded-xl border border-slate-100 text-[11px] leading-relaxed text-slate-600 font-medium">
                      {isImmutable ? (
                        <p className="text-emerald-800 font-semibold">{str.immutableInfoNote}</p>
                      ) : selectedTx.status === 99 ? (
                        <p className="text-amber-800 font-semibold">{str.mempoolInfoNote}</p>
                      ) : (
                        <p className="text-blue-900 font-semibold">{str.confirmingInfoNote.replace('{confirms}', confirms.toString()).replace('{needed}', (5 - confirms).toString()).replace('{time}', Math.ceil((5 - confirms) * 75 / 60).toString())}</p>
                      )}
                    </div>


                    {!isImmutable && selectedTx.status === 0 && (
                      <div className="text-[10px] text-amber-700 bg-amber-50/80 p-2.5 rounded-xl border border-amber-200/80 font-medium">
                        ⚠️ Trong khoảng 1-4 khối đè lên, giao dịch thuộc Vùng Linh hoạt và có rủi ro bị đảo ngược cực thấp.
                      </div>
                    )}
                  </div>
                );
              })()}

            </div>

          </div>

          <div className="pt-4 border-t border-slate-100">
            <button onClick={() => setSelectedTx(null)} className="w-full pill-btn pill-btn-primary shadow-md">
              {str.closeDetail}
            </button>
          </div>
        </div>
      )}

    </div>
  );
}
