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
    return this.props.children;
  }
}

export default ErrorBoundary;
