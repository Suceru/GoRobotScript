package GoRunner

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoVision"

	"gocv.io/x/gocv"
)

// ---------------------------------------------------------------------------
// 识别流水线：读入内存的关键点 + 快速区域缓存 + 后台预取 + 采用前复核
//
// 设计目标：把"昂贵的搜索"挪到回放的空档里，回放主线上只留一次廉价复核。
//
//	① 预取：脚本后续若干关键点已经被读入内存，并交由后台协程提前识别出
//	   一个大概位置（候选）；
//	② 快速区域：上一个关键点识别成功后会留下一个**边长 4 倍于样本**的快速
//	   区域，下一个关键点优先只在这个区域里找，代价比全屏低一到两个数量级；
//	③ 复核：真正轮到某个关键点时，用**实时画面**在候选位置附近确认一次，
//	   通过就直接采用（完成加速），不通过立刻回退标准识别；
//	④ 窗口滑动：滑出预取窗口的在途协程会被取消，不做无用功。
// ---------------------------------------------------------------------------

// 识别流水线默认参数
const (
	DefaultVisionCacheFactor = 4  // 快速区域边长 = 样本边长 × 该系数
	DefaultVisionSimilarity  = 85 // 默认匹配度阈值 (百分比，0~100，越大越严格)
	DefaultVisionBlur        = 3  // 匹配前高斯模糊核 (抵消模糊/锐化/降分辨率造成的像素差异)
	DefaultVisionLookahead   = 5  // 预取窗口：提前识别后续多少个关键点
	DefaultVisionFastTol     = 6  // 复核位置容差 (像素)
	DefaultVisionPairTol     = 32 // 同键对复用阈值：md/mu 位移不超过它才复用上一张图的识别结果
	DefaultVisionMaxParallel = 2  // 允许同时进行的全屏识别数

	// DefaultVisionRegionConfirm 受限区域结果的置信度门槛 (百分比，0 = 关闭门控)。
	//
	// 通用规则：**区域里的"局部最优"不等于"全局最优"**。
	// 任何缩小的搜索范围（同键对复用区域 / 快速区域缓存 / 预测附近 / 就近复核）
	// 里都可能恰好只落进一个"长得像"的错误目标，而真正的目标在区域之外 ——
	// 此时它照样能超过阈值，于是识别会稳定地停在错误位置上（路径也因此走错方向）。
	// 这不是某种界面的特例，而是所有"先在小范围里找"的策略共有的风险。
	//
	// 因此：受限区域的结果必须达到更高的置信度才敢采纳；达不到就扩大范围重找，
	// 直到全屏 —— 全屏是**全局最优**，不存在"区域外还有更像的"问题，只要求阈值。
	DefaultVisionRegionConfirm = 97

	// visionRegionExpandFactor 置信度不足时把搜索范围放大的倍数（逐级扩大）。
	visionRegionExpandFactor = 3

	// visionTrustMargin 关闭区域门控时，"够可信"要求比匹配度阈值高出的百分点。
	visionTrustMargin = 5.0

	// DefaultVisionSettleMs 点击/松开前，光标刚被复核修正挪动过时的默认等待 (毫秒)。
	//
	// 单靠 SetCursorPos 是"瞬移"：界面往往要等下一帧才更新悬停高亮/焦点，
	// 瞬移后立刻按下，这次点击可能仍被算在旧位置上。留一点时间即可避免。
	DefaultVisionSettleMs = 30
)

// absInt 绝对值
func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// visionDebug 置上 GOROBOT_VISION_DEBUG=1 可打印流水线每一步决策，
// 便于在游戏里排查"为什么没走快速区域 / 为什么回退"。
var visionDebug = os.Getenv("GOROBOT_VISION_DEBUG") != ""

func vdebug(format string, args ...any) {
	if visionDebug {
		fmt.Printf("[Vision/debug] "+format+"\n", args...)
	}
}

// anchorPred 一个关键点的预测信息
type anchorPred struct {
	x, y    int           // 预测的光标位置
	has     bool          // 是否有预测（false => 只能标准识别）
	witness *visionAnchor // 预测所依据的锚点（可作为交叉确认的标记来源）
}

// ---------------------------------------------------------------------------
// visionMatcher：匹配执行器（只负责"在什么范围里找"，不持有锚点状态）
// ---------------------------------------------------------------------------

type visionMatcher struct {
	dir              string
	thresholdPercent float64 // 匹配度阈值 (0~100)，越大越严格
	confirmPercent   float64 // 受限区域结果的置信度门槛 (0 = 关闭门控)
	minScale         float64
	maxScale         float64
	step             float64
	blur             int // 匹配前高斯模糊核 (抵消模糊/锐化/降分辨率带来的像素差异)

	// 统计（会被预取协程并发写入，必须用原子计数）
	full       atomic.Int64 // 全屏标准识别次数
	cacheTry   atomic.Int64 // 快速区域尝试次数
	cacheHit   atomic.Int64 // 快速区域命中次数
	regionLow  atomic.Int64 // 受限区域命中但置信度不足 => 扩大搜索范围的次数
	prefetch   atomic.Int64 // 后台预取实际执行的识别次数
	prefetchOK atomic.Int64
	verify     atomic.Int64 // 采用前的实时复核次数
	recheckOK  atomic.Int64 // 复核通过 (加速)
	recheckNo  atomic.Int64 // 复核失败 (回退)
	pairHit    atomic.Int64 // 同键对(并发扫描/复用)命中次数
	pairTry    atomic.Int64 // 同键对并发扫描未命中次数
}

