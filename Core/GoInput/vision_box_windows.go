package GoInput

import (
	"image"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"GoRobotScript/Core/GoVision"
)

// ---------------------------------------------------------------------------
// 识图绘图框 (Vision Box)
//
// 在光标周围画一个跟随鼠标移动的方框，直观显示"当前这一枪会截取多大范围"。
// 实现方式是标准的**拖动虚影窗口**做法：一个置顶的 WS_POPUP 窗口，用窗口区域
// (SetWindowRgn) 挖空内部只留边框，位置随光标变化调用 SetWindowPos 平移。
// 因此它既不重绘屏幕、也不参与命中测试（鼠标点击直接穿透到游戏里），
// 开销与拖动文件时的那个半透明附属框完全同级。
// ---------------------------------------------------------------------------

// 绘图框默认外观
const (
	VisionBoxColorDefault = 0x000000FF // COLORREF(0x00BBGGRR) = 纯红
	VisionBoxLineDefault  = 3          // 边框线宽 (像素)
)

// VisionBoxLongPressMs 长按判定阈值 (毫秒)
const VisionBoxLongPressMs = 450

// 窗口样式与消息常量
const (
	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExTransparent = 0x00000020
	wsExNoActivate  = 0x08000000

	wmDestroy     = 0x0002
	wmEraseBkgnd  = 0x0014
	wmSetCursor   = 0x0020
	wmNCHitTest   = 0x0084
	pmRemove      = 0x0001
	swHide        = 0
	swShowNoActive = 4
	rgnDiff       = 4

	swpNoSize     = 0x0001
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
)

// htTransparent 让窗口对鼠标完全透明 (点击穿透到下层窗口)
const htTransparent = ^uintptr(0) // (uintptr)(-1)

