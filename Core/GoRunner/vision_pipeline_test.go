package GoRunner

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoVision"

	"gocv.io/x/gocv"
)

// ---------------------------------------------------------------------------
// 识别流水线验证（确定性）：
// 用合成画面注入流水线，验证 预取候选 -> 采用前复核 -> 快速区域 -> 回退标准识别
// 以及预取窗口滑动/取消 的行为。
// ---------------------------------------------------------------------------

// TestPairFastScanFirstWins 同键对并发扫描：两张图同时找，谁先命中就用谁的结果，
// 且另一侧按录制位移直接换算（不必等慢的那一侧）。
func TestPairFastScanFirstWins(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	// 前一个已确认的关键点（提供预测来源 + 快速区域）
	prev := makeAnchor(t, screen, dir, "f0", 300, 220, 32, 300, 220)
	// 本次点击对：按下 (500,400)，松开只移了 6px
	md := makeAnchor(t, screen, dir, "fa", 500, 400, 32, 500, 400)
	mu := makeAnchor(t, screen, dir, "fb", 506, 404, 32, 506, 404)
	plan := planOf([]visionAnchor{prev, md, mu})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionPairTol: 32})
	defer vp.Close()

	// 前一个关键点已确认并留下快速区域缓存
	vp.mu.Lock()
	plan.anchors[0].resolved, plan.anchors[0].matched = true, true
	plan.anchors[0].tgtX, plan.anchors[0].tgtY = 300, 220
	vp.cache = image.Rect(236, 156, 364, 284) // 边长 128 = 32*4，中心 (300,220)
	vp.hasCache = true
	vp.mu.Unlock()

	ok := vp.ResolvePairFast(1, 2)
	if !ok {
		t.Fatal("同键对并发扫描应当完成这一对")
	}
	if !plan.anchors[1].matched || !plan.anchors[2].matched {
		t.Fatalf("两侧都应被确认: a=%v b=%v", plan.anchors[1].matched, plan.anchors[2].matched)
	}
	if plan.anchors[1].tgtX != 500 || plan.anchors[1].tgtY != 400 {
		t.Fatalf("按下位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if plan.anchors[2].tgtX != 506 || plan.anchors[2].tgtY != 404 {
		t.Fatalf("松开位置错误: (%d,%d)", plan.anchors[2].tgtX, plan.anchors[2].tgtY)
	}
	if int(vp.matcher.full.Load()) != 0 {
		t.Fatal("快速区域内完成的一对不应触发全屏识别")
	}
	fmt.Printf("[verify] 同键对并发扫描一次完成两个关键点: (%d,%d) 与 (%d,%d)，全屏识别 0 次\n",
		plan.anchors[1].tgtX, plan.anchors[1].tgtY, plan.anchors[2].tgtX, plan.anchors[2].tgtY)
}

// TestPairFastScanFallsBackWhenNoCache 没有快速区域时必须交回常规路径（不擅自并发扫描）
func TestPairFastScanFallsBackWhenNoCache(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()
	prev := makeAnchor(t, screen, dir, "g0", 300, 220, 32, 300, 220)
	md := makeAnchor(t, screen, dir, "ga", 400, 300, 32, 400, 300)
	mu := makeAnchor(t, screen, dir, "gb", 404, 302, 32, 404, 302)
	plan := planOf([]visionAnchor{prev, md, mu})
	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	defer vp.Close()

	vp.mu.Lock()
	plan.anchors[0].resolved, plan.anchors[0].matched = true, true
	plan.anchors[0].tgtX, plan.anchors[0].tgtY = 300, 220
	vp.mu.Unlock()

	if vp.ResolvePairFast(1, 2) {
		t.Fatal("没有缓存时不应走并发扫描")
	}
	if plan.anchors[1].resolved || plan.anchors[2].resolved {
		t.Fatal("交回常规路径时不应改动锚点状态")
	}
	fmt.Println("[verify] 无快速区域 => 并发扫描让位给常规路径（行为不变）")

	// 位移超过阈值同样不接管
	vp.mu.Lock()
	vp.cache = image.Rect(236, 156, 364, 284)
	vp.hasCache = true
	plan.anchors[2].rawX, plan.anchors[2].rawY = 900, 600
	vp.mu.Unlock()
	if vp.ResolvePairFast(1, 2) {
		t.Fatal("位移超过 pairTol 时不应走并发扫描")
	}
	fmt.Println("[verify] 位移超过阈值 => 并发扫描同样让位给常规路径")
}

