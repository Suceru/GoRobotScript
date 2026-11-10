package main

import (
	"GoRobotScript/Core/GoLua"
	"GoRobotScript/Core/GoPacker"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func registerAppMeta(L *lua.LState, name, ver string) {
	L.SetGlobal("AppInfo", L.NewFunction(func(L *lua.LState) int {
		t := L.NewTable()
		t.RawSetString("name", lua.LString(name))
		t.RawSetString("version", lua.LString(ver))
		L.Push(t)
		return 1
	}))

	L.SetGlobal("AppPackage", L.NewFunction(func(L *lua.LState) int {
		if L.GetTop() >= 1 {
			return 0
		}
		t := L.NewTable()
		t.RawSetString("name", lua.LString(name))
		t.RawSetString("version", lua.LString(ver))
		L.Push(t)
		return 1
	}))

	L.SetGlobal("PackageInfo", L.NewFunction(func(L *lua.LState) int {
		t := L.NewTable()
		t.RawSetString("name", lua.LString(name))
		t.RawSetString("version", lua.LString(ver))
		L.Push(t)
		return 1
	}))

	L.SetGlobal("PackageMode", L.NewFunction(func(L *lua.LState) int {
		return 0
	}))
}

// autoDetectScript 在无参数启动时推断要执行的脚本：
// 1) 与可执行文件同名的 .lua  2) main.lua  3) 目录内唯一的 .lua
func autoDetectScript(dir string) string {
	exePath, _ := os.Executable()
	exeBase := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))

	// 若可执行文件被改名为 <AppName>.exe，则同时尝试去掉 .pak 后缀的原始名
	candidates := []string{
		filepath.Join(dir, exeBase+".lua"),
		filepath.Join(dir, "main.lua"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}

	// 兜底：目录下只有一个 .lua 时直接用它
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".lua") {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

func main() {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	env := GoLua.NewEnvironment(exeDir)
	defer env.Close()

	_ = env.LoadAssets()

	// 内嵌负载：统一交由 GoPacker 读取（自动兼容 V1/V2 两种包格式）
	if pName, pVer, pKind, pScript, found := GoPacker.ReadEmbeddedPayload(); found {
		if pKind == GoPacker.PayloadKindScript {
			fmt.Fprintf(os.Stderr, "[Embedded Runtime Error] 本程序内嵌的是录制脚本(.script)，需要回放引擎。\n")
			fmt.Fprintf(os.Stderr, "  请使用 GoRunner.exe 运行，或改用 GoPacker 重新打包。\n")
			fmt.Println("Press Enter to exit...")
			var dummy string
			fmt.Scanln(&dummy)
			os.Exit(1)
		}
		registerAppMeta(env.L, pName, pVer)
		_ = os.Chdir(exeDir)

		if err := env.ExecuteString(string(pScript)); err != nil {
			fmt.Fprintf(os.Stderr, "[Execution Error] %v\n", err)
			fmt.Println("Press Enter to exit...")
			var dummy string
			fmt.Scanln(&dummy)
			os.Exit(1)
		}
		return
	}

	registerAppMeta(env.L, "GoLua.exe", "1.0.0")

	scriptpath := flag.String("script", "path", "Script Path")
	flag.Parse()

	// 无参数：优先执行同目录下的同名脚本，其次 main.lua，最后若目录内只有一个 .lua 则执行它。
	// 这样调试目录（GoPacker -unpak 的产物）可以直接双击启动。
	if len(os.Args) == 1 {
		target := autoDetectScript(exeDir)
		if target == "" {
			fmt.Fprintf(os.Stderr, "Usage: GoLua.exe [script.lua]\n")
			fmt.Fprintf(os.Stderr, "  或把 .lua 放在本程序同目录后直接双击运行。\n")
			return
		}
		fmt.Printf("[GoLua] 自动执行: %s\n", target)
		if err := env.ExecuteFile(target); err != nil {
			fmt.Fprintf(os.Stderr, "[Execution Error] %v\n", err)
			fmt.Println("按回车键退出...")
			var dummy string
			fmt.Scanln(&dummy)
			os.Exit(1)
		}
		return
	}

	target := os.Args[1]
	if target == "main" {
		target = "main.lua"
	} else if *scriptpath != "path" {
		target = *scriptpath
	}

	scriptDir := filepath.Dir(target)
	if scriptDir != "" && scriptDir != "." {
		scriptEnv := GoLua.NewEnvironment(scriptDir)
		defer scriptEnv.Close()
		_ = scriptEnv.LoadAssets()
		registerAppMeta(scriptEnv.L, filepath.Base(target), "1.0.0")
		if err := scriptEnv.ExecuteFile(target); err != nil {
			panic(err)
		}
		return
	}

	if err := env.ExecuteFile(target); err != nil {
		panic(err)
	}
}
