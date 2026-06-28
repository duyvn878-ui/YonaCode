@echo off
echo ===================================================
echo 🛠️  BIÊN DỊCH YonaCode Go CHO LINUX (x86_64-unknown-linux-musl)
echo ===================================================

:: 1. Tạo thư mục đầu ra
if not exist bin\linux mkdir bin\linux
del /Q bin\linux\*

:: 2. Thiết lập môi trường biên dịch chéo Rust (sử dụng Zig)
set CC=%~dp0scratch\zig-cc.bat
set CXX=%~dp0scratch\zig-cxx.bat
set AR=%~dp0scratch\zig-ar.bat
set CARGO_TARGET_X86_64_UNKNOWN_LINUX_MUSL_LINKER=%~dp0scratch\zig-cc.bat
set RUSTFLAGS=-C link-self-contained=no

echo [1/4] Đang biên dịch Rust SCL Server và Miner cho Linux...
cd 0_shared_lib
cargo build --release --lib --bin scl_server --bin genz_miner --target x86_64-unknown-linux-musl
if %ERRORLEVEL% neq 0 (
    echo ❌ Lỗi biên dịch Rust Kernel!
    cd ..
    exit /b %ERRORLEVEL%
)
cd ..

:: 3. Làm sạch và chuẩn bị thư mục đầu ra
echo [2/4] Sao chep file Rust vao bin/linux...
:: Sao chép trực tiếp ra thư mục bin/linux để phân phối chạy độc lập
copy /Y 0_shared_lib\target\x86_64-unknown-linux-musl\release\scl_server bin\linux\scl_server
copy /Y 0_shared_lib\target\x86_64-unknown-linux-musl\release\genz_miner bin\linux\genz_miner


:: 4. Biên dịch Go Core cho Linux (CGO_ENABLED=0 để tĩnh hóa hoàn toàn)
echo [3/4] Đang biên dịch Go Core cho Linux...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w" -o bin\linux\YonaCode .\6_user_interface\cmd\genz
if %ERRORLEVEL% neq 0 (
    echo ❌ Lỗi biên dịch Go Core!
    exit /b %ERRORLEVEL%
)
go build -ldflags="-s -w" -o bin\linux\cli_yona_code .\6_user_interface\cmd\cli_node
if %ERRORLEVEL% neq 0 (
    echo ❌ Lỗi biên dịch Go CLI Node!
    exit /b %ERRORLEVEL%
)

:: Trả lại môi trường mặc định
set GOOS=
set GOARCH=
set CGO_ENABLED=
set RUSTFLAGS=

echo ===================================================
echo ✅ BIÊN DỊCH LINUX THÀNH CÔNG!
echo 📂 Tệp tin đầu ra tại: bin\linux\
echo ===================================================
