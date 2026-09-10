package GoRunner

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoVision"

	"gocv.io/x/gocv"
)

// ---------------------------------------------------------------------------
// 识图对齐验证 (确定性)：
// 用一张合成的"画面"制造两个样本，模拟录制坐标与回放坐标不一致的情况，
// 检查 关键点定位 / 路径旋转拉伸 / 段间连续性 / 识图关闭断链 四项行为。
// ---------------------------------------------------------------------------

// makeSyntheticScreen 生成一张确定性的 BGR 噪声画面 (无随机源，可重复)
func makeSyntheticScreen(w, h int) gocv.Mat {
	data := make([]byte, w*h*3)
	seed := uint32(0x12345678)
	next := func() uint8 {
		seed = seed*1664525 + 1013904223
		return uint8(seed >> 16)
	}
	for i := 0; i < len(data); i++ {
		data[i] = next()
	}
	mat, err := gocv.NewMatFromBytes(h, w, gocv.MatTypeCV8UC3, data)
	if err != nil {
		panic(err)
	}
	return mat
}

// extractPatch 从合成画面截取样例并写入 PNG，模拟录制期采样
func extractPatch(t *testing.T, screen gocv.Mat, dir, name string, x, y, size int) {
	t.Helper()
	rect := screen.Region(imageRect(x, y, size, size))
	defer rect.Close()
	dest := filepath.Join(dir, name+".png")
	if !gocv.IMWrite(dest, rect) {
		t.Fatalf("写入样本失败: %s", dest)
	}
}

