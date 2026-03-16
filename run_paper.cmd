@echo off
setlocal

set "CONFIG_FILE=config_paper.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_paper.example.json"
set "NORTHSTAR_BIN=northstar.exe"

echo =========================================================
echo   Northstar Paper Trading Mode (Alpaca) 
echo =========================================================
echo.

echo Running Northstar with %CONFIG_FILE%...
echo.

if exist "%NORTHSTAR_BIN%" (
  echo Using release binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Release binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
