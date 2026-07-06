/**
 * @file lib.rs
 * @brief YonaCode V1.0 - Shared Calculation Library (SCL).
 * @details Thư viện lõi quản lý trạng thái, đồng thuận và mật mã.
 * Sử dụng UniFFI để cung cấp ràng buộc an toàn cho Go.
 * 
 * @author Vô Nhật Thiên (Khởi tạo) - YonaCode V1.0
 * @date 2026-03-24
 */


pub mod crypto_primitives;
pub const VERSION: &str = "1.0.1-VANGUARD-FIX";
pub mod reward_logic;
pub mod difficulty_logic;
pub mod fee_logic;
pub mod merkle;
pub mod genz_pow;
pub mod state_manager;
pub mod proto;
pub mod consensus;

#[derive(borsh::BorshSerialize, borsh::BorshDeserialize, serde::Serialize, serde::Deserialize, Clone, Debug, Default)]
pub struct SparseMerkleProof {
    pub leaf: Option<([u8; 32], [u8; 32])>,
    pub siblings: Vec<[u8; 32]>, 
}

macro_rules! get_mgr {
    ($default:expr) => {
        match crate::state_manager::get_state_manager() {
            Some(m) => m,
            None => return $default,
        }
    };
    () => {
        match crate::state_manager::get_state_manager() {
            Some(m) => m,
            None => return Default::default(),
        }
    };
}

use rayon::prelude::*;
use std::collections::{HashMap, HashSet};
use ahash::{AHashMap, AHashSet};
use jmt::{JellyfishMerkleTree, KeyHash};
use rand::seq::SliceRandom;
use prost::Message;
use crate::proto::transaction::Transaction;
use crate::proto::block::{BlockHeader, BlockBody, Block};
use crate::proto::common::*;
use tonic::Status;
use crate::state_manager::{AccountState, StateManager};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::{Serialize, Deserialize};
use primitive_types::U256;

static LOG_SENDER: std::sync::OnceLock<std::sync::mpsc::SyncSender<String>> = std::sync::OnceLock::new();

// Tại sao: Việc ghi log đồng bộ lên màn hình/file làm chậm luồng thực thi giao dịch. 
// Sử dụng luồng chạy ngầm bất đồng bộ giúp giảm thiểu độ trễ I/O.
pub fn log_async(msg: String) {
    let sender = LOG_SENDER.get_or_init(|| {
        let (tx, rx) = std::sync::mpsc::sync_channel::<String>(10000);
        std::thread::spawn(move || {
            for m in rx {
                println!("{}", m);
                log::info!("{}", m);
            }
        });
        tx
    });
    let _ = sender.try_send(msg);
}

// Cấu trúc kết quả thực thi khối
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug)]
pub struct ExecutionResult {
    pub state_root: Vec<u8>,
    pub success: bool,
    pub error_msg: String,
    pub failing_tx_index: i32, // [BITCOIN-STANDARDS] Vị trí giao dịch lỗi trong khối (-1 nếu không có)
}



#[derive(Debug)]
pub struct BlockExecutionError {
    pub message: String,
    pub failing_tx_index: i32, // -1 nếu lỗi không do giao dịch cụ thể (ví dụ: lỗi cung tiền)
}

impl std::fmt::Display for BlockExecutionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{} (TX Index: {})", self.message, self.failing_tx_index)
    }
}

#[derive(BorshSerialize, BorshDeserialize, Clone, Debug)]
pub struct BlockTemplateResult {
    pub block_raw: Vec<u8>,
    pub success: bool,
    pub error_msg: String,
    pub failing_tx_index: i32,
}

/// [VANGUARD-ATOMIC] Dữ liệu thực thi khối chờ chốt sổ (Staging Area)
pub struct ExecutionPayload {
    pub state_batch: rocksdb::WriteBatch,
    pub final_root: [u8; 32],
    pub tx_hashes: Vec<[u8; 32]>,
    pub touched_accs: Vec<[u8; 32]>,
    pub actual_total_supply: u64,
}


#[derive(Clone, Copy, Debug, PartialEq)]
pub enum BlockVerificationResult {
    Success,
    InvalidPoW,
    FirewallViolation,
    DbBusy, // [VANGUARD-FIX] Cơ chế Fail-Closed lỗi tạm thời do RocksDB bận đọc lịch sử DAA
}

#[derive(Clone, Debug, Default)]
pub struct TransactionStatus {
    pub height: u64,
    pub status: u32,
    pub is_finalized: bool,
    pub confirmations: u64,
    pub sender_prev_balance: u64,
    pub sender_post_balance: u64,
    pub receiver_prev_balance: u64,
    pub receiver_post_balance: u64,
}

// [VANGUARD-CONSENSUS] Cấu trúc phản hồi cho Đồng thuận
pub struct EvaluateHeaderChainResponse {
    pub status: u32, // 0 = Heavier, 1 = Lighter, 2 = Invalid
    pub fork_point: u64,
    pub error_msg: String,
}

pub struct ProcessNewBlockResponse {
    pub status: u32, // 0 = Accepted, 1 = Rejected, 2 = Orphan
    pub error_msg: String,
}

#[derive(BorshSerialize, BorshDeserialize, Serialize, Deserialize, Clone, Debug, Default)]
pub struct AccountSnapshot {
    pub address: Vec<u8>,
    pub balance: u64,
    pub nonce: u64,
    pub nano_weight: u32,
    pub last_full_cleanup: u64,
    pub coin_id: Vec<u8>,
    pub maturing_rewards: Vec<crate::state_manager::MaturingReward>,
}

/// [VANGUARD-CONSENSUS] FFI Mới cho Đồng thuận tập trung tại Rust
pub fn evaluate_header_chain(headers_raw: Vec<Vec<u8>>) -> EvaluateHeaderChainResponse {
    use crate::proto::block::BlockHeader;
    use prost::Message;
    use crate::difficulty_logic::{LWMA_WINDOW, calculate_next_difficulty};
    use primitive_types::U256;

    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "No StateManager".into() }
    };
    
    if headers_raw.is_empty() {
         return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "Danh sách rỗng".into() };
    }
    
    let mut headers = Vec::new();
    for raw in headers_raw {
        match BlockHeader::decode(&raw[..]) {
            Ok(h) => headers.push(h),
            Err(_) => return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "Giải mã header thất bại".into() }
        }
    }
    
    // Đảm bảo thứ tự tăng dần
    headers.sort_by_key(|h| h.height);
    
    let first_header = &headers[0];
    let is_bootstrap = first_header.height == 0;
    
    let mut history_ts = Vec::new();
    let mut history_diffs = Vec::new();
    let mut current_weight = U256::zero();
    let mut fork_point = 0;

    if is_bootstrap {
        // [BOOTSTRAP MODE] Bắt đầu từ Genesis
        // [VANGUARD-UNITY] Luôn sử dụng Vanguard cho khối Genesis.
        let packed_genesis = crate::genz_pow::pack_header_v112(
            0,
            &vec![0u8; 32],
            first_header.timestamp,
            &first_header.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default(),
            &first_header.difficulty
        );
        let genesis_hash = crate::crypto_primitives::calculate_blake3_hash(packed_genesis.to_vec(), 0);
        
        // [VANGUARD-UNITY] Tự động chấp nhận Genesis đầu tiên nếu GENESIS_HASH là zero (Chế độ Re-Genesis)
        if crate::crypto_primitives::GENESIS_HASH != [0u8; 32] && genesis_hash != crate::crypto_primitives::GENESIS_HASH {
             log::error!("[CONSENSUS-ALERT] 🚨 GENESIS HASH MISMATCH!");
             return EvaluateHeaderChainResponse { 
                 status: 2, 
                 fork_point: 0, 
                 error_msg: format!("Genesis Hash mismatch! Expected {}, got {}", hex::encode(crate::crypto_primitives::GENESIS_HASH), hex::encode(genesis_hash)) 
             };
        }
        fork_point = 0;
    } else {
        // [REORG MODE] Tìm điểm rẽ nhánh trong DB
        let parent_hash = first_header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default();
        let mut parent_hash_arr = [0u8; 32];
        if parent_hash.len() == 32 {
            parent_hash_arr.copy_from_slice(&parent_hash);
        } else {
            return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "Parent hash không hợp lệ".into() };
        }

        let stored_parent_header_raw = mgr.get_header_raw(&parent_hash_arr);
        if stored_parent_header_raw.is_none() {
            let parent_height = first_header.height - 1;
            let finalized_h = mgr.get_finalized_height();
            // Đã sửa thành `<`: Cho phép rẽ nhánh có chung gốc chính xác tại khối Finalized
            if parent_height < finalized_h { 
                log::error!("[CONSENSUS-SECURITY] 🚨 Điểm rẽ nhánh của chuỗi tại cao độ #{} vi phạm Tường lửa Bất biến (Nằm sâu dưới vùng chốt #{})!", parent_height, finalized_h);
                return EvaluateHeaderChainResponse {
                    status: 2,
                    fork_point: 0,
                    error_msg: format!("ERR_IMMUTABLE_FIREWALL_VIOLATION: Điểm rẽ nhánh tại #{} nằm sâu dưới mốc bất biến #{}!", parent_height, finalized_h),
                };
            }
            return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "Không tìm thấy điểm rẽ nhánh trong DB".into() };
        }
        
        let stored_raw = stored_parent_header_raw.unwrap();
        if stored_raw.is_empty() || stored_raw.iter().all(|&b| b == 0) {
            log::error!("[CONSENSUS-ERROR] ❌ Dữ liệu Header tại điểm rẽ nhánh rỗng hoặc toàn số 0!");
            return EvaluateHeaderChainResponse { status: 4, fork_point: 0, error_msg: "Local DB corrupted: Empty/Zero header in DB at fork point".into() };
        }

        let stored_parent_header = match BlockHeader::decode(&stored_raw[..]) {
            Ok(h) => h,
            Err(e) => {
                log::error!("[CONSENSUS-ERROR] ❌ Lỗi giải mã Header cha trong DB: {}", e);
                return EvaluateHeaderChainResponse { status: 4, fork_point: 0, error_msg: format!("Local DB corrupted: Failed to decode parent header: {}", e) };
            }
        };
        // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
        let mut weight_padded = [0u8; 32];
        let w_len = stored_parent_header.absolute_weight.len().min(32);
        if w_len > 0 {
            weight_padded[..w_len].copy_from_slice(&stored_parent_header.absolute_weight[..w_len]);
        }
        current_weight = U256::from_little_endian(&weight_padded);
        fork_point = first_header.height - 1;

        // Tải cửa sổ LWMA từ DB (Cần n+1 timestamps và n difficulties cho khối h.height)
        let max_n = 120u64;
        let start_h = first_header.height.saturating_sub(max_n + 1); 
        for h_idx in start_h..first_header.height {
            if let Some(raw) = mgr.get_block_hash(h_idx).and_then(|hash| mgr.get_header_raw(&hash)) {
                if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                    history_ts.push(hdr.timestamp);
                    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
                    let mut diff_padded = [0u8; 32];
                    let d_len = hdr.difficulty.len().min(32);
                    if d_len > 0 {
                        diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                    }
                    history_diffs.push(U256::from_little_endian(&diff_padded));
                }
            }
        }

        // [VÁ LỖI LWMA FRAGMENTATION]
        // Nếu chiều cao đủ lớn để có đủ lịch sử nhưng DB thiếu
        let first_n = 120u64;
        let expected_history_len = first_n + 1;
        if first_header.height >= expected_history_len && history_ts.len() < expected_history_len as usize {
            log::error!("[CONSENSUS-DB] 🛑 DB cục bộ bị thiếu dữ liệu lịch sử! Yêu cầu {} khối từ #{} đến #{} để tính LWMA, chỉ tìm thấy {}.",
                expected_history_len, start_h, first_header.height - 1, history_ts.len());
            return EvaluateHeaderChainResponse {
                status: 4, // Lỗi nội bộ, Go sẽ không ban Peer
                fork_point: 0,
                error_msg: "Local DB corrupted: Missing history blocks to calculate DAA/LWMA".into(),
            };
        }
    }

    let finalized_h = mgr.get_finalized_height();

    // [VERIFICATION LOOP] Kiểm tra từng Header một
    for (i, h) in headers.iter().enumerate() {
        if i > 0 {
            // 1. Kiểm tra tính liên tục của chiều cao
            if h.height != headers[i-1].height + 1 {
                 return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: "Chuỗi header không liên tục".into() };
            }

            // 1b. [SECURITY-HARDENING] Kiểm tra liên kết mã băm liên tục (Chain Hash Pointer Continuity)
            let prev_parent_h = headers[i-1].parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
            let prev_tx_r = headers[i-1].tx_root.as_ref().map(|tr| tr.value.clone()).unwrap_or_default();
            let prev_packed = crate::genz_pow::pack_header_v112(headers[i-1].height, &prev_parent_h, headers[i-1].timestamp, &prev_tx_r, &headers[i-1].difficulty);
            let prev_header_hash = crate::crypto_primitives::calculate_blake3_hash(prev_packed.to_vec(), headers[i-1].height);

            let current_parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
            if current_parent_h != prev_header_hash {
                 log::error!("[CONSENSUS-SECURITY] 🚨 Đứt gãy liên kết mã băm cha-con giữa #{} và #{}!", headers[i-1].height, h.height);
                 return EvaluateHeaderChainResponse { 
                     status: 2, 
                     fork_point: 0, 
                     error_msg: format!("Đứt gãy liên kết mã băm cha-con giữa #{} và #{}!", headers[i-1].height, h.height) 
                 };
            }
        }

        // [VANGUARD-CONSENSUS-OPTIMIZATION] Kiểm tra xem khối có trùng khớp hoàn toàn trên chuỗi chính hiện tại không
        let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        let tx_r = h.tx_root.as_ref().map(|tr| tr.value.clone()).unwrap_or_default();
        let packed = crate::genz_pow::pack_header_v112(h.height, &parent_h, h.timestamp, &tx_r, &h.difficulty);
        let header_hash = crate::crypto_primitives::calculate_blake3_hash(packed.to_vec(), h.height);

        let mut is_matched = false;
        if let Some(stored_hash) = mgr.get_block_hash(h.height) {
            if stored_hash == header_hash {
                is_matched = true;
            } else if h.height <= finalized_h {
                // [ANTI-BYPASS] Trực tiếp chặn đứng nỗ lực thay đổi chuỗi tại hoặc trước finalized height
                log::error!("[CONSENSUS-SECURITY] 🚨 Phát hiện chuỗi rẽ nhánh vi phạm Tường lửa Bất biến tại #{}!", h.height);
                return EvaluateHeaderChainResponse {
                    status: 2,
                    fork_point: 0,
                    error_msg: format!("ERR_IMMUTABLE_FIREWALL_VIOLATION: Chuỗi header tại cao độ #{} vi phạm khối đã finalize!", h.height),
                };
            }
        }

        if is_matched {
            // Khối này hoàn toàn trùng khớp với DB, tự động cập nhật fork_point lên cao nhất có thể (Highest Common Ancestor)
            fork_point = h.height;
            if let Some(stored_raw) = mgr.get_header_raw(&header_hash) {
                if let Ok(stored_h) = BlockHeader::decode(&stored_raw[..]) {
                    let mut weight_padded = [0u8; 32];
                    let w_len = stored_h.absolute_weight.len().min(32);
                    if w_len > 0 {
                        weight_padded[..w_len].copy_from_slice(&stored_h.absolute_weight[..w_len]);
                    }
                    current_weight = U256::from_little_endian(&weight_padded);
                }
            }
            
            // Cập nhật cửa sổ lịch sử cho khối trùng lặp này
            history_ts.push(h.timestamp);
            let mut diff_padded = [0u8; 32];
            let d_len = h.difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
            }
            history_diffs.push(U256::from_little_endian(&diff_padded));
            let next_n = 120;
            if history_ts.len() > next_n + 1 {
                let excess = history_ts.len() - (next_n + 1);
                history_ts.drain(0..excess);
            }
            if history_diffs.len() > next_n {
                let excess = history_diffs.len() - next_n;
                history_diffs.drain(0..excess);
            }
            continue; // Bỏ qua verify PoW/LWMA vì khối đã được chốt an sau trong DB
        }

        let needed_n = 120u64;
        if h.height >= needed_n + 1 && (history_ts.len() as u64) < needed_n + 1 {
            history_ts.clear();
            history_diffs.clear();
            let start_h = h.height - needed_n - 1;
            for h_idx in start_h..h.height {
                let mut found_hdr = None;
                if h_idx >= first_header.height {
                    let offset = (h_idx - first_header.height) as usize;
                    if offset < headers.len() {
                        found_hdr = Some(headers[offset].clone());
                    }
                }
                
                if found_hdr.is_none() {
                    if let Some(hash) = mgr.get_block_hash(h_idx) {
                        if let Some(raw) = mgr.get_header_raw(&hash) {
                            if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                                found_hdr = Some(hdr);
                            }
                        }
                    }
                }

                if let Some(hdr) = found_hdr {
                    history_ts.push(hdr.timestamp);
                    if h_idx > start_h {
                        let mut diff_padded = [0u8; 32];
                        let d_len = hdr.difficulty.len().min(32);
                        if d_len > 0 {
                            diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                        }
                        history_diffs.push(U256::from_little_endian(&diff_padded));
                    }
                }
            }
        }

        if !history_ts.is_empty() {
            // [HOTFIX V1.7.1] Đồng bộ hóa Height: calculate_next_difficulty cần height của khối đang được kiểm tra.
            // Trước đây dùng h.height - 1 gây lệch chu kỳ 5-block (Adjustment Cycle) giữa Miner và Kernel.
            let expected_diff = calculate_next_difficulty(&history_ts, &history_diffs, h.timestamp, h.height);
            
            // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
            let mut diff_padded = [0u8; 32];
            let d_len = h.difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
            }
            let actual_diff = U256::from_little_endian(&diff_padded);
            
            if actual_diff != expected_diff {
                 log::error!("[DIFF-MISMATCH] 🚨 Sai lệch độ khó tại #{}!", h.height);
                 log::info!("   - Mong đợi (calc): {}", expected_diff);
                 log::info!("   - Thực tế (block): {}", actual_diff);
                 log::info!("   - History TS Count: {}", history_ts.len());
                 log::info!("   - History Diff Count: {}", history_diffs.len());
                 return EvaluateHeaderChainResponse { 
                     status: 2, 
                     fork_point: 0, 
                     error_msg: format!("Sai lệch độ khó tại #{}! Kẻ tấn công phát hiện! (Yêu cầu: {}, Gửi: {})", h.height, expected_diff, actual_diff) 
                 };
            }
        }

        // 3. Xác thực PoW (Energy Check)
        let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        let tx_r = h.tx_root.as_ref().map(|tr| tr.value.clone()).unwrap_or_default();
        let packed = crate::genz_pow::pack_header_v112(h.height, &parent_h, h.timestamp, &tx_r, &h.difficulty);
        let header_hash = crate::crypto_primitives::calculate_blake3_hash(packed.to_vec(), h.height);
        
        if !crate::genz_pow::verify_pow_raw(header_hash.to_vec(), h.nonce, h.difficulty.clone(), h.height) {
            return EvaluateHeaderChainResponse { status: 2, fork_point: 0, error_msg: format!("Invalid PoW at height {}", h.height) };
        }

        // 3b. [CONSENSUS-SECURITY] MTP-11 Firewall Protection (Time-Warp Attack Shield)
        // Ngăn chặn Hacker thao túng thuật toán độ khó bằng cách giả mạo Timestamp của Headers.
        let mtp = if h.height == 0 {
            0
        } else {
            let count = history_ts.len().min(11);
            if count == 0 {
                0
            } else {
                let mut ts_subset = history_ts[history_ts.len() - count..].to_vec();
                ts_subset.sort();
                ts_subset[ts_subset.len() / 2]
            }
        };

        let current_now = chrono::Utc::now().timestamp() as u64;
        if !crate::verify_timestamp_firewall(h.timestamp, mtp, current_now, false) {
            log::error!("[CONSENSUS-SECURITY] 🚨 Header #{} vi phạm Tường lửa MTP-11! (TS: {}, MTP: {})", h.height, h.timestamp, mtp);
            return EvaluateHeaderChainResponse { 
                status: 2, 
                fork_point: 0, 
                error_msg: format!("VI PHẠM TƯỜNG LỬA THỜI GIAN (MTP-11 Violation tại #{})", h.height) 
            };
        }

        // 4. Cập nhật cửa sổ lịch sử
        history_ts.push(h.timestamp);
        
        // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
        let mut diff_padded = [0u8; 32];
        let d_len = h.difficulty.len().min(32);
        if d_len > 0 {
            diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
        }
        let diff_u256 = U256::from_little_endian(&diff_padded);
        
        history_diffs.push(diff_u256);
        let next_n = 120;
        if history_ts.len() > next_n + 1 {
            let excess = history_ts.len() - (next_n + 1);
            history_ts.drain(0..excess);
        }
        if history_diffs.len() > next_n {
            let excess = history_diffs.len() - next_n;
            history_diffs.drain(0..excess);
        }
        
        current_weight = current_weight + diff_u256;
    }

    // [FINAL EVALUATION] So sánh trọng số với chuỗi hiện tại (Chỉ khi không phải Bootstrap)
    if is_bootstrap {
        EvaluateHeaderChainResponse { status: 0, fork_point: 0, error_msg: "".into() }
    } else {
        let current_tip_height = mgr.get_current_version();
        if let Some(tip_hash) = mgr.get_block_hash(current_tip_height) {
            if let Some(tip_raw) = mgr.get_header_raw(&tip_hash) {
                if let Ok(tip_h) = BlockHeader::decode(&tip_raw[..]) {
                    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
                    let mut weight_padded = [0u8; 32];
                    let w_len = tip_h.absolute_weight.len().min(32);
                    if w_len > 0 {
                        weight_padded[..w_len].copy_from_slice(&tip_h.absolute_weight[..w_len]);
                    }
                    let current_tip_weight = U256::from_little_endian(&weight_padded);
                    if current_weight > current_tip_weight {
                        return EvaluateHeaderChainResponse { status: 0, fork_point, error_msg: "".into() };
                    }
                }
            }
        }
        EvaluateHeaderChainResponse { status: 1, fork_point, error_msg: "Nhẹ hơn hoặc bằng".into() }
    }
}

