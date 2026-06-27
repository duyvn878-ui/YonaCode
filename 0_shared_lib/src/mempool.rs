/**
 * @file mempool.rs
 * @brief Mempool quản lý giao dịch chờ xử lý (YonaCode V1.0).
 * @details Xử lý xác thực chữ ký, kiểm tra số dư và sắp xếp theo phí giao dịch trên RAM.
 */

use std::collections::{HashMap, BinaryHeap};
use std::sync::{Mutex, OnceLock};
use crate::proto::transaction::Transaction;
use crate::state_manager::{get_state_manager};
use crate::crypto_primitives;
use prost::Message;
use anyhow::{Result, anyhow};
use std::cmp::Ordering;

#[derive(Debug, Clone)]
pub struct MempoolTx {
    pub tx: Transaction,
    pub tx_hash: [u8; 32],
    pub added_at: u128,
}

impl PartialEq for MempoolTx {
    fn eq(&self, other: &Self) -> bool {
        self.tx_hash == other.tx_hash
    }
}

impl Eq for MempoolTx {}

impl PartialOrd for MempoolTx {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for MempoolTx {
    fn cmp(&self, other: &Self) -> Ordering {
        // [VANGUARD-FEE-PRIORITY] Ưu tiên phí cao nhất.
        match self.tx.fee.cmp(&other.tx.fee) {
            Ordering::Equal => {
                // Nếu phí bằng nhau, ưu tiên giao dịch cũ hơn (FIFO)
                other.added_at.cmp(&self.added_at)
            }
            ord => ord,
        }
    }
}

pub struct Mempool {
    // Bản đồ tra cứu nhanh theo Hash
    pub transactions: Mutex<HashMap<[u8; 32], MempoolTx>>,
    // Hàng đợi ưu tiên theo Phí
    pub queue: Mutex<BinaryHeap<MempoolTx>>,
    // [VANGUARD] Theo dõi Nonce dự phóng để tránh xung đột concurrency
    pub projected_nonces: Mutex<HashMap<[u8; 32], u64>>,
    // [VANGUARD] Theo dõi tổng số tiền đang treo (Amount + Fee) để kiểm tra số dư nhanh
    pub pending_spend: Mutex<HashMap<[u8; 32], u64>>,
}

static GLOBAL_MEMPOOL: OnceLock<Mempool> = OnceLock::new();

pub fn get_mempool() -> &'static Mempool {
    GLOBAL_MEMPOOL.get_or_init(|| Mempool {
        transactions: Mutex::new(HashMap::new()),
        queue: Mutex::new(BinaryHeap::new()),
        projected_nonces: Mutex::new(HashMap::new()),
        pending_spend: Mutex::new(HashMap::new()),
    })
}

