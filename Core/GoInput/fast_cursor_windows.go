package GoInput

import (
	"unsafe"
)

var (
	procSetCursorPos = modUser32.NewProc("SetCursorPos")
)

// IsCursorHidden 检查当前系统全局光标是否处于隐藏状态 (3D/VR 视角模式)
// 通过 Windows 内核 API GetCursorInfo 查询 CURSOR_SHOWING 标志位 (0x00000001)
func IsCursorHidden() bool {
	var ci CURSORINFO
	ci.CbSize = uint32(unsafe.Sizeof(ci))
	ret, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if ret != 0 {
		return (ci.Flags & CURSOR_SHOWING) == 0
	}
	return false
}

// FastSetCursorPos 直接调用 Windows 原生内核 API 设置鼠标绝对位置
// 单次耗时仅微秒级（~10微秒），零用户态延迟，是 Windows 上最高速、最精准的绝对光标定位方式。
func FastSetCursorPos(x, y int) {
	_, _, _ = procSetCursorPos.Call(uintptr(x), uintptr(y))
}

// FastMouseDown 发送鼠标按下事件，确保即使在快速移动中也能精准生效
func FastMouseDown(btn string) {
	var flag uint32 = MOUSEEVENTF_LEFTDOWN
	if btn == "right" {
		flag = MOUSEEVENTF_RIGHTDOWN
	} else if btn == "center" {
		flag = MOUSEEVENTF_MIDDLEDOWN
	}
	SendMouseButton(flag)
}

// FastMouseUp 发送鼠标释放事件
func FastMouseUp(btn string) {
	var flag uint32 = MOUSEEVENTF_LEFTUP
	if btn == "right" {
		flag = MOUSEEVENTF_RIGHTUP
	} else if btn == "center" {
		flag = MOUSEEVENTF_MIDDLEUP
	}
	SendMouseButton(flag)
}
