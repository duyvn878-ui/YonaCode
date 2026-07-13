use tonic::{transport::Server, Request, Response, Status};
use tower::limit::ConcurrencyLimitLayer;
use btc_genz_scl::proto::scl::scl_service_server::{SclService, SclServiceServer};
use btc_genz_scl::proto::scl::*;
use btc_genz_scl::proto::transaction::Transaction;
use btc_genz_scl::proto::block::*;
use btc_genz_scl::proto::common::Address;
use btc_genz_scl::proto::scl::{AccountSnapshot, MaturingReward};

use btc_genz_scl::proto::consensus::MiningResult;
use prost::Message;
use borsh::{BorshDeserialize, BorshSerialize};

use std::sync::Arc;
use tokio::sync::{Mutex, broadcast};
use tokio_stream::wrappers::ReceiverStream;
use lazy_static::lazy_static;

lazy_static! {
    static ref MINING_LOCK: Arc<Mutex<()>> = Arc::new(Mutex::new(()));
    // [SECURITY-HARDENING] Shared Secret Token được sinh ngẫu nhiên khi khởi chạy
    // Tại sao: Ngăn chặn malware/SSRF gọi các hàm hủy diệt (Purge, Rollback, Rebuild)
    // qua gRPC mà không có quyền hạn. Token chỉ tồn tại trong RAM, KHÔNG ghi ra file.
    static ref AUTH_TOKEN: String = {
        use rand::Rng;
        // [VANGUARD-FIX] Hỗ trợ nhận token từ môi trường nếu được set trước (Decoupled Startup)
        if let Ok(forced) = std::env::var("SCL_FORCE_TOKEN") {
            if forced.len() >= 32 {
                return forced;
            }
        }
        let token: [u8; 32] = rand::thread_rng().gen();
        hex::encode(token)
    };

    // [VANGUARD-EVENT] Kênh Broadcast phát sự kiện hệ thống tới Go Bridge, chứa tối đa 1024 sự kiện chưa đọc.
    pub static ref CORE_EVENT_TX: broadcast::Sender<CoreEvent> = {
        let (tx, _) = broadcast::channel(1024);
        tx
    };
}

// [VANGUARD-EVENT] Hàm tiện ích để phát sự kiện hệ thống từ Rust Core lên Go Bridge
pub fn emit_core_event(event_type: core_event::EventType, msg: String, payload: Vec<u8>) {
    let event = CoreEvent {
        r#type: event_type as i32,
        message: msg,
        timestamp: chrono::Utc::now().timestamp() as u64,
        payload,
    };
    // Send không blocking. Bỏ qua lỗi nếu Go chưa kết nối.
    let _ = CORE_EVENT_TX.send(event);
}

/// [SECURITY-HARDENING] Kiểm tra Shared Secret Token trong metadata gRPC
/// Trả về Ok(()) nếu token hợp lệ, Err(Status) nếu không
fn verify_auth_token<T>(request: &Request<T>) -> Result<(), Status> {
    let metadata = request.metadata();
    
    // [SECURITY-AUDIT] Trích xuất thông tin định danh để truy vết
    let client_id = metadata.get("client-id").and_then(|v| v.to_str().ok()).unwrap_or("unknown-client");
    let timestamp = metadata.get("timestamp").and_then(|v| v.to_str().ok()).unwrap_or("no-ts");

    match metadata.get("x-auth-token") {
        Some(token_val) => {
            let provided = token_val.to_str().unwrap_or("");
            if provided == AUTH_TOKEN.as_str() {
                Ok(())
            } else {
                log::error!(
                    "[SECURITY] 🚨 gRPC AUTH FAILED: Token không khớp! | Client: {} | TS: {} | Provided: {}...", 
                    client_id, timestamp, &provided[..8.min(provided.len())]
                );
                Err(Status::permission_denied("Unauthorized: Invalid auth token"))
            }
        }
        None => {
            log::error!(
                "[SECURITY] 🚨 gRPC AUTH FAILED: Thiếu x-auth-token! | Client: {} | TS: {}", 
                client_id, timestamp
            );
            Err(Status::permission_denied("Unauthorized: Missing auth token"))
        }
    }
}

pub struct MySclService {}

