/*
 * Tên tệp: yona_pow.cu
 * Tính năng chi tiết: Lập trình GPU CUDA tối ưu hóa cực hạn cho thuật toán băm Yona Hash.
 *                     Sử dụng các kỹ thuật:
 *                     1. Midstate Precomputation: Tính toán trước 4 phép trộn cố định của Round 0 ngoài vòng lặp nonce.
 *                     2. Multi-Nonce Per Thread Loop: Quét song song 32 nonces liên tiếp trên mỗi thread.
 *                     3. Intrinsic Funnel Shift: Thực thi dịch bit trong đúng 1 chu kỳ máy.
 *                     4. Lazy Target Rejection: So sánh từ word có trọng số lớn nhất để thoát sớm.
 * Ngày cập nhật: 31/07/2026
 */

#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>

// Các cờ điều hướng Blake3 / Yona
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
__constant__ uint32_t d_header_hash[8];
__constant__ uint32_t d_target[8];

// Biến toàn cục GPU nhận diện kết quả
__device__ uint64_t d_found_nonce;
__device__ unsigned int d_found_flag;

// Hàm xoay bit sử dụng intrinsic funnel shift của NVIDIA
__device__ __forceinline__ uint32_t rotr(uint32_t x, int n) {
    return __funnelshift_r(x, x, n);
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

// So sánh 256-bit (a <= b) - little endian words
__device__ __forceinline__ bool is_less_than_256(const uint32_t* a, const uint32_t* b) {
    #pragma unroll
    for (int i = 7; i >= 0; i--) {
        if (a[i] < b[i]) return true;
        if (a[i] > b[i]) return false;
    }
    return true;
}

// Hàm thiết bị thực hiện băm Yona Hash cho 40 bytes
__device__ __forceinline__ void yona_hash_device(
    uint32_t h0, uint32_t h1, uint32_t h2, uint32_t h3,
    uint32_t h4, uint32_t h5, uint32_t h6, uint32_t h7,
    uint64_t nonce,
    uint32_t* out_hash
) {
    uint32_t nonce_low = (uint32_t)(nonce & 0xffffffff);
    uint32_t nonce_high = (uint32_t)(nonce >> 32);

    uint32_t s[16];
    s[0] = IV[0];   s[1] = IV[1];   s[2] = IV[2];   s[3] = IV[3];
    s[4] = IV[4];   s[5] = IV[5];   s[6] = IV[6];   s[7] = IV[7];
    s[8] = IV[0];   s[9] = IV[1];   s[10] = IV[2];  s[11] = IV[3];
    s[12] = 0;      s[13] = 0;
    s[14] = 40;     s[15] = CHUNK_START | CHUNK_END | ROOT;

    g(s[0], s[4], s[8],  s[12], h0, h1);
    g(s[1], s[5], s[9],  s[13], h2, h3);
    g(s[2], s[6], s[10], s[14], h4, h5);
    g(s[3], s[7], s[11], s[15], h6, h7);

    g(s[0], s[5], s[10], s[15], nonce_low, nonce_high);
    g(s[1], s[6], s[11], s[12], 0, 0);
    g(s[2], s[7], s[8],  s[13], 0, 0);
    g(s[3], s[4], s[9],  s[14], 0, 0);

    // Mảng message cho Round 1-6 hoán vị
    uint32_t m[16] = {
        h0, h1, h2, h3, h4, h5, h6, h7,
        nonce_low, nonce_high, 0, 0, 0, 0, 0, 0
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

    // XOR kết quả chaining value và states
    #pragma unroll
    for (int i = 0; i < 8; i++) {
        out_hash[i] = s[i] ^ s[i + 8];
    }
}

// CUDA Kernel: Đào nonce tối ưu hóa cực hạn
__global__ __launch_bounds__(256, 2)
void mine_yona_kernel(uint64_t base_nonce) {
    if (d_found_flag) return;

    uint64_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    uint64_t thread_start_nonce = base_nonce + tid * 32;

    // Cache header hash
    uint32_t h0 = d_header_hash[0]; uint32_t h1 = d_header_hash[1];
    uint32_t h2 = d_header_hash[2]; uint32_t h3 = d_header_hash[3];
    uint32_t h4 = d_header_hash[4]; uint32_t h5 = d_header_hash[5];
    uint32_t h6 = d_header_hash[6]; uint32_t h7 = d_header_hash[7];

    // =========================================================================
    // KỸ THUẬT 1: Midstate Precomputation (Round 0)
    // Tính toán trước 4 phép trộn đầu tiên của Round 0 chỉ phụ thuộc header_hash
    // =========================================================================
    uint32_t mid_s0 = IV[0]; uint32_t mid_s1 = IV[1]; uint32_t mid_s2 = IV[2]; uint32_t mid_s3 = IV[3];
    uint32_t mid_s4 = IV[4]; uint32_t mid_s5 = IV[5]; uint32_t mid_s6 = IV[6]; uint32_t mid_s7 = IV[7];
    uint32_t mid_s8 = IV[0]; uint32_t mid_s9 = IV[1]; uint32_t mid_s10 = IV[2]; uint32_t mid_s11 = IV[3];
    uint32_t mid_s12 = 0;    uint32_t mid_s13 = 0;
    uint32_t mid_s14 = 40;   uint32_t mid_s15 = CHUNK_START | CHUNK_END | ROOT; // len = 40, flags = 0x0b

    g(mid_s0, mid_s4, mid_s8,  mid_s12, h0, h1);
    g(mid_s1, mid_s5, mid_s9,  mid_s13, h2, h3);
    g(mid_s2, mid_s6, mid_s10, mid_s14, h4, h5);
    g(mid_s3, mid_s7, mid_s11, mid_s15, h6, h7);

    // =========================================================================
    // KỸ THUẬT 2: Multi-Nonce Per Thread Loop
    // =========================================================================
    #pragma unroll
    for (int nonce_offset = 0; nonce_offset < 32; nonce_offset++) {
        if (d_found_flag) break;

        uint64_t current_nonce = thread_start_nonce + nonce_offset;
        uint32_t nonce_low = (uint32_t)(current_nonce & 0xffffffff);
        uint32_t nonce_high = (uint32_t)(current_nonce >> 32);

        // Khôi phục midstate
        uint32_t s[16];
        s[0] = mid_s0;   s[1] = mid_s1;   s[2] = mid_s2;   s[3] = mid_s3;
        s[4] = mid_s4;   s[5] = mid_s5;   s[6] = mid_s6;   s[7] = mid_s7;
        s[8] = mid_s8;   s[9] = mid_s9;   s[10] = mid_s10; s[11] = mid_s11;
        s[12] = mid_s12; s[13] = mid_s13; s[14] = mid_s14; s[15] = mid_s15;

        // Hoàn thành nốt Round 0 (trộn nonce và padding)
        g(s[0], s[5], s[10], s[15], nonce_low, nonce_high);
        g(s[1], s[6], s[11], s[12], 0, 0);
        g(s[2], s[7], s[8],  s[13], 0, 0);
        g(s[3], s[4], s[9],  s[14], 0, 0);

        // Mảng message cho Round 1-6 hoán vị
        uint32_t m[16] = {
            h0, h1, h2, h3, h4, h5, h6, h7,
            nonce_low, nonce_high, 0, 0, 0, 0, 0, 0
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

        // XOR kết quả chaining value và states
        uint32_t final_hash[8];
        #pragma unroll
        for (int i = 0; i < 8; i++) {
            final_hash[i] = s[i] ^ s[i + 8];
        }

        // KỸ THUẬT 4: Lazy Target Rejection
        if (final_hash[7] > d_target[7]) {
            continue;
        }

        // So sánh 256-bit đầy đủ
        if (is_less_than_256(final_hash, d_target)) {
            if (atomicCAS(&d_found_flag, 0, 1) == 0) {
                d_found_nonce = current_nonce;
            }
            break;
        }
    }
}

// Kernel đặc biệt hỗ trợ kiểm thử băm chính xác từ unit test
__global__ void test_yona_single_kernel(const uint32_t* header_hash, uint64_t nonce, uint32_t* out_hash) {
    yona_hash_device(
        header_hash[0], header_hash[1], header_hash[2], header_hash[3],
        header_hash[4], header_hash[5], header_hash[6], header_hash[7],
        nonce, out_hash
    );
}

extern "C" bool init_yona_cuda_device_id(int device_id) {
    int device_count = 0;
    cudaError_t err = cudaGetDeviceCount(&device_count);
    if (err != cudaSuccess || device_count == 0) {
        printf("[CUDA-ERROR] No NVIDIA GPU detected.\n");
        return false;
    }
    if (device_id < 0 || device_id >= device_count) {
        printf("[CUDA-ERROR] Invalid GPU device ID %d\n", device_id);
        return false;
    }
    cudaSetDevice(device_id);
    cudaFree(0);
    printf("[CUDA-INFO] Yona Hash GPU Miner successfully initialized Device %d.\n", device_id);
    return true;
}

extern "C" bool init_yona_cuda_device() {
    return init_yona_cuda_device_id(0);
}

extern "C" bool run_yona_cuda_miner_on_device(
    int device_id,
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
) {
    cudaError_t err = cudaSetDevice(device_id);
    if (err != cudaSuccess) return false;

    err = cudaMemcpyToSymbol(d_header_hash, header_hash, 32);
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_target, target, 32);
    if (err != cudaSuccess) return false;

    uint64_t zero_nonce = 0;
    unsigned int zero_flag = 0;
    err = cudaMemcpyToSymbol(d_found_nonce, &zero_nonce, sizeof(uint64_t));
    if (err != cudaSuccess) return false;
    err = cudaMemcpyToSymbol(d_found_flag, &zero_flag, sizeof(unsigned int));
    if (err != cudaSuccess) return false;

    mine_yona_kernel<<<number_of_blocks, threads_per_block>>>(base_nonce);

    err = cudaGetLastError();
    if (err != cudaSuccess) return false;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return false;

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

extern "C" bool run_yona_cuda_miner(
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
) {
    return run_yona_cuda_miner_on_device(0, header_hash, target, base_nonce, threads_per_block, number_of_blocks, out_nonce);
}

extern "C" void run_yona_single_hash(
    const uint8_t* header_hash,
    uint64_t nonce,
    uint8_t* out_hash
) {
    uint32_t* d_header = nullptr;
    uint32_t* d_out = nullptr;
    cudaMalloc(&d_header, 32);
    cudaMalloc(&d_out, 32);

    cudaMemcpy(d_header, header_hash, 32, cudaMemcpyHostToDevice);

    test_yona_single_kernel<<<1, 1>>>(d_header, nonce, d_out);

    cudaMemcpy(out_hash, d_out, 32, cudaMemcpyDeviceToHost);

    cudaFree(d_header);
    cudaFree(d_out);
}
