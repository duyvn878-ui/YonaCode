/**
 * @file genz_miner.rs
 * @brief Tiến trình thợ đào độc lập (Independent Miner) cho YonaCode.
 * @details Kết nối tới Go Node qua gRPC Bidirectional Streaming, nhận Task và băm Blake3.
 * @date 2026-06-23
 */

use std::sync::Arc;
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use rayon::prelude::*;

use btc_genz_scl::proto::consensus::{
    miner_gateway_client::MinerGatewayClient,
    NodeCommand, MinerMessage, MiningTask
};
use btc_genz_scl::genz_pow::{pack_header_v112, difficulty_to_target};
use btc_genz_scl::crypto_primitives::{calculate_blake3_hash, GENZ_POW_CONTEXT};
use primitive_types::U256;
use prost::Message;

struct MiningState {
    is_paused: bool,
    cpu_intensity: u32,
    active_task: Option<MiningTask>,
    session_id: u64,
    message_sender: Option<mpsc::Sender<MinerMessage>>,
}

#[tokio::main]
async fn main() {
    // Khởi tạo hệ thống ghi vết (Logger)
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    log::info!("===================================================");
    log::info!("  YonaCode Independent Miner v1.0 (BLAKE3)");
    log::info!("===================================================");

    // Xử lý đối số dòng lệnh để xác định địa chỉ node gRPC và số luồng băm
    let args: Vec<String> = std::env::args().collect();
    let mut server_url_opt = None;
    let mut threads_opt = None;
    let mut wallet_address = None;

    let mut i = 1;
    while i < args.len() {
        if args[i] == "--url" && i + 1 < args.len() {
            server_url_opt = Some(args[i + 1].clone());
            i += 2;
        } else if args[i] == "--port" && i + 1 < args.len() {
            server_url_opt = Some(format!("http://127.0.0.1:{}", args[i + 1]));
            i += 2;
        } else if (args[i] == "--threads" || args[i] == "-t") && i + 1 < args.len() {
            if let Ok(t) = args[i + 1].parse::<usize>() {
                threads_opt = Some(t);
            }
            i += 2;
        } else if (args[i] == "--address" || args[i] == "-a") && i + 1 < args.len() {
            wallet_address = Some(args[i + 1].clone());
            i += 2;
        } else {
            i += 1;
        }
    }

    let server_url = match server_url_opt {
        Some(url) => url,
        None => {
            if wallet_address.is_some() {
                // Đọc IP từ biến môi trường YONA_POOL_IP nếu có để tăng tính động, fallback về VPS IP mặc định
                let pool_ip = std::env::var("YONA_POOL_IP").unwrap_or_else(|_| "110.172.28.103".to_string());
                format!("http://{}:18080", pool_ip)
            } else {
                "http://127.0.0.1:18080".to_string()
            }
        }
    };

    log::info!("[MINER-INIT] 📡 Địa chỉ gRPC Go Node: {}", server_url);
    if let Some(ref addr) = wallet_address {
        log::info!("[MINER-INIT] 🏦 Chế độ đào Bể (Pool Mining) hoạt động. Ví thợ đào: {}", addr);
    }

    // Xác định số luồng khai thác tối ưu
    let total_cores = std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1);
    let num_threads = threads_opt.unwrap_or(if total_cores > 1 { total_cores - 1 } else { 1 });
    log::info!(
        "[MINER-INIT] ⚙️ Số luồng CPU sử dụng: {}/{} Cores (Chừa lại 1 Core tránh nghẽn Tokio/OS)",
        num_threads,
        total_cores
    );

    // Trạng thái khai thác dùng chung
    let state = Arc::new(Mutex::new(MiningState {
        is_paused: true,
        cpu_intensity: 50,
        active_task: None,
        session_id: 0,
        message_sender: None,
    }));

    // Khởi chạy luồng băm song song Rayon độc lập
    let state_clone = Arc::clone(&state);
    std::thread::spawn(move || {
        mining_worker_thread(state_clone, num_threads);
    });

    // Vòng lặp kết nối và duy trì gRPC Stream (Tự động kết nối lại khi đứt mạng)
    loop {
        log::info!("[MINER-CONN] 🔗 Đang kết nối tới Go Node...");
        match MinerGatewayClient::connect(server_url.clone()).await {
            Ok(mut client) => {
                log::info!("[MINER-CONN] ✅ Kết nối gRPC thành công!");
                
                // Khởi tạo kênh truyền dẫn cục bộ cho kết nối này
                let (local_tx, local_rx) = mpsc::channel::<MinerMessage>(100);
                
                // Cập nhật kênh truyền vào trạng thái dùng chung
                {
                    let mut lock = state.lock().unwrap();
                    lock.message_sender = Some(local_tx);
                }

                // Thực hiện kết nối luồng gRPC 2 chiều
                let mut req = tonic::Request::new(ReceiverStream::new(local_rx));
                if let Some(ref addr) = wallet_address {
                    if let Ok(meta_val) = tonic::metadata::MetadataValue::try_from(addr.as_str()) {
                        req.metadata_mut().insert("x-wallet-address", meta_val);
                    }
                }

                match client.connect_miner(req).await {
                    Ok(response) => {
                        let mut inbound = response.into_inner();
                        log::info!("[MINER-CONN] 🚀 Bắt đầu nhận lệnh điều khiển từ Go Node.");
                        
                        while let Ok(Some(cmd)) = inbound.message().await {
                            let mut lock = state.lock().unwrap();
                            lock.is_paused = cmd.is_paused;
                            lock.cpu_intensity = cmd.cpu_intensity;
                            lock.session_id = cmd.session_id;

                            if !cmd.block_template.is_empty() {
                                if let Ok(task) = MiningTask::decode(cmd.block_template.as_slice()) {
                                    log::info!(
                                        "[MINER-CMD] 📡 Nhận Task mới | Height: #{} | Cường độ: {}% | SID: {}",
                                        task.header.as_ref().map(|h| h.height).unwrap_or(0),
                                        lock.cpu_intensity,
                                        cmd.session_id
                                    );
                                    lock.active_task = Some(task);
                                } else {
                                    log::error!("[MINER-CMD] ❌ Lỗi giải mã MiningTask từ Go Node!");
                                }
                            } else {
                                log::info!(
                                    "[MINER-CMD] 📡 Cập nhật trạng thái: Pause = {}, Cường độ = {}% | SID: {}",
                                    cmd.is_paused,
                                    cmd.cpu_intensity,
                                    cmd.session_id
                                );
                            }
                        }
                        log::warn!("[MINER-CONN] ⚠️ Luồng gRPC bị đóng bởi Go Node.");
                    }
                    Err(e) => {
                        log::error!("[MINER-CONN] ❌ Lỗi gọi ConnectMiner: {}", e);
                    }
                }

                // Dọn dẹp kênh truyền khi ngắt kết nối
                {
                    let mut lock = state.lock().unwrap();
                    lock.message_sender = None;
                }
            }
            Err(e) => {
                log::error!("[MINER-CONN] ❌ Kết nối thất bại: {}. Thử lại sau 2 giây...", e);
            }
        }

        tokio::time::sleep(Duration::from_secs(2)).await;
    }
}

