@echo off
echo Cleaning up old executables...
del /Q bin\*.exe
del /Q bbuild\*.exe
del /Q bbuild\*.dll

echo Building Rust SCL Server...
cd 0_shared_lib
:: Thiết lập RUSTFLAGS bắt buộc biên dịch tĩnh (Static Linkage CRT) cho cả Rust và C++ (RocksDB)
set RUSTFLAGS=-C target-feature=+crt-static
:: Build cả lib (btc_genz_scl.dll) và bin (scl_server.exe) để đảm bảo cập nhật đầy đủ phiên bản mới nhất
cargo build --release --lib --bin scl_server
cd ..

:: [V1.3.2] Sao chép thêm trực tiếp vào thư mục đầu ra bin/ để phân phối đầy đủ 3 file chạy độc lập
copy /Y 0_shared_lib\target\release\scl_server.exe bin\scl_server.exe
copy /Y 0_shared_lib\target\release\btc_genz_scl.dll bin\btc_genz_scl.dll

echo Building Go Node Core...
go build -o bin\YonaCode.exe .\6_user_interface\cmd\genz

echo Starting Node 1...
if not exist node mkdir node
cd bin
start "" cmd /c "YonaCode.exe node run --port 8080 --p2p-port 9000 --scl-port 50051 --db-path ..\node\data --reward-address e5614e302e1eb27c72e1571c976d983c15cdd11c4679586ce6fa001d533d4bee > ..\node\node_stdout.log 2> ..\node\node_stderr.log"
cd ..
echo Done.