// TestRegionConfirmEscalatesToGlobalBest 受限区域里的"长得像"目标不得被当真。
//
// 这是通用规则，不依赖任何界面布局：缩小的搜索范围里找到的只是**局部最优**，
// 区域外可能还有更像的真目标（界面上有一堆相似的格子/图标/按钮时就会这样）。
// 因此区域结果必须达到置信度门槛，达不到就扩大范围，直到全屏找到全局最优。
func TestRegionConfirmEscalatesToGlobalBest(t *testing.T) {
	const (
		trueX, trueY = 300, 500 // 真目标：远离预测位置
		nearX, nearY = 806, 452 // 预测位置附近的"相似目标"（混了噪声，不是同一个东西）
	)
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	// 样本 = 真目标那一块
	extractCentered(t, screen, dir, "tpl", trueX, trueY, 32)

	// 在预测位置附近贴一个"相似目标"：真样本 50% + 噪声 50%
	tpl, err := GoVision.LoadTemplate(filepath.Join(dir, "tpl.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer tpl.Close()
	data := make([]byte, 32*32*3)
	seed := uint32(0x5EED1234)
	for i := range data {
		seed = seed*1664525 + 1013904223
		data[i] = uint8(seed >> 16)
	}
	noise, err := gocv.NewMatFromBytes(32, 32, gocv.MatTypeCV8UC3, data)
	if err != nil {
		t.Fatal(err)
	}
	defer noise.Close()
	blend := gocv.NewMat()
	defer blend.Close()
	gocv.AddWeighted(tpl, 0.5, noise, 0.5, 0, &blend)
	region := screen.Region(image.Rect(nearX-16, nearY-16, nearX+16, nearY+16))
	blend.CopyTo(&region)
	region.Close()

	// 锚点 0 已确认在 (200,200)；锚点 1 录制位置 (800,450) => 预测位置 (800,450)
	mkPlan := func() (*visionPlan, *visionPipeline) {
		a0 := visionAnchor{
			frame: 0, img: "tpl", iox: 16, ioy: 16, mode: "image", size: 32,
			rawX: 200, rawY: 200, tgtX: 200, tgtY: 200, resolved: true, matched: true, score: 100,
		}
		a1 := visionAnchor{
			frame: 1, img: "tpl", iox: 16, ioy: 16, mode: "image", size: 32,
			rawX: 800, rawY: 450, tgtX: 800, tgtY: 450,
		}
		p := planOf([]visionAnchor{a0, a1})
		vp := newTestPipeline(p, dir, screen, PlayOptions{VisionSimilarity: 20, VisionRegionConfirm: -1})
		vp.mu.Lock()
		vp.cache = image.Rect(800-64, 450-64, 800+64, 450+64) // 快速区域：只圈到相似目标
		vp.hasCache = true
		vp.mu.Unlock()
		return p, vp
	}

	// 先量一下"相似目标"的匹配度，确认它确实落在"过阈值但不够可信"的区间
	_, vp0 := mkPlan()
	defer vp0.Close()
	_, _, lookScore, lookOK := vp0.matcher.searchIn(screen, "tpl", 16, 16, false,
		regionAround(800, 450, 32*4, 16, 900, 640))
	if !lookOK {
		t.Fatalf("相似目标应当能在区域内被匹配到 (score=%.1f%%)", lookScore)
	}
	if lookScore >= 99 {
		t.Fatalf("构造的相似目标太像了 (%.1f%%)，无法验证门控", lookScore)
	}

	// ① 关掉门控：区域里的相似目标直接被采纳 —— 正是要避免的行为
	plan1, vp1 := mkPlan()
	defer vp1.Close()
	vp1.ResolveAnchorAtUse(1)
	if !plan1.anchors[1].matched {
		t.Fatal("关闭门控时应当命中")
	}
	if absInt(plan1.anchors[1].tgtX-nearX) > 4 || absInt(plan1.anchors[1].tgtY-nearY) > 4 {
		t.Fatalf("关闭门控时应停在区域内的相似目标 (%d,%d)，实际 (%d,%d)",
			nearX, nearY, plan1.anchors[1].tgtX, plan1.anchors[1].tgtY)
	}
	if int(vp1.matcher.full.Load()) != 0 {
		t.Fatal("关闭门控时不应扩大范围")
	}
	fmt.Printf("[verify] 门控关闭：相似目标 (匹配度 %.1f%%) 被就地采纳 -> (%d,%d)，全屏识别 0 次\n",
		lookScore, plan1.anchors[1].tgtX, plan1.anchors[1].tgtY)

	// ② 打开门控：相似目标置信度不足 => 逐级扩大范围 => 全屏找到真正的目标
	plan2, vp2 := mkPlan()
	defer vp2.Close()
	vp2.matcher.confirmPercent = 99
	vp2.ResolveAnchorAtUse(1)
	if !plan2.anchors[1].matched {
		t.Fatal("打开门控时应当命中")
	}
	if plan2.anchors[1].tgtX != trueX || plan2.anchors[1].tgtY != trueY {
		t.Fatalf("应当扩大范围找到真目标 (%d,%d)，实际 (%d,%d)",
			trueX, trueY, plan2.anchors[1].tgtX, plan2.anchors[1].tgtY)
	}
	if int(vp2.matcher.regionLow.Load()) == 0 {
		t.Fatal("应当记录到'区域命中但置信度不足 => 扩大范围'")
	}
	if int(vp2.matcher.full.Load()) == 0 {
		t.Fatal("置信度不足时应当最终由全屏全局最优接住")
	}
	fmt.Printf("[verify] 门控开启：相似目标被拒 (%.1f%% < %.0f%%)，扩大范围后找到真目标 (%d,%d) 匹配度=%.1f%%，全屏识别 %d 次\n",
		lookScore, vp2.matcher.confirmPercent, plan2.anchors[1].tgtX, plan2.anchors[1].tgtY,
		plan2.anchors[1].score, int(vp2.matcher.full.Load()))
}

// TestKeypointClickUsesConfirmedPosition 鼠标按下/松开的位置必须来自"该关键点自己
// 最终确认的识别位置"，而不能来自"用提前标记算好的段变换"。
//
// 复现的症状：鼠标先跑到错误位置等着，点击瞬间才被纠正回正确位置，
// 结果这次点击被算在旧位置上（按下与松开、点击与识别脱节）。
func TestKeypointClickUsesConfirmedPosition(t *testing.T) {
	screen := makeSyntheticScreen(1200, 800)
	defer screen.Close()
	dir := t.TempDir()

	const (
		mx, my = 200, 200 // 按下（真实位置）
		ux, uy = 700, 500 // 松开（真实位置）
	)
	extractCentered(t, screen, dir, "md0", mx, my, 32)
	extractCentered(t, screen, dir, "mu0", ux, uy, 32)

	on := true
	var frames []playFrame
	push := func(a GoInput.RecordAction) { frames = append(frames, playFrame{action: &a}) }
	push(GoInput.RecordAction{Op: "vision", Von: &on, Vslot: 1, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "mv", X: mx, Y: my})
	push(GoInput.RecordAction{Op: "md", Btn: "left", X: mx, Y: my, Vi: "md0", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "mv", X: ux, Y: uy})
	muFrame := len(frames)
	push(GoInput.RecordAction{Op: "mu", Btn: "left", X: ux, Y: uy, Vi: "mu0", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})

	plan := buildVisionPlan(frames)
	if len(plan.anchors) != 2 || !plan.usable() {
		t.Fatalf("应当解析出 2 个关键点 / 1 段路径")
	}
	mdFrame := plan.anchors[0].frame
	if plan.blockOf[muFrame] != 0 {
		t.Fatalf("最后一个关键点帧归属异常: %d", plan.blockOf[muFrame])
	}

	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	defer vp.Close()

	// 按下已确认（就是真实位置）
	vp.commit(0, mx, my, 100, false)

	// 松开的"提前标记"打偏了（模拟受限区域里挑到了相似目标）
	vp.mu.Lock()
	plan.anchors[1].resolved, plan.anchors[1].matched = true, true
	plan.anchors[1].fast, plan.anchors[1].early = true, true
	plan.anchors[1].score = 99
	plan.anchors[1].tgtX, plan.anchors[1].tgtY = 280, 520
	vp.mu.Unlock()

	// 进入本段：段变换基于那个错误的提前标记算出来
	plan.activateSeg(0, vp)
	if !plan.segs[0].ready {
		t.Fatal("本段应当已激活")
	}
	if x, y := plan.segs[0].m.apply(ux, uy); x != 280 || y != 520 {
		t.Fatalf("段变换应当基于提前标记 (280,520)，实际 (%d,%d)", x, y)
	}
	// 停顿开始前：光标先按"当前已知位置"到位（此时就是那个错误标记）
	if x, y, ok := vp.KeypointCursor(muFrame); !ok || x != 280 || y != 520 {
		t.Fatalf("停顿前应先把光标放到已知位置: (%d,%d) ok=%v", x, y, ok)
	}

	// 到达松开帧：实时复核否决错误标记 => 重新定位到真实位置
	vp.ConfirmAtFrame(muFrame)
	if x, y, ok := vp.KeypointTarget(muFrame); !ok || x != ux || y != uy {
		t.Fatalf("松开帧必须用最终确认的位置 (%d,%d)，实际 (%d,%d) ok=%v", ux, uy, x, y, ok)
	}
	if x, y, ok := vp.KeypointTarget(mdFrame); !ok || x != mx || y != my {
		t.Fatalf("按下帧位置错误: (%d,%d) ok=%v", x, y, ok)
	}
	// 位置被改掉 => 引用它的段变换必须作废（否则收尾路径还会按旧位置走）
	if plan.segs[0].ready {
		t.Fatal("关键点位置被修正后，段变换必须作废重建")
	}
	plan.activateSeg(0, vp)
	if x, y := plan.segs[0].m.apply(ux, uy); x != ux || y != uy {
		t.Fatalf("重建后的段变换应指向确认位置 (%d,%d)，实际 (%d,%d)", ux, uy, x, y)
	}
	if x, y := plan.segs[0].m.apply(mx, my); x != mx || y != my {
		t.Fatalf("重建后段起点不应被改动: (%d,%d)", x, y)
	}
	// 同一位置重复确认不应反复作废（避免每帧重建）
	plan.activateSeg(0, vp)
	vp.commit(1, ux, uy, 100, true)
	if !plan.segs[0].ready {
		t.Fatal("位置没变时不应作废段变换")
	}
	fmt.Printf("[verify] 提前标记打偏 (280,520) => 到点复核否决并重新定位 (%d,%d)；段变换作废重建，"+
		"按下/松开各用自己确认的位置（不再经过旧变换）\n", ux, uy)
}

// TestClickPairStaysConsistent 一次点击的"按下/松开"必须落在同一处。
//
// 录制的按下/松开位移很小（同一处点击），因此这一对本来就是同一张图。
// 若松开那侧跑去更大的范围里另找一个位置，这次点击就会被拆成一次拖拽
// （界面表现就是"点击效果不对""按下和松开脱节"）。
func TestClickPairStaysConsistent(t *testing.T) {
	screen := makeSyntheticScreen(1400, 900)
	defer screen.Close()
	dir := t.TempDir()

	const (
		mx, my = 400, 300 // 按下（真实位置）
		ux, uy = 402, 302 // 松开：与按下同一处，只差 2px
	)
	extractCentered(t, screen, dir, "mdp", mx, my, 32)
	// 松开那张样本取自画面另一处：模拟"这一对在外界看来不再重合"的情形
	extractCentered(t, screen, dir, "mup", 900, 600, 32)

	on := true
	var frames []playFrame
	push := func(a GoInput.RecordAction) { frames = append(frames, playFrame{action: &a}) }
	push(GoInput.RecordAction{Op: "vision", Von: &on, Vslot: 1, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "md", Btn: "left", X: mx, Y: my, Vi: "mdp", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})
	push(GoInput.RecordAction{Op: "mu", Btn: "left", X: ux, Y: uy, Vi: "mup", Vx: 16, Vy: 16, Vmode: "image", Vsize: 32})

	// ① 关掉同键对：松开那侧会跑到更大的范围里另找位置 => 一次点击变成 500px 拖拽
	plan1 := buildVisionPlan(frames)
	vp1 := newTestPipeline(plan1, dir, screen, PlayOptions{})
	defer vp1.Close()
	vp1.pairTol = 0 // 直接关掉同键对（-vpair 的最小值是默认值，测试里改字段更直接）
	vp1.commit(0, mx, my, 100, false)
	vp1.ResolveAnchorAtUse(1)
	if plan1.anchors[1].tgtX != 900 || plan1.anchors[1].tgtY != 600 {
		t.Fatalf("关闭同键对时应当在别处找到目标 (900,600)，实际 (%d,%d)",
			plan1.anchors[1].tgtX, plan1.anchors[1].tgtY)
	}
	fmt.Printf("[verify] 关闭同键对：按下 (%d,%d) / 松开 (%d,%d) —— 一次点击被拆成 500px 拖拽\n",
		plan1.anchors[0].tgtX, plan1.anchors[0].tgtY, plan1.anchors[1].tgtX, plan1.anchors[1].tgtY)

	// ② 默认（同键对生效）：松开必须与按下保持录制位移，点击仍然是点击
	plan2 := buildVisionPlan(frames)
	vp2 := newTestPipeline(plan2, dir, screen, PlayOptions{})
	defer vp2.Close()
	vp2.commit(0, mx, my, 100, false)
	vp2.ResolveAnchorAtUse(1)
	if plan2.anchors[1].tgtX != ux || plan2.anchors[1].tgtY != uy {
		t.Fatalf("松开必须与按下保持录制位移 (%d,%d)，实际 (%d,%d)",
			ux, uy, plan2.anchors[1].tgtX, plan2.anchors[1].tgtY)
	}
	if int(vp2.matcher.full.Load()) != 0 {
		t.Fatal("同一键对不应为了另找一个位置而做全屏搜索")
	}
	fmt.Printf("[verify] 同键对生效：按下 (%d,%d) / 松开 (%d,%d)，位移与录制一致，全屏识别 0 次\n",
		plan2.anchors[0].tgtX, plan2.anchors[0].tgtY, plan2.anchors[1].tgtX, plan2.anchors[1].tgtY)
}

// TestSegmentSquashTrustedWhenConfident 两端都是高置信度命中时，
// "位移比例看起来不合理"是布局重排的真实结果，必须照做。
//
// 复现日志里的症状：点击3→4、点击5→6 之间，路径朝**录制时的旧位置**走
// （也就是朝下一个点击目标的反方向），到点击那一刻才跳回正确位置。
// 原因：那两段两端的位移比例是 0.448x / 0.483x，被 [0.50, 2.00] 的
// "两端矛盾"门限挡掉 => 退化成纯平移（起点可信）= 按原坐标回放。
func TestSegmentSquashTrustedWhenConfident(t *testing.T) {
	// 直接取真实日志里"路径段#6"的两端：
	//   起点 录制(1769,711) -> 识别(1769,711)   匹配度 100.0%
	//   终点 录制(1976,817) -> 识别(1764,607)   匹配度  99.9%
	//   录制相距 232.6px，识别相距 104.1px => 比例 0.448x
	const (
		aRawX, aRawY = 1769, 711
		aTgtX, aTgtY = 1769, 711
		bRawX, bRawY = 1976, 817
		bTgtX, bTgtY = 1764, 607
	)
	mk := func(aScore, bScore float64) (*visionPlan, *visionPipeline) {
		screen := makeSyntheticScreen(2200, 1000)
		dir := t.TempDir()
		a := visionAnchor{
			img: "x", size: 32, mode: "image",
			rawX: aRawX, rawY: aRawY, tgtX: aTgtX, tgtY: aTgtY,
			resolved: true, matched: true, score: aScore,
		}
		b := visionAnchor{
			img: "x", size: 32, mode: "image",
			rawX: bRawX, rawY: bRawY, tgtX: bTgtX, tgtY: bTgtY,
			resolved: true, matched: true, score: bScore,
		}
		plan := planOf([]visionAnchor{a, b})
		return plan, newTestPipeline(plan, dir, screen, PlayOptions{})
	}

	dist2i := func(x0, y0, x1, y1 int) float64 { return math.Hypot(float64(x1-x0), float64(y1-y0)) }
	dist2f := func(x0, y0 int, x1, y1 float64) float64 { return math.Hypot(x1-float64(x0), y1-float64(y0)) }

	// ① 两端都是高置信度命中（>= -vconf 97%）=> 照做旋转/拉伸，两端严格对齐
	plan1, vp1 := mk(100.0, 99.9)
	defer vp1.Close()
	plan1.activateSeg(0, vp1)
	seg1 := plan1.segs[0]
	if seg1.mode != segSimilar {
		t.Fatalf("两端高置信度命中时段落应当照做旋转/拉伸，实际 %v (%s)", seg1.mode, seg1.reason)
	}
	if x, y := seg1.m.apply(aRawX, aRawY); x != aTgtX || y != aTgtY {
		t.Fatalf("起点未严格对齐: (%d,%d)", x, y)
	}
	if x, y := seg1.m.apply(bRawX, bRawY); x != bTgtX || y != bTgtY {
		t.Fatalf("终点未严格对齐: (%d,%d) 期望 (%d,%d)", x, y, bTgtX, bTgtY)
	}
	// 路径中点必须落在"识别两端之间"，即朝着下一个点击目标走
	mx, my := seg1.m.apply((aRawX+bRawX)/2, (aRawY+bRawY)/2)
	wantMX, wantMY := float64(aTgtX+bTgtX)/2, float64(aTgtY+bTgtY)/2
	if dist2f(mx, my, wantMX, wantMY) > 3 {
		t.Fatalf("路径中点应落在识别两端之间 (%.0f,%.0f)，实际 (%d,%d)", wantMX, wantMY, mx, my)
	}
	// 关键：路径必须**靠近**下一个点击目标，而不是朝录制旧位置去
	if dist2i(mx, my, bTgtX, bTgtY) > dist2i(mx, my, bRawX, bRawY) {
		t.Fatal("路径应当朝识别到的下一个点击目标走")
	}
	fmt.Printf("[verify] 两端高置信度 (100.0%%/99.9%%)：位移比例 %.3fx 照样采用，路径朝目标走，中点 (%d,%d)\n",
		seg1.m.rawScale, mx, my)

	// ② 一端只是"勉强命中"（低于 -vconf）=> 仍按门限降级为纯平移（安全网保留）
	plan2, vp2 := mk(96.0, 95.0)
	defer vp2.Close()
	plan2.activateSeg(0, vp2)
	seg2 := plan2.segs[0]
	if seg2.mode != segTransA {
		t.Fatalf("勉强命中时应降级为纯平移(起点可信)，实际 %v", seg2.mode)
	}
	ex, ey := seg2.m.apply(bRawX, bRawY)
	dst := dist2i(ex, ey, bTgtX, bTgtY)
	if dst < dist2i(aTgtX, aTgtY, bTgtX, bTgtY) {
		t.Fatal("降级为纯平移时，路径确实会朝录制旧位置去（这正是要避免的症状）")
	}
	fmt.Printf("[verify] 勉强命中 (96.0%%/95.0%%)：仍降级为纯平移 => 终点落在 (%d,%d)，"+
		"离目标 %.0fpx（症状成因：到点击那一刻才跳过去）\n", ex, ey, dst)
}

// imageRect 便捷构造矩形
func imageRect(x, y, w, h int) image.Rectangle { return image.Rect(x, y, x+w, y+h) }

// TestRotationFullCircle 旋转角的值域必须是完整一圈 (−180°, +180°]：
// 默认限制 180 意味着"任何旋转都照做"，收小限制才退回纯平移。
func TestRotationFullCircle(t *testing.T) {
	// ① 180° 翻转：录制向右，回放向左
	t180 := newSimTransform(100, 100, 100, 100, 300, 100, 0, 100)
	if math.Abs(math.Abs(t180.angle)-180) > 1 {
		t.Fatalf("180° 翻转应算得有符号角约 ±180°，实际 %.1f°", t180.angle)
	}
	// ② 顺时针 90°：向右 -> 向下
	t90 := newSimTransform(100, 100, 100, 100, 300, 100, 100, 300)
	if math.Abs(t90.angle-90) > 1 {
		t.Fatalf("向右转成向下应算得 +90°，实际 %.1f°", t90.angle)
	}
	// ③ 逆时针 159°（等价于顺时针 201°，同一个旋转）
	tNeg := newSimTransform(0, 0, 0, 0, 100, 0, -93, -37)
	if tNeg.angle > -150 || tNeg.angle < -170 {
		t.Fatalf("该场景应有符号角约 −159°，实际 %.1f°", tNeg.angle)
	}

	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	// 一个"翻转 180°"的路径：录制向右，回放向左
	mkPlan := func() *visionPlan {
		a := visionAnchor{img: "r1", iox: 16, ioy: 16, size: 32, mode: "image",
			rawX: 200, rawY: 300, tgtX: 200, tgtY: 300, resolved: true, matched: true, score: 99}
		b := visionAnchor{img: "r2", iox: 16, ioy: 16, size: 32, mode: "image",
			rawX: 400, rawY: 300, tgtX: 100, tgtY: 300, resolved: true, matched: true, score: 99}
		return planOf([]visionAnchor{a, b})
	}

	p1 := mkPlan()
	vp1 := newTestPipeline(p1, dir, screen, PlayOptions{})
	p1.activateSeg(0, vp1)
	vp1.Close()
	if p1.segs[0].mode != segSimilar {
		t.Fatalf("默认应允许整圈旋转（照做翻转），实际 %v (原因 %s)", p1.segs[0].mode, p1.segs[0].reason)
	}

	p2 := mkPlan()
	vp2 := newTestPipeline(p2, dir, screen, PlayOptions{VisionMaxRotation: 15})
	p2.activateSeg(0, vp2)
	vp2.Close()
	if p2.segs[0].mode == segSimilar {
		t.Fatal("限制 15° 时不应照做 180° 翻转")
	}
	if p2.segs[0].reason == "" {
		t.Fatal("降级必须给出原因，便于日志排查")
	}
	fmt.Printf("[verify] 旋转值域=整圈：默认(-vrot 180)照做 %.1f° 翻转；-vrot 15 则降级(%s)\n",
		t180.angle, p2.segs[0].reason)

	p3 := mkPlan()
	vp3 := newTestPipeline(p3, dir, screen, PlayOptions{VisionMaxRotation: 360})
	p3.activateSeg(0, vp3)
	vp3.Close()
	if p3.segs[0].mode != segSimilar {
		t.Fatal("-vrot 360 也应等价于不限制")
	}
	fmt.Println("[verify] -vrot 360 等价于不限制（360° 与 ±180° 是同一圈）")
}

// TestPairReuseSmallDisplacement 鼠标按下/松开位移极小时，松开那张图必须复用按下那张图的识别结果
func TestPairReuseSmallDisplacement(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	// md 在 (400,300)，mu 只移了 8px —— 两张图几乎是同一张
	md := makeAnchor(t, screen, dir, "mdp", 400, 300, 32, 400, 300)
	mu := makeAnchor(t, screen, dir, "mup", 408, 306, 32, 408, 306)
	plan := planOf([]visionAnchor{md, mu})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionPairTol: 32})
	defer vp.Close()

	vp.ResolveAnchorAtUse(0)
	if !plan.anchors[0].matched {
		t.Fatal("md 应当命中")
	}
	fullAfterMD := int(vp.matcher.full.Load())

	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched {
		t.Fatal("mu 应当命中")
	}
	if plan.anchors[1].tgtX != 408 || plan.anchors[1].tgtY != 306 {
		t.Fatalf("mu 位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if int(vp.matcher.pairHit.Load()) != 1 {
		t.Fatalf("mu 应当走同键对复用: pairHit=%d", vp.matcher.pairHit.Load())
	}
	if int(vp.matcher.full.Load()) != fullAfterMD {
		t.Fatal("同键对复用命中后不应再执行全屏识别")
	}
	// 复用时的搜索区域只需覆盖 位移+容差（远小于快速区域 160px）
	region := regionAround(400, 300, 32, 6+8, 900, 640)
	if region.Dx() != 32+2*14 {
		t.Fatalf("同键对复用区域尺寸异常: %d", region.Dx())
	}
	if region.Dx() >= 32*4 {
		t.Fatalf("同键对复用区域应当明显小于快速区域: %d", region.Dx())
	}
	fmt.Printf("[verify] md 走全屏；mu 复用按下结果命中 (%d,%d)，pairHit=%d，无新增全屏识别 (复用区域仅 %dpx)\n",
		plan.anchors[1].tgtX, plan.anchors[1].tgtY, vp.matcher.pairHit.Load(), region.Dx())
}

// TestPairReuseSkippedWhenFar 位移超过阈值时必须放弃复用，回到常规识别路径
func TestPairReuseSkippedWhenFar(t *testing.T) {
	screen := makeSyntheticScreen(1200, 800)
	defer screen.Close()
	dir := t.TempDir()

	md := makeAnchor(t, screen, dir, "mdf", 200, 200, 32, 200, 200)
	mu := makeAnchor(t, screen, dir, "muf", 900, 600, 32, 900, 600)
	plan := planOf([]visionAnchor{md, mu})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionPairTol: 32})
	defer vp.Close()

	vp.ResolveAnchorAtUse(0)
	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched {
		t.Fatal("mu 应当命中")
	}
	if plan.anchors[1].tgtX != 900 || plan.anchors[1].tgtY != 600 {
		t.Fatalf("mu 位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if int(vp.matcher.pairHit.Load()) != 0 {
		t.Fatal("位移超过阈值时不应使用同键对复用")
	}
	// 只要不是"同键对复用"、且位置正确即可（至于是快速区域/预测区域/全屏哪一级接住不限）
	if int(vp.matcher.cacheHit.Load())+int(vp.matcher.full.Load()) == 0 {
		t.Fatal("位移大时也应当由常规路径的某一级完成")
	}
	fmt.Printf("[verify] 位移 700px 超过阈值 => 不走复用，改由快速区域命中 (%d,%d)\n",
		plan.anchors[1].tgtX, plan.anchors[1].tgtY)
}

