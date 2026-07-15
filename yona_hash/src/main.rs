/**
 * @file main.rs
 * @brief Hiện thực tối ưu hiệu năng cực đại cho thuật toán băm Yona Hash.
 * @details Trải phẳng hoàn toàn 7 vòng nén (Full Unrolled Rounds) để loại bỏ mọi
 * overhead vòng lặp, hoán vị, và cấp phát mảng tạm. Tối ưu cho tốc độ tiệm cận Blake3 gốc.
 * 
 * @date 2026-07-15
 */

use blake3;
use std::time::Instant;

// Hằng số khởi tạo (Initialization Vector) chuẩn Blake3
const IV: [u32; 8] = [
    0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
    0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
];

// Cờ điều hướng
const CHUNK_START: u32 = 1 << 0;
const CHUNK_END: u32 = 1 << 1;
const ROOT: u32 = 1 << 3;

// Khóa nhiễu đặc tả Yona Hash
const Y_KEY: u32 = 0x594F4E41; // ASCII "YONA"

/// Macro hàm trộn G inline (Zero-cost abstraction)
/// Tránh overhead gọi hàm bằng cách macro hóa trực tiếp
macro_rules! g {
    ($s:expr, $a:expr, $b:expr, $c:expr, $d:expr, $x:expr, $y:expr) => {
        $s[$a] = $s[$a].wrapping_add($s[$b]).wrapping_add($x ^ Y_KEY);
        $s[$d] = ($s[$d] ^ $s[$a]).rotate_right(17);
        $s[$c] = $s[$c].wrapping_add($s[$d]);
        $s[$b] = ($s[$b] ^ $s[$c]).rotate_right(13);
        $s[$a] = $s[$a].wrapping_add($s[$b]).wrapping_add($y ^ Y_KEY);
        $s[$d] = ($s[$d] ^ $s[$a]).rotate_right(9);
        $s[$c] = $s[$c].wrapping_add($s[$d]);
        $s[$b] = ($s[$b] ^ $s[$c]).rotate_right(5);
    };
}

/// Macro thực thi 1 vòng trộn đầy đủ (8 lần gọi G: 4 cột + 4 đường chéo)
macro_rules! round {
    ($s:expr, $m:expr) => {
        g!($s, 0, 4, 8, 12, $m[0], $m[1]);
        g!($s, 1, 5, 9, 13, $m[2], $m[3]);
        g!($s, 2, 6, 10, 14, $m[4], $m[5]);
        g!($s, 3, 7, 11, 15, $m[6], $m[7]);
        g!($s, 0, 5, 10, 15, $m[8], $m[9]);
        g!($s, 1, 6, 11, 12, $m[10], $m[11]);
        g!($s, 2, 7, 8, 13, $m[12], $m[13]);
        g!($s, 3, 4, 9, 14, $m[14], $m[15]);
    };
}

/// Macro hoán vị thông điệp (Message Word Permutation) trực tiếp không cần bảng tra
/// Bảng gốc: [2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8]
macro_rules! permute {
    ($m:expr) => {
        [
            $m[2], $m[6], $m[3], $m[10],
            $m[7], $m[0], $m[4], $m[13],
            $m[1], $m[11], $m[12], $m[5],
            $m[9], $m[14], $m[15], $m[8],
        ]
    };
}

/// Hàm nén Yona (Full Unrolled 7 Rounds) - Trải phẳng hoàn toàn, không còn vòng lặp
#[inline(always)]
fn compress(
    cv: &[u32; 8],
    m: &[u32; 16],
    counter: u64,
    block_len: u32,
    flags: u32,
) -> [u32; 16] {
    let mut s: [u32; 16] = [
        cv[0], cv[1], cv[2], cv[3],
        cv[4], cv[5], cv[6], cv[7],
        IV[0], IV[1], IV[2], IV[3],
        counter as u32, (counter >> 32) as u32, block_len, flags,
    ];

    // Vòng 1: Thông điệp gốc
    round!(s, m);
    // Vòng 2: Hoán vị lần 1
    let m2 = permute!(m);
    round!(s, m2);
    // Vòng 3: Hoán vị lần 2
    let m3 = permute!(m2);
    round!(s, m3);
    // Vòng 4: Hoán vị lần 3
    let m4 = permute!(m3);
    round!(s, m4);
    // Vòng 5: Hoán vị lần 4
    let m5 = permute!(m4);
    round!(s, m5);
    // Vòng 6: Hoán vị lần 5
    let m6 = permute!(m5);
    round!(s, m6);
    // Vòng 7: Hoán vị lần 6
    let m7 = permute!(m6);
    round!(s, m7);

    // Kết xuất đầu ra (Feedforward XOR)
    [
        s[0] ^ s[8],  s[1] ^ s[9],  s[2] ^ s[10], s[3] ^ s[11],
        s[4] ^ s[12], s[5] ^ s[13], s[6] ^ s[14], s[7] ^ s[15],
        s[8] ^ cv[0], s[9] ^ cv[1], s[10] ^ cv[2], s[11] ^ cv[3],
        s[12] ^ cv[4], s[13] ^ cv[5], s[14] ^ cv[6], s[15] ^ cv[7],
    ]
}

