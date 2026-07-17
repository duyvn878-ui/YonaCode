#!/bin/bash
# =====================================================================
# YonaCode macOS Compilation Script (English version for developers)
# =====================================================================

set -e # Exit immediately if a command exits with a non-zero status

echo "==================================================="
echo "[BUILD-MACOS] 1. Rebuilding Web Wallet (React UI)..."
echo "==================================================="
cd 7_user_wallet
npm run build
cd ..

echo "==================================================="
echo "[BUILD-MACOS] 2. Compiling Rust Shared Core (scl_server)..."
echo "==================================================="
cargo build --release --manifest-path 0_shared_lib/Cargo.toml --bin scl_server

# Create bin and bbuild directories if they don't exist
mkdir -p bin
mkdir -p bbuild

# Copy compiled Rust server
cp 0_shared_lib/target/release/scl_server bin/scl_server
cp 0_shared_lib/target/release/scl_server bbuild/scl_server

echo "==================================================="
echo "[BUILD-MACOS] 3. Compiling Go Node (YonaCode)..."
echo "==================================================="
go build -o bin/YonaCode ./6_user_interface/cmd/genz
cp bin/YonaCode bbuild/YonaCode

echo "==================================================="
echo "[BUILD-MACOS] 4. Compiling Go Wallet Gateway (yona_wallet_server)..."
echo "==================================================="
go build -o bin/yona_wallet_server ./8_wallet_gateway
cp bin/yona_wallet_server bbuild/yona_wallet_server

echo "==================================================="
echo "[BUILD-MACOS] SUCCESS: MACOS BUILD COMPLETED!"
echo "==================================================="
echo "(Note: macOS GPU mining is not officially supported due to driver compatibility, please use CPU mining or run GPU miner on Linux/Windows)"