// TestCacheRefreshedAfterRecheckCommit 通过"复核通过"确认的关键点也必须刷新快速区域缓存
func TestCacheRefreshedAfterRecheckCommit(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	a0 := makeAnchor(t, screen, dir, "c0", 300, 220, 32, 300, 220)
	a1 := makeAnchor(t, screen, dir, "c1", 320, 240, 32, 320, 240)
	plan := planOf([]visionAnchor{a0, a1})
	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	defer vp.Close()

	// 人为给锚点 1 一个正确的预取候选，让它走"复核通过"分支
	vp.mu.Lock()
	plan.anchors[1].hasCand = true
	plan.anchors[1].candX, plan.anchors[1].candY = 320, 240
	vp.mu.Unlock()

	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched || !plan.anchors[1].fast {
		t.Fatal("应当由预取候选复核通过后采用")
	}
	vp.mu.Lock()
	hasCache, cache := vp.hasCache, vp.cache
	vp.mu.Unlock()
	if !hasCache {
		t.Fatal("复核通过确认后也必须刷新快速区域缓存")
	}
	if cx, cy := (cache.Min.X+cache.Max.X)/2, (cache.Min.Y+cache.Max.Y)/2; cx != 320 || cy != 240 {
		t.Fatalf("缓存中心应等于刚确认的位置: (%d,%d)", cx, cy)
	}
	fmt.Println("[verify] 复核通过分支同样刷新了快速区域缓存 (中心 320,240)")
}