func newVisionMatcher(opts PlayOptions, dir string) *visionMatcher {
	m := &visionMatcher{
		dir:              dir,
		thresholdPercent: opts.VisionSimilarity,
		confirmPercent:   opts.VisionRegionConfirm,
		minScale:         opts.VisionMinScale,
		maxScale:         opts.VisionMaxScale,
		step:             opts.VisionScaleStep,
		blur:             opts.VisionBlur,
	}
	if m.thresholdPercent <= 0 {
		m.thresholdPercent = DefaultVisionSimilarity
	}
	if m.thresholdPercent > 100 {
		m.thresholdPercent = 100
	}
	if opts.VisionRegionConfirm == 0 {
		m.confirmPercent = DefaultVisionRegionConfirm // 未设置 => 用默认门槛
	}
	if m.confirmPercent < 0 {
		m.confirmPercent = 0 // 显式关闭门控
	}
	if m.confirmPercent > 100 {
		m.confirmPercent = 100
	}
	if m.blur < 0 {
		m.blur = 0
	}
	if m.minScale <= 0 {
		m.minScale = 0.9
	}
	if m.maxScale <= 0 {
		m.maxScale = 1.1
	}
	if m.maxScale < m.minScale {
		m.minScale, m.maxScale = m.maxScale, m.minScale
	}
	if m.step <= 0 {
		m.step = 0.05
	}
	return m
}

// options 回放识别统一使用 ZNCC(零均值归一化互相关) + 轻度预模糊：
//   - ZNCC：看结构/特征，天然给出 0~1 的匹配度；对整体亮度/对比度变化免疫
//   - 预模糊：抵消动态模糊、局部锐化、分辨率降低造成的逐像素差异
func (m *visionMatcher) options() GoVision.MatchOptions {
	return GoVision.MatchOptions{
		MinScale:  m.minScale,
		MaxScale:  m.maxScale,
		ScaleStep: m.step,
		Metric:    GoVision.MetricCCoeffNormed,
		Blur:      m.blur,
	}
}

// searchIn 在给定 ROI (零值 = 整屏) 内匹配样本，返回光标位置与分数。
func (m *visionMatcher) searchIn(screen gocv.Mat, img string, iox, ioy int, color bool, roi image.Rectangle) (x, y int, score float64, matched bool) {
	if m.dir == "" || img == "" || screen.Empty() {
		return 0, 0, 0, false
	}
	tpl, err := GoVision.LoadTemplate(filepath.Join(m.dir, img+".png"))
	if err != nil {
		return 0, 0, 0, false
	}
	defer tpl.Close()

	opts := m.options()
	var res GoVision.MatchResult
	if roi.Dx() <= 0 || roi.Dy() <= 0 {
		if color {
			res, err = GoVision.ColorConvolutionMatch(screen, tpl, 3, 3, opts)
		} else {
			res, err = GoVision.MatchTemplate(screen, tpl, opts)
		}
	} else {
		if color {
			res, err = GoVision.ColorConvolutionMatchROI(screen, tpl, roi, 3, 3, opts)
		} else {
			res, err = GoVision.MatchTemplateROI(screen, tpl, roi, opts)
		}
	}
	if err != nil {
		return 0, 0, 0, false
	}
	// 命中区域是按 scale 缩放后的样本尺寸，样本内偏移需同步缩放
	scale := 1.0
	if tpl.Cols() > 0 {
		scale = float64(res.Width) / float64(tpl.Cols())
	}
	x = res.X + int(math.Round(float64(iox)*scale))
	y = res.Y + int(math.Round(float64(ioy)*scale))
	return x, y, res.Percent, res.Percent >= m.thresholdPercent
}

// acceptRegion 受限区域(非全屏)的搜索结果是否已经足够可信。
//
// 见 DefaultVisionRegionConfirm 的说明：区域内的最优只是"局部最优"，
// 区域外可能还有更像的（例如界面上有一堆长得差不多的格子/图标/按钮）。
// 达不到置信度就不要采纳，交给更宽的搜索范围，最后落到全屏的**全局最优**。
func (m *visionMatcher) acceptRegion(percent float64) bool {
	return m.confirmPercent <= 0 || percent >= m.confirmPercent
}

// endpointTrusted 某个关键点的识别结果是否"足够可信"，可以作为路径对齐的依据。
//
// 用途见 vision_align.go 的 activateSeg：两端都可信时，两端之间的位移比例
// （录制时相距很远、这一轮只相距一格，或反过来）是**布局重排的真实结果**，
// 不能拿几何比例去否决它，否则路径会朝录制时的旧位置走（看着像朝目标反方向走）、
// 到点击那一刻才跳回正确位置。
//
// 判定标准与"受限区域置信度门控"共用一条线（`-vconf`，默认 97%）：
// 达到它就是高置信度命中；门控关闭时要求比阈值高一点（勉强通过对的不算）。
func (vp *visionPipeline) endpointTrusted(a visionAnchor) bool {
	if !a.matched {
		return false
	}
	line := vp.matcher.confirmPercent
	if line <= 0 {
		line = vp.matcher.thresholdPercent + visionTrustMargin
	}
	return a.score >= line
}

// ---------------------------------------------------------------------------
// visionPipeline：识别流水线
// ---------------------------------------------------------------------------

