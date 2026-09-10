# GoRobotScript 架构与 AI Agent 协作规范

本文档为 AI Agent 以及开发者提供 `GoRobotScript` 模块架构、代码规范及调用方式的标准化描述。

---

## 1. 核心目录与职责结构 (Directory Layout)

所有核心模块与可执行工具入口均统一归整在 `Core/` 文件夹内，根目录下不再保留 `cmd/`：

```
GoRobotScript/
├── Core/                      # ★ 全部板块、核心基础模块与工具入口统统在此
│   ├── GoVision/              # 视觉核心基础库 (OpenCV 模板多尺度匹配、颜色网格匹配、多点比色)
│   ├── GoInput/               # 键鼠/手柄/窗口交互库 (按键、移动、后台消息点击、操作录制、虚拟 Xbox 手柄、Raw Input)
│   ├── GoFSM/                 # 独立有限状态机库 (Go 原生与 Lua 联动的状态机驱动)
│   ├── GoPak/                 # 资源包管理库 (/Asset 目录压缩封装为 .pak、解密与解压加载)
│   ├── GoLua/                 # 【板块1】Lua 脚本引擎模块
│   │   ├── engine.go          # Gopher-Lua 虚拟机封装、Go 前缀模块预加载、/Asset 自动加载
│   │   └── LuaCLI/            # → bin/GoLua.apppak (Lua 运行时载荷，-unpak 时复制改名为可执行程序)
│   ├── GoRunner/              # 【板块2】录制与回放执行器模块
│   │   ├── runner.go          # 执行调度核心库 (.script 回放 / .lua 执行 / .pak 装配)
│   │   └── RunnerCLI/         # → bin/GoRunner.exe (无参数 = 录制模式)
│   ├── GoPacker/              # 【板块3】脚本与资源打包器模块
│   │   ├── packer.go          # 单文件打包 / 独立文件夹 / -unpak 调试模式
│   │   ├── PackerCLI/         # → bin/GoPacker.exe
│   │   └── SingleLoader/      # → bin/SingleLoader.apppak (单文件解压引导器载荷)
│   └── libs/                  # 16 个必要动态库 (OpenCV / MinGW / ViGEmClient) + base.version 基础模块版本号
├── bin/                       # 编译构建输出产物 (不入 git)
│   ├── GoRunner.exe           # Core/GoRunner 产物
│   ├── GoLua.apppak           # Core/GoLua 产物 (Lua 运行时载荷，实为 exe)
│   ├── GoPacker.exe           # Core/GoPacker 产物
│   ├── SingleLoader.apppak    # Core/GoPacker/SingleLoader 产物 (引导器载荷)
│   ├── run/                   # GoPacker -unpak 调试产物目录
│   └── *.dll                  # 16 个运行依赖 DLL
├── Projects/                  # 各具体业务项目定制化脚本、资源与历史归档（不污染根目录）
│   ├── SurvivalCraft/         # 生存战争自动化工程
│   ├── gamepad_left_stick_loop/ # 手柄推杆示例项目
│   └── legacy_archive/        # 历史旧模块与旧入口归档
├── doc/                       # 架构设计、AI Agent 描述与规范文档
│   └── ARCHITECTURE.md        # 本规范文档
├── go.mod                     # Go 模块管理文件 (严格采用官方 go mod 规范)
└── go.sum                     # 依赖校验文件
```

### 命名规范（重要）
**`bin/` 下的产物与 `Core/` 下的模块目录一一对应**，不得产出没有 Core 模块对应的可执行文件。
历史遗留别名（`GokeyLua.exe`、`GokeyRun.exe`、`GokeyLog.exe`、`GokeyHotkey.exe`、`PackLua.exe`）已全部废弃。
`build.ps1` 只负责生成最新产物，**不做任何历史文件清理**。

---

## 2. 核心业务板块

### 板块 1：Lua 语言脚本引擎 (`GoLua`)
- **包路径**：`GoRobotScript/Core/GoLua`
- **模块注册规范**：统一以前缀 `Go` 开头导出，同时保留旧模块名（`SuScreen`, `SuKey`, `CallGo`）作为兼容别名。
- **内置导出模块**：
  - `GoVision`：`MatchTemplate`, `MatchColorGrid`, `GetPixel`, `CheckMultiColor`, `CaptureClient`, `CaptureScreen`
  - `GoInput`：`KeyTap`, `TypeStr`, `Click`, `Move`, `Sleep`, `ClickClient`
  - `GoInput` 手柄：`Gamepad`, `GamepadReset`, `GamepadSlots`, `GamepadClose`, `BTN` 常量表
  - `GoInput` 热键/音频：`CheckPgUpTrigger`, `CheckPgDnTrigger`, `SoundStart`, `SoundPause`, `SoundResume`, `SoundStop`
  - `GoFSM`：`new`, `addState`, `setInitial`, `step`, `getCurrent`
  - 全局辅助：`AssetPath("tpl.png")` 快速获取 `/Asset` 下的资源绝对路径。

### 板块 2：录制与调用执行器 (`GoRunner`)
- **包路径**：`GoRobotScript/Core/GoRunner`
- **录制能力（无参数启动）**：`PgUp` 开始 / 暂停继续，`PgDn` 结束保存；同时采集键鼠、3D/VR 原始相对位移（Raw Input）与手柄轴向。
- **资源自动装配规范**：
  - 执行器在加载并运行 `.lua` 脚本时，自动检测脚本同级目录下的 `Asset/` 文件夹以及同名 `.pak` 资源包。
  - 若存在 `.pak` 资源包，执行器会自动解包至 `/Asset` 目录并挂载；**无 `.pak` 时不创建 `Asset/` 目录**。
