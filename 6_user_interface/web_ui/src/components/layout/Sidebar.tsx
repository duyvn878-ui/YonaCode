import React from 'react';
import { Home, Wallet, Search, Pickaxe, Shield, Network } from 'lucide-react';
import { motion } from 'framer-motion';
import { useLanguage } from '../../LanguageContext';

interface SidebarProps {
  activeTab: string;
  setActiveTab: (tab: string) => void;
}

const Sidebar: React.FC<SidebarProps> = ({ activeTab, setActiveTab }) => {
  const { t } = useLanguage();

  const menuItems = [
    { id: 'dashboard', label: t.dashboard, icon: Home },
    { id: 'wallet', label: t.vault_wallet, icon: Wallet },
    { id: 'explorer', label: t.explorer, icon: Search },
    { id: 'miner', label: t.sidebar_mining, icon: Pickaxe },
    { id: 'pool', label: t.sidebar_pool, icon: Network },
    { id: 'monitor', label: t.sidebar_monitor, icon: Shield },
  ];

  return (
    <aside className="sidebar">
      <div className="px-6 mb-12">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 bg-accent-blue rounded-xl flex items-center justify-center shadow-[0_0_15px_rgba(0,136,255,0.3)]">
            <Shield size={18} color="white" />
          </div>
          <span className="text-lg font-black tracking-tighter uppercase italic text-white">YonaCode.</span>
        </div>
        <p className="tactical-label mt-2 opacity-40">Tactical Matrix V2.0</p>
        <p className="text-[8px] opacity-30 mt-1 uppercase text-text-secondary tracking-wider">
          Tên học thuật: <span className="text-white font-bold font-mono">YonaCode</span>
        </p>
      </div>

      <nav className="flex-1 flex flex-col pt-4">
        <p className="px-6 tactical-label mb-6 text-[9px] opacity-30">
          {t.tactical_division}
        </p>
        <div className="flex flex-col gap-1">
          {menuItems.map(item => (
            <motion.div 
              key={item.id}
              whileHover={{ x: 4 }}
              whileTap={{ scale: 0.98 }}
              onClick={() => setActiveTab(item.id)}
              className={`menu-item ${activeTab === item.id ? 'active' : ''}`}
            >
              <item.icon size={18} className={activeTab === item.id ? 'text-accent-blue' : ''} />
              <span>{item.label}</span>
            </motion.div>
          ))}
        </div>
      </nav>

      <div className="p-6 mt-auto">
        <div className="p-4 bg-white/[0.03] border border-white/[0.05] rounded-xl flex items-center gap-3">
          <div className="w-2 h-2 rounded-full bg-accent-green animate-pulse shadow-[0_0_8px_var(--accent-green)]" />
          <span className="text-[10px] font-black uppercase text-text-secondary tracking-widest leading-none">{t.node_ready_v2}</span>
        </div>
      </div>
    </aside>
  );
};

export default Sidebar;
