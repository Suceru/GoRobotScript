// Package GoPacker packages Lua and recorded scripts into executables or distribution folders,
// bundling the script and optional image recognition assets (.pak) along with native dependencies.
package GoPacker

import (
	"GoRobotScript/Core/GoPak"
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// PayloadMagicV1 旧版内嵌负载尾标（无 kind 字段，恒为 Lua）
	PayloadMagicV1 = []byte("GOKEYLUA_EMBEDDED_PAYLOAD_V1\x00")
	// PayloadMagic 内嵌负载尾标 V2：名称/版本/类型/脚本
	PayloadMagic = []byte("GOKEYLUA_EMBEDDED_PAYLOAD_V2\x00")
	// PayloadMagicV3 内嵌负载尾标 V3：名称/版本/类型/脚本/附加资源(zip，识图样本)
	PayloadMagicV3 = []byte("GOKEYLUA_EMBEDDED_PAYLOAD_V3\x00")
	SingleExeMagic = []byte("GOKEYLUA_SINGLE_BUNDLE_V2\x00")
)

// 内嵌负载类型：决定运行时用哪种引擎执行
const (
	PayloadKindLua    = "lua"    // Lua 自动化脚本 → 走 Lua 引擎
	PayloadKindScript = "script" // 录制回放脚本 (JSON Lines) → 走回放引擎
)

// KindFromPath 根据脚本扩展名推断负载类型
func KindFromPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".script") {
		return PayloadKindScript
	}
	return PayloadKindLua
}

// writeLPString 写入 uint32 长度前缀字符串
func writeLPString(buf *bytes.Buffer, s string) {
	b := []byte(s)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(b)))
	buf.Write(b)
}

// readLPString 读取 uint32 长度前缀字符串
func readLPString(r *bytes.Reader) (string, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

// BaseMetaName 基础模块(DLL)版本标记在包内的条目名
const BaseMetaName = "_base.meta"

// BaseVersionFile 基础模块版本号文件名，位于 DLL 目录中
const BaseVersionFile = "base.version"

// DefaultBaseVersion 未提供 base.version 时使用的默认基础模块版本
const DefaultBaseVersion = "1.0.0"

// ReadBaseVersion 读取基础模块版本号 (优先 <dllDir>/base.version，其次默认值)
func ReadBaseVersion(dllDir string) string {
	candidates := []string{
		filepath.Join(dllDir, BaseVersionFile),
		filepath.Join(dllDir, "..", BaseVersionFile),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			v := strings.TrimSpace(string(data))
			v = strings.TrimPrefix(v, "\xef\xbb\xbf")
			if i := strings.IndexAny(v, "\r\n"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			if v != "" {
				return v
			}
		}
	}
	return DefaultBaseVersion
}

type ScriptMeta struct {
	ExeName    string
	Version    string
	ModeFolder bool // true: folder mode with external DLLs; false: standalone single EXE
}

// ParseMeta parses metadata and pack directives from script content.
func ParseMeta(scriptPath string, scriptContent string) ScriptMeta {
	base := filepath.Base(scriptPath)
	ext := filepath.Ext(base)
	defaultName := strings.TrimSuffix(base, ext) + ".exe"
	defaultVer := "1.0.0"

	meta := ScriptMeta{
		ExeName:    defaultName,
		Version:    defaultVer,
		ModeFolder: false,
	}

	// 1. Comment directives
	commentModeRe := regexp.MustCompile(`(?i)--\s*@?(?:pack_?mode|package_?mode|mode)\s*[:=]?\s*["']?([a-zA-Z0-9_-]+)["']?`)
	if m := commentModeRe.FindStringSubmatch(scriptContent); len(m) >= 2 {
		val := strings.ToLower(strings.TrimSpace(m[1]))
		if val == "folder" || val == "dir" || val == "directory" || val == "external" {
			meta.ModeFolder = true
		}
	}
	commentTagRe := regexp.MustCompile(`(?i)--\s*@?(?:folder|dir|directory|external_?dlls?)\b`)
	if commentTagRe.MatchString(scriptContent) {
		meta.ModeFolder = true
	}

	// 2. PackageMode(...)
	modeRe := regexp.MustCompile(`(?i)(?:PackageMode|SetPackageMode|PackMode)\s*\(\s*["']([^"']+)["']\s*\)`)
	if m := modeRe.FindStringSubmatch(scriptContent); len(m) >= 2 {
		val := strings.ToLower(strings.TrimSpace(m[1]))
		if val == "folder" || val == "dir" || val == "directory" || val == "external" {
			meta.ModeFolder = true
		}
	}

	// 3. PackageInfo(...)
	info3Re := regexp.MustCompile(`(?i)(?:PackageInfo|AppPackage|SetPackageInfo|SetAppInfo)\s*\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)
	if m := info3Re.FindStringSubmatch(scriptContent); len(m) >= 4 {
		meta.ExeName = strings.TrimSpace(m[1])
		meta.Version = strings.TrimSpace(m[2])
		val := strings.ToLower(strings.TrimSpace(m[3]))
		if val == "folder" || val == "dir" || val == "directory" || val == "external" {
			meta.ModeFolder = true
		}
	} else {
		info2Re := regexp.MustCompile(`(?i)(?:PackageInfo|AppPackage|SetPackageInfo|SetAppInfo)\s*\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)
		if m := info2Re.FindStringSubmatch(scriptContent); len(m) >= 3 {
			meta.ExeName = strings.TrimSpace(m[1])
			meta.Version = strings.TrimSpace(m[2])
		} else {
			info1Re := regexp.MustCompile(`(?i)(?:PackageInfo|AppPackage|SetPackageInfo|SetAppInfo)\s*\(\s*["']([^"']+)["']\s*\)`)
			if m := info1Re.FindStringSubmatch(scriptContent); len(m) >= 2 {
				meta.ExeName = strings.TrimSpace(m[1])
			}
		}
	}

	if !strings.HasSuffix(strings.ToLower(meta.ExeName), ".exe") {
		meta.ExeName += ".exe"
	}

	return meta
}