- **打包产物识别**：执行器启动时优先检测自身尾部是否存在 GoPacker 写入的内嵌脚本负载，命中则直接执行该脚本（即「单文件独立 EXE」的运行形态）。

### 板块 3：脚本与资源打包器 (`GoPacker`)
- **包路径**：`GoRobotScript/Core/GoPacker`
- **运行时模板**：固定取自 `Core/GoRunner` 的产物 `GoRunner.exe`，脚本被焊接进该模板尾部。
- **内嵌负载类型（按扩展名自动判定）**：

  | 扩展名 | 负载类型 | 运行时引擎 | 说明 |
  | :--- | :--- | :--- | :--- |
  | `.lua` | `lua` | Lua 引擎 (`GoLua`) | 自动化逻辑脚本，支持 `Asset/` 资源 |
  | `.script` | `script` | 回放引擎 (`GoRunner`) | 录制的键鼠/手柄动作数据 (JSON Lines)，无需资源 |

- **负载包格式**：
  - 当前尾标 `GOKEYLUA_EMBEDDED_PAYLOAD_V2` → `[名称][版本][类型][脚本]`（各字符串带 uint32 长度前缀）
  - 兼容旧尾标 `GOKEYLUA_EMBEDDED_PAYLOAD_V1` → `[名称][版本][脚本]`（无类型字段，恒按 Lua 处理）
- **三种产物形态**：
  1. **纯单文件 EXE 模式（默认）**：
     - 将脚本、`/Asset` 打包后的 `.pak` 资源包、16 个 OpenCV/MinGW/ViGEm 依赖 DLL 全部压缩打包进一个 EXE。
     - 运行无需配置环境，双击即可直接运行。录制脚本产物同样支持 `-f`（直接播完）与 `-t`（倍速）。
  2. **独立文件夹模式（DLL 外部自动引用）**：
     - 在 Lua 脚本中指定标识：
       ```lua
       -- @pack_mode folder
       -- 或 PackageInfo("MyBot.exe", "1.0.0", "folder")
       ```
     - 生成 `{AppName}_Dist/` 文件夹，主程序 EXE、`.pak` 资源包 与 16 个依赖 DLL 放于同级，运行时通过 Windows 默认搜索路径自动引用外部 DLL，无需手动干预。
  3. **调试模式 `-unpak`（不做任何封装）**：
     - 命令：`GoPacker.exe <脚本> -unpak`
     - 产出 `bin/run/<AppName>/`，按脚本类型分流：
       - `.lua` → `<AppName>.exe`（由 `bin/GoLua.apppak` 复制改名）+ 明文 `.lua` + `Asset/` + DLL
         → 无参数启动自动执行同目录同名 `.lua`（其次 `main.lua`，再其次目录内唯一的 `.lua`），**双击即可调试**。
       - `.script` → `GoRunner.exe`（回放引擎本体）+ 明文 `.script` + DLL
         → 运行方式：`GoRunner.exe <脚本>.script`
     - 两种情况都**改完脚本无需重新打包**。
- **`GoLua.apppak` 的由来**：`Core/GoLua` 的产物在 bin 中用 `.apppak` 后缀存放，既区别于工具类 exe，也避免与资源包 `.pak` 混淆；`-unpak` 时复制到调试目录并改名为可执行程序。
- **空资源约定**：`Asset/` 为空目录（或仅含空子目录）时视为无资源，不生成 `.pak`。
- **`.pak` 识别**：加载器只把文件头为 `PK`（zip）的 `.pak` 当作资源包，非 zip 的同名文件一律跳过。
- **运行时缓存布局（基础模块与定制内容分离）**：
  ```
  %LOCALAPPDATA%\GoRobotScript\
  ├── base\b_<基础模块版本>_<DLL内容哈希>\   # 全部依赖 DLL，同基线各包共享一份
  └── runtime_<包内容哈希>\                  # runner.exe（含焊入脚本）与 assets.pak
  ```
  基础模块按「版本号 + 内容哈希」寻址，不同基线**并行共存、互不冲突**；超过 7 天未使用的目录自动回收。

### 板块 4：基础功能封装以 `Go` 为前缀 (`GoVision` / `GoInput`)
- **通用性要求**：所有图像识别、颜色比对、多尺度缩放、按键、窗口捕获底层代码均收拢在 `Core/GoVision` 和 `Core/GoInput` 中。
- **跨项目复用**：任何具体游戏、应用（如 Projects/SurvivalCraft、Projects/gamepad_left_stick_loop）仅作为客户端调用通用接口，不再为单个项目开发定制底层代码。

### 板块 5：状态机模块 (`GoFSM`)
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
  .\build.ps1
  # 或等价的单模块手动编译：
  go build -o bin/GoRunner.exe     ./Core/GoRunner/RunnerCLI
  go build -o bin/GoLua.apppak     ./Core/GoLua/LuaCLI
  go build -o bin/GoPacker.exe     ./Core/GoPacker/PackerCLI
  go build -o bin/SingleLoader.apppak ./Core/GoPacker/SingleLoader
  ```
- **产出命名规范**：`bin/` 产物与 `Core/` 模块一一对应；不得产出无 Core 模块对应的别名（历史 `GokeyLua.exe` / `GokeyRun.exe` / `GokeyLog.exe` / `GokeyHotkey.exe` / `PackLua.exe` 均已废弃）。`build.ps1` 只生成产物，不清理历史文件。
