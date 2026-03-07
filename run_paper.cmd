@echo off
setlocal

echo =========================================================
echo   AegisTrade Paper Trading Mode (Alpaca) 
echo =========================================================
echo.

echo Running AegisTrade with config_paper.json...
echo.

go run main.go config_paper.json
