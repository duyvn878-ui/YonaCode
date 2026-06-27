/**
 * @file difficulty_logic.rs
 * @brief Thuật toán điều chỉnh độ khó cho YonaCode - Elite V1.3.0 (Standard U256).
 * @details Sử dụng LWMA (Zawy Style) với toán học 256-bit để đảm bảo tính ổn định tuyệt đối.
 */

use primitive_types::U256;

// Thời gian mục tiêu giữa các khối: 75 giây
pub const TARGET_BLOCK_TIME_SECONDS: u64 = 75;
pub const LWMA_WINDOW: usize = 120;

// [V1.3.0] MIN_DIFFICULTY: 1.2 Tỷ (1,200,000,000) - Ngưỡng tối thiểu của độ khó trên Mainnet.
// Lưu ý: Trong hệ thống này, difficulty càng cao thì càng KHÓ (giống khái niệm Difficulty của Bitcoin),
// nhưng chúng ta sẽ chuyển đổi nó sang Target để so sánh Hash < Target.
lazy_static::lazy_static! {
    pub static ref MIN_DIFFICULTY: U256 = U256::from(1_200_000_000u64);
    pub static ref MAX_TARGET: U256 = U256::MAX;
}

/// [V1.3.0 ELITE] Thuật toán LWMA (Linear Weighted Moving Average) - Chuẩn U256
pub fn calculate_next_difficulty(
    timestamps: &[u64],
    difficulties: &[U256],
    _current_ts: u64,
    height: u64,
) -> U256 {
    let n = LWMA_WINDOW;
    let len = timestamps.len();
    let diff_len = difficulties.len();

    let last_diff = difficulties.last().cloned().unwrap_or(*MIN_DIFFICULTY);

    // 🛡️ [V1.3.0 BOOTSTRAP GUARD]
    if len < n + 1 || diff_len < n {
        return last_diff.max(*MIN_DIFFICULTY);
    }

    let mut solve_time_sum: i64 = 0;
    let mut weighted_diff_sum = U256::zero();
    
    // LWMA Core (Zawy Style) với U256 - Chỉ sử dụng dữ liệu lịch sử để chống Time-Warp Attack
    for i in 1..=n {
        let ts_i = timestamps[len - n - 1 + i] as i64;
        let ts_prev = timestamps[len - n - 1 + i - 1] as i64;
        
        // Giới hạn solve time để tránh outlier làm sập DAA
        let solve_time = (ts_i - ts_prev).max(1).min(TARGET_BLOCK_TIME_SECONDS as i64 * 10);
        
        solve_time_sum += solve_time * i as i64;
        let term = difficulties[diff_len - n + i - 1].saturating_mul(U256::from(i));
        weighted_diff_sum = weighted_diff_sum.saturating_add(term);
    }

    if solve_time_sum <= 0 { solve_time_sum = 1; }
    
    // Phép nhân trước chia sau để bảo toàn độ chính xác và sử dụng checked arithmetic để triệt tiêu rủi ro tràn số.
    // Loại bỏ hoàn toàn phép làm tròn cộng thêm nguy hiểm gây nguy cơ hoảng loạn (panic) hệ thống.
    let target_block_time_u256 = U256::from(TARGET_BLOCK_TIME_SECONDS);
    let denominator = U256::from(solve_time_sum as u64);

    let next_diff = match weighted_diff_sum.checked_mul(target_block_time_u256) {
        Some(numerator) => numerator.checked_div(denominator).unwrap_or(U256::MAX),
        None => U256::MAX,
    };

    println!("[DAA] H#{} | Next: {} | Last: {}", height, next_diff, last_diff);

    // [PRODUCTION] Giới hạn biến động độ khó: ±25% mỗi khối
    // Tại sao 25%: Đảm bảo DAA phản ứng đủ nhanh khi hashrate biến động đột ngột
    // (ví dụ: thợ đào cắm/rút ASIC), nhưng hạn chế dao động quá lớn gây bất ổn định chuỗi.
    // - Giảm tối đa: -25% (độ khó khối sau >= 0.75 lần khối trước)
    // - Tăng tối đa: +25% (độ khó khối sau <= 1.25 lần khối trước)
    let delta = last_diff.checked_div(U256::from(4u64)).unwrap_or(U256::zero());

    let min_allowed = last_diff.saturating_sub(delta);
    let max_allowed = last_diff.saturating_add(delta);
    
    let clamped = next_diff.max(min_allowed).min(max_allowed);
    
    clamped.max(*MIN_DIFFICULTY)
}


