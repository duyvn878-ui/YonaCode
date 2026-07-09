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
// Hằng số Hardfork dồn 300,000 Coin từ Kỷ nguyên 300 năm vào Năm 1 bắt đầu từ khối 17,000
pub const HARDFORK_HEIGHT: u64 = 17_000;
pub const REWARD_YEAR1_POST_FORK_VNT: u64 = 121_899_404;
pub const FORK_REMAINDER_VNT: u64 = 159_680;

// Chiều cao Kỷ nguyên dài hạn (Year 8+) mới rút ngắn từ 134,500,269 xuống 105,694,901 khối
pub const HEIGHT_YEAR307_END: u64 = 105_694_901;
pub const TAIL_CORRECTION_VNT: u64 = 1_434_078; // Lượng bù đắp làm tròn số học mới ở khối cuối cùng



/// [V1.0 FINAL] Tính toán phần thưởng khối BTC_Z chuẩn theo Bản đồ phát hành mới.
pub fn calculate_block_reward_btc_z(height: u64) -> u64 {
    if height <= HEIGHT_PHA0_END {
        // Pha 0: Khởi thủy (Khối 0 - 99): Cố định 10,000.000000 coin
        REWARD_PHA0_VNT
    } else if height <= HEIGHT_YEAR1_END {
        // Năm 1 (Khối 100 - 420,579): Cố định 0.475647 coin trước Fork, sau Fork tăng
        if height < HARDFORK_HEIGHT {
            REWARD_YEAR1_VNT
        } else if height == HARDFORK_HEIGHT {
            REWARD_YEAR1_POST_FORK_VNT + FORK_REMAINDER_VNT // Bù phần lẻ để tròn trịa toán học
        } else {
            REWARD_YEAR1_POST_FORK_VNT
        }
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
        let end_height = 134_500_269u128; // Giữ nguyên độ dốc phát thải gốc (trước hardfork)
        
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
    
    // 2. Năm 1 (Chứa Logic Hardfork)
    if height < HARDFORK_HEIGHT {
        let p1_blocks = (height.min(HEIGHT_YEAR1_END) - HEIGHT_PHA0_END) as u64;
        total_supply += p1_blocks * REWARD_YEAR1_VNT;
        if height <= HEIGHT_YEAR1_END {
            return total_supply;
        }
    } else {
        // Tính từ đầu Năm 1 đến trước Fork
        let pre_fork_blocks = (HARDFORK_HEIGHT - 1 - HEIGHT_PHA0_END) as u64;
        total_supply += pre_fork_blocks * REWARD_YEAR1_VNT;

        // Tính từ Fork đến hiện tại (hoặc hết Năm 1)
        let post_fork_blocks = (height.min(HEIGHT_YEAR1_END) - HARDFORK_HEIGHT + 1) as u64;
        total_supply += post_fork_blocks * REWARD_YEAR1_POST_FORK_VNT + FORK_REMAINDER_VNT;

        if height <= HEIGHT_YEAR1_END {
            return total_supply;
        }
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
    let s8_m = (134_500_269 - (HEIGHT_YEAR7_END + 1)) as u128; // Giữ nguyên độ dốc phát thải gốc (trước hardfork)
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
        
        // Năm 1 (trước và sau Fork)
        assert_eq!(calculate_block_reward_btc_z(100), 47_564_700);
        assert_eq!(calculate_block_reward_btc_z(16_999), 47_564_700);
        assert_eq!(calculate_block_reward_btc_z(17_000), 121_899_404 + 159_680); // Khối Hardfork nhận cả phần dư thừa
        assert_eq!(calculate_block_reward_btc_z(17_001), 121_899_404);
        assert_eq!(calculate_block_reward_btc_z(420_579), 121_899_404);
        
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
        assert_eq!(calculate_block_reward_btc_z(105_694_900), 2_082_945); // khối áp chót (chưa chạm mốc 0.0)
        assert_eq!(calculate_block_reward_btc_z(105_694_901), 1_434_078); // khối cuối cùng với lượng bù đắp
        
        // Ngoài lộ trình
        assert_eq!(calculate_block_reward_btc_z(105_694_902), 0);
        
        // Kiểm tra tổng cung tích lũy tại khối cuối cùng
        assert_eq!(calculate_expected_supply_from_genesis_fallback(105_694_901), 2_000_000_000_000_000);
        assert_eq!(calculate_expected_supply_from_genesis_fallback(105_694_902), 2_000_000_000_000_000);
    }

    #[test]
    fn test_expected_supply_consistency() {
        // Tổng cung tích lũy mốc Golden Hour: 100 * 10,000 coin = 1,000,000 coin
        assert_eq!(calculate_expected_supply_from_genesis_fallback(99), 100_000_000_000_000);
        
        // Khối 100: Thêm 0.475647 coin
        assert_eq!(calculate_expected_supply_from_genesis_fallback(100), 100_000_047_564_700);

        // So sánh tính nhất quán giữa cộng dồn thủ công và thuật toán Floor Sum
        let test_heights = [
            10, 99, 100, 16_999, 17_000, 17_001, 100_000, 420_579, 420_580, 841_059, 841_060,
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

    #[test]
    fn test_expected_supply_consistency_exhaustive() {
        // Kiểm tra cộng dồn liên tục từ khối 0 tới 200,000 để đảm bảo không có bất kỳ khối nào bị lệch
        // Đặc biệt kiểm tra KỸ vùng Hardfork (16990-17010) - đây là nơi node mới sẽ FAIL-STOP nếu sai
        let mut manual_sum = 0;
        for h in 0..=200_000 {
            manual_sum += calculate_block_reward_btc_z(h);
            if h % 1000 == 0 || (h >= 16990 && h <= 17010) {
                assert_eq!(
                    calculate_expected_supply_from_genesis_fallback(h),
                    manual_sum,
                    "Lệch tổng cung tại khối h = {}", h
                );
            }
        }
    }

    /// Kiểm tra TỪNG KHỐI qua MỌI ranh giới chuyển pha. Đây là nơi node mới đồng bộ sẽ
    /// gặp lỗi FAIL-STOP nếu calculate_expected_supply không khớp với cộng dồn thực tế.
    /// Kiểm tra ±50 khối quanh mỗi ranh giới.
    #[test]
    fn test_supply_at_every_phase_boundary() {
        let boundaries = [
            HEIGHT_PHA0_END,       // 99: Pha 0 → Năm 1
            HARDFORK_HEIGHT - 1,   // 16999: Trước Hardfork
            HARDFORK_HEIGHT,       // 17000: Khối Hardfork
            HARDFORK_HEIGHT + 1,   // 17001: Sau Hardfork
            HEIGHT_YEAR1_END,      // 420579: Năm 1 → Năm 2
            HEIGHT_YEAR2_END,      // 841059: Năm 2 → Năm 3
            HEIGHT_YEAR3_END,      // 1261539: Năm 3 → Năm 4
            HEIGHT_YEAR4_END,      // 1682019: Năm 4 → Năm 5
            HEIGHT_YEAR5_END,      // 2102499: Năm 5 → Năm 6 (bắt đầu giảm tuyến tính)
            HEIGHT_YEAR6_END,      // 2522979: Năm 6 → Năm 7
            HEIGHT_YEAR7_END,      // 2943459: Năm 7 → Năm 8 (kỷ nguyên dài hạn)
        ];

        for &boundary in &boundaries {
            // Tính tổng cung cộng dồn thủ công từ khối 0 đến boundary - 50
            let start = if boundary > 50 { boundary - 50 } else { 0 };
            let mut manual_sum = 0u64;
            for i in 0..=start {
                manual_sum += calculate_block_reward_btc_z(i);
            }
            assert_eq!(
                calculate_expected_supply_from_genesis_fallback(start),
                manual_sum,
                "[BOUNDARY-ANCHOR] Lệch tổng cung tại điểm neo h = {} (trước ranh giới {})", start, boundary
            );

            // Tiếp tục cộng dồn từng khối qua ranh giới
            for h in (start + 1)..=(boundary + 50) {
                manual_sum += calculate_block_reward_btc_z(h);
                assert_eq!(
                    calculate_expected_supply_from_genesis_fallback(h),
                    manual_sum,
                    "[PHASE-CROSS] Lệch tổng cung tại h = {} (quanh ranh giới {})", h, boundary
                );
            }
        }
    }

    /// Kiểm tra phần thưởng luôn dương (không bao giờ overflow/underflow) ở MỌI điểm trên đường cong.
    /// Đây là điều kiện tiên quyết để node không panic khi đồng bộ.
    #[test]
    fn test_reward_never_negative_or_overflow() {
        // Kiểm tra khối 0
        assert!(calculate_block_reward_btc_z(0) > 0);

        // Kiểm tra mọi giai đoạn cố định
        for h in [100, 16_999, 17_000, 17_001, 420_579, 420_580, 841_059, 841_060,
                  1_261_539, 1_261_540, 1_682_019, 1_682_020, 2_102_499] {
            assert!(calculate_block_reward_btc_z(h) > 0, "Phần thưởng <= 0 tại h = {}", h);
        }

        // Kiểm tra toàn bộ vùng giảm tuyến tính Năm 6 (lấy mẫu mỗi 1000 khối)
        for h in (2_102_500..=2_522_979).step_by(1000) {
            let r = calculate_block_reward_btc_z(h);
            assert!(r > 0, "Năm 6: Phần thưởng <= 0 tại h = {}", h);
            assert!(r <= 743_198_200, "Năm 6: Phần thưởng vượt trần tại h = {}", h);
        }

        // Kiểm tra toàn bộ vùng giảm tuyến tính Năm 7 (lấy mẫu mỗi 1000 khối)
        for h in (2_522_980..=2_943_459).step_by(1000) {
            let r = calculate_block_reward_btc_z(h);
            assert!(r > 0, "Năm 7: Phần thưởng <= 0 tại h = {}", h);
            assert!(r <= 347_222_200, "Năm 7: Phần thưởng vượt trần tại h = {}", h);
        }

        // Kiểm tra toàn bộ vùng giảm tuyến tính Năm 8-307 (lấy mẫu mỗi 100,000 khối)
        for h in (2_943_460..=105_694_901).step_by(100_000) {
            let r = calculate_block_reward_btc_z(h);
            assert!(r > 0, "Năm 8-307: Phần thưởng <= 0 tại h = {}", h);
            assert!(r <= 9_513_000, "Năm 8-307: Phần thưởng vượt trần tại h = {}", h);
        }

        // Ngoài lộ trình phải chính xác bằng 0
        assert_eq!(calculate_block_reward_btc_z(105_694_902), 0);
        assert_eq!(calculate_block_reward_btc_z(200_000_000), 0);
    }

    /// Kiểm tra tính đơn điệu giảm dần của phần thưởng trong các vùng giảm tuyến tính.
    /// Nếu vi phạm: node mới đồng bộ sẽ tính tổng cung sai → FAIL-STOP.
    #[test]
    fn test_reward_monotonically_decreasing() {
        // Năm 6: Phần thưởng phải giảm dần (hoặc giữ nguyên do floor) từ khối này sang khối kế
        let mut prev = calculate_block_reward_btc_z(2_102_500);
        for h in (2_102_501..=2_522_979).step_by(100) {
            let curr = calculate_block_reward_btc_z(h);
            assert!(curr <= prev, "Năm 6: Phần thưởng TĂNG bất thường tại h = {} ({} > {})", h, curr, prev);
            prev = curr;
        }

        // Năm 7: Phần thưởng phải giảm dần
        prev = calculate_block_reward_btc_z(2_522_980);
        for h in (2_522_981..=2_943_459).step_by(100) {
            let curr = calculate_block_reward_btc_z(h);
            assert!(curr <= prev, "Năm 7: Phần thưởng TĂNG bất thường tại h = {} ({} > {})", h, curr, prev);
            prev = curr;
        }

        // Năm 8-307: Phần thưởng phải giảm dần (ngoại trừ khối cuối cùng)
        prev = calculate_block_reward_btc_z(2_943_460);
        for h in (2_943_461..=105_694_900).step_by(10_000) {
            let curr = calculate_block_reward_btc_z(h);
            assert!(curr <= prev, "Năm 8-307: Phần thưởng TĂNG bất thường tại h = {} ({} > {})", h, curr, prev);
            prev = curr;
        }
    }

    /// Kiểm tra tổng cung tích lũy luôn đơn điệu tăng (không bao giờ giảm).
    /// Nếu vi phạm: node mới đồng bộ sẽ phát hiện tổng cung "lùi" → FAIL-STOP.
    #[test]
    fn test_total_supply_monotonically_increasing() {
        let checkpoints: Vec<u64> = vec![
            0, 1, 50, 99, 100, 101,
            16_998, 16_999, 17_000, 17_001, 17_002,
            100_000, 420_578, 420_579, 420_580, 420_581,
            841_058, 841_059, 841_060, 841_061,
            1_261_538, 1_261_539, 1_261_540, 1_261_541,
            1_682_018, 1_682_019, 1_682_020, 1_682_021,
            2_102_498, 2_102_499, 2_102_500, 2_102_501,
            2_522_978, 2_522_979, 2_522_980, 2_522_981,
            2_943_458, 2_943_459, 2_943_460, 2_943_461,
            3_000_000, 10_000_000, 50_000_000, 100_000_000,
            105_694_899, 105_694_900, 105_694_901,
            105_694_902, 105_694_903,
        ];

        let mut prev_supply = 0u64;
        for &h in &checkpoints {
            let supply = calculate_expected_supply_from_genesis_fallback(h);
            assert!(
                supply >= prev_supply,
                "Tổng cung GIẢM tại h = {}: {} < {}", h, supply, prev_supply
            );
            prev_supply = supply;
        }
    }

    /// Kiểm tra tổng cung tại các mốc chiến lược cụ thể phải bằng hằng số cố định.
    /// Đây là bộ "golden values" - nếu bất kỳ giá trị nào thay đổi, đồng thuận bị phá vỡ.
    #[test]
    fn test_golden_supply_values() {
        // Mốc 0: Genesis
        assert_eq!(calculate_expected_supply_from_genesis_fallback(0), 1_000_000_000_000);

        // Mốc cuối Pha 0
        assert_eq!(calculate_expected_supply_from_genesis_fallback(99), 100_000_000_000_000);

        // Mốc trước Hardfork (khối 16999)
        let supply_pre_fork = calculate_expected_supply_from_genesis_fallback(16_999);
        let expected_pre_fork = 100_000_000_000_000 + (16_999 - 99) as u64 * REWARD_YEAR1_VNT;
        assert_eq!(supply_pre_fork, expected_pre_fork, "Tổng cung trước Hardfork sai");

        // Mốc khối Hardfork (khối 17000) - QUAN TRỌNG NHẤT
        let supply_at_fork = calculate_expected_supply_from_genesis_fallback(17_000);
        let expected_at_fork = expected_pre_fork + REWARD_YEAR1_POST_FORK_VNT + FORK_REMAINDER_VNT;
        assert_eq!(supply_at_fork, expected_at_fork, "Tổng cung tại khối Hardfork sai");

        // Mốc cuối lộ trình: PHẢI BẰNG CHÍNH XÁC 20,000,000 Coin
        assert_eq!(
            calculate_expected_supply_from_genesis_fallback(105_694_901),
            2_000_000_000_000_000,
            "TỔNG CUNG CUỐI CÙNG KHÔNG BẰNG 20 TRIỆU COIN!"
        );

        // Mốc vượt lộ trình: Tổng cung không đổi
        assert_eq!(
            calculate_expected_supply_from_genesis_fallback(105_694_902),
            2_000_000_000_000_000
        );
        assert_eq!(
            calculate_expected_supply_from_genesis_fallback(200_000_000),
            2_000_000_000_000_000
        );
    }

    /// Kiểm tra tính liên tục của phần thưởng tại ranh giới giữa các năm (không nhảy bất thường).
    /// Nếu có bước nhảy quá lớn → node mới sẽ tính sai tổng cung.
    #[test]
    fn test_reward_continuity_at_year_boundaries() {
        // Năm 5 → Năm 6: Phần thưởng phải liên tục (cùng giá trị)
        let y5_last = calculate_block_reward_btc_z(HEIGHT_YEAR5_END);
        let y6_first = calculate_block_reward_btc_z(HEIGHT_YEAR5_END + 1);
        assert_eq!(y5_last, y6_first, "Gãy khúc tại ranh giới Năm 5 → Năm 6");

        // Năm 6 → Năm 7: Phần thưởng phải liên tục
        let y6_last = calculate_block_reward_btc_z(HEIGHT_YEAR6_END);
        let y7_first = calculate_block_reward_btc_z(HEIGHT_YEAR6_END + 1);
        assert_eq!(y6_last, y7_first, "Gãy khúc tại ranh giới Năm 6 → Năm 7");

        // Năm 7 → Năm 8: Phần thưởng phải liên tục
        let y7_last = calculate_block_reward_btc_z(HEIGHT_YEAR7_END);
        let y8_first = calculate_block_reward_btc_z(HEIGHT_YEAR7_END + 1);
        assert_eq!(y7_last, y8_first, "Gãy khúc tại ranh giới Năm 7 → Năm 8");
    }

    /// Kiểm tra Hardfork không ảnh hưởng tổng lượng coin phát hành trong Năm 1.
    /// Tổng phần thưởng Năm 1 trước Fork + sau Fork phải bù trừ chính xác thêm 300,000 Coin.
    #[test]
    fn test_hardfork_total_year1_coins() {
        let supply_end_year1 = calculate_expected_supply_from_genesis_fallback(HEIGHT_YEAR1_END);
        let supply_end_pha0 = calculate_expected_supply_from_genesis_fallback(HEIGHT_PHA0_END);
        let year1_total = supply_end_year1 - supply_end_pha0;

        // Tổng Năm 1 gốc (không Hardfork) = 420,480 khối * 47,564,700 VNT
        let original_year1_total = 420_480u64 * REWARD_YEAR1_VNT;

        // Phần chênh lệch phải bằng CHÍNH XÁC 300,000 Coin = 30,000,000,000,000 VNT
        let extra_coins = year1_total - original_year1_total;
        assert_eq!(
            extra_coins,
            300_000 * 100_000_000,
            "Lượng coin dồn từ đuôi 300 năm vào Năm 1 không bằng 300,000 Coin! (chênh {} VNT)",
            extra_coins as i64 - 300_000 * 100_000_000i64
        );
    }

    /// Kiểm tra khối cuối cùng mới (HEIGHT_YEAR307_END = 105,694,901) 
    /// và khối ngay sau đó phải trả 0 coin.
    #[test]
    fn test_tail_block_and_beyond() {
        let tail_reward = calculate_block_reward_btc_z(HEIGHT_YEAR307_END);
        assert_eq!(tail_reward, TAIL_CORRECTION_VNT, "Khối cuối cùng phải nhận TAIL_CORRECTION_VNT");

        // Khối ngay trước cuối phải > 0 và > TAIL_CORRECTION_VNT (vì nó nằm trên đường cong giảm)
        let pre_tail_reward = calculate_block_reward_btc_z(HEIGHT_YEAR307_END - 1);
        assert!(pre_tail_reward > 0, "Khối áp cuối phải > 0");

        // Khối ngay sau cuối phải = 0
        assert_eq!(calculate_block_reward_btc_z(HEIGHT_YEAR307_END + 1), 0);
        assert_eq!(calculate_block_reward_btc_z(HEIGHT_YEAR307_END + 100), 0);
    }

    /// Stress test: Kiểm tra cộng dồn liên tục qua TOÀN BỘ vùng giảm tuyến tính Năm 6 và Năm 7.
    /// Đây là vùng floor division dễ sai nhất khi tích phân.
    #[test]
    fn test_supply_through_linear_decay_zones() {
        // Kiểm tra từng khối qua Năm 6 (lấy mẫu mỗi 500 khối + toàn bộ đầu/cuối)
        let anchor_h = HEIGHT_YEAR5_END;
        let mut manual_sum = calculate_expected_supply_from_genesis_fallback(anchor_h);
        let mut prev_h = anchor_h;

        let mut test_points: Vec<u64> = (HEIGHT_YEAR5_END + 1..=HEIGHT_YEAR5_END + 100).collect();
        test_points.extend((HEIGHT_YEAR5_END + 100..=HEIGHT_YEAR6_END - 100).step_by(500));
        test_points.extend(HEIGHT_YEAR6_END - 100..=HEIGHT_YEAR6_END);

        for h in test_points {
            for i in (prev_h + 1)..=h {
                manual_sum += calculate_block_reward_btc_z(i);
            }
            prev_h = h;
            assert_eq!(
                calculate_expected_supply_from_genesis_fallback(h),
                manual_sum,
                "[NĂM 6] Lệch tổng cung tại h = {}", h
            );
        }

        // Kiểm tra từng khối qua Năm 7
        let mut test_points_y7: Vec<u64> = (HEIGHT_YEAR6_END + 1..=HEIGHT_YEAR6_END + 100).collect();
        test_points_y7.extend((HEIGHT_YEAR6_END + 100..=HEIGHT_YEAR7_END - 100).step_by(500));
        test_points_y7.extend(HEIGHT_YEAR7_END - 100..=HEIGHT_YEAR7_END);

        for h in test_points_y7 {
            for i in (prev_h + 1)..=h {
                manual_sum += calculate_block_reward_btc_z(i);
            }
            prev_h = h;
            assert_eq!(
                calculate_expected_supply_from_genesis_fallback(h),
                manual_sum,
                "[NĂM 7] Lệch tổng cung tại h = {}", h
            );
        }
    }

    /// Kiểm tra cộng dồn qua vùng Year 8-307 (kỷ nguyên dài hạn), lấy mẫu mỗi 500,000 khối
    /// và kiểm tra kỹ 200 khối cuối cùng (nơi tail correction được áp dụng).
    #[test]
    fn test_supply_through_long_epoch_and_tail() {
        let anchor_h = HEIGHT_YEAR7_END;
        let mut manual_sum = calculate_expected_supply_from_genesis_fallback(anchor_h);
        let mut prev_h = anchor_h;

        // Lấy mẫu thưa qua kỷ nguyên dài hạn
        let mut test_points: Vec<u64> = Vec::new();
        test_points.extend((HEIGHT_YEAR7_END + 1..=HEIGHT_YEAR7_END + 50).collect::<Vec<_>>());
        test_points.extend((HEIGHT_YEAR7_END + 50..HEIGHT_YEAR307_END - 200).step_by(500_000));
        test_points.extend(HEIGHT_YEAR307_END - 200..=HEIGHT_YEAR307_END + 5);

        for h in test_points {
            for i in (prev_h + 1)..=h {
                manual_sum += calculate_block_reward_btc_z(i);
            }
            prev_h = h;
            assert_eq!(
                calculate_expected_supply_from_genesis_fallback(h),
                manual_sum,
                "[EPOCH 8-307] Lệch tổng cung tại h = {}", h
            );
        }

        // Xác nhận tổng cung cuối cùng
        assert_eq!(manual_sum, 2_000_000_000_000_000, "Tổng cung cuối cùng KHÔNG BẰNG 20 TRIỆU COIN!");
    }
}

