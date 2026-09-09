package main

import (
	"GoRobotScript/Core/GoLua"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"
)

var payloadMagic = []byte("GOKEYLUA_EMBEDDED_PAYLOAD_V1\x00")

type EmbeddedPayload struct {
	ExeName string
	Version string
	Script  string
}

func readEmbeddedPayload() (*EmbeddedPayload, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(exePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()
	magicLen := int64(len(payloadMagic))
	trailerLen := magicLen + 8

	if fileSize < trailerLen {
		return nil, nil
	}

	if _, err := f.Seek(-trailerLen, io.SeekEnd); err != nil {
		return nil, err
	}

	buf := make([]byte, trailerLen)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}

	if !bytes.Equal(buf[:magicLen], payloadMagic) {
		return nil, nil
	}

	payloadSize := binary.LittleEndian.Uint64(buf[magicLen:])
	payloadOffset := fileSize - trailerLen - int64(payloadSize)
	if payloadOffset < 0 {
		return nil, fmt.Errorf("corrupt embedded payload size: %d", payloadSize)
	}

	if _, err := f.Seek(payloadOffset, io.SeekStart); err != nil {
		return nil, err
	}

	payloadBytes := make([]byte, payloadSize)
	if _, err := io.ReadFull(f, payloadBytes); err != nil {
		return nil, err
	}

	if len(payloadBytes) < 8 {
		return nil, fmt.Errorf("invalid payload format")
	}

	r := bytes.NewReader(payloadBytes)
	var nameLen uint32
	if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
		return nil, err
	}
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return nil, err
	}

	var verLen uint32
	if err := binary.Read(r, binary.LittleEndian, &verLen); err != nil {
		return nil, err
	}
	verBytes := make([]byte, verLen)
	if _, err := io.ReadFull(r, verBytes); err != nil {
		return nil, err
	}

	scriptBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return &EmbeddedPayload{
		ExeName: string(nameBytes),
		Version: string(verBytes),
		Script:  string(scriptBytes),
	}, nil
}

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

func main() {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	env := GoLua.NewEnvironment(exeDir)
	defer env.Close()

	_ = env.LoadAssets()

	payload, err := readEmbeddedPayload()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Embedded Runtime Error] %v\n", err)
		os.Exit(1)
	}

	if payload != nil {
		registerAppMeta(env.L, payload.ExeName, payload.Version)
		_ = os.Chdir(exeDir)

		if err := env.ExecuteString(payload.Script); err != nil {
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

	if len(os.Args) == 1 {
		fmt.Fprintf(os.Stderr, "Usage: GoLua.exe [script.lua]\n")
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
