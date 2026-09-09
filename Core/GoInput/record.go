package GoInput

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	hook "github.com/robotn/gohook"
)

// RecordAction 表示统一的键鼠与手柄动作帧
type RecordAction struct {
	Dt int64  `json:"dt"`           // 距离上一帧毫秒差 (整数 ms，极速解码)
	Op string `json:"op"`           // "init"(起点锚定), "mv"(绝对坐标移动), "rmv"(3D/VR相对位移), "kd"/"ku"(键按下/弹起), "md"/"mu"(鼠标按下/弹起), "mw"(滚轮), "gp"(手柄动作)

	// 键盘与鼠标按键
	Key string `json:"key,omitempty"` // 键名
	Btn string `json:"btn,omitempty"` // 鼠标按钮 ("left", "right", "center")

	// 鼠标坐标与位移
	X  int `json:"x,omitempty"`  // 屏幕绝对坐标 X
	Y  int `json:"y,omitempty"`  // 屏幕绝对坐标 Y
	Dx int `json:"dx,omitempty"` // 3D/VR 物理相对位移 X
	Dy int `json:"dy,omitempty"` // 3D/VR 物理相对位移 Y

	// 滚轮
	Roll int `json:"roll,omitempty"`

	// 游戏手柄状态 (Xbox / DirectInput 通用)
	GpBtns uint16 `json:"gp_b,omitempty"`  // 手柄按键掩码
	GpLT   uint8  `json:"gp_lt,omitempty"` // 左扳机 (0~255)
	GpRT   uint8  `json:"gp_rt,omitempty"` // 右扳机 (0~255)
	GpLX   int16  `json:"gp_lx,omitempty"` // 左摇杆 X (-32768 ~ 32767)
	GpLY   int16  `json:"gp_ly,omitempty"` // 左摇杆 Y
	GpRX   int16  `json:"gp_rx,omitempty"` // 右摇杆 X (视角)
	GpRY   int16  `json:"gp_ry,omitempty"` // 右摇杆 Y (视角)
}

// CURSORINFO 结构体用于实时判定光标是否处于隐藏状态
type CURSORINFO struct {
	CbSize      uint32
	Flags       uint32
	HCursor     uintptr
	PtScreenPos POINT
}

const CURSOR_SHOWING = 0x00000001

var (
	procGetCursorInfo = modUser32.NewProc("GetCursorInfo")
	procGetCursorPos  = modUser32.NewProc("GetCursorPos")
)

// GetCursorVisibleAndPos 检测系统光标是否可见及当前屏幕坐标
func GetCursorVisibleAndPos() (bool, int, int) {
	var ci CURSORINFO
	ci.CbSize = uint32(unsafe.Sizeof(ci))
	ret, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if ret != 0 {
		visible := (ci.Flags & CURSOR_SHOWING) != 0
		return visible, int(ci.PtScreenPos.X), int(ci.PtScreenPos.Y)
	}
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return true, int(pt.X), int(pt.Y)
}

// StreamlineRecorder 高性能非阻塞流式录制器
type StreamlineRecorder struct {
	OutFile     string
	stopFlag    int32
	pauseFlag   int32
	startedFlag int32
	is3DMode    bool
	mu          sync.Mutex
	lastTime    time.Time
	file        *os.File
	DoneChan    chan bool

	// 内存异步缓冲环形队列 (零磁盘 I/O 阻塞，保证录制 100% 顺畅)
	actionQueue chan []byte

	// 鼠标状态控制
	lastMouseX        int
	lastMouseY        int
	rawDx             int32
	rawDy             int32
	lastLButtonDown   bool
	lastRButtonDown   bool
	lastCursorVisible bool // 上一帧光标是否可见，用于平滑切换 3D 视角与 UI 界面

	// 键盘按键去重表
	pressedKeys map[string]bool

	// 手柄上一帧状态 (差值比对，减少冗余)
	lastGpState XINPUT_GAMEPAD
	hasGamepad  bool
}

// StartSmartRecordingDeprecated (已废弃旧版：保留作为历史参考)
// 旧版依赖 RawInput 的 rmv 物理位移与 mv 混合竞争机制，由于后台消息泵断流会导致 3D 旋转退化
func StartSmartRecordingDeprecated(destPath string, is3DMode bool) (*StreamlineRecorder, error) {
	return StartSmartRecording(destPath, is3DMode)
}

