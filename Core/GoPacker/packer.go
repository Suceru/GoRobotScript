// Package GoPacker packages Lua and recorded scripts into executables or distribution folders,
// bundling the script and optional image recognition assets (.pak) along with native dependencies.
package GoPacker

import (
	"GoRobotScript/Core/GoPak"
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	PayloadMagic   = []byte("GOKEYLUA_EMBEDDED_PAYLOAD_V1\x00")
	SingleExeMagic = []byte("GOKEYLUA_SINGLE_BUNDLE_V2\x00")
)

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

// BuildRunnerPayload attaches Lua script to runtime template binary.
func BuildRunnerPayload(runtimeExeBytes []byte, exeName string, version string, scriptBytes []byte) ([]byte, error) {
	var payloadBuf bytes.Buffer
	nameBytes := []byte(exeName)
	verBytes := []byte(version)

	_ = binary.Write(&payloadBuf, binary.LittleEndian, uint32(len(nameBytes)))
	payloadBuf.Write(nameBytes)

	_ = binary.Write(&payloadBuf, binary.LittleEndian, uint32(len(verBytes)))
	payloadBuf.Write(verBytes)

	payloadBuf.Write(scriptBytes)

	payloadRaw := payloadBuf.Bytes()
	payloadSize := uint64(len(payloadRaw))

	var trailerBuf bytes.Buffer
	trailerBuf.Write(PayloadMagic)
	_ = binary.Write(&trailerBuf, binary.LittleEndian, payloadSize)

	var result bytes.Buffer
	result.Write(runtimeExeBytes)
	result.Write(payloadRaw)
	result.Write(trailerBuf.Bytes())

	return result.Bytes(), nil
}

// PackOptions defines packing targets and locations.
type PackOptions struct {
	ScriptPath       string
	OutputDir        string
	GokeyLuaPath     string
	SingleLoaderPath string
	DllDir           string
}

// Pack packages the script according to its detected mode.
func Pack(opts PackOptions) error {
	scriptBytes, err := os.ReadFile(opts.ScriptPath)
	if err != nil {
		return fmt.Errorf("failed to read script: %w", err)
	}
	scriptBytes = bytes.TrimPrefix(scriptBytes, []byte("\xef\xbb\xbf"))

	meta := ParseMeta(opts.ScriptPath, string(scriptBytes))

	// Check if there is an Asset directory next to script
	scriptDir := filepath.Dir(opts.ScriptPath)
	assetDir := filepath.Join(scriptDir, "Asset")
	hasAssetDir := false
	if fi, err := os.Stat(assetDir); err == nil && fi.IsDir() {
		hasAssetDir = true
	}

	// Prepare .pak data if Asset dir exists
	var pakData []byte
	if hasAssetDir {
		pakTmp := filepath.Join(os.TempDir(), "temp_asset.pak")
		_, err := GoPak.PackAssetDir(assetDir, pakTmp)
		if err == nil {
			pakData, _ = os.ReadFile(pakTmp)
			_ = os.Remove(pakTmp)
		}
	}

	runtimeBytes, err := os.ReadFile(opts.GokeyLuaPath)
	if err != nil {
		return fmt.Errorf("failed to read GokeyLua template: %w", err)
	}

	runnerBytes, err := BuildRunnerPayload(runtimeBytes, meta.ExeName, meta.Version, scriptBytes)
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

	// Add all DLLs
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
