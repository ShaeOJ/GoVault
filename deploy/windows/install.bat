@echo off
:: GoVault Edge Node — Windows Service Installer
:: Requires: NSSM (Non-Sucking Service Manager) in PATH
::           https://nssm.cc/download
:: Run as Administrator.

setlocal

set SERVICE_NAME=GoVaultEdge
set INSTALL_DIR=C:\GoVaultEdge
set BINARY=%~dp0..\..\build\edgenode\gostratum-edge-windows.exe
set NSSM=nssm

echo.
echo  GoVault Edge Node — Windows Service Install
echo  ============================================
echo.

:: ── Check for admin ──────────────────────────────────────────────────────────
net session >nul 2>&1
if errorlevel 1 (
    echo ERROR: This script must be run as Administrator.
    echo        Right-click and select "Run as administrator".
    pause & exit /b 1
)

:: ── Check NSSM ───────────────────────────────────────────────────────────────
where %NSSM% >nul 2>&1
if errorlevel 1 (
    echo ERROR: nssm.exe not found in PATH.
    echo        Download from https://nssm.cc/download
    echo        Extract nssm.exe and place it in C:\Windows\System32\
    pause & exit /b 1
)

:: ── Check binary ─────────────────────────────────────────────────────────────
if not exist "%BINARY%" (
    echo ERROR: Binary not found: %BINARY%
    echo        Build it first: make edge-all
    echo        Or update the BINARY path at the top of this script.
    pause & exit /b 1
)

:: ── Create install dir ───────────────────────────────────────────────────────
echo Creating %INSTALL_DIR% ...
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if not exist "%INSTALL_DIR%\data" mkdir "%INSTALL_DIR%\data"
copy /y "%BINARY%" "%INSTALL_DIR%\gostratum-edge.exe"

:: ── First-run setup ───────────────────────────────────────────────────────────
if not exist "%INSTALL_DIR%\data\config.json" (
    echo.
    echo No config found. Running setup wizard...
    echo.
    cd /d "%INSTALL_DIR%"
    gostratum-edge.exe --init
    cd /d "%~dp0"
)

:: ── Remove old service if exists ─────────────────────────────────────────────
%NSSM% status %SERVICE_NAME% >nul 2>&1
if not errorlevel 1 (
    echo Removing existing service...
    %NSSM% stop %SERVICE_NAME% >nul 2>&1
    %NSSM% remove %SERVICE_NAME% confirm
)

:: ── Install service ───────────────────────────────────────────────────────────
echo Installing Windows service: %SERVICE_NAME%
%NSSM% install %SERVICE_NAME% "%INSTALL_DIR%\gostratum-edge.exe"
%NSSM% set %SERVICE_NAME% AppDirectory "%INSTALL_DIR%"
%NSSM% set %SERVICE_NAME% DisplayName "GoVault Edge Node"
%NSSM% set %SERVICE_NAME% Description "Firepool stratum relay node"
%NSSM% set %SERVICE_NAME% Start SERVICE_AUTO_START
%NSSM% set %SERVICE_NAME% AppStdout "%INSTALL_DIR%\data\edge-stdout.log"
%NSSM% set %SERVICE_NAME% AppStderr "%INSTALL_DIR%\data\edge-stderr.log"
%NSSM% set %SERVICE_NAME% AppRotateFiles 1
%NSSM% set %SERVICE_NAME% AppRotateOnline 1
%NSSM% set %SERVICE_NAME% AppRotateSeconds 86400

:: ── Start service ─────────────────────────────────────────────────────────────
echo Starting service...
%NSSM% start %SERVICE_NAME%

echo.
echo  Done!
echo.
echo  Service: %SERVICE_NAME%  (auto-starts on boot)
echo  Install: %INSTALL_DIR%
echo.
echo  Dashboard: http://localhost:8080
echo  Logs:      %INSTALL_DIR%\data\edge-stdout.log
echo.
echo  To stop:    nssm stop %SERVICE_NAME%
echo  To restart: nssm restart %SERVICE_NAME%
echo  To remove:  deploy\windows\uninstall.bat
echo.
pause