pub fn process_new_block(block_raw: Vec<u8>) -> ProcessNewBlockResponse {
    use crate::proto::block::Block;
    use prost::Message;

    let block = match Block::decode(&block_raw[..]) {
        Ok(b) => b,
        Err(_) => return ProcessNewBlockResponse { status: 1, error_msg: "Giải mã thất bại".into() }
    };
    
    let header = match block.header.as_ref() {
        Some(h) => h,
        None => return ProcessNewBlockResponse { status: 1, error_msg: "Thiếu Header".into() }
    };
    
    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => return ProcessNewBlockResponse { status: 1, error_msg: "StateManager chưa khởi tạo".into() }
    };
    
    let parent_hash = header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default();
    let mut parent_hash_arr = [0u8; 32];
    if parent_hash.len() == 32 {
        parent_hash_arr.copy_from_slice(&parent_hash);
    }
    
    if header.height > 0 {
        let stored_parent = mgr.get_block_hash(header.height - 1);
        if stored_parent.is_none() {
            return ProcessNewBlockResponse { status: 2, error_msg: "Orphan block".into() };
        }
        let stored_parent_hash = stored_parent.unwrap();
        if stored_parent_hash != parent_hash_arr {
            let s_hex = hex::encode(&stored_parent_hash);
            let p_hex = hex::encode(&parent_hash_arr);
            let err_msg = format!("Side-chain block. Cần phân giải rẽ nhánh. Stored: {}, Parent: {}", s_hex, p_hex);
            return ProcessNewBlockResponse { status: 2, error_msg: err_msg };
        }
        
        let current_tip = mgr.get_current_version();
        if (header.height - 1) != current_tip {
            return ProcessNewBlockResponse { status: 2, error_msg: "Chiều cao khối không nối tiếp đỉnh hiện tại. Cần phân giải.".into() };
        }
    } else {
        let current_tip = mgr.get_current_version();
        if current_tip > 0 {
             return ProcessNewBlockResponse { status: 1, error_msg: "Khối Genesis đã tồn tại.".into() };
        }
    }
    
    let header_bytes = header.encode_to_vec();
    let parent_h = header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default();
    let tx_r = header.tx_root.as_ref().map(|h| h.value.clone()).unwrap_or_default();
    
    let packed_header = crate::genz_pow::pack_header_v112(
        header.height,
        &parent_h,
        header.timestamp,
        &tx_r,
        &header.difficulty
    );
    
    let header_hash_32 = crate::crypto_primitives::calculate_blake3_hash(packed_header.to_vec(), header.height);
    
    // [VANGUARD-FIX] verify_pow_raw yêu cầu HEADER HASH (32 bytes).
    if !crate::genz_pow::verify_pow_raw(header_hash_32.to_vec(), header.nonce, header.difficulty.clone(), header.height) {
        return ProcessNewBlockResponse { status: 1, error_msg: "Invalid PoW".into() };
    }

    // [VANGUARD-CONSENSUS-DIFF-CHECK] Thẩm định độ khó (LWMA DAA)
    if header.height > 0 {
        let n = 120u64;
        let mut history_ts = Vec::new();
        let mut history_diffs = Vec::new();
        let start_h = header.height.saturating_sub(n + 1);
        
        for h_idx in start_h..header.height {
            if let Some(hash) = mgr.get_block_hash(h_idx) {
                if let Some(raw) = mgr.get_header_raw(&hash) {
                    if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                        history_ts.push(hdr.timestamp);
                        let mut diff_padded = [0u8; 32];
                        let d_len = hdr.difficulty.len().min(32);
                        if d_len > 0 {
                            diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                        }
                        history_diffs.push(U256::from_little_endian(&diff_padded));
                    }
                }
            }
        }
        let expected_history_len = header.height.min(n + 1) as usize;
        if history_ts.len() < expected_history_len {
            log::error!("[CONSENSUS-SECURITY] 🚨 DB cục bộ thiếu hoặc bận đọc lịch sử DAA tại #{}! Yêu cầu: {}, Tìm thấy: {}", header.height, expected_history_len, history_ts.len());
            return ProcessNewBlockResponse { 
                status: 1, 
                error_msg: "Local DB busy or corrupted: Missing history blocks to calculate DAA/LWMA".into() 
            };
        }

        if !history_ts.is_empty() {
            let expected_diff = crate::difficulty_logic::calculate_next_difficulty(&history_ts, &history_diffs, header.timestamp, header.height);
            let mut diff_padded = [0u8; 32];
            let d_len = header.difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&header.difficulty[..d_len]);
            }
            let actual_diff = U256::from_little_endian(&diff_padded);
            
            if actual_diff != expected_diff {
                log::error!("[CONSENSUS-SECURITY] 🚨 Sai lệch độ khó LWMA tại khối #{}! Mong đợi: {}, Thực tế: {}", header.height, expected_diff, actual_diff);
                return ProcessNewBlockResponse { 
                    status: 1, 
                    error_msg: format!("Sai lệch độ khó LWMA! Mong đợi: {}, Thực tế: {}", expected_diff, actual_diff) 
                };
            }
        }
    }

    // [VANGUARD-CONSENSUS] MTP-11 Firewall Protection (Time-Warp Attack Shield)
    // Ngăn chặn Hacker thao túng thuật toán độ khó bằng cách giả mạo Timestamp.
    let mtp = mgr.get_median_time_past(header.height);
    let block_timestamp = chrono::Utc::now().timestamp() as u64; // Fallback to current wall time
    let current_now = block_timestamp;

    if !crate::verify_timestamp_firewall(header.timestamp, mtp, current_now, false) {
        log::error!("[CONSENSUS-SECURITY] 🚨 Khối #{} vi phạm Tường lửa MTP-11! (TS: {}, MTP: {})", header.height, header.timestamp, mtp);
        return ProcessNewBlockResponse { status: 1, error_msg: "VI PHẠM TƯỜNG LỬA THỜI GIAN (MTP-11 Violation)".into() };
    }
    
    let body_bytes = block.body.as_ref().map(|b| b.encode_to_vec()).unwrap_or_default();
    let miner_addr = header.miner_address.as_ref().map(|a| a.value.clone()).unwrap_or_default();
    
    let exec_res = execute_block_transactions(body_bytes, miner_addr, parent_h.clone(), header.height, false);
    
    if !exec_res.success {
        return ProcessNewBlockResponse { status: 1, error_msg: exec_res.error_msg };
    }

    // [VANGUARD-FIX] Tính trọng lượng tích lũy tự tính (Self-computed weight)
    let mut parent_weight = U256::from(0);
    if header.height > 0 {
        if let Some(p_hash) = mgr.get_block_hash(header.height - 1) {
            if let Some(p_header_raw) = mgr.get_header_raw(&p_hash) {
                if let Ok(p_header) = BlockHeader::decode(&p_header_raw[..]) {
                    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
                    let mut weight_padded = [0u8; 32];
                    let w_len = p_header.absolute_weight.len().min(32);
                    if w_len > 0 {
                        weight_padded[..w_len].copy_from_slice(&p_header.absolute_weight[..w_len]);
                    }
                    parent_weight = U256::from_little_endian(&weight_padded);
                }
            }
        }
    }

    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation cho Difficulty
    let mut diff_padded = [0u8; 32];
    let d_len = header.difficulty.len().min(32);
    if d_len > 0 {
        diff_padded[..d_len].copy_from_slice(&header.difficulty[..d_len]);
    }
    let diff_u256 = U256::from_little_endian(&diff_padded);
    let current_weight = parent_weight + diff_u256;
    let mut weight_bytes = [0u8; 32];
    current_weight.to_little_endian(&mut weight_bytes);
    
    crate::save_block_raw(header.height, header_hash_32.to_vec(), block_raw, true, weight_bytes.to_vec());
    crate::commit_block_hash(header.height, header_hash_32.to_vec());
    
    ProcessNewBlockResponse { status: 0, error_msg: "".into() }
}

/// [V1.0 FINAL] Thực thi toàn bộ giao dịch trong một khối (Zero Simulation)

pub fn execute_block_transactions(
    block_body_raw: Vec<u8>,
    miner_address: Vec<u8>,
    expected_parent_hash: Vec<u8>,
    height: u64,
    is_simulation: bool,
) -> ExecutionResult {
    match execute_block_internal(block_body_raw, miner_address, expected_parent_hash, height, is_simulation) {
        Ok((payload, _, _)) => {
            if !is_simulation {
                // CHỐT SỔ (Legacy Mode - Sử dụng commit riêng lẻ nếu không gọi qua Reorg Atomic)
                let mgr = state_manager::get_state_manager().unwrap();
                mgr.db.write(payload.state_batch).expect("Legacy commit failed");
                mgr.current_version.store(height, std::sync::atomic::Ordering::SeqCst);
                mgr.actual_total_supply.store(payload.actual_total_supply, std::sync::atomic::Ordering::SeqCst);
            }
            ExecutionResult {
                state_root: payload.final_root.to_vec(),
                success: true,
                error_msg: String::new(),
                failing_tx_index: -1,
            }
        },
        Err(e) => ExecutionResult {
            state_root: vec![0u8; 32],
            success: false,
            error_msg: e.message,
            failing_tx_index: e.failing_tx_index,
        }
    }
}

