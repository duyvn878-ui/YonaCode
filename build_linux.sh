#!/bin/bash
# Tên file: build_linux.sh
# Tính năng chi tiết: Script tự động biên dịch, thiết lập phân quyền thực thi và đóng gói toàn bộ dự án YonaCode (gồm Web UI, Go Node, Rust Core và C++/CUDA GPU Miner) trực tiếp trên Linux.
# Ngày khởi tạo: 12/07/2026
# Cơ chế vận hành:
#   1. Biên dịch Rust Kernel (scl_server & genz_miner) qua Cargo.
#   2. Biên dịch GPU Miner (yona_gpu_miner) qua CMake và CUDA nvcc (nếu có môi trường CUDA).
#   3. Xây dựng Web UI thông qua npm.
#   4. Biên dịch Go Node (YonaCode & cli_yona_code).
#   5. Đóng gói phân phối tất cả binaries và cơ sở dữ liệu gốc thành zip/YonaCode_Linux.zip.

set -e

echo "==================================================="
echo "🛠️  BIÊN DỊCH & ĐÓNG GÓI YonaCode TRÊN LINUX"
echo "==================================================="

# 1. Tạo thư mục đầu ra
mkdir -p bin/linux
rm -f bin/linux/*

# 2. Biên dịch Rust Kernel
echo "[1/5] Đang biên dịch Rust Kernel..."
cd 0_shared_lib
cargo build --release --bin scl_server --bin genz_miner
cd ..
cp 0_shared_lib/target/release/scl_server bin/linux/scl_server
cp 0_shared_lib/target/release/genz_miner bin/linux/genz_miner

# 3. Biên dịch C++/CUDA GPU Miner (nếu có NVIDIA CUDA Toolkit và CMake)
echo "[2/5] Đang biên dịch GPU Miner..."
if command -v nvcc &> /dev/null && command -v cmake &> /dev/null; then
    cd 8_miner_gpu
    mkdir -p build
    cd build
    cmake -DCMAKE_BUILD_TYPE=Release ..
    make -j$(nproc)
    cd ../..
    cp 8_miner_gpu/build/yona_gpu_miner bin/linux/yona_gpu_miner
    echo "✅ Biên dịch thành công GPU Miner tại bin/linux/yona_gpu_miner"
else
    echo "⚠️  Không tìm thấy nvcc hoặc cmake. Bỏ qua biên dịch GPU Miner."
    echo "💡 Gợi ý: Hãy cài đặt NVIDIA CUDA Toolkit và CMake để hỗ trợ tự động build GPU Miner."
fi

# 4. Biên dịch Web UI
echo "[3/5] Đang xây dựng Web UI..."
if command -v npm &> /dev/null; then
    cd 6_user_interface/web_ui
    npm install
    npm run build
    cd ../..
else
    echo "⚠️  Không tìm thấy npm. Bỏ qua xây dựng Web UI mới (sử dụng bản dựng sẵn nhúng trong mã nguồn)."
fi

# 5. Biên dịch Go Core
echo "[4/5] Đang biên dịch Go Core cho Linux..."
go build -ldflags="-s -w" -o bin/linux/YonaCode ./6_user_interface/cmd/genz
go build -ldflags="-s -w" -o bin/linux/cli_yona_code ./6_user_interface/cmd/cli_node

# 6. Đóng gói phân phối Linux ZIP
echo "[5/5] Đóng gói thành phẩm YonaCode_Linux.zip..."
mkdir -p zip
temp_pack="temp_linux_pack"
rm -rf $temp_pack
mkdir -p $temp_pack/node/scl

# Sao chép các tệp chạy và dữ liệu
cp bin/linux/YonaCode $temp_pack/
cp bin/linux/cli_yona_code $temp_pack/
cp bin/linux/scl_server $temp_pack/
cp bin/linux/genz_miner $temp_pack/
if [ -f bin/linux/yona_gpu_miner ]; then
    cp bin/linux/yona_gpu_miner $temp_pack/
fi

# Sao chép dữ liệu khởi thủy của SCL (nếu có)
if [ -d node/scl ]; then
    cp -r node/scl/* $temp_pack/node/scl/ 2>/dev/null || true
fi

# Phân quyền thực thi chuẩn cho môi trường Linux
chmod +x $temp_pack/YonaCode
chmod +x $temp_pack/cli_yona_code
chmod +x $temp_pack/scl_server
chmod +x $temp_pack/genz_miner
if [ -f $temp_pack/yona_gpu_miner ]; then
    chmod +x $temp_pack/yona_gpu_miner
fi

# Nén
if command -v zip &> /dev/null; then
    cd $temp_pack
    zip -r ../zip/YonaCode_Linux.zip *
    cd ..
    echo "✅ Đóng gói thành công tại: zip/YonaCode_Linux.zip"
else
    # Fallback sang tar.gz nếu hệ thống không có zip
    cd $temp_pack
    tar -czf ../zip/YonaCode_Linux.tar.gz *
    cd ..
    echo "✅ Đóng gói thành công tại: zip/YonaCode_Linux.tar.gz (do thiếu lệnh 'zip')"
fi

# Dọn dẹp
rm -rf $temp_pack

echo "==================================================="
echo "🎉 HOÀN THÀNH BIÊN DỊCH VÀ ĐÓNG GÓI LINUX!"
echo "==================================================="
