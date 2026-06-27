/**
 * @file crypto_primitives.rs
 * @brief Wrapper cho các hàm mật mã học chuẩn của YonaCode V1.0.
 * @details Sử dụng Blake3 cho Hash và Ed25519 cho chữ ký.
 * Đã loại bỏ hoàn toàn các cơ chế Swarm PoW và Anonymous Proof cũ.
 * 
 * @author Vô Nhật Thiên (Khởi tạo) - YonaCode V1.0
 * @date 2026-03-24
 */

use blake3;
use ed25519_dalek::{VerifyingKey, Signature, Verifier, SigningKey, Signer};


pub const GENZ_POW_CONTEXT: &str = "BTC GenZ Toi Gian PoW v1.0";


// Hex: 2bc1cd01f2da4cb6747e051d095f1c2327ffff330feafcc75ea46f582a5b6cbc
pub const GENESIS_HASH: [u8; 32] = [
    0x2b, 0xc1, 0xcd, 0x01, 0xf2, 0xda, 0x4c, 0xb6,
    0x74, 0x7e, 0x05, 0x1d, 0x09, 0x5f, 0x1c, 0x23,
    0x27, 0xff, 0xff, 0x33, 0x0f, 0xea, 0xfc, 0xc7,
    0x5e, 0xa4, 0x6f, 0x58, 0x2a, 0x5b, 0x6c, 0xbc,
];


pub fn calculate_blake3_hash(data: Vec<u8>, _height: u64) -> [u8; 32] {
    // [VANGUARD CONSENSUS] Luôn sử dụng băm có Context bảo mật để chống ASIC.
    calculate_blake3_hash_vanguard(&data)
}

/// [V1.3.2 PERF] Phiên bản zero-copy — nhận tham chiếu &[u8] thay vì Vec<u8>.
#[inline(always)]
pub fn calculate_blake3_hash_ref(data: &[u8], _height: u64) -> [u8; 32] {
    blake3::derive_key(GENZ_POW_CONTEXT, data)
}

pub fn calculate_blake3_hash_vanguard(data: &[u8]) -> [u8; 32] {
    // [VANGUARD-V1.16] Sử dụng Keyed Context để vô hiệu hóa ASIC Blake3 tiêu chuẩn.
    // Chuỗi ngữ cảnh: "BTC GenZ Toi Gian PoW v1.0"
    blake3::derive_key(GENZ_POW_CONTEXT, data)
}

/// [V1.1.0] calculate_blake3_hash_ffi: Wrapper cho Go gọi băm chuẩn xác
pub fn calculate_blake3_hash_ffi(data: Vec<u8>, height: u64) -> Vec<u8> {
    calculate_blake3_hash(data, height).to_vec()
}

use std::collections::HashMap;
use std::sync::RwLock;
use lazy_static::lazy_static;

lazy_static! {
    // [VANGUARD-SIG-CACHE] Cache lưu kết quả xác thực chữ ký Ed25519 để tránh CPU storm dưới tải cao
    static ref SIG_CACHE: RwLock<HashMap<[u8; 32], bool>> = RwLock::new(HashMap::new());
}

/// [V1.0] Xác thực chữ ký Ed25519
pub fn verify_ed25519_signature(pub_key: &[u8; 32], message: &[u8], sig: &[u8; 64]) -> bool {
    // 1. Tạo Key duy nhất cho cache bằng cách băm Blake3 của (pub_key + message + sig)
    let mut cache_key_data = Vec::with_capacity(pub_key.len() + message.len() + sig.len());
    cache_key_data.extend_from_slice(pub_key);
    cache_key_data.extend_from_slice(message);
    cache_key_data.extend_from_slice(sig);
    let cache_key = calculate_blake3_hash_vanguard(&cache_key_data);

    // 2. Tra cứu nhanh trong Cache
    {
        let cache_read = SIG_CACHE.read().unwrap_or_else(|e| e.into_inner());
        if let Some(&valid) = cache_read.get(&cache_key) {
            return valid;
        }
    }

    // 3. Nếu chưa có trong cache, thực hiện xác thực Dalek Ed25519
    let Ok(verifying_key) = VerifyingKey::from_bytes(pub_key) else { 
        println!("🛡️  [CRYPTO-ERROR] Invalid Verifying Key!");
        return false; 
    };
    let signature = Signature::from_bytes(sig);
    let result = verifying_key.verify(message, &signature).is_ok();
    
    if !result {
        println!("🛡️  [CRYPTO-DEBUG] Signature Verification FAILED!");
        println!("🛡️  [CRYPTO-DEBUG]   Pubkey:  {}", hex::encode(pub_key));
        println!("🛡️  [CRYPTO-DEBUG]   Message: {}", hex::encode(message));
        println!("🛡️  [CRYPTO-DEBUG]   Sig:     {}", hex::encode(sig));
    }
    
    // 4. Lưu kết quả xác thực vào cache (DÙNG TRY_WRITE ĐỂ TRÁNH KẸT CỔ CHAI ĐA LUỒNG)
    if let Ok(mut cache_write) = SIG_CACHE.try_write() {
        if cache_write.len() > 100_000 {
            log::info!("[CRYPTO-CACHE] 🧹 Giải phóng Cache chữ ký để tránh phình to bộ nhớ.");
            cache_write.clear();
        }
        cache_write.insert(cache_key, result);
    }

    result
}

