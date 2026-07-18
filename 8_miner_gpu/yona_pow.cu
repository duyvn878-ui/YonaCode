/*
 * Tên tệp: yona_pow.cu
 * Tính năng chi tiết: Lập trình GPU CUDA tối ưu hóa cực hạn cho thuật toán băm Yona Hash.
 *                     Sử dụng các kỹ thuật:
 *                     1. Midstate Precomputation: Tính toán trước 5 phép trộn cố định của Round 0 (Block 0) ngoài vòng lặp nonce.
 *                     2. Multi-Nonce Per Thread Loop: Quét song song 32 nonces liên tiếp trên mỗi thread để phân bổ chi phí setup.
 *                     3. Intrinsic Funnel Shift và Byte Permute: Thực thi dịch bit và đảo byte trong đúng 1 chu kỳ máy.
 *                     4. Lazy Target Rejection: So sánh byte cao nhất trước để bỏ qua sớm 99.6% nonces sai.
 * Ngày cập nhật: 15/07/2026
 */

#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>

// Các cờ điều hướng Blake3
#define CHUNK_START (1 << 0)
#define CHUNK_END   (1 << 1)
#define ROOT        (1 << 3)

// Khóa nhiễu Yona Hash
#define Y_KEY 0x594F4E41

// Hằng số khởi tạo IV chuẩn Blake3
__constant__ uint32_t IV[8] = {
    0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
    0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19
};

// Các biến hằng số từ Host truyền vào
__constant__ uint64_t d_height;
__constant__ uint32_t d_parent_hash[8];
__constant__ uint32_t d_merkle_root[8];
__constant__ uint32_t d_target[8];

// Biến toàn cục GPU nhận diện kết quả
__device__ uint64_t d_found_nonce;
__device__ unsigned int d_found_flag;

// Hàm xoay bit sử dụng intrinsic funnel shift của NVIDIA
__device__ __forceinline__ uint32_t rotr(uint32_t x, int n) {
    return __funnelshift_r(x, x, n);
}

// Lệnh đảo ngược byte siêu tốc sử dụng byte_perm của GPU
__device__ __forceinline__ uint32_t byte_swap(uint32_t x) {
    return __byte_perm(x, 0, 0x0123);
}

// Hàm trộn lõi G của Yona Hash sử dụng reference để ép thanh ghi
__device__ __forceinline__ void g(uint32_t& a, uint32_t& b, uint32_t& c, uint32_t& d, uint32_t x, uint32_t y) {
    a = a + b + (x ^ Y_KEY);
    d = rotr(d ^ a, 17);
    c = c + d;
    b = rotr(b ^ c, 13);
    
    a = a + b + (y ^ Y_KEY);
    d = rotr(d ^ a, 9);
    c = c + d;
    b = rotr(b ^ c, 5);
}

#define ROUND(s, m) \
    g(s[0], s[4], s[8],  s[12], m[0],  m[1]);  \
    g(s[1], s[5], s[9],  s[13], m[2],  m[3]);  \
    g(s[2], s[6], s[10], s[14], m[4],  m[5]);  \
    g(s[3], s[7], s[11], s[15], m[6],  m[7]);  \
    g(s[0], s[5], s[10], s[15], m[8],  m[9]);  \
    g(s[1], s[6], s[11], s[12], m[10], m[11]); \
    g(s[2], s[7], s[8],  s[13], m[12], m[13]); \
    g(s[3], s[4], s[9],  s[14], m[14], m[15]);

// So sánh 256-bit Big Endian (a <= b)
__device__ __forceinline__ bool is_less_than_256(const uint32_t* a, const uint32_t* b) {
    #pragma unroll
    for (int i = 0; i < 8; i++) {
        uint32_t sa = byte_swap(a[i]);
        uint32_t sb = byte_swap(b[i]);
        if (sa < sb) return true;
        if (sa > sb) return false;
    }
    return true;
}