var (
	modGdi32 = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procSetWindowPos     = modUser32.NewProc("SetWindowPos")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procPeekMessageW     = modUser32.NewProc("PeekMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procFillRect         = modUser32.NewProc("FillRect")
	procSetWindowRgn     = modUser32.NewProc("SetWindowRgn")

	procCreateSolidBrush = modGdi32.NewProc("CreateSolidBrush")
	procCreateRectRgn    = modGdi32.NewProc("CreateRectRgn")
	procCombineRgn       = modGdi32.NewProc("CombineRgn")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// visionBoxBrush 绘图框背景刷 (边框颜色)，在窗口创建时建立
var visionBoxBrush uintptr

// visionBoxWndProc 绘图框窗口过程：只处理"点击穿透""填边框色""不碰光标"
func visionBoxWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmNCHitTest:
		// 让鼠标完全穿透，绝不影响游戏内的点击与拖拽
		return htTransparent
	case wmSetCursor:
		// 窗口在光标下方移动时，系统会把光标重置成窗口类默认 (箭头)。
		// 这里直接返回 TRUE 且不调用 SetCursor，保持当前光标形状不变，
		// 否则会出现"画笔/十字 <-> 箭头"反复闪烁。
		return 1
	case wmEraseBkgnd:
		if visionBoxBrush != 0 {
			var rc RECT
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
			procFillRect.Call(wparam, uintptr(unsafe.Pointer(&rc)), visionBoxBrush)
			return 1
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
}

// visionBox 跟随光标的边框附属框
type visionBox struct {
	mu      sync.Mutex
	hwnd    uintptr
	started bool
	stopped bool
	enabled bool
	visible bool
	line    int
	curX    int
	curY    int
	curW    int
	curH    int

	sizeFn func() int // 当前采样边长 (跟随预设槽)

	quit chan struct{}
	done chan struct{}
}

func newVisionBox(sizeFn func() int) *visionBox {
	return &visionBox{
		line:   VisionBoxLineDefault,
		sizeFn: sizeFn,
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// start 启动绘图框线程 (独占一个 OS 线程，自带消息泵)。
// 立即返回，不等待窗口就绪，避免阻塞热键轮询。
func (b *visionBox) start() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.started || b.stopped {
		b.mu.Unlock()
		return
	}
	b.started = true
	b.mu.Unlock()
	go b.run()
}

func (b *visionBox) run() {
	// 窗口与消息队列必须绑定固定线程
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(b.done)

	className, err := syscall.UTF16PtrFromString("GoRobotVisionBox")
	if err != nil {
		return
	}
	title, _ := syscall.UTF16PtrFromString("GoRobotVisionBox")
	hInst, _, _ := procGetModuleHandleW.Call(0)

	visionBoxBrush, _, _ = procCreateSolidBrush.Call(uintptr(VisionBoxColorDefault))

	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(visionBoxWndProc),
		HInstance:     hInst,
		HbrBackground: visionBoxBrush,
		LpszClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExTopmost|wsExToolWindow|wsExTransparent|wsExNoActivate),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		0, 0, 1, 1,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return
	}

	b.mu.Lock()
	b.hwnd = hwnd
	b.mu.Unlock()

	ticker := time.NewTicker(8 * time.Millisecond) // ~120Hz 跟随，单次开销仅一次坐标读取
	defer ticker.Stop()

	for {
		select {
		case <-b.quit:
			procDestroyWindow.Call(hwnd)
			return
		case <-ticker.C:
			b.pumpMessages()
			b.follow()
		}
	}
}

// pumpMessages 非阻塞清空消息队列 (窗口绘制、销毁等都需要)
func (b *visionBox) pumpMessages() {
	var msg MSG
	for {
		ret, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove)
		if ret == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// visionBoxWindowRect 由采样区域推出绘图框窗口矩形。
//
// **边框画在采样区域外侧**：窗口 = 采样区域四周各外扩一条边框宽度，
// 于是窗口被 SetWindowRgn 挖空后的内圈正好等于采样区域本身。
// 这样红框永远不会被截进样本图里（模板保持干净），
// 用户看到的空心内圈也就是实际采样范围。
func visionBoxWindowRect(patch image.Rectangle, line int) image.Rectangle {
	if line < 0 {
		line = 0
	}
	return image.Rect(
		patch.Min.X-line, patch.Min.Y-line,
		patch.Max.X+line, patch.Max.Y+line,
	)
}

// follow 让方框跟随光标；位置没变时什么都不做（不产生任何重绘）
func (b *visionBox) follow() {
	b.mu.Lock()
	hwnd, enabled, visible, line, sizeFn := b.hwnd, b.enabled, b.visible, b.line, b.sizeFn
	b.mu.Unlock()
	if hwnd == 0 {
		return
	}

	if !enabled || sizeFn == nil {
		b.hide()
		return
	}

	_, cx, cy := GetCursorVisibleAndPos()
	patch, _, _, ok := GoVision.PatchGeometry(cx, cy, sizeFn())
	if !ok || patch.Dx() <= 0 || patch.Dy() <= 0 {
		b.hide()
		return
	}

	win := visionBoxWindowRect(patch, line)
	w, h := win.Dx(), win.Dy()
	x, y := win.Min.X, win.Min.Y

	// 只有尺寸或位置真正变化时才触碰窗口
	if w != b.curW || h != b.curH {
		applyVisionBoxRegion(hwnd, w, h, line)
		b.curW, b.curH = w, h
		b.curX, b.curY = -1, -1 // 强制走一次定位
	}
	if x == b.curX && y == b.curY && visible {
		return
	}

	b.curX, b.curY = x, y
	// 必须同时设置尺寸：窗口初始为 1x1，若只移动不改变大小，
	// 窗口区域会被窗口自身尺寸裁剪成只剩一个像素
	flags := uintptr(swpNoActivate)
	if !visible {
		flags |= swpShowWindow
		b.mu.Lock()
		b.visible = true
		b.mu.Unlock()
	}
	// HWND_TOPMOST = -1
	procSetWindowPos.Call(hwnd, ^uintptr(0), uintptr(x), uintptr(y), uintptr(w), uintptr(h), flags)
}

// hide 隐藏方框
func (b *visionBox) hide() {
	b.mu.Lock()
	hwnd, visible := b.hwnd, b.visible
	b.visible = false
	b.mu.Unlock()
	if hwnd != 0 && visible {
		procShowWindow.Call(hwnd, swHide)
	}
}

// setEnabled 开关绘图框
func (b *visionBox) setEnabled(on bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.enabled = on
	b.mu.Unlock()
	if !on {
		b.hide()
	}
}

// isEnabled 当前是否开启
func (b *visionBox) isEnabled() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// stop 关闭绘图框并回收窗口
func (b *visionBox) stop() {
	if b == nil {
		return
	}
	b.setEnabled(false)
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.stopped = true
	quit := b.quit
	b.hwnd = 0
	b.mu.Unlock()
	if quit != nil {
		close(quit)
	}
}

// applyVisionBoxRegion 把窗口区域裁成"只有边框"的空心方框
func applyVisionBoxRegion(hwnd uintptr, w, h, line int) {
	if w <= 0 || h <= 0 {
		return
	}
	if line < 1 {
		line = 1
	}
	outer, _, _ := procCreateRectRgn.Call(0, 0, uintptr(w), uintptr(h))
	if outer == 0 {
		return
	}
	if w > 2*line && h > 2*line {
		inner, _, _ := procCreateRectRgn.Call(uintptr(line), uintptr(line), uintptr(w-line), uintptr(h-line))
		if inner != 0 {
			procCombineRgn.Call(outer, outer, inner, rgnDiff)
			procDeleteObject.Call(inner)
		}
	}
	// SetWindowRgn 之后区域归系统所有，不能自行释放
	procSetWindowRgn.Call(hwnd, outer, 1)
}