type visionPipeline struct {
	plan    *visionPlan
	matcher *visionMatcher

	lookahead   int
	cacheRatio  int
	tol         int
	pairTol     int     // 同键对复用阈值：md/mu 位移不超过它才认为"是同一张图"
	maxRotation float64 // 允许的最大旋转角 (度)，超出即放弃旋转/拉伸
	screenW     int
	screenH     int

	mu       sync.Mutex
	cache    image.Rectangle // 快速区域缓存（零值 = 无）
	hasCache bool
	gen      int64                 // 预测世代：基准变化时递增
	jobs     map[int]chan struct{} // 锚点 -> 在途预取的取消通道
	closed   bool
	wg       sync.WaitGroup
	fullSem  chan struct{} // 全屏识别并发限流

	// capture 画面来源，默认抓真实屏幕；测试可注入合成画面
	capture   func() (gocv.Mat, error)
	captureMu sync.Mutex
}

func newVisionPipeline(plan *visionPlan, m *visionMatcher, opts PlayOptions) *visionPipeline {
	vp := &visionPipeline{
		plan:        plan,
		matcher:     m,
		lookahead:   opts.VisionLookahead,
		cacheRatio:  opts.VisionCacheFactor,
		tol:         opts.VisionFastTol,
		maxRotation: opts.VisionMaxRotation,
		jobs:        make(map[int]chan struct{}),
		fullSem:     make(chan struct{}, DefaultVisionMaxParallel),
	}
	if vp.lookahead <= 0 {
		vp.lookahead = DefaultVisionLookahead
	}
	if vp.cacheRatio < 2 {
		vp.cacheRatio = DefaultVisionCacheFactor
	}
	if vp.tol <= 0 {
		vp.tol = DefaultVisionFastTol
	}
	if vp.maxRotation <= 0 {
		vp.maxRotation = DefaultVisionMaxRotation
	}
	vp.pairTol = opts.VisionPairTol
	if vp.pairTol <= 0 {
		vp.pairTol = DefaultVisionPairTol
	}
	vp.screenW, vp.screenH = GoVision.ScreenSize()
	vp.capture = GoVision.CaptureFullScreenMat
	return vp
}

// grab 取一帧画面。抓屏走 GDI，多个预取协程同时抓屏没有必要也容易踩重入，
// 因此串行化（单次 ~10ms，相对于识别本身可以忽略）。
func (vp *visionPipeline) grab() (gocv.Mat, error) {
	vp.captureMu.Lock()
	defer vp.captureMu.Unlock()
	if vp.capture == nil {
		return GoVision.CaptureFullScreenMat()
	}
	return vp.capture()
}

// Close 取消全部在途预取并等待退出
func (vp *visionPipeline) Close() {
	if vp == nil {
		return
	}
	vp.mu.Lock()
	vp.closed = true
	for k, ch := range vp.jobs {
		close(ch)
		delete(vp.jobs, k)
	}
	vp.mu.Unlock()
	vp.wg.Wait()
}

// ---------------------------------------------------------------------------
// 回放主线接口
// ---------------------------------------------------------------------------

// ResolveAnchorAtUse 采用前解析锚点 k：
//  1. 有预取候选 => 用实时画面在候选附近复核（廉价），通过即采用；
//  2. 无候选或复核失败 => 标准识别（先试快速区域，再全屏）。
func (vp *visionPipeline) ResolveAnchorAtUse(k int) {
	if vp == nil {
		return
	}
	vp.mu.Lock()
	if vp.closed || k < 0 || k >= len(vp.plan.anchors) || vp.plan.anchors[k].resolved {
		vp.mu.Unlock()
		return
	}
	a := vp.plan.anchors[k]
	hasCand := a.hasCand
	candX, candY := a.candX, a.candY
	img, iox, ioy, mode, size := a.img, a.iox, a.ioy, a.mode, a.size
	vp.mu.Unlock()

	if hasCand {
		if screen, err := vp.grab(); err == nil {
			vp.matcher.verify.Add(1)
			region := regionAround(candX, candY, size, vp.tol*2, vp.screenW, vp.screenH)
			x, y, score, ok := vp.matcher.searchIn(screen, img, iox, ioy, mode == GoInput.VisionModeColor, region)
			screen.Close()
			if ok && vp.matcher.acceptRegion(score) && absInt(x-candX) <= vp.tol && absInt(y-candY) <= vp.tol {
				vp.matcher.recheckOK.Add(1)
				vp.commit(k, x, y, score, true)
				vdebug("锚点#%d 预取候选复核通过 (%d,%d) 匹配度=%.1f%%", k, x, y, score)
				return
			}
			vp.matcher.recheckNo.Add(1)
			vdebug("锚点#%d 预取候选复核未通过 (候选 %d,%d -> 实测 %d,%d 匹配度=%.1f%%) => 标准识别",
				k, candX, candY, x, y, score)
		}
	} else {
		vdebug("锚点#%d 无预取候选 => 标准识别", k)
	}

	vp.standardResolve(k)
}

