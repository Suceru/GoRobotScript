// Package GoRunner provides execution facilities for recorded scripts (.script) and Lua automation scripts.
package GoRunner

import (
	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoLua"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

// Runner executes recorded scripts or Lua scripts with associated assets.
type Runner struct {
	BaseDir string
}

func NewRunner(baseDir string) *Runner {
	if baseDir == "" {
		exe, err := os.Executable()
		if err == nil {
			baseDir = filepath.Dir(exe)
		} else {
			baseDir, _ = os.Getwd()
		}
	}
	return &Runner{BaseDir: baseDir}
}

// RunLuaScript executes a Lua script and ensures .pak assets in its directory or /Asset are loaded.
func (r *Runner) RunLuaScript(luaPath string) error {
	dir := filepath.Dir(luaPath)
	env := GoLua.NewEnvironment(dir)
	defer env.Close()

	if err := env.LoadAssets(); err != nil {
		return fmt.Errorf("failed to load assets: %w", err)
	}

	return env.ExecuteFile(luaPath)
}

func (r *Runner) RunScriptFileWithOptions(filePath string, opts PlayOptions) error {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".script":
		return r.RunRecordScriptWithOptions(filePath, opts)
	case ".lua":
		return r.RunLuaScript(filePath)
	default:
		return r.RunLuaScript(filePath)
	}
}

// PlayOptions 回放控制选项
type PlayOptions struct {
	FastMode   bool    // -f 参数: 直接从头执行到尾，不等待按键
	SpeedScale float64 // -t 参数: 时间缩放倍率 (例如 1.2 加速 20%, 0.5 减速 50%, 默认 1.0)
}

// RunScriptFile runs either a .script (JSON Lines) or a .lua file with default options.
func (r *Runner) RunScriptFile(filePath string) error {
	return r.RunScriptFileWithOptions(filePath, PlayOptions{FastMode: false, SpeedScale: 1.0})
}

