// [FORCE-REBUILD-V5.4.1]
/**
 * @file genz_pow.rs
 * @brief Hạt nhân Blake3-PoW (YonaCode V1.3.0 Standard U256).
 * @details Thuật toán ASIC-friendly, xác thực CPU cực nhanh bằng U256.
 */

use blake3;
use std::sync::atomic::{AtomicU64, AtomicU32, AtomicBool, Ordering};
use std::sync::Mutex;
use primitive_types::U256;
use prost::Message;
use crate::proto::consensus::{MiningTask, MiningResult};

pub static HASHRATE_COUNTER: AtomicU64 = AtomicU64::new(0);
pub static LAST_HASHRATE_TIMESTAMP: AtomicU64 = AtomicU64::new(0);
pub static PAUSE_MINING: AtomicBool = AtomicBool::new(true); // [V40.1] Mặc định TẠM DỪNG (Chỉ đào thủ công)
pub static HOT_SWAP_VERSION: AtomicU32 = AtomicU32::new(0);
pub static MINING_CURSOR: AtomicU64 = AtomicU64::new(0); // [V37.9] Ghi nhớ tiến trình băm toàn cục
pub static CURRENT_MINING_HEIGHT: AtomicU64 = AtomicU64::new(0);
/// [V27.0] Biến toàn cục cho Intensity — mọi luồng đọc trực tiếp, thay đổi tức thì
pub static CURRENT_INTENSITY: AtomicU32 = AtomicU32::new(100); 
pub static SYNC_VIOLATION_COUNT: AtomicU32 = AtomicU32::new(0); // [V40.2] Theo dõi vi phạm đồng bộ

lazy_static::lazy_static! {
    pub static ref LATEST_MINING_TASK: std::sync::Mutex<Option<MiningTask>> = std::sync::Mutex::new(None);
    // [IRON-HAND-V5] ThreadPool toàn cục - Tiết kiệm hàng triệu chu kỳ CPU khởi tạo
    static ref MINER_POOL: rayon::ThreadPool = {
        let total_cores = std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1);
        // [FIX] Bắt buộc chừa lại ít nhất 1 luồng xử lý cho Tokio gRPC và OS để chống nghẽn luồng và timeout gRPC.
        let miner_cores = if total_cores > 1 { total_cores - 1 } else { 1 };
        rayon::ThreadPoolBuilder::new()
            .num_threads(miner_cores)
            .thread_name(|i| format!("genz-miner-{}", i))
            .build()
            .unwrap()
    };
}

/// [V1.3.0] Xác thực một bằng chứng PoW duy nhất (U256 Standard)
pub fn verify_pow_raw(
    header_hash: Vec<u8>,
    nonce: u64,
    difficulty_raw: Vec<u8>,
    height: u64,
) -> bool {
    if header_hash.len() != 32 { return false; }

    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
    let mut diff_padded = [0u8; 32];
    let d_len = difficulty_raw.len().min(32);
    if d_len > 0 {
        diff_padded[..d_len].copy_from_slice(&difficulty_raw[..d_len]);
    }
    let difficulty = U256::from_little_endian(&diff_padded);
    let target = difficulty_to_target(difficulty);
    
    let mut material = [0u8; 40];
    material[..32].copy_from_slice(&header_hash);
    material[32..].copy_from_slice(&nonce.to_le_bytes());
    
    // [VANGUARD-CONSENSUS] Sử dụng băm có nhận diện cao độ để chọn thuật toán (Standard vs Derived)
    let hash_result = crate::crypto_primitives::calculate_blake3_hash(material.to_vec(), height);
    
    // So sánh U256 (Hash < Target)
    let hash_u256 = U256::from_little_endian(&hash_result);
    
    hash_u256 < target
}

use rayon::prelude::*;

