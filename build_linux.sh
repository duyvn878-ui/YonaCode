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
echo "[BUILD-LINUX] 5. Compiling Linux GPU Miner (CMake)..."
echo "==================================================="
mkdir -p 8_miner_gpu/build
if cmake -S 8_miner_gpu -B 8_miner_gpu/build -DCMAKE_BUILD_TYPE=Release; then
    if cmake --build 8_miner_gpu/build --config Release; then
        cp 8_miner_gpu/build/yona_gpu_miner bin/yona_gpu_miner
        echo "[SUCCESS] Linux GPU Miner compiled successfully!"
    else
        echo "[WARN] Linux GPU Miner compilation failed!"
    fi
else
    echo "[WARN] CMake configuration failed for Linux GPU Miner!"
fi

echo "==================================================="
echo "[BUILD-LINUX] SUCCESS: LINUX BUILD COMPLETED!"
echo "==================================================="
