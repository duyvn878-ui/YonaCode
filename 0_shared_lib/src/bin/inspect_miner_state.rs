use btc_genz_scl::state_manager::{
    self, StateManager, CF_JMT, CF_ACC, CF_META, CF_RECEIPTS, CF_BLOCKS,
    CF_BLOCK_BODIES, CF_BLOCK_TXS, CF_TOUCHED_ACCS, CF_HEADERS,
    CF_COINBASE, CF_SMT_NODES, CF_ACC_HISTORY, CF_MEMPOOL,
    CF_TX_INDEX, CF_ACC_SYNC_STAGING, CF_REORG_BACKUP
};
use jmt::KeyHash;
use jmt::storage::TreeReader;
use std::sync::Arc;
use std::sync::atomic::AtomicU64;

fn main() {
    // Nhận đường dẫn cơ sở dữ liệu qua đối số, hoặc sử dụng mặc định "./data/scl"
    let args: Vec<String> = std::env::args().collect();
    let db_path = if args.len() > 1 {
        args[1].clone()
    } else {
        "./data/scl".to_string()
    };
    println!("🔍 Đang mở DB ở chế độ READ-ONLY tại: {}", db_path);
    
    let mut opts = rocksdb::Options::default();
    opts.create_if_missing(false);
    opts.create_missing_column_families(false);
    
    let cfs = vec![
        "default", CF_JMT, CF_ACC, CF_META, CF_RECEIPTS, CF_BLOCKS, 
        CF_BLOCK_BODIES, CF_BLOCK_TXS, CF_TOUCHED_ACCS, CF_HEADERS, 
        CF_COINBASE, CF_SMT_NODES, CF_ACC_HISTORY, CF_MEMPOOL, 
        CF_TX_INDEX, CF_ACC_SYNC_STAGING, CF_REORG_BACKUP
    ];
    
    let db = rocksdb::DB::open_cf_for_read_only(&opts, &db_path, cfs, false)
        .expect("❌ Failed to open read-only DB!");
    
    let db_arc = Arc::new(db);
    
    let meta_cf = db_arc.cf_handle(CF_META).expect("Missing META CF");
    let current_version = match db_arc.get_cf(meta_cf, b"jmt_v").unwrap_or(None) {
        Some(v) => {
            if v.len() == 8 {
                u64::from_le_bytes(v.try_into().unwrap())
            } else { 0 }
        },
        None => 0,
    };
    
    let mgr = Arc::new(StateManager {
        db: db_arc,
        current_version: AtomicU64::new(current_version),
        actual_total_supply: AtomicU64::new(0),
    });
    
    let addr_bytes = vec![0u8; 32];
    let key_hash = KeyHash::with::<blake3::Hasher>(&addr_bytes);

    let version = current_version;
    println!("\n--- KIỂM TRA PHIÊN BẢN HIGHT {} ---", version);

    // 1. Đọc qua get_value_option mới sửa
    let val_opt = TreeReader::get_value_option(mgr.as_ref(), version, key_hash).expect("Failed to get value option");
    if let Some(val) = val_opt {
        println!("✅ get_value_option trả về giá trị (hex): {}", hex::encode(&val));
        let val_hash = blake3::hash(&val);
        println!("   -> Blake3 hash của giá trị này: {}", hex::encode(val_hash.as_bytes()));
    } else {
        println!("❌ get_value_option không tìm thấy giá trị!");
    }

    // 2. Đọc JMT proof trực tiếp từ JMT Tree
    let jmt_tree = jmt::JellyfishMerkleTree::<'_, state_manager::StateManager, blake3::Hasher>::new(mgr.as_ref());
    let proof_res = jmt_tree.get_with_proof(key_hash, version);
    match proof_res {
        Ok((val_opt, proof)) => {
            println!("✅ get_with_proof thành công!");
            if let Some(val) = val_opt {
                let val_hash = blake3::hash(&val);
                println!("   -> Giá trị trả về bởi get_with_proof: {} (hash: {})", hex::encode(&val), hex::encode(val_hash.as_bytes()));
            } else {
                println!("   -> get_with_proof trả về None!");
            }
            println!("   -> Proof leaf: {:?}", proof.leaf());
            
            // Tự kiểm tra xem giá trị get_value_option trả về có khớp với proof leaf value hash không!
            if let Some(leaf) = proof.leaf() {
                println!("🎉 Đã lấy được proof leaf: {:?}", leaf);
            }
        }
        Err(e) => {
            println!("❌ get_with_proof thất bại: {:?}", e);
        }
    }
}
