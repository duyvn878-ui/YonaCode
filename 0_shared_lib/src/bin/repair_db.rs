use rocksdb::{DB, Options, WriteBatch};
fn main() {
    // Cho phép truyền đường dẫn cơ sở dữ liệu qua đối số, fallback về "./data/scl" cho tính tương thích đa nền tảng
    let args: Vec<String> = std::env::args().collect();
    let path = if args.len() > 1 {
        &args[1]
    } else {
        "./data/scl"
    };
    let mut opts = Options::default();
    let cfs = vec!["default", "jmt_nodes", "accounts", "meta", "receipts", "blocks", "block_bodies", "block_txs", "touched_accs", "headers", "coinbase", "smt_nodes", "acc_history", "mempool", "tx_index", "accounts_staging", "maturity_queue", "reorg_backup"];
    
    match DB::open_cf(&opts, path, cfs) {
        Ok(db) => {
            let history_cf = db.cf_handle("acc_history").unwrap();
            let acc_cf = db.cf_handle("accounts").unwrap();
            let mut batch = WriteBatch::default();
            
            println!("🔍 Đang quét lịch sử để khôi phục bảng Accounts...");
            let iter = db.iterator_cf(history_cf, rocksdb::IteratorMode::Start);
            let mut latest_states = std::collections::HashMap::new();
            
            for item in iter {
                if let Ok((key, value)) = item {
                    if key.len() == 40 {
                        let addr = &key[0..32];
                        let version = u64::from_be_bytes(key[32..40].try_into().unwrap());
                        
                        let entry = latest_states.entry(addr.to_vec()).or_insert((0u64, Vec::new()));
                        if version >= entry.0 {
                            *entry = (version, value.to_vec());
                        }
                    }
                }
            }
            
            let count = latest_states.len();
            for (addr, (_, state)) in latest_states {
                batch.put_cf(acc_cf, addr, state);
            }
            
            db.write(batch).unwrap();
            println!("✅ Đã khôi phục {} tài khoản vào bảng Accounts.", count);
        },
        Err(e) => println!("Error: {}", e),
    }
}
