package GoInput

import (
	"sync/atomic"
	"time"
)

// PlaybackHotkeys 回放控制回调接口
type PlaybackHotkeys struct {
	OnStart  func()
	OnPause  func()
	OnResume func()
	OnStop   func()
}

// StartPlaybackHotkeyListener 启动 Windows 系统级全局热键监听 (专门服务于回放控制)
// 采用 GetAsyncKeyState 高频物理扫描，无论焦点处于任何全屏游戏或窗口，均能 100% 捕获 PgUp 与 PgDn
func StartPlaybackHotkeyListener(playingFlag, pauseFlag, stopFlag *int32, cb PlaybackHotkeys) {
	go func() {
		var lastPgUpState uint16
		var lastPgDnState uint16

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for atomic.LoadInt32(stopFlag) == 0 {
			<-ticker.C

			// 检查 PgUp (0x21)
			retUp, _, _ := procGetAsyncKeyState.Call(0x21)
			currPgUp := uint16(retUp) & 0x8000
			if currPgUp != 0 && lastPgUpState == 0 {
				if atomic.LoadInt32(playingFlag) == 0 {
					atomic.StoreInt32(playingFlag, 1)
					SoundStart()
					if cb.OnStart != nil {
						cb.OnStart()
					}
				} else {
					if atomic.LoadInt32(pauseFlag) == 0 {
						atomic.StoreInt32(pauseFlag, 1)
						SoundPause()
						if cb.OnPause != nil {
							cb.OnPause()
						}
					} else {
						atomic.StoreInt32(pauseFlag, 0)
						SoundResume()
						if cb.OnResume != nil {
							cb.OnResume()
						}
					}
				}
			}
			lastPgUpState = currPgUp

			// 检查 PgDn (0x22)
			retDn, _, _ := procGetAsyncKeyState.Call(0x22)
			currPgDn := uint16(retDn) & 0x8000
			if currPgDn != 0 && lastPgDnState == 0 {
				SoundStop()
				atomic.StoreInt32(stopFlag, 1)
				if cb.OnStop != nil {
					cb.OnStop()
				}
				break
			}
			lastPgDnState = currPgDn
		}
	}()
}
