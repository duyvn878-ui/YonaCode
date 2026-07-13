/**
 * @file consensus_tests.rs
 * @brief Kiểm thử bộ quy tắc đồng thuận (Consensus Rules & DAA) cho BTC GenZ.
 * @details Kiểm toán thuật toán điều chỉnh độ khó LWMA (Zawy Style), median time past (MTP-11),
 *          xác thực PoW raw và các phép toán an toàn tránh tràn số.
 */

use btc_genz_scl::difficulty_logic::{calculate_next_difficulty, LWMA_WINDOW, MIN_DIFFICULTY};
use btc_genz_scl::genz_pow::{difficulty_to_target, verify_pow_raw};
use btc_genz_scl::state_manager::{StateManager, GLOBAL_STATE_MANAGER};
use btc_genz_scl::proto::block::{BlockHeader, Block, BlockBody};
use btc_genz_scl::proto::consensus::SyncChainRequest;
use primitive_types::U256;
use tempfile::tempdir;
use prost::Message;
use std::sync::Arc;

/// Hàm tiện ích khởi tạo StateManager tạm thời trong sandbox DB
fn create_test_state_manager() -> (Arc<StateManager>, tempfile::TempDir) {
    let tmp_dir = tempdir().unwrap();
    let db_path = tmp_dir.path().to_str().unwrap().to_string();
    let mgr = StateManager::try_new(db_path).expect("Khởi tạo StateManager tạm thời thất bại");
    (mgr, tmp_dir)
}

/// 1. Kiểm thử thuật toán DAA LWMA (Linear Weighted Moving Average)
/// Đảm bảo độ khó phản ứng chuẩn xác với thời gian đào thực tế và nằm trong clamping giới hạn.
#[test]
fn test_lwma_difficulty() {
    let min_difficulty = *MIN_DIFFICULTY;
    let last_diff = U256::from(15_000_000_000u64); // Độ khó trước đó

    // Dựng mảng timestamps và difficulties với kích thước cửa sổ LWMA_WINDOW (17)
    // Cần 18 timestamps và 17 difficulties.
    let mut difficulties = vec![last_diff; LWMA_WINDOW];
    let mut timestamps = Vec::new();

    // Trường hợp 1: Miner đào QUÁ NHANH (solve time trung bình 10s < Target 75s)
    let mut current_ts = 10000;
    timestamps.push(current_ts);
    for _ in 0..LWMA_WINDOW {
        current_ts += 10; // 10 giây mỗi khối
        timestamps.push(current_ts);
    }
    
    // LWMA chỉ điều chỉnh khi (height + 1) % 5 == 0. Ta mock height = 4 (height + 1 = 5)
    let next_diff_fast = calculate_next_difficulty(&timestamps, &difficulties, current_ts + 10, 4);

    // Xác nhận độ khó được kéo tăng lên do miner đào quá nhanh
    assert!(
        next_diff_fast > last_diff,
        "Độ khó mới ({}) phải lớn hơn độ khó cũ ({}) khi miner đào nhanh",
        next_diff_fast, last_diff
    );

    // Kiểm tra Clamping: độ khó không được thay đổi quá 50% mỗi khối
    let max_delta = last_diff / 2; // 50% delta
    let max_allowed = last_diff + max_delta;
    assert!(
        next_diff_fast <= max_allowed,
        "Độ khó mới ({}) vượt quá giới hạn clamping tối đa cho phép ({})",
        next_diff_fast, max_allowed
    );

    // Trường hợp 2: Miner đào QUÁ CHẬM (solve time trung bình 200s > Target 75s)
    timestamps.clear();
    current_ts = 10000;
    timestamps.push(current_ts);
    for _ in 0..LWMA_WINDOW {
        current_ts += 200; // 200 giây mỗi khối
        timestamps.push(current_ts);
    }

    let next_diff_slow = calculate_next_difficulty(&timestamps, &difficulties, current_ts + 200, 4);

    // Xác nhận độ khó bị giảm đi để kích thích miner
    assert!(
        next_diff_slow < last_diff,
        "Độ khó mới ({}) phải nhỏ hơn độ khó cũ ({}) khi miner đào chậm",
        next_diff_slow, last_diff
    );

    // Kiểm tra Clamping dưới: không được giảm quá 50%
    let min_allowed = last_diff - max_delta;
    assert!(
        next_diff_slow >= min_allowed,
        "Độ khó mới ({}) giảm sâu hơn giới hạn clamping tối thiểu cho phép ({})",
        next_diff_slow, min_allowed
    );

    // Trường hợp 3: Kiểm tra giới hạn tối thiểu của độ khó (MIN_DIFFICULTY)
    difficulties = vec![U256::from(1_000_000_000u64); LWMA_WINDOW]; // Mock độ khó cũ cực thấp dưới 10 tỷ
    let next_diff_min = calculate_next_difficulty(&timestamps, &difficulties, current_ts + 500, 4);
    assert!(
        next_diff_min >= min_difficulty,
        "Độ khó mới ({}) không được phép nhỏ hơn độ khó tối thiểu hệ thống quy định ({})",
        next_diff_min, min_difficulty
    );
}

