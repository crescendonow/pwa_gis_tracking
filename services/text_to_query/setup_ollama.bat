@echo off
REM ============================================
REM  Ollama Setup Script for PWA GIS Server
REM  Install Ollama + Typhoon 2.1 Gemma3 4B
REM  Store models on Drive E:
REM ============================================

echo.
echo ============================================
echo  Ollama Setup - PWA GIS Text-to-Query
echo ============================================
echo.

REM 1. Create folder on Drive E
echo [1/4] Creating model folder on E:\ollama\models ...
if not exist "E:\ollama\models" mkdir "E:\ollama\models"

REM 2. Set OLLAMA_MODELS environment variable (system-wide, persistent)
echo [2/4] Setting OLLAMA_MODELS environment variable ...
setx OLLAMA_MODELS "E:\ollama\models" /M
echo     OLLAMA_MODELS = E:\ollama\models

REM Set for current session too
set OLLAMA_MODELS=E:\ollama\models

REM 3. Check if Ollama is installed
echo [3/4] Checking Ollama installation ...
where ollama >nul 2>&1
if %errorlevel% neq 0 (
    echo.
    echo  Ollama is NOT installed.
    echo  Please download and install from:
    echo  https://ollama.com/download/windows
    echo.
    echo  After installing, run this script again.
    echo.
    pause
    exit /b 1
)

echo     Ollama found: OK

REM 4. Pull the Typhoon model
echo [4/4] Pulling Typhoon 2.1 Gemma3 4B model ...
echo     This may take 5-10 minutes depending on internet speed.
echo.
ollama pull scb10x/typhoon2.1-gemma3-4b

echo.
echo ============================================
echo  Setup Complete!
echo ============================================
echo.
echo  Model stored at: E:\ollama\models
echo  Model name: scb10x/typhoon2.1-gemma3-4b
echo.
echo  To test, run:
echo    ollama run scb10x/typhoon2.1-gemma3-4b "สวัสดีครับ"
echo.
echo  To start the Ollama server:
echo    ollama serve
echo.
pause
