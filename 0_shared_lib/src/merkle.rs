use rs_merkle::MerkleTree;

#[derive(Clone)]
pub struct Blake3Algorithm {}
impl rs_merkle::Hasher for Blake3Algorithm {
    type Hash = [u8; 32];
    fn hash(data: &[u8]) -> [u8; 32] {
        blake3::derive_key(crate::crypto_primitives::GENZ_POW_CONTEXT, data)
    }
}

pub static PAUSE_MINING: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

/// [V1.0] Xác thực một bằng chứng PoW duy nhất
/// [V13.0 INDUSTRIAL] Tính toán Merkle Root từ danh sách các hash (giao dịch)
/// Sử dụng rs_merkle (SIMD + Parallel Hashing) để đạt hiệu năng tối đa.
pub fn calculate_merkle_root(hashes: Vec<[u8; 32]>) -> [u8; 32] {
    if hashes.is_empty() {
        return [0u8; 32];
    }
    
    let merkle_tree = MerkleTree::<Blake3Algorithm>::from_leaves(&hashes);
    
    merkle_tree.root().unwrap_or([0u8; 32])
}
