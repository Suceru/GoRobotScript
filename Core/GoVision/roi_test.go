package GoVision

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"gocv.io/x/gocv"
)

// synthScreen 生成一张确定性噪声画面 (无随机源，可重复)
func synthScreen(w, h int) gocv.Mat {
	data := make([]byte, w*h*3)
	seed := uint32(0x51F0A3)
	for i := range data {
		seed = seed*1664525 + 1013904223
		data[i] = uint8(seed >> 16)
	}
	mat, err := gocv.NewMatFromBytes(h, w, gocv.MatTypeCV8UC3, data)
	if err != nil {
		panic(err)
	}
	return mat
}

// synthGradientScreen 生成平滑渐变画面：
// 颜色随坐标单调变化，因此任意位置的 3x3 颜色描述子都是局部唯一的，
// 适合用来验证颜色卷积匹配的定位精度（纯噪声做 3x3 池化后信息太少，本就不可判）。
func synthGradientScreen(w, h int) gocv.Mat {
	data := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			data[i] = uint8(60 + 140*x/w)   // B
			data[i+1] = uint8(30 + 180*y/h) // G
			data[i+2] = uint8(20 + 200*x/w) // R
		}
	}
	mat, err := gocv.NewMatFromBytes(h, w, gocv.MatTypeCV8UC3, data)
	if err != nil {
		panic(err)
	}
	return mat
}

func writePatch(t *testing.T, screen gocv.Mat, dir, name string, x, y, w, h int) string {
	t.Helper()
	r := screen.Region(image.Rect(x, y, x+w, y+h))
	defer r.Close()
	p := filepath.Join(dir, name+".png")
	if !gocv.IMWrite(p, r) {
		t.Fatalf("write patch failed: %s", p)
	}
	return p
}

// TestMatchTemplateROIAbsolute ROI 匹配必须返回**绝对**坐标，且与全屏搜索结果一致
func TestMatchTemplateROIAbsolute(t *testing.T) {
	screen := synthScreen(600, 400)
	defer screen.Close()
	dir := t.TempDir()
	p := writePatch(t, screen, dir, "t", 200, 150, 32, 32)

	tpl, err := LoadTemplate(p)
	if err != nil {
		t.Fatal(err)
	}
	defer tpl.Close()

	opts := MatchOptions{MinScale: 1.0, MaxScale: 1.0, ScaleStep: 0.05}

	full, err := MatchTemplate(screen, tpl, opts)
	if err != nil {
		t.Fatal(err)
	}
	if full.X != 200 || full.Y != 150 {
		t.Fatalf("全屏匹配位置错误: (%d,%d)", full.X, full.Y)
	}

	// ROI 包含目标 => 结果必须与全屏一致 (绝对坐标)
	roi := image.Rect(150, 100, 320, 260)
	sub, err := MatchTemplateROI(screen, tpl, roi, opts)
	if err != nil {
		t.Fatal(err)
	}
	if sub.X != 200 || sub.Y != 150 || sub.Score > 0.001 {
		t.Fatalf("ROI 匹配结果错误: (%d,%d) score=%.5f", sub.X, sub.Y, sub.Score)
	}

	// ROI 不含目标 => 结果必须落在 ROI 内 (且分数明显更差)
	far, err := MatchTemplateROI(screen, tpl, image.Rect(400, 280, 560, 380), opts)
	if err != nil {
		t.Fatal(err)
	}
	if far.X < 400 || far.Y < 280 {
		t.Fatalf("ROI 结果越出 ROI 范围: (%d,%d)", far.X, far.Y)
	}
	if far.Score <= sub.Score {
		t.Fatalf("ROI 外不应得到相同质量的结果: %.5f vs %.5f", far.Score, sub.Score)
	}

	// 颜色卷积 ROI 同样必须返回**绝对**坐标（用渐变画面，颜色描述子局部唯一）
	gscreen := synthGradientScreen(600, 400)
	defer gscreen.Close()
	gp := writePatch(t, gscreen, dir, "g", 200, 150, 32, 32)
	gtpl, err := LoadTemplate(gp)
	if err != nil {
		t.Fatal(err)
	}
	defer gtpl.Close()

	gfull, err := ColorConvolutionMatch(gscreen, gtpl, 3, 3, opts)
	if err != nil {
		t.Fatal(err)
	}
	if absInt(gfull.X-200) > 4 || absInt(gfull.Y-150) > 4 {
		t.Fatalf("颜色卷积全屏匹配偏差过大: (%d,%d)", gfull.X, gfull.Y)
	}
	cc, err := ColorConvolutionMatchROI(gscreen, gtpl, roi, 3, 3, opts)
	if err != nil {
		t.Fatal(err)
	}
	if absInt(cc.X-200) > 4 || absInt(cc.Y-150) > 4 {
		t.Fatalf("颜色卷积 ROI 结果偏差过大: (%d,%d) 期望约 (200,150)", cc.X, cc.Y)
	}
	// 颜色卷积 ROI 结果与全屏结果一致（绝对坐标）。
	// 渐变画面在局部近乎平坦，分数存在并列，允许 1~2 像素的并列抖动。
	if absInt(cc.X-gfull.X) > 2 || absInt(cc.Y-gfull.Y) > 2 {
		t.Fatalf("颜色卷积 ROI(%d,%d) 与全屏(%d,%d) 不一致", cc.X, cc.Y, gfull.X, gfull.Y)
	}

	// ROI 完全越界 => 报错，调用方据此回退
	if _, err := MatchTemplateROI(screen, tpl, image.Rect(2000, 2000, 2100, 2100), opts); err == nil {
		t.Fatal("越界 ROI 应当报错")
	}
}

