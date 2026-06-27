/**
 * @file state_manager_tests.rs
 * @brief Kiểm thử bộ quản lý trạng thái và sổ cái (StateManager & Ledger) cho BTC GenZ.
 * @details Sử dụng RocksDB sandbox qua tempfile để kiểm toán tính nhất quán, rollback,
 *          maturation và purge mà không ảnh hưởng tới dữ liệu chạy chính thức.
 */

use btc_genz_scl::state_manager::{
    StateManager, AccountState, MaturingReward,
    CF_ACC, CF_ACC_HISTORY, CF_BLOCK_BODIES, CF_RECEIPTS, CF_BLOCK_TXS, CF_TOUCHED_ACCS
};
use btc_genz_scl::AccountSnapshot;
use tempfile::tempdir;
use borsh::{BorshSerialize, BorshDeserialize};
use std::sync::Arc;

/// Hàm tiện ích khởi tạo StateManager trong một RocksDB sandbox tạm thời
fn create_test_state_manager() -> (Arc<StateManager>, tempfile::TempDir) {
    let tmp_dir = tempdir().unwrap();
    let db_path = tmp_dir.path().to_str().unwrap().to_string();
    let mgr = StateManager::try_new(db_path).expect("Khởi tạo StateManager tạm thời thất bại");
    (mgr, tmp_dir)
}

/// 1. Kiểm thử tính Tất định (Determinism) của cây Merkle JMT
/// Thao tác cập nhật trạng thái với các thứ tự xáo trộn khác nhau phải sinh ra cùng một State Root.
#[test]
fn test_state_determinism() {
    let (mgr, _tmp) = create_test_state_manager();

    // Chuẩn bị dữ liệu tài khoản
    let addr_a = [1u8; 32];
    let addr_b = [2u8; 32];
    let addr_c = [3u8; 32];

    let state_a = AccountState {
        btc_z: 1000,
        nonce: 1,
        nano_weight: 10,
        coin_id: [0u8; 32],
        last_full_cleanup: 0,
        maturing_rewards: vec![],
    };
    let state_b = AccountState {
        btc_z: 5000,
        nonce: 2,
        nano_weight: 20,
        coin_id: [0u8; 32],
        last_full_cleanup: 0,
        maturing_rewards: vec![],
    };
    let state_c = AccountState {
        btc_z: 9000,
        nonce: 3,
        nano_weight: 30,
        coin_id: [0u8; 32],
        last_full_cleanup: 0,
        maturing_rewards: vec![],
    };

    let data_a = borsh::to_vec(&state_a).unwrap();
    let data_b = borsh::to_vec(&state_b).unwrap();
    let data_c = borsh::to_vec(&state_c).unwrap();

    // Thứ tự 1: A, B, C
    let batch_1 = vec![
        (addr_a, data_a.clone()),
        (addr_b, data_b.clone()),
        (addr_c, data_c.clone()),
    ];

    // Thứ tự 2: C, A, B
    let batch_2 = vec![
        (addr_c, data_c.clone()),
        (addr_a, data_a.clone()),
        (addr_b, data_b.clone()),
    ];

    // Chạy với lô thứ nhất
    let root_1 = mgr.consolidate_smt_batch_at_version(batch_1, 1);

    // Xóa sạch trạng thái để chạy lô thứ hai trên cùng một version
    mgr.reset_state_completely().unwrap();

    // Chạy với lô thứ hai
    let root_2 = mgr.consolidate_smt_batch_at_version(batch_2, 1);

    // Xác nhận hai State Root hoàn toàn giống nhau bất kể thứ tự nạp đầu vào
    assert_ne!(root_1, [0u8; 32], "Root không được là mảng rỗng");
    assert_eq!(root_1, root_2, "State Root phải giống nhau (Tính tất định Merkle Tree)");
}