pub fn execute_block_internal(
    block_body_raw: Vec<u8>,
    miner_address: Vec<u8>,
    expected_parent_hash: Vec<u8>,
    height: u64,
    is_simulation: bool,
) -> Result<(ExecutionPayload, Vec<u8>, Vec<Transaction>), BlockExecutionError> {
    log::info!("[SCL-EXEC-V3] 🚀 Thực thi Khối #{} (Chế độ: {})", height, if is_simulation { "MÔ PHỎNG" } else { "GHI THỰC" });

    // [VANGUARD-RECEIPT-BATCH] Khai báo danh sách tạm thời thu thập toàn bộ biên lai giao dịch thành công trong khối
    let mut block_receipts: Vec<state_manager::TrackedTx> = Vec::new();

    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => {
            log::error!("[SCL-ERROR] ❌ StateManager chưa khởi tạo khi gọi execute_block_internal!");
            return Err(BlockExecutionError { message: "StateManager not initialized".to_string(), failing_tx_index: -1 });
        }
    };


    // [Audit S#3 FIX] Chống Fork & Nhầm lẫn Trạng thái: Kiểm tra Parent Hash
    if height > 0 {
        if expected_parent_hash.len() != 32 {
            println!("[DEBUG-SCL] Expected parent hash length invalid: {}", expected_parent_hash.len());
            return Err(BlockExecutionError { message: "Invalid Parent Hash Length".to_string(), failing_tx_index: -1 });
        }

        let mut p_hash_arr = [0u8; 32];
        p_hash_arr.copy_from_slice(&expected_parent_hash);
        
        let stored = mgr.get_block_hash(height - 1);
        if stored.is_none() {
            log::warn!("[DEBUG-SCL] Parent Block #{} NOT FOUND in DB", height - 1);
            return Err(BlockExecutionError { message: format!("Parent Block #{} not found", height - 1), failing_tx_index: -1 });
        }


        match stored {
            Some(stored_hash) if stored_hash == p_hash_arr => {},
            Some(stored_hash) => {
                println!("[DEBUG-SCL] Parent Hash Mismatch at #{}: Expected {}, Stored {}", height, hex::encode(p_hash_arr), hex::encode(stored_hash));
                return Err(BlockExecutionError { message: "Parent Hash Mismatch".to_string(), failing_tx_index: -1 });
            },
            _ => unreachable!()
        }

    }

    // 1. Giải mã BlockBody
    let mut body = match BlockBody::decode(&block_body_raw[..]) {
        Ok(b) => b,
        Err(e) => {
            println!("[DEBUG-SCL] BlockBody decoding failed: {:?}", e);
            return Err(BlockExecutionError { message: "Invalid Block Body".to_string(), failing_tx_index: -1 });
        }
    };

    // println!("[SCL-EXEC] 📦 Giải mã Body thành công. Số TX: {}", body.transactions.len());

    // [CHECKPOINT V1.0] Mã băm khối thực tế
    let block_hash = crypto_primitives::calculate_blake3_hash_ffi(block_body_raw.clone(), height);
    let block_hash_hex = hex::encode(&block_hash);
    // println!("[SCL-EXEC] 🧩 BlockHash: {}", block_hash_hex);

    
    let miner = if miner_address.len() == 32 {
        let mut addr = [0u8; 32];
        addr.copy_from_slice(&miner_address);
        addr
    } else {
        [0u8; 32]
    };

    let mut total_fees: u64 = 0;
    let mut tx_hashes = Vec::with_capacity(body.transactions.len());
    let mut touched_accs = std::collections::HashSet::new(); // [AUDIT V11.5 FIX] Thu thập acc bị tác động
    
    // [EISD HARDENING V2.0] Bộ đếm Nhân quả Tài chính (Conservation Law Counters)
    // Tại sao: Phép kiểm toán cung tiền cũ (dòng ~296) dùng 2 cache cộng dồn,
    // gần như Tautology khi height >= 100. Bộ đếm này kiểm tra ở mức GIAO DỊCH:
    // Tổng tiền rút khỏi sender PHẢI == Tổng tiền nạp vào receiver + Tổng phí.
    // Nếu sai → tiền bị tạo/hủy trái phép → từ chối khối.
    let mut audit_total_deducted: u64 = 0;  // Σ(amount + fee) rút khỏi sender
    let mut audit_total_credited: u64 = 0;  // Σ(amount) nạp vào receiver
    
    // [EISD FIX] State Cache: Chống Double-Spend trong cùng một khối
    let mut state_cache: AHashMap<[u8; 32], AccountState> = AHashMap::default();

    // [VANGUARD-PREFETCH] Tải trước trạng thái tài khoản song song (Parallel State Prefetching)
    // Tại sao: Việc truy xuất RocksDB/JMT tuần tự cho từng giao dịch sẽ tạo ra cổ chai I/O nghiêm trọng
    // dưới tải cao (lên tới 140.000 giao dịch). Bằng cách thu thập tất cả các địa chỉ bị tác động và 
    // tải trạng thái song song qua Rayon, ta tận dụng tối đa băng thông đọc của RocksDB và đa nhân CPU.
    let mut touched_addresses: AHashSet<[u8; 32]> = AHashSet::default();
    if miner != [0u8; 32] {
        touched_addresses.insert(miner);
    }
    for tx in &body.transactions {
        if let Some(s) = &tx.sender {
            if s.value.len() == 32 {
                let mut addr = [0u8; 32];
                addr.copy_from_slice(&s.value);
                touched_addresses.insert(addr);
            }
        }
        if let Some(r) = &tx.receiver {
            if r.value.len() == 32 {
                let mut addr = [0u8; 32];
                addr.copy_from_slice(&r.value);
                touched_addresses.insert(addr);
            }
        }
    }

    let touched_addresses_vec: Vec<[u8; 32]> = touched_addresses.into_iter().collect();
    // Tại sao thiết kế như vậy: Sử dụng par_iter() (đa luồng) để song song hóa việc prefetch trạng thái tài khoản từ RocksDB/JMT.
    // Việc đọc song song giúp giảm thiểu thời gian trễ đọc đĩa (I/O latency) khi gặp cold read (hàng chục ngàn địa chỉ)
    // tốt hơn rất nhiều so với đọc tuần tự đơn luồng (iter) vốn làm nghẽn luồng xử lý khối.
    let prefetched_states: Vec<([u8; 32], AccountState)> = touched_addresses_vec
        .par_iter()
        .map(|addr| {
            let state = mgr.get_account_state(addr);
            (*addr, state)
        })
        .collect();

    state_cache.extend(prefetched_states.into_iter());
    
    if miner != [0u8; 32] { 
        touched_accs.insert(miner); 
    }

    // [VANGUARD-FIX] Trích xuất timestamp từ Coinbase hoặc Header thực tế của khối hiện tại trong DB
    let block_timestamp = if !body.transactions.is_empty() {
        body.transactions[0].timestamp
    } else if let Some(hash) = mgr.get_block_hash(height) {
        if let Some(header_raw) = mgr.get_header_raw(&hash) {
            if let Ok(header) = BlockHeader::decode(&header_raw[..]) {
                header.timestamp
            } else { chrono::Utc::now().timestamp() as u64 }
        } else { chrono::Utc::now().timestamp() as u64 }
    } else { chrono::Utc::now().timestamp() as u64 };

    let mut tx_hashes_clone = Vec::with_capacity(body.transactions.len());

    // [V37.8 OPTIMIZATION] Nạp 100 Block Hash gần nhất vào RAM MỘT LẦN DUY NHẤT để chống Replay Attack hiệu quả (O(1) lookup)
    let mut recent_hashes = std::collections::HashSet::new();
    let start_h = height.saturating_sub(100);
    for h in start_h..height {
        if let Some(h_val) = mgr.get_block_hash(h) {
            recent_hashes.insert(h_val);
        }
    }

    // [VANGUARD-MATURATION V13.9] Thu thập các địa chỉ tiềm năng để giải phóng tiền thưởng
    let mut touched_for_maturation = Vec::new();
    touched_for_maturation.push(miner);
    for tx in &body.transactions {
        if let Some(s) = &tx.sender {
            let mut s_addr = [0u8; 32]; s_addr.copy_from_slice(&s.value);
            touched_for_maturation.push(s_addr);
        }
        if let Some(r) = &tx.receiver {
            let mut r_addr = [0u8; 32]; r_addr.copy_from_slice(&r.value);
            touched_for_maturation.push(r_addr);
        }
    }

    // --- BƯỚC 0: ĐÁO HẠN PHẦN THƯỞNG TỰ ĐỘNG (STATE-INTEGRATED MATURATION) ---
    // [VANGUARD-MATURATION-V3] Tự động hóa hoàn toàn: Logic đáo hạn đã được tích hợp vào vòng lặp 
    // touched_accs ở cuối hàm để đảm bảo tính nguyên tử và tự động Rollback theo JMT.



    // [VANGUARD-HYBRID] PHA 1: XÁC THỰC MẬT MÃ SONG SONG (CPU-CORE OPTIMIZATION)
    // Tại sao: Xác thực chữ ký Ed25519 là thao tác nặng nhất. 
    // Bằng cách chạy song song, ta có thể xử lý 35MB (~140k TX) trong < 1s trên CPU đa nhân.
    let pre_validated: Vec<Result<([u8; 32], [u8; 32], [u8; 32]), String>> = body.transactions.par_iter().enumerate().map(|(idx, tx)| {
        // [VANGUARD-SECURITY-FIX] Phân định ranh giới an ninh cứng giữa Coinbase (Index 0) và giao dịch thường (Index > 0)
        // Bắt buộc thực hiện ở mọi chế độ chạy (bao gồm cả mô phỏng đúc khối) để ngăn chặn EISD mismatch.
        if idx == 0 {
            if tx.amount > 0 {
                return Err(format!("Coinbase transaction (Index 0) must have amount == 0 at TX #{}", idx));
            }
        } else {
            if tx.amount == 0 {
                return Err(format!("Regular transaction (Index > 0) must have amount > 0 at TX #{}", idx));
            }
        }

        let sender_bytes = tx.sender.as_ref().map(|s| s.value.clone()).unwrap_or_default();
        let receiver_bytes = tx.receiver.as_ref().map(|s| s.value.clone()).unwrap_or_default();

        // 1. Kiểm tra định dạng địa chỉ
        if (tx.amount > 0 && sender_bytes.len() != 32) || receiver_bytes.len() != 32 {
            return Err(format!("Invalid Address Length at TX #{}", idx));
        }

        let (mut s_addr, mut r_addr) = ([0u8; 32], [0u8; 32]);
        if sender_bytes.len() == 32 { s_addr.copy_from_slice(&sender_bytes); }
        if receiver_bytes.len() == 32 { r_addr.copy_from_slice(&receiver_bytes); }

        // 2. Tính toán Signing Hash (TxID chuẩn) - Không băm full hash
        let tx_signing_hash = crypto_primitives::calculate_signing_hash(tx);

        // 3. Xác thực chữ ký
        // CHỈ XÁC THỰC CHỮ KÝ NẾU NHẬN TỪ MẠNG (is_simulation = false)
        // Nếu là mô phỏng (tạo khối), Go đã verify rồi -> Tin tưởng tuyệt đối, bypass để tiết kiệm CPU
        if !is_simulation {
            if let Some(sig) = &tx.signature {
                if sig.value.len() != 64 {
                    return Err(format!("Invalid Signature Length at TX #{}", idx));
                }
                let mut sig_bytes = [0u8; 64];
                sig_bytes.copy_from_slice(&sig.value);
                
                if !crypto_primitives::verify_ed25519_signature(&s_addr, &tx_signing_hash, &sig_bytes) {
                    return Err(format!("Invalid Signature at TX #{}", idx));
                }
            } else if tx.amount > 0 {
                return Err(format!("Missing Signature at TX #{}", idx));
            }
        }

        Ok((s_addr, r_addr, tx_signing_hash))
    }).collect();

    // PHA 2: KẾ TOÁN TUẦN TỰ (SEQUENTIAL STATE UPDATES)
    let mut valid_transactions = Vec::new();

    for (idx, tx) in body.transactions.into_iter().enumerate() {
        let mut s_prev: u64 = 0;
        let mut s_post: u64 = 0;
        let (sender_addr, mut receiver_addr, tx_signing_hash) = match &pre_validated[idx] {
            Ok(data) => *data,
            Err(e) => {
                if is_simulation {
                    continue;
                } else {
                    log::error!("[SCL-HYBRID] ❌ Giao dịch #{} bị từ chối ở Pha 1: {}", idx, e);
                    return Err(BlockExecutionError { message: e.clone(), failing_tx_index: idx as i32 });
                }
            }
        };

        // --- BƯỚC XÁC THỰC RÀNH GIỚI COINBASE ---
        if idx == 0 {
            if receiver_addr != miner {
                let error_message = "COINBASE_RECEIVER_MISMATCH: Coinbase receiver address must match block header miner address".to_string();
                if is_simulation {
                    continue;
                } else {
                    log::error!(
                        "[SCL-SECURITY] 🔴 SAI LỆCH ĐỊA CHỈ NHẬN COINBASE tại khối #{}! Header miner: {}, Coinbase receiver: {}",
                        height, hex::encode(&miner), hex::encode(&receiver_addr)
                    );
                    return Err(BlockExecutionError {
                        message: error_message,
                        failing_tx_index: 0,
                    });
                }
            }
        }

        // [V2.0 SEGWIT-TXID] Sử dụng SegWit TxID (tx_signing_hash không chứa signature) làm TxID chuẩn hệ thống.
        let tx_h_arr = tx_signing_hash;
        
        // [PERF-OPTIMIZATION] Đã loại bỏ println debug của từng giao dịch để chống I/O bottleneck

        // C. Kiểm tra Replay Attack (Sử dụng Cache RAM V37.8)
        if tx.amount > 0 && height >= 15 {
            if tx.recent_block_hash.len() == 32 {
                let mut tx_rbh = [0u8; 32];
                tx_rbh.copy_from_slice(&tx.recent_block_hash);
                
                if !recent_hashes.contains(&tx_rbh) {
                    let error_message = format!("Replay/Outdated TX at TX #{}", idx);
                    if is_simulation {
                        continue;
                    } else {
                        log::error!("[SCL-SECURITY] 🔴 Status 5: REPLAY/OUTDATED TXID {} - Recent hash {} (Window: #{}-#{})", 
                            hex::encode(&tx_signing_hash), hex::encode(&tx_rbh), start_h, height);
                        return Err(BlockExecutionError { message: error_message, failing_tx_index: idx as i32 });
                    }
                }
            } else {
                let error_message = format!("Missing Recent Hash at TX #{}", idx);
                if is_simulation {
                    continue;
                } else {
                    log::error!("[SCL-SECURITY] 🔴 Status 6: MISSING RECENT HASH (Len: {}) cho TXID {}", tx.recent_block_hash.len(), hex::encode(&tx_signing_hash));
                    return Err(BlockExecutionError { message: error_message, failing_tx_index: idx as i32 });
                }
            }
        }

        // D. Kiểm tra Nonce, Phí và Số dư — CHỈ áp dụng cho giao dịch chuyển tiền (Amount > 0)
        let mut total_spend: u64 = 0;
        let mut creation_fee: u64 = 0; // [VANGUARD-ANTI-BLOAT] Phí tạo ví mới tinh

        if tx.amount > 0 {
            // 1. ĐỊNH NGHĨA "VÍ MỚI" CHUẨN KỸ THUẬT (4 ĐIỀU KIỆN)
            let receiver_state = state_cache.entry(receiver_addr).or_insert_with(|| mgr.get_account_state(&receiver_addr));
            
            let is_new_wallet = receiver_state.btc_z == 0 
                             && receiver_state.nonce == 0 
                             && receiver_state.coin_id == [0u8; 32]
                             && receiver_state.maturing_rewards.is_empty();

            if is_new_wallet {
                // Ví hoàn toàn chưa có vết tích trong Sổ cái -> Áp dụng Phí Khởi Tạo 1.000 VNT
                creation_fee = 1000;
                // [ANTI-SPAM-LOG] Tắt hoàn toàn log để tránh nghẽn I/O khi duyệt nhiều giao dịch
                // if !is_simulation {
                //     log::debug!("[ECONOMY-SHIELD] 🛡️ Áp dụng Phí khởi tạo 1.000 VNT cho ví mới tinh: {}", hex::encode(&receiver_addr[..8]));
                // }
            }
            // 2. Kiểm tra số dư khả dụng (Số dư thực tế đã chín)
            let sender_state = state_cache.entry(sender_addr).or_insert_with(|| mgr.get_account_state(&sender_addr));
            let (spendable_balance, sender_nonce) = (sender_state.btc_z, sender_state.nonce);

            if tx.nonce != sender_nonce {
                let error_message = format!("Nonce Mismatch at TX #{} (Expected {}, Got {})", idx, sender_nonce, tx.nonce);
                if is_simulation {
                    continue;
                } else {
                    log::error!("[SCL-SECURITY] 🔴 Status 3: NONCE MISMATCH cho TXID {} - Tx: {}, DB: {}", hex::encode(&tx_signing_hash), tx.nonce, sender_nonce);
                    return Err(BlockExecutionError { message: error_message, failing_tx_index: idx as i32 });
                }
            }

            if !fee_logic::is_valid_fee(tx.fee) {
                let error_message = format!("Invalid Fee at TX #{}", idx);
                if is_simulation {
                    continue;
                } else {
                    log::error!("[ECONOMY] 🔴 PHÍ SAI QUY ĐỊNH: TXID {}", hex::encode(&tx_signing_hash));
                    return Err(BlockExecutionError { message: error_message, failing_tx_index: idx as i32 });
                }
            }

            // [VANGUARD-FIX] Tính tổng chi phí bao gồm cả phí khởi tạo ví mới
            total_spend = tx.amount.saturating_add(tx.fee).saturating_add(creation_fee);
            
            if spendable_balance < total_spend {
                let error_message = format!("Insufficient Balance at TX #{}", idx);
                if is_simulation {
                    continue;
                } else {
                    log::error!("[SCL-ECONOMY] 🔴 Status 7: KHÔNG ĐỦ SỐ DƯ (Spendable: {}, Required: {} bao gồm {} VNT phí tạo ví)", spendable_balance, total_spend, creation_fee);
                    return Err(BlockExecutionError { message: error_message, failing_tx_index: idx as i32 });
                }
            }


            // --- GHI NHẬN THAY ĐỔI SENDER VÀO CACHE ---
            let sender_state = state_cache.get_mut(&sender_addr).unwrap();
            s_prev = sender_state.btc_z; // [VANGUARD-AUDIT] Lưu số dư trước khi trừ
            sender_state.btc_z = sender_state.btc_z.saturating_sub(total_spend);
            s_post = sender_state.btc_z; // [VANGUARD-AUDIT] Lưu số dư sau khi trừ
            sender_state.nonce += 1;

            // [EISD] Cộng dồn bộ đếm nhân quả (Bao gồm cả Creation Fee để vượt qua bài kiểm toán tổng cung)
            audit_total_deducted = audit_total_deducted.saturating_add(total_spend);
            audit_total_credited = audit_total_credited.saturating_add(tx.amount);
        } else {
            // Coinbase: Amount=0, Fee=0, không có sender thực sự
            // [ANTI-SPAM-LOG] Tắt hoàn toàn log để tránh nghẽn I/O khi duyệt nhiều giao dịch
            // if !is_simulation {
            //     log::debug!("[COINBASE] ✅ TX #{} là Coinbase (Amount=0) — Bỏ qua kiểm tra Nonce/Phí/Số dư", idx);
            // }
        }

        // --- GHI NHẬN THAY ĐỔI RECEIVER VÀO CACHE ---
        let receiver_state = state_cache.entry(receiver_addr).or_insert_with(|| mgr.get_account_state(&receiver_addr));
        let r_prev = receiver_state.btc_z; // [VANGUARD-AUDIT] Lưu số dư trước khi nhận

        let amount_to_credit = if idx == 0 {
            0 
        } else {
            tx.amount
        };

        // [ANTI-SPAM-LOG] Tắt hoàn toàn log để tránh nghẽn I/O khi duyệt nhiều giao dịch
        // if !is_simulation {
        //     log::debug!("[SCL-EXEC] 💎 TX #{} | Height: {} | Amount: {} | To: {} | From: {}", 
        //         idx, height, amount_to_credit, hex::encode(&receiver_addr), hex::encode(&sender_addr));
        // }

        if amount_to_credit > 0 {
            receiver_state.btc_z = receiver_state.btc_z.saturating_add(amount_to_credit);
        }
        let r_post = receiver_state.btc_z; // [VANGUARD-AUDIT] Lưu số dư sau khi nhận

        if !is_simulation {
            if idx > 0 {
                let tracked_tx = state_manager::TrackedTx {
                    tx_id: tx_h_arr,
                    sender: sender_addr,
                    receiver: receiver_addr,
                    amount: tx.amount,
                    fee: tx.fee,
                    timestamp: block_timestamp as i64,
                    block_height: height,
                    nonce: tx.nonce,
                    status: 1, // SUCCESS
                    is_finalized: false,
                    confirmations: 0,
                    error_message: "".to_string(),
                    sender_prev_balance: if tx.amount > 0 { s_prev } else { 0 },
                    sender_post_balance: if tx.amount > 0 { s_post } else { 0 },
                    receiver_prev_balance: r_prev,
                    receiver_post_balance: r_post,
                };
                block_receipts.push(tracked_tx);
            }
        }
        
        touched_accs.insert(receiver_addr);
        if idx > 0 { touched_accs.insert(sender_addr); } 

        total_fees = total_fees.saturating_add(tx.fee).saturating_add(creation_fee);
        tx_hashes.push(tx_h_arr);
        tx_hashes_clone.push(tx_h_arr);

        // Mọi thứ OK, đưa nó vào danh sách hợp lệ
        valid_transactions.push(tx);
    }

    let block_reward = reward_logic::calculate_block_reward_btc_z(height);
    let total_miner_reward = block_reward.saturating_add(total_fees);

    touched_accs.insert(miner);

    let miner_state = state_cache.entry(miner).or_insert_with(|| mgr.get_account_state(&miner));

    // [VANGUARD-FIX] Ghi Receipt cho Coinbase SAU KHI đã có tổng Phí (total_fees)
    if !is_simulation && !tx_hashes.is_empty() {
        let coinbase_tx_id = tx_hashes[0];
        
        // [VANGUARD-RECOVERY-FIX] Lấy số dư hiện tại của Miner để ghi vào Receipt
        let miner_current_bal = miner_state.btc_z;

        let tracked_coinbase = state_manager::TrackedTx {
            tx_id: coinbase_tx_id,
            sender: [0u8; 32],
            receiver: miner,
            amount: total_miner_reward,
            fee: 0,
            timestamp: block_timestamp as i64, // [VANGUARD-DETERMINISM] Dùng timestamp của khối
            block_height: height,
            nonce: 0,
            status: 1,
            is_finalized: false,
            confirmations: 0,
            error_message: "".to_string(),
            sender_prev_balance: 0,
            sender_post_balance: 0,
            receiver_prev_balance: miner_current_bal,
            receiver_post_balance: miner_current_bal,
        };
        block_receipts.push(tracked_coinbase);
        // Tại sao: Chuyển sang log::debug! để triệt tiêu bão I/O khi duyệt hàng ngàn giao dịch.
        log::debug!("[COINBASE-RECEIPT] ✅ Đã lưu tạm Receipt cho Miner tại Khối #{}: {} BTC_Z (Bao gồm phí)", height, total_miner_reward as f64 / 100_000_000.0);
    }

    // [VANGUARD-MATURATION-ZEN] Thêm Miner vào danh sách tác động để xử lý phần thưởng
    touched_accs.insert(miner);
    let miner_state = state_cache.entry(miner).or_insert_with(|| mgr.get_account_state(&miner));
    miner_state.maturing_rewards.push(crate::state_manager::MaturingReward {
        amount: total_miner_reward,
        height: height + 6,
    });
    // Tại sao: Chuyển sang log::debug! để triệt tiêu bão I/O khi duyệt hàng ngàn giao dịch.
    log::debug!("[ECONOMY-ZEN] 🎁 Lập lịch thưởng {} BTC_Z cho Miner tại khối #{}, sẽ chín tại #{}", 
        total_miner_reward as f64 / 100_000_000.0, height, height + 6);

    // [VANGUARD-FIX] Sắp xếp touched_accs để đảm bảo tính Bit-perfect Parity giữa các Node
    let mut touched_accs_vec: Vec<[u8; 32]> = touched_accs.iter().cloned().collect();
    touched_accs_vec.sort();

    // --- BƯỚC KIỂM TOÁN TẬN GỐC (CRITICAL CORE) ---

    // [EISD HARDENING V2.0] TẦNG THÉP: Kiểm tra Định luật Bảo toàn Tiền tệ
    use std::sync::atomic::Ordering;
    if audit_total_deducted != audit_total_credited.saturating_add(total_fees) {
        let err = format!(
            "[SECURITY] 🔴 VI PHẠM ĐỊNH LUẬT BẢO TOÀN TIỀN TỆ tại khối #{}! (Deducted: {}, Credited: {}, Fees: {})",
            height, audit_total_deducted, audit_total_credited, total_fees
        );
        println!("{}", err);
        genz_pow::SYNC_VIOLATION_COUNT.fetch_add(1, Ordering::SeqCst);
        return Err(BlockExecutionError { message: "[TOXIC_BRANCH] EISD_CONSERVATION_VIOLATION".to_string(), failing_tx_index: -1 });
    }


    // [VANGUARD-DETERMINISTIC] Cung tiền hiện tại đã được tính trực tiếp từ công thức toán học (Epoch-safe).
    let actual_total_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(height);

    if height % 100 == 0 || height < 10 {
        let log_msg = format!("[EISD-AUDIT-TRACE] Khối #{}: Actual_Math={}", 
            height, actual_total_supply);
        // Tại sao: Sử dụng log bất đồng bộ (log_async) để giải phóng luồng chính khỏi việc ghi I/O.
        log_async(log_msg);
    }

    // --- CHỐT SỔ NGUYÊN TỬ (ATOMIC COMMITMENT) ---

    // 6. [LỖI CẤP S #2 FIX] Chuẩn bị Batch KV cuối cùng
    let mut final_batch_kv: Vec<([u8; 32], Vec<u8>)> = Vec::new();
    let mut sorted_touched: Vec<([u8; 32], [u8; 32])> = touched_accs.iter()
        .map(|addr| (KeyHash::with::<blake3::Hasher>(addr).0, *addr))
        .collect();
    sorted_touched.sort_by(|a, b| a.0.cmp(&b.0));

    for (_key_hash, addr) in sorted_touched {
        let mut state = if let Some(cached) = state_cache.get(&addr) {
            cached.clone()
        } else {
            mgr.get_account_state(&addr)
        };

        // [VANGUARD-ZEN-PAYOUT] Giải ngân phần thưởng đến hạn ngay trong State Tree.
        let mut payout_total: u64 = 0;
        state.maturing_rewards.retain(|reward| {
            if height >= reward.height {
                payout_total += reward.amount;
                false // Xóa khỏi danh sách chờ
            } else {
                true // Giữ lại
            }
        });

        if payout_total > 0 {
            state.btc_z = state.btc_z.saturating_add(payout_total);
            // Tại sao: Chuyển sang log::debug! để triệt tiêu bão I/O khi duyệt hàng ngàn giao dịch.
            log::debug!("[MATURATION-ZEN] ✅ Khối #{}: Đã giải ngân {} VNT cho ví {}", height, payout_total, hex::encode(&addr));
        }


        final_batch_kv.push((addr, borsh::to_vec(&state).expect("Serialize failed")));
    }

    // 7. Chốt rễ trạng thái SMT
    let (mut rocks_batch, final_root) = if is_simulation {
        let root = mgr.consolidate_smt_simulation(final_batch_kv, height);
        (rocksdb::WriteBatch::default(), root)
    } else {
        mgr.prepare_smt_write_batch(&final_batch_kv, height)
    };

    if !is_simulation {
        // Ghi siêu dữ liệu giao dịch và chỉ mục vào batch
        mgr.put_block_transactions_to_batch(&mut rocks_batch, height, tx_hashes_clone.clone(), touched_accs_vec.clone());
        
        let receipt_cf = mgr.db.cf_handle(state_manager::CF_RECEIPTS).expect("Missing RECEIPTS CF");
        let mut addr_to_txs: std::collections::HashMap<[u8; 32], Vec<[u8; 32]>> = std::collections::HashMap::new();
        
        for tx in &block_receipts {
            let tx_data = borsh::to_vec(tx).expect("Failed to serialize TrackedTx");
            rocks_batch.put_cf(receipt_cf, &tx.tx_id, &tx_data);
            
            if tx.sender != [0u8; 32] {
                addr_to_txs.entry(tx.sender).or_default().push(tx.tx_id);
            }
            addr_to_txs.entry(tx.receiver).or_default().push(tx.tx_id);
        }

        // Cập nhật chỉ mục giao dịch một lần duy nhất cho mỗi địa chỉ trong block này để tránh lỗi ghi đè trong batch
        let index_cf = mgr.db.cf_handle(state_manager::CF_TX_INDEX).expect("Missing TX_INDEX CF");
        for (addr, new_tx_ids) in addr_to_txs {
            let mut txs = match mgr.db.get_cf(index_cf, &addr).unwrap_or(None) {
                Some(data) => <Vec<[u8; 32]>>::try_from_slice(&data).unwrap_or_default(),
                None => Vec::new(),
            };
            let mut changed = false;
            for tx_id in new_tx_ids {
                if !txs.contains(&tx_id) {
                    txs.push(tx_id);
                    changed = true;
                }
            }
            if changed {
                if txs.len() > 10000 {
                    let drain_len = txs.len() - 10000;
                    txs.drain(0..drain_len);
                }
                let data = borsh::to_vec(&txs).unwrap();
                rocks_batch.put_cf(index_cf, &addr, &data);
            }
        }
        
        // Ghi siêu dữ liệu kinh tế vào batch
        mgr.set_actual_total_supply_to_batch(&mut rocks_batch, actual_total_supply);
        mgr.set_expected_supply_to_batch(&mut rocks_batch, actual_total_supply);
    }



    let payload = ExecutionPayload {
        state_batch: rocks_batch,
        final_root,
        tx_hashes: tx_hashes_clone,
        touched_accs: touched_accs_vec,
        actual_total_supply,
    };

    Ok((payload, block_hash, valid_transactions))
}



