@echo off
echo ===================================================
echo [MASTER BUILD] STARTING GLOBAL BUILD AND PACKAGING
echo ===================================================

:: 1. Rebuild Web UI (must be done before compiling Go binaries to embed the latest UI)
echo ---------------------------------------------------
echo [MASTER BUILD] Step 1: Rebuilding Web UI...
echo ---------------------------------------------------
cd 6_user_interface\web_ui
cmd /c "npm run build"
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Web UI build failed!
    cd ..\..
    exit /b %ERRORLEVEL%
)
cd ..\..

:: 2. Build Windows Binaries (Rust core and Go core)
echo ---------------------------------------------------
echo [MASTER BUILD] Step 2: Compiling Windows Binaries (Rust/Go Cores)...
echo ---------------------------------------------------
call .\build_all.bat
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Windows build failed!
    exit /b %ERRORLEVEL%
)

:: 3. Build Windows C++/CUDA GPU Miner
echo ---------------------------------------------------
echo [MASTER BUILD] Step 3: Compiling Windows GPU Miner (yona_gpu_miner.exe)...
echo ---------------------------------------------------
if not exist 8_miner_gpu\build mkdir 8_miner_gpu\build
cmake -S 8_miner_gpu -B 8_miner_gpu\build -DCMAKE_BUILD_TYPE=Release
cmake --build 8_miner_gpu\build --config Release
if %ERRORLEVEL% equ 0 (
    copy /Y 8_miner_gpu\build\Release\yona_gpu_miner.exe bin\yona_gpu_miner.exe
    echo Windows GPU Miner compiled and copied to bin\yona_gpu_miner.exe
) else (
    echo Windows GPU Miner compilation failed, skipping compilation but using existing binary if available.
)

:: 4. Build Linux Binaries (Rust core and Go core via cross-compilation)
echo ---------------------------------------------------
echo [MASTER BUILD] Step 4: Compiling Linux Binaries (Rust/Go Cores)...
echo ---------------------------------------------------
:: We temporarily bypass packaging in build_linux.bat so we can package it later via package_zips.py
:: To do this, we run the compilation steps directly
if not exist bin\linux mkdir bin\linux
del /Q bin\linux\*

set CC=%~dp0scratch\zig-cc.bat
set CXX=%~dp0scratch\zig-cxx.bat
set AR=%~dp0scratch\zig-ar.bat
set CARGO_TARGET_X86_64_UNKNOWN_LINUX_MUSL_LINKER=%~dp0scratch\zig-cc.bat
set RUSTFLAGS=-C link-self-contained=no
set CARGO_TARGET_DIR=d:\hanhtrinhhocta-p\sssd\target_linux

cd 0_shared_lib
cargo build --release --lib --bin scl_server --bin genz_miner --target x86_64-unknown-linux-musl
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Linux Rust compilation failed!
    cd ..
    exit /b %ERRORLEVEL%
)
cd ..

copy /Y d:\hanhtrinhhocta-p\sssd\target_linux\x86_64-unknown-linux-musl\release\scl_server bin\linux\scl_server
copy /Y d:\hanhtrinhhocta-p\sssd\target_linux\x86_64-unknown-linux-musl\release\genz_miner bin\linux\genz_miner

set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w" -o bin\linux\YonaCode .\6_user_interface\cmd\genz
go build -ldflags="-s -w" -o bin\linux\cli_yona_code .\6_user_interface\cmd\cli_node

set GOOS=
set GOARCH=
set CGO_ENABLED=
set RUSTFLAGS=

:: 5. Build Linux C++/CUDA GPU Miner inside WSL (Ubuntu)
echo ---------------------------------------------------
echo [MASTER BUILD] Step 5: Compiling Linux GPU Miner via WSL Ubuntu...
echo ---------------------------------------------------
wsl -d Ubuntu sh -c "cd /mnt/d/hanhtrinhhocta-p/sssd/BTC/8_miner_gpu && mkdir -p build_linux && cd build_linux && cmake -DCMAKE_BUILD_TYPE=Release .. && make -j4"
if %ERRORLEVEL% equ 0 (
    powershell Copy-Item -Path d:\hanhtrinhhocta-p\sssd\BTC\8_miner_gpu\build_linux\yona_gpu_miner -Destination d:\hanhtrinhhocta-p\sssd\BTC\bin\linux\yona_gpu_miner -Force
    echo Linux GPU Miner compiled and copied to bin\linux\yona_gpu_miner
) else (
    echo Linux GPU Miner compilation inside WSL failed!
)

:: 6. Package everything into distribution ZIPs using the Python script
echo ---------------------------------------------------
echo [MASTER BUILD] Step 6: Creating Unified ZIP Packages...
echo ---------------------------------------------------
python .\scratch\package_zips.py

echo ===================================================
echo [MASTER BUILD] SUCCESS: ALL PLATFORMS COMPILED AND PACKAGED!
echo [MASTER BUILD] Target zips are available in the .\zip\ folder.
echo ===================================================