/// 2. Kiểm thử median time past (MTP-11)
/// Kiểm toán tính trung vị của 11 khối gần nhất để phòng chống tấn công Time-Warp
#[test]
fn test_mtp11_firewall() {
    let (mgr, _tmp) = create_test_state_manager();

    // Đưa 11 khối vào cơ sở dữ liệu với timestamps xáo trộn nhưng tăng dần về logic
    // Timestamps mock: 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110 (Giá trị trung vị mong đợi là 60)
    let timestamps_mock = vec![10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110];

    for (h, &ts) in timestamps_mock.iter().enumerate() {
        let block_hash = [h as u8; 32];
        mgr.put_block_hash(h as u64, &block_hash);

        let header = BlockHeader {
            height: h as u64,
            timestamp: ts,
            ..Default::default()
        };
        let mut header_raw = Vec::new();
        header.encode(&mut header_raw).unwrap();
        mgr.put_header(&block_hash, header_raw);
    }

    // Tính toán MTP tại đỉnh cao độ 11
    let mtp = mgr.get_median_time_past(11);
    assert_eq!(
        mtp, 60,
        "Giá trị Median Time Past của 11 khối (10..110) phải là 60 (khối thứ 6 sau khi sắp xếp)"
    );
}

/// 3. Kiểm thử logic xác thực PoW raw (Verify Proof-of-Work)
#[test]
fn test_pow_boundary() {
    // Độ khó U256
    let difficulty = U256::from(1000u64);
    
    // Target = U256::MAX / difficulty
    let target = difficulty_to_target(difficulty);
    assert_eq!(target, U256::MAX / difficulty);

    // Mock header hash và target dưới dạng little endian bytes
    let header_hash = vec![1u8; 32];
    let mut diff_bytes = vec![0u8; 32];
    difficulty.to_little_endian(&mut diff_bytes);

    // Test verify_pow_raw với nonce không khớp (sinh hash lớn hơn target)
    let is_valid = verify_pow_raw(header_hash.clone(), 999999999u64, diff_bytes.clone(), 1);
    assert!(!is_valid, "PoW phải thất bại đối với một nonce rác ngẫu nhiên");
}

/// 4. Kiểm thử các phép toán an toàn ngăn chặn tràn số dư tài chính
#[test]
fn test_math_overflow_prevention() {
    let val_max = u64::MAX;
    
    // Kiểm tra tràn trên (Overflow)
    let overflow_res = val_max.saturating_add(1000);
    assert_eq!(overflow_res, val_max, "Saturating add phải chặn tràn số và giữ ở mức cực đại");

    // Kiểm tra tràn dưới (Underflow)
    let underflow_res = 0u64.saturating_sub(1000);
    assert_eq!(underflow_res, 0, "Saturating sub phải chặn số âm và giữ ở mức 0");
}