/// [LỚP 1] Kiểm tra định dạng và chữ ký thô
pub fn validate_transaction_sig(tx: &Transaction) -> Result<(), Status> {
    // [EISD-L1] Chữ ký Ed25519 phải đủ 64 bytes
    let sig_bytes = tx.signature.as_ref()
        .map(|s| s.value.clone())
        .unwrap_or_default();
    
    if sig_bytes.len() != 64 {
        return Err(Status::unauthenticated("[EISD-L1] Định dạng chữ ký không hợp lệ"));
    }
    
    // Thực thi verify chữ ký thực tế với ed25519-dalek (thông qua crypto_genesis)
    let sender_bytes = tx.sender.as_ref().map(|a| a.value.clone()).unwrap_or_default();
    let sender_addr: [u8; 32] = sender_bytes.try_into()
        .map_err(|_| Status::invalid_argument("Sender address không hợp lệ"))?;
    
    let msg_arr = crate::crypto_primitives::calculate_signing_hash(tx);
    
    let mut sig_arr = [0u8; 64];
    sig_arr.copy_from_slice(&sig_bytes);

    let is_valid = crypto_primitives::verify_ed25519_signature(&sender_addr, &msg_arr, &sig_arr);
    if !is_valid {
        return Err(Status::unauthenticated("[EISD-L1] Chữ ký Ed25519 giả mạo hoặc không khớp!"));
    }
    Ok(())
}

/// [LỚP 2] Kiểm tra trạng thái và số dư trước giao dịch
pub fn validate_sender_state(mgr: &StateManager, addr: &[u8; 32], amount: u64, nonce: u64) -> Result<(), Status> {
    let state = mgr.get_account_state(addr);
    
    // 1. Kiểm tra số dư (Balance)
    if state.btc_z < amount {
        return Err(Status::failed_precondition(format!(
            "[HSSD-L2] Số dư không đủ ({} < {})", 
            state.btc_z, amount
        )));
    }

    // 2. Kiểm tra Nonce (Chống Replay Attack)
    if state.nonce != nonce {
        return Err(Status::failed_precondition(format!(
            "[HSSD-L2] Nonce mismatch: mong đợi {}, nhận được {}", 
            state.nonce, nonce
        )));
    }

    Ok(())
}

/// [V1.2] Lấy số dư thực tế có thể chi tiêu (đã trừ phần thưởng chưa trưởng thành)
pub fn get_spendable_balance(address: Vec<u8>) -> u64 {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi truy vấn số dư trước khi StateManager khởi tạo xong.
    // Tại sao: Khi khởi động, các tiến trình Go Node và Web UI truy vấn số dư rất sớm, việc trả về 0 tạm thời
    // sẽ ngăn crash tiến trình scl_server.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    if address.len() != 32 { return 0; }
    let mut addr = [0u8; 32];
    addr.copy_from_slice(&address);
    let state = mgr.get_account_state(&addr);
    
    // [VANGUARD-FIX] Vì hiện tại btc_z chỉ được cộng khi tiền đã trưởng thành (Mature),
    // nên số dư khả dụng chính là btc_z.
    state.btc_z
}

