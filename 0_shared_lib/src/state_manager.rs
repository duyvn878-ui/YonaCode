/**
 * @file state_manager.rs
 * @brief Trình quản lý trạng thái (YonaCode V1.0 Tối Giản).
 * @details Sử dụng Jellyfish Merkle Tree (JMT) với RocksDB để quản lý tài khoản Account-based.
 * đừng tự ý tối ưu đoạn code này , nó cực kì nhạy cảm , hãy cẩn thận khi làm việc với đoạn fite code này 
 * @author Vô Nhật Thiên (Khởi tạo) - YonaCode V1.0
 * @date 2026-03-24
 */

use rocksdb::{DB, Options, WriteBatch, BlockBasedOptions, Cache, WriteOptions};
use std::sync::{Arc, RwLock};
use jmt::{JellyfishMerkleTree, KeyHash};
use jmt::storage::{TreeReader, TreeWriter, Node, NodeBatch, NodeKey};
use std::sync::atomic::{AtomicU64, Ordering};
use anyhow::{Result, Context};
use borsh::{BorshSerialize, BorshDeserialize};
use serde::{Serialize, Deserialize};
use prost::Message;
use lazy_static::lazy_static;

pub const CF_JMT: &str = "jmt_nodes";
pub const CF_ACC: &str = "accounts";
pub const CF_META: &str = "meta";
pub const CF_RECEIPTS: &str = "receipts";
pub const CF_BLOCK_TXS: &str = "block_txs";
pub const CF_TOUCHED_ACCS: &str = "touched_accs"; 
pub const CF_HEADERS: &str = "headers"; // [AUDIT V10.8 FIX] Lưu trữ Header theo Hash để hỗ trợ đa nhánh (Fork)
pub const CF_BLOCKS: &str = "blocks";   // [FIX] Lưu trữ Block Hash theo chiều cao (Height)
pub const CF_BLOCK_BODIES: &str = "block_bodies"; // [V1.19 FIX] Lưu trữ Block Body (Transactions)
pub const CF_ACC_HISTORY: &str = "acc_history"; // [NEW] Lưu trữ trạng thái tài khoản theo version
pub const CF_COINBASE: &str = "coinbase"; // [V37.9.13] Lưu trữ vĩnh viễn Coinbase để kiểm toán tổng cung
pub const CF_SMT_NODES: &str = "smt_nodes"; // [CONSENSUS-FIX] Cây Merkle đồng thuận (Luôn phiên bản 0)
pub const CF_MEMPOOL: &str = "mempool"; // [V1.50] Lưu trữ Mempool vật lý chống mất dữ liệu khi restart
pub const CF_TX_INDEX: &str = "tx_index"; // [V1.60] Chỉ mục giao dịch theo địa chỉ (Thay thế tx_history.json)
pub const CF_ACC_SYNC_STAGING: &str = "accounts_staging"; // [SECURITY-FIX] Bảng tạm cho Snapshot Sync
pub const CF_REORG_BACKUP: &str = "reorg_backup"; // [VANGUARD-REORG] Lưu trữ trạng thái trước reorg để phục hồi khi cần
pub const CF_KEYHASH_TO_ADDR: &str = "keyhash_to_addr"; // [BIG-DATA] Bản đồ ánh xạ KeyHash -> Address để phân trang O(1) RAM

use crate::AccountSnapshot;

#[derive(BorshSerialize, BorshDeserialize, Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
pub struct MaturingReward {
    pub amount: u64,
    pub height: u64,
}

#[derive(BorshSerialize, BorshDeserialize, Serialize, Deserialize, Clone, Debug, PartialEq, Eq, Default)]
pub struct AccountState {
    pub btc_z: u64,   // Tổng số dư BTC_Z ()
    pub nonce: u64,   // Nonce giao dịch
    pub nano_weight: u32, // Trọng số của tài khoản (Anti-Spam)
    pub coin_id: [u8; 32], // [SCP V1.0] Định danh Coin độc nhất (Deterministic)
    pub last_full_cleanup: u64, // Chiều cao khối cuối cùng thực hiện dọn dẹp triệt để
    pub maturing_rewards: Vec<MaturingReward>, // [V1.2] Phần thưởng đang trong thời gian chờ (Maturity)
}

#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, Default)]
pub struct TrackedTx {
    pub tx_id: [u8; 32],
    pub sender: [u8; 32],
    pub receiver: [u8; 32],
    pub amount: u64,
    pub fee: u64,
    pub timestamp: i64,
    pub block_height: u64,
    pub nonce: u64,
    pub status: u32,
    pub is_finalized: bool,
    pub confirmations: u64,
    pub error_message: String,
    pub sender_prev_balance: u64,
    pub sender_post_balance: u64,
    pub receiver_prev_balance: u64,
    pub receiver_post_balance: u64,
}

pub struct StateManager {
    pub db: Arc<DB>,
    pub current_version: AtomicU64,
    pub actual_total_supply: AtomicU64, // [VANGUARD-OPTIMIZED] Cache tổng cung thực tế
}

// [DEADLOCK-FIX] Thay thế RwLock toàn cục bằng OnceLock để triệt tiêu hoàn toàn Writer Starvation Deadlock
pub static GLOBAL_STATE_MANAGER: std::sync::OnceLock<Arc<StateManager>> = std::sync::OnceLock::new();

lazy_static! {
    pub static ref RECENT_HASHES_CACHE: RwLock<std::collections::HashMap<u64, [u8; 32]>> = RwLock::new(std::collections::HashMap::new());
}

pub fn get_state_manager() -> Option<Arc<StateManager>> {
    GLOBAL_STATE_MANAGER.get().cloned()
}

impl TreeReader for StateManager {
    fn get_node_option(&self, node_key: &NodeKey) -> Result<Option<Node>> {
        let cf = self.db.cf_handle(CF_JMT).context("Missing JMT CF")?;
        let key = bincode::serialize(node_key)?;
        let data = self.db.get_cf(cf, key)?;
        match data {
            Some(d) => Ok(Some(bincode::deserialize(&d)?)),
            None => Ok(None),
        }
    }

    fn get_value_option(&self, version: jmt::Version, key_hash: KeyHash) -> Result<Option<jmt::OwnedValue>> {
        thread_local! {
            static IN_GET_VALUE_OPTION: std::cell::Cell<bool> = std::cell::Cell::new(false);
        }

        let is_recursive = IN_GET_VALUE_OPTION.with(|cell| {
            if cell.get() {
                true
            } else {
                cell.set(true);
                false
            }
        });

        if is_recursive {
            // [COMPAT-RECURSION-SHIELD-FIX] Trả về candidate phẳng thay vì dummy value vec![] để JMT build proof chính xác
            return self.get_value_option_flat(version, key_hash);
        }

        // RAII Guard để đảm bảo IN_GET_VALUE_OPTION luôn được reset về false khi thoát khỏi hàm (kể cả khi panic/return sớm)
        struct ResetGuard;
        impl Drop for ResetGuard {
            fn drop(&mut self) {
                IN_GET_VALUE_OPTION.with(|cell| cell.set(false));
            }
        }
        let _guard = ResetGuard;

        self.get_value_option_internal(version, key_hash)
    }

    fn get_rightmost_leaf(&self) -> Result<Option<(NodeKey, jmt::storage::LeafNode)>> {
        let cf = self.db.cf_handle(CF_META).context("Missing META CF")?;
        if let Some(data) = self.db.get_cf(cf, b"rightmost_leaf")? {
            if let Ok(leaf_data) = bincode::deserialize::<(NodeKey, jmt::storage::LeafNode)>(&data) {
                return Ok(Some(leaf_data));
            }
        }
        // [VANGUARD-FALLBACK-RM] Thử đọc thêm từ khóa "rm_jmt_nodes" để triệt tiêu hoàn toàn bất đồng bộ tên khóa JMT
        let key = format!("rm_{}", CF_JMT);
        if let Some(data) = self.db.get_cf(cf, key.as_bytes())? {
            if let Ok(leaf_data) = bincode::deserialize::<(NodeKey, jmt::storage::LeafNode)>(&data) {
                return Ok(Some(leaf_data));
            }
        }
        Ok(None)
    }
}

impl StateManager {
    pub fn get_value_option_flat(&self, version: jmt::Version, key_hash: KeyHash) -> Result<Option<jmt::OwnedValue>> {
        let lowest_full_height = self.get_oldest_height();
        if version < lowest_full_height {
            return Ok(None);
        }

        let cf_history = self.db.cf_handle(CF_ACC_HISTORY).context("Missing ACC_HISTORY CF")?;
        let cf_acc = self.db.cf_handle(CF_ACC).context("Missing ACC CF")?;
        let key_hash_2 = KeyHash::with::<blake3::Hasher>(&key_hash.0);

        // 1. Ứng viên từ Lịch sử CF_ACC_HISTORY (Key băm 1 lần)
        let mut search_key = [0u8; 40];
        search_key[0..32].copy_from_slice(&key_hash.0);
        search_key[32..40].copy_from_slice(&version.to_be_bytes());

        let iter = self.db.iterator_cf(cf_history, rocksdb::IteratorMode::From(&search_key, rocksdb::Direction::Reverse));
        for item in iter {
            if let Ok((k, v)) = item {
                if k.len() == 40 && k[0..32] == key_hash.0 {
                    let found_v = u64::from_be_bytes(k[32..40].try_into().unwrap());
                    if found_v <= version {
                        return Ok(Some(v.to_vec()));
                    }
                } else {
                    break;
                }
            }
        }

        // 2. Ứng viên từ Lịch sử CF_ACC_HISTORY (Key băm 2 lần)
        let mut search_key_2 = [0u8; 40];
        search_key_2[0..32].copy_from_slice(&key_hash_2.0);
        search_key_2[32..40].copy_from_slice(&version.to_be_bytes());

        let iter_2 = self.db.iterator_cf(cf_history, rocksdb::IteratorMode::From(&search_key_2, rocksdb::Direction::Reverse));
        for item in iter_2 {
            if let Ok((k, v)) = item {
                if k.len() == 40 && k[0..32] == key_hash_2.0 {
                    let found_v = u64::from_be_bytes(k[32..40].try_into().unwrap());
                    if found_v <= version {
                        return Ok(Some(v.to_vec()));
                    }
                } else {
                    break;
                }
            }
        }

        // 3. Ứng viên từ CF_ACC phẳng (Tìm ngược Address từ KeyHash)
        let mut resolved_via_index = false;
        if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
            if let Some(addr_bytes) = self.db.get_cf(cf_kh_to_addr, key_hash.0)? {
                if let Some(val) = self.db.get_cf(cf_acc, addr_bytes)? {
                    return Ok(Some(val.to_vec()));
                    resolved_via_index = true;
                }
            }
        }

        // Fallback tương thích ngược nếu chưa có index KeyHash -> Address
        if !resolved_via_index {
            if let Some(val) = self.db.get_cf(cf_acc, key_hash.0)? {
                return Ok(Some(val.to_vec()));
            }
        }

        // 4. Ứng viên từ CF_ACC phẳng (Key băm 2 lần - fallback tương thích ngược)
        if let Some(val) = self.db.get_cf(cf_acc, key_hash_2.0)? {
            return Ok(Some(val.to_vec()));
        }

        Ok(None)
    }

    fn get_value_option_internal(&self, version: jmt::Version, key_hash: KeyHash) -> Result<Option<jmt::OwnedValue>> {
        let lowest_full_height = self.get_oldest_height();

        // [ANTI-FALLBACK FIREWALL]
        // Nếu JMT yêu cầu truy vấn một phiên bản nằm sâu trong vùng đã bị Đại thanh trừng
        // Tại sao: Tránh việc fallback lấy dữ liệu sai lệch gây lệch State Root khi đồng bộ.
        if version < lowest_full_height {
            log::warn!(
                "[SMT-SECURITY] 🛑 Từ chối truy vấn phiên bản đã bị Pruned! Version y/c: #{} < Mốc an toàn: #{}.",
                version, lowest_full_height
            );
            return Err(anyhow::anyhow!("ERR_STATE_PRUNED_BY_GREAT_PURGE"));
        }

        // Thu thập các ứng viên trạng thái khả dĩ (Candidates)
        let mut candidates = Vec::new();

        let cf_history = self.db.cf_handle(CF_ACC_HISTORY).context("Missing ACC_HISTORY CF")?;
        let cf_acc = self.db.cf_handle(CF_ACC).context("Missing ACC CF")?;
        let key_hash_2 = KeyHash::with::<blake3::Hasher>(&key_hash.0);

        // 1. Ứng viên từ Lịch sử CF_ACC_HISTORY (Key băm 1 lần)
        let mut search_key = [0u8; 40];
        search_key[0..32].copy_from_slice(&key_hash.0);
        search_key[32..40].copy_from_slice(&version.to_be_bytes());

        let iter = self.db.iterator_cf(cf_history, rocksdb::IteratorMode::From(&search_key, rocksdb::Direction::Reverse));
        for item in iter {
            if let Ok((k, v)) = item {
                if k.len() == 40 && k[0..32] == key_hash.0 {
                    let found_v = u64::from_be_bytes(k[32..40].try_into().unwrap());
                    if found_v <= version {
                        candidates.push(v.to_vec());
                        break;
                    }
                } else {
                    break;
                }
            }
        }

        // 2. Ứng viên từ Lịch sử CF_ACC_HISTORY (Key băm 2 lần)
        let mut search_key_2 = [0u8; 40];
        search_key_2[0..32].copy_from_slice(&key_hash_2.0);
        search_key_2[32..40].copy_from_slice(&version.to_be_bytes());

        let iter_2 = self.db.iterator_cf(cf_history, rocksdb::IteratorMode::From(&search_key_2, rocksdb::Direction::Reverse));
        for item in iter_2 {
            if let Ok((k, v)) = item {
                if k.len() == 40 && k[0..32] == key_hash_2.0 {
                    let found_v = u64::from_be_bytes(k[32..40].try_into().unwrap());
                    if found_v <= version {
                        candidates.push(v.to_vec());
                        break;
                    }
                } else {
                    break;
                }
            }
        }

        // 3. Ứng viên từ CF_ACC phẳng (Tìm ngược Address từ KeyHash)
        let mut resolved_via_index = false;
        if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
            if let Some(addr_bytes) = self.db.get_cf(cf_kh_to_addr, key_hash.0)? {
                if let Some(val) = self.db.get_cf(cf_acc, addr_bytes)? {
                    candidates.push(val.to_vec());
                    resolved_via_index = true;
                }
            }
        }

        // Fallback tương thích ngược nếu chưa có index KeyHash -> Address
        if !resolved_via_index {
            if let Some(val) = self.db.get_cf(cf_acc, key_hash.0)? {
                candidates.push(val.to_vec());
            }
        }

        // 4. Ứng viên từ CF_ACC phẳng (Key băm 2 lần - fallback tương thích ngược)
        if let Some(val) = self.db.get_cf(cf_acc, key_hash_2.0)? {
            candidates.push(val.to_vec());
        }

        if candidates.is_empty() {
            return Ok(None);
        }

        // TẠI SAO PHẢI XÓA BỎ BẢN VÁ O(1) CHO candidates.len() == 1?
        // Bản vá O(1) trước đây giả định rằng nếu chỉ có 1 ứng viên trong danh sách thì có thể trả về ngay lập tức.
        // Tuy nhiên, điều này cực kỳ nguy hiểm và vi phạm quy tắc đồng thuận khi Node đồng bộ các khối trong quá khứ (Historical Sync):
        // 1. CF_ACC phẳng luôn lưu trữ trạng thái mới nhất tại đỉnh chuỗi (ví dụ cao độ #53744).
        // 2. Khi Node đồng bộ một khối cũ (ví dụ cao độ #53225), version yêu cầu là quá khứ, nhưng ứng viên duy nhất tìm thấy
        //    lại đến từ CF_ACC phẳng của tương lai.
        // 3. Nếu trả về ngay lập tức không qua xác thực, Node sẽ nhận sai số dư lịch sử, dẫn đến tính toán sai State Root
        //    và gây ra lỗi nghiêm trọng "LỆCH STATE ROOT", khiến tiến trình đồng bộ bị từ chối liên tục (Sync Loop block).
        // Do đó, bắt buộc phải loại bỏ bypass này và chạy xác thực Merkle Proof toán học đầy đủ để bảo đảm tính nhất quán tuyệt đối.
        // Để tối ưu hóa hiệu năng và tránh nghẽn CPU/starvation, chúng ta tắt hoàn toàn các dòng log eprintln! rác ra console.


        // Gọi JMT để lấy proof và tự động xác thực toán học các ứng viên
        let jmt_tree = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        match jmt_tree.get_with_proof(key_hash, version) {
            Ok((Some(val), proof)) => {
                let root_hash = jmt_tree.get_root_hash(version).unwrap_or(jmt::RootHash([0u8; 32]));
                
                for candidate in candidates.iter() {
                    let res = proof.verify(root_hash, key_hash, Some(candidate));
                    if res.is_ok() {
                        return Ok(Some(candidate.clone()));
                    }
                }

                // [SHIELD-JMT-COMPAT] Trả về trực tiếp giá trị chuẩn JMT nếu lịch sử phẳng bị Prune!
                log::warn!(
                    "[StateManager] 🛡️ Phát hiện khuyết lịch sử phẳng cho key_hash {} tại version {}. Sử dụng dữ liệu gốc JMT.",
                    hex::encode(&key_hash.0[..4]),
                    version
                );
                return Ok(Some(val));
            }
            Ok((None, _)) => {
                // [VANGUARD-JMT-COMPAT-FIX] JMT xác nhận tài khoản không tồn tại tại version này!
                // Trả về None ngay lập tức để tránh việc fallback lấy sai lệch số dư làm hỏng rollback.
                return Ok(None);
            }
            Err(e) => {
                // Chỉ in log cảnh báo khi thực sự có lỗi toán học phát sinh
                eprintln!("[JMT-COMPAT] ⚠️ get_with_proof failed for key_hash {} at version {}: {:?}", hex::encode(&key_hash.0[..4]), version, e);
            }
        }

        // [FALLBACK] Nếu không khớp JMT proof toán học, trả về ứng viên đầu tiên tìm thấy để giữ tính tương thích ngược
        Ok(candidates.first().cloned())
    }
}

