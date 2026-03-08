@echo off
setlocal

echo =========================================================
echo   Northstar Paper Trading Mode (Alpaca) 
echo =========================================================
echo.

echo Running Northstar with config_paper.json...
echo.

go run main.go config_paper.json
