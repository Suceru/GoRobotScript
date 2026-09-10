// Package GoRunner provides execution facilities for recorded scripts (.script) and Lua automation scripts.
package GoRunner

import (
	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoLua"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	// 识图对齐 (vision) 选项。脚本中含识图样本时，鼠标按下/松开的关键点会用
	// 样本图重新定位，并把关键点之间的路径旋转/拉伸对齐到识别结果上。
	VisionDir        string  // 样本目录；留空时按 <脚本名>.vision 自动推导
	VisionSimilarity float64 // 匹配度阈值 (百分比 0~100，越大越严格)，默认 DefaultVisionSimilarity (85)
	VisionBlur       int     // 匹配前高斯模糊核 (奇数)，默认 DefaultVisionBlur (3)；0 关闭
	VisionMinScale   float64 // 匹配缩放下限，默认 0.9
	VisionMaxScale   float64 // 匹配缩放上限，默认 1.1
	VisionScaleStep  float64 // 匹配缩放步长，默认 0.05

	// 识别流水线（读入内存聚合 + 快速区域缓存 + 后台预取 + 采用前复核）
	VisionLookahead   int     // 预取窗口：提前识别后续多少个关键点，默认 DefaultVisionLookahead (5)
	VisionCacheFactor int     // 快速区域边长 = 样本边长 × 该系数，默认 DefaultVisionCacheFactor (4)
	VisionFastTol     int     // 复核位置容差 (像素)，默认 DefaultVisionFastTol (6)
	VisionMaxRotation float64 // 允许的最大旋转角 (度)，默认 DefaultVisionMaxRotation (15)；超出即放弃旋转/拉伸
	VisionPairTol     int     // 同键对复用阈值 (像素)，默认 DefaultVisionPairTol (32)
	// md/mu 位移不超过它时，松开那张图直接复用按下那张图的识别结果

	// VisionRegionConfirm 受限区域结果的置信度门槛 (百分比 0~100，默认 DefaultVisionRegionConfirm 97)。
	//
	// 缩小的搜索范围里找到的只是"局部最优"，区域外可能还有更像的目标；
	// 达不到这个门槛就扩大搜索范围，直到全屏（全屏是全局最优，只要求 VisionSimilarity）。
	// 传 0 表示使用默认值；传负数表示关闭门控（区域结果只要过阈值就采纳）。
	VisionRegionConfirm float64

	// VisionSettleMs 点击/松开前，若光标刚在**这一瞬间**被复核修正挪动过，
	// 先等待这么多毫秒再按下/松开（默认 DefaultVisionSettleMs 30；0 关闭）。
	//
	// 目的：有些界面要"先看到光标到位（悬停高亮/焦点切换）"才认这次点击，
	// 否则瞬移过去立刻按下，点击仍会被算在旧位置上（表现为"先点了再移动"）。
	// 停顿期间光标已经等在正确位置上的正常情形不会付这个等待。
	VisionSettleMs int
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

	// 识图样本目录默认与脚本同级同名 (<脚本名>.vision)
	if opts.VisionDir == "" {
		opts.VisionDir = GoInput.VisionAssetDir(scriptPath)
	}

	// 读取所有动作帧到内存，确保快速回放不被磁盘 I/O 阻塞
	rawLines, err := scanActionLines(file)
	if err != nil {
		return err
	}
	return r.runRecordLines(rawLines, opts)
}