/// [V1.0] Ký dữ liệu bằng Private Key Ed25519
pub fn sign_ed25519(priv_key: &[u8; 32], message: &[u8]) -> [u8; 64] {
    let signing_key = SigningKey::from_bytes(priv_key);
    let signature = signing_key.sign(message);
    signature.to_bytes()
}

/// [V1.0] Xác thực lô (batch) chữ ký Ed25519 (Tối ưu hóa hiệu năng)
pub fn verify_ed25519_batch(pub_keys: &[[u8; 32]], messages: &[[u8; 32]], sigs: &[[u8; 64]]) -> bool {
    if pub_keys.len() != messages.len() || messages.len() != sigs.len() { return false; }
    if pub_keys.is_empty() { return false; }
    
    let mut v_keys = Vec::with_capacity(pub_keys.len());
    let mut v_sigs = Vec::with_capacity(sigs.len());
    let mut v_msgs = Vec::with_capacity(messages.len());
    
    for i in 0..pub_keys.len() {
        if let Ok(vk) = VerifyingKey::from_bytes(&pub_keys[i]) {
            v_keys.push(vk);
            v_sigs.push(Signature::from_bytes(&sigs[i]));
            v_msgs.push(&messages[i][..]);
        } else {
            return false;
        }
    }
    ed25519_dalek::verify_batch(&v_msgs, &v_sigs, &v_keys).is_ok()
}

use crate::proto::transaction::Transaction;
use prost::Message;

/// [VANGUARD-V2] Tính toán bản băm để ký tên (Bản băm của TX khi chưa có Signature)
/// Đã chuyển đổi sang băm các trường nối tiếp (Concatenation) nhị phân cố định (Little-Endian) để bảo đảm nhất quán 100% giữa Go và Rust.
pub fn calculate_signing_hash(tx: &Transaction) -> [u8; 32] {
    let mut buf = Vec::with_capacity(180);

    // 1. Version (uint64)
    buf.extend_from_slice(&tx.version.to_le_bytes());

    // 2. Sender
    if let Some(ref sender) = tx.sender {
        buf.extend_from_slice(&(sender.value.len() as u32).to_le_bytes());
        buf.extend_from_slice(&sender.value);
    } else {
        buf.extend_from_slice(&0u32.to_le_bytes());
    }

    // 3. Receiver
    if let Some(ref receiver) = tx.receiver {
        buf.extend_from_slice(&(receiver.value.len() as u32).to_le_bytes());
        buf.extend_from_slice(&receiver.value);
    } else {
        buf.extend_from_slice(&0u32.to_le_bytes());
    }

    // 4. Amount (uint64)
    buf.extend_from_slice(&tx.amount.to_le_bytes());

    // 5. Fee (uint64)
    buf.extend_from_slice(&tx.fee.to_le_bytes());

    // 6. Nonce (uint64)
    buf.extend_from_slice(&tx.nonce.to_le_bytes());

    // 7. Timestamp (uint64)
    buf.extend_from_slice(&tx.timestamp.to_le_bytes());

    // 8. Recent Block Hash
    buf.extend_from_slice(&(tx.recent_block_hash.len() as u32).to_le_bytes());
    buf.extend_from_slice(&tx.recent_block_hash);

    // 9. Chain ID (uint64)
    buf.extend_from_slice(&tx.chain_id.to_le_bytes());

    // 10. Băm Blake3 Vanguard
    let hash = calculate_blake3_hash_vanguard(&buf);
    // println!("🛡️  [NATIVE-DEBUG] Signing Hash (Vanguard Mode) = {}", hex::encode(hash));
    hash
}
/// [VANGUARD-V2] Tính toán TxID ổn định (Stable TxID)
/// Tại sao: TxID không được phép phụ thuộc vào cao độ khối (height) để đảm bảo 
/// định danh giao dịch nhất quán từ khi ở Mempool cho đến khi được chốt vào Ledger.
/// Luôn sử dụng Vanguard Hash để bảo mật tối đa.
pub fn calculate_tx_id(data: Vec<u8>) -> [u8; 32] {
    calculate_blake3_hash_vanguard(&data)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::common::{Address, Signature};

    #[test]
    fn test_vector_signing_hash() {
        let tx = Transaction {
            version: 1,
            sender: Some(Address {
                value: vec![0u8; 32],
            }),
            receiver: Some(Address {
                value: vec![1u8; 32],
            }),
            amount: 123456789,
            fee: 500,
            nonce: 42,
            timestamp: 1600000000,
            recent_block_hash: vec![2u8; 32],
            chain_id: 25062025,
            signature: Some(Signature {
                value: vec![9u8; 64],
            }),
        };
        let hash = calculate_signing_hash(&tx);
        println!("RUST_TEST_VECTOR_HASH = {}", hex::encode(hash));
        assert_eq!(hex::encode(hash), "c46a079def867e938ead12e558334605437e7e86b46bbbafad04fcaba2c9b9d6");
    }
}


