use btc_genz_scl::state_manager;
use btc_genz_scl::proto::block::BlockHeader;
use prost::Message;
use primitive_types::U256;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let db_path = if args.len() > 1 { &args[1] } else { "./temp_db" };

    println!("🔍 Opening DB: {}", db_path);
    if let Err(e) = state_manager::init_global_state(db_path) {
        println!("❌ Error: {:?}", e);
        return;
    }

    let mgr = state_manager::get_state_manager().expect("StateManager not init");

    // Load all headers from DB into memory first (simulating received headers vector)
    let mut headers = Vec::new();
    for h in 1..=306 {
        if let Some(hash) = mgr.get_block_hash(h) {
            if let Some(raw) = mgr.get_header_raw(&hash) {
                if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                    headers.push(hdr);
                }
            }
        }
    }
    println!("Loaded {} headers from DB into memory.", headers.len());

    let first_header = &headers[0];
    let mut history_ts = Vec::new();
    let mut history_diffs = Vec::new();

    // Load initial history before height 1 (which is only height 0)
    let start_h = 0;
    if let Some(hash) = mgr.get_block_hash(0) {
        if let Some(raw) = mgr.get_header_raw(&hash) {
            if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                history_ts.push(hdr.timestamp);
                let mut diff_padded = [0u8; 32];
                let d_len = hdr.difficulty.len().min(32);
                if d_len > 0 {
                    diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                }
                history_diffs.push(U256::from_little_endian(&diff_padded));
            }
        }
    }

    // Run the loop exactly like evaluate_header_chain with clean rebuilding DAA history
    for (i, h) in headers.iter().enumerate() {
        let needed_n = 120u64;
        
        // Clean rebuild DAA history if mismatch in length
        if h.height >= needed_n + 1 && (history_ts.len() as u64) < needed_n + 1 {
            history_ts.clear();
            history_diffs.clear();
            let start_h = h.height - needed_n - 1;
            for h_idx in start_h..h.height {
                let mut found_hdr = None;
                if h_idx >= first_header.height {
                    let offset = (h_idx - first_header.height) as usize;
                    if offset < headers.len() {
                        found_hdr = Some(headers[offset].clone());
                    }
                }
                
                if found_hdr.is_none() {
                    if let Some(hash) = mgr.get_block_hash(h_idx) {
                        if let Some(raw) = mgr.get_header_raw(&hash) {
                            if let Ok(hdr) = BlockHeader::decode(&raw[..]) {
                                found_hdr = Some(hdr);
                            }
                        }
                    }
                }

                if let Some(hdr) = found_hdr {
                    history_ts.push(hdr.timestamp);
                    if h_idx > start_h {
                        let mut diff_padded = [0u8; 32];
                        let d_len = hdr.difficulty.len().min(32);
                        if d_len > 0 {
                            diff_padded[..d_len].copy_from_slice(&hdr.difficulty[..d_len]);
                        }
                        history_diffs.push(U256::from_little_endian(&diff_padded));
                    }
                }
            }
        }

        let mut actual_padded = [0u8; 32];
        let d_len = h.difficulty.len().min(32);
        if d_len > 0 {
            actual_padded[..d_len].copy_from_slice(&h.difficulty[..d_len]);
        }
        let actual_diff = U256::from_little_endian(&actual_padded);

        if !history_ts.is_empty() {
            let expected_diff = btc_genz_scl::difficulty_logic::calculate_next_difficulty(
                &history_ts, &history_diffs, h.timestamp, h.height
            );
            if actual_diff != expected_diff {
                println!(
                    "❌ Height: {} - Divergence! Expected: {}, Actual in DB: {}, TS len: {}, Diff len: {}",
                    h.height, expected_diff, actual_diff, history_ts.len(), history_diffs.len()
                );
            }
        }

        // Push and drain
        history_ts.push(h.timestamp);
        history_diffs.push(actual_diff);
        let next_n = 120;
        if history_ts.len() > next_n + 1 {
            let excess = history_ts.len() - (next_n + 1);
            history_ts.drain(0..excess);
        }
        if history_diffs.len() > next_n {
            let excess = history_diffs.len() - next_n;
            history_diffs.drain(0..excess);
        }
    }
    
    println!("Simulation finished.");
}
