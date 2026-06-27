/**
 * @file reward_logic.rs
 * @brief Logic tính toán phần thưởng khối mới cho YonaCode (Tổng cung 20 Triệu Coin, Chu kỳ 75s).
 * @details Phân bổ 20.000.000 BTC_Z theo lộ trình phát hành mới (Năm 1 - Năm 307).
 * Cơ chế "Single Winner" - Thợ đào nhận 100% phần thưởng và phí.
 * 
 */

// Tổng cung tối đa: 20 triệu coin (đơn vị VNT: 1 coin = 100,000,000 VNT)
pub const TOTAL_SUPPLY_VNT: u64 = 20_000_000 * 100_000_000;
pub const BLOCK_YEAR: u64 = 420_480; // 1 năm = 420,480 khối (chu kỳ khối 75 giây)

// Phần thưởng cố định tại các giai đoạn (đơn vị VNT)
pub const REWARD_PHA0_VNT: u64 = 10_000 * 100_000_000; // 1,000,000,000,000 VNT
pub const REWARD_YEAR1_VNT: u64 = 47_564_700;        // 0.475647 coin
pub const REWARD_YEAR2_VNT: u64 = 386_463_100;       // 3.864631 coin
pub const REWARD_YEAR3_VNT: u64 = 505_374_800;       // 5.053748 coin
pub const REWARD_YEAR4_VNT: u64 = 624_286_500;       // 6.242865 coin
pub const REWARD_YEAR5_VNT: u64 = 743_198_200;       // 7.431982 coin

// Điểm chiều cao khối giới hạn cho từng giai đoạn
pub const HEIGHT_PHA0_END: u64 = 99;
pub const HEIGHT_YEAR1_END: u64 = 420_579;
pub const HEIGHT_YEAR2_END: u64 = 841_059;
pub const HEIGHT_YEAR3_END: u64 = 1_261_539;
pub const HEIGHT_YEAR4_END: u64 = 1_682_019;
pub const HEIGHT_YEAR5_END: u64 = 2_102_499;
pub const HEIGHT_YEAR6_END: u64 = 2_522_979;
pub const HEIGHT_YEAR7_END: u64 = 2_943_459;
// Kéo dài thời gian phát thải Kỷ nguyên dài hạn (Year 8+) từ 126,144,000 khối lên 131,556,810 khối
// Tổng thời gian phát thải tăng lên ~320 năm để tổng cung đạt chính xác 20,000,000 coin.
pub const HEIGHT_YEAR307_END: u64 = 134_500_269;
pub const TAIL_CORRECTION_VNT: u64 = 1_788_118; // Lượng bù đắp làm tròn số học ở khối cuối cùng