impl TreeWriter for StateManager {
    fn write_node_batch(&self, node_batch: &NodeBatch) -> Result<()> {
        self.write_node_batch_custom(node_batch, CF_JMT)
    }
}

/// [CONSENSUS-FIX] Wrapper để JMT có thể làm việc trên các CF khác nhau (JMT vs SMT)
pub struct SmtStorage<'a> {
    pub mgr: &'a StateManager,
    pub cf: &'static str,
}

impl<'a> TreeReader for SmtStorage<'a> {
    fn get_node_option(&self, node_key: &NodeKey) -> Result<Option<Node>> {
        let cf = self.mgr.db.cf_handle(self.cf).context("Missing CF")?;
        let key = bincode::serialize(node_key)?;
        let data = self.mgr.db.get_cf(cf, key)?;
        match data {
            Some(d) => Ok(Some(bincode::deserialize(&d)?)),
            None => Ok(None),
        }
    }

    fn get_value_option(&self, _version: jmt::Version, key_hash: KeyHash) -> Result<Option<jmt::OwnedValue>> {
        let cf_acc = self.mgr.db.cf_handle(CF_ACC).context("Missing ACC CF")?;
        
        // 1. Thử tra cứu ngược KeyHash -> Address từ RocksDB
        if let Some(cf_kh_to_addr) = self.mgr.db.cf_handle(CF_KEYHASH_TO_ADDR) {
            if let Some(addr_bytes) = self.mgr.db.get_cf(cf_kh_to_addr, key_hash.0)? {
                if let Some(data) = self.mgr.db.get_cf(cf_acc, addr_bytes)? {
                    return Ok(Some(data));
                }
            }
        }
        
        // 2. Fallback tương thích ngược: Tìm trực tiếp bằng key_hash
        let data = self.mgr.db.get_cf(cf_acc, key_hash.0)?;
        Ok(data)
    }

    fn get_rightmost_leaf(&self) -> Result<Option<(NodeKey, jmt::storage::LeafNode)>> {
        let cf = self.mgr.db.cf_handle(CF_META).context("Missing META CF")?;
        let key = format!("rm_{}", self.cf);
        if let Some(data) = self.mgr.db.get_cf(cf, key.as_bytes())? {
            if let Ok(leaf_data) = bincode::deserialize::<(NodeKey, jmt::storage::LeafNode)>(&data) {
                return Ok(Some(leaf_data));
            }
        }
        Ok(None)
    }
}

impl<'a> TreeWriter for SmtStorage<'a> {
    fn write_node_batch(&self, node_batch: &NodeBatch) -> Result<()> {
        self.mgr.write_node_batch_custom(node_batch, self.cf)
    }
}

/// [VANGUARD-GHOST-OVERLAY] Bộ nhớ đệm RAM O(1) cho JMT / SMT trong quá trình Ghost Execution.
/// Giúp giả lập tính toán witness và Merkle root mà không ghi đĩa và không làm hỏng DB.
/// Hỗ trợ truy cập đa luồng an toàn thông qua RwLock và giải quyết bug ví mới.
pub struct SmtGhostOverlay<'a> {
    pub mgr: &'a StateManager,
    pub node_cache: std::sync::RwLock<std::collections::HashMap<jmt::storage::NodeKey, jmt::storage::Node>>,
    // Cache trạng thái: Key là Address (32 bytes)
    pub state_cache: std::sync::RwLock<std::collections::HashMap<[u8; 32], AccountState>>,
    // [BUGFIX VÍ MỚI] Cache ánh xạ KeyHash -> Address cho các ví sinh ra trong lúc giả lập
    pub new_keyhashes: std::sync::RwLock<std::collections::HashMap<[u8; 32], [u8; 32]>>,
}

impl<'a> SmtGhostOverlay<'a> {
    pub fn new(mgr: &'a StateManager) -> Self {
        Self {
            mgr,
            node_cache: std::sync::RwLock::new(std::collections::HashMap::new()),
            state_cache: std::sync::RwLock::new(std::collections::HashMap::new()),
            new_keyhashes: std::sync::RwLock::new(std::collections::HashMap::new()),
        }
    }

    pub fn get_account_state(&self, address: &[u8; 32]) -> AccountState {
        // 1. O(1) Lookup trên RAM Cache trước
        let mut state = if let Some(state) = self.state_cache.read().unwrap().get(address) {
            state.clone()
        } else {
            // 2. Fallback đọc từ DB gốc nếu RAM chưa có vết
            self.mgr.get_account_state(address)
        };

        // [VANGUARD-ZEN-PAYOUT-LAZY] Tự động giải ngân động các phần thưởng đã chín ngay khi đọc trạng thái ví.
        // Giải pháp này loại bỏ hoàn toàn tình trạng Catch-22 (Ví thợ đào không thể chi tiêu phần thưởng chín
        // vì btc_z chưa được cập nhật và giao dịch bị mempool từ chối trước do Insufficient Balance).
        let current_height = self.mgr.get_current_version();
        let mut payout_total: u64 = 0;
        state.maturing_rewards.retain(|reward| {
            if current_height >= reward.height {
                payout_total += reward.amount;
                false
            } else {
                true
            }
        });
        if payout_total > 0 {
            state.btc_z = state.btc_z.saturating_add(payout_total);
        }
        state
    }

    pub fn get_account_state_at_height(&self, address: &[u8; 32], _height: u64) -> Option<AccountState> {
        // Overlay RAM của một mẻ sync luôn lưu trữ trạng thái mới nhất,
        // do đó ta trỏ thẳng về get_account_state để đạt O(1)
        Some(self.get_account_state(address))
    }

    pub fn put_account_state(&self, address: [u8; 32], state: AccountState) {
        // 1. Lưu state vào RAM
        self.state_cache.write().unwrap().insert(address, state);
        
        // 2. Lưu ánh xạ KeyHash -> Address cho JMT (Rất quan trọng cho ví mới)
        let kh = KeyHash::with::<blake3::Hasher>(&address).0;
        self.new_keyhashes.write().unwrap().insert(kh, address);
    }

    pub fn get_merkle_proof_at_height(&self, address: &[u8; 32], height: u64) -> Vec<[u8; 32]> {
        let jmt_tree = JellyfishMerkleTree::<'_, SmtGhostOverlay, blake3::Hasher>::new(self);
        let key_hash = KeyHash::with::<blake3::Hasher>(address);

        if let Ok((_value, proof)) = jmt_tree.get_with_proof(key_hash, height) {
            let json = serde_json::to_string(&proof).unwrap_or_default();
            let fork_proof: crate::SparseMerkleProof = serde_json::from_str(&json).unwrap_or_default();
            return fork_proof.siblings;
        }
        Vec::new()
    }

    pub fn write_node_batch_ram(&self, node_batch: &NodeBatch) -> Result<()> {
        let mut cache = self.node_cache.write().unwrap();
        for (node_key, node) in node_batch.nodes() {
            cache.insert(node_key.clone(), node.clone());
        }
        Ok(())
    }
}

impl<'a> TreeReader for SmtGhostOverlay<'a> {
    fn get_node_option(&self, node_key: &NodeKey) -> Result<Option<Node>> {
        // O(1) check RAM
        if let Some(node) = self.node_cache.read().unwrap().get(node_key) {
            return Ok(Some(node.clone()));
        }
        // Fallback đọc từ RocksDB (CF_JMT)
        let cf = self.mgr.db.cf_handle(CF_JMT).context("Missing JMT CF")?;
        let key = bincode::serialize(node_key)?;
        let data = self.mgr.db.get_cf(cf, key)?;
        match data {
            Some(d) => Ok(Some(bincode::deserialize(&d)?)),
            None => Ok(None),
        }
    }

    fn get_value_option(&self, _version: jmt::Version, key_hash: KeyHash) -> Result<Option<jmt::OwnedValue>> {
        // 1. Thử giải mã KeyHash từ RAM Cache trước (Các ví bị tác động trong Simulation)
        let addr_from_ram = self.new_keyhashes.read().unwrap().get(&key_hash.0).cloned();
        
        let addr_bytes = if let Some(a) = addr_from_ram {
            Some(a.to_vec())
        } else {
            // 2. Fallback tìm KeyHash trong RocksDB nếu RAM không có
            if let Some(cf) = self.mgr.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                self.mgr.db.get_cf(cf, key_hash.0).unwrap_or(None)
            } else {
                None
            }
        };

        let cf_acc = self.mgr.db.cf_handle(CF_ACC).context("Missing ACC CF")?;

        // Nếu xác định được Address, lấy State từ RAM hoặc DB phẳng
        if let Some(addr_vec) = addr_bytes {
            let mut address = [0u8; 32];
            address.copy_from_slice(&addr_vec);
            
            // a. Lấy từ RAM cache trước
            if let Some(state) = self.state_cache.read().unwrap().get(&address) {
                return Ok(Some(borsh::to_vec(&state).unwrap_or_default()));
            }
            
            // b. Tra cứu phẳng trong DB bằng Address thực tế
            if let Some(val) = self.mgr.db.get_cf(cf_acc, address)? {
                return Ok(Some(val));
            }
        }
        
        // 3. Fallback cuối cùng: Đọc bằng key_hash.0 trực tiếp từ CF_ACC để tương thích ngược
        Ok(self.mgr.db.get_cf(cf_acc, key_hash.0)?)
    }

    fn get_rightmost_leaf(&self) -> Result<Option<(NodeKey, jmt::storage::LeafNode)>> {
        // Ít khi được gọi trong execution, redirect thẳng xuống DB gốc
        self.mgr.get_rightmost_leaf()
    }
}

impl<'a> TreeWriter for SmtGhostOverlay<'a> {
    fn write_node_batch(&self, node_batch: &NodeBatch) -> Result<()> {
        self.write_node_batch_ram(node_batch)
    }
}

impl StateManager {
    pub fn write_node_batch_custom(&self, node_batch: &NodeBatch, cf_name: &str) -> Result<()> {
        let cf = self.db.cf_handle(cf_name).context("Missing CF")?;
        let meta_cf = self.db.cf_handle(CF_META).context("Missing META CF")?;
        
        let mut batch = WriteBatch::default();
        let mut max_key = [0u8; 32];
        let mut rightmost: Option<(NodeKey, jmt::storage::LeafNode)> = None;

        for (node_key, node) in node_batch.nodes() {
            let key = bincode::serialize(node_key)?;
            let value = bincode::serialize(node)?;
            batch.put_cf(cf, key, value);

            if let jmt::storage::Node::Leaf(leaf) = node {
                if leaf.key_hash().0 > max_key {
                    max_key = leaf.key_hash().0;
                    rightmost = Some((node_key.clone(), leaf.clone()));
                }
            }
        }

        if let Some(rm) = rightmost {
            let key = format!("rm_{}", cf_name);
            batch.put_cf(meta_cf, key.as_bytes(), bincode::serialize(&rm)?);
            // [VANGUARD-RM-COMPAT] Ghi thêm khóa rightmost_leaf tiêu chuẩn nếu đang cập nhật CF_JMT chính
            if cf_name == CF_JMT {
                batch.put_cf(meta_cf, b"rightmost_leaf", bincode::serialize(&rm)?);
            }
        }

        // [VANGUARD-ATOMIC] Chốt chặn nguyên tử: Ghi toàn bộ dữ liệu hoặc không ghi gì.
        let mut opts = WriteOptions::default();
        opts.set_sync(true); // Đảm bảo dữ liệu thực sự nằm trên đĩa vật lý
        self.db.write_opt(batch, &opts)?;
        
        Ok(())
    }
}

impl StateManager {
    pub fn try_new(path: String) -> Result<Arc<Self>> {
        let mut opts = Options::default();
        opts.create_if_missing(true);
        opts.create_missing_column_families(true);
        
        // [HYBRID ENGINE V2.0] Phân bổ 256MB RAM cho Block Cache (An toàn cho mọi máy tính)
        let mut block_opts = BlockBasedOptions::default();
        let cache = Cache::new_lru_cache(256 * 1024 * 1024); // 256MB
        block_opts.set_block_cache(&cache);
        opts.set_block_based_table_factory(&block_opts);

        // [HYBRID ENGINE V2.0] Tối ưu hóa Write Path
        opts.set_max_write_buffer_number(4);
        opts.set_write_buffer_size(256 * 1024 * 1024); // Nâng lên 256MB để tránh hiện tượng Write Stall khi lưu trữ khối lớn 35MB dưới dạng WriteBatch khổng lồ
        
        println!("[DB-TRACE] 🚀 Khởi tạo thông số Động cơ Hybrid...");

        let cfs = ["default", CF_JMT, CF_ACC, CF_META, CF_RECEIPTS, CF_BLOCKS, CF_BLOCK_BODIES, CF_BLOCK_TXS, CF_TOUCHED_ACCS, CF_HEADERS, CF_COINBASE, CF_SMT_NODES, CF_ACC_HISTORY, CF_MEMPOOL, CF_TX_INDEX, CF_ACC_SYNC_STAGING, CF_REORG_BACKUP, CF_KEYHASH_TO_ADDR];
        println!("[DB-TRACE] 📥 Đang mở RocksDB tại: {}", path);
        let db = match DB::open_cf(&opts, &path, cfs) {
            Ok(db) => {
                println!("[DB-TRACE] ✅ Mở RocksDB thành công.");
                db
            },
            Err(e) => {
                eprintln!("[DB-TRACE] ❌ KHÔNG THỂ MỞ ROCKSDB TẠI: {}", path);
                eprintln!("[DB-TRACE] ❌ LỖI HỆ THỐNG: {}", e);
                if e.as_ref().contains("lock") {
                    eprintln!("[DB] 💡 GỢI Ý: Một tiến trình scl_server.exe khác đang giữ khóa. Vui lòng đóng tất cả các cửa sổ terminal cũ.");
                }
                return Err(anyhow::anyhow!("Failed to open RocksDB: {}", e));
            }
        };

        let db_arc = Arc::new(db);
        
        let meta_cf = db_arc.cf_handle(CF_META).expect("Missing META CF");
        let current_version = match db_arc.get_cf(meta_cf, b"jmt_v").unwrap_or(None) {
            Some(v) => {
                if v.len() == 8 {
                    u64::from_le_bytes(v.try_into().unwrap())
                } else { 0 }
            },
            None => 0,
        };

        println!("[HEALING-DEBUG] 🩺 Kiểm tra tính toàn vẹn: Current Version = #{}", current_version);
        let mgr = Arc::new(Self { 
            db: db_arc,
            current_version: AtomicU64::new(current_version),
            actual_total_supply: AtomicU64::new(0), // Sẽ được nạp ngay sau đây
        });

        let finalized_h = mgr.get_finalized_height();
        // [HEALING-V1] Tự chữa lành nếu phát hiện mất nhất quán giữa Meta và Ledger
        let mut check_version = current_version;
        
        // [HEALING-EMERGENCY] Kiểm tra file tín hiệu ép buộc rollback
        let force_heal_path = std::path::Path::new(&path).join("FORCE_HEAL");
        if force_heal_path.exists() && check_version > 0 {
            log::warn!("[HEALING-FORCED] 🚀 Phát hiện file tín hiệu FORCE_HEAL. Đang ép buộc Rollback từ #{} về #{}...", check_version, check_version - 1);
            if let Err(e) = mgr.rollback_state(check_version, check_version - 1) {
                log::error!("[HEALING-CRITICAL] ❌ Lỗi ép buộc Rollback: {}", e);
            } else {
                let _ = std::fs::remove_file(force_heal_path);
                log::info!("[HEALING-SUCCESS] ✅ Đã hoàn tất ép buộc Rollback. Đã xóa file tín hiệu.");
                check_version -= 1;
            }
        }

        while check_version > finalized_h && check_version > 0 {
            if mgr.is_block_complete(check_version) {
                break;
            }
            
            log::warn!("[HEALING] ⚠️ Phát hiện mất nhất quán tại #{} (Thiếu Header/Body). Đang tự động Rollback về #{}...", check_version, check_version - 1);
            if let Err(e) = mgr.rollback_state(check_version, check_version - 1) {
                log::error!("[HEALING-CRITICAL] ❌ Không thể tự động Rollback: {}", e);
                break;
            }
            check_version -= 1;
        }

        // [HEALING-V1.1] Kiểm toán tính nhất quán của Finality (Chốt chặn bảo mật thực tế)
        let finalized_h = mgr.get_finalized_height();
        println!("[HEALING-DEBUG] 🔒 Finalized Height = #{} | State Version = #{}", finalized_h, check_version);
        if finalized_h > check_version {
            println!("[HEALING-CRITICAL] 🚨 PHÁT HIỆN MÂU THUẪN FINALITY: Chốt #{} > Thực tế #{}. Đang căn chỉnh lại...", finalized_h, check_version);
            mgr.force_set_finalized_height(check_version);
        }

        // [VANGUARD-FIX TRIỆT ĐỂ] Khôi phục Tổng cung từ đĩa vật lý (O(1)) thay vì quét O(N)
        let cf_meta = mgr.db.cf_handle(CF_META).expect("Missing META CF");
        let mut audited_supply = match mgr.db.get_cf(cf_meta, b"actual_supply_v2").unwrap_or(None) {
            Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
            None => 0,
        };

        // Nếu Cache rỗng nhưng có khối, ta có thể an tâm lấy theo lý thuyết để chạy tiếp
        if audited_supply == 0 && current_version > 0 {
            audited_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(current_version);
            log::warn!("⚠️ [STARTUP-REPAIR] 🔴 Phát hiện tổng cung bị thất lạc (0 VNT) tại cao độ #{}. Đang ép về chuẩn Hiến pháp: {} VNT", current_version, audited_supply);
        } else if current_version > 0 {
            // [VANGUARD-AUDIT] Kiểm tra xem có lệch so với fallback không
            let expected = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(current_version);
            if audited_supply != expected {
                 log::error!("⚠️ [STARTUP-AUDIT] 🔴 Cảnh báo: Tổng cung DB ({}) lệch so với Kỳ vọng ({}) tại #{}", audited_supply, expected, current_version);
            }
        }

        mgr.actual_total_supply.store(audited_supply, Ordering::SeqCst);
        log::info!("✅ [AUDIT-STARTUP] Đã nạp Tổng cung an toàn: {} BTC_Z tại cao độ #{}", audited_supply as f64 / 1e8, current_version);


        Ok(mgr)
    }