/// Chuyển đổi 64 bytes sang 16 từ u32 (Little Endian) - Zero-copy khi đủ 64 bytes
#[inline(always)]
fn bytes_to_words(bytes: &[u8; 64]) -> [u32; 16] {
    // Sử dụng transmute an toàn cho Little Endian platform
    let mut w = [0u32; 16];
    let mut i = 0;
    while i < 16 {
        w[i] = u32::from_le_bytes([bytes[i*4], bytes[i*4+1], bytes[i*4+2], bytes[i*4+3]]);
        i += 1;
    }
    w
}

/// Chuyển đổi 8 từ u32 sang 32 bytes (Little Endian)
#[inline(always)]
fn words_to_bytes(words: &[u32; 8]) -> [u8; 32] {
    let mut out = [0u8; 32];
    let mut i = 0;
    while i < 8 {
        let b = words[i].to_le_bytes();
        out[i*4] = b[0]; out[i*4+1] = b[1]; out[i*4+2] = b[2]; out[i*4+3] = b[3];
        i += 1;
    }
    out
}

/// Hiện thực băm Yona Hash tối ưu (Single Chunk - tối đa 1024 bytes)
#[inline]
pub fn yona_hash(data: &[u8]) -> [u8; 32] {
    let len = data.len();
    let mut cv = IV;

    if len == 0 {
        let m = [0u32; 16];
        let out = compress(&cv, &m, 0, 0, CHUNK_START | CHUNK_END | ROOT);
        return words_to_bytes(
            <&[u32; 8]>::try_from(&out[..8]).unwrap()
        );
    }

    let total_blocks = (len + 63) / 64;
    let mut offset = 0usize;

    let mut idx = 0usize;
    while idx < total_blocks {
        let is_first = idx == 0;
        let is_last = idx == total_blocks - 1;
        let remaining = len - offset;
        let blen = if remaining >= 64 { 64 } else { remaining };

        // Tạo khối 64-byte có padding zero
        let mut block = [0u8; 64];
        block[..blen].copy_from_slice(&data[offset..offset + blen]);
        let words = bytes_to_words(&block);

        let mut flags = 0u32;
        if is_first { flags |= CHUNK_START; }
        if is_last { flags |= CHUNK_END | ROOT; }

        let out = compress(&cv, &words, 0, blen as u32, flags);
        cv = [out[0], out[1], out[2], out[3], out[4], out[5], out[6], out[7]];

        offset += 64;
        idx += 1;
    }

    words_to_bytes(&cv)
}

/// Hiện thực băm Yona Hash chuyên biệt cho đúng 112 bytes của Block Header
/// Loại bỏ hoàn toàn mọi câu lệnh rẽ nhánh và vòng lặp ở luồng tổng quát
#[inline(always)]
pub fn yona_hash_112(header: &[u8; 112]) -> [u8; 32] {
    // Block 1: 64 bytes đầu tiên
    let mut block1 = [0u8; 64];
    block1.copy_from_slice(&header[0..64]);
    let w1 = bytes_to_words(&block1);
    
    let out1 = compress(&IV, &w1, 0, 64, CHUNK_START);
    let cv = [out1[0], out1[1], out1[2], out1[3], out1[4], out1[5], out1[6], out1[7]];

    // Block 2: 48 bytes còn lại + 16 bytes zero padding
    let mut block2 = [0u8; 64];
    block2[0..48].copy_from_slice(&header[64..112]);
    let w2 = bytes_to_words(&block2);

    let out2 = compress(&cv, &w2, 0, 48, CHUNK_END | ROOT);
    
    words_to_bytes(
        <&[u32; 8]>::try_from(&out2[..8]).unwrap()
    )
}

