@echo off
cd /d "%~dp0"
title TgWsProxy - Telegram WebSocket Proxy

:restart
echo [%date% %time%] Starting proxy... >> "%APPDATA%\TgWsProxy\startup.log"
TgWsProxy.exe -v >> "%APPDATA%\TgWsProxy\startup.log" 2>&1
echo [%date% %time%] Proxy exited with code %errorlevel%, restarting... >> "%APPDATA%\TgWsProxy\startup.log"
timeout /t 3 >nul
goto restart
