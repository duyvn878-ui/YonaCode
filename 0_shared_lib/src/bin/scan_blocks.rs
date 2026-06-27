// Tên file: scan_blocks.rs
// Tính năng: Quét RocksDB để thống kê số lượng tài khoản và tìm các khối nặng nhất, nhiều giao dịch nhất.
// Cơ chế vận hành: Mở RocksDB ở chế độ Read-Only, lặp qua CF_ACC để đếm tài khoản (lọc KeyHash) và lặp qua CF_BLOCKS để phân tích Block Body / Block Header.

use btc_genz_scl::state_manager::{
    CF_ACC, CF_BLOCKS, CF_BLOCK_BODIES, CF_HEADERS, CF_KEYHASH_TO_ADDR
};
use btc_genz_scl::proto::block::{BlockBody, BlockHeader};
use prost::Message;
use jmt::KeyHash;
use std::sync::Arc;
use std::time::Instant;
use std::collections::HashSet;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    // Cho phép người dùng tùy chọn truyền đường dẫn DB, nếu không sẽ dùng đường dẫn mặc định
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
    
    let required_cfs = [
        CF_ACC, CF_BLOCKS, CF_BLOCK_BODIES, CF_HEADERS, CF_KEYHASH_TO_ADDR
    ];
    
    let mut cfs = vec!["default"];
    for cf in &required_cfs {
        if existing_cfs.iter().any(|c| c == *cf) {
            cfs.push(cf);
        }
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
    let blocks_cf = db.cf_handle(CF_BLOCKS).expect("Không tìm thấy CF_BLOCKS trong DB!");
    let bodies_cf = db.cf_handle(CF_BLOCK_BODIES).expect("Không tìm thấy CF_BLOCK_BODIES trong DB!");
    let headers_cf = db.cf_handle(CF_HEADERS).expect("Không tìm thấy CF_HEADERS trong DB!");
    let kh_to_addr_cf = db.cf_handle(CF_KEYHASH_TO_ADDR);
    
    // --- BƯỚC 1: ĐẾM SỐ LƯỢNG TÀI KHOẢN (ACCOUNTS) ---
    // Tại sao: Thuật toán này quét qua CF_ACC phẳng, đối chiếu với CF_KEYHASH_TO_ADDR để phân biệt 
    // giữa Address ví 32-byte thực và KeyHash ảo của cây JMT, đảm bảo số liệu thống kê tài khoản là chuẩn xác.
    println!("⚡ Bắt đầu đếm số lượng tài khoản (Accounts)...");
    let start_acc_time = Instant::now();
    let mut address_count = 0;
    let mut seen_addresses = HashSet::new();
    
    let acc_iter = db.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
    for item in acc_iter {
        if let Ok((key, _val)) = item {
            if key.len() == 32 {
                let mut addr = [0u8; 32];
                addr.copy_from_slice(&key);
                
                let (is_address, real_addr) = if let Some(kh_cf) = kh_to_addr_cf {
                    if let Some(addr_bytes) = db.get_cf(kh_cf, &key).unwrap_or(None) {
                        let mut r_addr = [0u8; 32];
                        r_addr.copy_from_slice(&addr_bytes);
                        (true, r_addr)
                    } else {
                        (true, addr)
                    }
                } else {
                    let kh = KeyHash::with::<blake3::Hasher>(&addr);
                    let kh_exists = db.get_cf(acc_cf, kh.0).unwrap_or(None).is_some();
                    (kh_exists, addr)
                };
                
                if !is_address {
                    continue;
                }
                
                if seen_addresses.insert(real_addr) {
                    address_count += 1;
                }
            }
        }
    }
    let acc_scan_duration = start_acc_time.elapsed();
    
    println!("👥 Tổng số ví thực tế (Address): {} (Quét trong {:?})", address_count, acc_scan_duration);
    
    // --- BƯỚC 2: QUÉT BLOCKCHAIN ---
    // Tại sao: Duyệt tuần tự qua các Height từ 0 trở lên để trích xuất Block Body và Block Header từ DB.
    // Điều này giúp tìm ra các khối có kích thước nặng nhất (size in bytes) và khối chứa nhiều giao dịch nhất một cách chính xác.
    println!("\n⚡ Bắt đầu quét toàn bộ Block-Chain...");
    let start_block_time = Instant::now();
    
    let mut highest_height: u64 = 0;
    loop {
        let h_bytes = highest_height.to_le_bytes();
        match db.get_cf(blocks_cf, h_bytes).unwrap_or(None) {
            Some(_) => {
                highest_height += 1;
            },
            None => {
                break;
            }
        }
    }
    highest_height = highest_height.saturating_sub(1);
    println!("📈 Chiều cao khối lớn nhất phát hiện: #{}", highest_height);
    
    #[derive(Clone)]
    struct BlockStat {
        height: u64,
        hash_hex: String,
        tx_count: usize,
        body_size: usize,
        absolute_weight_hex: String,
    }
    
    let mut block_stats = Vec::new();
    let mut total_tx_count = 0;
    let mut total_body_size = 0;
    
    for h in 0..=highest_height {
        let h_bytes = h.to_le_bytes();
        if let Some(hash_bytes) = db.get_cf(blocks_cf, h_bytes).unwrap_or(None) {
            let hash_arr: [u8; 32] = match hash_bytes.clone().try_into() {
                Ok(a) => a,
                Err(_) => continue,
            };
            let hash_hex = hex::encode(hash_arr);
            
            // Đọc và decode BlockBody từ DB để lấy số lượng giao dịch và kích thước
            if let Some(body_raw) = db.get_cf(bodies_cf, &hash_arr).unwrap_or(None) {
                let size = body_raw.len();
                let tx_count = if let Ok(body) = BlockBody::decode(body_raw.as_slice()) {
                    body.transactions.len()
                } else {
                    0
                };
                
                // Đọc và decode BlockHeader để lấy Absolute Weight
                let absolute_weight_hex = if let Some(header_raw) = db.get_cf(headers_cf, &hash_arr).unwrap_or(None) {
                    if let Ok(header) = BlockHeader::decode(header_raw.as_slice()) {
                        hex::encode(&header.absolute_weight)
                    } else {
                        "N/A".to_string()
                    }
                } else {
                    "N/A".to_string()
                };
                
                total_tx_count += tx_count;
                total_body_size += size;
                
                block_stats.push(BlockStat {
                    height: h,
                    hash_hex: hash_hex.clone(),
                    tx_count,
                    body_size: size,
                    absolute_weight_hex,
                });
            }
        }
    }
    
    let block_scan_duration = start_block_time.elapsed();
    
    // --- BƯỚC 3: IN BÁO CÁO THỐNG KÊ CHI TIẾT ---
    println!("\n==================================================");
    println!("📊 BÁO CÁO THỐNG KÊ SỔ CÁI BLOCKCHAIN (YONACODE)");
    println!("==================================================");
    println!("⏱️ Thời gian quét block : {:?}", block_scan_duration);
    println!("👥 Tổng số ví thực tế    : {}", address_count);
    println!("🧱 Tổng số khối (Blocks) : {}", highest_height + 1);
    println!("🚀 Tổng số giao dịch    : {}", total_tx_count);
    println!("📦 Tổng dung lượng body  : {:.2} MB", total_body_size as f64 / (1024.0 * 1024.0));
    println!("--------------------------------------------------");
    
    if block_stats.is_empty() {
        println!("📭 Không tìm thấy khối dữ liệu nào.");
        return;
    }
    
    // Tìm các khối nặng nhất (Dung lượng Block Body lớn nhất)
    let mut stats_by_size = block_stats.clone();
    stats_by_size.sort_by(|a, b| b.body_size.cmp(&a.body_size));
    
    println!("📦 TOP 5 KHỐI NẶNG NHẤT (DUNG LƯỢNG LỚN NHẤT):");
    for (i, stat) in stats_by_size.iter().take(5).enumerate() {
        println!(
            "  #{}: Khối #{:6} | Dung lượng: {:10} bytes | TXs: {:5} | Hash: 0x{}...",
            i + 1,
            stat.height,
            stat.body_size,
            stat.tx_count,
            &stat.hash_hex[..16]
        );
    }
    
    // Tìm các khối nhiều giao dịch nhất
    let mut stats_by_txs = block_stats.clone();
    stats_by_txs.sort_by(|a, b| b.tx_count.cmp(&a.tx_count));
    
    println!("\n🚀 TOP 5 KHỐI CÓ NHIỀU GIAO DỊCH NHẤT:");
    for (i, stat) in stats_by_txs.iter().take(5).enumerate() {
        println!(
            "  #{}: Khối #{:6} | Số giao dịch: {:5} | Dung lượng: {:10} bytes | Hash: 0x{}...",
            i + 1,
            stat.height,
            stat.tx_count,
            stat.body_size,
            &stat.hash_hex[..16]
        );
    }
    
    // Tìm các khối có PoW Absolute Weight lớn nhất (chuỗi tích lũy nặng nhất)
    let mut stats_by_weight = block_stats.clone();
    stats_by_weight.sort_by(|a, b| b.absolute_weight_hex.cmp(&a.absolute_weight_hex));
    
    println!("\n🧱 TOP 5 KHỐI CÓ ABSOLUTE WEIGHT LỚN NHẤT:");
    for (i, stat) in stats_by_weight.iter().take(5).enumerate() {
        println!(
            "  #{}: Khối #{:6} | Weight: 0x{}... | TXs: {:5} | Size: {:10} bytes",
            i + 1,
            stat.height,
            if stat.absolute_weight_hex.len() > 16 { &stat.absolute_weight_hex[..16] } else { &stat.absolute_weight_hex },
            stat.tx_count,
            stat.body_size
        );
    }
    println!("==================================================");
}