// StartSmartRecording 启动统一纯净录制器 (基于 mv 统一架构，无需判断 rmv)
// 录制端只负责采集标准 mv 动作帧（包含每 16ms 的 curX, curY 以及 moveDx, moveDy），
// 不再依赖不可靠的 RawInput rmv，简化结构，消除多轨竞争与断流问题。
func StartSmartRecording(destPath string, is3DMode bool) (*StreamlineRecorder, error) {
	if destPath == "" {
		exePath, err := os.Executable()
		var baseDir string
		if err == nil {
			baseDir = filepath.Dir(exePath)
		} else {
			baseDir, _ = os.Getwd()
		}
		scriptDir := filepath.Join(baseDir, "script")
		_ = os.MkdirAll(scriptDir, 0755)

		prefix := "DesktopView"
		if is3DMode {
			prefix = "VRView3D"
		}
		fileName := fmt.Sprintf("%s_%s.script", prefix, time.Now().Format("2006-01-02_15-04-05"))
		destPath = filepath.Join(scriptDir, fileName)
	} else {
		_ = os.MkdirAll(filepath.Dir(destPath), 0755)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("无法创建录制脚本文件: %w", err)
	}

	_, initX, initY := GetCursorVisibleAndPos()

	rec := &StreamlineRecorder{
		OutFile:     destPath,
		file:        f,
		is3DMode:    is3DMode,
		DoneChan:    make(chan bool, 1),
		actionQueue: make(chan []byte, 10000), // 10000 帧高容量内存缓冲区
		lastMouseX:  initX,
		lastMouseY:  initY,
		pressedKeys: make(map[string]bool),
	}

	// 检查是否有手柄连接
	if _, ok := GetGamepadState(0); ok {
		rec.hasGamepad = true
		fmt.Println(" [Hardware] 检测到已连接 Xbox/XInput 游戏手柄，已开启手柄轴向与按键录制！")
	}

	// 1. 启动专用后台磁盘持久化写入协程 (非阻塞写盘)
	go rec.diskWriterWorker()

	// 2. 启动系统全局热键监听 (PgUp 开始/暂停，PgDn 结束)
	rec.startGlobalHotkeyListener()

	// 3. 启动 3D/VR 原始硬件输入捕获 (Raw Input)
	if is3DMode {
		go rec.listenRawInput()
	}

	// 4. 启动键鼠主监听
	go rec.listenHook()

	// 5. 启动 60 FPS 定时器（融合原始物理相对位移与手柄轮询）
	go rec.tickerLoop()

	return rec, nil
}

// pushAction 将动作压入内存缓冲区，完全不执行任何磁盘写入或等待
func (r *StreamlineRecorder) pushAction(action RecordAction) {
	r.mu.Lock()
	now := time.Now()
	if r.lastTime.IsZero() {
		action.Dt = 0
	} else {
		action.Dt = now.Sub(r.lastTime).Milliseconds()
	}
	r.lastTime = now
	r.mu.Unlock()

	data, err := json.Marshal(action)
	if err == nil {
		select {
		case r.actionQueue <- data:
		default:
			// 内存队列满时的保护
		}
	}
}

// diskWriterWorker 后台专用落盘协程：批量高效写入磁盘，绝不干扰前台录制
func (r *StreamlineRecorder) diskWriterWorker() {
	defer r.file.Close()
	writer := bufio.NewWriterSize(r.file, 64*1024) // 64KB 高性能缓冲

	flushTicker := time.NewTicker(200 * time.Millisecond)
	defer flushTicker.Stop()

	for {
		select {
		case data, ok := <-r.actionQueue:
			if !ok {
				_ = writer.Flush()
				return
			}
			_, _ = writer.Write(data)
			_ = writer.WriteByte('\n')

		case <-flushTicker.C:
			_ = writer.Flush()
			if atomic.LoadInt32(&r.stopFlag) == 1 && len(r.actionQueue) == 0 {
				return
			}
		}
	}
}

