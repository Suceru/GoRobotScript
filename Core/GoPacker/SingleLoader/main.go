// loader_template.go
// 纯 Go 实现的独立单文件 EXE 引导器（无任何 CGO/外部 DLL 依赖，启动秒开）
// 核心机制：
// 1. 启动时检查当前目录或临时缓存目录中是否已存在内嵌的 DLL 依赖（OpenCV & MinGW）
// 2. 如果不存在，自动从自身尾部的 Embedded Payload 中解压释放全套 DLL（仅首次释放，后续秒启）
// 3. 将 DLL 目录注入 PATH 并调用 SetDllDirectoryW，然后自执行内嵌的 GokeyLua 运行时并传入内嵌 Lua 脚本
// 4. 用户拿到的是真正的【纯单文件独立 EXE】，无需安装任何环境，双击即可直接运行！

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var singleExeMagic = []byte("GOKEYLUA_SINGLE_BUNDLE_V2\x00")

type BundleInfo struct {
	ExeName string
	Version string
	ZipData []byte
}

func readBundle() (*BundleInfo, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(selfPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()
	magicLen := int64(len(singleExeMagic))
	trailerLen := magicLen + 8

	if fileSize < trailerLen {
		return nil, fmt.Errorf("file too small")
	}

	if _, err := f.Seek(-trailerLen, io.SeekEnd); err != nil {
		return nil, err
	}

	buf := make([]byte, trailerLen)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}

	if !bytes.Equal(buf[:magicLen], singleExeMagic) {
		return nil, fmt.Errorf("no single bundle magic found")
	}

	payloadSize := binary.LittleEndian.Uint64(buf[magicLen:])
	payloadOffset := fileSize - trailerLen - int64(payloadSize)
	if payloadOffset < 0 {
		return nil, fmt.Errorf("invalid payload offset")
	}

	if _, err := f.Seek(payloadOffset, io.SeekStart); err != nil {
		return nil, err
	}

	payloadBytes := make([]byte, payloadSize)
	if _, err := io.ReadFull(f, payloadBytes); err != nil {
		return nil, err
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

	zipBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return &BundleInfo{
		ExeName: string(nameBytes),
		Version: string(verBytes),
		ZipData: zipBytes,
	}, nil
}

func setDllDirectory(dir string) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setDllDirProc := kernel32.NewProc("SetDllDirectoryW")
	ptr, err := syscall.UTF16PtrFromString(dir)
	if err == nil {
		_, _, _ = setDllDirProc.Call(uintptr(unsafe.Pointer(ptr)))
	}
}

func main() {
	bundle, err := readBundle()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SingleBundle Error] 无法加载打包数据: %v\n", err)
		var dummy string
		fmt.Scanln(&dummy)
		os.Exit(1)
	}

	// Calculate cache directory based on bundle hash to prevent re-extracting
	h := sha256.New()
	h.Write(bundle.ZipData)
	hashStr := fmt.Sprintf("%x", h.Sum(nil))[:16]

	tempBase := os.Getenv("LOCALAPPDATA")
	if tempBase == "" {
		tempBase = os.TempDir()
	}
	runtimeDir := filepath.Join(tempBase, "GoRobotScript", "runtime_"+hashStr)
	markerFile := filepath.Join(runtimeDir, ".extracted")

	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		_ = os.MkdirAll(runtimeDir, 0755)
		zipReader, err := zip.NewReader(bytes.NewReader(bundle.ZipData), int64(len(bundle.ZipData)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SingleBundle Error] 无法读取解压包: %v\n", err)
			os.Exit(1)
		}

		for _, file := range zipReader.File {
			targetPath := filepath.Join(runtimeDir, file.Name)
			if file.FileInfo().IsDir() {
				_ = os.MkdirAll(targetPath, 0755)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
			if err != nil {
				continue
			}
			rc, err := file.Open()
			if err == nil {
				_, _ = io.Copy(outFile, rc)
				_ = rc.Close()
			}
			_ = outFile.Close()
		}
		_ = os.WriteFile(markerFile, []byte("ok"), 0644)
	}

	// Configure DLL lookup directory
	setDllDirectory(runtimeDir)
	_ = os.Setenv("PATH", runtimeDir+";"+os.Getenv("PATH"))

	// Path to the packaged runner inside runtimeDir
	runnerExe := filepath.Join(runtimeDir, "runner.exe")
	if _, err := os.Stat(runnerExe); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[SingleBundle Error] 运行时缺失 runner.exe\n")
		os.Exit(1)
	}

	// Working directory is the current directory where the user launched the standalone EXE
	cmd := exec.Command(runnerExe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
