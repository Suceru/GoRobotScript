# GoRobotScript 架构与 AI Agent 协作规范

本文档为 AI Agent 以及开发者提供 `GoRobotScript` 模块架构、代码规范及调用方式的标准化描述。

---

## 1. 核心目录与职责结构 (Directory Layout)

所有核心模块与可执行工具入口均统一归整在 `Core/` 文件夹内，根目录下不再保留 `cmd/`：

```
GoRobotScript/
├── Core/                      # ★ 全部板块、核心基础模块与工具入口统统在此
│   ├── GoVision/              # 视觉核心基础库 (OpenCV 模板多尺度匹配、颜色网格匹配、多点比色、区域抓屏 capture.go、ROI/多图匹配 roi.go)
│   ├── GoInput/               # 键鼠/手柄/窗口交互库 (按键、移动、后台消息点击、操作录制、虚拟 Xbox 手柄、Raw Input、识图采样 vision.go)
│   ├── GoFSM/                 # 独立有限状态机库 (Go 原生与 Lua 联动的状态机驱动)
│   ├── GoPak/                 # 资源包管理库 (/Asset 目录压缩封装为 .pak、解密与解压加载)
│   ├── GoLua/                 # 【板块1】Lua 脚本引擎模块
│   │   ├── engine.go          # Gopher-Lua 虚拟机封装、Go 前缀模块预加载、/Asset 自动加载
│   │   └── LuaCLI/            # → bin/GoLua.apppak (Lua 运行时载荷，-unpak 时复制改名为可执行程序)
│   ├── GoRunner/              # 【板块2】录制与回放执行器模块
│   │   ├── runner.go          # 执行调度核心库 (.script 回放 / .lua 执行 / .pak 装配)
│   │   ├── vision_align.go    # 识图关键点对齐 (相似变换路径旋转/拉伸、降级策略)
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
- **识图采样（录制期，`GoInput/vision.go`）**：
  - `Pause` 切换识图开关，`Home`/`End` 在 6 个预设槽间环形切换
    （槽位 1~3 = 图像 32/64/128，槽位 4~6 = 颜色 32/64/128）。
  - **长按 `Home`（≥450ms）开关光标绘图框**（`GoInput/vision_box_windows.go`）：
    一个置顶 `WS_POPUP` 窗口，用 `SetWindowRgn` 把窗口区域挖成"只有边框"的空心方框，
    位置随光标 `SetWindowPos` 平移；因为不是每帧重绘屏幕、位置不变时完全不触碰窗口，
    开销与拖动文件时的虚影框同级。
  - **边框画在采样区域之外**：`visionBoxWindowRect` 把采样区域四周各外扩一条边框宽度，
    挖空后的内圈正好等于采样区域本身 —— 因此红框绝不会被截进样本图（模板零污染），
    而用户看到的空心内圈就是实际采样范围。窗口尺寸 = 采样尺寸 + 2×线宽（38/70/134）。
  - 样式为 `WS_EX_TOPMOST|TOOLWINDOW|TRANSPARENT|NOACTIVATE`，`WM_NCHITTEST` 返回
    `HTTRANSPARENT`，且 `WM_SETCURSOR` 直接返回 TRUE 而不调用 `SetCursor` ——
    因此**点击穿透、永不抢焦点、不占 Alt+Tab、不改动鼠标指针形状**
    （否则窗口在光标下方移动时会被系统重置成默认箭头，表现为指针反复闪烁）。
  - 短按 `Home` 切槽、长按开关框，靠按下时长区分（切槽动作改到松开时判定）。
    识图关闭、`PgDn` 结束时自动收起并销毁窗口。
  - 识图开启时，每次鼠标左键/右键**按下与松开**都会以光标为中心截取一张样本图，
    命名 `状态+按键+3位序号`（`md-left-001`），存入 `<脚本名>.vision/`；截图在独立线程异步落盘，不阻塞 60FPS 录制主循环。
  - **样本尺寸恒定**：`GoVision.PatchGeometry` 保证样本永远是 `size×size`，
    光标贴屏幕边缘时把整块区域向内平移而不是裁小（`iox/ioy` 随之为非居中偏移），
    使样本尺寸、颜色网格描述与匹配尺度在所有位置保持一致。
  - 状态行以固定显示宽度 **`\r` 就地覆盖刷新**，不换行、不撑长控制台。
  - 开/关与槽位切换各写入**一行独立标记**（`op=vision` / `op=vslot`，`dt` 恒为 0 且不推进时间轴）。
  - 预设**不跨轮保留**：`PgDn` 结束录制即退出识图并清零。
- **识图对齐（回放期，`GoRunner/vision_align.go`）**：
  - 预扫描全部帧 → 依据标记行切分关键点链 → 相邻关键点间构成一段待对齐路径。
  - 每个 `md`/`mu` 关键点用样本图在实时画面重新定位，得到 `命中点 + iox*scale` 的光标坐标。
  - 段内路径用**相似变换**（旋转 + 等比拉伸 + 平移）对齐：该变换由两组对应点唯一确定
    （恰好用满 4 个自由度），因此 `录制起点→识别起点`、`录制终点→识别终点` 严格成立，段内**不跳变**。
  - 段变换的可信度分级（宁可不对齐也不乱跳），全部在 `activateSeg` 里判定：
    | 情形 | 行为 |
    | :--- | :--- |
    | 两端命中、旋转在 `-vrot` 内，且两端都是**高置信度命中**（≥ `-vconf`） | 旋转 + 拉伸 + 平移，起终点**精确对齐** |
    | 两端命中、旋转在 `-vrot` 内，但至少一端勉强命中（< `-vconf`） | 位移比例在 `[0.5, 2.0]` 内才做旋转/拉伸，越界降级为纯平移 |
    | 两端命中但旋转超出 `-vrot` | 纯平移（取匹配度更高的一端） |
    | 两端录制位置重合（同一次点击的按下/松开） | 相似变换无定义 => 纯平移 |
    | 单端命中 | 该端纯平移，路径形状不变 |
    | 两端都没命中 | 按原坐标回放 |
  - **位移比例不做门限**：内容每轮重排的界面里，同一段两端在新一轮可能落在任意两格
    （录制相距 300px、这一轮只相距 100px 即 0.45x 是真实布局，不是"两端矛盾"）。
    拿几何比例卡它会退化成"纯平移(起点可信)" = 按录制旧坐标回放，于是路径朝**下一个点击
    目标的反方向**走、到点击那一刻才跳回正确位置。`[0.5, 2.0]` 只在至少一端勉强命中时
    作为误匹配安全网；相似变换**不做缩放限幅**（限幅会让端点对不齐，路径少走一截再跳过去）。
  - **只读约定**：脚本以只读方式打开并整份读入内存，聚合与路径调整只作用于内存副本，
    原脚本文件全程不被改写（有回归测试锁定哈希与修改时间）。
  - **识别流水线**（`GoRunner/vision_pipeline.go`）按成本从低到高、范围从小到大逐级尝试：
    ① **同键对并发扫描**：鼠标按下/松开位移 ≤ `-vpair`（默认 32px）时，两张图在同一个快速区域内
       并发搜索，谁先命中用谁的结果、另一侧立刻作罢（避免被慢的那侧拖住）；无快速区域则不做；
    ② **同键对一致性**：同一键对（位移 ≤ `-vpair`）必须落在同一处：后来那张只在"位移+容差"的
       极小区里复核，极小区没命中就按录制位移与先命中那侧对齐，绝不到更大范围另找位置
       （否则一次点击会被拆成一次拖拽）；
    ③ **快速区域缓存**：关键点识别成功后留下边长 = 样本边长 × `-vcache`（默认 4）的区域，
       后续关键点优先在其中搜索；
    ④ **区域置信度门控**：受限区域（极小区 / 快速区域 / 扩大区域）里的最优只是**局部最优**，
       区域外的相似目标可能更优，因此区域结果必须达到 `-vconf`（默认 97%），
       达不到就逐级扩大范围（扩大区域 = `-vcache`×3），最终落到全屏的**全局最优**（只要求 `-vs`）。
       该规则与界面布局无关，是流水线的通用正确性约束；
    ⑤ **后台预取**：预取窗口（默认 5 个关键点）内的后续关键点由协程提前标出候选，
       全屏识别并发上限 2、抓屏串行化；
    ⑥ **采用前复核**：真正轮到该关键点时用实时画面复核候选，通过即采用（加速），
       不通过回退标准识别；
    ⑦ 滑出窗口的在途预取协程被取消。
    现场实测（2560×1440，9 组点击 = 18 个关键点）：全屏标准识别 2 次，同键对复用 1 次，
    受限区域命中 15/26 次（置信度不足扩大范围 4 次），后台预取 16 次（命中 16），
    采用前复核 33 次（通过 33 / 重新定位 0）。
  - **关键点位置 = 点击位置**（回放主线，`runner.go`）：关键点在自己的帧上会被复核甚至重新定位，
    因此 ① 关键点帧坐标一律取该关键点**最终确认的识别位置**，不经过"可能仍持旧值的段变换"；
    ② 位置一旦变化，`invalidateSegsUsing` 作废引用它的段变换并在下次使用时重建
    （含最后一个关键点之后的收尾路径），避免"点完又跳回旧位置"；
    ③ 关键点在**停顿开始前**就先把光标放到当前已知位置，若点击那一刻光标还需要再挪一下
    （位置刚被修正），挪动后等 `-vsettle`（默认 30ms）再按下/松开，让界面先看到光标到位。
  - 匹配度标定：回放识别统一用 **ZNCC + 轻度预模糊**，结果是 `0~100%` 相似度。
    实测完全相同 `100%`、5×5 模糊 `99.1~99.8%`、9×9 重度模糊 `98.2~99.7%`、局部锐化 `99.9%`、
    无关内容约 `10%`；默认阈值 `-vs 85`，区域门槛 `-vconf 97`（`0` 关闭）。
  - 结果矩阵极值定位使用 OpenCV 原生 `MinMaxLoc`（逐像素 cgo 遍历曾是秒级瓶颈）；
    统计量用原子计数（预取协程并发写入，`go test -race` 全绿）。
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
  | `.script` | `script` | 回放引擎 (`GoRunner`) | 录制的键鼠/手柄动作数据 (JSON Lines)；`<脚本名>.vision/` 识图样本自动内嵌 |

- **负载包格式**：
  - 当前尾标 `GOKEYLUA_EMBEDDED_PAYLOAD_V3` → `[名称][版本][类型][脚本长度][脚本][资源长度][资源(zip)]`
  - 兼容 `..._V2` → `[名称][版本][类型][脚本]`（脚本为剩余全部字节，无资源段）
  - 兼容 `..._V1` → `[名称][版本][脚本]`（无类型字段，恒按 Lua 处理）
  - 三种尾标**等长**，读取端统一按尾标魔数区分。
- **三种产物形态**：
  1. **纯单文件 EXE 模式（默认）**：
     - 将脚本、`/Asset` 打包后的 `.pak` 资源包、识图样本 zip、16 个 OpenCV/MinGW/ViGEm 依赖 DLL 全部压缩打包进一个 EXE。
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
       - `.script` → `GoRunner.exe`（回放引擎本体）+ 明文 `.script` + `<脚本名>.vision/`（识图样本）+ DLL
         → 运行方式：`GoRunner.exe <脚本>.script`
     - 两种情况都**改完脚本无需重新打包**。
- **`GoLua.apppak` 的由来**：`Core/GoLua` 的产物在 bin 中用 `.apppak` 后缀存放，既区别于工具类 exe，也避免与资源包 `.pak` 混淆；`-unpak` 时复制到调试目录并改名为可执行程序。
- **空资源约定**：`Asset/` 为空目录（或仅含空子目录）时视为无资源，不生成 `.pak`。
- **`.pak` 识别**：加载器只把文件头为 `PK`（zip）的 `.pak` 当作资源包，非 zip 的同名文件一律跳过。
- **运行时缓存布局（基础模块与定制内容分离）**：
  ```
  %LOCALAPPDATA%\GoRobotScript\
  ├── base\b_<基础模块版本>_<DLL内容哈希>\   # 全部依赖 DLL，同基线各包共享一份
  ├── runtime_<包内容哈希>\                  # runner.exe（含焊入脚本）与 assets.pak
  └── vision_<样本内容哈希>\                 # 录制脚本内嵌的识图样本（含 .ready 标记）
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