// ConfirmAtFrame 关键点到达自身时刻做最终确认。
//
// 段终点是在段起点处提前确认的（提前快速标记位置）。到达该关键点时：
//   - 此前是快速得到的坐标 => 就近复核（容差不够就放宽一档再试一次）；
//   - 复核通过就沿用（完成加速）；两次都不过 => **保留原标记，不推翻它**；
//   - 此前根本没识别成功 => 回退标准识别。
//
// 关键原则：**已经匹配成功的结果不因邻居或复核而被丢掉**。
// 某个关键点失败只回退它自己；其它关键点成功的结果继续作为参考。
func (vp *visionPipeline) ConfirmAtFrame(frameIdx int) {
	if vp == nil || vp.plan == nil {
		return
	}
	k, ok := vp.plan.anchorOf[frameIdx]
	if !ok {
		return
	}
	vp.mu.Lock()
	if vp.closed || !vp.plan.anchors[k].resolved || !vp.plan.anchors[k].early {
		vp.mu.Unlock()
		return
	}
	a := vp.plan.anchors[k]
	wasFast, wasMatched := a.fast, a.matched
	tgtX, tgtY, prevSim := a.tgtX, a.tgtY, a.score
	img, iox, ioy, mode, size := a.img, a.iox, a.ioy, a.mode, a.size
	vp.mu.Unlock()

	if !wasMatched {
		vp.standardResolve(k) // 之前没识别出来，这里补一次标准识别
		return
	}
	if !wasFast {
		return // 标准识别得到的结果，无需再验
	}

	screen, err := vp.grab()
	if err != nil {
		return
	}
	defer screen.Close()

	// 先用窄区域复核；不行再放宽一档。
	// 复核区域同样很小，也可能只圈到一个相似邻居，所以同样要过置信度门槛。
	var (
		lastX, lastY = tgtX, tgtY
		lastSim      float64
		lastOK       bool
	)
	for _, radius := range []int{vp.tol * 2, vp.tol * 4} {
		vp.matcher.verify.Add(1)
		region := regionAround(tgtX, tgtY, size, radius, vp.screenW, vp.screenH)
		x, y, sim, ok := vp.matcher.searchIn(screen, img, iox, ioy, mode == GoInput.VisionModeColor, region)
		lastX, lastY, lastSim, lastOK = x, y, sim, ok
		if ok && vp.matcher.acceptRegion(sim) && absInt(x-tgtX) <= vp.tol && absInt(y-tgtY) <= vp.tol {
			vp.matcher.recheckOK.Add(1)
			vp.commit(k, x, y, sim, true)
			vdebug("锚点#%d 标记复核通过 (%d,%d) 匹配度=%.1f%%", k, x, y, sim)
			return
		}
	}
	// 就近复核都没通过（容差不够，或区域里那个"长得像"的目标置信度不足）
	// => 这个提前标记不可信，必须重新定位。
	// 标准识别会从窄到宽逐级扩大搜索范围，最终落到全屏的全局最优，
	// 因此能找到真正的目标；位置会跳一下，但跳到的是对的地方。
	vp.matcher.recheckNo.Add(1)
	vdebug("锚点#%d 就近复核未通过 (原标记 %d,%d 存储匹配度=%.1f%%；复核 %d,%d 匹配度=%.1f%% 命中=%v，门槛 %.0f%%) => 重新定位",
		k, tgtX, tgtY, prevSim, lastX, lastY, lastSim, lastOK, vp.matcher.confirmPercent)
	vp.standardResolve(k)
}

// standardResolve 标准识别。按成本从低到高、范围从小到大逐级尝试：
//
//	① 同键对复用：与上一个关键点位移极小 => 两张近乎同一张图，只在小区域里复核；
//	② 快速区域缓存：上一次成功识别留下的 size×cacheRatio 区域；
//	③ 扩大区域：预测位置附近更大的范围（仍是全屏的零头）；
//	④ 全屏标准识别（最后手段，限流）。
//
// ②③ 是"受限区域"，必须达到置信度门槛才采纳（见 DefaultVisionRegionConfirm）：
// 区域内的最优可能只是局部最优，真正的目标也许在区域外，此时扩大范围重找。
// ④ 是全屏全局最优，只要求匹配度阈值。
func (vp *visionPipeline) standardResolve(k int) {
	if k < 0 || k >= len(vp.plan.anchors) {
		return
	}
	screen, err := vp.grab()
	if err != nil {
		vp.markRaw(k)
		return
	}
	defer screen.Close()

	vp.mu.Lock()
	a := vp.plan.anchors[k]
	img, iox, ioy, mode, size := a.img, a.iox, a.ioy, a.mode, a.size
	pred := vp.plan.predictionFor(k)
	cache, hasCache := vp.cache, vp.hasCache
	vp.mu.Unlock()
	color := mode == GoInput.VisionModeColor

	// ① 同一键对（按下/松开位移 ≤ -vpair）：两张图本来就是同一处
	if x, y, score, ok, used := vp.tryPairReuse(screen, k, pred, img, iox, ioy, size, color); used {
		if ok {
			vp.matcher.pairHit.Add(1)
			vp.commit(k, x, y, score, true)
			vdebug("锚点#%d 同键对复用命中 (%d,%d) 匹配度=%.1f%%", k, x, y, score)
			return
		}
		// 极小区里没命中 => **不能**再到更大的范围里另找一个位置：
		// 那会让"按下"和"松开"落在两个不同的地方，一次点击就被拆成了一次拖拽
		// （界面表现就是"点击效果不对"）。这一对本来就是同一处，直接按录制位移
		// 与按下那侧保持一致。
		vp.matcher.pairHit.Add(1)
		vp.commit(k, pred.x, pred.y, vp.witnessScore(pred), true)
		vdebug("锚点#%d 同键对极小区未命中 => 按录制位移与上一个关键点保持一致 (%d,%d)", k, pred.x, pred.y)
		return
	}

	// ②③ 受限区域逐级扩大
	for _, tr := range vp.regionTiers(pred, cache, hasCache, size) {
		vp.matcher.cacheTry.Add(1)
		x, y, score, ok := vp.matcher.searchIn(screen, img, iox, ioy, color, tr.region)
		if !ok {
			vdebug("锚点#%d %s %v 未命中 => 扩大范围", k, tr.name, tr.region)
			continue
		}
		if !vp.matcher.acceptRegion(score) {
			vp.matcher.regionLow.Add(1)
			vdebug("锚点#%d %s %v 命中但置信度不足 (%.1f%% < %.0f%%) => 扩大范围",
				k, tr.name, tr.region, score, vp.matcher.confirmPercent)
			continue
		}
		vp.matcher.cacheHit.Add(1)
		vp.commit(k, x, y, score, true)
		vdebug("锚点#%d 标准识别(%s %v)命中 (%d,%d) 匹配度=%.1f%%", k, tr.name, tr.region, x, y, score)
		return
	}

	// ④ 全屏标准识别
	vp.fullSem <- struct{}{}
	x, y, score, ok := vp.matcher.searchIn(screen, img, iox, ioy, color, image.Rectangle{})
	<-vp.fullSem
	vp.matcher.full.Add(1)
	if ok {
		vp.commit(k, x, y, score, false)
		vdebug("锚点#%d 全屏识别命中 (%d,%d) 匹配度=%.1f%%", k, x, y, score)
		return
	}
	vp.markRaw(k)
	vdebug("锚点#%d 全屏识别未命中 (最高匹配度只有 %.1f%%) => 按原坐标回放", k, score)
}

