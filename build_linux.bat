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
set CARGO_TARGET_DIR=d:\hanhtrinhhocta-p\sssd\target_linux

echo [1/4] Đang biên dịch Rust SCL Server và Miner cho Linux...
cd 0_shared_lib
set RETRY_COUNT=0

:cargo_loop
cargo build -j 1 --release --lib --bin scl_server --bin genz_miner --target x86_64-unknown-linux-musl
if %ERRORLEVEL% equ 0 goto cargo_success

set /a RETRY_COUNT=RETRY_COUNT+1
if %RETRY_COUNT% geq 7 goto cargo_fail

echo ⚠️ Cargo build gap loi lock file, dang doi 4 giay va thu lai (Lan thu %RETRY_COUNT%/6)...
ping 127.0.0.1 -n 5 >nul
goto cargo_loop

:cargo_fail
echo ❌ Loi bien dich Rust Kernel sau 6 lan thu!
cd ..
exit /b 1

:cargo_success
cd ..

:: 3. Làm sạch và chuẩn bị thư mục đầu ra
echo [2/4] Sao chep file Rust vao bin/linux...
:: Sao chép trực tiếp ra thư mục bin/linux để phân phối chạy độc lập
copy /Y d:\hanhtrinhhocta-p\sssd\target_linux\x86_64-unknown-linux-musl\release\scl_server bin\linux\scl_server
copy /Y d:\hanhtrinhhocta-p\sssd\target_linux\x86_64-unknown-linux-musl\release\genz_miner bin\linux\genz_miner


:: 4. Xây dựng lại Web UI để đảm bảo giao diện nhúng là mới nhất
echo [3/5] Xây dựng lại Web UI...
cd 6_user_interface\web_ui
cmd /c "npm run build"
cd ..\..

:: 5. Biên dịch Go Core cho Linux (CGO_ENABLED=0 để tĩnh hóa hoàn toàn)
echo [4/5] Đang biên dịch Go Core cho Linux...
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

:: 6. Đóng gói phân phối Linux ZIP
echo [5/5] Đóng gói phát hành YonaCode_Linux.zip...
powershell -ExecutionPolicy Bypass -File "%~dp0scratch\zip_pack_linux.ps1"

echo ===================================================
echo ✅ BIÊN DỊCH VÀ ĐÓNG GÓI LINUX THÀNH CÔNG!
echo 📂 Tệp tin zip tại: zip\YonaCode_Linux.zip
echo ===================================================