func TestVisionAlignPipeline(t *testing.T) {
	const (
		screenW, screenH = 900, 640
		size             = 32
	)
	screen := makeSyntheticScreen(screenW, screenH)
	defer screen.Close()

	dir := t.TempDir()

	// 关键点 1：录制期在 (300,220) 采样；录制坐标写在 (240,280) —— 即录制与回放差了 (-60,+60)
	// 关键点 2：录制期在 (700,470) 采样；录制坐标写在 (620,410) —— 偏移 (-80,-60)
	// 两个偏移不同 => 对齐需要旋转 + 拉伸，而不是单纯平移
	type kp struct {
		captureX, captureY int
		recordX, recordY   int
		name               string
	}
	kps := []kp{
		{300, 220, 240, 280, "md-left-001"},
		{700, 470, 620, 410, "mu-left-001"},
	}
	for _, k := range kps {
		extractPatch(t, screen, dir, k.name, k.captureX, k.captureY, size)
	}

	// 构造脚本帧：识图开启 -> 若干 mv 中间路径 -> md(锚点1) -> 中间路径 -> mu(锚点2)
	mid := 16 // 样本内光标偏移 = 半边
	rawPath := func(fx, fy, tx, ty int, steps int) [][2]int {
		var pts [][2]int
		for i := 1; i < steps; i++ {
			r := float64(i) / float64(steps)
			pts = append(pts, [2]int{
				int(math.Round(float64(fx) + (float64(tx)-float64(fx))*r)),
				int(math.Round(float64(fy) + (float64(ty)-float64(fy))*r)),
			})
		}
		return pts
	}

	on := true
	var frames []playFrame
	add := func(a GoInput.RecordAction) {
		frames = append(frames, playFrame{action: &a})
	}
	add(GoInput.RecordAction{Op: "vision", Von: &on, Vslot: 1, Vmode: "image", Vsize: size})
	add(GoInput.RecordAction{Op: "mv", X: 100, Y: 100})
	for _, p := range rawPath(240, 280, kps[0].recordX, kps[0].recordY, 6) {
		add(GoInput.RecordAction{Op: "mv", X: p[0], Y: p[1]})
	}
	mdIdx := len(frames)
	add(GoInput.RecordAction{
		Op: "md", Btn: "left", X: kps[0].recordX, Y: kps[0].recordY,
		Vi: kps[0].name, Vx: mid, Vy: mid, Vmode: "image", Vsize: size,
	})
	segPath := rawPath(kps[0].recordX, kps[0].recordY, kps[1].recordX, kps[1].recordY, 21)
	for _, p := range segPath {
		add(GoInput.RecordAction{Op: "mv", X: p[0], Y: p[1]})
	}
	muIdx := len(frames)
	add(GoInput.RecordAction{
		Op: "mu", Btn: "left", X: kps[1].recordX, Y: kps[1].recordY,
		Vi: kps[1].name, Vx: mid, Vy: mid, Vmode: "image", Vsize: size,
	})

	plan := buildVisionPlan(frames)
	if len(plan.anchors) != 2 {
		t.Fatalf("关键点数应为 2，实际 %d", len(plan.anchors))
	}
	if len(plan.segs) != 1 {
		t.Fatalf("应识别出 1 段路径，实际 %d", len(plan.segs))
	}
	if plan.anchors[0].frame != mdIdx || plan.anchors[1].frame != muIdx {
		t.Fatalf("关键点帧位置错误: %d/%d 期望 %d/%d", plan.anchors[0].frame, plan.anchors[1].frame, mdIdx, muIdx)
	}

	// 用合成画面解析两个关键点 (绕过真实抓屏，保证确定性)
	m := newVisionMatcher(PlayOptions{}, dir)
	for i := range plan.anchors {
		locateFullOn(m, screen, &plan.anchors[i])
	}

	for i, k := range kps {
		a := plan.anchors[i]
		if !a.matched {
			t.Fatalf("关键点%d 未命中 (score=%.4f)", i+1, a.score)
		}
		dx := float64(a.tgtX - (k.captureX + mid))
		dy := float64(a.tgtY - (k.captureY + mid))
		if math.Hypot(dx, dy) > 2 {
			t.Fatalf("关键点%d 定位偏差过大: 识别(%d,%d) 期望(%d,%d)",
				i+1, a.tgtX, a.tgtY, k.captureX+mid, k.captureY+mid)
		}
		fmt.Printf("[verify] 关键点%d 录制(%d,%d) -> 识别(%d,%d) 匹配度=%.2f%%\n",
			i+1, a.rawX, a.rawY, a.tgtX, a.tgtY, a.score)
	}

	// 激活路径段：两端已知，不再抓屏
	plan.activateSeg(0, newTestPipeline(plan, dir, screen, PlayOptions{}))
	seg := plan.segs[0]
	if seg.m.degenerate {
		t.Fatal("该场景应解出非退化的相似变换")
	}
	fmt.Printf("[verify] 相似变换: 旋转 %.2f°, 缩放 %.4fx\n", seg.m.angle, seg.m.scale)

	// 1) 段起点/终点必须精确落在识别点上
	sx, sy := seg.m.apply(plan.anchors[0].rawX, plan.anchors[0].rawY)
	ex, ey := seg.m.apply(plan.anchors[1].rawX, plan.anchors[1].rawY)
	if sx != plan.anchors[0].tgtX || sy != plan.anchors[0].tgtY {
		t.Fatalf("段起点未对齐: (%d,%d) != (%d,%d)", sx, sy, plan.anchors[0].tgtX, plan.anchors[0].tgtY)
	}
	if ex != plan.anchors[1].tgtX || ey != plan.anchors[1].tgtY {
		t.Fatalf("段终点未对齐: (%d,%d) != (%d,%d)", ex, ey, plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	// 且识别点必须就是真实位置 (即鼠标起点/终点确实移动到了最新识别的位置)
	if math.Hypot(float64(sx-(kps[0].captureX+mid)), float64(sy-(kps[0].captureY+mid))) > 2 ||
		math.Hypot(float64(ex-(kps[1].captureX+mid)), float64(ey-(kps[1].captureY+mid))) > 2 {
		t.Fatalf("段端点未移动到识别位置: 起(%d,%d) 终(%d,%d)", sx, sy, ex, ey)
	}

	// 2) 中间路径必须连续、不跳跃
	prevX, prevY := sx, sy
	maxJump := 0.0
	for _, p := range segPath {
		cx, cy := seg.m.apply(p[0], p[1])
		jump := math.Hypot(float64(cx-prevX), float64(cy-prevY))
		if jump > maxJump {
			maxJump = jump
		}
		prevX, prevY = cx, cy
	}
	jump := math.Hypot(float64(ex-prevX), float64(ey-prevY))
	if jump > maxJump {
		maxJump = jump
	}
	// 录制步长约 20px，拉伸系数不超过 2 倍 => 帧间位移必须远小于"跳跃" (几十像素以上)
	if maxJump > 60 {
		t.Fatalf("中间路径存在跳变: 最大帧间位移 %.1fpx", maxJump)
	}
	fmt.Printf("[verify] 中间路径 %d 帧, 最大帧间位移 %.1fpx (无跳跃)\n", len(segPath), maxJump)

	// 3) 帧归属：段内帧使用该变换，段外帧保持原样
	if si := plan.blockOf[mdIdx]; si != 0 {
		t.Fatalf("起点帧应归属段0，实际 %d", si)
	}
	if si := plan.blockOf[1]; si != -1 {
		t.Fatalf("起点之前的帧应保持原样，实际 %d", si)
	}
	if si := plan.blockOf[muIdx+0]; si != 0 {
		t.Fatalf("终点帧应归属段0，实际 %d", si)
	}
}

// TestVisionChainBreakByToggle 识图关闭必须打断关键点链 (下一轮起点不沿用上一轮终点)
func TestVisionChainBreakByToggle(t *testing.T) {
	on, off := true, false
	mk := func(op, vi string, x, y int) GoInput.RecordAction {
		if vi == "" {
			return GoInput.RecordAction{Op: op, X: x, Y: y}
		}
		return GoInput.RecordAction{Op: op, Vi: vi, X: x, Y: y, Vx: 16, Vy: 16, Vmode: "image", Vsize: 32}
	}
	frames := []playFrame{
		{action: ptr(mk("vision", "", 0, 0))}, // 占位，稍后覆盖
	}
	frames = nil
	push := func(a GoInput.RecordAction) { frames = append(frames, playFrame{action: &a}) }

	a := mk("vision", "", 0, 0)
	a.Von = &on
	push(a)
	push(mk("md", "md-left-001", 100, 100))
	push(mk("mu", "mu-left-001", 200, 200))
	// 中间关闭识图，再开启
	b := mk("vision", "", 0, 0)
	b.Von = &off
	push(b)
	push(mk("mv", "", 300, 300))
	c := mk("vision", "", 0, 0)
	c.Von = &on
	push(c)
	push(mk("md", "md-left-002", 400, 400))
	push(mk("mu", "mu-left-002", 500, 500))

	plan := buildVisionPlan(frames)
	if len(plan.anchors) != 4 {
		t.Fatalf("应有 4 个关键点，实际 %d", len(plan.anchors))
	}
	// 关闭识图 => 跨关闭点的关键点对不再连通：
	// 应得到 2 段独立路径 (md->mu 各一段)，而不是 1 段横跨关闭点的路径
	if len(plan.segs) != 2 {
		t.Fatalf("关闭识图应把关键点链切成 2 段，实际 %d", len(plan.segs))
	}
	if !plan.links[0] || plan.links[1] || !plan.links[2] {
		t.Fatalf("连通关系错误: %v", plan.links)
	}
	if plan.segs[0].to == plan.anchors[2].frame || plan.segs[1].from == plan.anchors[1].frame {
		t.Fatal("存在横跨识图关闭点的路径段")
	}
	// 关闭期间的中间帧必须保持原样回放 (不参与任何对齐)
	if si := plan.blockOf[4]; si != -1 {
		t.Fatalf("识图关闭期间的帧应原样回放，实际归属段 %d", si)
	}
	// 重新开启后的第一段路径以自身起点为准
	if si := plan.blockOf[plan.anchors[2].frame]; si != 1 {
		t.Fatalf("重新开启识图后的起点应开启新路径段，实际归属 %d", si)
	}
	fmt.Println("[verify] 识图关闭成功打断关键点链 (切成 2 段，关闭期间原样回放)")
}

// TestSimTransformIdentity 识别失败 (目标=原始坐标) 时必须退化为恒等变换
func TestSimTransformIdentity(t *testing.T) {
	m := newSimTransform(10, 20, 10, 20, 110, 220, 110, 220)
	for _, p := range [][2]int{{10, 20}, {110, 220}, {60, 120}} {
		x, y := m.apply(p[0], p[1])
		if x != p[0] || y != p[1] {
			t.Fatalf("恒等变换失败: (%d,%d) -> (%d,%d)", p[0], p[1], x, y)
		}
	}
	// 起终点重合 (原地点击) 必须退化为纯平移而不是 NaN
	d := newSimTransform(50, 50, 80, 90, 50, 50, 80, 90)
	if !d.degenerate {
		t.Fatal("起终点重合应退化为纯平移")
	}
	x, y := d.apply(0, 0)
	if x != 30 || y != 40 {
		t.Fatalf("纯平移结果错误: (%d,%d)", x, y)
	}
}

// TestVisionMarkerJSON 确认标记行写入/解析的字段稳定
func TestVisionMarkerJSON(t *testing.T) {
	on := true
	raw, err := json.Marshal(GoInput.RecordAction{
		Op: "vision", Von: &on, Vslot: 3, Vmode: "color", Vsize: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var back GoInput.RecordAction
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Von == nil || !*back.Von || back.Vslot != 3 || back.Vmode != "color" || back.Vsize != 64 {
		t.Fatalf("标记行往返失败: %s", string(raw))
	}
	fmt.Printf("[verify] 识图标记行: %s\n", string(raw))

	off := false
	raw, _ = json.Marshal(GoInput.RecordAction{Op: "vision", Von: &off})
	if string(raw) != `{"dt":0,"op":"vision","von":false}` {
		t.Fatalf("关闭标记行格式异常: %s", string(raw))
	}
}

// TestCapturePipeline 校验真实抓屏链路可用 (无屏幕环境自动跳过)
func TestCapturePipeline(t *testing.T) {
	sw, sh := GoVision.ScreenSize()
	if sw < 320 || sh < 240 {
		t.Skip("当前环境无可用屏幕")
	}
	mat, err := GoVision.CaptureFullScreenMat()
	if err != nil {
		t.Skipf("抓屏不可用: %v", err)
	}
	defer mat.Close()
	if mat.Empty() || mat.Cols() != sw || mat.Rows() != sh {
		t.Fatalf("抓屏尺寸异常: %dx%d 期望 %dx%d", mat.Cols(), mat.Rows(), sw, sh)
	}

	// 屏幕边缘也必须能采样，且**样本尺寸必须恒定**（贴边时整块平移而不是裁小）
	for _, c := range [][2]int{{0, 0}, {sw - 1, sh - 1}, {sw / 2, sh / 2}, {0, sh / 2}, {sw / 2, 0}} {
		rect, iox, ioy, ok := GoVision.PatchGeometry(c[0], c[1], 32)
		if !ok {
			t.Fatalf("边缘采样失败: %v", c)
		}
		if rect.Dx() != 32 || rect.Dy() != 32 {
			t.Fatalf("样本尺寸必须恒为 32x32，实际 %dx%d (光标 %v)", rect.Dx(), rect.Dy(), c)
		}
		if iox < 0 || iox >= 32 || ioy < 0 || ioy >= 32 {
			t.Fatalf("光标偏移必须落在样本图内: iox=%d ioy=%d (光标 %v)", iox, ioy, c)
		}
		// 样本区域必须完整落在屏幕内
		if rect.Min.X < 0 || rect.Min.Y < 0 || rect.Max.X > sw || rect.Max.Y > sh {
			t.Fatalf("样本区域越出屏幕: %v", rect)
		}
		// 回放还原公式 (命中点 + iox) 必须能还原光标
		if rect.Min.X+iox != c[0] || rect.Min.Y+ioy != c[1] {
			t.Fatalf("偏移还原失败: (%d,%d)+(%d,%d) != %v", rect.Min.X, rect.Min.Y, iox, ioy, c)
		}
	}
	// 屏幕中央时偏移必须正好是半边 (保证居中)
	if _, iox, ioy, _ := GoVision.PatchGeometry(sw/2, sh/2, 32); iox != 16 || ioy != 16 {
		t.Fatalf("屏幕中央偏移应为 16/16，实际 %d/%d", iox, ioy)
	}
	// 三个预设槽尺寸都必须恒定
	for _, size := range []int{32, 64, 128} {
		for _, c := range [][2]int{{1, 1}, {sw - 2, sh - 2}, {sw / 2, sh / 2}} {
			rect, _, _, ok := GoVision.PatchGeometry(c[0], c[1], size)
			if !ok || rect.Dx() != size || rect.Dy() != size {
				t.Fatalf("尺寸 %d 在 %v 处未保持恒定: ok=%v %v", size, c, ok, rect)
			}
		}
	}
	fmt.Printf("[verify] 抓屏链路正常: %dx%d，边缘采样尺寸恒定\n", sw, sh)
}

// TestSegmentModeDegrade 两端识别结果互相矛盾时必须退回纯平移，而不是乱拉乱转
func TestSegmentModeDegrade(t *testing.T) {
	dir := t.TempDir()
	screen := makeSyntheticScreen(600, 400)
	defer screen.Close()
	extractPatch(t, screen, dir, "md-left-001", 100, 100, 32)
	extractPatch(t, screen, dir, "mu-left-001", 500, 300, 32)

	// 场景：录制的起终点相距 500px，但识别结果都被拉到几乎同一点
	// (典型的误匹配特征) => 缩放会远小于 1，必须拒绝旋转/拉伸
	on := true
	var frames []playFrame
	push := func(a GoInput.RecordAction) { frames = append(frames, playFrame{action: &a}) }
	push(GoInput.RecordAction{Op: "vision", Von: &on, Vslot: 1, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "md", Btn: "left", X: 100, Y: 100, Vi: "md-left-001", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "mv", X: 300, Y: 200})
	push(GoInput.RecordAction{Op: "mu", Btn: "left", X: 600, Y: 400, Vi: "mu-left-001", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})

	plan := buildVisionPlan(frames)
	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	defer vp.Close()
	// 人为制造"矛盾命中"：两端都命中，但目标点几乎相同
	plan.anchors[0].resolved, plan.anchors[0].matched, plan.anchors[0].score = true, true, 96
	plan.anchors[0].tgtX, plan.anchors[0].tgtY = 300, 200
	plan.anchors[1].resolved, plan.anchors[1].matched, plan.anchors[1].score = true, true, 95
	plan.anchors[1].tgtX, plan.anchors[1].tgtY = 301, 200

	plan.activateSeg(0, newTestPipeline(plan, dir, screen, PlayOptions{}))
	seg := plan.segs[0]
	if seg.mode != segTransA {
		t.Fatalf("矛盾命中应退回纯平移(取分数更好的起点)，实际 %v", seg.mode)
	}
	// 纯平移：路径形状必须保持 (任意两点位移一致)
	x0, y0 := seg.m.apply(100, 100)
	x1, y1 := seg.m.apply(600, 400)
	if x1-x0 != 500 || y1-y0 != 300 {
		t.Fatalf("纯平移应保持路径形状: 位移 (%d,%d)", x1-x0, y1-y0)
	}
	if x0 != 300 || y0 != 200 {
		t.Fatalf("起点应对齐到识别点 (300,200)，实际 (%d,%d)", x0, y0)
	}
	fmt.Println("[verify] 矛盾命中已退回纯平移，路径形状保持不变")

	// 缩放处于安全区间的正常情形必须走旋转/拉伸
	plan2 := buildVisionPlan(frames)
	plan2.anchors[0].resolved, plan2.anchors[0].matched, plan2.anchors[0].score = true, true, 98
	plan2.anchors[0].tgtX, plan2.anchors[0].tgtY = 150, 130
	plan2.anchors[1].resolved, plan2.anchors[1].matched, plan2.anchors[1].score = true, true, 98
	plan2.anchors[1].tgtX, plan2.anchors[1].tgtY = 720, 460
	plan2.activateSeg(0, newTestPipeline(plan2, dir, screen, PlayOptions{}))
	if plan2.segs[0].mode != segSimilar {
		t.Fatalf("正常命中应使用旋转/拉伸对齐，实际 %v", plan2.segs[0].mode)
	}
}

// TestRealScreenFalsePositiveGuard 真实画面上的无关内容必须被阈值挡掉。
//
// 若这里失败，说明默认阈值过于宽松，回放会出现误匹配 -> 鼠标乱跳。
func TestRealScreenFalsePositiveGuard(t *testing.T) {
	sw, sh := GoVision.ScreenSize()
	if sw < 320 || sh < 240 {
		t.Skip("当前环境无可用屏幕")
	}
	screen, err := GoVision.CaptureFullScreenMat()
	if err != nil {
		t.Skipf("抓屏不可用: %v", err)
	}
	defer screen.Close()

	dir := t.TempDir()
	// 随机噪声样本：与画面内容无关，必须匹配失败
	seed := uint32(0xABCDEF)
	data := make([]byte, 32*32*3)
	for i := range data {
		seed = seed*1664525 + 1013904223
		data[i] = uint8(seed >> 16)
	}
	mat, err := gocv.NewMatFromBytes(32, 32, gocv.MatTypeCV8UC3, data)
	if err != nil {
		t.Fatal(err)
	}
	if !gocv.IMWrite(filepath.Join(dir, "noise.png"), mat) {
		t.Fatal("写入噪声样本失败")
	}
	mat.Close()

	m := newVisionMatcher(PlayOptions{}, dir)
	a := visionAnchor{img: "noise", iox: 16, ioy: 16, mode: "image", size: 32, rawX: 111, rawY: 222}
	locateFullOn(m, screen, &a)
	if a.matched {
		t.Fatalf("无关内容不应命中：匹配度 %.1f%% >= 阈值 %.1f%% -> 会造成鼠标乱跳", a.score, m.thresholdPercent)
	}
	fmt.Printf("[verify] 无关内容已按匹配度阈值 %.1f%% 拒识 (实测 %.1f%%)\n", m.thresholdPercent, a.score)
}

// TestRealScreenTruePositive 真实画面的真实命中必须通过，且定位精确
func TestRealScreenTruePositive(t *testing.T) {
	sw, sh := GoVision.ScreenSize()
	if sw < 320 || sh < 240 {
		t.Skip("当前环境无可用屏幕")
	}
	screen, err := GoVision.CaptureFullScreenMat()
	if err != nil {
		t.Skipf("抓屏不可用: %v", err)
	}
	defer screen.Close()

	// 取画面上纹理最强的 32x32 位置作为样本
	best, bx, by := 0.0, -1, -1
	for y := 20; y < sh-300; y += 8 {
		for x := 20; x < sw-40; x += 8 {
			r := screen.Region(imageRect(x, y, 32, 32))
			mean := gocv.NewMat()
			dev := gocv.NewMat()
			gocv.MeanStdDev(r, &mean, &dev)
			sd := dev.GetDoubleAt(0, 0)
			mean.Close()
			dev.Close()
			r.Close()
			if sd > best {
				best, bx, by = sd, x, y
			}
		}
	}
	if bx < 0 || best < 5 {
		t.Skip("画面上找不到足够纹理的区域")
	}

	dir := t.TempDir()
	patch := screen.Region(imageRect(bx, by, 32, 32))
	defer patch.Close()
	if !gocv.IMWrite(filepath.Join(dir, "real.png"), patch) {
		t.Fatal("写入样本失败")
	}

	// 固定单尺度：本用例验证的是"抓屏 -> 载入 -> 匹配 -> 换回绝对坐标"这条链路
	// 是否精确；多尺度搜索在缩略尺度上会有几像素的正常抖动，不适合做像素级断言。
	m := newVisionMatcher(PlayOptions{VisionMinScale: 1, VisionMaxScale: 1}, dir)
	a := visionAnchor{img: "real", iox: 16, ioy: 16, mode: "image", size: 32, rawX: 0, rawY: 0}
	locateFullOn(m, screen, &a)
	if !a.matched {
		t.Fatalf("真实内容应命中，实际 score=%.5f (纹理 stddev=%.1f)", a.score, best)
	}
	// 桌面内容可能自带重复/周期性图案（同一块内容在别处也出现），
	// 因此真实画面上只断言"确实命中了同一份内容"，像素级精度由合成画面的用例保证。
	if a.score < 98 {
		t.Fatalf("真实画面命中匹配度过低: %.2f%%", a.score)
	}
	if dx, dy := a.tgtX-(bx+16), a.tgtY-(by+16); absInt(dx) > 16 || absInt(dy) > 16 {
		t.Fatalf("真实命中定位偏离过大: (%d,%d) 期望约 (%d,%d)", a.tgtX, a.tgtY, bx+16, by+16)
	}
	fmt.Printf("[verify] 真实画面命中 匹配度=%.2f%% 定位(%d,%d) 源位置(%d,%d)\n", a.score, a.tgtX, a.tgtY, bx+16, by+16)
}

func ptr(a GoInput.RecordAction) *GoInput.RecordAction { return &a }

// extractCentered 以 (cx,cy) 为中心截取 size×size 样本
func extractCentered(t *testing.T, screen gocv.Mat, dir, name string, cx, cy, size int) {
	t.Helper()
	r := screen.Region(imageRect(cx-size/2, cy-size/2, size, size))
	defer r.Close()
	if !gocv.IMWrite(filepath.Join(dir, name+".png"), r) {
		t.Fatalf("写入样本失败: %s", name)
	}
}
