package GoInput

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

// VIGEM_ERROR_NONE ViGEm API 成功返回码
const VIGEM_ERROR_NONE = 0x20000000

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
	xinputDll          *syscall.LazyDLL
	procXInputGetState *syscall.LazyProc
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

// ConnectedGamepadSlots 返回当前已连接的 XInput 槽位列表 (0~3)
func ConnectedGamepadSlots() []int {
	var slots []int
	for i := uint32(0); i < 4; i++ {
		if _, ok := GetGamepadState(i); ok {
			slots = append(slots, int(i))
		}
	}
	return slots
}

// GamepadState 手柄完整状态 (与录制脚本 gp 帧字段一一对应)
type GamepadState struct {
	Buttons uint16 // 按键掩码
	LT      uint8  // 左扳机 0~255
	RT      uint8  // 右扳机 0~255
	LX      int16  // 左摇杆 X -32768~32767
	LY      int16  // 左摇杆 Y
	RX      int16  // 右摇杆 X
	RY      int16  // 右摇杆 Y
}

// xinputButtonNames 按键名称到 XInput 掩码的映射表 (供 Lua 层使用)
var xinputButtonNames = map[string]uint16{
	"A": XINPUT_GAMEPAD_A, "B": XINPUT_GAMEPAD_B,
	"X": XINPUT_GAMEPAD_X, "Y": XINPUT_GAMEPAD_Y,
	"LB": XINPUT_GAMEPAD_LEFT_SHOULDER, "RB": XINPUT_GAMEPAD_RIGHT_SHOULDER,
	"L1": XINPUT_GAMEPAD_LEFT_SHOULDER, "R1": XINPUT_GAMEPAD_RIGHT_SHOULDER,
	"LS": XINPUT_GAMEPAD_LEFT_THUMB, "RS": XINPUT_GAMEPAD_RIGHT_THUMB,
	"L3": XINPUT_GAMEPAD_LEFT_THUMB, "R3": XINPUT_GAMEPAD_RIGHT_THUMB,
	"START": XINPUT_GAMEPAD_START, "BACK": XINPUT_GAMEPAD_BACK,
	"UP": XINPUT_GAMEPAD_DPAD_UP, "DOWN": XINPUT_GAMEPAD_DPAD_DOWN,
	"LEFT": XINPUT_GAMEPAD_DPAD_LEFT, "RIGHT": XINPUT_GAMEPAD_DPAD_RIGHT,
}

// ButtonsFromNames 将按名称解析为 XInput 按键掩码 (未知名称忽略)
func ButtonsFromNames(names []string) uint16 {
	var mask uint16
	for _, n := range names {
		if v, ok := xinputButtonNames[strings.ToUpper(strings.TrimSpace(n))]; ok {
			mask |= v
		}
	}
	return mask
}

// ButtonNames 返回全部合法按键名称 (供 Lua 层构建常量表)
func ButtonNames() map[string]uint16 {
	out := make(map[string]uint16, len(xinputButtonNames))
	for k, v := range xinputButtonNames {
		out[k] = v
	}
	return out
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

// VirtualGamepad 虚拟 Xbox 360 手柄句柄
type VirtualGamepad struct {
	dll            *syscall.LazyDLL
	client         uintptr
	target         uintptr
	Slot           int // 本虚拟手柄实际占用的 XInput 槽位 (-1 表示未知)
	procUpdate     *syscall.LazyProc
	procDisconnect *syscall.LazyProc
	procFreeClient *syscall.LazyProc
	procRemove     *syscall.LazyProc
	procFreeTarget *syscall.LazyProc
	state          GamepadState
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
		return nil, fmt.Errorf("找不到 ViGEmClient.dll，请确保程序目录下存在该动态库")
	}

	procAlloc := loadedDll.NewProc("vigem_alloc")
	procConnect := loadedDll.NewProc("vigem_connect")
	procTargetAlloc := loadedDll.NewProc("vigem_target_x360_alloc")
	procTargetAdd := loadedDll.NewProc("vigem_target_add")

	client, _, _ := procAlloc.Call()
	if client == 0 {
		return nil, fmt.Errorf("vigem_alloc 分配客户端失败")
	}

	res, _, _ := procConnect.Call(client)
	// 0xE0000001 (VIGEM_ERROR_BUS_NOT_FOUND): 缺少 ViGEmBus 内核驱动
	if uint32(res) == 0xE0000001 {
		fmt.Println(" [Hardware] 检测到系统缺少 ViGEmBus 驱动，正在触发自动静默安装...")
		AutoInstallViGEmBusDriver()
		res, _, _ = procConnect.Call(client)
	}

	if uint32(res) != VIGEM_ERROR_NONE && uint32(res) != 0 {
		return nil, fmt.Errorf("vigem_connect 失败 (错误码: 0x%X)，请确认 ViGEmBus 驱动服务已启动", uint32(res))
	}

	slotsBefore := ConnectedGamepadSlots()

	target, _, _ := procTargetAlloc.Call()
	if target == 0 {
		return nil, fmt.Errorf("vigem_target_x360_alloc 创建虚拟手柄失败")
	}

	resAdd, _, _ := procTargetAdd.Call(client, target)
	if uint32(resAdd) != VIGEM_ERROR_NONE && uint32(resAdd) != 0 {
		return nil, fmt.Errorf("vigem_target_add 挂载虚拟手柄失败 (错误码: 0x%X)", uint32(resAdd))
	}

	vg := &VirtualGamepad{
		dll:            loadedDll,
		client:         client,
		target:         target,
		Slot:           -1,
		procUpdate:     loadedDll.NewProc("vigem_target_x360_update"),
		procDisconnect: loadedDll.NewProc("vigem_disconnect"),
		procFreeClient: loadedDll.NewProc("vigem_free"),
		procRemove:     loadedDll.NewProc("vigem_target_remove"),
		procFreeTarget: loadedDll.NewProc("vigem_target_free"),
	}
	globalVirtualPad = vg

	// 探测虚拟手柄实际落入的 XInput 槽位，并给出可用性诊断
	time.Sleep(150 * time.Millisecond)
	vg.Slot = detectNewSlot(slotsBefore)
	reportGamepadSlots(vg.Slot, slotsBefore)

	return vg, nil
}