#[tonic::async_trait]
impl SclService for MySclService {
    async fn init_scl(&self, request: Request<InitRequest>) -> Result<Response<GenericResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        match btc_genz_scl::init_scl_state(req.db_path) {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => {
                let err_msg = format!("RocksDB Initialization Failed: {}", e);
                eprintln!("[SCL-SERVER] ❌ {}", err_msg);
                Err(Status::internal(err_msg))
            }
        }
    }

    async fn get_balance(&self, request: Request<BalanceRequest>) -> Result<Response<BalanceResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        // Tại sao: Kiểm tra thủ công độ dài địa chỉ để từ chối sớm payload bất thường trước khi đi sâu vào xử lý, phòng chống DoS
        if req.address.len() > 32 {
            return Err(Status::invalid_argument("Address too long (max 32 bytes)"));
        }
        let balance = btc_genz_scl::get_balance(req.address);
        Ok(Response::new(BalanceResponse { balance }))
    }

    async fn get_balance_batch(
        &self,
        request: Request<BatchBalanceRequest>,
    ) -> Result<Response<BatchBalanceResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        
        // Tại sao thiết kế như vậy: Bọc xử lý I/O nặng nề và lặp lại của RocksDB trong spawn_blocking
        // để giải phóng các luồng worker async của Tokio, tránh nghẽn luồng (Thread Starvation).
        let balances = tokio::task::spawn_blocking(move || {
            req.addresses.into_iter().map(|addr| {
                let balance = btc_genz_scl::get_balance(addr.clone());
                let nonce = btc_genz_scl::get_nonce(addr.clone());
                BalanceEntry { address: addr, balance, nonce }
            }).collect()
        }).await.map_err(|e| Status::internal(format!("Lỗi spawn_blocking: {}", e)))?;

        Ok(Response::new(BatchBalanceResponse { balances }))
    }

    async fn get_nonce(&self, request: Request<NonceRequest>) -> Result<Response<NonceResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        // Tại sao: Kiểm tra thủ công độ dài địa chỉ để từ chối sớm payload bất thường
        if req.address.len() > 32 {
            return Err(Status::invalid_argument("Address too long (max 32 bytes)"));
        }
        let nonce = btc_genz_scl::get_nonce(req.address);
        Ok(Response::new(NonceResponse { nonce }))
    }

    async fn get_account_state(&self, request: Request<BalanceRequest>) -> Result<Response<AccountSnapshot>, Status> {
        let req = request.into_inner();
        let addr: [u8; 32] = req.address.try_into().map_err(|_| Status::invalid_argument("Invalid address length"))?;
        
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::unavailable("StateManager not initialized"))?;
        let state = mgr.get_account_state(&addr);
        
        Ok(Response::new(AccountSnapshot {
            address: addr.to_vec(),
            balance: state.btc_z,
            nonce: state.nonce,
            nano_weight: state.nano_weight,
            maturing_rewards: state.maturing_rewards.into_iter().map(|r| MaturingReward {
                amount: r.amount,
                height: r.height,
            }).collect(),
            coin_id: state.coin_id.to_vec(),
            last_full_cleanup: state.last_full_cleanup,
        }))
    }



    async fn get_state_root(&self, _request: Request<Empty>) -> Result<Response<HashResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let hash = btc_genz_scl::get_state_root();
        Ok(Response::new(HashResponse { hash }))
    }

    async fn get_spendable_balance(&self, request: Request<BalanceRequest>) -> Result<Response<BalanceResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        // Tại sao: Kiểm tra thủ công độ dài địa chỉ để phòng ngừa DoS
        if req.address.len() > 32 {
            return Err(Status::invalid_argument("Address too long (max 32 bytes)"));
        }
        let balance = btc_genz_scl::get_spendable_balance(req.address);
        Ok(Response::new(BalanceResponse { balance }))
    }

    async fn debug_dump_smt_nodes(&self, _request: Request<Empty>) -> Result<Response<StringResponse>, Status> {
        if let Some(mgr) = btc_genz_scl::state_manager::get_state_manager() {
            let value = mgr.debug_dump_smt_nodes();
            return Ok(Response::new(StringResponse { value }));
        }
        Ok(Response::new(StringResponse { value: "".to_string() }))
    }

    async fn get_finalized_height(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let value = btc_genz_scl::get_finalized_height();
        Ok(Response::new(Uint64Response { value }))
    }

    async fn set_finalized_height(&self, request: Request<Uint64Request>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let req = request.into_inner();
        btc_genz_scl::set_finalized_height(req.value);
        Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() }))
    }

    async fn force_set_finalized_height(&self, request: Request<Uint64Request>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let req = request.into_inner();
        btc_genz_scl::force_set_finalized_height(req.value);
        Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() }))
    }

    async fn get_current_version(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let value = btc_genz_scl::get_current_version();
        Ok(Response::new(Uint64Response { value }))
    }

    async fn get_oldest_height(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let value = btc_genz_scl::get_oldest_height();
        Ok(Response::new(Uint64Response { value }))
    }

    async fn get_median_time_past(&self, request: Request<Uint64Request>) -> Result<Response<Uint64Response>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        let value = if let Some(mgr) = btc_genz_scl::state_manager::get_state_manager() {
            mgr.get_median_time_past(req.value)
        } else { 0 };
        Ok(Response::new(Uint64Response { value }))
    }


    async fn get_transaction_status(&self, request: Request<HashRequest>) -> Result<Response<TxStatusResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        // Tại sao: Kiểm tra độ dài txHash đầu vào để từ chối sớm payload bất thường
        if req.hash.len() > 32 {
            return Err(Status::invalid_argument("Hash too long (max 32 bytes)"));
        }
        let res = btc_genz_scl::get_transaction_status(req.hash);
        Ok(Response::new(TxStatusResponse { 
            height: res.height, 
            status: res.status,
            is_finalized: res.is_finalized,
            confirmations: res.confirmations,
            sender_prev_balance: res.sender_prev_balance,
            sender_post_balance: res.sender_post_balance,
            receiver_prev_balance: res.receiver_prev_balance,
            receiver_post_balance: res.receiver_post_balance,
        }))
    }

    async fn get_transaction_status_batch(
        &self,
        request: Request<BatchTxStatusRequest>,
    ) -> Result<Response<BatchTxStatusResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        let hashes = req.hashes.clone();

        // Tại sao thiết kế như vậy: Hàm get_transaction_status_batch gọi thư viện để đọc trạng thái giao dịch
        // từ RocksDB liên tục. Bọc trong spawn_blocking ngăn chặn block luồng async của gRPC Server.
        let statuses = tokio::task::spawn_blocking(move || {
            let res = btc_genz_scl::get_transaction_status_batch(hashes.clone());
            res.into_iter().enumerate().map(|(idx, entry)| TxStatusEntry {
                hash: hashes[idx].clone(),
                height: entry.height,
                status: entry.status,
                is_finalized: entry.is_finalized,
                confirmations: entry.confirmations,
                sender_prev_balance: entry.sender_prev_balance,
                sender_post_balance: entry.sender_post_balance,
                receiver_prev_balance: entry.receiver_prev_balance,
                receiver_post_balance: entry.receiver_post_balance,
            }).collect()
        }).await.map_err(|e| Status::internal(format!("Lỗi spawn_blocking: {}", e)))?;

        Ok(Response::new(BatchTxStatusResponse { statuses }))
    }

    async fn purge_historical_data(
        &self,
        request: Request<BytesRequest>,
    ) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        // [SECURITY-HARDENING] Kiểm tra quyền trước khi thực thi lệnh hủy diệt
        verify_auth_token(&request)?;

        let req = request.into_inner();
        // Giải mã 16 bytes: 8 bytes start + 8 bytes end (BigEndian)
        if req.data.len() < 16 {
            return Ok(Response::new(GenericResponse {
                success: false,
                error_msg: "Dữ liệu Purge không hợp lệ (cần 16 bytes)".to_string(),
            }));
        }
        
        let start = u64::from_be_bytes(req.data[0..8].try_into().unwrap());
        let end = u64::from_be_bytes(req.data[8..16].try_into().unwrap());

        log::info!("[SCL-PURGE] 🧹 Nhận lệnh gRPC (ĐÃ XÁC THỰC): Xóa Body từ #{} đến #{}", start, end);
        let success = btc_genz_scl::purge_historical_data(start, end);
        
        Ok(Response::new(GenericResponse {
            success,
            error_msg: if success { "Purge completed".into() } else { "Purge failed".into() },
        }))
    }

    async fn emergency_state_rebuild(
        &self,
        request: Request<Uint64Request>,
    ) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        // [SECURITY-HARDENING] Kiểm tra quyền trước khi thực thi lệnh khôi phục khẩn cấp
        verify_auth_token(&request)?;

        let req = request.into_inner();
        log::info!("[SCL-RECOVERY] ☢️ Nhận lệnh gRPC (ĐÃ XÁC THỰC): Khôi phục Sổ cái từ Headers tại cao độ #{}", req.value);
        let success = btc_genz_scl::emergency_state_rebuild(req.value);
        
        Ok(Response::new(GenericResponse {
            success,
            error_msg: if success { "Rebuild successful".into() } else { "Rebuild failed".into() },
        }))
    }

    async fn reset_state_completely(
        &self,
        request: Request<Empty>,
    ) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        log::warn!("[SCL-SERVER] ⚠️ Nhận lệnh gRPC (ĐÃ XÁC THỰC): Reset toàn bộ trạng thái Sổ cái về Genesis!");
        let success = btc_genz_scl::reset_state_completely();
        Ok(Response::new(GenericResponse {
            success,
            error_msg: if success { "Reset successful".into() } else { "Reset failed".into() },
        }))
    }

    async fn get_highest_block_height(
        &self,
        request: Request<Empty>,
    ) -> Result<Response<Uint64Response>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let value = btc_genz_scl::get_highest_block_height();
        Ok(Response::new(Uint64Response { value }))
    }

    async fn execute_block(&self, request: Request<SclBlockExecutionRequest>) -> Result<Response<SclBlockExecutionResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        
        let exec_res = tokio::task::spawn_blocking(move || {
            btc_genz_scl::execute_block_transactions(
                req.body_raw,
                req.miner_address,
                req.parent_hash,
                req.height,
                req.is_simulation,
            )
        })
        .await
        .map_err(|e| Status::internal(format!("Tokio spawn blocking failed: {}", e)))?;

        Ok(Response::new(SclBlockExecutionResponse {
            state_root: exec_res.state_root,
            success: exec_res.success,
            error_msg: exec_res.error_msg,
            failing_tx_index: exec_res.failing_tx_index,
            data: vec![], // [VANGUARD-FIX] Match new Proto
        }))
    }

    async fn build_vanguard_block_template(
        &self,
        request: Request<BytesRequest>,
    ) -> Result<Response<BytesResponse>, Status> {
        let req_raw = request.into_inner();

        #[derive(BorshDeserialize)]
        struct InternalBuildRequest {
            height: u64,
            parent_hash: Vec<u8>,
            miner_address: Vec<u8>,
            transactions_bytes: Vec<Vec<u8>>,
            timestamp: u64,
            difficulty_raw: Vec<u8>,
        }

        let req: InternalBuildRequest = BorshDeserialize::try_from_slice(&req_raw.data)
            .map_err(|e| Status::invalid_argument(format!("Borsh Decode Failed (Template): {}", e)))?;

        let res_bytes = tokio::task::spawn_blocking(move || {
            btc_genz_scl::build_vanguard_block_template(
                req.height,
                req.parent_hash,
                req.miner_address,
                req.transactions_bytes,
                req.timestamp,
                req.difficulty_raw,
            )
        })
        .await
        .map_err(|e| Status::internal(format!("Tokio spawn blocking failed: {}", e)))?;

        Ok(Response::new(BytesResponse { data: res_bytes }))
    }



    async fn commit_block_hash(&self, request: Request<CommitHashRequest>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let req = request.into_inner();
        let success = btc_genz_scl::commit_block_hash(req.height, req.hash);
        Ok(Response::new(GenericResponse { success, error_msg: "".to_string() }))
    }

    async fn authoritative_sign(&self, request: Request<SignRequest>) -> Result<Response<SignResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        match btc_genz_scl::authoritative_sign(req.data, req.private_key) {
            Ok(sig) => Ok(Response::new(SignResponse { signature: sig, success: true, error_msg: "".to_string() })),
            Err(e) => Ok(Response::new(SignResponse { signature: Vec::new(), success: false, error_msg: e.to_string() })),
        }
    }

    async fn prepare_transaction(&self, request: Request<PrepareTxRequest>) -> Result<Response<Transaction>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        match btc_genz_scl::prepare_transaction(
            req.sender,
            req.receiver,
            req.amount,
            req.fee,
            req.nonce,
            req.private_key,
            req.recent_block_hash,
        ) {
            Ok(tx) => Ok(Response::new(tx)),
            Err(e) => Err(Status::internal(format!("Prepare TX Failed: {}", e))),
        }
    }

    async fn rollback_state(&self, request: Request<RollbackRequest>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        // [SECURITY-HARDENING] Kiểm tra quyền trước khi thực thi lệnh quay lùi trạng thái
        verify_auth_token(&request)?;

        let req = request.into_inner();
        log::info!("[SCL-ROLLBACK] ⚠️ Nhận lệnh gRPC (ĐÃ XÁC THỰC): Rollback từ #{} về #{}", req.current_height, req.target_height);
        let success = btc_genz_scl::rollback_state(req.current_height, req.target_height);
        Ok(Response::new(GenericResponse { success, error_msg: "".to_string() }))
    }

    /// [BÀN TAY VÔ HÌNH] Xóa khối vật lý — bỏ qua Tường lửa Bất biến.
    /// Chỉ gọi từ công cụ nhà vận hành node (localhost + mã xác nhận 01900).
    async fn force_delete_blocks(&self, request: Request<RollbackRequest>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;

        let req = request.into_inner();
        log::warn!("[SCL-INVISIBLE-HAND] ☢️ Nhận lệnh gRPC (ĐÃ XÁC THỰC): XÓA VẬT LÝ từ #{} về #{}", req.current_height, req.target_height);
        let success = btc_genz_scl::force_delete_blocks(req.current_height, req.target_height);
        Ok(Response::new(GenericResponse { success, error_msg: if success { "".to_string() } else { "Force delete blocks failed".to_string() } }))
    }


    async fn calculate_next_difficulty(&self, request: Request<DifficultyRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let data = btc_genz_scl::calculate_next_difficulty_v2(
            req.timestamps,
            req.difficulties,
            req.current_ts,
            req.height
        );
        Ok(Response::new(BytesResponse { data }))
    }

    async fn verify_pow(&self, request: Request<VerifyPowRequest>) -> Result<Response<VerifyPowResponse>, Status> {
        let req = request.into_inner();
        let res = btc_genz_scl::verify_pow(
            req.header_bytes,
            req.nonce,
            req.difficulty,
            req.height
        );
        
        let result = match res {
            btc_genz_scl::BlockVerificationResult::Success => 0,
            btc_genz_scl::BlockVerificationResult::InvalidPoW => 1,
            btc_genz_scl::BlockVerificationResult::FirewallViolation => 2,
            btc_genz_scl::BlockVerificationResult::DbBusy => 3, // [VANGUARD-FIX] Trả về mã 3 cho lỗi DB bận
        };
        
        Ok(Response::new(VerifyPowResponse { 
            is_valid: res == btc_genz_scl::BlockVerificationResult::Success,
            result 
        }))
    }



    async fn get_hashrate(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        let value = btc_genz_scl::get_hashrate();
        Ok(Response::new(Uint64Response { value }))
    }

    async fn calculate_absolute_weight(&self, request: Request<WeightRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let data = btc_genz_scl::calculate_absolute_weight(req.parent_weight, req.difficulty);
        Ok(Response::new(BytesResponse { data }))
    }




    async fn calculate_short_tx_id(&self, request: Request<ShortIdRequest>) -> Result<Response<Uint64Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::calculate_short_tx_id_ffi(req.tx_hash, req.nonce, 0);
        Ok(Response::new(Uint64Response { value }))
    }

    async fn verify_block_reconstruction(&self, request: Request<ReconstructionRequest>) -> Result<Response<BoolResponse>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::verify_block_reconstruction(req.expected_tx_root, req.tx_hashes);
        Ok(Response::new(BoolResponse { value }))
    }

    async fn verify_timestamp_firewall(&self, request: Request<TimestampFirewallRequest>) -> Result<Response<BoolResponse>, Status> {
        let req = request.into_inner();
        
        // req.median_time_past giờ đây chứa parent_timestamp được truyền đúng từ Go
        let mtp = req.median_time_past;

        // [SECURITY-FIX] Sử dụng thời gian thực của Node Rust để chống Timestamp Spoofing Bypass
        let current_now = chrono::Utc::now().timestamp() as u64;
        let value = btc_genz_scl::verify_timestamp_firewall(req.timestamp, mtp, current_now, false);
        Ok(Response::new(BoolResponse { value }))
    }


    async fn import_state_snapshot(&self, request: Request<SnapshotRequest>) -> Result<Response<SclBlockExecutionResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        let res = btc_genz_scl::import_state_snapshot_raw(req.data, req.version);
        Ok(Response::new(SclBlockExecutionResponse {
            state_root: res.state_root,
            success: res.success,
            error_msg: res.error_msg,
            failing_tx_index: res.failing_tx_index,
            data: vec![],
        }))
    }

    async fn import_state_snapshot_path(&self, request: Request<SnapshotPathRequest>) -> Result<Response<BytesResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        let res = btc_genz_scl::import_state_snapshot_path(req.path, req.version);
        // [VANGUARD-FIX] Đóng gói kết quả thành bytes bằng Borsh
        let data = borsh::to_vec(&res).unwrap_or_default();
        Ok(Response::new(BytesResponse { data }))
    }





    async fn verify_signature(&self, request: Request<SignatureCheckRequest>) -> Result<Response<BoolResponse>, Status> {
        let req = request.into_inner();
        if req.address.len() != 32 || req.message.len() != 32 || req.signature.len() != 64 {
            return Ok(Response::new(BoolResponse { value: false }));
        }
        
        let mut addr = [0u8; 32];
        addr.copy_from_slice(&req.address);
        let mut msg = [0u8; 32];
        msg.copy_from_slice(&req.message);
        let mut sig = [0u8; 64];
        sig.copy_from_slice(&req.signature);
        
        let valid = btc_genz_scl::crypto_primitives::verify_ed25519_signature(&addr, &msg, &sig);
        Ok(Response::new(BoolResponse { value: valid }))
    }

    async fn calculate_expected_supply(&self, request: Request<Uint64Request>) -> Result<Response<Uint64Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::calculate_expected_supply(req.value);
        Ok(Response::new(Uint64Response { value }))
    }

    async fn set_expected_supply(&self, request: Request<Uint64Request>) -> Result<Response<GenericResponse>, Status> {
        verify_auth_token(&request)?;
        let req = request.into_inner();
        btc_genz_scl::set_expected_supply(req.value);
        Ok(Response::new(GenericResponse { success: true, error_msg: "Supply updated".into() }))
    }


    async fn export_state_snapshot(&self, _request: Request<Empty>) -> Result<Response<BytesResponse>, Status> {
        let snapshot = btc_genz_scl::export_state_snapshot_raw();
        Ok(Response::new(BytesResponse { data: snapshot }))
    }

    async fn export_state_snapshot_at_height(&self, request: Request<Uint64Request>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let snapshot = btc_genz_scl::export_state_snapshot_at_height_raw(req.value);
        Ok(Response::new(BytesResponse { data: snapshot }))
    }

    async fn get_address_type(&self, request: Request<BytesRequest>) -> Result<Response<Int32Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::get_address_type(req.data);
        Ok(Response::new(Int32Response { value }))
    }

    async fn is_valid_fee(&self, request: Request<Uint64Request>) -> Result<Response<BoolResponse>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::is_valid_fee(req.value);
        Ok(Response::new(BoolResponse { value }))
    }

    async fn calculate_nano_fee(&self, request: Request<NanoFeeRequest>) -> Result<Response<Uint64Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::calculate_transaction_fee(req.amount, req.weight);
        Ok(Response::new(Uint64Response { value }))
    }

    async fn get_nano_weight(&self, request: Request<BytesRequest>) -> Result<Response<Uint64Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::get_nano_weight(req.data);
        Ok(Response::new(Uint64Response { value: value as u64 }))
    }

    async fn calculate_tx_hash_with_height(&self, request: Request<CalculateTxHashRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        // [VANGUARD-FIX] Đồng bộ cách băm giao dịch (SegWit Hash) loại bỏ signature tương tự Ledger
        let data = btc_genz_scl::calculate_tx_hash(req.data, req.height);
        Ok(Response::new(BytesResponse { data }))
    }
    
    async fn calculate_tx_hash(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        // [VANGUARD-FIX] Đồng bộ cách băm giao dịch (SegWit Hash) loại bỏ signature tương tự Ledger
        let data = btc_genz_scl::calculate_tx_hash(req.data, 0); 
        Ok(Response::new(BytesResponse { data }))
    }

    async fn calculate_signing_hash(&self, request: Request<CalculateSigningHashRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let tx = req.transaction.unwrap_or_default();
        let data = btc_genz_scl::crypto_primitives::calculate_signing_hash(&tx).to_vec();
        Ok(Response::new(BytesResponse { data }))
    }

    async fn calculate_blake3_hash_with_height(&self, request: Request<CalculateBlake3HashRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let data = btc_genz_scl::crypto_primitives::calculate_blake3_hash(req.data, req.height);
        Ok(Response::new(BytesResponse { data: data.to_vec() }))
    }

    async fn calculate_block_header_hash(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let data = btc_genz_scl::calculate_block_header_hash(req.data);
        Ok(Response::new(BytesResponse { data }))
    }

    async fn get_header_raw(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let res = btc_genz_scl::get_header_raw(req.data);
        let data = res.unwrap_or_default();
        Ok(Response::new(BytesResponse { data }))
    }

    async fn calculate_merkle_root(&self, request: Request<MerkleRootRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        let data = btc_genz_scl::calculate_merkle_root(req.flat_hashes);
        Ok(Response::new(BytesResponse { data }))
    }

    async fn calculate_block_reward_btc_z(&self, request: Request<Uint64Request>) -> Result<Response<Uint64Response>, Status> {
        let req = request.into_inner();
        let value = btc_genz_scl::calculate_block_reward_btc_z(req.value);
        Ok(Response::new(Uint64Response { value }))
    }

    // [V19 UNIFIED STORAGE] Handlers quản lý Sổ cái Nhất thể
    async fn get_block_raw(&self, request: Request<Uint64Request>) -> Result<Response<BytesResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        match btc_genz_scl::get_block_raw(req.value) {
            Some(data) => Ok(Response::new(BytesResponse { data })),
            None => Err(Status::not_found(format!("Block #{} not found", req.value))),
        }
    }



    async fn get_block_hash(&self, request: Request<Uint64Request>) -> Result<Response<BytesResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        let data = btc_genz_scl::get_block_hash(req.value);
        Ok(Response::new(BytesResponse { data }))
    }

    async fn get_raw_by_hash(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        let req = request.into_inner();
        let data = btc_genz_scl::get_raw_by_hash(req.data).unwrap_or_default();
        Ok(Response::new(BytesResponse { data }))
    }

    async fn save_block_raw(&self, request: Request<SaveBlockRequest>) -> Result<Response<BoolResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let req = request.into_inner();
        let success = btc_genz_scl::save_block_raw(req.height, req.hash, req.data, req.is_canonical, vec![]);
        Ok(Response::new(BoolResponse { value: success }))
    }

    async fn delete_by_hash(&self, request: Request<HashRequest>) -> Result<Response<GenericResponse>, Status> {
        if btc_genz_scl::state_manager::get_state_manager().is_none() {
            return Err(Status::unavailable("StateManager not initialized"));
        }
        verify_auth_token(&request)?;
        let req = request.into_inner();
        let success = btc_genz_scl::delete_by_hash_ffi(req.hash);
        Ok(Response::new(GenericResponse { success, error_msg: if success { "".to_string() } else { "Delete failed".to_string() } }))
    }


    async fn add_to_mempool(&self, request: Request<MempoolEntry>) -> Result<Response<GenericResponse>, Status> {
        let req = request.into_inner();
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        match mgr.add_to_mempool(&req.tx_hash, &req.tx_raw) {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => Ok(Response::new(GenericResponse { success: false, error_msg: e.to_string() })),
        }
    }

    async fn add_batch_to_mempool(&self, request: Request<MempoolEntriesResponse>) -> Result<Response<GenericResponse>, Status> {
        let req = request.into_inner();
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        
        // Tại sao: Chuyển đổi dữ liệu từ protobuf sang Vector bytes để gọi ghi theo lô WriteBatch của StateManager
        let entries: Vec<(Vec<u8>, Vec<u8>)> = req.entries.into_iter().map(|e| (e.tx_hash, e.tx_raw)).collect();
        
        match mgr.add_batch_to_mempool(entries) {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => Ok(Response::new(GenericResponse { success: false, error_msg: e.to_string() })),
        }
    }

    async fn remove_from_mempool(&self, request: Request<HashRequest>) -> Result<Response<GenericResponse>, Status> {
        let req = request.into_inner();
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        match mgr.remove_from_mempool(&req.hash) {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => Ok(Response::new(GenericResponse { success: false, error_msg: e.to_string() })),
        }
    }

    async fn remove_from_mempool_batch(&self, request: Request<RemoveBatchFromMempoolRequest>) -> Result<Response<GenericResponse>, Status> {
        let req = request.into_inner();
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        
        // Tại sao: Chuyển tiếp danh sách các hash cần xóa để StateManager thực hiện xóa theo lô WriteBatch
        // trong RocksDB, tránh gọi nhiều lệnh delete_cf tuần tự gây chậm I/O
        match mgr.remove_batch_from_mempool(req.hashes) {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => Ok(Response::new(GenericResponse { success: false, error_msg: e.to_string() })),
        }
    }

    async fn get_mempool_entries(&self, _request: Request<Empty>) -> Result<Response<MempoolEntriesResponse>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let entries = mgr.get_mempool_entries();
        let pb_entries = entries.into_iter().map(|(h, r)| MempoolEntry {
            tx_hash: h.to_vec(),
            tx_raw: r,
        }).collect();
        Ok(Response::new(MempoolEntriesResponse { entries: pb_entries }))
    }

    async fn get_transactions_by_address(&self, request: Request<AddressRequest>) -> Result<Response<TransactionsResponse>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let req = request.into_inner();
        let addr: [u8; 32] = req.address.try_into().map_err(|_| Status::invalid_argument("Invalid address length"))?;
        
        let txs = mgr.get_transactions_by_address(&addr);
        let pb_txs = txs.into_iter().map(|tx| TrackedTx {
            tx_id: tx.tx_id.to_vec(),
            sender: tx.sender.to_vec(),
            receiver: tx.receiver.to_vec(),
            amount: tx.amount,
            fee: tx.fee,
            timestamp: tx.timestamp,
            block_height: tx.block_height,
            nonce: tx.nonce,
            status: tx.status,
            is_finalized: tx.is_finalized,
            confirmations: tx.confirmations,
            error_message: tx.error_message,
            sender_prev_balance: tx.sender_prev_balance,
            sender_post_balance: tx.sender_post_balance,
            receiver_prev_balance: tx.receiver_prev_balance,
            receiver_post_balance: tx.receiver_post_balance,
        }).collect();

        Ok(Response::new(TransactionsResponse { transactions: pb_txs }))
    }



    async fn calculate_actual_total_supply(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        // [VANGUARD-FIX] Sử dụng cache O(1) thay vì quét toàn bộ database O(N) để chống nghẽn I/O khi UI gọi status định kỳ.
        let supply = mgr.get_actual_total_supply();
        Ok(Response::new(Uint64Response { value: supply }))
    }

    async fn get_actual_total_supply(&self, _request: Request<Empty>) -> Result<Response<Uint64Response>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let value = mgr.get_actual_total_supply();
        Ok(Response::new(Uint64Response { value }))
    }


    async fn get_node_config(&self, _request: Request<Empty>) -> Result<Response<ConfigResponse>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let data = mgr.get_node_config().unwrap_or_default();
        Ok(Response::new(ConfigResponse { data }))
    }

    async fn set_node_config(&self, request: Request<ConfigRequest>) -> Result<Response<GenericResponse>, Status> {
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        verify_auth_token(&request)?;
        let req = request.into_inner();
        mgr.set_node_config(&req.data);
        Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() }))
    }

    async fn evaluate_header_chain(&self, request: Request<EvaluateHeaderChainRequest>) -> Result<Response<EvaluateHeaderChainResponse>, Status> {
        let req = request.into_inner();
        let res = btc_genz_scl::evaluate_header_chain(req.headers_raw);
        Ok(Response::new(EvaluateHeaderChainResponse {
            status: res.status,
            fork_point: res.fork_point,
            error_msg: res.error_msg,
        }))
    }

    async fn process_new_block(&self, request: Request<ProcessNewBlockRequest>) -> Result<Response<ProcessNewBlockResponse>, Status> {
        let req = request.into_inner();
        let res = tokio::task::spawn_blocking(move || {
            btc_genz_scl::process_new_block(req.block_raw)
        })
        .await
        .map_err(|e| Status::internal(format!("Tokio spawn blocking failed: {}", e)))?;
        Ok(Response::new(ProcessNewBlockResponse {
            status: res.status,
            error_msg: res.error_msg,
        }))
    }

    async fn process_chain(&self, request: Request<btc_genz_scl::proto::consensus::SyncChainRequest>) -> Result<Response<btc_genz_scl::proto::consensus::SyncChainResponse>, Status> {
        let is_syncing = request.metadata().get("x-is-syncing")
            .and_then(|v| v.to_str().ok())
            .map(|s| s == "true")
            .unwrap_or(false);
            
        let deadline = request.metadata().get("x-deadline")
            .and_then(|v| v.to_str().ok())
            .and_then(|s| s.parse::<u64>().ok())
            .unwrap_or(0);

        let req = request.into_inner();
        let res = tokio::task::spawn_blocking(move || {
            btc_genz_scl::consensus::process_chain(req, is_syncing, deadline)
        })
        .await
        .map_err(|e| Status::internal(format!("Tokio spawn blocking failed: {}", e)))?;
        Ok(Response::new(res))
    }



    async fn reindex_miner_history(&self, request: Request<AddressRequest>) -> Result<Response<GenericResponse>, Status> {
        verify_auth_token(&request)?;
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let req = request.into_inner();
        let addr: [u8; 32] = req.address.try_into().map_err(|_| Status::invalid_argument("Invalid address"))?;
        
        // [VANGUARD-FIX] Chạy reindex_miner_history trong spawn_blocking để không chặn tokio runtime thread
        let mgr_clone = mgr.clone();
        let res = tokio::task::spawn_blocking(move || {
            mgr_clone.reindex_miner_history(&addr);
        }).await;

        match res {
            Ok(_) => Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() })),
            Err(e) => Err(Status::internal(format!("Tokio spawn blocking failed: {}", e))),
        }
    }



    async fn clear_staging_area(&self, request: Request<Empty>) -> Result<Response<GenericResponse>, Status> {
        verify_auth_token(&request)?;
        if let Some(mgr) = btc_genz_scl::state_manager::get_state_manager() {
            mgr.clear_staging_area();
            return Ok(Response::new(GenericResponse { success: true, error_msg: "".to_string() }));
        }
        Ok(Response::new(GenericResponse { success: false, error_msg: "StateManager not initialized".to_string() }))
    }

    async fn validate_transaction_batch(
        &self,
        request: Request<ValidateTxBatchRequest>,
    ) -> Result<Response<ValidateTxBatchResponse>, Status> {
        let req = request.into_inner();
        let mgr = btc_genz_scl::state_manager::get_state_manager()
            .ok_or_else(|| Status::internal("StateManager not initialized"))?;

        let results = tokio::task::spawn_blocking(move || {
            use rayon::prelude::*;
            use btc_genz_scl::proto::transaction::Transaction;
            use btc_genz_scl::crypto_primitives::{calculate_signing_hash, verify_ed25519_signature};

            // Pha 1: Xác thực song song (Chữ ký & định dạng cơ bản) sử dụng Rayon
            let pre_validated: Vec<Result<(Transaction, [u8; 32], [u8; 32], [u8; 64]), (Vec<u8>, u32, String)>> = req
                .raw_txs
                .par_iter()
                .map(|tx_bytes| {
                    let tx = match Transaction::decode(tx_bytes.as_slice()) {
                        Ok(t) => t,
                        Err(_) => {
                            return Err((
                                vec![],
                                101,
                                "Giải mã Protobuf thất bại".to_string(),
                            ));
                        }
                    };

                    // Tính toán tx_hash (signing_hash)
                    let tx_signing_hash = calculate_signing_hash(&tx);

                    let sender_bytes = match &tx.sender {
                        Some(s) if s.value.len() == 32 => {
                            let mut arr = [0u8; 32];
                            arr.copy_from_slice(&s.value);
                            arr
                        }
                        _ => {
                            return Err((
                                tx_signing_hash.to_vec(),
                                102,
                                "Địa chỉ người gửi thiếu hoặc sai định dạng (yêu cầu 32 bytes)".to_string(),
                            ));
                        }
                    };

                    let sig_bytes = match &tx.signature {
                        Some(s) if s.value.len() == 64 => {
                            let mut arr = [0u8; 64];
                            arr.copy_from_slice(&s.value);
                            arr
                        }
                        _ => {
                            return Err((
                                tx_signing_hash.to_vec(),
                                103,
                                "Chữ ký thiếu hoặc sai định dạng (yêu cầu 64 bytes)".to_string(),
                            ));
                        }
                    };

                    // Kiểm tra chữ ký Ed25519
                    if !verify_ed25519_signature(&sender_bytes, &tx_signing_hash, &sig_bytes) {
                        return Err((
                            tx_signing_hash.to_vec(),
                            104,
                            "Chữ ký không hợp lệ".to_string(),
                        ));
                    }

                    Ok((tx, tx_signing_hash, sender_bytes, sig_bytes))
                })
                .collect();

            // Tải trước trạng thái theo lô (Batch State Prefetching)
            // Tại sao: Truy cập cơ sở dữ liệu tuần tự cho từng giao dịch tạo ra cổ chai I/O cực lớn.
            // Bằng cách gom tất cả các địa chỉ và truy xuất trạng thái song song qua Rayon, chúng ta có thể nạp sẵn vào RAM cache.
            let mut unique_addrs = std::collections::HashSet::<[u8; 32]>::new();
            for item in &pre_validated {
                if let Ok((tx, _, sender_bytes, _)) = item {
                    unique_addrs.insert(*sender_bytes);
                    if let Some(ref rec) = tx.receiver {
                        if rec.value.len() == 32 {
                            let mut rec_addr = [0u8; 32];
                            rec_addr.copy_from_slice(&rec.value);
                            unique_addrs.insert(rec_addr);
                        }
                    }
                }
            }

            let unique_addrs_vec: Vec<[u8; 32]> = unique_addrs.into_iter().collect();
            let prefetched_states: Vec<([u8; 32], (u64, u64, bool))> = unique_addrs_vec
                .par_iter()
                .map(|addr| {
                    let state = mgr.get_account_state(addr);
                    let is_new = state.btc_z == 0
                        && state.nonce == 0
                        && state.maturing_rewards.is_empty();
                    (*addr, (state.btc_z, state.nonce, is_new))
                })
                .collect();

            // Pha 2: Xác thực tuần tự trên RAM (Double Spend & Nonce Gap)
            let mut results = Vec::with_capacity(pre_validated.len());
            
            // Cache tạm thời cho Balance và Nonce của các Ví gửi/nhận để trừ lùi / kiểm tra
            // Key: địa chỉ ví [u8; 32] -> Value: (SpendableBalance, ExpectedNonce, IsNewWallet)
            let mut wallet_cache: std::collections::HashMap<[u8; 32], (u64, u64, bool)> = prefetched_states.into_iter().collect();



            for item in pre_validated {
                match item {
                    Err((tx_hash, status_code, error_msg)) => {
                        results.push(TxValidationResult {
                            tx_hash,
                            is_valid: false,
                            status_code,
                            error_msg,
                        });
                    }
                    Ok((tx, tx_signing_hash, sender_bytes, _sig_bytes)) => {
                        let tx_hash_vec = tx_signing_hash.to_vec();

                        // Lấy hoặc khởi tạo cache cho người gửi (bản sao các thông số ra)
                        let (sender_balance, sender_nonce, _) = {
                            if !wallet_cache.contains_key(&sender_bytes) {
                                let state = mgr.get_account_state(&sender_bytes);
                                wallet_cache.insert(sender_bytes, (state.btc_z, state.nonce, false));
                            }
                            *wallet_cache.get(&sender_bytes).unwrap()
                        };

                        // Kiểm tra Nonce
                        if tx.nonce < sender_nonce {
                            results.push(TxValidationResult {
                                tx_hash: tx_hash_vec,
                                is_valid: false,
                                status_code: 105,
                                error_msg: format!(
                                    "Nonce quá thấp: {} (Mong đợi: {})",
                                    tx.nonce, sender_nonce
                                ),
                            });
                            continue;
                        }
                        if tx.nonce != sender_nonce {
                            results.push(TxValidationResult {
                                tx_hash: tx_hash_vec,
                                is_valid: false,
                                status_code: 106,
                                error_msg: format!(
                                    "Nonce không tuần tự (Nonce gap): {} (Mong đợi: {})",
                                    tx.nonce, sender_nonce
                                ),
                            });
                            continue;
                        }

                        // Tính creation fee (phí tạo ví mới) nếu ví nhận chưa tồn tại
                        let mut creation_fee = 0u64;
                        if let Some(ref rec) = tx.receiver {
                            if rec.value.len() == 32 {
                                let mut rec_addr = [0u8; 32];
                                rec_addr.copy_from_slice(&rec.value);

                                // Kiểm tra xem ví nhận có mới tinh không
                                let is_new_rec = {
                                    if !wallet_cache.contains_key(&rec_addr) {
                                        let rec_state = mgr.get_account_state(&rec_addr);
                                        let is_new = rec_state.btc_z == 0
                                            && rec_state.nonce == 0
                                            && rec_state.maturing_rewards.is_empty();
                                        wallet_cache.insert(rec_addr, (rec_state.btc_z, rec_state.nonce, is_new));
                                    }
                                    let (_, _, is_new) = wallet_cache.get(&rec_addr).unwrap();
                                    *is_new
                                };

                                if is_new_rec {
                                    creation_fee = 1000;
                                }
                            }
                        }

                        // Tính tổng chi phí (Amount + Fee + Creation Fee)
                        let total_cost = tx.amount.saturating_add(tx.fee).saturating_add(creation_fee);

                        // Kiểm tra số dư
                        if sender_balance < total_cost {
                            results.push(TxValidationResult {
                                tx_hash: tx_hash_vec,
                                is_valid: false,
                                status_code: 107,
                                error_msg: format!(
                                    "Không đủ số dư: Yêu cầu {} VNT, hiện có {} VNT",
                                    total_cost, sender_balance
                                ),
                            });
                            continue;
                        }

                        // Cập nhật ví nhận (không còn là ví mới nữa sau khi nhận coin)
                        if let Some(ref rec) = tx.receiver {
                            if rec.value.len() == 32 {
                                let mut rec_addr = [0u8; 32];
                                rec_addr.copy_from_slice(&rec.value);
                                if let Some((rec_bal, _, is_new_rec)) = wallet_cache.get_mut(&rec_addr) {
                                    *rec_bal = rec_bal.saturating_add(tx.amount);
                                    *is_new_rec = false; // Đã nhận tiền, không còn mới nữa
                                }
                            }
                        }

                        // Cập nhật ví gửi
                        if let Some((s_bal, s_nonce, _)) = wallet_cache.get_mut(&sender_bytes) {
                            *s_bal = s_bal.saturating_sub(total_cost);
                            *s_nonce = s_nonce.saturating_add(1);
                        }

                        results.push(TxValidationResult {
                            tx_hash: tx_hash_vec,
                            is_valid: true,
                            status_code: 1, // SUCCESS
                            error_msg: "".to_string(),
                        });
                    }
                }
            }
            results
        })
        .await
        .map_err(|e| Status::internal(format!("Tokio spawn blocking failed: {}", e)))?;

        Ok(Response::new(ValidateTxBatchResponse { results }))
    }




    async fn pack_header_v112(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        let req_raw = request.into_inner();
        
        #[derive(BorshDeserialize)]
        struct InternalPackRequest {
            height: u64,
            parent_hash: Vec<u8>,
            timestamp: u64,
            merkle_root: Vec<u8>,
            difficulty: Vec<u8>,
        }

        let req: InternalPackRequest = BorshDeserialize::try_from_slice(&req_raw.data)
            .map_err(|e| Status::invalid_argument(format!("Borsh Decode Failed: {}", e)))?;

        let packed = btc_genz_scl::genz_pow::pack_header_v112(
            req.height,
            &req.parent_hash,
            req.timestamp,
            &req.merkle_root,
            &req.difficulty,
        );
        
        Ok(Response::new(BytesResponse { data: packed.to_vec() }))
    }

    async fn unpack_header_v112(&self, request: Request<BytesRequest>) -> Result<Response<BytesResponse>, Status> {
        let req = request.into_inner();
        if req.data.len() != 112 {
            return Err(Status::invalid_argument("Invalid header length: expected 112 bytes"));
        }

        #[derive(BorshSerialize)]
        struct InternalUnpackResponse {
            height: u64,
            parent_hash: Vec<u8>,
            timestamp: u64,
            merkle_root: Vec<u8>,
            difficulty: Vec<u8>,
        }

        let buf = &req.data;
        let height = u64::from_le_bytes(buf[0..8].try_into().unwrap());
        let parent_hash = buf[8..40].to_vec();
        let timestamp = u64::from_le_bytes(buf[40..48].try_into().unwrap());
        let merkle_root = buf[48..80].to_vec();
        let difficulty = buf[80..112].to_vec();

        let res = InternalUnpackResponse {
            height,
            parent_hash,
            timestamp,
            merkle_root,
            difficulty,
        };

        let data = borsh::to_vec(&res).unwrap_or_default();
        Ok(Response::new(BytesResponse { data }))
    }

    async fn is_new_wallet_detailed(&self, request: Request<BalanceRequest>) -> Result<Response<BoolResponse>, Status> {
        let req = request.into_inner();
        let addr: [u8; 32] = req.address.try_into().map_err(|_| Status::invalid_argument("Invalid address length"))?;
        
        let mgr = btc_genz_scl::state_manager::get_state_manager().ok_or_else(|| Status::internal("StateManager not initialized"))?;
        let state = mgr.get_account_state(&addr);
        let value = state.btc_z == 0 && state.nonce == 0;
        
        Ok(Response::new(BoolResponse { value }))
    }

    async fn calculate_tx_hashes_batch(
        &self,
        request: Request<BatchTxHashRequest>,
    ) -> Result<Response<BatchTxHashResponse>, Status> {
        let req = request.into_inner();
        
        // Tại sao thiết kế như vậy: Sử dụng spawn_blocking để băm song song hàng chục ngàn giao dịch bằng Rayon
        // trên ThreadPool của Rayon bên ngoài async runtime, ngăn chặn việc chiếm dụng luồng worker async của Tokio.
        let hashes = tokio::task::spawn_blocking(move || {
            use rayon::prelude::*;
            req.raw_txs.par_iter().map(|tx_bytes| {
                btc_genz_scl::calculate_tx_hash(tx_bytes.clone(), req.height)
            }).collect()
        }).await.map_err(|e| Status::internal(format!("Lỗi spawn_blocking: {}", e)))?;

        Ok(Response::new(BatchTxHashResponse { hashes }))
    }

    type WatchCoreEventsStream = ReceiverStream<Result<CoreEvent, Status>>;

    async fn watch_core_events(
        &self,
        request: Request<Empty>,
    ) -> Result<Response<Self::WatchCoreEventsStream>, Status> {
        verify_auth_token(&request)?;
        log::info!("[SCL-SERVER] 📡 Go Node đã kết nối vào Kênh Báo Động (Event Stream).");

        let mut rx = CORE_EVENT_TX.subscribe();
        let (tx, stream_rx) = tokio::sync::mpsc::channel(128);

        // Sinh 1 task chạy ngầm đẩy dữ liệu từ Broadcast channel sang gRPC Stream
        tokio::spawn(async move {
            loop {
                match rx.recv().await {
                    Ok(event) => {
                        if tx.send(Ok(event)).await.is_err() {
                            break; // Go Client đã ngắt kết nối
                        }
                    }
                    Err(broadcast::error::RecvError::Lagged(_)) => {
                        // Go đọc quá chậm, bị trôi mất event. Bỏ qua.
                        continue;
                    }
                    Err(broadcast::error::RecvError::Closed) => {
                        break;
                    }
                }
            }
            log::info!("[SCL-SERVER] 🔌 Go Node ngắt kết nối khỏi Kênh Báo Động.");
        });

        Ok(Response::new(ReceiverStream::new(stream_rx)))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    if std::env::var("RUST_LOG").is_err() {
        std::env::set_var("RUST_LOG", "info");
    }
    env_logger::init();
    
    // [FIREWALL-TRACE] Bẫy Panic để tóm gọn mọi sự cố đột tử của lõi Rust
    std::panic::set_hook(Box::new(|info| {
        use std::io::Write;
        let mut stderr = std::io::stderr();
        let _ = writeln!(stderr, "\n[SCL-SERVER] ☢️ HOẢNG LOẠN (PANIC) ĐÃ XẢY RA!");
        
        if let Some(s) = info.payload().downcast_ref::<&str>() {
            let _ = writeln!(stderr, "[SCL-SERVER] ❌ Thông điệp: {}", s);
        } else if let Some(s) = info.payload().downcast_ref::<String>() {
            let _ = writeln!(stderr, "[SCL-SERVER] ❌ Thông điệp: {}", s);
        } else {
            let _ = writeln!(stderr, "[SCL-SERVER] ❌ Thông điệp: <kiểu dữ liệu không xác định>");
        }
        
        if let Some(loc) = info.location() {
            let _ = writeln!(stderr, "[SCL-SERVER] 📍 Tại: {}:{}:{}", loc.file(), loc.line(), loc.column());
        }
        // [VANGUARD-SAFE] Tuyệt đối không gọi println!/eprintln! ở đây vì có thể gây panic bồi nếu pipe bị đóng.
    }));
    
    let args: Vec<String> = std::env::args().collect();
    println!("[SCL-SERVER] 🛠️ Tham số đầu vào: {:?}", args);

    // [SECURITY-HARDENING] gRPC Auth Token được cấp qua biến môi trường SCL_FORCE_TOKEN.
    // Đã gỡ bỏ println! in token ra stdout để bảo vệ an toàn cho hệ thống.
    println!("[SCL-SERVER] 🔐 Cơ chế bảo mật Auth Token đã được kích hoạt.");
    let socket_path: Option<&String> = args.iter().position(|r| r == "--socket")
        .and_then(|i| args.get(i + 1));

    let target_port = args.iter().position(|r| r == "--port")
        .and_then(|i| args.get(i + 1))
        .and_then(|p: &String| p.parse::<u16>().ok())
        .unwrap_or(50051);

    let scl_service = MySclService {};

    if let Some(path) = socket_path {
        println!("[SCL-SERVER] 🚀 Rust gRPC Server starting on IPC: {}", path);
        
        #[cfg(unix)]
        {
            use tokio::net::UnixListener;
            use tokio_stream::wrappers::UnixListenerStream;
            
            let _ = std::fs::remove_file(path); // Dọn dẹp socket cũ
            let uds = UnixListener::bind(path)?;
            let uds_stream = UnixListenerStream::new(uds);
            
            Server::builder()
                .add_service(SclServiceServer::new(scl_service)
                    .max_decoding_message_size(512 * 1024 * 1024) // Đồng bộ giới hạn 512MB để tránh lỗi khi xử lý khối lớn trên Unix IPC
                    .max_encoding_message_size(512 * 1024 * 1024)) // Đồng bộ giới hạn 512MB để tránh lỗi khi xử lý khối lớn trên Unix IPC
                .serve_with_incoming(uds_stream)
                .await?;
        }

        #[cfg(windows)]
        {
            use tokio::net::windows::named_pipe::ServerOptions;
            use tokio::sync::mpsc;
            use tokio_stream::wrappers::ReceiverStream;
            use std::task::{Context, Poll};
            use std::pin::Pin;
            use tokio::io::{AsyncRead, AsyncWrite, ReadBuf};

            struct NamedPipeConnection {
                pipe: tokio::net::windows::named_pipe::NamedPipeServer,
            }

            impl tonic::transport::server::Connected for NamedPipeConnection {
                type ConnectInfo = ();
                fn connect_info(&self) -> Self::ConnectInfo {
                    ()
                }
            }

            impl AsyncRead for NamedPipeConnection {
                fn poll_read(mut self: Pin<&mut Self>, cx: &mut Context<'_>, buf: &mut ReadBuf<'_>) -> Poll<std::io::Result<()>> {
                    Pin::new(&mut self.pipe).poll_read(cx, buf)
                }
            }
            impl AsyncWrite for NamedPipeConnection {
                fn poll_write(mut self: Pin<&mut Self>, cx: &mut Context<'_>, buf: &[u8]) -> Poll<std::io::Result<usize>> {
                    Pin::new(&mut self.pipe).poll_write(cx, buf)
                }
                fn poll_flush(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<std::io::Result<()>> {
                    Pin::new(&mut self.pipe).poll_flush(cx)
                }
                fn poll_shutdown(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<std::io::Result<()>> {
                    Pin::new(&mut self.pipe).poll_shutdown(cx)
                }
            }

            let (tx, rx) = mpsc::channel::<Result<NamedPipeConnection, std::io::Error>>(16);
            let path_clone: String = path.clone();

            // [PRODUCTION ACCEPTOR LOOP] 
            // Chạy ngầm để liên tục tạo pipe instance và đợi client kết nối một cách Async.
            tokio::spawn(async move {
                let mut is_first = true;
                loop {
                    let server_res = ServerOptions::new()
                        .first_pipe_instance(is_first)
                        .in_buffer_size(64 * 1024 * 1024)  // 64MB buffer cho khối dữ liệu lớn
                        .out_buffer_size(64 * 1024 * 1024)
                        .create(&path_clone);

                    match server_res {
                        Ok(server) => {
                            // Đợi Go kết nối - Hoàn toàn không nghẽn Executor
                            if let Ok(_) = server.connect().await {
                                let _ = tx.send(Ok(NamedPipeConnection { pipe: server })).await;
                                is_first = false;
                            }
                        }
                        Err(e) => {
                            eprintln!("[SCL-SERVER] ❌ Lỗi tạo Pipe Instance: {}", e);
                            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
                        }
                    }
                }
            });

            Server::builder()
                .layer(ConcurrencyLimitLayer::new(1024)) // Tại sao: Nâng giới hạn gRPC requests đồng thời lên 1024 để giải phóng năng lực xử lý cho 6 luồng gRPC mới từ Go
                .add_service(SclServiceServer::new(scl_service)
                    .max_decoding_message_size(512 * 1024 * 1024) // Tại sao: Giữ nguyên giới hạn 512MB để đảm bảo quy trình Reorg/Sync khối lượng lớn không bị lỗi
                    .max_encoding_message_size(512 * 1024 * 1024))
                .serve_with_incoming(ReceiverStream::new(rx))
                .await?;
        }
    } else {
        let addr: std::net::SocketAddr = format!("127.0.0.1:{}", target_port).parse()?;
        println!("[SCL-SERVER] 🚀 Rust gRPC Server starting on TCP: {}", addr);

        Server::builder()
            .layer(ConcurrencyLimitLayer::new(1024)) // Tại sao: Nâng giới hạn concurrency lên 1024 để bảo vệ tài nguyên tốt hơn dưới tải 6 luồng song song
            .add_service(SclServiceServer::new(scl_service)
                .max_decoding_message_size(512 * 1024 * 1024) // Tại sao: Đồng bộ giới hạn 512MB
                .max_encoding_message_size(512 * 1024 * 1024))
            .serve(addr)
            .await?;
    }

    Ok(())
}
