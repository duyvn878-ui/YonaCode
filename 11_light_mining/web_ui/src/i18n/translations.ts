export type Language = 'vn' | 'zh' | 'en';

export interface Translations {
  statusConnecting: string;
  statusActive: string;
  statusPaused: string;
  currentHashrate: string;
  liveHashes: string;
  walletSetup: string;
  yourWallet: string;
  btnCopyWallet: string;
  btnGenBip39: string;
  networkStatus: string;
  networkHeight: string;
  networkHashrate: string;
  rewardsBlocks: string;
  minedBlocks: string;
  totalRewards: string;
  pendingRewards: string;
  consoleOutput: string;
  btnStart: string;
  btnStop: string;
  uniqueWords: string;
  btnCopySeed: string;
  btnDownloadPDF: string;
  btnClose: string;
  minedBlocksHistory: string;
  blockHeight: string;
  blockReward: string;
  blockTime: string;
  noMinedBlocks: string;
}

export const translations: Record<Language, Translations> = {
  vn: {
    statusConnecting: "TRẠNG THÁI: Đang kết nối VPS...",
    statusActive: "TRẠNG THÁI: Đang đào CUDA GPU",
    statusPaused: "TRẠNG THÁI: Tạm dừng / Sẵn sàng",
    currentHashrate: "Tốc độ băm hiện tại",
    liveHashes: "Hashes/giây trực tiếp:",
    walletSetup: "Ví & Thiết lập",
    yourWallet: "Địa chỉ Ví nhận thưởng:",
    btnCopyWallet: "Sao chép Địa chỉ Ví 1-Click",
    btnGenBip39: "Tạo 12 từ BIP39 Mnemonic",
    networkStatus: "Trạng thái Mạng lưới",
    networkHeight: "Độ cao Mạng lưới:",
    networkHashrate: "Tốc độ băm Toàn mạng:",
    rewardsBlocks: "Phần thưởng & Khối",
    minedBlocks: "Số Khối đã đào:",
    totalRewards: "Tổng Phần thưởng $YGO:",
    pendingRewards: "Đang xử lý:",
    consoleOutput: "Nhật ký Console Live",
    btnStart: "🟢 KÍCH HOẠT ĐỘNG CƠ ĐÀO GPU CUDA 100%",
    btnStop: "🔴 DỪNG ĐỘNG CƠ ĐÀO GPU CUDA",
    uniqueWords: "Chuỗi 12 từ bảo mật khôi phục:",
    btnCopySeed: "Sao chép Seed",
    btnDownloadPDF: "Tải PDF Bảo mật",
    btnClose: "Đóng",
    minedBlocksHistory: "Lịch sử Khối đã Đào",
    blockHeight: "Độ cao Khối",
    blockReward: "Phần thưởng",
    blockTime: "Thời gian",
    noMinedBlocks: "Chưa có khối nào được khai thác trong phiên này"
  },
  zh: {
    statusConnecting: "状态: 正在连接VPS...",
    statusActive: "状态: GPU CUDA 挖矿中",
    statusPaused: "状态: 暂停 / 就绪",
    currentHashrate: "当前算力速度",
    liveHashes: "实时哈希率/秒:",
    walletSetup: "钱包与设置",
    yourWallet: "您的收款钱包地址:",
    btnCopyWallet: "一键复制钱包地址",
    btnGenBip39: "生成 BIP39 助记词",
    networkStatus: "全网状态",
    networkHeight: "全网区块高度:",
    networkHashrate: "全网总算力:",
    rewardsBlocks: "奖励与出块",
    minedBlocks: "已爆块数:",
    totalRewards: "总 $YGO 挖矿奖励:",
    pendingRewards: "待结算:",
    consoleOutput: "控制台输出日志",
    btnStart: "🟢 启动 100% GPU CUDA 算力引擎",
    btnStop: "🔴 停止 GPU CUDA 算力引擎",
    uniqueWords: "12位安全恢复助记词:",
    btnCopySeed: "复制助记词",
    btnDownloadPDF: "下载加密PDF",
    btnClose: "关闭",
    minedBlocksHistory: "已出块历史记录",
    blockHeight: "区块高度",
    blockReward: "爆块奖励",
    blockTime: "时间",
    noMinedBlocks: "本会话尚未爆块"
  },
  en: {
    statusConnecting: "STATUS: Connecting VPS...",
    statusActive: "STATUS: Active & Hashing",
    statusPaused: "STATUS: Standby / Paused",
    currentHashrate: "Current Hashrate",
    liveHashes: "Live Hashes/sec:",
    walletSetup: "Wallet & Setup",
    yourWallet: "Your Wallet Address:",
    btnCopyWallet: "1-Click Copy Wallet Address",
    btnGenBip39: "Generate BIP39 Mnemonic",
    networkStatus: "Network Status",
    networkHeight: "Network Height:",
    networkHashrate: "Network Hashrate:",
    rewardsBlocks: "Rewards & Blocks",
    minedBlocks: "Mined Blocks:",
    totalRewards: "Total $YGO Rewards:",
    pendingRewards: "Pending:",
    consoleOutput: "Console Output",
    btnStart: "🟢 START 100% GPU CUDA MINING ENGINE",
    btnStop: "🔴 STOP GPU CUDA MINING ENGINE",
    uniqueWords: "Unique recovery words:",
    btnCopySeed: "Copy Seed",
    btnDownloadPDF: "Download PDF",
    btnClose: "Close",
    minedBlocksHistory: "Mined Blocks History",
    blockHeight: "Block Height",
    blockReward: "Reward",
    blockTime: "Time",
    noMinedBlocks: "No blocks mined in this session"
  }
};