/// [LỚP 3] Kiểm tra UTXO/Nonce và áp dụng thay đổi
pub fn apply_transaction(mgr: &mut StateManager, tx: &Transaction) -> Result<(), Status> {
    let mut s_addr = [0u8; 32];
    if let Some(ref addr) = tx.sender { s_addr.copy_from_slice(&addr.value); }
    let mut r_addr = [0u8; 32];
    if let Some(ref addr) = tx.receiver { r_addr.copy_from_slice(&addr.value); }

    // 1. Cập nhật Sender
    let mut s_state = mgr.get_account_state(&s_addr);
    s_state.btc_z = s_state.btc_z.checked_sub(tx.amount).ok_or_else(|| Status::internal("Underflow sender"))?;
    s_state.nonce += 1;
    mgr.update_account(&s_addr, s_state);

    // 2. Cập nhật Receiver
    let mut r_state = mgr.get_account_state(&r_addr);
    r_state.btc_z = r_state.btc_z.checked_add(tx.amount).ok_or_else(|| Status::internal("Overflow receiver"))?;
    mgr.update_account(&r_addr, r_state);

    Ok(())
}

pub fn calculate_absolute_weight(parent_weight: Vec<u8>, difficulty_raw: Vec<u8>) -> Vec<u8> {
    // [HOTFIX V1.20] Sử dụng U256 Little Endian nhất quán với reorg.rs
    // Tại sao: Hàm cũ dùng BigInt::from_bytes_be (Big Endian), nhưng reorg.rs dòng 101
    // đọc weight bằng U256::from_little_endian. Bất đồng bộ Endianness gây Reorg bất thường.
    let p_weight = {
        let mut weight_padded = [0u8; 32];
        let w_len = parent_weight.len().min(32);
        if w_len > 0 {
            weight_padded[..w_len].copy_from_slice(&parent_weight[..w_len]);
        }
        U256::from_little_endian(&weight_padded)
    };

    let mut diff_padded = [0u8; 32];
    let d_len = difficulty_raw.len().min(32);
    diff_padded[..d_len].copy_from_slice(&difficulty_raw[..d_len]);
    let diff_u256 = U256::from_little_endian(&diff_padded);

    let weight = p_weight + diff_u256;
    let mut out = [0u8; 32];
    weight.to_little_endian(&mut out);
    out.to_vec()
}


pub fn calculate_block_reward_btc_z(height: u64) -> u64 {
    reward_logic::calculate_block_reward_btc_z(height)
}


pub fn calculate_next_difficulty(prev_ts: u64, parent_ts: u64, current_diff_raw: Vec<u8>) -> Vec<u8> {
    let mgr = get_mgr!();
    let height = mgr.get_current_version(); 
    let next_height = height + 1;
    let n = 120;
    
    let mut timestamps = Vec::with_capacity(n + 1);
    let mut difficulties = Vec::with_capacity(n);
    let current_diff = U256::from_little_endian(&current_diff_raw);
    
    let start_h = height.saturating_sub(n as u64);
    for h in start_h..=height {
        if let Some(hash) = mgr.get_block_hash(h) {
            if let Some(header_raw) = mgr.get_header_raw(&hash) {
                if let Ok(header) = BlockHeader::decode(&header_raw[..]) {
                    timestamps.push(header.timestamp);
                    // [VANGUARD-FIX] Sửa điều kiện lọc từ h < height thành h > start_h để lấy độ khó từ start_h + 1 đến height,
                    // tránh lệch pha độ khó khi sinh block template ở node cục bộ (đỉnh chuỗi height đã nằm trong DB).
                    if h > start_h {
                        difficulties.push(U256::from_little_endian(&header.difficulty));
                    }
                    println!("[DIFF-WINDOW] 📋 H#{} | TS: {} | Diff: {}", h, header.timestamp, hex::encode(&header.difficulty));
                }
            }
        }
    }
    
    if difficulties.len() < n {
        difficulties.push(current_diff);
    }
    
    if timestamps.len() < n + 1 {
        // [VANGUARD-FIX] Đồng bộ hóa: Khi thiếu lịch sử, sử dụng độ khó của khối trước (last_diff.max(*MIN_DIFFICULTY))
        // giống hệt như logic calculate_next_difficulty của DAA để tránh lệch pha giữa Miner và Validator.
        let next = current_diff.max(*difficulty_logic::MIN_DIFFICULTY);
        let mut out = [0u8; 32];
        next.to_little_endian(&mut out);
        return out.to_vec();
    }

    let next_diff = difficulty_logic::calculate_next_difficulty(&timestamps, &difficulties, prev_ts, next_height);
    
    let mut out = [0u8; 32];
    next_diff.to_little_endian(&mut out);
    out.to_vec()
}

/// [V1.7.0] calculate_next_difficulty_v2 sử dụng mảng dữ liệu lịch sử nhận từ Go

pub fn calculate_next_difficulty_v2(
    timestamps: Vec<u64>,
    difficulties_raw: Vec<Vec<u8>>,
    current_ts: u64,
    height: u64,
) -> Vec<u8> {
    let difficulties: Vec<U256> = difficulties_raw.iter()
        .map(|d| {
            // [FIX-VANGUARD] Bù số 0 chống Panic từ Protobuf (Go trims trailing zeros)
            let mut padded = [0u8; 32];
            let len = d.len().min(32);
            if len > 0 {
                padded[..len].copy_from_slice(&d[..len]);
            }
            U256::from_little_endian(&padded)
        })
        .collect();

    // [VANGUARD-FIX] Tính toán cho chiều cao cụ thể (thường là nextHeight)
    let next_diff = difficulty_logic::calculate_next_difficulty(&timestamps, &difficulties, current_ts, height);
    
    let mut out = [0u8; 32];
    next_diff.to_little_endian(&mut out);
    out.to_vec()
}

pub fn get_mining_difficulty() -> Vec<u8> {
    let mgr = get_mgr!();
    let h = mgr.get_current_version();
    if let Some(hash) = mgr.get_block_hash(h) {
        if let Some(raw) = mgr.get_header_raw(&hash) {
            if let Ok(header) = BlockHeader::decode(&raw[..]) {
                return header.difficulty;
            }
        }
    }
    vec![0; 32]
}


pub fn set_expected_supply(supply: u64) {
    let mgr = get_mgr!();
    mgr.set_expected_supply(supply);
}


pub fn init_scl_state(path: String) -> anyhow::Result<()> {
    println!("[SCL-INIT] 📂 Đang khởi tạo Global State...");
    state_manager::init_global_state(&path)?;
    println!("[SCL-INIT] ✅ Đã khởi tạo Global State.");
    
    // [V8 BOOTSTRAP] Kiểm toán Cán cân khi khởi động
    println!("[SCL-INIT] 🔍 Đang kiểm tra StateManager...");
    if let Some(mgr) = state_manager::get_state_manager() {
        println!("[SCL-INIT] 📂 Đang lấy Current Height...");
        let current_h = mgr.get_current_version();
        println!("[SCL-INIT] 📊 Current Height: #{}", current_h);
        // [VANGUARD-DETERMINISM-REBORN] Kích hoạt lại lớp phòng thủ Fail-Stop.
        // Sau khi áp dụng Deterministic Supply, mọi sai lệch tại Bootstrap đều là dấu hiệu của Corrupted DB.
        let actual = mgr.get_actual_total_supply();
        let expected = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(current_h);
        
        let is_fresh_node = current_h == 0 && actual == 0 && mgr.get_block_hash(0).is_none();

        if !is_fresh_node && actual != expected {
            log::error!("[BOOTSTRAP] 🚨 FAIL-STOP: Phát hiện sai lệch cung tiền nghiêm trọng! (DB: {}, Math: {}, Height: #{})", actual, expected, current_h);
            
            if current_h == 0 {
                log::warn!("[BOOTSTRAP] ⚠️  CẢNH BÁO: Sai lệch tại Genesis. Có thể do nạp Snapshot chưa hoàn tất.");
            } else {
                // [SECURITY-CRITICAL] Dừng Node ngay lập tức để bảo vệ tài sản người dùng.
                panic!(
                    "[SECURITY-CRITICAL] FAIL-STOP: Cán cân cung tiền không khớp tại Bootstrap! DB={}, Math={}, Height=#{}. Vui lòng dọn dẹp data và đồng bộ lại.",
                    actual, expected, current_h
                );
            }
        }

        println!("[SCL-INIT] 🧹 Đang dọn dẹp mã băm ma...");
        mgr.cleanup_future_hashes(current_h);
        println!("[SCL-INIT] ✅ Đã dọn dẹp xong.");

        log::info!("[BOOTSTRAP] ✅ Sổ cái bảo toàn. Chiều cao hiện tại: #{}", current_h);
    }
    Ok(())
}




pub fn get_balance(address: Vec<u8>) -> u64 {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    if address.len() != 32 { return 0; }
    let mut addr = [0u8; 32];
    addr.copy_from_slice(&address);
    
    let state = mgr.get_account_state(&addr);
    let pending: u64 = state.maturing_rewards.iter().map(|r| r.amount).sum();
    state.btc_z.saturating_add(pending)
}


pub fn get_nonce(address: Vec<u8>) -> u64 {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    if address.len() != 32 { return 0; }
    let mut addr = [0u8; 32];
    addr.copy_from_slice(&address);
    mgr.get_account_state(&addr).nonce
}


pub fn get_nano_weight(address: Vec<u8>) -> u32 {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi truy vấn trọng lượng trước khi StateManager khởi tạo xong.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    if address.len() != 32 { return 0; }
    let mut addr = [0u8; 32];
    addr.copy_from_slice(&address);
    mgr.get_account_state(&addr).nano_weight
}


pub fn rollback_state(current_height: u64, target_height: u64) -> bool {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) nếu gọi rollback khi chưa khởi tạo xong.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return false,
    };
    mgr.rollback_state(current_height, target_height).is_ok()
}

/// [BÀN TAY VÔ HÌNH] Xóa khối vật lý — bỏ qua Tường lửa Bất biến.
/// Chỉ dùng cho công cụ nhà vận hành node (localhost + mã xác nhận 01900).
pub fn force_delete_blocks(current_height: u64, target_height: u64) -> bool {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi gọi thao tác xóa khối vật lý.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return false,
    };
    mgr.force_delete_blocks(current_height, target_height).is_ok()
}

/// [V1.0.4] ĐẠI THANH TRỪNG (Batch Purge)

pub fn purge_historical_data(start_height: u64, end_height: u64) -> bool {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi thực hiện tiến trình dọn dẹp dữ liệu lịch sử.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return false,
    };
    mgr.purge_historical_data(start_height, end_height).is_ok()
}


pub fn rollback_jmt_version(target_version: u64) -> bool {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi rollback phiên bản JMT.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return false,
    };
    mgr.rollback_jmt_version(target_version).is_ok()
}


pub fn update_account_state(address: Vec<u8>, amount: u64, nonce: u64, _height: u64) {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return,
    };
    if address.len() == 32 {
        let mut addr = [0u8; 32];
        addr.copy_from_slice(&address);
        let mut state = mgr.get_account_state(&addr);
        state.btc_z = amount;
        state.nonce = nonce;
        mgr.update_account(&addr, state);
    }
}



pub fn get_oldest_height() -> u64 {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    mgr.get_oldest_height()
}




pub fn verify_pow(
    header_bytes: Vec<u8>,
    nonce: u64,
    difficulty: Vec<u8>,
    height: u64,
) -> BlockVerificationResult {
    let mgr = crate::state_manager::get_state_manager();
    let mgr = mgr.as_ref();
    
    let mut header_hash = [0u8; 32];
    if header_bytes.len() == 112 {
        // [Vanguard V112 Standard]
        let mut height_bytes = [0u8; 8];
        height_bytes.copy_from_slice(&header_bytes[0..8]);
        let h_val = u64::from_le_bytes(height_bytes);

        let h_bytes = crypto_primitives::calculate_blake3_hash_ffi(header_bytes.clone(), h_val);
        header_hash.copy_from_slice(&h_bytes);
    } else if let Ok(header) = BlockHeader::decode(&header_bytes[..]) {
        // [HOTFIX] KIÊN QUYẾT ĐỒNG NHẤT CHUẨN BĂM VANGUARD 112-BYTE CHO MỌI ĐẦU VÀO!
        // Tuyệt đối không băm chuỗi Protobuf thô như trước đây nữa.
        let parent_h = header.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        let tx_root = header.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
        let packed_header = crate::genz_pow::pack_header_v112(
            header.height,
            &parent_h,
            header.timestamp,
            &tx_root,
            &header.difficulty
        );
        let h_bytes = crypto_primitives::calculate_blake3_hash(packed_header.to_vec(), header.height);
        header_hash.copy_from_slice(&h_bytes);
    } else {
        log::info!("[SCL-FFI] ❌ Dữ liệu Header không hợp lệ (Len: {})", header_bytes.len());
        return BlockVerificationResult::InvalidPoW;
    }

    // [ANTI-REORG] Chặn đứng kẻ tấn công nếu vi phạm Finality
    let mut h_arr = [0u8; 32];
    h_arr.copy_from_slice(&header_hash);
    if let Some(mgr) = mgr {
        if mgr.validate_block_finality(height, &h_arr).is_err() {
            println!("[FIREWALL] 🔴 VI PHẠM TƯỜNG LỬA BẤT BIẾN TẠI KHỐI #{}", height);
            return BlockVerificationResult::FirewallViolation;
        }
    }

    // [VANGUARD-CONSENSUS-DIFF-CHECK] Tường lửa độ khó LWMA DAA tại cửa ngõ Go
    // Tại sao: Bất kỳ khối mới nào nhận được từ mạng (qua Gossip hoặc Sync) đều được Go chuyển xuống đây
    // để xác thực. Việc kiểm soát độ khó DAA ở đây giúp triệt tiêu hoàn toàn các khối rác độ khó thấp (DoS)
    // từ peer độc hại trước khi chúng đi sâu vào hệ thống.
    // Thiết kế: Chỉ chạy kiểm tra khi khối cha đã được lưu trong database. Nếu là khối mồ côi (chưa có cha),
    // ta chấp nhận bypass kiểm tra độ khó này để Go có thể nhận làm khối mồ côi nhằm kích hoạt cơ chế đồng bộ lùi.
    if height > 0 {
        if let Some(mgr) = mgr {
            let mut parent_hash = [0u8; 32];
            let mut timestamp = 0u64;
            
            if header_bytes.len() == 112 {
                let mut ts_bytes = [0u8; 8];
                ts_bytes.copy_from_slice(&header_bytes[40..48]);
                timestamp = u64::from_le_bytes(ts_bytes);
                parent_hash.copy_from_slice(&header_bytes[8..40]);
            } else if let Ok(header) = BlockHeader::decode(&header_bytes[..]) {
                timestamp = header.timestamp;
                let p_bytes = header.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
                let p_len = p_bytes.len().min(32);
                if p_len > 0 {
                    parent_hash[..p_len].copy_from_slice(&p_bytes[..p_len]);
                }
            }

            // [VANGUARD-CONSENSUS-DOS-FIX]
            // Tại sao: Đối với khối mồ côi (chưa có cha trong DB), hệ thống bỏ qua kiểm tra LWMA DAA.
            // Nếu không giới hạn độ khó tối thiểu, kẻ tấn công có thể set difficulty = 1 trong header của khối mồ côi,
            // làm target cực kỳ lớn và dễ dàng tìm được nonce hợp lệ bằng vài phép băm để spam rác gây nghẽn RAM và P2P.
            // Việc cưỡng chế độ khó >= MIN_DIFFICULTY (hiện tại là 10 tỷ) đối với mọi khối chiều cao > 0 sẽ
            // loại bỏ hoàn toàn khả năng bypass DAA bằng độ khó cực thấp này.
            let mut diff_padded = [0u8; 32];
            let d_len = difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&difficulty[..d_len]);
            }
            let diff_u256 = U256::from_little_endian(&diff_padded);
            if diff_u256 < *difficulty_logic::MIN_DIFFICULTY {
                log::error!(
                    "[CONSENSUS-SECURITY] verify_pow: 🚨 Khối #{} có độ khó nhỏ hơn MIN_DIFFICULTY! (Gửi: {}, MIN: {})",
                    height, diff_u256, *difficulty_logic::MIN_DIFFICULTY
                );
                return BlockVerificationResult::InvalidPoW;
            }

            if mgr.get_header_raw(&parent_hash).is_some() {
                let n = 120u64;
                let mut history_ts = Vec::new();
                let mut history_diffs = Vec::new();
                
                let mut curr_hash = parent_hash;
                for _ in 0..=n {
                    if curr_hash == [0u8; 32] {
                        break;
                    }
                    if let Some(raw) = mgr.get_header_raw(&curr_hash) {
                        if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                            history_ts.push(hdr.timestamp);
                            let mut diff_padded = [0u8; 32];
                            let d_len = hdr.difficulty.len().min(32);
                            if d_len > 0 {
                                diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                            }
                            history_diffs.push(U256::from_little_endian(&diff_padded));
                            
                            let p_hash = hdr.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
                            if p_hash.len() == 32 {
                                curr_hash.copy_from_slice(&p_hash);
                            } else {
                                break;
                            }
                        } else {
                            break;
                        }
                    } else {
                        break;
                    }
                }
                
                history_ts.reverse();
                history_diffs.reverse();
                
                // [VANGUARD-FIX] Đưa tường lửa LWMA DAA vào khóa chết cửa ngõ của go.
                // Cơ chế Fail-Closed: Nếu chiều cao khối yêu cầu lịch sử DAA mà DB không trả về đủ
                // (ví dụ: DB bận, nghẽn I/O hoặc dữ liệu bị thiếu), ta phải báo lỗi và từ chối khối (Fail-Closed).
                // Không được tự ý bypass hoặc kích hoạt bootstrap guard trả về độ khó cũ.
                let expected_history_len = height.min(n + 1) as usize;
                if history_ts.len() < expected_history_len {
                    log::error!(
                        "[CONSENSUS-SECURITY] verify_pow DAA: 🚨 DB cục bộ thiếu hoặc bận đọc lịch sử DAA tại #{}! Yêu cầu: {}, Tìm thấy: {}",
                        height, expected_history_len, history_ts.len()
                    );
                    return BlockVerificationResult::DbBusy;
                }
                
                if !history_ts.is_empty() {
                    let expected_diff = crate::difficulty_logic::calculate_next_difficulty(&history_ts, &history_diffs, timestamp, height);
                    let mut diff_padded = [0u8; 32];
                    let d_len = difficulty.len().min(32);
                    if d_len > 0 {
                        diff_padded[..d_len].copy_from_slice(&difficulty[..d_len]);
                    }
                    let actual_diff = U256::from_little_endian(&diff_padded);
                    
                    if actual_diff != expected_diff {
                        log::error!(
                            "[CONSENSUS-SECURITY] verify_pow DAA: 🚨 Sai lệch độ khó LWMA tại khối #{}! Mong đợi: {}, Thực tế: {}",
                            height, expected_diff, actual_diff
                        );
                        return BlockVerificationResult::InvalidPoW;
                    }
                }
            }
        }
    }

    
    // Công thức băm đơn lớp: Keyed_Blake3(Context, HeaderHash + Nonce)
    let mut material = [0u8; 40];
    material[..32].copy_from_slice(&header_hash);
    material[32..].copy_from_slice(&nonce.to_le_bytes());
    
    let hash_result = crypto_primitives::calculate_blake3_hash(material.to_vec(), height);
    let hash_u256 = U256::from_little_endian(&hash_result);
    let mut diff_padded = [0u8; 32];
    let d_len = difficulty.len().min(32);
    if d_len > 0 {
        diff_padded[..d_len].copy_from_slice(&difficulty[..d_len]);
    }
    let diff_u256 = U256::from_little_endian(&diff_padded);

    let target = genz_pow::difficulty_to_target(diff_u256);
    let mut target_bytes = [0u8; 32];
    target.to_big_endian(&mut target_bytes);

    if hash_u256 < target {
        BlockVerificationResult::Success
    } else {
        log::warn!("[POW-REJECT] 🔴 H#{} | Hash: {} | Target: {} | HeaderHash: {}", 
            height, hex::encode(&hash_result), hex::encode(target_bytes), hex::encode(&header_hash));
        BlockVerificationResult::InvalidPoW
    }
}