    pub fn is_block_complete(&self, height: u64) -> bool {
        let finalized_h = self.get_finalized_height();
        if let Some(hash) = self.get_block_hash(height) {
            let h_exists = self.get_header_raw(&hash).is_some();
            if height <= finalized_h {
                // [SNAP-SYNC-SUPPORT] Trong vùng đã chốt hạ (Finalized), chỉ cần Header là đủ "hoàn tất".
                // Điều này cho phép Node chạy Snap Sync mà không bị Healing logic xóa dữ liệu.
                h_exists
            } else {
                // [NORMAL-SYNC] Ngoài vùng Snap Sync, bắt buộc phải có cả Body để thực thi.
                let b_exists = self.get_block_body_raw(&hash).is_some();
                h_exists && b_exists
            }
        } else {
            false
        }
    }

    pub fn delete_by_hash(&self, hash: &[u8; 32]) -> Result<()> {
        let headers_cf = self.db.cf_handle(CF_HEADERS).context("Missing CF_HEADERS")?;
        let bodies_cf = self.db.cf_handle(CF_BLOCK_BODIES).context("Missing CF_BLOCK_BODIES")?;
        
        let mut batch = WriteBatch::default();
        batch.delete_cf(headers_cf, hash);
        batch.delete_cf(bodies_cf, hash);
        
        let mut opts = WriteOptions::default();
        opts.set_sync(true);
        self.db.write_opt(batch, &opts)?;
        Ok(())
    }

    /// [Reorg Manager] Hoàn tác toàn bộ dữ liệu lịch sử (Blocks, Receipts) và trạng thái. 
    /// Dùng cho trường hợp xác định rẽ nhánh tại tầng Đồng thuận.
    pub fn rollback_state(&self, current_height: u64, target_height: u64) -> Result<()> {
        if target_height >= current_height { return Ok(()); }
        
        let finalized_h = self.get_finalized_height();
        if target_height < finalized_h {
            return Err(anyhow::anyhow!("ERR_FINALITY_VIOLATION: Node cố gắng rollback thoát ly khỏi vùng an toàn đã chốt #{}! (Target: #{})", finalized_h, target_height));
        }
        
        let mut batch = WriteBatch::default();
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let blocks_cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        let txs_cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");
        let coinbase_cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");

        // 1. Dọn dẹp receipts, records và dọn sạch CF_ACC cho từng khối bị orphaned
        let touched_cf = self.db.cf_handle(CF_TOUCHED_ACCS).expect("Missing TOUCHED CF");

        for h in (target_height + 1)..=current_height {
            let h_bytes = h.to_le_bytes();
            
            // Xóa block hash
            batch.delete_cf(blocks_cf, h_bytes);
            
            // Xóa Receipts
            if let Some(tx_hashes_raw) = self.db.get_cf(txs_cf, h_bytes)? {
                if let Ok(tx_hashes) = <Vec<[u8; 32]>>::try_from_slice(&tx_hashes_raw) {
                    for tx_hash in tx_hashes {
                        batch.delete_cf(receipt_cf, tx_hash);
                    }
                }
            }

            if let Some(accs_raw) = self.db.get_cf(touched_cf, h_bytes)? {
                if let Ok(accs) = <Vec<[u8; 32]>>::try_from_slice(&accs_raw) {
                    let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
                    let history_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF"); // [VÁ LỖI] Lấy Handle History
                    let jmt = jmt::JellyfishMerkleTree::<'_, Self, blake3::Hasher>::new(self);

                    for acc_addr in accs {
                        let key_hash = KeyHash::with::<blake3::Hasher>(&acc_addr);

                        // ================= [VÁ LỖI BẢO MẬT] =================
                        // Quét sạch Zombie State trong Lịch sử trước khi phục hồi
                        // Xóa cấu trúc khóa versioned_key [KeyHash][Height]
                        let mut versioned_key = [0u8; 40];
                        versioned_key[0..32].copy_from_slice(&key_hash.0);
                        versioned_key[32..40].copy_from_slice(&h.to_be_bytes()); 
                        batch.delete_cf(history_cf, &versioned_key);
                        // ====================================================

                        // [VANGUARD-FIX] Sử dụng JMT để lấy trạng thái chính xác nhất tại target_height
                        // JMT sẽ tự động lùi về phiên bản gần nhất của tài khoản này.
                        match jmt.get(key_hash, target_height) {
                            Ok(Some(val)) => {
                                batch.put_cf(acc_cf, acc_addr, &val);
                                if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                                    batch.put_cf(cf_kh_to_addr, key_hash.0, acc_addr);
                                }
                            },
                            _ => {
                                // Nếu JMT không thấy tài khoản này tại target_height, 
                                // nghĩa là nó thực sự chưa tồn tại -> Xóa khỏi bảng phẳng.
                                batch.delete_cf(acc_cf, acc_addr);
                                if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                                    batch.delete_cf(cf_kh_to_addr, key_hash.0);
                                }
                            }
                        }
                    }
                }
            }
            
            // Xóa metadata của khối
            batch.delete_cf(txs_cf, h_bytes);
            batch.delete_cf(touched_cf, h_bytes);

            // [V1.0.4 ROLLBACK-HISTORY] Xóa sạch cả Header, Block Index và Body để đồng bộ History
            if let Some(headers_cf) = self.db.cf_handle(CF_HEADERS) {
                if let Some(h_hash) = self.db.get_cf(self.db.cf_handle(CF_BLOCKS).unwrap(), h_bytes).unwrap_or(None) {
                    batch.delete_cf(headers_cf, h_hash);
                }
            }
            if let Some(blocks_cf) = self.db.cf_handle(CF_BLOCKS) {
                batch.delete_cf(blocks_cf, h_bytes);
            }
            if let Some(bodies_cf) = self.db.cf_handle(CF_BLOCK_BODIES) {
                batch.delete_cf(bodies_cf, h_bytes);
            }
            