// RunRecordFromBytes 直接回放内存中的录制脚本内容（供打包产物内嵌脚本使用，无需落地临时文件）
func (r *Runner) RunRecordFromBytes(data []byte, opts PlayOptions) error {
	rawLines, err := scanActionLines(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return r.runRecordLines(rawLines, opts)
}

// scanActionLines 逐行读取动作帧（跳过空行）
func scanActionLines(rd io.Reader) ([][]byte, error) {
	var rawLines [][]byte
	scanner := bufio.NewScanner(rd)
	// 单行可能较长（含手柄轴向等字段），放宽缓冲区上限
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) > 0 {
			lineCopy := make([]byte, len(b))
			copy(lineCopy, b)
			rawLines = append(rawLines, lineCopy)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rawLines, nil
}

// playFrame 预解析后的一帧：新版 RecordAction 或旧版 hook.Event
type playFrame struct {
	action *GoInput.RecordAction
	event  *hook.Event
}

// parsePlayFrames 预解析全部帧，供识图对齐做整体规划
func parsePlayFrames(rawLines [][]byte) []playFrame {
	frames := make([]playFrame, 0, len(rawLines))
	for _, line := range rawLines {
		var action GoInput.RecordAction
		if err := json.Unmarshal(line, &action); err == nil && action.Op != "" {
			a := action
			frames = append(frames, playFrame{action: &a})
			continue
		}
		var ev hook.Event
		if err := json.Unmarshal(line, &ev); err == nil {
			e := ev
			frames = append(frames, playFrame{event: &e})
		}
	}
	return frames
}

// runRecordLines 回放主循环
func (r *Runner) runRecordLines(rawLines [][]byte, opts PlayOptions) error {
	if opts.SpeedScale <= 0 {
		opts.SpeedScale = 1.0
	}
	// 0 = 未设置（用默认值）；负数 = 显式关闭
	if opts.VisionSettleMs == 0 {
		opts.VisionSettleMs = DefaultVisionSettleMs
	}
	if opts.VisionSettleMs < 0 {
		opts.VisionSettleMs = 0
	}

	frames := parsePlayFrames(rawLines)

	// 识图对齐方案：脚本中没有识图样本时自动退化为原样回放。
	// 整份脚本已读入内存，关键点、连通关系与帧归属一次性聚合完成，
	// 识别本身则交给"快速区域缓存 + 后台预取 + 采用前复核"的流水线。
	plan := buildVisionPlan(frames)
	matcher := newVisionMatcher(opts, opts.VisionDir)
	var pipeline *visionPipeline
	if plan.usable() {
		pipeline = newVisionPipeline(plan, matcher, opts)
		defer pipeline.Close()
		fmt.Printf("[Vision] 检测到 %d 个识图关键点，%d 段路径将按识别结果旋转/拉伸对齐\n", len(plan.anchors), len(plan.segs))
		fmt.Printf("[Vision] 样本目录: %s (匹配度阈值 %.0f%%, 区域置信度 %.0f%%, 缩放 %.2f~%.2f, 预取窗口 %d, 快速区域 %d 倍边长)\n",
			opts.VisionDir, matcher.thresholdPercent, matcher.confirmPercent, matcher.minScale, matcher.maxScale, pipeline.lookahead, pipeline.cacheRatio)
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
	curX, curY := -1, -1 // 光标当前所在位置（用于判断点击前是否刚被挪动过）
	for idx, f := range frames {
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
		if f.action != nil {
			action := *f.action

			// 识图状态标记行：只做记录，不产生任何输入
			if action.Op == "vision" || action.Op == "vslot" {
				continue
			}

			isKeypoint := action.Vi != "" && mousePosOps[action.Op]

			// 识图：关键点在停顿**开始前**先把光标放到它当前已知的位置。
			//
			// 录制的语义是"鼠标先到位、停一下、再点击"。如果等到点击那一刻才把光标
			// 挪到识别位置，就会出现：停顿期间光标停在旧位置 -> 点击瞬间瞬移过去并同
			// 时按下，界面来不及看到"光标已经到位"，于是这次的点击被算在旧位置上
			// （表现为"好像先点了再移动"）。先到位再停顿，点击就一定落在正确位置上。
			if pipeline != nil && isKeypoint {
				if x, y, ok := pipeline.KeypointCursor(idx); ok && (x != curX || y != curY) {
					GoInput.FastSetCursorPos(x, y)
					curX, curY = x, y
				}
			}

			if action.Dt > 0 {
				// 按照 -t 指定的速率缩放时间间隔
				adjustedDt := float64(action.Dt) / opts.SpeedScale
				time.Sleep(time.Duration(adjustedDt) * time.Millisecond)
			}

			// 识图对齐：关键点重新定位 + 段内路径旋转/拉伸
			if pipeline != nil {
				if action.Vi != "" {
					// 段终点曾在本段开始时提前确认，到点用实时画面复核一次
					pipeline.ConfirmAtFrame(idx)
				}
				if mousePosOps[action.Op] {
					si := plan.blockOf[idx]
					if si >= 0 {
						// 复核/重新定位可能刚改掉位置 => 段变换会按新位置重建
						plan.activateSeg(si, pipeline)
					}
					// 关键点帧：坐标以该关键点**最终确认的识别位置**为准。
					// 绝不能经由"可能是旧值的段变换"，否则点击会落在旧位置上。
					if x, y, ok := pipeline.KeypointTarget(idx); ok {
						action.X, action.Y = x, y
					} else if si >= 0 {
						action.X, action.Y = plan.segs[si].m.apply(action.X, action.Y)
					}
				}
			}

			// 光标在这一瞬间被挪动过（复核改了位置）=> 按下/松开前留一点时间，
			// 让界面先"看到"光标已经到位，否则这次点击可能仍被算在旧位置上。
			if pipeline != nil && isKeypoint && opts.VisionSettleMs > 0 && (action.X != curX || action.Y != curY) {
				GoInput.FastSetCursorPos(action.X, action.Y)
				curX, curY = action.X, action.Y
				time.Sleep(time.Duration(float64(opts.VisionSettleMs)/opts.SpeedScale) * time.Millisecond)
			}

			replayAction(&action)
			if mousePosOps[action.Op] {
				curX, curY = action.X, action.Y
			}
			continue
		}

		// 2. 兼容旧版原语事件 hook.Event
		if f.event != nil {
			ev := f.event
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
			replayEvent(ev)
		}
	}

	// 播放结束时，若使用了虚拟手柄，安全归中并彻底释放，避免摇杆卡死残留
	vg, _ := GoInput.GetOrInitVirtualGamepad()
	if vg != nil {
		vg.Close()
	}

	GoInput.SoundStop()
	if s := matcher.summary(); s != "" {
		fmt.Println(s)
	}
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
