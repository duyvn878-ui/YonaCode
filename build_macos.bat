@echo off
echo ===================================================
echo [BUILD] 1. Creating output folders for macOS...
echo ===================================================
if not exist bin\macos\arm64 mkdir bin\macos\arm64
if not exist bin\macos\x64 mkdir bin\macos\x64
del /Q bin\macos\arm64\* >nul 2>&1
del /Q bin\macos\x64\* >nul 2>&1

echo [BUILD] 2. Skipping Rust Kernel compilation for macOS ARM64 (using pre-built recovered binaries)...

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

echo [BUILD] 4. Skipping Rust Kernel compilation for macOS x86_64 (using pre-built recovered binaries)...

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