// BuildRunnerPayload 把脚本负载附加到运行时模板二进制尾部。
// kind 取值见 PayloadKindLua / PayloadKindScript，运行时据此选择 Lua 引擎或回放引擎。
func BuildRunnerPayload(runtimeExeBytes []byte, exeName string, version string, kind string, scriptBytes []byte) ([]byte, error) {
	return BuildRunnerPayloadEx(runtimeExeBytes, exeName, version, kind, scriptBytes, nil)
}

// BuildRunnerPayloadEx 在 BuildRunnerPayload 基础上附带一份资源负载 (zip 字节)。
// 录制脚本的识图样本目录 (<脚本名>.vision) 就装在这里，运行时解包到缓存目录后
// 供识图对齐读取；assets 为空时资源段长度为 0。统一输出 V3 布局，
// 旧版 V1/V2 产物仍由读取端兼容。
func BuildRunnerPayloadEx(runtimeExeBytes []byte, exeName string, version string, kind string, scriptBytes []byte, assets []byte) ([]byte, error) {
	var payloadBuf bytes.Buffer
	writeLPString(&payloadBuf, exeName)
	writeLPString(&payloadBuf, version)
	writeLPString(&payloadBuf, kind)
	_ = binary.Write(&payloadBuf, binary.LittleEndian, uint32(len(scriptBytes)))
	payloadBuf.Write(scriptBytes)
	_ = binary.Write(&payloadBuf, binary.LittleEndian, uint32(len(assets)))
	payloadBuf.Write(assets)

	payloadRaw := payloadBuf.Bytes()
	payloadSize := uint64(len(payloadRaw))

	var trailerBuf bytes.Buffer
	trailerBuf.Write(PayloadMagicV3)
	_ = binary.Write(&trailerBuf, binary.LittleEndian, payloadSize)

	var result bytes.Buffer
	result.Write(runtimeExeBytes)
	result.Write(payloadRaw)
	result.Write(trailerBuf.Bytes())

	return result.Bytes(), nil
}

