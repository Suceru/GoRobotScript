package GoInput

import (
	"syscall"
	"unsafe"
)

// XINPUT 按钮标志位定义
const (
	XINPUT_GAMEPAD_DPAD_UP        = 0x0001
	XINPUT_GAMEPAD_DPAD_DOWN      = 0x0002
	XINPUT_GAMEPAD_DPAD_LEFT      = 0x0004
	XINPUT_GAMEPAD_DPAD_RIGHT     = 0x0008
	XINPUT_GAMEPAD_START          = 0x0010
	XINPUT_GAMEPAD_BACK           = 0x0020
	XINPUT_GAMEPAD_LEFT_THUMB     = 0x0040
	XINPUT_GAMEPAD_RIGHT_THUMB    = 0x0080
	XINPUT_GAMEPAD_LEFT_SHOULDER  = 0x0100
	XINPUT_GAMEPAD_RIGHT_SHOULDER = 0x0200
	XINPUT_GAMEPAD_A              = 0x1000
	XINPUT_GAMEPAD_B              = 0x2000
	XINPUT_GAMEPAD_X              = 0x4000
	XINPUT_GAMEPAD_Y              = 0x8000
)

type XINPUT_GAMEPAD struct {
	WButtons      uint16
	BLeftTrigger  uint8
	BRightTrigger uint8
	SThumbLX      int16
	SThumbLY      int16
	SThumbRX      int16
	SThumbRY      int16
}

type XINPUT_STATE struct {
	DwPacketNumber uint32
	Gamepad        XINPUT_GAMEPAD
}

var (
	xinputDll           *syscall.LazyDLL
	procXInputGetState  *syscall.LazyProc
)

func init() {
	// 动态加载 XInput 运行库 (优先 1.4，回退 1.3 或 9.1.0)
	dlls := []string{"xinput1_4.dll", "xinput1_3.dll", "xinput9_1_0.dll"}
	for _, name := range dlls {
		dll := syscall.NewLazyDLL(name)
		if proc := dll.NewProc("XInputGetState"); proc.Find() == nil {
			xinputDll = dll
			procXInputGetState = proc
			break
		}
	}
}

// GetGamepadState 读取指定手柄 (userIndex 0~3) 的实时物理状态
func GetGamepadState(userIndex uint32) (*XINPUT_STATE, bool) {
	if procXInputGetState == nil {
		return nil, false
	}
	var state XINPUT_STATE
	ret, _, _ := procXInputGetState.Call(uintptr(userIndex), uintptr(unsafe.Pointer(&state)))
	if ret == 0 { // ERROR_SUCCESS
		return &state, true
	}
	return nil, false
}