// locateFullOn 用匹配器在全屏范围内定位锚点（测试用，等价于"标准识别"）
func locateFullOn(m *visionMatcher, screen gocv.Mat, a *visionAnchor) {
	x, y, score, ok := m.searchIn(screen, a.img, a.iox, a.ioy, a.mode == GoInput.VisionModeColor, image.Rectangle{})
	a.resolved = true
	a.matched = ok
	a.fast = false
	a.score = score
	if ok {
		a.tgtX, a.tgtY = x, y
	} else {
		a.tgtX, a.tgtY = a.rawX, a.rawY
	}
}

// newTestPipeline 构建一条只作用于合成画面的流水线
func newTestPipeline(plan *visionPlan, dir string, screen gocv.Mat, opts PlayOptions) *visionPipeline {
	m := newVisionMatcher(opts, dir)
	vp := newVisionPipeline(plan, m, opts)
	vp.screenW, vp.screenH = screen.Cols(), screen.Rows()
	// 每次抓屏返回一份独立副本，避免被 Close 影响
	vp.capture = func() (gocv.Mat, error) { return screen.Clone(), nil }
	return vp
}

// makeAnchor 由合成画面上的一个位置构造关键点
func makeAnchor(t *testing.T, screen gocv.Mat, dir, img string, cx, cy, size int, rawX, rawY int) visionAnchor {
	t.Helper()
	extractCentered(t, screen, dir, img, cx, cy, size)
	return visionAnchor{img: img, iox: size / 2, ioy: size / 2, size: size, mode: "image",
		rawX: rawX, rawY: rawY, tgtX: rawX, tgtY: rawY}
}

