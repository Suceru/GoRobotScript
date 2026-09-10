package GoPacker

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestPayloadV3RoundTrip 校验带识图样本的内嵌负载可打包、可读回、可解包
func TestPayloadV3RoundTrip(t *testing.T) {
	dir := t.TempDir()
	vision := filepath.Join(dir, "A.vision")
	if err := os.MkdirAll(vision, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vision, "md-left-001.png"), []byte("PNGDATA-1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vision, "mu-right-002.png"), []byte("PNGDATA-2"), 0644); err != nil {
		t.Fatal(err)
	}

	assets, err := ZipDirToBytes(vision)
	if err != nil {
		t.Fatalf("打包识图样本失败: %v", err)
	}

	script := []byte("{\"op\":\"mv\",\"x\":1,\"y\":2}\n")
	out, err := BuildRunnerPayloadEx([]byte("RUNTIME-BYTES"), "A.exe", "1.2.3", PayloadKindScript, script, assets)
	if err != nil {
		t.Fatal(err)
	}

	packed := filepath.Join(dir, "A.exe")
	if err := os.WriteFile(packed, out, 0755); err != nil {
		t.Fatal(err)
	}

	name, ver, kind, gotScript, gotAssets, found := ReadEmbeddedPayloadFrom(packed)
	if !found {
		t.Fatal("未能读回内嵌负载")
	}
	if name != "A.exe" || ver != "1.2.3" || kind != PayloadKindScript {
		t.Fatalf("负载元信息错误: %s/%s/%s", name, ver, kind)
	}
	if string(gotScript) != string(script) {
		t.Fatalf("脚本内容错误: %q", string(gotScript))
	}
	if len(gotAssets) != len(assets) {
		t.Fatalf("样本负载长度不一致: %d != %d", len(gotAssets), len(assets))
	}

	// 解包到缓存目录并校验文件齐全
	cacheDir, err := ExtractEmbeddedAssets(gotAssets)
	if err != nil {
		t.Fatalf("解包识图样本失败: %v", err)
	}
	for _, f := range []string{"md-left-001.png", "mu-right-002.png"} {
		if _, err := os.Stat(filepath.Join(cacheDir, f)); err != nil {
			t.Fatalf("解包缺少文件 %s: %v", f, err)
		}
	}
	// 二次调用应命中缓存，返回同一目录
	again, err := ExtractEmbeddedAssets(gotAssets)
	if err != nil || again != cacheDir {
		t.Fatalf("解包缓存未命中: %s vs %s (%v)", again, cacheDir, err)
	}
	// 重复解包内容正确
	data, _ := os.ReadFile(filepath.Join(cacheDir, "md-left-001.png"))
	if string(data) != "PNGDATA-1" {
		t.Fatalf("解包内容错误: %q", string(data))
	}
	t.Logf("样本解包目录: %s", cacheDir)

	// 无样本时资源段长度为 0，仍然可正常读回
	out2, err := BuildRunnerPayloadEx([]byte("RUNTIME-BYTES"), "B.exe", "1.0.0", PayloadKindScript, script, nil)
	if err != nil {
		t.Fatal(err)
	}
	packed2 := filepath.Join(dir, "B.exe")
	if err := os.WriteFile(packed2, out2, 0755); err != nil {
		t.Fatal(err)
	}
	_, _, _, gotScript2, gotAssets2, found2 := ReadEmbeddedPayloadFrom(packed2)
	if !found2 || string(gotScript2) != string(script) || len(gotAssets2) != 0 {
		t.Fatalf("无样本负载往返失败: found=%v script=%q assets=%d", found2, string(gotScript2), len(gotAssets2))
	}

	// 旧版 V2 产物必须继续可读 (脚本为尾部剩余字节，无资源段)
	var v2 bytes.Buffer
	v2.Write([]byte("RUNTIME-BYTES"))
	writeLPString(&v2, "Old.exe")
	writeLPString(&v2, "0.9.0")
	writeLPString(&v2, PayloadKindScript)
	v2.Write(script)
	var v2Trailer bytes.Buffer
	v2Trailer.Write(PayloadMagic)
	_ = binary.Write(&v2Trailer, binary.LittleEndian, uint64(v2.Len()-len("RUNTIME-BYTES")))
	var v2Out bytes.Buffer
	v2Out.Write(v2.Bytes())
	v2Out.Write(v2Trailer.Bytes())
	packed3 := filepath.Join(dir, "Old.exe")
	if err := os.WriteFile(packed3, v2Out.Bytes(), 0755); err != nil {
		t.Fatal(err)
	}
	n3, _, k3, s3, a3, f3 := ReadEmbeddedPayloadFrom(packed3)
	if !f3 || n3 != "Old.exe" || k3 != PayloadKindScript || string(s3) != string(script) || len(a3) != 0 {
		t.Fatalf("V2 兼容读取失败: found=%v name=%s kind=%s script=%q assets=%d", f3, n3, k3, string(s3), len(a3))
	}
}

// TestVisionAssetDirConvention 目录约定必须与 Core/GoInput.VisionAssetDir 一致
func TestVisionAssetDirConvention(t *testing.T) {
	got := VisionAssetDir(filepath.Join("bin", "script", "VRView3D_2026-09-10_01-00-22.script"))
	want := filepath.Join("bin", "script", "VRView3D_2026-09-10_01-00-22.vision")
	if got != want {
		t.Fatalf("样本目录约定错误: %s != %s", got, want)
	}
}
