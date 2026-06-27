/**
 * @file consensus_tests.rs
 * @brief Kiểm thử bộ quy tắc đồng thuận (Consensus Rules & DAA) cho BTC GenZ.
 * @details Kiểm toán thuật toán điều chỉnh độ khó LWMA (Zawy Style), median time past (MTP-11),
 *          xác thực PoW raw và các phép toán an toàn tránh tràn số.
 */

use btc_genz_scl::difficulty_logic::{calculate_next_difficulty, LWMA_WINDOW, MIN_DIFFICULTY};
use btc_genz_scl::genz_pow::{difficulty_to_target, verify_pow_raw};
use btc_genz_scl::state_manager::StateManager;
use btc_genz_scl::proto::block::BlockHeader;
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

