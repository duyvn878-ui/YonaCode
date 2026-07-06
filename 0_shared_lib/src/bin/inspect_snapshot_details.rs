use btc_genz_scl::state_manager::{
    StateManager, CF_JMT, CF_ACC, CF_META, CF_RECEIPTS, CF_BLOCKS,
    CF_BLOCK_BODIES, CF_BLOCK_TXS, CF_TOUCHED_ACCS, CF_HEADERS,
    CF_COINBASE, CF_SMT_NODES, CF_ACC_HISTORY, CF_MEMPOOL,
    CF_TX_INDEX, CF_ACC_SYNC_STAGING, CF_REORG_BACKUP, AccountState
};
use borsh::BorshDeserialize;
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
    println!("🔍 Đang kiểm tra DB tại: {}", db_path);
    
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
        .expect("❌ Không thể mở DB read-only!");
    
    let db_arc = Arc::new(db);
    
    let meta_cf = db_arc.cf_handle(CF_META).expect("Missing META CF");
    let current_version = match db_arc.get_cf(meta_cf, b"jmt_v").unwrap_or(None) {
        Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
        None => 0,
    };
    
    let finalized_h = match db_arc.get_cf(meta_cf, b"finalized_h").unwrap_or(None) {
        Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
        None => 0,
    };

    let lowest_full_height = match db_arc.get_cf(meta_cf, b"lowest_full_height").unwrap_or(None) {
        Some(v) => u64::from_le_bytes(v.try_into().unwrap_or([0u8; 8])),
        None => 0,
    };

    println!("📊 Current Version (jmt_v): #{}", current_version);
    println!("📊 Finalized Height:         #{}", finalized_h);
    println!("📊 Lowest Full Height:       #{}", lowest_full_height);

    let mgr = Arc::new(StateManager {
        db: db_arc.clone(),
        current_version: AtomicU64::new(current_version),
        actual_total_supply: AtomicU64::new(0),
    });

    let acc_cf = db_arc.cf_handle(CF_ACC).expect("Missing ACC CF");
    let iter = db_arc.iterator_cf(acc_cf, rocksdb::IteratorMode::Start);
    let mut total_keys = 0;
    let mut address_keys = 0;
    let mut keyhash_keys = 0;

    let mut sample_addresses = Vec::new();

    for item in iter {
        if let Ok((key, _)) = item {
            total_keys += 1;
            if key.len() == 32 {
                let mut addr = [0u8; 32];
                addr.copy_from_slice(&key);
                // Check if it's an address or a keyhash
                let kh = KeyHash::with::<blake3::Hasher>(&addr);
                let is_keyhash = db_arc.get_cf(acc_cf, kh.0).unwrap_or(None).is_none();
                if is_keyhash {
                    keyhash_keys += 1;
                } else {
                    address_keys += 1;
                    if sample_addresses.len() < 5 {
                        sample_addresses.push(addr);
                    }
                }
            }
        }
    }

    println!("🔑 Tổng số key trong CF_ACC: {}", total_keys);
    println!("   -> Address keys: {}", address_keys);
    println!("   -> KeyHash keys: {}", keyhash_keys);

    println!("\n--- QUÉT TOÀN BỘ PHIÊN BẢN JMT TRONG CF_JMT ---");
    let jmt_cf = db_arc.cf_handle(CF_JMT).expect("Missing JMT CF");
    let jmt_iter = db_arc.iterator_cf(jmt_cf, rocksdb::IteratorMode::Start);
    let mut version_counts = std::collections::BTreeMap::new();
    let mut total_jmt_nodes = 0;

    for item in jmt_iter {
        if let Ok((key, _)) = item {
            total_jmt_nodes += 1;
            if let Ok(node_key) = bincode::deserialize::<jmt::storage::NodeKey>(&key) {
                let v = node_key.version();
                *version_counts.entry(v).or_insert(0) += 1;
            }
        }
    }

    println!("Total JMT nodes in DB: {}", total_jmt_nodes);
    println!("Versions found in JMT:");
    for (v, count) in version_counts.iter().take(10) {
        println!("   -> Version #{}: {} nodes", v, count);
    }
    if version_counts.len() > 10 {
        println!("   -> ... (Tổng cộng {} versions)", version_counts.len());
        if let Some((v, count)) = version_counts.iter().next_back() {
            println!("   -> Version cao nhất #{}: {} nodes", v, count);
        }
    }

    println!("\n--- QUÉT HEADERS CỦA CÁC MỐC SNAPSHOT QUAN TRỌNG ---");
    let target_heights = vec![62209u64, 63361u64, 64513u64, 65665u64];
    let headers_cf = db_arc.cf_handle(CF_HEADERS).unwrap();
    let header_iter = db_arc.iterator_cf(headers_cf, rocksdb::IteratorMode::Start);
    for item in header_iter {
        if let Ok((_hash, header_raw)) = item {
            use prost::Message;
            if let Ok(hdr) = btc_genz_scl::proto::block::BlockHeader::decode(&header_raw[..]) {
                if target_heights.contains(&hdr.height) {
                    if let Some(sr) = &hdr.state_root {
                        println!("   -> Header #{}: StateRoot = {}", hdr.height, hex::encode(&sr.value));
                    } else {
                        println!("   -> Header #{}: StateRoot = None", hdr.height);
                    }
                }
            }
        }
    }

    println!("\n--- QUÉT CÁC STATE ROOT HASH CỦA JMT TẠI CÁC MỐC SNAPSHOT ---");
    let jmt_tree = jmt::JellyfishMerkleTree::<'_, StateManager, blake3::Hasher>::new(mgr.as_ref());
    for v in &target_heights {
        match jmt_tree.get_root_hash(*v) {
            Ok(root) => println!("   -> JMT Version #{}: Root = {}", v, hex::encode(root.0)),
            Err(e) => println!("   -> JMT Version #{}: ❌ LỖI: {:?}", v, e),
        }
    }

    // println!("\n--- KIỂM TRA GET STATE RANGE TẠI #64000 ---");
    // let (accounts, proof_bytes, last_key, is_last, _) = mgr.get_state_range([0u8; 32], 100000, 64000, [0u8; 32]);
    // println!("👉 get_state_range(64000) trả về: {} accounts", accounts.len());
    // println!("   -> Is Last: {}", is_last);
    // println!("   -> Last Key: {}", hex::encode(last_key));

    println!("\n--- THỬ TRUY XUẤT CÁC TÀI KHOẢN MẪU TẠI #64000 ---");
    let history_cf = db_arc.cf_handle(CF_ACC_HISTORY).expect("Missing HISTORY CF");
    for addr in sample_addresses {
        let kh = KeyHash::with::<blake3::Hasher>(&addr);
        let kh_2 = KeyHash::with::<blake3::Hasher>(&kh.0);
        println!("Address: 0x{}", hex::encode(addr));
        println!("   -> KeyHash 1 (Blake3): 0x{}", hex::encode(kh.0));
        
        // 1. Xem giá trị hiện tại ở CF_ACC
        if let Some(val) = db_arc.get_cf(acc_cf, addr).unwrap_or(None) {
            println!("   -> CF_ACC (addr key) len: {} | Blake3: {}", val.len(), hex::encode(blake3::hash(&val).as_bytes()));
            if let Ok(state) = AccountState::try_from_slice(&val) {
                print!("      Balance={:.2} BTC_Z, Nonce={}", state.btc_z as f64 / 1e8, state.nonce);
                println!();
            }
        }

        // 3. Gọi get_value_option tại version 64000
        let val_opt = TreeReader::get_value_option(mgr.as_ref(), 64000, kh).expect("Failed get_value_option");
        if let Some(val) = val_opt {
            print!("   -> get_value_option(64000) len: {}", val.len());
            if let Ok(state) = AccountState::try_from_slice(&val) {
                print!(" | Balance={:.2} BTC_Z, Nonce={}", state.btc_z as f64 / 1e8, state.nonce);
            }
            println!();
        } else {
            println!("   -> get_value_option(64000): ❌ KHÔNG TÌM THẤY");
        }
    }
}