/// 2. Kiểm thử đảo ngược trạng thái (Rollback/Reorg) khi rẽ nhánh chuỗi
/// Sau khi rollback, số dư và nonce của các ví liên quan phải quay về mốc ban đầu chính xác.
#[test]
fn test_state_rollback() {
    let (mgr, _tmp) = create_test_state_manager();

    // Thiết lập lowest_full_height bằng 0 để tránh bị nhận diện nhầm là pruned
    let meta_cf = mgr.db.cf_handle("meta").expect("Missing META CF");
    mgr.db.put_cf(meta_cf, b"lowest_full_height", 0u64.to_le_bytes()).unwrap();

    let addr_miner = [11u8; 32];
    let addr_user = [12u8; 32];

    // Trạng thái ban đầu tại Block 1 (Miner có 50 VNT, User có 10 VNT)
    let state_miner_v1 = AccountState {
        btc_z: 50_000_000, // 50 VNT (Zatoshi)
        nonce: 0,
        ..Default::default()
    };
    let state_user_v1 = AccountState {
        btc_z: 10_000_000, // 10 VNT
        nonce: 0,
        ..Default::default()
    };

    let batch_v1 = vec![
        (addr_miner, borsh::to_vec(&state_miner_v1).unwrap()),
        (addr_user, borsh::to_vec(&state_user_v1).unwrap()),
    ];
    mgr.consolidate_smt_batch(batch_v1, 1);
    
    // Đăng ký block 1 để lọt qua Healing / Rollback checks
    mgr.put_block_hash(1, &[1u8; 32]);

    // Trạng thái tại Block 2 (Miner chuyển 20 VNT cho User, Miner tốn 1 nonce)
    let state_miner_v2 = AccountState {
        btc_z: 30_000_000, // 30 VNT
        nonce: 1,
        ..Default::default()
    };
    let state_user_v2 = AccountState {
        btc_z: 30_000_000, // 30 VNT
        nonce: 0,
        ..Default::default()
    };

    let batch_v2 = vec![
        (addr_miner, borsh::to_vec(&state_miner_v2).unwrap()),
        (addr_user, borsh::to_vec(&state_user_v2).unwrap()),
    ];
    mgr.consolidate_smt_batch(batch_v2, 2);
    mgr.put_block_hash(2, &[2u8; 32]);

    // Đăng ký danh sách địa chỉ thay đổi ở Block 2 phục vụ reorg rollback
    mgr.put_block_transactions(2, vec![], vec![addr_miner, addr_user]);

    // Xác thực trạng thái tại đỉnh Block 2
    let cur_miner = mgr.get_account_state(&addr_miner);
    let cur_user = mgr.get_account_state(&addr_user);
    assert_eq!(cur_miner.btc_z, 30_000_000);
    assert_eq!(cur_miner.nonce, 1);
    assert_eq!(cur_user.btc_z, 30_000_000);

    // Kích hoạt Rollback từ Block 2 về Block 1
    mgr.rollback_state(2, 1).expect("Rollback thất bại");

    // Xác nhận trạng thái đã quay về Block 1 chính xác
    let rolled_miner = mgr.get_account_state(&addr_miner);
    let rolled_user = mgr.get_account_state(&addr_user);

    assert_eq!(rolled_miner.btc_z, 50_000_000, "Số dư Miner phải quay về 50 VNT");
    assert_eq!(rolled_miner.nonce, 0, "Nonce Miner phải quay về 0");
    assert_eq!(rolled_user.btc_z, 10_000_000, "Số dư User phải quay về 10 VNT");
}

/// 3. Kiểm thử Tường lửa Bất biến (Finality Firewall)
/// Xác nhận hệ thống chủ động chặn đứng mọi nỗ lực rollback sâu hơn mốc finalized_height.
#[test]
fn test_finality_firewall() {
    let (mgr, _tmp) = create_test_state_manager();

    // Giả thiết đỉnh hiện tại là Block 12, mốc finalized chốt tại Block 10
    mgr.put_block_hash(10, &[10u8; 32]);
    let meta_cf = mgr.db.cf_handle("meta").expect("Missing META CF");
    mgr.db.put_cf(meta_cf, b"jmt_v", 12u64.to_le_bytes()).unwrap();
    mgr.current_version.store(12, std::sync::atomic::Ordering::SeqCst);

    mgr.force_set_finalized_height(10);
    assert_eq!(mgr.get_finalized_height(), 10);

    // Thử rollback về Block 9 (vi phạm vì < 10)
    let res = mgr.rollback_state(12, 9);
    assert!(res.is_err(), "Hệ thống bắt buộc phải báo lỗi khi rollback sâu hơn mốc finalized");
    
    let err_msg = res.err().unwrap().to_string();
    assert!(
        err_msg.contains("ERR_FINALITY_VIOLATION"),
        "Thông điệp lỗi phải chứa mã bảo mật ERR_FINALITY_VIOLATION"
    );
}

