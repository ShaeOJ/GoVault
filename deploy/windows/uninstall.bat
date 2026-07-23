@echo off
:: GoVault Edge Node — Windows Service Uninstaller
:: Run as Administrator.

setlocal
set SERVICE_NAME=GoVaultEdge

net session >nul 2>&1
if errorlevel 1 (
    echo ERROR: Run as Administrator.
    pause & exit /b 1
)

echo Stopping and removing service: %SERVICE_NAME%
nssm stop    %SERVICE_NAME% >nul 2>&1
nssm remove  %SERVICE_NAME% confirm

echo.
echo Service removed. Install directory C:\GoVaultEdge was NOT deleted.
echo Delete it manually if no longer needed.
echo.
pause
