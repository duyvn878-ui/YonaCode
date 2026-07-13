use crate::proto::block::{Block, BlockHeader};
use crate::proto::consensus::{SyncChainRequest, SyncChainResponse, SyncInstruction, sync_instruction};
use prost::Message;
use chrono;
use rayon::prelude::*;
use primitive_types::U256;

/**
 * @file reorg.rs
 * @brief Xử lý tái cấu trúc chuỗi (Reorganization) và đồng bộ hóa tập trung.
 * @details Thay thế logic findForkPoint và RollbackState từ phía Go.
 */

fn get_mtp_for_reorg(
    mgr: &crate::state_manager::StateManager,
    height: u64,
    block_timestamps: &std::collections::HashMap<u64, u64>,
) -> u64 {
    if height == 0 { return 0; }
    
    let mut timestamps = Vec::new();
    let start = height.saturating_sub(11);
    
    for h in start..height {
        if let Some(&ts) = block_timestamps.get(&h) {
            timestamps.push(ts);
        } else {
            if let Some(hash) = mgr.get_block_hash(h) {
                if let Some(header_raw) = mgr.get_header_raw(&hash) {
                    if let Ok(header) = BlockHeader::decode(&header_raw[..]) {
                        timestamps.push(header.timestamp);
                    }
                }
            }
        }
    }
    
    if timestamps.is_empty() { return 0; }
    timestamps.sort();
    timestamps[timestamps.len() / 2]
}

fn is_db_or_internal_error(msg: &str) -> bool {
    let msg_lower = msg.to_lowercase();
    msg_lower.contains("rocksdb") 
        || msg_lower.contains("database") 
        || msg_lower.contains("jmt") 
        || msg_lower.contains("i/o") 
        || msg_lower.contains("io error") 
        || msg_lower.contains("corrupted")
        || msg_lower.contains("not initialized")
        || msg_lower.contains("not found in db")
}

