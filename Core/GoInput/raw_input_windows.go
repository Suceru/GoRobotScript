package GoInput

import (
	"unsafe"
)

const (
	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040
	MOUSEEVENTF_WHEEL      = 0x0800
	INPUT_MOUSE            = 0

	RIM_TYPEMOUSE       = 0
	RIDEV_INPUTSINK     = 0x00000100
	WM_INPUT            = 0x00FF
	MOUSE_MOVE_RELATIVE = 0
)

type MOUSEINPUT struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	_pad        uint32
	DwExtraInfo uintptr
}

type INPUT struct {
	Type uint32
	_pad uint32
	Mi   MOUSEINPUT
}

type RAWINPUTHEADER struct {
	DwType  uint32
	DwSize  uint32
	HDevice uintptr
	WParam  uintptr
}

type RAWMOUSE struct {
	UsFlags            uint16
	Padding            uint16
	UsButtonFlags      uint16
	UsButtonData       uint16
	UlRawButtons       uint32
	LastX              int32
	LastY              int32
	UlExtraInformation uint32
}

type RAWINPUT struct {
	Header RAWINPUTHEADER
	Mouse  RAWMOUSE
}

type RAWINPUTDEVICE struct {
	UsUsagePage uint16
	UsUsage     uint16
	DwFlags     uint32
	HwndTarget  uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

var (
	procSendInput               = modUser32.NewProc("SendInput")
	procMouseEvent              = modUser32.NewProc("mouse_event")
	procRegisterRawInputDevices = modUser32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = modUser32.NewProc("GetRawInputData")
	procCreateWindowExW         = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow           = modUser32.NewProc("DestroyWindow")
	procGetMessageW             = modUser32.NewProc("GetMessageW")
	procTranslateMessage        = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW        = modUser32.NewProc("DispatchMessageW")
)

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// SendRelativeMouseMove 向 Windows 系统注入底层的硬件级相对位移脉冲
func SendRelativeMouseMove(dx, dy int32) {
	var in INPUT
	in.Type = INPUT_MOUSE
	in.Mi.Dx = dx
	in.Mi.Dy = dy
	in.Mi.DwFlags = MOUSEEVENTF_MOVE

	ret, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Sizeof(in)))
	if ret == 0 {
		procMouseEvent.Call(
			MOUSEEVENTF_MOVE,
			uintptr(uint32(dx)),
			uintptr(uint32(dy)),
			0, 0,
		)
	}
}

// SendMouseButton 注入鼠标按键事件
func SendMouseButton(flags uint32) {
	procMouseEvent.Call(uintptr(flags), 0, 0, 0, 0)
}
