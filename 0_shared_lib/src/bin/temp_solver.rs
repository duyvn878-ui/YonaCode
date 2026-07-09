use btc_genz_scl::reward_logic::*;

fn main() {
    // CHỐT MỐC HARDFORK TẠI KHỐI 17.000
    let fork_height: u64 = 17_000; 
    
    let extra_coins_vnt: u64 = 300_000 * 100_000_000; // 300,000 Coin
    let remaining_blocks = HEIGHT_YEAR1_END - fork_height + 1; // Số khối còn lại của Năm 1

    let extra_per_block = extra_coins_vnt / remaining_blocks;
    let remainder = extra_coins_vnt % remaining_blocks;

    let reward_year1_post_fork = REWARD_YEAR1_VNT + extra_per_block;

    println!("=== COPY ĐOẠN NÀY VÀO reward_logic.rs ===");
    println!("pub const HARDFORK_HEIGHT: u64 = {};", fork_height);
    println!("pub const REWARD_YEAR1_POST_FORK_VNT: u64 = {};", reward_year1_post_fork);
    println!("pub const FORK_REMAINDER_VNT: u64 = {};", remainder);

    // Cắt giảm từ đuôi Năm 307
    let target_reduction = extra_coins_vnt;
    let s8_start_reward = 9_513_000u128;
    let s8_m = (134_500_269 - (HEIGHT_YEAR7_END + 1)) as u128; // Giữ nguyên độ dốc phát thải gốc
    let s8_r_delta = s8_start_reward; 

    // Bắt đầu tích lũy phần cắt giảm
    // Khối cuối cùng đóng góp TAIL_CORRECTION_VNT gốc
    let mut current_reduction = TAIL_CORRECTION_VNT;
    let mut new_end_height = 134_500_269;
    let mut tail_correction = 0;

    // Duyệt ngược các khối từ HEIGHT_YEAR307_END - 1 về đầu Year 8
    for h in (HEIGHT_YEAR7_END + 1..134_500_269).rev() {
        let h_delta = (h - (HEIGHT_YEAR7_END + 1)) as u128;
        let reward_at_h = s8_start_reward - ((s8_r_delta * h_delta) / s8_m);
        
        current_reduction += reward_at_h as u64;
        new_end_height = h;

        if current_reduction >= target_reduction {
            tail_correction = current_reduction - target_reduction;
            break;
        }
    }

    println!("pub const HEIGHT_YEAR307_END_NEW: u64 = {};", new_end_height);
    println!("pub const TAIL_CORRECTION_VNT_NEW: u64 = {};", tail_correction);
}
