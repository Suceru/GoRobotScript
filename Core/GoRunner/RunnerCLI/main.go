package main

import (
	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoRunner"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// 当无参数直接双击运行时，默认进入智能录制待机模式（自动保存至 exe 目录的 /script 下，以视角模式命名）
	if len(os.Args) < 2 {
		is3D := true
		rec, err := GoInput.StartSmartRecording("", is3D)
		if err != nil {
			fmt.Fprintf(os.Stderr, "启动录制失败: %v\n", err)
			var dummy string
			fmt.Scanln(&dummy)
			os.Exit(1)
		}

		absPath, _ := filepath.Abs(rec.OutFile)
		fmt.Println("==========================================================")
		fmt.Println("  GoRunner 自动化工具 (双击启动 - 默认录制模式)")
		fmt.Println("==========================================================")
		fmt.Printf(" -> 默认保存位置: %s\n", absPath)
		fmt.Println()
		fmt.Println(" 快捷键与音频提示:")
		fmt.Println("   • [PgUp] 首次按下:  开始录制 (双声上扬提示音)")
		fmt.Println("   • [PgUp] 再次按下:  暂停 / 继续 切换 (暂停双低音 / 继续单高音)")
		fmt.Println("   • [PgDn] 按一下:    结束录制并自动保存 (两声下降提示音)")
		fmt.Println()
		fmt.Println(" 常用命令行扩展:")
		fmt.Println("   • 回放执行:   GoRunner.exe <脚本路径.script | 脚本路径.lua>")
		fmt.Println("   • 快速回放:   GoRunner.exe -f <脚本路径.script>")
		fmt.Println("   • 倍速回放:   GoRunner.exe -t 1.5 <脚本路径.script>")
		fmt.Println("==========================================================")
		fmt.Println("请切换至目标游戏/窗口，随时按下 [PgUp] 即可开始录制...")

		// 等待全局热键 PgDn 结束信号
		<-rec.DoneChan
		rec.Stop()
		fmt.Printf("\n[SUCCESS] 录制完成！脚本已保存到:\n%s\n", absPath)
		fmt.Println("按回车键关闭窗口...")
		var dummy string
		fmt.Scanln(&dummy)
		return
	}

	arg1 := os.Args[1]
	lowerArg := strings.ToLower(arg1)
	if lowerArg == "record" || lowerArg == "-record" || lowerArg == "record-3d" || lowerArg == "-record-3d" {
		outPath := ""
		if len(os.Args) > 2 {
			outPath = os.Args[2]
		}
		is3D := true // 默认集成 3D/VR 相对视角捕获能力

		fmt.Println("==========================================================")
		fmt.Println(" [GoRunner 智能精简录制模式 (已集成 3D/VR 视角支持)]")
		fmt.Println(" 操作快捷键与音频提示:")
		fmt.Println("   • [PgUp] 首次按:  开始录制 (两声上扬提示音)")
		fmt.Println("   • [PgUp] 再次按:  暂停 / 继续 切换 (暂停双低音 / 继续单高音)")
		fmt.Println("   • [PgDn] 按一下:  结束录制并自动保存 (两声下降提示音)")
		fmt.Println("==========================================================")
		rec, err := GoInput.StartSmartRecording(outPath, is3D)
		if err != nil {
			fmt.Fprintf(os.Stderr, "启动录制失败: %v\n", err)
			os.Exit(1)
		}
		absPath, _ := filepath.Abs(rec.OutFile)
		fmt.Printf(" -> 脚本保存路径: %s\n", absPath)
		fmt.Println("请切换至目标窗口，按下 [PgUp] 开始录制...")

		select {
		case <-rec.DoneChan:
		}
		rec.Stop()
		fmt.Println("[SUCCESS] 录制完成已保存！可直接使用 GoRunner.exe 回放。")
		return
	}

	// 解析播放参数: -f (快速播放到底), -t (播放倍率)
	fs := flag.NewFlagSet("runner", flag.ExitOnError)
	fastMode := fs.Bool("f", false, "无需按键确认，直接执行播放到结束")
	speedScale := fs.Float64("t", 1.0, "指定播放时间缩放倍率 (例如 1.2 加速, 0.8 减速)")

	_ = fs.Parse(os.Args[1:])
	remainingArgs := fs.Args()

	target := ""
	if len(remainingArgs) > 0 {
		target = remainingArgs[0]
	} else {
		target = arg1
	}

	opts := GoRunner.PlayOptions{
		FastMode:   *fastMode,
		SpeedScale: *speedScale,
	}

	runner := GoRunner.NewRunner(filepath.Dir(target))
	err := runner.RunScriptFileWithOptions(target, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
		os.Exit(1)
	}
}