// RunRecordScriptWithOptions 执行 .script 回放
// - 默认模式: 按下 PgUp 开始播放，再次按 PgUp 暂停/继续，PgDn 强制停止退出。
// - FastMode (-f): 直接从头播放到结束，不需要人工按键交互。
// - SpeedScale (-t): 调整时间轴速率 (实际间隔 = dt / SpeedScale)。
func (r *Runner) RunRecordScriptWithOptions(scriptPath string, opts PlayOptions) error {
	file, err := os.Open(scriptPath)
	if err != nil {
		return fmt.Errorf("无法打开脚本文件: %w", err)
	}
	defer file.Close()

	if opts.SpeedScale <= 0 {
		opts.SpeedScale = 1.0
	}

	// 读取所有动作帧到内存，确保快速回放不被磁盘 I/O 阻塞
	var rawLines [][]byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) > 0 {
			lineCopy := make([]byte, len(b))
			copy(lineCopy, b)
			rawLines = append(rawLines, lineCopy)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	var playingFlag int32
	var pauseFlag int32
	var stopFlag int32

	if opts.FastMode {
		// -f 模式：直接启动播放
		atomic.StoreInt32(&playingFlag, 1)
		GoInput.SoundStart()
		fmt.Println(">>> [GoRunner FastMode (-f)] 直接开始播放...")
	} else {
		// 默认模式：等待用户按下 PgUp 开始播放
		fmt.Println("==========================================================")
		fmt.Println(" [GoRunner 交互回放模式]")
		fmt.Printf(" 速率倍率 (-t): %.2fx\n", opts.SpeedScale)
		fmt.Println(" 控制快捷键与系统音频提示:")
		fmt.Println("   • [PgUp] 首次按:  开始播放 (双声上扬提示音)")
		fmt.Println("   • [PgUp] 再次按:  暂停 / 继续 播放 (暂停双低音 / 继续单高音)")
		fmt.Println("   • [PgDn] 按一下:  停止播放并退出 (两声下降提示音)")
		fmt.Println("==========================================================")
		fmt.Println("请切换至目标窗口，按下 [PgUp] 开始播放...")

		// 挂载操作系统级全局系统热键 (RegisterHotKey)，跨窗口与全屏游戏 100% 捕获
		GoInput.StartPlaybackHotkeyListener(&playingFlag, &pauseFlag, &stopFlag, GoInput.PlaybackHotkeys{
			OnStart: func() {
				fmt.Println("\n>>> [全局热键: 开始播放] 正在执行脚本...")
			},
			OnPause: func() {
				fmt.Println("\n||| [全局热键: 暂停播放] (按 PgUp 继续，按 PgDn 停止)")
			},
			OnResume: func() {
				fmt.Println("\n>>> [全局热键: 继续播放] 恢复执行...")
			},
			OnStop: func() {
				fmt.Println("\n■■■ [全局热键: 停止播放] 正在退出...")
			},
		})

		// 等待启动或退出
		for atomic.LoadInt32(&playingFlag) == 0 && atomic.LoadInt32(&stopFlag) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// 播放主循环
	var lastTime time.Time
	for _, line := range rawLines {
		if atomic.LoadInt32(&stopFlag) == 1 {
			break
		}

		// 暂停等待
		for atomic.LoadInt32(&pauseFlag) == 1 && atomic.LoadInt32(&stopFlag) == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if atomic.LoadInt32(&stopFlag) == 1 {
			break
		}

		// 1. 新版 RecordAction 格式
		var action GoInput.RecordAction
		if err := json.Unmarshal(line, &action); err == nil && action.Op != "" {
			if action.Dt > 0 {
				// 按照 -t 指定的速率缩放时间间隔
				adjustedDt := float64(action.Dt) / opts.SpeedScale
				time.Sleep(time.Duration(adjustedDt) * time.Millisecond)
			}
			replayAction(&action)
			continue
		}

		// 2. 兼容旧版原语事件 hook.Event
		var ev hook.Event
		if err := json.Unmarshal(line, &ev); err == nil {
			if lastTime.IsZero() {
				lastTime = ev.When
			} else {
				delay := ev.When.Sub(lastTime)
				if delay > 0 {
					adjustedDelay := time.Duration(float64(delay) / opts.SpeedScale)
					time.Sleep(adjustedDelay)
				}
				lastTime = ev.When
			}
			replayEvent(&ev)
		}
	}

	// 播放结束时，若使用了虚拟手柄，安全归中并释放
	vg, _ := GoInput.GetOrInitVirtualGamepad()
	if vg != nil {
		vg.SendReport(0, 0, 0, 0, 0, 0, 0)
	}

	GoInput.SoundStop()
	fmt.Println("\n[SUCCESS] 脚本播放完毕！")
	return nil
}

// RunRecordScript replays a recorded JSON Lines .script file with default options.
func (r *Runner) RunRecordScript(scriptPath string) error {
	return r.RunRecordScriptWithOptions(scriptPath, PlayOptions{FastMode: false, SpeedScale: 1.0})
}

// replayActionDeprecated (已废弃旧版：保留作为历史参考)
func replayActionDeprecated(a *GoInput.RecordAction) {
	replayAction(a)
}

// replayAction 执行动作回放
// 1. rmv (Raw Input 原始硬件位移)：1:1 原生直接注入，无任何加速或缩放失真，完美还原 3D 视角旋转
// 2. mv (桌面/UI 绝对光标位移)：执行 FastSetCursorPos(x, y) 保证点对点绝对精确
func replayAction(a *GoInput.RecordAction) {
	switch a.Op {
	case "init":
		// 第一个鼠标位置：瞬间归位/跳转到录制时的初始绝对坐标，对齐起点
		if a.X != 0 || a.Y != 0 {
			GoInput.FastSetCursorPos(a.X, a.Y)
		}
	case "kd":
		if a.Key != "" {
			robotgo.KeyToggle(a.Key, "down")
		}
	case "ku":
		if a.Key != "" {
			robotgo.KeyToggle(a.Key, "up")
		}
	case "mv":
		// 按照设计：凡是带有绝对坐标 (x, y) 的 mv 动作，彻底舍弃 dx/dy 相对偏移，
		// 只回放鼠标绝对坐标 FastSetCursorPos(x, y)，保证 2D UI 界面点对点精准对齐，
		// 从根本上杜绝 3D->2D 切换（如按 E 开关背包）时由于光标瞬间重置复位导致的 3D 视角暴甩！
		if a.X != 0 || a.Y != 0 {
			GoInput.FastSetCursorPos(a.X, a.Y)
		}
	case "rmv":
		// 3D/VR 视角模式：底层硬件 Raw Input 物理相对位移 (dx, dy)，1:1 纯物理脉冲精准转动镜头视角
		if a.Dx != 0 || a.Dy != 0 {
			GoInput.SendRelativeMouseMove(int32(a.Dx), int32(a.Dy))
		}
	case "md":
		// 鼠标按下：确保坐标同步，并直接注入 Windows 物理按键信号，解决拖拽丢失按键问题
		if a.X != 0 || a.Y != 0 {
			GoInput.FastSetCursorPos(a.X, a.Y)
		}
		GoInput.FastMouseDown(a.Btn)
	case "mu":
		// 鼠标松开：确保坐标同步并释放
		if a.X != 0 || a.Y != 0 {
			GoInput.FastSetCursorPos(a.X, a.Y)
		}
		GoInput.FastMouseUp(a.Btn)
	case "mw":
		robotgo.Scroll(a.X, a.Y)
	case "gp":
		// 手柄动作帧：包含完整的摇杆坐标 (lx, ly, rx, ry)、扳机 (lt, rt) 与按键掩码
		// 通过虚拟手柄总线驱动 (ViGEmBus) 1:1 精确回放手柄物理状态
		vg, err := GoInput.GetOrInitVirtualGamepad()
		if err == nil && vg != nil {
			vg.SendReport(a.GpBtns, a.GpLT, a.GpRT, a.GpLX, a.GpLY, a.GpRX, a.GpRY)
		}
	}
}

func replayEvent(e *hook.Event) {
	switch e.Kind {
	case hook.KeyUp:
		key := hook.RawcodetoKeychar(e.Rawcode)
		if key == "" && e.Keychar != 0 && e.Keychar != 65535 {
			key = string(rune(e.Keychar))
		}
		if key != "" {
			robotgo.KeyToggle(key, "up")
		}
	case hook.KeyDown:
		key := hook.RawcodetoKeychar(e.Rawcode)
		if key == "" && e.Keychar != 0 && e.Keychar != 65535 {
			key = string(rune(e.Keychar))
		}
		if key != "" {
			robotgo.KeyToggle(key, "down")
		}
	case hook.KeyHold:
		key := hook.RawcodetoKeychar(e.Rawcode)
		if key == "" && e.Keychar != 0 && e.Keychar != 65535 {
			key = string(rune(e.Keychar))
		}
		if key != "" {
			robotgo.KeyToggle(key, "down")
		}
	case hook.MouseUp:
		btn := "left"
		if e.Button == hook.MouseMap["right"] {
			btn = "right"
		} else if e.Button == hook.MouseMap["center"] {
			btn = "center"
		}
		robotgo.MouseUp(btn)
	case hook.MouseDown:
		btn := "left"
		if e.Button == hook.MouseMap["right"] {
			btn = "right"
		} else if e.Button == hook.MouseMap["center"] {
			btn = "center"
		}
		robotgo.MouseDown(btn)
	case hook.MouseMove:
		robotgo.Move(int(e.X), int(e.Y))
	case hook.MouseDrag:
		robotgo.Drag(int(e.X), int(e.Y))
	case hook.MouseWheel:
		robotgo.Scroll(int(e.X), int(e.Y))
	}
}
