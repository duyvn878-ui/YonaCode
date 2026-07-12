@echo off
echo ===================================================
echo [BUILD] 1. Dang xoa file binary cu...
echo ===================================================
if exist bin\YonaCode.exe del /Q bin\YonaCode.exe
if exist bin\scl_server.exe del /Q bin\scl_server.exe
if exist bin\genz_miner.exe del /Q bin\genz_miner.exe
if exist bin\btc_genz_scl.dll del /Q bin\btc_genz_scl.dll

echo ===================================================
echo [BUILD] 2. Dang bien dich Rust Core (DLL, scl_server, genz_miner)...
echo ===================================================
cd 0_shared_lib
set RUSTFLAGS=-C target-feature=+crt-static
set CARGO_TARGET_DIR=d:\hanhtrinhhocta-p\sssd\target_win
cargo build -j 1 --release --lib --bin scl_server --bin genz_miner
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Cargo build that bai!
    exit /b %ERRORLEVEL%
)
cd ..

echo ===================================================
echo [BUILD] 3. Copy file Rust vao bin...
echo ===================================================
copy /Y d:\hanhtrinhhocta-p\sssd\target_win\release\scl_server.exe bin\scl_server.exe
copy /Y d:\hanhtrinhhocta-p\sssd\target_win\release\genz_miner.exe bin\genz_miner.exe
copy /Y d:\hanhtrinhhocta-p\sssd\target_win\release\btc_genz_scl.dll bin\btc_genz_scl.dll

echo ===================================================
echo [BUILD] 4. Bien dich Go Core (YonaCode.exe)...
echo ===================================================
go build -o bin\YonaCode.exe .\6_user_interface\cmd\genz
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go core build that bai!
    exit /b %ERRORLEVEL%
)
go build -o bin\cli_yona_code.exe .\6_user_interface\cmd\cli_node
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go CLI Node build that bai!
    exit /b %ERRORLEVEL%
)

echo ===================================================
echo [BUILD] 5. Tu dong don dep va dong goi bbuild...
echo ===================================================
if not exist bbuild mkdir bbuild
del /Q bbuild\*.exe >nul 2>&1
del /Q bbuild\*.dll >nul 2>&1
copy /Y bin\YonaCode.exe bbuild\YonaCode.exe

echo ===================================================
echo [BUILD] HOAN THANH THANH CONG!
echo ===================================================
