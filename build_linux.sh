#!/bin/bash
# =====================================================================
# YonaCode Linux Compilation Script (English version for developers)
# =====================================================================

set -e # Exit immediately if a command exits with a non-zero status

echo "==================================================="
echo "[BUILD-LINUX] 1. Rebuilding Web Wallet (React UI)..."
echo "==================================================="
cd 7_user_wallet
npm run build
cd ..

echo "==================================================="
echo "[BUILD-LINUX] 2. Compiling Rust Shared Core (scl_server)..."
echo "==================================================="
cargo build --release --manifest-path 0_shared_lib/Cargo.toml --bin scl_server

# Create bin and bbuild directories if they don't exist
mkdir -p bin
mkdir -p bbuild

# Copy compiled Rust server
cp 0_shared_lib/target/release/scl_server bin/scl_server
cp 0_shared_lib/target/release/scl_server bbuild/scl_server

echo "==================================================="
echo "[BUILD-LINUX] 3. Compiling Go Node (YonaCode)..."
echo "==================================================="
go build -o bin/YonaCode ./6_user_interface/cmd/genz
cp bin/YonaCode bbuild/YonaCode

echo "==================================================="
echo "[BUILD-LINUX] 4. Compiling Go Wallet Gateway (yona_wallet_server)..."
echo "==================================================="
go build -o bin/yona_wallet_server ./8_wallet_gateway
cp bin/yona_wallet_server bbuild/yona_wallet_server

echo "==================================================="
echo "[BUILD-LINUX] 4.5. Compiling Go GPU Setup Tool (yona_gpu_setup)..."
echo "==================================================="
go build -o bin/yona_gpu_setup ./6_user_interface/cmd/gpu_installer
cp bin/yona_gpu_setup bbuild/yona_gpu_setup


echo "==================================================="
echo "[BUILD-LINUX] 5. Compiling Linux GPU Miner (CMake)..."
echo "==================================================="
mkdir -p 8_miner_gpu/build
if cmake -S 8_miner_gpu -B 8_miner_gpu/build -DCMAKE_BUILD_TYPE=Release; then
    if cmake --build 8_miner_gpu/build --config Release; then
        cp 8_miner_gpu/build/yona_gpu_miner bin/yona_gpu_miner
        echo "[SUCCESS] Linux GPU Miner compiled successfully!"
        
        # Package HiveOS Custom Miner ZIP
        echo "[BUILD-LINUX] Packaging HiveOS Custom Miner ZIP..."
        mkdir -p bbuild/yona_gpu_miner_hiveos
        cp bin/yona_gpu_miner bbuild/yona_gpu_miner_hiveos/
        cp 10_miner_hiveos/h-manifest.conf bbuild/yona_gpu_miner_hiveos/
        cp 10_miner_hiveos/h-run.sh bbuild/yona_gpu_miner_hiveos/
        cp 10_miner_hiveos/h-stats.sh bbuild/yona_gpu_miner_hiveos/
        
        # Nén gói cài đặt và dọn dẹp thư mục tạm
        cd bbuild
        zip -r ../zip/yona_gpu_miner_hiveos.zip yona_gpu_miner_hiveos
        rm -rf yona_gpu_miner_hiveos
        cd ..
        echo "[SUCCESS] HiveOS Custom Miner package zip/yona_gpu_miner_hiveos.zip created!"
    else
        echo "[WARN] Linux GPU Miner compilation failed!"
    fi
else
    echo "[WARN] CMake configuration failed for Linux GPU Miner!"
fi

echo "==================================================="
echo "[BUILD-LINUX] SUCCESS: LINUX BUILD COMPLETED!"
echo "==================================================="
