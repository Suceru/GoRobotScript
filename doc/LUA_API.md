# GoRobotScript Lua API 手册（开发者版）

> 面向人类开发者的完整接口说明，含参数表、返回值、示例与注意事项。
> AI / 代码生成场景请使用更紧凑的 [`LUA_API_AI.md`](./LUA_API_AI.md)。

---

## 目录

- [1. 运行与加载](#1-运行与加载)
- [2. 全局函数](#2-全局函数)
- [3. GoInput — 键鼠 / 手柄 / 热键](#3-goinput--键鼠--手柄--热键)
- [4. GoVision — 屏幕与视觉识别](#4-govision--屏幕与视觉识别)
- [5. GoFSM — 有限状态机](#5-gofsm--有限状态机)
- [6. 兼容别名模块](#6-兼容别名模块)
- [7. 按键名称表](#7-按键名称表)
- [8. 手柄按键掩码表](#8-手柄按键掩码表)
- [9. 录制脚本 .script 格式](#9-录制脚本-script-格式)
- [10. 返回值与错误约定](#10-返回值与错误约定)
- [11. 完整示例](#11-完整示例)
- [12. 常见陷阱](#12-常见陷阱)

---

## 1. 运行与加载

Lua 脚本由 **`GoRunner.exe`** 执行（`GoLua.apppak` 为纯 Lua 运行时载荷，调试目录中被复制为可执行文件）：

```powershell
bin\GoRunner.exe MyScript.lua            # 直接执行
bin\GoRunner.exe bin\script\x.script     # 回放录制脚本（PgUp 开始/暂停，PgDn 停止）
bin\GoRunner.exe -f bin\script\x.script  # 回放录制脚本：直接播完，不等按键
bin\GoRunner.exe -t 1.5 x.script         # 回放倍速
bin\GoRunner.exe -vs 85 x.script        # 回放识图匹配度阈值（百分比，越大越严格）
bin\GoRunner.exe -vdir D:\samples x.script  # 手动指定识图样本目录
```

脚本内通过标准 `require` 加载内置模块：

```lua
local pad     = require("GoInput")    -- 键鼠 / 手柄 / 热键
local vision  = require("GoVision")   -- 屏幕截图 / 模板匹配 / 比色
local FSM     = require("GoFSM")      -- 有限状态机
```

**已注册模块**：`GoInput`、`GoVision`、`GoFSM`、`SuKey`、`SuScreen`、`CallGo`
**未注册**：`GoRecord`（录制器已并入 `GoRunner`，无 Lua 接口）

---

## 2. 全局函数

### `AssetPath(sub)`

取得程序 `Asset/` 资源目录下的绝对路径。

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| `sub` | string | 否 | 相对子路径，缺省为 `""`（返回 `Asset/` 自身） |

**返回**：`string` — **绝对路径**（正斜杠分隔）。

```lua
local p = AssetPath("tpl/button.png")
-- → D:/.../Asset/tpl/button.png
```

> ⚠️ 仅做路径拼接，**不检查文件是否存在**。打包后资源由 `.pak` 解压产生；若程序无资源包，`Asset/` 目录不会被创建。
> 返回的是脚本所在目录下的 `Asset/`，与进程当前工作目录无关，可直接交给 `GoVision` 的匹配函数。

---

### `AppInfo()`

**返回**：`table { name = string, version = string }`

- `name` = 程序所在目录名（未打包时为运行目录名）
- `version` = 固定 `"1.0.0"`

---

### `PackageInfo(name, version, mode)` / `PackageMode(mode)`

**这两个函数不产生运行时行为**，仅供 `GoPacker` 在打包时用正则解析脚本文本，以决定产物名与打包模式。

```lua
PackageInfo("MyBot.exe", "2.1.0")              -- 指定产物名与版本
PackageInfo("MyBot.exe", "2.1.0", "folder")    -- 同时指定「独立文件夹模式」
```

等价的注释写法（推荐，更不易被误认为是运行时调用）：

```lua
-- @pack_mode folder
-- @external_dll
```

---

## 3. GoInput — 键鼠 / 手柄 / 热键

```lua
local input = require("GoInput")
```

### 3.1 键盘

#### `input.KeyTap(key [, ...mods])`

按下并释放按键或组合键。

| 形式 | 示例 |
| :--- | :--- |
| 单键 | `input.KeyTap("a")` |
| 修饰键组合 | `input.KeyTap("c", "ctrl")` |
| 表形式 | `input.KeyTap({"c", "ctrl"})` |

**返回**：无

> 表形式取 `arr[1]` 作为主键，其余作为修饰键；表内元素顺序即参数顺序。
> 键名遵循 robotgo 约定，见 [按键名称表](#7-按键名称表)。

#### `input.TypeStr(text)`

逐字符输入字符串。**返回**：无

---

### 3.2 鼠标

#### `input.Move(x, y)`

瞬间移动光标到屏幕绝对坐标。**返回**：无

#### `input.Click([button, double])`

| 参数 | 类型 | 默认 | 说明 |
| :--- | :--- | :--- | :--- |
| `button` | string | `"left"` | `"left"` / `"right"` / `"center"` |
| `double` | boolean | `false` | 是否双击 |

**返回**：无

#### `input.ClickClient(title, cx, cy)`

以 **窗口消息**（而非物理鼠标）方式点击指定窗口的客户区坐标——不移动真实光标，适合后台操作。

| 参数 | 类型 | 说明 |
| :--- | :--- | :--- |
| `title` | string | 窗口标题（子串匹配） |
| `cx`, `cy` | int | 客户区坐标 |

**返回**：`boolean` — 是否找到窗口并投递成功。

---

### 3.3 定时

#### `input.Sleep(ms)`

阻塞指定毫秒数。**返回**：无

> 脚本的主循环节奏靠它控制，建议 15~30 ms 量级。

---

### 3.4 全局热键（轮询式）

#### `input.CheckPgUpTrigger()`

#### `input.CheckPgDnTrigger()`

**返回**：`boolean` — 自**上一次调用**以来，该键是否发生了「按下」的**上升沿**。

> **必须在循环中周期性调用**（建议 20 ms 一次）。它内部保存上次的按下状态，
> 每次调用都会刷新状态；如果你 5 秒才调一次，中间的按键不会被分别识别。
>
> 使用 `GetAsyncKeyState` 物理扫描，**不受窗口焦点影响**，全屏游戏内同样有效。

```lua
while true do
    if input.CheckPgDnTrigger() then break end
    input.Sleep(20)
end
```

---

### 3.5 系统提示音

| 函数 | 音效 |
| :--- | :--- |
| `input.SoundStart()` | 双声上扬 |
| `input.SoundPause()` | 双声下降（低音） |
| `input.SoundResume()` | 单声高音 |
| `input.SoundStop()` | 双声下降 |

**返回**：均为无

---

### 3.6 虚拟手柄（Xbox 360）

底层通过 **ViGEmBus** 总线驱动向系统挂载一个虚拟 Xbox 360 手柄，游戏可像识别真实手柄一样识别它。

#### `input.Gamepad([lx, ly])` / `input.Gamepad{...}`

**一次调用即可提交任意通道**，未给出的字段**保持当前值不变**（因此可以「按住 A 键的同时推摇杆」）。

表字段：

| 字段 | 类型 | 范围 | 说明 |
| :--- | :--- | :--- | :--- |
| `lx`, `ly` | int | -32768 ~ 32767 | 左摇杆（移动） |
| `rx`, `ry` | int | -32768 ~ 32767 | 右摇杆（视角） |
| `lt`, `rt` | int | 0 ~ 255 | 左 / 右扳机 |
| `buttons` | number \| string \| table | — | 按键掩码 / 名称 / 名称数组 |

**返回**：成功 `true`；失败 `false, 错误信息`

```lua
-- 表形式：只改左摇杆
input.Gamepad{ lx = 0, ly = 32767 }

-- 简写形式：等价于只设左摇杆
input.Gamepad(0, 32767)

-- 按键：单键 / 组合键（掩码相加）
input.Gamepad{ buttons = input.BTN.START }
input.Gamepad{ buttons = input.BTN.A + input.BTN.LB }

-- buttons 的三种写法等价
input.Gamepad{ buttons = input.BTN.A }
input.Gamepad{ buttons = "A" }
input.Gamepad{ buttons = {"A"} }

-- 组合：按住 A 键 + 右扳机按到底 + 右摇杆右偏
input.Gamepad{ buttons = input.BTN.A, rt = 255, rx = 20000 }
```

> ⚠️ **摇杆与扳机是「状态」不是「事件」**：设置后会一直保持，直到你再次修改或调用 `GamepadReset()`。

#### `input.GamepadReset()`

所有通道归零：摇杆回中、扳机松开、按键全部弹起。**返回**：`boolean`

#### `input.GamepadSlots()`

**返回**：`table` — 当前已连接手柄占用的 XInput 槽位数组，如 `{0, 1}`。

> 用于诊断：虚拟手柄会占用**最小空闲槽位**。若你的物理手柄已占 0，虚拟手柄会落到 1，
> 而只读取槽位 0 的游戏会忽略它——此时需暂时拔掉物理手柄。

#### `input.GamepadClose()`

释放虚拟手柄（摇杆回中并卸载设备）。**返回**：无

#### `input.GamepadLeftStick(lx, ly)` / `input.GamepadRightStick(rx, ry)`

旧版兼容接口，等价于 `Gamepad{ lx=..., ly=... }` / `Gamepad{ rx=..., ry=... }`。
**返回**：`boolean` 或 `false, 错误信息`

---

## 4. GoVision — 屏幕与视觉识别

```lua
local vision = require("GoVision")
```

所有匹配函数基于 OpenCV **归一化平方差**（`TM_SQDIFF_NORMED`），因此
**`score` 越接近 0 越匹配**（不是越大越匹配）；同时每个结果里都有一个与指标无关的
**`percent`（匹配度百分比 `0~100`，越大越像）**，日常判断用它更直观，
也和回放端识图对齐的阈值 `-vs` 是同一套口径。

> 回放端的识图对齐内部改用 **ZNCC（零均值归一化互相关）** 以获得对模糊/锐化的鲁棒性，
> 见 [第 9 节「识图对齐」](#识图对齐回放行为)；Lua 侧仍保持 `TM_SQDIFF_NORMED` 不变。

### `vision.MatchTemplate(screenPath, tplPath [, minScale, maxScale, step])`

多尺度模板匹配。在 `minScale`~`maxScale` 之间按 `step` 遍历缩放模板并取最优。

| 参数 | 类型 | 默认 | 说明 |
| :--- | :--- | :--- | :--- |
| `screenPath` | string | 必填 | 截图文件路径 |
| `tplPath` | string | 必填 | 模板图片路径 |
| `minScale` | number | `0.5` | 最小缩放比 |
| `maxScale` | number | `1.5` | 最大缩放比 |
| `step` | number | `0.05` | 缩放步长 |

**返回**：成功 `table`；失败 `nil, 错误信息`

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `x`, `y` | number | 匹配区域左上角坐标 |
| `width`, `height` | number | 匹配区域尺寸（已含缩放） |
| `scale` | number | 命中的缩放比 |
| `score` | number | 原始指标值，**越小越好**（0 为完美） |
| `percent` | number | **匹配度百分比 `0~100`，越大越像**（推荐用它做判断） |
| `centerX`, `centerY` | number | 匹配区域中心坐标（可直接用于点击） |

> `score` 与 `percent` 都是同一个匹配结果的两种表示：Lua 侧走的是
> `TM_SQDIFF_NORMED`，所以 `score` 是"误差"（越小越好）；
> `percent` 是统一换算出的相似度（`0~100`，越大越好），
> 回放端的识图对齐用的阈值 `-vs` 也是同一套百分比口径。

```lua
vision.CaptureClient("Notepad", "screen.png")
local r, err = vision.MatchTemplate("screen.png", AssetPath("btn_ok.png"), 0.8, 1.2, 0.05)
if r and r.percent >= 85 then
    input.Move(r.centerX, r.centerY)
    input.Click()
end
```

### `vision.MatchColorGrid(screenPath, tplPath [, rows, cols, minScale, maxScale, step])`

**颜色卷积匹配**：把模板与候选区域都缩放到 `rows × cols` 的色块网格后比较。
对细节纹理不敏感、对整体配色敏感，适合抗压缩噪点干扰。

| 参数 | 类型 | 默认 |
| :--- | :--- | :--- |
| `rows`, `cols` | int | `3`, `3` |
| `minScale` / `maxScale` / `step` | number | `0.5` / `1.5` / `0.05` |

**返回**：同 `MatchTemplate`（字段一致，同样含 `percent`），失败为 `nil, 错误信息`

### `vision.GetPixel(screenPath, x, y)`

读取指定截图指定坐标的像素颜色。

**返回**：成功 `r, g, b`（**三个独立数值**）；失败 `nil, 错误信息`

```lua
local r, g, b = vision.GetPixel("screen.png", 100, 200)
```

### `vision.CheckMultiColor(screenPath, points [, tolerance])`

多点比色：一次性校验多个坐标的颜色是否都与期望相符。

| 参数 | 类型 | 说明 |
| :--- | :--- | :--- |
| `points` | table | `{ {x=,y=,r=,g=,b=}, ... }` |
| `tolerance` | int | 默认 `20`，每个通道允许的误差 |

**返回**：成功 `matched(boolean), ratio(number)`；失败 `false, 错误信息`

- `matched` — 是否**全部**点都匹配
- `ratio` — 匹配比例 `命中点数 / 有效点数`（可做模糊判定）
- `points` 为空表时返回 `true, 1.0`（**但图片仍会先被加载**，路径无效时依然报错）
- 坐标越界的点会被跳过，不参与统计；若有效点为 0 则报错

```lua
local ok, ratio = vision.CheckMultiColor("screen.png", {
    {x=10,  y=10,  r=255, g=255, b=255},
    {x=100, y=50,  r=0,   g=128, b=0},
}, 25)
if ratio and ratio > 0.8 then ... end
```

### `vision.CaptureClient(title, savePath)`

截取指定窗口的**客户区**（不含标题栏边框）并保存为 PNG。

**返回**：成功 `true`；失败 `false, 错误信息`

### `vision.CaptureScreen(key, {x, y, w, h})`

截屏并缓存到内部位图缓存（**不落盘**）。

**返回**：无（失败静默）

### `vision.SaveBitmap(key, destPath)`

把缓存位图保存为文件。**返回**：无（失败静默）

> `CaptureScreen` / `SaveBitmap` 是历史遗留接口，仅支持整数数组形式与静默失败，
> 新脚本建议直接用 `CaptureClient` 落盘再交给匹配函数。

---

## 5. GoFSM — 有限状态机

```lua
local FSM = require("GoFSM")
local fsm = FSM.new()
```

| 方法 | 参数 | 返回 | 说明 |
| :--- | :--- | :--- | :--- |
| `fsm:addState(name, fn)` | `name` string；`fn` function | 无 | 注册状态及处理函数 |
| `fsm:setInitial(name)` | string | 无 | 设置初始状态 |
| `fsm:step()` | 无 | `字符串` 或 `nil, 错误信息` | 执行一次当前状态，返回**执行后的当前状态名** |
| `fsm:getCurrent()` | 无 | `字符串` | 当前状态名 |

**状态处理函数签名**：

```lua
fsm:addState("STATE_NAME", function(ctx)
    -- ctx 是包含少量数据的只读表
    return "NEXT_STATE"   -- 返回下一个状态名
end)
```

- 返回 `nil` 或空串 → **状态保持不变**
- 返回未注册的状态名 → 下次 `step()` 报错

**`ctx` 的注意点**：
* 每次调用时**新建的只读快照**，只包含 data 中 `string / int / float / bool` 类型的字段
* **在脚本里修改 `ctx` 不会回写**，也不要在其中保存状态；请改用 Lua 的 upvalue（外部局部变量）

```lua
local FSM = require("GoFSM")
local fsm = FSM.new()
local count = 0                      -- 用 upvalue 保存状态

fsm:addState("LOOP", function(ctx)
    count = count + 1
    print("第 " .. count .. " 次")
    if count >= 3 then return "DONE" end
    return nil
end)
fsm:addState("DONE", function(ctx) print("结束"); return nil end)

fsm:setInitial("LOOP")
while fsm:getCurrent() ~= "DONE" do
    fsm:step()
    input.Sleep(100)
end
```

---

## 6. 兼容别名模块

| 模块 | 等价于 | 说明 |
| :--- | :--- | :--- |
| `require("SuKey")` | `require("GoInput")` | 旧版命名 |
| `require("SuScreen")` | `require("GoVision")` | 旧版命名 |

`require("CallGo")` 为独立兼容模块：

| 函数 | 返回 | 说明 |
| :--- | :--- | :--- |
| `CallGo.showalert(title, msg)` | `boolean` | 弹出系统对话框并激活该标题窗口 |
| `CallGo.keyLog()` | 无 | 以 3D 模式启动一个录制器（**会阻塞并占用输入**，仅调试用） |

> 新脚本请统一使用 `GoInput` / `GoVision`。

---

## 7. 按键名称表

键名遵循 **robotgo** 约定（大小写不敏感）。常用：

| 类别 | 名称 |
| :--- | :--- |
| 字母 / 数字 | `"a"`~`"z"`、`"0"`~`"9"` |
| 功能键 | `"f1"`~`"f12"` |
| 编辑键 | `"enter"` `"esc"` `"tab"` `"space"` `"backspace"` `"delete"` `"insert"` `"home"` `"end"` `"pageup"` `"pagedown"` |
| 方向键 | `"up"` `"down"` `"left"` `"right"` |
| 修饰键 | `"ctrl"` `"alt"` `"shift"` `"cmd"`（Windows 键） |
| 锁定键 | `"capslock"` `"numlock"` `"scrolllock"` |
| 符号 | `"."` `","` `"-"` `"="` `"/"` `"\\"` `";"` `"'"` `"["` `"]"` 等 |

```lua
input.KeyTap("e")                    -- 单键
input.KeyTap("s", "ctrl")            -- Ctrl+S
input.KeyTap("z", "ctrl", "shift")   -- Ctrl+Shift+Z
```

---

## 8. 手柄按键掩码表

`input.BTN` 提供全部常量，**可直接相加组合**：

| 按键 | 别名 | 掩码 |
| :--- | :--- | ---: |
| `A` | — | `0x1000` |
| `B` | — | `0x2000` |
| `X` | — | `0x4000` |
| `Y` | — | `0x8000` |
| `LB` | `L1` | `0x0100` |
| `RB` | `R1` | `0x0200` |
| `LS`（左摇杆按下） | `L3` | `0x0040` |
| `RS`（右摇杆按下） | `R3` | `0x0080` |
| `START` | — | `0x0010` |
| `BACK` | — | `0x0020` |
| `UP` / `DOWN` / `LEFT` / `RIGHT`（十字键） | — | `0x0001` / `0x0002` / `0x0004` / `0x0008` |

```lua
input.Gamepad{ buttons = input.BTN.START + input.BTN.A }   -- 同时按 Start 与 A
input.Gamepad{ buttons = 0 }                                -- 全部松开
```

---

## 9. 录制脚本 `.script` 格式

`GoRunner.exe` 无参数启动即可录制，产物为 **JSON Lines**（每行一个动作帧）：

```json
{"dt":3,"op":"init","x":1122,"y":596}
{"dt":1455,"op":"gp","gp_ly":2258}
{"dt":15,"op":"rmv","dx":-3,"dy":1}
```

### 字段说明

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `dt` | int | 距上一帧的毫秒间隔（回放按此节流，受 `-t` 倍率缩放） |
| `op` | string | 动作类型，见下表 |

### `op` 类型

| `op` | 含义 | 相关字段 |
| :--- | :--- | :--- |
| `init` | 起点锚定（把光标归位到录制起点） | `x`,`y` |
| `mv` | 光标移动（绝对定位；**回放时忽略其 `dx/dy`**） | `x`,`y`,`dx`,`dy` |
| `rmv` | 3D/VR 视角的硬件相对位移（Raw Input） | `dx`,`dy` |
| `kd` / `ku` | 键盘按下 / 弹起 | `key` |
| `md` / `mu` | 鼠标按下 / 弹起 | `btn`(`left`/`right`/`center`),`x`,`y`, 识图样本字段 |
| `mw` | 滚轮 | `roll` |
| `gp` | 手柄一帧状态 | `gp_b`,`gp_lt`,`gp_rt`,`gp_lx`,`gp_ly`,`gp_rx`,`gp_ry` |
| `vision` | **识图开关标记行**（单独成行，`dt` 恒为 0） | `von`,`vsl`,`vm`,`vz` |
| `vslot` | **识图预设槽切换标记行** | `vsl`,`vm`,`vz` |

`vision` / `vslot` 是**纯标记行**：回放时不产生任何输入、也不占用时间轴（`dt` 恒为 0），
仅用于后续脚本处理时还原"这一刻识图是开还是关、用的哪个槽"。

### 识图样本字段（`md` / `mu` 帧携带）

录制中按 `Pause` 开启识图后，每次鼠标按下/松开都会截一张以光标为中心的样本图，
并把引用写进该帧：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `vi` | string | 样本名（**不含扩展名**），如 `md-left-001`，实际文件为 `<样本名>.png` |
| `vx` / `vy` | int | 光标在样本图片内的偏移（**样本尺寸恒为 `vz`×`vz`**；贴屏幕边缘时整块区域向内平移，因此偏移 != `vz/2`） |
| `vm` | string | 匹配方式：`image` 模板匹配 / `color` 颜色卷积匹配 |
| `vz` | int | 采样边长：32 / 64 / 128 |

* 样本目录：与脚本同级同名的 `<脚本名>.vision\`。
* 命名规则：`状态+按键+序号`，序号 3 位、各组合独立计数（`md-left-001`、`mu-right-002` …）。
* **每次点击只存一张图**（尺寸 = 当前预设槽）。加速由回放端的识别流水线完成，
  不需要额外多存样本（见下节"识别流水线"）。
* 6 个预设槽：槽位 1~3 = 图像 `32/64/128`，槽位 4~6 = 颜色 `32/64/128`
  （`Home` 短按前进 / `End` 后退，环形切换；识图开启时**长按 `Home`（≥450ms）开关光标绘图框**）。
* 回放时用 `命中坐标 + vx*scale / vy*scale` 还原光标点，见下节"识图对齐"。
* 录制端**按正常方式录制**，不对段终点做任何特殊处理；全部聚合与调整都在回放端内存中完成。

### 识图对齐（回放行为）

带样本的 `md`/`mu` 帧在回放时是**关键点**：

1. 用样本图在当前画面上重新定位（`vm=image` → 模板匹配；`vm=color` → 颜色卷积匹配），
   命中则把该关键点的坐标更新为识别位置；
2. 相邻两个关键点之间的整段 `init`/`mv`/`md`/`mu` 坐标，用一个**相似变换**
   （旋转 + 等比拉伸 + 平移）做对齐 —— 该变换恰好满足
   `录制起点→识别起点`、`录制终点→识别终点`，所以段内路径连续、**不跳变**；
3. 上一轮的结束点即下一轮的起点（相邻段共用同一个识别结果）；
4. 中途出现 `{"op":"vision","von":false}` 则关键点链在此断开：关闭期间的帧
   **按原坐标回放**，重新开启后的起点不沿用上一轮终点。

**只读约定**：回放时整份脚本**一次性读入内存**，聚合与路径调整只作用于内存副本，
**原脚本文件全程只读、不会被改写**（回放前后哈希与修改时间一致，有回归测试锁定）。

#### 识别流水线（只影响"在哪找"）

按成本从低到高、范围从小到大逐级尝试：

1. **同键对并发扫描**：位移 ≤ `-vpair`（默认 32px）时，按下/松开两张图在**同一个区域里同时找**，
   谁先命中就用谁的结果，另一张按录制位移直接换算，另一侧还在跑的扫描立刻作罢；
   没有快速区域时不做这件事，直接回常规路径；
2. **同键对一致性**：同一键对**必须落在同一处** —— 后来那张只在"位移+容差"的极小区里复核，
   极小区没命中就按录制位移与先命中那侧对齐，**绝不扩大到更大的范围另找一个位置**
   （否则一次点击会被拆成一次拖拽）；
3. **快速区域缓存**：关键点识别成功后留下边长 = 样本边长 × `-vcache`（默认 4）的区域，
   后续关键点优先在其中搜索；
4. **区域置信度门控（通用规则）**：受限区域里的最优只是**局部最优**，区域外可能还有更像的目标，
   所以区域结果必须达到 `-vconf`（默认 97%）；达不到就**逐级扩大范围**重找
   （极小区 → 快速区域 → 扩大区域 = `-vcache`×3 → 全屏），全屏是**全局最优**、只要求阈值 `-vs`。
   `-vconf 0` 或负数关闭门控；
5. **后台预取**：预取窗口（`-vfast`，默认 5 个关键点）内的后续关键点由协程提前标出候选，
   全屏识别并发上限 2、抓屏串行化；滑出窗口的在途协程会被取消；
6. **采用前复核**：真正轮到该关键点时用实时画面在候选附近复核一次，通过即采用（加速），
   不通过则重新定位。

#### 铁律：关键点的位置 = 点击的位置

关键点在自己的帧上会被复核、甚至重新定位，因此回放主线遵守三条硬规则：

* 关键点帧的坐标**一律取该关键点最终确认的识别位置**，不经过"可能还是旧值的段变换"；
* 位置一旦变化，引用它的段变换**作废并重建**（含最后一个关键点之后的收尾路径），
  所以不会"点完又跳回去"；
* 关键点在**停顿开始前**先把光标放到当前已知位置（停顿期间就等在正确位置上，
  界面的悬停/焦点状态与录制时一致）；若点击那一刻光标还需要再挪一下（位置刚被复核修正），
  挪过去后等 `-vsettle`（默认 30ms）再按下/松开。

#### 匹配度与段变换的可信度分级

**匹配度（百分比）**：回放识别用 **ZNCC（零均值归一化互相关）**，先各自减掉平均亮度再算相关，
对整体亮度/对比度以及**动态模糊、局部锐化、分辨率差异**都不敏感（比的是结构/特征而非像素值），
结果天然是 `0~100%`，越大越像。实测：完全相同 `100%`；画面 9×9 重度模糊仍有 `98%+`；
无关内容约 `10%`。默认阈值 `-vs 85`。

| 情形 | 行为 |
| :--- | :--- |
| 两端都命中、旋转在 `-vrot` 内，且两端都是**高置信度命中**（≥ `-vconf`） | 旋转 + 拉伸 + 平移，起终点**精确对齐**（位移比例多离谱都照做） |
| 两端都命中、旋转在 `-vrot` 内，但至少一端只是勉强命中（< `-vconf`） | 位移比例落在 `[0.5, 2.0]` 才做旋转/拉伸；越界降级为**纯平移**（取匹配度更高的一端） |
| 两端都命中但旋转超出 `-vrot` | 降级为**纯平移**（取匹配度更高的一端） |
| 两端**录制位置重合**（同一次点击的按下/松开） | 相似变换无定义 => **纯平移** |
| 只有一端命中 | 该端做**纯平移**，路径形状不变 |
| 两端都没命中（含样本缺失） | **按原坐标回放** |

**旋转角（`-vrot`，默认 180 = 不限制）**：内容每轮重排的界面（如舒尔特方格每轮打乱数字）里，
同一段的两个端点回放时可能换到完全不同的方位，旋转 90°/180° 都必须照做，否则端点落不到
识别位置、点击会偏格；固定布局界面把 `-vrot` 收小（如 15）即可挡掉"打到相似元素"造成的路径横拉。
有符号角值域为 `(−180°, +180°]`，所以**限制值 ≥ 180 等于不限制**。

> **为什么位移比例不做门限**：内容每轮重排的界面里，同一段的两端在新一轮可能落在**任意两格** ——
> 录制时相距 300px、这一轮只相距 100px（比例 0.45x）完全正常，这不是"两端矛盾"，
> 而是这一轮的真实布局。拿 `[0.5, 2.0]` 去卡它，会把这类段降级成"纯平移（起点可信）"
> = 按**录制时的旧位置**回放，于是路径朝**下一个点击目标的反方向**走、到点击那一刻才跳回正确位置。
> 现在只有"至少一端是勉强命中"时才用几何比例兜误匹配。相似变换本身也**不做缩放限幅**
> —— 限幅会让端点对不齐（路径少走一截再跳过去）。
>
> **失败只回退它自己**：某个关键点识别失败只影响它自己，其它关键点匹配成功的结果不受牵连、
> 仍作为参考（日志中表现为 `纯平移对齐(终点可信)` 等）；降级原因会打印在段信息之后。

### 手柄帧字段

| 字段 | 类型 | 范围 | 对应 Lua API |
| :--- | :--- | :--- | :--- |
| `gp_b` | int | 按键掩码 | `buttons` |
| `gp_lt` / `gp_rt` | int | 0~255 | `lt` / `rt` |
| `gp_lx` / `gp_ly` | int | -32768~32767 | `lx` / `ly` |
| `gp_rx` / `gp_ry` | int | -32768~32767 | `rx` / `ry` |

### 回放与打包

```powershell
bin\GoRunner.exe script\a.script            # 交互：PgUp 开始/暂停，PgDn 停止
bin\GoRunner.exe -f script\a.script         # 直接播放到底
bin\GoRunner.exe -t 1.5 script\a.script     # 1.5 倍速
bin\GoRunner.exe -vs 85 script\a.script     # 识图匹配度阈值 (百分比，越大越严格，默认 85)
bin\GoRunner.exe -vfast 5 script\a.script   # 预取窗口：提前识别后续几个关键点 (0 关闭预取)
bin\GoRunner.exe -vcache 4 script\a.script  # 快速区域边长 = 样本边长 × 该系数
bin\GoRunner.exe -vtol 6 script\a.script    # 复核位置容差 (像素)
bin\GoRunner.exe -vpair 32 script\a.script  # 同键对复用阈值 (像素)：md/mu 位移不超过它时复用按下那张图的结果
bin\GoRunner.exe -vconf 97 script\a.script  # 受限区域结果的置信度门槛 (默认 97；达不到就扩大范围，0/负数关闭)
bin\GoRunner.exe -vsettle 30 script\a.script # 点击前等待界面看到光标到位 (毫秒，默认 30；仅点击瞬间被修正挪动过才生效，0 关闭)
bin\GoRunner.exe -vblur 3 script\a.script   # 匹配前高斯模糊核 (抗动态模糊/锐化/分辨率差异)
bin\GoRunner.exe -vrot 15 script\a.script   # 允许的最大旋转角 (默认 180 不限制；固定布局界面可收小)
bin\GoRunner.exe -vmin 0.8 -vmax 1.3 a.script  # 缩放搜索范围 (游戏分辨率与录制时不同就放宽)
bin\GoRunner.exe -vdir dir script\a.script  # 手动指定识图样本目录
bin\GoPacker.exe script\a.script            # 打包为单文件 EXE (样本自动内嵌)
```

> **打包产物（单文件 EXE）的命令行参数**：`.script` 负载只认 `-f`（直接播完）、`-t <倍率>`、
> `-vt <百分比>`（识图匹配度阈值，即散装命令行里的 `-vs`）；其余识图参数尚未透传。
> `.lua` 负载不接受参数。调试期想随便调参数，用 `GoPacker.exe -unpak` 产出明文目录后
> 直接跑 `GoRunner.exe <脚本>.script`。

---

## 10. 返回值与错误约定

| 模式 | 表现 | 例子 |
| :--- | :--- | :--- |
| **正常** | 返回业务值（`table` / `boolean` / 多个数值），**不返回错误槽** | `MatchTemplate` 命中、`ClickClient` 成功 |
| **失败** | 返回 `nil`（或 `false`）**并在第 2 个返回值给出错误字符串** | `MatchTemplate` 失败 → `nil, "..."` |
| **无返回（0 个值）** | 直接执行，连 `nil` 都不返回 | `Move` / `Click` / `Sleep` / `KeyTap` / `TypeStr` / `Sound*` / `CaptureScreen` / `SaveBitmap` / `GamepadClose` |

标准处理写法：

```lua
local res, err = vision.MatchTemplate(...)
if not res then
    print("匹配失败: " .. tostring(err))
    return
end
```

> ⚠️ **0 返回值函数不能放进需要值的表达式**：
> ```lua
> local x = input.Move(0, 0)          -- 合法，x = nil
> print(tostring(input.Move(0,0)))    -- 报错: bad argument #1 to tostring (value expected)
> ```
> 想知道「有没有执行成功」只能看 `Move`/`Click` 这类接口——它们**不给反馈**；
> 需要确认结果时请用带返回值的形式（如 `ClickClient` 返回 `bool`）。

---

## 11. 完整示例

### 示例 A：带热键控制的手柄推杆循环

```lua
local pad = require("GoInput")

local HOLD_MS, TICK_MS = 4000, 20
local phase, elapsed = "UP", 0
local running, paused = false, false

print("按 [PgUp] 开始/暂停，按 [PgDn] 结束")

while true do
    if pad.CheckPgDnTrigger() then
        pad.GamepadReset()
        pad.SoundStop()
        break
    end

    if pad.CheckPgUpTrigger() then
        if not running then
            running, paused = true, false
            pad.SoundStart()
        elseif not paused then
            paused = true
            pad.GamepadReset()      -- 暂停时务必回中，否则摇杆会一直偏着
            pad.SoundPause()
        else
            paused = false
            pad.SoundResume()
        end
    end

    if running and not paused then
        pad.Gamepad{ lx = 0, ly = phase == "UP" and 32767 or -32768 }
        elapsed = elapsed + TICK_MS
        if elapsed >= HOLD_MS then
            elapsed = 0
            phase = (phase == "UP") and "DOWN" or "UP"
        end
    end

    pad.Sleep(TICK_MS)
end

pad.GamepadClose()
```

### 示例 B：视觉驱动的点击

```lua
local input  = require("GoInput")
local vision = require("GoVision")

local TITLE, SHOT = "MyGame", "shot.png"

local function clickImage(tpl, minPercent)
    if not vision.CaptureClient(TITLE, SHOT) then return false end
    local r = vision.MatchTemplate(SHOT, AssetPath(tpl), 0.9, 1.1, 0.05)
    if r and r.percent >= (minPercent or 85) then   -- percent = 匹配度百分比 (0~100)
        input.Move(r.centerX, r.centerY)
        input.Click()
        return true
    end
    return false
end

if clickImage("btn_start.png") then
    print("已点击开始按钮")
end
```

---

## 12. 常见陷阱

| 陷阱 | 说明与对策 |
| :--- | :--- |
| **热键检测不生效** | `CheckPgUpTrigger` 是**上升沿**且必须在循环里高频轮询（建议 20 ms）。不要在循环里长时间 `Sleep`。 |
| **摇杆一直偏着** | 摇杆/扳机是**状态**。暂停、出错、退出前都要 `GamepadReset()`。 |
| **游戏不响应手柄** | 虚拟手柄占用最小空闲槽位。物理手柄占了 0 时虚拟手柄会落到 1，只读槽位 0 的游戏会忽略它 → 回放时暂时拔掉物理手柄；用 `GamepadSlots()` 确认（返回如 `{0,1}`）。 |
| **`score` 越大越好？** | 相反。Lua 侧用的是归一化平方差，**`score` 越接近 0 越匹配**（0 = 完全一致）；想用"越大越像"的口径就用同一个结果里的 **`percent`（0~100）**，它和回放端识图阈值 `-vs` 是同一套百分比。 |
| **`AssetPath` 返回的路径打不开** | 它只做字符串拼接，不检查存在性。程序无 `.pak` 资源时 `Asset/` 不会存在；打包时 `Asset/` 为空目录也**不会**生成资源包。 |
| **`tostring(input.Move(...))` 报 value expected** | 这类接口返回 **0 个值**（不是 `nil`）。不要放进表达式里当参数用。 |
| **改 `ctx` 无效** | GoFSM 的 `ctx` 是只读快照。状态请用 Lua upvalue 保存。 |
| **打包后 Lua 报 `syntax error`** | 用 `GoPacker` 打包了 `.script`（JSON 数据）却又按 Lua 执行。现版本已按扩展名自动分流，请确认使用最新 `GoPacker.exe`。 |
| **打包产物文件被占用** | 产物正在运行时无法覆盖同名文件，先关闭再打包。 |

---

## 相关文档

- [`LUA_API_AI.md`](./LUA_API_AI.md) — 面向 AI 的紧凑接口清单
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — 项目架构与打包机制
- [`../README.md`](../README.md) — 安装、录制、打包快速上手