// TestMatchMulti 多图一次匹配：各自独立 ROI，全部返回绝对坐标，单项失败不影响其它
func TestMatchMulti(t *testing.T) {
	screen := synthScreen(800, 500)
	defer screen.Close()
	dir := t.TempDir()
	aPath := writePatch(t, screen, dir, "a", 120, 90, 32, 32)
	bPath := writePatch(t, screen, dir, "b", 520, 360, 64, 64)

	ta, err := LoadTemplate(aPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ta.Close()
	tb, err := LoadTemplate(bPath)
	if err != nil {
		t.Fatal(err)
	}
	defer tb.Close()

	opts := MatchOptions{MinScale: 1.0, MaxScale: 1.0, ScaleStep: 0.05}
	items := []MultiMatchItem{
		{Template: ta, ROI: image.Rect(80, 50, 260, 220), Options: opts},
		{Template: tb, ROI: image.Rect(480, 320, 700, 470), Options: opts},
		// 第三项：ROI 故意越界 => 该项报错，不影响前两项
		{Template: ta, ROI: image.Rect(5000, 5000, 5100, 5100), Options: opts},
	}
	res := MatchMulti(screen, items)
	if len(res) != 3 {
		t.Fatalf("结果数量应为 3，实际 %d", len(res))
	}
	if res[0].Err != nil || res[0].X != 120 || res[0].Y != 90 {
		t.Fatalf("多图#1 结果错误: (%d,%d) err=%v", res[0].X, res[0].Y, res[0].Err)
	}
	if res[1].Err != nil || res[1].X != 520 || res[1].Y != 360 {
		t.Fatalf("多图#2 结果错误: (%d,%d) err=%v", res[1].X, res[1].Y, res[1].Err)
	}
	if res[2].Err == nil {
		t.Fatal("越界 ROI 的单项应当报错")
	}
}

// TestCenterROIClamp 中心 ROI 必须按画面裁剪
func TestCenterROIClamp(t *testing.T) {
	screen := synthScreen(400, 300)
	defer screen.Close()

	r := CenterROI(200, 150, 32, 32, 64, screen)
	if r.Min.X != 136 || r.Min.Y != 86 || r.Max.X != 296 || r.Max.Y != 246 {
		t.Fatalf("中心 ROI 计算错误: %v", r)
	}
	// 贴边时裁剪到画面内
	e := CenterROI(2, 2, 32, 32, 64, screen)
	if e.Min.X != 0 || e.Min.Y != 0 || e.Max.X > 400 || e.Max.Y > 300 {
		t.Fatalf("边缘 ROI 未正确裁剪: %v", e)
	}
	// 完全越界 => 空
	if z := CenterROI(5000, 5000, 32, 32, 64, screen); z.Dx() > 0 {
		t.Fatalf("越界 ROI 应为空: %v", z)
	}
	_ = os.Stdout
}