// ReadEmbeddedPayload 从当前运行的可执行文件尾部读取内嵌脚本负载。
// 返回 (程序名, 版本号, 负载类型, 脚本字节, 是否命中)；非打包产物时 found 为 false。
// 兼容 V1（无 kind 字段，恒按 Lua）、V2（含 kind）与 V3（含附加资源）三种格式。
func ReadEmbeddedPayload() (exeName string, version string, kind string, script []byte, found bool) {
	name, ver, k, s, _, f := ReadEmbeddedPayloadEx()
	return name, ver, k, s, f
}

// ReadEmbeddedPayloadEx 在 ReadEmbeddedPayload 基础上返回 V3 内嵌资源负载 (zip 字节)。
func ReadEmbeddedPayloadEx() (exeName string, version string, kind string, script []byte, assets []byte, found bool) {
	selfPath, err := os.Executable()
	if err != nil {
		return "", "", "", nil, nil, false
	}
	return ReadEmbeddedPayloadFrom(selfPath)
}

// ReadEmbeddedPayloadFrom 从指定文件尾部读取内嵌负载 (便于校验打包产物)
func ReadEmbeddedPayloadFrom(selfPath string) (exeName string, version string, kind string, script []byte, assets []byte, found bool) {
	no := func() (string, string, string, []byte, []byte, bool) { return "", "", "", nil, nil, false }

	f, err := os.Open(selfPath)
	if err != nil {
		return no()
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return no()
	}
	fileSize := stat.Size()

	// V1 / V2 / V3 尾标等长，可统一处理
	magicLen := int64(len(PayloadMagic))
	trailerLen := magicLen + 8
	if fileSize < trailerLen {
		return no()
	}

	if _, err := f.Seek(-trailerLen, io.SeekEnd); err != nil {
		return no()
	}
	trailer := make([]byte, trailerLen)
	if _, err := io.ReadFull(f, trailer); err != nil {
		return no()
	}

	isV1 := bytes.Equal(trailer[:magicLen], PayloadMagicV1)
	isV2 := bytes.Equal(trailer[:magicLen], PayloadMagic)
	isV3 := bytes.Equal(trailer[:magicLen], PayloadMagicV3)
	if !isV1 && !isV2 && !isV3 {
		return no()
	}

	payloadSize := int64(binary.LittleEndian.Uint64(trailer[magicLen:]))
	payloadOffset := fileSize - trailerLen - payloadSize
	if payloadOffset < 0 {
		return no()
	}

	if _, err := f.Seek(payloadOffset, io.SeekStart); err != nil {
		return no()
	}
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(f, payload); err != nil {
		return no()
	}

	r := bytes.NewReader(payload)
	name, err := readLPString(r)
	if err != nil {
		return no()
	}
	ver, err := readLPString(r)
	if err != nil {
		return no()
	}

	kind = PayloadKindLua // V1 恒为 Lua
	if isV2 || isV3 {
		kind, err = readLPString(r)
		if err != nil {
			return no()
		}
		if kind == "" {
			kind = PayloadKindLua
		}
	}

	if isV3 {
		var scriptLen uint32
		if err := binary.Read(r, binary.LittleEndian, &scriptLen); err != nil {
			return no()
		}
		script = make([]byte, scriptLen)
		if _, err := io.ReadFull(r, script); err != nil {
			return no()
		}
		var assetLen uint32
		if err := binary.Read(r, binary.LittleEndian, &assetLen); err != nil {
			return no()
		}
		if assetLen > 0 {
			assets = make([]byte, assetLen)
			if _, err := io.ReadFull(r, assets); err != nil {
				return no()
			}
		}
		return name, ver, kind, script, assets, true
	}

	script, err = io.ReadAll(r)
	if err != nil {
		return no()
	}

	return name, ver, kind, script, nil, true
}