/// [V1.0 FINAL] Tính toán phần thưởng khối BTC_Z chuẩn theo Bản đồ phát hành mới.
pub fn calculate_block_reward_btc_z(height: u64) -> u64 {
    if height <= HEIGHT_PHA0_END {
        // Pha 0: Khởi thủy (Khối 0 - 99): Cố định 10,000.000000 coin
        REWARD_PHA0_VNT
    } else if height <= HEIGHT_YEAR1_END {
        // Năm 1 (Khối 100 - 420,579): Cố định 0.475647 coin
        REWARD_YEAR1_VNT
    } else if height <= HEIGHT_YEAR2_END {
        // Năm 2 (Khối 420,580 - 841,059): Cố định 3.864631 coin
        REWARD_YEAR2_VNT
    } else if height <= HEIGHT_YEAR3_END {
        // Năm 3 (Khối 841,060 - 1,261,539): Cố định 5.053748 coin
        REWARD_YEAR3_VNT
    } else if height <= HEIGHT_YEAR4_END {
        // Năm 4 (Khối 1,261,540 - 1,682,019): Cố định 6.242865 coin
        REWARD_YEAR4_VNT
    } else if height <= HEIGHT_YEAR5_END {
        // Năm 5 (Khối 1,682,020 - 2,102,499): Cố định 7.431982 coin
        REWARD_YEAR5_VNT
    } else if height <= HEIGHT_YEAR6_END {
        // Năm 6 (Khối 2,102,500 - 2,522,979): Giảm tuyến tính từ 7.431982 về 3.472222 coin
        let start_reward = REWARD_YEAR5_VNT as u128;
        let end_reward = 347_222_200u128; // 3.472222 coin
        let start_height = (HEIGHT_YEAR5_END + 1) as u128;
        let end_height = HEIGHT_YEAR6_END as u128;
        
        let h_delta = height as u128 - start_height;
        let h_total = end_height - start_height;
        let r_delta = start_reward - end_reward;
        
        let reduction = (r_delta * h_delta) / h_total;
        (start_reward - reduction) as u64
    } else if height <= HEIGHT_YEAR7_END {
        // Năm 7 (Khối 2,522,980 - 2,943,459): Giảm tuyến tính từ 3.472222 về 0.095130 coin
        let start_reward = 347_222_200u128; // 3.472222 coin
        let end_reward = 9_513_000u128;     // 0.095130 coin
        let start_height = (HEIGHT_YEAR6_END + 1) as u128;
        let end_height = HEIGHT_YEAR7_END as u128;
        
        let h_delta = height as u128 - start_height;
        let h_total = end_height - start_height;
        let r_delta = start_reward - end_reward;
        
        let reduction = (r_delta * h_delta) / h_total;
        (start_reward - reduction) as u64
    } else if height <= HEIGHT_YEAR307_END {
        // Năm 8 - Năm 320 (Khối 2,943,460 - 134,500,269): Kỷ nguyên dài hạn giảm dần từ 0.095130 về 0 coin
        if height == HEIGHT_YEAR307_END {
            return TAIL_CORRECTION_VNT; // Khối cuối cùng nhận lượng bù đắp làm tròn để tổng cung đạt chính xác 20M
        }
        let start_reward = 9_513_000u128; // 0.095130 coin
        let end_reward = 0u128;
        let start_height = (HEIGHT_YEAR7_END + 1) as u128;
        let end_height = HEIGHT_YEAR307_END as u128;
        
        let h_delta = height as u128 - start_height;
        let h_total = end_height - start_height;
        let r_delta = start_reward - end_reward;
        
        let reduction = (r_delta * h_delta) / h_total;
        (start_reward - reduction) as u64
    } else {
        // Hết lộ trình: Phần thưởng về 0
        0
    }
}

/// [V1.0] Miner nhận 100% phí giao dịch
pub fn distribute_fees_to_miner(total_fee_pool: u64) -> u64 {
    total_fee_pool
}

/// [V1.0] Tổng hợp phần thưởng và phí cho khối
pub fn get_total_miner_reward(height: u64, fees: u64) -> u64 {
    calculate_block_reward_btc_z(height) + distribute_fees_to_miner(fees)
}

