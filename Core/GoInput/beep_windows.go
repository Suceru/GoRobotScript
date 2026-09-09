package GoInput

import "syscall"

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	procBeep    = modKernel32.NewProc("Beep")
)

// Beep 调用 Windows 原生主板/系统音频提示音
// freq: 频率 (Hz), durationMs: 持续毫秒数
func Beep(freq uint32, durationMs uint32) {
	_, _, _ = procBeep.Call(uintptr(freq), uintptr(durationMs))
}

// 预设音效提示 (各状态声音清晰区别)
func SoundStart() {
	// 启动：两声清脆向上的短音
	go func() {
		Beep(1000, 100)
		Beep(1500, 150)
	}()
}

func SoundPause() {
	// 暂停：两声平直低音
	go func() {
		Beep(600, 120)
		Beep(600, 120)
	}()
}

func SoundResume() {
	// 继续：单声清脆高音
	go func() {
		Beep(1200, 150)
	}()
}

func SoundStop() {
	// 结束/停止：两声向下的低音
	go func() {
		Beep(800, 120)
		Beep(500, 200)
	}()
}