/// [Audit S1 FIX] Tường lửa Thời gian (Time Firewall)
/// Ngăn chặn tấn công Timestamp Spoofing làm lệch thuật toán LWMA.

pub fn verify_timestamp_firewall(
    timestamp: u64,
    parent_timestamp: u64,
    current_now: u64,
    _bypass_future_check: bool,
) -> bool {
    // 1. Luật nhân quả: Khối mới phải có thời gian sau khối cha
    // QUY TẮC BẤT BIẾN: Tuyệt đối không chấp nhận bằng nhau hoặc nhỏ hơn (Enforced causality)
    if timestamp <= parent_timestamp {
        return false;
    }

    // 2. Luật đồng bộ: Luôn kiểm tra tương lai 5 phút (300 giây) bất kể đang sync hay không.
    // [BẢO MẬT] Rút ngắn từ 2 giờ (7200 giây) xuống 5 phút (300 giây) để tương thích với block time 75s
    // và triệt tiêu hoàn toàn khả năng thực hiện Time-Warp Attack thao túng LWMA DAA.
    if timestamp > current_now + 300 {
        return false;
    }

    true
}

/// [Audit V10.4] Trả về Hashrate (H/s) trung bình kể từ lần gọi cuối cùng.

pub fn emergency_state_rebuild(target_height: u64) -> bool {
    log::warn!("[SCL-RECOVERY] ☢️ Khởi động tái thiết trạng thái khẩn cấp đến #{}", target_height);
    
    let current = get_current_version();
    if target_height >= current {
        log::info!("[SCL-RECOVERY] Target >= Current ({} >= {}), bỏ qua.", target_height, current);
        return true;
    }
    
    log::info!("[SCL-RECOVERY] Đang thực thi Rollback từ {} về {}...", current, target_height);
    rollback_state(current, target_height)
}

pub fn reset_state_completely() -> bool {
    if let Some(mgr) = state_manager::get_state_manager() {
        if let Err(e) = mgr.reset_state_completely() {
            log::error!("[SCL] ❌ Lỗi reset state: {:?}", e);
            return false;
        }
        true
    } else {
        false
    }
}

pub fn get_highest_block_height() -> u64 {
    if let Some(mgr) = state_manager::get_state_manager() {
        mgr.get_highest_block_height()
    } else {
        0
    }
}

pub fn get_hashrate() -> u64 {
    use std::sync::atomic::Ordering;
    let val = genz_pow::HASHRATE_COUNTER.swap(0, Ordering::SeqCst);
    if val > 0 {
        log::debug!("[DEBUG-SCL] 📊 Trích xuất bộ đếm: {}", val);
    }
    val
}


pub fn commit_block_hash(height: u64, hash: Vec<u8>) -> bool {
    let mgr = state_manager::get_state_manager().expect("StateManager not initialized");
    if hash.len() != 32 { 
        log::info!("[SCL-FFI] ❌ Từ chối Commit Hash sai kích thước: {} bytes (Height {})", hash.len(), height);
        return false; 
    }
    
    let mut h_arr = [0u8; 32];
    h_arr.copy_from_slice(&hash);
    
    // [V10.2 LOGGING] Theo dõi dữ liệu đầu vào từ Go
    log::info!("[SCL-FFI] 📥 Nhận Hash cho khối #{}: {}", height, hex::encode(&h_arr));
    
    mgr.put_block_hash(height, &h_arr);

    // [Audit S#2 FIX] Tự động hóa Bất biến (HSSD Layer 5)
    // Ngay khi khối H được commit bình thường, khóa lịch sử H-5. 
    mgr.set_finalized_height(height.saturating_sub(5));
    
    true
}

/// [V1.2.9] Đồng bộ hóa Mã băm Định danh (Canonical Hashing)
/// Đảm bảo TxID tính toán từ Go luôn khớp với Ledger thực thi.

pub fn calculate_tx_hash(tx_bytes: Vec<u8>, height: u64) -> Vec<u8> {
    if let Ok(tx) = <Transaction as Message>::decode(&tx_bytes[..]) {
        // [V2.0 SEGWIT-TXID] Nhất thể hóa TxID: Luôn loại bỏ chữ ký trước khi băm để
        // triệt tiêu hoàn toàn rủi ro Hacker thay đổi chữ ký số (Malleability DoS).
        return crypto_primitives::calculate_signing_hash(&tx).to_vec();
    }
    Vec::new()
}

pub fn calculate_signing_hash_ffi(tx_bytes: Vec<u8>, height: u64) -> Vec<u8> {
    if let Ok(tx) = <Transaction as Message>::decode(&tx_bytes[..]) {
        return crypto_primitives::calculate_signing_hash(&tx).to_vec();
    }
    Vec::new()
}

/// [V1.0 REFACTORED] Ký dữ liệu ủy quyền từ Rust Core
pub fn authoritative_sign(data: Vec<u8>, private_key: Vec<u8>) -> anyhow::Result<Vec<u8>> {
    if private_key.len() != 32 {
        return Err(anyhow::anyhow!("Private Key phải có độ dài 32 bytes"));
    }
    let mut priv_arr = [0u8; 32];
    priv_arr.copy_from_slice(&private_key);
    
    let sig = crypto_primitives::sign_ed25519(&priv_arr, &data);
    Ok(sig.to_vec())
}

/// [V1.0 REFACTORED] Tự tay Rust đóng gói và ký Giao dịch (Source of Truth)
pub fn prepare_transaction(
    sender: Vec<u8>,
    receiver: Vec<u8>,
    amount: u64,
    fee: u64,
    nonce: u64,
    private_key: Vec<u8>,
    recent_block_hash: Vec<u8>,
) -> anyhow::Result<Transaction> {
    if private_key.len() != 32 {
        return Err(anyhow::anyhow!("Private Key không hợp lệ"));
    }

    // 1. Tạo đối tượng TX thô
    let mut tx = Transaction {
        version: 1,
        sender: Some(crate::proto::common::Address { value: sender }),
        receiver: Some(crate::proto::common::Address { value: receiver }),
        amount,
        fee,
        nonce,
        timestamp: std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos() as u64,
        recent_block_hash,
        signature: None,
        chain_id: 25062025,
    };

    // 2. Tính Signing Hash
    let signing_hash = crypto_primitives::calculate_signing_hash(&tx);
    
    // 3. Ký bằng Rust
    let mut priv_arr = [0u8; 32];
    priv_arr.copy_from_slice(&private_key);
    let sig_bytes = crypto_primitives::sign_ed25519(&priv_arr, &signing_hash);
    
    // 4. Gắn chữ ký vào TX
    tx.signature = Some(crate::proto::common::Signature { value: sig_bytes.to_vec() });
    
    Ok(tx)
}


pub fn calculate_block_header_hash(header_bytes: Vec<u8>) -> Vec<u8> {
    if header_bytes.len() == 112 {
        // [Vanguard V112 Standard] Trích xuất Height
        let mut height_bytes = [0u8; 8];
        height_bytes.copy_from_slice(&header_bytes[0..8]);
        let h_val = u64::from_le_bytes(height_bytes);

        let h_hash = crypto_primitives::calculate_blake3_hash_ffi(header_bytes, h_val);
        return h_hash;
    } else if let Ok(header) = BlockHeader::decode(&header_bytes[..]) {
        // [HOTFIX] CHUẨN HÓA VANGUARD HASH TỪ PROTOBUF
        let parent_h = header.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        let tx_root = header.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
        let packed_header = crate::genz_pow::pack_header_v112(
            header.height,
            &parent_h,
            header.timestamp,
            &tx_root,
            &header.difficulty
        );
        let h_hash = crypto_primitives::calculate_blake3_hash(packed_header.to_vec(), header.height);
        return h_hash.to_vec();
    }
    log::warn!("[SCL-FFI] ⚠️ Yêu cầu tính BlockHash với dữ liệu không xác định (Len: {})", header_bytes.len());
    Vec::new()
}


pub fn put_header(hash: Vec<u8>, header_raw: Vec<u8>) {
    let mgr = state_manager::get_state_manager().expect("StateManager not initialized");
    if hash.len() == 32 {
        let mut h_arr = [0u8; 32];
        h_arr.copy_from_slice(&hash);
        mgr.put_header(&h_arr, header_raw);
    }
}


pub fn get_header_raw(hash: Vec<u8>) -> Option<Vec<u8>> {
    if hash.len() == 32 {
        let mut h_arr = [0u8; 32];
        h_arr.copy_from_slice(&hash);
        let mgr = state_manager::get_state_manager().expect("StateManager not initialized");
        mgr.get_header_raw(&h_arr)
    } else {
        None
    }
}

pub fn get_block_body_raw(hash: Vec<u8>) -> Option<Vec<u8>> {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi đọc dữ liệu thân khối.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return None,
    };
    if hash.len() == 32 {
        let mut h_arr = [0u8; 32];
        h_arr.copy_from_slice(&hash);
        mgr.get_block_body_raw(&h_arr)
    } else {
        None
    }
}

// --- [V19 UNIFIED STORAGE] Logic quản lý Sổ cái Nhất thể ---

pub fn get_block_raw(height: u64) -> Option<Vec<u8>> {
    let mgr = get_mgr!(None);
    
    if let Some(hash_arr) = mgr.get_block_hash(height) {
        let h_raw = mgr.get_header_raw(&hash_arr);
        let b_raw = mgr.get_block_body_raw(&hash_arr);
        
        log::debug!("[DEEP-PROBE] 🔎 Khối #{}: Header={}, Body={}", 
            height, h_raw.is_some(), b_raw.is_some());

        // [GHOST-RECOVERY V1.2] 🛡️ Cơ chế phục hồi mỏ neo
        // Nếu thiếu Header/Body trong DB nhưng có Hash trong mục lục:
        // Chúng ta sẽ khởi tạo một khung khối "Ma" với các thông tin tối thiểu để Miner có thể tiếp tục.
        
        let header = match h_raw {
            Some(ref raw) => BlockHeader::decode(&raw[..]).ok(),
            None => {
                // [ANCHOR-RECOVERY] Tuyệt đối không tự ý gán Timestamp giả định. 
                // Hệ thống phải yêu cầu Header chuẩn từ Peer để bảo vệ tính toàn vẹn LWMA.
                println!("[RECOVERY] 🛑 THẤT BẠI: Thiếu Header cho khối #{}. Không thể phục hồi bằng giả định.", height);
                return None;
            }
        };

        let oldest_h = mgr.get_oldest_height();
        let body = match b_raw {
            Some(ref raw) => BlockBody::decode(&raw[..]).ok(),
            None => {
                // Chỉ cho phép trả về BlockBody::default() nếu height < oldestHeight (vùng đại thanh trừng / snapshot nhảy cóc)
                if height < oldest_h && h_raw.is_some() {
                    log::debug!("[SCL] 📦 Khối #{} đã được thanh lọc (Pruned) hoặc Snapshot nhảy cóc. Trả về Header-Only.", height);
                    Some(BlockBody::default())
                } else {
                    None
                }
            }
        };

        if let (Some(h), Some(b)) = (header, body) {
            let full_block = Block {
                header: Some(h),
                body: Some(b),
            };
            return Some(full_block.encode_to_vec());
        }
    } else {
        log::debug!("[DEEP-PROBE] ❌ Không tìm thấy mã băm cho cao độ #{}", height);
    }
    None
}

pub fn get_block_hash(height: u64) -> Vec<u8> {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return Vec::new(),
    };
    mgr.get_block_hash(height).map(|h| h.to_vec()).unwrap_or_default()
}

pub fn get_raw_by_hash(hash: Vec<u8>) -> Option<Vec<u8>> {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi tìm dữ liệu thô bằng mã hash khối.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return None,
    };
    if hash.len() != 32 { return None; }
    let mut h_arr = [0u8; 32];
    h_arr.copy_from_slice(&hash);

    // 1. Thử tìm trong Header CF để ghép nối thành khối Block đầy đủ
    if let Some(h_raw) = mgr.get_header_raw(&h_arr) {
        if let Ok(header) = BlockHeader::decode(&h_raw[..]) {
            let oldest_h = mgr.get_oldest_height();
            // Thử lấy Body tương ứng, nếu thiếu chỉ cho phép dùng Body trống nếu dưới mốc đại thanh trừng / snapshot nhảy cóc
            let body = match mgr.get_block_body_raw(&h_arr) {
                Some(b_raw) => BlockBody::decode(&b_raw[..]).ok(),
                None => {
                    if header.height < oldest_h {
                        Some(BlockBody::default())
                    } else {
                        None
                    }
                }
            };
            
            if let Some(b) = body {
                let full_block = Block {
                    header: Some(header),
                    body: Some(b),
                };
                return Some(full_block.encode_to_vec());
            }
        }
    }
    
    // 2. Fallback: Thử tìm trong Body CF nếu không có Header
    if let Some(data) = mgr.get_block_body_raw(&h_arr) {
        return Some(data);
    }
    None
}