/// [V2.1 ELITE] Tính tổng cung kỳ vọng từ Genesis đến height với độ chính xác tuyệt đối (Bit-perfect).
/// Sử dụng thuật toán Floor Sum để tính tổng các phần thưởng có hàm làm tròn xuống trong O(log N).
pub fn calculate_expected_supply_from_genesis_fallback(height: u64) -> u64 {
    // 1. Pha 0: Khởi Thủy
    let p0_blocks = height.min(HEIGHT_PHA0_END);
    let mut total_supply = (p0_blocks + 1) * REWARD_PHA0_VNT;
    if height <= HEIGHT_PHA0_END {
        return total_supply;
    }
    
    // 2. Năm 1
    let p1_blocks = (height.min(HEIGHT_YEAR1_END) - HEIGHT_PHA0_END) as u64;
    total_supply += p1_blocks * REWARD_YEAR1_VNT;
    if height <= HEIGHT_YEAR1_END {
        return total_supply;
    }
    
    // 3. Năm 2
    let p2_blocks = (height.min(HEIGHT_YEAR2_END) - HEIGHT_YEAR1_END) as u64;
    total_supply += p2_blocks * REWARD_YEAR2_VNT;
    if height <= HEIGHT_YEAR2_END {
        return total_supply;
    }
    
    // 4. Năm 3
    let p3_blocks = (height.min(HEIGHT_YEAR3_END) - HEIGHT_YEAR2_END) as u64;
    total_supply += p3_blocks * REWARD_YEAR3_VNT;
    if height <= HEIGHT_YEAR3_END {
        return total_supply;
    }
    
    // 5. Năm 4
    let p4_blocks = (height.min(HEIGHT_YEAR4_END) - HEIGHT_YEAR3_END) as u64;
    total_supply += p4_blocks * REWARD_YEAR4_VNT;
    if height <= HEIGHT_YEAR4_END {
        return total_supply;
    }
    
    // 6. Năm 5
    let p5_blocks = (height.min(HEIGHT_YEAR5_END) - HEIGHT_YEAR4_END) as u64;
    total_supply += p5_blocks * REWARD_YEAR5_VNT;
    if height <= HEIGHT_YEAR5_END {
        return total_supply;
    }
    
    // 7. Năm 6 (Giảm tuyến tính)
    let s6_start_reward = REWARD_YEAR5_VNT as u128;
    let s6_end_reward = 347_222_200u128;
    let s6_m = (HEIGHT_YEAR6_END - (HEIGHT_YEAR5_END + 1)) as u128;
    let s6_r_delta = s6_start_reward - s6_end_reward;
    
    if height <= HEIGHT_YEAR6_END {
        let n = (height - HEIGHT_YEAR5_END) as u128;
        let sum_reduction = floor_sum(n, s6_m, s6_r_delta, 0);
        let supply = n * s6_start_reward - sum_reduction;
        return total_supply + supply as u64;
    }
    
    // Cộng dồn toàn bộ Năm 6
    let s6_n_total = (HEIGHT_YEAR6_END - HEIGHT_YEAR5_END) as u128;
    let s6_sum_reduction = floor_sum(s6_n_total, s6_m, s6_r_delta, 0);
    total_supply += (s6_n_total * s6_start_reward - s6_sum_reduction) as u64;
    
    // 8. Năm 7 (Giảm tuyến tính)
    let s7_start_reward = 347_222_200u128;
    let s7_end_reward = 9_513_000u128;
    let s7_m = (HEIGHT_YEAR7_END - (HEIGHT_YEAR6_END + 1)) as u128;
    let s7_r_delta = s7_start_reward - s7_end_reward;
    
    if height <= HEIGHT_YEAR7_END {
        let n = (height - HEIGHT_YEAR6_END) as u128;
        let sum_reduction = floor_sum(n, s7_m, s7_r_delta, 0);
        let supply = n * s7_start_reward - sum_reduction;
        return total_supply + supply as u64;
    }
    
    // Cộng dồn toàn bộ Năm 7
    let s7_n_total = (HEIGHT_YEAR7_END - HEIGHT_YEAR6_END) as u128;
    let s7_sum_reduction = floor_sum(s7_n_total, s7_m, s7_r_delta, 0);
    total_supply += (s7_n_total * s7_start_reward - s7_sum_reduction) as u64;
    
    // 9. Năm 8 - Năm 320 (Giảm tuyến tính + bù đắp ở khối cuối cùng)
    let s8_start_reward = 9_513_000u128;
    let s8_end_reward = 0u128;
    let s8_m = (HEIGHT_YEAR307_END - (HEIGHT_YEAR7_END + 1)) as u128;
    let s8_r_delta = s8_start_reward - s8_end_reward;
    
    if height <= HEIGHT_YEAR307_END {
        if height == HEIGHT_YEAR307_END {
            let n = (HEIGHT_YEAR307_END - 1 - HEIGHT_YEAR7_END) as u128;
            let sum_reduction = floor_sum(n, s8_m, s8_r_delta, 0);
            let supply = n * s8_start_reward - sum_reduction;
            return total_supply + supply as u64 + TAIL_CORRECTION_VNT;
        }
        let n = (height - HEIGHT_YEAR7_END) as u128;
        let sum_reduction = floor_sum(n, s8_m, s8_r_delta, 0);
        let supply = n * s8_start_reward - sum_reduction;
        return total_supply + supply as u64;
    }
    
    // Cộng dồn toàn bộ Kỷ nguyên dài hạn (đã bao gồm khối cuối cùng với lượng bù đắp)
    let s8_n_total = (HEIGHT_YEAR307_END - 1 - HEIGHT_YEAR7_END) as u128;
    let s8_sum_reduction = floor_sum(s8_n_total, s8_m, s8_r_delta, 0);
    total_supply += (s8_n_total * s8_start_reward - s8_sum_reduction) as u64 + TAIL_CORRECTION_VNT;
    
    total_supply
}

