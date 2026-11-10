@echo off
rem GoRobotScript Windows 一键编译与依赖还原批处理
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1"
pause