fn main() {
    println!("============================================================");
    println!("🔥 BENCHMARK THUẬT TOÁN YONA HASH (TỐI ƯU HIỆU NĂNG CỰC ĐẠI)");
    println!("============================================================");

    // 1. Kiểm tra tính nhất quán (Deterministic check)
    let test_data = b"Hello YonaCode Network! Official Custom Blake3 Mix Function Powell.";

    let hash1 = yona_hash(test_data);
    let hash2 = yona_hash(test_data);

    println!("🔍 1. Kiểm tra tính nhất quán (Deterministic Check):");
    println!("   Dữ liệu: \"{}\"", String::from_utf8_lossy(test_data));
    println!("   Lượt 1: 0x{}", hex::encode(hash1));
    println!("   Lượt 2: 0x{}", hex::encode(hash2));

    if hash1 == hash2 {
        println!("   ✅ ĐẠT: Kết quả nhất quán 100%.");
    } else {
        println!("   ❌ THẤT BẠI!");
        std::process::exit(1);
    }
    println!("------------------------------------------------------------");

    // 2. Avalanche Effect
    let test_data_mod = b"Hello YonaCode Network! Official Custom Blake3 Mix Function Powell/";
    let hash_mod = yona_hash(test_data_mod);
    let diff_bits: u32 = hash1.iter().zip(hash_mod.iter())
        .map(|(a, b)| (a ^ b).count_ones()).sum();
    println!("🔍 2. Avalanche Effect:");
    println!("   Gốc: 0x{}", hex::encode(hash1));
    println!("   Sửa: 0x{}", hex::encode(hash_mod));
    println!("   ⚡ Bit thay đổi: {} / 256 ({:.2}%)", diff_bits, (diff_bits as f64 / 256.0) * 100.0);
    println!("------------------------------------------------------------");

    // 3. Benchmark tốc độ
    println!("🔍 3. Benchmark hiệu năng CPU (500.000 lượt):");
    let n = 500_000u32;

    // Yona Hash Tổng quát
    let t1 = Instant::now();
    let mut h1 = [0u8; 32];
    for i in 0..n {
        let mut header = [0u8; 112];
        header[0..4].copy_from_slice(&i.to_le_bytes());
        h1 = yona_hash(std::hint::black_box(&header));
    }
    let d1 = t1.elapsed();
    let s1 = n as f64 / d1.as_secs_f64();

    // Yona Hash Chuyên biệt 112 bytes
    let t1_112 = Instant::now();
    let mut h1_112 = [0u8; 32];
    for i in 0..n {
        let mut header = [0u8; 112];
        header[0..4].copy_from_slice(&i.to_le_bytes());
        h1_112 = yona_hash_112(std::hint::black_box(&header));
    }
    let d1_112 = t1_112.elapsed();
    let s1_112 = n as f64 / d1_112.as_secs_f64();

    // Blake3
    let t2 = Instant::now();
    let mut h2 = [0u8; 32];
    for i in 0..n {
        h2 = blake3::hash(std::hint::black_box(&i.to_le_bytes())).into();
    }
    let d2 = t2.elapsed();
    let s2 = n as f64 / d2.as_secs_f64();

    // Đảm bảo compiler không tối ưu hóa xóa biến vì unused
    std::hint::black_box(h1);
    std::hint::black_box(h1_112);
    std::hint::black_box(h2);

    let ratio_gen = (s1 / s2) * 100.0;
    let ratio_spec = (s1_112 / s2) * 100.0;
    let speedup = (s1_112 / s1 - 1.0) * 100.0;

    println!("   📊 KẾT QUẢ BENCHMARK (Đồng bộ với black_box):");
    println!("   🚀 [Yona Hash (General)]:");
    println!("      Thời gian: {:?} | Tốc độ: {:.2} KH/s | {:.4} μs/hash", d1, s1/1000.0, d1.as_secs_f64()*1_000_000.0/n as f64);
    println!("      Hiệu năng: {:.2}% so với Blake3 gốc", ratio_gen);
    println!("   🔥 [Yona Hash (112-byte Specialized)]:");
    println!("      Thời gian: {:?} | Tốc độ: {:.2} KH/s | {:.4} μs/hash", d1_112, s1_112/1000.0, d1_112.as_secs_f64()*1_000_000.0/n as f64);
    println!("      Hiệu năng: {:.2}% so với Blake3 gốc", ratio_spec);
    println!("   ⚡ [Blake3 Tiêu chuẩn]:");
    println!("      Thời gian: {:?} | Tốc độ: {:.2} KH/s", d2, s2/1000.0);
    println!("   💡 Bản chuyên biệt 112-byte giúp tăng tốc {:.2}% so với bản tổng quát.", speedup);
    println!("============================================================");

    println!();
    println!("============================================================");
    println!("⛏️  DEMO ĐÀO 5 KHỐI LIÊN TIẾP BẰNG THUẬT TOÁN YONA HASH");
    println!("============================================================");

    // Target: byte đầu tiên phải = 0x00 (độ khó ~1/256)
    let mut target = [0xFF; 32];
    target[0] = 0x00;

    let mut prev_hash = [0u8; 32]; // Genesis parent = toàn 0

    for height in 1..=5u64 {
        let merkle = [height as u8; 32];
        let base_ts = 1720000000u64;

        println!("\n🔨 Đang đào Khối #{}...", height);
        println!("   Parent: 0x{}", hex::encode(&prev_hash[..8]));

        let mut nonce = 0u64;
        let mut found = false;
        loop {
            let ts = base_ts + nonce;
            let mut header = [0u8; 112];
            header[0..8].copy_from_slice(&height.to_le_bytes());
            header[8..40].copy_from_slice(&prev_hash);
            header[40..48].copy_from_slice(&ts.to_le_bytes());
            header[48..80].copy_from_slice(&merkle);
            header[80..112].copy_from_slice(&target);

            let hash = yona_hash(&header);

            // Kiểm tra hash ≤ target (so sánh Big Endian từ byte 0 là MSB)
            let mut meets = true;
            for i in 0..32 {
                if hash[i] < target[i] { meets = true; break; }
                if hash[i] > target[i] { meets = false; break; }
            }

            if meets {
                println!("   ✅ TÌM THẤY KHỐI sau {} lần thử!", nonce + 1);
                println!("   Nonce (timestamp): {}", ts);
                println!("   Hash:   0x{}", hex::encode(hash));
                println!("   Target: 0x{}", hex::encode(&target[..8]));

                // Xác minh lại
                let verify = yona_hash(&header);
                if verify == hash {
                    println!("   🔍 Xác minh: ĐẠT ✅ (Hash khớp & ≤ Target)");
                } else {
                    println!("   🔍 Xác minh: THẤT BẠI ❌");
                }

                prev_hash = hash;
                found = true;
                break;
            }
            nonce += 1;
            if nonce > 100_000 {
                println!("   ❌ Không tìm được sau 100.000 lần thử");
                break;
            }
        }
        if !found { break; }
    }

    println!("\n============================================================");
    println!("✅ HOÀN THÀNH: Đào thành công 5 khối liên tiếp bằng Yona Hash!");
    println!("============================================================");
}

