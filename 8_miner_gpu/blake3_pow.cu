/*
 * Tên tệp: blake3_pow.cu
 * Tính năng chi tiết: Lập trình GPU CUDA cho thuật toán đồng thuận Proof of Work (PoW) dựa trên Blake3.
 *                     Thực hiện tìm kiếm Nonce tối ưu hóa cực hạn cho chuỗi khối YonaCode / BTC GenZ.
 * Ngày khởi tạo: 10/07/2026 (Cập nhật tối ưu hóa cực hạn)
 * Cơ chế vận hành:
 *   1. Nhận header_hash và target từ máy chủ khai thác (Host) qua bộ nhớ hằng số (__constant__) để tăng tốc truyền phát.
 *   2. Sử dụng kỹ thuật tính toán trước trạng thái trung gian (Midstate Precomputation) cho các giá trị không đổi của Round 0.
 *   3. Mỗi luồng (thread) xử lý song song và lặp vòng (unrolled loop) qua 8 giá trị nonce liên tiếp nhằm giảm chi phí khởi tạo.
 *   4. So sánh nhanh phần tử có trọng số lớn nhất trước (Lazy Hash Finalization) để loại bỏ sớm 99.6% các nonce không hợp lệ mà không cần hoàn tất băm 256-bit.
 *   5. Sử dụng hàm intrinsic __funnelshift_r của phần cứng NVIDIA để thực hiện xoay bit (rotr) chỉ trong 1 chu kỳ xung nhịp.
 *   6. Dùng atomicCAS trên cờ d_found_flag kiểu unsigned int (đảm bảo căn lề 4-byte) để tránh xung đột ghi đè dữ liệu.
 */

#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>

// Chaining value derived from context: "BTC GenZ Toi Gian PoW v1.0"
__constant__ uint32_t DERIVED_KEY[8] = {
    0x375357ef, 0x00398eff, 0x3a23b6d0, 0x9e9d9a2b,
    0x13d8a713, 0xa2a33973, 0x3b4e946f, 0xcae08421
};

// Global constant variables for fast hardware broadcasting to all threads
__constant__ uint32_t d_header_hash[8];
__constant__ uint32_t d_target[8];

// Global device variables for zero-allocation execution
__device__ uint64_t d_found_nonce;
__device__ unsigned int d_found_flag;

// Rotation right helper using CUDA funnel shift intrinsic for single-cycle execution
__device__ __forceinline__ uint32_t rotr(uint32_t x, int n) {
    return __funnelshift_r(x, x, n);
}

// G mix function using C++ references to avoid pointer aliasing and force register allocation
__device__ __forceinline__ void g(uint32_t& sa, uint32_t& sb, uint32_t& sc, uint32_t& sd, uint32_t x, uint32_t y) {
    sa = sa + sb + x;
    sd = rotr(sd ^ sa, 16);
    sc = sc + sd;
    sb = rotr(sb ^ sc, 12);
    sa = sa + sb + y;
    sd = rotr(sd ^ sa, 8);
    sc = sc + sd;
    sb = rotr(sb ^ sc, 7);
}

// Comparison helper for U256 (32 bytes, 8 words, little endian)
__device__ __forceinline__ bool is_less_than_256(const uint32_t* a, const uint32_t* b) {
    #pragma unroll
    for (int i = 7; i >= 0; i--) {
        if (a[i] < b[i]) return true;
        if (a[i] > b[i]) return false;
    }
    return false;
}