/// 4. Kiểm thử Trưởng thành phần thưởng (Reward Maturation)
/// Miner reward chưa thể sử dụng (btc_z chưa cộng) nếu block height chưa đạt đủ độ trễ.
#[test]
fn test_reward_maturation() {
    let (mgr, _tmp) = create_test_state_manager();
    let addr_miner = [99u8; 32];

    // Tạo trạng thái ví miner có 10 VNT sẵn, cộng thêm phần thưởng 50 VNT chờ chín tại height 6
    let state = AccountState {
        btc_z: 10_000_000, // 10 VNT số dư khả dụng
        nonce: 0,
        maturing_rewards: vec![MaturingReward {
            amount: 50_000_000, // 50 VNT phần thưởng miner
            height: 6,
        }],
        ..Default::default()
    };

    // Ghi nhận trạng thái tại height 5
    mgr.update_account_at_height(&addr_miner, state, 5);

    // Truy vấn khi blockchain ở height 5
    let miner_state_h5 = mgr.get_account_state(&addr_miner);
    assert_eq!(
        miner_state_h5.btc_z, 10_000_000,
        "Tại height 5, số dư khả dụng chỉ được là 10 VNT (phần thưởng ở height 6 chưa chín)"
    );
    assert_eq!(miner_state_h5.maturing_rewards.len(), 1, "Giữ nguyên hàng chờ chín");

    // Mock chuyển tiếp blockchain sang height 6 (bằng cách cập nhật current version)
    // Để thực hiện việc này, ta có thể ghi đĩa phiên bản meta hoặc cập nhật trực tiếp qua ghi đĩa dummy.
    let meta_cf = mgr.db.cf_handle("meta").expect("Missing META CF");
    mgr.db.put_cf(meta_cf, b"jmt_v", 6u64.to_le_bytes()).unwrap();
    mgr.current_version.store(6, std::sync::atomic::Ordering::SeqCst);

    // Truy vấn lại khi blockchain đạt height 6
    let miner_state_h6 = mgr.get_account_state(&addr_miner);
    assert_eq!(
        miner_state_h6.btc_z, 60_000_000,
        "Tại height 6, số dư khả dụng phải tăng lên 60 VNT (đã giải ngân phần thưởng miner)"
    );
    assert!(miner_state_h6.maturing_rewards.is_empty(), "Hàng chờ phần thưởng chín phải được làm sạch");
}

/// 5. Kiểm thử Đại thanh trừng lịch sử (Great Purge)
/// Chạy lệnh xóa dữ liệu lịch sử và kiểm tra xem cây trạng thái JMT và số dư phẳng có bị hỏng hay không.
#[test]
fn test_great_purge() {
    let (mgr, _tmp) = create_test_state_manager();

    let addr_user = [77u8; 32];
    let tx_id = [255u8; 32];

    // Ghi nhận trạng thái ví ở height 5 (User có 100 VNT)
    let state = AccountState {
        btc_z: 100_000_000,
        nonce: 5,
        ..Default::default()
    };
    mgr.update_account_at_height(&addr_user, state, 5);

    // Ghi dữ liệu lịch sử thô (Headers, Bodies, Block transactions, Touched accounts, Receipts)
    let block_hash = [111u8; 32];
    mgr.put_block_hash(5, &block_hash);
    mgr.put_header(&block_hash, vec![0u8; 80]); // Mock header raw
    mgr.put_block_body(&block_hash, vec![0u8; 200]); // Mock body raw
    mgr.put_block_transactions(5, vec![tx_id], vec![addr_user]);
    mgr.put_transaction_receipt(&tx_id, 5, 1); // Confirmed status

    // Chạy Great Purge để thanh lý lịch sử khối 5
    mgr.purge_historical_data(5, 5).expect("Đại thanh trừng thất bại");

    // Kiểm toán: Các dữ liệu lịch sử trung gian phải bị xóa
    let body_cf = mgr.db.cf_handle(CF_BLOCK_BODIES).unwrap();
    let receipt_cf = mgr.db.cf_handle(CF_RECEIPTS).unwrap();
    let txs_cf = mgr.db.cf_handle(CF_BLOCK_TXS).unwrap();
    let touched_cf = mgr.db.cf_handle(CF_TOUCHED_ACCS).unwrap();
    let hist_cf = mgr.db.cf_handle(CF_ACC_HISTORY).unwrap();

    assert!(mgr.db.get_cf(body_cf, &block_hash).unwrap().is_none(), "Block Body phải bị xóa");
    assert!(mgr.db.get_cf(receipt_cf, &tx_id).unwrap().is_none(), "Transaction Receipt phải bị xóa");
    assert!(mgr.db.get_cf(txs_cf, 5u64.to_le_bytes()).unwrap().is_none(), "Block TXs mapping phải bị xóa");
    assert!(mgr.db.get_cf(touched_cf, 5u64.to_le_bytes()).unwrap().is_none(), "Touched Accounts list phải bị xóa");

    // Kiểm tra versioned key trong ACC_HISTORY cũng phải bị quét sạch
    let key_hash = jmt::KeyHash::with::<blake3::Hasher>(&addr_user);
    let mut versioned_key = [0u8; 40];
    versioned_key[0..32].copy_from_slice(&key_hash.0);
    versioned_key[32..40].copy_from_slice(&5u64.to_be_bytes());
    assert!(mgr.db.get_cf(hist_cf, &versioned_key).unwrap().is_none(), "Account History versioned record phải bị xóa");

    // ĐẶC BIỆT QUAN TRỌNG: Trạng thái phẳng và cây Merkle JMT hiện tại phải hoạt động bình thường!
    let current_state = mgr.get_account_state(&addr_user);
    assert_eq!(
        current_state.btc_z, 100_000_000,
        "Số dư phẳng của tài khoản phải được bảo toàn nguyên vẹn sau purge"
    );
    assert_eq!(current_state.nonce, 5);
}

