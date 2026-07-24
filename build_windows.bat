@echo off
:: =====================================================================
:: YonaCode Windows Compilation Script (English version for developers)
:: =====================================================================

echo ===================================================
echo [BUILD-WIN] 1. Rebuilding Web Wallet (React UI)...
echo ===================================================
cd 7_user_wallet
cmd /c "npm run build"
if %ERRORLEVEL% neq 0 (
    echo [ERROR] React UI build failed!
    cd ..
    exit /b %ERRORLEVEL%
)
cd ..

echo ===================================================
echo [BUILD-WIN] 2. Compiling Rust Shared Core (scl_server)...
echo ===================================================
cargo build --release --manifest-path 0_shared_lib/Cargo.toml --bin scl_server
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Rust core compilation failed!
    exit /b %ERRORLEVEL%
)

:: Create bin and bbuild directories if they don't exist
if not exist bin mkdir bin
if not exist bbuild mkdir bbuild

:: Copy compiled Rust server
copy /Y 0_shared_lib\target\release\scl_server.exe bin\scl_server.exe
copy /Y 0_shared_lib\target\release\scl_server.exe bbuild\scl_server.exe

echo ===================================================
echo [BUILD-WIN] 3. Compiling Go Node (YonaCode)...
echo ===================================================
go build -o bin/YonaCode.exe ./6_user_interface/cmd/genz
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go Node compilation failed!
    exit /b %ERRORLEVEL%
)
copy /Y bin\YonaCode.exe bbuild\YonaCode.exe

echo ===================================================
echo [BUILD-WIN] 4. Compiling Go Wallet Gateway (yona_wallet_server)...
echo ===================================================
go build -o bin/yona_wallet_server.exe ./8_wallet_gateway
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go Wallet Gateway compilation failed!
    exit /b %ERRORLEVEL%
)
copy /Y bin\yona_wallet_server.exe bbuild\yona_wallet_server.exe

echo ===================================================
echo [BUILD-WIN] 4.5. Compiling Go GPU Setup Tool (yona_gpu_setup)...
echo ===================================================
go build -o bin/yona_gpu_setup.exe ./6_user_interface/cmd/gpu_installer
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go GPU Setup Tool compilation failed!
    exit /b %ERRORLEVEL%
)
copy /Y bin\yona_gpu_setup.exe bbuild\yona_gpu_setup.exe


echo ===================================================
echo [BUILD-WIN] 5. Compiling Windows GPU Miner (CMake)...
echo ===================================================
if not exist 8_miner_gpu\build mkdir 8_miner_gpu\build
cmake -S 8_miner_gpu -B 8_miner_gpu\build -DCMAKE_BUILD_TYPE=Release
cmake --build 8_miner_gpu\build --config Release
if %ERRORLEVEL% equ 0 (
    copy /Y 8_miner_gpu\build\Release\yona_gpu_miner.exe bin\yona_gpu_miner.exe
    if exist bbuild\yona_gpu_miner.exe del /Q bbuild\yona_gpu_miner.exe
    echo [SUCCESS] Windows GPU Miner compiled successfully!
) else (
    echo [WARN] Windows GPU Miner compilation failed! (CUDA/OpenCL drivers might be missing)
)

echo ===================================================
echo [BUILD-WIN] SUCCESS: WINDOWS BUILD COMPLETED!
echo ===================================================
pause