            // [VANGUARD-REORG-FIX] Dọn dẹp coinbase để tránh nợ kỹ thuật
            batch.delete_cf(coinbase_cf, h_bytes);
        }

        // 2. Cập nhật JMT Version để khớp với trạng thái khối đích
        batch.put_cf(meta_cf, b"jmt_v", target_height.to_le_bytes());
        
        // [HOTFIX V1.17] Rollback Expected Supply Cache về target_height
        let reverted_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(target_height);
        batch.put_cf(meta_cf, b"actual_supply_v2", reverted_supply.to_le_bytes());
        batch.put_cf(meta_cf, b"expected_total_supply", reverted_supply.to_le_bytes());

        let mut opts = WriteOptions::default();
        opts.set_sync(true);
        self.db.write_opt(batch, &opts).unwrap();
        self.current_version.store(target_height, Ordering::SeqCst);
        
        // [VANGUARD-FIX] Đồng bộ hóa bộ đếm RAM với giá trị đã rollback trong DB để tránh sai lệch cung tiền
        self.actual_total_supply.store(reverted_supply, Ordering::SeqCst);
        
        // [VANGUARD-REPAIR] JMT versioning tự động xử lý rollback khi ta cập nhật jmt_v.
        // Không cần rebuild cây đồng thuận riêng biệt nữa.

        log::info!("[ROLLBACK-BLOCK] 🔄 Đã hoàn tác lịch sử về khối #{} (từ #{})", target_height, current_height);
        log::info!("[ROLLBACK-SUPPLY] ⚖️ Cân bằng lại tổng cung về: {} VNT", reverted_supply);

        // Xóa sạch cache gần nhất để tránh zombie block hashes
        if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
            cache.clear();
        }
        Ok(())
    }

    /// [BÀN TAY VÔ HÌNH] Xóa khối vật lý — KHÔNG kiểm tra Tường lửa Bất biến.
    /// Tại sao tách riêng: Hàm rollback_state bảo vệ hệ thống khỏi reorg tự động từ P2P.
    /// Hàm này là công cụ thủ công cho nhà vận hành node, chỉ gọi qua localhost + mã xác nhận.
    /// Bảo mật: Không ảnh hưởng đến rollback_state gốc (Tường lửa vẫn nguyên vẹn).
    pub fn force_delete_blocks(&self, current_height: u64, target_height: u64) -> Result<()> {
        if target_height >= current_height { return Ok(()); }
        
        // ⚠️ KHÔNG CÓ KIỂM TRA FINALITY — Đây là tính năng xóa vật lý có chủ đích
        log::warn!("[INVISIBLE-HAND] ☢️ Xóa vật lý {} khối (từ #{} về #{}). Bỏ qua Tường lửa Bất biến.", 
            current_height - target_height, current_height, target_height);
        
        let mut batch = WriteBatch::default();
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let blocks_cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        let txs_cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");
        let coinbase_cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");
        let touched_cf = self.db.cf_handle(CF_TOUCHED_ACCS).expect("Missing TOUCHED CF");

        for h in (target_height + 1)..=current_height {
            let h_bytes = h.to_le_bytes();
            
            batch.delete_cf(blocks_cf, h_bytes);
            
            if let Some(tx_hashes_raw) = self.db.get_cf(txs_cf, h_bytes)? {
                if let Ok(tx_hashes) = <Vec<[u8; 32]>>::try_from_slice(&tx_hashes_raw) {
                    for tx_hash in tx_hashes {
                        batch.delete_cf(receipt_cf, tx_hash);
                    }
                }
            }

            if let Some(accs_raw) = self.db.get_cf(touched_cf, h_bytes)? {
                if let Ok(accs) = <Vec<[u8; 32]>>::try_from_slice(&accs_raw) {
                    let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
                    let history_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");
                    let jmt = jmt::JellyfishMerkleTree::<'_, Self, blake3::Hasher>::new(self);

                    for acc_addr in accs {
                        let key_hash = KeyHash::with::<blake3::Hasher>(&acc_addr);

                        let mut versioned_key = [0u8; 40];
                        versioned_key[0..32].copy_from_slice(&key_hash.0);
                        versioned_key[32..40].copy_from_slice(&h.to_be_bytes()); 
                        batch.delete_cf(history_cf, &versioned_key);

                        match jmt.get(key_hash, target_height) {
                            Ok(Some(val)) => {
                                batch.put_cf(acc_cf, acc_addr, &val);
                                batch.put_cf(acc_cf, key_hash.0, &val);
                                if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                                    batch.put_cf(cf_kh_to_addr, key_hash.0, acc_addr);
                                }
                            },
                            _ => {
                                batch.delete_cf(acc_cf, acc_addr);
                                batch.delete_cf(acc_cf, key_hash.0);
                                if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                                    batch.delete_cf(cf_kh_to_addr, key_hash.0);
                                }
                            }
                        }
                    }
                }
            }
            
            batch.delete_cf(txs_cf, h_bytes);
            batch.delete_cf(touched_cf, h_bytes);

            if let Some(headers_cf) = self.db.cf_handle(CF_HEADERS) {
                if let Some(h_hash) = self.db.get_cf(self.db.cf_handle(CF_BLOCKS).unwrap(), h_bytes).unwrap_or(None) {
                    batch.delete_cf(headers_cf, h_hash);
                }
            }
            if let Some(blocks_cf) = self.db.cf_handle(CF_BLOCKS) {
                batch.delete_cf(blocks_cf, h_bytes);
            }
            if let Some(bodies_cf) = self.db.cf_handle(CF_BLOCK_BODIES) {
                batch.delete_cf(bodies_cf, h_bytes);
            }
            
            batch.delete_cf(coinbase_cf, h_bytes);
        }

        batch.put_cf(meta_cf, b"jmt_v", target_height.to_le_bytes());
        
        let reverted_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(target_height);
        batch.put_cf(meta_cf, b"actual_supply_v2", reverted_supply.to_le_bytes());
        batch.put_cf(meta_cf, b"expected_total_supply", reverted_supply.to_le_bytes());

        // Hạ mốc chốt (Finalized Height) xuống target để node có thể đồng bộ lại
        batch.put_cf(meta_cf, b"finalized_h", target_height.to_le_bytes());

        let mut opts = WriteOptions::default();
        opts.set_sync(true);
        self.db.write_opt(batch, &opts).unwrap();
        self.current_version.store(target_height, Ordering::SeqCst);
        self.actual_total_supply.store(reverted_supply, Ordering::SeqCst);

        log::info!("[INVISIBLE-HAND] ✅ Đã xóa vật lý {} khối. Node hiện ở #{}", current_height - target_height, target_height);
        log::info!("[INVISIBLE-HAND] ⚖️ Tổng cung đã căn chỉnh: {} VNT", reverted_supply);

        // Xóa sạch cache gần nhất để tránh zombie block hashes
        if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
            cache.clear();
        }
        Ok(())
    }

    /// [S#1 FIX] Hoàn tác duy nhất phiên bản JMT (JMT Version). 
    /// Dùng cho Ghost Execution (tạo ZK-Witness) mà không làm hỏng dữ liệu Blocks/Receipts.
    pub fn rollback_jmt_version(&self, target_version: u64) -> Result<()> {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        self.db.put_cf(cf, b"jmt_v", target_version.to_le_bytes())?;
        self.current_version.store(target_version, Ordering::SeqCst);
        log::debug!("[ROLLBACK-JMT] 🔄 Đã hoàn tác JMT Version về v{}", target_version);
        Ok(())
    }

    pub fn put_block_transactions_to_batch(&self, batch: &mut rocksdb::WriteBatch, height: u64, tx_hashes: Vec<[u8; 32]>, touched_accs: Vec<[u8; 32]>) {
        let cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");
        let touched_cf = self.db.cf_handle(CF_TOUCHED_ACCS).expect("Missing TOUCHED CF");
        let coinbase_cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");
        
        let tx_data = borsh::to_vec(&tx_hashes).expect("Failed to serialize tx hashes");
        batch.put_cf(cf, height.to_le_bytes(), &tx_data);
        
        let touched_data = borsh::to_vec(&touched_accs).expect("Failed to serialize touched accounts");
        batch.put_cf(touched_cf, height.to_le_bytes(), &touched_data);

        // Lưu coinbase tách biệt để phục vụ audit nhanh (Dùng Height làm Key)
        if !tx_hashes.is_empty() {
             let coinbase_tx = tx_hashes[0];
             batch.put_cf(coinbase_cf, height.to_le_bytes(), &coinbase_tx);
        }
    }

    pub fn put_block_transactions(&self, height: u64, tx_hashes: Vec<[u8; 32]>, touched_accs: Vec<[u8; 32]>) {
        let cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");
        let touched_cf = self.db.cf_handle(CF_TOUCHED_ACCS).expect("Missing TOUCHED CF");
        let coinbase_cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");
        
        let tx_data = borsh::to_vec(&tx_hashes).expect("Failed to serialize tx hashes");
        let acc_data = borsh::to_vec(&touched_accs).expect("Failed to serialize touched accs");
        
        log::info!("[SCL-DB] 💾 Lưu {} TxHashes cho khối #{}", tx_hashes.len(), height);
        self.db.put_cf(cf, height.to_le_bytes(), &tx_data).expect("Failed to write block txs");
        self.db.put_cf(touched_cf, height.to_le_bytes(), &acc_data).expect("Failed to write touched accs");

        // [V37.9.13] Lưu riêng Coinbase (Giao dịch đầu tiên) để bảo tồn vĩnh viễn
        if let Some(coinbase_hash) = tx_hashes.first() {
            self.db.put_cf(coinbase_cf, height.to_le_bytes(), coinbase_hash).expect("Failed to write coinbase hash");
        }
    }

    pub fn get_block_transactions(&self, height: u64) -> Vec<[u8; 32]> {
        let cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");
        match self.db.get_cf(cf, height.to_le_bytes()).unwrap_or(None) {
            Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
            None => Vec::new(),
        }
    }

    pub fn validate_block_finality(&self, height: u64, hash: &[u8; 32]) -> Result<()> {
        let finalized_h = self.get_finalized_height();
        
        // [VANGUARD-HOTFIX] Đặc xá cho Khối Genesis (Khối 0) khi Sổ cái hoàn toàn trống.
        // Tránh việc mặc định finalized_h = 0 khóa nhầm khối 0 chưa được sinh ra.
        if height == 0 && self.get_block_hash(0).is_none() {
            return Ok(());
        }

        if height <= finalized_h {
            match self.get_block_hash(height) {
                Some(stored_hash) => {
                    if &stored_hash != hash {
                        // [V40.3] Kích hoạt trừng phạt đồng bộ và gắn nhãn TOXIC
                        crate::genz_pow::SYNC_VIOLATION_COUNT.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                        return Err(anyhow::anyhow!("[TOXIC_BRANCH] ERR_IMMUTABLE_FIREWALL_VIOLATION: Khối #{} cố gắng thay đổi lịch sử đã finalize!", height));
                    }
                },
                None => {
                    // [Audit #5 FIX] Tuyệt đối không để lọt khối rỗng vào vùng Finality
                    return Err(anyhow::anyhow!("ERR_FINALITY_DATA_MISSING: Khối #{} thuộc vùng bất biến nhưng thiếu dữ liệu cục bộ!", height));
                }
            }
        }
        Ok(())
    }



    pub fn put_block_hash(&self, height: u64, hash: &[u8; 32]) {
        let cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        
        // [VANGUARD-ANTI-OVERWRITE] KIỂM TRA TRƯỚC KHI GHI
        // Không bao giờ cho phép ghi đè mã băm của một khối đã tồn tại,
        // TRỪ KHI hệ thống đang thực hiện lệnh Rollback (Reorg hợp lệ).
        if let Some(existing_hash_bytes) = self.db.get_cf(cf, height.to_le_bytes()).unwrap_or(None) {
            if existing_hash_bytes.len() == 32 {
                let mut existing_hash = [0u8; 32];
                existing_hash.copy_from_slice(&existing_hash_bytes);
                
                if &existing_hash != hash {
                    // Nếu Hash mới khác Hash cũ, CẤM GHI ĐÈ Trực tiếp.
                    // Reorg (ProcessChain) sẽ tự xử lý việc xóa (Delete) hash cũ trước khi ghi hash mới.
                    log::warn!(
                        "[DATABASE-GUARD] 🛡️ Chặn đứng nỗ lực GHI ĐÈ TRÁI PHÉP tại khối #{}. (Cũ: {}, Mới: {})", 
                        height, hex::encode(existing_hash), hex::encode(hash)
                    );
                    return; // TỪ CHỐI GHI ĐÈ!
                }
            }
        }

        // [V10.2 LOCKDOWN] Phát hiện dấu vết nhiễm độc định dạng cũ (28+4)
        let tail = u32::from_le_bytes([hash[28], hash[29], hash[30], hash[31]]);
        if tail > 0 && tail < 1000 && height > 1000 {
             log::error!("[DATABASE-CRITICAL] 🛑 Phát hiện nỗ lực ghi đè Hash nhiễm độc (Đuôi {} tại Height {})", tail, height);
        }

        self.db.put_cf(cf, height.to_le_bytes(), hash).expect("Failed to write block hash");
        log::info!("[SCL-DB-V10.3] ✅ Đã chốt Canonical Hash cho Khối #{}: {}", height, hex::encode(hash));

        // Cập nhật cache RAM
        if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
            cache.insert(height, *hash);
        }
    }

    pub fn put_block_hash_to_batch(&self, batch: &mut rocksdb::WriteBatch, height: u64, hash: &[u8; 32]) {
        let cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        
        // Logic LOCKDOWN cho batch (Cảnh báo thôi, không chặn batch)
        let tail = u32::from_le_bytes([hash[28], hash[29], hash[30], hash[31]]);
        if tail > 0 && tail < 1000 && height > 1000 {
             log::error!("[DATABASE-CRITICAL] 🛑 [BATCH] Phát hiện Hash nhiễm độc tại Height {}", height);
        }

        batch.put_cf(cf, height.to_le_bytes(), hash);
    }


    /// [AUDIT V10.1] Tường lửa Khối Ma (Ghost Block Firewall) - Phiên bản Cách ly
    /// Xóa sạch mọi mã băm có chiều cao lớn hơn hoặc bằng phiên bản SMT hiện tại 
    /// để tránh hiện tượng "bóng ma lịch sử" khi Node restart hoặc rẽ nhánh.
    pub fn cleanup_future_hashes(&self, current_height: u64) {
        let cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        let mut deleted_count = 0;
        let mut kept_count = 0;
        
        // [V10.2 VANGUARD-VALIDATOR] Lấy mã băm của đỉnh hiện tại (JMT Version) để làm điểm tựa
        let mut last_valid_hash = self.get_block_hash(current_height);

        for h in (current_height + 1)..(current_height + 1000) {
            let key = h.to_le_bytes();
            if let Some(hash_bytes) = self.db.get_cf(cf, key).unwrap_or(None) {
                let mut is_legit = false;
                
                // Kiểm tra xem khối này có nối tiếp được với khối trước đó không
                if let Some(parent_h) = last_valid_hash {
                    if let Some(block_raw) = self.get_block_raw_by_height(h) {
                         use crate::proto::block::Block;
                         if let Ok(block) = Block::decode(&block_raw[..]) {
                             if let Some(header) = block.header {
                                 if let Some(p_hash) = header.parent_hash {
                                     if p_hash.value == parent_h.to_vec() {
                                         is_legit = true;
                                     }
                                 }
                             }
                         }
                    }
                }

                if is_legit {
                    // Khối hợp lệ, nối tiếp được chuỗi! Giữ lại để hệ thống tái thực thi trạng thái (Re-play)
                    kept_count += 1;
                    let mut h_arr = [0u8; 32];
                    h_arr.copy_from_slice(&hash_bytes);
                    last_valid_hash = Some(h_arr);
                    log::info!("[VALIDATOR-V10.2] ✅ Khối #{} hợp lệ (Parent khớp). Giữ lại để khôi phục trạng thái.", h);
                } else {
                    // Khối không khớp cha, thực sự là "Bóng ma lịch sử" từ một nhánh rác. Trảm!
                    log::warn!("[FIREWALL-V10.2] 🧹 Đang dọn dẹp khối 'ma' thực sự #{} (Mất liên kết).", h);
                    self.db.delete_cf(cf, key).ok();
                    deleted_count += 1;
                    last_valid_hash = None; // Ngắt chuỗi kiểm tra từ đây
                }
            } else {
                break; // Hết dữ liệu tương lai
            }
        }
        
        if kept_count > 0 || deleted_count > 0 {
            log::info!("[VANGUARD-V10.2] 🛡️ Kết quả thanh tra: Khôi phục {} khối, Xóa {} khối ma thực sự.", kept_count, deleted_count);
        }
    }

    fn get_block_hash_db(&self, height: u64) -> Option<[u8; 32]> {
        let cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        match self.db.get_cf(cf, height.to_le_bytes()).unwrap_or(None) {
            Some(data) => {
                let mut hash = [0u8; 32];
                hash.copy_from_slice(&data);
                Some(hash)
            },
            None => None,
        }
    }

    pub fn get_block_hash(&self, height: u64) -> Option<[u8; 32]> {
        // 1. Kiểm tra cache RAM
        if let Ok(cache) = RECENT_HASHES_CACHE.read() {
            if let Some(hash) = cache.get(&height) {
                return Some(*hash);
            }
        }
        
        // 2. Fallback truy vấn DB
        let hash = self.get_block_hash_db(height)?;
        
        // 3. Cập nhật cache RAM và giữ tối đa 150 blocks gần nhất
        if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
            cache.insert(height, hash);
            let current = self.get_current_version();
            let limit = current.saturating_sub(150);
            cache.retain(|&k, _| k >= limit);
        }
        
        Some(hash)
    }

    /// [VANGUARD-V2.1] Tính toán Median Time Past (MTP-11) của 11 khối gần nhất
    /// Giúp chống lại Tấn công Bóp méo Thời gian (Time-Warp Attack).
    pub fn get_median_time_past(&self, current_height: u64) -> u64 {
        if current_height == 0 { return 0; }
        
        let mut timestamps = Vec::new();
        let start = current_height.saturating_sub(11);
        for h in start..current_height {
            if let Some(hash) = self.get_block_hash(h) {
                if let Some(header_raw) = self.get_header_raw(&hash) {
                    if let Ok(header) = crate::proto::block::BlockHeader::decode(&header_raw[..]) {
                        timestamps.push(header.timestamp);
                    }
                }
            }
        }
        
        if timestamps.is_empty() { return 0; }
        timestamps.sort();
        timestamps[timestamps.len() / 2]
    }

    pub fn put_header(&self, hash: &[u8; 32], header_raw: Vec<u8>) {
        let cf = self.db.cf_handle(CF_HEADERS).expect("Missing HEADERS CF");
        self.db.put_cf(cf, hash, header_raw).expect("Failed to write header");
    }

    pub fn put_header_to_batch(&self, batch: &mut rocksdb::WriteBatch, hash: &[u8; 32], header_raw: Vec<u8>, weight: Vec<u8>) {
        let cf = self.db.cf_handle(CF_HEADERS).expect("Missing HEADERS CF");
        batch.put_cf(cf, hash, header_raw);
        
        // Lưu trọng số vào cf_meta hoặc một CF chuyên biệt (ở đây Go mong đợi trong HEADERS với suffix hoặc meta)
        // [VANGUARD-FIX] Lưu trọng số vào CF_META với key là hash_weight
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        let mut weight_key = Vec::with_capacity(40);
        weight_key.extend_from_slice(b"weight_");
        weight_key.extend_from_slice(hash);
        batch.put_cf(meta_cf, &weight_key, weight);
    }



    pub fn get_header_raw(&self, hash: &[u8; 32]) -> Option<Vec<u8>> {
        let cf = self.db.cf_handle(CF_HEADERS).expect("Missing HEADERS CF");
        self.db.get_cf(cf, hash).unwrap_or(None)
    }

    pub fn get_account_state(&self, address: &[u8; 32]) -> AccountState {
        // [V1.2 FINAL FIX] Kiểm tra CF_ACC (RocksDB Cache) trước để lấy trạng thái mới nhất trong khối
        let cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let mut state = if let Some(data) = self.db.get_cf(cf, address).unwrap_or(None) {
            if let Ok(state) = AccountState::try_from_slice(&data) {
                state
            } else {
                AccountState::default()
            }
        } else {
            // [VANGUARD-IO-OPTIMIZATION] 
            // Nếu không có trong Sổ cái phẳng (CF_ACC), tài khoản 100% là mới tinh.
            // BỎ QUA việc tra cứu cây JMT (Tránh hàng ngàn Reverse Iterators trên RocksDB gây treo đĩa).
            AccountState::default()
        };

        // [VANGUARD-ZEN-PAYOUT-LAZY] Tự động giải ngân động các phần thưởng đã chín
        let current_height = self.get_current_version();
        let mut payout_total: u64 = 0;
        state.maturing_rewards.retain(|reward| {
            if current_height >= reward.height {
                payout_total += reward.amount;
                false
            } else {
                true
            }
        });
        if payout_total > 0 {
            state.btc_z = state.btc_z.saturating_add(payout_total);
        }
        state
    }

    pub fn get_account_state_at_height(&self, address: &[u8; 32], height: u64) -> Option<AccountState> {
        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        let key_hash = KeyHash::with::<blake3::Hasher>(*address);
        match jmt.get(key_hash, height) {
            Ok(Some(val)) => Some(AccountState::try_from_slice(&val).unwrap_or_default()),
            _ => None,
        }
    }


    pub fn update_account_at_height(&self, address: &[u8; 32], state: AccountState, height: u64) {
        let mut batch = WriteBatch::default();
        let cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let history_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");
        let data = borsh::to_vec(&state).expect("Failed to serialize account state");
        
        // [INDEX 1] Raw Address (Latest)
        batch.put_cf(cf, address, &data);
        
        let key_hash = KeyHash::with::<blake3::Hasher>(address);

        // [INDEX 3] Versioned History (KeyHash + Version BE)
        // [VANGUARD-V1.2] Cấu trúc tối ưu: [32-byte KeyHash][8-byte Height BE]
        let mut versioned_key = [0u8; 40];
        versioned_key[0..32].copy_from_slice(&key_hash.0);
        versioned_key[32..40].copy_from_slice(&height.to_be_bytes());
        batch.put_cf(history_cf, &versioned_key, &data);

        // [BIG-DATA-INDEX] Ghi KeyHash -> Address index để tìm ngược cực nhanh
        if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
            batch.put_cf(cf_kh_to_addr, key_hash.0, address);
        }

        self.db.write(batch).expect("Failed to update account with history");
    }

    pub fn update_account_at_height_to_batch(&self, batch: &mut rocksdb::WriteBatch, address: &[u8; 32], state: AccountState, height: u64) {
        let cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let history_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");
        let data = borsh::to_vec(&state).expect("Failed to serialize account state");
        
        batch.put_cf(cf, address, &data);
        let key_hash = KeyHash::with::<blake3::Hasher>(address);

        let mut versioned_key = [0u8; 40];
        versioned_key[0..32].copy_from_slice(&key_hash.0);
        versioned_key[32..40].copy_from_slice(&height.to_be_bytes());
        batch.put_cf(history_cf, &versioned_key, &data);

        // [BIG-DATA-INDEX] Ghi KeyHash -> Address index
        if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
            batch.put_cf(cf_kh_to_addr, key_hash.0, address);
        }
    }


    pub fn update_account(&self, address: &[u8; 32], state: AccountState) {
        let h = self.get_current_version(); 
        self.update_account_at_height(address, state, h);
    }

    /// [VANGUARD-ATOMIC] Chuẩn bị WriteBatch cho SMT và Account State mà KHÔNG thực thi ghi đĩa ngay.
    /// Giúp gom nhóm với dữ liệu Block để đảm bảo tính nguyên tử (Atomicity).
    pub fn prepare_smt_write_batch(&self, batch_kv: &Vec<([u8; 32], Vec<u8>)>, height: u64) -> (rocksdb::WriteBatch, [u8; 32]) {
        let mut rocks_batch = rocksdb::WriteBatch::default();
        if batch_kv.is_empty() { 
            return (rocks_batch, self.get_state_root()); 
        }
        
        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        let mut value_set = Vec::new();
        for (addr, data) in batch_kv {
            value_set.push((KeyHash::with::<blake3::Hasher>(addr), Some(data.clone())));
        }

        // 1. Tính toán rễ mới và các Node JMT mới
        let (new_root, batch) = jmt.put_value_set(value_set, height).expect("JMT batch update failed");
        
        // 2. Ghi dữ liệu tài khoản (Latest & History)
        let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let history_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");
        
        for (addr, data) in batch_kv {
            // [LATEST]
            rocks_batch.put_cf(acc_cf, addr, data);
            let key_hash = KeyHash::with::<blake3::Hasher>(addr);

            // [HISTORY] - KeyHash + Version BE
            let mut versioned_key = [0u8; 40];
            versioned_key[0..32].copy_from_slice(&key_hash.0);
            versioned_key[32..40].copy_from_slice(&height.to_be_bytes());
            rocks_batch.put_cf(history_cf, &versioned_key, data);


            // [BIG-DATA-INDEX] Ghi KeyHash -> Address index
            if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                rocks_batch.put_cf(cf_kh_to_addr, key_hash.0, addr);
            }
        }
        
        // 3. Ghi các nút JMT mới
        let node_batch = batch.node_batch;
        let jmt_cf = self.db.cf_handle(CF_JMT).expect("Missing JMT CF");
        for (node_key, node) in node_batch.nodes() {
            let key = bincode::serialize(node_key).expect("Serialize node key failed");
            let value = bincode::serialize(node).expect("Serialize node failed");
            rocks_batch.put_cf(jmt_cf, key, value);
        }
        
        // 4. Chốt chặn Metadata JMT Version
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        rocks_batch.put_cf(meta_cf, b"jmt_v", height.to_le_bytes());

        (rocks_batch, new_root.0)
    }

    pub fn consolidate_smt_batch(&self, batch_kv: Vec<([u8; 32], Vec<u8>)>, height: u64) -> [u8; 32] {
        let (batch, root) = self.prepare_smt_write_batch(&batch_kv, height);
        
        // Chốt chặn nguyên tử
        self.db.write(batch).expect("Consolidate batch failed");
        
        // Cập nhật cao độ trong RAM sau khi ghi đĩa thành công
        self.current_version.store(height, Ordering::SeqCst);
        root
    }

    /// [VANGUARD-GHOST] Thực thi chốt chặn SMT tạm thời cho Ghost Execution.
    /// Tốc độ cực cao (No Sync) và không làm thay đổi trạng thái RAM.
    pub fn consolidate_smt_batch_ghost(&self, batch_kv: Vec<([u8; 32], Vec<u8>)>, height: u64) -> [u8; 32] {
        if batch_kv.is_empty() { return self.get_state_root(); }
        
        let (batch, root) = self.prepare_smt_write_batch(&batch_kv, height);
        
        // Ghi dữ liệu xuống đĩa nhưng tắt Sync để tối ưu tốc độ cho Ghost Execution
        let mut opts = rocksdb::WriteOptions::default();
        opts.set_sync(false); 
        self.db.write_opt(batch, &opts).expect("Ghost consolidate failed");
        
        // LƯU Ý QUAN TRỌNG: KHÔNG cập nhật self.current_version ở đây!
        // Để tránh làm sai lệch trạng thái của Node chính đang vận hành.
        
        root
    }

    /// [VANGUARD-NUCLEAR-SHIELD] CHỐT SỔ NGUYÊN TỬ TUYỆT ĐỐI
    /// Đảm bảo Block Data, State JMT, Metadata và Cao độ hệ thống được ghi trong DUY NHẤT 1 Transaction.
    /// Triệt tiêu hoàn toàn hiện tượng "Khối Ma" (Ghost Block) khi xảy ra sự cố sập nguồn.
    pub fn commit_block_atomic(
        &self,
        height: u64,
        hash: &[u8; 32],
        header_raw: Vec<u8>,
        body_raw: Vec<u8>,
        tx_hashes: Vec<[u8; 32]>,
        touched_accs: Vec<[u8; 32]>,
        state_batch: rocksdb::WriteBatch,
        actual_supply: u64,
        expected_supply: u64,
        weight: Vec<u8>,
    ) {
        let mut final_batch = state_batch; 
        
        // 1. Ghi dữ liệu Ledger cốt lõi
        self.put_header_to_batch(&mut final_batch, hash, header_raw, weight);
        self.put_block_body_to_batch(&mut final_batch, hash, body_raw);
        self.put_block_hash_to_batch(&mut final_batch, height, hash);

        
        // 2. Ghi siêu dữ liệu giao dịch và chỉ mục
        self.put_block_transactions_to_batch(&mut final_batch, height, tx_hashes, touched_accs);
        
        // 3. Ghi siêu dữ liệu kinh tế (Supply Audit)
        self.set_actual_total_supply_to_batch(&mut final_batch, actual_supply);
        self.set_expected_supply_to_batch(&mut final_batch, expected_supply);
        
        // 4. Tự động hóa chốt chặn Finality (Khóa 5 khối phía sau)
        let cf_meta = self.db.cf_handle(CF_META).expect("Missing META CF");
        let finalized_h = height.saturating_sub(5);
        final_batch.put_cf(cf_meta, b"finalized_h", finalized_h.to_le_bytes());

        // --- CHỐT SỔ ---
        let mut opts = rocksdb::WriteOptions::default();
        opts.set_sync(true); // Ép buộc ghi xuống đĩa vật lý (Fsync) để đảm bảo an toàn tuyệt đối
        self.db.write_opt(final_batch, &opts).expect("Atomic block commit failed! Nuclear meltdown imminent.");
        
        // Cập nhật cache RAM cho block vừa commit
        if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
            cache.insert(height, *hash);
        }

        // 5. Cập nhật trạng thái RAM sau khi đĩa đã an toàn
        self.current_version.store(height, Ordering::SeqCst);
        self.actual_total_supply.store(actual_supply, Ordering::SeqCst);
        
        log::info!("✅ [NUCLEAR-SHIELD] Khối #{} đã được chốt sổ nguyên tử. Hash: {}", height, hex::encode(hash));
    }



    /// [SIMULATION V1.0] Mô phỏng cập nhật SMT để lấy StateRoot mới mà KHÔNG ghi đĩa.
    /// Giúp thợ đào chuẩn bị Header mà không gây lạm phát hoặc xung đột dữ liệu.
    pub fn consolidate_smt_simulation(&self, batch_kv: Vec<([u8; 32], Vec<u8>)>, height: u64) -> [u8; 32] {
        if batch_kv.is_empty() { return self.get_state_root(); }
        
        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        
        let mut value_set = Vec::new();
        for (addr, data) in batch_kv {
            value_set.push((KeyHash::with::<blake3::Hasher>(addr), Some(data)));
        }

        // Chú ý: JMT.put_value_set tính toán các Node mới trong bộ nhớ và trả về batch.
        // Ta chỉ lấy new_root và BỎ QUA việc gửi self.write_node_batch().
        let (new_root, _) = jmt.put_value_set(value_set, height).expect("JMT simulation failed");
        
        new_root.0
     }
 
     pub fn reset_state_completely(&self) -> Result<()> {
         log::warn!("[SCL-RESET] ⚠️ Bắt đầu xóa sạch trạng thái JMT và tài khoản...");
         
         let cfs_to_clear = [
             CF_JMT,
             CF_ACC,
             CF_ACC_HISTORY,
             CF_SMT_NODES,
             CF_TOUCHED_ACCS,
             CF_RECEIPTS,
             CF_TX_INDEX,
             CF_ACC_SYNC_STAGING,
             CF_REORG_BACKUP,
             CF_COINBASE,
         ];
 
         for cf_name in cfs_to_clear {
             if let Some(cf) = self.db.cf_handle(cf_name) {
                 let mut batch = WriteBatch::default();
                 let iter = self.db.iterator_cf(cf, rocksdb::IteratorMode::Start);
                 let mut count = 0;
                 for item in iter {
                     if let Ok((key, _)) = item {
                         batch.delete_cf(cf, key);
                         count += 1;
                     }
                 }
                 let mut opts = WriteOptions::default();
                 opts.set_sync(true);
                 self.db.write_opt(batch, &opts)?;
                 log::info!("[SCL-RESET] ✅ Đã xóa {} bản ghi trong Column Family {}", count, cf_name);
             }
         }
 
         // Reset các key trong CF_META
         if let Some(meta_cf) = self.db.cf_handle(CF_META) {
             let keys_to_delete = [
                 b"jmt_v".as_slice(),
                 b"actual_supply_v2".as_slice(),
                 b"expected_supply".as_slice(),
                 b"finalized_h".as_slice(),
                 b"lowest_full_height".as_slice(),
             ];
             let mut batch = WriteBatch::default();
             for key in keys_to_delete {
                 batch.delete_cf(meta_cf, key);
             }
             let mut opts = WriteOptions::default();
             opts.set_sync(true);
             self.db.write_opt(batch, &opts)?;
             log::info!("[SCL-RESET] ✅ Đã xóa các key trạng thái trong CF_META");
         }
 
         // Reset các AtomicU64 trong bộ nhớ
         self.current_version.store(0, Ordering::SeqCst);
         self.actual_total_supply.store(0, Ordering::SeqCst);
         if let Ok(mut cache) = RECENT_HASHES_CACHE.write() {
             cache.clear();
         }
 
         log::warn!("[SCL-RESET] 🚀 Đã reset hoàn toàn trạng thái về Genesis!");
         Ok(())
     }
 
     pub fn get_highest_block_height(&self) -> u64 {
         let cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
         let mut height: u64 = 0;
         loop {
             let h_bytes = height.to_le_bytes();
             match self.db.get_cf(cf, h_bytes).unwrap_or(None) {
                 Some(_) => {
                     height += 1;
                 },
                 None => {
                     break;
                 }
             }
         }
         height.saturating_sub(1)
     }
 
     pub fn get_current_version(&self) -> u64 {
         self.current_version.load(Ordering::SeqCst)
     }

    pub fn get_state_root(&self) -> [u8; 32] {
        let version = self.get_current_version();
        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        match jmt.get_root_hash(version) {
            Ok(root) => root.0,
            Err(_) => {
                // Nếu không tìm thấy root tại version hiện tại (có thể là Genesis hoặc lỗi DB)
                [0u8; 32]
            },
        }
    }

    /// [VANGUARD-SYNC] Tính toán State Root bằng cách quét toàn bộ tài khoản (Chuẩn hóa 100%)
    pub fn get_state_root_clean(&self) -> [u8; 32] {
        let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let cf_kh_to_addr = self.db.cf_handle(CF_KEYHASH_TO_ADDR).expect("Missing KEYHASH_TO_ADDR CF");
        let iter = self.db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
        
        let mut seen = std::collections::HashSet::new();
        let mut batch_kv = Vec::new();
        let mut version: jmt::Version = 0;
        let mut last_root = [0u8; 32];
        
        // [VANGUARD-SYNC-OPTIMIZATION] Duyệt RocksDB iterator và cập nhật SMT lũy tiến theo từng lô 50,000 phần tử 
        // để tránh nạp toàn bộ hàng triệu tài khoản vào bộ nhớ RAM gây tràn bộ nhớ (OOM). 
        // Tại sao thiết kế như vậy: Việc cập nhật lũy tiến (incremental updates) cho ra kết quả mã băm root cuối cùng 
        // hoàn toàn trùng khớp với việc nạp cả lô dữ liệu khổng lồ do Jellyfish Merkle Tree là cấu trúc bản đồ không có thứ tự.
        for item in iter {
            if let Ok((key, val)) = item {
                if key.len() == 32 {
                    // Để tương thích ngược và tương thích xuôi 100%:
                    // 1. Nếu key là KeyHash (tra cứu ngược trong CF_KEYHASH_TO_ADDR trả về Some) -> Bỏ qua.
                    // 2. Nếu key là Address thực tế -> Nhận!
                    // Chúng ta kiểm tra key_hash tương ứng có tồn tại trong CF_KEYHASH_TO_ADDR hoặc CF_ACC (với DB cũ) hay không.
                    let kh = KeyHash::with::<blake3::Hasher>(&key).0;
                    
                    let is_addr = if self.db.get_cf(cf_kh_to_addr, key.clone()).unwrap_or(None).is_some() {
                        false // key thực chất là KeyHash của một địa chỉ nào đó
                    } else {
                        self.db.get_cf(cf_kh_to_addr, kh).unwrap_or(None).is_some()
                            || self.db.get_cf(acc_cf, kh).unwrap_or(None).is_some()
                    };

                    if is_addr {
                        let mut addr = [0u8; 32];
                        addr.copy_from_slice(&key);
                        if seen.insert(addr) {
                            batch_kv.push((addr, val.to_vec()));
                            
                            if batch_kv.len() >= 50000 {
                                println!("[VANGUARD-SYNC] Đang xử lý lô SMT sạch, phiên bản JMT: {}, số lượng: {}", version, batch_kv.len());
                                last_root = self.consolidate_smt_batch_custom(batch_kv, version, CF_SMT_NODES);
                                batch_kv = Vec::new();
                                version += 1;
                            }
                        }
                    }
                }
            }
        }
        
        // Xử lý nốt lô cuối cùng nếu còn dữ liệu
        if !batch_kv.is_empty() || version == 0 {
            println!("[VANGUARD-SYNC] Đang xử lý lô SMT sạch cuối cùng, phiên bản JMT: {}, số lượng: {}", version, batch_kv.len());
            last_root = self.consolidate_smt_batch_custom(batch_kv, version, CF_SMT_NODES);
        }
        
        last_root
    }

    // --- Metadata & Receipts ---

    pub fn get_finalized_height(&self) -> u64 {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        match self.db.get_cf(cf, b"finalized_h").unwrap_or(None) {
            Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
            None => 0,
        }
    }

    pub fn set_finalized_height(&self, height: u64) {
        let current_fin = self.get_finalized_height();
        let current_ver = self.get_current_version();
        
        // [VANGUARD-CONSTITUTION] Hiến pháp chốt hạ: 
        // 1. Chỉ được phép tiến lên (không được kéo lùi lịch sử).
        // 2. PHẢI CÓ ÍT NHẤT 5 KHỐI BẢO VỆ (Confirmations) đè lên trên (height + 5 <= current_ver).
        if height > current_fin && (height + 5) <= current_ver {
            let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
            self.db.put_cf(cf, b"finalized_h", height.to_le_bytes()).expect("Failed to update finalized height");
            log::info!("[FINALITY-COMMIT] 🔒 Đã chốt hạ khối #{} (Xác nhận bởi #{} khối tiếp theo)", height, current_ver - height);
        } else {
            // log::debug!("[FINALITY-GUARD] Chặn nỗ lực chốt khối #{} chưa đủ điều kiện (Current: #{})", height, current_ver);
        }
    }

    /// [HEALING] Cưỡng chế đặt mốc chốt (Chỉ dùng cho quy trình phục hồi dữ liệu khi khởi động)
    /// @note Vẫn tuân thủ chốt chặn an ninh: Không được vượt quá thực tế sổ cái (Anti-Ghost Finalization)
    pub fn force_set_finalized_height(&self, height: u64) {
        let current_ver = self.get_current_version();
        if height <= current_ver {
            let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
            self.db.put_cf(cf, b"finalized_h", height.to_le_bytes()).expect("Failed to force update finalized height");
            log::info!("[HEALING-VANGUARD] ✅ Đã cưỡng chế căn chỉnh mốc chốt về #{}", height);
        } else {
            log::error!("[FINALITY-GUARD] 🚨 Từ chối tạo chốt ảo #{} vượt quá thực tế sổ cái #{}", height, current_ver);
        }
    }

    pub fn put_transaction_receipt_v2(&self, tx: TrackedTx) {
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let index_cf = self.db.cf_handle(CF_TX_INDEX).expect("Missing TX_INDEX CF");

        // 1. Lưu chi tiết giao dịch (Dùng TrackedTx chuẩn)
        let tx_data = borsh::to_vec(&tx).expect("Failed to serialize TrackedTx");
        self.db.put_cf(receipt_cf, &tx.tx_id, &tx_data).expect("Failed to write receipt");

        // 2. Lập chỉ mục cho người gửi (nếu không phải coinbase)
        if tx.sender != [0u8; 32] {
            self.add_tx_to_index(index_cf, &tx.sender, &tx.tx_id);
        }

        // 3. Lập chỉ mục cho người nhận
        self.add_tx_to_index(index_cf, &tx.receiver, &tx.tx_id);
    }

    fn add_tx_to_index(&self, cf: &rocksdb::ColumnFamily, addr: &[u8; 32], tx_id: &[u8; 32]) {
        let mut txs = match self.db.get_cf(cf, addr).unwrap_or(None) {
            Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
            None => Vec::new(),
        };

        // Tránh trùng lặp (nếu re-index)
        if !txs.contains(tx_id) {
            txs.push(*tx_id);
            // Giới hạn lịch sử 10,000 giao dịch mỗi địa chỉ để tránh phình DB quá mức
            if txs.len() > 10000 {
                txs.remove(0);
            }
            let data = borsh::to_vec(&txs).unwrap();
            self.db.put_cf(cf, addr, &data).expect("Failed to update address index");
        }
    }

    pub fn add_tx_to_index_to_batch(&self, batch: &mut rocksdb::WriteBatch, cf: &rocksdb::ColumnFamily, addr: &[u8; 32], tx_id: &[u8; 32]) {
        let mut txs = match self.db.get_cf(cf, addr).unwrap_or(None) {
            Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
            None => Vec::new(),
        };

        if !txs.contains(tx_id) {
            txs.push(*tx_id);
            if txs.len() > 10000 {
                txs.remove(0);
            }
            let data = borsh::to_vec(&txs).unwrap();
            batch.put_cf(cf, addr, &data);
        }
    }

    pub fn get_transactions_by_address(&self, addr: &[u8; 32]) -> Vec<TrackedTx> {
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let index_cf = self.db.cf_handle(CF_TX_INDEX).expect("Missing TX_INDEX CF");

        let tx_ids = match self.db.get_cf(index_cf, addr).unwrap_or(None) {
            Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
            None => Vec::new(),
        };

        let mut results = Vec::new();
        for tx_id in tx_ids {
            if let Some(data) = self.db.get_cf(receipt_cf, &tx_id).unwrap_or(None) {
                if let Ok(tx) = TrackedTx::try_from_slice(&data) {
                    results.push(tx);
                }
            }
        }
        // Trả về theo thứ tự mới nhất lên đầu
        results.reverse();
        results
    }

    /// [VANGUARD-RECOVERY] Khôi phục toàn bộ lịch sử thợ đào bằng cách quét Sổ cái (Issuance-based)
    pub fn reindex_miner_history(&self, addr: &[u8; 32]) {
        let blocks_cf = self.db.cf_handle(CF_BLOCKS).expect("Missing BLOCKS CF");
        let header_cf = self.db.cf_handle(CF_HEADERS).expect("Missing HEADERS CF");
        let body_cf = self.db.cf_handle(CF_BLOCK_BODIES).expect("Missing BLOCK_BODIES CF");
        let index_cf = self.db.cf_handle(CF_TX_INDEX).expect("Missing TX_INDEX CF");
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let coinbase_cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");
        
        let current_height = self.current_version.load(Ordering::SeqCst);
        log::info!("[SCL-REINDEX] 🔍 Đang quét lại {} khối để khôi phục lịch sử thợ đào cho {}", current_height, hex::encode(addr));

        // Đọc chỉ mục giao dịch một lần duy nhất vào bộ nhớ để tránh đọc RocksDB liên tục trong vòng lặp
        let mut tx_index_updated = false;
        let mut txs = match self.db.get_cf(index_cf, addr).unwrap_or(None) {
            Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
            None => Vec::new(),
        };

        let mut batch = rocksdb::WriteBatch::default();

        for h in 0..=current_height {
            // [VANGUARD-REINDEX-OPTIMIZED] Sử dụng CF_COINBASE để bỏ qua việc giải mã Header nếu không cần
            if let Some(coinbase_hash) = self.db.get_cf(coinbase_cf, h.to_le_bytes()).unwrap_or(None) {
                let coinbase_tx_id: [u8; 32] = coinbase_hash.try_into().unwrap_or([0u8; 32]);
                
                // Kiểm tra xem Receipt đã có Miner address này chưa (Xác thực qua DB)
                if let Some(receipt_raw) = self.db.get_cf(receipt_cf, &coinbase_tx_id).unwrap_or(None) {
                     if let Ok(receipt) = TrackedTx::try_from_slice(&receipt_raw) {
                         if &receipt.receiver == addr {
                             // Đã khớp! Ghi vào chỉ mục nếu chưa có
                             if !txs.contains(&coinbase_tx_id) {
                                 txs.push(coinbase_tx_id);
                                 if txs.len() > 10000 {
                                     txs.remove(0);
                                 }
                                 tx_index_updated = true;
                             }
                             continue;
                         }
                     }
                }
            }

            if let Some(h_bytes) = self.db.get_cf(blocks_cf, h.to_le_bytes()).unwrap_or(None) {
                if let Some(header_raw) = self.db.get_cf(header_cf, &h_bytes).unwrap_or(None) {
                    if let Ok(header) = crate::proto::block::BlockHeader::decode(header_raw.as_slice()) {
                        // So sánh địa chỉ thợ đào (phải khớp 32 bytes)
                        let miner_addr = header.miner_address.map(|a| a.value).unwrap_or_default();
                        if miner_addr.len() == 32 && miner_addr.as_slice() == addr {
                            // Khớp! Tìm giao dịch Coinbase trong Body
                            if let Some(body_raw) = self.db.get_cf(body_cf, &h_bytes).unwrap_or(None) {
                                if let Ok(body) = crate::proto::block::BlockBody::decode(body_raw.as_slice()) {
                                    if let Some(coinbase_tx) = body.transactions.first() {
                                        // [VANGUARD-FIX] Tính toán lại Hash của Coinbase vì Protobuf Transaction không lưu Hash
                                        let tx_bytes = <crate::proto::transaction::Transaction as prost::Message>::encode_to_vec(coinbase_tx);
                                        let tx_hash_vec = crate::calculate_tx_hash(tx_bytes, h);
                                        let tx_id_arr: [u8; 32] = tx_hash_vec.try_into().unwrap_or([0u8; 32]);
                                        
                                        // Nếu chưa có receipt (khối cũ), tạo mới dựa trên Issuance Schedule
                                        if self.db.get_cf(receipt_cf, &tx_id_arr).unwrap_or(None).is_none() {
                                            let reward = crate::reward_logic::calculate_block_reward_btc_z(h);
                                            let mut total_fees = 0;
                                            for t in &body.transactions {
                                                total_fees += t.fee;
                                            }
                                            let total_reward = reward + total_fees;

                                            let tracked = TrackedTx {
                                                tx_id: tx_id_arr,
                                                sender: [0u8; 32],
                                                receiver: *addr,
                                                amount: total_reward,
                                                fee: 0,
                                                timestamp: header.timestamp as i64,
                                                block_height: h,
                                                nonce: 0,
                                                status: 1,
                                                is_finalized: true,
                                                confirmations: current_height - h,
                                                ..Default::default()
                                            };
                                            let data = borsh::to_vec(&tracked).unwrap();
                                            batch.put_cf(receipt_cf, &tx_id_arr, &data);
                                        }
                                        
                                        // Cập nhật index
                                        if !txs.contains(&tx_id_arr) {
                                            txs.push(tx_id_arr);
                                            if txs.len() > 10000 {
                                                txs.remove(0);
                                            }
                                            tx_index_updated = true;
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // Ghi lô xuống database định kỳ mỗi 1000 khối để giảm dung lượng RAM tích lũy và tránh lỗi ngoại lệ
            if h > 0 && h % 1000 == 0 {
                if tx_index_updated {
                    let data = borsh::to_vec(&txs).unwrap();
                    batch.put_cf(index_cf, addr, &data);
                    tx_index_updated = false;
                }
                self.db.write(batch).expect("Failed to commit reindex batch");
                batch = rocksdb::WriteBatch::default();
            }
        }

        if tx_index_updated {
            let data = borsh::to_vec(&txs).unwrap();
            batch.put_cf(index_cf, addr, &data);
        }
        self.db.write(batch).expect("Failed to commit reindex batch");
        log::info!("[SCL-REINDEX] ✅ Khôi phục hoàn tất cho {}", hex::encode(addr));
    }

    pub fn get_transaction_receipt(&self, tx_id: &[u8]) -> (u64, u32) {
        let cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        match self.db.get_cf(cf, tx_id).unwrap_or(None) {
            Some(data) => {
                let tx = TrackedTx::try_from_slice(&data).unwrap_or_default();
                (tx.block_height, tx.status)
            },
            None => (0, 0),
        }
    }

    pub fn put_transaction_receipt(&self, tx_hash: &[u8], height: u64, status: u32) {
        // [VANGUARD-FIX] Đồng nhất hóa: Luôn ghi dưới dạng TrackedTx để get_transaction_status không bị lỗi unwrap
        let cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let mut tx = TrackedTx::default();
        tx.tx_id.copy_from_slice(tx_hash);
        tx.block_height = height;
        tx.status = status;
        
        let data = borsh::to_vec(&tx).expect("Failed to serialize TrackedTx");
        self.db.put_cf(cf, tx_hash, &data).expect("Failed to write receipt");
    }

    pub fn export_state_snapshot_at_version(&self, version: u64) -> Vec<AccountSnapshot> {
        // [SECURITY-GUARD] Chỉ cho phép xuất Snapshot cho các khối đã được Finalized
        let finalized_h = self.get_finalized_height();
        if version > finalized_h {
            log::warn!("[SNAPSHOT-GUARD] ⚠️ Từ chối xuất Snapshot tại #{} (Vượt quá vùng an toàn finalized #{})", version, finalized_h);
            return Vec::new();
        }

        log::info!("📸 [SNAPSHOT] Đang trích xuất trạng thái Bất biến tại phiên bản: #{}", version);
        
        let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let cf_kh_to_addr = self.db.cf_handle(CF_KEYHASH_TO_ADDR).expect("Missing KEYHASH_TO_ADDR CF");
        
        // Nhất thể hóa định danh bằng cách lặp trực tiếp qua CF_KEYHASH_TO_ADDR
        let iter = self.db.iterator_cf(cf_kh_to_addr, rocksdb::IteratorMode::Start);
        let mut snapshot = Vec::new();
        let mut seen_addresses = std::collections::HashSet::new();

        let current_v = self.get_current_version();

        // [PERFORMANCE-FIX] Khởi tạo JMT một lần duy nhất bên ngoài vòng lặp để tránh overhead cấp phát lại liên tục
        let jmt_tree = JellyfishMerkleTree::<'_, Self, blake3::Hasher>::new(self);

        for item in iter {
            if let Ok((_kh_bytes, addr_bytes)) = item {
                if addr_bytes.len() == 32 {
                    let mut addr = [0u8; 32];
                    addr.copy_from_slice(&addr_bytes);
                    
                    if !seen_addresses.insert(addr) {
                        continue;
                    }
                    
                    let kh = jmt::KeyHash::with::<blake3::Hasher>(&addr);
                    
                    // [VANGUARD-ULTIMATE-FIX] BỎ QUA JMT! 
                    // Nếu đang ở đỉnh (Current), lấy luôn latest_val từ CF_ACC cực nhanh (O(1))
                    if version == current_v {
                        if let Some(latest_val) = self.db.get_cf(acc_cf, addr).unwrap_or(None) {
                            if let Ok(state) = AccountState::try_from_slice(&latest_val) {
                                snapshot.push(crate::AccountSnapshot {
                                    address: addr.to_vec(),
                                    balance: state.btc_z,
                                    nonce: state.nonce,
                                    nano_weight: state.nano_weight,
                                    coin_id: state.coin_id.to_vec(),
                                    last_full_cleanup: state.last_full_cleanup,
                                    maturing_rewards: state.maturing_rewards,
                                });
                            }
                        }
                    } else {
                        // Tại sao: Jellyfish Merkle Tree (JMT) là nguồn lưu trữ trạng thái chính xác và duy nhất được bảo chứng bởi Merkle Root.
                        // CF_ACC_HISTORY (lịch sử lưu trữ phẳng RocksDB) có khả năng bị thiếu bản ghi hoặc không cập nhật đúng
                        // sau các tiến trình Fast Sync hoặc các sự kiện Fork/Reorg phức tạp. Do đó, việc đọc trực tiếp từ JMT đảm bảo dữ liệu
                        // của Snapshot luôn khớp 100% với StateRoot của khối đó, loại bỏ triệt để lỗi lệch root khi Node khác nạp snapshot.
                        if let Ok(Some(val_bytes)) = jmt_tree.get(kh, version) {
                            if let Ok(state) = AccountState::try_from_slice(&val_bytes) {
                                snapshot.push(crate::AccountSnapshot {
                                    address: addr.to_vec(),
                                    balance: state.btc_z,
                                    nonce: state.nonce,
                                    nano_weight: state.nano_weight,
                                    coin_id: state.coin_id.to_vec(),
                                    last_full_cleanup: state.last_full_cleanup,
                                    maturing_rewards: state.maturing_rewards,
                                });
                            }
                        }
                    }
                }
            }
        }
        
        log::info!("✅ [SNAPSHOT] Đã đóng gói thành công {} tài khoản bất biến từ khối #{}", snapshot.len(), version);
        snapshot
    }

    pub fn export_state_snapshot(&self) -> Vec<AccountSnapshot> {
        let version = self.get_finalized_height();
        self.export_state_snapshot_at_version(version)
    }

    /// [VANGUARD-OOM-SHIELD] Trích xuất Snapshot trực tiếp vào File bằng cơ chế Luồng (Streaming)
    /// Tiêu diệt hoàn toàn nguy cơ tràn RAM khi Ledger phình to.
    pub fn export_state_snapshot_to_file(&self, path: &str, version: u64) -> anyhow::Result<u64> {
        use std::fs::File;
        use std::io::{BufWriter, Write, Seek, SeekFrom};
        use borsh::BorshSerialize;

        log::info!("📸 [SNAPSHOT-STREAM] Đang trích xuất luồng trạng thái khối #{} vào file: {}", version, path);
        
        let mut file = File::create(path)?;
        file.write_all(&[0u8; 4])?; // Dự phòng 4 byte đầu tiên để ghi tổng số lượng tài khoản (tương thích Vec<AccountSnapshot>)
        
        let mut writer = BufWriter::new(file);
        
        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let iter = self.db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
        
        let mut count = 0u64;
        let mut seen_addresses = std::collections::HashSet::new();

        // [STREAMING-LOOP] Duyệt và ghi trực tiếp, RAM chỉ chứa duy nhất 1 AccountState tại một thời điểm
        for item in iter {
            if let Ok((key, _)) = item {
                if key.len() == 32 {
                    let mut addr_bytes = [0u8; 32];
                    addr_bytes.copy_from_slice(&key);
                    let kh = jmt::KeyHash::with::<blake3::Hasher>(&addr_bytes);
                    
                    if let Ok(Some(val_bytes)) = jmt.get(kh, version) {
                        if seen_addresses.insert(addr_bytes) {
                            if let Ok(state) = AccountState::try_from_slice(&val_bytes) {
                                let snapshot_item = crate::AccountSnapshot {
                                    address: addr_bytes.to_vec(),
                                    balance: state.btc_z,
                                    nonce: state.nonce,
                                    nano_weight: state.nano_weight,
                                    coin_id: state.coin_id.to_vec(),
                                    last_full_cleanup: state.last_full_cleanup,
                                    maturing_rewards: state.maturing_rewards,
                                };
                                // Serialize trực tiếp vào BufWriter
                                BorshSerialize::serialize(&snapshot_item, &mut writer)?;
                                count += 1;
                            }
                        }
                    }
                }
            }
        }
        
        writer.flush()?;
        
        // Quay lại đầu file để ghi chính xác số lượng tài khoản đã trích xuất thực tế (LE bytes)
        let mut file = writer.into_inner()?;
        file.seek(SeekFrom::Start(0))?;
        file.write_all(&(count as u32).to_le_bytes())?;
        
        log::info!("✅ [SNAPSHOT-STREAM] Đã trích xuất xong {} tài khoản vào file.", count);
        Ok(count)
    }

    pub fn import_state_snapshot(&self, snapshot: Vec<AccountSnapshot>, version: u64) -> Result<[u8; 32]> {
        log::info!("🧬 [IMPORT] Đang nạp Snapshot tại phiên bản: {} ({} tài khoản)...", version, snapshot.len());
        
        // [VANGUARD-FIX] Khử trùng lặp Snapshot trước khi nạp để bảo vệ tổng cung
        let mut unique_map = std::collections::HashMap::new();
        for acc in snapshot {
            let mut addr = [0u8; 32];
            if acc.address.len() == 32 { addr.copy_from_slice(&acc.address); }
            unique_map.insert(addr, acc);
        }
        let snapshot_final: Vec<AccountSnapshot> = unique_map.into_values().collect();

        let acc_cf = self.db.cf_handle(CF_ACC).context("Missing ACC CF")?;
        let history_cf = self.db.cf_handle(CF_ACC_HISTORY).context("Missing ACC_HISTORY CF")?;
        let mut value_set = Vec::new();

        // 1. Nạp trực tiếp vào sổ cái thô (CF_ACC) và chuẩn bị dữ liệu cho JMT
        let mut batch = WriteBatch::default();
        for acc in snapshot_final {
            let addr: [u8; 32] = acc.address.try_into().map_err(|_| anyhow::anyhow!("Invalid address length"))?;
            
            let state = AccountState {
                btc_z: acc.balance,
                nonce: acc.nonce,
                nano_weight: acc.nano_weight,
                coin_id: acc.coin_id.try_into().unwrap_or([0u8; 32]),
                last_full_cleanup: acc.last_full_cleanup,
                maturing_rewards: acc.maturing_rewards,
            };
            
            let state_bytes = borsh::to_vec(&state).unwrap();
            batch.put_cf(acc_cf, addr, &state_bytes);
            
            let key_hash = KeyHash::with::<blake3::Hasher>(&addr);

            // [VANGUARD-FIX] Ghi vào Lịch sử (KeyHash + Version) để TreeReader có thể tìm thấy dữ liệu khi get
            let mut versioned_key = [0u8; 40];
            versioned_key[0..32].copy_from_slice(&key_hash.0);
            versioned_key[32..40].copy_from_slice(&version.to_be_bytes());
            batch.put_cf(history_cf, &versioned_key, &state_bytes);

            // [BIG-DATA-INDEX] Ghi KeyHash -> Address index
            if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                batch.put_cf(cf_kh_to_addr, key_hash.0, addr);
            }

            let val_bytes = state.try_to_vec().unwrap();
            value_set.push((key_hash, Some(val_bytes)));
        }
        self.db.write(batch).context("Failed to write snapshot batch to CF_ACC")?;

        // 2. Tái cấu trúc Cây Merkle chính thức (JMT) tại phiên bản Snapshot
        // [JMT-LEXICOGRAPHICAL-FIX] BẮT BUỘC SẮP XẾP VALUE_SET THEO THỨ TỰ TĂNG DẦN CỦA KEYHASH
        value_set.sort_by(|a, b| a.0.0.cmp(&b.0.0));

        let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        let (new_root, batch) = jmt.put_value_set(value_set, version).context("JMT snapshot build failed")?;
        self.write_node_batch(&batch.node_batch).context("Failed to write snapshot JMT nodes")?;

        // 3. Cập nhật Metadata và TỔNG CUNG (Kiểm toán toàn diện sau nạp)
        let meta_cf = self.db.cf_handle(CF_META).context("Missing META CF")?;
        self.db.put_cf(meta_cf, b"jmt_v", version.to_le_bytes()).context("Failed to update jmt_v")?;
        self.current_version.store(version, Ordering::SeqCst);
        self.db.put_cf(meta_cf, b"finalized_h", version.to_le_bytes()).context("Failed to update finalized_h")?;
        // [VANGUARD-FIX] Cập nhật mốc snapshot vào lowest_full_height để tối ưu hóa get_oldest_height()
        self.db.put_cf(meta_cf, b"lowest_full_height", version.to_le_bytes()).context("Failed to update lowest_full_height")?;
        
        let expected_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(version);
        self.set_actual_total_supply(expected_supply);
        self.set_expected_supply(expected_supply); 
        
        log::info!("✅ [IMPORT] Hoàn tất nạp Snapshot. Root JMT (v{}): {}, Supply: {}", version, hex::encode(&new_root.0), expected_supply);
        Ok(new_root.0)
    }

    /// [VANGUARD-V2.2] Tái cấu trúc Jellyfish Merkle Tree từ dữ liệu phẳng (CF_ACC)
    /// Đây là "Cứu cánh" cho các Node sau khi Snap Sync thành công nhưng bị thiếu các nút trung gian.
    pub fn rebuild_jmt_from_accounts(&self, version: u64, expected_root: [u8; 32]) -> Result<[u8; 32], String> {
        log::warn!("🛠️ [JMT-REBUILD] Bắt đầu tái cấu trúc cây Merkle từ dữ liệu phẳng tại version: {}", version);

        // --- [VANGUARD-SAFETY-GATE] CHỐT CHẶN LOGIC AN TOÀN ---
        // Kiểm tra xem Header của cao độ này đã được tải và xác thực PoW chưa
        let anchor_hash = self.get_block_hash(version).ok_or_else(|| {
            format!("❌ [VANGUARD-ERROR] Không tìm thấy mã băm khối cho # {}. Bạn phải tải chuỗi Header trước khi nạp Snapshot!", version)
        })?;

        let header_raw = self.get_header_raw(&anchor_hash).ok_or_else(|| {
            format!("❌ [VANGUARD-ERROR] Thiếu dữ liệu Header cho khối # {}. Tính toàn vẹn chuỗi PoW chưa được đảm bảo!", version)
        })?;

        // Giải mã Header để lấy StateRoot "chính chủ" từ chuỗi PoW
        use prost::Message;
        let header = crate::proto::block::BlockHeader::decode(&header_raw[..]).map_err(|e| format!("Lỗi giải mã Header: {}", e))?;
        let official_root = header.state_root.as_ref().ok_or("Header không chứa StateRoot")?.value.clone();

        // [CRITICAL] StateRoot trong Snapshot phải KHỚP TUYỆT ĐỐI với StateRoot trong Header (PoW verified)
        if official_root != expected_root {
            log::error!("🚨 [SECURITY-ALERT] PHÁT HIỆN GIAN LẬN SNAPSHOT!");
            log::error!("StateRoot trong Header (PoW): {}", hex::encode(&official_root));
            log::error!("StateRoot yêu cầu nạp:      {}", hex::encode(&expected_root));
            return Err("VANGUARD_FRAUD_DETECTION: StateRoot mismatch between Header and Snapshot".to_string());
        }
        log::info!("✅ [VANGUARD-GATE] Đã xác thực StateRoot khớp với chuỗi PoW tại #{}", version);
        
        // [SNAP-SYNC-CLEAN-SLATE] Xóa bỏ toàn bộ cây JMT cũ trước khi tái cấu trúc.
        // Tránh việc các node trung gian cũ không còn hợp lệ làm hỏng cấu trúc cây mới.
        // [VANGUARD-CLEAN-SLATE] Sử dụng cận xóa &[] đến &[0xffu8; 128] để dọn sạch hoàn toàn 100% mọi key.
        if let Some(cf_jmt) = self.db.cf_handle(CF_JMT) {
            let start_all: &[u8] = &[];
            let end_all: &[u8] = &[0xffu8; 128];
            let _ = self.db.delete_range_cf(cf_jmt, start_all, end_all);
            log::info!("🧹 [JMT-REBUILD] Đã dọn sạch CF_JMT an toàn để chuẩn bị xây dựng lại cây.");
        }

        // --- [VANGUARD-BATCHED-REBUILD] Xử lý theo lô 50.000 tài khoản ---
        // Tại sao: Nạp toàn bộ CF_ACC vào BTreeMap trên RAM sẽ gây OOM trên VPS nhỏ
        // khi số tài khoản tăng trên 500k+. Thay vào đó, ta stream qua iterator,
        // tích lũy batch 50k entries, gọi put_value_set -> write_node_batch -> drop batch,
        // rồi lặp lại. JMT sẽ tự đọc lại cây đã ghi từ batch trước thông qua TreeReader.
        const BATCH_SIZE: usize = 50_000;

        let acc_cf = self.db.cf_handle(CF_ACC).expect("Missing ACC CF");
        let mut actual_supply: u64 = 0;
        let mut total_count: usize = 0;
        let mut batch_num: usize = 0;
        let mut new_root = jmt::RootHash([0u8; 32]);

        // [PHASE 1] Đếm tổng số tài khoản trước để log tiến trình chính xác
        // Sử dụng iterator nhẹ chỉ đếm key, không giữ value trên RAM
        let mut total_accounts: usize = 0;
        {
            let count_iter = self.db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
            for item in count_iter {
                if let Ok((key, _)) = item {
                    if key.len() == 32 {
                        total_accounts += 1;
                    }
                }
            }
        }

        if total_accounts == 0 {
            log::error!("🚨 [JMT-REBUILD] CRITICAL: Snapshot rỗng (0 tài khoản trong CF_ACC) nhưng expected_root != ZERO!");
            log::error!("   expected_root = {}", hex::encode(&expected_root));
            log::error!("   Từ chối nạp snapshot rỗng để tránh lệch CF_ACC vs CF_JMT!");
            return Err("SNAPSHOT_EMPTY: Không có dữ liệu tài khoản nào để tái cấu trúc JMT. Snapshot bị lỗi hoặc peer gửi dữ liệu rỗng.".to_string());
        }

        let total_batches = (total_accounts + BATCH_SIZE - 1) / BATCH_SIZE;
        log::info!("📊 [JMT-REBUILD] Phát hiện {} tài khoản. Chia thành {} lô (mỗi lô ≤ {} tài khoản).",
            total_accounts, total_batches, BATCH_SIZE);

        // [PHASE 2] Stream qua CF_ACC và xử lý theo batch
        let iter = self.db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
        let mut value_set: Vec<(KeyHash, Option<jmt::OwnedValue>)> = Vec::with_capacity(BATCH_SIZE);

        for item in iter {
            if let Ok((key, value)) = item {
                if key.len() == 32 {
                    let addr_bytes: [u8; 32] = key[..].try_into().unwrap();
                    let kh = KeyHash::with::<blake3::Hasher>(&addr_bytes);
                    
                    if let Ok(Some(_)) = self.db.get_cf(acc_cf, kh.0) {
                        // Tích lũy supply xuyên suốt các batch (bao gồm cả maturing_rewards chưa chín để đồng bộ hóa kế toán)
                        if let Ok(state) = crate::state_manager::AccountState::try_from_slice(&value) {
                            actual_supply = actual_supply.saturating_add(state.btc_z);
                            for reward in &state.maturing_rewards {
                                actual_supply = actual_supply.saturating_add(reward.amount);
                            }
                        }
                        
                        value_set.push((kh, Some(value.to_vec())));
                        total_count += 1;

                        // Khi đạt BATCH_SIZE, flush batch xuống DB
                        if value_set.len() >= BATCH_SIZE {
                            batch_num += 1;
                            log::info!("⚙️ [JMT-REBUILD] Đang xử lý lô {}/{} ({} tài khoản, tổng cộng: {}/{})",
                                batch_num, total_batches, value_set.len(), total_count, total_accounts);

                            // [JMT-LEXICOGRAPHICAL-FIX] BẮT BUỘC SẮP XẾP VALUE_SET THEO THỨ TỰ TĂNG DẦN CỦA KEYHASH
                            value_set.sort_by(|a, b| a.0.0.cmp(&b.0.0));

                            let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
                            let (root, batch) = jmt.put_value_set(value_set, version)
                                .map_err(|e| format!("JMT_BUILD_ERROR tại lô {}: {}", batch_num, e))?;
                            
                            // Ghi ngay node_batch xuống DB để giải phóng RAM và cho batch sau đọc lại
                            self.write_node_batch(&batch.node_batch)
                                .map_err(|e| format!("WRITE_ERROR tại lô {}: {}", batch_num, e))?;
                            
                            new_root = root;
                            log::info!("   ✅ Lô {} hoàn tất. Root trung gian: {}", batch_num, hex::encode(&new_root.0));

                            // Drop batch cũ, tạo buffer mới → giải phóng RAM hoàn toàn
                            value_set = Vec::with_capacity(BATCH_SIZE);
                        }
                    }
                }
            }
        }

        // [PHASE 3] Flush batch cuối cùng (phần dư < BATCH_SIZE)
        if !value_set.is_empty() {
            batch_num += 1;
            log::info!("⚙️ [JMT-REBUILD] Đang xử lý lô cuối {}/{} ({} tài khoản, tổng cộng: {}/{})",
                batch_num, total_batches, value_set.len(), total_count, total_accounts);

            // [JMT-LEXICOGRAPHICAL-FIX] BẮT BUỘC SẮP XẾP VALUE_SET THEO THỨ TỰ TĂNG DẦN CỦA KEYHASH
            value_set.sort_by(|a, b| a.0.0.cmp(&b.0.0));

            let jmt = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
            let (root, batch) = jmt.put_value_set(value_set, version)
                .map_err(|e| format!("JMT_BUILD_ERROR tại lô cuối {}: {}", batch_num, e))?;
            
            self.write_node_batch(&batch.node_batch)
                .map_err(|e| format!("WRITE_ERROR tại lô cuối {}: {}", batch_num, e))?;
            
            new_root = root;
            log::info!("   ✅ Lô cuối {} hoàn tất. Root cuối cùng: {}", batch_num, hex::encode(&new_root.0));
        }

        log::info!("📊 [JMT-REBUILD] Hoàn tất xử lý {} tài khoản qua {} lô. Supply tích lũy: {}",
            total_count, batch_num, actual_supply);

        // Chốt chặn phiên bản JMT và mốc Finalized
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        self.db.put_cf(meta_cf, b"jmt_v", version.to_le_bytes()).map_err(|e| e.to_string())?;
        self.db.put_cf(meta_cf, b"finalized_h", version.to_le_bytes()).map_err(|e| e.to_string())?;
        self.current_version.store(version, Ordering::SeqCst);
        // [VANGUARD-FIX] Cập nhật mốc snapshot vào lowest_full_height để tối ưu hóa get_oldest_height()
        self.db.put_cf(meta_cf, b"lowest_full_height", version.to_le_bytes()).map_err(|e| e.to_string())?;

        // [VANGUARD-FIX] Cập nhật Supply Cache đã tính toán
        self.set_actual_total_supply(actual_supply);
        let expected = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(version);
        self.set_expected_supply(expected);
        log::info!("✅ [JMT-REBUILD] Đã cập nhật Supply Cache: Actual={}, Expected={}", actual_supply, expected);

        // KIỂM TOÁN TỐI THƯỢNG: State Root phải khớp 100% với kỳ vọng của mạng lưới
        if new_root.0 != expected_root {
            log::error!("🚨 [JMT-REBUILD-FATAL] SAI LỆCH STATE ROOT SAU TÁI CẤU TRÚC!");
            log::error!("Thực tế: {}", hex::encode(&new_root.0));
            log::error!("Kỳ vọng: {}", hex::encode(&expected_root));
            return Err(format!("STATE_ROOT_MISMATCH: {} != {}", hex::encode(&new_root.0), hex::encode(&expected_root)));
        }
        
        log::info!("✅ [JMT-REBUILD] Tái cấu trúc thành công. Node đã sẵn sàng vận hành tại cao độ #{}", version);
        Ok(new_root.0)
    }

    /// [Audit V11.0 Cấp A FIX] Sinh Merkle Proof thực tế từ JMT
    /// Sử dụng jmt_fork::SparseMerkleProof để truy cập dữ liệu an toàn mà không cần hack proxy.
    pub fn get_merkle_proof(&self, address: &[u8; 32]) -> Vec<[u8; 32]> {
        let version = self.get_current_version();
        self.get_merkle_proof_at_height(address, version)
    }

    /// [Audit V11.0 Cấp A FIX] Sinh Merkle Proof thực tế từ JMT tại cao độ cụ thể
    pub fn get_merkle_proof_at_height(&self, address: &[u8; 32], height: u64) -> Vec<[u8; 32]> {
        let jmt_tree = JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(self);
        let key_hash = KeyHash::with::<blake3::Hasher>(address);

        if let Ok((_value, proof)) = jmt_tree.get_with_proof(key_hash, height) {
            // [VANGUARD-V2.2] Chuyển đổi an toàn sang siblings hashes thông qua Serde marshalling
            let json = serde_json::to_string(&proof).unwrap_or_default();
            let fork_proof: crate::SparseMerkleProof = serde_json::from_str(&json).unwrap_or_default();
            return fork_proof.siblings;
        }
        Vec::new()
    }



    /// [VANGUARD-V1.1.4] ĐẠI THANH TRỪNG (GREAT PURGE)
    /// Xóa sạch toàn bộ dữ liệu lịch sử của một Epoch (1.152 khối).
    /// Sử dụng WriteBatch nguyên tử để đảm bảo tốc độ và tính toàn vẹn.
    pub fn purge_historical_data(&self, start_height: u64, end_height: u64) -> Result<()> {
        if start_height > end_height { return Ok(()); }
        
        log::warn!("🧹 [GREAT-PURGE] Kích hoạt Cầu dao Thanh trừng: #{} -> #{}", start_height, end_height);
        
        let mut batch = WriteBatch::default();
        
        // [FIX V2.0] GIỮ LẠI CF_COINBASE theo Hiến pháp: Chỉ xóa TXS và TOUCHED_ACCS
        let le_cf_names = [CF_BLOCK_TXS, CF_TOUCHED_ACCS];
        let le_cfs: Vec<_> = le_cf_names.iter()
            .filter_map(|name| self.db.cf_handle(name))
            .collect();
        
        let body_cf = self.db.cf_handle(CF_BLOCK_BODIES).expect("Missing BODIES CF");
        let receipt_cf = self.db.cf_handle(CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let block_txs_cf = self.db.cf_handle(CF_BLOCK_TXS).expect("Missing BLOCK_TXS CF");

        let touched_cf = self.db.cf_handle(CF_TOUCHED_ACCS).expect("Missing TOUCHED CF");
        let hist_cf = self.db.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");

        for h in start_height..=end_height {
            let key = h.to_le_bytes();
            let key_be = h.to_be_bytes();
            
            // 1. Xóa các bản ghi lịch sử tối ưu [KeyHash][Height BE]
            // Ta phải lấy danh sách các tài khoản đã thay đổi tại height này
            if let Some(accs_raw) = self.db.get_cf(touched_cf, key).unwrap_or(None) {
                if let Ok(accs) = <Vec<[u8; 32]>>::try_from_slice(&accs_raw) {
                    for addr in accs {
                        let key_hash = jmt::KeyHash::with::<blake3::Hasher>(&addr);
                        let mut versioned_key = [0u8; 40];
                        versioned_key[0..32].copy_from_slice(&key_hash.0);
                        versioned_key[32..40].copy_from_slice(&key_be);
                        batch.delete_cf(hist_cf, &versioned_key);
                    }
                }
            }

            // 2. Xóa tất cả LE CFs cho height này (TXS, TOUCHED, ...)
            for cf in &le_cfs {
                batch.delete_cf(cf, &key);
            }
            
            // 3. Xóa Block Body (keyed by hash)
            if let Some(h_hash) = self.get_block_hash(h) {
                batch.delete_cf(body_cf, &h_hash);
                
                // Truy vết và xóa Receipts
                if let Some(tx_data) = self.db.get_cf(block_txs_cf, key).unwrap_or(None) {
                    if let Ok(tx_hashes) = <Vec<[u8; 32]>>::try_from_slice(&tx_data) {
                        for tx_id in tx_hashes {
                            batch.delete_cf(receipt_cf, &tx_id);
                        }
                    }
                }
            }
        }


        // [VANGUARD-FIX] Xóa cache lowest_full_height để tính toán lại sau khi thanh trừng dữ liệu
        let meta_cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        batch.delete_cf(meta_cf, b"lowest_full_height");

        // Thực thi nguyên tử
        let mut opts = rocksdb::WriteOptions::default();
        opts.set_sync(true);
        self.db.write_opt(batch, &opts)?;
        
        log::info!("✅ [GREAT-PURGE] Đại Thanh Trừng hoàn tất ({} khối). Đã dọn sạch Versioned History.", end_height - start_height + 1);
        Ok(())
    }

    /// [LỖI CẤP S #2 FIX] Đọc Tổng cung Thực tế từ Cache CF_META (O(1))
    /// Nếu cache chưa được cập nhật phiên bản V2 (bao gồm Maturing Rewards), thực hiện quét lại.
    /// [LỖI CẤP S #2 FIX] Đọc Tổng cung Thực tế từ Atomic Cache (O(1))
    pub fn get_actual_total_supply(&self) -> u64 {
        let val = self.actual_total_supply.load(Ordering::SeqCst);
        if val == 0 && self.get_current_version() > 0 {
             log::error!("🔴 [CRITICAL-STATE] get_actual_total_supply TRẢ VỀ 0 tại cao độ #{}! Hệ thống đang bị hổng bộ nhớ RAM.", self.get_current_version());
        }
        val
    }

    pub fn calculate_actual_total_supply_full_scan(&self) -> u64 {
        // Hàm này giờ đây CHỈ DÙNG CHO LỆNH CLI "ledger scan" (người dùng tự gõ)
        // Tuyệt đối không dùng để quyết định sinh tử của hệ thống khi chạy khối.
        let snapshot = self.export_state_snapshot();
        let mut actual_supply = 0u64;
        
        for state in snapshot {
            actual_supply = actual_supply.saturating_add(state.balance);
            for reward in state.maturing_rewards {
                actual_supply = actual_supply.saturating_add(reward.amount);
            }
        }

        // Lưới an toàn: Nếu quét ra 0 nhưng chiều cao mạng > 0, trả về số trong đĩa!
        if actual_supply == 0 && self.get_current_version() > 0 {
            let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
            return match self.db.get_cf(cf, b"actual_supply_v2").unwrap_or(None) {
                Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
                None => 0,
            };
        }

        actual_supply
    }


    /// [LỖI CẤP S #2 FIX] Lưu Tổng cung Thực tế vào Cache CF_META (O(1))
    pub fn set_actual_total_supply(&self, supply: u64) {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        // Lưu vào cả 2 key để tương thích
        self.db.put_cf(cf, b"actual_supply", supply.to_le_bytes()).expect("Failed to cache supply v1");
        self.db.put_cf(cf, b"actual_supply_v2", supply.to_le_bytes()).expect("Failed to cache supply v2");
        self.actual_total_supply.store(supply, Ordering::SeqCst);
    }

    pub fn set_actual_total_supply_to_batch(&self, batch: &mut rocksdb::WriteBatch, supply: u64) {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        batch.put_cf(cf, b"actual_supply", supply.to_le_bytes());
        batch.put_cf(cf, b"actual_supply_v2", supply.to_le_bytes());
    }


    /// [V1.19] Truy xuất Thân khối (Body) từ Column Family chuyên biệt
    pub fn get_block_body_raw(&self, hash: &[u8; 32]) -> Option<Vec<u8>> {
        let cf = self.db.cf_handle(CF_BLOCK_BODIES).expect("Missing BLOCK_BODIES CF");
        self.db.get_cf(cf, hash).unwrap_or(None)
    }

    /// [VANGUARD-REORG] Lấy dữ liệu khối đầy đủ theo chiều cao
    pub fn get_block_raw_by_height(&self, height: u64) -> Option<Vec<u8>> {
        use crate::proto::block::Block;
        let hash = self.get_block_hash(height)?;
        let header_raw = self.get_header_raw(&hash)?;
        let body_raw = self.get_block_body_raw(&hash).unwrap_or_default();
        
        let mut block = Block::default();
        if let Ok(header) = Message::decode(&header_raw[..]) {
            block.header = Some(header);
        }
        if !body_raw.is_empty() {
            if let Ok(body) = Message::decode(&body_raw[..]) {
                block.body = Some(body);
            }
        }
        
        Some(block.encode_to_vec())
    }

    /// [VANGUARD-V2] Truy xuất Giao dịch Coinbase - Bảo tồn sau Purge
    pub fn get_coinbase_raw(&self, hash: &[u8; 32]) -> Option<Vec<u8>> {
        // [AUDIT] Lấy Header để giải mã Height
        if let Some(header_data) = self.get_header_raw(hash) {
            use crate::proto::block::BlockHeader;
            if let Ok(header) = BlockHeader::decode(&header_data[..]) {
                let cf = self.db.cf_handle(CF_COINBASE).expect("Missing COINBASE CF");
                return self.db.get_cf(cf, header.height.to_le_bytes()).unwrap_or(None);
            }
        }
        None
    }

    /// [V1.19] Lưu trữ Thân khối (Body) phục vụ truy vấn lịch sử
    pub fn put_block_body(&self, hash: &[u8; 32], data: Vec<u8>) {
        let cf = self.db.cf_handle(CF_BLOCK_BODIES).expect("Missing BLOCK_BODIES CF");
        self.db.put_cf(cf, hash, data).expect("Failed to put block body");
    }

    pub fn put_block_body_to_batch(&self, batch: &mut rocksdb::WriteBatch, hash: &[u8; 32], data: Vec<u8>) {
        let cf = self.db.cf_handle(CF_BLOCK_BODIES).expect("Missing BLOCK_BODIES CF");
        batch.put_cf(cf, hash, data);
    }


    /// [HOTFIX V1.17] Đọc Expected Supply Cached trong CF_META (O(1))
    pub fn get_expected_supply(&self) -> u64 {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        match self.db.get_cf(cf, b"expected_supply").unwrap_or(None) {
            Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
            None => 0,
        }
    }

    /// [HOTFIX V1.17] Lưu Expected Supply vào Cache CF_META (O(1))
    pub fn set_expected_supply(&self, supply: u64) {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        self.db.put_cf(cf, b"expected_supply", supply.to_le_bytes()).expect("Failed to cache expected supply");
    }

    pub fn set_expected_supply_to_batch(&self, batch: &mut rocksdb::WriteBatch, supply: u64) {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        batch.put_cf(cf, b"expected_supply", supply.to_le_bytes());
    }




    pub fn consolidate_smt_batch_custom(&self, batch_kv: Vec<([u8; 32], Vec<u8>)>, version: jmt::Version, cf_name: &'static str) -> [u8; 32] {
        let storage = SmtStorage { mgr: self, cf: cf_name };
        let jmt = JellyfishMerkleTree::<'_, SmtStorage, blake3::Hasher>::new(&storage);
        let mut value_set = Vec::new();
        for (addr, data) in batch_kv {
            value_set.push((KeyHash::with::<blake3::Hasher>(addr), Some(data)));
        }

        let (new_root, batch) = jmt.put_value_set(value_set, version).expect("JMT batch write failed");
        self.write_node_batch_custom(&batch.node_batch, cf_name).expect("JMT write failed");
        new_root.0
    }
    pub fn consolidate_smt_batch_at_version(&self, batch_kv: Vec<([u8; 32], Vec<u8>)>, version: jmt::Version) -> [u8; 32] {
        self.consolidate_smt_batch_custom(batch_kv, version, CF_JMT)
    }

    pub fn debug_dump_smt_nodes(&self) -> String {
        let mut dump = String::new();
        let cf = self.db.cf_handle(CF_SMT_NODES).expect("Missing SMT CF");
        let iter = self.db.iterator_cf(cf, rocksdb::IteratorMode::Start);
        for item in iter {
            if let Ok((key, val)) = item {
                dump.push_str(&format!("{}:{}\n", hex::encode(key), hex::encode(val)));
            }
        }
        dump
    }


    /// [V1.50] Lưu trữ giao dịch vào Mempool vật lý
    pub fn add_to_mempool(&self, tx_hash: &[u8], tx_bytes: &[u8]) -> Result<()> {
        let cf = self.db.cf_handle(CF_MEMPOOL).context("Missing MEMPOOL CF")?;
        self.db.put_cf(cf, tx_hash, tx_bytes)?;
        Ok(())
    }

    /// [BATCHING] Lưu trữ lô giao dịch vào Mempool vật lý
    /// Tại sao: Để tăng TPS, giảm số cuộc gọi ghi đĩa đơn lẻ, tránh tắc nghẽn IO RocksDB khi có lượng giao dịch lớn
    pub fn add_batch_to_mempool(&self, entries: Vec<(Vec<u8>, Vec<u8>)>) -> Result<()> {
        let cf = self.db.cf_handle(CF_MEMPOOL).context("Missing MEMPOOL CF")?;
        let mut batch = WriteBatch::default();
        for (tx_hash, tx_bytes) in entries {
            batch.put_cf(cf, &tx_hash, &tx_bytes);
        }
        self.db.write(batch)?;
        Ok(())
    }

    /// [V1.50] Gỡ bỏ giao dịch khỏi Mempool (khi đã vào khối hoặc hết hạn)
    pub fn remove_from_mempool(&self, tx_hash: &[u8]) -> Result<()> {
        let cf = self.db.cf_handle(CF_MEMPOOL).context("Missing MEMPOOL CF")?;
        self.db.delete_cf(cf, tx_hash)?;
        Ok(())
    }

    /// [BATCHING] Xóa lô giao dịch khỏi Mempool vật lý
    /// Tại sao: Tránh bão I/O RocksDB khi thực thi và cập nhật mempool sau khi hoàn thành khối lớn
    pub fn remove_batch_from_mempool(&self, tx_hashes: Vec<Vec<u8>>) -> Result<()> {
        let cf = self.db.cf_handle(CF_MEMPOOL).context("Missing MEMPOOL CF")?;
        let mut batch = WriteBatch::default();
        for hash in tx_hashes {
            batch.delete_cf(cf, &hash);
        }
        self.db.write(batch)?;
        Ok(())
    }

    /// [V1.60] Lưu trữ cấu hình Node (Thay thế node_config.json)
    pub fn get_node_config(&self) -> Option<Vec<u8>> {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        self.db.get_cf(cf, b"node_config").unwrap_or(None)
    }

    pub fn set_node_config(&self, data: &[u8]) {
        let cf = self.db.cf_handle(CF_META).expect("Missing META CF");
        self.db.put_cf(cf, b"node_config", data).expect("Failed to save node config");
    }

    /// [V1.50] Lấy toàn bộ giao dịch đang chờ xử lý trong Mempool từ đĩa
    pub fn get_mempool_entries(&self) -> Vec<(Vec<u8>, Vec<u8>)> {
        let cf = self.db.cf_handle(CF_MEMPOOL).expect("Missing MEMPOOL CF");
        let mut results = Vec::new();
        let iter = self.db.iterator_cf(cf, rocksdb::IteratorMode::Start);
        for item in iter {
            if let Ok((key, val)) = item {
                results.push((key.to_vec(), val.to_vec()));
            }
        }
        results
    }
    pub fn get_oldest_height(&self) -> u64 {
        let current = self.get_current_version();
        if current == 0 { return 0; }
        
        let cf_meta = self.db.cf_handle(CF_META).expect("Missing META CF");
        
        // [VANGUARD-OPTIMIZED] Đọc lowest_full_height đã được cache trong CF_META để tối ưu hiệu năng O(1)
        if let Some(v) = self.db.get_cf(cf_meta, b"lowest_full_height").unwrap_or(None) {
            if v.len() == 8 {
                return u64::from_le_bytes(v.try_into().unwrap());
            }
        }

        // [VANGUARD-FIX] Nếu chưa cache, tính toán bằng cách tìm kiếm nhị phân khối X lớn nhất KHÔNG có body.
        // Khối bắt đầu có dữ liệu body liên tục chính là X + 1. Điều này giải quyết triệt để lỗi DB dơ (chứa block body cũ lẻ tẻ).
        let mut low = 0;
        let mut high = current;
        let mut max_no_body = None;
        
        while low <= high {
            let mid = (low + high) / 2;
            let has_body = if let Some(h_hash) = self.get_block_hash(mid) {
                self.db.get_cf(self.db.cf_handle(CF_BLOCK_BODIES).unwrap(), h_hash).unwrap_or(None).is_some()
            } else {
                false
            };

            if !has_body {
                max_no_body = Some(mid);
                low = mid + 1; // Tiếp tục tìm ở vùng cao hơn để tìm khối X lớn nhất không có body
            } else {
                if mid == 0 { break; }
                high = mid - 1;
            }
        }

        let oldest = match max_no_body {
            Some(x) => x + 1,
            None => 0,
        };

        // Cache lại vào CF_META để tối ưu hóa cho các lần gọi sau
        let _ = self.db.put_cf(cf_meta, b"lowest_full_height", oldest.to_le_bytes());
        
        log::info!("📊 [OLD-HEIGHT-AUDIT] Đã tính toán oldest_height thực tế: #{} (Current: #{})", oldest, current);
        oldest
    }



    /// [SECURITY-FIX] Di chuyển dữ liệu từ bảng tạm sang Sổ cái chính thức một cách nguyên tử
    /// Tích hợp cơ chế Chunking WriteBatch mỗi 10.000 tài khoản để triệt tiêu hoàn toàn nguy cơ OOM trên RAM.
    fn commit_staging_to_main(&self, version: u64) -> Result<()> {
        let staging_cf = self.db.cf_handle(CF_ACC_SYNC_STAGING).context("Missing staging CF")?;
        let acc_cf = self.db.cf_handle(CF_ACC).context("Missing main ACC CF")?;
        let history_cf = self.db.cf_handle(CF_ACC_HISTORY).context("Missing history CF")?;
        
        let mut opts = WriteOptions::default();
        opts.set_sync(true);

        // [SNAP-SYNC-CLEAN-SLATE] Xóa bỏ dữ liệu phẳng cũ trước khi nạp Snapshot.
        // Điều này đảm bảo StateRoot sau khi rebuild sẽ CHỈ chứa các tài khoản có trong Snapshot.
        // Thực hiện xóa sạch bằng WriteBatch nguyên tử đầu tiên để giữ an toàn tuyệt đối.
        // [VANGUARD-CLEAN-SLATE] Sử dụng cận xóa &[] đến &[0xffu8; 128] để dọn sạch hoàn toàn 100% mọi key phẳng.
        {
            let mut init_batch = WriteBatch::default();
            let start_all: &[u8] = &[];
            let end_all: &[u8] = &[0xffu8; 128];
            init_batch.delete_range_cf(acc_cf, start_all, end_all);
            init_batch.delete_range_cf(history_cf, start_all, end_all);

            if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                init_batch.delete_range_cf(cf_kh_to_addr, start_all, end_all);
            }
            log::info!("🧹 [SNAP-SYNC] Đang thực hiện dọn sạch triệt để CF_ACC, CF_ACC_HISTORY và CF_KEYHASH_TO_ADDR...");
            self.db.write_opt(init_batch, &opts)?;
            log::info!("🧹 [SNAP-SYNC] Dọn dẹp hoàn tất.");
        }

        let iter = self.db.iterator_cf(staging_cf, rocksdb::IteratorMode::Start);
        let mut batch = WriteBatch::default();
        let mut count = 0;
        
        for item in iter {
            if let Ok((addr, state_bytes)) = item {
                // 1. Ghi vào Sổ cái phẳng
                batch.put_cf(acc_cf, &addr, &state_bytes);
                
                // 2. Ghi vào Lịch sử (KeyHash + Version)
                if addr.len() == 32 {
                    let key_hash = jmt::KeyHash::with::<blake3::Hasher>(&addr);
                    let mut versioned_key = [0u8; 40];
                    versioned_key[0..32].copy_from_slice(&key_hash.0);
                    versioned_key[32..40].copy_from_slice(&version.to_be_bytes());
                    batch.put_cf(history_cf, &versioned_key, &state_bytes);
                    
                    // Ghi kèm key hash vào acc_cf để JMT có thể truy cập nhanh
                    batch.put_cf(acc_cf, key_hash.0, &state_bytes);

                    // [BIG-DATA-INDEX] Ghi KeyHash -> Address index
                    if let Some(cf_kh_to_addr) = self.db.cf_handle(CF_KEYHASH_TO_ADDR) {
                        let mut addr_arr = [0u8; 32];
                        addr_arr.copy_from_slice(&addr);
                        batch.put_cf(cf_kh_to_addr, key_hash.0, addr_arr);
                    }
                }
                
                count += 1;
                
                // Chunking WriteBatch mỗi 10.000 tài khoản để giải phóng RAM vật lý ngay lập tức
                if count % 10_000 == 0 {
                    self.db.write_opt(batch, &opts)?;
                    batch = WriteBatch::default(); // Reset batch & giải phóng bộ nhớ RAM
                }
            }
        }
        
        // Flush nốt phần dữ liệu dư còn lại
        self.db.write_opt(batch, &opts)?;
        log::info!("📦 [SNAP-SYNC] Đã commit thành công tổng cộng {} tài khoản vào Sổ cái chính thức.", count);
        
        // Dọn dẹp staging sau khi hoàn tất bằng cách xóa toàn bộ dải dữ liệu
        if let Some(staging_handle) = self.db.cf_handle(CF_ACC_SYNC_STAGING) {
            self.db.delete_range_cf(staging_handle, [0u8; 32], [0xffu8; 32])?;
        }
        
        Ok(())
    }



    pub fn clear_staging_area(&self) -> Result<()> {
        let cf = self.db.cf_handle(CF_ACC_SYNC_STAGING).context("Missing STAGING CF")?;
        // Xóa sạch toàn bộ dữ liệu trong bảng tạm staging
        let mut opts = rocksdb::WriteOptions::default();
        opts.set_sync(true);
        // [VANGUARD-CLEAN-SLATE] Sử dụng cận xóa &[] đến &[0xffu8; 128] để dọn sạch hoàn toàn 100% staging
        let start_all: &[u8] = &[];
        let end_all: &[u8] = &[0xffu8; 128];
        self.db.delete_range_cf(cf, start_all, end_all)?;
        Ok(())
    }
}

pub fn init_global_state(path: &str) -> Result<()> {
    let mgr = StateManager::try_new(path.to_string())?;
    // [DEADLOCK-FIX] Sử dụng OnceLock::set để nạp trạng thái một lần duy nhất mà không cần khóa ghi
    if let Err(_) = GLOBAL_STATE_MANAGER.set(mgr) {
        log::warn!("[StateManager] GLOBAL_STATE_MANAGER đã được khởi tạo trước đó. Bỏ qua khởi tạo lại.");
    }
    Ok(())
}