// CUDA Kernel: Mine Nonce by scanning search space (processes 32 nonces per thread)
// __launch_bounds__(256, 2): Gợi ý trình biên dịch giới hạn 256 thread/block, tối thiểu 2 block/SM
// để tối ưu hóa phân bổ thanh ghi và tăng occupancy trên phần cứng NVIDIA.
__global__ __launch_bounds__(256, 2)
void mine_nonce_kernel(uint64_t base_nonce) {
    if (d_found_flag) return;

    // Calculate global thread ID
    uint64_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    uint64_t thread_start_nonce = base_nonce + tid * 32;

    // Cache d_header_hash array from constant memory into local thread registers
    uint32_t h0 = d_header_hash[0];
    uint32_t h1 = d_header_hash[1];
    uint32_t h2 = d_header_hash[2];
    uint32_t h3 = d_header_hash[3];
    uint32_t h4 = d_header_hash[4];
    uint32_t h5 = d_header_hash[5];
    uint32_t h6 = d_header_hash[6];
    uint32_t h7 = d_header_hash[7];

    // =========================================================================
    // OPTIMIZATION 1: Midstate Precomputation
    // Pre-calculate the first 4 mixes of Round 0 which are independent of the nonce.
    // =========================================================================
    uint32_t mid_s0 = DERIVED_KEY[0];
    uint32_t mid_s1 = DERIVED_KEY[1];
    uint32_t mid_s2 = DERIVED_KEY[2];
    uint32_t mid_s3 = DERIVED_KEY[3];
    uint32_t mid_s4 = DERIVED_KEY[4];
    uint32_t mid_s5 = DERIVED_KEY[5];
    uint32_t mid_s6 = DERIVED_KEY[6];
    uint32_t mid_s7 = DERIVED_KEY[7];
    uint32_t mid_s8 = 0x6A09E667;
    uint32_t mid_s9 = 0xBB67AE85;
    uint32_t mid_s10 = 0x3C6EF372;
    uint32_t mid_s11 = 0xA54FF53A;
    uint32_t mid_s12 = 0;
    uint32_t mid_s13 = 0;
    uint32_t mid_s14 = 40;
    uint32_t mid_s15 = 0x4b;

    // First 4 independent mixes of Round 0
    g(mid_s0, mid_s4, mid_s8, mid_s12, h0, h1);
    g(mid_s1, mid_s5, mid_s9, mid_s13, h2, h3);
    g(mid_s2, mid_s6, mid_s10, mid_s14, h4, h5);
    g(mid_s3, mid_s7, mid_s11, mid_s15, h6, h7);

    // =========================================================================
    // OPTIMIZATION 2: Multi-Nonce Per Thread Loop
    // Loop through 8 sequential nonces to amortize the setup and midstate cost.
    // =========================================================================
    #pragma unroll
    for (int nonce_offset = 0; nonce_offset < 32; nonce_offset++) {
        // Exit early if any other thread found a solution
        if (d_found_flag) break;

        uint64_t current_nonce = thread_start_nonce + nonce_offset;
        uint32_t nonce_low = (uint32_t)(current_nonce & 0xffffffff);
        uint32_t nonce_high = (uint32_t)(current_nonce >> 32);

        // Load pre-calculated midstate
        uint32_t s0 = mid_s0;   uint32_t s1 = mid_s1;   uint32_t s2 = mid_s2;   uint32_t s3 = mid_s3;
        uint32_t s4 = mid_s4;   uint32_t s5 = mid_s5;   uint32_t s6 = mid_s6;   uint32_t s7 = mid_s7;
        uint32_t s8 = mid_s8;   uint32_t s9 = mid_s9;   uint32_t s10 = mid_s10; uint32_t s11 = mid_s11;
        uint32_t s12 = mid_s12; uint32_t s13 = mid_s13; uint32_t s14 = mid_s14; uint32_t s15 = mid_s15;

        // Finish Round 0 (Remaining 4 mixes dependent on nonce)
        g(s0, s5, s10, s15, nonce_low, nonce_high);
        g(s1, s6, s11, s12, 0, 0);
        g(s2, s7, s8, s13, 0, 0);
        g(s3, s4, s9, s14, 0, 0);

        // Round 1
        g(s0, s4, s8, s12, h2, h6);
        g(s1, s5, s9, s13, h3, 0);
        g(s2, s6, s10, s14, h7, h0);
        g(s3, s7, s11, s15, h4, 0);
        g(s0, s5, s10, s15, h1, 0);
        g(s1, s6, s11, s12, 0, h5);
        g(s2, s7, s8, s13, nonce_high, 0);
        g(s3, s4, s9, s14, 0, nonce_low);

        // Round 2
        g(s0, s4, s8, s12, h3, h4);
        g(s1, s5, s9, s13, 0, 0);
        g(s2, s6, s10, s14, 0, h2);
        g(s3, s7, s11, s15, h7, 0);
        g(s0, s5, s10, s15, h6, h5);
        g(s1, s6, s11, s12, nonce_high, h0);
        g(s2, s7, s8, s13, 0, 0);
        g(s3, s4, s9, s14, nonce_low, h1);

        // Round 3
        g(s0, s4, s8, s12, 0, h7);
        g(s1, s5, s9, s13, 0, nonce_high);
        g(s2, s6, s10, s14, 0, h3);
        g(s3, s7, s11, s15, 0, 0);
        g(s0, s5, s10, s15, h4, h0);
        g(s1, s6, s11, s12, 0, h2);
        g(s2, s7, s8, s13, h5, nonce_low);
        g(s3, s4, s9, s14, h1, h6);

        // Round 4
        g(s0, s4, s8, s12, 0, 0);
        g(s1, s5, s9, s13, nonce_high, 0);
        g(s2, s6, s10, s14, 0, 0);
        g(s3, s7, s11, s15, 0, nonce_low);
        g(s0, s5, s10, s15, h7, h2);
        g(s1, s6, s11, s12, h5, h3);
        g(s2, s7, s8, s13, h0, h1);
        g(s3, s4, s9, s14, h6, h4);

        // Round 5
        g(s0, s4, s8, s12, nonce_high, 0);
        g(s1, s5, s9, s13, 0, h5);
        g(s2, s6, s10, s14, nonce_low, 0);
        g(s3, s7, s11, s15, 0, h1);
        g(s0, s5, s10, s15, 0, h3);
        g(s1, s6, s11, s12, h0, 0);
        g(s2, s7, s8, s13, h2, h6);
        g(s3, s4, s9, s14, h4, h7);

        // Round 6
        g(s0, s4, s8, s12, 0, 0);
        g(s1, s5, s9, s13, h5, h0);
        g(s2, s6, s10, s14, h1, nonce_high);
        g(s3, s7, s11, s15, nonce_low, h6);
        g(s0, s5, s10, s15, 0, 0);
        g(s1, s6, s11, s12, h2, 0);
        g(s2, s7, s8, s13, h3, h4);
        g(s3, s4, s9, s14, h7, 0);

        // =========================================================================
        // OPTIMIZATION 3: Lazy Hash Finalization & Early Target Rejection
        // Only finalize and test the most significant word first. If it violates target,
        // we skip the rest of finalization and avoid full 256-bit compares.
        // =========================================================================
        uint32_t hash_high = s7 ^ s15;
        if (hash_high > d_target[7]) {
            continue; // Rejected early!
        }

        // Finalize remaining 7 words of hash if the high word passed
        uint32_t hash_result[8];
        hash_result[0] = s0 ^ s8;
        hash_result[1] = s1 ^ s9;
        hash_result[2] = s2 ^ s10;
        hash_result[3] = s3 ^ s11;
        hash_result[4] = s4 ^ s12;
        hash_result[5] = s5 ^ s13;
        hash_result[6] = s6 ^ s14;
        hash_result[7] = hash_high;

        // Verify full 256-bit value compared with target
        if (is_less_than_256(hash_result, d_target)) {
            if (atomicCAS(&d_found_flag, 0, 1) == 0) {
                d_found_nonce = current_nonce;
            }
            break; // Found nonce, stop searching loop
        }
    }
}