// regionTier 一级受限搜索范围
type regionTier struct {
	name   string
	region image.Rectangle
}

// regionTiers 受限搜索范围的逐级扩大序列（不含全屏）。
//
// 预测位置是有依据的（上一个关键点的实测位置 + 录制位移），所以从小到大试：
// 范围越小越快，但区域外的目标会被漏掉 => 靠"置信度不足就扩大"兜住。
// 与界面类型无关，任何界面都适用。
func (vp *visionPipeline) regionTiers(pred anchorPred, cache image.Rectangle, hasCache bool, size int) []regionTier {
	if !pred.has {
		return nil
	}
	var tiers []regionTier
	if hasCache {
		if r := moveRegion(cache, pred.x, pred.y, size, vp.screenW, vp.screenH); r.Dx() > 0 {
			tiers = append(tiers, regionTier{"快速区域", r})
		}
	}
	side := size * vp.cacheRatio * visionRegionExpandFactor
	if r := regionAround(pred.x, pred.y, side, size/2, vp.screenW, vp.screenH); r.Dx() > 0 {
		// 与上一级等价就不重复试
		if len(tiers) == 0 || r.Dx() > tiers[len(tiers)-1].region.Dx() {
			tiers = append(tiers, regionTier{"扩大区域", r})
		}
	}
	return tiers
}

