package GoInput

import (
	"fmt"
	"sync/atomic"
	"time"

	hook "github.com/robotn/gohook"
)

var (
	procGetAsyncKeyState = modUser32.NewProc("GetAsyncKeyState")
)

// 录制期物理按键 (GetAsyncKeyState 虚拟键码)
const (
	VKPgUp   = 0x21 // 开始 / 暂停 录制
	VKPgDn   = 0x22 // 结束录制并保存
	VKEnd    = 0x23 // 识图预设槽后退
	VKHome   = 0x24 // 识图预设槽前进
	VKPause  = 0x13 // 识图开启 / 关闭 切换
)

// RegisterSystemHotkeys 启动高频物理硬件轮询 (GetAsyncKeyState)，
// 免疫一切游戏全屏、Hook 拦截与消息泵阻塞，保证无论在任何全屏游戏中均能 100% 触发热键！
//
//	PgUp  : 开始录制 / 暂停继续
//	PgDn  : 结束录制并保存 (同时退出识图与绘图框，预设不保留到下一轮)
//	Pause : 识图开启 / 关闭 切换
//	Home  : 短按 = 识图预设槽前进一个；长按 = 开关光标绘图框 (均需识图开启)
//	End   : 识图预设槽后退一个 (仅在识图开启时有效)
func (r *StreamlineRecorder) startGlobalHotkeyListener() {
	go func() {
		var lastPgUpState uint16
		var lastPgDnState uint16
		var lastPauseState uint16
		var lastHomeState uint16
		var lastEndState uint16

		// Home 长按判定
		var homeDownAt time.Time
		var homeConsumed bool

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for atomic.LoadInt32(&r.stopFlag) == 0 {
			<-ticker.C

			// 检查 PgUp (VK_PRIOR = 0x21)
			retUp, _, _ := procGetAsyncKeyState.Call(VKPgUp)
			currPgUp := uint16(retUp) & 0x8000
			if currPgUp != 0 && lastPgUpState == 0 {
				// PgUp 按下瞬间
				if atomic.LoadInt32(&r.startedFlag) == 0 {
					atomic.StoreInt32(&r.startedFlag, 1)
					r.mu.Lock()
					r.lastTime = time.Now()
					r.mu.Unlock()
					SoundStart()
					fmt.Println("\n>>> [系统热键: 开始录制] 正在精准捕捉操作与视角...")
					r.PrintVisionStatus()
				} else {
					if atomic.LoadInt32(&r.pauseFlag) == 0 {
						atomic.StoreInt32(&r.pauseFlag, 1)
						SoundPause()
						fmt.Println("\n||| [系统热键: 暂停录制] (再次按 PgUp 继续，按 PgDn 保存退出)")
					} else {
						atomic.StoreInt32(&r.pauseFlag, 0)
						r.mu.Lock()
						r.lastTime = time.Now()
						r.mu.Unlock()
						SoundResume()
						fmt.Println("\n>>> [系统热键: 继续录制] 正在恢复捕捉...")
					}
				}
			}
			lastPgUpState = currPgUp

			// 检查 Pause (VK_PAUSE = 0x13)：识图开启 / 关闭 切换
			retPause, _, _ := procGetAsyncKeyState.Call(VKPause)
			currPause := uint16(retPause) & 0x8000
			if currPause != 0 && lastPauseState == 0 {
				if atomic.LoadInt32(&r.startedFlag) == 1 {
					// 控制台：固定列宽就地覆盖刷新状态 (不换行、不撑长控制台)
					on := r.ToggleVision()
					r.PrintVisionStatus()
					// 脚本：单独一行标记，方便后续处理
					r.PushVisionMarker(on)
				}
			}
			lastPauseState = currPause

			// 检查 Home (VK_HOME = 0x24)：短按切槽 / 长按开关绘图框
			retHome, _, _ := procGetAsyncKeyState.Call(VKHome)
			currHome := uint16(retHome) & 0x8000
			switch {
			case currHome != 0 && lastHomeState == 0:
				// 按下：先不动，等松手才能区分短按与长按
				homeDownAt = time.Now()
				homeConsumed = false
			case currHome != 0 && !homeConsumed && time.Since(homeDownAt) >= VisionBoxLongPressMs*time.Millisecond:
				// 长按到点：开关绘图框（只触发一次）
				homeConsumed = true
				r.toggleVisionBoxByHotkey()
			case currHome == 0 && lastHomeState != 0:
				// 松开：未达长按阈值 => 短按切槽
				if !homeConsumed {
					r.switchVisionSlotByHotkey(1)
				}
				homeConsumed = false
			}
			lastHomeState = currHome

			// 检查 End (VK_END = 0x23)：预设槽后退一个
			retEnd, _, _ := procGetAsyncKeyState.Call(VKEnd)
			currEnd := uint16(retEnd) & 0x8000
			if currEnd != 0 && lastEndState == 0 {
				r.switchVisionSlotByHotkey(-1)
			}
			lastEndState = currEnd

			// 检查 PgDn (VK_NEXT = 0x22)
			retDn, _, _ := procGetAsyncKeyState.Call(VKPgDn)
			currPgDn := uint16(retDn) & 0x8000
			if currPgDn != 0 && lastPgDnState == 0 {
				// PgDn 按下瞬间：先退出识图功能 (不保留预设到下一轮)，再收尾保存
				if r.DisableVision() {
					r.PushVisionMarker(false)
				}
				r.PrintVisionStatus()
				fmt.Println()
				SoundStop()
				fmt.Println("\n■■■ [系统热键: 结束录制] 正在保存...")
				atomic.StoreInt32(&r.stopFlag, 1)
				hook.End()
				if r.file != nil {
					_ = r.file.Sync()
				}
				if r.DoneChan != nil {
					select {
					case r.DoneChan <- true:
					default:
					}
				}
				break
			}
			lastPgDnState = currPgDn
		}
	}()
}

// switchVisionSlotByHotkey 识图开启时切换预设槽 (Home 短按前进 / End 后退)；未开启时忽略
func (r *StreamlineRecorder) switchVisionSlotByHotkey(delta int) {
	if atomic.LoadInt32(&r.startedFlag) == 0 {
		return
	}
	if !r.VisionEnabled() {
		return
	}
	r.ShiftVisionSlot(delta)
	// 就地覆盖刷新状态行 + 脚本内单独成行标记
	r.PrintVisionStatus()
	r.PushVisionSlotMarker()
}

// toggleVisionBoxByHotkey 识图开启时长按 Home：开关光标绘图框
func (r *StreamlineRecorder) toggleVisionBoxByHotkey() {
	if atomic.LoadInt32(&r.startedFlag) == 0 {
		return
	}
	r.ToggleVisionBox()
	r.PrintVisionStatus()
}