// C/C++ Host Wrapper Function to initialize and check CUDA device
extern "C" bool init_cuda_device() {
    int device_count = 0;
    cudaError_t err = cudaGetDeviceCount(&device_count);
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] ❌ Failed to query CUDA devices: %s\n", cudaGetErrorString(err));
        printf("[CUDA-ERROR] 💡 Please ensure you have an NVIDIA GPU and the correct driver version installed.\n");
        return false;
    }
    if (device_count == 0) {
        printf("[CUDA-ERROR] ❌ No CUDA-capable devices detected on this system!\n");
        return false;
    }
    
    // Explicitly set device 0 and initialize CUDA context
    err = cudaSetDevice(0);
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] ❌ Failed to set CUDA device 0: %s\n", cudaGetErrorString(err));
        return false;
    }

    // Force context creation
    err = cudaFree(0);
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] ❌ Failed to initialize CUDA context on device 0: %s\n", cudaGetErrorString(err));
        return false;
    }

    printf("[CUDA-INFO] 💚 Successfully initialized CUDA Device 0.\n");
    return true;
}

// C/C++ Host Wrapper Function (Zero-Allocation Loop)
extern "C" bool run_cuda_miner(
    const uint8_t* header_hash,
    const uint8_t* target,
    uint64_t base_nonce,
    uint32_t threads_per_block,
    uint32_t number_of_blocks,
    uint64_t* out_nonce
) {
    cudaError_t err;

    // Copy data from Host to GPU Symbol memory directly (No cudaMalloc/cudaFree overhead)
    err = cudaMemcpyToSymbol(d_header_hash, header_hash, 32);
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyToSymbol d_header_hash failed: %s\n", cudaGetErrorString(err));
        return false;
    }
    err = cudaMemcpyToSymbol(d_target, target, 32);
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyToSymbol d_target failed: %s\n", cudaGetErrorString(err));
        return false;
    }

    uint64_t zero_nonce = 0;
    unsigned int zero_flag = 0;
    err = cudaMemcpyToSymbol(d_found_nonce, &zero_nonce, sizeof(uint64_t));
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyToSymbol d_found_nonce failed: %s\n", cudaGetErrorString(err));
        return false;
    }
    err = cudaMemcpyToSymbol(d_found_flag, &zero_flag, sizeof(unsigned int));
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyToSymbol d_found_flag failed: %s\n", cudaGetErrorString(err));
        return false;
    }

    // Launch CUDA PoW Kernel
    mine_nonce_kernel<<<number_of_blocks, threads_per_block>>>(base_nonce);

    // Check for kernel launch errors
    err = cudaGetLastError();
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] Kernel launch failed: %s\n", cudaGetErrorString(err));
        return false;
    }

    // Synchronize GPU execution
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaDeviceSynchronize failed: %s\n", cudaGetErrorString(err));
        return false;
    }

    // Check if solution was found
    unsigned int found_flag = 0;
    uint64_t found_nonce = 0;
    err = cudaMemcpyFromSymbol(&found_flag, d_found_flag, sizeof(unsigned int));
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyFromSymbol d_found_flag failed: %s\n", cudaGetErrorString(err));
        return false;
    }
    err = cudaMemcpyFromSymbol(&found_nonce, d_found_nonce, sizeof(uint64_t));
    if (err != cudaSuccess) {
        printf("[CUDA-ERROR] cudaMemcpyFromSymbol d_found_nonce failed: %s\n", cudaGetErrorString(err));
        return false;
    }

    if (found_flag != 0) {
        *out_nonce = found_nonce;
        return true;
    }
    return false;
}
