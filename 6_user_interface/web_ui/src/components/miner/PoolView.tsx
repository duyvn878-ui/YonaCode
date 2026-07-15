import React from 'react';
import { AlertTriangle } from 'lucide-react';
import { useLanguage } from '../../LanguageContext';

interface PoolViewProps {
  status: any;
  onNotify: (msg: string, type: 'info' | 'success' | 'error' | 'finality') => void;
}

const PoolView: React.FC<PoolViewProps> = () => {
  const { lang } = useLanguage();

  const disabledTitle = lang === 'vi' 
    ? 'TÍNH NĂNG KHAI THÁC BỂ ĐÀO ĐÃ BỊ VÔ HIỆU HÓA' 
    : 'POOL MINING HAS BEEN DISABLED';
  const disabledDesc = lang === 'vi'
    ? 'Tính năng khai thác qua Pool hiện đang bị vô hiệu hóa trên hệ thống và không khả dụng ở thời điểm hiện tại.'
    : 'Pool mining features are currently disabled on the system and not available at this moment.';

  return (
    <div className="flex flex-col items-center justify-center min-h-[400px] vanguard-glass p-8 border border-white/10 rounded-2xl relative overflow-hidden shadow-2xl animate-in fade-in duration-500 w-full">
      <div className="absolute inset-0 bg-gradient-to-br from-accent-red/5 via-transparent to-transparent pointer-events-none" />
      <div className="w-16 h-16 rounded-full bg-accent-red/10 border border-accent-red/20 flex items-center justify-center text-accent-red mb-6 shadow-[0_0_30px_rgba(239,68,68,0.2)] animate-pulse">
        <AlertTriangle size={32} />
      </div>
      <h2 className="text-xl font-black text-white uppercase tracking-wider mb-2 text-center drop-shadow-[0_0_15px_rgba(255,255,255,0.1)]">
        {disabledTitle}
      </h2>
      <p className="text-sm text-white/60 text-center max-w-md leading-relaxed">
        {disabledDesc}
      </p>
    </div>
  );
};

export default PoolView;
