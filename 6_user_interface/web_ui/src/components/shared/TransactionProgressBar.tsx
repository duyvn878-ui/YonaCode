/**
 * @file TransactionProgressBar.tsx
 * @brief Tiến độ xác nhận dạng Chấm V2.1 (6-Step Correct Model)
 * @tính_năng:
 *   - Hiển thị 6 điểm tròn: 1 "Đã vào khối" (xanh dương) + 5 "Khối đè lên" (cam→xanh lá→cyan)
 *   - Label "X/5 Khối đè lên" hiển thị trực quan phía trên
 *   - Lý do: Backend tính confirmations = highest - blockHeight + 1 (tính CẢ khối chứa TX)
 *     Nên cần 6 confirmations = 1 (khối chứa) + 5 (khối đè lên) để đạt Bất biến
 */

import React from 'react';

interface TransactionProgressBarProps {
  confirmations: number;
}

const OVERLAY_BLOCKS_NEEDED = 5; // Số khối ĐÈ LÊN cần thiết
const TOTAL_STEPS = 6; // 1 khối chứa TX + 5 khối đè lên

const TransactionProgressBar: React.FC<TransactionProgressBarProps> = ({ confirmations }) => {
  // Số khối ĐÈ LÊN đã có = confirmations - 1 (trừ đi khối chứa TX)
  const overlayBlocksDone = Math.max(0, confirmations - 1);
  const isFinalized = confirmations >= TOTAL_STEPS;

  // Logic màu sắc theo mô hình 6 bước
  const getDotColor = (index: number) => {
    // index 0: Khối chứa TX ("Đã nhận")
    // index 1-5: 5 khối đè lên bất biến
    if (index < confirmations) {
      if (index === 0) return 'bg-[#3b82f6] shadow-[0_0_8px_rgba(59,130,246,0.5)] border-[#3b82f6]'; // Xanh dương: Đã vào khối
      if (index >= 1 && index <= 3) return 'bg-[#f97316] shadow-[0_0_8px_rgba(249,115,22,0.5)] border-[#f97316]'; // Cam: Đè lên 1-3
      if (index === 4) return 'bg-[#22c55e] shadow-[0_0_8px_rgba(34,197,94,0.5)] border-[#22c55e]'; // Xanh lá: Đè lên 4
      if (index === 5) return 'bg-[#06b6d4] shadow-[0_0_10px_rgba(6,182,212,0.6)] border-[#06b6d4]'; // Cyan: Chốt bất biến
    }
    return 'bg-white/5 border-white/10 opacity-20'; // Grey/Empty
  };

  return (
    <div className="flex flex-col gap-2 items-center min-w-[140px]">
      {/* Label Khối đè lên */}
      <div className="flex items-center gap-1">
        <span className={`text-[10px] font-black italic tracking-tighter ${isFinalized ? 'text-[#06b6d4]' : (confirmations > 0 ? 'text-accent-blue' : 'text-white/20')}`}>
          {isFinalized ? `${OVERLAY_BLOCKS_NEEDED}/${OVERLAY_BLOCKS_NEEDED} (Đã chốt)` : `${overlayBlocksDone}/${OVERLAY_BLOCKS_NEEDED} Khối đè lên`}
        </span>
      </div>

      {/* Dãy 6 chấm tròn: 1 "Đã nhận" + 5 "Đè lên" */}
      <div className="flex gap-1.5">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <div 
            key={i}
            className={`w-3.5 h-3.5 rounded-full border-2 transition-all duration-500 scale-100 ${getDotColor(i)}`}
            title={i === 0 ? 'Đã vào khối' : `Khối đè lên #${i}`}
          />
        ))}
      </div>
      
      {/* Progress Line (Optional subtle connector) */}
      <div className="w-full h-[2px] bg-white/5 rounded-full mt-[-10px] z-[-1] max-w-[85%] mx-auto" />
    </div>
  );
};

export default TransactionProgressBar;