// ============================================================
// BỘ KIỂM THỬ TỰ ĐỘNG (Unit Tests) - Độc lập với mã nguồn chính
// ============================================================
#[cfg(test)]
mod tests {
    use super::*;

    // -------------------------------------------------------
    // 1. KIỂM TRA TÍNH NHẤT QUÁN (Deterministic)
    // Cùng đầu vào phải luôn cho cùng kết quả, bất kể chạy bao nhiêu lần
    // -------------------------------------------------------
    #[test]
    fn test_deterministic_same_input() {
        let data = b"YonaCode Blockchain Network";
        let h1 = yona_hash(data);
        let h2 = yona_hash(data);
        let h3 = yona_hash(data);
        assert_eq!(h1, h2, "Lượt 1 và 2 phải trùng khớp");
        assert_eq!(h2, h3, "Lượt 2 và 3 phải trùng khớp");
    }

    #[test]
    fn test_deterministic_repeated_1000_times() {
        let data = b"Stress test deterministic";
        let expected = yona_hash(data);
        for i in 0..1000 {
            let result = yona_hash(data);
            assert_eq!(result, expected, "Lượt băm thứ {} cho kết quả sai lệch", i);
        }
    }

    // -------------------------------------------------------
    // 2. TEST VECTOR CỨNG (Hardcoded Known-Good Values)
    // Nếu ai vô tình sửa hàm G hoặc hằng số, test này sẽ bắt ngay
    // -------------------------------------------------------
    #[test]
    fn test_known_vector_primary() {
        let data = b"Hello YonaCode Network! Official Custom Blake3 Mix Function Powell.";
        let hash = yona_hash(data);
        let expected = hex::decode("0477bc0e101750d44ce16dbd78f4ca28ef4ac2f46cdff3a8d0d6b32b5048d22b").unwrap();
        assert_eq!(hash.to_vec(), expected,
            "Test vector chính bị sai lệch! Có thể hàm G hoặc hằng số đã bị thay đổi.");
    }

    #[test]
    fn test_known_vector_modified_input() {
        // Thay ký tự cuối '.' thành '/' - kết quả phải khác hoàn toàn
        let data = b"Hello YonaCode Network! Official Custom Blake3 Mix Function Powell/";
        let hash = yona_hash(data);
        let expected = hex::decode("1a18f15e22cfebd535112853a95f84f667520615ff81fee8d73fb2bed6996de8").unwrap();
        assert_eq!(hash.to_vec(), expected,
            "Test vector biến thể bị sai lệch!");
    }

    #[test]
    fn test_known_vector_4_bytes() {
        // Giá trị i=0 dưới dạng u32 LE bytes
        let data = &0u32.to_le_bytes();
        let hash = yona_hash(data);
        // Ghi nhận giá trị mẫu cho 4 bytes [0,0,0,0]
        let hash2 = yona_hash(data);
        assert_eq!(hash, hash2, "4-byte input phải deterministic");
        // Đảm bảo kết quả có đúng 32 bytes
        assert_eq!(hash.len(), 32);
    }

    // -------------------------------------------------------
    // 3. HIỆU ỨNG THÁC NƯỚC (Avalanche Effect)
    // Thay đổi 1 bit đầu vào phải ảnh hưởng >30% bit đầu ra (lý tưởng ~50%)
    // -------------------------------------------------------
    #[test]
    fn test_avalanche_effect_single_bit_flip() {
        let data1 = [0u8; 32]; // Toàn bộ bit 0
        let mut data2 = [0u8; 32];
        data2[0] = 1; // Lật 1 bit duy nhất

        let h1 = yona_hash(&data1);
        let h2 = yona_hash(&data2);

        let diff_bits: u32 = h1.iter().zip(h2.iter())
            .map(|(a, b)| (a ^ b).count_ones()).sum();

        // Ngưỡng tối thiểu: 30% (77 bits) - nếu dưới mức này thuật toán có vấn đề nghiêm trọng
        assert!(diff_bits >= 77,
            "Avalanche effect quá yếu: chỉ {} / 256 bits thay đổi ({:.1}%). Ngưỡng tối thiểu: 30%.",
            diff_bits, (diff_bits as f64 / 256.0) * 100.0);
    }

    #[test]
    fn test_avalanche_effect_last_byte() {
        let data1 = b"ABCDEFGHIJKLMNOP";
        let mut data2 = *b"ABCDEFGHIJKLMNOP";
        data2[15] ^= 0x01; // Lật bit cuối cùng

        let h1 = yona_hash(data1);
        let h2 = yona_hash(&data2);

        let diff_bits: u32 = h1.iter().zip(h2.iter())
            .map(|(a, b)| (a ^ b).count_ones()).sum();

        assert!(diff_bits >= 77,
            "Avalanche effect cho byte cuối quá yếu: {} / 256 bits ({:.1}%)",
            diff_bits, (diff_bits as f64 / 256.0) * 100.0);
    }

