/**
 * @file fee_logic.rs
 * @brief Logic tính toán phí giao dịch (YonaCode V1.3 - PHỤ LỤC H).
 * @details Cấu trúc Phí 3 Tầng Cố định: Standard (250), Priority (500), VIP (1000) VNT.
 * .
 * 
 * @author Vô Nhật Thiên (Cập nhật) - YonaCode V1.3
 * @date 2026-04-03
 */

pub const FEE_STANDARD: u64 = 250;
pub const FEE_PRIORITY: u64 = 500;
pub const FEE_VIP: u64 = 1000;

/// [V1.3] Kiểm tra xem mức phí có thuộc 3 tầng quy định không
pub fn is_valid_fee(fee: u64) -> bool {
    fee == FEE_STANDARD || fee == FEE_PRIORITY || fee == FEE_VIP
}

/// [V1.19 FINAL] Tính toán phí giao dịch cố định (Minimalist Economics)
pub fn calculate_transaction_fee(_amount: u64, weight: u32) -> u64 {
    // V1.19: Đơn giản hóa tuyệt đối. Lựa chọn tầng phí dựa trên trọng lượng yêu cầu.
    // Nếu trọng lượng không được chỉ định hoặc không khớp, mặc định dùng mức Standard (250).
    if weight >= 1000 {
        FEE_VIP
    } else if weight >= 500 {
        FEE_PRIORITY
    } else {
        FEE_STANDARD
    }
}