// ---------------------------------------------------------------------------
// 识图样本包 (zip) 打包 / 解包
// ---------------------------------------------------------------------------

// ZipDirToBytes 把目录整体压缩为 zip 字节 (相对路径保留子目录结构)
func ZipDirToBytes(dir string) ([]byte, error) {
	if !dirHasAnyFile(dir) {
		return nil, fmt.Errorf("directory is empty: %s", dir)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:   filepath.ToSlash(rel),
			Method: zip.Deflate,
		})
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnzipToDir 把 zip 字节解压到目标目录
func UnzipToDir(data []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, entry := range zr.File {
		name := filepath.Clean(filepath.FromSlash(entry.Name))
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue // 防目录穿越
		}
		dest := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ExtractEmbeddedAssets 把内嵌资源负载解压到缓存目录并返回该目录。
// 目录名由内容哈希决定，重复运行不会重复解压，也不会互相覆盖。
func ExtractEmbeddedAssets(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	sum := sha1.Sum(data)
	name := "vision_" + hex.EncodeToString(sum[:8])

	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	root = filepath.Join(root, "GoRobotScript")
	dir := filepath.Join(root, name)

	if _, err := os.Stat(filepath.Join(dir, ".ready")); err == nil {
		return dir, nil
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := UnzipToDir(data, dir); err != nil {
		return "", err
	}
	_ = os.WriteFile(filepath.Join(dir, ".ready"), []byte("ok"), 0644)
	return dir, nil
}

// VisionAssetDir 识图样本目录约定 (与 Core/GoInput.VisionAssetDir 保持一致)：
// 与脚本同级、同名的 <脚本名>.vision 目录。
// 例：bin/script/A.script 的样本目录为 bin/script/A.vision/
func VisionAssetDir(scriptPath string) string {
	base := strings.TrimSuffix(filepath.Base(scriptPath), filepath.Ext(scriptPath))
	return filepath.Join(filepath.Dir(scriptPath), base+".vision")
}

// dirHasAnyFile 递归判断目录下是否存在任何实际文件 (空目录或仅含空子目录 => false)
func dirHasAnyFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if dirHasAnyFile(filepath.Join(dir, e.Name())) {
				return true
			}
			continue
		}
		return true
	}
	return false
}

// PackOptions defines packing targets and locations.
type PackOptions struct {
	ScriptPath       string
	OutputDir        string
	BinDir           string // bin/ 目录：单文件模式取运行时模板与 DLL，unpak 模式输出到 <BinDir>/run/
	RunnerPath       string // Core/GoRunner 产物 (GoRunner.exe)，作为内嵌运行时模板
	SingleLoaderPath string
	DllDir           string
	UnpackMode       bool // true: 不封装任何内容，平铺复制到 <BinDir>/run/<AppName>/ 便于调试
}

// copyFile 复制单个文件
func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0644
	}
	return os.WriteFile(dst, data, mode)
}

// copyDirTree 递归复制目录
func copyDirTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirTree(sp, dp); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(sp, dp, 0644); err != nil {
			return err
		}
	}
	return nil
}

