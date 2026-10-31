@echo off
docker compose -f "%~dp0compose.yaml" logs -f --tail 100 game
pause