// planOf 把若干关键点包成最小可用的 visionPlan（每个相邻对一段）
func planOf(anchors []visionAnchor) *visionPlan {
	p := &visionPlan{
		anchors:  anchors,
		anchorOf: map[int]int{},
	}
	for i := range anchors {
		anchors[i].frame = i
		p.anchorOf[i] = i
		// 相邻关键点默认连通
		if i+1 < len(anchors) {
			p.links = append(p.links, true)
			p.segs = append(p.segs, visionSeg{a: i, b: i + 1, from: i, to: i + 1})
		}
	}
	p.blockOf = make([]int, len(anchors))
	for i := range p.blockOf {
		p.blockOf[i] = -1
	}
	return p
}

// TestPipelinePrefetchThenRecheck 预取出的候选必须能在采用前被复核通过并直接采用
func TestPipelinePrefetchThenRecheck(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	// 锚点 0 已确认在 (200,150)；锚点 1 的真实位置是 (600,420)
	a0 := makeAnchor(t, screen, dir, "a0", 200, 150, 32, 300, 250)
	a1 := makeAnchor(t, screen, dir, "a1", 600, 420, 32, 640, 460)
	plan := planOf([]visionAnchor{a0, a1})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionLookahead: 4, VisionCacheFactor: 4})
	defer vp.Close()

	// 锚点 0 先标准识别（缓存区还没建立）
	vp.ResolveAnchorAtUse(0)
	if !plan.anchors[0].matched {
		t.Fatal("锚点0 标准识别应当命中")
	}
	if plan.anchors[0].tgtX != 200 || plan.anchors[0].tgtY != 150 {
		t.Fatalf("锚点0 位置错误: (%d,%d)", plan.anchors[0].tgtX, plan.anchors[0].tgtY)
	}

	// 以锚点 0 为基准，预取锚点 1（预测位置 = 识别位置 + 录制位移）
	vp.mu.Lock()
	pred := plan.predictionFor(1)
	vp.mu.Unlock()
	if !pred.has {
		t.Fatal("锚点1 应当有预测")
	}
	vp.schedule(1)
	// 等预取完成
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		vp.mu.Lock()
		hasCand := plan.anchors[1].hasCand
		vp.mu.Unlock()
		if hasCand {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	vp.mu.Lock()
	candX, candY, hasCand := plan.anchors[1].candX, plan.anchors[1].candY, plan.anchors[1].hasCand
	vp.mu.Unlock()
	if !hasCand {
		t.Fatal("预取应当产出候选")
	}
	if candX != 600 || candY != 420 {
		t.Fatalf("预取候选位置错误: (%d,%d)", candX, candY)
	}
	fmt.Printf("[verify] 预取候选 (%d,%d) 已就绪\n", candX, candY)

	fullBefore := int(vp.matcher.full.Load())
	verifyBefore := int(vp.matcher.verify.Load())
	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched || !plan.anchors[1].fast {
		t.Fatalf("锚点1 应当由预取候选复核后直接采用: matched=%v fast=%v",
			plan.anchors[1].matched, plan.anchors[1].fast)
	}
	if plan.anchors[1].tgtX != 600 || plan.anchors[1].tgtY != 420 {
		t.Fatalf("锚点1 位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if int(vp.matcher.verify.Load()) != verifyBefore+1 {
		t.Fatalf("应当恰好复核一次: %d -> %d", verifyBefore, int(vp.matcher.verify.Load()))
	}
	if int(vp.matcher.full.Load()) != fullBefore {
		t.Fatal("复核通过时不应再执行全屏识别")
	}
	fmt.Printf("[verify] 采用前复核通过并直接采用，未新增全屏识别 (全屏次数仍为 %d)\n", int(vp.matcher.full.Load()))
}

// TestPipelineRecheckFailsFallback 候选失效时必须回退标准识别并仍然找对位置
func TestPipelineRecheckFailsFallback(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	a0 := makeAnchor(t, screen, dir, "a0", 200, 150, 32, 300, 250)
	a1 := makeAnchor(t, screen, dir, "a1", 600, 420, 32, 640, 460)
	plan := planOf([]visionAnchor{a0, a1})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	defer vp.Close()
	plan.anchors[0].resolved, plan.anchors[0].matched = true, true
	plan.anchors[0].tgtX, plan.anchors[0].tgtY = 200, 150

	// 人为写入一个错误候选（离真实位置很远）
	vp.mu.Lock()
	plan.anchors[1].hasCand = true
	plan.anchors[1].candX, plan.anchors[1].candY = 800, 100
	vp.mu.Unlock()

	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched {
		t.Fatal("候选失效后回退标准识别应当命中")
	}
	// 回退后由"受限区域/全屏"重新定位。区域结果会带 fast 标记（到达自身时刻再复核一次），
	// 所以这里只要求：位置被纠正、不再沿用那个失效候选、且通过置信度门槛。
	if plan.anchors[1].tgtX != 600 || plan.anchors[1].tgtY != 420 {
		t.Fatalf("回退后位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if plan.anchors[1].candX == plan.anchors[1].tgtX && plan.anchors[1].candY == plan.anchors[1].tgtY {
		t.Fatal("不应沿用复核否决掉的候选")
	}
	if plan.anchors[1].score < vp.matcher.confirmPercent {
		t.Fatalf("重新定位的结果应当通过置信度门槛: %.1f%%", plan.anchors[1].score)
	}
	if int(vp.matcher.recheckNo.Load()) != 1 {
		t.Fatalf("应记录一次复核失败: %d", int(vp.matcher.recheckNo.Load()))
	}
	fmt.Printf("[verify] 候选失效 => 复核否决并回退标准识别 (%d,%d) 匹配度=%.1f%%，全屏识别 %d 次\n",
		plan.anchors[1].tgtX, plan.anchors[1].tgtY, plan.anchors[1].score, int(vp.matcher.full.Load()))
}

// TestPipelineFastRegionCache 上一个关键点识别成功后留下的快速区域必须被后续关键点利用
func TestPipelineFastRegionCache(t *testing.T) {
	screen := makeSyntheticScreen(2000, 1200)
	defer screen.Close()
	dir := t.TempDir()

	// a0 在 (300,300)，a1 在 (360,340)：相距很近，落在 4 倍边长的快速区域内
	a0 := makeAnchor(t, screen, dir, "a0", 300, 300, 32, 300, 300)
	a1 := makeAnchor(t, screen, dir, "a1", 360, 340, 32, 360, 340)
	plan := planOf([]visionAnchor{a0, a1})

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionCacheFactor: 4})
	defer vp.Close()

	vp.ResolveAnchorAtUse(0)
	if !plan.anchors[0].matched {
		t.Fatal("锚点0 应当命中")
	}
	vp.mu.Lock()
	hasCache, cache := vp.hasCache, vp.cache
	vp.mu.Unlock()
	if !hasCache {
		t.Fatal("锚点0 命中后应当留下快速区域缓存")
	}
	if cache.Dx() != 32*4 {
		t.Fatalf("快速区域边长应为样本的 4 倍 (%d)，实际 %d", 32*4, cache.Dx())
	}
	fmt.Printf("[verify] 快速区域 %v (边长 %d = 样本 32 × 4)\n", cache, cache.Dx())

	fullBefore := int(vp.matcher.full.Load())
	vp.ResolveAnchorAtUse(1)
	if !plan.anchors[1].matched {
		t.Fatal("锚点1 应当命中")
	}
	if plan.anchors[1].tgtX != 360 || plan.anchors[1].tgtY != 340 {
		t.Fatalf("锚点1 位置错误: (%d,%d)", plan.anchors[1].tgtX, plan.anchors[1].tgtY)
	}
	if int(vp.matcher.cacheHit.Load()) == 0 {
		t.Fatal("锚点1 应当至少命中过一次快速区域")
	}
	if int(vp.matcher.full.Load()) != fullBefore {
		t.Fatalf("快速区域命中后不应再全屏识别: %d -> %d", fullBefore, int(vp.matcher.full.Load()))
	}
	fmt.Printf("[verify] 锚点1 由快速区域命中，未执行全屏识别 (快速区域命中 %d 次)\n", int(vp.matcher.cacheHit.Load()))
}

