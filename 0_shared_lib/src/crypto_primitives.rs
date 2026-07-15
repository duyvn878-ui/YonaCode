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


// Hằng số khởi tạo (Initialization Vector) chuẩn Blake3 cho Yona Hash
const YONA_IV: [u32; 8] = [
    0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
    0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
];

const YONA_CHUNK_START: u32 = 1 << 0;
const YONA_CHUNK_END: u32 = 1 << 1;
const YONA_ROOT: u32 = 1 << 3;
const Y_KEY: u32 = 0x594F4E41; // ASCII "YONA"

macro_rules! yona_g {
    ($s:expr, $a:expr, $b:expr, $c:expr, $d:expr, $x:expr, $y:expr) => {
        $s[$a] = $s[$a].wrapping_add($s[$b]).wrapping_add($x ^ Y_KEY);
        $s[$d] = ($s[$d] ^ $s[$a]).rotate_right(17);
        $s[$c] = $s[$c].wrapping_add($s[$d]);
        $s[$b] = ($s[$b] ^ $s[$c]).rotate_right(13);
        $s[$a] = $s[$a].wrapping_add($s[$b]).wrapping_add($y ^ Y_KEY);
        $s[$d] = ($s[$d] ^ $s[$a]).rotate_right(9);
        $s[$c] = $s[$c].wrapping_add($s[$d]);
        $s[$b] = ($s[$b] ^ $s[$c]).rotate_right(5);
    };
}

macro_rules! yona_round {
    ($s:expr, $m:expr) => {
        yona_g!($s, 0, 4, 8, 12, $m[0], $m[1]);
        yona_g!($s, 1, 5, 9, 13, $m[2], $m[3]);
        yona_g!($s, 2, 6, 10, 14, $m[4], $m[5]);
        yona_g!($s, 3, 7, 11, 15, $m[6], $m[7]);
        yona_g!($s, 0, 5, 10, 15, $m[8], $m[9]);
        yona_g!($s, 1, 6, 11, 12, $m[10], $m[11]);
        yona_g!($s, 2, 7, 8, 13, $m[12], $m[13]);
        yona_g!($s, 3, 4, 9, 14, $m[14], $m[15]);
    };
}

macro_rules! yona_permute {
    ($m:expr) => {
        [
            $m[2], $m[6], $m[3], $m[10],
            $m[7], $m[0], $m[4], $m[13],
            $m[1], $m[11], $m[12], $m[5],
            $m[9], $m[14], $m[15], $m[8],
        ]
    };
}

#[inline(always)]
fn yona_compress(
    cv: &[u32; 8],
    m: &[u32; 16],
    counter: u64,
    block_len: u32,
    flags: u32,
) -> [u32; 16] {
    let mut s: [u32; 16] = [
        cv[0], cv[1], cv[2], cv[3],
        cv[4], cv[5], cv[6], cv[7],
        YONA_IV[0], YONA_IV[1], YONA_IV[2], YONA_IV[3],
        counter as u32, (counter >> 32) as u32, block_len, flags,
    ];

    yona_round!(s, m);
    let m2 = yona_permute!(m);
    yona_round!(s, m2);
    let m3 = yona_permute!(m2);
    yona_round!(s, m3);
    let m4 = yona_permute!(m3);
    yona_round!(s, m4);
    let m5 = yona_permute!(m4);
    yona_round!(s, m5);
    let m6 = yona_permute!(m5);
    yona_round!(s, m6);
    let m7 = yona_permute!(m6);
    yona_round!(s, m7);

    [
        s[0] ^ s[8],  s[1] ^ s[9],  s[2] ^ s[10], s[3] ^ s[11],
        s[4] ^ s[12], s[5] ^ s[13], s[6] ^ s[14], s[7] ^ s[15],
        s[8] ^ cv[0], s[9] ^ cv[1], s[10] ^ cv[2], s[11] ^ cv[3],
        s[12] ^ cv[4], s[13] ^ cv[5], s[14] ^ cv[6], s[15] ^ cv[7],
    ]
}

#[inline(always)]
fn yona_bytes_to_words(bytes: &[u8; 64]) -> [u32; 16] {
    let mut w = [0u32; 16];
    for i in 0..16 {
        let start = i * 4;
        w[i] = u32::from_le_bytes([
            bytes[start],
            bytes[start + 1],
            bytes[start + 2],
            bytes[start + 3],
        ]);
    }
    w
}

#[inline(always)]
fn yona_words_to_bytes(words: &[u32; 8]) -> [u8; 32] {
    let mut out = [0u8; 32];
    for i in 0..8 {
        let bytes = words[i].to_le_bytes();
        let start = i * 4;
        out[start] = bytes[0];
        out[start + 1] = bytes[1];
        out[start + 2] = bytes[2];
        out[start + 3] = bytes[3];
    }
    out
}

#[inline]
pub fn yona_hash(data: &[u8]) -> [u8; 32] {
    let len = data.len();
    let mut cv = YONA_IV;

    if len == 0 {
        let m = [0u32; 16];
        let out = yona_compress(&cv, &m, 0, 0, YONA_CHUNK_START | YONA_CHUNK_END | YONA_ROOT);
        return yona_words_to_bytes(
            <&[u32; 8]>::try_from(&out[..8]).unwrap()
        );
    }

    let total_blocks = (len + 63) / 64;
    let mut offset = 0usize;

    let mut idx = 0usize;
    while idx < total_blocks {
        let is_first = idx == 0;
        let is_last = idx == total_blocks - 1;
        let remaining = len - offset;
        let blen = if remaining >= 64 { 64 } else { remaining };

        let mut block = [0u8; 64];
        block[..blen].copy_from_slice(&data[offset..offset + blen]);
        let words = yona_bytes_to_words(&block);

        let mut flags = 0u32;
        if is_first { flags |= YONA_CHUNK_START; }
        if is_last { flags |= YONA_CHUNK_END | YONA_ROOT; }

        let out = yona_compress(&cv, &words, 0, blen as u32, flags);
        cv = [out[0], out[1], out[2], out[3], out[4], out[5], out[6], out[7]];

        offset += 64;
        idx += 1;
    }

    yona_words_to_bytes(&cv)
}

