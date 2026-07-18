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

/**
 * formatDifficulty: Định dạng Độ khó mục tiêu (U256 Target) hoặc độ khó thường
 * thành chuỗi rút gọn dễ đọc dạng tài chính Terahash (T), Gigahash (G), Megahash (M)...
 */
export const formatDifficulty = (diff: string | number): string => {
  const diffStr = diff.toString().trim();
  if (diffStr.length > 15) {
    try {
      let target: bigint;
      // Kiểm tra xem có phải định dạng Target Hex (256-bit Little-Endian) không
      const cleanHex = diffStr.replace(/^0x/, "");
      const isHex = /^[0-9a-fA-F]+$/.test(cleanHex);
      if (isHex && (diffStr.startsWith("0x") || diffStr.length === 64)) {
        // Đảo ngược byte từ Little-Endian sang Big-Endian
        const bytes = cleanHex.match(/.{1,2}/g);
        const targetHex = bytes ? bytes.reverse().join("") : cleanHex;
        target = BigInt("0x" + targetHex);
      } else {
        target = BigInt(diffStr);
      }

      if (target === 0n) return "0";
      // MaxTarget = 2^256 - 1
      const maxTarget = (1n << 256n) - 1n;
      const actualDiff = maxTarget / target;
      
      const num = Number(actualDiff);
      if (num >= 1e12) {
        return `${(num / 1e12).toFixed(2)} T`;
      }
      if (num >= 1e9) {
        return `${(num / 1e9).toFixed(2)} G`;
      }
      if (num >= 1e6) {
        return `${(num / 1e6).toFixed(2)} M`;
      }
      if (num >= 1e3) {
        return `${(num / 1e3).toFixed(2)} K`;
      }
      return num.toLocaleString('en-US');
    } catch (e) {
      return diffStr;
    }
  }
  
  const num = Number(diff);
  if (isNaN(num)) return diffStr;
  if (num >= 1e12) {
    return `${(num / 1e12).toFixed(2)} T`;
  }
  if (num >= 1e9) {
    return `${(num / 1e9).toFixed(2)} G`;
  }
  if (num >= 1e6) {
    return `${(num / 1e6).toFixed(2)} M`;
  }
  if (num >= 1e3) {
    return `${(num / 1e3).toFixed(2)} K`;
  }
  return num.toLocaleString('en-US');
};


