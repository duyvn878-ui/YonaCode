import React, { useState, useEffect } from 'react';
import { Terminal, Code, Clipboard, Check, Play, Plus, Trash, Info, RefreshCw, Layers, Cpu, ShieldAlert } from 'lucide-react';
import api from '../../api';

interface ExchangeViewProps {
  address: string;
  balance: number;
}

interface BatchTxInput {
  receiver: string;
  amount: string;
  base_fee: number;
}

const ExchangeView: React.FC<ExchangeViewProps> = ({ address: currentAddress, balance: currentBalance }) => {
  const [subTab, setSubTab] = useState<'sandbox' | 'docs'>('sandbox');
  const [sender, setSender] = useState(currentAddress);
  const [balance, setBalance] = useState(currentBalance);
  const [nonce, setNonce] = useState(0);
  const [expectedNonce, setExpectedNonce] = useState(0);
  const [seqNum, setSeqNum] = useState(1);
  const [password, setPassword] = useState('');
  
  // Chế độ ký lô: 'node' (Node tự ký) hoặc 'offline' (Gửi lô đã ký offline)
  const [signMode, setSignMode] = useState<'node' | 'offline'>('node');
  
  // Dành cho chế độ node-signed
  const [batchTxs, setBatchTxs] = useState<BatchTxInput[]>([
    { receiver: '0000000000000000000000000000000000000000000000000000000000000000', amount: '0.00001', base_fee: 250 },
    { receiver: '0000000000000000000000000000000000000000000000000000000000000000', amount: '0.00002', base_fee: 250 },
  ]);

  // Dành cho chế độ offline-signed (Mảng các hex giao dịch đã ký sẵn)
  const [offlineHexList, setOfflineHexList] = useState<string>('');

  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);
  const [copiedCode, setCopiedCode] = useState<string | null>(null);

  // Lấy danh sách ví nội bộ để người dùng có thể chọn nhanh làm địa chỉ Sàn
  const [wallets, setWallets] = useState<any[]>([]);

  useEffect(() => {
    api.getWallets().then(setWallets).catch(console.warn);
  }, []);

  const refreshAccountInfo = async (addr: string) => {
    if (!addr) return;
    try {
      const bal = await api.getBalance(addr);
      setBalance(bal);
      const balInfo = await api.getBalanceInfo(addr);
      setNonce(balInfo.nonce);
      setExpectedNonce(balInfo.expected_nonce);
    } catch (e) {
      console.warn("Lấy thông tin tài khoản sàn thất bại:", e);
    }
  };

  useEffect(() => {
    if (sender) {
      refreshAccountInfo(sender);
    }
  }, [sender]);

  const handleAddTx = () => {
    if (batchTxs.length >= 2500) return;
    setBatchTxs(prev => [...prev, { receiver: '', amount: '0.00001', base_fee: 250 }]);
  };

  const handleRemoveTx = (index: number) => {
    setBatchTxs(prev => prev.filter((_, i) => i !== index));
  };

  const handleTxChange = (index: number, field: keyof BatchTxInput, value: any) => {
    setBatchTxs(prev => {
      const next = [...prev];
      next[index] = { ...next[index], [field]: value };
      return next;
    });
  };

  const generateDemoTxs = () => {
    // Sinh nhanh 3 giao dịch demo tới ví Zero
    const demo = [
      { receiver: '0000000000000000000000000000000000000000000000000000000000000000', amount: '0.00005000', base_fee: 250 },
      { receiver: '0000000000000000000000000000000000000000000000000000000000000000', amount: '0.00012000', base_fee: 500 },
      { receiver: '0000000000000000000000000000000000000000000000000000000000000000', amount: '0.00008500', base_fee: 250 },
    ];
    setBatchTxs(demo);
    setSeqNum(prev => prev + 1);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setResult(null);
    setError(null);

    try {
      let params: any = {
        sender: sender,
        seq_num: Number(seqNum),
      };

      if (signMode === 'node') {
        if (!password) {
          throw new Error('Vui lòng nhập mật khẩu ví để Node thực hiện ký duyệt!');
        }
        params.password = password;
        params.transactions = batchTxs.map((tx, idx) => ({
          receiver: tx.receiver,
          amount: tx.amount,
          base_fee: Number(tx.base_fee),
          nonce: expectedNonce + idx // Bơm nonce dự phóng tự động tuần tự tránh xung đột
        }));
      } else {
        // Chế độ Offline Signed
        const hexes = offlineHexList
          .split('\n')
          .map(h => h.trim())
          .filter(h => h.length > 0);
        
        if (hexes.length === 0) {
          throw new Error('Vui lòng nhập danh sách các hex giao dịch đã ký sẵn (mỗi giao dịch một dòng)!');
        }
        params.signed_txs = hexes;
      }

      console.log("[EBP-SANDBOX] Gửi yêu cầu lô giao dịch Sàn:", params);
      const resp = await api.sendBatchTx(params);
      setResult(resp);
      
      // Tự động tăng Sequence Number cho lô tiếp theo
      setSeqNum(prev => prev + 1);
      
      // Đồng bộ lại thông tin tài khoản sàn
      setTimeout(() => refreshAccountInfo(sender), 1000);
    } catch (err: any) {
      console.error("[EBP-SANDBOX] Lỗi gửi lô giao dịch:", err);
      setError(err.message || 'Lỗi hệ thống không xác định khi gửi lô.');
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text);
    setCopiedCode(key);
    setTimeout(() => setCopiedCode(null), 2000);
  };

  // Các đoạn mã code mẫu cho Sàn
  const codeTemplates = {
    curl: `curl -X POST http://localhost:8080/api/v1/send_batch_tx \\
  -H "Content-Type: application/json" \\
  -d '{
    "sender": "${sender || '0xVíSànHex32Bytes'}",
    "seq_num": ${seqNum},
    "password": "MậtKhẩuVíSàn",
    "transactions": [
      {
        "receiver": "0000000000000000000000000000000000000000000000000000000000000000",
        "amount": "0.00015000",
        "base_fee": 250
      },
      {
        "receiver": "0000000000000000000000000000000000000000000000000000000000000000",
        "amount": "0.00030000",
        "base_fee": 500
      }
    ]
  }'`,
    js: `// Gửi Lô Giao Dịch Sequential Batch (EBP) từ Javascript (NodeJS/Browser)
const sendBatch = async () => {
  const payload = {
    sender: "${sender || '0xVíSànHex32Bytes'}",
    seq_num: ${seqNum},
    password: "MậtKhẩuVíSàn",
    transactions: [
      {
        receiver: "0000000000000000000000000000000000000000000000000000000000000000",
        amount: "0.00015000",
        base_fee: 250
      },
      {
        receiver: "0000000000000000000000000000000000000000000000000000000000000000",
        amount: "0.00030000",
        base_fee: 500
      }
    ]
  };

  try {
    const response = await fetch('http://localhost:8080/api/v1/send_batch_tx', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    
    const result = await response.json();
    if (result.status === "Success") {
      console.log("Gửi lô EBP thành công! Mã tuần tự:", result.sequence);
      console.log("Danh sách TxIDs:", result.tx_hashes);
      console.log("Logs kiểm toán (Audit Logs):", result.audit_logs);
    } else {
      console.error("Lỗi từ Node:", result.message);
    }
  } catch (error) {
    console.error("Lỗi kết nối:", error);
  }
};`,
    go: `package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type TxInput struct {
	Receiver string \`json:"receiver"\`
	Amount   string \`json:"amount"\`
	BaseFee  uint64 \`json:"base_fee"\`
}

type BatchRequest struct {
	Sender       string    \`json:"sender"\`
	SeqNum       uint64    \`json:"seq_num"\`
	Password     string    \`json:"password,omitempty"\`
	Transactions []TxInput \`json:"transactions,omitempty"\`
	SignedTxs    []string  \`json:"signed_txs,omitempty"\`
}

type BatchResponse struct {
	Status     string   \`json:"status"\`
	Sequence   uint64   \`json:"sequence"\`
	TxCount    int      \`json:"tx_count"\`
	TxHashes   []string \`json:"tx_hashes"\`
	AuditLogs  []string \`json:"audit_logs"\`
	DurationMs int64    \`json:"duration_ms"\`
}

func main() {
	url := "http://localhost:8080/api/v1/send_batch_tx"
	
	reqPayload := BatchRequest{
		Sender: "${sender || "0xVíSànHex32Bytes"}",
		SeqNum: ${seqNum},
		Password: "MậtKhẩuVíSàn",
		Transactions: []TxInput{
			{Receiver: "0000000000000000000000000000000000000000000000000000000000000000", Amount: "0.00015000", BaseFee: 250},
			{Receiver: "0000000000000000000000000000000000000000000000000000000000000000", Amount: "0.00030000", BaseFee: 500},
		},
	}

	jsonData, _ := json.Marshal(reqPayload)
	
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Lỗi kết nối: %v\\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var batchResult BatchResponse
	json.Unmarshal(body, &batchResult)

	if batchResult.Status == "Success" {
		fmt.Printf("Gửi lô EBP thành công! Lô: #%d, Tổng số giao dịch: %d\\n", batchResult.Sequence, batchResult.TxCount)
		fmt.Printf("Logs kiểm toán:\\n")
		for _, logLine := range batchResult.AuditLogs {
			fmt.Printf(" - %s\\n", logLine)
		}
	} else {
		fmt.Printf("Thất bại: %s\\n", string(body))
	}
}`
  };

  return (
    <div className="vanguard-flex-v vanguard-gap-large">
      {/* Tab Selector */}
      <div className="flex border-b border-white/5 gap-6">
        <button
          onClick={() => setSubTab('sandbox')}
          className={`pb-4 text-xs font-black uppercase tracking-[0.2em] transition-all flex items-center gap-2 ${
            subTab === 'sandbox' ? 'text-accent-blue border-b-2 border-accent-blue' : 'text-text-muted hover:text-white'
          }`}
        >
          <Terminal size={14} />
          Batch Sender Sandbox
        </button>
        <button
          onClick={() => setSubTab('docs')}
          className={`pb-4 text-xs font-black uppercase tracking-[0.2em] transition-all flex items-center gap-2 ${
            subTab === 'docs' ? 'text-accent-blue border-b-2 border-accent-blue' : 'text-text-muted hover:text-white'
          }`}
        >
          <Code size={14} />
          Developer Integration Guide
        </button>
      </div>

      {/* Content */}
      {subTab === 'sandbox' ? (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Cột trái & giữa: Cấu hình Sandbox */}
          <div className="lg:col-span-2 vanguard-flex-v vanguard-gap-medium">
            {/* Thẻ Trạng thái Sàn */}
            <div className="card p-6 bg-gradient-to-r from-accent-blue/10 via-black/40 to-black border border-white/5 rounded-3xl relative overflow-hidden">
              <div className="absolute top-0 right-0 w-32 h-32 bg-accent-blue/5 blur-[50px] rounded-full pointer-events-none" />
              <h3 className="text-sm font-black uppercase tracking-wider text-white mb-4 flex items-center gap-2">
                <Cpu size={16} className="text-accent-blue animate-pulse" />
                Thông Tin Tài Khoản Sàn (Exchange Status)
              </h3>
              
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div>
                  <label className="text-[9px] font-black uppercase tracking-widest text-text-muted block mb-2">Ví Sàn (Exchange Wallet)</label>
                  <select 
                    value={sender}
                    onChange={(e) => setSender(e.target.value)}
                    className="w-full bg-white/[0.02] border border-white/10 rounded-xl px-4 py-3 text-xs font-mono text-white focus:outline-none focus:border-accent-blue/40"
                  >
                    {wallets.map((w, idx) => (
                      <option key={w.address} value={w.address} className="bg-black text-white font-mono">
                        {w.name} (0x{w.address.slice(0, 8)}... - Bal: {(w.balance / 1e8).toFixed(4)} GO)
                      </option>
                    ))}
                    {wallets.length === 0 && (
                      <option value={sender} className="bg-black text-white font-mono">
                        {sender ? `0x${sender.slice(0, 16)}...` : 'Không tìm thấy ví nội bộ'}
                      </option>
                    )}
                  </select>
                </div>
                
                <div className="grid grid-cols-3 gap-4">
                  <div className="bg-white/[0.02] border border-white/5 p-4 rounded-xl text-center">
                    <span className="text-[8px] font-black uppercase tracking-widest text-text-muted block mb-1">Số dư Sàn</span>
                    <span className="text-xs font-black text-white font-mono">{(balance / 1e8).toFixed(6)}</span>
                    <span className="text-[7px] text-text-muted block font-mono mt-0.5">GO</span>
                  </div>
                  <div className="bg-white/[0.02] border border-white/5 p-4 rounded-xl text-center">
                    <span className="text-[8px] font-black uppercase tracking-widest text-text-muted block mb-1">Ledger Nonce</span>
                    <span className="text-xs font-black text-accent-blue font-mono">#{nonce}</span>
                  </div>
                  <div className="bg-white/[0.02] border border-white/5 p-4 rounded-xl text-center cursor-pointer hover:border-accent-blue/30 transition-all" onClick={() => refreshAccountInfo(sender)}>
                    <span className="text-[8px] font-black uppercase tracking-widest text-text-muted block mb-1">Nonce Dự phóng</span>
                    <span className="text-xs font-black text-accent-green font-mono flex items-center justify-center gap-1">
                      #{expectedNonce}
                      <RefreshCw size={10} className="animate-spin-slow text-accent-green" />
                    </span>
                  </div>
                </div>
              </div>
            </div>

            {/* Form Thiết Lập Lô Giao Dịch */}
            <form onSubmit={handleSubmit} className="card p-6 bg-black/40 border border-white/5 rounded-3xl vanguard-flex-v vanguard-gap-medium">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-white/5 pb-4">
                <h3 className="text-sm font-black uppercase tracking-wider text-white flex items-center gap-2">
                  <Layers size={16} className="text-accent-blue" />
                  Cấu Hình Lô Tuần Tự (EBP Packing Config)
                </h3>
                <button
                  type="button"
                  onClick={generateDemoTxs}
                  className="px-4 py-2 bg-accent-blue/10 hover:bg-accent-blue/20 border border-accent-blue/30 hover:border-accent-blue/50 text-[9px] font-black uppercase tracking-widest text-accent-blue rounded-xl transition-all"
                >
                  Sinh Giao Dịch Demo
                </button>
              </div>

              {/* Chế độ đóng gói/ký lô */}
              <div>
                <label className="text-[9px] font-black uppercase tracking-widest text-text-muted block mb-2">Phương Thức Ký Lô (Signing Mode)</label>
                <div className="grid grid-cols-2 gap-3">
                  <button
                    type="button"
                    onClick={() => setSignMode('node')}
                    className={`p-3 border rounded-xl text-[10px] font-black uppercase tracking-wider transition-all flex items-center justify-center gap-2 ${
                      signMode === 'node'
                        ? 'bg-accent-blue/10 border-accent-blue text-white shadow-[0_0_15px_rgba(0,136,255,0.15)]'
                        : 'bg-white/[0.01] border-white/5 text-text-muted hover:border-white/15'
                    }`}
                  >
                    Node Ký Tự Động (Stress Test / Local)
                  </button>
                  <button
                    type="button"
                    onClick={() => setSignMode('offline')}
                    className={`p-3 border rounded-xl text-[10px] font-black uppercase tracking-wider transition-all flex items-center justify-center gap-2 ${
                      signMode === 'offline'
                        ? 'bg-accent-blue/10 border-accent-blue text-white shadow-[0_0_15px_rgba(0,136,255,0.15)]'
                        : 'bg-white/[0.01] border-white/5 text-text-muted hover:border-white/15'
                    }`}
                  >
                    Lô Đã Ký Offline (Sàn Sản Xuất)
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="text-[9px] font-black uppercase tracking-widest text-text-muted block mb-2">Sequence Number (#Batch ID)</label>
                  <input
                    type="number"
                    value={seqNum}
                    onChange={(e) => setSeqNum(Math.max(1, Number(e.target.value)))}
                    className="w-full bg-white/[0.02] border border-white/10 rounded-xl px-4 py-3 text-xs font-mono text-white focus:outline-none focus:border-accent-blue/40"
                    placeholder="Mã số thứ tự lô..."
                    required
                  />
                </div>
                
                {signMode === 'node' && (
                  <div>
                    <label className="text-[9px] font-black uppercase tracking-widest text-text-muted block mb-2">Mật Khẩu Ví Sàn (Wallet Password)</label>
                    <input
                      type="password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      className="w-full bg-white/[0.02] border border-white/10 rounded-xl px-4 py-3 text-xs text-white focus:outline-none focus:border-accent-blue/40"
                      placeholder="Nhập password ví..."
                      required
                    />
                  </div>
                )}
              </div>

              {signMode === 'node' ? (
                /* CHẾ ĐỘ 1: NODE SIGNED - DANH SÁCH GIAO DỊCH */
                <div className="vanguard-flex-v vanguard-gap-tiny">
                  <div className="flex items-center justify-between mb-2">
                    <label className="text-[9px] font-black uppercase tracking-widest text-text-muted">Danh Sách Giao Dịch Trong Lô ({batchTxs.length}/2500)</label>
                    <button
                      type="button"
                      onClick={handleAddTx}
                      className="text-[9px] font-black uppercase tracking-widest text-accent-blue hover:text-white flex items-center gap-1 transition-all"
                    >
                      <Plus size={12} /> Thêm Giao Dịch
                    </button>
                  </div>

                  <div className="max-h-[300px] overflow-y-auto vanguard-flex-v vanguard-gap-tiny pr-1">
                    {batchTxs.map((tx, idx) => (
                      <div key={idx} className="p-4 bg-white/[0.01] border border-white/5 rounded-2xl flex flex-col md:flex-row items-center gap-3">
                        <div className="text-[10px] font-black text-text-muted w-6">#{idx + 1}</div>
                        
                        <div className="flex-1 w-full">
                          <input
                            type="text"
                            value={tx.receiver}
                            onChange={(e) => handleTxChange(idx, 'receiver', e.target.value)}
                            className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-[10px] font-mono text-white focus:outline-none focus:border-accent-blue/40"
                            placeholder="Địa chỉ ví nhận (0xHex32Bytes)..."
                            required
                          />
                        </div>
                        
                        <div className="w-full md:w-32">
                          <input
                            type="text"
                            value={tx.amount}
                            onChange={(e) => handleTxChange(idx, 'amount', e.target.value)}
                            className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-[10px] font-mono text-white focus:outline-none focus:border-accent-blue/40"
                            placeholder="Số tiền..."
                            required
                          />
                        </div>

                        <div className="w-full md:w-28">
                          <select
                            value={tx.base_fee}
                            onChange={(e) => handleTxChange(idx, 'base_fee', Number(e.target.value))}
                            className="w-full bg-black/40 border border-white/10 rounded-xl px-3 py-2 text-[10px] text-white focus:outline-none focus:border-accent-blue/40"
                          >
                            <option value={250}>STANDARD (250)</option>
                            <option value={500}>PRIORITY (500)</option>
                            <option value={1000}>SUPERCHARGE (1000)</option>
                          </select>
                        </div>

                        {batchTxs.length > 1 && (
                          <button
                            type="button"
                            onClick={() => handleRemoveTx(idx)}
                            className="text-accent-red/60 hover:text-accent-red p-2 transition-all"
                          >
                            <Trash size={14} />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                /* CHẾ ĐỘ 2: OFFLINE SIGNED - NHẬP HEX */
                <div className="vanguard-flex-v vanguard-gap-tiny">
                  <label className="text-[9px] font-black uppercase tracking-widest text-text-muted mb-1 block">
                    Nhập Danh Sách Hex Giao Dịch Đã Ký Sẵn (Offline Hexes)
                  </label>
                  <textarea
                    value={offlineHexList}
                    onChange={(e) => setOfflineHexList(e.target.value)}
                    rows={8}
                    className="w-full bg-white/[0.02] border border-white/10 rounded-xl px-4 py-3 text-xs font-mono text-white focus:outline-none focus:border-accent-blue/40"
                    placeholder="Dán các mã hex giao dịch vào đây, mỗi giao dịch một dòng.&#10;Ví dụ:&#10;0a8f7c6e00...02018a...&#10;0a8f7c6e01...02018b..."
                  />
                  <span className="text-[8px] text-text-muted italic">
                    * Lưu ý: Trong chế độ offline, Sàn tự duy trì Sequence, Nonce và chữ ký. Node chỉ đóng gói nhị phân thành TXSQ và chuyển tiếp.
                  </span>
                </div>
              )}

              {/* Nút gửi lô */}
              <button
                type="submit"
                disabled={loading}
                className="w-full py-4 bg-accent-blue hover:bg-accent-blue-hover text-white text-xs font-black uppercase tracking-widest rounded-xl transition-all shadow-[0_0_20px_rgba(0,136,255,0.3)] flex items-center justify-center gap-2 disabled:opacity-50"
              >
                {loading ? (
                  <>
                    <RefreshCw size={16} className="animate-spin" />
                    Đang Đóng Gói Và Truyền Lô EBP...
                  </>
                ) : (
                  <>
                    <Play size={14} fill="white" />
                    Kích Hoạt Gửi Lô Giao Dịch Tuần Tự (Submit EBP Batch)
                  </>
                )}
              </button>
            </form>
          </div>

          {/* Cột phải: Log kết quả kiểm toán (Audit Logs) */}
          <div className="lg:col-span-1">
            <div className="card p-6 bg-black/40 border border-white/5 rounded-3xl min-h-[400px] flex flex-col h-full">
              <h3 className="text-sm font-black uppercase tracking-wider text-white mb-4 flex items-center gap-2 border-b border-white/5 pb-4">
                <Terminal size={16} className="text-accent-blue" />
                Console Audit Logs (Live P2P Output)
              </h3>
              
              <div className="flex-1 bg-black/60 border border-white/5 rounded-2xl p-4 font-mono text-[10px] leading-relaxed overflow-y-auto max-h-[500px]">
                {loading && (
                  <div className="text-accent-blue animate-pulse">
                    📡 Node: Đang nhận lô... Khởi động quy trình thẩm định 5 lớp...
                  </div>
                )}
                
                {error && (
                  <div className="text-accent-red">
                    <span className="font-black">❌ LỖI NGHIỆP VỤ:</span> {error}
                  </div>
                )}

                {result && (
                  <div className="vanguard-flex-v vanguard-gap-tiny">
                    <div className="text-accent-green font-black mb-2">
                      ✅ TRUYỀN TẢI THÀNH CÔNG (EBP EXECUTED)
                    </div>
                    <div className="text-text-muted">
                      - Batch Seq: <span className="text-white">#{result.sequence}</span>
                    </div>
                    <div className="text-text-muted">
                      - Số lượng TX: <span className="text-white">{result.tx_count} giao dịch</span>
                    </div>
                    <div className="text-text-muted">
                      - Thời gian xử lý: <span className="text-white">{result.duration_ms} ms</span>
                    </div>
                    
                    <div className="border-t border-white/5 my-3 pt-3">
                      <span className="text-[9px] font-black text-text-secondary block mb-1">Tiến trình kiểm toán (P2P Ingestion Logs):</span>
                      {result.audit_logs?.map((line: string, i: number) => (
                        <div key={i} className="text-accent-blue pl-2 border-l border-accent-blue/30 my-0.5">
                          {line}
                        </div>
                      ))}
                    </div>

                    <div className="border-t border-white/5 mt-2 pt-2">
                      <span className="text-[9px] font-black text-text-secondary block mb-1">Mã băm giao dịch (TxIDs in Mempool):</span>
                      {result.tx_hashes?.map((hash: string, i: number) => (
                        <div key={i} className="text-[8px] font-mono text-white/55 truncate">
                          [{i}] {hash}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {!loading && !error && !result && (
                  <div className="text-text-muted italic text-center py-20">
                    Chưa có hoạt động. Vui lòng cấu hình và kích hoạt gửi lô giao dịch Sandbox ở bên trái.
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      ) : (
        /* TAB 2: DEVELOPER GUIDE & API DOCS */
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Cột trái & giữa: Hướng dẫn tích hợp */}
          <div className="lg:col-span-2 vanguard-flex-v vanguard-gap-medium">
            {/* Kiến trúc EBP */}
            <div className="card p-6 bg-black/40 border border-white/5 rounded-3xl">
              <h3 className="text-sm font-black uppercase tracking-wider text-white mb-4 flex items-center gap-2">
                <Info size={16} className="text-accent-blue" />
                Kiến Trúc Giao Thức EBP (Exchange Batch Protocol)
              </h3>
              
              <div className="text-xs text-text-muted leading-relaxed vanguard-flex-v vanguard-gap-tiny font-medium">
                <p>
                  YonaCode phân tách luồng giao dịch thông minh: Người dùng thường phát tán giao dịch đơn lẻ ngay lập tức với **độ trễ 0ms**. Sàn giao dịch (Exchanges) gom hàng loạt giao dịch lại và gọi API chuyên biệt để được vận chuyển tối ưu.
                </p>
                <div className="p-4 bg-white/[0.02] border border-white/5 rounded-2xl my-2">
                  <span className="text-[10px] font-black text-white uppercase block mb-2">📌 Nguyên Tắc Cốt Lõi Vận Hành:</span>
                  <ul className="list-disc list-inside flex flex-col gap-1.5 pl-2">
                    <li><span className="text-white font-bold">Batch chỉ là cơ chế vận chuyển:</span> Node nhận gói tin lô <span className="font-mono text-accent-blue">TXSQ</span> sẽ ngay lập tức rã lô (unpack) thành các giao dịch riêng lẻ (<span className="font-mono text-white">TX1, TX2, TX3...</span>) và đẩy vào mempool xử lý thông thường.</li>
                    <li><span className="text-white font-bold">Không ảnh hưởng Consensus:</span> Lớp đồng thuận không phụ thuộc vào khái niệm lô (<span className="font-mono text-text-secondary">batch #1, batch #2</span>) để tránh rủi ro fork chuỗi và phức tạp hóa hệ thống.</li>
                    <li><span className="text-white font-bold">Bảo vệ Nonce và Chống Spam:</span> API áp dụng cơ chế xác thực Fail-Fast 5 lớp nghiêm ngặt, rà soát chữ ký và số dư của toàn bộ giao dịch trước khi phân tán lên Gossip P2P.</li>
                  </ul>
                </div>
              </div>
            </div>

            {/* Tài liệu kỹ thuật chi tiết */}
            <div className="card p-6 bg-black/40 border border-white/5 rounded-3xl vanguard-flex-v vanguard-gap-medium">
              <h3 className="text-sm font-black uppercase tracking-wider text-white flex items-center gap-2 border-b border-white/5 pb-4">
                <Code size={16} className="text-accent-blue" />
                Tài Liệu Chi Tiết Endpoint REST API
              </h3>

              <div className="vanguard-flex-v vanguard-gap-medium">
                <div>
                  <div className="flex items-center gap-3 mb-2">
                    <span className="px-3 py-1 bg-accent-green/20 text-accent-green text-[10px] font-black rounded-lg">POST</span>
                    <span className="font-mono text-xs text-white">/api/v1/send_batch_tx</span>
                  </div>
                  <p className="text-xs text-text-muted leading-relaxed font-medium">
                    API dành riêng cho Sàn gửi lô giao dịch tuần tự. Node sẽ đóng gói mảng giao dịch thành cấu trúc nhị phân mang đầy đủ metadata của sàn và phát tán nguyên lô qua mạng lưới P2P Gossip.
                  </p>
                </div>

                <div className="p-4 bg-white/[0.01] border border-white/5 rounded-2xl">
                  <span className="text-[10px] font-black text-text-secondary uppercase block mb-3">Cấu trúc Request Payload JSON:</span>
                  <pre className="text-[10px] font-mono text-white/70 overflow-x-auto leading-relaxed">
{`{
  "sender": "0xVíSàn32BytesHex",  // Địa chỉ ví Sàn
  "seq_num": 1,                   // Mã số thứ tự lô để node nhận phân biệt
  "password": "MậtKhẩuVíSàn",      // Mật khẩu ví (chỉ khi nhờ node ký online)
  
  // MẢNG 1: Danh sách thông tin để Node tự chuẩn bị & ký online
  "transactions": [
    {
      "receiver": "0xVíNhận32BytesHex",
      "amount": "0.00015000",      // Số tiền GO
      "base_fee": 250,             // Phí đa tầng (250, 500, 1000)
      "nonce": 12                  // Nonce cụ thể (tùy chọn)
    }
  ],
  
  // MẢNG 2: Danh sách hex giao dịch đã ký offline sẵn từ Sàn (Sản xuất khuyên dùng)
  "signed_txs": [
    "0a8f7c6e00ab812...", 
    "0a8f7c6e00ab813..."
  ]
}`}
                  </pre>
                </div>

                <div className="p-4 bg-white/[0.01] border border-white/5 rounded-2xl">
                  <span className="text-[10px] font-black text-text-secondary uppercase block mb-3">Cấu trúc Response JSON (Thành công):</span>
                  <pre className="text-[10px] font-mono text-white/70 overflow-x-auto leading-relaxed">
{`{
  "status": "Success",
  "sequence": 1,                  // Sequence number đã xử lý
  "tx_count": 2,                  // Số lượng giao dịch trong lô
  "tx_hashes": [                  // Danh sách các TxID đã đưa vào Mempool
    "a6e87f8b9d7...",
    "b8f7c9e0d1a..."
  ],
  "audit_logs": [                 // Logs kiểm toán an toàn 5 lớp từ Node
    "🛡️ [EBP-PHASE 1] Nhận dạng lô giao dịch từ Sàn...",
    "🛡️ [EBP-PHASE 5] Đóng gói lô nhị phân TXSQ..."
  ],
  "duration_ms": 15               // Thời gian thực thi tại Node (mili giây)
}`}
                  </pre>
                </div>
              </div>
            </div>
          </div>

          {/* Cột phải: Code Mẫu (cURL, JS, Go) */}
          <div className="lg:col-span-1 vanguard-flex-v vanguard-gap-medium">
            <div className="card p-6 bg-black/40 border border-white/5 rounded-3xl vanguard-flex-v vanguard-gap-medium">
              <h3 className="text-sm font-black uppercase tracking-wider text-white flex items-center gap-2 border-b border-white/5 pb-4">
                <Code size={16} className="text-accent-blue" />
                Mã Nguồn Tích Hợp (SDK / Code Templates)
              </h3>

              {/* cURL Template */}
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <div className="flex items-center justify-between">
                  <span className="text-[9px] font-black text-text-secondary uppercase tracking-widest">cURL Command</span>
                  <button
                    onClick={() => copyToClipboard(codeTemplates.curl, 'curl')}
                    className="text-[9px] text-accent-blue hover:text-white transition-all flex items-center gap-1 font-bold"
                  >
                    {copiedCode === 'curl' ? <Check size={12} className="text-accent-green" /> : <Clipboard size={12} />}
                    {copiedCode === 'curl' ? 'Đã copy' : 'Copy cURL'}
                  </button>
                </div>
                <div className="bg-black/60 border border-white/5 rounded-xl p-3 font-mono text-[9px] text-white/80 overflow-x-auto whitespace-pre">
                  {codeTemplates.curl}
                </div>
              </div>

              {/* Javascript (Fetch) Template */}
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <div className="flex items-center justify-between">
                  <span className="text-[9px] font-black text-text-secondary uppercase tracking-widest">JavaScript (fetch)</span>
                  <button
                    onClick={() => copyToClipboard(codeTemplates.js, 'js')}
                    className="text-[9px] text-accent-blue hover:text-white transition-all flex items-center gap-1 font-bold"
                  >
                    {copiedCode === 'js' ? <Check size={12} className="text-accent-green" /> : <Clipboard size={12} />}
                    {copiedCode === 'js' ? 'Đã copy' : 'Copy JS'}
                  </button>
                </div>
                <div className="bg-black/60 border border-white/5 rounded-xl p-3 font-mono text-[9px] text-white/80 overflow-x-auto whitespace-pre max-h-[150px] overflow-y-auto">
                  {codeTemplates.js}
                </div>
              </div>

              {/* Go http.Client Template */}
              <div className="vanguard-flex-v vanguard-gap-tiny">
                <div className="flex items-center justify-between">
                  <span className="text-[9px] font-black text-text-secondary uppercase tracking-widest">Go (Golang SDK)</span>
                  <button
                    onClick={() => copyToClipboard(codeTemplates.go, 'go')}
                    className="text-[9px] text-accent-blue hover:text-white transition-all flex items-center gap-1 font-bold"
                  >
                    {copiedCode === 'go' ? <Check size={12} className="text-accent-green" /> : <Clipboard size={12} />}
                    {copiedCode === 'go' ? 'Đã copy' : 'Copy Go'}
                  </button>
                </div>
                <div className="bg-black/60 border border-white/5 rounded-xl p-3 font-mono text-[9px] text-white/80 overflow-x-auto whitespace-pre max-h-[150px] overflow-y-auto">
                  {codeTemplates.go}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default ExchangeView;