// tickerLoop 60 FPS 采样循环：智能处理光标显示（绝对坐标）/光标隐藏（3D相对脉冲）以及手柄状态
func (r *StreamlineRecorder) tickerLoop() {
	ticker := time.NewTicker(16 * time.Millisecond) // 60 FPS
	defer ticker.Stop()

	firstFrame := true

	for range ticker.C {
		if atomic.LoadInt32(&r.stopFlag) == 1 {
			return
		}
		if atomic.LoadInt32(&r.startedFlag) == 0 || atomic.LoadInt32(&r.pauseFlag) == 1 {
			continue
		}

		cursorVisible, curX, curY := GetCursorVisibleAndPos()

		// 第一帧启动处理：
		if firstFrame {
			// 清空启动前积累的所有硬件相对位移，消除启动瞬间回中导致的巨大跳变！
			r.mu.Lock()
			r.rawDx = 0
			r.rawDy = 0
			r.mu.Unlock()

			r.lastMouseX = curX
			r.lastMouseY = curY
			r.lastCursorVisible = cursorVisible

			// 只有光标未隐藏（常规桌面/UI模式）才需要写入 init 起点定位；光标隐藏（3D视角）不记录 init 避免视角突变！
			if cursorVisible {
				r.pushAction(RecordAction{
					Op: "init",
					X:  curX,
					Y:  curY,
				})
			}
			firstFrame = false
			continue
		}

		// 1. 获取底层硬件 Raw Input 相对位移脉冲
		r.mu.Lock()
		dx := r.rawDx
		dy := r.rawDy
		r.rawDx = 0
		r.rawDy = 0
		r.mu.Unlock()

		if r.is3DMode && (dx != 0 || dy != 0) {
			// 3D 模式：只要捕获到底层硬件脉冲，直接写入 rmv 原始输入帧 (100% 原始位移，无系统加速失真)
			r.pushAction(RecordAction{
				Op: "rmv",
				Dx: int(dx),
				Dy: int(dy),
			})
		}

		// 2. 采集光标移动 (UI 界面或桌面模式)
		moveDx := curX - r.lastMouseX
		moveDy := curY - r.lastMouseY
		if moveDx != 0 || moveDy != 0 {
			r.lastMouseX = curX
			r.lastMouseY = curY

			// 如果不是 3D 模式，或者光标可见（UI/背包界面），记录标准绝对定位 mv
			if !r.is3DMode || cursorVisible {
				// 【核心防跳变机制】：
				// 当从 3D 模式切换到 2D UI (光标刚刚从隐藏变为可见) 的瞬间，
				// 游戏会把光标从屏幕任意位置直接瞬移到窗口中心或 UI 起点。
				// 这种单帧巨大跳跃（或光标从隐藏到显示的首次出现）是坐标复位，不是视角的物理位移，
				// 将其相对差值 moveDx/moveDy 归零，只记录绝对坐标 (x, y)，彻底防止 3D 视角发生暴甩跳变！
				justBecameVisible := cursorVisible && !r.lastCursorVisible
				if justBecameVisible || absInt(int(moveDx)) > 80 || absInt(int(moveDy)) > 80 {
					moveDx = 0
					moveDy = 0
				}

				r.pushAction(RecordAction{
					Op: "mv",
					X:  curX,
					Y:  curY,
					Dx: moveDx,
					Dy: moveDy,
				})
			}
		}

		r.lastCursorVisible = cursorVisible

		// 实时检测鼠标物理按键状态 (左键 VK_LBUTTON 0x01, 右键 VK_RBUTTON 0x02)
		// 硬件级轮询保证 100% 捕获按下与松开，绝对不丢失 MouseUp
		retL, _, _ := procGetAsyncKeyState.Call(0x01)
		currL := (uint16(retL) & 0x8000) != 0
		if currL != r.lastLButtonDown {
			r.lastLButtonDown = currL
			if currL {
				r.pushAction(RecordAction{
					Op:  "md",
					Btn: "left",
					X:   curX,
					Y:   curY,
				})
			} else {
				r.pushAction(RecordAction{
					Op:  "mu",
					Btn: "left",
					X:   curX,
					Y:   curY,
				})
			}
		}

		retR, _, _ := procGetAsyncKeyState.Call(0x02)
		currR := (uint16(retR) & 0x8000) != 0
		if currR != r.lastRButtonDown {
			r.lastRButtonDown = currR
			if currR {
				r.pushAction(RecordAction{
					Op:  "md",
					Btn: "right",
					X:   curX,
					Y:   curY,
				})
			} else {
				r.pushAction(RecordAction{
					Op:  "mu",
					Btn: "right",
					X:   curX,
					Y:   curY,
				})
			}
		}
		if r.hasGamepad {
			if state, ok := GetGamepadState(0); ok {
				gp := state.Gamepad
				last := r.lastGpState
				// 只要硬件上报的状态有任何实际变化，原汁原味记录原始轴向和按键数据
				if gp.WButtons != last.WButtons ||
					gp.BLeftTrigger != last.BLeftTrigger ||
					gp.BRightTrigger != last.BRightTrigger ||
					gp.SThumbLX != last.SThumbLX ||
					gp.SThumbLY != last.SThumbLY ||
					gp.SThumbRX != last.SThumbRX ||
					gp.SThumbRY != last.SThumbRY {

					r.lastGpState = gp
					r.pushAction(RecordAction{
						Op:     "gp",
						GpBtns: gp.WButtons,
						GpLT:   gp.BLeftTrigger,
						GpRT:   gp.BRightTrigger,
						GpLX:   gp.SThumbLX,
						GpLY:   gp.SThumbLY,
						GpRX:   gp.SThumbRX,
						GpRY:   gp.SThumbRY,
					})
				}
			}
		}
	}
}

