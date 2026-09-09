package main

import (
	"GoRobotScript/Core/GoPacker"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func findPath(subPath string) string {
	selfExe, _ := os.Executable()
	selfDir := filepath.Dir(selfExe)
	candidates := []string{
		filepath.Join(selfDir, subPath),
		filepath.Join(selfDir, "bin", subPath),
		filepath.Join(selfDir, "..", "bin", subPath),
		filepath.Join(selfDir, "..", "Core", "libs"),
		filepath.Join("D:\\Golang\\src\\suceru\\GoRobotScript", subPath),
		filepath.Join("D:\\Golang\\src\\suceru\\GoRobotScript\\bin", subPath),
		filepath.Join("D:\\Golang\\src\\suceru\\GoRobotScript\\Core\\libs"),
	}
	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil {
			if !fi.IsDir() || strings.Contains(cand, "libs") {
				return cand
			}
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("==========================================================")
		fmt.Println("  脚本与资源快速打包工具 (PackLua / GoPacker)")
		fmt.Println("==========================================================")
		fmt.Println(" 使用方式:")
		fmt.Println("   直接将 .lua 脚本拖放到本程序上，即可全自动打包！")
		fmt.Println("   若脚本同目录下存在 /Asset 文件夹，将自动打包为 .pak 并装载！")
		fmt.Println()
		fmt.Println(" 打包模式说明:")
		fmt.Println("   1. 【默认方式：单文件独立 EXE】")
		fmt.Println("      将脚本、/Asset 资源包 (.pak)、全套依赖 DLL 全部打包进单文件 EXE 中。")
		fmt.Println()
		fmt.Println("   2. 【第二种方式：独立文件夹模式】")
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

	scriptPath, err := filepath.Abs(os.Args[1])
	if err != nil {
		scriptPath = os.Args[1]
	}

	gokeyLuaPath := findPath("GokeyLua.exe")
	if gokeyLuaPath == "" {
		gokeyLuaPath = findPath("GoLua.exe")
	}
	if gokeyLuaPath == "" {
		fmt.Fprintf(os.Stderr, "未找到 GokeyLua.exe/GoLua.exe 运行时模板\n")
		os.Exit(1)
	}

	singleLoaderPath := findPath("SingleLoader.exe")
	dllDir := findPath("Core/libs")
	if dllDir == "" {
		// 回退使用 bin/
		dllDir = filepath.Dir(gokeyLuaPath)
	}

	opts := GoPacker.PackOptions{
		ScriptPath:       scriptPath,
		GokeyLuaPath:     gokeyLuaPath,
		SingleLoaderPath: singleLoaderPath,
		DllDir:           dllDir,
	}

	fmt.Println("==========================================================")
	fmt.Println("  开始打包:", scriptPath)
	fmt.Println("==========================================================")

	err = GoPacker.Pack(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] 打包失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("[SUCCESS] 打包完成！")
}
