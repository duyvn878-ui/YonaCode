/**
 * @file ErrorBoundary.tsx
 * @brief Cầu dao an toàn (Circuit Breaker) - V6.2.3 (Fix Types)
 */

import { Component } from 'react';
import type { ErrorInfo, ReactNode } from 'react';
import { ShieldAlert } from 'lucide-react';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

class ErrorBoundary extends Component<Props, State> {
  public state: State = {
    hasError: false,
    error: null
  };

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error("Uncaught error:", error, errorInfo);
  }

  public render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center min-h-screen bg-black text-white p-10">
          <div className="w-20 h-20 rounded-full bg-red-500/10 flex items-center justify-center border border-red-500/30 mb-8 animate-pulse">
            <ShieldAlert size={40} className="text-red-500" />
          </div>
          <h1 className="text-3xl font-black uppercase tracking-tighter mb-4 italic">Hệ thống bị gián đoạn (RRF Active)</h1>
          <p className="text-white/40 font-mono text-sm mb-8 max-w-md text-center leading-relaxed">
            Hệ thống phòng thủ phản ứng nhanh (RRF) đã kích hoạt cầu dao an toàn để bảo vệ Ma trận.
          </p>
          <div className="p-6 bg-red-500/5 border border-red-500/20 rounded-2xl w-full max-w-2xl overflow-auto max-h-48 font-mono text-xs text-red-400">
             {this.state.error?.toString()}
          </div>
          <button 
            onClick={() => window.location.reload()}
            className="mt-10 px-8 py-3 bg-accent-blue text-white font-bold rounded-xl hover:bg-accent-blue/80 transition-all uppercase tracking-widest text-xs"
          >
            Tái khởi động Ma trận
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
