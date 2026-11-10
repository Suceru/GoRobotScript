// loader_template.go
// 纯 Go 实现的独立单文件 EXE 引导器（无任何 CGO/外部 DLL 依赖，启动秒开）
//
// 核心机制（基础模块与定制内容分离）：
//   1. 包内条目按类型分成两份：
//        · 基础模块 base —— 全部 .dll（OpenCV / MinGW / ViGEmClient），同基线各包字节完全一致
//        · 定制内容 pack —— runner.exe（已焊入 Lua 脚本）与 assets.pak 等
//   2. 基础模块按「版本号 + 内容哈希」落到共享目录：
//        %LOCALAPPDATA%\GoRobotScript\base\b_<版本>_<哈希>\
//      同一基线的所有包共用一份，绝不重复解压；不同基线因哈希不同而天然并行共存，互不冲突。
//   3. 定制内容落到每包独立目录：
//        %LOCALAPPDATA%\GoRobotScript\runtime_<包哈希>\
//   4. 注入 DLL 搜索路径后启动 runner.exe。
//   5. 启动时顺带回收长期未使用的过期缓存目录。

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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var singleExeMagic = []byte("GOKEYLUA_SINGLE_BUNDLE_V2\x00")

// baseMetaName 基础模块版本标记条目名（与 GoPacker.BaseMetaName 保持一致）
const baseMetaName = "_base.meta"

// cacheKeepDays 未使用的缓存目录保留天数（可用环境变量 GOROBOT_CACHE_KEEP_DAYS 覆盖）
const cacheKeepDays = 7

type BundleInfo struct {
	ExeName string
	Version string
	ZipData []byte
}

type zipEntry struct {
	Name string
	Data []byte
	Mode os.FileMode
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

// partitionEntries 将包内条目拆分为「基础模块 .dll」与「定制内容」两部分
func partitionEntries(zipBytes []byte) (base []zipEntry, pack []zipEntry, version string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, nil, "", err
	}

	version = ""
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}

		name := filepath.Base(f.Name)
		switch {
		case name == baseMetaName:
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "version="); ok {
					version = strings.TrimSpace(v)
				}
			}
		case strings.HasSuffix(strings.ToLower(name), ".dll"):
			base = append(base, zipEntry{Name: name, Data: data, Mode: f.Mode()})
		default:
			pack = append(pack, zipEntry{Name: name, Data: data, Mode: f.Mode()})
		}
	}
	return base, pack, version, nil
}

// computeHash 对条目集合（名称 + 内容 + 附加标识）计算稳定哈希
func computeHash(entries []zipEntry, extra string) string {
	sorted := make([]zipEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	h := sha256.New()
	h.Write([]byte(extra))
	h.Write([]byte{0})
	for _, e := range sorted {
		h.Write([]byte(e.Name))
		h.Write([]byte{0})
		_ = binary.Write(h, binary.LittleEndian, uint64(len(e.Data)))
		h.Write(e.Data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// extractEntries 把条目写入目标目录；标记文件存在则视为已完成，跳过
func extractEntries(dir string, marker string, entries []zipEntry) error {
	markerPath := filepath.Join(dir, marker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		mode := e.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name), e.Data, mode); err != nil {
			return err
		}
	}
	return os.WriteFile(markerPath, []byte("ok"), 0644)
}

// sanitizeLabel 把版本号转成适合做目录名的形式
func sanitizeLabel(v string) string {
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// pruneStaleDirs 回收长期未使用的缓存目录（保留 keep 集合内的目录）
func pruneStaleDirs(rootDir string, keep map[string]bool) {
	days := cacheKeepDays
	if v := os.Getenv("GOROBOT_CACHE_KEEP_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	maxAge := time.Duration(days) * 24 * time.Hour

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() || keep[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			_ = os.RemoveAll(filepath.Join(rootDir, e.Name()))
		}
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

	baseEntries, packEntries, baseVersion, err := partitionEntries(bundle.ZipData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SingleBundle Error] 无法读取解压包: %v\n", err)
		os.Exit(1)
	}

	// 基础模块身份 = 版本号 + DLL 内容哈希；不同基线哈希不同，天然并行共存不冲突
	baseHash := computeHash(baseEntries, "base/"+baseVersion)
	baseDirName := fmt.Sprintf("b_%s_%s", sanitizeLabel(baseVersion), baseHash)

	// 定制内容身份 = runner.exe / assets.pak 等内容哈希
	packHash := computeHash(packEntries, "pack/"+bundle.Version)
	packDirName := "runtime_" + packHash

	tempBase := os.Getenv("LOCALAPPDATA")
	if tempBase == "" {
		tempBase = os.TempDir()
	}
	rootDir := filepath.Join(tempBase, "GoRobotScript")
	baseRoot := filepath.Join(rootDir, "base")
	baseDir := filepath.Join(baseRoot, baseDirName)
	packDir := filepath.Join(rootDir, packDirName)

	// 解压基础模块（同基线只解压一次，所有包共享）
	if len(baseEntries) > 0 {
		if err := extractEntries(baseDir, ".base_ready", baseEntries); err != nil {
			fmt.Fprintf(os.Stderr, "[SingleBundle Error] 基础模块解压失败: %v\n", err)
			os.Exit(1)
		}
	}

	// 解压定制内容
	if err := extractEntries(packDir, ".extracted", packEntries); err != nil {
		fmt.Fprintf(os.Stderr, "[SingleBundle Error] 定制内容解压失败: %v\n", err)
		os.Exit(1)
	}

	// 顺带回收过期缓存
	pruneStaleDirs(baseRoot, map[string]bool{baseDirName: true})
	pruneStaleDirs(rootDir, map[string]bool{packDirName: true, "base": true})

	// runner.exe 位于定制目录，依赖 DLL 位于共享基础目录
	runnerExe := filepath.Join(packDir, "runner.exe")
	if _, err := os.Stat(runnerExe); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[SingleBundle Error] 运行时缺失 runner.exe\n")
		os.Exit(1)
	}

	// 注入 DLL 搜索路径：SetDllDirectory 作用于本进程，PATH 会被子进程继承
	if len(baseEntries) > 0 {
		setDllDirectory(baseDir)
		_ = os.Setenv("PATH", baseDir+";"+os.Getenv("PATH"))
	}

	cmd := exec.Command(runnerExe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
