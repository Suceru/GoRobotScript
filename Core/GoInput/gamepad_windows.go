package GoInput

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// 动态虚拟手柄驱动接口定义 (ViGEm / VirtualDesktop 兼容协议)
type vigem_alloc_fn func() uintptr
type vigem_connect_fn func(uintptr) int32
type vigem_disconnect_fn func(uintptr)
type vigem_free_fn func(uintptr)
type vigem_target_x360_alloc_fn func() uintptr
type vigem_target_add_fn func(uintptr, uintptr) int32
type vigem_target_remove_fn func(uintptr, uintptr) int32
type vigem_target_free_fn func(uintptr)

type xusb_report struct {
	wButtons      uint16
	bLeftTrigger  uint8
	bRightTrigger uint8
	sThumbLX      int16
	sThumbLY      int16
	sThumbRX      int16
	sThumbRY      int16
}

type VirtualGamepad struct {
	dll            *syscall.LazyDLL
	client         uintptr
	target         uintptr
	procUpdate     *syscall.LazyProc
	procDisconnect *syscall.LazyProc
	procFreeClient *syscall.LazyProc
	procRemove     *syscall.LazyProc
	procFreeTarget *syscall.LazyProc
	currentReport  xusb_report
}

var globalVirtualPad *VirtualGamepad

// GetOrInitVirtualGamepad 获取或初始化一个虚拟 Xbox 360 手柄
func GetOrInitVirtualGamepad() (*VirtualGamepad, error) {
	if globalVirtualPad != nil {
		return globalVirtualPad, nil
	}

	dllPaths := []string{
		`ViGEmClient.dll`,
		`C:\Program Files\Virtual Desktop\VirtualDesktop.GamepadEmulation.dll`,
	}

	var loadedDll *syscall.LazyDLL
	for _, p := range dllPaths {
		d := syscall.NewLazyDLL(p)
		if d.NewProc("vigem_alloc").Find() == nil {
			loadedDll = d
			break
		}
	}

	if loadedDll == nil {
		return nil, fmt.Errorf("找不到 ViGEmClient.dll，请确保 bin 目录下存在该动态库")
	}

	procAlloc := loadedDll.NewProc("vigem_alloc")
	procConnect := loadedDll.NewProc("vigem_connect")
	procTargetAlloc := loadedDll.NewProc("vigem_target_x360_alloc")
	procTargetAdd := loadedDll.NewProc("vigem_target_add")

	client, _, _ := procAlloc.Call()
	if client == 0 {
		return nil, syscall.EINVAL
	}

	res, _, _ := procConnect.Call(client)
	// VIGEM_ERROR_NONE = 0x20000000 (ERROR_SUCCESS in ViGEm API)
	// 0xE0000001 (VIGEM_ERROR_BUS_NOT_FOUND): 缺少 ViGEmBus 内核驱动
	if uint32(res) == 0xE0000001 {
		fmt.Println(" [Hardware] 检测到系统缺少 ViGEmBus 驱动，正在触发自动静默安装...")
		AutoInstallViGEmBusDriver()
		// 重新尝试连接一次
		res, _, _ = procConnect.Call(client)
	}

	if uint32(res) != 0x20000000 && uint32(res) != 0 {
		return nil, fmt.Errorf("vigem_connect 失败 (错误码: 0x%X)，请确认 ViGEmBus 驱动服务已启动", uint32(res))
	}

	target, _, _ := procTargetAlloc.Call()
	if target == 0 {
		return nil, syscall.EINVAL
	}

	resAdd, _, _ := procTargetAdd.Call(client, target)
	if uint32(resAdd) != 0x20000000 && uint32(resAdd) != 0 {
		return nil, fmt.Errorf("vigem_target_add failed: 0x%X", uint32(resAdd))
	}

	vg := &VirtualGamepad{
		dll:            loadedDll,
		client:         client,
		target:         target,
		procUpdate:     loadedDll.NewProc("vigem_target_x360_update"),
		procDisconnect: loadedDll.NewProc("vigem_disconnect"),
		procFreeClient: loadedDll.NewProc("vigem_free"),
		procRemove:     loadedDll.NewProc("vigem_target_remove"),
		procFreeTarget: loadedDll.NewProc("vigem_target_free"),
	}
	globalVirtualPad = vg
	return vg, nil
}

// SetLeftStick 设置虚拟手柄左摇杆轴向 (lx, ly 范围 -32768 ~ 32767)
func (vg *VirtualGamepad) SetLeftStick(lx, ly int16) bool {
	vg.currentReport.sThumbLX = lx
	vg.currentReport.sThumbLY = ly
	return vg.Update()
}

// SetRightStick 设置虚拟手柄右摇杆轴向 (rx, ry 范围 -32768 ~ 32767)
func (vg *VirtualGamepad) SetRightStick(rx, ry int16) bool {
	vg.currentReport.sThumbRX = rx
	vg.currentReport.sThumbRY = ry
	return vg.Update()
}

// SendReport 发送完整手柄状态包 (摇杆、按键、扳机)
func (vg *VirtualGamepad) SendReport(wButtons uint16, lt, rt uint8, lx, ly, rx, ry int16) bool {
	vg.currentReport.wButtons = wButtons
	vg.currentReport.bLeftTrigger = lt
	vg.currentReport.bRightTrigger = rt
	vg.currentReport.sThumbLX = lx
	vg.currentReport.sThumbLY = ly
	vg.currentReport.sThumbRX = rx
	vg.currentReport.sThumbRY = ry
	return vg.Update()
}

// Update 将当前内部状态同步给系统虚拟手柄
func (vg *VirtualGamepad) Update() bool {
	if vg.procUpdate != nil && vg.client != 0 && vg.target != 0 {
		r, _, _ := vg.procUpdate.Call(vg.client, vg.target, uintptr(unsafe.Pointer(&vg.currentReport)))
		return uint32(r) == 0x20000000 || uint32(r) == 0
	}
	return false
}

// AutoInstallViGEmBusDriver 当检测到系统未安装驱动时，自动静默安装 ViGEmBus 驱动
func AutoInstallViGEmBusDriver() bool {
	exePath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(exePath)
	}
	setupCandidates := []string{
		filepath.Join(baseDir, "tools", "ViGEmBusSetup.exe"),
		filepath.Join(baseDir, "ViGEmBusSetup.exe"),
		filepath.Join(baseDir, "..", "tools", "ViGEmBusSetup.exe"),
	}

	var installer string
	for _, p := range setupCandidates {
		if _, err := os.Stat(p); err == nil {
			installer = p
			break
		}
	}

	if installer == "" {
		fmt.Println(" [ViGEmBus] 未找到本地驱动安装包，可通过 build.ps1 或运行 tools/ViGEmBusSetup.exe 进行安装。")
		return false
	}

	cmd := exec.Command(installer, "/quiet", "/norestart")
	err = cmd.Run()
	if err == nil {
		fmt.Println(" [ViGEmBus] 驱动已完成静默安装！")
		return true
	}
	return false
}

// Close 关闭并释放虚拟手柄
func (vg *VirtualGamepad) Close() {
	if vg.client != 0 && vg.target != 0 {
		if vg.procRemove != nil {
			vg.procRemove.Call(vg.client, vg.target)
		}
		if vg.procFreeTarget != nil {
			vg.procFreeTarget.Call(vg.target)
		}
		if vg.procDisconnect != nil {
			vg.procDisconnect.Call(vg.client)
		}
		if vg.procFreeClient != nil {
			vg.procFreeClient.Call(vg.client)
		}
		vg.client = 0
		vg.target = 0
	}
	globalVirtualPad = nil
}
