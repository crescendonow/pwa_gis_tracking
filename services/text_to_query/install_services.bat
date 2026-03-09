@echo off
REM ============================================
REM  Install Windows Services using NSSM
REM  Services: Ollama + Text-to-Query (Python)
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
REM  1. OLLAMA SERVICE
REM ============================================
echo [1/2] Installing Ollama service ...

REM Find ollama.exe
set OLLAMA_EXE=
where ollama >nul 2>&1
if %errorlevel% equ 0 (
    for /f "delims=" %%i in ('where ollama') do set OLLAMA_EXE=%%i
)
if "%OLLAMA_EXE%"=="" (
    REM Try common install paths
    if exist "C:\Users\%USERNAME%\AppData\Local\Programs\Ollama\ollama.exe" (
        set OLLAMA_EXE=C:\Users\%USERNAME%\AppData\Local\Programs\Ollama\ollama.exe
    ) else if exist "C:\Program Files\Ollama\ollama.exe" (
        set OLLAMA_EXE=C:\Program Files\Ollama\ollama.exe
    )
)

if "%OLLAMA_EXE%"=="" (
    echo [WARN] ollama.exe not found. Please install Ollama first.
    echo        https://ollama.com/download/windows
    echo        Then edit this script to set OLLAMA_EXE path.
    echo.
) else (
    echo     ollama.exe: %OLLAMA_EXE%

    REM Remove old service if exists
    "%NSSM%" stop OllamaService >nul 2>&1
    "%NSSM%" remove OllamaService confirm >nul 2>&1

    REM Install service
    "%NSSM%" install OllamaService "%OLLAMA_EXE%"
    "%NSSM%" set OllamaService AppParameters "serve"
    "%NSSM%" set OllamaService DisplayName "Ollama LLM Server"
    "%NSSM%" set OllamaService Description "Ollama LLM inference server for PWA GIS Text-to-Query"
    "%NSSM%" set OllamaService Start SERVICE_AUTO_START

    REM Environment: model storage on Drive E
    "%NSSM%" set OllamaService AppEnvironmentExtra "OLLAMA_MODELS=E:\ollama\models" "OLLAMA_HOST=127.0.0.1:11434"

    REM Logging
    if not exist "E:\ollama\logs" mkdir "E:\ollama\logs"
    "%NSSM%" set OllamaService AppStdout "E:\ollama\logs\ollama_stdout.log"
    "%NSSM%" set OllamaService AppStderr "E:\ollama\logs\ollama_stderr.log"
    "%NSSM%" set OllamaService AppStdoutCreationDisposition 4
    "%NSSM%" set OllamaService AppStderrCreationDisposition 4
    "%NSSM%" set OllamaService AppRotateFiles 1
    "%NSSM%" set OllamaService AppRotateBytes 10485760

    REM Auto-restart on failure
    "%NSSM%" set OllamaService AppRestartDelay 5000
    "%NSSM%" set OllamaService AppThrottle 10000

    echo     [OK] OllamaService installed
)

echo.

REM ============================================
REM  2. TEXT-TO-QUERY PYTHON SERVICE
REM ============================================
echo [2/2] Installing Text-to-Query service ...

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

REM Dependencies: Ollama must start first
"%NSSM%" set TextToQueryService DependOnService OllamaService

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
echo  Starting services ...
echo ============================================

REM Start Ollama first
echo Starting OllamaService ...
"%NSSM%" start OllamaService
timeout /t 5 /nobreak >nul

REM Then Text-to-Query
echo Starting TextToQueryService ...
"%NSSM%" start TextToQueryService

echo.
echo ============================================
echo  Done! Service Status:
echo ============================================
"%NSSM%" status OllamaService
"%NSSM%" status TextToQueryService

echo.
echo  Services installed:
echo  ┌──────────────────────────────────────────┐
echo  │ OllamaService       │ port 11434         │
echo  │ TextToQueryService  │ port 5012          │
echo  │ pwa_gis_tracking    │ port 5011 (exists) │
echo  └──────────────────────────────────────────┘
echo.
echo  Logs:
echo    Ollama:        E:\ollama\logs\
echo    Text-to-Query: %SERVICE_DIR%\logs\
echo.
echo  Management commands:
echo    nssm start/stop/restart OllamaService
echo    nssm start/stop/restart TextToQueryService
echo.
pause