/// [MATHEMATICAL-CORE] Thuật toán Floor Sum: Tính Sum_{i=0}^{n-1} floor((a*i + b) / m)
/// Độ phức tạp: O(log(min(a, m)))
fn floor_sum(n: u128, m: u128, a: u128, b: u128) -> u128 {
    let mut ans = 0;
    let mut n = n;
    let mut m = m;
    let mut a = a;
    let mut b = b;

    if a >= m {
        ans += (n - 1) * n / 2 * (a / m);
        a %= m;
    }
    if b >= m {
        ans += n * (b / m);
        b %= m;
    }

    let y_max = (a * n + b) / m;
    let x_max = y_max * m - b;
    if y_max == 0 {
        return ans;
    }
    ans += (n - (x_max + a - 1) / a) * y_max;
    ans += floor_sum(y_max, a, m, (a - x_max % a) % a);
    ans
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_block_rewards_boundary() {
        // Pha 0 (Golden Hour)
        assert_eq!(calculate_block_reward_btc_z(0), 1_000_000_000_000);
        assert_eq!(calculate_block_reward_btc_z(99), 1_000_000_000_000);
        
        // Năm 1
        assert_eq!(calculate_block_reward_btc_z(100), 47_564_700);
        assert_eq!(calculate_block_reward_btc_z(420_579), 47_564_700);
        
        // Năm 2
        assert_eq!(calculate_block_reward_btc_z(420_580), 386_463_100);
        assert_eq!(calculate_block_reward_btc_z(841_059), 386_463_100);
        
        // Năm 3
        assert_eq!(calculate_block_reward_btc_z(841_060), 505_374_800);
        assert_eq!(calculate_block_reward_btc_z(1_261_539), 505_374_800);
        
        // Năm 4
        assert_eq!(calculate_block_reward_btc_z(1_261_540), 624_286_500);
        assert_eq!(calculate_block_reward_btc_z(1_682_019), 624_286_500);
        
        // Năm 5
        assert_eq!(calculate_block_reward_btc_z(1_682_020), 743_198_200);
        assert_eq!(calculate_block_reward_btc_z(2_102_499), 743_198_200);

        // Năm 6 (giảm dần từ 7.431982 về 3.472222)
        assert_eq!(calculate_block_reward_btc_z(2_102_500), 743_198_200);
        assert_eq!(calculate_block_reward_btc_z(2_522_979), 347_222_200);
        assert!(calculate_block_reward_btc_z(2_312_740) < 743_198_200);
        assert!(calculate_block_reward_btc_z(2_312_740) > 347_222_200);

        // Năm 7 (giảm dần từ 3.472222 về 0.095130)
        assert_eq!(calculate_block_reward_btc_z(2_522_980), 347_222_200);
        assert_eq!(calculate_block_reward_btc_z(2_943_459), 9_513_000);

        // Năm 8 - 320 (giảm dần từ 0.095130 về 0)
        assert_eq!(calculate_block_reward_btc_z(2_943_460), 9_513_000);
        assert_eq!(calculate_block_reward_btc_z(134_500_268), 1); // khối áp chót
        assert_eq!(calculate_block_reward_btc_z(134_500_269), 1_788_118); // khối cuối cùng với lượng bù đắp
        
        // Ngoài lộ trình
        assert_eq!(calculate_block_reward_btc_z(134_500_270), 0);
        
        // Kiểm tra tổng cung tích lũy tại khối cuối cùng
        assert_eq!(calculate_expected_supply_from_genesis_fallback(134_500_269), 2_000_000_000_000_000);
        assert_eq!(calculate_expected_supply_from_genesis_fallback(134_500_270), 2_000_000_000_000_000);
    }

    #[test]
    fn test_expected_supply_consistency() {
        // Tổng cung tích lũy mốc Golden Hour: 100 * 10,000 coin = 1,000,000 coin
        assert_eq!(calculate_expected_supply_from_genesis_fallback(99), 100_000_000_000_000);
        
        // Khối 100: Thêm 0.475647 coin
        assert_eq!(calculate_expected_supply_from_genesis_fallback(100), 100_000_047_564_700);

        // So sánh tính nhất quán giữa cộng dồn thủ công và thuật toán Floor Sum
        let test_heights = [
            10, 99, 100, 420_579, 420_580, 841_059, 841_060,
            1_261_539, 1_261_540, 1_682_019, 1_682_020, 2_102_499,
            2_102_500, 2_102_501, 2_200_000, 2_522_979, 2_522_980,
            2_522_981, 2_700_000, 2_943_459, 2_943_460, 2_943_461,
            3_000_000
        ];
        for &h in &test_heights {
            let mut manual_sum = 0;
            for i in 0..=h {
                manual_sum += calculate_block_reward_btc_z(i);
            }
            assert_eq!(
                calculate_expected_supply_from_genesis_fallback(h),
                manual_sum,
                "Lệch tổng cung tại h = {}", h
            );
        }
    }
}
