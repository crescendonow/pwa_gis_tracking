@echo off
REM ============================================
REM  Uninstall Windows Service
REM  ** Run as Administrator **
REM ============================================

set NSSM=C:\nssm-2.24\nssm-2.24\win64\nssm.exe

echo.
echo ============================================
echo  PWA GIS - Service Uninstaller
echo ============================================
echo.
echo  This will STOP and REMOVE:
echo    - TextToQueryService
echo.
set /p CONFIRM=Are you sure? (y/n):
if /i not "%CONFIRM%"=="y" exit /b 0

echo.
echo Stopping service ...
"%NSSM%" stop TextToQueryService >nul 2>&1

echo Removing service ...
"%NSSM%" remove TextToQueryService confirm

echo.
echo [OK] Service removed.
echo.
pause