/// [V1.3.1] Tìm kiếm Nonce (Optimized for i5/Multi-core)
/// Sử dụng Hasher Cloning và Rayon Parallelism.
pub fn find_nonce(
    header_hash: Vec<u8>,
    start_nonce: u64,
    difficulty_raw: Vec<u8>,
    iterations: u32,
    thread_count: u32,
    height: u64,
) -> Option<u64> {
    if header_hash.len() != 32 { return None; }
    // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
    let mut diff_padded = [0u8; 32];
    let d_len = difficulty_raw.len().min(32);
    if d_len > 0 {
        diff_padded[..d_len].copy_from_slice(&difficulty_raw[..d_len]);
    }
    let difficulty = U256::from_little_endian(&diff_padded);
    let target = difficulty_to_target(difficulty);

    // [V40.0] Bỏ qua kiểm tra PAUSE_MINING

    if height >= 38500 {
        let target_u64_last = target.0[3]; // Lấy 64-bit cao nhất để Fast-Reject
        let found_nonce = (0..iterations).into_par_iter().find_map_any(move |i| {
            let n = start_nonce.wrapping_add(i as u64);
            
            let mut material = [0u8; 40];
            material[..32].copy_from_slice(&header_hash);
            material[32..].copy_from_slice(&n.to_le_bytes());
            
            let hash_result = crate::crypto_primitives::yona_hash(&material);
            
            // Fast Reject: So sánh 64-bit cao nhất trước
            let h_last = u64::from_le_bytes(hash_result[24..32].try_into().unwrap());
            if h_last > target_u64_last {
                if i % 1000 == 0 { HASHRATE_COUNTER.fetch_add(1000, Ordering::Relaxed); }
                return None;
            }

            let hash_u256 = U256::from_little_endian(&hash_result);
            if hash_u256 < target {
                return Some(n);
            }
            
            if i % 1000 == 0 {
                HASHRATE_COUNTER.fetch_add(1000, Ordering::Relaxed);
            }
            None
        });
        return found_nonce;
    }

    // [VANGUARD-UNITY] Luôn sử dụng Vanguard cho mọi chiều cao khối.
    let mut mid_hasher = blake3::Hasher::new_derive_key(crate::crypto_primitives::GENZ_POW_CONTEXT);
    mid_hasher.update(&header_hash);

    let mid_h = mid_hasher;
    let target_u64_last = target.0[3]; // Lấy 64-bit cao nhất để Fast-Reject

    let found_nonce = (0..iterations).into_par_iter().find_map_any(move |i| {
        let n = start_nonce.wrapping_add(i as u64);
        
        let mut h = mid_h.clone();
        h.update(&n.to_le_bytes());
        let hash_result: [u8; 32] = h.finalize().into();
        
        // [VANGUARD-PERF] Fast Reject: So sánh 64-bit cao nhất trước
        let h_last = u64::from_le_bytes(hash_result[24..32].try_into().unwrap());
        if h_last > target_u64_last {
            if i % 1000 == 0 { HASHRATE_COUNTER.fetch_add(1000, Ordering::Relaxed); }
            return None;
        }

        let hash_u256 = U256::from_little_endian(&hash_result);
        if hash_u256 < target {
            return Some(n);
        }
        
        if i % 1000 == 0 {
            HASHRATE_COUNTER.fetch_add(1000, Ordering::Relaxed);
        }
        None
    });
    
    found_nonce
}

/// [V1.3.0] Chuyển đổi Độ khó sang Target 256-bit
pub fn difficulty_to_target(difficulty: U256) -> U256 {
    if difficulty <= U256::from(1) {
        return U256::MAX;
    }
    U256::MAX / difficulty
}

/// [V2.0.0] ENGINE API: Vòng lặp khai thác chính (Black Box)
/// Được gọi từ Go qua FFI, chạy liên tục cho đến khi tìm thấy hoặc bị ngắt.
lazy_static::lazy_static! {
    static ref MINING_SESSION_ID: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    static ref MINING_RESULT: std::sync::Mutex<Option<MiningResult>> = std::sync::Mutex::new(None);
    static ref WORKER_INIT: std::sync::Once = std::sync::Once::new();
}




/// [Vanguard V112] Gom các trường cốt lõi thành 112 bytes bất biến (Deterministic Packing)
/// Chú thích: Header bao gồm StateRoot và Reward đã được bao gồm trong merkle_root.
pub fn pack_header_v112(
    height: u64,
    parent_hash: &[u8],
    timestamp: u64,
    merkle_root: &[u8],
    difficulty: &[u8],
) -> [u8; 112] {
    let mut buf = [0u8; 112];
    
    // 1. Height (8 bytes, Little Endian)
    buf[0..8].copy_from_slice(&height.to_le_bytes());
    
    // 2. ParentHash (32 bytes)
    let p_len = parent_hash.len().min(32);
    buf[8..8+p_len].copy_from_slice(&parent_hash[..p_len]);
    
    // 3. Timestamp (8 bytes, Little Endian)
    buf[40..48].copy_from_slice(&timestamp.to_le_bytes());
    
    // 4. Merkle Root (32 bytes)
    let root_len = merkle_root.len().min(32);
    buf[48..48+root_len].copy_from_slice(&merkle_root[..root_len]);
    
    // 5. Difficulty (32 bytes, Raw Target)
    let d_len = difficulty.len().min(32);
    buf[80..80+d_len].copy_from_slice(&difficulty[..d_len]);

    buf
}



pub fn global_get_hashrate() -> u64 {
    HASHRATE_COUNTER.load(Ordering::Relaxed)
}