/// 5. Kiểm thử DAA cửa sổ cố định 120 khối
#[test]
fn test_daa_constant_window() {
    let min_difficulty = U256::from(1_200_000_000u64);
    let last_diff = U256::from(2_000_000_000u64); // Lớn hơn MIN_DIFFICULTY

    // Thử với lịch sử thiếu (chỉ có 18 TS & 17 Diff) -> bootstrap guard bắt buộc kích hoạt
    let mut difficulties_short = vec![last_diff; 17];
    let mut timestamps_short = Vec::new();
    let mut current_ts = 10000;
    timestamps_short.push(current_ts);
    for _ in 0..17 {
        current_ts += 10;
        timestamps_short.push(current_ts);
    }
    let diff_insufficient = calculate_next_difficulty(&timestamps_short, &difficulties_short, current_ts + 10, 50);
    assert_eq!(diff_insufficient, last_diff);

    // Thử với lịch sử đủ 121 TS và 120 Diff -> tính toán LWMA bình thường
    // Miner đào QUÁ NHANH (10s mỗi khối) -> độ khó tăng lên tối đa (+25% clamped -> 2.5 tỷ)
    let mut difficulties_120 = vec![last_diff; 120];
    let mut timestamps_121 = Vec::new();
    let mut ts_121 = 10000;
    timestamps_121.push(ts_121);
    for _ in 0..120 {
        ts_121 += 10;
        timestamps_121.push(ts_121);
    }
    let diff_sufficient = calculate_next_difficulty(&timestamps_121, &difficulties_120, ts_121 + 10, 150);
    assert_eq!(diff_sufficient, U256::from(2_500_000_000u64));
}

/// Helper function to calculate header hash for test mock blocks
fn calculate_header_hash(h: &BlockHeader) -> Vec<u8> {
    let parent_h = h.parent_hash.as_ref().map(|ph| ph.value.clone()).unwrap_or_default();
    let tx_root = h.tx_root.as_ref().map(|tr| tr.value.clone()).unwrap_or_default();
    let packed = btc_genz_scl::genz_pow::pack_header_v112(
        h.height,
        &parent_h,
        h.timestamp,
        &tx_root,
        &h.difficulty
    );
    btc_genz_scl::crypto_primitives::calculate_blake3_hash(packed.to_vec(), h.height).to_vec()
}

/// Helper function to create mock block for consensus reorg tests
fn create_mock_block(height: u64, parent_hash: Vec<u8>, difficulty: Vec<u8>, weight: U256, state_root: Vec<u8>) -> Block {
    let mut weight_bytes = [0u8; 32];
    weight.to_little_endian(&mut weight_bytes);
    
    let header = BlockHeader {
        height,
        parent_hash: Some(btc_genz_scl::proto::common::Hash { value: parent_hash }),
        timestamp: 1000 + height * 10,
        tx_root: Some(btc_genz_scl::proto::common::Hash { value: vec![0; 32] }),
        difficulty,
        absolute_weight: weight_bytes.to_vec(),
        nonce: 0,
        miner_address: Some(btc_genz_scl::proto::common::Address { value: vec![0; 32] }),
        state_root: Some(btc_genz_scl::proto::common::Hash { value: state_root }),
        ..Default::default()
    };
    
    Block {
        header: Some(header),
        body: Some(btc_genz_scl::proto::block::BlockBody {
            transactions: vec![],
        }),
    }
}

/// Helper function to save mock block to StateManager DB
fn commit_mock_block_to_db(mgr: &StateManager, block: &Block) -> [u8; 32] {
    let header = block.header.as_ref().unwrap();
    let header_hash = calculate_header_hash(header);
    let mut hash_arr = [0u8; 32];
    hash_arr.copy_from_slice(&header_hash);
    
    let header_raw = header.encode_to_vec();
    let body_raw = block.body.as_ref().unwrap().encode_to_vec();
    
    mgr.put_block_hash(header.height, &hash_arr);
    mgr.put_header(&hash_arr, header_raw);
    mgr.put_block_body(&hash_arr, body_raw);
    
    hash_arr
}

