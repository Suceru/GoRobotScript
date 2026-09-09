# GoRobotScript 架构与 AI Agent 协作规范

本文档为 AI Agent 以及开发者提供 `GoRobotScript` 模块架构、代码规范及调用方式的标准化描述。

---

## 1. 核心目录与职责结构 (Directory Layout)

所有核心模块与可执行工具入口均统一归整在 `Core/` 文件夹内，根目录下不再保留 `cmd/`：

```
GoRobotScript/
├── Core/                      # ★ 全部板块、核心基础模块与工具入口统统在此
│   ├── GoVision/              # 视觉核心基础库 (OpenCV 模板多尺度匹配、颜色网格匹配、多点比色)
│   ├── GoInput/               # 键鼠与窗口交互库 (按键、输入、平滑移动、后台消息点击、循环热键控制器)
│   ├── GoFSM/                 # 独立有限状态机库 (Go 原生与 Lua 联动的状态机驱动)
│   ├── GoPak/                 # 资源包管理库 (/Asset 目录压缩封装为 .pak、解密与解压加载)
│   ├── GoRecord/              # 【板块1】操作录制模块
│   │   ├── record.go          # 录制基础库
│   │   └── RecordCLI/         # 独立录制命令行工具入口 (对应 GokeyLog)
│   ├── GoLua/                 # 【板块2】Lua 脚本引擎模块
│   │   ├── engine.go          # Gopher-Lua 虚拟机封装、Go 前缀模块预加载、/Asset 自动加载
│   │   └── LuaCLI/            # 独立 Lua 运行时解释器入口 (对应 GokeyLua)
│   ├── GoRunner/              # 【板块3】脚本与资源调用执行器模块
│   │   ├── runner.go          # 执行调度核心库
│   │   └── RunnerCLI/         # 独立调用执行器程序入口 (对应 GokeyRun)
│   ├── GoPacker/              # 【板块4】脚本与资源打包器模块
│   │   ├── packer.go          # 打包引擎核心逻辑
│   │   ├── PackerCLI/         # 独立打包工具入口 (对应 PackLua)
│   │   └── SingleLoader/      # 纯 Go 单文件极速解压引导器
│   └── GoHotkey/              # 【板块5】热键控制器程序入口 (对应 GokeyHotkey)
├── bin/                       # 编译构建输出产物与 15 个 OpenCV/MinGW 依赖 DLL (runtime_dlls/)
├── Projects/                  # 各具体业务项目定制化脚本、资源与历史归档（不污染根目录）
│   ├── SurvivalCraft/         # 生存战争自动化工程
│   └── legacy_archive/        # 历史旧模块与旧入口归档
├── doc/                       # 架构设计、AI Agent 描述与规范文档
│   └── ARCHITECTURE.md        # 本规范文档
├── go.mod                     # Go 模块管理文件 (严格采用官方 go mod 规范)
└── go.sum                     # 依赖校验文件
```

---

## 2. 六大核心业务板块

### 板块 1：操作录制成可执行脚本 (`GoRecord` / `GokeyLog`)
- **包路径**：`GoRobotScript/Core/GoRecord`
- **输出格式**：JSON Lines 标准 `.script` 文件，每行包含一个键盘/鼠标事件数据。
- **调用示例**：
  ```go
  rec, err := GoRecord.StartRecording("script/demo.script", '[')
  // ... 录制中，按快捷键 '[' 或调用 rec.Stop() 结束录制
  rec.Stop()
  ```

### 板块 2：Lua 语言脚本引擎 (`GoLua`)
- **包路径**：`GoRobotScript/Core/GoLua`
- **模块注册规范**：统一以前缀 `Go` 开头导出，同时保留旧模块名（`SuScreen`, `SuKey`, `CallGo`）作为兼容别名。
- **内置导出模块**：
  - `GoVision`：`MatchTemplate`, `MatchColorGrid`, `GetPixel`, `CheckMultiColor`, `CaptureClient`, `CaptureScreen`
  - `GoInput`：`KeyTap`, `TypeStr`, `Click`, `Move`, `Sleep`, `ClickClient`
  - `GoRecord`：`StartRecording`
  - `GoFSM`：`new`, `addState`, `setInitial`, `step`, `getCurrent`
  - 全局辅助：`AssetPath("tpl.png")` 快速获取 `/Asset` 下的资源绝对路径。

### 板块 3：调用执行器 (`GoRunner` / `GokeyRun`)
- **包路径**：`GoRobotScript/Core/GoRunner`
- **资源自动装配规范**：
  - 执行器在加载并运行 `.lua` 脚本时，自动检测脚本同级目录下的 `Asset/` 文件夹以及同名 `.pak` 资源包。
  - 若存在 `.pak` 资源包，执行器会自动解包至 `/Asset` 目录并挂载，供脚本内的图像识别与匹配无缝调用。

### 板块 4：脚本与资源打包器 (`GoPacker` / `PackLua`)
- **包路径**：`GoRobotScript/Core/GoPacker`
- **两种打包模式**：
  1. **纯单文件 EXE 模式（默认）**：
     - 将 Lua 脚本、`/Asset` 打包后的 `.pak` 资源包、15 个 OpenCV/MinGW 依赖 DLL 全部压缩打包进一个 EXE。
     - 运行无需配置环境，双击即可直接运行。
  2. **独立文件夹模式（DLL 外部自动引用）**：
     - 在 Lua 脚本中指定标识：
       ```lua
       -- @pack_mode folder
       -- 或 PackageInfo("MyBot.exe", "1.0.0", "folder")
       ```
     - 生成 `{AppName}_Dist/` 文件夹，主程序 EXE、`.pak` 资源包 与 15 个依赖 DLL 放于同级，运行时通过 Windows 默认搜索路径自动引用外部 DLL，无需手动干预。

### 板块 5：基础功能封装以 `Go` 为前缀 (`GoVision` / `GoInput`)
- **通用性要求**：所有图像识别、颜色比对、多尺度缩放、按键、窗口捕获底层代码均收拢在 `Core/GoVision` 和 `Core/GoInput` 中。
- **跨项目复用**：任何具体游戏、应用（如 Projects/SurvivalCraft、Projects/OtherGame）仅作为客户端调用通用接口，不再为单个项目开发定制底层代码。

### 板块 6：状态机模块 (`GoFSM`)
- **包路径**：`GoRobotScript/Core/GoFSM`
- **支持语言**：Go 原生接口与 Lua 绑定接口。
- **Lua 状态机示例**：
  ```lua
  local GoFSM = require("GoFSM")
  local fsm = GoFSM.new()

  fsm:addState("CHECK_SCREEN", function(ctx)
      -- 图像或颜色比对
      return "NEXT_STATE"
  end)

  fsm:setInitial("CHECK_SCREEN")
  fsm:step()
  ```

---

## 3. Go 依赖与环境规范
- 严格使用 `go.mod` 管理依赖，禁止在仓库中提交庞大私有目录（如原有的 `third_party/robotgo` 已全面切换回规范模块依赖 `github.com/go-vgo/robotgo v1.0.2`）。
- 禁止将外部工具构建链（如 cmake 树）提交进源码仓库。
- 构建命令统一为：
  ```bash
  go build -o bin/GokeyLua.exe ./GokeyLua
  go build -o bin/SingleLoader.exe ./cmd/SingleLoader
  go build -o bin/PackLua.exe ./cmd/PackLua
  go build -o bin/GokeyRun.exe ./GokeyRun
  go build -o bin/GokeyLog.exe ./GokeyLog
  ```
