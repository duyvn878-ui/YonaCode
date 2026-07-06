/**
 * @file inspect_db.rs
 * @brief Công cụ chẩn đoán: So sánh hash khối Genesis trong DB vs tính lại bằng code hiện tại.
 * @date 2026-06-25
 * 
 * Mục đích: Xác định chính xác tại sao hash trong DB (685c...) khác với hash mà UI tính ra (706e...).
 * Bằng cách đọc header Genesis từ DB, tính lại hash theo cùng logic mà `calculate_block_header_hash` sử dụng,
 * và in ra toàn bộ dữ liệu trung gian để tìm điểm sai lệch.
 */
use btc_genz_scl::state_manager;
use btc_genz_scl::proto::block::BlockHeader;
use btc_genz_scl::crypto_primitives;
use btc_genz_scl::genz_pow;
use prost::Message;
use primitive_types::U256;


fn main() {
    let args: Vec<String> = std::env::args().collect();
    let db_path = if args.len() > 1 { &args[1] } else { "./temp_db" };

    println!("🔍 Mở DB: {}", db_path);
    if let Err(e) = state_manager::init_global_state(db_path) {
        println!("❌ Lỗi: {:?}", e);
        return;
    }

    let mgr = state_manager::get_state_manager().expect("StateManager not init");

    println!("\n=== [1/4] HASH LƯU TRONG DB (get_block_hash(0)) ===");
    let db_hash = match mgr.get_block_hash(0) {
        Some(h) => {
            println!("  DB Hash: {}", hex::encode(&h));
            h
        }
        None => {
            println!("  ❌ Block #0 hash KHÔNG TỒN TẠI trong DB!");
            return;
        }
    };

    println!("\n=== [2/4] HEADER PROTOBUF TRONG DB ===");
    let header_raw = match mgr.get_header_raw(&db_hash) {
        Some(raw) => {
            println!("  Header raw size: {} bytes", raw.len());
            raw
        }
        None => {
            println!("  ❌ Header raw cho hash {} KHÔNG TỒN TẠI!", hex::encode(&db_hash));
            // Thử tìm bằng GENESIS_HASH constant
            println!("  → Thử tìm bằng GENESIS_HASH constant (706e...)...");
            let alt_hash = crypto_primitives::GENESIS_HASH;
            match mgr.get_header_raw(&alt_hash) {
                Some(raw) => {
                    println!("  ✅ Tìm thấy header bằng GENESIS_HASH constant! Size: {} bytes", raw.len());
                    raw
                }
                None => {
                    println!("  ❌ Cũng không tìm thấy bằng GENESIS_HASH constant!");
                    return;
                }
            }
        }
    };

    println!("\n=== [3/4] GIẢI MÃ VÀ PHÂN TÍCH HEADER ===");
    let header = match BlockHeader::decode(&header_raw[..]) {
        Ok(h) => {
            println!("  Height     : {}", h.height);
            println!("  Timestamp  : {}", h.timestamp);
            println!("  Nonce      : {}", h.nonce);
            println!("  Version    : {}", h.version);
            println!("  Manifesto  : {}", h.manifesto);
            
            let parent = h.parent_hash.as_ref().map(|p| hex::encode(&p.value)).unwrap_or("NONE".into());
            let tx_root = h.tx_root.as_ref().map(|r| hex::encode(&r.value)).unwrap_or("NONE".into());
            let state_root = h.state_root.as_ref().map(|s| hex::encode(&s.value)).unwrap_or("NONE".into());
            let miner = h.miner_address.as_ref().map(|a| hex::encode(&a.value)).unwrap_or("NONE".into());
            let diff = hex::encode(&h.difficulty);
            
            println!("  ParentHash : {}", parent);
            println!("  TxRoot     : {}", tx_root);
            println!("  StateRoot  : {}", state_root);
            println!("  MinerAddr  : {}", miner);
            println!("  Difficulty : {}", diff);
            println!("  AbsWeight  : {}", hex::encode(&h.absolute_weight));
            
            h
        }
        Err(e) => {
            println!("  ❌ Lỗi decode protobuf: {:?}", e);
            return;
        }
    };

    println!("\n=== [4/4] TÍNH LẠI HASH VÀ SO SÁNH ===");
    
    // Cách 1: Dùng pack_header_v112 rồi blake3 (giống calculate_block_header_hash cho protobuf input)
    let parent_h = header.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
    let tx_root = header.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
    let packed = genz_pow::pack_header_v112(
        header.height,
        &parent_h,
        header.timestamp,
        &tx_root,
        &header.difficulty,
    );
    println!("  Packed V112 (112 bytes): {}", hex::encode(&packed));
    
    let recomputed_hash = crypto_primitives::calculate_blake3_hash(packed.to_vec(), header.height);
    println!("  → Hash (pack_v112 → blake3)   : {}", hex::encode(&recomputed_hash));
    
    // Cách 2: Gọi trực tiếp calculate_block_header_hash với protobuf raw (giống UI path)
    let hash_from_protobuf = btc_genz_scl::calculate_block_header_hash(header_raw.clone());
    println!("  → Hash (protobuf → lib func)  : {}", hex::encode(&hash_from_protobuf));

    // Cách 3: Gọi với packed 112 bytes (giống miner path khi len == 112)
    let hash_from_packed = btc_genz_scl::calculate_block_header_hash(packed.to_vec());
    println!("  → Hash (packed 112 trực tiếp) : {}", hex::encode(&hash_from_packed));
    
    println!("\n=== KẾT LUẬN ===");
    println!("  GENESIS_HASH constant         : {}", hex::encode(crypto_primitives::GENESIS_HASH));
    println!("  DB Hash thực tế               : {}", hex::encode(&db_hash));
    println!("  Hash tính lại (pack→blake3)   : {}", hex::encode(&recomputed_hash));
    println!("  Hash từ protobuf raw          : {}", hex::encode(&hash_from_protobuf));
    
    if recomputed_hash == db_hash {
        println!("  ✅ Hash tính lại KHỚP với DB hash. Không có sai lệch.");
    } else {
        println!("  ❌ KHÔNG KHỚP! DB lưu hash khác với code hiện tại tính ra.");
    }
    
    if recomputed_hash == crypto_primitives::GENESIS_HASH {
        println!("  ✅ Hash tính lại KHỚP với GENESIS_HASH constant.");
    } else {
        println!("  ❌ GENESIS_HASH constant KHÔNG KHỚP với hash tính lại!");
    }

    println!("\n=== [5/5] DANH SÁCH KHỐI VÀ ĐỘ KHÓ TRONG DB ===");
    let mut h = 0;
    let mut prev_ts = None;
    loop {
        match mgr.get_block_hash(h) {
            Some(hash) => {
                match mgr.get_header_raw(&hash) {
                    Some(raw) => {
                        if let Ok(header) = BlockHeader::decode(&raw[..]) {
                            let diff_padded = {
                                let mut padded = [0u8; 32];
                                let d_len = header.difficulty.len().min(32);
                                if d_len > 0 {
                                    padded[..d_len].copy_from_slice(&header.difficulty[..d_len]);
                                }
                                U256::from_little_endian(&padded)
                            };
                            
                            let solve_time_str = match prev_ts {
                                Some(pts) => {
                                    let st = header.timestamp as i64 - pts as i64;
                                    format!("{}s", st)
                                }
                                None => "Genesis".to_string(),
                            };
                            
                            println!(
                                "  H#{} | Hash: {}... | Time: {} | SolveTime: {:>7} | Diff: {}",
                                h,
                                &hex::encode(&hash)[0..12],
                                header.timestamp,
                                solve_time_str,
                                diff_padded
                            );
                            prev_ts = Some(header.timestamp);
                        }
                    }
                    None => {
                        println!("  ❌ Block #{} có hash nhưng không tìm thấy header!", h);
                    }
                }
            }
            None => {
                println!("  🏁 Đỉnh chuỗi đạt được tại chiều cao: {}", h);
                break;
            }
        }
        h += 1;
    }
}