#[inline(always)]
pub fn yona_hash_112(header: &[u8; 112]) -> [u8; 32] {
    let mut block1 = [0u8; 64];
    block1.copy_from_slice(&header[0..64]);
    let w1 = yona_bytes_to_words(&block1);
    
    let out1 = yona_compress(&YONA_IV, &w1, 0, 64, YONA_CHUNK_START);
    let cv = [out1[0], out1[1], out1[2], out1[3], out1[4], out1[5], out1[6], out1[7]];

    let mut block2 = [0u8; 64];
    block2[0..48].copy_from_slice(&header[64..112]);
    let w2 = yona_bytes_to_words(&block2);

    let out2 = yona_compress(&cv, &w2, 0, 48, YONA_CHUNK_END | YONA_ROOT);
    
    yona_words_to_bytes(
        <&[u32; 8]>::try_from(&out2[..8]).unwrap()
    )
}

pub fn calculate_blake3_hash(data: Vec<u8>, height: u64) -> [u8; 32] {
    if height >= 38500 {
        if data.len() == 112 {
            let arr: &[u8; 112] = <&[u8; 112]>::try_from(&data[..]).unwrap();
            return yona_hash_112(arr);
        } else {
            return yona_hash(&data);
        }
    }
    calculate_blake3_hash_vanguard(&data)
}

/// [V1.3.2 PERF] Phiên bản zero-copy — nhận tham chiếu &[u8] thay vì Vec<u8>.
#[inline(always)]
pub fn calculate_blake3_hash_ref(data: &[u8], height: u64) -> [u8; 32] {
    if height >= 38500 {
        if data.len() == 112 {
            let arr: &[u8; 112] = <&[u8; 112]>::try_from(&data[..]).unwrap();
            return yona_hash_112(arr);
        } else {
            return yona_hash(data);
        }
    }
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

    #[test]
    fn test_yona_hash_fork() {
        let dummy_data_40 = [0u8; 40];
        let dummy_data_112 = [0u8; 112];

        // 1. Height below fork (38499) -> must use Blake3
        let hash_40_pre = calculate_blake3_hash(dummy_data_40.to_vec(), 38499);
        let hash_112_pre = calculate_blake3_hash(dummy_data_112.to_vec(), 38499);
        assert_ne!(hash_40_pre, yona_hash(&dummy_data_40));
        assert_ne!(hash_112_pre, yona_hash_112(&dummy_data_112));

        // 2. Height at fork (38500) -> must use Yona Hash
        let hash_40_post = calculate_blake3_hash(dummy_data_40.to_vec(), 38500);
        let hash_112_post = calculate_blake3_hash(dummy_data_112.to_vec(), 38500);
        assert_eq!(hash_40_post, yona_hash(&dummy_data_40));
        assert_eq!(hash_112_post, yona_hash_112(&dummy_data_112));
        
        println!("Pre-fork hash (40B): {}", hex::encode(hash_40_pre));
        println!("Post-fork hash (40B): {}", hex::encode(hash_40_post));
        println!("Pre-fork hash (112B): {}", hex::encode(hash_112_pre));
        println!("Post-fork hash (112B): {}", hex::encode(hash_112_post));
    }

    #[test]
    fn test_mining_and_verification_fork() {
        use primitive_types::U256;
        let parent_hash = [0u8; 32];
        let merkle_root = [0u8; 32];
        let difficulty = U256::from(200);
        let mut diff_bytes = [0u8; 32];
        difficulty.to_little_endian(&mut diff_bytes);

        // 1. Pack header at height = 38500
        let packed_header = crate::genz_pow::pack_header_v112(
            38500,
            &parent_hash,
            1600000000,
            &merkle_root,
            &diff_bytes
        );

        // 2. Hash the header using the new Yona Hash logic at height = 38500
        let header_hash = calculate_blake3_hash(packed_header.to_vec(), 38500);

        // 3. Use find_nonce to mine a block at height = 38500
        let mined_nonce = crate::genz_pow::find_nonce(
            header_hash.to_vec(),
            10000,
            diff_bytes.to_vec(),
            100000,
            4,
            38500
        );

        assert!(mined_nonce.is_some(), "Mining should easily find a nonce at diff 200");
        let nonce = mined_nonce.unwrap();
        println!("Mined Nonce: {}", nonce);

        // 4. Verify PoW using the same logic as verify_pow
        let mut material = [0u8; 40];
        material[..32].copy_from_slice(&header_hash);
        material[32..].copy_from_slice(&nonce.to_le_bytes());

        let hash_result = calculate_blake3_hash(material.to_vec(), 38500);
        let hash_u256 = U256::from_little_endian(&hash_result);
        
        let target = crate::genz_pow::difficulty_to_target(difficulty);
        assert!(hash_u256 < target, "Mined block must satisfy PoW target criteria");
        println!("Successfully validated mined Yona block!");
    }
}


