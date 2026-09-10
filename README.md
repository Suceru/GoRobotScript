# GoRobotScript

基于 Go 语言（Go 1.25+）开发的轻量级 Windows 自动化框架，支持**键盘、鼠标（含 3D/VR 游戏视角）、虚拟 Xbox 手柄**的精准录制与回放，并内置完整的 **Lua 自动化脚本引擎**。

## 文档导航

| 文档 | 面向 | 内容 |
| :--- | :--- | :--- |
| **本 README** | 所有人 | 安装、录制、回放、打包快速上手 |
| [`doc/LUA_API.md`](doc/LUA_API.md) | 开发者 | Lua 接口完整手册（参数表、返回值、示例、陷阱） |
| [`doc/LUA_API_AI.md`](doc/LUA_API_AI.md) | AI / 代码生成 | 紧凑接口清单（精确签名、语义、失败模式） |
| [`doc/ARCHITECTURE.md`](doc/ARCHITECTURE.md) | 维护者 | 模块架构、打包机制、缓存布局、命名规范 |

---

## 核心组件与工具说明

通过一键编译后，`bin/` 下的每个产物都与 `Core/` 中的一个模块目录**一一对应**：

| Core 模块 | 产物 | 说明与功能定位 |
| :--- | :--- | :--- |
| `Core/GoRunner` | **GoRunner.exe** | **录制 + 回放**。无参数双击进入智能录制模式；带参数回放 `.script` 或执行 `.lua`。 |
| `Core/GoPacker` | **GoPacker.exe** | **打包工具**。把 `.lua` / `.script` 打包为单文件 EXE，或用 `-unpak` 生成明文调试目录。 |
| `Core/GoLua` | **GoLua.apppak** | **Lua 运行时载荷**（实为可执行程序，用 `.apppak` 后缀区别于工具、也避免与资源包 `.pak` 混淆）。 |
| `Core/GoPacker/SingleLoader` | **SingleLoader.apppak** | **单文件引导器载荷**。打包时被拼到产物开头，双击产物时由它解压并启动真正程序。**无需也无法手工运行**。 |

> 用户实际只需要认识 **`GoRunner.exe`** 和 **`GoPacker.exe`** 两个工具；两个 `.apppak` 是内部载荷。
> 其余 `Core/` 模块（`GoInput`、`GoVision`、`GoFSM`、`GoPak`）均为**库**，不产出可执行文件。

---

## 快速上手

### 1. 操作录制（双击即用）

1. 直接**双击打开 `bin/GoRunner.exe`**，程序进入录制待机状态。
2. 切换至目标游戏或应用窗口。
3. **全局热键控制与音频提示**（物理级扫描，全屏游戏内同样有效）：
   * **首次按 `PageUp`**：开始录制（双声上扬提示音）
   * **再次按 `PageUp`**：暂停 / 继续 切换（暂停双低音 / 继续单高音）
   * **按 `PageDown`**：结束录制并自动保存（两声下降提示音）
4. 录制产物为 `.script` 文件，自动保存在 `bin/script/`，以时间戳命名。

录制内容涵盖：键盘、鼠标（含 3D/VR 视角的原始相对位移）、**手柄全部轴向与按键**。

*也可指定输出路径：*
```powershell
bin\GoRunner.exe record
bin\GoRunner.exe record bin\script\MyTask.script
```

---

### 2. 脚本回放执行

支持以下几种回放方式：

#### 交互回放模式（推荐）
直接双击将 `.script` 脚本拖拽到 `GoRunner.exe` 上，或在命令行中运行：
```powershell
bin\GoRunner.exe bin\script\MyTask.script
```
* **按 `PageUp` 开始执行**（提供充足准备时间切换至目标窗口）；
* 回放过程中可按 `PageUp` 随时暂停/恢复；
* 遇到意外情况可按 `PageDown` 紧急强制停止退出。

#### 快速执行模式（无人值守）
加上 `-f` 参数，程序启动后直接开始执行到脚本结束，无需按键等待：
```powershell
bin\GoRunner.exe -f bin\script\MyTask.script
```

#### 倍速回放
加上 `-t` 参数指定时间缩放速率（如 `1.5` 倍速加快，`0.8` 倍速减慢）：
```powershell
bin\GoRunner.exe -t 1.5 bin\script\MyTask.script
```

---

### 3. Lua 脚本编写与执行

`GoRunner.exe` 内置完备的 Lua 运行时，支持调用 Go 原生封装的高性能键鼠控制、多尺度模板匹配、颜色匹配与有限状态机：

```powershell
bin\GoRunner.exe MyScript.lua
```

#### 常用 Lua 内置模块

