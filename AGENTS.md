# GoRobotScript (核心架构规范)

Windows 自动化工具集与机器人脚本开发框架，Go 1.25+，CGo 启用。

## 目录结构

所有核心能力与工具生成均收拢在 `Core/` 文件夹中。

```
GoRobotScript/
├── Core/                      # 核心模块源码目录
│   ├── GoVision/              # 视觉库 (OpenCV 模板多尺度匹配、颜色卷积匹配、多点比色、区域抓屏 capture.go、ROI/多图匹配 roi.go)
│   ├── GoInput/               # 键鼠/手柄/窗口交互库 (按键、移动、后台点击、录制、虚拟 Xbox 手柄、Raw Input、识图采样 vision.go)
│   ├── GoFSM/                 # 有限状态机库 (Go 原生与 Lua 绑定的状态机驱动)
│   ├── GoPak/                 # 资源包管理库 (/Asset 目录压缩封装为 .pak、解密与解压加载)
│   ├── GoLua/                 # Lua 语言引擎 (Gopher-Lua 虚拟机封装、Go 前缀模块预加载、/Asset 资源加载)
│   │   └── LuaCLI/            # → bin/GoLua.apppak (Lua 运行时载荷，-unpak 时复制改名为可执行程序)
│   ├── GoRunner/              # 【执行器】录制 + 回放 (回放 .script、执行 .lua、自动装配 .pak 资源、识图关键点对齐)
│   │   ├── runner.go          # 执行调度核心库
│   │   ├── vision_align.go    # 识图对齐 (关键点、相似变换路径旋转/拉伸、降级策略)
│   │   ├── vision_pipeline.go # 识别流水线 (快速区域缓存、区域置信度门控、后台预取、采用前复核)
│   │   └── RunnerCLI/         # → bin/GoRunner.exe
│   ├── GoPacker/              # 【打包器】脚本与资源打包器
│   │   ├── packer.go          # 单文件打包 / 独立文件夹 / -unpak 调试模式
│   │   ├── PackerCLI/         # → bin/GoPacker.exe
│   │   └── SingleLoader/      # → bin/SingleLoader.apppak (单文件引导器载荷)
│   └── libs/                  # 16 个必要动态库 (OpenCV / MinGW / ViGEmClient) + base.version 基础模块版本号
├── bin/                       # 编译产物目录 (不入 git)
│   ├── GoRunner.exe           # Core/GoRunner 产物：录制 + 回放
│   ├── GoLua.apppak           # Core/GoLua 产物：Lua 运行时载荷 (实为 exe)
│   ├── GoPacker.exe           # Core/GoPacker 产物：打包 + -unpak 调试
│   ├── SingleLoader.apppak    # Core/GoPacker/SingleLoader 产物：单文件引导器载荷
│   ├── run/                   # GoPacker -unpak 的调试产物目录 (不入 git)
│   └── *.dll                  # 16 个运行依赖 DLL
├── Projects/                  # 用户本地定制项目根目录 (仅保留 Projects/.gitignore，内容不入 git)
│   └── .gitignore             # 忽略所有子项目
├── doc/                       # 架构规范与 AI Agent 描述文档
├── build.bat / build.ps1      # 一键全自动编译与依赖还原脚本
└── go.mod / go.sum            # 官方规范依赖管理
```

### 命名规范（重要）

**`bin/` 下的产物与 `Core/` 下的模块目录一一对应**，不得产出没有 Core 模块对应的可执行文件。
历史遗留别名（`GokeyLua.exe`、`GokeyRun.exe`、`GokeyLog.exe`、`GokeyHotkey.exe`、`PackLua.exe`）已全部废弃。
`build.ps1` 只负责生成最新产物，**不做任何历史文件清理**（清理由人工决定）。

## 构建命令

直接运行根目录下的一键构建脚本即可：
```powershell
.\build.ps1
# 或双击 build.bat
```
*(手动编译：`go build -o bin/GoRunner.exe ./Core/GoRunner/RunnerCLI`，`go build -o bin/GoPacker.exe ./Core/GoPacker/PackerCLI`)*

## 测试

`Core/` 下的关键逻辑带回归测试（识图对齐变换、识图流水线与区域置信度门控、样本命名、打包负载往返）。
测试二进制同样依赖 `bin/` 中的 OpenCV DLL，**必须先把 `bin` 放进 `PATH`**，否则报 `0xc0000135`：

```powershell
$env:PATH = "$PWD\bin;$env:PATH"
go test ./Core/...
go test -race ./Core/...     # 识图流水线有后台预取协程，提交前建议带 -race 跑一遍
```

涉及真实抓屏的用例在无屏幕环境会自动跳过。注意 `go build ./...` 会因
`Projects/legacy_archive/**` 与 `Projects/SurvivalCraft/**` 的历史残留而失败，
只构建核心请用 `go build ./Core/...`。
