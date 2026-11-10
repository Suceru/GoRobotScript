package GoInput

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoRobotScript/Core/GoVision"
)

// TestVisionStatusText 状态文本必须与约定的固定格式完全一致
func TestVisionStatusText(t *testing.T) {
	cases := []struct {
		on   bool
		slot int
		want string
	}{
		{false, 1, "识图关闭"},
		{false, 6, "识图关闭"},
		{true, 1, "识图开启-图像32*32p"},
		{true, 2, "识图开启-图像64*64p"},
		{true, 3, "识图开启-图像128*128p"},
		{true, 4, "识图开启-颜色32*32p"},
		{true, 5, "识图开启-颜色64*64p"},
		{true, 6, "识图开启-颜色128*128p"},
	}
	for _, c := range cases {
		if got := VisionStatusText(c.on, c.slot); got != c.want {
			t.Fatalf("VisionStatusText(%v,%d) = %q, 期望 %q", c.on, c.slot, got, c.want)
		}
	}
}

// TestVisionPresetCycle 6 个预设槽：先 图像(32/64/128)，再 颜色(32/64/128)
func TestVisionPresetCycle(t *testing.T) {
	wantSizes := []int{32, 64, 128, 32, 64, 128}
	wantModes := []string{"image", "image", "image", "color", "color", "color"}
	for i := 1; i <= VisionPresetCount; i++ {
		p := VisionPresetAt(i)
		if p.Size != wantSizes[i-1] || p.Mode != wantModes[i-1] {
			t.Fatalf("槽位%d = %d/%s，期望 %d/%s", i, p.Size, p.Mode, wantSizes[i-1], wantModes[i-1])
		}
	}
}

// TestVisionAssetDir 样本目录约定
func TestVisionAssetDir(t *testing.T) {
	got := VisionAssetDir(filepath.Join("bin", "script", "A.script"))
	want := filepath.Join("bin", "script", "A.vision")
	if got != want {
		t.Fatalf("VisionAssetDir = %s，期望 %s", got, want)
	}
}

// TestVisionBoxGatingAndSize 绘图框：只在识图开启时可用，尺寸跟随预设槽
// (只验证状态机，不创建真实窗口)
func TestVisionBoxGatingAndSize(t *testing.T) {
	r := &StreamlineRecorder{visionSlot: 1, visionSeq: make(map[string]int)}
	r.visionBox = newVisionBox(func() int { return VisionPresetAt(r.VisionSlot()).Size })

	// 识图关闭时不允许开框
	if r.ToggleVisionBox() {
		t.Fatal("识图未开启时不应能打开绘图框")
	}
	if r.SetVisionBox(true) {
		t.Fatal("识图未开启时 SetVisionBox(true) 应被拒绝")
	}
	if r.VisionBoxEnabled() {
		t.Fatal("绘图框不应处于开启状态")
	}

	// 识图开启后可用
	r.visionOn = true
	r.visionBox.setEnabled(true)
	if !r.VisionBoxEnabled() {
		t.Fatal("绘图框应处于开启状态")
	}

	// 框尺寸必须跟随预设槽 (槽1=32, 槽2=64)
	if got := r.visionBox.sizeFn(); got != 32 {
		t.Fatalf("槽位1 框尺寸应为 32，实际 %d", got)
	}
	r.ShiftVisionSlot(1)
	if got := r.visionBox.sizeFn(); got != 64 {
		t.Fatalf("槽位2 框尺寸应为 64，实际 %d", got)
	}

	// 关闭识图必须同时收起绘图框
	r.ToggleVision()
	if r.VisionBoxEnabled() {
		t.Fatal("关闭识图后绘图框必须一并收起")
	}

	// 长按阈值必须是合理的"长按"量级
	if VisionBoxLongPressMs < 300 || VisionBoxLongPressMs > 1000 {
		t.Fatalf("长按阈值不合理: %d ms", VisionBoxLongPressMs)
	}
}

// TestVisionStatusLineShowsBox PrintVisionStatus 必须带绘图框状态、就地覆盖且占满固定列宽
func TestVisionStatusLineShowsBox(t *testing.T) {
	r := &StreamlineRecorder{visionSlot: 1, visionSeq: make(map[string]int)}
	r.visionBox = newVisionBox(func() int { return 32 })
	r.visionOn = true

	read := func() string {
		old := os.Stdout
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = pw
		r.PrintVisionStatus()
		_ = pw.Close()
		os.Stdout = old
		buf := make([]byte, 1024)
		n, _ := pr.Read(buf)
		_ = pr.Close()
		return string(buf[:n])
	}

	off := read()
	if !strings.Contains(off, "绘图框:关") {
		t.Fatalf("状态行应显示 绘图框:关，实际 %q", off)
	}
	r.visionBox.setEnabled(true)
	on := read()
	if !strings.Contains(on, "绘图框:开") {
		t.Fatalf("状态行应显示 绘图框:开，实际 %q", on)
	}
	for _, line := range []string{off, on} {
		if !strings.HasPrefix(line, "\r") {
			t.Fatalf("状态行必须以 \\r 开头 (就地覆盖)，实际 %q", line)
		}
		body := strings.TrimPrefix(line, "\r")
		if strings.Contains(body, "\n") {
			t.Fatal("状态行不得换行")
		}
		if w := visionDisplayWidth(body); w != visionStatusFieldWidth {
			t.Fatalf("状态行显示宽度应为 %d，实际 %d (%q)", visionStatusFieldWidth, w, body)
		}
	}
}

