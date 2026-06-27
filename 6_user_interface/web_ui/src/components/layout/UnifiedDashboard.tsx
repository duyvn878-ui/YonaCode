/**
 * @file UnifiedDashboard.tsx
 * @brief Dashboard Chính V6.3 — Ánh sáng Tối thượng (Fixed VILS)
 * @tính_năng:
 *   - Phục hồi cấu trúc VILS (Vanguard Invariant Layout System).
 *   - Sử dụng các Sector Wrapper chuyên dụng (.dashboard-sector).
 */

import type { NodeStatus, MinerStatus, BlockHeader, Transaction } from '../../api';
import UnifiedWalletPanel from '../wallet/UnifiedWalletPanel';
import UnifiedExplorerPanel from '../explorer/UnifiedExplorerPanel';
import NodeControlPanel from '../dashboard/NodeControlPanel';

interface UnifiedDashboardProps {
  status: NodeStatus | null;
  balance: number;
  address: string;
  miner: MinerStatus | null;
  blocks: BlockHeader[];
  transactions: Transaction[];
  supply: number;
  handleToggleMiner: () => void;
  handleSend: () => void;
  onRestoreClick: () => void;
  onCreateClick: () => void;
  onTransactionClick: (tx: Transaction) => void;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
  onReceiveClick: () => void;
  isStopping?: boolean;
  pendingTxCount?: number;
  txRefreshCounter?: number;
}

const UnifiedDashboard: React.FC<UnifiedDashboardProps> = ({ 
  status, balance, address, miner, blocks, transactions, supply,
  handleToggleMiner, handleSend, onRestoreClick, onCreateClick, onTransactionClick, onNotify, onReceiveClick,
  isStopping = false, pendingTxCount = 0, txRefreshCounter = 0
}) => {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 w-full items-start relative z-10 pb-10">
      
      {/* 🛡️ SECTOR LEFT: WALLET COMMAND & CONTROL */}
      <div className="dashboard-sector">
        <UnifiedWalletPanel
          balance={balance}
          address={address}
          handleSend={handleSend}
          onRestoreClick={onRestoreClick}
          onCreateClick={onCreateClick}
          onTransactionClick={onTransactionClick}
          onReceiveClick={onReceiveClick}
          pendingTxCount={pendingTxCount}
          txRefreshCounter={txRefreshCounter}
        />
        
        <NodeControlPanel
          status={status}
          miner={miner}
          onToggleMiner={handleToggleMiner}
          onNotify={onNotify}
          isStopping={isStopping}
        />
      </div>

      {/* 🛰️ SECTOR RIGHT: NETWORK EXPLORER MATRIX */}
      <div className="dashboard-sector">
        <UnifiedExplorerPanel 
          status={status}
          miner={miner}
          blocks={blocks}
          transactions={transactions}
          supply={supply}
          onTransactionClick={onTransactionClick}
        />
      </div>

      {/* ⚡ TACTICAL VERTICAL DIVIDER (Absolute) */}
      <div className="hidden lg:block absolute top-0 bottom-20 left-1/2 -translate-x-1/2 w-[1px] bg-gradient-to-b from-transparent via-white/10 to-transparent pointer-events-none z-0" />
    </div>
  );
};

export default UnifiedDashboard;
