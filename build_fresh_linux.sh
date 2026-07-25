#!/bin/bash
set -e
export PATH=$HOME/.cargo/bin:$PATH
export CARGO_TARGET_DIR=/tmp/cargo_target
export CXXFLAGS="-include cstdint"
unset CFLAGS

cd /mnt/d/hanhtrinhhocta-p/sssd/BTC
mkdir -p bin/linux

echo "[BUILD] 1. Compiling Rust Core (scl_server) for Linux..."
cargo build --release --manifest-path 0_shared_lib/Cargo.toml --bin scl_server
cp /tmp/cargo_target/release/scl_server bin/linux/scl_server

echo "[BUILD] 2. Compiling Rust CPU Miner (genz_miner) for Linux..."
cargo build --release --manifest-path 0_shared_lib/Cargo.toml --bin genz_miner
cp /tmp/cargo_target/release/genz_miner bin/linux/genz_miner

echo "[BUILD] 3. Compiling Go Node (YonaCode)..."
go build -o bin/linux/YonaCode ./6_user_interface/cmd/genz

echo "[BUILD] 4. Compiling Go CLI Node (cli_yona_code)..."
go build -o bin/linux/cli_yona_code ./6_user_interface/cmd/cli_node

echo "[BUILD] 5. Compiling Go Wallet Server (yona_wallet_server)..."
go build -o bin/linux/yona_wallet_server ./8_wallet_gateway

echo "[BUILD] 6. Compiling Go GPU Setup (yona_gpu_setup)..."
go build -o bin/linux/yona_gpu_setup ./6_user_interface/cmd/gpu_installer

echo "[BUILD] 7. Copying Linux GPU Miner (yona_gpu_miner)..."
cp 8_miner_gpu/build_linux/yona_gpu_miner bin/linux/yona_gpu_miner

echo "[SUCCESS] ALL LINUX BINARIES BUILT FRESH 100%!"
