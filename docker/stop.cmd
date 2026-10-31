@echo off
docker compose -f "%~dp0compose.yaml" stop
pause