pub fn save_block_raw(
    height: u64,
    hash: Vec<u8>,
    data: Vec<u8>,
    is_canonical: bool,
    absolute_weight: Vec<u8>, // [VANGUARD-FIX] Trọng lượng tự tính bởi Node
) -> bool {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi lưu khối mới.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return false,
    };
    if hash.len() != 32 { return false; }
    let mut h_arr = [0u8; 32];
    h_arr.copy_from_slice(&hash);

    // 1. Giải phẫu khối dữ liệu thô
    if let Ok(mut full_block) = Block::decode(&data[..]) {
        if let Some(ref mut header) = full_block.header.as_mut() {
            // [VANGUARD-FIX] Cập nhật trọng lượng chuẩn (Trusted by Node)
            if !absolute_weight.is_empty() {
                header.absolute_weight = absolute_weight;
            }

            // [DETERMINISM-FIX] Kiểm tra tính toàn vẹn của mã Hash trước khi ghi
            let tx_root = header.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
            let parent_h = header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default();
            let packed = crate::genz_pow::pack_header_v112(
                header.height,
                &parent_h,
                header.timestamp,
                &tx_root,
                &header.difficulty
            );
            let recomputed_hash = crate::crypto_primitives::calculate_blake3_hash(packed.to_vec(), header.height);
            
            if recomputed_hash != h_arr {
                log::error!("[DATABASE-SECURITY] 🚨 MÃ BĂM KHÔNG KHỚP! Từ chối ghi khối #{} để bảo vệ tính toàn vẹn.", header.height);
                return false; 
            }

            // 2. Lưu Header (Chốt chặn bất biến)
            mgr.put_header(&h_arr, <BlockHeader as Message>::encode_to_vec(header));
            
            // 3. Lưu Body (Chỉ lưu nếu có - Tránh ép buộc dữ liệu lịch sử)
            if let Some(body) = full_block.body {
                if !body.transactions.is_empty() {
                    mgr.put_block_body(&h_arr, <BlockBody as Message>::encode_to_vec(&body));
                }
            } else {
                log::info!("[DATABASE-LIGHT] 🕊️ Lưu mỏ neo Header-Only cho khối #{}", header.height);
            }

            // 4. Nếu là khối chính thống, chốt Hash cho Height
            if is_canonical {
                mgr.put_block_hash(height, &h_arr);
            }
            return true;
        }
    }
    false
}

// Hàm nội bộ dành cho Thợ đào (Không xuất khẩu qua UniFFI để tránh lách luật Firewall)
pub fn verify_pow_raw(header_bytes: Vec<u8>, nonce: u64, difficulty: Vec<u8>, height: u64) -> bool {
    let result = verify_pow(header_bytes, nonce, difficulty, height);
    match result {
        BlockVerificationResult::Success => true,
        _ => false,
    }
}


pub fn find_nonce(header_hash: Vec<u8>, _start_nonce: u64, difficulty: Vec<u8>, iterations: u32, thread_count: u32, height: u64) -> Option<u64> {
    genz_pow::find_nonce(header_hash, _start_nonce, difficulty, iterations, thread_count, height)
}


pub fn is_valid_fee(fee: u64) -> bool {
    fee_logic::is_valid_fee(fee)
}


pub fn calculate_transaction_fee(amount: u64, n: u32) -> u64 {
    fee_logic::calculate_transaction_fee(amount, n)
}


pub fn calculate_nano_fee(base_fee: u64, _amount: u64, _n: u32, _is_anon: bool) -> u64 {
    // V1.19: Loại bỏ hoàn toàn hệ số nhân cho giao dịch nhỏ.
    // Kiểm tra tính hợp lệ của base_fee trước khi chấp nhận.
    if fee_logic::is_valid_fee(base_fee) {
        base_fee
    } else {
        fee_logic::FEE_STANDARD
    }
}


pub fn put_transaction_receipt(tx_hash: Vec<u8>, height: u64, status: u32) {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi lưu hóa đơn giao dịch.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return,
    };
    mgr.put_transaction_receipt(&tx_hash, height, status);
}

pub fn get_coinbase_raw(hash: Vec<u8>) -> Option<Vec<u8>> {
    if hash.len() != 32 { return None; }
    let mut h_arr = [0u8; 32];
    h_arr.copy_from_slice(&hash);
    
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi đọc dữ liệu khối đào (coinbase).
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return None,
    };
    mgr.get_coinbase_raw(&h_arr)
}


pub fn get_transaction_status(tx_hash: Vec<u8>) -> TransactionStatus {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return TransactionStatus::default(),
    };
    let cf = mgr.db.cf_handle(state_manager::CF_RECEIPTS).expect("Missing RECEIPTS CF");
    match mgr.db.get_cf(cf, &tx_hash).unwrap_or(None) {
        Some(data) => {
            let tx = state_manager::TrackedTx::try_from_slice(&data).unwrap_or_default();
            let current_h = mgr.get_current_version();
            let finalized_h = mgr.get_finalized_height();
            
            let mut confirmations = 0;
            let mut is_finalized = false;
            
            if tx.block_height > 0 {
                confirmations = (current_h as i64 - tx.block_height as i64 + 1).max(0) as u64;
                is_finalized = tx.block_height <= finalized_h && finalized_h > 0;
            }
            
            TransactionStatus { 
                height: tx.block_height, 
                status: tx.status, 
                is_finalized, 
                confirmations,
                sender_prev_balance: tx.sender_prev_balance,
                sender_post_balance: tx.sender_post_balance,
                receiver_prev_balance: tx.receiver_prev_balance,
                receiver_post_balance: tx.receiver_post_balance,
            }
        }
        None => TransactionStatus::default(),
    }
}

pub fn get_transaction_status_batch(tx_hashes: Vec<Vec<u8>>) -> Vec<TransactionStatus> {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return vec![TransactionStatus::default(); tx_hashes.len()],
    };
    let current_h = mgr.get_current_version();
    let finalized_h = mgr.get_finalized_height();
    let receipt_cf = mgr.db.cf_handle(state_manager::CF_RECEIPTS).expect("Missing RECEIPTS CF");

    tx_hashes.into_iter().map(|tx_hash| {
        match mgr.db.get_cf(receipt_cf, &tx_hash).unwrap_or(None) {
            Some(data) => {
                let tx = state_manager::TrackedTx::try_from_slice(&data).unwrap_or_default();
                let mut confirmations = 0;
                let mut is_finalized = false;
                if tx.block_height > 0 {
                    confirmations = (current_h as i64 - tx.block_height as i64 + 1).max(0) as u64;
                    is_finalized = tx.block_height <= finalized_h && finalized_h > 0;
                }
                TransactionStatus {
                    height: tx.block_height,
                    status: tx.status,
                    is_finalized,
                    confirmations,
                    sender_prev_balance: tx.sender_prev_balance,
                    sender_post_balance: tx.sender_post_balance,
                    receiver_prev_balance: tx.receiver_prev_balance,
                    receiver_post_balance: tx.receiver_post_balance,
                }
            }
            None => TransactionStatus::default(),
        }
    }).collect()
}


pub fn get_actual_total_supply() -> u64 {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    mgr.get_actual_total_supply()
}



pub fn get_finalized_height() -> u64 {
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    mgr.get_finalized_height()
}


pub fn set_finalized_height(height: u64) {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi cập nhật mốc khối xác định (finalized height).
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return,
    };
    mgr.set_finalized_height(height);
}

pub fn force_set_finalized_height(height: u64) {
    // [VANGUARD-FIX] Tránh hoảng loạn (panic) khi ép buộc mốc khối xác định.
    let mgr = match state_manager::get_state_manager() {
        Some(m) => m,
        None => return,
    };
    mgr.force_set_finalized_height(height);
}


pub fn get_state_root() -> Vec<u8> {
    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => return vec![0u8; 32],
    };
    mgr.get_state_root().to_vec()
}


pub fn get_current_version() -> u64 {
    if let Some(mgr) = crate::state_manager::get_state_manager() {
        mgr.get_current_version()
    } else {
        0
    }
}

// [FIX V2.0] Định nghĩa trùng lặp đã bị xóa. Sử dụng get_block_body_raw ở trên.

pub fn calculate_actual_total_supply() -> u64 {
    let mgr = crate::state_manager::get_state_manager().expect("StateManager not initialized");
    mgr.calculate_actual_total_supply_full_scan()
}


pub fn get_expected_supply() -> u64 {
    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => return 0,
    };
    mgr.get_expected_supply()
}


pub fn calculate_expected_supply(height: u64) -> u64 {
    crate::reward_logic::calculate_expected_supply_from_genesis_fallback(height)
}


pub fn get_merkle_proof_ffi(address: Vec<u8>) -> Vec<Vec<u8>> {
    let mgr = state_manager::get_state_manager().expect("StateManager not initialized");
    if address.len() != 32 { return Vec::new(); }
    let mut addr = [0u8; 32];
    addr.copy_from_slice(&address);
    let proof = mgr.get_merkle_proof(&addr);
    proof.iter().map(|p| p.to_vec()).collect()
}


pub fn calculate_merkle_root(flat_hashes: Vec<u8>) -> Vec<u8> {
    let mut hashes = Vec::new();
    for chunk in flat_hashes.chunks(32) {
        if chunk.len() == 32 {
            let mut h = [0u8; 32];
            h.copy_from_slice(chunk);
            hashes.push(h);
        }
    }
    merkle::calculate_merkle_root(hashes).to_vec()
}






// --- [V1.0 FINAL] COMPACT BLOCK PROPAGATION LOGIC ---

/// [BIP152 Style] Sinh ID 64-bit rút gọn cho giao dịch để truyền tải khối siêu tốc.

pub fn calculate_short_tx_id_ffi(tx_hash: Vec<u8>, nonce: u64, height: u64) -> u64 {
    if tx_hash.len() != 32 { return 0; }
    let mut data = tx_hash;
    data.extend_from_slice(&nonce.to_le_bytes());
    let hash = crypto_primitives::calculate_blake3_hash_ffi(data, height);
    let mut bytes = [0u8; 8];
    bytes.copy_from_slice(&hash[0..8]);
    u64::from_le_bytes(bytes)
}


/// Xác thực tính toàn vẹn của khối vừa được lắp ráp tại chỗ (Local Reconstruction).

pub fn verify_block_reconstruction(tx_root: Vec<u8>, tx_hashes: Vec<Vec<u8>>) -> bool {
    if tx_root.len() != 32 { return false; }
    let mut hashes = Vec::new();
    for h in tx_hashes {
        if h.len() == 32 {
            let mut h_arr = [0u8; 32];
            h_arr.copy_from_slice(&h);
            hashes.push(h_arr);
        } else {
            return false;
        }
    }
    let calculated_root = merkle::calculate_merkle_root(hashes);
    calculated_root.as_ref() == tx_root
}


pub fn set_mining_pause(pause: bool) {
    log::info!("[FFI-MINER] 🕹️ Lệnh điều khiển từ UI: PAUSE = {}", pause);
    genz_pow::PAUSE_MINING.store(pause, std::sync::atomic::Ordering::Relaxed);
}

pub fn get_account_state(address: &[u8; 32]) -> AccountState {
    if let Some(mgr) = state_manager::get_state_manager() {
        mgr.get_account_state(address)
    } else {
        AccountState::default()
    }
}

pub fn update_account_at_height(address: &[u8; 32], state: AccountState, height: u64) {
    if let Some(mgr) = state_manager::get_state_manager() {
        mgr.update_account_at_height(address, state, height);
    }
}


pub fn is_mining_paused() -> bool {
    genz_pow::PAUSE_MINING.load(std::sync::atomic::Ordering::Relaxed)
}


/// [VANGUARD-V2] Block Proposer tập trung tại Rust Core.
/// Hàm này xây dựng một Block Template hoàn chỉnh, bao gồm giao dịch Coinbase
/// và tính toán StateRoot thông qua việc mô phỏng thực thi (Dry-run).
pub fn build_vanguard_block_template(
    height: u64,
    parent_hash: Vec<u8>,
    miner_address: Vec<u8>,
    transactions_bytes: Vec<Vec<u8>>,
    timestamp: u64,
    difficulty_raw: Vec<u8>,
) -> Vec<u8> {
    if height == 0 {
        log::error!("[VANGUARD-BUILDER] 🚨 Chặn nỗ lực tự tạo template khai thác cho khối Genesis (khối #0)!");
        let res = BlockTemplateResult {
            block_raw: Vec::new(),
            success: false,
            error_msg: "Genesis block mining is forbidden. It must be synchronized or loaded from config.".to_string(),
            failing_tx_index: -1,
        };
        return borsh::to_vec(&res).unwrap_or_default();
    }
    log::info!("[VANGUARD-BUILDER] 🏗️ Đang xây dựng Template cho khối #{}...", height);

    // 1. Giải mã danh sách giao dịch từ phía Go (Mempool)
    let mut transactions = Vec::new();
    for tx_bytes in transactions_bytes {
        if let Ok(tx) = Transaction::decode(&tx_bytes[..]) {
            transactions.push(tx);
        }
    }

    // 2. Khởi tạo Giao dịch Thưởng (Coinbase)
    // [RULE] Coinbase luôn nằm ở vị trí Index 0 của khối.
    let mut coinbase_tx = Transaction::default();
    coinbase_tx.version = 1;
    coinbase_tx.amount = 0; // Phần thưởng thực tế được tính toán và cấp phát trong lõi execute_block_transactions
    coinbase_tx.fee = 0;
    coinbase_tx.nonce = 0;
    coinbase_tx.timestamp = timestamp;
    coinbase_tx.receiver = Some(crate::proto::common::Address { value: miner_address.clone() });
    
    // Giao dịch Coinbase không có sender và không cần chữ ký (Nó được bảo vệ bởi PoW của Block)
    
    // [VANGUARD-DDoS-SHIELD] Thực hiện dry-run một lượt duy nhất thông qua execute_block_internal
    // để lọc sạch toàn bộ giao dịch lỗi (như sai nonce, không đủ số dư) khỏi Block Template ngay tại Rust Core.
    // Tại sao: Thiết kế này loại bỏ vòng lặp O(N*M) trước đó, giảm độ phức tạp thuật toán xuống O(N) tuyến tính,
    // triệt tiêu hoàn toàn nguy cơ nghẽn gRPC thread của Miner dưới tải stress test cực cao.
    let mut trial_transactions = Vec::new();
    trial_transactions.push(coinbase_tx.clone());
    trial_transactions.extend(transactions);

    let trial_body = BlockBody { transactions: trial_transactions };
    let body_raw = trial_body.encode_to_vec();

    // Thực thi dry-run 1 lần duy nhất để lấy các giao dịch thực sự hợp lệ và StateRoot
    let (final_transactions, state_root_bytes, valid_tx_hashes) = match execute_block_internal(
        body_raw,
        miner_address.clone(),
        parent_hash.clone(),
        height,
        true, // is_simulation = true -> Không ghi dữ liệu thật vào DB
    ) {
        Ok((payload, _, valid_txs)) => {
            (valid_txs, payload.final_root.to_vec(), payload.tx_hashes)
        }
        Err(e) => {
            // Lỗi hệ thống nghiêm trọng không do giao dịch người dùng (ví dụ: lỗi Coinbase hoặc hụt cung tiền)
            log::error!("[VANGUARD-BUILDER] ❌ Lỗi hệ thống không thể tự sửa trong dry-run: {} (FailIdx: {})", e.message, e.failing_tx_index);
            let res = BlockTemplateResult {
                block_raw: Vec::new(),
                success: false,
                error_msg: e.message,
                failing_tx_index: e.failing_tx_index,
            };
            return borsh::to_vec(&res).unwrap_or_default();
        }
    };

    // 3. Tính toán TxRoot (Merkle Root) cho toàn bộ danh sách giao dịch thực sự hợp lệ cực kỳ nhanh
    let tx_root_bytes = merkle::calculate_merkle_root(valid_tx_hashes).to_vec();

    // 4. Xây dựng Body khối thô từ các giao dịch hợp lệ
    let body = BlockBody { transactions: final_transactions };

    // 5. Tính toán Absolute Weight (Trọng số tích lũy)
    // [VANGUARD-CONSENSUS] Miner phải điền đúng trọng số này để Syncer có thể so sánh chuỗi nặng nhất.
    let abs_weight = {
        let mgr = state_manager::get_state_manager().expect("StateManager not initialized");
        
        let parent_weight = if height > 0 {
            let mut p_hash_arr = [0u8; 32];
            if parent_hash.len() == 32 {
                p_hash_arr.copy_from_slice(&parent_hash);
            }
            
            if let Some(raw) = mgr.get_header_raw(&p_hash_arr) {
                if let Ok(ph) = BlockHeader::decode(&raw[..]) {
                    ph.absolute_weight
                } else { Vec::new() }
            } else { Vec::new() }
        } else { Vec::new() };
        
        calculate_absolute_weight(parent_weight, difficulty_raw.clone())
    };

    // 6. Hoàn thiện Block Header
    let header = BlockHeader {
        version: 1,
        height,
        parent_hash: Some(crate::proto::common::Hash { value: parent_hash }),
        state_root: Some(crate::proto::common::Hash { value: state_root_bytes }),
        tx_root: Some(crate::proto::common::Hash { value: tx_root_bytes }),
        timestamp,
        difficulty: difficulty_raw,
        nonce: 0, // Sẽ được Mining Engine (Go/Rust) điền vào sau khi băm thành công
        miner_address: Some(crate::proto::common::Address { value: miner_address }),
        absolute_weight: abs_weight, 
        manifesto: "YonaCode - Decentralized Future".to_string(),
    };

    // 7. Đóng gói Khối đầy đủ
    let full_block = Block {
        header: Some(header),
        body: Some(body),
    };

    // Serialized theo Protobuf lồng trong BlockTemplateResult
    let block_bytes = full_block.encode_to_vec();
    let res = BlockTemplateResult {
        block_raw: block_bytes,
        success: true,
        error_msg: String::new(),
        failing_tx_index: -1,
    };
    borsh::to_vec(&res).unwrap_or_default()
}