// PackUnpack 调试模式（-unpak）：不做任何封装，把运行必需的文件平铺复制到
// <BinDir>/run/<AppName>/ 下，便于直接修改脚本反复调试。
//
// 依据脚本类型产出不同形态：
//
//	.lua 脚本：
//	  <AppName>.exe   由 bin/GoLua.apppak 复制并改名的 Lua 解释器
//	  <脚本>.lua      明文脚本        Asset/  明文资源        *.dll  依赖
//	  双击 <AppName>.exe 即自动执行同目录脚本。
//
//	.script 录制脚本：
//	  GoRunner.exe    由 bin/GoRunner.exe 复制（回放引擎，不焊入脚本）
//	  <脚本>.script   明文录制脚本，可直接编辑后重放
//	  <脚本>.vision/  识图样本目录（录制时鼠标按下/松开处截取的模板图）
//	  *.dll           依赖
//	  用法：GoRunner.exe <脚本>.script
func PackUnpack(opts PackOptions) error {
	scriptBytes, err := os.ReadFile(opts.ScriptPath)
	if err != nil {
		return fmt.Errorf("failed to read script: %w", err)
	}
	scriptBytes = bytes.TrimPrefix(scriptBytes, []byte("\xef\xbb\xbf"))
	meta := ParseMeta(opts.ScriptPath, string(scriptBytes))
	appName := strings.TrimSuffix(meta.ExeName, ".exe")
	kind := KindFromPath(opts.ScriptPath)

	binDir := opts.BinDir
	if binDir == "" {
		binDir = filepath.Dir(opts.RunnerPath)
	}
	outDir := filepath.Join(binDir, "run", appName)
	_ = os.RemoveAll(outDir)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create unpack dir: %w", err)
	}

	// 1. 运行时载荷
	var runtimeSrc, runtimeDst string
	if kind == PayloadKindScript {
		// 录制脚本用回放引擎 GoRunner.exe
		runtimeSrc = opts.RunnerPath
		runtimeDst = filepath.Join(outDir, "GoRunner.exe")
		if _, err := os.Stat(runtimeSrc); err != nil {
			return fmt.Errorf("未找到回放引擎 bin/GoRunner.exe（请先执行 build.ps1 生成）")
		}
	} else {
		// Lua 脚本用解释器载荷 GoLua.apppak，并改名为可执行程序
		runtimeSrc = filepath.Join(binDir, "GoLua.apppak")
		runtimeDst = filepath.Join(outDir, meta.ExeName)
		if _, err := os.Stat(runtimeSrc); err != nil {
			// 兼容：若尚未改名为 .apppak，则退回 GoLua.exe
			alt := filepath.Join(binDir, "GoLua.exe")
			if _, err2 := os.Stat(alt); err2 == nil {
				runtimeSrc = alt
			} else {
				return fmt.Errorf("未找到 Lua 运行时载荷 bin/GoLua.apppak（请先执行 build.ps1 生成）")
			}
		}
	}
	if err := copyFile(runtimeSrc, runtimeDst, 0755); err != nil {
		return fmt.Errorf("failed to copy runtime: %w", err)
	}

	// 2. 明文脚本（保持原文件名）
	scriptName := filepath.Base(opts.ScriptPath)
	if err := os.WriteFile(filepath.Join(outDir, scriptName), scriptBytes, 0644); err != nil {
		return fmt.Errorf("failed to copy script: %w", err)
	}

	// 3. 明文资源目录（仅在含文件时；录制脚本无 Asset，改为随带识图样本目录）
	if kind == PayloadKindLua {
		scriptDir := filepath.Dir(opts.ScriptPath)
		assetSrc := filepath.Join(scriptDir, "Asset")
		if dirHasAnyFile(assetSrc) {
			if err := copyDirTree(assetSrc, filepath.Join(outDir, "Asset")); err != nil {
				return fmt.Errorf("failed to copy Asset: %w", err)
			}
		}
	} else {
		// 识图样本：<脚本名>.vision 平移到调试目录旁，回放引擎按同名约定直接读取
		visionSrc := VisionAssetDir(opts.ScriptPath)
		if dirHasAnyFile(visionSrc) {
			if err := copyDirTree(visionSrc, filepath.Join(outDir, filepath.Base(visionSrc))); err != nil {
				return fmt.Errorf("failed to copy vision samples: %w", err)
			}
		}
	}

	// 4. 全套依赖 DLL
	if opts.DllDir != "" {
		entries, err := os.ReadDir(opts.DllDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".dll") {
					continue
				}
				_ = copyFile(filepath.Join(opts.DllDir, entry.Name()),
					filepath.Join(outDir, entry.Name()), 0644)
			}
		}
	}

	fmt.Printf("[GoPacker] 调试模式(未封装)已完成: %s\n", outDir)
	if kind == PayloadKindScript {
		fmt.Printf("           运行方式: GoRunner.exe %s\n", scriptName)
		fmt.Printf("           修改 %s 后直接重跑，无需重新打包\n", scriptName)
	} else {
		fmt.Printf("           双击 %s 即可运行（自动执行同目录 %s）\n", meta.ExeName, scriptName)
		fmt.Printf("           修改 %s 或 Asset/ 后直接重跑，无需重新打包\n", scriptName)
	}
	return nil
}

