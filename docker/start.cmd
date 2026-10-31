@echo off
docker compose -f "%~dp0compose.yaml" up -d --build
pause
