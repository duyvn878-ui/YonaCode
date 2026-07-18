cmake_minimum_required(VERSION 3.18)
project(yona_gpu_miner LANGUAGES CXX CUDA)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Liên kết tĩnh thư viện CUDA Runtime để tránh phụ thuộc vào cudart64_xx.dll
set(CMAKE_CUDA_RUNTIME_LIBRARY Static)

# Hỗ trợ biên dịch cho các kiến trúc GPU NVIDIA phổ biến (GTX 16xx, RTX 20xx, 30xx, 40xx)
set(CMAKE_CUDA_ARCHITECTURES "75;80;86;89")

add_executable(yona_gpu_miner main.cpp blake3_pow.cu yona_pow.cu)

# Liên kết các thư viện mạng trên Windows
if(WIN32)
    target_link_libraries(yona_gpu_miner PRIVATE ws2_32)
endif()

# Tối ưu hóa hiệu năng biên dịch CUDA
set_target_properties(yona_gpu_miner PROPERTIES
    CUDA_SEPARABLE_COMPILATION OFF
    CUDA_LINK_LIBRARIES_SHARED OFF
)

# Thêm mục tiêu Unit Test để kiểm thử
add_executable(yona_gpu_test test_miner.cpp blake3_pow.cu yona_pow.cu)
if(WIN32)
    target_link_libraries(yona_gpu_test PRIVATE ws2_32)
endif()