// witnessScore 取预测来源锚点的匹配度（加锁读取）
func (vp *visionPipeline) witnessScore(pred anchorPred) float64 {
	if pred.witness == nil {
		return 0
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	return pred.witness.score
}

// tryPairReuse 同键对复用：
//
// 鼠标按下与松开之间位移很小时，两张样本图几乎是同一张图。
// 此时不必让第二张再走一遍大范围搜索，而是**直接复用上一张图的识别结果**：
// 以"上一个关键点识别位置 + 录制位移"为中心，只开一块刚好够用的极小区域复核一次。
//
// 位移超过 pairTol 时不走这条路径（偏移太大就不再是"同一张图"了）。
// 这块区域同样很小，因此结果也要过置信度门控（used=true 但 ok=false => 交回上一级）。
func (vp *visionPipeline) tryPairReuse(screen gocv.Mat, k int, pred anchorPred, img string, iox, ioy, size int, color bool) (x, y int, score float64, ok bool, used bool) {
	if !pred.has || pred.witness == nil || !pred.witness.matched {
		return 0, 0, 0, false, false
	}
	vp.mu.Lock()
	a := vp.plan.anchors[k]
	w := pred.witness
	dx := a.rawX - w.rawX
	dy := a.rawY - w.rawY
	vp.mu.Unlock()

	slack := vp.pairTol
	if absInt(dx) > slack || absInt(dy) > slack {
		return 0, 0, 0, false, false // 位移太大：不算"同一张图"
	}

	// 区域只需覆盖"录制位移 + 复核容差"
	radius := vp.tol + maxOf(absInt(dx), absInt(dy))
	region := regionAround(pred.x, pred.y, size, radius, vp.screenW, vp.screenH)
	x, y, score, ok = vp.matcher.searchIn(screen, img, iox, ioy, color, region)
	if ok && !vp.matcher.acceptRegion(score) {
		vp.matcher.regionLow.Add(1)
		vdebug("锚点#%d 同键对复用区域命中但置信度不足 (%.1f%% < %.0f%%) => 扩大范围",
			k, score, vp.matcher.confirmPercent)
		ok = false
	}
	return x, y, score, ok, true
}

// commit 写入识别成功结果，并刷新快速区域缓存供后续关键点复用
func (vp *visionPipeline) commit(k, x, y int, score float64, fast bool) {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if k < 0 || k >= len(vp.plan.anchors) {
		return
	}
	a := &vp.plan.anchors[k]
	// 位置与之前不一致（首次确认、或复核/重新定位改掉了提前标记的位置）
	// => 已经算好的段变换用的是旧位置，必须作废重建。
	changed := !a.matched || a.tgtX != x || a.tgtY != y
	a.resolved = true
	a.matched = true
	a.fast = fast
	a.score = score
	a.tgtX, a.tgtY = x, y
	if changed {
		vp.plan.invalidateSegsUsing(k)
	}
	// 缓存必须以识别位置为中心刷新 —— 无论是复核通过还是标准识别命中，
	// 后续关键点（尤其是同一键对里的另一张图）都靠它省掉大范围搜索。
	vp.cache = regionAround(x, y, a.size*vp.cacheRatio, 0, vp.screenW, vp.screenH)
	vp.hasCache = true
	vp.cancelLocked(k)
}

// markRaw 识别失败：按原坐标回放
func (vp *visionPipeline) markRaw(k int) {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if k < 0 || k >= len(vp.plan.anchors) {
		return
	}
	a := &vp.plan.anchors[k]
	changed := a.tgtX != a.rawX || a.tgtY != a.rawY || a.matched
	a.resolved = true
	a.matched = false
	a.fast = false
	a.tgtX, a.tgtY = a.rawX, a.rawY
	if changed {
		vp.plan.invalidateSegsUsing(k)
	}
	vp.cancelLocked(k)
}

// ---------------------------------------------------------------------------
// 关键点帧的光标位置（回放主循环使用）
//
// 这里解决的是"鼠标按下/松开与识别脱节"：
// 关键点在自己的帧上会被复核甚至重新定位，位置随后才变；
// 若那一刻仍用"之前按旧位置算好的段变换"去摆光标，点击就会落在旧位置上。
// 因此：
//   - KeypointCursor：停顿开始前，先把光标放到该关键点**当前已知**的位置，
//     让它在停顿期间就等在正确的位置上（画面状态、悬停高亮也与录制时一致）；
//   - KeypointTarget：事件真正发出前，取该关键点**最终确认**的位置为准。
// ---------------------------------------------------------------------------

// KeypointCursor 关键点当前已知的最佳光标位置：
// 已确认的识别位置优先，其次是后台预取复核过的候选；都没有则 ok=false。
func (vp *visionPipeline) KeypointCursor(frameIdx int) (int, int, bool) {
	if vp == nil || vp.plan == nil {
		return 0, 0, false
	}
	k, ok := vp.plan.anchorOf[frameIdx]
	if !ok {
		return 0, 0, false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	a := vp.plan.anchors[k]
	if a.matched {
		return a.tgtX, a.tgtY, true
	}
	if a.hasCand {
		return a.candX, a.candY, true
	}
	return 0, 0, false
}

// KeypointTarget 关键点最终确认的识别位置（仅识别成功时 ok=true）。
// 关键点帧的坐标必须走这里，而不是"可能是旧值的段变换"。
func (vp *visionPipeline) KeypointTarget(frameIdx int) (int, int, bool) {
	if vp == nil || vp.plan == nil {
		return 0, 0, false
	}
	k, ok := vp.plan.anchorOf[frameIdx]
	if !ok {
		return 0, 0, false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	a := vp.plan.anchors[k]
	if !a.matched {
		return 0, 0, false
	}
	return a.tgtX, a.tgtY, true
}

// updateCache 用刚确认的锚点刷新快速区域缓存（以它的识别位置为中心）
func (vp *visionPipeline) updateCache(k int) {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if k < 0 || k >= len(vp.plan.anchors) {
		return
	}
	a := &vp.plan.anchors[k]
	if !a.matched {
		vp.hasCache = false
		return
	}
	vp.cache = regionAround(a.tgtX, a.tgtY, a.size*vp.cacheRatio, 0, vp.screenW, vp.screenH)
	vp.hasCache = true
	vp.gen++
}

// AfterSegmentResolved 本段两端已确认：刷新预取窗口
func (vp *visionPipeline) AfterSegmentResolved(si int) {
	if vp == nil || vp.plan == nil || si < 0 || si >= len(vp.plan.segs) {
		return
	}
	vp.schedule(vp.plan.segs[si].b + 1)
}

// schedule 预取从 from 开始的后续关键点（窗口 = lookahead），并取消窗口外的在途任务
func (vp *visionPipeline) schedule(from int) {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.closed {
		return
	}
	n := len(vp.plan.anchors)
	for k, ch := range vp.jobs {
		if k < from || k >= from+vp.lookahead {
			close(ch)
			delete(vp.jobs, k)
			vdebug("取消窗口外的在途预取 锚点#%d", k)
		}
	}
	started := 0
	for k := from; k < from+vp.lookahead && k < n; k++ {
		if vp.plan.anchors[k].resolved || vp.plan.anchors[k].hasCand {
			continue
		}
		if _, running := vp.jobs[k]; running {
			continue
		}
		ch := make(chan struct{})
		vp.jobs[k] = ch
		gen := vp.gen
		vp.wg.Add(1)
		go vp.prefetch(k, gen, ch)
		started++
	}
	if started > 0 {
		vdebug("启动预取 %d 个关键点 (窗口 %d~%d)", started, from, from+vp.lookahead-1)
	}
}

// cancelLocked 取消某个锚点的在途预取（调用方需持有锁）
func (vp *visionPipeline) cancelLocked(k int) {
	if ch, ok := vp.jobs[k]; ok {
		close(ch)
		delete(vp.jobs, k)
	}
}

// prefetch 后台预取：提前把这个关键点的大概位置标出来（候选）。
// 预取结果只作为"大概位置"，真正采用前一定会用实时画面复核一次。
func (vp *visionPipeline) prefetch(k int, gen int64, cancel chan struct{}) {
	defer vp.wg.Done()

	vp.mu.Lock()
	if vp.closed || k < 0 || k >= len(vp.plan.anchors) {
		vp.mu.Unlock()
		return
	}
	a := vp.plan.anchors[k]
	img, iox, ioy, mode, size := a.img, a.iox, a.ioy, a.mode, a.size
	pred := vp.plan.predictionFor(k)
	cache, hasCache := vp.cache, vp.hasCache
	vp.mu.Unlock()

	if !pred.has {
		// 链首没有预测来源：交给主线上的标准识别，预取不做全屏搜索
		return
	}

	select {
	case <-cancel:
		return
	default:
	}

	screen, err := vp.grab()
	if err != nil {
		return
	}
	defer screen.Close()

	color := mode == GoInput.VisionModeColor
	var x, y int
	var score float64
	var ok bool

	// ① 同键对复用（与主线同一套规则）：位移极小 => 只在小区域里找
	if px, py, ps, pok, used := vp.tryPairReuse(screen, k, pred, img, iox, ioy, size, color); used {
		x, y, score, ok = px, py, ps, pok
		if ok {
			vp.matcher.pairHit.Add(1)
		} else {
			// 与主线同一条规则：同一键对必须落在同一处，极小区没命中就按录制位移换算
			x, y, score, ok = pred.x, pred.y, vp.witnessScore(pred), true
			vp.matcher.pairHit.Add(1)
		}
	} else {
		// ②③ 受限区域逐级扩大：与主线同一条规则，置信度不足就扩大范围，
		//     免得预取出来的"大概位置"其实是区域外目标的相似邻居。
		for _, tr := range vp.regionTiers(pred, cache, hasCache, size) {
			vp.matcher.cacheTry.Add(1)
			x, y, score, ok = vp.matcher.searchIn(screen, img, iox, ioy, color, tr.region)
			if !ok {
				continue
			}
			if !vp.matcher.acceptRegion(score) {
				vp.matcher.regionLow.Add(1)
				vdebug("预取 锚点#%d %s 命中但置信度不足 (%.1f%% < %.0f%%) => 扩大范围",
					k, tr.name, score, vp.matcher.confirmPercent)
				ok = false
				continue
			}
			vp.matcher.cacheHit.Add(1)
			break
		}
	}
	// ④ 仍然不行才全屏（限流）
	if !ok {
		select {
		case <-cancel:
			return
		default:
		}
		vp.fullSem <- struct{}{}
		x, y, score, ok = vp.matcher.searchIn(screen, img, iox, ioy, color, image.Rectangle{})
		<-vp.fullSem
		vp.matcher.full.Add(1)
	}
	vp.matcher.prefetch.Add(1)
	if ok {
		vp.matcher.prefetchOK.Add(1)
	}

	// 写回候选（世代未变、且尚未被主线确认才采纳）
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if _, running := vp.jobs[k]; !running {
		return // 已被取消/替换
	}
	delete(vp.jobs, k)
	if vp.closed || gen != vp.gen || vp.plan.anchors[k].resolved {
		return
	}
	c := &vp.plan.anchors[k]
	c.hasCand = true
	c.candGen = gen
	c.candScore = score
	if ok {
		c.candX, c.candY = x, y
	} else {
		// 预取失败：仍然给出"预测位置"作为候选，由主线复核决定
		c.candX, c.candY = pred.x, pred.y
		c.hasCand = false
	}
	if ok {
		vdebug("预取成功 锚点#%d -> (%d,%d) 匹配度=%.1f%%", k, x, y, score)
	} else {
		vdebug("预取失败 锚点#%d (预测 %d,%d)", k, pred.x, pred.y)
	}
}

// ---------------------------------------------------------------------------
// 同键对并发快速扫描
// ---------------------------------------------------------------------------

// pairScanResult 一次配对快速扫描的单侧结果
type pairScanResult struct {
	who   int // 0 = 按下(aIdx)，1 = 松开(bIdx)
	x, y  int
	score float64
	ok    bool
}

// ResolvePairFast 同键对并发快速扫描。
//
// 鼠标按下与松开两张图位置几乎相同时，让它们在**同一个快速区域里同时找**：
// 谁先命中就用谁的结果，另一张按录制位移直接换算；
// 一旦有一侧命中，另一侧还在进行的扫描立刻取消（"谁先亮灯用谁的"）。
//
// 好处：不必等慢的那一侧。若按下那张在这个区域里找不到、本会滑向全屏搜索，
// 只要松开那张在区域内命中，这一对就已经完成，不会被拖住。
//
// **只在快速 cache 路径内生效**：没有预测、没有可用区域、或两侧都没命中时返回
// false，由调用方回到常规路径（该项行为不变）。
func (vp *visionPipeline) ResolvePairFast(aIdx, bIdx int) bool {
	if vp == nil || vp.plan == nil || aIdx < 0 || bIdx < 0 ||
		aIdx >= len(vp.plan.anchors) || bIdx >= len(vp.plan.anchors) {
		return false
	}

	vp.mu.Lock()
	if vp.closed || vp.plan.anchors[aIdx].resolved || vp.plan.anchors[bIdx].resolved {
		vp.mu.Unlock()
		return false
	}
	a, b := vp.plan.anchors[aIdx], vp.plan.anchors[bIdx]
	pred := vp.plan.predictionFor(aIdx)
	cache, hasCache := vp.cache, vp.hasCache
	vp.mu.Unlock()

	// 只处理"两张图几乎同一张"的情形
	dx := b.rawX - a.rawX
	dy := b.rawY - a.rawY
	if absInt(dx) > vp.pairTol || absInt(dy) > vp.pairTol {
		return false
	}
	// 没有预测来源就没有"快速区域"可言
	if !pred.has || !hasCache {
		return false
	}

	// 快速区域：覆盖两张图的预测位置（预测差就是录制位移，已在阈值内）
	region := moveRegion(cache, pred.x, pred.y, a.size, vp.screenW, vp.screenH)

	screen, err := vp.grab()
	if err != nil {
		return false
	}
	defer screen.Close()

	cancel := make(chan struct{})
	ch := make(chan pairScanResult, 2)
	targets := []visionAnchor{a, b}
	idxs := []int{aIdx, bIdx}
	for i := range targets {
		go func(i int) {
			t := targets[i]
			// 大区域才值得做并发；模板载入后、真正匹配前再查一次取消
			x, y, score, ok := vp.matcher.searchIn(screen, t.img, t.iox, t.ioy,
				t.mode == GoInput.VisionModeColor, region)
			select {
			case <-cancel:
			default:
			}
			ch <- pairScanResult{who: i, x: x, y: y, score: score, ok: ok}
		}(i)
	}

	var win pairScanResult
	found := false
	low := false
	for i := 0; i < 2; i++ {
		r := <-ch
		// "亮灯"必须是可信的命中：这块区域同样可能只圈到一个相似目标
		if r.ok && !vp.matcher.acceptRegion(r.score) {
			low = true
			continue
		}
		if r.ok && !found {
			win, found = r, true
			close(cancel) // 另一侧还在跑的扫描作废
		}
	}
	if !found {
		vp.matcher.pairTry.Add(1)
		if low {
			vp.matcher.regionLow.Add(1)
			vdebug("同键对并发扫描：有命中但置信度不足 (门槛 %.0f%%) => 常规路径",
				vp.matcher.confirmPercent)
		} else {
			vdebug("同键对并发扫描：两侧都未命中 (%d,%d) => 常规路径", aIdx, bIdx)
		}
		return false
	}

	// 命中侧用它自己的位置；另一侧按录制位移换算（两张图本来就是同一处）
	vp.matcher.pairHit.Add(1)
	w := targets[win.who]
	loseIdx := idxs[1-win.who]
	vp.commit(idxs[win.who], win.x, win.y, win.score, true)
	vp.mu.Lock()
	la := vp.plan.anchors[loseIdx]
	lx := win.x + (la.rawX - w.rawX)
	ly := win.y + (la.rawY - w.rawY)
	vp.mu.Unlock()
	vp.commit(loseIdx, lx, ly, win.score, true)

	vdebug("同键对并发扫描：%s 先命中 (%d,%d) 匹配度=%.1f%%，%s 按位移换算 (%d,%d)",
		w.img, win.x, win.y, win.score, vp.anchorImg(loseIdx), lx, ly)
	return true
}

// anchorImg 取锚点样本名（加锁）
func (vp *visionPipeline) anchorImg(k int) string {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if k < 0 || k >= len(vp.plan.anchors) {
		return ""
	}
	return vp.plan.anchors[k].img
}

// ---------------------------------------------------------------------------
// 几何工具
// ---------------------------------------------------------------------------

// regionAround 以 (cx,cy) 为中心、边长 side+2*radius 的矩形（按画面裁剪）
func regionAround(cx, cy, side, radius, screenW, screenH int) image.Rectangle {
	side += 2 * radius
	if side < 1 {
		side = 1
	}
	half := side / 2
	return clampToScreen(image.Rect(cx-half, cy-half, cx-half+side, cy-half+side), screenW, screenH)
}

// moveRegion 把快速区域平移到这次预测的位置。
//
// 语义：以预测**光标位置**为中心、边长 size×cacheRatio 的窗口，再按模板尺寸外扩，
// 因此只要真实位置落在预测的 ±(边长/2) 以内就能在区域内找到；
// 超出去就交给下一级（预测附近区域 -> 全屏）处理。
func moveRegion(cache image.Rectangle, cx, cy, size, screenW, screenH int) image.Rectangle {
	side := cache.Dx()
	if side < size*2 {
		side = size * DefaultVisionCacheFactor
	}
	return regionAround(cx, cy, side, size/2, screenW, screenH)
}

func clampToScreen(r image.Rectangle, w, h int) image.Rectangle {
	out := r.Intersect(image.Rect(0, 0, w, h))
	if out.Dx() <= 0 || out.Dy() <= 0 {
		return image.Rectangle{}
	}
	return out
}

// summary 回放结束后的识别统计
func (m *visionMatcher) summary() string {
	if m == nil {
		return ""
	}
	full, hit, try := m.full.Load(), m.cacheHit.Load(), m.cacheTry.Load()
	low := m.regionLow.Load()
	pair := m.pairHit.Load()
	pre, preOK := m.prefetch.Load(), m.prefetchOK.Load()
	ver, ok, no := m.verify.Load(), m.recheckOK.Load(), m.recheckNo.Load()
	if full+hit+preOK+pair+low == 0 {
		return ""
	}
	return fmt.Sprintf("[Vision] 识别流水线统计: 全屏标准识别 %d 次，同键对复用 %d 次，受限区域命中 %d/%d 次(置信度不足扩大范围 %d 次)，后台预取 %d 次(命中 %d)，复核 %d 次(通过 %d / 重新定位 %d)",
		full, pair, hit, try, low, pre, preOK, ver, ok, no)
}
