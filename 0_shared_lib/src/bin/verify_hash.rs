use blake3;

fn main() {
    let height = 0u64;
    let parent_hash = vec![
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
    ];
    let timestamp = 1782345726u64;
    let tx_root = vec![
        70, 173, 86, 155, 103, 105, 214, 216, 118, 172, 157, 121, 196, 215, 9, 209,
        26, 223, 175, 21, 223, 77, 146, 218, 218, 215, 76, 116, 164, 61, 192, 220
    ];
    let difficulty = vec![
        0, 228, 11, 84, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
    ];

    let mut buf = [0u8; 112];
    buf[0..8].copy_from_slice(&height.to_le_bytes());
    buf[8..40].copy_from_slice(&parent_hash);
    buf[40..48].copy_from_slice(&timestamp.to_le_bytes());
    buf[48..80].copy_from_slice(&tx_root);
    buf[80..112].copy_from_slice(&difficulty);

    // Băm với context `"BTC GenZ Toi Gian PoW v1.0"`
    let hash_vanguard = blake3::derive_key("BTC GenZ Toi Gian PoW v1.0", &buf);
    println!("Packed 112-byte Hash với Vanguard Context : {}", hex::encode(hash_vanguard));

    // Băm với context cũ chăng? "btc-genz-v112"
    let hash_old_ctx = blake3::derive_key("btc-genz-v112", &buf);
    println!("Packed 112-byte Hash với btc-genz-v112   : {}", hex::encode(hash_old_ctx));
    
    // Băm không context (standard Blake3)
    let hash_standard = blake3::hash(&buf);
    println!("Packed 112-byte Hash Standard Blake3     : {}", hex::encode(hash_standard.as_bytes()));
}
