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
 * formatBigIntWithCommas: Định dạng BigInt hoặc chuỗi số thành số nguyên đầy đủ có dấu phẩy phân cách (ví dụ 2,146,578,328,639)
 * Tuyệt đối không dùng ký hiệu khoa học (e+52) và không viết tắt T / G / M / K.
 */
export const formatBigIntWithCommas = (val: bigint | string | number): string => {
  try {
    let str = "";
    if (typeof val === "bigint") {
      str = val.toString();
    } else {
      const s = val.toString().trim();
      if (s.includes("e") || s.includes("E")) {
        const n = Number(s);
        if (!isNaN(n) && isFinite(n)) {
          str = BigInt(Math.round(n)).toString();
        } else {
          str = s;
        }
      } else {
        str = BigInt(s).toString();
      }
    }
    return str.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  } catch (e) {
    const num = Number(val);
    if (!isNaN(num)) {
      return Math.round(num).toLocaleString('en-US');
    }
    return val.toString();
  }
};

/**
 * formatDifficulty: Định dạng Độ khó mục tiêu (U256 Target) hoặc độ khó thường
 * thành con số nguyên chính xác đầy đủ có dấu phân cách hàng ngàn (en-US format).
 */
export const formatDifficulty = (diff: string | number): string => {
  if (diff === undefined || diff === null || diff === "") return "0";
  const diffStr = diff.toString().trim();

  try {
    const cleanHex = diffStr.replace(/^0x/, "");
    const isHex = /^[0-9a-fA-F]+$/.test(cleanHex);

    if (isHex && (diffStr.startsWith("0x") || diffStr.length === 64)) {
      // Định dạng Target Hex (256-bit Little-Endian)
      const bytes = cleanHex.match(/.{1,2}/g);
      const targetHex = bytes ? bytes.reverse().join("") : cleanHex;
      const target = BigInt("0x" + targetHex);

      if (target === 0n) return "0";
      const maxTarget = (1n << 256n) - 1n;
      const actualDiff = maxTarget / target;

      return formatBigIntWithCommas(actualDiff);
    }

    // Định dạng chuỗi số hoặc number nguyên bản
    return formatBigIntWithCommas(diffStr);
  } catch (e) {
    return formatBigIntWithCommas(diffStr);
  }
};


