$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location $ScriptDir

Write-Host "=========================================================="
Write-Host "  GoRobotScript Core Build Tool"
Write-Host "=========================================================="

$BinDir = Join-Path $ScriptDir "bin"
if (!(Test-Path $BinDir)) {
    Write-Host "Creating bin directory..."
    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
}

$LibsDir = Join-Path $ScriptDir "Core\libs"
if (Test-Path $LibsDir) {
    Write-Host "Restoring 15 dependency DLLs from Core/libs to bin/..."
    Copy-Item -Path "$LibsDir\*" -Destination $BinDir -Force
}

# 仅编译三大核心工具
Write-Host "Building GoRunner.exe (Unified Runner)..."
go build -o "$BinDir\GoRunner.exe" ./Core/GoRunner/RunnerCLI
Copy-Item "$BinDir\GoRunner.exe" "$BinDir\GokeyLua.exe" -Force
Copy-Item "$BinDir\GoRunner.exe" "$BinDir\GokeyRun.exe" -Force

Write-Host "Building PackLua.exe (Script & Asset Packer)..."
go build -o "$BinDir\PackLua.exe" ./Core/GoPacker/PackerCLI

Write-Host "Building SingleLoader.exe (Standalone Loader Template)..."
go build -o "$BinDir\SingleLoader.exe" ./Core/GoPacker/SingleLoader

# 清理 bin 中非核心旧二进制
Remove-Item -Path "$BinDir\GoHotkey.exe", "$BinDir\GokeyHotkey.exe", "$BinDir\GoRecord.exe", "$BinDir\GokeyLog.exe", "$BinDir\GoLua.exe" -Force -ErrorAction SilentlyContinue

Write-Host "=========================================================="
Write-Host "Core build completed successfully!"
Write-Host "=========================================================="
