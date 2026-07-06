use rocksdb::{DB, Options};

fn main() {
    // Cho phép truyền đường dẫn cơ sở dữ liệu qua đối số, fallback về "./data/scl" cho tính tương thích đa nền tảng
    let args: Vec<String> = std::env::args().collect();
    let db_path = if args.len() > 1 {
        &args[1]
    } else {
        "./data/scl"
    };
    let mut opts = Options::default();
    opts.create_missing_column_families(true);
    let cfs = DB::list_cf(&opts, db_path).unwrap_or_else(|_| vec!["default".to_string()]);
    let db = DB::open_cf_for_read_only(&opts, db_path, cfs, false).unwrap();

    let meta_cf = db.cf_handle("meta").expect("Missing meta CF");
    
    if let Ok(Some(v)) = db.get_cf(meta_cf, b"jmt_v") {
        let height = u64::from_le_bytes(v.try_into().unwrap());
        println!("🚀 Cao độ hiện tại của Node: #{}", height);
    }

    if let Ok(Some(v)) = db.get_cf(meta_cf, b"finalized_h") {
        let finalized_h = u64::from_le_bytes(v.try_into().unwrap());
        println!("🔒 Cao độ đã chốt (Finalized): #{}", finalized_h);
    }
}