// TestPipelinePrefetchWindowSlide 预取窗口滑动必须取消窗口外的在途任务
func TestPipelinePrefetchWindowSlide(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()

	anchors := make([]visionAnchor, 0, 8)
	for i := 0; i < 8; i++ {
		anchors = append(anchors, makeAnchor(t, screen, dir, fmt.Sprintf("a%d", i), 100+i*60, 100+i*40, 32, 100+i*60, 100+i*40))
	}
	plan := planOf(anchors)
	plan.anchors[0].resolved, plan.anchors[0].matched = true, true
	plan.anchors[0].tgtX, plan.anchors[0].tgtY = anchors[0].tgtX, anchors[0].tgtY

	vp := newTestPipeline(plan, dir, screen, PlayOptions{VisionLookahead: 3})
	defer vp.Close()

	vp.schedule(1)
	vp.mu.Lock()
	n1 := len(vp.jobs)
	vp.mu.Unlock()
	if n1 == 0 || n1 > 3 {
		t.Fatalf("预取窗口应为 3，实际在途 %d", n1)
	}
	// 窗口前移
	vp.schedule(4)
	vp.mu.Lock()
	keys := make([]int, 0, len(vp.jobs))
	for k := range vp.jobs {
		if k < 4 || k >= 7 {
			t.Errorf("窗口滑动后不应残留窗口外的任务: 锚点#%d", k)
		}
		keys = append(keys, k)
	}
	vp.mu.Unlock()
	fmt.Printf("[verify] 预取窗口滑动：初始在途 %d 个 -> 前移后 %v (仅窗口内)\n", n1, keys)
}