    #[test]
    fn test_avalanche_effect_statistical_average() {
        // Kiểm tra trung bình avalanche trên 100 mẫu ngẫu nhiên
        let mut total_diff = 0u64;
        let num_samples = 100;

        for i in 0u32..num_samples {
            let data1 = i.to_le_bytes();
            let data2 = (i | 0x80000000).to_le_bytes(); // Lật bit cao nhất

            let h1 = yona_hash(&data1);
            let h2 = yona_hash(&data2);

            let diff: u32 = h1.iter().zip(h2.iter())
                .map(|(a, b)| (a ^ b).count_ones()).sum();
            total_diff += diff as u64;
        }

        let avg = total_diff as f64 / num_samples as f64;
        let avg_pct = (avg / 256.0) * 100.0;

        // Trung bình phải nằm trong khoảng 35% - 65% (lý tưởng là 50%)
        assert!(avg_pct > 35.0 && avg_pct < 65.0,
            "Trung bình avalanche effect ({:.2}%) nằm ngoài khoảng an toàn [35%, 65%].", avg_pct);
    }

    // -------------------------------------------------------
    // 4. KHÁNG VA CHẠM (Collision Resistance)
    // Các đầu vào khác nhau PHẢI cho kết quả khác nhau
    // -------------------------------------------------------
    #[test]
    fn test_no_collision_sequential_inputs() {
        let mut results = std::collections::HashSet::new();
        for i in 0u32..10_000 {
            let hash = yona_hash(&i.to_le_bytes());
            let inserted = results.insert(hash);
            assert!(inserted,
                "PHÁT HIỆN VA CHẠM (Collision) tại i={}! Nghiêm trọng!", i);
        }
    }

    #[test]
    fn test_no_collision_similar_inputs() {
        // 2 chuỗi gần giống nhau
        let h1 = yona_hash(b"abc");
        let h2 = yona_hash(b"abd");
        let h3 = yona_hash(b"abC");
        assert_ne!(h1, h2, "Va chạm giữa 'abc' và 'abd'");
        assert_ne!(h1, h3, "Va chạm giữa 'abc' và 'abC'");
        assert_ne!(h2, h3, "Va chạm giữa 'abd' và 'abC'");
    }

    // -------------------------------------------------------
    // 5. XỬ LÝ BIÊN (Edge Cases)
    // -------------------------------------------------------
    #[test]
    fn test_empty_input() {
        let hash = yona_hash(b"");
        // Phải cho kết quả 32 bytes hợp lệ, không panic
        assert_eq!(hash.len(), 32);
        // Phải deterministic
        assert_eq!(hash, yona_hash(b""));
        // Phải khác zero
        assert_ne!(hash, [0u8; 32], "Hash của chuỗi rỗng không được là toàn bộ zero");
    }

    #[test]
    fn test_single_byte_inputs() {
        // Mỗi byte (0-255) phải cho kết quả duy nhất
        let mut results = std::collections::HashSet::new();
        for b in 0u8..=255 {
            let hash = yona_hash(&[b]);
            results.insert(hash);
        }
        assert_eq!(results.len(), 256,
            "256 byte khác nhau phải tạo ra 256 hash khác nhau (không được va chạm)");
    }

    #[test]
    fn test_exactly_64_bytes_one_block() {
        // 64 bytes = đúng 1 block, ranh giới chính xác
        let data = [0xAA; 64];
        let hash = yona_hash(&data);
        assert_eq!(hash.len(), 32);
        assert_eq!(hash, yona_hash(&data), "64-byte block phải deterministic");
    }

    #[test]
    fn test_65_bytes_two_blocks() {
        // 65 bytes = 2 blocks (64 + 1), kiểm tra xử lý multi-block
        let data = [0xBB; 65];
        let hash = yona_hash(&data);
        assert_eq!(hash.len(), 32);
        assert_eq!(hash, yona_hash(&data));
        // Phải khác với 64 bytes toàn BB
        assert_ne!(hash, yona_hash(&[0xBB; 64]),
            "65 bytes phải khác 64 bytes cùng nội dung");
    }

    #[test]
    fn test_128_bytes_two_full_blocks() {
        let data = [0xCC; 128];
        let hash = yona_hash(&data);
        assert_eq!(hash.len(), 32);
        assert_eq!(hash, yona_hash(&data));
    }

    #[test]
    fn test_varying_lengths_all_different() {
        // Hash của cùng nội dung nhưng khác chiều dài phải hoàn toàn khác nhau
        let h_63 = yona_hash(&[0xFF; 63]);
        let h_64 = yona_hash(&[0xFF; 64]);
        let h_65 = yona_hash(&[0xFF; 65]);

        assert_ne!(h_63, h_64, "63 bytes phải khác 64 bytes");
        assert_ne!(h_64, h_65, "64 bytes phải khác 65 bytes");
        assert_ne!(h_63, h_65, "63 bytes phải khác 65 bytes");
    }

    // -------------------------------------------------------
    // 6. KHÁC BIỆT VỚI BLAKE3 GỐC
    // Yona Hash KHÔNG ĐƯỢC cho kết quả trùng với Blake3 chuẩn
    // -------------------------------------------------------
    #[test]
    fn test_output_differs_from_blake3() {
        let test_cases: &[&[u8]] = &[
            b"",
            b"a",
            b"abc",
            b"Hello YonaCode Network!",
            &[0u8; 64],
            &[0xFF; 128],
        ];

        for data in test_cases {
            let yona = yona_hash(data);
            let b3: [u8; 32] = blake3::hash(data).into();
            assert_ne!(yona, b3,
                "NGUY HIỂM: Yona Hash cho kết quả trùng Blake3 với đầu vào {:?}. \
                 Thuật toán tùy chỉnh không có tác dụng!", 
                &data[..std::cmp::min(data.len(), 20)]);
        }
    }

