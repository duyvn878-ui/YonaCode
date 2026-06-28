@echo off
echo ===================================================
echo [BUILD] 1. Creating output folders for macOS...
echo ===================================================
if not exist bin\macos\arm64 mkdir bin\macos\arm64
if not exist bin\macos\x64 mkdir bin\macos\x64
del /Q bin\macos\arm64\* >nul 2>&1
del /Q bin\macos\x64\* >nul 2>&1

echo ===================================================
echo [BUILD] 2. Compiling Rust Kernel for macOS ARM64...
echo ===================================================
set CC=%~dp0scratch\zig-cc-macos-arm64.bat
set CXX=%~dp0scratch\zig-cxx-macos-arm64.bat
set AR=%~dp0scratch\zig-ar-macos-arm64.bat
set CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER=%~dp0scratch\zig-cc-macos-arm64.bat
cd 0_shared_lib
cargo build --release --lib --bin scl_server --bin genz_miner --target aarch64-apple-darwin
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Rust ARM64 build failed!
    cd ..
    exit /b %ERRORLEVEL%
)
cd ..
copy /Y 0_shared_lib\target\aarch64-apple-darwin\release\scl_server bin\macos\arm64\scl_server
copy /Y 0_shared_lib\target\aarch64-apple-darwin\release\genz_miner bin\macos\arm64\genz_miner
copy /Y 0_shared_lib\target\aarch64-apple-darwin\release\libbtc_genz_scl.dylib bin\macos\arm64\libbtc_genz_scl.dylib

echo [BUILD] 3. Compiling Go Core for macOS ARM64...
set GOOS=darwin
set GOARCH=arm64
set CGO_ENABLED=0
go build -ldflags="-s -w" -o bin\macos\arm64\YonaCode .\6_user_interface\cmd\genz
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go Core ARM64 build failed!
    exit /b %ERRORLEVEL%
)
go build -ldflags="-s -w" -o bin\macos\arm64\cli_yona_code .\6_user_interface\cmd\cli_node
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go CLI Node ARM64 build failed!
    exit /b %ERRORLEVEL%
)

echo ===================================================
echo [BUILD] 4. Compiling Rust Kernel for macOS x86_64...
echo ===================================================
set CC=%~dp0scratch\zig-cc-macos-x64.bat
set CXX=%~dp0scratch\zig-cxx-macos-x64.bat
set AR=%~dp0scratch\zig-ar-macos-x64.bat
set CARGO_TARGET_X86_64_APPLE_DARWIN_LINKER=%~dp0scratch\zig-cc-macos-x64.bat
cd 0_shared_lib
cargo build --release --lib --bin scl_server --bin genz_miner --target x86_64-apple-darwin
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Rust x86_64 build failed!
    cd ..
    exit /b %ERRORLEVEL%
)
cd ..
copy /Y 0_shared_lib\target\x86_64-apple-darwin\release\scl_server bin\macos\x64\scl_server
copy /Y 0_shared_lib\target\x86_64-apple-darwin\release\genz_miner bin\macos\x64\genz_miner
copy /Y 0_shared_lib\target\x86_64-apple-darwin\release\libbtc_genz_scl.dylib bin\macos\x64\libbtc_genz_scl.dylib

echo [BUILD] 5. Compiling Go Core for macOS x86_64...
set GOOS=darwin
set GOARCH=amd64
set CGO_ENABLED=0
go build -ldflags="-s -w" -o bin\macos\x64\YonaCode .\6_user_interface\cmd\genz
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go Core x86_64 build failed!
    exit /b %ERRORLEVEL%
)
go build -ldflags="-s -w" -o bin\macos\x64\cli_yona_code .\6_user_interface\cmd\cli_node
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go CLI Node x86_64 build failed!
    exit /b %ERRORLEVEL%
)

:: Reset env
set GOOS=
set GOARCH=
set CGO_ENABLED=
set RUSTFLAGS=
set CC=
set CXX=
set AR=

echo ===================================================
echo [BUILD] 6. Done!
echo ===================================================
