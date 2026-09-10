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

# 基础模块版本号：决定打包产物解压时使用的共享 base 目录
$BaseVersionFile = Join-Path $LibsDir "base.version"
$BaseVersion = if (Test-Path $BaseVersionFile) { (Get-Content $BaseVersionFile -Raw).Trim() } else { "1.0.0" }
Write-Host " [Base] Base module version: $BaseVersion  (packs extract to base\b_${BaseVersion}_<hash>\)"

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

# 编译核心模块；产出与 Core/ 下的模块目录一一对应
#   bin 下只放 2 个用户工具 + 2 个 .apppak 载荷 + 依赖 DLL：
#   Core/GoRunner              -> bin/GoRunner.exe         (录制 + 回放)        用户工具
#   Core/GoPacker              -> bin/GoPacker.exe         (打包 + -unpak 调试) 用户工具
#   Core/GoLua                 -> bin/GoLua.apppak         (Lua 运行时载荷，-unpak 时复制改名为可执行程序)
#   Core/GoPacker/SingleLoader -> bin/SingleLoader.apppak  (单文件引导器载荷，单文件打包时拼接到产物开头)
Write-Host "Building GoRunner.exe (from Core/GoRunner)..."
go build -o "$BinDir\GoRunner.exe" ./Core/GoRunner/RunnerCLI

Write-Host "Building GoPacker.exe (from Core/GoPacker)..."
go build -o "$BinDir\GoPacker.exe" ./Core/GoPacker/PackerCLI

Write-Host "Building GoLua.apppak (Lua runtime payload, from Core/GoLua)..."
go build -o "$BinDir\GoLua.apppak" ./Core/GoLua/LuaCLI

Write-Host "Building SingleLoader.apppak (bootstrap payload, from Core/GoPacker/SingleLoader)..."
go build -o "$BinDir\SingleLoader.apppak" ./Core/GoPacker/SingleLoader

Write-Host "=========================================================="
Write-Host "Core build completed successfully!"
Write-Host "=========================================================="