| 模块 | 能力 |
| :--- | :--- |
| **`GoInput`**（别名 `SuKey`） | 键盘/组合键、鼠标移动与点击、后台窗口点击、滚轮；**虚拟 Xbox 360 手柄**（双摇杆 / 双扳机 / 14 个按键）；全局热键检测；系统提示音 |
| **`GoVision`**（别名 `SuScreen`） | 窗口截图、多尺度模板匹配、颜色卷积匹配、单点取色、多点比色 |
| **`GoFSM`** | 有限状态机调度，适合复杂流程自动化 |
| **`CallGo`** | 旧版兼容（系统对话框等） |

> **完整接口手册见 [`doc/LUA_API.md`](doc/LUA_API.md)**（含全部参数、返回值、错误约定与陷阱）。
> AI / 代码生成请用 [`doc/LUA_API_AI.md`](doc/LUA_API_AI.md)。

#### 最小示例

```lua
local input  = require("GoInput")
local vision = require("GoVision")

-- 视觉：截窗口并匹配模板，命中则点击中心
vision.CaptureClient("MyGame", "shot.png")
local r = vision.MatchTemplate("shot.png", AssetPath("btn_start.png"), 0.9, 1.1, 0.05)
if r and r.score < 0.12 then        -- score 越小越匹配
    input.Move(r.centerX, r.centerY)
    input.Click()
end

-- 键盘
input.KeyTap("e")                   -- 单键
input.KeyTap("s", "ctrl")           -- Ctrl+S
input.Sleep(200)

-- 热键循环控制（PgUp 开始/暂停，PgDn 结束）
while not input.CheckPgDnTrigger() do
    if input.CheckPgUpTrigger() then print("切换开始/暂停") end
    input.Sleep(20)                 -- 热键是上升沿，需高频轮询
end
```

#### 虚拟手柄接口（Xbox 360）

```lua
local pad = require("GoInput")

pad.Gamepad{ ly = 32767 }                                   -- 左摇杆推满向上
pad.Gamepad{ lx = 0, ly = -32768 }                          -- 左摇杆推满向下
pad.Gamepad{ buttons = pad.BTN.A }                          -- 按下 A
pad.Gamepad{ buttons = pad.BTN.START + pad.BTN.LB }         -- 组合键（掩码相加）
pad.Gamepad{ buttons = pad.BTN.A, rt = 255, rx = 20000 }    -- 按键 + 扳机 + 右摇杆
pad.Gamepad(0, 32767)                                       -- 简写：仅左摇杆
pad.GamepadReset()                                          -- 全通道归零
pad.GamepadSlots()                                          -- 已连接手柄占用的 XInput 槽位
pad.GamepadClose()                                          -- 释放虚拟手柄
```

* 表内未给出的字段**保持原值**，可「按住按键的同时推摇杆」；**摇杆/扳机是状态不是事件**，暂停或退出前务必 `GamepadReset()`。
* `pad.BTN` 常量（可直接相加）：`A B X Y` / `LB RB`（`L1 R1`）/ `LS RS`（`L3 R3`）/ `START BACK` / `UP DOWN LEFT RIGHT`。
* `buttons` 支持三种写法：数字掩码 / 名称字符串 `"A"` / 名称数组 `{"A","LB"}`。

> 虚拟手柄基于 **ViGEmBus** 总线驱动。系统未安装时 `build.ps1` 或程序会自动**静默安装**。
> 注意虚拟手柄会占用**最小空闲槽位**：若物理手柄已占槽位 0，虚拟手柄会落到槽位 1，
> 只读取槽位 0 的游戏会忽略它 —— 此时回放手柄脚本需暂时拔掉物理手柄（可用 `GamepadSlots()` 确认）。

---

### 4. 脚本与资源打包 (GoPacker)

#### 单文件独立 EXE（默认）

```powershell
# 打包 Lua 自动化脚本
bin\GoPacker.exe Projects\MyProject\MyProject.lua

# 打包录制回放脚本（.script）
bin\GoPacker.exe bin\script\VRView3D_2026-09-10_21-59-15.script
```

**GoPacker 支持两类脚本，按扩展名自动分流：**