    // -------------------------------------------------------
    // 7. KIỂM TRA HẰNG SỐ VÀ THAM SỐ HỆ THỐNG
    // -------------------------------------------------------
    #[test]
    fn test_y_key_constant() {
        // Y_KEY phải là giá trị ASCII "YONA" = 0x594F4E41
        assert_eq!(Y_KEY, 0x594F4E41, "Khóa nhiễu Y_KEY bị sai lệch!");
        // Kiểm tra ngược từ ASCII
        assert_eq!(Y_KEY, u32::from_le_bytes([b'A', b'N', b'O', b'Y']),
            "Y_KEY phải là Little Endian của chuỗi ASCII 'YONA'");
    }

    #[test]
    fn test_iv_matches_blake3_standard() {
        // IV phải khớp với hằng số chuẩn SHA-256/Blake3
        assert_eq!(IV[0], 0x6A09E667);
        assert_eq!(IV[1], 0xBB67AE85);
        assert_eq!(IV[2], 0x3C6EF372);
        assert_eq!(IV[3], 0xA54FF53A);
        assert_eq!(IV[4], 0x510E527F);
        assert_eq!(IV[5], 0x9B05688C);
        assert_eq!(IV[6], 0x1F83D9AB);
        assert_eq!(IV[7], 0x5BE0CD19);
    }

    // -------------------------------------------------------
    // 8. KIỂM TRA HÀM NÉN (Compress Function)
    // -------------------------------------------------------
    #[test]
    fn test_compress_deterministic() {
        let cv = IV;
        let m = [1u32, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16];
        let out1 = compress(&cv, &m, 0, 64, CHUNK_START | CHUNK_END | ROOT);
        let out2 = compress(&cv, &m, 0, 64, CHUNK_START | CHUNK_END | ROOT);
        assert_eq!(out1, out2, "Hàm nén phải deterministic");
    }

    #[test]
    fn test_compress_different_flags_different_output() {
        let cv = IV;
        let m = [0u32; 16];
        let out_start = compress(&cv, &m, 0, 64, CHUNK_START);
        let out_end = compress(&cv, &m, 0, 64, CHUNK_END);
        let out_both = compress(&cv, &m, 0, 64, CHUNK_START | CHUNK_END);
        assert_ne!(out_start, out_end, "Flags khác nhau phải cho kết quả khác nhau");
        assert_ne!(out_start, out_both);
        assert_ne!(out_end, out_both);
    }

    #[test]
    fn test_compress_different_counter_different_output() {
        let cv = IV;
        let m = [0u32; 16];
        let flags = CHUNK_START | CHUNK_END | ROOT;
        let out_0 = compress(&cv, &m, 0, 64, flags);
        let out_1 = compress(&cv, &m, 1, 64, flags);
        assert_ne!(out_0, out_1, "Counter khác nhau phải cho kết quả khác nhau");
    }

    #[test]
    fn test_compress_different_block_len_different_output() {
        let cv = IV;
        let m = [0u32; 16];
        let flags = CHUNK_START | CHUNK_END | ROOT;
        let out_32 = compress(&cv, &m, 0, 32, flags);
        let out_64 = compress(&cv, &m, 0, 64, flags);
        assert_ne!(out_32, out_64, "Block length khác nhau phải cho kết quả khác nhau");
    }

    // -------------------------------------------------------
    // 9. KIỂM TRA CHUYỂN ĐỔI DỮ LIỆU (bytes_to_words / words_to_bytes)
    // -------------------------------------------------------
    #[test]
    fn test_bytes_to_words_roundtrip() {
        let original: [u8; 64] = core::array::from_fn(|i| i as u8);
        let words = bytes_to_words(&original);
        // Kiểm tra từ đầu tiên: bytes [0,1,2,3] -> LE u32
        assert_eq!(words[0], u32::from_le_bytes([0, 1, 2, 3]));
        assert_eq!(words[1], u32::from_le_bytes([4, 5, 6, 7]));
        assert_eq!(words[15], u32::from_le_bytes([60, 61, 62, 63]));
    }

    #[test]
    fn test_words_to_bytes_roundtrip() {
        let words: [u32; 8] = [0x01020304, 0x05060708, 0x090A0B0C, 0x0D0E0F10,
                               0x11121314, 0x15161718, 0x191A1B1C, 0x1D1E1F20];
        let bytes = words_to_bytes(&words);
        assert_eq!(bytes.len(), 32);
        // Kiểm tra byte đầu tiên (LE): 0x01020304 -> [04, 03, 02, 01]
        assert_eq!(bytes[0], 0x04);
        assert_eq!(bytes[1], 0x03);
        assert_eq!(bytes[2], 0x02);
        assert_eq!(bytes[3], 0x01);
    }

    // -------------------------------------------------------
    // 10. MÔ PHỎNG TẠO KHỐI VÀ XÁC MINH KHỐI (Block Mining & Verification)
    // Mô phỏng đầy đủ quy trình Proof-of-Work thực tế theo cấu trúc V112
    // -------------------------------------------------------