/// 6. Kiểm thử Bàn tay Vô hình / Trọng tài Năng lượng (VNT Consensus)
/// Đảm bảo sidechain rẽ nhánh sâu (dưới mốc finalized) bị từ chối nếu nhẹ, nhưng được thông hành nếu nặng gấp 5 lần.
#[test]
fn test_heavier_sidechain_reorg() {
    let (mgr, _tmp) = create_test_state_manager();
    let _ = GLOBAL_STATE_MANAGER.set(mgr.clone());
    
    let mut diff_bytes = vec![0u8; 32];
    (*MIN_DIFFICULTY).to_little_endian(&mut diff_bytes);
    
    // Khởi tạo Genesis Block (Height 0)
    let genesis_root = mgr.consolidate_smt_batch(vec![], 0);
    let genesis_block = create_mock_block(0, vec![0; 32], diff_bytes.clone(), U256::from(10), genesis_root.to_vec());
    let genesis_hash = commit_mock_block_to_db(&mgr, &genesis_block);
    
    // Tiền tính toán State Roots từ Genesis đến cao độ 30 bằng cách thực thi trực tiếp trên mgr
    let mut state_roots = vec![[0u8; 32]; 31];
    state_roots[0] = genesis_root;
    
    let mut parent_hash = genesis_hash.to_vec();
    let mut current_weight = U256::from(10);
    for h in 1..=30 {
        // Thực thi để sinh SMT state root đúng chuẩn (có block reward)
        let body_raw = btc_genz_scl::proto::block::BlockBody { transactions: vec![] }.encode_to_vec();
        let (payload, _, _) = btc_genz_scl::execute_block_internal(
            body_raw.clone(),
            vec![0; 32],
            parent_hash.clone(),
            h as u64,
            false
        ).unwrap();
        state_roots[h as usize] = payload.final_root;
        
        current_weight = current_weight + U256::from(1);
        let block = create_mock_block(h as u64, parent_hash.clone(), diff_bytes.clone(), current_weight, payload.final_root.to_vec());
        
        let header = block.header.as_ref().unwrap();
        let header_hash = calculate_header_hash(header);
        let mut hash_arr = [0u8; 32];
        hash_arr.copy_from_slice(&header_hash);
        
        let mut weight_bytes = [0u8; 32];
        current_weight.to_little_endian(&mut weight_bytes);
        
        mgr.commit_block_atomic(
            h as u64,
            &hash_arr,
            header.encode_to_vec(),
            body_raw,
            payload.tx_hashes,
            payload.touched_accs,
            payload.state_batch,
            payload.actual_total_supply,
            payload.actual_total_supply,
            weight_bytes.to_vec()
        );
        parent_hash = header_hash;
    }

    // Reset sạch trạng thái DB để triệt tiêu mọi node thừa của các version cũ trong JMT
    mgr.reset_state_completely().unwrap();
    
    // Xóa sạch cả các CF lưu trữ block dữ liệu để đảm bảo database mới tinh khi tái xây dựng
    let blocks_cf = mgr.db.cf_handle("blocks").unwrap();
    let headers_cf = mgr.db.cf_handle("headers").unwrap();
    let bodies_cf = mgr.db.cf_handle("block_bodies").unwrap();
    for cf in [blocks_cf, headers_cf, bodies_cf] {
        let mut batch = rocksdb::WriteBatch::default();
        let iter = mgr.db.iterator_cf(cf, rocksdb::IteratorMode::Start);
        for item in iter {
            if let Ok((key, _)) = item {
                batch.delete_cf(cf, key);
            }
        }
        mgr.db.write(batch).unwrap();
    }
    
    // Tái thiết Genesis Block
    let genesis_root = mgr.consolidate_smt_batch(vec![], 0);
    let genesis_block = create_mock_block(0, vec![0; 32], diff_bytes.clone(), U256::from(10), genesis_root.to_vec());
    let genesis_hash = commit_mock_block_to_db(&mgr, &genesis_block);
    mgr.current_version.store(0, std::sync::atomic::Ordering::SeqCst);

    // Tái xây dựng Main Chain cao độ 1..=10 sạch sẽ hoàn toàn
    let mut parent_hash = genesis_hash.to_vec();
    let mut current_weight = U256::from(10);
    for h in 1..=10 {
        let body_raw = btc_genz_scl::proto::block::BlockBody { transactions: vec![] }.encode_to_vec();
        let (payload, _, _) = btc_genz_scl::execute_block_internal(
            body_raw.clone(),
            vec![0; 32],
            parent_hash.clone(),
            h as u64,
            false
        ).unwrap();
        
        current_weight = current_weight + U256::from(1);
        let block = create_mock_block(h as u64, parent_hash.clone(), diff_bytes.clone(), current_weight, payload.final_root.to_vec());
        
        let header = block.header.as_ref().unwrap();
        let header_hash = calculate_header_hash(header);
        let mut hash_arr = [0u8; 32];
        hash_arr.copy_from_slice(&header_hash);
        
        let mut weight_bytes = [0u8; 32];
        current_weight.to_little_endian(&mut weight_bytes);
        
        mgr.commit_block_atomic(
            h as u64,
            &hash_arr,
            header.encode_to_vec(),
            body_raw,
            payload.tx_hashes,
            payload.touched_accs,
            payload.state_batch,
            payload.actual_total_supply,
            payload.actual_total_supply,
            weight_bytes.to_vec()
        );
        parent_hash = header_hash;
    }
    
    assert_eq!(mgr.get_current_version(), 10);

    // Thiết lập Tường lửa Đá Tảng (Finalized Height) chốt cứng tại cao độ 10
    mgr.force_set_finalized_height(10);
    assert_eq!(mgr.get_finalized_height(), 10);

    // Điểm rẽ nhánh sâu tại cao độ 8 (< 10 finalized)
    // Trọng số tại cao độ 8 là 18, trọng số tại local tip #10 là 20.
    // Local Segment Weight = 20 - 18 = 2.
    // Required Weight for x5 bypass = 2 * 5 = 10.
    let fork_point_hash = mgr.get_block_hash(8).unwrap().to_vec();

    // TRƯỜNG HỢP A: Nhánh rẽ sâu nhẹ hơn (Chỉ có 1 khối rẽ nhánh)
    // Sidechain B chỉ bắt đầu từ 9..=9, trọng số tích lũy của đoạn mới = 1.
    // 1 < 2 -> Vi phạm tường lửa, trả về status 2 (bị ban).
    let mut side_a_parent = fork_point_hash.clone();
    let mut side_a_weight = U256::from(18);
    let mut blocks_side_a = Vec::new();
    for h in 9..=9 {
        side_a_weight = side_a_weight + U256::from(1);
        let block = create_mock_block(h, side_a_parent.clone(), diff_bytes.clone(), side_a_weight, state_roots[h as usize].to_vec());
        let block_raw = block.encode_to_vec();
        side_a_parent = calculate_header_hash(block.header.as_ref().unwrap());
        blocks_side_a.push((block, block_raw));
    }

    // Kết quả mong đợi: Bị từ chối thẳng thừng vì năng lượng phân đoạn mới quá nhẹ
    let req_a = SyncChainRequest {
        blocks_raw: blocks_side_a.into_iter().map(|(_, raw)| raw).collect(),
    };
    let resp_a = btc_genz_scl::consensus::process_chain(req_a, false, 0);
    println!("DEBUG TEST - resp_a status: {}, error_msg: {}", resp_a.status, resp_a.error_msg);
    assert_eq!(resp_a.status, 2, "Chuỗi rẽ nhánh sâu nhẹ hơn phải bị từ chối với status 2");
    assert!(resp_a.error_msg.contains("ERR_IMMUTABLE_FIREWALL_VIOLATION"), "Lỗi phải báo vi phạm tường lửa");

    // TRƯỜNG HỢP B: Nhánh rẽ sâu nặng gấp 10 lần (Bàn tay vô hình giải cứu)
    // Sidechain C bắt đầu từ 9..=30 (22 khối), mỗi khối tích lũy 1 đơn vị độ khó.
    // Tổng năng lượng mới = 22 >= 20 -> Đạt ngưỡng x10!
    let mut side_b_parent = fork_point_hash.clone();
    let mut side_b_weight = U256::from(18);
    let mut blocks_side_b = Vec::new();
    for h in 9..=30 {
        side_b_weight = side_b_weight + U256::from(1);
        let block = create_mock_block(h, side_b_parent.clone(), diff_bytes.clone(), side_b_weight, state_roots[h as usize].to_vec());
        let block_raw = block.encode_to_vec();
        side_b_parent = calculate_header_hash(block.header.as_ref().unwrap());
        blocks_side_b.push((block, block_raw));
    }

    // Kết quả mong đợi: Chấp nhận, rollback, và Reorg thành công!
    let req_b = SyncChainRequest {
        blocks_raw: blocks_side_b.into_iter().map(|(_, raw)| raw).collect(),
    };
    let resp_b = btc_genz_scl::consensus::process_chain(req_b, false, 0);
    println!("DEBUG TEST - resp_b status: {}, error_msg: {}", resp_b.status, resp_b.error_msg);
    assert_eq!(resp_b.status, 1, "Chuỗi rẽ nhánh sâu nặng x10 phải được thông hành thành công");
    assert_eq!(mgr.get_current_version(), 30, "Chiều cao node phải cập nhật lên 30");
}