// Pack packages the script according to its detected mode.
func Pack(opts PackOptions) error {
	if opts.UnpackMode {
		return PackUnpack(opts)
	}

	scriptBytes, err := os.ReadFile(opts.ScriptPath)
	if err != nil {
		return fmt.Errorf("failed to read script: %w", err)
	}
	scriptBytes = bytes.TrimPrefix(scriptBytes, []byte("\xef\xbb\xbf"))

	meta := ParseMeta(opts.ScriptPath, string(scriptBytes))
	kind := KindFromPath(opts.ScriptPath)
	scriptDir := filepath.Dir(opts.ScriptPath)

	// 仅 Lua 脚本会装配资源包：录制脚本(.script)是键鼠/手柄动作回放数据，
	// 不含 Lua 逻辑、也不使用 Asset 资源。
	var pakData []byte
	if kind == PayloadKindLua {
		assetDir := filepath.Join(scriptDir, "Asset")
		if dirHasAnyFile(assetDir) {
			pakTmp := filepath.Join(os.TempDir(), "temp_asset.pak")
			if _, err := GoPak.PackAssetDir(assetDir, pakTmp); err == nil {
				pakData, _ = os.ReadFile(pakTmp)
				_ = os.Remove(pakTmp)
			}
		}
	}

	// 录制脚本的识图样本 (<脚本名>.vision) 压缩后随负载内嵌，
	// 运行时解包到缓存目录供识图对齐使用，保证单文件产物依然只有一个 exe。
	var assetData []byte
	if kind == PayloadKindScript {
		visionDir := VisionAssetDir(opts.ScriptPath)
		if dirHasAnyFile(visionDir) {
			assetData, err = ZipDirToBytes(visionDir)
			if err != nil {
				fmt.Printf("[GoPacker] 警告: 识图样本打包失败，将不带样本发布: %v\n", err)
				assetData = nil
			} else {
				fmt.Printf("[GoPacker] 识图样本已内嵌: %s (%d 字节)\n", visionDir, len(assetData))
			}
		}
	}

	runtimeBytes, err := os.ReadFile(opts.RunnerPath)
	if err != nil {
		return fmt.Errorf("failed to read GoRunner template: %w", err)
	}

	runnerBytes, err := BuildRunnerPayloadEx(runtimeBytes, meta.ExeName, meta.Version, kind, scriptBytes, assetData)
	if err != nil {
		return err
	}

	outDir := opts.OutputDir
	if outDir == "" {
		outDir = scriptDir
	}

	if meta.ModeFolder {
		// 文件夹模式：已知的脚本和资源包打包进 exe，依赖和打包后的 exe 放进同一个文件夹，依赖自动被引用
		distDir := filepath.Join(outDir, strings.TrimSuffix(meta.ExeName, ".exe")+"_Dist")
		_ = os.MkdirAll(distDir, 0755)

		// 写入主程序 exe
		exePath := filepath.Join(distDir, meta.ExeName)
		if err := os.WriteFile(exePath, runnerBytes, 0755); err != nil {
			return err
		}

		// 如果有资源，写入 .pak 到 dist 文件夹中供自动加载
		if len(pakData) > 0 {
			pakPath := filepath.Join(distDir, strings.TrimSuffix(meta.ExeName, ".exe")+".pak")
			_ = os.WriteFile(pakPath, pakData, 0644)
		}

		// 拷贝外部依赖 DLL 到同一个文件夹
		if opts.DllDir != "" {
			entries, err := os.ReadDir(opts.DllDir)
			if err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".dll") {
						src := filepath.Join(opts.DllDir, entry.Name())
						dst := filepath.Join(distDir, entry.Name())
						data, err := os.ReadFile(src)
						if err == nil {
							_ = os.WriteFile(dst, data, 0644)
						}
					}
				}
			}
		}
		fmt.Printf("[GoPacker] Folder mode completed: %s\n", distDir)
		return nil
	}

	// 默认模式：脚本、资源包、依赖全部打包进单文件 exe
	if opts.SingleLoaderPath == "" {
		return fmt.Errorf("single loader template path is required for standalone mode")
	}

	loaderBytes, err := os.ReadFile(opts.SingleLoaderPath)
	if err != nil {
		return fmt.Errorf("failed to read SingleLoader template: %w", err)
	}

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	// Add runner.exe
	rw, err := zw.CreateHeader(&zip.FileHeader{Name: "runner.exe", Method: zip.Deflate})
	if err != nil {
		return err
	}
	if _, err := rw.Write(runnerBytes); err != nil {
		return err
	}

	// Add .pak if present
	if len(pakData) > 0 {
		pw, err := zw.CreateHeader(&zip.FileHeader{Name: "assets.pak", Method: zip.Deflate})
		if err == nil {
			_, _ = pw.Write(pakData)
		}
	}

	// Add all DLLs + base 版本标记（基础模块仅由 DLL 构成，供 loader 解压到共享 base 目录）
	if opts.DllDir != "" {
		entries, err := os.ReadDir(opts.DllDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".dll") {
					src := filepath.Join(opts.DllDir, entry.Name())
					c, err := os.ReadFile(src)
					if err == nil {
						dw, err := zw.CreateHeader(&zip.FileHeader{Name: entry.Name(), Method: zip.Deflate})
						if err == nil {
							_, _ = dw.Write(c)
						}
					}
				}
			}
		}
		// 写入基础模块版本标记，供 loader 生成 base_<版本>_<哈希> 目录名
		if mw, err := zw.CreateHeader(&zip.FileHeader{Name: BaseMetaName, Method: zip.Deflate}); err == nil {
			_, _ = mw.Write([]byte("version=" + ReadBaseVersion(opts.DllDir) + "\n"))
		}
	}

	_ = zw.Close()

	// Append bundle to SingleLoader
	var bundleBuf bytes.Buffer
	nameBytes := []byte(meta.ExeName)
	verBytes := []byte(meta.Version)

	_ = binary.Write(&bundleBuf, binary.LittleEndian, uint32(len(nameBytes)))
	bundleBuf.Write(nameBytes)
	_ = binary.Write(&bundleBuf, binary.LittleEndian, uint32(len(verBytes)))
	bundleBuf.Write(verBytes)
	bundleBuf.Write(zipBuf.Bytes())

	bundleRaw := bundleBuf.Bytes()
	var trailerBuf bytes.Buffer
	trailerBuf.Write(SingleExeMagic)
	_ = binary.Write(&trailerBuf, binary.LittleEndian, uint64(len(bundleRaw)))

	outExePath := filepath.Join(outDir, meta.ExeName)
	outF, err := os.OpenFile(outExePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer outF.Close()

	if _, err := outF.Write(loaderBytes); err != nil {
		return err
	}
	if _, err := outF.Write(bundleRaw); err != nil {
		return err
	}
	if _, err := outF.Write(trailerBuf.Bytes()); err != nil {
		return err
	}

	fmt.Printf("[GoPacker] Standalone single EXE completed: %s\n", outExePath)
	return nil
}