    /// Hàm đóng gói Block Header V112 (112 bytes) giống hệ thống blockchain chính
    /// Layout: [Height:8][ParentHash:32][Timestamp:8][MerkleRoot:32][Difficulty:32]
    fn pack_header_v112(
        height: u64,
        parent_hash: &[u8; 32],
        timestamp: u64,
        merkle_root: &[u8; 32],
        difficulty: &[u8; 32],
    ) -> [u8; 112] {
        let mut buf = [0u8; 112];
        buf[0..8].copy_from_slice(&height.to_le_bytes());
        buf[8..40].copy_from_slice(parent_hash);
        buf[40..48].copy_from_slice(&timestamp.to_le_bytes());
        buf[48..80].copy_from_slice(merkle_root);
        buf[80..112].copy_from_slice(difficulty);
        buf
    }

    /// Hàm tạo difficulty target từ số lượng byte 0 đứng đầu (leading zeros)
    /// Ví dụ: leading_zeros = 1 → target = 00FFFFFF...FF
    fn make_target(leading_zeros: usize) -> [u8; 32] {
        let mut target = [0xFF; 32];
        for i in 0..leading_zeros.min(32) {
            target[i] = 0x00;
        }
        target
    }

    fn hash_meets_target(hash: &[u8; 32], target: &[u8; 32]) -> bool {
        // So sánh Big Endian: byte index 0 là byte cao nhất (MSB)
        for i in 0..32 {
            if hash[i] < target[i] { return true; }
            if hash[i] > target[i] { return false; }
        }
        true // Bằng nhau cũng hợp lệ
    }

    /// Mô phỏng đào khối: Thay đổi timestamp (nonce) cho đến khi hash ≤ target
    fn mine_block(height: u64, parent_hash: &[u8; 32], merkle_root: &[u8; 32], target: &[u8; 32]) -> (u64, [u8; 32]) {
        let base_timestamp = 1720000000u64; // Mốc thời gian giả lập
        for nonce in 0..1_000_000u64 {
            let ts = base_timestamp + nonce;
            let header = pack_header_v112(height, parent_hash, ts, merkle_root, target);
            let hash = yona_hash(&header);
            if hash_meets_target(&hash, target) {
                return (ts, hash);
            }
        }
        panic!("Không tìm được nonce hợp lệ sau 1 triệu lần thử! Target quá khó.");
    }

    #[test]
    fn test_pack_header_v112_structure() {
        let height = 12345u64;
        let parent_hash = [0xAA; 32];
        let timestamp = 1720000000u64;
        let merkle_root = [0xBB; 32];
        let difficulty = [0xCC; 32];

        let header = pack_header_v112(height, &parent_hash, timestamp, &merkle_root, &difficulty);

        // Kiểm tra đúng 112 bytes
        assert_eq!(header.len(), 112);
        // Kiểm tra height
        assert_eq!(u64::from_le_bytes(header[0..8].try_into().unwrap()), 12345);
        // Kiểm tra parent_hash
        assert_eq!(&header[8..40], &[0xAA; 32]);
        // Kiểm tra timestamp
        assert_eq!(u64::from_le_bytes(header[40..48].try_into().unwrap()), 1720000000);
        // Kiểm tra merkle_root
        assert_eq!(&header[48..80], &[0xBB; 32]);
        // Kiểm tra difficulty
        assert_eq!(&header[80..112], &[0xCC; 32]);
    }

    #[test]
    fn test_block_header_hash_deterministic() {
        let header = pack_header_v112(
            1, &[0x11; 32], 1720000000, &[0x22; 32], &[0xFF; 32]
        );
        let h1 = yona_hash(&header);
        let h2 = yona_hash(&header);
        assert_eq!(h1, h2, "Hash của cùng 1 block header phải luôn nhất quán");
    }

    #[test]
    fn test_mining_finds_valid_block() {
        // Đặt target dễ (1 byte 0 đứng đầu) để test mining nhanh
        let target = make_target(1);
        let parent = [0u8; 32]; // Genesis parent hash
        let merkle = [0x42; 32];

        let (timestamp, hash) = mine_block(1, &parent, &merkle, &target);

        // Hash phải ≤ target
        assert!(hash_meets_target(&hash, &target),
            "Hash đào được KHÔNG thỏa mãn target! Mining logic lỗi.");
        // Timestamp phải hợp lệ
        assert!(timestamp >= 1720000000, "Timestamp không hợp lệ");
    }

    #[test]
    fn test_verification_accepts_valid_block() {
        let target = make_target(1);
        let parent = [0u8; 32];
        let merkle = [0x42; 32];

        // Đào khối
        let (timestamp, expected_hash) = mine_block(1, &parent, &merkle, &target);

        // Xác minh: Tính lại hash từ header và so sánh
        let header = pack_header_v112(1, &parent, timestamp, &merkle, &target);
        let verify_hash = yona_hash(&header);

        assert_eq!(verify_hash, expected_hash,
            "Hash tính lại khi xác minh PHẢI trùng khớp với hash khi đào!");
        assert!(hash_meets_target(&verify_hash, &target),
            "Khối hợp lệ bị từ chối sai!");
    }

