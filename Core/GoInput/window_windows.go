package GoInput

import (
	"syscall"
	"unsafe"
)

var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	procFindWindowW       = modUser32.NewProc("FindWindowW")
	procGetClientRect     = modUser32.NewProc("GetClientRect")
	procClientToScreen    = modUser32.NewProc("ClientToScreen")
	procSetForegroundWin  = modUser32.NewProc("SetForegroundWindow")
	procPostMessageW      = modUser32.NewProc("PostMessageW")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

const (
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	MK_LBUTTON     = 0x0001
)

// FindWindow finds top-level window by title substring or exact title.
func FindWindow(title string) uintptr {
	ptr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	return hwnd
}

// GetClientBounds returns the screen coordinates (x, y, width, height) of the window client area.
func GetClientBounds(hwnd uintptr) (x, y, w, h int, ok bool) {
	if hwnd == 0 {
		return 0, 0, 0, 0, false
	}
	var rc RECT
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	var pt POINT
	pt.X = rc.Left
	pt.Y = rc.Top
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y), int(rc.Right - rc.Left), int(rc.Bottom - rc.Top), true
}

// SetForeground sets the target window to foreground.
func SetForeground(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	ret, _, _ := procSetForegroundWin.Call(hwnd)
	return ret != 0
}

// PostClick sends background mouse down and up messages to window client coordinates.
func PostClick(hwnd uintptr, clientX, clientY int) {
	lParam := uintptr((uint32(clientY) << 16) | (uint32(clientX) & 0xFFFF))
	procPostMessageW.Call(hwnd, WM_LBUTTONDOWN, MK_LBUTTON, lParam)
	procPostMessageW.Call(hwnd, WM_LBUTTONUP, 0, lParam)
}