// detectNewSlot 通过前后槽位差异定位新挂载的虚拟手柄槽位
func detectNewSlot(before []int) int {
	after := ConnectedGamepadSlots()
	for _, s := range after {
		found := false
		for _, b := range before {
			if b == s {
				found = true
				break
			}
		}
		if !found {
			return s
		}
	}
	if len(after) > 0 {
		return after[len(after)-1]
	}
	return -1
}

// reportGamepadSlots 输出虚拟手柄槽位诊断信息，指导用户正确使用
func reportGamepadSlots(slot int, before []int) {
	if slot < 0 {
		fmt.Println(" [Hardware] 虚拟手柄已挂载，但未能探测到其 XInput 槽位。")
		return
	}
	fmt.Printf(" [Hardware] 虚拟 Xbox 360 手柄已就绪，占用 XInput 槽位 %d。\n", slot)
	for _, s := range before {
		if s < slot {
			fmt.Printf(" [提示] 检测到物理手柄占用槽位 %d；若目标游戏只读取槽位 0，请在回放手柄脚本时暂时拔掉物理手柄。\n", s)
			break
		}
	}
}

// State 返回虚拟手柄当前完整状态
func (vg *VirtualGamepad) State() GamepadState {
	return vg.state
}

// Apply 提交一份完整的手柄状态到虚拟手柄
func (vg *VirtualGamepad) Apply(s GamepadState) bool {
	vg.state = s
	if vg.procUpdate == nil || vg.client == 0 || vg.target == 0 {
		return false
	}
	rep := xusb_report{
		wButtons:      s.Buttons,
		bLeftTrigger:  s.LT,
		bRightTrigger: s.RT,
		sThumbLX:      s.LX,
		sThumbLY:      s.LY,
		sThumbRX:      s.RX,
		sThumbRY:      s.RY,
	}
	r, _, _ := vg.procUpdate.Call(vg.client, vg.target, uintptr(unsafe.Pointer(&rep)))
	return uint32(r) == VIGEM_ERROR_NONE || uint32(r) == 0
}

// SetLeftStick 设置虚拟手柄左摇杆轴向 (lx, ly 范围 -32768 ~ 32767)
func (vg *VirtualGamepad) SetLeftStick(lx, ly int16) bool {
	s := vg.state
	s.LX, s.LY = lx, ly
	return vg.Apply(s)
}

// SetRightStick 设置虚拟手柄右摇杆轴向 (rx, ry 范围 -32768 ~ 32767)
func (vg *VirtualGamepad) SetRightStick(rx, ry int16) bool {
	s := vg.state
	s.RX, s.RY = rx, ry
	return vg.Apply(s)
}

// SendReport 发送完整手柄状态包 (摇杆、扳机、按键掩码)，与录制脚本 gp 帧字段一一对应
func (vg *VirtualGamepad) SendReport(wButtons uint16, lt, rt uint8, lx, ly, rx, ry int16) bool {
	return vg.Apply(GamepadState{
		Buttons: wButtons,
		LT:      lt, RT: rt,
		LX: lx, LY: ly, RX: rx, RY: ry,
	})
}

// Reset 将虚拟手柄全部通道归零 (摇杆回中、扳机松开、按键弹起)
func (vg *VirtualGamepad) Reset() bool {
	return vg.Apply(GamepadState{})
}

// Update 将当前内部状态同步给系统虚拟手柄
func (vg *VirtualGamepad) Update() bool {
	return vg.Apply(vg.state)
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

// ReleaseVirtualGamepad 若已挂载虚拟手柄，则全通道归零并释放 (供进程退出前调用)
func ReleaseVirtualGamepad() {
	if globalVirtualPad != nil {
		globalVirtualPad.Close()
	}
}

// Close 关闭并释放虚拟手柄
func (vg *VirtualGamepad) Close() {
	if vg.client != 0 && vg.target != 0 {
		vg.Reset()
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
