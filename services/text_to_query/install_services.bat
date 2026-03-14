@echo off
REM ============================================
REM  Install Windows Service using NSSM
REM  Service: Text-to-Query (Python/FastAPI)
REM  ** Run as Administrator **
REM ============================================

set NSSM=C:\nssm-2.24\nssm-2.24\win64\nssm.exe
set SERVICE_DIR=C:\Projects\pwa_gis_tracking\services\text_to_query

echo.
echo ============================================
echo  PWA GIS - Service Installer
echo  NSSM: %NSSM%
echo ============================================
echo.

REM ── Check NSSM exists ──
if not exist "%NSSM%" (
    echo [ERROR] NSSM not found at %NSSM%
    pause
    exit /b 1
)

REM ============================================
REM  TEXT-TO-QUERY PYTHON SERVICE
REM ============================================
echo Installing Text-to-Query service ...

REM Find python.exe
set PYTHON_EXE=
where python >nul 2>&1
if %errorlevel% equ 0 (
    for /f "delims=" %%i in ('where python') do set PYTHON_EXE=%%i
)
if "%PYTHON_EXE%"=="" (
    echo [WARN] python.exe not found. Please install Python 3.8+
    echo        Then edit this script to set PYTHON_EXE path.
    pause
    exit /b 1
)

echo     python.exe: %PYTHON_EXE%
echo     service dir: %SERVICE_DIR%

REM Remove old service if exists
"%NSSM%" stop TextToQueryService >nul 2>&1
"%NSSM%" remove TextToQueryService confirm >nul 2>&1

REM Install service
"%NSSM%" install TextToQueryService "%PYTHON_EXE%"
"%NSSM%" set TextToQueryService AppParameters "-m uvicorn main:app --host 127.0.0.1 --port 5012"
"%NSSM%" set TextToQueryService AppDirectory "%SERVICE_DIR%"
"%NSSM%" set TextToQueryService DisplayName "PWA GIS Text-to-Query"
"%NSSM%" set TextToQueryService Description "Thai NL to MongoDB/PostGIS query service (port 5012)"
"%NSSM%" set TextToQueryService Start SERVICE_AUTO_START

REM Logging
if not exist "%SERVICE_DIR%\logs" mkdir "%SERVICE_DIR%\logs"
"%NSSM%" set TextToQueryService AppStdout "%SERVICE_DIR%\logs\service_stdout.log"
"%NSSM%" set TextToQueryService AppStderr "%SERVICE_DIR%\logs\service_stderr.log"
"%NSSM%" set TextToQueryService AppStdoutCreationDisposition 4
"%NSSM%" set TextToQueryService AppStderrCreationDisposition 4
"%NSSM%" set TextToQueryService AppRotateFiles 1
"%NSSM%" set TextToQueryService AppRotateBytes 10485760

REM Auto-restart on failure
"%NSSM%" set TextToQueryService AppRestartDelay 5000
"%NSSM%" set TextToQueryService AppThrottle 10000

echo     [OK] TextToQueryService installed

echo.
echo ============================================
echo  Starting service ...
echo ============================================

echo Starting TextToQueryService ...
"%NSSM%" start TextToQueryService

echo.
echo ============================================
echo  Done! Service Status:
echo ============================================
"%NSSM%" status TextToQueryService

echo.
echo  Service installed:
echo  +---------------------------------------------+
echo  : TextToQueryService  : port 5012             :
echo  : pwa_gis_tracking    : port 5011 (Go, exists):
echo  +---------------------------------------------+
echo.
echo  Logs: %SERVICE_DIR%\logs\
echo.
echo  Management commands:
echo    nssm start/stop/restart TextToQueryService
echo.
pause
