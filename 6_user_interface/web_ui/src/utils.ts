/**
 * @file utils.ts
 * @brief Tiện ích hệ thống BTC GenZ - Tuyệt đối không làm tròn
 */

/**
 * formatBtcZ: Chuyển đổi VNT (uint64) sang BTC_Z với đúng 8 chữ số thập phân.
 * Tuyệt đối không dùng phép chia số thực để tránh sai số làm tròn (Floating Point Error).
 */
export const formatBtcZ = (vnt: number | string | bigint): string => {
  const bn = BigInt(vnt);
  const s = bn.toString().padStart(9, '0');
  const integerPart = s.slice(0, -8) || '0';
  const decimalPart = s.slice(-8);
  
  // Ép định dạng hàng ngàn dùng dấu phẩy (Standard Financial Format)
  const formattedInteger = Number(integerPart).toLocaleString('en-US');
    
  return `${formattedInteger}.${decimalPart}`;
};

/**
 * formatVnt: Hiển thị đơn vị VNT thô cho người dùng đối soát.
 */
export const formatVnt = (vnt: number | string | bigint): string => {
  return BigInt(vnt).toLocaleString('en-US');
};

/**
 * formatTime: Hiển thị thời gian (HH:mm:ss) từ Unix Timestamp (seconds).
 */
export const formatTime = (ts: number): string => {
  if (!ts) return "--:--:--";
  const date = new Date(ts * 1000);
  return date.toLocaleTimeString('vi-VN', { hour12: false });
};

/**
 * formatDate: Hiển thị ngày tháng (DD/MM/YYYY) từ Unix Timestamp (seconds).
 */
export const formatDate = (ts: number): string => {
  if (!ts) return "--/--/----";
  const date = new Date(ts * 1000);
  return date.toLocaleDateString('vi-VN');
};
