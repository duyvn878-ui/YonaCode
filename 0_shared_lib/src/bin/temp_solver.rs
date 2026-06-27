use btc_genz_scl::reward_logic::{
    HEIGHT_YEAR5_END, HEIGHT_YEAR6_END, HEIGHT_YEAR7_END, HEIGHT_YEAR307_END
};

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

fn calculate_y6_sum(r5: u64, e: u64) -> u64 {
    let s6_start_reward = r5 as u128;
    let s6_end_reward = e as u128;
    let s6_m = (HEIGHT_YEAR6_END - (HEIGHT_YEAR5_END + 1)) as u128;
    let s6_r_delta = s6_start_reward - s6_end_reward;
    
    let s6_n_total = (HEIGHT_YEAR6_END - HEIGHT_YEAR5_END) as u128;
    let s6_sum_reduction = floor_sum(s6_n_total, s6_m, s6_r_delta, 0);
    (s6_n_total * s6_start_reward - s6_sum_reduction) as u64
}

fn calculate_y7_sum(e: u64) -> u64 {
    let s7_start_reward = e as u128;
    let s7_end_reward = 9_513_000u128;
    let s7_m = (HEIGHT_YEAR7_END - (HEIGHT_YEAR6_END + 1)) as u128;
    let s7_r_delta = s7_start_reward - s7_end_reward;
    let s7_n_total = (HEIGHT_YEAR7_END - HEIGHT_YEAR6_END) as u128;
    let s7_sum_reduction = floor_sum(s7_n_total, s7_m, s7_r_delta, 0);
    (s7_n_total * s7_start_reward - s7_sum_reduction) as u64
}

fn main() {
    let target_total: u64 = 2_000_000_000_000_000;
    let pha0_sum: u64 = 100_000_000_000_000; // 100 * 10_000 Coin

    // Year 8-307 sum is constant
    let s8_start_reward = 9_513_000u128;
    let s8_end_reward = 0u128;
    let s8_m = (HEIGHT_YEAR307_END - (HEIGHT_YEAR7_END + 1)) as u128;
    let s8_r_delta = s8_start_reward - s8_end_reward;
    let s8_n_total = (HEIGHT_YEAR307_END - HEIGHT_YEAR7_END) as u128;
    let s8_sum_reduction = floor_sum(s8_n_total, s8_m, s8_r_delta, 0);
    let y8_307_sum = (s8_n_total * s8_start_reward - s8_sum_reduction) as u64;

    let orig_r5 = 743_198_200u64;
    let orig_e = 347_222_200u64;
    
    println!("Starting 2D search...");
    for offset_r5 in -10_000i64..10_000i64 {
        let r5 = (orig_r5 as i64 + offset_r5) as u64;
        let y6_sum = calculate_y6_sum(r5, orig_e);
        
        // We can also vary E to find a remainder of 0
        for offset_e in -10_000i64..10_000i64 {
            let e = (orig_e as i64 + offset_e) as u64;
            let y6_sum = calculate_y6_sum(r5, e);
            let y7_sum = calculate_y7_sum(e);
            
            let fixed_parts = pha0_sum + y6_sum + y7_sum + y8_307_sum;
            if target_total < fixed_parts {
                continue;
            }
            let remaining = target_total - fixed_parts;
            if remaining % 420_480 == 0 {
                let const_sum = remaining / 420_480;
                let r1 = 47_564_700u64;
                let r3 = 505_374_800u64;
                let r4 = 624_286_500u64;
                
                if const_sum > r1 + r3 + r4 + r5 {
                    let r2 = const_sum - r1 - r3 - r4 - r5;
                    let r2_coin = r2 as f64 / 100_000_000.0;
                    println!(
                        "FOUND: r5_offset={}, e_offset={}, R5={}, E={}, R2={} ({} Coin)",
                        offset_r5, offset_e, r5, e, r2, r2_coin
                    );
                    println!("pub const REWARD_YEAR1_VNT: u64 = {};", r1);
                    println!("pub const REWARD_YEAR2_VNT: u64 = {};", r2);
                    println!("pub const REWARD_YEAR3_VNT: u64 = {};", r3);
                    println!("pub const REWARD_YEAR4_VNT: u64 = {};", r4);
                    println!("pub const REWARD_YEAR5_VNT: u64 = {};", r5);
                    println!("Year 6 end reward: {} (offset {})", e, offset_e);
                    
                    let y1_sum = r1 * 420_480;
                    let y2_sum = r2 * 420_480;
                    let y3_sum = r3 * 420_480;
                    let y4_sum = r4 * 420_480;
                    let y5_sum = r5 * 420_480;
                    let calculated_total = pha0_sum + y1_sum + y2_sum + y3_sum + y4_sum + y5_sum + y6_sum + y7_sum + y8_307_sum;
                    assert_eq!(calculated_total, target_total);
                    return;
                }
            }
        }
    }
    println!("No solution found within search range.");
}
