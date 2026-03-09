@echo off
REM ============================================
REM  Uninstall Windows Services
REM  ** Run as Administrator **
REM ============================================

set NSSM=C:\nssm-2.24\nssm-2.24\win64\nssm.exe

echo.
echo ============================================
echo  PWA GIS - Service Uninstaller
echo ============================================
echo.
echo  This will STOP and REMOVE:
echo    - OllamaService
echo    - TextToQueryService
echo.
set /p CONFIRM=Are you sure? (y/n):
if /i not "%CONFIRM%"=="y" exit /b 0

echo.
echo Stopping services ...
"%NSSM%" stop TextToQueryService >nul 2>&1
"%NSSM%" stop OllamaService >nul 2>&1

echo Removing services ...
"%NSSM%" remove TextToQueryService confirm
"%NSSM%" remove OllamaService confirm

echo.
echo [OK] Services removed.
echo.
pause