/// [VANGUARD-L3] Nạp giao dịch vào Mempool với đầy đủ các bước kiểm tra an ninh
pub fn submit_tx_to_mempool(tx_bytes: Vec<u8>) -> Result<()> {
    // 1. Giải mã Protobuf
    let tx = Transaction::decode(&tx_bytes[..])
        .map_err(|e| anyhow!("Lỗi giải mã giao dịch: {}", e))?;

    // 2. Kiểm tra định dạng cơ bản (Lớp 1 - Cảnh vệ Vòng ngoài)
    if tx.version != 1 {
        return Err(anyhow!("Phiên bản giao dịch không hỗ trợ: {}", tx.version));
    }
    
    let sender_addr = tx.sender.as_ref()
        .ok_or_else(|| anyhow!("Thiếu địa chỉ người gửi"))?;
    if sender_addr.value.len() != 32 {
        return Err(anyhow!("Độ dài địa chỉ người gửi không hợp lệ"));
    }
    let mut sender_arr = [0u8; 32];
    sender_arr.copy_from_slice(&sender_addr.value);

    // 3. Tính toán TxID và Signing Hash (Lớp 2 - An ninh Nội bộ)
    let tx_hash = crypto_primitives::calculate_tx_id(tx_bytes.clone());
    
    // Kiểm tra xem đã tồn tại trong Mempool chưa (Tránh spam)
    {
        let txs = get_mempool().transactions.lock().unwrap();
        if txs.contains_key(&tx_hash) {
            return Err(anyhow!("Giao dịch đã tồn tại trong Mempool"));
        }
    }

    let signing_hash = crypto_primitives::calculate_signing_hash(&tx);

    // 4. Xác thực chữ ký Ed25519 (Lớp 3 - Chống khủng bố)
    let sig = tx.signature.as_ref()
        .ok_or_else(|| anyhow!("Thiếu chữ ký giao dịch"))?;
    if sig.value.len() != 64 {
        return Err(anyhow!("Độ dài chữ ký không hợp lệ"));
    }
    let mut sig_arr = [0u8; 64];
    sig_arr.copy_from_slice(&sig.value);

    if !crypto_primitives::verify_ed25519_signature(&sender_arr, &signing_hash, &sig_arr) {
        return Err(anyhow!("Chữ ký không hợp lệ (Signature Verification Failed)"));
    }

    // 5. Kiểm tra Số dư và Nonce (Lớp 2 tiếp diễn)
    let mgr = get_state_manager().ok_or_else(|| anyhow!("StateManager chưa được khởi tạo"))?;
    let account_state = mgr.get_account_state(&sender_arr);

    // Chống Replay Attack
    if tx.nonce < account_state.nonce {
         return Err(anyhow!("Nonce quá thấp (Ledger: {}, Tx: {})", account_state.nonce, tx.nonce));
    }
    
    // Kiểm tra khả năng chi trả (Amount + Fee)
    let total_cost = tx.amount.checked_add(tx.fee)
        .ok_or_else(|| anyhow!("Tràn số khi tính tổng chi phí giao dịch"))?;
    
    // [VANGUARD-FEE] Kiểm tra phí tối thiểu
    if tx.fee < 250 {
        return Err(anyhow!("Phí giao dịch quá thấp (Tối thiểu 250 VNT)"));
    }

    if account_state.btc_z < total_cost {
        return Err(anyhow!("Số dư không đủ: Yêu cầu {}, Hiện có {}", total_cost, account_state.btc_z));
    }

    // 6. Chốt nạp vào RAM
    let tx_nonce = tx.nonce;
    let m_tx = MempoolTx {
        tx,
        tx_hash,
        added_at: std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos(),
    };

    let mut txs = get_mempool().transactions.lock().unwrap();
    let mut queue = get_mempool().queue.lock().unwrap();
    // [VANGUARD-UPGRADE] Cơ chế Đào thải Phí thấp (Low-Fee Eviction) & Admission Control
    if txs.len() >= 625_000 {
        // Tìm giao dịch "yếu" nhất (phí thấp nhất) để hy sinh
        // Lưu ý: BinaryHeap (Max-Heap) không hỗ trợ tìm Min hiệu quả (O(N)), 
        // nhưng với 625k TXs trên RAM, việc quét Map là cực nhanh (vài ms).
        let mut min_fee = u64::MAX;
        let mut victim_hash = None;
        let mut victim_sender = [0u8; 32];
        let mut victim_total_cost = 0u64;

        for (hash, m_tx) in txs.iter() {
            if m_tx.tx.fee < min_fee {
                min_fee = m_tx.tx.fee;
                victim_hash = Some(*hash);
                victim_sender.copy_from_slice(&m_tx.tx.sender);
                victim_total_cost = m_tx.tx.amount + m_tx.tx.fee;
            }
        }

        if let Some(h) = victim_hash {
            // Chỉ đào thải nếu giao dịch mới thực sự "ngon" hơn (phí cao hơn)
            if tx.fee > min_fee {
                log::warn!("[MEMPOOL] ♻️ Mempool đầy! Đào thải TX phí thấp: {} (Fee: {}) để nhường chỗ cho TX: {} (Fee: {})", 
                    hex::encode(&h[..4]), min_fee, hex::encode(&tx_hash[..4]), tx.fee);
                
                // 1. Xóa khỏi Map chính
                txs.remove(&h);

                // 2. Cập nhật Pending Spend của nạn nhân (Tránh kẹt số dư ảo)
                let mut pending = get_mempool().pending_spend.lock().unwrap();
                if let Some(spent) = pending.get_mut(&victim_sender) {
                    *spent = spent.saturating_sub(victim_total_cost);
                }
                drop(pending); // Giải phóng sớm

                // 3. Rebuild Queue (Heap) - Đây là thao tác đắt nhất O(N)
                // Tuy nhiên, việc Mempool đầy 625k là trạng thái cực đoan, việc rebuild đảm bảo tính nhất quán tuyệt đối.
                let all_txs: Vec<MempoolTx> = txs.values().cloned().collect();
                *queue = BinaryHeap::from(all_txs);
            } else {
                log::error!("[MEMPOOL] 🚨 Mempool đầy và phí của bạn quá thấp ({}) để thay thế mức tối thiểu ({})", tx.fee, min_fee);
                return Err(anyhow!("Mempool đã đầy (Max 625,000 TXs). Hãy tăng phí giao dịch để được ưu tiên."));
            }
        }
    }

    txs.insert(tx_hash, m_tx.clone());
    queue.push(m_tx);

    // [VANGUARD] Cập nhật số dư treo và Nonce dự phóng
    let mut pending = get_mempool().pending_spend.lock().unwrap();
    let current_spend = pending.entry(sender_arr).or_insert(0);
    *current_spend += total_cost;

    let mut nonces = get_mempool().projected_nonces.lock().unwrap();
    nonces.insert(sender_arr, tx_nonce + 1);

    log::info!("[MEMPOOL] ✅ Đã chấp nhận TX: {} (Fee: {})", hex::encode(&tx_hash[..4]), total_cost);

    Ok(())
}