| 脚本类型 | 打包后运行方式 | 说明 |
| :--- | :--- | :--- |
| `.lua` | Lua 引擎执行 | 自动化逻辑脚本；同目录有内容的 `Asset\` 会自动压成 `.pak` 装入 |
| `.script` | 回放引擎重放 | 录制下来的键鼠/手柄动作数据；同样支持 `-f`（直接播完）与 `-t`（倍速） |

* **`Asset\` 为空目录时视为无资源，不生成 `.pak`**。
* 输出程序名默认取脚本文件名，可用脚本内 `PackageInfo("MyBot.exe", "1.0.0")` 指定。
* 期望「独立文件夹模式」（EXE + .pak + DLL 同目录）时，在脚本里加 `-- @pack_mode folder`。

#### 调试模式（`-unpak`，不封装）

```powershell
bin\GoPacker.exe Projects\gamepad_left_stick_loop\gamepad_left_stick_loop.lua -unpak
bin\GoPacker.exe bin\script\VRView3D_2026-09-10_21-59-15.script -unpak
```

产出 `bin\run\<名称>\`，**脚本与资源保持明文、不做任何封装**：

```
Lua 脚本：                              录制脚本：
bin\run\gamepad_left_stick_loop\       bin\run\VRView3D_2026-09-10_21-59-15\
├── gamepad_left_stick_loop.exe        ├── GoRunner.exe
│     ← 由 GoLua.apppak 复制改名        │     ← 由 GoRunner.exe 复制（回放引擎）
├── gamepad_left_stick_loop.lua        ├── VRView3D_..._21-59-15.script
├── Asset\                             └── *.dll
└── *.dll                              （运行：GoRunner.exe <脚本>.script）
```

* Lua 形态：**双击 `<名称>.exe` 即可运行**（无参数时自动执行同目录同名 `.lua`，其次 `main.lua`，再其次目录内唯一的 `.lua`）。
* 改完脚本或资源 **直接重跑，无需重新打包** —— 这就是它存在的意义。

---

### 5. 录制脚本 `.script` 格式

录制产物是 **JSON Lines**（每行一个动作帧），纯文本、可直接手改：

```json
{"dt":3,"op":"init","x":1122,"y":596}
{"dt":1455,"op":"gp","gp_ly":2258}
{"dt":15,"op":"rmv","dx":-3,"dy":1}
```

| 字段 | 说明 |
| :--- | :--- |
| `dt` | 距上一帧的毫秒间隔（回放按此节流，受 `-t` 缩放） |
| `op` | 动作类型：`init` 起点锚定 / `mv` 光标绝对移动 / `rmv` 3D视角相对位移 / `kd`·`ku` 键按下弹起 / `md`·`mu` 鼠标按下弹起 / `mw` 滚轮 / `gp` 手柄一帧 |

手柄帧 `gp` 的字段与 Lua 手柄接口**一一对应**：
`gp_b`→`buttons`、`gp_lt`/`gp_rt`→`lt`/`rt`、`gp_lx`/`gp_ly`→`lx`/`ly`、`gp_rx`/`gp_ry`→`rx`/`ry`。

> 完整字段表见 [`doc/LUA_API.md` 第 9 节](doc/LUA_API.md#9-录制脚本-script-格式)。

---

## 编译与构建说明

本项目采用标准的 PowerShell / Bat 一键构建脚本，支持依赖库自动还原与编译：

```powershell
# 在项目根目录下执行
.\build.ps1
# 或直接双击运行 build.bat
```

构建脚本会自动将 `Core/libs` 中的 16 个必要运行时动态库（OpenCV / MinGW / ViGEmClient）同步还原至 `bin/` 目录，并编译生成全部核心模块产物。

> `build.ps1` **只生成最新产物，不清理任何历史文件**（清理由人工决定）。

### 基础模块版本（base.version）

`Core/libs/base.version` 定义**基础模块（DLL 集合）的版本号**。打包产物运行时会把 DLL 解压到：

```
%LOCALAPPDATA%\GoRobotScript\base\b_<版本号>_<DLL内容哈希>\
```

同一基线的所有包**共享同一份**（约 56 MB，不再逐包重复占用）；升级 DLL 后把版本号改成 `1.0.1` 再构建，新旧基线因哈希不同而**并行共存、互不冲突**，旧包继续使用它自己的基线正常运行。超过 7 天未使用的缓存目录会被自动回收（可用环境变量 `GOROBOT_CACHE_KEEP_DAYS` 调整）。

---

## 依赖说明

* [robotn/gohook](https://github.com/robotn/gohook)：系统底层全局键鼠事件监听。
* [go-vgo/robotgo](https://github.com/go-vgo/robotgo)：跨平台键鼠模拟输入。
* [gopher-lua](https://github.com/yuin/gopher-lua)：Go 原生 Lua 虚拟机与编译器。
* [GoCV](https://gocv.io/)：OpenCV 4.13+ 计算机视觉与模板匹配。
* [ViGEmBus / ViGEmClient](https://github.com/nefarius/ViGEmBus)：虚拟 Xbox 360 手柄总线驱动与客户端。

---

## 开源协议与作者

* **License**：MIT
* **Author**：Suceru
