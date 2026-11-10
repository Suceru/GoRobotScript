package main

import (
	"GoRobotScript/Core/GoPacker"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findPath 定位构建产物。约定 GoPacker.exe 位于 <root>/bin/ 下：
//   findPath("GoRunner.exe")     -> <root>/bin/GoRunner.exe
//   findPath("Core/libs")        -> <root>/Core/libs （目录型目标）
//
// 优先匹配文件，其次才匹配目录，绝不返回与目标无关的路径。
func findPath(subPath string) string {
	selfExe, err := os.Executable()
	if err != nil {
		return ""
	}
	binDir := filepath.Dir(selfExe)
	rootDir := filepath.Dir(binDir)

	candidates := []string{
		filepath.Join(binDir, subPath),
		filepath.Join(rootDir, subPath),
		filepath.Join(rootDir, "bin", subPath),
	}

	// 1. 优先精确命中文件
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	// 2. 其次命中目录（例如 Core/libs）
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("==========================================================")
		fmt.Println("  GoPacker — 脚本与资源打包工具 (Core/GoPacker)")
		fmt.Println("==========================================================")
		fmt.Println(" 使用方式:")
		fmt.Println("   直接把 .lua 脚本拖放到本程序上，即可全自动打包为单文件 EXE。")
		fmt.Println("   若脚本同目录下存在含文件的 /Asset 文件夹，会自动打包为 .pak 并装载")
		fmt.Println("   （/Asset 为空目录时视为无资源，不生成 .pak）。")
		fmt.Println()
		fmt.Println(" 命令行参数:")
		fmt.Println("   GoPacker.exe <脚本.lua>             打包为单文件独立 EXE")
		fmt.Println("   GoPacker.exe <脚本.lua> -unpak      调试模式：不封装，平铺复制到 bin/run/<名称>/")
		fmt.Println("                                        内含 <名称>.exe、明文 .lua、Asset/、依赖 DLL")
		fmt.Println()
		fmt.Println(" 打包模式说明:")
		fmt.Println("   1. 【默认方式：单文件独立 EXE】")
		fmt.Println("      将脚本、/Asset 资源包 (.pak)、全套依赖 DLL 全部打包进单文件 EXE 中。")
		fmt.Println()
		fmt.Println("   2. 【独立文件夹模式】")
		fmt.Println("      生成包含 EXE、.pak 资源包 与同级依赖 DLL 的分发文件夹，自动引用同级 DLL。")
		fmt.Println("      Lua 声明标识（任选其一）:")
		fmt.Println("        -- @pack_mode folder       (推荐，注释声明)")
		fmt.Println("        -- @external_dll           (注释声明)")
		fmt.Println("        PackageInfo(\"MyBot.exe\", \"1.0.0\", \"folder\")")
		fmt.Println("        PackageMode(\"folder\")")
		fmt.Println("==========================================================")
		fmt.Println("按回车键退出...")
		var dummy string
		fmt.Scanln(&dummy)
		return
	}

	// 参数解析：支持 -unpak / --unpak / unpak 出现在任意位置
	unpackMode := false
	var scriptArg string
	for _, a := range os.Args[1:] {
		l := strings.ToLower(a)
		if l == "-unpak" || l == "--unpak" || l == "unpak" || l == "-unpack" || l == "--unpack" {
			unpackMode = true
			continue
		}
		if scriptArg == "" {
			scriptArg = a
		}
	}
	if scriptArg == "" {
		fmt.Fprintf(os.Stderr, "缺少脚本路径参数\n")
		os.Exit(1)
	}

	scriptPath, err := filepath.Abs(scriptArg)
	if err != nil {
		scriptPath = scriptArg
	}

	// 运行时模板固定取自 Core/GoRunner 模块的产物 GoRunner.exe（脚本将被焊入其中）
	runnerPath := findPath("GoRunner.exe")
	if runnerPath == "" {
		fmt.Fprintf(os.Stderr, "未找到运行时模板 GoRunner.exe（Core/GoRunner 的编译产物）\n")
		os.Exit(1)
	}

	// 单文件引导器载荷（Core/GoPacker/SingleLoader 的产物，以 .apppak 存放避免与资源包 .pak 混淆）
	singleLoaderPath := findPath("SingleLoader.apppak")
	if singleLoaderPath == "" {
		singleLoaderPath = findPath("SingleLoader.exe")
	}
	dllDir := findPath("Core/libs")
	if dllDir == "" {
		// 回退使用 bin/
		dllDir = filepath.Dir(runnerPath)
	}

	opts := GoPacker.PackOptions{
		ScriptPath:       scriptPath,
		BinDir:           filepath.Dir(runnerPath),
		RunnerPath:       runnerPath,
		SingleLoaderPath: singleLoaderPath,
		DllDir:           dllDir,
		UnpackMode:       unpackMode,
	}

	fmt.Println("==========================================================")
	if unpackMode {
		fmt.Println("  调试模式(-unpak) 开始:", scriptPath)
	} else {
		fmt.Println("  开始打包:", scriptPath)
	}
	fmt.Println("==========================================================")

	err = GoPacker.Pack(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] 处理失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("[SUCCESS] 完成！")
}
