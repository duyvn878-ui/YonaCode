use btc_genz_scl::state_manager::{AccountState, CF_ACC, CF_KEYHASH_TO_ADDR};
use borsh::BorshDeserialize;
use jmt::KeyHash;
use std::sync::Arc;
use std::time::Instant;
use std::collections::HashSet;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let db_path = if args.len() > 1 {
        &args[1]
    } else {
        "./data/scl"
    };
    println!("🔍 [Rust Tool] Bắt đầu mở RocksDB tại: {}", db_path);
    
    let mut opts = rocksdb::Options::default();
    opts.create_if_missing(false);
    opts.create_missing_column_families(false);
    
    // Kiểm tra danh sách Column Family thực tế trong DB để tránh lỗi khi mở DB cũ/mới khác nhau.
    let existing_cfs = rocksdb::DB::list_cf(&opts, db_path)
        .unwrap_or_else(|_| vec!["default".to_string()]);
    
    let mut cfs = vec!["default"];
    if existing_cfs.iter().any(|c| c == CF_ACC) {
        cfs.push(CF_ACC);
    }
    if existing_cfs.iter().any(|c| c == CF_KEYHASH_TO_ADDR) {
        cfs.push(CF_KEYHASH_TO_ADDR);
    }
    
    let db = match rocksdb::DB::open_cf_for_read_only(&opts, db_path, cfs, false) {
        Ok(d) => Arc::new(d),
        Err(e) => {
            eprintln!("❌ [LỖI] Không thể mở DB ở chế độ read-only: {:?}", e);
            eprintln!("Hãy chắc chắn rằng đường dẫn DB chính xác và thư mục tồn tại.");
            return;
        }
    };
    
    let acc_cf = db.cf_handle(CF_ACC).expect("Không tìm thấy CF_ACC trong DB!");
    let kh_to_addr_cf = db.cf_handle(CF_KEYHASH_TO_ADDR);
    
    // 2. Bắt đầu duyệt qua toàn bộ Key-Value trong CF_ACC
    println!("⚡ Đang quét toàn bộ dữ liệu trong column family '{}'...", CF_ACC);
    let start_time = Instant::now();
    
    let mut total_keys = 0;
    let mut address_count = 0;
    let mut keyhash_count = 0;
    let mut total_balance: u64 = 0;
    
    // Đếm số lượng KeyHash cache trong CF_KEYHASH_TO_ADDR nếu có (định dạng DB mới)
    if let Some(kh_cf) = kh_to_addr_cf {
        let kh_iter = db.iterator_cf(kh_cf, rocksdb::IteratorMode::Start);
        for item in kh_iter {
            if item.is_ok() {
                keyhash_count += 1;
            }
        }
    }
    
    let iter = db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
    
    // Dùng vector để chứa danh sách tài khoản hoạt động để sắp xếp thống kê
    let mut active_accounts: Vec<(String, u64, u64)> = Vec::new(); // (Địa chỉ hex, số dư btc_z, nonce)
    let mut seen_addresses = HashSet::new();
    
    for item in iter {
        if let Ok((key, val)) = item {
            total_keys += 1;
            
            // Mỗi key trong CF_ACC có độ dài 32 bytes (có thể là Address ví hoặc KeyHash của JMT)
            if key.len() == 32 {
                let mut addr = [0u8; 32];
                addr.copy_from_slice(&key);
                
                // Thuật toán kiểm tra chốt chặn bảo mật phân biệt Address và KeyHash:
                let (is_address, real_addr) = if let Some(kh_cf) = kh_to_addr_cf {
                    // Định dạng DB mới:
                    // Nếu key tồn tại trong CF_KEYHASH_TO_ADDR làm key, thì key thực chất là KeyHash.
                    // Ta truy xuất Address gốc từ value của nó.
                    if let Some(addr_bytes) = db.get_cf(kh_cf, &key).unwrap_or(None) {
                        let mut r_addr = [0u8; 32];
                        r_addr.copy_from_slice(&addr_bytes);
                        (true, r_addr)
                    } else {
                        // Nếu không tìm thấy trong CF_KEYHASH_TO_ADDR, thì key chính là Address thực tế
                        (true, addr)
                    }
                } else {
                    // Định dạng DB cũ: CF_ACC lưu cả Address lẫn KeyHash.
                    // Nếu Blake3 hash của key (KeyHash) tồn tại trong CF_ACC, thì key chính là Address thực.
                    let kh = KeyHash::with::<blake3::Hasher>(&addr);
                    let kh_exists = db.get_cf(acc_cf, kh.0).unwrap_or(None).is_some();
                    if !kh_exists {
                        keyhash_count += 1;
                    }
                    (kh_exists, addr)
                };
                
                if !is_address {
                    continue;
                }
                
                // Tránh đếm trùng địa chỉ
                if !seen_addresses.insert(real_addr) {
                    continue;
                }
                
                // Đây chính xác là địa chỉ ví (Address) hợp lệ
                address_count += 1;
                
                // Deserialize dữ liệu AccountState bằng Borsh (nhanh và an toàn)
                if let Ok(state) = AccountState::try_from_slice(&val) {
                    total_balance += state.btc_z;
                    
                    // Chỉ lưu các tài khoản có giao dịch (nonce > 0) hoặc số dư > 0 để tối ưu bộ nhớ in báo cáo
                    if state.nonce > 0 || state.btc_z > 0 {
                        active_accounts.push((hex::encode(real_addr), state.btc_z, state.nonce));
                    }
                }
            }
        }
    }
    
    let duration = start_time.elapsed();
    
    // 3. In báo cáo thống kê chi tiết
    println!("\n==================================================");
    println!("📊 BÁO CÁO THỐNG KÊ TÀI KHOẢN (QUÉT TRỰC TIẾP BẰNG RUST)");
    println!("==================================================");
    println!("⏱️ Thời gian quét: {:?}", duration);
    println!("🔑 Tổng số Keys duyệt qua: {}", total_keys);
    println!("👥 Tổng số ví thực tế (Address): {}", address_count);
    println!("🏷️ Tổng số JMT KeyHash cache: {}", keyhash_count);
    println!("💰 Tổng số dư tất cả ví: {:.8} BTC_Z", total_balance as f64 / 1e8);
    println!("📈 Số lượng ví hoạt động (Số dư > 0 hoặc Nonce > 0): {}", active_accounts.len());
    println!("--------------------------------------------------");
    
    if active_accounts.is_empty() {
        println!("📭 Không tìm thấy tài khoản hoạt động nào.");
        return;
    }
    
    // Sắp xếp các tài khoản theo Nonce giảm dần để tìm Spammer lớn nhất
    active_accounts.sort_by(|a, b| b.2.cmp(&a.2));
    
    println!("🚀 TOP 10 TÀI KHOẢN CÓ SỐ GIAO DỊCH (NONCE) CAO NHẤT:");
    for (i, (addr, balance, nonce)) in active_accounts.iter().take(10).enumerate() {
        println!(
            "  #{}: 0x{} | Nonce: {:6} | Số dư: {:14.8} BTC_Z",
            i + 1,
            addr,
            nonce,
            *balance as f64 / 1e8
        );
    }
    
    // Sắp xếp các tài khoản theo Số dư giảm dần để tìm cá mập (Whales)
    active_accounts.sort_by(|a, b| b.1.cmp(&a.1));
    
    println!("\n🐋 TOP 10 TÀI KHOẢN CÓ SỐ DƯ LỚN NHẤT:");
    for (i, (addr, balance, nonce)) in active_accounts.iter().take(10).enumerate() {
        println!(
            "  #{}: 0x{} | Nonce: {:6} | Số dư: {:14.8} BTC_Z",
            i + 1,
            addr,
            nonce,
            *balance as f64 / 1e8
        );
    }
    println!("==================================================");
}