/// [V1.3.0] ASERT - Chuẩn U256 (ASERT-2* chống trôi tuyệt đối)
pub fn calculate_next_difficulty_asert_u256(
    prev_height: u64,
    prev_ts: u64,
    anchor_height: u64,
    anchor_timestamp: u64,
    anchor_diff: U256,
) -> U256 {
    // Chu kỳ bán rã (Halflife) = 2 ngày (172.800 giây)
    const HALF_LIFE_SECONDS: u64 = 172_800;

    if prev_height <= anchor_height {
        return anchor_diff.max(*MIN_DIFFICULTY);
    }

    // T_target = (prev_height - anchor_height) * TARGET_BLOCK_TIME_SECONDS
    let t_target = (prev_height - anchor_height) * TARGET_BLOCK_TIME_SECONDS;
    
    // T_actual = prev_ts - anchor_timestamp
    let t_actual = prev_ts.saturating_sub(anchor_timestamp);

    // input_val = T_target - T_actual
    let input_val = t_target as i128 - t_actual as i128;
    let is_positive = input_val >= 0;
    let abs_input = input_val.abs() as u64;

    // exponent = abs_input / HALF_LIFE_SECONDS
    let d = abs_input / HALF_LIFE_SECONDS;
    let r = abs_input % HALF_LIFE_SECONDS;

    // Tính factor = 2^(r / HALF_LIFE_SECONDS) * 2^16 qua xấp xỉ đa thức Taylor bậc 2:
    // 2^y ≈ 1 + y*ln(2) + 0.5*(y*ln(2))^2
    // Với y = r / HALF_LIFE_SECONDS, ta nhân với 2^16 = 65536 để dùng số nguyên
    let r_u256 = U256::from(r);
    let tau_u256 = U256::from(HALF_LIFE_SECONDS);
    
    // Tính toán Taylor chính xác hơn bằng cách gom phép nhân tử số trước
    let term1 = (r_u256 * U256::from(45426)) / tau_u256;
    
    // Tránh mất độ chính xác số nguyên của bậc 2 bằng cách gom phép nhân tử số trước
    let term2_numerator = r_u256 * r_u256 * U256::from(15741);
    let term2_denominator = tau_u256 * tau_u256;
    let term2 = term2_numerator / term2_denominator;
    
    let factor = U256::from(65536) + term1 + term2;

    let mut next_diff = if is_positive {
        // Xử lý an toàn nhân trước tránh tràn và dùng checked_shl
        match anchor_diff.checked_mul(factor) {
            Some(prod) => {
                let temp = prod >> 16;
                if d < 256 {
                    if temp.bits() + d as usize > 256 {
                        U256::MAX
                    } else {
                        temp << d
                    }
                } else {
                    U256::MAX
                }
            }
            None => U256::MAX,
        }
    } else {
        // Thay vì dùng dịch bit trái trực tiếp (anchor_diff << 16) dễ mất bit, ta dùng checked_mul
        match anchor_diff.checked_mul(U256::from(65536)) {
            Some(scaled_anchor) => {
                let temp = scaled_anchor / factor;
                if d < 256 {
                    temp >> d
                } else {
                    U256::zero()
                }
            }
            None => {
                // Dự phòng nếu anchor_diff quá lớn không nhân nổi với 65536, chia trước dịch sau
                let temp = (anchor_diff / factor) * U256::from(65536);
                if d < 256 { temp >> d } else { U256::zero() }
            }
        }
    };

    next_diff.max(*MIN_DIFFICULTY)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_asert_difficulty_calculations() {
        let anchor_diff = U256::from(15_000_000_000u64);
        let anchor_ts = 10000u64;
        let anchor_height = 100u64;

        // 1. T_actual == T_target -> Độ khó không đổi
        let prev_height = 104u64; // +4 blocks
        let prev_ts = anchor_ts + 4 * TARGET_BLOCK_TIME_SECONDS; // Đúng 4 * 75s
        let next_diff = calculate_next_difficulty_asert_u256(
            prev_height,
            prev_ts,
            anchor_height,
            anchor_ts,
            anchor_diff,
        );
        assert_eq!(next_diff, anchor_diff);

        // 2. T_actual < T_target -> Đào quá nhanh -> Độ khó tăng
        let prev_ts_fast = anchor_ts + 2 * TARGET_BLOCK_TIME_SECONDS; // Lẽ ra là 4, mới trôi qua 2
        let next_diff_fast = calculate_next_difficulty_asert_u256(
            prev_height,
            prev_ts_fast,
            anchor_height,
            anchor_ts,
            anchor_diff,
        );
        assert!(next_diff_fast > anchor_diff);

        // 3. T_actual > T_target -> Đào quá chậm -> Độ khó giảm
        let prev_ts_slow = anchor_ts + 8 * TARGET_BLOCK_TIME_SECONDS; // Lẽ ra là 4, trôi qua tận 8
        let next_diff_slow = calculate_next_difficulty_asert_u256(
            prev_height,
            prev_ts_slow,
            anchor_height,
            anchor_ts,
            anchor_diff,
        );
        assert!(next_diff_slow < anchor_diff);
    }
}