/// [VANGUARD] Lấy Nonce dự phóng tiếp theo
pub fn get_next_nonce(address: [u8; 32], ledger_nonce: u64) -> u64 {
    let mut nonces = get_mempool().projected_nonces.lock().unwrap();
    let p_nonce = nonces.entry(address).or_insert(ledger_nonce);
    
    // Nếu ledger_nonce cao hơn (do block mới được commit), cập nhật p_nonce
    if ledger_nonce > *p_nonce {
        *p_nonce = ledger_nonce;
    }
    
    let result = *p_nonce;
    *p_nonce += 1;
    result
}

/// [VANGUARD] Lấy tổng số tiền đang treo của địa chỉ
pub fn get_pending_spend(address: [u8; 32]) -> u64 {
    let pending = get_mempool().pending_spend.lock().unwrap();
    *pending.get(&address).unwrap_or(&0)
}

/// [VANGUARD] Xóa danh sách giao dịch khỏi Mempool (Thường gọi sau khi Block được commit)
pub fn remove_txs(hashes: Vec<[u8; 32]>) {
    let mut txs_map = get_mempool().transactions.lock().unwrap();
    let mut pending = get_mempool().pending_spend.lock().unwrap();
    
    for hash in hashes {
        if let Some(m_tx) = txs_map.remove(&hash) {
            let sender = m_tx.tx.sender.unwrap().value;
            let mut sender_arr = [0u8; 32];
            sender_arr.copy_from_slice(&sender);
            
            let total_cost = m_tx.tx.amount + m_tx.tx.fee;
            if let Some(current) = pending.get_mut(&sender_arr) {
                if *current >= total_cost {
                    *current -= total_cost;
                } else {
                    *current = 0;
                }
            }
        }
    }
    // [NOTE] Queue không được dọn dẹp ngay lập tức (BinaryHeap không hỗ trợ remove ngẫu nhiên hiệu quả)
    // Queue sẽ tự được dọn dẹp trong get_mempool_batch thông qua kiểm tra txs_map.contains_key.
}

/// [VANGUARD] Lấy quy mô mempool hiện tại
pub fn get_mempool_size() -> usize {
    get_mempool().transactions.lock().unwrap().len()
}

/// [VANGUARD-MINER] Lấy ra N giao dịch có phí cao nhất để đóng gói vào khối
pub fn get_mempool_batch(max_count: usize) -> Vec<Vec<u8>> {
    let mut queue = get_mempool().queue.lock().unwrap();
    let mut txs_map = get_mempool().transactions.lock().unwrap();
    
    let mut batch = Vec::new();

    // Do BinaryHeap không hỗ trợ lấy mà không xóa, chúng ta sẽ Pop ra.
    // Sau khi lấy xong, nếu khối không được chốt, chúng ta có thể nạp lại hoặc để miner gọi lại.
    // [STRATEGY] Miner gọi Template -> Lấy TX. Nếu Miner tìm thấy Nonce -> Khối được gởi đi -> Mempool sạch.
    
    while batch.len() < max_count {
        if let Some(m_tx) = queue.pop() {
            // Kiểm tra xem TX còn trong Map không (Tránh double spending hoặc đã bị xóa)
            if txs_map.contains_key(&m_tx.tx_hash) {
                batch.push(m_tx.tx.encode_to_vec());
                txs_map.remove(&m_tx.tx_hash);
            }
        } else {
            break;
        }
    }

    batch
}