// CUDA Kernel: Đào nonce tối ưu hóa cực hạn
__global__ __launch_bounds__(256, 2)
void mine_yona_kernel(uint64_t base_nonce) {
    if (d_found_flag) return;

    uint64_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    uint64_t thread_start_nonce = base_nonce + tid * 32;

    // Cache các giá trị cố định vào các thanh ghi của thread
    uint32_t h_low = (uint32_t)(d_height & 0xffffffff);
    uint32_t h_high = (uint32_t)(d_height >> 32);
    
    uint32_t p0 = d_parent_hash[0]; uint32_t p1 = d_parent_hash[1];
    uint32_t p2 = d_parent_hash[2]; uint32_t p3 = d_parent_hash[3];
    uint32_t p4 = d_parent_hash[4]; uint32_t p5 = d_parent_hash[5];
    uint32_t p6 = d_parent_hash[6]; uint32_t p7 = d_parent_hash[7];

    uint32_t mr0 = d_merkle_root[0]; uint32_t mr1 = d_merkle_root[1];
    uint32_t mr2 = d_merkle_root[2]; uint32_t mr3 = d_merkle_root[3];
    uint32_t mr4 = d_merkle_root[4]; uint32_t mr5 = d_merkle_root[5];
    uint32_t mr6 = d_merkle_root[6]; uint32_t mr7 = d_merkle_root[7];

    // =========================================================================
    // KỸ THUẬT 1: Midstate Precomputation (Block 0 - Round 0)
    // Tính toán trước 5 phép trộn đầu tiên của Round 0 hoàn toàn độc lập với Nonce
    // =========================================================================
    uint32_t mid_s0 = IV[0]; uint32_t mid_s1 = IV[1]; uint32_t mid_s2 = IV[2]; uint32_t mid_s3 = IV[3];
    uint32_t mid_s4 = IV[4]; uint32_t mid_s5 = IV[5]; uint32_t mid_s6 = IV[6]; uint32_t mid_s7 = IV[7];
    uint32_t mid_s8 = IV[0]; uint32_t mid_s9 = IV[1]; uint32_t mid_s10 = IV[2]; uint32_t mid_s11 = IV[3];
    uint32_t mid_s12 = 0;    uint32_t mid_s13 = 0;
    uint32_t mid_s14 = 64;
    uint32_t mid_s15 = CHUNK_START;

    g(mid_s0, mid_s4, mid_s8,  mid_s12, h_low, h_high); // Trộn với Height
    g(mid_s1, mid_s5, mid_s9,  mid_s13, p0, p1);       // Trộn với ParentHash[0..1]
    g(mid_s2, mid_s6, mid_s10, mid_s14, p2, p3);       // Trộn với ParentHash[2..3]
    g(mid_s3, mid_s7, mid_s11, mid_s15, p4, p5);       // Trộn với ParentHash[4..5]
    g(mid_s0, mid_s5, mid_s10, mid_s15, p6, p7);       // Trộn chéo với ParentHash[6..7]

    // =========================================================================
    // KỸ THUẬT 2: Multi-Nonce Per Thread Loop
    // Quét song song 32 nonces liên tiếp trên mỗi luồng để tái sử dụng midstate
    // =========================================================================
    #pragma unroll
    for (int nonce_offset = 0; nonce_offset < 32; nonce_offset++) {
        if (d_found_flag) break;

        uint64_t current_nonce = thread_start_nonce + nonce_offset;
        uint32_t nonce_low = (uint32_t)(current_nonce & 0xffffffff);
        uint32_t nonce_high = (uint32_t)(current_nonce >> 32);

        // --- BLOCK 0 COMPRESSION ---
        // Khôi phục midstate đã tính toán trước
        uint32_t s[16];
        s[0] = mid_s0;   s[1] = mid_s1;   s[2] = mid_s2;   s[3] = mid_s3;
        s[4] = mid_s4;   s[5] = mid_s5;   s[6] = mid_s6;   s[7] = mid_s7;
        s[8] = mid_s8;   s[9] = mid_s9;   s[10] = mid_s10; s[11] = mid_s11;
        s[12] = mid_s12; s[13] = mid_s13; s[14] = mid_s14; s[15] = mid_s15;

        // Hoàn thành nốt Round 0 của Block 0 (3 phép trộn còn lại phụ thuộc nonce)
        g(s[1], s[6], s[11], s[12], nonce_low, nonce_high);
        g(s[2], s[7], s[8],  s[13], mr0, mr1);
        g(s[3], s[4], s[9],  s[14], mr2, mr3);

        // Mảng message gốc cho Block 0 phục vụ hoán vị các vòng sau
        // Layout: [Height:8][ParentHash:32][Nonce:8][MerkleRootPart1:16]
        uint32_t m[16] = {
            h_low, h_high, p0, p1, p2, p3, p4, p5, p6, p7,
            nonce_low, nonce_high, mr0, mr1, mr2, mr3
        };

        // Round 1
        uint32_t m1[16] = {
            m[2], m[6], m[3], m[10], m[7], m[0], m[4], m[13],
            m[1], m[11], m[12], m[5], m[9], m[14], m[15], m[8]
        };
        ROUND(s, m1);

        // Round 2
        uint32_t m2[16] = {
            m1[2], m1[6], m1[3], m1[10], m1[7], m1[0], m1[4], m1[13],
            m1[1], m1[11], m1[12], m1[5], m1[9], m1[14], m1[15], m1[8]
        };
        ROUND(s, m2);

        // Round 3
        uint32_t m3[16] = {
            m2[2], m2[6], m2[3], m2[10], m2[7], m2[0], m2[4], m2[13],
            m2[1], m2[11], m2[12], m2[5], m2[9], m2[14], m2[15], m2[8]
        };
        ROUND(s, m3);

        // Round 4
        uint32_t m4[16] = {
            m3[2], m3[6], m3[3], m3[10], m3[7], m3[0], m3[4], m3[13],
            m3[1], m3[11], m3[12], m3[5], m3[9], m3[14], m3[15], m3[8]
        };
        ROUND(s, m4);

        // Round 5
        uint32_t m5[16] = {
            m4[2], m4[6], m4[3], m4[10], m4[7], m4[0], m4[4], m4[13],
            m4[1], m4[11], m4[12], m4[5], m4[9], m4[14], m4[15], m4[8]
        };
        ROUND(s, m5);

        // Round 6
        uint32_t m6[16] = {
            m5[2], m5[6], m5[3], m5[10], m5[7], m5[0], m5[4], m5[13],
            m5[1], m5[11], m5[12], m5[5], m5[9], m5[14], m5[15], m5[8]
        };
        ROUND(s, m6);

        // Feedforward XOR cho Block 0 -> cv1 (chaining value 1)
        uint32_t cv1[8];
        #pragma unroll
        for (int i = 0; i < 8; i++) {
            cv1[i] = s[i] ^ s[i + 8];
        }

        // --- BLOCK 1 COMPRESSION (48 bytes) ---
        // Khởi tạo trạng thái Block 1
        s[0] = cv1[0]; s[1] = cv1[1]; s[2] = cv1[2]; s[3] = cv1[3];
        s[4] = cv1[4]; s[5] = cv1[5]; s[6] = cv1[6]; s[7] = cv1[7];
        s[8] = IV[0];  s[9] = IV[1];  s[10] = IV[2]; s[11] = IV[3];
        s[12] = 0;     s[13] = 0;
        s[14] = 48;
        s[15] = CHUNK_END | ROOT;

        // Message gốc cho Block 1
        // Layout: [MerkleRootPart2:16][Difficulty:32][Padding:16]
        uint32_t mb[16] = {
            mr4, mr5, mr6, mr7,
            d_target[0], d_target[1], d_target[2], d_target[3],
            d_target[4], d_target[5], d_target[6], d_target[7],
            0, 0, 0, 0
        };

        // Round 0
        ROUND(s, mb);

        // Round 1
        uint32_t mb1[16] = {
            mb[2], mb[6], mb[3], mb[10], mb[7], mb[0], mb[4], mb[13],
            mb[1], mb[11], mb[12], mb[5], mb[9], mb[14], mb[15], mb[8]
        };
        ROUND(s, mb1);

        // Round 2
        uint32_t mb2[16] = {
            mb1[2], mb1[6], mb1[3], mb1[10], mb1[7], mb1[0], mb1[4], mb1[13],
            mb1[1], mb1[11], mb1[12], mb1[5], mb1[9], mb1[14], mb1[15], mb1[8]
        };
        ROUND(s, mb2);

        // Round 3
        uint32_t mb3[16] = {
            mb2[2], mb2[6], mb2[3], mb2[10], mb2[7], mb2[0], mb2[4], mb2[13],
            mb2[1], mb2[11], mb2[12], mb2[5], mb2[9], mb2[14], mb2[15], mb2[8]
        };
        ROUND(s, mb3);

        // Round 4
        uint32_t mb4[16] = {
            mb3[2], mb3[6], mb3[3], mb3[10], mb3[7], mb3[0], mb3[4], mb3[13],
            mb3[1], mb3[11], mb3[12], mb3[5], mb3[9], mb3[14], mb3[15], mb3[8]
        };
        ROUND(s, mb4);

        // Round 5
        uint32_t mb5[16] = {
            mb4[2], mb4[6], mb4[3], mb4[10], mb4[7], mb4[0], mb4[4], mb4[13],
            mb4[1], mb4[11], mb4[12], mb4[5], mb4[9], mb4[14], mb4[15], mb4[8]
        };
        ROUND(s, mb5);

        // Round 6
        uint32_t mb6[16] = {
            mb5[2], mb5[6], mb5[3], mb5[10], mb5[7], mb5[0], mb5[4], mb5[13],
            mb5[1], mb5[11], mb5[12], mb5[5], mb5[9], mb5[14], mb5[15], mb5[8]
        };
        ROUND(s, mb6);

        // Kết xuất kết quả cuối cùng của Block 1 (XOR chaining value và states)
        uint32_t final_hash[8];
        #pragma unroll
        for (int i = 0; i < 8; i++) {
            final_hash[i] = s[i] ^ s[i + 8];
        }

        // =========================================================================
        // KỸ THUẬT 4: Lazy Target Rejection (Early Rejection)
        // So sánh byte cao nhất trước. Nếu vi phạm, bỏ qua sớm toàn bộ nonce.
        // =========================================================================
        uint32_t hash_high = byte_swap(final_hash[0]);
        uint32_t target_high = byte_swap(d_target[0]);
        if (hash_high > target_high) {
            continue; // Loại bỏ sớm
        }

        // So sánh toàn bộ 256-bit
        if (is_less_than_256(final_hash, d_target)) {
            if (atomicCAS(&d_found_flag, 0, 1) == 0) {
                d_found_nonce = current_nonce;
            }
            break; // Tìm thấy, dừng quét
        }
    }
}