/// 6. Kiểm thử xuất và nhập Bản chụp Trạng thái (Snapshot Import/Export)
/// Xác nhận việc tạo bản chụp trạng thái (snapshot), lưu vào file và khôi phục hoạt động bình thường.
#[test]
fn test_snapshot_import_export() {
    let (mgr, _tmp) = create_test_state_manager();

    let addr_a = [1u8; 32];
    let addr_b = [2u8; 32];

    // Trạng thái ví ban đầu của A và B
    let state_a = AccountState {
        btc_z: 10_000_000,
        nonce: 1,
        nano_weight: 10,
        coin_id: [0u8; 32],
        last_full_cleanup: 0,
        maturing_rewards: vec![],
    };

    let state_b = AccountState {
        btc_z: 20_000_000,
        nonce: 2,
        nano_weight: 20,
        coin_id: [0u8; 32],
        last_full_cleanup: 0,
        maturing_rewards: vec![],
    };

    // Nạp trạng thái tại height 1
    let batch = vec![
        (addr_a, borsh::to_vec(&state_a).unwrap()),
        (addr_b, borsh::to_vec(&state_b).unwrap()),
    ];
    mgr.consolidate_smt_batch(batch, 1);
    mgr.put_block_hash(1, &[1u8; 32]);
    mgr.force_set_finalized_height(1);

    // Thiết lập lowest_full_height bằng 0 để tránh bị nhận diện nhầm là pruned
    let meta_cf = mgr.db.cf_handle("meta").expect("Missing META CF");
    mgr.db.put_cf(meta_cf, b"lowest_full_height", 0u64.to_le_bytes()).unwrap();

    // Xuất snapshot
    let snapshot = mgr.export_state_snapshot_at_version(1);
    assert_eq!(snapshot.len(), 2, "Snapshot phải chứa 2 tài khoản");

    // Xác nhận thông tin tài khoản trong snapshot
    let acc_a = snapshot.iter().find(|acc| acc.address == addr_a).expect("Thiếu tài khoản A trong snapshot");
    assert_eq!(acc_a.balance, 10_000_000);
    assert_eq!(acc_a.nonce, 1);

    let acc_b = snapshot.iter().find(|acc| acc.address == addr_b).expect("Thiếu tài khoản B trong snapshot");
    assert_eq!(acc_b.balance, 20_000_000);
    assert_eq!(acc_b.nonce, 2);

    // Thử nghiệm xuất trực tiếp ra file (Streaming)
    let temp_file_dir = tempdir().unwrap();
    let file_path = temp_file_dir.path().join("snapshot.bin").to_str().unwrap().to_string();
    let count = mgr.export_state_snapshot_to_file(&file_path, 1).expect("Xuất snapshot ra file thất bại");
    assert_eq!(count, 2, "Số lượng tài khoản ghi ra file không khớp");

    // Khởi tạo một StateManager sạch để nạp snapshot
    let (mgr_new, _tmp_new) = create_test_state_manager();
    // Bắt buộc phải có block hash của block 1 ở DB mới để vượt qua validation khi rebuild/import
    mgr_new.put_block_hash(1, &[1u8; 32]);

    // Nhập snapshot
    let imported_root = mgr_new.import_state_snapshot(snapshot, 1).expect("Nạp snapshot thất bại");
    assert_ne!(imported_root, [0u8; 32], "Imported root không được là rỗng");

    // Kiểm tra tài khoản sau khi nhập snapshot ở DB mới
    let state_a_new = mgr_new.get_account_state(&addr_a);
    assert_eq!(state_a_new.btc_z, 10_000_000, "Số dư tài khoản A sau nạp snapshot không khớp");
    assert_eq!(state_a_new.nonce, 1);

    let state_b_new = mgr_new.get_account_state(&addr_b);
    assert_eq!(state_b_new.btc_z, 20_000_000, "Số dư tài khoản B sau nạp snapshot không khớp");
    assert_eq!(state_b_new.nonce, 2);
}