// listenRawInput 捕获底层的硬件鼠标相对微位移 (Raw Input)
// 使用 runtime.LockOSThread 锁定专属 Win32 线程，保证 Windows 消息队列不会因协程调度而漂移断流
func (r *StreamlineRecorder) listenRawInput() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("STATIC")
	windowName, _ := syscall.UTF16PtrFromString("RawInputReceiver")

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0, 0, 0, 0, 0,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return
	}
	defer procDestroyWindow.Call(hwnd)

	var rid RAWINPUTDEVICE
	rid.UsUsagePage = 0x01
	rid.UsUsage = 0x02
	rid.DwFlags = RIDEV_INPUTSINK
	rid.HwndTarget = hwnd

	ret, _, _ := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, uintptr(unsafe.Sizeof(rid)))
	if ret == 0 {
		return
	}

	var msg MSG
	for atomic.LoadInt32(&r.stopFlag) == 0 {
		res, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(res) <= 0 {
			break
		}

		if msg.Message == WM_INPUT {
			var raw RAWINPUT
			size := uint32(unsafe.Sizeof(raw))
			procGetRawInputData.Call(
				msg.LParam,
				0x10000003,
				uintptr(unsafe.Pointer(&raw)),
				uintptr(unsafe.Pointer(&size)),
				uintptr(unsafe.Sizeof(raw.Header)),
			)

			if raw.Header.DwType == RIM_TYPEMOUSE && (raw.Mouse.UsFlags&0x01) == MOUSE_MOVE_RELATIVE {
				dx := raw.Mouse.LastX
				dy := raw.Mouse.LastY
				if dx != 0 || dy != 0 {
					r.mu.Lock()
					r.rawDx += dx
					r.rawDy += dy
					r.mu.Unlock()
				}
			}
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// listenHook 独立监听键盘与鼠标点击，各通道互不阻塞
func (r *StreamlineRecorder) listenHook() {
	evChan := hook.Start()
	defer hook.End()

	for ev := range evChan {
		if atomic.LoadInt32(&r.stopFlag) == 1 {
			break
		}
		if ev.Rawcode == 33 || ev.Rawcode == 34 {
			continue
		}
		if atomic.LoadInt32(&r.startedFlag) == 0 || atomic.LoadInt32(&r.pauseFlag) == 1 {
			continue
		}

		switch ev.Kind {
		case hook.KeyDown:
			key := hook.RawcodetoKeychar(ev.Rawcode)
			if key == "" && ev.Keychar != 0 && ev.Keychar != 65535 {
				key = string(rune(ev.Keychar))
			}
			if key == "" {
				continue
			}

			r.mu.Lock()
			if r.pressedKeys[key] {
				r.mu.Unlock()
				continue
			}
			r.pressedKeys[key] = true
			r.mu.Unlock()

			r.pushAction(RecordAction{
				Op:  "kd",
				Key: key,
			})

		case hook.KeyUp:
			key := hook.RawcodetoKeychar(ev.Rawcode)
			if key == "" && ev.Keychar != 0 && ev.Keychar != 65535 {
				key = string(rune(ev.Keychar))
			}
			if key == "" {
				continue
			}

			r.mu.Lock()
			r.pressedKeys[key] = false
			r.mu.Unlock()

			r.pushAction(RecordAction{
				Op:  "ku",
				Key: key,
			})

		case hook.MouseDown:
			// 优先使用硬件级检测处理，防止重复事件，在此仅作为辅助兜底
		case hook.MouseUp:
			// 优先使用硬件级检测处理，防止重复事件，在此仅作为辅助兜底

		case hook.MouseWheel:
			r.pushAction(RecordAction{
				Op:   "mw",
				Roll: int(ev.Rotation),
			})
		}
	}
}

// Stop 停止录制
func (r *StreamlineRecorder) Stop() {
	atomic.StoreInt32(&r.stopFlag, 1)
	hook.End()
	close(r.actionQueue)
}