// TestPipelineCloseCancels 关闭流水线必须取消全部在途预取且不 panic
func TestPipelineCloseCancels(t *testing.T) {
	screen := makeSyntheticScreen(900, 640)
	defer screen.Close()
	dir := t.TempDir()
	var anchors []visionAnchor
	for i := 0; i < 6; i++ {
		anchors = append(anchors, makeAnchor(t, screen, dir, fmt.Sprintf("c%d", i), 200+i*50, 200+i*30, 32, 200+i*50, 200+i*30))
	}
	plan := planOf(anchors)
	vp := newTestPipeline(plan, dir, screen, PlayOptions{})
	plan.anchors[0].resolved, plan.anchors[0].matched = true, true
	vp.schedule(1)
	vp.Close()
	vp.mu.Lock()
	n := len(vp.jobs)
	vp.mu.Unlock()
	if n != 0 {
		t.Fatalf("关闭后不应残留在途任务: %d", n)
	}
	// 关闭后再调用应当安全返回
	vp.ResolveAnchorAtUse(2)
	vp.ConfirmAtFrame(2)
	fmt.Println("[verify] 流水线关闭后：在途任务清零，后续调用安全返回")
}

// TestRegionGeometry 快速区域/复核区域的几何计算
func TestRegionGeometry(t *testing.T) {
	r := regionAround(500, 400, 32, 12, 2000, 1200)
	if r.Dx() != 32+24 || r.Dy() != 32+24 {
		t.Fatalf("复核区域尺寸错误: %v", r)
	}
	if cx, cy := (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2; cx != 500 || cy != 400 {
		t.Fatalf("复核区域未居中: (%d,%d)", cx, cy)
	}
	// 贴边必须裁剪到画面内
	e := regionAround(2, 2, 32, 12, 2000, 1200)
	if e.Min.X < 0 || e.Min.Y < 0 {
		t.Fatalf("复核区域未裁剪: %v", e)
	}
	// 快速区域随预测位置平移。缓存本身是"光标空间"窗口（128），
	// 真正拿去搜索时按模板尺寸外扩，因此实际 ROI 是 128+32=160，
	// 覆盖"真实位置偏离预测 ±64"的情形。
	cache := image.Rect(0, 0, 128, 128)
	mv := moveRegion(cache, 1000, 800, 32, 2000, 1200)
	if mv.Dx() != 128+32 {
		t.Fatalf("实际搜索 ROI 应为 缓存边长+模板边长 = 160，实际 %d", mv.Dx())
	}
	if cx, cy := (mv.Min.X+mv.Max.X)/2, (mv.Min.Y+mv.Max.Y)/2; cx != 1000 || cy != 800 {
		t.Fatalf("快速区域未跟随预测位置: (%d,%d)", cx, cy)
	}
	_ = os.Stdout
	_ = filepath.Join
	_ = GoVision.ClampROI
	_ = GoInput.VisionModeColor
}