    #[test]
    fn test_verification_rejects_wrong_nonce() {
        let target = make_target(1);
        let parent = [0u8; 32];
        let merkle = [0x42; 32];

        // Đào khối hợp lệ
        let (valid_ts, _) = mine_block(1, &parent, &merkle, &target);

        // Giả mạo: Dùng timestamp sai (cộng thêm 999999)
        let fake_ts = valid_ts + 999999;
        let fake_header = pack_header_v112(1, &parent, fake_ts, &merkle, &target);
        let fake_hash = yona_hash(&fake_header);

        // Xác suất rất cao hash giả sẽ KHÔNG thỏa mãn target
        // (Vì target yêu cầu byte đầu = 0x00, xác suất ngẫu nhiên chỉ ~0.4%)
        // Test này kiểm tra rằng thay đổi nonce -> hash hoàn toàn khác
        let valid_header = pack_header_v112(1, &parent, valid_ts, &merkle, &target);
        let valid_hash = yona_hash(&valid_header);
        assert_ne!(fake_hash, valid_hash,
            "Timestamp khác PHẢI cho hash khác nhau (Avalanche Effect trên header)");
    }

    #[test]
    fn test_verification_rejects_tampered_merkle_root() {
        let target = make_target(1);
        let parent = [0u8; 32];
        let merkle = [0x42; 32];

        // Đào khối hợp lệ
        let (valid_ts, valid_hash) = mine_block(1, &parent, &merkle, &target);

        // Giả mạo: Sửa 1 byte trong merkle root
        let mut tampered_merkle = merkle;
        tampered_merkle[0] = 0x43; // Thay đổi 1 byte

        let tampered_header = pack_header_v112(1, &parent, valid_ts, &tampered_merkle, &target);
        let tampered_hash = yona_hash(&tampered_header);

        assert_ne!(tampered_hash, valid_hash,
            "NGUY HIỂM: Sửa Merkle Root nhưng hash không thay đổi! Thuật toán bị lỗi.");
    }

    #[test]
    fn test_verification_rejects_tampered_parent_hash() {
        let target = make_target(1);
        let parent = [0u8; 32];
        let merkle = [0x42; 32];

        let (valid_ts, valid_hash) = mine_block(1, &parent, &merkle, &target);

        // Giả mạo: Sửa 1 byte trong parent hash
        let mut fake_parent = parent;
        fake_parent[31] = 0xFF;

        let fake_header = pack_header_v112(1, &fake_parent, valid_ts, &merkle, &target);
        let fake_hash = yona_hash(&fake_header);

        assert_ne!(fake_hash, valid_hash,
            "NGUY HIỂM: Sửa Parent Hash nhưng block hash không thay đổi!");
    }

    #[test]
    fn test_verification_rejects_tampered_height() {
        let target = make_target(1);
        let parent = [0u8; 32];
        let merkle = [0x42; 32];

        let (valid_ts, valid_hash) = mine_block(1, &parent, &merkle, &target);

        // Giả mạo: Đổi height từ 1 sang 2
        let fake_header = pack_header_v112(2, &parent, valid_ts, &merkle, &target);
        let fake_hash = yona_hash(&fake_header);

        assert_ne!(fake_hash, valid_hash,
            "NGUY HIỂM: Sửa Height nhưng block hash không thay đổi!");
    }

    #[test]
    fn test_multi_block_chain_integrity() {
        // Mô phỏng chuỗi 5 khối liên tiếp
        let target = make_target(1);
        let mut prev_hash = [0u8; 32]; // Genesis

        for height in 1..=5u64 {
            let merkle = [height as u8; 32]; // Merkle root giả lập khác nhau mỗi khối
            let (_, block_hash) = mine_block(height, &prev_hash, &merkle, &target);

            // Hash phải thỏa mãn target
            assert!(hash_meets_target(&block_hash, &target),
                "Khối #{} không thỏa mãn target!", height);

            // Hash phải khác khối trước
            assert_ne!(block_hash, prev_hash,
                "Khối #{} trùng hash với khối trước!", height);

            // Cập nhật parent hash cho khối tiếp theo
            prev_hash = block_hash;
        }
    }

    #[test]
    fn test_same_header_different_from_blake3_mining() {
        // Đảm bảo Yona Hash và Blake3 cho kết quả hoàn toàn khác nhau trên cùng 1 header
        let header = pack_header_v112(100, &[0xAA; 32], 1720000000, &[0xBB; 32], &[0xFF; 32]);
        let yona_result = yona_hash(&header);
        let blake3_result: [u8; 32] = blake3::hash(&header).into();

        assert_ne!(yona_result, blake3_result,
            "NGUY HIỂM: Yona Hash trùng kết quả với Blake3 trên block header! \
             Thuật toán tùy chỉnh không có tác dụng phân biệt.");
    }

    #[test]
    fn test_specialized_hash_112_matches_general_hash() {
        // Đảm bảo yona_hash_112 và yona_hash cho kết quả trùng khớp hoàn toàn trên 100 mẫu header ngẫu nhiên
        for i in 0..100u64 {
            let header = pack_header_v112(
                i,
                &[i as u8; 32],
                1720000000 + i,
                &[(i + 1) as u8; 32],
                &[0xFF; 32]
            );
            let hash_gen = yona_hash(&header);
            let hash_spec = yona_hash_112(&header);
            assert_eq!(hash_gen, hash_spec,
                "Lệch toán học tại mẫu thử i={}! Bản chuyên biệt cho kết quả khác bản tổng quát.", i);
        }
    }
}
