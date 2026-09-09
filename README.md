# GoRobotScript

基于 Go 语言（Go 1.25+）开发的轻量级 Windows 自动化框架与机器人脚本执行工具，全面支持键盘、鼠标（常规桌面 UI 与 3D/VR 游戏视角）、手柄状态的精准流式录制、回放执行与 Lua 脚本编写。

---

## 核心组件与工具说明

通过一键编译后，所有核心可执行程序均输出至 `bin/` 目录：

| 工具名称 | 说明与功能定位 |
| :--- | :--- |
| **GoRunner.exe** | **统一脚本执行与录制工具**。无参数直接双击进入智能录制模式；支持带参数回放 `.script` 录制脚本或 `.lua` 自动化脚本。同时兼容旧版 `GokeyLua.exe` 与 `GokeyRun.exe`。 |
| **PackLua.exe** | **脚本与资源打包工具**。可将 `.lua` 自动化脚本与其关联的资源目录封装打包为单文件独立执行程序。 |
| **SingleLoader.exe** | **单文件引导器模板**。配合打包器生成便携式单文件应用。 |

---

## 快速上手与使用方法

### 1. 操作录制（双击即用）

1. 直接**双击打开 `bin/GoRunner.exe`**，程序进入录制待机状态。
2. 切换至目标游戏或应用窗口。
3. **全局热键控制与音频提示**：
   * **首次按 `PageUp`**：开始录制（发出双声上扬提示音）。
   * **再次按 `PageUp`**：暂停 / 继续 录制切换（暂停发出双低音，继续发出单高音）。
   * **按 `PageDown`**：结束录制并自动保存脚本（发出双声下降提示音）。
4. 录制生成的 `.script` 文件将自动保存在 `bin/script/` 目录下，并以时间戳命名（如 `VRView3D_2026-09-10_01-00-22.script`）。

*也可以通过命令行指定输出路径启动录制：*
```powershell
bin\GoRunner.exe record
# 或指定保存文件路径
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

*若无参数直接双击 `bin/GokeyLua.exe`，默认自动执行同目录下的 `main.lua`。*

#### 常用 Lua 内置模块：
* **`GoInput` / `SuKey`**：键盘按键、按键连击、鼠标瞬移/平滑移动、滚轮、窗口前后台操作。
* **`GoVision` / `SuScreen`**：屏幕截图、模板多尺度搜索匹配、颜色卷积匹配、多点比色。
* **`GoFSM`**：有限状态机调度驱动，适用于复杂流程自动化。

---

### 4. 脚本与资源打包 (PackLua)

使用 `PackLua.exe` 可以将 Lua 脚本及其资源目录一键打包为独立的可执行文件：

```powershell
# 将 demo.lua 与同目录的 Asset 资源打包为 demo_app.exe
bin\PackLua.exe -script demo.lua -asset Asset -out demo_app.exe
```

---

## 编译与构建说明

本项目采用标准的 PowerShell / Bat 一键构建脚本，支持依赖库自动还原与编译：

```powershell
# 在项目根目录下执行
.\build.ps1
# 或直接双击运行 build.bat
```

构建脚本会自动将 `Core/libs` 中的 15 个必要运行时动态库（OpenCV 等）同步还原至 `bin/` 目录，并编译生成全部核心二进制工具。

---

## 依赖说明

* [robotn/gohook](https://github.com/robotn/gohook)：系统底层全局键鼠事件监听。
* [go-vgo/robotgo](https://github.com/go-vgo/robotgo)：跨平台键鼠模拟输入。
* [gopher-lua](https://github.com/yuin/gopher-lua)：Go 原生 Lua 虚拟机与编译器。
* [GoCV](https://gocv.io/)：OpenCV 4.13+ 计算机视觉与模板匹配。

---

## 开源协议与作者

* **License**：MIT
* **Author**：Suceru
