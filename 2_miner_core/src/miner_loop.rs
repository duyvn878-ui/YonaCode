/**
 * @file miner_loop.rs
 * @brief Vòng lặp khai thác (Mining Loop) của YonaCode.
 * @details Trái tim của cơ chế Proof-of-Work:
 *  - Tìm kiếm Nonce cho Slot 1 (Anchor) duy nhất để mở rộng chuỗi.
 * 
 * @author Vô Nhật Thiên (Khởi tạo) - V1.3.0 Standard U256
 */

use std::sync::atomic::{Ordering};
pub use btc_genz_scl::genz_pow::{PAUSE_MINING, pack_header_v112};
use std::time::{SystemTime, UNIX_EPOCH};
use primitive_types::U256;

pub struct MinerConfig {
    pub miner_address: Vec<u8>,
    pub thread_count: u32,
}

pub struct MiningTask {
    pub parent_hash: [u8; 32],
    pub state_root: [u8; 32],
    pub height: u64,
    pub difficulty: [u8; 32], // Little-endian U256
    pub miner_address: [u8; 32],
    pub tx_root: [u8; 32],
}

pub struct MiningResult {
    pub anchor_nonce: u64,
}

/// [ĐẶC TẢ V1.3.0] Vòng lặp PoW chuẩn U256 cho ASIC (Blake3)
impl MiningTask {
    /// [AUDIT V1.3.0] Tìm kiếm Anchor Nonce (Standard U256)
    pub fn find_anchor(&self, cancel: &std::sync::atomic::AtomicBool) -> Option<u64> {
        let diff_u256 = U256::from_little_endian(&self.difficulty);
        let target = btc_genz_scl::genz_pow::difficulty_to_target(diff_u256);

        println!("[MINER] ASIC-Mining khai hỏa khối #{} (Difficulty: {})", self.height, diff_u256);
        
        let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
        let header_buf = pack_header_v112(
            self.height,
            &self.parent_hash,
            now,
            &self.miner_address,
            &self.difficulty,
        );
        let header_hash = btc_genz_scl::crypto_primitives::calculate_blake3_hash(header_buf.to_vec(), self.height);

        let mut nonce = 0u64; // [FIX] Định nghĩa nonce bắt đầu từ 0
        loop {
            if cancel.load(Ordering::Relaxed) || PAUSE_MINING.load(Ordering::Relaxed) {
                return None;
            }

            let mut material = [0u8; 40]; // 32 (header_hash) + 8 (nonce)
            material[0..32].copy_from_slice(&header_hash);
            material[32..40].copy_from_slice(&nonce.to_le_bytes());

            let mut hasher = blake3::Hasher::new_derive_key(btc_genz_scl::crypto_primitives::GENZ_POW_CONTEXT);
            hasher.update(&material);
            let hash_result = hasher.finalize();
            let hash_u256 = U256::from_little_endian(hash_result.as_bytes());

            if hash_u256 < target {
                println!("[SUCCESS] Tìm thấy Anchor PoW: nonce={}", nonce);
                return Some(nonce);
            }

            nonce = nonce.wrapping_add(1);
            if nonce % 10000 == 0 {
                btc_genz_scl::genz_pow::HASHRATE_COUNTER.fetch_add(10000, Ordering::Relaxed);
            }
        }
    }
} // [FIX] Đóng block impl