pub fn export_state_snapshot() -> Vec<AccountSnapshot> {
    let mgr = crate::state_manager::get_state_manager().expect("StateManager not initialized");
    mgr.export_state_snapshot()
}


pub fn import_state_snapshot(snapshot: Vec<AccountSnapshot>, version: u64) -> ExecutionResult {
    println!("[DEBUG] Nhận được Snapshot Array có độ dài: {}", snapshot.len());
    let mgr = crate::state_manager::get_state_manager().expect("StateManager not initialized");
    
    let root = match mgr.import_state_snapshot(snapshot, version) {
        Ok(r) => r.to_vec(),
        Err(e) => return ExecutionResult { state_root: vec![], success: false, error_msg: e.to_string(), failing_tx_index: -1 },
    };
    
    // [VANGUARD-FIX] LÀM ĐÚNG BẢN CHẤT MÁY TRẠNG THÁI!
    // Tại thời điểm Snapshot (version), Tổng cung là CHÂN LÝ TOÁN HỌC.
    // XÓA BỎ LỆNH GÂY LỖI: mgr.calculate_actual_total_supply_full_scan() (O(N) ngớ ngẩn gây nghẽn)
    let expected_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(version);
    
    // Ghi thẳng chân lý vào RAM và DB
    mgr.set_actual_total_supply(expected_supply);
    mgr.set_expected_supply(expected_supply);
    
    println!("[AUDIT] ✅ Nạp Snapshot thành công! Base Supply chốt tại Khối #{}: {} VNT", version, expected_supply);
    ExecutionResult { state_root: root, success: true, error_msg: "".into(), failing_tx_index: -1 }
}


pub fn export_state_snapshot_raw() -> Vec<u8> {
    let mgr = crate::state_manager::get_state_manager().expect("StateManager not initialized");
    let snapshot = mgr.export_state_snapshot();
    borsh::to_vec(&snapshot).expect("Borsh serialization failed")
}

pub fn export_state_snapshot_at_height_raw(height: u64) -> Vec<u8> {
    let mgr = crate::state_manager::get_state_manager().expect("StateManager not initialized");
    let snapshot = mgr.export_state_snapshot_at_version(height);
    borsh::to_vec(&snapshot).expect("Borsh serialization failed")
}


pub fn import_state_snapshot_raw(data: Vec<u8>, version: u64) -> ExecutionResult {
    let snapshot: Vec<AccountSnapshot> = match Vec::<AccountSnapshot>::try_from_slice(&data) {
        Ok(s) => s,
        Err(e) => return ExecutionResult { state_root: vec![], success: false, error_msg: format!("Borsh deserialization failed: {}", e), failing_tx_index: -1 },
    };
    import_state_snapshot(snapshot, version)
}





pub fn get_address_type(addr: Vec<u8>) -> i32 {
    if addr.len() == 32 { 1 } else { 0 }
}



// --- DAG Stubs ---
// [Audit V10.4 FINAL] Đã xóa bỏ hoàn toàn (Khai tử DAG logic).
pub fn start_mining_v2_ffi(task_bytes: Vec<u8>) -> Vec<u8> {
    genz_pow::start_mining_v2_internal(task_bytes)
}

pub fn submit_mining_task_ffi(task_bytes: Vec<u8>) {
    genz_pow::submit_mining_task_internal(task_bytes)
}

pub fn get_mining_result_ffi() -> Vec<u8> {
    genz_pow::get_mining_result_internal()
}

pub fn delete_by_hash_ffi(hash: Vec<u8>) -> bool {
    if hash.len() != 32 { return false; }
    let mut h = [0u8; 32];
    h.copy_from_slice(&hash);
    if let Some(mgr) = state_manager::get_state_manager() {
        mgr.delete_by_hash(&h).is_ok()
    } else {
        false
    }
}

pub fn import_state_snapshot_path(path: String, version: u64) -> ExecutionResult {
    use std::fs::File;
    use std::io::{BufReader, Read};
    use borsh::BorshDeserialize;
    use rocksdb::WriteBatch;
    use std::sync::atomic::Ordering;
    use jmt::storage::TreeWriter;
    use crate::state_manager::{CF_ACC, CF_ACC_HISTORY, CF_KEYHASH_TO_ADDR, CF_META, CF_JMT};

    log::info!("🧬 [IMPORT-PATH] Bắt đầu nạp Snapshot Streaming từ file: {} tại phiên bản: #{}", path, version);
    
    let file = match File::open(&path) {
        Ok(f) => f,
        Err(e) => {
            return ExecutionResult { 
                state_root: vec![], 
                success: false, 
                error_msg: format!("Không thể mở file: {} | Lỗi: {}", path, e),
                failing_tx_index: -1,
            };
        }
    };
    
    let mut reader = BufReader::new(file);
    
    // [VANGUARD-STREAMING-FIX]
    // Vì file snapshot được serialize từ một Vec<AccountSnapshot> qua borsh::to_vec(&snapshot),
    // 4 byte đầu tiên của file chứa độ dài của Vector (u32).
    // Chúng ta cần đọc và bỏ qua 4 byte này để reader trỏ đúng vào phần tử AccountSnapshot đầu tiên.
    let mut len_bytes = [0u8; 4];
    if let Err(e) = reader.read_exact(&mut len_bytes) {
        return ExecutionResult {
            state_root: vec![],
            success: false,
            error_msg: format!("Không thể đọc kích thước Vector ở đầu file: {}", e),
            failing_tx_index: -1,
        };
    }
    let num_accounts = u32::from_le_bytes(len_bytes);
    log::info!("📊 [IMPORT-PATH] Phát hiện tổng số tài khoản khai báo trong file: {}", num_accounts);

    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: "StateManager chưa được khởi tạo".into(),
                failing_tx_index: -1,
            };
        }
    };

    let acc_cf = match mgr.db.cf_handle(CF_ACC) {
        Some(cf) => cf,
        None => {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: "Không tìm thấy ACC Column Family".into(),
                failing_tx_index: -1,
            };
        }
    };
    let history_cf = match mgr.db.cf_handle(CF_ACC_HISTORY) {
        Some(cf) => cf,
        None => {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: "Không tìm thấy ACC_HISTORY Column Family".into(),
                failing_tx_index: -1,
            };
        }
    };
    let cf_kh_to_addr = mgr.db.cf_handle(CF_KEYHASH_TO_ADDR);

    // ================= [SNAP-SYNC-CLEAN-SLATE] =================
    // Tại sao: Khi thực hiện Fast Sync qua snapshot, RocksDB hiện tại của node có thể đang chứa dữ liệu rác
    // hoặc dữ liệu cũ từ các block đã chạy thử trước đó. Nếu không xóa sạch dữ liệu phẳng cũ (CF_ACC, CF_JMT, v.v.),
    // các tài khoản rác này sẽ không bị xóa và vẫn tồn tại trong DB, làm lệch pha StateRoot JMT sau khi rebuild.
    // [VANGUARD-CLEAN-SLATE] Sử dụng cận xóa &[] đến &[0xffu8; 128] để đảm bảo bao phủ và xóa triệt để 100% mọi key.
    // Đồng thời xóa các dữ liệu metadata cũ (rightmost_leaf, rm_jmt_nodes) trong CF_META để tránh JMT rebuild đọc dữ liệu tương lai.
    log::info!("🧹 [IMPORT-PATH] Thực hiện dọn sạch các Column Family cũ trước khi nạp snapshot...");
    let start_all: &[u8] = &[];
    let end_all: &[u8] = &[0xffu8; 128];
    if let Some(cf) = mgr.db.cf_handle(CF_ACC) {
        let _ = mgr.db.delete_range_cf(cf, start_all, end_all);
    }
    if let Some(cf) = mgr.db.cf_handle(CF_JMT) {
        let _ = mgr.db.delete_range_cf(cf, start_all, end_all);
    }
    if let Some(cf) = mgr.db.cf_handle(CF_ACC_HISTORY) {
        let _ = mgr.db.delete_range_cf(cf, start_all, end_all);
    }
    if let Some(cf) = cf_kh_to_addr {
        let _ = mgr.db.delete_range_cf(cf, start_all, end_all);
    }
    if let Some(cf) = mgr.db.cf_handle(CF_META) {
        let _ = mgr.db.delete_cf(cf, b"rightmost_leaf");
        let _ = mgr.db.delete_cf(cf, format!("rm_{}", CF_JMT).as_bytes());
        let _ = mgr.db.delete_cf(cf, format!("rm_{}", crate::state_manager::CF_ACC_SYNC_STAGING).as_bytes());
    }
    log::info!("🧹 [IMPORT-PATH] Hoàn tất làm sạch cơ sở dữ liệu cũ và dữ liệu rác metadata. Bắt đầu nạp dữ liệu.");

    let mut write_batch = WriteBatch::default();
    let mut value_set = Vec::with_capacity(num_accounts as usize);
    let mut batch_count = 0;
    let mut total_processed = 0;

    let batch_size = 20000;
    let mut temp_batch = Vec::with_capacity(batch_size);

    loop {
        let mut eof = false;
        while temp_batch.len() < batch_size {
            match crate::AccountSnapshot::deserialize_reader(&mut reader) {
                Ok(acc) => temp_batch.push(acc),
                Err(_) => {
                    eof = true;
                    break;
                }
            }
        }

        if temp_batch.is_empty() {
            if eof { break; }
            continue;
        }

        let size = temp_batch.len();
        total_processed += size;
        batch_count += 1;
        log::info!("⚡ [IMPORT-PATH] Đang ghi lô phẳng #{} xuống RocksDB (gồm {} tài khoản, tiến trình: {}/{})", batch_count, size, total_processed, num_accounts);

        // Khử trùng lặp lô hiện tại
        let mut unique_map = std::collections::HashMap::new();
        for acc in temp_batch.drain(..) {
            let mut addr = [0u8; 32];
            if acc.address.len() == 32 { 
                addr.copy_from_slice(&acc.address); 
            }
            unique_map.insert(addr, acc);
        }

        for acc in unique_map.into_values() {
            let addr: [u8; 32] = match acc.address.try_into() {
                Ok(a) => a,
                Err(_) => continue,
            };
            
            let state = crate::state_manager::AccountState {
                btc_z: acc.balance,
                nonce: acc.nonce,
                nano_weight: acc.nano_weight,
                coin_id: acc.coin_id.try_into().unwrap_or([0u8; 32]),
                last_full_cleanup: acc.last_full_cleanup,
                maturing_rewards: acc.maturing_rewards,
            };
            
            let state_bytes = borsh::to_vec(&state).unwrap();
            write_batch.put_cf(acc_cf, addr, &state_bytes);
            
            let key_hash = jmt::KeyHash::with::<blake3::Hasher>(&addr);

            let mut versioned_key = [0u8; 40];
            versioned_key[0..32].copy_from_slice(&key_hash.0);
            versioned_key[32..40].copy_from_slice(&version.to_be_bytes());
            write_batch.put_cf(history_cf, &versioned_key, &state_bytes);

            if let Some(cf) = cf_kh_to_addr {
                write_batch.put_cf(cf, key_hash.0, addr);
            }

            // Lưu trữ thông số KeyHash tối giản vào RAM
            value_set.push((key_hash, Some(state_bytes)));
        }

        // Ghi thô lô tài khoản hiện tại vào RocksDB để giải phóng RAM
        if let Err(e) = mgr.db.write(write_batch) {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: format!("Lỗi ghi RocksDB batch: {}", e),
                failing_tx_index: -1,
            };
        }
        write_batch = WriteBatch::default(); // Reset batch

        if eof { break; }
    }

    log::info!("🌳 [IMPORT-PATH] Đang dựng cây Merkle JMT một lần duy nhất cho {} tài khoản...", value_set.len());

    // [JMT-LEXICOGRAPHICAL-FIX] BẮT BUỘC SẮP XẾP VALUE_SET THEO THỨ TỰ TĂNG DẦN CỦA KEYHASH
    // Triệt tiêu hoàn toàn lỗi lệch State Root do duyệt HashMap ngẫu nhiên hoặc đọc tuần tự từ file!
    value_set.sort_by(|a, b| a.0.0.cmp(&b.0.0));

    let jmt = jmt::JellyfishMerkleTree::<'_, crate::StateManager, blake3::Hasher>::new(&mgr);
    let mut current_root = [0u8; 32];
    match jmt.put_value_set(value_set, version) {
        Ok((new_root, batch)) => {
            current_root = new_root.0;
            if let Err(e) = mgr.write_node_batch(&batch.node_batch) {
                return ExecutionResult {
                    state_root: vec![],
                    success: false,
                    error_msg: format!("Lỗi ghi JMT nodes: {}", e),
                    failing_tx_index: -1,
                };
            }
        }
        Err(e) => {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: format!("JMT put_value_set thất bại: {}", e),
                failing_tx_index: -1,
            };
        }
    }

    // Cập nhật Metadata và Tổng cung sau khi hoàn tất nạp
    let meta_cf = match mgr.db.cf_handle(CF_META) {
        Some(cf) => cf,
        None => {
            return ExecutionResult {
                state_root: vec![],
                success: false,
                error_msg: "Không tìm thấy META Column Family".into(),
                failing_tx_index: -1,
            };
        }
    };

    let _ = mgr.db.put_cf(meta_cf, b"jmt_v", version.to_le_bytes());
    mgr.current_version.store(version, Ordering::SeqCst);
    let _ = mgr.db.put_cf(meta_cf, b"finalized_h", version.to_le_bytes());
    let _ = mgr.db.put_cf(meta_cf, b"lowest_full_height", version.to_le_bytes());

    let expected_supply = crate::reward_logic::calculate_expected_supply_from_genesis_fallback(version);
    mgr.set_actual_total_supply(expected_supply);
    mgr.set_expected_supply(expected_supply);

    log::info!("✅ [IMPORT-PATH] Hoàn tất nạp snapshot. Số tài khoản thực tế đã nạp: {}. Root JMT: {}, Supply: {}", 
        total_processed, hex::encode(current_root), expected_supply);

    ExecutionResult {
        state_root: current_root.to_vec(),
        success: true,
        error_msg: "".into(),
        failing_tx_index: -1,
    }
}