// Host Wrapper Function khởi tạo thiết bị
extern "C" bool init_yona_cuda_device() {
    int device_count = 0;
    cudaError_t err = cudaGetDeviceCount(&device_count);
    if (err != cudaSuccess || device_count == 0) {
        printf("[CUDA-ERROR] No NVIDIA GPU detected.\n");
        return false;
    }
    cudaSetDevice(0);
    cudaFree(0);
    printf("[CUDA-INFO] Yona Hash GPU Miner successfully initialized Device 0.\n");
    return true;
}

// Host Wrapper Function chạy miner (Zero-Allocation Loop)
extern "C" bool run_yona_cuda_miner(
    uint64_t height,
    const uint8_t* parent_hash,
    const uint8_t* merkle_root,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
) {
    cudaError_t err;

    // Truyền tải dữ liệu qua constant memory của GPU (nhanh hơn Global memory 10 lần)
    err = cudaMemcpyToSymbol(d_height, &height, sizeof(uint64_t));
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_parent_hash, parent_hash, 32);
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_merkle_root, merkle_root, 32);
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_target, target, 32);
    if (err != cudaSuccess) return false;

    // Reset cờ tìm kiếm
    uint64_t zero_nonce = 0;
    unsigned int zero_flag = 0;
    err = cudaMemcpyToSymbol(d_found_nonce, &zero_nonce, sizeof(uint64_t));
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_found_flag, &zero_flag, sizeof(unsigned int));
    if (err != cudaSuccess) return false;

    // Kích hoạt Kernel GPU quét Nonce song song (mỗi thread quét 32 nonces)
    mine_yona_kernel<<<number_of_blocks, threads_per_block>>>(base_nonce);

    err = cudaGetLastError();
    if (err != cudaSuccess) return false;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return false;

    // Lấy kết quả từ GPU
    unsigned int found_flag = 0;
    uint64_t found_nonce = 0;
    err = cudaMemcpyFromSymbol(&found_flag, d_found_flag, sizeof(unsigned int));
    if (err != cudaSuccess) return false;
    err = cudaMemcpyFromSymbol(&found_nonce, d_found_nonce, sizeof(uint64_t));
    if (err != cudaSuccess) return false;

    if (found_flag != 0) {
        *out_nonce = found_nonce;
        return true;
    }
    return false;
}
