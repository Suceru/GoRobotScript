# GoRobotScript (核心架构规范)

Windows 自动化工具集与机器人脚本开发框架，Go 1.25+，CGo 启用。

## 目录结构

所有核心能力与工具生成均收拢在 `Core/` 文件夹中。

```
GoRobotScript/
├── Core/                      # 核心模块源码目录
│   ├── GoVision/              # 视觉核心库 (OpenCV 模板多尺度匹配、颜色卷积匹配、多点比色)
│   ├── GoInput/               # 键鼠与窗口交互库 (按键、输入、平滑移动、后台点击、操作录制与循环热键)
│   ├── GoFSM/                 # 有限状态机库 (Go 原生与 Lua 绑定的状态机驱动)
│   ├── GoPak/                 # 资源包管理库 (/Asset 目录压缩封装为 .pak、解密与解压加载)
│   ├── GoLua/                 # Lua 语言引擎 (Gopher-Lua 虚拟机封装、Go 前缀模块预加载、/Asset 资源加载)
│   ├── GoRunner/              # 【核心执行器】统一调用执行器 (执行录制脚本与 Lua 脚本，自动装配 .pak 资源)
│   │   ├── runner.go          # 执行调度核心库
│   │   └── RunnerCLI/         # GoRunner.exe 入口
│   ├── GoPacker/              # 【核心打包器】脚本与资源打包器
│   │   ├── packer.go          # 双模式打包引擎核心逻辑
│   │   ├── PackerCLI/         # PackLua.exe 入口
│   │   └── SingleLoader/      # SingleLoader.exe 引导器模板
│   └── libs/                  # 15 个必要动态库永久持久化存储 (用于构建还原与打包)
├── bin/                       # 核心可执行文件生成目录 (仅生成核心模块產物，不入 git)
│   ├── GoRunner.exe           # 核心统一脚本执行器 (兼容 GokeyLua.exe / GokeyRun.exe)
│   ├── PackLua.exe            # 核心脚本与资源打包器
│   ├── SingleLoader.exe       # 核心单文件引导器
│   └── *.dll                  # 15 个运行依赖 DLL
├── Projects/                  # 用户本地定制项目根目录 (仅保留 Projects/.gitignore，内容不入 git)
│   └── .gitignore             # 忽略所有子项目
├── doc/                       # 架构规范与 AI Agent 描述文档
├── build.bat / build.ps1      # 一键全自动编译与依赖还原脚本
└── go.mod / go.sum            # 官方规范依赖管理
```

## 构建命令

直接运行根目录下的一键构建脚本即可：
```powershell
.\build.ps1
# 或双击 build.bat
```
*(手动编译：`go build -o bin/GoRunner.exe ./Core/GoRunner/RunnerCLI`，`go build -o bin/PackLua.exe ./Core/GoPacker/PackerCLI`)*
