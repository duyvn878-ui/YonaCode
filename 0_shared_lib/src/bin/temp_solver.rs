use hex;
use primitive_types::U256;

fn main() {
    let height: u64 = 38500;
    let timestamp: u64 = 1785429376;
    let nonce: u64 = 67336993565826;
    
    let difficulty_hex = "963451d55c030000000000000000000000000000000000000000000000000000";
    let parent_hash_hex = "8026ef8e9944147921cf638ef28361c9a829eeed937bcc166300502e5605abd9";
    let tx_root_hex = "c239e98fb3c9a4f32bf21b079c677dd431f28dabfdb8b4bded30ad70ce4864c4";
    let explorer_block_hash_hex = "973c5c98db769db14ca6ae418f66063d6f491b093cc63ba7f30716fe40c75622";

    let difficulty = hex::decode(difficulty_hex).unwrap();
    let parent_hash = hex::decode(parent_hash_hex).unwrap();
    let tx_root = hex::decode(tx_root_hex).unwrap();

    // 1. Pack header v112
    let packed = btc_genz_scl::genz_pow::pack_header_v112(
        height,
        &parent_hash,
        timestamp,
        &tx_root,
        &difficulty
    );

    // 2. Compute Blake3 Vanguard hash
    let blake3_hash = btc_genz_scl::crypto_primitives::calculate_blake3_hash_vanguard(&packed);
    
    // 3. Compute Yona Hash (since height is 38500, calculate_blake3_hash will use Yona Hash)
    let yona_header_hash = btc_genz_scl::crypto_primitives::calculate_blake3_hash(packed.to_vec(), height);

    println!("=== Block Header Hash Analysis at Height 38500 ===");
    println!("Explorer reported block hash : {}", explorer_block_hash_hex);
    println!("Blake3 Vanguard header hash   : {}", hex::encode(blake3_hash));
    println!("Yona Hash header hash         : {}", hex::encode(yona_header_hash));
    
    if hex::encode(blake3_hash) == explorer_block_hash_hex {
        println!("✅ VERDICT: Explorer block hash matches Blake3 Vanguard header hash!");
    } else {
        println!("❌ Explorer block hash does NOT match Blake3 Vanguard!");
    }

    if hex::encode(yona_header_hash) == explorer_block_hash_hex {
        println!("✅ VERDICT: Explorer block hash matches Yona Hash header hash!");
    } else {
        println!("❌ Explorer block hash does NOT match Yona Hash!");
    }

    // 4. Now let's calculate PoW verification for BOTH
    // material = header_hash (32 bytes) + nonce (8 bytes)
    // target = difficulty_to_target(difficulty)
    let mut diff_padded = [0u8; 32];
    diff_padded.copy_from_slice(&difficulty);
    let diff_u256 = U256::from_little_endian(&diff_padded);
    let target = btc_genz_scl::genz_pow::difficulty_to_target(diff_u256);
    
    println!("\n=== PoW Verification ===");
    println!("Target: {:064x}", target);

    // Case A: Mined on old VPS (using Blake3 Vanguard header hash + Blake3 Vanguard PoW hash)
    let mut mat_old = [0u8; 40];
    mat_old[..32].copy_from_slice(&blake3_hash);
    mat_old[32..].copy_from_slice(&nonce.to_le_bytes());
    let pow_hash_old = btc_genz_scl::crypto_primitives::calculate_blake3_hash_vanguard(&mat_old);
    let pow_u256_old = U256::from_little_endian(&pow_hash_old);
    println!("Blake3 Vanguard PoW Hash: {}", hex::encode(pow_hash_old));
    println!("Blake3 Vanguard PoW < Target: {}", pow_u256_old < target);

    // Case B: Evaluated under Yona Hash (using Yona header hash + Yona PoW hash)
    let mut mat_yona = [0u8; 40];
    mat_yona[..32].copy_from_slice(&yona_header_hash);
    mat_yona[32..].copy_from_slice(&nonce.to_le_bytes());
    let pow_hash_yona = btc_genz_scl::crypto_primitives::yona_hash(&mat_yona);
    let pow_u256_yona = U256::from_little_endian(&pow_hash_yona);
    println!("Yona Hash PoW Hash: {}", hex::encode(pow_hash_yona));
    println!("Yona Hash PoW < Target: {}", pow_u256_yona < target);
}