/// Luồng băm chính chạy song song bằng Rayon
fn mining_worker_thread(state: Arc<Mutex<MiningState>>, num_threads: usize) {
    let mut current_session_id = 0;
    let mut cursor = 0u64;
    let mut midstate_hasher = None;
    let mut initial_target = U256::zero();
    let mut initial_target_u64_last = 0u64;
    let mut header = None;

    // Khởi tạo ThreadPool với số thread tương ứng cấu hình
    let rayon_pool = rayon::ThreadPoolBuilder::new()
        .num_threads(num_threads)
        .thread_name(|i| format!("genz-miner-{}", i))
        .build()
        .unwrap();

    let mut last_hashrate_report = Instant::now();
    let mut last_console_report = Instant::now();
    let mut hashes_in_period = 0u64;
    let mut hashes_for_console = 0u64;

    loop {
        // 1. Đọc trạng thái khai thác hiện tại
        let (is_paused, cpu_intensity, active_task_opt, session_id, message_sender) = {
            let lock = state.lock().unwrap();
            (
                lock.is_paused,
                lock.cpu_intensity,
                lock.active_task.clone(),
                lock.session_id,
                lock.message_sender.clone()
            )
        };

        // Nếu tạm dừng hoặc chưa có task/kết nối, ngủ một lúc
        if is_paused || active_task_opt.is_none() || message_sender.is_none() {
            if last_hashrate_report.elapsed() >= Duration::from_secs(2) {
                if let Some(ref sender) = message_sender {
                    let _ = sender.blocking_send(MinerMessage {
                        current_hashrate: 0,
                        found_nonce: 0,
                        block_hash: vec![],
                        session_id,
                    });
                }
                last_hashrate_report = Instant::now();
                last_console_report = Instant::now();
                hashes_in_period = 0;
                hashes_for_console = 0;
            }
            std::thread::sleep(Duration::from_millis(200));
            continue;
        }

        let task = active_task_opt.unwrap();
        let Some(t_header) = &task.header else {
            std::thread::sleep(Duration::from_millis(200));
            continue;
        };

        // 2. Nếu đổi phiên đào (Session ID), cấu hình lại Hasher
        if session_id != current_session_id {
            log::info!("[MINER-WORKER] ⛏️ Bắt đầu đào Session: {} | Khối #{}", session_id, t_header.height);
            current_session_id = session_id;
            cursor = 0;
            header = Some(t_header.clone());

            let mut diff_padded = [0u8; 32];
            let d_len = t_header.difficulty.len().min(32);
            if d_len > 0 {
                diff_padded[..d_len].copy_from_slice(&t_header.difficulty[..d_len]);
            }
            let initial_difficulty = U256::from_little_endian(&diff_padded);
            initial_target = difficulty_to_target(initial_difficulty);
            initial_target_u64_last = initial_target.0[3];

            let header_buf = pack_header_v112(
                t_header.height,
                &t_header.parent_hash.as_ref().map(|h| h.value.clone()).unwrap_or_default(),
                t_header.timestamp,
                &t_header.tx_root.as_ref().map(|m| m.value.clone()).unwrap_or_default(),
                &t_header.difficulty,
            );
            let header_hash = calculate_blake3_hash(header_buf.to_vec(), t_header.height);

            let base_hasher = blake3::Hasher::new_derive_key(GENZ_POW_CONTEXT);
            let mut hasher = base_hasher.clone();
            hasher.update(&header_hash);
            midstate_hasher = Some(hasher);

            last_hashrate_report = Instant::now();
            last_console_report = Instant::now();
            hashes_in_period = 0;
            hashes_for_console = 0;
        }

        let Some(ref hasher) = midstate_hasher else {
            std::thread::sleep(Duration::from_millis(100));
            continue;
        };

        // 3. Thực hiện băm một lô nonces
        let batch_size = 5_000_000;
        let start_time = Instant::now();

        let found_nonce = rayon_pool.install(|| {
            (0..batch_size).into_par_iter().find_map_any(|i| {
                let nonce_to_test = cursor + i as u64;
                let mut h = hasher.clone();
                h.update(&nonce_to_test.to_le_bytes());
                let hash_result: [u8; 32] = h.finalize().into();

                let h_last = u64::from_le_bytes(hash_result[24..32].try_into().unwrap());
                if h_last <= initial_target_u64_last {
                    let hash_u256 = U256::from_little_endian(&hash_result);
                    if hash_u256 < initial_target {
                        return Some((nonce_to_test, hash_result.to_vec()));
                    }
                }
                None
            })
        });

        cursor += batch_size as u64;
        hashes_in_period += batch_size as u64;
        hashes_for_console += batch_size as u64;

        // Báo cáo Hashrate định kỳ mỗi 2 giây lên Go Node
        if last_hashrate_report.elapsed() >= Duration::from_secs(2) {
            let elapsed_secs = last_hashrate_report.elapsed().as_secs_f64();
            let hashrate = (hashes_in_period as f64 / elapsed_secs) as u64;

            if let Some(ref sender) = message_sender {
                let _ = sender.blocking_send(MinerMessage {
                    current_hashrate: hashrate,
                    found_nonce: 0,
                    block_hash: vec![],
                    session_id,
                });
            }

            last_hashrate_report = Instant::now();
            hashes_in_period = 0;
        }

        // In log hashrate định kỳ mỗi 10 giây ra console
        if last_console_report.elapsed() >= Duration::from_secs(10) {
            let elapsed_secs = last_console_report.elapsed().as_secs_f64();
            let hashrate_khs = (hashes_for_console as f64 / elapsed_secs / 1000.0) as u64;
            log::info!(
                "[MINER-WORKER] 📊 Tốc độ đào: {} KH/s (~{} MH/s) | Khối #{} | Cường độ CPU: {}%",
                hashrate_khs,
                hashrate_khs / 1000,
                header.as_ref().map(|h| h.height).unwrap_or(0),
                cpu_intensity
            );
            last_console_report = Instant::now();
            hashes_for_console = 0;
        }

        // 4. Nếu tìm thấy Nonce hợp lệ
        if let Some((nonce, hash)) = found_nonce {
            log::info!("[MINER-WORKER] 🎉 THÀNH CÔNG! Tìm thấy Nonce: {} cho Khối #{}", nonce, header.as_ref().unwrap().height);
            if let Some(ref sender) = message_sender {
                let _ = sender.blocking_send(MinerMessage {
                    current_hashrate: 0,
                    found_nonce: nonce,
                    block_hash: hash,
                    session_id,
                });
            }

            // Giải phóng task để tránh tiếp tục đào trùng
            let mut lock = state.lock().unwrap();
            if lock.session_id == session_id {
                lock.active_task = None;
            }
            continue;
        }

        // 5. Kiểm soát cường độ CPU (Intensity)
        let work_micros = start_time.elapsed().as_micros() as u64;
        let intensity = cpu_intensity.clamp(1, 100) as u64;
        if intensity < 100 {
            let sleep_micros = (work_micros * (100 - intensity)) / intensity;
            if sleep_micros >= 1000 {
                std::thread::sleep(Duration::from_micros(sleep_micros.min(1_000_000)));
            } else if sleep_micros > 0 {
                std::thread::yield_now();
            }
        } else {
            // Sleep cực ngắn trên Windows để hệ điều hành chuyển đổi luồng gRPC/Tokio mượt mà
            std::thread::sleep(Duration::from_micros(100));
        }
    }
}
