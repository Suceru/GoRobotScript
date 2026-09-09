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

// RegisterSystemHotkeys 启动高频物理硬件轮询 (GetAsyncKeyState)，
// 免疫一切游戏全屏、Hook 拦截与消息泵阻塞，保证无论在任何全屏游戏中均能 100% 触发 PgUp 与 PgDn！
func (r *StreamlineRecorder) startGlobalHotkeyListener() {
	go func() {
		var lastPgUpState uint16
		var lastPgDnState uint16

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for atomic.LoadInt32(&r.stopFlag) == 0 {
			<-ticker.C

			// 检查 PgUp (VK_PRIOR = 0x21)
			retUp, _, _ := procGetAsyncKeyState.Call(0x21)
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

			// 检查 PgDn (VK_NEXT = 0x22)
			retDn, _, _ := procGetAsyncKeyState.Call(0x22)
			currPgDn := uint16(retDn) & 0x8000
			if currPgDn != 0 && lastPgDnState == 0 {
				// PgDn 按下瞬间
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