pub fn process_chain(req: SyncChainRequest, is_syncing: bool, deadline: u64) -> SyncChainResponse {
    let mgr = match crate::state_manager::get_state_manager() {
        Some(m) => m,
        None => return SyncChainResponse { 
            status: 4, // INTERNAL_ERROR
            error_msg: "StateManager not initialized".into(),
            ..Default::default()
        }
    };

    // [VANGUARD-COMMAND-HELPER] Giúp Rust Core ra lệnh cho Go
    let create_cmd = |strategy: sync_instruction::Strategy, start: u64, end: u64| {
        Some(SyncInstruction {
            strategy: strategy as i32,
            start_height: start,
            end_height: end,
        })
    };

    if req.blocks_raw.is_empty() {
        return SyncChainResponse { 
            status: 4, // INTERNAL_ERROR (Không phải lỗi gian lận của Peer)
            error_msg: "Empty block list".into(),
            ..Default::default()
        };
    }

    // 1. Giải mã danh sách khối
    let mut blocks = Vec::new();
    for raw in req.blocks_raw {
        match Block::decode(&raw[..]) {
            Ok(b) => blocks.push((b, raw)),
            Err(e) => {
                let error_detail = format!("Failed to decode block: {}", e);
                return SyncChainResponse { 
                    status: 2, 
                    error_msg: error_detail.into(),
                    ..Default::default()
                }
            }
        }
    }

    // Sắp xếp theo chiều cao để đảm bảo thứ tự
    blocks.sort_by_key(|(b, _)| b.header.as_ref().map(|h| h.height).unwrap_or(0));

    // Tạo bản đồ tra cứu nhanh timestamp theo chiều cao của khối trong batch mới
    let mut block_timestamps = std::collections::HashMap::new();
    for (b, _) in &blocks {
        if let Some(hdr) = b.header.as_ref() {
            block_timestamps.insert(hdr.height, hdr.timestamp);
        }
    }

    // [VANGUARD-PRE-VALIDATION] PHA 0: XÁC THỰC CHỮ KÝ GIAO DỊCH SONG SONG CHO TOÀN BỘ CHÙM KHỐI MỚI
    // Chống "Bom Chữ Ký" (Signature Bomb DoS) từ peer độc hại trước khi tiến hành sửa đổi DB.
    for (b, _) in &blocks {
        if let Some(body) = &b.body {
            let pre_validated: Vec<Result<(), String>> = body.transactions.par_iter().enumerate().map(|(idx, tx)| {
                if tx.amount == 0 { return Ok(()); } // Bỏ qua coinbase
                let sender_bytes = tx.sender.as_ref().map(|s| s.value.clone()).unwrap_or_default();
                if sender_bytes.len() != 32 { return Err("Invalid sender address length".to_string()); }
                let mut s_addr = [0u8; 32]; s_addr.copy_from_slice(&sender_bytes);
                
                let sig_bytes = if let Some(sig) = &tx.signature {
                    if sig.value.len() != 64 { return Err("Invalid signature length".to_string()); }
                    let mut b = [0u8; 64]; b.copy_from_slice(&sig.value);
                    b
                } else {
                    return Err("Missing signature".to_string());
                };

                let tx_signing_hash = crate::crypto_primitives::calculate_signing_hash(tx);

                if !crate::crypto_primitives::verify_ed25519_signature(&s_addr, &tx_signing_hash, &sig_bytes) {
                    return Err(format!("Invalid signature in transaction #{} at block height {}", idx, b.header.as_ref().unwrap().height));
                }
                Ok(())
            }).collect();

            for res in pre_validated {
                if let Err(e) = res {
                    log::error!("[REORG-SECURITY] 🚨 Phát hiện giao dịch giả mạo chữ ký! Hủy bỏ Reorg ngay lập tức: {}", e);
                    return SyncChainResponse {
                        status: 2, // FRAUD (Gian lận)
                        error_msg: format!("Invalid transaction signature in sync chain: {}", e),
                        ..Default::default()
                    };
                }
            }
        }
    }

    let (first_block, _) = &blocks[0];
    let first_header = match first_block.header.as_ref() {
        Some(h) => h,
        None => return SyncChainResponse { status: 2, error_msg: "Missing header in first block".into(), ..Default::default() }
    };

    // 2. Tìm điểm rẽ nhánh (Fork Point)
    let parent_hash = first_header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default();
    if parent_hash.len() != 32 {
        return SyncChainResponse { status: 2, error_msg: "Invalid parent hash length".into(), ..Default::default() };
    }
    let mut parent_hash_arr = [0u8; 32];
    parent_hash_arr.copy_from_slice(&parent_hash);

    let mut fork_point_height = 0;
    let mut fork_point_weight = U256::from(0);

    if first_header.height == 0 {
        // [VANGUARD-BOOTSTRAP] Khối Genesis không có cha. Chấp nhận nếu DB đang trống hoặc Reorg khối 0.
        if mgr.get_block_hash(0).is_some() && mgr.get_current_version() > 0 {
             return SyncChainResponse { status: 2, error_msg: "Genesis already exists and not at tip".into(), ..Default::default() };
        }
        fork_point_height = 0;
        fork_point_weight = U256::from(0);
        // Lưu ý: parent_hash_arr lúc này là zeros, phù hợp với quy ước Genesis.
    } else {
        let stored_parent_header_raw = mgr.get_header_raw(&parent_hash_arr);
        if stored_parent_header_raw.is_none() {
            // [VANGUARD-DIAGNOSTIC] Phân tích tại sao thiếu cha
            let expected_parent_h = first_header.height.saturating_sub(1);
            let db_hash_at_parent = mgr.get_block_hash(expected_parent_h);
            
            log::error!("[SYNC-DIAGNOSTIC] 🔍 Khối #{} báo thiếu cha: {}",
                first_header.height, hex::encode(parent_hash_arr));
            
            if let Some(actual_hash) = db_hash_at_parent {
                log::error!("[SYNC-DIAGNOSTIC] ⚠️ DB hiện lưu #{} với hash: {}", expected_parent_h, hex::encode(actual_hash));
            }

            return SyncChainResponse { 
                status: 3, // ORPHAN
                error_msg: format!("Parent hash {} not found.", hex::encode(parent_hash_arr)),
                missing_parent_hash: hex::encode(parent_hash_arr),
                ..Default::default()
            };
        }

        let stored_raw = stored_parent_header_raw.unwrap();
        if stored_raw.is_empty() || stored_raw.iter().all(|&b| b == 0) {
            log::error!("[REORG-FATAL] ❌ Dữ liệu Header tại điểm rẽ nhánh rỗng hoặc toàn số 0!");
            return SyncChainResponse { status: 4, error_msg: "Local DB corrupted: Empty/Zero header in DB at fork point".into(), ..Default::default() };
        }

        let fork_point_header = match BlockHeader::decode(&stored_raw[..]) {
            Ok(h) => h,
            Err(e) => {
                log::error!("[REORG-FATAL] ❌ Không thể giải mã Header tại điểm rẽ nhánh {:x?}! Dữ liệu có thể bị nhiễm độc. Lỗi: {}", parent_hash_arr, e);
                return SyncChainResponse { 
                    status: 4, 
                    error_msg: format!("Local DB corrupted: Corrupted header at fork point: {}", e),
                    ..Default::default()
                };
            }
        };
        fork_point_height = fork_point_header.height;
        // [FIX-V1.21] Padding an toàn cho absolute_weight để tránh panic khi mảng < 32 bytes
        fork_point_weight = if fork_point_header.absolute_weight.len() == 32 {
            U256::from_little_endian(&fork_point_header.absolute_weight)
        } else {
            let mut padded = [0u8; 32];
            let copy_len = fork_point_header.absolute_weight.len().min(32);
            if copy_len > 0 {
                padded[..copy_len].copy_from_slice(&fork_point_header.absolute_weight[..copy_len]);
            }
            U256::from_little_endian(&padded)
        };
    }

    // Kiểm tra Tường lửa Đá Tảng (Finality)
    let mut finalized_h = mgr.get_finalized_height();
    let is_deep_reorg = fork_point_height < finalized_h;
    if is_deep_reorg {
        log::warn!("[VNT-CONSENSUS] ⚠️ CẢNH BÁO: Phát hiện rẽ nhánh sâu tại #{} (Tường lửa: #{}). Chờ phán quyết từ Trọng tài Năng lượng...", fork_point_height, finalized_h);
    }

    // 3. Tính toán trọng số chuỗi mới & Xác thực Năng lượng (PoW)
    let mut new_chain_weight = fork_point_weight;
    let mut last_hash = parent_hash_arr; // Bắt đầu từ Fork Point (Zeros đối với Genesis)
    
    // Nạp lịch sử trước điểm rẽ nhánh để tính toán độ khó (LWMA DAA)
    let max_height = fork_point_height + blocks.len() as u64;
    let max_n = 120u64;
    let mut history_ts = Vec::new();
    let mut history_diffs = Vec::new();
    let start_h = (fork_point_height + 1).saturating_sub(max_n + 1);
    for h_idx in start_h..=fork_point_height {
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

    let first_n = 120u64;
    let expected_history_len = if first_header.height == 0 {
        0
    } else {
        (fork_point_height + 1).min(first_n + 1) as usize
    };
    if history_ts.len() < expected_history_len {
        log::error!("[REORG-FATAL] ❌ Không thể tải đủ lịch sử DAA từ DB! Yêu cầu: {}, Tìm thấy: {}", expected_history_len, history_ts.len());
        return SyncChainResponse {
            status: 4, // INTERNAL_ERROR
            error_msg: "Local DB busy or corrupted: Missing history blocks to calculate DAA/LWMA".into(),
            ..Default::default()
        };
    }


    for (b, raw) in &blocks {
        let h = match b.header.as_ref() {
            Some(hdr) => hdr,
            None => {
                return SyncChainResponse { 
                    status: 2, 
                    error_msg: format!("❌ DỮ LIỆU LỖI: Khối không có Header!"),
                    ..Default::default()
                };
            }
        };
        
        // [VANGUARD-CONSENSUS] Kiểm tra tính liên tục: Khối này có thực sự nối tiếp khối trước không?
        let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        if parent_h != last_hash {
             return SyncChainResponse { 
                status: 2, 
                error_msg: format!("❌ CHUỖI ĐỨT ĐOẠN: Khối #{} không trỏ về khối trước đó!", h.height),
                ..Default::default()
            };
        }

        // [VANGUARD-CONSENSUS] Node tự tính lại năng lượng: Không tin vào con số ghi trong header
        let tx_root = h.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
        let packed_header = crate::genz_pow::pack_header_v112(
            h.height,
            &parent_h,
            h.timestamp,
            &tx_root,
            &h.difficulty
        );
        let header_hash = crate::crypto_primitives::calculate_blake3_hash(packed_header.to_vec(), h.height);
        
        println!("[POW-DEBUG-REORG] Height: {}, Nonce: {}, Diff: {}", h.height, h.nonce, hex::encode(&h.difficulty));
        println!("[POW-DEBUG-REORG] PackedHeader: {}", hex::encode(&packed_header));
        println!("[POW-DEBUG-REORG] HeaderHash:   {}", hex::encode(&header_hash));
        
        // [VANGUARD-FIX] verify_pow_raw yêu cầu HEADER HASH (32 bytes).
        let is_valid_pow = crate::genz_pow::verify_pow_raw(
            header_hash.to_vec(),
            h.nonce,
            h.difficulty.clone(),
            h.height
        );

        if !is_valid_pow {
            return SyncChainResponse { 
                status: 2, 
                error_msg: format!("❌ VI PHẠM NĂNG LƯỢNG: Khối #{} có chữ ký PoW không hợp lệ!", h.height),
                ..Default::default()
            };
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
                    if offset < blocks.len() {
                        if let Some(ref block) = blocks[offset].0.header {
                            found_hdr = Some(block.clone());
                        }
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

        if h.height > 0 && !history_ts.is_empty() {
            let expected_diff = crate::difficulty_logic::calculate_next_difficulty(&history_ts, &history_diffs, h.timestamp, h.height);
            let mut diff_padded = [0u8; 32];
            let d_len = h.difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
            }
            let actual_diff = U256::from_little_endian(&diff_padded);
            
            if actual_diff != expected_diff {
                return SyncChainResponse {
                    status: 2, // FRAUD (Gian lận)
                    error_msg: format!("❌ VI PHẠM ĐỘ KHÓ DAA: Khối #{} có độ khó thực tế {} lệch với kỳ vọng {}!", h.height, actual_diff, expected_diff),
                    ..Default::default()
                };
            }
        }
        
        // Thêm khối hiện tại vào mảng lịch sử động để tính cho khối tiếp theo
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

        // [VANGUARD-CONSENSUS] MTP-11 Firewall Protection (Time-Warp Attack Shield)
        // Tại sao: Chống Hacker giả mạo Timestamp để thao túng độ khó (LWMA). 
        // Tường lửa này phải nằm ở Rust Core để đảm bảo an toàn tuyệt đối ngay cả khi đồng bộ chuỗi dài.
        let mtp = get_mtp_for_reorg(&mgr, h.height, &block_timestamps);
        let current_now = chrono::Utc::now().timestamp() as u64;

        if !crate::verify_timestamp_firewall(h.timestamp, mtp, current_now, is_syncing) {
            log::error!("[REORG-SECURITY] 🚨 PHÁT HIỆN TIME-WARP ATTACK! Khối #{} có Timestamp {} vi phạm MTP-11 (MTP: {})", h.height, h.timestamp, mtp);
            return SyncChainResponse { 
                status: 2, 
                error_msg: format!("❌ VI PHẠM TƯỜNG LỬA THỜI GIAN: Khối #{} (MTP-11 Violation)", h.height),
                ..Default::default()
            };
        }

        // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
        let mut diff_padded = [0u8; 32];
        let d_len = h.difficulty.len().min(32);
        if d_len > 0 {
            diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
        }
        let diff = U256::from_little_endian(&diff_padded);
        new_chain_weight = new_chain_weight + diff;
        
        // Cập nhật last_hash để kiểm tra khối tiếp theo
        last_hash.copy_from_slice(&header_hash);
    }

    // Lấy trọng số đỉnh hiện tại
    let current_tip_h = mgr.get_current_version();
    let current_tip_hash = mgr.get_block_hash(current_tip_h).unwrap_or([0u8; 32]);
    let current_tip_header_raw = mgr.get_header_raw(&current_tip_hash);
    
    let current_weight = if let Some(raw) = current_tip_header_raw {
        match BlockHeader::decode(&raw[..]) {
            Ok(h) => {
                // [FIX-V1.21] BẢO VỆ CHỐNG PANIC KHI ABSOLUTE_WEIGHT BỊ RỖNG HOẶC SAI LENGTH
                if h.absolute_weight.len() == 32 {
                    U256::from_little_endian(&h.absolute_weight)
                } else {
                    let mut padded = [0u8; 32];
                    let copy_len = h.absolute_weight.len().min(32);
                    if copy_len > 0 {
                        padded[..copy_len].copy_from_slice(&h.absolute_weight[..copy_len]);
                    }
                    U256::from_little_endian(&padded)
                }
            },
            Err(e) => {
                log::error!("[REORG-ERROR] ❌ Lỗi giải mã Header đỉnh hiện tại ({}): {}", hex::encode(current_tip_hash), e);
                U256::from(0)
            }
        }
    } else {
        U256::from(0)
    };

    // 4. Quyết định: Phân xử bằng Trọng tài Năng lượng (VNT Consensus)
    if is_deep_reorg {
        // BƯỚC 3: TÍNH TOÁN NĂNG LƯỢNG PHÂN ĐOẠN TRANH CHẤP
        let local_segment_weight = current_weight.saturating_sub(fork_point_weight);
        let new_segment_weight = new_chain_weight.saturating_sub(fork_point_weight);

        // BƯỚC 4: TRỌNG TÀI NĂNG LƯỢNG (Quy tắc x10 Override)
        let required_weight = local_segment_weight.saturating_mul(U256::from(10u64));

        log::info!("[VNT-CONSENSUS] ⚖️ Phân đoạn rẽ nhánh sâu: Cục bộ = {}, Mạng = {}, Yêu cầu x10 = {}", local_segment_weight, new_segment_weight, required_weight);

        if new_segment_weight >= required_weight {
            // KỊCH BẢN 4.1: Bàn tay vô hình kích hoạt (Chuỗi chính cứu hộ)
            log::warn!("[INVISIBLE-HAND] ✋ BÀN TAY VÔ HÌNH KÍCH HOẠT: Mở khóa tường lửa bất biến tại #{}! Năng lượng chuỗi mới áp đảo (>= 10x) chuỗi kẹt cục bộ.", finalized_h);
            mgr.force_set_finalized_height(fork_point_height);
            finalized_h = fork_point_height;
        } else if new_segment_weight >= local_segment_weight {
            // [TRƯỜNG HỢP 1] Nặng hơn cục bộ nhưng CHƯA ĐỦ x10 -> KHÔNG BAN, CHỈ BỎ QUA
            log::warn!("[VNT-CONSENSUS] ⚠️ Chuỗi rẽ nhánh sâu có năng lượng cao nhưng chưa đủ ngưỡng x10. Bỏ qua, không phạt Peer.");
            return SyncChainResponse { 
                status: 0, // Trả về 0 để Go Node hiểu là Side-chain hợp lệ nhưng bị bỏ qua, KHÔNG BAN
                error_msg: format!("Chain is heavier but < 10x. New: {} < Required: {}. Ignored.", new_segment_weight, required_weight),
                instruction: create_cmd(sync_instruction::Strategy::Continue, 0, 0),
                ..Default::default()
            };
        } else {
            // [TRƯỜNG HỢP 2] Nhẹ hơn cục bộ -> LÀ RÁC/DDOS -> BAN
            log::error!("[FIREWALL] 🧱 CHẶN ĐỨNG HACKER: Chuỗi rác rẽ nhánh sâu bị từ chối.");
            return SyncChainResponse {
                status: 2, // Trả về 2 để Go Node BAN Peer này
                error_msg: format!("ERR_IMMUTABLE_FIREWALL_VIOLATION: Chuỗi rác cố tình rẽ nhánh sâu"),
                instruction: create_cmd(sync_instruction::Strategy::DeepRecovery, 0, 0),
                ..Default::default()
            };
        }
    } else {
        // VÙNG LINH HOẠT (Flexible Zone): Luật PoW Nakamoto truyền thống
        if new_chain_weight <= current_weight {
            log::warn!("[VANGUARD-CONSENSUS] ⚠️ Chuỗi nhận được nhẹ hơn hoặc bằng chuỗi hiện tại. Bỏ qua Reorg.");
            return SyncChainResponse { 
                status: 0, 
                error_msg: format!("Chain is lighter or equal. New: {} <= Current: {}. Stored but not canonical.", new_chain_weight, current_weight),
                instruction: create_cmd(sync_instruction::Strategy::Continue, 0, 0),
                ..Default::default()
            };
        }
    }

    // --- BẮT ĐẦU QUY TRÌNH REORG AN TOÀN ---
    println!("[RUST-CONSENSUS] 🔄 Phát hiện chuỗi nặng hơn. Chuẩn bị Reorg từ #{}...", fork_point_height);

    // A. SAO LƯU THỰC TẠI CŨ (Backup) - Để phục hồi nếu nhánh mới có độc
    let mut old_blocks_raw = Vec::new();
    let mut old_tx_hashes = std::collections::HashSet::new();
    
    for h in (fork_point_height + 1)..=current_tip_h {
        if let Some(raw) = mgr.get_block_raw_by_height(h) {
            old_blocks_raw.push(raw.clone());
            // Thu thập TX hash cũ để tính Orphaned TXs sau này
            if let Ok(b) = Block::decode(&raw[..]) {
                if let Some(body) = b.body {
                    for tx in body.transactions {
                        if tx.sender.is_some() {
                             let tx_raw = tx.encode_to_vec();
                             // [V2.0 SEGWIT-TXID] Sử dụng SegWit TxID để đồng bộ hoá hoàn toàn chỉ mục
                             let tx_h = crate::calculate_tx_hash(tx_raw, 0);
                             old_tx_hashes.insert(tx_h.to_vec());
                        }
                    }
                }
            }
        }
    }

    // B. ROLLBACK VỀ ĐIỂM CHUNG
    if current_tip_h > fork_point_height {
        if let Err(e) = mgr.rollback_state(current_tip_h, fork_point_height) {
            return SyncChainResponse { 
                status: 4, // INTERNAL_ERROR (Lỗi DB nội bộ)
                error_msg: format!("CRITICAL: Rollback failed: {}", e),
                ..Default::default()
            };
        }
    }

    // C. THỰC THI CHUỖI MỚI (Vùng nguy hiểm)
	let mut success_all = true;
	let mut fail_msg = String::new();
	let mut applied_heights = Vec::new();
	let mut new_chain_tx_hashes = std::collections::HashSet::new();

	// [BẢN VÁ CONSENSUS]: Khôi phục lại trọng số mỏ neo trước khi thực thi mẻ khối (Batch)
	// Tại sao: Tránh lỗi "Double-Counting" khi biến new_chain_weight đã chứa tổng trọng số của cả mẻ từ vòng lặp xác thực trước đó.
	let mut rolling_weight = fork_point_weight; 

	for (b, _raw_full) in &blocks {
        // --- KIỂM TRA TIMEOUT (gRPC Deadline Protection) ---
        if deadline > 0 && chrono::Utc::now().timestamp() as u64 >= deadline {
            success_all = false;
            fail_msg = "ProcessChain timeout reached (gRPC Deadline)".to_string();
            break;
        }
        let h = b.header.as_ref().unwrap();
        let body_raw = b.body.as_ref().map(|body| body.encode_to_vec()).unwrap_or_default();
        let miner_addr = h.miner_address.as_ref().map(|a| a.value.clone()).unwrap_or_default();
        let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
        
        // --- THỰC THI VÀ CHỐT SỔ NGUYÊN TỬ (NUCLEAR SHIELD) ---
        // [ULTRA-LIGHT-SYNC] Kiểm tra xem có cần thực thi đầy đủ (Heavy) hay chỉ thẩm định PoW (Light)
        let has_body = b.body.is_some() && !b.body.as_ref().unwrap().transactions.is_empty();
        
        // [VANGUARD-FIX] Chấp nhận Header-Only dưới mốc Finality hoặc mốc Purge (oldest_height) của mạng
        let oldest_h = mgr.get_oldest_height();
        if (h.height <= finalized_h || h.height < oldest_h) && !has_body {
            log::info!("[SYNC-LIGHT] 🕊️ Khối lịch sử #{} (Header-Only). Bỏ qua thực thi giao dịch.", h.height);
            
            // Tính toán mã băm và trọng số để cập nhật chỉ mục
            let header_raw = crate::genz_pow::pack_header_v112(
                h.height, &h.parent_hash.as_ref().unwrap().value, h.timestamp, 
                &h.tx_root.as_ref().unwrap().value, &h.difficulty
            ).to_vec();
            let header_hash = crate::crypto_primitives::calculate_blake3_hash(header_raw.clone(), h.height);
            let mut h_arr = [0u8; 32];
            h_arr.copy_from_slice(&header_hash);

            let mut diff_padded = [0u8; 32];
            let d_len = h.difficulty.len().min(32);
            if d_len > 0 { diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]); }
            let diff_u256 = U256::from_little_endian(&diff_padded);
            rolling_weight = rolling_weight + diff_u256;
            let mut weight_bytes = [0u8; 32];
            rolling_weight.to_little_endian(&mut weight_bytes);

            let mut h_mut = h.clone();
            h_mut.absolute_weight = weight_bytes.to_vec();
            let header_proto_raw = h_mut.encode_to_vec();

            // Cập nhật chỉ mục Hash và Header, nhưng KHÔNG gọi commit_block_atomic (vì không có state change)
            mgr.put_block_hash(h.height, &h_arr);
            mgr.put_header(&h_arr, header_proto_raw);
            
            continue;
        }

        match crate::execute_block_internal(
            body_raw.clone(),
            miner_addr,
            parent_h,
            h.height,
            false
        ) {
            Ok((payload, _block_hash, _)) => {
                
                // ================= [VÁ LỖI BẢO MẬT] =================
                // 1. Kiểm toán State Root (Chống lệch sổ cái)
                let declared_state_root = h.state_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();

                if h.height > 0 && declared_state_root != payload.final_root {
                    success_all = false;
                    fail_msg = format!("LỆCH STATE ROOT! Khai báo từ Miner: {}, Thực tế tại Node: {}", hex::encode(&declared_state_root), hex::encode(&payload.final_root));
                    break;
                }

                // 2. Kiểm toán Tx Root (Chống ráp thiếu giao dịch)
                let calculated_tx_root = crate::merkle::calculate_merkle_root(payload.tx_hashes.clone());
                let declared_tx_root = h.tx_root.as_ref().map(|r| r.value.clone()).unwrap_or_default();
                if declared_tx_root != calculated_tx_root.to_vec() {
                    success_all = false;
                    fail_msg = format!("LỆCH TX ROOT! Khai báo: {}, Thực tế: {}", hex::encode(&declared_tx_root), hex::encode(&calculated_tx_root));
                    break;
                }
                // ====================================================

                // A. Đóng gói Header
                let header_raw = crate::genz_pow::pack_header_v112(
                    h.height, &h.parent_hash.as_ref().unwrap().value, h.timestamp, 
                    &h.tx_root.as_ref().unwrap().value, &h.difficulty
                ).to_vec();

                let header_hash = crate::crypto_primitives::calculate_blake3_hash(header_raw.clone(), h.height);
                let mut h_arr = [0u8; 32];
                h_arr.copy_from_slice(&header_hash);

                // [FIX-VANGUARD] Padding an toàn chống Protobuf truncation
                let mut diff_padded = [0u8; 32];
                let d_len = h.difficulty.len().min(32);
                if d_len > 0 {
                    diff_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
                }
                // [VÁ LỖI TRỌNG SỐ]: Sử dụng rolling_weight được reset về điểm rẽ nhánh
                let diff_u256 = U256::from_little_endian(&diff_padded);
                rolling_weight = rolling_weight + diff_u256;
                let mut weight_bytes = [0u8; 32];
                rolling_weight.to_little_endian(&mut weight_bytes);

                // [DETERMINISM-FIX] Tuyệt đối không re-encode Header.
                // Ta sẽ tách Header thô từ chính block_raw_full để đảm bảo mã Hash bất biến.
                let header_proto_raw = if _raw_full.len() > 0 {
                    // Trong Protobuf, Header là field số 1. Ta có thể lấy bytes thô nếu cần, 
                    // nhưng ở đây ta đã có header_raw (112-byte packed) dùng để băm.
                    // Để UI/RPC đọc được, ta vẫn cần Protobuf, nhưng ta phải nạp đúng Weight.
                    let mut h_mut = h.clone();
                    h_mut.absolute_weight = weight_bytes.to_vec();
                    h_mut.encode_to_vec()
                } else {
                    h.encode_to_vec()
                };

                // C. CHỐT SỔ NGUYÊN TỬ (Atomic Commit)
                mgr.commit_block_atomic(
                    h.height,
                    &h_arr,
                    header_proto_raw, 
                    body_raw,
                    payload.tx_hashes,
                    payload.touched_accs,
                    payload.state_batch,
                    payload.actual_total_supply,
                    payload.actual_total_supply,
                    weight_bytes.to_vec()
                );
            },
            Err(e) => {
                success_all = false;
                fail_msg = format!("Lỗi thực thi tại khối #{}: {}", h.height, e);
                break;
            }
        }

        // Lưu vết giao dịch mới để khử trùng lặp
        if let Some(body) = &b.body {
            for tx in &body.transactions {
                let tx_raw = tx.encode_to_vec();
                let tx_h = crate::crypto_primitives::calculate_blake3_hash(tx_raw, 0);
                new_chain_tx_hashes.insert(tx_h.to_vec());
            }
        }

        applied_heights.push(h.height);
    }


    // D. PHỤC HỒI NẾU THẤT BẠI (VANGUARD RECOVERY)
    if !success_all {
        println!("[RUST-CONSENSUS] 🚨 NHÁNH MỚI CÓ ĐỘC: {}. Đang khôi phục thực tại cũ...", fail_msg);
        
        // 1. Rollback phần chuỗi mới đã lỡ nạp
        let failed_tip = applied_heights.last().cloned().unwrap_or(fork_point_height);
        if failed_tip > fork_point_height {
            let _ = mgr.rollback_state(failed_tip, fork_point_height);
        }

        // 2. Nạp lại chuỗi cũ (Phải thành công vì nó đã từng ở đó)
        let mut old_rolling_weight = fork_point_weight;
        for raw in old_blocks_raw {
            if let Ok(b) = Block::decode(&raw[..]) {
                let h = b.header.as_ref().unwrap();
                let body_raw = b.body.as_ref().map(|body| body.encode_to_vec()).unwrap_or_default();
                let miner_addr = h.miner_address.as_ref().map(|a| a.value.clone()).unwrap_or_default();
                let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
                
                let exec_res = crate::execute_block_transactions(body_raw, miner_addr, parent_h, h.height, false);
                if !exec_res.success {
                    // Tại sao: Nếu khôi phục thực tại cũ bị lỗi (ví dụ: tràn RAM, I/O đĩa hoặc hỏng JMT),
                    // việc tiếp tục lưu khối vào DB mà không có state tương ứng sẽ gây lệch pha sổ cái vĩnh viễn.
                    // Do đó, chúng ta bắt buộc phải dừng khẩn cấp (panic) để bảo vệ tính toàn vẹn của cơ sở dữ liệu.
                    log::error!("CRITICAL FATAL: Không thể phục hồi thực tại cũ. Khối #{} bị hỏng state! Lỗi: {}", h.height, exec_res.error_msg);
                    panic!("RECOVERY FATAL ERROR: Database is corrupted. Please restart node.");
                }
                
                let diff_u256 = U256::from_little_endian(&h.difficulty);
                old_rolling_weight = old_rolling_weight + diff_u256;
                let mut weight_bytes = [0u8; 32];
                old_rolling_weight.to_little_endian(&mut weight_bytes);

                let header_hash = crate::crypto_primitives::calculate_blake3_hash(
                    crate::genz_pow::pack_header_v112(
                        h.height, &h.parent_hash.as_ref().unwrap().value, h.timestamp, 
                        &h.tx_root.as_ref().unwrap().value, &h.difficulty
                    ).to_vec(), 
                    h.height
                );
                crate::save_block_raw(h.height, header_hash.to_vec(), raw, true, weight_bytes.to_vec());
                crate::commit_block_hash(h.height, header_hash.to_vec());
            }
        }

        let is_internal = is_db_or_internal_error(&fail_msg);
        let resp_status = if is_internal { 4 } else { 2 };

        return SyncChainResponse { 
            status: resp_status, 
            error_msg: format!("Reorg failed, node restored to original tip. Error: {}", fail_msg),
            instruction: create_cmd(sync_instruction::Strategy::DeepRecovery, 0, 0),
            ..Default::default()
        };
    }

    // E. CHỐT SỔ (COMMIT) & KHỬ TRÙNG LẶP MEMPOOL
    for (b, _) in &blocks {
        let h = b.header.as_ref().unwrap();
        let header_hash = crate::crypto_primitives::calculate_blake3_hash(
            crate::genz_pow::pack_header_v112(
                h.height, &h.parent_hash.as_ref().unwrap().value, h.timestamp, 
                &h.tx_root.as_ref().unwrap().value, &h.difficulty
            ).to_vec(), 
            h.height
        );
        crate::commit_block_hash(h.height, header_hash.to_vec());
    }

    // Lọc Orphaned TXs: Chỉ những TX cũ KHÔNG có trong chuỗi mới
    let mut final_orphaned_hashes = Vec::new();
    let mut final_orphaned_raws = Vec::new();

    // Duyệt lại chuỗi cũ để lấy data thô (optimized)
    for raw in old_blocks_raw {
        if let Ok(b) = Block::decode(&raw[..]) {
            if let Some(body) = b.body {
                for tx in body.transactions {
                    if tx.sender.is_some() {
                        let tx_raw = tx.encode_to_vec();
                        // [V2.0 SEGWIT-TXID] Sử dụng SegWit TxID để đồng bộ hoá hoàn toàn chỉ mục
                        let tx_h = crate::calculate_tx_hash(tx_raw.clone(), 0);
                        if !new_chain_tx_hashes.contains(&tx_h.to_vec()) {
                            final_orphaned_hashes.push(tx_h.to_vec());
                            final_orphaned_raws.push(tx_raw);
                        }
                    }
                }
            }
        }
    }

    println!("[RUST-CONSENSUS] ✅ Reorg thành công lên cao độ #{}", applied_heights.last().unwrap_or(&fork_point_height));

    SyncChainResponse {
        status: 1, // REORG_SUCCESS
        new_height: mgr.get_current_version(),
        fork_point: fork_point_height,
        orphaned_tx_hashes: final_orphaned_hashes,
        orphaned_txs_raw: final_orphaned_raws,
        error_msg: "".into(),
        missing_parent_hash: "".into(),
        instruction: create_cmd(sync_instruction::Strategy::Continue, 0, 0),
    }
}