// TestVisionBoxIsOutsidePatch 绘图框必须画在采样区域**外侧**：
// 窗口比采样区大两圈边框，挖空后的内圈正好等于采样区域。
// 这条不变量保证红框绝不会被截进样本图里污染模板。
func TestVisionBoxIsOutsidePatch(t *testing.T) {
	for _, size := range []int{32, 64, 128} {
		for _, at := range [][2]int{{1000, 700}, {0, 0}, {2559, 1439}} {
			patch, _, _, ok := GoVision.PatchGeometry(at[0], at[1], size)
			if !ok {
				t.Fatalf("采样区域求解失败: %v size=%d", at, size)
			}
			line := VisionBoxLineDefault
			win := visionBoxWindowRect(patch, line)

			// 窗口 = 采样区 + 四周各一条边框
			if win.Dx() != size+2*line || win.Dy() != size+2*line {
				t.Fatalf("窗口尺寸应为 %d，实际 %dx%d (size=%d)", size+2*line, win.Dx(), win.Dy(), size)
			}
			// 关键：挖空后剩下的内圈必须正好等于采样区域 —— 红框完全在采样区之外
			hole := win.Inset(line)
			if hole != patch {
				t.Fatalf("内圈必须等于采样区域: hole=%v patch=%v", hole, patch)
			}
			// 边框区域与采样区域不得有任何重叠
			if win.Overlaps(hole) && hole != patch {
				t.Fatalf("边框与采样区重叠: win=%v hole=%v", win, hole)
			}
			if patch.Overlaps(image.Rect(win.Min.X, win.Min.Y, win.Min.X+line, win.Max.Y)) {
				t.Fatal("左侧边框侵入了采样区域")
			}
			if patch.Overlaps(image.Rect(win.Max.X-line, win.Min.Y, win.Max.X, win.Max.Y)) {
				t.Fatal("右侧边框侵入了采样区域")
			}
		}
	}
	// 默认外观：红色 / 3 像素
	if VisionBoxLineDefault != 3 {
		t.Fatalf("默认线宽应为 3 像素，实际 %d", VisionBoxLineDefault)
	}
	if VisionBoxColorDefault != 0x000000FF {
		t.Fatalf("默认颜色应为纯红 COLORREF 0x000000FF，实际 0x%08X", VisionBoxColorDefault)
	}
}

// TestVisionSampleNaming 采样命名与序号规则：状态+按键+3 位序号，各组合独立计数
func TestVisionSampleNaming(t *testing.T) {
	r := &StreamlineRecorder{
		visionOn:    true,
		visionSlot:  1,
		visionSeq:   make(map[string]int),
		visionDir:   t.TempDir(),
		visionQueue: make(chan visionCaptureJob, 64),
	}

	type sample struct {
		op, btn string
		want    string
	}
	seq := []sample{
		{"md", "left", "md-left-001"},
		{"md", "right", "md-right-001"},
		{"mu", "left", "mu-left-001"},
		{"md", "left", "md-left-002"},
		{"mu", "right", "mu-right-001"},
		{"md", "left", "md-left-003"},
	}
	for _, s := range seq {
		name, iox, ioy, size, mode, ok := r.visionTakeSample(s.op, s.btn, 1000, 1000)
		if !ok {
			t.Fatalf("识图开启时采样应成功: %s-%s", s.op, s.btn)
		}
		if name != s.want {
			t.Fatalf("样本命名 %q，期望 %q", name, s.want)
		}
		if size != 32 || mode != VisionModeImage {
			t.Fatalf("槽位1 应为 32p/图像，实际 %dp/%s", size, mode)
		}
		if iox != 16 || ioy != 16 {
			t.Fatalf("屏幕中央采样偏移应为 16/16，实际 %d/%d", iox, ioy)
		}
	}

	// 切换槽位后：尺寸与匹配方式同步变化 (槽位4 = 颜色 32p)
	p := r.ShiftVisionSlot(3) // 1 -> 4
	if p.Slot != 4 || p.Size != 32 || p.Mode != VisionModeColor {
		t.Fatalf("槽位前进结果错误: %+v", p)
	}
	_, _, _, size, mode, _ := r.visionTakeSample("md", "left", 1000, 1000)
	if size != 32 || mode != VisionModeColor {
		t.Fatalf("槽位4 应为 32p/颜色，实际 %dp/%s", size, mode)
	}
	if p2 := r.ShiftVisionSlot(-3); p2.Slot != 1 {
		t.Fatalf("槽位回退结果错误: %+v", p2)
	}
	// 环形切换
	if p3 := r.ShiftVisionSlot(-1); p3.Slot != VisionPresetCount {
		t.Fatalf("槽位应环形回绕到 %d，实际 %d", VisionPresetCount, p3.Slot)
	}

	// 关闭识图后不再采样
	r.visionOn = false
	if _, _, _, _, _, ok := r.visionTakeSample("md", "left", 1000, 1000); ok {
		t.Fatal("识图关闭时不应采样")
	}
}
