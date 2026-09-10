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
    Write-Host "Restoring dependency DLLs from Core/libs to bin/..."
    Copy-Item -Path "$LibsDir\*" -Destination $BinDir -Force
}

# 检测并静默安装虚拟手柄总线驱动 ViGEmBus
Write-Host "Checking ViGEmBus driver status..."
$vigemService = Get-Service -Name "ViGEmBus" -ErrorAction SilentlyContinue
if ($null -eq $vigemService) {
    Write-Host " [ViGEmBus] Not installed. Preparing silent install..."
    $toolsDir = Join-Path $ScriptDir "tools"
    if (!(Test-Path $toolsDir)) { New-Item -ItemType Directory -Path $toolsDir -Force | Out-Null }
    $setupExe = Join-Path $toolsDir "ViGEmBusSetup.exe"
    $downloadUrl = "https://ghproxy.net/https://github.com/nefarius/ViGEmBus/releases/download/v1.22.0/ViGEmBus_1.22.0_x64_x86_arm64.exe"
    
    if (!(Test-Path $setupExe)) {
        Write-Host " -> Downloading ViGEmBus installer..."
        try {
            curl.exe -L $downloadUrl -o $setupExe
        } catch {
            Write-Warning "Download failed: $downloadUrl"
        }
    }

    if (Test-Path $setupExe) {
        Write-Host " -> Running silent installation (/quiet /norestart)..."
        Start-Process -FilePath $setupExe -ArgumentList "/quiet", "/norestart" -Verb RunAs -Wait
        $vigemService = Get-Service -Name "ViGEmBus" -ErrorAction SilentlyContinue
        if ($vigemService) {
            Write-Host " [SUCCESS] ViGEmBus driver installed and ready!" -ForegroundColor Green
        } else {
            Write-Warning "ViGEmBus install completed, administrator approval required."
        }
    }
} else {
    Write-Host " [PASS] ViGEmBus driver is active and running ($($vigemService.Status))." -ForegroundColor Green
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
