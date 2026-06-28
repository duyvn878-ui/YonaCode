@echo off
echo ====================================
echo Starting Copying Rust release files
echo ====================================
copy /Y 0_shared_lib\target\release\scl_server.exe bin\scl_server.exe
copy /Y 0_shared_lib\target\release\btc_genz_scl.dll bin\btc_genz_scl.dll

echo ====================================
echo Building Go Core (YonaCode.exe)
echo ====================================
go build -o bin\YonaCode.exe .\6_user_interface\cmd\genz
if %ERRORLEVEL% neq 0 (
    echo Building Go Core failed!
    exit /b %ERRORLEVEL%
)

echo ====================================
echo Cleaning bbuild folder
echo ====================================
if exist bbuild (
    del /Q bbuild\*.exe
    del /Q bbuild\*.dll
)

echo ====================================
echo Build Success!
echo ====================================
